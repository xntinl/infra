# 19. Golden File Testing

<!--
difficulty: advanced
concepts: [golden-files, testdata, update-flag, snapshot-testing, text-output-verification, diff-comparison]
tools: [go test]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-your-first-test, 07-test-fixtures-and-testdata, 04-subtests-and-t-run]
-->

## Prerequisites

- Go 1.22+ installed
- Familiarity with `testdata/` directory conventions
- Understanding of `os.ReadFile` and `os.WriteFile`

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a golden file test pattern that compares function output against reference files
- **Use** an `-update` flag to regenerate golden files when output intentionally changes
- **Apply** golden file testing to structured outputs like JSON, HTML, and CLI help text
- **Evaluate** when golden file testing is appropriate versus inline assertions

## The Problem

You are building a report generator that produces formatted text, JSON, and HTML output. The output is complex enough that inline assertions would be fragile and unreadable. Golden file testing solves this: you store the expected output in a file (the "golden file"), and the test compares the actual output against it. When the output intentionally changes, you run the test with `-update` to regenerate the golden files.

Build a report generator and a reusable golden file test helper.

## Requirements

1. **Create a golden file helper** that handles reading, comparing, and updating golden files:

```go
// golden.go
package report

import (
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var update = flag.Bool("update", false, "update golden files")

// Golden compares got against the golden file at testdata/<name>.golden.
// If -update is set, it writes got to the golden file instead.
func Golden(t *testing.T, name string, got []byte) {
	t.Helper()

	goldenPath := filepath.Join("testdata", name+".golden")

	if *update {
		dir := filepath.Dir(goldenPath)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("creating testdata dir: %v", err)
		}
		if err := os.WriteFile(goldenPath, got, 0o644); err != nil {
			t.Fatalf("updating golden file: %v", err)
		}
		t.Logf("updated golden file: %s", goldenPath)
		return
	}

	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("reading golden file %s: %v\nRun with -update to create it.", goldenPath, err)
	}

	if string(got) != string(want) {
		// Show a useful diff
		t.Errorf("output does not match golden file %s\n"+
			"To update, run: go test -run %s -update\n\n"+
			"GOT:\n%s\n\nWANT:\n%s",
			goldenPath, t.Name(), got, want)
	}
}
```

2. **Create a report generator** with multiple output formats:

```go
// report.go
package report

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/template"
	"bytes"
)

type SalesReport struct {
	Title   string
	Quarter string
	Regions []RegionData
	Total   float64
}

type RegionData struct {
	Name    string
	Revenue float64
	Growth  float64
}

func (r *SalesReport) Text() string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("=== %s ===\n", r.Title))
	b.WriteString(fmt.Sprintf("Quarter: %s\n\n", r.Quarter))
	b.WriteString(fmt.Sprintf("%-15s %12s %8s\n", "Region", "Revenue", "Growth"))
	b.WriteString(strings.Repeat("-", 37) + "\n")
	for _, region := range r.Regions {
		b.WriteString(fmt.Sprintf("%-15s %12.2f %7.1f%%\n",
			region.Name, region.Revenue, region.Growth))
	}
	b.WriteString(strings.Repeat("-", 37) + "\n")
	b.WriteString(fmt.Sprintf("%-15s %12.2f\n", "TOTAL", r.Total))
	return b.String()
}

func (r *SalesReport) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func (r *SalesReport) HTML() (string, error) {
	const tmpl = `<!DOCTYPE html>
<html>
<head><title>{{.Title}}</title></head>
<body>
<h1>{{.Title}}</h1>
<p>Quarter: {{.Quarter}}</p>
<table>
<tr><th>Region</th><th>Revenue</th><th>Growth</th></tr>
{{- range .Regions}}
<tr><td>{{.Name}}</td><td>{{printf "%.2f" .Revenue}}</td><td>{{printf "%.1f" .Growth}}%</td></tr>
{{- end}}
<tr><td><strong>TOTAL</strong></td><td><strong>{{printf "%.2f" .Total}}</strong></td><td></td></tr>
</table>
</body>
</html>`

	t, err := template.New("report").Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, r); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}
	return buf.String(), nil
}
```

