# State Machines with `:gen_statem`

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` needs a circuit breaker to protect downstream microservices. The circuit
breaker has exactly three states (`:closed`, `:open`, `:half_open`) whose behavior is
radically different. Modeling this as a GenServer with `if state == :open do ...` guards
scattered across handlers is error-prone and hard to reason about. You will use `:gen_statem`
to make state explicit and transitions first-class.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── application.ex
│       ├── router.ex
│       ├── rate_limiter/
│       └── circuit_breaker/
│           ├── breaker.ex          # ← you implement this
│           └── supervisor.ex       # already exists
├── test/
│   └── api_gateway/
│       └── circuit_breaker/
│           └── breaker_test.exs    # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The payments microservice is occasionally slow. When it fails, all requests to the gateway
that need payment validation block for 30 seconds waiting for a timeout, exhausting the
connection pool and cascading into a full gateway outage.

The circuit breaker must:
1. Track failures in `:closed` state
2. Open the circuit (fast-fail without calling payments) after N consecutive failures
3. After a recovery timeout, let through one probe request in `:half_open`
4. Close the circuit on probe success; re-open on probe failure

This behavior is fundamentally state-dependent: the same event (`{:call, fun}`) has three
different responses depending on the current state.

---

## Why `:gen_statem` and not GenServer

```elixir
# GenServer approach — state leaks into every handler
def handle_call({:execute, fun}, from, %{state: :open} = s) do
  {:reply, {:error, :circuit_open}, s}
end
def handle_call({:execute, fun}, from, %{state: :closed} = s) do
  # count failures, maybe transition...
end
def handle_call({:execute, fun}, from, %{state: :half_open} = s) do
  # single probe...
end
```

With `:gen_statem`, the *current state is the function that handles events*:

```elixir
# Every handler for :closed is in the `closed/3` function
def closed({:call, from}, {:execute, fun}, data) -> ...

# Every handler for :open is in `open/3`
def open({:call, from}, {:execute, _fun}, data) ->
  {:keep_state, data, [{:reply, from, {:error, :circuit_open}}]}
```

State-specific behavior is co-located with the state definition. Adding a new state means
adding a new function, not modifying every existing handler.

---

## Callback modes

**`state_functions`**: each state is a separate function. Best when states have mostly
independent logic.

```elixir
def callback_mode, do: :state_functions

def closed(:cast, :ping, data), do: {:keep_state, data}
def open(:cast, :ping, data),   do: {:keep_state, data}
```

**`handle_event_function`**: one `handle_event/4` with pattern matching. Best when the
same event is handled identically across many states, or when guards span multiple states.

```elixir
def callback_mode, do: :handle_event_function

def handle_event(:cast, :reset, _state, data), do: {:next_state, :closed, %{data | failures: 0}}
# This handles :reset the same way regardless of state — one clause, no duplication
```

---

## State timeout

```elixir
# Attached to a state — cancelled automatically when the state changes
{:next_state, :open, data, [{:state_timeout, 10_000, :recovery_tick}]}

