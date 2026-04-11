# Enumerable and Collectable Protocols

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` manages a job queue and a job registry. Both are custom data structures that contain multiple items. The ops team wants to use standard `Enum` functions — `Enum.count/1`, `Enum.filter/2`, `Enum.map/2` — directly on these structures, and use `Enum.into/2` to copy jobs from one queue to another or from a list into a queue.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── job_queue.ex            # ← you implement Enumerable and Collectable
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── protocols_test.exs      # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The analytics team needs to query the in-memory job queue:

```elixir
# How many pending jobs of type "send_email" are waiting?
Enum.count(queue, fn job -> job.type == "send_email" end)

# Get all failed jobs for the dashboard
Enum.filter(queue, fn job -> job.status == :failed end)

# Copy jobs matching a filter into a new queue for retry
Enum.into(retryable_jobs, %JobQueue{})
```

None of this works with a custom `%JobQueue{}` struct unless it implements `Enumerable` and `Collectable`.

---

## The two protocols

**`Enumerable`** enables all `Enum` and `Stream` functions. It requires one callback:

```elixir
@callback reduce(t, acc, (element, acc -> acc | {:suspend, acc} | {:halt, acc})) ::
  {:done, acc} | {:halted, acc} | {:suspended, acc, continuation}
```

The simplest implementation converts the struct to a list and delegates to the list's `Enumerable`:

```elixir
defimpl Enumerable, for: MyStruct do
  def reduce(struct, acc, fun), do: Enumerable.List.reduce(to_list(struct), acc, fun)
  def count(struct), do: {:ok, length(to_list(struct))}
  def member?(struct, element), do: {:ok, element in to_list(struct)}
  def slice(struct), do: {:ok, length(to_list(struct)), &Enumerable.List.slice(to_list(struct), &1, &2, length(to_list(struct)))}
