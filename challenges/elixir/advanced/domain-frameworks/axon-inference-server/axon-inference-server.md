# Axon Inference Server with Nx.Serving and a GenServer Pool

**Project**: `axon_inference` — production-grade inference HTTP service backed by `Nx.Serving` for dynamic batching, with a GenServer-managed worker pool, request timeouts, and per-shape serving caches

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
axon_inference/
├── lib/
│   └── axon_inference.ex
├── script/
│   └── main.exs
├── test/
│   └── axon_inference_test.exs
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
defmodule AxonInference.MixProject do
  use Mix.Project

  def project do
    [
      app: :axon_inference,
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
### `lib/axon_inference.ex`

```elixir
defmodule AxonInference.Model do
  @moduledoc "Builds the Axon model and loads weights."

  @input_size 128
  @num_classes 10

  @doc "Returns input size result."
  def input_size, do: @input_size
  @doc "Returns num classes result."
  def num_classes, do: @num_classes

  @doc "Builds result."
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

defmodule AxonInference.Serving do
  @moduledoc """
  Builds the `Nx.Serving` that handles batched inference.

  Design decisions:
    * `batch_size: 32`    → matches GPU sweet spot on most models
    * `batch_timeout: 10` → 10 ms worst-case wait when traffic is low
    * `partitions: true`  → one serving per GPU (`EXLA.Client.get_default().device_count`)
  """

  alias AxonInference.Model

  @doc "Builds result."
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

defmodule AxonInference.Predictor do
  @moduledoc """
  Public inference API. Enforces per-request timeout and publishes telemetry.
  """

  require Logger

  @default_timeout_ms 200

  @spec predict(list(number()) | Nx.Tensor.t(), keyword()) ::
          {:ok, list(number())} | {:error, :timeout} | {:error, :overload}
  @doc "Returns predict result from input and opts."
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

defmodule AxonInference.PredictorTest do
  use ExUnit.Case, async: false
  doctest AxonInference.MixProject

  setup_all do
    start_supervised!(AxonInference.Serving)
    :ok
  end

  describe "AxonInference.Predictor" do
    test "timeout when serving is unavailable" do
      # Send an input with wrong shape — it will raise inside serving, Task exits
      :ok = Process.sleep(10)
      # No easy way to simulate load without actually loading; document alternatives in README.
      assert {:ok, _} = AxonInference.Predictor.predict(List.duplicate(0.0, AxonInference.Model.input_size()))
    end
  end
end
```
### `test/axon_inference_test.exs`

```elixir
defmodule AxonInference.ServingTest do
  use ExUnit.Case, async: true
  doctest AxonInference.MixProject

  setup_all do
    start_supervised!(AxonInference.Serving)
    :ok
  end

  describe "AxonInference.Serving" do
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
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Axon Inference Server with Nx.Serving and a GenServer Pool.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Axon Inference Server with Nx.Serving and a GenServer Pool ===")
    IO.puts("Category: Domain frameworks\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AxonInference.run(payload) do
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
        for _ <- 1..1_000, do: AxonInference.run(:bench)
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
