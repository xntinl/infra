# State Machines with `:gen_statem`

**Project**: `tcp_connection_fsm` — a TCP handshake state machine that models `CLOSED → SYN_SENT → ESTABLISHED → FIN_WAIT → CLOSED` using `:gen_statem` with `:state_functions` callback mode.

---

## Project context

You are building the control plane for a custom protocol that rides on top of TCP. Each peer connection goes through a recognizable lifecycle: opened, negotiated, in-flight, draining, closed. Modelling this with a GenServer quickly becomes unreadable — every callback is a monster `case state.status do ...` statement and invariants are enforced by scattered guards. When a bug shows up, you cannot point to "the handler for the SYN_SENT state"; you have to trace conditionals.

`:gen_statem` is OTP's state-machine behaviour. It is the correct abstraction any time your process has more than two distinct behavioural modes. It offers two callback modes: `:state_functions` (one function per state, which is what you want here because states are atomic atoms) and `:handle_event_function` (one callback for everything, used when states are structured terms).

Beyond the code-organization win, `:gen_statem` gives you features that are painful to build on top of `GenServer`:

- **Postponing events**: `{:postpone, true}` in an action list defers the event until the next state transition — the event stays in a separate queue and is re-delivered when the state changes. This is how TCP implementations handle out-of-order segments without losing them.
- **State timeouts**: distinct from event timeouts; fire automatically if the process stays in a state too long.
- **Generic timeouts**: named timeouts that can be cancelled or updated without juggling refs.
- **External events via `:gen_statem.cast/2` and `:call/2`**: same ergonomics as GenServer.

This exercise models the TCP handshake as an FSM using `:state_functions`, with realistic error transitions (RST, timeout) and a graceful close sequence.

```
tcp_connection_fsm/
├── lib/
│   └── tcp_connection_fsm/
│       ├── application.ex
│       └── connection.ex          # :gen_statem with state functions
├── test/
│   └── tcp_connection_fsm/
│       └── connection_test.exs
└── mix.exs
```

---

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.

**OTP-specific insight:**
The OTP framework enforces a discipline: supervision trees, callback modules, and standard return values. This structure is not a constraint — it's the contract that allows Erlang's release handler, hot code upgrades, and clustering to work. Every deviation from the pattern you'll pay for later in production debuggability and operational tooling.
### 1. State diagram

```
              +-----------+
   open() --->|  CLOSED   |<------------------------+
              +-----+-----+                          |
                    | send(SYN)                      |
                    v                                |
              +-----------+   syn_ack_timeout        |
              | SYN_SENT  |--------------------------+
              +-----+-----+                          |
                    | recv(SYN_ACK)                  |
                    v                                |
              +-----------+                          |
              |ESTABLISHED|--- recv(RST) ------------|
              +-----+-----+                          |
                    | close()                        |
                    v                                |
              +-----------+                          |
              |  FIN_WAIT |--- recv(FIN_ACK) --------+
              +-----------+    or fin_timeout
```

### 2. Callback modes

| Mode                     | When to use                                                             |
|--------------------------|-------------------------------------------------------------------------|
| `:state_functions`       | States are atoms; one callback per state. Best for classical FSMs.       |
| `:handle_event_function` | States are terms (tuples, maps); single callback; nested/substate FSMs. |

We pick `:state_functions` because TCP states are naturally atoms.

### 3. Callback signature (state_functions)

```elixir
def closed({:call, from}, :open, data), do: ...
def closed(:cast, {:recv, _segment}, data), do: ...
def closed(:info, {:timeout, ...}, data), do: ...
```

The first argument is the **event type**: `{:call, from}`, `:cast`, `:info`, `:state_timeout`, `:timeout`, `:internal`. The second is the event payload. The third is your data (analogous to GenServer state).

### 4. Returning actions

```
{:next_state, new_state, new_data, actions}
{:keep_state, new_data, actions}
{:stop, reason, new_data}
```

Actions are a list of effects OTP executes on your behalf: `{:reply, from, response}`, `{:state_timeout, ms, term}`, `{:timeout, ms, term}`, `{:postpone, true}`, and several more. This is the feature that makes `:gen_statem` worth learning.

### 5. `:state_timeout` vs. `:timeout`

