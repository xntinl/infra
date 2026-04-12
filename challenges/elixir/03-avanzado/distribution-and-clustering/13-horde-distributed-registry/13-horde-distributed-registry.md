# Horde: Distributed Registry and Supervisor

## Goal

Replace a single-node `DynamicSupervisor` for circuit breaker workers with `Horde.DynamicSupervisor` and `Horde.Registry` so workers are distributed across cluster nodes and survive node failures. Includes a `HordeMembership` GenServer that synchronizes Horde's member list when nodes join or leave the cluster.

---

## Why Horde and not `:global`

`:global` is CP: during a netsplit, registration operations block or fail. It also does not manage process lifecycle -- you would need custom restart logic. Horde provides:

- Automatic restart on node crash (built-in)
- O(1) local registry lookup (eventual consistency)
- AP behavior during netsplits (diverges, reconciles on heal)
- Delta CRDT propagation without blocking locks

---

## How Horde works

### Delta CRDTs

Horde's state is a delta CRDT (Conflict-free Replicated Data Type). Each node maintains a local copy. Changes propagate as small deltas:

```
Node A registers {payment-svc -> PID<0.234.0>}
  -> broadcasts delta to all nodes
  -> all nodes merge into local copy
  -> consistent without locking
```

### Cluster membership

Horde does not auto-discover nodes. You must call `Horde.Cluster.set_members/2` when topology changes. The standard pattern: a GenServer that subscribes to `:net_kernel.monitor_nodes` and calls `set_members` on node up/down.

### Process distribution

Horde uses consistent hashing on the process `id`. When a node is added, only a fraction of processes migrate. When a node crashes, only its processes restart elsewhere.

---

## Full implementation

### `mix.exs` dependency

```elixir
defp deps do
  [{:horde, "~> 0.9"}]
end
```

### `lib/api_gateway/cluster/horde_membership.ex`

Listens for node events and synchronizes Horde's member list.

```elixir
defmodule ApiGateway.Cluster.HordeMembership do
  use GenServer
  require Logger

  @horde_members [
    ApiGateway.CircuitBreaker.Registry,
    ApiGateway.CircuitBreaker.HordeSupervisor
  ]

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :net_kernel.monitor_nodes(true)
    send(self(), :sync_members)
    {:ok, []}
  end

  @impl true
  def handle_info({:nodeup, node}, state) do
    Logger.info("HordeMembership: node joined #{node}, syncing")
    sync_members()
    {:noreply, state}
  end

  @impl true
  def handle_info({:nodedown, node}, state) do
    Logger.warning("HordeMembership: node left #{node}, syncing")
    sync_members()
    {:noreply, state}
  end

  @impl true
  def handle_info(:sync_members, state) do
    sync_members()
    {:noreply, state}
  end

  defp sync_members do
    all_nodes = [node() | Node.list()]

    Enum.each(@horde_members, fn component ->
      members = Enum.map(all_nodes, &{component, &1})

      case Horde.Cluster.set_members(component, members) do
        :ok ->
          Logger.debug("HordeMembership: synced #{inspect(component)} with #{length(members)} members")
        {:error, reason} ->
          Logger.error("HordeMembership: failed to sync #{inspect(component)}: #{inspect(reason)}")
      end
    end)
  end
end
```

### `lib/api_gateway/circuit_breaker/worker.ex`

Uses a via tuple to register with `Horde.Registry` for cluster-wide discoverability.

```elixir
defmodule ApiGateway.CircuitBreaker.Worker do
  use GenServer

  @failure_threshold 5

  def start_link(service_name) do
    GenServer.start_link(__MODULE__, service_name,
      name: via(service_name)
    )
  end

  def child_spec(service_name) do
    %{
      id:      {__MODULE__, service_name},
      start:   {__MODULE__, :start_link, [service_name]},
      restart: :transient
    }
  end

  defp via(service_name) do
    {:via, Horde.Registry, {ApiGateway.CircuitBreaker.Registry, service_name}}
  end

  @impl true
  def init(service_name) do
    {:ok, %{service: service_name, status: :closed, failures: 0}}
  end

  @impl true
  def handle_call(:status, _from, state) do
    {:reply, state.status, state}
  end

  @impl true
  def handle_cast(:success, state) do
    new_status = if state.status == :half_open, do: :closed, else: state.status
    {:noreply, %{state | failures: 0, status: new_status}}
  end

  @impl true
  def handle_cast(:failure, state) do
    new_failures = state.failures + 1

    new_status =
      cond do
        state.status == :closed and new_failures >= @failure_threshold -> :open
        state.status == :half_open -> :open
        true -> state.status
      end

    {:noreply, %{state | failures: new_failures, status: new_status}}
  end
end
```

### `lib/api_gateway/circuit_breaker/supervisor.ex`

Facade that wraps Horde operations.

```elixir
defmodule ApiGateway.CircuitBreaker.Supervisor do
  @moduledoc """
  Distributed circuit breaker supervisor using Horde.
  Workers are distributed across cluster nodes. If a node crashes,
  its workers restart on surviving nodes automatically.
  """

  @doc "Start a circuit breaker worker. Idempotent -- safe to call if already started."
  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, term()}
  def start_worker(service_name) do
    child_spec = ApiGateway.CircuitBreaker.Worker.child_spec(service_name)

    case Horde.DynamicSupervisor.start_child(ApiGateway.CircuitBreaker.HordeSupervisor, child_spec) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
      {:error, reason} -> {:error, reason}
    end
  end

  @doc "Look up the PID of a running circuit breaker worker."
  @spec find_worker(String.t()) :: {:ok, pid()} | {:error, :not_found}
  def find_worker(service_name) do
    case Horde.Registry.lookup(ApiGateway.CircuitBreaker.Registry, service_name) do
      [{pid, _meta}] -> {:ok, pid}
      [] -> {:error, :not_found}
    end
  end

  @doc "List PIDs of all running circuit breaker workers across the cluster."
  @spec list_workers() :: [pid()]
  def list_workers do
    Horde.Registry.select(ApiGateway.CircuitBreaker.Registry,
      [{{:"$1", :"$2", :"$3"}, [], [:"$2"]}]
    )
  end
end
```

