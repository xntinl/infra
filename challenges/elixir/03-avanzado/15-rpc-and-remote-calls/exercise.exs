# Exercise 15: RPC and Remote Calls Between Nodes
# Level: Advanced
# Topic: Invoking functions and processes across Erlang cluster nodes
#
# Prerequisites:
#   - Exercise 11 (Distributed Erlang Clustering) completed
#   - Two or more nodes running (same cookie)
#
# ============================================================
# BACKGROUND
# ============================================================
#
# "RPC" in the Erlang/Elixir world means running a function on a
# remote node and receiving the result. The OTP :rpc module provides
# this, built directly on top of distributed message passing.
#
# Unlike HTTP RPC (JSON over TCP), Erlang RPC:
#   - Serializes arguments and results automatically (ETF)
#   - Reuses the existing BEAM distribution channel (no extra ports)
#   - Supports all BEAM types natively (atoms, PIDs, references, etc.)
#   - Has no schema definition — any function can be called remotely
#
# :rpc functions:
#
#   :rpc.call(node, mod, fun, args)
#     Synchronous. Blocks until result or timeout.
#     Returns: result | {:badrpc, reason}
#
#   :rpc.call(node, mod, fun, args, timeout)
#     Same but with explicit timeout in ms.
#
#   :rpc.cast(node, mod, fun, args)
#     Fire-and-forget. Returns true immediately.
#     No result. Use when you don't need confirmation.
#
#   :rpc.async_call(node, mod, fun, args)
#     Non-blocking. Returns a "key" immediately.
#     Retrieve result later with :rpc.yield/1 or :rpc.nb_yield/2.
#
#   :rpc.yield(key)
#     Blocks until the async_call result is ready.
#     Returns: result | {:badrpc, reason}
#
#   :rpc.nb_yield(key, timeout)
#     Non-blocking yield. Returns {:value, result} | :timeout
#
#   :rpc.multicall(nodes, mod, fun, args)
#     Calls all nodes simultaneously.
#     Returns: {results_list, bad_nodes_list}
#
#   :rpc.pmap({mod, fun}, args_list)  — NOT standard, use for reference only
#
# Error handling:
#   {:badrpc, :nodedown}      — node is unreachable
#   {:badrpc, :timeout}       — call exceeded timeout
#   {:badrpc, {:EXIT, reason}} — function raised an exception
#   {:badrpc, reason}         — other RPC infrastructure error
#
# ============================================================
# EXERCISE 1 — :rpc.call: Execute a Function on a Remote Node
# ============================================================
#
# Goal: call a function on a remote node, handle errors, and compare
# with the equivalent GenServer.call approach.

defmodule Exercise15.RemoteCompute do
  @moduledoc """
  A module with CPU-intensive functions meant to be called via RPC
  on a remote node — useful for offloading work from a busy node.
  """

  @doc """
  Computes the Nth Fibonacci number. Intentionally naive (exponential)
  to simulate CPU work.
  """
  def fib(0), do: 0
  def fib(1), do: 1
  def fib(n) when n > 1, do: fib(n - 1) + fib(n - 2)

  @doc """
  Returns system statistics of the node running this function.
  """
  def system_info do
    %{
      node: Node.self(),
      schedulers: System.schedulers_online(),
      memory_mb: :erlang.memory(:total) |> div(1_048_576),
      process_count: :erlang.system_info(:process_count)
    }
  end

  @doc """
  Intentionally raises — for testing :badrpc error handling.
  """
  def crash! do
    raise RuntimeError, "deliberate crash for testing"
  end
end

