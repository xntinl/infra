# 9. Shell Completion Generation

<!--
difficulty: advanced
concepts: [shell-completion, bash-completion, zsh-completion, fish-completion, cobra-completion, dynamic-completion]
tools: [go, bash, zsh]
estimated_time: 30m
bloom_level: analyze
prerequisites: [cobra-commands-flags-args, subcommands-with-flagset]
-->

## Prerequisites

- Go 1.22+ installed
- Understanding of Cobra commands and subcommands
- A shell (bash, zsh, or fish)

## Learning Objectives

After completing this exercise, you will be able to:

- **Generate** shell completion scripts for bash, zsh, and fish using Cobra
- **Register** custom completions for flag values and positional arguments
- **Implement** dynamic completion functions that query live data
- **Install** completion scripts so they persist across sessions

## Why Shell Completion

Shell completion lets users press Tab to auto-complete commands, flags, and arguments. Without it, users must memorize every flag and subcommand. Professional CLI tools like `kubectl`, `docker`, and `gh` all provide completion scripts. Cobra generates these scripts automatically, and you can enhance them with dynamic completions.

## The Problem

You have a Cobra-based CLI tool. Add shell completion support so that users can Tab-complete subcommands, flags, and even dynamic values like task names fetched at runtime.

## Requirements

1. Add a `completion` subcommand that generates scripts for bash, zsh, and fish
2. Register static completions for enum-like flag values (e.g., log levels)
3. Implement a dynamic completion function for positional arguments
4. Document how users install the completion scripts

## Step 1 -- Add a Completion Command

```bash
mkdir -p ~/go-exercises/completion
cd ~/go-exercises/completion
go mod init completion
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
		Short: "A task manager",
	}

	completionCmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish]",
		Short: "Generate shell completion script",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			default:
				return fmt.Errorf("unsupported shell: %s", args[0])
			}
		},
	}

	addCmd := &cobra.Command{
		Use:   "add [name]",
		Short: "Add a task",
		Args:  cobra.ExactArgs(1),
		Run: func(cmd *cobra.Command, args []string) {
			priority, _ := cmd.Flags().GetString("priority")
			fmt.Printf("Added: %s (priority=%s)\n", args[0], priority)
		},
	}
	addCmd.Flags().StringP("priority", "p", "medium", "task priority")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("Listing tasks...")
		},
	}

	rootCmd.AddCommand(completionCmd, addCmd, listCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
```

### Intermediate Verification

```bash
go run main.go completion bash > /tmp/tasks-completion.bash
head -5 /tmp/tasks-completion.bash
```

Expected: the first few lines of a bash completion script.

## Step 2 -- Register Static Flag Completions

Tell Cobra the valid values for a flag:

```go
addCmd.Flags().StringP("priority", "p", "medium", "task priority")
addCmd.RegisterFlagCompletionFunc("priority", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{"low", "medium", "high", "critical"}, cobra.ShellCompDirectiveNoFileComp
})

addCmd.Flags().StringP("status", "s", "todo", "task status")
addCmd.RegisterFlagCompletionFunc("status", func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"todo\tnot started",
		"in-progress\tcurrently working",
		"done\tcompleted",
		"blocked\twaiting on dependency",
	}, cobra.ShellCompDirectiveNoFileComp
})
```

The `\t` separator adds a description that some shells display alongside the completion.

### Intermediate Verification

```bash
go build -o tasks .
./tasks __complete add -- -p ""
```

Expected: `low`, `medium`, `high`, `critical` appear as completions.

## Step 3 -- Dynamic Positional Argument Completion

Complete positional arguments with live data (e.g., existing task names):

```go
// Simulate fetching existing tasks
func getExistingTasks() []string {
	return []string{"buy-groceries", "fix-bug-123", "write-docs", "deploy-v2"}
}

doneCmd := &cobra.Command{
	Use:   "done [task-name]",
	Short: "Mark a task as done",
	Args:  cobra.ExactArgs(1),
	ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != 0 {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return getExistingTasks(), cobra.ShellCompDirectiveNoFileComp
	},
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Marked %s as done\n", args[0])
	},
}

rootCmd.AddCommand(doneCmd)
```

### Intermediate Verification

```bash
./tasks __complete done ""
```

Expected: `buy-groceries`, `fix-bug-123`, `write-docs`, `deploy-v2`.

## Step 4 -- Install Completion Scripts

Document the installation for each shell:

**Bash:**

```bash
# Add to ~/.bashrc
eval "$(tasks completion bash)"

# Or install system-wide
tasks completion bash > /etc/bash_completion.d/tasks
```

**Zsh:**

```bash
# Add to ~/.zshrc
eval "$(tasks completion zsh)"

# Or install to fpath
tasks completion zsh > "${fpath[1]}/_tasks"
```

**Fish:**

```bash
tasks completion fish > ~/.config/fish/completions/tasks.fish
```

## Hints

- `cobra.ShellCompDirectiveNoFileComp` prevents falling back to file completion
- `cobra.ShellCompDirectiveDefault` allows normal file completion alongside custom values
- Return `cobra.ShellCompDirectiveFilterFileExt` with extensions to filter file completions
- The `__complete` hidden subcommand is used internally by completion scripts to query completions

## Verification

1. Build the binary: `go build -o tasks .`
2. Source the completion: `source <(./tasks completion bash)` (or zsh/fish equivalent)
3. Type `./tasks ` and press Tab -- subcommands appear
4. Type `./tasks add -p ` and press Tab -- priority values appear
5. Type `./tasks done ` and press Tab -- existing task names appear

## What's Next

Continue to [10 - Building a Complete CLI Tool](../10-building-a-complete-cli-tool/10-building-a-complete-cli-tool.md) to combine everything into a production-quality CLI application.

## Summary

- Cobra generates bash, zsh, and fish completion scripts with one method call
- `RegisterFlagCompletionFunc` provides completions for flag values
- `ValidArgsFunction` provides dynamic completions for positional arguments
- Shell directives control whether file completion is offered alongside custom values
- Descriptions can be added to completions using `\t` separators
- Users install completion by sourcing the script or saving it to their shell's completion directory

## Reference

- [Cobra shell completions](https://github.com/spf13/cobra/blob/main/site/content/completions/_index.md)
- [Bash completion guide](https://www.gnu.org/software/bash/manual/html_node/Programmable-Completion.html)
- [Zsh completion system](https://zsh.sourceforge.io/Doc/Release/Completion-System.html)
