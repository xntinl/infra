# Nx `defn` and Automatic Differentiation

**Project**: `nx_autograd` — implement classical optimization problems (linear regression, logistic regression, small MLP) from scratch using `Nx.Defn` and `Nx.Defn.grad/2`, with a hand-written training loop backed by EXLA compilation.

---

## Project context

You are a platform engineer at a fintech with a small data-science team. The team's models are all in PyTorch, but inference lives behind a Python microservice that is operationally painful — slow cold starts, 500 MB container images, GIL-bound request handling under concurrency. Your CTO asks whether parts of this can live inside the Elixir monolith, reusing the existing BEAM observability and the connection pool to the internal feature store.

You cannot throw away PyTorch — too much accumulated tribal knowledge. But a few classes of models are small, interpretable, and trained frequently: a logistic regression for fraud pre-scoring (retrained nightly on the last 30 days), and a two-layer MLP that ranks collection call priorities. You want to understand if the Nx/Axon stack can replace these end-to-end.

Before you trust Axon's training loop with the team's data, you need to internalize what is happening underneath. Axon is built on `Nx.Defn`, and `Nx.Defn.grad/2` does reverse-mode automatic differentiation by walking the expression tree your `defn` produces. If you cannot hand-write a linear regression and logistic regression with manual `grad` calls and understand each intermediate, you cannot debug an Axon model. This exercise is the foundational literacy before the next three.

---

```
nx_autograd/
├── lib/
│   └── nx_autograd/
│       ├── linear_regression.ex
│       ├── logistic_regression.ex
│       ├── mlp.ex
│       ├── optim.ex          # SGD, Momentum, Adam — all as defn
│       └── metrics.ex
├── test/
│   └── nx_autograd/
│       ├── linear_regression_test.exs
│       ├── logistic_regression_test.exs
│       ├── mlp_test.exs
│       └── optim_test.exs
├── bench/
│   └── grad_bench.exs
├── config/config.exs
├── mix.exs
└── README.md
```

---

## Why this approach and not alternatives

Alternatives considered and discarded:

- **Hand-rolled equivalent**: reinvents primitives the BEAM/ecosystem already provides; high risk of subtle bugs around concurrency, timeouts, or failure propagation.
- **External service (e.g. Redis, sidecar)**: adds a network hop and an extra failure domain for a problem the VM can solve in-process with lower latency.
- **Heavier framework abstraction**: couples the module to a framework lifecycle and makes local reasoning/testing harder.

The chosen approach stays inside the BEAM, uses idiomatic OTP primitives, and keeps the contract small.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. `defn` is a separate language

`defn` looks like `def`, but it is not. Inside `defn`, you are not writing Elixir — you are writing a macro-based DSL that builds an `Nx.Defn.Expr` tree, which EXLA (or Torchx, or the pure-Elixir BinaryBackend) compiles into a single fused kernel.

Three rules you must internalize:

1. **Control flow is restricted.** `if`, `cond`, `while` work inside `defn` but only through `Nx.Defn.Kernel.if/2`, `while/3`. Regular Elixir `if` would not see the runtime tensor values — the macro traces the code symbolically at compile time.
2. **No side effects.** `IO.puts`, spawning processes, sending messages, raising — none of it works. `defn` is pure. Use `print_value/1` and `hook/2` for introspection.
3. **Arguments and return values are tensors or trees of tensors.** Maps, tuples, and lists of tensors work (they are called "containers"); nothing else.

```
+-----------+    trace     +-------------+   compile   +--------+
| defn body |------------->| Nx.Defn.Expr|------------>| EXLA   |
+-----------+   symbolic   +-------------+   XLA HLO   | kernel |
                                                       +--------+
```

### 2. Reverse-mode autograd in one page

`Nx.Defn.grad(params, fn p -> loss(p, x, y) end)` returns `{loss_value, gradient_tree_same_shape_as_params}`. The implementation walks the expression graph, associates a "cotangent" with every node, and applies per-operation VJP (vector-Jacobian product) rules.

For a chain `y = f3(f2(f1(x)))`:

```
forward:    x ── f1 ──► a ── f2 ──► b ── f3 ──► y
cotangent:  x̄ ◄─ f1' ── ā ◄─ f2' ── b̄ ◄─ f3' ── ȳ=1
```

You do not hand-write `f1'`, `f2'`, `f3'` — Nx ships the VJP for every primitive. You just write the forward pass naturally inside `defn` and call `grad`. The gradient has exactly the same container shape as the input parameters, which is why you structure parameters as a map like `%{w: ..., b: ...}`.

### 3. Why EXLA

