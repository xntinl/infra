# 29. Advanced Pattern Matching

**Difficulty**: Intermedio

## Prerequisites
- Completed: enums, basic match expressions, if let, Option/Result
- Comfortable with structs, tuples, and references
- Understanding of ownership and borrowing

## Learning Objectives
After completing this exercise, you will be able to:
- Use nested patterns to destructure complex data in a single match arm
- Combine alternatives with or-patterns (`|`)
- Apply match guards for conditional logic within arms
- Bind matched values with `@` bindings
- Destructure in `let` bindings, function parameters, and `for` loops
- Use `ref` and `ref mut` patterns when needed
- Leverage the `matches!` macro for boolean pattern checks
- Understand `if let` chains (stabilized in Rust 2024 edition)
- Rely on exhaustiveness checking to catch missed cases

## Concepts

### Nested Patterns

Patterns can be nested to destructure deeply:

```rust
enum Shape {
    Circle { center: (f64, f64), radius: f64 },
    Rect { top_left: (f64, f64), bottom_right: (f64, f64) },
}

let shape = Shape::Circle { center: (1.0, 2.0), radius: 5.0 };

match shape {
    Shape::Circle { center: (x, y), radius } => {
        println!("Circle at ({}, {}) with r={}", x, y, radius);
    }
    Shape::Rect { top_left: (x1, y1), bottom_right: (x2, y2) } => {
        println!("Rect from ({}, {}) to ({}, {})", x1, y1, x2, y2);
    }
}
```

### Or-Patterns (`|`)

Match multiple alternatives in a single arm:

```rust
let code = 404;
let message = match code {
    200 | 201 | 202 => "Success",
    301 | 302 => "Redirect",
    400 | 401 | 403 => "Client Error",
    404 => "Not Found",
    500 | 502 | 503 => "Server Error",
    _ => "Unknown",
};
```

Or-patterns work inside nested patterns too:

```rust
enum Command {
    Quit,
    Echo(String),
    Move { x: i32, y: i32 },
}

match command {
    Command::Quit | Command::Echo(_) => println!("Simple command"),
    Command::Move { x, y } => println!("Move to ({}, {})", x, y),
}
```

### Match Guards

A match guard adds an `if` condition to an arm. The arm matches only if the pattern matches AND the guard is true:

```rust
let num = 4;
match num {
    n if n < 0 => println!("Negative"),
    n if n == 0 => println!("Zero"),
    n if n % 2 == 0 => println!("Positive even: {}", n),
    n => println!("Positive odd: {}", n),
}
```

Guards are checked after the pattern matches, and they can reference variables bound in the pattern:

```rust
let pair = (2, -3);
match pair {
    (x, y) if x > 0 && y > 0 => println!("Both positive"),
    (x, y) if x < 0 && y < 0 => println!("Both negative"),
    (x, y) => println!("Mixed: {} and {}", x, y),
}
```

**Important**: Match guards do NOT count for exhaustiveness. The compiler may require a catch-all `_` even if your guards cover all cases logically.

### `@` Bindings

The `@` operator lets you bind a name to a value while also testing it against a pattern:

```rust
let age = 25;
match age {
    child @ 0..=12 => println!("Child, age {}", child),
    teen @ 13..=19 => println!("Teenager, age {}", teen),
    adult @ 20..=64 => println!("Adult, age {}", adult),
    senior @ 65.. => println!("Senior, age {}", senior),
}
```

Without `@`, you would need a guard and lose the binding to the range:

```rust
// Less readable alternative
match age {
    n if n <= 12 => println!("Child, age {}", n),
    // ...
}
```

`@` bindings work with enums too:

```rust
enum Message {
    Hello { id: i32 },
}

match msg {
    Message::Hello { id: id_val @ 3..=7 } => {
        println!("Found id in range: {}", id_val);
    }
    Message::Hello { id } => {
        println!("Other id: {}", id);
    }
}
```

### Destructuring in `let`, Function Params, and `for`

Patterns are not limited to `match`. They work everywhere a binding occurs:

```rust
// let destructuring
let (x, y, z) = (1, 2, 3);
let Point { x, y } = point;

// Function parameter destructuring
fn distance(&(x1, y1): &(f64, f64), &(x2, y2): &(f64, f64)) -> f64 {
    ((x2 - x1).powi(2) + (y2 - y1).powi(2)).sqrt()
}

// for loop destructuring
let pairs = vec![(1, "one"), (2, "two"), (3, "three")];
for (num, name) in &pairs {
    println!("{}: {}", num, name);
}
```

### `ref` and `ref mut` in Patterns

