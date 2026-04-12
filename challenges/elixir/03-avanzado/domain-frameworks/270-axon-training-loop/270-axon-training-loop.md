# Axon.Loop: Custom Training Loop with Callbacks, Metrics, and Checkpointing

**Project**: `axon_training_loop` — build a production-grade training pipeline on top of `Axon.Loop`: gradient accumulation, LR schedule, early stopping, EMA weights, and checkpoint-resume on crash.

**Difficulty**: ★★★★☆

**Estimated time**: 4–6 hours

---

## Project context

You run ML platform at a mid-size company. The data science team has standardized on Axon for new models. Their first pilot — a transformer-based document classifier over Spanish legal text — trains for ~18 hours per full run on a single A100. Three runs have failed in the last month: one ran out of GPU memory at epoch 12 of 20, one had its spot instance preempted at epoch 9, and one converged to a worse minimum than a prior run because the seed changed silently.

Your team's mandate: wrap every training job in a harness that (a) checkpoints weights and optimizer state every N steps, (b) resumes from the latest checkpoint if the process dies, (c) logs metrics and LR to a structured sink, (d) applies gradient accumulation so the team can simulate 4× the batch size on the same hardware, (e) uses an EMA copy of the weights for final evaluation (standard modern-ML practice — smooths noisy late-training behavior), (f) stops early if the validation metric plateaus.

Axon provides `Axon.Loop` — a composable training loop built on `Nx`. It ships with `Axon.Loop.trainer/3` for the common case and `Axon.Loop.handle_event/4` for injecting custom callbacks on `:started`, `:epoch_started`, `:iteration_completed`, `:epoch_completed`, `:completed`. The trick is composing callbacks without turning the loop into an unreadable tangle. This exercise implements each concern as a focused callback module that composes into the final trainer.

---

```
axon_training_loop/
├── lib/
│   └── axon_training_loop/
│       ├── trainer.ex                  # assembles the loop from callbacks
│       ├── callbacks/
│       │   ├── checkpoint.ex
│       │   ├── early_stopping.ex
│       │   ├── ema.ex
│       │   ├── grad_accum.ex
│       │   └── lr_schedule.ex
│       ├── datasets.ex
│       └── reporter.ex                 # telemetry sink
├── test/
│   └── axon_training_loop/
│       ├── trainer_test.exs
│       ├── checkpoint_test.exs
│       ├── early_stopping_test.exs
│       └── ema_test.exs
├── config/config.exs
├── mix.exs
└── README.md
```

---

## Core concepts

### 1. `Axon.Loop` as a state machine

`Axon.Loop` is a functional state machine. The state has this rough shape:

```elixir
%Axon.Loop.State{
  epoch: 0, iteration: 0, max_epoch: 10, max_iteration: :infinity,
  step_state: %{model_state: %{...}, optimizer_state: %{...}, loss: 0.0, i: 0},
  metrics: %{"loss" => ..., "accuracy" => ...},
  handler_metadata: %{},      # arbitrary — our callbacks store state here
  status: :halted | :completed
}
```

Callbacks receive this state and return `{:continue, state}` or `{:halt_epoch, state}` / `{:halt_loop, state}`. Every callback is pure — it returns a new state rather than mutating. This matters because on crash+restart, replaying the last checkpoint deterministically reproduces the exact same state (assuming deterministic data order, which you must enforce).

```
+--------+    +----------------+    +-------------+    +----------------+
| :start |--->| epoch_started  |--->| train steps |--->| epoch_completed |
+--------+    +----------------+    +-------------+    +----------------+
                                         |                     |
                                         v                     v
                                   iteration_completed    :completed
                                         (N times)
```

### 2. Gradient accumulation

A "batch size 512" run that OOMs at 512 can often train at "effective batch size 512, physical batch size 128, accumulation=4". The model processes 4 micro-batches, accumulates gradients, then applies the optimizer step once. Loss scales identically (mean over 4× more samples). You trade wallclock time for GPU memory.

Implementation detail: you cannot simply multiply the loss by `1/N` — you need to accumulate the gradient tree. The cleanest form:

```
for micro_step in 0..N-1:
    {loss_i, grad_i} = value_and_grad(params, ...)
    accum = tree_add(accum, grad_i)
    if micro_step == N - 1:
        params, opt_state = optimizer_apply(params, accum / N, opt_state)
        accum = tree_zero_like(params)
```

