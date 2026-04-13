# Hot Code Upgrades with Release Handlers

**Project**: `coin_counter` — GenServer running as a release, upgraded from v1 (counts clicks) to v2 (counts clicks and records timestamps) without restarting the node, using `relup` scripts and the `release_handler`.

## Project context

Hot code upgrade is BEAM's party trick: a running node can swap code modules, migrate process state, and keep serving traffic — no rolling deploy, no connection drain. It was designed for telecom switches that had availability SLAs measured in minutes of downtime per year. Today most teams deploy behind load balancers and never use it; the ones that do use it (Heroku routers, some gaming backends, the WhatsApp cluster) derive real value.

Hot upgrades are misunderstood: people think they just "change a module and call it a day". That works inside `iex -S mix` with `r Module`, but a production upgrade with *state migration* and *supervision-tree changes* is a different world — it needs `relup` scripts, version numbers, and the `release_handler` application. This exercise walks the real path: build two releases (v1 and v2), produce a `relup`, apply it, and migrate state in `code_change/3`.

```
coin_counter/
├── lib/
│   └── coin_counter/
│       ├── application.ex
│       └── counter.ex                # the GenServer being upgraded
├── rel/
│   ├── coin_counter-1.0.0.rel        # release spec v1
│   └── coin_counter-2.0.0.rel        # release spec v2
├── relup/
│   └── 1.0.0_to_2.0.0.relup          # upgrade instructions
├── test/
│   └── coin_counter/
│       └── code_change_test.exs
├── mix.exs
└── UPGRADE_STEPS.md
```

## Why `relup` and not `Code.load_file/1`

`Code.load_file/1` puts new bytecode in the code server. Existing processes keep running old code until they call a *fully-qualified* function. If the module only changed a private function, they will never upgrade. If the state struct changed, the first call with the new shape crashes. `relup` scripts orchestrate the dance: suspend each process, run `code_change/3` to migrate its state, load the new module, and resume. It is the difference between "the bytecode is loaded" and "every running process is running the new version".

## Why this still matters

You probably deploy with Kubernetes and rolling upgrades. Then why learn this? Three reasons:

1. **State you cannot lose**: a GenServer holding 10GB of in-memory cache cannot be restarted without a warm-up cost. Hot upgrade keeps the state.
2. **Long-lived connections**: WebSocket hubs, game servers — a restart disconnects every client. Hot upgrade keeps them connected.
3. **It makes you understand OTP better**: `code_change/3` forces you to know the shape of your process state at every release.

## Core concepts

### 1. `.appup` file
Per-application upgrade instructions. Tells the release handler which modules changed and how (`:update`, `:add_module`, `:load_module`, `:remove_module`).

### 2. `.relup` file
Per-release upgrade instructions. Combines all `.appup`s into an ordered sequence of low-level operations (`:suspend`, `:load`, `:code_change`, `:resume`).

### 3. `code_change/3` callback
`GenServer.code_change(old_vsn, state, extra)` migrates the state. This is where `%StateV1{}` → `%StateV2{}` happens.

### 4. Supervision behaviour
`:soft_purge` (unload old code if no process is running it) vs `:brutal_purge` (kill any process still running old code). Choose soft; brutal is a panic button.

## Design decisions

- **Option A — `:code_change` migrates state in place**: safest; no data loss.
- **Option B — restart the process, reconstruct state from event log**: works if you have event sourcing. Con: defeats the point of a hot upgrade.

→ A for in-memory state; B where you already have events. We use A.

- **Option A — version bump per module (`@vsn`)**: release handler can detect drift.
- **Option B — single application version**: simpler but coarse.

→ A. Even with one module changing, emit `@vsn "2"` explicitly.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defmodule CoinCounter.MixProject do
  use Mix.Project

  def project do
    [
      app: :coin_counter,
      version: "1.0.0",       # bump to 2.0.0 before the second release
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      releases: releases(),
      deps: []
    ]
  end

  def application do
    [
      extra_applications: [:logger, :sasl, :runtime_tools],
      mod: {CoinCounter.Application, []}
    ]
  end

  defp releases do
    [
      coin_counter: [
        include_executables_for: [:unix],
        applications: [runtime_tools: :permanent]
      ]
    ]
  end
end
```

> `:sasl` is mandatory: `release_handler` is part of it.

### Step 1: Application

**Objective**: Minimize supervision tree so upgrade surface concentrates on code_change/3, not restart semantics.

```elixir
defmodule CoinCounter.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [CoinCounter.Counter]
    Supervisor.start_link(children, strategy: :one_for_one, name: CoinCounter.Supervisor)
  end
end
```

### Step 2: Counter v1

**Objective**: Stamp @vsn "1" so release_handler matches code version and dispatches correct code_change/3 clause on upgrade.

```elixir
defmodule CoinCounter.Counter do
  @moduledoc "v1 — just a click counter."
  @vsn "1"

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct [:count]
  end

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def click, do: GenServer.cast(__MODULE__, :click)
  def count, do: GenServer.call(__MODULE__, :count)

  @impl true
  def init(:ok), do: {:ok, %State{count: 0}}

  @impl true
  def handle_cast(:click, %State{count: c} = s), do: {:noreply, %{s | count: c + 1}}

  @impl true
  def handle_call(:count, _from, %State{count: c} = s), do: {:reply, c, s}
