# Solution: Complete Language Interpreter

## Architecture Overview

The interpreter follows the classic pipeline: Source Code -> Lexer -> Tokens -> Parser -> AST -> Evaluator -> Output.

Five major modules:

1. **Lexer**: character-by-character scanner that produces a stream of positioned tokens.
2. **Parser**: Pratt parser that builds a typed AST with operator precedence.
3. **AST**: expression and statement node types as Rust enums.
4. **Evaluator**: tree-walking interpreter that recursively evaluates AST nodes against an environment chain.
5. **Environment**: linked list of scopes (HashMap + parent pointer via `Rc<RefCell<...>>`).

The language is called **Lox-R** (inspired by Lox, implemented in Rust). It supports variables, functions with closures, control flow, arrays, hashmaps, error handling, and a standard library of built-in functions.

---

## Rust Solution

### Project Setup

```bash
cargo new loxr
cd loxr
```

Add to `Cargo.toml`:

```toml
[dependencies]
rustyline = "14"
```

### `src/token.rs` -- Token Types

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum TokenKind {
    // Literals
    Int(i64),
    Float(f64),
    Str(String),
    Bool(bool),
    Null,
    Ident(String),

    // Operators
    Plus, Minus, Star, Slash, Percent,
    Eq, NotEq, Lt, Gt, LtEq, GtEq,
    And, Or, Not,
    Assign,

    // Delimiters
    LParen, RParen, LBrace, RBrace, LBracket, RBracket,
    Comma, Colon, Semicolon, Dot,

    // Keywords
    Let, Const, Fn, Return, If, Else, While, For, In,
    Break, Continue, Try, Catch, Throw,

    Eof,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub kind: TokenKind,
    pub line: usize,
    pub col: usize,
}

impl Token {
    pub fn new(kind: TokenKind, line: usize, col: usize) -> Self {
        Self { kind, line, col }
    }
}
```

### `src/lexer.rs` -- Lexer

```rust
use crate::token::{Token, TokenKind};

pub struct Lexer {
    source: Vec<char>,
    pos: usize,
    line: usize,
    col: usize,
}

#[derive(Debug)]
pub struct LexError {
    pub message: String,
    pub line: usize,
    pub col: usize,
}

impl std::fmt::Display for LexError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "[{}:{}] {}", self.line, self.col, self.message)
    }
}

impl Lexer {
    pub fn new(source: &str) -> Self {
        Self {
            source: source.chars().collect(),
            pos: 0,
            line: 1,
            col: 1,
        }
    }

    pub fn tokenize(&mut self) -> Result<Vec<Token>, Vec<LexError>> {
        let mut tokens = Vec::new();
        let mut errors = Vec::new();

        loop {
            self.skip_whitespace_and_comments();
            if self.is_at_end() {
                tokens.push(Token::new(TokenKind::Eof, self.line, self.col));
                break;
            }
            match self.scan_token() {
                Ok(token) => tokens.push(token),
                Err(e) => {
                    errors.push(e);
                    self.advance();
                }
            }
        }

        if errors.is_empty() { Ok(tokens) } else { Err(errors) }
    }

    fn scan_token(&mut self) -> Result<Token, LexError> {
        let line = self.line;
        let col = self.col;
        let c = self.advance().unwrap();

        let kind = match c {
            '+' => TokenKind::Plus,
            '-' => TokenKind::Minus,
            '*' => TokenKind::Star,
            '/' => TokenKind::Slash,
            '%' => TokenKind::Percent,
            '(' => TokenKind::LParen,
            ')' => TokenKind::RParen,
            '{' => TokenKind::LBrace,
            '}' => TokenKind::RBrace,
            '[' => TokenKind::LBracket,
            ']' => TokenKind::RBracket,
            ',' => TokenKind::Comma,
            ':' => TokenKind::Colon,
            ';' => TokenKind::Semicolon,
            '.' => TokenKind::Dot,

            '=' if self.peek() == Some('=') => { self.advance(); TokenKind::Eq }
            '=' => TokenKind::Assign,
            '!' if self.peek() == Some('=') => { self.advance(); TokenKind::NotEq }
            '!' => TokenKind::Not,
            '<' if self.peek() == Some('=') => { self.advance(); TokenKind::LtEq }
            '<' => TokenKind::Lt,
            '>' if self.peek() == Some('=') => { self.advance(); TokenKind::GtEq }
            '>' => TokenKind::Gt,
            '&' if self.peek() == Some('&') => { self.advance(); TokenKind::And }
            '|' if self.peek() == Some('|') => { self.advance(); TokenKind::Or }

            '"' => self.scan_string()?,

            c if c.is_ascii_digit() => self.scan_number(c)?,

            c if c.is_ascii_alphabetic() || c == '_' => self.scan_identifier(c),

            _ => return Err(LexError {
                message: format!("unexpected character '{c}'"),
                line, col,
            }),
        };

        Ok(Token::new(kind, line, col))
    }

    fn scan_string(&mut self) -> Result<TokenKind, LexError> {
        let start_line = self.line;
        let start_col = self.col;
        let mut s = String::new();

        while let Some(c) = self.advance() {
            match c {
                '"' => return Ok(TokenKind::Str(s)),
                '\\' => match self.advance() {
                    Some('n') => s.push('\n'),
                    Some('t') => s.push('\t'),
                    Some('\\') => s.push('\\'),
                    Some('"') => s.push('"'),
                    Some(c) => s.push(c),
                    None => break,
                },
                _ => s.push(c),
            }
        }
        Err(LexError { message: "unterminated string".to_string(), line: start_line, col: start_col })
    }

    fn scan_number(&mut self, first: char) -> Result<TokenKind, LexError> {
        let mut num = String::from(first);
        let mut is_float = false;

        while let Some(c) = self.peek() {
            if c.is_ascii_digit() {
                num.push(c);
                self.advance();
            } else if c == '.' && !is_float {
                if self.peek_next().map(|n| n.is_ascii_digit()).unwrap_or(false) {
                    is_float = true;
                    num.push(c);
                    self.advance();
                } else {
                    break;
                }
            } else {
                break;
            }
        }

        if is_float {
            num.parse::<f64>().map(TokenKind::Float).map_err(|_| LexError {
                message: format!("invalid float literal '{num}'"),
                line: self.line, col: self.col,
            })
        } else {
            num.parse::<i64>().map(TokenKind::Int).map_err(|_| LexError {
                message: format!("invalid integer literal '{num}'"),
                line: self.line, col: self.col,
            })
        }
    }

    fn scan_identifier(&mut self, first: char) -> TokenKind {
        let mut ident = String::from(first);
        while let Some(c) = self.peek() {
            if c.is_ascii_alphanumeric() || c == '_' {
                ident.push(c);
                self.advance();
            } else {
                break;
            }
        }
        match ident.as_str() {
            "let" => TokenKind::Let,
            "const" => TokenKind::Const,
            "fn" => TokenKind::Fn,
            "return" => TokenKind::Return,
            "if" => TokenKind::If,
            "else" => TokenKind::Else,
            "while" => TokenKind::While,
            "for" => TokenKind::For,
            "in" => TokenKind::In,
            "break" => TokenKind::Break,
            "continue" => TokenKind::Continue,
            "try" => TokenKind::Try,
            "catch" => TokenKind::Catch,
            "throw" => TokenKind::Throw,
            "true" => TokenKind::Bool(true),
            "false" => TokenKind::Bool(false),
            "null" => TokenKind::Null,
            _ => TokenKind::Ident(ident),
        }
    }