By default, pattern matching moves or copies values. Use `ref` to borrow instead:

```rust
let name = String::from("Alice");

match name {
    ref n => println!("Borrowed: {}", n), // n is &String
}
// name is still usable here because we only borrowed it

match name {
    ref mut n => n.push_str("!"), // would need `let mut name`
}
```

In modern Rust (2021+), `match &value` or `match &mut value` usually makes `ref`/`ref mut` unnecessary since the compiler performs "match ergonomics" — automatically adding references. But `ref` is still needed in certain patterns.

### The `matches!` Macro

`matches!` returns `true` if a value matches a pattern. It is syntactic sugar for a `match` that returns `bool`:

```rust
let value = Some(42);
assert!(matches!(value, Some(x) if x > 0));
assert!(!matches!(value, None));

let status = 200;
assert!(matches!(status, 200 | 201 | 204));

// Useful in iterator chains
let numbers = vec![1, 2, 3, 4, 5];
let evens: Vec<_> = numbers.iter().filter(|n| matches!(n, x if *x % 2 == 0)).collect();
```

### `if let` Chains (Rust 2024 Edition)

Starting with Rust 2024, you can chain multiple `if let` conditions with `&&`:

```rust
// Rust 2024 edition
let config = Some(("host", 8080));
let auth = Some("admin");

if let Some((host, port)) = config
    && let Some(user) = auth
    && port > 0
{
    println!("Connecting to {} on port {} as {}", host, port, user);
}
```

Before 2024, you needed nested `if let` blocks:

```rust
// Pre-2024 equivalent
if let Some((host, port)) = config {
    if let Some(user) = auth {
        if port > 0 {
            println!("Connecting to {} on port {} as {}", host, port, user);
        }
    }
}
```

### Exhaustiveness Checking

The compiler ensures all possible values are covered:

```rust
enum Color {
    Red,
    Green,
    Blue,
}

match color {
    Color::Red => println!("Red"),
    Color::Green => println!("Green"),
    // Error: non-exhaustive patterns: `Blue` not covered
}
```

Adding `#[non_exhaustive]` to an enum (in a library) forces downstream matches to include `_`:

```rust
#[non_exhaustive]
pub enum Error {
    NotFound,
    Timeout,
}
// External users MUST have a _ arm, even if they list all variants
```

## Exercises

### Exercise 1: Nested Destructuring and Match Guards

Parse and categorize HTTP-like responses.

```rust
#[derive(Debug)]
enum Body {
    Json(String),
    Text(String),
    Empty,
}

#[derive(Debug)]
struct Response {
    status: u16,
    headers: Vec<(String, String)>,
    body: Body,
}

// TODO: Write a function `categorize` that matches on a Response and returns
// a &str description. Use nested patterns and match guards.
//
// Rules:
// - status 200..=299 with Body::Json(data) where data.len() > 100:
//     "Large JSON response"
// - status 200..=299 with Body::Json(_):
//     "JSON response"
// - status 200..=299 with Body::Text(text) where text.contains("error"):
//     "Success with error in body"
// - status 200..=299 with Body::Text(_):
//     "Text response"
// - status 200..=299 with Body::Empty:
//     "Empty success"
// - status 301 | 302 | 307 | 308:
//     "Redirect"
// - status 400..=499:
//     "Client error"
// - status 500..=599:
//     "Server error"
// - anything else:
//     "Unknown"

fn categorize(response: &Response) -> &str {
    // TODO: Implement using a single match expression
    todo!()
}

fn main() {
    let responses = vec![
        Response {
            status: 200,
            headers: vec![],
            body: Body::Json("{\"key\": \"value\"}".to_string()),
        },
        Response {
            status: 200,
            headers: vec![],
            body: Body::Json("x".repeat(150)),
        },
        Response {
            status: 201,
            headers: vec![],
            body: Body::Text("Created with error details".to_string()),
        },
        Response {
            status: 204,
            headers: vec![],
            body: Body::Empty,
        },
        Response {
            status: 301,
            headers: vec![("Location".into(), "/new".into())],
            body: Body::Empty,
        },
        Response {
            status: 404,
            headers: vec![],
            body: Body::Text("Not Found".into()),
        },
        Response {
            status: 503,
            headers: vec![],
            body: Body::Empty,
        },
    ];

    for resp in &responses {
        println!("Status {}: {}", resp.status, categorize(resp));
    }
}
```

Expected output:
```
Status 200: JSON response
Status 200: Large JSON response
Status 201: Success with error in body
Status 204: Empty success
Status 301: Redirect
Status 404: Client error
Status 503: Server error
```

