# Live tracing with :sys, :dbg, and :recon_trace

**Project**: `tracing_toolkit` — safe, production-grade tracing helpers that don't require a restart.

**Difficulty**: ★★★★☆
**Estimated time**: 3–6 hours

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

## Core concepts

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

## Implementation

### Step 1: project

```bash
mix new tracing_toolkit --sup
cd tracing_toolkit
```

### Step 2: `mix.exs`

```elixir
defmodule TracingToolkit.MixProject do
  use Mix.Project

  def project, do: [app: :tracing_toolkit, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application, do: [extra_applications: [:logger], mod: {TracingToolkit.Application, []}]

  defp deps, do: [{:recon, "~> 2.5"}]
end
```

### Step 3: `lib/tracing_toolkit/application.ex`

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
```

### Step 9: runbook-style usage

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

## Resources

- [`:sys` docs](https://www.erlang.org/doc/man/sys.html) — OTP system messages
- [`:recon_trace`](https://ferd.github.io/recon/recon_trace.html) — Fred Hébert
- [Erlang in Anger — chapter 6 Tracing](https://www.erlang-in-anger.com/)
- [Tracing in Erlang — Stavros blog](https://www.stavros.io/posts/erlang-tracing/)
- [`:dbg` User's Guide](https://www.erlang.org/doc/apps/runtime_tools/tracing_in_erlang.html)
- [Saša Jurić: Debugging live systems](https://www.theerlangelist.com/) — The Erlang-elist blog
- [`:erlang.trace/3` reference](https://www.erlang.org/doc/man/erlang.html#trace-3)
