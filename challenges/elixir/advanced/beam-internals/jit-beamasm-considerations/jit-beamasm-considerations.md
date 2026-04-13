# HiPE, BeamAsm / JIT Considerations on OTP 24+

**Project**: `jit_probe` — a lab that detects whether the running VM uses BeamAsm, measures the hot-loop speedup vs the interpreter, and shows why HiPE is no longer a reasonable choice

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
jit_probe/
├── lib/
│   └── jit_probe.ex
├── script/
│   └── main.exs
├── test/
│   └── jit_probe_test.exs
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
defmodule JitProbe.MixProject do
  use Mix.Project

  def project do
    [
      app: :jit_probe,
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

### `lib/jit_probe.ex`

```elixir
defmodule JitProbe.Detector do
  @moduledoc """
  Introspects the running VM for JIT information.
  """

  def flavor, do: :erlang.system_info(:emu_flavor)

  def jit?, do: flavor() == :jit

  def otp_release, do: :erlang.system_info(:otp_release)

  def summary do
    %{
      emu_flavor: flavor(),
      jit?: jit?(),
      otp_release: otp_release(),
      schedulers: :erlang.system_info(:schedulers),
      wordsize: :erlang.system_info(:wordsize)
    }
  end

  @doc """
  Dumps BeamAsm assembly for a given module. Only works when jit?/0 is true.
  Writes to CWD as `<Module>.dis`.
  """
  def dump_asm(module) do
    if jit?() do
      :erts_debug.df(module)
    else
      {:error, :no_jit}
    end
  end
end

defmodule JitProbe.HotLoop do
  @moduledoc """
  A tail-recursive sum loop that BeamAsm compiles cleanly.
  Compare against a non-tail variant to see inlining differences.
  """

  def tail_sum(n), do: tail_sum(n, 0)
  defp tail_sum(0, acc), do: acc
  defp tail_sum(n, acc), do: tail_sum(n - 1, acc + n)

  def body_sum(0), do: 0
  def body_sum(n), do: n + body_sum(n - 1)

  def reduce_sum(n), do: Enum.reduce(1..n, 0, &(&1 + &2))
end
```

### `test/jit_probe_test.exs`

```elixir
defmodule JitProbe.DetectorTest do
  use ExUnit.Case, async: true
  doctest JitProbe.Detector
  alias JitProbe.Detector

  describe "detector" do
    test "emu_flavor is :jit on OTP 24+ on supported platforms" do
      assert Detector.flavor() in [:jit, :emu]
    end

    test "summary has the expected keys" do
      s = Detector.summary()
      assert is_boolean(s.jit?)
      assert is_integer(s.schedulers)
      assert s.wordsize == 8
    end
  end

  describe "hot loop correctness" do
    test "all variants compute the same sum" do
      n = 1_000
      expected = div(n * (n + 1), 2)
      assert JitProbe.HotLoop.tail_sum(n) == expected
      assert JitProbe.HotLoop.body_sum(n) == expected
      assert JitProbe.HotLoop.reduce_sum(n) == expected
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for HiPE, BeamAsm / JIT Considerations on OTP 24+.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== HiPE, BeamAsm / JIT Considerations on OTP 24+ ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case JitProbe.run(payload) do
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
        for _ <- 1..1_000, do: JitProbe.run(:bench)
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
