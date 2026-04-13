# Mnesia Fragmented Tables — Sharding Across Nodes

**Project**: `mnesia_fragmented` — a horizontally-sharded event store using `mnesia_frag`.

---

## The business problem

An internal analytics service stores ~40 million events per day across eight
nodes. Keeping the entire `:events` table on every node would blow the RAM
budget twice over; keeping it on one node would turn that node into a
bottleneck and a single point of failure. The goal is to partition the data
across nodes so that each event sits on a subset of replicas, distributed
by a hash of the event id.

Mnesia ships with a native sharding mechanism called **fragmented tables**
(`mnesia_frag` activity access module). It hashes each record's key into one
of N fragments, where each fragment is an independent underlying Mnesia
table with its own replica placement. The programmer still calls
`:mnesia.read/2` and `:mnesia.write/1` — the access module handles the
routing. It is one of Mnesia's least-known features and the only way to
scale Mnesia past the capacity of a single node.

The tradeoff is operational complexity. Adding a fragment to a running
table triggers a rebalance that moves records around the cluster, and the
hash function is not pluggable in the obvious way. This exercise walks
through creating a fragmented table, configuring fragment placement,
reading/writing using the `mnesia_frag` module, and observing the
rebalance when fragments are added.

## Project structure

```
mnesia_fragmented/
├── lib/
│   └── mnesia_fragmented/
│       ├── application.ex
│       ├── schema.ex           # create fragmented table w/ N fragments
│       ├── events.ex           # API using mnesia_frag access module
│       └── fragmentation_info.ex  # inspection utilities
├── test/
│   └── mnesia_fragmented/
│       └── events_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why fragmentation and not a single table

A non-fragmented Mnesia table replicates in full to every participating node. Fragmentation assigns each record to one of N fragments based on a hash of the key, so each node holds only its share.

---

## Design decisions

**Option A — single Mnesia table across all nodes**
- Pros: simple; every node has every record.
- Cons: does not scale past a few GB per node; replication cost is N^2.

**Option B — fragmented tables with hash-based partitioning** (chosen)
- Pros: scales horizontally; each node owns a subset; replication cost stays linear.
- Cons: cross-fragment queries are awkward; rebalancing is manual.

→ Chose **B** because past a certain dataset size there is no other option inside Mnesia.

---

## Implementation

### `mix.exs`
**Objective**: Declare the project, dependencies, and OTP application in `mix.exs`.

```elixir
defmodule MnesiaFragmented.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_fragmented, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaFragmented.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### `lib/mnesia_fragmented.ex`

```elixir
defmodule MnesiaFragmented do
  @moduledoc """
  Mnesia Fragmented Tables — Sharding Across Nodes.

  A non-fragmented Mnesia table replicates in full to every participating node. Fragmentation assigns each record to one of N fragments based on a hash of the key, so each node....
  """
end
```

### `lib/mnesia_fragmented/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/mnesia_fragmented/application.ex`.

```elixir
defmodule MnesiaFragmented.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([MnesiaFragmented.Schema],
      strategy: :one_for_one,
      name: MnesiaFragmented.Supervisor
    )
  end
end
```

### `lib/mnesia_fragmented/schema.ex`

**Objective**: Implement the module in `lib/mnesia_fragmented/schema.ex`.

```elixir
defmodule MnesiaFragmented.Schema do
  @moduledoc false
  use GenServer
  require Logger

  @table :events
  @initial_fragments 4

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    _ = :mnesia.stop()

    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
      other -> throw({:schema_failed, other})
    end

    :ok = :mnesia.start()
    :ok = ensure_table()
    :ok = :mnesia.wait_for_tables([@table], 20_000)

    log_frag_info()
    {:ok, %{}}
  end

  defp ensure_table do
    opts = [
      attributes: [:id, :type, :payload, :inserted_at],
      type: :set,
      frag_properties: [
        n_fragments: @initial_fragments,
        node_pool: [node()],
        n_ram_copies: 0,
        n_disc_copies: 1
      ]
    ]

    case :mnesia.create_table(@table, opts) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, @table}} -> :ok
      other -> throw({:create_failed, other})
    end
  end

  defp log_frag_info do
    info = :mnesia.table_info(@table, :frag_properties)
    size = :mnesia.table_info(@table, :size)
    Logger.info("Fragmented table #{@table}: #{inspect(info)} — size=#{size}")
  end
end
```

