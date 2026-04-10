# 31. Build a Test Framework

**Difficulty**: Insane

---

## Prerequisites

- Elixir macros: `defmacro`, `quote`, `unquote`, `__using__`
- Elixir process isolation and `Task.Supervisor`
- `Module.__info__/1` and compile-time attribute accumulation
- Understanding of property-based testing theory (shrinking, generators)
- OTP `Application` lifecycle and code loading
- Elixir Mix task infrastructure

---

## Problem Statement

Build a complete test framework from scratch. It must discover and execute tests defined using a macro DSL, provide a rich set of assertions, format results, and support the full lifecycle of setup and teardown. The framework must:

1. Discover test modules at compile time by scanning for modules that `use` the framework
2. Run each test in an isolated process, capturing exits and exceptions without crashing the runner
3. Provide assertions that produce informative failure messages showing the left-hand and right-hand values
4. Support setup callbacks that run before each test and teardown callbacks guaranteed to run even if the test fails
5. Allow tests to be tagged and filtered by tag when running
6. Execute async-safe tests concurrently, ensuring they do not interfere with each other
7. Include a basic property-based testing module with typed generators and shrinking of failing cases

---

## Acceptance Criteria

- [ ] Test discovery: `use MyFramework.Case` registers the module as a test module via a module attribute; a Mix task or entry point collects all registered modules and their test functions without executing them; test functions are identified by name prefix `test_` or by macro registration
- [ ] Test runner: each test runs in its own process spawned under a `Task.Supervisor`; if the test process exits abnormally, the result is `:error` with the reason; if it times out, the result is `:timeout`; the runner collects results without crashing even if all tests fail
- [ ] Assertions: `assert expr` reports the value of `expr` if false; `refute expr` reports the value if true; `assert_raise ExceptionType, fn` verifies an exception is raised; `assert_receive pattern, timeout` waits for a message matching the pattern; all failures include file and line information
- [ ] Formatters: at least two formatters — `dot` (one character per test: `.` pass, `F` fail, `E` error) and `verbose` (one line per test with name and duration); a `json` formatter emits a machine-readable result array; formatter is selectable at runtime
- [ ] Setup/teardown: `setup/1` callback runs before each test and can inject context into the test via a map; `on_exit/1` registers a cleanup function that runs after the test regardless of outcome; nested `describe` blocks have their own `setup` that composes with the outer one
- [ ] Tags: `@tag :slow`, `@tag integration: true`; the runner accepts `--only slow`, `--exclude integration`, and `--only key:value`; untagged tests run by default when no `--only` is specified
- [ ] Property-based testing: `check all x <- integer(), y <- string()` runs the property function 100 times with generated values; when a failure is found, the framework shrinks the inputs to the minimal failing case; built-in generators: `integer/0`, `integer/1`, `float/0`, `string/0`, `boolean/0`, `list_of/1`, `one_of/1`
- [ ] Parallel execution: tests marked `async: true` run concurrently; the framework ensures their setup/teardown do not share mutable state; tests not marked async run sequentially after all async tests complete; the total run time is measurably shorter when async tests are parallelized

---

## What You Will Learn

- Elixir macro system: accumulating test definitions at compile time
- Process isolation as a unit test primitive
- How ExUnit implements assertion introspection (the `assert` macro inspects the AST)
- Property-based testing: random generation, shrinking strategies, and the `StreamData` model
- Formatter abstraction and event-driven test reporting
- Compose-able `describe` scopes and how setup context propagates
- Concurrent test execution without global state interference

---

## Hints

- Study ExUnit source code — it is well-structured and the macro layer is a perfect reference
- Research how `assert` uses AST pattern matching to extract the left and right operands for failure messages
- Investigate `@tag` accumulation using `Module.register_attribute/3` with `accumulate: true`
- Property-based shrinking requires a strategy per generator type — integers shrink toward 0, lists shrink by removing elements and shrinking elements
- Think about how `on_exit` hooks must run even when the test process is killed — they need to run in a separate monitor process
- Look into how StreamData implements `Enumerable` for lazy generator composition

---

## Reference Material

- ExUnit source code (github.com/elixir-lang/elixir, lib/ex_unit)
- "Property-Based Testing with PropEr, Erlang, and Elixir" — Fred Hebert
- StreamData documentation (hex.pm/packages/stream_data)
- "Metaprogramming Elixir" — Chris McCord
- PropEr source code for shrinking strategy reference (github.com/proper-testing/proper)

---

## Difficulty Rating ★★★★★★

Building a macro-based DSL, property-based testing with shrinking, and a correct parallel runner that isolates state all at once is a complete study of Elixir's most advanced features.

---

## Estimated Time

50–80 hours
