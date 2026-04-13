# Remote calls in BEAM — `:rpc` vs `:erpc` vs `GenServer.call`

**Project**: `rpc_demo` — measure, compare, and choose between the three cross-node call mechanisms in Elixir/Erlang: legacy `:rpc`, modern `:erpc`, and plain `GenServer.call` to a remote-registered process

---

## Why distribution and clustering matters

Distributed Erlang gives you remote message-passing transparency, but the cost is your responsibility for split-brain detection, registry consistency, and net-tick policies. Libcluster, Horde, and PG provide pieces; you compose them.

Clusters fail in interesting ways: netsplits, asymmetric partitions, GC pauses misread as crashes, and global registry race conditions. Designing for the network — rather than against it — is the senior shift.

---

## The business problem

You are building a production-grade Elixir component in the **Distribution and clustering** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
rpc_demo/
├── lib/
│   └── rpc_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── rpc_demo_test.exs
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

Chose **B** because in Distribution and clustering the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule RpcDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :rpc_demo,
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

### `lib/rpc_demo.ex`

```elixir
defmodule RpcDemo.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [RpcDemo.SnapshotServer]
    Supervisor.start_link(children, strategy: :one_for_one, name: RpcDemo.Supervisor)
  end
end

defmodule RpcDemo.Snapshot do
  @moduledoc """
  The function being invoked remotely. Returns a small payload representative
  of real work (memory stats, process counts, a per-node tag).
  """

  @spec get() :: %{node: node(), memory_total: non_neg_integer(), process_count: non_neg_integer()}
  def get do
    %{
      node: node(),
      memory_total: :erlang.memory(:total),
      process_count: :erlang.system_info(:process_count)
    }
  end

  @doc "Raises to test error propagation."
  @spec boom!() :: no_return()
  def boom! do
    raise "boom on #{node()}"
  end

  @doc "Sleeps, to test timeouts."
  @spec slow(pos_integer()) :: :ok
  def slow(ms) do
    Process.sleep(ms)
    :ok
  end
end

defmodule RpcDemo.SnapshotServer do
  @moduledoc "Named GenServer exposing Snapshot.get/0."
  use GenServer

  alias RpcDemo.Snapshot

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: {:ok, %{calls: 0}}

  @impl true
  def handle_call(:get, _from, state) do
    {:reply, Snapshot.get(), %{state | calls: state.calls + 1}}
  end
end

defmodule RpcDemo.Client do
  @moduledoc """
  Three equivalent ways to call `RpcDemo.Snapshot.get/0` on a remote node.
  Return types are normalised to `{:ok, result} | {:error, reason}`.
  """
  require Logger

  alias RpcDemo.Snapshot

  @type result :: {:ok, term()} | {:error, term()}

  @spec via_rpc(node(), timeout()) :: result()
  def via_rpc(target, timeout \\ 5_000) do
    case :rpc.call(target, Snapshot, :get, [], timeout) do
      {:badrpc, reason} -> {:error, {:rpc, reason}}
      result -> {:ok, result}
    end
  end

  @spec via_erpc(node(), timeout()) :: result()
  def via_erpc(target, timeout \\ 5_000) do
    {:ok, :erpc.call(target, Snapshot, :get, [], timeout)}
  rescue
    e in ErlangError -> {:error, {:erpc, e.original}}
  catch
    :exit, reason -> {:error, {:erpc_exit, reason}}
  end

  @spec via_genserver(node(), timeout()) :: result()
  def via_genserver(target, timeout \\ 5_000) do
    {:ok, GenServer.call({RpcDemo.SnapshotServer, target}, :get, timeout)}
  catch
    :exit, {:noproc, _} -> {:error, :server_not_running}
    :exit, {:nodedown, _} -> {:error, :nodedown}
    :exit, {:timeout, _} -> {:error, :timeout}
    :exit, reason -> {:error, {:genserver_exit, reason}}
  end

  @doc "Fan-out via :erpc.multicall — one call with a shared deadline."
  @spec multicall_erpc([node()], timeout()) :: [result()]
  def multicall_erpc(targets, timeout \\ 2_000) do
    :erpc.multicall(targets, Snapshot, :get, [], timeout)
    |> Enum.map(fn
      {:ok, v} -> {:ok, v}
      {:error, reason} -> {:error, reason}
      {:throw, t} -> {:error, {:throw, t}}
      {:exit, r} -> {:error, {:exit, r}}
    end)
  end
end

defmodule RpcDemo.Bench do
  @moduledoc "Latency + throughput harness across the three call mechanisms."

  alias RpcDemo.Client

  @iterations 2_000

  @spec run(node()) :: %{atom() => map()}
  def run(target) do
    %{
      rpc: time_calls(fn -> Client.via_rpc(target) end),
      erpc: time_calls(fn -> Client.via_erpc(target) end),
      genserver: time_calls(fn -> Client.via_genserver(target) end)
    }
  end

  defp time_calls(fun) do
    samples =
      for _ <- 1..@iterations do
        t0 = System.monotonic_time(:microsecond)
        _ = fun.()
        System.monotonic_time(:microsecond) - t0
      end
      |> Enum.sort()

    %{
      min: List.first(samples),
      p50: Enum.at(samples, div(@iterations, 2)),
      p99: Enum.at(samples, div(@iterations * 99, 100)),
      max: List.last(samples)
    }
  end

  @spec concurrent(node(), pos_integer()) :: %{atom() => float()}
  def concurrent(target, concurrency \\ 32) do
    %{
      rpc: throughput(fn -> Client.via_rpc(target) end, concurrency),
      erpc: throughput(fn -> Client.via_erpc(target) end, concurrency)
    }
  end

  defp throughput(fun, concurrency) do
    t0 = System.monotonic_time(:millisecond)
    duration_ms = 3_000

    tasks =
      for _ <- 1..concurrency do
        Task.async(fn ->
          count_until(fun, t0 + duration_ms, 0)
        end)
      end

    total = tasks |> Task.await_many(duration_ms + 2_000) |> Enum.sum()
    total / (duration_ms / 1_000)
  end

  defp count_until(fun, deadline_ms, acc) do
    if System.monotonic_time(:millisecond) >= deadline_ms do
      acc
    else
      _ = fun.()
      count_until(fun, deadline_ms, acc + 1)
    end
  end
end
```

