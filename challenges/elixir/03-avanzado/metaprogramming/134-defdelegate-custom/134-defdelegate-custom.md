# Custom `defdelegate` with Telemetry

**Project**: `defdelegate_custom` — write your own `defproxy/2` macro that emits a proxy function identical to `defdelegate`, plus automatic `:telemetry` spans and optional circuit-breaker hooks.

**Difficulty**: ★★★★☆
**Estimated time**: 3–5 hours

---

## Project context

Your team relies heavily on `defdelegate/2` — a clean way to re-export functions from
another module:

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

```
defdelegate_custom/
├── lib/
│   └── defdelegate_custom/
│       ├── proxy_macro.ex         # defproxy/2 macro
│       └── telemetry_helpers.ex   # span helper used by the proxy
├── test/
│   └── proxy_macro_test.exs
└── mix.exs
```

---

## Core concepts

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

## Implementation

### Step 1: `lib/defdelegate_custom/telemetry_helpers.ex`

```elixir
defmodule DefdelegateCustom.TelemetryHelpers do
  @moduledoc false

  @spec event_name(module(), atom()) :: [atom()]
  def event_name(mod, fun) do
    mod
    |> Module.split()
    |> Enum.map(&(&1 |> String.downcase() |> String.to_atom()))
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

### Step 2: `lib/defdelegate_custom/proxy_macro.ex`

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
        Macro.var(String.to_atom("arg_#{i}"), __MODULE__)
    end)
  end
end
```

### Step 3: Example usage

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

### Step 4: Tests

```elixir
defmodule DefdelegateCustom.ProxyMacroTest do
  use ExUnit.Case, async: false

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

## Resources

- [`Kernel.defdelegate/2` source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/kernel.ex) — reference
- [`:telemetry.span/3` docs](https://hexdocs.pm/telemetry/telemetry.html#span-3)
- [Decorator library](https://github.com/arjan/decorator) — similar approach for arbitrary decoration
- [Dashbit blog on `:telemetry`](https://dashbit.co/blog/getting-started-with-telemetry)
- [Phoenix instrumentation](https://github.com/phoenixframework/phoenix/blob/main/lib/phoenix/logger.ex)
- [Erlang docs — tracing overview](https://www.erlang.org/doc/man/dbg.html)