# Received as:
def open(:state_timeout, :recovery_tick, data) -> {:next_state, :half_open, data}
```

No manual cancellation needed: when `:half_open` transitions back to `:closed`, the
`:open` state timeout is gone. This is the key advantage over `Process.send_after` in
GenServer — the timer is scoped to the state, not the process.

---

## Implementation

### Step 1: `lib/api_gateway/circuit_breaker/breaker.ex`

```elixir
defmodule ApiGateway.CircuitBreaker.Breaker do
  @moduledoc """
  Circuit breaker implemented as a :gen_statem state machine.

  States:
    :closed   — normal operation; counts consecutive failures
    :open     — fast-fail; waits for recovery_timeout, then → :half_open
    :half_open — allows one probe; success → :closed, failure → :open

  Usage:
    {:ok, cb} = Breaker.start_link(name: :payments_cb, threshold: 5, timeout: 10_000)
    Breaker.call(cb, fn -> PaymentsClient.charge(amount) end)
  """

  @behaviour :gen_statem

  defstruct [:name, :threshold, :timeout_ms, failures: 0]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts) do
    name       = Keyword.fetch!(opts, :name)
    threshold  = Keyword.get(opts, :threshold, 5)
    timeout_ms = Keyword.get(opts, :timeout, 10_000)

    data = %__MODULE__{name: name, threshold: threshold, timeout_ms: timeout_ms}
    :gen_statem.start_link({:local, name}, __MODULE__, data, [])
  end

  @spec call(GenServer.server(), (-> term())) ::
          {:ok, term()} | {:error, :circuit_open} | {:error, term()}
  def call(cb, fun) when is_function(fun, 0) do
    :gen_statem.call(cb, {:execute, fun})
  end

  @spec state(GenServer.server()) :: :closed | :open | :half_open
  def state(cb) do
    :gen_statem.call(cb, :get_state)
  end

  # ---------------------------------------------------------------------------
  # :gen_statem callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def callback_mode, do: :state_functions

  @impl true
  def init(data) do
    {:ok, :closed, data}
  end

  # ---------------------------------------------------------------------------
  # State: CLOSED
  # ---------------------------------------------------------------------------

  def closed({:call, from}, {:execute, fun}, data) do
    # TODO: call safe_call(fun)
    # On {:ok, _}: reset failures to 0, reply with result
    # On {:error, _}: increment failures
    #   if failures >= threshold: transition to :open with state_timeout
    #   else: stay in :closed
    # HINT: {:next_state, :open, new_data, [{:reply, from, result}, {:state_timeout, data.timeout_ms, :recovery}]}
  end

  def closed({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, :closed}]}
  end

  # ---------------------------------------------------------------------------
  # State: OPEN
  # ---------------------------------------------------------------------------

  def open({:call, from}, {:execute, _fun}, data) do
    # TODO: fast-fail — do not call fun
    # HINT: {:keep_state, data, [{:reply, from, {:error, :circuit_open}}]}
  end

  def open({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, :open}]}
  end

  def open(:state_timeout, :recovery, data) do
    # TODO: transition to :half_open — no timer needed here
    # The :open state_timeout fires only once; :half_open has no auto-timeout
  end

  # ---------------------------------------------------------------------------
  # State: HALF_OPEN
  # ---------------------------------------------------------------------------

  def half_open({:call, from}, {:execute, fun}, data) do
    # TODO: one probe call
    # On {:ok, _}: close the circuit (reset failures), reply with result
    # On {:error, _}: reopen the circuit with state_timeout again
  end

  def half_open({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, :half_open}]}
  end

  # ---------------------------------------------------------------------------
  # Catch-all: ignore unknown messages (e.g., stale state_timeout from :open
  # arriving in :closed after a fast recovery)
  # ---------------------------------------------------------------------------

  def closed(:state_timeout, _, data), do: {:keep_state, data}
  def half_open(:state_timeout, _, data), do: {:keep_state, data}

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp safe_call(fun) do
    try do
      case fun.() do
        {:ok, _} = ok   -> ok
        {:error, _} = e -> e
        other           -> {:ok, other}
      end
    rescue
      e -> {:error, Exception.message(e)}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/api_gateway/circuit_breaker/breaker_test.exs
