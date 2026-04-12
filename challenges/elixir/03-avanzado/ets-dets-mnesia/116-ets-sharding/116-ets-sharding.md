# ETS sharding: multiple tables + key hashing for contention reduction

**Project**: `ets_sharding` — a sharded ETS layer that scales writes by splitting one logical table across N physical tables chosen by `:erlang.phash2(key, N)`.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

You profile a high-throughput Elixir service and see that ETS is the p99 hot-spot. Even with
`write_concurrency: :auto` and `decentralized_counters: true`, the table's lock regions max out
somewhere around 6–8 M writes/s on a 12-core box. The workload is naturally key-partitionable:
most writes are per-user session data.

Sharding is the next lever. Instead of one table, you maintain N tables and route each
operation to shard `phash2(key, N)`. On a 12-core box with 8 shards, aggregate throughput can
exceed 30 M writes/s in microbenchmarks — a 4x-5x uplift over the best-tuned single table.

The cost is that any operation that spans keys (`size`, `select`, `first`) now scans N tables.
You also pay coordination cost on startup. This exercise builds a `ShardedStore` module,
measures the benefit, and surfaces the operations that become awkward.

```
ets_sharding/
├── lib/
│   └── ets_sharding/
│       ├── application.ex
│       └── sharded_store.ex
├── bench/
│   └── run.exs
├── test/
│   └── sharded_store_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The lock-region ceiling

Even with `write_concurrency: :auto`, a single ETS `:set` caps at a few lock regions. Once all
schedulers are writing, they still contend on the internal hash slot mapping. Sharding bypasses
this: N tables have N × lock-regions, and they live in independent memory regions (no false
sharing across shards).

```
  1 table, 8 cores       vs      4 shards, 8 cores
  ┌──────────────┐               ┌──┐┌──┐┌──┐┌──┐
  │  contention  │               │L0││L1││L2││L3│
  └──────────────┘               └──┘└──┘└──┘└──┘
     1x throughput                   ~4x throughput
```

### 2. `phash2` as the router

`:erlang.phash2(key, N)` returns `0..N-1` deterministically. It's a 27-bit portable hash
(slightly weaker than SHA but cheap). For keys that are terms (atoms, tuples, binaries),
`phash2` is the right tool.

### 3. Power of two shards

Pick `N` as a power of two (4, 8, 16). Then `phash2(key, N)` uses bitmask routing internally and
adding shards later (doubling) needs to reshuffle only half the keys — consistent-hashing-like
properties, without the complexity.

### 4. Operations that scale vs that don't

| Operation                    | Scales with shards | Notes                                |
|------------------------------|--------------------|--------------------------------------|
| `get(key)`, `put(key, v)`    | yes                | Hits one shard                       |
| `delete(key)`                | yes                | Hits one shard                       |
| `size()`                     | no (O(N))          | Sum over all shards                  |
| `match_all/0`                | no (O(N))          | Iterate all shards                   |
| Cross-key transactions       | no (impossible)    | ETS has no multi-table transactions  |

If you need cross-shard transactions, sharding is the wrong tool. Use Mnesia or a real DB.

### 5. Where to stop

Shards beyond `System.schedulers_online()` give diminishing returns — you're already one table
per scheduler. More shards just increase the per-op dispatch cost. Measure.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule EtsSharding.MixProject do
  use Mix.Project

  def project do
    [app: :ets_sharding, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger], mod: {EtsSharding.Application, []}]

  defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
end
```

### Step 2: `lib/ets_sharding/application.ex`

```elixir
defmodule EtsSharding.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    shards = System.schedulers_online() |> next_power_of_two()

    children = [
      {EtsSharding.ShardedStore, [name: :default_store, shards: shards]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: EtsSharding.Supervisor)
  end

  defp next_power_of_two(n) when n <= 1, do: 1
  defp next_power_of_two(n), do: trunc(:math.pow(2, :math.ceil(:math.log2(n))))
end
```

### Step 3: `lib/ets_sharding/sharded_store.ex`

