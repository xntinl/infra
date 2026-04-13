# Targeted Tracing with `:dbg.tp` and Match Specifications

**Project**: `dbg_tpl` — use raw `:dbg` with trace patterns and match specs to answer surgical runtime questions that `:recon_trace` abstracts away.

---

## Project context

Runtime tracing is essential for production debugging. The standard `:recon_trace` tool handles 95% of cases. The
remaining 5% is when you need something `recon_trace` does not expose: a
tracer process you own and drive yourself, trace flags on a specific pid
rather than a function, local-call tracing (`:dbg.tpl/3`) that catches
non-exported helpers, or integration with `:erlang.trace_pattern/3` for
sequential tracing.

Owning the tracer process means you choose the backpressure strategy. You
can route matches into a GenStage pipeline, dump them to a circular buffer,
or correlate `:call` and `:return_from` pairs to compute per-call wall time
for a specific MFA. Tools like `ExUnit`'s `trace: true`, `AppSignal`'s
auto-instrumentation, and Erlang's `fprof` all use the raw `:dbg` primitives
under the hood.

The goal: build `DbgTpl`, a small framework that turns `:dbg.tp`,
`:dbg.tpl`, and match spec `fun2ms` compilation into a typed, GenServer-owned
API. It produces **pair-matched call/return events with elapsed time** —
the kind of data a profiler needs and `recon_trace` does not give you.

```
dbg_tpl/
├── lib/
│   └── dbg_tpl/
│       ├── application.ex
│       ├── tracer.ex             # GenServer owning the tracer
│       ├── patterns.ex           # high-level API over tp/tpl
│       └── call_pairing.ex       # match call with return_from
├── test/
│   └── dbg_tpl/
│       └── tracer_test.exs
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
### 1. `tp` vs `tpl` vs `tpe`

Three sibling functions, three different scopes:

| Function | Scope | Use when |
|----------|-------|----------|
| `:dbg.tp/2,3` | **Global** calls — exported functions invoked by fully-qualified name | Most of the time |
| `:dbg.tpl/2,3` | **Local** calls — including intra-module helper calls | Investigating a private helper |
| `:dbg.tpe/2` | **Send** events to a destination | Tracking message flow to a registered name |

`:dbg.tp` does NOT match when a function is called from inside its own module
without the module prefix (`do_work(...)` instead of `MyMod.do_work(...)`).
`:dbg.tpl` does. That is the only difference, and it matters: Elixir's
`Kernel.defp` defines private functions which are always local-call. To trace
a private helper through the `@compile {:inline, ...}` boundary, you need
`:dbg.tpl` or the trace is empty.

### 2. The tracer process

`:dbg.tracer/0` spawns a default tracer that prints to the group leader.
`:dbg.tracer(:process, {fun, state})` lets you own the tracer:

```
traced process ──(trace msg)──▶ tracer ──(fun.(msg, state))──▶ new_state
                                                                 │
                                                                 ▼
                                                         GenStage? ETS? log?
