# Graceful shutdown and drain

**Project**: `drain_shutdown` — drain in-flight requests before `terminate/2` completes.

---

## Project context

You run an HTTP API that handles 800 rps with a p95 of 120 ms. Deploys happen 8 times a day. Each deploy kills the old VM — historically by sending `SIGKILL` after Kubernetes' 30 s grace period. Reality: the 30 s was being wasted because the app was not wired for graceful shutdown. Every deploy dropped ~100 in-flight requests, which returned `502 Bad Gateway` to clients. That was acceptable a year ago; now it's a user-facing SLO violation.

You need to implement a four-stage shutdown for your request-handling GenServer:

1. **Stop accepting new work** (reject fast, flip an "unavailable" flag).
2. **Wait for in-flight work to complete** (bounded by a drain timeout).
3. **Flush side effects** (commit pending DB writes, ship telemetry buffer).
4. **Exit cleanly** so the supervisor reports `:shutdown` not `:killed`.

Pattern: `Process.flag(:trap_exit, true)` in `init/1` to intercept the `:shutdown` signal from the supervisor, then run the drain in `terminate/2`. The parent supervisor must specify a `shutdown:` timeout ≥ your drain budget, otherwise the supervisor sends `:brutal_kill` before drain completes.

```
drain_shutdown/
├── lib/
│   └── drain_shutdown/
│       ├── application.ex
│       ├── server.ex          # the drainable GenServer
│       └── dispatcher.ex      # sends jobs, mimics the request layer
└── test/
    └── drain_shutdown/
        └── drain_test.exs
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
### 1. The shutdown signal path

```
Supervisor.terminate_child(sup, pid)
       │
       ├── sends {:EXIT, sup_pid, :shutdown} to the child
       │
       ├── waits up to `shutdown:` ms for child to exit
       │
       └── if still alive → Process.exit(pid, :kill)  # brutal
```

Without `trap_exit: true`, a `GenServer` receiving `{:EXIT, sup_pid, :shutdown}` exits immediately, skipping `terminate/2`. With `trap_exit`, the EXIT becomes a regular message, the `GenServer` runs `terminate/2`, and you have the full `shutdown:` budget to drain.

### 2. `shutdown:` values and their meaning

```elixir
%{id: MyServer,
  start: {MyServer, :start_link, []},
  shutdown: 10_000,      # ms
  restart: :permanent}
```

| Value | Behavior |
|---|---|
| `:brutal_kill` | `Process.exit(pid, :kill)` immediately. No `terminate/2`. |
| `:infinity` | Wait forever. Dangerous — hangs shutdown if drain hangs. |
| integer N (ms) | Wait N ms; then `Process.exit(pid, :kill)`. |

Pick N = (typical drain) + (safety margin). Never `:infinity` for workers; reserve it for nested supervisors only.

### 3. Application-wide shutdown timeout

`Application.stop/1` has its own timer (default `:infinity`). The OS supervisor (systemd, K8s) gives you a fixed window (K8s `terminationGracePeriodSeconds`, default 30 s). If your app's drain takes 60 s, K8s sends `SIGKILL` at 30 s regardless.

Align these three:

```
K8s terminationGracePeriodSeconds (30s)
    ≥  Application drain budget (25s)
        ≥  Root supervisor shutdown (20s)
            ≥  Leaf server shutdown (15s)
```

### 4. The four-stage drain pattern

```elixir
def terminate(reason, state) do
  # Stage 1: flip gate to reject new work.
  :ets.insert(:gate, {:accepting, false})

  # Stage 2: wait for in-flight work to drain.
  wait_for_drain(state, _deadline_ms = 10_000)

  # Stage 3: flush side effects.
  flush_buffer(state)

  # Stage 4: return — Supervisor will log reason.
  :ok
end
```

### 5. The gate pattern

A public "accepting" flag (ETS or `:persistent_term`) that the entry points check BEFORE doing work:

```elixir
def handle(req) do
  if accepting?() do
    GenServer.call(Server, {:handle, req})
  else
    {:error, :draining}
  end
