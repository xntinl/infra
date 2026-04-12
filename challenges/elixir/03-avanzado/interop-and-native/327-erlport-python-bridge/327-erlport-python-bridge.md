# BEAM ↔ Python via ErlPort

**Project**: `ml_inference` — serve predictions from a scikit-learn model via a Python worker pool bridged to Elixir over the ErlPort term protocol.

## Project context

The data science team maintains a trained scikit-learn model that must be served online.
Rewriting the inference path in Elixir or Rust is not feasible (the team iterates weekly),
and calling a Python HTTP service adds 2-5ms of network overhead per request. ErlPort is
the middle ground: it keeps a Python interpreter alive as a port, exchanges Erlang terms
(via the external term format) with the BEAM, and lets Elixir call Python functions as if
they were local.

ErlPort's advantage over naive `System.cmd "python inference.py"`: no per-call Python
interpreter startup (~300ms), the model stays loaded in RAM, and terms are auto-converted
(Elixir lists ↔ Python lists, maps ↔ dicts, atoms ↔ atoms-as-strings).

```
ml_inference/
├── lib/
│   └── ml_inference/
│       ├── application.ex
│       ├── worker.ex
│       └── pool.ex
├── priv/
│   └── python/
│       ├── __init__.py
│       └── predictor.py
├── test/ml_inference/pool_test.exs
└── mix.exs
```

## Why ErlPort and not gRPC/HTTP

| Concern | ErlPort | gRPC to Python service |
|---|---|---|
| Per-call overhead | ~100µs (local port) | ~1-5ms (TCP) |
| Serialization | term format (zero schema) | protobuf (needs .proto) |
| Failure isolation | per-port (kills that interpreter) | per-process/container |
| Operational surface | one Elixir app | two services + network |
| Cross-language types | automatic | manual mapping |

For inline model serving inside an existing Elixir app, ErlPort wins on simplicity. For
a model served to many tenants across a K8s cluster, gRPC is the right tool.

## Why a pool and not a single persistent worker

A single Python interpreter is single-threaded (GIL). For concurrent inference requests
you need N Python workers. Wrap them in a pool (`poolboy` or a hand-rolled round-robin
GenServer) and every request checks out one worker for the duration of a call.

## Core concepts

### 1. Term auto-conversion

ErlPort maps:
- Elixir `:atom` ↔ Python `erlport.Atom(b"atom")`
- Elixir list ↔ Python list
- Elixir map ↔ Python dict
- Elixir tuple ↔ Python tuple
- Elixir binary ↔ Python `bytes`

Keep Elixir atoms out of inference payloads — they become `erlport.Atom` objects in
Python, which surprises code expecting strings.

### 2. The port message loop

ErlPort's Python side runs a loop that reads terms from stdin, dispatches to the named
function, and writes the response. Blocking in Python blocks that port's whole loop —
always return quickly or spawn Python threads explicitly.

### 3. `cast` vs `call`

`:python.call/4` is synchronous. `:python.cast/4` is fire-and-forget. Always use `call`
for inference (you need the result); reserve `cast` for side effects like writing to
Python-side logs.

### 4. Process ownership

Each worker is one Python interpreter tied to an Elixir GenServer. If the GenServer dies,
the supervisor restarts it and spawns a fresh interpreter (which reloads the model —
non-trivial startup cost). Keep workers stable; make their code idempotent.

## Design decisions

- **Option A — one Python worker GenServer, serialized**: simple, low throughput.
- **Option B — pool of N workers**: matches concurrency to CPU; needs pool library.
- **Option C — embed Python inside BEAM via PyO3 NIF**: lowest latency, massive complexity.

→ **Option B**. The pool size should match the number of CPU cores allocated to inference.

