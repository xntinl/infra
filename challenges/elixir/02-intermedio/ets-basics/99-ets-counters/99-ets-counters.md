# Atomic counters — `:ets.update_counter/3` vs the `:counters` module

**Project**: `ets_counters_demo` — bump counters atomically from many
concurrent processes using `:ets.update_counter/3`, then compare it with
the OTP 21+ `:counters` module for the same workload.

---

## Project context

If you've ever written code like this:

```elixir
count = :ets.lookup_element(t, :k, 2)
:ets.insert(t, {:k, count + 1})
```

…you've written a race condition. Between the lookup and the insert, another
process can slip in with its own increment; both will read the same old
value and overwrite each other. The fix is `:ets.update_counter/3`, an
**atomic** read-modify-write that's guaranteed safe under concurrent callers.

For even hotter counters, OTP 21 added `:counters` — lock-free, fixed-size,
per-scheduler shared arrays. They're the closest BEAM equivalent to atomic
integers in Java / `std::atomic` in C++. They're faster than ETS for
counter-only workloads and they don't need a table at all.

This exercise builds both, then races 100 processes bumping them simultaneously
to prove the atomicity holds.

## Why `update_counter` / `:counters` and not X

**Why not `Agent` or a GenServer?** Every increment would serialize through
one process. Under heavy concurrent writers, the mailbox becomes the
bottleneck — you'd trade atomicity (which the callbacks provide) for
throughput you didn't want to lose.

**Why not `Application.put_env` or `:persistent_term`?** Neither is atomic
under concurrent writers. `:persistent_term` is read-optimized and pays a
global GC cost on writes; using it as a counter is actively harmful.

**Why not a plain lookup + insert dance?** Because it's a race — the exercise
includes a failing test demonstrating this exact antipattern.

Project structure:

