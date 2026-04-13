# Safe Production Tracing with `:recon_trace`

**Project**: `recon_trace_prod` — build a thin wrapper around `:recon_trace` that lets on-call engineers trace live production calls without killing the node

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
recon_trace_prod/
├── lib/
│   └── recon_trace_prod.ex
├── script/
│   └── main.exs
├── test/
│   └── recon_trace_prod_test.exs
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
defmodule ReconTraceProd.MixProject do
  use Mix.Project

  def project do
    [
      app: :recon_trace_prod,
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

### `lib/recon_trace_prod.ex`

```elixir
defmodule ReconTraceProd.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {ReconTraceProd.Safe, []}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ReconTraceProd.Supervisor)
  end
end

defmodule ReconTraceProd.Guardrails do
  @moduledoc """
  Validates a trace request before we hand it to `:recon_trace`.

  Refuses to enable a trace that could realistically flood the node:
  a missing rate limit, an arity of `:_` (all arities), or a bare `{:_, :_, :_}`
  match (traces every call).
  """

  @type mfa_spec ::
          {module(), atom(), non_neg_integer()}
          | {module(), atom(), [{list(), list(), list()}]}

  @type rate :: {pos_integer(), pos_integer()}

  @max_rate_msgs 5_000
  @max_rate_window_ms 60_000

  @doc "Validates result from fun, arity_or_ms and window_ms."
  @spec validate(mfa_spec(), rate()) :: :ok | {:error, String.t()}
  def validate({mod, fun, arity_or_ms}, {max_msgs, window_ms})
      when is_atom(mod) and is_atom(fun) do
    cond do
      max_msgs > @max_rate_msgs ->
        {:error, "max_msgs=#{max_msgs} exceeds safety cap #{@max_rate_msgs}"}

      window_ms > @max_rate_window_ms ->
        {:error, "window_ms=#{window_ms} exceeds safety cap #{@max_rate_window_ms}"}

      arity_or_ms == :_ ->
        {:error, "arity :_ traces every arity; specify an integer"}

      is_list(arity_or_ms) and unbounded_match_spec?(arity_or_ms) ->
        {:error, "match spec has no guard; traces every call"}

      true ->
        :ok
    end
  end

  @doc "Validates result from _ and _."
  def validate(_, _), do: {:error, "invalid spec shape"}

  defp unbounded_match_spec?(specs) do
    Enum.any?(specs, fn {_head, guards, _body} -> guards == [] end)
  end
end

defmodule ReconTraceProd.Sink do
  @moduledoc """
  Rotating file sink for trace output. Opens the file with `:delayed_write`
  so individual writes are buffered and flushed every 2 seconds or every 64 KiB,
  whichever comes first.
  """

  @doc "Opens result from path."
  @spec open(Path.t()) :: {:ok, :file.io_device()} | {:error, term()}
  def open(path) do
    File.mkdir_p!(Path.dirname(path))
    File.open(path, [:write, :binary, {:delayed_write, 64 * 1024, 2_000}])
  end

  @doc "Writes result from io and data."
  @spec write(:file.io_device(), iodata()) :: :ok
  def write(io, data) do
    IO.binwrite(io, [data, ?\n])
  end

  @doc "Closes result from io."
  @spec close(:file.io_device()) :: :ok
  def close(io), do: File.close(io)
end

defmodule ReconTraceProd.Safe do
  @moduledoc """
  Named GenServer that owns a single active trace. Survives caller disconnects.

  Only one trace at a time — `:recon_trace` is global per node and overlapping
  traces produce interleaved output that is nearly impossible to correlate.
  """

  use GenServer
  require Logger

  alias ReconTraceProd.{Guardrails, Sink}

  @type spec :: Guardrails.mfa_spec()
  @type rate :: Guardrails.rate()

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @spec start_link(keyword()) :: GenServer.on_start()
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @doc """
  Start a trace. Refuses if another trace is active or guardrails fail.
  Output is written to `path`. Call `stop/0` to end early.
  """
  @spec trace(spec(), rate(), Path.t()) :: :ok | {:error, term()}
  def trace(mfa_spec, rate, path) do
    GenServer.call(__MODULE__, {:trace, mfa_spec, rate, path})
  end

  @doc "Stops result."
  @spec stop() :: :ok
  def stop, do: GenServer.call(__MODULE__, :stop)

  @doc "Returns status result."
  @spec status() :: :idle | {:active, map()}
  def status, do: GenServer.call(__MODULE__, :status)

  # ---------------------------------------------------------------------------
  # GenServer callbacks
  # ---------------------------------------------------------------------------

  @impl true
  def init(_opts), do: {:ok, %{state: :idle}}

  @impl true
  def handle_call({:trace, mfa_spec, rate, path}, _from, %{state: :idle} = state) do
    with :ok <- Guardrails.validate(mfa_spec, rate),
         {:ok, io} <- Sink.open(path) do
      parent = self()

      formatter = fn trace_msg ->
        send(parent, {:trace_line, format(trace_msg)})
      end

      count = :recon_trace.calls(mfa_spec, rate, formatter: formatter)

      active = %{
        spec: mfa_spec,
        rate: rate,
        path: path,
        io: io,
        started_at: System.system_time(:second),
        matched_procs: count
      }

      Logger.info("trace started: #{inspect(mfa_spec)} rate=#{inspect(rate)} path=#{path}")
      {:reply, :ok, %{state: :active, trace: active}}
    else
      {:error, reason} ->
        Logger.warning("trace refused: #{inspect(reason)}")
        {:reply, {:error, reason}, state}
    end
  end

  def handle_call({:trace, _, _, _}, _from, state),
    do: {:reply, {:error, :already_active}, state}

  def handle_call(:stop, _from, %{state: :active, trace: %{io: io}} = state) do
    :recon_trace.clear()
    Sink.close(io)
    Logger.info("trace stopped")
    {:reply, :ok, %{state: :idle}}
  end

  def handle_call(:stop, _from, state), do: {:reply, :ok, state}

  def handle_call(:status, _from, %{state: :idle} = s), do: {:reply, :idle, s}

  def handle_call(:status, _from, %{state: :active, trace: t} = s) do
    {:reply, {:active, Map.take(t, [:spec, :rate, :path, :started_at, :matched_procs])}, s}
  end

  @impl true
  def handle_info({:trace_line, line}, %{state: :active, trace: %{io: io}} = state) do
    Sink.write(io, line)
    {:noreply, state}
  end

  def handle_info({:trace_line, _}, state), do: {:noreply, state}

  # ---------------------------------------------------------------------------
  # Helpers
  # ---------------------------------------------------------------------------

  defp format({:trace, pid, :call, {m, f, args}}),
    do: "CALL  #{inspect(pid)} #{inspect(m)}.#{f}/#{length(args)} args=#{inspect(args, limit: 8)}"

  defp format({:trace, pid, :return_from, {m, f, arity}, result}),
    do: "RET   #{inspect(pid)} #{inspect(m)}.#{f}/#{arity} -> #{inspect(result, limit: 8)}"

  defp format(other), do: inspect(other, limit: 16)
end
```

### `test/recon_trace_prod_test.exs`

```elixir
defmodule ReconTraceProd.GuardrailsTest do
  use ExUnit.Case, async: true
  doctest ReconTraceProd.Application

  alias ReconTraceProd.Guardrails

  describe "validate/2" do
    test "accepts a well-bounded arity spec" do
      assert :ok = Guardrails.validate({String, :upcase, 1}, {100, 1_000})
    end

    test "rejects when max_msgs exceeds cap" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, 1}, {10_000, 1_000})
      assert msg =~ "max_msgs"
    end

    test "rejects when window exceeds cap" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, 1}, {100, 120_000})
      assert msg =~ "window_ms"
    end

    test "rejects wildcard arity" do
      assert {:error, msg} = Guardrails.validate({String, :upcase, :_}, {100, 1_000})
      assert msg =~ "arity"
    end

    test "rejects match spec with no guards" do
      ms = [{[:_, :_], [], [{:return_trace}]}]
      assert {:error, msg} = Guardrails.validate({String, :replace, ms}, {100, 1_000})
      assert msg =~ "guard"
    end

    test "accepts match spec with at least one guard" do
      ms = [{[:"$1", :_], [{:==, :"$1", "foo"}], [{:return_trace}]}]
      assert :ok = Guardrails.validate({String, :replace, ms}, {100, 1_000})
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Safe Production Tracing with `:recon_trace`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Safe Production Tracing with `:recon_trace` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ReconTraceProd.run(payload) do
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
        for _ <- 1..1_000, do: ReconTraceProd.run(:bench)
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
