# GenStage buffering — `:buffer_size`, `:buffer_keep`, and back-pressure limits

**Project**: `buffer_demand` — explore what happens when a Producer emits
*faster* than downstream demand, using explicit buffer configuration to
control overflow behavior.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Why genstage buffer demand matters

GenStage is demand-driven by default, so a Producer only emits what was
asked for. But real producers often have an *external* source that pushes
data regardless of demand — a TCP socket, a message queue, a timer that
polls. That data arrives whether or not the Consumer wants it yet.

GenStage's solution is the producer-side buffer. By setting `:buffer_size`
and `:buffer_keep` in a Producer's init, you tell GenStage to hold pending
events until demand arrives, and to decide which events to drop if the
buffer overflows. This is the configurable "how much back-pressure
tolerance do I have?" knob.

---

## Project structure

```
buffer_demand/
├── lib/
│   └── buffer_demand.ex
├── script/
│   └── main.exs
├── test/
│   └── buffer_demand_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Push vs pull producers

A *pull* producer computes events in `handle_demand/2` — like a simple
counter producer. A *push* producer receives events from outside (via `cast`, raw
`send`, a port) and calls `GenStage.reply/2` or returns events from
`handle_cast/2`. The buffer exists so push events can arrive faster than
demand briefly without losing data.

### 2. `:buffer_size` and `:buffer_keep`

```elixir
def init(state) do
  {:producer, state, buffer_size: 10_000, buffer_keep: :last}
end
```

- `:buffer_size` — max events held when no demand is available.
  Default is `10_000`; `:infinity` means unbounded (dangerous in prod).
- `:buffer_keep` — on overflow:
  - `:first` (default) — drop new events, keep oldest.
  - `:last` — drop oldest, keep newest.

The right choice depends on semantics: for *deltas* (current-state
snapshots) you want `:last` so you never fall behind reality. For
*events* that each matter individually (audit log), `:first` is safer —
or better, use a durable queue.

### 3. Emitting from outside `handle_demand`

In a push producer, you return events from `handle_cast/2` or
`handle_info/2`:

```elixir
def handle_cast({:emit, event}, state) do
  {:noreply, [event], state}
