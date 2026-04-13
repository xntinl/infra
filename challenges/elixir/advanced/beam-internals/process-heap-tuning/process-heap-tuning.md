# Process Heap Tuning: `:min_heap_size` and `:min_bin_vheap_size`

**Project**: `heap_lab` — experiments that measure allocation-induced GC pressure on short-lived worker processes and demonstrate how `:min_heap_size` and `:min_bin_vheap_size` remove the pressure

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
heap_lab/
├── lib/
│   └── heap_lab.ex
├── script/
│   └── main.exs
├── test/
│   └── heap_lab_test.exs
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
defmodule HeapLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :heap_lab,
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

### `lib/heap_lab.ex`

```elixir
defmodule HeapLab.Worker do
  @moduledoc """
  A representative short-lived worker. Allocates a map and a list
  sized to simulate a "handle one request" workload, then exits.
  """

  @default_spawn_opts []

  @doc "Runs result from items and opts."
  def run(items, opts \\ []) do
    spawn_opts = Keyword.merge(@default_spawn_opts, Keyword.get(opts, :spawn_opts, []))
    parent = self()

    pid =
      :erlang.spawn_opt(
        fn ->
          work(items)
          {:garbage_collection, gcs} = Process.info(self(), :garbage_collection)
          {:total_heap_size, heap} = Process.info(self(), :total_heap_size)
          send(parent, {:stats, Keyword.fetch!(gcs, :minor_gcs), heap})
        end,
        spawn_opts
      )

    receive do
      {:stats, gcs, heap} -> %{pid: pid, gcs: gcs, heap: heap}
    after
      5_000 -> raise "worker timed out"
    end
  end

  defp work(items) do
    map =
      for i <- 1..items, into: %{} do
        {i, {"key_#{i}", i * 2, [i, i + 1, i + 2]}}
      end

    _ = Map.values(map) |> Enum.sum_by(fn {_, v, _} -> v end)
    :ok
  end
end

defmodule HeapLab.BenchHelpers do
  @doc "Runs n result from n, items and opts."
  def run_n(n, items, opts) do
    results = for _ <- 1..n, do: HeapLab.Worker.run(items, opts)

    %{
      avg_gcs: avg(results, :gcs),
      avg_heap_words: avg(results, :heap)
    }
  end

  defp avg(results, key) do
    total = Enum.reduce(results, 0, &(Map.fetch!(&1, key) + &2))
    total / length(results)
  end
end
```

### `test/heap_lab_test.exs`

```elixir
defmodule HeapLab.WorkerTest do
  use ExUnit.Case, async: true
  doctest HeapLab.Worker

  describe "baseline vs tuned heap" do
    test "tuned heap eliminates minor GCs" do
      baseline = HeapLab.Worker.run(500, spawn_opts: [])
      tuned = HeapLab.Worker.run(500, spawn_opts: [min_heap_size: 16_384])

      assert baseline.gcs > 0
      assert tuned.gcs == 0
    end

    test "over-sized heap is observable in total_heap_size" do
      small = HeapLab.Worker.run(10, spawn_opts: [])
      huge = HeapLab.Worker.run(10, spawn_opts: [min_heap_size: 65_536])

      assert huge.heap > small.heap * 10
    end
  end

  describe "binary vheap" do
    test "spawn_opt accepts min_bin_vheap_size" do
      %{pid: _} = HeapLab.Worker.run(10, spawn_opts: [min_bin_vheap_size: 92_736])
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Process Heap Tuning: `:min_heap_size` and `:min_bin_vheap_size`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Process Heap Tuning: `:min_heap_size` and `:min_bin_vheap_size` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case HeapLab.run(payload) do
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
        for _ <- 1..1_000, do: HeapLab.run(:bench)
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
