# 27. CLI Applications with Clap

**Difficulty**: Avanzado

## Prerequisites

- Completed: exercises 01-10 (ownership, traits, error handling, advanced traits)
- Comfortable with derive macros and attribute syntax
- Basic experience writing Rust binaries (not just libraries)

## Learning Objectives

- Build production-grade CLI tools using the clap derive API
- Implement subcommands, argument groups, and value parsers
- Generate shell completions and man pages from the same source of truth
- Integrate CLI arguments with configuration files and environment variables
- Test CLI behavior end-to-end with assert_cmd and predicates

## Concepts

### Clap Derive API

Clap 4.x provides a derive-based API where your CLI interface is defined as Rust structs and enums. The compiler enforces correctness:

```rust
use clap::{Parser, Subcommand, Args, ValueEnum};

#[derive(Parser)]
#[command(name = "mytool", version, about = "A file processing tool")]
struct Cli {
    /// Enable verbose output
    #[arg(short, long, global = true)]
    verbose: bool,

    /// Config file path
    #[arg(short, long, default_value = "config.toml")]
    config: String,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Process input files
    Process(ProcessArgs),
    /// Show current configuration
    Config,
    /// Generate shell completions
    Completions {
        #[arg(value_enum)]
        shell: ShellChoice,
    },
}

#[derive(Args)]
struct ProcessArgs {
    /// Input files to process
    #[arg(required = true)]
    files: Vec<String>,

    /// Output format
    #[arg(short, long, value_enum, default_value_t = Format::Text)]
    format: Format,

    /// Maximum line length (0 = unlimited)
    #[arg(long, default_value_t = 120, value_parser = clap::value_parser!(u32))]
    max_width: u32,
}

#[derive(Clone, ValueEnum)]
enum Format {
    Text,
    Json,
    Csv,
}

#[derive(Clone, ValueEnum)]
enum ShellChoice {
    Bash,
    Zsh,
    Fish,
}
```

Key derive attributes:

| Attribute | Scope | Purpose |
|---|---|---|
| `#[command(...)]` | Struct/Enum | App-level metadata: name, version, about |
| `#[arg(...)]` | Field | Argument behavior: short, long, default, required |
| `#[command(subcommand)]` | Field | Marks field as subcommand enum |
| `#[arg(value_enum)]` | Field | Restricts values to enum variants |
| `#[arg(global = true)]` | Field | Flag available to all subcommands |
| `#[arg(env = "MY_VAR")]` | Field | Fall back to environment variable |

### Positional Args vs Flags vs Options

```rust
#[derive(Parser)]
struct Example {
    /// Positional: mytool input.txt
    file: String,

    /// Flag (boolean): mytool --verbose
    #[arg(short, long)]
    verbose: bool,

    /// Option (takes value): mytool --output out.txt
    #[arg(short, long)]
    output: Option<String>,

    /// Option with default: mytool --jobs 4
    #[arg(short, long, default_value_t = num_cpus())]
    jobs: usize,

    /// Multiple values: mytool --tag foo --tag bar
    #[arg(long)]
    tag: Vec<String>,
}
```

Clap infers the kind from the type:
- `bool` -> flag (no value)
- `Option<T>` -> optional argument
- `Vec<T>` -> repeatable argument
- `T` (bare) -> required argument

### Custom Value Parsers

For validation beyond type parsing:

```rust
use std::path::PathBuf;

fn parse_existing_file(s: &str) -> Result<PathBuf, String> {
    let path = PathBuf::from(s);
    if path.exists() && path.is_file() {
        Ok(path)
    } else {
        Err(format!("file does not exist: {}", s))
    }
}

#[derive(Parser)]
struct Cli {
    /// Input file (must exist)
    #[arg(value_parser = parse_existing_file)]
    input: PathBuf,

    /// Port number (1024-65535)
    #[arg(long, value_parser = clap::value_parser!(u16).range(1024..))]
    port: u16,
}
```

### Shell Completions with clap_complete

Generate completions at build time or runtime from the same `Parser` definition:

```rust
use clap::CommandFactory;
use clap_complete::{generate, Shell};

fn print_completions(shell: Shell) {
    let mut cmd = Cli::command();
    generate(shell, &mut cmd, "mytool", &mut std::io::stdout());
}

// Or in build.rs for compile-time generation:
// clap_complete::generate_to(Shell::Bash, &mut Cli::command(), "mytool", "completions/")
```

