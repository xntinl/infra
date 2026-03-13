# 18. File I/O and Filesystem

**Difficulty**: Intermedio

## Prerequisites

- Completed: 01-basico exercises (strings, ownership, error handling basics)
- Completed: 07-error-handling-patterns (Result, the `?` operator, custom errors)
- Familiar with closures, iterators, and trait implementations

## Learning Objectives

- Read and write files using both convenience functions and buffered I/O
- Navigate the filesystem with `Path`, `PathBuf`, and `std::fs` operations
- Apply buffered readers and writers for efficient line-by-line processing
- Handle I/O errors idiomatically with `io::Result` and the `?` operator
- Traverse directories recursively with `read_dir` and build practical file tools

## Concepts

### Two Levels of File I/O

Rust provides two levels of file I/O:

**Convenience functions** in `std::fs` -- one-liners for simple tasks:

```rust
use std::fs;

// Read entire file into a String:
let content = fs::read_to_string("config.toml")?;

// Read entire file into bytes:
let bytes = fs::read("image.png")?;

// Write a string to a file (creates or overwrites):
fs::write("output.txt", "Hello, world!")?;

// Append is not directly available -- use OpenOptions (shown below).
```

These are convenient but read/write the entire file at once. For large files or line-by-line processing, use buffered I/O.

**Buffered I/O** with `BufReader` and `BufWriter`:

```rust
use std::fs::File;
use std::io::{self, BufRead, BufReader, BufWriter, Write};

// Read line by line:
let file = File::open("large.log")?;
let reader = BufReader::new(file);
for line in reader.lines() {
    let line = line?;  // each line is a Result<String>
    println!("{line}");
}

// Write with buffering:
let file = File::create("output.txt")?;
let mut writer = BufWriter::new(file);
writeln!(writer, "Line 1")?;
writeln!(writer, "Line 2")?;
writer.flush()?;  // ensure all data is written
```

`BufReader` wraps any `Read` type and adds an internal buffer, reducing system calls. `BufWriter` does the same for writes. The `lines()` method on `BufRead` returns an iterator of `io::Result<String>`.

### Path and PathBuf

`Path` and `PathBuf` are the filesystem counterparts of `str` and `String`:

```rust
use std::path::{Path, PathBuf};

// Path is a borrowed reference (like &str):
let path = Path::new("/home/user/documents/report.txt");

// PathBuf is an owned path (like String):
let mut path_buf = PathBuf::from("/home/user");
path_buf.push("documents");
path_buf.push("report.txt");

// Useful methods:
path.file_name();      // Some("report.txt")
path.extension();      // Some("txt")
path.parent();         // Some("/home/user/documents")
path.file_stem();      // Some("report")
path.is_absolute();    // true
path.exists();         // checks filesystem

// Join paths (like push but returns a new PathBuf):
let full = Path::new("/home/user").join("docs").join("file.txt");
// "/home/user/docs/file.txt"

// Convert between Path and str:
let s: &str = path.to_str().unwrap();         // Path -> &str (can fail on non-UTF8)
let p: &Path = Path::new("/some/path");       // &str -> &Path (free)
let display = path.display();                  // for Display formatting
```

### Opening Files with Options

`File::create` truncates existing files. For more control, use `OpenOptions`:

```rust
use std::fs::OpenOptions;

// Append to a file:
let file = OpenOptions::new()
    .append(true)
    .create(true)  // create if it doesn't exist
    .open("log.txt")?;

// Read and write:
let file = OpenOptions::new()
    .read(true)
    .write(true)
    .open("data.bin")?;

// Create only if it doesn't exist (fail if it does):
let file = OpenOptions::new()
    .write(true)
    .create_new(true)
    .open("unique.txt")?;
```

### Filesystem Operations

```rust
use std::fs;

// Create directories:
fs::create_dir("new_dir")?;               // fails if parent doesn't exist
fs::create_dir_all("a/b/c/d")?;           // creates all parents

// Remove:
fs::remove_file("temp.txt")?;
fs::remove_dir("empty_dir")?;             // must be empty
fs::remove_dir_all("dir_with_contents")?; // recursive delete

// Copy and rename:
fs::copy("source.txt", "dest.txt")?;      // returns bytes copied
fs::rename("old.txt", "new.txt")?;        // atomic on same filesystem

// Metadata:
let meta = fs::metadata("file.txt")?;
meta.len();          // file size in bytes
meta.is_file();      // true if regular file
meta.is_dir();       // true if directory
meta.modified()?;    // last modification time (SystemTime)
```

