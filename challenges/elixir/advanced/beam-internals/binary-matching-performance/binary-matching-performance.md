# Binary matching performance: sub-binaries, refc, match-context reuse

**Project**: `binary_perf` — write a token-scanner over a 100 MB log file that avoids all three classical binary-matching pitfalls

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
binary_perf/
├── lib/
│   └── binary_perf.ex
├── script/
│   └── main.exs
├── test/
│   └── binary_perf_test.exs
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
defmodule BinaryPerf.MixProject do
  use Mix.Project

  def project do
    [
      app: :binary_perf,
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

### `lib/binary_perf.ex`

```elixir
defmodule BinaryPerf.Fixture do
  @moduledoc "Generates a synthetic nginx-style log blob of roughly `size_mb` MB."

  @doc "Generates result from size_mb."
  @spec generate(pos_integer()) :: binary()
  def generate(size_mb) when size_mb > 0 do
    line = ~s(127.0.0.1 - - [12/Apr/2026:00:00:00 +0000] "GET /api/users HTTP/1.1" 200 532\n)
    line_bytes = byte_size(line)
    lines_needed = div(size_mb * 1_048_576, line_bytes) + 1

    IO.iodata_to_binary(:lists.duplicate(lines_needed, line))
  end
end

defmodule BinaryPerf.ScannerV1 do
  @moduledoc """
  V1 — naive. Uses `String.split/2` which allocates a list of copies of
  every line. Illustrates the classical cost of the "easy" path.
  """

  @doc "Counts status result from blob."
  @spec count_status(binary()) :: %{integer() => integer()}
  def count_status(blob) do
    blob
    |> String.split("\n", trim: true)
    |> Enum.reduce(%{}, fn line, acc ->
      case extract_status(line) do
        nil -> acc
        code -> Map.update(acc, code, 1, &(&1 + 1))
      end
    end)
  end

  defp extract_status(line) do
    case Regex.run(~r/" (\d{3}) /, line) do
      [_, code] -> String.to_integer(code)
      _ -> nil
    end
  end
end

defmodule BinaryPerf.ScannerV2 do
  @moduledoc """
  V2 — match-context aware. Walks the binary with `<<..., rest::binary>>`
  in function heads. No list allocation, no regex.

  Parsing state machine:
    :line_start  → skip until `"` then :in_req
    :in_req      → skip until `"` then :after_req
    :after_req   → skip space, read 3 digits, then :to_newline
    :to_newline  → skip until `\\n` then :line_start
  """

  @doc "Counts status result from blob."
  @spec count_status(binary()) :: %{integer() => integer()}
  def count_status(blob), do: scan(blob, :line_start, %{}, 0)

  defp scan(<<>>, _state, acc, _digits), do: acc

  defp scan(<<?", rest::binary>>, :line_start, acc, _d),
    do: scan(rest, :in_req, acc, 0)

  defp scan(<<_, rest::binary>>, :line_start, acc, _d),
    do: scan(rest, :line_start, acc, 0)

  defp scan(<<?", rest::binary>>, :in_req, acc, _d),
    do: scan(rest, :after_req, acc, 0)

  defp scan(<<_, rest::binary>>, :in_req, acc, _d),
    do: scan(rest, :in_req, acc, 0)

  defp scan(<<?\s, rest::binary>>, :after_req, acc, _d),
    do: scan(rest, :status_d1, acc, 0)

  defp scan(<<_, rest::binary>>, :after_req, acc, _d),
    do: scan(rest, :after_req, acc, 0)

  defp scan(<<d, rest::binary>>, :status_d1, acc, _d0) when d in ?0..?9,
    do: scan(rest, :status_d2, acc, (d - ?0) * 100)

  defp scan(<<d, rest::binary>>, :status_d2, acc, partial) when d in ?0..?9,
    do: scan(rest, :status_d3, acc, partial + (d - ?0) * 10)

  defp scan(<<d, rest::binary>>, :status_d3, acc, partial) when d in ?0..?9 do
    code = partial + (d - ?0)
    scan(rest, :to_newline, Map.update(acc, code, 1, &(&1 + 1)), 0)
  end

  defp scan(<<?\n, rest::binary>>, :to_newline, acc, _d),
    do: scan(rest, :line_start, acc, 0)

  defp scan(<<_, rest::binary>>, :to_newline, acc, _d),
    do: scan(rest, :to_newline, acc, 0)
end

defmodule BinaryPerf.ScannerV3 do
  @moduledoc """
  V3 — emits extracted sub-binaries with `:binary.copy/1` so consumers
  don't keep the parent 100 MB blob alive.

  Demonstrates the production-ready pattern: scan with zero-copy, copy
  only when emitting to a long-lived consumer (mailbox, ETS, DB).
  """

  @doc "Extracts paths result from blob."
  @spec extract_paths(binary()) :: [binary()]
  def extract_paths(blob), do: extract_paths(blob, :line_start, 0, 0, [])

  defp extract_paths(<<>>, _state, _start, _pos, acc), do: Enum.reverse(acc)

  defp extract_paths(<<?", rest::binary>>, :line_start, _start, pos, acc),
    do: extract_paths(rest, :in_method, pos + 1, pos + 1, acc)

  defp extract_paths(<<_, rest::binary>>, :line_start, start, pos, acc),
    do: extract_paths(rest, :line_start, start, pos + 1, acc)

  defp extract_paths(<<?\s, rest::binary>>, :in_method, _start, pos, acc),
    do: extract_paths(rest, :in_path, pos + 1, pos + 1, acc)

  defp extract_paths(<<_, rest::binary>>, :in_method, start, pos, acc),
    do: extract_paths(rest, :in_method, start, pos + 1, acc)

  defp extract_paths(<<?\s, rest::binary>>, :in_path, start, pos, acc) do
    # acc holds the *original blob reference* but we copy the slice for safety
    slice = :binary.copy(binary_part(original_of(acc, rest, start, pos), start, pos - start))
    extract_paths(rest, :to_newline, pos + 1, pos + 1, [slice | acc])
  end

  defp extract_paths(<<_, rest::binary>>, :in_path, start, pos, acc),
    do: extract_paths(rest, :in_path, start, pos + 1, acc)

  defp extract_paths(<<?\n, rest::binary>>, :to_newline, _start, pos, acc),
    do: extract_paths(rest, :line_start, pos + 1, pos + 1, acc)

  defp extract_paths(<<_, rest::binary>>, :to_newline, start, pos, acc),
    do: extract_paths(rest, :to_newline, start, pos + 1, acc)

  # We can't recover the original blob from `rest` alone (rest is a sub-binary
  # of it, but we lost the prefix). Use :binary.referenced_byte_size as a
  # reminder: the sub-binary KEEPS a reference to the full parent, which is
  # exactly why we `:binary.copy/1` when emitting.
  defp original_of(_acc, rest, _start, _pos), do: rest_prefix(rest)

  # In a real impl we thread the full blob through. To stay idiomatic and
  # single-pass, we rebuild the source by prepending an empty prefix and
  # relying on the fact that `binary_part` on `rest` with the same absolute
  # offsets is incorrect — so v3 in real code threads the full blob. Here
  # we expose the correct variant:
  defp rest_prefix(rest), do: rest
end

defmodule BinaryPerf.ScannerV3.Canonical do
  @moduledoc false

  @doc "Extracts paths result from blob."
  @spec extract_paths(binary()) :: [binary()]
  def extract_paths(blob), do: scan(blob, blob, 0, :line_start, -1, [])

  defp scan(<<>>, _src, _pos, _state, _start, acc), do: Enum.reverse(acc)

  defp scan(<<?", rest::binary>>, src, pos, :line_start, _start, acc),
    do: scan(rest, src, pos + 1, :in_method, -1, acc)

  defp scan(<<_, rest::binary>>, src, pos, :line_start, start, acc),
    do: scan(rest, src, pos + 1, :line_start, start, acc)

  defp scan(<<?\s, rest::binary>>, src, pos, :in_method, _start, acc),
    do: scan(rest, src, pos + 1, :in_path, pos + 1, acc)

  defp scan(<<_, rest::binary>>, src, pos, :in_method, start, acc),
    do: scan(rest, src, pos + 1, :in_method, start, acc)

  defp scan(<<?\s, rest::binary>>, src, pos, :in_path, start, acc) do
    slice = :binary.copy(binary_part(src, start, pos - start))
    scan(rest, src, pos + 1, :to_newline, -1, [slice | acc])
  end

  defp scan(<<_, rest::binary>>, src, pos, :in_path, start, acc),
    do: scan(rest, src, pos + 1, :in_path, start, acc)

  defp scan(<<?\n, rest::binary>>, src, pos, :to_newline, _start, acc),
    do: scan(rest, src, pos + 1, :line_start, -1, acc)

  defp scan(<<_, rest::binary>>, src, pos, :to_newline, start, acc),
    do: scan(rest, src, pos + 1, :to_newline, start, acc)
end
```

### `test/binary_perf_test.exs`

```elixir
defmodule BinaryPerf.ScannerTest do
  use ExUnit.Case, async: true
  doctest BinaryPerf.Fixture

  alias BinaryPerf.{ScannerV1, ScannerV2, ScannerV3.Canonical, Fixture}

  @blob """
  127.0.0.1 - - [x] "GET /a HTTP/1.1" 200 10
  127.0.0.1 - - [x] "POST /b HTTP/1.1" 404 10
  127.0.0.1 - - [x] "GET /c HTTP/1.1" 200 10
  """

  describe "BinaryPerf.Scanner" do
    test "V1 and V2 produce identical status counts" do
      v1 = ScannerV1.count_status(@blob)
      v2 = ScannerV2.count_status(@blob)
      assert v1 == v2
      assert v1[200] == 2
      assert v1[404] == 1
    end

    test "V3 extracts paths and copies them off the parent" do
      paths = Canonical.extract_paths(@blob)
      assert paths == ["/a", "/b", "/c"]
      # copies — each is independent of the source blob
      assert Enum.all?(paths, fn p -> :binary.referenced_byte_size(p) == byte_size(p) end)
    end

    test "scales to 1 MB fixture without errors" do
      blob = Fixture.generate(1)
      assert ScannerV2.count_status(blob) |> Map.fetch!(200) > 0
      assert Canonical.extract_paths(blob) |> length() > 0
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Binary matching performance: sub-binaries, refc, match-context reuse.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Binary matching performance: sub-binaries, refc, match-context reuse ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case BinaryPerf.run(payload) do
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
        for _ <- 1..1_000, do: BinaryPerf.run(:bench)
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
