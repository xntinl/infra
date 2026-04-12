# Shared config with `Agent`

**Project**: `config_agent` — a tiny key/value store for runtime configuration backed by `Agent`.

---

## Project context

You're building a small service that needs a handful of shared, mutable
settings (feature flags, a rate limit, the current "theme") readable from
many processes and occasionally updatable by an admin. A full `GenServer`
is overkill — there's no request protocol, no side effects, just "hold
some state for me and let me `get`/`update` it".

`Agent` is exactly that: a one-function GenServer where the function is
*your* closure. You get a dedicated process for the state, serialized
updates, and a tiny API (`get`, `update`, `get_and_update`) — nothing else.

This exercise teaches you when `Agent` is the right answer and when it's
the wrong one (spoiler: as soon as you need to react to messages, it's
the wrong one).

Project structure:

```
config_agent/
├── lib/
│   └── config_agent.ex
├── test/
│   └── config_agent_test.exs
└── mix.exs
```

---


## Why X and not Y

- **Why not a lower-level alternative?** For agent basico, OTP's pattern is what reviewers will expect and what observability tools support out of the box.

## Core concepts

### 1. `Agent` is a GenServer with a closure for state

Under the hood, `Agent.start_link(fn -> initial end)` spawns a GenServer
whose only job is to hold a term and run the closures you send it.

```
Agent.get(pid, fn state -> ... end)           # read-only, returns value
Agent.update(pid, fn state -> new_state end)  # writer, returns :ok
Agent.get_and_update(pid, fn s -> {v, s2} end) # both at once
```

The closure runs **inside the agent process**, not the caller. That matters
for two reasons: the caller is blocked until the closure returns, and any
exception inside the closure crashes the agent (not the caller, unless
linked).

### 2. All operations are serialized

An `Agent` processes one message at a time. Two concurrent `update`s
cannot race — they queue and apply in order. This is the whole reason to
use an `Agent` instead of `:ets` or `:persistent_term`: you get
update-as-a-function with consistent state.

### 3. `get` is not a free read

Even a read (`Agent.get/2`) is a message round-trip to the agent process.
If you have hot-path readers at very high frequency, an `Agent` becomes
the bottleneck. For read-heavy shared state, prefer `:ets` (with
`read_concurrency: true`) or `:persistent_term`.

### 4. `Agent` has no `handle_info`, no `handle_cast`, no timers

As soon as you need to react to external events — a timer, a monitor DOWN,
a message from another process — `Agent` stops being the right tool. You
want a `GenServer`. Mixing "holds state" and "reacts to things" in an
`Agent` is the #1 sign you should refactor.

---

## Design decisions

**Option A — a GenServer for shared state**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — an Agent (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because state is trivial and has no logic — Agent's smaller API reduces boilerplate.


## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    # stdlib-only by default; add `{:benchee, "~> 1.3", only: :dev}` if you benchmark
  ]
end
```


### Step 1: Create the project

```bash
mix new config_agent
cd config_agent
```

### Step 2: `lib/config_agent.ex`

```elixir
defmodule ConfigAgent do
  @moduledoc """
  A small key/value configuration store backed by `Agent`.

  Designed for low-frequency reads and writes of process-shared settings
  (feature flags, tunables, the current theme). For high-frequency reads,
  see the trade-offs section and consider `:ets` or `:persistent_term`.
  """

  use Agent

  @type key :: atom() | String.t()
  @type value :: term()

  @doc """
  Starts the agent with an initial map of settings.

  Options:
    * `:name` — optional registered name (defaults to `__MODULE__`).
    * `:initial` — initial map of settings (defaults to `%{}`).
  """
  @spec start_link(keyword()) :: Agent.on_start()
  def start_link(opts \\ []) do
    initial = Keyword.get(opts, :initial, %{})
    name = Keyword.get(opts, :name, __MODULE__)
    Agent.start_link(fn -> initial end, name: name)
  end

  @doc "Returns the value at `key`, or `default` if absent."
  @spec get(Agent.agent(), key(), value()) :: value()
  def get(agent \\ __MODULE__, key, default \\ nil) do
    Agent.get(agent, fn state -> Map.get(state, key, default) end)
  end

  @doc "Returns the full settings map — useful for diagnostics."
  @spec all(Agent.agent()) :: map()
  def all(agent \\ __MODULE__) do
    Agent.get(agent, & &1)
  end

  @doc "Puts `value` at `key`, overwriting any previous value."
  @spec put(Agent.agent(), key(), value()) :: :ok
  def put(agent \\ __MODULE__, key, value) do
    Agent.update(agent, fn state -> Map.put(state, key, value) end)
  end

  @doc """
  Atomically updates `key` by applying `fun` to the current value (or to
  `default` if absent). Returns the new value.
  """
  @spec update(Agent.agent(), key(), value(), (value() -> value())) :: value()
  def update(agent \\ __MODULE__, key, default, fun) when is_function(fun, 1) do
    Agent.get_and_update(agent, fn state ->
      new_value = fun.(Map.get(state, key, default))
      {new_value, Map.put(state, key, new_value)}
    end)
  end

  @doc "Removes `key` from the store."
  @spec delete(Agent.agent(), key()) :: :ok
  def delete(agent \\ __MODULE__, key) do
    Agent.update(agent, fn state -> Map.delete(state, key) end)
  end
