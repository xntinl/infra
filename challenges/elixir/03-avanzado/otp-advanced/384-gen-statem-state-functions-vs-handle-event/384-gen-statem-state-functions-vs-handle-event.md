# `:gen_statem` — State Functions vs `handle_event_function`

**Project**: `door_controller` — smart-lock state machine implemented twice, once with `:state_functions` callback mode and once with `:handle_event_function`, to compare ergonomics, code locality, and the cost of each style.

## Project context

`:gen_statem` is OTP's state-machine behaviour. It is a better fit than `GenServer` whenever state transitions are the essence of the code — authentication protocols, connection managers, checkout flows. It gives you state timeouts, postponed events, entry actions, and reply queues, without the hand-rolled `{:handle_call, %{state: :ringing} = s}` pattern explosion that GenServers fall into.

`:gen_statem` has two canonical callback modes:

- `:state_functions` — each state is its own function clause: `locked(:call, {:unlock, pin}, data)`. Dispatch is by function name.
- `:handle_event_function` — a single callback handles every event for every state: `handle_event(:call, {:unlock, pin}, :locked, data)`. Dispatch is inside the function.

Everyone picks one without understanding the trade-off. This exercise implements the *same* door-controller state machine in both styles and benchmarks dispatch cost.

```
door_controller/
├── lib/
│   └── door_controller/
│       ├── application.ex
│       ├── door_sf.ex             # state_functions mode
│       ├── door_hef.ex            # handle_event_function mode
│       └── pin.ex                 # shared PIN validator
├── test/
│   └── door_controller/
│       ├── door_sf_test.exs
│       └── door_hef_test.exs
├── bench/
│   └── mode_bench.exs
└── mix.exs
```

## Why `:gen_statem` and not `GenServer`

A `GenServer` can implement a state machine by keeping `state.mode` as a field and pattern-matching in every callback. That works for two or three states. At five states with timeouts and reentrant transitions, it collapses into pattern spaghetti. `:gen_statem` offers, out of the box:

- **State timeouts** — `{:state_timeout, 30_000, :auto_lock}` delivered only while in a specific state.
- **Generic timeouts** — many, named, survive transitions if you want.
- **Postponed events** — `:postpone` an event; it will be redelivered after the next transition.
- **Reply actions** — `{:reply, From, msg}` returned from any clause, so you can transition *and* reply in one step.
- **Enter calls** — a callback that fires whenever you enter a state, independent of which event got you there.

## Core concepts

### 1. Callback mode
`:state_functions` (one function per state) vs `:handle_event_function` (one function, multi-clause by state).

### 2. Event type
`:call`, `:cast`, `:info`, `:internal`, `:state_timeout`, `:generic_timeout`, `:timeout`.

### 3. Action
A return instruction: `{:reply, From, reply}`, `{:state_timeout, time, msg}`, `:postpone`, `:hibernate`, etc.

### 4. Data
The equivalent of GenServer `state`. State is an atom; data is the map/struct.

### 5. Enter events
With `[:state_enter]` in callback modes, a synthetic `:enter` event fires on every state change; useful for timeouts and logging.

## Design decisions

- **Option A — `:state_functions`**: each state's behaviour is co-located in one function; easy to read at a glance; the compiler enforces state-name/function-name equivalence.
- **Option B — `:handle_event_function`**: every event in one place; easy to implement "for all states do X"; easier composition (macros, helpers).

→ There is no universal winner. State functions win for small, well-bounded FSMs with clear state locality (auth flows, connection handshakes). `handle_event_function` wins when lots of events behave identically across states, or you want to share logic via macros.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: [:dev, :test]}
  ]
end
```

### Step 1: Shared PIN validator

```elixir
defmodule DoorController.Pin do
  @valid_pin "1234"

  def valid?(pin), do: pin == @valid_pin
