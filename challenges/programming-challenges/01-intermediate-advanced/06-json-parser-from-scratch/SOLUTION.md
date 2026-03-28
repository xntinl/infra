# 6. JSON Parser from Scratch -- Solution

## Architecture Overview

The parser follows a classic two-phase architecture:

1. **Lexer** (`Lexer`) -- scans raw bytes, emits a sequence of `Token` values, each annotated with its source position (line, column)
2. **Parser** (`Parser`) -- consumes the token stream via recursive descent, builds a `JsonValue` tree

A third component, the **Streaming Parser** (`StreamParser`), replaces tree construction with event emission. It uses an explicit state stack instead of call-stack recursion, allowing it to yield one event at a time as an `Iterator`.

```
Input (&str)
    |
    v
  Lexer --> Vec<Token>
    |
    v
  Parser --> JsonValue (tree)
    |
    v
  StreamParser --> Iterator<JsonEvent> (event stream)
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "json-parser"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
mod lexer;
mod parser;
mod stream;
mod value;

pub use lexer::{Lexer, Token, TokenKind};
pub use parser::Parser;
pub use stream::{JsonEvent, StreamParser};
pub use value::JsonValue;

use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub struct Position {
    pub line: usize,
    pub col: usize,
}

impl fmt::Display for Position {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}:{}", self.line, self.col)
    }
}

#[derive(Debug, Clone)]
pub struct ParseError {
    pub message: String,
    pub position: Position,
}

impl fmt::Display for ParseError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{} at {}", self.message, self.position)
    }
}

impl std::error::Error for ParseError {}

pub fn parse(input: &str) -> Result<JsonValue, ParseError> {
    let tokens = Lexer::new(input).tokenize()?;
    Parser::new(&tokens).parse()
}

pub fn parse_stream(input: &str) -> Result<Vec<JsonEvent>, ParseError> {
    let tokens = Lexer::new(input).tokenize()?;
    let parser = StreamParser::new(&tokens);
    parser.collect()
}
```

### src/value.rs

```rust
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub enum JsonValue {
    Null,
    Bool(bool),
    Number(f64),
    String(String),
    Array(Vec<JsonValue>),
    Object(Vec<(String, JsonValue)>),
}

impl JsonValue {
    pub fn serialize(&self) -> String {
        let mut buf = String::new();
        self.write_to(&mut buf);
        buf
    }

    fn write_to(&self, buf: &mut String) {
        match self {
            JsonValue::Null => buf.push_str("null"),
            JsonValue::Bool(b) => buf.push_str(if *b { "true" } else { "false" }),
            JsonValue::Number(n) => {
                if n.fract() == 0.0 && n.is_finite() && n.abs() < 1e15 {
                    buf.push_str(&format!("{}", *n as i64));
                } else {
                    buf.push_str(&format!("{}", n));
                }
            }
            JsonValue::String(s) => {
                buf.push('"');
                for ch in s.chars() {
                    match ch {
                        '"' => buf.push_str("\\\""),
                        '\\' => buf.push_str("\\\\"),
                        '\n' => buf.push_str("\\n"),
                        '\r' => buf.push_str("\\r"),
                        '\t' => buf.push_str("\\t"),
                        '\u{08}' => buf.push_str("\\b"),
                        '\u{0C}' => buf.push_str("\\f"),
                        c if c < '\u{20}' => buf.push_str(&format!("\\u{:04x}", c as u32)),
                        c => buf.push(c),
                    }
                }
                buf.push('"');
            }
            JsonValue::Array(items) => {
                buf.push('[');
                for (i, item) in items.iter().enumerate() {
                    if i > 0 {
                        buf.push(',');
                    }
                    item.write_to(buf);
                }
                buf.push(']');
            }
            JsonValue::Object(pairs) => {
                buf.push('{');
                for (i, (key, value)) in pairs.iter().enumerate() {
                    if i > 0 {
                        buf.push(',');
                    }
                    JsonValue::String(key.clone()).write_to(buf);
                    buf.push(':');
                    value.write_to(buf);
                }
                buf.push('}');
            }
        }
    }
}

impl fmt::Display for JsonValue {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.serialize())
    }
}
```

### src/lexer.rs