```

The `fun/2` runs on the tracer. If it blocks on I/O, the mailbox grows and
backpressure hits the traced processes. Keep `fun/2` under 5 µs; defer heavy
work to a separate process via `send/2` (non-blocking) or a `:queue`.

### 3. Match specifications: `fun2ms` vs hand-written

A match spec is a list of 3-tuples `{HeadPattern, Guards, Body}` — a small,
compiled filter language evaluated in C. Writing them by hand is error-prone,
so Erlang provides the `:dbg.fun2ms/1` parse transform:

```erlang
ms = :dbg.fun2ms(fn
  [arg1, arg2] when arg1 > 100 -> :return_trace
end)
```

In Elixir this works through the `:ms_transform` parse transform when you
load `:matchSpec` fixups — or you write the spec directly:

```elixir
# Hand-written: match head, guard, body
match_spec = [
  {[:"$1", :"$2"],           # arity-2, bind args
   [{:>, :"$1", 100}],        # first arg > 100
   [{:return_trace}, {:exception_trace}]}  # body actions
]
```

The body action `{:return_trace}` emits a `{:trace, Pid, :return_from, MFA, Ret}`
message when the matched call returns. `{:exception_trace}` is the same plus
it fires on exceptions.

### 4. Pairing `:call` with `:return_from`

A traced call produces two messages:

```
t=0   {:trace, pid, :call,        {M, F, args}}
t=Δ   {:trace, pid, :return_from, {M, F, arity}, result}
```

To compute per-call wall time you pair them. The naive approach is "match
by MFA" — but a pid can be in the middle of a recursive call when another
`:call` arrives. Pair by a **stack** per pid: push on `:call`, pop on
`:return_from`. The difference in timestamps gives elapsed time.

The tracer message does not include a timestamp by default. Set the
`:timestamp` flag when you enable tracing — then messages become 5-tuples
with an extra timestamp element.

### 5. Trace flags: `:call`, `:return_to`, `:arity`, `:procs`

`:dbg.p(PidSpec, Flags)` activates flags on one or more processes.
Common flags:

- `:call` — produce trace messages for patterns enabled via `tp`/`tpl`
- `:return_to` — track stack unwinding between calls
- `:arity` — drop args in the trace msg, replace with arity (cheaper)
- `:procs` — spawn/exit/register events
- `:timestamp` — prepend a `{MegaSec, Sec, MicroSec}` timestamp to every message
- `:running` — scheduler in/out events (useful for scheduler debugging)

For per-call wall time you need `[:call, :timestamp]`. For process lifecycle
auditing you need `[:procs]`. Combine as needed; more flags = more messages.

### 6. Stopping cleanly: `:dbg.stop_clear/0` vs `:dbg.stop/0`

`:dbg.stop/0` kills the tracer process but leaves trace flags on the traced
processes. Subsequent `:dbg.tracer/0` calls will re-use those flags — often
not what you want. Use `:dbg.stop_clear/0` to clear all flags AND kill the
tracer. Always call it in `terminate/2` or `after` blocks; otherwise a
partial trace can haunt the node until the next restart.

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

### Step 1: `mix.exs`

**Objective**: Include `:runtime_tools` in `extra_applications` so `:dbg` is loaded alongside the OTP app boot.

```elixir
defmodule DbgTpl.MixProject do
  use Mix.Project

  def project, do: [app: :dbg_tpl, version: "0.1.0", elixir: "~> 1.15", deps: []]

  def application, do: [extra_applications: [:logger, :runtime_tools], mod: {DbgTpl.Application, []}]
end
```

`:runtime_tools` is required — it contains `:dbg`.

### Step 2: `lib/dbg_tpl/application.ex`

**Objective**: Supervise `DbgTpl.Tracer` under `:one_for_one` so a crashed tracer auto-recovers without leaking active patterns.

```elixir
defmodule DbgTpl.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([DbgTpl.Tracer], strategy: :one_for_one, name: DbgTpl.Supervisor)
  end
end
```

### Step 3: `lib/dbg_tpl/patterns.ex`

**Objective**: Build typed match specs for `:dbg.tp/tpl` so callers compose filters without hand-rolling Erlang MS tuples.

```elixir
defmodule DbgTpl.Patterns do
  @moduledoc """
  Build typed match specifications for `:dbg.tp/3` and `:dbg.tpl/3`.
  """

  @type match_spec :: [{list(), list(), list()}]

  @doc "Match every call; emit call + return + wall-clock elapsed."
  @spec any_call_with_return(non_neg_integer()) :: match_spec()
  def any_call_with_return(arity) when arity >= 0 do
    [{List.duplicate(:_, arity), [], [{:return_trace}, {:exception_trace}]}]
  end

  @doc "Match when the first argument equals `value`; trace return too."
  @spec first_arg_equals(non_neg_integer(), term()) :: match_spec()
  def first_arg_equals(arity, value) when arity >= 1 do
    head = [{:"$1"} | List.duplicate(:_, arity - 1)] |> List.flatten()
    # Construct [:"$1", :_, :_, ...] correctly:
    head = [:"$1" | List.duplicate(:_, arity - 1)]
    [{head, [{:==, :"$1", value}], [{:return_trace}, {:exception_trace}]}]
  end

  @doc "Drop args in the trace message to save bytes; emit arity only."
  @spec arity_only(non_neg_integer()) :: match_spec()
  def arity_only(arity) do
    [{List.duplicate(:_, arity), [], [{:message, {:arity}}]}]
  end
