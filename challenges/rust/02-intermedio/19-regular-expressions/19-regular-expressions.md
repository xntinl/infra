# 19. Regular Expressions

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (strings, ownership, error handling basics)
- Completed: 07-error-handling-patterns, 10-cargo-and-dependencies
- Completed: 18-file-io-and-filesystem (for the log analyzer exercise)
- Familiar with iterators, closures, and `HashMap`

## Learning Objectives

- Use the `regex` crate for pattern matching, captures, and replacements
- Compile regular expressions efficiently with `LazyLock` to avoid repeated compilation
- Extract structured data from text using named and unnamed capture groups
- Apply `RegexSet` to match multiple patterns simultaneously
- Build a practical log analyzer combining regex with file I/O

## Concepts

### The `regex` Crate

Rust's standard library does not include regular expressions. The `regex` crate is the de facto standard. Add it to `Cargo.toml`:

```toml
[dependencies]
regex = "1"
```

Basic usage:

```rust
use regex::Regex;

let re = Regex::new(r"\d{3}-\d{4}").unwrap();

// Check if a string matches:
assert!(re.is_match("Call 555-1234 now"));

// Find the first match:
if let Some(m) = re.find("Call 555-1234 or 555-5678") {
    println!("Found: {} at [{}, {})", m.as_str(), m.start(), m.end());
    // Found: 555-1234 at [5, 13)
}

// Find all matches:
for m in re.find_iter("555-1234 and 555-5678") {
    println!("Match: {}", m.as_str());
}
// Match: 555-1234
// Match: 555-5678
```

The `r"..."` raw string literal is important: it avoids having to double-escape backslashes. Without it, `\d` would need to be written as `\\d`.

### Compilation Cost and `LazyLock`

`Regex::new` compiles the pattern into an internal automaton. This is an expensive operation (microseconds to milliseconds). If you use the same pattern repeatedly, compile it once:

```rust
// WRONG: Recompiles on every call
fn is_email(s: &str) -> bool {
    let re = Regex::new(r"^[\w.+-]+@[\w-]+\.[\w.]+$").unwrap();
    re.is_match(s)
}

// RIGHT: Compile once with LazyLock (standard library, Rust 1.80+)
use std::sync::LazyLock;
use regex::Regex;

static EMAIL_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"^[\w.+-]+@[\w-]+\.[\w.]+$").unwrap()
});

fn is_email(s: &str) -> bool {
    EMAIL_RE.is_match(s)
}
```

`LazyLock` initializes the regex on first access and caches it for all subsequent uses. It is thread-safe and requires no external crate.

For Rust versions before 1.80, the `once_cell` crate provides `Lazy`:

```rust
use once_cell::sync::Lazy;

static EMAIL_RE: Lazy<Regex> = Lazy::new(|| {
    Regex::new(r"^[\w.+-]+@[\w-]+\.[\w.]+$").unwrap()
});
```

### Capture Groups

Parentheses create capture groups:

```rust
let re = Regex::new(r"(\d{4})-(\d{2})-(\d{2})").unwrap();
let text = "Date: 2026-03-13";

if let Some(caps) = re.captures(text) {
    println!("Full match: {}", &caps[0]);  // 2026-03-13
    println!("Year:  {}", &caps[1]);       // 2026
    println!("Month: {}", &caps[2]);       // 03
    println!("Day:   {}", &caps[3]);       // 13
}
```

### Named Capture Groups

Named groups make code self-documenting:

```rust
let re = Regex::new(r"(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})").unwrap();

if let Some(caps) = re.captures("Date: 2026-03-13") {
    println!("Year:  {}", &caps["year"]);   // 2026
    println!("Month: {}", &caps["month"]);  // 03
    println!("Day:   {}", &caps["day"]);    // 13
}
```

The syntax is `(?P<name>pattern)`. Access with `&caps["name"]` or `caps.name("name")`.

### Iterating Captures

`captures_iter` yields all matches with their capture groups:

```rust
let re = Regex::new(r"(?P<name>\w+)=(?P<value>\w+)").unwrap();
let text = "color=red size=large weight=heavy";

for caps in re.captures_iter(text) {
    println!("{} => {}", &caps["name"], &caps["value"]);
}
// color => red
// size => large
// weight => heavy
```

### Replacement