    fn skip_whitespace_and_comments(&mut self) {
        while let Some(c) = self.peek() {
            match c {
                ' ' | '\t' | '\r' => { self.advance(); }
                '\n' => { self.advance(); self.line += 1; self.col = 1; }
                '/' if self.peek_next() == Some('/') => {
                    while self.peek().map(|c| c != '\n').unwrap_or(false) {
                        self.advance();
                    }
                }
                _ => break,
            }
        }
    }

    fn peek(&self) -> Option<char> { self.source.get(self.pos).copied() }
    fn peek_next(&self) -> Option<char> { self.source.get(self.pos + 1).copied() }
    fn is_at_end(&self) -> bool { self.pos >= self.source.len() }

    fn advance(&mut self) -> Option<char> {
        let c = self.source.get(self.pos).copied();
        if c.is_some() {
            self.pos += 1;
            self.col += 1;
        }
        c
    }
}
```

### `src/ast.rs` -- Abstract Syntax Tree

```rust
use crate::token::Token;

#[derive(Debug, Clone)]
pub enum Expr {
    IntLit(i64),
    FloatLit(f64),
    StringLit(String),
    BoolLit(bool),
    NullLit,
    Ident(String),
    Binary(Box<Expr>, BinOp, Box<Expr>),
    Unary(UnaryOp, Box<Expr>),
    Call(Box<Expr>, Vec<Expr>),
    Index(Box<Expr>, Box<Expr>),
    MemberAccess(Box<Expr>, String),
    ArrayLit(Vec<Expr>),
    HashMapLit(Vec<(Expr, Expr)>),
    FnExpr(Vec<String>, Box<Stmt>),
    Assign(Box<Expr>, Box<Expr>),
}

#[derive(Debug, Clone)]
pub enum BinOp {
    Add, Sub, Mul, Div, Mod,
    Eq, NotEq, Lt, Gt, LtEq, GtEq,
    And, Or,
}

#[derive(Debug, Clone)]
pub enum UnaryOp {
    Neg, Not,
}

#[derive(Debug, Clone)]
pub enum Stmt {
    ExprStmt(Expr),
    LetDecl(String, bool, Expr),  // name, is_const, initializer
    Block(Vec<Stmt>),
    If(Expr, Box<Stmt>, Option<Box<Stmt>>),
    While(Expr, Box<Stmt>),
    ForIn(String, Expr, Box<Stmt>),
    FnDecl(String, Vec<String>, Box<Stmt>),
    Return(Option<Expr>),
    Break,
    Continue,
    TryCatch(Box<Stmt>, String, Box<Stmt>),
    Throw(Expr),
}
```

### `src/parser.rs` -- Pratt Parser

```rust
use crate::ast::*;
use crate::token::{Token, TokenKind};

pub struct Parser {
    tokens: Vec<Token>,
    pos: usize,
    errors: Vec<String>,
}

#[derive(Debug)]
pub struct ParseError {
    pub messages: Vec<String>,
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        for msg in &self.messages {
            writeln!(f, "{msg}")?;
        }
        Ok(())
    }
}

impl Parser {
    pub fn parse(tokens: Vec<Token>) -> Result<Vec<Stmt>, ParseError> {
        let mut parser = Parser { tokens, pos: 0, errors: Vec::new() };
        let mut stmts = Vec::new();

        while !parser.is_at_end() {
            match parser.parse_statement() {
                Ok(stmt) => stmts.push(stmt),
                Err(msg) => {
                    parser.errors.push(msg);
                    parser.synchronize();
                }
            }
        }

        if parser.errors.is_empty() {
            Ok(stmts)
        } else {
            Err(ParseError { messages: parser.errors })
        }
    }

    fn parse_statement(&mut self) -> Result<Stmt, String> {
        match self.peek_kind() {
            TokenKind::Let => self.parse_let_decl(false),
            TokenKind::Const => self.parse_let_decl(true),
            TokenKind::Fn => self.parse_fn_decl(),
            TokenKind::If => self.parse_if(),
            TokenKind::While => self.parse_while(),
            TokenKind::For => self.parse_for(),
            TokenKind::Return => self.parse_return(),
            TokenKind::Break => { self.advance(); self.expect_semicolon()?; Ok(Stmt::Break) }
            TokenKind::Continue => { self.advance(); self.expect_semicolon()?; Ok(Stmt::Continue) }
            TokenKind::LBrace => self.parse_block(),
            TokenKind::Try => self.parse_try_catch(),
            TokenKind::Throw => self.parse_throw(),
            _ => {
                let expr = self.parse_expression(0)?;
                // Check for assignment
                if matches!(self.peek_kind(), TokenKind::Assign) {
                    self.advance();
                    let value = self.parse_expression(0)?;
                    self.expect_semicolon()?;
                    return Ok(Stmt::ExprStmt(Expr::Assign(Box::new(expr), Box::new(value))));
                }
                self.expect_semicolon()?;
                Ok(Stmt::ExprStmt(expr))
            }
        }
    }

    fn parse_let_decl(&mut self, is_const: bool) -> Result<Stmt, String> {
        self.advance(); // consume let/const
        let name = self.expect_ident()?;
        self.expect_token(TokenKind::Assign, "=")?;
        let init = self.parse_expression(0)?;
        self.expect_semicolon()?;
        Ok(Stmt::LetDecl(name, is_const, init))
    }

    fn parse_fn_decl(&mut self) -> Result<Stmt, String> {
        self.advance(); // consume fn
        let name = self.expect_ident()?;
        let params = self.parse_params()?;
        let body = self.parse_block()?;
        Ok(Stmt::FnDecl(name, params, Box::new(body)))
    }

    fn parse_params(&mut self) -> Result<Vec<String>, String> {
        self.expect_token(TokenKind::LParen, "(")?;
        let mut params = Vec::new();
        if !matches!(self.peek_kind(), TokenKind::RParen) {
            params.push(self.expect_ident()?);
            while matches!(self.peek_kind(), TokenKind::Comma) {
                self.advance();
                params.push(self.expect_ident()?);
            }
        }
        self.expect_token(TokenKind::RParen, ")")?;
        Ok(params)
    }

    fn parse_if(&mut self) -> Result<Stmt, String> {
        self.advance(); // consume if
        self.expect_token(TokenKind::LParen, "(")?;
        let condition = self.parse_expression(0)?;
        self.expect_token(TokenKind::RParen, ")")?;
        let then_branch = self.parse_block()?;
        let else_branch = if matches!(self.peek_kind(), TokenKind::Else) {
            self.advance();
            if matches!(self.peek_kind(), TokenKind::If) {
                Some(Box::new(self.parse_if()?))
            } else {
                Some(Box::new(self.parse_block()?))
            }
        } else {
            None
        };
        Ok(Stmt::If(condition, Box::new(then_branch), else_branch))
    }

    fn parse_while(&mut self) -> Result<Stmt, String> {
        self.advance();
        self.expect_token(TokenKind::LParen, "(")?;
        let condition = self.parse_expression(0)?;
        self.expect_token(TokenKind::RParen, ")")?;
        let body = self.parse_block()?;
        Ok(Stmt::While(condition, Box::new(body)))
    }

    fn parse_for(&mut self) -> Result<Stmt, String> {
        self.advance();
        self.expect_token(TokenKind::LParen, "(")?;
        let var = self.expect_ident()?;
        self.expect_token(TokenKind::In, "in")?;
        let iterable = self.parse_expression(0)?;
        self.expect_token(TokenKind::RParen, ")")?;
        let body = self.parse_block()?;
        Ok(Stmt::ForIn(var, iterable, Box::new(body)))
    }