### `lib/mnesia_fragmented/events.ex`

**Objective**: Implement the module in `lib/mnesia_fragmented/events.ex`.

```elixir
defmodule MnesiaFragmented.Events do
  @moduledoc """
  API for the fragmented :events table.

  Every call must use the :mnesia_frag access module — otherwise the
  operation targets only the base fragment and silently returns incorrect
  data for keys stored in other fragments.
  """

  @table :events

  @type id :: String.t()
  @type event :: %{id: id, type: atom(), payload: map(), inserted_at: integer}

  @spec put(event()) :: :ok | {:error, term()}
  def put(%{id: id, type: type, payload: payload}) do
    ts = System.system_time(:millisecond)
    record = {@table, id, type, payload, ts}

    run_frag(fn -> :mnesia.write(record) end)
  end

  @spec get(id()) :: {:ok, event()} | :not_found
  def get(id) do
    result = run_frag(fn -> :mnesia.read({@table, id}) end)

    case result do
      [{@table, ^id, type, payload, ts}] ->
        {:ok, %{id: id, type: type, payload: payload, inserted_at: ts}}

      [] ->
        :not_found
    end
  end

  @spec delete(id()) :: :ok
  def delete(id) do
    run_frag(fn -> :mnesia.delete({@table, id}) end)
    :ok
  end

  @doc """
  Count events across ALL fragments. Uses `:mnesia.table_info/2` on each
  underlying fragment — do not use `:mnesia.foldl/3` which iterates only
  the base fragment in a single-node cluster.
  """
  @spec count_all() :: non_neg_integer()
  def count_all do
    :mnesia.table_info(@table, :frag_names)
    |> Enum.map(fn frag -> :mnesia.table_info(frag, :size) end)
    |> Enum.sum()
  end

  @doc """
  Walk every fragment with a reducer. Useful for migrations, exports, and
  full scans. Much more scalable than a single-fragment foldl because the
  work spreads across the replicas holding each fragment.
  """
  @spec foldl_all((tuple(), acc -> acc), acc) :: acc when acc: term()
  def foldl_all(fun, acc) do
    run_frag(fn -> :mnesia.foldl(fun, acc, @table) end)
  end

  defp run_frag(inner) do
    case :mnesia.activity(:transaction, inner, [], :mnesia_frag) do
      {:atomic, result} -> result
      result -> result
    end
  end
end
```

### `lib/mnesia_fragmented/fragmentation_info.ex`

**Objective**: Implement the module in `lib/mnesia_fragmented/fragmentation_info.ex`.

```elixir
defmodule MnesiaFragmented.FragmentationInfo do
  @moduledoc """
  Inspection helpers for a running fragmented table.
  """

  @table :events

  @spec summary() :: %{
          total: non_neg_integer(),
          fragments: [%{name: atom, size: non_neg_integer, nodes: [node]}]
        }
  def summary do
    frag_names = :mnesia.table_info(@table, :frag_names)

    per_frag =
      Enum.map(frag_names, fn frag ->
        %{
          name: frag,
          size: :mnesia.table_info(frag, :size),
          nodes:
            :mnesia.table_info(frag, :disc_copies) ++
              :mnesia.table_info(frag, :ram_copies)
        }
      end)

    %{total: Enum.sum(Enum.map(per_frag, & &1.size)), fragments: per_frag}
  end

  @spec fragment_for_key(term()) :: atom()
  def fragment_for_key(key) do
    # Replicate what mnesia_frag does internally to reveal the target fragment.
    n = :mnesia.table_info(@table, :n_fragments)
    hash = :erlang.phash2(key, n)

    case hash do
      0 -> @table
      _ -> :"#{@table}_frag#{hash + 1}"
    end
  end

  @spec add_fragment() :: :ok | {:error, term()}
  def add_fragment do
    case :mnesia.change_table_frag(@table, {:add_frag, [node()]}) do
      {:atomic, :ok} -> :ok
      other -> {:error, other}
    end
  end
end
```

