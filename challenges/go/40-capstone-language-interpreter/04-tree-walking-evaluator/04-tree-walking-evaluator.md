<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Tree-Walking Evaluator

The evaluator is where your language comes to life. A tree-walking evaluator recursively traverses the AST, executing each node directly: evaluating expressions to produce values, executing statements for their effects, and managing an environment of variable bindings. Your task is to build a complete tree-walking evaluator in Go that supports integers, floats, strings, booleans, null, arrays, hash maps, first-class functions, control flow, error propagation, and a type system that handles coercions and type errors gracefully. By the end, you will have a working programming language.

## Requirements

1. Define the object system representing runtime values: `Object` interface with `Type() ObjectType` and `Inspect() string`. Implement concrete types: `Integer` (int64), `Float` (float64), `String` (string), `Boolean` (bool), `Null`, `Array` ([]Object), `Hash` (map with Object keys), `Function` (parameters + body + closure environment), `ReturnValue` (wrapper for propagating returns), `Error` (message + stack trace), and `Break`/`Continue` (signal objects for loop control). Implement `Hashable` interface for objects that can be hash map keys (Integer, String, Boolean).

2. Implement the `Environment` as a linked chain of scopes. Each `Environment` has a `store map[string]Object` and an `outer *Environment` pointer. `Get(name string) (Object, bool)` walks the chain outward until the variable is found or the chain is exhausted. `Set(name string, val Object)` creates or updates a variable in the current scope. `Update(name string, val Object) error` walks the chain and updates the variable where it was defined (for assignment to outer-scope variables). Implement `NewEnclosedEnvironment(outer *Environment) *Environment` for function calls and blocks.

3. Implement `Eval(node ast.Node, env *Environment) Object` as a recursive function that switches on the node type. For expressions: integer/float/string/boolean/null literals return their corresponding objects; identifiers look up the environment; prefix expressions apply the operator to the evaluated operand; infix expressions evaluate both sides and apply the operator; if-else expressions evaluate the condition and choose a branch; function literals capture the current environment as a closure. For statements: let/const bind the evaluated value to the name; return wraps the value in a ReturnValue; expression statements evaluate and discard.

4. Implement arithmetic and comparison operations with type coercion rules: Integer op Integer -> Integer (with overflow detection), Float op Float -> Float, Integer op Float -> Float (promote integer), String + String -> String (concatenation), any == any -> Boolean (structural equality for arrays and hashes, reference equality for functions). Division by zero produces an Error object. Comparison operators (<, >, <=, >=) work on integers, floats, and strings (lexicographic). Boolean operators (&&, ||) use short-circuit evaluation.

5. Implement function calls: evaluate the function expression (may be an identifier, a literal, or any expression returning a Function), evaluate all arguments left-to-right, create a new enclosed environment extending the function's closure environment, bind parameters to argument values in the new environment, evaluate the function body, and unwrap any ReturnValue. Handle arity mismatch (wrong number of arguments) as an error. Support variadic functions with a rest parameter syntax (`fn(a, b, ...rest)`).

6. Implement control flow: `if`/`else` with truthiness rules (false and null are falsy, everything else including 0 and "" is truthy -- or choose Python-style where 0 and "" are also falsy, but document your choice), `while` loops that evaluate the body repeatedly while the condition is truthy, `for` loops with init/condition/update, `break` to exit the nearest enclosing loop, `continue` to skip to the next iteration, and `return` to exit the nearest enclosing function. Break/continue in nested function-inside-loop scenarios must be handled correctly.

7. Implement comprehensive error handling: runtime errors (type mismatch, undefined variable, division by zero, index out of bounds, wrong argument count) produce Error objects that propagate up the evaluation stack without panicking. Each Error includes a message and a stack trace showing the chain of function calls. Implement error checking at every Eval call: if any sub-expression evaluates to an Error, immediately return that Error (short-circuit error propagation). Implement a `try/catch`-like mechanism or a `try()` built-in function.

8. Write tests covering: arithmetic operations with all type combinations, string concatenation and comparison, boolean short-circuit evaluation, variable scoping (inner scope shadows outer, updates to outer scope work), recursive functions (factorial, fibonacci), mutual recursion, closures capturing variables from enclosing scopes, closures over loop variables (the classic closure-in-loop gotcha), array indexing and out-of-bounds errors, hash map operations, error propagation through nested function calls, break/continue in nested loops, and a comprehensive integration test evaluating a multi-function program that implements merge sort on an array.

## Hints

- The key insight of tree-walking evaluation is that `Eval` is a recursive interpreter: each node type knows how to evaluate itself, and compound nodes evaluate their children first.
- For error propagation, define `isError(obj Object) bool` and check it after every `Eval` call. This is verbose but reliable. An alternative is to use Go's `error` return, but it makes the evaluator less clean.
- ReturnValue unwrapping must happen at the right level: when evaluating a function body (BlockStatement), if you encounter a ReturnValue, return it as-is. The CallExpression handler unwraps it. This way, return propagates through nested blocks within a function but stops at the function boundary.
- Break/Continue work similarly to Return: they are signal objects that propagate up through block evaluation until they reach a loop handler.
- For closures, the critical detail is that the Function object captures the environment at definition time, not at call time. This is what makes closures work.
- Short-circuit evaluation for `&&` and `||`: evaluate the left side first. For `&&`, if left is falsy, return left without evaluating right. For `||`, if left is truthy, return left.

## Success Criteria

1. Basic arithmetic: `(5 + 10 * 2 + 15 / 3) * 2 + -10` evaluates to `50`.
2. Recursive factorial: `let fact = fn(n) { if (n < 2) { 1 } else { n * fact(n - 1) } }; fact(10)` evaluates to `3628800`.
3. Closures work: `let adder = fn(x) { fn(y) { x + y } }; let add5 = adder(5); add5(10)` evaluates to `15`.
4. Variable scoping: inner blocks shadow outer variables, and modifications to outer-scope variables via `Update` are visible in the outer scope after the block exits.
5. Error propagation: a type error deep in a nested function call chain produces an Error with a stack trace showing all intermediate calls.
6. Array operations: creating, indexing, and out-of-bounds handling all work correctly.
7. A merge sort implementation in the language correctly sorts a 100-element array.
8. Break and continue work correctly in nested while and for loops.

## Research Resources

- [Crafting Interpreters - Evaluating Expressions](https://craftinginterpreters.com/evaluating-expressions.html)
- [Crafting Interpreters - Statements and State](https://craftinginterpreters.com/statements-and-state.html)
- [Writing An Interpreter In Go - Evaluation Chapter (Thorsten Ball)](https://interpreterbook.com/)
- [Tree-Walking vs Bytecode Interpreters](https://tratt.net/laurie/blog/2017/a_comparison_of_language_implementation_strategies.html)
- [Closure (Computer Programming)](https://en.wikipedia.org/wiki/Closure_(computer_programming))
- [Short-Circuit Evaluation](https://en.wikipedia.org/wiki/Short-circuit_evaluation)
