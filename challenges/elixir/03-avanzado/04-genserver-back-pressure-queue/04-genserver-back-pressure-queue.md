# GenServer Back-Pressure with Internal Queues

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway forwards requests to upstream services and
collects audit log entries for every request. The audit writer receives up to 5,000
entries per second during peak traffic, but the downstream log storage accepts at most
500 writes per second. Without back-pressure, the writer's mailbox grows without limit
and the node eventually OOMs.

You also need a two-level priority queue for outgoing requests: `:critical` traffic
(health checks, payment confirmations) must never be delayed by `:normal` traffic
(catalog browsing, analytics pings) regardless of queue depth.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── middleware/
│           ├── audit_writer.ex        # ← you implement this
│           └── priority_dispatcher.ex # ← you implement this
├── test/
│   └── api_gateway/
│       └── middleware/
│           ├── audit_writer_test.exs       # given tests — must pass
│           └── priority_dispatcher_test.exs # given tests — must pass
└── mix.exs
```

---

## Why unbounded mailboxes are a production hazard

Every BEAM process has a mailbox: a FIFO queue of messages waiting to be processed.
By default it is unbounded. A GenServer under heavy load will accept every message
ever sent to it — but if it processes slower than producers send, the mailbox grows
without limit:

- Memory grows until the node OOMs or the OS kills it
- Latency degrades because old messages wait behind a growing backlog
- GC pauses increase as the process heap (which includes mailbox pointers) grows
- The BEAM scheduler grants more time to processes with large mailboxes, starving others

Back-pressure makes the producer aware of the consumer's capacity. When the GenServer
is full, it signals callers who can then slow down, retry, or fail fast.

---

## Two-layer queue architecture

```
Producer                GenServer
  │                        │
  │  GenServer.call        │
  ├───────────────────────▶│  ← mailbox (BEAM layer)
  │                        │
  │  {:ok, _} | {:error,   │  ← bounded internal queue
  │   :overloaded}         │     (explicit `:queue` + depth counter)
  ◀───────────────────────▶│
                           │  ← drain via handle_continue
                           │
                      downstream
```

The mailbox is the BEAM's natural queue. The internal `:queue` is an explicit structure
you manage. By checking depth before enqueuing, you can reject work **before** it enters
the queue — giving callers a synchronous rejection signal.

---

## The `:queue` module

`:queue` implements a functional double-ended queue using two lists (front and back).
It provides O(1) amortised enqueue and dequeue.

```elixir
q = :queue.new()
q = :queue.in(item, q)      # enqueue to back — O(1) amortised
case :queue.out(q) do
  {{:value, item}, rest} -> # got item from front
  {:empty, _q}           -> # queue is empty