### Directory Traversal

`fs::read_dir` returns an iterator over directory entries:

```rust
use std::fs;

for entry in fs::read_dir("src")? {
    let entry = entry?;  // each entry is a Result<DirEntry>
    let path = entry.path();
    let file_type = entry.file_type()?;

    if file_type.is_file() {
        println!("File: {}", path.display());
    } else if file_type.is_dir() {
        println!("Dir:  {}", path.display());
    }
}
```

For recursive traversal, you write a recursive function:

```rust
fn walk_dir(dir: &Path) -> io::Result<Vec<PathBuf>> {
    let mut files = Vec::new();
    for entry in fs::read_dir(dir)? {
        let entry = entry?;
        let path = entry.path();
        if path.is_dir() {
            files.extend(walk_dir(&path)?);
        } else {
            files.push(path);
        }
    }
    Ok(files)
}
```

### Error Handling Patterns

All filesystem operations return `io::Result<T>`, which is `Result<T, io::Error>`. Common patterns:

```rust
use std::io;

// Propagate with ?:
fn read_config() -> io::Result<String> {
    let content = std::fs::read_to_string("config.toml")?;
    Ok(content)
}

// Handle specific error kinds:
match std::fs::read_to_string("missing.txt") {
    Ok(content) => println!("{content}"),
    Err(e) if e.kind() == io::ErrorKind::NotFound => {
        println!("File not found, using defaults");
    }
    Err(e) if e.kind() == io::ErrorKind::PermissionDenied => {
        println!("Access denied");
    }
    Err(e) => return Err(e),
}
```

### Anti-Pattern: Ignoring Buffering

```rust
use std::fs::File;
use std::io::{Read, Write};

// SLOW: Unbuffered single-byte writes
let mut file = File::create("output.txt")?;
for byte in data {
    file.write_all(&[byte])?;  // one system call per byte!
}

// FAST: Buffered writes
let mut writer = BufWriter::new(File::create("output.txt")?);
for byte in data {
    writer.write_all(&[byte])?;  // buffered, far fewer system calls
}
writer.flush()?;
```

### Anti-Pattern: Reading Entire Large Files

```rust
// WRONG for large files: loads everything into memory
let content = fs::read_to_string("10gb.log")?;
for line in content.lines() {
    // process line
}

// RIGHT for large files: streams line by line
let reader = BufReader::new(File::open("10gb.log")?);
for line in reader.lines() {
    let line = line?;
    // process line -- only one line in memory at a time
}
```

## Exercises

### Exercise 1: File Read/Write Basics

Practice reading, writing, and appending to files.

```rust
use std::fs::{self, File, OpenOptions};
use std::io::{self, Write, BufWriter};
use std::path::Path;

fn main() -> io::Result<()> {
    let dir = Path::new("exercise_output");

    // Setup: create a working directory
    if dir.exists() {
        fs::remove_dir_all(dir)?;
    }
    fs::create_dir_all(dir)?;

    // TODO 1: Write a greeting to "exercise_output/hello.txt"
    // Use fs::write to create the file with the content "Hello, Rust file I/O!\n"
    //
    // fs::write(todo!(), todo!())?;

    // TODO 2: Read it back and verify
    // Use fs::read_to_string and print the content.
    //
    // let content = todo!();
    // println!("Read: {content}");
    // assert!(content.contains("Hello"));

    // TODO 3: Append three lines to the file using OpenOptions.
    // Open with append(true), then use writeln!() for each line.
    //
    // let mut file = OpenOptions::new()
    //     .todo!()    // set append mode
    //     .open(dir.join("hello.txt"))?;
    // writeln!(file, "Line 2: appended")?;
    // writeln!(file, "Line 3: also appended")?;
    // writeln!(file, "Line 4: final line")?;

    // TODO 4: Read the full file again and count the lines.
    //
    // let content = fs::read_to_string(dir.join("hello.txt"))?;
    // let line_count = todo!(); // hint: content.lines().count()
    // println!("Total lines: {line_count}");
    // assert_eq!(line_count, 4);

    // TODO 5: Write a CSV file using BufWriter for efficiency.
    // Create "exercise_output/data.csv" with a header and 100 rows.
    // Header: "id,name,score"
    // Rows: "{i},student_{i},{i*7 % 100}"
    //
    // let file = File::create(dir.join("data.csv"))?;
    // let mut writer = BufWriter::new(file);
    // writeln!(writer, "id,name,score")?;
    // for i in 1..=100 {
    //     writeln!(writer, todo!())?;  // write the CSV row
    // }
    // writer.flush()?;

    // TODO 6: Read the CSV and find the row with the highest score.
    // Use BufReader + lines() to read line by line.
    // Skip the header, split each line by ',', parse the score.
    //
    // use std::io::{BufRead, BufReader};
    // let file = File::open(dir.join("data.csv"))?;
    // let reader = BufReader::new(file);
    // let mut best_name = String::new();
    // let mut best_score = 0u32;
    //
    // for line in reader.lines().skip(1) {  // skip header
    //     let line = line?;
    //     let parts: Vec<&str> = line.split(',').collect();
    //     // TODO: parse parts[1] as name and parts[2] as score
    //     // Update best_name and best_score if this score is higher
    // }
    // println!("Best student: {best_name} with score {best_score}");

    // Cleanup:
    println!("\nExercise output in: {}", dir.display());

    Ok(())
}
```

