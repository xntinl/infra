# Graceful Shutdown & Drain

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The ops team just completed your first zero-downtime
deploy using a Kubernetes rolling update. During the rollout they observed:

- 37 requests returned HTTP 502 (connection reset by the pod being terminated)
- 12 audit log entries were lost (the `AuditWriter` was killed mid-drain)
- 3 database connections were leaked (the `DBPool` never ran its close logic)

All three problems have the same root cause: the processes were killed (`SIGKILL`-equivalent)
before they could finish in-flight work. This exercise implements proper graceful shutdown
across the gateway's three critical components.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex                          # ← add drain logic
│       └── middleware/
│           └── audit_writer.ex                # ← add drain logic
│       └── cache/
│           └── connection_pool.ex             # ← you implement (new)
├── test/
│   └── api_gateway/
│       └── shutdown/
│           ├── router_drain_test.exs          # given tests — must pass
│           ├── audit_writer_drain_test.exs    # given tests — must pass
│           └── pool_drain_test.exs            # given tests — must pass
└── mix.exs
```

---

## The OTP shutdown lifecycle

```
SIGTERM received by the VM
  ↓
Application.prep_stop/1  ← last chance to signal "going down"
  ↓
Supervisor.stop (each supervisor, in reverse start order)
  ↓
Each child receives its shutdown signal:
  - Supervisor child → propagates to its children
  - Worker (GenServer with trap_exit) → receives {:EXIT, sup_pid, :shutdown}
  - Worker (no trap_exit) → terminate/2 is called directly by the supervisor
  ↓
Worker has :shutdown ms to finish — then :brutal_kill
  ↓
Application.stop/1
```

The `:shutdown` field in `child_spec` is the budget you give each process for cleanup.
Default is 5,000 ms. If your drain can take 30 seconds, set `:shutdown` to 35,000 ms
(5 s buffer over the drain timeout).

---

## When `terminate/2` runs

`terminate/2` is **not** a guaranteed hook. It only runs when:

1. The supervisor sends a shutdown signal AND `shutdown: N` (not `:brutal_kill`)
2. The process has `Process.flag(:trap_exit, true)` set in `init/1`
   (ensures the signal arrives as a message rather than killing the process outright)

For GenServers managed by a supervisor with a numeric `:shutdown` value, OTP calls
`terminate/2` before killing the process — but only if the process is not already
dead. Set `trap_exit: true` in any process that must run cleanup on shutdown.

---

## The drain pattern

The challenge: `terminate/2` runs inside the GenServer process. If you block with
`receive` waiting for work to finish, the GenServer cannot process the `handle_info`
messages that signal work completion — deadlock.

Solution: intercept the shutdown signal **before** `terminate/2`, convert it to a
message in `handle_info`, and let the normal message loop drive the drain:

```
trap_exit: true in init/1
  ↓
Supervisor sends :shutdown
  ↓
{:EXIT, sup_pid, :shutdown} arrives in handle_info (not terminate)
  ↓
handle_info: stop accepting new work, set state.draining = true
  ↓
Normal message loop processes remaining work
  ↓
When work is drained: {:stop, :shutdown, state}
  ↓
terminate/2 runs (logs final state, nothing else needed)
```

---

## Implementation

### Step 1: Add drain logic to `Router`

The Router uses `Process.flag(:trap_exit, true)` so the supervisor's shutdown signal
arrives as a `{:EXIT, ...}` message in `handle_info` rather than killing the process
outright. This gives the Router control over the shutdown sequence:

1. Stop accepting new requests (return `{:error, :draining}`)
2. Wait for in-flight requests to complete (tracked via MapSet)
3. Schedule a drain timeout (if requests don't complete in time, stop anyway)
4. When all active requests finish (or timeout fires), stop the process

The `child_spec` sets `:shutdown` to 35 seconds (30s drain + 5s buffer) so the
supervisor gives the Router enough time to finish in-flight work before force-killing.

```elixir
# In lib/api_gateway/router.ex

