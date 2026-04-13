# Chaos Engineering with Process Suspension and Kill

**Project**: `chaos_lab` — controlled fault injection that suspends, slows, or kills target processes at runtime so you can verify supervision trees, timeouts, and retry logic under realistic failure modes.

## Project context

Your system has a supervision tree with 30+ workers. Code review says "if the `Pricing` GenServer dies, `Orders` will retry". Production has never actually lost Pricing, so nobody knows if the retry path works. Adding a feature is risky because the failure path is untested.

Chaos engineering (Netflix, Gremlin) deliberately induces failures in running systems to validate resilience. On the BEAM we have two cheap primitives the JVM doesn't: `:erlang.suspend_process/1` freezes a process until resumed (no OS equivalent), and `Process.exit(pid, :kill)` terminates it unconditionally. Combined with a small scheduler, we can simulate slow dependencies, total outages, and partial degradation.

```
chaos_lab/
├── lib/
│   └── chaos_lab/
│       ├── application.ex
│       ├── chaos.ex                # public API
│       ├── victim.ex               # example target GenServer
│       └── scheduler.ex            # time-based experiments
├── test/
│   └── chaos_lab/
│       └── chaos_test.exs
└── mix.exs
```

## Why chaos and not just unit tests

Unit tests exercise code; chaos exercises the *system*. A unit test for retry logic doesn't tell you if the supervisor restart interval is too aggressive, if the connection pool recovers, if telemetry fires. Chaos reveals the gap between "my logic is correct" and "my system is correct".

## Why not libraries like `chaos_monkey`

`chaos_monkey` and friends are thin wrappers over `Process.exit`. Building this from scratch exposes the BEAM-specific primitives (`suspend_process`, `erlang:garbage_collect/1`) that give you more failure modes than "kill".

## Core concepts

### 1. Suspend: freeze without kill
```
:erlang.suspend_process(pid)   # process runs 0 instructions until resumed
:erlang.resume_process(pid)    # back to normal
```
Messages to a suspended process accumulate in its mailbox. Callers see timeouts. Upon resume the process drains its mailbox. Perfect for simulating a stalled service.

### 2. Latency injection: inject a sleep in message handling
We do this *externally* by putting the target behind a middleware GenServer that delays forwarding, rather than modifying the target.

### 3. Kill with exit reasons
- `Process.exit(pid, :kill)` — unconditional, bypasses trap_exit.
- `Process.exit(pid, :shutdown)` — supervisor-initiated clean shutdown.
- `Process.exit(pid, :crash)` — simulates an uncaught exception with a specific reason.

### 4. Scheduler
```
Scheduler.schedule(:pricing, :kill, in_ms: 5_000)
```
Chaos events triggered deterministically at chosen times; stop any time with `Scheduler.cancel/1`.

## Design decisions

- **Option A — Monkey-patch target modules**: intrusive, requires code changes.
- **Option B — External process operations (`suspend`, `exit`)**: target unchanged, chaos fully orthogonal.
→ Chose **B**. Non-invasive is the whole point.

- **Option A — Fire chaos immediately from test**: simple.
- **Option B — Scheduler that fires later**: needed for experiments like "kill 10s into a 30s load test".
→ Support **both**.

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
defmodule ChaosLab.MixProject do
  use Mix.Project
  def project, do: [app: :chaos_lab, version: "0.1.0", elixir: "~> 1.17", deps: []]
  def application, do: [mod: {ChaosLab.Application, []}, extra_applications: [:logger]]
end
```

### Step 1: Application

**Objective**: Register victim and scheduler as supervised children so chaos events trigger against stable, restartable processes and restart behavior remains observable.

```elixir
defmodule ChaosLab.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      ChaosLab.Scheduler,
      {ChaosLab.Victim, name: :victim_demo}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end
```

### Step 2: Victim (`lib/chaos_lab/victim.ex`)

**Objective**: Implement call/catch wrapper that converts :timeout and :noproc exits to typed {:error, reason} so experiments detect failure modes deterministically.

```elixir
defmodule ChaosLab.Victim do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: opts[:name])

  def ping(name, timeout \\ 1_000) do
    try do
      GenServer.call(name, :ping, timeout)
    catch
      :exit, {:timeout, _} -> {:error, :timeout}
      :exit, {:noproc, _} -> {:error, :noproc}
    end
  end

  @impl true
  def init(_opts), do: {:ok, %{count: 0}}

  @impl true
  def handle_call(:ping, _from, state) do
    {:reply, {:ok, state.count + 1}, %{state | count: state.count + 1}}
  end
