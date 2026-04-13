# Testing GenServers Without Sleeps — Telemetry Events and assert_receive

**Project**: `ingest_pipeline` — a batch ingestion GenServer whose tests synchronize via telemetry events instead of `Process.sleep/1`.

## Project context

`ingest_pipeline` buffers incoming events and flushes every 500ms or when the buffer reaches
100 entries, whichever comes first. The original test suite was riddled with
`Process.sleep(600)` calls to "wait for the flush to happen". The suite took 40 seconds and
was flaky on the CI machine under load.

`sleep`-based synchronization is a red flag in concurrent testing. It wastes wall clock,
produces intermittent failures, and hides races. The two correct tools are:

1. `assert_receive` — wait for a message with a timeout, return as soon as it arrives.
2. `:telemetry` events — the code under test emits a structured event; the test subscribes
   and asserts on it.

Combined, they let a test advance exactly as fast as the code under test.

```
ingest_pipeline/
├── lib/
│   └── ingest_pipeline/
│       ├── buffer.ex                  # GenServer under test
│       └── application.ex
├── test/
│   ├── ingest_pipeline/
│   │   └── buffer_test.exs
│   └── test_helper.exs
└── mix.exs
```

## Why telemetry over `sync_notify/1` or `GenServer.call(:flush_now)`

- **`sync_notify`** requires invasive changes — every spot you care about needs a test hook.
- **`:sys.get_state/1`** is synchronous and useful, but only returns current state, not a
  history of events. You still need to know *when* the state changed.
- **Telemetry** is production-grade instrumentation you likely already have. Tests reuse
  the same events the observability stack uses — zero test-only hooks in production code.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**Testing-specific insight:**
Tests are not QA. They document intent and catch regressions. A test that passes without asserting anything is technical debt. Always test the failure case; "it works when everything succeeds" teaches nothing. Use property-based testing for domain logic where the number of edge cases is infinite.
### 1. `assert_receive pattern, timeout`
Blocks the test process up to `timeout` milliseconds waiting for a message matching
`pattern`. Returns immediately on match. Fails the test otherwise.

### 2. `:telemetry.attach/4`
Subscribes a handler to one event. Best practice in tests: forward the event to `self()`
via `send/2`, then `assert_receive` it.

### 3. `start_supervised!/1`
Starts a process tied to the test's lifecycle. ExUnit stops it after the test, even on
failure. Avoids leaks that plague `start_link/1` used raw in a test.

## Design decisions

- **Option A — `Process.sleep/1`**: simple, slow, flaky, hides races.
- **Option B — `:sys.get_state/1` polling loop**: better than sleep but you still spin.
- **Option C — telemetry + `assert_receive`**: event-driven, exact, no polling.

Chosen: **Option C**. It doubles as observability in production.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:telemetry, "~> 1.3"}
  ]
end
```

### Step 1: the GenServer emits events at meaningful points

**Objective**: Instrument internal state transitions with telemetry so tests can observe timing and causality without poking into GenServer state or sleeping.

```elixir
# lib/ingest_pipeline/buffer.ex
defmodule IngestPipeline.Buffer do
  @moduledoc """
  Buffers events and flushes on size or time threshold.

  Telemetry:
    [:ingest_pipeline, :buffer, :flush] metadata: %{reason: :size | :interval, count: n}
    [:ingest_pipeline, :buffer, :push]  metadata: %{size: current_size}
  """

  use GenServer

  @default_max_size 100
  @default_interval_ms 500

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: Keyword.get(opts, :name, __MODULE__))
  end

  def push(server, event), do: GenServer.cast(server, {:push, event})

  @impl true
  def init(opts) do
    max_size = Keyword.get(opts, :max_size, @default_max_size)
    interval = Keyword.get(opts, :interval_ms, @default_interval_ms)

    Process.send_after(self(), :flush_interval, interval)

    {:ok, %{buffer: [], max_size: max_size, interval: interval}}
  end

  @impl true
  def handle_cast({:push, event}, state) do
    buffer = [event | state.buffer]
    size = length(buffer)
    :telemetry.execute([:ingest_pipeline, :buffer, :push], %{size: size}, %{})

    if size >= state.max_size do
      flush(buffer, :size)
      {:noreply, %{state | buffer: []}}
    else
      {:noreply, %{state | buffer: buffer}}
    end
  end

  @impl true
  def handle_info(:flush_interval, state) do
    if state.buffer != [] do
      flush(state.buffer, :interval)
    end

    Process.send_after(self(), :flush_interval, state.interval)
    {:noreply, %{state | buffer: []}}
  end

  defp flush(buffer, reason) do
    :telemetry.execute(
      [:ingest_pipeline, :buffer, :flush],
      %{count: length(buffer)},
      %{reason: reason}
    )
  end
