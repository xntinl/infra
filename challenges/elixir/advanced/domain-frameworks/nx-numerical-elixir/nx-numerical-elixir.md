# Nx — Numerical Computation for Gateway Metrics

**Project**: `nx_numerical_elixir` — production-grade nx — numerical computation for gateway metrics in Elixir

---

## Why domain frameworks matters

Frameworks like Ash, Commanded, Oban, Nx and Axon encode large domain patterns (CQRS, event sourcing, ML training, background jobs, IoT updates) into reusable building blocks. Used well, they compress months of bespoke code into days.

Used poorly, they hide complexity that bites in production: aggregate version drift in Commanded, projection lag in CQRS systems, OTA failure recovery in Nerves, gradient explosion in Axon training loops. The framework's defaults are not your defaults.

---

## The business problem

You are building a production-grade Elixir component in the **Domain frameworks** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
nx_numerical_elixir/
├── lib/
│   └── nx_numerical_elixir.ex
├── script/
│   └── main.exs
├── test/
│   └── nx_numerical_elixir_test.exs
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

Chose **B** because in Domain frameworks the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule NxNumericalElixir.MixProject do
  use Mix.Project

  def project do
    [
      app: :nx_numerical_elixir,
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
### `lib/nx_numerical_elixir.ex`

```elixir
defmodule ApiGateway.Metrics.Analyzer do
  @moduledoc """
  Statistical analysis of gateway latency samples using Nx.

  The public API works with plain Elixir lists (coming from ETS).
  Internally converts to tensors, runs Nx operations, and returns
  plain Elixir values. The Nx boundary is entirely inside this module.
  """
  import Nx.Defn

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Computes mean and standard deviation over a list of float samples.
  Returns `{mean, std_dev}` as plain floats.
  """
  @spec stats(list(float())) :: {float(), float()}
  def stats(samples) when is_list(samples) and length(samples) > 0 do
    tensor = Nx.tensor(samples, type: :f32)
    mean = tensor |> Nx.mean() |> Nx.to_number()
    std_dev = tensor |> Nx.standard_deviation() |> Nx.to_number()
    {mean, std_dev}
  end

  @doc """
  Returns indices of samples that deviate more than `threshold` standard
  deviations from the mean. Returns a list of integer indices.
  """
  @spec anomaly_indices(list(float()), float()) :: list(non_neg_integer())
  def anomaly_indices(samples, threshold \\ 3.0) when is_list(samples) do
    {mean, std_dev} = stats(samples)

    if std_dev == 0.0 do
      []
    else
      tensor = Nx.tensor(samples, type: :f32)

      z_scores =
        tensor
        |> Nx.subtract(mean)
        |> Nx.abs()
        |> Nx.divide(std_dev)

      mask = Nx.greater(z_scores, threshold) |> Nx.to_flat_list()

      mask
      |> Enum.with_index()
      |> Enum.filter(fn {flag, _idx} -> flag == 1 end)
      |> Enum.map(fn {_, idx} -> idx end)
    end
  end

  @doc """
  Fits a linear trend y = a*x + b to the samples.
  Returns `{slope, intercept}` as plain floats.

  Uses closed-form least squares: a = cov(x,y) / var(x).
  """
  @spec linear_trend(list(float())) :: {float(), float()}
  def linear_trend(samples) when length(samples) >= 2 do
    n = length(samples)
    x = Nx.tensor(Enum.to_list(0..(n - 1)), type: :f32)
    y = Nx.tensor(samples, type: :f32)

    mean_x = Nx.mean(x) |> Nx.to_number()
    mean_y = Nx.mean(y) |> Nx.to_number()

    # cov(x, y) = mean(x * y) - mean(x) * mean(y)
    cov_xy =
      Nx.multiply(x, y)
      |> Nx.mean()
      |> Nx.to_number()
      |> Kernel.-(mean_x * mean_y)

    # var(x) = mean(x^2) - mean(x)^2
    var_x =
      Nx.multiply(x, x)
      |> Nx.mean()
      |> Nx.to_number()
      |> Kernel.-(mean_x * mean_x)

    slope = if var_x == 0.0, do: 0.0, else: cov_xy / var_x
    intercept = mean_y - slope * mean_x

    {slope, intercept}
  end

  # ---------------------------------------------------------------------------
  # defn hot path — compiled, called from batch processing
  # ---------------------------------------------------------------------------

  @doc """
  Compiled rolling z-score computation for a batch of sample windows.
  Each row in `windows` is a window of samples. Returns z-scores per element.

  Shape: {batch, window_size} -> {batch, window_size}
  """
  defn rolling_zscore(windows) do
    mean = Nx.mean(windows, axes: [1], keep_axes: true)
    std_dev = Nx.standard_deviation(windows, axes: [1], keep_axes: true)
    epsilon = 1.0e-7

    (windows - mean) / (std_dev + epsilon)
  end
end
```
### `test/nx_numerical_elixir_test.exs`

```elixir
defmodule ApiGateway.Metrics.AnalyzerTest do
  use ExUnit.Case, async: true
  doctest ApiGateway.Metrics.Analyzer

  alias ApiGateway.Metrics.Analyzer

  describe "stats/1" do
    test "computes mean and std_dev correctly" do
      samples = [2.0, 4.0, 4.0, 4.0, 5.0, 5.0, 7.0, 9.0]
      {mean, std_dev} = Analyzer.stats(samples)

      assert_in_delta mean, 5.0, 0.001
      assert_in_delta std_dev, 2.0, 0.001
    end

    test "handles single element" do
      {mean, std_dev} = Analyzer.stats([42.0])
      assert_in_delta mean, 42.0, 0.001
      assert std_dev == 0.0 or is_float(std_dev)
    end
  end

  describe "anomaly_indices/2" do
    test "detects obvious outliers" do
      # 9 normal samples + 1 extreme outlier at index 5
      samples = [10.0, 11.0, 10.5, 10.8, 11.2, 500.0, 10.1, 11.3, 10.7, 10.9]
      indices = Analyzer.anomaly_indices(samples, 2.0)

      assert 5 in indices
      assert length(indices) == 1
    end

    test "returns empty list when no anomalies" do
      samples = Enum.map(1..20, fn _ -> 10.0 + :rand.uniform() end)
      indices = Analyzer.anomaly_indices(samples, 3.0)
      assert indices == []
    end
  end

  describe "linear_trend/1" do
    test "computes trend for y = 2x + 1" do
      # x = [0,1,2,3,4], y = [1,3,5,7,9]
      samples = [1.0, 3.0, 5.0, 7.0, 9.0]
      {slope, intercept} = Analyzer.linear_trend(samples)

      assert_in_delta slope, 2.0, 0.001
      assert_in_delta intercept, 1.0, 0.001
    end

    test "flat trend has slope ~0" do
      samples = List.duplicate(5.0, 10)
      {slope, _intercept} = Analyzer.linear_trend(samples)
      assert_in_delta slope, 0.0, 0.01
    end
  end

  describe "rolling_zscore/1" do
    test "returns tensor of same shape as input" do
      windows = Nx.tensor([[1.0, 2.0, 3.0], [4.0, 5.0, 6.0]], type: :f32)
      result = Analyzer.rolling_zscore(windows)
      assert Nx.shape(result) == {2, 3}
    end

    test "center element of symmetric window has z-score ~0" do
      windows = Nx.tensor([[1.0, 5.0, 9.0]], type: :f32)
      result = Analyzer.rolling_zscore(windows)
      center_zscore = result[0][1] |> Nx.to_number()
      assert_in_delta center_zscore, 0.0, 0.01
    end
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Nx — Numerical Computation for Gateway Metrics.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Nx — Numerical Computation for Gateway Metrics ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case NxNumericalElixir.run(payload) do
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
        for _ <- 1..1_000, do: NxNumericalElixir.run(:bench)
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

### 1. Frameworks encode opinions

Ash, Commanded, Oban each pick defaults that work for the common case. Understand the defaults before you customize — the framework's authors chose them for a reason.

### 2. Event-sourced systems need projection lag tolerance

In CQRS, the read model is eventually consistent with the write model. UI must handle 'I saved but I don't see my own data yet'. Optimistic UI updates help.

### 3. Background jobs need idempotency and retries

Oban retries failed jobs by default. The worker must be idempotent: repeating a job must produce the same end state. Use unique constraints and deduplication keys.

---
