# 4. GenServer Back-Pressure with Internal Queues

**Difficulty**: Avanzado

## Prerequisites
- Mastered: GenServer, `handle_call/3`, `handle_cast/2`, `handle_continue/2`
- Mastered: Erlang `:queue` module — FIFO, `in/2`, `out/1`, `len/1`
- Familiarity with: Back-pressure concepts, overload protection, demand-driven systems

## Learning Objectives
- Analyze the difference between unbounded mailbox growth and explicit queue bounding
- Design a GenServer that separates work acceptance from work processing
- Evaluate overflow strategies: drop, reject, or block — and their impact on callers
- Implement a priority queue with two separate queues and deterministic drain order

## Concepts

### The BEAM Mailbox Is Unbounded by Default

Every BEAM process has a mailbox: a FIFO queue of messages waiting to be processed.
By default, it is unbounded. A GenServer under heavy load will accept every message
ever sent to it — but if it processes slower than producers send, the mailbox grows
without limit. This has several consequences:

- Memory grows until the system OOMs or the OS kills the node
- Latency degrades because old messages wait behind a growing backlog
- GC pauses increase as the process heap (which includes mailbox pointers) grows
- The BEAM scheduler gives more time to processes with large mailboxes, starving others

Back-pressure solves this by making the producer aware of the consumer's capacity.
When the GenServer is full, it signals callers — who can then slow down, retry, or fail.

### Two Layers: Mailbox and Internal Queue

A common architecture uses TWO queues:

1. **BEAM mailbox** — accepts `cast` messages for fast, non-blocking submission
2. **Internal `:queue`** — explicit bounded queue managed by the GenServer

Callers use `GenServer.cast` (fire-and-forget) to submit work. The GenServer
moves work from its mailbox into the internal queue and tracks depth. When the
internal queue exceeds a threshold, the GenServer starts replying to `call`-based
submissions with `{:error, :overloaded}`.

For even stronger guarantees, use `GenServer.call` for submissions: this gives the
caller a synchronous acknowledgment and lets the GenServer reject work explicitly.

```elixir
defmodule QueuedWorker do
  use GenServer

  @max_queue 1_000

  def submit(pid, job), do: GenServer.call(pid, {:submit, job})

  def handle_call({:submit, _job}, _from, state)
      when :queue.len(state.queue) >= @max_queue do
    {:reply, {:error, :overloaded}, state}
  end

  def handle_call({:submit, job}, _from, state) do
    new_queue = :queue.in(job, state.queue)
    new_state = %{state | queue: new_queue}
    {:reply, :ok, new_state, {:continue, :drain}}
  end

  def handle_continue(:drain, state) do
    case :queue.out(state.queue) do
      {:empty, _} -> {:noreply, state}
      {{:value, job}, rest} ->
        process(job)
        {:noreply, %{state | queue: rest}, {:continue, :drain}}
    end
  end
end
```

### The :queue Module

`:queue` implements a functional double-ended queue using two lists (front and back).
It provides O(1) amortized enqueue and dequeue. Key functions:

```elixir
q = :queue.new()
q = :queue.in(item, q)          # enqueue (back)
:queue.len(q)                   # O(n) — cache this in state if called frequently
case :queue.out(q) do           # dequeue (front)
  {{:value, item}, rest} -> # got item
  {:empty, _} ->            # empty
end
:queue.to_list(q)               # convert to list (for inspection/serialization)
:queue.from_list(list)          # restore from list
```

Important: `:queue.len/1` is O(n) — it traverses the internal lists. If you call it
on every message in a hot path, cache the size as a separate integer in your state.

### Priority Queue with Two Queues

A single FIFO queue treats all work equally. When some work is more important than
other work, you need priority. The simplest correct approach for two priority levels:
maintain two separate `:queue` instances (`high` and `low`). Always drain high-priority
work first; fall through to low-priority only when high is empty.

```
state = %{
  high_queue: :queue.new(),
  low_queue: :queue.new(),
  processing: false
}
```

Drain logic:
```
1. If high_queue is non-empty → take from high_queue
2. Else if low_queue is non-empty → take from low_queue
3. Else → done, set processing: false
```

This is deterministic and starvation-free for short bursts of low-priority work.
However, if high-priority work arrives continuously, low-priority work may starve.
For production systems, consider a weighted approach: process N high-priority items
then one low-priority item regardless of high-priority depth.

### Bounded Mailbox With :max_heap_size

As a defense-in-depth mechanism, BEAM supports killing a process when its heap
exceeds a threshold:

```elixir
def init(_) do
  Process.flag(:max_heap_size, %{size: 100_000, kill: true, error_logger: true})
  {:ok, initial_state()}
end
```