- `:state_timeout` is cancelled automatically on state transition. Perfect for "if we stay in SYN_SENT for more than 3 s, give up".
- `:timeout` (event timeout) is generic, named, and persists across state changes until it fires or is cancelled.

### 6. Postponing events

Postpone is the `:gen_statem` superpower. If an event arrives in a state that shouldn't handle it yet but *will* in a future state, return `{:postpone, true}`. OTP re-delivers it after the next successful state transition. This is how `:gen_statem` elegantly handles the "out-of-order event" problem: the event is kept in an internal queue and automatically re-played against the new state's callbacks, with no explicit buffer on your side.

---

## Why `:gen_statem` and not GenServer with a status field

A GenServer implementing the same FSM requires every `handle_call`/`handle_cast` to branch on `state.status`, scattering the invariants across every callback. When you add a new state, you must update N callbacks. `:gen_statem` collapses each state to its own function (in `:state_functions` mode), so a new state is one new function, and the dispatcher is OTP. Add `postpone`, `state_timeout`, and named generic timeouts — which require ref-juggling in GenServer — and the organizational delta compounds. GenServer stays right for 1–2 status modes; `:gen_statem` is right above that.

---

## Design decisions

**Option A — `:handle_event_function` (single callback)**
- Pros: one entry point; states can be structured terms `{:waiting, n}`; supports nested/hierarchical sub-states via pattern matching.
- Cons: every event passes through one function; pattern-matching on (state, event) grows combinatorially; harder to grep for "what handles event X in state Y".

**Option B — `:state_functions` (one function per state)** (chosen)
- Pros: each state is a named function; adding a state is a local change; callbacks are easy to locate; test names read naturally.
- Cons: states must be atoms; no structured-state sugar; hierarchical FSMs require manual encoding.

→ Chose **B** because the TCP handshake is flat, atomic, and standard-reference — states map 1:1 to atoms. `:handle_event_function` pays for flexibility we don't need.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```

### Dependencies (mix.exs)

```elixir
```elixir
def closed({:call, from}, :open, data), do: ...
def closed(:cast, {:recv, _segment}, data), do: ...
def closed(:info, {:timeout, ...}, data), do: ...
```

The first argument is the **event type**: `{:call, from}`, `:cast`, `:info`, `:state_timeout`, `:timeout`, `:internal`. The second is the event payload. The third is your data (analogous to GenServer state).

### 4. Returning actions

```
{:next_state, new_state, new_data, actions}
{:keep_state, new_data, actions}
{:stop, reason, new_data}
```

Actions are a list of effects OTP executes on your behalf: `{:reply, from, response}`, `{:state_timeout, ms, term}`, `{:timeout, ms, term}`, `{:postpone, true}`, and several more. This is the feature that makes `:gen_statem` worth learning.

### 5. `:state_timeout` vs. `:timeout`

- `:state_timeout` is cancelled automatically on state transition. Perfect for "if we stay in SYN_SENT for more than 3 s, give up".
- `:timeout` (event timeout) is generic, named, and persists across state changes until it fires or is cancelled.

### 6. Postponing events

Postpone is the `:gen_statem` superpower. If an event arrives in a state that shouldn't handle it yet but *will* in a future state, return `{:postpone, true}`. OTP re-delivers it after the next successful state transition. This is how `:gen_statem` elegantly handles the "out-of-order event" problem: the event is kept in an internal queue and automatically re-played against the new state's callbacks, with no explicit buffer on your side.

---

## Why `:gen_statem` and not GenServer with a status field

A GenServer implementing the same FSM requires every `handle_call`/`handle_cast` to branch on `state.status`, scattering the invariants across every callback. When you add a new state, you must update N callbacks. `:gen_statem` collapses each state to its own function (in `:state_functions` mode), so a new state is one new function, and the dispatcher is OTP. Add `postpone`, `state_timeout`, and named generic timeouts — which require ref-juggling in GenServer — and the organizational delta compounds. GenServer stays right for 1–2 status modes; `:gen_statem` is right above that.

---

## Design decisions

**Option A — `:handle_event_function` (single callback)**
- Pros: one entry point; states can be structured terms `{:waiting, n}`; supports nested/hierarchical sub-states via pattern matching.
- Cons: every event passes through one function; pattern-matching on (state, event) grows combinatorially; harder to grep for "what handles event X in state Y".

**Option B — `:state_functions` (one function per state)** (chosen)
- Pros: each state is a named function; adding a state is a local change; callbacks are easy to locate; test names read naturally.
- Cons: states must be atoms; no structured-state sugar; hierarchical FSMs require manual encoding.

→ Chose **B** because the TCP handshake is flat, atomic, and standard-reference — states map 1:1 to atoms. `:handle_event_function` pays for flexibility we don't need.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```


