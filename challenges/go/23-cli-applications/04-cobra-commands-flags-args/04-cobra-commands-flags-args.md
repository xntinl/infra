# 4. Cobra Commands, Flags, and Args

<!--
difficulty: intermediate
concepts: [cobra, command-tree, persistent-flags, local-flags, args-validation, cobra-init]
tools: [go]
estimated_time: 30m
bloom_level: apply
prerequisites: [flag-package-basics, subcommands-with-flagset, go-modules]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [03 - Subcommands with FlagSet](../03-subcommands-with-flagset/03-subcommands-with-flagset.md)
- Familiarity with Go modules and third-party dependencies

## Learning Objectives

After completing this exercise, you will be able to:

- **Build** a CLI application using `cobra.Command`
- **Define** persistent flags (inherited by subcommands) and local flags
- **Validate** positional arguments with Cobra's built-in validators
- **Organize** a multi-command CLI with parent-child relationships

## Why Cobra

The standard `flag` package requires manual subcommand routing, custom help formatting, and boilerplate validation. Cobra is the most widely used Go CLI framework -- used by `kubectl`, `hugo`, `gh`, and `docker`. It provides a declarative command tree, automatic help generation, shell completion, and argument validation out of the box.

## Step 1 -- Create a Basic Cobra Command

```bash
mkdir -p ~/go-exercises/cobra-cli
cd ~/go-exercises/cobra-cli
go mod init cobra-cli
go get github.com/spf13/cobra@latest
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "tasks",
		Short: "A task management CLI",
		Long:  "Tasks is a command-line tool for managing your to-do list.",
	}

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: help text showing "Tasks is a command-line tool for managing your to-do list."

```bash
go run main.go --help
```

Expected: auto-generated help with usage, description, and available commands section.

## Step 2 -- Add Subcommands

```go
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var verbose bool

func main() {
	rootCmd := &cobra.Command{
		Use:   "tasks",
		Short: "A task management CLI",
	}

	// Persistent flag -- inherited by all subcommands
	rootCmd.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "enable verbose output")

	addCmd := &cobra.Command{
		Use:   "add [task name]",
		Short: "Add a new task",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			priority, _ := cmd.Flags().GetInt("priority")
			if verbose {
				fmt.Printf("[verbose] Adding task with priority %d\n", priority)
			}
			fmt.Printf("Added task: %s (priority=%d)\n", args[0], priority)
		},
	}
	addCmd.Flags().IntP("priority", "p", 0, "task priority (0-5)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List all tasks",
		Run: func(cmd *cobra.Command, args []string) {
			all, _ := cmd.Flags().GetBool("all")
			if verbose {
				fmt.Println("[verbose] Listing tasks...")
			}
			if all {
				fmt.Println("Showing all tasks (including completed)")
			} else {
				fmt.Println("Showing active tasks only")
			}
		},
	}
	listCmd.Flags().BoolP("all", "a", false, "show all tasks including completed")

	rootCmd.AddCommand(addCmd, listCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
go run main.go add "Buy groceries" -p 3
```

Expected:

```
Added task: Buy groceries (priority=3)
```

```bash
go run main.go -v list --all
```

Expected:

```
[verbose] Listing tasks...
Showing all tasks (including completed)
```

```bash
go run main.go add
```

Expected: error message "accepts 1 arg(s), received 0".

## Step 3 -- Argument Validation

Cobra provides built-in argument validators:

```go
doneCmd := &cobra.Command{
	Use:   "done [task IDs...]",
	Short: "Mark tasks as done",
	Args:  cobra.MinimumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		for _, id := range args {
			fmt.Printf("Marked task %s as done\n", id)
		}
	},
}

deleteCmd := &cobra.Command{
	Use:   "delete [task ID]",
	Short: "Delete a task",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		force, _ := cmd.Flags().GetBool("force")
		if !force {
			return fmt.Errorf("use --force to confirm deletion of task %s", args[0])
		}
		fmt.Printf("Deleted task %s\n", args[0])
		return nil
	},
}
deleteCmd.Flags().BoolP("force", "f", false, "force deletion without confirmation")

rootCmd.AddCommand(doneCmd, deleteCmd)
```

Common validators:
- `cobra.NoArgs` -- no positional arguments allowed
- `cobra.ExactArgs(n)` -- exactly n arguments
- `cobra.MinimumNArgs(n)` -- at least n arguments
- `cobra.MaximumNArgs(n)` -- at most n arguments
- `cobra.RangeArgs(min, max)` -- between min and max arguments

### Intermediate Verification

```bash
go run main.go done 1 2 3
```

Expected:

```
Marked task 1 as done
Marked task 2 as done
Marked task 3 as done
```

```bash
go run main.go delete abc
```

Expected: `Error: use --force to confirm deletion of task abc`

```bash
go run main.go delete abc --force
```

Expected: `Deleted task abc`

## Step 4 -- Persistent vs Local Flags

```go
// Persistent: available to this command AND all children
rootCmd.PersistentFlags().StringVar(&outputFormat, "output", "text", "output format")

// Local: available ONLY to this specific command
addCmd.Flags().IntP("priority", "p", 0, "task priority")
```

Persistent flags propagate down the command tree. Use them for global settings like `--verbose`, `--output`, or `--config`. Local flags are for command-specific options.

### Intermediate Verification

```bash
go run main.go add --help
```

Expected: shows both the local `-p/--priority` flag and the inherited `--verbose` and `--output` flags.

## Common Mistakes

### Using Run Instead of RunE

**Wrong:** Using `Run` when you need to return errors. `Run` ignores errors silently.

**Fix:** Use `RunE` to return errors. Cobra prints the error and exits with code 1.

### Defining Persistent Flags on a Subcommand

**Wrong:** Putting persistent flags on a leaf command where they have no children to inherit them.

**Fix:** Put persistent flags on the root or on a parent command that has subcommands.

### Forgetting Short-Hand Flags

**Wrong:** `cmd.Flags().Bool("verbose", false, "verbose")` -- no short form.

**Fix:** Use `BoolVarP` or `BoolP` with a short character: `BoolVarP(&v, "verbose", "v", false, "verbose")`.

## Verify What You Learned

```bash
go run main.go --help
go run main.go add --help
go run main.go list --help
go run main.go add "task" -p 5 -v
go run main.go done
```

Confirm: help is auto-generated per command, flags work, and argument validation catches errors.

## What's Next

Continue to [05 - Interactive Prompts](../05-interactive-prompts/05-interactive-prompts.md) to add interactive user input with the `huh` library.

## Summary

- `cobra.Command` defines a command with `Use`, `Short`, `Long`, `Args`, and `Run`/`RunE`
- `AddCommand` builds a parent-child command tree
- `PersistentFlags` are inherited by subcommands; `Flags` are local to one command
- `Args` validators like `ExactArgs` and `MinimumNArgs` enforce argument counts
- Use `RunE` instead of `Run` to propagate errors cleanly
- Cobra auto-generates help, usage text, and error messages

## Reference

- [Cobra documentation](https://cobra.dev/)
- [Cobra GitHub](https://github.com/spf13/cobra)
- [User Guide](https://github.com/spf13/cobra/blob/main/site/content/user_guide.md)