```rust
let re = Regex::new(r"\b(\w)").unwrap();

// Replace first match:
let result = re.replace("hello world", "[$1]");
println!("{result}"); // [h]ello world

// Replace all matches:
let result = re.replace_all("hello world", "[$1]");
println!("{result}"); // [h]ello [w]orld

// Replace with named groups:
let re = Regex::new(r"(?P<last>\w+), (?P<first>\w+)").unwrap();
let result = re.replace_all("Doe, John and Smith, Jane", "$first $last");
println!("{result}"); // John Doe and Jane Smith

// Replace with a closure for dynamic replacements:
let re = Regex::new(r"\d+").unwrap();
let result = re.replace_all("price: 10, tax: 2", |caps: &regex::Captures| {
    let num: i32 = caps[0].parse().unwrap();
    (num * 2).to_string()
});
println!("{result}"); // price: 20, tax: 4
```

### `RegexSet` for Multiple Patterns

When you need to check a string against multiple patterns, `RegexSet` is more efficient than running each regex separately:

```rust
use regex::RegexSet;

let set = RegexSet::new(&[
    r"\d{3}-\d{4}",        // phone
    r"[\w.]+@[\w.]+",      // email
    r"https?://\S+",       // URL
    r"\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}", // IP
]).unwrap();

let text = "Contact: alice@example.com or 555-1234";
let matches: Vec<usize> = set.matches(text).into_iter().collect();
println!("Matched patterns: {matches:?}"); // [0, 1] (phone and email)

// Check specific patterns:
let results = set.matches(text);
if results.matched(1) {
    println!("Contains an email address");
}
```

`RegexSet` compiles all patterns together and evaluates them in a single pass. It tells you which patterns matched, but it does not give you the match positions or capture groups. For those, you need to run the individual regex after identifying which patterns matched.

### Common Regex Syntax Reference

| Pattern | Meaning |
|---|---|
| `.` | Any character except newline |
| `\d` | Digit `[0-9]` |
| `\w` | Word character `[a-zA-Z0-9_]` |
| `\s` | Whitespace |
| `\b` | Word boundary |
| `^` / `$` | Start / end of line (or string) |
| `*` | 0 or more (greedy) |
| `+` | 1 or more (greedy) |
| `?` | 0 or 1 |
| `*?` / `+?` | Non-greedy versions |
| `{n}` / `{n,m}` | Exactly n / between n and m |
| `(...)` | Capture group |
| `(?P<name>...)` | Named capture group |
| `(?:...)` | Non-capturing group |
| `[abc]` / `[^abc]` | Character class / negated |
| `a\|b` | Alternation |

### Anti-Pattern: Compiling Regex in a Loop

```rust
// WRONG: Compiles regex 1000 times
for line in lines {
    let re = Regex::new(r"\d+").unwrap(); // compiled every iteration!
    if re.is_match(&line) { /* ... */ }
}

// RIGHT: Compile once before the loop
let re = Regex::new(r"\d+").unwrap();
for line in lines {
    if re.is_match(&line) { /* ... */ }
}

// BEST: For module-level reuse, use LazyLock
static NUM_RE: LazyLock<Regex> = LazyLock::new(|| Regex::new(r"\d+").unwrap());
```

### Anti-Pattern: Regex When String Methods Suffice

```rust
// OVERKILL: Regex for simple substring check
let re = Regex::new("error").unwrap();
if re.is_match(line) { /* ... */ }

// BETTER: Use str methods
if line.contains("error") { /* ... */ }

// OVERKILL: Regex for starts_with
let re = Regex::new(r"^ERROR").unwrap();

// BETTER:
if line.starts_with("ERROR") { /* ... */ }
```

Use regex when you need pattern matching, repetition, character classes, or captures. For fixed strings, `str` methods are simpler and faster.

## Exercises

### Exercise 1: Pattern Matching Basics

Practice fundamental regex operations: matching, finding, and iterating.

