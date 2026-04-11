# Typespecs and Dialyzer

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system has grown to a dozen modules. The scheduler calls the worker pool,
the worker pool calls the registry, the registry is called from batch runners. At this
scale, a mismatched argument type (passing a worker_id atom where a string is expected,
or calling a function with the wrong arity) becomes harder to catch by inspection alone.

`@spec`, `@type`, `@typep`, and Dialyzer give you machine-readable documentation that
doubles as a static analysis tool. This exercise adds typespecs throughout the existing
modules and configures Dialyzer to run as part of the CI pipeline.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── scheduler.ex         # ← you add specs to this (new module)
│       └── [all previous modules]
├── test/
│   └── task_queue/
│       └── typespecs_test.exs   # given tests — must pass without modification
└── mix.exs                      # ← add dialyxir
```

---

## Why typespecs matter beyond documentation

`@spec` documents what types a function accepts and returns. Alone, it has no runtime
effect — Elixir does not enforce types at runtime. Its value comes from:

1. **Reader communication**: `@spec route(job_map()) :: module()` tells the caller exactly
   what to pass and what to expect. No need to read the implementation.

2. **Dialyzer analysis**: Dialyzer performs success-typing analysis — it determines what
   types a function can actually produce and detects when callers pass impossible arguments
   or ignore possible error returns. It finds real bugs that tests often miss.

3. **IDE tooling**: ElixirLS uses specs for completion and inline type hints.

The key insight: Dialyzer finds bugs without running the code. It found the bug in your
`route/1` function where you forgot to handle `nil` payloads before you wrote a test for it.

---

## The business problem

`TaskQueue.Scheduler` is the top-level coordinator. It:
1. Checks the queue depth.
2. Decides whether to scale workers up or down.
3. Dispatches the next batch of jobs to available workers.
4. Returns a structured `%SchedulerResult{}` with the outcome.

The module must be fully specced: all public functions with `@spec`, all types with
`@type`, and all opaque types with `@opaque`. Dialyzer must pass clean.

---

## Implementation

### Step 1: Add `dialyxir` to `mix.exs`

```elixir
defp deps do
  [
    {:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}
  ]
end
```

```bash
mix deps.get
mix dialyzer  # First run builds the PLT — takes a few minutes
```

### Step 2: `lib/task_queue/scheduler.ex`

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Coordinates the task_queue: monitors queue depth, scales workers,
  and dispatches jobs to available workers.
  """

  require Logger

  # ---------------------------------------------------------------------------
  # Type definitions — must be precise enough for Dialyzer to find real bugs
  # ---------------------------------------------------------------------------

  @type worker_id :: String.t()
  @type job_id :: String.t()

  @type scaling_decision :: :scale_up | :scale_down | :hold

  @type dispatch_outcome :: %{
    required(:job_id) => job_id(),
    required(:worker_id) => worker_id(),
    required(:result) => {:ok, any()} | {:error, any()}
  }

  @type scheduler_result :: %{
    required(:queue_depth) => non_neg_integer(),
    required(:active_workers) => non_neg_integer(),
    required(:scaling_decision) => scaling_decision(),
    required(:dispatched) => [dispatch_outcome()]
  }

  # Module attribute constants — not types, but affect type inference
  @min_workers 1
  @max_workers 10
  @scale_up_threshold 5
  @scale_down_threshold 1

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Runs one scheduling cycle: inspect, scale, dispatch.
  Returns a complete summary of what happened.
  """
  @spec run_cycle() :: scheduler_result()
  def run_cycle do
    queue_depth = TaskQueue.QueueServer.size()
    active_workers = TaskQueue.WorkerPool.count()

    decision = decide_scaling(queue_depth, active_workers)
    apply_scaling(decision, active_workers)

    dispatched = dispatch_batch(queue_depth)

    %{
      queue_depth: queue_depth,
      active_workers: TaskQueue.WorkerPool.count(),
      scaling_decision: decision,
      dispatched: dispatched
    }
  end

  @doc """
  Determines whether to scale up, scale down, or hold based on queue depth
  and current worker count.
  """
  @spec decide_scaling(non_neg_integer(), non_neg_integer()) :: scaling_decision()
  def decide_scaling(queue_depth, active_workers)
      when queue_depth > @scale_up_threshold and active_workers < @max_workers do
    :scale_up
  end

  def decide_scaling(queue_depth, active_workers)
      when queue_depth <= @scale_down_threshold and active_workers > @min_workers do
    :scale_down
  end

  def decide_scaling(_queue_depth, _active_workers), do: :hold

  @doc """
  Applies the scaling decision. Returns the number of workers started or stopped.
  """
  @spec apply_scaling(scaling_decision(), non_neg_integer()) :: integer()
  def apply_scaling(:scale_up, _current) do
    worker_id = generate_worker_id()
    case TaskQueue.WorkerPool.start_worker(worker_id) do
      {:ok, _pid} ->
        Logger.info("Scheduler: scaled up, started worker #{worker_id}")
        1
      {:error, reason} ->
        Logger.error("Scheduler: scale up failed: #{inspect(reason)}")
        0
    end
  end

  def apply_scaling(:scale_down, current) when current > @min_workers do
    # HINT: get the first worker ID from WorkerPool, stop it, return -1
    # If no workers, return 0
    # TODO: implement
  end

  def apply_scaling(:hold, _current), do: 0

  # Required for exhaustiveness — Dialyzer enforces this
  def apply_scaling(_, _), do: 0

  @doc """
  Dispatches up to `batch_size` jobs to available workers.
  Returns a list of dispatch outcomes.
  """
  @spec dispatch_batch(non_neg_integer()) :: [dispatch_outcome()]
  def dispatch_batch(0), do: []

  def dispatch_batch(batch_size) do
    worker_ids = TaskQueue.DynamicWorker.list_ids()

    if worker_ids == [] do
      []
    else
      # Take min(batch_size, worker_count) items
      limit = min(batch_size, length(worker_ids))

      worker_ids
      |> Enum.take(limit)
      |> Enum.map(fn worker_id ->
        result = TaskQueue.DynamicWorker.process_job(worker_id)
        # HINT: return a dispatch_outcome map
        # TODO: implement
      end)
      # Filter workers that had no job available
      |> Enum.reject(&is_nil/1)
    end
  end

  # ---------------------------------------------------------------------------
  # Private
  # ---------------------------------------------------------------------------

  @spec generate_worker_id() :: worker_id()
  defp generate_worker_id do
    "auto_worker_#{:crypto.strong_rand_bytes(4) |> Base.url_encode64(padding: false)}"
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/typespecs_test.exs
defmodule TaskQueue.TypespecsTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Scheduler

  setup do
    # Ensure a clean system state
    case Process.whereis(TaskQueue.WorkerPool) do
      nil -> :ok
      _ ->
        for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
    end
    Process.sleep(50)
    :ok
  end

  describe "decide_scaling/2" do
    test "returns :scale_up when queue is deep and workers are below max" do
      assert :scale_up = Scheduler.decide_scaling(10, 2)
    end

    test "returns :scale_down when queue is shallow and workers above min" do
      assert :scale_down = Scheduler.decide_scaling(0, 3)
    end

    test "returns :hold when queue is moderate" do
      assert :hold = Scheduler.decide_scaling(3, 5)
    end

    test "returns :hold when at max workers regardless of queue depth" do
      assert :hold = Scheduler.decide_scaling(100, 10)
    end

    test "returns :hold when at min workers regardless of queue depth" do
      assert :hold = Scheduler.decide_scaling(0, 1)
    end
  end

  describe "run_cycle/0 return type" do
    test "returns a map with required keys" do
      result = Scheduler.run_cycle()
      assert is_map(result)
      assert Map.has_key?(result, :queue_depth)
      assert Map.has_key?(result, :active_workers)
      assert Map.has_key?(result, :scaling_decision)
      assert Map.has_key?(result, :dispatched)
    end

    test ":scaling_decision is one of the valid atoms" do
      result = Scheduler.run_cycle()
      assert result.scaling_decision in [:scale_up, :scale_down, :hold]
    end

    test ":dispatched is a list" do
      result = Scheduler.run_cycle()
      assert is_list(result.dispatched)
    end
  end

  describe "apply_scaling/2" do
    test ":hold returns 0" do
      assert 0 = Scheduler.apply_scaling(:hold, 5)
    end

    test ":scale_up with room returns 1 and a new worker appears" do
      initial = TaskQueue.WorkerPool.count()
      result = Scheduler.apply_scaling(:scale_up, initial)
      Process.sleep(50)
      assert result in [0, 1]
      # Clean up
      for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
    end
  end
end
```

