# Internal DSL with `Macro.prewalk` and `Kernel.SpecialForms`

**Project**: `rule_engine` — small internal DSL for business rules (`rule :foo, when: ..., then: ...`) that is rewritten by `Macro.prewalk/2` into plain Elixir functions at compile time. Zero runtime overhead; every rule compiles to a function head.

## Project context

Business teams often write rules in prose ("if customer tier is gold and subtotal > 100, apply 10% discount"). Shipping that as data (JSON, a table) requires a runtime interpreter — slow, fragile, untyped. Shipping it as Elixir gives you the compiler for free but puts the rule logic out of the business team's reach unless the shape is friendly.

An internal DSL is the middle path: you give the rule author a tiny vocabulary that looks like Elixir, and at compile time you rewrite it into idiomatic Elixir that the compiler type-checks and optimises. The author's rule becomes a function; the mismatches become compile errors.

This exercise builds `RuleEngine`, a DSL where each `rule` declaration produces a function clause in the host module. The DSL uses `Macro.prewalk/2` to rewrite allowed primitives (comparisons, logical operators) and rejects anything outside the vocabulary with a compile error. The entry point is a macro — no runtime parser.

```
rule_engine/
├── lib/
│   ├── rule_engine.ex                # use macro + rule/2 macro + prewalk
│   ├── rule_engine/
│   │   ├── guards.ex                 # allowed operator whitelist
│   │   └── errors.ex
│   └── discount_rules.ex             # example use of the DSL
├── test/
│   ├── rule_engine_test.exs
│   └── discount_rules_test.exs
└── mix.exs
```

## Why an internal DSL and not a data format

A JSON rule (`{"when": {">": ["subtotal", 100]}, "then": {"discount": 0.1}}`) requires a runtime evaluator — a stack machine, or a recursive interpreter. That evaluator must handle every edge case (missing field, type mismatch, division by zero) with runtime error handling. Elixir already has all that, free. By making rules Elixir-shaped code, you get:

- compile-time type inference on comparisons,
- `match_spec`-friendly pattern matching,
- coverage of the rule set by `mix test`,
- source-mapped stack traces when a rule crashes.

The cost: deploying a rule change requires a compile and a deploy. If that is unacceptable (rules must change without a deploy), you want the data approach — it is not what this exercise is for.

## Why `Macro.prewalk` and not `Macro.postwalk`

`Macro.prewalk/2` visits nodes top-down: parent first, then children. It lets you *replace* a node with a different tree before the children get walked. `Macro.postwalk/2` visits bottom-up: children first, then the parent sees already-rewritten children. For a whitelist, prewalk is cleaner because you decide "does this node belong?" before drilling into its arguments.

## Core concepts

### 1. Quoted expression
Elixir source parsed into a 3-tuple AST: `{name, meta, args}`. Literals are themselves.

### 2. `Macro.prewalk/2`
Walks a quoted expression top-down, applying a transformation at each node.

### 3. `Kernel.SpecialForms`
The built-in forms that the compiler understands directly: `=`, `case`, `fn`, `quote`, `unquote`, `__aliases__`, etc. They are not overridable.

### 4. `unquote` / `unquote_splicing`
Inside `quote do ... end`, `unquote(var)` injects a runtime value into the AST. `unquote_splicing(list)` inlines a list of nodes.

### 5. Compile-time error
`raise CompileError, description: "..."` from inside a macro surfaces as a red mix error at the rule author's file:line.

## Design decisions

- **Option A — compile each rule into a function clause** with guards expressing the `when:` condition.
- **Option B — compile into a single function that pattern matches a `{facts, rule_name}` tuple**.

→ A. Function clauses are the idiomatic way to dispatch in Elixir, and the VM already optimises multi-clause dispatch.

- **Option A — explicit whitelist of operators** (`>`, `<`, `==`, `and`, `or`, `in`).
- **Option B — free Elixir inside `when:` and `then:`**.

→ A. Otherwise the DSL is just "inline Elixir" and the author can call `System.cmd/2` or `File.rm_rf!/1` from a rule. A whitelist is a safety boundary.

## Implementation

