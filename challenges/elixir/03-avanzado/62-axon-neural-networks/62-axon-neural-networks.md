# Axon — Neural Networks for Request Classification

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The metrics analyzer (previous exercise) can detect
anomalies statistically. Now the security team wants something smarter: classify
incoming requests as `normal`, `suspicious`, or `abusive` based on a feature vector
extracted from each request (rate, payload size, endpoint pattern, time-of-day).

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── ml/
│       │   ├── feature_extractor.ex  # ← you implement this
│       │   ├── classifier.ex         # ← and this
│       │   └── training.ex           # ← and this
├── test/
│   └── api_gateway/
│       └── ml_classifier_test.exs    # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

Statistical anomaly detection (z-score) triggers on any deviation from the mean —
including legitimate traffic spikes during business hours. The security team wants a
model trained on labeled examples of past attacks to distinguish malicious patterns
from legitimate load. The model must be:

1. Small enough to run inference in < 1ms per request
2. Retrained periodically as new attack patterns emerge
3. Serializable — the trained model persists across gateway restarts

This is a classification problem with a fixed 8-feature input and 3-class output.
A two-layer dense network is sufficient — no need for transformers or CNNs.

---

## Why Axon over raw Nx

Raw Nx gives you tensors and `defn`. Building a neural network with raw Nx means:
implementing weight initialization, forward pass, loss functions, optimizer state,
gradient computation, training loop, and serialization manually. Axon provides all
of these. The difference is not convenience — it is correctness. Weight initialization
strategies (Glorot, He) and optimizers (Adam momentum terms) have subtle numerical
properties that are easy to get wrong from scratch.

Axon's `%Axon{}` struct is a description of the computation graph — not a running
process. `Axon.build/2` compiles it to init/predict functions. `Axon.Loop.trainer/3`
constructs the training loop. This separation makes the model definition readable and
the training logic independent.

---

## How `Axon.freeze/1` works

`Axon.freeze/1` marks layers so that their parameters are excluded from gradient
computation. Mathematically, it inserts a `stop_gradient` operation at the boundary —
gradients flow forward (for inference) but not backward through frozen layers (for
training). This is equivalent to TensorFlow's `trainable=False` or PyTorch's
`requires_grad=False`.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    # existing deps...
    {:nx,   "~> 0.7"},
    {:axon, "~> 0.6"},
    {:exla, "~> 0.7", only: [:dev, :prod]}
  ]
