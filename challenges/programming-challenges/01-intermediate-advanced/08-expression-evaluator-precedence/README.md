# 8. Expression Evaluator with Operator Precedence

<!--
difficulty: intermediate-advanced
category: parsers-and-compilers
languages: [rust]
concepts: [pratt-parsing, operator-precedence, repl, ast, evaluation, variable-binding]
estimated_time: 5-7 hours
bloom_level: analyze
prerequisites: [rust-basics, enums, pattern-matching, closures, hash-maps, traits]
-->

## Languages

- Rust (stable)

## Prerequisites

- Rust enums with data variants and `Box<T>` for recursive structures
- `HashMap` for variable storage
- Closures and function pointers for user-defined function dispatch
- Basic understanding of operator precedence (PEMDAS/BODMAS)

## Learning Objectives

- **Implement** a Pratt parser (top-down operator precedence) that handles prefix and infix operators
- **Analyze** how binding power drives parsing decisions for left-associative and right-associative operators
- **Design** an extensible operator table that separates grammar rules from parsing logic
- **Apply** the visitor pattern to evaluate an AST with variable bindings and function calls
- **Evaluate** the trade-offs between direct interpretation and AST-based evaluation

## The Challenge

Every programming language, spreadsheet, and calculator needs an expression evaluator. The core problem is operator precedence: `2 + 3 * 4` must evaluate to 14, not 20. Parentheses override precedence. Unary minus must bind tighter than addition but allow expressions like `-3 * 4`.

The naive approach -- writing a parsing function per precedence level -- works but creates deep, rigid code. Pratt parsing solves this elegantly: each operator carries a "binding power" that drives parsing decisions. Adding a new operator means adding one table entry, not restructuring the parser.

Your task is to build a complete expression evaluator in Rust using Pratt parsing. It must handle arithmetic with correct precedence, unary operators, parentheses, variables, user-defined functions, and provide a REPL interface for interactive use. Error messages must be descriptive, not cryptic.

## Requirements

1. Implement a tokenizer that recognizes: numbers (integers and floats), identifiers, operators (`+`, `-`, `*`, `/`, `%`), parentheses, `=` for assignment, and `,` for function argument separation
2. Implement a Pratt parser with explicit binding power for each operator: `+`/`-` at power 1, `*`/`/`/`%` at power 2, unary `-` as prefix with power 3, `^` (exponentiation) as right-associative at power 4
3. Build an AST with nodes: `Number(f64)`, `BinaryOp`, `UnaryOp`, `Variable(String)`, `FuncCall(String, Vec<Expr>)`, `Assignment(String, Expr)`
4. Implement an evaluator that walks the AST with a variable environment (`HashMap<String, f64>`)
5. Support variable assignment (`x = 5`) and variable reference (`x + 3`) within the same session
6. Register built-in functions: `sin`, `cos`, `tan`, `sqrt`, `abs`, `ln`, `log2`, `floor`, `ceil`, `min(a, b)`, `max(a, b)`, `pow(base, exp)`
7. The REPL reads one line at a time, evaluates it, and prints the result. Variables persist across lines
8. Error messages must describe the problem clearly: "Unknown variable 'y' at column 5", not "evaluation error"
9. Handle edge cases: division by zero, sqrt of negative number, nested function calls, chained assignments
10. Support implicit multiplication shorthand: `2(3)` = `2 * 3` and `2x` = `2 * x` (optional stretch goal)

## Hints

<details>
<summary>Hint 1: Pratt parsing core loop</summary>

The key insight is two functions: `parse_expr(min_bp)` drives parsing, and each operator has a left binding power (lbp) and right binding power (rbp):

```rust
fn parse_expr(&mut self, min_bp: u8) -> Result<Expr, ParseError> {
    let mut left = self.parse_prefix()?;

    while let Some((op, lbp, rbp)) = self.peek_infix() {
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
```

For left-associative operators: `rbp = lbp + 1`. For right-associative (like `^`): `rbp = lbp`.
</details>

<details>
<summary>Hint 2: Prefix operators in Pratt parsing</summary>

Prefix operators (unary minus, function calls, parentheses) are handled in `parse_prefix`:

```rust
fn parse_prefix(&mut self) -> Result<Expr, ParseError> {
    match self.advance() {
        Token::Number(n) => Ok(Expr::Number(n)),
        Token::Ident(name) => {
            if self.peek() == Some(&Token::LParen) {
                self.parse_func_call(name)
            } else {
                Ok(Expr::Variable(name))
            }
        }
        Token::Minus => {
            let expr = self.parse_expr(PREFIX_BP)?;
            Ok(Expr::UnaryOp { op: UnaryOp::Neg, expr: Box::new(expr) })
        }
        Token::LParen => {
            let expr = self.parse_expr(0)?;
            self.expect(Token::RParen)?;
            Ok(expr)
        }
        tok => Err(ParseError::unexpected(tok)),
    }
}
```
</details>

<details>
<summary>Hint 3: Function registry pattern</summary>

Store built-in functions as closures in a `HashMap`:

```rust
type BuiltinFn = Box<dyn Fn(&[f64]) -> Result<f64, EvalError>>;

fn register_builtins() -> HashMap<String, (usize, BuiltinFn)> {
    let mut fns: HashMap<String, (usize, BuiltinFn)> = HashMap::new();
    fns.insert("sin".into(), (1, Box::new(|args| Ok(args[0].sin()))));
    fns.insert("cos".into(), (1, Box::new(|args| Ok(args[0].cos()))));
    fns.insert("sqrt".into(), (1, Box::new(|args| {
        if args[0] < 0.0 {
            Err(EvalError::new("sqrt of negative number"))
        } else {
            Ok(args[0].sqrt())
        }
    })));
    fns.insert("min".into(), (2, Box::new(|args| Ok(args[0].min(args[1])))));
    fns
}
```

The `usize` tracks expected argument count for validation.
</details>

<details>
<summary>Hint 4: REPL with persistent state</summary>

The REPL loop is simple -- the key is that the evaluator state (variables) persists:

```rust
fn repl() {
    let mut env = Environment::new();
    let stdin = io::stdin();
    loop {
        print!("> ");
        io::stdout().flush().unwrap();
        let mut line = String::new();
        if stdin.read_line(&mut line).unwrap() == 0 { break; }
        let line = line.trim();
        if line.is_empty() || line == "quit" { break; }
        match env.eval_line(line) {
            Ok(result) => println!("= {}", result),
            Err(e) => eprintln!("Error: {}", e),
        }
    }
}
```
</details>

## Acceptance Criteria

- [ ] Arithmetic respects precedence: `2 + 3 * 4` evaluates to `14`
- [ ] Parentheses override: `(2 + 3) * 4` evaluates to `20`
- [ ] Unary minus works: `-3 * 4` = `-12`, `-(3 + 4)` = `-7`
- [ ] Exponentiation is right-associative: `2 ^ 3 ^ 2` = `512` (not `64`)
- [ ] Modulo operator: `10 % 3` = `1`
- [ ] Variable assignment and reference: `x = 5` then `x + 3` = `8`
- [ ] Built-in functions: `sqrt(16)` = `4`, `sin(0)` = `0`, `max(3, 7)` = `7`
- [ ] Nested calls: `sqrt(pow(3, 2) + pow(4, 2))` = `5`
- [ ] Division by zero produces a descriptive error, not a crash
- [ ] Unknown variables produce a descriptive error with position
- [ ] REPL maintains state across lines
- [ ] All tests pass with `cargo test`

## Research Resources

- [Simple but Powerful Pratt Parsing (matklad)](https://matklad.github.io/2020/04/13/simple-but-powerful-pratt-parsing.html) -- the definitive modern tutorial on Pratt parsing
- [Pratt Parsers: Expression Parsing Made Easy (Bob Nystrom)](http://journal.stuffwithstuff.com/2011/03/19/pratt-parsers-expression-parsing-made-easy/) -- the article that popularized Pratt parsing
- [Top Down Operator Precedence (Vaughan Pratt, 1973)](https://tdop.github.io/) -- the original paper
- [Crafting Interpreters: Compiling Expressions](https://craftinginterpreters.com/compiling-expressions.html) -- Pratt parsing in the context of a full compiler
- [Rust by Example: Closures](https://doc.rust-lang.org/rust-by-example/fn/closures.html) -- closures for the function registry
