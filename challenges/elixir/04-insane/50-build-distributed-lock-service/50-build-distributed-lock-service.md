# 50. Build a Distributed Lock Service (ZooKeeper-like)

## Context

Your team runs a three-node Elixir cluster. A background job — reindexing a large catalog — must not run on more than one node simultaneously. Using `:global.register_name/2` worked in development but caused a split-brain incident in production: after a network hiccup, both nodes thought they were the sole index job runner and both completed, writing conflicting data.

The problem with `:global`: it uses two-phase locking with no progress guarantee under partition. A partial network failure can leave both sides of a partition believing they hold the lock.

You will build `Locksmith`: a quorum-based distributed lock service where acquiring a lock requires agreement from a majority (2 of 3) of nodes. Locks are lease-based with TTL: a dead holder's lock expires automatically. Watches enable leader-election without polling.

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

## Step 1 — Lease and epoch

```elixir
defmodule Locksmith.Lease do
  @enforce_keys [:name, :holder, :holder_node, :expires_at, :epoch, :token]
  defstruct [:name, :holder, :holder_node, :expires_at, :epoch, :token, count: 1]

  @doc "Create a new lease. Token is a monotonically increasing fencing token."
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

  def expired?(%__MODULE__{expires_at: exp}) do
    System.monotonic_time(:millisecond) > exp
  end

  def held_by?(%__MODULE__{holder: h, holder_node: n}, pid) do
    h == pid and n == node()
  end
end
```

## Step 2 — Lock manager (per node)

```elixir
defmodule Locksmith.LockManager do
  use GenServer

  # State:
  # %{
  #   locks: %{name => Lease.t()},      # currently held locks
  #   queues: %{name => [{ts, from, pid}]},  # FIFO waiters
  #   watches: %{name => [pid]},         # watch subscribers
  #   epoch: integer(),                  # monotonically increasing per node
  #   token_counter: integer()           # fencing token counter (global monotonic)
  # }

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, Keyword.merge([name: __MODULE__], opts))
  end

  def init(_opts) do
    schedule_expiry_check()
    {:ok, %{locks: %{}, queues: %{}, watches: %{}, epoch: 0, token_counter: 0}}
  end

  @doc "Attempt to acquire a lock locally. Returns :granted or :denied or :reentrant."
  def handle_call({:try_acquire, name, holder, ttl_ms, epoch}, _from, state) do
    case Map.get(state.locks, name) do
      nil ->
        # Lock is free
        token = state.token_counter + 1
        lease = Locksmith.Lease.new(name, holder, ttl_ms, epoch, token)
        new_locks = Map.put(state.locks, name, lease)
        notify_watches(state, name, :acquired, lease)
        {:reply, {:granted, lease}, %{state | locks: new_locks, token_counter: token}}

      %{} = existing when not Locksmith.Lease.expired?(existing) ->
        if Locksmith.Lease.held_by?(existing, holder) do
          # Reentrant: same holder
          renewed = %{existing | count: existing.count + 1}
          {:reply, {:reentrant, renewed}, %{state | locks: Map.put(state.locks, name, renewed)}}
        else
          # Lock held by someone else
          {:reply, :denied, state}
        end

      %{} ->
        # Lock exists but expired — grant
        token = state.token_counter + 1
        lease = Locksmith.Lease.new(name, holder, ttl_ms, epoch, token)
        new_locks = Map.put(state.locks, name, lease)
        notify_watches(state, name, :acquired, lease)
        {:reply, {:granted, lease}, %{state | locks: new_locks, token_counter: token}}
    end
  end

  @doc "Release a lock. Returns :ok or :error (not holder or wrong epoch)."
  def handle_call({:release, %Locksmith.Lease{name: name, holder: h, epoch: ep}}, _from, state) do
    case Map.get(state.locks, name) do
      %{holder: ^h, epoch: ^ep, count: 1} ->
        new_locks = Map.delete(state.locks, name)
        notify_watches(state, name, :released, nil)
        notify_next_waiter(state, name)
        {:reply, :ok, %{state | locks: new_locks}}
      %{holder: ^h, epoch: ^ep} = lease ->
        # Reentrant: decrement count
        updated = %{lease | count: lease.count - 1}
        {:reply, :ok, %{state | locks: Map.put(state.locks, name, updated)}}
      _ ->
        {:reply, {:error, :not_holder}, state}
    end
  end

  @doc "Renew a lease. Returns {:ok, new_lease} or {:error, :expired}."
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

  # Periodic expiry check
  def handle_info(:check_expiry, state) do
    now = System.monotonic_time(:millisecond)
    expired_names = state.locks
      |> Enum.filter(fn {_name, lease} -> lease.expires_at < now end)
      |> Enum.map(fn {name, _} -> name end)

    new_locks = Enum.reduce(expired_names, state.locks, fn name, locks ->
      notify_watches(state, name, :expired, nil)
      notify_next_waiter(state, name)
      Map.delete(locks, name)
    end)

    schedule_expiry_check()
    {:noreply, %{state | locks: new_locks}}
  end

  # Watch registration
  def handle_cast({:watch, name, subscriber_pid}, state) do
    watchers = Map.get(state.watches, name, [])
    {:noreply, %{state | watches: Map.put(state.watches, name, [subscriber_pid | watchers])}}
  end

  def handle_cast({:unwatch, name, subscriber_pid}, state) do
    watchers = Map.get(state.watches, name, []) |> List.delete(subscriber_pid)
    {:noreply, %{state | watches: Map.put(state.watches, name, watchers)}}
  end

  defp notify_watches(state, name, event, metadata) do
    watchers = Map.get(state.watches, name, [])
    Enum.each(watchers, fn pid ->
      send(pid, {:lock_event, name, event, metadata})
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

## Step 3 — Quorum protocol

```elixir
defmodule Locksmith.Quorum do
  @quorum_size 2

  @doc "Acquire a lock with quorum. Returns {:ok, lease} or {:error, reason}."
  def acquire(name, holder, ttl_ms \\ 30_000) do
    nodes = [node() | Node.list()]
    if length(nodes) < @quorum_size do
      {:error, :insufficient_nodes}
    else
      epoch = :erlang.monotonic_time(:millisecond)
      results = broadcast({:try_acquire, name, holder, ttl_ms, epoch}, nodes)
      granted = Enum.filter(results, fn
        {:granted, _lease} -> true
        {:reentrant, _lease} -> true
        _ -> false
      end)
      if length(granted) >= @quorum_size do
        # Use the lease from the responding quorum majority
        {:granted, lease} = hd(granted)
        # TODO: monitor holder process for automatic release on death
        Process.monitor(holder)
        {:ok, lease}
      else
        # Rollback: release on nodes that granted
        Enum.each(granted, fn {:granted, lease} ->
          # TODO: find which node returned this and release there
          :ok
        end)
        {:error, :locked}
      end
    end
  end

  @doc "Release a lock with quorum notification."
  def release(lease) do
    nodes = [node() | Node.list()]
    broadcast({:release, lease}, nodes)
    :ok
  end

  @doc "Renew a lease with quorum. A majority must acknowledge."
  def renew(lease, ttl_ms \\ 30_000) do
    nodes = [node() | Node.list()]
    results = broadcast({:renew, lease, ttl_ms}, nodes)
    ok_count = Enum.count(results, fn
      {:ok, _} -> true
      _ -> false
    end)
    if ok_count >= @quorum_size do
      {:ok, elem(hd(results), 1)}
    else
      {:error, :expired}
    end
  end

  defp broadcast(message, nodes) do
    nodes
    |> Enum.map(fn node ->
      Task.async(fn ->
        try do
          :rpc.call(node, Locksmith.LockManager, :call_local, [message], 5_000)
        catch
          :exit, _ -> {:error, :node_down}
        end
      end)
    end)
    |> Task.await_many(5_000)
  end