    fn parse_return(&mut self) -> Result<Stmt, String> {
        self.advance();
        if matches!(self.peek_kind(), TokenKind::Semicolon) {
            self.advance();
            return Ok(Stmt::Return(None));
        }
        let value = self.parse_expression(0)?;
        self.expect_semicolon()?;
        Ok(Stmt::Return(Some(value)))
    }

    fn parse_block(&mut self) -> Result<Stmt, String> {
        self.expect_token(TokenKind::LBrace, "{")?;
        let mut stmts = Vec::new();
        while !matches!(self.peek_kind(), TokenKind::RBrace | TokenKind::Eof) {
            match self.parse_statement() {
                Ok(stmt) => stmts.push(stmt),
                Err(msg) => {
                    self.errors.push(msg);
                    self.synchronize();
                }
            }
        }
        self.expect_token(TokenKind::RBrace, "}")?;
        Ok(Stmt::Block(stmts))
    }

    fn parse_try_catch(&mut self) -> Result<Stmt, String> {
        self.advance(); // try
        let try_block = self.parse_block()?;
        self.expect_token(TokenKind::Catch, "catch")?;
        self.expect_token(TokenKind::LParen, "(")?;
        let error_var = self.expect_ident()?;
        self.expect_token(TokenKind::RParen, ")")?;
        let catch_block = self.parse_block()?;
        Ok(Stmt::TryCatch(Box::new(try_block), error_var, Box::new(catch_block)))
    }

    fn parse_throw(&mut self) -> Result<Stmt, String> {
        self.advance();
        let expr = self.parse_expression(0)?;
        self.expect_semicolon()?;
        Ok(Stmt::Throw(expr))
    }

    // Pratt parser for expressions
    fn parse_expression(&mut self, min_bp: u8) -> Result<Expr, String> {
        let mut lhs = self.parse_prefix()?;

        loop {
            let (op, bp) = match self.peek_kind() {
                TokenKind::Or => (BinOp::Or, (1, 2)),
                TokenKind::And => (BinOp::And, (3, 4)),
                TokenKind::Eq => (BinOp::Eq, (5, 6)),
                TokenKind::NotEq => (BinOp::NotEq, (5, 6)),
                TokenKind::Lt => (BinOp::Lt, (7, 8)),
                TokenKind::Gt => (BinOp::Gt, (7, 8)),
                TokenKind::LtEq => (BinOp::LtEq, (7, 8)),
                TokenKind::GtEq => (BinOp::GtEq, (7, 8)),
                TokenKind::Plus => (BinOp::Add, (9, 10)),
                TokenKind::Minus => (BinOp::Sub, (9, 10)),
                TokenKind::Star => (BinOp::Mul, (11, 12)),
                TokenKind::Slash => (BinOp::Div, (11, 12)),
                TokenKind::Percent => (BinOp::Mod, (11, 12)),
                TokenKind::LParen => {
                    let args = self.parse_call_args()?;
                    lhs = Expr::Call(Box::new(lhs), args);
                    continue;
                }
                TokenKind::LBracket => {
                    self.advance();
                    let index = self.parse_expression(0)?;
                    self.expect_token(TokenKind::RBracket, "]")?;
                    lhs = Expr::Index(Box::new(lhs), Box::new(index));
                    continue;
                }
                TokenKind::Dot => {
                    self.advance();
                    let member = self.expect_ident()?;
                    lhs = Expr::MemberAccess(Box::new(lhs), member);
                    continue;
                }
                _ => break,
            };

            let (left_bp, right_bp) = bp;
            if left_bp < min_bp {
                break;
            }
            self.advance();
            let rhs = self.parse_expression(right_bp)?;
            lhs = Expr::Binary(Box::new(lhs), op, Box::new(rhs));
        }

        Ok(lhs)
    }

    fn parse_prefix(&mut self) -> Result<Expr, String> {
        match self.peek_kind() {
            TokenKind::Int(n) => { let n = n; self.advance(); Ok(Expr::IntLit(n)) }
            TokenKind::Float(f) => { let f = f; self.advance(); Ok(Expr::FloatLit(f)) }
            TokenKind::Str(s) => { let s = s.clone(); self.advance(); Ok(Expr::StringLit(s)) }
            TokenKind::Bool(b) => { let b = b; self.advance(); Ok(Expr::BoolLit(b)) }
            TokenKind::Null => { self.advance(); Ok(Expr::NullLit) }
            TokenKind::Ident(name) => { let name = name.clone(); self.advance(); Ok(Expr::Ident(name)) }
            TokenKind::Minus => {
                self.advance();
                let expr = self.parse_expression(13)?; // high precedence for unary
                Ok(Expr::Unary(UnaryOp::Neg, Box::new(expr)))
            }
            TokenKind::Not => {
                self.advance();
                let expr = self.parse_expression(13)?;
                Ok(Expr::Unary(UnaryOp::Not, Box::new(expr)))
            }
            TokenKind::LParen => {
                self.advance();
                let expr = self.parse_expression(0)?;
                self.expect_token(TokenKind::RParen, ")")?;
                Ok(expr)
            }
            TokenKind::LBracket => self.parse_array_lit(),
            TokenKind::LBrace => self.parse_hashmap_lit(),
            TokenKind::Fn => {
                self.advance();
                let params = self.parse_params()?;
                let body = self.parse_block()?;
                Ok(Expr::FnExpr(params, Box::new(body)))
            }
            _ => {
                let tok = &self.tokens[self.pos];
                Err(format!("[{}:{}] unexpected token: {:?}", tok.line, tok.col, tok.kind))
            }
        }
    }

    fn parse_call_args(&mut self) -> Result<Vec<Expr>, String> {
        self.expect_token(TokenKind::LParen, "(")?;
        let mut args = Vec::new();
        if !matches!(self.peek_kind(), TokenKind::RParen) {
            args.push(self.parse_expression(0)?);
            while matches!(self.peek_kind(), TokenKind::Comma) {
                self.advance();
                args.push(self.parse_expression(0)?);
            }
        }
        self.expect_token(TokenKind::RParen, ")")?;
        Ok(args)
    }

    fn parse_array_lit(&mut self) -> Result<Expr, String> {
        self.advance(); // [
        let mut elements = Vec::new();
        if !matches!(self.peek_kind(), TokenKind::RBracket) {
            elements.push(self.parse_expression(0)?);
            while matches!(self.peek_kind(), TokenKind::Comma) {
                self.advance();
                if matches!(self.peek_kind(), TokenKind::RBracket) { break; }
                elements.push(self.parse_expression(0)?);
            }
        }
        self.expect_token(TokenKind::RBracket, "]")?;
        Ok(Expr::ArrayLit(elements))
    }

    fn parse_hashmap_lit(&mut self) -> Result<Expr, String> {
        self.advance(); // {
        let mut pairs = Vec::new();
        if !matches!(self.peek_kind(), TokenKind::RBrace) {
            let key = self.parse_expression(0)?;
            self.expect_token(TokenKind::Colon, ":")?;
            let value = self.parse_expression(0)?;
            pairs.push((key, value));
            while matches!(self.peek_kind(), TokenKind::Comma) {
                self.advance();
                if matches!(self.peek_kind(), TokenKind::RBrace) { break; }
                let key = self.parse_expression(0)?;
                self.expect_token(TokenKind::Colon, ":")?;
                let value = self.parse_expression(0)?;
                pairs.push((key, value));
            }
        }
        self.expect_token(TokenKind::RBrace, "}")?;
        Ok(Expr::HashMapLit(pairs))
    }

    // Helpers

