# Kernel and Advanced Builtins

## Goal

Build a `task_queue` project that uses `apply/3` for dynamic job dispatch, `get_in/2` / `put_in/3` / `update_in/3` for nested config access, and `__MODULE__` for portable module references. These Kernel builtins solve real problems: dispatching to handler modules determined at runtime, reading deeply nested state, and referencing the current module without hardcoding names.

---

## Why `apply/3` and not anonymous functions

```elixir
job_handler = "TaskQueue.Handlers.Email"   # from job payload
# You cannot write: job_handler.execute(args)  <- syntax error
# apply/3 bridges the gap:
apply(String.to_existing_atom("Elixir.#{job_handler}"), :execute, [args])
```

This is the foundation of plugin systems, command dispatchers, and any architecture where the module to call is determined from data rather than code.

The risk: `String.to_atom/1` creates atoms permanently. Use `String.to_existing_atom/1` -- it only succeeds if the atom was already compiled into the VM, preventing atom table exhaustion from untrusted input.

---

## Why `get_in` and `update_in` and not pattern matching

Pattern matching to read three levels deep is verbose and breaks when the structure changes:

```elixir
# Pattern matching -- verbose, fragile
%{^job_id => %{meta: %{retries: retries}}} = registry

# get_in/update_in -- concise, composable
retries = get_in(registry, [job_id, :meta, :retries])
new_registry = update_in(registry, [job_id, :meta, :retries], &(&1 + 1))
```

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
    [
      extra_applications: [:logger],
      mod: {TaskQueue.Application, []}
    ]
  end

  defp deps, do: []
end
```

### Step 2: `lib/task_queue/application.ex`

```elixir
defmodule TaskQueue.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      TaskQueue.Registry
    ]

    opts = [strategy: :one_for_one, name: TaskQueue.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Step 3: `lib/task_queue/worker.ex` -- handler for dispatch tests

The Worker module implements `execute/1` so that the Scheduler's dynamic dispatch has a known target module to call during tests.

```elixir
defmodule TaskQueue.Worker do
  @moduledoc """
  Processes a single job. Implements execute/1 for dynamic dispatch.
  """

  @spec execute(map()) :: {:ok, term()} | {:error, term()}
  def execute(%{type: "noop"}), do: {:ok, :noop}
  def execute(%{type: "echo", args: args}), do: {:ok, args}
  def execute(%{}), do: {:error, :missing_required_fields}
  def execute(_), do: {:error, :invalid_job}
end
```

### Step 4: `lib/task_queue/scheduler.ex` -- dynamic dispatch with apply/3

`apply/3` calls a module and function determined at runtime. When the handler name comes from external data (a job payload), `String.to_existing_atom/1` ensures only compiled modules can be invoked. An attacker cannot create new atoms by submitting arbitrary handler names.

```elixir
defmodule TaskQueue.Scheduler do
  @moduledoc """
  Dispatches jobs to handler modules determined at runtime.
  Handler modules must implement `execute/1`.
  """

  @spec dispatch(map()) :: {:ok, term()} | {:error, term()}
  def dispatch(%{handler: handler_name, args: args}) when is_atom(handler_name) do
    try do
      result = apply(handler_name, :execute, [args])
      {:ok, result}
    rescue
      UndefinedFunctionError ->
        {:error, :unknown_handler}
    end
  end

  def dispatch(%{handler: handler_name, args: args}) when is_binary(handler_name) do
    try do
      module = String.to_existing_atom("Elixir.#{handler_name}")
      dispatch(%{handler: module, args: args})
    rescue
      ArgumentError ->
        {:error, :unknown_handler}
    end
  end

  def dispatch(_), do: {:error, :invalid_job_format}

  @doc """
  Returns the module name as an atom -- useful for logging and child specs.

  ## Examples

      iex> TaskQueue.Scheduler.module_name()
      TaskQueue.Scheduler

  """
  def module_name, do: __MODULE__
end
```

### Step 5: `lib/task_queue/registry.ex` -- nested access patterns

The Registry stores job metadata in a deeply nested map. `get_in/2` reads values at arbitrary depth without pattern matching boilerplate. `update_in/3` and `put_in/3` modify nested values and return the updated structure. `Map.new/2` is used for batch operations across all entries (since `Access.all()` works only on lists, not maps).

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
    get_in(state, [job_id, :meta, :retries]) || 0
  end

  @doc """
  Increments the retry counter for a job and records the error.
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
    new_state =
      state
      |> update_in([job_id, :meta, :retries], &((&1 || 0) + 1))
      |> put_in([job_id, :meta, :last_error], error)
      |> put_in([job_id, :status], :retrying)

    {:reply, :ok, new_state}
  end

  @impl true
  def handle_call(:mark_all_stale, _from, state) do
    new_state = Map.new(state, fn {job_id, entry} -> {job_id, %{entry | status: :stale}} end)
    {:reply, :ok, new_state}
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

### Step 6: Tests

```elixir
# test/task_queue/kernel_builtins_test.exs
defmodule TaskQueue.KernelBuiltinsTest do
  use ExUnit.Case, async: false

  alias TaskQueue.{Scheduler, Registry}

  setup do
    try do
      GenServer.call(Registry, :reset)
    rescue
      _ -> :ok
    end

    :ok
  end

  describe "Scheduler.dispatch/1 -- apply/3 dispatch" do
    test "dispatches to a known handler atom" do
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

  describe "Registry -- nested get_in / update_in" do
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

### Step 7: Run

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
| `Access.all()` | batch update elements in a **list** | works on lists only -- use `Map.new/2` for maps |
| `__MODULE__` | self-reference in GenServers | expands at compile time per module scope |

`put_in(user, [:address, :city], "Madrid")` raises if `:address` does not exist. Raising is the right default when you expect the path to exist and a missing intermediate key signals a programming error. Silent insertion would be preferable when building structures from partial data (like API responses where nested objects may be absent).

---

## Common production mistakes

**1. `apply/3` with `String.to_atom/1`**
Atoms are never garbage collected. External payloads can exhaust the atom table.

**2. `get_in` with atom keys on a map with string keys**
`get_in(data, [:name])` returns `nil` on `%{"name" => "Alice"}`. After JSON decode, use string keys.

**3. `put_in` on a path with missing intermediate keys**
`put_in(%{}, [:a, :b], 1)` raises `KeyError`. Create intermediate levels first.

**4. `__MODULE__` in nested modules**
Inside `defmodule Outer.Inner`, `__MODULE__` expands to `Outer.Inner`, not `Outer`.

**5. `update_in` with `Access.all()` on maps**
`Access.all()` works on lists. On maps, use `Map.new/2`.

---

## Resources

- [Kernel module -- official docs](https://hexdocs.pm/elixir/Kernel.html)
- [Access module -- official docs](https://hexdocs.pm/elixir/Access.html)
- [apply/3 -- Erlang docs](https://www.erlang.org/doc/man/erlang.html#apply-3)
- [get_in/put_in/update_in guide](https://hexdocs.pm/elixir/Kernel.html#get_in/2)
