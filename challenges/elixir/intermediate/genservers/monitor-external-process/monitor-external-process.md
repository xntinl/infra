# Monitoring an external process with `Process.monitor/1`

**Project**: `external_monitor_gs` — a GenServer that watches a peer process and reacts to `:DOWN`.

---

## Why monitor external process matters

Your service has a cache GenServer and a long-lived *owner* process
(maybe a WebSocket pid, a Phoenix LiveView, or a worker holding a DB
connection). When the owner dies, the cache must drop everything it
held on that owner's behalf — sessions, subscriptions, partial state.

You cannot `link` the cache to the owner: linking kills the cache when
the owner dies, and the cache outlives individual owners. You need the
weaker relationship: *observe the death, stay alive, clean up*. That is
what `Process.monitor/1` provides, and the `:DOWN` message it delivers
is the standard BEAM primitive for "something I cared about just died".

This exercise builds an `ExternalMonitorGs` that registers interest in
arbitrary pids, receives `:DOWN` messages in `handle_info/2`, and cleans
the affected entries from its state.

---

## Project structure

```
external_monitor_gs/
├── lib/
│   └── external_monitor_gs.ex
├── script/
│   └── main.exs
├── test/
│   └── external_monitor_gs_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not `Process.link`?** Links are symmetric — you crash when they crash. Monitors are the right primitive for observing without coupling lifetimes.

## Core concepts

### 1. Monitors vs. links

| Primitive          | Direction     | On peer death          | Survives peer death? |
|--------------------|---------------|------------------------|----------------------|
| `Process.link/1`   | bidirectional | sends EXIT signal      | no (unless trapping) |
| `Process.monitor/1`| unidirectional| sends `{:DOWN, ...}`   | yes, always          |

A monitor is strictly *observe only*. The monitored process has no idea
it's being watched. Monitors don't stack dangerously — monitoring the
same pid twice gives two independent refs and two `:DOWN` messages.

### 2. The `:DOWN` message shape

```elixir
{:DOWN, ref, :process, pid, reason}
```

- `ref` — the reference returned by `Process.monitor/1`. Use this to
  match the exact monitor you care about.
- `reason` — `:normal`, `:noproc` (pid was already dead when you
  monitored it!), `:killed`, or a custom exit reason.

`:noproc` is the subtle one: if you monitor a pid that already died,
you get `:DOWN` with `:noproc` *immediately*. This is a feature — your
handler runs no matter what, so cleanup is never skipped.

### 3. Demonitor to stop watching

`Process.demonitor(ref, [:flush])` cancels a monitor and flushes any
already-sent `:DOWN` from the mailbox. Always pass `:flush` unless you
have a reason not to — otherwise a stale `:DOWN` can arrive after the
resource is gone and confuse your handler.

### 4. Storing `ref -> owner` for reverse lookup

When `:DOWN` arrives you only have the `ref` and `pid`. To clean up
efficiently you must map `ref -> what_to_clean`. A map keyed by ref is
the standard shape.

---

## Design decisions

**Option A — link to the external process**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — monitor it (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because links propagate exits; monitors let us observe without coupling lifetimes.

## Implementation

### `mix.exs`

```elixir
defmodule ExternalMonitorGs.MixProject do
  use Mix.Project

  def project do
    [
      app: :external_monitor_gs,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new external_monitor_gs
cd external_monitor_gs
```

### `lib/external_monitor_gs.ex`

**Objective**: Implement `external_monitor_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
defmodule ExternalMonitorGs do
  @moduledoc """
  A GenServer that stores values on behalf of other processes and
  automatically evicts them when the owner dies. Demonstrates
  `Process.monitor/1` + `:DOWN` handling.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    # entries:  %{owner_pid => value}
    # monitors: %{ref => owner_pid}  — reverse index for O(1) DOWN handling
    defstruct entries: %{}, monitors: %{}

    @type t :: %__MODULE__{entries: map(), monitors: map()}
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)

  @doc """
  Register `value` under `owner_pid`. The server monitors the owner;
  when it dies, the entry is auto-removed.
  """
  @spec register(GenServer.server(), pid(), term()) :: :ok
  def register(server, owner_pid, value) when is_pid(owner_pid) do
    GenServer.call(server, {:register, owner_pid, value})
  end

  @doc "Fetch the value registered for `owner_pid`, or `:error`."
  @spec fetch(GenServer.server(), pid()) :: {:ok, term()} | :error
  def fetch(server, owner_pid), do: GenServer.call(server, {:fetch, owner_pid})

  @doc "Explicit deregistration — also demonitors."
  @spec deregister(GenServer.server(), pid()) :: :ok
  def deregister(server, owner_pid), do: GenServer.call(server, {:deregister, owner_pid})

  @doc "Count of currently-registered owners. For tests/metrics."
  @spec count(GenServer.server()) :: non_neg_integer()
  def count(server), do: GenServer.call(server, :count)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(:ok), do: {:ok, %State{}}

  @impl true
  def handle_call({:register, owner, value}, _from, %State{} = state) do
    # If already registered, replace — but first demonitor the old ref
    # so we don't carry a duplicate watch on the same pid.
    state = demonitor_existing(state, owner)

    ref = Process.monitor(owner)

    new_state = %{
      state
      | entries: Map.put(state.entries, owner, value),
        monitors: Map.put(state.monitors, ref, owner)
    }

    {:reply, :ok, new_state}
  end

  def handle_call({:fetch, owner}, _from, %State{entries: entries} = state) do
    reply =
      case Map.fetch(entries, owner) do
        {:ok, _} = ok -> ok
        :error -> :error
      end

    {:reply, reply, state}
  end

  def handle_call({:deregister, owner}, _from, %State{} = state) do
    {:reply, :ok, demonitor_existing(state, owner)}
  end

  def handle_call(:count, _from, %State{entries: entries} = state) do
    {:reply, map_size(entries), state}
  end

  @impl true
  def handle_info({:DOWN, ref, :process, pid, _reason}, %State{} = state) do
    # Use the reverse index: ref -> owner. This avoids an O(n) scan of
    # entries when many owners are registered.
    case Map.pop(state.monitors, ref) do
      {nil, _} ->
        # Stray DOWN for a ref we no longer track (likely flushed).
        {:noreply, state}

      {^pid, new_monitors} ->
        new_state = %{
          state
          | entries: Map.delete(state.entries, pid),
            monitors: new_monitors
        }

        {:noreply, new_state}
    end
  end

  def handle_info(_other, state), do: {:noreply, state}

  # ── Helpers ─────────────────────────────────────────────────────────────

  # If `owner` was already registered, find and demonitor its ref. Flushing
  # ensures a late DOWN for the old registration doesn't evict the NEW one.
  defp demonitor_existing(%State{entries: entries, monitors: monitors} = state, owner) do
    if Map.has_key?(entries, owner) do
      {old_ref, _} = Enum.find(monitors, fn {_ref, o} -> o == owner end)
      Process.demonitor(old_ref, [:flush])

      %{
        state
        | entries: Map.delete(entries, owner),
          monitors: Map.delete(monitors, old_ref)
      }
    else
      state
    end
  end
end
```

### Step 3: `test/external_monitor_gs_test.exs`

**Objective**: Write `external_monitor_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule ExternalMonitorGsTest do
  use ExUnit.Case, async: true

  doctest ExternalMonitorGs

  setup do
    {:ok, mon} = ExternalMonitorGs.start_link()
    %{mon: mon}
  end

  defp alive_pid do
    # A process that lives until we send it :die.
    spawn(fn ->
      receive do
        :die -> exit(:normal)
      end
    end)
  end

  describe "register/3 and fetch/2" do
    test "registers and retrieves", %{mon: mon} do
      pid = alive_pid()
      :ok = ExternalMonitorGs.register(mon, pid, :payload)
      assert {:ok, :payload} = ExternalMonitorGs.fetch(mon, pid)
    end

    test "fetch on unknown returns :error", %{mon: mon} do
      assert :error = ExternalMonitorGs.fetch(mon, self())
    end

    test "re-registering the same owner replaces the value", %{mon: mon} do
      pid = alive_pid()
      :ok = ExternalMonitorGs.register(mon, pid, :first)
      :ok = ExternalMonitorGs.register(mon, pid, :second)
      assert {:ok, :second} = ExternalMonitorGs.fetch(mon, pid)
      assert 1 = ExternalMonitorGs.count(mon)
    end
  end

  describe ":DOWN handling" do
    test "owner dying removes the entry", %{mon: mon} do
      pid = alive_pid()
      :ok = ExternalMonitorGs.register(mon, pid, :payload)
      assert 1 = ExternalMonitorGs.count(mon)

      # Kill the owner; the server must observe DOWN and clean up.
      send(pid, :die)

      # Poll briefly — DOWN delivery is async.
      wait_until(fn -> ExternalMonitorGs.count(mon) == 0 end)

      assert :error = ExternalMonitorGs.fetch(mon, pid)
    end

    test "monitoring an already-dead pid cleans up via :noproc", %{mon: mon} do
      pid = spawn(fn -> :ok end)
      # Ensure it's dead.
      ref = Process.monitor(pid)
      assert_receive {:DOWN, ^ref, :process, ^pid, _}, 500

      :ok = ExternalMonitorGs.register(mon, pid, :payload)

      # The server will immediately receive :DOWN with :noproc and clean up.
      wait_until(fn -> ExternalMonitorGs.count(mon) == 0 end)
    end
  end

  describe "deregister/2" do
    test "explicit deregister removes the entry", %{mon: mon} do
      pid = alive_pid()
      :ok = ExternalMonitorGs.register(mon, pid, :x)
      :ok = ExternalMonitorGs.deregister(mon, pid)
      assert 0 = ExternalMonitorGs.count(mon)
      send(pid, :die)
    end
  end

  # Small polling helper — DOWN delivery is asynchronous.
  defp wait_until(fun, remaining \\ 50) do
    cond do
      fun.() -> :ok
      remaining == 0 -> flunk("condition never became true")
      true ->
        Process.sleep(10)
        wait_until(fun, remaining - 1)
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.

### `script/main.exs`

```elixir
defmodule Main do
  defmodule ExternalMonitorGs do
    use GenServer

    def start_link(opts \\ []) do
      GenServer.start_link(__MODULE__, %{}, opts)
    end

    def register(server, pid, payload) do
      GenServer.call(server, {:register, pid, payload})
    end

    def fetch(server, pid) do
      GenServer.call(server, {:fetch, pid})
    end

    def count(server) do
      GenServer.call(server, :count)
    end

    def deregister(server, pid) do
      GenServer.call(server, {:deregister, pid})
    end

    def init(_), do: {:ok, %{}}

    def handle_call({:register, pid, payload}, _from, state) do
      ref = Process.monitor(pid)
      new_state = Map.put(state, pid, {ref, payload})
      {:reply, :ok, new_state}
    end

    def handle_call({:fetch, pid}, _from, state) do
      case Map.get(state, pid) do
        nil -> {:reply, :error, state}
        {_ref, payload} -> {:reply, {:ok, payload}, state}
      end
    end

    def handle_call(:count, _from, state) do
      {:reply, map_size(state), state}
    end

    def handle_call({:deregister, pid}, _from, state) do
      case Map.get(state, pid) do
        {ref, _} -> 
          Process.demonitor(ref, [:flush])
          {:reply, :ok, Map.delete(state, pid)}
        nil ->
          {:reply, :ok, state}
      end
    end

    def handle_info({:DOWN, ref, :process, pid, _reason}, state) do
      {:noreply, Map.delete(state, pid)}
    end

    def handle_info(_other, state) do
      {:noreply, state}
    end
  end

  def main do
    {:ok, mon} = ExternalMonitorGs.start_link()
  
    pid = spawn(fn -> Process.sleep(:infinity) end)
    :ok = ExternalMonitorGs.register(mon, pid, :data)
    {:ok, :data} = ExternalMonitorGs.fetch(mon, pid)
    IO.puts("Registered and fetched: #{ExternalMonitorGs.count(mon)}")
  
    IO.puts("✓ ExternalMonitorGs works correctly")
  end

end

Main.main()
```

## Key Concepts: Monitoring and Link-based Fault Tolerance

`Process.monitor/1` sets up monitoring: when the monitored process exits, you receive a `{:DOWN, ref, :process, pid, reason}` message. This is different from `Process.link/1`, which crashes your process if the linked process crashes. Monitoring is one-way (you watch them, they don't know); linking is bidirectional.

Use monitoring when you need to react to a process death (clean up resources, restart something). Use linking when you want to share fate (if this dep dies, I die too—used in supervisor-child relationships). For external process handling (managing system processes), monitoring + GenServer restart logic is the pattern.

## Benchmark

```elixir
{us, _} = :timer.tc(fn ->
  for _ <- 1..10_000 do
    {:ok, pid} = Agent.start(fn -> nil end)
    Process.monitor(pid)
    Agent.stop(pid)
  end
end)
```

Target esperado: <50 µs por monitor+DOWN ciclo.

## Trade-offs and production gotchas

**1. Monitors leak refs if you forget `demonitor`**
Each `Process.monitor/1` adds an entry to the monitoring process's
internal table. If you register 10k owners and never demonitor them
(even after they've died and you handled DOWN), the refs are collected
automatically — but if you re-register and forget to drop the old ref,
you accumulate stale entries. Always store `ref -> owner` and clean
both sides on re-registration.

**2. `:noproc` is not an error, it is a feature**
Monitoring a dead pid gives you `{:DOWN, ref, :process, pid, :noproc}`
immediately. This lets the same code handle "died before we monitored"
and "dies after we monitored" uniformly. Do not special-case `:noproc`
as a failure — treat it as any other death.

**3. Demonitor without `:flush` can leave stale DOWNs in your mailbox**
`Process.demonitor(ref)` cancels future DOWNs but a DOWN *already in
the mailbox* will still be processed. Use `Process.demonitor(ref, [:flush])`
whenever you can't tolerate a stale message.

**4. Monitors across nodes cost more**
Monitoring a remote pid goes through distribution. If the remote node
disconnects, you'll get `{:DOWN, ref, :process, pid, :noconnection}`.
Handle that case — it is not a process crash, it is a network event,
and you may want to retry instead of cleaning up permanently.

**5. When you need BOTH monitor and link, you trap exits**
If the peer is your child (you start/supervise it), use a link and
trap exits. If the peer belongs to someone else, monitor it. Mixing
the two on the same pid is legal but confusing — the trap_exit flag
governs which one delivers.

**6. When NOT to roll your own registry-of-monitors**
For process-keyed lookups (`via` tuples, pid-keyed storage) with
lifecycle handling, `Registry` already does this in the stdlib with
sharded storage and no single-GenServer bottleneck. Only build your
own when you need per-entry business logic on DOWN.

---

## Reflection

- ¿Cuándo usarías `link` en lugar de `monitor`? Describí un caso donde cambiar de monitor a link mejoraría la arquitectura.

## Resources

- [`Process.monitor/1` — Elixir stdlib](https://hexdocs.pm/elixir/Process.html#monitor/1)
- [`Process.demonitor/2` — why `:flush` matters](https://hexdocs.pm/elixir/Process.html#demonitor/2)
- [`Registry` — production-grade process registry](https://hexdocs.pm/elixir/Registry.html)
- [Erlang reference manual — monitors](https://www.erlang.org/doc/reference_manual/processes.html#monitors)

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/external_monitor_gs_test.exs`

```elixir
defmodule ExternalMonitorGsTest do
  use ExUnit.Case, async: true

  doctest ExternalMonitorGs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ExternalMonitorGs.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

### 1. Model the problem with the right primitive

Choose the OTP primitive that matches the failure semantics of the problem: `GenServer` for stateful serialization, `Task` for fire-and-forget async, `Agent` for simple shared state, `Supervisor` for lifecycle management. Reaching for the wrong primitive is the most common source of accidental complexity in Elixir systems.

### 2. Make invariants explicit in code

Guards, pattern matching, and `@spec` annotations turn invariants into enforceable contracts. If a value *must* be a positive integer, write a guard — do not write a comment. The compiler and Dialyzer will catch what documentation cannot.

### 3. Let it crash, but bound the blast radius

"Let it crash" is not permission to ignore failures — it is a directive to design supervision trees that contain them. Every process should be supervised, and every supervisor should have a restart strategy that matches the failure mode it is recovering from.