defmodule ApiGateway.CircuitBreaker.BreakerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.CircuitBreaker.Breaker

  defp start_breaker(opts \\ []) do
    name      = :"breaker_#{System.unique_integer([:positive])}"
    threshold = Keyword.get(opts, :threshold, 3)
    timeout   = Keyword.get(opts, :timeout, 100)
    {:ok, _}  = Breaker.start_link(name: name, threshold: threshold, timeout: timeout)
    name
  end

  test "starts in :closed state" do
    cb = start_breaker()
    assert Breaker.state(cb) == :closed
  end

  test "successful calls keep circuit closed" do
    cb = start_breaker()
    assert {:ok, :result} = Breaker.call(cb, fn -> {:ok, :result} end)
    assert Breaker.state(cb) == :closed
  end

  test "failures below threshold keep circuit closed" do
    cb = start_breaker(threshold: 3)
    for _ <- 1..2, do: Breaker.call(cb, fn -> {:error, :down} end)
    assert Breaker.state(cb) == :closed
  end

  test "failures at threshold open the circuit" do
    cb = start_breaker(threshold: 3)
    for _ <- 1..3, do: Breaker.call(cb, fn -> {:error, :down} end)
    assert Breaker.state(cb) == :open
  end

  test "open circuit fast-fails without calling the function" do
    cb = start_breaker(threshold: 1)
    Breaker.call(cb, fn -> {:error, :down} end)

    called = :counters.new(1, [])
    assert {:error, :circuit_open} =
             Breaker.call(cb, fn ->
               :counters.add(called, 1, 1)
               {:ok, :never}
             end)

    assert :counters.get(called, 1) == 0
  end

  test "transitions to :half_open after timeout" do
    cb = start_breaker(threshold: 1, timeout: 50)
    Breaker.call(cb, fn -> {:error, :down} end)
    assert Breaker.state(cb) == :open

    Process.sleep(80)
    assert Breaker.state(cb) == :half_open
  end

  test "successful probe in :half_open closes the circuit" do
    cb = start_breaker(threshold: 1, timeout: 50)
    Breaker.call(cb, fn -> {:error, :down} end)
    Process.sleep(80)
    assert Breaker.state(cb) == :half_open

    assert {:ok, :recovered} = Breaker.call(cb, fn -> {:ok, :recovered} end)
    assert Breaker.state(cb) == :closed
  end

  test "failed probe in :half_open reopens the circuit" do
    cb = start_breaker(threshold: 1, timeout: 50)
    Breaker.call(cb, fn -> {:error, :down} end)
    Process.sleep(80)

    Breaker.call(cb, fn -> {:error, :still_down} end)
    assert Breaker.state(cb) == :open
  end

  test "success in :closed resets the failure counter" do
    cb = start_breaker(threshold: 3)
    Breaker.call(cb, fn -> {:error, :err} end)
    Breaker.call(cb, fn -> {:error, :err} end)
    Breaker.call(cb, fn -> {:ok, :good} end)   # resets counter to 0
    Breaker.call(cb, fn -> {:error, :err} end)
    Breaker.call(cb, fn -> {:error, :err} end)
    # only 2 failures since the reset — still closed
    assert Breaker.state(cb) == :closed
  end
end
```

### Step 3: Run the tests

```bash
mix test test/api_gateway/circuit_breaker/breaker_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `state_functions` | `handle_event_function` |
|--------|-------------------|-------------------------|
| Best for | States with independent logic | Shared event logic across states |
| Adding a new state | New function | New match clause |
| Adding a shared event | One clause per state (duplication risk) | One clause with guard |
| Dialyzer support | Cleaner typespecs per state | Harder to specify |
| `:gen_statem` docs example style | Yes | Yes |

For `CircuitBreaker.Breaker`, `state_functions` is the right choice: `:closed`, `:open`,
and `:half_open` respond to `{:execute, fun}` in fundamentally different ways with no
shared logic to extract.

Reflection: if you needed to add a fourth state (`:degraded`, where the circuit allows
10% of traffic), which callback mode would make that addition simpler?

---

## Common production mistakes

**1. Not handling stale state timeouts**
When the circuit transitions from `:open` to `:half_open` via a probe success back to
`:closed`, a second `state_timeout` from the previous `:open` activation may still
fire in `:closed`. Without a catch-all clause, this causes a `bad event` crash.

**2. Mutable data leaking between states**
If you store the `from` reference in `data` for async replies, you must clear it after
replying. A stale `from` in `:open` state that gets replied to a second time will crash
the caller with `already replied`.

**3. Using `GenServer.call` timeout shorter than the circuit's `timeout_ms`**
If the caller's `GenServer.call` timeout fires before the circuit transitions to
`:half_open`, the caller gets a timeout exit instead of `:circuit_open`. Set call
timeout > circuit `timeout_ms` or use a very low `timeout_ms` for tests.

**4. Forgetting that `state_timeout` is per-state-entry**
Each time you enter a state with `{:state_timeout, ms, event}`, the timer resets. If
the circuit flaps (open → half_open → open → half_open) rapidly, each entry of `:open`
resets the timer to `timeout_ms`. This is the correct behavior — but it surprises
developers who expect a global timeout.

---

## Resources

- [`:gen_statem` — Erlang docs](https://www.erlang.org/doc/man/gen_statem.html)
- [GenStateMachine — HexDocs](https://hexdocs.pm/gen_state_machine/GenStateMachine.html) — idiomatic Elixir wrapper
- [Circuit Breaker pattern — Martin Fowler](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Release It! — Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/)