end
```

## Step 4 — Leader election

```elixir
defmodule Locksmith.Election do
  use GenServer

  @election_lock "leader-election"
  @ttl_ms 10_000
  @heartbeat_interval_ms 3_000

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts)
  end

  def init(opts) do
    candidates = Keyword.fetch!(opts, :candidates)
    me = self()
    Locksmith.LockManager.watch(@election_lock, me, [:acquired, :released, :expired])
    schedule_attempt()
    {:ok, %{candidates: candidates, role: :candidate, leader: nil, lease: nil}}
  end

  def handle_info(:attempt_election, state) do
    case Locksmith.Quorum.acquire(@election_lock, self(), @ttl_ms) do
      {:ok, lease} ->
        announce_leadership(state.candidates)
        schedule_heartbeat()
        {:noreply, %{state | role: :leader, lease: lease}}
      {:error, _} ->
        # Will be notified via watch when lock is released
        {:noreply, state}
    end
  end

  def handle_info({:lock_event, @election_lock, event, _meta}, state)
      when event in [:released, :expired] do
    # Compete again
    schedule_attempt()
    {:noreply, %{state | role: :candidate, leader: nil}}
  end

  def handle_info({:lock_event, @election_lock, :acquired, lease}, state) do
    if lease.holder == self() do
      {:noreply, state}
    else
      # Someone else is leader
      notify_self_follower(lease.holder)
      {:noreply, %{state | role: :follower, leader: lease.holder}}
    end
  end

  def handle_info(:heartbeat, %{role: :leader, lease: lease} = state) do
    case Locksmith.Quorum.renew(lease, @ttl_ms) do
      {:ok, new_lease} ->
        schedule_heartbeat()
        {:noreply, %{state | lease: new_lease}}
      {:error, :expired} ->
        # Lost leadership; re-enter as candidate
        schedule_attempt()
        {:noreply, %{state | role: :candidate, lease: nil}}
    end
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

## Given tests