defmodule Exercise15.RpcClient do
  @moduledoc """
  Client-side wrapper for remote calls to Exercise15.RemoteCompute.

  Demonstrates three patterns:
    1. Synchronous :rpc.call with error handling
    2. Node selection based on availability
    3. Retry logic for transient failures
  """
  require Logger

  @default_timeout 10_000

  @doc """
  Computes fib(n) on `target_node`.
  Returns {:ok, result} or {:error, reason}.
  """
  def remote_fib(target_node, n, timeout \\ @default_timeout) do
    # TODO: implement using :rpc.call/5
    # Handle all :badrpc variants explicitly
    :not_implemented
  end

  @doc """
  Fetches system info from `target_node`.
  """
  def remote_system_info(target_node) do
    # TODO: implement
    :not_implemented
  end

  @doc """
  Calls a function on multiple nodes simultaneously using :rpc.multicall/4.
  Returns {:ok, results_map} where results_map is %{node => result}.
  Nodes that failed are reported separately.
  """
  def call_all_nodes(nodes, mod, fun, args) do
    # TODO: implement using :rpc.multicall/4
    # :rpc.multicall returns {[results], [bad_nodes]}
    # Zip nodes with results to build a map
    :not_implemented
  end

  @doc """
  Retries `fun` up to `max_attempts` times with exponential backoff.
  Returns {:ok, result} or {:error, :max_retries_exceeded}.
  """
  def with_retry(fun, max_attempts \\ 3) do
    do_retry(fun, max_attempts, 1)
  end

  # --- Private ---

  defp do_retry(_fun, 0, _attempt) do
    {:error, :max_retries_exceeded}
  end

  defp do_retry(fun, attempts_remaining, attempt) do
    case fun.() do
      {:ok, _} = ok ->
        ok

      {:error, :nodedown} ->
        backoff_ms = min(:math.pow(2, attempt) * 100 |> round(), 5_000)
        Logger.warning("RPC failed (nodedown). Retrying in #{backoff_ms}ms...")
        Process.sleep(backoff_ms)
        do_retry(fun, attempts_remaining - 1, attempt + 1)

      {:error, reason} ->
        # Non-retriable errors: bad args, function crash, etc.
        {:error, reason}
    end
  end

  defp handle_rpc_result({:badrpc, :nodedown}), do: {:error, :nodedown}
  defp handle_rpc_result({:badrpc, :timeout}), do: {:error, :timeout}
  defp handle_rpc_result({:badrpc, {:EXIT, reason}}), do: {:error, {:remote_crash, reason}}
  defp handle_rpc_result({:badrpc, reason}), do: {:error, {:badrpc, reason}}
  defp handle_rpc_result(result), do: {:ok, result}
end

# --------------------------------------------------
# Manual test steps:
# --------------------------------------------------
#
# Terminal 1 (node_a):
#   iex --sname node_a --cookie mycluster
#   Code.require_file("exercise.exs")
#
# Terminal 2 (node_b):
#   iex --sname node_b --cookie mycluster
#   Node.connect(:node_a@hostname)
#   Code.require_file("exercise.exs")
#   Exercise15.RpcClient.remote_fib(:node_a@hostname, 30)
#   # => {:ok, 832040}  (computed on node_a's schedulers)
#   Exercise15.RpcClient.remote_system_info(:node_a@hostname)
#   # => {:ok, %{node: :node_a@hostname, ...}}

# --------------------------------------------------
# Hints for Exercise 1
# --------------------------------------------------
#
# Hint 1: Basic :rpc.call structure:
#   case :rpc.call(node, mod, fun, args, timeout) do
#     {:badrpc, reason} -> handle_rpc_result({:badrpc, reason})
#     result -> {:ok, result}
#   end
#
# Hint 2: :rpc.multicall returns {results, bad_nodes} where results
#   is a list parallel to the nodes list:
#   {results, bad_nodes} = :rpc.multicall(nodes, mod, fun, args)
#   Enum.zip(nodes, results) |> Map.new()
#
# Hint 3: :rpc.call is NOT the same as a raw send — it serializes
#   args/result through ETF. Functions, closures (fns), and ETS tables
#   are NOT serializable. Pass only data (maps, lists, atoms, etc.).
#
# Hint 4: For the crash!/0 test:
#   Exercise15.RpcClient.remote_system_info(:node_a@hostname)
#   |> case do — this is your error path.
#   Calling crash! via RPC returns {:badrpc, {:EXIT, {%RuntimeError{}, stacktrace}}}.