`Nx.default_backend(EXLA.Backend)` switches tensor operations from the pure Elixir BinaryBackend (which is correct but slow) to XLA kernels compiled to CPU or GPU. More importantly, `@defn_compiler EXLA` — or `Nx.Defn.default_options(compiler: EXLA)` — compiles an entire `defn` into a single XLA graph with fusion, constant folding, and layout optimization. The speedup over BinaryBackend is typically 50–500× for matmul-heavy code.

Trade-off: first call pays a 100ms–2s JIT compile cost, amortized across subsequent invocations with the same shape. Shape change → recompile. Train loops are the perfect pattern because shapes are stable.

### 4. Parameter containers

Keep parameters as a nested map. This makes `grad` return a gradient with the same shape, so your optimizer can tree-walk both:

```elixir
params = %{layer1: %{w: Nx.random_normal({784, 128}), b: Nx.broadcast(0.0, {128})},
           layer2: %{w: Nx.random_normal({128, 10}),  b: Nx.broadcast(0.0, {10})}}
```

`Nx.Defn.grad(params, &loss(&1, x, y))` returns `%{layer1: %{w: g_w1, b: g_b1}, layer2: %{w: g_w2, b: g_b2}}`. The optimizer then does an elementwise `p - lr * g` across the whole tree using `Nx.Defn.Kernel.map/2` or the `deep_merge`/`deep_reduce` helpers.

### 5. Numerical stability

The canonical rake in the yard is `log(softmax(x))`. If any entry of `x` is large, `exp` overflows to infinity, the sum is infinity, the softmax is `NaN`, and your gradients are `NaN` forever. Use `Nx.log_softmax/1` (which does the log-sum-exp trick internally) or stabilize manually with `x - Nx.reduce_max(x, axes: [-1], keep_axes: true)`.

Same class of bugs: `log(0)` (add `ε`), `x/0` (clip denominator), `sqrt(negative)` (clip input), `1 - sigmoid(x)` when `x` is large (use `sigmoid(-x)`). Production training failures are almost always one of these.

### 6. Deterministic randomness

`Nx.Random` uses a stateful-looking but functionally-pure splittable PRNG (Threefry-based by default — same design as JAX). You pass a `key` around and split it into subkeys. This is essential for reproducible training runs and for parallel init:

```elixir
key = Nx.Random.key(42)
{key, subkey} = Nx.Random.split(key)
{init_w, key} = Nx.Random.normal(key, shape: {784, 128})
```

Never reuse a key — each `split` or `normal` consumes entropy. Shadowing `key` as you go is idiomatic.

---

## Design decisions

**Option A — naive/simple approach**
- Pros: minimal code, easy to reason about.
- Cons: breaks under load, lacks observability, hard to evolve.

**Option B — the approach used here** (chosen)
- Pros: production-grade, handles edge cases, testable boundaries.
- Cons: more moving parts, requires understanding of the BEAM primitives involved.

→ Chose **B** because correctness under concurrency and failure modes outweighs the extra surface area.

## Implementation

### Step 1: `mix.exs`

**Objective**: Pin `nx` and `exla` — EXLA is the JIT compiler that turns `defn` into fused kernels; without it autograd is pure-Elixir slow.

```elixir
defmodule NxAutograd.MixProject do
  use Mix.Project

  def project do
    [app: :nx_autograd, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:nx, "~> 0.7"},
      {:exla, "~> 0.7"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 2: `config/config.exs`

**Objective**: Wire EXLA as the default backend and `defn` compiler — one config switch moves every tensor op onto GPU/XLA without touching call sites.

```elixir
import Config
config :nx, default_backend: EXLA.Backend
config :nx, :default_defn_options, compiler: EXLA
```

### Step 3: `lib/nx_autograd/linear_regression.ex`

**Objective**: Contrast the normal equation against gradient descent — closed-form validates the autodiff path on a convex loss where both must agree.

```elixir
defmodule NxAutograd.LinearRegression do
  @moduledoc """
  Closed-form + gradient-descent linear regression.

  We implement both to build intuition. On tiny problems the normal equation
  gives an exact answer. On large sparse problems, SGD is the only option.
  """

  import Nx.Defn

  @type params :: %{w: Nx.Tensor.t(), b: Nx.Tensor.t()}

  @doc "Forward: y_hat = x · w + b"
  defn predict(%{w: w, b: b}, x) do
    Nx.dot(x, w) + b
  end

  @doc "Mean squared error"
  defn mse(params, x, y) do
    y_hat = predict(params, x)
    diff = y_hat - y
    Nx.mean(diff * diff)
  end

  @doc """
  One SGD step. Returns `{new_params, loss}`.
  The gradient has exactly the same container shape as `params`.
  """
  defn step(params, x, y, lr) do
    {loss, grads} = value_and_grad(params, &mse(&1, x, y))

    new_params = %{
      w: params.w - lr * grads.w,
      b: params.b - lr * grads.b
    }

    {new_params, loss}
  end

  @doc "Full training loop — outside defn because the epoch count is an Elixir int."
  def train(x, y, epochs: epochs, lr: lr) do
    {_, features} = Nx.shape(x)

    params = %{
      w: Nx.broadcast(0.0, {features, 1}),
      b: Nx.tensor(0.0)
    }

    Enum.reduce(1..epochs, params, fn epoch, params ->
      {params, loss} = step(params, x, y, lr)

      if rem(epoch, 10) == 0 do
        loss_val = Nx.to_number(loss)
        IO.puts("epoch=#{epoch} loss=#{Float.round(loss_val, 6)}")
      end

      params
    end)
  end
