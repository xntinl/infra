<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# AST Representation and Manipulation

The Abstract Syntax Tree is the central data structure of your interpreter -- every phase after parsing reads, transforms, or evaluates the AST. A well-designed AST makes the interpreter elegant; a poorly designed one makes every subsequent phase painful. Your task is to design and implement a comprehensive AST in Go using idiomatic interface-based polymorphism, with support for visitor-pattern traversal, source position tracking on every node, AST-to-source-code pretty printing, deep cloning, structural equality comparison, and an AST transformation framework that enables optimization passes. This exercise is about software design as much as implementation.

## Requirements

1. Define the core AST node interfaces: `Node` (base with `TokenLiteral() string` and `String() string` and `Pos() Position`), `Statement` (embeds Node, adds `statementNode()` marker), and `Expression` (embeds Node, adds `expressionNode()` marker). Every concrete AST node must implement the appropriate interface. Define `Position` as `{File string, Line int, Column int, EndLine int, EndColumn int}` so each node records its complete source span, not just its start.

2. Implement all expression node types: `IntegerLiteral`, `FloatLiteral`, `StringLiteral`, `BooleanLiteral`, `NullLiteral`, `Identifier`, `PrefixExpression` (operator + operand), `InfixExpression` (left + operator + right), `IfExpression` (condition + consequence + alternative), `FunctionLiteral` (parameters + body + optional name), `CallExpression` (function + arguments), `ArrayLiteral` (elements), `IndexExpression` (left + index), `HashLiteral` (key-value pairs), `AssignExpression` (target + value), `WhileExpression` (condition + body), `ForExpression` (init + condition + update + body), `DotExpression` (object + field), `RangeExpression` (start + end), and `TernaryExpression` (condition + consequence + alternative).

3. Implement all statement node types: `LetStatement` (name + value + optional type annotation), `ConstStatement` (name + value), `ReturnStatement` (value), `ExpressionStatement` (expression), `BlockStatement` (list of statements), `BreakStatement`, `ContinueStatement`, and `Program` (the root node, containing a list of statements). Each statement must store the token that initiated it for error reporting.

4. Implement the Visitor pattern: define a `Visitor` interface with a `Visit(node Node) Visitor` method (returning a Visitor allows controlling traversal). Implement `Walk(visitor Visitor, node Node)` that traverses the AST depth-first, calling `visitor.Visit()` on each node. Before visiting children, call `Visit` on the parent; if it returns nil, skip the children. This enables custom traversal logic. Implement convenience functions: `Inspect(node Node, fn func(Node) bool)` that calls `fn` for every node (return false to stop), and `Collect[T Node](root Node) []T` using generics to find all nodes of a specific type.

5. Implement a pretty printer that converts any AST back to source code with proper formatting. The printer must: indent nested blocks with configurable indentation (spaces or tabs), add line breaks between statements, format binary expressions with spaces around operators, parenthesize sub-expressions only when necessary for precedence clarity, and produce output that re-parses to a structurally identical AST. Implement both compact (minimal whitespace) and formatted (readable) modes.

6. Implement deep cloning: `Clone(node Node) Node` returns a completely independent copy of the AST subtree. Mutations to the clone must not affect the original. This is essential for AST transformations that should not modify the input. Implement structural equality: `Equal(a, b Node) bool` that recursively compares two AST nodes for structural equivalence (same node types, same values, same children) ignoring source positions.

7. Implement an AST transformation framework: `Transform(node Node, fn func(Node) Node) Node` that traverses the AST bottom-up, calling `fn` on each node and replacing it with the returned node. If `fn` returns the same node, no replacement occurs. Use this framework to implement at least two optimization passes: constant folding (evaluate `3 + 4` at compile time to `7`, `"hello" + " world"` to `"hello world"`, `true && false` to `false`) and dead code elimination (remove `if (false) { ... }` blocks, remove code after `return` in a block).

8. Write tests covering: every AST node type has correct `String()` output, the Visitor/Walk correctly visits all nodes in a complex AST in the expected order, `Inspect` and `Collect` find the right nodes, the pretty printer produces re-parseable output for 20+ test programs, `Clone` produces independent copies (mutate clone, verify original unchanged), `Equal` correctly identifies identical and different ASTs, constant folding transforms `let x = 2 + 3 * 4;` into `let x = 14;`, and dead code elimination removes unreachable branches.

## Hints

- Go's interface-based polymorphism is the natural fit for AST nodes. Marker methods (`statementNode()`, `expressionNode()`) are Go's way of creating closed interface hierarchies without sum types.
- The Visitor pattern in Go differs from Java's because Go lacks method overloading. The `Visit(Node) Visitor` pattern (used by `go/ast`) is the idiomatic approach: return `nil` to prune, return `self` to continue, return a new visitor to change state.
- For the pretty printer, track the current indentation level as state. Each `BlockStatement` increments it, and each closing brace decrements it. Use `strings.Builder` for efficient string construction.
- Constant folding must be careful with integer overflow, division by zero, and floating-point precision. Only fold operations that cannot fail at runtime.
- For `Transform`, process children before the parent (bottom-up) so that the transformation function sees already-transformed children. This enables multi-level folding (e.g., `(1 + 2) + (3 + 4)` -> `3 + 7` -> `10`).
- `reflect.DeepEqual` is tempting for `Equal` but won't work because you need to ignore source positions. Write a manual recursive comparator.

## Success Criteria

1. Every AST node type implements its interface correctly and produces valid `String()` output.
2. The Visitor pattern visits every node in the tree exactly once, in depth-first pre-order.
3. `Inspect` correctly counts all nodes and `Collect` correctly filters by type.
4. The pretty printer produces source code that re-parses to a structurally equivalent AST (verified with `Equal`).
5. `Clone` produces a fully independent copy: modifying a cloned identifier's name does not affect the original.
6. Constant folding reduces `let x = (2 + 3) * (10 - 4) / 2;` to `let x = 15;` in a single pass.
7. Dead code elimination removes the else branch from `if (true) { a } else { b }` and code after return statements.
8. All AST operations handle edge cases: empty blocks, deeply nested expressions, and zero-argument function literals.

## Research Resources

- [Go AST Package (standard library reference)](https://pkg.go.dev/go/ast)
- [Writing An Interpreter In Go - AST Chapter (Thorsten Ball)](https://interpreterbook.com/)
- [Crafting Interpreters - Representing Code](https://craftinginterpreters.com/representing-code.html)
- [The Visitor Pattern in Go](https://eli.thegreenplace.net/2019/to-oop-or-not-to-oop/)
- [Constant Folding Optimization](https://en.wikipedia.org/wiki/Constant_folding)
- [Go Generics for AST Operations](https://go.dev/blog/intro-generics)