end
```

### Step 2: `:state_functions` implementation

```elixir
defmodule DoorController.DoorSF do
  @behaviour :gen_statem

  alias DoorController.Pin

  @auto_lock_ms 30_000
  @lockout_ms 10_000

  defstruct failed_attempts: 0

  # --- public API ---

  def start_link(name) do
    :gen_statem.start_link({:local, name}, __MODULE__, [], [])
  end

  def unlock(name, pin), do: :gen_statem.call(name, {:unlock, pin})
  def lock(name), do: :gen_statem.call(name, :lock)
  def state(name), do: :sys.get_state(name)

  # --- :gen_statem callbacks ---

  @impl true
  def callback_mode, do: [:state_functions, :state_enter]

  @impl true
  def init(_), do: {:ok, :locked, %__MODULE__{}}

  # === locked state ===

  def locked(:enter, _old_state, data), do: {:keep_state, data}

  def locked({:call, from}, {:unlock, pin}, data) do
    if Pin.valid?(pin) do
      {:next_state, :unlocked, %{data | failed_attempts: 0}, [{:reply, from, :ok}]}
    else
      attempts = data.failed_attempts + 1

      if attempts >= 3 do
        {:next_state, :locked_out, %{data | failed_attempts: 0},
         [{:reply, from, {:error, :locked_out}}]}
      else
        {:keep_state, %{data | failed_attempts: attempts},
         [{:reply, from, {:error, :bad_pin}}]}
      end
    end
  end

  def locked({:call, from}, :lock, data),
    do: {:keep_state, data, [{:reply, from, :ok}]}

  # === unlocked state ===

  def unlocked(:enter, _old_state, data),
    do: {:keep_state, data, [{:state_timeout, @auto_lock_ms, :auto_lock}]}

  def unlocked({:call, from}, :lock, data),
    do: {:next_state, :locked, data, [{:reply, from, :ok}]}

  def unlocked({:call, from}, {:unlock, _}, data),
    do: {:keep_state, data, [{:reply, from, :already_unlocked}]}

  def unlocked(:state_timeout, :auto_lock, data),
    do: {:next_state, :locked, data}

  # === locked_out state ===

  def locked_out(:enter, _old_state, data),
    do: {:keep_state, data, [{:state_timeout, @lockout_ms, :release}]}

  def locked_out({:call, from}, _event, data),
    do: {:keep_state, data, [{:reply, from, {:error, :locked_out}}]}

  def locked_out(:state_timeout, :release, data),
    do: {:next_state, :locked, data}
end
```

### Step 3: `:handle_event_function` implementation

```elixir
defmodule DoorController.DoorHEF do
  @behaviour :gen_statem

  alias DoorController.Pin

  @auto_lock_ms 30_000
  @lockout_ms 10_000

  defstruct failed_attempts: 0

  def start_link(name), do: :gen_statem.start_link({:local, name}, __MODULE__, [], [])
  def unlock(name, pin), do: :gen_statem.call(name, {:unlock, pin})
  def lock(name), do: :gen_statem.call(name, :lock)

  @impl true
  def callback_mode, do: [:handle_event_function, :state_enter]

  @impl true
  def init(_), do: {:ok, :locked, %__MODULE__{}}

  # --- enter events (centralised in one place) ---

  @impl true
  def handle_event(:enter, _old, :unlocked, data),
    do: {:keep_state, data, [{:state_timeout, @auto_lock_ms, :auto_lock}]}

  def handle_event(:enter, _old, :locked_out, data),
    do: {:keep_state, data, [{:state_timeout, @lockout_ms, :release}]}

  def handle_event(:enter, _old, _state, data), do: {:keep_state, data}

  # --- locked ---

  def handle_event({:call, from}, {:unlock, pin}, :locked, data) do
    if Pin.valid?(pin) do
      {:next_state, :unlocked, %{data | failed_attempts: 0}, [{:reply, from, :ok}]}
    else
      attempts = data.failed_attempts + 1

      if attempts >= 3 do
        {:next_state, :locked_out, %{data | failed_attempts: 0},
         [{:reply, from, {:error, :locked_out}}]}
      else
        {:keep_state, %{data | failed_attempts: attempts},
         [{:reply, from, {:error, :bad_pin}}]}
      end
    end
  end

  def handle_event({:call, from}, :lock, :locked, data),
    do: {:keep_state, data, [{:reply, from, :ok}]}

  # --- unlocked ---

  def handle_event({:call, from}, :lock, :unlocked, data),
    do: {:next_state, :locked, data, [{:reply, from, :ok}]}

  def handle_event({:call, from}, {:unlock, _}, :unlocked, data),
    do: {:keep_state, data, [{:reply, from, :already_unlocked}]}

  def handle_event(:state_timeout, :auto_lock, :unlocked, data),
    do: {:next_state, :locked, data}

  # --- locked_out (absorb everything) ---

  def handle_event({:call, from}, _event, :locked_out, data),
    do: {:keep_state, data, [{:reply, from, {:error, :locked_out}}]}

  def handle_event(:state_timeout, :release, :locked_out, data),
    do: {:next_state, :locked, data}
end
```

## State diagram

```
           ┌───────────────────────┐
           │        locked         │
           │  failed_attempts=N    │
           └──┬───────────────┬────┘
              │ unlock(ok)    │ unlock(bad, attempts=3)
              ▼               ▼
      ┌─────────────┐    ┌──────────────────┐
      │  unlocked   │    │   locked_out     │
      │ state_timeout─▶  │  state_timeout──▶│
      │ :auto_lock       │  :release        │
      └──────┬──────┘    └──────────────────┘
             │ lock
             ▼
          locked
