# `use` Patterns: Passing Options to `__using__/1`

**Project**: `use_patterns` — catalogue the canonical shapes of `use Module, opts` found in Phoenix, Ecto, and Plug, and build one that combines compile-time options with module attributes for a typed-config DSL.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–4 hours

---

## Project context

You maintain a shared "base module" that 40+ services in the company `use`. Each one
passes different options:

```elixir
defmodule PaymentWorker do
  use MyApp.Worker, queue: :payments, max_attempts: 5, telemetry_prefix: [:payment]
end
```

The challenge: `__using__/1` receives `opts` at compile time, but the author wants:

- Compile-time validation (unknown keys → `CompileError`)
- Sensible defaults
- Some options that become module attributes (queryable at runtime)
- Some that generate code (e.g. `telemetry_prefix` influences event names)

This is exactly the pattern in `use Oban.Worker, queue: :default, max_attempts: 3`,
`use Ecto.Schema` with `@primary_key`, `use Phoenix.Controller, namespace: ...`.

```
use_patterns/
├── lib/
│   └── use_patterns/
│       ├── worker.ex             # the base module
│       └── sample_workers.ex     # example consumers
├── test/
│   └── worker_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The anatomy of a `use` call

```
use MyApp.Worker, queue: :payments
```

is equivalent to:

```
require MyApp.Worker
MyApp.Worker.__using__(queue: :payments)
```

`__using__/1` receives the opts keyword list AT COMPILE TIME. It must return a quoted
expression that will be injected into the caller's module body.

### 2. Validate opts, fail fast

Never accept silent unknown keys — they hide typos. Define a whitelist:

```
@known_opts [:queue, :max_attempts, :telemetry_prefix]

def validate!(opts) do
  case Keyword.keys(opts) -- @known_opts do
    [] -> :ok
    extras -> raise CompileError, description: "unknown options: #{inspect(extras)}"
  end
end
```

### 3. Defaults via `Keyword.merge/2`

Centralize defaults at the top of `__using__`:

```
opts = Keyword.merge(defaults(), user_opts)
```

This way downstream code always sees a full keyword list.

### 4. Persist opts into module attributes

Inside the returned `quote`, set attributes the caller's module can read:

```
quote do
  @worker_queue unquote(opts[:queue])
  @worker_max_attempts unquote(opts[:max_attempts])
end
```

### 5. Emit functions parameterized by opts

For opts that influence generated code (e.g. `telemetry_prefix`), interpolate the
literal into the generated function body:

```
quote do
  def perform(job) do
    :telemetry.execute(unquote(opts[:telemetry_prefix] ++ [:start]), %{}, %{job: job})
    # ...
  end
end
```

---

## Implementation

### Step 1: `lib/use_patterns/worker.ex`

```elixir
defmodule UsePatterns.Worker do
  @moduledoc """
  Base behaviour + code-generating `__using__`.

      use UsePatterns.Worker,
        queue: :payments,
        max_attempts: 5,
        telemetry_prefix: [:payment, :worker]
  """

  @callback perform(job :: map()) :: :ok | {:error, term()}

  @defaults [queue: :default, max_attempts: 3, telemetry_prefix: [:worker]]
  @known_opts Keyword.keys(@defaults)

  defmacro __using__(opts) do
    opts = Keyword.merge(@defaults, opts)
    validate!(opts)

    queue = Keyword.fetch!(opts, :queue)
    max_attempts = Keyword.fetch!(opts, :max_attempts)
    telemetry_prefix = Keyword.fetch!(opts, :telemetry_prefix)

    validate_telemetry_prefix!(telemetry_prefix)

    quote do
      @behaviour UsePatterns.Worker

      @worker_queue unquote(queue)
      @worker_max_attempts unquote(max_attempts)
      @worker_telemetry_prefix unquote(telemetry_prefix)

      @spec queue() :: atom()
      def queue, do: @worker_queue

      @spec max_attempts() :: pos_integer()
      def max_attempts, do: @worker_max_attempts

      @spec telemetry_prefix() :: [atom()]
      def telemetry_prefix, do: @worker_telemetry_prefix

      @spec run(map()) :: :ok | {:error, term()}
      def run(job) do
        event_start = @worker_telemetry_prefix ++ [:start]
        event_stop = @worker_telemetry_prefix ++ [:stop]

        :telemetry.execute(event_start, %{system_time: System.system_time()}, %{job: job})
        start_mono = System.monotonic_time()

        result = perform(job)

        :telemetry.execute(
          event_stop,
          %{duration: System.monotonic_time() - start_mono},
          %{job: job, result: result}
        )

        result
      end
    end
  end

  @doc false
  def validate!(opts) do
    case Keyword.keys(opts) -- @known_opts do
      [] ->
        :ok

      extras ->
        raise CompileError,
          description:
            "UsePatterns.Worker: unknown options #{inspect(extras)}. " <>
              "Known: #{inspect(@known_opts)}"
    end
  end

  @doc false
  def validate_telemetry_prefix!(prefix) when is_list(prefix) do
    if Enum.all?(prefix, &is_atom/1) do
      :ok
    else
      raise CompileError,
        description: ":telemetry_prefix must be a list of atoms, got #{inspect(prefix)}"
    end
  end

  def validate_telemetry_prefix!(other) do
    raise CompileError,
      description: ":telemetry_prefix must be a list of atoms, got #{inspect(other)}"
  end