3. **Write golden file tests** for each output format:

```go
// report_test.go
package report

import "testing"

func sampleReport() *SalesReport {
	return &SalesReport{
		Title:   "Annual Sales Report",
		Quarter: "Q4 2024",
		Regions: []RegionData{
			{Name: "North America", Revenue: 1250000.50, Growth: 12.3},
			{Name: "Europe", Revenue: 890000.75, Growth: 8.7},
			{Name: "Asia Pacific", Revenue: 650000.25, Growth: 22.1},
			{Name: "Latin America", Revenue: 320000.00, Growth: 15.4},
		},
		Total: 3110001.50,
	}
}

func TestSalesReport_Text(t *testing.T) {
	report := sampleReport()
	got := report.Text()
	Golden(t, "sales_text", []byte(got))
}

func TestSalesReport_JSON(t *testing.T) {
	report := sampleReport()
	got, err := report.JSON()
	if err != nil {
		t.Fatalf("JSON() error: %v", err)
	}
	Golden(t, "sales_json", got)
}

func TestSalesReport_HTML(t *testing.T) {
	report := sampleReport()
	got, err := report.HTML()
	if err != nil {
		t.Fatalf("HTML() error: %v", err)
	}
	Golden(t, "sales_html", []byte(got))
}

func TestSalesReport_EmptyRegions(t *testing.T) {
	report := &SalesReport{
		Title:   "Empty Report",
		Quarter: "Q1 2025",
		Total:   0,
	}
	got := report.Text()
	Golden(t, "sales_empty", []byte(got))
}
```

4. **Generate the initial golden files** and verify the workflow:

```bash
# First run: create golden files
go test -update

# Second run: verify they match
go test -v
```

## Hints

- `testdata/` is a special directory that `go test` ignores when looking for packages. It is the standard place for test fixtures and golden files.
- The `-update` flag is a convention, not built into Go. You register it with `flag.Bool` and `go test` passes it through.
- For large outputs, consider using `go-cmp` or `diff` to produce a readable comparison instead of dumping the full got/want strings.
- Golden files should be committed to version control. Reviewers can see exactly what changed in the output during code review.
- Avoid putting non-deterministic data (timestamps, random IDs) in golden file output. Either inject fixed values or strip them before comparison.
- When testing JSON output, `json.MarshalIndent` produces deterministic output for structs (field order is defined by struct definition).

## Verification

```bash
# Generate golden files
go test -update

# Verify all tests pass
go test -v

# Intentionally change output and see the test fail
# (edit report.go to change formatting, then run go test)

# Update after intentional change
go test -update

# Verify clean again
go test -v
```

## What's Next

Continue to [20 - Test Coverage Analysis](../20-test-coverage-analysis/20-test-coverage-analysis.md) to learn how to measure, visualize, and interpret test coverage in Go.

## Summary

- Golden file testing compares actual output against a committed reference file
- Use `-update` flag to regenerate golden files when output intentionally changes
- Store golden files in `testdata/` -- Go ignores this directory for package discovery
- Golden files are ideal for complex, multi-line output: formatted text, JSON, HTML, CLI help
- Commit golden files to version control so output changes are visible in code review
- Avoid non-deterministic data in golden file output (timestamps, random values)
- The helper function pattern (`Golden(t, name, got)`) keeps tests concise

## Reference

- [Go test fixtures convention](https://pkg.go.dev/cmd/go#hdr-Test_packages)
- [testing/fstest](https://pkg.go.dev/testing/fstest)
- [Mitchell Hashimoto: Advanced Testing with Go (GopherCon 2017)](https://www.youtube.com/watch?v=8hQG7QlcLBk)
