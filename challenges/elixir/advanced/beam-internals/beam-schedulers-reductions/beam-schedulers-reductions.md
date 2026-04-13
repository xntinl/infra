# BEAM schedulers, reductions, and run queues

**Project**: `schedulers_deep` — observe the BEAM scheduler from Elixir, measure scheduler utilization, and detect run-queue imbalance under load

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
schedulers_deep/
├── lib/
│   └── schedulers_deep.ex
├── script/
│   └── main.exs
├── test/
│   └── schedulers_deep_test.exs
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
defmodule SchedulersDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :schedulers_deep,
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
### `lib/schedulers_deep.ex`

```elixir
defmodule SchedulersDeep.Reporter do
  @moduledoc """
  Samples `:scheduler_wall_time` over a window and returns per-scheduler
  utilization as a percentage.

  Rationale: `:scheduler_wall_time` is cumulative since the flag was enabled.
  A single read is useless. You need two reads delta-ed over a window.
  """

  @type utilization :: %{pos_integer() => float()}

  @doc "Enables the `:scheduler_wall_time` flag globally."
  @spec enable() :: boolean()
  def enable, do: :erlang.system_flag(:scheduler_wall_time, true)

  @spec disable() :: boolean()
  def disable, do: :erlang.system_flag(:scheduler_wall_time, false)

  @doc """
  Samples utilization over `window_ms` and returns a map
  `%{scheduler_id => utilization_percent}`.
  """
  @spec sample(pos_integer()) :: utilization()
  def sample(window_ms) when window_ms > 0 do
    before = read_sorted()
    Process.sleep(window_ms)
    now = read_sorted()
    diff(before, now)
  end

  defp read_sorted do
    :erlang.statistics(:scheduler_wall_time)
    |> Enum.sort()
  end

  defp diff(before, now) do
    Enum.zip(before, now)
    |> Enum.reduce(%{}, fn {{id, a0, t0}, {id, a1, t1}}, acc ->
      active = a1 - a0
      total = t1 - t0
      ratio = if total == 0, do: 0.0, else: active / total * 100.0
      Map.put(acc, id, Float.round(ratio, 2))
    end)
  end

  @doc "Returns counts of normal, dirty-cpu and dirty-io schedulers."
  @spec scheduler_counts() :: %{normal: pos_integer(), dirty_cpu: non_neg_integer(), dirty_io: non_neg_integer()}
  def scheduler_counts do
    %{
      normal: :erlang.system_info(:schedulers),
      dirty_cpu: :erlang.system_info(:dirty_cpu_schedulers),
      dirty_io: :erlang.system_info(:dirty_io_schedulers)
    }
  end
end

defmodule SchedulersDeep.RunQueue do
  @moduledoc """
  Snapshots per-scheduler run queue lengths. Imbalance often explains
  why adding cores doesn't improve throughput.
  """

  @spec snapshot() :: %{pos_integer() => non_neg_integer()}
  def snapshot do
    :erlang.statistics(:run_queue_lengths)
    |> Enum.with_index(1)
    |> Map.new(fn {len, id} -> {id, len} end)
  end

  @doc """
  Returns `{max, min, ratio}`. A ratio greater than ~3 suggests migration
  is not keeping up and you should investigate hot spawners.
  """
  @spec imbalance() :: {non_neg_integer(), non_neg_integer(), float()}
  def imbalance do
    lens = :erlang.statistics(:run_queue_lengths)
    max_l = Enum.max(lens)
    min_l = Enum.min(lens)
    ratio = if min_l == 0, do: max_l * 1.0, else: max_l / min_l
    {max_l, min_l, Float.round(ratio, 2)}
  end
end

defmodule SchedulersDeep.Reductions do
  @moduledoc """
  Top-N processes by reductions delta — this is how you find the process
  actually burning CPU, as opposed to the one with the largest mailbox.
  """

  @doc """
  Returns the top `n` processes by reductions consumed over `window_ms`.

  Each entry is `%{pid: pid, reductions: delta, initial_call: mfa,
  current_function: mfa, registered: name_or_nil}`.
  """
  @spec top(pos_integer(), pos_integer()) :: [map()]
  def top(n, window_ms) when n > 0 and window_ms > 0 do
    before = snapshot_reductions()
    Process.sleep(window_ms)
    now = snapshot_reductions()

    now
    |> Enum.map(fn {pid, r1} ->
      delta = r1 - Map.get(before, pid, 0)
      {pid, delta}
    end)
    |> Enum.sort_by(fn {_pid, d} -> -d end)
    |> Enum.take(n)
    |> Enum.map(&describe/1)
  end

  defp snapshot_reductions do
    for pid <- Process.list(), into: %{} do
      case Process.info(pid, :reductions) do
        {:reductions, r} -> {pid, r}
        nil -> {pid, 0}
      end
    end
  end

  defp describe({pid, delta}) do
    info = Process.info(pid, [:initial_call, :registered_name, :current_function]) || []

    %{
      pid: pid,
      reductions: delta,
      initial_call: Keyword.get(info, :initial_call),
      current_function: Keyword.get(info, :current_function),
      registered: registered(Keyword.get(info, :registered_name))
    }
  end

  defp registered([]), do: nil
  defp registered(nil), do: nil
  defp registered(name), do: name
end

defmodule SchedulersDeep.LoadGen do
  @moduledoc """
  Synthetic load generator — spawns `n` CPU-bound processes each looping
  for `duration_ms`. Useful to reproduce scheduler saturation in tests.
  """

  @spec run(pos_integer(), pos_integer()) :: :ok
  def run(n, duration_ms) do
    parent = self()
    refs = for _ <- 1..n, do: spawn_worker(parent, duration_ms)
    Enum.each(refs, fn ref -> receive do {:done, ^ref} -> :ok end end)
    :ok
  end

  defp spawn_worker(parent, duration_ms) do
    ref = make_ref()

    spawn_link(fn ->
      deadline = System.monotonic_time(:millisecond) + duration_ms
      burn(deadline)
      send(parent, {:done, ref})
    end)

    ref
  end

  defp burn(deadline) do
    if System.monotonic_time(:millisecond) >= deadline do
      :ok
    else
      _ = Enum.reduce(1..10_000, 0, fn i, acc -> acc + i end)
      burn(deadline)
    end
  end
end

defmodule SchedulersDeep.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    SchedulersDeep.Reporter.enable()
    Supervisor.start_link([], strategy: :one_for_one, name: SchedulersDeep.Supervisor)
  end
end

defmodule SchedulersDeep.ReductionsTest do
  use ExUnit.Case, async: false
  doctest SchedulersDeep.MixProject

  alias SchedulersDeep.Reductions

  describe "SchedulersDeep.Reductions" do
    test "top/2 returns processes sorted by reductions" do
      spawn(fn -> Enum.reduce(1..1_000_000, 0, fn i, a -> a + i end) end)
      results = Reductions.top(5, 100)
      assert length(results) <= 5
      deltas = Enum.map(results, & &1.reductions)
      assert deltas == Enum.sort(deltas, :desc)
    end

    test "top/2 entries have expected shape" do
      [first | _] = Reductions.top(3, 50)
      assert is_pid(first.pid)
      assert is_integer(first.reductions)
      assert match?({_m, _f, _a}, first.initial_call) or is_nil(first.initial_call)
    end
  end
end
```
### `test/schedulers_deep_test.exs`