### Dependencies (`mix.exs`)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # No external dependencies — pure Elixir
  ]
end
```

```elixir
defp deps, do: []
```

### Step 1: Whitelist and errors

**Objective**: Define Guards whitelist >, <, >=, <=, ==, !=, and, or, not, in, . and InvalidRule exception for DSL safety.

```elixir
defmodule RuleEngine.Errors do
  defmodule InvalidRule do
    defexception [:message]
  end
end

defmodule RuleEngine.Guards do
  @moduledoc false

  # Operators and functions allowed inside :when expressions.
  @allowed_ops [:>, :<, :>=, :<=, :==, :!=, :and, :or, :not, :in, :.]

  # Facts are accessed as atoms inside the rule. We allow Map.get/2 expansion.
  @allowed_fact_keys :any

  def allowed_op?(name) when name in @allowed_ops, do: true
  def allowed_op?(_), do: false

  def allowed_ops, do: @allowed_ops
end
```

### Step 2: The DSL

**Objective**: Implement __using__, rule/2 macro that uses Macro.prewalk to validate when/then, emits function clauses with guards.

```elixir
defmodule RuleEngine do
  @moduledoc """
  Declarative business rules DSL.

      defmodule DiscountRules do
        use RuleEngine

        rule :gold_bulk,
          when: tier == :gold and subtotal > 100,
          then: {:discount, 0.10}

        rule :silver_bulk,
          when: tier == :silver and subtotal > 200,
          then: {:discount, 0.05}
      end

      iex> DiscountRules.evaluate(%{tier: :gold, subtotal: 150})
      [{:discount, 0.10}]
  """

  alias RuleEngine.Guards
  alias RuleEngine.Errors.InvalidRule

  defmacro __using__(_opts) do
    quote do
      import RuleEngine, only: [rule: 2]
      Module.register_attribute(__MODULE__, :rules, accumulate: true)
      @before_compile RuleEngine
    end
  end

  defmacro rule(name, opts) when is_atom(name) and is_list(opts) do
    condition = Keyword.fetch!(opts, :when)
    action    = Keyword.fetch!(opts, :then)

    # Walk the condition AST and verify every operator is whitelisted.
    # Simultaneously rewrite bare variables into `Map.fetch!(facts, :name)`.
    rewritten =
      Macro.prewalk(condition, fn
        # Allowed binary/unary ops
        {op, meta, args} = node when is_list(args) ->
          if op in Guards.allowed_ops() or macro_allowed?(op) do
            node
          else
            {op, meta, args} |> ensure_fact_access()
          end

        literal ->
          literal
      end)

    quote bind_quoted: [name: name, rewritten: Macro.escape(rewritten), action: Macro.escape(action)] do
      @rules {name, rewritten, action}
    end
    |> then(fn ast ->
      # We need to generate the function BEFORE __before_compile__ at the macro call site too,
      # but keeping definitions inside @before_compile makes ordering deterministic.
      ast
    end)
  end

  defmacro __before_compile__(env) do
    rules = Module.get_attribute(env.module, :rules) |> Enum.reverse()

    clauses =
      for {name, cond_ast, action_ast} <- rules do
        {cond_expanded, _} =
          Macro.prewalk(cond_ast, [], fn
            {op, _, _} = node, acc when is_atom(op) ->
              if op in RuleEngine.Guards.allowed_ops() or op in [:facts, :., :->, :__block__] do
                {node, acc}
              else
                raise_invalid(name, op)
              end

            {var, _meta, ctx} = node, acc when is_atom(var) and is_atom(ctx) ->
              # Bare variable like `tier` → Map.fetch!(facts, :tier)
              {quote(do: Map.fetch!(var!(facts), unquote(var))), acc}

            other, acc ->
              {other, acc}
          end)

        quote do
          def evaluate_rule(unquote(name), var!(facts)) when is_map(var!(facts)) do
            if unquote(cond_expanded) do
              {:match, unquote(Macro.escape(action_ast))}
            else
              :no_match
            end
          end
        end
      end

    dispatcher =
      quote do
        @doc "Evaluate all rules against `facts` and collect actions of matching rules."
        def evaluate(facts) when is_map(facts) do
          for {name, _, _} <- __rules__(),
              match?({:match, _}, evaluate_rule(name, facts)),
              do: elem(evaluate_rule(name, facts), 1)
        end

        @doc "Returns the declared rules for introspection."
        def __rules__, do: unquote(Macro.escape(rules))
      end

    quote do
      (unquote_splicing(clauses))
      unquote(dispatcher)
    end
  end

  # --- helpers ---

  defp macro_allowed?(:__aliases__), do: true
  defp macro_allowed?(:__block__), do: true
  defp macro_allowed?(_), do: false

  defp ensure_fact_access(node), do: node

  defp raise_invalid(rule_name, op) do
    raise InvalidRule,
      message:
        "rule #{inspect(rule_name)} uses disallowed operator #{inspect(op)}. " <>
          "Allowed operators: #{inspect(RuleEngine.Guards.allowed_ops())}."
  end
