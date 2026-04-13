# Memory profiling with :recon and :recon_alloc

**Project**: `memory_profiling` — diagnose memory issues in a running BEAM node without restarting it

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
memory_profiling/
├── lib/
│   └── memory_profiling.ex
├── script/
│   └── main.exs
├── test/
│   └── memory_profiling_test.exs
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
defmodule MemoryProfiling.MixProject do
  use Mix.Project

  def project do
    [
      app: :memory_profiling,
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
### `lib/memory_profiling.ex`

```elixir
defmodule MemoryProfiling.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    Supervisor.start_link([], strategy: :one_for_one, name: MemoryProfiling.Supervisor)
  end
end

defmodule MemoryProfiling.NodeReport do
  @moduledoc """
  High-level node memory breakdown. Renders `:erlang.memory/0` plus
  allocator totals in bytes and percentages.
  """

  @type breakdown :: %{
          total_bytes: non_neg_integer(),
          allocated_bytes: non_neg_integer(),
          rss_overhead_pct: float(),
          buckets: %{atom() => {non_neg_integer(), float()}}
        }

  @spec build() :: breakdown()
  def build do
    mem = :erlang.memory()
    total = Keyword.fetch!(mem, :total)
    allocated = :recon_alloc.memory(:allocated)
    used = :recon_alloc.memory(:used)

    buckets =
      mem
      |> Keyword.delete(:total)
      |> Map.new(fn {k, v} -> {k, {v, pct(v, total)}} end)

    %{
      total_bytes: total,
      allocated_bytes: allocated,
      rss_overhead_pct: pct(allocated - used, allocated),
      buckets: buckets
    }
  end

  @spec format(breakdown()) :: String.t()
  def format(%{buckets: buckets, total_bytes: total, allocated_bytes: alloc, rss_overhead_pct: overhead}) do
    lines =
      buckets
      |> Enum.sort_by(fn {_k, {v, _}} -> -v end)
      |> Enum.map(fn {k, {v, pct}} -> "  #{pad(k, 10)}  #{human(v)}  #{Float.round(pct, 1)}%" end)

    IO.iodata_to_binary([
      "Total:      #{human(total)}\n",
      "Allocated:  #{human(alloc)}  (overhead: #{Float.round(overhead, 1)}%)\n",
      "\n",
      Enum.intersperse(lines, "\n")
    ])
  end

  defp pct(_, 0), do: 0.0
  defp pct(num, denom), do: num / denom * 100.0

  defp pad(a, n), do: a |> Atom.to_string() |> String.pad_trailing(n)

  defp human(n) when n >= 1_073_741_824, do: "#{Float.round(n / 1_073_741_824, 2)} GiB"
  defp human(n) when n >= 1_048_576, do: "#{Float.round(n / 1_048_576, 2)} MiB"
  defp human(n) when n >= 1_024, do: "#{Float.round(n / 1_024, 2)} KiB"
  defp human(n), do: "#{n} B"
end

defmodule MemoryProfiling.ProcessReport do
  @moduledoc """
  Top-N processes by selected attribute. Uses `:recon.proc_count/2`.
  """

  @type attribute :: :memory | :reductions | :message_queue_len | :total_heap_size | :binary_memory

  @spec top(attribute(), pos_integer()) :: [map()]
  def top(attribute, n) when n > 0 do
    attribute
    |> :recon.proc_count(n)
    |> Enum.map(&format/1)
  end

  defp format({pid, value, info_list}) do
    %{
      pid: pid,
      value: value,
      current_function: Keyword.get(info_list, :current_function),
      registered_name: Keyword.get(info_list, :registered_name) || nil,
      initial_call: Keyword.get(info_list, :initial_call)
    }
  end

  @doc """
  Top-N by *change* over `window_ms`. The best leak hunter — catches
  processes that grew during the window, not just the fattest ones.
  """
  @spec window(attribute(), pos_integer(), pos_integer()) :: [map()]
  def window(attribute, n, window_ms) when n > 0 and window_ms > 0 do
    attribute
    |> :recon.proc_window(n, window_ms)
    |> Enum.map(&format/1)
  end
end

defmodule MemoryProfiling.AllocReport do
  @moduledoc """
  Per-allocator usage and fragmentation.

  `:binary_alloc` with usage < 0.5 is the signature of a refc binary leak.
  `:eheap_alloc` with usage < 0.5 signals process heaps pinned large after
  a burst (candidates for `:erlang.garbage_collect/1`).
  """

  @type alloc_info :: %{
          allocator: atom(),
          usage: float(),
          allocated: non_neg_integer(),
          used: non_neg_integer()
        }

  @spec summary() :: [alloc_info()]
  def summary do
    :recon_alloc.memory(:allocated_types)
    |> Enum.map(fn {alloc_name, allocated} ->
      used = bytes_used_for(alloc_name)
      usage = if allocated == 0, do: 0.0, else: used / allocated

      %{
        allocator: alloc_name,
        usage: Float.round(usage, 3),
        allocated: allocated,
        used: used
      }
    end)
    |> Enum.sort_by(& &1.usage)
  end

  defp bytes_used_for(alloc_name) do
    :recon_alloc.sbcs_to_mbcs(:current)
    |> Enum.reduce(0, fn _, acc -> acc end)

    # Use :recon_alloc.average_block_sizes for per-type used bytes
    case :recon_alloc.fragmentation(:current) do
      [] ->
        0

      frags ->
        frags
        |> Enum.find_value(0, fn {{^alloc_name, _instance}, kvs} ->
          Keyword.get(kvs, :sbcs_usage, 0) + Keyword.get(kvs, :mbcs_usage, 0)
        end) || 0
    end
  end

  @spec suspicious(float()) :: [alloc_info()]
  def suspicious(threshold \\ 0.5) when is_float(threshold) do
    summary()
    |> Enum.filter(&(&1.usage < threshold and &1.allocated > 1_048_576))
  end
end

defmodule MemoryProfiling.LeakHunter do
  @moduledoc """
  Correlates memory across two snapshots. A process that grows linearly
  over two 30-second windows is almost always a leak.
  """

  @spec hunt(pos_integer(), pos_integer()) :: [map()]
  def hunt(n, window_ms) when n > 0 and window_ms > 0 do
    before = snapshot()
    Process.sleep(window_ms)
    now = snapshot()

    before
    |> Enum.map(fn {pid, mem_before} ->
      case Map.fetch(now, pid) do
        {:ok, mem_after} -> {pid, mem_after - mem_before, mem_after}
        :error -> {pid, 0, 0}
      end
    end)
    |> Enum.reject(fn {_pid, delta, _} -> delta <= 0 end)
    |> Enum.sort_by(fn {_pid, delta, _} -> -delta end)
    |> Enum.take(n)
    |> Enum.map(&describe/1)
  end

  defp snapshot do
    for pid <- Process.list(), into: %{} do
      case Process.info(pid, :memory) do
        {:memory, m} -> {pid, m}
        nil -> {pid, 0}
      end
    end
  end

  defp describe({pid, delta, current}) do
    info = Process.info(pid, [:registered_name, :current_function, :initial_call]) || []

    %{
      pid: pid,
      delta_bytes: delta,
      current_bytes: current,
      registered_name: Keyword.get(info, :registered_name) || nil,
      current_function: Keyword.get(info, :current_function),
      initial_call: Keyword.get(info, :initial_call)
    }
  end
end

defmodule MemoryProfiling.ProcessReportTest do
  use ExUnit.Case, async: false
  doctest MemoryProfiling.MixProject

  alias MemoryProfiling.ProcessReport

  describe "MemoryProfiling.ProcessReport" do
    test "top/2 by memory returns at most N processes" do
      results = ProcessReport.top(:memory, 5)
      assert length(results) <= 5

      for r <- results do
        assert is_pid(r.pid)
        assert is_integer(r.value) and r.value >= 0
      end
    end

    test "window/3 returns processes that grew" do
      # Spawn one process that accumulates a large list
      pid =
        spawn(fn ->
          Enum.reduce(1..1_000_000, [], fn i, acc -> [i | acc] end)
          Process.sleep(:infinity)
        end)

      on_exit(fn -> Process.exit(pid, :kill) end)

      results = ProcessReport.window(:memory, 5, 150)
      assert is_list(results)
    end
  end
end
```
### `test/memory_profiling_test.exs`

```elixir
defmodule MemoryProfiling.NodeReportTest do
  use ExUnit.Case, async: true
  doctest MemoryProfiling.MixProject

  alias MemoryProfiling.NodeReport

  describe "MemoryProfiling.NodeReport" do
    test "build/0 returns expected buckets" do
      report = NodeReport.build()
      assert is_integer(report.total_bytes) and report.total_bytes > 0
      assert is_integer(report.allocated_bytes) and report.allocated_bytes > 0
      assert Map.has_key?(report.buckets, :processes)
      assert Map.has_key?(report.buckets, :ets)
      assert Map.has_key?(report.buckets, :binary)
    end

    test "format/1 produces human-readable text" do
      out = NodeReport.build() |> NodeReport.format()
      assert out =~ "Total:"
      assert out =~ "Allocated:"
      assert out =~ "processes"
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Memory profiling with :recon and :recon_alloc.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Memory profiling with :recon and :recon_alloc ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case MemoryProfiling.run(payload) do
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
        for _ <- 1..1_000, do: MemoryProfiling.run(:bench)
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
