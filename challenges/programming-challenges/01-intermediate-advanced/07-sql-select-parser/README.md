# 7. SQL SELECT Statement Parser

<!--
difficulty: intermediate-advanced
category: parsers-and-compilers
languages: [go, rust]
concepts: [lexer, ast, recursive-descent, sql, pretty-printing, subqueries]
estimated_time: 8-10 hours
bloom_level: analyze
prerequisites: [go-basics, rust-basics, enums, pattern-matching, string-processing, tree-data-structures]
-->

## Languages

- Go (1.22+)
- Rust (stable)

## Prerequisites

- Recursive descent parsing concepts (or willingness to learn)
- Understanding of SQL SELECT statement semantics at a user level
- Go: interfaces, structs, slices, `fmt.Stringer`
- Rust: enums with data, `Box<T>` for recursive types, `Display` trait

## Learning Objectives

- **Implement** a tokenizer that handles SQL keywords, identifiers, string literals, and operators
- **Design** a typed AST that captures the full structure of a SELECT statement with all clauses
- **Apply** recursive descent with precedence to parse WHERE clause boolean expressions
- **Analyze** how operator precedence (AND binds tighter than OR) maps to AST nesting
- **Evaluate** how a well-typed AST enables reliable transformation by implementing a SQL pretty-printer

## The Challenge

SQL is the most widely used query language, and every database, ORM, and migration tool must parse it at some level. SELECT statements are the most complex single statement in SQL: they combine column expressions, table references with aliases, boolean predicates with operator precedence, joins, grouping, ordering, and subqueries.

Your task is to build a parser that accepts a subset of SQL SELECT statements and produces a well-typed AST. The AST must capture enough structure to regenerate syntactically equivalent SQL through a pretty-printer. Both Go and Rust implementations are required, and comparing the two designs is part of the exercise.

You are not building a query optimizer or executor -- just the parser and AST. But the AST must be precise enough that someone could build either on top of it.

## Requirements

1. Tokenize SQL input into keywords (`SELECT`, `FROM`, `WHERE`, `JOIN`, `ON`, `AND`, `OR`, `NOT`, `ORDER`, `BY`, `GROUP`, `HAVING`, `LIMIT`, `OFFSET`, `AS`, `INNER`, `LEFT`, `RIGHT`, `NULL`, `IN`, `BETWEEN`, `LIKE`, `IS`), identifiers, string literals (single-quoted), numeric literals, operators (`=`, `!=`, `<>`, `<`, `>`, `<=`, `>=`), and punctuation (`(`, `)`, `,`, `.`, `*`)
2. Parse `SELECT` with column expressions: `*`, `table.*`, column names, column aliases (`AS alias` or implicit), and simple expressions (arithmetic on columns/literals)
3. Parse `FROM` clause with table names and optional aliases
4. Parse `JOIN` clauses: `INNER JOIN`, `LEFT JOIN`, `RIGHT JOIN` with `ON` conditions
5. Parse `WHERE` clause with comparison operators, `AND`, `OR`, `NOT`, parenthesized groups, `IS NULL`, `IS NOT NULL`, `IN (...)`, `BETWEEN ... AND ...`, `LIKE`
6. Parse `ORDER BY` with column references and `ASC`/`DESC`
7. Parse `GROUP BY` with column references and optional `HAVING` clause
8. Parse `LIMIT` and `OFFSET` with numeric values
9. Support subqueries in `WHERE` clauses (e.g., `WHERE id IN (SELECT id FROM other)`)
10. Implement a pretty-printer that converts the AST back to formatted SQL
11. Both Go and Rust implementations must handle the same input and produce equivalent ASTs

## Hints

<details>
<summary>Hint 1: Token type with keyword recognition</summary>

Lex identifiers first, then check if the identifier matches a keyword:

```rust
fn classify_word(word: &str) -> TokenKind {
    match word.to_uppercase().as_str() {
        "SELECT" => TokenKind::Select,
        "FROM" => TokenKind::From,
        "WHERE" => TokenKind::Where,
        // ... other keywords
        _ => TokenKind::Identifier(word.to_string()),
    }
}
```