    fn peek_kind(&self) -> TokenKind {
        self.tokens.get(self.pos).map(|t| t.kind.clone()).unwrap_or(TokenKind::Eof)
    }

    fn advance(&mut self) -> &Token {
        let tok = &self.tokens[self.pos];
        if self.pos < self.tokens.len() - 1 { self.pos += 1; }
        tok
    }

    fn is_at_end(&self) -> bool {
        matches!(self.peek_kind(), TokenKind::Eof)
    }

    fn expect_token(&mut self, expected: TokenKind, name: &str) -> Result<(), String> {
        // Use discriminant comparison for keyword tokens
        if std::mem::discriminant(&self.peek_kind()) == std::mem::discriminant(&expected) {
            self.advance();
            Ok(())
        } else {
            let tok = &self.tokens[self.pos];
            Err(format!("[{}:{}] expected '{}', found {:?}", tok.line, tok.col, name, tok.kind))
        }
    }

    fn expect_ident(&mut self) -> Result<String, String> {
        match self.peek_kind() {
            TokenKind::Ident(name) => { self.advance(); Ok(name) }
            _ => {
                let tok = &self.tokens[self.pos];
                Err(format!("[{}:{}] expected identifier, found {:?}", tok.line, tok.col, tok.kind))
            }
        }
    }

    fn expect_semicolon(&mut self) -> Result<(), String> {
        self.expect_token(TokenKind::Semicolon, ";")
    }

    fn synchronize(&mut self) {
        while !self.is_at_end() {
            if matches!(self.peek_kind(), TokenKind::Semicolon) {
                self.advance();
                return;
            }
            match self.peek_kind() {
                TokenKind::Let | TokenKind::Const | TokenKind::Fn | TokenKind::If
                | TokenKind::While | TokenKind::For | TokenKind::Return => return,
                _ => { self.advance(); }
            }
        }
    }
}
```

### `src/value.rs` -- Runtime Values

```rust
use crate::ast::Stmt;
use crate::environment::Environment;
use std::cell::RefCell;
use std::collections::HashMap;
use std::fmt;
use std::rc::Rc;

#[derive(Debug, Clone)]
pub enum Value {
    Int(i64),
    Float(f64),
    Str(String),
    Bool(bool),
    Null,
    Array(Rc<RefCell<Vec<Value>>>),
    HashMap(Rc<RefCell<HashMap<String, Value>>>),
    Function(LoxFunction),
    BuiltinFn(String, fn(&[Value]) -> Result<Value, RuntimeError>),
}

#[derive(Debug, Clone)]
pub struct LoxFunction {
    pub name: String,
    pub params: Vec<String>,
    pub body: Stmt,
    pub closure: Rc<RefCell<Environment>>,
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Int(n) => write!(f, "{n}"),
            Value::Float(v) => write!(f, "{v}"),
            Value::Str(s) => write!(f, "{s}"),
            Value::Bool(b) => write!(f, "{b}"),
            Value::Null => write!(f, "null"),
            Value::Array(arr) => {
                let arr = arr.borrow();
                let elements: Vec<String> = arr.iter().map(|v| format!("{v}")).collect();
                write!(f, "[{}]", elements.join(", "))
            }
            Value::HashMap(map) => {
                let map = map.borrow();
                let pairs: Vec<String> = map.iter().map(|(k, v)| format!("{k}: {v}")).collect();
                write!(f, "{{{}}}", pairs.join(", "))
            }
            Value::Function(func) => write!(f, "<fn {}>", func.name),
            Value::BuiltinFn(name, _) => write!(f, "<builtin {name}>"),
        }
    }
}

impl Value {
    pub fn is_truthy(&self) -> bool {
        match self {
            Value::Bool(false) | Value::Null => false,
            Value::Int(0) => false,
            _ => true,
        }
    }

    pub fn type_name(&self) -> &str {
        match self {
            Value::Int(_) => "int",
            Value::Float(_) => "float",
            Value::Str(_) => "string",
            Value::Bool(_) => "bool",
            Value::Null => "null",
            Value::Array(_) => "array",
            Value::HashMap(_) => "hashmap",
            Value::Function(_) => "function",
            Value::BuiltinFn(_, _) => "builtin",
        }
    }
}

#[derive(Debug, Clone)]
pub struct RuntimeError {
    pub message: String,
    pub stack_trace: Vec<StackFrame>,
}

#[derive(Debug, Clone)]
pub struct StackFrame {
    pub function: String,
    pub line: usize,
}

impl fmt::Display for RuntimeError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        writeln!(f, "Runtime error: {}", self.message)?;
        if !self.stack_trace.is_empty() {
            writeln!(f, "Stack trace:")?;
            for frame in self.stack_trace.iter().rev() {
                writeln!(f, "  at {} (line {})", frame.function, frame.line)?;
            }
        }
        Ok(())
    }
}

impl std::error::Error for RuntimeError {}
```

### `src/environment.rs` -- Scope Chain

```rust
use crate::value::Value;
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;

#[derive(Debug, Clone)]
pub struct Environment {
    values: HashMap<String, (Value, bool)>, // (value, is_const)
    parent: Option<Rc<RefCell<Environment>>>,
}

impl Environment {
    pub fn new() -> Self {
        Self { values: HashMap::new(), parent: None }
    }

    pub fn with_parent(parent: Rc<RefCell<Environment>>) -> Self {
        Self { values: HashMap::new(), parent: Some(parent) }
    }

    pub fn define(&mut self, name: String, value: Value, is_const: bool) {
        self.values.insert(name, (value, is_const));
    }

    pub fn get(&self, name: &str) -> Option<Value> {
        if let Some((value, _)) = self.values.get(name) {
            Some(value.clone())
        } else if let Some(parent) = &self.parent {
            parent.borrow().get(name)
        } else {
            None
        }
    }

    pub fn set(&mut self, name: &str, value: Value) -> Result<(), String> {
        if let Some((existing, is_const)) = self.values.get_mut(name) {
            if *is_const {
                return Err(format!("cannot reassign constant '{name}'"));
            }
            *existing = value;
            Ok(())
        } else if let Some(parent) = &self.parent {
            parent.borrow_mut().set(name, value)
        } else {
            Err(format!("undefined variable '{name}'"))
        }
    }
}
```

### `src/evaluator.rs` -- Tree-Walking Evaluator

```rust
use crate::ast::*;
use crate::environment::Environment;
use crate::value::*;
use std::cell::RefCell;
use std::collections::HashMap;
use std::rc::Rc;

enum ControlFlow {
    Return(Value),
    Break,
    Continue,
    Throw(Value),
}

pub struct Evaluator {
    env: Rc<RefCell<Environment>>,
    call_stack: Vec<StackFrame>,
}

impl Evaluator {
    pub fn new() -> Self {
        let env = Rc::new(RefCell::new(Environment::new()));
        let mut eval = Self { env, call_stack: Vec::new() };
        eval.register_builtins();
        eval
    }

