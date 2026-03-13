<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# SQL Lexer and Tokenizer

Before a database can understand a SQL query, it must break the raw string of characters into meaningful tokens -- keywords, identifiers, literals, operators, and punctuation. This process, called lexical analysis or tokenization, is the critical first stage of the query processing pipeline. Your task is to build a hand-written SQL lexer in Go (no parser generators, no regex) that handles the full range of SQL token types, tracks source positions for error reporting, handles edge cases like quoted identifiers and string escaping, and performs efficiently enough to tokenize millions of characters per second. This lexer will feed directly into the SQL parser you build in the next exercise.

## Requirements

1. Define a `TokenType` enum (using `iota`) covering at least: keywords (SELECT, FROM, WHERE, INSERT, INTO, VALUES, UPDATE, SET, DELETE, CREATE, TABLE, DROP, INDEX, ON, AND, OR, NOT, NULL, TRUE, FALSE, ORDER, BY, ASC, DESC, LIMIT, OFFSET, JOIN, LEFT, RIGHT, INNER, OUTER, GROUP, HAVING, AS, DISTINCT, COUNT, SUM, AVG, MIN, MAX, IN, BETWEEN, LIKE, IS, EXISTS, PRIMARY, KEY, INTEGER, TEXT, REAL, BOOLEAN, BEGIN, COMMIT, ROLLBACK), identifiers, integer literals, float literals, string literals, operators (+, -, *, /, =, !=, <>, <, >, <=, >=), punctuation (parentheses, comma, semicolon, dot, asterisk), whitespace, comments (-- line comments and /* block comments */), and EOF.

2. Define a `Token` struct containing: `Type TokenType`, `Literal string` (the raw text), `Line int`, `Column int`, and `Position int` (byte offset in the source). Implement `String()` on `Token` for debug-friendly output (e.g., `Token(SELECT, "SELECT", 1:1)`).

3. Implement a `Lexer` struct that holds the source string, current position, current line/column, and peek character. Implement `NextToken() Token` that advances through the source and returns the next token. The lexer must operate as a single-pass scanner with O(1) lookahead (peek one character ahead without consuming it).

4. Handle SQL string literals with single quotes, including escaped single quotes via doubling (`'it''s'` -> `it's`). Handle quoted identifiers with double quotes (`"column name"` -> `column name`). Handle numeric literals including integers, floats with decimal points, and scientific notation (`1.5e10`). Handle negative numbers as a unary minus operator followed by a numeric literal (not as a single negative number token).

5. Implement keyword recognition by first lexing any alphabetic sequence as an identifier, then checking it against a case-insensitive keyword map. SQL keywords are case-insensitive (`select` == `SELECT` == `Select`), but identifiers preserve their original case. Store the canonical uppercase form for keywords in the token's `Literal` field.

6. Handle both line comments (`-- comment until end of line`) and block comments (`/* comment that can span multiple lines */`), including nested block comments (`/* outer /* inner */ still comment */`). Comments should be skipped by default but optionally preserved (configurable via a `KeepComments bool` option) for tooling use cases.

7. Implement comprehensive error handling: unterminated string literals, unterminated block comments, and invalid characters must produce error tokens with descriptive messages including line and column numbers. The lexer must not panic on any input -- it must always produce either valid tokens or error tokens until EOF.

8. Write tests covering: tokenization of every keyword, every operator, every punctuation mark, string literals with escapes, quoted identifiers, numeric literals (integer, float, scientific notation), multi-line input with correct line/column tracking, comment handling (line, block, nested block), error cases (unterminated string, unterminated comment, invalid characters), round-trip verification (concatenating all token literals reproduces the original source), and a benchmark tokenizing a 1 MB SQL file generated programmatically.

## Hints

- A classic lexer pattern in Go: store `input string`, `pos int`, `readPos int` (one ahead), and `ch byte`. `readChar()` advances `pos` to `readPos` and reads the next byte. `peekChar()` returns `input[readPos]` without advancing.
- For keyword lookup, build a `map[string]TokenType` at init time with all keywords in uppercase. After lexing an identifier, do `strings.ToUpper(literal)` and check the map.
- For string escaping, when you encounter a single quote inside a string, peek ahead: if the next character is also a single quote, consume both and emit one quote in the literal; otherwise, end the string.
- Track `line` and `col` by incrementing `col` on every `readChar()` and resetting `col = 1` and incrementing `line` on newline characters.
- Nested block comments require a depth counter: increment on `/*`, decrement on `*/`, and only end the comment when depth reaches 0.
- For the benchmark, generate SQL like `SELECT col1, col2 FROM table1 WHERE id = 1;` repeated thousands of times.

## Success Criteria

1. Every SQL keyword is recognized case-insensitively and produces the correct `TokenType`.
2. String literals with embedded escaped quotes are correctly unescaped in the token literal.
3. Numeric literals including scientific notation produce correct integer or float token types.
4. Line and column numbers are accurate for every token in a multi-line, multi-statement SQL input.
5. Nested block comments are handled correctly to arbitrary depth.
6. Error tokens are produced (not panics) for every malformed input case, with descriptive messages.
7. Benchmark achieves at least 100 MB/s throughput on a generated SQL corpus.

## Research Resources

- [Crafting Interpreters - Scanning (Bob Nystrom)](https://craftinginterpreters.com/scanning.html)
- [Writing a Lexer in Go (Thorsten Ball)](https://interpreterbook.com/)
- [SQL Language Reference - Lexical Structure (PostgreSQL)](https://www.postgresql.org/docs/current/sql-syntax-lexical.html)
- [Go strings, bytes, runes and characters](https://go.dev/blog/strings)
- [Lexical Analysis (Wikipedia)](https://en.wikipedia.org/wiki/Lexical_analysis)
