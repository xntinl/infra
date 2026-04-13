# Custom `defdelegate` with Telemetry

**Project**: `defdelegate_custom` — write your own `defproxy/2` macro that emits a proxy function identical to `defdelegate`, plus automatic `:telemetry` spans and optional circuit-breaker hooks.

---

## The business problem

Your team relies heavily on `defdelegate/2` — a clean way to re-export functions from
another module:

### `mix.exs`
```elixir
defmodule DefdelegateCustom.MixProject do
  use Mix.Project

  def project do
    [
      app: :defdelegate_custom,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    [# No external dependencies — pure Elixir]
  end
end
```
```elixir
defmodule MyApp.API do
  defdelegate create_user(params), to: MyApp.Users
  defdelegate find_user(id),       to: MyApp.Users
end
```
The pain: `defdelegate` produces a zero-cost proxy with no instrumentation. Your
observability stack needs `[:my_app, :api, :create_user, :start/:stop]` telemetry
events on every call. Wrapping manually is tedious and error-prone. You want:

```elixir
defmodule MyApp.API do
  use ProxyMacro

  defproxy create_user(params), to: MyApp.Users
  defproxy find_user(id),       to: MyApp.Users
end
```
One declaration. Every call emits start/stop events with duration, errors are caught
and re-emitted as `:exception` events. This is what `Sentry.Plug`, `NewRelic.Agent`,
and `Decorator` (hex) do internally.

## Project structure

```
defdelegate_custom/
├── lib/
│   └── defdelegate_custom/
│       ├── proxy_macro.ex         # defproxy/2 macro
│       └── telemetry_helpers.ex   # span helper used by the proxy
├── test/
│   └── proxy_macro_test.exs
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why a macro and not hand-written wrappers

Hand wrappers work for five functions; past that they rot. A macro emits identical `def` clauses with the target's `@spec` interpolated, so adding a delegate is one line and there is no wrapper-vs-target skew to chase.

---

## Design decisions

**Option A — hand-written wrapper functions**
- Pros: explicit, trivially debuggable stacktraces; easy to add logging or arg transformation.
- Cons: repetitive; drift between wrapper and target; no introspection.

**Option B — custom `defdelegate`-style macro** (chosen)
- Pros: single source of truth, compile-time spec propagation, metadata available for docs.
- Cons: opaque stacktraces if unwrapped; macro machinery to maintain.

→ Chose **B** because the delegation table is long and mechanical; the macro keeps signatures and specs in lockstep.

---

## Implementation

### `lib/defdelegate_custom.ex`

```elixir
defmodule DefdelegateCustom do
  @moduledoc """
  Custom `defdelegate` with Telemetry.

  Hand wrappers work for five functions; past that they rot. A macro emits identical `def` clauses with the target's `@spec` interpolated, so adding a delegate is one line and there....
  """
end
```
### `lib/defdelegate_custom/telemetry_helpers.ex`

**Objective**: Expose event_name/2 and span/3 so defproxy macro dispatches telemetry spans with normalized names.

```elixir
defmodule DefdelegateCustom.TelemetryHelpers do
  @moduledoc false

  @spec event_name(module(), atom()) :: [atom()]
  def event_name(mod, fun) do
    mod
    |> Module.split()
    |> Enum.map(&(&1 |> String.downcase() |> String.to_existing_atom()))
    |> Kernel.++([fun])
  end

  @spec span([atom()], map(), (-> result)) :: result when result: var
  def span(event_name, meta, fun) do
    :telemetry.span(event_name, meta, fn ->
      result = fun.()
      {result, Map.put(meta, :result_tag, tag(result))}
    end)
  end

  defp tag(:ok), do: :ok
  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :error
  defp tag(_), do: :ok
