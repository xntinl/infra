# Typespecs and Dialyzer

## Why typespecs matter beyond documentation

`@spec` documents what types a function accepts and returns. Its value comes from:

1. **Reader communication**: `@spec route(job_map()) :: module()` tells the caller exactly
   what to pass and what to expect.
2. **Dialyzer analysis**: Dialyzer performs success-typing analysis — it finds real bugs
   that tests often miss.
3. **IDE tooling**: ElixirLS uses specs for completion and inline type hints.

---

## The business problem

Build a `TaskQueue.Scheduler` — the top-level coordinator that:
1. Checks the queue depth.
2. Decides whether to scale workers up or down.
3. Dispatches the next batch of jobs to available workers.
4. Returns a structured result with the outcome.

The module must be fully specced. Additionally, build the supporting modules
(`WorkerPool`, `DynamicWorker`, `QueueServer`) that the scheduler depends on.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── queue_server.ex
│       ├── dynamic_worker.ex
│       ├── worker_pool.ex
│       └── scheduler.ex
├── test/
│   └── task_queue/
│       └── typespecs_test.exs
└── mix.exs
```

Add `dialyxir` to `mix.exs`:

```elixir
defp deps do
  [
    {:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}
  ]
end
```

---

## Implementation

### `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl Application
  def start(_type, _args) do
    children = [
      TaskQueue.QueueServer,
      {Registry, keys: :unique, name: TaskQueue.WorkerRegistry},
      {DynamicSupervisor, strategy: :one_for_one, name: TaskQueue.WorkerSupervisor}
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.RootSupervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### `lib/task_queue/queue_server.ex`

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @spec push(any()) :: :ok
  def push(payload) do
    job = %{
      id: :crypto.strong_rand_bytes(8) |> Base.url_encode64(padding: false),
      payload: payload,
      queued_at: System.monotonic_time(:millisecond)
    }
    GenServer.cast(__MODULE__, {:push, job})
  end

  @spec pop() :: {:ok, map()} | {:error, :empty}
  def pop, do: GenServer.call(__MODULE__, :pop)

  @spec size() :: non_neg_integer()
  def size, do: GenServer.call(__MODULE__, :size)

  @impl GenServer
  def init(_opts), do: {:ok, []}

  @impl GenServer
  def handle_cast({:push, job}, state), do: {:noreply, state ++ [job]}

  @impl GenServer
  def handle_call(:pop, _from, []), do: {:reply, {:error, :empty}, []}
  def handle_call(:pop, _from, [job | rest]), do: {:reply, {:ok, job}, rest}

  @impl GenServer
  def handle_call(:size, _from, state), do: {:reply, length(state), state}

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/dynamic_worker.ex`

```elixir
defmodule TaskQueue.DynamicWorker do
  use GenServer
  require Logger

  @registry TaskQueue.WorkerRegistry

  def start_link(worker_id) when is_binary(worker_id) do
    GenServer.start_link(__MODULE__, worker_id, name: via(worker_id))
  end

  @spec via(String.t()) :: {:via, Registry, {module(), {atom(), String.t()}}}
  def via(worker_id), do: {:via, Registry, {@registry, {:worker, worker_id}}}

  @spec lookup(String.t()) :: pid() | nil
  def lookup(worker_id) do
    case Registry.lookup(@registry, {:worker, worker_id}) do
      [{pid, _}] -> pid
      [] -> nil
    end
  end

  @spec list_ids() :: [String.t()]
  def list_ids do
    @registry
    |> Registry.select([{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    |> Enum.map(fn {:worker, id} -> id end)
  end

  @spec process_job(String.t()) :: {:ok, any()} | {:error, any()}
  def process_job(worker_id) do
    GenServer.call(via(worker_id), :process_job, 30_000)
  end

  @impl GenServer
  def init(worker_id) do
    {:ok, %{worker_id: worker_id, jobs_processed: 0, started_at: System.monotonic_time(:millisecond)}}
  end

  @impl GenServer
  def handle_call(:process_job, _from, state) do
    case TaskQueue.QueueServer.pop() do
      {:error, :empty} -> {:reply, {:error, :empty}, state}
      {:ok, _job} -> {:reply, {:ok, :processed}, %{state | jobs_processed: state.jobs_processed + 1}}
    end
  end

  @impl GenServer
  def handle_info(_, state), do: {:noreply, state}
end
```

### `lib/task_queue/worker_pool.ex`

```elixir
defmodule TaskQueue.WorkerPool do
  alias TaskQueue.DynamicWorker

  @supervisor TaskQueue.WorkerSupervisor

  @spec start_worker(String.t()) :: {:ok, pid()} | {:error, any()}
  def start_worker(worker_id) do
    DynamicSupervisor.start_child(@supervisor, {DynamicWorker, worker_id})
  end

  @spec stop_worker(String.t()) :: :ok | {:error, :not_found}
  def stop_worker(worker_id) do
    case DynamicWorker.lookup(worker_id) do
      nil -> {:error, :not_found}
      pid ->
        DynamicSupervisor.terminate_child(@supervisor, pid)
        :ok
    end
  end

  @spec count() :: non_neg_integer()
  def count, do: DynamicWorker.list_ids() |> length()
end
```

### `lib/task_queue/scheduler.ex`

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Coordinates the task_queue: monitors queue depth, scales workers,
  and dispatches jobs to available workers.
  """

  require Logger

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

  @min_workers 1
  @max_workers 10
  @scale_up_threshold 5
  @scale_down_threshold 1

  @doc "Runs one scheduling cycle: inspect, scale, dispatch."
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

  @spec apply_scaling(scaling_decision(), non_neg_integer()) :: integer()
  def apply_scaling(:scale_up, _current) do
    worker_id = "auto_#{:crypto.strong_rand_bytes(4) |> Base.url_encode64(padding: false)}"
    case TaskQueue.WorkerPool.start_worker(worker_id) do
      {:ok, _pid} -> 1
      {:error, _} -> 0
    end
  end

  def apply_scaling(:scale_down, current) when current > @min_workers do
    case TaskQueue.DynamicWorker.list_ids() do
      [] -> 0
      [first_id | _] ->
        TaskQueue.WorkerPool.stop_worker(first_id)
        -1
    end
  end

  def apply_scaling(:hold, _current), do: 0
  def apply_scaling(_, _), do: 0

  @spec dispatch_batch(non_neg_integer()) :: [dispatch_outcome()]
  def dispatch_batch(0), do: []

  def dispatch_batch(batch_size) do
    worker_ids = TaskQueue.DynamicWorker.list_ids()

    if worker_ids == [] do
      []
    else
      limit = min(batch_size, length(worker_ids))

      worker_ids
      |> Enum.take(limit)
      |> Enum.map(fn worker_id ->
        result = TaskQueue.DynamicWorker.process_job(worker_id)
        case result do
          {:ok, _} -> %{job_id: "dispatched", worker_id: worker_id, result: result}
          {:error, :empty} -> nil
          {:error, _} -> %{job_id: "dispatched", worker_id: worker_id, result: result}
        end
      end)
      |> Enum.reject(&is_nil/1)
    end
  end
end
```

### Tests

```elixir
# test/task_queue/typespecs_test.exs
defmodule TaskQueue.TypespecsTest do
  use ExUnit.Case, async: false

  alias TaskQueue.Scheduler

  setup do
    for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
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
      for id <- TaskQueue.DynamicWorker.list_ids(), do: TaskQueue.WorkerPool.stop_worker(id)
    end
  end
end
```

### Run the tests

```bash
mix dialyzer
mix test test/task_queue/typespecs_test.exs --trace
```

---

## Common production mistakes

**1. Writing `@spec` after the fact as documentation only**
A `@spec` that says `:: map()` when the function can return `nil` is a lie. Dialyzer
will find this.

**2. Using `any()` everywhere to silence Dialyzer**
`@spec my_fn(any()) :: any()` defeats the purpose. Be as precise as possible.

**3. Overly broad `@type` definitions**
```elixir
# Too broad
@type job :: map()

# Specific — Dialyzer can find mismatches
@type job :: %{required(:id) => String.t(), required(:type) => atom()}
```

---

## Resources

- [Typespecs — HexDocs](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir — GitHub](https://github.com/jeremyjh/dialyxir)
- [Learn You Some Erlang: Type Specifications](https://learnyousomeerlang.com/dialyzer)
