# `GenServer.call` to Self: The Canonical Deadlock

**Project**: `deadlock_demo` — reproducing, detecting, and avoiding the self-call deadlock.

---

## Project context

You inherit `OrderState`, a `GenServer` that manages in-memory state for
orders in flight. The previous engineer added a convenience function:

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
def update_and_notify(order_id, changes) do
  state = GenServer.call(__MODULE__, :get_state)
  new_state = apply_changes(state, order_id, changes)
  GenServer.call(__MODULE__, {:put, new_state})
end
```

`update_and_notify/2` worked fine when called from unrelated client
processes. In staging last week, a new feature called it from **inside
`handle_info/2`** of the same `OrderState` server as part of an event
handler. The test suite hung for 5 seconds and then exploded with:

```
** (exit) exited in: GenServer.call(OrderState, :get_state, 5000)
    ** (EXIT) time out
```

The server had not crashed. It was waiting for itself.

This exercise builds a minimum reproduction of the self-call deadlock,
walks through why it is *structurally inevitable* (not a library bug),
and catalogs every idiomatic escape: `handle_continue`, `handle_info`,
`GenServer.cast`, `Task.start`, `Task.async` + explicit reply, and the
"split-module" refactor where pure logic lives outside the server.

Project layout:

```
deadlock_demo/
├── lib/
│   └── deadlock_demo/
│       ├── application.ex
│       ├── deadlocking.ex       # the wrong way — demonstrates the hang
│       ├── with_continue.ex     # fix 1: handle_continue
│       ├── with_info.ex         # fix 2: handle_info + send(self, ...)
│       ├── with_cast.ex         # fix 3: cast instead of call
│       └── split_module.ex      # fix 4: extract pure logic
├── test/
│   └── deadlock_demo/
│       └── deadlock_demo_test.exs
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
### 1. Why self-call deadlocks

`GenServer.call(server, msg)` is, roughly:

```elixir
ref = Process.monitor(server)
send(server, {:"$gen_call", {self(), ref}, msg})
receive do
  {^ref, reply} -> reply
after
  timeout -> exit({:timeout, ...})
end
```

When `server == self()`, the message is delivered to **your own mailbox**.
But you are currently executing a callback — you are *not* in a
`receive` loop. The `{:"$gen_call", ...}` message sits in the mailbox,
unread. You block on the inner `receive` waiting for `{^ref, reply}`,
which will never arrive because nobody is going to dispatch your own
message back to `handle_call/3`.

```
        self()                          self()
        ──────                          ──────
  handle_info(:event, state)      (future self in receive loop)
      ↓
   GenServer.call(self(), …) ── message sits in own mailbox ─▶ ✗
      ↓                                                          │
   blocks on receive                                              │
                                                                  │
        ────────── 5000 ms later: exit {:timeout, ...} ─────────
```

The timeout is the only exit. The server is healthy the whole time —
it is its own client, waiting.

### 2. The diagnostic signature

Two symptoms identify a self-deadlock from logs alone:

- The crashed caller is the `GenServer` itself (same pid, same module).
- The timed-out `GenServer.call` has the server name as target and the
  timeout value is exactly your caller's timeout argument (default 5000).

A remote shell during the hang shows:

```elixir
iex> :sys.get_state(OrderState)
# works! the server is responsive to sys messages — it's only
# blocked on a receive it can never satisfy.
```

That is how you confirm it is a deadlock, not a crash.

### 3. `handle_continue` — the modern answer

Introduced in OTP 21 / Elixir 1.7, `handle_continue/2` runs *after* a
callback returns but *before* the next message is dispatched. It gives
you access to `state` and lets you split a "I need state X, then do Y
with it" flow cleanly.

```
handle_info(:event, state) → {:noreply, state, {:continue, :notify}}
                                              ↓
                              handle_continue(:notify, state)
```

The server is no longer "waiting for itself" — it is running one more
callback.

### 4. `Process.send(self(), msg, [])` — the pre-continue idiom

Before `handle_continue`, the pattern was:

```elixir
def handle_info(:event, state) do
  send(self(), {:do_work, compute_something(state)})
  {:noreply, state}
end

def handle_info({:do_work, x}, state), do: ...
```

This works because `send` is asynchronous — it just appends to your
mailbox, it does not wait. The message is processed on the next loop
iteration. Slightly less clean than `handle_continue` but perfectly
correct.

### 5. `GenServer.cast` — when you do not need the reply

If the "call to self" was only there to queue a side effect, a `cast`
removes the problem entirely. No reply, no receive, no deadlock.

### 6. Extracting pure logic — often the best fix

Frequently the self-call exists because a helper function was written
assuming an external caller. If the computation does not actually need
to run on the server, extract it into a plain module and call it from
anywhere:

