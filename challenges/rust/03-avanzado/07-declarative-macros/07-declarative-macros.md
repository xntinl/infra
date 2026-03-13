# 7. Declarative Macros

**Difficulty**: Avanzado

## Prerequisites
- Completed: Intermedio exercises on generics, traits, and pattern matching
- Familiarity with: `vec!`, `println!`, `assert!`, `cfg!` (you've used macros, now you'll write them)

## Learning Objectives
- Design declarative macros with correct matcher patterns and repetition
- Analyze macro hygiene and its implications for identifier scoping
- Debug macro expansion using `cargo expand` and compiler tools
- Evaluate when macros are appropriate vs generics, traits, or code generation

## Concepts

### macro_rules! Syntax

A declarative macro is a pattern-matching code generator. The compiler matches the input tokens against patterns and expands the corresponding template:

```rust
macro_rules! say_hello {
    () => {
        println!("Hello!");
    };
    ($name:expr) => {
        println!("Hello, {}!", $name);
    };
}

say_hello!();           // Hello!
say_hello!("world");    // Hello, world!
```

Rules are tried top to bottom. The first match wins. Each rule has a **matcher** (left of `=>`) and a **transcriber** (right of `=>`).

### Fragment Specifiers (Designators)

The `$name:specifier` syntax captures a fragment of the input:

| Specifier | Matches | Example |
|-----------|---------|---------|
| `expr` | Any expression | `x + 1`, `foo()`, `if a { b } else { c }` |
| `ty` | A type | `i32`, `Vec<String>`, `&'a str` |
| `ident` | An identifier | `foo`, `MyStruct`, `x` |
| `pat` | A pattern | `Some(x)`, `(a, b)`, `_` |
| `path` | A path | `std::io::Error`, `crate::foo` |
| `stmt` | A statement | `let x = 5;` |
| `block` | A block | `{ let x = 1; x + 2 }` |
| `item` | An item | `fn foo() {}`, `struct Bar;` |
| `tt` | A single token tree | Anything. The catch-all. |
| `literal` | A literal | `42`, `"hello"`, `true` |
| `meta` | Attribute content | `derive(Debug)`, `cfg(test)` |
| `lifetime` | A lifetime | `'a`, `'static` |

`tt` (token tree) is the most flexible -- it matches a single token or a delimited group `(...)`, `[...]`, `{...}`. When nothing else works, use `tt`.

### Repetition

Repetition handles variable-length inputs:

```rust
macro_rules! make_vec {
    ( $( $elem:expr ),* ) => {
        {
            let mut v = Vec::new();
            $( v.push($elem); )*
            v
        }
    };
}

let v = make_vec![1, 2, 3]; // Vec containing 1, 2, 3
```

- `$( ... ),*` -- zero or more, separated by commas
- `$( ... ),+` -- one or more, separated by commas
- `$( ... );*` -- zero or more, separated by semicolons
- `$( ... )*` -- zero or more, no separator

The separator is the token between `)` and the `*`/`+`.

### Nested Repetition

Repetitions can nest, but inner repetitions must use a variable from the outer:

```rust
macro_rules! matrix {
    ( $( [ $( $elem:expr ),* ] ),* ) => {
        vec![ $( vec![ $( $elem ),* ] ),* ]
    };
}

let m = matrix![[1, 2, 3], [4, 5, 6]];
// Vec<Vec<i32>> = [[1,2,3], [4,5,6]]
```

### Macro Hygiene

Rust macros are (partially) hygienic. Variables defined inside a macro don't leak into the caller's scope, and vice versa:

```rust
macro_rules! make_x {
    () => {
        let x = 42; // this x is in the macro's scope
    };
}

fn main() {
    make_x!();
    // println!("{x}"); // ERROR: x is not in scope here
}
```

However, hygiene has limits. If the macro accepts an `$ident`, that identifier lives in the caller's scope:

```rust
macro_rules! declare {
    ($name:ident, $val:expr) => {
        let $name = $val; // $name is in caller's scope
    };
}

fn main() {
    declare!(y, 10);
    println!("{y}"); // works: 10
}
```

This partial hygiene is why Rust macros are safer than C macros but occasionally surprising. If you need a temporary variable inside a macro, use a name that's unlikely to collide, or wrap in a block:

```rust
macro_rules! safe_temp {
    ($val:expr) => {
        {
            let __temp = $val; // block-scoped, doesn't leak
            __temp * 2
        }
    };
}
```

### Debugging Macros

**cargo expand** (install with `cargo install cargo-expand`):
```bash
cargo expand           # expands all macros in the crate
cargo expand main      # expands just fn main
```