end
```

### Step 2: Sample consumers

```elixir
defmodule UsePatterns.Sample.PaymentWorker do
  use UsePatterns.Worker,
    queue: :payments,
    max_attempts: 5,
    telemetry_prefix: [:payment, :worker]

  @impl true
  def perform(%{amount: amt}) when amt > 0, do: :ok
  def perform(_), do: {:error, :invalid_amount}
end

defmodule UsePatterns.Sample.EmailWorker do
  use UsePatterns.Worker, queue: :emails

  @impl true
  def perform(%{to: _}), do: :ok
  def perform(_), do: {:error, :missing_to}
end
```

### Step 3: Tests

```elixir
defmodule UsePatterns.WorkerTest do
  use ExUnit.Case, async: false

  alias UsePatterns.Sample.{PaymentWorker, EmailWorker}

  describe "generated accessors" do
    test "queue/0 returns the opt" do
      assert PaymentWorker.queue() == :payments
      assert EmailWorker.queue() == :emails
    end

    test "max_attempts/0 falls back to default" do
      assert PaymentWorker.max_attempts() == 5
      assert EmailWorker.max_attempts() == 3
    end

    test "telemetry_prefix/0 uses passed value or default" do
      assert PaymentWorker.telemetry_prefix() == [:payment, :worker]
      assert EmailWorker.telemetry_prefix() == [:worker]
    end
  end

  describe "run/1 + telemetry" do
    test "emits start + stop with the worker's prefix" do
      parent = self()

      :telemetry.attach_many(
        :use_patterns_test,
        [[:payment, :worker, :start], [:payment, :worker, :stop]],
        fn e, m, md, _ -> send(parent, {e, m, md}) end,
        nil
      )

      assert :ok = PaymentWorker.run(%{amount: 100})
      assert_receive {[:payment, :worker, :start], _, _}
      assert_receive {[:payment, :worker, :stop], %{duration: _}, %{result: :ok}}
    after
      :telemetry.detach(:use_patterns_test)
    end
  end

  describe "compile-time validation" do
    test "unknown option raises" do
      assert_raise CompileError, ~r/unknown options/, fn ->
        Code.compile_string("""
        defmodule Bad do
          use UsePatterns.Worker, queu: :x
          @impl true
          def perform(_), do: :ok
        end
        """)
      end
    end

    test "non-atom telemetry_prefix entries raise" do
      assert_raise CompileError, ~r/list of atoms/, fn ->
        Code.compile_string("""
        defmodule BadPrefix do
          use UsePatterns.Worker, telemetry_prefix: ["not", "atoms"]
          @impl true
          def perform(_), do: :ok
        end
        """)
      end
    end
  end
end
```

---

## Trade-offs and production gotchas

**1. `use` re-runs on every recompile of the caller.** If `__using__/1` does heavy
work, compile times of downstream modules suffer. Keep it to opts validation + a
small `quote` block; push compile-time analysis to helper functions called inside
`quote`.

**2. Opts that are runtime config, not compile-time.** If a value changes between
environments (e.g. queue name per deploy), do NOT bake it via `use`. Accept it as
`Application.get_env/2` or `System.get_env` read inside `run/1`.

**3. Compile-time errors must include the caller file.** The macro's stack frame
leaks — pass `file:` and `line:` from `__CALLER__` to `raise CompileError` for
friendly errors.

**4. Opts are raw AST in some paths.** If you allow users to pass arbitrary Elixir
(e.g. a function reference), `opts` may arrive as quoted form. Evaluate or document.

**5. `@behaviour` warnings are per-callback.** Declaring the behaviour from inside
`__using__` ensures the compiler warns about missing callbacks in the caller module.

**6. Hierarchical `use`.** If `MyApp.Worker` itself uses `GenServer` internally,
double-`use` can shadow or conflict. Prefer "use exactly one bedrock" per module.

**7. Options explosion.** When opts grow past ~6, the macro becomes a mini
configuration language. Consider splitting into multiple macros
(`use Worker` + `worker_options do ... end`) or a runtime config struct.

**8. When NOT to use this.** If consumers only want a handful of functions and zero
generated code, a plain `import` or module alias is simpler — no `__using__`
needed.

---

## Benchmark

`use` runs at compile time only; there is nothing to bench at runtime. Any generated
functions (`queue/0`, `run/1`) are plain functions with normal BEAM performance.

---

## Resources

- [`Kernel.use/2` — hexdocs.pm](https://hexdocs.pm/elixir/Kernel.html#use/2)
- [Oban.Worker source](https://github.com/sorentwo/oban/blob/main/lib/oban/worker.ex) — canonical `use ... opts`
- [Phoenix.Controller source](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/controller.ex)
- [Ecto.Schema](https://github.com/elixir-ecto/ecto/blob/master/lib/ecto/schema.ex)
- [*Metaprogramming Elixir* — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
- [Dashbit blog — "mastering `use`"](https://dashbit.co/blog)
