# 23. Recursive Descent Parser + AST Builder

<!--
difficulty: advanced
category: parsers-and-compilers
languages: [go, rust]
concepts: [recursive-descent, ast, error-recovery, visitor-pattern, source-locations, pretty-printing]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [go-advanced, rust-advanced, enums, interfaces, tree-structures, parsing-basics]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Solid understanding of recursive descent parsing (from challenges 6-8 or equivalent)
- Go interfaces and Rust trait objects for the visitor pattern
- Tree data structures and recursive traversal
- Understanding of scope and variable binding (conceptual level)
- Familiarity with at least one imperative language grammar (C, Go, Rust, JavaScript)

## Learning Objectives

- **Design** a grammar for a simple imperative language that balances expressiveness with parse-ability
- **Evaluate** error recovery strategies that continue parsing after syntax errors to report multiple issues in one pass
- **Analyze** trade-offs between panic-mode recovery and synchronization-point recovery in terms of error quality
- **Create** a well-typed AST with source location tracking that supports downstream analysis passes
- **Implement** the visitor pattern for tree walking, enabling pretty-printing and node counting as concrete visitors
- **Compare** how Go's interface-based dispatch and Rust's enum-based matching affect extensibility and type safety

## The Challenge

A parser that stops at the first error is useless in practice. Real compilers report as many errors as possible in a single pass so the programmer can fix them all at once. This requires **error recovery**: when the parser encounters invalid syntax, it must resynchronize to a known-good state and continue parsing.

The simplest recovery strategy is "panic mode": when a parse error occurs, the parser discards tokens until it reaches a **synchronization point** -- a token that reliably marks the start or end of a construct (semicolons, closing braces, keywords like `if` or `let`). The parser then resumes normal operation. This sounds crude but works well in practice -- GCC, Clang, and rustc all use variants of this approach.

Build a complete recursive descent parser for a simple programming language with variables, arithmetic, conditionals, loops, functions, and print statements. The parser must produce a typed AST where every node carries its source location (file, line, column). When the input contains syntax errors, the parser must recover and continue, collecting all errors to report at the end.

The AST is the central data structure that downstream passes (type checking, optimization, code generation) consume. A well-typed AST prevents entire categories of bugs: if the type system says a `WhileStmt` always has a `condition` of type `Expr` and a `body` of type `Block`, no downstream code needs to check for null or wrong-type nodes. Source locations on every node enable precise error messages in those later passes.

Implement the visitor pattern on the AST. Prove it works by building a pretty-printer visitor that regenerates formatted source code from the AST. The visitor pattern decouples traversal logic from the tree structure: adding a new pass (constant folding, dead code detection) means adding a new visitor, not modifying every AST node.

## Requirements

1. Define a grammar supporting: `let` variable declarations, assignment statements, arithmetic expressions (`+`, `-`, `*`, `/`), comparison operators (`==`, `!=`, `<`, `>`, `<=`, `>=`), boolean operators (`and`, `or`, `not`), `if`/`else` blocks, `while` loops, function definitions with parameters, function calls, `return` statements, and `print` statements
2. Every AST node must carry a `Span` (start line:column, end line:column) tracing back to the source
3. The AST must be well-typed: each node type is a distinct type/variant, not a generic "Node" with a string tag
4. Implement error recovery using synchronization points: on a parse error, advance tokens until you find a statement boundary (`;`, `}`, or a keyword like `let`, `if`, `while`, `fn`, `return`) and resume parsing
5. The parser must report multiple errors from a single parse attempt (at least 3 different errors in a single input)
6. Errors must include position and context: "Expected ')' to close function arguments at 5:12, found '}'"
7. Implement the visitor pattern: define a trait/interface with one method per AST node type, and implement at least two visitors (pretty-printer and a simple node counter)
8. The pretty-printer must produce valid, consistently formatted source code from the AST
9. Implement a second visitor: a node counter that reports how many nodes of each type exist in the AST
10. Both Go and Rust implementations must handle the same language grammar

## Hints

The hints for advanced challenges are text-only -- no code snippets.

