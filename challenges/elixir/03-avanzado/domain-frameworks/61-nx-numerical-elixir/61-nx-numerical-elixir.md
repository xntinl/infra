# Nx — Numerical Computation for Gateway Metrics

## Project context

You are building `api_gateway`, an internal HTTP gateway. The gateway accumulates request metrics — latency samples, error rates, payload sizes — stored as lists of floats. The ops team wants anomaly detection: flag services whose p99 latency deviates significantly from their rolling mean, and project future load based on recent trends. You will use `Nx` for the tensor operations and `defn` for the compiled hot path. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── metrics/
│           └── analyzer.ex         # statistical analysis with Nx
├── test/
│   └── api_gateway/
│       └── metrics_analyzer_test.exs # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The metrics store collects latency samples per service: `[12.3, 14.1, 11.8, 98.4, 13.2, ...]`. The anomaly detector needs to:

1. Compute rolling statistics (mean, std dev) over a sliding window of samples
2. Flag samples more than N standard deviations from the mean as anomalies
3. Fit a linear trend to the last 60 minutes of request counts to project the next 15 minutes
4. Process batches of samples at maximum throughput — compiled with `defn`

---

## Why Nx instead of plain Elixir math

The naive approach — `Enum.sum(samples) / length(samples)` — copies data, allocates intermediate lists, and runs in O(n) interpreted Elixir. Nx tensors are contiguous binary arrays. Operations run in native code. The key difference is `defn`: Nx macros that compile the entire function into a single native kernel. The BEAM calls the compiled kernel once per invocation instead of dispatching each arithmetic operation through the VM.

The trade-off: `defn` functions can only call other `defn` functions and Nx operations. They cannot call arbitrary Elixir code.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Implementation

### Step 1: `mix.exs` — add Nx

**Objective**: Declare project dependencies and configure the Mix build.

```elixir
defp deps do
  [
    {:nx, "~> 0.7"},
    {:exla, "~> 0.7", only: [:dev, :prod]},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 2: `lib/api_gateway/metrics/analyzer.ex`

**Objective**: Build Nx-powered statistical analysis for latency anomalies: mean, std_dev, z-score masking.

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

The `stats/1` function converts a plain Elixir list to an `:f32` tensor, computes mean and standard deviation with single Nx calls, and converts back to floats. The Nx boundary is fully encapsulated — callers never see tensors.

`anomaly_indices/2` computes a z-score for each sample: `|sample - mean| / std_dev`. The result is a boolean mask tensor where `1` means the z-score exceeds the threshold. When `std_dev` is zero (all samples identical), no anomalies are possible, so it short-circuits to an empty list.

`linear_trend/1` implements ordinary least squares regression in closed form. The slope formula `cov(x,y) / var(x)` avoids matrix inversion and works correctly for the 1D case.

`rolling_zscore/1` is the `defn` hot path. It operates on a 2D tensor where each row is a window of samples. The epsilon prevents division by zero when all values in a window are identical.

### Step 3: Given tests — must pass without modification

**Objective**: Verify stats/1, anomaly_indices/2, linear_trend/1 and rolling_zscore/1 against regression test suite.

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

### Step 4: Run the tests

**Objective**: Verify the implementation by running the test suite.

```bash
mix test test/api_gateway/metrics_analyzer_test.exs --trace
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.

---

## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---
## Trade-off analysis

| Aspect | Nx + `defn` | Plain `Enum` + `:math` | `Statistics` library |
|--------|------------|------------------------|----------------------|
| Throughput on large tensors | Very high | Low | Medium |
| `defn` compilation cost | One-time per call-site | None | None |
| Arbitrary Elixir in hot path | No (defn only) | Yes | Yes |
| GPU acceleration | EXLA backend | No | No |
| Type safety | Explicit tensor types | None | None |

Reflection question: `defn` functions cannot call arbitrary Elixir. What does this mean for error handling inside a `defn`? What happens if a division by zero occurs at the Nx level versus at the Elixir level?

---

## Common production mistakes

**1. Converting tensors to lists inside `defn`**
`Nx.to_flat_list/1` is not available in `defn` — it crosses the Nx/Elixir boundary. Move all tensor-to-Elixir conversions outside `defn` functions.

**2. Not specifying tensor types explicitly**
Default type depends on the backend. Always pass `type: :f32` or `type: :s64` explicitly when creating tensors from user data.

**3. Building tensors inside a loop**
`Nx.tensor([])` in a loop rebuilds the tensor each iteration. Build the tensor once from the full list, then operate on it with batch operations.

**4. Forgetting EXLA is optional**
EXLA requires XLA to be compiled on the first run. In CI, use the default `Nx.BinaryBackend` unless you are specifically benchmarking backend differences.

---

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
defp deps do
  [
    {:nx, "~> 0.7"},
    {:exla, "~> 0.7", only: [:dev, :prod]},
    {:benchee, "~> 1.3", only: :dev}
  ]


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

  @doc """
  Returns indices of samples that deviate more than `threshold` standard
  deviations from the mean. Returns a list of integer indices.
  """
  @spec anomaly_indices(list(float()), float()) :: list(non_neg_integer())
  def anomaly_indices(samples, threshold \ 3.0) when is_list(samples) do
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


# test/api_gateway/metrics_analyzer_test.exs
defmodule ApiGateway.Metrics.AnalyzerTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Metrics.Analyzer

    test "handles single element" do
      {mean, std_dev} = Analyzer.stats([42.0])
      assert_in_delta mean, 42.0, 0.001
      assert std_dev == 0.0 or is_float(std_dev)
    end
  end

    test "returns empty list when no anomalies" do
      samples = Enum.map(1..20, fn _ -> 10.0 + :rand.uniform() end)
      indices = Analyzer.anomaly_indices(samples, 3.0)
      assert indices == []
    end
  end

    test "flat trend has slope ~0" do
      samples = List.duplicate(5.0, 10)
      {slope, _intercept} = Analyzer.linear_trend(samples)
      assert_in_delta slope, 0.0, 0.01
    end
  end

    test "center element of symmetric window has z-score ~0" do
      windows = Nx.tensor([[1.0, 5.0, 9.0]], type: :f32)
      result = Analyzer.rolling_zscore(windows)
      center_zscore = result[0][1] |> Nx.to_number()
      assert_in_delta center_zscore, 0.0, 0.01
    end
  end
end

defmodule Main do
  def main do
      :ok
  end
end

Main.main()
```