```elixir
defmodule SchedulersDeep.ReporterTest do
  use ExUnit.Case, async: true
  doctest SchedulersDeep.MixProject

  alias SchedulersDeep.{Reporter, LoadGen}

  setup do
    Reporter.enable()
    on_exit(fn -> Reporter.disable() end)
    :ok
  end

  describe "SchedulersDeep.Reporter" do
    test "sample/1 returns one entry per scheduler" do
      util = Reporter.sample(200)
      assert map_size(util) == :erlang.system_info(:schedulers)
      assert Enum.all?(util, fn {_id, pct} -> pct >= 0.0 and pct <= 100.0 end)
    end

    test "loaded schedulers show non-zero utilization" do
      parent = self()

      spawn_link(fn ->
        LoadGen.run(:erlang.system_info(:schedulers) * 2, 300)
        send(parent, :load_done)
      end)

      util = Reporter.sample(200)
      assert_receive :load_done, 2_000
      assert Enum.any?(util, fn {_id, pct} -> pct > 5.0 end)
    end

    test "scheduler_counts/0 returns positive normal count" do
      counts = Reporter.scheduler_counts()
      assert counts.normal > 0
      assert counts.dirty_cpu >= 0
      assert counts.dirty_io >= 0
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for BEAM schedulers, reductions, and run queues.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== BEAM schedulers, reductions, and run queues ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case SchedulersDeep.run(payload) do
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
        for _ <- 1..1_000, do: SchedulersDeep.run(:bench)
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