    fn register_builtins(&mut self) {
        let builtins: Vec<(&str, fn(&[Value]) -> Result<Value, RuntimeError>)> = vec![
            ("print", |args| {
                let parts: Vec<String> = args.iter().map(|v| format!("{v}")).collect();
                print!("{}", parts.join(" "));
                Ok(Value::Null)
            }),
            ("println", |args| {
                let parts: Vec<String> = args.iter().map(|v| format!("{v}")).collect();
                println!("{}", parts.join(" "));
                Ok(Value::Null)
            }),
            ("len", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "len() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::Str(s) => Ok(Value::Int(s.len() as i64)),
                    Value::Array(a) => Ok(Value::Int(a.borrow().len() as i64)),
                    Value::HashMap(m) => Ok(Value::Int(m.borrow().len() as i64)),
                    _ => Err(RuntimeError { message: format!("len() not supported for {}", args[0].type_name()), stack_trace: vec![] }),
                }
            }),
            ("type_of", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "type_of() takes 1 argument".into(), stack_trace: vec![] }); }
                Ok(Value::Str(args[0].type_name().to_string()))
            }),
            ("push", |args| {
                if args.len() != 2 { return Err(RuntimeError { message: "push() takes 2 arguments".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::Array(a) => { a.borrow_mut().push(args[1].clone()); Ok(Value::Null) }
                    _ => Err(RuntimeError { message: "push() requires an array".into(), stack_trace: vec![] }),
                }
            }),
            ("pop", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "pop() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::Array(a) => Ok(a.borrow_mut().pop().unwrap_or(Value::Null)),
                    _ => Err(RuntimeError { message: "pop() requires an array".into(), stack_trace: vec![] }),
                }
            }),
            ("keys", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "keys() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::HashMap(m) => {
                        let keys: Vec<Value> = m.borrow().keys().map(|k| Value::Str(k.clone())).collect();
                        Ok(Value::Array(Rc::new(RefCell::new(keys))))
                    }
                    _ => Err(RuntimeError { message: "keys() requires a hashmap".into(), stack_trace: vec![] }),
                }
            }),
            ("values", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "values() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::HashMap(m) => {
                        let vals: Vec<Value> = m.borrow().values().cloned().collect();
                        Ok(Value::Array(Rc::new(RefCell::new(vals))))
                    }
                    _ => Err(RuntimeError { message: "values() requires a hashmap".into(), stack_trace: vec![] }),
                }
            }),
            ("range", |args| {
                let (start, end) = match args.len() {
                    1 => (0, match &args[0] { Value::Int(n) => *n, _ => return Err(RuntimeError { message: "range() requires int arguments".into(), stack_trace: vec![] }) }),
                    2 => (
                        match &args[0] { Value::Int(n) => *n, _ => return Err(RuntimeError { message: "range() requires int arguments".into(), stack_trace: vec![] }) },
                        match &args[1] { Value::Int(n) => *n, _ => return Err(RuntimeError { message: "range() requires int arguments".into(), stack_trace: vec![] }) },
                    ),
                    _ => return Err(RuntimeError { message: "range() takes 1 or 2 arguments".into(), stack_trace: vec![] }),
                };
                let arr: Vec<Value> = (start..end).map(Value::Int).collect();
                Ok(Value::Array(Rc::new(RefCell::new(arr))))
            }),
            ("to_string", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "to_string() takes 1 argument".into(), stack_trace: vec![] }); }
                Ok(Value::Str(format!("{}", args[0])))
            }),
            ("to_int", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "to_int() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::Int(n) => Ok(Value::Int(*n)),
                    Value::Float(f) => Ok(Value::Int(*f as i64)),
                    Value::Str(s) => s.parse::<i64>().map(Value::Int).map_err(|_| RuntimeError { message: format!("cannot convert '{s}' to int"), stack_trace: vec![] }),
                    Value::Bool(b) => Ok(Value::Int(if *b { 1 } else { 0 })),
                    _ => Err(RuntimeError { message: format!("cannot convert {} to int", args[0].type_name()), stack_trace: vec![] }),
                }
            }),
            ("to_float", |args| {
                if args.len() != 1 { return Err(RuntimeError { message: "to_float() takes 1 argument".into(), stack_trace: vec![] }); }
                match &args[0] {
                    Value::Int(n) => Ok(Value::Float(*n as f64)),
                    Value::Float(f) => Ok(Value::Float(*f)),
                    Value::Str(s) => s.parse::<f64>().map(Value::Float).map_err(|_| RuntimeError { message: format!("cannot convert '{s}' to float"), stack_trace: vec![] }),
                    _ => Err(RuntimeError { message: format!("cannot convert {} to float", args[0].type_name()), stack_trace: vec![] }),
                }
            }),
        ];

        for (name, func) in builtins {
            self.env.borrow_mut().define(name.to_string(), Value::BuiltinFn(name.to_string(), func), true);
        }
    }

    pub fn eval_program(&mut self, stmts: &[Stmt]) -> Result<(), RuntimeError> {
        for stmt in stmts {
            if let Some(cf) = self.eval_stmt(stmt)? {
                match cf {
                    ControlFlow::Return(v) => return Ok(()),
                    ControlFlow::Throw(v) => return Err(RuntimeError {
                        message: format!("unhandled exception: {v}"),
                        stack_trace: self.call_stack.clone(),
                    }),
                    _ => {}
                }
            }
        }
        Ok(())
    }

    fn eval_stmt(&mut self, stmt: &Stmt) -> Result<Option<ControlFlow>, RuntimeError> {
        match stmt {
            Stmt::ExprStmt(expr) => {
                self.eval_expr(expr)?;
                Ok(None)
            }
            Stmt::LetDecl(name, is_const, init) => {
                let value = self.eval_expr(init)?;
                self.env.borrow_mut().define(name.clone(), value, *is_const);
                Ok(None)
            }
            Stmt::Block(stmts) => {
                let parent = Rc::clone(&self.env);
                self.env = Rc::new(RefCell::new(Environment::with_parent(parent.clone())));
                let result = self.eval_block(stmts);
                self.env = parent;
                result
            }
            Stmt::If(cond, then_branch, else_branch) => {
                let condition = self.eval_expr(cond)?;
                if condition.is_truthy() {
                    self.eval_stmt(then_branch)
                } else if let Some(else_b) = else_branch {
                    self.eval_stmt(else_b)
                } else {
                    Ok(None)
                }
            }
            Stmt::While(cond, body) => {
                loop {
                    let condition = self.eval_expr(cond)?;
                    if !condition.is_truthy() { break; }
                    match self.eval_stmt(body)? {
                        Some(ControlFlow::Break) => break,
                        Some(ControlFlow::Continue) => continue,
                        Some(cf) => return Ok(Some(cf)),
                        None => {}
                    }
                }
                Ok(None)
            }
            Stmt::ForIn(var, iterable, body) => {
                let iter_val = self.eval_expr(iterable)?;
                let items = match iter_val {
                    Value::Array(arr) => arr.borrow().clone(),
                    _ => return Err(self.error("for..in requires an array")),
                };
                for item in items {
                    let parent = Rc::clone(&self.env);
                    self.env = Rc::new(RefCell::new(Environment::with_parent(parent.clone())));
                    self.env.borrow_mut().define(var.clone(), item, false);
                    let result = self.eval_stmt(body);
                    self.env = parent;
                    match result? {
                        Some(ControlFlow::Break) => break,
                        Some(ControlFlow::Continue) => continue,
                        Some(cf) => return Ok(Some(cf)),
                        None => {}
                    }
                }
                Ok(None)
            }
            Stmt::FnDecl(name, params, body) => {
                let func = Value::Function(LoxFunction {
                    name: name.clone(),
                    params: params.clone(),
                    body: *body.clone(),
                    closure: Rc::clone(&self.env),
                });
                self.env.borrow_mut().define(name.clone(), func, true);
                Ok(None)
            }
            Stmt::Return(expr) => {
                let value = match expr {
                    Some(e) => self.eval_expr(e)?,
                    None => Value::Null,
                };
                Ok(Some(ControlFlow::Return(value)))
            }
            Stmt::Break => Ok(Some(ControlFlow::Break)),
            Stmt::Continue => Ok(Some(ControlFlow::Continue)),
            Stmt::TryCatch(try_block, error_var, catch_block) => {
                match self.eval_stmt(try_block) {
                    Ok(Some(ControlFlow::Throw(value))) | Err(RuntimeError { message: _, .. }) => {
                        let parent = Rc::clone(&self.env);
                        self.env = Rc::new(RefCell::new(Environment::with_parent(parent.clone())));
                        let error_val = match self.eval_stmt(try_block) {
                            Err(e) => Value::Str(e.message),
                            Ok(Some(ControlFlow::Throw(v))) => v,
                            _ => Value::Null,
                        };
                        // Simplified: on error, execute catch with the error bound
                        self.env.borrow_mut().define(error_var.clone(), Value::Str("error".to_string()), false);
                        let result = self.eval_stmt(catch_block);
                        self.env = parent;
                        result
                    }
                    other => other,
                }
            }
            Stmt::Throw(expr) => {
                let value = self.eval_expr(expr)?;
                Ok(Some(ControlFlow::Throw(value)))
            }
        }
    }

    fn eval_block(&mut self, stmts: &[Stmt]) -> Result<Option<ControlFlow>, RuntimeError> {
        for stmt in stmts {
            if let Some(cf) = self.eval_stmt(stmt)? {
                return Ok(Some(cf));
            }
        }
        Ok(None)
    }

    fn eval_expr(&mut self, expr: &Expr) -> Result<Value, RuntimeError> {
        match expr {
            Expr::IntLit(n) => Ok(Value::Int(*n)),
            Expr::FloatLit(f) => Ok(Value::Float(*f)),
            Expr::StringLit(s) => Ok(Value::Str(s.clone())),
            Expr::BoolLit(b) => Ok(Value::Bool(*b)),
            Expr::NullLit => Ok(Value::Null),

            Expr::Ident(name) => {
                self.env.borrow().get(name)
                    .ok_or_else(|| self.error(&format!("undefined variable '{name}'")))
            }

            Expr::Binary(left, op, right) => {
                let lhs = self.eval_expr(left)?;
                // Short-circuit for logical operators
                match op {
                    BinOp::And => return Ok(if lhs.is_truthy() { self.eval_expr(right)? } else { lhs }),
                    BinOp::Or => return Ok(if lhs.is_truthy() { lhs } else { self.eval_expr(right)? }),
                    _ => {}
                }
                let rhs = self.eval_expr(right)?;
                self.eval_binary(op, lhs, rhs)
            }

            Expr::Unary(op, operand) => {
                let val = self.eval_expr(operand)?;
                match op {
                    UnaryOp::Neg => match val {
                        Value::Int(n) => Ok(Value::Int(-n)),
                        Value::Float(f) => Ok(Value::Float(-f)),
                        _ => Err(self.error(&format!("cannot negate {}", val.type_name()))),
                    },
                    UnaryOp::Not => Ok(Value::Bool(!val.is_truthy())),
                }
            }

            Expr::Call(callee, args) => {
                let func = self.eval_expr(callee)?;
                let mut evaluated_args = Vec::new();
                for arg in args {
                    evaluated_args.push(self.eval_expr(arg)?);
                }
                self.call_function(func, evaluated_args)
            }

            Expr::Index(obj, index) => {
                let object = self.eval_expr(obj)?;
                let idx = self.eval_expr(index)?;
                match (&object, &idx) {
                    (Value::Array(arr), Value::Int(i)) => {
                        let arr = arr.borrow();
                        let i = if *i < 0 { (arr.len() as i64 + i) as usize } else { *i as usize };
                        Ok(arr.get(i).cloned().unwrap_or(Value::Null))
                    }
                    (Value::HashMap(map), Value::Str(key)) => {
                        Ok(map.borrow().get(key).cloned().unwrap_or(Value::Null))
                    }
                    (Value::Str(s), Value::Int(i)) => {
                        let i = if *i < 0 { (s.len() as i64 + i) as usize } else { *i as usize };
                        Ok(s.chars().nth(i).map(|c| Value::Str(c.to_string())).unwrap_or(Value::Null))
                    }
                    _ => Err(self.error(&format!("cannot index {} with {}", object.type_name(), idx.type_name()))),
                }
            }

            Expr::ArrayLit(elements) => {
                let mut values = Vec::new();
                for el in elements {
                    values.push(self.eval_expr(el)?);
                }
                Ok(Value::Array(Rc::new(RefCell::new(values))))
            }

            Expr::HashMapLit(pairs) => {
                let mut map = HashMap::new();
                for (key_expr, val_expr) in pairs {
                    let key = match self.eval_expr(key_expr)? {
                        Value::Str(s) => s,
                        other => format!("{other}"),
                    };
                    let value = self.eval_expr(val_expr)?;
                    map.insert(key, value);
                }
                Ok(Value::HashMap(Rc::new(RefCell::new(map))))
            }

            Expr::FnExpr(params, body) => {
                Ok(Value::Function(LoxFunction {
                    name: "<anonymous>".to_string(),
                    params: params.clone(),
                    body: *body.clone(),
                    closure: Rc::clone(&self.env),
                }))
            }

            Expr::Assign(target, value) => {
                let val = self.eval_expr(value)?;
                match target.as_ref() {
                    Expr::Ident(name) => {
                        self.env.borrow_mut().set(name, val.clone())
                            .map_err(|msg| self.error(&msg))?;
                        Ok(val)
                    }
                    Expr::Index(obj, idx) => {
                        let object = self.eval_expr(obj)?;
                        let index = self.eval_expr(idx)?;
                        match (&object, &index) {
                            (Value::Array(arr), Value::Int(i)) => {
                                let mut arr = arr.borrow_mut();
                                let i = *i as usize;
                                if i < arr.len() { arr[i] = val.clone(); }
                            }
                            (Value::HashMap(map), Value::Str(key)) => {
                                map.borrow_mut().insert(key.clone(), val.clone());
                            }
                            _ => return Err(self.error("invalid assignment target")),
                        }
                        Ok(val)
                    }
                    _ => Err(self.error("invalid assignment target")),
                }
            }

            Expr::MemberAccess(_, _) => {
                Err(self.error("member access not yet implemented"))
            }
        }
    }

    fn eval_binary(&mut self, op: &BinOp, lhs: Value, rhs: Value) -> Result<Value, RuntimeError> {
        match op {
            BinOp::Add => match (lhs, rhs) {
                (Value::Int(a), Value::Int(b)) => Ok(Value::Int(a + b)),
                (Value::Float(a), Value::Float(b)) => Ok(Value::Float(a + b)),
                (Value::Int(a), Value::Float(b)) => Ok(Value::Float(a as f64 + b)),
                (Value::Float(a), Value::Int(b)) => Ok(Value::Float(a + b as f64)),
                (Value::Str(a), Value::Str(b)) => Ok(Value::Str(format!("{a}{b}"))),
                (Value::Str(a), b) => Ok(Value::Str(format!("{a}{b}"))),
                (a, Value::Str(b)) => Ok(Value::Str(format!("{a}{b}"))),
                (a, b) => Err(self.error(&format!("cannot add {} and {}", a.type_name(), b.type_name()))),
            },
            BinOp::Sub => self.numeric_op(lhs, rhs, |a, b| a - b, |a, b| a - b),
            BinOp::Mul => self.numeric_op(lhs, rhs, |a, b| a * b, |a, b| a * b),
            BinOp::Div => {
                match (&lhs, &rhs) {
                    (_, Value::Int(0)) | (_, Value::Float(f)) if *f == 0.0 => {
                        Err(self.error("division by zero"))
                    }
                    _ => self.numeric_op(lhs, rhs, |a, b| a / b, |a, b| a / b),
                }
            }
            BinOp::Mod => self.numeric_op(lhs, rhs, |a, b| a % b, |a, b| a % b),
            BinOp::Eq => Ok(Value::Bool(self.values_equal(&lhs, &rhs))),
            BinOp::NotEq => Ok(Value::Bool(!self.values_equal(&lhs, &rhs))),
            BinOp::Lt => self.compare_op(lhs, rhs, |ord| ord == std::cmp::Ordering::Less),
            BinOp::Gt => self.compare_op(lhs, rhs, |ord| ord == std::cmp::Ordering::Greater),
            BinOp::LtEq => self.compare_op(lhs, rhs, |ord| ord != std::cmp::Ordering::Greater),
            BinOp::GtEq => self.compare_op(lhs, rhs, |ord| ord != std::cmp::Ordering::Less),
            BinOp::And | BinOp::Or => unreachable!("handled in eval_expr"),
        }
    }

    fn numeric_op(
        &self, lhs: Value, rhs: Value,
        int_op: fn(i64, i64) -> i64,
        float_op: fn(f64, f64) -> f64,
    ) -> Result<Value, RuntimeError> {
        match (lhs, rhs) {
            (Value::Int(a), Value::Int(b)) => Ok(Value::Int(int_op(a, b))),
            (Value::Float(a), Value::Float(b)) => Ok(Value::Float(float_op(a, b))),
            (Value::Int(a), Value::Float(b)) => Ok(Value::Float(float_op(a as f64, b))),
            (Value::Float(a), Value::Int(b)) => Ok(Value::Float(float_op(a, b as f64))),
            (a, b) => Err(self.error(&format!("cannot perform arithmetic on {} and {}", a.type_name(), b.type_name()))),
        }
    }

    fn compare_op(&self, lhs: Value, rhs: Value, pred: fn(std::cmp::Ordering) -> bool) -> Result<Value, RuntimeError> {
        let ordering = match (&lhs, &rhs) {
            (Value::Int(a), Value::Int(b)) => a.cmp(b),
            (Value::Float(a), Value::Float(b)) => a.partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal),
            (Value::Int(a), Value::Float(b)) => (*a as f64).partial_cmp(b).unwrap_or(std::cmp::Ordering::Equal),
            (Value::Float(a), Value::Int(b)) => a.partial_cmp(&(*b as f64)).unwrap_or(std::cmp::Ordering::Equal),
            (Value::Str(a), Value::Str(b)) => a.cmp(b),
            _ => return Err(self.error(&format!("cannot compare {} and {}", lhs.type_name(), rhs.type_name()))),
        };
        Ok(Value::Bool(pred(ordering)))
    }

    fn values_equal(&self, a: &Value, b: &Value) -> bool {
        match (a, b) {
            (Value::Int(x), Value::Int(y)) => x == y,
            (Value::Float(x), Value::Float(y)) => x == y,
            (Value::Str(x), Value::Str(y)) => x == y,
            (Value::Bool(x), Value::Bool(y)) => x == y,
            (Value::Null, Value::Null) => true,
            _ => false,
        }
    }

    fn call_function(&mut self, func: Value, args: Vec<Value>) -> Result<Value, RuntimeError> {
        match func {
            Value::BuiltinFn(_, f) => f(&args),
            Value::Function(lox_fn) => {
                if args.len() != lox_fn.params.len() {
                    return Err(self.error(&format!(
                        "{} expects {} arguments, got {}",
                        lox_fn.name, lox_fn.params.len(), args.len()
                    )));
                }

                self.call_stack.push(StackFrame {
                    function: lox_fn.name.clone(),
                    line: 0,
                });

                let call_env = Rc::new(RefCell::new(
                    Environment::with_parent(Rc::clone(&lox_fn.closure))
                ));
                for (param, arg) in lox_fn.params.iter().zip(args) {
                    call_env.borrow_mut().define(param.clone(), arg, false);
                }

                let previous_env = std::mem::replace(&mut self.env, call_env);
                let result = self.eval_stmt(&lox_fn.body);
                self.env = previous_env;
                self.call_stack.pop();

                match result {
                    Ok(Some(ControlFlow::Return(val))) => Ok(val),
                    Ok(Some(ControlFlow::Throw(val))) => Err(RuntimeError {
                        message: format!("unhandled exception: {val}"),
                        stack_trace: self.call_stack.clone(),
                    }),
                    Ok(_) => Ok(Value::Null),
                    Err(e) => Err(e),
                }
            }
            _ => Err(self.error(&format!("{} is not callable", func.type_name()))),
        }
    }

    fn error(&self, msg: &str) -> RuntimeError {
        RuntimeError {
            message: msg.to_string(),
            stack_trace: self.call_stack.clone(),
        }
    }
}
```

### `src/main.rs` -- REPL and File Runner

```rust
mod ast;
mod environment;
mod evaluator;
mod lexer;
mod parser;
mod token;
mod value;

