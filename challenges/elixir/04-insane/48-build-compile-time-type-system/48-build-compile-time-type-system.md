# 48. Build a Compile-time Type System for Elixir Macros
**Difficulty**: Insane

## Prerequisites
- Mastered: Elixir macro system (`quote/2`, `unquote/1`, `__using__/1`, `@before_compile`, `__on_definition__/6`), Elixir AST structure and traversal (`Macro.traverse/4`, `Macro.prewalk/3`), `CompileError` and `Module` attributes at compile time, Dialyzer and `@spec` type annotations, pattern matching and guard semantics
- Study first: "Metaprogramming Elixir" (McCord, Pragmatic Bookshelf), "Types and Programming Languages" (Pierce, MIT Press) chapters 1–15, Dialyzer documentation and PLT internals, TypeScript compiler design goals (Anders Hejlsberg talks), "Practical Foundations of Mathematics" (Taylor) — chapter on type theory

## Problem Statement
Build a compile-time type checker for Elixir implemented entirely as macros and `__before_compile__` hooks — no external processes, no runtime overhead. The system verifies type annotations on function definitions at compile time and raises `CompileError` with precise source locations when types are violated.

1. Design and implement the type annotation syntax: `@type_checked def my_func(x :: Integer, y :: String) :: Boolean do ... end` — the annotation must be parsed entirely at compile time from the AST of the `def` expression, with no modification to the Elixir parser
2. Implement compile-time type inference for basic expressions: integer and string literals, boolean operators, arithmetic expressions, string concatenation, and direct variable references within a single function body — the inferred type of each sub-expression must be computed from the AST before the function is compiled
3. Produce precise `CompileError` messages on type mismatches: the error must include the file name, the exact line number from the AST metadata, the expected type, the inferred type, and the expression that caused the mismatch — no generic "type error" messages
4. Implement union types in annotations: `Integer | String | nil` — a value is valid if it matches any member of the union; union simplification must be applied (deduplication, `any()` absorption, `never()` elimination)
5. Implement generic type variables for standard collection types: `List.t(T)`, `Map.t(K, V)`, `Tuple.t(A, B)` — the type variable `T` must be resolved per call site and propagated through the expression; mismatches like passing a `List.t(Integer)` where `List.t(String)` is expected must be caught at compile time
6. Implement struct type checking: when a struct is used as a parameter type (e.g., `x :: %MyStruct{}`), verify at the call site that the argument is known at compile time to be that struct type, and verify that all fields referenced inside the function body exist on the struct definition and match their declared types
7. Integrate with Dialyxir: generate valid `@spec` declarations automatically from `@type_checked` annotations, so that `mix dialyzer` sees the same types without duplication — no manual `@spec` should be required when `@type_checked` is used
8. Validate macro expansion performance: the total additional compile time introduced by the type-checking macro expansion must not exceed 10% of baseline compile time for a module with 50 annotated functions — measured by `mix compile --force` with and without the annotation module loaded

## Acceptance Criteria
- [ ] `@type_checked def f(x :: Integer) :: String` compiles without error when the body provably returns a `String`; raises `CompileError` with correct file, line, expected type, and inferred type when it returns an `Integer`
- [ ] Type inference correctly infers the type of: integer literals, string literals, `true`/`false`, binary `+`/`-`/`*`, `<>`  string concatenation, and `if` expressions where both branches have compatible types
- [ ] `CompileError` messages include the source file path, the line number extracted from AST metadata, the expression text (via `Macro.to_string/1`), the expected type, and the actual inferred type — no generic messages
- [ ] `Integer | String` is a valid annotation; passing an `Integer` or a `String` satisfies it; passing a `Boolean` raises a `CompileError`; `any() | Integer` simplifies to `any()`
- [ ] `List.t(Integer)` is a valid annotation for a parameter; passing a `List.t(String)` raises a `CompileError` at the call site with the concrete resolved types shown in the error message
- [ ] A function annotated with `x :: %User{}` raises `CompileError` if the function body references `x.nonexistent_field`; it compiles cleanly when all referenced fields exist and their types match the struct definition
- [ ] Running `mix dialyzer` on a module using `@type_checked` passes without warnings; the generated `@spec` declarations match the annotations exactly; no `@spec` is required alongside `@type_checked`
- [ ] `mix compile --force` on a 50-function annotated module takes no more than 10% longer than the same module without `@type_checked` — benchmarked with `:timer.tc` wrapping the compile call in a test Mix task

## What You Will Learn
- The Elixir macro system at its deepest level: how `__on_definition__/6`, `@before_compile`, and accumulating module attributes enable multi-pass compile-time analysis
- Abstract Syntax Tree traversal and transformation: how to walk an AST, annotate nodes with type information, and accumulate type constraints across an expression tree
- Type inference fundamentals: the Hindley-Milner algorithm simplified for a single-pass, expression-local checker; unification; constraint propagation
- Union type theory: subtyping, covariance, contravariance, and how union types interact with function signatures
- Parametric polymorphism (generics) at compile time: type variable introduction, scoping, and resolution without a runtime type representation
- The relationship between macros and the compiler: how macros see the AST before expansion, how `CompileError` is raised with metadata, and how `Macro.Env` tracks scope
- Dialyzer's type system: the difference between success typings (Dialyzer) and nominal types (what you are building) and how to bridge them

## Hints (research topics, NO tutorials)
- Study `Module.__info__(:functions)` and `__on_definition__/6` — this callback fires for every function definition and receives the raw AST of the body, which is the entry point for your type checker
- Research how `quote/2` preserves line metadata in the `:meta` field of AST tuples — this is the source of your error line numbers; understand why stripping metadata breaks error reporting
- Look into Algorithm W (the classic Hindley-Milner inference algorithm) — you do not need the full algorithm, but understanding unification will help you implement union resolution and generic instantiation
- Study how Dialyzer represents types internally (success types, `erl_types` module) — you don't need to replicate this, but understanding the gap between success typing and nominal typing will clarify your design
- Research `Macro.prewalk/3` vs `Macro.postwalk/3` — type inference is typically bottom-up (postwalk), while type checking is top-down (prewalk); understand why
- Look into how TypeScript handles excess property checking on object literals — similar to how you might check struct field access
- Study compile-time performance profiling: `mix compile` with `--profile time` and how to interpret macro expansion cost versus actual compilation cost

## Reference Material (papers/docs primarios)
- "Types and Programming Languages" — Benjamin C. Pierce (MIT Press, 2002), chapters 1–15 (lambda calculus, simple types, subtyping, polymorphism)
- "Metaprogramming Elixir" — Chris McCord (Pragmatic Bookshelf, 2015)
- Elixir documentation: `Macro`, `Module`, `Code.Typespec`
- Dialyzer source: `lib/dialyzer/` in the Erlang/OTP repository — particularly `dialyzer_typesig.erl` and `erl_types.erl`
- "Hindley-Milner Type Inference" — original Milner paper: "A Theory of Type Polymorphism in Programming" (1978)
- Anders Hejlsberg, "The TypeScript Compiler's Architecture" — NDC Oslo 2016 (structural type system design)
- "Gradual Typing for Functional Languages" — Siek & Taha (2006) — relevant for understanding partial type checking
- Erlang `erl_parse` documentation — Elixir's AST is built on this; understanding the parent helps with metadata handling

## Difficulty Rating ★★★★★★★
This exercise sits at the intersection of programming language theory and Elixir's macro system. Building a correct type checker requires understanding type theory, AST mechanics, and compile-time execution simultaneously. The Dialyzer integration and performance constraint prevent shortcuts.

## Estimated Time
250–400 hours
