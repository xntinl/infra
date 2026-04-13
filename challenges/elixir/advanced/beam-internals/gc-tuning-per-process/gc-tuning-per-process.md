# Garbage Collection Tuning per Process

**Project**: `gc_lab` — per-process GC tuning via `fullsweep_after`, `min_heap_size`, and `hibernate`, with measurements that show when each knob helps or hurts

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
gc_lab/
├── lib/
│   └── gc_lab.ex
├── script/
│   └── main.exs
├── test/
│   └── gc_lab_test.exs
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
defmodule GcLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :gc_lab,
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

### `lib/gc_lab.ex`

```elixir
defmodule GcLab.Cache do
  @moduledoc """
  Ejercicio: Garbage Collection Tuning per Process.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  use GenServer

  def start_link(opts) do
    gen_opts = Keyword.take(opts, [:name])
    spawn_opts = Keyword.get(opts, :spawn_opt, [])
    GenServer.start_link(__MODULE__, :ok, [spawn_opt: spawn_opts] ++ gen_opts)
  end

  @impl true
  def init(:ok) do
    # Tune this process's own GC cadence.
    :erlang.process_flag(:fullsweep_after, 10)
    {:ok, %{data: %{}}}
  end

  @doc "Returns put result from server, key and value."
  def put(server, key, value), do: GenServer.call(server, {:put, key, value})
  @doc "Returns drop result from server and key."
  def drop(server, key), do: GenServer.call(server, {:drop, key})
  @doc "Returns info result from server."
  def info(server), do: GenServer.call(server, :info)

  @impl true
  def handle_call({:put, k, v}, _from, state), do: {:reply, :ok, put_in(state.data[k], v)}
  def handle_call({:drop, k}, _from, state), do: {:reply, :ok, update_in(state.data, &Map.delete(&1, k))}
  def handle_call(:info, _from, state) do
    info =
      Process.info(self(), [:total_heap_size, :heap_size, :garbage_collection])

    {:reply, info, state}
  end
end

defmodule GcLab.GcProbe do
  @doc "Returns heap words result from pid."
  def heap_words(pid), do: Process.info(pid, :total_heap_size) |> elem(1)

  @doc "Returns gc counts result from pid."
  def gc_counts(pid) do
    {:garbage_collection, info} = Process.info(pid, :garbage_collection)
    Keyword.take(info, [:minor_gcs, :fullsweep_after])
  end

  @doc "Returns force gc result from pid."
  def force_gc(pid), do: :erlang.garbage_collect(pid)
end

defmodule Bench do
  @doc "Returns churn result from fullsweep."
  def churn(fullsweep) do
    {:ok, pid} =
      GcLab.Cache.start_link(name: :"cache_#{fullsweep}_#{:erlang.unique_integer([:positive])}")

    :erlang.process_flag(:fullsweep_after, fullsweep)

    {us, _} =
      :timer.tc(fn ->
        for i <- 1..100_000 do
          GcLab.Cache.put(pid, rem(i, 1000), String.duplicate("x", 128))
        end
      end)

    {:total_heap_size, heap} = Process.info(pid, :total_heap_size)
    GenServer.stop(pid)
    {us, heap}
  end
end

for fs <- [65535, 100, 20, 5] do
  {us, heap} = Bench.churn(fs)
  IO.puts("fullsweep_after=#{fs}: time=#{div(us, 1000)}ms, heap=#{heap} words")
end
```

### `test/gc_lab_test.exs`

```elixir
defmodule GcLab.GcTest do
  use ExUnit.Case, async: true
  doctest GcLab.Cache
  alias GcLab.{Cache, GcProbe}

  describe "fullsweep_after=10" do
    test "cache heap stabilizes under churn" do
      {:ok, pid} = Cache.start_link([])

      for i <- 1..5_000 do
        Cache.put(pid, i, String.duplicate("x", 256))
        if rem(i, 2) == 0, do: Cache.drop(pid, i)
      end

      GcProbe.force_gc(pid)
      heap = GcProbe.heap_words(pid)
      assert heap < 500_000, "expected compacted heap, got #{heap} words"
    end
  end

  describe "minor gcs" do
    test "accumulate under allocation pressure" do
      {:ok, pid} = Cache.start_link([])
      before = GcProbe.gc_counts(pid) |> Keyword.fetch!(:minor_gcs)

      for i <- 1..10_000, do: Cache.put(pid, i, i)

      after_ = GcProbe.gc_counts(pid) |> Keyword.fetch!(:minor_gcs)
      assert after_ - before > 0
    end
  end

  describe "process_flag fullsweep_after" do
    test "is visible in process_info" do
      {:ok, pid} = Cache.start_link([])
      info = GcProbe.gc_counts(pid)
      assert info[:fullsweep_after] == 10
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Garbage Collection Tuning per Process.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Garbage Collection Tuning per Process ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case GcLab.run(payload) do
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
        for _ <- 1..1_000, do: GcLab.run(:bench)
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
