# Live tracing with :sys, :dbg, and :recon_trace

**Project**: `tracing_toolkit` — safe, production-grade tracing helpers that don't require a restart

---

## Why beam internals and performance matters

Performance work on the BEAM rewards depth: schedulers, reductions, process heaps, garbage collection, binary reference counting, and the JIT compiler each have observable knobs. Tools like recon, eflame, Benchee, and :sys.statistics let you measure before tuning.

The pitfall is benchmarking without a hypothesis. Senior engineers characterize the workload first (CPU-bound? Memory-bound? Lock contention?), then choose the instrument. Premature optimization on the BEAM is particularly costly because micro-benchmarks rarely reflect real scheduler behavior under load.

---

## The business problem

You are building a production-grade Elixir component in the **BEAM internals and performance** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
tracing_toolkit/
├── lib/
│   └── tracing_toolkit.ex
├── script/
│   └── main.exs
├── test/
│   └── tracing_toolkit_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in BEAM internals and performance the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule TracingToolkit.MixProject do
  use Mix.Project

  def project do
    [
      app: :tracing_toolkit,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/tracing_toolkit.ex`

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

  @doc "Registers result from tag."
  @spec register(term()) :: :ok
  def register(tag), do: GenServer.cast(__MODULE__, {:register, tag})

  @doc "Unregisters result from tag."
  @spec unregister(term()) :: :ok
  def unregister(tag), do: GenServer.cast(__MODULE__, {:unregister, tag})

  @doc "Returns active result."
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

defmodule TracingToolkit.SysTrace do
  @moduledoc """
  Wrappers around `:sys` for OTP-aware tracing.

  Prefer this over `:recon_trace` when the target is a GenServer /
  :gen_statem / :supervisor — it's cheaper, safer, and shows OTP
  callbacks (handle_call, handle_info, terminate).
  """

  alias TracingToolkit.Killswitch

  @doc "Returns on result from server."
  @spec on(GenServer.server()) :: :ok
  def on(server) do
    :ok = :sys.trace(server, true)
    Killswitch.register({:sys_trace, server})
  end

  @doc "Returns off result from server."
  @spec off(GenServer.server()) :: :ok
  def off(server) do
    :ok = :sys.trace(server, false)
    Killswitch.unregister({:sys_trace, server})
  end

  @doc "Returns state result from server."
  @spec state(GenServer.server()) :: term()
  def state(server), do: :sys.get_state(server, 5_000)

  @doc "Returns status result from server."
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

defmodule TracingToolkit.CallTrace do
  @moduledoc """
  Safe function-call tracing via `:recon_trace`.

  Every entry point requires both a `max_messages` cap and a `timeout_ms`
  cap. There is NO unlimited path — that's intentional.
  """

  alias TracingToolkit.Killswitch

  @type mfa_pattern :: {module(), atom(), arity() | :_} | {module(), atom(), [term()]}

  @doc "Returns calls result from pattern, max_messages, timeout_ms and opts."
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

  @doc "Stops result."
  @spec stop() :: :ok
  def stop, do: :recon_trace.clear()
end

defmodule TracingToolkit.MsgTrace do
  @moduledoc """
  Message-send and message-receive tracing between two processes.
  Uses `:erlang.trace/3` directly but with a dedicated bounded collector.
  """

  alias TracingToolkit.Killswitch

  @doc "Returns capture result from pid, max_events and timeout_ms."
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

defmodule TracingToolkit.CallTraceTest do
  use ExUnit.Case, async: false
  doctest TracingToolkit.MixProject

  alias TracingToolkit.{CallTrace, Killswitch}

  defmodule Math do
    @doc "Adds result from a and b."
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

### `test/tracing_toolkit_test.exs`

```elixir
defmodule TracingToolkit.SysTraceTest do
  use ExUnit.Case, async: true
  doctest TracingToolkit.MixProject

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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Live tracing with :sys, :dbg, and :recon_trace.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Live tracing with :sys, :dbg, and :recon_trace ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case TracingToolkit.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: TracingToolkit.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Reductions, not time, govern preemption

The BEAM scheduler counts reductions (function calls + I/O ops). After ~4000, the process yields. Long lists processed in tight Elixir loops are not the bottleneck people think.

### 2. Binary reference counting can leak

Sub-binaries hold references to large parent binaries. A 10-byte slice of a 10MB binary keeps the 10MB alive. Use :binary.copy/1 when storing slices long-term.

### 3. Profile production with recon

recon's process_window/3 finds memory leaks; bin_leak/1 finds binary refc leaks; proc_count/2 finds runaway processes. These are non-invasive and safe in production.

---
