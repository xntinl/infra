# 26. Build a Mini-Language Compiler

**Difficulty**: Insane

---

## Prerequisites

- Elixir metaprogramming: `quote`, `unquote`, AST manipulation
- Understanding of compiler phases: lex, parse, type-check, codegen
- Familiarity with operator precedence parsing algorithms
- Core Erlang or BEAM internal representation basics
- Erlang's `:compile` module and abstract forms
- Type theory fundamentals: Hindley-Milner or simple type inference

---

## Problem Statement

Build a compiler for a statically-typed mini-language that targets the BEAM virtual machine. Programs written in the mini-language compile to Erlang Core or BEAM abstract forms and run natively on the BEAM. The compiler must:

1. Lex source text into tokens with position metadata for error reporting
2. Parse tokens into a typed AST using a Pratt parser or Shunting-yard algorithm that correctly handles operator precedence and associativity
3. Type-check the AST, inferring types where not annotated and reporting violations with the source location
4. Generate valid Core Erlang AST or BEAM abstract forms that the OTP compiler can compile to `.beam` bytecode
5. Support first-class functions, closures, and recursion in the compiled output
6. Report errors with file name, line number, column, and a human-readable message
7. Allow compiled code to call Elixir and Erlang functions directly

---

## Acceptance Criteria

- [ ] Lexer: tokenizes identifiers, keywords (`if`, `else`, `while`, `fn`, `let`, `return`, `true`, `false`), arithmetic and comparison operators, integer and float literals, string literals, and punctuation; tracks line and column for each token
- [ ] Parser: uses Pratt parsing (or equivalent) to handle operator precedence correctly; `1 + 2 * 3` binds as `1 + (2 * 3)`; produces a typed AST node tree; reports syntax errors with position
- [ ] Type checker: supports `Int`, `Float`, `Bool`, `String`, and function types; infers the type of expressions from operand types; reports type mismatches (e.g., adding `Int` to `Bool`) with source position; function return types are checked against the declared signature
- [ ] Code generation: emits valid Core Erlang (`.core`) or Erlang abstract forms (`.beam`-compilable); the output can be fed to `:compile.forms/2` or the `erlc` compiler to produce a loadable `.beam` file
- [ ] Control flow: `if/else` expressions (not statements) produce a value; `while` loops with `break` and `continue`; `return` exits the current function with a value
- [ ] Functions: first-class function values; closures capture free variables; direct and mutual recursion; variadic functions are not required
- [ ] Error reporting: every error includes file name, line number, column number, and a message; the compiler reports all errors in a single pass rather than stopping at the first one
- [ ] Standard library: built-in functions for `print`, `to_string`, basic math (`abs`, `floor`, `ceil`), and string concatenation
- [ ] Interop: the mini-language can call any zero-arity to three-arity Erlang/Elixir function using `Module.function(args)` syntax; return values are mapped to mini-language types

---

## What You Will Learn

- Pratt parsers and precedence climbing for expression parsing
- Type inference using unification (Robinson's algorithm)
- Core Erlang as a compilation target: modules, functions, clauses
- The BEAM abstract format and how OTP compiles it
- Error recovery in parsers to report multiple errors
- How closures are compiled to lambda lifts or environment records
- Metaprogramming in Elixir as a model for AST manipulation

---

## Hints

- Read Thorsten Ball's "Writing An Interpreter In Go" — the concepts transfer directly to Elixir
- Research "Pratt parsing" (top-down operator precedence) — it handles precedence more cleanly than recursive descent alone
- Study Core Erlang syntax (`cerl` module in OTP) before designing your codegen
- Investigate how the Elixir compiler represents ASTs internally — it is a valid inspiration
- Think about how closures must be "closure-converted" before generating code for a flat module structure
- Look into "constraint-based type inference" as an alternative to Hindley-Milner for simpler implementation

---

## Reference Material

- "Writing An Interpreter In Go" & "Writing A Compiler In Go" — Thorsten Ball
- Core Erlang 1.0.3 Specification (it.uu.se)
- Erlang abstract format documentation (erlang.org)
- "Types and Programming Languages" — Benjamin Pierce (type theory)
- "Crafting Interpreters" — Robert Nystrom (free online)

---

## Difficulty Rating ★★★★★★

Targeting the BEAM with a type checker and real interop requires deep understanding of both compiler theory and OTP internals simultaneously.

---

## Estimated Time

80–120 hours
