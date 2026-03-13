# 6. Built-in Functions

<!--
difficulty: basic
concepts: [string-functions, path-functions, utility-functions, uuid, sha256, datetime]
tools: [just]
estimated_time: 25m
bloom_level: apply
prerequisites: [05-conditionals-and-expressions]
-->

## Prerequisites

- `just` installed (`brew install just` / `cargo install just`)
- A terminal with `bash` available

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** string manipulation functions to transform project names and identifiers
- **Use** path functions to extract directory names, file stems, and extensions
- **Construct** build metadata recipes using `uuid()`, `sha256()`, and `datetime_utc()`

## Why Built-in Functions

Shell commands can do everything that just's built-in functions do, but at a cost: shell commands introduce platform dependencies, require subprocess spawning, and clutter recipe bodies with implementation details. When you need to uppercase a project name or extract a file extension, a built-in function communicates intent more clearly than piping through `tr` or `sed`.

Just's function library covers three domains that appear in nearly every project. String functions handle the naming transformations that arise when converting between conventions (kebab-case to snake_case, lowercase to uppercase). Path functions decompose file paths without shell-specific syntax. Utility functions generate UUIDs, checksums, and timestamps for build metadata.

These functions are evaluated by just itself, which means they produce identical results on macOS, Linux, and Windows. This is a significant advantage over shell equivalents, where `date` flags, `sed` syntax, and path separators vary across platforms.

## Step 1 -- String Manipulation Functions

Create a justfile that demonstrates string transformation functions on a project name.

### `justfile`

```justfile
project := "my-awesome-project"

project_upper     := uppercase(project)
project_snake     := replace(project, "-", "_")
project_capitalized := capitalize(project)
project_title     := titlecase(project)

# Display string transformations of the project name
strings:
    @echo "Original:    {{project}}"
    @echo "Uppercase:   {{project_upper}}"
    @echo "Snake case:  {{project_snake}}"
    @echo "Capitalized: {{project_capitalized}}"
    @echo "Titlecase:   {{project_title}}"
    @echo ""
    @echo "Trim demo:   '{{trim("  padded  ")}}'"
    @echo "Starts with: {{if project =~ '^my-' { "yes" } else { "no" } }}"
    @echo "Contains:    {{ if project =~ 'awesome' { "yes" } else { "no" } }}"
    @echo "Replace:     {{replace(project, "awesome", "simple")}}"
```

### Intermediate Verification

```bash
just strings
```

Expected:

```
Original:    my-awesome-project
Uppercase:   MY-AWESOME-PROJECT
Snake case:  my_awesome_project
Capitalized: My-awesome-project
Titlecase:   My-Awesome-Project
Trim demo:   'padded'
Starts with: yes
Contains:    yes
Replace:     my-simple-project
```

## Step 2 -- Path Functions

Path functions decompose file paths into their component parts. These are useful for generating output paths, log file names, or derived identifiers from source files.

Add to your justfile:

```justfile
sample_path := "/home/user/projects/my-app/src/main.rs"

# Display path decompositions
paths:
    @echo "Full path:        {{sample_path}}"
    @echo "Parent directory: {{parent_directory(sample_path)}}"
    @echo "File name:        {{file_name(sample_path)}}"
    @echo "File stem:        {{file_stem(sample_path)}}"
    @echo "Extension:        {{extension(sample_path)}}"
    @echo ""
    @echo "Nested parent:    {{parent_directory(parent_directory(sample_path))}}"
    @echo ""
    @echo "Join demo:        {{join("src", "lib", "utils.rs")}}"
    @echo "Clean demo:       {{clean("./src/../src/./main.rs")}}"
    @echo ""
    @echo "Justfile:         {{justfile()}}"
    @echo "Justfile dir:     {{justfile_directory()}}"
    @echo "Invocation dir:   {{invocation_directory()}}"
```

### Intermediate Verification

```bash
just paths
```

Expected (justfile-specific paths will vary):

```
Full path:        /home/user/projects/my-app/src/main.rs
Parent directory: /home/user/projects/my-app/src
File name:        main.rs
File stem:        main
Extension:        rs

Nested parent:    /home/user/projects/my-app
Join demo:        src/lib/utils.rs
Clean demo:       src/main.rs

Justfile:         /path/to/your/exercise/justfile
Justfile dir:     /path/to/your/exercise
Invocation dir:   /path/to/your/exercise
```

## Step 3 -- Build Metadata with Utility Functions

Use `uuid()`, `sha256()`, and `datetime_utc()` to generate unique build identifiers and timestamps. These are invaluable for tagging artifacts, generating correlation IDs, and embedding build provenance.

Add to your justfile:

