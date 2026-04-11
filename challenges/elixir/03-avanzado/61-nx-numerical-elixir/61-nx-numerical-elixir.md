# Nx — Numerical Computation for Gateway Metrics

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway is accumulating request metrics — latency
samples, error rates, payload sizes — stored as lists of floats in ETS. The ops team
wants anomaly detection: flag services whose p99 latency deviates significantly from
their rolling mean, and project future load based on recent trends. You will use `Nx`
for the tensor operations and `defn` for the compiled hot path.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── metrics/
│       │   ├── store.ex            # already exists — ETS-backed metric collector
│       │   ├── analyzer.ex         # ← you implement this
│       │   └── anomaly_detector.ex # ← and this
├── test/
│   └── api_gateway/
│       └── metrics_analyzer_test.exs # given tests — must pass without modification
├── bench/
│   └── metrics_bench.exs           # benchmark — run at the end
└── mix.exs
```

---

## The business problem

The metrics store collects latency samples per service: `[12.3, 14.1, 11.8, 98.4, 13.2, ...]`.
The anomaly detector needs to:

1. Compute rolling statistics (mean, std dev) over a sliding window of samples
2. Flag samples more than N standard deviations from the mean as anomalies
3. Fit a linear trend to the last 60 minutes of request counts to project the next 15 minutes
4. Process batches of samples at maximum throughput — compiled with `defn`

The computations must be fast enough to run on every request flush (every 10 seconds)
without blocking the main request path.

---

## Why Nx instead of plain Elixir math

The naive approach — `Enum.sum(samples) / length(samples)` — copies data, allocates
intermediate lists, and runs in O(n) interpreted Elixir. For 10,000 samples across
50 services (500,000 values), this is measurably slow.

Nx tensors are contiguous binary arrays. Operations run in native code (C, XLA). The
key difference is `defn`: Nx macros that compile the entire function — including loops
and conditionals — into a single native kernel. The BEAM calls the compiled kernel once
per invocation instead of dispatching each arithmetic operation through the VM.

The trade-off: `defn` functions can only call other `defn` functions and Nx operations.
They cannot call arbitrary Elixir code. The boundary between `defn` and regular Elixir
is the performance-vs-expressiveness trade-off you will feel in this exercise.

---

## Why `defn` improves performance over `Nx.*` calls from regular Elixir

Each `Nx.*` call from regular Elixir code dispatches to the backend, transfers control,
and returns a tensor. A chain of 10 operations is 10 round-trips. `defn` traces the
entire function and compiles it into a single native kernel — the 10 operations execute
as one, with no intermediate dispatch overhead and with opportunities for backend-level
fusion (e.g., EXLA can fuse `add + multiply` into a single GPU kernel).

---

## Implementation

### Step 1: `mix.exs` — add Nx

```elixir
defp deps do
  [
    # existing deps...
    {:nx, "~> 0.7"},
    {:exla, "~> 0.7", only: [:dev, :prod]},  # optional — CPU/GPU acceleration
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: `lib/api_gateway/metrics/analyzer.ex`

`# TODO` marks what you implement. Do not change the function signatures.

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
    # TODO: convert samples to an Nx tensor with type :f32
    # TODO: compute mean and std_dev using Nx.mean/1 and Nx.standard_deviation/1
    # TODO: convert results back to floats with Nx.to_number/1
    # TODO: return {mean, std_dev}
  end

  @doc """
  Returns indices of samples that deviate more than `threshold` standard
  deviations from the mean. Returns a list of integer indices.
  """
  @spec anomaly_indices(list(float()), float()) :: list(non_neg_integer())
  def anomaly_indices(samples, threshold \\ 3.0) when is_list(samples) do
    # TODO: compute stats/1 to get mean and std_dev
    # TODO: build tensor from samples
    # TODO: compute z_scores = |sample - mean| / std_dev for each element
    # TODO: find indices where z_score > threshold
    # HINT: Nx.abs(Nx.subtract(t, mean)) |> Nx.divide(std_dev)
    # HINT: Nx.greater(z_scores, threshold) gives a boolean tensor
    # HINT: Nx.to_flat_list/1 to get back a list of 0/1; filter with indices
  end

  @doc """
  Fits a linear trend y = a*x + b to the samples.
  Returns `{slope, intercept}` as plain floats.

  Uses closed-form least squares: a = cov(x,y) / var(x).
  """
  @spec linear_trend(list(float())) :: {float(), float()}
  def linear_trend(samples) when length(samples) >= 2 do
    # TODO: build x = [0, 1, 2, ..., n-1] and y = samples as tensors
    # TODO: compute slope: cov(x,y) / var(x)
    #   cov(x,y) = mean(x*y) - mean(x)*mean(y)
    #   var(x)   = mean(x^2) - mean(x)^2
    # TODO: compute intercept: mean(y) - slope * mean(x)
    # TODO: return {slope, intercept} as floats
  end

  # ---------------------------------------------------------------------------
  # defn hot path — compiled, called from batch processing
  # ---------------------------------------------------------------------------

  @doc """
  Compiled rolling z-score computation for a batch of sample windows.
  Each row in `windows` is a window of samples. Returns z-scores per element.

  Shape: {batch, window_size} → {batch, window_size}
  """
  defn rolling_zscore(windows) do
    # TODO: compute per-row mean with axes: [1], keep_axes: true
    # TODO: compute per-row std_dev with axes: [1], keep_axes: true
    # TODO: return (windows - mean) / (std_dev + epsilon) where epsilon = 1.0e-7
    # Broadcasting handles the shape mismatch automatically when keep_axes: true
  end
end
```

### Step 3: `lib/api_gateway/metrics/anomaly_detector.ex`

```elixir
defmodule ApiGateway.Metrics.AnomalyDetector do
  @moduledoc """
  Flags services with anomalous latency patterns.

  Reads from MetricsStore, runs statistical analysis, and returns
  a list of services that require attention.
  """
  alias ApiGateway.Metrics.{Store, Analyzer}

  @threshold_stddev 3.0
  @min_samples 10

  @doc """
  Returns a list of `%{service: name, anomalies: count, p99: float}` maps
  for services with at least one anomalous sample in their recent window.
  """
  def detect_anomalies do
    Store.all_services()
    |> Enum.map(fn service_name ->
      samples = Store.get_samples(service_name, limit: 100)
      analyze_service(service_name, samples)
    end)
    |> Enum.reject(&is_nil/1)
  end

  # -- private --

  defp analyze_service(_name, samples) when length(samples) < @min_samples, do: nil

  defp analyze_service(name, samples) do
    # TODO: call Analyzer.anomaly_indices/2 with @threshold_stddev
    # TODO: if no anomalies, return nil
    # TODO: compute p99: sort samples, take the element at the 99th percentile index
    # TODO: return %{service: name, anomalies: length(indices), p99: p99_value}
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/api_gateway/metrics_analyzer_test.exs
defmodule ApiGateway.Metrics.AnalyzerTest do
  use ExUnit.Case, async: true

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

### Step 5: Run the tests

```bash
mix test test/api_gateway/metrics_analyzer_test.exs --trace
```

### Step 6: Benchmark

```elixir
# bench/metrics_bench.exs
samples_1k  = Enum.map(1..1_000, fn _ -> :rand.uniform() * 100 end)
samples_10k = Enum.map(1..10_000, fn _ -> :rand.uniform() * 100 end)

windows = Nx.random_uniform({100, 60}, type: :f32)

Benchee.run(
  %{
    "stats — 1k samples"  => fn -> ApiGateway.Metrics.Analyzer.stats(samples_1k) end,
    "stats — 10k samples" => fn -> ApiGateway.Metrics.Analyzer.stats(samples_10k) end,
    "rolling_zscore — 100 windows of 60" =>
      fn -> ApiGateway.Metrics.Analyzer.rolling_zscore(windows) end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

```bash
mix run bench/metrics_bench.exs
```

---

## Trade-off analysis

| Aspect | Nx + `defn` | Plain `Enum` + `:math` | `Statistics` library |
|--------|------------|------------------------|----------------------|
| Throughput on large tensors | Very high | Low | Medium |
| `defn` compilation cost | One-time per call-site | None | None |
| Arbitrary Elixir in hot path | No (defn only) | Yes | Yes |
| GPU acceleration | EXLA backend | No | No |
| Type safety | Explicit tensor types | None | None |
| Debugging | Hard (compiled) | Easy | Medium |

Reflection question: `defn` functions cannot call arbitrary Elixir. What does this mean
for error handling inside a `defn`? What happens if a division by zero occurs at the Nx
level versus at the Elixir level?

---

## Common production mistakes

**1. Converting tensors to lists inside `defn`**
`Nx.to_flat_list/1` is not available in `defn` — it crosses the Nx/Elixir boundary.
Move all tensor-to-Elixir conversions outside `defn` functions.

**2. Using `System.os_time` for time-based sliding windows**
If the BEAM node experiences an NTP adjustment, `os_time` can jump backwards. Use
`System.monotonic_time` for any comparison where ordering matters.

**3. Not specifying tensor types explicitly**
Default type depends on the backend. On EXLA, integers may become `:s64`; on BinaryBackend,
`:f32`. Code that works on one backend silently breaks on another. Always pass `type: :f32`
or `type: :s64` explicitly when creating tensors from user data.

**4. Building tensors inside a loop**
`Nx.tensor([])` in a loop rebuilds the tensor each iteration, allocating memory each time.
Build the tensor once from the full list, then operate on it with batch operations.

**5. Forgetting EXLA is optional**
EXLA requires XLA to be compiled on the first run (can take minutes). In CI, use the
default `Nx.BinaryBackend` unless you are specifically benchmarking backend differences.

---

## Resources

- [Nx HexDocs](https://hexdocs.pm/nx/Nx.html) — tensor operations reference
- [Nx.Defn](https://hexdocs.pm/nx/Nx.Defn.html) — `defn` semantics and limitations
- [EXLA Backend](https://hexdocs.pm/exla/EXLA.html) — XLA compilation and GPU support
- [Machine Learning in Elixir — Sean Moriarity](https://pragprog.com/titles/smelixir/machine-learning-in-elixir/) — chapters on Nx fundamentals
