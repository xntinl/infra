# 24. Type Checker for Simple Language

<!--
difficulty: advanced
category: parsers-and-compilers
languages: [rust]
concepts: [type-checking, type-inference, generics, scope-analysis, semantic-analysis, error-reporting]
estimated_time: 12-16 hours
bloom_level: evaluate
prerequisites: [rust-advanced, enums, traits, hash-maps, recursive-data-structures, parsing-basics]
-->

## Languages

- Rust (stable)

## Prerequisites

- Strong Rust fundamentals: enums, traits, `Box`, `HashMap`, `Rc`/`Arc` for shared types
- Understanding of recursive descent parsing (challenge 23 or equivalent)
- Conceptual understanding of type systems: what types are, why they exist, static vs dynamic
- Familiarity with scope rules: lexical scoping, variable shadowing, function scope
- Experience with a statically-typed language (Rust, Go, TypeScript, Java) to understand the user-facing expectations

## Learning Objectives

- **Design** a type representation that captures primitives, functions, structs, and generic containers
- **Implement** bidirectional type checking: explicit annotations verified, uninitialized types inferred from context
- **Evaluate** type compatibility rules: when can one type be used where another is expected
- **Analyze** how scope chains enable correct variable resolution and type lookup
- **Create** a type error reporting system that pinpoints the exact location and nature of mismatches

## The Challenge

Type checking is where a compiler transitions from syntax (is the program well-formed?) to semantics (does the program make sense?). A type checker walks the AST after parsing and verifies that every operation is applied to compatible types: you cannot add a string to an integer, call a non-function, or access a field that does not exist on a struct.

The harder part is **type inference**: when the programmer writes `let x = 5;` without a type annotation, the compiler must figure out that `x` is an integer. When they write `let y = some_func(x);`, the type of `y` depends on the return type of `some_func`. This requires propagating type information forward and backward through the AST.

Type inference ranges from trivial (literal types) to deeply complex (Hindley-Milner, constraint solving). This challenge focuses on **local type inference**: the type of a variable is determined from its initializer expression, and function signatures are always explicit. This is the approach TypeScript, Go, Kotlin, and Rust use for local variables -- it provides most of the ergonomic benefit without the complexity of global inference.

A critical design decision is how to handle errors. A type checker that stops at the first error forces the programmer into fix-one-recompile cycles. A type checker that continues must deal with **error propagation**: if `x` has an unknown type due to an earlier error, every expression using `x` would trigger a cascade of false errors. The standard solution is an `Unknown` or "error" type that is compatible with everything, suppressing downstream noise.

Build a type checker for a simple statically-typed language. The language supports primitive types, functions with typed parameters and return types, local type inference, struct definitions with typed fields, and basic generics (`Array<T>`). The type checker operates on an AST (you can define your own or adapt from challenge 23).

## Requirements

1. Define a type representation covering: `int`, `float`, `bool`, `string` primitives, `void` (for functions that return nothing), function types (`(int, int) -> int`), struct types with named fields, and generic types (`Array<T>`)
2. Implement a scope chain: each scope (global, function body, block) has its own symbol table, linked to its parent. Variable lookup walks up the chain
3. Type-check variable declarations: if annotated (`let x: int = 5;`), verify the initializer matches. If unnannotated (`let x = 5;`), infer the type from the initializer
4. Type-check arithmetic: both operands must be numeric (`int` or `float`). `int + int = int`, `float + float = float`, `int + float = float` (implicit widening)
5. Type-check comparisons: `==` and `!=` work on any matching types, `<`/`>`/`<=`/`>=` only on numeric types. Result is always `bool`
6. Type-check function definitions: parameter types are declared, return type is declared or inferred from the body's return statements. All return paths must return the same type
7. Type-check function calls: argument count and types must match the function's parameter types. The call expression's type is the function's return type
8. Type-check struct definitions and field access: `point.x` is valid only if `point` has type `Point` and `Point` has a field `x`. The expression's type is the field's type
9. Implement basic generics: `Array<int>`, `Array<string>`, etc. Type-check element access and insertion with the correct element type
10. Report all type errors (do not stop at the first), with source position, expected type, and actual type

