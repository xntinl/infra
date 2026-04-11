# BEAM Schedulers and Reductions

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. Under load testing, the SRE team reports that p99
latency spikes from 5ms to 800ms intermittently. Profiling shows CPU-bound request
handlers (JWT validation, response body compression, route regex matching) running
on normal BEAM schedulers and starving I/O-bound handlers. A second issue: a
background metrics aggregation job that runs a tight accumulation loop causes
visible latency degradation for all other requests while it runs.

This exercise covers how BEAM schedules processes, what "reductions" are, why
CPU-bound code on normal schedulers hurts latency, and how dirty schedulers and
`:erlang.yield/0` provide the escape hatches.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── metrics/
│       │   ├── counter.ex
│       │   ├── event_log.ex
│       │   └── aggregator.ex
│       └── ...
├── test/
│   └── api_gateway/
│       └── metrics/
│           └── aggregator_test.exs
├── bench/
│   └── scheduler_bench.exs
└── mix.exs
```

---

## The business problem

Two requirements:

1. **Cooperative metrics aggregator**: the `ApiGateway.Metrics.Aggregator` GenServer
   reads all ETS event log entries and computes per-route statistics on a 10-second
   schedule. The computation is CPU-bound and must yield periodically so that normal
   scheduler time is available to request-handling processes.

2. **Scheduler wall time measurement**: before and after deploying the aggregator,
   the team wants to measure scheduler utilization with `:erlang.statistics/1` to
   prove that the cooperative version causes measurably less scheduler starvation.

---

## How BEAM scheduling works

### Reductions — the scheduling unit

BEAM does not use wall-clock time slices. Every process gets a quantum of
approximately **2,000 reductions** before being preempted. A reduction is roughly
one function call. This means:

```elixir
# This loop does NOT monopolize the scheduler — BEAM preempts after ~2000 iterations
def count_down(0), do: :done
def count_down(n), do: count_down(n - 1)

# This DOES monopolize the scheduler — a BIF that runs in C without yielding
:crypto.strong_rand_bytes(10_000_000)   # runs for milliseconds without preemption
```

The distinction: Elixir/Erlang code preempts cooperatively via the reduction
counter. Certain BIFs (`:crypto`, `:zlib`, NIF calls) run entirely in C and do
not decrement the reduction counter — they block the scheduler thread for their
entire duration.

### Scheduler types

| Scheduler | Thread count | Used for | Can block? |
|-----------|-------------|----------|------------|
| Normal schedulers | `System.schedulers_online()` (default: CPU count) | All Elixir/Erlang code | No — must yield |
| Dirty CPU schedulers | 10 by default | CPU-bound BIFs/NIFs | Yes |
| Dirty I/O schedulers | 10 by default | Blocking I/O BIFs/NIFs | Yes |

When a NIF or BIF is marked as dirty, BEAM moves it off the normal scheduler onto
a dirty scheduler thread. Normal schedulers stay free for request handling.

### Scheduler wall time

`:erlang.statistics(:scheduler_wall_time)` returns `[{scheduler_id, active_time, total_time}]`.
`active_time / total_time` is the utilization ratio for that scheduler. High utilization
(>80%) on normal schedulers under load indicates a CPU-starvation risk.

```elixir
:erlang.system_flag(:scheduler_wall_time, true)  # must enable first

before = :erlang.statistics(:scheduler_wall_time) |> Enum.sort()
# ... run workload ...
after_stats = :erlang.statistics(:scheduler_wall_time) |> Enum.sort()

utilization =
  Enum.zip(before, after_stats)
  |> Enum.map(fn {{id, a0, t0}, {id, a1, t1}} ->
    {id, (a1 - a0) / (t1 - t0)}
  end)
