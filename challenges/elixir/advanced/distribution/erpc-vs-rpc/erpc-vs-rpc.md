# :erpc vs :rpc — Benchmarking the Modern Remote Call API

**Project**: `erpc_benchmark` — a head-to-head comparison you can run in CI

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
erpc_benchmark/
├── lib/
│   └── erpc_benchmark.ex
├── script/
│   └── main.exs
├── test/
│   └── erpc_benchmark_test.exs
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
defmodule ErpcBenchmark.MixProject do
  use Mix.Project

  def project do
    [
      app: :erpc_benchmark,
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
### `lib/erpc_benchmark.ex`

```elixir
defmodule ErpcBenchmark.Worker do
  @moduledoc """
  Functions called remotely from the benchmark harness. Keep them pure and
  fast so we measure RPC overhead, not the work itself.
  """

  @spec echo(term()) :: term()
  def echo(term), do: term

  @spec compute(pos_integer()) :: non_neg_integer()
  def compute(n) do
    Enum.reduce(1..n, 0, &+/2)
  end

  @spec sleep_then_return(non_neg_integer(), term()) :: term()
  def sleep_then_return(ms, value) do
    Process.sleep(ms)
    value
  end

  @spec raise_boom() :: no_return()
  def raise_boom, do: raise("boom")
end

defmodule ErpcBenchmark.RpcRunner do
  @moduledoc "Wraps :rpc calls with a uniform Elixir-y API."

  @spec call(node(), module(), atom(), list(), timeout()) ::
          {:ok, term()} | {:error, term()}
  def call(node, mod, fun, args, timeout \\ 5_000) do
    case :rpc.call(node, mod, fun, args, timeout) do
      {:badrpc, reason} -> {:error, reason}
      value -> {:ok, value}
    end
  end

  @spec multicall([node()], module(), atom(), list(), timeout()) ::
          %{ok: [{node(), term()}], bad: [node()]}
  def multicall(nodes, mod, fun, args, timeout \\ 5_000) do
    {replies, bad} = :rpc.multicall(nodes, mod, fun, args, timeout)

    good =
      nodes
      |> Enum.reject(&(&1 in bad))
      |> Enum.zip(replies)
      |> Enum.reject(fn {_, r} -> match?({:badrpc, _}, r) end)

    %{ok: good, bad: bad}
  end
end

defmodule ErpcBenchmark.ErpcRunner do
  @moduledoc """
  Wraps :erpc with Elixir-idiomatic return shapes.

  :erpc raises on failure — we capture into tagged tuples so callers can pattern
  match uniformly. The underlying exception type is preserved in `:reason`.
  """

  @spec call(node(), module(), atom(), list(), timeout()) ::
          {:ok, term()} | {:error, term()}
  def call(node, mod, fun, args, timeout \\ 5_000) do
    try do
      {:ok, :erpc.call(node, mod, fun, args, timeout)}
    rescue
      e in ErlangError -> {:error, e.original}
      e -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  @spec multicall([node()], module(), atom(), list(), timeout()) ::
          %{ok: [{node(), term()}], errors: [{node(), term()}]}
  def multicall(nodes, mod, fun, args, timeout \\ 5_000) do
    results = :erpc.multicall(nodes, mod, fun, args, timeout)

    Enum.zip(nodes, results)
    |> Enum.reduce(%{ok: [], errors: []}, fn
      {node, {:ok, val}}, acc -> %{acc | ok: [{node, val} | acc.ok]}
      {node, {:error, reason}}, acc -> %{acc | errors: [{node, reason} | acc.errors]}
      {node, {:throw, val}}, acc -> %{acc | errors: [{node, {:throw, val}} | acc.errors]}
      {node, {:exit, reason}}, acc -> %{acc | errors: [{node, {:exit, reason}} | acc.errors]}
    end)
  end

  @doc """
  Async request/response pair. Returns a request id usable with `await/2`.
  Equivalent of :rpc.async_call + yield, but without the :rex bottleneck.
  """
  @spec send_request(node(), module(), atom(), list()) :: :erpc.request_id()
  def send_request(node, mod, fun, args) do
    :erpc.send_request(node, mod, fun, args)
  end

  @spec await(:erpc.request_id(), timeout()) :: {:ok, term()} | {:error, term()}
  def await(request_id, timeout \\ 5_000) do
    try do
      {:ok, :erpc.receive_response(request_id, timeout)}
    rescue
      e in RuntimeError -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end
end

defmodule ErpcBenchmark.Harness do
  @moduledoc """
  Boots helper nodes with :peer for local benchmarking. Each helper node has
  the benchmark code paths loaded so we can remote-call into it.
  """

  @spec start_peer(atom()) :: {:ok, pid(), node()} | {:error, term()}
  def start_peer(name) do
    :net_kernel.start([:"bench@127.0.0.1"], %{name_domain: :longnames})
    case :peer.start_link(%{name: name, host: ~c"127.0.0.1", longnames: true}) do
      {:ok, peer, node} ->
        :rpc.call(node, :code, :add_paths, [:code.get_path()])
        :rpc.call(node, Application, :ensure_all_started, [:erpc_benchmark])
        {:ok, peer, node}

      err ->
        err
    end
  end

  @spec stop_peer(pid()) :: :ok
  def stop_peer(peer), do: :peer.stop(peer)
end
```
### `test/erpc_benchmark_test.exs`

```elixir
defmodule ErpcBenchmark.ErpcRunnerTest do
  use ExUnit.Case, async: true
  doctest ErpcBenchmark.Worker

  alias ErpcBenchmark.{ErpcRunner, Harness, Worker}

  setup_all do
    {:ok, peer, node} = Harness.start_peer(:t1)
    on_exit(fn -> Harness.stop_peer(peer) end)
    %{node: node}
  end

  describe "ErpcBenchmark.ErpcRunner" do
    test "call returns {:ok, value} on success", %{node: node} do
      assert {:ok, :hello} = ErpcRunner.call(node, Worker, :echo, [:hello])
    end

    test "call returns {:error, reason} on raise", %{node: node} do
      assert {:error, %RuntimeError{message: "boom"}} =
               ErpcRunner.call(node, Worker, :raise_boom, [])
    end

    test "call enforces timeout and does not hang", %{node: node} do
      started = System.monotonic_time(:millisecond)

      assert {:error, _reason} =
               ErpcRunner.call(node, Worker, :sleep_then_return, [500, :late], 50)

      elapsed = System.monotonic_time(:millisecond) - started
      # 50ms timeout + some slack for scheduling
      assert elapsed < 200
    end

    test "multicall returns per-node ok/error map", %{node: node} do
      result = ErpcRunner.multicall([node, node], Worker, :compute, [100])
      assert %{ok: oks, errors: []} = result
      assert length(oks) == 2
      assert Enum.all?(oks, fn {_, v} -> v == 5050 end)
    end

    test "send_request / await — async without :rex bottleneck", %{node: node} do
      id = ErpcRunner.send_request(node, Worker, :compute, [1000])
      assert {:ok, 500_500} = ErpcRunner.await(id, 1_000)
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for :erpc vs :rpc — Benchmarking the Modern Remote Call API.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== :erpc vs :rpc — Benchmarking the Modern Remote Call API ===")
    IO.puts("Category: Distribution and clustering\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ErpcBenchmark.run(payload) do
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
        for _ <- 1..1_000, do: ErpcBenchmark.run(:bench)
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
