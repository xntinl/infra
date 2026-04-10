# 25. Build a Lisp Interpreter

**Difficulty**: Insane

---

## Prerequisites

- Elixir pattern matching and recursive data structures
- Understanding of lexing, parsing, and evaluation concepts
- Functional programming: closures, higher-order functions, recursion
- Tail call optimization (TCO) theory
- Elixir `IO` and process management for REPL
- Familiarity with Scheme or any Lisp dialect

---

## Problem Statement

Build a complete interpreter for a Scheme-like Lisp dialect. The system must:

1. Lex raw source text into a flat token stream, correctly handling nested structures, strings with escape sequences, and numeric literals
2. Parse the token stream into an AST represented as nested Elixir lists and atoms, faithfully preserving the homoiconic nature of Lisp
3. Evaluate the AST in a lexical environment, looking up symbols, applying procedures, and evaluating forms recursively
4. Implement all required special forms with correct semantics, including proper `tail call optimization` so deeply recursive programs do not overflow the call stack
5. Support first-class closures that capture their definition-time lexical environment
6. Provide a standard library of list-processing functions sufficient to write non-trivial programs
7. Offer an interactive REPL with line editing and command history

---

## Acceptance Criteria

- [ ] Lexer: tokenizes `(`, `)`, symbols, integers, floats, strings (with `\"` and `\\` escapes), `#t`, `#f`, and `'` (quote shorthand); returns a list of typed tokens with position information
- [ ] Parser: converts a token list to a nested Elixir structure; `(a (b c) d)` becomes `[:a, [:b, :c], :d]`; reports parse errors with line and column
- [ ] Evaluator: evaluates atoms as symbol lookup, lists as procedure application, and recognizes special forms by name
- [ ] Special forms: `define`, `lambda`, `if`, `begin`, `quote`, `let`, `let*`, `letrec`, `and`, `or`, `cond`, `set!`
- [ ] Tail call optimization: `(define (fact n acc) (if (= n 0) acc (fact (- n 1) (* n acc))))` with `(fact 1000000 1)` completes without stack overflow
- [ ] Standard library: `car`, `cdr`, `cons`, `list`, `null?`, `pair?`, `number?`, `symbol?`, `equal?`, `map`, `filter`, `for-each`, `apply`, `+`, `-`, `*`, `/`, `<`, `>`, `=`, `not`, `display`, `newline`
- [ ] Closures: `(define (make-adder n) (lambda (x) (+ x n)))` returns a closure; the closure correctly captures `n` from its definition environment regardless of how it is called later
- [ ] REPL: starts an interactive loop that reads a line, evaluates it, prints the result, and repeats; supports multi-line input when parentheses are unbalanced; keeps history across inputs in the session

---

## What You Will Learn

- Implementing a complete language pipeline: lex → parse → eval
- Environment chains as persistent linked maps for lexical scope
- Trampolining or continuation-passing style for tail call optimization
- Homoiconicity and `quote` semantics
- Building a REPL with proper error recovery (don't crash on bad input)
- Macro expansion as a pre-evaluation transformation
- Why Lisp is the ideal language for studying language implementation

---

## Hints

- Research "metacircular evaluator" — the SICP chapter on it is the canonical reference
- Study trampolining in Elixir as a technique to convert tail calls to heap-allocated continuations
- Investigate how environments are implemented as a linked list of maps (child → parent chain)
- Think about how `set!` differs from `define` and what it means for the environment model
- Look into `read` as a separate phase — Lisp's `read` is itself a parser for s-expressions
- Research how `apply` works when the argument list is itself a runtime value

---

## Reference Material

- "Structure and Interpretation of Computer Programs" (SICP) — Abelson & Sussman, MIT Press
- "The Little Schemer" — Friedman & Felleisen
- R7RS Small Language Specification (scheme.org)
- "Lisp in Small Pieces" — Queinnec (advanced)
- Peter Norvig's "lis.py" as a minimal reference implementation

---

## Difficulty Rating ★★★★★★

Tail call optimization without language-level support and proper closure semantics make this a deep dive into the foundations of programming language theory.

---

## Estimated Time

40–70 hours
