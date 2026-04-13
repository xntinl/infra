# Distributed Lock Service (ZooKeeper-like)

**Project**: `locksmith` — quorum-based distributed lock manager with lease TTLs, fencing tokens, and watches. Tolerates single-node failure in a 3-node cluster; safe under asymmetric network partitions.

---

## Why distributed locks matter

A "mutex across machines" is deceptively simple. `:global.register_name/2` and Redis `SETNX` look correct in a happy-path demo and fail in production for the same reason: neither enforces linearizable ownership under network partition plus process pause. The canonical failure:

1. Client A acquires the lock.
2. Client A's BEAM scheduler stalls (major GC, swap, 500ms virtualization jitter).
3. The lock's TTL expires.
4. Client B acquires the lock.
5. Client A resumes and writes to the protected resource using its (now-stale) grant.

Both A and B believe they hold the lock; data is corrupted. This is the scenario Martin Kleppmann dissected in ["How to do distributed locking"](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html). The fix requires **fencing tokens** (monotonically increasing per lock name, checked by the downstream resource) on top of quorum consensus — neither alone is sufficient.

References:
- [Kleppmann, "How to do distributed locking"](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html)
- [ZooKeeper recipes: locks](https://zookeeper.apache.org/doc/current/recipes.html#sc_recipes_Locks)
- [Paxos Made Simple, Lamport](https://lamport.azurewebsites.net/pubs/paxos-simple.pdf)

---

## The business problem

Your team runs a three-node Elixir cluster serving a catalog service. A nightly reindex job must execute exactly once across the cluster: running on two nodes concurrently produces duplicate entries and wedges the search index. The naive implementation used `:global.register_name/2` and suffered a split-brain after a brief partition — both sides registered the name, both jobs ran, the index corrupted.

Requirements for the replacement:
1. **Mutual exclusion**: at most one holder per lock name, provably, under partition.
2. **Liveness under single failure**: a 3-node cluster survives any 1-node loss with no downtime.
3. **Automatic recovery**: a crashed holder releases the lock within a bounded TTL (default 30s).
4. **Fencing**: a lease carries a token that downstream storage can use to reject stale writers.
5. **Watches**: leader-election needs push notification on release (no polling).
6. **Latency**: p99 acquire < 20ms on a local 3-node cluster; release < 5ms.

Locks are rare-but-critical: the QPS is low (100s/s), but every false grant is a data-corruption incident.

---

## Project structure

```
locksmith/
├── lib/
│   ├── locksmith.ex
│   └── locksmith/
│       ├── application.ex        # supervisor for manager + replication
│       ├── lock_manager.ex       # quorum state machine (per-node)
│       ├── quorum.ex             # 2-of-3 acquire/release protocol
│       ├── lease.ex              # TTL + heartbeat + monotonic-time checks
│       ├── fencing.ex            # monotonic token issuance + store API
│       ├── watch.ex              # subscription registry with replay buffer
│       ├── replication.ex        # inter-node gossip of lock state
│       ├── rpc.ex                # bounded-timeout call wrapper
│       └── client.ex             # public API: acquire/2, release/1, watch/2
├── script/
│   └── main.exs                  # stress: 20k concurrent acquires + partition
├── test/
│   └── locksmith_test.exs        # quorum, lease, fencing, split-brain, linearizability
└── mix.exs
```

---

## Design decisions

**Option A — single-leader lock manager (Redis SETNX style)**
- Pros: trivially linearizable, 1 RTT per op.
- Cons: leader is a SPOF; leader pause = global unavailability; no fencing primitive.

**Option B — Raft-replicated lock log (etcd style)**
- Pros: full linearizability via log replication.
- Cons: 2 RTTs per op, overkill for lock state (which is small and rarely contended at the byte level).

**Option C — majority-quorum acquire with epoch + fencing** (chosen)
- Pros: tolerates 1 failure in 3-node cluster; lease model handles crashes without external monitors; fencing prevents the "pause-expire-write" corruption; no Raft log overhead since lock state is small and reconcilable.
- Cons: requires careful rollback on partial-quorum acquisition; lease renewals must be fast (< TTL/3) to avoid flapping.

Chose **C**. It is the minimum that satisfies both safety (quorum + fencing) and liveness (TTL). The alternative (Raft for lock log) is the defensible industrial choice, but we're building a teaching-grade implementation where the quorum protocol is the subject.

**Monotonic time for TTL**: `System.monotonic_time/1`, never `System.system_time/1`. NTP adjustments and BEAM clock step can reverse wall-clock time; monotonic cannot. Cross-node TTL comparison carries a ±1s grace window since monotonic clocks are not synchronized across nodes.

**Fencing tokens are monotonically increasing per lock name**: issued by the node that completes quorum acquisition. Stored alongside the protected resource; downstream writes include the token; the resource rejects any write with `token < max_seen_token`. This is the only safe pattern for "pause-expire-resume" bugs.

---

## Implementation

### `mix.exs`

```elixir
defmodule Locksmith.MixProject do
  use Mix.Project

  def project do
    [
      app: :locksmith,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 85]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Locksmith.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `lib/locksmith.ex`

```elixir
defmodule Locksmith do
  @moduledoc """
  Quorum-based distributed lock service with fencing tokens.

  ## Semantics
    * Lock is granted iff a majority of cluster members (2-of-3) ack within timeout.
    * Grants include a monotonically increasing fencing token scoped to lock name.
    * Leases auto-expire after TTL; holder must renew at TTL/3 cadence.
    * Watches deliver `:acquired | :renewed | :released | :expired` events.

  ## Safety
    * Mutual exclusion: partition minority cannot grant; 2-of-3 quorum required.
    * Fencing: downstream resource must reject writes with stale token.
    * Time: monotonic-time TTL only; cross-node compares use ±1s grace.

  ## Liveness
    * Holder crash: lease expires within TTL, next acquirer wins.
    * Single-node failure: 2 surviving nodes form quorum, service continues.
    * Two-node failure: service unavailable (by design; cannot preserve safety).
  """

  alias Locksmith.{Client, Quorum, Watch}

  @type lock_name :: binary()
  @type token :: pos_integer()
  @type lease :: %{
          name: lock_name(),
          holder: pid(),
          token: token(),
          expires_at: integer(),
          epoch: pos_integer()
        }

  @default_ttl 30_000
  @max_ttl 300_000
  @min_ttl 1_000
  @max_name_bytes 256

  @doc """
  Acquires a lock. Blocks up to `timeout_ms` for quorum. Returns the lease
  or an error. On success, caller must periodically call `renew/1` or
  eventually the lease expires.

  ## Examples

      iex> Locksmith.acquire("orders:reindex", ttl: 10_000)
      {:ok, %{name: "orders:reindex", token: _, expires_at: _, epoch: _, holder: _}}

      iex> Locksmith.acquire("")
      {:error, :invalid_name}
  """
  @spec acquire(lock_name(), keyword()) ::
          {:ok, lease()}
          | {:error,
             :invalid_name
             | :invalid_ttl
             | :timeout
             | :insufficient_nodes
             | :quorum_denied
             | :rollback_failed}
  def acquire(name, opts \\ []) when is_binary(name) do
    ttl = Keyword.get(opts, :ttl, @default_ttl)
    timeout = Keyword.get(opts, :timeout, 5_000)

    with :ok <- validate_name(name),
         :ok <- validate_ttl(ttl) do
      Quorum.acquire(name, self(), ttl, timeout)
    end
  end

  @doc """
  Releases a previously acquired lease. Idempotent if called twice with the
  same lease; safe no-op if the lease has already expired.
  """
  @spec release(lease()) :: :ok | {:error, :expired | :wrong_holder | :unknown}
  def release(%{name: name, epoch: epoch} = lease) when is_map(lease) do
    Quorum.release(name, epoch, self())
  end

  @doc """
  Renews a lease, extending its TTL. Must be called before expiry;
  returns {:error, :expired} if called too late.
  """
  @spec renew(lease()) :: {:ok, lease()} | {:error, :expired | :quorum_denied}
  def renew(%{name: name, epoch: epoch, token: token} = lease) do
    Quorum.renew(name, epoch, token, self())
  end

  @doc """
  Subscribes the calling process to events for a lock name. Events are
  delivered as `{:lock_event, name, type, metadata}`.
  """
  @spec watch(lock_name(), [atom()]) :: :ok | {:error, :invalid_name}
  def watch(name, types \\ [:acquired, :released, :expired]) when is_binary(name) do
    with :ok <- validate_name(name) do
      Watch.subscribe(name, self(), types)
    end
  end

  @spec validate_name(binary()) :: :ok | {:error, :invalid_name}
  defp validate_name(""), do: {:error, :invalid_name}
  defp validate_name(n) when byte_size(n) > @max_name_bytes, do: {:error, :invalid_name}
  defp validate_name(n) do
    if String.printable?(n), do: :ok, else: {:error, :invalid_name}
  end

  @spec validate_ttl(integer()) :: :ok | {:error, :invalid_ttl}
  defp validate_ttl(t) when is_integer(t) and t >= @min_ttl and t <= @max_ttl, do: :ok
  defp validate_ttl(_), do: {:error, :invalid_ttl}
end
```
### `test/locksmith_test.exs`

```elixir
defmodule LocksmithTest do
  use ExUnit.Case, async: true
  use ExUnitProperties
  doctest Locksmith

  alias Locksmith.{Quorum, Fencing, Watch}

  setup do
    {:ok, _} = Application.ensure_all_started(:locksmith)
    Locksmith.TestCluster.start(nodes: 3)
    on_exit(fn -> Locksmith.TestCluster.stop() end)
    :ok
  end

  describe "acquire/1 input validation" do
    test "rejects empty name" do
      assert {:error, :invalid_name} = Locksmith.acquire("")
    end

    test "rejects oversized name" do
      assert {:error, :invalid_name} = Locksmith.acquire(String.duplicate("x", 300))
    end

    test "rejects ttl below minimum" do
      assert {:error, :invalid_ttl} = Locksmith.acquire("k", ttl: 100)
    end

    test "rejects ttl above maximum" do
      assert {:error, :invalid_ttl} = Locksmith.acquire("k", ttl: 1_000_000)
    end
  end

  describe "mutual exclusion" do
    test "exactly one of N concurrent acquires succeeds" do
      n = 50
      name = "excl-#{System.unique_integer([:positive])}"
      me = self()

      for _ <- 1..n do
        spawn(fn -> send(me, Locksmith.acquire(name, ttl: 5_000, timeout: 3_000)) end)
      end

      results = for _ <- 1..n, do: (receive do r -> r after 5_000 -> :timeout end)
      oks = Enum.count(results, &match?({:ok, _}, &1))
      assert oks == 1
    end
  end

  describe "fencing tokens" do
    test "tokens are strictly monotonic per lock name" do
      name = "fence-#{System.unique_integer([:positive])}"

      {:ok, l1} = Locksmith.acquire(name, ttl: 2_000)
      Locksmith.release(l1)
      {:ok, l2} = Locksmith.acquire(name, ttl: 2_000)
      assert l2.token > l1.token
      Locksmith.release(l2)
    end

    test "storage rejects stale tokens" do
      name = "stale-#{System.unique_integer([:positive])}"
      {:ok, l1} = Locksmith.acquire(name, ttl: 5_000)
      assert :ok = Fencing.write(name, l1.token, "v1")
      # simulate pause-expire
      Locksmith.Lease.force_expire(l1)
      {:ok, l2} = Locksmith.acquire(name, ttl: 5_000)
      assert :ok = Fencing.write(name, l2.token, "v2")
      assert {:error, :stale_token} = Fencing.write(name, l1.token, "v3")
    end
  end

  describe "split-brain prevention" do
    test "partition minority cannot grant lock" do
      name = "split-#{System.unique_integer([:positive])}"
      Locksmith.TestCluster.partition([:node_a], [:node_b, :node_c])

      # minority side cannot acquire
      assert {:error, :insufficient_nodes} =
               Quorum.acquire_on(:node_a, name, self(), 5_000, 1_000)

      # majority side can
      assert {:ok, _} = Quorum.acquire_on(:node_b, name, self(), 5_000, 1_000)
    end
  end

  describe "watch" do
    test "subscribers receive :acquired and :released" do
      name = "watch-#{System.unique_integer([:positive])}"
      :ok = Locksmith.watch(name, [:acquired, :released])
      {:ok, lease} = Locksmith.acquire(name, ttl: 2_000)
      assert_receive {:lock_event, ^name, :acquired, %{token: _}}, 500
      Locksmith.release(lease)
      assert_receive {:lock_event, ^name, :released, _}, 500
    end

    test "subscribers receive :expired on TTL elapse" do
      name = "expire-#{System.unique_integer([:positive])}"
      :ok = Locksmith.watch(name, [:expired])
      {:ok, _} = Locksmith.acquire(name, ttl: 1_000)
      assert_receive {:lock_event, ^name, :expired, _}, 2_500
    end
  end

  describe "liveness under failure" do
    test "survives single-node crash" do
      name = "liveness-#{System.unique_integer([:positive])}"
      :ok = Locksmith.TestCluster.crash(:node_c)
      assert {:ok, lease} = Locksmith.acquire(name, ttl: 5_000, timeout: 3_000)
      Locksmith.release(lease)
    end

    test "unavailable under two-node crash (safety preserved)" do
      name = "liveness-2-#{System.unique_integer([:positive])}"
      :ok = Locksmith.TestCluster.crash(:node_b)
      :ok = Locksmith.TestCluster.crash(:node_c)
      assert {:error, :insufficient_nodes} = Locksmith.acquire(name, ttl: 5_000, timeout: 1_000)
    end
  end

  describe "linearizability property" do
    property "serializable history across concurrent clients" do
      check all ops <- list_of(op_gen(), min_length: 10, max_length: 100), max_runs: 20 do
        name = "prop-#{System.unique_integer([:positive])}"
        history = Locksmith.TestHarness.run(name, ops)
        assert Locksmith.TestHarness.linearizable?(history)
      end
    end
  end

  defp op_gen do
    StreamData.one_of([
      StreamData.constant(:acquire),
      StreamData.constant(:release),
      StreamData.constant(:renew)
    ])
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Stress harness for Locksmith: 20k concurrent acquire attempts on contended
  keys, then a partition scenario, then holder-crash recovery. Fails with
  exit 1 if any safety invariant is violated or SLO is breached.
  """

  @contended_keys 100
  @concurrency 20_000
  @slo_p99_ms 20

  def main do
    {:ok, _} = Application.ensure_all_started(:locksmith)
    Locksmith.TestCluster.start(nodes: 3)

    IO.puts("=== Phase 1: #{@concurrency} concurrent acquires on #{@contended_keys} keys ===")
    phase1 = contention_phase()

    IO.puts("\n=== Phase 2: partition injection (2/3 majority side) ===")
    phase2 = partition_phase()

    IO.puts("\n=== Phase 3: holder crash, lease expiry recovery ===")
    phase3 = crash_recovery_phase()

    IO.puts("\n=== Phase 4: fencing-token safety invariant ===")
    phase4 = fencing_phase()

    IO.puts("\n=== Phase 5: sustained throughput (60s) ===")
    phase5 = throughput_phase(60)

    report([phase1, phase2, phase3, phase4, phase5])
  end

  defp contention_phase do
    me = self()
    started = System.monotonic_time(:millisecond)

    for i <- 1..@concurrency do
      spawn(fn ->
        key = "k#{rem(i, @contended_keys)}"
        t0 = System.monotonic_time(:microsecond)
        result = Locksmith.acquire(key, ttl: 2_000, timeout: 5_000)
        elapsed = System.monotonic_time(:microsecond) - t0
        case result do
          {:ok, lease} ->
            Process.sleep(:rand.uniform(10))
            Locksmith.release(lease)
          _ -> :skip
        end
        send(me, {:done, result, elapsed})
      end)
    end

    results = for _ <- 1..@concurrency, do: (receive do m -> m after 30_000 -> :timeout end)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    lats = for {:done, _, us} <- results, do: us
    oks = Enum.count(results, fn {:done, {:ok, _}, _} -> true; _ -> false end)
    percentiles(lats) |> Map.merge(%{phase: :contention, oks: oks, throughput: round(oks / elapsed_s)})
  end

  defp partition_phase do
    Locksmith.TestCluster.partition([:node_a], [:node_b, :node_c])
    minority = Task.async(fn -> Locksmith.Quorum.acquire_on(:node_a, "part", self(), 1_000, 2_000) end)
    majority = Task.async(fn -> Locksmith.Quorum.acquire_on(:node_b, "part", self(), 5_000, 2_000) end)

    m_res = Task.await(minority, 5_000)
    maj_res = Task.await(majority, 5_000)
    Locksmith.TestCluster.heal()

    safe = match?({:error, :insufficient_nodes}, m_res) and match?({:ok, _}, maj_res)
    %{phase: :partition, safe: safe, minority: m_res, majority: maj_res}
  end

  defp crash_recovery_phase do
    name = "crash-#{System.unique_integer([:positive])}"
    holder = spawn(fn ->
      {:ok, _lease} = Locksmith.acquire(name, ttl: 2_000)
      Process.sleep(:infinity)
    end)
    Process.sleep(200)
    Process.exit(holder, :kill)

    t0 = System.monotonic_time(:millisecond)
    result =
      Enum.reduce_while(1..100, nil, fn _, _ ->
        case Locksmith.acquire(name, ttl: 2_000, timeout: 500) do
          {:ok, lease} -> {:halt, {:ok, lease}}
          {:error, _} -> Process.sleep(50); {:cont, nil}
        end
      end)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    %{phase: :crash, recovered: match?({:ok, _}, result), recovery_ms: recovery_ms}
  end

  defp fencing_phase do
    name = "fence-#{System.unique_integer([:positive])}"
    {:ok, l1} = Locksmith.acquire(name, ttl: 2_000)
    :ok = Locksmith.Fencing.write(name, l1.token, "v1")
    Locksmith.Lease.force_expire(l1)
    {:ok, l2} = Locksmith.acquire(name, ttl: 2_000)
    stale = Locksmith.Fencing.write(name, l1.token, "corrupt")
    fresh = Locksmith.Fencing.write(name, l2.token, "v2")
    Locksmith.release(l2)
    %{phase: :fencing, stale_rejected: stale == {:error, :stale_token}, fresh_ok: fresh == :ok}
  end

  defp throughput_phase(seconds) do
    deadline = System.monotonic_time(:millisecond) + seconds * 1000
    workers = System.schedulers_online() * 2
    me = self()

    for w <- 1..workers do
      spawn(fn -> throughput_worker(w, deadline, me, 0, []) end)
    end

    all = for _ <- 1..workers, do: (receive do {:batch, lats} -> lats after seconds * 1000 + 5000 -> [] end)
    lats = List.flatten(all)
    percentiles(lats) |> Map.merge(%{phase: :throughput, ops: length(lats)})
  end

  defp throughput_worker(w, deadline, parent, count, acc) do
    if System.monotonic_time(:millisecond) >= deadline do
      send(parent, {:batch, acc})
    else
      key = "t-#{rem(w, 50)}-#{rem(count, 20)}"
      t0 = System.monotonic_time(:microsecond)
      case Locksmith.acquire(key, ttl: 1_000, timeout: 500) do
        {:ok, l} -> Locksmith.release(l)
        _ -> :skip
      end
      elapsed = System.monotonic_time(:microsecond) - t0
      throughput_worker(w, deadline, parent, count + 1, [elapsed | acc])
    end
  end

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(lats) do
    s = Enum.sort(lats)
    n = length(s)
    %{
      p50: Enum.at(s, div(n, 2)),
      p95: Enum.at(s, div(n * 95, 100)),
      p99: Enum.at(s, div(n * 99, 100)),
      p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
    }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      (Map.get(Enum.find(phases, &(&1.phase == :contention)), :p99, 0) > @slo_p99_ms * 1000) or
        (Map.get(Enum.find(phases, &(&1.phase == :partition)), :safe, false) == false) or
        (Map.get(Enum.find(phases, &(&1.phase == :fencing)), :stale_rejected, false) == false)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
---

## Error Handling and Recovery

Locksmith treats safety violations as **non-recoverable** and liveness issues as **retryable with bounded attempts**.

### Critical invariant violations (halt, alert)

| Violation | Detection | Response |
|---|---|---|
| Two holders observed for same name + overlapping epochs | Log scanner cross-checks lease grants from all nodes | Emit `:safety_violation` telemetry, halt service, dump WAL for forensics |
| Fencing token decreased for same lock | `Fencing.write/3` sees token < max_seen_token | Reject write with `:stale_token`, emit counter, DO NOT crash |
| Quorum reports diverging epoch in same term | Replication background checker | Enter read-only mode, page SRE |
| Monotonic time regression | `System.monotonic_time/1` observed going backward | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bound |
|---|---|---|
| `acquire` timeout on quorum | Client retries with exponential backoff (100ms base, 20% jitter) | Max 3 attempts total |
| Node unreachable during quorum | Skip, proceed if remaining form majority | Fail with `:insufficient_nodes` if < ceil(N/2)+1 reachable |
| Renewal RPC timeout | Retry once immediately, then backoff | Abort lease if < TTL/4 remaining |
| Partial quorum rollback failure | Log warning, rely on TTL to expire orphans | Never blocks subsequent acquires (epoch-scoped) |
| Watch delivery fails (pid dead) | Remove subscription silently | No retries for dead pids |

### Recovery protocol

1. **Cold start**: Node discovers cluster via `libcluster`, syncs current lease table from any healthy peer (best-effort; stale is acceptable because TTLs expire).
2. **Partition heal**: When a partition heals, both sides exchange lease tables. Conflicts resolved by `(epoch, token, expires_at)` highest wins. Losing leases are "released" locally and emit a `:lease_lost` event for the holder to handle.
3. **Lease expiry sweep**: Every 1s, each node scans local leases for `expires_at + 1s_grace < now` (the 1s grace handles cross-node monotonic clock drift). Expired leases emit `:expired` and are removed.
4. **Epoch-tagged rollback**: If `acquire` succeeded on A and B but C timed out, we roll back on A and B. The rollback includes the epoch. If a newer acquire has already started on A (new epoch), the rollback is a no-op — preventing the classic "rollback clobbers new holder" bug.

### Bulkheads

- Each lock name has a bounded waiter queue (max 1000 pending). Overflow returns `{:error, :contention_overflow}` immediately.
- Watch subscriptions are per-pid; a pid cannot have more than 100 watches (DoS protection).
- RPC calls carry hard timeouts (default 1s) so a slow peer cannot stall the local node.

---

## Performance Targets

| Metric | Target | Notes |
|---|---|---|
| Acquire p50 | **< 3 ms** | uncontended, local 3-node cluster |
| Acquire p99 | **< 20 ms** | uncontended |
| Acquire p99 contended | **< 100 ms** | 50 waiters on same key |
| Release p99 | **< 5 ms** | fire-and-forget, ack quorum |
| Renew p99 | **< 5 ms** | on heartbeat cadence (TTL/3) |
| Throughput | **> 5,000 ops/s** | sustained, varied keys |
| Recovery after holder crash | **< TTL + 1s** | lease expiry + sweep latency |
| Recovery after partition heal | **< 5 s** | lease table reconciliation |
| Memory per active lease | **< 500 B** | map entry + watch subscribers |
| Watch notification latency | **< 10 ms** | from state change to delivery |

**Baselines we should beat/match**:
- etcd distributed lock (v3 clientv3 `sync/semaphore`): acquire p99 ~ 10-30ms on localhost cluster; comparable target.
- Redis `SET NX PX` + Lua release: acquire p99 ~ 1-5ms on localhost — faster, but no fencing and not safe under Redis failover.
- ZooKeeper recipe with ephemeral nodes: acquire p99 ~ 20-50ms; our target improves due to shorter heartbeat path.

---

## Key concepts

### 1. Quorum is necessary but not sufficient
Quorum consensus (2-of-3) prevents split-brain where both partition sides grant the lock. It does *not* prevent a paused-then-resumed holder from writing stale data. That requires fencing tokens plus a downstream resource that validates them.

### 2. Lease TTL is a liveness mechanism, not a safety mechanism
TTL guarantees a crashed holder's lock eventually becomes available (liveness). It does NOT guarantee the old holder never writes again — the holder may resume between "I still have my lease according to my clock" and the server expiring it. Hence fencing.

### 3. Monotonic time is mandatory, wall-clock is toxic
`System.system_time/1` can jump backward (NTP correction, VM pause-and-resume, container migration). Using it for TTL expiry creates silent bugs where leases never expire or expire unexpectedly. `System.monotonic_time/1` is guaranteed non-decreasing within a single node's BEAM process.

### 4. Cross-node time is untrustworthy; build in grace windows
Node A's monotonic time has zero relationship to node B's. Cross-node TTL comparisons need a grace window (e.g., ±1s) to avoid false expirations during heartbeat lag.

### 5. Epoch tagging rescues rollback
The non-obvious bug: `acquire` partially succeeds, we roll back, but a new acquirer has already started on the nodes that acked. Without epoch tagging, the rollback releases the new holder's lock. With epoch tagging, the rollback says "release iff epoch matches", making it safe under interleaving.

### 6. Watches are strictly-less-safe than polling
Push notifications are efficient but fail silently if delivery is lost (dead pid, network blip). A robust client combines watches (fast path) with periodic polling as a recovery mechanism (safety net).

### 7. Reentrance requires a counter
A process acquires the same lock twice, must release twice. Without a counter, the first release frees the lock mid-critical-section and another process enters. Track `count` in the lease and only free on `count == 1`.

---

## Why Distributed Lock Service (ZooKeeper-like) matters

Mastering **Distributed Lock Service (ZooKeeper-like)** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.
