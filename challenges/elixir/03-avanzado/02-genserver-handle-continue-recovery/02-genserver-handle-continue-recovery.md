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
    # TODO: return immediately so the supervisor is not blocked.
    # Defer route loading to handle_continue/2.
    #
    # Initial state fields:
    #   :routes  — map of path_prefix => upstream_url (empty until loaded)
    #   :ready   — false until loading completes
    #   :traffic_class — from opts, defaults to :default
    #
    # HINT: {:ok, initial_state, {:continue, :load_routes}}
    Logger.info("RouteTable starting — loading deferred")
    _traffic_class = Keyword.get(opts, :traffic_class, :default)
    # TODO
  end

  @impl true
  def handle_continue(:load_routes, state) do
    # TODO: call ApiGateway.RouteTable.Loader.load(state.traffic_class)
    # which simulates a 2-second remote call.
    # On success: update state with routes and set ready: true.
    # On error: log and retry via {:noreply, state, {:continue, :load_routes}}
    #
    # HINT: pattern match on {:ok, routes} | {:error, reason}
  end

  @impl true
  def handle_call({:lookup, _path}, _from, %{ready: false} = state) do
    # TODO: return {:error, :not_ready} without crashing the caller
  end

  @impl true
  def handle_call({:lookup, path}, _from, %{ready: true} = state) do
    # TODO: find the longest matching prefix in state.routes
    # HINT: Enum.find_value on Map.keys sorted by length desc
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

Add recovery to the existing server from the rate limiter exercise:

```elixir
# In lib/api_gateway/rate_limiter/server.ex
# Extend init/1 and add handle_continue(:recover, state)

@table :rate_limiter_windows

@impl true
def init(_opts) do
  # TODO: check if the ETS table already exists (survived a crash)
  # If yes: {:ok, state, {:continue, :recover}}
  # If no:  create table, return {:ok, state}
  #
  # HINT: :ets.info(@table) returns :undefined if table does not exist
  # HINT: create with [:named_table, :public, :bag]
  #   :named_table — accessed by name in check/3
  #   :public      — concurrent reads from any process
  #   :bag         — multiple timestamps per client_id
end

@impl true
def handle_continue(:recover, state) do
  # TODO: count how many entries are in the table and log the recovery
  # HINT: :ets.info(@table, :size) returns the number of entries
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