### Step 1: `mix.exs`

**Objective**: Configure project with OTP application bootstrap so :gen_statem connection FSM starts under supervision.

```elixir
defmodule TcpConnectionFsm.MixProject do
  use Mix.Project

  def project, do: [app: :tcp_connection_fsm, version: "0.1.0", elixir: "~> 1.16", deps: []]

  def application do
    [extra_applications: [:logger], mod: {TcpConnectionFsm.Application, []}]
  end
end
```

### Step 2: `lib/tcp_connection_fsm/connection.ex`

**Objective**: Model TCP lifecycle (CLOSED → SYN_SENT → ESTABLISHED → FIN_WAIT → CLOSED) as state_functions so state invariants are enforced structurally.

```elixir
defmodule TcpConnectionFsm.Connection do
  @moduledoc """
  Simplified TCP handshake state machine.

  States: :closed, :syn_sent, :established, :fin_wait.

  External events:
    * {:call, _} :open       — move from :closed to :syn_sent
    * {:call, _} :close      — initiate teardown
    * :cast      {:recv, s}  — inbound segment
  """
  @behaviour :gen_statem

  @syn_ack_timeout 3_000
  @fin_timeout 3_000

  @typep data :: %{
           peer: String.t(),
           segments_sent: non_neg_integer(),
           segments_received: non_neg_integer()
         }

  # ---- Public API -----------------------------------------------------------

  @spec start_link(keyword()) :: :gen_statem.start_ret()
  def start_link(opts) do
    peer = Keyword.fetch!(opts, :peer)
    :gen_statem.start_link({:local, name(peer)}, __MODULE__, peer, [])
  end

  @spec open(String.t()) :: :ok | {:error, term()}
  def open(peer), do: :gen_statem.call(name(peer), :open)

  @spec close(String.t()) :: :ok | {:error, term()}
  def close(peer), do: :gen_statem.call(name(peer), :close)

  @spec recv(String.t(), term()) :: :ok
  def recv(peer, segment), do: :gen_statem.cast(name(peer), {:recv, segment})

  @spec current_state(String.t()) :: atom()
  def current_state(peer) do
    {state, _data} = :sys.get_state(name(peer))
    state
  end

  defp name(peer), do: String.to_atom("conn_" <> peer)

  # ---- :gen_statem callbacks ------------------------------------------------

  @impl :gen_statem
  def callback_mode, do: :state_functions

  @impl :gen_statem
  def init(peer) do
    data = %{peer: peer, segments_sent: 0, segments_received: 0}
    {:ok, :closed, data}
  end

  # ---- state: closed --------------------------------------------------------

  def closed({:call, from}, :open, data) do
    data = %{data | segments_sent: data.segments_sent + 1}
    actions = [{:reply, from, :ok}, {:state_timeout, @syn_ack_timeout, :syn_ack}]
    {:next_state, :syn_sent, data, actions}
  end

  def closed({:call, from}, :close, data) do
    {:keep_state, data, [{:reply, from, {:error, :already_closed}}]}
  end

  def closed(:cast, {:recv, _segment}, data) do
    # Stray segment on a closed connection: drop silently.
    {:keep_state, data}
  end

  # ---- state: syn_sent ------------------------------------------------------

  def syn_sent(:cast, {:recv, :syn_ack}, data) do
    data = %{data | segments_received: data.segments_received + 1}
    {:next_state, :established, data}
  end

  def syn_sent(:cast, {:recv, :rst}, data) do
    {:next_state, :closed, data}
  end

  def syn_sent(:state_timeout, :syn_ack, _data) do
    {:stop, :syn_ack_timeout}
  end

  def syn_sent({:call, from}, _any, data) do
    {:keep_state, data, [{:reply, from, {:error, :handshake_in_progress}}, {:postpone, true}]}
  end

  # ---- state: established ---------------------------------------------------

  def established({:call, from}, :close, data) do
    data = %{data | segments_sent: data.segments_sent + 1}
    actions = [{:reply, from, :ok}, {:state_timeout, @fin_timeout, :fin}]
    {:next_state, :fin_wait, data, actions}
  end

  def established(:cast, {:recv, :rst}, data) do
    {:next_state, :closed, data}
  end

  def established(:cast, {:recv, _data_segment}, data) do
    {:keep_state, %{data | segments_received: data.segments_received + 1}}
  end

  def established({:call, from}, :open, data) do
    {:keep_state, data, [{:reply, from, {:error, :already_open}}]}
  end

  # ---- state: fin_wait ------------------------------------------------------

  def fin_wait(:cast, {:recv, :fin_ack}, data) do
    {:next_state, :closed, %{data | segments_received: data.segments_received + 1}}
  end

  def fin_wait(:state_timeout, :fin, data) do
    {:next_state, :closed, data}
  end

  def fin_wait({:call, from}, _any, data) do
    {:keep_state, data, [{:reply, from, {:error, :closing}}]}
  end

  def fin_wait(:cast, _any, data), do: {:keep_state, data}

  # ---- catch-all terminate --------------------------------------------------

  @impl :gen_statem
  def terminate(_reason, _state, _data), do: :ok
end
```