# ============================================================
# EXERCISE 2 — Async RPC: Parallel Calls to Multiple Nodes
# ============================================================
#
# Goal: fire async RPC calls to N nodes simultaneously, then collect
# all results — without waiting for each node sequentially.
#
# Pattern: scatter-gather
#   1. Fire async_call to each node (returns a key immediately)
#   2. Do other work or fire more calls
#   3. Gather results with :rpc.yield/1 in parallel
#
# This is MUCH faster than sequential :rpc.call when latency matters:
#   Sequential: total_time ≈ N × per_node_latency
#   Parallel:   total_time ≈ max(per_node_latency) + overhead

defmodule Exercise15.AsyncRpc do
  @moduledoc """
  Demonstrates :rpc.async_call for concurrent multi-node RPC.
  """
  require Logger

  @doc """
  Computes `fun.(arg)` on all `nodes` concurrently using async RPC.
  Returns a list of {:ok, node, result} or {:error, node, reason},
  sorted by completion order.

  Example:
    Exercise15.AsyncRpc.scatter(
      [:node_a@host, :node_b@host],
      Exercise15.RemoteCompute,
      :system_info,
      []
    )
  """
  def scatter(nodes, mod, fun, args, timeout \\ 10_000) do
    # TODO: implement scatter-gather using :rpc.async_call/:rpc.yield
    #
    # Steps:
    # 1. For each node, fire :rpc.async_call(node, mod, fun, args)
    #    and keep a map of %{key => node}
    # 2. For each key, call :rpc.yield(key) to collect the result
    # 3. Normalize each result to {:ok, node, result} or {:error, node, reason}
    :not_implemented
  end

  @doc """
  Runs `fun.(data_chunk)` on nodes in round-robin, using async RPC.
  Distributes a list of work items across nodes and collects results.

  Example:
    data = [1, 2, 3, 4, 5, 6, 7, 8]
    nodes = [:node_a@host, :node_b@host]
    Exercise15.AsyncRpc.map_across_nodes(nodes, data, Exercise15.RemoteCompute, :fib)
    # node_a gets [1, 3, 5, 7], node_b gets [2, 4, 6, 8]
  """
  def map_across_nodes(nodes, data_items, mod, fun) do
    # TODO: implement
    # Hint: pair each item with a node via round-robin (rem/2)
    # Fire async_call for each item, then gather results
    :not_implemented
  end

  # --- Private ---

  defp normalize_result(node, {:badrpc, reason}), do: {:error, node, reason}
  defp normalize_result(node, result), do: {:ok, node, result}
end

# --------------------------------------------------
# Hints for Exercise 2
# --------------------------------------------------
#
# Hint 1: scatter/5 structure:
#   keys_to_nodes =
#     nodes
#     |> Enum.map(fn node ->
#       key = :rpc.async_call(node, mod, fun, args)
#       {key, node}
#     end)
#     |> Map.new()
#
#   Enum.map(keys_to_nodes, fn {key, node} ->
#     result = :rpc.yield(key, timeout)
#     normalize_result(node, result)
#   end)
#
# Hint 2: :rpc.nb_yield/2 (non-blocking yield) lets you poll:
#   case :rpc.nb_yield(key, 0) do
#     {:value, result} -> result
#     :timeout -> :still_waiting
#   end
#   This is useful for implementing a "first N responses wins" pattern.
#
# Hint 3: For map_across_nodes/4 — pair each item with its node:
#   data_items
#   |> Enum.with_index()
#   |> Enum.map(fn {item, i} ->
#     node = Enum.at(nodes, rem(i, length(nodes)))
#     key = :rpc.async_call(node, mod, fun, [item])
#     {key, node, item}
#   end)
#   Then yield each key.
#
# Hint 4: If a node goes down while async calls are in flight,
#   :rpc.yield/1 returns {:badrpc, :nodedown}. Build your gather
#   loop to handle partial failures — don't let one bad node
#   block the others.

# ============================================================
# EXERCISE 3 — RPC Load Balancer: Round-Robin Distribution
# ============================================================
#
# Goal: implement a GenServer that maintains a pool of worker nodes
# and distributes RPC calls across them using round-robin, with
# automatic failover when a node goes down.