This is a last-resort safety valve, not a back-pressure mechanism. The process is
killed (not gracefully shut down) when the heap limit is exceeded. Use it alongside
explicit queue bounding, not instead of it.

### Trade-offs

| Strategy | Caller Impact | Throughput | Complexity |
|---|---|---|---|
| Unbounded mailbox (default) | No back-pressure | High short-term | None |
| Reject when full (`{:error, :overloaded}`) | Must handle error | Stable | Low |
| Block caller (call with no timeout) | Caller hangs | Throttled | Low |
| Drop oldest (evict from queue tail) | Silent data loss | Stable | Medium |
| Priority queue (two queues) | Priority-aware | Stable | Medium |
| Demand-driven (GenStage) | Explicit demand | Optimal | High |

---

## Exercises

### Exercise 1: Queue-Backed Sequential Processor

**Problem**: You have an `ImageProcessor` GenServer that encodes images. Encoding is
CPU-intensive: one image takes ~100ms. During peak traffic, clients submit 50 images
per second — 10x more than the processor can handle. Build a queue-backed processor
that accepts submissions immediately (non-blocking), processes them one at a time
in FIFO order, and reports results via callback when done.

**Requirements**:
- `submit/3` accepts `{image_id, image_data, callback_pid}` and returns `:ok` immediately
- Work is queued internally and processed one by one
- Processing is simulated with `Process.sleep(100)` + result construction
- When done, send `{:processed, image_id, result}` to `callback_pid`
- Expose `queue_depth/1` to monitor backlog

**Hints**:
- Use `handle_continue(:drain, state)` after each enqueue to trigger processing
- A `:processing` boolean flag prevents concurrent `handle_continue` chains from
  spawning multiple drain loops — check it before starting to drain
- Process one item per `handle_continue` invocation, then return another
  `{:noreply, state, {:continue, :drain}}` — this lets other messages interleave
  (like `queue_depth/1` calls) between processing steps
- The callback pattern (`send(callback_pid, result)`) decouples the processor from
  any specific response mechanism — the caller registers where results go

**One possible solution**:
```elixir
defmodule ImageProcessor do
  use GenServer

  def start_link(_opts), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def submit(image_id, image_data, callback_pid) do
    GenServer.call(__MODULE__, {:submit, image_id, image_data, callback_pid})
  end

  def queue_depth, do: GenServer.call(__MODULE__, :queue_depth)

  def init(_) do
    {:ok, %{queue: :queue.new(), depth: 0, processing: false}}
  end

  def handle_call({:submit, id, data, cb}, _from, state) do
    job = {id, data, cb}
    new_state = %{state |
      queue: :queue.in(job, state.queue),
      depth: state.depth + 1
    }
    continuation = if state.processing, do: [], else: {:continue, :drain}
    {:reply, :ok, new_state, continuation}
  end

  def handle_call(:queue_depth, _from, state) do
    {:reply, state.depth, state}
  end

  def handle_continue(:drain, state) do
    case :queue.out(state.queue) do
      {:empty, _} ->
        {:noreply, %{state | processing: false}}

      {{:value, {id, data, cb}}, rest} ->
        result = encode_image(id, data)
        send(cb, {:processed, id, result})
        new_state = %{state | queue: rest, depth: state.depth - 1, processing: true}
        {:noreply, new_state, {:continue, :drain}}
    end
  end

  defp encode_image(id, _data) do
    Process.sleep(100)
    %{id: id, encoded_at: System.system_time(:second), size_bytes: :rand.uniform(1_000_000)}
  end
end
```

---

### Exercise 2: Bounded Queue With Overflow Rejection

**Problem**: Your `EmailDispatcher` GenServer sends transactional emails. During a
marketing campaign, submissions can spike to 10,000 emails per minute. The SMTP
relay accepts at most 100 emails per minute. Without bounding, the internal queue
would grow to millions of items and exhaust memory. Implement a bounded queue that
rejects submissions when the queue exceeds 1,000 items, returning
`{:error, :overloaded}` to callers so they can retry later or apply client-side
back-pressure.

**Requirements**:
- Queue bounded to 1,000 items maximum
- Submission above the limit: immediate `{:error, :overloaded}` — no queuing
- Submission within the limit: `{:ok, queued_at}` with a monotonic timestamp
- Processing: one email per 600ms (simulated), FIFO order
- Expose `stats/1`: `{queued: n, processed: n, rejected: n}`

**Hints**:
- Track `depth` as a separate integer counter — do NOT call `:queue.len/1` in `handle_call`
  because it is O(n) and will degrade with large queues. Maintain the count manually:
  increment on enqueue, decrement on dequeue