```rust
use crate::{ParseError, Position};

#[derive(Debug, Clone, PartialEq)]
pub enum TokenKind {
    LeftBrace,
    RightBrace,
    LeftBracket,
    RightBracket,
    Colon,
    Comma,
    String(String),
    Number(f64),
    True,
    False,
    Null,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub kind: TokenKind,
    pub pos: Position,
}

pub struct Lexer<'a> {
    input: &'a [u8],
    pos: usize,
    line: usize,
    col: usize,
}

impl<'a> Lexer<'a> {
    pub fn new(input: &'a str) -> Self {
        Lexer {
            input: input.as_bytes(),
            pos: 0,
            line: 1,
            col: 1,
        }
    }

    pub fn tokenize(&mut self) -> Result<Vec<Token>, ParseError> {
        let mut tokens = Vec::new();
        loop {
            self.skip_whitespace();
            if self.pos >= self.input.len() {
                break;
            }
            tokens.push(self.next_token()?);
        }
        Ok(tokens)
    }

    fn current_pos(&self) -> Position {
        Position {
            line: self.line,
            col: self.col,
        }
    }

    fn error(&self, msg: impl Into<String>) -> ParseError {
        ParseError {
            message: msg.into(),
            position: self.current_pos(),
        }
    }

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

    fn skip_whitespace(&mut self) {
        while let Some(ch) = self.peek() {
            if ch == b' ' || ch == b'\t' || ch == b'\n' || ch == b'\r' {
                self.advance();
            } else {
                break;
            }
        }
    }

    fn next_token(&mut self) -> Result<Token, ParseError> {
        let pos = self.current_pos();
        let ch = self.peek().ok_or_else(|| self.error("Unexpected end of input"))?;

        match ch {
            b'{' => { self.advance(); Ok(Token { kind: TokenKind::LeftBrace, pos }) }
            b'}' => { self.advance(); Ok(Token { kind: TokenKind::RightBrace, pos }) }
            b'[' => { self.advance(); Ok(Token { kind: TokenKind::LeftBracket, pos }) }
            b']' => { self.advance(); Ok(Token { kind: TokenKind::RightBracket, pos }) }
            b':' => { self.advance(); Ok(Token { kind: TokenKind::Colon, pos }) }
            b',' => { self.advance(); Ok(Token { kind: TokenKind::Comma, pos }) }
            b'"' => self.lex_string(pos),
            b't' => self.lex_keyword("true", TokenKind::True, pos),
            b'f' => self.lex_keyword("false", TokenKind::False, pos),
            b'n' => self.lex_keyword("null", TokenKind::Null, pos),
            b'-' | b'0'..=b'9' => self.lex_number(pos),
            _ => Err(self.error(format!("Unexpected character: '{}'", ch as char))),
        }
    }

    fn lex_keyword(&mut self, expected: &str, kind: TokenKind, pos: Position) -> Result<Token, ParseError> {
        for byte in expected.bytes() {
            match self.advance() {
                Some(ch) if ch == byte => {}
                Some(ch) => return Err(ParseError {
                    message: format!("Expected '{}', found '{}'", expected, ch as char),
                    position: pos,
                }),
                None => return Err(ParseError {
                    message: format!("Unexpected end of input while parsing '{}'", expected),
                    position: pos,
                }),
            }
        }
        Ok(Token { kind, pos })
    }

    fn lex_string(&mut self, pos: Position) -> Result<Token, ParseError> {
        self.advance(); // consume opening quote
        let mut s = String::new();

        loop {
            match self.advance() {
                None => return Err(ParseError {
                    message: "Unterminated string".into(),
                    position: pos,
                }),
                Some(b'"') => return Ok(Token { kind: TokenKind::String(s), pos }),
                Some(b'\\') => {
                    let escaped = self.advance().ok_or_else(|| ParseError {
                        message: "Unterminated escape sequence".into(),
                        position: self.current_pos(),
                    })?;
                    match escaped {
                        b'"' => s.push('"'),
                        b'\\' => s.push('\\'),
                        b'/' => s.push('/'),
                        b'b' => s.push('\u{08}'),
                        b'f' => s.push('\u{0C}'),
                        b'n' => s.push('\n'),
                        b'r' => s.push('\r'),
                        b't' => s.push('\t'),
                        b'u' => {
                            let cp = self.lex_unicode_escape()?;
                            if (0xD800..=0xDBFF).contains(&cp) {
                                self.expect_byte(b'\\')?;
                                self.expect_byte(b'u')?;
                                let low = self.lex_unicode_escape()?;
                                if !(0xDC00..=0xDFFF).contains(&low) {
                                    return Err(self.error(format!(
                                        "Expected low surrogate (\\uDC00-\\uDFFF), found \\u{:04X}", low
                                    )));
                                }
                                let codepoint = 0x10000 + ((cp as u32 - 0xD800) << 10) + (low as u32 - 0xDC00);
                                let ch = char::from_u32(codepoint).ok_or_else(|| {
                                    self.error(format!("Invalid Unicode codepoint: U+{:04X}", codepoint))
                                })?;
                                s.push(ch);
                            } else if (0xDC00..=0xDFFF).contains(&cp) {
                                return Err(self.error("Unexpected low surrogate without preceding high surrogate"));
                            } else {
                                let ch = char::from_u32(cp as u32).ok_or_else(|| {
                                    self.error(format!("Invalid Unicode codepoint: U+{:04X}", cp))
                                })?;
                                s.push(ch);
                            }
                        }
                        _ => return Err(self.error(format!("Invalid escape character: '\\{}'", escaped as char))),
                    }
                }
                Some(ch) if ch < 0x20 => {
                    return Err(self.error(format!("Control character U+{:04X} in string", ch)));
                }
                Some(ch) => s.push(ch as char),
            }
        }
    }

    fn lex_unicode_escape(&mut self) -> Result<u16, ParseError> {
        let mut value: u16 = 0;
        for _ in 0..4 {
            let ch = self.advance().ok_or_else(|| self.error("Unexpected end in Unicode escape"))?;
            let digit = match ch {
                b'0'..=b'9' => ch - b'0',
                b'a'..=b'f' => ch - b'a' + 10,
                b'A'..=b'F' => ch - b'A' + 10,
                _ => return Err(self.error(format!("Invalid hex digit in Unicode escape: '{}'", ch as char))),
            };
            value = value * 16 + digit as u16;
        }
        Ok(value)
    }

    fn expect_byte(&mut self, expected: u8) -> Result<(), ParseError> {
        match self.advance() {
            Some(ch) if ch == expected => Ok(()),
            Some(ch) => Err(self.error(format!("Expected '{}', found '{}'", expected as char, ch as char))),
            None => Err(self.error(format!("Expected '{}', found end of input", expected as char))),
        }
    }

    fn lex_number(&mut self, pos: Position) -> Result<Token, ParseError> {
        let start = self.pos;

        if self.peek() == Some(b'-') {
            self.advance();
        }

        match self.peek() {
            Some(b'0') => {
                self.advance();
                if let Some(ch) = self.peek() {
                    if ch.is_ascii_digit() {
                        return Err(ParseError {
                            message: "Leading zeros not allowed in numbers".into(),
                            position: pos,
                        });
                    }
                }
            }
            Some(b'1'..=b'9') => {
                self.advance();
                while let Some(ch) = self.peek() {
                    if ch.is_ascii_digit() {
                        self.advance();
                    } else {
                        break;
                    }
                }
            }
            _ => return Err(ParseError {
                message: "Expected digit after '-'".into(),
                position: pos,
            }),
        }

        if self.peek() == Some(b'.') {
            self.advance();
            let frac_start = self.pos;
            while let Some(ch) = self.peek() {
                if ch.is_ascii_digit() {
                    self.advance();
                } else {
                    break;
                }
            }
            if self.pos == frac_start {
                return Err(ParseError {
                    message: "Expected digit after decimal point".into(),
                    position: self.current_pos(),
                });
            }
        }

        if let Some(b'e' | b'E') = self.peek() {
            self.advance();
            if let Some(b'+' | b'-') = self.peek() {
                self.advance();
            }
            let exp_start = self.pos;
            while let Some(ch) = self.peek() {
                if ch.is_ascii_digit() {
                    self.advance();
                } else {
                    break;
                }
            }
            if self.pos == exp_start {
                return Err(ParseError {
                    message: "Expected digit in exponent".into(),
                    position: self.current_pos(),
                });
            }
        }

        let num_str = std::str::from_utf8(&self.input[start..self.pos]).unwrap();
        let value: f64 = num_str.parse().map_err(|_| ParseError {
            message: format!("Invalid number: '{}'", num_str),
            position: pos,
        })?;

        Ok(Token { kind: TokenKind::Number(value), pos })
    }
}
```