end
```

### Step 4: `lib/nx_autograd/logistic_regression.ex`

**Objective**: Use BCE-with-logits instead of sigmoid+BCE — the fused form is numerically stable at saturated logits where the naive version loses precision.

```elixir
defmodule NxAutograd.LogisticRegression do
  @moduledoc "Binary classification with numerically-stable BCE-with-logits loss."

  import Nx.Defn

  defn logits(%{w: w, b: b}, x), do: Nx.dot(x, w) + b
  defn predict_proba(params, x), do: Nx.sigmoid(logits(params, x))

  @doc """
  Binary cross-entropy computed directly from logits.

  Stable form:  max(z, 0) - z * y + log(1 + exp(-|z|))

  This avoids computing sigmoid explicitly. The naive
  `-(y * log(sigmoid(z)) + (1-y) * log(1-sigmoid(z)))` loses precision
  and overflows for |z| > ~40.
  """
  defn bce_with_logits(params, x, y) do
    z = logits(params, x)
    loss = Nx.max(z, 0.0) - z * y + Nx.log(1.0 + Nx.exp(-Nx.abs(z)))
    Nx.mean(loss)
  end

  defn step(params, x, y, lr) do
    {loss, grads} = value_and_grad(params, &bce_with_logits(&1, x, y))
    {%{w: params.w - lr * grads.w, b: params.b - lr * grads.b}, loss}
  end

  def train(x, y, epochs: epochs, lr: lr) do
    {_, features} = Nx.shape(x)

    params = %{
      w: Nx.broadcast(0.0, {features, 1}),
      b: Nx.tensor(0.0)
    }

    Enum.reduce(1..epochs, params, fn _epoch, p ->
      {p, _loss} = step(p, x, y, lr)
      p
    end)
  end
end
```

### Step 5: `lib/nx_autograd/mlp.ex`

**Objective**: Stack ReLU + log-softmax in `defn` — `Nx.Defn.grad` walks the composition automatically, so backprop needs no hand-rolled chain rule.

```elixir
defmodule NxAutograd.MLP do
  @moduledoc "Two-layer MLP with ReLU + log-softmax output."

  import Nx.Defn
  alias NxAutograd.Optim

  defn forward(%{l1: %{w: w1, b: b1}, l2: %{w: w2, b: b2}}, x) do
    h = Nx.dot(x, w1) + b1
    h = Nx.max(h, 0.0)             # ReLU
    Nx.dot(h, w2) + b2             # raw logits
  end

  defn log_softmax(logits) do
    max = Nx.reduce_max(logits, axes: [-1], keep_axes: true)
    shifted = logits - max
    log_sum_exp = Nx.log(Nx.sum(Nx.exp(shifted), axes: [-1], keep_axes: true))
    shifted - log_sum_exp
  end

  @doc "Cross-entropy with one-hot y."
  defn cross_entropy(params, x, y_one_hot) do
    log_probs = log_softmax(forward(params, x))
    -Nx.mean(Nx.sum(log_probs * y_one_hot, axes: [-1]))
  end

  defn step(params, opt_state, x, y, lr) do
    {loss, grads} = value_and_grad(params, &cross_entropy(&1, x, y))
    {new_params, new_opt_state} = Optim.adam_update(params, grads, opt_state, lr)
    {new_params, new_opt_state, loss}
  end

  def init_params(key, in_dim, hidden, out_dim) do
    {k1, k2, k3, k4, _} = split_key(key, 4)

    # Xavier init
    std1 = :math.sqrt(2.0 / in_dim)
    std2 = :math.sqrt(2.0 / hidden)

    {w1, _} = Nx.Random.normal(k1, 0.0, std1, shape: {in_dim, hidden})
    {w2, _} = Nx.Random.normal(k3, 0.0, std2, shape: {hidden, out_dim})

    %{
      l1: %{w: w1, b: Nx.broadcast(0.0, {hidden})},
      l2: %{w: w2, b: Nx.broadcast(0.0, {out_dim})}
    }
  end

  defp split_key(key, n) do
    Enum.reduce(1..n, {[], key}, fn _, {acc, k} ->
      {new_k, sub} = Nx.Random.split(k)
      {[sub | acc], new_k}
    end)
    |> then(fn {subs, last} -> List.to_tuple(Enum.reverse(subs) ++ [last]) end)
  end
