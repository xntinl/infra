# Designing a GenServer for testability with Mox

**Project**: `mockable_gs` — a GenServer that depends on an external behaviour, mocked with `Mox` in tests.

---

## Why testing with mox matters

Your GenServer talks to "the outside world" — an HTTP API, a payment
gateway, an SMS provider, a time source. Unit tests that actually hit
those systems are slow, flaky, and dangerous (you don't want "charge
$5" running in CI). The standard Elixir answer is `Mox`: you define a
`behaviour`, the real and mock implementations both conform to it, and
tests swap in the mock via application config.

The critical design move is **dependency injection at module boundaries
via Application config**. The GenServer does not hardcode `HTTPClient`;
it looks up `Application.get_env(:my_app, :http_client)`, which resolves
to the real module in production and the Mox mock in tests. This
exercise builds that shape end-to-end: behaviour, real implementation,
GenServer depending on the behaviour, and `Mox`-based tests.

---

## Project structure

```
mockable_gs/
├── lib/
│   └── mockable_gs.ex
├── script/
│   └── main.exs
├── test/
│   └── mockable_gs_test.exs
└── mix.exs
```

---

## Why X and not Y

- **Why not Meck?** Meck patches modules at runtime; Mox uses explicit behaviours, compile-time checked, concurrency-safe.

## Core concepts

### 1. Behaviours define the seam

A `@behaviour` module declares the contract every implementation must
satisfy. The real module implements it with real side effects; the mock
implements it with whatever the test wants to return. Callers depend on
the behaviour, not on either implementation.

```elixir
@callback now() :: integer()
```

### 2. Application config = runtime injection

```elixir
defp clock, do: Application.get_env(:mockable_gs, :clock, MockableGs.SystemClock)
```

In `config/config.exs` the real module is wired up; in `config/test.exs`
the mock is wired up. Code never references either module directly
outside config.

### 3. `Mox` is compile-time-safe mocking

`Mox.defmock/2` generates a module that implements a behaviour and
lets you set expectations in tests. It is not a metaprogramming monkey
patch — it is a concrete module that raises if the test doesn't set up
the expected calls, preventing the "my mock silently returned nil"
failure mode of looser tools.

### 4. `set_mox_from_context: :global` vs. `private`

Default is `:private` — expectations live in the current test process.
If your GenServer runs in a different process (which it does!), the
mock must be usable by *that* process too. Two solutions:

- `Mox.set_mox_global()` in the test (or `use MyCase, :global`) —
  simple but forces `async: false`.
- `Mox.allow(Mock, test_pid, genserver_pid)` — per-test fine-grained
  allow, keeps `async: true`.

This exercise uses the global approach for simplicity; production
codebases usually prefer `allow/3`.

---

## Design decisions

**Option A — in-process stubs**
- Pros: simpler upfront, fewer moving parts.
- Cons: hides the trade-off that this exercise exists to teach.

**Option B — Mox-backed behaviour mocks (chosen)**
- Pros: explicit about the semantic that matters in production.
- Cons: one more concept to internalize.

→ Chose **B** because explicit behaviour contracts + verify-on-exit prevent silent test drift.

## Implementation

### `mix.exs`

```elixir
defmodule MockableGs.MixProject do
  use Mix.Project

  def project do
    [
      app: :mockable_gs,
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
mix new mockable_gs
cd mockable_gs
```

Add `mox` to `mix.exs` deps:

### `lib/mockable_gs/clock.ex`

**Objective**: Implement `clock.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
defmodule MockableGs.Clock do
  @moduledoc """
  Behaviour for a time source. The GenServer depends on this — not on
  any concrete module — so tests can substitute a deterministic clock.
  """

  @doc "Returns the current unix time in seconds."
  @callback now() :: integer()
end
```

### `lib/mockable_gs/system_clock.ex`

**Objective**: Implement `system_clock.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
defmodule MockableGs.SystemClock do
  @moduledoc "Production implementation of `MockableGs.Clock` using `System.os_time/1`."

  @behaviour MockableGs.Clock

  @impl true
  def now, do: System.os_time(:second)
end
```

### `lib/mockable_gs.ex`

**Objective**: Implement `mockable_gs.ex` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
defmodule MockableGs do
  @moduledoc """
  A GenServer that records timestamped events. Depends on a `Clock`
  behaviour resolved from application config so tests can inject a mock.
  """

  use GenServer

  defmodule State do
    @moduledoc false
    defstruct events: []
    @type t :: %__MODULE__{events: [{integer(), term()}]}
  end

  # ── Public API ──────────────────────────────────────────────────────────

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, :ok, opts)

  @doc "Records an event with the current timestamp from the configured clock."
  @spec record(GenServer.server(), term()) :: :ok
  def record(server, event), do: GenServer.call(server, {:record, event})

  @doc "Returns all events as `[{ts, event}, ...]` in reverse-chronological order."
  @spec events(GenServer.server()) :: [{integer(), term()}]
  def events(server), do: GenServer.call(server, :events)

  # ── Callbacks ───────────────────────────────────────────────────────────

  @impl true
  def init(:ok), do: {:ok, %State{}}

  @impl true
  def handle_call({:record, event}, _from, %State{events: evs} = state) do
    ts = clock().now()
    {:reply, :ok, %{state | events: [{ts, event} | evs]}}
  end

  def handle_call(:events, _from, %State{events: evs} = state) do
    {:reply, evs, state}
  end

  # ── Dependency resolution ───────────────────────────────────────────────

  # Resolved per-call so tests can override in `Application.put_env/3` without
  # restarting the server. Cheap — one ETS lookup.
  defp clock, do: Application.get_env(:mockable_gs, :clock, MockableGs.SystemClock)