end
```

### Step 4: `lib/dbg_tpl/call_pairing.ex`

**Objective**: Pair `:call` with `:return_from` per pid via an ETS stack so recursion unwinds to correct elapsed timings.

```elixir
defmodule DbgTpl.CallPairing do
  @moduledoc """
  Pair :call events with :return_from events per pid. Produces
  %{pid: pid, mfa: mfa, args: term, result: term, elapsed_us: integer} records.

  Call depth is tracked as a per-pid stack in an ETS table `:dbg_tpl_stack`.
  """

  @table :dbg_tpl_stack

  @spec init() :: :ok
  def init do
    case :ets.whereis(@table) do
      :undefined -> :ets.new(@table, [:named_table, :public, :duplicate_bag])
      _ -> :ok
    end

    :ok
  end

  @spec handle({:trace_ts, pid(), atom(), term(), term(), {integer(), integer(), integer()}}, term()) ::
          term() | nil
  def handle({:trace_ts, pid, :call, {m, f, args}, ts}, _ctx) do
    :ets.insert(@table, {pid, {m, f, args}, micros(ts)})
    nil
  end

  def handle({:trace_ts, pid, :return_from, {m, f, arity}, result, ts}, _ctx) do
    case pop_call(pid, m, f, arity) do
      {args, started_at} ->
        %{
          pid: pid,
          mfa: {m, f, arity},
          args: args,
          result: result,
          elapsed_us: micros(ts) - started_at
        }

      nil ->
        nil
    end
  end

  def handle({:trace_ts, pid, :exception_from, {m, f, arity}, {kind, reason}, ts}, _ctx) do
    case pop_call(pid, m, f, arity) do
      {args, started_at} ->
        %{
          pid: pid,
          mfa: {m, f, arity},
          args: args,
          result: {:raise, kind, reason},
          elapsed_us: micros(ts) - started_at
        }

      nil ->
        nil
    end
  end

  def handle(_other, _ctx), do: nil

  defp pop_call(pid, m, f, arity) do
    # A process can be mid-recursion — pop the most recent matching call.
    case :ets.lookup(@table, pid) do
      [] ->
        nil

      rows ->
        matching =
          Enum.find(Enum.reverse(rows), fn {^pid, {^m, ^f, args}, _} -> length(args) == arity end)

        case matching do
          {^pid, {^m, ^f, args}, started_at} = obj ->
            :ets.delete_object(@table, obj)
            {args, started_at}

          _ ->
            nil
        end
    end
  end

  defp micros({mega, sec, micro}), do: (mega * 1_000_000 + sec) * 1_000_000 + micro
end
```

### Step 5: `lib/dbg_tpl/tracer.ex`

**Objective**: Own the `:dbg` tracer inside a GenServer that arms patterns and fan-outs paired events to subscribers.

```elixir
defmodule DbgTpl.Tracer do
  @moduledoc """
  Owns a `:dbg` tracer. Exposes a typed API to arm/disarm trace patterns.

  Matched call/return pairs are forwarded to a subscriber pid as
  `{:dbg_tpl, %{...}}` messages.
  """

  use GenServer

  alias DbgTpl.{CallPairing, Patterns}

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec subscribe(pid()) :: :ok
  def subscribe(pid \\ self()), do: GenServer.call(__MODULE__, {:subscribe, pid})

  @doc "Arm a global trace pattern."
  @spec arm(module(), atom(), non_neg_integer(), Patterns.match_spec()) :: :ok
  def arm(m, f, arity, match_spec) do
    GenServer.call(__MODULE__, {:arm, :tp, m, f, arity, match_spec})
  end

  @doc "Arm a local trace pattern (includes private / intra-module calls)."
  @spec arm_local(module(), atom(), non_neg_integer(), Patterns.match_spec()) :: :ok
  def arm_local(m, f, arity, match_spec) do
    GenServer.call(__MODULE__, {:arm, :tpl, m, f, arity, match_spec})
  end

  @spec disarm_all() :: :ok
  def disarm_all, do: GenServer.call(__MODULE__, :disarm_all)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    CallPairing.init()
    Process.flag(:trap_exit, true)
    {:ok, %{subscribers: MapSet.new(), running: false}}
  end

  @impl true
  def handle_call({:subscribe, pid}, _from, state) do
    Process.monitor(pid)
    {:reply, :ok, %{state | subscribers: MapSet.put(state.subscribers, pid)}}
  end

  def handle_call({:arm, kind, m, f, arity, ms}, _from, state) do
    :ok = ensure_tracer_running(state.running)
    {:ok, _} = arm_pattern(kind, m, f, arity, ms)
    {:reply, :ok, %{state | running: true}}
  end

  def handle_call(:disarm_all, _from, state) do
    :dbg.stop_clear()
    :ets.delete_all_objects(:dbg_tpl_stack)
    {:reply, :ok, %{state | running: false}}
  end

  @impl true
  def handle_info({:dbg_msg, trace}, state) do
    case CallPairing.handle(trace, nil) do
      nil ->
        :ok

      event ->
        Enum.each(state.subscribers, fn sub -> send(sub, {:dbg_tpl, event}) end)
    end

    {:noreply, state}
  end

  def handle_info({:DOWN, _ref, :process, pid, _}, state),
    do: {:noreply, %{state | subscribers: MapSet.delete(state.subscribers, pid)}}

  def handle_info(_, state), do: {:noreply, state}

  @impl true
  def terminate(_reason, _state) do
    :dbg.stop_clear()
    :ok
  end

  # ---------------------------------------------------------------------------
  # Internals
  # ---------------------------------------------------------------------------

  defp ensure_tracer_running(true), do: :ok

  defp ensure_tracer_running(false) do
    parent = self()

    {:ok, _} =
      :dbg.tracer(
        :process,
        {fn msg, _ ->
           send(parent, {:dbg_msg, msg})
           []
         end, []}
      )

    {:ok, _} = :dbg.p(:all, [:call, :timestamp])
    :ok
  end

  defp arm_pattern(:tp, m, f, arity, ms), do: :dbg.tp({m, f, arity}, ms)
  defp arm_pattern(:tpl, m, f, arity, ms), do: :dbg.tpl({m, f, arity}, ms)