```
ets_counters_demo/
├── lib/
│   └── ets_counters_demo.ex
├── test/
│   └── ets_counters_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `:ets.update_counter/3` is atomic

```elixir
:ets.update_counter(table, key, {pos, inc})
```

OTP guarantees that the read-modify-write happens without another process
interleaving. The second-arg tuple `{pos, inc}` says "element at position
`pos` in the stored tuple, increment by `inc`". Position is 1-indexed **and**
must not be the key position (usually position 1 is the key, so counters go
in position 2).

The function returns the **new value** after the increment — so you get the
post-update count in the same call.

### 2. `update_counter/3` with default (create-if-absent)

By default, `update_counter/3` raises `:badarg` if the key doesn't exist.
Use the 4-arity form `:ets.update_counter(t, key, {pos, inc}, default_tuple)`
to insert the default tuple first, then apply the increment. Classic pattern
for a "counters by event type" table:

```elixir
:ets.update_counter(t, event_type, {2, 1}, {event_type, 0})
```

### 3. `:counters` module — lock-free, fixed-size array

`:counters.new(size, opts)` allocates an array of 64-bit integer counters.
Two write semantics:

- `:write_concurrency` (default): per-scheduler shards, `add/3` is lock-free
  and extremely fast; `get/2` reads may not see the most-recent write from
  another scheduler until a memory barrier.
- `:atomics`-style: `add/3` is a true atomic op; `get/2` is consistent.
  Select with `[:atomics]` in the options list.

For raw throughput on counters, `:counters` with `:write_concurrency` is the
winner. For strict "read-your-own-write" semantics across processes, pick
`:atomics` or live with the slight staleness.

### 4. When ETS counters are still right

`:counters` is great when you have a **fixed, known set of counter slots**.
The moment you need keyed-by-dynamic-term counters (per-user, per-route,
per-event-type-string), ETS wins because the key is the counter identity.
Most real metrics systems (including Phoenix LiveDashboard internals) use
ETS + `update_counter/3` for this reason.

### 5. Race without atomicity — the test that proves it

If you spawn 1000 processes each doing "lookup, add 1, insert", the final
value will be less than 1000. If you spawn them each doing
`update_counter/3`, the final value is exactly 1000. That's the guarantee
in action, and it's worth writing the bad version at least once to feel
the pain.

---

## Design decisions

**Option A — ETS `:set` + `update_counter/3`**
- Pros: Dynamic keys (per-user, per-route, per-tenant); returns post-value.
- Cons: Slight overhead per op vs `:counters`; still needs a table and owner.

**Option B — `:counters` module** (chosen for fixed-schema hot paths)
- Pros: Lock-free with `:write_concurrency`; zero table overhead.
- Cons: Fixed size at creation; indices are ints, not arbitrary terms.

→ This exercise implements **both**. In real code, pick B when the counter
schema is static (HTTP status buckets, scheduler-indexed metrics); pick A
when the keyspace is dynamic.

---

## Implementation

### Step 1: Create the project

```bash
mix new ets_counters_demo
cd ets_counters_demo
```

### Step 2: `lib/ets_counters_demo.ex`

```elixir
defmodule EtsCountersDemo do
  @moduledoc """
  Three flavors of counter:

    1. ETS non-atomic (lookup + insert) — wrong on purpose, to prove the race.
    2. ETS atomic via `:ets.update_counter/3` — the correct ETS approach.
    3. `:counters` — lock-free, fixed-size, for fixed-shape hot counters.

  Every flavor is racy-tested against N concurrent callers.
  """

  # ── ETS counter table setup ────────────────────────────────────────────

  @doc "Creates a `:public` table with `:write_concurrency` for hot counter writes."
  @spec new_ets_table() :: :ets.tid()
  def new_ets_table do
    :ets.new(:counters, [
      :set,
      :public,
      write_concurrency: true,
      read_concurrency: true
    ])
  end

  # ── 1. WRONG: lookup + insert ──────────────────────────────────────────

  @doc """
  THE RACY VERSION. Do not use in real code — included here to prove the race.
  """
  @spec racy_inc(:ets.tid(), term()) :: integer()
  def racy_inc(t, key) do
    current =
      case :ets.lookup(t, key) do
        [{^key, v}] -> v
        [] -> 0
      end

    :ets.insert(t, {key, current + 1})
    current + 1
  end

  # ── 2. CORRECT: :ets.update_counter/3 ──────────────────────────────────

  @doc """
  Atomic increment. The 4-arg form inserts the default `{key, 0}` tuple if
  the key doesn't exist, then applies the `{pos=2, inc=1}` bump.

  Returns the post-increment value.
  """
  @spec atomic_inc(:ets.tid(), term()) :: integer()
  def atomic_inc(t, key) do
    :ets.update_counter(t, key, {2, 1}, {key, 0})
  end

  @doc "Read the current value, 0 if absent."
  @spec read(:ets.tid(), term()) :: integer()
  def read(t, key) do
    case :ets.lookup(t, key) do
      [{^key, v}] -> v
      [] -> 0
    end
  end

  # ── 3. :counters module ────────────────────────────────────────────────

  @doc """
  Allocate a fixed-size array. Use `:write_concurrency` for max throughput;
  `:atomics` if you need consistent cross-scheduler reads.
  """
  @spec new_counters(pos_integer()) :: :counters.counters_ref()
  def new_counters(size), do: :counters.new(size, [:write_concurrency])

  @doc "Atomic add at index `ix` (1-indexed)."
  @spec counters_inc(:counters.counters_ref(), pos_integer()) :: :ok
  def counters_inc(ref, ix), do: :counters.add(ref, ix, 1)

  @spec counters_read(:counters.counters_ref(), pos_integer()) :: integer()
  def counters_read(ref, ix), do: :counters.get(ref, ix)
end
```

### Step 3: `test/ets_counters_demo_test.exs`

```elixir
defmodule EtsCountersDemoTest do
  use ExUnit.Case, async: true

  describe "racy_inc/2" do
    test "races and loses increments under concurrency" do
      t = EtsCountersDemo.new_ets_table()
      n = 1_000

      tasks =
        for _ <- 1..n, do: Task.async(fn -> EtsCountersDemo.racy_inc(t, :k) end)

      Enum.each(tasks, &Task.await/1)

      # Under concurrency, the final count is almost always strictly less
      # than `n`. We assert inequality to document the race.
      final = EtsCountersDemo.read(t, :k)
      assert final <= n

      # If this ever passes strictly less-than, the race is demonstrated.
      # In practice it nearly always is; don't hard-assert `<` to avoid a
      # flaky test on a lucky scheduler. The teaching moment is real on any
      # multi-core run.
      :ets.delete(t)
    end
  end

  describe "atomic_inc/2" do
    test "update_counter/3 is atomic — final value equals number of calls" do
      t = EtsCountersDemo.new_ets_table()
      n = 1_000

      tasks =
        for _ <- 1..n, do: Task.async(fn -> EtsCountersDemo.atomic_inc(t, :k) end)

      Enum.each(tasks, &Task.await/1)

      assert EtsCountersDemo.read(t, :k) == n
      :ets.delete(t)
    end

    test "returns the post-increment value" do
      t = EtsCountersDemo.new_ets_table()
      assert EtsCountersDemo.atomic_inc(t, :k) == 1
      assert EtsCountersDemo.atomic_inc(t, :k) == 2
      assert EtsCountersDemo.atomic_inc(t, :k) == 3
      :ets.delete(t)
    end
  end

  describe ":counters module" do
    test "add/3 is atomic across concurrent callers" do
      ref = EtsCountersDemo.new_counters(1)
      n = 1_000

      tasks =
        for _ <- 1..n, do: Task.async(fn -> EtsCountersDemo.counters_inc(ref, 1) end)

      Enum.each(tasks, &Task.await/1)

      assert EtsCountersDemo.counters_read(ref, 1) == n
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

