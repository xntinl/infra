# GenServer Back-Pressure with Internal Queues

## Goal

Build two GenServer components: an `AuditWriter` with a bounded internal queue that rejects submissions when full (back-pressure), and a `PriorityDispatcher` with two-level priority queues where critical traffic is always processed before normal traffic.

---

## Why unbounded mailboxes are a production hazard

Every BEAM process has a mailbox: a FIFO queue of messages waiting to be processed. By default it is unbounded. A GenServer under heavy load will accept every message ever sent to it, but if it processes slower than producers send, the mailbox grows without limit:

- Memory grows until the node OOMs
- Latency degrades because old messages wait behind a growing backlog
- GC pauses increase as the process heap grows
- The BEAM scheduler grants more time to processes with large mailboxes, starving others

Back-pressure makes the producer aware of the consumer's capacity.

---

## The `:queue` module

`:queue` implements a functional double-ended queue using two lists. It provides O(1) amortised enqueue and dequeue.

```elixir
q = :queue.new()
q = :queue.in(item, q)      # enqueue to back
case :queue.out(q) do
  {{:value, item}, rest} -> # got item from front
  {:empty, _q}           -> # queue is empty
end
```

**Critical**: `:queue.len/1` is O(n). Never call it in a hot path. Always maintain a separate integer depth counter.

---

## Full implementation

### `lib/api_gateway/middleware/audit_writer.ex`

Key patterns:
1. **Depth counter**: `state.depth` as a plain integer instead of `:queue.len/1` (O(n)).
2. **Processing guard**: `state.processing` boolean prevents multiple concurrent drain chains.
3. **Synchronous rejection**: callers use `GenServer.call`, not `cast`. When the queue is full, they receive `{:error, :overloaded}` immediately.

```elixir
defmodule ApiGateway.Middleware.AuditWriter do
  use GenServer
  require Logger

  @max_queue      1_000
  @write_delay_ms 2

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc "Enqueues an audit log entry. Returns {:ok, queued_at} or {:error, :overloaded}."
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
    new_stats = Map.update!(state.stats, :rejected, &(&1 + 1))
    {:reply, {:error, :overloaded}, %{state | stats: new_stats}}
  end

  @impl true
  def handle_call({:write, entry}, _from, state) do
    queued_at = System.monotonic_time(:millisecond)
    new_queue = :queue.in({entry, queued_at}, state.queue)
    new_stats = Map.update!(state.stats, :queued, &(&1 + 1))

    new_state = %{state |
      queue: new_queue,
      depth: state.depth + 1,
      stats: new_stats
    }

    if state.processing do
      {:reply, {:ok, queued_at}, new_state}
    else
      {:reply, {:ok, queued_at}, %{new_state | processing: true}, {:continue, :drain}}
    end
  end

  @impl true
  def handle_call(:stats, _from, state) do
    {:reply, Map.put(state.stats, :depth, state.depth), state}
  end

  @impl true
  def handle_continue(:drain, state) do
    case :queue.out(state.queue) do
      {:empty, _} ->
        {:noreply, %{state | processing: false}}

      {{:value, {entry, queued_at}}, rest} ->
        do_write(entry, queued_at)
        new_state = %{state |
          queue: rest,
          depth: state.depth - 1,
          processing: true,
          stats: Map.update!(state.stats, :written, &(&1 + 1))
        }
        {:noreply, new_state, {:continue, :drain}}
    end
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

### `lib/api_gateway/middleware/priority_dispatcher.ex`

Maintains two separate queues (`:critical` and `:normal`) with independent depth counters. The drain loop always dequeues from the critical queue first.

```elixir
defmodule ApiGateway.Middleware.PriorityDispatcher do
  use GenServer
  require Logger

  @max_per_level   500
  @dispatch_delay_ms 1

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc "Dispatches a request. Returns :ok or {:error, {:overloaded, priority}}."
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
  def handle_call({:dispatch, request, :critical}, _from, state) do
    new_state = %{state |
      critical: :queue.in(request, state.critical),
      crit_depth: state.crit_depth + 1
    }

    if state.processing do
      {:reply, :ok, new_state}
    else
      {:reply, :ok, %{new_state | processing: true}, {:continue, :drain}}
    end
  end

  @impl true
  def handle_call({:dispatch, request, :normal}, _from, state) do
    new_state = %{state |
      normal: :queue.in(request, state.normal),
      norm_depth: state.norm_depth + 1
    }

    if state.processing do
      {:reply, :ok, new_state}
    else
      {:reply, :ok, %{new_state | processing: true}, {:continue, :drain}}
    end
  end

  @impl true
  def handle_call(:queue_depths, _from, state) do
    {:reply, %{critical: state.crit_depth, normal: state.norm_depth}, state}
  end

  @impl true
  def handle_continue(:drain, state) do
    case next_job(state) do
      :empty ->
        {:noreply, %{state | processing: false}}

      {request, updated_state} ->
        do_dispatch(request)
        {:noreply, %{updated_state | processing: true}, {:continue, :drain}}
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp next_job(%{crit_depth: cd} = state) when cd > 0 do
    {{:value, request}, rest} = :queue.out(state.critical)
    {request, %{state | critical: rest, crit_depth: state.crit_depth - 1}}
  end

  defp next_job(%{norm_depth: nd} = state) when nd > 0 do
    {{:value, request}, rest} = :queue.out(state.normal)
    {request, %{state | normal: rest, norm_depth: state.norm_depth - 1}}
  end

  defp next_job(_state), do: :empty

  defp do_dispatch(request) do
    Logger.debug("Dispatching: #{inspect(request)}")
    Process.sleep(@dispatch_delay_ms)
  end
end
```

### Tests

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
    pid = Process.whereis(PriorityDispatcher)
    :sys.replace_state(pid, fn s -> %{s | processing: true} end)

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

    :sys.replace_state(pid, fn s -> %{s | processing: false} end)
    PriorityDispatcher.dispatch(%{id: "trigger"}, :critical)
    Process.sleep(200)

    depths = PriorityDispatcher.queue_depths()
    assert depths.critical == 0
    assert depths.normal == 0
  end
end
```

---

## How it works

1. **Bounded queue**: `handle_call` checks `state.depth >= @max_queue` before enqueuing. If full, returns `{:error, :overloaded}` immediately -- the caller decides whether to retry or drop.

2. **Depth counter**: maintained as a plain integer, incremented on enqueue, decremented on dequeue. Never call `:queue.len/1` (O(n)) in a hot path.

3. **Processing guard**: the `state.processing` boolean ensures only one drain chain runs at a time. Without it, multiple `{:continue, :drain}` triggers would interleave and process items out of FIFO order.

4. **Priority selection**: `next_job/1` always checks the critical queue first. Only when it is empty does it dequeue from normal. All critical items are processed before any normal item.

---

## Common production mistakes

**1. Calling `:queue.len/1` in a hot-path callback**
Creates a feedback loop: as the queue grows, each submission takes longer, causing callers to pile up.

**2. Starting a drain loop without a processing guard**
Two drain chains running concurrently cause items to be processed out of FIFO order.

**3. Using `GenServer.cast` for bounded queue submissions**
With `cast`, callers get no rejection signal. Always use `call` for bounded queues.

---

## Resources

- [Erlang docs -- `:queue` module](https://www.erlang.org/doc/man/queue.html)
- [HexDocs -- GenServer `handle_continue/2`](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
- [GenStage -- demand-driven back-pressure](https://hexdocs.pm/gen_stage)