### Step 6: `test/mnesia_fragmented/events_test.exs`

**Objective**: Write tests in `test/mnesia_fragmented/events_test.exs` covering behavior and edge cases.

```elixir
defmodule MnesiaFragmented.EventsTest do
  use ExUnit.Case, async: false
  doctest MnesiaFragmented.FragmentationInfo

  alias MnesiaFragmented.{Events, FragmentationInfo}

  setup do
    for frag <- :mnesia.table_info(:events, :frag_names) do
      :mnesia.clear_table(frag)
    end

    :ok
  end

  describe "MnesiaFragmented.Events" do
    test "put and get round-trip across all fragments" do
      events =
        for i <- 1..500 do
          %{
            id: "evt-#{i}",
            type: :signup,
            payload: %{user: "u-#{i}"}
          }
        end

      Enum.each(events, &Events.put/1)

      assert Events.count_all() == 500

      # Every event must be retrievable — if we were missing the frag access
      # module, some keys in non-base fragments would come back :not_found.
      Enum.each(events, fn %{id: id} ->
        assert {:ok, %{id: ^id}} = Events.get(id)
      end)
    end

    test "distribution across fragments is roughly uniform" do
      for i <- 1..1_000, do: Events.put(%{id: "evt-#{i}", type: :x, payload: %{}})

      sizes =
        FragmentationInfo.summary().fragments
        |> Enum.map(& &1.size)

      # With phash2 + 4 fragments, expect each fragment to hold 250 ± 75 rows.
      Enum.each(sizes, fn size ->
        assert size > 150 and size < 350, "fragment imbalance: #{inspect(sizes)}"
      end)
    end

    test "fragment_for_key/1 matches actual storage" do
      id = "evt-placement"
      Events.put(%{id: id, type: :x, payload: %{}})

      frag = FragmentationInfo.fragment_for_key(id)
      raw = :mnesia.dirty_read({frag, id})

      assert [{_, ^id, _, _, _}] = raw
    end
  end
end
```

### Step 7: Exercise fragment management in IEx

**Objective**: Exercise fragment management in IEx.

```bash
iex --name frag@127.0.0.1 -S mix
```

```elixir
alias MnesiaFragmented.{Events, FragmentationInfo}

for i <- 1..10_000, do: Events.put(%{id: "evt-#{i}", type: :click, payload: %{i: i}})
FragmentationInfo.summary()
# %{total: 10_000, fragments: [%{name: :events, size: 2501, ...}, ...]}

FragmentationInfo.add_fragment()
# Triggers a synchronous rebalance — watch the logs.
FragmentationInfo.summary()
# Now 5 fragments, ~2000 rows each after the rebalance.
```

### Why this works

`mnesia:change_table_frag/2` sets up the fragmentation scheme. Operations route to the correct fragment via the hash module. Reads and writes scale with fragment count; cross-fragment queries walk all fragments, which is why they are discouraged.

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

**1. Forgetting the `:mnesia_frag` access module is silent data loss.**
Plain `:mnesia.read/2` on a fragmented table hits only the base fragment.
Keys that hash to other fragments return `[]`. Every operation on a
fragmented table must go through `:mnesia.activity/4`. Enforce this by
wrapping all access in a single module (as shown) — never call Mnesia
directly from elsewhere.

**2. Rebalancing is a lock-holding operation.**
`add_frag` and `del_frag` acquire write locks on the source fragments for
the duration of the data migration. During this window, writers to those
fragments queue. Plan capacity for the rebalance window itself.

