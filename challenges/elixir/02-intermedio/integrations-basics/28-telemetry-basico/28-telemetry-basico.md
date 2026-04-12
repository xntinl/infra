# Telemetry basics: attach, execute, and a first handler

**Project**: `telemetry_intro` — a tiny module that emits `:telemetry` events and an attached handler that counts them.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Before you reach for PromEx, `Telemetry.Metrics`, or OpenTelemetry, you need
a mental model for the thing underneath: the `:telemetry` library itself.
It is astonishingly small — effectively a globally-registered dispatch table
that maps an event name (a list of atoms) to a list of handler functions.
There is no aggregation, no sampling, no transport. Phoenix, Ecto, Finch,
Oban and Broadway all emit `:telemetry` events; every metrics/tracing library
you'll use in production is "just" a handler attached to those events.

In this exercise you'll emit your own events, attach a handler, and observe
how the three-argument event shape (`measurements`, `metadata`, `config`) is
designed for cheap emission and flexible aggregation downstream. You'll also
meet `:telemetry.span/3`, which is the canonical way to wrap a piece of work
and emit `:start` / `:stop` / `:exception` as a trio.

Project structure:

```
telemetry_intro/
├── lib/
│   ├── telemetry_intro.ex
│   └── telemetry_intro/
│       └── counter_handler.ex
├── test/
│   └── telemetry_intro_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Event names are lists of atoms

```elixir
[:my_app, :repo, :query]
[:phoenix, :endpoint, :stop]
```

The convention is `[:app, :component, :action]`. Lists let handlers filter by
prefix in the future (some libraries like `Telemetry.Metrics` match on the
full path). Always use atoms — strings would mean unbounded atom creation,
which is a BEAM footgun.

### 2. `:telemetry.execute/3` — three maps, nothing more

```elixir
:telemetry.execute([:my_app, :order, :created], %{count: 1, amount: 99}, %{order_id: id})
```

* **measurements** — numeric data (counts, durations, sizes). Handlers aggregate this.
* **metadata** — context (ids, tags, user info). Handlers use it for labels/spans.
* **config** — static per-handler data passed to your handler (see `attach/4`).

This split matters: metrics libraries consume measurements as floats and
cardinalize on metadata. Putting a free-form string in measurements and a
number in metadata is a common mistake.

### 3. `:telemetry.attach/4` — global, synchronous, and runs in the caller

```elixir
:telemetry.attach(
  "my-handler-id",
  [:my_app, :order, :created],
  &MyHandler.handle/4,
  _config = %{}
)
```

The handler fires **synchronously on the process that calls `execute/3`**.
If your handler crashes, `:telemetry` detaches it and logs a warning —
one bad handler won't take down emitters, but a slow handler blocks them.
Handlers must be fast and side-effect-free beyond sending a message or
bumping an ETS counter.

Prefer `attach_many/4` when one handler covers multiple events — it avoids
N separate lookups in the dispatch table.

### 4. `:telemetry.span/3` — start/stop/exception in one call

```elixir
:telemetry.span([:my_app, :work], %{user: id}, fn ->
  result = do_work()
  {result, %{bytes: byte_size(result)}}
end)
```

`span/3` emits `[:my_app, :work, :start]` before, `[:my_app, :work, :stop]`
after (with `duration` in native time units), or `[:my_app, :work, :exception]`
if the function raises. This is the shape every distributed-tracing backend
expects; roll your own at your peril.

---

## Implementation

### Step 1: Create the project

```bash
mix new telemetry_intro
cd telemetry_intro
```

Add `:telemetry` to `mix.exs`:

```elixir
defp deps do
  [
    {:telemetry, "~> 1.2"}
  ]
end
```

Then `mix deps.get`.

### Step 2: `lib/telemetry_intro.ex`

```elixir
defmodule TelemetryIntro do
  @moduledoc """
  Emits domain events via `:telemetry` and provides a tiny in-memory counter
  handler. This is the same mechanism Phoenix, Ecto and Finch use — with a
  fancier handler on the other end.
  """

  @order_created [:telemetry_intro, :order, :created]
  @work_event [:telemetry_intro, :work]

  @doc "Event name emitted when an order is created."
  def order_created_event, do: @order_created

  @doc "Event *prefix* used by `do_work/2` via `:telemetry.span/3`."
  def work_event, do: @work_event

  @doc """
  Records an order. Emits `[:telemetry_intro, :order, :created]` with
  `%{count: 1, amount: amount}` measurements and `%{order_id: id}` metadata.
  """
  @spec record_order(String.t(), number()) :: :ok
  def record_order(order_id, amount) when is_binary(order_id) and is_number(amount) do
    :telemetry.execute(
      @order_created,
      %{count: 1, amount: amount},
      %{order_id: order_id}
    )
  end

  @doc """
  Runs `fun` inside `:telemetry.span/3`. Emits `:start` / `:stop` with
  `duration` in native time units, or `:exception` if `fun` raises.

  The caller passes a plain 0-arity function; we wrap it in the span's
  `{result, extra_metadata}` shape.
  """
  @spec do_work((-> any()), map()) :: any()
  def do_work(fun, start_metadata \\ %{}) when is_function(fun, 0) do
    :telemetry.span(@work_event, start_metadata, fn ->
      result = fun.()
      # The second element is *additional* metadata merged into :stop.
      {result, %{result_byte_size: byte_size(to_string(result))}}
    end)
  end