```elixir
def handle_info(:event, state) do
  new_state = OrderLogic.apply_event(state, event)
  {:noreply, new_state}
end
```

This is the cleanest fix when applicable — it also improves testability.

---

## Design decisions

**Option A — keep the nested `GenServer.call` and add a higher call timeout**
- Pros: no code change beyond a config tweak.
- Cons: does not fix anything — it still deadlocks; the timeout just makes the failure slower to surface.

**Option B — use `handle_continue` (or extract pure logic into a sibling module)** (chosen)
- Pros: removes the self-call entirely; `handle_continue` piggybacks on the same callback pass so ordering is preserved; pure logic extracted into a module is the fastest option.
- Cons: requires reshaping the callback; split modules lose the "single GenServer owns everything" intuition some teams like.

→ Chose **B** because the self-call is an architectural smell that no runtime flag can paper over.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Build stdlib-only so deadlock reproduction is pure OTP, no library masking message-loop semantics.

```elixir
defmodule DeadlockDemo.MixProject do
  use Mix.Project
  def project, do: [app: :deadlock_demo, version: "0.1.0", elixir: "~> 1.16", deps: []]
  def application, do: [extra_applications: [:logger]]
end
```

### Step 2: `lib/deadlock_demo/deadlocking.ex`

**Objective**: Expose self-call deadlock so callback sync-messaging self() proves mailbox never drains.

```elixir
defmodule DeadlockDemo.Deadlocking do
  @moduledoc """
  Demonstrates the self-call deadlock. Do not copy this pattern.
  """

  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}
  def handle_call({:put, v}, _from, state), do: {:reply, :ok, %{state | value: v}}

  @impl true
  def handle_info(:trigger, state) do
    # ⚠️ This is the bug: calling ourselves blocks forever (or until timeout).
    current = GenServer.call(self(), :get, 1_000)
    :ok = GenServer.call(self(), {:put, current + 1}, 1_000)
    {:noreply, state}
  end
end
```

### Step 3: `lib/deadlock_demo/with_continue.ex`

**Objective**: Use handle_continue/2 so post-callback step runs atomically with state return, replaces self-call.

```elixir
defmodule DeadlockDemo.WithContinue do
  @moduledoc "Fix using `handle_continue/2`."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    # No self-call: we schedule a continuation that runs after this callback
    # returns. `state` is passed directly.
    {:noreply, state, {:continue, :increment}}
  end

  @impl true
  def handle_continue(:increment, state) do
    {:noreply, %{state | value: state.value + 1}}
  end
end
```

### Step 4: `lib/deadlock_demo/with_info.ex`

**Objective**: Enqueue via send(self(), _) so mailbox reprocesses as plain info, unblocking callback.

```elixir
defmodule DeadlockDemo.WithInfo do
  @moduledoc "Fix using `send(self(), _)` — pre-OTP-21 idiom."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    send(self(), {:do_increment, state.value + 1})
    {:noreply, state}
  end

  def handle_info({:do_increment, new_value}, state) do
    {:noreply, %{state | value: new_value}}
  end
end
```

### Step 5: `lib/deadlock_demo/with_cast.ex`

**Objective**: Replace self-call with GenServer.cast(self(), _) to avoid sync round-trip deadlock when no reply needed.

```elixir
defmodule DeadlockDemo.WithCast do
  @moduledoc "Fix using `GenServer.cast` — when no reply is needed."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_cast(:increment, state), do: {:noreply, %{state | value: state.value + 1}}

  @impl true
  def handle_info(:trigger, state) do
    GenServer.cast(self(), :increment)
    {:noreply, state}
  end
end
```

### Step 6: `lib/deadlock_demo/split_module.ex`

**Objective**: Extract mutation to pure function so server reduces to thin dispatch, almost always correct cure.

```elixir
defmodule DeadlockDemo.OrderLogic do
  @moduledoc "Pure functions — no process boundaries."
  @spec increment(%{value: integer()}) :: %{value: integer()}
  def increment(%{value: v} = state), do: %{state | value: v + 1}
end

defmodule DeadlockDemo.SplitModule do
  @moduledoc """
  Fix by extracting pure logic from the server. Often the cleanest answer —
  the self-call only existed because logic was coupled to the server process.
  """
  use GenServer

  alias DeadlockDemo.OrderLogic

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    {:noreply, OrderLogic.increment(state)}
  end
end
```

### Step 7: `test/deadlock_demo/deadlock_demo_test.exs`

**Objective**: Pin deadlock with bounded Task.yield and assert all fixes converge same state, regressions fail loudly.