### Exercise 2: Line Processing with BufReader

Build a log file analyzer that processes a log file line by line.

```rust
use std::fs::{self, File};
use std::io::{self, BufRead, BufReader, Write, BufWriter};
use std::path::Path;
use std::collections::HashMap;

/// Generate a sample log file for the exercise.
fn generate_log(path: &Path) -> io::Result<()> {
    let mut writer = BufWriter::new(File::create(path)?);
    let levels = ["INFO", "WARN", "ERROR", "DEBUG"];
    let messages = [
        "Request processed in {}ms",
        "Database connection pool at {}%",
        "Cache miss for key user_{}",
        "Memory usage: {}MB",
        "Failed to connect to service_{}",
        "Retry attempt {} for operation",
        "Response sent to client_{}",
        "Timeout waiting for resource_{}",
    ];

    for i in 0..500 {
        let level = levels[i % levels.len()];
        let msg_template = messages[i % messages.len()];
        let msg = msg_template.replace("{}", &(i % 100).to_string());
        let hour = 10 + (i / 60) % 14;
        let min = i % 60;
        let sec = (i * 7) % 60;
        writeln!(
            writer,
            "2026-03-13T{hour:02}:{min:02}:{sec:02} [{level}] {msg}"
        )?;
    }
    writer.flush()
}

fn main() -> io::Result<()> {
    let dir = Path::new("exercise_output");
    fs::create_dir_all(dir)?;

    let log_path = dir.join("app.log");
    generate_log(&log_path)?;
    println!("Generated log at: {}", log_path.display());

    let file = File::open(&log_path)?;
    let reader = BufReader::new(file);

    let mut total_lines = 0u32;
    let mut level_counts: HashMap<String, u32> = HashMap::new();
    let mut error_messages: Vec<String> = Vec::new();
    let mut longest_line = String::new();

    // TODO: Process each line from reader.lines():
    //   1. Increment total_lines
    //   2. Extract the log level (between '[' and ']')
    //      Hint: line.find('[') and line.find(']'), then slice
    //   3. Increment the count in level_counts for that level
    //   4. If the level is "ERROR", push the full line to error_messages
    //   5. Track the longest line
    //
    // for line in reader.lines() {
    //     let line = line?;
    //     total_lines += 1;
    //
    //     // Extract level:
    //     if let (Some(start), Some(end)) = (line.find('['), line.find(']')) {
    //         let level = todo!(); // slice between '[' and ']'
    //         *level_counts.entry(todo!()).or_insert(0) += 1;
    //
    //         if level == "ERROR" {
    //             todo!() // push to error_messages
    //         }
    //     }
    //
    //     if line.len() > longest_line.len() {
    //         todo!() // update longest_line
    //     }
    // }

    println!("\n--- Log Analysis ---");
    println!("Total lines: {total_lines}");
    println!("Counts by level:");
    for (level, count) in &level_counts {
        println!("  [{level}]: {count}");
    }
    println!("Error count: {}", error_messages.len());
    if !error_messages.is_empty() {
        println!("First error: {}", error_messages[0]);
        println!("Last error:  {}", error_messages[error_messages.len() - 1]);
    }
    println!("Longest line ({} chars): {}", longest_line.len(), &longest_line[..80.min(longest_line.len())]);

    // TODO: Write a summary report to "exercise_output/report.txt"
    // Include: total lines, level counts, number of errors.
    //
    // let report_path = dir.join("report.txt");
    // let mut report = BufWriter::new(File::create(&report_path)?);
    // writeln!(report, "Log Analysis Report")?;
    // writeln!(report, "===================")?;
    // writeln!(report, "Total lines: {total_lines}")?;
    // // TODO: write level counts and error count
    // report.flush()?;
    // println!("\nReport written to: {}", report_path.display());

    Ok(())
}
```