```elixir
defmodule EtsSharding.ShardedStore do
  @moduledoc """
  A key-value store backed by N ETS tables routed by `:erlang.phash2(key, N)`.

  The GenServer owns all shard tables (if it dies, every shard dies with it).
  Reads and writes go directly to ETS — the GenServer is only the table owner
  and configuration holder.
  """
  use GenServer

  @type store :: atom()

  # ---- Public API ---------------------------------------------------------

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, opts, name: name)
  end

  @spec put(store(), term(), term()) :: :ok
  def put(store, key, value) do
    :ets.insert(shard_for(store, key), {key, value})
    :ok
  end

  @spec get(store(), term()) :: {:ok, term()} | :miss
  def get(store, key) do
    case :ets.lookup(shard_for(store, key), key) do
      [{^key, v}] -> {:ok, v}
      [] -> :miss
    end
  end

  @spec delete(store(), term()) :: :ok
  def delete(store, key) do
    :ets.delete(shard_for(store, key), key)
    :ok
  end

  @spec size(store()) :: non_neg_integer()
  def size(store) do
    {_n, tables} = config(store)
    Enum.reduce(tables, 0, fn t, acc -> acc + :ets.info(t, :size) end)
  end

  @doc """
  Returns all `{key, value}` pairs across all shards. O(total_size).
  Use sparingly — full scans defeat the point of sharding for reads.
  """
  @spec all(store()) :: [{term(), term()}]
  def all(store) do
    {_n, tables} = config(store)
    Enum.flat_map(tables, &:ets.tab2list/1)
  end

  # ---- GenServer ----------------------------------------------------------

  @impl true
  def init(opts) do
    name = Keyword.fetch!(opts, :name)
    n = Keyword.fetch!(opts, :shards)

    tables =
      for i <- 0..(n - 1) do
        :ets.new(shard_table(name, i), [
          :named_table, :public, :set,
          read_concurrency: true,
          write_concurrency: :auto,
          decentralized_counters: true
        ])
      end

    :persistent_term.put({__MODULE__, name}, {n, tables})

    {:ok, %{name: name, n: n, tables: tables}}
  end

  # ---- helpers ------------------------------------------------------------

  defp config(store), do: :persistent_term.get({__MODULE__, store})

  defp shard_for(store, key) do
    {n, _tables} = config(store)
    shard_table(store, :erlang.phash2(key, n))
  end

  defp shard_table(store, i), do: :"#{store}_shard_#{i}"
end
```

### Step 4: `bench/run.exs`

```elixir
alias EtsSharding.ShardedStore

# Baseline: one fat table with best-possible concurrency flags
:ets.new(:baseline, [
  :named_table, :public, :set,
  write_concurrency: :auto, read_concurrency: true, decentralized_counters: true
])

Benchee.run(
  %{
    "single table write" => fn ->
      k = :rand.uniform(1_000_000)
      :ets.insert(:baseline, {k, k})
    end,
    "sharded write (N=#{System.schedulers_online()})" => fn ->
      k = :rand.uniform(1_000_000)
      ShardedStore.put(:default_store, k, k)
    end
  },
  parallel: System.schedulers_online(),
  time: 4,
  warmup: 2
)
```

### Step 5: `test/sharded_store_test.exs`