defmodule Exercise15.RpcLoadBalancer do
  @moduledoc """
  A round-robin RPC load balancer for a pool of worker nodes.

  Features:
    - Distributes calls evenly across healthy nodes
    - Detects node failures via :net_kernel.monitor_nodes
    - Removes failed nodes and re-adds them when they come back
    - Exposes health/stats for observability
  """
  use GenServer
  require Logger

  defstruct nodes: [],
            current_index: 0,
            total_calls: 0,
            failed_calls: 0,
            calls_per_node: %{}

  # --- Client API ---

  @doc """
  Starts the load balancer with an initial list of worker nodes.
  """
  def start_link(nodes) when is_list(nodes) do
    GenServer.start_link(__MODULE__, nodes, name: __MODULE__)
  end

  @doc """
  Calls `mod.fun(args)` on the next node in the round-robin sequence.
  Returns {:ok, result} or {:error, reason}.

  If the chosen node is down, automatically tries the next node.
  If all nodes are down, returns {:error, :no_nodes_available}.
  """
  def call(mod, fun, args, timeout \\ 5_000) do
    GenServer.call(__MODULE__, {:rpc_call, mod, fun, args, timeout})
  end

  @doc """
  Adds a new node to the pool. Idempotent.
  """
  def add_node(node) do
    GenServer.cast(__MODULE__, {:add_node, node})
  end

  @doc """
  Manually removes a node from the pool.
  """
  def remove_node(node) do
    GenServer.cast(__MODULE__, {:remove_node, node})
  end

  @doc """
  Returns current pool status.
  """
  def status do
    GenServer.call(__MODULE__, :status)
  end

  # --- Server Callbacks ---

  @impl true
  def init(nodes) do
    :net_kernel.monitor_nodes(true, node_type: :visible)
    state = %__MODULE__{
      nodes: nodes,
      calls_per_node: Map.new(nodes, &{&1, 0})
    }
    Logger.info("RpcLoadBalancer started with nodes: #{inspect(nodes)}")
    {:ok, state}
  end

  @impl true
  def handle_call({:rpc_call, mod, fun, args, timeout}, _from, state) do
    case pick_and_call(mod, fun, args, timeout, state) do
      {:ok, result, new_state} ->
        {:reply, {:ok, result}, new_state}

      {:error, reason, new_state} ->
        {:reply, {:error, reason}, new_state}
    end
  end

  @impl true
  def handle_call(:status, _from, state) do
    status = %{
      nodes: state.nodes,
      total_calls: state.total_calls,
      failed_calls: state.failed_calls,
      calls_per_node: state.calls_per_node,
      success_rate: success_rate(state)
    }

    {:reply, status, state}
  end

  @impl true
  def handle_cast({:add_node, node}, state) do
    if node in state.nodes do
      {:noreply, state}
    else
      Logger.info("Adding node to pool: #{node}")
      new_state = %{state |
        nodes: state.nodes ++ [node],
        calls_per_node: Map.put(state.calls_per_node, node, 0)
      }
      {:noreply, new_state}
    end
  end

  @impl true
  def handle_cast({:remove_node, node}, state) do
    Logger.info("Removing node from pool: #{node}")
    {:noreply, drop_node(state, node)}
  end

  @impl true
  def handle_info({:nodedown, node, _info}, state) do
    Logger.warning("Node went down, removing from pool: #{node}")
    {:noreply, drop_node(state, node)}
  end

  @impl true
  def handle_info({:nodeup, node, _info}, state) do
    # Only re-add if the node was previously in our pool.
    # We don't automatically add unknown nodes — they must be added explicitly.
    Logger.info("Node came back up: #{node}")
    {:noreply, state}
  end

  # --- Private ---

  defp pick_and_call(_mod, _fun, _args, _timeout, %{nodes: []} = state) do
    {:error, :no_nodes_available, %{state | failed_calls: state.failed_calls + 1}}
  end

  defp pick_and_call(mod, fun, args, timeout, state) do
    # TODO: implement round-robin selection and RPC call
    # 1. Pick node at state.current_index (wrap around with rem/2)
    # 2. Call :rpc.call/5 on that node
    # 3. On :badrpc :nodedown, remove the node and try the next one
    # 4. Update current_index, total_calls, calls_per_node
    :not_implemented
  end

  defp drop_node(state, node) do
    new_nodes = List.delete(state.nodes, node)
    # Reset index if it's now out of bounds
    new_index =
      if state.current_index >= length(new_nodes) do
        0
      else
        state.current_index
      end

    %{state | nodes: new_nodes, current_index: new_index}
  end

  defp success_rate(%{total_calls: 0}), do: 1.0
  defp success_rate(%{total_calls: t, failed_calls: f}), do: (t - f) / t