### Exercise 3: Path Manipulation and Metadata

Work with paths, file metadata, and directory creation.

```rust
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::time::SystemTime;

/// Format a SystemTime as a simple string.
fn format_time(time: SystemTime) -> String {
    match time.duration_since(SystemTime::UNIX_EPOCH) {
        Ok(duration) => {
            let secs = duration.as_secs();
            let hours = (secs / 3600) % 24;
            let mins = (secs / 60) % 60;
            format!("{}h{}m (epoch: {})", hours, mins, secs)
        }
        Err(_) => "unknown".to_string(),
    }
}

/// Format byte size in human-readable form.
fn format_size(bytes: u64) -> String {
    if bytes < 1024 {
        format!("{bytes} B")
    } else if bytes < 1024 * 1024 {
        format!("{:.1} KB", bytes as f64 / 1024.0)
    } else if bytes < 1024 * 1024 * 1024 {
        format!("{:.1} MB", bytes as f64 / (1024.0 * 1024.0))
    } else {
        format!("{:.2} GB", bytes as f64 / (1024.0 * 1024.0 * 1024.0))
    }
}

fn main() -> io::Result<()> {
    // --- Part A: Path manipulation ---

    let path = PathBuf::from("/home/user/projects/rust/main.rs");

    // TODO: Extract and print each component:
    //   File name:  main.rs
    //   Extension:  rs
    //   Stem:       main
    //   Parent:     /home/user/projects/rust
    //   Is absolute: true
    //
    // println!("File name:   {:?}", path.file_name());
    // println!("Extension:   {:?}", todo!());
    // println!("Stem:        {:?}", todo!());
    // println!("Parent:      {:?}", todo!());
    // println!("Is absolute: {}", todo!());

    // TODO: Build a path by joining components:
    // Start with "/var/log", join "app", join "2026", join "03", join "output.log"
    //
    // let log_path = Path::new("/var/log")
    //     .join(todo!())
    //     .join(todo!())
    //     .join(todo!())
    //     .join(todo!());
    // println!("Log path: {}", log_path.display());
    // Expected: /var/log/app/2026/03/output.log

    // TODO: Change the extension of a path.
    // Start with "report.txt", change extension to "md", then to "html"
    //
    // let mut doc = PathBuf::from("report.txt");
    // doc.set_extension(todo!());
    // println!("Changed ext: {}", doc.display()); // report.md
    // doc.set_extension(todo!());
    // println!("Changed again: {}", doc.display()); // report.html

    // TODO: Iterate over path components:
    // Print each component of "/usr/local/bin/rustc"
    //
    // let bin_path = Path::new("/usr/local/bin/rustc");
    // println!("\nComponents of {}:", bin_path.display());
    // for component in bin_path.components() {
    //     println!("  {:?}", component);
    // }

    // --- Part B: File metadata ---

    // Create some files to inspect:
    let dir = Path::new("exercise_output/metadata_test");
    fs::create_dir_all(dir)?;

    // Create files of different sizes:
    fs::write(dir.join("small.txt"), "Hello")?;
    fs::write(dir.join("medium.txt"), "x".repeat(10_000))?;
    fs::write(dir.join("large.txt"), "y".repeat(1_000_000))?;
    fs::create_dir_all(dir.join("subdir"))?;
    fs::write(dir.join("subdir/nested.txt"), "Nested file")?;

    // TODO: Read metadata for each file in the directory.
    // For each entry in read_dir, print:
    //   - Name
    //   - Type (file/dir)
    //   - Size (using format_size)
    //   - Modified time (using format_time)
    //
    // println!("\n--- Directory listing: {} ---", dir.display());
    // for entry in fs::read_dir(dir)? {
    //     let entry = entry?;
    //     let metadata = entry.metadata()?;
    //     let name = entry.file_name();
    //     let type_str = if metadata.is_file() {
    //         "FILE"
    //     } else if metadata.is_dir() {
    //         "DIR "
    //     } else {
    //         "OTHER"
    //     };
    //     // TODO: print name, type, size, modified time
    //     // println!(
    //     //     "  [{type_str}] {:20} {:>10}  modified: {}",
    //     //     todo!(), todo!(), todo!()
    //     // );
    // }

    Ok(())
}
```

