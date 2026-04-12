# Sharded workers with PartitionSupervisor and cross-shard aggregation

**Project**: `sharded_workers` — `PartitionSupervisor` + `:erlang.phash2/2` sharding for per-shard isolation with deliberate cross-shard operations.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

You maintain a real-time leaderboard service for a mobile game. Each match belongs to a `match_id`; players submit score updates every 2 s during the match; at match end, you compute final rankings and emit an event. Single-process approach fell over at 3 000 concurrent matches because the one `LeaderboardServer` GenServer mailbox routinely hit 10 000 messages.

Your first refactor was to use `PartitionSupervisor` (exercise 07) with 16 partitions keyed by `match_id`. Latency dropped 40×. But two new requirements emerged:

1. **Global top-K across all matches** — the home screen shows the top 100 players across every active match. This REQUIRES cross-shard aggregation.
2. **Match migration** — when a match ends, its state must be drained and persisted. A shard shouldn't accumulate dead matches forever.

This exercise goes beyond basic partitioning: it shows the full pattern — consistent sharding with `:erlang.phash2/2`, per-shard state, and a **cross-shard aggregator** that fans out concurrently to all partitions and merges results.

```
sharded_workers/
├── lib/
│   └── sharded_workers/
│       ├── application.ex
│       ├── shard.ex                # the per-partition GenServer
│       ├── leaderboard.ex          # public API: sharded ops + cross-shard aggregate
│       └── config.ex
└── test/
    └── sharded_workers/
        ├── leaderboard_test.exs
        └── sharding_test.exs
```

---

## Core concepts

### 1. Why re-derive the shard index when `PartitionSupervisor` already does it

`PartitionSupervisor` with `{:via, PartitionSupervisor, {name, key}}` internally does `:erlang.phash2(key, n)`. For single-key ops (read/write one match), that's all you need.

But for cross-shard aggregation, you want to iterate **every partition index deterministically**, NOT to hash keys. For that you call `PartitionSupervisor.which_children/1` OR directly address by partition index:

```elixir
# Option A: iterate by index
for idx <- 0..(partitions - 1) do
  GenServer.call({:via, PartitionSupervisor, {MyName, idx}}, :dump)
end

# Option B: iterate via which_children (equivalent, O(n) lookup)
PartitionSupervisor.which_children(MyName)
|> Enum.map(fn {_id, pid, _, _} -> GenServer.call(pid, :dump) end)
```

Both work. Option A is slightly faster (no supervisor mutex) and clearer when you reason about "shard i".

### 2. Shard keys: what to hash

The key is whatever you want to co-locate. Good choices:

- **`match_id`** — all updates for a match hit the same shard. Low contention per match.
- **`player_id`** — if a player's records must be together across matches. But most matches are short-lived, so `match_id` is better.

Bad choices:

- **Random per request** — defeats the purpose; you lose co-location.
- **Timestamp** — hot-shards on current time buckets.
- **User input without salting** — adversarial DoS potential.

### 3. Cross-shard aggregation

```
Request: "top 100 scores globally"
       │
       ▼
Aggregator.top_k(100)
       │
       ├── shard 0 → top 100 local
       ├── shard 1 → top 100 local        (all in parallel)
       ├── ...
       └── shard 15 → top 100 local
              │
              ▼
       merge-sort → top 100 global
```

Why "top 100 local" from each shard (not all scores)? Pruning. If the globally top-100 exists, each shard's top-100 is a superset of the globally top-100's shard-local portion. You merge 16 × 100 = 1 600 records into 100. You do NOT fetch all records from every shard.

### 4. Preventing aggregator hotspots

An aggregator that runs every second and fans out to 16 shards generates 16× GenServer.call load. At 16 shards × 1 rps = 16 ops/s, fine. At 16 × 100 rps (multiple clients) = 1 600 ops/s, you just undid your sharding.

Mitigations:

