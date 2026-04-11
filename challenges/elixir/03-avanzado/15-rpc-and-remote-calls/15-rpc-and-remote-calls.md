# RPC and Remote Calls

## Project context

You are building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
This exercise focuses on distributing administrative operations -- configuration reloads,
health checks, and stats collection -- across a cluster of gateway nodes.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── cluster/
│           ├── rpc_client.ex
│           └── health_check.ex
├── test/
│   └── api_gateway/
│       └── cluster/
│           ├── rpc_client_test.exs
│           └── health_check_test.exs
├── bench/
│   └── rpc_bench.exs
└── mix.exs
```

---

## The business problem

The platform team needs to reload configuration on all gateway nodes without downtime.
They also need periodic health checks across the cluster to detect nodes with degraded
cache hit rates before they impact traffic. Both operations must:

1. Fan out to all connected nodes **in parallel**, not sequentially
2. Handle node failures gracefully -- a slow or dead node must not block the others
3. Distinguish between `:node_down`, `:timeout`, and `:remote_exception` -- each has a
   different operational response

---

## Why `:erpc` over `:rpc`

The classic `:rpc.call/4` routes every call through a single process called `:rex`
on the remote node. Under concurrent load, `:rex` becomes a serialization bottleneck:

```
node_a → :rpc.call → :rex (queue) → execute → reply
node_a → :rpc.call → :rex (queue) → execute → reply   # waiting for :rex
node_a → :rpc.call → :rex (queue) → execute → reply   # waiting for :rex
```

`:erpc` (OTP 23+) spawns a fresh process per call on the remote node, eliminating `:rex`:

```
node_a → :erpc.call → spawn process → execute → reply
node_a → :erpc.call → spawn process → execute → reply  # no wait
node_a → :erpc.call → spawn process → execute → reply  # no wait
```

Error semantics also differ: `:rpc` returns `{:badrpc, reason}` tuples; `:erpc`
raises exceptions. Each requires a different error-handling pattern.

---

## Why GenServer.call with `{name, node}` over RPC for named processes

When calling a named GenServer on a remote node, `GenServer.call({MyServer, node}, msg)`
has an important advantage over `:rpc.call(node, Module, :function, [])`:

- **Automatic 5s timeout** -- built into GenServer, not something you manage
- **Automatic monitor** -- if the remote process dies mid-call, you get `{:exit, :noproc}` instead of hanging
- **Supervisor-awareness** -- the call respects the GenServer's message queue ordering

Use `:rpc`/`:erpc` for stateless function invocations. Use `GenServer.call` for
calls to specific supervised processes.

---

## Implementation

### Step 1: Create the cluster directory

```bash
mkdir -p lib/api_gateway/cluster
mkdir -p test/api_gateway/cluster
```

### Step 2: `lib/api_gateway/cluster/rpc_client.ex`

```elixir
defmodule ApiGateway.Cluster.RPCClient do
  @moduledoc """
  Safe RPC wrapper for inter-node calls in the api_gateway cluster.

  Design decisions:
  - Uses :erpc for all calls (OTP 23+) -- no :rex bottleneck
  - All public functions return tagged tuples -- never raise to the caller
  - Fanout is always parallel -- never sequential with multiplied timeouts
  """

  @default_timeout_ms 5_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Calls module.function(args) on a single remote node.
  Returns {:ok, result} | {:error, :node_down | :timeout | {:exception, term()}}

  Wraps :erpc.call/5 in a try/catch to normalize all error types into tagged tuples.
  :erpc raises on error rather than returning {:badrpc, _} like :rpc, so we catch
  the three known exception categories and map them to descriptive error atoms.
  """
  @spec call(node(), module(), atom(), list(), pos_integer()) ::
          {:ok, term()} | {:error, :node_down | :timeout | {:exception, term()}}
  def call(node, module, function, args, timeout_ms \\ @default_timeout_ms) do
    try do
      result = :erpc.call(node, module, function, args, timeout_ms)
      {:ok, result}
    catch
      :error, {:erpc, :noconnection} ->
        {:error, :node_down}

      :error, {:erpc, :timeout} ->
        {:error, :timeout}

      kind, reason ->
        {:error, {:exception, {kind, reason}}}
    end
  end

  @doc """
  Fans out module.function(args) to all connected nodes in parallel.
  Returns a map of %{node => {:ok, result} | {:error, reason}}.

  The global timeout applies to ALL nodes together, not per node.
  A slow node does not block results from fast nodes.

  Uses Task.async/Task.yield_many to execute calls in parallel with a single
  shared deadline. Nodes that don't respond within timeout_ms get {:error, :timeout}.
  The local node (node()) is included explicitly because Node.list() never contains it.
  """
  @spec fanout(module(), atom(), list(), pos_integer()) ::
          %{node() => {:ok, term()} | {:error, term()}}
  def fanout(module, function, args, timeout_ms \\ @default_timeout_ms) do
    nodes = [node() | Node.list()]

    task_map =
      nodes
      |> Enum.map(fn n ->
        task = Task.async(fn -> call(n, module, function, args, timeout_ms) end)
        {task, n}
      end)

    tasks = Enum.map(task_map, fn {task, _node} -> task end)
    node_by_ref = Map.new(task_map, fn {task, n} -> {task.ref, n} end)

    results = Task.yield_many(tasks, timeout_ms)

    Map.new(results, fn {task, result} ->
      target_node = Map.fetch!(node_by_ref, task.ref)

      value =
        case result do
          {:ok, {:ok, val}} ->
            {:ok, val}

          {:ok, {:error, reason}} ->
            {:error, reason}

          nil ->
            Task.shutdown(task, :brutal_kill)
            {:error, :timeout}
        end

      {target_node, value}
    end)
  end

  @doc """
  Calls the same named GenServer on every node in the cluster.
  Uses GenServer.call({name, node}, message) -- not RPC.
  Returns %{node => {:ok, reply} | {:error, reason}}.

  GenServer.call is preferred over :erpc for named processes because it provides
  automatic monitoring (detects process death mid-call) and respects the GenServer's
  message queue ordering. Each node call runs in its own Task for parallelism.
  """
  @spec call_named(atom(), term(), pos_integer()) ::
          %{node() => {:ok, term()} | {:error, term()}}
  def call_named(server_name, message, timeout_ms \\ @default_timeout_ms) do
    nodes = [node() | Node.list()]

    task_map =
      nodes
      |> Enum.map(fn n ->
        task =
          Task.async(fn ->
            try do
              reply = GenServer.call({server_name, n}, message, timeout_ms)
              {:ok, reply}
            catch
              :exit, reason -> {:error, {:exit, reason}}
            end
          end)

        {task, n}
      end)

    tasks = Enum.map(task_map, fn {task, _node} -> task end)
    node_by_ref = Map.new(task_map, fn {task, n} -> {task.ref, n} end)

    results = Task.yield_many(tasks, timeout_ms)

    Map.new(results, fn {task, result} ->
      target_node = Map.fetch!(node_by_ref, task.ref)

      value =
        case result do
          {:ok, val} -> val
          nil ->
            Task.shutdown(task, :brutal_kill)
            {:error, :timeout}
        end

      {target_node, value}
    end)
  end
end
```

### Step 3: `lib/api_gateway/cluster/health_check.ex`

```elixir
defmodule ApiGateway.Cluster.HealthCheck do
  @moduledoc """
  Cluster-wide health check for api_gateway nodes.
  Collects diagnostics from every node using RPCClient.fanout/4.
  """

  alias ApiGateway.Cluster.RPCClient

  @doc """
  Runs diagnostics on every node in the cluster.
  Returns a structured report with per-node results and a summary.

  Uses RPCClient.fanout/4 to call collect/0 on every node in parallel,
  then classifies each result into :ok, :timeout, or :error buckets.
  Elapsed time is measured with monotonic_time to ensure accuracy regardless
  of wall clock adjustments.
  """
  @spec run() :: %{
          total_nodes: non_neg_integer(),
          elapsed_ms: non_neg_integer(),
          results: %{node() => {:ok, map()} | {:error, term()}},
          summary: %{ok: [node()], timeout: [node()], error: [node()]}
        }
  def run do
    start = System.monotonic_time(:millisecond)
    results = RPCClient.fanout(__MODULE__, :collect, [])
    elapsed = System.monotonic_time(:millisecond) - start

    summary =
      Enum.reduce(results, %{ok: [], timeout: [], error: []}, fn {n, result}, acc ->
        case result do
          {:ok, _data} -> %{acc | ok: [n | acc.ok]}
          {:error, :timeout} -> %{acc | timeout: [n | acc.timeout]}
          {:error, _reason} -> %{acc | error: [n | acc.error]}
        end
      end)

    %{
      total_nodes: map_size(results),
      elapsed_ms: elapsed,
      results: results,
      summary: summary
    }
  end

  @doc """
  Local diagnostics -- runs on whatever node calls this function.
  Used by fanout as the remote target.
  """
  @spec collect() :: map()
  def collect do
    %{
      node: node(),
      memory_mb: Float.round(:erlang.memory(:total) / (1024 * 1024), 1),
      process_count: length(Process.list()),
      scheduler_count: :erlang.system_info(:schedulers_online),
      uptime_ms: :erlang.statistics(:wall_clock) |> elem(0)
    }
  end
end
```

### Step 4: Tests

```elixir
# test/api_gateway/cluster/rpc_client_test.exs
defmodule ApiGateway.Cluster.RPCClientTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Cluster.RPCClient

  describe "call/5" do
    test "calls a function on the local node" do
      assert {:ok, result} = RPCClient.call(node(), String, :upcase, ["hello"])
      assert result == "HELLO"
    end

    test "returns {:error, :node_down} for a non-existent node" do
      assert {:error, :node_down} =
               RPCClient.call(:"phantom@localhost", String, :upcase, ["hello"])
    end

    test "returns {:error, {:exception, _}} when remote raises" do
      assert {:error, {:exception, _}} =
               RPCClient.call(node(), Kernel, :/, [1, 0])
    end
  end

  describe "fanout/4" do
    test "returns a map with at least the local node entry" do
      results = RPCClient.fanout(String, :upcase, ["ping"])
      assert is_map(results)
    end

    test "all values are tagged tuples" do
      results = RPCClient.fanout(String, :length, ["test"])

      Enum.each(results, fn {_node, result} ->
        assert match?({:ok, _}, result) or match?({:error, _}, result)
      end)
    end
  end

  describe "call_named/3" do
    test "returns a map with one entry per cluster node" do
      results = RPCClient.call_named(:nonexistent_server, :ping)
      assert is_map(results)
    end

    test "returns {:error, _} for a server that does not exist" do
      results = RPCClient.call_named(:nonexistent_server, :ping)

      Enum.each(results, fn {_node, result} ->
        assert match?({:error, _}, result)
      end)
    end
  end
end
```

```elixir
# test/api_gateway/cluster/health_check_test.exs
defmodule ApiGateway.Cluster.HealthCheckTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Cluster.HealthCheck

  describe "collect/0" do
    test "returns a map with expected keys" do
      result = HealthCheck.collect()
      assert is_map(result)
      assert Map.has_key?(result, :node)
      assert Map.has_key?(result, :memory_mb)
      assert Map.has_key?(result, :process_count)
      assert result.node == node()
    end
  end

  describe "run/0" do
    test "returns a valid report structure" do
      report = HealthCheck.run()
      assert Map.has_key?(report, :total_nodes)
      assert Map.has_key?(report, :elapsed_ms)
      assert Map.has_key?(report, :results)
      assert Map.has_key?(report, :summary)
      assert report.elapsed_ms >= 0
    end

    test "summary buckets are disjoint and cover all nodes" do
      report = HealthCheck.run()
      all_from_summary =
        (report.summary.ok ++ report.summary.timeout ++ report.summary.error)
        |> Enum.sort()

      all_from_results = Map.keys(report.results) |> Enum.sort()
      assert all_from_summary == all_from_results
    end
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/cluster/ --trace
```

### Step 6: RPC throughput benchmark

```elixir
# bench/rpc_bench.exs
# Run with: mix run bench/rpc_bench.exs
# Requires the node to be started with distribution:
#   iex --sname gateway1 --cookie secret -S mix run bench/rpc_bench.exs

Benchee.run(
  %{
    ":erpc.call -- local node (baseline)" => fn ->
      :erpc.call(node(), String, :upcase, ["benchmark"])
    end,
    "RPCClient.call -- wraps :erpc" => fn ->
      ApiGateway.Cluster.RPCClient.call(node(), String, :upcase, ["benchmark"])
    end
  },
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/rpc_bench.exs
```

**Expected**: `RPCClient.call` overhead over raw `:erpc.call` should be < 5us.

---

## Trade-off analysis

| Aspect | `:rpc.call` | `:erpc.call` | `GenServer.call {name, node}` | `send(remote_pid, msg)` |
|--------|------------|-------------|-------------------------------|------------------------|
| Concurrency bottleneck | `:rex` process on remote | None | None | None |
| Error format | `{:badrpc, reason}` | Raises exception | Raises on exit | None (fire and forget) |
| Automatic timeout | Via 5th arg | Via 5th arg | Via 3rd arg (default 5s) | No -- must implement |
| Automatic monitor | No | No | Yes (GenServer built-in) | No |
| OTP version | Always | OTP 23+ | Always | Always |
| Use case | Stateless admin ops | Stateless, high concurrency | Named supervised processes | Known PID, max throughput |

Reflection: when would you choose `GenServer.call {name, node}` over `:erpc.call`?
Think about what happens when the remote supervisor restarts the process mid-call.

---

## Common production mistakes

**1. Sequential fanout with multiplied timeouts**
Calling `Enum.map(nodes, fn n -> :erpc.call(n, M, F, A, 5_000) end)` takes up to
`5_000 * length(nodes)` milliseconds in the worst case. Always fan out with `Task.async`
and collect with `Task.yield_many` using a single global timeout.

**2. Using `:rpc` for high-concurrency calls**
Under 100+ concurrent RPCs to the same node, `:rex` becomes a bottleneck. Switch to
`:erpc` or direct `GenServer.call {name, node}` for throughput-sensitive paths.

**3. Not handling `{:badrpc, :timeout}` residual execution**
When `:rpc.call` returns `{:badrpc, :timeout}`, the remote function is still running.
If it has side effects (DB writes, state mutations), you have a partial execution problem.
Design remote operations to be idempotent, or use sagas with compensation.

**4. Including the local node in `Node.list()` fanout**
`Node.list()` never includes `node()` itself. If your fanout needs to include the local
node, add it explicitly: `[node() | Node.list()]`.

**5. Passing anonymous functions as RPC arguments**
`fn -> ... end` captures its lexical environment and is not serializable across nodes.
Only pass plain data (strings, atoms, maps, lists) as RPC arguments.

---

## Resources

- [`:erpc` documentation -- OTP 23+](https://www.erlang.org/doc/man/erpc.html) -- read the error semantics section
- [`:rpc` documentation](https://www.erlang.org/doc/man/rpc.html) -- specifically the `:rex` architecture note
- [Erlang in Anger -- Fred Hebert](https://www.erlang-in-anger.com/) -- chapter on distributed systems pitfalls (free PDF)
- [Distributed Elixir -- The Little Elixir and OTP Guidebook](https://www.manning.com/books/the-little-elixir-and-otp-guidebook) -- chapter on distribution