end
```
### `lib/defdelegate_custom/proxy_macro.ex`

**Objective**: Emit defproxy/2 macro that wraps target calls in telemetry.span for transparent start/stop/exception events.

```elixir
defmodule DefdelegateCustom.ProxyMacro do
  @moduledoc """
  Drop-in replacement for `defdelegate/2` that emits telemetry spans.

      use DefdelegateCustom.ProxyMacro

      defproxy create_user(params), to: MyApp.Users
  """

  alias DefdelegateCustom.TelemetryHelpers

  defmacro __using__(_opts) do
    quote do
      import DefdelegateCustom.ProxyMacro, only: [defproxy: 2]
    end
  end

  defmacro defproxy({name, _meta, args} = _head, opts) do
    target =
      Keyword.fetch!(opts, :to)
      |> Macro.expand(__CALLER__)

    remote_name = Keyword.get(opts, :as, name)

    arity = length(args || [])
    arg_vars = build_arg_vars(args || [])

    quote do
      @doc "Proxy for #{unquote(inspect(target))}.#{unquote(remote_name)}/#{unquote(arity)} with telemetry span."
      def unquote(name)(unquote_splicing(arg_vars)) do
        event = DefdelegateCustom.TelemetryHelpers.event_name(__MODULE__, unquote(name))

        DefdelegateCustom.TelemetryHelpers.span(
          event,
          %{target: unquote(target), arity: unquote(arity)},
          fn ->
            unquote(target).unquote(remote_name)(unquote_splicing(arg_vars))
          end
        )
      end
    end
  end

  defp build_arg_vars(args) do
    args
    |> Enum.with_index()
    |> Enum.map(fn
      {{name, _meta, ctx}, _i} when is_atom(name) and is_atom(ctx) ->
        Macro.var(name, __MODULE__)

      {_other, i} ->
        Macro.var(String.to_existing_atom("arg_#{i}"), __MODULE__)
    end)
  end
end
```
### Step 3: Example usage

**Objective**: Define sample target module and proxied API so users can trace forwarded function calls ergonomically.

```elixir
defmodule DefdelegateCustom.Sample.Users do
  @moduledoc false
  def create_user(%{valid: true} = p), do: {:ok, p}
  def create_user(_), do: {:error, :invalid}

  def find_user(id) when is_integer(id), do: {:ok, %{id: id}}
end

defmodule DefdelegateCustom.Sample.API do
  use DefdelegateCustom.ProxyMacro

  defproxy create_user(params), to: DefdelegateCustom.Sample.Users
  defproxy find_user(id),       to: DefdelegateCustom.Sample.Users
end
```
### `test/defdelegate_custom_test.exs`

**Objective**: Assert start/stop telemetry events fire, result tags match outcomes, and exceptions propagate with :exception events.

```elixir
defmodule DefdelegateCustom.ProxyMacroTest do
  use ExUnit.Case, async: true
  doctest DefdelegateCustom.Sample.Users

  alias DefdelegateCustom.Sample.API

  setup do
    parent = self()

    :telemetry.attach_many(
      :proxy_test,
      [
        [:defdelegate_custom, :sample, :api, :create_user, :start],
        [:defdelegate_custom, :sample, :api, :create_user, :stop],
        [:defdelegate_custom, :sample, :api, :create_user, :exception],
        [:defdelegate_custom, :sample, :api, :find_user, :start],
        [:defdelegate_custom, :sample, :api, :find_user, :stop]
      ],
      fn event, meas, meta, _ -> send(parent, {event, meas, meta}) end,
      nil
    )

    on_exit(fn -> :telemetry.detach(:proxy_test) end)
    :ok
  end

  describe "proxied calls" do
    test "forwards arguments and returns the target result" do
      assert {:ok, %{valid: true}} = API.create_user(%{valid: true})
    end

    test "emits start and stop spans" do
      API.find_user(42)
      assert_receive {[_, _, _, :find_user, :start], _, %{target: _}}
      assert_receive {[_, _, _, :find_user, :stop], %{duration: d}, _} when is_integer(d)
    end

    test "stop includes result tag" do
      API.create_user(%{valid: true})
      assert_receive {[_, _, _, :create_user, :stop], _, %{result_tag: :ok}}

      API.create_user(%{valid: false})
      assert_receive {[_, _, _, :create_user, :stop], _, %{result_tag: :error}}
    end

    test "emits :exception on raise" do
      defmodule Boom do
        def kaboom(_), do: raise("boom")
      end

      defmodule BoomAPI do
        use DefdelegateCustom.ProxyMacro
        defproxy kaboom(x), to: Boom
      end

      :telemetry.attach(
        :boom_test,
        [:defdelegate_custom, :proxy_macro_test, :boom_api, :kaboom, :exception],
        fn event, meas, meta, _ -> send(self(), {event, meas, meta}) end,
        nil
      )

      assert_raise RuntimeError, "boom", fn -> BoomAPI.kaboom(1) end
    after
      :telemetry.detach(:boom_test)
    end
  end
