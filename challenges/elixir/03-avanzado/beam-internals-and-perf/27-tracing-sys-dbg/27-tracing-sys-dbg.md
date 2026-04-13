# Live tracing with :sys, :dbg, and :recon_trace

**Project**: `tracing_toolkit` — safe, production-grade tracing helpers that don't require a restart.

---

## Project context

A customer reports a stuck checkout. Logs show the order was created but the
`PaymentWorker` never moved it to `:charged`. The node is live and serving 10k
concurrent sessions — you cannot restart it, you cannot deploy more logs, you
need answers *now*.

BEAM's built-in tracing (`:sys`, `:erlang.trace`, `:dbg`) lets you observe a
running process without changing its code. It's one of the platform's
superpowers — and it's also a foot-gun. `:dbg.tp(:_, :_, [])` on a busy
production node will melt it in seconds.

You are building `tracing_toolkit`, a thin Elixir API that exposes the useful
tracing primitives with *safety rails*: hard message-count limits, mandatory
timeouts, per-process scoping, and an always-on killswitch.

Project structure:

```
tracing_toolkit/
├── lib/
│   └── tracing_toolkit/
│       ├── sys_trace.ex        # :sys.trace on/off, get_state, replace_state
│       ├── call_trace.ex       # :recon_trace wrapper with limits
│       ├── msg_trace.ex        # message-level tracing between processes
│       └── killswitch.ex       # a GenServer that stops everything on a panic
├── test/
│   └── tracing_toolkit/
│       ├── sys_trace_test.exs
│       └── call_trace_test.exs
└── mix.exs
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

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
### 1. Three levels of tracing

```
┌─────────────────────────────────────────────────────────────┐
│ 1. :sys.trace/2  — OTP-aware, per-process                   │
│    - Only works on processes using :gen, :gen_server, etc.  │
│    - Logs callbacks, state transitions, replies             │
│    - Zero risk on a single process. Great first step.       │
├─────────────────────────────────────────────────────────────┤
│ 2. :recon_trace.calls/3  — function-call tracing            │
│    - Any MFA, any process, with match specs                 │
│    - Hard-limited by message count (critical!)              │
│    - ~1 µs overhead per traced call                         │
├─────────────────────────────────────────────────────────────┤
│ 3. :erlang.trace/3 + :dbg — full-power, dangerous           │
│    - Sends a message for every traced event                 │
│    - Can send millions/sec — floods the tracer mailbox      │
│    - Only with explicit rate-limit or `:recon_trace`        │
└─────────────────────────────────────────────────────────────┘
```

Default rule: **always start with `:sys.trace/2`**. Escalate to
`:recon_trace` only if you need function-level visibility. Touch raw
`:erlang.trace/3` only if you've already profiled the tracer impact.

### 2. `:sys.get_state/1` and `:sys.replace_state/2`

Every OTP behaviour process responds to `:sys` system messages. You can
inspect and even mutate its state from iex. This is often enough to
resolve an incident:

```
iex> :sys.get_state(PaymentWorker)
%State{pending: [...], retries: 17, last_error: :timeout}
```

`:sys.replace_state/2` is the emergency escape hatch — e.g., unstick a
state machine by dropping a bad message from a queue. Use sparingly and
leave a runbook entry.

### 3. `:recon_trace.calls/3` with hard limits

```
:recon_trace.calls(
  {Module, :fun, :_},       # MFA pattern
  10,                       # max messages before auto-off
  [scope: :local, time: 5_000]  # 5-second hard timeout
)
```

The two parameters that keep you safe:

- **count**: stop after N trace events. Default cannot be unlimited.
- **time**: stop after T milliseconds no matter what.

If either fires, `:recon_trace` calls `:dbg.stop()` on its behalf. This
is why `:recon_trace` is what Erlang shops actually run in prod, not
raw `:dbg`.

### 4. Match specs: filter before send

A trace message is formed *before* filtering only if you use the naive
path. Match specs run in-VM before a trace message is produced, so you
can filter on arguments without paying the enqueue cost:

```
# only trace calls where the first arg is "vip_user_123"
:recon_trace.calls(
  {MyApp.Billing, :charge, fn [user_id, _amount] when user_id == "vip_user_123" -> :return_trace end},
  50
)
```

This is the difference between "trace drowns the node" and "trace shows
exactly 3 messages".

### 5. Tracer process and back-pressure

Every trace event is a message delivered to a *tracer process* (by
default, the shell that issued `:dbg.tpl`). If you trace 100k calls/s
and the tracer is slow, its mailbox grows — which steals memory and
eventually crashes the tracer and possibly the node. Mitigations:

- Always use `:recon_trace` count/time limits.
- Send traces to a dedicated file tracer (`:dbg.tracer(:port, ...)`).
- Never trace the `Logger` backend, `:gen_server`, or anything on the
  critical path of your own tracing.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: project

**Objective**: Scaffold supervised app so Killswitch GenServer survives ad-hoc trace sessions and enforces single panic() entry point.

```bash
mix new tracing_toolkit --sup
cd tracing_toolkit
```

### Step 2: `mix.exs`

**Objective**: Pin `:recon` so `:recon_trace` count/timeout limits prevent mailbox floods that melt production BEAM nodes.

```elixir
defmodule TracingToolkit.MixProject do
  use Mix.Project

  def project, do: [app: :tracing_toolkit, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application, do: [extra_applications: [:logger], mod: {TracingToolkit.Application, []}]

  defp deps, do: [{:recon, "~> 2.5"}]
end
```

### Step 3: `lib/tracing_toolkit/application.ex`

**Objective**: Start Killswitch so single panic() call clears all active `:recon_trace` + `:dbg` without hunting individual tag IDs.

```elixir
defmodule TracingToolkit.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [TracingToolkit.Killswitch]
    Supervisor.start_link(children, strategy: :one_for_one, name: TracingToolkit.Supervisor)
  end
end
```

### Step 4: `lib/tracing_toolkit/killswitch.ex`

**Objective**: Maintain MapSet of active trace tags so `panic()` atomically stops `:recon_trace` + `:dbg` without manual tag enumeration.

```elixir
defmodule TracingToolkit.Killswitch do
  @moduledoc """
  Centralized kill switch. Any tracing started through this toolkit is
  registered here, so `panic/0` can unconditionally stop every trace.

  Rationale: if a trace is melting the node, you may not have time to
  remember which fun you called. `TracingToolkit.Killswitch.panic()` is
  the only call you need to know at 3 AM.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(_opts \\ []), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec register(term()) :: :ok
  def register(tag), do: GenServer.cast(__MODULE__, {:register, tag})

  @spec unregister(term()) :: :ok
  def unregister(tag), do: GenServer.cast(__MODULE__, {:unregister, tag})

  @spec active() :: [term()]
  def active, do: GenServer.call(__MODULE__, :active)

  @doc "Stops every active trace unconditionally."
  @spec panic() :: :ok
  def panic do
    :recon_trace.clear()

    try do
      :dbg.stop()
    catch
      _, _ -> :ok
    end

    GenServer.call(__MODULE__, :reset)
  end

  @impl true
  def init(:ok), do: {:ok, %{active: MapSet.new()}}

  @impl true
  def handle_cast({:register, tag}, state),
    do: {:noreply, update_in(state.active, &MapSet.put(&1, tag))}

  def handle_cast({:unregister, tag}, state),
    do: {:noreply, update_in(state.active, &MapSet.delete(&1, tag))}

  @impl true
  def handle_call(:active, _from, state), do: {:reply, MapSet.to_list(state.active), state}
  def handle_call(:reset, _from, _state), do: {:reply, :ok, %{active: MapSet.new()}}
end
```

### Step 5: `lib/tracing_toolkit/sys_trace.ex`

**Objective**: Wrap `:sys.trace` with auto-registration + cleanup so GenServer callback logs (handle_call, terminate) surface without scheduler pause.

```elixir
defmodule TracingToolkit.SysTrace do
  @moduledoc """
  Wrappers around `:sys` for OTP-aware tracing.

  Prefer this over `:recon_trace` when the target is a GenServer /
  :gen_statem / :supervisor — it's cheaper, safer, and shows OTP
  callbacks (handle_call, handle_info, terminate).
  """

  alias TracingToolkit.Killswitch

  @spec on(GenServer.server()) :: :ok
  def on(server) do
    :ok = :sys.trace(server, true)
    Killswitch.register({:sys_trace, server})
  end

  @spec off(GenServer.server()) :: :ok
  def off(server) do
    :ok = :sys.trace(server, false)
    Killswitch.unregister({:sys_trace, server})
  end

  @spec state(GenServer.server()) :: term()
  def state(server), do: :sys.get_state(server, 5_000)

  @spec status(GenServer.server()) :: term()
  def status(server), do: :sys.get_status(server, 5_000)

  @doc """
  Runs `fun` as a one-shot trace session on `server`: enables, executes,
  collects output via the supplied IO device, disables on return.
  """
  @spec with_trace(GenServer.server(), (-> result)) :: result when result: var
  def with_trace(server, fun) when is_function(fun, 0) do
    on(server)

    try do
      fun.()
    after
      off(server)
    end
  end
end
```

### Step 6: `lib/tracing_toolkit/call_trace.ex`

**Objective**: Enforce `max_messages ≤ 1000` + `timeout_ms ≤ 60s` guards so unlimited-trace code never escapes into production.

```elixir
defmodule TracingToolkit.CallTrace do
  @moduledoc """
  Safe function-call tracing via `:recon_trace`.

  Every entry point requires both a `max_messages` cap and a `timeout_ms`
  cap. There is NO unlimited path — that's intentional.
  """

  alias TracingToolkit.Killswitch

  @type mfa_pattern :: {module(), atom(), arity() | :_} | {module(), atom(), [term()]}

  @spec calls(mfa_pattern(), pos_integer(), pos_integer(), keyword()) :: non_neg_integer()
  def calls(pattern, max_messages, timeout_ms, opts \\ [])
      when max_messages > 0 and max_messages <= 1_000 and
             timeout_ms > 0 and timeout_ms <= 60_000 do
    scope = Keyword.get(opts, :scope, :local)
    pid_scope = Keyword.get(opts, :pid, :all)

    tag = {:call_trace, pattern, System.unique_integer([:positive])}
    Killswitch.register(tag)

    count =
      :recon_trace.calls(
        pattern,
        max_messages,
        scope: scope,
        pid_spec: pid_scope,
        time: timeout_ms
      )

    Task.start(fn ->
      Process.sleep(timeout_ms + 100)
      Killswitch.unregister(tag)
    end)

    count
  end

  @spec stop() :: :ok
  def stop, do: :recon_trace.clear()
end
```

### Step 7: `lib/tracing_toolkit/msg_trace.ex`

**Objective**: Collect send/receive events via bounded mailbox so message trace never exhausts scheduler reductions or tracer memory.

```elixir
defmodule TracingToolkit.MsgTrace do
  @moduledoc """
  Message-send and message-receive tracing between two processes.
  Uses `:erlang.trace/3` directly but with a dedicated bounded collector.
  """

  alias TracingToolkit.Killswitch

  @spec capture(pid(), pos_integer(), pos_integer()) :: [tuple()]
  def capture(pid, max_events, timeout_ms)
      when is_pid(pid) and max_events > 0 and max_events <= 500 and
             timeout_ms > 0 and timeout_ms <= 30_000 do
    parent = self()
    tag = {:msg_trace, pid, System.unique_integer([:positive])}
    Killswitch.register(tag)

    collector =
      spawn_link(fn ->
        events = collect([], max_events, timeout_ms)
        send(parent, {tag, events})
      end)

    :erlang.trace(pid, true, [:send, :receive, {:tracer, collector}])

    receive do
      {^tag, events} ->
        :erlang.trace(pid, false, [:all])
        Killswitch.unregister(tag)
        events
    after
      timeout_ms + 1_000 ->
        :erlang.trace(pid, false, [:all])
        Killswitch.unregister(tag)
        []
    end
  end

  defp collect(acc, 0, _timeout_ms), do: Enum.reverse(acc)

  defp collect(acc, remaining, timeout_ms) do
    receive do
      {:trace, _from, :send, msg, to} ->
        collect([{:send, msg, to} | acc], remaining - 1, timeout_ms)

      {:trace, _from, :receive, msg} ->
        collect([{:receive, msg} | acc], remaining - 1, timeout_ms)
    after
      timeout_ms -> Enum.reverse(acc)
    end
  end
end
```

### Step 8: tests

**Objective**: Validate killswitch state isolation and :recon_trace timeout guards prevent mailbox overflow during edge cases.

```elixir
# test/tracing_toolkit/sys_trace_test.exs
defmodule TracingToolkit.SysTraceTest do
  use ExUnit.Case, async: false

  alias TracingToolkit.SysTrace

  defmodule Counter do
    use GenServer
    def start_link(_), do: GenServer.start_link(__MODULE__, 0, name: __MODULE__)
    def bump, do: GenServer.call(__MODULE__, :bump)
    @impl true
    def init(n), do: {:ok, n}
    @impl true
    def handle_call(:bump, _from, n), do: {:reply, n + 1, n + 1}
  end

  setup do
    start_supervised!(Counter)
    :ok
  end

  describe "TracingToolkit.SysTrace" do
    test "state/1 returns the GenServer state" do
      Counter.bump()
      Counter.bump()
      assert SysTrace.state(Counter) == 2
    end

    test "with_trace/2 enables and disables tracing" do
      result = SysTrace.with_trace(Counter, fn -> Counter.bump() end)
      assert result == 1
      # After returning, tracing is disabled
      refute Counter in TracingToolkit.Killswitch.active()
    end
  end
end
```

```elixir
# test/tracing_toolkit/call_trace_test.exs
defmodule TracingToolkit.CallTraceTest do
  use ExUnit.Case, async: false

  alias TracingToolkit.{CallTrace, Killswitch}

  defmodule Math do
    def add(a, b), do: a + b
  end

  setup do
    on_exit(fn -> Killswitch.panic() end)
    :ok
  end

  describe "TracingToolkit.CallTrace" do
    test "calls/4 rejects unbounded limits" do
      assert_raise FunctionClauseError, fn ->
        CallTrace.calls({Math, :add, :_}, 100_000, 100)
      end

      assert_raise FunctionClauseError, fn ->
        CallTrace.calls({Math, :add, :_}, 10, 10_000_000)
      end
    end

    test "calls/4 returns the number of processes matching the spec" do
      count = CallTrace.calls({Math, :add, 2}, 10, 500)
      assert is_integer(count) and count >= 0
      CallTrace.stop()
    end

    test "panic/0 clears all active traces" do
      _ = CallTrace.calls({Math, :add, 2}, 10, 500)
      assert Killswitch.panic() == :ok
      assert Killswitch.active() == []
    end
  end
end
```

### Step 9: runbook-style usage

**Objective**: Showcase incident workflow: live :sys.get_state/1 → bounded :recon_trace.calls/3 → panic/0 cleanup without manual tag enumeration.

```
iex> :sys.get_state(PaymentWorker)
iex> TracingToolkit.SysTrace.on(PaymentWorker)
iex> # reproduce the issue — logs show callbacks
iex> TracingToolkit.SysTrace.off(PaymentWorker)

iex> TracingToolkit.CallTrace.calls({MyApp.Billing, :charge, :_}, 20, 5_000)
iex> # watch output in the shell; auto-stops after 20 msgs or 5s

iex> TracingToolkit.Killswitch.panic()  # emergency stop
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive: BEAM Scheduler Tuning and Memory Profiling in Production

The BEAM scheduler is not "magic" — it's a preemptive work-stealing scheduler that divides CPU time 
into reductions (bytecode instructions). Understanding scheduler tuning is critical when you suspect 
latency spikes in production.

**Key concepts**:
- **Reductions budget**: By default, a process gets ~2000 reductions before yielding to another process.
  Heavy CPU work (binary matching, list recursion) can exhaust the budget and cause tail latency.
- **Dirty schedulers**: If a process does CPU-intensive work (crypto, compression, numerical), it blocks 
  the main scheduler. Use dirty NIFs or `spawn_opt(..., [{:fullsweep_after, 0}])` for GC tuning.
- **Heap tuning per process**: `Process.flag(:min_heap_size, ...)` reserves heap upfront, reducing GC 
  pauses. Measure; don't guess.

**Memory profiling workflow**:
1. Run `recon:memory/0` in iex; identify top 10 memory consumers by type (atoms, binaries, ets).
2. If binaries dominate, check for refc binary leaks (binary held by process that should have been freed).
3. Use `eprof` or `fprof` for function-level CPU attribution; `recon:proc_window/3` for process memory trends.

**Production pattern**: Deploy with `+K true` (async IO), `-env ERL_MAX_PORTS 65536` (port limit), 
`+T 9` (async threads). Measure GC time with `erlang:statistics(garbage_collection)` — if >5% of uptime, 
tune heap or reduce allocation pressure. Never assume defaults are optimal for YOUR workload.

---

## Advanced Considerations

Understanding BEAM internals at production scale requires deep knowledge of scheduler behavior, memory models, and garbage collection dynamics. The soft real-time guarantees of BEAM only hold under specific conditions — high system load, uneven process distribution across schedulers, or GC pressure can break predictable latency completely. Monitor `erlang:statistics(run_queue)` in production to catch scheduler saturation before it degrades latency significantly. The difference between immediate, offheap, and continuous GC garbage collection strategies can significantly impact tail latencies in systems with millions of messages per second and sustained memory pressure.

Process reductions and the reduction counter affect scheduler fairness fundamentally. A process that runs for extended periods without yielding can starve other processes, even though the scheduler treats it fairly by reduction count per scheduling interval. This is especially critical in pipelines processing large data structures or performing recursive computations where yielding points are infrequent and difficult to predict. The BEAM's preemption model is deterministic per reduction, making performance testing reproducible but sometimes hiding race conditions that only manifest under specific load patterns and GC interactions.

The interaction between ETS, Mnesia, and process message queues creates subtle bottlenecks in distributed systems. ETS reads don't block other processes, but writes require acquiring locks; understanding when your workload transitions from read-heavy to write-heavy is crucial for capacity planning. Port drivers and NIFs bypass the BEAM scheduler entirely, which can lead to unexpected priority inversions if not carefully managed. Always profile with `eprof` and `fprof` in realistic production-like environments before deployment to catch performance surprises.


## Deep Dive: Otp Patterns and Production Implications

OTP primitives (GenServer, Supervisor, Application) are tested through their public interfaces, not by inspecting internal state. This discipline forces correct design: if you can't test a behavior without peeking into the server's state, the behavior is not public. Production systems with tight integration tests on GenServer internals are fragile and hard to refactor.

---

## Trade-offs and production gotchas

**1. `:dbg.tpl(:_, :_, [])` will melt your node**
Tracing `:_/:_` produces a trace event for every function call on every
process. A node with 1M calls/s will crash the tracer mailbox within
seconds. Never use a "match anything" pattern without a count limit.

**2. The tracer must not be on a hot path**
If you trace from iex and the shell process is pinned on the same
scheduler as a busy worker, the scheduler can starve. Send traces to a
port tracer (`:dbg.tracer(:port, ...)`) or to a dedicated pid.

**3. `:sys.replace_state/2` is not transactional**
The function swaps state mid-callback in some behaviours. If a message
is being processed, you may lose it or corrupt state. Reserve for
emergencies; document any use.

**4. `:recon_trace` counts events, not function invocations**
One traced function may produce 2 events (`call` + `return_trace`).
Tune `max_messages` accordingly (double what you expect).

**5. Match spec syntax is unforgiving**
`{M, F, :_}` traces all arities. `{M, F, 2}` traces arity 2 only.
Passing a function literal into `:recon_trace.calls/3` (like
`fn [x] when x > 10 -> :return_trace end`) is the ergonomic path —
don't hand-write `[{[:"$1"], [{:>, :"$1", 10}], [...]}]` unless you
know `dbg:fun2ms/1` by heart.

**6. Remote shell traces output to the shell, not logs**
Output vanishes when you disconnect. For overnight traces, use
`:dbg.tracer(:port, :dbg.trace_port(:file, "/tmp/trace.log"))`.

**7. When NOT to use this**
If you already have structured logs (Logger + telemetry events), add
a `Logger.debug` and redeploy. Live tracing is for cases where you
cannot deploy or the issue won't reproduce again. It is a diagnosis
tool, not a permanent observability solution.

---

## Performance notes

| Scenario | Overhead |
|----------|----------|
| `:sys.trace/2` on one GenServer at 1k msgs/s | <1% CPU |
| `:recon_trace.calls/3` with 100 msg cap | negligible |
| `:erlang.trace/3` with `:all`, `:c` on 10 procs | 5–15% CPU |
| `:dbg.tpl(:_, :_, [])` on a 40k req/s node | melts the node in ~3 s |

Rule of thumb: if you can't say in advance how many events/sec the
trace will produce, start with `max_messages: 10, timeout_ms: 2_000`,
read the results, then widen the filter.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule TracingToolkit.Killswitch do
  @moduledoc """
  Centralized kill switch. Any tracing started through this toolkit is
  registered here, so `panic/0` can unconditionally stop every trace.

  Rationale: if a trace is melting the node, you may not have time to
  remember which fun you called. `TracingToolkit.Killswitch.panic()` is
  the only call you need to know at 3 AM.
  """

  use GenServer

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(_opts \\ []), do: GenServer.start_link(__MODULE__, :ok, name: __MODULE__)

  @spec register(term()) :: :ok
  def register(tag), do: GenServer.cast(__MODULE__, {:register, tag})

  @spec unregister(term()) :: :ok
  def unregister(tag), do: GenServer.cast(__MODULE__, {:unregister, tag})

  @spec active() :: [term()]
  def active, do: GenServer.call(__MODULE__, :active)

  @doc "Stops every active trace unconditionally."
  @spec panic() :: :ok
  def panic do
    :recon_trace.clear()

    try do
      :dbg.stop()
    catch
      _, _ -> :ok
    end

    GenServer.call(__MODULE__, :reset)
  end

  @impl true
  def init(:ok), do: {:ok, %{active: MapSet.new()}}

  @impl true
  def handle_cast({:register, tag}, state),
    do: {:noreply, update_in(state.active, &MapSet.put(&1, tag))}

  def handle_cast({:unregister, tag}, state),
    do: {:noreply, update_in(state.active, &MapSet.delete(&1, tag))}

  @impl true
  def handle_call(:active, _from, state), do: {:reply, MapSet.to_list(state.active), state}
  def handle_call(:reset, _from, _state), do: {:reply, :ok, %{active: MapSet.new()}}
end

defmodule Main do
  def main do
      IO.puts("Benchmarking initialized")
      {elapsed_us, result} = :timer.tc(fn ->
        Enum.reduce(1..1000, 0, &+/2)
      end)
      if is_number(elapsed_us) do
        IO.puts("✓ Benchmark completed: sum(1..1000) = " <> inspect(result) <> " in " <> inspect(elapsed_us) <> "µs")
      end
  end
end

Main.main()
```
