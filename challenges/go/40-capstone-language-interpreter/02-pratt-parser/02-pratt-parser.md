<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# Pratt Parser for Expression Parsing

Pratt parsing (also called top-down operator precedence parsing) is an elegant technique that maps directly to how we think about expressions: each token type knows how to parse itself as either a prefix (beginning of an expression) or an infix (middle of an expression), with precedence levels controlling how tightly operators bind. Your task is to implement a complete Pratt parser in Go that handles the Monkey language's full expression syntax including arithmetic, comparisons, boolean logic, function calls, array indexing, hash literals, prefix and postfix operators, and grouped expressions. This parser will consume the tokens from your lexer and produce an Abstract Syntax Tree.

## Requirements

1. Implement the core Pratt parser framework: define a `Parser` struct holding the lexer, current and peek tokens, a map of prefix parse functions (`prefixParseFns map[TokenType]prefixParseFn`), and a map of infix parse functions (`infixParseFns map[TokenType]infixParseFn`). A `prefixParseFn` is `func() ast.Expression` (no left-side argument). An `infixParseFn` is `func(left ast.Expression) ast.Expression` (takes the left operand). Register parse functions for each token type during parser initialization.

2. Define precedence levels as an ordered enum: `LOWEST < ASSIGN < OR < AND < EQUALS < LESSGREATER < SUM < PRODUCT < POWER < PREFIX < CALL < INDEX`. Implement `peekPrecedence() int` and `curPrecedence() int` that look up the precedence for the current/peek token. Implement the core `parseExpression(precedence int) ast.Expression` function that: parses the prefix (left side), then loops while the peek token's precedence is greater than the current precedence, consuming infix operators and building binary expression nodes.

3. Implement prefix parse functions for: integer literals, float literals, string literals, boolean literals (true/false), null literal, identifiers, prefix operators (!, -, ~), grouped expressions `(expr)`, if-else expressions `if (cond) { block } else { block }`, function literals `fn(params) { body }`, array literals `[elem, elem, ...]`, hash literals `{key: value, key: value}`, and while expressions `while (cond) { body }`. Each prefix parse function returns the appropriate AST node.

4. Implement infix parse functions for: arithmetic operators (+, -, *, /, %, **), comparison operators (==, !=, <, >, <=, >=), logical operators (&&, ||), assignment operators (=, +=, -=, *=, /=), call expressions `expr(args)`, index expressions `expr[index]`, dot access `expr.field`, range expressions `expr..expr`, and the ternary conditional `expr ? expr : expr`. The call expression uses the left side as the function and parses the argument list. Index expressions parse the bracketed subscript.

5. Implement right-associativity for appropriate operators: assignment (`=`, `+=`, etc.) and exponentiation (`**`) are right-associative, meaning `a = b = c` parses as `a = (b = c)` and `2 ** 3 ** 2` parses as `2 ** (3 ** 2)`. Implement this by reducing the binding power by 1 when recursing on the right side for right-associative operators.

6. Implement comprehensive error handling: when the parser encounters an unexpected token, record the error with source position, expected token description, and actual token found. Collect all errors in a `[]ParseError` slice rather than stopping at the first error. Implement synchronization points: after an error, skip tokens until a statement boundary (semicolon, `}`, or keyword) to attempt recovering and finding more errors.

7. Implement statement parsing alongside expression parsing: `LetStatement` (`let x = expr;`), `ConstStatement` (`const x = expr;`), `ReturnStatement` (`return expr;`), `ExpressionStatement` (bare expression followed by semicolon or newline), `BlockStatement` (`{ stmt; stmt; ... }`), `ForStatement` (`for (init; cond; update) { body }`), `BreakStatement`, and `ContinueStatement`. The parser's top-level `ParseProgram()` method returns a `Program` node containing a list of statements.

8. Write tests covering: every precedence level in isolation (verify AST structure for `a + b * c`, `a * b + c`, etc.), right-associativity of assignment and power, all prefix operators, all infix operators, call expressions with varying argument counts (0, 1, many), nested function calls `f(g(x))`, index expressions including chained `a[0][1]`, hash literal parsing, if-else with nested ifs, function literals as arguments, error recovery (multiple errors reported for `let = 5; let x 5; let y = ;`), and an AST pretty-printer test that produces readable tree output for complex expressions.

## Hints

- The magic of Pratt parsing is in `parseExpression(precedence)`: parse the prefix, then keep consuming infix operators as long as their precedence exceeds the minimum. The precedence parameter acts as a "right binding power" that controls how much of the expression this call will consume.
- Right-associative operators call `parseExpression(currentPrecedence - 1)` for their right operand, while left-associative operators call `parseExpression(currentPrecedence)`. This subtle difference controls associativity.
- For call expressions, register an infix parse function on the `LPAREN` token type. The "left" argument is the function expression, and the infix function parses the argument list.
- Similarly, for index expressions, register an infix parse function on `LBRACKET`.
- The ternary operator `?:` is tricky: register `?` as an infix operator, parse the "then" expression at LOWEST precedence (allowing any expression), expect `:`, then parse the "else" expression.
- Error recovery's goal is reporting multiple useful errors per parse, not perfect recovery. Skipping to the next semicolon or closing brace is a pragmatic approach.

## Success Criteria

1. Operator precedence is correct for all levels: `1 + 2 * 3` produces an AST equivalent to `1 + (2 * 3)`, and `1 * 2 + 3` produces `(1 * 2) + 3`.
2. Right-associative operators bind correctly: `a = b = c` parses as `a = (b = c)`.
3. All prefix expressions (!, -, ~, if-else, fn, array, hash) produce correct AST nodes.
4. Call expressions, index expressions, and dot access chain correctly: `obj.method(args)[0]` parses as `((obj.method)(args))[0]`.
5. Error recovery reports at least 3 errors for a source with 3 distinct syntax mistakes, with accurate line/column numbers.
6. The parser handles deeply nested expressions (100+ levels) without stack overflow.
7. All statement types parse correctly with their associated expressions.
8. A complex multi-line program with functions, control flow, and data structures parses into a correct, complete AST.

## Research Resources

- [Pratt Parsers: Expression Parsing Made Easy (Bob Nystrom)](https://journal.stuffwithstuff.com/2011/03/19/pratt-parsers-expression-parsing-made-easy/)
- [Simple but Powerful Pratt Parsing (matklad)](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html)
- [Writing An Interpreter In Go - Parsing Chapter (Thorsten Ball)](https://interpreterbook.com/)
- [Crafting Interpreters - Parsing Expressions](https://craftinginterpreters.com/parsing-expressions.html)
- [Top Down Operator Precedence (Vaughan Pratt, 1973)](https://tdop.github.io/)
- [From Precedence Climbing to Pratt Parsing](https://www.engr.mun.ca/~theo/Misc/pratt_parsing.htm)