end
```

**`Collectable`** enables `Enum.into/2` and `for ... into:` comprehensions. It requires one callback:

```elixir
@callback into(t) :: {initial_acc, collector_fun}
# collector_fun.(accumulator, command) -> new_accumulator
# Commands:
#   {:cont, element} — add element; return updated accumulator
#   :done            — finalize; return the finished collection
#   :halt            — abort; return :ok (accumulator is discarded)
```

---

## Implementation

### Step 1: `lib/task_queue/job_queue.ex` — custom queue with protocol implementations

```elixir
defmodule TaskQueue.JobQueue do
  @moduledoc """
  An in-memory FIFO job queue implemented as a struct.

  Implements `Enumerable` to support all `Enum.*` and `Stream.*` operations.
  Implements `Collectable` to support `Enum.into/2` and `for ... into:`.

  ## Examples

      iex> queue = %TaskQueue.JobQueue{}
      iex> queue = Enum.into([%{type: "noop"}, %{type: "echo"}], queue)
      iex> Enum.count(queue)
      2
      iex> Enum.map(queue, & &1.type)
      ["noop", "echo"]

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
  @doc """
  Implements reduction over the queue by converting to a list.

  This enables all Enum and Stream operations on a JobQueue.
  """
  def reduce(queue, acc, fun) do
    # TODO: convert queue to a list and delegate to Enumerable.List.reduce/3
    # HINT: Enumerable.List.reduce(TaskQueue.JobQueue.to_list(queue), acc, fun)
  end

  def count(queue) do
    # TODO: return {:ok, size} using JobQueue.size/1
    # HINT: {:ok, TaskQueue.JobQueue.size(queue)}
  end

  def member?(queue, element) do
    # TODO: check if element is in the queue's list
    # HINT: {:ok, element in TaskQueue.JobQueue.to_list(queue)}
  end

  def slice(queue) do
    # TODO: return {:ok, size, slicer_fun}
    # The slicer_fun takes (start, length, size) and returns a sublist
    # Simplest approach: delegate to Enumerable.List.slice/4
    # HINT:
    # list = TaskQueue.JobQueue.to_list(queue)
    # size = length(list)
    # {:ok, size, &Enumerable.List.slice(list, &1, &2, size)}
  end
end

defimpl Collectable, for: TaskQueue.JobQueue do
  @doc """
  Implements collection into a JobQueue, enabling `Enum.into/2`.

  ## Examples

      iex> jobs = [%{type: "noop"}, %{type: "echo"}]
      iex> queue = Enum.into(jobs, %TaskQueue.JobQueue{})
      iex> TaskQueue.JobQueue.size(queue)
      2

  """
  def into(initial_queue) do
    collector_fun = fn
      # TODO: implement the three clauses.
      # The collector receives (accumulator, command):
      # queue_acc, {:cont, job_map} -> push job_map into queue_acc, return new queue
      # queue_acc, :done            -> return the final queue
      # _queue_acc, :halt           -> return :ok (discard accumulator)
      #
      # HINT:
      # queue, {:cont, job} -> TaskQueue.JobQueue.push(queue, job)
      # queue, :done        -> queue
      # _, :halt            -> :ok
    end

    {initial_queue, collector_fun}
  end
end
```

### Step 2: Given tests — must pass without modification

```elixir
# test/task_queue/protocols_test.exs
defmodule TaskQueue.ProtocolsTest do
  use ExUnit.Case, async: true

  alias TaskQueue.JobQueue

  describe "Enumerable — basic operations" do
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
      # Stream doesn't evaluate until Enum.to_list is called
      stream = Stream.filter(queue, fn j -> j.type == "send_email" end)
      result = Enum.to_list(stream)
      assert length(result) == 2
    end
  end

  describe "Collectable — Enum.into" do
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

    test "Enum.into with filter — only passing jobs collected" do
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

  describe "Enumerable — empty queue" do
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

### Step 3: Run the tests

```bash
mix test test/task_queue/protocols_test.exs --trace
```

---

## Trade-off analysis

| Approach | Supports `Enum.*` | Supports `Enum.into` | Lazy streams | Implementation effort |
|----------|------------------|---------------------|--------------|----------------------|
| `defimpl Enumerable` | yes | no | yes (via Stream) | medium — 4 callbacks |
| `defimpl Collectable` | no | yes | N/A | low — 1 callback |
| Both | yes | yes | yes | medium |
| Convert to list first | only after conversion | no | no | none — but loses struct type |

Reflection question: the `reduce/3` callback for `Enumerable` must handle three accumulator values: `{:cont, acc}`, `{:halt, acc}`, and `{:suspend, acc}`. When does `Stream.take(queue, 2)` produce a `{:halt, acc}` signal, and why is it important that the implementation stops iterating immediately when this signal arrives?

---

## Common production mistakes

**1. Not handling `{:halt, acc}` in the `reduce` callback**

`Enum.take/2` and `Stream.take_while/2` send a halt signal after collecting enough elements. If your `reduce` ignores it and keeps iterating, `Enum.take(queue, 1)` processes the entire queue instead of stopping after the first element.

Delegating to `Enumerable.List.reduce/3` handles this correctly — which is why the list-delegation approach is the right starting point.

**2. Returning the wrong shape from the `Collectable` collector**

The collector function signature is `fn accumulator, command -> new_accumulator`.
The command is `{:cont, element}`, `:done`, or `:halt` — it is the **second** argument.

```elixir
# Wrong — element and command are swapped; this will raise a FunctionClauseError
collector_fun = fn
  {:cont, job}, queue -> TaskQueue.JobQueue.push(queue, job)
  :done, queue        -> queue
  :halt, _            -> :ok
end

# Right — accumulator is first, command tuple is second
collector_fun = fn
  queue, {:cont, job} -> TaskQueue.JobQueue.push(queue, job)
  queue, :done        -> queue
  _, :halt            -> :ok
end
```

`Enum.into/2` calls `collector_fun.(accumulator, {:cont, element})` for each element,
then `collector_fun.(accumulator, :done)` to finalize.

**3. `count/1` returning `{:error, __MODULE__}` instead of `{:ok, n}`**

If `count/1` returns `{:error, __MODULE__}`, `Enum.count/1` falls back to a full traversal via `reduce/3`. This is correct but slower. Returning `{:ok, n}` gives O(1) count.

**4. `member?/2` returning `{:error, __MODULE__}` causing unnecessary traversals**

Similarly, if `member?/2` returns `{:error, __MODULE__}`, `Enum.member?/2` traverses the entire collection. For a FIFO queue where O(n) membership checks are expected, this is acceptable — but document it.

**5. Mutable state in the `Collectable` collector closure**

The collector function receives the accumulator as an argument and must return the new accumulator. Do not use captured mutable state — the `{:halt, _}` clause discards the accumulator and the collector must be side-effect free.

---

## Resources

- [Enumerable protocol — official docs](https://hexdocs.pm/elixir/Enumerable.html)
- [Collectable protocol — official docs](https://hexdocs.pm/elixir/Collectable.html)
- [Implementing Enumerable — Elixir School](https://elixirschool.com/en/lessons/advanced/protocols)
- [Stream module — lazy enumeration](https://hexdocs.pm/elixir/Stream.html)