- **Cache the aggregate** — TTL 1s via `:persistent_term` or a dedicated aggregator GenServer.
- **Event-driven** — shards push aggregates to a central process on changes, NOT on query.
- **Sampled** — aggregate only the hottest N shards per query.

### 5. Draining a shard

When a match ends, you want to remove its state from the shard and persist to DB. Do NOT restart the shard (that would drop other matches on the same shard). Add a `drop/2` API:

```elixir
# In the shard:
def handle_call({:drop, match_id}, _from, state) do
  {match, remaining} = Map.pop(state.matches, match_id)
  # persist asynchronously — don't block shard
  spawn(fn -> MatchArchive.save(match_id, match) end)
  {:reply, :ok, %{state | matches: remaining}}
end
```

---

## Implementation

### Step 1: Application

```elixir
# lib/sharded_workers/application.ex
defmodule ShardedWorkers.Application do
  use Application

  @partitions 16

  @impl true
  def start(_type, _args) do
    children = [
      {PartitionSupervisor,
       child_spec: ShardedWorkers.Shard,
       name: ShardedWorkers.Shards,
       partitions: @partitions}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ShardedWorkers.Supervisor)
  end

  def partitions, do: @partitions
end
```

### Step 2: Per-shard GenServer

```elixir
# lib/sharded_workers/shard.ex
defmodule ShardedWorkers.Shard do
  @moduledoc """
  One shard of the leaderboard. Holds N matches; each match holds a map of
  player_id → score. All ops in one shard serialize against each other.
  """
  use GenServer

  @type match_id :: String.t()
  @type player_id :: String.t()

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @impl true
  def init(_opts), do: {:ok, %{matches: %{}}}

  # ---------------------------------------------------------------------------
  # Single-match ops
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:record, match_id, player_id, score}, _from, state) do
    match = Map.get(state.matches, match_id, %{})
    updated = Map.update(match, player_id, score, &max(&1, score))
    {:reply, :ok, %{state | matches: Map.put(state.matches, match_id, updated)}}
  end

  def handle_call({:top_k_match, match_id, k}, _from, state) do
    result =
      state.matches
      |> Map.get(match_id, %{})
      |> top_k_from_map(k)

    {:reply, result, state}
  end

  def handle_call({:drop, match_id}, _from, state) do
    {:reply, :ok, %{state | matches: Map.delete(state.matches, match_id)}}
  end

  # ---------------------------------------------------------------------------
  # Cross-shard ops — return shard-local aggregates.
  # ---------------------------------------------------------------------------

  def handle_call({:top_k_global_partial, k}, _from, state) do
    partial =
      state.matches
      |> Enum.flat_map(fn {_mid, players} -> Map.to_list(players) end)
      |> top_k_from_list(k)

    {:reply, partial, state}
  end

  def handle_call(:match_count, _from, state) do
    {:reply, map_size(state.matches), state}
  end

  # ---------------------------------------------------------------------------
  # Helpers
  # ---------------------------------------------------------------------------

  defp top_k_from_map(map, k) do
    map |> Map.to_list() |> top_k_from_list(k)
  end

  defp top_k_from_list(list, k) do
    list
    |> Enum.sort_by(fn {_player, score} -> score end, :desc)
    |> Enum.take(k)
  end
end
```

### Step 3: Public API — sharded and cross-shard