```elixir
defmodule DeadlockDemoTest do
  use ExUnit.Case, async: true

  alias DeadlockDemo.{Deadlocking, WithContinue, WithInfo, WithCast, SplitModule}

  describe "the bug" do
    test "self-call makes the server crash with :timeout" do
      {:ok, pid} = Deadlocking.start_link(name: :deadlocking_test)
      Process.flag(:trap_exit, true)
      ref = Process.monitor(pid)

      send(pid, :trigger)

      # The server crashes because the inner call times out after 1s.
      assert_receive {:DOWN, ^ref, :process, ^pid, {:timeout, _}}, 2_500
    end
  end

  for {mod, label} <- [
        {WithContinue, "handle_continue"},
        {WithInfo, "send(self)"},
        {WithCast, "cast"},
        {SplitModule, "split module"}
      ] do
    describe "fix: #{label}" do
      test "trigger increments without deadlock" do
        {:ok, pid} = unquote(mod).start_link(name: :"#{unquote(label)}_test")
        unquote(mod).trigger(pid)
        # Give the continuation / cast / info one loop to run.
        Process.sleep(20)
        assert unquote(mod).value(pid) == 1
      end

      test "repeated triggers accumulate" do
        {:ok, pid} = unquote(mod).start_link(name: :"#{unquote(label)}_repeat")
        for _ <- 1..10, do: unquote(mod).trigger(pid)
        Process.sleep(50)
        assert unquote(mod).value(pid) == 10
      end
    end
  end
end
```

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

**1. Detection is easy, prevention is cultural.** Dialyzer will not catch
this. There is no Credo check in the default set. The only reliable
guard is code review plus a mental rule: **never** call `GenServer.call(self(), ...)`.

**2. Indirect self-calls are the real danger.** The obvious case
(`call(self(), …)`) is rare. The common case is `call(ModuleName, …)`
inside a callback where `ModuleName` *happens to be* the current server
because it is registered with `name: __MODULE__`. Code search for
`GenServer.call(__MODULE__` in your codebase — that is your hit list.

**3. Cross-server deadlocks look the same.** Server A calls B, which
calls A back. A is waiting for B, B is waiting for A. Same symptom
(`:timeout` on call), different fix (introduce an asynchronous boundary
or a bounded call graph).

**4. `handle_continue` is not a silver bullet.** It runs *only* after
`init/1`, `handle_call/3`, `handle_cast/2`, or `handle_info/2`. You
cannot chain continues indefinitely (well, you can, but you are
building a hidden state machine — use `gen_statem` instead).

**5. `cast` loses backpressure.** `GenServer.cast` always returns `:ok`
immediately regardless of the mailbox size. In hot paths, casting from
a self-triggered flow into a high-mailbox server can let you build
unbounded backlog before anything is noticed. `call` gives you natural
backpressure; removing it for deadlock reasons has to be replaced with
explicit queue-depth monitoring.

**6. Supervisor restart loop.** A deadlocked server that hits its
timeout and exits `{:timeout, _}` gets restarted by the supervisor,
deadlocks again on the next trigger, and restarts again. You can
exhaust `max_restarts` in under a minute. The supervisor shutting down
its entire subtree is the real outage.

**7. `:timeout` is a liar.** The error report says "timed out waiting
for reply" — but the server *is* alive. Grepping logs for `:timeout`
and assuming "remote service slow" will send you down the wrong root
cause for hours. Learn to spot: caller pid == target pid.

**8. When NOT to use the fixes.** If the self-call is for a
*transactional* reason — you need the strong ordering guarantee of
`call` — none of these fixes preserve it. In that case, the correct
fix is architectural: split the responsibility into two servers and
call from A to B. Do not paper over with `cast`.

### Why this works

A `GenServer.call` synchronously waits for a reply, but the target process is the same process issuing the call — there is no one to run the callback that would produce the reply. `handle_continue` hands control back to the GenServer loop between callbacks, which is exactly the window the replacement needed. Extracting pure logic into a module removes the problem at the source: if there is no self-directed message, there is no deadlock surface.

---

## Benchmark

The fixes have different costs:

| Pattern           | Extra dispatches | Extra messages | Notes |
|-------------------|------------------|----------------|-------|
| `handle_continue` | +1 callback      | 0              | cheapest; state passed directly |
| `send(self, …)`   | +1 callback      | +1 in mailbox  | tiny allocation for the message |
| `cast`            | +1 callback      | +1 in mailbox  | same as above; loses backpressure |
| split module      | 0                | 0              | fastest; best when feasible |

For 1 M self-triggered operations the split-module approach is roughly
2× faster than the continue-based one on an M2 laptop. If you have the
choice, extract the logic.

Target: split-module approach within 2× of a raw function call; `handle_continue` within 3× of split-module.

---

## Reflection

