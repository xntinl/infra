# Inspecting a live system with `:observer`

**Project**: `observable_app` — a tiny app that runs a supervised worker,
creates an ETS table, and opens an ephemeral port, so you can practice
reading each of those things in Observer.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

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
> `:observer_cli` (exercise 125 territory) there.

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

## Implementation

### Step 1: Create the project

```bash
mix new observable_app --sup
cd observable_app
```

`--sup` generates `lib/observable_app/application.ex` with a supervision
tree ready to edit.

### Step 2: `lib/observable_app/worker.ex` — a slightly busy GenServer

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

---

## Trade-offs and production gotchas

**1. Observer is heavy — don't run it IN prod**
Starting `:observer` on a production node allocates significant memory
and pulls every process's info every second. Use it LOCALLY, connected
to prod via distribution. For headless prod, use `:observer_cli` or
`:recon` (exercise 125).

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

## Resources

- [`:observer` — Erlang docs](https://www.erlang.org/doc/apps/observer/observer_ug.html) — the full UG
- [`observer_cli`](https://hexdocs.pm/observer_cli/) — headless variant; critical for production
- [Distribution Protocol — Erlang](https://www.erlang.org/doc/apps/erts/erl_dist_protocol.html) — how remote connections work
- [`:ets.info/1`](https://www.erlang.org/doc/man/ets.html#info-1) — what Observer's Table Viewer shows
- [`:runtime_tools`](https://www.erlang.org/doc/man/runtime_tools.html) — Observer depends on this; loaded via `extra_applications`
