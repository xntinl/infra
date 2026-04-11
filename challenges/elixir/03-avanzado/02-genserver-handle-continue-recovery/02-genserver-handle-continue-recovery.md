# GenServer handle_continue & Lazy Recovery

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The router component needs a `RouteTable` GenServer that
loads its routing rules from a config service at startup. Loading takes ~2 seconds.
The gateway has 8 such GenServers (one per traffic class). A blocking `init/1` makes
the supervisor take 16 seconds to start — deployments time out in Kubernetes.

A second problem: the `RateLimiter.Server` (a previous exercise) holds per-client
window data in ETS. When the process crashes and restarts, the ETS table is recreated
from scratch — all window state is lost and clients briefly get a free pass.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       └── rate_limiter/
│           ├── server.ex            # ← extend with crash recovery
│           └── window.ex
│       └── route_table/
│           ├── server.ex            # ← you implement this
│           └── loader.ex            # already exists — simulates slow load
├── test/
│   └── api_gateway/
│       ├── route_table/
│       │   └── server_test.exs      # given tests — must pass without modification
│       └── rate_limiter/
│           └── recovery_test.exs    # given tests — must pass without modification
└── mix.exs
```

---

## Why `handle_continue` exists

Before `handle_continue/2` (added in OTP 21), developers faced a dilemma: expensive
initialization work had to happen either in `init/1` — blocking the supervisor until
done — or via a `send(self(), :init)` trick that opened a race window where external
messages could be processed before the process was ready.

`handle_continue/2` closes that race. It runs **after** the current callback returns
and **before** any new message from the mailbox is processed. The supervisor sees the
process as started immediately; no external message can jump the queue.

```
Supervisor calls start_link
  └─ init/1 returns {:ok, state, {:continue, :load_routes}}
       └─ Supervisor marks process started  ← fast
            └─ handle_continue(:load_routes, state) runs immediately
                 └─ state is now fully populated
                      └─ first external message handled HERE
```

---

## Why ETS as crash-safe mirror works

When a GenServer crashes, its supervisor restarts it with a fresh `init/1` — all
in-memory state is gone. By mirroring every write to a named ETS table with
`{:heir, :none}` (which survives the process but not the node), `handle_continue`
can restore full state post-crash without any DB call.

```
Normal operation:
  handle_cast({:record, client, ts}, state) →
    :ets.insert(@table, {client, ts})     # mirror write
    update in-memory state

Crash + restart:
  init/1 → :ets.info(@table) == info    # table still exists
         → {:ok, empty_state, {:continue, :recover}}

handle_continue(:recover, state) →
  :ets.tab2list(@table)                  # restore from mirror
  → {:noreply, fully_recovered_state}
```

---

## Implementation

### Step 1: `lib/api_gateway/route_table/server.ex`

The RouteTable server demonstrates the `handle_continue` pattern for deferred
initialization. The key insight: `init/1` returns immediately with an empty route
map and a `{:continue, :load_routes}` instruction. The supervisor sees the process
as started and moves on to the next child. Meanwhile, `handle_continue` runs the
expensive 2-second load in the GenServer's own process, before any external message
can be processed.

Callers that arrive during the loading window receive `{:error, :not_ready}` — a
clear, non-crashing signal that they should retry. This is deliberate: the alternative
(blocking in `init/1`) makes the entire supervisor wait, while returning `:not_ready`
lets the system start fast and degrade gracefully.

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

  @doc """
  Returns true once the route table has loaded its rules.
  """
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
    Logger.info("RouteTable starting — loading deferred")
    traffic_class = Keyword.get(opts, :traffic_class, :default)

    initial_state = %{
      routes: %{},
      ready: false,
      traffic_class: traffic_class
    }

    # Return immediately so the supervisor is not blocked. The {:continue, :load_routes}
    # tuple tells GenServer to call handle_continue(:load_routes, state) right after
    # init/1 returns — before any message from the mailbox is processed.
    {:ok, initial_state, {:continue, :load_routes}}
  end

  @impl true
  def handle_continue(:load_routes, state) do
    # This runs in the GenServer process, after init/1 has returned to the supervisor.
    # The supervisor has already moved on to start the next child.
    case ApiGateway.RouteTable.Loader.load(state.traffic_class) do
      {:ok, routes} ->
        Logger.info("RouteTable loaded #{map_size(routes)} routes for #{state.traffic_class}")
        {:noreply, %{state | routes: routes, ready: true}}

      {:error, reason} ->
        # Retry by chaining another handle_continue. This is safe because
        # handle_continue runs before any mailbox message, so the retry
        # does not starve callers — they simply get {:error, :not_ready}.
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
    # Find the longest matching prefix in the route table. Routes are stored as
    # exact path prefixes (e.g., "/api/payments"). We sort by descending length
    # so "/api/payments/refund" matches before "/api/payments" if both existed.
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

### Step 2: `lib/api_gateway/route_table/loader.ex` — already provided

```elixir
defmodule ApiGateway.RouteTable.Loader do
  @doc """
  Simulates loading routes from a remote config service (~2 seconds).
  """
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

