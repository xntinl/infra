<!-- difficulty: insane -->
<!-- category: formal-verification -->
<!-- languages: [rust] -->
<!-- concepts: [symbolic-execution, constraint-solving, path-exploration, smt, abstract-interpretation] -->
<!-- estimated_time: 25-40 hours -->
<!-- bloom_level: create, evaluate, synthesize -->
<!-- prerequisites: [ast-representation, visitor-pattern, expression-trees, basic-solver-theory, rust-enums-advanced] -->

# Challenge 129: Symbolic Execution Engine

## Languages

Rust (stable, latest edition)

## Prerequisites

- Strong experience with AST representation and tree-walking interpreters
- Understanding of expression trees and recursive evaluation
- Familiarity with constraint satisfaction and basic solver theory
- Comfortable with Rust enums, pattern matching, and recursive data structures
- Knowledge of program analysis concepts (control flow, path conditions)

## Learning Objectives

- **Create** a symbolic execution engine that tracks symbolic rather than concrete values through program execution
- **Evaluate** path feasibility by generating and solving path constraints
- **Synthesize** concrete test inputs from symbolic path constraints that trigger specific program paths
- **Analyze** programs for assertion violations, division by zero, and out-of-bounds access using symbolic reasoning

## The Challenge

Build a symbolic execution engine for a simple imperative language. Unlike concrete execution (which runs a program with specific inputs), symbolic execution treats inputs as symbolic variables and tracks how they flow through computations. When execution reaches a branch, the engine explores both paths, accumulating *path constraints* that describe which inputs lead down each path.

Define a simple language with: integer variables, arithmetic (`+`, `-`, `*`, `/`), comparisons (`<`, `>`, `==`, `!=`, `<=`, `>=`), `if/else`, `while` (bounded), `assert`, and `assume`. Variables hold symbolic expressions, not concrete values. Arithmetic produces symbolic expressions (e.g., `x + 1`). Branches produce path constraints (e.g., `x > 0` on the true branch, `x <= 0` on the false branch).

Implement a constraint solver for linear integer arithmetic that can determine if a set of constraints is satisfiable and, if so, produce a satisfying assignment. Use this to detect: assertion violations (an `assert` whose negation is satisfiable), division by zero (a divisor that can be zero), and array out-of-bounds access.

The fundamental challenge is *path explosion*: the number of paths grows exponentially with the number of branches. Implement a bounded exploration strategy.

## Requirements

1. Define an AST for a simple language: `let x = expr`, `x = expr`, `if cond { ... } else { ... }`, `while cond { ... }` (with a loop unrolling bound), `assert(cond)`, `assume(cond)`, `return expr`
2. Represent symbolic values as expression trees: `Concrete(i64)`, `Symbol(name)`, `Add(Box<Expr>, Box<Expr>)`, `Sub`, `Mul`, `Div`, `Eq`, `Lt`, `Gt`, `Le`, `Ge`, `Ne`, `Not`, `And`, `Or`
3. Implement a symbolic executor that walks the AST, maintaining a symbolic store (variable -> symbolic expression) and a path constraint stack
4. At `if/else` branches, fork execution into two paths, each adding the appropriate constraint
5. Implement a constraint solver for linear integer arithmetic: determine satisfiability and produce a satisfying assignment (concrete values for symbolic variables)
6. Detect assertion violations: for each `assert(cond)`, check if `NOT(cond)` is satisfiable under the current path constraints; if so, report the violation with a concrete counterexample
7. Detect division by zero: for each division, check if the divisor can be zero under the current path constraints
8. Implement bounded loop unrolling: unroll `while` loops up to a configurable depth
9. Generate test inputs: for each explored path, produce a concrete input assignment that exercises that path
10. Report per-path results: path constraint, feasibility, detected bugs, generated inputs

## Hints

The constraint solver is the hardest part. For linear arithmetic over integers, a simple approach is to reduce constraints to the form `a*x + b*y + ... <= c` and solve using iterative bounds propagation. Alternatively, for this challenge, a brute-force solver that tries values in a bounded range is acceptable for small constraint sets.

`assume(cond)` adds a constraint to the path without forking. This lets users specify preconditions on inputs. If the path constraints become unsatisfiable after an `assume`, the path is infeasible and can be pruned.

## Acceptance Criteria

- [ ] AST represents the full specified language (let, assign, if/else, while, assert, assume, return)
- [ ] Symbolic store maps variables to symbolic expressions, not concrete values
- [ ] Branch execution forks into two paths with complementary constraints
- [ ] Constraint solver determines satisfiability of linear integer constraints
- [ ] Satisfiable constraints produce concrete variable assignments
- [ ] Assertion violations are detected when NOT(condition) is satisfiable
- [ ] Division by zero is detected when divisor can symbolically equal zero
- [ ] While loops are bounded by a configurable unrolling depth
- [ ] Each explored path reports its constraint, feasibility, and any bugs found
- [ ] Test inputs are generated for each feasible path
- [ ] At least three example programs demonstrate bug detection (assert violation, div-by-zero, combined)
- [ ] All tests pass with `cargo test`

## Research Resources

- [A Survey of Symbolic Execution Techniques (Baldoni et al.)](https://arxiv.org/abs/1610.00502) -- comprehensive survey of the field
- [KLEE: Unassisted and Automatic Generation of High-Coverage Tests (Cadar et al.)](https://www.doc.ic.ac.uk/~cristic/papers/klee-osdi-08.pdf) -- the landmark symbolic execution tool
- [Symbolic Execution for Software Testing: Three Decades Later (Cadar & Sen)](https://people.eecs.berkeley.edu/~ksen/papers/cacm2013.pdf) -- accessible overview of techniques and challenges
- [The Z3 Theorem Prover](https://github.com/Z3Prover/z3) -- state-of-the-art SMT solver, reference for constraint solving concepts
- [EXE: Automatically Generating Inputs of Death (Cadar et al.)](https://web.stanford.edu/~engler/exe-ccs-06.pdf) -- early practical symbolic execution system