<details>
<summary>Solution</summary>

```rust
#[derive(Debug)]
enum Body {
    Json(String),
    Text(String),
    Empty,
}

#[derive(Debug)]
struct Response {
    status: u16,
    headers: Vec<(String, String)>,
    body: Body,
}

fn categorize(response: &Response) -> &str {
    match response {
        Response { status: 200..=299, body: Body::Json(data), .. } if data.len() > 100 => {
            "Large JSON response"
        }
        Response { status: 200..=299, body: Body::Json(_), .. } => {
            "JSON response"
        }
        Response { status: 200..=299, body: Body::Text(text), .. } if text.contains("error") => {
            "Success with error in body"
        }
        Response { status: 200..=299, body: Body::Text(_), .. } => {
            "Text response"
        }
        Response { status: 200..=299, body: Body::Empty, .. } => {
            "Empty success"
        }
        Response { status: 301 | 302 | 307 | 308, .. } => {
            "Redirect"
        }
        Response { status: 400..=499, .. } => {
            "Client error"
        }
        Response { status: 500..=599, .. } => {
            "Server error"
        }
        _ => "Unknown",
    }
}

fn main() {
    let responses = vec![
        Response {
            status: 200,
            headers: vec![],
            body: Body::Json("{\"key\": \"value\"}".to_string()),
        },
        Response {
            status: 200,
            headers: vec![],
            body: Body::Json("x".repeat(150)),
        },
        Response {
            status: 201,
            headers: vec![],
            body: Body::Text("Created with error details".to_string()),
        },
        Response {
            status: 204,
            headers: vec![],
            body: Body::Empty,
        },
        Response {
            status: 301,
            headers: vec![("Location".into(), "/new".into())],
            body: Body::Empty,
        },
        Response {
            status: 404,
            headers: vec![],
            body: Body::Text("Not Found".into()),
        },
        Response {
            status: 503,
            headers: vec![],
            body: Body::Empty,
        },
    ];

    for resp in &responses {
        println!("Status {}: {}", resp.status, categorize(resp));
    }
}
```
</details>

### Exercise 2: `@` Bindings and Range Patterns

Build a grading system with detailed feedback using `@` bindings.

```rust
#[derive(Debug)]
struct Student {
    name: String,
    score: u32,
    attendance: f64, // 0.0 to 1.0
}

// TODO: Write a function `evaluate` that takes a &Student and returns a String
// with detailed feedback. Use @ bindings to capture matched values.
//
// Rules (check in this order):
// - score @ 90..=100 with attendance >= 0.9:
//     "{name}: Outstanding (score={score}, attendance={attendance:.0%})"
// - score @ 90..=100:
//     "{name}: Excellent academically but low attendance (score={score})"
// - score @ 70..=89 with attendance >= 0.8:
//     "{name}: Good standing (score={score})"
// - score @ 70..=89:
//     "{name}: Passing but attendance concern (score={score}, attendance={attendance:.0%})"
// - score @ 50..=69:
//     "{name}: At risk (score={score})"
// - failing @ 0..=49:
//     "{name}: Failing (score={failing}) — immediate intervention needed"
// - anything else:
//     "{name}: Invalid score"
fn evaluate(student: &Student) -> String {
    // TODO: Implement using match with @ bindings and guards
    todo!()
}

fn main() {
    let students = vec![
        Student { name: "Alice".into(), score: 95, attendance: 0.95 },
        Student { name: "Bob".into(), score: 92, attendance: 0.70 },
        Student { name: "Carol".into(), score: 78, attendance: 0.85 },
        Student { name: "Dave".into(), score: 75, attendance: 0.60 },
        Student { name: "Eve".into(), score: 55, attendance: 0.50 },
        Student { name: "Frank".into(), score: 30, attendance: 0.40 },
    ];

    for student in &students {
        println!("{}", evaluate(student));
    }
}
```

Expected output:
```
Alice: Outstanding (score=95, attendance=95%)
Bob: Excellent academically but low attendance (score=92)
Carol: Good standing (score=78)
Dave: Passing but attendance concern (score=75, attendance=60%)
Eve: At risk (score=55)
Frank: Failing (score=30) — immediate intervention needed
```

<details>
<summary>Solution</summary>