end
```

### Step 2: `lib/api_gateway/ml/feature_extractor.ex`

```elixir
defmodule ApiGateway.ML.FeatureExtractor do
  @moduledoc """
  Converts raw request metadata into a normalized 8-element feature vector.

  Features (all normalized to [0, 1] or z-scored):
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

  def feature_count, do: @feature_count
end
```

### Step 3: `lib/api_gateway/ml/classifier.ex`

```elixir
defmodule ApiGateway.ML.Classifier do
  @moduledoc """
  Neural network classifier for request patterns.

  Architecture: Dense(16, relu) → Dropout(0.3) → Dense(8, relu) → Dense(3, softmax)
  Output classes: 0=normal, 1=suspicious, 2=abusive
  """

  alias ApiGateway.ML.FeatureExtractor

  @model_path "priv/ml/classifier.axon"
  @classes [:normal, :suspicious, :abusive]

  @doc """
  Builds and returns the model architecture. Does not initialize weights.
  """
  def build do
    # TODO: build the network with Axon
    # Input: shape {nil, 8} (batch x features), name "request_features"
    # Dense(16) with relu activation
    # Dropout(rate: 0.3)
    # Dense(8) with relu activation
    # Dense(3) with softmax activation (3 output classes)
    # Return the Axon model
  end

  @doc """
  Classifies a request stats map. Returns `{class_atom, confidence_float}`.
  Loads the saved model state from disk. Raises if model not trained yet.
  """
  def classify(stats) do
    model = build()
    model_state = load_state!()
    features = FeatureExtractor.extract(stats)

    predictions = Axon.predict(model, model_state, %{"request_features" => features})
    # predictions shape: {1, 3}
    class_idx = predictions[0] |> Nx.argmax() |> Nx.to_number()
    confidence = predictions[0][class_idx] |> Nx.to_number()

    {Enum.at(@classes, class_idx), confidence}
  end

  @doc """
  Saves the model state to disk.
  """
  def save_state(model, state) do
    File.mkdir_p!(Path.dirname(@model_path))
    serialized = Axon.serialize(model, state)
    File.write!(@model_path, serialized)
    :ok
  end

  @doc """
  Loads the model state from disk. Returns `{model, state}`.
  """
  def load_state! do
    @model_path
    |> File.read!()
    |> Axon.deserialize()
    |> elem(1)
  end

  def classes, do: @classes
end
```

### Step 4: `lib/api_gateway/ml/training.ex`

```elixir
defmodule ApiGateway.ML.Training do
  @moduledoc """
  Training pipeline for the request classifier.

  The training data comes from labeled historical request logs.
  Labels: 0=normal, 1=suspicious, 2=abusive (one-hot encoded).
  """

  alias ApiGateway.ML.{Classifier, FeatureExtractor}

  @doc """
  Trains the classifier on labeled examples and saves the model.

  `data` is a list of `{stats_map, label_integer}` tuples.
  `label_integer` is 0, 1, or 2.

  Returns `{model, model_state}`.
  """
  def train(data, opts \\ []) do
    epochs        = Keyword.get(opts, :epochs, 20)
    learning_rate = Keyword.get(opts, :learning_rate, 0.001)
    batch_size    = Keyword.get(opts, :batch_size, 32)

    model = Classifier.build()

    # TODO: convert data list to batches:
    #   - For each {stats_map, label} pair: extract features tensor, one-hot encode label
    #   - Group into batches of batch_size
    #   - Each batch is %{"request_features" => {batch, 8} tensor, "labels" => {batch, 3} tensor}

    # TODO: build a training loop with Axon.Loop.trainer/3:
    #   - loss: :categorical_cross_entropy
    #   - optimizer: Axon.Optimizers.adam(learning_rate)
    # TODO: add :accuracy metric with Axon.Loop.metric/3
    # TODO: run with Axon.Loop.run/4, epochs: epochs
    # TODO: save model with Classifier.save_state/2
    # TODO: return {model, model_state}
  end

  @doc """
  Generates synthetic labeled training data for testing.

  In production this comes from labeled incident logs.
  """
  def synthetic_data(n \\ 1_000) do
    Enum.map(1..n, fn _ ->
      label = :rand.uniform(3) - 1

      stats =
        case label do
          0 ->
            # Normal traffic profile
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
            # Suspicious — elevated rate, some errors
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
            # Abusive — very high rate, high errors, tight burst
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
end
```

### Step 5: Given tests — must pass without modification

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

```bash
mix test test/api_gateway/ml_classifier_test.exs --trace
```

---

## Trade-off analysis

| Aspect | Axon neural network | Statistical z-score | Rule-based |
|--------|---------------------|---------------------|------------|
| Handles novel patterns | After retraining | No | No |
| Interpretability | Low (black box) | High | High |
| Training data required | Yes (labeled) | No | No |
| Inference latency | <1ms (dense, small) | <0.1ms | <0.01ms |
| False positives | Tunable via threshold | High on traffic spikes | Binary |
| Maintenance | Retrain on new attacks | Adjust threshold | Update rules |

Reflection question: the `Dropout` layer in `Classifier.build/0` deactivates neurons
randomly during training. What does `Axon.predict/4` do with the dropout layer by default
during inference? What argument would change this behavior?

---

## Common production mistakes

**1. Using `Axon.Loop.run/4` return value incorrectly**
`Axon.Loop.run/4` returns the model state directly (not `{model, state}`). The model
definition is separate from the state. You need both to call `Axon.predict/4`.

**2. Not seeding randomness in tests**
Synthetic training data with `:rand.uniform()` produces different results each run.
Tests that check accuracy after training may pass or fail non-deterministically.
Use `Nx.Random` with an explicit key in test fixtures.

**3. Saving state without the model**
`Axon.serialize/2` takes both model and state. `Axon.deserialize/1` returns `{model, state}`.
If you serialize only the state and lose the model definition, you cannot deserialize.

**4. Training in the request path**
`Axon.Loop.run/4` is synchronous and CPU/GPU intensive. Never call it in a request
handler. Train in a background task or scheduled job, then swap the loaded model state
atomically (e.g., via an Agent).

**5. One-hot encoding mismatch**
`Axon.Loop.trainer/3` with `:categorical_cross_entropy` expects one-hot encoded labels
`{batch, num_classes}`. Passing integer class indices `{batch}` produces wrong gradients
silently — the loss decreases but the model does not learn.

---

## Resources

- [Axon HexDocs](https://hexdocs.pm/axon/Axon.html) — layers, build, predict
- [Axon.Loop](https://hexdocs.pm/axon/Axon.Loop.html) — trainer, metric, run
- [Bumblebee](https://hexdocs.pm/bumblebee) — pre-trained Hugging Face models in Elixir
- [Machine Learning in Elixir — Sean Moriarity](https://pragprog.com/titles/smelixir/machine-learning-in-elixir/)
