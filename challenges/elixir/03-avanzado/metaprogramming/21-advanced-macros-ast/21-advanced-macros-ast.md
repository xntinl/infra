# Advanced Macros and AST Surgery

**Project**: `ast_surgeon` — a toolkit that rewrites Elixir source code at compile time by walking and transforming the AST (Abstract Syntax Tree).

---

## Project context

Your platform team maintains a large Elixir monorepo with ~300 modules. The security team
wants every call to `HTTPoison.get/1`, `HTTPoison.post/2`, and friends to be wrapped in
a tracing helper that emits `:telemetry` events and enforces a timeout. Manually editing
300 files is out of the question — and even if you did, a developer adding a new call
next week would bypass the instrumentation.

The right answer is a **compile-time AST transformation**: a macro that scans the quoted
expression of a module body and rewrites every `HTTPoison.<fun>(args)` call into
`Traced.HTTPoison.<fun>(args)`. This is exactly what libraries like `Boundary` and
`Credo` do internally — they read the AST and either validate or rewrite it.

The same technique powers Phoenix's `~H` sigil, Ecto's `from` query builder, and Nx's
`defn` — all of them are macros that walk a quoted expression and produce a new one.

```
ast_surgeon/
├── lib/
│   └── ast_surgeon/
│       ├── rewriter.ex         # Macro.prewalk / postwalk driver
│       ├── transforms.ex       # individual transformation rules
│       └── traced_http.ex      # runtime helper used after rewrite
├── test/
│   └── ast_surgeon/
│       ├── rewriter_test.exs
│       └── transforms_test.exs
└── mix.exs
```

---

## Why AST manipulation and not string building

String-built code loses all Elixir's scoping, macro hygiene, and source-location info. AST code keeps each node anchored to an original source location, so a broken macro points at the user's line, not at a generated blob.

---

## Core concepts

### 1. Quoted expressions are tagged tuples

Every Elixir expression, when quoted, becomes a 3-tuple `{call, meta, args}` or a literal.

```
quote do: a + 1
#=> {:+, [context: Elixir, imports: [...]], [{:a, [], Elixir}, 1]}
```

Literals (atoms, integers, floats, empty list, 2-tuples of literals, binaries) are
represented as themselves. Everything else is a 3-tuple. This is what makes traversal tractable.

### 2. `Macro.prewalk/2` vs `Macro.postwalk/2`

- `prewalk` visits a node **before** its children — useful when the transformation
  depends on outer context, e.g. wrapping a call without recursing into the wrapped form.
- `postwalk` visits **after** children — useful when child transformations must
  complete first, e.g. constant folding where `1 + 2` becomes `3` before the surrounding
  `3 * x`.

```
        expr: f(g(x))
        ┌────────────┐
prewalk │ visits f/1 │ ──▶ then g/1 ──▶ then x
        └────────────┘
postwalk visits x ──▶ then g/1 ──▶ then f/1
```

### 3. `Macro.traverse/4` — the two-pass powerhouse

When you need both pre and post handlers with shared accumulator state:

```
Macro.traverse(ast, acc,
  fn node, acc -> {maybe_transformed, new_acc} end,   # pre
  fn node, acc -> {maybe_transformed, new_acc} end    # post
)
```

This is what Credo uses to both gather metadata (pre) and emit issues (post).

### 4. Hygienic vs unhygienic substitution

When a macro injects a variable like `var!(result)`, it deliberately breaks hygiene so
the caller can reference it. Silent hygiene violations are a common AST-surgery bug —
always use `Macro.var(name, context)` when building new variables to guarantee the
correct context, and `Macro.unique_var/2` when you want a fresh collision-free name.

### 5. `Macro.expand/2` vs raw AST

Some nodes look like function calls but are actually macros that would expand further
(e.g. `|>`, `if`, `unless`). If your rewriter ignores expansion, it will miss logic
that only becomes visible after `Macro.expand/2`. For rewriters that operate on
user-authored code (not post-expansion), this is usually fine — document it.

