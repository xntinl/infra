<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Closures and First-Class Functions

First-class functions and closures are the features that separate a scripting language from a genuinely powerful programming tool. When functions can be passed as arguments, returned from other functions, stored in data structures, and close over their defining environment, entirely new programming paradigms become possible: callbacks, decorators, iterators, partial application, and more. Your task is to ensure your interpreter handles these patterns flawlessly, including the subtle edge cases around mutable closed-over variables, recursive closures, closures in loops, and higher-order function composition. This exercise is less about new code and more about correctness, edge cases, and advanced patterns.

## Requirements

1. Implement and verify immediate function invocation (IIFE): `let result = fn(x) { x * 2 }(21);` should evaluate to `42`. The parser must handle a function literal immediately followed by call arguments. Verify that the function's environment is isolated -- variables defined inside the IIFE are not visible outside.

2. Implement higher-order functions that return functions (function factories): `let multiplier = fn(factor) { fn(x) { factor * x } };` creates a closure that captures `factor`. Verify that `let double = multiplier(2); let triple = multiplier(3); double(5)` returns `10` and `triple(5)` returns `15`. The closed-over `factor` in each closure must be independent -- modifying one must not affect the other.

3. Implement mutable closures: when a closure captures a variable from an outer scope, mutations to that variable (via reassignment) must be visible to the closure, and vice versa. Implement this by having closures capture the `Environment` by reference (pointer), not by copying values. Verify: `let counter = fn() { let count = 0; let inc = fn() { count = count + 1; count }; inc }; let c = counter(); c()` returns `1`, `c()` returns `2`, `c()` returns `3`. The `count` variable is shared between the outer scope and the `inc` closure.

4. Handle the classic closure-in-loop problem: when closures are created inside a loop, they must each capture their own copy of the loop variable (or the shared variable's value at that iteration, depending on language semantics). Implement and test both behaviors: the "JavaScript var" behavior (all closures share the same variable, seeing the final value) and a `let`-per-iteration behavior (each iteration creates a new scope, so each closure captures a different value). Document which is the default and provide the mechanism for the other.

5. Implement recursive closures: a named function that calls itself recursively must work when assigned to a variable. Handle the binding order issue: in `let fact = fn(n) { if (n < 2) { 1 } else { n * fact(n - 1) } };`, the identifier `fact` must be resolvable inside the function body even though the function is being defined as the value of `fact`. Implement this by pre-binding the name in the environment before evaluating the function body, or by using a forward reference that is filled in after the let binding completes.

6. Implement function composition utilities entirely within the language (not as built-ins): `let compose = fn(f, g) { fn(x) { f(g(x)) } };` and `let pipe = fn(...fns) { fn(x) { reduce(fns, x, fn(acc, f) { f(acc) }) } };`. Verify that `let addOne = fn(x) { x + 1 }; let double = fn(x) { x * 2 }; let addOneThenDouble = compose(double, addOne); addOneThenDouble(5)` returns `12`. Verify pipe with 5+ functions chained.

7. Implement partial application (currying): `let partial = fn(f, ...bound) { fn(...args) { let allArgs = push(bound, ...args); f(...allArgs) } };` where `...` is the spread operator for variadic functions and array expansion in call arguments. If you haven't implemented variadic functions and spread, implement them now. Verify: `let add = fn(a, b, c) { a + b + c }; let add5 = partial(add, 5); add5(3, 2)` returns `10`.

8. Write tests covering: IIFE with environment isolation, function factories producing independent closures, mutable closures (counter pattern), closure-in-loop with both shared and per-iteration semantics, recursive factorial and fibonacci via closures, mutual recursion (`isEven`/`isOdd` calling each other), deeply nested closures (5+ levels of nesting), function composition with 5+ functions, partial application with various arities, closures stored in arrays and hashes and called later, a closure that returns another closure that returns another closure (curried function), and memory verification that unreferenced closures and their environments can be garbage collected (verify with `runtime.MemStats` that memory does not grow unboundedly when creating and discarding closures in a loop).

## Hints

- The key to mutable closures is that the `Function` object stores a pointer to the `Environment`, not a copy. When the closure accesses a variable, it walks the environment chain and finds the current value, including mutations made after the closure was created.
- For recursive closures, one approach: when evaluating `let fact = fn(n) { ... }`, first create the environment binding `fact = null`, then evaluate the function literal (which captures the environment containing the `fact` binding), then update the binding to point to the new function. Now the function's closure environment contains a reference to itself.
- The closure-in-loop problem: `for (let i = 0; i < 5; i++) { push(fns, fn() { i }) }` -- if the for loop creates a new environment for each iteration (as in JavaScript's `let`), each closure captures a different `i`. If it reuses the same environment (as in JavaScript's `var`), all closures see the final value of `i`.
- For variadic functions (`fn(...args)`), the rest parameter collects excess arguments into an array. For spread in calls (`f(...arr)`), expand the array into individual arguments.
- Go's garbage collector handles memory for environments automatically since they are heap-allocated and referenced by pointers. Closures that go out of scope will be collected along with their captured environments.

## Success Criteria

1. IIFE expressions evaluate correctly and their internal variables are not accessible from outside.
2. Function factories produce independent closures with separate captured state.
3. The counter pattern (mutable closure) correctly increments across multiple calls, proving closures share the environment by reference.
4. Closure-in-loop behavior matches the documented semantics, with tests demonstrating both shared-variable and per-iteration-scope behavior.
5. Recursive closures (factorial of 20, fibonacci of 30) produce correct results without stack overflow or infinite loops.
6. Function composition and partial application produce correct results for all tested combinations.
7. Variadic functions and spread operator work correctly for both definition and call sites.
8. Creating and discarding 100,000 closures in a loop does not cause memory to grow unboundedly (verified by checking memory stats before and after GC).

## Research Resources

- [Closures in Depth (MDN Web Docs)](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Closures)
- [Writing An Interpreter In Go - Functions and Closures (Thorsten Ball)](https://interpreterbook.com/)
- [Crafting Interpreters - Closures](https://craftinginterpreters.com/closures.html)
- [Lambda Calculus and Closures](https://en.wikipedia.org/wiki/Closure_(computer_programming))
- [The Problem with Closures in Loops](https://developer.mozilla.org/en-US/docs/Web/JavaScript/Closures#creating_closures_in_loops_a_common_mistake)
- [Currying vs Partial Application](https://www.baeldung.com/cs/currying-vs-partial-application)
