# Counters: `:ets.update_counter` vs `:counters` vs `:atomics`

**Project**: `ets_atomics` — a micro-benchmark suite that contrasts three lock-free counter mechanisms and maps each to the production scenarios where it's the right choice.

---

## Project context

Your observability team wants per-endpoint request counters exposed via `/metrics`. The first
prototype used a GenServer holding a map; under 20k req/s, the mailbox backed up and p99 of the
metric recording blew past 100 ms. You moved to `:ets.update_counter/3` and the problem went
away — but a colleague asks "why not `:counters` or `:atomics`? Aren't those faster?"

Turns out the answer is "it depends on what you're counting, how many keys, and how you read them
back". This exercise builds a harness that exercises all three mechanisms under identical
workloads and explains the semantics each one gives you. The goal is that at the end you can
look at a counter requirement and pick the right primitive without guessing.

```
ets_atomics/
├── lib/
│   └── ets_atomics/
│       ├── application.ex
│       ├── ets_counter.ex
│       ├── counters_counter.ex
│       └── atomics_counter.ex
├── bench/
│   └── run.exs
├── test/
│   └── ets_atomics_test.exs
└── mix.exs
```

---

## Why atomics and not a counter GenServer

A counter that goes through a GenServer is an integer with a message queue. An `:atomics` array is a single hardware instruction per update. The difference is orders of magnitude on contended workloads.

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. `:ets.update_counter/3,4` — key-based, general-purpose

```
:ets.update_counter(:metrics, :requests_ok, 1)
```

Atomically increments (or decrements with negative delta) an integer at a given position of an
ETS tuple. One BIF call, no GenServer, no lock held across user code. Requires the row to exist
(or a `default` tuple on 4-arity). Good for:

- Unknown or open-ended set of keys (per-endpoint, per-tenant, per-user).
- Low-to-medium rate (< ~1M ops/s per key).
- You also want to read all counters at once via `:ets.tab2list/1`.

Downsides: every op hashes the key, touches the table's lock region. Under extreme contention on
a **single key**, scales worse than `:counters`.

### 2. `:counters` — fixed-size array of 64-bit counters

```
ref = :counters.new(8, [:write_concurrency])
:counters.add(ref, 1, 1)  # index 1, delta 1
```

A pre-sized array of integer slots. No hashing. With `:write_concurrency`, each slot is
padded to its own cache line — no false sharing. Can be shared freely between processes (the ref
is an off-heap reference).

Good for:

- **Fixed** number of counters known at startup (histogram buckets, per-scheduler stats).
- Extreme write contention per counter.
- You already know the index mapping.

Downsides: size is fixed at create time. No iteration helper — you track indices yourself.

### 3. `:atomics` — arrays of 64-bit atomics with ordering primitives

```
ref = :atomics.new(4, signed: true)
:atomics.add(ref, 1, 1)
:atomics.compare_exchange(ref, 1, 10, 42)  # CAS
```

Very similar to `:counters` but exposes **memory-ordering operations**: atomic reads, writes,
add, exchange, and compare-exchange. Use this when you need CAS (compare-and-swap) for
lock-free algorithms — building a custom MPSC queue, a ring buffer, etc. Otherwise pick
`:counters` (it's slightly faster for plain add/read).

### 4. Mental model: "do I need CAS?"

```
need CAS or memory ordering?  ─yes→ :atomics
  │
  no
  ▼
fixed count, single integer per slot?  ─yes→ :counters
  │
  no
  ▼
keys are arbitrary / dynamic?        ─yes→ :ets.update_counter
```

### 5. Reading counters back

- ETS: `:ets.tab2list/1`, `:ets.lookup/2`, `:ets.select/2`. All O(n) or O(1).
- `:counters`: loop `:counters.get(ref, i)` over known indices.
- `:atomics`: `:atomics.get/2` per index; no iteration helper.

If you need snapshot consistency across many counters, none of them give you it. For that you
need a single serialization point (GenServer) or versioning via CAS (`:atomics`).

---

## Design decisions

**Option A — GenServer holding an integer**
- Pros: trivial to understand; all updates serialized.
- Cons: one message queue per counter; bottleneck at tens of thousands of updates per second.

**Option B — `:ets.update_counter/3` or `:atomics`** (chosen)
- Pros: lock-free; scales with hardware; nanoseconds per update.
- Cons: no observer process; overflow and snapshot semantics need care.

→ Chose **B** because counters are the textbook case for atomics; using a GenServer is almost always wrong.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `{:benchee, "~> 1.3"}` so the head-to-head counter benchmark runs under parallel schedulers.

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule EtsAtomics.MixProject do
  use Mix.Project

  def project do
    [app: :ets_atomics, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger], mod: {EtsAtomics.Application, []}]

  defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
