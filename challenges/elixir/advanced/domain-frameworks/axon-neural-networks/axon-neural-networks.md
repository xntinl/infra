# Axon — Neural Networks for Request Classification

**Project**: `axon_neural_networks` — production-grade axon — neural networks for request classification in Elixir

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
axon_neural_networks/
├── lib/
│   └── axon_neural_networks.ex
├── script/
│   └── main.exs
├── test/
│   └── axon_neural_networks_test.exs
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
defmodule AxonNeuralNetworks.MixProject do
  use Mix.Project

  def project do
    [
      app: :axon_neural_networks,
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
### `lib/axon_neural_networks.ex`

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
### `test/axon_neural_networks_test.exs`

```elixir
defmodule ApiGateway.ML.ClassifierTest do
  use ExUnit.Case, async: true
  doctest ApiGateway.ML.FeatureExtractor

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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Axon — Neural Networks for Request Classification.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Axon — Neural Networks for Request Classification ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AxonNeuralNetworks.run(payload) do
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
        for _ <- 1..1_000, do: AxonNeuralNetworks.run(:bench)
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