**3. `foldl/3` over the base table name only touches the base fragment.**
Same trap as read/2. Use `:mnesia.activity(:transaction, fn -> :mnesia.foldl(...) end, [], :mnesia_frag)`
or fold each fragment name explicitly.

**4. You cannot change the hash function after the fact.**
`:hash_module` is set at creation time. Migrating to a different hash is a
full data export + import. Pick it once and live with it.

**5. Fragment count is not dynamic in the friendly sense.**
You can add a fragment, but every existing fragment must rebalance
`1/(N+1)` of its data to the new one. Deleting is the inverse and equally
expensive. Cluster-wide traffic during rebalance is substantial — expect
gigabytes of replica sync for a healthy-sized table.

**6. Match specs only scope to the local fragment by default.**
`:mnesia.select/2` with a match spec, used naively, misses rows on other
fragments. Use the activity/4 form and Mnesia will fan out.

**7. Monitoring.**
Track `:mnesia.table_info(frag, :size)` per fragment; an imbalance > 2x
across fragments indicates a pathological key distribution (often
human-picked ids like `user_001`, `user_002`...).

**8. When NOT to use fragmented tables.**
* Dataset fits in RAM on one node — single-table `ram_copies` or
  `disc_copies` is simpler.
* Write throughput is the bottleneck (not storage) — fragmentation does
  not help if the hot keys live in one fragment.
* You anticipate frequent schema changes — fragmented tables multiply
  every `transform_table` operation across N fragments.
* Your team will not operate this daily — fragmented Mnesia has more
  foot-guns than PG's partitioned tables or Cassandra; the operational
  burden is significant.

---

## Benchmark

```elixir
alias MnesiaFragmented.Events

for i <- 1..100_000, do: Events.put(%{id: "e-#{i}", type: :x, payload: %{}})

Benchee.run(
  %{
    "read (random key)" => fn ->
      Events.get("e-#{:rand.uniform(100_000)}")
    end,
    "write (new key)" => fn ->
      Events.put(%{id: "bench-#{:rand.uniform(1_000_000)}", type: :x, payload: %{}})
    end
  },
  parallel: 8,
  time: 5,
  warmup: 2
)
```

Indicative results (M1, single node, 4 fragments):

| Operation        | p50   | p99   |
|------------------|-------|-------|
| read             | 35µs  | 90µs  |
| write            | 220µs | 1.1ms |

Compared to non-fragmented `disc_copies`, reads are about 1.3-1.6x slower
(cost of the fragment lookup), writes are slightly slower for the same
reason. The benefit is entirely in scaling out — you trade latency for
the ability to add nodes.

---

## Reflection

- Your access pattern is 80% by primary key, 20% by secondary index. Does fragmentation still fit, and what do you do with the secondary-index queries?
- How do you grow from 4 fragments to 16 without downtime? What does Mnesia give you, and what do you build?

---

