# `Agent` vs `GenServer` — when to upgrade

**Project**: `agent_vs_gs` — the same counter, implemented twice, so you can feel the ceiling of `Agent`.

---

## Project context

Every Elixir project eventually hits the question: "is this an `Agent`
or a `GenServer`?". The textbook answer — "Agent is for state,
GenServer is for behavior" — is correct but too abstract. You don't
actually feel the difference until you've written the same thing both
ways and watched the `Agent` version bend the moment a requirement
arrives that it can't accommodate.

This exercise implements a counter with both. Both versions pass the
same basic tests. Then we add three requirements that progressively
break the `Agent` version: a periodic reset, a monitored client that
auto-decrements on DOWN, and a structured start-up log. The `GenServer`
swallows each addition; the `Agent` requires a rewrite.

The takeaway is a concrete decision rule, not a slogan.

Project structure:

```
agent_vs_gs/
├── lib/
│   ├── counter_agent.ex
│   └── counter_gs.ex
├── test/
│   └── counter_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not always GenServer?** For pure state, Agent's API is smaller and clearer — boilerplate matters for readability.

## Core concepts

### 1. `Agent` has exactly one operation shape: closure over state

```
Agent.get(pid, fun)            # read
Agent.update(pid, fun)         # write
Agent.get_and_update(pid, fun) # read+write
```

That's the whole API. The closure is your "protocol". There is no
`handle_info`, no `terminate`, no `handle_continue`. It is a deliberate
reduction of GenServer, not a smaller alternative.

### 2. `GenServer` is the full OTP worker

```
handle_call/3       # synchronous request with reply
handle_cast/2       # asynchronous request, no reply
handle_info/2       # non-OTP messages: timers, monitors, raw sends
handle_continue/2   # run after init, with state visible to callers
init/1              # startup
terminate/2         # cleanup
code_change/3       # hot code upgrades (rare, but real)
```

Any process that needs to react to time, monitors, ports, nodes, or
unstructured messages *must* be a `GenServer` (or another OTP behaviour).

### 3. The decision rule

Use `Agent` when **all** of the following hold:

- State-only: every operation is "read state" or "transform state".
- No timers, no monitors, no raw `send` messages to handle.
- Operations fit comfortably in a closure (no protocol to document).
- You're not likely to need any of the above in the near future.

If any one of those goes sideways, skip straight to `GenServer`.
Refactoring later is cheap if the API is small, but once `Agent` state
spreads across callsites (each with its own closure), migration costs
climb.

### 4. Trade in both directions is underrated

Some teams reach for `GenServer` reflexively and end up with boilerplate
wrappers over `Map.put`/`Map.get`. `Agent` exists to avoid that. Others
write everything as `Agent` and then contort around missing callbacks.
The right answer is usually "agent until it hurts, then genserver" —
and you won't feel the hurt unless you've used both.

---

## Design decisions

**Option A — Agent for everything with state**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — Agent only for pure state, GenServer for anything with logic (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because logic in Agent callbacks hides behind closures and is untestable in isolation.


## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
  ]
end
```




### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new agent_vs_gs
cd agent_vs_gs
```

### Step 2: `lib/counter_agent.ex`

**Objective**: Implement `counter_agent.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.


```elixir
defmodule CounterAgent do
  @moduledoc """
  A counter implemented as an `Agent`. Supports `inc`, `dec`, `value`,
  and `reset`. Good as long as the requirements stay purely state-shaped.
  """

  use Agent

  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(opts \\ []) do
    initial = Keyword.get(opts, :initial, 0)
    Agent.start_link(fn -> initial end, Keyword.take(opts, [:name]))
  end

  @spec value(Agent.agent()) :: integer()
  def value(agent), do: Agent.get(agent, & &1)

  @spec inc(Agent.agent(), integer()) :: :ok
  def inc(agent, by \\ 1), do: Agent.update(agent, &(&1 + by))

  @spec dec(Agent.agent(), integer()) :: :ok
  def dec(agent, by \\ 1), do: Agent.update(agent, &(&1 - by))

  @spec reset(Agent.agent()) :: :ok
  def reset(agent), do: Agent.update(agent, fn _ -> 0 end)
end
```

### Step 3: `lib/counter_gs.ex`

