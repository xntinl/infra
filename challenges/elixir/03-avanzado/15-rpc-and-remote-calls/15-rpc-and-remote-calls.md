# RPC and Remote Calls

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`, an internal HTTP gateway that routes traffic to microservices.
Routing, rate limiting, and caching are already working. Now you need to distribute
administrative operations — configuration reloads, health checks, and stats collection —
across a cluster of gateway nodes.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       │   └── server.ex
│       ├── cache/
│       │   └── store.ex
│       └── cluster/
│           ├── rpc_client.ex      # ← you implement this
│           └── health_check.ex   # ← and this
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
2. Handle node failures gracefully — a slow or dead node must not block the others
3. Distinguish between `:node_down`, `:timeout`, and `:remote_exception` — each has a
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

- **Automatic 5s timeout** — built into GenServer, not something you manage
- **Automatic monitor** — if the remote process dies mid-call, you get `{:exit, :noproc}` instead of hanging
- **Supervisor-awareness** — the call respects the GenServer's message queue ordering

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
  - Uses :erpc for all calls (OTP 23+) — no :rex bottleneck
  - All public functions return tagged tuples — never raise to the caller
  - Fanout is always parallel — never sequential with multiplied timeouts
  """

  @default_timeout_ms 5_000

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Calls module.function(args) on a single remote node.
  Returns {:ok, result} | {:error, :node_down | :timeout | {:exception, term()}}
  """
  @spec call(node(), module(), atom(), list(), pos_integer()) ::
          {:ok, term()} | {:error, :node_down | :timeout | {:exception, term()}}
  def call(node, module, function, args, timeout_ms \\ @default_timeout_ms) do
    # HINT: :erpc.call/5 raises on error — wrap in try/catch
    # HINT: catch :error, {:erpc, :noconnection} for node_down
    # HINT: catch :error, {:erpc, :timeout} for timeout
    # HINT: catch :error, {exception, _stacktrace} for remote exceptions
    # TODO: implement
  end

  @doc """
  Fans out module.function(args) to all connected nodes in parallel.
  Returns a map of %{node => {:ok, result} | {:error, reason}}.

  The global timeout applies to ALL nodes together, not per node.
  A slow node does not block results from fast nodes.
  """
  @spec fanout(module(), atom(), list(), pos_integer()) ::
          %{node() => {:ok, term()} | {:error, term()}}
  def fanout(module, function, args, timeout_ms \\ @default_timeout_ms) do
    nodes = Node.list()
    # HINT: use Task.async per node, then Task.yield_many with the global timeout
    # HINT: {:ok, result} on success, nil on timeout → {:error, :timeout}
    # HINT: include the local node using call to localhost (node()) for consistency
    # TODO: implement
  end

  @doc """
  Calls the same named GenServer on every node in the cluster.
  Uses GenServer.call({name, node}, message) — not RPC.
  Returns %{node => {:ok, reply} | {:error, reason}}.
  """
  @spec call_named(atom(), term(), pos_integer()) ::
          %{node() => {:ok, term()} | {:error, term()}}
  def call_named(server_name, message, timeout_ms \\ @default_timeout_ms) do
    nodes = [node() | Node.list()]
    # HINT: Task.async per node, GenServer.call({server_name, node}, message, timeout_ms)
    # HINT: wrap in try/catch — the remote process may not exist
    # TODO: implement
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
  """
  @spec run() :: %{
          total_nodes: non_neg_integer(),
          elapsed_ms: non_neg_integer(),
          results: %{node() => {:ok, map()} | {:error, term()}},
          summary: %{ok: [node()], timeout: [node()], error: [node()]}
        }
  def run do
    # HINT: use RPCClient.fanout/4 with NodeDiagnostics.collect/0
    # HINT: measure elapsed time with System.monotonic_time(:millisecond)
    # HINT: summarize results into :ok, :timeout, :error buckets
    # TODO: implement
  end

  @doc """
  Local diagnostics — runs on whatever node calls this function.
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

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/cluster/rpc_client_test.exs
defmodule ApiGateway.Cluster.RPCClientTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Cluster.RPCClient

  describe "call/5" do
    test "calls a function on the local node" do
      # Calling node() itself with :erpc is valid
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
      # In a single-node test environment, Node.list() is empty.
      # The implementation must handle this gracefully.
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
    ":erpc.call — local node (baseline)" => fn ->
      :erpc.call(node(), String, :upcase, ["benchmark"])
    end,
    "RPCClient.call — wraps :erpc" => fn ->
      ApiGateway.Cluster.RPCClient.call(node(), String, :upcase, ["benchmark"])
    end,
    "GenServer.call — local named server" => fn ->
      # Replace with an actual named GenServer in your app
      GenServer.call(ApiGateway.Cache.Store, :stats)
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

**Expected**: `RPCClient.call` overhead over raw `:erpc.call` should be < 5µs.
The GenServer path will be faster for local named processes because it skips
the `:erpc` spawn overhead.

---

## Trade-off analysis

| Aspect | `:rpc.call` | `:erpc.call` | `GenServer.call {name, node}` | `send(remote_pid, msg)` |
|--------|------------|-------------|-------------------------------|------------------------|
| Concurrency bottleneck | `:rex` process on remote | None | None | None |
| Error format | `{:badrpc, reason}` | Raises exception | Raises on exit | None (fire and forget) |
| Automatic timeout | Via 5th arg | Via 5th arg | Via 3rd arg (default 5s) | No — must implement |
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

- [`:erpc` documentation — OTP 23+](https://www.erlang.org/doc/man/erpc.html) — read the error semantics section
- [`:rpc` documentation](https://www.erlang.org/doc/man/rpc.html) — specifically the `:rex` architecture note
- [Erlang in Anger — Fred Hebert](https://www.erlang-in-anger.com/) — chapter on distributed systems pitfalls (free PDF)
- [Distributed Elixir — The Little Elixir and OTP Guidebook](https://www.manning.com/books/the-little-elixir-and-otp-guidebook) — chapter on distribution
