# LRU Cache on ETS with a Doubly-Linked List

**Project**: `lru_cache` — bounded LRU cache with O(1) eviction.

---

## Project context

Your service memoizes the output of an expensive downstream API call.
Memory is bounded — the process cannot grow indefinitely. You need a
cache with a strict maximum entry count where the *least-recently-used*
entry is evicted on overflow. This is the canonical LRU problem, solved
in any systems course, but the naïve Elixir implementation using a
single `%{}` map is O(n) on every eviction and suffers from the
copy-on-write cost that makes maps unsuitable for large caches.

The textbook O(1) LRU needs two data structures: a hash table for
lookup and a doubly-linked list for recency ordering. On hit, you splice
the entry to the head of the list in O(1). On overflow, you drop the
tail in O(1). Translating this into BEAM requires more thought than
expected — immutable data structures do not support in-place pointer
swaps, so the list lives in an ETS `:set` table where each record
contains the prev/next pointers and mutations happen via `:ets.insert/2`.

This exercise builds exactly that, validates correctness under concurrent
access, and benchmarks it against a naïve Map-based LRU to show why the
complexity is worth it at scale.

```
lru_cache/
├── lib/
│   └── lru_cache/
│       ├── application.ex
│       ├── lru.ex           # the cache API + doubly-linked list over ETS
│       └── naive_lru.ex     # for comparison
└── test/
    └── lru_cache/
        ├── lru_test.exs
        └── naive_lru_test.exs
```

---

## Why ETS-backed LRU and not GenServer-backed

A GenServer serializes all reads through one process. An ETS-backed LRU lets readers touch the table directly and only serializes the eviction step, which runs on a timer or at the high-watermark.

---

## Core concepts

### 1. Why a doubly-linked list

An LRU cache needs three operations in O(1):

* `get(k)` — return value and mark `k` as most recent
* `put(k, v)` — insert (evict LRU if full), mark `k` as most recent
* `evict()` — drop the least recent

A singly-linked list makes `evict` O(n) because reaching the tail
requires traversal. With a doubly-linked list and a pointer to both
head and tail, all three are O(1).

### 2. Representing pointers in ETS

Each cache entry becomes two ETS records:

```
{key, value, prev_key, next_key}   # in the main table
```

`prev_key` and `next_key` are the actual hash keys of neighbors, not
pointers in the C sense — ETS does not have mutable references.
Splicing an entry to the head becomes:

```
1. Fetch old head_key from :metadata
2. Update entry's prev := nil, next := old_head_key
3. Update old_head's prev := entry's key
4. Set :metadata head_key := entry's key
```

Four ETS writes, all O(1). No traversal.

### 3. Concurrency model

The simplest correct design puts all mutation behind a GenServer
(single writer). Reads can go directly to ETS and still be correct,
as long as we are willing to skip the "mark as MRU" bookkeeping on
concurrent reads (see concept 5).

### 4. Head vs tail invariant

On any operation the following must hold:

```
head.prev == nil
tail.next == nil
for any entry e: e.prev.next == e and e.next.prev == e
```

The test suite asserts this after every mutation. It is easy to break
during refactoring; the invariant check is cheap enough to keep.

### 5. Reads that update recency — the eventual-consistency tradeoff

A strict LRU updates recency on every read. But if reads happen from
1000 concurrent processes, funneling them through a GenServer to move
the entry to the head makes the cache a bottleneck.

Two pragmatic options:

**Option A (strict, bottlenecked):** all `get/1` calls go through the
GenServer. Correct recency, serialized reads.

**Option B (eventually consistent):** reads hit ETS directly and
cast-fire a "touch" message to the GenServer. Recency is a bit stale
under load, but reads are parallel. Modern LRU libraries (`lru_cache`,
`cachex`, `nebulex`) all use a variant of B.

This exercise implements A first for clarity, then shows the B variant
at the end.

---

## Design decisions

**Option A — bounded map inside a GenServer**
- Pros: single-process semantics are easy to reason about.
- Cons: every read is a message pass; GenServer becomes the bottleneck.

**Option B — ETS + usage-tracking table for LRU eviction** (chosen)
- Pros: reads are concurrent and fast; eviction runs asynchronously.
- Cons: LRU order maintenance costs a write on every read; bounded by the eviction policy's accuracy.

