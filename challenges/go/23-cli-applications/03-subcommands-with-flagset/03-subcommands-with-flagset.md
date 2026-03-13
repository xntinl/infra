# 3. Subcommands with FlagSet

<!--
difficulty: intermediate
concepts: [flag-newFlagSet, subcommands, os-args-routing, per-command-flags, usage-customization]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [flag-package-basics, custom-flag-types, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [02 - Custom Flag Types](../02-custom-flag-types/02-custom-flag-types.md)
- Understanding of `os.Args` and error handling

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** independent flag sets with `flag.NewFlagSet`
- **Route** subcommands by inspecting `os.Args[1]`
- **Define** per-subcommand flags without conflicts
- **Customize** usage output for each subcommand

## Why FlagSet

Tools like `git`, `docker`, and `kubectl` have subcommands: `git commit -m "msg"`, `docker run -p 8080:80`. Each subcommand has its own flags. The default `flag` package uses a single global flag set, which means all flags are shared. `flag.NewFlagSet` creates isolated flag sets so each subcommand can define its own flags independently.

## Step 1 -- Route Subcommands

```bash
mkdir -p ~/go-exercises/subcommands
cd ~/go-exercises/subcommands
go mod init subcommands
```

Create `main.go`:

```go
package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "expected subcommand: list or add")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "list":
		handleList(os.Args[2:])
	case "add":
		handleAdd(os.Args[2:])
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

func handleList(args []string) {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	all := listCmd.Bool("all", false, "show all items including archived")
	format := listCmd.String("format", "table", "output format (table|json)")

	listCmd.Parse(args)

	fmt.Printf("Listing items (all=%v, format=%s)\n", *all, *format)
}

func handleAdd(args []string) {
	addCmd := flag.NewFlagSet("add", flag.ExitOnError)
	name := addCmd.String("name", "", "item name (required)")
	priority := addCmd.Int("priority", 0, "item priority (0-5)")

	addCmd.Parse(args)

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: -name is required")
		addCmd.Usage()
		os.Exit(1)
	}

	fmt.Printf("Adding item: %s (priority=%d)\n", *name, *priority)
}
```

### Intermediate Verification

```bash
go run main.go list -all -format=json
```

Expected:

```
Listing items (all=true, format=json)
```

```bash
go run main.go add -name=groceries -priority=3
```

Expected:

```
Adding item: groceries (priority=3)
```

```bash
go run main.go add
```

Expected:

```
error: -name is required
Usage of add:
  -name string
    	item name (required)
  -priority int
    	item priority (0-5)
```

## Step 2 -- Custom Usage Function

Override the default usage message for better help output:

```go
func handleList(args []string) {
	listCmd := flag.NewFlagSet("list", flag.ExitOnError)
	listCmd.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: tasks list [options]\n\nList all tasks.\n\nOptions:\n")
		listCmd.PrintDefaults()
	}

	all := listCmd.Bool("all", false, "show all items including archived")
	format := listCmd.String("format", "table", "output format (table|json)")

	listCmd.Parse(args)

	fmt.Printf("Listing items (all=%v, format=%s)\n", *all, *format)
}
```

### Intermediate Verification

```bash
go run main.go list -h
```

Expected:

```
Usage: tasks list [options]

List all tasks.

Options:
  -all
    	show all items including archived
  -format string
    	output format (table|json) (default "table")
```

## Step 3 -- Error Handling Modes

`flag.NewFlagSet` accepts an error handling mode:

- `flag.ContinueOnError` -- returns the error, lets you handle it
- `flag.ExitOnError` -- prints the error and calls `os.Exit(2)`
- `flag.PanicOnError` -- panics on error

Use `ContinueOnError` for testable code:

```go
func handleAdd(args []string) error {
	addCmd := flag.NewFlagSet("add", flag.ContinueOnError)
	name := addCmd.String("name", "", "item name (required)")
	priority := addCmd.Int("priority", 0, "item priority (0-5)")

	if err := addCmd.Parse(args); err != nil {
		return err
	}

	if *name == "" {
		return fmt.Errorf("-name is required")
	}

	if *priority < 0 || *priority > 5 {
		return fmt.Errorf("priority must be 0-5, got %d", *priority)
	}

	fmt.Printf("Adding item: %s (priority=%d)\n", *name, *priority)
	return nil
}
```

### Intermediate Verification

```bash
go run main.go add -name=test -priority=7
```

Expected: error message about priority being out of range.

## Step 4 -- Nested Global and Subcommand Flags

Combine global flags (applied before the subcommand) with per-subcommand flags:

```go
func main() {
	verbose := flag.Bool("verbose", false, "enable verbose output")
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		fmt.Fprintln(os.Stderr, "expected subcommand: list or add")
		os.Exit(1)
	}

	if *verbose {
		fmt.Println("[verbose] starting...")
	}

	switch args[0] {
	case "list":
		handleList(args[1:])
	case "add":
		if err := handleAdd(args[1:]); err != nil {
			fmt.Fprintf(os.Stderr, "add error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", args[0])
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
go run main.go -verbose list -all
```

Expected:

```
[verbose] starting...
Listing items (all=true, format=table)
```

## Common Mistakes

### Passing os.Args Instead of os.Args[2:]

**Wrong:**

```go
listCmd.Parse(os.Args) // includes program name and subcommand name
```

**Fix:** Pass `os.Args[2:]` to skip the program name and subcommand name.

### Reusing the Global FlagSet

**Wrong:** Defining subcommand flags on the global `flag` package. All subcommands then share flags, and unrelated flags cause parse errors.

**Fix:** Create a separate `flag.NewFlagSet` for each subcommand.

### Forgetting to Call Parse on the FlagSet

**Wrong:** Defining flags on a `FlagSet` but never calling `Parse()`. Values remain at their defaults.

## Verify What You Learned

```bash
go run main.go list -h
go run main.go add -h
go run main.go list -all -format=json
go run main.go add -name=task1
go run main.go unknown
```

Confirm: each subcommand has its own help text, flags are independent, and unknown subcommands produce an error.

## What's Next

Continue to [04 - Cobra Commands, Flags, and Args](../04-cobra-commands-flags-args/04-cobra-commands-flags-args.md) to build professional CLI tools with the Cobra library.

## Summary

- `flag.NewFlagSet` creates an isolated flag set for a subcommand
- Route subcommands by switching on `os.Args[1]` and passing `os.Args[2:]` to the flag set
- Each flag set can have its own `Usage` function for custom help
- Use `flag.ContinueOnError` for testable command handlers that return errors
- Global flags go on the default `flag` package; subcommand flags go on their `FlagSet`

## Reference

- [flag.NewFlagSet](https://pkg.go.dev/flag#NewFlagSet)
- [flag.ErrorHandling](https://pkg.go.dev/flag#ErrorHandling)