### `application.ex` setup

```elixir
# In the children list:
{Horde.Registry,
  name:    ApiGateway.CircuitBreaker.Registry,
  keys:    :unique,
  members: []},

{Horde.DynamicSupervisor,
  name:     ApiGateway.CircuitBreaker.HordeSupervisor,
  strategy: :one_for_one,
  members:  []},

ApiGateway.Cluster.HordeMembership
```

### Tests

```elixir
# test/api_gateway/circuit_breaker/distributed_test.exs
defmodule ApiGateway.CircuitBreaker.DistributedTest do
  use ExUnit.Case, async: false

  alias ApiGateway.CircuitBreaker.{Worker, Supervisor}

  setup do
    Supervisor.list_workers()
    |> Enum.each(fn pid ->
      DynamicSupervisor.terminate_child(
        ApiGateway.CircuitBreaker.HordeSupervisor, pid
      )
    end)
    Process.sleep(50)
    :ok
  end

  describe "start_worker/1" do
    test "starts a worker and registers it" do
      assert {:ok, pid} = Supervisor.start_worker("payment-service")
      assert Process.alive?(pid)
    end

    test "second start_worker for same service returns existing pid" do
      {:ok, pid1} = Supervisor.start_worker("idempotent-service")
      result = Supervisor.start_worker("idempotent-service")
      assert match?({:ok, ^pid1}, result) or match?({:error, {:already_started, ^pid1}}, result)
    end
  end

  describe "find_worker/1" do
    test "returns {:ok, pid} for a running worker" do
      {:ok, _} = Supervisor.start_worker("find-test")
      assert {:ok, pid} = Supervisor.find_worker("find-test")
      assert Process.alive?(pid)
    end

    test "returns {:error, :not_found} for unknown service" do
      assert {:error, :not_found} = Supervisor.find_worker("nonexistent")
    end
  end

  describe "list_workers/0" do
    test "includes all running workers" do
      {:ok, pid_a} = Supervisor.start_worker("svc-list-a")
      {:ok, pid_b} = Supervisor.start_worker("svc-list-b")
      Process.sleep(100)

      workers = Supervisor.list_workers()
      assert pid_a in workers
      assert pid_b in workers
    end
  end

  describe "automatic restart on worker crash" do
    test "crashed worker is restarted by Horde" do
      {:ok, original_pid} = Supervisor.start_worker("crash-svc")
      ref = Process.monitor(original_pid)

      Process.exit(original_pid, :kill)
      assert_receive {:DOWN, ^ref, :process, _, _}, 1_000

      Process.sleep(500)

      assert {:ok, new_pid} = Supervisor.find_worker("crash-svc")
      assert Process.alive?(new_pid)
      assert new_pid != original_pid
    end
  end

  describe "worker state machine (via Horde)" do
    test "worker starts in :closed state" do
      {:ok, _} = Supervisor.start_worker("state-test-svc")
      {:ok, pid} = Supervisor.find_worker("state-test-svc")
      state = :sys.get_state(pid)
      assert state.status == :closed
    end
  end
end
```

---

## How it works

1. **Horde.DynamicSupervisor**: distributes processes across cluster nodes using consistent hashing on the child spec `id`. When a node crashes, Horde restarts its workers on surviving nodes.

2. **Horde.Registry**: cluster-wide process registry using delta CRDTs. Lookups are O(1) on the local node (eventual consistency). The `via` tuple makes workers discoverable from any node.

3. **HordeMembership**: listens for `:nodeup`/`:nodedown` events and calls `Horde.Cluster.set_members/2` to keep Horde's consistent hash ring synchronized with the actual cluster topology.

4. **`restart: :transient`**: workers removed intentionally (clean exit) are not restarted by Horde. Crashes (abnormal exits) are restarted.

5. **Idempotent `start_worker`**: handles `{:error, {:already_started, pid}}` as a success case -- safe to call concurrently from multiple nodes.

---

## Common production mistakes

**1. Not calling `Horde.Cluster.set_members/2` after node changes**
Horde does not auto-discover nodes. Without membership updates, new nodes are isolated.

**2. Treating Horde as a distributed ETS**
Horde registry lookups may be slightly stale. Do not use it for rate limiting counters where global uniqueness is required.

**3. Not handling the process restart window**
When a node crashes, Horde takes 100ms-2s to restart workers elsewhere. During this window, `find_worker/1` returns `{:error, :not_found}`. Callers must retry with backoff.

**4. Starting workers without checking `{:error, {:already_started, pid}}`**
If your code only handles `{:ok, pid}`, it crashes when another node started the same worker concurrently.

---

## Resources

- [Horde documentation](https://hexdocs.pm/horde/readme.html)
- [Horde.DynamicSupervisor](https://hexdocs.pm/horde/Horde.DynamicSupervisor.html)
- [Horde.Registry](https://hexdocs.pm/horde/Horde.Registry.html)
- [DeltaCrdt -- underlying CRDT library](https://hexdocs.pm/delta_crdt/DeltaCrdt.html)
- [libcluster -- automatic cluster formation](https://hexdocs.pm/libcluster/readme.html)