```elixir
# lib/sharded_workers/leaderboard.ex
defmodule ShardedWorkers.Leaderboard do
  @moduledoc """
  Public API. Per-match ops route via phash2(match_id).
  Global top-K fans out across all shards in parallel.
  """

  alias ShardedWorkers.Application, as: App
  alias ShardedWorkers.Shards

  @doc "Record a score for a player in a match."
  @spec record(String.t(), String.t(), integer()) :: :ok
  def record(match_id, player_id, score) do
    GenServer.call(shard_for(match_id), {:record, match_id, player_id, score})
  end

  @doc "Top-K players for a single match."
  @spec top_k_match(String.t(), pos_integer()) :: [{String.t(), integer()}]
  def top_k_match(match_id, k) do
    GenServer.call(shard_for(match_id), {:top_k_match, match_id, k})
  end

  @doc "Global top-K across ALL matches across ALL shards. O(n_shards)."
  @spec top_k_global(pos_integer()) :: [{String.t(), integer()}]
  def top_k_global(k) do
    partials =
      0..(App.partitions() - 1)
      |> Task.async_stream(
        fn idx ->
          pid = {:via, PartitionSupervisor, {Shards, idx}}
          GenServer.call(pid, {:top_k_global_partial, k})
        end,
        max_concurrency: App.partitions(),
        timeout: 5_000,
        ordered: false
      )
      |> Enum.flat_map(fn {:ok, partial} -> partial end)

    # Deduplicate (a player might appear in multiple matches, we keep max score).
    partials
    |> Enum.reduce(%{}, fn {player, score}, acc ->
      Map.update(acc, player, score, &max(&1, score))
    end)
    |> Enum.sort_by(fn {_p, s} -> s end, :desc)
    |> Enum.take(k)
  end

  @doc "Drop a finished match from its shard (async archive elsewhere)."
  @spec drop_match(String.t()) :: :ok
  def drop_match(match_id) do
    GenServer.call(shard_for(match_id), {:drop, match_id})
  end

  @doc "Diagnostic: match count per shard. Useful to detect hot shards."
  @spec shard_load() :: [{non_neg_integer(), non_neg_integer()}]
  def shard_load do
    for idx <- 0..(App.partitions() - 1) do
      pid = {:via, PartitionSupervisor, {Shards, idx}}
      {idx, GenServer.call(pid, :match_count)}
    end
  end

  defp shard_for(match_id) do
    {:via, PartitionSupervisor, {Shards, match_id}}
  end
end
```

### Step 4: Tests

```elixir
# test/sharded_workers/leaderboard_test.exs
defmodule ShardedWorkers.LeaderboardTest do
  use ExUnit.Case, async: false

  alias ShardedWorkers.Leaderboard

  setup do
    # Clean state from previous test runs by dropping known match ids.
    for i <- 1..100, do: Leaderboard.drop_match("m-#{i}")
    :ok
  end

  test "records and retrieves per-match top-k" do
    Leaderboard.record("m-1", "alice", 100)
    Leaderboard.record("m-1", "bob", 80)
    Leaderboard.record("m-1", "charlie", 120)

    assert [{"charlie", 120}, {"alice", 100}] = Leaderboard.top_k_match("m-1", 2)
  end

  test "global top-k aggregates across shards" do
    # Spread across 50 matches → likely lands on all 16 shards.
    for i <- 1..50 do
      Leaderboard.record("m-#{i}", "player-#{i}", i * 10)
    end

    top3 = Leaderboard.top_k_global(3)
    assert length(top3) == 3
    assert [{"player-50", 500}, {"player-49", 490}, {"player-48", 480}] = top3
  end

  test "drop removes a match from its shard" do
    Leaderboard.record("m-42", "alice", 999)
    Leaderboard.drop_match("m-42")
    assert [] = Leaderboard.top_k_match("m-42", 10)
  end

  test "shard_load distributes matches across shards" do
    for i <- 1..64 do
      Leaderboard.record("m-#{i}", "p", i)
    end

    load = Leaderboard.shard_load()
    total = Enum.sum(Enum.map(load, fn {_, n} -> n end))
    assert total == 64
    # Not ALL shards will have matches (64 across 16 is ~4/shard, variance is real),
    # but most should.
    populated = Enum.count(load, fn {_, n} -> n > 0 end)
    assert populated >= 10
  end
end
```

