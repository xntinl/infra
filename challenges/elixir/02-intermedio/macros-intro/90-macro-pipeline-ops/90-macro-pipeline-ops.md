# Custom pipeline operators with defmacro

**Project**: `custom_pipe` — implement `~>` (ok-bind) and `|~>` (maybe-pipe) operators using `defmacro`, mirroring the Haskell-ish `Result` and `Maybe` plumbing Elixir programmers rebuild by hand.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Elixir's `|>` passes a value as the first argument of the next call. It's
powerful, but it breaks the moment you're threading an `{:ok, value}` /
`{:error, _}` tuple through a chain: you end up with `case` towers or a
`with` block that doesn't compose neatly across modules.

This exercise builds two custom operators:

- `a ~> b` — "ok-bind": if `a` evaluates to `{:ok, value}`, call `b(value)`;
  otherwise short-circuit and return the error tuple unchanged.
- `a |~> b` — "maybe-pipe": if `a` is `nil`, propagate `nil`; otherwise pipe
  `a` into `b`.

Because Elixir is homoiconic and the parser treats any `a OP b` as
`{:OP, _, [a, b]}`, you can define operators by simply writing a
`defmacro` with the operator as its name. No grammar changes, no plugins.

Project structure:

```
custom_pipe/
├── lib/
│   └── custom_pipe.ex
├── test/
│   └── custom_pipe_test.exs
└── mix.exs
```

---

## Core concepts

### 1. A binary operator is just a 2-arg macro

If the macro is named the same as an existing operator symbol, you can
override it inside modules that `import` it. The allowed operator
identifiers are fixed by the parser; `~>` and `|~>` are both valid.

### 2. Short-circuiting requires `case` inside `quote`

A macro can't "return early" — it always returns AST. To short-circuit
you emit a `case` that decides at runtime:

```elixir
quote do
  case unquote(left) do
    {:ok, value} -> unquote(right).(value)
    {:error, _} = err -> err
    other -> raise "expected {:ok, _} or {:error, _}, got: #{inspect(other)}"
  end
end
```

### 3. Right-hand side is a *call*, not a value

With `|>`, the right side is always a call expression: `foo(a, b)` means
"insert the piped value as the first argument." You want the same:
`{:ok, 1} ~> double()` should expand so that `double` receives the
unwrapped `1`. This is exactly what `Macro.pipe/3` does — reusing it
keeps semantics identical to `|>`.

### 4. Operator precedence is fixed by the parser

You don't pick the precedence of your custom operator — it's determined by
its symbol. `~>` and `|~>` share precedence with `|>`. In practice this
means the usual pipe spacing conventions work unchanged.

---

## Implementation

### Step 1: Create the project

```bash
mix new custom_pipe
cd custom_pipe
```

### Step 2: `lib/custom_pipe.ex`

```elixir
defmodule CustomPipe do
  @moduledoc """
  Custom pipeline operators for Result and Maybe threading.

      import CustomPipe

      {:ok, 2}
      ~> double()
      ~> to_string()
      #=> {:ok, "4"}

      {:error, :boom}
      ~> double()
      #=> {:error, :boom}

      value |~> String.upcase()
  """

  @doc """
  Ok-bind. Short-circuits on `{:error, _}`.

  Expansion: `left ~> right_call` becomes

      case left do
        {:ok, v}            -> Macro.pipe(v, right_call, 0) wrapped in {:ok, _}
        {:error, _} = err   -> err
      end
  """
  defmacro left ~> right do
    # Reuse Macro.pipe to inherit |> semantics: "insert as 1st arg".
    piped = Macro.pipe(quote(do: value), right, 0)

    quote do
      case unquote(left) do
        {:ok, value} ->
          case unquote(piped) do
            {:ok, _} = ok -> ok
            {:error, _} = err -> err
            plain -> {:ok, plain}
          end

        {:error, _} = err ->
          err

        other ->
          raise ArgumentError,
                "~> expected {:ok, _} or {:error, _}, got: #{inspect(other)}"
      end
    end
  end

  @doc """
  Maybe-pipe. Short-circuits on `nil`; otherwise behaves exactly like `|>`.
  """
  defmacro left |~> right do
    piped = Macro.pipe(quote(do: value), right, 0)

    quote do
      case unquote(left) do
        nil -> nil
        value -> unquote(piped)
      end
    end
  end
end
```

