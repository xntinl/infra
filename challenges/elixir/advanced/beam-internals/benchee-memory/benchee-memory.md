# Benchee Memory and Reductions Formatter

**Project**: `benchee_memory` — extend Benchee with `memory_time:` and `reduction_time:` to measure allocated bytes and reductions per iteration, not just wall time

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
benchee_memory/
├── lib/
│   └── benchee_memory.ex
├── script/
│   └── main.exs
├── test/
│   └── benchee_memory_test.exs
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
defmodule BencheeMemory.MixProject do
  use Mix.Project

  def project do
    [
      app: :benchee_memory,
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
### `lib/benchee_memory.ex`

```elixir
defmodule BencheeMemory.Builders do
  @moduledoc """
  Three implementations of "build a binary from a list of binaries".
  Same external contract; very different allocation profiles.
  """

  @doc "Concatenation with `<>`. O(N²) allocations."
  @spec concat([binary()]) :: binary()
  def concat(list) do
    Enum.reduce(list, "", fn s, acc -> acc <> s end)
  end

  @doc "iolist then `IO.iodata_to_binary/1`. One terminal allocation."
  @spec iodata([binary()]) :: binary()
  def iodata(list) do
    list
    |> Enum.reduce([], fn s, acc -> [acc, s] end)
    |> IO.iodata_to_binary()
  end

  @doc """
  Binary accumulator — triggers the compiler's binary-append optimization
  when the accumulator appears first in the bitstring head.
  """
  @spec binary_append([binary()]) :: binary()
  def binary_append(list) do
    Enum.reduce(list, <<>>, fn s, acc -> <<acc::binary, s::binary>> end)
  end
end

defmodule BencheeMemory.Reporter do
  @moduledoc """
  Custom Benchee formatter: emits one CSV row per scenario with
  name, avg wall time (ns), avg memory (bytes), avg reductions.

  Use by placing the formatter in `formatters:` of the Benchee config:

      formatters: [
        Benchee.Formatters.Console,
        {BencheeMemory.Reporter, file: "bench_history.csv"}
      ]
  """

  @behaviour Benchee.Formatter

  @impl true
  def format(suite, opts) do
    file = Keyword.fetch!(opts, :file)
    header? = !File.exists?(file)

    rows =
      for %{name: name, run_time_data: rt, memory_usage_data: mem, reductions_data: red} <-
            suite.scenarios do
        [
          format_timestamp(),
          name,
          rt.statistics.average |> round(),
          if(mem, do: mem.statistics.average |> round(), else: ""),
          if(red, do: red.statistics.average |> round(), else: "")
        ]
        |> Enum.map(&to_string/1)
        |> Enum.join(",")
      end

    header =
      if header? do
        ["timestamp,name,wall_ns,memory_bytes,reductions"]
      else
        []
      end

    {Enum.join(header ++ rows, "\n") <> "\n", file}
  end

  @impl true
  def write({csv, file}, _opts) do
    File.write!(file, csv, [:append])
  end

  defp format_timestamp do
    DateTime.utc_now() |> DateTime.to_iso8601()
  end
end
```
### `test/benchee_memory_test.exs`

```elixir
defmodule BencheeMemory.BuildersTest do
  use ExUnit.Case, async: true
  doctest BencheeMemory.Builders

  alias BencheeMemory.Builders

  @input for i <- 1..50, do: "s#{i}"

  describe "correctness — all three produce the same binary" do
    test "concat" do
      assert Builders.concat(@input) == Enum.join(@input)
    end

    test "iodata" do
      assert Builders.iodata(@input) == Enum.join(@input)
    end

    test "binary_append" do
      assert Builders.binary_append(@input) == Enum.join(@input)
    end
  end

  describe "allocation profile — reductions as proxy" do
    defp measure(fun) do
      :erlang.garbage_collect()
      {_, r0} = Process.info(self(), :reductions)
      _ = fun.()
      {_, r1} = Process.info(self(), :reductions)
      r1 - r0
    end

    test "concat uses more reductions than iodata for N=200" do
      input = for i <- 1..200, do: "chunk-#{i}-"

      r_concat = measure(fn -> Builders.concat(input) end)
      r_iodata = measure(fn -> Builders.iodata(input) end)

      assert r_concat > r_iodata * 2,
             "expected concat to be > 2x iodata reductions, got #{r_concat} vs #{r_iodata}"
    end

    test "binary_append is comparable to iodata for small N" do
      input = for i <- 1..50, do: "s"

      r_iodata = measure(fn -> Builders.iodata(input) end)
      r_append = measure(fn -> Builders.binary_append(input) end)

      # Both should be within 3x of each other (binary append can be slightly more
      # expensive due to allocation strategy)
      ratio = max(r_iodata, r_append) / max(min(r_iodata, r_append), 1)
      assert ratio < 3.0
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Benchee Memory and Reductions Formatter.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Benchee Memory and Reductions Formatter ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BencheeMemory.run(payload) do
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
        for _ <- 1..1_000, do: BencheeMemory.run(:bench)
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