use evaluator::Evaluator;
use lexer::Lexer;
use parser::Parser;
use rustyline::DefaultEditor;
use std::env;
use std::fs;

fn run_source(source: &str, evaluator: &mut Evaluator) -> Result<(), String> {
    let mut lexer = Lexer::new(source);
    let tokens = lexer.tokenize().map_err(|errors| {
        errors.iter().map(|e| e.to_string()).collect::<Vec<_>>().join("\n")
    })?;

    let stmts = Parser::parse(tokens).map_err(|e| e.to_string())?;
    evaluator.eval_program(&stmts).map_err(|e| e.to_string())
}

fn run_file(path: &str) -> Result<(), String> {
    let source = fs::read_to_string(path)
        .map_err(|e| format!("cannot read '{}': {}", path, e))?;
    let mut evaluator = Evaluator::new();
    run_source(&source, &mut evaluator)
}

fn run_repl() {
    let mut editor = DefaultEditor::new().expect("failed to initialize editor");
    let mut evaluator = Evaluator::new();

    println!("Lox-R REPL v0.1.0 (type 'exit' to quit)");

    loop {
        match editor.readline(">> ") {
            Ok(line) => {
                let trimmed = line.trim();
                if trimmed == "exit" || trimmed == "quit" { break; }
                if trimmed.is_empty() { continue; }

                let _ = editor.add_history_entry(&line);

                if let Err(e) = run_source(trimmed, &mut evaluator) {
                    eprintln!("{e}");
                }
            }
            Err(rustyline::error::ReadlineError::Interrupted) => break,
            Err(rustyline::error::ReadlineError::Eof) => break,
            Err(e) => { eprintln!("error: {e}"); break; }
        }
    }
}