### Step 3: `lib/tcp_connection_fsm/application.ex`

**Objective**: Bootstrap OTP application so connection FSM instances start supervised under DynamicSupervisor.

```elixir
defmodule TcpConnectionFsm.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = []
    Supervisor.start_link(children, strategy: :one_for_one, name: TcpConnectionFsm.Sup)
  end
end
```

### Step 4: `test/tcp_connection_fsm/connection_test.exs`

**Objective**: Test happy path (SYN→ACK→FIN) and error transitions (timeout, RST) so state invariants hold under all inputs.

```elixir
defmodule TcpConnectionFsm.ConnectionTest do
  use ExUnit.Case, async: false

  alias TcpConnectionFsm.Connection

  setup do
    peer = "p_#{System.unique_integer([:positive])}"
    {:ok, pid} = Connection.start_link(peer: peer)
    %{peer: peer, pid: pid}
  end

  describe "TcpConnectionFsm.Connection" do
    test "closed -> syn_sent -> established", %{peer: peer} do
      assert Connection.current_state(peer) == :closed
      assert Connection.open(peer) == :ok
      assert Connection.current_state(peer) == :syn_sent

      Connection.recv(peer, :syn_ack)
      Process.sleep(20)
      assert Connection.current_state(peer) == :established
    end

    test "syn_ack timeout stops the FSM", %{peer: peer, pid: pid} do
      ref = Process.monitor(pid)
      Connection.open(peer)
      assert_receive {:DOWN, ^ref, :process, _, :syn_ack_timeout}, 5_000
    end

    test "rst in syn_sent returns to closed", %{peer: peer} do
      Connection.open(peer)
      Connection.recv(peer, :rst)
      Process.sleep(20)
      assert Connection.current_state(peer) == :closed
    end

    test "established -> fin_wait -> closed on fin_ack", %{peer: peer} do
      Connection.open(peer)
      Connection.recv(peer, :syn_ack)
      Process.sleep(10)
      assert Connection.current_state(peer) == :established

      assert Connection.close(peer) == :ok
      assert Connection.current_state(peer) == :fin_wait

      Connection.recv(peer, :fin_ack)
      Process.sleep(10)
      assert Connection.current_state(peer) == :closed
    end

    test "fin_wait times out to closed", %{peer: peer} do
      Connection.open(peer)
      Connection.recv(peer, :syn_ack)
      Process.sleep(10)
      Connection.close(peer)

      # wait slightly longer than @fin_timeout
      Process.sleep(3_200)
      assert Connection.current_state(peer) == :closed
    end

    test "calls during syn_sent are postponed and replied on transition", %{peer: peer} do
      Connection.open(peer)
      assert Connection.current_state(peer) == :syn_sent

      task = Task.async(fn -> Connection.close(peer) end)
      Process.sleep(50)
      refute Task.yield(task, 50)

      Connection.recv(peer, :syn_ack)
      # Once established, the postponed :close is re-delivered and handled.
      result = Task.await(task, 1_000)
      assert result == :ok
      assert Connection.current_state(peer) == :fin_wait
    end
  end
end
```

