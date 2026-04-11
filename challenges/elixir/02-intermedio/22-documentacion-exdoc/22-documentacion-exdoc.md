# Documentation with ExDoc

## Goal

Build a `task_queue` project with comprehensive documentation using `@moduledoc`, `@doc`, doctests (`iex>` examples), and ExDoc HTML generation. Learn why documentation is first-class in Elixir and how to write testable examples.

---

## Why documentation is first-class in Elixir

`@doc` and `@moduledoc` are not comments -- they are module attributes compiled into the BEAM binary and accessible at runtime via `h/1` in IEx. This means:

- Documentation is introspectable without reading source code
- `doctest` runs the `iex>` examples as ExUnit tests -- stale examples fail CI
- Libraries published on Hex are rated by HexDocs coverage

The cultural norm in the Elixir ecosystem is that an undocumented public function is incomplete code, not a style choice.

---

## Implementation

### Step 1: `mix.exs` -- add ExDoc configuration

```elixir
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
          "Infrastructure": [TaskQueue.Scheduler]
        ]
      ],
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps do
    [
      {:jason, "~> 1.4"},
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.QueueServer
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/queue_server.ex` -- documented GenServer

The `@moduledoc` goes immediately after `defmodule`. The `@doc` before each public function includes `iex>` examples that `doctest` can verify. `@doc false` hides internal functions from the generated HTML while keeping them `def` (public) so they can be called from tests or other modules.

```elixir
defmodule TaskQueue.QueueServer do
  use GenServer

  @moduledoc """
  Manages the in-memory job queue for `task_queue`.

  Jobs are enqueued as maps and processed in FIFO order.
  The queue is bounded -- enqueueing to a full queue returns `{:error, :queue_full}`.

  ## Usage

      iex> {:ok, _pid} = TaskQueue.QueueServer.start_link(max_size: 100)
      iex> TaskQueue.QueueServer.enqueue(%{type: "send_email", args: %{}})
      {:ok, 1}

  ## Notes

  All public functions return `{:ok, value}` or `{:error, reason}`.
  The queue is not persistent -- it is lost when the process restarts.
  """

  @default_max_size 1_000

  @doc """
  Starts the QueueServer process with optional configuration.

  ## Options

    * `:max_size` - maximum number of jobs the queue can hold (default: #{@default_max_size})

  ## Examples

      iex> {:ok, pid} = TaskQueue.QueueServer.start_link(max_size: 100)
      iex> is_pid(pid)
      true

  """
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

  @doc false
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

### Step 4: `lib/task_queue/worker.ex` -- documented with tested doctests

Jason sorts keys alphabetically, so `%{b: 2, a: 1}` encodes to `{"a":1,"b":2}`, not `{"b":2,"a":1}`. Doctests must match the actual output exactly.

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

      iex> TaskQueue.Worker.encode_job(%{type: "ping"})
      {:ok, ~s({"type":"ping"})}

      iex> TaskQueue.Worker.encode_job(%{b: 2, a: 1})
      {:ok, ~s({"a":1,"b":2})}

      iex> TaskQueue.Worker.encode_job(%{})
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

  @doc false
  def __test_reset__ do
    :ok
  end
end
```

### Step 5: Tests -- doctests verified automatically

```elixir
# test/task_queue/doc_test.exs
defmodule TaskQueue.DocTest do
  use ExUnit.Case, async: true

  doctest TaskQueue.QueueServer
  doctest TaskQueue.Worker

  test "__reset__/0 has no public documentation" do
    docs = Code.fetch_docs(TaskQueue.QueueServer)
    {:docs_v1, _, _, _, _, _, fn_docs} = docs

    reset_doc =
      Enum.find(fn_docs, fn {{type, name, _arity}, _, _, _, _} ->
        type == :function and name == :__reset__
      end)

    assert elem(reset_doc, 3) == :hidden
  end
end
```

The `doctest` macro extracts every `iex>` example from `@doc` and `@moduledoc`, runs it as an ExUnit assertion, and fails CI if the output does not match. This is why doctests must be precise -- including map key ordering and exact return values.

### Step 6: Run doctests and generate docs

```bash
mix deps.get
mix test test/task_queue/doc_test.exs --trace
mix docs
open doc/index.html
```

---

## Trade-off analysis

| Aspect | `@doc` with `iex>` examples | `@doc` prose only | No `@doc` |
|--------|----------------------------|-------------------|-----------|
| Testable | yes -- `doctest` catches staleness | no | no |
| IEx `h/1` support | full | full | shows "No documentation" |
| HexDocs coverage | high | medium | zero |
| Maintenance cost | must keep examples accurate | lower | none |

When would `@doc false` be preferable to `defp`? When the function is public by necessity -- for example, callbacks invoked by macros (`__using__/1`), functions called by test helpers (`__reset__/0` for resetting GenServer state), or functions required by behaviours that are implementation details rather than user-facing API.

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
Maps are unordered. `%{a: 1, b: 2}` and `%{b: 2, a: 1}` are the same map but print differently. Pin the representation or test with `assert`.

**4. `@doc false` on `defp` functions**
Private functions are already excluded from documentation. `@doc false` has no effect on `defp`.

**5. ExDoc without `runtime: false`**
Without `runtime: false`, ExDoc starts as an OTP application, adding startup overhead to every `iex -S mix` session.

---

## Resources

- [Writing Documentation -- Elixir official guide](https://hexdocs.pm/elixir/writing-documentation.html)
- [ExDoc -- hex package](https://hexdocs.pm/ex_doc/readme.html)
- [ExUnit doctest -- docs](https://hexdocs.pm/ex_unit/ExUnit.DocTest.html)
- [h/1 in IEx -- Kernel.SpecialForms](https://hexdocs.pm/iex/IEx.Helpers.html#h/1)
