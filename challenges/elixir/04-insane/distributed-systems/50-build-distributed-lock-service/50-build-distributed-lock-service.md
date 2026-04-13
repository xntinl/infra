# Distributed Lock Service (ZooKeeper-like)

**Project**: `locksmith` — Distributed lock manager using quorum writes and lease-based TTL

## Project context

Your team runs a three-node Elixir cluster. A background job — reindexing a large catalog — must not run on more than one node simultaneously. Using `:global.register_name/2` worked in development but caused a split-brain incident in production: after a network hiccup, both nodes thought they were the sole index job runner and both completed, writing conflicting data.

The problem with `:global`: it uses two-phase locking with no progress guarantee under partition. A partial network failure can leave both sides of a partition believing they hold the lock.

You will build `Locksmith`: a quorum-based distributed lock service where acquiring a lock requires agreement from a majority (2 of 3) of nodes. Locks are lease-based with TTL: a dead holder's lock expires automatically. Watches enable leader-election without polling.

## Design decisions

**Option A — single-leader lock manager**
- Pros: trivially linearizable, simple to reason about
- Cons: leader is a SPOF and a bottleneck

**Option B — Raft-replicated lock state with lease-based client leases** (chosen)
- Pros: HA, linearizable, leases survive client crashes
- Cons: Raft adds latency on every lock acquire

→ Chose **B** because a lock service that loses locks on node failure is worse than no lock service — HA is mandatory.

## Why quorum (majority) and not consensus of all nodes

Requiring all 3 nodes to agree makes the service unavailable if any one node is down. Requiring only a majority (2 of 3) tolerates one node failure. The key property: any two quorums share at least one node in common. If node A grants a lock to process X with quorum {A, B}, and node C later tries to grant the same lock to process Y, C must contact at least one of {A, B} — and that node will report the lock is held. The overlap guarantees no two processes can hold the lock simultaneously.

## Why lease-based TTL rather than explicit release

A lock holder that crashes never releases its lock. Without TTL, the lock stays held forever. With TTL, the holder must send periodic heartbeats to renew the lease. If the holder dies, the lease expires, and the lock is released automatically after at most `TTL` time. The trade-off: the holder might be alive but temporarily partitioned, and the lock expires. To handle this, holders must check the lease is still valid before acting on it (fencing tokens).

## Why watches and not polling for leader election

Polling for lock availability adds latency and wastes network round-trips. Watches are subscriptions: a process registers to be notified when a lock is acquired, released, or expired. When the current leader's lock expires, all watching followers are notified immediately and the first to acquire the lock becomes the new leader. This enables sub-second leader failover without polling.

## Project Structure

```
locksmith/
├── mix.exs
├── lib/
│   ├── locksmith/
│   │   ├── lock_manager.ex    # GenServer per node: lock state, quorum vote handler
│   │   ├── lease.ex           # Lease struct: name, holder, expires_at, epoch, count
│   │   ├── quorum.ex          # Quorum protocol: broadcast vote, collect responses
│   │   ├── heartbeat.ex       # Client heartbeat: renew lease before TTL
│   │   ├── watch.ex           # Watch registry: subscribe, notify on events
│   │   ├── election.ex        # Leader election: compete, follow, monitor leader
│   │   └── fencing.ex         # Monotonic fencing token for safe write validation
│   └── locksmith.ex           # Public API: acquire/2, release/1, renew/1, watch/2
├── test/
│   ├── lock_manager_test.exs
│   ├── quorum_test.exs
│   ├── lease_test.exs
│   ├── watch_test.exs
│   └── concurrent_test.exs    # N processes racing for same lock
└── bench/
    └── contention.exs
```

### Step 1: Lease and epoch

**Objective**: Bind each lease to a monotonic epoch so expired holders cannot forge valid writes after renewal failure.


A lease represents a lock grant with an expiration time. The `epoch` field is set when the quorum acquisition starts, tying all node-level grants for one logical lock attempt together. The `token` is a monotonically increasing fencing token: downstream storage systems can reject writes from stale holders by comparing tokens.


### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule Locksmith.Lease do
  @moduledoc """
  Lease struct representing a granted lock.

  The lease ties together the lock name, the holder process, the node
  that created it, and a fencing token that increases monotonically.
  The `count` field supports reentrant locks: the same holder can
  acquire the same lock multiple times, and must release it the
  same number of times.
  """

  @enforce_keys [:name, :holder, :holder_node, :expires_at, :epoch, :token]
  defstruct [:name, :holder, :holder_node, :expires_at, :epoch, :token, count: 1]

  @type t :: %__MODULE__{
    name: String.t(),
    holder: pid(),
    holder_node: node(),
    expires_at: integer(),
    epoch: integer(),
    token: pos_integer(),
    count: pos_integer()
  }

  @spec new(String.t(), pid(), pos_integer(), integer(), pos_integer()) :: t()
  def new(name, holder, ttl_ms, epoch, token) do
    %__MODULE__{
      name: name,
      holder: holder,
      holder_node: node(),
      expires_at: System.monotonic_time(:millisecond) + ttl_ms,
      epoch: epoch,
      token: token,
      count: 1
    }
  end

  @spec expired?(t()) :: boolean()
  def expired?(%__MODULE__{expires_at: exp}) do
    System.monotonic_time(:millisecond) > exp
  end

  @spec held_by?(t(), pid()) :: boolean()
  def held_by?(%__MODULE__{holder: h, holder_node: n}, pid) do
    h == pid and n == node()
  end