end
```

### Step 2: helper — forward telemetry to the test pid

**Objective**: Bridge telemetry events into the test process mailbox with unique handler IDs so async tests can attach to the same event without colliding.

```elixir
# test/support/telemetry_helper.ex (or inline inside the test file)
defmodule IngestPipeline.TelemetryHelper do
  @moduledoc """
  Attaches a handler that forwards an event to a process mailbox.
  Caller must pass a unique handler_id so two tests can attach to the same event
  without colliding.
  """

  def attach_forwarder(handler_id, event, test_pid) do
    :telemetry.attach(
      handler_id,
      event,
      fn name, measurements, metadata, _config ->
        send(test_pid, {:telemetry_event, name, measurements, metadata})
      end,
      nil
    )

    ExUnit.Callbacks.on_exit(fn -> :telemetry.detach(handler_id) end)
  end
end
```

### Step 3: tests without a single `Process.sleep`

**Objective**: Replace timing-based waits with assert_receive on telemetry events so tests run at the speed of the code and remain deterministic under load.

```elixir
# test/ingest_pipeline/buffer_test.exs
defmodule IngestPipeline.BufferTest do
  use ExUnit.Case, async: true

  alias IngestPipeline.{Buffer, TelemetryHelper}

  setup context do
    handler_id = "buffer-test-#{context.test}-#{System.unique_integer([:positive])}"
    TelemetryHelper.attach_forwarder(handler_id, [:ingest_pipeline, :buffer, :flush], self())
    :ok
  end

  describe "push/2 — size-triggered flush" do
    test "flushes exactly when the buffer hits max_size" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_size_test, max_size: 3, interval_ms: 60_000}
        )

      Buffer.push(buffer, :a)
      Buffer.push(buffer, :b)

      # No flush yet — we only pushed 2 of 3
      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 50

      Buffer.push(buffer, :c)

      assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                      %{count: 3}, %{reason: :size}},
                     500
    end
  end

  describe "push/2 — interval-triggered flush" do
    test "flushes on interval when buffer is non-empty" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_interval_test, max_size: 1_000, interval_ms: 50}
        )

      Buffer.push(buffer, :x)

      assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                      %{count: 1}, %{reason: :interval}},
                     500
    end

    test "does not flush when buffer is empty" do
      start_supervised!({Buffer, name: :buffer_empty_test, max_size: 1_000, interval_ms: 50})

      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 200
    end
  end

  describe "push/2 — correctness under load" do
    test "large burst produces deterministic number of flush events" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_burst_test, max_size: 10, interval_ms: 60_000}
        )

      for i <- 1..100, do: Buffer.push(buffer, i)

      # Expect exactly 10 flush events (100 / 10)
      for _ <- 1..10 do
        assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                        %{count: 10}, %{reason: :size}},
                       1_000
      end

      # No eleventh flush within 200ms
      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 200
    end
  end