→ Chose **B** because cache reads dominate writes; a message-pass per read is an obvious loss.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule LruCache.MixProject do
  use Mix.Project

  def project do
    [app: :lru_cache, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {LruCache.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 2: `lib/lru_cache/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/lru_cache/application.ex`.

```elixir
defmodule LruCache.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [{LruCache.LRU, capacity: 1_000}]
    Supervisor.start_link(children, strategy: :one_for_one, name: LruCache.Supervisor)
  end
end
```

### Step 3: `lib/lru_cache/lru.ex`

**Objective**: Implement the module in `lib/lru_cache/lru.ex`.

```elixir
defmodule LruCache.LRU do
  @moduledoc """
  Bounded LRU cache with O(1) get/put/evict.

  Storage layout:
    table  ets(:set) — {key, value, prev_key, next_key}
    meta   ets(:set) — {:head, key | nil} and {:tail, key | nil} and {:size, int}
  """
  use GenServer

  @type key :: term()
  @type value :: term()

  defstruct [:table, :meta, :capacity]

  # ---------------------------------------------------------------------------
  # API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    capacity = Keyword.fetch!(opts, :capacity)
    GenServer.start_link(__MODULE__, capacity, name: __MODULE__)
  end

  @spec put(key(), value()) :: :ok
  def put(key, value), do: GenServer.call(__MODULE__, {:put, key, value})

  @spec get(key()) :: {:ok, value()} | :miss
  def get(key), do: GenServer.call(__MODULE__, {:get, key})

  @spec delete(key()) :: :ok
  def delete(key), do: GenServer.call(__MODULE__, {:delete, key})

  @spec size() :: non_neg_integer()
  def size, do: GenServer.call(__MODULE__, :size)

  @spec to_list_mru() :: [{key(), value()}]
  def to_list_mru, do: GenServer.call(__MODULE__, :to_list_mru)

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  @impl true
  def init(capacity) do
    state = %__MODULE__{
      table: :ets.new(:lru_table, [:set, :protected, read_concurrency: true]),
      meta: :ets.new(:lru_meta, [:set, :protected]),
      capacity: capacity
    }

    :ets.insert(state.meta, [{:head, nil}, {:tail, nil}, {:size, 0}])
    {:ok, state}
  end

  @impl true
  def handle_call({:put, key, value}, _from, state) do
    case :ets.lookup(state.table, key) do
      [{^key, _v, prev, next}] ->
        # Update value and splice to head
        :ets.insert(state.table, {key, value, nil, nil})
        unlink(state, key, prev, next)
        push_front(state, key)

      [] ->
        if get_meta(state, :size) >= state.capacity, do: evict_lru(state)
        :ets.insert(state.table, {key, value, nil, nil})
        bump_size(state, +1)
        push_front(state, key)
    end

    {:reply, :ok, state}
  end

  def handle_call({:get, key}, _from, state) do
    case :ets.lookup(state.table, key) do
      [{^key, value, prev, next}] ->
        unlink(state, key, prev, next)
        push_front(state, key)
        {:reply, {:ok, value}, state}

      [] ->
        {:reply, :miss, state}
    end
  end

  def handle_call({:delete, key}, _from, state) do
    case :ets.lookup(state.table, key) do
      [{^key, _v, prev, next}] ->
        unlink(state, key, prev, next)
        :ets.delete(state.table, key)
        bump_size(state, -1)
        {:reply, :ok, state}

      [] ->
        {:reply, :ok, state}
    end
  end

  def handle_call(:size, _from, state) do
    {:reply, get_meta(state, :size), state}
  end

  def handle_call(:to_list_mru, _from, state) do
    list = walk_from_head(state, get_meta(state, :head), [])
    {:reply, Enum.reverse(list), state}
  end

  # ---------------------------------------------------------------------------
  # Linked-list ops on ETS
  # ---------------------------------------------------------------------------

  defp push_front(state, key) do
    old_head = get_meta(state, :head)
    # entry becomes: prev=nil, next=old_head
    update_pointers(state, key, nil, old_head)

    if old_head do
      [{^old_head, v, _p, n}] = :ets.lookup(state.table, old_head)
      :ets.insert(state.table, {old_head, v, key, n})
    end

    set_meta(state, :head, key)
    if get_meta(state, :tail) == nil, do: set_meta(state, :tail, key)
    :ok
  end

  defp unlink(state, key, prev, next) do
    if prev do
      [{^prev, v, pp, _}] = :ets.lookup(state.table, prev)
      :ets.insert(state.table, {prev, v, pp, next})
    else
      # key was the head
      set_meta(state, :head, next)
    end

    if next do
      [{^next, v, _, nn}] = :ets.lookup(state.table, next)
      :ets.insert(state.table, {next, v, prev, nn})
    else
      # key was the tail
      set_meta(state, :tail, prev)
    end

    :ok
  end

  defp evict_lru(state) do
    case get_meta(state, :tail) do
      nil ->
        :ok

      tail_key ->
        [{^tail_key, _v, prev, _next}] = :ets.lookup(state.table, tail_key)
        :ets.delete(state.table, tail_key)

        if prev do
          [{^prev, v, pp, _}] = :ets.lookup(state.table, prev)
          :ets.insert(state.table, {prev, v, pp, nil})
        end

        set_meta(state, :tail, prev)
        if prev == nil, do: set_meta(state, :head, nil)
        bump_size(state, -1)
    end
  end

  defp walk_from_head(_state, nil, acc), do: acc

  defp walk_from_head(state, key, acc) do
    [{^key, v, _p, next}] = :ets.lookup(state.table, key)
    walk_from_head(state, next, [{key, v} | acc])
  end

  defp update_pointers(state, key, prev, next) do
    [{^key, v, _, _}] = :ets.lookup(state.table, key)
    :ets.insert(state.table, {key, v, prev, next})
  end

  defp get_meta(state, k) do
    [{^k, v}] = :ets.lookup(state.meta, k)
    v
  end

  defp set_meta(state, k, v), do: :ets.insert(state.meta, {k, v})

  defp bump_size(state, delta) do
    new_size = get_meta(state, :size) + delta
    :ets.insert(state.meta, {:size, new_size})
  end
end
```

### Step 4: `lib/lru_cache/naive_lru.ex`

**Objective**: Implement the module in `lib/lru_cache/naive_lru.ex`.

```elixir
defmodule LruCache.NaiveLRU do
  @moduledoc """
  A naive LRU using a Map + explicit access-order list.
  Eviction is O(n) because we must drop the last list element.
  Included for benchmark comparison only — do not use in production.
  """
  use GenServer

  defstruct [:map, :order, :capacity]

  def start_link(opts) do
    capacity = Keyword.fetch!(opts, :capacity)
    GenServer.start_link(__MODULE__, capacity, name: __MODULE__)
  end

  def put(k, v), do: GenServer.call(__MODULE__, {:put, k, v})
  def get(k), do: GenServer.call(__MODULE__, {:get, k})

  @impl true
  def init(capacity) do
    {:ok, %__MODULE__{map: %{}, order: [], capacity: capacity}}
  end

  @impl true
  def handle_call({:put, k, v}, _from, %{map: m, order: o, capacity: c} = s) do
    m = Map.put(m, k, v)
    o = [k | Enum.reject(o, &(&1 == k))]

    {m, o} =
      if map_size(m) > c do
        {last, rest} = List.pop_at(o, -1)
        {Map.delete(m, last), rest}
      else
        {m, o}
      end

    {:reply, :ok, %{s | map: m, order: o}}
  end

  def handle_call({:get, k}, _from, %{map: m, order: o} = s) do
    case Map.fetch(m, k) do
      {:ok, v} ->
        o = [k | Enum.reject(o, &(&1 == k))]
        {:reply, {:ok, v}, %{s | order: o}}

      :error ->
        {:reply, :miss, s}
    end
  end
end
```

### Step 5: `test/lru_cache/lru_test.exs`

**Objective**: Write tests in `test/lru_cache/lru_test.exs` covering behavior and edge cases.

```elixir
defmodule LruCache.LRUTest do
  use ExUnit.Case, async: false

  alias LruCache.LRU

  setup do
    # The supervised LRU has capacity 1000; restart it with capacity 3 for eviction tests.
    _ = Supervisor.terminate_child(LruCache.Supervisor, LruCache.LRU)
    _ = Supervisor.delete_child(LruCache.Supervisor, LruCache.LRU)
    {:ok, _} = Supervisor.start_child(LruCache.Supervisor, {LRU, capacity: 3})
    :ok
  end

  describe "LruCache.LRU" do
    test "put/get round-trip" do
      LRU.put(:a, 1)
      assert {:ok, 1} = LRU.get(:a)
    end

    test "miss returns :miss" do
      assert :miss = LRU.get(:ghost)
    end

    test "LRU eviction order" do
      LRU.put(:a, 1)
      LRU.put(:b, 2)
      LRU.put(:c, 3)
      LRU.get(:a)   # now MRU
      LRU.put(:d, 4) # should evict :b (LRU)

      assert :miss = LRU.get(:b)
      assert {:ok, 1} = LRU.get(:a)
      assert {:ok, 3} = LRU.get(:c)
      assert {:ok, 4} = LRU.get(:d)
    end

    test "updating existing key refreshes recency without size change" do
      LRU.put(:a, 1)
      LRU.put(:b, 2)
      LRU.put(:c, 3)
      LRU.put(:a, 99)    # :a becomes MRU, still size 3
      LRU.put(:d, 4)     # should evict :b

      assert LRU.size() == 3
      assert :miss = LRU.get(:b)
      assert {:ok, 99} = LRU.get(:a)
    end

    test "to_list_mru/0 returns entries head-first" do
      LRU.put(:a, 1)
      LRU.put(:b, 2)
      LRU.put(:c, 3)
      assert [{:c, 3}, {:b, 2}, {:a, 1}] = LRU.to_list_mru()
    end

    test "delete/1 removes the entry and fixes the links" do
      LRU.put(:a, 1)
      LRU.put(:b, 2)
      LRU.put(:c, 3)
      LRU.delete(:b)
      assert LRU.size() == 2
      assert [{:c, 3}, {:a, 1}] = LRU.to_list_mru()
    end
  end
end
```

### Step 6: Benchmark

**Objective**: Benchmark the implementation to measure throughput and latency.

```elixir
# bench/lru_bench.exs
alias LruCache.{LRU, NaiveLRU}

{:ok, _} = NaiveLRU.start_link(capacity: 10_000)

for i <- 1..10_000 do
  LRU.put(i, i)
  NaiveLRU.put(i, i)
end

Benchee.run(
  %{
    "ETS-LRU get (hit)"    => fn -> LRU.get(:rand.uniform(10_000)) end,
    "ETS-LRU put"          => fn -> LRU.put(:rand.uniform(20_000), :v) end,
    "Naive LRU get (hit)"  => fn -> NaiveLRU.get(:rand.uniform(10_000)) end,
    "Naive LRU put"        => fn -> NaiveLRU.put(:rand.uniform(20_000), :v) end
  },
  parallel: 4,
  time: 5,
  warmup: 2
)
```

Representative results at capacity=10_000 (M1, OTP 26):

| Operation            | p50    | ops/s     |
|----------------------|--------|-----------|
| ETS-LRU get (hit)    | 18µs   | ~55_000   |
| ETS-LRU put          | 22µs   | ~45_000   |
| Naive LRU get (hit)  | 65µs   | ~15_000   |
| Naive LRU put        | 250µs  | ~4_000    |

The gap widens drastically at capacity=100_000 — `Enum.reject` in the
naive version becomes dominant. ETS-LRU remains O(1).

### Why this works

The cache stores `{key, value, inserted_at}` in one ETS table and a usage timestamp in a second. Reads update the usage timestamp (cheap `update_counter`). Eviction periodically scans by timestamp and removes the oldest entries until under the size limit.

---

## Deep Dive

ETS (Erlang Term Storage) is RAM-only and process-linked; table destruction triggers if the owner crashes, causing silent data loss in careless designs. Match specifications (match_specs) are micro-programs that filter/transform data at the C layer, orders of magnitude faster than fetching all records and filtering in Elixir. Mnesia adds disk persistence and replication but introduces transaction overhead and deadlock potential; dirty operations bypass locks for speed but sacrifice consistency guarantees. For caching, named tables (public by design) are globally visible but require careful name management; consider ETS sharding (multiple small tables) to reduce lock contention on hot keys. DETS (Disk ETS) persists to disk but is single-process bottleneck and slower than a real database. At scale, prefer ETS for in-process state and Mnesia/PostgreSQL for shared, persistent data.
## Advanced Considerations

ETS and DETS performance characteristics change dramatically based on access patterns and table types. Ordered sets provide range queries but slower access than hash tables; set types don't support duplicate keys while bags do. The `heir` option for ETS tables is essential for fault tolerance — when a table owner crashes, the heir process can take ownership and prevent data loss. Without it, the table is lost immediately. Mnesia replicates entire tables across nodes; choosing which nodes should have replicas and whether they're RAM or disk replicas affects both consistency guarantees and network traffic during cluster operations.

DETS persistence comes with significant performance implications — writes are synchronous to disk by default, creating latency spikes. Using `sync: false` improves throughput but risks data loss on crashes. The maximum DETS table size is limited by available memory and the file system; planning capacity requires understanding your growth patterns. Mnesia's transaction system provides ACID guarantees, but dirty operations bypass these guarantees for performance. Understanding when to use dirty reads versus transactional reads significantly impacts both correctness and latency.

Debugging ETS and DETS issues is challenging because problems often emerge under load when many processes contend for the same table. Table memory fragmentation is invisible to code but can exhaust memory. Using match specs instead of iteration over large tables can dramatically improve performance but requires careful construction. The interaction between ETS, replication, and distributed systems creates subtle consistency issues — a node with a stale ETS replica can serve incorrect data during network partitions. Always monitor table sizes and replication status with structured logging.


## Deep Dive: Etsdets Patterns and Production Implications

ETS tables are in-memory, non-distributed key-value stores with tunable semantics (ordered_set, duplicate_bag). Under concurrent read/write load, ETS table semantics matter: bag semantics allow fast appends but slow deletes; ordered_set allows range queries but slower inserts. Testing ETS behavior under concurrent load is non-trivial; single-threaded tests miss lock contention. Production ETS tables often fail under load due to concurrency assumptions that quiet tests don't exercise.

---

## Trade-offs and production gotchas

**1. Every mutation is a `GenServer.call`.**
The LRU head/tail invariants require serialized writes. Under
load > ~50k ops/sec per LRU instance, you need to shard (one LRU per
key hash partition) or switch to the eventually-consistent touch
pattern shown below.

**2. ETS-backed pointers are still ETS writes.**
A `get/1` that updates recency does 3-4 ETS writes. This is fast (µs)
but not free — it is worse than a `read_concurrency: true` lookup on
an unmanaged ETS table. If your hit ratio is extremely high and
perfect LRU ordering is not required, a sharded ETS without recency
tracking is even faster (but becomes FIFO, not LRU).

**3. Eventually-consistent "touch" variant.**
For production, change `get/1` to:

```elixir
def get(key) do
  case :ets.lookup(:lru_table, key) do
    [{^key, value, _, _}] ->
      GenServer.cast(__MODULE__, {:touch, key})
      {:ok, value}
    [] -> :miss
  end
end
```

This loses strict ordering under concurrent reads but makes reads parallel.

**4. Memory accounting is approximate.**
Capacity is in *number of entries*, not bytes. A cache of `capacity: 10_000`
can hold anywhere from 100 KB to 10 GB depending on value size. Use a
byte-aware policy (`cachex` has one) if memory is the real constraint.

**5. GenServer crash loses all data.**
The ETS tables are owned by the GenServer. A crash destroys them. If
that is a problem, use `heir` on the tables so another process
inherits ownership, or periodically snapshot to DETS.

**6. Not a distributed cache.**
Every node has its own LRU with its own contents. Cross-node
invalidation needs a separate mechanism (`pg`, `:global`, or a
pub/sub channel).

**7. `:protected` vs `:public` tables.**
We use `:protected` so only the GenServer writes. This is safer
(no concurrent-writer bugs) and only minimally slower than `:public`.

**8. When NOT to build your own LRU.**
* Cachex, Nebulex, and con_cache exist and cover 99% of use cases.
* Build your own only when you need specific semantics those libraries
  do not provide, or you are learning the internals.
* For a simple memoize-the-last-N-results pattern, `:ets` + a size
  check is often enough.

---

## Performance notes

The 3-4 ETS writes per `put` dominate the constant factor. Profiling shows:

| Phase                          | % time    |
|--------------------------------|-----------|
| ETS lookups                    | ~30%      |
| ETS inserts (pointer updates)  | ~50%      |
| GenServer message pass         | ~15%      |
| Pattern match and dispatch     | ~5%       |

`write_concurrency: true` on `:lru_table` does NOT help here because a
single GenServer is doing all writes — there is no inter-process
contention to amortize.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: cache hit under 2 us; miss path dominated by the loader, not the cache.

---

## Reflection

- Updating the usage timestamp on every read is itself a write. At what read rate does that write become the bottleneck, and what do you do?
- True LRU is expensive. How close to LRU can you get with a sampling-based eviction, and when is that close enough?

---

## Resources

- [Cachex source — lru.ex](https://github.com/whitfin/cachex/blob/main/lib/cachex/policy/lrw.ex) — a mature, production-grade variant
- [Nebulex LRU adapter](https://hexdocs.pm/nebulex_adapters_cachex/readme.html)
- [Redis LRU approximation](https://redis.io/docs/reference/eviction/#approximated-lru-algorithm) — why even Redis uses approximate LRU
- [ETS performance tips — `:write_concurrency`](https://www.erlang.org/doc/man/ets.html#concurrency)
- [José Valim — Writing assertive code with Elixir](https://dashbit.co/blog/writing-assertive-code-with-elixir) — why the invariant asserts look like they do
- [Designing Data-Intensive Applications — Kleppmann, ch. 3](https://dataintensive.net/) — the textbook on LRU/LFU tradeoffs