defmodule ApiGateway.Router do
  use GenServer
  require Logger

  @drain_timeout_ms 30_000

  def child_spec(opts) do
    %{
      id:       __MODULE__,
      start:    {__MODULE__, :start_link, [opts]},
      restart:  :permanent,
      shutdown: @drain_timeout_ms + 5_000,
      type:     :worker
    }
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns :accepting or :draining."
  @spec status() :: :accepting | :draining
  def status, do: GenServer.call(__MODULE__, :status)

  @doc "Simulates handling a new incoming request."
  @spec handle_request(String.t(), (-> any())) :: {:ok, String.t()} | {:error, :draining}
  def handle_request(request_id, work_fn) do
    GenServer.call(__MODULE__, {:new_request, request_id, work_fn})
  end

  @impl true
  def init(_opts) do
    # trap_exit ensures the supervisor's shutdown signal arrives as a message
    # in handle_info rather than killing the process outright.
    Process.flag(:trap_exit, true)
    {:ok, %{accepting: true, active: MapSet.new(), draining_ref: nil}}
  end

  @impl true
  def handle_call(:status, _from, state) do
    status = if state.accepting, do: :accepting, else: :draining
    {:reply, status, state}
  end

  @impl true
  def handle_call({:new_request, _id, _fn}, _from, %{accepting: false} = state) do
    {:reply, {:error, :draining}, state}
  end

  @impl true
  def handle_call({:new_request, request_id, work_fn}, _from, state) do
    # Launch work in a Task. When done, the task sends {:request_done, id} back.
    server = self()
    Task.start(fn ->
      work_fn.()
      send(server, {:request_done, request_id})
    end)

    new_state = %{state | active: MapSet.put(state.active, request_id)}
    {:reply, {:ok, request_id}, new_state}
  end

  @impl true
  def handle_info({:request_done, request_id}, state) do
    new_active = MapSet.delete(state.active, request_id)
    new_state = %{state | active: new_active}

    # If we are draining and all active requests have finished, stop the process.
    if state.draining_ref && MapSet.size(new_active) == 0 do
      {:stop, :shutdown, new_state}
    else
      {:noreply, new_state}
    end
  end

  @impl true
  def handle_info({:EXIT, _from, reason}, state) do
    # Supervisor sent shutdown signal. Stop accepting new requests.
    Logger.info("Router draining #{MapSet.size(state.active)} active requests")
    new_state = %{state | accepting: false}

    if MapSet.size(state.active) == 0 do
      # No active requests — stop immediately.
      {:stop, reason, new_state}
    else
      # Active requests in flight — schedule a drain timeout.
      Process.send_after(self(), {:drain_timeout, reason}, @drain_timeout_ms)
      {:noreply, %{new_state | draining_ref: reason}}
    end
  end

  @impl true
  def handle_info({:drain_timeout, reason}, state) do
    Logger.warning("Router drain timeout — #{MapSet.size(state.active)} requests abandoned")
    {:stop, reason, state}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("Router terminated. Reason: #{inspect(reason)}. " <>
                "Remaining active: #{MapSet.size(state.active)}")
    :ok
  end
end
```

### Step 2: Add drain logic to `AuditWriter`

The AuditWriter extends the back-pressure queue (exercise 04) with graceful shutdown.
When the shutdown signal arrives, it stops accepting new entries and continues draining
the internal queue. When the queue is empty (or a timeout fires), it stops.

```elixir
# In lib/api_gateway/middleware/audit_writer.ex — extend existing module

@impl true
def init(_opts) do
  Process.flag(:trap_exit, true)
  state = %{
    queue:      :queue.new(),
    depth:      0,
    processing: false,
    accepting:  true,
    draining:   false,
    stats:      %{queued: 0, written: 0, rejected: 0}
  }
  {:ok, state}
end

@impl true
def handle_info({:EXIT, _from, reason}, state) do
  Logger.info("AuditWriter draining #{state.depth} pending entries before shutdown")
  new_state = %{state | accepting: false, draining: true}

  if state.depth == 0 do
    {:stop, reason, new_state}
  else
    # Schedule drain timeout
    Process.send_after(self(), {:drain_timeout, reason}, 60_000)
    # Ensure drain loop is running
    if not state.processing do
      {:noreply, %{new_state | processing: true}, {:continue, :drain}}
    else
      {:noreply, new_state}
    end
  end
end

@impl true
def handle_info({:drain_timeout, reason}, state) do
  Logger.warning("AuditWriter drain timeout — #{state.depth} entries lost")
  {:stop, reason, state}
end