```go
func classifyWord(word string) TokenKind {
    switch strings.ToUpper(word) {
    case "SELECT": return KwSelect
    case "FROM":   return KwFrom
    case "WHERE":  return KwWhere
    // ... other keywords
    default: return Identifier
    }
}
```
</details>

<details>
<summary>Hint 2: AST structure for SELECT</summary>

The top-level AST node captures all optional clauses:

```rust
struct SelectStmt {
    columns: Vec<SelectColumn>,
    from: Option<FromClause>,
    joins: Vec<JoinClause>,
    where_clause: Option<Expr>,
    group_by: Vec<Expr>,
    having: Option<Expr>,
    order_by: Vec<OrderByItem>,
    limit: Option<u64>,
    offset: Option<u64>,
}
```

Where `Expr` is a recursive enum for expressions and conditions.
</details>

<details>
<summary>Hint 3: Parsing boolean expressions with precedence</summary>

Use precedence climbing for WHERE expressions. OR has lower precedence than AND, which has lower precedence than NOT:

```rust
fn parse_expr(&mut self, min_prec: u8) -> Result<Expr, ParseError> {
    let mut left = self.parse_primary()?;
    while let Some(op) = self.peek_binary_op() {
        let prec = op.precedence();
        if prec < min_prec { break; }
        self.advance();
        let right = self.parse_expr(prec + 1)?;
        left = Expr::BinaryOp { left: Box::new(left), op, right: Box::new(right) };
    }
    Ok(left)
}
```
</details>

<details>
<summary>Hint 4: Subquery detection</summary>

When parsing a value in a WHERE clause and you encounter `(`, peek ahead: if the next token is `SELECT`, parse a full sub-SELECT. Otherwise, parse a parenthesized expression or value list:

```go
func (p *Parser) parsePrimaryExpr() (Expr, error) {
    if p.peek() == TokenLParen {
        p.advance()
        if p.peek() == KwSelect {
            subquery := p.parseSelect()
            p.expect(TokenRParen)
            return &SubqueryExpr{Query: subquery}, nil
        }
        expr := p.parseExpr(0)
        p.expect(TokenRParen)
        return expr, nil
    }
    // ...
}
```
</details>

## Acceptance Criteria

- [ ] Tokenizer handles all required SQL keywords, operators, identifiers, and literals
- [ ] Parser produces correct AST for: `SELECT a, b FROM t WHERE x = 1`
- [ ] Column aliases work: `SELECT a AS alias1, b alias2 FROM t`
- [ ] JOINs parse correctly: `SELECT * FROM a INNER JOIN b ON a.id = b.a_id`
- [ ] WHERE clause respects precedence: `a = 1 AND b = 2 OR c = 3` groups AND before OR
- [ ] Subqueries parse: `SELECT * FROM t WHERE id IN (SELECT id FROM other)`
- [ ] GROUP BY with HAVING: `SELECT dept, COUNT(*) FROM emp GROUP BY dept HAVING COUNT(*) > 5`
- [ ] ORDER BY, LIMIT, OFFSET all parse and appear in AST
- [ ] Pretty-printer reproduces syntactically equivalent SQL from the AST
- [ ] Both Go and Rust solutions handle the same test inputs
- [ ] All tests pass (`go test ./...` and `cargo test`)

## Research Resources

- [Crafting Interpreters: Parsing Expressions](https://craftinginterpreters.com/parsing-expressions.html) -- recursive descent and precedence climbing
- [SQLite SELECT Syntax Diagrams](https://www.sqlite.org/lang_select.html) -- railroad diagrams showing the full grammar
- [PostgreSQL SQL Syntax: SELECT](https://www.postgresql.org/docs/current/sql-select.html) -- production grammar reference
- [Simple but Powerful Pratt Parsing](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) -- matklad's approach to precedence parsing
- [sqlparser-rs](https://github.com/sqlparser-rs/sqlparser-rs) -- production Rust SQL parser for comparison
- [go-sqlparser (xwb1989/sqlparser)](https://github.com/xwb1989/sqlparser) -- Go SQL parser reference
