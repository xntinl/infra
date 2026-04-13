# Axon — Neural Networks for Request Classification

## Project context

You are building `api_gateway`, an internal HTTP gateway. The security team wants to classify incoming requests as `normal`, `suspicious`, or `abusive` based on a feature vector extracted from each request (rate, payload size, endpoint pattern, time-of-day). All modules — feature extractor, classifier model, and training pipeline — are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── ml/
│           ├── feature_extractor.ex  # converts raw request stats to tensors
│           ├── classifier.ex         # neural network model definition
│           └── training.ex           # training pipeline
├── test/
│   └── api_gateway/
│       └── ml_classifier_test.exs    # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Statistical anomaly detection (z-score) triggers on any deviation from the mean — including legitimate traffic spikes during business hours. The security team wants a model trained on labeled examples of past attacks to distinguish malicious patterns from legitimate load. The model must be:

1. Small enough to run inference in < 1ms per request
2. Retrained periodically as new attack patterns emerge
3. Serializable — the trained model persists across gateway restarts

This is a classification problem with a fixed 8-feature input and 3-class output. A two-layer dense network is sufficient.

---

## Why Axon over raw Nx

Raw Nx gives you tensors and `defn`. Building a neural network with raw Nx means: implementing weight initialization, forward pass, loss functions, optimizer state, gradient computation, training loop, and serialization manually. Axon provides all of these. The difference is not convenience — it is correctness. Weight initialization strategies (Glorot, He) and optimizers (Adam momentum terms) have subtle numerical properties that are easy to get wrong from scratch.

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

### Step 1: `mix.exs`

**Objective**: Declare project dependencies and configure the Mix build.

```elixir
defp deps do
  [
    {:nx,   "~> 0.7"},
    {:axon, "~> 0.6"},
    {:exla, "~> 0.7", only: [:dev, :prod]}
  ]
end
```

### Step 2: `lib/api_gateway/ml/feature_extractor.ex`

**Objective**: Normalize 8 request features (rate, size, diversity, error, time, weekend, latency, burst) to [0,1] tensors.

```elixir
defmodule ApiGateway.ML.FeatureExtractor do
  @moduledoc """
  Converts raw request metadata into a normalized 8-element feature vector.

  Features (all normalized to [0, 1]):
    0: requests_per_minute / 1000  (rate, normalized)
    1: payload_bytes / 65536       (size, normalized)
    2: unique_endpoints / 100      (diversity)
    3: error_rate                  (fraction of 4xx/5xx)
    4: hour_of_day / 24            (time pattern)
    5: is_weekend (0.0 or 1.0)
    6: p99_latency_ms / 10_000     (normalized)
    7: burst_score                 (stddev of per-second counts, normalized)
  """

  @feature_count 8

  @doc "Extracts and returns an Nx tensor of shape {1, 8} from a request stats map."
  @spec extract(%{
    requests_per_minute: number(),
    payload_bytes: number(),
    unique_endpoints: number(),
    error_rate: float(),
    hour_of_day: integer(),
    is_weekend: boolean(),
    p99_latency_ms: number(),
    burst_score: number()
  }) :: Nx.Tensor.t()
  def extract(stats) do
    features = [
      min(stats.requests_per_minute / 1_000.0, 1.0),
      min(stats.payload_bytes / 65_536.0, 1.0),
      min(stats.unique_endpoints / 100.0, 1.0),
      stats.error_rate,
      stats.hour_of_day / 24.0,
      if(stats.is_weekend, do: 1.0, else: 0.0),
      min(stats.p99_latency_ms / 10_000.0, 1.0),
      min(stats.burst_score, 1.0)
    ]

    Nx.tensor([features], type: :f32)
  end

  @spec feature_count() :: pos_integer()
  def feature_count, do: @feature_count
end
```

### Step 3: `lib/api_gateway/ml/classifier.ex`

**Objective**: Build 4-layer neural net (16→8→3 neurons) with ReLU, Dropout, and softmax for 3-class classification.

```elixir
defmodule ApiGateway.ML.Classifier do
  @moduledoc """
  Neural network classifier for request patterns.

  Architecture: Dense(16, relu) -> Dropout(0.3) -> Dense(8, relu) -> Dense(3, softmax)
  Output classes: 0=normal, 1=suspicious, 2=abusive
  """

  @classes [:normal, :suspicious, :abusive]

  @doc """
  Builds and returns the model architecture. Does not initialize weights.
  """
  @spec build() :: Axon.t()
  def build do
    Axon.input("request_features", shape: {nil, 8})
    |> Axon.dense(16, activation: :relu)
    |> Axon.dropout(rate: 0.3)
    |> Axon.dense(8, activation: :relu)
    |> Axon.dense(3, activation: :softmax)
  end

  @spec classes() :: [:normal | :suspicious | :abusive]
  def classes, do: @classes
end
```