end

# --------------------------------------------------
# Hints for Exercise 3
# --------------------------------------------------
#
# Hint 1: pick_and_call/5 round-robin + RPC:
#   node = Enum.at(state.nodes, state.current_index)
#   next_index = rem(state.current_index + 1, length(state.nodes))
#
#   case :rpc.call(node, mod, fun, args, timeout) do
#     {:badrpc, :nodedown} ->
#       # Node died — remove it and try again with the next one
#       new_state = drop_node(%{state | current_index: next_index}, node)
#       pick_and_call(mod, fun, args, timeout, new_state)
#     {:badrpc, reason} ->
#       new_state = %{state | current_index: next_index, failed_calls: state.failed_calls + 1}
#       {:error, {:badrpc, reason}, new_state}
#     result ->
#       new_state = %{state |
#         current_index: next_index,
#         total_calls: state.total_calls + 1,
#         calls_per_node: Map.update(state.calls_per_node, node, 1, &(&1 + 1))
#       }
#       {:ok, result, new_state}
#   end
#
# Hint 2: Guard against infinite recursion — if all nodes are down,
#   the recursive call hits the %{nodes: []} clause and returns
#   {:error, :no_nodes_available}. No risk of infinite loop.
#
# Hint 3: For weighted load balancing (prefer nodes with lower load),
#   replace round-robin with a priority queue keyed on calls_per_node.
#   Horde.UniformQuorumDistribution does something similar internally.
#
# Hint 4: Production load balancers use health checks (periodic ping)
#   rather than relying solely on :nodedown events, because :nodedown
#   can be delayed by up to net_ticktime seconds (~60s by default).
#   Add a periodic :check_health message with a 5s interval.

# ============================================================
# Node.spawn_link — Remote Process Spawn (Bonus)
# ============================================================
#
# An alternative to RPC: spawn a linked process on a remote node.
# The spawned process runs arbitrary code; the spawning process
# receives its result via message.
#
# Node.spawn_link(node, fun)       — spawn linked anonymous fn
# Node.spawn_link(node, mod, fun, args)  — spawn named function
#
# The link means: if the remote process dies, the local spawner
# receives an exit signal. This gives you crash detection for free.
#
# Example:

defmodule Exercise15.RemoteSpawn do
  @doc """
  Spawns a process on `target_node` that computes `fib(n)` and sends
  the result back to the calling process.
  """
  def spawn_fib(target_node, n) do
    caller = self()

    # spawn_link/2 returns the remote PID immediately
    remote_pid =
      Node.spawn_link(target_node, fn ->
        result = Exercise15.RemoteCompute.fib(n)
        send(caller, {:fib_result, n, result, Node.self()})
      end)

    {remote_pid, target_node}
  end

  @doc """
  Awaits the result from spawn_fib/2 with a timeout.
  """
  def await_fib(n, timeout \\ 5_000) do
    receive do
      {:fib_result, ^n, result, from_node} ->
        {:ok, result, from_node}
    after
      timeout -> {:error, :timeout}
    end
  end
end

