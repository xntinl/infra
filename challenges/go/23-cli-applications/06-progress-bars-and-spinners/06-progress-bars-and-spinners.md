# 6. Progress Bars and Spinners

<!--
difficulty: intermediate
concepts: [progress-bars, spinners, terminal-ui, bubbles, lip-gloss, concurrent-progress]
tools: [go]
estimated_time: 25m
bloom_level: apply
prerequisites: [goroutines, channels, interactive-prompts]
-->

## Prerequisites

- Go 1.22+ installed
- Completed [05 - Interactive Prompts](../05-interactive-prompts/05-interactive-prompts.md)
- Understanding of goroutines and channels

## Learning Objectives

After completing this exercise, you will be able to:

- **Display** a spinner during indeterminate operations
- **Show** a progress bar for operations with known total steps
- **Run** multiple progress indicators concurrently
- **Integrate** progress feedback into a real CLI workflow

## Why Progress Bars and Spinners

Long-running CLI operations without visual feedback feel broken. Users do not know whether the tool is working, stuck, or crashed. Spinners indicate indeterminate work ("loading..."), while progress bars show determinate progress ("42% complete"). Both dramatically improve the user experience of CLI tools.

## Step 1 -- Simple Spinner with huh

```bash
mkdir -p ~/go-exercises/progress
cd ~/go-exercises/progress
go mod init progress
go get github.com/charmbracelet/huh/spinner@latest
```

Create `main.go`:

```go
package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh/spinner"
)

func main() {
	action := func() {
		time.Sleep(3 * time.Second) // simulate work
	}

	err := spinner.New().
		Title("Downloading dependencies...").
		Action(action).
		Run()

	if err != nil {
		fmt.Printf("Error: %v\n", err)
		return
	}

	fmt.Println("Done!")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: a spinning animation with "Downloading dependencies..." for 3 seconds, then "Done!".

## Step 2 -- Sequential Spinners for Multi-Step Operations

Chain spinners for a multi-step workflow:

```go
package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/huh/spinner"
)

type step struct {
	title    string
	duration time.Duration
}

func main() {
	steps := []step{
		{"Fetching configuration...", 1 * time.Second},
		{"Building project...", 2 * time.Second},
		{"Running tests...", 2 * time.Second},
		{"Deploying to staging...", 1 * time.Second},
	}

	for i, s := range steps {
		err := spinner.New().
			Title(s.title).
			Action(func() {
				time.Sleep(s.duration)
			}).
			Run()

		if err != nil {
			fmt.Printf("Step %d failed: %v\n", i+1, err)
			return
		}
		fmt.Printf("  [%d/%d] %s done\n", i+1, len(steps), s.title)
	}

	fmt.Println("\nAll steps completed successfully!")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: each step shows a spinner, then prints a completion message before moving to the next.

## Step 3 -- Manual Progress Bar with Terminal Output

For a progress bar showing percentage, build one with standard output:

```go
package main

import (
	"fmt"
	"strings"
	"time"
)

func printProgress(current, total int, label string) {
	width := 40
	filled := width * current / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	percent := 100 * current / total
	fmt.Printf("\r%s [%s] %3d%%", label, bar, percent)
}

func main() {
	total := 50

	for i := 0; i <= total; i++ {
		printProgress(i, total, "Processing")
		time.Sleep(50 * time.Millisecond)
	}
	fmt.Println(" Done!")
}
```

Key detail: `\r` (carriage return) moves the cursor to the beginning of the line, overwriting the previous bar.

### Intermediate Verification

```bash
go run main.go
```

Expected: a progress bar that fills from 0% to 100%, then prints "Done!".

## Step 4 -- Progress with Real Work

Combine progress reporting with actual work using a channel:

```go
package main

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

func printProgress(current, total int, label string) {
	width := 40
	filled := width * current / total
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	percent := 100 * current / total
	fmt.Printf("\r%s [%s] %3d%% (%d/%d)", label, bar, percent, current, total)
}

func processFiles(files []string, progress chan<- int) {
	for i, f := range files {
		_ = f
		// Simulate variable processing time
		time.Sleep(time.Duration(50+rand.Intn(100)) * time.Millisecond)
		progress <- i + 1
	}
	close(progress)
}

func main() {
	files := make([]string, 30)
	for i := range files {
		files[i] = fmt.Sprintf("file_%03d.dat", i)
	}

	progress := make(chan int)
	go processFiles(files, progress)

	for completed := range progress {
		printProgress(completed, len(files), "Files")
	}
	fmt.Println(" Complete!")
}
```

### Intermediate Verification

```bash
go run main.go
```

Expected: a progress bar that updates as each "file" is processed, with slightly variable speed.

## Common Mistakes

### Not Using Carriage Return

**Wrong:** Using `\n` (newline) instead of `\r`. Each update prints a new line, flooding the terminal.

**Fix:** Use `\r` to overwrite the current line. Use `fmt.Print` not `fmt.Println`.

### Blocking the Main Thread

**Wrong:** Running the spinner's action on the main goroutine. The spinner cannot animate if the main thread is busy.

**Fix:** The spinner library runs the action in a goroutine internally. For manual progress bars, do work in a separate goroutine and send updates through a channel.

### Not Printing a Final Newline

**Wrong:** The program ends with `\r` output and no newline. The shell prompt overwrites the last progress line.

**Fix:** Print `\n` or `fmt.Println()` after the progress bar completes.

## Verify What You Learned

1. Run the spinner example and confirm the animation plays
2. Run the progress bar and confirm it fills to 100%
3. Resize your terminal and confirm the bar adapts to a fixed width
4. Press Ctrl+C during a spinner and confirm clean exit

## What's Next

Continue to [07 - Output Formatting](../07-output-formatting/07-output-formatting.md) to learn how to format CLI output as tables, JSON, and other structured formats.

## Summary

- Spinners show indeterminate progress for operations without a known total
- Progress bars show determinate progress as a percentage
- Use `\r` (carriage return) to overwrite the current terminal line
- Run work in a goroutine and report progress through a channel
- Always print a final newline after progress output completes
- `charmbracelet/huh/spinner` provides ready-made spinner animations

## Reference

- [charmbracelet/huh spinner](https://github.com/charmbracelet/huh)
- [Bubble Tea progress component](https://github.com/charmbracelet/bubbles/tree/master/progress)
- [Terminal control sequences](https://en.wikipedia.org/wiki/ANSI_escape_code)