end
```

### Step 6: `test/dbg_tpl/tracer_test.exs`

**Objective**: Exercise arm, filter, and disarm flows against `String.upcase/1` to prove pairing and pattern isolation end-to-end.

```elixir
defmodule DbgTpl.TracerTest do
  use ExUnit.Case, async: false

  alias DbgTpl.{Patterns, Tracer}

  setup do
    Tracer.disarm_all()
    :ok = Tracer.subscribe(self())
    on_exit(fn -> Tracer.disarm_all() end)
    :ok
  end

  describe "DbgTpl.Tracer" do
    test "paired call/return with elapsed time" do
      Tracer.arm(String, :upcase, 1, Patterns.any_call_with_return(1))

      String.upcase("hello")

      assert_receive {:dbg_tpl, event}, 1_000
      assert event.mfa == {String, :upcase, 1}
      assert event.args == ["hello"]
      assert event.result == "HELLO"
      assert event.elapsed_us >= 0
    end

    test "first_arg_equals filters unmatched calls" do
      Tracer.arm(String, :upcase, 1, Patterns.first_arg_equals(1, "watch_me"))

      String.upcase("ignored")
      String.upcase("watch_me")

      assert_receive {:dbg_tpl, %{args: ["watch_me"]}}, 1_000
      refute_receive {:dbg_tpl, %{args: ["ignored"]}}, 100
    end

    test "disarm_all clears patterns and subsequent calls are silent" do
      Tracer.arm(String, :downcase, 1, Patterns.any_call_with_return(1))
      String.downcase("HI")
      assert_receive {:dbg_tpl, _}, 500

      Tracer.disarm_all()
      String.downcase("QUIET")
      refute_receive {:dbg_tpl, _}, 200
    end
  end
end
```

### Step 7: Exploratory usage in `iex`

**Objective**: Drive an IEx session that arms `:erlang.length/1` and confirms paired events arrive via `flush/0`.

```elixir
# iex -S mix
DbgTpl.Tracer.subscribe(self())

match_spec = DbgTpl.Patterns.any_call_with_return(1)
DbgTpl.Tracer.arm(:erlang, :length, 1, match_spec)

:erlang.length([1, 2, 3])
flush()
# → {:dbg_tpl, %{mfa: {:erlang, :length, 1}, args: [[1, 2, 3]], result: 3, elapsed_us: 2, pid: #PID<...>}}

DbgTpl.Tracer.disarm_all()
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

**1. `:dbg` is global.** There is only one tracer per node. Starting a second
`:dbg.tracer/0` kills the first silently. Wrap ownership in a singleton
GenServer (as above) or coordinate across modules.

**2. `fun2ms` is a parse transform, not a function.** It must be called at
compile time via `require Ex2ms` or invoked inside `:dbg` shell mode. At
runtime you write match specs literally — that's why the exercise builds
them with helper functions.

**3. `:timestamp` flag changes message shape.** Without `:timestamp`:
`{:trace, pid, :call, mfa}`. With `:timestamp`:
`{:trace_ts, pid, :call, mfa, ts}`. Your message handler must pattern-match
both or you'll silently drop events.

**4. Recursive calls need a stack, not a counter.** The pairing implementation
above uses a duplicate_bag ETS table. A single `call_depth` counter gets
confused by nested calls with different arities.

**5. `:arity` drops argument data, saves serialization cost.** For high-volume
functions, use `Patterns.arity_only/1` to get call events without copying
argument terms across the tracer boundary. You lose argument visibility but
gain 5x throughput.

**6. `{:return_trace}` alone cannot see tail-recursive returns.** The VM
optimizes tail calls by replacing the stack frame — the return is "to the
caller's caller", not "from this function". Add `:return_to` flag to trace
unwinding.

