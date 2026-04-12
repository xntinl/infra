# GenServer handle_continue & Lazy Recovery

## Goal

Build two GenServer components for an API gateway: a `RouteTable.Server` that defers expensive initialization using `handle_continue/2` so the supervisor is never blocked, and a `RateLimiter.Server` that recovers window state from a surviving ETS table after a crash using the same `handle_continue` mechanism.

---

## Why `handle_continue` exists

Before `handle_continue/2` (added in OTP 21), developers faced a dilemma: expensive initialization work had to happen either in `init/1` (blocking the supervisor) or via a `send(self(), :init)` trick that opened a race window where external messages could be processed before the process was ready.

`handle_continue/2` closes that race. It runs after the current callback returns and before any new message from the mailbox is processed. The supervisor sees the process as started immediately; no external message can jump the queue.

```
Supervisor calls start_link
  -> init/1 returns {:ok, state, {:continue, :load_routes}}
       -> Supervisor marks process started    <- fast
            -> handle_continue(:load_routes, state) runs immediately
                 -> state is now fully populated
                      -> first external message handled HERE
```

---

## Why ETS as crash-safe mirror works

When a GenServer crashes, its supervisor restarts it with a fresh `init/1` -- all in-memory state is gone. By mirroring every write to a named ETS table that survives the process crash, `handle_continue` can restore full state post-crash without any database call.

---

## Full implementation

### `lib/api_gateway/route_table/loader.ex` -- simulates slow config load

```elixir
defmodule ApiGateway.RouteTable.Loader do
  @doc """
  Simulates loading routes from a remote config service (~2 seconds).
  """
  @spec load(atom()) :: {:ok, map()} | {:error, term()}
  def load(traffic_class) do
    Process.sleep(2_000)
    routes = %{
      "/api/payments"  => "http://payments-svc:8080",
      "/api/orders"    => "http://orders-svc:8080",
      "/api/inventory" => "http://inventory-svc:8080",
      "/health"        => "http://healthcheck-svc:8080"
    }
    {:ok, Map.put(routes, "/class/#{traffic_class}", "http://#{traffic_class}-svc:8080")}
  end
end
```

### `lib/api_gateway/route_table/server.ex`

The RouteTable server demonstrates `handle_continue` for deferred initialization. `init/1` returns immediately with an empty route map and a `{:continue, :load_routes}` instruction. The supervisor sees the process as started and moves on. Meanwhile, `handle_continue` runs the expensive 2-second load before any external message is processed.

Callers that arrive during the loading window receive `{:error, :not_ready}` -- a clear, non-crashing signal that they should retry.

```elixir
defmodule ApiGateway.RouteTable.Server do
  use GenServer
  require Logger

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Looks up the upstream URL for a given path prefix.
  Returns {:ok, upstream} or {:error, :not_ready | :not_found}.
  """
  @spec lookup(String.t()) :: {:ok, String.t()} | {:error, :not_ready | :not_found}
  def lookup(path) do
    GenServer.call(__MODULE__, {:lookup, path})
  end

  @doc "Returns true once the route table has loaded its rules."
  @spec ready?() :: boolean()
  def ready? do
    GenServer.call(__MODULE__, :ready?)
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(opts) do
    Logger.info("RouteTable starting -- loading deferred")
    traffic_class = Keyword.get(opts, :traffic_class, :default)

    initial_state = %{
      routes: %{},
      ready: false,
      traffic_class: traffic_class
    }

    # Return immediately so the supervisor is not blocked. The {:continue, :load_routes}
    # tuple tells GenServer to call handle_continue(:load_routes, state) right after
    # init/1 returns -- before any message from the mailbox is processed.
    {:ok, initial_state, {:continue, :load_routes}}
  end

  @impl true
  def handle_continue(:load_routes, state) do
    case ApiGateway.RouteTable.Loader.load(state.traffic_class) do
      {:ok, routes} ->
        Logger.info("RouteTable loaded #{map_size(routes)} routes for #{state.traffic_class}")
        {:noreply, %{state | routes: routes, ready: true}}

      {:error, reason} ->
        Logger.error("Failed to load routes: #{inspect(reason)}, retrying...")
        {:noreply, state, {:continue, :load_routes}}
    end
  end

  @impl true
  def handle_call({:lookup, _path}, _from, %{ready: false} = state) do
    {:reply, {:error, :not_ready}, state}
  end

  @impl true
  def handle_call({:lookup, path}, _from, %{ready: true} = state) do
    result =
      state.routes
      |> Map.keys()
      |> Enum.sort_by(&byte_size/1, :desc)
      |> Enum.find_value(fn prefix ->
        if String.starts_with?(path, prefix), do: {:ok, Map.fetch!(state.routes, prefix)}
      end)

    {:reply, result || {:error, :not_found}, state}
  end

  @impl true
  def handle_call(:ready?, _from, state) do
    {:reply, state.ready, state}
  end
end
```