end
```

### Step 5: `test/test_helper.exs`

**Objective**: Implement `test_helper.exs` — the GenServer callback shape that determines blocking vs fire-and-forget semantics and state invariants.

```elixir
ExUnit.start()

# Define the mock once, at test suite start. `ClockMock` implements
# the `MockableGs.Clock` behaviour.
Mox.defmock(MockableGs.ClockMock, for: MockableGs.Clock)

# Wire the application to use the mock in tests.
Application.put_env(:mockable_gs, :clock, MockableGs.ClockMock)
```

### Step 6: `test/mockable_gs_test.exs`

**Objective**: Write `mockable_gs_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule MockableGsTest do
  # Global mocks + GenServer in another process → async: false.
  use ExUnit.Case, async: false

  doctest MockableGs
  import Mox

  # Verify that every `expect` set in a test actually runs. If a test says
  # "the clock will be called twice" and only one call happens, we fail.
  setup :verify_on_exit!

  # The GenServer is spawned by start_link and runs in its own process.
  # Without this, the mock is invisible to the server and calls blow up.
  setup :set_mox_global

  setup do
    {:ok, pid} = MockableGs.start_link()
    %{pid: pid}
  end

  describe "record/2 with a mocked clock" do
    test "uses the injected clock to timestamp events", %{pid: pid} do
      # Freeze time: every now/0 returns 1_700_000_000.
      MockableGs.ClockMock
      |> expect(:now, 2, fn -> 1_700_000_000 end)

      :ok = MockableGs.record(pid, :login)
      :ok = MockableGs.record(pid, :logout)

      assert [{1_700_000_000, :logout}, {1_700_000_000, :login}] =
               MockableGs.events(pid)
    end

    test "distinct timestamps from a sequence of clock values", %{pid: pid} do
      # Feed three different times in order.
      times = [1_000, 1_005, 1_010]
      {:ok, agent} = Agent.start_link(fn -> times end)

      MockableGs.ClockMock
      |> expect(:now, 3, fn ->
        Agent.get_and_update(agent, fn [h | t] -> {h, t} end)
      end)

      for event <- [:a, :b, :c], do: :ok = MockableGs.record(pid, event)

      assert [{1_010, :c}, {1_005, :b}, {1_000, :a}] = MockableGs.events(pid)
    end
  end

  describe "no clock calls when not asked" do
    test "reading events does not consult the clock", %{pid: pid} do
      # No expectations set — `verify_on_exit!` will fail the test if the
      # server invokes the clock unexpectedly.
      assert [] = MockableGs.events(pid)
    end
  end
end
```

### Step 7: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix deps.get
mix test
```

---

### Why this works

The design leans on OTP primitives that already encode the invariants we care about (supervision, back-pressure, explicit message semantics), so failure modes are visible at the right layer instead of being reinvented ad-hoc. Tests exercise the edges (timeouts, crashes, boundary states), which is where hand-rolled alternatives silently drift over time.

### `script/main.exs`