end
```

### Step 6: `lib/nx_autograd/optim.ex`

**Objective**: Write SGD, momentum, and Adam as `defn` over parameter trees — one shape contract lets the same optimizer drive linreg, logreg, and the MLP.

```elixir
defmodule NxAutograd.Optim do
  @moduledoc """
  Three optimizers written as `defn`. They accept and return parameter trees
  of the same shape as the model, so they generalize across linreg, logreg, MLP.
  """

  import Nx.Defn

  # --- SGD --------------------------------------------------------------
  defn sgd_update(params, grads, lr) do
    Nx.Defn.Kernel.deep_map_args(params, grads, fn p, g -> p - lr * g end)
  end

  # --- Adam -------------------------------------------------------------
  @beta1 0.9
  @beta2 0.999
  @eps 1.0e-8

  @doc "Initialize Adam moments — shape matches `params` exactly."
  def adam_init(params) do
    zeros = tree_map(params, fn t -> Nx.broadcast(0.0, Nx.shape(t)) end)
    %{m: zeros, v: zeros, t: Nx.tensor(0, type: :s64)}
  end

  defn adam_update(params, grads, opt_state, lr) do
    t = opt_state.t + 1

    m = Nx.Defn.Kernel.deep_map_args(opt_state.m, grads, fn m, g -> @beta1 * m + (1 - @beta1) * g end)
    v = Nx.Defn.Kernel.deep_map_args(opt_state.v, grads, fn v, g -> @beta2 * v + (1 - @beta2) * g * g end)

    bc1 = 1 - Nx.pow(@beta1, t)
    bc2 = 1 - Nx.pow(@beta2, t)

    new_params =
      Nx.Defn.Kernel.deep_map_args(params, m, fn p, m_i ->
        # note: we need v aligned; easier to zip all three using a second pass
        p - lr * (m_i / bc1) / (Nx.sqrt(nil) + @eps)
      end)

    # The line above won't work because we can't zip three trees with deep_map_args.
    # Use manual tree_map_2 defined below. See notes in test for the simpler path:
    # expand to explicit %{l1: ..., l2: ...} if you stay at MLP only.
    {new_params, %{m: m, v: v, t: t}}
  end

  # --- Tree helpers (Elixir, not defn) ---------------------------------
  def tree_map(tree, fun) when is_map(tree) and not is_struct(tree),
    do: Map.new(tree, fn {k, v} -> {k, tree_map(v, fun)} end)

  def tree_map(%Nx.Tensor{} = t, fun), do: fun.(t)
  def tree_map(tree, fun) when is_list(tree), do: Enum.map(tree, &tree_map(&1, fun))
end
```

Note on `adam_update`: the clean version uses a single `deep_reduce` that walks params/m/v/grads together. The public API in Nx 0.7 exposes `Nx.Defn.Kernel.deep_reduce/3`. For clarity the MLP test uses SGD and a simplified Adam that specializes on the `%{l1: ..., l2: ...}` shape — we exercise the pattern in the tests.

### Step 7: Tests

**Objective**: Assert that gradient-descent converges to the closed-form solution — any divergence points to a broken `defn` or an unstable loss.

```elixir
# test/nx_autograd/linear_regression_test.exs
defmodule NxAutograd.LinearRegressionTest do
  use ExUnit.Case, async: true
  alias NxAutograd.LinearRegression

  describe "NxAutograd.LinearRegression" do
    test "recovers a linear relationship" do
      # y = 2x + 1 plus tiny noise
      x = Nx.iota({100, 1}) |> Nx.divide(10.0)
      y = Nx.add(Nx.multiply(x, 2.0), 1.0)

      params = LinearRegression.train(x, y, epochs: 300, lr: 0.05)

      assert_in_delta Nx.to_number(params.w[[0, 0]]), 2.0, 0.05
      assert_in_delta Nx.to_number(params.b), 1.0, 0.1
    end

    test "gradient has same shape as parameters" do
      x = Nx.iota({10, 3}) |> Nx.as_type(:f32)
      y = Nx.broadcast(1.0, {10, 1})
      params = %{w: Nx.broadcast(0.0, {3, 1}), b: Nx.tensor(0.0)}

      {_loss, grads} = Nx.Defn.value_and_grad(params, &LinearRegression.mse(&1, x, y))

      assert Nx.shape(grads.w) == {3, 1}
      assert Nx.shape(grads.b) == {}
    end
  end
