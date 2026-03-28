# 6. JSON Parser from Scratch

<!--
difficulty: intermediate-advanced
category: parsers-and-compilers
languages: [rust]
concepts: [lexer, tokenizer, recursive-descent, json, error-reporting, streaming]
estimated_time: 6-8 hours
bloom_level: analyze
prerequisites: [rust-basics, enums, pattern-matching, iterators, error-handling, traits]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust enums with data variants and exhaustive pattern matching
- The `Result` type and `?` operator for error propagation
- Iterators and `Peekable` for lookahead
- Trait definitions and implementations
- Basic understanding of JSON structure (RFC 8259)

## Learning Objectives

- **Implement** a two-phase parser architecture separating tokenization from tree construction
- **Apply** recursive descent parsing to handle nested data structures of arbitrary depth
- **Analyze** how lookahead drives parsing decisions at each grammar production
- **Design** error messages that report the exact position (line:column) of invalid input
- **Evaluate** trade-offs between DOM-style parsing (full tree in memory) and streaming/event-based parsing

## The Challenge

JSON appears simple until you try to parse it correctly. The specification (RFC 8259) has subtle rules: strings allow Unicode escape sequences (`\uXXXX`) including surrogate pairs, numbers cannot have leading zeros, and the grammar is recursive (arrays contain values, values contain arrays). Most "simple" JSON parsers fail on at least one of these edge cases.

Your task is to build a complete JSON parser in Rust following RFC 8259. The parser operates in two phases: a **lexer** that converts raw input into a stream of tokens, and a **parser** that consumes tokens and builds a typed value tree. When the input is invalid, the parser must report what went wrong and where, with line and column numbers.

After the DOM parser works, implement a **streaming variant** that emits events (start object, key, value, end object, etc.) without building the full tree in memory. This is how real parsers handle multi-gigabyte files.

## Requirements

1. Define a `Token` enum covering all JSON tokens: left/right brace, left/right bracket, colon, comma, string, number, `true`, `false`, `null`
2. Implement a `Lexer` that converts a `&str` into a `Vec<Token>`, tracking line and column for each token
3. The lexer must handle all string escape sequences: `\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`, and `\uXXXX` (including surrogate pairs for characters outside the BMP)
4. The lexer must parse JSON numbers per spec: optional minus, integer part (no leading zeros except `0` itself), optional fractional part, optional exponent
5. Define a `JsonValue` enum: `Null`, `Bool(bool)`, `Number(f64)`, `String(String)`, `Array(Vec<JsonValue>)`, `Object(Vec<(String, JsonValue)>)` (use `Vec` to preserve insertion order)
6. Implement a recursive descent parser that consumes the token stream and produces a `JsonValue`
7. Error types must include the position (line, column) and a descriptive message (e.g., "Expected ':' after object key at 3:15, found ','")
8. Reject invalid input: trailing commas, single quotes, unquoted keys, leading zeros in numbers, bare values outside a container at the top level (per strict RFC reading) -- or document if you allow extensions
9. Implement a streaming parser that emits events (`StartObject`, `EndObject`, `StartArray`, `EndArray`, `Key(String)`, `Value(JsonValue)`) via a callback or iterator
10. Handle edge cases: deeply nested structures (1000+ levels), empty objects/arrays, strings with null bytes, numbers at the limits of f64 precision

## Hints

<details>
<summary>Hint 1: Lexer structure with position tracking</summary>

Track position as you consume characters. A `Lexer` struct holding the source, current index, line, and column works well:

```rust
struct Lexer<'a> {
    input: &'a [u8],
    pos: usize,
    line: usize,
    col: usize,
}

impl<'a> Lexer<'a> {
    fn peek(&self) -> Option<u8> {
        self.input.get(self.pos).copied()
    }

    fn advance(&mut self) -> Option<u8> {
        let ch = self.input.get(self.pos).copied()?;
        self.pos += 1;
        if ch == b'\n' {
            self.line += 1;
            self.col = 1;
        } else {
            self.col += 1;
        }
        Some(ch)
    }
}
```
</details>