### src/parser.rs

```rust
use crate::{ParseError, Position, Token, TokenKind, JsonValue};

pub struct Parser<'a> {
    tokens: &'a [Token],
    pos: usize,
}

impl<'a> Parser<'a> {
    pub fn new(tokens: &'a [Token]) -> Self {
        Parser { tokens, pos: 0 }
    }

    pub fn parse(&mut self) -> Result<JsonValue, ParseError> {
        let value = self.parse_value()?;
        if self.pos < self.tokens.len() {
            let tok = &self.tokens[self.pos];
            return Err(ParseError {
                message: format!("Unexpected token after top-level value: {:?}", tok.kind),
                position: tok.pos.clone(),
            });
        }
        Ok(value)
    }

    fn current_pos(&self) -> Position {
        if self.pos < self.tokens.len() {
            self.tokens[self.pos].pos.clone()
        } else {
            Position { line: 0, col: 0 }
        }
    }

    fn peek(&self) -> Option<&TokenKind> {
        self.tokens.get(self.pos).map(|t| &t.kind)
    }

    fn advance(&mut self) -> Result<&Token, ParseError> {
        if self.pos < self.tokens.len() {
            let tok = &self.tokens[self.pos];
            self.pos += 1;
            Ok(tok)
        } else {
            Err(ParseError {
                message: "Unexpected end of input".into(),
                position: Position { line: 0, col: 0 },
            })
        }
    }

    fn expect(&mut self, expected: &TokenKind) -> Result<&Token, ParseError> {
        let tok = self.advance()?;
        if std::mem::discriminant(&tok.kind) == std::mem::discriminant(expected) {
            Ok(tok)
        } else {
            Err(ParseError {
                message: format!("Expected {:?}, found {:?}", expected, tok.kind),
                position: tok.pos.clone(),
            })
        }
    }

    fn parse_value(&mut self) -> Result<JsonValue, ParseError> {
        match self.peek() {
            Some(TokenKind::LeftBrace) => self.parse_object(),
            Some(TokenKind::LeftBracket) => self.parse_array(),
            Some(TokenKind::String(_)) => {
                let tok = self.advance()?;
                if let TokenKind::String(s) = &tok.kind {
                    Ok(JsonValue::String(s.clone()))
                } else {
                    unreachable!()
                }
            }
            Some(TokenKind::Number(_)) => {
                let tok = self.advance()?;
                if let TokenKind::Number(n) = &tok.kind {
                    Ok(JsonValue::Number(*n))
                } else {
                    unreachable!()
                }
            }
            Some(TokenKind::True) => { self.advance()?; Ok(JsonValue::Bool(true)) }
            Some(TokenKind::False) => { self.advance()?; Ok(JsonValue::Bool(false)) }
            Some(TokenKind::Null) => { self.advance()?; Ok(JsonValue::Null) }
            Some(other) => Err(ParseError {
                message: format!("Expected value, found {:?}", other),
                position: self.current_pos(),
            }),
            None => Err(ParseError {
                message: "Expected value, found end of input".into(),
                position: self.current_pos(),
            }),
        }
    }

    fn parse_object(&mut self) -> Result<JsonValue, ParseError> {
        self.advance()?; // consume '{'
        let mut pairs = Vec::new();

        if self.peek() == Some(&TokenKind::RightBrace) {
            self.advance()?;
            return Ok(JsonValue::Object(pairs));
        }

        loop {
            let key_tok = self.advance()?;
            let key = match &key_tok.kind {
                TokenKind::String(s) => s.clone(),
                other => return Err(ParseError {
                    message: format!("Expected string key, found {:?}", other),
                    position: key_tok.pos.clone(),
                }),
            };

            self.expect(&TokenKind::Colon)?;
            let value = self.parse_value()?;
            pairs.push((key, value));

            match self.peek() {
                Some(TokenKind::Comma) => { self.advance()?; }
                Some(TokenKind::RightBrace) => { self.advance()?; return Ok(JsonValue::Object(pairs)); }
                Some(other) => return Err(ParseError {
                    message: format!("Expected ',' or '}}' in object, found {:?}", other),
                    position: self.current_pos(),
                }),
                None => return Err(ParseError {
                    message: "Unterminated object".into(),
                    position: self.current_pos(),
                }),
            }
        }
    }

    fn parse_array(&mut self) -> Result<JsonValue, ParseError> {
        self.advance()?; // consume '['
        let mut items = Vec::new();

        if self.peek() == Some(&TokenKind::RightBracket) {
            self.advance()?;
            return Ok(JsonValue::Array(items));
        }

        loop {
            items.push(self.parse_value()?);

            match self.peek() {
                Some(TokenKind::Comma) => { self.advance()?; }
                Some(TokenKind::RightBracket) => { self.advance()?; return Ok(JsonValue::Array(items)); }
                Some(other) => return Err(ParseError {
                    message: format!("Expected ',' or ']' in array, found {:?}", other),
                    position: self.current_pos(),
                }),
                None => return Err(ParseError {
                    message: "Unterminated array".into(),
                    position: self.current_pos(),
                }),
            }
        }
    }
}
```

