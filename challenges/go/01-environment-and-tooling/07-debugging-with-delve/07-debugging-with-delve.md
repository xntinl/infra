# 7. Debugging with Delve

<!--
difficulty: basic
concepts: [delve, dlv-debug, breakpoints, stepping, variable-inspection, debugging]
tools: [go, dlv]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-your-first-go-program]
-->

## Prerequisites

- Go 1.22+ installed
- A terminal and text editor

## Learning Objectives

After completing this exercise, you will be able to:

- **Install** and launch Delve (`dlv`)
- **Set** breakpoints and step through Go code
- **Inspect** variables, goroutines, and stack frames during debugging
- **Use** conditional breakpoints to stop at specific conditions

## Why Debugging with Delve

`fmt.Println` debugging works for simple issues, but as programs grow in complexity you need a real debugger. Delve is the Go-specific debugger that understands goroutines, channels, interfaces, and the Go runtime. It is the debugger used by VS Code, GoLand, and other IDEs under the hood.

Learning Delve at the command line gives you debugging skills that transfer to any editor. When a production issue requires attaching to a running process or debugging a core dump, the CLI debugger is often your only option. Even if you prefer a GUI, understanding the underlying commands makes you faster and more effective.

Delve is designed specifically for Go. Generic debuggers like GDB struggle with Go's goroutines and garbage collector. Delve handles them natively.

## Step 1 -- Install Delve

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
```

### Intermediate Verification

```bash
dlv version
```

Expected (version may differ):

```
Delve Debugger
Version: 1.22.1
Build: ...
```

## Step 2 -- Create a Program to Debug

```bash
mkdir -p ~/go-exercises/debug-demo
cd ~/go-exercises/debug-demo
go mod init debug-demo
```

Create `main.go`:

```go
package main

import "fmt"

func fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

func sumSlice(nums []int) int {
	total := 0
	for _, v := range nums {
		total += v
	}
	return total
}

func main() {
	for i := 0; i < 10; i++ {
		fmt.Printf("fib(%d) = %d\n", i, fibonacci(i))
	}

	nums := []int{10, 20, 30, 40, 50}
	sum := sumSlice(nums)
	fmt.Printf("Sum: %d\n", sum)
}
```

### Intermediate Verification

```bash
cd ~/go-exercises/debug-demo && go run main.go
```

Expected:

```
fib(0) = 0
fib(1) = 1
fib(2) = 1
fib(3) = 2
fib(4) = 3
fib(5) = 5
fib(6) = 8
fib(7) = 13
fib(8) = 21
fib(9) = 34
Sum: 150
```

## Step 3 -- Start a Debug Session

Launch Delve in debug mode:

```bash
cd ~/go-exercises/debug-demo
dlv debug
```

You are now in the Delve REPL. The program has not started yet. Type `help` to see available commands.

Key commands:

| Command | Short | Description |
|---|---|---|
| `break` | `b` | Set a breakpoint |
| `continue` | `c` | Run until breakpoint |
| `next` | `n` | Step over (next line) |
| `step` | `s` | Step into function |
| `print` | `p` | Print variable value |
| `locals` | | Show local variables |
| `stack` | `bt` | Print stack trace |
| `quit` | `q` | Exit debugger |

### Intermediate Verification

Inside `dlv`:

```
(dlv) help
```

Expected: a list of available commands.

## Step 4 -- Set Breakpoints and Step Through Code

Set a breakpoint at the `fibonacci` function:

```
(dlv) break fibonacci
Breakpoint 1 set at 0x... for main.fibonacci() ./main.go:5
```

Run the program:

```
(dlv) continue
> main.fibonacci() ./main.go:5 (hits goroutine(1):1 total:1)
```

Inspect the argument:

```
(dlv) print n
0
```

Continue to the next breakpoint hit:

```
(dlv) continue
> main.fibonacci() ./main.go:5 (hits goroutine(1):2 total:2)
(dlv) print n
1
```

### Intermediate Verification

```
(dlv) print n
```

Expected: the current value of `n` at each breakpoint hit.

## Step 5 -- Use Conditional Breakpoints

Clear existing breakpoints and set one that only triggers when `n == 5`:

```
(dlv) clearall
(dlv) break main.go:5
(dlv) condition 1 n == 5
(dlv) continue
```

Now Delve stops only when `fibonacci` is called with `n == 5`:

```
(dlv) print n
5
```

This is powerful for isolating specific iterations without manually continuing.

### Intermediate Verification

```
(dlv) print n
```

Expected:

```
5
```

## Step 6 -- Inspect Local Variables and Stack

Set a breakpoint in `sumSlice`:

```
(dlv) clearall
(dlv) break main.go:14
(dlv) continue
```

When stopped inside the loop:

```
(dlv) locals
total = 0
v = 10

(dlv) print nums
[]int len: 5, cap: 5, [10,20,30,40,50]

(dlv) stack
0  main.sumSlice() ./main.go:14
1  main.main() ./main.go:25
2  runtime.main() ...
```

Step through the loop with `next`:

```
(dlv) next
(dlv) print total
10
(dlv) next
(dlv) next
(dlv) print total
30
```

### Intermediate Verification

```
(dlv) print total
```

Expected: the running total as you step through iterations.

## Step 7 -- Exit the Debugger

```
(dlv) quit
```

If the program is still running, Delve will ask for confirmation. Type `y` to confirm.

### Intermediate Verification

You should be back at your normal shell prompt.

## Common Mistakes

### Debugging Optimized Binaries

**Wrong:** Building with optimizations and wondering why variables show `<optimized out>`.

**What happens:** The Go compiler inlines functions and eliminates variables. The debugger cannot show their values.

**Fix:** `dlv debug` disables optimizations automatically. If building manually, use:

```bash
go build -gcflags="all=-N -l" -o myapp .
```

### Setting Breakpoints by Line Without Checking

**Wrong:** Setting a breakpoint on a blank line or comment.

**What happens:** Delve moves the breakpoint to the nearest executable line, which may not be where you intended.

**Fix:** Set breakpoints on lines with actual code. Use `list` to see the source around a breakpoint:

```
(dlv) list main.go:14
```

### Forgetting to Quit Before Re-running

**Wrong:** Leaving a `dlv` session open and starting another one.

**What happens:** Port conflicts or confusion about which session is active.

**Fix:** Always `quit` the current session before starting a new one.

## Verify What You Learned

Start a debug session, set a breakpoint in `main`, and verify you can print variables:

```bash
cd ~/go-exercises/debug-demo
dlv debug
```

Inside `dlv`:

```
(dlv) break main.main
(dlv) continue
(dlv) next
(dlv) next
(dlv) next
(dlv) locals
(dlv) quit
```

You should see local variables like `i` and the output of the first `fibonacci` call.

## What's Next

Continue to [08 - Cross-Compilation and Build Tags](../08-cross-compilation-and-build-tags/08-cross-compilation-and-build-tags.md) to learn how to build Go programs for different platforms.

## Summary

- Delve (`dlv`) is the standard debugger for Go
- `dlv debug` compiles and starts a debug session with optimizations disabled
- `break`, `continue`, `next`, `step`, and `print` are the core commands
- Conditional breakpoints let you stop only when specific conditions are met
- `locals` shows all local variables; `stack` shows the call stack
- Delve understands goroutines, interfaces, and Go-specific types natively

## Reference

- [Delve documentation](https://github.com/go-delve/delve/tree/master/Documentation)
- [Delve command reference](https://github.com/go-delve/delve/blob/master/Documentation/cli/README.md)
- [Debugging Go with Delve](https://go.dev/doc/gdb) (also covers GDB)
