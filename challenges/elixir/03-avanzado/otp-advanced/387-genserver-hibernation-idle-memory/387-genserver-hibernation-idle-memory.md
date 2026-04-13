# GenServer Hibernation for Idle Processes

**Project**: `chat_presence` — presence tracker with one GenServer per connected user. Most users are idle most of the time; hibernation keeps per-process RSS minimal while keeping the session warm.

## Project context

You're building a chat application where each connected user has a dedicated GenServer holding their session state (unread count, typing state, last-read ptr). At peak you have 500k concurrent sessions, but the working set — users actively sending messages right now — is only about 2% of them. The rest are connected but idle for tens of seconds between keystrokes.

A plain GenServer keeps its heap alive forever. After a minute of activity, each process's young-gen heap might be 4–8 KB, its reduction counter has been frobbed, and its stack has grown. 500k × 8 KB = 4 GB of RSS for processes that are doing nothing. Hibernation fixes this: `:hibernate` forces a fullsweep garbage collection, compacts the heap to the smallest possible size (typically 233 words ≈ 2 KB), and suspends the scheduler from even running this process until a message arrives.

The wake cost is a few microseconds of heap allocation and a full copy of the incoming message onto the new heap. Net effect: idle processes become essentially free. Under hibernation, a 500k-session chat server runs in hundreds of MB instead of GBs.

```
chat_presence/
├── lib/
│   └── chat_presence/
│       ├── application.ex
│       ├── session.ex                # the GenServer — hibernates when idle
│       ├── session_supervisor.ex     # DynamicSupervisor
│       └── session_registry.ex       # Registry for user_id → pid
├── test/
│   └── chat_presence/
│       └── session_test.exs
├── bench/
│   └── memory_bench.exs
└── mix.exs
```

## Why not just shrink the heap manually

You could call `:erlang.garbage_collect/1` periodically. But the BEAM has a smarter primitive: hibernation. Instead of GC-ing the heap *and* keeping the process scheduled, hibernation:

1. Runs a fullsweep GC.
2. Reclaims the stack.
3. Removes the process from the run queue entirely.
4. Saves only the MFA continuation (`{module, function, args}`) — no heap.

When a message arrives, the scheduler re-allocates a fresh heap and invokes the MFA. Steady-state cost: as close to zero as BEAM gets.

## Why not use ETS for the state

An ETS table with one row per user avoids the per-process memory entirely. But then you lose isolation: two messages for user A serialize through whatever process owns the ETS row (or nothing enforces ordering). You lose the supervision tree's error boundary. You lose per-user timers without manual bookkeeping. Per-user process + hibernation is the BEAM idiom; it keeps semantics while costing almost nothing.

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
### 1. `:hibernate` return
Any callback can return `{:noreply, state, :hibernate}` or `{:reply, reply, state, :hibernate}`. The process is hibernated *after* the callback returns.

### 2. Continuation
A hibernated process is just an MFA waiting on a mailbox. On next message, the MFA runs and the state is re-built.

### 3. `Process.info(pid, :memory)`
Reports live memory including heap, stack, bookkeeping, and mailbox. Best metric for verifying hibernation.

### 4. Idle detection
The process knows it is idle because it receives a `:timeout` or an `:idle_check` message. Hibernation only makes sense after *some* idleness; hibernating on every message would thrash.

## Design decisions

- **Option A — hibernate after each idle-timeout fires**: predictable memory, cheap check, common pattern.
- **Option B — hibernate on every `handle_info`/`handle_cast`**: maximum memory savings. Con: each incoming message pays the fullsweep cost; chatty sessions suffer.
- **Option C — hibernate on a heap-size threshold**: best memory/CPU trade-off. Con: complexity; requires reading `Process.info/2` on every callback.

→ A for most applications. A chat session that is silent for 30s likely stays silent; GC and hibernate. Pay once, reap the memory win.

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
defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]
```

### Step 1: Registry and Supervisor

**Objective**: Wire Registry + DynamicSupervisor so per-user sessions hibernate independently via :via addressing.

```elixir
defmodule ChatPresence.SessionRegistry do
  def child_spec(_), do: Registry.child_spec(keys: :unique, name: __MODULE__)
  def via(user_id), do: {:via, Registry, {__MODULE__, user_id}}
