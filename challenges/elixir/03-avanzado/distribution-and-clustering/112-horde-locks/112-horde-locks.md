# Distributed Locks with Horde

**Project**: `horde_distributed_locks` — lease-based locks over a cluster

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

Your billing service runs a nightly job that charges every overdue invoice. When the fleet
was a single node, `GenServer` state was enough to ensure only one worker ran at a time. The
cluster is now five nodes and libcluster auto-forms the mesh. Your nightly job has run
twice for several customers, double-charging them.

You need a **mutual-exclusion primitive** that holds across nodes. Options:

- `:global.trans/4` — works but slow (locks every node, blocks on netsplit).
- External: Redis `SETNX` with TTL, Postgres advisory locks, Consul sessions — more infra.
- **Horde.Registry** as a distributed lock table — CRDT-backed, no external infra, lease
  semantics you control.

Horde is not a lock service out of the box; `Horde.Registry` is a distributed process
registry. But the primitive "exactly one process claims this key cluster-wide" composes
with `Process.monitor` and timeouts to build leases. This exercise implements a proper
lease-based lock on top of Horde with heartbeat, renewal, stealing-on-death, and
acquire-with-timeout.

```
horde_distributed_locks/
├── lib/
│   └── horde_distributed_locks/
│       ├── application.ex
│       ├── lock_registry.ex       # Horde.Registry for lock holders
│       ├── lock.ex                # public API: acquire/release/with_lock
│       ├── lease_holder.ex        # per-lock GenServer holding the lease
│       └── clock.ex               # monotonic wall-clock helper
├── test/
│   └── horde_distributed_locks/
│       ├── lock_test.exs
│       └── cluster_lock_test.exs
└── mix.exs
```

---

## Core concepts

### 1. What "distributed lock" actually means

A lock is a contract: "only one holder at any time". In a distributed system, "any time"
and "one" both get complicated:

- Clock skew means two nodes can disagree on the current time.
- Network delay means a lock-release message takes finite time to propagate.
- A holder can crash or be partitioned after acquiring — should the lock expire?

Martin Kleppmann's "How to do distributed locking" famously argued that locks without
**fencing tokens** (monotonically increasing IDs proving who held the lock at which time)
are broken for any operation where correctness matters — because a laggy client can resume
thinking it still holds the lock after it was expired and reassigned.

For our billing job: we use lease locks with fencing. The lease holder gets a token; any
external side-effect (DB writes) checks the token.

### 2. Horde.Registry as a lock table

Horde.Registry maps `key → pid` across the cluster. Only one pid can hold a given key —
if two processes call `Horde.Registry.register/3` for the same key, one wins, the other
gets `{:error, {:already_registered, pid}}`.

```
+----------------+       +----------------+
|   Node A       |       |   Node B       |
|                |       |                |
|  LeaseHolder   |       |                |
|  for :job_X    |       |                |
|    pid=#PID<1> |       |                |
+--------+-------+       +--------+-------+
         |                        |
         └──► Horde.Registry ◄────┘
              {:job_X, #PID<1>, epoch: 42}
```

When `LeaseHolder` dies, Horde removes the registration; another node can now acquire.

### 3. The lease lifecycle

```
  acquire(:job_X, ttl: 30_000)
       │
       ▼
  Horde.Registry.register(:locks, :job_X, {self(), epoch, deadline})
       │
    ┌──┴──┐
    │     │
  :ok   {:error, {:already_registered, other}}
    │     │
    │     └─► return {:error, :held_by, other}
    │
    ▼
  spawn heartbeat: every ttl/3, renew deadline in the registry metadata
    │
    ▼
  caller work()
    │
    ▼
  release() → Horde.Registry.unregister(:locks, :job_X)

  If caller crashes: LeaseHolder is linked → LeaseHolder dies → Horde unregisters
  If node partitions: Horde sees that side as gone → other side can re-acquire
```

### 4. Fencing tokens

Every lock acquisition increments a global counter (via an atomic, or a :pg-coordinated
process, or Horde metadata). The holder's fencing token is higher than any prior holder's.
External systems — your DB writes — should reject operations with tokens lower than the
highest they've seen. This prevents a stale holder from clobbering new work.