end
```

The flag is set by the GenServer's `terminate/2` in stage 1. Readers do NOT go through the GenServer, so the flip is effective even if the GenServer is already handling 50 queued messages.

---

## Why a four-stage drain and not `Process.flag(:trap_exit, true)` alone

Trapping exits converts the supervisor's `:shutdown` signal into a `terminate/2` call — that is necessary but not sufficient. Without a gate that rejects new work, producers keep `call`-ing the GenServer during drain and push its mailbox beyond the shutdown budget. Without a deadline inside the drain loop, `terminate/2` blocks forever on a stuck downstream and the supervisor `:brutal_kill`s at `shutdown:` expiry — losing exactly the state you were trying to flush. The four stages (gate → drain → flush → exit) are the minimum needed to make the guarantee load-bearing.

---

## Design decisions

**Option A — accept everything and drain on terminate, no gate**
- Pros: simplest code; single state transition.
- Cons: producers keep writing into the mailbox during drain; the drain window never closes; `:brutal_kill` dominates under load.

**Option B — gate flag + bounded drain + flush** (chosen)
- Pros: producers see `:unavailable` immediately and back off; the drain budget is deterministic; flush happens once the mailbox is empty of in-flight work.
- Cons: more state (`draining?` flag), and every public callback must inspect it — a touch point that is easy to forget on new endpoints.

→ Chose **B** because the SLO is about not dropping in-flight requests, which requires a bounded closing window that only gating makes possible.

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
%{id: MyServer,
  start: {MyServer, :start_link, []},
  shutdown: 10_000,      # ms
  restart: :permanent}
```

| Value | Behavior |
|---|---|
| `:brutal_kill` | `Process.exit(pid, :kill)` immediately. No `terminate/2`. |
| `:infinity` | Wait forever. Dangerous — hangs shutdown if drain hangs. |
| integer N (ms) | Wait N ms; then `Process.exit(pid, :kill)`. |

Pick N = (typical drain) + (safety margin). Never `:infinity` for workers; reserve it for nested supervisors only.

### 3. Application-wide shutdown timeout

`Application.stop/1` has its own timer (default `:infinity`). The OS supervisor (systemd, K8s) gives you a fixed window (K8s `terminationGracePeriodSeconds`, default 30 s). If your app's drain takes 60 s, K8s sends `SIGKILL` at 30 s regardless.

Align these three:

```
K8s terminationGracePeriodSeconds (30s)
    ≥  Application drain budget (25s)
        ≥  Root supervisor shutdown (20s)
            ≥  Leaf server shutdown (15s)
```

### 4. The four-stage drain pattern

```elixir
def terminate(reason, state) do
  # Stage 1: flip gate to reject new work.
  :ets.insert(:gate, {:accepting, false})

  # Stage 2: wait for in-flight work to drain.
  wait_for_drain(state, _deadline_ms = 10_000)

  # Stage 3: flush side effects.
  flush_buffer(state)

  # Stage 4: return — Supervisor will log reason.
  :ok
end
```

### 5. The gate pattern

A public "accepting" flag (ETS or `:persistent_term`) that the entry points check BEFORE doing work:

```elixir
def handle(req) do
  if accepting?() do
    GenServer.call(Server, {:handle, req})
  else
    {:error, :draining}
  end
end
```

The flag is set by the GenServer's `terminate/2` in stage 1. Readers do NOT go through the GenServer, so the flip is effective even if the GenServer is already handling 50 queued messages.

---

## Why a four-stage drain and not `Process.flag(:trap_exit, true)` alone

Trapping exits converts the supervisor's `:shutdown` signal into a `terminate/2` call — that is necessary but not sufficient. Without a gate that rejects new work, producers keep `call`-ing the GenServer during drain and push its mailbox beyond the shutdown budget. Without a deadline inside the drain loop, `terminate/2` blocks forever on a stuck downstream and the supervisor `:brutal_kill`s at `shutdown:` expiry — losing exactly the state you were trying to flush. The four stages (gate → drain → flush → exit) are the minimum needed to make the guarantee load-bearing.

---

## Design decisions

**Option A — accept everything and drain on terminate, no gate**
- Pros: simplest code; single state transition.
- Cons: producers keep writing into the mailbox during drain; the drain window never closes; `:brutal_kill` dominates under load.

**Option B — gate flag + bounded drain + flush** (chosen)
- Pros: producers see `:unavailable` immediately and back off; the drain budget is deterministic; flush happens once the mailbox is empty of in-flight work.
- Cons: more state (`draining?` flag), and every public callback must inspect it — a touch point that is easy to forget on new endpoints.

→ Chose **B** because the SLO is about not dropping in-flight requests, which requires a bounded closing window that only gating makes possible.

