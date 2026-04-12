# Targeted Tracing with `:dbg.tp` and Match Specifications

**Project**: `dbg_tpl` — use raw `:dbg` with trace patterns and match specs to answer surgical runtime questions that `:recon_trace` abstracts away.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

`:recon_trace` (previous exercise) is the right tool 95% of the time. The
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

## Core concepts

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

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule DbgTpl.MixProject do
  use Mix.Project

  def project, do: [app: :dbg_tpl, version: "0.1.0", elixir: "~> 1.15", deps: []]

  def application, do: [extra_applications: [:logger, :runtime_tools], mod: {DbgTpl.Application, []}]
end
```

`:runtime_tools` is required — it contains `:dbg`.

### Step 2: `lib/dbg_tpl/application.ex`

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
```

### Step 7: Exploratory usage in `iex`

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

## Resources

- [`:dbg` reference — Erlang/OTP](https://www.erlang.org/doc/man/dbg.html) — full API surface
- [Match specifications reference](https://www.erlang.org/doc/apps/erts/match_spec.html) — the grammar you must program against
- [`:ms_transform`](https://www.erlang.org/doc/man/ms_transform.html) — `fun2ms` parse transform details
- [Fred Hébert — tracing patterns](https://ferd.ca/) — blog posts on when to choose raw `:dbg` over wrappers
- [`ex2ms`](https://hexdocs.pm/ex2ms/readme.html) — Elixir macro alternative to `fun2ms` for match specs
- [`:erlang.trace/3` and friends](https://www.erlang.org/doc/man/erlang.html#trace-3) — lower layer below `:dbg`