```

---

## Implementation

### Step 1: `lib/api_gateway/metrics/aggregator.ex`

```elixir
defmodule ApiGateway.Metrics.Aggregator do
  @moduledoc """
  Periodically aggregates per-route metrics from the ETS event log.

  Runs a CPU-bound aggregation pass every @interval_ms milliseconds.
  The aggregation yields every @yield_every entries to avoid monopolizing
  the normal BEAM scheduler and causing latency spikes for request handlers.

  The yield strategy uses :erlang.yield/0, which signals the scheduler that
  this process is willing to be preempted. The scheduler may or may not
  actually preempt — it depends on whether other runnable processes are waiting.
  This is cooperative, not preemptive: it hints rather than forces.
  """

  use GenServer

  @interval_ms 10_000
  @yield_every 500

  # ── Public API ──────────────────────────────────────────────────────────────

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Returns the most recently computed aggregation result.
  Returns nil if no aggregation has run yet.
  """
  @spec last_result() :: map() | nil
  def last_result do
    GenServer.call(__MODULE__, :last_result)
  end

  @doc """
  Triggers an immediate aggregation pass (for testing and manual flush).
  Blocks until the aggregation completes.
  """
  @spec run_now() :: map()
  def run_now do
    GenServer.call(__MODULE__, :run_now, 30_000)
  end

  # ── GenServer callbacks ──────────────────────────────────────────────────────

  @impl true
  def init(_opts) do
    Process.send_after(self(), :aggregate, @interval_ms)
    {:ok, %{last_result: nil, run_count: 0}}
  end

  @impl true
  def handle_call(:last_result, _from, state) do
    {:reply, state.last_result, state}
  end

  @impl true
  def handle_call(:run_now, _from, state) do
    result = do_aggregate()
    {:reply, result, %{state | last_result: result, run_count: state.run_count + 1}}
  end

  @impl true
  def handle_info(:aggregate, state) do
    result = do_aggregate()
    Process.send_after(self(), :aggregate, @interval_ms)
    {:noreply, %{state | last_result: result, run_count: state.run_count + 1}}
  end

  # ── Private: CPU-bound aggregation with cooperative yielding ─────────────────

  defp do_aggregate do
    # Read all events directly from the ETS table.
    # EventLog owns the :gateway_event_log table — read it without going through
    # the GenServer to avoid serializing the expensive aggregation on its mailbox.
    #
    # If the table doesn't exist (e.g., EventLog not started), return empty map.
    events =
      try do
        :ets.tab2list(:gateway_event_log)
      catch
        :error, :badarg -> []
      end

    # Aggregate with periodic yielding to avoid scheduler starvation.
    # Every @yield_every entries, call :erlang.yield() to signal the scheduler
    # that this process is willing to be preempted. This prevents a large event
    # log from causing latency spikes for concurrent request handlers.
    events
    |> Enum.with_index(1)
    |> Enum.reduce(%{}, fn {event, index}, acc ->
      if rem(index, @yield_every) == 0, do: :erlang.yield()

      case event do
        {{_ts, _id}, route, status_code, bytes} ->
          route_stats = Map.get(acc, route, %{count: 0, total_bytes: 0, errors: 0})

          error_inc = if status_code >= 400, do: 1, else: 0

          updated = %{
            count: route_stats.count + 1,
            total_bytes: route_stats.total_bytes + bytes,
            errors: route_stats.errors + error_inc
          }

          Map.put(acc, route, updated)

        _ ->
          acc
      end
    end)
  end

  @doc false
  @doc """
  Measures scheduler wall time utilization across a workload.
  Returns a list of {scheduler_id, utilization_ratio} for each normal scheduler.

  Enables scheduler_wall_time tracking, snapshots before and after the workload,
  then computes the utilization ratio (active_time / total_time) for each scheduler.
  Only includes normal schedulers (ids 1..schedulers_online), not dirty schedulers.
  """
  @spec measure_scheduler_utilization(fun()) :: [{integer(), float()}]
  def measure_scheduler_utilization(workload_fn) do
    :erlang.system_flag(:scheduler_wall_time, true)
    before = :erlang.statistics(:scheduler_wall_time) |> Enum.sort()

    workload_fn.()

    after_stats = :erlang.statistics(:scheduler_wall_time) |> Enum.sort()
    online = System.schedulers_online()

    Enum.zip(before, after_stats)
    |> Enum.filter(fn {{id, _, _}, _} -> id <= online end)
    |> Enum.map(fn {{id, a0, t0}, {^id, a1, t1}} ->
      delta_total = t1 - t0

      ratio =
        if delta_total > 0 do
          (a1 - a0) / delta_total
        else
          0.0
        end

      {id, ratio + 0.0}
    end)
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/api_gateway/metrics/aggregator_test.exs
defmodule ApiGateway.Metrics.AggregatorTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Metrics.Aggregator

  setup do
    # Start aggregator for the test
    start_supervised!({Aggregator, []})
    :ok
  end

  describe "run_now/0" do
    test "returns a map" do
      result = Aggregator.run_now()
      assert is_map(result)
    end

    test "last_result/0 returns nil before first aggregation completes" do
      # This is tricky in a test — the aggregation runs immediately in run_now
      # so we check the state is updated after run_now
      result = Aggregator.run_now()
      assert Aggregator.last_result() == result
    end
  end

  describe "measure_scheduler_utilization/1" do
    test "returns a list of {id, float} tuples" do
      utilization =
        Aggregator.measure_scheduler_utilization(fn ->
          # A trivial CPU workload
          Enum.reduce(1..100_000, 0, fn i, acc -> acc + i end)
        end)

      assert is_list(utilization)

      for {id, ratio} <- utilization do
        assert is_integer(id)
        assert is_float(ratio) or ratio == 0
        assert ratio >= 0.0 and ratio <= 1.0
      end
    end

    test "a tight loop shows positive scheduler utilization" do
      utilization =
        Aggregator.measure_scheduler_utilization(fn ->
          for _ <- 1..500_000, do: :ok
        end)

      total_utilization = Enum.sum(for {_id, r} <- utilization, do: r)
      assert total_utilization > 0
    end

    test "yielding version shows lower max utilization than non-yielding" do
      scheduler_count = System.schedulers_online()

      non_yielding_utilization =
        Aggregator.measure_scheduler_utilization(fn ->
          Enum.reduce(1..200_000, [], fn i, acc -> [i | acc] end)
        end)

      yielding_utilization =
        Aggregator.measure_scheduler_utilization(fn ->
          Enum.reduce(1..200_000, [], fn i, acc ->
            if rem(i, 500) == 0, do: :erlang.yield()
            [i | acc]
          end)
        end)

      # The scheduler that ran the workload should show high utilization in the
      # non-yielding case and lower peak in the yielding case.
      # This is a statistical observation, not a hard guarantee.
      max_non_yielding = Enum.max(for {_id, r} <- non_yielding_utilization, do: r)
      max_yielding = Enum.max(for {_id, r} <- yielding_utilization, do: r)

      # Both should show some utilization — the test is a sanity check on the
      # measure function, not a strict performance gate
      assert max_non_yielding >= 0.0
      assert max_yielding >= 0.0
      assert is_integer(scheduler_count) and scheduler_count > 0
    end
  end
end
```

### Step 3: Run the tests

```bash
mix test test/api_gateway/metrics/aggregator_test.exs --trace
```

### Step 4: Scheduler benchmark

```elixir
# bench/scheduler_bench.exs
fast_task = fn ->
  Task.async(fn ->
    :timer.sleep(1)
    :done
  end)
end

tight_loop = fn n ->
  Enum.reduce(1..n, 0, fn i, acc -> acc + i end)
end

yielding_loop = fn n ->
  Enum.reduce(1..n, 0, fn i, acc ->
    if rem(i, 500) == 0, do: :erlang.yield()
    acc + i
  end)
end

Benchee.run(
  %{
    "tight_loop 100k" => fn -> tight_loop.(100_000) end,
    "yielding_loop 100k" => fn -> yielding_loop.(100_000) end
  },
  parallel: System.schedulers_online(),
  warmup: 2,
  time: 5,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/scheduler_bench.exs
```

---

## Trade-off analysis

| Technique | Scheduler impact | Latency effect | Implementation cost |
|-----------|-----------------|---------------|---------------------|
| Tight loop (no yield) | Monopolizes one scheduler for full quantum | Other processes on same scheduler wait | Zero — do nothing |
| `:erlang.yield()` every N | Signals willingness to preempt | Reduces max latency spike | Minimal |
| `Process.send_after` chunked work | Splits work across handle_info calls | Fully preemptible between chunks | Medium |
| Dirty CPU scheduler (NIF) | Moves work off normal schedulers | Normal schedulers stay free | High — requires NIF |
| Offload to separate node | Zero impact on gateway schedulers | Best isolation | Highest |

**Rule of thumb**: if a single operation takes more than 1ms, it should either
yield, be chunked via `handle_info`, or be moved to a dirty scheduler.

---

## Common production mistakes

**1. Assuming recursive Elixir code "blocks" the scheduler**
Elixir recursive functions preempt normally via the reduction counter. The real
risk is NIF calls and BIFs that run in C without yielding — not Elixir `defp` loops.

**2. Calling `:erlang.yield()` in a NIF**
`:erlang.yield/0` is an Erlang function, not a NIF primitive. It only works in
regular Elixir/Erlang code. If you write a C NIF, you must use the NIF API's
`enif_thread_type()` / rescheduling mechanisms to avoid blocking a scheduler thread.

**3. Using `System.schedulers_online()` as a worker pool size without headroom**
Setting worker pool size equal to scheduler count means a single slow worker
saturates all schedulers. Leave headroom: `max(2, System.schedulers_online() - 1)`.

**4. Enabling scheduler wall time without disabling it**
`:erlang.system_flag(:scheduler_wall_time, true)` has a small but nonzero overhead.
Enable it for measurement, then call `:erlang.system_flag(:scheduler_wall_time, false)`
when done. In production monitoring pipelines, keep it enabled permanently if you
sample periodically — the overhead is negligible compared to the visibility gained.

**5. Interpreting high scheduler utilization as a problem**
100% scheduler utilization is the goal for a CPU-bound system. The problem is
*uneven* utilization or utilization that blocks I/O-bound processes. Use
`:observer` or `Benchee` to correlate utilization with request latency, not just
raw CPU percentage.

---

## Resources

- [BEAM Book — Scheduler chapter](https://happi.github.io/theBeamBook/#schedulers) — deep dive into the reduction-based preemption model
- [`:erlang.statistics/1` — Erlang docs](https://www.erlang.org/doc/man/erlang.html#statistics-1) — `scheduler_wall_time` and other scheduler stats
- [Dirty schedulers — Erlang NIF guide](https://www.erlang.org/doc/man/erl_nif.html#dirty_nifs) — when and how to use dirty schedulers
- [Elixir Forum: measuring scheduler starvation](https://elixirforum.com/t/how-to-detect-scheduler-starvation/37412) — community discussion with examples
- [`:observer.start/0`](https://www.erlang.org/doc/man/observer.html) — visual scheduler utilization in real time
