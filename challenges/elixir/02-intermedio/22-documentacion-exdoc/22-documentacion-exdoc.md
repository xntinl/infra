# Documentation with ExDoc

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` is used by multiple teams. Without documentation, every integration requires reading source code. You need to document the public API so that `mix docs` generates a navigable HTML site and `h/1` in IEx works for every public function.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex               # ← you document this
│       ├── queue_server.ex         # ← and this
│       ├── scheduler.ex            # ← and this
│       └── registry.ex             # ← and this
├── test/
│   └── task_queue/
│       └── doc_test.exs            # given tests — doctests must pass
└── mix.exs                         # ← add ExDoc config here
```

---

## The business problem

The platform team has asked for:

1. A self-hosted HTML documentation site generated with `mix docs`
2. All public functions documented with at least one verifiable `iex>` example
3. Internal helpers hidden from the HTML (`@doc false`)
4. A README that appears as the landing page

The goal is not just to write text — it is to write documentation that can be tested and that consumers can trust is accurate.

---

## Why documentation is first-class in Elixir

`@doc` and `@moduledoc` are not comments — they are module attributes that are compiled into the BEAM binary and accessible at runtime via `h/1` in IEx. This means:

- Documentation is introspectable without reading source code
- `doctest` runs the `iex>` examples as ExUnit tests — stale examples fail CI
- Libraries published on Hex are rated by HexDocs coverage

The cultural norm in the Elixir ecosystem is that an undocumented public function is incomplete code, not a style choice.

---

## Implementation

### Step 1: `mix.exs` — add ExDoc configuration

```elixir
# mix.exs
defmodule TaskQueue.MixProject do
  use Mix.Project

  def project do
    [
      app: :task_queue,
      version: "0.1.0",
      elixir: "~> 1.15",
      name: "TaskQueue",
      source_url: "https://github.com/your-org/task_queue",
      docs: [
        main: "TaskQueue",
        extras: ["README.md"],
        groups_for_modules: [
          "Core": [TaskQueue, TaskQueue.QueueServer, TaskQueue.Worker],
          "Infrastructure": [TaskQueue.Scheduler, TaskQueue.Registry]
        ]
      ],
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      releases: releases()
    ]
  end

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:req, "~> 0.5"},
      # TODO: add {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### Step 2: `lib/task_queue/queue_server.ex` — document the public API

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer

  @moduledoc """
  Manages the in-memory job queue for `task_queue`.

  Jobs are enqueued as maps and processed in FIFO order by `TaskQueue.Scheduler`.
  The queue is bounded — enqueueing to a full queue returns `{:error, :queue_full}`.

  ## Usage

      iex> {:ok, _pid} = TaskQueue.QueueServer.start_link(max_size: 100)
      iex> TaskQueue.QueueServer.enqueue(%{type: "send_email", args: %{}})
      {:ok, 1}

  ## Notes

  All public functions return `{:ok, value}` or `{:error, reason}`.
  The queue is not persistent — it is lost when the process restarts.
  """

  @default_max_size 1_000

  # TODO: add @doc with description, ## Examples, and ## Returns to each function below

  def start_link(opts \\ []) do
    max_size = Keyword.get(opts, :max_size, @default_max_size)
    GenServer.start_link(__MODULE__, %{queue: :queue.new(), max_size: max_size}, name: __MODULE__)
  end

  @doc """
  Enqueues a job and returns the new queue length.

  Returns `{:error, :queue_full}` if the queue has reached its maximum size.

  ## Examples

      iex> TaskQueue.QueueServer.enqueue(%{type: "ping", args: %{}})
      {:ok, 1}

      iex> TaskQueue.QueueServer.enqueue("not a map")
      {:error, :invalid_job}

  """
  def enqueue(job) when is_map(job) do
    GenServer.call(__MODULE__, {:enqueue, job})
  end

  def enqueue(_), do: {:error, :invalid_job}

  @doc """
  Dequeues and returns the next job, or `{:error, :empty}` if the queue is empty.

  ## Examples

      iex> TaskQueue.QueueServer.enqueue(%{type: "ping", args: %{}})
      {:ok, 1}
      iex> {:ok, job} = TaskQueue.QueueServer.dequeue()
      iex> job.type
      "ping"

  """
  def dequeue do
    GenServer.call(__MODULE__, :dequeue)
  end

  @doc """
  Returns the current number of jobs in the queue.

  ## Examples

      iex> TaskQueue.QueueServer.size()
      0

  """
  def size do
    GenServer.call(__MODULE__, :size)
  end

  # Internal helper — not part of the public API
  # TODO: add @doc false to this function
  def __reset__ do
    GenServer.call(__MODULE__, :reset)
  end

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call({:enqueue, job}, _from, %{queue: q, max_size: max} = state) do
    if :queue.len(q) >= max do
      {:reply, {:error, :queue_full}, state}
    else
      new_q = :queue.in(job, q)
      {:reply, {:ok, :queue.len(new_q)}, %{state | queue: new_q}}
    end
  end

  @impl true
  def handle_call(:dequeue, _from, %{queue: q} = state) do
    case :queue.out(q) do
      {{:value, job}, new_q} -> {:reply, {:ok, job}, %{state | queue: new_q}}
      {:empty, _} -> {:reply, {:error, :empty}, state}
    end
  end

  @impl true
  def handle_call(:size, _from, %{queue: q} = state) do
    {:reply, :queue.len(q), state}
  end

  @impl true
  def handle_call(:reset, _from, %{max_size: max}) do
    {:reply, :ok, %{queue: :queue.new(), max_size: max}}
  end
end
```