```elixir
def charge_invoice(invoice_id, fencing_token) do
  Repo.update_all(
    from(i in Invoice,
      where: i.id == ^invoice_id and i.lock_epoch <= ^fencing_token
    ),
    set: [status: :charged, lock_epoch: fencing_token]
  )
end
```

If a stale holder tries to update with an older token, the `where` clause excludes the
row — the update is a no-op.

### 5. Renewal and heartbeats

A TTL-based lock expires if not renewed. Renewal must be **more frequent than expiry**
(typically 3x margin: renew every TTL/3). If renewal fails (network issue), the holder
must proactively release and stop doing work — otherwise it may race with a new holder.

The holder does NOT unilaterally decide it still has the lock because its local clock hasn't
elapsed. It checks the registry entry on every renewal; if the entry is missing or owned
by another pid, the lease is lost.

### 6. Split-brain under Horde

Horde uses delta-CRDTs. During a partition, both sides see the lock as held by whoever
was recorded before the split. A new acquirer on the minority side sees
`{:already_registered, _}` and fails — good. But if the original holder WAS on the
minority side and dies there, the majority side doesn't learn about the death until
heal. They'll see the lock as held until:

- The LeaseHolder's monitor fires on reconnect, OR
- The lease TTL expires cluster-wide.

**Design rule**: always use a lease TTL short enough that your app's SLA can tolerate it
as recovery time. Never use infinite-TTL locks in production.

---

## Implementation

### Step 1: Mix deps

```elixir
defp deps do
  [
    {:horde, "~> 0.9"},
    {:libcluster, "~> 3.3"}
  ]
end
```

### Step 2: Application supervisor

```elixir
defmodule HordeDistributedLocks.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    topologies = Application.get_env(:libcluster, :topologies, [])

    children = [
      {Cluster.Supervisor, [topologies, [name: HordeDistributedLocks.ClusterSupervisor]]},
      {Horde.Registry,
       name: HordeDistributedLocks.LockRegistry,
       keys: :unique,
       members: :auto,
       delta_crdt_options: [sync_interval: 100]},
      {DynamicSupervisor,
       name: HordeDistributedLocks.LeaseSupervisor, strategy: :one_for_one}
    ]

    opts = [strategy: :one_for_one, name: HordeDistributedLocks.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: Clock helper

```elixir
defmodule HordeDistributedLocks.Clock do
  @moduledoc "Wrapper around system time — one place to stub in tests."

  @spec now_ms() :: integer()
  def now_ms, do: System.system_time(:millisecond)
end
```

### Step 4: LeaseHolder — the process that owns the lock

```elixir
defmodule HordeDistributedLocks.LeaseHolder do
  @moduledoc """
  A process that registers itself in Horde under the lock key. While alive
  and reachable, it holds the lock. Renews its deadline in the registry
  metadata every `ttl/3` ms.
  """
  use GenServer
  require Logger

  alias HordeDistributedLocks.{Clock, LockRegistry}

  @type state :: %{
          key: term(),
          epoch: pos_integer(),
          ttl_ms: pos_integer(),
          deadline: integer(),
          caller: pid(),
          caller_ref: reference()
        }

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @doc "Returns {:ok, token} if this process still holds the lock, {:error, :lost} otherwise."
  @spec fencing_token(pid()) :: {:ok, pos_integer()} | {:error, :lost}
  def fencing_token(pid), do: GenServer.call(pid, :fencing_token)

  @impl true
  def init(opts) do
    key = Keyword.fetch!(opts, :key)
    ttl = Keyword.fetch!(opts, :ttl_ms)
    caller = Keyword.fetch!(opts, :caller)
    epoch = Keyword.get(opts, :epoch, :erlang.unique_integer([:positive, :monotonic]))

    deadline = Clock.now_ms() + ttl

    case Horde.Registry.register(LockRegistry, key, %{
           owner: self(),
           epoch: epoch,
           deadline: deadline
         }) do
      {:ok, _} ->
        ref = Process.monitor(caller)
        schedule_renew(ttl)

        state = %{
          key: key,
          epoch: epoch,
          ttl_ms: ttl,
          deadline: deadline,
          caller: caller,
          caller_ref: ref
        }

        {:ok, state}

      {:error, {:already_registered, _pid}} ->
        {:stop, :already_registered}
    end
  end

  @impl true
  def handle_call(:fencing_token, _from, state) do
    if lock_still_mine?(state) do
      {:reply, {:ok, state.epoch}, state}
    else
      {:reply, {:error, :lost}, state}
    end
  end

  @impl true
  def handle_info(:renew, state) do
    new_deadline = Clock.now_ms() + state.ttl_ms
    # Horde.Registry.update_value/3 only accepts a function that takes the old value
    Horde.Registry.update_value(LockRegistry, state.key, fn old ->
      Map.put(old, :deadline, new_deadline)
    end)

    schedule_renew(state.ttl_ms)
    {:noreply, %{state | deadline: new_deadline}}
  end

  def handle_info({:DOWN, ref, :process, _pid, _reason}, %{caller_ref: ref} = state) do
    Logger.info("LeaseHolder: caller died, releasing lock key=#{inspect(state.key)}")
    {:stop, :normal, state}
  end

  def handle_info(_other, state), do: {:noreply, state}

  @impl true
  def terminate(_reason, state) do
    Horde.Registry.unregister(LockRegistry, state.key)
    :ok
  end

  defp schedule_renew(ttl), do: Process.send_after(self(), :renew, div(ttl, 3))

  defp lock_still_mine?(%{key: key, epoch: epoch}) do
    case Horde.Registry.lookup(LockRegistry, key) do
      [{pid, %{epoch: ^epoch}}] when pid == self() -> true
      _ -> false
    end
  end
