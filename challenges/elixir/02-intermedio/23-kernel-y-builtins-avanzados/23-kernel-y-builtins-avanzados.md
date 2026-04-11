# Kernel and Advanced Builtins

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

`task_queue` needs utilities for dynamic job dispatch, deeply nested configuration access, and portable module references. These use cases are perfect for `apply/3`, `get_in/2`, `put_in/3`, `update_in/3`, and `__MODULE__`.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       ├── application.ex
│       ├── worker.ex
│       ├── queue_server.ex
│       ├── scheduler.ex            # ← you add dynamic dispatch here
│       └── registry.ex
├── test/
│   └── task_queue/
│       └── kernel_builtins_test.exs   # given tests — must pass
└── mix.exs
```

---

## The business problem

Three concrete problems in `task_queue`:

1. **Dynamic dispatch** — job handlers are determined at runtime from job payloads. `apply/3` lets the scheduler call `MyHandler.execute(args)` without knowing the handler at compile time.

2. **Nested config access** — the job registry stores deeply nested metadata: `%{job_id => %{status: :running, meta: %{retries: 2, last_error: nil}}}`. Reading and updating individual fields without pattern-matching boilerplate requires `get_in` and `update_in`.

3. **Portable module references** — every GenServer in `task_queue` uses `__MODULE__` in `start_link` and `child_spec` so that renaming a module never requires hunting down hardcoded references.

---

## Why `apply/3` and not anonymous functions

```
job_handler = "TaskQueue.Handlers.Email"   # from job payload
# You cannot write: job_handler.execute(args)  ← syntax error
# apply/3 bridges the gap:
apply(String.to_existing_atom("Elixir.#{job_handler}"), :execute, [args])
```

This is the foundation of plugin systems, command dispatchers, and any architecture where the module to call is determined from data rather than code.

The risk: `String.to_atom/1` creates atoms permanently. Use `String.to_existing_atom/1` — it only succeeds if the atom was already compiled into the VM, preventing atom table exhaustion from untrusted input.

---

## Why `get_in` and `update_in` and not pattern matching

Pattern matching to read three levels deep is verbose and breaks when the structure changes:

```elixir
# Pattern matching — verbose, fragile
%{^job_id => %{meta: %{retries: retries}}} = registry
# update is even worse

# get_in/update_in — concise, composable
retries = get_in(registry, [job_id, :meta, :retries])
new_registry = update_in(registry, [job_id, :meta, :retries], &(&1 + 1))
```

When the registry is a list, `Access.all()` lets you batch-update every entry in one expression.
For a map-based registry, use `Map.new/2` to iterate and rebuild with updated values.

---

## Implementation

### Step 1: `lib/task_queue/scheduler.ex` — dynamic dispatch with apply/3

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Dispatches jobs to handler modules determined at runtime.

  Handler modules must implement `execute/1`.
  The handler name is read from the job's `:handler` field.
  """

  @doc """
  Dispatches a job to its handler module using `apply/3`.

  Returns `{:ok, result}` or `{:error, reason}`.

  The handler is resolved from the job's `:handler` key as a module atom.
  Only atoms that are already compiled into the VM are accepted.
  """
  @spec dispatch(map()) :: {:ok, term()} | {:error, term()}
  def dispatch(%{handler: handler_name, args: args}) when is_atom(handler_name) do
    # TODO: use apply/3 to call handler_name.execute(args)
    # Wrap in try/rescue to return {:error, reason} on UndefinedFunctionError
    # HINT: apply(handler_name, :execute, [args])
  end

  def dispatch(%{handler: handler_name, args: args}) when is_binary(handler_name) do
    # TODO: convert handler_name to an existing atom, then delegate to the above clause
    # HINT: String.to_existing_atom("Elixir.#{handler_name}")
    # Return {:error, :unknown_handler} if the atom does not exist
  end

  def dispatch(_), do: {:error, :invalid_job_format}

  @doc """
  Returns the module name as an atom — useful for logging and child specs.

  ## Examples

      iex> TaskQueue.Scheduler.module_name()
      TaskQueue.Scheduler

  """
  def module_name, do: __MODULE__
end
```

### Step 2: `lib/task_queue/registry.ex` — nested access patterns

