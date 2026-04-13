# Inspecting a live system with `:observer`

**Project**: `observable_app` — a tiny app that runs a supervised worker,
creates an ETS table, and opens an ephemeral port, so you can practice
reading each of those things in Observer.

---

## Project context

`:observer` is the GUI system inspector that ships with OTP. It connects
to any BEAM node (local or remote), and shows you processes, memory,
supervision trees, ETS tables, ports, applications, and schedulers — in
real time, sortable, drillable.

Every Elixir developer should be able to answer "why is this node
misbehaving?" with `:observer.start()`. This exercise creates a deliberate
mini-zoo: a worker GenServer, an ETS table, a port to `/bin/cat`, and a
supervision tree — so you have something concrete to explore.

Project structure:

```
observable_app/
├── lib/
│   ├── observable_app.ex
│   ├── observable_app/
│   │   ├── application.ex
│   │   ├── worker.ex
│   │   └── cache.ex      # ETS owner
├── test/
│   └── observable_app_test.exs
└── mix.exs
```

> **Important**: `:observer` requires `wx`. Debian/Ubuntu: `apt install
> erlang-observer`. macOS with Homebrew Erlang: `brew install erlang`
> (installs with wx). Nerves and Alpine often don't have wx — use
> `:observer_cli` (headless TUI) there.

---

## Core concepts

### 1. `:observer.start/0` launches the GUI

```elixir
iex -S mix
iex> :observer.start()
```

The System tab opens first. The Applications tab shows every started OTP
app with its supervision tree. Clicking a process opens its detail view —
state, mailbox, stacktrace, links, monitors.

### 2. Connecting to a REMOTE node

For production nodes you start Observer locally and connect over
distribution:

```bash
iex --sname debug --cookie shared_secret
iex> Node.connect(:"myapp@prod-host")
iex> :observer.start()
# File → Connect... → pick the remote node
```

The node must already be started with `--name`/`--sname` and share a cookie.
Observer's network tab will list it.

### 3. What to click FIRST

| Problem                         | Tab to visit                              |
|---------------------------------|--------------------------------------------|
| "Node is slow"                  | Load Charts → scheduler utilization        |
| "Memory is climbing"            | System → memory totals; Processes → memory |
| "Which process is busy?"        | Processes → sort by Reductions/sec         |
| "Which process is stuck?"       | Processes → Message Queue Len column       |
| "Is my supervisor tree right?"  | Applications → click the app name          |
| "How big is my ETS table?"      | Table Viewer                               |
| "Are my ports leaking?"         | Ports tab                                  |

### 4. ETS and Ports inside Observer

- **Table Viewer** tab shows every ETS/DETS table. Double-click to see
  entries (careful on huge tables — Observer loads them all).
- **Ports** tab shows open ports and who owns them. This is how you
  detect leaked external-process ports.

---

## Why Observer and not logs + metrics

Logs tell you what happened; metrics tell you trends over time. Neither
answers "right now, which process is holding 500MB?" or "what's in this
GenServer's state?". Observer is the live-node introspection layer:
process state, mailbox contents, supervision topology — the shape of the
system at this instant. You still need logs and metrics; Observer is the
third leg.

---

## Design decisions

**Option A — Run `:observer` directly on the production node**
- Pros: Zero setup; immediate visibility into the node with the problem.
- Cons: Needs `wx` (not in Alpine/distroless); allocates significant
  memory on the target; ties the GUI session to the node's lifetime.

**Option B — Run Observer locally, connect via distribution** (chosen)
- Pros: The heavy GUI runs on your laptop; the prod node only pays the
  cost of answering remote calls; survives local shell crashes.
- Cons: Requires distribution + shared cookie + network reachability
  (SSH tunnel in practice).

→ Chose **B** because production nodes should stay lean and headless;
  the observer workstation pattern is how every serious BEAM shop does it.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {genserver},
    {noreply},
    {observer},
    {ok},
    {reply},
    {spawn},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new observable_app --sup
