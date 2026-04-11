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
      # TODO: set shutdown to @drain_timeout_ms + 5_000
      # This gives the drain logic time to finish before the supervisor force-kills
      shutdown: :timer.seconds(35),
      type:     :worker
    }
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc "Returns :accepting or :draining."
  def status, do: GenServer.call(__MODULE__, :status)

  @doc "Simulates handling a new incoming request."
  def handle_request(request_id, work_fn) do
    GenServer.call(__MODULE__, {:new_request, request_id, work_fn})
  end

  @impl true
  def init(_opts) do
    # TODO: set trap_exit so shutdown arrives as {:EXIT, ...} message in handle_info
    # HINT: Process.flag(:trap_exit, true)
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
    # TODO: launch work_fn in a Task (fire-and-forget)
    # When the task finishes, it should send {:request_done, request_id} to self()
    # Add request_id to state.active
    # Reply {:ok, request_id}
    server = self()
    Task.start(fn ->
      work_fn.()
      send(server, {:request_done, request_id})
    end)
    {:reply, {:ok, request_id},
     %{state | active: MapSet.put(state.active, request_id)}}
  end

  @impl true
  def handle_info({:request_done, request_id}, state) do
    new_active = MapSet.delete(state.active, request_id)
    new_state  = %{state | active: new_active}

    # TODO: if draining and active is now empty, stop the process
    # HINT:
    #   if state.draining_ref && MapSet.size(new_active) == 0 do
    #     {:stop, :shutdown, new_state}
    #   else
    #     {:noreply, new_state}
    #   end
    {:noreply, new_state}
  end

  @impl true
  def handle_info({:EXIT, _from, reason}, state) do
    # TODO: stop accepting new requests
    # If active is empty: stop immediately
    # If active is non-empty: set draining mode, schedule a drain timeout
    Logger.info("Router draining #{MapSet.size(state.active)} active requests")
    new_state = %{state | accepting: false}

    if MapSet.size(state.active) == 0 do
      {:stop, reason, new_state}
    else
      # TODO: schedule drain timeout — if drain doesn't complete in time, force stop
      # HINT: Process.send_after(self(), {:drain_timeout, reason}, @drain_timeout_ms)
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

```elixir
# In lib/api_gateway/middleware/audit_writer.ex — extend existing module

@impl true
def init(_opts) do
  # TODO: set trap_exit
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
      send(self(), {:continue, :drain_internal})
    end
    {:noreply, new_state}
  end
end

@impl true
def handle_info({:drain_timeout, reason}, state) do
  Logger.warning("AuditWriter drain timeout — #{state.depth} entries lost")
  {:stop, reason, state}
end

@impl true
def handle_continue(:drain, state) do
  # TODO: same drain logic as before, but when queue empties AND draining == true,
  # call {:stop, :shutdown, state} instead of just setting processing: false
  case :queue.out(state.queue) do
    {:empty, _} ->
      if state.draining do
        {:stop, :shutdown, %{state | processing: false}}
      else
        {:noreply, %{state | processing: false}}
      end

    {{:value, {entry, queued_at}}, rest} ->
      # TODO: write entry, update stats, continue drain
      {:noreply, state}
  end
end
```

### Step 3: `lib/api_gateway/cache/connection_pool.ex`

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
  def checkout, do: GenServer.call(__MODULE__, :checkout, 5_000)

  @doc "Returns a connection to the pool."
  def checkin(ref), do: GenServer.cast(__MODULE__, {:checkin, ref})

  @impl true
  def init(pool_size) do
    # TODO: set trap_exit
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
    # TODO: return connection to available pool
    # If shutdown mode and checked_out becomes empty → {:stop, :shutdown, state}
    case Map.pop(state.checked_out, ref) do
      {nil, _} ->
        Logger.warning("Unknown checkin ref: #{inspect(ref)}")
        {:noreply, state}

      {conn, remaining} ->
        new_state = %{state |
          available:   [conn | state.available],
          checked_out: remaining
        }
        if state.shutdown && map_size(remaining) == 0 do
          {:stop, :shutdown, new_state}
        else
          {:noreply, new_state}
        end
    end
  end

  @impl true
  def handle_info({:EXIT, _from, reason}, state) do
    # TODO: close all available connections immediately
    # If checked_out is empty: stop now
    # Else: enter shutdown mode, schedule force-close timeout
    Enum.each(state.available, &close_conn/1)
    new_state = %{state | available: [], accepting: false, shutdown: true}

    if map_size(state.checked_out) == 0 do
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