<details>
<summary>Hint 2: Recursive descent for values</summary>

The parser mirrors the JSON grammar directly. A `parse_value` function peeks at the next token and dispatches:

```rust
fn parse_value(&mut self) -> Result<JsonValue, ParseError> {
    match self.peek_token()? {
        Token::LeftBrace => self.parse_object(),
        Token::LeftBracket => self.parse_array(),
        Token::String(_) => { /* consume and wrap */ },
        Token::Number(_) => { /* consume and wrap */ },
        Token::True => { /* consume, return Bool(true) */ },
        Token::False => { /* consume, return Bool(false) */ },
        Token::Null => { /* consume, return Null */ },
        other => Err(self.error(format!("unexpected token: {:?}", other))),
    }
}
```
</details>

<details>
<summary>Hint 3: Unicode surrogate pair decoding</summary>

Characters outside the Basic Multilingual Plane are encoded as surrogate pairs in JSON: `\uD800\uDC00` through `\uDBFF\uDFFF`. Detect a high surrogate, require `\u` to follow immediately, read the low surrogate, then combine:

```rust
fn decode_surrogate_pair(high: u16, low: u16) -> Result<char, ParseError> {
    let codepoint = 0x10000 + ((high as u32 - 0xD800) << 10) + (low as u32 - 0xDC00);
    char::from_u32(codepoint).ok_or_else(|| /* error */)
}
```
</details>

<details>
<summary>Hint 4: Streaming parser as an iterator</summary>

Define events as an enum and implement `Iterator` on your streaming parser:

```rust
enum JsonEvent {
    StartObject,
    EndObject,
    StartArray,
    EndArray,
    Key(String),
    Value(JsonValue), // only primitives here
}

impl Iterator for StreamParser<'_> {
    type Item = Result<JsonEvent, ParseError>;
    fn next(&mut self) -> Option<Self::Item> { /* ... */ }
}
```

Use an explicit stack of states instead of recursion so you can yield one event at a time.
</details>

## Acceptance Criteria

- [ ] Lexer tokenizes all valid JSON tokens with correct position tracking
- [ ] Parser produces correct `JsonValue` for all JSON types: null, bool, number, string, array, object
- [ ] All string escape sequences handled: `\"`, `\\`, `\/`, `\b`, `\f`, `\n`, `\r`, `\t`, `\uXXXX`
- [ ] Unicode surrogate pairs decode correctly (e.g., `\uD83D\uDE00` produces the grinning face emoji)
- [ ] Numbers parse per spec: `-0`, `1.5e10`, `2.3E-4`, reject `01`, `+1`, `.5`
- [ ] Error messages include line:column position and describe what was expected vs found
- [ ] Streaming parser emits correct event sequence for nested structures
- [ ] Handles 1000-level nesting without stack overflow (increase stack or use iterative approach)
- [ ] Round-trip: parse then serialize produces semantically equivalent JSON
- [ ] All tests pass with `cargo test`

## Research Resources

- [RFC 8259: The JSON Data Interchange Format](https://datatracker.ietf.org/doc/html/rfc8259) -- the actual specification, short and readable
- [JSON.org](https://www.json.org/json-en.html) -- grammar railroad diagrams that map directly to parser functions
- [Crafting Interpreters: Scanning](https://craftinginterpreters.com/scanning.html) -- lexer patterns applicable to any language
- [Nystrom: Recursive Descent Parsing](https://craftinginterpreters.com/parsing-expressions.html) -- the recursive descent technique in depth
- [JSONTestSuite](https://github.com/nst/JSONTestSuite) -- 300+ test cases categorized as must-accept, must-reject, and implementation-defined
- [serde_json source](https://github.com/serde-rs/json) -- production Rust JSON parser for reference