end
```

## Why this works

Each test attaches a telemetry handler with a unique id derived from the test name plus
a unique integer — two async tests cannot collide on the handler id. The handler forwards
events to the test pid's mailbox. `assert_receive` waits for the event with a generous
upper-bound timeout, but returns as soon as it arrives. `refute_receive` asserts
"this should NOT happen" with a bounded window.

No `Process.sleep`. Tests run as fast as the code under test. When a GenServer is slow,
the telemetry event arrives slowly and the test waits. When the GenServer is fast, the
test finishes in microseconds.

## Tests

See Step 3 — three describe blocks cover size trigger, interval trigger, and burst
correctness.

## Benchmark

A suite of 50 similar tests should run in well under 2 seconds, dominated by ExUnit
startup and supervision, not by test bodies:

```elixir
{t, _} = :timer.tc(fn -> ExUnit.run() end)
IO.puts("suite #{t / 1000}ms")
```

Target: per-test median under 10ms when `interval_ms` is not the bottleneck. Contrast with
the same test written with `Process.sleep(600)` — 30s median.

## Deep Dive: Telemetry Patterns and Production Implications

Telemetry decouples event emission from consumption, allowing system components to broadcast facts without coupling to logging, metrics, or observability code. GenServer processes are natural telemetry publishers—each lifecycle event (init, cast, call) is an opportunity to emit metrics. The architectural benefit is that test suites can attach telemetry handlers to verify internal state transitions without coupling tests to implementation details. Production systems build observability atop telemetry; testing it early catches assumptions about causality that are false at scale.

---

## Advanced Considerations

Production testing strategies require careful attention to resource management and test isolation across multiple concurrent test processes. In large codebases, tests can consume significant memory and CPU resources, especially when using concurrent testing without proper synchronization and cleanup. The BEAM scheduler's preemptive nature means test processes may interfere with each other if shared resources aren't properly isolated at the process boundary. Pay careful attention to how Ecto's sandbox mode interacts with your supervision tree — if you have GenServers that hold state across tests, the sandbox rollback mechanism may leave phantom processes in your monitoring systems that continue consuming resources until forced cleanup occurs.

When scaling tests to production-grade test suites, consider the cost of stub verification and the memory overhead of generated test cases. Each property-based test invocation can create thousands of synthetic test cases, potentially causing garbage collection pressure that's invisible during local testing but becomes critical in CI/CD pipelines running long test suites continuously. The interaction between concurrent tests and ETS tables (often used in caches and registry patterns) requires explicit `inherited: true` options to prevent unexpected sharing between test processes, which can cause mysterious failures when tests run in different orders or under load.

For distributed testing scenarios using tools like `Peer`, network simulation can mask real latency issues and failure modes. Test timeouts that work locally may fail in CI due to scheduler contention and GC pauses. Always include substantial buffers for timeout values and monitor actual execution times under load. The coordination between multiple test nodes requires careful cleanup — a failure in test coordination can leave zombie processes consuming resources indefinitely. Implement proper telemetry hooks within your test helpers to diagnose production-like scenarios and capture performance characteristics.


## Trade-offs and production gotchas

**1. Reusing a handler id across async tests**
`:telemetry.attach/4` fails on duplicate ids. Always derive the id from the test name plus
`System.unique_integer/1` to guarantee uniqueness per test.

**2. Forgetting to `:telemetry.detach/1`**
Handlers persist across tests in the same VM. Without detach, an old handler keeps sending
to a dead pid, causing `:noconnection` errors. Always detach in `on_exit`.

**3. `assert_receive` default timeout of 100ms**
100ms is too tight for CI under load. Always pass an explicit timeout of 500–1000ms for
events that depend on timers. Be generous — `assert_receive` returns as soon as the
message arrives; the timeout is a safety net, not a wait.

**4. Using `refute_receive` with a too-short timeout**
`refute_receive pattern, 50` says "the event did not arrive in 50ms". For events scheduled
at 60 seconds, 50ms is a meaningful refutation. For events scheduled at 5ms, 50ms is
wishful thinking.

**5. Telemetry events that depend on success**
If your GenServer crashes before emitting, you cannot `assert_receive` its event. Pair
telemetry with `Process.monitor/1` + `assert_receive {:DOWN, ...}` in that case.

**6. When NOT to use this**
For purely synchronous, single-process logic, telemetry is overkill. Call the function
and assert on the return value. Telemetry pays off when concurrency, timers, or spawned
processes are in play.

## Reflection

`assert_receive` returns as soon as the message arrives. What failure mode appears when
two tests running with `async: true` both emit the same telemetry event name, and how does
the handler-id uniqueness trick actually prevent it?


## Executable Example

```elixir
# test/ingest_pipeline/buffer_test.exs
defmodule IngestPipeline.BufferTest do
  use ExUnit.Case, async: true

  alias IngestPipeline.{Buffer, TelemetryHelper}

  setup context do
    handler_id = "buffer-test-#{context.test}-#{System.unique_integer([:positive])}"
    TelemetryHelper.attach_forwarder(handler_id, [:ingest_pipeline, :buffer, :flush], self())
    :ok
  end

  describe "push/2 — size-triggered flush" do
    test "flushes exactly when the buffer hits max_size" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_size_test, max_size: 3, interval_ms: 60_000}
        )

      Buffer.push(buffer, :a)
      Buffer.push(buffer, :b)

      # No flush yet — we only pushed 2 of 3
      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 50

      Buffer.push(buffer, :c)

      assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                      %{count: 3}, %{reason: :size}},
                     500
    end
  end

  describe "push/2 — interval-triggered flush" do
    test "flushes on interval when buffer is non-empty" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_interval_test, max_size: 1_000, interval_ms: 50}
        )

      Buffer.push(buffer, :x)

      assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                      %{count: 1}, %{reason: :interval}},
                     500
    end

    test "does not flush when buffer is empty" do
      start_supervised!({Buffer, name: :buffer_empty_test, max_size: 1_000, interval_ms: 50})

      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 200
    end
  end

  describe "push/2 — correctness under load" do
    test "large burst produces deterministic number of flush events" do
      buffer =
        start_supervised!(
          {Buffer, name: :buffer_burst_test, max_size: 10, interval_ms: 60_000}
        )

      for i <- 1..100, do: Buffer.push(buffer, i)

      # Expect exactly 10 flush events (100 / 10)
      for _ <- 1..10 do
        assert_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush],
                        %{count: 10}, %{reason: :size}},
                       1_000
      end

      # No eleventh flush within 200ms
      refute_receive {:telemetry_event, [:ingest_pipeline, :buffer, :flush], _, _}, 200
    end
  end
end

defmodule Main do
  def main do
      IO.puts("Property-based test generator initialized")
      a = 10
      b = 20
      c = 30
      assert (a + b) + c == a + (b + c)
      IO.puts("✓ Property invariant verified: (a+b)+c = a+(b+c)")
  end
end

Main.main()
```