```rust
// Add to Cargo.toml: regex = "1"
use regex::Regex;

fn main() {
    // --- Part A: Validation ---

    // TODO: Create a regex that matches a valid US phone number.
    // Formats: 555-1234, (555) 123-4567, 555.123.4567, +1-555-123-4567
    // Hint: Start simple, e.g., r"\d{3}[-.]?\d{3}[-.]?\d{4}"
    //
    // let phone_re = Regex::new(todo!()).unwrap();

    let test_phones = [
        ("555-1234", false),        // too short for full match
        ("555-123-4567", true),
        ("(555) 123-4567", true),
        ("555.123.4567", true),
        ("+1-555-123-4567", true),
        ("not-a-phone", false),
        ("123456789012345", false),
    ];

    println!("Phone validation:");
    for (input, expected) in &test_phones {
        // let matched = phone_re.is_match(input);
        // let status = if matched == *expected { "OK" } else { "FAIL" };
        // println!("  {status}: '{input}' -> {matched} (expected {expected})");
    }

    // --- Part B: Extraction ---

    // TODO: Create a regex that extracts all IPv4 addresses from text.
    // Pattern: 4 groups of 1-3 digits separated by dots.
    //
    // let ip_re = Regex::new(todo!()).unwrap();

    let server_log = "Connection from 192.168.1.100 to 10.0.0.1 \
                      via gateway 172.16.0.1, rejected by 192.168.1.254";

    // TODO: Use find_iter to extract all IPs:
    // println!("\nIP addresses found:");
    // for m in ip_re.find_iter(server_log) {
    //     println!("  {}", m.as_str());
    // }
    // Expected: 192.168.1.100, 10.0.0.1, 172.16.0.1, 192.168.1.254

    // --- Part C: Counting and Aggregation ---

    // TODO: Create a regex to find all words (sequences of alphabetic characters).
    // Count total words and find the longest word.
    //
    // let word_re = Regex::new(todo!()).unwrap();

    let paragraph = "The quick brown fox jumps over the lazy dog. \
                     The extraordinary circumstances required immediate attention.";

    // TODO: Find all words, count them, find the longest:
    // let words: Vec<&str> = word_re.find_iter(paragraph)
    //     .map(|m| m.as_str())
    //     .collect();
    // println!("\nWord count: {}", words.len());
    // let longest = words.iter().max_by_key(|w| w.len()).unwrap();
    // println!("Longest word: {longest}");

    // --- Part D: Splitting ---

    // TODO: Use Regex to split a string on multiple delimiters.
    // Split "one,two;three:four five" on commas, semicolons, colons, or spaces.
    //
    // let delim_re = Regex::new(todo!()).unwrap();
    // let parts: Vec<&str> = delim_re.split("one,two;three:four five").collect();
    // println!("\nSplit result: {parts:?}");
    // Expected: ["one", "two", "three", "four", "five"]
}
```

### Exercise 2: Capture Groups and Data Extraction

Parse structured text using capture groups.

```rust
use regex::Regex;
use std::collections::HashMap;
use std::sync::LazyLock;

// TODO: Define these as LazyLock statics to avoid recompilation.
// Hint:
// static DATE_RE: LazyLock<Regex> = LazyLock::new(|| {
//     Regex::new(r"(?P<year>\d{4})-(?P<month>\d{2})-(?P<day>\d{2})").unwrap()
// });

// TODO: Define a regex for parsing log lines:
// Format: "2026-03-13T10:30:45 [INFO] Request processed in 42ms"
// Groups: timestamp, level, message
//
// static LOG_RE: LazyLock<Regex> = LazyLock::new(|| {
//     Regex::new(todo!()).unwrap()
// });

// TODO: Define a regex for parsing key=value pairs:
// Examples: "name=Alice", "age=30", "city=\"New York\""
// Handle both quoted and unquoted values.
//
// static KV_RE: LazyLock<Regex> = LazyLock::new(|| {
//     Regex::new(todo!()).unwrap()
// });

#[derive(Debug)]
struct LogEntry {
    timestamp: String,
    level: String,
    message: String,
}

// TODO: Implement parse_log_line that extracts fields from a log line.
// Return None if the line doesn't match the expected format.
//
// fn parse_log_line(line: &str) -> Option<LogEntry> {
//     let caps = LOG_RE.captures(line)?;
//     Some(LogEntry {
//         timestamp: caps["timestamp"].to_string(),
//         level: todo!(),
//         message: todo!(),
//     })
// }

// TODO: Implement parse_key_values that extracts all key=value pairs from text.
// Return a HashMap<String, String>.
//
// fn parse_key_values(text: &str) -> HashMap<String, String> {
//     let mut map = HashMap::new();
//     for caps in KV_RE.captures_iter(text) {
//         let key = caps["key"].to_string();
//         // Value might be in the "quoted" or "unquoted" group
//         let value = caps.name("quoted")
//             .or(caps.name("unquoted"))
//             .map(|m| m.as_str().to_string())
//             .unwrap_or_default();
//         map.insert(key, value);
//     }
//     map
// }

// TODO: Implement extract_urls that finds all URLs in text.
// Pattern: http(s)://followed-by-non-whitespace
//
// fn extract_urls(text: &str) -> Vec<&str> {
//     static URL_RE: LazyLock<Regex> = LazyLock::new(|| {
//         Regex::new(r"https?://\S+").unwrap()
//     });
//     URL_RE.find_iter(text).map(|m| m.as_str()).collect()
// }

fn main() {
    // Parse dates:
    let text = "Events: 2026-03-13, 2026-04-01, and 2025-12-25";
    println!("Dates found:");
    for caps in DATE_RE.captures_iter(text) {
        println!("  {}-{}-{}", &caps["year"], &caps["month"], &caps["day"]);
    }

    // Parse log lines:
    let log_lines = [
        "2026-03-13T10:30:45 [INFO] Request processed in 42ms",
        "2026-03-13T10:30:46 [ERROR] Database connection failed",
        "2026-03-13T10:30:47 [WARN] Cache miss for key user_123",
        "This is not a valid log line",
    ];

    println!("\nParsed log entries:");
    for line in &log_lines {
        match parse_log_line(line) {
            Some(entry) => println!("  [{:5}] {} - {}", entry.level, entry.timestamp, entry.message),
            None => println!("  SKIP: {line}"),
        }
    }

    // Parse key-value pairs:
    let config = r#"host=localhost port=8080 name="My App" debug=true path="/var/log""#;
    let kv = parse_key_values(config);
    println!("\nKey-value pairs:");
    for (k, v) in &kv {
        println!("  {k} = {v}");
    }

    // Extract URLs:
    let post = "Check out https://rust-lang.org and http://crates.io/crates/regex for more info.";
    let urls = extract_urls(post);
    println!("\nURLs found:");
    for url in &urls {
        println!("  {url}");
    }
}
```