```elixir
# test/concurrent_test.exs
defmodule Locksmith.ConcurrentTest do
  use ExUnit.Case, async: false
  alias Locksmith.Quorum

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

    # Acquire lock in a spawned process, then kill it
    holder = spawn(fn ->
      {:ok, lease} = Quorum.acquire(lock_name, self(), 30_000)
      receive do
        :hold -> :ok
      end
    end)

    Process.sleep(50)
    # Verify lock is held
    assert {:error, :locked} = Quorum.acquire(lock_name, self(), 5_000)

    # Kill holder — TTL expiry or monitor should release
    Process.exit(holder, :kill)
    Process.sleep(1_100)  # Wait for expiry check (1s interval)

    assert {:ok, _lease} = Quorum.acquire(lock_name, self(), 5_000)
  end

  test "reentrant: same process can acquire twice, needs two releases" do
    lock_name = "test-reentrant-#{System.unique_integer()}"
    {:ok, lease1} = Quorum.acquire(lock_name, self(), 30_000)
    {:ok, lease2} = Quorum.acquire(lock_name, self(), 30_000)

    # First release decrements count
    assert :ok = Quorum.release(lease2)
    # Lock still held (count == 1)
    assert {:error, :locked} = Task.async(fn -> Quorum.acquire(lock_name, self(), 1_000) end) |> Task.await()

    # Second release frees the lock
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
    Locksmith.Quorum.acquire(lock_name, self(), 500)  # 500ms TTL
    assert_receive {:lock_event, ^lock_name, :expired, _}, 2_000
  end
end

# test/quorum_test.exs
defmodule Locksmith.QuorumTest do
  use ExUnit.Case, async: false

  test "acquire fails if quorum cannot be reached (insufficient nodes)" do
    # Simulate a 1-node cluster (only self, no peers)
    # With quorum_size=2, this should fail
    original_nodes = Node.list()
    # Cannot disconnect nodes in a unit test; instead test the guard directly
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
```

## Trade-offs

| Design choice | Selected | Alternative | Trade-off |
|---|---|---|---|
| Quorum protocol | Majority vote (2/3) | All-node agreement | All-node: simpler; unavailable if any node down — unacceptable for HA |
| Lock expiry | Lease with heartbeat | Eternal lock + process monitor | Monitor: instant detection within-cluster; lease: handles network partition where monitor doesn't fire |
| FIFO queue | Timestamp-ordered waiters in LockManager | First-come-first-served via randomized retry | Random retry: simpler; starvation possible under high contention |
| Fencing token | Monotonic integer per lock | No fencing | No fencing: double-write possible if old holder acts after expiry; token: storage backend can reject stale writes |
| Split-brain detection | Node count check before acquire | None | None: two nodes each grant a lock during partition; count check: sacrifices availability on partition side |

## Production mistakes

**Using wall-clock time for TTL.** Different BEAM nodes may have different system clocks (NTP drift up to ~100ms). A lock acquired with TTL 30s based on node A's clock may expire after 29.9s from node B's perspective. Use monotonic time within each node for expiry checks, and accept ±500ms inaccuracy in cross-node TTL comparisons. Critical operations should use a grace period.

**Not using fencing tokens for downstream operations.** A process holds a lock, pauses (GC, network delay), the lock expires, another process acquires it, and then the original process resumes and writes. Without a fencing token, both processes successfully write, corrupting data. Downstream storage must reject writes with a token less than the last seen token for that lock name.

**Allowing quorum rollback without idempotency.** If `acquire` succeeds on nodes A and B but fails on C, the rollback sends `release` to A and B. If the rollback message to B is lost (network), B still thinks the lock is held. The next legitimate acquirer is denied because B has a phantom lock. Use an epoch-tagged release: if the epoch matches, release; if not (new lock exists), ignore the rollback.

**Not testing split-brain explicitly.** The split-brain scenario requires two separate clusters each with a majority. In a test, simulate this by starting 4 nodes and using `:net_kernel.disconnect/1` to partition {A, B} from {C, D}. Verify that {A, B} can grant a lock and {C, D} can grant the same lock (it should fail in your quorum implementation but passing this test is what validates the design).

**Storing watch subscriptions only on one node.** If a watcher is on node A and the lock is managed by node B, the watch event must reach node A. Use `send(watcher_pid, event)` directly — BEAM message passing is location-transparent across connected nodes. But if the network is partitioned, the event may be dropped. Buffer events in the LockManager for N seconds and replay on reconnect.

## Resources

- Hunt et al. — "ZooKeeper: Wait-free Coordination for Internet-scale Systems" (2010) — USENIX ATC
- Burrows — "The Chubby Lock Service for Loosely-Coupled Distributed Systems" (2006) — OSDI
- Kleppmann — "How to do distributed locking" — https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html
- Kleppmann — "Designing Data-Intensive Applications" Chapters 8–9 (clocks, distributed system problems)
- Takada — "Distributed Systems for Fun and Profit" — http://book.mixu.net/distsys/ (free online)
- Erlang `:global` module source — `lib/kernel/src/global.erl` in OTP (reference implementation)