**Hint 1**: Start by writing the grammar in BNF or EBNF before writing any code. The grammar drives the structure of every parsing function. A function per grammar production is the standard recursive descent approach. Get the grammar right on paper first -- fixing grammar ambiguities in code is much harder.

**Hint 2**: Error recovery works by catching parse failures at statement boundaries. When a statement-level parse function fails, it enters "panic mode" -- advancing tokens until it finds a synchronization token (`;`, `}`, or a statement-starting keyword). Then it resumes normal parsing. The key design choice is where to catch errors: too high (program level) and you miss most errors; too low (expression level) and you get cascading false errors.

**Hint 3**: The visitor pattern in Rust uses an enum for AST nodes and a trait with one method per variant. In Go, use an interface with `Visit(node Node)` or a method per node type. The double-dispatch pattern (node calls `visitor.visitThisNodeType(self)`) gives type safety. Consider providing default implementations that recursively visit children -- most visitors only override a few node types.

**Hint 4**: Source location tracking requires threading a `Span` through every AST constructor. Build the span from the first and last token of each production. The parser's `expect` function should record where it was when it called, not just where the error was. A common pattern is a `span_from(start_token, end_token)` helper.

**Hint 5**: When comparing Go and Rust implementations, notice how Go's visitor requires interface methods for every node type (or a type switch), while Rust's `match` on an enum is exhaustive at compile time. This means adding a new AST node in Rust breaks all visitors at compile time, forcing you to handle it. In Go, a missing case compiles silently and produces a runtime bug.

## Acceptance Criteria

- [ ] Parser handles valid programs: variable declarations, arithmetic, if/else, while, functions, print
- [ ] Every AST node includes source span (start and end positions)
- [ ] Parser recovers from errors and reports at least 3 errors from a single malformed input
- [ ] Error messages include line:column and describe expected vs actual tokens
- [ ] Visitor trait/interface defined with methods for all node types
- [ ] Pretty-printer visitor produces valid, re-parseable source code from the AST
- [ ] Node-counter visitor correctly counts each node type
- [ ] Both Go and Rust handle the same test inputs
- [ ] All tests pass (`go test ./...` and `cargo test`)
- [ ] No panics or crashes on malformed input -- all errors are reported through the error list
- [ ] The grammar handles operator precedence correctly: `1 + 2 * 3` parses as `1 + (2 * 3)`
- [ ] Function calls with nested expressions parse: `foo(1 + 2, bar(3))`

## Research Resources

- [Crafting Interpreters: Parsing Expressions](https://craftinginterpreters.com/parsing-expressions.html) -- recursive descent fundamentals
- [Crafting Interpreters: Statements and State](https://craftinginterpreters.com/statements-and-state.html) -- variable declarations, scoping
- [Error Recovery in Recursive Descent Parsers](https://supunsetunga.medium.com/writing-a-parser-syntax-error-handling-b57b8989147b) -- synchronization strategies
- [GCC Error Recovery](https://gcc.gnu.org/wiki/ErrorRecovery) -- how a production compiler handles recovery
- [Visitor Pattern (Refactoring Guru)](https://refactoring.guru/design-patterns/visitor) -- pattern explanation with examples in multiple languages
- [Rust Design Patterns: Visitor](https://rust-unofficial.github.io/patterns/patterns/behavioural/visitor.html) -- Rust-specific visitor idioms
- [The Lox Language Grammar](https://craftinginterpreters.com/appendix-i.html) -- a complete grammar for a simple language, useful as design reference
- [Roslyn (C# Compiler) Error Recovery](https://github.com/dotnet/roslyn/wiki/Roslyn-Overview) -- how a production IDE compiler maximizes error reporting
- [Bob Nystrom: Crafting Interpreters (full book)](https://craftinginterpreters.com/) -- builds a complete parser with error recovery, AST, and visitors from scratch
- [matklad: Resilient LL Parsing](https://matklad.github.io/2023/05/21/resilient-ll-parsing-tutorial.html) -- modern error-resilient parsing for IDE-quality parsers