end

defmodule ChatPresence.SessionSupervisor do
  use DynamicSupervisor

  def start_link(_), do: DynamicSupervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: DynamicSupervisor.init(strategy: :one_for_one)

  def ensure_session(user_id) do
    case Registry.lookup(ChatPresence.SessionRegistry, user_id) do
      [{pid, _}] -> {:ok, pid}
      [] ->
        DynamicSupervisor.start_child(__MODULE__, {ChatPresence.Session, user_id})
    end
  end
end
```

### Step 2: Application

**Objective**: Start Registry before DynamicSupervisor so sessions register :via names atomically, avoiding startup races.

```elixir
defmodule ChatPresence.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ChatPresence.SessionRegistry,
      ChatPresence.SessionSupervisor
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChatPresence.Supervisor)
  end
end
```

### Step 3: Session GenServer with hibernation

**Objective**: Return :hibernate on idle timeout so dormant sessions shrink from GBs to MBs for 100k users at scale.

```elixir
defmodule ChatPresence.Session do
  use GenServer

  @idle_timeout_ms 10_000

  defmodule State do
    @moduledoc false
    defstruct user_id: nil,
              last_seen_at: 0,
              unread_count: 0,
              hibernated?: false
  end

  # --- public API ---

  def start_link(user_id),
    do: GenServer.start_link(__MODULE__, user_id, name: ChatPresence.SessionRegistry.via(user_id))

  def ping(user_id), do: GenServer.cast(via(user_id), :ping)
  def increment_unread(user_id), do: GenServer.cast(via(user_id), :increment_unread)
  def read_state(user_id), do: GenServer.call(via(user_id), :state)

  defp via(user_id), do: ChatPresence.SessionRegistry.via(user_id)

  # --- callbacks ---

  @impl true
  def init(user_id) do
    state = %State{user_id: user_id, last_seen_at: now_ms()}
    {:ok, state, @idle_timeout_ms}
  end

  @impl true
  def handle_cast(:ping, state) do
    state = %{state | last_seen_at: now_ms(), hibernated?: false}
    {:noreply, state, @idle_timeout_ms}
  end

  def handle_cast(:increment_unread, state) do
    state = %{state | unread_count: state.unread_count + 1, hibernated?: false}
    {:noreply, state, @idle_timeout_ms}
  end

  @impl true
  def handle_call(:state, _from, state) do
    {:reply, Map.from_struct(state), state, @idle_timeout_ms}
  end

  @impl true
  def handle_info(:timeout, state) do
    # No activity for @idle_timeout_ms. Hibernate.
    # The fullsweep GC runs after this callback returns.
    {:noreply, %{state | hibernated?: true}, :hibernate}
  end

  defp now_ms, do: System.monotonic_time(:millisecond)
end
```

## Memory behaviour diagram

```
Time │ mailbox │ heap     │ scheduled?
─────┼─────────┼──────────┼────────────
 t0  │ init    │ ~2.7 KB  │ yes
 t1  │ ping    │ ~3.2 KB  │ yes
 t2  │ ping    │ ~4.1 KB  │ yes
 ...
 t9  │ (idle)  │ ~9.8 KB  │ yes   ◀── timeout fires
 t9+ │         │ ~2.1 KB  │ no    ◀── hibernated: minimum heap
  .  │         │          │
 tN  │ ping    │ ~3.0 KB  │ yes   ◀── woken up, fresh heap