end
```

### Step 3: Example rules module

**Objective**: Define DiscountRules using rule DSL with gold_bulk, silver_bulk, first_purchase rules to demonstrate ergonomics.

```elixir
defmodule DiscountRules do
  use RuleEngine

  rule :gold_bulk,
    when: tier == :gold and subtotal > 100,
    then: {:discount, 0.10}

  rule :silver_bulk,
    when: tier == :silver and subtotal > 200,
    then: {:discount, 0.05}

  rule :first_purchase,
    when: lifetime_orders == 0 and subtotal > 0,
    then: {:discount, 0.15}
end
```

## Transformation diagram

```
Source                                 Parsed AST
─────────────────────────────────────  ─────────────────────────────────────
rule :gold_bulk,                       {:rule, _, [:gold_bulk, [
  when: tier == :gold and subtotal > 100,   {:when, {:and, _, [
  then: {:discount, 0.10}                      {:==, _, [tier, :gold]},
                                               {:>, _, [subtotal, 100]}]}},
                                          {:then, {:discount, 0.10}}]]}

                 ▼ Macro.prewalk rewrites
                   bare variables → Map.fetch!(facts, :name)

After rewrite (conceptual):
def evaluate_rule(:gold_bulk, facts) when is_map(facts) do
  if Map.fetch!(facts, :tier) == :gold and Map.fetch!(facts, :subtotal) > 100 do
    {:match, {:discount, 0.10}}
  else
    :no_match
  end
end
```

## Tests

```elixir
defmodule DiscountRulesTest do
  use ExUnit.Case, async: true

  describe "evaluate/1" do
    test "matches gold_bulk" do
      assert [{:discount, 0.10}] =
               DiscountRules.evaluate(%{tier: :gold, subtotal: 150, lifetime_orders: 10})
    end

    test "matches first_purchase separately" do
      assert [{:discount, 0.15}] =
               DiscountRules.evaluate(%{tier: :bronze, subtotal: 50, lifetime_orders: 0})
    end

    test "matches multiple rules" do
      facts = %{tier: :gold, subtotal: 200, lifetime_orders: 0}
      results = DiscountRules.evaluate(facts)
      assert {:discount, 0.10} in results
      assert {:discount, 0.15} in results
    end

    test "no match returns empty" do
      assert [] = DiscountRules.evaluate(%{tier: :bronze, subtotal: 10, lifetime_orders: 5})
    end
  end

  describe "introspection" do
    test "__rules__/0 lists declared rules" do
      names = DiscountRules.__rules__() |> Enum.map(&elem(&1, 0))
      assert :gold_bulk in names
      assert :silver_bulk in names
      assert :first_purchase in names
    end
  end
end
```

```elixir
defmodule RuleEngineTest do
  use ExUnit.Case, async: false

  describe "compile-time validation" do
    test "rejects disallowed operator" do
      ast =
        quote do
          defmodule BadRules do
            use RuleEngine

            rule :dangerous,
              when: System.cmd("rm", ["-rf", "/"]) == {"", 0},
              then: :boom
          end
        end

      assert_raise RuleEngine.Errors.InvalidRule, ~r/disallowed operator/, fn ->
        Code.eval_quoted(ast)
      end
    end
  end
