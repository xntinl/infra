# Enumerable and Collectable Protocols

## Goal

Build a `task_queue` FIFO job queue struct that implements `Enumerable` (enabling all `Enum.*` and `Stream.*` functions) and `Collectable` (enabling `Enum.into/2` and `for ... into:`). Learn how these protocols work internally and how to implement them for custom data structures.

---

## The two protocols

**`Enumerable`** enables all `Enum` and `Stream` functions. It requires four callbacks: `reduce/3`, `count/1`, `member?/2`, and `slice/1`. The simplest approach: convert to a list and delegate to `Enumerable.List`.

**`Collectable`** enables `Enum.into/2` and `for ... into:` comprehensions. It requires one callback: `into/1` which returns `{initial_acc, collector_fun}`. The collector function receives `(accumulator, command)` where command is `{:cont, element}`, `:done`, or `:halt`.

---

## Implementation

### Step 1: `mix.exs`

```elixir
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/job_queue.ex` -- custom queue with protocol implementations

The `JobQueue` struct wraps an Erlang `:queue` (a double-ended queue with O(1) amortized push/pop). The `to_list/1` function converts it to a list for the `Enumerable` implementation.

The `Enumerable` implementation delegates to `Enumerable.List.reduce/3` which correctly handles all three accumulator signals (`{:cont, acc}`, `{:halt, acc}`, `{:suspend, acc}`). This means `Enum.take(queue, 2)` stops after two elements instead of traversing the entire queue. `count/1` returns `{:ok, size}` for O(1) counting.

The `Collectable` implementation's collector function takes `(accumulator, command)` -- note that accumulator is first and the command tuple is second. This is a common source of bugs when the arguments are swapped.

```elixir
defmodule TaskQueue.JobQueue do
  @moduledoc """
  An in-memory FIFO job queue implemented as a struct.

  Implements `Enumerable` to support all `Enum.*` and `Stream.*` operations.
  Implements `Collectable` to support `Enum.into/2` and `for ... into:`.
  """

  defstruct jobs: :queue.new()

  @type t :: %__MODULE__{jobs: :queue.queue()}

  @doc """
  Creates an empty queue.
  """
  @spec new() :: t()
  def new, do: %__MODULE__{}

  @doc """
  Adds a job to the back of the queue.
  """
  @spec push(t(), map()) :: t()
  def push(%__MODULE__{jobs: q} = queue, job) when is_map(job) do
    %{queue | jobs: :queue.in(job, q)}
  end

  @doc """
  Removes and returns the front job, or `{:error, :empty}`.
  """
  @spec pop(t()) :: {map(), t()} | {:error, :empty}
  def pop(%__MODULE__{jobs: q} = queue) do
    case :queue.out(q) do
      {{:value, job}, new_q} -> {job, %{queue | jobs: new_q}}
      {:empty, _}            -> {:error, :empty}
    end
  end

  @doc """
  Returns the number of jobs in the queue.
  """
  @spec size(t()) :: non_neg_integer()
  def size(%__MODULE__{jobs: q}), do: :queue.len(q)

  @doc false
  def to_list(%__MODULE__{jobs: q}), do: :queue.to_list(q)
end

defimpl Enumerable, for: TaskQueue.JobQueue do
  def reduce(queue, acc, fun) do
    Enumerable.List.reduce(TaskQueue.JobQueue.to_list(queue), acc, fun)
  end

  def count(queue) do
    {:ok, TaskQueue.JobQueue.size(queue)}
  end

  def member?(queue, element) do
    {:ok, element in TaskQueue.JobQueue.to_list(queue)}
  end

  def slice(queue) do
    list = TaskQueue.JobQueue.to_list(queue)
    size = length(list)
    {:ok, size, &Enumerable.List.slice(list, &1, &2, size)}
  end
end

defimpl Collectable, for: TaskQueue.JobQueue do
  def into(initial_queue) do
    collector_fun = fn
      queue, {:cont, job} -> TaskQueue.JobQueue.push(queue, job)
      queue, :done        -> queue
      _, :halt            -> :ok
    end

    {initial_queue, collector_fun}
  end
end
```

### Step 3: Tests