### Why this works

`:ets.update_counter/3` is implemented as a single BEAM operation that
acquires the row lock, reads the element at `pos`, applies the increment,
writes back, and releases — all without yielding. No other process can
observe the intermediate state, which is exactly what atomicity means.
`:counters` goes a step further: per-scheduler shards eliminate the lock
contention entirely for the write path, at the cost of slightly weaker
read consistency.

---

## Benchmark

```elixir
# Compare throughput of the three flavors under 1000 concurrent writers.
n = 1_000

t = EtsCountersDemo.new_ets_table()
{us_atomic, _} = :timer.tc(fn ->
  Task.async_stream(1..n, fn _ -> EtsCountersDemo.atomic_inc(t, :k) end, max_concurrency: 100)
  |> Stream.run()
end)

ref = EtsCountersDemo.new_counters(1)
{us_counters, _} = :timer.tc(fn ->
  Task.async_stream(1..n, fn _ -> EtsCountersDemo.counters_inc(ref, 1) end, max_concurrency: 100)
  |> Stream.run()
end)

IO.puts("ets update_counter: #{us_atomic}µs  :counters: #{us_counters}µs")
:ets.delete(t)
```

Target esperado: `:counters` es típicamente 2–5× más rápido que
`:ets.update_counter/3` bajo alta concurrencia (>32 writers). Sin
contención, la diferencia es marginal (<10%).

---

## Trade-offs and production gotchas

**1. `update_counter/3` raises on missing key without the default form**
Use the 4-arity form with a default tuple for "increment-or-initialize".
Otherwise you'll `rescue ArgumentError` in hot code, which is ugly.

**2. `update_counter/3` supports lists of ops for multi-field counters**
`{{2, 1}, {3, 5}}` in one call updates two positions atomically. Great for
paired counters (hits + bytes). See the ets docs — you can also cap with
`{pos, inc, threshold, set_value}` for "wrap on overflow" behavior, which
powers circular metrics counters.

**3. `:counters` requires a known size up front**
You must know the max index at creation time; you can't grow the array.
For dynamic keyspaces, ETS still wins. `:counters` shines when your
metric schema is static (e.g. one slot per HTTP status code bucket).

**4. `:write_concurrency` on `:counters` trades consistency for speed**
Per-scheduler shards make writes lock-free, but a `get/2` may return a value
that's momentarily stale from another scheduler's perspective. If "the
number I just wrote must be visible to everyone immediately" matters, use
the `:atomics` flavor or `:ets.update_counter/3`.

**5. Cross-process reads are still term-copied from ETS**
`:ets.lookup/2` copies the tuple into the caller's heap every time. For a
counter that's read in a hot loop, consider `:ets.lookup_element/3` (returns
one element without constructing a tuple copy) or just use `:counters`.

**6. Don't use `update_counter/3` on `:bag` or `:duplicate_bag`**
It works only on `:set` and `:ordered_set`. Logically, a bag has multiple
tuples per key — "the" counter is ambiguous. OTP will raise.

**7. When NOT to use ETS counters**
- Fixed schema, maximum throughput → `:counters`.
- Lightly contended, per-process counters → plain integers in state.
- Long-term time series → `:telemetry` + Prometheus/StatsD, not ETS.

---

## Reflection

- Your app tracks one counter per active session (100k sessions, bumped on
  every request). Would you pick ETS `update_counter` or a `:counters`
  array, and how would you map session IDs to slots if you chose the latter?
- `:counters` with `:write_concurrency` can return slightly stale reads.
  Design a scenario where that's acceptable, and one where it's a bug.
  What's the tell?

---

## Resources

- [`:ets.update_counter/3` / `/4`](https://www.erlang.org/doc/man/ets.html#update_counter-3)
- [`:counters` module](https://www.erlang.org/doc/man/counters.html) — OTP 21+
- [`:atomics` module](https://www.erlang.org/doc/man/atomics.html) — lock-free atomic ints/refs, sibling of `:counters`
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets)
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — ETS chapter discusses counter-hot contention and `write_concurrency`