---

## Design decisions

**Option A — build code by string concatenation and `Code.eval_string/1`**
- Pros: feels like templating; trivial to prototype.
- Cons: no scope safety, horrific error messages, injection-adjacent in every sense.

**Option B — operate directly on the AST with `quote` / `unquote`** (chosen)
- Pros: hygienic by default, compile-time errors point to the right line, survives refactors.
- Cons: requires understanding the three-element tuple shape and special forms.

→ Chose **B** because `Code.eval_string/1` is a dead end for production macros; the AST is the only path that scales.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Declare telemetry and benchee so AST rewrite transformation and tracing harness compile idempotently.

```elixir
defmodule AstSurgeon.MixProject do
  use Mix.Project

  def project do
    [app: :ast_surgeon, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger, :telemetry]]

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule AstSurgeon.MixProject do
  use Mix.Project

  def project do
    [app: :ast_surgeon, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application, do: [extra_applications: [:logger, :telemetry]]

  defp deps do
    [{:telemetry, "~> 1.2"}, {:benchee, "~> 1.3", only: :dev}]
  end
end
```

### Step 2: `lib/ast_surgeon/traced_http.ex` — the runtime target

**Objective**: Emit telemetry events pre/post so rewritten HTTP call sites have observable latency and error context.

```elixir
defmodule AstSurgeon.TracedHTTP do
  @moduledoc """
  Runtime wrapper that the AST rewrite targets. Emits telemetry.
  """

  @spec get(String.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def get(url, opts \\ []), do: traced(:get, [url, opts])

  @spec post(String.t(), binary(), keyword()) :: {:ok, map()} | {:error, term()}
  def post(url, body, opts \\ []), do: traced(:post, [url, body, opts])

  defp traced(verb, args) do
    start = System.monotonic_time()
    :telemetry.execute([:ast_surgeon, :http, :start], %{system_time: start}, %{verb: verb})

    result =
      try do
        simulated_call(verb, args)
      catch
        kind, reason -> {:error, {kind, reason}}
      end

    duration = System.monotonic_time() - start
    :telemetry.execute([:ast_surgeon, :http, :stop], %{duration: duration}, %{verb: verb})
    result
  end

  defp simulated_call(:get, [url | _]), do: {:ok, %{status: 200, url: url}}
  defp simulated_call(:post, [url, _body | _]), do: {:ok, %{status: 201, url: url}}
end
```

### Step 3: `lib/ast_surgeon/transforms.ex`

**Objective**: Compose pure postwalk-friendly transforms: HTTPoison alias redirection and constant folding optimizations.

```elixir
defmodule AstSurgeon.Transforms do
  @moduledoc """
  Individual AST transformation rules. Each takes a node and returns a
  possibly-rewritten node. Composed by `Rewriter`.
  """

  @doc """
  Rewrites `HTTPoison.get(...)` / `HTTPoison.post(...)` into the traced helper.
  """
  @spec rewrite_http(Macro.t()) :: Macro.t()
  def rewrite_http({{:., meta1, [{:__aliases__, alias_meta, [:HTTPoison]}, verb]}, meta2, args})
      when verb in [:get, :post] do
    target = {:__aliases__, alias_meta, [:AstSurgeon, :TracedHTTP]}
    {{:., meta1, [target, verb]}, meta2, args}
  end

  def rewrite_http(node), do: node

  @doc """
  Constant-folds simple arithmetic on literal integers — demonstrates postwalk
  bottom-up simplification.
  """
  @spec fold_constants(Macro.t()) :: Macro.t()
  def fold_constants({op, _meta, [a, b]})
      when op in [:+, :-, :*] and is_integer(a) and is_integer(b) do
    case op do
      :+ -> a + b
      :- -> a - b
      :* -> a * b
    end
  end

  def fold_constants(node), do: node
end
```

### Step 4: `lib/ast_surgeon/rewriter.ex`

