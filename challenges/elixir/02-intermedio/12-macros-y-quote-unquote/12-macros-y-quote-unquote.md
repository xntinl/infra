# Macros: Compile-Time Code Generation

**Project**: `task_queue` — built incrementally across the intermediate level

---

## Project context

The task_queue system has many GenServer callbacks that follow a repetitive pattern:
validate arguments, log the operation, execute, log the result. Writing this boilerplate
for every handler type is error-prone and obscures the intent. A macro can generate this
boilerplate at compile time, leaving only the business logic visible.

This exercise covers the fundamentals: `quote`, `unquote`, and `defmacro`. The goal is
to understand when macros are the right tool — and when they are not.

Project structure at this point:

```
task_queue/
├── lib/
│   └── task_queue/
│       └── defhandler.ex
├── test/
│   └── task_queue/
│       └── macros_test.exs  # given tests — must pass without modification
└── mix.exs
```

---

## Why macros exist — and the cost

Macros let you extend Elixir syntax at compile time. Every `defmodule`, `def`, `use`,
`import`, and `require` is itself a macro. When you write `use GenServer`, a macro injects
`@behaviour GenServer`, default `child_spec/1`, and other boilerplate into your module.

The cost: macros are harder to debug than functions. A wrong `unquote` produces a
compilation error with a non-obvious message. Macros execute at compile time, so
`IO.puts` inside a macro runs when you `mix compile`, not when the code runs. Stack traces
from macros point to the generated code, not to the macro definition.

**Rule of thumb**: write a function first. Reach for a macro only when:
1. You need to inject code into the **caller's module** (new function definitions, module attributes).
2. You need access to the caller's **AST** (capturing expressions, building DSLs).
3. You need something that cannot be expressed as a function (e.g., `assert` in ExUnit
   captures the expression text to show it in failure messages).

---

## The business problem

`TaskQueue.Defhandler` is a macro module used inside handler modules to:

1. `defhandler/2` — defines a `handle_job/2` function with automatic error wrapping,
   logging, and duration tracking. The user only writes the business logic.
2. `defjob_type/1` — defines a `supported?/1` function that returns true for a list of
   job type atoms, false for everything else.

Both use `quote` and `unquote` to generate code in the caller's module.

---

## Implementation

### Step 1: `lib/task_queue/defhandler.ex`

```elixir
defmodule TaskQueue.Defhandler do
  @moduledoc """
  Macros for defining job handlers with built-in observability.

  Usage in a handler module:
      defmodule MyHandler do
        use TaskQueue.Defhandler

        defhandler :my_job, job do
          # business logic here — job is bound to the job map
          {:ok, process(job.payload)}
        end

        defjob_type [:my_job, :related_job]
      end
  """

  defmacro __using__(_opts) do
    quote do
      # Import the macros so the caller can use them without module prefix
      import TaskQueue.Defhandler, only: [defhandler: 2, defjob_type: 1]
      require Logger
    end
  end

  @doc """
  Defines a `handle_job/1` function that wraps the body with:
  - Duration tracking (start/end monotonic time)
  - Error capture (exceptions become {:error, exception} return values)
  - Logging (job_id, duration_ms, outcome)

  The `job_var` is the variable name bound to the job map inside the body.
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

  defjob_type [:webhook, :cron]
  # expands to:
  # def supported?(:webhook), do: true
  # def supported?(:cron), do: true
  # def supported?(_), do: false
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

The `defhandler/2` macro generates a `def handle_job/1` function clause that pattern
matches on `%{type: handler_name}`. When the caller writes:

```elixir
defhandler :webhook, job do
  {:ok, process(job.payload)}