### Man Page Generation with clap_mangen

```rust
// build.rs
fn main() {
    let cmd = Cli::command();
    let man = clap_mangen::Man::new(cmd);
    let mut buf = Vec::new();
    man.render(&mut buf).expect("failed to render man page");
    std::fs::write("target/mytool.1", buf).expect("failed to write man page");
}
```

### Environment Variable Integration

Clap supports env vars natively, but for layered config (file + env + CLI), combine with the `config` crate:

```rust
#[derive(Parser)]
struct Cli {
    /// Server port
    #[arg(long, env = "APP_PORT", default_value_t = 8080)]
    port: u16,

    /// Database URL
    #[arg(long, env = "DATABASE_URL")]
    database_url: Option<String>,
}
```

Priority order (highest wins): CLI argument > environment variable > config file > default value. Clap handles the first three natively with `env`. For config file defaults, parse the file first and use the values as programmatic defaults.

### Colored Output with owo-colors

```rust
use owo_colors::OwoColorize;

fn print_result(success: bool, message: &str) {
    if success {
        println!("{} {}", "OK".green().bold(), message);
    } else {
        eprintln!("{} {}", "ERROR".red().bold(), message);
    }
}

// Respect NO_COLOR environment variable
fn should_color() -> bool {
    std::env::var("NO_COLOR").is_err()
}
```

### Testing CLIs with assert_cmd

`assert_cmd` runs your binary as a subprocess and asserts on exit code, stdout, and stderr:

```rust
// tests/integration.rs
use assert_cmd::Command;
use predicates::prelude::*;

#[test]
fn test_help_flag() {
    Command::cargo_bin("mytool")
        .unwrap()
        .arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("A file processing tool"));
}

#[test]
fn test_missing_required_arg() {
    Command::cargo_bin("mytool")
        .unwrap()
        .arg("process")
        // No files provided -- should fail
        .assert()
        .failure()
        .stderr(predicate::str::contains("required"));
}

#[test]
fn test_process_json_output() {
    Command::cargo_bin("mytool")
        .unwrap()
        .args(["process", "--format", "json", "testdata/sample.txt"])
        .assert()
        .success()
        .stdout(predicate::str::starts_with("{"));
}
```

## Exercises

### Exercise 1: File Processing CLI

Build a CLI tool called `fproc` with the following interface:

```
fproc [OPTIONS] <COMMAND>

Commands:
  count    Count lines, words, or characters
  search   Search for a pattern in files
  convert  Convert between formats (text/json/csv)
  completions  Generate shell completions

Options:
  -v, --verbose   Enable verbose output
  -c, --config    Config file path [default: fproc.toml]
  -h, --help      Print help
  -V, --version   Print version
```

The `count` subcommand: `fproc count [--mode lines|words|chars] <FILE>...`
The `search` subcommand: `fproc search [--ignore-case] <PATTERN> <FILE>...`
The `convert` subcommand: `fproc convert --from <FMT> --to <FMT> <FILE>`

Requirements:
- Use `ValueEnum` for format and mode enums
- Custom `value_parser` that validates files exist
- `--verbose` is global across all subcommands
- Support `APP_CONFIG` environment variable for config path

**Cargo.toml:**
```toml
[package]
name = "fproc"
edition = "2021"

[dependencies]
clap = { version = "4.5", features = ["derive", "env"] }
clap_complete = "4.5"
owo-colors = "4.2"
serde = { version = "1", features = ["derive"] }
serde_json = "1"
csv = "1.3"

[dev-dependencies]
assert_cmd = "2.0"
predicates = "3.1"
tempfile = "3"
```

**Hints:**
- Derive `Parser` on the root struct, `Subcommand` on the commands enum, `Args` on subcommand args
- Use `#[arg(global = true)]` for `--verbose`
- The `count` subcommand reads files and counts based on mode
- Test with `assert_cmd::Command::cargo_bin("fproc")`

<details>
<summary>Solution</summary>