### Exercise 3: Search and Replace

Use regex replacements to transform text.

```rust
use regex::Regex;
use std::sync::LazyLock;

fn main() {
    // --- Part A: Simple replacements ---

    // TODO: Redact phone numbers in text.
    // Replace any sequence matching a phone pattern with "[REDACTED]".
    //
    // let phone_re = Regex::new(r"\d{3}[-.]?\d{3}[-.]?\d{4}").unwrap();
    // let text = "Call me at 555-123-4567 or 555.987.6543 for details.";
    // let redacted = phone_re.replace_all(text, "[REDACTED]");
    // println!("Redacted: {redacted}");
    // Expected: "Call me at [REDACTED] or [REDACTED] for details."

    // --- Part B: Replacement with captures ---

    // TODO: Reformat dates from MM/DD/YYYY to YYYY-MM-DD.
    // Use capture groups and backreferences ($1, $2, $3).
    //
    // let date_re = Regex::new(r"(\d{2})/(\d{2})/(\d{4})").unwrap();
    // let text = "Dates: 03/13/2026, 12/25/2025, 01/01/2027";
    // let reformatted = date_re.replace_all(text, todo!()); // use "$3-$1-$2"
    // println!("Reformatted: {reformatted}");
    // Expected: "Dates: 2026-03-13, 2025-12-25, 2027-01-01"

    // --- Part C: Replacement with a closure ---

    // TODO: Convert all temperatures from Fahrenheit to Celsius.
    // Pattern: "72F" or "72 F" or "72°F"
    // Formula: C = (F - 32) * 5 / 9
    //
    // let temp_re = Regex::new(r"(\d+)\s*°?F\b").unwrap();
    // let text = "Today: 72F, tomorrow: 85°F, next week: 60 F";
    // let converted = temp_re.replace_all(text, |caps: &regex::Captures| {
    //     let f: f64 = caps[1].parse().unwrap();
    //     let c = todo!(); // compute Celsius
    //     format!("{c:.1}°C")
    // });
    // println!("Converted: {converted}");
    // Expected: "Today: 22.2°C, tomorrow: 29.4°C, next week: 15.6°C"

    // --- Part D: Clean up whitespace ---

    // TODO: Normalize whitespace in messy text:
    //   1. Replace multiple spaces/tabs with a single space
    //   2. Remove leading/trailing whitespace from each line
    //   3. Remove blank lines
    //
    // let multi_space = Regex::new(r"[ \t]+").unwrap();
    // let blank_lines = Regex::new(r"\n\s*\n").unwrap();
    //
    // let messy = "  Hello    world  \n\n\n  This  is    messy   \n\n  text  ";
    // let clean = multi_space.replace_all(messy.trim(), " ");
    // let clean = blank_lines.replace_all(&clean, "\n");
    // println!("Clean: '{clean}'");

    // --- Part E: Markdown link to HTML ---

    // TODO: Convert Markdown links [text](url) to HTML <a href="url">text</a>.
    // Use named capture groups.
    //
    // let md_link = Regex::new(r"\[(?P<text>[^\]]+)\]\((?P<url>[^)]+)\)").unwrap();
    // let markdown = "Visit [Rust](https://rust-lang.org) or [Crates](https://crates.io).";
    // let html = md_link.replace_all(markdown, todo!()); // "<a href=\"$url\">$text</a>"
    // println!("HTML: {html}");
    // Expected: Visit <a href="https://rust-lang.org">Rust</a> or <a href="https://crates.io">Crates</a>.
}
```

