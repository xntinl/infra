# 1. GenServer Hibernation & State Compaction

**Difficulty**: Avanzado

## Prerequisites
- Mastered: GenServer lifecycle, `handle_call/3`, `handle_cast/2`, `handle_info/2`
- Mastered: BEAM memory model, process heap, garbage collection
- Familiarity with: `:recon` library, `:erlang.hibernate/3`, process flags

## Learning Objectives
- Analyze trade-offs between memory savings and latency introduced by hibernation
- Design a cache GenServer that automatically hibernates idle processes
- Evaluate when state compaction before hibernation is worth the CPU cost
- Measure P99 latency impact using `:recon` and `:timer` tools

## Concepts

### Process Hibernation in the BEAM

In the BEAM, every process has its own heap. When a GenServer is idle, its heap still occupies
memory — even if it holds no useful work to do. `:erlang.hibernate/3` is a low-level primitive
that suspends a process, runs a full garbage collection on its heap, and puts it in a minimal
footprint state. The process is resumed only when a message arrives in its mailbox.

GenServer exposes this via the `:hibernate` atom in return tuples. Returning
`{:noreply, state, :hibernate}` from any callback will trigger hibernation immediately after
the callback returns. The process wakes up when a message arrives and executes the appropriate
callback normally — there is no explicit "wake up" handler.

The cost of hibernation is real: the first message after waking incurs a cold-heap penalty
because the process must rebuild its stack and potentially page in data. In latency-sensitive
systems, this manifests as P99 spikes on calls that hit a dormant process. You must measure
this before deploying hibernation in production.

```elixir
defmodule Cache.Worker do
  use GenServer

  @inactivity_ms 30_000

  def start_link(key) do
    GenServer.start_link(__MODULE__, key)
  end

  def init(key) do
    {:ok, %{key: key, value: nil, last_access: monotonic()}}
  end

  # Return :hibernate after a timeout period — the next handle_info(:timeout) triggers it
  def handle_call(:get, _from, state) do
    new_state = %{state | last_access: monotonic()}
    {:reply, state.value, new_state, @inactivity_ms}
  end

  # Inactivity fires — go dormant
  def handle_info(:timeout, state) do
    {:noreply, compact_state(state), :hibernate}
  end

  defp compact_state(state), do: Map.drop(state, [:last_access])
  defp monotonic, do: System.monotonic_time(:millisecond)
end
```

### State Compaction Before Hibernation

Hibernation runs GC on the current heap. If the state still holds large binaries, reference-
counted sub-binaries, or ETS references pointing to large structures, GC cannot collect them —
the memory stays high even after hibernation. Compaction means explicitly reducing the state to
its smallest meaningful representation before calling `:hibernate`.

Patterns for compaction:
- Drop fields that can be recomputed on wake-up (timestamps, derived caches)
- Serialize large in-memory structures to a compact binary and store the binary
- Move data to ETS and keep only the table reference in state
- Truncate history lists to a fixed maximum

The trade-off is CPU: compaction runs on every hibernation. If your processes hibernate
frequently and their state is large, compaction can become a bottleneck. Profile first.

```elixir
defmodule EventBuffer do
  use GenServer

  @max_history 100

  def handle_info(:timeout, state) do
    compacted = %{
      id: state.id,
      # Truncate unbounded history before hibernating
      recent_events: Enum.take(state.events, @max_history),
      snapshot: summarize(state.events)
    }
    {:noreply, compacted, :hibernate}
  end

  defp summarize(events) do
    %{
      count: length(events),
      last_seen: List.first(events)
    }
  end
end
```

### Measuring Memory with :recon

`:recon` is a production-safe introspection library. It does not require pausing the scheduler
and is safe to run on live nodes. The key function for memory analysis is:

```elixir
# Top 10 processes by memory usage
:recon.proc_count(:memory, 10)

# Returns: [{pid, memory_bytes, [registered_name: ..., current_function: ...]}]
```

To measure hibernation impact:
1. Spawn N idle GenServers, record baseline memory with `:recon.proc_count`
2. Let inactivity timeout trigger hibernation on all processes
3. Record memory again — difference shows hibernation savings
4. Send a message to a hibernated process, measure response time with `:timer.tc`

### Trade-offs