```rust
use clap::{CommandFactory, Parser, Subcommand, Args, ValueEnum};
use clap_complete::{generate, Shell};
use owo_colors::OwoColorize;
use std::path::PathBuf;
use std::fs;

fn parse_existing_path(s: &str) -> Result<PathBuf, String> {
    let p = PathBuf::from(s);
    if p.exists() {
        Ok(p)
    } else {
        Err(format!("path does not exist: {s}"))
    }
}

#[derive(Parser)]
#[command(name = "fproc", version, about = "A file processing tool")]
struct Cli {
    /// Enable verbose output
    #[arg(short, long, global = true)]
    verbose: bool,

    /// Config file path
    #[arg(short, long, env = "APP_CONFIG", default_value = "fproc.toml")]
    config: String,

    #[command(subcommand)]
    command: Commands,
}

#[derive(Subcommand)]
enum Commands {
    /// Count lines, words, or characters
    Count(CountArgs),
    /// Search for a pattern in files
    Search(SearchArgs),
    /// Convert between formats
    Convert(ConvertArgs),
    /// Generate shell completions
    Completions {
        #[arg(value_enum)]
        shell: Shell,
    },
}

#[derive(Args)]
struct CountArgs {
    /// What to count
    #[arg(short, long, value_enum, default_value_t = CountMode::Lines)]
    mode: CountMode,

    /// Files to process
    #[arg(required = true, value_parser = parse_existing_path)]
    files: Vec<PathBuf>,
}

#[derive(Args)]
struct SearchArgs {
    /// Case-insensitive search
    #[arg(short, long)]
    ignore_case: bool,

    /// Pattern to search for
    pattern: String,

    /// Files to search
    #[arg(required = true, value_parser = parse_existing_path)]
    files: Vec<PathBuf>,
}

#[derive(Args)]
struct ConvertArgs {
    /// Source format
    #[arg(long, value_enum)]
    from: Format,

    /// Target format
    #[arg(long, value_enum)]
    to: Format,

    /// Input file
    #[arg(value_parser = parse_existing_path)]
    file: PathBuf,
}

#[derive(Clone, ValueEnum)]
enum CountMode {
    Lines,
    Words,
    Chars,
}

#[derive(Clone, ValueEnum)]
enum Format {
    Text,
    Json,
    Csv,
}

fn cmd_count(args: &CountArgs, verbose: bool) {
    for file in &args.files {
        let content = fs::read_to_string(file).unwrap_or_else(|e| {
            eprintln!("{} reading {}: {e}", "ERROR".red().bold(), file.display());
            std::process::exit(1);
        });

        let count = match args.mode {
            CountMode::Lines => content.lines().count(),
            CountMode::Words => content.split_whitespace().count(),
            CountMode::Chars => content.chars().count(),
        };

        let label = match args.mode {
            CountMode::Lines => "lines",
            CountMode::Words => "words",
            CountMode::Chars => "chars",
        };

        if verbose {
            println!("{}: {} {} ({})", file.display(), count, label,
                     format!("{} bytes", content.len()).dimmed());
        } else {
            println!("{}\t{}", count, file.display());
        }
    }
}

fn cmd_search(args: &SearchArgs, verbose: bool) {
    let mut total_matches = 0usize;

    for file in &args.files {
        let content = fs::read_to_string(file).unwrap_or_else(|e| {
            eprintln!("{} reading {}: {e}", "ERROR".red().bold(), file.display());
            std::process::exit(1);
        });

        for (line_num, line) in content.lines().enumerate() {
            let matches = if args.ignore_case {
                line.to_lowercase().contains(&args.pattern.to_lowercase())
            } else {
                line.contains(&args.pattern)
            };

            if matches {
                total_matches += 1;
                println!(
                    "{}:{}:{}",
                    file.display().to_string().green(),
                    (line_num + 1).to_string().yellow(),
                    line
                );
            }
        }
    }

    if verbose {
        eprintln!("{} matches found", total_matches);
    }
}

fn cmd_convert(args: &ConvertArgs, _verbose: bool) {
    let content = fs::read_to_string(&args.file).unwrap_or_else(|e| {
        eprintln!("{} reading {}: {e}", "ERROR".red().bold(), args.file.display());
        std::process::exit(1);
    });

    // Simplified: read lines as records, convert between formats
    let records: Vec<Vec<String>> = content
        .lines()
        .map(|line| line.split('\t').map(String::from).collect())
        .collect();

    match args.to {
        Format::Json => {
            let json = serde_json::to_string_pretty(&records).unwrap();
            println!("{json}");
        }
        Format::Csv => {
            for record in &records {
                println!("{}", record.join(","));
            }
        }
        Format::Text => {
            for record in &records {
                println!("{}", record.join("\t"));
            }
        }
    }
}

fn main() {
    let cli = Cli::parse();

    match &cli.command {
        Commands::Count(args) => cmd_count(args, cli.verbose),
        Commands::Search(args) => cmd_search(args, cli.verbose),
        Commands::Convert(args) => cmd_convert(args, cli.verbose),
        Commands::Completions { shell } => {
            generate(*shell, &mut Cli::command(), "fproc", &mut std::io::stdout());
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use clap::Parser;

    #[test]
    fn verify_cli() {
        // clap's built-in verification
        Cli::command().debug_assert();
    }

    #[test]
    fn parse_count_command() {
        // Simulates: fproc count --mode words Cargo.toml
        let cli = Cli::try_parse_from([
            "fproc", "count", "--mode", "words", "Cargo.toml",
        ]).unwrap();

        assert!(matches!(cli.command, Commands::Count(_)));
    }

    #[test]
    fn global_verbose_propagates() {
        let cli = Cli::try_parse_from([
            "fproc", "--verbose", "count", "Cargo.toml",
        ]).unwrap();

        assert!(cli.verbose);
    }

    #[test]
    fn invalid_file_rejected() {
        let result = Cli::try_parse_from([
            "fproc", "count", "nonexistent_file_xyz.txt",
        ]);

        assert!(result.is_err());
    }
}
```

