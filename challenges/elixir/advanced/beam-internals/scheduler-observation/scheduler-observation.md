# Scheduler Observation with `:scheduler` and `:erlang.statistics/1`

**Project**: `scheduler_observatory` — a GenServer that samples scheduler utilization, run queue lengths, and active counts, exposing them as telemetry events you can plot or alert on

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
scheduler_observatory/
├── lib/
│   └── scheduler_observatory.ex
├── script/
│   └── main.exs
├── test/
│   └── scheduler_observatory_test.exs
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
defmodule SchedulerObservatory.MixProject do
  use Mix.Project

  def project do
    [
      app: :scheduler_observatory,
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

### `lib/scheduler_observatory.ex`

```elixir
defmodule SchedulerObservatory.Application do
  use Application

  @impl true
  def start(_type, _args) do
    :erlang.system_flag(:scheduler_wall_time, true)

    children = [
      {SchedulerObservatory.Sampler, interval_ms: 1_000}
    ]

    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

defmodule SchedulerObservatory.Sampler do
  @moduledoc """
  Samples scheduler utilization on a fixed interval.

  Emits telemetry event [:beam, :scheduler, :sample] with measurements:
    - active_percent_total    aggregate across all schedulers
    - active_percent_per      list of per-scheduler percentages
    - run_queue_total
    - run_queue_per           per-scheduler run queue length
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @impl true
  def init(opts) do
    interval = Keyword.fetch!(opts, :interval_ms)
    state = %{interval: interval, baseline: :scheduler.sample()}
    Process.send_after(self(), :sample, interval)
    {:ok, state}
  end

  @impl true
  def handle_info(:sample, %{baseline: baseline, interval: interval} = state) do
    current = :scheduler.sample()

    util = :scheduler.utilization(baseline, current)
    aggregate = Enum.find(util, &match?({:total, _, _}, &1))
    per_scheduler = Enum.filter(util, &match?({:normal, _, _, _}, &1))

    run_q_per = :erlang.statistics(:run_queue_lengths)
    run_q_total = :erlang.statistics(:total_run_queue_lengths)

    :telemetry.execute(
      [:beam, :scheduler, :sample],
      %{
        active_percent_total: percent(aggregate),
        active_percent_per: Enum.map(per_scheduler, &percent/1),
        run_queue_total: run_q_total,
        run_queue_per: run_q_per
      },
      %{}
    )

    Process.send_after(self(), :sample, interval)
    {:noreply, %{state | baseline: current}}
  end

  defp percent({:total, active, total}), do: ratio(active, total)
  defp percent({:normal, _id, active, total}), do: ratio(active, total)
  defp percent({_, active, total}), do: ratio(active, total)

  defp ratio(_active, 0), do: 0.0
  defp ratio(active, total), do: active / total * 100
end

defmodule SchedulerObservatory.Workload do
  @doc """
  Spin up N busy-loopers that hog reductions. Useful to see run queues grow.
  """
  def saturate(n) do
    for _ <- 1..n do
      spawn(fn -> loop(0) end)
    end
  end

  defp loop(n) when n > 100_000_000, do: :done
  defp loop(n), do: loop(n + 1)
end
```

### `test/scheduler_observatory_test.exs`

```elixir
defmodule SchedulerObservatory.SamplerTest do
  use ExUnit.Case, async: true
  doctest SchedulerObservatory.Application

  setup do
    :erlang.system_flag(:scheduler_wall_time, true)
    :ok
  end

  describe "telemetry events" do
    test "emits [:beam, :scheduler, :sample] at the configured interval" do
      :telemetry.attach(
        "test-handler",
        [:beam, :scheduler, :sample],
        fn event, measurements, meta, pid -> send(pid, {event, measurements, meta}) end,
        self()
      )

      {:ok, _pid} = SchedulerObservatory.Sampler.start_link(interval_ms: 50)

      assert_receive {[:beam, :scheduler, :sample], measurements, _}, 500
      assert is_number(measurements.active_percent_total)
      assert is_list(measurements.active_percent_per)
      assert is_integer(measurements.run_queue_total)
    after
      :telemetry.detach("test-handler")
    end
  end

  describe "values under load" do
    test "aggregate utilization rises when CPU is busy" do
      :erlang.system_flag(:scheduler_wall_time, true)
      base = :scheduler.sample()

      tasks =
        for _ <- 1..System.schedulers_online() do
          Task.async(fn -> Enum.reduce(1..2_000_000, 0, &(&1 + &2)) end)
        end

      Task.await_many(tasks, 5_000)
      after_sample = :scheduler.sample()
      [{:total, active, total} | _] = :scheduler.utilization(base, after_sample)
      assert active / total > 0.1
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Scheduler Observation with `:scheduler` and `:erlang.statistics/1`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Scheduler Observation with `:scheduler` and `:erlang.statistics/1` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case SchedulerObservatory.run(payload) do
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
        for _ <- 1..1_000, do: SchedulerObservatory.run(:bench)
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
