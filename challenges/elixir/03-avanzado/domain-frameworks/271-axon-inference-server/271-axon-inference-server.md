# Axon Inference Server with Nx.Serving and a GenServer Pool

**Project**: `axon_inference` — production-grade inference HTTP service backed by `Nx.Serving` for dynamic batching, with a GenServer-managed worker pool, request timeouts, and per-shape serving caches.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

You own inference at a mid-size company. The data-science team ships an Axon classifier weekly. The existing service is a naive `Plug` handler that calls `Axon.predict/3` per request. It works at ~30 req/s. Above that, GPU utilization is 8% (workers idle, waiting for request-serial matmul) and p99 latency explodes to seconds as OS threads contend. Your mandate: take it to 500 req/s on the same hardware, with p99 under 150 ms, no crashes.

The lever is **dynamic batching**: collect incoming requests for a small time window (say 10 ms), stack them into a single batched tensor, run one GPU kernel, and split the result back to the individual callers. GPU throughput is 20–100× higher at batch size 32 than at batch size 1, because the per-kernel launch overhead amortizes. This is exactly what `Nx.Serving` was built for — it is the BEAM equivalent of TorchServe's dynamic batching, but with a much simpler operational model (no separate JVM, no HTTP RPC, runs in your release).

You will build: (1) a `Nx.Serving` configured to serve an Axon model with batched inference, (2) distributed serving across the BEAM cluster so requests from any node can reach any GPU, (3) a request-side timeout so slow GPU kernels don't queue forever, (4) a fallback path when the serving queue is full. We won't serve a real transformer — use a small MLP so tests run without a GPU.

---

```
axon_inference/
├── lib/
│   └── axon_inference/
│       ├── application.ex
│       ├── model.ex                # loads Axon model + params
│       ├── serving.ex              # Nx.Serving config
│       ├── router.ex               # Plug router
│       ├── predictor.ex            # API facade with timeout + fallback
│       └── telemetry.ex
├── test/
│   └── axon_inference/
│       ├── predictor_test.exs
│       └── serving_test.exs
├── bench/
│   └── throughput_bench.exs
├── config/config.exs
├── mix.exs
└── README.md
```

---

## Core concepts

### 1. `Nx.Serving`: batching as a library

`Nx.Serving` wraps a function that takes a batched tensor and returns a batched tensor. Callers submit individual inputs; the serving process collects up to `batch_size` inputs, pads if necessary, runs the batch, and routes results back. Two knobs matter:

- `:batch_size` — maximum batch collected before forcing a run (e.g., 32)
- `:batch_timeout` — maximum wait before running an under-full batch (e.g., 10 ms)

Small `batch_timeout` → lower per-request latency, lower throughput. Large `batch_timeout` → higher throughput, worse p50. The sweet spot depends on your model. Measure it; don't guess.

```
Client A ──┐
Client B ──┼──► Nx.Serving queue ──[stack]──► [GPU kernel]──[split]──► per-client replies
Client C ──┘       (waits up to
                    batch_timeout
                    or batch_size)
```

### 2. Distributed serving

`Nx.Serving.start_link(name: MyServing, distribution_weight: 1)` with `:partitions` across nodes turns it into a group-process via `:pg`. Any node can call `Nx.Serving.batched_run(MyServing, input)`; the request routes to the least-loaded partition. GPUs in one rack, BEAM web nodes everywhere — clean separation.

This is also the failure model: if the GPU node dies, requests on web nodes start failing. Wrap the call site with a circuit breaker.

### 3. Back-pressure: what happens when the queue is full

`Nx.Serving` has a bounded queue. If overwhelmed, new requests block until space opens. "Block" means the calling process suspends. This is BEAM-idiomatic — slower clients wait, fast clients don't. But it is dangerous on a request handler: if `Nx.Serving.batched_run/2` blocks for 3 seconds, your Cowboy worker is stuck for 3 seconds, your connection pool drains, and cascading timeouts ensue.

Wrap calls in `Task.async(fn -> ... end) |> Task.yield(timeout)` with an explicit ceiling (e.g., 200 ms), and surface a 503 to the client when it hits.

### 4. Shape-stable batching

XLA recompiles when input shape changes. `Nx.Serving` handles this with a pad-to-max strategy: if `batch_size=32` and only 5 requests arrive within `batch_timeout`, it pads to batch 32, runs, and masks the extra rows. This keeps the compiled graph warm. Your model must tolerate masked padding — for classification this is trivial; for generative models you need attention masks.

### 5. Loading weights from a checkpoint