Integration tests (in `tests/cli.rs`):

```rust
use assert_cmd::Command;
use predicates::prelude::*;
use tempfile::NamedTempFile;
use std::io::Write;

#[test]
fn test_help() {
    Command::cargo_bin("fproc")
        .unwrap()
        .arg("--help")
        .assert()
        .success()
        .stdout(predicate::str::contains("A file processing tool"));
}

#[test]
fn test_count_lines() {
    let mut f = NamedTempFile::new().unwrap();
    writeln!(f, "line one").unwrap();
    writeln!(f, "line two").unwrap();
    writeln!(f, "line three").unwrap();

    Command::cargo_bin("fproc")
        .unwrap()
        .args(["count", "--mode", "lines"])
        .arg(f.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("3"));
}

#[test]
fn test_search_case_insensitive() {
    let mut f = NamedTempFile::new().unwrap();
    writeln!(f, "Hello World").unwrap();
    writeln!(f, "goodbye world").unwrap();

    Command::cargo_bin("fproc")
        .unwrap()
        .args(["search", "--ignore-case", "hello"])
        .arg(f.path())
        .assert()
        .success()
        .stdout(predicate::str::contains("Hello World"));
}

#[test]
fn test_no_subcommand_shows_help() {
    Command::cargo_bin("fproc")
        .unwrap()
        .assert()
        .failure()
        .stderr(predicate::str::contains("Usage"));
}

#[test]
fn test_env_config() {
    Command::cargo_bin("fproc")
        .unwrap()
        .env("APP_CONFIG", "/custom/path.toml")
        .arg("--help")
        .assert()
        .success();
}
```

**Trade-off analysis:**

| Approach | Pros | Cons |
|---|---|---|
| Derive API | Type-safe, discoverable, doc comments become help text | Compile time cost, less flexible than builder |
| Builder API | Maximum control, dynamic construction | Verbose, error-prone, no compile-time checks |
| Arg groups | Mutual exclusion, dependency constraints | Adds complexity, harder to understand |
| value_parser | Validation at parse time, good error messages | Must write custom functions for complex rules |
| assert_cmd testing | True end-to-end, catches argument parsing bugs | Slow (subprocess per test), requires cargo build |
| Unit test with try_parse_from | Fast, tests parsing logic | Does not test actual binary behavior |

</details>

### Exercise 2: Config File + CLI Layered Defaults

Extend the CLI from Exercise 1 so that:
1. If `fproc.toml` (or `--config` path) exists, load defaults from it
2. Environment variables override config file values
3. CLI arguments override everything