**Objective**: Implement `counter_gs.ex` — the concurrency primitive whose back-pressure, linking, and timeout semantics we are isolating.


```elixir
defmodule CounterGs do
  @moduledoc """
  The same counter as `CounterAgent`, plus three things `Agent` cannot
  express:

    * Periodic auto-reset on a `Process.send_after/3` tick.
    * Monitoring a client pid and auto-decrementing when it exits.
    * Structured `Logger` lifecycle events from `init/1` / `terminate/2`.
  """

  use GenServer
  require Logger

  defmodule State do
    @moduledoc false
    defstruct count: 0, reset_ms: nil, timer: nil, watched: %{}
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, Keyword.take(opts, [:name]))
  end

  @spec value(GenServer.server()) :: integer()
  def value(server), do: GenServer.call(server, :value)

  @spec inc(GenServer.server(), integer()) :: :ok
  def inc(server, by \\ 1), do: GenServer.cast(server, {:inc, by})

  @spec dec(GenServer.server(), integer()) :: :ok
  def dec(server, by \\ 1), do: GenServer.cast(server, {:dec, by})

  @spec reset(GenServer.server()) :: :ok
  def reset(server), do: GenServer.cast(server, :reset)

  @doc """
  Registers a client pid. When that pid exits, the counter decrements
  by 1 automatically — something impossible with `Agent`.
  """
  @spec watch(GenServer.server(), pid()) :: :ok
  def watch(server, pid), do: GenServer.call(server, {:watch, pid})

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(opts) do
    initial = Keyword.get(opts, :initial, 0)
    reset_ms = Keyword.get(opts, :reset_ms)
    Logger.debug("CounterGs start initial=#{initial} reset_ms=#{inspect(reset_ms)}")
    state = schedule_reset(%State{count: initial, reset_ms: reset_ms})
    {:ok, state}
  end

  @impl true
  def handle_call(:value, _from, state), do: {:reply, state.count, state}

  def handle_call({:watch, pid}, _from, state) do
    ref = Process.monitor(pid)
    {:reply, :ok, %{state | watched: Map.put(state.watched, ref, pid)}}
  end

  @impl true
  def handle_cast({:inc, by}, state), do: {:noreply, %{state | count: state.count + by}}
  def handle_cast({:dec, by}, state), do: {:noreply, %{state | count: state.count - by}}
  def handle_cast(:reset, state), do: {:noreply, %{state | count: 0}}

  @impl true
  def handle_info(:auto_reset, state) do
    Logger.debug("CounterGs auto_reset")
    {:noreply, schedule_reset(%{state | count: 0})}
  end

  def handle_info({:DOWN, ref, :process, _pid, _reason}, state) do
    case Map.pop(state.watched, ref) do
      {nil, _} -> {:noreply, state}
      {_pid, rest} -> {:noreply, %{state | count: state.count - 1, watched: rest}}
    end
  end

  def handle_info(_other, state), do: {:noreply, state}

  @impl true
  def terminate(reason, _state) do
    Logger.debug("CounterGs terminate reason=#{inspect(reason)}")
    :ok
  end

  # ── Helpers ─────────────────────────────────────────────────────────────

  defp schedule_reset(%State{reset_ms: nil} = state), do: state

  defp schedule_reset(%State{reset_ms: ms} = state) when is_integer(ms) do
    ref = Process.send_after(self(), :auto_reset, ms)
    %{state | timer: ref}
  end
end
```

### Step 4: `test/counter_test.exs`