### Step 3: `lib/task_queue/worker.ex` — fix broken doctests

The following `@doc` examples are broken. Identify and fix all problems before running `mix doctest`.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Processes a single job from the task queue.

  Encodes jobs as JSON for transport and decodes them for execution.
  Each worker is supervised and may retry failed jobs up to `max_retries` times.
  """

  @doc """
  Encodes a job map as a JSON string.

  ## Examples

      # BUG 1: result is on the same line as the expression
      iex> TaskQueue.Worker.encode_job(%{type: "ping"}) {:ok, ~s({"type":"ping"})}

      # BUG 2: wrong expected result — Jason sorts keys alphabetically
      iex> TaskQueue.Worker.encode_job(%{b: 2, a: 1})
      {:ok, ~s({"b":2,"a":1})}

      # BUG 3: missing iex> prefix
      TaskQueue.Worker.encode_job(%{})
      {:ok, "{}"}

  """
  def encode_job(job) when is_map(job) do
    Jason.encode(job)
  end

  def encode_job(_), do: {:error, :not_a_map}

  @doc """
  Returns the configured maximum number of retries.

  ## Examples

      iex> is_integer(TaskQueue.Worker.max_retries())
      true

  """
  def max_retries do
    Application.get_env(:task_queue, :max_retries, 3)
  end

  # This function is called by macros in the test framework.
  # It is public by necessity but should not appear in the documentation.
  # TODO: add @doc false
  def __test_reset__ do
    :ok
  end
end
```

### Step 4: Given tests — must pass without modification

```elixir
# test/task_queue/doc_test.exs
defmodule TaskQueue.DocTest do
  use ExUnit.Case, async: true

  # Run all iex> examples from the module's @doc and @moduledoc
  doctest TaskQueue.QueueServer
  doctest TaskQueue.Worker

  # Verify that @doc false functions are not documented
  test "__reset__/0 has no public documentation" do
    docs = Code.fetch_docs(TaskQueue.QueueServer)
    {:docs_v1, _, _, _, _, _, fn_docs} = docs

    reset_doc =
      Enum.find(fn_docs, fn {{type, name, _arity}, _, _, _, _} ->
        type == :function and name == :__reset__
      end)

    # @doc false means the doc entry is :hidden, not a string
    assert elem(reset_doc, 3) == :hidden
  end
end
```

### Step 5: Run doctests and generate docs

```bash
mix deps.get
mix test test/task_queue/doc_test.exs --trace

# Generate HTML documentation
mix docs

# Open in browser
open doc/index.html
```

---

## Trade-off analysis

| Aspect | `@doc` with `iex>` examples | `@doc` prose only | No `@doc` |
|--------|----------------------------|-------------------|-----------|
| Testable | yes — `doctest` catches staleness | no | no |
| IEx `h/1` support | full | full | shows "No documentation" |
| HexDocs coverage | high | medium | zero |
| Maintenance cost | must keep examples accurate | lower | none |

Reflection question: when would `@doc false` be preferable to `defp`? Name a real scenario where a function must be `def` but should not appear in the public API.

---

## Common production mistakes

**1. Doctest result on the same line as the expression**
```elixir
# Wrong
iex> String.upcase("hello") "HELLO"

# Right
iex> String.upcase("hello")
"HELLO"
```

**2. `@moduledoc` after `@behaviour` or other attributes**
ExDoc ignores or misplaces `@moduledoc` when it is not immediately after `defmodule`. Always place it first.

**3. Map key order in doctests**
Maps are unordered. `%{a: 1, b: 2}` and `%{b: 2, a: 1}` are the same map but print differently. Pin the representation or test with `assert Map.equal?/2` instead.

**4. `@doc false` on `defp` functions**
Private functions are already excluded from documentation. `@doc false` has no effect on `defp` and is misleading.

**5. ExDoc without `runtime: false`**
Without `runtime: false`, ExDoc starts as an OTP application. It adds startup overhead to every `iex -S mix` session and may conflict with your application.

---

## Resources

- [Writing Documentation — Elixir official guide](https://hexdocs.pm/elixir/writing-documentation.html)
- [ExDoc — hex package](https://hexdocs.pm/ex_doc/readme.html)
- [ExUnit doctest — docs](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [h/1 in IEx — Kernel.SpecialForms](https://hexdocs.pm/iex/IEx.Helpers.html#h/1)