### Exercise 4: `RegexSet` for Classification

Use `RegexSet` to classify log messages into categories.

```rust
use regex::{Regex, RegexSet};
use std::collections::HashMap;
use std::sync::LazyLock;

#[derive(Debug, Clone, PartialEq, Eq, Hash)]
enum LogCategory {
    HttpRequest,
    DatabaseQuery,
    Authentication,
    CacheOperation,
    SystemMetric,
    ExternalApi,
    Unknown,
}

// TODO: Create a RegexSet with patterns for each category.
// Each pattern should match characteristic strings for that category.
//
// static CATEGORY_SET: LazyLock<RegexSet> = LazyLock::new(|| {
//     RegexSet::new(&[
//         r"(?i)(GET|POST|PUT|DELETE|PATCH)\s+/",    // 0: HttpRequest
//         r"(?i)(SELECT|INSERT|UPDATE|DELETE)\s+",    // 1: DatabaseQuery
//         r"(?i)(login|logout|auth|token|session)",   // 2: Authentication
//         r"(?i)(cache\s+(hit|miss|set|expire))",     // 3: CacheOperation
//         r"(?i)(cpu|memory|disk|load|usage)\s*[:=]", // 4: SystemMetric
//         r"(?i)(external|api|webhook|callback)\s+(call|request|response)", // 5: ExternalApi
//     ]).unwrap()
// });

// TODO: Implement classify_message that returns the category for a log message.
// Use CATEGORY_SET.matches(message) to find which patterns matched.
// If multiple match, pick the first. If none match, return Unknown.
//
// fn classify_message(message: &str) -> LogCategory {
//     let matches = CATEGORY_SET.matches(message);
//     if matches.matched(0) { return LogCategory::HttpRequest; }
//     // TODO: check patterns 1..5
//     // ...
//     LogCategory::Unknown
// }

// TODO: Implement extract_http_details that extracts method and path
// from HTTP request log lines.
// Example: "GET /api/users/123 HTTP/1.1" -> Some(("GET", "/api/users/123"))
//
// fn extract_http_details(message: &str) -> Option<(String, String)> {
//     static HTTP_RE: LazyLock<Regex> = LazyLock::new(|| {
//         Regex::new(r"(?P<method>GET|POST|PUT|DELETE|PATCH)\s+(?P<path>/\S+)").unwrap()
//     });
//     let caps = HTTP_RE.captures(message)?;
//     Some((
//         caps["method"].to_string(),
//         caps["path"].to_string(),
//     ))
// }

// TODO: Implement extract_query_details that extracts the table name
// from SQL queries.
// Example: "SELECT * FROM users WHERE id = 5" -> Some(("SELECT", "users"))
//
// fn extract_query_details(message: &str) -> Option<(String, String)> {
//     static SQL_RE: LazyLock<Regex> = LazyLock::new(|| {
//         Regex::new(r"(?i)(?P<op>SELECT|INSERT|UPDATE|DELETE)\s+.*?(?:FROM|INTO|UPDATE)\s+(?P<table>\w+)").unwrap()
//     });
//     // TODO: capture and return (operation, table)
//     todo!()
// }

fn main() {
    let messages = vec![
        "GET /api/users/123 HTTP/1.1 - 200 OK (45ms)",
        "SELECT * FROM orders WHERE customer_id = 42",
        "User login successful: alice@example.com",
        "Cache hit for key: session_abc123",
        "Memory usage: 78% (2.4GB / 3.1GB)",
        "External API call to payment service (230ms)",
        "POST /api/orders HTTP/1.1 - 201 Created (120ms)",
        "INSERT INTO audit_log VALUES (1, 'login', NOW())",
        "Token refresh for user_456",
        "Cache miss for key: product_789",
        "CPU load: 0.45 (4 cores)",
        "Webhook response received from Stripe",
        "DELETE FROM temp_sessions WHERE expired = true",
        "This is just a generic log message",
        "Application started successfully",
    ];

    // Classify all messages:
    let mut category_counts: HashMap<LogCategory, usize> = HashMap::new();

    println!("Message classification:");
    for msg in &messages {
        let category = classify_message(msg);
        *category_counts.entry(category.clone()).or_insert(0) += 1;
        println!("  [{:15?}] {}", category, &msg[..60.min(msg.len())]);
    }

    println!("\nCategory summary:");
    let mut sorted: Vec<_> = category_counts.iter().collect();
    sorted.sort_by(|a, b| b.1.cmp(a.1));
    for (cat, count) in sorted {
        println!("  {cat:?}: {count}");
    }

    // Extract HTTP details:
    println!("\nHTTP requests:");
    for msg in &messages {
        if let Some((method, path)) = extract_http_details(msg) {
            println!("  {method} {path}");
        }
    }

    // Extract SQL details:
    println!("\nDatabase queries:");
    for msg in &messages {
        if let Some((op, table)) = extract_query_details(msg) {
            println!("  {op} on {table}");
        }
    }
}
```