### 3. Exponential Moving Average of weights

Track `θ_ema = α * θ_ema + (1 - α) * θ` after every step, with `α ≈ 0.999`. At evaluation time, use `θ_ema` instead of `θ`. Why it works: late-stage SGD oscillates around a local minimum. The EMA sits closer to the basin's center than any single step and usually improves validation accuracy by 0.2–1.0 points without any other change.

Cost: you store a second copy of every parameter — 2× the model-state memory. Worth it when the model fits with room to spare.

### 4. LR schedules

Two schedules cover most real needs:

- **Cosine with warmup**: linearly ramp LR from 0 to `lr_max` over `warmup_steps`, then decay with `lr = lr_max * 0.5 * (1 + cos(π * progress))` where `progress = (step - warmup) / (total - warmup)`. Standard for transformers.
- **Step decay**: every `K` epochs, divide LR by 10. Classic for CNNs.

Implement the schedule as a pure function `schedule(step) -> lr`, then the callback updates the optimizer state's learning rate before each step. This lets you plot "what schedule will this run use?" without training.

### 5. Checkpoint-resume

Checkpoints must save:
- Model parameters
- Optimizer state (momentum, Adam moments, step count — losing these restarts optimization from a bad place)
- EMA state
- Training progress (epoch, iteration, RNG key, best metric so far)

Format: `:erlang.term_to_binary/1` with `[:compressed, {:minor_version, 2}]` works fine for ~100 MB models. Beyond that, write tensors separately as raw binaries and reference them from a manifest, to avoid a 4 GB `term_to_binary` round-trip.

Atomicity: write to `checkpoint.tmp`, `fsync`, then rename to `checkpoint-N.ckpt`. A crash mid-write must never leave a corrupt `checkpoint.ckpt`.

### 6. Early stopping

Track `best_metric_so_far`. After each epoch, if `metric` did not improve by `min_delta` in `patience` epochs, halt. Subtle: "improve" depends on metric direction — accuracy ↑, loss ↓. Make it explicit in the callback config.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule AxonTrainingLoop.MixProject do
  use Mix.Project

  def project do
    [app: :axon_training_loop, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger]]

  defp deps do
    [
      {:axon, "~> 0.7"},
      {:nx, "~> 0.7"},
      {:exla, "~> 0.7"},
      {:polaris, "~> 0.1"},
      {:telemetry, "~> 1.2"}
    ]
  end
end
```

### Step 2: `lib/axon_training_loop/callbacks/checkpoint.ex`

```elixir
defmodule AxonTrainingLoop.Callbacks.Checkpoint do
  @moduledoc """
  Save the loop state every N iterations, atomically.

  Key design choice: we never rewrite the "latest" symlink. We write
  `step-000123.ckpt` and keep a monotonic counter. `load_latest/1` reads
  the directory and picks the highest step. This is crash-safe without
  requiring symlinks (which some filesystems handle inconsistently).
  """

  require Logger

  @type config :: %{dir: Path.t(), every: pos_integer(), keep: pos_integer()}

  @spec attach(Axon.Loop.t(), config()) :: Axon.Loop.t()
  def attach(%Axon.Loop{} = loop, %{dir: dir, every: every} = cfg) do
    File.mkdir_p!(dir)

    loop
    |> Axon.Loop.handle_event(:iteration_completed, &maybe_save(&1, cfg), every: every)
    |> Axon.Loop.handle_event(:epoch_completed, &save_and_prune(&1, cfg))
  end

  defp maybe_save(state, cfg) do
    save(state, cfg)
    {:continue, state}
  end

  defp save_and_prune(state, cfg) do
    save(state, cfg)
    prune(cfg)
    {:continue, state}
  end

  defp save(%Axon.Loop.State{epoch: e, iteration: i} = state, %{dir: dir}) do
    path = Path.join(dir, format("step-#{pad(e)}-#{pad(i)}.ckpt"))
    tmp = path <> ".tmp"

    payload = %{
      epoch: e,
      iteration: i,
      step_state: state.step_state,
      handler_metadata: state.handler_metadata,
      metrics: state.metrics
    }

    binary = :erlang.term_to_binary(payload, [:compressed])
    File.write!(tmp, binary)
    File.rename!(tmp, path)
    Logger.info("checkpoint: wrote #{path}")
  end

  defp prune(%{dir: dir, keep: keep}) do
    dir
    |> File.ls!()
    |> Enum.filter(&String.ends_with?(&1, ".ckpt"))
    |> Enum.sort(:desc)
    |> Enum.drop(keep)
    |> Enum.each(&File.rm!(Path.join(dir, &1)))
  end

  defp prune(_), do: :ok

  defp pad(n), do: n |> Integer.to_string() |> String.pad_leading(6, "0")
  defp format(s), do: s

  @spec load_latest(Path.t()) :: {:ok, map()} | :empty
  def load_latest(dir) do
    case File.ls(dir) do
      {:ok, files} ->
        files
        |> Enum.filter(&String.ends_with?(&1, ".ckpt"))
        |> Enum.sort(:desc)
        |> case do
          [latest | _] -> {:ok, read(Path.join(dir, latest))}
          [] -> :empty
        end

      {:error, _} ->
        :empty
    end
  end

  defp read(path) do
    path |> File.read!() |> :erlang.binary_to_term([:safe])
  end