cd observable_app
```

`--sup` generates `lib/observable_app/application.ex` with a supervision
tree ready to edit.

### Step 2: `lib/observable_app/worker.ex` — a slightly busy GenServer

**Objective**: Edit `worker.ex` — a slightly busy GenServer, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.


```elixir
defmodule ObservableApp.Worker do
  @moduledoc """
  A GenServer that periodically increments a counter. Creates measurable
  reduction counts so it's visible in Observer's Processes tab.
  """

  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(_opts) do
    schedule()
    {:ok, %{ticks: 0}}
  end

  @impl true
  def handle_info(:tick, %{ticks: t} = state) do
    # Do a bit of CPU work so Observer shows non-zero reductions.
    _ = Enum.sum(1..1_000)
    schedule()
    {:noreply, %{state | ticks: t + 1}}
  end

  @doc "Returns the current tick count."
  @spec count() :: non_neg_integer()
  def count, do: GenServer.call(__MODULE__, :count)

  @impl true
  def handle_call(:count, _from, %{ticks: t} = state), do: {:reply, t, state}

  defp schedule, do: Process.send_after(self(), :tick, 100)
end
```

### Step 3: `lib/observable_app/cache.ex` — an ETS owner

**Objective**: Edit `cache.ex` — an ETS owner, exposing code whose shape is chosen to exercise the tool's capabilities, not to solve a domain problem.


```elixir
defmodule ObservableApp.Cache do
  @moduledoc """
  Owns a named public ETS table. Under the hood, Observer's Table Viewer
  reads :ets.info/1 for any named table — owning one makes it visible.
  """

  use GenServer

  @table :observable_app_cache

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def put(key, value), do: :ets.insert(@table, {key, value})
  def get(key) do
    case :ets.lookup(@table, key) do
      [{^key, value}] -> {:ok, value}
      [] -> :error
    end
  end

  @impl true
  def init(_opts) do
    # :named_table + :public so the ETS is inspectable AND writable without a copy.
    :ets.new(@table, [:set, :named_table, :public, read_concurrency: true])
    {:ok, %{}}
  end
end
```

### Step 4: `lib/observable_app/application.ex`

**Objective**: Wire `application.ex` to start the runtime wiring needed so the tool under study has something real to inspect, format, or report on.


```elixir
defmodule ObservableApp.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ObservableApp.Cache,
      ObservableApp.Worker
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ObservableApp.Supervisor)
  end
end
```

Make sure `mix.exs` lists `mod:` so the Application starts:

```elixir
def application do
  [
    extra_applications: [:logger],
    mod: {ObservableApp.Application, []}
  ]
end
```

### Step 5: A simple API so tests have something to hit

**Objective**: Provide A simple API so tests have something to hit — these are the supporting fixtures the main module depends on to make its concept demonstrable.


```elixir
# lib/observable_app.ex
defmodule ObservableApp do
  @moduledoc """
  Entry point. Use `iex -S mix` and then call `:observer.start()` to explore
  the running system.
  """

  defdelegate tick_count(), to: ObservableApp.Worker, as: :count
  defdelegate put(k, v), to: ObservableApp.Cache
  defdelegate get(k), to: ObservableApp.Cache
end
```

### Step 6: `test/observable_app_test.exs`

**Objective**: Write `observable_app_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule ObservableAppTest do
  use ExUnit.Case, async: false  # the app supervision tree is shared state

  test "worker starts and increments" do
    initial = ObservableApp.tick_count()
    Process.sleep(250)
    assert ObservableApp.tick_count() > initial
  end

  test "cache stores and retrieves" do
    :ok = ObservableApp.put("k", 42) && :ok
    assert ObservableApp.get("k") == {:ok, 42}
  end

  test "ETS table is named and alive" do
    # A sanity assertion that Observer would reflect: the named table exists.
    info = :ets.info(:observable_app_cache)
    assert is_list(info)
    assert Keyword.get(info, :named_table) == true
  end
end
```

### Step 7: Run it

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
iex -S mix
iex> :observer.start()
# Then:
#   1) Applications tab → click :observable_app → see the supervision tree
#   2) Processes tab → sort by Reductions → find ObservableApp.Worker
#   3) Table Viewer tab → open :observable_app_cache
#   4) Put some data: ObservableApp.put("hello", "world"); refresh Table Viewer.
```

To see a port: open an ad-hoc port from IEx:

```elixir
iex> port = Port.open({:spawn, "cat"}, [:binary])
# now open Observer → Ports tab, find that port, see its connected pid (YOU).
iex> Port.close(port)
```

### Why this works