### src/stream.rs

```rust
use crate::{ParseError, Position, Token, TokenKind, JsonValue};

#[derive(Debug, Clone, PartialEq)]
pub enum JsonEvent {
    StartObject,
    EndObject,
    StartArray,
    EndArray,
    Key(String),
    Value(JsonValue),
}

enum State {
    Value,
    ObjectStart,
    ObjectKey,
    ObjectColon,
    ObjectValue,
    ObjectCommaOrEnd,
    ArrayStart,
    ArrayValue,
    ArrayCommaOrEnd,
    Done,
}

pub struct StreamParser<'a> {
    tokens: &'a [Token],
    pos: usize,
    state_stack: Vec<State>,
}

impl<'a> StreamParser<'a> {
    pub fn new(tokens: &'a [Token]) -> Self {
        StreamParser {
            tokens,
            pos: 0,
            state_stack: vec![State::Value],
        }
    }

    fn peek(&self) -> Option<&TokenKind> {
        self.tokens.get(self.pos).map(|t| &t.kind)
    }

    fn advance(&mut self) -> Result<&Token, ParseError> {
        if self.pos < self.tokens.len() {
            let tok = &self.tokens[self.pos];
            self.pos += 1;
            Ok(tok)
        } else {
            Err(ParseError {
                message: "Unexpected end of input".into(),
                position: Position { line: 0, col: 0 },
            })
        }
    }

    fn current_pos(&self) -> Position {
        if self.pos < self.tokens.len() {
            self.tokens[self.pos].pos.clone()
        } else {
            Position { line: 0, col: 0 }
        }
    }

    pub fn collect(mut self) -> Result<Vec<JsonEvent>, ParseError> {
        let mut events = Vec::new();
        while let Some(event) = self.next_event()? {
            events.push(event);
        }
        Ok(events)
    }

    fn next_event(&mut self) -> Result<Option<JsonEvent>, ParseError> {
        loop {
            let state = match self.state_stack.pop() {
                Some(s) => s,
                None => return Ok(None),
            };

            match state {
                State::Done => return Ok(None),

                State::Value => {
                    match self.peek() {
                        Some(TokenKind::LeftBrace) => {
                            self.advance()?;
                            self.state_stack.push(State::ObjectStart);
                            return Ok(Some(JsonEvent::StartObject));
                        }
                        Some(TokenKind::LeftBracket) => {
                            self.advance()?;
                            self.state_stack.push(State::ArrayStart);
                            return Ok(Some(JsonEvent::StartArray));
                        }
                        Some(_) => {
                            let tok = self.advance()?;
                            let val = match &tok.kind {
                                TokenKind::String(s) => JsonValue::String(s.clone()),
                                TokenKind::Number(n) => JsonValue::Number(*n),
                                TokenKind::True => JsonValue::Bool(true),
                                TokenKind::False => JsonValue::Bool(false),
                                TokenKind::Null => JsonValue::Null,
                                other => return Err(ParseError {
                                    message: format!("Expected value, found {:?}", other),
                                    position: tok.pos.clone(),
                                }),
                            };
                            return Ok(Some(JsonEvent::Value(val)));
                        }
                        None => return Err(ParseError {
                            message: "Expected value".into(),
                            position: self.current_pos(),
                        }),
                    }
                }

                State::ObjectStart => {
                    if self.peek() == Some(&TokenKind::RightBrace) {
                        self.advance()?;
                        return Ok(Some(JsonEvent::EndObject));
                    }
                    self.state_stack.push(State::ObjectKey);
                }

                State::ObjectKey => {
                    let tok = self.advance()?;
                    let key = match &tok.kind {
                        TokenKind::String(s) => s.clone(),
                        other => return Err(ParseError {
                            message: format!("Expected string key, found {:?}", other),
                            position: tok.pos.clone(),
                        }),
                    };
                    self.state_stack.push(State::ObjectColon);
                    return Ok(Some(JsonEvent::Key(key)));
                }

                State::ObjectColon => {
                    let tok = self.advance()?;
                    if tok.kind != TokenKind::Colon {
                        return Err(ParseError {
                            message: format!("Expected ':', found {:?}", tok.kind),
                            position: tok.pos.clone(),
                        });
                    }
                    self.state_stack.push(State::ObjectCommaOrEnd);
                    self.state_stack.push(State::Value);
                }

                State::ObjectValue => {
                    self.state_stack.push(State::ObjectCommaOrEnd);
                    self.state_stack.push(State::Value);
                }

                State::ObjectCommaOrEnd => {
                    match self.peek() {
                        Some(TokenKind::Comma) => {
                            self.advance()?;
                            self.state_stack.push(State::ObjectKey);
                        }
                        Some(TokenKind::RightBrace) => {
                            self.advance()?;
                            return Ok(Some(JsonEvent::EndObject));
                        }
                        _ => return Err(ParseError {
                            message: "Expected ',' or '}' in object".into(),
                            position: self.current_pos(),
                        }),
                    }
                }

                State::ArrayStart => {
                    if self.peek() == Some(&TokenKind::RightBracket) {
                        self.advance()?;
                        return Ok(Some(JsonEvent::EndArray));
                    }
                    self.state_stack.push(State::ArrayCommaOrEnd);
                    self.state_stack.push(State::Value);
                }

                State::ArrayValue => {
                    self.state_stack.push(State::ArrayCommaOrEnd);
                    self.state_stack.push(State::Value);
                }

                State::ArrayCommaOrEnd => {
                    match self.peek() {
                        Some(TokenKind::Comma) => {
                            self.advance()?;
                            self.state_stack.push(State::ArrayCommaOrEnd);
                            self.state_stack.push(State::Value);
                        }
                        Some(TokenKind::RightBracket) => {
                            self.advance()?;
                            return Ok(Some(JsonEvent::EndArray));
                        }
                        _ => return Err(ParseError {
                            message: "Expected ',' or ']' in array".into(),
                            position: self.current_pos(),
                        }),
                    }
                }
            }
        }
    }
}
```