This shows the actual code after macro expansion. Essential for debugging.

**Compiler built-ins** (nightly only):
```rust
#![feature(trace_macros, log_syntax)]

trace_macros!(true);
my_macro!(foo, bar);
trace_macros!(false);
```

On stable, `cargo expand` is your best tool.

### Common Patterns

**Reimplementing vec!**:
```rust
macro_rules! my_vec {
    () => { Vec::new() };
    ( $( $elem:expr ),+ $(,)? ) => {
        {
            let mut v = Vec::with_capacity( count!($($elem),+) );
            $( v.push($elem); )+
            v
        }
    };
    ( $elem:expr ; $count:expr ) => {
        vec![$elem; $count] // delegate the repeat syntax
    };
}
```

Note `$(,)?` -- optional trailing comma. Without this, `my_vec![1, 2, 3,]` would fail.

**Builder pattern generation**:
```rust
macro_rules! builder {
    ($name:ident { $( $field:ident : $ty:ty ),* $(,)? }) => {
        struct $name {
            $( $field: Option<$ty>, )*
        }

        impl $name {
            fn new() -> Self {
                Self { $( $field: None, )* }
            }

            $(
                fn $field(mut self, val: $ty) -> Self {
                    self.$field = Some(val);
                    self
                }
            )*
        }
    };
}

builder!(Config {
    host: String,
    port: u16,
    timeout_ms: u64,
});

let cfg = Config::new().host("localhost".into()).port(8080);
```

**Match extensions / enum dispatch**:
```rust
macro_rules! dispatch {
    ($val:expr, $method:ident, $( $variant:ident ),+) => {
        match $val {
            $( Self::$variant(inner) => inner.$method(), )+
        }
    };
}
```

### Macros vs Generics

| Aspect | Macros | Generics |
|--------|--------|----------|
| Type checking | After expansion | During definition |
| Error messages | Often confusing | Clear |
| IDE support | Limited | Full |
| Can generate items | Yes (structs, impls) | No |
| Works across types | Token manipulation | Trait bounds |
| Recursion | Yes (but limited depth) | Via trait impls |

**Use macros when**: you need to generate repetitive struct/impl code, create DSLs, or work at the token level. **Use generics when**: the variation is in types and all variants share the same structure.

## Exercises

### Exercise 1: Typed Configuration Map

**Problem**: Create a macro `config_map!` that generates a strongly-typed configuration struct from a declarative specification:

```rust
config_map!(AppConfig {
    database_url: String = "postgres://localhost/dev",
    port: u16 = 8080,
    debug: bool = false,
    max_connections: usize = 10,
});
```

This should generate:
- A struct with those fields (non-optional, with the given types).
- A `default()` implementation using the specified defaults.
- A `from_env()` method that reads `DATABASE_URL`, `PORT`, `DEBUG`, `MAX_CONNECTIONS` from environment variables (uppercased, underscored), falling back to defaults.

**Hints**:
- You need to convert an ident to an uppercase string. Macros can't do string manipulation -- `from_env` will need the env var name as a separate string literal, or you accept both: `database_url("DATABASE_URL"): String = "..."`.
- For parsing, `str::parse::<$ty>()` works for all standard types.
- Handle parse errors gracefully -- fall back to default on failure, or propagate.

**One possible approach** (accepting explicit env var names):

```rust
macro_rules! config_map {
    ($name:ident {
        $( $field:ident ($env:literal) : $ty:ty = $default:expr ),* $(,)?
    }) => {
        #[derive(Debug, Clone)]
        struct $name {
            $( pub $field: $ty, )*
        }

        impl Default for $name {
            fn default() -> Self {
                Self {
                    $( $field: $default.into(), )*
                }
            }
        }

        impl $name {
            fn from_env() -> Self {
                Self {
                    $(
                        $field: std::env::var($env)
                            .ok()
                            .and_then(|v| v.parse::<$ty>().ok())
                            .unwrap_or_else(|| $default.into()),
                    )*
                }
            }
        }
    };
}

config_map!(AppConfig {
    database_url("DATABASE_URL"): String = "postgres://localhost/dev",
    port("PORT"): u16 = 8080,
    debug("DEBUG"): bool = false,
});

fn main() {
    let config = AppConfig::from_env();
    println!("{config:?}");
}
```

The limitation of requiring explicit env var names is real. Compare this to proc macros (next exercise), which can do string manipulation at compile time.

### Exercise 2: Retry with Backoff DSL

**Problem**: Create a macro that wraps a fallible expression with retry logic:

```rust
let result = retry!(
    attempts: 3,
    delay_ms: 100,
    backoff: exponential,
    {
        connect_to_database(&url)
    }
);
```

Support `backoff: linear` (delay * attempt) and `backoff: exponential` (delay * 2^attempt). The block should return `Result<T, E>`. The macro retries on `Err`, returns the last error if all attempts fail.

**Hints**:
- Use `tt` matching for the backoff strategy, then match on the token in the transcriber.
- `std::thread::sleep` for the synchronous version. Consider an async variant too.
- The hardest part: computing the delay. `match` inside the expansion works.

**One possible solution**:

```rust
macro_rules! retry {
    (
        attempts: $attempts:expr,
        delay_ms: $delay:expr,
        backoff: $strategy:ident,
        $body:block
    ) => {{
        let mut last_err = None;
        for attempt in 0..$attempts {
            match (|| $body)() {
                Ok(val) => { last_err = None; break; }
                Err(e) => {
                    last_err = Some(Err(e));
                    if attempt < $attempts - 1 {
                        let delay_ms = retry!(@delay $strategy, $delay, attempt);
                        std::thread::sleep(std::time::Duration::from_millis(delay_ms));
                    }
                }
            }
        }
        last_err.unwrap_or_else(|| (|| $body)())
    }};
    (@delay linear, $base:expr, $attempt:expr) => {
        $base * ($attempt as u64 + 1)
    };
    (@delay exponential, $base:expr, $attempt:expr) => {
        $base * (1u64 << $attempt as u64)
    };
}
```

Note the internal rules (`@delay`) -- this is the standard pattern for helper arms within a macro. The `@` is just a convention; any unique token works.

### Exercise 3: Macro Limitations Analysis (Design Challenge)

**Problem**: Attempt to implement the following with `macro_rules!`. For each, determine whether it's possible and explain why or why not:

1. A macro that counts the number of arguments passed to it and stores it as a const.
2. A macro that generates `impl Display for MyEnum` that prints variant names as strings.
3. A macro that accepts a struct definition and adds `#[derive(Debug, Clone)]` to it.

For the ones that fail, explain what capability is missing and whether a proc macro could solve it.

This exercise is about understanding boundaries. Not every problem should be solved with `macro_rules!`.

## Design Decisions

**Macro vs trait default methods**: If your "code generation" is just providing a default implementation that varies by a type parameter, a trait with a default method is cleaner and has better error messages.

**Internal rules pattern**: Use `@helper` arms for recursive or computed expansions. This keeps the public interface clean and moves complexity into named internal arms.

**Trailing comma handling**: Always add `$(,)?` at the end of comma-separated repetitions. Users will add trailing commas, and the error message if you don't handle it is cryptic.

## Common Mistakes

1. **Forgetting the outer braces** in the transcriber when the expansion is multiple statements. Without `{ ... }`, the macro expands to a bare statement list that doesn't work in expression position.
2. **Token ambiguity**: `$($x:expr),*` can't be followed by `+` because the parser can't tell if `+` is a separator or an operator. Use `tt` when the grammar is ambiguous.
3. **Recursive depth limits**: `macro_rules!` has a recursion limit (default 128). Deep recursion for counting or computation hits this. Use `#![recursion_limit = "256"]` as a workaround, but prefer proc macros for heavy computation.
4. **Hygiene surprises with method calls**: `$x.foo()` works, but if `$x` is captured as `expr`, it might need parentheses: `($x).foo()` to prevent precedence issues.

## Summary

- `macro_rules!` is pattern matching on token trees, expanded at compile time.
- Fragment specifiers (`expr`, `ty`, `ident`, `tt`, etc.) control what each capture matches.
- Repetition (`$(...)*`) handles variable-length inputs. Nest repetitions for tabular data.
- Macros are partially hygienic -- internal variables don't leak, but captured identifiers live in the caller's scope.
- Use `cargo expand` to debug. It's not optional -- it's essential tooling.
- Macros excel at reducing boilerplate for struct/impl generation and DSLs. They're the wrong tool for logic that depends on type information.

## What's Next

Declarative macros can't inspect types, manipulate strings, or access the AST. Procedural macros can. Next exercise covers derive macros, attribute macros, and the syn/quote ecosystem.

## Resources

- [The Little Book of Rust Macros](https://veykril.github.io/tlborm/)
- [Rust Reference: Macros By Example](https://doc.rust-lang.org/reference/macros-by-example.html)
- [cargo-expand](https://github.com/dtolnay/cargo-expand)
- [Daniel Keep: Macro Patterns](https://danielkeep.github.io/tlborm/book/pat-README.html)