### `lib/api_gateway/rate_limiter/server.ex` -- with crash recovery

The RateLimiter.Server uses a named ETS table to mirror rate limit window data. When the process crashes and restarts, `init/1` detects that the ETS table already exists and defers recovery to `handle_continue(:recover, state)` instead of starting from scratch.

```elixir
defmodule ApiGateway.RateLimiter.Server do
  use GenServer
  require Logger

  @table :rate_limiter_windows

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc "Records a request for the given client."
  @spec record(String.t()) :: :ok
  def record(client_id) do
    GenServer.cast(__MODULE__, {:record, client_id})
  end

  @doc """
  Checks whether client_id is within its rate limit.
  Returns {:allow, remaining} or {:deny, retry_after_ms}.
  """
  @spec check(String.t(), pos_integer(), pos_integer()) ::
          {:allow, non_neg_integer()} | {:deny, pos_integer()}
  def check(client_id, limit, window_ms) do
    GenServer.call(__MODULE__, {:check, client_id, limit, window_ms})
  end

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # ---------------------------------------------------------------------------
  # GenServer lifecycle
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # Check if the ETS table survived a crash. :ets.info/1 returns :undefined
    # if the table does not exist.
    case :ets.info(@table) do
      :undefined ->
        :ets.new(@table, [:named_table, :public, :bag])
        {:ok, %{}}

      _info ->
        # Table already exists -- we crashed and restarted.
        {:ok, %{}, {:continue, :recover}}
    end
  end

  @impl true
  def handle_continue(:recover, state) do
    count = :ets.info(@table, :size)
    Logger.info("RateLimiter recovered #{count} window entries from ETS")
    {:noreply, state}
  end

  @impl true
  def handle_cast({:record, client_id}, state) do
    ts = System.monotonic_time(:millisecond)
    :ets.insert(@table, {client_id, ts})
    {:noreply, state}
  end

  @impl true
  def handle_call({:check, client_id, limit, window_ms}, _from, state) do
    now = System.monotonic_time(:millisecond)
    cutoff = now - window_ms

    count =
      :ets.lookup(@table, client_id)
      |> Enum.count(fn {_id, ts} -> ts > cutoff end)

    result =
      if count >= limit do
        oldest_in_window =
          :ets.lookup(@table, client_id)
          |> Enum.filter(fn {_id, ts} -> ts > cutoff end)
          |> Enum.min_by(fn {_id, ts} -> ts end, fn -> {nil, now} end)
          |> elem(1)

        retry_after = window_ms - (now - oldest_in_window)
        {:deny, max(retry_after, 1)}
      else
        remaining = limit - count
        {:allow, remaining}
      end

    {:reply, result, state}
  end
end
```

### Tests