### Step 3: `test/custom_pipe_test.exs`

```elixir
defmodule CustomPipeTest do
  use ExUnit.Case, async: true
  import CustomPipe

  # Helpers used inside the pipelines.
  defp double(n), do: n * 2
  defp safe_div(_, 0), do: {:error, :zero}
  defp safe_div(a, b), do: {:ok, div(a, b)}

  describe "~> (ok-bind)" do
    test "threads through on :ok" do
      result = {:ok, 2} ~> double() ~> to_string()
      assert result == {:ok, "4"}
    end

    test "short-circuits on :error" do
      result = {:error, :nope} ~> double()
      assert result == {:error, :nope}
    end

    test "keeps existing :ok wrappers untouched" do
      result = {:ok, 10} ~> safe_div(5)
      # safe_div(10, 5) => {:ok, 2}, should NOT double-wrap.
      assert result == {:ok, 2}
    end

    test "propagates a new :error from the RHS" do
      result = {:ok, 10} ~> safe_div(0)
      assert result == {:error, :zero}
    end

    test "raises on a non-result value" do
      assert_raise ArgumentError, fn ->
        apply(fn -> 42 ~> double() end, [])
      end
    end
  end

  describe "|~> (maybe-pipe)" do
    test "pipes when the value is not nil" do
      result = "hello" |~> String.upcase()
      assert result == "HELLO"
    end

    test "short-circuits on nil" do
      result = nil |~> String.upcase()
      assert result == nil
    end

    test "composes multiple steps" do
      result = "  hi  " |~> String.trim() |~> String.upcase()
      assert result == "HI"
    end
  end
end
```

### Step 4: Run

```bash
mix test
```

Expand a pipeline in IEx to see what the macro produces:

```
iex> require CustomPipe
iex> import CustomPipe
iex> ast = quote do: {:ok, 1} ~> double()
iex> Macro.expand(ast, __ENV__) |> Macro.to_string() |> IO.puts
```

---

## Trade-offs and production gotchas

**1. Custom operators raise the bar for new readers**
A developer opening the file for the first time has to learn what `~>`
means before they can read anything. If the project has three custom
operators scattered across five modules, onboarding cost balloons.
Prefer a named macro (`ok_pipe`, `maybe_pipe`) or a library (`monad_ex`,
`ok`) if you expect broad usage.

**2. Semantics of "error" values must be locked down**
Is `{:error, _, _}` an error? Is a bare atom `:error` an error? Your
macro needs one explicit convention and failure mode for anything else —
otherwise you ship a footgun. The example above picks strict
`{:ok, _}` / `{:error, _}` and raises on everything else.

**3. `Macro.pipe/3` is your friend**
Rolling your own "insert as first arg" logic is how you introduce subtle
differences from `|>`. Reuse `Macro.pipe/3` so that the user's intuition
transfers 1:1 from the built-in pipe.

**4. Operator overloading is visible in stack traces**
If your macro raises an `ArgumentError`, the trace points at the call
site but not at which of three chained operators failed. Include the
original value in the error message so the user doesn't need to bisect.

**5. Dialyzer has opinions about `~>` on tuples**
Custom operators aren't `@spec`-able, so Dialyzer can only infer types
through the expanded AST. For libraries, write helper *functions*
(`ok_bind/2`) alongside the macro so users who want typing get it.

**6. When NOT to ship custom operators**
In application code, almost always. `with/1` handles the same use cases
with no extra vocabulary for readers. Custom operators earn their keep
in library code where they appear dozens of times and the reader
investment pays off. If you find yourself writing `~>` three times in
one module, use `with` instead.

---

## Resources

- [`Macro.pipe/3`](https://hexdocs.pm/elixir/Macro.html#pipe/3) — how `|>` is actually implemented
- [`Kernel.SpecialForms.with/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1) — the built-in alternative to ok-bind
- ["Operators" section of the Elixir guide](https://hexdocs.pm/elixir/operators.html) — which operator symbols are available for custom definition
- [`ok` library](https://hexdocs.pm/ok/readme.html) — a production-grade Result-pipeline library
- ["Metaprogramming Elixir" — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/), chapter on DSLs
