# Distributed Lock Service (ZooKeeper-like)

**Project**: `locksmith` — Distributed lock manager using quorum writes and lease-based TTL, tolerating single-node failure in a 3-node cluster.

---

## Project context

Your team runs a three-node Elixir cluster. A background job — reindexing a large catalog — must not run on more than one node simultaneously. Using `:global.register_name/2` worked in development but caused a split-brain incident in production: after a network hiccup, both nodes thought they were the sole index job runner and both completed, writing conflicting data.

The problem with `:global`: it uses two-phase locking with no progress guarantee under partition. A partial network failure can leave both sides of a partition believing they hold the lock.

You will build `Locksmith`: a quorum-based distributed lock service where acquiring a lock requires agreement from a majority (2 of 3) of nodes. Locks are lease-based with TTL: a dead holder's lock expires automatically. Watches enable leader-election without polling.

---

## The problem

Distributed locks are necessary when multiple processes (on different nodes) must coordinate exclusive access to a resource. The naive approach — one lock manager on one node — creates a SPOF: if the lock manager crashes, locks cannot be acquired or released.

Distributed locks are hard because:
1. **Network partitions**: two sides of a partition may each think they hold the lock.
2. **Process crashes**: a lock holder crashes; the lock is held forever unless there's a TTL.
3. **Clock skew**: node A's clock is ahead of node B's, so a "30-second TTL" expires at different times.
4. **Delayed clients**: a lock holder is slow (GC pause) but technically still alive, and another process acquires the lock and starts writing. No mutual exclusion.

---

## Why this design

**Quorum-based consensus**: acquiring a lock requires a majority of nodes (2 of 3) to agree. This prevents split-brain: if the cluster partitions into {A, B} and {C}, only {A, B} can grant locks because they have the majority. {C} cannot grant locks because it's a minority.

**Lease-based TTL**: a lock holder must periodically renew the lease. If the holder crashes or is partitioned, the lease expires and the lock is released. Without TTL, locks are held forever.

**Watches**: instead of polling to check if a lock is available, subscribers register a watch. When the lock is released or expires, all watchers are notified immediately. This enables leader election with sub-second failover.

**Fencing tokens**: to prevent a delayed lock holder from corrupting data, every lease includes a monotonically increasing token. The storage system tracks the highest token it has seen and rejects writes with a lower token.

---

## Design decisions

**Option A — single-leader lock manager**
- Pros: trivially linearizable, simple to reason about.
- Cons: leader is a SPOF and a bottleneck.

**Option B — Raft-replicated lock state with lease-based client leases** (chosen)
- Pros: HA, linearizable, leases survive client crashes.
- Cons: Raft adds latency on every lock acquire.

→ Chose **B** because a lock service that loses locks on node failure is worse than no lock service — HA is mandatory.

---

## Key Concepts: Distributed Consensus and Mutual Exclusion

Mutual exclusion ensures that at most one process holds a resource at a time. In a distributed system, this requires agreement across multiple nodes — which is the consensus problem.

**Quorum-based consensus**: A quorum is a subset of nodes such that any two quorums share at least one node in common. For N=3 nodes, a quorum is 2 nodes. The insight: if node A grants a lock to process X with quorum {A, B}, and node C tries to grant the same lock to process Y, node C must contact at least one of {A, B} — and that node will report the lock is held.

**Lease-based TTL**: A lease is time-bounded, unlike a traditional lock that is held until explicitly released. If the holder crashes, the lease expires. The trade-off: if the holder is alive but temporarily partitioned (network partition, GC pause), the lease may expire and the lock may be granted to another process. To handle this, the original holder must check that its lease is still valid before acting on it — this is "fencing tokens."

**Fencing tokens**: When a process acquires a lock, it receives a token (monotonically increasing). Before the process writes to a protected resource, it includes the token. The resource (e.g., a file, a database record) rejects any write with a token less than the highest token it has seen. This prevents a delayed or stale lock holder from overwriting newer data.

**Production insight**: Correct distributed locking is non-obvious. Many systems get it wrong:
- etcd uses Raft but doesn't validate fencing tokens downstream (your application must check).
- ZooKeeper uses quorum consensus correctly but requires applications to use watches properly.
- Redis with TTL is not safe for critical locks (no fencing, relies on clock accuracy).

---

## Trade-off analysis

| Design choice | Selected | Alternative | Trade-off |
|---|---|---|---|
| Quorum protocol | Majority (2/3) | All-node agreement | All-node: simpler, unavailable if any node down — unacceptable for HA |
| Lock expiry | Lease with heartbeat | Eternal lock + process monitor | Monitor: instant detection within-cluster; lease: handles network partition where monitor doesn't fire |
| Waiter queue | Timestamp-ordered waiters | First-come-first-served via randomized retry | Random retry: simpler; starvation possible under high contention |
| Fencing token | Monotonic integer per lock | No fencing | No fencing: double-write possible if old holder acts after expiry |
| Watch notifications | Synchronous to subscribers | Async with queuing | Async: decouples watchers, better if many watchers; sync: simpler, lock release is slower |

---

## Common production mistakes

