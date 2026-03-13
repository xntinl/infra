<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 2h
-->

# Lexer and Tokenizer for a Programming Language

Every programming language implementation begins with a lexer that transforms raw source code into a stream of tokens. Unlike the SQL lexer you may have built for the database engine capstone, this lexer handles a general-purpose programming language with different syntactic conventions: C-style operators, Unicode identifiers, string interpolation, and diverse literal types. Your task is to build a production-quality lexer in Go for the Monkey programming language (and extensions) that serves as the foundation for the interpreter you will construct across this capstone section. The lexer must be fast, correct, and produce tokens with rich source location information for error reporting.

## Requirements

1. Define a comprehensive `TokenType` enum using `iota` covering: keywords (let, fn, return, if, else, while, for, true, false, null, import, const, break, continue), identifiers, integer literals, float literals, string literals (double-quoted), character literals (single-quoted), operators (=, ==, !=, <, >, <=, >=, +, -, *, /, %, !, &&, ||, &, |, ^, ~, <<, >>), delimiters (( ) { } [ ] , ; : . .. ...), assignment operators (+=, -=, *=, /=, %=), arrow (=>), and special tokens (ILLEGAL, EOF, NEWLINE). Each token type must have a human-readable string representation via a `String()` method.

2. Define a `Token` struct with fields: `Type TokenType`, `Literal string`, `Line int`, `Column int`, `Offset int` (byte position), and `Length int`. Implement a `Position` struct with `File string`, `Line int`, `Column int` for error messages. Define a `TokenStream` type as a slice of tokens with utility methods: `Filter(keep func(Token) bool) TokenStream`, `String() string`.

3. Implement the `Lexer` struct as a single-pass scanner with two-character lookahead. The lexer must correctly handle: multi-character operators by checking the next character (e.g., `=` vs `==`, `!` vs `!=`, `<` vs `<=` vs `<<`, `&` vs `&&`), two-character tokens like `..` (range) and `=>` (arrow), and correctly distinguish between the subtraction operator `-` and negative number prefixes based on context.

4. Handle string literals with escape sequences: `\"` (escaped quote), `\\` (backslash), `\n` (newline), `\t` (tab), `\r` (carriage return), `\0` (null byte), `\x41` (hex byte), and `\u0041` (Unicode code point). Detect and report unterminated strings with the line where the string started. Support raw strings prefixed with backticks where no escaping occurs and newlines are preserved.

5. Handle numeric literals in multiple bases: decimal (`42`), hexadecimal (`0xFF`), octal (`0o77`), binary (`0b1010`), and with underscore separators for readability (`1_000_000`, `0xFF_FF`). Floating-point numbers support decimal notation (`3.14`), scientific notation (`1.5e10`, `2.3E-4`), and must correctly distinguish the `.` in `3.14` from the `.` in method calls or range operators.

6. Support Unicode identifiers: identifiers can start with any Unicode letter or underscore and continue with Unicode letters, digits, or underscores. Use `unicode.IsLetter()` and `unicode.IsDigit()` for classification. The lexer must correctly handle multi-byte UTF-8 characters by working with `rune` instead of `byte` for character classification, while tracking byte offsets for source positions.

7. Implement a `Tokenize(source string) ([]Token, []LexError)` function that produces all tokens at once and collects all lexical errors (rather than stopping at the first error). Each `LexError` contains a message, position, and the offending character(s). Implement also an iterator-based `NextToken() Token` for streaming use. Both interfaces must produce identical token sequences.

8. Write tests covering: every token type individually, multi-character operator disambiguation, string escape sequences (including invalid escapes), numeric literals in all bases with underscores, Unicode identifiers (e.g., variable names in Greek, Chinese, emoji as error case), error recovery (multiple errors in one source), position tracking across multi-line inputs with tabs and Unicode characters, round-trip verification (all token literals concatenated equal the original source), and a benchmark tokenizing a 100,000-line generated source file targeting 200+ MB/s throughput.

## Hints

- For two-character lookahead, maintain `ch` (current rune) and use `peekChar()` to look one ahead. For three-character sequences like `<<=`, peek twice or use a `peekN(n int) rune` helper.
- When scanning numbers, the prefix `0x`, `0o`, `0b` determines the base. After the prefix, consume all valid digits for that base (hex: 0-9, a-f, A-F; octal: 0-7; binary: 0, 1). Underscores are allowed between digits but not at the start or end.
- For UTF-8 handling, use `utf8.DecodeRuneInString(input[pos:])` to get the next rune and its byte width. Advance `pos` by the byte width, not by 1.
- The distinction between `-` as subtraction vs. unary negation is a parsing concern, not a lexing concern. Always emit `-` as a MINUS token and let the parser decide.
- For error recovery, when the lexer encounters an invalid character, emit an ILLEGAL token for that character and continue scanning from the next character. This allows collecting multiple errors in one pass.
- Pre-compute the keyword map at init time: `var keywords = map[string]TokenType{"let": LET, "fn": FN, ...}`.

## Success Criteria

1. Every defined token type is correctly produced for its corresponding input syntax.
2. Multi-character operators are never mis-tokenized: `<=` is always LE, never LT followed by ASSIGN.
3. String escape sequences produce correct literal values, and invalid escapes produce error tokens with descriptive messages.
4. Numeric literals in all bases (decimal, hex, octal, binary) with underscore separators are correctly parsed.
5. Unicode identifiers are correctly recognized, and byte/column tracking remains accurate for multi-byte characters.
6. The batch `Tokenize()` and streaming `NextToken()` interfaces produce identical token sequences for any input.
7. Benchmark achieves at least 200 MB/s throughput on a large generated source file.

## Research Resources

- [Crafting Interpreters - Scanning (Bob Nystrom)](https://craftinginterpreters.com/scanning.html)
- [Writing An Interpreter In Go - Lexer Chapter (Thorsten Ball)](https://interpreterbook.com/)
- [Rob Pike - Lexical Scanning in Go (talk)](https://www.youtube.com/watch?v=HxaD_trXwRE)
- [Go Unicode Package](https://pkg.go.dev/unicode)
- [UTF-8 Encoding in Go](https://pkg.go.dev/unicode/utf8)
- [Monkey Programming Language Specification](https://monkeylang.org/)