```justfile
build_id    := uuid()
build_date  := datetime_utc("%Y-%m-%d")
build_time  := datetime_utc("%H:%M:%S")
build_ts    := datetime_utc("%Y%m%d%H%M%S")

# Generate and display build metadata
build-meta:
    @echo "Build ID:        {{build_id}}"
    @echo "Build date:      {{build_date}}"
    @echo "Build time:      {{build_time}}"
    @echo "Build timestamp: {{build_ts}}"
    @echo ""
    @echo "SHA-256 of project name:"
    @echo "  {{sha256(project)}}"
    @echo ""
    @echo "SHA-256 of build ID:"
    @echo "  {{sha256(build_id)}}"
    @echo ""
    @echo "CPU cores: {{num_cpus()}}"
```

### Intermediate Verification

```bash
just build-meta
```

Expected (values will vary):

```
Build ID:        a1b2c3d4-e5f6-7890-abcd-ef1234567890
Build date:      2026-03-09
Build time:      14:30:00
Build timestamp: 20260309143000

SHA-256 of project name:
  7a1f3c4b...  (64-character hex string)

SHA-256 of build ID:
  e9d8c7b6...  (64-character hex string)

CPU cores: 10
```

## Step 4 -- Combined Info Recipe

Bring all functions together in a comprehensive info recipe that serves as a project dashboard.

Add to your justfile:

```justfile
# Show all project information
info: strings paths build-meta
    @echo ""
    @echo "=== Summary ==="
    @echo "Project: {{project}} ({{project_upper}})"
    @echo "OS/Arch: {{os()}}/{{arch()}}"
    @echo "Build:   {{build_id}}"
    @echo "Date:    {{build_date}}T{{build_time}}Z"
```

### Intermediate Verification

```bash
just info 2>&1 | tail -5
```

Expected (values will vary):

```
=== Summary ===
Project: my-awesome-project (MY-AWESOME-PROJECT)
OS/Arch: macos/aarch64
Build:   a1b2c3d4-e5f6-7890-abcd-ef1234567890
Date:    2026-03-09T14:30:00Z
```

## Common Mistakes

### Confusing `trim()` with `trim_start_match()` / `trim_end_match()`

**Wrong assumption:** `trim()` removes a specific substring.

```justfile
# Trying to remove a "v" prefix from a version tag
version := trim("v1.2.3")
```

**What happens:**

`version` becomes `"v1.2.3"` -- `trim()` only removes leading and trailing whitespace, not arbitrary characters.

**Fix:**

```justfile
version := trim_start_match("v1.2.3", "v")
```

Result: `"1.2.3"`

### Using `extension()` on Paths Without Extensions

**Wrong:**

```justfile
ext := extension("Makefile")
```

**What happens:**

`ext` is an empty string `""`. No error is raised, which can cause silent bugs if you use the extension to construct output paths.

**Fix:**

Guard with a conditional:

```justfile
ext := extension("Makefile")
has_ext := if ext == "" { "false" } else { "true" }
```

### Expecting `uuid()` to Be Stable Across Invocations

**Wrong assumption:** calling `just --evaluate build_id` twice gives the same value.

Each invocation of `just` re-evaluates all expressions. `uuid()` generates a new value every time the justfile is loaded. If you need a stable identifier, write it to a file:

```justfile
# Generate a build ID and persist it
generate-id:
    @echo "{{uuid()}}" > .build-id
    @echo "Build ID saved to .build-id"
```

## Verify What You Learned

```bash
just strings 2>&1 | head -3
```

Expected:

```
Original:    my-awesome-project
Uppercase:   MY-AWESOME-PROJECT
Snake case:  my_awesome_project
```

```bash
just paths 2>&1 | grep "File stem"
```

Expected:

```
File stem:        main
```

```bash
just build-meta 2>&1 | grep "Build date"
```

Expected (date will vary):

```
Build date:      2026-03-09
```

```bash
just --evaluate project_upper
```

Expected:

```
MY-AWESOME-PROJECT
```

```bash
just --evaluate project_snake
```

Expected:

```
my_awesome_project
```

## What's Next

In [Exercise 7 -- Essential Settings](../07-essential-settings/07-essential-settings.md), you will learn how to configure just's behavior with settings like `set shell`, `set dotenv-load`, `set export`, and `set positional-arguments`.

## Summary

- **String functions** -- `uppercase()`, `lowercase()`, `replace()`, `trim()`, `capitalize()`, `titlecase()` for name transformations
- **Path functions** -- `parent_directory()`, `file_name()`, `file_stem()`, `extension()`, `join()`, `clean()` for path decomposition
- **`uuid()`** -- generates a random UUID v4, re-evaluated on each justfile load
- **`sha256()`** -- computes a SHA-256 hash of a string, useful for content-based identifiers
- **`datetime_utc()`** -- formats the current UTC time using strftime patterns

## Reference

- [Just Manual](https://just.systems/man/en/)
- [Just GitHub](https://github.com/casey/just)

## Additional Resources

- [Just Manual -- Functions](https://just.systems/man/en/chapter_32.html) -- complete function reference with examples
- [strftime Format Codes](https://strftime.org/) -- reference for `datetime_utc()` format patterns
- [Just Manual -- Strings](https://just.systems/man/en/chapter_26.html) -- string concatenation and quoting rules
