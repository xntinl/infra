# 5. Interactive Prompts

<!--
difficulty: intermediate
concepts: [interactive-cli, huh-library, text-input, select-prompt, confirm-prompt, form-groups]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [cobra-commands-flags-args, structs-and-methods]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [04 - Cobra Commands, Flags, and Args](../04-cobra-commands-flags-args/04-cobra-commands-flags-args.md)
- Understanding of structs and interfaces

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** interactive text inputs, selects, confirms, and multi-selects using `charmbracelet/huh`
- **Compose** multiple prompts into a form with groups
- **Validate** user input within prompts
- **Bind** prompt results to Go variables

## Why Interactive Prompts

Not every CLI input fits neatly into flags. When creating a new project, deploying to production, or configuring settings, an interactive prompt guides the user through choices step by step. The `charmbracelet/huh` library provides beautiful, accessible terminal forms with validation, theming, and keyboard navigation.

## Step 1 -- Basic Text and Confirm Prompts

```bash
mkdir -p ~/go-exercises/interactive
cd ~/go-exercises/interactive
go mod init interactive
go get github.com/charmbracelet/huh@latest
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"log"

	"github.com/charmbracelet/huh"
)

func main() {
	var name string
	var confirm bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("What is your name?").
				Value(&name).
				Validate(func(s string) error {
					if len(s) == 0 {
						return fmt.Errorf("name cannot be empty")
					}
					return nil
				}),

			huh.NewConfirm().
				Title("Ready to continue?").
				Value(&confirm),
		),
	)

	err := form.Run()
	if err != nil {
		log.Fatal(err)
	}

	if confirm {
		fmt.Printf("Welcome, %s!\n", name)
	} else {
		fmt.Println("Maybe next time.")
	}
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: an interactive prompt asking for your name, then a confirmation. Type a name and press Enter, then select Yes/No. The result is printed.

## Step 2 -- Select and Multi-Select Prompts

Add selection prompts for structured choices:

```go
package main

import (
	"fmt"
	"log"
	"strings"

	"github.com/charmbracelet/huh"
)

func main() {
	var (
		projectName string
		language    string
		features    []string
		confirm     bool
	)

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Project name").
				Value(&projectName).
				Validate(func(s string) error {
					if len(s) < 2 {
						return fmt.Errorf("name must be at least 2 characters")
					}
					return nil
				}),

			huh.NewSelect[string]().
				Title("Language").
				Options(
					huh.NewOption("Go", "go"),
					huh.NewOption("Rust", "rust"),
					huh.NewOption("Python", "python"),
					huh.NewOption("TypeScript", "typescript"),
				).
				Value(&language),
		),

		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Features").
				Options(
					huh.NewOption("CI/CD Pipeline", "cicd"),
					huh.NewOption("Docker Support", "docker"),
					huh.NewOption("Linting", "lint"),
					huh.NewOption("Testing Framework", "test"),
				).
				Value(&features),

			huh.NewConfirm().
				Title("Create project?").
				Value(&confirm),
		),
	)

	err := form.Run()
	if err != nil {
		log.Fatal(err)
	}

	if !confirm {
		fmt.Println("Cancelled.")
		return
	}

	fmt.Printf("\nCreating project: %s\n", projectName)
	fmt.Printf("Language: %s\n", language)
	fmt.Printf("Features: %s\n", strings.Join(features, ", "))
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: a two-page form. First page asks for project name and language (arrow keys to select). Second page asks for features (space to toggle, enter to confirm) and confirmation.

## Step 3 -- Conditional Prompts with Accessible Mode

For non-interactive environments (CI, piped input), use accessible mode:

```go
form := huh.NewForm(
	huh.NewGroup(
		huh.NewInput().
			Title("Name").
			Value(&name),
	),
).WithAccessible(true)
```

Accessible mode uses simple line-based input instead of the TUI, making it work in environments without terminal capabilities.

### Intermediate Verification

```bash
echo "TestProject" | go run main.go
```

With accessible mode enabled, the form accepts piped input.

## Step 4 -- Themed Prompts

Apply a built-in theme:

```go
form := huh.NewForm(
	huh.NewGroup(
		huh.NewInput().
			Title("Name").
			Value(&name),
	),
).WithTheme(huh.ThemeCatppuccin())
```

Available themes include `ThemeBase()`, `ThemeCharm()`, `ThemeDracula()`, and `ThemeCatppuccin()`.

### Intermediate Verification

```bash
go run main.go
```

Expected: the prompt renders with the selected theme's color scheme.

## Common Mistakes

### Forgetting to Check the Error from Run

**Wrong:**

```go
form.Run()
// user pressed Ctrl+C but code continues
```

**Fix:** Always check the error. `huh` returns `huh.ErrUserAborted` when the user presses Ctrl+C.

### Not Initializing Pointer Values

**Wrong:** Passing an uninitialized pointer to `Value()`. The form has nowhere to store the result.

**Fix:** Declare variables before the form and pass their addresses with `&`.

### Mixing Interactive and Non-Interactive Flows

**Wrong:** Using interactive prompts inside a script without detecting whether stdin is a terminal.

**Fix:** Check `os.Stdin` with `term.IsTerminal()` or use the `--no-input` flag pattern to skip prompts and require flags instead.

## Verify What You Learned

Run the program and test:
1. Empty name triggers validation error
2. Arrow keys navigate select options
3. Space toggles multi-select options
4. Ctrl+C aborts the form
5. Confirmation controls whether the project is created

## What's Next

Continue to [06 - Progress Bars and Spinners](../06-progress-bars-and-spinners/06-progress-bars-and-spinners.md) to add visual progress feedback to long-running CLI operations.

## Summary

- `huh.NewForm` composes groups of prompts into a multi-page form
- `NewInput`, `NewSelect`, `NewMultiSelect`, and `NewConfirm` cover common prompt types
- `Validate` adds inline validation with error messages
- `Value(&variable)` binds the prompt result to a Go variable
- Use `WithAccessible(true)` for non-interactive environments
- Always handle the error from `form.Run()` to catch user aborts

## Reference

- [charmbracelet/huh GitHub](https://github.com/charmbracelet/huh)
- [Charm documentation](https://charm.sh/)
- [huh examples](https://github.com/charmbracelet/huh/tree/main/examples)