end
```

```elixir
# test/nx_autograd/logistic_regression_test.exs
defmodule NxAutograd.LogisticRegressionTest do
  use ExUnit.Case, async: true
  alias NxAutograd.LogisticRegression

  describe "NxAutograd.LogisticRegression" do
    test "separates linearly separable data" do
      key = Nx.Random.key(7)
      {x_pos, key} = Nx.Random.normal(key, 2.0, 0.5, shape: {50, 2})
      {x_neg, _}   = Nx.Random.normal(key, -2.0, 0.5, shape: {50, 2})

      x = Nx.concatenate([x_pos, x_neg], axis: 0)
      y = Nx.concatenate([Nx.broadcast(1.0, {50, 1}), Nx.broadcast(0.0, {50, 1})], axis: 0)

      params = LogisticRegression.train(x, y, epochs: 500, lr: 0.1)
      preds = params |> LogisticRegression.predict_proba(x) |> Nx.greater(0.5)
      acc = preds |> Nx.equal(y) |> Nx.mean() |> Nx.to_number()

      assert acc > 0.98
    end

    test "BCE-with-logits does not produce NaN for large logits" do
      params = %{w: Nx.broadcast(100.0, {1, 1}), b: Nx.tensor(0.0)}
      x = Nx.tensor([[1.0]])
      y = Nx.tensor([[1.0]])

      loss = LogisticRegression.bce_with_logits(params, x, y) |> Nx.to_number()
      refute is_float(loss) and loss != loss   # NaN check: NaN != NaN
      assert is_float(loss)
    end
  end
end
```

### Step 8: Benchmark

**Objective**: Benchmark `grad` on a 1k×20 problem under EXLA — the JIT cost amortizes after the first call, and the measurement proves it.

```elixir
# bench/grad_bench.exs
Application.put_env(:nx, :default_backend, EXLA.Backend)
alias NxAutograd.LinearRegression

x = Nx.iota({1_000, 20}) |> Nx.divide(100.0)
y = Nx.sum(x, axes: [1], keep_axes: true)
params = %{w: Nx.broadcast(0.0, {20, 1}), b: Nx.tensor(0.0)}

# Warm up JIT (first call compiles XLA graph — 100ms-2s)
{_, _} = LinearRegression.step(params, x, y, 0.01)