### Exercise 5: Build a Log Analyzer

Combine regex with file I/O to build a complete log analysis tool.

```rust
use regex::Regex;
use std::collections::HashMap;
use std::fs::{self, File};
use std::io::{self, BufRead, BufReader, Write, BufWriter};
use std::path::Path;
use std::sync::LazyLock;

// Pre-compiled regex patterns:

static LOG_LINE_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(
        r"^(?P<timestamp>\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2})\s+\[(?P<level>\w+)\]\s+(?P<message>.+)$"
    ).unwrap()
});

static RESPONSE_TIME_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?P<time>\d+)ms").unwrap()
});

static HTTP_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"(?P<method>GET|POST|PUT|DELETE|PATCH)\s+(?P<path>/\S+)").unwrap()
});

static IP_RE: LazyLock<Regex> = LazyLock::new(|| {
    Regex::new(r"\b(?P<ip>\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})\b").unwrap()
});

#[derive(Debug)]
struct ParsedLog {
    timestamp: String,
    level: String,
    message: String,
    response_time_ms: Option<u64>,
    http_method: Option<String>,
    http_path: Option<String>,
    ip_address: Option<String>,
}

#[derive(Debug, Default)]
struct AnalysisReport {
    total_lines: usize,
    parsed_lines: usize,
    level_counts: HashMap<String, usize>,
    endpoint_counts: HashMap<String, usize>,
    ip_counts: HashMap<String, usize>,
    response_times: Vec<u64>,
    error_messages: Vec<String>,
    slowest_requests: Vec<(String, u64)>,
}

/// Generate a realistic sample log file.
fn generate_sample_log(path: &Path) -> io::Result<()> {
    let mut writer = BufWriter::new(File::create(path)?);
    let methods = ["GET", "POST", "PUT", "DELETE"];
    let paths = ["/api/users", "/api/orders", "/api/products", "/api/auth/login",
                 "/api/health", "/api/search", "/api/cart", "/api/payments"];
    let levels = ["INFO", "INFO", "INFO", "INFO", "WARN", "ERROR", "DEBUG"];
    let ips = ["192.168.1.100", "10.0.0.42", "172.16.0.5", "192.168.1.200", "10.0.0.99"];

    for i in 0..300 {
        let hour = 10 + (i / 60) % 8;
        let min = i % 60;
        let sec = (i * 13) % 60;
        let level = levels[i % levels.len()];
        let method = methods[i % methods.len()];
        let path_str = paths[i % paths.len()];
        let ip = ips[i % ips.len()];
        let time = 20 + (i * 37 % 500);

        let message = match level {
            "ERROR" => match i % 3 {
                0 => format!("Connection timeout from {ip} - {method} {path_str}"),
                1 => format!("Database error processing {method} {path_str} from {ip}"),
                _ => format!("Internal server error at {path_str} for {ip} ({time}ms)"),
            },
            "WARN" => match i % 2 {
                0 => format!("Slow query from {ip}: {method} {path_str} completed in {time}ms"),
                _ => format!("Rate limit approaching for {ip} on {path_str}"),
            },
            "DEBUG" => format!("Processing {method} {path_str} from {ip}"),
            _ => format!("{method} {path_str} from {ip} - 200 OK ({time}ms)"),
        };

        writeln!(
            writer,
            "2026-03-13T{hour:02}:{min:02}:{sec:02} [{level}] {message}"
        )?;
    }
    writer.flush()
}

// TODO: Implement parse_log_line that parses a single log line into a ParsedLog.
// Use LOG_LINE_RE to extract timestamp, level, and message.
// Then use RESPONSE_TIME_RE, HTTP_RE, and IP_RE on the message to extract details.
//
// fn parse_log_line(line: &str) -> Option<ParsedLog> {
//     let caps = LOG_LINE_RE.captures(line)?;
//     let timestamp = caps["timestamp"].to_string();
//     let level = caps["level"].to_string();
//     let message = caps["message"].to_string();
//
//     // TODO: Extract response time (Option<u64>) from message:
//     let response_time_ms = RESPONSE_TIME_RE.captures(&message)
//         .and_then(|c| todo!()); // parse the "time" group
//
//     // TODO: Extract HTTP method and path (both Option<String>) from message:
//     let (http_method, http_path) = match HTTP_RE.captures(&message) {
//         Some(c) => todo!(), // extract method and path
//         None => (None, None),
//     };
//
//     // TODO: Extract IP address (Option<String>) from message:
//     let ip_address = IP_RE.captures(&message)
//         .map(|c| todo!()); // extract ip group
//
//     Some(ParsedLog {
//         timestamp,
//         level,
//         message,
//         response_time_ms,
//         http_method,
//         http_path,
//         ip_address,
//     })
// }

// TODO: Implement analyze_logs that reads a log file and produces an AnalysisReport.
//
// fn analyze_logs(path: &Path) -> io::Result<AnalysisReport> {
//     let file = File::open(path)?;
//     let reader = BufReader::new(file);
//     let mut report = AnalysisReport::default();
//
//     for line in reader.lines() {
//         let line = line?;
//         report.total_lines += 1;
//
//         if let Some(parsed) = parse_log_line(&line) {
//             report.parsed_lines += 1;
//
//             // TODO: Increment level count
//             // *report.level_counts.entry(todo!()).or_insert(0) += 1;
//
//             // TODO: If there's a response time, push it to response_times
//             // and track slowest requests
//
//             // TODO: If there's an HTTP path, increment endpoint_counts
//
//             // TODO: If there's an IP, increment ip_counts
//
//             // TODO: If level is ERROR, push message to error_messages
//         }
//     }
//
//     // Sort slowest requests:
//     report.slowest_requests.sort_by(|a, b| b.1.cmp(&a.1));
//     report.slowest_requests.truncate(10);
//
//     Ok(report)
// }

// TODO: Implement print_report that displays the analysis results.
//
// fn print_report(report: &AnalysisReport) {
//     println!("=== Log Analysis Report ===\n");
//     println!("Lines processed: {} ({} parsed)", report.total_lines, report.parsed_lines);
//
//     // TODO: Print level counts sorted by count descending
//     println!("\nLog levels:");
//     // ...
//
//     // TODO: Print response time statistics (min, max, avg, p50, p95)
//     if !report.response_times.is_empty() {
//         let mut times = report.response_times.clone();
//         times.sort();
//         let sum: u64 = times.iter().sum();
//         let avg = sum as f64 / times.len() as f64;
//         let min = times[0];
//         let max = times[times.len() - 1];
//         let p50 = times[times.len() / 2];
//         let p95 = times[times.len() * 95 / 100];
//         println!("\nResponse times:");
//         println!("  Count: {}", times.len());
//         println!("  Min:   {}ms", min);
//         println!("  Max:   {}ms", max);
//         println!("  Avg:   {:.1}ms", avg);
//         println!("  p50:   {}ms", p50);
//         println!("  p95:   {}ms", p95);
//     }
//
//     // TODO: Print top 5 endpoints
//     println!("\nTop endpoints:");
//     // ...
//
//     // TODO: Print top 5 IPs
//     println!("\nTop IPs:");
//     // ...
//
//     // TODO: Print top 5 slowest requests
//     println!("\nSlowest requests:");
//     // ...
//
//     // TODO: Print first 5 error messages
//     if !report.error_messages.is_empty() {
//         println!("\nRecent errors ({} total):", report.error_messages.len());
//         for msg in report.error_messages.iter().take(5) {
//             println!("  {}", &msg[..80.min(msg.len())]);
//         }
//     }
// }

fn main() -> io::Result<()> {
    let dir = Path::new("exercise_output");
    fs::create_dir_all(dir)?;

    let log_path = dir.join("server.log");
    generate_sample_log(&log_path)?;
    println!("Generated log: {}\n", log_path.display());

    let report = analyze_logs(&log_path)?;
    print_report(&report);

    // Write the report to a file too:
    // (Bonus: redirect print_report output to a file using a custom writer)

    Ok(())
}
```