end
```

If there's pending demand, the event goes straight to the Consumer.
Otherwise it's buffered (subject to `:buffer_size` / `:buffer_keep`).

### 4. Overflow is silent by default

Dropped events disappear with no warning. GenStage doesn't log; your
Consumer just sees a gap. Always instrument your producer with
`:telemetry` events for drops, or a counter you can observe externally.

---

## Design decisions

**Option A — Ad-hoc implementation without OTP primitives**
- Pros: Less ceremony; the genstage buffer demand flow fits in a single short module.
- Cons: Reinvents supervision, restart, back-pressure, and observability — the four properties OTP gives us for free.

**Option B — Use the canonical OTP shape for genstage buffer demand** (chosen)
- Pros: Predictable failure semantics; integrates with `:observer`, telemetry, and supervision trees; future maintainers recognise the pattern.
- Cons: One extra layer of indirection; you must learn the callback shape and the lifecycle rules.

Chose **B** because the abstraction cost is paid once and its benefits are paid every day — especially in production where partial failure is the norm, not the exception.

## Implementation

### `mix.exs`

```elixir
defmodule BufferDemand.MixProject do
  use Mix.Project

  def project do
    [
      app: :buffer_demand,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new buffer_demand --sup
cd buffer_demand
```

Add `gen_stage` to `mix.exs`:

```elixir
defp deps, do: [{:gen_stage, "~> 1.2"}]
```

Then `mix deps.get`.

### `lib/buffer_demand/push_producer.ex`

**Objective**: Implement `push_producer.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.

```elixir
defmodule BufferDemand.PushProducer do
  @moduledoc """
  A push-style GenStage Producer. External callers send events via
  `emit/2`; the producer forwards immediately if demand exists, otherwise
  buffers up to `buffer_size`. Older events are dropped on overflow
  (`:buffer_keep, :last`) — common for telemetry use cases where newer
  is more valuable.
  """

  use GenStage

  @default_buffer_size 100

  def start_link(opts \\ []) do
    buffer_size = Keyword.get(opts, :buffer_size, @default_buffer_size)
    buffer_keep = Keyword.get(opts, :buffer_keep, :last)
    GenStage.start_link(__MODULE__, {buffer_size, buffer_keep}, name: __MODULE__)
  end

  @doc "Returns emit result from producer and event."
  @spec emit(GenServer.server(), any()) :: :ok
  def emit(producer \\ __MODULE__, event), do: GenStage.cast(producer, {:emit, event})

  @impl true
  def init({buffer_size, buffer_keep}) do
    {:producer, :no_state, buffer_size: buffer_size, buffer_keep: buffer_keep}
  end

  @impl true
  def handle_cast({:emit, event}, state) do
    # Returning [event] gives GenStage the event; it either dispatches it
    # immediately to satisfy pending demand or buffers it. We never touch
    # the buffer ourselves.
    {:noreply, [event], state}
  end

  @doc "Handles demand result from _demand and state."
  @impl true
  def handle_demand(_demand, state) do
    # Pull-producer contract requires this callback. In a pure push
    # producer we have nothing to generate on demand — the buffer handles
    # it. Return no events; GenStage will record the demand and use it
    # when the next push arrives.
    {:noreply, [], state}
  end
end
```

### `lib/buffer_demand/slow_consumer.ex`

**Objective**: Implement `slow_consumer.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.

```elixir
defmodule BufferDemand.SlowConsumer do
  @moduledoc """
  A deliberately slow Consumer — sleeps `delay_ms` per batch — so we can
  observe buffer behavior by outpacing it from the producer side.
  """

  use GenStage

  def start_link({notify_pid, delay_ms}) when is_pid(notify_pid) do
    GenStage.start_link(__MODULE__, {notify_pid, delay_ms})
  end

  @impl true
  def init({notify_pid, delay_ms}) do
    {:consumer, {notify_pid, delay_ms},
     subscribe_to: [{BufferDemand.PushProducer, max_demand: 5, min_demand: 2}]}
  end

  @doc "Handles events result from events, _from and delay_ms."
  @impl true
  def handle_events(events, _from, {notify_pid, delay_ms} = state) do
    Process.sleep(delay_ms)
    for e <- events, do: send(notify_pid, {:received, e})
    {:noreply, [], state}
  end
end
```

### `lib/buffer_demand.ex`

**Objective**: Implement `buffer_demand.ex` — the lazy operator whose resource and memory profile only becomes visible when the stream is actually run.

```elixir
defmodule BufferDemand do
  @moduledoc "Starts a push producer and slow consumer pair."

  alias BufferDemand.{PushProducer, SlowConsumer}

  @doc "Starts pipeline result from notify_pid and opts."
  def start_pipeline(notify_pid, opts \\ []) do
    producer_opts = Keyword.take(opts, [:buffer_size, :buffer_keep])
    delay_ms = Keyword.get(opts, :delay_ms, 10)

    {:ok, _p} = PushProducer.start_link(producer_opts)
    {:ok, consumer} = SlowConsumer.start_link({notify_pid, delay_ms})
    {:ok, consumer}
  end
end
```

### Step 5: `test/buffer_demand_test.exs`

**Objective**: Write `buffer_demand_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule BufferDemandTest do
  use ExUnit.Case, async: false

  doctest BufferDemand

  setup do
    on_exit(fn ->
      if pid = Process.whereis(BufferDemand.PushProducer), do: GenStage.stop(pid)
    end)

    :ok
  end

  describe "core functionality" do
    test "events emitted within buffer size are all delivered" do
      {:ok, _} = BufferDemand.start_pipeline(self(), buffer_size: 100, delay_ms: 1)

      for i <- 1..20, do: BufferDemand.PushProducer.emit(i)

      received =
        for _ <- 1..20 do
          assert_receive {:received, n}, 2_000
          n
        end

      assert Enum.sort(received) == Enum.to_list(1..20)
    end

    test "buffer_keep: :last drops oldest events on overflow" do
      # Tiny buffer + slow consumer — overflow is guaranteed.
      {:ok, _} =
        BufferDemand.start_pipeline(self(),
          buffer_size: 5,
          buffer_keep: :last,
          delay_ms: 50
        )

      # Emit 50 events fast. Consumer can hold max_demand (5) in flight plus
      # the buffer of 5 = 10 "safe"; the rest is subject to drop.
      for i <- 1..50, do: BufferDemand.PushProducer.emit(i)

      # Drain the mailbox with a generous timeout.
      received = drain_received([], 1_500)

      # With :last, the LATEST events win — the tail of 1..50 must be
      # present in what arrived.
      assert 50 in received
      # And we must have lost at least some early ones.
      assert length(received) < 50
    end

    test "buffer_keep: :first drops newest events on overflow" do
      {:ok, _} =
        BufferDemand.start_pipeline(self(),
          buffer_size: 5,
          buffer_keep: :first,
          delay_ms: 50
        )

      for i <- 1..50, do: BufferDemand.PushProducer.emit(i)

      received = drain_received([], 1_500)

      # With :first, the EARLIEST events win — 1 must survive.
      assert 1 in received
      assert length(received) < 50
    end

    defp drain_received(acc, timeout) do
      receive do
        {:received, n} -> drain_received([n | acc], timeout)
      after
        timeout -> Enum.reverse(acc)
      end
    end
  end
end
```

### Step 6: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule BufferDemand.PushProducer do
    @moduledoc """
    A push-style GenStage Producer. External callers send events via
    `emit/2`; the producer forwards immediately if demand exists, otherwise
    buffers up to `buffer_size`. Older events are dropped on overflow
    (`:buffer_keep, :last`) — common for telemetry use cases where newer
    is more valuable.
    """

    use GenStage

    @default_buffer_size 100

    def start_link(opts \\ []) do
      buffer_size = Keyword.get(opts, :buffer_size, @default_buffer_size)
      buffer_keep = Keyword.get(opts, :buffer_keep, :last)
      GenStage.start_link(__MODULE__, {buffer_size, buffer_keep}, name: __MODULE__)
    end

    @spec emit(GenServer.server(), any()) :: :ok
    def emit(producer \\ __MODULE__, event), do: GenStage.cast(producer, {:emit, event})

    @impl true
    def init({buffer_size, buffer_keep}) do
      {:producer, :no_state, buffer_size: buffer_size, buffer_keep: buffer_keep}
    end

    @impl true
    def handle_cast({:emit, event}, state) do
      # Returning [event] gives GenStage the event; it either dispatches it
      # immediately to satisfy pending demand or buffers it. We never touch
      # the buffer ourselves.
      {:noreply, [event], state}
    end

    @impl true
    def handle_demand(_demand, state) do
      # Pull-producer contract requires this callback. In a pure push
      # producer we have nothing to generate on demand — the buffer handles
      # it. Return no events; GenStage will record the demand and use it
      # when the next push arrives.
      {:noreply, [], state}
    end
  end

  def main do
    IO.puts("BufferDemand OK")
  end

end

Main.main()
```

## Trade-offs and production gotchas

**1. `buffer_size: :infinity` is a memory bomb**
Unbounded buffers don't have back-pressure by definition. If the producer
runs faster than the consumer for long enough, memory grows without
limit. Never set `:infinity` in a service that accepts external input.

**2. Silent drops are hard to debug**
When `buffer_size` is hit, events vanish. Emit a `:telemetry` event or
increment an ETS counter on every drop, and wire an alert. "Data
mysteriously missing" is a 2am page with no clues otherwise.

**3. Picking `:first` vs `:last` is a semantic decision**
- **`:last`** (keep newest): use for *current-state snapshots* —
  temperature readings, position updates, "latest known price". Stale
  data is strictly worse than fresh.
- **`:first`** (keep oldest): use for *replay-critical sequences* —
  audit logs, event-sourcing streams. Start of the sequence matters most.
- If both matter equally, you don't want a bounded buffer — you want a
  durable queue (Kafka, Redis Streams, RabbitMQ) in front of GenStage.

**4. Buffer is per-producer, not per-consumer**
If multiple consumers subscribe to one producer, they share the buffer.
A slow consumer slows all of them through back-pressure. For
fan-out where consumers are independent, use a `BroadcastDispatcher` or
one producer per consumer.

**5. Don't use the buffer for throughput smoothing**
If your producer emits 10k/s and consumer handles 1k/s sustained, no
buffer will save you — you'll just delay the inevitable overflow by
`buffer_size / rate_diff` seconds. Buffers handle *bursts*, not
*sustained overload*.

**6. When NOT to configure a buffer**
- Pull producers that already compute events in `handle_demand/2` —
  demand *is* the buffer. Setting one adds latency for no benefit.
- When loss is unacceptable → use durable storage upstream instead.
- When bursts are bigger than RAM can hold → the same.

## Resources

- [`GenStage` — hexdocs](https://hexdocs.pm/gen_stage/GenStage.html)
- [GenStage README — buffering events](https://github.com/elixir-lang/gen_stage#buffering-events)
- [`Broadway`](https://hexdocs.pm/broadway/Broadway.html) — built-in back-pressure, acknowledgements, and dead-letter handling
- [`:telemetry`](https://hexdocs.pm/telemetry/) — for instrumenting drop counters
- José Valim — [Elixir Forum thread on GenStage buffer semantics](https://elixirforum.com/t/gen-stage-buffer-options/) (several long-form discussions)

## Deep Dive

Streams are lazy, composable data pipelines that process one element at a time without materializing intermediate collections. This is fundamentally different from Enum, which materializes the entire dataset before the next operation.

**Lazy evaluation semantics:**
Stream operations return a `%Stream{}` struct containing a function. The actual computation is deferred until consumed by a terminal operation (`.run()`, `Enum.to_list()`, etc.). This allows streams to:
- Chain indefinite sequences (e.g., `Stream.iterate(0, &(&1 + 1))`)
- Transform without memory bloat (e.g., processing multi-gigabyte files)
- Compose reusable pipelines as first-class values

**Resource lifecycle in streams:**
Streams wrapping resources (`Stream.resource/3`) must define cleanup functions. A stream created from a file remains "open" (in terms of the lambda) until the consumer finishes or errors. If the consumer crashes or stops early, the cleanup function still runs — critical for proper file/socket/port management.

**Backpressure and demand:**
Unlike streams in other languages, Elixir's synchronous streams don't inherently implement backpressure. Backpressure is demand-based: the consumer pulls data at its own pace. `GenStage` and `Flow` add explicit backpressure — the producer waits for the consumer to request more elements. This is why benchmarking matters: a naive stream consumer can overwhelm memory if the pipeline produces faster than it consumes.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/buffer_demand_test.exs`

```elixir
defmodule BufferDemandTest do
  use ExUnit.Case, async: true

  doctest BufferDemand

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert BufferDemand.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