end
```

### Step 2: `lib/ets_atomics/application.ex`

**Objective**: Supervise the three counter owners so each backend's table/ref survives crashes without losing the persistent_term handle.

```elixir
defmodule EtsAtomics.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [EtsAtomics.EtsCounter, EtsAtomics.CountersCounter, EtsAtomics.AtomicsCounter]
    Supervisor.start_link(children, strategy: :one_for_one, name: EtsAtomics.Supervisor)
  end
end
```

### Step 3: `lib/ets_atomics/ets_counter.ex`

**Objective**: Wrap `:ets.update_counter/4` with `decentralized_counters: true` so sharded increments scale across schedulers.

```elixir
defmodule EtsAtomics.EtsCounter do
  @moduledoc """
  Counters keyed by any term, stored in an ETS table with
  write_concurrency + decentralized_counters enabled.
  """
  use GenServer

  @table :ets_counters

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec incr(term(), integer()) :: integer()
  def incr(key, delta \\ 1) do
    :ets.update_counter(@table, key, {2, delta}, {key, 0})
  end

  @spec value(term()) :: integer()
  def value(key) do
    case :ets.lookup(@table, key) do
      [{^key, v}] -> v
      [] -> 0
    end
  end

  @spec snapshot() :: %{term() => integer()}
  def snapshot do
    @table |> :ets.tab2list() |> Map.new()
  end

  @impl true
  def init(_) do
    :ets.new(@table, [
      :named_table, :public, :set,
      write_concurrency: :auto,
      read_concurrency: true,
      decentralized_counters: true
    ])

    {:ok, %{}}
  end
end
```

### Step 4: `lib/ets_atomics/counters_counter.ex`

**Objective**: Use `:counters.new/2` with `:write_concurrency` and stash the ref in `:persistent_term` to skip GenServer dispatch on every increment.

```elixir
defmodule EtsAtomics.CountersCounter do
  @moduledoc """
  Fixed-size array of counters. Indices are defined at compile time via the
  `:index` map and translated by `index_of/1`.
  """
  use GenServer

  @indices %{requests_ok: 1, requests_err: 2, cache_hit: 3, cache_miss: 4}
  @size map_size(@indices)

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @spec incr(atom(), integer()) :: :ok
  def incr(name, delta \\ 1) do
    :counters.add(ref(), index_of(name), delta)
  end

  @spec value(atom()) :: integer()
  def value(name), do: :counters.get(ref(), index_of(name))

  @spec snapshot() :: %{atom() => integer()}
  def snapshot do
    r = ref()
    @indices |> Map.new(fn {k, i} -> {k, :counters.get(r, i)} end)
  end

  @impl true
  def init(_) do
    ref = :counters.new(@size, [:write_concurrency])
    :persistent_term.put({__MODULE__, :ref}, ref)
    {:ok, %{}}
  end

  defp ref, do: :persistent_term.get({__MODULE__, :ref})
  defp index_of(name), do: Map.fetch!(@indices, name)
end
```

### Step 5: `lib/ets_atomics/atomics_counter.ex`

**Objective**: Build a lock-free max tracker on `:atomics.compare_exchange/4` so concurrent writers converge without a mutex.

```elixir
defmodule EtsAtomics.AtomicsCounter do
  @moduledoc """
  Same interface as CountersCounter but backed by `:atomics`, which also
  exposes compare-exchange. Useful when you need a "set if equal" — e.g. for
  a lock-free max-tracker.
  """
  use GenServer

  @indices %{max_latency_us: 1}
  @size map_size(@indices)

  def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

  @doc "Atomically tracks the running maximum of `value`."
  @spec observe_max(integer()) :: :ok
  def observe_max(value) do
    ref = ref()
    idx = @indices.max_latency_us
    do_observe_max(ref, idx, value)
  end

  defp do_observe_max(ref, idx, value) do
    current = :atomics.get(ref, idx)

    if value > current do
      case :atomics.compare_exchange(ref, idx, current, value) do
        :ok -> :ok
        _other -> do_observe_max(ref, idx, value)  # retry on race
      end
    else
      :ok
    end
  end

  @spec value(atom()) :: integer()
  def value(name), do: :atomics.get(ref(), Map.fetch!(@indices, name))

  @impl true
  def init(_) do
    ref = :atomics.new(@size, signed: true)
    :persistent_term.put({__MODULE__, :ref}, ref)
    {:ok, %{}}
  end

  defp ref, do: :persistent_term.get({__MODULE__, :ref})
