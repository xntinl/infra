# 36. Complete Language Interpreter

<!--
difficulty: insane
category: compilers-interpreters
languages: [rust]
concepts: [tree-walking-interpreter, lexer, parser, ast, closures, environments, error-handling, repl]
estimated_time: 20-30 hours
bloom_level: create
prerequisites: [recursive-descent-parsing, ast-construction, hash-maps, closures, error-handling, trait-objects]
-->

## Languages

- Rust (1.75+ stable)

## The Challenge

Build a complete tree-walking interpreter for a Turing-complete language in Rust. The language must be expressive enough to write real programs: variables, functions with closures, control flow, compound data structures, and error handling. The interpreter must support both a REPL with line editing and source file execution with meaningful runtime error messages including stack traces.

This is not a toy calculator or a Brainfuck interpreter. You are building a language that someone could actually use to solve problems -- with first-class functions, closures that capture their environment, arrays, hashmaps, and a standard library that includes higher-order functions like map, filter, and reduce.

The interpreter pipeline has four stages: the lexer scans source text into tokens with position information. The parser consumes tokens and builds an AST, recovering from errors to report multiple issues per run. The evaluator walks the AST recursively, evaluating each node against an environment chain that implements lexical scoping. The REPL wraps this pipeline with line editing and persistent state between inputs.

## Acceptance Criteria

- [ ] Lexer tokenizes the full language syntax with position tracking (line and column)
- [ ] Parser produces a well-typed AST with error recovery (reports multiple parse errors, does not stop at the first one)
- [ ] Variables with `let` (mutable) and `const` (immutable) bindings
- [ ] Data types: integers (64-bit), floats (64-bit), strings, booleans, null, arrays, hashmaps
- [ ] Arithmetic operators: `+`, `-`, `*`, `/`, `%` with type coercion rules (int + float = float)
- [ ] Comparison operators: `==`, `!=`, `<`, `>`, `<=`, `>=`
- [ ] Logical operators: `&&`, `||`, `!`
- [ ] String concatenation with `+` and interpolation
- [ ] Control flow: `if`/`else if`/`else`, `while`, `for..in` loops, `break`, `continue`
- [ ] Functions: declaration, first-class (assignable to variables, passable as arguments), closures that capture enclosing scope
- [ ] Recursive functions work correctly (fibonacci, factorial)
- [ ] Error handling: `try`/`catch` blocks or a Result-based mechanism
- [ ] Standard library built-ins: `print`, `println`, `len`, `type_of`, `push`, `pop`, `keys`, `values`, `map`, `filter`, `reduce`, `range`, `to_string`, `to_int`, `to_float`
- [ ] REPL with line editing (arrow keys, history) using the `rustyline` crate
- [ ] Source file execution: `./interpreter run file.lang`
- [ ] Runtime errors include: error message, source location (file, line, column), and call stack trace
- [ ] No panics on any valid or invalid input -- all errors are caught and reported cleanly
- [ ] A non-trivial test program (at least 50 lines) that exercises all features runs correctly

## Starting Points

- **Crafting Interpreters, Part II**: Robert Nystrom's `jlox` is the canonical tree-walking interpreter. Study how environments chain for lexical scoping and how closures capture their enclosing environment at declaration time, not call time.
- **Environment as linked list**: Each scope is a HashMap with a pointer to its parent. Variable lookup walks the chain. Closure creation snapshots the current environment pointer.
- **Pratt parsing**: Vaughan Pratt's top-down operator precedence parsing handles infix, prefix, and mixfix operators elegantly. Study `matklad`'s [Simple but Powerful Pratt Parsing](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) for a Rust-native approach.

## Hints

1. The hardest design decision is value representation. You need a type that can be an int, float, string, bool, null, array, hashmap, or function -- and functions capture an environment. `Rc<RefCell<...>>` for mutable shared state is the standard approach for a tree-walker. Fight the urge to use `unsafe` for this.

2. Closures are the most subtle feature. A closure is a function value paired with the environment that was active when the function was defined. When called, the closure executes in its captured environment, not the caller's. Get this wrong and nested closures will silently share or lose variables.

3. Error recovery in the parser is worth the investment. Synchronize on statement boundaries (semicolons, keywords) after an error and keep parsing. Reporting five errors at once saves the user from fix-one-recompile-fix-next cycles.

## Going Further

- Add a type system: optional type annotations with inference, checked at parse time
- Compile to bytecode and run on a stack VM (connects to Challenge 37)
- Add modules and imports
- Implement tail-call optimization for recursive functions
- Add pattern matching with destructuring

## Resources

- [Crafting Interpreters (full book)](https://craftinginterpreters.com/) -- the definitive resource, Chapters 4-13 cover the tree-walking interpreter
- [Writing An Interpreter In Go (Thorsten Ball)](https://interpreterbook.com/) -- the Monkey language, similar scope to this challenge
- [Pratt Parsing in Rust (matklad)](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) -- clean Pratt parser implementation in Rust
- [Make A Language (arzg)](https://arzg.github.io/lang/) -- series building a language in Rust from scratch
- [Engineering a Compiler (Cooper & Torczon)](https://www.cs.rice.edu/~keith/Errata.html) -- textbook covering lexing, parsing, and IR design
- [Rust `rustyline` crate](https://docs.rs/rustyline/latest/rustyline/) -- readline implementation for the REPL
- [Bob Nystrom: Pratt Parsers: Expression Parsing Made Easy](https://journal.stuffwithstuff.com/2011/03/19/pratt-parsers-expression-parsing-made-easy/) -- the original explanation that inspired matklad's Rust version
