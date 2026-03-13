<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 4h+
-->

# Full Monkey Language Interpreter

This is the culmination of the language interpreter capstone. You will integrate the lexer, Pratt parser, AST, tree-walking evaluator, built-in functions, closure system, and REPL into a complete, polished programming language implementation. Beyond integration, you will add a module/import system, a testing framework written in the language itself, a standard library, error handling with stack traces, performance optimization, and a command-line tool that can run scripts, start the REPL, or format source code. The result will be a language that someone could genuinely use for scripting and learning.

## Requirements

1. Create a unified `monkey` command-line tool with subcommands: `monkey run <file.mk>` executes a script file, `monkey repl` starts the interactive REPL, `monkey fmt <file.mk>` formats a source file (parse + pretty-print), `monkey ast <file.mk>` prints the AST for debugging, `monkey tokens <file.mk>` prints the token stream, and `monkey test <file.mk>` runs test functions in the file. Use `os.Args` parsing (no external CLI libraries) with `--help` support for each subcommand. Exit with appropriate codes (0 success, 1 runtime error, 2 usage error).

2. Implement a module/import system: `import("path/to/module.mk")` loads and evaluates a source file, returning a hash object containing all top-level `let` bindings as key-value pairs. Module loading is cached -- importing the same path twice returns the same module object. Implement relative path resolution (relative to the importing file's directory) and a `MONKEY_PATH` environment variable for library lookup. Circular imports must be detected and produce a clear error rather than infinite recursion.

3. Implement a standard library as a set of `.mk` files distributed with the interpreter: `std/math.mk` (additional math utilities beyond the built-ins), `std/string.mk` (string manipulation functions implemented in Monkey), `std/array.mk` (sorting, searching, flattening, chunking), `std/hash.mk` (deep merge, invert, group-by), `std/functional.mk` (compose, pipe, curry, memoize, throttle), and `std/test.mk` (the testing framework). Each module must be importable via `import("std/math")`.

4. Implement a testing framework as a standard library module (`std/test.mk`): `let t = import("std/test"); t.assert(condition, message)`, `t.assertEqual(actual, expected)`, `t.assertError(fn)` (verify that calling fn produces an error), `t.describe("name", fn)` for test suites, and `t.it("description", fn)` for individual test cases. The `monkey test` subcommand finds all functions whose names start with `test_` and runs them, reporting pass/fail with colored output, execution time per test, and a summary.

5. Implement enhanced error handling with stack traces: when a runtime error occurs, capture the call stack (function name, source file, line number) at each level. Display the stack trace in reverse (most recent call first) with source context showing the offending line. Implement `try(fn)` as a built-in that catches errors: `let result = try(fn() { riskyOperation() })` returns `{"ok": true, "value": result}` on success or `{"ok": false, "error": errorMsg}` on error. Implement `panic(message)` for unrecoverable errors.

6. Implement performance optimizations: (a) intern common integer objects (-5 to 256) and boolean/null singletons to avoid allocation, (b) implement tail-call optimization for recursive functions where the recursive call is in tail position (reuse the current stack frame instead of creating a new one), (c) cache compiled regular expressions for `match` operations, (d) implement a string interning pool for identifier lookups, and (e) add an optional `--profile` flag to `monkey run` that reports execution time per function and call counts.

7. Implement additional language features that make Monkey genuinely useful: (a) destructuring assignment `let [a, b, c] = array;` and `let {x, y} = hash;`, (b) string interpolation `` `Hello, ${name}!` `` using backtick strings, (c) the `match` expression for pattern matching `match value { 1 => "one", 2 => "two", _ => "other" }`, (d) method-like syntax via dot notation `array.map(fn)` as sugar for `map(array, fn)`, and (e) operator overloading or protocols for user-defined types (stretch goal).

8. Write integration tests that exercise the complete system: a script that imports multiple modules, uses the standard library, defines functions with closures, handles errors with try/catch, performs I/O, and produces correct output. A test suite for the standard library itself (test each function in each module). Performance benchmarks: recursive fibonacci(35) completes in under 10 seconds, array operations on 100,000 elements complete in under 5 seconds, the interpreter can handle a 10,000-line source file without memory issues. A comprehensive test using `monkey test` on a non-trivial test file with at least 50 test cases covering all language features.

## Hints

- For the module system, maintain a `moduleCache map[string]*Object` keyed by absolute file path. When `import("path")` is called: resolve the path, check the cache, read the file, evaluate it in a fresh environment, extract the top-level bindings into a hash, cache and return it.
- Circular import detection: maintain a `loading set[string]` of files currently being loaded. If an import request hits a file already in the loading set, that's a circular dependency.
- For tail-call optimization, detect when a function's last expression is a call to itself. Instead of recursively calling `Eval`, replace the current environment's parameter bindings with the new arguments and restart evaluation of the function body (a loop instead of recursion).
- String interpolation in backtick strings: scan for `${...}` sequences, extract the expression between `${}`, parse and evaluate it, and concatenate the results. This requires calling the parser from within the evaluator.
- The testing framework can be mostly implemented in Monkey itself, with a few built-in hooks (like `testRunner` that collects results). The `monkey test` subcommand evaluates the file, finds test functions, calls them, and reports results.
- For `--profile`, wrap each function call in timing code: record `time.Now()` before the call, compute the duration after, and accumulate per-function statistics in a global map.

## Success Criteria

1. `monkey run script.mk` correctly executes a multi-file program using imports, producing expected output.
2. `monkey repl` provides a fully featured interactive environment with history, completion, and highlighting.
3. `monkey fmt` produces consistently formatted output that re-parses to an equivalent AST.
4. `monkey test` discovers and runs test functions, reporting results with pass/fail counts and timing.
5. The module system correctly handles relative imports, `MONKEY_PATH` lookup, caching, and circular import detection.
6. The standard library modules are importable and all their functions work correctly (verified by their own test suites).
7. Error messages include stack traces with file names and line numbers across module boundaries.
8. Tail-call optimized recursive functions handle 1,000,000+ recursion depth without stack overflow.
9. All benchmarks meet their targets: fib(35) < 10s, 100k array ops < 5s, 10k-line file loads without issues.

## Research Resources

- [Writing An Interpreter In Go (Thorsten Ball)](https://interpreterbook.com/)
- [Writing A Compiler In Go (Thorsten Ball)](https://compilerbook.com/)
- [Crafting Interpreters (Bob Nystrom)](https://craftinginterpreters.com/)
- [Structure and Interpretation of Computer Programs](https://web.mit.edu/6.001/6.037/sicp.pdf)
- [Monkey Language Specification](https://monkeylang.org/)
- [Tail Call Optimization in Interpreters](https://blog.klipse.tech/lambda/2016/12/31/tco-in-clojure.html)
- [Module Systems in Programming Languages](https://www.cs.cmu.edu/~rwh/students/lee.pdf)