end
```

### Step 5: Public Lock API

```elixir
defmodule HordeDistributedLocks.Lock do
  @moduledoc """
  Public lock API.

      {:ok, handle} = Lock.acquire(:nightly_billing, ttl_ms: 30_000)
      {:ok, token} = Lock.fencing_token(handle)
      try do
        do_charges(token)
      after
        Lock.release(handle)
      end
  """

  alias HordeDistributedLocks.{LeaseHolder, LeaseSupervisor, LockRegistry}

  @type handle :: pid()

  @doc """
  Try to acquire `key`. Returns `{:ok, handle}` or `{:error, :held_by, pid}`.

  `ttl_ms` is how long the lease lasts without renewal; we renew automatically
  while the handle is alive.
  """
  @spec acquire(term(), keyword()) :: {:ok, handle()} | {:error, :held_by, pid()}
  def acquire(key, opts \\ []) do
    ttl = Keyword.get(opts, :ttl_ms, 30_000)
    caller = self()

    spec = %{
      id: {LeaseHolder, key},
      start:
        {LeaseHolder, :start_link,
         [[key: key, ttl_ms: ttl, caller: caller]]},
      restart: :temporary
    }

    case DynamicSupervisor.start_child(LeaseSupervisor, spec) do
      {:ok, pid} ->
        {:ok, pid}

      {:error, {:already_registered, _}} ->
        [{pid, _}] = Horde.Registry.lookup(LockRegistry, key)
        {:error, :held_by, pid}

      {:error, :already_registered} ->
        [{pid, _}] = Horde.Registry.lookup(LockRegistry, key)
        {:error, :held_by, pid}

      other ->
        other
    end
  end

  @doc "Acquire, retrying until success or timeout."
  @spec acquire_with_timeout(term(), keyword()) :: {:ok, handle()} | {:error, :timeout}
  def acquire_with_timeout(key, opts \\ []) do
    timeout = Keyword.get(opts, :wait_ms, 5_000)
    deadline = System.monotonic_time(:millisecond) + timeout
    do_acquire_loop(key, opts, deadline)
  end

  defp do_acquire_loop(key, opts, deadline) do
    case acquire(key, opts) do
      {:ok, h} ->
        {:ok, h}

      {:error, :held_by, _} ->
        if System.monotonic_time(:millisecond) >= deadline do
          {:error, :timeout}
        else
          Process.sleep(50 + :rand.uniform(50))
          do_acquire_loop(key, opts, deadline)
        end
    end
  end

  @spec release(handle()) :: :ok
  def release(handle) when is_pid(handle) do
    if Process.alive?(handle) do
      GenServer.stop(handle, :normal)
    end

    :ok
  end

  @spec fencing_token(handle()) :: {:ok, pos_integer()} | {:error, :lost}
  def fencing_token(handle), do: LeaseHolder.fencing_token(handle)

  @doc """
  Run `fun` while holding `key`. Releases unconditionally afterward, even on
  exceptions. If the lock cannot be acquired within `wait_ms`, returns
  `{:error, :timeout}`.
  """
  @spec with_lock(term(), keyword(), (pos_integer() -> result)) ::
          {:ok, result} | {:error, :timeout}
        when result: term()
  def with_lock(key, opts, fun) when is_function(fun, 1) do
    case acquire_with_timeout(key, opts) do
      {:ok, handle} ->
        try do
          case fencing_token(handle) do
            {:ok, token} -> {:ok, fun.(token)}
            {:error, :lost} -> {:error, :lock_lost}
          end
        after
          release(handle)
        end

      {:error, :timeout} = err ->
        err
    end
  end
