# 8. Expression Evaluator with Operator Precedence -- Solution

## Architecture Overview

The evaluator has four components:

1. **Tokenizer** -- breaks input into tokens: numbers, identifiers, operators, parentheses
2. **Pratt Parser** -- builds an AST using top-down operator precedence (binding power)
3. **Evaluator** -- walks the AST with a variable environment and function registry
4. **REPL** -- interactive loop that persists variables across expressions

```
Input (&str)
    |
    v
  Tokenizer --> Vec<Token>
    |
    v
  Pratt Parser --> Expr (AST)
    |
    v
  Evaluator(Environment) --> f64
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "expr-eval"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
pub mod token;
pub mod parser;
pub mod eval;

pub use eval::Environment;
```

### src/token.rs

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum Token {
    Number(f64),
    Ident(String),
    Plus,
    Minus,
    Star,
    Slash,
    Percent,
    Caret,
    Eq,
    LParen,
    RParen,
    Comma,
    Eof,
}

#[derive(Debug, Clone)]
pub struct Spanned {
    pub token: Token,
    pub col: usize,
}

pub struct Tokenizer<'a> {
    input: &'a [u8],
    pos: usize,
}

impl<'a> Tokenizer<'a> {
    pub fn new(input: &'a str) -> Self {
        Tokenizer { input: input.as_bytes(), pos: 0 }
    }

    pub fn tokenize(&mut self) -> Result<Vec<Spanned>, String> {
        let mut tokens = Vec::new();
        loop {
            self.skip_whitespace();
            if self.pos >= self.input.len() {
                tokens.push(Spanned { token: Token::Eof, col: self.pos + 1 });
                return Ok(tokens);
            }
            let col = self.pos + 1;
            let ch = self.input[self.pos];
            let token = match ch {
                b'+' => { self.pos += 1; Token::Plus }
                b'-' => { self.pos += 1; Token::Minus }
                b'*' => { self.pos += 1; Token::Star }
                b'/' => { self.pos += 1; Token::Slash }
                b'%' => { self.pos += 1; Token::Percent }
                b'^' => { self.pos += 1; Token::Caret }
                b'=' => { self.pos += 1; Token::Eq }
                b'(' => { self.pos += 1; Token::LParen }
                b')' => { self.pos += 1; Token::RParen }
                b',' => { self.pos += 1; Token::Comma }
                b'0'..=b'9' | b'.' => self.read_number()?,
                _ if ch.is_ascii_alphabetic() || ch == b'_' => self.read_ident(),
                _ => return Err(format!("Unexpected character '{}' at column {}", ch as char, col)),
            };
            tokens.push(Spanned { token, col });
        }
    }

    fn skip_whitespace(&mut self) {
        while self.pos < self.input.len() && self.input[self.pos].is_ascii_whitespace() {
            self.pos += 1;
        }
    }

    fn read_number(&mut self) -> Result<Token, String> {
        let start = self.pos;
        while self.pos < self.input.len() && self.input[self.pos].is_ascii_digit() {
            self.pos += 1;
        }
        if self.pos < self.input.len() && self.input[self.pos] == b'.' {
            self.pos += 1;
            while self.pos < self.input.len() && self.input[self.pos].is_ascii_digit() {
                self.pos += 1;
            }
        }
        let s = std::str::from_utf8(&self.input[start..self.pos]).unwrap();
        let n: f64 = s.parse().map_err(|_| format!("Invalid number: '{}'", s))?;
        Ok(Token::Number(n))
    }

    fn read_ident(&mut self) -> Token {
        let start = self.pos;
        while self.pos < self.input.len()
            && (self.input[self.pos].is_ascii_alphanumeric() || self.input[self.pos] == b'_')
        {
            self.pos += 1;
        }
        let name = std::str::from_utf8(&self.input[start..self.pos]).unwrap().to_string();
        Token::Ident(name)
    }
}
```

### src/parser.rs

```rust
use crate::token::{Token, Spanned};

