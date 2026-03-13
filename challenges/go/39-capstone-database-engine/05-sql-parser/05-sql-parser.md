<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 3h
-->

# SQL Parser

With tokens in hand from the lexer, the parser's job is to impose grammatical structure -- transforming a flat sequence of tokens into a tree that captures the meaning of the SQL statement. Your task is to build a recursive descent SQL parser in Go that consumes the token stream from your lexer and produces a typed Abstract Syntax Tree (AST). You will support SELECT queries with joins, WHERE clauses with complex expressions, INSERT/UPDATE/DELETE DML, and CREATE TABLE/DROP TABLE DDL. The parser must produce clear error messages with source locations when it encounters invalid SQL, and the AST must be designed for easy consumption by the query planner.

## Requirements

1. Define AST node types as Go interfaces and structs. Create a `Statement` interface with implementations: `SelectStatement`, `InsertStatement`, `UpdateStatement`, `DeleteStatement`, `CreateTableStatement`, `DropTableStatement`, `BeginStatement`, `CommitStatement`, `RollbackStatement`. Create an `Expression` interface with implementations: `IdentifierExpr`, `LiteralExpr` (integer, float, string, boolean, null), `BinaryExpr` (with operator), `UnaryExpr` (NOT, unary minus), `FunctionCallExpr`, `SubqueryExpr`, `BetweenExpr`, `InExpr`, `IsNullExpr`, `LikeExpr`, `ColumnRef` (with optional table alias prefix).

2. Implement the `Parser` struct that wraps the lexer, maintains a current token and peek token, and provides helper methods: `expectToken(t TokenType) error` (consume and verify), `peekTokenIs(t TokenType) bool`, `curTokenIs(t TokenType) bool`, and `nextToken()`. Error messages must include the line, column, expected token type, and actual token found.

3. Implement `ParseSelect()` that parses: `SELECT [DISTINCT] select_list FROM table_references [WHERE condition] [GROUP BY expr_list [HAVING condition]] [ORDER BY order_list] [LIMIT count [OFFSET skip]]`. The select list supports expressions with optional `AS alias`, including `*` and `table.*`. Table references support `table [AS alias]` and JOIN clauses (`[LEFT|RIGHT|INNER] JOIN table ON condition`).

4. Implement expression parsing using the Pratt parsing technique (operator precedence climbing). Define precedence levels: lowest < OR < AND < NOT < comparison (=, !=, <, >, <=, >=, IS, IN, BETWEEN, LIKE) < addition (+, -) < multiplication (*, /) < unary (-, NOT) < function call. Parse parenthesized subexpressions and subqueries (`(SELECT ...)`).

5. Implement `ParseInsert()` for `INSERT INTO table (columns) VALUES (values), (values)...` with support for multiple value rows. Implement `ParseUpdate()` for `UPDATE table SET col = expr, col = expr [WHERE condition]`. Implement `ParseDelete()` for `DELETE FROM table [WHERE condition]`.

6. Implement `ParseCreateTable()` for `CREATE TABLE [IF NOT EXISTS] name (column_def, column_def, ..., [PRIMARY KEY (columns)])` where each column definition is `name type [NOT NULL] [PRIMARY KEY] [DEFAULT expr]` and type is one of INTEGER, TEXT, REAL, BOOLEAN. Implement `ParseDropTable()` for `DROP TABLE [IF EXISTS] name`.

7. Implement an AST printer that produces a formatted, indented string representation of any AST node (for debugging and testing). Also implement a `String()` method on each AST node that regenerates valid SQL from the AST (round-trip capability). The regenerated SQL need not match the original character-for-character but must be semantically equivalent and re-parseable to an identical AST.

8. Write tests covering: parsing every statement type with all optional clauses, operator precedence verification (e.g., `a + b * c` parses as `a + (b * c)`), parenthesized expressions overriding precedence, JOIN clause parsing with ON conditions, error messages for missing commas, missing FROM, unclosed parentheses, multi-statement parsing (semicolon separated), and round-trip tests (parse SQL -> generate SQL -> re-parse -> compare ASTs). Include at least 30 distinct SQL statements covering all supported syntax.

## Hints

- Recursive descent is the most natural fit for SQL parsing in Go. Each grammar rule becomes a function: `parseStatement()`, `parseSelectStatement()`, `parseExpression()`, etc.
- Pratt parsing works by associating a "binding power" (precedence) with each operator. `parseExpression(minBP int)` parses the left side, then loops consuming operators whose binding power exceeds `minBP`, recursively parsing the right side with the operator's binding power.
- For JOIN parsing, after the FROM table, loop checking for JOIN keywords. Each JOIN has a type (LEFT/RIGHT/INNER), a table reference, and an ON condition.
- AST comparison in tests is easiest with `reflect.DeepEqual` or by comparing the `String()` output of both ASTs.
- For error recovery, consider a `synchronize()` method that skips tokens until it finds a statement boundary (semicolon or keyword), allowing the parser to report multiple errors in one pass.
- The distinction between `ColumnRef` (table.column) and `IdentifierExpr` (bare name) is resolved during parsing by checking for a dot after an identifier.

## Success Criteria

1. All supported SQL statement types parse correctly into the expected AST structure.
2. Operator precedence is correct for all arithmetic and logical operators, verified by AST inspection of ambiguous expressions.
3. JOIN clauses (LEFT, RIGHT, INNER, implicit cross join) parse correctly with their ON conditions.
4. Error messages for syntax errors include accurate line/column numbers and describe the expected vs. actual token.
5. Round-trip tests pass: for every test SQL statement, `parse(sql).String()` produces SQL that `parse()` converts to an identical AST.
6. Multi-statement inputs separated by semicolons parse into a slice of statements, each independently correct.
7. Complex nested expressions including subqueries, function calls, and BETWEEN/IN/LIKE parse correctly.

## Research Resources

- [Crafting Interpreters - Parsing Expressions (Pratt Parsing)](https://craftinginterpreters.com/parsing-expressions.html)
- [Pratt Parsers: Expression Parsing Made Easy](https://journal.stuffwithstuff.com/2011/03/19/pratt-parsers-expression-parsing-made-easy/)
- [Writing a SQL Parser (CockroachDB Engineering Blog)](https://www.cockroachlabs.com/blog/)
- [SQLite SQL Syntax Diagrams](https://www.sqlite.org/syntaxdiagrams.html)
- [Go AST Package Design (for inspiration)](https://pkg.go.dev/go/ast)
- [Simple but Complete Pratt Parser in Go](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html)