end
```

### Step 3: Counter v2 — add `:timestamps` field

**Objective**: Add :timestamps field and code_change/3 to migrate v1 state live without losing accumulated count.

```elixir
defmodule CoinCounter.Counter do
  @moduledoc "v2 — clicks + timestamps (last 100)."
  @vsn "2"

  use GenServer

  @max_timestamps 100

  defmodule State do
    @moduledoc false
    defstruct count: 0, timestamps: []
  end

  def start_link(_), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)
  def click, do: GenServer.cast(__MODULE__, :click)
  def count, do: GenServer.call(__MODULE__, :count)
  def recent(n \\ 10), do: GenServer.call(__MODULE__, {:recent, n})

  @impl true
  def init(:ok), do: {:ok, %State{}}

  @impl true
  def handle_cast(:click, %State{} = s) do
    now = System.monotonic_time(:millisecond)
    ts = [now | Enum.take(s.timestamps, @max_timestamps - 1)]
    {:noreply, %{s | count: s.count + 1, timestamps: ts}}
  end

  @impl true
  def handle_call(:count, _f, s), do: {:reply, s.count, s}
  def handle_call({:recent, n}, _f, s), do: {:reply, Enum.take(s.timestamps, n), s}

  # --- state migration v1 → v2 ---

  @impl true
  def code_change("1", old_state, _extra) do
    # old_state has shape %CoinCounter.Counter.State{count: N} — only :count defaulted.
    # We re-wrap into the new struct explicitly so the record descriptor is the new one.
    count = Map.get(old_state, :count, 0)
    {:ok, %State{count: count, timestamps: []}}
  end

  def code_change(_other, state, _extra), do: {:ok, state}
end
```

### Step 4: `.rel` files — release specs

**Objective**: Lock exact ERTS + OTP versions so systools diffs releases and emits valid relup instructions.

```erlang
%% rel/coin_counter-1.0.0.rel
{release,
 {"coin_counter", "1.0.0"},
 {erts, "15.0"},
 [
  {kernel, "10.0"},
  {stdlib, "6.0"},
  {sasl, "4.2"},
  {coin_counter, "1.0.0"}
 ]}.
```

```erlang
%% rel/coin_counter-2.0.0.rel
{release,
 {"coin_counter", "2.0.0"},
 {erts, "15.0"},
 [
  {kernel, "10.0"},
  {stdlib, "6.0"},
  {sasl, "4.2"},
  {coin_counter, "2.0.0"}
 ]}.
```

### Step 5: `.appup` file — required for `mix release` to produce a `relup`

**Objective**: Declare {update, Counter, {advanced, []}} so release_handler suspends GenServer, loads new code, routes to code_change/3.

Place at `priv/appup/coin_counter.appup` (or rely on `Mix.Tasks.Release` when using `distillery`/`mix release` + appup generator):

```erlang
{"2.0.0",
 [{"1.0.0", [
    {update, 'Elixir.CoinCounter.Counter', {advanced, []}, [], []}
  ]}],
 [{"1.0.0", [
    {update, 'Elixir.CoinCounter.Counter', {advanced, []}, [], []}
  ]}]}.
```

`{advanced, []}` means "use `code_change/3`". The empty argument goes into `extra`.

### Step 6: Assemble and upgrade — `UPGRADE_STEPS.md`

**Objective**: Test full v1→v2 cycle so state survives code swap on running node end-to-end via release_handler.

```bash
# -- 1. Build v1 --
MIX_ENV=prod mix release --path _build/prod/rel/coin_counter --version 1.0.0

# -- 2. Start v1 in the foreground --
_build/prod/rel/coin_counter/bin/coin_counter daemon

# -- 3. Generate some state --
_build/prod/rel/coin_counter/bin/coin_counter rpc 'CoinCounter.Counter.click()'
_build/prod/rel/coin_counter/bin/coin_counter rpc 'CoinCounter.Counter.count()'  #=> 1

# -- 4. Bump mix.exs version to 2.0.0 and rebuild as an upgrade package --
# (swap the Counter module source for v2)
MIX_ENV=prod mix release --path _build/prod/rel/coin_counter --version 2.0.0 --upgrade

# -- 5. Copy the new tarball into the release's releases/ directory --
cp _build/prod/rel/coin_counter/releases/2.0.0/coin_counter.tar.gz \
   _build/prod/rel/coin_counter/releases/

# -- 6. Apply the upgrade via release_handler --
_build/prod/rel/coin_counter/bin/coin_counter rpc \
  ':release_handler.unpack_release(~c"coin_counter_2.0.0")'