```elixir
# test/sharded_workers/sharding_test.exs
defmodule ShardedWorkers.ShardingTest do
  use ExUnit.Case, async: true

  test "same key always lands on the same shard" do
    p1 = GenServer.whereis({:via, PartitionSupervisor, {ShardedWorkers.Shards, "match-x"}})
    p2 = GenServer.whereis({:via, PartitionSupervisor, {ShardedWorkers.Shards, "match-x"}})
    assert p1 == p2
  end

  test "phash2 distribution across 1000 synthetic keys is roughly uniform" do
    counts =
      for i <- 1..1_000 do
        :erlang.phash2("match-#{i}", 16)
      end
      |> Enum.frequencies()

    # No shard should own more than 2× the mean (62.5)
    max_count = counts |> Map.values() |> Enum.max()
    assert max_count < 125
  end
end
```

---

## Trade-offs and production gotchas

**1. Cross-shard ops don't scale the same as per-shard ops.** Per-shard work scales with partitions (N× throughput). Cross-shard work scales INVERSELY (the aggregator is O(N)). Design the API so cross-shard ops are rare — cache them with a short TTL or push them to a dedicated projection.

**2. Rebalancing is not automatic.** If you change `partitions: 16` to `partitions: 32`, every existing key rehashes. Matches currently in shard 7 might now hash to shard 23. State loss is guaranteed on restart. Plan: drain to DB, restart with new N, restore.

**3. `phash2` distribution is not perfectly uniform.** For 1 000 keys over 16 shards you'll see a max shard with ~80 keys and a min with ~45. Budget for 2× the mean in worst-case shard load.

**4. Map mutation on the hot path is O(log n).** `Map.put/3` on a large state map inside the GenServer is sub-microsecond up to thousands of entries but scales logarithmically. For very large per-shard state, move state to ETS owned by the shard.

**5. Cross-shard aggregation holds each shard for the duration of its `call`.** If `top_k_global_partial` takes 20 ms, every match op on that shard waits 20 ms. Either keep partial aggregates pre-computed (event-driven) or use `:ets.tab2list` into ETS-owned state.

**6. `Task.async_stream` with N=partitions starts N tasks.** For 16 partitions that's fine; for 1 024 partitions that's a task explosion per aggregation. Cap `max_concurrency` sanely.

**7. `drop_match` is synchronous — but the archive is a `spawn/1`.** If the archive fails, you've already removed state from the shard. Use `Task.Supervisor.start_child` with `:transient` for retry, and write-ahead-log the archive before removing.

**8. When NOT to use this.** If cross-shard aggregation is the dominant workload (>30% of operations), sharding costs more than it saves. Use a single-writer-multi-reader design with ETS or a dedicated stats process instead.

---

## Performance notes

With 16 partitions on an 8-core machine:

- Per-match ops: ~800 µs p99 (vs 25 ms on single GenServer — 30× improvement).
- Cross-shard top-K for 50 active matches: ~2 ms p99 (fan-out dominated by slowest shard).
- Aggregator cost grows sublinearly with shard count IF shards are idle; linearly if shards are busy (you queue behind match ops).

Measure via `:timer.tc/1`:

```elixir
{t_us, _} = :timer.tc(fn -> ShardedWorkers.Leaderboard.top_k_global(100) end)
IO.puts("top_k_global: #{t_us} µs")
```

---

## Resources

- [`PartitionSupervisor` — hexdocs](https://hexdocs.pm/elixir/PartitionSupervisor.html) — the core primitive.
- [`:erlang.phash2/2` — erlang docs](https://www.erlang.org/doc/man/erlang.html#phash2-2) — distribution characteristics.
- [Designing Elixir Systems with OTP — sharding chapter](https://pragprog.com/titles/jgotp/designing-elixir-systems-with-otp/) — production sharding patterns.
- [Discord — how Discord scaled Elixir to 11M concurrent users](https://discord.com/blog/how-discord-scaled-elixir-to-5-000-000-concurrent-users) — real-world sharded-state architecture.
- [Oban queue sharding](https://github.com/sorentwo/oban/blob/main/lib/oban/peer.ex) — production sharding in a popular library.
- [PartitionSupervisor + Registry patterns — Dashbit](https://dashbit.co/blog/welcome-to-elixir-1-14) — canonical sharding design.
