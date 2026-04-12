# Partitioned Registry for high-concurrency workloads

**Project**: `partitioned_reg` — a benchmark harness that measures contention in a default Registry vs a `:partitions`-configured one.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

You deployed a service that uses `Registry` to name millions of short-lived
GenServers (one per user session, say). Under load you see a flat
throughput ceiling. Profiling shows the bottleneck isn't your code — it's
the single ETS table behind the registry taking the hit from all scheduler
cores at once.

`Registry` has a `:partitions` option precisely for this. It shards entries
across N internal ETS tables, one per partition, chosen by `phash2(key)`.
With `N == System.schedulers_online()` the hot path spreads across cores
and the contention disappears.

This exercise builds a small benchmark that exhibits the difference: many
concurrent processes registering and looking up keys, measured on a
1-partition vs an N-partition registry.

Project structure:

```
partitioned_reg/
├── lib/
│   ├── partitioned_reg.ex
│   └── partitioned_reg/bench.ex
├── test/
│   └── partitioned_reg_test.exs
└── mix.exs
```

---

## Core concepts

### 1. How partitioning actually works

A non-partitioned registry = 1 ETS table + 1 listener process. Every
registration goes through that listener. A partitioned registry = N ETS
tables + N listeners, with each key routed to a partition by
`:erlang.phash2(key, partitions)`. Within a partition, ETS still
serializes writes; across partitions, writes are genuinely parallel.

### 2. Read vs write paths

Reads on modern ETS are almost entirely lock-free and don't benefit much
from partitioning. Writes (register/unregister/monitor-triggered cleanup)
contend on a per-table mutex. Partitioning therefore mainly helps
churn-heavy workloads (lots of register/unregister), not steady-state
lookups.

### 3. Partition count heuristic

```elixir
partitions: System.schedulers_online()
```

That's the default recommendation in the docs. More partitions than
schedulers is wasteful (more ETS tables, no more parallelism); fewer
misses the benefit. If your app is always pinned to fewer schedulers (a
dedicated service on a big box), match that number instead.

### 4. `dispatch/3` and `parallel: true`

With N partitions, `Registry.dispatch/3` can spawn one task per partition
to run the callback concurrently — useful for fanning out a pubsub
broadcast to millions of subscribers. Pass `parallel: true`. Without it,
dispatch walks partitions serially in the caller.

---

## Implementation

### Step 1: Create the project

```bash
mix new partitioned_reg --sup
cd partitioned_reg
```

### Step 2: `lib/partitioned_reg.ex`

```elixir
defmodule PartitionedReg do
  @moduledoc """
  Helpers to start two registries side-by-side — one single-partition and
  one with `:partitions` set to `System.schedulers_online()` — so the
  benchmark can compare them under identical workloads.
  """

  @doc "Starts a single-partition registry."
  @spec start_single(atom()) :: Supervisor.on_start_child()
  def start_single(name) do
    Registry.start_link(keys: :unique, name: name)
  end

  @doc """
  Starts a partitioned registry. Defaults to `System.schedulers_online()`
  partitions, which is the documented sweet spot for concurrent workloads.
  """
  @spec start_partitioned(atom(), pos_integer()) :: Supervisor.on_start_child()
  def start_partitioned(name, partitions \\ System.schedulers_online()) do
    Registry.start_link(keys: :unique, name: name, partitions: partitions)
  end
end
```

### Step 3: `lib/partitioned_reg/bench.ex`

```elixir
defmodule PartitionedReg.Bench do
  @moduledoc """
  A simple write-heavy benchmark: N workers, each registering `ops` keys
  concurrently under the given registry name. Returns the wall-clock
  microseconds taken.

  Not a scientific benchmark — the goal is educational: run it on both
  registries and watch the partitioned one pull ahead under high
  concurrency.
  """

  @doc """
  Runs `workers` concurrent processes, each performing `ops` registrations
  plus lookups against `registry`. Returns the duration in microseconds.
  """
  @spec run(atom(), pos_integer(), pos_integer()) :: non_neg_integer()
  def run(registry, workers, ops) do
    parent = self()

    {elapsed, _} =
      :timer.tc(fn ->
        tasks =
          for w <- 1..workers do
            Task.async(fn ->
              for o <- 1..ops do
                key = {w, o}
                # Register self under a unique key, then look it up — the
                # common "addressable per-entity server" pattern.
                {:ok, _} = Registry.register(registry, key, nil)
                [{pid, _}] = Registry.lookup(registry, key)
                ^pid = self()
                :ok = Registry.unregister(registry, key)
              end

              send(parent, :done)
            end)
          end

        Enum.each(tasks, &Task.await(&1, :infinity))
      end)

    elapsed
  end
end
```