**Objective**: Orchestrate postwalk composition and define deftraced DSL so HTTP calls rewrite transparently at compile time.

```elixir
defmodule AstSurgeon.Rewriter do
  @moduledoc """
  Drives AST walks. Exposes `rewrite/2` for programmatic use and `deftraced/2`
  for the DSL style.
  """

  alias AstSurgeon.Transforms

  @type transform :: (Macro.t() -> Macro.t())

  @spec rewrite(Macro.t(), [transform]) :: Macro.t()
  def rewrite(ast, transforms) do
    Macro.postwalk(ast, fn node ->
      Enum.reduce(transforms, node, fn t, acc -> t.(acc) end)
    end)
  end

  @spec collect(Macro.t(), (Macro.t() -> boolean())) :: [Macro.t()]
  def collect(ast, predicate) do
    {_, acc} =
      Macro.prewalk(ast, [], fn node, acc ->
        if predicate.(node), do: {node, [node | acc]}, else: {node, acc}
      end)

    Enum.reverse(acc)
  end

  defmacro __using__(_opts) do
    quote do
      import AstSurgeon.Rewriter, only: [deftraced: 2]
    end
  end

  @doc """
  Defines a function whose body is rewritten by the default transform list
  (currently `rewrite_http/1`). Compile-time transformation, zero runtime cost.
  """
  defmacro deftraced(call, do: body) do
    new_body = rewrite(body, [&Transforms.rewrite_http/1])

    quote do
      def unquote(call), do: unquote(new_body)
    end
  end
end
```

### Step 5: Tests

**Objective**: Assert AST node substitution succeeds, telemetry payload matches context, and nested calls receive rewrites.

```elixir
defmodule AstSurgeon.TransformsTest do
  use ExUnit.Case, async: true
  alias AstSurgeon.Transforms

  describe "rewrite_http/1" do
    test "rewrites HTTPoison.get into TracedHTTP.get" do
      ast = quote do: HTTPoison.get("https://example.com")
      new_ast = Macro.postwalk(ast, &Transforms.rewrite_http/1)
      source = Macro.to_string(new_ast)
      assert source =~ "AstSurgeon.TracedHTTP.get"
      refute source =~ "HTTPoison.get"
    end

    test "leaves unrelated calls alone" do
      ast = quote do: Enum.map([1, 2], &(&1 + 1))
      assert Macro.postwalk(ast, &Transforms.rewrite_http/1) == ast
    end
  end

  describe "fold_constants/1" do
    test "reduces 1 + 2 * 3 to 7 bottom-up" do
      ast = quote do: 1 + 2 * 3
      assert Macro.postwalk(ast, &Transforms.fold_constants/1) == 7
    end

    test "ignores non-literal operands" do
      ast = quote do: x + 1
      assert Macro.postwalk(ast, &Transforms.fold_constants/1) == ast
    end
  end
end

defmodule AstSurgeon.RewriterTest do
  use ExUnit.Case, async: true

  defmodule Subject do
    use AstSurgeon.Rewriter

    deftraced fetch(url) do
      HTTPoison.get(url)
    end
  end

  test "deftraced rewrites body to call TracedHTTP" do
    assert {:ok, %{status: 200, url: "https://x"}} = Subject.fetch("https://x")
  end

  test "telemetry events fire" do
    parent = self()

    :telemetry.attach_many(
      :ast_surgeon_test,
      [[:ast_surgeon, :http, :start], [:ast_surgeon, :http, :stop]],
      fn event, meas, meta, _ -> send(parent, {event, meas, meta}) end,
      nil
    )

    Subject.fetch("https://x")

    assert_receive {[:ast_surgeon, :http, :start], _, %{verb: :get}}
    assert_receive {[:ast_surgeon, :http, :stop], %{duration: _}, _}
  after
    :telemetry.detach(:ast_surgeon_test)
  end
end
```

### Why this works

`quote` reifies an expression into an AST tuple tree without evaluating it. `unquote` injects a runtime value into that tree during compilation. The result is a plain AST the compiler treats identically to hand-written source.