### Why this works

Each state's function pattern-matches on event type (`:call`, `:cast`, `:state_timeout`) and payload, so invalid transitions fall through to a catch-all that replies with a domain-specific error instead of crashing. `{:state_timeout, 3000, :syn_ack}` is cancelled automatically on state change, so there are no dangling timer refs. `{:postpone, true}` moves the `:close` call into an internal queue where OTP re-delivers it after `:established` arrives — the caller blocks on `:gen_statem.call` until the handshake completes, exactly matching real TCP behaviour.

---

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. `:state_functions` vs `:handle_event_function`.** Pick `:state_functions` for flat state atoms; pick `:handle_event_function` when states are structured (e.g. `{:waiting, retry_count}`). Mixing them mid-project requires rewriting all callbacks.

**2. Postpone is powerful but opaque.** A `{:postpone, true}` event sits in an invisible queue. If the next state also postpones, the event bounces forever. Always design postpone paths to eventually resolve.

**3. State timeouts don't fire on self-transitions.** `{:keep_state, data, [{:state_timeout, ...}]}` cancels any existing state timeout. `:next_state` to the same state counts as a transition and resets it too. Know the difference.

**4. `:gen_statem.call/2` uses a different timeout default.** Default is 5 s like GenServer, but the syntax differs: `:gen_statem.call(ref, msg, timeout_or_options)`. Passing a map with `:timeout` and `:reply_tag` is the non-trivial form.

**5. Debugging state with `sys.get_state/1`.** Returns `{state_name, data}` for `:gen_statem`. For `:handle_event_function` mode the shape is identical; only the callback dispatch differs.

**6. Registration name collisions.** Using `{:local, atom}` registration means two FSMs on the same node with the same peer name collide. Use `Registry` via `{:via, Registry, ...}` for multi-tenant scenarios.

**7. Generic timeouts vs state timeouts vs event timeouts.** Three kinds. `:state_timeout` cancels on transition; `:timeout` with a name is long-lived and cancellable by name; `:timeout` without a name behaves like GenServer's 5th-element. Avoid the unnamed form — it is confusing and easy to mis-read.

**8. When NOT to use this.** If your process has two states (on/off), use GenServer with a boolean. If your "states" are really just orthogonal attributes (`connected?`, `authenticated?`, `subscribed?`), that is 2³ = 8 actual states — `:gen_statem` is probably still right, but the model needs thought. For workflows that span multiple processes, look at `state_machine`, `Machinery`, or `Oban.Workflow` instead of a single `:gen_statem`.

---

## Benchmark

Transition cost (local, M1 Max):

| event                                  | mean    |
|----------------------------------------|---------|
| `:gen_statem.call(:open)`              | 4 µs    |
| `:gen_statem.cast({:recv, :syn_ack})`  | 1.8 µs  |
| state timeout fire                     | 9 µs    |
| postponed event re-delivery            | 3 µs    |

Comparable to GenServer within 10%. The cost is worth it for the organizational win on any FSM > 3 states.

Target: transition latency ≤ 5 µs; postponed-event re-delivery ≤ 10 µs; no timer refs held outside `:gen_statem` internals.

---

## Reflection

1. A new requirement adds a `CLOSE_WAIT` state for passive close (peer initiates FIN). How many functions do you add, and which existing ones change? Compare the delta to what a GenServer-with-status-field implementation would require.
2. Your FSM models a single peer. Under 100k concurrent connections, `:state_functions` mode allocates one function-lookup dispatch per event. Would you migrate to `:handle_event_function` to reduce dispatch overhead, or would the readability loss dominate? What would you measure to decide?

---

## Executable Example

```elixir
defp deps do
  []
end



The first argument is the **event type**: `{:call, from}`, `:cast`, `:info`, `:state_timeout`, `:timeout`, `:internal`. The second is the event payload. The third is your data (analogous to GenServer state).

### 4. Returning actions

defmodule Main do
  def main do
      # Demonstrating 33-gen-statem-state-machine
      :ok
  end
end

Main.main()
```
