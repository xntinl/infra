# Designing a GenServer for testability with Mox

**Project**: `mockable_gs` — a GenServer that depends on an external behaviour, mocked with `Mox` in tests.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

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

Project structure:

```
mockable_gs/
├── lib/
│   ├── mockable_gs.ex
│   ├── mockable_gs/clock.ex
│   └── mockable_gs/system_clock.ex
├── test/
│   ├── mockable_gs_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

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

## Implementation

### Step 1: Create the project

```bash
mix new mockable_gs
cd mockable_gs
```

Add `mox` to `mix.exs` deps:

```elixir
defp deps do
  [{:mox, "~> 1.0", only: :test}]
end
```

### Step 2: `lib/mockable_gs/clock.ex`

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

### Step 3: `lib/mockable_gs/system_clock.ex`

```elixir
defmodule MockableGs.SystemClock do
  @moduledoc "Production implementation of `MockableGs.Clock` using `System.os_time/1`."

  @behaviour MockableGs.Clock

  @impl true
  def now, do: System.os_time(:second)
end
```

### Step 4: `lib/mockable_gs.ex`

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

```elixir
ExUnit.start()

# Define the mock once, at test suite start. `ClockMock` implements
# the `MockableGs.Clock` behaviour.
Mox.defmock(MockableGs.ClockMock, for: MockableGs.Clock)

# Wire the application to use the mock in tests.
Application.put_env(:mockable_gs, :clock, MockableGs.ClockMock)
```

### Step 6: `test/mockable_gs_test.exs`

```elixir
defmodule MockableGsTest do
  # Global mocks + GenServer in another process → async: false.
  use ExUnit.Case, async: false
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

```bash
mix deps.get
mix test
```

---

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

## Resources

- [`Mox` — library docs](https://hexdocs.pm/mox/)
- [José Valim, "Mocks and explicit contracts"](http://blog.plataformatec.com.br/2015/10/mocks-and-explicit-contracts/) — the article that established the pattern
- [`Behaviour` in Elixir](https://hexdocs.pm/elixir/typespecs.html#behaviours)
- [`Application` config at runtime](https://hexdocs.pm/elixir/Application.html#get_env/3)
- Saša Jurić, *Elixir in Action* — section on testing OTP code