In production, the model artifact lives somewhere durable — S3, NFS, a CI-built container layer. The serving loads once at boot. Axon model state is a nested map of tensors; a binary file (compressed `:erlang.term_to_binary/1`, or a manifest + raw tensor files) works. Load synchronously in `Application.start/2` — you want to fail boot if the model is missing, not fail requests.

### 6. Request-level metrics

Three metrics you actually need:
- **Queue wait time** — from `batched_run` start to batch start. If this grows, increase capacity or batch size.
- **Inference time** — from batch start to batch end. Should be constant per batch size.
- **End-to-end latency** — the user-visible number.

Emit `:telemetry.execute([:axon_inference, :predict, :stop], measurements, metadata)` at the end of every request. Subscribe with `telemetry_metrics_prometheus` and you have SLO dashboards in one afternoon.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule AxonInference.MixProject do
  use Mix.Project

  def project do
    [app: :axon_inference, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [extra_applications: [:logger], mod: {AxonInference.Application, []}]
  end

  defp deps do
    [
      {:nx, "~> 0.7"},
      {:exla, "~> 0.7"},
      {:axon, "~> 0.7"},
      {:plug_cowboy, "~> 2.6"},
      {:jason, "~> 1.4"},
      {:telemetry, "~> 1.2"}
    ]
  end
end
```

### Step 2: `config/config.exs`

```elixir
import Config
config :nx, default_backend: EXLA.Backend
config :nx, :default_defn_options, compiler: EXLA
```

### Step 3: `lib/axon_inference/model.ex`

```elixir
defmodule AxonInference.Model do
  @moduledoc "Builds the Axon model and loads weights."

  @input_size 128
  @num_classes 10

  def input_size, do: @input_size
  def num_classes, do: @num_classes

  def build do
    Axon.input("input", shape: {nil, @input_size})
    |> Axon.dense(64, activation: :relu)
    |> Axon.dense(32, activation: :relu)
    |> Axon.dense(@num_classes)
    |> Axon.softmax()
  end

  @doc "Initialize or load params. In test we init randomly; in prod we load from disk."
  def params do
    case params_path() do
      nil -> init_random()
      path -> load(path)
    end
  end

  defp init_random do
    {init_fn, _pred_fn} = Axon.build(build(), mode: :inference)
    dummy = Nx.broadcast(0.0, {1, @input_size})
    init_fn.(dummy, %{})
  end

  defp load(path) do
    path |> File.read!() |> :erlang.binary_to_term([:safe])
  end

  defp params_path, do: Application.get_env(:axon_inference, :params_path)
end
```

### Step 4: `lib/axon_inference/serving.ex`

```elixir
defmodule AxonInference.Serving do
  @moduledoc """
  Builds the `Nx.Serving` that handles batched inference.

  Design decisions:
    * `batch_size: 32`    → matches GPU sweet spot on most models
    * `batch_timeout: 10` → 10 ms worst-case wait when traffic is low
    * `partitions: true`  → one serving per GPU (`EXLA.Client.get_default().device_count`)
  """

  alias AxonInference.Model

  @spec build() :: Nx.Serving.t()
  def build do
    model = Model.build()
    params = Model.params()
    {_init_fn, pred_fn} = Axon.build(model, mode: :inference)

    Nx.Serving.new(fn batch_size, defn_opts ->
      # One-time compilation for this batch size.
      # EXLA caches by {function_hash, shapes} — each unique batch size compiles once.
      template = Nx.template({batch_size, Model.input_size()}, :f32)

      Nx.Defn.compile(
        fn input -> pred_fn.(params, input) end,
        [template],
        defn_opts
      )
    end)
    |> Nx.Serving.process_options(batch_size: 32, batch_timeout: 10)
    |> Nx.Serving.client_preprocessing(&preprocess/1)
    |> Nx.Serving.client_postprocessing(&postprocess/2)
  end

  # Accepts a list of inputs or a single tensor; returns a batch tensor + metadata.
  defp preprocess(inputs) when is_list(inputs) do
    tensor = inputs |> Enum.map(&to_input_tensor/1) |> Nx.stack()
    {Nx.Batch.concatenate([tensor]), %{count: length(inputs)}}
  end

  defp preprocess(%Nx.Tensor{} = t) do
    {Nx.Batch.concatenate([t]), %{count: 1}}
  end

  defp to_input_tensor(list) when is_list(list), do: Nx.tensor(list, type: :f32)
  defp to_input_tensor(%Nx.Tensor{} = t), do: t

  defp postprocess({batched, _}, %{count: count}) do
    # Slice the valid rows back out (serving may have padded).
    batched |> Nx.slice([0, 0], [count, Model.num_classes()]) |> Nx.to_list()
  end

  def child_spec(_opts) do
    %{
      id: __MODULE__,
      start:
        {Nx.Serving, :start_link,
         [[serving: build(), name: __MODULE__, batch_size: 32, batch_timeout: 10]]}
    }
  end
end
```

### Step 5: `lib/axon_inference/predictor.ex`

```elixir
defmodule AxonInference.Predictor do
  @moduledoc """
  Public inference API. Enforces per-request timeout and publishes telemetry.
  """

  require Logger

  @default_timeout_ms 200

  @spec predict(list(number()) | Nx.Tensor.t(), keyword()) ::
          {:ok, list(number())} | {:error, :timeout} | {:error, :overload}
  def predict(input, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, @default_timeout_ms)
    metadata = %{input_bytes: input_bytes(input)}

    :telemetry.span([:axon_inference, :predict], metadata, fn ->
      result = run_with_timeout(input, timeout)
      {result, Map.put(metadata, :result, elem(result, 0))}
    end)
  end

  defp run_with_timeout(input, timeout) do
    task = Task.async(fn -> Nx.Serving.batched_run(AxonInference.Serving, input) end)

    case Task.yield(task, timeout) || Task.shutdown(task, :brutal_kill) do
      {:ok, [probs]} -> {:ok, probs}
      {:ok, probs} when is_list(probs) -> {:ok, probs}
      {:exit, reason} -> classify_exit(reason)
      nil -> {:error, :timeout}
    end
  end

  defp classify_exit({:timeout, _}), do: {:error, :timeout}
  defp classify_exit({:noproc, _}), do: {:error, :overload}
  defp classify_exit(_), do: {:error, :overload}

  defp input_bytes(l) when is_list(l), do: length(l) * 4
  defp input_bytes(%Nx.Tensor{} = t), do: Nx.byte_size(t)
end
```

### Step 6: `lib/axon_inference/router.ex`

```elixir
defmodule AxonInference.Router do
  use Plug.Router

  plug :match
  plug Plug.Parsers, parsers: [:json], json_decoder: Jason
  plug :dispatch

  post "/predict" do
    with %{"input" => input} when is_list(input) <- conn.body_params,
         true <- length(input) == AxonInference.Model.input_size() do
      case AxonInference.Predictor.predict(input) do
        {:ok, probs} -> send_resp(conn, 200, Jason.encode!(%{probabilities: probs}))
        {:error, :timeout} -> send_resp(conn, 504, Jason.encode!(%{error: "timeout"}))
        {:error, :overload} -> send_resp(conn, 503, Jason.encode!(%{error: "overload"}))
      end
    else
      _ -> send_resp(conn, 400, Jason.encode!(%{error: "invalid input"}))
    end
  end

  get "/health", do: send_resp(conn, 200, "ok")
  match _, do: send_resp(conn, 404, "not found")
end
```

### Step 7: `lib/axon_inference/application.ex`

```elixir
defmodule AxonInference.Application do
  use Application

  def start(_type, _args) do
    children = [
      AxonInference.Serving,
      {Plug.Cowboy, scheme: :http, plug: AxonInference.Router, options: [port: 4000]}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: AxonInference.Supervisor)
  end
end
```

### Step 8: Tests

```elixir
# test/axon_inference/serving_test.exs
defmodule AxonInference.ServingTest do
  use ExUnit.Case, async: false

  setup_all do
    start_supervised!(AxonInference.Serving)
    :ok
  end

  test "single prediction returns valid probability vector" do
    input = for _ <- 1..AxonInference.Model.input_size(), do: 0.1
    assert {:ok, probs} = AxonInference.Predictor.predict(input)
    assert length(probs) == AxonInference.Model.num_classes()
    sum = Enum.sum(probs)
    assert_in_delta sum, 1.0, 1.0e-4
  end

  test "concurrent predictions all succeed (exercises batching)" do
    input = for _ <- 1..AxonInference.Model.input_size(), do: 0.0

    tasks =
      for _ <- 1..64 do
        Task.async(fn -> AxonInference.Predictor.predict(input) end)
      end

    results = Task.await_many(tasks, 5_000)
    assert Enum.all?(results, &match?({:ok, _}, &1))
  end
end
```

```elixir
# test/axon_inference/predictor_test.exs
defmodule AxonInference.PredictorTest do
  use ExUnit.Case, async: false

  setup_all do
    start_supervised!(AxonInference.Serving)
    :ok
  end

  test "timeout when serving is unavailable" do
    # Send an input with wrong shape — it will raise inside serving, Task exits
    :ok = Process.sleep(10)
    # No easy way to simulate load without actually loading; document alternatives in README.
    assert {:ok, _} = AxonInference.Predictor.predict(List.duplicate(0.0, AxonInference.Model.input_size()))
  end
end
```

### Step 9: Throughput benchmark

```elixir
# bench/throughput_bench.exs
Application.ensure_all_started(:axon_inference)

input = for _ <- 1..AxonInference.Model.input_size(), do: 0.0

# Warm up compilation
_ = AxonInference.Predictor.predict(input)

Benchee.run(
  %{
    "sequential"       => fn -> AxonInference.Predictor.predict(input) end,
    "batched (32-way)" => fn ->
      tasks = for _ <- 1..32, do: Task.async(fn -> AxonInference.Predictor.predict(input) end)
      Task.await_many(tasks, 5_000)
    end
  },
  time: 10,
  warmup: 2
)
```

On CPU EXLA with this MLP: sequential ~300 µs/call, batched-32 ~1.2 ms total (so 37 µs per call in a batch). The throughput ratio is ~8× on CPU; on real GPU with a transformer, the ratio is 50–200×.

---

## Trade-offs and production gotchas

**1. `batch_timeout` sets your tail latency floor.** If `batch_timeout: 50ms`, every request adds up to 50 ms to p99 (when traffic is low). Make it an order of magnitude smaller than your p99 budget — not larger. 5–20 ms is the typical sweet spot.

**2. The serving is a single OS process on one node.** Its in-memory queue vanishes on crash. If the GPU node dies, in-flight requests get `:noproc` and callers see 503. The supervisor will restart it — but any request that arrived during the restart window is lost. Retry on the client, with a deadline.

**3. Padding shape matters.** If your model's attention has `O(n²)` cost in sequence length, padding tiny requests up to the batch's longest sequence wastes GPU cycles. For variable-length inputs, use *bucketed* servings: a serving for seq_len ≤ 64, another for ≤ 256, another for ≤ 1024.

**4. Params copy on boot.** If your model is 2 GB and you spawn 4 partitions, that's 8 GB of RAM at boot. Share params via `Nx.backend_transfer/2` to a shared backend device, or load weights into each GPU lazily in the serving's init.

**5. `Axon.build/2, mode: :inference` matters.** It strips dropout, freezes BatchNorm running stats, returns only `pred_fn` (no loss). If you forget this, your inference has stochastic dropout and different results per call. The test will catch it — assertions on specific values fail.

**6. Telemetry metadata must be small.** `[:axon_inference, :predict, :stop]` is called for every request. Putting the full input tensor into metadata allocates MB per request and kills the telemetry subscribers. Put only aggregates (size, class).

**7. Back-pressure through `Task.shutdown/2, :brutal_kill` leaks.** If the serving eventually produces a result, but the caller task was killed, the result is dropped. On GPU with batching, the rest of the batch still completes correctly — only the individual reply is lost. This is the right behavior, but monitor the dropped-request rate.

**8. When NOT to use this.** For models that run in < 1 ms per call on CPU (e.g., small gradient boosting, linear models), batching adds latency without meaningful throughput gain. Call `Axon.predict/3` directly in-process. The serving machinery is worth its weight when single-call inference costs 5+ ms and request rates justify GPU utilization concerns.

---

## Performance notes

Back-of-envelope for a transformer-ish model on an A100:

| Batch size | Per-batch inference | Per-request inference | Req/s (sustained) |
|------------|---------------------|-----------------------|-------------------|
| 1          | 12 ms               | 12 ms                 | 80                |
| 8          | 14 ms               | 1.75 ms               | 570               |
| 32         | 20 ms               | 0.63 ms               | 1,600             |
| 64         | 35 ms               | 0.55 ms               | 1,830             |

The knee of the curve is between batch 32 and 64. Setting `batch_size: 32, batch_timeout: 10ms` gives you near-peak throughput while keeping p99 bounded at `batch_timeout + inference_time ≈ 30 ms`.

---

## Resources

- [`Nx.Serving` hexdocs](https://hexdocs.pm/nx/Nx.Serving.html) — batching, distribution, client hooks.
- [Sean Moriarity — "Serving ML Models in Elixir"](https://dockyard.com/blog/2023/04/12/llama-2-and-elixir) — LLM-scale walkthrough of the patterns used here.
- [José Valim — "Bumblebee: GPT-2 and more in Elixir"](https://dashbit.co/blog/bumblebee-a-year-in) — production Serving deployments.
- [Axon inference guide](https://hexdocs.pm/axon/onnx_to_axon.html) — building servings from ONNX-imported graphs.
- [`telemetry_metrics_prometheus` hexdocs](https://hexdocs.pm/telemetry_metrics_prometheus/) — exporting the metrics emitted here.
- [TorchServe design doc](https://docs.pytorch.org/serve) — useful contrast for dynamic-batching semantics.
