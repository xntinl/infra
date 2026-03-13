# 7. Output Formatting

<!--
difficulty: intermediate
concepts: [tabwriter, json-output, text-template, table-formatting, structured-output, output-modes]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [encoding-json, fmt-package, text-template]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [06 - Progress Bars and Spinners](../06-progress-bars-and-spinners/06-progress-bars-and-spinners.md)
- Familiarity with `encoding/json` and `fmt`

## Learning Objectives

After completing this exercise, you will be able to:

- **Format** tabular data with `text/tabwriter` for aligned columns
- **Output** structured data as JSON and JSON Lines
- **Implement** a `--output` flag to switch between table, JSON, and YAML formats
- **Use** `text/template` for custom output templates

## Why Output Formatting

Professional CLI tools support multiple output formats. Humans prefer aligned tables. Scripts and pipelines prefer JSON or YAML. Tools like `kubectl`, `docker`, and `gh` all support `--output` flags. Implementing this pattern makes your CLI useful both interactively and in automation.

## Step 1 -- Aligned Tables with tabwriter

```bash
mkdir -p ~/go-exercises/output-fmt
cd ~/go-exercises/output-fmt
go mod init output-fmt
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"os"
	"text/tabwriter"
)

type Task struct {
	ID       int
	Name     string
	Status   string
	Priority int
}

func printTable(tasks []Task) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tPRIORITY")
	fmt.Fprintln(w, "--\t----\t------\t--------")
	for _, t := range tasks {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\n", t.ID, t.Name, t.Status, t.Priority)
	}
	w.Flush()
}

func main() {
	tasks := []Task{
		{1, "Buy groceries", "active", 3},
		{2, "Write documentation", "active", 1},
		{3, "Fix login bug", "done", 5},
		{4, "Deploy to production", "active", 4},
	}

	printTable(tasks)
}
```

`tabwriter` aligns columns by inserting padding at tab characters. The arguments are: output, minwidth, tabwidth, padding, padchar, flags.

### Intermediate Verification

```bash
go run main.go
```

Expected:

```
ID  NAME                  STATUS  PRIORITY
--  ----                  ------  --------
1   Buy groceries         active  3
2   Write documentation   active  1
3   Fix login bug         done    5
4   Deploy to production  active  4
```

## Step 2 -- JSON Output

Add a JSON output mode:

```go
import "encoding/json"

func printJSON(tasks []Task) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(tasks)
}

func printJSONLines(tasks []Task) error {
	enc := json.NewEncoder(os.Stdout)
	for _, t := range tasks {
		if err := enc.Encode(t); err != nil {
			return err
		}
	}
	return nil
}
```

### Intermediate Verification

JSON output:

```json
[
  {
    "ID": 1,
    "Name": "Buy groceries",
    "Status": "active",
    "Priority": 3
  }
]
```

JSON Lines output (one object per line, ideal for piping to `jq`):

```
{"ID":1,"Name":"Buy groceries","Status":"active","Priority":3}
{"ID":2,"Name":"Write documentation","Status":"active","Priority":1}
```

## Step 3 -- Output Flag Pattern

Wire up a `--output` flag to switch formats:

```go
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"text/tabwriter"
	"text/template"
)

type Task struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

func printTable(tasks []Task) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tNAME\tSTATUS\tPRIORITY")
	for _, t := range tasks {
		fmt.Fprintf(w, "%d\t%s\t%s\t%d\n", t.ID, t.Name, t.Status, t.Priority)
	}
	w.Flush()
}

func printJSON(tasks []Task) {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(tasks)
}

func printTemplate(tasks []Task, tmpl string) error {
	t, err := template.New("output").Parse(tmpl)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		if err := t.Execute(os.Stdout, task); err != nil {
			return err
		}
	}
	return nil
}

func main() {
	output := flag.String("output", "table", "output format: table, json, template")
	tmpl := flag.String("template", "", "Go template string (used with -output=template)")
	flag.Parse()

	tasks := []Task{
		{1, "Buy groceries", "active", 3},
		{2, "Write documentation", "active", 1},
		{3, "Fix login bug", "done", 5},
		{4, "Deploy to production", "active", 4},
	}

	switch *output {
	case "table":
		printTable(tasks)
	case "json":
		printJSON(tasks)
	case "template":
		if *tmpl == "" {
			fmt.Fprintln(os.Stderr, "error: -template is required with -output=template")
			os.Exit(1)
		}
		if err := printTemplate(tasks, *tmpl); err != nil {
			fmt.Fprintf(os.Stderr, "template error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown output format: %s\n", *output)
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
go run main.go -output=table
go run main.go -output=json
go run main.go -output=template -template='{{.ID}}: {{.Name}} [{{.Status}}]\n'
```

Expected template output:

```
1: Buy groceries [active]
2: Write documentation [active]
3: Fix login bug [done]
4: Deploy to production [active]
```

## Step 4 -- Piping Friendly Output

Detect whether stdout is a terminal. If piped, default to JSON:

```go
import "golang.org/x/term"

func isTerminal() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// In main:
if *output == "" {
	if isTerminal() {
		*output = "table"
	} else {
		*output = "json"
	}
}
```

### Intermediate Verification

```bash
go run main.go | head -1
```

Expected: JSON output (because stdout is piped), not table output.

## Common Mistakes

### Not Flushing tabwriter

**Wrong:**

```go
w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
fmt.Fprintln(w, "COL1\tCOL2")
// forgot w.Flush() -- nothing is printed
```

**Fix:** Always call `w.Flush()` after writing all rows.

### Using json.Marshal for Large Output

**Wrong:** `json.Marshal` builds the entire JSON in memory, then prints it.

**Fix:** Use `json.NewEncoder(os.Stdout)` to stream directly to stdout.

### Inconsistent JSON Field Names

**Wrong:** Struct fields like `Priority` produce `{"Priority": 3}` in JSON. Scripts expecting `{"priority": 3}` break.

**Fix:** Add `json:"priority"` struct tags for consistent, lowercase JSON keys.

## Verify What You Learned

```bash
go run main.go -output=table
go run main.go -output=json | jq '.[0].name'
go run main.go -output=template -template='{{.Name}}\n'
```

Confirm: table output is aligned, JSON is parseable by `jq`, and templates render correctly.

## What's Next

Continue to [08 - Config Loading](../08-config-loading/08-config-loading.md) to learn how to load layered configuration from files, environment variables, and flags.

## Summary

- `text/tabwriter` aligns columns using tab characters and padding
- `json.NewEncoder` streams JSON output without buffering everything in memory
- The `--output` flag pattern lets users choose between table, JSON, and template formats
- `text/template` enables custom output formatting with Go templates
- Always call `Flush()` on `tabwriter` and use struct tags for consistent JSON keys

## Reference

- [text/tabwriter](https://pkg.go.dev/text/tabwriter)
- [encoding/json](https://pkg.go.dev/encoding/json)
- [text/template](https://pkg.go.dev/text/template)