end
```
### Why this works

The macro captures the target `{mod, fun, arity}` at compile time and emits a `def` clause whose body is a direct remote call. The clause is a normal Elixir function — it appears in stacktraces, supports `@doc` and `@spec`, and is optimized like any other function.

---

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---

## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. `span/3` measures duration but also allocates.** The meta map is built on every
call. For very hot paths (> 1M calls/s), consider a conditional `if
:telemetry.list_handlers_for_event(...) != []`.

**2. Argument names may shadow caller variables.** Because you use `Macro.var(name,
__MODULE__)`, the generated proxy introduces a variable in the proxy module's
context — safe, but if the user writes `defproxy foo(user), to: X` and already has a
module-level `@user`, watch for confusion in error messages.

**3. Spec lost.** `defdelegate` preserves no `@spec` — neither does your macro by
default. For Dialyzer coverage, emit `@spec` by reading the target's typespecs via
`Code.Typespec.fetch_specs/1` at compile time.

**4. `:as` option.** The macro supports `as:` to rename. Keep it, since real
delegates rely on it (e.g. `Kernel.to_list/1` proxying `:erlang.tuple_to_list/1`).

**5. Default argument erasure.** If the target has `def foo(x \\ 1)`, the proxy
cannot "see" the default from the AST — it sees `x` without the `\\`. You have to
either require users to redeclare defaults or fetch `fun_info` from the target.

**6. Circular references.** `defproxy foo(x), to: __MODULE__` compiles but blows the
stack at runtime. Add an `env.module == target` check and raise `CompileError` if so.

**7. Cost vs `defdelegate`.** A telemetry-instrumented proxy is roughly 150 ns slower
per call than vanilla `defdelegate`. Measure before wrapping low-level hot paths.

**8. When NOT to use this.** For pure forwarding (no observability need), stick with
`defdelegate`. The cost is zero and the compile-time semantics are better understood
by tools (Dialyzer, formatter).

---

## Benchmark

```elixir
# bench/proxy_bench.exs
alias DefdelegateCustom.Sample.API