Observer is a `runtime_tools` client that reads process / ETS / port info
via the same BEAM primitives `Process.info/1` and `:ets.info/1` expose —
it just packages them into an auto-refreshing GUI. Connecting over
distribution means every query runs RPC-style on the remote node; the
local VM only renders. The app's supervision tree, the ETS table, and
the port on cat give the GUI something concrete to render in each tab.

---

## Benchmark

<!-- benchmark N/A: Observer is an interactive introspection tool.
     The relevant cost is the overhead it adds to the observed node
     (~1-5% CPU per refresh window); absolute throughput is not meaningful. -->

---

## Trade-offs and production gotchas

**1. Observer is heavy — don't run it IN prod**
Starting `:observer` on a production node allocates significant memory
and pulls every process's info every second. Use it LOCALLY, connected
to prod via distribution. For headless prod, use `:observer_cli` or
`:recon`.

**2. The Processes tab snapshots — reductions are rates, not totals**
Observer's default Reductions column shows the delta since the last
refresh, not absolute. Sorting finds the currently busy processes.
`runtime_info/0` shows cumulative counts for the shell session.

**3. Clicking big ETS tables LOADS THEM**
The Table Viewer is not streaming — it pulls every entry. Clicking a
10-million-row table freezes your Observer AND puts load on the node.
Use `:ets.select/2` with a pattern instead, from IEx.

**4. `:observer` needs `:wx` — absent on many container images**
Alpine, distroless, and Nerves don't bundle `wx`. Install the full
`erlang-observer` / `erlang-wx` packages explicitly if you need local
GUI observation. Remote observation from your laptop is the escape hatch.

**5. Distributed connection uses unencrypted TCP by default**
Don't open your prod cookie over the public internet. Tunnel through SSH
(`ssh -L 4369:localhost:4369 prod-host`) or configure TLS distribution
(`-proto_dist inet_tls`). Observer works fine over the tunnel.

**6. When NOT to use Observer**
- On headless/containerized nodes — use `:observer_cli` or `:recon`.
- For automated monitoring — use `:telemetry` + Prometheus, not screen
  scraping.
- For trace-level debugging — use `:recon_trace` or `:dbg`; Observer
  shows state, not call flow.

---

## Reflection

- A prod node reports "memory keeps climbing, nothing restarts". You
  open Observer over SSH. Walk through the sequence of tabs you'd visit
  and what you'd look for in each to narrow down whether the culprit is
  a specific process, ETS, binaries, or the atom table.
- Your team ships on Alpine Linux containers without wx. How would you
  reproduce an Observer-equivalent workflow for postmortem inspection,
  and what tradeoffs would you accept (latency of inspection vs node
  overhead vs expressiveness)?

---

## Resources

- [`:observer` — Erlang docs](https://www.erlang.org/doc/apps/observer/observer_ug.html) — the full UG
- [`observer_cli`](https://hexdocs.pm/observer_cli/) — headless variant; critical for production
- [Distribution Protocol — Erlang](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html) — how remote connections work
- [`:ets.info/1`](https://www.erlang.org/doc/man/ets.html#info-1) — what Observer's Table Viewer shows
- [`:runtime_tools`](https://www.erlang.org/doc/man/runtime_tools.html) — Observer depends on this; loaded via `extra_applications`


## Deep Dive

Elixir's tooling ecosystem extends beyond the language into DevOps, profiling, and observability. Understanding each tool's role prevents misuse and false optimizations.

**Mix tasks and releases:**
Custom mix tasks (`mix myapp.setup`, `mix myapp.migrate`) encapsulate operational knowledge. Tasks run in the host environment (not the compiled app), so they're ideal for setup, teardown, or scripting. Releases, built with `mix release`, create self-contained OTP applications deployable without Elixir installed. They're immutable: no source code changes after release — all config comes from environment variables or runtime files.

**Debugging and profiling tools:**
- `:observer` (GUI): real-time process tree, metrics, and port inspection
- `Recon`: production-safe introspection (stable even under high load)
- `:eprof`: function-level timing; lower overhead than `:fprof`
- `:fprof`: detailed trace analysis; use only in staging

**Profiling approaches:**
Ceiling profiling (e.g., "which modules consume CPU?") is cheap; go there first with `perf` or `eprof`. Floor profiling (e.g., "which lines in this function are slow?") is expensive; reserve for specific functions. In production, prefer metrics (Prometheus, New Relic) over profiling — continuous profiling has overhead. Store profiling data for post-mortem analysis, not real-time dashboards.