#[derive(Debug, Clone)]
pub enum Expr {
    Number(f64),
    Variable(String),
    BinaryOp {
        left: Box<Expr>,
        op: BinOp,
        right: Box<Expr>,
    },
    UnaryOp {
        op: UnaryOp,
        expr: Box<Expr>,
    },
    FuncCall {
        name: String,
        args: Vec<Expr>,
    },
    Assignment {
        name: String,
        value: Box<Expr>,
    },
}

#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BinOp {
    Add, Sub, Mul, Div, Mod, Pow,
}

impl std::fmt::Display for BinOp {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BinOp::Add => write!(f, "+"),
            BinOp::Sub => write!(f, "-"),
            BinOp::Mul => write!(f, "*"),
            BinOp::Div => write!(f, "/"),
            BinOp::Mod => write!(f, "%"),
            BinOp::Pow => write!(f, "^"),
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub enum UnaryOp {
    Neg,
}

pub struct Parser {
    tokens: Vec<Spanned>,
    pos: usize,
}

impl Parser {
    pub fn new(tokens: Vec<Spanned>) -> Self {
        Parser { tokens, pos: 0 }
    }

    fn peek(&self) -> &Token {
        &self.tokens[self.pos].token
    }

    fn peek_col(&self) -> usize {
        self.tokens[self.pos].col
    }

    fn advance(&mut self) -> &Spanned {
        let tok = &self.tokens[self.pos];
        self.pos += 1;
        tok
    }

    fn expect(&mut self, expected: &Token) -> Result<(), String> {
        let tok = self.advance();
        if std::mem::discriminant(&tok.token) == std::mem::discriminant(expected) {
            Ok(())
        } else {
            Err(format!("Expected {:?}, found {:?} at column {}", expected, tok.token, tok.col))
        }
    }

    pub fn parse(&mut self) -> Result<Expr, String> {
        // Check for assignment: ident = expr
        if let Token::Ident(name) = self.peek().clone() {
            if self.pos + 1 < self.tokens.len() && self.tokens[self.pos + 1].token == Token::Eq {
                self.advance(); // consume ident
                self.advance(); // consume =
                let value = self.parse_expr(0)?;
                return Ok(Expr::Assignment {
                    name,
                    value: Box::new(value),
                });
            }
        }
        let expr = self.parse_expr(0)?;
        if *self.peek() != Token::Eof {
            return Err(format!("Unexpected token {:?} at column {}", self.peek(), self.peek_col()));
        }
        Ok(expr)
    }

    fn parse_expr(&mut self, min_bp: u8) -> Result<Expr, String> {
        let mut left = self.parse_prefix()?;

        loop {
            if *self.peek() == Token::Eof || *self.peek() == Token::RParen || *self.peek() == Token::Comma {
                break;
            }

            let (op, lbp, rbp) = match self.peek() {
                Token::Plus => (BinOp::Add, 1, 2),
                Token::Minus => (BinOp::Sub, 1, 2),
                Token::Star => (BinOp::Mul, 3, 4),
                Token::Slash => (BinOp::Div, 3, 4),
                Token::Percent => (BinOp::Mod, 3, 4),
                Token::Caret => (BinOp::Pow, 6, 5), // right-associative: rbp < lbp
                _ => break,
            };

            if lbp < min_bp {
                break;
            }

            self.advance();
            let right = self.parse_expr(rbp)?;
            left = Expr::BinaryOp {
                left: Box::new(left),
                op,
                right: Box::new(right),
            };
        }

        Ok(left)
    }