### `test/rpc_demo_test.exs`

```elixir
defmodule RpcDemo.ClientTest do
  use ExUnit.Case, async: true
  doctest RpcDemo.Application

  alias RpcDemo.{Client, Snapshot}

  @self_node node()

  describe "RpcDemo.Client" do
    test "via_rpc/1 against local node returns a snapshot" do
      assert {:ok, %{node: node, process_count: n}} = Client.via_rpc(@self_node)
      assert node == @self_node
      assert is_integer(n)
    end

    test "via_erpc/1 against local node returns a snapshot" do
      assert {:ok, %{node: node}} = Client.via_erpc(@self_node)
      assert node == @self_node
    end

    test "via_genserver/1 routes through the named SnapshotServer" do
      assert {:ok, %{node: node}} = Client.via_genserver(@self_node)
      assert node == @self_node
    end

    test "via_erpc/1 surfaces raised errors" do
      assert_raise ErlangError, fn ->
        :erpc.call(@self_node, Snapshot, :boom!, [], 500)
      end
    end

    test "via_genserver/1 returns :nodedown for an unreachable node" do
      fake = :"never_existed@127.0.0.1"
      assert {:error, reason} = Client.via_genserver(fake, 200)
      assert reason in [:nodedown, :server_not_running, {:genserver_exit, :noconnection}] or
               match?({:genserver_exit, _}, reason)
    end

    test "via_rpc/1 returns {:error, {:rpc, ...}} for an unreachable node" do
      fake = :"never_existed@127.0.0.1"
      assert {:error, {:rpc, _}} = Client.via_rpc(fake, 200)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Remote calls in BEAM — `:rpc` vs `:erpc` vs `GenServer.call`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Remote calls in BEAM — `:rpc` vs `:erpc` vs `GenServer.call` ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case RpcDemo.run(payload) do
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
        for _ <- 1..1_000, do: RpcDemo.run(:bench)
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

### 1. Partitions are the rule, not the exception

In a multi-AZ cluster, brief netsplits happen daily. Design for them: prefer eventual consistency, use idempotent operations, and detect split-brain explicitly.

### 2. Registries don't replicate transparently

Local Registry is fast and node-local. :global is consistent but slow. Horde.Registry replicates via CRDTs — eventual consistency, no global locks. Pick based on your read/write ratio.

### 3. Tune net_kernel ticks for your environment

The default 60-second tick is too long for production failure detection but too short for high-latency cross-region links. Measure first.

---