- **Option A — each call ships the feature vector as a list**: simple, some overhead.
- **Option B — use a binary representation (numpy's tobytes)**: minimal overhead but
  extra encoding step both sides.

→ **Option A**. Move to B only when you can prove the list encode cost dominates.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule MlInference.MixProject do
  use Mix.Project

  def project do
    [
      app: :ml_inference,
      version: "0.1.0",
      elixir: "~> 1.17",
      deps: [
        {:erlport, "~> 0.11"}
      ]
    ]
  end

  def application,
    do: [extra_applications: [:logger], mod: {MlInference.Application, []}]
end
```

Python dependencies are managed with `pip` inside a virtualenv that ErlPort uses. The
`priv/python/requirements.txt` would list `scikit-learn==1.5.1` (pinned). In this exercise
we stub the model so the example runs without installing scikit-learn.

### Step 1: Python side (`priv/python/predictor.py`)

```python
"""
Inference module called from Elixir via erlport.
All public functions take Erlang-encoded terms and return the same.

The model is loaded once at import time (module scope). Subsequent
predict() calls are in-process.
"""

import math

# In production: model = joblib.load("model.pkl")
# For exercise portability, we use a stub.
class _StubModel:
    """Trivial stand-in: outputs sigmoid(sum(features))."""
    def predict_proba(self, features):
        s = sum(features)
        p = 1.0 / (1.0 + math.exp(-s))
        return [1.0 - p, p]

_MODEL = _StubModel()


def predict(features):
    """
    features: list of floats (Erlang list decoded by erlport).
    Returns: tuple (class, probability) where class is 0 or 1.
    """
    probs = _MODEL.predict_proba(list(features))
    cls = 0 if probs[0] > probs[1] else 1
    return (cls, probs[cls])


def healthcheck():
    return b"ok"


def model_version():
    # Any Elixir side can query this to detect model drift.
    return b"stub-v1"
```

Empty `priv/python/__init__.py` makes it an importable package.

### Step 2: Worker GenServer (`lib/ml_inference/worker.ex`)

```elixir
defmodule MlInference.Worker do
  @moduledoc """
  Owns one long-lived Python interpreter. Starts the interpreter with the
  priv/python dir on PYTHONPATH and calls predictor.predict/1.
  """
  use GenServer
  require Logger

  # ---- Public API ---------------------------------------------------------

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  @spec predict(pid(), [float()]) :: {:ok, {0 | 1, float()}} | {:error, term()}
  def predict(worker, features) do
    GenServer.call(worker, {:predict, features}, 5_000)
  end

  # ---- GenServer ---------------------------------------------------------

  @impl true
  def init(_opts) do
    python_path = :code.priv_dir(:ml_inference) |> Path.join("python") |> to_charlist()
    {:ok, pid} = :python.start([{:python_path, python_path}, {:python, ~c"python3"}])
    # Warm up the interpreter so the first real call is fast.
    _ = :python.call(pid, :predictor, :healthcheck, [])
    {:ok, %{python: pid}}
  end

  @impl true
  def handle_call({:predict, features}, _from, %{python: py} = state) do
    try do
      result = :python.call(py, :predictor, :predict, [features])
      {:reply, {:ok, result}, state}
    catch
      :exit, reason ->
        {:reply, {:error, {:python_exit, reason}}, state}
    end
  end

  @impl true
  def terminate(_reason, %{python: py}) do
    :python.stop(py)
    :ok
  end
end
```

### Step 3: Pool (`lib/ml_inference/pool.ex`)

```elixir
defmodule MlInference.Pool do
  @moduledoc """
  Round-robin pool of MlInference.Worker processes. Deterministic,
  no queuing — if all workers are busy, the caller waits on GenServer.call.
  """
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  @spec predict([float()]) :: {:ok, {0 | 1, float()}} | {:error, term()}
  def predict(features) do
    worker = GenServer.call(__MODULE__, :checkout)
    MlInference.Worker.predict(worker, features)
  end

  @impl true
  def init(opts) do
    size = Keyword.get(opts, :size, System.schedulers_online())

    workers =
      for _ <- 1..size do
        {:ok, pid} = MlInference.Worker.start_link([])
        pid
      end

    {:ok, %{workers: List.to_tuple(workers), index: 0, size: size}}
  end

  @impl true
  def handle_call(:checkout, _from, %{workers: w, index: i, size: n} = state) do
    worker = elem(w, i)
    {:reply, worker, %{state | index: rem(i + 1, n)}}
  end
end
```

### Step 4: Application supervision

```elixir
defmodule MlInference.Application do
  use Application

  @impl true
  def start(_, _) do
    children = [
      {MlInference.Pool, size: System.schedulers_online()}
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: __MODULE__)
  end
end
```

## Why this works

```
Elixir caller ─Pool.predict──▶ Pool (round-robin picker)
                                     │
                                     ▼
                           Worker GenServer N
                                     │ :python.call
                                     ▼
                  Python interpreter N (model preloaded)
                                     │ result term
                                     ▼
                              {ok, {1, 0.82}}
```

- Each worker owns one Python interpreter — the model is loaded once per worker at boot.
- `:python.call` blocks the worker's GenServer. The pool ensures callers do not pile on
  one worker; a Python-side `while True: pass` only hangs one worker, not all.
- The round-robin index is maintained inside the pool GenServer; checkout is a single
  constant-time call.

## Tests (`test/ml_inference/pool_test.exs`)

```elixir
defmodule MlInference.PoolTest do
  use ExUnit.Case, async: false

  setup_all do
    # Start pool with a small size for tests.
    {:ok, _} = start_supervised({MlInference.Pool, size: 2})
    :ok
  end

  describe "predict/1" do
    test "returns class and probability" do
      assert {:ok, {cls, prob}} = MlInference.Pool.predict([0.1, 0.2, 0.3])
      assert cls in [0, 1]
      assert is_float(prob) and prob >= 0.0 and prob <= 1.0
    end

    test "higher feature sum biases toward class 1" do
      {:ok, {c_neg, _}} = MlInference.Pool.predict([-5.0, -5.0, -5.0])
      {:ok, {c_pos, _}} = MlInference.Pool.predict([5.0, 5.0, 5.0])
      assert c_neg == 0
      assert c_pos == 1
    end
  end

  describe "concurrent predictions" do
    test "100 concurrent calls all succeed" do
      tasks =
        for i <- 1..100 do
          Task.async(fn -> MlInference.Pool.predict([i * 0.01, 1.0, -1.0]) end)
        end

      results = Task.await_many(tasks, 30_000)
      assert Enum.all?(results, &match?({:ok, {_, _}}, &1))
    end
  end
end
```

## Trade-offs and production gotchas

**1. Python GIL bottleneck.** Each worker can process one inference at a time. CPU-bound
inference at 100Hz needs 100/throughput-per-worker many workers. Measure `predict`
latency and size accordingly.

**2. Interpreter startup cost on restart.** Loading scikit-learn + joblib + model can take
seconds. A supervisor restarting a worker blocks new requests on that slot for the boot
time. Use a larger pool to absorb restarts, or delay `predict` calls to that worker until
`healthcheck` returns `ok`.

**3. Memory growth.** Python interpreters can leak via cached DataFrames. Recycle workers
periodically (`:timer.send_interval` with `{:stop, :recycle}`) or monitor RSS and kill
when it exceeds a threshold.

**4. Term conversion surprises.** Elixir atoms become `erlport.Atom` bytes, not strings.
Integer tuples are tuples, not lists. Write a thin adapter at the Elixir/Python boundary
to normalize — don't let encoding quirks leak into business logic.

**5. stderr from Python.** Exceptions print to stderr by default and do not come back as
error terms unless you wrap calls in Python try/except. Wrap everything and return
`{"error", message}` tuples explicitly.

**6. When NOT to use ErlPort.** For models that take hundreds of ms (LLM inference,
image generation), the port overhead is noise — use a networked service. For sub-ms
numerical work on pure-Python NumPy code, consider a NIF (with PyO3 or Rust reimpl).

## Reflection

The pool uses round-robin: worker N gets every Nth request regardless of current load.
If prediction times vary (some calls 1ms, some 50ms), a uniformly-distributed queue
wins. What load shape makes round-robin strictly better than least-loaded, and what
checkout protocol changes would support least-loaded without a global coordinator?

## Resources

- [ErlPort on GitHub](https://github.com/hdima/erlport)
- [ErlPort Python API reference](http://erlport.org/docs/python.html)
- [Serving ML models in Elixir — Dashbit blog](https://dashbit.co/blog/)
- [`:python` module — ErlPort hexdocs](https://hexdocs.pm/erlport/)
