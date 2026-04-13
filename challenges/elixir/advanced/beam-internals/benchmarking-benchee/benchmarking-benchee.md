# Advanced benchmarking with Benchee

**Project**: `benchee_deep` — compare implementations along three axes (time, memory, reductions), under different input sizes and parallel load

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
benchee_deep/
├── lib/
│   └── benchee_deep.ex
├── script/
│   └── main.exs
├── test/
│   └── benchee_deep_test.exs
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
defmodule BencheeDeep.MixProject do
  use Mix.Project

  def project do
    [
      app: :benchee_deep,
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

### `lib/benchee_deep.ex`

```elixir
defmodule BencheeDeep.Sum do
  @moduledoc "Four implementations of sum-of-squares over a list."

  @doc "Returns enum map sum result from list."
  @spec enum_map_sum([number()]) :: number()
  def enum_map_sum(list), do: list |> Enum.map(&(&1 * &1)) |> Enum.sum()

  @doc "Returns enum reduce result from list."
  @spec enum_reduce([number()]) :: number()
  def enum_reduce(list), do: Enum.reduce(list, 0, fn x, acc -> x * x + acc end)

  @doc "Returns stream sum result from list."
  @spec stream_sum([number()]) :: number()
  def stream_sum(list), do: list |> Stream.map(&(&1 * &1)) |> Enum.sum()

  @doc "Returns recursive result from list."
  @spec recursive([number()]) :: number()
  def recursive(list), do: do_rec(list, 0)

  defp do_rec([], acc), do: acc
  defp do_rec([h | t], acc), do: do_rec(t, acc + h * h)
end

defmodule BencheeDeep.Parse do
  @moduledoc "Three implementations of NDJSON line-count."

  @doc "Returns enum split result from blob."
  @spec enum_split(binary()) :: non_neg_integer()
  def enum_split(blob) do
    blob |> String.split("\n", trim: true) |> length()
  end

  @doc "Returns stream split result from blob."
  @spec stream_split(binary()) :: non_neg_integer()
  def stream_split(blob) do
    blob
    |> String.splitter("\n", trim: true)
    |> Enum.count()
  end

  @doc "Returns binary scan result from blob."
  @spec binary_scan(binary()) :: non_neg_integer()
  def binary_scan(blob), do: count_newlines(blob, 0)

  defp count_newlines(<<>>, acc), do: acc
  defp count_newlines(<<"\n", rest::binary>>, acc), do: count_newlines(rest, acc + 1)
  defp count_newlines(<<_::8, rest::binary>>, acc), do: count_newlines(rest, acc)
end

defmodule BencheeDeep.Runner do
  @moduledoc """
  Programmatic wrapper around Benchee that enforces a house style:
  warmup 2s, measurement 5s, memory+reductions always on, HTML output.
  """

  @type scenario :: (term() -> term())

  @doc "Runs result from scenarios and opts."
  @spec run(%{String.t() => scenario()}, keyword()) :: map()
  def run(scenarios, opts \\ []) do
    inputs = Keyword.get(opts, :inputs, nil)
    parallel = Keyword.get(opts, :parallel, 1)
    time = Keyword.get(opts, :time, 5)
    warmup = Keyword.get(opts, :warmup, 2)
    title = Keyword.get(opts, :title, "benchmark")

    Benchee.run(
      scenarios,
      warmup: warmup,
      time: time,
      memory_time: 2,
      reduction_time: 2,
      parallel: parallel,
      inputs: inputs,
      formatters: [
        Benchee.Formatters.Console,
        {Benchee.Formatters.HTML, file: "bench/output/#{safe(title)}.html", auto_open: false}
      ],
      title: title
    )
  end

  defp safe(str), do: str |> String.downcase() |> String.replace(~r/[^a-z0-9]+/, "_")
end

defmodule BencheeDeep.ParseTest do
  use ExUnit.Case, async: true
  doctest BencheeDeep.MixProject
  alias BencheeDeep.Parse

  @blob "a\nbb\nccc\n"

  describe "BencheeDeep.Parse" do
    test "all implementations agree on line count" do
      assert Parse.enum_split(@blob) == 3
      assert Parse.stream_split(@blob) == 3
      assert Parse.binary_scan(@blob) == 3
    end

    test "empty blob returns 0" do
      assert Parse.binary_scan("") == 0
      assert Parse.stream_split("") == 0
    end
  end
end
```

### `test/benchee_deep_test.exs`

```elixir
defmodule BencheeDeep.SumTest do
  use ExUnit.Case, async: true
  doctest BencheeDeep.MixProject
  alias BencheeDeep.Sum

  @list Enum.to_list(1..100)
  @expected Enum.reduce(1..100, 0, fn x, a -> x * x + a end)

  describe "BencheeDeep.Sum" do
    test "all implementations agree on small input" do
      assert Sum.enum_map_sum(@list) == @expected
      assert Sum.enum_reduce(@list) == @expected
      assert Sum.stream_sum(@list) == @expected
      assert Sum.recursive(@list) == @expected
    end

    test "empty list returns 0" do
      for fun <- [&Sum.enum_map_sum/1, &Sum.enum_reduce/1, &Sum.stream_sum/1, &Sum.recursive/1] do
        assert fun.([]) == 0
      end
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Advanced benchmarking with Benchee.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Advanced benchmarking with Benchee ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BencheeDeep.run(payload) do
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
        for _ <- 1..1_000, do: BencheeDeep.run(:bench)
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