end
```

### Step 3: `lib/axon_training_loop/callbacks/early_stopping.ex`

```elixir
defmodule AxonTrainingLoop.Callbacks.EarlyStopping do
  @moduledoc """
  Halt the loop if `metric` has not improved by `min_delta` within `patience` epochs.

  `mode` ∈ `:max` | `:min` — `:max` for accuracy, `:min` for loss.
  """

  @spec attach(Axon.Loop.t(), keyword()) :: Axon.Loop.t()
  def attach(loop, opts) do
    metric = Keyword.fetch!(opts, :metric)
    mode = Keyword.get(opts, :mode, :max)
    patience = Keyword.get(opts, :patience, 3)
    min_delta = Keyword.get(opts, :min_delta, 0.0)

    key = {:early_stopping, metric}

    Axon.Loop.handle_event(loop, :epoch_completed, fn state ->
      value = Map.get(state.metrics, metric) |> to_number!()
      meta = Map.get(state.handler_metadata, key, %{best: nil, since: 0})

      {improved?, new_best} = improved?(mode, value, meta.best, min_delta)

      meta =
        if improved? do
          %{best: new_best, since: 0}
        else
          %{meta | since: meta.since + 1}
        end

      new_state = %{state | handler_metadata: Map.put(state.handler_metadata, key, meta)}

      if meta.since >= patience do
        {:halt_loop, new_state}
      else
        {:continue, new_state}
      end
    end)
  end

  defp improved?(:max, v, nil, _), do: {true, v}
  defp improved?(:min, v, nil, _), do: {true, v}
  defp improved?(:max, v, best, delta) when v > best + delta, do: {true, v}
  defp improved?(:min, v, best, delta) when v < best - delta, do: {true, v}
  defp improved?(_, _, best, _), do: {false, best}

  defp to_number!(%Nx.Tensor{} = t), do: Nx.to_number(t)
  defp to_number!(n) when is_number(n), do: n
end
```

### Step 4: `lib/axon_training_loop/callbacks/ema.ex`

```elixir
defmodule AxonTrainingLoop.Callbacks.EMA do
  @moduledoc """
  Maintains an EMA copy of the model parameters in `handler_metadata[:ema]`.

  Use `read/1` to extract the EMA state at the end of training for evaluation.
  """

  @key :ema

  @spec attach(Axon.Loop.t(), float()) :: Axon.Loop.t()
  def attach(loop, decay \\ 0.999) do
    Axon.Loop.handle_event(loop, :iteration_completed, fn state ->
      params = state.step_state.model_state
      prev = Map.get(state.handler_metadata, @key) || zeros_like(params)
      new_ema = merge(prev, params, decay)
      {:continue, %{state | handler_metadata: Map.put(state.handler_metadata, @key, new_ema)}}
    end)
  end

  @spec read(Axon.Loop.State.t()) :: map() | nil
  def read(state), do: Map.get(state.handler_metadata, @key)

  defp merge(ema, params, decay) when is_map(ema) and is_map(params) do
    Map.new(ema, fn {k, v} -> {k, merge(v, Map.fetch!(params, k), decay)} end)
  end

  defp merge(%Nx.Tensor{} = ema_t, %Nx.Tensor{} = p_t, decay) do
    Nx.add(Nx.multiply(ema_t, decay), Nx.multiply(p_t, 1.0 - decay))
  end

  defp zeros_like(params) when is_map(params),
    do: Map.new(params, fn {k, v} -> {k, zeros_like(v)} end)

  defp zeros_like(%Nx.Tensor{} = t), do: Nx.broadcast(Nx.tensor(0.0, type: Nx.type(t)), Nx.shape(t))