end
```

The macro expands to a full function definition that:
1. Records the start time with `System.monotonic_time/1`.
2. Wraps the user's body in a `try/rescue` to capture exceptions.
3. Calculates the duration after execution.
4. Logs the outcome with the handler name, job ID, and duration.
5. Returns the result (either `{:ok, value}` or `{:error, exception}`).

The `defjob_type/1` macro uses `Macro.expand/2` to evaluate the types list at compile
time, then generates one `def supported?(type), do: true` clause per type, plus a
catch-all `def supported?(_), do: false`. The `{:__block__, [], ...}` wrapping combines
multiple AST nodes into a single block that Elixir can inject into the caller's module.

### Step 2: Example handler using the macros (for reference)

```elixir
defmodule TaskQueue.Handlers.WebhookHandler do
  use TaskQueue.Defhandler

  defhandler :webhook, job do
    # This body is wrapped by the macro — no try/rescue, no logging boilerplate
    %{url: url, method: method} = job.payload
    response = make_http_request(method, url)
    {:ok, response}
  end

  defjob_type [:webhook, :webhook_retry]

  defp make_http_request(method, url) do
    # Simulated HTTP call
    %{status: 200, method: method, url: url}
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/task_queue/macros_test.exs
defmodule TaskQueue.MacrosTest do
  use ExUnit.Case, async: true

  # Define a test handler inline using the macro
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
      # Verify the macro actually generated a function
      assert function_exported?(TestHandler, :handle_job, 1)
    end

    test "supported?/1 is a real function in the module" do
      assert function_exported?(TestHandler, :supported?, 1)
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/task_queue/macros_test.exs --trace
```

### Step 5: Inspect the generated AST

```bash
# In iex -S mix, after the tests pass:
iex> require TaskQueue.Defhandler
iex> ast = quote do
...>   TaskQueue.Defhandler.defhandler :my_job, job do
...>     {:ok, job.payload}
...>   end
...> end
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts()
```

Reading the expanded output shows exactly what code the macro generates.

---

## Trade-off analysis

| Aspect | Macro | Higher-order function | Protocol / Behaviour |
|--------|-------|-----------------------|---------------------|
| Injects code into caller module | Yes | No | No |
| Compile-time execution | Yes | No — runtime | No |
| Debuggability | Low — errors in generated code | High | High |
| Readable callsite | Yes — looks like native syntax | Yes — explicit function call | Yes |
| Stack traces | Point to generated code | Point to actual code | Point to actual code |
| When to use | DSLs, code injection, AST manipulation | Any reusable logic | Module contracts, type dispatch |

Reflection question: the `defhandler` macro generates a `def handle_job/1` clause with
pattern matching on `%{type: handler_name}`. If two calls to `defhandler` in the same
module use the same handler name, what happens? Is this a compile error, a warning, or
a runtime problem? How does Elixir handle multiple clauses of the same function?

---

## Common production mistakes

**1. Using a macro when a function works**
If you can pass the value as an argument, use a function. Macros are only necessary when
you need to manipulate the caller's AST or inject into the caller's module. The rule:
macros should feel magical from the outside but boring on the inside.

**2. Forgetting `quote` hygiene**
Variables bound inside `quote do` are hygienic by default — they do not leak into the
caller's scope. If you need to inject a variable into the caller's scope, use
`var!(name)` explicitly.

**3. `IO.puts` inside a macro runs at compile time**
```elixir
defmacro log_and_call(expr) do
  IO.puts("Expanding macro")  # Prints during mix compile, not at runtime
  quote do: unquote(expr)
end
```

**4. Calling macros before `require`**
Macros are compile-time constructs. If you use a macro from another module, you must
`require` that module before the call site. `import` makes this automatic, but explicit
`require` is safer.

---

## Resources

- [Macros — Elixir Getting Started](https://elixir-lang.org/getting-started/meta/macros.html)
- [Kernel.SpecialForms — HexDocs](https://hexdocs.pm/elixir/Kernel.SpecialForms.html) — `quote`, `unquote`, `unquote_splicing`
- [Macro — HexDocs](https://hexdocs.pm/elixir/Macro.html) — `Macro.expand/2`, `Macro.to_string/1`
- [Metaprogramming Elixir — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — the definitive book on Elixir macros