end
```

### Step 2: Lock manager (per node)

**Objective**: Serialize acquire and release through one GenServer per key so mutual exclusion holds without cross-key deadlock risk.


Each node runs one `LockManager` GenServer. It stores the local view of which locks are held, maintains per-lock watch subscriber lists, and runs a periodic timer to expire stale leases. The `call_local/1` function is the entry point used by the quorum protocol via `:rpc.call/4` — it forwards messages to the local GenServer.

The watch system filters events by the set of event types the subscriber registered for. This avoids sending irrelevant notifications (e.g., a subscriber interested only in `:released` events does not receive `:acquired` events).

```elixir
defmodule Locksmith.LockManager do
  @moduledoc """
  Per-node lock manager GenServer.

  Holds the local view of locks, processes quorum vote requests,
  and manages watch subscriptions. The quorum protocol calls
  `call_local/1` via `:rpc.call/4` to reach this GenServer on
  each node.
  """

  use GenServer

  @type state :: %{
    locks: %{String.t() => Locksmith.Lease.t()},
    queues: %{String.t() => [{integer(), GenServer.from(), pid()}]},
    watches: %{String.t() => [{pid(), list(atom())}]},
    epoch: integer(),
    token_counter: pos_integer(),
    holder_monitors: %{reference() => String.t()}
  }

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, Keyword.merge([name: __MODULE__], opts))
  end

  @spec call_local(term()) :: term()
  def call_local(message) do
    GenServer.call(__MODULE__, message, 5_000)
  end

  @spec watch(String.t(), pid(), list(atom())) :: :ok
  def watch(lock_name, subscriber_pid, event_types \\ [:acquired, :released, :expired]) do
    GenServer.cast(__MODULE__, {:watch, lock_name, subscriber_pid, event_types})
  end

  @spec unwatch(String.t(), pid()) :: :ok
  def unwatch(lock_name, subscriber_pid) do
    GenServer.cast(__MODULE__, {:unwatch, lock_name, subscriber_pid})
  end

  @impl true
  def init(_opts) do
    schedule_expiry_check()
    {:ok, %{
      locks: %{},
      queues: %{},
      watches: %{},
      epoch: 0,
      token_counter: 0,
      holder_monitors: %{}
    }}
  end

  @impl true
  def handle_call({:try_acquire, name, holder, ttl_ms, epoch}, _from, state) do
    case Map.get(state.locks, name) do
      nil ->
        {lease, new_state} = grant_lock(name, holder, ttl_ms, epoch, state)
        notify_watches(new_state, name, :acquired, lease)
        {:reply, {:granted, lease}, new_state}

      %Locksmith.Lease{} = existing ->
        if Locksmith.Lease.expired?(existing) do
          new_state = cleanup_expired_lock(name, existing, state)
          {lease, new_state} = grant_lock(name, holder, ttl_ms, epoch, new_state)
          notify_watches(new_state, name, :acquired, lease)
          {:reply, {:granted, lease}, new_state}
        else
          if Locksmith.Lease.held_by?(existing, holder) do
            renewed = %{existing | count: existing.count + 1}
            new_locks = Map.put(state.locks, name, renewed)
            {:reply, {:reentrant, renewed}, %{state | locks: new_locks}}
          else
            {:reply, :denied, state}
          end
        end
    end
  end

  @impl true
  def handle_call({:release, %Locksmith.Lease{name: name, holder: h, epoch: ep}}, _from, state) do
    case Map.get(state.locks, name) do
      %{holder: ^h, epoch: ^ep, count: 1} ->
        new_state = remove_lock(name, state)
        notify_watches(new_state, name, :released, nil)
        notify_next_waiter(new_state, name)
        {:reply, :ok, new_state}

      %{holder: ^h, epoch: ^ep} = lease ->
        updated = %{lease | count: lease.count - 1}
        {:reply, :ok, %{state | locks: Map.put(state.locks, name, updated)}}

      _ ->
        {:reply, {:error, :not_holder}, state}
    end
  end

  @impl true
  def handle_call({:renew, lease, ttl_ms}, _from, state) do
    case Map.get(state.locks, lease.name) do
      %{holder: h, epoch: ep} = existing
          when h == lease.holder and ep == lease.epoch ->
        renewed = %{existing | expires_at: System.monotonic_time(:millisecond) + ttl_ms}
        {:reply, {:ok, renewed}, %{state | locks: Map.put(state.locks, lease.name, renewed)}}

      _ ->
        {:reply, {:error, :expired}, state}
    end
  end

  @impl true
  def handle_info(:check_expiry, state) do
    now = System.monotonic_time(:millisecond)

    expired_names =
      state.locks
      |> Enum.filter(fn {_name, lease} -> lease.expires_at < now end)
      |> Enum.map(fn {name, _} -> name end)

    new_state = Enum.reduce(expired_names, state, fn name, acc ->
      notify_watches(acc, name, :expired, nil)
      notify_next_waiter(acc, name)
      remove_lock(name, acc)
    end)

    schedule_expiry_check()
    {:noreply, new_state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, _pid, _reason}, state) do
    case Map.get(state.holder_monitors, ref) do
      nil ->
        {:noreply, state}

      lock_name ->
        new_monitors = Map.delete(state.holder_monitors, ref)
        new_state = %{state | holder_monitors: new_monitors}

        case Map.get(new_state.locks, lock_name) do
          nil ->
            {:noreply, new_state}

          _lease ->
            notify_watches(new_state, lock_name, :released, nil)
            notify_next_waiter(new_state, lock_name)
            final_state = %{new_state | locks: Map.delete(new_state.locks, lock_name)}
            {:noreply, final_state}
        end
    end
  end

  @impl true
  def handle_cast({:watch, name, subscriber_pid, event_types}, state) do
    watchers = Map.get(state.watches, name, [])
    new_watchers = [{subscriber_pid, event_types} | watchers]
    {:noreply, %{state | watches: Map.put(state.watches, name, new_watchers)}}
  end

  @impl true
  def handle_cast({:unwatch, name, subscriber_pid}, state) do
    watchers =
      Map.get(state.watches, name, [])
      |> Enum.reject(fn {pid, _} -> pid == subscriber_pid end)

    {:noreply, %{state | watches: Map.put(state.watches, name, watchers)}}
  end

  # --- Private helpers ---

  defp grant_lock(name, holder, ttl_ms, epoch, state) do
    token = state.token_counter + 1
    lease = Locksmith.Lease.new(name, holder, ttl_ms, epoch, token)
    ref = Process.monitor(holder)
    new_locks = Map.put(state.locks, name, lease)
    new_monitors = Map.put(state.holder_monitors, ref, name)
    {lease, %{state | locks: new_locks, token_counter: token, holder_monitors: new_monitors}}
  end

  defp remove_lock(name, state) do
    monitor_ref =
      Enum.find(state.holder_monitors, fn {_ref, n} -> n == name end)

    new_monitors =
      case monitor_ref do
        {ref, _} ->
          Process.demonitor(ref, [:flush])
          Map.delete(state.holder_monitors, ref)
        nil ->
          state.holder_monitors
      end

    %{state | locks: Map.delete(state.locks, name), holder_monitors: new_monitors}
  end

  defp cleanup_expired_lock(name, _expired_lease, state) do
    remove_lock(name, state)
  end

  defp notify_watches(state, name, event, metadata) do
    watchers = Map.get(state.watches, name, [])

    Enum.each(watchers, fn {pid, event_types} ->
      if event in event_types do
        send(pid, {:lock_event, name, event, metadata})
      end
    end)
  end

  defp notify_next_waiter(state, name) do
    case Map.get(state.queues, name, []) do
      [] -> :ok
      [{_ts, from, _pid} | _rest] -> GenServer.reply(from, :retry)
    end
  end

  defp schedule_expiry_check do
    Process.send_after(self(), :check_expiry, 1_000)
  end