```rust
#[derive(Debug)]
struct Student {
    name: String,
    score: u32,
    attendance: f64,
}

fn evaluate(student: &Student) -> String {
    match student {
        Student { name, score: s @ 90..=100, attendance } if *attendance >= 0.9 => {
            format!(
                "{}: Outstanding (score={}, attendance={:.0}%)",
                name, s, attendance * 100.0
            )
        }
        Student { name, score: s @ 90..=100, .. } => {
            format!("{}: Excellent academically but low attendance (score={})", name, s)
        }
        Student { name, score: s @ 70..=89, attendance } if *attendance >= 0.8 => {
            format!("{}: Good standing (score={})", name, s)
        }
        Student { name, score: s @ 70..=89, attendance } => {
            format!(
                "{}: Passing but attendance concern (score={}, attendance={:.0}%)",
                name, s, attendance * 100.0
            )
        }
        Student { name, score: s @ 50..=69, .. } => {
            format!("{}: At risk (score={})", name, s)
        }
        Student { name, score: failing @ 0..=49, .. } => {
            format!(
                "{}: Failing (score={}) — immediate intervention needed",
                name, failing
            )
        }
        Student { name, .. } => {
            format!("{}: Invalid score", name)
        }
    }
}

fn main() {
    let students = vec![
        Student { name: "Alice".into(), score: 95, attendance: 0.95 },
        Student { name: "Bob".into(), score: 92, attendance: 0.70 },
        Student { name: "Carol".into(), score: 78, attendance: 0.85 },
        Student { name: "Dave".into(), score: 75, attendance: 0.60 },
        Student { name: "Eve".into(), score: 55, attendance: 0.50 },
        Student { name: "Frank".into(), score: 30, attendance: 0.40 },
    ];

    for student in &students {
        println!("{}", evaluate(student));
    }
}
```
</details>

### Exercise 3: Destructuring in Function Params and `for` Loops

Process a collection of records using pattern destructuring everywhere.

```rust
#[derive(Debug)]
enum Department {
    Engineering,
    Marketing,
    Sales,
    HR,
}

#[derive(Debug)]
struct Employee {
    name: String,
    department: Department,
    salary: (u32, String), // (amount, currency)
    active: bool,
}

// TODO: Write `format_salary` that destructures the tuple parameter directly
// fn format_salary(&(amount, ref currency): &(u32, String)) -> String
// Return: "{currency} {amount}"
fn format_salary(salary: &(u32, String)) -> String {
    // TODO: Destructure in the function body OR in the parameter
    todo!()
}

// TODO: Write `department_emoji` that pattern matches on Department
// Engineering -> "eng", Marketing -> "mkt", Sales -> "sales", HR -> "hr"
fn department_code(dept: &Department) -> &str {
    todo!()
}

// TODO: Write `summarize_team` that:
// 1. Iterates with `for` loop destructuring: for Employee { name, department, salary, active } in employees
// 2. Skips inactive employees using `matches!` or `if !active`
// 3. Uses nested destructuring on salary
// 4. Prints: "[{dept_code}] {name}: {formatted_salary}"
// 5. Returns count of active employees
fn summarize_team(employees: &[Employee]) -> usize {
    todo!()
}

fn main() {
    let team = vec![
        Employee {
            name: "Alice".into(),
            department: Department::Engineering,
            salary: (120000, "USD".into()),
            active: true,
        },
        Employee {
            name: "Bob".into(),
            department: Department::Marketing,
            salary: (90000, "USD".into()),
            active: true,
        },
        Employee {
            name: "Carol".into(),
            department: Department::Engineering,
            salary: (110000, "EUR".into()),
            active: false,
        },
        Employee {
            name: "Dave".into(),
            department: Department::Sales,
            salary: (85000, "GBP".into()),
            active: true,
        },
        Employee {
            name: "Eve".into(),
            department: Department::HR,
            salary: (95000, "USD".into()),
            active: true,
        },
    ];

    let active_count = summarize_team(&team);
    println!("\nActive employees: {}", active_count);

    // Demonstrate matches! macro
    let has_engineering = team.iter().any(|e| matches!(e.department, Department::Engineering));
    println!("Has engineering: {}", has_engineering);

    let high_earners: Vec<_> = team
        .iter()
        .filter(|e| matches!(e, Employee { salary: (s, _), active: true, .. } if *s > 100_000))
        .map(|e| &e.name)
        .collect();
    println!("High earners (active, >100k): {:?}", high_earners);
}
```

Expected output:
```
[eng] Alice: USD 120000
[mkt] Bob: USD 90000
[sales] Dave: GBP 85000
[hr] Eve: USD 95000

Active employees: 4
Has engineering: true
High earners (active, >100k): ["Alice"]
```

<details>
<summary>Solution</summary>