end
```

### Step 6: Tests (single-node)

```elixir
defmodule HordeDistributedLocks.LockTest do
  use ExUnit.Case, async: false

  alias HordeDistributedLocks.Lock

  setup do
    # App supervisor already started via test helper
    :ok
  end

  test "acquire returns :ok for a free key" do
    assert {:ok, handle} = Lock.acquire(:free_key, ttl_ms: 5_000)
    assert is_pid(handle)
    assert Process.alive?(handle)
    Lock.release(handle)
  end

  test "acquire twice returns :held_by" do
    {:ok, h1} = Lock.acquire(:contended, ttl_ms: 5_000)
    assert {:error, :held_by, ^h1} = Lock.acquire(:contended, ttl_ms: 5_000)
    Lock.release(h1)
  end

  test "release allows re-acquire" do
    {:ok, h1} = Lock.acquire(:cycle, ttl_ms: 5_000)
    Lock.release(h1)
    # Give Horde a moment to propagate the unregister
    Process.sleep(50)
    assert {:ok, h2} = Lock.acquire(:cycle, ttl_ms: 5_000)
    Lock.release(h2)
  end

  test "fencing tokens are strictly increasing" do
    {:ok, h1} = Lock.acquire(:fence, ttl_ms: 5_000)
    {:ok, t1} = Lock.fencing_token(h1)
    Lock.release(h1)
    Process.sleep(50)

    {:ok, h2} = Lock.acquire(:fence, ttl_ms: 5_000)
    {:ok, t2} = Lock.fencing_token(h2)
    Lock.release(h2)

    assert t2 > t1
  end

  test "acquire_with_timeout blocks up to wait_ms" do
    {:ok, h1} = Lock.acquire(:blocking, ttl_ms: 10_000)

    t0 = System.monotonic_time(:millisecond)
    result = Lock.acquire_with_timeout(:blocking, ttl_ms: 10_000, wait_ms: 300)
    elapsed = System.monotonic_time(:millisecond) - t0

    assert {:error, :timeout} = result
    assert elapsed >= 300
    assert elapsed < 600

    Lock.release(h1)
  end

  test "with_lock/3 runs fun and releases on success" do
    assert {:ok, 42} =
             Lock.with_lock(:wl, [ttl_ms: 5_000, wait_ms: 100], fn _token -> 42 end)
  end

  test "with_lock/3 releases on exception" do
    assert_raise RuntimeError, "oops", fn ->
      Lock.with_lock(:wl_ex, [ttl_ms: 5_000, wait_ms: 100], fn _token ->
        raise "oops"
      end)
    end

    # Lock is free again
    Process.sleep(50)
    assert {:ok, h} = Lock.acquire(:wl_ex, ttl_ms: 5_000)
    Lock.release(h)
  end

  test "caller death releases the lock" do
    parent = self()

    child =
      spawn(fn ->
        {:ok, _} = Lock.acquire(:auto_release, ttl_ms: 10_000)
        send(parent, :acquired)
        receive do
          :stop -> :ok
        end
      end)

    assert_receive :acquired, 1_000

    Process.exit(child, :kill)
    Process.sleep(100)

    assert {:ok, h} = Lock.acquire(:auto_release, ttl_ms: 5_000)
    Lock.release(h)
  end