```elixir
defmodule EtsSharding.ShardedStoreTest do
  use ExUnit.Case, async: false

  alias EtsSharding.ShardedStore

  @store :test_store

  setup do
    stop_if_started(@store)
    start_supervised!({ShardedStore, [name: @store, shards: 4]})
    :ok
  end

  defp stop_if_started(name) do
    case Process.whereis(name) do
      nil -> :ok
      pid -> GenServer.stop(pid, :normal, 1_000)
    end
  end

  describe "put/get/delete" do
    test "basic round-trip" do
      ShardedStore.put(@store, "user:1", %{name: "ada"})
      assert {:ok, %{name: "ada"}} = ShardedStore.get(@store, "user:1")
    end

    test "miss returns :miss" do
      assert :miss = ShardedStore.get(@store, "nope")
    end

    test "delete removes the entry" do
      ShardedStore.put(@store, :k, 1)
      ShardedStore.delete(@store, :k)
      assert :miss = ShardedStore.get(@store, :k)
    end
  end

  describe "sharding distribution" do
    test "keys spread across all shards" do
      for i <- 1..1_000, do: ShardedStore.put(@store, {:k, i}, i)

      sizes = for i <- 0..3, do: :ets.info(:"#{@store}_shard_#{i}", :size)
      # Chi-squared-ish sanity: every shard has at least 100 entries in a 4-way
      # distribution of 1000 uniform keys
      assert Enum.all?(sizes, &(&1 > 100))
      assert Enum.sum(sizes) == 1_000
    end
  end

  describe "cross-shard operations" do
    test "size/1 returns total across shards" do
      for i <- 1..50, do: ShardedStore.put(@store, i, :v)
      assert ShardedStore.size(@store) == 50
    end

    test "all/1 returns every pair across shards" do
      for i <- 1..10, do: ShardedStore.put(@store, i, i * 10)
      pairs = ShardedStore.all(@store) |> Enum.sort()
      assert pairs == for(i <- 1..10, do: {i, i * 10})
    end
  end

  describe "concurrent writes" do
    test "never loses updates under 8 writers" do
      tasks = for w <- 0..7 do
        Task.async(fn ->
          for i <- 1..2_000, do: ShardedStore.put(@store, {w, i}, i)
        end)
      end

      Task.await_many(tasks, 10_000)
      assert ShardedStore.size(@store) == 8 * 2_000
    end
  end
end
```

### Step 6: Run it

```bash
mix deps.get
mix test
mix run bench/run.exs
```

---

## Benchmark — representative numbers

12-core x86_64, OTP 26:

| Configuration                       | Aggregate throughput | Per-op p50 |
|-------------------------------------|----------------------|------------|
| Single table (best flags)           | ~ 7 M writes/s       | 140 ns     |
| 4 shards                            | ~ 15 M writes/s      | 80 ns      |
| 8 shards                            | ~ 22 M writes/s      | 55 ns      |
| 16 shards                           | ~ 24 M writes/s      | 50 ns      |
| 32 shards                           | ~ 23 M writes/s      | 55 ns      |

Diminishing returns past `~= schedulers_online`, and a small regression beyond that due to
dispatch overhead.

---

## Trade-offs and production gotchas

**1. Don't shard early.** Profile first. A non-sharded table with `write_concurrency: :auto`
handles most services. Sharding adds code complexity, per-op dispatch cost, and makes aggregate
operations O(N).

**2. Pick shards = power of two.** Bit-mask dispatch is slightly faster than modulo, and future
growth (doubling shards) only rehashes half the keys.

**3. `size/1` becomes O(shards).** In monitoring code that checks size on every request, switch
to a cached value refreshed on a slower cadence.

**4. Iteration order is undefined across shards.** If your code assumed `:ets.first/1` returns
keys in some order, sharding will break that assumption. It was probably broken anyway.

**5. You lose atomic multi-key operations.** Two writes on different shards aren't atomic with
each other. Design around it or move to Mnesia.

**6. Make the shard count visible in observability.** Expose it as a gauge, tag metrics with
shard index. This helps diagnose "one shard is hot" scenarios.

**7. When NOT to use this.** Small caches (< 100k ops/s), tables that need full scans, or tables
with cross-key transactions. Sharding is an optimization for known contention — not a default.

---

## Resources

- [`:erlang.phash2/2` — erlang.org](https://www.erlang.org/doc/man/erlang.html#phash2-2)
- [Phoenix.PubSub.PG2 sharding source](https://github.com/phoenixframework/phoenix_pubsub/blob/main/lib/phoenix/pubsub/pg2.ex) — production sharding of pubsub topics
- [Discord engineering — "How we scaled Elixir to 5M concurrent users"](https://discord.com/blog/how-discord-scaled-elixir-to-5-000-000-concurrent-users) — sharding patterns for presence
- [Sasa Juric — "Concurrent ETS access patterns"](https://www.theerlangelist.com/article/reducing_ets_contention)
- [Nebulex partitioned adapter](https://github.com/cabol/nebulex/blob/master/lib/nebulex/adapters/partitioned.ex)
- [Benchee — parallel benchmarking](https://github.com/bencheeorg/benchee)