### Step 3: Extend `RateLimiter.Server` with crash recovery

The ETS table survives a process crash because it is a named table owned by the BEAM,
not by the process. When the process restarts, `init/1` checks whether the table already
exists. If it does, the data from the previous incarnation is still there — we recover
it via `handle_continue(:recover, state)` instead of starting from scratch.

The `:heir` option is not used here because the table uses `:named_table` and `:public`,
which means any process can read/write it. The table persists as long as the owning
process is alive; when the process dies, the table ownership transfers to the next
process that creates a table with the same name — but since the table already exists,
we skip creation entirely.

```elixir
# In lib/api_gateway/rate_limiter/server.ex

@table :rate_limiter_windows

@impl true
def init(_opts) do
  # Check if the ETS table survived a crash. :ets.info/1 returns :undefined
  # if the table does not exist, or a keyword list with table metadata if it does.
  case :ets.info(@table) do
    :undefined ->
      # First start — create the table from scratch.
      # :named_table allows access by atom name from any process.
      # :public allows concurrent reads from router processes (not just the owner).
      # :bag allows multiple timestamps per client_id (one per request).
      :ets.new(@table, [:named_table, :public, :bag])
      {:ok, %{}}

    _info ->
      # Table already exists — we crashed and restarted.
      # Defer recovery to handle_continue so init/1 returns fast.
      {:ok, %{}, {:continue, :recover}}
  end
end

@impl true
def handle_continue(:recover, state) do
  count = :ets.info(@table, :size)
  require Logger
  Logger.info("RateLimiter recovered #{count} window entries from ETS")
  {:noreply, state}
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/route_table/server_test.exs
defmodule ApiGateway.RouteTable.ServerTest do
  use ExUnit.Case, async: false

  alias ApiGateway.RouteTable.Server

  setup do
    # Start a fresh server for each test
    start_supervised!({Server, [traffic_class: :test]})
    :ok
  end

  describe "init/1 is non-blocking" do
    test "start_link returns before routes are loaded" do
      # The supervised server started in setup without blocking 2 seconds
      # If init/1 blocked, the test setup would take 2+ seconds
      assert true
    end

    test "returns not_ready before load completes" do
      # Immediately after startup, the server may not be ready yet
      # (handle_continue may still be running)
      case Server.lookup("/api/payments") do
        {:ok, _} -> :ok                          # loaded fast enough
        {:error, :not_ready} -> :ok              # still loading
        other -> flunk("unexpected: #{inspect(other)}")
      end
    end
  end

  describe "after loading completes" do
    setup do
      # Wait for handle_continue to finish
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

  # Helper: poll until ready or timeout
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
    # Clean the ETS table if it exists
    case :ets.info(:rate_limiter_windows) do
      :undefined -> :ok
      _ -> :ets.delete_all_objects(:rate_limiter_windows)
    end
    :ok
  end

  test "recovers window entries after crash" do
    {:ok, pid} = Server.start_link([])

    # Record some requests
    for _ <- 1..5, do: Server.record("client_crash_test")
    Process.sleep(20)

    # Verify entries exist
    assert {:allow, 5} = Server.check("client_crash_test", 10, 60_000)

    # Kill the process (simulates crash)
    Process.exit(pid, :kill)
    Process.sleep(50)

    # Start a new server — should recover from ETS
    {:ok, _new_pid} = Server.start_link([])
    Process.sleep(20)

    # The window entries must still be counted
    assert {:allow, remaining} = Server.check("client_crash_test", 10, 60_000)
    assert remaining == 5
  end
end
```