The config file format:
```toml
verbose = false
default_format = "json"
max_width = 100
```

**Hints:**
- Parse the config file first with `toml` + `serde`
- Use clap's `default_value_t` with values from the config struct
- Alternatively, parse CLI first, then merge: `cli_value.unwrap_or(config_value)`

<details>
<summary>Solution</summary>

```rust
use serde::Deserialize;
use std::path::Path;

#[derive(Deserialize, Default)]
struct FileConfig {
    verbose: Option<bool>,
    default_format: Option<String>,
    max_width: Option<u32>,
}

impl FileConfig {
    fn load(path: &str) -> Self {
        let p = Path::new(path);
        if p.exists() {
            let content = std::fs::read_to_string(p).unwrap_or_default();
            toml::from_str(&content).unwrap_or_default()
        } else {
            Self::default()
        }
    }
}

// After parsing CLI:
fn merge_config(cli: &Cli) -> MergedConfig {
    let file_cfg = FileConfig::load(&cli.config);

    MergedConfig {
        verbose: cli.verbose || file_cfg.verbose.unwrap_or(false),
        max_width: if cli_has_explicit_max_width() {
            // CLI was explicitly set
            cli.max_width
        } else {
            file_cfg.max_width.unwrap_or(120)
        },
    }
}

// The challenge: clap does not distinguish "user passed --max-width 120"
// from "default_value_t = 120 was used". To detect explicit CLI args,
// use Option<u32> in the struct and treat None as "not provided":

#[derive(Parser)]
struct Cli {
    /// Maximum line width
    #[arg(long)]
    max_width: Option<u32>,  // None = not on CLI, Some = explicit
}

// Then: cli.max_width.or(file_cfg.max_width).unwrap_or(120)
```

**Key insight:** Use `Option<T>` for CLI fields that have config-file defaults. Clap's `default_value_t` makes it impossible to distinguish "user passed the default" from "default was applied". With `Option<T>`, you get a clean three-layer merge:

```
final_value = cli_arg          // highest priority
    .or(env_var)               // middle priority
    .or(config_file_value)     // lower priority
    .unwrap_or(hardcoded)      // fallback
```

</details>

## Common Mistakes

1. **Using `String` instead of `PathBuf` for file arguments.** `PathBuf` works with value_parser and integrates with `std::fs` without conversion.

2. **Not using `try_parse_from` in tests.** `Cli::parse()` calls `std::process::exit` on error, killing your test process. Always use `try_parse_from` in unit tests.

3. **Forgetting `#[command(subcommand)]` on the field.** Without this attribute, clap treats the enum as a regular argument, producing confusing errors.

4. **Putting `default_value_t` on `Option<T>`.** This defeats the purpose of `Option`. If the field has a default, it is always `Some`. Use bare `T` with `default_value_t`, or `Option<T>` without it.

5. **Not running `Cli::command().debug_assert()` in tests.** This catches invalid clap configurations (conflicting args, missing help text) at test time rather than runtime.

## Verification

- `cargo run -- --help` shows formatted help
- `cargo run -- count --mode words Cargo.toml` counts words
- `cargo run -- completions bash` outputs bash completions
- `cargo test` passes unit and integration tests
- `cargo clippy -- -W clippy::all` produces no warnings

## Summary

Clap's derive API turns your CLI interface into a type-safe Rust struct hierarchy. Subcommands map to enums, argument types to fields, and documentation comments to help text. Shell completions and man pages are generated from the same definition. For testing, `assert_cmd` provides true end-to-end coverage while `try_parse_from` enables fast unit tests. The layered config pattern (CLI > env > file > default) requires `Option<T>` fields to distinguish explicit values from defaults.

## Resources

- [clap documentation](https://docs.rs/clap/4.5)
- [clap derive tutorial](https://docs.rs/clap/latest/clap/_derive/_tutorial/index.html)
- [clap_complete documentation](https://docs.rs/clap_complete)
- [clap_mangen documentation](https://docs.rs/clap_mangen)
- [assert_cmd documentation](https://docs.rs/assert_cmd/2.0)
- [predicates documentation](https://docs.rs/predicates/3.1)
- [owo-colors documentation](https://docs.rs/owo-colors/4.2)
- [Command Line Applications in Rust (book)](https://rust-cli.github.io/book/)
