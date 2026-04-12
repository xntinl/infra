# Counters: `:ets.update_counter` vs `:counters` vs `:atomics`

**Project**: `ets_atomics` — a micro-benchmark suite that contrasts three lock-free counter mechanisms and maps each to the production scenarios where it's the right choice.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

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

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

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

```bash
mix deps.get
mix test
mix run bench/run.exs
```

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
keys across multiple tables (exercise 116) is the next step.

**5. Reading a consistent snapshot across many counters is impossible with these primitives.**
They give per-op atomicity, not cross-counter. For coherent snapshots, serialize through a
single process or publish a version number via CAS.

**6. Don't use `:counters`/`:atomics` for rate windowing.** These are counters, not sliding
windows. For "requests in the last 60s" patterns, keep using ETS or a dedicated rate limiter
(exercise 71).

**7. When NOT to use any of these.** When the counter update triggers a side effect (alerting,
persistence, etc.), inlining it on every request is a latency tax. Batch and flush from a
separate process.

---

## Resources

- [`:ets.update_counter/3,4` — erlang.org](https://www.erlang.org/doc/man/ets.html#update_counter-3)
- [`:counters` — erlang.org](https://www.erlang.org/doc/man/counters.html)
- [`:atomics` — erlang.org](https://www.erlang.org/doc/man/atomics.html)
- [Telemetry.Metrics — how LastValue and Counter work under the hood](https://github.com/beam-telemetry/telemetry_metrics)
- [`:prometheus_ex` source — counter implementation](https://github.com/deadtrickster/prometheus.ex) — uses `:counters` internally
- [OTP 22 release notes — introduction of `:counters` and `:atomics`](https://www.erlang.org/blog/my-otp-22-highlights/)