end
```

### Step 3: Quorum protocol

**Objective**: Require a quorum of prepare-acks before granting so a single node crash cannot strand a lock in an ambiguous state.


The quorum module broadcasts lock operations to all nodes in the cluster and requires a majority (2 of 3) to agree. When acquisition fails to reach quorum, it rolls back grants on nodes that did respond positively — each rollback uses the lease returned by that node, so the release is targeted and epoch-safe.

The `test_acquire_with_nodes/4` function exists for unit testing the quorum guard when you cannot control the actual node list.

```elixir
defmodule Locksmith.Quorum do
  @moduledoc """
  Quorum-based distributed lock protocol.

  Broadcasts lock operations to all connected nodes and requires
  a majority to agree. Uses `:rpc.call/4` to invoke each node's
  local LockManager.
  """

  @quorum_size 2

  @spec acquire(String.t(), pid(), pos_integer()) :: {:ok, Locksmith.Lease.t()} | {:error, atom()}
  def acquire(name, holder, ttl_ms \\ 30_000) do
    nodes = [node() | Node.list()]
    do_acquire(name, holder, ttl_ms, nodes, @quorum_size)
  end

  @doc "Testable variant that accepts an explicit node list and quorum size."
  @spec test_acquire_with_nodes(String.t(), pid(), [node()], pos_integer()) ::
    {:ok, Locksmith.Lease.t()} | {:error, atom()}
  def test_acquire_with_nodes(name, holder, nodes, quorum_size) do
    do_acquire(name, holder, 30_000, nodes, quorum_size)
  end

  @spec release(Locksmith.Lease.t()) :: :ok
  def release(lease) do
    nodes = [node() | Node.list()]
    broadcast({:release, lease}, nodes)
    :ok
  end

  @spec renew(Locksmith.Lease.t(), pos_integer()) :: {:ok, Locksmith.Lease.t()} | {:error, atom()}
  def renew(lease, ttl_ms \\ 30_000) do
    nodes = [node() | Node.list()]
    results = broadcast({:renew, lease, ttl_ms}, nodes)

    ok_results =
      Enum.filter(results, fn
        {:ok, _} -> true
        _ -> false
      end)

    if length(ok_results) >= @quorum_size do
      {:ok, _renewed_lease} = hd(ok_results)
    else
      {:error, :expired}
    end
  end

  # --- Private implementation ---

  defp do_acquire(name, holder, ttl_ms, nodes, quorum_size) do
    if length(nodes) < quorum_size do
      {:error, :insufficient_nodes}
    else
      epoch = System.monotonic_time(:millisecond)
      results = broadcast({:try_acquire, name, holder, ttl_ms, epoch}, nodes)

      node_results = Enum.zip(nodes, results)

      granted =
        Enum.filter(node_results, fn
          {_node, {:granted, _lease}} -> true
          {_node, {:reentrant, _lease}} -> true
          _ -> false
        end)

      if length(granted) >= quorum_size do
        lease_to_return =
          case Enum.find(granted, fn {_, {tag, _}} -> tag == :granted end) do
            {_, {:granted, l}} -> l
            nil ->
              {_, {:reentrant, l}} = hd(granted)
              l
          end

        {:ok, lease_to_return}
      else
        # Rollback: release on nodes that granted, using the lease each node returned
        Enum.each(granted, fn {rollback_node, {_tag, granted_lease}} ->
          Task.start(fn ->
            try do
              :rpc.call(rollback_node, Locksmith.LockManager, :call_local,
                [{:release, granted_lease}], 5_000)
            catch
              :exit, _ -> :ok
            end
          end)
        end)

        {:error, :locked}
      end
    end
  end

  defp broadcast(message, nodes) do
    nodes
    |> Enum.map(fn target_node ->
      Task.async(fn ->
        try do
          :rpc.call(target_node, Locksmith.LockManager, :call_local, [message], 5_000)
        catch
          :exit, _ -> {:error, :node_down}
        end
      end)
    end)
    |> Task.await_many(5_000)
  end