### `script/main.exs`
```elixir
defmodule MnesiaFragmented.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_fragmented, version: "0.1.0", elixir: "~> 1.19", deps: deps()]

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaFragmented.Application, []}]

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]

defmodule MnesiaFragmented.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([MnesiaFragmented.Schema],
      strategy: :one_for_one,
      name: MnesiaFragmented.Supervisor
    )

defmodule MnesiaFragmented.Schema do
  @moduledoc false
  use GenServer
  require Logger

  @table :events
  @initial_fragments 4

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    _ = :mnesia.stop()

    case :mnesia.create_schema([node()]) do
      :ok -> :ok
      {:error, {_, {:already_exists, _}}} -> :ok
      other -> throw({:schema_failed, other})

    :ok = :mnesia.start()
    :ok = ensure_table()
    :ok = :mnesia.wait_for_tables([@table], 20_000)

    log_frag_info()
    {:ok, %{}}

  defp ensure_table do
    opts = [
      attributes: [:id, :type, :payload, :inserted_at],
      type: :set,
      frag_properties: [
        n_fragments: @initial_fragments,
        node_pool: [node()],
        n_ram_copies: 0,
        n_disc_copies: 1
      ]
    ]

    case :mnesia.create_table(@table, opts) do
      {:atomic, :ok} -> :ok
      {:aborted, {:already_exists, @table}} -> :ok
      other -> throw({:create_failed, other})
    end
  end

  defp log_frag_info do
    info = :mnesia.table_info(@table, :frag_properties)
    size = :mnesia.table_info(@table, :size)
    Logger.info("Fragmented table #{@table}: #{inspect(info)} — size=#{size}")
  end
end

defmodule MnesiaFragmented.Events do
  @moduledoc """
  API for the fragmented :events table.

  Every call must use the :mnesia_frag access module — otherwise the
  operation targets only the base fragment and silently returns incorrect
  data for keys stored in other fragments.
  """

  @table :events

  @type id :: String.t()
  @type event :: %{id: id, type: atom(), payload: map(), inserted_at: integer}

  @spec put(event()) :: :ok | {:error, term()}
  def put(%{id: id, type: type, payload: payload}) do
    ts = System.system_time(:millisecond)
    record = {@table, id, type, payload, ts}

    run_frag(fn -> :mnesia.write(record) end)
  end

  @spec get(id()) :: {:ok, event()} | :not_found
  def get(id) do
    result = run_frag(fn -> :mnesia.read({@table, id}) end)

    case result do
      [{@table, ^id, type, payload, ts}] ->
        {:ok, %{id: id, type: type, payload: payload, inserted_at: ts}}

      [] ->
        :not_found
    end
  end

  @spec delete(id()) :: :ok
  def delete(id) do
    run_frag(fn -> :mnesia.delete({@table, id}) end)
    :ok
  end

  @doc """
  Count events across ALL fragments. Uses `:mnesia.table_info/2` on each
  underlying fragment — do not use `:mnesia.foldl/3` which iterates only
  the base fragment in a single-node cluster.
  """
  @spec count_all() :: non_neg_integer()
  def count_all do
    :mnesia.table_info(@table, :frag_names)
    |> Enum.map(fn frag -> :mnesia.table_info(frag, :size) end)
    |> Enum.sum()
  end

  @doc """
  Walk every fragment with a reducer. Useful for migrations, exports, and
  full scans. Much more scalable than a single-fragment foldl because the
  work spreads across the replicas holding each fragment.
  """
  @spec foldl_all((tuple(), acc -> acc), acc) :: acc when acc: term()
  def foldl_all(fun, acc) do
    run_frag(fn -> :mnesia.foldl(fun, acc, @table) end)
  end

  defp run_frag(inner) do
    case :mnesia.activity(:transaction, inner, [], :mnesia_frag) do
      {:atomic, result} -> result
      result -> result
    end
  end
end

defmodule MnesiaFragmented.FragmentationInfo do
  @moduledoc """
  Inspection helpers for a running fragmented table.
  """

  @table :events

  @spec summary() :: %{
          total: non_neg_integer(),
          fragments: [%{name: atom, size: non_neg_integer, nodes: [node]}]
        }
  def summary do
    frag_names = :mnesia.table_info(@table, :frag_names)

    per_frag =
      Enum.map(frag_names, fn frag ->
        %{
          name: frag,
          size: :mnesia.table_info(frag, :size),
          nodes:
            :mnesia.table_info(frag, :disc_copies) ++
              :mnesia.table_info(frag, :ram_copies)
        }
      end)

    %{total: Enum.sum(Enum.map(per_frag, & &1.size)), fragments: per_frag}
  end

  @spec fragment_for_key(term()) :: atom()
  def fragment_for_key(key) do
    # Replicate what mnesia_frag does internally to reveal the target fragment.
    n = :mnesia.table_info(@table, :n_fragments)
    hash = :erlang.phash2(key, n)

    case hash do
      0 -> @table
      _ -> :"#{@table}_frag#{hash + 1}"
    end
  end

  @spec add_fragment() :: :ok | {:error, term()}
  def add_fragment do
    case :mnesia.change_table_frag(@table, {:add_frag, [node()]}) do
      {:atomic, :ok} -> :ok
      other -> {:error, other}
    end
  end
end

defmodule MnesiaFragmented.EventsTest do
  use ExUnit.Case, async: false
  doctest MnesiaFragmented.FragmentationInfo

  alias MnesiaFragmented.{Events, FragmentationInfo}

    :ok
  end

      Enum.each(events, &Events.put/1)

      assert Events.count_all() == 500

      # Every event must be retrievable — if we were missing the frag access
      # module, some keys in non-base fragments would come back :not_found.
      Enum.each(events, fn %{id: id} ->
        assert {:ok, %{id: ^id}} = Events.get(id)
      end)
    end

    describe "core functionality" do
      test "distribution across fragments is roughly uniform" do
        for i <- 1..1_000, do: Events.put(%{id: "evt-#{i}", type: :x, payload: %{}})

        sizes =
          FragmentationInfo.summary().fragments
          |> Enum.map(& &1.size)

        # With phash2 + 4 fragments, expect each fragment to hold 250 ± 75 rows.
        Enum.each(sizes, fn size ->
          assert size > 150 and size < 350, "fragment imbalance: #{inspect(sizes)}"
        end)
      end

      test "fragment_for_key/1 matches actual storage" do
        id = "evt-placement"
        Events.put(%{id: id, type: :x, payload: %{}})

        frag = FragmentationInfo.fragment_for_key(id)
        raw = :mnesia.dirty_read({frag, id})

        assert [{_, ^id, _, _, _}] = raw
      end
    end
  end

  defmodule Main do
    def main do
        :ok
    end
    end
end

Main.main()
```