---

## Implementation

### Dependencies (`mix.exs`)

```elixir
defp deps do
  []
end
```

### Step 1: Application

**Objective**: Define the OTP application and wire the supervision tree.

```elixir
# lib/drain_shutdown/application.ex
defmodule DrainShutdown.Application do
  use Application

  @impl true
  def start(_type, _args) do
    :ets.new(:drain_gate, [:named_table, :public, read_concurrency: true])
    :ets.insert(:drain_gate, {:accepting, true})

    children = [
      %{
        id: DrainShutdown.Server,
        start: {DrainShutdown.Server, :start_link, []},
        shutdown: 15_000,
        restart: :permanent
      }
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,
      name: DrainShutdown.Supervisor
    )
  end
end
```

### Step 2: The drainable server

**Objective**: Implement The drainable server.

```elixir
# lib/drain_shutdown/server.ex
defmodule DrainShutdown.Server do
  @moduledoc """
  Request-handling GenServer with four-stage graceful shutdown.
  """
  use GenServer
  require Logger

  @drain_deadline_ms 10_000

  # ---------------------------------------------------------------------------
  # Public API — the gate is on the CLIENT side for fast rejection.
  # ---------------------------------------------------------------------------

  @spec handle(term()) :: {:ok, term()} | {:error, :draining}
  def handle(req) do
    if accepting?() do
      GenServer.call(__MODULE__, {:handle, req}, 30_000)
    else
      {:error, :draining}
    end
  end

  @spec in_flight() :: non_neg_integer()
  def in_flight, do: GenServer.call(__MODULE__, :in_flight)

  defp accepting? do
    case :ets.lookup(:drain_gate, :accepting) do
      [{:accepting, true}] -> true
      _ -> false
    end
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # Without trap_exit, terminate/2 is NOT called on :shutdown from supervisor.
    Process.flag(:trap_exit, true)
    {:ok, %{in_flight: 0, buffer: []}}
  end

  @impl true
  def handle_call({:handle, req}, from, state) do
    # Simulate async work: spawn a task, reply when done.
    parent = self()
    ref = make_ref()

    Task.start(fn ->
      Process.sleep(100)
      send(parent, {:work_done, ref, from, {:ok, {:handled, req}}})
    end)

    {:noreply, %{state | in_flight: state.in_flight + 1}}
  end

  def handle_call(:in_flight, _from, state), do: {:reply, state.in_flight, state}

  @impl true
  def handle_info({:work_done, _ref, from, reply}, state) do
    GenServer.reply(from, reply)
    {:noreply, %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("[drain] starting, reason=#{inspect(reason)}, in_flight=#{state.in_flight}")

    # Stage 1: stop accepting new requests.
    :ets.insert(:drain_gate, {:accepting, false})

    # Stage 2: wait for in-flight work to complete.
    final_state = wait_for_drain(state, System.monotonic_time(:millisecond) + @drain_deadline_ms)

    # Stage 3: flush buffer.
    flushed = flush_buffer(final_state.buffer)
    Logger.info("[drain] flushed #{flushed} buffered replies")

    # Stage 4: return :ok so Supervisor logs :shutdown cleanly.
    :ok
  end

  # ---------------------------------------------------------------------------
  # Drain loop — process messages ourselves while draining.
  # ---------------------------------------------------------------------------

  defp wait_for_drain(%{in_flight: 0} = state, _deadline), do: state

  defp wait_for_drain(state, deadline) do
    now = System.monotonic_time(:millisecond)

    if now >= deadline do
      Logger.warning("[drain] deadline exceeded with #{state.in_flight} in flight")
      state
    else
      remaining = deadline - now

      receive do
        {:work_done, _ref, from, reply} ->
          GenServer.reply(from, reply)
          new_state = %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}
          wait_for_drain(new_state, deadline)
      after
        remaining -> state
      end
    end
  end

  defp flush_buffer(buffer), do: length(buffer)
end
```

### Step 3: Tests

**Objective**: Add tests that cover the expected behavior and edge cases.