    fn parse_prefix(&mut self) -> Result<Expr, String> {
        let col = self.peek_col();
        match self.peek().clone() {
            Token::Number(n) => {
                self.advance();
                Ok(Expr::Number(n))
            }
            Token::Ident(name) => {
                self.advance();
                if *self.peek() == Token::LParen {
                    self.advance(); // consume (
                    let mut args = Vec::new();
                    if *self.peek() != Token::RParen {
                        loop {
                            args.push(self.parse_expr(0)?);
                            if *self.peek() == Token::Comma {
                                self.advance();
                            } else {
                                break;
                            }
                        }
                    }
                    self.expect(&Token::RParen)?;
                    Ok(Expr::FuncCall { name, args })
                } else {
                    Ok(Expr::Variable(name))
                }
            }
            Token::Minus => {
                self.advance();
                let expr = self.parse_expr(7)?; // prefix bp higher than all infix
                Ok(Expr::UnaryOp {
                    op: UnaryOp::Neg,
                    expr: Box::new(expr),
                })
            }
            Token::LParen => {
                self.advance();
                let expr = self.parse_expr(0)?;
                self.expect(&Token::RParen)?;
                Ok(expr)
            }
            other => Err(format!("Unexpected token {:?} at column {}", other, col)),
        }
    }
}
```

### src/eval.rs

```rust
use crate::parser::{Expr, BinOp, UnaryOp};
use crate::token::Tokenizer;
use crate::parser::Parser;
use std::collections::HashMap;

type BuiltinFn = Box<dyn Fn(&[f64]) -> Result<f64, String>>;

pub struct Environment {
    variables: HashMap<String, f64>,
    functions: HashMap<String, (usize, BuiltinFn)>,
}

impl Environment {
    pub fn new() -> Self {
        let mut env = Environment {
            variables: HashMap::new(),
            functions: HashMap::new(),
        };
        env.register_builtins();
        env
    }

    fn register_builtins(&mut self) {
        self.register_fn("sin", 1, |args| Ok(args[0].sin()));
        self.register_fn("cos", 1, |args| Ok(args[0].cos()));
        self.register_fn("tan", 1, |args| Ok(args[0].tan()));
        self.register_fn("sqrt", 1, |args| {
            if args[0] < 0.0 {
                Err(format!("sqrt of negative number: {}", args[0]))
            } else {
                Ok(args[0].sqrt())
            }
        });
        self.register_fn("abs", 1, |args| Ok(args[0].abs()));
        self.register_fn("ln", 1, |args| {
            if args[0] <= 0.0 {
                Err(format!("ln of non-positive number: {}", args[0]))
            } else {
                Ok(args[0].ln())
            }
        });
        self.register_fn("log2", 1, |args| {
            if args[0] <= 0.0 {
                Err(format!("log2 of non-positive number: {}", args[0]))
            } else {
                Ok(args[0].log2())
            }
        });
        self.register_fn("floor", 1, |args| Ok(args[0].floor()));
        self.register_fn("ceil", 1, |args| Ok(args[0].ceil()));
        self.register_fn("min", 2, |args| Ok(args[0].min(args[1])));
        self.register_fn("max", 2, |args| Ok(args[0].max(args[1])));
        self.register_fn("pow", 2, |args| Ok(args[0].powf(args[1])));
    }

    fn register_fn<F>(&mut self, name: &str, arity: usize, f: F)
    where
        F: Fn(&[f64]) -> Result<f64, String> + 'static,
    {
        self.functions.insert(name.to_string(), (arity, Box::new(f)));
    }

    pub fn eval_line(&mut self, input: &str) -> Result<f64, String> {
        let mut tokenizer = Tokenizer::new(input);
        let tokens = tokenizer.tokenize()?;
        let mut parser = Parser::new(tokens);
        let expr = parser.parse()?;
        self.eval(&expr)
    }