**Objective**: Write `counter_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule CounterTest do
  use ExUnit.Case, async: true

  describe "CounterAgent — basic API parity" do
    test "inc/dec/reset/value behave as expected" do
      {:ok, c} = CounterAgent.start_link()
      assert CounterAgent.value(c) == 0
      CounterAgent.inc(c, 5)
      CounterAgent.dec(c, 2)
      assert CounterAgent.value(c) == 3
      CounterAgent.reset(c)
      assert CounterAgent.value(c) == 0
    end
  end

  describe "CounterGs — API parity" do
    test "inc/dec/reset/value behave as expected" do
      {:ok, c} = CounterGs.start_link()
      assert CounterGs.value(c) == 0
      CounterGs.inc(c, 5)
      CounterGs.dec(c, 2)
      assert CounterGs.value(c) == 3
      CounterGs.reset(c)
      assert CounterGs.value(c) == 0
    end
  end

  describe "CounterGs — features that Agent cannot express" do
    test "auto-resets on a timer" do
      {:ok, c} = CounterGs.start_link(initial: 7, reset_ms: 30)
      assert CounterGs.value(c) == 7
      Process.sleep(60)
      assert CounterGs.value(c) == 0
    end

    test "decrements when a watched client dies" do
      {:ok, c} = CounterGs.start_link(initial: 2)
      client = spawn(fn -> Process.sleep(10) end)
      :ok = CounterGs.watch(c, client)

      # Wait for the client to exit and the DOWN to be processed.
      ref = Process.monitor(client)
      assert_receive {:DOWN, ^ref, :process, ^client, _}, 200
      # Flush through the gen_server mailbox with a call.
      assert CounterGs.value(c) == 1
    end
  end
end
```

### Step 5: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.



## Deep Dive: Task Spawn vs GenServer for Ephemeral Work

A Task is lightweight `spawn/1` for bounded, self-contained work: compute, return, exit. Unlike GenServer (which receives messages indefinitely), Task is inherently ephemeral. This shapes everything: no callbacks, no state management, no back-pressure.

Advantages: simplicity (few lines vs GenServer boilerplate). Disadvantages: no explicit state or message handling—Tasks assume pure computation or simple I/O. If you need a long-lived process responding to external events, you've outgrown Task.

For CPU-bound work (calculations, parsing), Task.Supervisor with `:temporary` is ideal: spawn tasks, let them exit, don't restart. For coordinated async work (multiple tasks handing off results), GenServer + worker tasks often clarifies intent despite more boilerplate. Measure first: if code clarity improves with GenServer, the overhead is justified.

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and decision rules

**1. Use `Agent` when the API fits in three functions**
`get/1`, `put/2`, `update/2` — if that covers your whole protocol, an
`Agent` is strictly less code than a `GenServer`. A config store, a
per-request scratchpad, a test helper: these are agent-shaped.

**2. Use `GenServer` as soon as time or messages enter the picture**
Self-scheduled ticks, monitored clients, port drivers, node notifications,
reacting to `{:DOWN, ...}` or `{:EXIT, ...}` — none of these are
expressible in `Agent`. Starting as a `GenServer` saves the rewrite.

**3. Closures hide your protocol**
`Agent.update(agent, &MyStore.update_user_balance(&1, user_id, amount))`
scatters logic across callsites. A `GenServer.call(pid, {:update_balance,
user_id, amount})` keeps the contract explicit. When operations have
preconditions, concurrency concerns, or telemetry, a documented protocol
pays off.

**4. Performance is usually a tie**
Both run on a single process. Both serialize operations. A `GenServer`
has slightly more dispatch overhead because of the `:"$gen_call"` tag,
but at realistic rates it's noise. Pick based on fit, not
microbenchmarks.

**5. Hot code upgrades favor `GenServer`**
`Agent` stores closures in the state (well, conceptually — it stores
the result). `GenServer` has an explicit `code_change/3` callback. If
you live-upgrade (rare in typical web services, common in long-running
embedded/telecom systems), `GenServer` is the safer home.

**6. When NOT to use either**
- Pure functional data — use a struct passed through functions; processes
  cost memory and add latency.
- Hot read paths — use `:ets` or `:persistent_term`.
- Cross-node shared state — neither works out of the box; reach for
  `:mnesia`, `Horde.Registry`, a CRDT, or a proper database.

---


## Reflection

- Escribí una regla de un párrafo que un dev junior pueda aplicar para decidir Agent vs GenServer sin preguntar.

## Resources

- [`Agent` — Elixir stdlib](https://hexdocs.pm/elixir/Agent.html)
- [`GenServer` — Elixir stdlib](https://hexdocs.pm/elixir/GenServer.html)
- ["Agent vs GenServer" — Elixir school](https://elixirschool.com/en/lessons/intermediate/agent/)
- [Saša Jurić — "The Soul of Erlang and Elixir"](https://www.youtube.com/watch?v=JvBT4XBdoUE) — covers the process mental model underlying both
