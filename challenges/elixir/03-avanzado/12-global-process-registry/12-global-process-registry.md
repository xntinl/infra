# Global Process Registry: Cluster-Wide Coordination

## Goal

Build a leader election system using Erlang's `:global` module for cluster-wide singleton processes. The implementation includes a `Leader` GenServer that periodically attempts to claim a global name, handles netsplit conflict resolution deterministically, and a `Janitor.Worker` that only runs cleanup tasks on the leader node.

---

## Why `:global` for leader election

`:global` maintains a cluster-wide mapping from name to PID. Every node has a local copy. When any node registers a name, all others are updated. When a registered process dies, the name is automatically removed.

`:global` is CP (consistent, not available during netsplits). For a route table coordinator or janitor, that is the correct trade-off -- wrong data is worse than temporary unavailability.

---

## How `:global` works

```elixir
# Register the current process under a global name
:global.register_name(:leader, self())     # :yes | :no

# Look up from any node
:global.whereis_name(:leader)              # PID or :undefined

# Using with GenServer's via tuple:
GenServer.start_link(__MODULE__, opts, name: {:global, :leader})
GenServer.call({:global, :leader}, :get_data)
```

### Conflict resolution after netsplits

During a netsplit, both partitions can independently register the same name. When the network heals, `:global` detects the conflict and calls a resolution callback:

```elixir
resolve = fn _name, pid1, pid2 ->
  if to_string(node(pid1)) <= to_string(node(pid2)), do: pid1, else: pid2
end

:global.register_name(:leader, self(), resolve)
```

The losing process receives `{:global_name_conflict, name}`.

---

## Full implementation

### `lib/api_gateway/cluster/leader.ex`

```elixir
defmodule ApiGateway.Cluster.Leader do
  use GenServer
  require Logger

  @election_interval_ms 3_000
  @leader_name          :api_gateway_leader

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns true if this node currently holds the leader registration."
  @spec leader?() :: boolean()
  def leader? do
    GenServer.call(__MODULE__, :leader?)
  end

  @doc "Returns the node atom of the current leader, or nil if no leader."
  @spec current_leader_node() :: atom() | nil
  def current_leader_node do
    case :global.whereis_name(@leader_name) do
      :undefined -> nil
      pid        -> node(pid)
    end
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    :net_kernel.monitor_nodes(true)
    send(self(), :attempt_election)
    {:ok, %{leader: false}}
  end

  # ---------------------------------------------------------------------------
  # Election logic
  # ---------------------------------------------------------------------------

  @impl true
  def handle_info(:attempt_election, state) do
    new_leader =
      if state.leader do
        # Already leader -- verify we still hold the name.
        if :global.whereis_name(@leader_name) == self() do
          true
        else
          Logger.warning("Lost leadership (detected during periodic check)")
          false
        end
      else
        case :global.register_name(@leader_name, self(), &resolve_conflict/3) do
          :yes ->
            Logger.info("Became leader on #{node()}")
            true
          :no ->
            false
        end
      end

    Process.send_after(self(), :attempt_election, @election_interval_ms)
    {:noreply, %{state | leader: new_leader}}
  end

  @impl true
  def handle_info({:nodedown, _node}, state) do
    leader_node = current_leader_node()

    if leader_node == nil do
      send(self(), :attempt_election)
    end

    {:noreply, state}
  end

  @impl true
  def handle_info({:nodeup, _node}, state) do
    {:noreply, state}
  end

  @impl true
  def handle_info({:global_name_conflict, @leader_name}, state) do
    Logger.warning("Lost leadership via conflict resolution")
    {:noreply, %{state | leader: false}}
  end

  @impl true
  def handle_call(:leader?, _from, state) do
    # Verify against :global, not just local state
    actual = :global.whereis_name(@leader_name) == self()
    {:reply, actual, %{state | leader: actual}}
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  # Deterministic conflict resolution: lexicographically smaller node name wins.
  defp resolve_conflict(_name, pid1, pid2) do
    if to_string(node(pid1)) <= to_string(node(pid2)), do: pid1, else: pid2
  end
end
```

### `lib/api_gateway/janitor/worker.ex`

Only the leader runs cleanup tasks -- non-leader nodes skip the work.