The `build/0` function defines a sequential network:
- Input layer: `{nil, 8}` — the `nil` dimension is the batch size, determined at runtime.
- Dense(16, relu): 16 neurons with ReLU activation. First hidden layer.
- Dropout(0.3): randomly deactivates 30% of neurons during training to prevent overfitting. During inference (`Axon.predict/4`), dropout is automatically disabled.
- Dense(8, relu): second hidden layer narrows the representation.
- Dense(3, softmax): output layer with 3 neurons, one per class. Softmax ensures outputs sum to 1.0, making them interpretable as probabilities.

### Step 4: `lib/api_gateway/ml/training.ex`

**Objective**: Loop Axon training for N epochs with Adam optimizer, batching, and checkpointing best weights.

```elixir
defmodule ApiGateway.ML.Training do
  @moduledoc """
  Training pipeline for the request classifier.

  The training data comes from labeled historical request logs.
  Labels: 0=normal, 1=suspicious, 2=abusive (one-hot encoded).
  """

  alias ApiGateway.ML.{Classifier, FeatureExtractor}

  @doc """
  Trains the classifier on labeled examples.

  `data` is a list of `{stats_map, label_integer}` tuples.
  `label_integer` is 0, 1, or 2.

  Returns `{model, model_state}`.
  """
  @spec train(list({map(), 0 | 1 | 2}), keyword()) :: {Axon.t(), map()}
  def train(data, opts \\ []) do
    epochs        = Keyword.get(opts, :epochs, 20)
    learning_rate = Keyword.get(opts, :learning_rate, 0.001)
    batch_size    = Keyword.get(opts, :batch_size, 32)

    model = Classifier.build()

    # Convert data to batched tensors
    {features_list, labels_list} =
      data
      |> Enum.map(fn {stats, label} ->
        features = FeatureExtractor.extract(stats) |> Nx.squeeze(axes: [0])
        one_hot = Nx.tensor(one_hot_encode(label, 3), type: :f32)
        {features, one_hot}
      end)
      |> Enum.unzip()

    # Stack all features and labels into single tensors
    all_features = Nx.stack(features_list)
    all_labels = Nx.stack(labels_list)

    # Create batched stream for training
    batches =
      Stream.zip(
        Nx.to_batched(all_features, batch_size),
        Nx.to_batched(all_labels, batch_size)
      )
      |> Stream.map(fn {feat_batch, label_batch} ->
        %{"request_features" => feat_batch, "labels" => label_batch}
      end)

    # Build training loop
    model_state =
      model
      |> Axon.Loop.trainer(:categorical_cross_entropy, Polaris.Optimizers.adam(learning_rate: learning_rate))
      |> Axon.Loop.metric(:accuracy)
      |> Axon.Loop.run(batches, %{}, epochs: epochs, compiler: EXLA)

    {model, model_state}
  end

  @doc """
  Generates synthetic labeled training data for testing.
  In production this comes from labeled incident logs.
  """
  @spec synthetic_data(pos_integer()) :: list({map(), 0 | 1 | 2})
  def synthetic_data(n \\ 1_000) do
    Enum.map(1..n, fn _ ->
      label = :rand.uniform(3) - 1

      stats =
        case label do
          0 ->
            %{
              requests_per_minute: 10 + :rand.uniform(50),
              payload_bytes: 100 + :rand.uniform(500),
              unique_endpoints: 1 + :rand.uniform(10),
              error_rate: :rand.uniform() * 0.05,
              hour_of_day: :rand.uniform(24) - 1,
              is_weekend: :rand.uniform(2) == 1,
              p99_latency_ms: 50 + :rand.uniform(100),
              burst_score: :rand.uniform() * 0.1
            }

          1 ->
            %{
              requests_per_minute: 200 + :rand.uniform(300),
              payload_bytes: 100 + :rand.uniform(2000),
              unique_endpoints: 5 + :rand.uniform(20),
              error_rate: 0.1 + :rand.uniform() * 0.2,
              hour_of_day: :rand.uniform(24) - 1,
              is_weekend: :rand.uniform(2) == 1,
              p99_latency_ms: 200 + :rand.uniform(500),
              burst_score: 0.3 + :rand.uniform() * 0.3
            }

          2 ->
            %{
              requests_per_minute: 800 + :rand.uniform(200),
              payload_bytes: 500 + :rand.uniform(10_000),
              unique_endpoints: 1 + :rand.uniform(3),
              error_rate: 0.4 + :rand.uniform() * 0.5,
              hour_of_day: :rand.uniform(24) - 1,
              is_weekend: false,
              p99_latency_ms: 1_000 + :rand.uniform(5_000),
              burst_score: 0.7 + :rand.uniform() * 0.3
            }
        end

      {stats, label}
    end)
  end

  # Converts an integer class index to a one-hot list.
  # one_hot_encode(1, 3) => [0.0, 1.0, 0.0]
  defp one_hot_encode(class_idx, num_classes) do
    Enum.map(0..(num_classes - 1), fn i ->
      if i == class_idx, do: 1.0, else: 0.0
    end)
  end
end
```