end
```

### Step 3: `test/config_agent_test.exs`

```elixir
defmodule ConfigAgentTest do
  use ExUnit.Case, async: true

  setup do
    # Give each test its own un-named agent to keep `async: true` viable.
    {:ok, agent} = ConfigAgent.start_link(name: nil, initial: %{theme: :dark})
    %{agent: agent}
  end

  describe "get/3 and put/3" do
    test "returns the seeded value", %{agent: agent} do
      assert ConfigAgent.get(agent, :theme) == :dark
    end

    test "returns the default when the key is missing", %{agent: agent} do
      assert ConfigAgent.get(agent, :missing, :fallback) == :fallback
    end

    test "overwrites existing values", %{agent: agent} do
      :ok = ConfigAgent.put(agent, :theme, :light)
      assert ConfigAgent.get(agent, :theme) == :light
    end
  end

  describe "update/4 — atomic read-modify-write" do
    test "applies the function to the current value", %{agent: agent} do
      :ok = ConfigAgent.put(agent, :counter, 10)
      assert ConfigAgent.update(agent, :counter, 0, &(&1 + 1)) == 11
      assert ConfigAgent.get(agent, :counter) == 11
    end

    test "uses the default when the key is missing", %{agent: agent} do
      assert ConfigAgent.update(agent, :new_key, 5, &(&1 * 2)) == 10
    end

    test "concurrent updates are serialized", %{agent: agent} do
      :ok = ConfigAgent.put(agent, :n, 0)

      # 100 processes each incrementing — if we had a race, we'd lose some.
      1..100
      |> Enum.map(fn _ ->
        Task.async(fn -> ConfigAgent.update(agent, :n, 0, &(&1 + 1)) end)
      end)
      |> Task.await_many(1_000)

      assert ConfigAgent.get(agent, :n) == 100
    end
  end

  describe "delete/2" do
    test "removes the key", %{agent: agent} do
      :ok = ConfigAgent.delete(agent, :theme)
      assert ConfigAgent.get(agent, :theme, :absent) == :absent
    end
  end

  describe "all/1" do
    test "returns the full map", %{agent: agent} do
      assert ConfigAgent.all(agent) == %{theme: :dark}
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.


## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `Agent` serializes everything — even reads**
Every `get/update` is a message to the agent process. Under heavy read
load, that single process becomes a bottleneck. For hot read paths (e.g.
per-request feature-flag lookups at 10k rps), use `:ets` with
`read_concurrency: true` or `:persistent_term` for truly read-mostly data.

**2. Closures run inside the agent — don't do slow work there**
`Agent.update(agent, &expensive_computation/1)` blocks every other caller
until it returns. Compute first, then update with a small closure:

```elixir
result = expensive_computation(params)
Agent.update(agent, &Map.put(&1, :result, result))
```

**3. A crash in the closure crashes the agent**
If the function raises, the agent dies. If you used `start_link`, callers
linked to it may die too. Validate inputs **before** calling `update`.

**4. `Agent` cannot receive timers, monitors, or arbitrary messages**
There's no `handle_info` hook. If you need periodic cleanup, a TTL, or
reactions to external events, you've outgrown `Agent` — switch to
`GenServer`. Exercise 49 (`agent_vs_gs`) walks through that trade-off.

**5. Named agents break `async: true` test isolation**
A named agent (`name: __MODULE__`) is global. Two tests running in parallel
will stomp each other. In tests, start anonymous agents (`name: nil`) and
pass the pid through the context.

**6. When NOT to use `Agent`**
- Read-heavy hot paths → `:ets`/`:persistent_term`.
- Anything that needs timers, `handle_info`, or a protocol → `GenServer`.
- State shared across nodes → `Agent` is node-local; use `:mnesia`, a
  CRDT, or a proper database.

---


## Reflection

- Si agregás validación y side-effects al Agent, ¿cuándo sabés que es momento de migrar a GenServer? Dá el criterio concreto.

## Resources

- [`Agent` — Elixir stdlib](https://hexdocs.pm/elixir/Agent.html)
- ["Agent" — Elixir getting started](https://hexdocs.pm/elixir/agents.html)
- [`:ets` — Erlang](https://www.erlang.org/doc/man/ets.html) — when you need concurrent reads
- [`:persistent_term`](https://www.erlang.org/doc/man/persistent_term.html) — for rarely-updated, read-almost-always data