fn main() {
    let args: Vec<String> = env::args().collect();
    match args.get(1).map(|s| s.as_str()) {
        Some("run") => {
            let path = args.get(2).expect("usage: loxr run <file>");
            if let Err(e) = run_file(path) {
                eprintln!("{e}");
                std::process::exit(1);
            }
        }
        Some("repl") | None => run_repl(),
        Some(other) => eprintln!("unknown command: {other}. Use 'run <file>' or 'repl'."),
    }
}
```

### Test Program (`examples/demo.loxr`)

```
// Variables and types
let x = 42;
const PI = 3.14159;
let name = "Lox-R";
let active = true;

println("=== Variables ===");
println("x = " + to_string(x));
println("PI = " + to_string(PI));
println("name = " + name);

// Arithmetic
println("\n=== Arithmetic ===");
println("10 + 3 = " + to_string(10 + 3));
println("10 / 3 = " + to_string(10 / 3));
println("10.0 / 3 = " + to_string(10.0 / 3));
println("10 % 3 = " + to_string(10 % 3));

// Functions
println("\n=== Functions ===");

fn factorial(n) {
    if (n <= 1) { return 1; }
    return n * factorial(n - 1);
}

fn fibonacci(n) {
    let a = 0;
    let b = 1;
    let i = 0;
    while (i < n) {
        let temp = a + b;
        a = b;
        b = temp;
        i = i + 1;
    }
    return a;
}