### Step 5: Given tests — must pass without modification

**Objective**: Implement: Given tests — must pass without modification.

```elixir
# test/api_gateway/ml_classifier_test.exs
defmodule ApiGateway.ML.ClassifierTest do
  use ExUnit.Case, async: true

  alias ApiGateway.ML.{Classifier, FeatureExtractor, Training}

  describe "FeatureExtractor.extract/1" do
    test "returns tensor of shape {1, 8}" do
      stats = %{
        requests_per_minute: 100,
        payload_bytes: 1024,
        unique_endpoints: 5,
        error_rate: 0.01,
        hour_of_day: 14,
        is_weekend: false,
        p99_latency_ms: 200,
        burst_score: 0.1
      }
      tensor = FeatureExtractor.extract(stats)
      assert Nx.shape(tensor) == {1, 8}
      assert Nx.type(tensor) == {:f, 32}
    end

    test "all features are in [0, 1]" do
      stats = %{
        requests_per_minute: 100_000,
        payload_bytes: 999_999,
        unique_endpoints: 99_999,
        error_rate: 1.0,
        hour_of_day: 23,
        is_weekend: true,
        p99_latency_ms: 999_999,
        burst_score: 999_999
      }
      tensor = FeatureExtractor.extract(stats)
      values = Nx.to_flat_list(tensor)
      assert Enum.all?(values, fn v -> v >= 0.0 and v <= 1.0 end)
    end
  end

  describe "Classifier.build/0" do
    test "returns an Axon model" do
      model = Classifier.build()
      assert %Axon{} = model
    end
  end

  describe "Training.train/2" do
    test "trains and returns a model state that classifies all three classes" do
      data = Training.synthetic_data(300)
      {model, state} = Training.train(data, epochs: 3, batch_size: 32)

      # Verify the model can classify a normal request
      normal_stats = %{
        requests_per_minute: 20,
        payload_bytes: 256,
        unique_endpoints: 3,
        error_rate: 0.01,
        hour_of_day: 10,
        is_weekend: false,
        p99_latency_ms: 80,
        burst_score: 0.05
      }

      features = FeatureExtractor.extract(normal_stats)
      predictions = Axon.predict(model, state, %{"request_features" => features})
      assert Nx.shape(predictions) == {1, 3}

      # All three output probabilities must sum to ~1.0 (softmax)
      sum = predictions[0] |> Nx.sum() |> Nx.to_number()
      assert_in_delta sum, 1.0, 0.001
    end
  end
end
```

### Step 6: Run the tests

**Objective**: Verify the implementation by running the test suite.

```bash
mix test test/api_gateway/ml_classifier_test.exs --trace
```

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.


## Deep Dive: Neural Networks and Deep Learning Framework Patterns in Production

Axon is Elixir's deep learning library, built on top of Nx (Numerical Elixir). Production machine learning systems must handle retraining pipelines, model versioning, and inference latency budgets. The architecture decision—whether to embed models in the gateway or delegate to a sidecar service—affects failure isolation: a crashed model inference should not take down the entire gateway. Axon models are deterministic and pure, allowing you to test them offline before deployment. The trade-off is that model accuracy depends on training data quality; a model trained on stale attack patterns becomes a liability. Production ML systems require monitoring prediction confidence, not just accuracy, because low-confidence predictions often precede mode failures. Elixir's functional semantics and immutable tensors make it natural to build reproducible training pipelines that survive restarts.


## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Trade-off analysis

| Aspect | Axon neural network | Statistical z-score | Rule-based |
|--------|---------------------|---------------------|------------|
| Handles novel patterns | After retraining | No | No |
| Interpretability | Low (black box) | High | High |
| Training data required | Yes (labeled) | No | No |
| Inference latency | <1ms (dense, small) | <0.1ms | <0.01ms |
| False positives | Tunable via threshold | High on traffic spikes | Binary |

