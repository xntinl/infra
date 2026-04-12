# `:telemetry.span/3` — start, stop, and exception in one call

**Project**: `telemetry_spans` — wraps a piece of work with `:telemetry.span/3` so a single function call emits the `:start` / `:stop` / `:exception` trio that every tracing backend expects.

**Difficulty**: ★★★☆☆
**Estimated time**: 2 hours

---

## Project context

If exercise 28 taught you to attach a handler, this one teaches you how
work-spanning libraries (Ecto, Phoenix, Finch, Oban) actually emit their
events: with `:telemetry.span/3`. A span is the canonical shape of
"something started, something happened, something ended (or blew up)".
Every distributed-tracing backend (OpenTelemetry, Tapestry, Datadog APM)
interprets the three-event trio natively; every metrics library uses the
`:stop` event's `duration` measurement.

You'll build a small `Worker` module that runs a function inside a span,
add a handler that records the trio, and cover the three termination
paths: normal return, raised exception, and exited task. You'll also see
how `span/3` differs from a hand-rolled start/stop pair — the differences
are in exception safety and the measurement shape, both of which you'd
otherwise get wrong.

Project structure:

```
telemetry_spans/
├── lib/
│   └── telemetry_spans.ex
├── test/
│   └── telemetry_spans_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The three events of a span

For a span with prefix `[:my_app, :work]`:

| Event                              | When                     | Measurements                                  | Metadata                                           |
|------------------------------------|--------------------------|-----------------------------------------------|----------------------------------------------------|
| `[:my_app, :work, :start]`         | Before `fun.()`          | `%{monotonic_time: _, system_time: _}`        | The `start_metadata` you passed.                   |
| `[:my_app, :work, :stop]`          | After `fun.()` returns   | `%{monotonic_time: _, duration: _}`           | Merge of start metadata + the map you returned.    |
| `[:my_app, :work, :exception]`     | On raise/throw/exit      | `%{monotonic_time: _, duration: _}`           | `%{kind:, reason:, stacktrace:}` + start metadata. |

Note: `:exception` replaces `:stop`. You never see both for the same span.

### 2. The return shape — `{result, extra_metadata}`

```elixir
:telemetry.span([:my_app, :work], %{user: id}, fn ->
  result = do_stuff()
  {result, %{bytes_written: byte_size(result)}}
end)
```

`span/3` unwraps the tuple: it returns `result` to the caller, and it
merges `extra_metadata` into the `:stop` event. This lets you compute
observability data from the result (bytes, row count, cache hit?) without
threading it through a separate handler.

### 3. Exception propagation

If `fun` raises/throws/exits, `span/3` emits `:exception` with the error
details and then **re-raises**. The caller sees the original exception —
`span/3` is transparent to failure paths. This is what makes it safe to
sprinkle liberally.

### 4. Monotonic time — not wall clock

`duration` is measured with `:erlang.monotonic_time/0`: it only goes
forward, unaffected by NTP jumps or DST. Use it for latency. Use
`system_time` (also emitted) if you need a wall-clock timestamp for
correlation with external systems.

### 5. Why not a hand-rolled start/stop?

A tempting shortcut:

```elixir
:telemetry.execute(prefix ++ [:start], ...)
t0 = System.monotonic_time()
result = fun.()
:telemetry.execute(prefix ++ [:stop], %{duration: System.monotonic_time() - t0}, ...)
```

It *almost* works. But:

- If `fun.()` raises, `:stop` is never emitted and your backend sees an
  orphan `:start`. `span/3` emits `:exception` instead.
- It's easy to forget `:start` or mis-key `duration`. `span/3` is one
  line and correct by construction.
- Libraries that auto-discover spans (OpenTelemetry) look for
  `[prefix, :start]` / `[prefix, :stop]` / `[prefix, :exception]`
  specifically. Deviating breaks auto-instrumentation.

---

## Implementation

### Step 1: Create the project

```bash
mix new telemetry_spans
cd telemetry_spans
```

Add `:telemetry` in `mix.exs`:

```elixir
defp deps do
  [{:telemetry, "~> 1.2"}]