Benchee.run(%{
  "telemetry proxy (defproxy)" => fn -> API.find_user(42) end,
  "vanilla defdelegate"        => fn -> VanillaAPI.find_user(42) end
})
```
Expect ~100–200 ns overhead for the proxy when no handlers are attached.

---

## Reflection

- If your delegate needs to log every call or translate error shapes, do you still keep the macro form, or fall back to explicit wrappers? Where is the line?
- A teammate argues that `defdelegate` hides the real implementation and makes code harder to read. How do you answer with evidence from your benchmark and stacktraces?

---

### `script/main.exs`
```elixir
defmodule DefdelegateCustom.ProxyMacroTest do
  use ExUnit.Case, async: false
  doctest DefdelegateCustom.Sample.Users

  alias DefdelegateCustom.Sample.API

  setup do
    parent = self()

    :telemetry.attach_many(
      :proxy_test,
      [
        [:defdelegate_custom, :sample, :api, :create_user, :start],
        [:defdelegate_custom, :sample, :api, :create_user, :stop],
        [:defdelegate_custom, :sample, :api, :create_user, :exception],
        [:defdelegate_custom, :sample, :api, :find_user, :start],
        [:defdelegate_custom, :sample, :api, :find_user, :stop]
      ],
      fn event, meas, meta, _ -> send(parent, {event, meas, meta}) end,
      nil
    )

    on_exit(fn -> :telemetry.detach(:proxy_test) end)
    :ok
  end

  describe "proxied calls" do
    test "forwards arguments and returns the target result" do
      assert {:ok, %{valid: true}} = API.create_user(%{valid: true})
    end

    test "emits start and stop spans" do
      API.find_user(42)
      assert_receive {[_, _, _, :find_user, :start], _, %{target: _}}
      assert_receive {[_, _, _, :find_user, :stop], %{duration: d}, _} when is_integer(d)
    end

    test "stop includes result tag" do
      API.create_user(%{valid: true})
      assert_receive {[_, _, _, :create_user, :stop], _, %{result_tag: :ok}}

      API.create_user(%{valid: false})
      assert_receive {[_, _, _, :create_user, :stop], _, %{result_tag: :error}}
    end

    test "emits :exception on raise" do
      defmodule Boom do
        def kaboom(_), do: raise("boom")
      end

      defmodule BoomAPI do
        use DefdelegateCustom.ProxyMacro
        defproxy kaboom(x), to: Boom
      end

      :telemetry.attach(
        :boom_test,
        [:defdelegate_custom, :proxy_macro_test, :boom_api, :kaboom, :exception],
        fn event, meas, meta, _ -> send(self(), {event, meas, meta}) end,
        nil
      )

      assert_raise RuntimeError, "boom", fn -> BoomAPI.kaboom(1) end
    after
      :telemetry.detach(:boom_test)
    end
  end
end

defmodule Main do
  def main do
      # Demonstrate custom defdelegate with telemetry
      defmodule Upstream do
        def process_value(data), do: {:ok, String.upcase(data)}
      end

      defmodule Proxy do
        # Custom defproxy: delegate + emit telemetry
        defmacro defproxy(name, opts) do
          quote do
            def unquote(name)(data) do
              # Emit telemetry start
              :telemetry.execute([:proxy, :call, :start], %{}, %{func: unquote(name)})

              # Call upstream
              result = Upstream.unquote(name)(data)

              # Emit telemetry stop
              :telemetry.execute([:proxy, :call, :stop], %{}, %{func: unquote(name)})

              result
            end
          end
        end

        require Proxy
        defproxy(:process_value, to: Upstream)
      end

      # Test proxy
      result = Proxy.process_value("hello")

      IO.inspect(result, label: "✓ Proxied function result")
      assert match?({:ok, "HELLO"}, result), "Delegation works"

      IO.puts("✓ Custom defdelegate: telemetry-enabled proxying working")
  end
end

Main.main()
```
---

## Why Custom `defdelegate` with Telemetry matters

Mastering **Custom `defdelegate` with Telemetry** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

---

## Key concepts

### 1. Stdlib `defdelegate` is just a macro

Looking at `Kernel.defdelegate/2` source: it parses the function head, expands
`{name, args}`, and emits a `def name(args), do: target.name(args)`. That's it.
No telemetry, no error handling — pure forwarding.

### 2. Parsing a function head into `name` + `args`

```
{name, _meta, args} = quote do: create_user(params)
#=> name: :create_user, args: [{:params, [], nil}]
```

You need this to build both the wrapper's `def` and the target call.

### 3. Building the arg-reference list

When calling the target, you reference `params` as a variable — not re-interpolate its
default. `Macro.var/2` yields the correct contextual variable. The typical pattern:

```
arg_names = Enum.map(args, fn {name, _, _} -> Macro.var(name, __MODULE__) end)
```

### 4. `:telemetry.span/3`

Wraps a function, emitting `[... , :start]` before and `[... , :stop]` after with the
duration. Also emits `[... , :exception]` when the function raises. The span helper
lets you avoid writing this yourself.

### 5. Name generation

`[:my_app, :api, :create_user]` is the canonical event name format. Derive it from
`__MODULE__` + `name`, lowercasing the module segments.

---