### Exercise 4: Recursive Directory Walker

Build a function that recursively walks a directory tree and collects file information.

```rust
use std::fs;
use std::io;
use std::path::{Path, PathBuf};
use std::collections::HashMap;

#[derive(Debug)]
struct FileInfo {
    path: PathBuf,
    size: u64,
    extension: String,
}

// TODO: Implement walk_directory that recursively collects all files.
// For each file (not directory), create a FileInfo with its path, size,
// and extension (use "none" if no extension).
//
// fn walk_directory(dir: &Path) -> io::Result<Vec<FileInfo>> {
//     let mut files = Vec::new();
//
//     for entry in fs::read_dir(dir)? {
//         let entry = entry?;
//         let path = entry.path();
//
//         if path.is_dir() {
//             // TODO: recurse into subdirectory and extend files
//         } else if path.is_file() {
//             let metadata = fs::metadata(&path)?;
//             let extension = path.extension()
//                 .and_then(|e| e.to_str())
//                 .unwrap_or("none")
//                 .to_string();
//             // TODO: push FileInfo
//         }
//     }
//
//     Ok(files)
// }

// TODO: Implement summarize_by_extension that groups files by extension
// and returns a HashMap with extension -> (count, total_size).
//
// fn summarize_by_extension(files: &[FileInfo]) -> HashMap<&str, (usize, u64)> {
//     let mut summary: HashMap<&str, (usize, u64)> = HashMap::new();
//     for file in files {
//         let entry = summary.entry(file.extension.as_str()).or_insert((0, 0));
//         // TODO: increment count and add size
//     }
//     summary
// }

// TODO: Implement find_largest_files that returns the N largest files
// sorted by size descending.
//
// fn find_largest_files(files: &[FileInfo], n: usize) -> Vec<&FileInfo> {
//     let mut sorted: Vec<&FileInfo> = files.iter().collect();
//     // TODO: sort by size descending, take first n
//     sorted
// }

// TODO: Implement find_duplicates_by_size that finds files with the same size.
// Return a Vec of groups (Vec of paths) where each group has 2+ files.
//
// fn find_duplicates_by_size(files: &[FileInfo]) -> Vec<Vec<&Path>> {
//     let mut by_size: HashMap<u64, Vec<&Path>> = HashMap::new();
//     for file in files {
//         by_size.entry(file.size).or_default().push(&file.path);
//     }
//     by_size.into_values()
//         .filter(|group| group.len() > 1)
//         .collect()
// }

fn main() -> io::Result<()> {
    // Create a test directory structure:
    let base = Path::new("exercise_output/walk_test");
    if base.exists() {
        fs::remove_dir_all(base)?;
    }

    // Build a directory tree with various files:
    fs::create_dir_all(base.join("src/models"))?;
    fs::create_dir_all(base.join("src/handlers"))?;
    fs::create_dir_all(base.join("tests"))?;
    fs::create_dir_all(base.join("docs"))?;

    fs::write(base.join("src/main.rs"), "fn main() {}\n")?;
    fs::write(base.join("src/lib.rs"), "pub mod models;\npub mod handlers;\n")?;
    fs::write(base.join("src/models/user.rs"), "pub struct User { name: String }\n")?;
    fs::write(base.join("src/models/order.rs"), "pub struct Order { id: u64 }\n")?;
    fs::write(base.join("src/handlers/api.rs"), "pub fn handle() {}\n")?;
    fs::write(base.join("tests/test_user.rs"), "#[test]\nfn test_user() {}\n")?;
    fs::write(base.join("docs/README.md"), "# Project\nDocumentation here.\n")?;
    fs::write(base.join("docs/CHANGELOG.md"), "# Changelog\n## v0.1.0\n- Initial release\n")?;
    fs::write(base.join("Cargo.toml"), "[package]\nname = \"demo\"\nversion = \"0.1.0\"\n")?;
    fs::write(base.join(".gitignore"), "target/\n*.swp\n")?;

    println!("Created test directory: {}", base.display());

    // Walk the directory:
    let files = walk_directory(base)?;
    println!("\nAll files ({}):", files.len());
    for file in &files {
        println!("  {} ({} bytes) [.{}]",
            file.path.display(), file.size, file.extension);
    }

    // Summary by extension:
    let summary = summarize_by_extension(&files);
    println!("\nBy extension:");
    let mut sorted_exts: Vec<_> = summary.iter().collect();
    sorted_exts.sort_by(|a, b| b.1.1.cmp(&a.1.1)); // sort by total size
    for (ext, (count, size)) in sorted_exts {
        println!("  .{ext}: {count} files, {size} bytes");
    }

    // Largest files:
    let largest = find_largest_files(&files, 3);
    println!("\nTop 3 largest:");
    for file in largest {
        println!("  {} ({} bytes)", file.path.display(), file.size);
    }

    // Potential duplicates by size:
    let dupes = find_duplicates_by_size(&files);
    if dupes.is_empty() {
        println!("\nNo size-based duplicates found.");
    } else {
        println!("\nPotential duplicates (same size):");
        for group in dupes {
            println!("  Group:");
            for path in group {
                println!("    {}", path.display());
            }
        }
    }

    Ok(())
}
```