```

## Tests

```elixir
defmodule ChatPresence.SessionTest do
  use ExUnit.Case, async: false

  alias ChatPresence.{Session, SessionSupervisor, SessionRegistry}

  setup do
    start_supervised!(SessionRegistry)
    start_supervised!(SessionSupervisor)
    :ok
  end

  describe "lifecycle" do
    test "session tracks unread count" do
      {:ok, _pid} = SessionSupervisor.ensure_session("u1")
      Session.increment_unread("u1")
      Session.increment_unread("u1")
      assert %{unread_count: 2} = Session.read_state("u1")
    end
  end

  describe "hibernation" do
    test "process hibernates after idle timeout" do
      {:ok, pid} = SessionSupervisor.ensure_session("u2")
      Session.ping("u2")

      mem_before = Process.info(pid, :memory) |> elem(1)
      assert mem_before > 1_500

      # Idle timeout is 10s — wait long enough for a fullsweep + hibernation.
      Process.sleep(11_000)

      mem_after = Process.info(pid, :memory) |> elem(1)
      # After hibernation, the process should be near minimum heap.
      assert mem_after < mem_before,
             "expected hibernation to shrink memory; before=#{mem_before} after=#{mem_after}"

      assert Process.info(pid, :current_function) == {:current_function, {:erlang, :hibernate, 3}}
    end

    @tag :slow
    test "wake from hibernation preserves state" do
      {:ok, _pid} = SessionSupervisor.ensure_session("u3")
      Session.increment_unread("u3")
      Process.sleep(11_000)

      # Wake up
      Session.increment_unread("u3")
      assert %{unread_count: 2} = Session.read_state("u3")
    end
  end
end
```

## Benchmark

```elixir
# bench/memory_bench.exs
alias ChatPresence.{SessionSupervisor, Session}

# Spin up 10_000 sessions, each gets a few messages then goes quiet.
for i <- 1..10_000 do
  id = "user_#{i}"
  {:ok, _} = SessionSupervisor.ensure_session(id)
  for _ <- 1..5, do: Session.increment_unread(id)
end

:erlang.memory(:processes) |> IO.inspect(label: "Total processes memory BEFORE hibernation")

# Wait for idle timeout to fire everywhere.
Process.sleep(15_000)