- Use a monotonic clock for `queued_at`: `System.monotonic_time(:millisecond)` — this
  lets you calculate queue wait time when the item is processed
- The 600ms processing rate can be simulated with `Process.sleep(600)` inside
  `handle_continue` — but note this blocks the GenServer for 600ms per item. In a
  real system you would spawn a Task; for this exercise, sequential is acceptable
- When rejecting, still return quickly — do not block the caller

**One possible solution**:
```elixir
defmodule EmailDispatcher do
  use GenServer

  @max_queue 1_000
  @process_interval_ms 600

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def dispatch(email), do: GenServer.call(__MODULE__, {:dispatch, email})
  def stats, do: GenServer.call(__MODULE__, :stats)

  def init(_) do
    state = %{
      queue: :queue.new(),
      depth: 0,
      stats: %{queued: 0, processed: 0, rejected: 0},
      processing: false
    }
    {:ok, state}
  end

  def handle_call({:dispatch, _email}, _from, %{depth: d} = state) when d >= @max_queue do
    new_stats = Map.update!(state.stats, :rejected, &(&1 + 1))
    {:reply, {:error, :overloaded}, %{state | stats: new_stats}}
  end

  def handle_call({:dispatch, email}, _from, state) do
    queued_at = System.monotonic_time(:millisecond)
    entry = {email, queued_at}
    new_state = %{state |
      queue: :queue.in(entry, state.queue),
      depth: state.depth + 1,
      stats: Map.update!(state.stats, :queued, &(&1 + 1))
    }
    continuation = if state.processing, do: [], else: {:continue, :drain}
    {:reply, {:ok, queued_at}, new_state, continuation}
  end

  def handle_call(:stats, _from, state) do
    {:reply, Map.put(state.stats, :depth, state.depth), state}
  end

  def handle_continue(:drain, state) do
    case :queue.out(state.queue) do
      {:empty, _} ->
        {:noreply, %{state | processing: false}}

      {{:value, {email, queued_at}}, rest} ->
        send_email(email, queued_at)
        new_stats = Map.update!(state.stats, :processed, &(&1 + 1))
        new_state = %{state |
          queue: rest,
          depth: state.depth - 1,
          stats: new_stats,
          processing: true
        }
        {:noreply, new_state, {:continue, :drain}}
    end
  end

  defp send_email(email, queued_at) do
    wait_ms = System.monotonic_time(:millisecond) - queued_at
    require Logger
    Logger.debug("Sending email to #{email.to} (waited #{wait_ms}ms in queue)")
    Process.sleep(@process_interval_ms)
  end
end
```

---

### Exercise 3: Two-Level Priority Queue

**Problem**: Your `NotificationService` GenServer sends two kinds of notifications:
`:critical` (security alerts, payment failures) and `:normal` (marketing, digests).
During high load, critical notifications must never be delayed by normal ones. Build
a priority queue that always drains critical notifications first, falling back to
normal ones only when the critical queue is empty. Both queues are individually
bounded to 500 items.

**Requirements**:
- `notify/2` accepts `{message, priority}` where priority is `:critical` or `:normal`
- Each priority level has its own bounded queue (500 items each)
- Drain: always check critical queue first; only pull from normal if critical is empty
- Submission to a full queue returns `{:error, {:overloaded, priority}}`
- Expose `queue_depths/1`: `{critical: n, normal: n}`
- Prove priority ordering with a test: fill both queues, then drain and verify
  all critical messages come before any normal messages in the processing log

**Hints**:
- State: `%{critical: :queue.new(), normal: :queue.new(), crit_depth: 0, norm_depth: 0}`
- The drain function should be a private `next_job/1` that encapsulates the priority
  selection logic: check critical first, then normal — return `{job, new_state}` or `:empty`
- For the ordering test: use `Agent` or a simple list (passed by reference via pid)
  to collect processing order, then assert all items from one group precede another
- Consider what happens when critical items arrive while a normal item is being processed —
  in the sequential model, the normal item finishes first. This is acceptable for this
  exercise; document it as a known limitation