```rust
#[derive(Debug)]
enum Department {
    Engineering,
    Marketing,
    Sales,
    HR,
}

#[derive(Debug)]
struct Employee {
    name: String,
    department: Department,
    salary: (u32, String),
    active: bool,
}

fn format_salary(&(amount, ref currency): &(u32, String)) -> String {
    format!("{} {}", currency, amount)
}

fn department_code(dept: &Department) -> &str {
    match dept {
        Department::Engineering => "eng",
        Department::Marketing => "mkt",
        Department::Sales => "sales",
        Department::HR => "hr",
    }
}

fn summarize_team(employees: &[Employee]) -> usize {
    let mut count = 0;
    for Employee { name, department, salary, active } in employees {
        if !active {
            continue;
        }
        count += 1;
        println!(
            "[{}] {}: {}",
            department_code(department),
            name,
            format_salary(salary)
        );
    }
    count
}

fn main() {
    let team = vec![
        Employee {
            name: "Alice".into(),
            department: Department::Engineering,
            salary: (120000, "USD".into()),
            active: true,
        },
        Employee {
            name: "Bob".into(),
            department: Department::Marketing,
            salary: (90000, "USD".into()),
            active: true,
        },
        Employee {
            name: "Carol".into(),
            department: Department::Engineering,
            salary: (110000, "EUR".into()),
            active: false,
        },
        Employee {
            name: "Dave".into(),
            department: Department::Sales,
            salary: (85000, "GBP".into()),
            active: true,
        },
        Employee {
            name: "Eve".into(),
            department: Department::HR,
            salary: (95000, "USD".into()),
            active: true,
        },
    ];

    let active_count = summarize_team(&team);
    println!("\nActive employees: {}", active_count);

    let has_engineering = team.iter().any(|e| matches!(e.department, Department::Engineering));
    println!("Has engineering: {}", has_engineering);

    let high_earners: Vec<_> = team
        .iter()
        .filter(|e| matches!(e, Employee { salary: (s, _), active: true, .. } if *s > 100_000))
        .map(|e| &e.name)
        .collect();
    println!("High earners (active, >100k): {:?}", high_earners);
}
```
</details>

### Exercise 4: `matches!` Macro and Complex Filtering

Use `matches!` for concise pattern-based filtering in iterator chains.

```rust
#[derive(Debug, Clone)]
enum Token {
    Number(f64),
    Operator(char),
    Identifier(String),
    Keyword(String),
    StringLiteral(String),
    LeftParen,
    RightParen,
    Semicolon,
    Whitespace,
    Comment(String),
}

// TODO: Write `is_meaningful` using matches! — returns true for tokens that
// are NOT Whitespace and NOT Comment
fn is_meaningful(token: &Token) -> bool {
    // TODO: Use matches! with negation
    todo!()
}

// TODO: Write `is_literal` using matches! — returns true for Number, StringLiteral
fn is_literal(token: &Token) -> bool {
    todo!()
}

// TODO: Write `is_keyword_if_or_while` using matches! — returns true if
// token is Keyword("if") or Keyword("while")
fn is_keyword_if_or_while(token: &Token) -> bool {
    // TODO: Use matches! with a guard
    todo!()
}

// TODO: Write `token_category` that returns a &str category for each token
// using a match expression. Use or-patterns where possible.
// Number | StringLiteral -> "literal"
// Operator(_) -> "operator"
// Identifier(_) -> "identifier"
// Keyword(_) -> "keyword"
// LeftParen | RightParen -> "delimiter"
// Semicolon -> "punctuation"
// Whitespace | Comment(_) -> "trivia"
fn token_category(token: &Token) -> &str {
    todo!()
}

fn main() {
    let tokens = vec![
        Token::Keyword("if".into()),
        Token::Whitespace,
        Token::Identifier("x".into()),
        Token::Whitespace,
        Token::Operator('>'),
        Token::Whitespace,
        Token::Number(10.0),
        Token::Whitespace,
        Token::LeftParen,
        Token::Comment("// check threshold".into()),
        Token::Keyword("return".into()),
        Token::Whitespace,
        Token::StringLiteral("hello".into()),
        Token::Semicolon,
        Token::RightParen,
    ];

    println!("=== All tokens ===");
    for token in &tokens {
        println!("  {:?} -> {}", token, token_category(token));
    }

    let meaningful: Vec<_> = tokens.iter().filter(|t| is_meaningful(t)).collect();
    println!("\n=== Meaningful tokens ({}) ===", meaningful.len());
    for token in &meaningful {
        println!("  {:?}", token);
    }

    let literals: Vec<_> = tokens.iter().filter(|t| is_literal(t)).collect();
    println!("\n=== Literals ({}) ===", literals.len());
    for token in &literals {
        println!("  {:?}", token);
    }

    let control_flow: Vec<_> = tokens.iter().filter(|t| is_keyword_if_or_while(t)).collect();
    println!("\n=== Control flow keywords ({}) ===", control_flow.len());
    for token in &control_flow {
        println!("  {:?}", token);
    }
}
```