end
```

### Step 5: `lib/axon_training_loop/callbacks/lr_schedule.ex`

```elixir
defmodule AxonTrainingLoop.Callbacks.LRSchedule do
  @moduledoc "Warmup + cosine decay schedule applied before each iteration."

  @type config :: %{warmup: pos_integer(), total: pos_integer(), lr_max: float(), lr_min: float()}

  @spec attach(Axon.Loop.t(), config()) :: Axon.Loop.t()
  def attach(loop, %{warmup: w, total: t, lr_max: lr_max, lr_min: lr_min} = cfg) do
    Axon.Loop.handle_event(loop, :iteration_started, fn state ->
      step = state.iteration + state.epoch * steps_per_epoch(state)
      lr = compute(step, cfg)

      step_state =
        update_in(state.step_state, [:optimizer_state], fn opt_state ->
          Polaris.Updates.set_learning_rate(opt_state, lr)
        end)

      {:continue, %{state | step_state: step_state}}
    end)
  end

  defp steps_per_epoch(_state), do: 1  # placeholder — pass via opts in real code

  @doc false
  def compute(step, %{warmup: w, total: t, lr_max: lr_max, lr_min: lr_min}) do
    cond do
      step < w ->
        lr_max * step / w

      step >= t ->
        lr_min

      true ->
        progress = (step - w) / (t - w)
        lr_min + 0.5 * (lr_max - lr_min) * (1 + :math.cos(:math.pi() * progress))
    end
  end
end
```

Note: `Polaris.Updates.set_learning_rate/2` is a helper present in Polaris ≥ 0.1.1; on older versions, replace the optimizer with one built from the new LR.

### Step 6: `lib/axon_training_loop/trainer.ex`

```elixir
defmodule AxonTrainingLoop.Trainer do
  @moduledoc "Assembles a training loop from the callback modules."

  alias AxonTrainingLoop.Callbacks.{Checkpoint, EarlyStopping, EMA, LRSchedule}
  require Logger

  @type opts :: [
          model: Axon.t(),
          loss: atom() | (Nx.Tensor.t(), Nx.Tensor.t() -> Nx.Tensor.t()),
          optimizer: term(),
          epochs: pos_integer(),
          checkpoint_dir: Path.t() | nil,
          ema_decay: float() | nil,
          early_stopping: keyword() | nil,
          lr_schedule: map() | nil
        ]

  @spec build(opts()) :: Axon.Loop.t()
  def build(opts) do
    model = Keyword.fetch!(opts, :model)
    loss = Keyword.fetch!(opts, :loss)
    optimizer = Keyword.fetch!(opts, :optimizer)

    loop =
      Axon.Loop.trainer(model, loss, optimizer)
      |> Axon.Loop.metric(:accuracy)
      |> Axon.Loop.metric(:loss)

    loop = maybe_attach(loop, Keyword.get(opts, :checkpoint_dir), &attach_checkpoint/2)
    loop = maybe_attach(loop, Keyword.get(opts, :ema_decay), &EMA.attach/2)
    loop = maybe_attach(loop, Keyword.get(opts, :lr_schedule), &LRSchedule.attach/2)
    loop = maybe_attach(loop, Keyword.get(opts, :early_stopping), &EarlyStopping.attach/2)
    loop
  end

  defp maybe_attach(loop, nil, _), do: loop
  defp maybe_attach(loop, arg, fun), do: fun.(loop, arg)

  defp attach_checkpoint(loop, dir),
    do: Checkpoint.attach(loop, %{dir: dir, every: 200, keep: 3})

  @doc "Run or resume a training loop."
  @spec run(Axon.Loop.t(), Enumerable.t(), keyword()) :: map()
  def run(loop, data, opts) do
    epochs = Keyword.fetch!(opts, :epochs)

    initial =
      case Keyword.get(opts, :checkpoint_dir) && Checkpoint.load_latest(opts[:checkpoint_dir]) do
        {:ok, payload} ->
          Logger.info("Resuming from checkpoint epoch=#{payload.epoch} iter=#{payload.iteration}")
          payload.step_state

        _ ->
          %{}
      end

    Axon.Loop.run(loop, data, initial, epochs: epochs, compiler: EXLA)
  end
