# 20. Writing a Compiler Frontend

**Difficulty**: Insane

## The Challenge

A compiler frontend is where raw source text becomes structured meaning. It is the bridge between the messy reality of human-written code and the precise abstractions a machine needs to generate correct output. Building one from scratch --- lexer, parser, abstract syntax tree, type checker, semantic analysis, and error recovery --- is one of the most demanding and rewarding exercises in software engineering. Every design decision has consequences: how you represent spans affects error message quality, how you structure the AST affects every downstream pass, how you handle errors determines whether the compiler is pleasant or infuriating to use.

Your task is to build a complete compiler frontend for a small statically-typed language called "Lux." Lux is a simple expression-oriented language with let bindings, functions, if/else expressions, integer and boolean types, basic algebraic data types (enums with associated data), and pattern matching. It is not Turing-complete in any exotic way --- the interesting part is not the language design but the engineering of the compiler itself. You must implement a hand-written lexer (no parser generators), a recursive descent parser with Pratt parsing for expressions, a fully typed AST with source spans on every node, a Hindley-Milner-style type inference engine with unification, semantic analysis passes (name resolution, exhaustiveness checking for pattern matches, unused variable detection), and structured diagnostic output modeled after `rustc`'s error format with colored source snippets and suggestions.

The emphasis is on production-quality engineering, not language features. Your compiler should never panic on malformed input. It should recover from errors gracefully and continue parsing to report as many errors as possible in a single run. It should produce error messages that are genuinely helpful: pointing at the exact character, underlining the relevant span, suggesting fixes when possible, and providing notes with additional context. The quality bar is `rustc` and `elm` --- compilers famous for their error messages.

## Acceptance Criteria

### Language Specification

- [ ] Lux supports the following types: `Int`, `Bool`, `String` (literals only, no operations beyond equality), `()` (unit), function types `(T) -> U`, tuple types `(T, U, ...)`, and user-defined enum types
- [ ] Lux supports the following expressions:
  - Integer literals: `42`, `-7`, `0xFF` (hex), `0b1010` (binary), `1_000_000` (underscores)
  - Boolean literals: `true`, `false`
  - String literals: `"hello"` with escape sequences `\\`, `\"`, `\n`, `\t`, `\0`
  - Identifiers: `foo`, `bar_baz`, `_unused`
  - Binary operations: `+`, `-`, `*`, `/`, `%`, `==`, `!=`, `<`, `>`, `<=`, `>=`, `&&`, `||`
  - Unary operations: `-` (negate), `!` (not)
  - Function calls: `f(x, y)`
  - Let bindings: `let x = expr` and `let x: Type = expr`
  - If/else: `if condition { expr } else { expr }` (both branches required, expression-oriented)
  - Block expressions: `{ stmt; stmt; expr }` where the last expression is the block's value
  - Lambda expressions: `|x: Int, y: Int| -> Int { x + y }` and `|x| x + 1` (inferred types)
  - Tuple construction: `(1, true, "hello")`
  - Tuple field access: `tuple.0`, `tuple.1`
  - Match expressions: `match expr { pattern => expr, pattern => expr }`
  - Enum variant construction: `Option::Some(42)`, `Color::Red`
- [ ] Lux supports the following top-level declarations:
  - Function declarations: `fn add(x: Int, y: Int) -> Int { x + y }`
  - Enum declarations: `enum Option<T> { Some(T), None }` and `enum Color { Red, Green, Blue }`
  - Type aliases: `type Point = (Int, Int)`