_build/prod/rel/coin_counter/bin/coin_counter rpc \
  ':release_handler.install_release(~c"2.0.0")'

_build/prod/rel/coin_counter/bin/coin_counter rpc \
  ':release_handler.make_permanent(~c"2.0.0")'

# -- 7. Verify state survived the upgrade --
_build/prod/rel/coin_counter/bin/coin_counter rpc 'CoinCounter.Counter.count()'    #=> 1 (preserved)
_build/prod/rel/coin_counter/bin/coin_counter rpc 'CoinCounter.Counter.click()'
_build/prod/rel/coin_counter/bin/coin_counter rpc 'CoinCounter.Counter.recent(5)'  #=> [ts...]
```

## Upgrade sequence diagram

```
Operator          release_handler        Supervisor          Counter proc
   │                     │                    │                   │
   │ unpack_release      │                    │                   │
   ├────────────────────▶│                    │                   │
   │                     │                    │                   │
   │ install_release     │                    │                   │
   ├────────────────────▶│                    │                   │
   │                     │  load new code     │                   │
   │                     │────────────────────┼──▶ code server    │
   │                     │                    │                   │
   │                     │  sys:suspend       │                   │
   │                     │────────────────────┼──────────────────▶│
   │                     │                    │                   │ (blocks)
   │                     │  sys:change_code   │                   │
   │                     │────────────────────┼──────────────────▶│
   │                     │                    │                   │ code_change/3
   │                     │                    │                   │ state v1 → v2
   │                     │  sys:resume        │                   │
   │                     │────────────────────┼──────────────────▶│
   │                     │                    │                   │ (runs v2)
   │                     │                    │                   │
   │ make_permanent      │                    │                   │
   ├────────────────────▶│                    │                   │
```

## Tests

```elixir
defmodule CoinCounter.CodeChangeTest do
  use ExUnit.Case, async: true

  alias CoinCounter.Counter

  describe "code_change/3 v1 → v2" do
    test "migrates old state with only :count into new struct" do
      old_state = %{__struct__: Counter.State, count: 7}
      assert {:ok, %Counter.State{count: 7, timestamps: []}} =
               Counter.code_change("1", old_state, [])
    end

    test "passthrough for same version" do
      state = %Counter.State{count: 1, timestamps: [100]}
      assert {:ok, ^state} = Counter.code_change("2", state, [])
    end
  end
end
```

## Benchmark

The operationally meaningful metric is **upgrade time**. Measure the wall clock around `install_release`:

```elixir
# bench/upgrade_time.exs — run inside a live v1 node.
:timer.tc(fn ->
  :release_handler.install_release(~c"2.0.0")
end)
```

Target: under 500ms for a small app with a single GenServer, independent of in-memory state size (state migration itself is O(n) over fields, not O(items) unless you touch collections). If upgrade time > 5 seconds, the likely cause is `code_change/3` doing real work.

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Struct updates are not automatic**
A struct is a map with a `__struct__` field. If v2 adds a field, old states still have the v1 shape. Without `code_change/3` explicitly re-wrapping, you get a `KeyError` on first access of the new field. Always migrate.

**2. Removing callbacks breaks `:sys` commands**
If v2 removes `handle_call(:count, ...)`, every pending `GenServer.call(..., :count, ...)` in-flight during the upgrade will crash the process on resume. Never drop callbacks; deprecate, then remove across two releases.

**3. Supervisor shape changes need `:supervisor.change_children_spec`**
Adding/removing a child under a live supervisor requires an `.appup` instruction, not a simple module reload. `mix release --upgrade` does NOT generate this automatically — you must hand-edit the `.appup`.

**4. Hot upgrade between BEAM versions**
An upgrade that bumps ERTS is *not* hot. You need to restart the node. Release your own app on the same ERTS whenever feasible.

**5. `:brutal_purge` can evict a running process**
If two processes are running the same module and only one has migrated, `:brutal_purge` kills the lagging one. `:soft_purge` waits. Default to `:soft_purge` in `.appup` and only escalate when you have diagnosed the hold-out.

**6. When NOT to hot upgrade**
Stateless web workers behind a load balancer. Rolling restart is simpler, safer, and gives you rollback for free. Hot upgrade pays off only when the state or connections are *expensive* to recreate.

## Reflection

`code_change/3` is the only place in your codebase that sees *two* versions of your state at once. It's also the only place that runs during the upgrade window — so it must be fast, deterministic, and crash-free. Pick a recent PR you merged that changed a GenServer's state. Sketch the `code_change/3` you would have needed. Were there any migrations that would have been impossible without downtime?

## Resources

- [OTP Design Principles — Release Handling](https://www.erlang.org/doc/design_principles/release_handling.html)
- [`:release_handler` reference](https://www.erlang.org/doc/man/release_handler.html)
- [`.appup` cookbook](https://www.erlang.org/doc/design_principles/appup_cookbook.html)
- [Adopting Erlang — hot upgrades chapter](https://adoptingerlang.org/docs/production/hot_code_loading/) (free online)