## Hints

Hints for advanced challenges are text-only.

**Hint 1**: Your `Type` enum is the core data structure. It must be recursive (function types contain parameter types and a return type) and comparable (to check if two types match). Derive `PartialEq` and `Clone`. Use `Box` for recursive variants.

**Hint 2**: Bidirectional type checking has two modes: "checking" (I know the expected type, verify this expression has it) and "synthesizing" (I do not know the type, figure it out from the expression). Variable declarations without annotations use synthesis on the initializer. Function parameters use checking against declared types.

**Hint 3**: The scope chain is a linked list of `HashMap<String, Type>`. When you enter a function body, push a new scope. When you leave, pop it. Variable lookup starts at the innermost scope and walks outward. This naturally handles shadowing.

**Hint 4**: For generics, use a `TypeParam` variant in your type enum. When a generic type is instantiated (`Array<int>`), substitute the type parameter with the concrete type throughout the struct's field types. This is monomorphization at the type level.

**Hint 5**: Use a two-pass approach. First, scan all top-level declarations (structs, functions) and register their types in the global scope. Second, type-check all function bodies and variable initializers. This allows forward references -- a function can call another function defined later in the file.

**Hint 6**: When you encounter a type error, record it and return `Type::Unknown`. In every type-checking rule, treat `Unknown` as compatible with every other type. This prevents cascading errors: one mistake in a variable declaration does not produce errors in every subsequent expression that uses that variable.

## Acceptance Criteria

- [ ] Primitives type-check: `let x: int = 5;` passes, `let x: int = "hello";` reports error
- [ ] Type inference: `let x = 5;` infers `int`, `let y = 3.14;` infers `float`, `let z = true;` infers `bool`
- [ ] Arithmetic: `int + int` OK, `string + int` error, `int + float` yields `float`
- [ ] String concatenation: `string + string` produces `string`
- [ ] Function type-checking: wrong argument types or counts produce errors
- [ ] Return type consistency: function with both `return 5;` and `return "hello";` reports error
- [ ] Struct field access: valid fields type-check, invalid fields produce errors with the struct and field names
- [ ] Generic Array: `Array<int>` only accepts `int` elements, rejects `string`
- [ ] Array indexing: `arr[0]` requires `arr` to be `Array<T>` and the index to be `int`, result is `T`
- [ ] Scope chain: inner scope can shadow outer variables, type lookups resolve correctly
- [ ] Forward references: a function can call another function defined later in the same file
- [ ] Multiple errors reported from a single type-check pass (at least 3)
- [ ] Error messages include source position, expected type, and actual type
- [ ] All tests pass with `cargo test`

## Research Resources

- [Crafting Interpreters: Resolving and Binding](https://craftinginterpreters.com/resolving-and-binding.html) -- scope chains and variable resolution
- [Types and Programming Languages (Pierce)](https://www.cis.upenn.edu/~bcpierce/tapl/) -- the definitive textbook on type systems (chapters 1-11 for basics)
- [Write You a Haskell: Type Systems](http://dev.stephendiehl.com/fun/006_hindley_milner.html) -- Hindley-Milner type inference explained
- [Bidirectional Type Checking (Dunfield & Krishnaswami)](https://arxiv.org/abs/1908.05839) -- the theory behind checking vs synthesizing
- [Rust Analyzer Internals](https://rust-analyzer.github.io/blog/2020/07/20/three-architectures-for-responsive-ide.html) -- how a real type checker is structured
- [TypeScript Compiler Internals](https://github.com/microsoft/TypeScript/wiki/Architectural-Overview) -- how TypeScript handles type inference and checking
- [So You Want to Write a Type Checker (Noel Welsh)](https://www.lihaoyi.com/post/SoYouWantToWriteATypeChecker.html) -- practical walkthrough of building a type checker
- [Crafting Interpreters: Classes](https://craftinginterpreters.com/classes.html) -- implementing struct-like types with field access