**1. Using wall-clock time for TTL**

Different BEAM nodes may have different system clocks (NTP drift up to ~100ms). A lock acquired with TTL 30s based on node A's clock may expire after 29.9s from node B's perspective. Use monotonic time within each node for expiry checks, and accept ±500ms inaccuracy in cross-node TTL comparisons.

Mitigation: use `System.monotonic_time(:millisecond)` and add a grace period (e.g., 1 second) to all TTL comparisons across nodes.

**2. Not using fencing tokens for downstream operations**

A process holds a lock, pauses (GC, network delay), the lock expires, another process acquires it, and then the original process resumes and writes. Without a fencing token, both processes successfully write, corrupting data.

Mitigation: downstream storage must reject writes with a token less than the last seen token for that lock name.

**3. Quorum rollback without idempotency**

If `acquire` succeeds on nodes A and B but fails on C, the rollback sends `release` to A and B. If the rollback message to B is lost (network), B still thinks the lock is held. The next legitimate acquirer is denied.

Mitigation: use an epoch-tagged release: if the epoch matches, release; if not (new lock exists), ignore the rollback.

**4. Not testing split-brain explicitly**

The split-brain scenario requires two separate clusters each with a majority. In a test, simulate this by starting 4 nodes and using `:net_kernel.disconnect/1` to partition {A, B} from {C, D}. Verify that {A, B} can grant a lock and {C, D} cannot.

**5. Storing watch subscriptions only on one node**

If a watcher is on node A and the lock is managed by node B, the watch event must reach node A. Use `send(watcher_pid, event)` directly — BEAM message passing is location-transparent across connected nodes.

Mitigation: buffer events in the LockManager for N seconds and replay on reconnect if a watcher's node was partitioned.

**6. Reentrant lock without decrement counter**

A process acquires the same lock twice and must release it twice. Without a counter, the first release deletes the lock, and the second release fails or allows another process to acquire it.

Mitigation: track `count` in the lease; only release on `count == 1`.

---

## Implementation milestones (abbreviated)

### Lease struct with epoch and fencing token

A lease represents a lock grant with an expiration time. The `epoch` field is set when the quorum acquisition starts, tying all node-level grants for one logical lock attempt together. The `token` is a monotonically increasing fencing token.

### Quorum protocol

Broadcasts lock operations to all connected nodes and requires a majority (2 of 3) to agree. When acquisition fails to reach quorum, it rolls back grants on nodes that did respond positively.

### Heartbeat client

The heartbeat process runs on the lock holder's side. It periodically renews the lease before the TTL expires. If renewal fails (quorum lost, lease expired), it notifies the holder.

### Watch registry

Subscribers register to be notified when a lock is acquired, released, or expired. Watches filter events by type, avoiding irrelevant notifications.

### Leader election built on locks

Each candidate tries to acquire a well-known lock. The winner becomes the leader and maintains leadership via heartbeat renewal. Watches enable instant notification when the leader's lock is released.

---

## Given tests — must pass without modification

```elixir
# test/concurrent_test.exs
test "exactly one of N concurrent acquire attempts succeeds" do
  lock_name = "test-lock-#{System.unique_integer()}"
  n = 20
  me = self()

  pids = Enum.map(1..n, fn _ ->
    spawn(fn ->
      result = Locksmith.Quorum.acquire(lock_name, self(), 5_000)
      send(me, result)
    end)
  end)

  results = Enum.map(1..n, fn _ ->
    receive do
      r -> r
    after 3000 -> {:error, :timeout}
    end
  end)

  ok_count = Enum.count(results, fn {:ok, _} -> true; _ -> false end)
  assert ok_count == 1, "Expected exactly 1 success, got #{ok_count}"
end

# test/watch_test.exs
test "watch receives :acquired event when lock is taken" do
  lock_name = "watch-test-#{System.unique_integer()}"
  Locksmith.LockManager.watch(lock_name, self(), [:acquired, :released])
  Locksmith.Quorum.acquire(lock_name, self(), 30_000)
  assert_receive {:lock_event, ^lock_name, :acquired, _}, 500
end

# test/quorum_test.exs
test "acquire fails if quorum cannot be reached (insufficient nodes)" do
  assert {:error, :insufficient_nodes} =
    Locksmith.Quorum.test_acquire_with_nodes("test-quorum", self(), [], 2)
end
```

### Run the tests

```bash
mix test test/locksmith/ --trace
```

#
## Main Entry Point

```elixir
def main do
  IO.puts("======== 50-build-distributed-lock-service ========")
  IO.puts("Build Distributed Lock Service")
  IO.puts("")
  
{{:ok, locks}} = LockService.start_link([])
  IO.puts("Lock service started")
  
  {{:ok, lock_id}} = LockService.acquire(locks, "resource:1", timeout: 1000)
  IO.puts("Lock acquired: #{lock_id}")
  
  {{:error, :already_locked}} = LockService.acquire(locks, "resource:1", timeout: 100)
  IO.puts("Lock properly blocking second attempt")
  
  :ok = LockService.release(locks, lock_id)
  IO.puts("Lock released")
  
  IO.puts("Run: mix test")
end
```