end
:queue.len(q)               # O(n) — NEVER call this in a hot path
```

**Critical**: `:queue.len/1` is O(n). Calling it in `handle_call` on every submission
creates a feedback loop: more load → slower acceptance → longer call latency →
callers queue up → mailbox grows. Always maintain a separate integer depth counter
in state and update it manually on enqueue and dequeue.

---

## Implementation

### Step 1: `lib/api_gateway/middleware/audit_writer.ex`

```elixir
defmodule ApiGateway.Middleware.AuditWriter do
  use GenServer
  require Logger

  @max_queue      1_000
  @write_delay_ms 2     # simulates ~500 writes/s (1000 ms / 500 = 2 ms/write)

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Enqueues an audit log entry. Returns {:ok, queued_at} or {:error, :overloaded}.
  """
  @spec write(map()) :: {:ok, integer()} | {:error, :overloaded}
  def write(entry), do: GenServer.call(__MODULE__, {:write, entry})

  @doc "Returns %{queued: n, written: n, rejected: n, depth: n}."
  @spec stats() :: map()
  def stats, do: GenServer.call(__MODULE__, :stats)

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    state = %{
      queue:      :queue.new(),
      depth:      0,
      processing: false,
      stats:      %{queued: 0, written: 0, rejected: 0}
    }
    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:write, _entry}, _from, state) when state.depth >= @max_queue do
    # TODO: reject the entry, increment stats.rejected, return {:error, :overloaded}
    # HINT: do not modify the queue
  end

  @impl true
  def handle_call({:write, entry}, _from, state) do
    # TODO:
    # 1. timestamp the entry with System.monotonic_time(:millisecond)
    # 2. enqueue {entry, queued_at} into state.queue
    # 3. increment depth and stats.queued
    # 4. if not already processing, trigger {:continue, :drain}
    # 5. reply {:ok, queued_at}
    #
    # HINT: use state.processing to gate drain entry
    # HINT: {:reply, {:ok, queued_at}, new_state, {:continue, :drain}}
    #       (only include {:continue, :drain} when NOT already processing)
  end

  @impl true
  def handle_call(:stats, _from, state) do
    {:reply, Map.put(state.stats, :depth, state.depth), state}
  end

  @impl true
  def handle_continue(:drain, state) do
    # TODO: dequeue one item, write it, decrement depth, continue drain
    # When queue is empty: set processing: false and stop the chain
    #
    # HINT: case :queue.out(state.queue) do
    #   {:empty, _} -> {:noreply, %{state | processing: false}}
    #   {{:value, {entry, queued_at}}, rest} ->
    #     do_write(entry, queued_at)
    #     new_state = %{state | queue: rest, depth: state.depth - 1,
    #                           processing: true,
    #                           stats: Map.update!(state.stats, :written, &(&1 + 1))}
    #     {:noreply, new_state, {:continue, :drain}}
    # end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp do_write(entry, queued_at) do
    wait_ms = System.monotonic_time(:millisecond) - queued_at
    Logger.debug("Writing audit entry (waited #{wait_ms} ms): #{inspect(entry)}")
    Process.sleep(@write_delay_ms)
  end
end
```

### Step 2: `lib/api_gateway/middleware/priority_dispatcher.ex`

```elixir
defmodule ApiGateway.Middleware.PriorityDispatcher do
  use GenServer
  require Logger

  @max_per_level   500
  @dispatch_delay_ms 1

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Dispatches a request. Priority is :critical or :normal.
  Returns :ok or {:error, {:overloaded, priority}}.
  """
  @spec dispatch(map(), :critical | :normal) :: :ok | {:error, {:overloaded, :critical | :normal}}
  def dispatch(request, priority), do: GenServer.call(__MODULE__, {:dispatch, request, priority})

  @doc "Returns %{critical: n, normal: n} queue depths."
  @spec queue_depths() :: %{critical: non_neg_integer(), normal: non_neg_integer()}
  def queue_depths, do: GenServer.call(__MODULE__, :queue_depths)

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    state = %{
      critical:    :queue.new(),
      normal:      :queue.new(),
      crit_depth:  0,
      norm_depth:  0,
      processing:  false
    }
    {:ok, state}
  end

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def handle_call({:dispatch, _req, :critical}, _from, state)
      when state.crit_depth >= @max_per_level do
    {:reply, {:error, {:overloaded, :critical}}, state}
  end

  @impl true
  def handle_call({:dispatch, _req, :normal}, _from, state)
      when state.norm_depth >= @max_per_level do
    {:reply, {:error, {:overloaded, :normal}}, state}
  end

  @impl true
  def handle_call({:dispatch, request, priority}, _from, state) do
    # TODO: enqueue into the correct queue, increment the corresponding depth counter
    # Start drain if not already processing
    # HINT: similar pattern to AuditWriter
  end

  @impl true
  def handle_call(:queue_depths, _from, state) do
    {:reply, %{critical: state.crit_depth, normal: state.norm_depth}, state}
  end

  @impl true
  def handle_continue(:drain, state) do
    # TODO: call next_job/1 to select the highest-priority item
    # Process it, decrement the appropriate depth, continue drain
    # When empty: {:noreply, %{state | processing: false}}
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp next_job(state) do
    # TODO: implement priority selection
    # 1. If crit_depth > 0: dequeue from critical queue
    # 2. Else if norm_depth > 0: dequeue from normal queue
    # 3. Else: return :empty
    #
    # Return: :empty | {request, updated_state}
  end

  defp do_dispatch(request) do
    Logger.debug("Dispatching: #{inspect(request)}")
    Process.sleep(@dispatch_delay_ms)
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/middleware/audit_writer_test.exs
defmodule ApiGateway.Middleware.AuditWriterTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Middleware.AuditWriter

  setup do
    start_supervised!(AuditWriter)
    :ok
  end

  test "accepts entries within the limit" do
    results = for i <- 1..10, do: AuditWriter.write(%{request_id: i})
    assert Enum.all?(results, fn
      {:ok, ts} when is_integer(ts) -> true
      _ -> false
    end)
  end

  test "rejects entries when queue is full" do
    # Fill the queue by bypassing the drain (replace_state to freeze processing)
    :sys.replace_state(Process.whereis(AuditWriter), fn s ->
      entries = for i <- 1..1_000, do: {%{id: i}, 0}
      q = Enum.reduce(entries, :queue.new(), fn e, q -> :queue.in(e, q) end)
      %{s | queue: q, depth: 1_000, processing: true}
    end)

    assert {:error, :overloaded} = AuditWriter.write(%{request_id: :overflow})
  end

  test "stats reflect queued and rejected counts" do
    AuditWriter.write(%{request_id: "s1"})
    AuditWriter.write(%{request_id: "s2"})
    Process.sleep(50)

    stats = AuditWriter.stats()
    assert stats.queued >= 2
    assert is_integer(stats.written)
    assert is_integer(stats.rejected)
  end