## Try It Yourself

1. **Email validator**: Write a comprehensive email regex that handles common edge cases (subdomains, plus addressing like `user+tag@domain.com`, hyphens in domains). Test against a list of valid and invalid addresses.

2. **Markdown parser**: Use regex to convert a subset of Markdown to HTML: headings (`# H1`, `## H2`), bold (`**text**`), italic (`*text*`), code blocks (`` `code` ``), and links (`[text](url)`).

3. **CSV parser**: Build a simple CSV parser using regex that handles quoted fields containing commas. Pattern: `(?:"([^"]*)"|([^,]*))`captures either a quoted field or an unquoted field.

4. **Semantic versioning**: Parse version strings like `"1.2.3-beta.1+build.456"` using named groups for major, minor, patch, prerelease, and build metadata. Implement comparison (which version is newer?).

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Compiling regex inside a loop | Slow performance | Use `LazyLock` or compile before the loop |
| Forgetting raw strings `r"..."` | Unexpected escape behavior, `\\d` instead of `\d` | Always use raw strings for regex patterns |
| Using `&caps[1]` on a non-matching group | Panic at runtime | Use `caps.get(1)` which returns `Option<Match>` |
| Greedy matching consuming too much | `.*` matches more than intended | Use `.*?` for non-greedy, or be more specific with character classes |
| Using regex for simple string operations | Unnecessary complexity and overhead | Use `str::contains`, `starts_with`, `split` for fixed patterns |
| Not anchoring validation patterns | `is_match("abc123xyz")` returns true for `\d+` | Use `^` and `$` anchors for full-string validation |

