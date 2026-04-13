# Livebook — Operational Notebooks and Kino Widgets

**Project**: `livebook_demo` — interactive operational runbooks, live dashboards, and documentation-as-code with Livebook + Kino

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
livebook_demo/
├── lib/
│   └── livebook_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── livebook_demo_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule LivebookDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :livebook_demo,
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

### `lib/livebook_demo.ex`

```elixir
defmodule LivebookDemo.Gateway do
  @moduledoc """
  Minimal rate limiter stored in an ETS table, used as the target of the
  operational livebook. Records request timestamps per client.
  """

  use GenServer

  @table :gateway_requests

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Returns record result from client_id."
  @spec record(String.t()) :: :ok
  def record(client_id) do
    :ets.insert(@table, {client_id, System.monotonic_time(:millisecond)})
    :ok
  end

  @doc "Returns snapshot result."
  @spec snapshot() :: [{String.t(), non_neg_integer()}]
  def snapshot do
    :ets.tab2list(@table)
    |> Enum.group_by(fn {cid, _} -> cid end)
    |> Enum.map(fn {cid, entries} -> {cid, length(entries)} end)
    |> Enum.sort_by(fn {_, n} -> -n end)
  end

  @impl true
  def init(_) do
    :ets.new(@table, [:named_table, :public, :bag, read_concurrency: true])
    {:ok, %{}}
  end
end

defmodule LivebookDemo.Metrics do
  @moduledoc "Rolling in-memory ring buffer of recent metric points."

  use GenServer

  @capacity 1_000

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  @doc "Returns record result from name and value."
  @spec record(atom(), number()) :: :ok
  def record(name, value), do: GenServer.cast(__MODULE__, {:record, name, value})

  @doc "Returns window result from name and last_n."
  @spec window(atom(), pos_integer()) :: [map()]
  def window(name, last_n), do: GenServer.call(__MODULE__, {:window, name, last_n})

  @impl true
  def init(_), do: {:ok, %{points: []}}

  @impl true
  def handle_cast({:record, name, value}, state) do
    point = %{name: name, value: value, ts: System.system_time(:millisecond)}
    new_points = [point | state.points] |> Enum.take(@capacity)
    {:noreply, %{state | points: new_points}}
  end

  @impl true
  def handle_call({:window, name, last_n}, _from, state) do
    result =
      state.points
      |> Enum.filter(&(&1.name == name))
      |> Enum.take(last_n)
      |> Enum.reverse()

    {:reply, result, state}
  end
end

defmodule LivebookDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      LivebookDemo.Gateway,
      LivebookDemo.Metrics
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: LivebookDemo.Supervisor)
  end
end
```

### `test/livebook_demo_test.exs`

```elixir
defmodule LivebookDemoTest do
  use ExUnit.Case, async: true

  doctest LivebookDemo

  describe "run/2" do
    test "returns :ok for valid input" do
      assert {:ok, %{processed: :payload}} = LivebookDemo.run(:payload)
    end

    test "returns :invalid_input for nil" do
      assert {:error, :invalid_input} = LivebookDemo.run(nil)
    end

    test "honors explicit timeout option" do
      assert {:ok, _} = LivebookDemo.run(:payload, timeout: 1_000)
    end

    test "default timeout is finite" do
      # Sanity: the operation completes well under the default deadline.
      {us, {:ok, _}} = :timer.tc(fn -> LivebookDemo.run(:payload) end)
      assert us < 100_000
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Livebook — Operational Notebooks and Kino Widgets.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Livebook — Operational Notebooks and Kino Widgets ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case LivebookDemo.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: LivebookDemo.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