end
```

```elixir
# test/api_gateway/middleware/priority_dispatcher_test.exs
defmodule ApiGateway.Middleware.PriorityDispatcherTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Middleware.PriorityDispatcher

  setup do
    start_supervised!(PriorityDispatcher)
    :ok
  end

  test "critical and normal requests are accepted" do
    assert :ok = PriorityDispatcher.dispatch(%{path: "/health"}, :critical)
    assert :ok = PriorityDispatcher.dispatch(%{path: "/catalog"}, :normal)
  end

  test "rejects critical when critical queue is full" do
    pid = Process.whereis(PriorityDispatcher)
    :sys.replace_state(pid, fn s -> %{s | crit_depth: 500, processing: true} end)
    assert {:error, {:overloaded, :critical}} =
      PriorityDispatcher.dispatch(%{path: "/health"}, :critical)
  end

  test "rejects normal when normal queue is full but critical still accepted" do
    pid = Process.whereis(PriorityDispatcher)
    :sys.replace_state(pid, fn s -> %{s | norm_depth: 500, processing: true} end)
    assert {:error, {:overloaded, :normal}} =
      PriorityDispatcher.dispatch(%{path: "/catalog"}, :normal)
    assert :ok = PriorityDispatcher.dispatch(%{path: "/health"}, :critical)
  end

  test "queue_depths returns correct counts" do
    pid = Process.whereis(PriorityDispatcher)
    :sys.replace_state(pid, fn s ->
      %{s | crit_depth: 3, norm_depth: 7, processing: true}
    end)
    assert %{critical: 3, normal: 7} = PriorityDispatcher.queue_depths()
  end

  test "priority ordering: all critical processed before any normal" do
    collector = self()

    # Bypass drain so we control when processing starts
    pid = Process.whereis(PriorityDispatcher)
    :sys.replace_state(pid, fn s -> %{s | processing: true} end)

    # Submit both priorities
    for i <- 1..5 do
      :sys.replace_state(pid, fn s ->
        req = %{id: "c#{i}", priority: :critical}
        %{s | critical: :queue.in(req, s.critical), crit_depth: s.crit_depth + 1}
      end)
    end
    for i <- 1..5 do
      :sys.replace_state(pid, fn s ->
        req = %{id: "n#{i}", priority: :normal}
        %{s | normal: :queue.in(req, s.normal), norm_depth: s.norm_depth + 1}
      end)
    end

    # Resume drain
    :sys.replace_state(pid, fn s -> %{s | processing: false} end)
    PriorityDispatcher.dispatch(%{id: "trigger"}, :critical)
    Process.sleep(200)

    # At this point the drain should have consumed everything
    depths = PriorityDispatcher.queue_depths()
    assert depths.critical == 0
    assert depths.normal == 0
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/middleware/ --trace
```

---

## Trade-off analysis

| Strategy | Caller impact | Throughput | Complexity |
|----------|---------------|------------|------------|
| Unbounded mailbox (default) | No back-pressure | High short-term, crash long-term | None |
| Reject when full (`{:error, :overloaded}`) | Must handle error and retry | Stable | Low |
| Block caller (call with no timeout) | Caller hangs until slot opens | Throttled | Low |
| Drop oldest (evict from queue tail) | Silent data loss | Stable | Medium |
| Priority queue (two queues) | Priority-aware rejection | Stable | Medium |
| Demand-driven (GenStage/Broadway) | Explicit demand signalling | Optimal | High |

Reflection question: the drain loop in `handle_continue` blocks the GenServer
for `@write_delay_ms` on every item. During that time, `stats/0` calls from a
monitoring dashboard will queue up in the mailbox. How would you redesign this to
keep the GenServer responsive during heavy drain? (Hint: think Task.)

---

## Common production mistakes

**1. Calling `:queue.len/1` in a hot-path callback**
`:queue.len/1` traverses both internal lists — it is O(n). Calling it in `handle_call`
on every submission creates a feedback loop: as the queue grows, each submission takes
longer, causing callers to pile up, causing the mailbox to grow. Always maintain a
separate `depth` integer in state and update it manually.

**2. Starting a drain loop without a processing guard**
If you trigger `{:continue, :drain}` from every `handle_call`, and two separate code
paths both trigger drain, you can end up with two drain chains running concurrently
in the same process. GenServer is sequential so they interleave — causing items to
be processed out of FIFO order and the drain to never terminate cleanly. Use a
`:processing` boolean as a single-entry gate.

**3. Blocking the GenServer with long processing steps**
`Process.sleep(600)` inside `handle_continue` blocks the GenServer for 600 ms. During
that time no `handle_call` can be served, including monitoring queries. For production
systems, spawn a Task for the actual write and let `handle_info` collect the result.
Keep `handle_continue` lightweight.

**4. Using `GenServer.cast` for bounded queue submissions**
With `cast`, callers get no acknowledgment and no rejection signal when the queue is
full. Items are silently dropped or queue grows silently. Always use `GenServer.call`
for submissions to bounded queues — callers need the `{:error, :overloaded}` signal
to implement their own back-pressure or retry logic.

---

## Resources

- [Erlang docs — `:queue` module](https://www.erlang.org/doc/man/queue.html)
- [HexDocs — GenServer `handle_continue/2`](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
- [Concurrent Data Processing in Elixir — Saša Jurić](https://pragprog.com/titles/sgdpelixir/)
- [GenStage — demand-driven back-pressure](https://hexdocs.pm/gen_stage)
- [Broadway — data ingestion with back-pressure](https://hexdocs.pm/broadway)
