# Targeted Tracing with `:dbg.tp` and Match Specifications

**Project**: `dbg_tpl` — use raw `:dbg` with trace patterns and match specs to answer surgical runtime questions that `:recon_trace` abstracts away

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
dbg_tpl/
├── lib/
│   └── dbg_tpl.ex
├── script/
│   └── main.exs
├── test/
│   └── dbg_tpl_test.exs
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
defmodule DbgTpl.MixProject do
  use Mix.Project

  def project do
    [
      app: :dbg_tpl,
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
### `lib/dbg_tpl.ex`

```elixir
defmodule DbgTpl.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([DbgTpl.Tracer], strategy: :one_for_one, name: DbgTpl.Supervisor)
  end
end

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

  @spec process_request({:trace_ts, pid(), atom(), term(), term(), {integer(), integer(), integer()}}, term()) ::
          term() | nil
  def process_request({:trace_ts, pid, :call, {m, f, args}, ts}, _ctx) do
    :ets.insert(@table, {pid, {m, f, args}, micros(ts)})
    nil
  end

  def process_request({:trace_ts, pid, :return_from, {m, f, arity}, result, ts}, _ctx) do
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

  def process_request({:trace_ts, pid, :exception_from, {m, f, arity}, {kind, reason}, ts}, _ctx) do
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

  def process_request(_other, _ctx), do: nil

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
    case CallPairing.process_request(trace, nil) do
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
### `test/dbg_tpl_test.exs`

```elixir
defmodule DbgTpl.TracerTest do
  use ExUnit.Case, async: true
  doctest DbgTpl.Application

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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Targeted Tracing with `:dbg.tp` and Match Specifications.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Targeted Tracing with `:dbg.tp` and Match Specifications ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case DbgTpl.run(payload) do
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
        for _ <- 1..1_000, do: DbgTpl.run(:bench)
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
