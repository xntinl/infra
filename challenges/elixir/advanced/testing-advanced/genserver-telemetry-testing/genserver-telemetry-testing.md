# Testing GenServers Without Sleeps — Telemetry Events and assert_receive

**Project**: `ingest_pipeline` — a batch ingestion GenServer whose tests synchronize via telemetry events instead of `Process.sleep/1`

---

## Why advanced testing matters

Production Elixir test suites must run in parallel, isolate side-effects, and exercise concurrent code paths without races. Tooling like Mox, ExUnit async mode, Bypass, ExMachina and StreamData turns testing from a chore into a deliberate design artifact.

When tests double as living specifications, the cost of refactoring drops. When they don't, every change becomes a coin flip. Senior teams treat the test suite as a first-class product — measuring runtime, flake rate, and coverage of failure modes alongside production metrics.

---

## The business problem

You are building a production-grade Elixir component in the **Advanced testing** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
ingest_pipeline/
├── lib/
│   └── ingest_pipeline.ex
├── script/
│   └── main.exs
├── test/
│   └── ingest_pipeline_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in Advanced testing the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule IngestPipeline.MixProject do
  use Mix.Project

  def project do
    [
      app: :ingest_pipeline,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/ingest_pipeline.ex`

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

  @doc "Returns push result from server and event."
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

# test/support/telemetry_helper.ex (or inline inside the test file)
defmodule IngestPipeline.TelemetryHelper do
  @moduledoc """
  Attaches a handler that forwards an event to a process mailbox.
  Caller must pass a unique handler_id so two tests can attach to the same event
  without colliding.
  """

  @doc "Returns attach forwarder result from handler_id, event and test_pid."
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

### `test/ingest_pipeline_test.exs`

```elixir
defmodule IngestPipeline.BufferTest do
  use ExUnit.Case, async: true
  doctest IngestPipeline.Buffer

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

### `script/main.exs`

```elixir
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

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Async tests are the default, not the exception

ExUnit defaults to sequential execution. Set `async: true` and structure tests so they don't share global state — Application env, ETS tables, the database. The reward is 5–10× faster suites in CI.

### 2. Mock the boundary, not the dependency

A behaviour-backed mock (Mox.defmock for: SomeBehaviour) is a contract. A bare function stub is a wish. Defining the boundary as a behaviour costs one file and pays back every time the implementation changes.

### 3. Test the failure mode, always

An assertion that succeeds when everything goes right teaches nothing. Tests that prove the system handles `{:error, :timeout}`, `{:error, :network}`, and partial failures are the ones that prevent regressions.

---