end
```

### Step 3: `lib/telemetry_intro/counter_handler.ex`

```elixir
defmodule TelemetryIntro.CounterHandler do
  @moduledoc """
  A minimal handler: forwards every event it sees to a target pid as
  `{:telemetry_event, event, measurements, metadata}`. Useful for tests
  and for understanding handler semantics without bringing in ETS.
  """

  @doc """
  Attaches a handler under `handler_id` that forwards events to `target_pid`.
  `events` is a list of event names.
  """
  @spec attach(String.t(), [[atom()]], pid()) :: :ok | {:error, :already_exists}
  def attach(handler_id, events, target_pid) do
    :telemetry.attach_many(
      handler_id,
      events,
      &__MODULE__.handle/4,
      %{target: target_pid}
    )
  end

  @spec detach(String.t()) :: :ok | {:error, :not_found}
  def detach(handler_id), do: :telemetry.detach(handler_id)

  # Arity 4: event, measurements, metadata, config — the fixed handler shape.
  @doc false
  def handle(event, measurements, metadata, %{target: pid}) do
    send(pid, {:telemetry_event, event, measurements, metadata})
  end
end
```

### Step 4: `test/telemetry_intro_test.exs`

```elixir
defmodule TelemetryIntroTest do
  use ExUnit.Case, async: false
  # async: false — :telemetry handlers are global process state, so parallel
  # tests that attach/detach the same handler_id would step on each other.

  alias TelemetryIntro.CounterHandler

  setup do
    handler_id = "test-#{System.unique_integer([:positive])}"

    on_exit(fn ->
      # Best effort — detach may already have happened in-test.
      _ = :telemetry.detach(handler_id)
    end)

    {:ok, handler_id: handler_id}
  end

  describe "record_order/2" do
    test "emits the expected event with measurements and metadata",
         %{handler_id: id} do
      :ok = CounterHandler.attach(id, [TelemetryIntro.order_created_event()], self())

      TelemetryIntro.record_order("order-1", 42.50)

      assert_receive {:telemetry_event, [:telemetry_intro, :order, :created],
                      %{count: 1, amount: 42.50}, %{order_id: "order-1"}}
    end

    test "multiple orders fire multiple events in order", %{handler_id: id} do
      :ok = CounterHandler.attach(id, [TelemetryIntro.order_created_event()], self())

      TelemetryIntro.record_order("a", 1)
      TelemetryIntro.record_order("b", 2)

      assert_receive {:telemetry_event, _, %{amount: 1}, %{order_id: "a"}}
      assert_receive {:telemetry_event, _, %{amount: 2}, %{order_id: "b"}}
    end
  end

  describe "do_work/2 — :telemetry.span" do
    test "emits :start and :stop with a duration measurement", %{handler_id: id} do
      prefix = TelemetryIntro.work_event()
      :ok = CounterHandler.attach(id, [prefix ++ [:start], prefix ++ [:stop]], self())

      result = TelemetryIntro.do_work(fn -> "done" end, %{job: "test"})

      assert result == "done"
      assert_receive {:telemetry_event, [:telemetry_intro, :work, :start],
                      %{monotonic_time: _, system_time: _}, %{job: "test"}}
      assert_receive {:telemetry_event, [:telemetry_intro, :work, :stop],
                      %{duration: duration},
                      %{job: "test", result_byte_size: 4}}
      assert is_integer(duration) and duration >= 0
    end

    test "emits :exception when the work raises", %{handler_id: id} do
      prefix = TelemetryIntro.work_event()
      :ok = CounterHandler.attach(id, [prefix ++ [:exception]], self())

      assert_raise RuntimeError, "boom", fn ->
        TelemetryIntro.do_work(fn -> raise "boom" end)
      end

      assert_receive {:telemetry_event, [:telemetry_intro, :work, :exception],
                      %{duration: _},
                      %{kind: :error, reason: %RuntimeError{message: "boom"}}}
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

---

## Trade-offs and production gotchas

**1. Handlers run synchronously in the emitter's process**
A slow handler (e.g. one that does I/O, or logs under load) becomes
backpressure for every caller. Never make HTTP calls, DB calls, or heavy
computation inside a handler. Cast to a GenServer or write to ETS/atomics.

**2. Exceptions detach your handler**
If your handler raises, `:telemetry` logs a warning and **permanently
detaches it**. The emitter keeps running, but your metrics silently stop.
Always wrap risky work in try/rescue inside the handler, or — better — do
nothing that could raise.

**3. Cardinality lives in metadata, not event names**
Don't encode high-cardinality values (user id, request id) in the event
name itself — that explodes your dispatch table. Put them in metadata, and
let the metrics library decide which metadata keys become labels.

**4. Anonymous functions as handlers are a warning source**
Since `:telemetry` 1.0, attaching an anonymous function works but logs
"using local function capture" warnings because they can't be upgraded on
hot code reload. Always use a `&Module.fun/4` capture in production code.

**5. `attach_many` beats N × `attach`**
One `attach_many/4` with a list of events is cheaper at attach time and
keeps your handler ids manageable. Prefer it once you have more than one
event.

**6. When NOT to roll your own handler**
For real metrics, use `Telemetry.Metrics` + a reporter (Prometheus,
StatsD, LiveDashboard). For tracing, use OpenTelemetry. A handwritten
handler is great for in-process logic (audit trails, internal counters,
feature flags) but a poor substitute for the real ecosystem.

---

## Resources

- [`:telemetry` — hexdocs](https://hexdocs.pm/telemetry/) — the entire API in ~5 functions
- [`:telemetry.execute/3`](https://hexdocs.pm/telemetry/telemetry.html#execute/3)
- [`:telemetry.span/3`](https://hexdocs.pm/telemetry/telemetry.html#span/3)
- [Dashbit blog: writing efficient Telemetry handlers](https://dashbit.co/blog/)
- [`Telemetry.Metrics`](https://hexdocs.pm/telemetry_metrics/) — the aggregation layer on top
- [Phoenix telemetry guide](https://hexdocs.pm/phoenix/telemetry.html) — real-world example of events in the wild
