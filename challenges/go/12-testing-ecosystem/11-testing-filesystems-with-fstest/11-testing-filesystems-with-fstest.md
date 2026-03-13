# 11. Testing Filesystems with fstest

<!--
difficulty: intermediate
concepts: [fstest-mapfs, fs-interface, testing-without-disk, io-fs, embed]
tools: [go test]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-your-first-test, 08-mocking-with-interfaces]
-->

## Prerequisites

Before starting this exercise, you should be comfortable with:
- The `io/fs` interfaces (`fs.FS`, `fs.File`)
- Writing and running Go tests
- Working with files and directories in Go

## Learning Objectives

By the end of this exercise, you will be able to:
1. Use `testing/fstest.MapFS` to create in-memory filesystems for testing
2. Write code that accepts `fs.FS` instead of file paths for testability
3. Test file-processing logic without touching the real filesystem
4. Validate custom `fs.FS` implementations with `fstest.TestFS`

## Why This Matters

Code that reads files is hard to test. You need test files on disk, cleanup logic, and path management. The `io/fs` interface and `testing/fstest.MapFS` solve this by letting you create an in-memory filesystem in your test. Your production code accepts `fs.FS`, and your test code passes a `MapFS`. No files on disk, no cleanup, deterministic content.

## Instructions

You will build a template loader and a config reader that use `fs.FS`, then test them with `MapFS`.

### Scaffold

```bash
mkdir -p ~/go-exercises/fstest-testing && cd ~/go-exercises/fstest-testing
go mod init fstest-testing
```

`loader.go`:

```go
package loader

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"
)

// CountFiles counts files matching a glob pattern in the filesystem.
func CountFiles(fsys fs.FS, pattern string) (int, error) {
	matches, err := fs.Glob(fsys, pattern)
	if err != nil {
		return 0, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return len(matches), nil
}

// ReadConfig reads a config file from the filesystem and returns key=value pairs.
func ReadConfig(fsys fs.FS, path string) (map[string]string, error) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	config := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
	return config, nil
}

// ListTemplates returns all .html files under a directory in the filesystem.
func ListTemplates(fsys fs.FS, dir string) ([]string, error) {
	var templates []string
	err := fs.WalkDir(fsys, dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Ext(path) == ".html" {
			templates = append(templates, path)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walking %s: %w", dir, err)
	}
	return templates, nil
}
```

### Your Task

Create `loader_test.go`:

```go
package loader

import (
	"testing"
	"testing/fstest"
)

func TestCountFiles(t *testing.T) {
	fsys := fstest.MapFS{
		"docs/readme.md":    {Data: []byte("# Readme")},
		"docs/guide.md":     {Data: []byte("# Guide")},
		"docs/notes.txt":    {Data: []byte("notes")},
		"src/main.go":       {Data: []byte("package main")},
		"src/handler.go":    {Data: []byte("package main")},
	}

	tests := []struct {
		pattern string
		want    int
	}{
		{"docs/*.md", 2},
		{"src/*.go", 2},
		{"docs/*.txt", 1},
		{"*.yaml", 0},
	}

	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			count, err := CountFiles(fsys, tt.pattern)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if count != tt.want {
				t.Errorf("CountFiles(%q) = %d, want %d", tt.pattern, count, tt.want)
			}
		})
	}
}

func TestReadConfig(t *testing.T) {
	fsys := fstest.MapFS{
		"config/app.conf": {Data: []byte(`# Application config
host = localhost
port = 8080

# Debug mode
debug = true
`)},
	}

	config, err := ReadConfig(fsys, "config/app.conf")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := map[string]string{
		"host":  "localhost",
		"port":  "8080",
		"debug": "true",
	}

	for k, want := range expected {
		got := config[k]
		if got != want {
			t.Errorf("config[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestReadConfig_FileNotFound(t *testing.T) {
	fsys := fstest.MapFS{}
	_, err := ReadConfig(fsys, "missing.conf")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestListTemplates(t *testing.T) {
	fsys := fstest.MapFS{
		"templates/index.html":        {Data: []byte("<h1>Home</h1>")},
		"templates/about.html":        {Data: []byte("<h1>About</h1>")},
		"templates/partials/nav.html": {Data: []byte("<nav>Nav</nav>")},
		"templates/style.css":         {Data: []byte("body{}")},
		"other/page.html":             {Data: []byte("<h1>Other</h1>")},
	}

	templates, err := ListTemplates(fsys, "templates")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(templates) != 3 {
		t.Errorf("got %d templates, want 3: %v", len(templates), templates)
	}

	// Verify CSS is excluded
	for _, tmpl := range templates {
		if tmpl == "templates/style.css" {
			t.Error("style.css should not be in template list")
		}
	}
}

func TestMapFS_Validity(t *testing.T) {
	fsys := fstest.MapFS{
		"a.txt":     {Data: []byte("file a")},
		"dir/b.txt": {Data: []byte("file b")},
	}

	// fstest.TestFS validates that the FS implementation is correct
	if err := fstest.TestFS(fsys, "a.txt", "dir/b.txt"); err != nil {
		t.Fatal(err)
	}
}
```

### Verification

```bash
go test -v
```

All tests pass without creating any files on disk.

## Common Mistakes

1. **Leading slashes in MapFS paths**: MapFS paths must not start with `/`. Use `"config/app.conf"`, not `"/config/app.conf"`.

2. **Forgetting directory entries**: MapFS creates directories implicitly from file paths. You do not need to add directory entries unless you want empty directories.

3. **Accepting `string` paths instead of `fs.FS`**: If your function takes a file path string, you cannot test it with MapFS. Refactor to accept `fs.FS`.

4. **Not setting `Data` in MapFile**: A `MapFile` with no `Data` field represents an empty file, not a missing file. If you want a file to be absent, do not add it to the map.

## Verify What You Learned

1. What is the difference between `fstest.MapFS` and `os.DirFS`?
2. Why should production code accept `fs.FS` instead of file path strings?
3. What does `fstest.TestFS` validate?
4. How do you represent an empty directory in `MapFS`?

## What's Next

The next exercise covers **`t.Cleanup` patterns** -- registering cleanup functions that run when a test finishes.

## Summary

- `fstest.MapFS` creates an in-memory filesystem for testing
- Write production code against `fs.FS` for testability
- `MapFS` paths are slash-separated, no leading slash
- `fstest.TestFS` validates that an `fs.FS` implementation is correct
- No temp files, no cleanup, deterministic test data
- Use `os.DirFS(".")` in production to bridge to the real filesystem

## Reference

- [testing/fstest](https://pkg.go.dev/testing/fstest)
- [io/fs](https://pkg.go.dev/io/fs)
- [fstest.MapFS](https://pkg.go.dev/testing/fstest#MapFS)
- [fstest.TestFS](https://pkg.go.dev/testing/fstest#TestFS)