| Aspect | Before Hibernation | After Hibernation |
|---|---|---|
| Memory (idle process) | ~2–8 KB heap | ~0.5–1 KB (GC'd) |
| Memory (with large state) | High | High unless compacted |
| First-message latency | Baseline | +50–500 µs (GC + heap rebuild) |
| Subsequent latency | Baseline | Baseline |
| Scheduler impact | None | Brief GC pause on wake |
| Complexity | Low | Medium (compaction logic needed) |

**When to use**: Cache workers with thousands of keys where most are idle, session servers
where users are active for short bursts, any "fan-out" server farm where active/idle ratio
is low.

**When to avoid**: Processes that receive messages at <100ms intervals (hibernation overhead
exceeds benefit), real-time systems where P99 latency is a hard SLA.

---

## Exercises

### Exercise 1: Hibernating Cache Worker

**Problem**: You run a distributed cache where each key is owned by a dedicated GenServer
process. At peak, you have 50,000 key-worker processes. A profiling session shows that
at any moment only ~2,000 workers are actively serving requests — the other 48,000 are
idle, each consuming ~4 KB of heap. Your ops team reports 192 MB of wasted process heap.
Implement automatic hibernation after 30 seconds of inactivity.

**Requirements**:
- Worker hibernates after exactly 30s of no `get` or `put` calls
- Any call resets the inactivity timer
- Hibernation must be transparent: callers see no difference in behavior
- Track hibernation count in state so tests can assert it happened

**Hints**:
- The GenServer timeout mechanism (`{:reply, val, state, timeout_ms}`) fires a
  `handle_info(:timeout, state)` — combine this with `:hibernate` in that handler
- Do not use `:timer.send_after` for this — the built-in timeout is reset automatically
  on each callback return, which is exactly the semantic you want
- After hibernation, the process wakes on the next message — no special handling needed
  in `handle_call` because GenServer dispatches normally post-wake

**One possible solution**:
```elixir
defmodule Cache.Worker do
  use GenServer

  @hibernate_after_ms 30_000

  def start_link({key, initial_value}) do
    GenServer.start_link(__MODULE__, {key, initial_value})
  end

  def get(pid), do: GenServer.call(pid, :get)
  def put(pid, value), do: GenServer.call(pid, {:put, value})

  def init({key, value}) do
    state = %{key: key, value: value, hibernations: 0}
    {:ok, state, @hibernate_after_ms}
  end

  def handle_call(:get, _from, state) do
    {:reply, state.value, state, @hibernate_after_ms}
  end

  def handle_call({:put, value}, _from, state) do
    {:reply, :ok, %{state | value: value}, @hibernate_after_ms}
  end

  def handle_info(:timeout, state) do
    new_state = %{state | hibernations: state.hibernations + 1}
    {:noreply, new_state, :hibernate}
  end

  # Expose for testing
  def hibernation_count(pid), do: GenServer.call(pid, :hibernation_count)

  # ... rest of implementation
end
```

---

### Exercise 2: Unbounded History Compaction

**Problem**: You have a `MetricsCollector` GenServer that accumulates raw event structs
throughout the day. Each event is ~500 bytes. By end of day, a single collector holds
~86,400 events (one per second) — that is 43 MB of in-process heap. The collector is
idle overnight. Design a compaction strategy that runs at hibernation time to reduce
the heap footprint to under 1 MB while preserving enough data to answer "what happened
in the last hour" queries after the process wakes.

**Requirements**:
- Full event list is maintained during active hours
- On hibernation: compact to `{summary, recent_events}` where `recent_events` is the
  last 3,600 entries and `summary` captures aggregate statistics
- On wake (first call after hibernation): log a warning that state was compacted
- Compaction must be idempotent: compacting an already-compacted state is safe

**Hints**:
- Use a version tag in state to distinguish full vs. compacted: `%{mode: :full, ...}`
  vs. `%{mode: :compact, ...}`. Pattern match on `mode` in callbacks
- `:erlang.term_to_binary/2` with `[:compressed]` can reduce binary size by 60–80%
  for repetitive data, but adds CPU cost on deserialization — measure before using
- The `handle_info(:timeout, state)` → `{:noreply, compact(state), :hibernate}` chain
  runs synchronously; keep compaction under 100ms or it blocks the process

**One possible solution**:
```elixir
defmodule MetricsCollector do
  use GenServer

  @inactivity_ms 60_000
  @recent_window 3_600

  def init(collector_id) do
    {:ok, %{id: collector_id, mode: :full, events: [], summary: nil}, @inactivity_ms}
  end

  def handle_call(:query_recent, _from, %{mode: :compact} = state) do
    # Log degraded mode so operators are aware
    require Logger
    Logger.warning("MetricsCollector #{state.id} serving from compacted state")
    {:reply, {:compacted, state.recent_events}, state, @inactivity_ms}
  end

  def handle_call(:query_recent, _from, %{mode: :full} = state) do
    recent = Enum.take(state.events, @recent_window)
    {:reply, {:full, recent}, state, @inactivity_ms}
  end

  def handle_info(:timeout, state) do
    {:noreply, compact(state), :hibernate}
  end

  defp compact(%{mode: :compact} = state), do: state

  defp compact(%{mode: :full, events: events} = state) do
    %{
      id: state.id,
      mode: :compact,
      recent_events: Enum.take(events, @recent_window),
      summary: build_summary(events)
    }
  end

  defp build_summary(events) do
    # Aggregate: do not store raw events in summary
    %{
      total_count: length(events),
      # ... aggregate fields
    }
  end
end
```

---

### Exercise 3: Measuring P99 Latency Impact

**Problem**: Before proposing hibernation to your team you need hard numbers. Write a
benchmark module that spawns 100 worker GenServers (use Exercise 1's `Cache.Worker`),
measures baseline P99 call latency, forces hibernation on all of them, then measures
the first-call P99 after wake-up. Report the delta.

**Requirements**:
- Spawn 100 workers, warm them up with 1,000 calls each
- Record latency of 10,000 calls distributed across all workers (`:timer.tc`)
- Force hibernation: send a direct message that triggers `:hibernate` or wait for timeout
- Record latency of 100 calls (one per worker, first-call-after-hibernate)
- Output: `baseline_p99_µs`, `post_hibernate_p99_µs`, `overhead_µs`

**Hints**:
- `:timer.tc(fn -> GenServer.call(pid, :get) end)` returns `{microseconds, result}`
- To calculate P99: sort the latency list, take element at index `floor(length * 0.99)`
- You can force hibernation without waiting 30s by using `:sys.replace_state/2` to
  corrupt the timer, then sending `:timeout` directly with `send(pid, :timeout)`
- Use `Process.sleep(10)` between the hibernate trigger and the measurement call to
  ensure the process has fully entered hibernation before you call it

**One possible solution**:
```elixir
defmodule HibernationBenchmark do
  @worker_count 100
  @warmup_calls 1_000
  @measure_calls 10_000

  def run do
    workers = spawn_workers()
    warmup(workers)

    baseline = measure_p99(workers, @measure_calls)
    force_hibernate(workers)
    # Give all processes time to fully enter hibernation
    Process.sleep(50)
    post_hibernate = measure_p99(workers, @worker_count)

    IO.puts("Baseline P99:      #{baseline} µs")
    IO.puts("Post-hibernate P99: #{post_hibernate} µs")
    IO.puts("Overhead:          #{post_hibernate - baseline} µs")
  end

  defp spawn_workers do
    for i <- 1..@worker_count do
      {:ok, pid} = Cache.Worker.start_link({"key_#{i}", nil})
      pid
    end
  end

  defp warmup(workers) do
    for _ <- 1..@warmup_calls, w <- workers do
      Cache.Worker.get(w)
    end
  end

  defp measure_p99(workers, n) do
    latencies =
      for _ <- 1..n do
        worker = Enum.random(workers)
        {µs, _} = :timer.tc(fn -> Cache.Worker.get(worker) end)
        µs
      end

    percentile(latencies, 99)
  end

  defp force_hibernate(workers) do
    Enum.each(workers, fn pid -> send(pid, :timeout) end)
  end

  defp percentile(list, p) do
    sorted = Enum.sort(list)
    idx = floor(length(sorted) * p / 100)
    Enum.at(sorted, idx)
  end
end
```

---

## Common Mistakes

### Mistake: Using `:timer.send_after` Instead of Built-in Timeout

Calling `:timer.send_after(@delay, self(), :hibernate_now)` in `init/1` and then
resetting it in every callback creates timer leaks. If a callback forgets to cancel
the previous timer reference before scheduling a new one, you accumulate phantom timers
that fire after the process has moved on. The built-in GenServer timeout
(`{:reply, val, state, ms}`) is reset automatically on every callback return — it is
the correct primitive for inactivity detection.

### Mistake: Hibernating Without Compacting Large Binaries

A process can hold a reference-counted sub-binary that points into a large binary on
the shared heap. Even after GC, that reference keeps the large binary alive. Hibernation
will not reclaim it. You must explicitly nil out or drop those fields before hibernating.
Use `:recon_trace` or `:erlang.process_info(pid, :binary)` to audit binary references.

### Mistake: Hibernating Frequently-Active Processes

If a process receives a message every 200ms, putting it to sleep for 30s of inactivity
means it will never hibernate in practice — which is fine. But if you set the threshold
too low (e.g., 1s) on a process that receives bursts separated by 1.5s gaps, you create
a pathological pattern: hibernate → wake (latency spike) → work → hibernate → wake.
Profile your actual traffic pattern before choosing the inactivity threshold.

### Mistake: Assuming Hibernation Is Free on Wake

The first call to a hibernated process must rebuild the process stack and may trigger
paging if the OS swapped the process memory. On loaded systems, post-hibernation P99 can
be 10x higher than baseline. Always measure on hardware similar to production.

---

## Summary
- `:hibernate` reduces idle process memory significantly; combined with state compaction
  it can cut per-process footprint by 80–95%
- The cost is first-message latency: plan for P99 spikes of 100–500µs on wake-up
- Use the built-in GenServer timeout, not `:timer.send_after`, for inactivity detection
- Always measure with `:recon` and `:timer.tc` before and after — never assume

## What's Next
Exercise 02 — `handle_continue/2`: lazy initialization and async recovery patterns that
complement hibernation by keeping startup fast while deferring expensive work.

## Resources
- Erlang docs: `:erlang.hibernate/3` — https://www.erlang.org/doc/man/erlang.html#hibernate-3
- Fred Hébert — "Stuff Goes Bad: Erlang in Anger" (`:recon` usage)
- Saša Jurić — "Elixir in Action" ch. 12 (process internals)
- BEAM Wisdoms — Process Memory Layout — http://beam-wisdoms.clau.se/en/latest/
