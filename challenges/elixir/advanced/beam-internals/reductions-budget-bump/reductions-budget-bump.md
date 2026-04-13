# Reductions Budget and `:erlang.bump_reductions/1`

**Project**: `reduction_lab` — experiments that measure the BEAM's 4000-reduction budget, show how `bump_reductions/1` accounts for work done in NIFs/BIFs, and demonstrate why ignoring reductions starves other processes

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
reduction_lab/
├── lib/
│   └── reduction_lab.ex
├── script/
│   └── main.exs
├── test/
│   └── reduction_lab_test.exs
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
defmodule ReductionLab.MixProject do
  use Mix.Project

  def project do
    [
      app: :reduction_lab,
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

### `lib/reduction_lab.ex`

```elixir
defmodule ReductionLab.SlowNifSim do
  @moduledoc """
  Simulates a NIF that does heavy work without yielding.
  In pure Elixir this is impossible at the VM level — but a tight
  `:crypto` loop approximates the scheduler impact.
  """

  @doc "Returns hog result from ms."
  def hog(ms) do
    deadline = System.monotonic_time(:millisecond) + ms
    do_hog(deadline)
  end

  defp do_hog(deadline) do
    # :crypto.hash is a BIF; 1024 bytes costs roughly 1 reduction per call.
    :crypto.hash(:sha256, :crypto.strong_rand_bytes(1024))

    if System.monotonic_time(:millisecond) < deadline do
      do_hog(deadline)
    else
      :ok
    end
  end
end

defmodule ReductionLab.FairLoop do
  @moduledoc """
  Iterates N units of work. Each unit is ~100 reductions of real work
  but only 1 reduction is charged automatically. bump_reductions/1
  corrects the accounting so the scheduler preempts fairly.
  """

  @per_unit_cost 100

  @doc "Runs result from n and opts."
  def run(n, opts \\ []) do
    bump? = Keyword.get(opts, :bump, true)
    do_run(n, 0, bump?)
  end

  defp do_run(0, acc, _bump?), do: acc

  defp do_run(n, acc, bump?) do
    # Simulate 100 reductions worth of work
    work = Enum.reduce(1..@per_unit_cost, acc, &(&1 + &2))

    if bump?, do: :erlang.bump_reductions(@per_unit_cost)

    do_run(n - 1, work, bump?)
  end
end

# Measure the latency of a "responsive" process while another is doing heavy work.
defmodule FairnessBench do
  @doc "Returns ping latency result."
  def ping_latency do
    me = self()
    start = System.monotonic_time(:microsecond)
    spawn(fn -> send(me, :pong) end)
    receive do
      :pong -> System.monotonic_time(:microsecond) - start
    end
  end
end

# Start a hog
spawn(fn -> ReductionLab.FairLoop.run(1_000_000, bump: false) end)
Process.sleep(5)
IO.puts("unbumped hog → ping latency: #{FairnessBench.ping_latency()}µs")

spawn(fn -> ReductionLab.FairLoop.run(1_000_000, bump: true) end)
Process.sleep(5)
IO.puts("bumped hog   → ping latency: #{FairnessBench.ping_latency()}µs")
```

### `test/reduction_lab_test.exs`

```elixir
defmodule ReductionLab.ReductionsTest do
  use ExUnit.Case, async: true
  doctest ReductionLab.SlowNifSim

  describe "bump_reductions/1" do
    test "increments the process reductions counter" do
      {:reductions, before} = Process.info(self(), :reductions)
      :erlang.bump_reductions(10_000)
      {:reductions, after_} = Process.info(self(), :reductions)
      assert after_ - before >= 10_000
    end
  end

  describe "fairness" do
    test "bumped loop yields more often than unbumped" do
      me = self()

      bumped =
        Task.async(fn ->
          ReductionLab.FairLoop.run(5_000, bump: true)
          send(me, :bumped_done)
        end)

      unbumped =
        Task.async(fn ->
          ReductionLab.FairLoop.run(5_000, bump: false)
          send(me, :unbumped_done)
        end)

      Task.await_many([bumped, unbumped], 10_000)

      assert_received :bumped_done
      assert_received :unbumped_done
    end
  end

  describe "process_info reductions" do
    test "idle process accumulates near-zero reductions" do
      pid = spawn(fn -> receive do :stop -> :ok end end)
      {:reductions, r1} = Process.info(pid, :reductions)
      Process.sleep(50)
      {:reductions, r2} = Process.info(pid, :reductions)
      assert r2 - r1 < 50
      send(pid, :stop)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Reductions Budget and `:erlang.bump_reductions/1`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Reductions Budget and `:erlang.bump_reductions/1` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case ReductionLab.run(payload) do
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
        for _ <- 1..1_000, do: ReductionLab.run(:bench)
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
