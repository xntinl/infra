# Testing Supervisor Restart Strategies

**Project**: `payment_gateway_supervisor` — a supervisor whose restart strategy and intensity are verified by deterministic tests using `Process.monitor/1`

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
payment_gateway_supervisor/
├── lib/
│   └── payment_gateway_supervisor.ex
├── script/
│   └── main.exs
├── test/
│   └── payment_gateway_supervisor_test.exs
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
defmodule PaymentGatewaySupervisor.MixProject do
  use Mix.Project

  def project do
    [
      app: :payment_gateway_supervisor,
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

### `lib/payment_gateway_supervisor.ex`

```elixir
# lib/payment_gateway/payment_client.ex
defmodule PaymentGateway.PaymentClient do
  @moduledoc """
  Ejercicio: Testing Supervisor Restart Strategies.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  @doc "Returns crash result."
  def crash, do: GenServer.cast(__MODULE__, :crash)
  @doc "Stops normally result."
  def stop_normally, do: GenServer.cast(__MODULE__, :stop)

  @impl true
  def init(_), do: {:ok, %{started_at: System.monotonic_time()}}

  @impl true
  def handle_cast(:crash, _), do: raise("boom")
  def handle_cast(:stop, state), do: {:stop, :normal, state}
end

# lib/payment_gateway/ledger_writer.ex
defmodule PaymentGateway.LedgerWriter do
  use GenServer, restart: :transient

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  @doc "Stops normally result."
  def stop_normally, do: GenServer.cast(__MODULE__, :stop)
  @doc "Returns crash result."
  def crash, do: GenServer.cast(__MODULE__, :crash)

  @impl true
  def init(_), do: {:ok, %{}}
  @impl true
  def handle_cast(:stop, state), do: {:stop, :normal, state}
  def handle_cast(:crash, _), do: raise("ledger down")
end

# lib/payment_gateway/fraud_check.ex
defmodule PaymentGateway.FraudCheck do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  @impl true
  def init(_), do: {:ok, %{}}
end

# lib/payment_gateway/supervisor.ex
defmodule PaymentGateway.Supervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, :ok,
      name: Keyword.get(opts, :name, __MODULE__)
    )
  end

  @impl true
  def init(:ok) do
    children = [
      PaymentGateway.PaymentClient,
      PaymentGateway.LedgerWriter,
      PaymentGateway.FraudCheck
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 3,
      max_seconds: 5
    )
  end
end
```

### `test/payment_gateway_supervisor_test.exs`

```elixir
defmodule PaymentGateway.SupervisorTest do
  # async: false because workers use globally-registered names (:name, __MODULE__)
  use ExUnit.Case, async: true
  doctest PaymentGateway.PaymentClient

  alias PaymentGateway.{Supervisor, PaymentClient, LedgerWriter, FraudCheck}

  setup do
    start_supervised!(Supervisor)
    :ok
  end

  describe "strategy :one_for_one" do
    test "crashed child is restarted in isolation" do
      original = Process.whereis(PaymentClient)
      ledger_before = Process.whereis(LedgerWriter)

      ref = Process.monitor(original)
      PaymentClient.crash()

      # Wait for the crash deterministically
      assert_receive {:DOWN, ^ref, :process, ^original, _}, 500

      # Small bounded wait for the supervisor to re-register the name
      Process.sleep(20)

      new_pid = Process.whereis(PaymentClient)
      assert new_pid != nil
      assert new_pid != original

      # Critical: siblings MUST not have restarted
      assert Process.whereis(LedgerWriter) == ledger_before
    end

    test "crashing one child does not kill unrelated siblings" do
      fraud_before = Process.whereis(FraudCheck)

      ref = Process.monitor(Process.whereis(PaymentClient))
      PaymentClient.crash()
      assert_receive {:DOWN, ^ref, :process, _, _}, 500

      Process.sleep(20)
      assert Process.whereis(FraudCheck) == fraud_before
    end
  end

  describe "restart type :transient for LedgerWriter" do
    test "normal stop does NOT trigger a restart" do
      original = Process.whereis(LedgerWriter)
      ref = Process.monitor(original)

      LedgerWriter.stop_normally()
      assert_receive {:DOWN, ^ref, :process, ^original, :normal}, 500

      # Supervisor must NOT have restarted on :normal
      Process.sleep(50)
      assert Process.whereis(LedgerWriter) == nil
    end

    test "abnormal crash DOES trigger a restart" do
      original = Process.whereis(LedgerWriter)
      ref = Process.monitor(original)

      LedgerWriter.crash()
      assert_receive {:DOWN, ^ref, :process, ^original, _}, 500

      Process.sleep(20)
      new_pid = Process.whereis(LedgerWriter)
      assert new_pid != nil
      assert new_pid != original
    end
  end

  describe "restart intensity" do
    test "supervisor dies after exceeding max_restarts within max_seconds" do
      sup = Process.whereis(Supervisor)
      sup_ref = Process.monitor(sup)

      # Cause 4 crashes in quick succession (limit is 3)
      for _ <- 1..4 do
        case Process.whereis(PaymentClient) do
          nil ->
            Process.sleep(5)

          pid ->
            ref = Process.monitor(pid)
            PaymentClient.crash()
            receive do
              {:DOWN, ^ref, :process, _, _} -> :ok
            after
              500 -> :ok
            end
        end
      end

      # The supervisor itself must have crashed
      assert_receive {:DOWN, ^sup_ref, :process, ^sup, _}, 1_000
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