### Exercise 5: Build a File Search Tool

Combine everything into a practical `grep`-like file search tool.

```rust
use std::fs::{self, File};
use std::io::{self, BufRead, BufReader, Write, BufWriter};
use std::path::{Path, PathBuf};

#[derive(Debug)]
struct SearchMatch {
    path: PathBuf,
    line_number: usize,
    line: String,
}

#[derive(Debug)]
struct SearchOptions {
    pattern: String,
    case_insensitive: bool,
    extensions: Vec<String>,    // empty = all files
    max_results: usize,         // 0 = unlimited
}

impl SearchOptions {
    fn new(pattern: &str) -> Self {
        SearchOptions {
            pattern: pattern.to_string(),
            case_insensitive: false,
            extensions: Vec::new(),
            max_results: 0,
        }
    }

    fn case_insensitive(mut self) -> Self {
        self.case_insensitive = true;
        self
    }

    fn with_extensions(mut self, exts: &[&str]) -> Self {
        self.extensions = exts.iter().map(|s| s.to_string()).collect();
        self
    }

    fn with_max_results(mut self, n: usize) -> Self {
        self.max_results = n;
        self
    }
}

// TODO: Implement should_search_file that checks if a file matches the extension filter.
// If options.extensions is empty, search all files.
// Otherwise, check if the file's extension is in the list.
//
// fn should_search_file(path: &Path, options: &SearchOptions) -> bool {
//     if options.extensions.is_empty() {
//         return true;
//     }
//     // TODO: get the extension from path and check if it's in options.extensions
//     todo!()
// }

// TODO: Implement search_file that searches a single file for the pattern.
// Open the file with BufReader, iterate lines with enumerate (for line numbers).
// Check if each line contains the pattern (respecting case_insensitive option).
// Return a Vec<SearchMatch>.
//
// fn search_file(path: &Path, options: &SearchOptions) -> io::Result<Vec<SearchMatch>> {
//     let file = File::open(path)?;
//     let reader = BufReader::new(file);
//     let mut matches = Vec::new();
//
//     let pattern = if options.case_insensitive {
//         options.pattern.to_lowercase()
//     } else {
//         options.pattern.clone()
//     };
//
//     for (line_num, line) in reader.lines().enumerate() {
//         let line = line?;
//         let haystack = if options.case_insensitive {
//             todo!() // lowercase the line
//         } else {
//             todo!() // use line as-is (clone it or reference it)
//         };
//
//         if haystack.contains(&pattern) {
//             // TODO: create a SearchMatch and push it
//             // line_number should be 1-based (line_num + 1)
//         }
//     }
//
//     Ok(matches)
// }

// TODO: Implement search_directory that recursively searches a directory.
// Walk all files, filter by extension, search each matching file.
// Respect max_results by stopping early if the limit is reached.
//
// fn search_directory(dir: &Path, options: &SearchOptions) -> io::Result<Vec<SearchMatch>> {
//     let mut all_matches = Vec::new();
//
//     for entry in fs::read_dir(dir)? {
//         let entry = entry?;
//         let path = entry.path();
//
//         if path.is_dir() {
//             // TODO: recurse and extend all_matches
//             // Check max_results limit after each recursion
//         } else if path.is_file() && should_search_file(&path, options) {
//             // TODO: search the file and extend all_matches
//             // Check max_results limit
//         }
//
//         // Early exit if we have enough results:
//         if options.max_results > 0 && all_matches.len() >= options.max_results {
//             all_matches.truncate(options.max_results);
//             break;
//         }
//     }
//
//     Ok(all_matches)
// }

fn main() -> io::Result<()> {
    // Create a test project to search:
    let base = Path::new("exercise_output/search_test");
    if base.exists() {
        fs::remove_dir_all(base)?;
    }

    fs::create_dir_all(base.join("src"))?;
    fs::create_dir_all(base.join("tests"))?;

    fs::write(base.join("src/main.rs"), r#"
fn main() {
    println!("Hello, world!");
    let config = load_config();
    process_data(&config);
}

fn load_config() -> Config {
    // TODO: load from file
    Config::default()
}

fn process_data(config: &Config) {
    println!("Processing data with config: {:?}", config);
    for item in get_items() {
        handle_item(item);
    }
}
"#)?;

    fs::write(base.join("src/lib.rs"), r#"
pub struct Config {
    pub debug: bool,
    pub max_items: usize,
}

impl Default for Config {
    fn default() -> Self {
        Config { debug: false, max_items: 100 }
    }
}

pub fn get_items() -> Vec<String> {
    vec!["item1".into(), "item2".into()]
}

pub fn handle_item(item: String) {
    println!("Handling: {item}");
}
"#)?;

    fs::write(base.join("tests/test_config.rs"), r#"
use search_test::Config;

#[test]
fn test_default_config() {
    let config = Config::default();
    assert!(!config.debug);
    assert_eq!(config.max_items, 100);
}

#[test]
fn test_config_override() {
    let config = Config { debug: true, max_items: 50 };
    assert!(config.debug);
}
"#)?;

    fs::write(base.join("README.md"), r#"
# Search Test Project

This project demonstrates file search functionality.
The Config struct handles configuration management.
"#)?;

    println!("Test project created at: {}\n", base.display());

    // Search 1: Find all occurrences of "config" (case insensitive)
    let opts = SearchOptions::new("config").case_insensitive();
    let matches = search_directory(base, &opts)?;
    println!("=== Search: 'config' (case insensitive) ===");
    println!("Found {} matches:", matches.len());
    for m in &matches {
        println!("  {}:{}: {}", m.path.display(), m.line_number, m.line.trim());
    }

    // Search 2: Find "fn" only in .rs files
    let opts = SearchOptions::new("fn ")
        .with_extensions(&["rs"]);
    let matches = search_directory(base, &opts)?;
    println!("\n=== Search: 'fn ' in .rs files ===");
    println!("Found {} matches:", matches.len());
    for m in &matches {
        println!("  {}:{}: {}", m.path.display(), m.line_number, m.line.trim());
    }

    // Search 3: Find "TODO" with max 2 results
    let opts = SearchOptions::new("TODO").with_max_results(2);
    let matches = search_directory(base, &opts)?;
    println!("\n=== Search: 'TODO' (max 2) ===");
    println!("Found {} matches:", matches.len());
    for m in &matches {
        println!("  {}:{}: {}", m.path.display(), m.line_number, m.line.trim());
    }

    // Search 4: Find "assert" in test files only
    let opts = SearchOptions::new("assert")
        .with_extensions(&["rs"]);
    let matches = search_directory(base, &opts)?;
    let test_matches: Vec<_> = matches.iter()
        .filter(|m| m.path.to_str().map_or(false, |s| s.contains("test")))
        .collect();
    println!("\n=== Search: 'assert' in test .rs files ===");
    println!("Found {} matches:", test_matches.len());
    for m in test_matches {
        println!("  {}:{}: {}", m.path.display(), m.line_number, m.line.trim());
    }

    Ok(())
}
```