- [ ] Lux supports single-line comments `// ...` and block comments `/* ... */` (with nesting)
- [ ] Semicolons separate statements within blocks; the last expression in a block has no trailing semicolon (it is the block's value, as in Rust)

### Lexer

- [ ] Implement a hand-written lexer (no dependencies on `logos`, `nom`, or other parsing libraries for the lexer itself)
- [ ] The lexer produces a stream of `Token` values, each carrying a `Span` (byte offset start, byte offset end, source file ID)
- [ ] The lexer correctly handles all token types: keywords (`let`, `fn`, `if`, `else`, `match`, `true`, `false`, `enum`, `type`), identifiers, integer literals (decimal, hex, binary, with underscores), string literals (with escape sequences), operators, delimiters, comments
- [ ] The lexer produces meaningful error tokens (not panics) for:
  - Unterminated string literals
  - Invalid escape sequences
  - Invalid number literals (e.g., `0xGG`, `0b23`)
  - Unexpected characters
- [ ] Each error token carries a `Span` and an error message
- [ ] The lexer is implemented as an iterator (`impl Iterator<Item = Token>`)
- [ ] The lexer handles Unicode identifiers (at minimum, reject them with a helpful error message suggesting ASCII alternatives)
- [ ] The lexer is exhaustively tested with at least 25 test cases covering all token types, edge cases, and error conditions

### Parser

- [ ] Implement a recursive descent parser with Pratt parsing for expressions
- [ ] The parser takes a token stream and produces an AST (or a list of errors, or both --- the parser should recover and continue)
- [ ] Implement operator precedence via Pratt parsing with at least 5 precedence levels:
  - Level 1 (lowest): `||`
  - Level 2: `&&`
  - Level 3: `==`, `!=`, `<`, `>`, `<=`, `>=`
  - Level 4: `+`, `-`
  - Level 5 (highest): `*`, `/`, `%`
  - Unary `-` and `!` have higher precedence than any binary operator
- [ ] The parser handles operator associativity correctly: arithmetic operators are left-associative, comparison operators are non-associative (chaining like `a < b < c` is a parse error)
- [ ] Implement error recovery using synchronization tokens: when the parser encounters an unexpected token, it skips tokens until it finds a synchronization point (`;`, `}`, `fn`, `enum`, `type`, EOF) and continues parsing
- [ ] Produce at least 3 error recovery test cases showing that the parser reports multiple errors from a single input
- [ ] The parser produces meaningful error messages:
  - "expected expression, found `}`" with a span pointing at the `}`
  - "expected `)` to close function call started here" with a span pointing at the opening `(`
  - "unexpected token `+` --- did you mean to write a binary expression?" with a suggestion
- [ ] The parser handles trailing commas in function arguments, tuple expressions, and match arms
- [ ] The parser is tested with at least 30 test cases covering all expression types, declarations, error recovery, and edge cases

### Abstract Syntax Tree

- [ ] Define the AST as a set of enums and structs in a dedicated `ast` module
- [ ] Every AST node carries a `Span` indicating its source location (from the first token to the last token of the node)
- [ ] The AST uses `NodeId` (integer index) for every node, enabling efficient lookups in side tables (type information, resolution information)
- [ ] The AST supports the following node types at minimum:
  - `Expr`: `IntLit`, `BoolLit`, `StringLit`, `Ident`, `Binary`, `Unary`, `Call`, `If`, `Block`, `Lambda`, `Tuple`, `TupleField`, `Match`, `EnumVariant`
  - `Stmt`: `Let`, `Expr` (expression statement)
  - `Decl`: `FnDecl`, `EnumDecl`, `TypeAlias`
  - `Pattern`: `Wildcard`, `Ident`, `Literal`, `Tuple`, `EnumVariant`, `Or`
  - `Type`: `Named`, `Function`, `Tuple`, `Unit`, `Infer` (placeholder for type inference)
- [ ] The AST is arena-allocated or uses `Box` with indices --- not raw `Box<Expr>` trees (to enable efficient traversal and avoid deep stack recursion during later passes)
- [ ] Implement `Display` for the AST that pretty-prints it back to valid Lux source code (this tests that the AST preserves enough information)

### Type Checker

- [ ] Implement Hindley-Milner type inference with Algorithm W or Algorithm J
- [ ] The type checker resolves types for all expressions without requiring explicit type annotations (except for top-level function signatures, which must be annotated)
- [ ] Implement unification: when two types must be equal, the type checker either unifies them or produces a type error
- [ ] Support generic functions: `fn identity<T>(x: T) -> T { x }` is inferred to have a polymorphic type
- [ ] Support generic enum types: `enum Option<T> { Some(T), None }` instantiates `T` at each usage site
- [ ] Implement the occurs check to prevent infinite types (e.g., `let f = |x| x(x)` should produce a type error, not infinite recursion)
- [ ] Produce clear type error messages:
  - "type mismatch: expected `Int`, found `Bool`" with spans pointing at the expected and actual locations
  - "cannot call `x` because it has type `Int`, which is not a function" with a span on the call site and a note pointing at the definition of `x`
  - "type `Option<Int>` does not have a variant `Missing`" with a suggestion of similar variant names
- [ ] The type checker populates a `TypeTable` mapping `NodeId -> Type` for every expression node in the AST
- [ ] Integer literals default to `Int`, boolean literals to `Bool`; there is no implicit coercion between types
- [ ] Test the type checker with at least 25 test cases covering:
  - Successful inference for all expression types
  - Generic function instantiation
  - Type errors with good messages
  - Occurs check triggering
  - Recursive function typing (functions can call themselves; their return type must be annotated)

### Semantic Analysis

- [ ] Implement name resolution as a separate pass that runs before type checking
- [ ] Name resolution builds a `ScopeMap` tracking which names are defined in which scopes (block scopes, function scopes, module scope)
- [ ] Report errors for:
  - Undefined variables: "cannot find value `foo` in this scope --- did you mean `bar`?" (with Levenshtein distance suggestion)
  - Duplicate definitions: "`x` is defined multiple times in this scope" with spans for both definitions
  - Use before definition (within a block): "`x` is used before it is defined" (only applicable to let bindings; functions are hoisted)
- [ ] Implement exhaustiveness checking for match expressions:
  - A match on `Bool` must cover `true` and `false` (or have a wildcard)
  - A match on an enum must cover all variants (or have a wildcard)
  - Report: "non-exhaustive patterns: `Option::None` not covered" with a suggestion to add the missing arm
- [ ] Implement unused variable detection:
  - Warn for variables that are defined but never used
  - Suppress the warning for variables prefixed with `_` (e.g., `_unused`)
  - Report: "unused variable: `x` --- if this is intentional, prefix it with an underscore: `_x`"
- [ ] Implement dead code detection: warn about code after a `return` expression or unreachable match arms
- [ ] All semantic analysis passes produce structured diagnostics (not panics or plain strings)

### Diagnostics

- [ ] Implement a `Diagnostic` type with severity (Error, Warning, Note), message, primary span, secondary labeled spans, and optional suggestions
- [ ] Implement a diagnostic renderer that produces colored terminal output in the style of `rustc`:
  ```
  error[E001]: type mismatch
   --> main.lux:3:12
    |
  3 |   let x: Int = true;
    |          ---   ^^^^ expected `Int`, found `Bool`
    |          |
    |          expected due to this type annotation
  ```
- [ ] Support multi-span diagnostics where the primary and secondary spans may be on different lines
- [ ] Support suggestion diagnostics:
  ```
  help: consider adding a type annotation
    |
  3 |   let x: Int = 42;
    |        +++++
  ```
- [ ] The renderer handles edge cases: very long lines (truncation with `...`), spans at the beginning or end of a line, multiple diagnostics on the same line, spans across multiple lines
- [ ] Use ANSI color codes for terminal output: red for errors, yellow for warnings, blue for notes, green for suggestions
- [ ] Support a "no color" mode (controlled by an environment variable or flag) for CI environments
- [ ] The diagnostic system is a reusable module with no dependency on the Lux language specifics (it takes source text, spans, and messages --- it could be used for any language)
- [ ] Test the diagnostic renderer output with at least 10 snapshot tests (compare rendered output against expected strings)

### Error Recovery

- [ ] The compiler NEVER panics on any input, including empty input, binary garbage, and adversarial nesting
- [ ] The lexer, parser, and type checker all continue after errors, collecting diagnostics
- [ ] A single compiler invocation reports ALL errors it can find (not just the first one)
- [ ] Error recovery does not produce cascading false positives: if a let binding fails to parse, subsequent uses of that variable should produce at most one "undefined variable" error, not one per use
- [ ] Fuzz the compiler with `cargo-fuzz` or `afl` for at least 10 minutes with no panics or crashes (document the fuzzing setup and results)

### Driver and Integration

- [ ] Implement a CLI binary that reads a `.lux` file, runs all compiler passes, and prints diagnostics to stderr
- [ ] The CLI exits with code 0 if no errors (warnings are OK) and code 1 if there are errors
- [ ] Implement a `--dump-ast` flag that prints the pretty-printed AST to stdout
- [ ] Implement a `--dump-types` flag that prints every expression with its inferred type
- [ ] Implement a `--dump-tokens` flag that prints the token stream
- [ ] Include at least 5 example `.lux` files in a `examples/` directory demonstrating the language features
- [ ] Include at least 5 error-case `.lux` files in a `tests/error_cases/` directory with corresponding expected diagnostic output
- [ ] Total test count across all modules: at least 80 tests

## Starting Points

- Study the [`rustc` lexer source](https://github.com/rust-lang/rust/tree/master/compiler/rustc_lexer/src) --- it is a standalone crate with no dependencies that lexes Rust syntax. Focus on `lib.rs` for the architecture (cursor-based advancement, token categorization) and `unescape.rs` for string escape handling
- Read [Bob Nystrom's "Crafting Interpreters"](https://craftinginterpreters.com/) --- specifically Part II (Tree-Walk Interpreter), chapters 4-13. The scanner and parser chapters are directly applicable. The entire book is free online
- Study the [Pratt parsing algorithm](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) via matklad's (Aleksey Kladov, rust-analyzer author) excellent tutorial. This is the definitive modern reference for implementing Pratt parsing in Rust
- Read matklad's [resilient parsing tutorial](https://matklad.github.io/2023/05/21/resilient-ll-parsing.html) for error recovery patterns in recursive descent parsers
- Study the [`rust-analyzer` source](https://github.com/rust-lang/rust-analyzer) --- specifically `crates/parser/src/grammar/` for how a production Rust parser handles error recovery, and `crates/hir-ty/src/infer/` for type inference
- Read the [original Algorithm W paper by Damas and Milner](https://dl.acm.org/doi/10.1145/582153.582176) "Principal type-schemes for functional programs" for the formal foundation of Hindley-Milner inference
- Study [Oleg Kiselyov's tutorial on Algorithm W implementation](http://okmij.org/ftp/ML/generalization.html) for practical insights on efficient generalization and instantiation
- Read the [`codespan-reporting` crate source](https://github.com/brendanzab/codespan) for an existing implementation of `rustc`-style diagnostic rendering in Rust. You will reimplement this from scratch, but studying it first will save design time
- Study the [`ariadne` crate source](https://github.com/zesterer/ariadne) for an alternative diagnostic rendering library with beautiful output. Compare its architecture with `codespan-reporting`
- Read the [Elm compiler's error message design philosophy](https://elm-lang.org/news/compiler-errors-for-humans) and study the [source of the Elm compiler's error module](https://github.com/elm/compiler/tree/master/compiler/src/Reporting)
- Study the [`typed-arena` crate](https://docs.rs/typed-arena/latest/typed_arena/) for arena allocation of AST nodes
- Read the [`miette` crate documentation](https://docs.rs/miette/latest/miette/) for another approach to structured diagnostics in Rust
- Examine the [GHC/Haskell type checker](https://gitlab.haskell.org/ghc/ghc/-/wikis/commentary/compiler/type-checker) documentation for insights on constraint-based type inference (more scalable than Algorithm W for complex type systems)

## Hints

1. Start with the lexer. It is the simplest component and everything depends on it. Represent `Span` as `{ start: u32, end: u32 }` (byte offsets into the source), not `{ line: u32, col: u32 }`. You can compute line/column from byte offsets on demand (for error messages) but byte offsets are much easier to work with during parsing.

2. Build a `SourceMap` that maps byte offsets to line/column numbers. Pre-compute line start offsets when loading the source: scan for `\n` and store the byte offset of each line start in a `Vec<u32>`. Then `offset_to_line_col` is a binary search on this vector. This makes diagnostic rendering fast regardless of file size.

3. For the lexer, use a cursor pattern: a struct holding `&str` (remaining source) and `pos: usize` (current byte offset). Implement `peek_char()`, `advance()`, `eat_while(predicate)`. Each `next_token()` call peeks at the current character, dispatches to the appropriate handler (e.g., `'0'..='9' => lex_number()`, `'"' => lex_string()`, ...), and returns a `Token`.

4. For the Pratt parser, the key abstraction is two functions: `parse_expr(min_bp: u8) -> Expr` and a table mapping each operator to its `(left_binding_power, right_binding_power)`. Left-associative operators have `right_bp = left_bp + 1`. Right-associative operators have `right_bp = left_bp`. Non-associative operators have `right_bp = left_bp` and a check that rejects chaining.

5. For error recovery in the parser, use the "synchronize" pattern: when an error is encountered, record the error, then advance the token stream until you reach a "synchronization point" (a token that likely starts a new construct). Then resume parsing. Good synchronization tokens for Lux: `fn`, `enum`, `type`, `let`, `}`, `;`, EOF.

6. To prevent cascading errors from undefined names, keep a "poison" set of names that failed to resolve. When a name is in the poison set, suppress further "undefined variable" errors for that name. This dramatically improves the user experience.

7. For the AST, consider using an index-based approach: store all expressions in a `Vec<Expr>`, all statements in a `Vec<Stmt>`, etc. Use newtype indices (`struct ExprId(u32)`) to refer to nodes. This is the architecture used by `rust-analyzer` and it enables efficient side-table storage (the type table is just a `Vec<Option<Type>>` indexed by `ExprId`).

8. For Hindley-Milner type inference, the core data structure is a union-find (disjoint set) of type variables. Each type variable either points to another type variable (they are unified) or to a concrete type. Unification is `union` on this structure. The occurs check prevents cycles (prevents `T = List<T>`).

9. Implement the union-find as a `Vec<TypeVar>` where each `TypeVar` is either `Bound(TypeId)` (pointing to a concrete type) or `Unbound(u32)` (with a unique ID and a level for generalization). Use path compression for efficient lookups.

10. Generalization and instantiation: when a `let` binding is typed, generalize its type by converting any unbound type variables whose level is higher than the current scope level into universally quantified variables. When the binding is used, instantiate by replacing all quantified variables with fresh unbound variables. This is how `let id = |x| x` gives `id` the type `forall T. T -> T`.

11. For exhaustiveness checking, model it as a matrix problem. Each row in the matrix is a pattern, each column is a constructor position. The algorithm recursively checks: for each possible constructor of the scrutinee type, is there at least one row that matches it? If not, that constructor is missing. The classic reference is Maranget's "Warnings for pattern matching" (2007).

12. For the diagnostic renderer, work backwards from the desired output. First, design the exact format you want (look at `rustc` output, take screenshots, count spaces). Then implement the renderer to produce that exact format. The details matter enormously: how many spaces before the pipe `|`, whether the line number is right-aligned, where underscores `^` vs tildes `~` are used.

13. For multi-line spans in diagnostics, show the first line and last line of the span with `...` in between if the span is too long. For single-line spans, show the line with underscores beneath the span. For zero-length spans (insertion points), use a caret `^`.

14. Color handling: use ANSI escape codes directly, or use the `termcolor` or `yansi` crate. Detect color support via the `NO_COLOR` environment variable (see https://no-color.org/) and `isatty` on stdout/stderr.

15. For fuzzing, set up `cargo-fuzz` with a target that feeds arbitrary bytes to the lexer and parser. The invariant to test: the compiler never panics. It can produce errors, but it must not crash. Run `cargo fuzz run compile_target -- -max_len=10000 -timeout=10` for at least 10 minutes and fix any crashes.

16. Structure the crate as a library with a thin CLI binary. The library exposes: `lex(source: &str) -> Vec<Token>`, `parse(tokens: &[Token]) -> (Ast, Vec<Diagnostic>)`, `check(ast: &Ast) -> (TypeTable, Vec<Diagnostic>)`, and `render_diagnostic(source: &str, diag: &Diagnostic) -> String`. This makes the compiler embeddable and testable.

17. For the `--dump-ast` output, implement a custom pretty-printer, not `Debug`. Use indentation to show nesting, and include the span and inferred type (if available) on each node. This is invaluable for debugging the parser and type checker.

18. When implementing match expressions, parse the pattern language as a subset of the expression language at first (identifiers, literals, tuples), then distinguish patterns from expressions in a later pass. This simplifies the parser because patterns and expressions share syntax.

19. For recursive functions, require the return type annotation. Without it, you would need to solve a fixed-point equation during type inference (typing `f` requires typing the body, which calls `f`). With the annotation, you can add the function's type to the environment before checking the body. This is a standard simplification in Hindley-Milner implementations.

20. Test strategically: write snapshot tests for the lexer (input source -> expected token sequence), the parser (input source -> expected AST pretty-print), the type checker (input source -> expected type annotations), and the diagnostics (input source -> expected rendered error output). Snapshot tests are the most productive test type for a compiler because they catch regressions across the entire output.

21. Consider adding a REPL (`--repl` flag) as a stretch goal. A REPL needs incremental compilation: each line is parsed, type-checked, and evaluated in the context of previous lines. This forces your architecture to support incremental environments, which is a good design validation.

22. The most common parser bug: forgetting to consume the closing token. When parsing `if condition { body } else { body }`, make sure you consume the `}` after each block. Use a helper `expect(TokenKind::RBrace, "expected `}` to close if body")` that produces an error and recovers if the token is missing.