```

## Tests

```elixir
defmodule DoorController.DoorSFTest do
  use ExUnit.Case, async: false
  alias DoorController.DoorSF

  setup do
    name = :"door_sf_#{:erlang.unique_integer([:positive])}"
    {:ok, _pid} = DoorSF.start_link(name)
    {:ok, name: name}
  end

  describe "unlock/2" do
    test "valid PIN unlocks", %{name: name} do
      assert :ok = DoorSF.unlock(name, "1234")
      assert {:unlocked, _} = DoorSF.state(name)
    end

    test "bad PIN increments attempts", %{name: name} do
      assert {:error, :bad_pin} = DoorSF.unlock(name, "0000")
      assert {:locked, %{failed_attempts: 1}} = DoorSF.state(name)
    end

    test "3 bad PINs transitions to locked_out", %{name: name} do
      for _ <- 1..2, do: DoorSF.unlock(name, "0000")
      assert {:error, :locked_out} = DoorSF.unlock(name, "0000")
      assert {:locked_out, _} = DoorSF.state(name)
    end
  end

  describe "locked_out absorbs input" do
    test "even a valid PIN is rejected", %{name: name} do
      for _ <- 1..3, do: DoorSF.unlock(name, "0000")
      assert {:error, :locked_out} = DoorSF.unlock(name, "1234")
    end
  end
end
```

```elixir
defmodule DoorController.DoorHEFTest do
  use ExUnit.Case, async: false
  alias DoorController.DoorHEF

  setup do
    name = :"door_hef_#{:erlang.unique_integer([:positive])}"
    {:ok, _pid} = DoorHEF.start_link(name)
    {:ok, name: name}
  end

  describe "parity with DoorSF" do
    test "unlock → lock flow", %{name: name} do
      assert :ok = DoorHEF.unlock(name, "1234")
      assert :ok = DoorHEF.lock(name)
    end

    test "unlock twice returns :already_unlocked", %{name: name} do
      :ok = DoorHEF.unlock(name, "1234")
      assert :already_unlocked = DoorHEF.unlock(name, "1234")
    end
  end
end
```

## Benchmark

```elixir
# bench/mode_bench.exs
{:ok, _} = DoorController.DoorSF.start_link(:bench_sf)
{:ok, _} = DoorController.DoorHEF.start_link(:bench_hef)

Benchee.run(
  %{
    "state_functions"      => fn -> DoorController.DoorSF.unlock(:bench_sf, "1234") end,
    "handle_event_function" => fn -> DoorController.DoorHEF.unlock(:bench_hef, "1234") end
  },
  time: 5,
  warmup: 2
)
```

Expected result on OTP 27+ on modern hardware: the two modes are within 3% of each other (the BEAM compiler generates similar dispatch code). If you see more than 10% difference, you have a bug. The **cost is in code shape, not CPU cycles** — choose based on readability.

## Trade-offs and production gotchas

**1. `:state_enter` is opt-in**
Without `[:state_enter]` in `callback_mode/0`, entering a state fires no event. Every state timeout you want must be set from whatever event triggered the transition, duplicated everywhere. Always opt into `:state_enter` unless you really don't need it.

**2. `:postpone` queue growth**
`:postpone` defers an event until the next transition. If transitions are rare and events flow fast, the queue grows without bound. Always bound how many events you postpone.

**3. State timeouts are cancelled on state change**
A `:state_timeout` set in state X is automatically cancelled when the machine leaves X. That is usually what you want. If you need a timer that survives transitions, use `:generic_timeout` with a name.

**4. `sys.get_state/1` returns `{state, data}`**
Tests often expect just `data`. Be explicit.

**5. `handle_event/4` clause order matters**
In `:handle_event_function`, clauses are tried top-down. Put `:enter` handlers first, then specific `(event, state)` pairs, then a catch-all last to avoid accidental fallthrough.

**6. When NOT to use `:gen_statem`**
If your process has 1–2 states and the transitions are trivial, a GenServer is clearer. `:gen_statem` pays off starting around 4 states or when timeouts are central.

## Reflection

You implemented the same machine twice. Which file would you rather revisit six months from now? Which would you rather extend with a new `maintenance_mode` state? The real question `:state_functions` vs `:handle_event_function` asks is: *does your machine organise around states (each state knows what it can do) or around events (each event knows how to dispatch)?* Pick based on the grain of change.

## Resources

- [`:gen_statem` reference](https://www.erlang.org/doc/man/gen_statem.html)
- [`:gen_statem` user's guide](https://www.erlang.org/doc/design_principles/statem.html) — read it end to end
- [Fred Hebert — `gen_statem` deep dive (Learn You Some Erlang)](https://learnyousomeerlang.com/) — chapter on FSMs