```elixir
defmodule TaskQueue.Registry do
  @moduledoc """
  Tracks running jobs and their metadata.

  State shape:
      %{job_id => %{status: atom, meta: %{retries: integer, last_error: term}}}
  """

  use GenServer

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  end

  @doc """
  Registers a new job with initial metadata.
  """
  def register(job_id, initial_meta \\ %{}) do
    GenServer.call(__MODULE__, {:register, job_id, initial_meta})
  end

  @doc """
  Returns the retry count for a job.
  """
  def get_retries(job_id) do
    state = GenServer.call(__MODULE__, :get_state)
    # TODO: use get_in to read state[job_id][:meta][:retries] with default 0
    # HINT: get_in(state, [job_id, :meta, :retries]) || 0
  end

  @doc """
  Increments the retry counter for a job and records the error.
  Returns the updated registry state.
  """
  def record_retry(job_id, error) do
    GenServer.call(__MODULE__, {:record_retry, job_id, error})
  end

  @doc """
  Returns all job IDs currently in the registry.
  """
  def list_jobs do
    state = GenServer.call(__MODULE__, :get_state)
    Map.keys(state)
  end

  @doc """
  Marks all running jobs as :stale (e.g., after a restart).
  Uses Map.new/2 to batch-update every entry in the map-based registry.
  """
  def mark_all_stale do
    GenServer.call(__MODULE__, :mark_all_stale)
  end

  @impl true
  def init(state), do: {:ok, state}

  @impl true
  def handle_call({:register, job_id, meta}, _from, state) do
    entry = %{status: :registered, meta: Map.merge(%{retries: 0, last_error: nil}, meta)}
    {:reply, :ok, Map.put(state, job_id, entry)}
  end

  @impl true
  def handle_call({:record_retry, job_id, error}, _from, state) do
    # TODO: use update_in to increment state[job_id][:meta][:retries] by 1
    # TODO: use put_in to set state[job_id][:meta][:last_error] to error
    # TODO: use put_in to set state[job_id][:status] to :retrying
    # Return {:reply, :ok, new_state}
    {:reply, :ok, state}
  end

  @impl true
  def handle_call(:mark_all_stale, _from, state) do
    # TODO: set every job's :status to :stale
    #
    # Note: Access.all() works on lists, not maps.
    # For a map of job_id => entry, use Map.new/2 to rebuild with updated entries:
    #
    # HINT:
    # new_state = Map.new(state, fn {job_id, entry} -> {job_id, %{entry | status: :stale}} end)
    # {:reply, :ok, new_state}
    {:reply, :ok, state}
  end

  @impl true
  def handle_call(:reset, _from, _state) do
    {:reply, :ok, %{}}
  end

  @impl true
  def handle_call(:get_state, _from, state) do
    {:reply, state, state}
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/kernel_builtins_test.exs
defmodule TaskQueue.KernelBuiltinsTest do
  use ExUnit.Case, async: false

  alias TaskQueue.{Scheduler, Registry}

  setup do
    # Reset registry state between tests
    try do
      GenServer.call(Registry, :reset)
    rescue
      _ -> :ok
    end

    :ok
  end

  describe "Scheduler.dispatch/1 — apply/3 dispatch" do
    test "dispatches to a known handler atom" do
      # TaskQueue.Worker implements execute/1 for testing
      result = Scheduler.dispatch(%{handler: TaskQueue.Worker, args: %{}})
      assert match?({:ok, _} | {:error, :missing_required_fields}, result)
    end

    test "returns error for unknown handler string" do
      result = Scheduler.dispatch(%{handler: "Unknown.Handler.DoesNotExist", args: %{}})
      assert {:error, :unknown_handler} = result
    end

    test "returns error for invalid job format" do
      assert {:error, :invalid_job_format} = Scheduler.dispatch("not a map")
    end
  end

  describe "Registry — nested get_in / update_in" do
    test "get_retries returns 0 for new job" do
      Registry.register("job-1")
      assert Registry.get_retries("job-1") == 0
    end

    test "record_retry increments retry count" do
      Registry.register("job-2")
      Registry.record_retry("job-2", :timeout)
      Registry.record_retry("job-2", :timeout)
      assert Registry.get_retries("job-2") == 2
    end

    test "mark_all_stale updates every job" do
      Registry.register("job-3")
      Registry.register("job-4")
      Registry.mark_all_stale()

      state = GenServer.call(Registry, :get_state)
      assert Enum.all?(state, fn {_, entry} -> entry.status == :stale end)
    end
  end

  describe "__MODULE__ portability" do
    test "Scheduler.module_name/0 returns the module atom" do
      assert Scheduler.module_name() == TaskQueue.Scheduler
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/kernel_builtins_test.exs --trace
```

---

## Trade-off analysis

| Tool | Use case | What to watch for |
|------|----------|-------------------|
| `apply/3` | handler name from data | `String.to_existing_atom` over `String.to_atom` |
| `get_in/2` | read nested path | returns `nil` silently if path is missing |
| `put_in/3` | write nested path | raises if intermediate key is missing |
| `update_in/3` | transform nested value | same as `put_in` for missing paths |
| `Access.all()` | batch update elements in a **list** | works on lists only — use `Map.new/2` for maps |
| `__MODULE__` | self-reference in GenServers | expands at compile time per module scope |

Reflection question: `put_in(user, [:address, :city], "Madrid")` raises if `:address` does not exist. When is this behavior the right default, and when would silent nil insertion be preferable?

---

## Common production mistakes

**1. `apply/3` with `String.to_atom/1`**
Atoms are never garbage collected. A job payload from an external source can create unbounded atoms, exhausting the atom table and crashing the VM. Always use `String.to_existing_atom/1`.

**2. `get_in` with atom keys on a map with string keys**
`get_in(data, [:name])` returns `nil` on `%{"name" => "Alice"}`. String-keyed maps (typical after JSON decode) require string keys: `get_in(data, ["name"])`.

**3. `put_in` on a path with missing intermediate keys**
`put_in(%{}, [:a, :b], 1)` raises `KeyError`. Create the intermediate levels first or use `Map.put` for the top level.

**4. `__MODULE__` in nested modules**
Inside `defmodule Outer.Inner`, `__MODULE__` expands to `Outer.Inner`, not `Outer`. This trips developers who copy a GenServer pattern into a nested module expecting the outer name.

**5. `update_in` with `Access.all()` on maps**
`Access.all()` works on lists. On maps, use `Enum.map/2` and rebuild the map, or iterate with `Map.new/2`.

---

## Resources

- [Kernel module — official docs](https://hexdocs.pm/elixir/Kernel.html)
- [Access module — official docs](https://hexdocs.pm/elixir/Access.html)
- [apply/3 — Erlang docs](https://www.erlang.org/doc/man/erlang.html#apply-3)
- [get_in/put_in/update_in guide](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
