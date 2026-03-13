# Exercise 06: The embed Directive

**Difficulty:** Intermediate | **Estimated Time:** 25 minutes | **Section:** 19 - I/O and Filesystem

## Overview

Go 1.16 introduced `//go:embed` which lets you bake files and directories into your compiled binary at build time. No more worrying about file paths at runtime, no more asset bundling tools. The embedded data is available as strings, byte slices, or an `embed.FS` filesystem.

## Prerequisites

- File I/O basics (Exercise 01)
- `io/fs` interface awareness
- `text/template` or `html/template` basics (helpful but not required)

## Key Concepts

```go
import "embed"

// Embed a single file as a string
//go:embed version.txt
var version string

// Embed a single file as bytes
//go:embed logo.png
var logo []byte

// Embed a directory as a filesystem
//go:embed templates/*
var templateFS embed.FS

// Embed multiple patterns
//go:embed static/* templates/*
var assets embed.FS
```

Rules:
- The `//go:embed` comment must immediately precede the variable declaration (no blank line)
- The variable must be at package level, not inside a function
- Patterns are relative to the source file's directory
- Only `string`, `[]byte`, and `embed.FS` types are allowed
- `embed.FS` implements `io/fs.FS` -- it works with `http.FileServer`, `template.ParseFS`, etc.

## Task

Build a self-contained CLI tool with embedded assets:

### Setup: Create the asset files

Create this file structure in your exercise directory:

```
main.go
version.txt          -> "1.2.3"
config/default.yaml  -> (a sample YAML config)
templates/
  greeting.tmpl      -> "Hello, {{.Name}}! Welcome to {{.App}}."
  report.tmpl        -> "Report for {{.Date}}\n{{range .Items}}- {{.}}\n{{end}}"
static/
  style.css          -> "body { font-family: sans-serif; }"
  help.txt           -> (multi-line help text)
```

### Part 1: Embed single files

Embed `version.txt` as a `string`. Print the version on startup.

### Part 2: Embed and use templates

Embed the `templates/` directory as an `embed.FS`. Use `template.ParseFS` to load all templates. Execute each template with sample data and print the output.

### Part 3: List embedded files

Walk the embedded `embed.FS` using `fs.WalkDir` and print all embedded file paths and their sizes. This demonstrates that `embed.FS` implements `io/fs.FS`.

### Part 4: Serve embedded static files

Use `http.FileServer(http.FS(staticFS))` to serve the embedded static files over HTTP. Start the server, make a request with `http.Get`, print the response body, and shut down.

## Hints

- `template.ParseFS(fs, "templates/*.tmpl")` parses all matching templates from an embedded FS.
- `embed.FS` is read-only; you cannot modify embedded files at runtime.
- `fs.WalkDir(embedFS, ".", walkFunc)` walks the embedded filesystem.
- To read a single file from `embed.FS`: `data, err := fs.ReadFile(embedFS, "config/default.yaml")`.
- For the HTTP server, use `httptest.NewServer` for easy testing, or start a real server with a timeout.
- Embedded files do not include files starting with `.` or `_` unless explicitly named.
- The `all:` prefix includes hidden files: `//go:embed all:directory`.

## Verification

```
Version: 1.2.3

=== Templates ===
greeting: Hello, Alice! Welcome to MyApp.
report: Report for 2026-03-13
- Task 1
- Task 2
- Task 3

=== Embedded Files ===
config/default.yaml (45 bytes)
static/help.txt (128 bytes)
static/style.css (38 bytes)
templates/greeting.tmpl (47 bytes)
templates/report.tmpl (52 bytes)
version.txt (6 bytes)

=== Static Server ===
GET /style.css -> body { font-family: sans-serif; }
```

## Key Takeaways

- `//go:embed` bakes files into the binary at compile time -- no runtime file dependencies
- Three variable types: `string` (single file), `[]byte` (single file), `embed.FS` (directory tree)
- `embed.FS` implements `io/fs.FS`, making it compatible with `template.ParseFS`, `http.FS`, `fs.WalkDir`, etc.
- Embedded files are read-only and immutable
- This replaces third-party asset bundling tools for most use cases
- Patterns are relative to the Go source file's directory