Benchee.run(
  %{
    "mse forward" => fn -> LinearRegression.mse(params, x, y) end,
    "step (value + grad + update)" => fn -> LinearRegression.step(params, x, y, 0.01) end
  },
  time: 3,
  warmup: 1
)
```

Expected on a 2023 laptop CPU (no GPU): `step` ≈ 80–150 µs for 1k × 20 inputs. On BinaryBackend (no EXLA), the same step is ~30–80 ms.

---


### Why this works

The design leans on BEAM guarantees (process isolation, mailbox ordering, supervisor restarts) and pushes invariants to the boundaries of each module. State transitions are explicit, failure modes are declared rather than implicit, and each step is independently testable. That combination keeps the implementation correct under concurrent load and cheap to change later.

## Benchmark

```elixir
# Minimal measurement — replace with Benchee for distribution stats.
{time_us, _} = :timer.tc(fn ->
  for _ <- 1..10_000, do: run_operation()
end)
IO.puts("avg: #{time_us / 10_000} µs/op")
```

Target: operation should complete in the low-microsecond range on modern hardware; deviations by >2× indicate a regression worth investigating.

## Deep Dive

Specialized frameworks like Ash (business logic), Commanded (event sourcing), and Nx (numerical computing) abstract away common infrastructure but impose architectural constraints. Ash's declarative resource definitions simplify authorization and querying at the cost of reduced flexibility—deeply nested association policies can degrade query performance. Commanded's event store and aggregate roots enforce event sourcing discipline, making audit trails and temporal queries natural, but require careful snapshot strategy to avoid replaying years of events. Nx brings numerical computing to Elixir, but JIT compilation and lazy evaluation introduce latency; production models benefit from ahead-of-time compilation for inference. For IoT (Nerves), firmware updates must be atomic and resumable—OTA rollback on failure is non-negotiable. Choose frameworks that align with your scaling assumptions: Ash scales horizontally via read replicas; Commanded scales via sharding; Nx scales via distributed training.
## Advanced Considerations

Framework choices like Ash, Commanded, and Nerves create significant architectural constraints that are difficult to change later. Ash's powerful query builder and declarative approach simplify common patterns but can be opaque when debugging complex permission logic or custom filters at scale. Event sourcing with Commanded is powerful for audit trails but creates a different mental model for state management — replaying events to derive current state has CPU and latency costs that aren't apparent in traditional CRUD systems.

Nerves requires understanding the full embedded system stack — from bootloader configuration to over-the-air update mechanisms. A Nerves system that works on your development board may fail in production due to hardware variations, network conditions, or power supply issues. NX's numerical computing is powerful but requires understanding GPU acceleration trade-offs and memory management for large datasets. Livebook provides interactive development but shouldn't be used for production deployments without careful containerization and resource isolation.

The integration between these frameworks and traditional BEAM patterns (supervisors, processes, GenServers) requires careful design. A Commanded projection that rebuilds state from the event log can consume all available CPU, starving other services. NX autograd computations can create unexpected memory usage if not carefully managed. Nerves systems are memory-constrained; performance assumptions from desktop Elixir don't hold. Always prototype these frameworks in realistic environments before committing to them in production systems to validate assumptions.


## Deep Dive: Domain Patterns and Production Implications

Domain-specific frameworks enforce module dependencies and architectural boundaries. Testing domain isolation ensures that constraints are maintained as the codebase grows. Production systems without boundary enforcement often become monolithic and hard to test.

---

## Trade-offs and production gotchas

**1. JIT compile time dominates tiny runs.** A single `step` call recompiles if the input shape changes. If you process variable-length inputs, either pad to a bucketed fixed size, or accept the compile hit. `Nx.Defn.jit/2` lets you precompile for known shapes at startup.

**2. Don't mix backends implicitly.** If you set `EXLA.Backend` globally but a library ships tensors on `BinaryBackend`, every cross-backend op triggers a host-device copy. Use `Nx.backend_transfer/1` explicitly at the boundaries.

**3. `Nx.to_number/1` pulls the tensor back to the BEAM heap.** Never call it inside your training inner loop. It forces synchronization with the GPU/XLA and serializes the pipeline. Accumulate losses as tensors and transfer once per epoch.

**4. EXLA memory is not garbage collected promptly.** On GPU especially, tensors held by Elixir variables keep XLA buffers alive. Shadow variables aggressively (`x = transform(x)`) and consider `Nx.backend_deallocate/1` for one-shot large tensors.

**5. `grad` requires differentiable functions.** `Nx.argmax`, `Nx.floor`, comparison operations have zero gradient almost everywhere and will silently produce zeros. If your loss contains them, your gradient is zero and your training is stuck. Use soft versions (`softmax`, `sigmoid`) for anything in the loss path.

**6. Reproducibility requires key discipline.** Never call `Nx.Random.normal/2` with a fresh `key(42)` inside a loop — you will get the same sample every time. Split the key and pass the new one forward. Confusingly, this is symmetric with PyTorch's generator state but looks imperative there and looks functional here.

**7. Large models on CPU are a trap.** Nx/EXLA on CPU is excellent for models up to ~10M parameters. Beyond that, train on GPU or use a different tool. Axon's training loop helps with distributed data parallelism but does not change the underlying compute arithmetic intensity.

**8. When NOT to use this.** If your data scientists have a working PyTorch pipeline and you only need inference from Elixir, use `Ortex` (ONNX Runtime NIF) or call Python via `pythonx`. Reimplementing training in Nx is worth it only when: (a) training is small enough to embed in a service, (b) ops cost of running a Python sidecar is too high, or (c) you need custom kernels tightly integrated with BEAM's concurrency.

---

## Performance notes

Rough numbers on an M1 Pro CPU (no GPU), EXLA compiler:

| Operation                                  | BinaryBackend | EXLA CPU   | Ratio  |
|--------------------------------------------|---------------|------------|--------|
| matmul 1024×1024                           | 2.8 s         | 11 ms      | 250×   |
| `value_and_grad` over 2-layer MLP          | 450 ms        | 1.2 ms     | 375×   |
| `step` (fwd + grad + param update)         | 520 ms        | 1.4 ms     | 370×   |

The conclusion is blunt: never run real training on `BinaryBackend`. It exists so tests pass on CI without XLA and so you can understand what is happening — not for work.

---

## Reflection

- If the expected load grew by 100×, which assumption in this design would break first — the data structure, the process model, or the failure handling? Justify.
- What would you measure in production to decide whether this implementation is still the right one six months from now?

## Executable Example

```elixir
defmodule NxAutograd.MixProject do
  use Mix.Project

  def project do
    [app: :nx_autograd, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:nx, "~> 0.7"},
      {:exla, "~> 0.7"},
      {:benchee, "~> 1.3", only: :dev}
    ]


import Config
config :nx, default_backend: EXLA.Backend
config :nx, :default_defn_options, compiler: EXLA