### src/main.rs

```rust
use json_parser::parse;
use std::io::{self, Read};

fn main() {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input).expect("Failed to read stdin");

    match parse(&input) {
        Ok(value) => {
            println!("Parsed successfully:");
            println!("{}", value.serialize());
        }
        Err(e) => {
            eprintln!("Parse error: {}", e);
            std::process::exit(1);
        }
    }
}
```

## Tests

### tests/lexer_test.rs

```rust
use json_parser::{Lexer, TokenKind};

#[test]
fn tokenize_simple_object() {
    let mut lexer = Lexer::new(r#"{"key": "value"}"#);
    let tokens = lexer.tokenize().unwrap();
    let kinds: Vec<_> = tokens.iter().map(|t| &t.kind).collect();
    assert_eq!(kinds, vec![
        &TokenKind::LeftBrace,
        &TokenKind::String("key".into()),
        &TokenKind::Colon,
        &TokenKind::String("value".into()),
        &TokenKind::RightBrace,
    ]);
}

#[test]
fn tokenize_escape_sequences() {
    let mut lexer = Lexer::new(r#""hello\nworld\t\"quoted\"""#);
    let tokens = lexer.tokenize().unwrap();
    if let TokenKind::String(s) = &tokens[0].kind {
        assert_eq!(s, "hello\nworld\t\"quoted\"");
    } else {
        panic!("Expected string token");
    }
}

#[test]
fn tokenize_unicode_escape() {
    let mut lexer = Lexer::new(r#""\u0041\u0042\u0043""#);
    let tokens = lexer.tokenize().unwrap();
    if let TokenKind::String(s) = &tokens[0].kind {
        assert_eq!(s, "ABC");
    } else {
        panic!("Expected string token");
    }
}

#[test]
fn tokenize_surrogate_pair() {
    let mut lexer = Lexer::new(r#""\uD83D\uDE00""#);
    let tokens = lexer.tokenize().unwrap();
    if let TokenKind::String(s) = &tokens[0].kind {
        assert_eq!(s, "\u{1F600}");
    } else {
        panic!("Expected string token");
    }
}

#[test]
fn tokenize_numbers() {
    let cases = vec![
        ("0", 0.0), ("-0", -0.0), ("42", 42.0),
        ("3.14", 3.14), ("-1.5e10", -1.5e10), ("2.3E-4", 2.3e-4),
    ];
    for (input, expected) in cases {
        let mut lexer = Lexer::new(input);
        let tokens = lexer.tokenize().unwrap();
        if let TokenKind::Number(n) = tokens[0].kind {
            assert!((n - expected).abs() < 1e-15, "{}: got {} expected {}", input, n, expected);
        } else {
            panic!("Expected number for input: {}", input);
        }
    }
}

#[test]
fn reject_leading_zeros() {
    let mut lexer = Lexer::new("01");
    assert!(lexer.tokenize().is_err());
}

#[test]
fn position_tracking() {
    let input = "{\n  \"a\": 1\n}";
    let mut lexer = Lexer::new(input);
    let tokens = lexer.tokenize().unwrap();
    assert_eq!(tokens[0].pos.line, 1);
    assert_eq!(tokens[0].pos.col, 1);
    assert_eq!(tokens[1].pos.line, 2);
    assert_eq!(tokens[3].pos.line, 2);
}
```