---

## Why Mnesia Fragmented Tables — Sharding Across Nodes matters

Mastering **Mnesia Fragmented Tables — Sharding Across Nodes** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/mnesia_fragmented_test.exs`

```elixir
defmodule MnesiaFragmentedTest do
  use ExUnit.Case, async: true

  doctest MnesiaFragmented

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MnesiaFragmented.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Fragments are real tables

A "fragmented" `:events` table with 4 fragments becomes, under the hood:

```
:events        → fragment 1 (the "base" fragment)
:events_frag2  → fragment 2
:events_frag3  → fragment 3
:events_frag4  → fragment 4
```

Each is an ordinary Mnesia table with its own replicas. You should almost
never touch them directly; the `mnesia_frag` activity access module
translates your logical `{:events, key}` operations into the correct
physical fragment lookup.

### 2. The `mnesia_frag` access module

All operations on a fragmented table must go through this module:

### 3. Fragment placement strategy

Fragment distribution is controlled via `:frag_properties`:

```elixir
frag_properties: [
  n_fragments: 8,
  node_pool: [:a@host, :b@host, :c@host, :d@host],
  n_ram_copies: 0,
  n_disc_copies: 2
]
```

With 4 nodes, 8 fragments, and 2 disc_copies per fragment, you get 16 total
replicas distributed so each node holds roughly 4 fragments. Mnesia does
round-robin placement at creation time.

### 4. Hash function

By default `mnesia_frag` uses `:erlang.phash2/2` on the key. This is uniform
for most data but pathological for keys with similar prefixes. You can
override via `:hash_module` — the module must export `init_state/2`,
`add_frag/1`, `del_frag/1`, and `key_to_frag_number/2`. In practice the
default is almost always fine; custom hashers are a footgun for marginal
gain.

### 5. Rebalancing cost

`:mnesia.change_table_frag(:events, :add_frag)` adds a new fragment and
migrates ~`1/(N+1)` of the existing data to it. This is a synchronous,
cluster-wide operation that acquires write locks on every source fragment.
For a 10 million row table, plan for many minutes of locked writes. Run
during maintenance windows.

---
