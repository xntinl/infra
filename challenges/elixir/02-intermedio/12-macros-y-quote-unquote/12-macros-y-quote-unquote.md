# Macros: Compile-Time Code Generation

## Why macros exist — and the cost

Macros let you extend Elixir syntax at compile time. Every `defmodule`, `def`, `use`,
`import`, and `require` is itself a macro.

The cost: macros are harder to debug. A wrong `unquote` produces non-obvious compilation
errors. Stack traces point to generated code, not the macro definition.

**Rule of thumb**: write a function first. Reach for a macro only when:
1. You need to inject code into the **caller's module** (new function definitions).
2. You need access to the caller's **AST**.
3. You need something that cannot be expressed as a function.

---

## The business problem

Build a `TaskQueue.Defhandler` macro module used inside handler modules to:

1. `defhandler/2` — defines a `handle_job/1` function with automatic error wrapping,
   logging, and duration tracking. The user only writes the business logic.
2. `defjob_type/1` — defines a `supported?/1` function that returns true for a list of
   job type atoms.

All modules are defined completely in this exercise.

---

## Project setup

```
task_queue/
├── lib/
│   └── task_queue/
│       └── defhandler.ex
├── test/
│   └── task_queue/
│       └── macros_test.exs
└── mix.exs
```

---

## Implementation

### `lib/task_queue/defhandler.ex`

```elixir
defmodule TaskQueue.Defhandler do
  @moduledoc """
  Macros for defining job handlers with built-in observability.

  Usage in a handler module:
      defmodule MyHandler do
        use TaskQueue.Defhandler

        defhandler :my_job, job do
          {:ok, process(job.payload)}
        end

        defjob_type [:my_job, :related_job]
      end
  """

  defmacro __using__(_opts) do
    quote do
      import TaskQueue.Defhandler, only: [defhandler: 2, defjob_type: 1]
      require Logger
    end
  end

  @doc """
  Defines a `handle_job/1` function that wraps the body with:
  - Duration tracking (start/end monotonic time)
  - Error capture (exceptions become {:error, exception} return values)
  - Logging (handler name, job_id, duration_ms, outcome)
  """
  defmacro defhandler(handler_name, job_var, do: body) do
    quote do
      def handle_job(%{type: unquote(handler_name)} = unquote(job_var)) do
        start_ms = System.monotonic_time(:millisecond)

        result =
          try do
            unquote(body)
          rescue
            e -> {:error, e}
          end

        duration_ms = System.monotonic_time(:millisecond) - start_ms

        case result do
          {:ok, _} ->
            Logger.debug(
              "handler=#{unquote(handler_name)} job_id=#{unquote(job_var).id} " <>
              "status=ok duration_ms=#{duration_ms}"
            )

          {:error, reason} ->
            Logger.warning(
              "handler=#{unquote(handler_name)} job_id=#{unquote(job_var).id} " <>
              "status=error reason=#{inspect(reason)} duration_ms=#{duration_ms}"
            )
        end

        result
      end
    end
  end

  @doc """
  Defines a `supported?/1` function that returns true if the given job type
  is in the provided list.
  """
  defmacro defjob_type(types) do
    evaluated_types = Macro.expand(types, __CALLER__)

    clauses =
      Enum.map(evaluated_types, fn type ->
        quote do
          def supported?(unquote(type)), do: true
        end
      end)

    catch_all =
      quote do
        def supported?(_), do: false
      end

    {:__block__, [], clauses ++ [catch_all]}
  end
end
```

When the caller writes `defhandler :webhook, job do ... end`, the macro expands to a full
function definition that records start time, wraps the body in `try/rescue`, calculates
duration, logs the outcome, and returns the result.

The `defjob_type/1` macro generates one `def supported?(type), do: true` clause per type,
plus a catch-all `def supported?(_), do: false`.

### Tests

```elixir
# test/task_queue/macros_test.exs
defmodule TaskQueue.MacrosTest do
  use ExUnit.Case, async: true

  defmodule TestHandler do
    use TaskQueue.Defhandler

    defhandler :test_job, job do
      if job.payload == :fail do
        raise "intentional failure"
      end

      {:ok, "processed: #{inspect(job.payload)}"}
    end

    defhandler :another_job, job do
      {:ok, job.payload * 2}
    end

    defjob_type [:test_job, :another_job]
  end

  describe "defhandler/2" do
    test "generates handle_job/1 that returns {:ok, result} for successful execution" do
      job = %{id: "j1", type: :test_job, payload: :hello}
      assert {:ok, "processed: :hello"} = TestHandler.handle_job(job)
    end

    test "wraps exceptions into {:error, exception}" do
      job = %{id: "j2", type: :test_job, payload: :fail}
      assert {:error, %RuntimeError{}} = TestHandler.handle_job(job)
    end

    test "dispatches to the correct clause based on job type" do
      job = %{id: "j3", type: :another_job, payload: 21}
      assert {:ok, 42} = TestHandler.handle_job(job)
    end

    test "raises FunctionClauseError for unsupported job type" do
      job = %{id: "j4", type: :unknown_type, payload: :x}

      assert_raise FunctionClauseError, fn ->
        TestHandler.handle_job(job)
      end
    end
  end

  describe "defjob_type/1" do
    test "supported? returns true for declared types" do
      assert TestHandler.supported?(:test_job)
      assert TestHandler.supported?(:another_job)
    end

    test "supported? returns false for unknown types" do
      refute TestHandler.supported?(:cron)
      refute TestHandler.supported?(:webhook)
      refute TestHandler.supported?(:nonexistent)
    end
  end

  describe "Macro expansion inspection" do
    test "handle_job/1 is a real function in the module" do
      assert function_exported?(TestHandler, :handle_job, 1)
    end

    test "supported?/1 is a real function in the module" do
      assert function_exported?(TestHandler, :supported?, 1)
    end
  end
end
```

### Run the tests

```bash
mix test test/task_queue/macros_test.exs --trace
```

---

## Common production mistakes

**1. Using a macro when a function works**
If you can pass the value as an argument, use a function. Macros are only necessary when
you need to manipulate the caller's AST or inject into the caller's module.

**2. Forgetting `quote` hygiene**
Variables bound inside `quote do` are hygienic — they do not leak into the caller's scope.
Use `var!(name)` explicitly if needed.

**3. `IO.puts` inside a macro runs at compile time**
```elixir
defmacro log_and_call(expr) do
  IO.puts("Expanding macro")  # Prints during mix compile
  quote do: unquote(expr)
end
```

**4. Calling macros before `require`**
Macros are compile-time constructs. You must `require` the module before using its macros.

---

## Resources

- [Macros — Elixir Getting Started](https://elixir-lang.org/getting-started/meta/macros.html)
- [Kernel.SpecialForms — HexDocs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html)
- [Macro — HexDocs](https://hexdocs.pm/elixir/Macro.html)
- [Metaprogramming Elixir — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