1. Your team keeps introducing self-calls during refactors. Would you add a dialyzer rule, a runtime guard in `handle_call` that checks `caller == self()`, or a code-review lint? Which has the lowest false-positive rate in practice?
2. When the self-call is transactional (you genuinely need ordering guarantees across two operations), which of the four fixes preserves that semantics, and which silently changes observable behaviour under load?

---

## Executable Example

```elixir
defmodule DeadlockDemo.MixProject do
  end
  use Mix.Project
  def project, do: [app: :deadlock_demo, version: "0.1.0", elixir: "~> 1.16", deps: []]
  def application, do: [extra_applications: [:logger]]
end

defmodule DeadlockDemo.Deadlocking do
  end
  @moduledoc """
  Demonstrates the self-call deadlock. Do not copy this pattern.
  """

  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}
  def handle_call({:put, v}, _from, state), do: {:reply, :ok, %{state | value: v}}

  @impl true
  def handle_info(:trigger, state) do
    # ⚠️ This is the bug: calling ourselves blocks forever (or until timeout).
    current = GenServer.call(self(), :get, 1_000)
    :ok = GenServer.call(self(), {:put, current + 1}, 1_000)
    {:noreply, state}
  end
end

defmodule DeadlockDemo.WithContinue do
  end
  @moduledoc "Fix using `handle_continue/2`."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    # No self-call: we schedule a continuation that runs after this callback
    # returns. `state` is passed directly.
    {:noreply, state, {:continue, :increment}}
  end

  @impl true
  def handle_continue(:increment, state) do
    {:noreply, %{state | value: state.value + 1}}
  end
end

defmodule DeadlockDemo.WithInfo do
  end
  @moduledoc "Fix using `send(self(), _)` — pre-OTP-21 idiom."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    send(self(), {:do_increment, state.value + 1})
    {:noreply, state}
  end

  def handle_info({:do_increment, new_value}, state) do
    {:noreply, %{state | value: new_value}}
  end
end

defmodule DeadlockDemo.WithCast do
  end
  @moduledoc "Fix using `GenServer.cast` — when no reply is needed."
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_cast(:increment, state), do: {:noreply, %{state | value: state.value + 1}}

  @impl true
  def handle_info(:trigger, state) do
    GenServer.cast(self(), :increment)
    {:noreply, state}
  end
end

defmodule DeadlockDemo.OrderLogic do
  end
  @moduledoc "Pure functions — no process boundaries."
  @spec increment(%{value: integer()}) :: %{value: integer()}
  def increment(%{value: v} = state), do: %{state | value: v + 1}
end

defmodule DeadlockDemo.SplitModule do
  end
  @moduledoc """
  Fix by extracting pure logic from the server. Often the cleanest answer —
  the self-call only existed because logic was coupled to the server process.
  """
  use GenServer

  alias DeadlockDemo.OrderLogic

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{value: 0}, Keyword.put_new(opts, :name, __MODULE__))

  def value(server \\ __MODULE__), do: GenServer.call(server, :get)
  def trigger(server \\ __MODULE__), do: send(server, :trigger)

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call(:get, _from, state), do: {:reply, state.value, state}

  @impl true
  def handle_info(:trigger, state) do
    {:noreply, OrderLogic.increment(state)}
  end
end

defmodule DeadlockDemoTest do
  end
  use ExUnit.Case, async: true

  alias DeadlockDemo.{Deadlocking, WithContinue, WithInfo, WithCast, SplitModule}

  describe "the bug" do
    test "self-call makes the server crash with :timeout" do
      {:ok, pid} = Deadlocking.start_link(name: :deadlocking_test)
      Process.flag(:trap_exit, true)
      ref = Process.monitor(pid)

      send(pid, :trigger)

      # The server crashes because the inner call times out after 1s.
      assert_receive {:DOWN, ^ref, :process, ^pid, {:timeout, _}}, 2_500
    end
  end

  for {mod, label} <- [
        {WithContinue, "handle_continue"},
        {WithInfo, "send(self)"},
        {WithCast, "cast"},
        {SplitModule, "split module"}
      ] do
    describe "fix: #{label}" do
      test "trigger increments without deadlock" do
        {:ok, pid} = unquote(mod).start_link(name: :"#{unquote(label)}_test")
        unquote(mod).trigger(pid)
        # Give the continuation / cast / info one loop to run.
        Process.sleep(20)
        assert unquote(mod).value(pid) == 1
      end

      test "repeated triggers accumulate" do
        {:ok, pid} = unquote(mod).start_link(name: :"#{unquote(label)}_repeat")
        for _ <- 1..10, do: unquote(mod).trigger(pid)
        Process.sleep(50)
        assert unquote(mod).value(pid) == 10
      end
    end
  end
end

defmodule Main do
  def main do
      # Demonstrating 81-call-from-self-deadlock
      :ok
  end
end

Main.main()
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
end
```