end
```

### Step 3: Chaos (`lib/chaos_lab/chaos.ex`)

**Objective**: Expose suspend/resume/:kill primitives via thin API so chaos scenarios are auditable and distinguish stall (suspend) from total outage (kill).

```elixir
defmodule ChaosLab.Chaos do
  @moduledoc "Fault-injection primitives. Use in staging; never in prod without a kill switch."

  @spec suspend(pid() | atom()) :: :ok | {:error, :noproc}
  def suspend(target) do
    with pid when is_pid(pid) <- resolve(target) do
      :erlang.suspend_process(pid)
      :ok
    end
  end

  @spec resume(pid() | atom()) :: :ok | {:error, :noproc}
  def resume(target) do
    with pid when is_pid(pid) <- resolve(target) do
      :erlang.resume_process(pid)
      :ok
    end
  end

  @spec kill(pid() | atom(), term()) :: :ok | {:error, :noproc}
  def kill(target, reason \\ :kill) do
    with pid when is_pid(pid) <- resolve(target) do
      Process.exit(pid, reason)
      :ok
    end
  end

  @doc """
  Suspend the target for `ms` and resume. Runs in the caller process,
  so spawn in a Task if you don't want to block.
  """
  @spec pause(pid() | atom(), pos_integer()) :: :ok | {:error, :noproc}
  def pause(target, ms) do
    with :ok <- suspend(target) do
      Process.sleep(ms)
      resume(target)
    end
  end

  defp resolve(target) when is_pid(target), do: target

  defp resolve(target) when is_atom(target) do
    case Process.whereis(target) do
      nil -> {:error, :noproc}
      pid -> pid
    end
  end
end
```

### Step 4: Scheduler (`lib/chaos_lab/scheduler.ex`)

**Objective**: Track scheduled chaos events in GenServer state so experiments are cancellable and reproducible instead of scattered in Process.send_after calls.

```elixir
defmodule ChaosLab.Scheduler do
  use GenServer
  alias ChaosLab.Chaos

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def schedule(id, action, opts) do
    GenServer.call(__MODULE__, {:schedule, id, action, opts})
  end

  def cancel(id), do: GenServer.call(__MODULE__, {:cancel, id})

  @impl true
  def init(_), do: {:ok, %{}}

  @impl true
  def handle_call({:schedule, id, action, opts}, _from, state) do
    delay = Keyword.fetch!(opts, :in_ms)
    target = Keyword.fetch!(opts, :target)

    timer = Process.send_after(self(), {:fire, id, action, target, opts}, delay)
    {:reply, :ok, Map.put(state, id, timer)}
  end

  def handle_call({:cancel, id}, _from, state) do
    case Map.pop(state, id) do
      {nil, _} -> {:reply, :not_found, state}
      {timer, new_state} -> Process.cancel_timer(timer); {:reply, :ok, new_state}
    end
  end

  @impl true
  def handle_info({:fire, _id, :kill, target, _opts}, state) do
    Chaos.kill(target)
    {:noreply, state}
  end

  def handle_info({:fire, _id, :suspend, target, _opts}, state) do
    Chaos.suspend(target)
    {:noreply, state}
  end

  def handle_info({:fire, _id, {:pause, ms}, target, _opts}, state) do
    Task.start(fn -> Chaos.pause(target, ms) end)
    {:noreply, state}
  end
end
```

## Why this works

- **`:erlang.suspend_process/1` is a primitive** — it stops the BEAM scheduler from scheduling that process. No message is sent, no state changes. Truly paused until resumed.
- **Suspended processes accumulate mailbox** — callers hit their `GenServer.call` timeout. When resumed, the process drains: excellent for testing what happens when a service recovers from a stall.
- **`kill` bypasses `trap_exit`** — the only way to test a truly "server crashed" scenario. Reasons other than `:kill` *can* be trapped; this is how you test graceful shutdown.
- **Scheduler decouples fire time from setup time** — lets you describe the experiment upfront (`"kill victim at t+5s, suspend at t+10s for 2s"`) and run it reproducibly.

## Tests

```elixir
defmodule ChaosLab.ChaosTest do
  use ExUnit.Case, async: false
  alias ChaosLab.{Chaos, Scheduler, Victim}

  describe "baseline" do
    test "victim responds to ping" do
      assert {:ok, _} = Victim.ping(:victim_demo)
    end
  end

  describe "suspend" do
    test "ping times out while suspended, recovers after resume" do
      :ok = Chaos.suspend(:victim_demo)
      assert {:error, :timeout} = Victim.ping(:victim_demo, 50)

      :ok = Chaos.resume(:victim_demo)
      assert {:ok, _} = Victim.ping(:victim_demo, 500)
    end
  end

  describe "pause" do
    test "blocks for the duration then resumes" do
      Task.start(fn -> Chaos.pause(:victim_demo, 100) end)
      Process.sleep(10)

      assert {:error, :timeout} = Victim.ping(:victim_demo, 30)

      Process.sleep(150)
      assert {:ok, _} = Victim.ping(:victim_demo)
    end
  end

  describe "kill" do
    test "victim is restarted by supervisor" do
      pid_before = Process.whereis(:victim_demo)
      :ok = Chaos.kill(:victim_demo)

      Process.sleep(50)
      pid_after = Process.whereis(:victim_demo)

      assert pid_after != nil
      assert pid_after != pid_before
    end
  end

  describe "scheduler" do
    test "fires kill at scheduled time" do
      pid_before = Process.whereis(:victim_demo)
      Scheduler.schedule(:exp1, :kill, in_ms: 30, target: :victim_demo)
      Process.sleep(100)

      pid_after = Process.whereis(:victim_demo)
      assert pid_after != pid_before
    end

    test "cancel prevents the event" do
      pid_before = Process.whereis(:victim_demo)
      Scheduler.schedule(:exp2, :kill, in_ms: 100, target: :victim_demo)
      :ok = Scheduler.cancel(:exp2)
      Process.sleep(150)

      pid_after = Process.whereis(:victim_demo)
      assert pid_after == pid_before
    end
  end