Expected output:
```
=== All tokens ===
  Keyword("if") -> keyword
  Whitespace -> trivia
  Identifier("x") -> identifier
  Whitespace -> trivia
  Operator('>') -> operator
  Whitespace -> trivia
  Number(10.0) -> literal
  Whitespace -> trivia
  LeftParen -> delimiter
  Comment("// check threshold") -> trivia
  Keyword("return") -> keyword
  Whitespace -> trivia
  StringLiteral("hello") -> literal
  Semicolon -> punctuation
  RightParen -> delimiter

=== Meaningful tokens (10) ===
  Keyword("if")
  Identifier("x")
  Operator('>')
  Number(10.0)
  LeftParen
  Keyword("return")
  StringLiteral("hello")
  Semicolon
  RightParen

=== Literals (2) ===
  Number(10.0)
  StringLiteral("hello")

=== Control flow keywords (1) ===
  Keyword("if")
```

<details>
<summary>Solution</summary>

```rust
#[derive(Debug, Clone)]
enum Token {
    Number(f64),
    Operator(char),
    Identifier(String),
    Keyword(String),
    StringLiteral(String),
    LeftParen,
    RightParen,
    Semicolon,
    Whitespace,
    Comment(String),
}

fn is_meaningful(token: &Token) -> bool {
    !matches!(token, Token::Whitespace | Token::Comment(_))
}

fn is_literal(token: &Token) -> bool {
    matches!(token, Token::Number(_) | Token::StringLiteral(_))
}

fn is_keyword_if_or_while(token: &Token) -> bool {
    matches!(token, Token::Keyword(kw) if kw == "if" || kw == "while")
}

fn token_category(token: &Token) -> &str {
    match token {
        Token::Number(_) | Token::StringLiteral(_) => "literal",
        Token::Operator(_) => "operator",
        Token::Identifier(_) => "identifier",
        Token::Keyword(_) => "keyword",
        Token::LeftParen | Token::RightParen => "delimiter",
        Token::Semicolon => "punctuation",
        Token::Whitespace | Token::Comment(_) => "trivia",
    }
}

fn main() {
    let tokens = vec![
        Token::Keyword("if".into()),
        Token::Whitespace,
        Token::Identifier("x".into()),
        Token::Whitespace,
        Token::Operator('>'),
        Token::Whitespace,
        Token::Number(10.0),
        Token::Whitespace,
        Token::LeftParen,
        Token::Comment("// check threshold".into()),
        Token::Keyword("return".into()),
        Token::Whitespace,
        Token::StringLiteral("hello".into()),
        Token::Semicolon,
        Token::RightParen,
    ];

    println!("=== All tokens ===");
    for token in &tokens {
        println!("  {:?} -> {}", token, token_category(token));
    }

    let meaningful: Vec<_> = tokens.iter().filter(|t| is_meaningful(t)).collect();
    println!("\n=== Meaningful tokens ({}) ===", meaningful.len());
    for token in &meaningful {
        println!("  {:?}", token);
    }

    let literals: Vec<_> = tokens.iter().filter(|t| is_literal(t)).collect();
    println!("\n=== Literals ({}) ===", literals.len());
    for token in &literals {
        println!("  {:?}", token);
    }

    let control_flow: Vec<_> = tokens.iter().filter(|t| is_keyword_if_or_while(t)).collect();
    println!("\n=== Control flow keywords ({}) ===", control_flow.len());
    for token in &control_flow {
        println!("  {:?}", token);
    }
}
```
</details>

### Exercise 5: Command Parser — Combining All Techniques

Build a command-line argument parser that uses every advanced pattern matching technique.