### tests/parser_test.rs

```rust
use json_parser::{parse, JsonValue};

#[test]
fn parse_null() {
    assert_eq!(parse("null").unwrap(), JsonValue::Null);
}

#[test]
fn parse_bool() {
    assert_eq!(parse("true").unwrap(), JsonValue::Bool(true));
    assert_eq!(parse("false").unwrap(), JsonValue::Bool(false));
}

#[test]
fn parse_number() {
    assert_eq!(parse("42").unwrap(), JsonValue::Number(42.0));
    assert_eq!(parse("-3.14").unwrap(), JsonValue::Number(-3.14));
}

#[test]
fn parse_string() {
    assert_eq!(parse(r#""hello""#).unwrap(), JsonValue::String("hello".into()));
}

#[test]
fn parse_empty_array() {
    assert_eq!(parse("[]").unwrap(), JsonValue::Array(vec![]));
}

#[test]
fn parse_nested_object() {
    let input = r#"{"a": {"b": [1, 2, 3]}, "c": true}"#;
    let value = parse(input).unwrap();
    if let JsonValue::Object(pairs) = &value {
        assert_eq!(pairs.len(), 2);
        assert_eq!(pairs[0].0, "a");
        assert_eq!(pairs[1].0, "c");
    } else {
        panic!("Expected object");
    }
}

#[test]
fn parse_empty_object() {
    assert_eq!(parse("{}").unwrap(), JsonValue::Object(vec![]));
}

#[test]
fn reject_trailing_comma_array() {
    assert!(parse("[1, 2,]").is_err());
}

#[test]
fn reject_trailing_comma_object() {
    assert!(parse(r#"{"a": 1,}"#).is_err());
}

#[test]
fn error_has_position() {
    let err = parse("{ invalid }").unwrap_err();
    assert!(err.position.line >= 1);
    assert!(err.message.len() > 0);
}

#[test]
fn deeply_nested_array() {
    let depth = 200;
    let input = "[".repeat(depth) + &"]".repeat(depth);
    let value = parse(&input).unwrap();
    let mut current = &value;
    for _ in 0..depth {
        if let JsonValue::Array(items) = current {
            if items.is_empty() {
                break;
            }
            current = &items[0];
        } else {
            panic!("Expected array at each nesting level");
        }
    }
}

#[test]
fn round_trip() {
    let input = r#"{"name":"test","values":[1,2.5,true,null,"hello"]}"#;
    let value = parse(input).unwrap();
    let serialized = value.serialize();
    let reparsed = parse(&serialized).unwrap();
    assert_eq!(value, reparsed);
}
```