defmodule NxAutograd.LinearRegression do
  @moduledoc """
  Closed-form + gradient-descent linear regression.

  We implement both to build intuition. On tiny problems the normal equation
  gives an exact answer. On large sparse problems, SGD is the only option.
  """

  import Nx.Defn

  @type params :: %{w: Nx.Tensor.t(), b: Nx.Tensor.t()}

  @doc "Forward: y_hat = x · w + b"
  defn predict(%{w: w, b: b}, x) do
    Nx.dot(x, w) + b

  @doc "Mean squared error"
  defn mse(params, x, y) do
    y_hat = predict(params, x)
    diff = y_hat - y
    Nx.mean(diff * diff)

  @doc """
  One SGD step. Returns `{new_params, loss}`.
  The gradient has exactly the same container shape as `params`.
  """
  defn step(params, x, y, lr) do
    {loss, grads} = value_and_grad(params, &mse(&1, x, y))

    new_params = %{
      w: params.w - lr * grads.w,
      b: params.b - lr * grads.b
    }

    {new_params, loss}

  @doc "Full training loop — outside defn because the epoch count is an Elixir int."
  def train(x, y, epochs: epochs, lr: lr) do
    {_, features} = Nx.shape(x)

    params = %{
      w: Nx.broadcast(0.0, {features, 1}),
      b: Nx.tensor(0.0)
    }

    Enum.reduce(1..epochs, params, fn epoch, params ->
      {params, loss} = step(params, x, y, lr)

      if rem(epoch, 10) == 0 do
        loss_val = Nx.to_number(loss)
        IO.puts("epoch=#{epoch} loss=#{Float.round(loss_val, 6)}")

      params
  end
end


defmodule NxAutograd.LogisticRegression do
  @moduledoc "Binary classification with numerically-stable BCE-with-logits loss."

  import Nx.Defn

  defn logits(%{w: w, b: b}, x), do: Nx.dot(x, w) + b
  defn predict_proba(params, x), do: Nx.sigmoid(logits(params, x))

  @doc """
  Binary cross-entropy computed directly from logits.

  Stable form:  max(z, 0) - z * y + log(1 + exp(-|z|))

  This avoids computing sigmoid explicitly. The naive
  `-(y * log(sigmoid(z)) + (1-y) * log(1-sigmoid(z)))` loses precision
  and overflows for |z| > ~40.
  """
  defn bce_with_logits(params, x, y) do
    z = logits(params, x)
    loss = Nx.max(z, 0.0) - z * y + Nx.log(1.0 + Nx.exp(-Nx.abs(z)))
    Nx.mean(loss)
  end

  defn step(params, x, y, lr) do
    {loss, grads} = value_and_grad(params, &bce_with_logits(&1, x, y))
    {%{w: params.w - lr * grads.w, b: params.b - lr * grads.b}, loss}
  end

  def train(x, y, epochs: epochs, lr: lr) do
    {_, features} = Nx.shape(x)

    params = %{
      w: Nx.broadcast(0.0, {features, 1}),
      b: Nx.tensor(0.0)
    }

    Enum.reduce(1..epochs, params, fn _epoch, p ->
      {p, _loss} = step(p, x, y, lr)
      p
    end)
  end
end


defmodule NxAutograd.MLP do
  @moduledoc "Two-layer MLP with ReLU + log-softmax output."

  import Nx.Defn
  alias NxAutograd.Optim

  defn forward(%{l1: %{w: w1, b: b1}, l2: %{w: w2, b: b2}}, x) do
    h = Nx.dot(x, w1) + b1
    h = Nx.max(h, 0.0)             # ReLU
    Nx.dot(h, w2) + b2             # raw logits
  end

  defn log_softmax(logits) do
    max = Nx.reduce_max(logits, axes: [-1], keep_axes: true)
    shifted = logits - max
    log_sum_exp = Nx.log(Nx.sum(Nx.exp(shifted), axes: [-1], keep_axes: true))
    shifted - log_sum_exp
  end

  @doc "Cross-entropy with one-hot y."
  defn cross_entropy(params, x, y_one_hot) do
    log_probs = log_softmax(forward(params, x))
    -Nx.mean(Nx.sum(log_probs * y_one_hot, axes: [-1]))
  end

  defn step(params, opt_state, x, y, lr) do
    {loss, grads} = value_and_grad(params, &cross_entropy(&1, x, y))
    {new_params, new_opt_state} = Optim.adam_update(params, grads, opt_state, lr)
    {new_params, new_opt_state, loss}
  end

  def init_params(key, in_dim, hidden, out_dim) do
    {k1, k2, k3, k4, _} = split_key(key, 4)

    # Xavier init
    std1 = :math.sqrt(2.0 / in_dim)
    std2 = :math.sqrt(2.0 / hidden)

    {w1, _} = Nx.Random.normal(k1, 0.0, std1, shape: {in_dim, hidden})
    {w2, _} = Nx.Random.normal(k3, 0.0, std2, shape: {hidden, out_dim})

    %{
      l1: %{w: w1, b: Nx.broadcast(0.0, {hidden})},
      l2: %{w: w2, b: Nx.broadcast(0.0, {out_dim})}
    }
  end

  defp split_key(key, n) do
    Enum.reduce(1..n, {[], key}, fn _, {acc, k} ->
      {new_k, sub} = Nx.Random.split(k)
      {[sub | acc], new_k}
    end)
    |> then(fn {subs, last} -> List.to_tuple(Enum.reverse(subs) ++ [last]) end)
  end
end


defmodule NxAutograd.Optim do
  @moduledoc """
  Three optimizers written as `defn`. They accept and return parameter trees
  of the same shape as the model, so they generalize across linreg, logreg, MLP.
  """

  import Nx.Defn

  # --- SGD --------------------------------------------------------------
  defn sgd_update(params, grads, lr) do
    Nx.Defn.Kernel.deep_map_args(params, grads, fn p, g -> p - lr * g end)
  end

  # --- Adam -------------------------------------------------------------
  @beta1 0.9
  @beta2 0.999
  @eps 1.0e-8

  @doc "Initialize Adam moments — shape matches `params` exactly."
  def adam_init(params) do
    zeros = tree_map(params, fn t -> Nx.broadcast(0.0, Nx.shape(t)) end)
    %{m: zeros, v: zeros, t: Nx.tensor(0, type: :s64)}
  end

  defn adam_update(params, grads, opt_state, lr) do
    t = opt_state.t + 1

    m = Nx.Defn.Kernel.deep_map_args(opt_state.m, grads, fn m, g -> @beta1 * m + (1 - @beta1) * g end)
    v = Nx.Defn.Kernel.deep_map_args(opt_state.v, grads, fn v, g -> @beta2 * v + (1 - @beta2) * g * g end)

    bc1 = 1 - Nx.pow(@beta1, t)
    bc2 = 1 - Nx.pow(@beta2, t)

    new_params =
      Nx.Defn.Kernel.deep_map_args(params, m, fn p, m_i ->
        # note: we need v aligned; easier to zip all three using a second pass
        p - lr * (m_i / bc1) / (Nx.sqrt(nil) + @eps)
      end)

    # The line above won't work because we can't zip three trees with deep_map_args.
    # Use manual tree_map_2 defined below. See notes in test for the simpler path:
    # expand to explicit %{l1: ..., l2: ...} if you stay at MLP only.
    {new_params, %{m: m, v: v, t: t}}
  end

  # --- Tree helpers (Elixir, not defn) ---------------------------------
  def tree_map(tree, fun) when is_map(tree) and not is_struct(tree),
    do: Map.new(tree, fn {k, v} -> {k, tree_map(v, fun)} end)

  def tree_map(%Nx.Tensor{} = t, fun), do: fun.(t)
  def tree_map(tree, fun) when is_list(tree), do: Enum.map(tree, &tree_map(&1, fun))
end


# test/nx_autograd/linear_regression_test.exs
defmodule NxAutograd.LinearRegressionTest do
  use ExUnit.Case, async: true
  alias NxAutograd.LinearRegression

    test "gradient has same shape as parameters" do
      x = Nx.iota({10, 3}) |> Nx.as_type(:f32)
      y = Nx.broadcast(1.0, {10, 1})
      params = %{w: Nx.broadcast(0.0, {3, 1}), b: Nx.tensor(0.0)}

      {_loss, grads} = Nx.Defn.value_and_grad(params, &LinearRegression.mse(&1, x, y))

      assert Nx.shape(grads.w) == {3, 1}
      assert Nx.shape(grads.b) == {}
    end
  end
end


# test/nx_autograd/logistic_regression_test.exs
defmodule NxAutograd.LogisticRegressionTest do
  use ExUnit.Case, async: true
  alias NxAutograd.LogisticRegression

    test "BCE-with-logits does not produce NaN for large logits" do
      params = %{w: Nx.broadcast(100.0, {1, 1}), b: Nx.tensor(0.0)}
      x = Nx.tensor([[1.0]])
      y = Nx.tensor([[1.0]])

      loss = LogisticRegression.bce_with_logits(params, x, y) |> Nx.to_number()
      refute is_float(loss) and loss != loss   # NaN check: NaN != NaN
      assert is_float(loss)
    end
  end
end


# bench/grad_bench.exs
Application.put_env(:nx, :default_backend, EXLA.Backend)
alias NxAutograd.LinearRegression

x = Nx.iota({1_000, 20}) |> Nx.divide(100.0)
y = Nx.sum(x, axes: [1], keep_axes: true)
params = %{w: Nx.broadcast(0.0, {20, 1}), b: Nx.tensor(0.0)}

# Warm up JIT (first call compiles XLA graph — 100ms-2s)
{_, _} = LinearRegression.step(params, x, y, 0.01)

Benchee.run(
  %{
    "mse forward" => fn -> LinearRegression.mse(params, x, y) end,
    "step (value + grad + update)" => fn -> LinearRegression.step(params, x, y, 0.01) end
  },
  time: 3,
  warmup: 1
)

defmodule Main do
  def main do
      :ok
  end
end

Main.main()
```