### Step 5: Run the tests

```bash
mix test test/api_gateway/route_table/server_test.exs \
         test/api_gateway/rate_limiter/recovery_test.exs --trace
```

### Step 6: Verify startup time does not block

```bash
# Time the application startup — should complete well under 1 second
# even though each RouteTable takes 2 seconds to load routes
time mix run --no-halt &
sleep 3
kill %1
```

---

## Trade-off analysis

| Approach | Startup time | Race safety | Complexity |
|----------|--------------|-------------|------------|
| All work in `init/1` | Slow — blocks supervisor | Safe | Low |
| `send(self(), :init)` (pre-OTP-21 hack) | Fast | Unsafe — race window | Medium |
| `handle_continue` from `init/1` | Fast | Safe | Low |
| `Task.async` inside `handle_continue` | Fast | Safe | Medium |
| ETS mirror + crash recovery | Fast | Safe | Medium |

Reflection question: `handle_continue` still runs in the GenServer process — it is not
truly asynchronous. If `Loader.load/1` takes 2 seconds, callers during that window get
`{:error, :not_ready}`. When would you spawn a `Task` from within `handle_continue`
instead of doing the work inline?

---

## Common production mistakes

**1. Blocking `init/1` with expensive work**
Putting a 2-second DB call directly in `init/1` blocks the supervisor. If 8 workers
start concurrently under a `Supervisor`, the first finishes in 2 s, the next at 4 s,
and so on — 16 s total. Always delegate slow initialization to `handle_continue`.

**2. Using `send(self(), :init)` instead of `handle_continue`**
This pattern predates `handle_continue`. It has a real race condition: if another process
sends a message to the new GenServer before the `:init` message is processed, the external
message runs before initialization completes. `handle_continue` guarantees ordering.

**3. Assuming `handle_continue` makes the GenServer non-blocking**
`handle_continue` runs in the GenServer's own process. While it executes, no other messages
are processed. A 2-second `handle_continue` blocks all callers for 2 seconds. That is why
callers must handle `{:error, :not_ready}` gracefully. Spawn a Task if you need true
concurrency.

**4. Forgetting to handle partial state on crash-recovery failure**
If `handle_continue(:recover, state)` itself crashes (e.g., ETS table was corrupted),
the supervisor restarts the process again. If `init/1` then sees the corrupted table and
tries to recover again, you loop. Always validate recovered state before using it, and
have a fallback to start fresh.

**5. Long `handle_continue` chains without per-step error handling**
A chain that crashes partway leaves the process in a half-initialized state. Each step
should be idempotent or include a rollback. If step 3 of a 5-step chain fails, the
process restarts and re-runs steps 1–2 — make sure they are safe to repeat.

---

## Resources

- [OTP 21 release notes — `handle_continue/2` introduction](https://www.erlang.org/blog/my-otp-21-highlights/)
- [Erlang/OTP docs — `gen_server`](https://www.erlang.org/doc/man/gen_server.html)
- [Saša Jurić — Elixir in Action, 2nd ed.](https://www.manning.com/books/elixir-in-action-second-edition) — ch. 7, GenServer internals
- [HexDocs — GenServer.handle_continue/2](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