## Verification

```bash
# Create a project for the exercises:
cargo new regex_exercises && cd regex_exercises

# Add dependency:
echo 'regex = "1"' >> Cargo.toml

# Exercise 1: Pattern matching
# Copy exercise code to src/bin/ex1.rs
cargo run --bin ex1
# Expected: Phone validation, IP extraction, word counting

# Exercise 2: Captures
cargo run --bin ex2
# Expected: Date parsing, log parsing, key-value extraction

# Exercise 3: Replacement
cargo run --bin ex3
# Expected: Phone redaction, date reformatting, temp conversion

# Exercise 4: RegexSet
cargo run --bin ex4
# Expected: Messages classified into categories with counts

# Exercise 5: Log analyzer
cargo run --bin ex5
# Expected: Full analysis report with levels, response times, top endpoints
```

## Summary

The `regex` crate brings full regular expression support to Rust with an emphasis on safety (no catastrophic backtracking) and performance (compiled to DFA/NFA automata). The critical optimization is to compile patterns once using `LazyLock` or similar mechanisms. Capture groups (named and unnamed) extract structured data from text. `replace_all` with closures enables dynamic transformations. `RegexSet` efficiently tests multiple patterns in a single pass. Combined with Rust's I/O facilities, regex is a powerful tool for log analysis, data parsing, and text transformation. But remember: when a simple `str::contains` or `str::starts_with` suffices, use that instead -- regex is for patterns, not fixed strings.

## What You Learned

- Using the `regex` crate for matching, finding, and iterating over pattern matches
- Compiling regex once with `LazyLock` to avoid repeated compilation overhead
- Extracting data with unnamed and named capture groups
- Replacing text with static strings, backreferences, and closures
- Classifying strings against multiple patterns with `RegexSet`
- Building a complete log analyzer combining regex with buffered file I/O
- When to use regex vs simple string methods

## Resources

- [regex crate documentation](https://docs.rs/regex/latest/regex/)
- [Regex syntax reference](https://docs.rs/regex/latest/regex/#syntax)
- [Rust Cookbook: Regular Expressions](https://rust-lang-nursery.github.io/rust-cookbook/text/regex.html)
- [LazyLock documentation](https://doc.rust-lang.org/std/sync/struct.LazyLock.html)
- [RegexSet documentation](https://docs.rs/regex/latest/regex/struct.RegexSet.html)
- [regex101.com](https://regex101.com/) -- interactive regex tester (select "Rust" flavor)