end
```

### Step 4: Heartbeat client

**Objective**: Detect failed peers via missed heartbeats within a bounded interval.


The heartbeat process runs on the lock holder's side. It periodically renews the lease before the TTL expires. If renewal fails (quorum lost, lease expired), it notifies the holder so the holder can stop performing protected work.

```elixir
defmodule Locksmith.Heartbeat do
  @moduledoc """
  Client-side heartbeat process that renews a lease before TTL expires.

  The heartbeat interval is set to 1/3 of the TTL, giving two chances
  to renew before expiry. If renewal fails, the holder is notified
  via a message so it can stop performing protected work.
  """

  use GenServer

  defstruct [:lease, :ttl_ms, :holder_pid, :interval_ms]

  @spec start_link(Locksmith.Lease.t(), pos_integer(), pid()) :: GenServer.on_start()
  def start_link(lease, ttl_ms, holder_pid) do
    interval_ms = div(ttl_ms, 3)
    GenServer.start_link(__MODULE__, %__MODULE__{
      lease: lease,
      ttl_ms: ttl_ms,
      holder_pid: holder_pid,
      interval_ms: interval_ms
    })
  end

  @spec stop(pid()) :: :ok
  def stop(pid), do: GenServer.stop(pid, :normal)

  @impl true
  def init(state) do
    schedule_heartbeat(state.interval_ms)
    {:ok, state}
  end

  @impl true
  def handle_info(:heartbeat, state) do
    case Locksmith.Quorum.renew(state.lease, state.ttl_ms) do
      {:ok, new_lease} ->
        schedule_heartbeat(state.interval_ms)
        {:noreply, %{state | lease: new_lease}}

      {:error, reason} ->
        send(state.holder_pid, {:lease_lost, state.lease.name, reason})
        {:stop, :normal, state}
    end
  end

  defp schedule_heartbeat(interval_ms) do
    Process.send_after(self(), :heartbeat, interval_ms)
  end
end
```

### Step 5: Leader election

**Objective**: Randomize election timeouts so split votes resolve within one term rather than livelocking under symmetric timer skew.


The election process uses the lock service to elect a leader among a set of candidate processes. It acquires a well-known lock name, and the holder becomes the leader. Watches provide instant notification when the leader's lock is released or expires, triggering a new election round.

```elixir
defmodule Locksmith.Election do
  @moduledoc """
  Leader election built on top of the quorum lock service.

  Each candidate tries to acquire a well-known lock. The winner
  becomes the leader and maintains leadership via heartbeat renewal.
  Watch notifications trigger re-election when the leader's lock
  is released or expires.
  """

  use GenServer

  @election_lock "leader-election"
  @ttl_ms 10_000
  @heartbeat_interval_ms 3_000

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  @spec role(pid()) :: :leader | :follower | :candidate
  def role(pid), do: GenServer.call(pid, :get_role)

  @impl true
  def init(opts) do
    candidates = Keyword.fetch!(opts, :candidates)
    me = self()
    Locksmith.LockManager.watch(@election_lock, me, [:acquired, :released, :expired])
    schedule_attempt()
    {:ok, %{candidates: candidates, role: :candidate, leader: nil, lease: nil}}
  end

  @impl true
  def handle_call(:get_role, _from, state) do
    {:reply, state.role, state}
  end

  @impl true
  def handle_info(:attempt_election, state) do
    case Locksmith.Quorum.acquire(@election_lock, self(), @ttl_ms) do
      {:ok, lease} ->
        announce_leadership(state.candidates)
        schedule_heartbeat()
        {:noreply, %{state | role: :leader, lease: lease}}

      {:error, _} ->
        {:noreply, state}
    end
  end

  @impl true
  def handle_info({:lock_event, @election_lock, event, _meta}, state)
      when event in [:released, :expired] do
    schedule_attempt()
    {:noreply, %{state | role: :candidate, leader: nil}}
  end

  @impl true
  def handle_info({:lock_event, @election_lock, :acquired, lease}, state) do
    if lease.holder == self() do
      {:noreply, state}
    else
      notify_self_follower(lease.holder)
      {:noreply, %{state | role: :follower, leader: lease.holder}}
    end
  end

  @impl true
  def handle_info(:heartbeat, %{role: :leader, lease: lease} = state) do
    case Locksmith.Quorum.renew(lease, @ttl_ms) do
      {:ok, new_lease} ->
        schedule_heartbeat()
        {:noreply, %{state | lease: new_lease}}

      {:error, :expired} ->
        schedule_attempt()
        {:noreply, %{state | role: :candidate, lease: nil}}
    end
  end

  @impl true
  def handle_info(:heartbeat, state) do
    {:noreply, state}
  end

  defp announce_leadership(candidates) do
    Enum.each(candidates, fn pid ->
      send(pid, {:election_result, :leader, self()})
    end)
  end

  defp notify_self_follower(leader_pid) do
    send(self(), {:election_result, :follower, leader_pid})
  end

  defp schedule_attempt, do: Process.send_after(self(), :attempt_election, 100)
  defp schedule_heartbeat, do: Process.send_after(self(), :heartbeat, @heartbeat_interval_ms)