end
```

---

## Trade-offs and production gotchas

**1. Horde isn't a consensus algorithm**
Horde uses CRDTs — convergent on heal, not linearizable during a partition. Both sides of a
split can briefly register the same key. The check `lock_still_mine?` in `LeaseHolder`
helps, but there is a short window where two holders believe they have the lock. Fencing
tokens in downstream side effects are the real protection.

**2. TTL vs renewal interval ratio matters**
Renewing every TTL/3 means one missed renewal cycle leaves 2/3 of the TTL as margin. Going
to TTL/2 is risky: one slow renewal and the lock expires during legitimate work. Stay at
TTL/3 or TTL/4.

**3. No "steal with proof" primitive**
If a holder is clearly dead (node partitioned) but Horde hasn't yet converged, you cannot
safely steal. You MUST wait for the TTL to expire cluster-wide, or use an external
consensus system. Don't try to manually unregister someone else's key — it's a race.

**4. Horde requires :auto members OR explicit management**
`members: :auto` works for simple libcluster setups; but on highly dynamic clusters
(Kubernetes with pods churning), manual `Horde.Cluster.set_members/2` gives more control.
Read Horde's docs before production deploy.

**5. LeaseSupervisor restart strategy**
We set `restart: :temporary` on LeaseHolder — a crashed holder is NOT automatically
restarted. If it were, the new incarnation would get a fresh epoch and might interfere
with the caller who thought they lost the lock. Let them explicitly retry.

**6. Long-running work exceeds lease TTL**
If `with_lock/3`'s `fun` runs longer than `ttl_ms`, renewal keeps happening but the risk
of lease loss (network hiccup, LeaseHolder crash) grows linearly with work duration.
For multi-hour jobs, consider checkpointing and re-acquiring the lock mid-flight.

**7. Not a barrier, not a semaphore**
This primitive is mutual exclusion — one holder at a time. For "up to N holders", use a
semaphore (Horde.Registry with count-keys + a custom check-and-increment). For "wait until
all parties arrive", use a barrier (GenServer tracking arrivals).

**8. When NOT to use this**
- Single-node deployments — `:global.trans/4` or a plain GenServer is simpler and safer.
- When you need strict linearizability — use `:ra` (Raft), ZooKeeper, or etcd.
- For idempotency tokens (one-shot deduplication) — use Oban/Redis with UNIQUE constraints.
- For rate limiting — use a token bucket, not a lock.

---

## Benchmark

```elixir
Benchee.run(
  %{
    "acquire + release (uncontended)" => fn ->
      {:ok, h} = HordeDistributedLocks.Lock.acquire(:bench, ttl_ms: 60_000)
      HordeDistributedLocks.Lock.release(h)
    end,
    "with_lock (noop fun)" => fn ->
      HordeDistributedLocks.Lock.with_lock(
        :bench_wl, [ttl_ms: 60_000, wait_ms: 100], fn _ -> :ok end
      )
    end
  },
  time: 3,
  warmup: 1,
  parallel: 1
)
```

Expected, single-node: ~100-300 µs per acquire/release cycle (dominated by Horde's
inter-process messaging and CRDT update). Cross-node: add ~1-10 ms for delta-CRDT sync.

Under high contention (`parallel: 8`, single key), p99 grows because of retry loops.
Design contract: locks are coarse — don't put them on the hot path of a request handler.

---

## Resources

- [Horde docs — hexdocs](https://hexdocs.pm/horde/Horde.html) — concepts and guides
- [Horde GitHub](https://github.com/derekkraan/horde) — source and examples
- [Martin Kleppmann — "How to do distributed locking"](https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html) — the fencing-token argument
- [antirez — "Redis distributed locks"](https://redis.io/docs/manual/patterns/distributed-locks/) — the Redlock spec
- [`:global.trans/4` docs](https://www.erlang.org/doc/man/global.html#trans-4) — BEAM-native alternative
- [delta_crdt hex package](https://hexdocs.pm/delta_crdt/) — what Horde uses under the hood
- [Derek Kraan — "Building a distributed system with Horde"](https://derekkraan.com/blog/2020/06/01/announcing-horde-0-8/) — author's blog