```elixir
# test/api_gateway/route_table/server_test.exs
defmodule ApiGateway.RouteTable.ServerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RouteTable.Server

  setup do
    start_supervised!({Server, [traffic_class: :test]})
    :ok
  end

  describe "init/1 is non-blocking" do
    test "start_link returns before routes are loaded" do
      assert true
    end

    test "returns not_ready before load completes" do
      case Server.lookup("/api/payments") do
        {:ok, _} -> :ok
        {:error, :not_ready} -> :ok
        other -> flunk("unexpected: #{inspect(other)}")
      end
    end
  end

  describe "after loading completes" do
    setup do
      wait_until_ready(Server, 5_000)
      :ok
    end

    test "ready? returns true" do
      assert Server.ready?() == true
    end

    test "known routes resolve to upstream URLs" do
      assert {:ok, upstream} = Server.lookup("/api/payments")
      assert String.starts_with?(upstream, "http://")
    end

    test "unknown route returns not_found" do
      assert {:error, :not_found} = Server.lookup("/unknown/path")
    end
  end

  defp wait_until_ready(server, timeout_ms) do
    deadline = System.monotonic_time(:millisecond) + timeout_ms
    Stream.repeatedly(fn -> server.ready?() end)
    |> Stream.take_while(fn ready ->
      not ready and System.monotonic_time(:millisecond) < deadline
    end)
    |> Enum.each(fn _ -> Process.sleep(50) end)
  end
end
```

```elixir
# test/api_gateway/rate_limiter/recovery_test.exs
defmodule ApiGateway.RateLimiter.RecoveryTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RateLimiter.Server

  setup do
    case :ets.info(:rate_limiter_windows) do
      :undefined -> :ok
      _ -> :ets.delete_all_objects(:rate_limiter_windows)
    end
    :ok
  end

  test "recovers window entries after crash" do
    {:ok, pid} = Server.start_link([])

    for _ <- 1..5, do: Server.record("client_crash_test")
    Process.sleep(20)

    assert {:allow, 5} = Server.check("client_crash_test", 10, 60_000)

    Process.exit(pid, :kill)
    Process.sleep(50)

    {:ok, _new_pid} = Server.start_link([])
    Process.sleep(20)

    assert {:allow, remaining} = Server.check("client_crash_test", 10, 60_000)
    assert remaining == 5
  end
end
```

---

## How it works

1. **Deferred init**: `init/1` returns `{:ok, state, {:continue, :load_routes}}`. The supervisor marks the process as started immediately. `handle_continue(:load_routes, state)` runs in the GenServer process before any mailbox message is processed.

2. **Race-free**: unlike the pre-OTP-21 `send(self(), :init)` trick, `handle_continue` cannot be preempted by an external message. The ordering guarantee is enforced by the GenServer implementation itself.

3. **ETS crash recovery**: named ETS tables survive process crashes (the table ownership transfers). On restart, `init/1` checks `(:ets.info(@table) != :undefined)` and defers recovery to `handle_continue(:recover, state)`.

4. **Graceful degradation**: callers that arrive during loading receive `{:error, :not_ready}` instead of crashing or blocking.

---

## Common production mistakes

**1. Blocking `init/1` with expensive work**
A 2-second DB call in `init/1` blocks the supervisor. With 8 workers starting sequentially, total startup is 16 seconds. Always delegate slow initialization to `handle_continue`.

**2. Using `send(self(), :init)` instead of `handle_continue`**
This pre-OTP-21 pattern has a real race condition: external messages can arrive before the `:init` message is processed.

**3. Assuming `handle_continue` makes the GenServer non-blocking**
`handle_continue` runs in the GenServer's own process. While it executes, no other messages are processed. Callers must handle `{:error, :not_ready}` gracefully.

**4. Long `handle_continue` chains without per-step error handling**
A chain that crashes partway leaves the process in a half-initialized state. Each step should be idempotent or include a rollback.

---

## Resources

- [OTP 21 release notes -- `handle_continue/2`](https://www.erlang.org/blog/my-otp-21-highlights/)
- [HexDocs -- GenServer.handle_continue/2](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
- [Erlang/OTP docs -- `gen_server`](https://www.erlang.org/doc/man/gen_server.html)