## Try It Yourself

1. **CSV processor**: Read a CSV file with `BufReader`, parse each row into a struct, filter rows by a condition, and write the results to a new CSV. Handle malformed rows gracefully by skipping them and logging warnings.

2. **File deduplicator**: Extend Exercise 4's duplicate finder to compute a simple hash (sum of all bytes) for files with the same size, then report true duplicates (same size AND same hash).

3. **Watch mode**: Write a loop that checks a file's modification time every second and prints a message when it changes. Use `std::thread::sleep` and `fs::metadata().modified()`.

4. **Directory size report**: Build a tool that shows the total size of each subdirectory, sorted by size descending. This is similar to the `du` command.

## Common Mistakes

| Mistake | Symptom | Fix |
|---|---|---|
| Not using `BufReader`/`BufWriter` | Slow I/O for many small reads/writes | Always wrap `File` in `BufReader` or `BufWriter` |
| Forgetting `flush()` on `BufWriter` | Data not written to disk | Call `flush()` before the writer is dropped, or rely on `Drop` (but explicit is safer) |
| Using `read_to_string` on binary files | Garbled output or `Utf8Error` | Use `fs::read` for binary files, `read_to_string` only for text |
| Ignoring `Result` from `lines()` | `line` is `Result<String>`, not `String` | Always handle with `let line = line?` |
| Using `to_string()` on `Path` | `Path` is not always valid UTF-8 | Use `path.display()` for printing, `to_str()` only when UTF-8 is guaranteed |
| Not handling `NotFound` error | Unhelpful panic messages | Match on `err.kind() == io::ErrorKind::NotFound` for user-friendly messages |

