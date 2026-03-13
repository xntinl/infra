<!--
difficulty: insane
bloom_level: create
tools: [go]
estimated_time: 40h
-->

# Interactive Debugger with ptrace

## The Challenge

Build an interactive source-level debugger for Go programs, similar to Delve (dlv) or GDB, using the `ptrace` system call for process control and DWARF debug information for source-level mapping. Your debugger must be able to launch or attach to a Go process, set breakpoints by source file and line number, single-step through code, inspect and modify variables, print stack traces with function names and argument values, evaluate simple expressions in the context of the current frame, and handle Go-specific features like goroutine inspection. This is a monumental project that integrates low-level process control (ptrace), binary format parsing (ELF), debug information parsing (DWARF), and user interface design into a single cohesive tool.

## Requirements

1. Implement process control: launch a target Go binary under ptrace (`exec.Command` with `SysProcAttr.Ptrace = true`) or attach to a running process with `PTRACE_ATTACH`; implement `continue`, `step` (single instruction), `next` (step over function calls), and `stepi` (single machine instruction) commands.
2. Parse the target binary's ELF format to extract section headers, the symbol table (`.symtab`), and the DWARF debug information (`.debug_info`, `.debug_line`, `.debug_abbrev`, `.debug_str`, `.debug_ranges`) using Go's `debug/elf` and `debug/dwarf` packages.
3. Implement source-line-to-address mapping: parse the DWARF `.debug_line` section to build a bidirectional mapping between source file:line positions and machine code addresses, enabling breakpoints by source location and displaying the current source line during execution.
4. Implement breakpoints: write an `INT3` instruction (`0xCC` on x86_64) at the target address using `PTRACE_POKETEXT`, save the original byte, and restore it when the breakpoint is hit; handle the one-byte instruction pointer adjustment needed after `INT3` fires.
5. Implement variable inspection: parse DWARF `.debug_info` entries to find local variables and function parameters in the current scope, read their values from the target's memory (stack frame or registers) based on DWARF location expressions (`DW_AT_location`), and format them according to their type (int, string, struct, slice, pointer).
6. Implement stack trace (`backtrace`): walk the stack frames using the frame pointer chain (or DWARF `.debug_frame` CFI -- Call Frame Information), resolving return addresses to function names and source locations using the symbol table and line table.
7. Implement an interactive REPL with these commands: `break <file>:<line>` (set breakpoint), `continue` (resume), `step`/`next`/`stepi` (stepping), `print <var>` (inspect variable), `backtrace` (stack trace), `list` (show source around current line), `info breakpoints` (list breakpoints), `delete <n>` (remove breakpoint), `goroutines` (list goroutines), and `quit`.
8. Implement basic goroutine awareness: parse the Go runtime's internal data structures (the `runtime.allgs` slice and `runtime.g` struct) from the target's memory to list all goroutines with their IDs, status (running, waiting, syscall), and current function.

## Hints

- `runtime.LockOSThread()` is mandatory in the debugger's ptrace goroutine.
- On x86_64, after `INT3` fires, the instruction pointer (RIP) is one byte past the breakpoint; decrement it by 1, restore the original byte, single-step past it, re-insert the breakpoint, then continue.
- DWARF location expressions are a stack-based mini-language; the most common forms are `DW_OP_fbreg N` (offset N from the frame base) and `DW_OP_reg N` (register N). Use `debug/dwarf` to parse entries, but you may need to interpret location expressions manually.
- Go strings are `struct { ptr *byte; len int }`; read the pointer and length, then read the string data from the pointer address.
- Go slices are `struct { array *T; len int; cap int }`; read the header, then read elements from the array pointer.
- For goroutine inspection, find the `runtime.allgs` global variable's address from the symbol table, read the slice header, then read each `g` struct to extract the goroutine ID (`goid`) and status (`atomicstatus`).
- Use `debug/elf` to find the `.text` section base address and compute virtual addresses for breakpoints.
- The `next` command (step over) is implemented by setting a temporary breakpoint at the next source line's address and continuing.

## Success Criteria

1. The debugger launches a Go binary, hits a breakpoint set at `main.go:10`, and stops execution at that line.
2. The `list` command shows the source code around the current line with the current line highlighted.
3. The `print` command correctly displays the values of `int`, `string`, `bool`, `float64`, `struct`, and `slice` variables in the current scope.
4. The `backtrace` command shows the call stack with function names, file names, and line numbers.
5. The `step` command advances to the next source line, including stepping into function calls.
6. The `next` command steps over function calls, stopping at the next line in the current function.
7. The `goroutines` command lists all goroutines with their IDs and current function names.
8. Multiple breakpoints can be set and independently enabled/disabled, with the debugger stopping at each one correctly.
9. The debugger handles the target program's normal exit and abnormal exit (panic) gracefully, displaying the exit status or panic message.

## Research Resources

- Delve debugger source code -- https://github.com/go-delve/delve -- the reference Go debugger
- "Writing a Debugger" series by Liz Rice -- https://www.youtube.com/watch?v=ZrpkrMKYvqQ
- DWARF debugging standard -- https://dwarfstd.org/
- Linux ptrace man page -- `man 2 ptrace`
- Go `debug/elf` and `debug/dwarf` packages
- "How debuggers work" (Eli Bendersky's blog) -- https://eli.thegreenplace.net/2011/01/23/how-debuggers-work-part-1
- x86_64 ABI -- System V AMD64 ABI supplement (register usage, calling conventions)
- Go runtime internal structures -- https://github.com/golang/go/blob/master/src/runtime/runtime2.go (`g` struct definition)