```elixir
# test/drain_shutdown/drain_test.exs
defmodule DrainShutdown.DrainTest do
  use ExUnit.Case, async: false

  alias DrainShutdown.Server

  setup do
    :ets.insert(:drain_gate, {:accepting, true})
    :ok
  end

  describe "DrainShutdown.Drain" do
    test "accepts requests under normal conditions" do
      assert {:ok, {:handled, :ping}} = Server.handle(:ping)
    end

    test "terminate drains in-flight work before returning" do
      # Kick off 5 in-flight requests.
      tasks = for i <- 1..5, do: Task.async(fn -> Server.handle({:req, i}) end)
      Process.sleep(20)

      # Manually invoke terminate under controlled conditions.
      pid = Process.whereis(Server)
      ref = Process.monitor(pid)

      # Send a :shutdown exit like the supervisor would.
      Process.exit(pid, :shutdown)

      assert_receive {:DOWN, ^ref, :process, ^pid, :shutdown}, 15_000

      # All in-flight tasks should have received a reply (not a timeout/exit).
      results = Task.await_many(tasks, 15_000)
      assert Enum.all?(results, &match?({:ok, {:handled, _}}, &1))
    end

    test "rejects new work after gate flips" do
      :ets.insert(:drain_gate, {:accepting, false})
      assert {:error, :draining} = Server.handle(:new_req)
    end
  end
end
```

### Why this works

`trap_exit` turns the supervisor's `:shutdown` into a `terminate/2` call instead of an instant death. The ETS-backed gate flag flips in `terminate/2` and is read by public callbacks *without* going through the GenServer itself — so callers get `:unavailable` instantly instead of queuing behind the draining process. The bounded `receive` loop in `terminate/2` processes remaining `:work_done` signals until either the in-flight counter hits zero or the drain deadline expires; either way `terminate/2` returns within the `shutdown:` budget, so the supervisor never has to fall back to `:brutal_kill`.

---

## Advanced Considerations: Partitioned Supervisors and Custom Restart Strategies

A standard Supervisor is a single process managing a static tree. For thousands of children, a single supervisor becomes a bottleneck: all supervisor callbacks run on one process, and supervisor restart logic is sequential. PartitionSupervisor (OTP 25+) spawns N independent supervisors, each managing a subset of children. Hashing the child ID determines which partition supervises it, distributing load and enabling horizontal scaling.

Custom restart strategies (via `Supervisor.init/2` callback) allow logic beyond the defaults. A strategy might prioritize restarting dependent services in a specific order, or apply backoff based on restart frequency. The downside is complexity: custom logic is harder to test and reason about, and mistakes cascade. Start with defaults and profile before adding custom behavior.

Selective restart via `:rest_for_one` or `:one_for_all` affects failure isolation. `:one_for_all` restarts all children when one fails (simulating a total system failure), which can be necessary for consistency but is expensive. `:rest_for_one` restarts the failed child and any started after it, balancing isolation and dependencies. Understanding which strategy fits your architecture prevents cascading failures and unnecessary restarts.

---


## Deep Dive: Property Patterns and Production Implications

Property-based testing inverts the testing mindset: instead of writing examples, you state invariants (properties) and let a generator find counterexamples. StreamData's shrinking capability is its superpower—when a property fails on a 10,000-element list, the framework reduces it to the minimal list that still fails, cutting debugging time from hours to minutes. The trade-off is that properties require rigorous thinking about domain constraints, and not every invariant is worth expressing as a property. Teams that adopt property testing often find bugs in specifications themselves, not just implementations.

---

## Trade-offs and production gotchas

**1. `trap_exit` + slow `terminate/2` + short `shutdown:` = brutal kill.** The supervisor enforces `shutdown:`. If your drain takes 20 s but `shutdown: 5_000`, at 5 s the supervisor sends `:kill` and `terminate/2` is interrupted mid-flush. Always: `shutdown:` ≥ drain budget + 2s safety.

**2. `terminate/2` runs in the GenServer's process — it still receives messages.** Casts and calls keep arriving during drain. The `receive` loop above only handles `:work_done`; other messages pile up in the mailbox. Either selectively receive (as shown) or explicitly drain the mailbox.

**3. The gate must be accessible without going through the GenServer.** If `accepting?/0` calls `GenServer.call(Server, :accepting?)`, and the GenServer is busy draining, new callers block on the call. ETS or `:persistent_term` with O(1) read is the correct primitive.

**4. `:persistent_term.put/2` is NOT fast on hot paths.** Each put triggers a global GC scan. Use it ONCE at shutdown, not per-request. ETS is the better fit for flipping state during normal operation.