@impl true
def handle_continue(:drain, state) do
  case :queue.out(state.queue) do
    {:empty, _} ->
      if state.draining do
        # Queue is empty and we are shutting down — stop the process.
        {:stop, :shutdown, %{state | processing: false}}
      else
        {:noreply, %{state | processing: false}}
      end

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
```

### Step 3: `lib/api_gateway/cache/connection_pool.ex`

The ConnectionPool demonstrates graceful shutdown for resource-managing processes.
On shutdown, it immediately closes all available (not checked-out) connections and
waits for checked-out connections to be returned. If connections are not returned
within the timeout, it force-closes them.

```elixir
defmodule ApiGateway.Cache.ConnectionPool do
  use GenServer
  require Logger

  @checkin_timeout_ms 10_000

  def child_spec(opts) do
    %{
      id:       __MODULE__,
      start:    {__MODULE__, :start_link, [opts]},
      restart:  :permanent,
      shutdown: @checkin_timeout_ms + 5_000,
      type:     :worker
    }
  end

  def start_link(opts \\ []) do
    pool_size = Keyword.get(opts, :pool_size, 5)
    GenServer.start_link(__MODULE__, pool_size, name: __MODULE__)
  end

  @doc "Checks out a connection. Returns {:ok, ref, conn} or {:error, reason}."
  @spec checkout() :: {:ok, reference(), map()} | {:error, atom()}
  def checkout, do: GenServer.call(__MODULE__, :checkout, 5_000)

  @doc "Returns a connection to the pool."
  @spec checkin(reference()) :: :ok
  def checkin(ref), do: GenServer.cast(__MODULE__, {:checkin, ref})

  @impl true
  def init(pool_size) do
    Process.flag(:trap_exit, true)
    connections = Enum.map(1..pool_size, &open_conn/1)
    {:ok, %{available: connections, checked_out: %{}, accepting: true, shutdown: false}}
  end

  @impl true
  def handle_call(:checkout, _from, %{accepting: false} = state) do
    {:reply, {:error, :pool_shutting_down}, state}
  end

  @impl true
  def handle_call(:checkout, _from, state) do
    case state.available do
      [] ->
        {:reply, {:error, :pool_empty}, state}

      [conn | rest] ->
        ref = make_ref()
        {:reply, {:ok, ref, conn},
         %{state | available: rest, checked_out: Map.put(state.checked_out, ref, conn)}}
    end
  end

  @impl true
  def handle_cast({:checkin, ref}, state) do
    case Map.pop(state.checked_out, ref) do
      {nil, _} ->
        Logger.warning("Unknown checkin ref: #{inspect(ref)}")
        {:noreply, state}

      {conn, remaining} ->
        new_state = %{state |
          available:   [conn | state.available],
          checked_out: remaining
        }

        # If in shutdown mode and all connections have been returned, stop.
        if state.shutdown && map_size(remaining) == 0 do
          {:stop, :shutdown, new_state}
        else
          {:noreply, new_state}
        end
    end
  end

  @impl true
  def handle_info({:EXIT, _from, reason}, state) do
    # Close all available connections immediately — they are not in use.
    Enum.each(state.available, &close_conn/1)
    new_state = %{state | available: [], accepting: false, shutdown: true}

    if map_size(state.checked_out) == 0 do
      # No checked-out connections — stop now.
      {:stop, reason, new_state}
    else
      Logger.info("Pool waiting for #{map_size(state.checked_out)} connections to be returned")
      Process.send_after(self(), :force_close, @checkin_timeout_ms)
      {:noreply, new_state}
    end
  end

  @impl true
  def handle_info(:force_close, state) do
    Logger.warning("Pool force-closing #{map_size(state.checked_out)} unreturned connections")
    Enum.each(state.checked_out, fn {_, conn} -> close_conn(conn) end)
    {:stop, :shutdown, %{state | checked_out: %{}}}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("ConnectionPool terminated. Reason: #{inspect(reason)}. " <>
                "Leaked connections: #{map_size(state.checked_out)}")
    :ok
  end

  defp open_conn(n),  do: %{id: n, status: :connected}
  defp close_conn(c), do: Logger.info("Closing connection #{c.id}")
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/shutdown/router_drain_test.exs
defmodule ApiGateway.Shutdown.RouterDrainTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Router

  setup do
    {:ok, _pid} = Router.start_link([])
    on_exit(fn ->
      if pid = Process.whereis(Router), do: GenServer.stop(pid, :normal, 5_000)
    end)
    :ok
  end

  test "rejects new requests during drain" do
    pid = Process.whereis(Router)
    # Trigger shutdown signal
    send(pid, {:EXIT, pid, :shutdown})
    Process.sleep(20)
    assert {:error, :draining} = Router.handle_request("late-req", fn -> :ok end)
  end

  test "active requests complete before shutdown" do
    done = self()
    Router.handle_request("r1", fn ->
      Process.sleep(200)
      send(done, :work_done)
    end)

    pid = Process.whereis(Router)
    send(pid, {:EXIT, pid, :shutdown})

    assert_receive :work_done, 1_000
  end

  test "status transitions from accepting to draining" do
    assert Router.status() == :accepting
    pid = Process.whereis(Router)
    send(pid, {:EXIT, pid, :shutdown})
    Process.sleep(20)
    assert Router.status() == :draining
  end