Reflection question: the `Dropout` layer in `Classifier.build/0` deactivates neurons randomly during training. What does `Axon.predict/4` do with the dropout layer by default during inference?

---

## Common production mistakes

**1. Using `Axon.Loop.run/4` return value incorrectly**
`Axon.Loop.run/4` returns the model state directly (not `{model, state}`). The model definition is separate from the state.

**2. Not seeding randomness in tests**
Synthetic training data with `:rand.uniform()` produces different results each run. Tests that check accuracy may pass or fail non-deterministically.

**3. Training in the request path**
`Axon.Loop.run/4` is synchronous and CPU/GPU intensive. Never call it in a request handler. Train in a background task.

**4. One-hot encoding mismatch**
`Axon.Loop.trainer/3` with `:categorical_cross_entropy` expects one-hot encoded labels `{batch, num_classes}`. Passing integer class indices produces wrong gradients silently.

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
defmodule Main do
  defp deps do
    [
      {:nx,   "~> 0.7"},
      {:axon, "~> 0.6"},
      {:exla, "~> 0.7", only: [:dev, :prod]}
    ]


  defmodule ApiGateway.ML.FeatureExtractor do
    @moduledoc """
    Converts raw request metadata into a normalized 8-element feature vector.

    Features (all normalized to [0, 1]):
      0: requests_per_minute / 1000  (rate, normalized)
      1: payload_bytes / 65536       (size, normalized)
      2: unique_endpoints / 100      (diversity)
      3: error_rate                  (fraction of 4xx/5xx)
      4: hour_of_day / 24            (time pattern)
      5: is_weekend (0.0 or 1.0)
      6: p99_latency_ms / 10_000     (normalized)
      7: burst_score                 (stddev of per-second counts, normalized)
    """

    @feature_count 8

    @doc "Extracts and returns an Nx tensor of shape {1, 8} from a request stats map."
    @spec extract(%{
      requests_per_minute: number(),
      payload_bytes: number(),
      unique_endpoints: number(),
      error_rate: float(),
      hour_of_day: integer(),
      is_weekend: boolean(),
      p99_latency_ms: number(),
      burst_score: number()
    }) :: Nx.Tensor.t()
    def extract(stats) do
      features = [
        min(stats.requests_per_minute / 1_000.0, 1.0),
        min(stats.payload_bytes / 65_536.0, 1.0),
        min(stats.unique_endpoints / 100.0, 1.0),
        stats.error_rate,
        stats.hour_of_day / 24.0,
        if(stats.is_weekend, do: 1.0, else: 0.0),
        min(stats.p99_latency_ms / 10_000.0, 1.0),
        min(stats.burst_score, 1.0)
      ]

      Nx.tensor([features], type: :f32)

    @spec feature_count() :: pos_integer()
    def feature_count, do: @feature_count


  defmodule ApiGateway.ML.Classifier do
    @moduledoc """
    Neural network classifier for request patterns.

    Architecture: Dense(16, relu) -> Dropout(0.3) -> Dense(8, relu) -> Dense(3, softmax)
    Output classes: 0=normal, 1=suspicious, 2=abusive
    """

    @classes [:normal, :suspicious, :abusive]

    @doc """
    Builds and returns the model architecture. Does not initialize weights.
    """
    @spec build() :: Axon.t()
    def build do
      Axon.input("request_features", shape: {nil, 8})
      |> Axon.dense(16, activation: :relu)
      |> Axon.dropout(rate: 0.3)
      |> Axon.dense(8, activation: :relu)
      |> Axon.dense(3, activation: :softmax)

    @spec classes() :: [:normal | :suspicious | :abusive]
    def classes, do: @classes
  end


  defmodule ApiGateway.ML.Training do
    @moduledoc """
    Training pipeline for the request classifier.

    The training data comes from labeled historical request logs.
    Labels: 0=normal, 1=suspicious, 2=abusive (one-hot encoded).
    """

    alias ApiGateway.ML.{Classifier, FeatureExtractor}

    @doc """
    Trains the classifier on labeled examples.

    `data` is a list of `{stats_map, label_integer}` tuples.
    `label_integer` is 0, 1, or 2.

    Returns `{model, model_state}`.
    """
    @spec train(list({map(), 0 | 1 | 2}), keyword()) :: {Axon.t(), map()}
    def train(data, opts \ []) do
      epochs        = Keyword.get(opts, :epochs, 20)
      learning_rate = Keyword.get(opts, :learning_rate, 0.001)
      batch_size    = Keyword.get(opts, :batch_size, 32)

      model = Classifier.build()

      # Convert data to batched tensors
      {features_list, labels_list} =
        data
        |> Enum.map(fn {stats, label} ->
          features = FeatureExtractor.extract(stats) |> Nx.squeeze(axes: [0])
          one_hot = Nx.tensor(one_hot_encode(label, 3), type: :f32)
          {features, one_hot}
        end)
        |> Enum.unzip()

      # Stack all features and labels into single tensors
      all_features = Nx.stack(features_list)
      all_labels = Nx.stack(labels_list)

      # Create batched stream for training
      batches =
        Stream.zip(
          Nx.to_batched(all_features, batch_size),
          Nx.to_batched(all_labels, batch_size)
        )
        |> Stream.map(fn {feat_batch, label_batch} ->
          %{"request_features" => feat_batch, "labels" => label_batch}
        end)

      # Build training loop
      model_state =
        model
        |> Axon.Loop.trainer(:categorical_cross_entropy, Polaris.Optimizers.adam(learning_rate: learning_rate))
        |> Axon.Loop.metric(:accuracy)
        |> Axon.Loop.run(batches, %{}, epochs: epochs, compiler: EXLA)

      {model, model_state}
    end

    @doc """
    Generates synthetic labeled training data for testing.
    In production this comes from labeled incident logs.
    """
    @spec synthetic_data(pos_integer()) :: list({map(), 0 | 1 | 2})
    def synthetic_data(n \ 1_000) do
      Enum.map(1..n, fn _ ->
        label = :rand.uniform(3) - 1

        stats =
          case label do
            0 ->
              %{
                requests_per_minute: 10 + :rand.uniform(50),
                payload_bytes: 100 + :rand.uniform(500),
                unique_endpoints: 1 + :rand.uniform(10),
                error_rate: :rand.uniform() * 0.05,
                hour_of_day: :rand.uniform(24) - 1,
                is_weekend: :rand.uniform(2) == 1,
                p99_latency_ms: 50 + :rand.uniform(100),
                burst_score: :rand.uniform() * 0.1
              }

            1 ->
              %{
                requests_per_minute: 200 + :rand.uniform(300),
                payload_bytes: 100 + :rand.uniform(2000),
                unique_endpoints: 5 + :rand.uniform(20),
                error_rate: 0.1 + :rand.uniform() * 0.2,
                hour_of_day: :rand.uniform(24) - 1,
                is_weekend: :rand.uniform(2) == 1,
                p99_latency_ms: 200 + :rand.uniform(500),
                burst_score: 0.3 + :rand.uniform() * 0.3
              }

            2 ->
              %{
                requests_per_minute: 800 + :rand.uniform(200),
                payload_bytes: 500 + :rand.uniform(10_000),
                unique_endpoints: 1 + :rand.uniform(3),
                error_rate: 0.4 + :rand.uniform() * 0.5,
                hour_of_day: :rand.uniform(24) - 1,
                is_weekend: false,
                p99_latency_ms: 1_000 + :rand.uniform(5_000),
                burst_score: 0.7 + :rand.uniform() * 0.3
              }
          end

        {stats, label}
      end)
    end

    # Converts an integer class index to a one-hot list.
    # one_hot_encode(1, 3) => [0.0, 1.0, 0.0]
    defp one_hot_encode(class_idx, num_classes) do
      Enum.map(0..(num_classes - 1), fn i ->
        if i == class_idx, do: 1.0, else: 0.0
      end)
    end
  end


  # test/api_gateway/ml_classifier_test.exs
  defmodule ApiGateway.ML.ClassifierTest do
    use ExUnit.Case, async: true

    alias ApiGateway.ML.{Classifier, FeatureExtractor, Training}

      test "all features are in [0, 1]" do
        stats = %{
          requests_per_minute: 100_000,
          payload_bytes: 999_999,
          unique_endpoints: 99_999,
          error_rate: 1.0,
          hour_of_day: 23,
          is_weekend: true,
          p99_latency_ms: 999_999,
          burst_score: 999_999
        }
        tensor = FeatureExtractor.extract(stats)
        values = Nx.to_flat_list(tensor)
        assert Enum.all?(values, fn v -> v >= 0.0 and v <= 1.0 end)
      end
    end
    end
    end
  end

  defmodule Main do
    def main do
        :ok
    end
  end
end

Main.main()
```