**5. K8s `preStop` hook races with `SIGTERM`.** K8s sends `preStop` THEN `SIGTERM` simultaneously. If your app starts draining on `SIGTERM` but your Service endpoint still routes traffic for 2 s (endpoint propagation delay), your "unavailable" window accepts traffic. Solution: `preStop` sleep(3s) + separate readiness probe that fails as soon as drain starts.

**6. `terminate/2` does NOT run on `:brutal_kill`, VM crash, or `Process.exit(pid, :kill)`.** It is a best-effort hook. Durability invariants belong in the DB, not `terminate/2`. Idempotent writes + DB transactions are the real guarantee.

**7. Monitored callers see `:noproc` not `:draining`.** If a request is in `GenServer.call` when the GenServer dies, the caller gets `{:noproc, _}` or `:timeout`. Wrap all calls with a try/catch or make them idempotent at the client layer.

**8. When NOT to use this.** For stateless workers whose loss is harmless (pure in-memory caches, telemetry aggregators that re-read on startup), `:brutal_kill` is simpler and faster. Drain is for processes with external side effects or user-visible replies.

---

## Benchmark

`Process.flag(:trap_exit, true)` has no measurable runtime cost — it's a single bit on the PCB. The drain itself is bounded by the slowest in-flight job; measure it with `:timer.tc/1` wrapped around the supervisor's terminate call.

ETS lookups for the gate are ~50 ns. `:persistent_term.get/1` is ~20 ns but with global GC cost on writes — the trade-off favors ETS for frequently-flipped flags.

Target: drain completes within `shutdown:` budget for 99% of deploys; new-request rejection latency ≤ 1 µs (ETS read); zero `502`s attributable to in-flight loss after deploy.

---

## Reflection

1. Your deploys finish drain in 4 s on average but once a week a single slow request pushes it past the 10 s `shutdown:` budget and the supervisor `:brutal_kill`s. Do you raise `shutdown:`, add a per-request timeout that fails the one slow request, or split the GenServer into two (fast + slow paths)? What's the cost of each under a deploy-every-hour cadence?
2. K8s sends `SIGTERM` and removes the pod from Service endpoints simultaneously, but endpoint propagation takes ~2 s. Your gate flips immediately, so for 2 s clients that still resolve to this pod hit a `:unavailable` response. How do you close that window without adding a `Process.sleep` to your `terminate/2`?

---


## Executable Example