end
```

## Benchmark

```elixir
# bench/rule_bench.exs
facts = %{tier: :gold, subtotal: 150, lifetime_orders: 10}

Benchee.run(
  %{
    "evaluate/1 (3 rules)" => fn -> DiscountRules.evaluate(facts) end
  },
  time: 3,
  warmup: 1
)
```

Target on modern hardware: < 2µs for a 3-rule module. The generated function clauses are just pattern matches and comparisons — the compiler inlines aggressively. If you see >50µs, something in `evaluate/1` is reconstructing the AST at runtime; it should not.

## Advanced Considerations: Macro Hygiene and Compile-Time Validation

Macros execute at compile time, walking the AST and returning new AST. That power is easy to abuse: a macro that generates variables can shadow outer scope bindings, or a quote block that references variables directly can fail if the macro is used in a context where those variables don't exist. The `unquote` mechanism is the escape hatch, but misusing it leads to hard-to-debug compile errors.

Macro hygiene is about capturing intent correctly. A `defmacro` that takes `:my_option` and uses it directly might match an unrelated `:my_option` from the caller's scope. The idiomatic pattern is to use `unquote` for values that should be "from the outside" and keep AST nodes quoted for safety. The `quote` block's binding of `var!` and `binding!` provides escape valves for the rare case when shadowing is intentional.

Compile-time validation unlocks errors that would otherwise surface at runtime. A macro can call functions to validate input, generate code conditionally, or fail the build with `IO.warn`. Schema libraries like `Ecto` and `Ash` use macros to define fields at compile time, so runtime queries are guaranteed type-safe. The cost is cognitive load: developers must reason about both the code as written and the code generated.

---


## Deep Dive: Metaprogramming Patterns and Production Implications

Metaprogramming (macros, AST manipulation) requires testing at compile time and runtime. The challenge is that macro tests often involve parsing and expanding code, which couples tests to compiler internals. Production bugs in macros can corrupt entire modules; testing macros rigorously is non-negotiable.

---

## Trade-offs and production gotchas

**1. `var!/1` is required for hygiene escape**
The DSL references `facts` inside rule bodies without the author writing it. `var!(facts)` tells Elixir "this variable is the one the DSL injected, not a hygienic macro local". Forget it and the author gets `facts is unbound` at compile time.

**2. AST leaks into runtime when you forget `Macro.escape`**
`@rules {name, condition, action}` must escape its literals if they are put into a quoted form later. A plain tuple of atoms works; a tuple containing a struct (like a `Decimal`) fails cryptically.

**3. `Module.register_attribute(..., accumulate: true)` for many rules**
Without `accumulate: true`, each `@rules` overwrites the previous. With it, you get a list (prepended). Always reverse before iterating.

**4. Rule ordering**
`Module.get_attribute/2` returns newest first. If rule order matters (e.g. first match wins), you must `Enum.reverse`.

**5. `use RuleEngine` vs inherited macros**
If the author does `use RuleEngine` *inside* another `use SomeFramework`, the ordering of `@before_compile` hooks matters. `@before_compile` runs last-first; name the hook clearly and document ordering.

**6. When NOT to build a DSL**
If your rules are straightforward Elixir with maybe a `case` or a helper, just write Elixir. A DSL earns its keep when rule authors *aren't Elixir developers* or the vocabulary is the whole point.

## Reflection

The whitelist in `RuleEngine.Guards` is what keeps the DSL safe. Today the whitelist includes `>` and `and`. A business team will eventually want `Enum.any?/2` to match "any of these SKUs". How would you extend the whitelist without opening the door to arbitrary code? Sketch the policy: which functions are safe, which aren't, and where does the line sit?

## Resources

- [`Macro` module documentation](https://hexdocs.pm/elixir/Macro.html)
- [`Macro.prewalk/2` vs `Macro.postwalk/2`](https://hexdocs.pm/elixir/Macro.html#prewalk/2)
- [Chris McCord — Metaprogramming Elixir (book)](https://pragprog.com/titles/cmelixir/metaprogramming-elixir/)
- [Elixir source — `Kernel.SpecialForms`](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/kernel/special_forms.ex)