```rust
#[derive(Debug)]
enum Value {
    Int(i64),
    Float(f64),
    Str(String),
    Bool(bool),
    List(Vec<Value>),
}

#[derive(Debug)]
enum Command {
    Set { key: String, value: Value },
    Get { key: String },
    Delete { key: String },
    List { pattern: Option<String> },
    Quit,
}

// TODO: Write `parse_value` that converts a &str into a Value
// - "true" | "false" -> Value::Bool
// - Starts with '"' and ends with '"' -> Value::Str (without quotes)
// - Starts with '[' and ends with ']' -> Value::List (split by comma, parse each)
// - Parseable as i64 -> Value::Int
// - Parseable as f64 -> Value::Float
// - Anything else -> Value::Str
fn parse_value(input: &str) -> Value {
    // TODO: Use match with guards and or-patterns
    todo!()
}

// TODO: Write `parse_command` that takes a &str line and returns Option<Command>
// Format: "COMMAND arg1 arg2..."
// - "SET key value" -> Some(Command::Set { key, value: parse_value(value) })
// - "GET key" -> Some(Command::Get { key })
// - "DEL key" -> Some(Command::Delete { key })
// - "LIST" -> Some(Command::List { pattern: None })
// - "LIST pattern" -> Some(Command::List { pattern: Some(pattern) })
// - "QUIT" | "EXIT" | "Q" -> Some(Command::Quit)
// - Anything else -> None
//
// Hint: split the input, collect into a Vec, then match on the slice with [..] patterns
fn parse_command(input: &str) -> Option<Command> {
    // TODO: Use slice patterns like [cmd, key, rest @ ..]
    todo!()
}

// TODO: Write `format_value` that formats a Value for display
// - Int(n) -> "{n} (int)"
// - Float(f) -> "{f} (float)"
// - Str(s) -> "\"{s}\" (string)"
// - Bool(b) -> "{b} (bool)"
// - List(items) -> "[{item1}, {item2}, ...] ({len} items)"
fn format_value(value: &Value) -> String {
    todo!()
}

fn main() {
    let inputs = vec![
        "SET name \"Alice\"",
        "SET age 30",
        "SET pi 3.14159",
        "SET active true",
        "SET tags [rust,programming,systems]",
        "GET name",
        "DEL temp",
        "LIST",
        "LIST user:*",
        "QUIT",
        "INVALID COMMAND HERE TOO MANY ARGS",
        "",
    ];

    for input in inputs {
        print!("Input: {:30} -> ", format!("\"{}\"", input));
        match parse_command(input) {
            Some(Command::Set { key, ref value }) => {
                println!("SET {} = {}", key, format_value(value));
            }
            Some(Command::Get { key }) => {
                println!("GET {}", key);
            }
            Some(Command::Delete { key }) => {
                println!("DEL {}", key);
            }
            Some(Command::List { pattern: Some(p) }) => {
                println!("LIST matching '{}'", p);
            }
            Some(Command::List { pattern: None }) => {
                println!("LIST all");
            }
            Some(Command::Quit) => {
                println!("QUIT");
            }
            None => {
                println!("(unrecognized)");
            }
        }
    }
}
```

Expected output:
```
Input: "SET name "Alice""              -> SET name = "Alice" (string)
Input: "SET age 30"                    -> SET age = 30 (int)
Input: "SET pi 3.14159"               -> SET pi = 3.14159 (float)
Input: "SET active true"              -> SET active = true (bool)
Input: "SET tags [rust,programming,systems]" -> SET tags = [rust, programming, systems] (3 items)
Input: "GET name"                      -> GET name
Input: "DEL temp"                      -> DEL temp
Input: "LIST"                          -> LIST all
Input: "LIST user:*"                   -> LIST matching 'user:*'
Input: "QUIT"                          -> QUIT
Input: "INVALID COMMAND HERE TOO MANY ARGS" -> (unrecognized)
Input: ""                              -> (unrecognized)
```

<details>
<summary>Solution</summary>