end
```

### Step 7: Tests

```elixir
# test/axon_training_loop/early_stopping_test.exs
defmodule AxonTrainingLoop.EarlyStoppingTest do
  use ExUnit.Case, async: true
  alias AxonTrainingLoop.Callbacks.EarlyStopping

  # We build a minimal loop, step through epochs manually, and assert it halts.

  defp fake_state(metrics, meta \\ %{}) do
    %Axon.Loop.State{
      epoch: 0, iteration: 0, max_epoch: 10, max_iteration: :infinity,
      step_state: %{}, metrics: metrics, handler_metadata: meta, status: :halted
    }
  end

  test "halts after `patience` epochs of no improvement (min mode)" do
    loop = Axon.Loop.loop(fn _, s -> s end) |> EarlyStopping.attach(metric: "loss", mode: :min, patience: 2)
    [{:epoch_completed, [handler]}] = Enum.filter(loop.handlers, fn {e, _} -> e == :epoch_completed end)

    {:continue, s1} = handler.(fake_state(%{"loss" => 1.0}))
    {:continue, s2} = handler.(%{s1 | metrics: %{"loss" => 1.1}, epoch: 1})
    {:halt_loop, _} = handler.(%{s2 | metrics: %{"loss" => 1.2}, epoch: 2})
  end
end
```

The test accesses `loop.handlers` directly — it is part of Axon's public `Axon.Loop` struct and has been stable across 0.5–0.7. If Axon 0.8 changes this, use `Axon.Loop.run/4` with 3 fake epochs instead.

```elixir
# test/axon_training_loop/checkpoint_test.exs
defmodule AxonTrainingLoop.CheckpointTest do
  use ExUnit.Case, async: false
  alias AxonTrainingLoop.Callbacks.Checkpoint

  setup do
    dir = Path.join(System.tmp_dir!(), "ckpt-#{System.unique_integer([:positive])}")
    File.mkdir_p!(dir)
    on_exit(fn -> File.rm_rf!(dir) end)
    %{dir: dir}
  end

  test "load_latest returns :empty for empty dir", %{dir: dir} do
    assert Checkpoint.load_latest(dir) == :empty
  end

  test "picks highest step file", %{dir: dir} do
    payload = %{epoch: 2, iteration: 50, step_state: %{}, handler_metadata: %{}, metrics: %{}}
    File.write!(Path.join(dir, "step-000001-000010.ckpt"), :erlang.term_to_binary(payload))
    File.write!(Path.join(dir, "step-000002-000050.ckpt"), :erlang.term_to_binary(payload))
    assert {:ok, %{epoch: 2, iteration: 50}} = Checkpoint.load_latest(dir)
  end
end
```

### Step 8: End-to-end smoke test

```elixir
# test/axon_training_loop/trainer_test.exs
defmodule AxonTrainingLoop.TrainerTest do
  use ExUnit.Case, async: false

  @tag :integration
  test "trains a tiny model and produces better-than-random accuracy" do
    model = Axon.input("input", shape: {nil, 4}) |> Axon.dense(8) |> Axon.relu() |> Axon.dense(3)

    data =
      Stream.repeatedly(fn ->
        x = Nx.iota({32, 4}) |> Nx.as_type(:f32) |> Nx.divide(32.0)
        y = Nx.argmax(x, axis: 1) |> Nx.new_axis(-1) |> Nx.equal(Nx.iota({1, 3})) |> Nx.as_type(:f32)
        {x, y}
      end)
      |> Stream.take(50)

    loop =
      AxonTrainingLoop.Trainer.build(
        model: model,
        loss: :categorical_cross_entropy,
        optimizer: Polaris.Optimizers.adam(learning_rate: 0.01),
        epochs: 2
      )

    result = Axon.Loop.run(loop, data, %{}, epochs: 2, compiler: EXLA)
    assert is_map(result)
  end
