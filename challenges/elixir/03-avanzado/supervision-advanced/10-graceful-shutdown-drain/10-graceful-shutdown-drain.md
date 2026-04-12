# Graceful Shutdown & Drain

## Goal

Implement graceful shutdown for three API gateway components: a Router that drains in-flight HTTP requests, an AuditWriter that drains its internal queue, and a ConnectionPool that waits for checked-out connections to be returned. Each component uses `Process.flag(:trap_exit, true)` to intercept the supervisor's shutdown signal and convert it into a controlled drain sequence.

---

## The OTP shutdown lifecycle

```
SIGTERM received by the VM
  -> Application.prep_stop/1
  -> Supervisor.stop (each supervisor, in reverse start order)
  -> Each child receives its shutdown signal:
     - Worker with trap_exit -> {:EXIT, sup_pid, :shutdown} in handle_info
     - Worker without trap_exit -> terminate/2 called directly
  -> Worker has :shutdown ms to finish -- then :brutal_kill
  -> Application.stop/1
```

The `:shutdown` field in `child_spec` is the budget each process gets for cleanup. Default is 5,000 ms.

---

## The drain pattern

The challenge: `terminate/2` runs inside the GenServer process. If you block there waiting for work to finish, the GenServer cannot process the `handle_info` messages that signal work completion -- deadlock.

Solution: intercept the shutdown signal in `handle_info`, stop accepting new work, and let the normal message loop drive the drain:

```
trap_exit: true in init/1
  -> Supervisor sends :shutdown
  -> {:EXIT, sup_pid, :shutdown} arrives in handle_info
  -> Stop accepting new work, set state.draining = true
  -> Normal message loop processes remaining work
  -> When drained: {:stop, :shutdown, state}
  -> terminate/2 runs (logs final state)
```

---

## Full implementation

### `lib/api_gateway/router.ex` -- drain in-flight requests

```elixir
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

    if state.draining_ref && MapSet.size(new_active) == 0 do
      {:stop, :shutdown, new_state}
    else
      {:noreply, new_state}
    end
  end

  @impl true
  def handle_info({:EXIT, _from, reason}, state) do
    Logger.info("Router draining #{MapSet.size(state.active)} active requests")
    new_state = %{state | accepting: false}

    if MapSet.size(state.active) == 0 do
      {:stop, reason, new_state}
    else
      Process.send_after(self(), {:drain_timeout, reason}, @drain_timeout_ms)
      {:noreply, %{new_state | draining_ref: reason}}
    end
  end

  @impl true
  def handle_info({:drain_timeout, reason}, state) do
    Logger.warning("Router drain timeout -- #{MapSet.size(state.active)} requests abandoned")
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

### `lib/api_gateway/cache/connection_pool.ex` -- wait for checked-out connections

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

### Tests

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

    assert Process.alive?(pid)

    ConnectionPool.checkin(ref1)
    ConnectionPool.checkin(ref2)
    Process.sleep(100)

    refute Process.alive?(pid)
  end
end
```

---

## How it works

1. **`trap_exit` in `init/1`**: converts the supervisor's shutdown signal from an immediate kill into a `{:EXIT, ...}` message in `handle_info`.

2. **Stop accepting, keep processing**: on receiving the shutdown signal, set `accepting: false` and schedule a drain timeout. The normal message loop continues processing remaining work.

3. **Drain completion**: when all active work finishes (requests drained, connections returned, queue empty), return `{:stop, :shutdown, state}`.

4. **Custom `:shutdown` in child_spec**: gives the supervisor enough time to wait for the drain before force-killing.

---

## Common production mistakes

**1. `:brutal_kill` for all workers**
Terminates processes immediately without cleanup. Requests are dropped, connections leaked.

**2. Not setting `trap_exit`**
Without it, the supervisor's shutdown signal kills the process directly. The drain logic in `handle_info` never runs.

**3. Shutdown timeout smaller than maximum work duration**
If a request can take 30 seconds and `:shutdown` is 5 seconds, the supervisor kills mid-request.

**4. Doing expensive work in `terminate/2`**
`terminate/2` should be fast. Do not call other processes there -- they may already be shutting down.

---

## Resources

- [HexDocs -- GenServer.terminate/2](https://hexdocs.pm/elixir/GenServer.html#c:terminate/2)
- [Erlang OTP -- Shutdown](https://www.erlang.org/doc/design_principles/sup_princ.html#shutdown)
- [Kubernetes Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