```rust
#[derive(Debug)]
enum Value {
    Int(i64),
    Float(f64),
    Str(String),
    Bool(bool),
    List(Vec<Value>),
}

#[derive(Debug)]
enum Command {
    Set { key: String, value: Value },
    Get { key: String },
    Delete { key: String },
    List { pattern: Option<String> },
    Quit,
}

fn parse_value(input: &str) -> Value {
    match input {
        "true" => Value::Bool(true),
        "false" => Value::Bool(false),
        s if s.starts_with('"') && s.ends_with('"') && s.len() >= 2 => {
            Value::Str(s[1..s.len() - 1].to_string())
        }
        s if s.starts_with('[') && s.ends_with(']') => {
            let inner = &s[1..s.len() - 1];
            let items = inner
                .split(',')
                .map(|item| parse_value(item.trim()))
                .collect();
            Value::List(items)
        }
        s if s.parse::<i64>().is_ok() => Value::Int(s.parse().unwrap()),
        s if s.parse::<f64>().is_ok() => Value::Float(s.parse().unwrap()),
        s => Value::Str(s.to_string()),
    }
}

fn parse_command(input: &str) -> Option<Command> {
    let parts: Vec<&str> = input.splitn(3, ' ').collect();
    match parts.as_slice() {
        ["SET", key, value] => Some(Command::Set {
            key: key.to_string(),
            value: parse_value(value),
        }),
        ["GET", key] => Some(Command::Get {
            key: key.to_string(),
        }),
        ["DEL", key] => Some(Command::Delete {
            key: key.to_string(),
        }),
        ["LIST", pattern] => Some(Command::List {
            pattern: Some(pattern.to_string()),
        }),
        ["LIST"] => Some(Command::List { pattern: None }),
        ["QUIT" | "EXIT" | "Q"] => Some(Command::Quit),
        _ => None,
    }
}

fn format_value(value: &Value) -> String {
    match value {
        Value::Int(n) => format!("{} (int)", n),
        Value::Float(f) => format!("{} (float)", f),
        Value::Str(s) => format!("\"{}\" (string)", s),
        Value::Bool(b) => format!("{} (bool)", b),
        Value::List(items) => {
            let formatted: Vec<String> = items.iter().map(|v| match v {
                Value::Str(s) => s.clone(),
                other => format_value(other),
            }).collect();
            format!("[{}] ({} items)", formatted.join(", "), items.len())
        }
    }
}

fn main() {
    let inputs = vec![
        "SET name \"Alice\"",
        "SET age 30",
        "SET pi 3.14159",
        "SET active true",
        "SET tags [rust,programming,systems]",
        "GET name",
        "DEL temp",
        "LIST",
        "LIST user:*",
        "QUIT",
        "INVALID COMMAND HERE TOO MANY ARGS",
        "",
    ];

    for input in inputs {
        print!("Input: {:30} -> ", format!("\"{}\"", input));
        match parse_command(input) {
            Some(Command::Set { key, ref value }) => {
                println!("SET {} = {}", key, format_value(value));
            }
            Some(Command::Get { key }) => {
                println!("GET {}", key);
            }
            Some(Command::Delete { key }) => {
                println!("DEL {}", key);
            }
            Some(Command::List { pattern: Some(p) }) => {
                println!("LIST matching '{}'", p);
            }
            Some(Command::List { pattern: None }) => {
                println!("LIST all");
            }
            Some(Command::Quit) => {
                println!("QUIT");
            }
            None => {
                println!("(unrecognized)");
            }
        }
    }
}
```
</details>

## Common Mistakes

### Mistake 1: Match Guard Shadowing Exhaustiveness

```rust
let x = 5;
match x {
    n if n > 0 => println!("Positive"),
    n if n < 0 => println!("Negative"),
    // Compiler still requires a catch-all — guards don't count for exhaustiveness
}
```

**Fix**: Always add a final arm: `_ => println!("Zero")` or `0 => ...`.

### Mistake 2: Moving Out of a Pattern When You Need a Reference

```rust
let data = vec![String::from("hello")];
for s in data {
    // s is String (moved out of vec) — data is consumed
}
// data is no longer usable!
```

**Fix**: Use `for s in &data` or `for ref s in data` if you need references.

### Mistake 3: Forgetting `..` in Struct Patterns

```rust
struct Config { host: String, port: u16, debug: bool }
let c = Config { host: "x".into(), port: 80, debug: true };

match c {
    Config { host, .. } => println!("{}", host), // .. ignores remaining fields
    // Without .., you must list ALL fields or get a compiler error
}
```

## Verification

```bash
cargo run
```

For each exercise, verify:
1. Does the output match exactly?
2. Try adding a new enum variant — does the compiler warn about non-exhaustive matches?
3. Remove a match guard — does the behavior change?
4. Replace a `matches!` with a full `match` — do they produce the same result?

## What You Learned

- Nested patterns destructure complex data types in a single arm.
- Or-patterns (`|`) reduce code duplication for similar cases.
- Match guards add conditions beyond what patterns alone can express, but do not contribute to exhaustiveness.
- `@` bindings capture values while simultaneously testing them against ranges or patterns.
- Destructuring works in `let`, function parameters, `for` loops, and closures.
- `matches!` provides concise boolean pattern checks ideal for filtering.
- Exhaustiveness checking ensures you handle all possible values.

## What's Next

Exercise 30 covers competitive programming with HashMaps — the entry API, counting patterns, and classic problems like two-sum and group anagrams.

## Resources

- [The Rust Book — Patterns](https://doc.rust-lang.org/book/ch19-00-patterns.html)
- [Rust Reference — Patterns](https://doc.rust-lang.org/reference/patterns.html)
- [matches! macro](https://doc.rust-lang.org/std/macro.matches.html)
- [RFC 2497 — if let chains](https://rust-lang.github.io/rfcs/2497-if-let-chains.html)