# ============================================================
# TRADE-OFFS
# ============================================================
#
# :rpc.call vs GenServer.call({name, node}) vs Node.spawn_link:
#
#   :rpc.call
#     PRO: Simple API. Works for any public function. No process setup.
#     CON: Runs in a temporary "rex" process on the remote node —
#          not supervised, no rate limiting. Under heavy load, you can
#          accidentally spawn thousands of rex processes.
#
#   GenServer.call({name, node}, req)
#     PRO: Uses a supervised, rate-limited process. Back-pressure
#          from GenServer's mailbox. Familiar error handling.
#     CON: Requires a running GenServer on the remote node.
#          Couples the client to the server's registered name.
#
#   Node.spawn_link/2
#     PRO: Most flexible — arbitrary code, crash-linked to caller.
#          No setup required on the remote side.
#     CON: No supervision, no back-pressure, no retry logic.
#          Linking across nodes: if the remote node dies, you get
#          a :noconnection exit signal (not :nodedown).
#
# :rpc.cast vs send({name, node}, msg):
#   :rpc.cast uses the rex process — slightly more overhead than
#   direct send. But send({name, node}, msg) requires the recipient
#   to be registered under a name. Use whichever is more readable.
#
# Timeout gotcha:
#   :rpc.call with timeout = 5000 means the CALLER waits up to 5s.
#   But the function may STILL be running on the remote node after
#   the timeout! The rex process on the remote node is not killed.
#   This is a resource leak — long-running RPC calls that time out
#   will pile up on the remote node. Mitigate by ensuring remote
#   functions are quick or have their own internal timeout.
#
# PRODUCTION RECOMMENDATION:
#   - Use :rpc.call for simple, infrequent administrative operations
#     (health checks, config reads, feature flag queries).
#   - Use GenServer.call({name, node}, req) for business-critical
#     cross-node communication that needs supervision.
#   - Use :rpc.multicall for fan-out operations (broadcast config,
#     flush caches on all nodes, collect metrics).
#   - Avoid :rpc for hot paths (high-throughput, low-latency) —
#     prefer co-locating hot data on the calling node via consistent
#     hashing (libring, jumper) so calls are local.

# ============================================================
# ONE POSSIBLE SOLUTION (sparse)
# ============================================================

defmodule Exercise15.Solution do
  # Exercise 1
  def remote_fib(target_node, n, timeout \\ 10_000) do
    case :rpc.call(target_node, Exercise15.RemoteCompute, :fib, [n], timeout) do
      {:badrpc, :nodedown} -> {:error, :nodedown}
      {:badrpc, :timeout} -> {:error, :timeout}
      {:badrpc, {:EXIT, reason}} -> {:error, {:remote_crash, reason}}
      {:badrpc, reason} -> {:error, {:badrpc, reason}}
      result -> {:ok, result}
    end
  end

  def call_all_nodes(nodes, mod, fun, args) do
    {results, bad_nodes} = :rpc.multicall(nodes, mod, fun, args)

    good =
      nodes
      |> Enum.reject(&(&1 in bad_nodes))
      |> Enum.zip(results)
      |> Map.new()

    bad = Map.new(bad_nodes, &{&1, :nodedown})
    Map.merge(good, bad)
  end

  # Exercise 2
  def scatter(nodes, mod, fun, args, timeout \\ 10_000) do
    keys_to_nodes =
      nodes
      |> Enum.map(fn node ->
        key = :rpc.async_call(node, mod, fun, args)
        {key, node}
      end)
      |> Map.new()

    Enum.map(keys_to_nodes, fn {key, node} ->
      result = :rpc.yield(key, timeout)

      case result do
        {:badrpc, reason} -> {:error, node, reason}
        value -> {:ok, node, value}
      end
    end)
  end

  def map_across_nodes(nodes, data_items, mod, fun) do
    keys =
      data_items
      |> Enum.with_index()
      |> Enum.map(fn {item, i} ->
        node = Enum.at(nodes, rem(i, length(nodes)))
        key = :rpc.async_call(node, mod, fun, [item])
        {key, node, item}
      end)

    Enum.map(keys, fn {key, node, item} ->
      case :rpc.yield(key) do
        {:badrpc, reason} -> {:error, node, item, reason}
        result -> {:ok, node, item, result}
      end
    end)
  end

  # Exercise 3: pick_and_call/5 — see hint 1 above for the full implementation
end