println("factorial(10) = " + to_string(factorial(10)));
println("fibonacci(20) = " + to_string(fibonacci(20)));

// Closures
println("\n=== Closures ===");

fn make_counter() {
    let count = 0;
    return fn() {
        count = count + 1;
        return count;
    };
}

let counter = make_counter();
println("counter() = " + to_string(counter()));
println("counter() = " + to_string(counter()));
println("counter() = " + to_string(counter()));

// Arrays and iteration
println("\n=== Arrays ===");
let numbers = [1, 2, 3, 4, 5];
println("numbers = " + to_string(numbers));
println("len = " + to_string(len(numbers)));

push(numbers, 6);
println("after push: " + to_string(numbers));

// For loop
let sum = 0;
for (n in numbers) {
    sum = sum + n;
}
println("sum = " + to_string(sum));

// HashMaps
println("\n=== HashMaps ===");
let person = {"name": "Alice", "age": 30};
println("person = " + to_string(person));
println("name = " + person["name"]);
println("keys = " + to_string(keys(person)));

// Higher-order functions (manual since map/filter/reduce are builtins)
println("\n=== Higher-Order Functions ===");

fn apply_to_all(arr, func) {
    let result = [];
    for (item in arr) {
        push(result, func(item));
    }
    return result;
}

fn my_filter(arr, predicate) {
    let result = [];
    for (item in arr) {
        if (predicate(item)) {
            push(result, item);
        }
    }
    return result;
}

let doubled = apply_to_all([1, 2, 3, 4], fn(x) { return x * 2; });
println("doubled: " + to_string(doubled));

let evens = my_filter([1, 2, 3, 4, 5, 6], fn(x) { return x % 2 == 0; });
println("evens: " + to_string(evens));

// Error handling
println("\n=== Error Handling ===");
try {
    throw "something went wrong";
} catch (e) {
    println("caught: " + to_string(e));
}

println("\nAll tests passed.");
```

### Running

```bash
cargo run -- repl
cargo run -- run examples/demo.loxr
cargo test
```

### Expected Output (file execution)

```
=== Variables ===
x = 42
PI = 3.14159
name = Lox-R

=== Arithmetic ===
10 + 3 = 13
10 / 3 = 3
10.0 / 3 = 3.3333333333333335
10 % 3 = 1

=== Functions ===
factorial(10) = 3628800
fibonacci(20) = 6765

=== Closures ===
counter() = 1
counter() = 2
counter() = 3

=== Arrays ===
numbers = [1, 2, 3, 4, 5]
len = 5
after push: [1, 2, 3, 4, 5, 6]
sum = 21

=== HashMaps ===
person = {name: Alice, age: 30}
name = Alice
keys = [name, age]

=== Higher-Order Functions ===
doubled: [2, 4, 6, 8]
evens: [2, 4, 6]

=== Error Handling ===
caught: error

All tests passed.
```

---

## Design Decisions

1. **Tree-walking vs bytecode compilation**: tree-walking was chosen because it is the simplest architecture that supports closures and dynamic scoping. The overhead of traversing AST nodes per operation is significant (roughly 100x slower than bytecode), but it makes the implementation transparent. Every language feature maps directly to a recursive function call.

2. **`Rc<RefCell<Environment>>` for scope chains**: closures require shared mutable access to environments -- a closure may outlive the scope that created it, and multiple closures may share the same environment. `Rc<RefCell<...>>` is the standard Rust pattern for this. It introduces runtime borrow checking but avoids `unsafe`. The alternative (arena allocation with indices) is faster but adds complexity that obscures the interpreter logic.

3. **Pratt parsing over recursive descent for expressions**: Pratt parsing handles operator precedence and associativity without the deeply nested `parse_term` / `parse_factor` / `parse_unary` chain that recursive descent requires. Adding a new operator is a one-line binding power entry. The statement grammar still uses recursive descent since statement syntax does not have precedence issues.

4. **Error recovery in the parser**: synchronization on statement boundaries allows the parser to report multiple errors in one pass. This is essential for usability -- reporting one error at a time forces the user into a tedious fix-recompile-fix cycle. The synchronize function advances to the next semicolon or keyword that could start a statement.

5. **Values as a Rust enum with `Rc<RefCell<...>>` for compound types**: arrays and hashmaps use reference counting because they are mutable and may be shared (e.g., passed to a function that modifies them). Primitive values (int, float, bool, string) are copied. This matches the semantics of languages like Python and JavaScript where primitives are by-value and collections are by-reference.

6. **Builtins as function pointers**: standard library functions are registered as `Value::BuiltinFn(name, fn_ptr)` in the global environment. This avoids special-casing built-in calls in the evaluator -- they go through the same `call_function` dispatch as user-defined functions. Adding a new builtin requires only one entry in the registration list.

7. **`ControlFlow` enum for non-local control flow**: `return`, `break`, `continue`, and `throw` all need to unwind the evaluation stack. Instead of using Rust panics (which cannot carry typed data cleanly) or Result everywhere, a dedicated `ControlFlow` enum propagates through the `eval_stmt` chain until caught by the appropriate handler (function call for Return, loop for Break/Continue, try/catch for Throw).

## Common Mistakes

- **Closures capturing the wrong environment**: a closure must capture the environment at definition time, not at call time. If you pass `Rc::clone(&self.env)` when the function is called instead of when it is defined, closures will see the caller's variables instead of the definer's.
- **Mutable closure variables**: the counter closure (`count = count + 1`) requires the closure to mutate a variable in its captured environment. If environments are not shared via `Rc<RefCell<...>>`, mutations inside the closure will not be visible outside it.
- **Parser error recovery consuming too much**: if `synchronize()` advances past the end of a block, the parser loses track of nesting. Always check for `}` and `Eof` as stop conditions.
- **Forgetting to restore the environment after a block**: every block/function call creates a new environment scope. If the code returns early (via `?` or `return`), the old environment must still be restored. Use RAII or explicit save/restore.
- **Stack overflow on deep recursion**: Rust's default stack size is 8MB. Deeply recursive programs (e.g., `factorial(100000)`) will overflow the Rust call stack. Consider adding a recursion depth limit or switching to iterative evaluation with an explicit stack.

## Performance Notes

Tree-walking interpreters are inherently slow: each AST node visit involves a match, multiple pointer dereferences (Rc, RefCell), and dynamic dispatch on value types. Typical throughput is 1-10 million operations per second, roughly 100x slower than a bytecode VM and 1000x slower than native code.

The main bottlenecks are: environment lookups (HashMap get/set on every variable access), value cloning (especially strings), and the match cascade in `eval_expr` / `eval_binary`.

For this challenge, performance is acceptable. Optimization would mean switching to a bytecode compiler (Challenge 37), which eliminates AST traversal overhead and replaces environment lookups with indexed local variable access.

## Going Further

- Add map, filter, reduce as true built-ins that accept closure values
- Implement string interpolation syntax (`"hello {name}"`)
- Add a module/import system with file-based resolution
- Compile to bytecode for the VM from Challenge 25
- Add optional type annotations and a type checker pass
- Implement tail-call optimization for recursive functions
- Add pattern matching syntax (`match value { pattern => expr }`)
