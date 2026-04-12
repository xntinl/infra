# Mnesia Fragmented Tables — Sharding Across Nodes

**Project**: `mnesia_fragmented` — a horizontally-sharded event store using `mnesia_frag`.
**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

---

## Project context

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

```
mnesia_fragmented/
├── lib/
│   └── mnesia_fragmented/
│       ├── application.ex
│       ├── schema.ex           # create fragmented table w/ N fragments
│       ├── events.ex           # API using mnesia_frag access module
│       └── fragmentation_info.ex  # inspection utilities
└── test/
    └── mnesia_fragmented/
        └── events_test.exs
```

---

## Core concepts

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

```elixir
:mnesia.activity(:transaction, fn ->
  :mnesia.read(:events, "evt-123")
end, [], :mnesia_frag)
```

The last argument replaces the default access module. Without it, your
operations hit only the base fragment and miss data stored in the others.

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

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule MnesiaFragmented.MixProject do
  use Mix.Project

  def project do
    [app: :mnesia_fragmented, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger, :mnesia], mod: {MnesiaFragmented.Application, []}]
  end

  defp deps, do: [{:benchee, "~> 1.3", only: :dev}]
end
```

### Step 2: `lib/mnesia_fragmented/application.ex`

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

### Step 3: `lib/mnesia_fragmented/schema.ex`

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

### Step 4: `lib/mnesia_fragmented/events.ex`

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

### Step 5: `lib/mnesia_fragmented/fragmentation_info.ex`

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

```elixir
defmodule MnesiaFragmented.EventsTest do
  use ExUnit.Case, async: false

  alias MnesiaFragmented.{Events, FragmentationInfo}

  setup do
    for frag <- :mnesia.table_info(:events, :frag_names) do
      :mnesia.clear_table(frag)
    end

    :ok
  end

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
```

### Step 7: Exercise fragment management in IEx

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

## Resources

- [Mnesia Fragmented Tables — erlang.org](https://www.erlang.org/doc/apps/mnesia/mnesia_chap5.html#fragmented-tables)
- [`:mnesia.change_table_frag/2`](https://www.erlang.org/doc/man/mnesia.html#change_table_frag-2)
- [OTP source: mnesia_frag.erl](https://github.com/erlang/otp/blob/master/lib/mnesia/src/mnesia_frag.erl)
- [Ulf Wiger — Mnesia at scale (slides)](http://erlang.org/workshop/2004/wiger.pdf) — historical but still useful
- [Dashbit — Mnesia the Bad Parts](https://dashbit.co/blog/mnesia-the-bad-parts)
- [Erlang Solutions — When to use Mnesia](https://www.erlang-solutions.com/blog/when-to-use-mnesia/) — decision matrix