**One possible solution**:
```elixir
defmodule NotificationService do
  use GenServer

  @max_per_level 500

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def notify(message, priority), do: GenServer.call(__MODULE__, {:notify, message, priority})
  def queue_depths, do: GenServer.call(__MODULE__, :queue_depths)

  def init(_) do
    state = %{
      critical: :queue.new(),
      normal: :queue.new(),
      crit_depth: 0,
      norm_depth: 0,
      processing: false
    }
    {:ok, state}
  end

  def handle_call({:notify, _msg, :critical}, _from, %{crit_depth: d} = state)
      when d >= @max_per_level do
    {:reply, {:error, {:overloaded, :critical}}, state}
  end

  def handle_call({:notify, _msg, :normal}, _from, %{norm_depth: d} = state)
      when d >= @max_per_level do
    {:reply, {:error, {:overloaded, :normal}}, state}
  end

  def handle_call({:notify, msg, priority}, _from, state) do
    new_state = enqueue(state, msg, priority)
    continuation = if state.processing, do: [], else: {:continue, :drain}
    {:reply, :ok, new_state, continuation}
  end

  def handle_call(:queue_depths, _from, state) do
    {:reply, %{critical: state.crit_depth, normal: state.norm_depth}, state}
  end

  def handle_continue(:drain, state) do
    case next_job(state) do
      :empty ->
        {:noreply, %{state | processing: false}}

      {job, new_state} ->
        process_notification(job)
        {:noreply, %{new_state | processing: true}, {:continue, :drain}}
    end
  end

  # Priority selection: critical always wins
  defp next_job(%{crit_depth: 0, norm_depth: 0}), do: :empty

  defp next_job(%{crit_depth: d} = state) when d > 0 do
    {{:value, job}, rest} = :queue.out(state.critical)
    {job, %{state | critical: rest, crit_depth: state.crit_depth - 1}}
  end

  defp next_job(state) do
    {{:value, job}, rest} = :queue.out(state.normal)
    {job, %{state | normal: rest, norm_depth: state.norm_depth - 1}}
  end

  defp enqueue(state, msg, :critical) do
    %{state |
      critical: :queue.in(msg, state.critical),
      crit_depth: state.crit_depth + 1
    }
  end

  defp enqueue(state, msg, :normal) do
    %{state |
      normal: :queue.in(msg, state.normal),
      norm_depth: state.norm_depth + 1
    }
  end

  defp process_notification(job) do
    require Logger
    Logger.debug("Sending notification: #{inspect(job)}")
    # Simulate delivery
    Process.sleep(10)
  end
end
```

---

## Common Mistakes

### Mistake: Using :queue.len/1 in Hot-Path Callbacks

`:queue.len/1` traverses both internal lists to count elements — it is O(n). Calling
it in `handle_call` on every submission means that as the queue grows, each submission
takes longer to execute, creating a feedback loop: more load → slower acceptance →
longer call latency → callers queue up → GenServer mailbox grows. Always maintain
a separate `depth` counter in state and update it manually on enqueue and dequeue.

### Mistake: Starting a Drain Loop Without a Processing Guard

If you trigger `{:continue, :drain}` from every `handle_call` submission, and the
drain loop itself ends with another `{:continue, :drain}` after each item, you get
at most one drain chain running — because a GenServer processes one callback at a
time. However, if you trigger drain from both `handle_call` AND `handle_cast`, and
you use `handle_cast` for a separate enqueueing path, you may accidentally start two
drain chains. Use a `:processing` boolean to gate drain entry.

### Mistake: Blocking the GenServer During Long Processing Steps

Calling `Process.sleep(600)` inside `handle_continue` blocks the entire GenServer
for 600ms. During that time, no `handle_call` can be served — including `queue_depth/1`
or any submission. For production systems, spawn a Task for the actual work and let
`handle_info` collect the result. This keeps the GenServer responsive at the cost of
slightly higher complexity.

### Mistake: Silent Queue Overflow With Cast-Based Submission

Using `GenServer.cast` for submissions means callers get no acknowledgment and no
error signal when the queue is full. Items are simply dropped (if you check in
`handle_cast`) or silently enqueued beyond the limit. Always use `GenServer.call`
for bounded queue submission so callers can handle `{:error, :overloaded}` and
implement their own back-pressure or retry logic.

---

## Summary
- Maintain explicit queue depth counters in state — never call `:queue.len/1` in hot paths
- Use `GenServer.call` for submissions to bounded queues: callers need the rejection signal
- `handle_continue` for drain loops keeps processing sequential and interleaved with messages
- Priority queues need deterministic drain order: always check high before low; document starvation risks
- For production: spawn Tasks for long processing steps to keep the GenServer responsive

## What's Next
Exercise 05 — Hot State Migration: `code_change/3` and `:sys.change_code` for live
state migration during hot code upgrades.

## Resources
- Erlang docs — `:queue` module: https://www.erlang.org/doc/man/queue.html
- BEAM documentation — Process flags: `:max_heap_size`
- Saša Jurić — "Elixir in Action" — ch. 12 (system limits, process management)
- GenStage hex docs — demand-driven back-pressure: https://hexdocs.pm/gen_stage