```elixir
defmodule Main do
  defmodule MockableGs.SystemClock do
    @moduledoc "Production implementation of `MockableGs.Clock` using `System.os_time/1`."

    @behaviour MockableGs.Clock

    @impl true
    def now, do: System.os_time(:second)
  end

  defmodule MockableGs.Clock do
    @callback now() :: integer()
  end

  defmodule MockableGs do
    use GenServer
  
    defmodule State do
      defstruct [:events]
    end
  
    def start_link, do: GenServer.start_link(__MODULE__, :ok)
    def record(server, event), do: GenServer.call(server, {:record, event})
    def events(server), do: GenServer.call(server, :events)
  
    def init(:ok), do: {:ok, %State{events: []}}
    def handle_call({:record, event}, _from, %State{events: evs} = state) do
      ts = System.os_time(:second)
      {:reply, :ok, %{state | events: [{ts, event} | evs]}}
    end
    def handle_call(:events, _from, %State{events: evs} = state) do
      {:reply, evs, state}
    end
  end

  def main do
    {:ok, pid} = MockableGs.start_link()
    :ok = MockableGs.record(pid, :event1)
    :ok = MockableGs.record(pid, :event2)
    evs = MockableGs.events(pid)
    IO.puts("Recorded #{length(evs)} events")
    IO.puts("✓ MockableGs works correctly")
  end

end

Main.main()
```

## Benchmark

<!-- benchmark N/A: tema conceptual -->

## Trade-offs and production gotchas

**1. `set_mox_global` forces `async: false`**
Global mocks leak across concurrent tests. If you want `async: true`,
use `Mox.allow(Mock, test_pid, genserver_pid)` inside the test. It's
more ceremony but parallel tests are much faster in a large suite.

**2. The seam is the behaviour, not Mox**
Mox is a tool; the architecture decision is "depend on a behaviour via
config". With that seam in place you can swap Mox for a hand-written
stub, a Bypass HTTP server, or a real sandbox. Without it, no mocking
library saves you.

**3. Don't mock what you don't own**
Define the behaviour at YOUR boundary, not the library's. Wrap
`HTTPoison` in `MyApp.HttpClient` with your own `@callback` shape, then
mock `MyApp.HttpClient`. Mocking the library's API directly ties your
tests to its internals and breaks on library upgrades.

**4. `verify_on_exit!` is non-negotiable**
Without it, a test can silently set expectations the server never hits,
and the green check lies to you. Always add it in `setup`. Combined
with `:global`/`allow`, it catches both missing and extra interactions.

**5. One behaviour per collaborator, not one megabehaviour**
A behaviour with 30 callbacks is unmockable — each test has to stub or
ignore all of them. Split it: one behaviour per cohesive collaboration
(one for the clock, one for the HTTP client, one for the storage). Each
mock then has a handful of callbacks.

**6. When NOT to use Mox**
For pure functions, don't mock — test them directly with real inputs.
For process-boundary checks (did we send the right message?), use
`assert_receive` with a test-process relay. For database code, use
`Ecto.Adapters.SQL.Sandbox`. Mox is specifically for behaviour-typed
external collaborators.

---

## Reflection

- Si las behaviours cambian cada sprint, ¿Mox sigue valiendo la pena o caes en integración? Definí el punto de inflexión.

## Resources

- [`Mox` — library docs](https://hexdocs.pm/mox/)
- [José Valim, "Mocks and explicit contracts"](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/) — the article that established the pattern
- [`Behaviour` in Elixir](https://hexdocs.pm/elixir/typespecs.html#behaviours)
- [`Application` config at runtime](https://hexdocs.pm/elixir/Application.html#get_env/3)
- Saša Jurić, *Elixir in Action* — section on testing OTP code

## Advanced Considerations

GenServer is the foundation of stateful concurrent systems in Elixir. Advanced patterns emerge from understanding the synchronous/asynchronous nature of callbacks and state evolution.

**State evolution and message handling:**
A GenServer's state is private, evolving only through synchronous (`handle_call`) or asynchronous (`handle_cast`) message handlers. The key insight: `handle_call` blocks the caller until the handler returns; `handle_cast` is fire-and-forget. Use `call` for operations requiring acknowledgment or returning results; use `cast` for notifications. Mixing them incorrectly leads to deadlocks (caller waiting forever) or lost updates (state changed before caller knows).

**Advanced reply patterns:**
The tuple `{:reply, reply, state}` is the standard, but you can split reply and state persistence. Use `:noreply` in `handle_call` if you need to send the reply later (e.g., after an async operation). The `:hibernate` flag tells the VM to garbage-collect the process and switch to a lightweight state — useful for long-lived processes that spend time idle.

**Debugging and observability:**
`format_status/2` controls how a GenServer appears in `:observer` and logs. It's critical for large state structures (hide sensitive fields, summarize collections). In production, comprehensive logging in callbacks (not just errors) reveals timing issues, message flow anomalies, and resource leaks before they become critical.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/mockable_gs_test.exs`

```elixir
defmodule MockableGsTest do
  use ExUnit.Case, async: true

  doctest MockableGs

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MockableGs.run(:noop) == :ok
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