**7. Exceptions need `{:exception_trace}`.** `{:return_trace}` fires only on
normal return. A function that raises produces no trace event unless you
also include `{:exception_trace}` in the match spec body.

**8. When NOT to use this.** If you just need "was this function called in
the last 5 seconds?" use `:recon_trace.calls/3` and read output — don't
build a tracer. Raw `:dbg` is for cases where you need **programmatic**
access to trace events (correlating with telemetry, feeding GenStage, etc).

---

## Performance notes

| Configuration | Cost per matched call |
|---------------|-----------------------|
| `tp` + `arity_only` + no `:timestamp` | ~2 µs |
| `tp` + args + `:timestamp` | ~5 µs |
| `tpl` + `return_trace` + `:timestamp` | ~11 µs |
| Same plus a GenServer hop (tracer → subscriber) | ~25 µs |

The GenServer hop is the single biggest cost. If you need sub-µs tracing,
subscribe the tracer's internal `fun/2` directly to an ETS table or an
atomic counter rather than sending messages.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?


## Executable Example

```elixir
defmodule DbgTpl.Tracer do
  @moduledoc """
  Owns a `:dbg` tracer. Exposes a typed API to arm/disarm trace patterns.

  Matched call/return pairs are forwarded to a subscriber pid as
  `{:dbg_tpl, %{...}}` messages.
  """

  use GenServer

  alias DbgTpl.{CallPairing, Patterns}

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec subscribe(pid()) :: :ok
  def subscribe(pid \\ self()), do: GenServer.call(__MODULE__, {:subscribe, pid})

  @doc "Arm a global trace pattern."
  @spec arm(module(), atom(), non_neg_integer(), Patterns.match_spec()) :: :ok
  def arm(m, f, arity, match_spec) do
    GenServer.call(__MODULE__, {:arm, :tp, m, f, arity, match_spec})
  end

  @doc "Arm a local trace pattern (includes private / intra-module calls)."
  @spec arm_local(module(), atom(), non_neg_integer(), Patterns.match_spec()) :: :ok
  def arm_local(m, f, arity, match_spec) do
    GenServer.call(__MODULE__, {:arm, :tpl, m, f, arity, match_spec})
  end

  @spec disarm_all() :: :ok
  def disarm_all, do: GenServer.call(__MODULE__, :disarm_all)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts) do
    CallPairing.init()
    Process.flag(:trap_exit, true)
    {:ok, %{subscribers: MapSet.new(), running: false}}
  end

  @impl true
  def handle_call({:subscribe, pid}, _from, state) do
    Process.monitor(pid)
    {:reply, :ok, %{state | subscribers: MapSet.put(state.subscribers, pid)}}
  end

  def handle_call({:arm, kind, m, f, arity, ms}, _from, state) do
    :ok = ensure_tracer_running(state.running)
    {:ok, _} = arm_pattern(kind, m, f, arity, ms)
    {:reply, :ok, %{state | running: true}}
  end

  def handle_call(:disarm_all, _from, state) do
    :dbg.stop_clear()
    :ets.delete_all_objects(:dbg_tpl_stack)
    {:reply, :ok, %{state | running: false}}
  end

  @impl true
  def handle_info({:dbg_msg, trace}, state) do
    case CallPairing.handle(trace, nil) do
      nil ->
        :ok

      event ->
        Enum.each(state.subscribers, fn sub -> send(sub, {:dbg_tpl, event}) end)
    end

    {:noreply, state}
  end

  def handle_info({:DOWN, _ref, :process, pid, _}, state),
    do: {:noreply, %{state | subscribers: MapSet.delete(state.subscribers, pid)}}

  def handle_info(_, state), do: {:noreply, state}

  @impl true
  def terminate(_reason, _state) do
    :dbg.stop_clear()
    :ok
  end

  # ---------------------------------------------------------------------------
  # Internals
  # ---------------------------------------------------------------------------

  defp ensure_tracer_running(true), do: :ok

  defp ensure_tracer_running(false) do
    parent = self()

    {:ok, _} =
      :dbg.tracer(
        :process,
        {fn msg, _ ->
           send(parent, {:dbg_msg, msg})
           []
         end, []}
      )

    {:ok, _} = :dbg.p(:all, [:call, :timestamp])
    :ok
  end

  defp arm_pattern(:tp, m, f, arity, ms), do: :dbg.tp({m, f, arity}, ms)
  defp arm_pattern(:tpl, m, f, arity, ms), do: :dbg.tpl({m, f, arity}, ms)
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