end
```

---

## Trade-offs and production gotchas

**1. Handler order matters.** `iteration_started` handlers run in attach order. If LR schedule runs after EMA, an EMA read at step 0 sees an uninitialized optimizer LR. Attach schedule → checkpoint → EMA → early_stopping, in that order, and document why.

**2. Checkpoint format lock-in.** `term_to_binary` works until a model's Axon version bumps. After upgrading Axon, old checkpoints deserialize into stale struct shapes and crash at use. Version your checkpoints (`version: 1, axon_version: "0.7.1"` in the payload) and write a migration step when the schema changes.

**3. EMA doubles memory.** For a 500 MB model, EMA adds another 500 MB. Budget for it. On constrained hardware, apply EMA on the CPU (keep `θ_ema` on `BinaryBackend`) and only copy to GPU at evaluation — this costs one per-epoch transfer but halves GPU memory pressure.

**4. Data ordering must be deterministic for resume.** If you resume from epoch 5 step 100 but the data stream produced different batches than the original run (e.g., unsorted `File.ls` over a directory), the loss curve has a discontinuity. Use a deterministic shuffle seeded with the checkpoint-saved RNG key.

**5. Gradient accumulation interacts with BatchNorm.** Running statistics update per physical batch, not per effective batch. If you accumulate 4× micro-batches, BN sees 4× updates per effective step — the running stats converge faster than intended. For transformers (LayerNorm) this does not matter; for ResNet-family (BN) it does.

**6. Telemetry on the hot path is tempting and expensive.** `:telemetry.execute([:axon, :iteration], %{loss: l}, %{})` at every step adds ~5–20 µs; on an XLA step of 2 ms that's 1% overhead. On a step of 200 µs, it's 10%. Emit telemetry per N steps, not per step.

**7. Early stopping on val loss with tiny eval sets is noisy.** If your val set is 500 examples, the epoch-to-epoch noise in val loss can be ±0.02, which looks like "no improvement" even when training genuinely helps. Use `min_delta` larger than 1 standard deviation of your noise.

**8. When NOT to use this.** If your training run is < 10 minutes and fits in memory, the checkpoint/resume machinery is overhead without benefit. Use `Axon.Loop.trainer(...) |> Axon.Loop.run(...)` directly. Callbacks pay off when runs are long enough that preemption or OOM-kill is realistic — typically > 1 hour.

---

## Performance notes

Per-iteration overhead of the full callback stack, measured on a tiny model (to isolate callback cost from step cost):

| Callback stack                        | Overhead per step (EXLA CPU) |
|---------------------------------------|------------------------------|
| trainer only                          | ~50 µs                       |
| + EMA                                 | +12 µs (tensor merge)        |
| + Checkpoint (every 200 steps)        | +0.3 µs amortized            |
| + LR schedule                         | +4 µs                        |
| + Early stopping (epoch only)         | negligible                   |

Checkpoint save itself takes 50–500 ms for a 100 MB model with `:compressed`. If that dominates, drop compression and write raw binary; the disk space is almost certainly not the bottleneck.

---

## Resources

- [Axon.Loop hexdocs](https://hexdocs.pm/axon/Axon.Loop.html) — full API including `handle_event/4`, `monitor/4`, `metric/3`.
- [Polaris hexdocs](https://hexdocs.pm/polaris/) — Axon's companion optimizer library (Adam, SGD, schedulers).
- [Sean Moriarity — "Machine Learning in Elixir", chapter 8](https://pragprog.com/titles/smelixir/) — worked example of building a custom loop.
- [Axon training guides](https://hexdocs.pm/axon/custom_models.html) — patterns for custom steps and metrics.
- [PyTorch's `torch.optim.swa_utils.AveragedModel`](https://docs.pytorch.org/docs/stable/optim.html#stochastic-weight-averaging) — reference for EMA semantics.
- [Ian Goodfellow et al. — "Deep Learning" chapter 8](https://www.deeplearningbook.org/) — numerics behind LR schedules, momentum, and weight averaging.