### Step 4: `test/partitioned_reg_test.exs`

```elixir
defmodule PartitionedRegTest do
  use ExUnit.Case, async: false

  alias PartitionedReg.Bench

  setup do
    {:ok, _} = PartitionedReg.start_single(:bench_single)
    {:ok, _} = PartitionedReg.start_partitioned(:bench_parted)

    on_exit(fn ->
      for reg <- [:bench_single, :bench_parted] do
        case Process.whereis(reg) do
          nil -> :ok
          pid -> Process.exit(pid, :normal)
        end
      end
    end)

    :ok
  end

  describe "functional equivalence" do
    test "both registries expose the same API and cleanup semantics" do
      {:ok, _} = Registry.register(:bench_single, "a", 1)
      {:ok, _} = Registry.register(:bench_parted, "a", 1)

      assert [{_, 1}] = Registry.lookup(:bench_single, "a")
      assert [{_, 1}] = Registry.lookup(:bench_parted, "a")

      :ok = Registry.unregister(:bench_single, "a")
      :ok = Registry.unregister(:bench_parted, "a")

      assert [] = Registry.lookup(:bench_single, "a")
      assert [] = Registry.lookup(:bench_parted, "a")
    end
  end

  describe "concurrent workload" do
    @tag :bench
    test "partitioned registry is not slower than single-partition (informational)" do
      # Small workload so the test is fast; the absolute numbers are
      # uninteresting — the interesting data is the ratio when you crank
      # `workers` and `ops` on a multi-core box.
      workers = max(2, System.schedulers_online())
      ops = 200

      single = Bench.run(:bench_single, workers, ops)
      parted = Bench.run(:bench_parted, workers, ops)

      IO.puts(
        "\n[bench] single=#{single}us parted=#{parted}us " <>
          "ratio=#{Float.round(single / max(parted, 1), 2)}x"
      )

      # We assert only that both completed successfully — don't flake on
      # CI timing noise by asserting a speedup.
      assert single > 0
      assert parted > 0
    end
  end
end
```

### Step 5: Run

```bash
mix test
```

On a multi-core machine you'll typically see the partitioned registry
running 1.5x–3x faster under heavy write concurrency. On a single-core
VM the numbers converge.

---

## Trade-offs and production gotchas

**1. Partitions help writes, not reads**
If your app is dominated by `Registry.lookup/2` with low registration
churn, partitions won't move the needle much. Measure before tuning.

**2. `dispatch/3` semantics change with partitions**
Your dispatch callback is now invoked **once per partition**, not once
with all entries. Code that aggregates across the whole key must cope —
either pass a shared accumulator (e.g., `:counters`) or post-process the
list of partition results.

**3. Per-partition ordering only**
If you rely on seeing `register` events in a consistent order across
subscribers, partitioning breaks that — partitions race each other. In
practice, you almost never relied on this and shouldn't.

**4. Memory overhead**
Each partition is its own ETS table (plus listener/process). With N
partitions you pay O(N) memory for the table headers. Usually negligible;
worth knowing if you're running thousands of registries.

**5. `:partitions` is set once, at start_link**
You can't reshuffle live; you'd have to start a new registry and migrate.
Pick a number up front and leave it — `System.schedulers_online()` is
a safe default for virtually all deployments.

**6. When NOT to partition**
Low-churn registries, single-topic pubsub, or small registries with
hundreds of keys — the default 1 partition is fine. Partitioning adds
complexity and you won't notice the difference below concurrency where
ETS contention actually shows up (~thousands of ops/sec).

---

## Resources

- [`Registry` — partitions option](https://hexdocs.pm/elixir/Registry.html#start_link/1)
- [Fix Process Bottlenecks with Elixir 1.14's Partition Supervisor — AppSignal](https://blog.appsignal.com/2022/09/20/fix-process-bottlenecks-with-elixir-1-14s-partition-supervisor.html)
- [A journey to Syn v2 — Roberto Ostinelli](https://www.ostinelli.net/a-journey-to-syn-v2/) — deep dive into partitioned registry design
- [`PartitionSupervisor`](https://hexdocs.pm/elixir/PartitionSupervisor.html) — the same idea applied to any GenServer