### Step 4: Run Dialyzer

```bash
mix dialyzer
```

Dialyzer should pass with zero warnings. If it reports a warning, fix the code rather
than adding `# dialyzer:ignore` suppressions — suppressions hide real bugs.

### Step 5: Run the tests

```bash
mix test test/task_queue/typespecs_test.exs --trace
```

---

## Trade-off analysis

| Aspect | `@spec` + Dialyzer | Runtime type validation | No type annotations |
|--------|-------------------|-----------------------|---------------------|
| Bug detection | Before tests run | At runtime | Only when test covers the path |
| Performance overhead | None — compile-time only | Non-trivial per call | None |
| Maintenance cost | Medium — keep specs in sync | High | Low initially |
| Finds unreachable clauses | Yes | No | No |
| Finds wrong return type usage | Yes | No — only catches current call | No |
| IDE support | Full — completion, hover | None | Partial |

Reflection question: Dialyzer uses **success typing** — it infers what types a function
can successfully return and flags callers that would never succeed. This means Dialyzer
does **not** catch all type errors — only provably wrong ones. What kinds of bugs will
Dialyzer find that tests miss, and what kinds will Dialyzer miss that tests catch?

---

## Common production mistakes

**1. Writing `@spec` after the fact as documentation only**
A `@spec` that says `@spec get(String.t()) :: map()` when the function can actually
return `nil` is a lie. Dialyzer will find this — the spec says one thing, the
implementation does another.

**2. Using `any()` everywhere to silence Dialyzer**
`@spec my_fn(any()) :: any()` defeats the purpose. Be as precise as possible. Use
union types (`atom() | binary()`) instead of `any()` when you know the possible values.

**3. Forgetting to re-run Dialyzer after refactoring**
The PLT (Persistent Lookup Table) Dialyzer builds caches type information. After major
refactoring, run `mix dialyzer --no-check` to force a full reanalysis. Stale PLT data
produces false negatives.

**4. Overly broad `@type` definitions**
```elixir
# Too broad — does not help Dialyzer
@type job :: map()

# Specific — Dialyzer can find mismatches
@type job :: %{
  required(:id) => String.t(),
  required(:type) => atom(),
  required(:payload) => any()
}
```

---

## Resources

- [Typespecs — HexDocs](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir — GitHub](https://github.com/jeremyjh/dialyxir) — the Mix wrapper for Dialyzer
- [Dialyzer — Erlang/OTP](https://www.erlang.org/doc/man/dialyzer.html) — the underlying tool documentation
- [Learn You Some Erlang: Type Specifications](https://learnyousomeerlang.com/dialyzer) — the best explanation of success typing