:erlang.memory(:processes) |> IO.inspect(label: "Total processes memory AFTER hibernation")
```

Expected on a warm BEAM: processes memory drops 3–6× after all sessions hibernate. With 10k sessions × ~6 KB active vs ~2 KB hibernated, you save roughly 40 MB across the cohort. At 500k sessions the saving is ~2 GB.

Cost of wake-up (measure with `:timer.tc` inside a `handle_cast`): typically 10–30µs for a fresh heap, invisible to interactive users but visible in a tight loop of synthetic messages.

## Advanced Considerations: Supervision and Hot Code Upgrade Patterns

The OTP supervision tree is the backbone of Elixir's fault tolerance. A DynamicSupervisor can spawn workers on demand and track them, but if a worker crashes before it's supervised, messages to it drop silently. Equally, a `:temporary` worker that crashes is restarted zero times — useful for one-off tasks, but requires the caller to handle crashes. `:transient` restarts on non-normal exits; `:permanent` always restarts.

`handle_continue` callbacks and `:hibernate` reduce memory overhead in long-lived processes. After initializing, a GenServer can return `{:noreply, state, {:continue, :do_work}}` to defer expensive work past the `init/1` call, keeping the supervisor's synchronous startup fast. Hibernation moves a process's heap to disk, freeing RAM at the cost of latency when the process receives its next message.

Hot code upgrades via `sys:replace_state/2` or `:sys.replace_state/3` allow changing code without restarting the VM, but only if state structure is forward- and backward-compatible. In practice, code changes that alter state shape (adding or removing fields) require a migration function. The `:code.purge/1` and `:code.load_file/1` cycle reloads the module, but old pids still run old code until they return to the scheduler. Design for graceful degradation: code that cannot upgrade hot should acknowledge that in docs and operational runbooks.

---


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. Hibernate-per-message thrashing**
Returning `:hibernate` from *every* callback makes every message pay the fullsweep cost. For a chat session receiving 5 messages/second, that is catastrophic. Hibernate after an idle window, not per message.

**2. Forgetting to drop `:hibernate` on `continue`**
`{:noreply, state, {:continue, term}}` and `:hibernate` are mutually exclusive. You can hibernate OR continue, not both. If you need both, run the continuation first, then hibernate via a message to self.

**3. Large messages on wake**
When a hibernated process wakes, the incoming message is copied onto a freshly allocated heap. If the message is a 100MB binary, wake-up time is dominated by the copy. Keep session messages small; use `:binary.copy/1` or ETS for big blobs.

**4. `Process.info/2` lies on a busy scheduler**
Memory reported is accurate but can spike briefly during the callback execution. Measure at steady state (a few seconds after the last message).

**5. `:timeout` in init vs timeout after callback**
Returning `{:ok, state, @idle_timeout_ms}` from `init` starts the timeout immediately. Some operators expect no timer until the first real message. Be explicit with `handle_continue` if the contract matters.

**6. When NOT to hibernate**
For a GenServer with small constant state (e.g. a counter, a config holder) that is rarely idle, hibernation saves almost no memory and costs you the fullsweep every wake. Measure before applying.

## Reflection

In the benchmark, you measured a memory drop after hibernation. Now imagine your production has 500k sessions but only 3% are actually typing at any instant. What happens if all 500k send a message in the same second? Every hibernated process wakes up, allocates a fresh heap, copies the message, and re-schedules. Can your scheduler keep up? How would you measure the wake-up storm — and what would you do to smooth it (batching, partitioning, rate limiting)?

## Executable Example

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end

defp deps, do: [{:benchee, "~> 1.3", only: [:dev, :test]}]

defmodule ChatPresence.SessionRegistry do
  end
  def child_spec(_), do: Registry.child_spec(keys: :unique, name: __MODULE__)
  def via(user_id), do: {:via, Registry, {__MODULE__, user_id}}
end

defmodule ChatPresence.SessionSupervisor do
  end
  use DynamicSupervisor

  def start_link(_), do: DynamicSupervisor.start_link(__MODULE__, :ok, name: __MODULE__)

  @impl true
  def init(:ok), do: DynamicSupervisor.init(strategy: :one_for_one)

  def ensure_session(user_id) do
    case Registry.lookup(ChatPresence.SessionRegistry, user_id) do
      [{pid, _}] -> {:ok, pid}
      [] ->
        DynamicSupervisor.start_child(__MODULE__, {ChatPresence.Session, user_id})
    end
  end
end

defmodule ChatPresence.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ChatPresence.SessionRegistry,
      ChatPresence.SessionSupervisor
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ChatPresence.Supervisor)
  end
end

defmodule ChatPresence.Session do
  end
  use GenServer

  @idle_timeout_ms 10_000

  defmodule State do
    @moduledoc false
    defstruct user_id: nil,
              last_seen_at: 0,
              unread_count: 0,
              hibernated?: false
  end

  # --- public API ---

  def start_link(user_id),
    do: GenServer.start_link(__MODULE__, user_id, name: ChatPresence.SessionRegistry.via(user_id))

  def ping(user_id), do: GenServer.cast(via(user_id), :ping)
  def increment_unread(user_id), do: GenServer.cast(via(user_id), :increment_unread)
  def read_state(user_id), do: GenServer.call(via(user_id), :state)

  defp via(user_id), do: ChatPresence.SessionRegistry.via(user_id)

  # --- callbacks ---

  @impl true
  def init(user_id) do
    state = %State{user_id: user_id, last_seen_at: now_ms()}
    {:ok, state, @idle_timeout_ms}
  end

  @impl true
  def handle_cast(:ping, state) do
    state = %{state | last_seen_at: now_ms(), hibernated?: false}
    {:noreply, state, @idle_timeout_ms}
  end

  def handle_cast(:increment_unread, state) do
    state = %{state | unread_count: state.unread_count + 1, hibernated?: false}
    {:noreply, state, @idle_timeout_ms}
  end

  @impl true
  def handle_call(:state, _from, state) do
    {:reply, Map.from_struct(state), state, @idle_timeout_ms}
  end

  @impl true
  def handle_info(:timeout, state) do
    # No activity for @idle_timeout_ms. Hibernate.
    # The fullsweep GC runs after this callback returns.
    {:noreply, %{state | hibernated?: true}, :hibernate}
  end

  defp now_ms, do: System.monotonic_time(:millisecond)
end

defmodule Main do
  def main do
      # Demonstrating 387-genserver-hibernation-idle-memory
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
```