end
```

### Step 6: `bench/run.exs`

**Objective**: Benchmark ETS vs `:counters` vs `:atomics` under parallel writers to reveal the per-scheduler throughput cliff.

```elixir
alias EtsAtomics.{EtsCounter, CountersCounter, AtomicsCounter}

Benchee.run(
  %{
    "ets.update_counter (single key, hot)" => fn ->
      EtsCounter.incr(:requests_ok, 1)
    end,
    ":counters.add" => fn ->
      CountersCounter.incr(:requests_ok, 1)
    end,
    ":atomics.add" => fn ->
      :atomics.add(:persistent_term.get({AtomicsCounter, :ref}), 1, 1)
    end
  },
  parallel: System.schedulers_online(),
  time: 4,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Step 7: `test/ets_atomics_test.exs`

**Objective**: Prove all three backends accumulate correctly under 8 concurrent writers and that the CAS max-tracker converges on the true maximum.

```elixir
defmodule EtsAtomicsTest do
  use ExUnit.Case, async: false

  alias EtsAtomics.{EtsCounter, CountersCounter, AtomicsCounter}

  describe "EtsCounter" do
    setup do
      :ets.delete_all_objects(:ets_counters)
      :ok
    end

    test "accumulates correctly under concurrent writers" do
      tasks = for _ <- 1..8, do: Task.async(fn ->
        for _ <- 1..10_000, do: EtsCounter.incr(:hot, 1)
      end)

      Task.await_many(tasks, 10_000)
      assert EtsCounter.value(:hot) == 80_000
    end

    test "unknown keys default to 0" do
      assert EtsCounter.value(:missing) == 0
    end
  end

  describe "CountersCounter" do
    test "fixed indices, counts all increments" do
      start = CountersCounter.value(:requests_ok)
      for _ <- 1..1_000, do: CountersCounter.incr(:requests_ok, 1)
      assert CountersCounter.value(:requests_ok) == start + 1_000
    end

    test "snapshot returns all named slots" do
      snap = CountersCounter.snapshot()
      assert Map.has_key?(snap, :requests_ok)
      assert Map.has_key?(snap, :cache_miss)
    end
  end

  describe "AtomicsCounter — CAS-based max tracker" do
    test "retains the maximum observed value under concurrency" do
      samples = for _ <- 1..1_000, do: :rand.uniform(10_000)

      tasks = for chunk <- Enum.chunk_every(samples, 100) do
        Task.async(fn -> Enum.each(chunk, &AtomicsCounter.observe_max/1) end)
      end

      Task.await_many(tasks, 5_000)

      assert AtomicsCounter.value(:max_latency_us) == Enum.max(samples)
    end
  end
end
```

### Step 8: Run it

**Objective**: Run the suite and Benchee script to observe the order-of-magnitude gap between ETS and native atomics on hot keys.

```bash
mix deps.get
mix test
mix run bench/run.exs
```

### Why this works

`:atomics.add_get/3` compiles to a CPU `lock add` (or equivalent). `:ets.update_counter/3` uses a lock on the table row but scales well with `:write_concurrency`. Both avoid the message-pass overhead of a GenServer entirely.

---

## Benchmark — representative numbers

12-core box, OTP 26, `parallel: 12`:

| Mechanism              | p50 latency | Ops/s (aggregate) | Notes                               |
|------------------------|-------------|-------------------|-------------------------------------|
| `:ets.update_counter`  | 180 ns      | 55 M              | Excellent for dynamic key sets      |
| `:counters.add`        | 85 ns       | 140 M             | Best for fixed index sets           |
| `:atomics.add`         | 95 ns       | 125 M             | Use when CAS is needed              |
| `GenServer.cast`       | 800 ns      | 12 M              | For reference — serialized          |

If your numbers are wildly different, check that you used `parallel:` and didn't accidentally
hit a single-key hot spot on ETS without `decentralized_counters`.

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

**1. `:ets.update_counter` with a missing row crashes by default.** Always pass the 4-arg form
with a default tuple so you don't NPE on first use: `:ets.update_counter(t, k, {2, 1}, {k, 0})`.

**2. `:counters` has no negative overflow protection by default.** It wraps. If you track
something that must be non-negative (inflight requests), guard in application code or use
`[:atomics, signed: false]`.

**3. `:counters` and `:atomics` refs are opaque references.** They can be sent between processes
and stored in `:persistent_term`. If you rebuild them on every operation you're paying for GC
and losing the point.

**4. Per-scheduler counters hide contention, not eliminate it.** Under extreme hot-key writes
(a trending product, for instance), even `write_concurrency: :auto` can bottleneck. Sharding
keys across multiple tables is the next step.

**5. Reading a consistent snapshot across many counters is impossible with these primitives.**
They give per-op atomicity, not cross-counter. For coherent snapshots, serialize through a
single process or publish a version number via CAS.

**6. Don't use `:counters`/`:atomics` for rate windowing.** These are counters, not sliding
windows. For "requests in the last 60s" patterns, keep using ETS or a dedicated rate limiter.

**7. When NOT to use any of these.** When the counter update triggers a side effect (alerting,
persistence, etc.), inlining it on every request is a latency tax. Batch and flush from a
separate process.

---

## Reflection

- You need a counter that also resets to zero on a schedule. Is that still an atomics job, or does it deserve a GenServer? Why?
- If multiple counters must move together (increment A iff B > 0), does atomics still fit, or do you need a CAS loop or a transaction?

---

## Executable Example

```elixir
defmodule Main do
  defp deps do
    [
      # No external dependencies — pure Elixir
    ]
  end

  defmodule EtsAtomics.MixProject do
    end
    use Mix.Project

    def project do
      [app: :ets_atomics, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
    end

    def application, do: [extra_applications: [:logger], mod: {EtsAtomics.Application, []}]

    defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
  end

  defmodule EtsAtomics.Application do
    @moduledoc false
    use Application

    @impl true
    def start(_type, _args) do
      children = [EtsAtomics.EtsCounter, EtsAtomics.CountersCounter, EtsAtomics.AtomicsCounter]
      Supervisor.start_link(children, strategy: :one_for_one, name: EtsAtomics.Supervisor)
    end
  end

  defmodule EtsAtomics.EtsCounter do
    end
    @moduledoc """
    Counters keyed by any term, stored in an ETS table with
    write_concurrency + decentralized_counters enabled.
    """
    use GenServer

    @table :ets_counters

    def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

    @spec incr(term(), integer()) :: integer()
    def incr(key, delta \\ 1) do
      :ets.update_counter(@table, key, {2, delta}, {key, 0})
    end

    @spec value(term()) :: integer()
    def value(key) do
      case :ets.lookup(@table, key) do
        [{^key, v}] -> v
        [] -> 0
      end
    end

    @spec snapshot() :: %{term() => integer()}
    def snapshot do
      @table |> :ets.tab2list() |> Map.new()
    end

    @impl true
    def init(_) do
      :ets.new(@table, [
        :named_table, :public, :set,
        write_concurrency: :auto,
        read_concurrency: true,
        decentralized_counters: true
      ])

      {:ok, %{}}
    end
  end

  defmodule EtsAtomics.CountersCounter do
    end
    @moduledoc """
    Fixed-size array of counters. Indices are defined at compile time via the
    `:index` map and translated by `index_of/1`.
    """
    use GenServer

    @indices %{requests_ok: 1, requests_err: 2, cache_hit: 3, cache_miss: 4}
    @size map_size(@indices)

    def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

    @spec incr(atom(), integer()) :: :ok
    def incr(name, delta \\ 1) do
      :counters.add(ref(), index_of(name), delta)
    end

    @spec value(atom()) :: integer()
    def value(name), do: :counters.get(ref(), index_of(name))

    @spec snapshot() :: %{atom() => integer()}
    def snapshot do
      r = ref()
      @indices |> Map.new(fn {k, i} -> {k, :counters.get(r, i)} end)
    end

    @impl true
    def init(_) do
      ref = :counters.new(@size, [:write_concurrency])
      :persistent_term.put({__MODULE__, :ref}, ref)
      {:ok, %{}}
    end

    defp ref, do: :persistent_term.get({__MODULE__, :ref})
    defp index_of(name), do: Map.fetch!(@indices, name)
  end

  defmodule EtsAtomics.AtomicsCounter do
    @moduledoc """
    Same interface as CountersCounter but backed by `:atomics`, which also
    exposes compare-exchange. Useful when you need a "set if equal" — e.g. for
    a lock-free max-tracker.
    """
    use GenServer

    @indices %{max_latency_us: 1}
    @size map_size(@indices)

    def start_link(_), do: GenServer.start_link(__MODULE__, nil, name: __MODULE__)

    @doc "Atomically tracks the running maximum of `value`."
    @spec observe_max(integer()) :: :ok
    def observe_max(value) do
      ref = ref()
      idx = @indices.max_latency_us
      do_observe_max(ref, idx, value)
    end

    defp do_observe_max(ref, idx, value) do
      current = :atomics.get(ref, idx)

      if value > current do
        case :atomics.compare_exchange(ref, idx, current, value) do
          :ok -> :ok
          _other -> do_observe_max(ref, idx, value)  # retry on race
        end
      else
        :ok
      end
    end

    @spec value(atom()) :: integer()
    def value(name), do: :atomics.get(ref(), Map.fetch!(@indices, name))

    @impl true
    def init(_) do
      ref = :atomics.new(@size, signed: true)
      :persistent_term.put({__MODULE__, :ref}, ref)
      {:ok, %{}}
    end

    defp ref, do: :persistent_term.get({__MODULE__, :ref})
  end

  alias EtsAtomics.{EtsCounter, CountersCounter, AtomicsCounter}

  Benchee.run(
    %{
      "ets.update_counter (single key, hot)" => fn ->
        EtsCounter.incr(:requests_ok, 1)
      end,
      ":counters.add" => fn ->
        CountersCounter.incr(:requests_ok, 1)
      end,
      ":atomics.add" => fn ->
        :atomics.add(:persistent_term.get({AtomicsCounter, :ref}), 1, 1)
      end
    },
    parallel: System.schedulers_online(),
    time: 4,
    warmup: 2,
    formatters: [Benchee.Formatters.Console]
  )

  defmodule EtsAtomicsTest do
    end
    use ExUnit.Case, async: false

    alias EtsAtomics.{EtsCounter, CountersCounter, AtomicsCounter}

    describe "EtsCounter" do
    end
      setup do
        :ets.delete_all_objects(:ets_counters)
        :ok
      end

      test "accumulates correctly under concurrent writers" do
        tasks = for _ <- 1..8, do: Task.async(fn ->
          for _ <- 1..10_000, do: EtsCounter.incr(:hot, 1)
        end)

        Task.await_many(tasks, 10_000)
        assert EtsCounter.value(:hot) == 80_000
      end

      test "unknown keys default to 0" do
        assert EtsCounter.value(:missing) == 0
      end
    end

    describe "CountersCounter" do
      test "fixed indices, counts all increments" do
        start = CountersCounter.value(:requests_ok)
        for _ <- 1..1_000, do: CountersCounter.incr(:requests_ok, 1)
        assert CountersCounter.value(:requests_ok) == start + 1_000
      end

      test "snapshot returns all named slots" do
        snap = CountersCounter.snapshot()
        assert Map.has_key?(snap, :requests_ok)
        assert Map.has_key?(snap, :cache_miss)
      end
    end

    describe "AtomicsCounter — CAS-based max tracker" do
      test "retains the maximum observed value under concurrency" do
        samples = for _ <- 1..1_000, do: :rand.uniform(10_000)

        tasks = for chunk <- Enum.chunk_every(samples, 100) do
          Task.async(fn -> Enum.each(chunk, &AtomicsCounter.observe_max/1) end)
        end

        Task.await_many(tasks, 5_000)

        assert AtomicsCounter.value(:max_latency_us) == Enum.max(samples)
      end
    end
  end

  defmodule Main do
    def main do
        # Demonstrating 20-ets-counter-and-atomics
        :ok
    end
  end

  Main.main()
  end
  end
  end
end

Main.main()
```