## Verification

```bash
# Exercise 1: Read/write basics
rustc exercises/ex1_readwrite.rs && ./ex1_readwrite
# Expected: Creates hello.txt, appends lines, writes CSV, finds best student

# Exercise 2: Log analyzer
rustc exercises/ex2_log_analyzer.rs && ./ex2_log_analyzer
# Expected: Generates 500-line log, reports counts by level, errors, longest line

# Exercise 3: Path manipulation
rustc exercises/ex3_paths.rs && ./ex3_paths
# Expected: Path components extracted, paths joined, metadata listed

# Exercise 4: Directory walker
rustc exercises/ex4_walker.rs && ./ex4_walker
# Expected: 10 files found, grouped by extension, largest listed

# Exercise 5: File search
rustc exercises/ex5_search.rs && ./ex5_search
# Expected: Searches for "config", "fn", "TODO" with correct match counts

# Cleanup:
rm -rf exercise_output
```

## Summary

Rust's file I/O is built on a layered system: convenience functions in `std::fs` for one-shot operations, `File` for explicit open/close control, and `BufReader`/`BufWriter` for efficient streaming. `Path` and `PathBuf` handle cross-platform path manipulation. All operations return `io::Result`, making error handling explicit and composable with the `?` operator. Directory traversal with `read_dir` gives you an iterator of entries, and recursive walking is a natural extension. Together, these tools let you build practical file processing utilities entirely within the standard library.

## What You Learned

- Reading and writing files with `fs::read_to_string`, `fs::write`, and `OpenOptions`
- Streaming large files line-by-line with `BufReader` and `lines()`
- Efficient writes with `BufWriter` and explicit `flush()`
- Path manipulation: `join`, `file_name`, `extension`, `parent`, `components`
- File metadata: size, type, modification time
- Recursive directory traversal with `read_dir`
- Building a practical file search tool combining all the above

## Resources

- [The Rust Book: Reading a File](https://doc.rust-lang.org/book/ch12-02-reading-a-file.html)
- [std::fs module documentation](https://doc.rust-lang.org/std/fs/index.html)
- [std::io module documentation](https://doc.rust-lang.org/std/io/index.html)
- [std::path module documentation](https://doc.rust-lang.org/std/path/index.html)
- [BufReader documentation](https://doc.rust-lang.org/std/io/struct.BufReader.html)
- [Rust by Example: File I/O](https://doc.rust-lang.org/rust-by-example/std_misc/file.html)