end
```

```elixir
# test/api_gateway/shutdown/pool_drain_test.exs
defmodule ApiGateway.Shutdown.PoolDrainTest do
  use ExUnit.Case, async: false

  alias ApiGateway.Cache.ConnectionPool

  setup do
    {:ok, _pid} = ConnectionPool.start_link(pool_size: 3)
    on_exit(fn ->
      if pid = Process.whereis(ConnectionPool), do: GenServer.stop(pid, :normal, 15_000)
    end)
    :ok
  end

  test "rejects checkout after shutdown signal" do
    pid = Process.whereis(ConnectionPool)
    send(pid, {:EXIT, pid, :shutdown})
    Process.sleep(20)
    assert {:error, :pool_shutting_down} = ConnectionPool.checkout()
  end

  test "checked-out connections are waited for before shutdown" do
    {:ok, ref1, _} = ConnectionPool.checkout()
    {:ok, ref2, _} = ConnectionPool.checkout()

    pid = Process.whereis(ConnectionPool)
    send(pid, {:EXIT, pid, :shutdown})
    Process.sleep(50)

    # Pool should still be alive waiting for checkins
    assert Process.alive?(pid)

    ConnectionPool.checkin(ref1)
    ConnectionPool.checkin(ref2)
    Process.sleep(100)

    # After all checked-in, pool should shut down
    refute Process.alive?(pid)
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/shutdown/ --trace
```

---

## Trade-off analysis

| Component | Shutdown strategy | Max wait | What is lost if killed early |
|-----------|-------------------|----------|------------------------------|
| Router | Drain active requests | 30 s | In-flight HTTP responses (502 to clients) |
| AuditWriter | Drain internal queue | 60 s | Unwritten audit log entries |
| ConnectionPool | Wait for checkins | 10 s | Open DB connections (pool exhaustion on restart) |

Reflection question: the drain pattern intercepts `{:EXIT, ...}` in `handle_info`.
This requires `Process.flag(:trap_exit, true)`. What other exit signals does
`trap_exit` intercept that you must handle — or risk silently swallowing?
(Hint: `{:EXIT, pid, :normal}` and `{:EXIT, pid, :shutdown}`.)

---

## Common production mistakes

**1. `:brutal_kill` for all workers**
`shutdown: :brutal_kill` terminates the process immediately without calling `terminate/2`.
Requests are dropped, connections are leaked, queues are lost. Use `:brutal_kill` only
for stateless CPU-bound workers that have nothing to clean up.

**2. Not setting `Process.flag(:trap_exit, true)`**
Without `trap_exit`, the supervisor's shutdown signal terminates the process directly,
bypassing the message loop. The `{:EXIT, ...}` drain logic in `handle_info` never runs.
`terminate/2` may still be called, but it runs synchronously and cannot process more
messages — you cannot drain a queue from inside `terminate/2`.

**3. Shutdown timeout smaller than maximum work duration**
If an HTTP request can take 30 seconds and your `:shutdown` is 5 seconds, the supervisor
kills the process after 5 seconds — mid-request. Set `:shutdown` to at least
`max_request_duration + buffer`.

**4. Doing expensive work in `terminate/2`**
`terminate/2` should be fast and self-contained. Do not call other processes in
`terminate/2` — those processes may already be shutting down (the order of sibling
termination depends on the supervisor strategy). Use `handle_info({:EXIT, ...})` for
any work that requires coordination with other processes.

---

## Resources

- [HexDocs — GenServer.terminate/2](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2)
- [HexDocs — Application behaviour callbacks](https://hexdocs.pm/elixir/Application.html)
- [Erlang OTP — Shutdown](https://www.erlang.org/doc/design_principles/sup_princ.html#shutdown)
- [Kubernetes Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
- [Mix.Release — HexDocs](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
