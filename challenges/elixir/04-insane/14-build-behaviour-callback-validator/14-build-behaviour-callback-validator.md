# 14. Build a Compile-Time Behaviour Callback Validator

**Difficulty**: Insane

## Prerequisites

- Mastered: Elixir macros, `__using__`, `@before_compile`, `@after_compile`, `Module` introspection
- Mastered: Elixir type system (`@spec`, `@type`, `@callback`), Dialyzer PLT mechanics
- Familiarity with: Elixir compiler pipeline, Mix tasks, abstract code format (`:beam_lib`, `:compile.forms`)

## Problem Statement

Build a static analysis tool and compile-time framework that enforces strict behaviour
compliance beyond what the Elixir compiler provides by default:

1. Detect at compile time when a module declares `@behaviour MyBehaviour` but is missing
   one or more required callbacks. Emit a compiler error (not a warning) that names the
   specific missing callbacks and their expected signatures.
2. Distinguish `@optional_callbacks`: missing optional callbacks produce a warning with
   the recommendation to document why the callback is deliberately omitted.
3. Validate return types: compare the return type annotation on the implementing module's
   function `@spec` against the `@callback` spec declared in the behaviour. Report a
   mismatch as a compiler warning with both the expected and actual types shown.
4. Implement behaviour inheritance: `BehaviourB` can `use BehaviourA`, meaning any module
   implementing `BehaviourB` must also satisfy all callbacks from `BehaviourA`.
5. Enforce documentation: a callback implementation without a `@doc` annotation generates
   a compiler warning. The warning must identify the module, function name, and arity.
6. Implement a Mix task `mix behaviour.check` that scans all compiled modules in the
   project (including dependencies if requested), reports every module with missing or
   non-conforming callbacks, and exits with a non-zero status code if violations exist.

## Acceptance Criteria

- [ ] A module with `@behaviour MyBehaviour` that is missing callback `foo/2` causes
      `mix compile` to emit a compiler error: `(CompileError) MyModule does not implement
      required callback MyBehaviour.foo/2`.
- [ ] A module missing an `@optional_callbacks` entry compiles successfully but emits:
      `warning: MyModule does not implement optional callback MyBehaviour.bar/1`.
- [ ] A module where `foo/2` returns `{:ok, String.t()}` but the `@callback` declares
      `foo(atom, integer) :: {:ok, integer} | {:error, term}` emits a type mismatch warning.
- [ ] `BehaviourB` that extends `BehaviourA` causes implementing modules to satisfy both
      sets of callbacks; missing a callback from either triggers the appropriate error.
- [ ] A callback implementation without `@doc` emits:
      `warning: callback MyBehaviour.foo/2 implemented in MyModule without documentation`.
- [ ] `mix behaviour.check` scans all modules in `_build/dev/lib/**/*.beam`, reports
      violations in a structured format, and exits with code 1 if any violations exist.
- [ ] `mix behaviour.check --include-deps` also checks dependency modules.
- [ ] The validator itself compiles with zero warnings under `--warnings-as-errors`.
- [ ] All checks are implemented as a compiler pass using `@after_compile` hooks or a
      custom Mix compiler, not as a runtime check.

## What You Will Learn

- The Elixir compiler's module attribute accumulation and how `@after_compile` hooks execute
- How to read and parse `@callback`, `@spec`, and `@type` from module beam attributes at compile time
- The Erlang abstract code format and how to extract type information from `.beam` files using `:beam_lib`
- How Mix compilers and Mix tasks integrate with the build pipeline
- The difference between compile-time diagnostics (errors/warnings from `IO.warn` vs raising `CompileError`) and their stack trace formats
- Static analysis without Dialyzer: building custom type-compatibility checks on spec ASTs

## Hints

This exercise is intentionally sparse. Research:

- `Module.get_attribute(module, :behaviour)` retrieves the declared behaviours from within an `@after_compile` hook
- `module.__info__(:functions)` lists implemented functions; `Code.fetch_docs/1` retrieves `@doc` annotations from beam
- Behaviour callbacks are stored in `module.behaviour_info(:callbacks)` for Erlang modules; Elixir stores them differently via `__MODULE__.__info__(:functions)` — inspect the actual beam attributes with `:beam_lib.chunks/2` and the `"ExCk"` chunk
- For type checking, parse `@spec` ASTs using `Code.string_to_quoted/1` on the spec string and compare type trees structurally
- `Mix.Task` requires implementing `run/1`; use `Mix.Task.run("compile")` to ensure beam files are fresh before scanning

## Reference Material

- Elixir compiler internals: https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/module.ex
- Erlang abstract format: https://www.erlang.org/doc/apps/erts/absform
- `:beam_lib` documentation: https://www.erlang.org/doc/man/beam_lib
- Dialyzer PLT and type inference: https://www.erlang.org/doc/man/dialyzer
- "Metaprogramming Elixir" — Chris McCord, Chapters 4–5

## Difficulty Rating

★★★★★★

## Estimated Time

35–50 hours