```elixir
# lib/drain_shutdown/server.ex
defmodule DrainShutdown.Server do
  @moduledoc """
  Request-handling GenServer with four-stage graceful shutdown.
  """
  use GenServer
  require Logger

  @drain_deadline_ms 10_000

  # ---------------------------------------------------------------------------
  # Public API — the gate is on the CLIENT side for fast rejection.
  # ---------------------------------------------------------------------------

  @spec handle(term()) :: {:ok, term()} | {:error, :draining}
  def handle(req) do
    if accepting?() do
      GenServer.call(__MODULE__, {:handle, req}, 30_000)
    else
      {:error, :draining}
    end
  end

  @spec in_flight() :: non_neg_integer()
  def in_flight, do: GenServer.call(__MODULE__, :in_flight)

  defp accepting? do
    case :ets.lookup(:drain_gate, :accepting) do
      [{:accepting, true}] -> true
      _ -> false
    end
  end

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  # ---------------------------------------------------------------------------
  # Callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    # Without trap_exit, terminate/2 is NOT called on :shutdown from supervisor.
    Process.flag(:trap_exit, true)
    {:ok, %{in_flight: 0, buffer: []}}
  end

  @impl true
  def handle_call({:handle, req}, from, state) do
    # Simulate async work: spawn a task, reply when done.
    parent = self()
    ref = make_ref()

    Task.start(fn ->
      Process.sleep(100)
      send(parent, {:work_done, ref, from, {:ok, {:handled, req}}})
    end)

    {:noreply, %{state | in_flight: state.in_flight + 1}}
  end

  def handle_call(:in_flight, _from, state), do: {:reply, state.in_flight, state}

  @impl true
  def handle_info({:work_done, _ref, from, reply}, state) do
    GenServer.reply(from, reply)
    {:noreply, %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}}
  end

  @impl true
  def terminate(reason, state) do
    Logger.info("[drain] starting, reason=#{inspect(reason)}, in_flight=#{state.in_flight}")

    # Stage 1: stop accepting new requests.
    :ets.insert(:drain_gate, {:accepting, false})

    # Stage 2: wait for in-flight work to complete.
    final_state = wait_for_drain(state, System.monotonic_time(:millisecond) + @drain_deadline_ms)

    # Stage 3: flush buffer.
    flushed = flush_buffer(final_state.buffer)
    Logger.info("[drain] flushed #{flushed} buffered replies")

    # Stage 4: return :ok so Supervisor logs :shutdown cleanly.
    :ok
  end

  # ---------------------------------------------------------------------------
  # Drain loop — process messages ourselves while draining.
  # ---------------------------------------------------------------------------

  defp wait_for_drain(%{in_flight: 0} = state, _deadline), do: state

  defp wait_for_drain(state, deadline) do
    now = System.monotonic_time(:millisecond)

    if now >= deadline do
      Logger.warning("[drain] deadline exceeded with #{state.in_flight} in flight")
      state
    else
      remaining = deadline - now

      receive do
        {:work_done, _ref, from, reply} ->
          GenServer.reply(from, reply)
          new_state = %{state | in_flight: state.in_flight - 1, buffer: [reply | state.buffer]}
          wait_for_drain(new_state, deadline)
      after
        remaining -> state
      end
    end
  end

  defp flush_buffer(buffer), do: length(buffer)
end

defmodule Main do
  def main do
      # Demonstrate graceful shutdown with drain on deploy

      # Set up the drain gate ETS table (used for controlling acceptance)
      :ets.new(:drain_gate, [:named_table, :public, {:write_concurrency, true}])
      :ets.insert(:drain_gate, {:accepting, true})

      # Start the drain server
      {:ok, server_pid} = DrainShutdown.Server.start_link(name: DrainShutdown.Server)
      assert is_pid(server_pid), "Server must start"
      IO.inspect(server_pid, label: "DrainShutdown.Server PID")

      # Dispatch some work while accepting is true
      {:ok, task_pid_1} = DrainShutdown.Dispatcher.send_work(
        DrainShutdown.Server,
        {:query, "SELECT * FROM users"}
      )
      assert is_pid(task_pid_1), "Task should be created"

      {:ok, task_pid_2} = DrainShutdown.Dispatcher.send_work(
        DrainShutdown.Server,
        {:query, "SELECT COUNT(*) FROM orders"}
      )
      assert is_pid(task_pid_2), "Second task should be created"

      # Give tasks a moment to complete
      Process.sleep(100)

      IO.puts("✓ Server initialized with accepting=true")
      IO.puts("✓ Dispatched tasks for processing")

      # Now simulate graceful shutdown: stop accepting, drain in-flight
      # This will trigger the four-stage drain:
      # Stage 1: stop accepting new requests (flip flag)
      :ets.insert(:drain_gate, {:accepting, false})
      IO.puts("✓ Stage 1: Stopped accepting new requests")

      # Try to send work while draining (should fail or queue)
      drain_result = DrainShutdown.Dispatcher.send_work(
        DrainShutdown.Server,
        {:query, "SELECT * FROM products"}
      )
      # Either {:error, :unavailable} or queued depending on implementation
      IO.inspect(drain_result, label: "Work submission during drain")

      # Stage 2: Wait for in-flight to drain (handled by terminate/2)
      IO.puts("✓ Stage 2: Waiting for in-flight work to complete...")

      # Stage 3 & 4: Graceful termination
      ref = Process.monitor(server_pid)
      Supervisor.terminate_child(DrainShutdown.Supervisor, DrainShutdown.Server)

      assert_receive {:DOWN, ^ref, :process, ^server_pid, :shutdown}, 2_000
      IO.puts("✓ Stage 3 & 4: Buffer flushed and server shutdown cleanly")

      # Verify the server is down
      assert Process.whereis(DrainShutdown.Server) == nil,
        "Server should be stopped"

      IO.puts("\n✓ Graceful shutdown with drain demonstrated:")
      IO.puts("  - Stage 1: Stop accepting (flip gate)")
      IO.puts("  - Stage 2: Drain in-flight requests (wait_for_drain)")
      IO.puts("  - Stage 3: Flush buffers and side effects")
      IO.puts("  - Stage 4: Exit with :shutdown (no :brutal_kill)")
      IO.puts("✓ Drain timeout ensures bounded shutdown window")
      IO.puts("✓ Ready for zero-downtime deploys")

      # Clean up
      :ets.delete(:drain_gate)
  end
end

Main.main()
```
