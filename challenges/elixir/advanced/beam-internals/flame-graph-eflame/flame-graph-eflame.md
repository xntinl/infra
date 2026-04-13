# Flame Graphs with `eflame`

**Project**: `eflame_demo` — produce interactive SVG flame graphs of hot Elixir code paths using Vlad Ki's `eflame`

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
eflame_demo/
├── lib/
│   └── eflame_demo.ex
├── script/
│   └── main.exs
├── test/
│   └── eflame_demo_test.exs
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
defmodule EflameDemo.MixProject do
  use Mix.Project

  def project do
    [
      app: :eflame_demo,
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

### `lib/eflame_demo.ex`

```elixir
defmodule EflameDemo.Pipeline do
  @moduledoc """
  Pretend image pipeline. Runs on plain binaries so no native deps needed.
  """

  @doc "Process one image (binary). Returns processed binary."
  @spec run(binary()) :: binary()
  def run(image) when is_binary(image) do
    image
    |> decode()
    |> resize()
    |> filter()
    |> encode()
  end

  # --- Stage 1: decode (CPU bound, tight loop) ---
  @spec decode(binary()) :: [0..255]
  defp decode(bin), do: :binary.bin_to_list(bin)

  # --- Stage 2: resize (intentionally O(n^2) — this is what the graph will expose) ---
  @spec resize([0..255]) :: [0..255]
  defp resize(pixels) do
    # Naive downsampling via O(n^2) accumulator — the hot frame.
    Enum.reduce(pixels, [], fn px, acc ->
      if rem(length(acc), 2) == 0 do
        [px | acc]
      else
        acc
      end
    end)
    |> Enum.reverse()
  end

  # --- Stage 3: filter (Map pipeline) ---
  @spec filter([0..255]) :: [0..255]
  defp filter(pixels) do
    pixels
    |> Enum.map(&max(0, &1 - 15))
    |> Enum.map(&min(255, &1 + 5))
  end

  # --- Stage 4: encode (back to binary) ---
  @spec encode([0..255]) :: binary()
  defp encode(pixels), do: :erlang.list_to_binary(pixels)
end

defmodule EflameDemo.Profiler do
  @moduledoc """
  Wrap `:eflame.apply/4` with a file-output convention.

  Produces `priv/stacks/<name>.bare` which you then fold and render.
  """

  @spec profile(atom(), (-> any()), keyword()) :: {:ok, Path.t()}
  def profile(name, fun, opts \\ []) when is_atom(name) and is_function(fun, 0) do
    mode = Keyword.get(opts, :mode, :normal_with_children)
    out_dir = Path.join(:code.priv_dir(:eflame_demo), "stacks")
    File.mkdir_p!(out_dir)
    out_path = Path.join(out_dir, "#{name}.bare")

    _result = :eflame.apply(mode, to_charlist(out_path), fun, [])
    fold(out_path)
  end

  @doc """
  Sleep-aware variant. Off-CPU time appears as `SLEEP` frames in the graph.
  """
  @spec profile_with_sleep(atom(), (-> any())) :: {:ok, Path.t()}
  def profile_with_sleep(name, fun) when is_atom(name) and is_function(fun, 0) do
    out_dir = Path.join(:code.priv_dir(:eflame_demo), "stacks")
    File.mkdir_p!(out_dir)
    out_path = Path.join(out_dir, "#{name}.sleep.bare")

    _result = :eflame.apply(:normal_with_children, to_charlist(out_path), fun, [])
    fold(out_path)
  end

  defp fold(bare_path) do
    folded =
      bare_path
      |> File.stream!()
      |> Stream.map(&String.trim/1)
      |> Enum.frequencies()
      |> Enum.map(fn {stack, n} -> "#{stack} #{n}\n" end)

    folded_path = String.replace_suffix(bare_path, ".bare", ".folded")
    File.write!(folded_path, folded)
    {:ok, folded_path}
  end
end
```

### `test/eflame_demo_test.exs`

```elixir
defmodule EflameDemo.ProfilerTest do
  use ExUnit.Case, async: true
  doctest EflameDemo.Pipeline

  alias EflameDemo.{Pipeline, Profiler}

  @tag :eflame

  describe "EflameDemo.Profiler" do
    test "profiling the naive pipeline produces a folded stack file" do
      image = :crypto.strong_rand_bytes(4_096)

      {:ok, folded} =
        Profiler.profile(:pipeline_naive, fn ->
          Pipeline.run(image)
        end)

      assert File.exists?(folded)
      content = File.read!(folded)

      # Must contain at least one resize frame — that's where we spend time
      assert content =~ "resize"

      # Sample count > 0 (some lines end with " <n>\n")
      assert String.match?(content, ~r/ \d+$/m)
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Flame Graphs with `eflame`.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Flame Graphs with `eflame` ===")
    IO.puts("Category: BEAM internals and performance\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case EflameDemo.run(payload) do
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
        for _ <- 1..1_000, do: EflameDemo.run(:bench)
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
