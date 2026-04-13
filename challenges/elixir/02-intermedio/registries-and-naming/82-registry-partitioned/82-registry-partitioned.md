# Partitioned Registry for high-concurrency workloads

**Project**: `partitioned_reg` — a benchmark harness that measures contention in a default Registry vs a `:partitions`-configured one.

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

## Why `:partitions` and not a single registry or a pool of registries

**Single-partition Registry.** One ETS table, one listener — a write-side serialization point once several schedulers hammer it concurrently. Fine for low-churn workloads.

**Manual pool of N registries, sharded at the call site.** You pay for routing logic in every caller, lose `:via` integration (callers must know which registry owns a key), and re-implement something the stdlib already offers.

**`Registry` with `:partitions` (chosen).** Built-in sharding by `phash2(key)`, same public API as a single registry, parallel `dispatch/3` when you need it, and the default recommendation from the Elixir core team.

---

## Design decisions

**Option A — Keep one registry and scale vertically**
- Pros: Simplest setup; one ETS table; `dispatch/3` aggregates naturally.
- Cons: Write path bottlenecks on a single mutex once concurrency exceeds a couple of schedulers.

**Option B — `:partitions: System.schedulers_online()`** (chosen)
- Pros: Writes scale across cores; `dispatch/3` can fan out in parallel; same API surface as the single-partition version.
- Cons: `dispatch/3` callbacks run per-partition (aggregation requires shared accumulators); partition count is fixed at `start_link/1`; more ETS tables and listeners.

→ Chose **B** because under realistic churn (per-session GenServers, dynamic subscriptions) the write-side contention is the first wall the single-partition version hits, and the partition count heuristic is a one-line change.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {ok},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new partitioned_reg --sup
cd partitioned_reg
```

### Step 2: `lib/partitioned_reg.ex`

**Objective**: Implement `partitioned_reg.ex` — the naming/lookup strategy that decides how processes are addressed under concurrency and failure.


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

**Objective**: Implement `bench.ex` — the naming/lookup strategy that decides how processes are addressed under concurrency and failure.


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

**Objective**: Write `partitioned_reg_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


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

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

On a multi-core machine you'll typically see the partitioned registry
running 1.5x–3x faster under heavy write concurrency. On a single-core
VM the numbers converge.

### Why this works

Partitioning shards registrations across N independent ETS tables keyed by `:erlang.phash2(key, N)`. Each table has its own mutex, so on an N-core box, N concurrent writers progress in parallel instead of serializing on a single write-lock. The public API is unchanged — `register/3`, `lookup/2`, and `dispatch/3` route transparently through the partition — which is why the upgrade is a one-line configuration change, not a rewrite.

---


## Key Concepts: Partitioned Registries and Scalability

By default, `Registry` is a single GenServer, which can become a bottleneck under high registration throughput. Partitioned registries shard the registry across N partitions, reducing contention. Each partition is its own GenServer.

Example: `Registry.start_link(name: MyReg, keys: :unique, partitions: System.schedulers_online())` creates one partition per scheduler, so registrations don't serialize. The trade-off: slightly slower lookups (hash the key to find the partition), but much better throughput.


## Benchmark

The repo already ships a benchmark (`PartitionedReg.Bench.run/3`) wired into the test suite behind `@tag :bench`. Run it directly:

```elixir
workers = System.schedulers_online() * 4
ops = 1_000

single = PartitionedReg.Bench.run(:bench_single, workers, ops)
parted = PartitionedReg.Bench.run(:bench_parted, workers, ops)

IO.puts("single=#{single}µs parted=#{parted}µs ratio=#{Float.round(single / parted, 2)}x")
```

Target esperado: ratio ≥ 1.8x a favor de la versión particionada en un box multi-core con `workers = schedulers * 4`. En una VM single-core ambos convergen (~1.0x) — eso también es información útil.

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

## Reflection

- If your registry is 99% reads and 1% writes, does `:partitions` still buy you anything? What profile signal (e.g., `recon:scheduler_usage/1`, `:erlang.statistics(:scheduler_wall_time)`) would you inspect before turning the knob?
- Your dispatch callback aggregates a count of delivered messages. With partitions, it's invoked once per partition — how do you combine the results safely (shared `:counters` atomic? post-process a list?) without introducing a new bottleneck?

---

## Resources

- [`Registry` — partitions option](https://hexdocs.pm/elixir/Registry.html#start_link/1)
- [Fix Process Bottlenecks with Elixir 1.14's Partition Supervisor — AppSignal](https://blog.appsignal.com/2022/09/20/fix-process-bottlenecks-with-elixir-1-14s-partition-supervisor.html)
- [A journey to Syn v2 — Roberto Ostinelli](https://www.ostinelli.net/a-journey-to-syn-v2/) — deep dive into partitioned registry design
- [`PartitionSupervisor`](https://hexdocs.pm/elixir/PartitionSupervisor.html) — the same idea applied to any GenServer


## Key Concepts

Registry patterns in Elixir provide distributed name resolution through a central registry process. Unlike traditional naming services, Elixir registries are per-node by default but can be partitioned globally. Process name resolution follows a lookup chain: local registry → distributed registry (if configured) → `:global` → fallback mechanisms.

**Critical concepts:**
- **Via tuple pattern** `{:via, module, name}`: Enables pluggable naming backends. The registry module intercepts `:whereis`, `:register`, `:unregister` calls, allowing both local and distributed strategies.
- **Partitioned registries** (`Registry.start_link(partitions: 8)`): Reduce contention by sharding the registry across multiple ETS tables. Each partition handles independent name lookups, improving throughput under high concurrency.
- **Clustering implications**: Global registries across nodes require consensus. Elixir's registry design favors availability (CAP theorem) — a node can register locally and replicate asynchronously. This is why `:global` exists separately from local registries.

**Senior-level gotcha**: Mixing local and global registration without explicit sync logic can cause "phantom" processes — a process registered locally appears available to local callers but fails remote calls. Always make registry scope explicit in your architecture.