### tests/stream_test.rs

```rust
use json_parser::{parse_stream, JsonEvent, JsonValue};

#[test]
fn stream_simple_object() {
    let events = parse_stream(r#"{"a": 1}"#).unwrap();
    assert_eq!(events, vec![
        JsonEvent::StartObject,
        JsonEvent::Key("a".into()),
        JsonEvent::Value(JsonValue::Number(1.0)),
        JsonEvent::EndObject,
    ]);
}

#[test]
fn stream_nested() {
    let events = parse_stream(r#"[1, {"x": true}]"#).unwrap();
    assert_eq!(events, vec![
        JsonEvent::StartArray,
        JsonEvent::Value(JsonValue::Number(1.0)),
        JsonEvent::StartObject,
        JsonEvent::Key("x".into()),
        JsonEvent::Value(JsonValue::Bool(true)),
        JsonEvent::EndObject,
        JsonEvent::EndArray,
    ]);
}

#[test]
fn stream_empty_containers() {
    let events = parse_stream(r#"{"a": [], "b": {}}"#).unwrap();
    assert_eq!(events, vec![
        JsonEvent::StartObject,
        JsonEvent::Key("a".into()),
        JsonEvent::StartArray,
        JsonEvent::EndArray,
        JsonEvent::Key("b".into()),
        JsonEvent::StartObject,
        JsonEvent::EndObject,
        JsonEvent::EndObject,
    ]);
}
```