end
```

### Step 6: Fencing token validation

**Objective**: Reject writes whose fencing token is below the stored epoch so delayed clients cannot corrupt protected state.


Downstream storage systems use fencing tokens to reject writes from stale lock holders. When a lock is acquired, the lease includes a monotonically increasing token. The storage layer records the highest token it has seen for each lock name and rejects any write with a lower token.

```elixir
defmodule Locksmith.Fencing do
  @moduledoc """
  Fencing token validation for downstream storage systems.

  Maintains a mapping of lock_name -> highest_seen_token. Any write
  attempt with a token lower than the highest seen is rejected,
  preventing stale holders from corrupting data after their lease
  has expired and been re-granted to another process.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, Keyword.merge([name: __MODULE__], opts))
  end

  @spec validate_token(String.t(), pos_integer()) :: :ok | {:error, :stale_token}
  def validate_token(lock_name, token) do
    GenServer.call(__MODULE__, {:validate, lock_name, token})
  end

  @impl true
  def init(_opts) do
    {:ok, %{tokens: %{}}}
  end

  @impl true
  def handle_call({:validate, lock_name, token}, _from, state) do
    highest = Map.get(state.tokens, lock_name, 0)

    if token >= highest do
      new_tokens = Map.put(state.tokens, lock_name, token)
      {:reply, :ok, %{state | tokens: new_tokens}}
    else
      {:reply, {:error, :stale_token}, state}
    end
  end
end
```

### Step 7: Public API

**Objective**: Funnel acquire, renew, and release through one module so callers cannot skip quorum or fencing validation paths.


The `Locksmith` module provides a clean public interface that wraps the quorum protocol and heartbeat management.

```elixir
defmodule Locksmith do
  @moduledoc """
  Public API for the distributed lock service.

  Provides acquire/release/renew/watch operations backed by
  quorum-based distributed consensus.
  """

  @spec acquire(String.t(), keyword()) :: {:ok, Locksmith.Lease.t()} | {:error, atom()}
  def acquire(lock_name, opts \\ []) do
    ttl_ms = Keyword.get(opts, :ttl_ms, 30_000)
    holder = Keyword.get(opts, :holder, self())
    Locksmith.Quorum.acquire(lock_name, holder, ttl_ms)
  end

  @spec release(Locksmith.Lease.t()) :: :ok
  def release(lease) do
    Locksmith.Quorum.release(lease)
  end

  @spec renew(Locksmith.Lease.t(), keyword()) :: {:ok, Locksmith.Lease.t()} | {:error, atom()}
  def renew(lease, opts \\ []) do
    ttl_ms = Keyword.get(opts, :ttl_ms, 30_000)
    Locksmith.Quorum.renew(lease, ttl_ms)
  end

  @spec watch(String.t(), pid(), list(atom())) :: :ok
  def watch(lock_name, subscriber \\ self(), events \\ [:acquired, :released, :expired]) do
    Locksmith.LockManager.watch(lock_name, subscriber, events)
  end
end
```

### Why this works

The design isolates correctness-critical invariants from latency-critical paths and from evolution-critical contracts. Modules expose narrow interfaces and fail fast on contract violations, so bugs surface close to their source. Tests target invariants rather than implementation details, so refactors don't produce false alarms. The trade-offs are explicit in the Design decisions section, which makes the "why" auditable instead of folklore.

## Given tests

```elixir
# test/concurrent_test.exs
defmodule Locksmith.ConcurrentTest do
  use ExUnit.Case, async: false
  alias Locksmith.Quorum


  describe "Concurrent" do

  test "exactly one of N concurrent acquire attempts succeeds" do
    lock_name = "test-lock-#{System.unique_integer()}"
    n = 20
    me = self()

    pids = Enum.map(1..n, fn _ ->
      spawn(fn ->
        result = Quorum.acquire(lock_name, self(), 5_000)
        send(me, result)
      end)
    end)

    results = Enum.map(1..n, fn _ ->
      receive do
        r -> r
      after 3000 -> {:error, :timeout}
      end
    end)

    ok_count = Enum.count(results, fn
      {:ok, _} -> true
      _ -> false
    end)

    assert ok_count == 1, "Expected exactly 1 success, got #{ok_count}"
  end

  test "lock is released when holder process dies" do
    lock_name = "test-expire-#{System.unique_integer()}"

    holder = spawn(fn ->
      {:ok, lease} = Quorum.acquire(lock_name, self(), 30_000)
      receive do
        :hold -> :ok
      end
    end)

    Process.sleep(50)
    assert {:error, :locked} = Quorum.acquire(lock_name, self(), 5_000)

    Process.exit(holder, :kill)
    Process.sleep(1_100)

    assert {:ok, _lease} = Quorum.acquire(lock_name, self(), 5_000)
  end

  test "reentrant: same process can acquire twice, needs two releases" do
    lock_name = "test-reentrant-#{System.unique_integer()}"
    {:ok, lease1} = Quorum.acquire(lock_name, self(), 30_000)
    {:ok, lease2} = Quorum.acquire(lock_name, self(), 30_000)

    assert :ok = Quorum.release(lease2)
    assert {:error, :locked} = Task.async(fn -> Quorum.acquire(lock_name, self(), 1_000) end) |> Task.await()

    assert :ok = Quorum.release(lease1)
    Process.sleep(50)
    assert {:ok, _} = Quorum.acquire(lock_name, self(), 5_000)
  end
end

# test/watch_test.exs
defmodule Locksmith.WatchTest do
  use ExUnit.Case, async: false

  test "watch receives :acquired event when lock is taken" do
    lock_name = "watch-test-#{System.unique_integer()}"
    Locksmith.LockManager.watch(lock_name, self(), [:acquired, :released])
    Locksmith.Quorum.acquire(lock_name, self(), 30_000)
    assert_receive {:lock_event, ^lock_name, :acquired, _}, 500
  end

  test "watch receives :released event when lock is freed" do
    lock_name = "watch-release-#{System.unique_integer()}"
    Locksmith.LockManager.watch(lock_name, self(), [:released])
    {:ok, lease} = Locksmith.Quorum.acquire(lock_name, self(), 30_000)
    Locksmith.Quorum.release(lease)
    assert_receive {:lock_event, ^lock_name, :released, _}, 500
  end

  test "watch receives :expired event when TTL passes" do
    lock_name = "watch-expire-#{System.unique_integer()}"
    Locksmith.LockManager.watch(lock_name, self(), [:expired])
    Locksmith.Quorum.acquire(lock_name, self(), 500)
    assert_receive {:lock_event, ^lock_name, :expired, _}, 2_000
  end
end

# test/quorum_test.exs
defmodule Locksmith.QuorumTest do
  use ExUnit.Case, async: false

  test "acquire fails if quorum cannot be reached (insufficient nodes)" do
    assert {:error, :insufficient_nodes} =
      Locksmith.Quorum.test_acquire_with_nodes("test-quorum", self(), [], 2)
  end

  test "renew succeeds before TTL expires" do
    name = "renew-test-#{System.unique_integer()}"
    {:ok, lease} = Locksmith.Quorum.acquire(name, self(), 5_000)
    Process.sleep(100)
    assert {:ok, new_lease} = Locksmith.Quorum.renew(lease, 5_000)
    assert new_lease.expires_at > lease.expires_at
  end


  end
end
```

## Benchmark

```elixir
# bench/contention.exs
defmodule Locksmith.Bench.Contention do
  @lock_count 100
  @holders_per_lock 10
  @ttl_ms 5_000

  def run do
    IO.puts("Benchmark: #{@lock_count} locks, #{@holders_per_lock} concurrent acquires each")

    {time_us, results} = :timer.tc(fn ->
      tasks = for lock_i <- 1..@lock_count do
        Task.async(fn ->
          name = "bench-lock-#{lock_i}"
          times = for _ <- 1..@holders_per_lock do
            {us, result} = :timer.tc(fn -> Locksmith.Quorum.acquire(name, self(), @ttl_ms) end)
            case result do
              {:ok, lease} -> Locksmith.Quorum.release(lease)
              _ -> :ok
            end
            {result, us}
          end
          times
        end)
      end
      Task.await_many(tasks, 30_000) |> List.flatten()
    end)

    oks = Enum.count(results, fn {r, _} -> match?({:ok, _}, r) end)
    errors = Enum.count(results, fn {r, _} -> match?({:error, _}, r) end)
    latencies = Enum.map(results, fn {_, us} -> us / 1000.0 end)
    sorted = Enum.sort(latencies)
    n = length(sorted)
    p50 = Enum.at(sorted, div(n, 2))
    p99 = Enum.at(sorted, trunc(n * 0.99))

    IO.puts("Success: #{oks}, Errors: #{errors}")
    IO.puts("P50 acquire latency: #{Float.round(p50, 2)} ms")
    IO.puts("P99 acquire latency: #{Float.round(p99, 2)} ms")
    IO.puts("Total time: #{Float.round(time_us / 1000, 0)} ms")
  end
end

Locksmith.Bench.Contention.run()
def main do
  IO.puts("[Locksmith.Heartbeat] GenServer demo")
  :ok
end

```

## Key Concepts: Consensus and Distributed Agreement

The core challenge in distributed systems is reaching agreement across multiple nodes when some may fail, be slow, or partition from the network. Consensus algorithms formalize three properties:

1. **Safety**: All nodes that decide must decide the same value.
2. Liveness**: Every non-faulty node eventually decides.
3. Fault tolerance**: The system tolerates up to F faulty nodes out of 2F+1 total.

Raft achieves this via a leader-based approach: the leader serializes writes through a log, and quorum commit ensures no data loss across failures. The log-up-to-date vote rule prevents stale nodes from becoming leader, and the "commit only current-term entries" rule prevents committed entries from being overwritten.

This contrasts with leaderless protocols (e.g., CRDTs) that sacrifice strong consistency for eventual consistency, enabling offline-first systems. For the BEAM, Raft fits naturally into the GenServer + OTP supervision model: each node is a GenServer with local state (log, term, vote), and RPCs are asynchronous messages that do not block the caller.

**Production insight**: Raft's safety depends on three invariants holding simultaneously. A single violated invariant (e.g., committing an entry from a previous term by index alone) causes data loss on specific failure patterns that may never surface in testing. This is why production systems use formal verification or extensive failure injection (Jepsen tests) to validate safety, not just positive test cases.

---

## Trade-off analysis

| Design choice | Selected | Alternative | Trade-off |
|---|---|---|---|
| Quorum protocol | Majority vote (2/3) | All-node agreement | All-node: simpler; unavailable if any node down — unacceptable for HA |
| Lock expiry | Lease with heartbeat | Eternal lock + process monitor | Monitor: instant detection within-cluster; lease: handles network partition where monitor doesn't fire |
| FIFO queue | Timestamp-ordered waiters in LockManager | First-come-first-served via randomized retry | Random retry: simpler; starvation possible under high contention |
| Fencing token | Monotonic integer per lock | No fencing | No fencing: double-write possible if old holder acts after expiry; token: storage backend can reject stale writes |
| Split-brain detection | Node count check before acquire | None | None: two nodes each grant a lock during partition; count check: sacrifices availability on partition side |

## Common production mistakes

**Using wall-clock time for TTL.** Different BEAM nodes may have different system clocks (NTP drift up to ~100ms). A lock acquired with TTL 30s based on node A's clock may expire after 29.9s from node B's perspective. Use monotonic time within each node for expiry checks, and accept ±500ms inaccuracy in cross-node TTL comparisons. Critical operations should use a grace period.

**Not using fencing tokens for downstream operations.** A process holds a lock, pauses (GC, network delay), the lock expires, another process acquires it, and then the original process resumes and writes. Without a fencing token, both processes successfully write, corrupting data. Downstream storage must reject writes with a token less than the last seen token for that lock name.

**Allowing quorum rollback without idempotency.** If `acquire` succeeds on nodes A and B but fails on C, the rollback sends `release` to A and B. If the rollback message to B is lost (network), B still thinks the lock is held. The next legitimate acquirer is denied because B has a phantom lock. Use an epoch-tagged release: if the epoch matches, release; if not (new lock exists), ignore the rollback.

**Not testing split-brain explicitly.** The split-brain scenario requires two separate clusters each with a majority. In a test, simulate this by starting 4 nodes and using `:net_kernel.disconnect/1` to partition {A, B} from {C, D}. Verify that {A, B} can grant a lock and {C, D} can grant the same lock (it should fail in your quorum implementation but passing this test is what validates the design).

**Storing watch subscriptions only on one node.** If a watcher is on node A and the lock is managed by node B, the watch event must reach node A. Use `send(watcher_pid, event)` directly — BEAM message passing is location-transparent across connected nodes. But if the network is partitioned, the event may be dropped. Buffer events in the LockManager for N seconds and replay on reconnect.

## Reflection

A client acquires a lock, GC-pauses for 30 seconds, then wakes up and mutates the resource. Your lease was 10s. What prevents the pause-and-write from corrupting state, and what happens if the client ignores fencing?

## Resources

- Hunt et al. — "ZooKeeper: Wait-free Coordination for Internet-scale Systems" (2010) — USENIX ATC
- Burrows — "The Chubby Lock Service for Loosely-Coupled Distributed Systems" (2006) — OSDI
- Kleppmann — "How to do distributed locking" — https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html
- Kleppmann — "Designing Data-Intensive Applications" Chapters 8–9 (clocks, distributed system problems)
- Takada — "Distributed Systems for Fun and Profit" — http://book.mixu.net/distsys/ (free online)
- Erlang `:global` module source — `lib/kernel/src/global.erl` in OTP (reference implementation)