```elixir
defmodule ApiGateway.Janitor.Worker do
  use GenServer
  require Logger

  @task_interval_ms 30_000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    Process.send_after(self(), :run_tasks, @task_interval_ms)
    {:ok, %{tasks_run: 0}}
  end

  @impl true
  def handle_info(:run_tasks, state) do
    is_leader = ApiGateway.Cluster.Leader.leader?()

    new_tasks_run =
      if is_leader do
        Logger.info("Janitor running cleanup tasks (leader on #{node()})")
        purge_expired_audit_entries()
        remove_stale_circuit_breakers()
        state.tasks_run + 1
      else
        Logger.debug("Janitor skipping tasks -- not the leader")
        state.tasks_run
      end

    Process.send_after(self(), :run_tasks, @task_interval_ms)
    {:noreply, %{state | tasks_run: new_tasks_run}}
  end

  defp purge_expired_audit_entries do
    Logger.info("Purging audit entries older than 90 days")
    :ok
  end

  defp remove_stale_circuit_breakers do
    Logger.info("Removing circuit breaker workers with no traffic in 24h")
    :ok
  end
end
```

### Tests

```elixir
# test/api_gateway/cluster/leader_test.exs
defmodule ApiGateway.Cluster.LeaderTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cluster.Leader

  setup do
    :global.unregister_name(:api_gateway_leader)
    {:ok, _} = start_supervised(Leader)
    Process.sleep(50)
    :ok
  end

  describe "initial election" do
    test "becomes leader when no other leader exists" do
      assert Leader.leader?() == true
    end

    test "current_leader_node/0 returns this node" do
      assert Leader.current_leader_node() == node()
    end
  end

  describe "leader?/0 consistency" do
    test "leader?/0 reflects :global state, not cached state" do
      :global.unregister_name(:api_gateway_leader)
      Process.sleep(50)

      refute Leader.leader?()
    end

    test "leader reclaims name after it is cleared" do
      :global.unregister_name(:api_gateway_leader)
      Process.sleep(4_000)

      assert Leader.leader?() == true
    end
  end

  describe "conflict resolution" do
    test "handle_info :global_name_conflict clears leader state" do
      pid = Process.whereis(Leader)
      send(pid, {:global_name_conflict, :api_gateway_leader})
      Process.sleep(50)

      refute Leader.leader?()
    end
  end

  describe "nodedown handling" do
    test "triggers election attempt on nodedown" do
      pid = Process.whereis(Leader)

      :global.unregister_name(:api_gateway_leader)

      send(pid, {:nodedown, :"some_other_node@nohost"})
      Process.sleep(100)

      assert Leader.leader?() == true
    end
  end
end
```

---

## How it works

1. **Periodic election**: the Leader process attempts `:global.register_name` every 3 seconds. If already leader, it verifies it still holds the name (catches silent leadership loss).

2. **Conflict resolution**: `resolve_conflict/3` uses lexicographic node name comparison -- deterministic and symmetric, both sides agree on the winner.

3. **`:global_name_conflict` handling**: when the process loses a post-netsplit conflict, it clears its local leader flag. Without this handler, the process would believe it is still leader.

4. **Leader verification on every `leader?/0` call**: checks `:global.whereis_name` instead of trusting cached state, preventing stale leadership after conflict resolution.

5. **Leader-gated janitor**: the janitor checks `Leader.leader?()` before each task run. Non-leader nodes skip the work entirely.

---

## Common production mistakes

**1. Not handling `{:global_name_conflict, name}`**
The losing process still believes it is leader while another process holds the name.

**2. Using `:global` registration as a mutex for frequent operations**
`:global.register_name` involves cross-cluster locking. Use it only for low-frequency coordination.

**3. Forgetting to start the `:pg` scope**
`:pg` functions crash with `{:noproc, ...}` if the scope is not running. Start it in the supervision tree before any `:pg` calls.

**4. Using `:global` for high-churn registrations**
`:global` is optimized for long-lived singletons. For high-churn process registries, use `Horde.Registry` or local `Registry` with `:pg`.

---

## Resources

- [Erlang :global module](https://www.erlang.org/doc/man/global.html)
- [Erlang :pg module (OTP 23+)](https://www.erlang.org/doc/man/pg.html)
- [HexDocs -- GenServer via tuple](https://hexdocs.pm/elixir/GenServer.html#module-name-registration)
- [Horde documentation](https://hexdocs.pm/horde/readme.html)