end
```

Run `mix deps.get`.

### Step 2: `lib/telemetry_spans.ex`

```elixir
defmodule TelemetrySpans do
  @moduledoc """
  A tiny demo of `:telemetry.span/3`. `do_work/2` wraps a caller-provided
  function so a single invocation emits :start and then either :stop or
  :exception — the exact trio every tracing backend understands.
  """

  @event_prefix [:telemetry_spans, :work]

  @doc "Event prefix used by `do_work/2` — convenient for attaching handlers."
  def event_prefix, do: @event_prefix

  @doc """
  Runs `fun` inside `:telemetry.span/3`.

  * On success, emits `:start` then `:stop`. `:stop`'s metadata includes
    `result_tag: :ok` plus anything the caller returned in the extra map.
  * On raise/throw/exit, emits `:start` then `:exception`, then re-raises
    (so the caller sees the original failure).
  """
  @spec do_work((-> any()), map()) :: any()
  def do_work(fun, start_metadata \\ %{}) when is_function(fun, 0) do
    :telemetry.span(@event_prefix, start_metadata, fn ->
      result = fun.()
      # Second element is merged into :stop metadata.
      {result, %{result_tag: :ok}}
    end)
  end
end
```

### Step 3: `test/telemetry_spans_test.exs`

```elixir
defmodule TelemetrySpansTest do
  use ExUnit.Case, async: false
  # :telemetry handlers are global; serial avoids cross-test interference.

  setup do
    parent = self()
    handler_id = "spans-#{System.unique_integer([:positive])}"
    prefix = TelemetrySpans.event_prefix()

    :ok =
      :telemetry.attach_many(
        handler_id,
        [prefix ++ [:start], prefix ++ [:stop], prefix ++ [:exception]],
        fn event, measurements, metadata, _config ->
          send(parent, {:span, event, measurements, metadata})
        end,
        nil
      )

    on_exit(fn -> :telemetry.detach(handler_id) end)

    :ok
  end

  describe "do_work/2 — success path" do
    test "emits :start then :stop with a non-negative duration" do
      assert 42 == TelemetrySpans.do_work(fn -> 42 end, %{job: "add"})

      assert_receive {:span, [:telemetry_spans, :work, :start],
                      %{monotonic_time: _, system_time: _}, %{job: "add"}}

      assert_receive {:span, [:telemetry_spans, :work, :stop],
                      %{monotonic_time: _, duration: duration},
                      %{job: "add", result_tag: :ok}}

      assert is_integer(duration) and duration >= 0
    end

    test "does NOT emit :exception on success" do
      TelemetrySpans.do_work(fn -> :ok end)

      # Drain start and stop.
      assert_receive {:span, [:telemetry_spans, :work, :start], _, _}
      assert_receive {:span, [:telemetry_spans, :work, :stop], _, _}

      refute_receive {:span, [:telemetry_spans, :work, :exception], _, _}, 50
    end
  end

  describe "do_work/2 — exception path" do
    test "emits :start then :exception, and re-raises" do
      assert_raise RuntimeError, "boom", fn ->
        TelemetrySpans.do_work(fn -> raise "boom" end, %{job: "explode"})
      end

      assert_receive {:span, [:telemetry_spans, :work, :start], _, %{job: "explode"}}

      assert_receive {:span, [:telemetry_spans, :work, :exception],
                      %{duration: _},
                      %{job: "explode", kind: :error, reason: %RuntimeError{message: "boom"},
                        stacktrace: st}}

      assert is_list(st)
    end

    test "does NOT emit :stop when the work raises" do
      catch_error(TelemetrySpans.do_work(fn -> raise "nope" end))

      assert_receive {:span, [:telemetry_spans, :work, :start], _, _}
      assert_receive {:span, [:telemetry_spans, :work, :exception], _, _}
      refute_receive {:span, [:telemetry_spans, :work, :stop], _, _}, 50
    end

    test "a `throw` is captured as :exception with kind: :throw" do
      catch_throw(TelemetrySpans.do_work(fn -> throw(:abort) end))

      assert_receive {:span, [:telemetry_spans, :work, :start], _, _}
      assert_receive {:span, [:telemetry_spans, :work, :exception], _,
                      %{kind: :throw, reason: :abort}}
    end

    test "a `exit` is captured as :exception with kind: :exit" do
      catch_exit(TelemetrySpans.do_work(fn -> exit(:gone) end))

      assert_receive {:span, [:telemetry_spans, :work, :start], _, _}
      assert_receive {:span, [:telemetry_spans, :work, :exception], _,
                      %{kind: :exit, reason: :gone}}
    end
  end

  describe "caller-supplied extra metadata" do
    test "extra map from the body is merged into :stop metadata" do
      # Override do_work locally to return extra stop metadata.
      result =
        :telemetry.span([:telemetry_spans, :work], %{op: "hash"}, fn ->
          digest = :crypto.hash(:sha256, "hello") |> Base.encode16(case: :lower)
          {digest, %{bytes: 5, cached?: false}}
        end)

      assert is_binary(result)

      assert_receive {:span, [:telemetry_spans, :work, :stop], _,
                      %{op: "hash", bytes: 5, cached?: false}}
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. `span/3` re-raises — don't swallow the exception in a handler**
The `:exception` event includes the full stacktrace. Your handler should
record it (or forward it to a tracer) and return quickly; if it tries to
"handle" the error it is running after `span/3` has already decided to
re-raise. Swallowing is impossible here, which is correct.