end
```

## Benchmark

Not a throughput benchmark — chaos is about correctness, not speed. A sanity check:

```elixir
# Kill/restart cycle should complete in single-digit ms.
{t, _} = :timer.tc(fn ->
  :ok = ChaosLab.Chaos.kill(:victim_demo)
  wait_for_restart(:victim_demo, 10)
end)
IO.puts("kill+restart: #{t / 1_000} ms")

defp wait_for_restart(name, attempts_left) when attempts_left > 0 do
  case Process.whereis(name) do
    nil -> Process.sleep(1); wait_for_restart(name, attempts_left - 1)
    pid -> pid
  end
end
```

Expected: 1-5ms on a modern laptop.

## Advanced Considerations: Circuit Breakers and Bulkheads in Production

A circuit breaker monitors downstream service health and rejects new requests when failures exceed a threshold, failing fast instead of queuing indefinitely. States: `:closed` (normal), `:open` (fast-fail), `:half_open` (testing recovery). A timeout-based pattern monitors; once requests succeed again, the circuit closes. Half-open tests with a single request; if it succeeds, all requests resume.

Bulkheads isolate resource pools so one slow endpoint doesn't starve others. A GenServer pool with a bounded queue (e.g., `:queue.len(state) >= 100`) can return `{:error, :overloaded}` immediately, preventing queue buildup. Combined with exponential backoff on the client (caller retries with increasing delays), this creates a natural circuit breaker behavior without explicit state.

Graceful degradation means serving stale data or reduced functionality when a service is slow. A cached value with a 5-minute TTL is acceptable for many reads; serve it if the live source is timing out. Feature flags allow disabling expensive operations at runtime. Cascading timeout windows (outer service times out after 5s, inner calls must complete in 3s) prevent unbounded waiting. The cost is complexity: tracking degradation modes, testing failure scenarios, and ensuring data consistency under partial failures.

---


## Deep Dive: Resilience Patterns and Production Implications

Resilience patterns (circuit breakers, timeouts, retries) are easy to implement but hard to test. The insight is that resilience patterns must be tested under failure: timeouts matter only when calls actually take time, retries matter only when transient failures occur. Production systems with untested resilience patterns often fail gracefully in test and catastrophically in production.

---

## Trade-offs and production gotchas

**1. `suspend_process` in production is dangerous** — if the suspended process owns a critical lock (ETS table, socket), suspending it can deadlock unrelated consumers. Only suspend processes you own and understand.

**2. Chaos in prod needs a kill switch** — always guard chaos experiments behind a feature flag with short TTL. Netflix's Chaos Monkey stops firing automatically if error rates spike.

**3. `Process.exit(pid, :kill)` bypasses trap_exit** — intentional. Use `:normal` to *not* trigger a supervisor restart; use `:kill` to force one.

**4. Mailbox backlog post-resume** — a process suspended for 10s at 1000 msg/s resumes with 10,000 messages to process. If downstream also stalled, you can cascade. Bound the mailbox at the source.

**5. `Scheduler` is a single point of failure** — if the Scheduler crashes, scheduled events are lost. For production-grade chaos use an external orchestrator (Gremlin).

**6. When NOT to run chaos** — not during incidents, not without a rollback, not on databases you can't recreate. First chaos experiment should target a single worker, not your Postgres primary.

## Reflection

You run `Chaos.pause(:payments, 5_000)` during a load test. Checkout p99 latency spikes to 5s as expected. After resume, what do you observe in the 10 seconds following? Why?

## Resources

- [Chaos Engineering — Principles (principlesofchaos.org)](https://principlesofchaos.org/)
- [`:erlang.suspend_process/1` — Erlang docs](https://www.erlang.org/doc/man/erlang.html#suspend_process-1)
- [Netflix Chaos Monkey](https://github.com/Netflix/chaosmonkey)
- [Learn You Some Erlang — processes chapter](https://learnyousomeerlang.com/the-hitchhikers-guide-to-concurrency)
