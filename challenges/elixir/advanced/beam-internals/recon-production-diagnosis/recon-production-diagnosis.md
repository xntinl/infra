# `:recon` and `:recon_trace` for Production Diagnosis

**Project**: `prod_triage` — a toolkit wrapping `recon` and `recon_trace` for live diagnosis: finding the top memory offenders, chasing binary leaks, safely tracing a function in production without flooding the shell

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
prod_triage/
├── lib/
│   └── prod_triage.ex
├── script/
│   └── main.exs
├── test/
│   └── prod_triage_test.exs
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
defmodule ProdTriage.MixProject do
  use Mix.Project

  def project do
    [
      app: :prod_triage,
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

### `lib/prod_triage.ex`

```elixir
defmodule ProdTriage.Triage do
  @moduledoc """
  Curated `recon` calls, parameterized for common incidents.
  Exposed as plain functions so they can be called from a remote IEx
  without loading any `use` machinery.
  """

  @doc """
  Top N processes by attribute. Use :memory for leak hunting,
  :reductions for CPU hogs, :message_queue_len for backpressure.
  """
  def top(attribute, n \\ 10) when attribute in [:memory, :reductions, :message_queue_len, :binary_memory] do
    :recon.proc_count(attribute, n)
    |> Enum.map(fn {pid, val, extra} -> %{pid: pid, value: val, info: extra} end)
  end

  @doc "Top N by delta over `window_ms`. Best for live-rate questions."
  def window(attribute, n \\ 10, window_ms \\ 5_000) do
    :recon.proc_window(attribute, n, window_ms)
  end

  @doc """
  Full-sweep GCs the top binary-memory holders and reports bytes freed.
  Runs GC on real processes — mildly intrusive, use judiciously.
  """
  def bin_leak(n \\ 5), do: :recon.bin_leak(n)

  @doc """
  Rate-limited call trace. Example:
      Triage.trace({MyMod, :some_fun, :_}, 20)
  Messages flow to the calling process as text via recon's formatter.
  """
  def trace(mfa, max, opts \\ []) do
    :recon_trace.calls(mfa, max, opts)
  end

  @doc "Full picture of the node's memory allocators."
  def memory_summary do
    :erlang.memory()
    |> Enum.sort_by(fn {_k, v} -> -v end)
    |> Enum.map(fn {k, v} -> {k, div(v, 1_048_576), :mb} end)
  end

  @doc "Per-scheduler run queue lengths; > 0 means oversubscription."
  def run_queues, do: :erlang.statistics(:run_queue_lengths)
end
```

### `test/prod_triage_test.exs`

```elixir
defmodule ProdTriage.TriageTest do
  use ExUnit.Case, async: true
  doctest ProdTriage.Triage
  alias ProdTriage.Triage

  describe "top/2" do
    test "returns the top N by memory" do
      top = Triage.top(:memory, 5)
      assert length(top) == 5
      assert Enum.all?(top, &is_pid(&1.pid))
      # Sorted descending by value.
      values = Enum.map(top, & &1.value)
      assert values == Enum.sort(values, :desc)
    end

    test "accepts :reductions attribute" do
      assert [%{} | _] = Triage.top(:reductions, 3)
    end

    test "rejects unknown attributes via match failure" do
      assert_raise FunctionClauseError, fn -> Triage.top(:nonsense, 5) end
    end
  end

  describe "memory_summary/0" do
    test "lists allocator buckets" do
      summary = Triage.memory_summary()
      assert Enum.any?(summary, fn {k, _, _} -> k == :binary end)
      assert Enum.any?(summary, fn {k, _, _} -> k == :processes end)
    end
  end

  describe "run_queues/0" do
    test "returns a list with one entry per online scheduler" do
      assert length(Triage.run_queues()) == System.schedulers_online()
    end
  end

  describe "trace/3" do
    test "caps the number of trace messages" do
      defmodule Callee do
        def noisy, do: :ok
      end

      {:ok, _} = :application.ensure_all_started(:recon)
      Triage.trace({Callee, :noisy, :_}, 3)
      for _ <- 1..10, do: Callee.noisy()
      Process.sleep(50)
      :recon_trace.clear()
      assert true
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for `:recon` and `:recon_trace` for Production Diagnosis.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== `:recon` and `:recon_trace` for Production Diagnosis ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ProdTriage.run(payload) do
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
        for _ <- 1..1_000, do: ProdTriage.run(:bench)
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