**2. Measurements are in `:native` units**
`duration` is raw monotonic ticks. Convert at the consumer
(`System.convert_time_unit(duration, :native, :millisecond)` or via
`Telemetry.Metrics` unit conversion). Do not convert in the span body —
you'd be taxing every caller for a display concern.

**3. `monotonic_time` alone isn't a wall-clock timestamp**
If you need "when did this happen, to correlate with an access log", use
`system_time` from the `:start` event. `monotonic_time` is only
meaningful as a duration basis on the same machine.

**4. Metadata must be safe to serialize**
Reporters may JSON-encode metadata, send it over a pipe, or log it.
Avoid large binaries, PIDs, and functions. Keep it to primitives and
small structs.

**5. Nested spans are fine, but think about parent-child correlation**
`:telemetry.span/3` doesn't carry a span id. OpenTelemetry's Elixir SDK
wraps `:telemetry` events and injects span/trace ids via the process
dictionary. If you roll your own tracing, you'll need to thread
correlation ids via metadata manually.

**6. Never emit `:start` / `:stop` / `:exception` by hand if you can use `span/3`**
The manual form is error-prone (forgotten `:stop` on exception, wrong
duration key, wrong metadata shape). Only roll your own when you
*cannot* wrap the work in a function — e.g. asynchronous operations
where start and end live in different processes. For those, consider
whether two independent events make more sense than a span.

**7. When NOT to span**
Per-invocation overhead is small but not free: ETS lookup + handler
dispatch per event. Inside tight CPU loops (per-row in a 1M-row stream),
span the *outer* batch, not each iteration. Metric aggregation handles
the rest.

---

## Resources

- [`:telemetry.span/3` — hexdocs](https://hexdocs.pm/telemetry/telemetry.html#span/3)
- [`:telemetry` overview](https://hexdocs.pm/telemetry/)
- [Dashbit blog — Telemetry deep dives and handler patterns](https://dashbit.co/blog/)
- [OpenTelemetry Erlang/Elixir](https://hexdocs.pm/opentelemetry/) — how real tracing libraries consume spans
- [Phoenix telemetry guide](https://hexdocs.pm/phoenix/telemetry.html) — real-world examples of spans for requests and LiveView events
- [Ecto telemetry events](https://hexdocs.pm/ecto/Ecto.Repo.html#module-telemetry-events) — another library emitting the same trio