---


## Key Concepts: AST Manipulation and Code Generation

Macros receive the AST (Abstract Syntax Tree) of their arguments as quoted expressions, transform them, and return new code to be injected into the caller's context. `quote` captures code as a data structure; `unquote` breaks the quote to insert dynamic values. `quote do ... end` returns a quoted form; you can manipulate it with pattern matching, then return it for the compiler to expand.

The power: macros can generate repetitive boilerplate automatically. The danger: they're hard to debug (the error occurs at compile time, not at the point of use), and they can create confusing code. A rule of thumb: write your code imperatively first, then extract the pattern into a macro. For simple code generation, consider a function that returns quoted code instead of a macro—it's easier to understand and test.


## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. Compile-time cost scales with AST size.** Every `prewalk`/`postwalk` is O(n) in nodes.
A 10k-LOC module can have ~300k AST nodes. Running 20 transforms sequentially = 6M ops
per compile. Combine transforms into a single walk when possible.

**2. Macro debugging is slippery.** `IO.inspect` inside a macro prints at compile time,
not runtime. Use `Macro.to_string/1` to see generated source, and
`Macro.expand_once/2` to step expansion manually..

**3. Hygiene surprises.** Variables you introduce inside the quoted output carry the
macro's context, not the caller's. If you need the caller to see them, use `var!/1` and
document it.

**4. `@before_compile` runs once per module.** State accumulated across multiple macro
invocations lives in module attributes. Accumulator attributes are the
right tool.

**5. AST rewriting across abstraction boundaries is brittle.** If your user writes
`alias HTTPoison, as: H` and then `H.get(...)`, your pattern on
`{:__aliases__, _, [:HTTPoison]}` will miss it. Either expand aliases
(`Macro.expand/2` with an `env`) or document the limitation.

**6. Rewriting macros, not just function calls.** Nodes like `|>`, `for`, `with` are
macros. Decide deliberately whether you operate before or after their expansion.

**7. When NOT to use this.** If the rewrite can be expressed as a normal function wrapper
(`def my_get(url), do: Traced.get(url)` + a Credo rule to ban direct calls), prefer that.
AST surgery is only justified when the rewrite is invisible to callers, enforces a
cross-cutting concern, or eliminates boilerplate across dozens of modules.

---

## Benchmark

```elixir
# bench/rewrite_bench.exs
ast =
  Enum.reduce(1..1_000, quote(do: :ok), fn _, acc ->
    quote do
      HTTPoison.get("https://example.com")
      unquote(acc)
    end
  end)

Benchee.run(%{
  "postwalk rewrite" => fn ->
    Macro.postwalk(ast, &AstSurgeon.Transforms.rewrite_http/1)
  end,
  "prewalk rewrite" => fn ->
    Macro.prewalk(ast, &AstSurgeon.Transforms.rewrite_http/1)
  end
})
```

Expect ~1–3 ms for 1k-node ASTs on modern hardware. Compile time only — zero runtime cost.

---

## Reflection

- Your macro needs to introduce a helper function the user did not ask for. How do you name it so it does not collide with user code, without relying on user trust?
- A macro works in your test module but breaks when a user aliases one of its symbols. What did you do wrong, and what does the fix look like?

---

## Resources

- [`Macro` — hexdocs.pm](https://hexdocs.pm/elixir/Macro.html) — canonical reference
- [*Metaprogramming Elixir* — Chris McCord](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/) — still the best intro
- [Phoenix.HTML.Engine source](https://github.com/phoenixframework/phoenix_html/blob/main/lib/phoenix_html/engine.ex) — real-world AST walk
- [Credo's AST code module](https://github.com/rrrene/credo/tree/master/lib/credo/code) — production-grade traversal
- [Dashbit blog — compile-time techniques](https://dashbit.co/blog) — José Valim
- [`Macro` source in Elixir](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/macro.ex) — read `traverse/4`