```elixir
# test/task_queue/protocols_test.exs
defmodule TaskQueue.ProtocolsTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobQueue

  describe "Enumerable -- basic operations" do
    setup do
      queue =
        JobQueue.new()
        |> JobQueue.push(%{type: "send_email", status: :pending})
        |> JobQueue.push(%{type: "send_sms", status: :pending})
        |> JobQueue.push(%{type: "send_email", status: :failed})

      {:ok, queue: queue}
    end

    test "Enum.count/1 returns number of jobs", %{queue: queue} do
      assert Enum.count(queue) == 3
    end

    test "Enum.count/2 with predicate counts matching jobs", %{queue: queue} do
      assert Enum.count(queue, fn j -> j.type == "send_email" end) == 2
    end

    test "Enum.filter/2 returns matching jobs", %{queue: queue} do
      failed = Enum.filter(queue, fn j -> j.status == :failed end)
      assert length(failed) == 1
      assert hd(failed).type == "send_email"
    end

    test "Enum.map/2 transforms jobs", %{queue: queue} do
      types = Enum.map(queue, & &1.type)
      assert types == ["send_email", "send_sms", "send_email"]
    end

    test "Enum.any?/2 works", %{queue: queue} do
      assert Enum.any?(queue, fn j -> j.status == :failed end)
      refute Enum.any?(queue, fn j -> j.type == "nonexistent" end)
    end

    test "Enum.member?/2 checks containment", %{queue: queue} do
      job = %{type: "send_email", status: :pending}
      assert Enum.member?(queue, job)
      refute Enum.member?(queue, %{type: "unknown"})
    end

    test "Enum.to_list/1 returns all jobs in order", %{queue: queue} do
      list = Enum.to_list(queue)
      assert length(list) == 3
      assert hd(list).type == "send_email"
    end

    test "Enum.reduce/3 works", %{queue: queue} do
      count = Enum.reduce(queue, 0, fn _, acc -> acc + 1 end)
      assert count == 3
    end

    test "Stream.filter works (lazy evaluation)", %{queue: queue} do
      stream = Stream.filter(queue, fn j -> j.type == "send_email" end)
      result = Enum.to_list(stream)
      assert length(result) == 2
    end
  end

  describe "Collectable -- Enum.into" do
    test "Enum.into/2 collects a list of jobs into a queue" do
      jobs = [
        %{type: "noop", status: :pending},
        %{type: "echo", status: :pending}
      ]

      queue = Enum.into(jobs, JobQueue.new())

      assert JobQueue.size(queue) == 2
      assert Enum.count(queue) == 2
    end

    test "for ... into: works with a JobQueue" do
      source = [%{type: "a"}, %{type: "b"}, %{type: "c"}]

      queue =
        for job <- source, into: JobQueue.new() do
          Map.put(job, :processed, true)
        end

      assert JobQueue.size(queue) == 3
      assert Enum.all?(queue, fn j -> Map.get(j, :processed) == true end)
    end

    test "Enum.into with filter -- only passing jobs collected" do
      all_jobs = [
        %{type: "send_email", status: :pending},
        %{type: "send_email", status: :failed},
        %{type: "noop", status: :pending}
      ]

      pending_queue =
        all_jobs
        |> Enum.filter(fn j -> j.status == :pending end)
        |> Enum.into(JobQueue.new())

      assert JobQueue.size(pending_queue) == 2
    end

    test "copying from one queue to another with Enum.into" do
      source =
        JobQueue.new()
        |> JobQueue.push(%{type: "a"})
        |> JobQueue.push(%{type: "b"})

      dest = Enum.into(source, JobQueue.new())

      assert JobQueue.size(dest) == 2
    end
  end

  describe "Enumerable -- empty queue" do
    test "Enum.count on empty queue returns 0" do
      assert Enum.count(JobQueue.new()) == 0
    end

    test "Enum.to_list on empty queue returns []" do
      assert Enum.to_list(JobQueue.new()) == []
    end

    test "Enum.any? on empty queue returns false" do
      refute Enum.any?(JobQueue.new(), fn _ -> true end)
    end
  end
end
```

### Step 4: Run

```bash
mix test test/task_queue/protocols_test.exs --trace
```

---

## Trade-off analysis

| Approach | Supports `Enum.*` | Supports `Enum.into` | Lazy streams | Implementation effort |
|----------|------------------|---------------------|--------------|----------------------|
| `defimpl Enumerable` | yes | no | yes (via Stream) | medium -- 4 callbacks |
| `defimpl Collectable` | no | yes | N/A | low -- 1 callback |
| Both | yes | yes | yes | medium |
| Convert to list first | only after conversion | no | no | none -- but loses struct type |

---

## Common production mistakes

**1. Not handling `{:halt, acc}` in the `reduce` callback**
`Enum.take/2` sends a halt signal. If your `reduce` ignores it, `Enum.take(queue, 1)` processes the entire queue. Delegating to `Enumerable.List.reduce/3` handles this correctly.

**2. Returning the wrong shape from the `Collectable` collector**
The collector receives `(accumulator, command)`. The accumulator is first, the command tuple is second. Swapping them causes a `FunctionClauseError`.

**3. `count/1` returning `{:error, __MODULE__}` instead of `{:ok, n}`**
If `count/1` returns `{:error, __MODULE__}`, `Enum.count/1` falls back to a full traversal via `reduce/3`. Returning `{:ok, n}` gives O(1) count.

**4. `member?/2` returning `{:error, __MODULE__}` causing unnecessary traversals**
If `member?/2` returns `{:error, __MODULE__}`, `Enum.member?/2` traverses the entire collection.

**5. Mutable state in the `Collectable` collector closure**
The collector function receives the accumulator as an argument and returns the new accumulator. Do not use captured mutable state.

---

## Resources

- [Enumerable protocol -- official docs](https://hexdocs.pm/elixir/Enumerable.html)
- [Collectable protocol -- official docs](https://hexdocs.pm/elixir/Collectable.html)
- [Implementing Enumerable -- Elixir School](https://elixirschool.com/en/lessons/advanced/protocols)
- [Stream module -- lazy enumeration](https://hexdocs.pm/elixir/Stream.html)