## Running

```bash
cargo test

echo '{"name": "test", "values": [1, 2.5, true, null]}' | cargo run
```

## Expected Output

```
running 17 tests
test lexer_test::tokenize_simple_object ... ok
test lexer_test::tokenize_escape_sequences ... ok
test lexer_test::tokenize_unicode_escape ... ok
test lexer_test::tokenize_surrogate_pair ... ok
test lexer_test::tokenize_numbers ... ok
test lexer_test::reject_leading_zeros ... ok
test lexer_test::position_tracking ... ok
test parser_test::parse_null ... ok
test parser_test::parse_bool ... ok
test parser_test::parse_number ... ok
test parser_test::parse_string ... ok
test parser_test::parse_empty_array ... ok
test parser_test::parse_nested_object ... ok
test parser_test::parse_empty_object ... ok
test parser_test::reject_trailing_comma_array ... ok
test parser_test::reject_trailing_comma_object ... ok
test parser_test::error_has_position ... ok
test parser_test::deeply_nested_array ... ok
test parser_test::round_trip ... ok
test stream_test::stream_simple_object ... ok
test stream_test::stream_nested ... ok
test stream_test::stream_empty_containers ... ok

test result: ok. 22 passed; 0 failed
```

```
Parsed successfully:
{"name":"test","values":[1,2.5,true,null]}
```

## Design Decisions

1. **Two-phase vs single-pass**: Separating lexing from parsing keeps both phases simple and testable independently. The token vector adds a memory allocation but enables better error messages since tokens carry position data.

2. **`Vec<(String, JsonValue)>` for objects**: Using a `Vec` of pairs instead of `HashMap` preserves insertion order, which matters for serialization round-trips and debugging. Lookup is O(n) but JSON objects are rarely used for random access at the parser level.

3. **f64 for all numbers**: JSON does not distinguish integers from floats. Using `f64` universally matches the spec's intent. The serializer detects whole numbers and formats them without a decimal point for cleaner output.

4. **Explicit state stack in StreamParser**: Recursive descent naturally uses the call stack, but yielding events mid-parse requires explicit continuation state. The state stack simulates the call stack, allowing the parser to pause and resume between events.

## Common Mistakes

1. **Forgetting surrogate pairs**: Many implementations parse `\uXXXX` as a single codepoint and break on emoji or CJK characters encoded as surrogate pairs. Always check if the codepoint falls in the high surrogate range (0xD800-0xDBFF) and expect a low surrogate to follow.

2. **Accepting leading zeros**: `007` is invalid JSON. The spec requires that numbers start with a non-zero digit, except for `0` itself (and `0.5`, `0e3`, etc.).

3. **Not handling control characters in strings**: Bare control characters (U+0000 through U+001F) inside strings are invalid per RFC 8259. They must be escaped.

4. **Incorrect position tracking on newlines**: When the lexer encounters `\n`, the column must reset to 1 and the line must increment. A common bug is off-by-one errors that report the column of the character after the newline as 0 instead of 1.

## Performance Notes

- The lexer allocates a `String` for each string token and a `Vec` for all tokens. For a streaming-only use case, you could avoid the token vector and lex on demand.
- `f64` parsing delegates to Rust's standard library, which handles edge cases (subnormals, infinity) correctly. Rolling your own float parser is a common source of precision bugs.
- Deep nesting uses stack space proportional to depth in the recursive parser. For untrusted input, set a max depth limit (e.g., 128) to prevent stack overflows.

## Going Further

- Add a max depth parameter to prevent stack overflow on adversarial input
- Implement `serde::Deserialize` on top of your parser to integrate with the Rust ecosystem
- Add a pretty-print serializer with configurable indentation
- Benchmark against `serde_json` using the [nativejson-benchmark](https://github.com/miloyip/nativejson-benchmark) test corpus
- Add support for JSON5 extensions (comments, trailing commas, unquoted keys) behind a feature flag