    fn eval(&mut self, expr: &Expr) -> Result<f64, String> {
        match expr {
            Expr::Number(n) => Ok(*n),

            Expr::Variable(name) => {
                // Check for constants
                match name.as_str() {
                    "pi" | "PI" => return Ok(std::f64::consts::PI),
                    "e" | "E" => return Ok(std::f64::consts::E),
                    _ => {}
                }
                self.variables.get(name).copied()
                    .ok_or_else(|| format!("Unknown variable '{}'", name))
            }

            Expr::BinaryOp { left, op, right } => {
                let l = self.eval(left)?;
                let r = self.eval(right)?;
                match op {
                    BinOp::Add => Ok(l + r),
                    BinOp::Sub => Ok(l - r),
                    BinOp::Mul => Ok(l * r),
                    BinOp::Div => {
                        if r == 0.0 {
                            Err("Division by zero".to_string())
                        } else {
                            Ok(l / r)
                        }
                    }
                    BinOp::Mod => {
                        if r == 0.0 {
                            Err("Modulo by zero".to_string())
                        } else {
                            Ok(l % r)
                        }
                    }
                    BinOp::Pow => Ok(l.powf(r)),
                }
            }

            Expr::UnaryOp { op, expr } => {
                let val = self.eval(expr)?;
                match op {
                    UnaryOp::Neg => Ok(-val),
                }
            }

            Expr::FuncCall { name, args } => {
                let evaluated_args: Vec<f64> = args.iter()
                    .map(|a| self.eval(a))
                    .collect::<Result<Vec<_>, _>>()?;

                let (arity, func) = self.functions.get(name)
                    .ok_or_else(|| format!("Unknown function '{}'", name))?;

                if evaluated_args.len() != *arity {
                    return Err(format!(
                        "Function '{}' expects {} argument(s), got {}",
                        name, arity, evaluated_args.len()
                    ));
                }

                func(&evaluated_args)
            }

            Expr::Assignment { name, value } => {
                let val = self.eval(value)?;
                self.variables.insert(name.clone(), val);
                Ok(val)
            }
        }
    }
}

impl Default for Environment {
    fn default() -> Self {
        Self::new()
    }
}
```

### src/main.rs

```rust
use std::io::{self, Write, BufRead};
use expr_eval::Environment;

fn main() {
    let mut env = Environment::new();
    let stdin = io::stdin();

    println!("Expression Evaluator (type 'quit' to exit)");
    println!("Operators: + - * / % ^ | Functions: sin, cos, sqrt, abs, ln, ...");
    println!("Variables: x = 5, then x + 3 | Constants: pi, e");
    println!();

    loop {
        print!("> ");
        io::stdout().flush().unwrap();

        let mut line = String::new();
        if stdin.lock().read_line(&mut line).unwrap() == 0 {
            break;
        }
        let line = line.trim();
        if line.is_empty() {
            continue;
        }
        if line == "quit" || line == "exit" {
            break;
        }

        match env.eval_line(line) {
            Ok(result) => {
                if result == result.floor() && result.abs() < 1e15 {
                    println!("= {}", result as i64);
                } else {
                    println!("= {}", result);
                }
            }
            Err(e) => eprintln!("Error: {}", e),
        }
    }
}
```

## Tests

### tests/eval_test.rs

```rust
use expr_eval::Environment;

fn eval(input: &str) -> f64 {
    Environment::new().eval_line(input).unwrap()
}

fn eval_with_env(env: &mut Environment, input: &str) -> f64 {
    env.eval_line(input).unwrap()
}

fn eval_err(input: &str) -> String {
    Environment::new().eval_line(input).unwrap_err()
}

#[test]
fn basic_arithmetic() {
    assert_eq!(eval("2 + 3"), 5.0);
    assert_eq!(eval("10 - 4"), 6.0);
    assert_eq!(eval("3 * 7"), 21.0);
    assert_eq!(eval("15 / 3"), 5.0);
    assert_eq!(eval("10 % 3"), 1.0);
}

#[test]
fn precedence() {
    assert_eq!(eval("2 + 3 * 4"), 14.0);
    assert_eq!(eval("2 * 3 + 4"), 10.0);
    assert_eq!(eval("10 - 2 * 3"), 4.0);
    assert_eq!(eval("10 / 2 + 3"), 8.0);
}

#[test]
fn parentheses() {
    assert_eq!(eval("(2 + 3) * 4"), 20.0);
    assert_eq!(eval("2 * (3 + 4)"), 14.0);
    assert_eq!(eval("((2 + 3))"), 5.0);
}

#[test]
fn unary_minus() {
    assert_eq!(eval("-3"), -3.0);
    assert_eq!(eval("-3 * 4"), -12.0);
    assert_eq!(eval("-(3 + 4)"), -7.0);
    assert_eq!(eval("5 + -3"), 2.0);
}

#[test]
fn exponentiation_right_assoc() {
    assert_eq!(eval("2 ^ 3"), 8.0);
    assert_eq!(eval("2 ^ 3 ^ 2"), 512.0); // right-assoc: 2^(3^2) = 2^9 = 512
    assert_eq!(eval("4 ^ 0.5"), 2.0);
}

#[test]
fn variables() {
    let mut env = Environment::new();
    assert_eq!(eval_with_env(&mut env, "x = 5"), 5.0);
    assert_eq!(eval_with_env(&mut env, "x + 3"), 8.0);
    assert_eq!(eval_with_env(&mut env, "y = x * 2"), 10.0);
    assert_eq!(eval_with_env(&mut env, "y"), 10.0);
}

#[test]
fn builtin_functions() {
    assert!((eval("sin(0)")).abs() < 1e-10);
    assert!((eval("cos(0)") - 1.0).abs() < 1e-10);
    assert_eq!(eval("sqrt(16)"), 4.0);
    assert_eq!(eval("abs(-5)"), 5.0);
    assert_eq!(eval("max(3, 7)"), 7.0);
    assert_eq!(eval("min(3, 7)"), 3.0);
    assert_eq!(eval("floor(3.7)"), 3.0);
    assert_eq!(eval("ceil(3.2)"), 4.0);
}

#[test]
fn nested_functions() {
    assert_eq!(eval("sqrt(pow(3, 2) + pow(4, 2))"), 5.0);
    assert_eq!(eval("max(min(5, 3), min(7, 2))"), 3.0);
    assert_eq!(eval("abs(min(-5, -3))"), 5.0);
}

#[test]
fn constants() {
    assert!((eval("pi") - std::f64::consts::PI).abs() < 1e-10);
    assert!((eval("e") - std::f64::consts::E).abs() < 1e-10);
    assert!((eval("sin(pi)")).abs() < 1e-10);
}

#[test]
fn division_by_zero() {
    let err = eval_err("1 / 0");
    assert!(err.contains("Division by zero"));
}

#[test]
fn sqrt_negative() {
    let err = eval_err("sqrt(-1)");
    assert!(err.contains("negative"));
}

#[test]
fn unknown_variable() {
    let err = eval_err("x + 1");
    assert!(err.contains("Unknown variable"));
}

#[test]
fn unknown_function() {
    let err = eval_err("foo(1)");
    assert!(err.contains("Unknown function"));
}

#[test]
fn wrong_arity() {
    let err = eval_err("sin(1, 2)");
    assert!(err.contains("expects 1"));
}

#[test]
fn complex_expressions() {
    assert_eq!(eval("2 + 3 * 4 - 1"), 13.0);
    assert_eq!(eval("(1 + 2) * (3 + 4)"), 21.0);
    assert_eq!(eval("10 / 2 / 5"), 1.0); // left-assoc: (10/2)/5
}

#[test]
fn floating_point() {
    assert!((eval("0.1 + 0.2") - 0.3).abs() < 1e-10);
    assert_eq!(eval("3.14 * 2"), 6.28);
}
```

## Running

```bash
# Run tests
cargo test

# Run the REPL
cargo run
```

## Expected Output

```
running 17 tests
test eval_test::basic_arithmetic ... ok
test eval_test::precedence ... ok
test eval_test::parentheses ... ok
test eval_test::unary_minus ... ok
test eval_test::exponentiation_right_assoc ... ok
test eval_test::variables ... ok
test eval_test::builtin_functions ... ok
test eval_test::nested_functions ... ok
test eval_test::constants ... ok
test eval_test::division_by_zero ... ok
test eval_test::sqrt_negative ... ok
test eval_test::unknown_variable ... ok
test eval_test::unknown_function ... ok
test eval_test::wrong_arity ... ok
test eval_test::complex_expressions ... ok
test eval_test::floating_point ... ok

test result: ok. 17 passed; 0 failed
```

REPL session:

```
Expression Evaluator (type 'quit' to exit)
Operators: + - * / % ^ | Functions: sin, cos, sqrt, abs, ln, ...
Variables: x = 5, then x + 3 | Constants: pi, e

> 2 + 3 * 4
= 14
> x = 5
= 5
> x ^ 2 + 3
= 28
> sqrt(pow(3, 2) + pow(4, 2))
= 5
> sin(pi / 2)
= 1
> 1 / 0
Error: Division by zero
> quit
```

## Design Decisions

1. **Pratt parsing over recursive descent with precedence levels**: Each precedence level in recursive descent requires a separate function. Pratt parsing collapses this into a single `parse_expr(min_bp)` function with a binding power table. Adding `^` required one tuple, not a new function.

2. **Right-associativity via binding power asymmetry**: For left-associative operators, `rbp = lbp + 1`. For right-associative (`^`), `rbp = lbp - 1`. This single number difference produces completely different parse trees without any special-case code.

3. **`f64` for everything**: Matching the standard calculator model. Integer-only mode would require a separate value type and type-checking logic, adding complexity without pedagogical value for this challenge.

4. **Assignment as a statement, not an expression**: The parser detects `ident = expr` at the top level. This prevents ambiguity with equality testing (which this evaluator does not support) and makes the grammar unambiguous.

5. **Function registry with closures**: Using `Box<dyn Fn>` allows builtin functions to capture state if needed and makes the registry extensible at runtime. The arity check happens before the call, producing clear error messages.

## Common Mistakes

1. **Unary minus precedence**: If unary minus has the same binding power as binary minus, then `-3^2` parses as `(-3)^2 = 9` instead of `-(3^2) = -9`. Unary prefix operators need higher binding power than all infix operators.

2. **Forgetting right-associativity for `^`**: `2^3^2` must be `2^(3^2) = 512`, not `(2^3)^2 = 64`. If both lbp and rbp are the same value, the parser enters an infinite loop. The right operand must parse with `lbp` (not `lbp + 1`) to achieve right-associativity.

3. **Token lookahead for assignment vs expression**: `x = 5` (assignment) vs `x + 5` (expression starting with variable). The parser must look ahead two tokens before committing to the assignment path.

4. **Floating-point comparison in tests**: Never use `==` for float results. Use `(result - expected).abs() < epsilon`. The tests above use `1e-10` as epsilon.

## Performance Notes

- The tokenizer and parser allocate a `Vec` for tokens and recursive `Box<Expr>` nodes. For a REPL that processes one line at a time, this is negligible.
- The function registry uses dynamic dispatch (`Box<dyn Fn>`). For a hot loop evaluating millions of expressions, you could use an enum of builtin IDs with a match statement for static dispatch.
- The evaluator recursively walks the AST. For deeply nested expressions (which are rare in practice), this uses O(depth) stack space. A stack-based bytecode evaluator would use heap space instead.

## Going Further

- Add comparison operators (`<`, `>`, `==`) that return 0.0 or 1.0 (C-style booleans)
- Add conditional expressions: `if(condition, then_value, else_value)`
- Support user-defined functions: `f(x, y) = x^2 + y^2` then `f(3, 4)`
- Add complex number support: `2 + 3i`, `sqrt(-1)` = `i`
- Compile the AST to a stack-based bytecode for faster repeated evaluation
- Add a `graph` command that plots a function: `graph sin(x) from -pi to pi`
