# 16. Building a Language VM

**Difficulty**: Insane

## Problem Statement

Build a complete bytecode virtual machine from scratch in Rust. Your VM will execute
a custom instruction set using a stack-based execution engine, manage local variables,
handle function calls with activation frames, perform garbage collection using a
mark-and-sweep algorithm, treat closures as first-class values, and apply basic
peephole optimizations to the bytecode before execution.

This is the kind of project that teaches you how languages actually work under the
hood. You will confront every layer of abstraction that higher-level languages hide
from you: how values are represented in memory, how function calls create and destroy
stack frames, how closures capture their environment, and how a garbage collector
reclaims memory without breaking live references.

### The Language

Your VM will execute a simple dynamically-typed language with the following features:

- **Primitive types**: integers (i64), floats (f64), booleans, strings, nil
- **Compound types**: arrays, closures
- **Expressions**: arithmetic (`+`, `-`, `*`, `/`, `%`), comparison (`==`, `!=`, `<`, `>`, `<=`, `>=`), logical (`and`, `or`, `not`), string concatenation
- **Statements**: variable declaration (`let`), assignment, print, if/else, while loops, for loops, return
- **Functions**: first-class functions, closures with captured environments, recursion
- **Built-in functions**: `print`, `len`, `push`, `clock` (for benchmarking)

You do not need to write a parser for the source language (though you may if you want).
Instead, you can build bytecode programmatically using a builder API. The focus of this
exercise is the VM itself.

### The Bytecode Format

Design a compact binary instruction set. Each instruction consists of an opcode (one byte)
followed by zero or more operand bytes. Your instruction set should include at minimum:

```
// Stack operations
OP_CONST       <idx:u16>     // Push constant from pool onto stack
OP_POP                       // Discard top of stack
OP_DUP                       // Duplicate top of stack

// Local variables
OP_GET_LOCAL   <slot:u8>     // Push local variable onto stack
OP_SET_LOCAL   <slot:u8>     // Pop stack into local variable

// Global variables
OP_GET_GLOBAL  <idx:u16>     // Push global variable onto stack
OP_SET_GLOBAL  <idx:u16>     // Pop stack into global variable
OP_DEF_GLOBAL  <idx:u16>     // Define a new global variable

// Arithmetic
OP_ADD
OP_SUB
OP_MUL
OP_DIV
OP_MOD
OP_NEG                       // Unary negate

// Comparison
OP_EQ
OP_NE
OP_LT
OP_GT
OP_LE
OP_GE

// Logic
OP_NOT
OP_AND
OP_OR

// Control flow
OP_JMP         <offset:i16>  // Unconditional jump
OP_JMP_IF_FALSE <offset:i16> // Conditional jump
OP_LOOP        <offset:u16>  // Jump backward (for loops)

// Functions and closures
OP_CALL        <argc:u8>     // Call function with argc arguments
OP_RETURN                    // Return from function
OP_CLOSURE     <idx:u16> <upvalue_count:u8> [<is_local:u8> <index:u8>]*

// Upvalues (for closures)
OP_GET_UPVALUE <slot:u8>     // Read captured variable
OP_SET_UPVALUE <slot:u8>     // Write captured variable
OP_CLOSE_UPVALUE             // Close over variable on stack

// Arrays
OP_ARRAY       <count:u16>   // Create array from top N stack values
OP_INDEX_GET                 // array[index]
OP_INDEX_SET                 // array[index] = value

// Misc
OP_PRINT
OP_NIL
OP_TRUE
OP_FALSE
OP_HALT
```

### The Constant Pool

Each function (or "chunk" of bytecode) has an associated constant pool that stores
values too large to encode inline: numbers, strings, and function prototypes. Constants
are referenced by index.

### The Execution Engine

Your VM uses a value stack and a call stack (separate or combined). When a function is
called:

1. A new `CallFrame` is pushed onto the call stack
2. The frame records the return address (instruction pointer), the base pointer
   (where this frame's locals begin on the value stack), and a reference to the
   function's bytecode chunk
3. Local variables are accessed relative to the frame's base pointer
4. On return, the frame is popped, the stack is unwound to the base pointer, and the
   return value is pushed

### Closures and Upvalues

Closures are the hardest part. When a closure is created, it captures references to
variables from enclosing scopes. These captured variables are called "upvalues."

The tricky part: a captured variable might still be on the stack (an "open" upvalue)
or it might have already been popped because the enclosing function returned (a "closed"
upvalue). Your VM must handle both cases:

- **Open upvalue**: points directly to a stack slot
- **Closed upvalue**: the value has been moved off the stack into a heap-allocated box

When a local variable goes out of scope and it has been captured by a closure, the VM
must "close" the upvalue by moving the value from the stack into the upvalue object itself.

### Garbage Collection

Implement a mark-and-sweep garbage collector:

1. **Allocation**: All heap objects (strings, arrays, closures, upvalues) are allocated
   through the GC. The GC maintains a linked list of all allocated objects.
2. **Triggering**: GC runs when total allocated bytes exceed a threshold. The threshold
   grows after each collection (e.g., doubles).
3. **Mark phase**: Starting from roots (the value stack, the call stack, global variables,
   open upvalues, and compiler temporaries), recursively mark all reachable objects.
4. **Sweep phase**: Walk the allocation list. Free any unmarked objects. Clear marks on
   survivors.
5. **Stress testing mode**: A debug mode that triggers GC on every allocation, to flush
   out GC bugs immediately.

### Peephole Optimization

Before execution, apply basic peephole optimizations to the bytecode:

1. **Constant folding**: `OP_CONST 3; OP_CONST 4; OP_ADD` becomes `OP_CONST 7`
2. **Dead store elimination**: `OP_SET_LOCAL n; OP_GET_LOCAL n` becomes `OP_DUP; OP_SET_LOCAL n`
3. **Jump threading**: If a jump targets another jump, rewrite to jump directly to the final destination
4. **Strength reduction**: Multiplication/division by powers of 2 to shift operations (if you add shift instructions)
5. **Unreachable code elimination**: Remove instructions after unconditional jumps or returns that are not jump targets

---

## Acceptance Criteria

### Core VM (Must Pass All)

1. **Bytecode encoding/decoding**: Instructions round-trip through encode and decode
   without data loss. Every opcode is tested. Operand widths are respected (u8, u16, i16).

2. **Arithmetic operations**: All arithmetic ops work correctly on integers and floats.
   Integer overflow wraps (or produces an error, your choice, but document it). Division
   by zero produces a runtime error, not a panic. Mixed int/float operations promote to float.

3. **Comparison and logic**: All comparison operators return boolean values. Equality is
   structural for strings and arrays. Logical operators short-circuit.

4. **Variable management**: Local variables are accessed by slot index. Globals are
   accessed by name (via constant pool index). Assigning to an undefined global is a
   runtime error. At least 256 local variables per scope.

5. **Control flow**: Forward and backward jumps work correctly. Nested if/else chains
   with 10+ levels execute correctly. While loops can execute 1,000,000 iterations
   without stack overflow.

6. **Function calls**: Functions with 0 to 255 arguments. Recursive functions work to
   at least depth 1000 (with appropriate call stack size). Wrong argument count produces
   a runtime error, not a panic.

7. **Return values**: Functions return their value correctly. Missing return yields nil.
   Return from nested scopes unwinds correctly.

### Closures (Must Pass All)

8. **Basic closure capture**: A closure captures a variable from its enclosing function
   and reads it correctly after the enclosing function has returned.

9. **Mutable upvalues**: A closure can modify a captured variable, and the modification
   is visible to other closures that captured the same variable.

10. **Nested closures**: A closure inside a closure inside a function, three levels deep,
    each capturing variables from different levels. All reads and writes work correctly.

11. **Upvalue closing**: When a local variable goes out of scope, its upvalue is closed.
    The closure continues to read and write the correct value after closing.

12. **Closure as argument**: Closures can be passed as arguments to other functions and
    called from there.

13. **Closure as return value**: Functions can return closures. The returned closure works
    correctly after the creating function's stack frame has been destroyed.

### Garbage Collection (Must Pass All)

14. **Objects are collected**: After dropping all references to a string/array/closure,
    a subsequent GC cycle frees the memory. Verify using allocation counts.

15. **Live objects survive**: Objects reachable from any root survive GC. Running GC
    mid-computation does not corrupt the VM state.

16. **Stress test mode**: With GC triggered on every allocation, the Fibonacci benchmark
    (fib(20)) still produces the correct result (6765). This is the ultimate test for
    GC correctness.

17. **Cycle handling**: Circular references (e.g., an array containing itself) do not
    prevent collection when the root reference is dropped. (Note: mark-and-sweep handles
    this naturally, but verify it.)

18. **GC threshold growth**: The GC threshold increases after each collection. Verify
    that GC frequency decreases as the program reaches steady state. Log GC events in
    debug mode.

### Peephole Optimization (Must Pass All)

19. **Constant folding**: `3 + 4 * 2` compiles to a single `OP_CONST 11` after
    optimization. Verify by inspecting the optimized bytecode.

20. **Jump threading**: A chain of three jumps (A -> B -> C -> D) is collapsed to
    (A -> D). Verify by inspecting the optimized bytecode.

21. **Dead store elimination**: Consecutive set/get of the same local is optimized.
    Verify by bytecode inspection and execution correctness.

22. **Optimization preserves semantics**: Every optimization pass must not change the
    observable behavior. Run the full test suite with and without optimization enabled;
    results must be identical.

### Performance Benchmarks (Must Pass All)

23. **Fibonacci(35)**: Naive recursive fibonacci(35) executes in under 2 seconds on a
    modern machine (Apple M1 or equivalent). The result must be 9227465.

24. **Loop performance**: A tight loop incrementing a counter 10,000,000 times completes
    in under 1 second.

25. **String concatenation**: Concatenating 100,000 short strings completes in under
    5 seconds.

26. **Closure-heavy workload**: Creating and invoking 100,000 closures, each capturing
    one variable, completes in under 3 seconds.

27. **GC throughput**: Allocating and discarding 1,000,000 small objects (triggering
    many GC cycles) completes in under 10 seconds.

### Error Handling (Must Pass All)

28. **Runtime errors are descriptive**: Every runtime error includes the instruction
    offset, the opcode that triggered it, and a human-readable message. No panics for
    user errors.

29. **Stack trace on error**: When a runtime error occurs inside a nested function call,
    the error report includes a stack trace showing each call frame with function name
    and instruction offset.

30. **Type errors**: Attempting `"hello" - 3` produces a type error, not a panic.
    Attempting to call a non-function produces a "not callable" error.

31. **Stack overflow**: Infinite recursion produces a "stack overflow" error with a stack
    trace, not a Rust stack overflow.

### Code Quality (Must Pass All)

32. **Zero `unsafe`**: The entire VM implementation uses only safe Rust. If you need
    `unsafe` for performance, isolate it behind a safe API and justify it in comments.

33. **Comprehensive tests**: At least 50 unit tests covering all opcodes, all error
    conditions, closure edge cases, and GC stress scenarios.

34. **Modular architecture**: Separate modules for bytecode representation, compiler/builder,
    VM execution, GC, and optimization. No module exceeds 800 lines.

35. **Debug tooling**: A disassembler that prints human-readable bytecode. A trace mode
    that prints each instruction as it executes, with the current stack state.

---

## Starting Points

### Recommended Architecture

```
src/
  lib.rs              // Public API
  value.rs            // Value type (enum: Int, Float, Bool, String, Array, Closure, Nil)
  chunk.rs            // Bytecode chunk: instructions + constant pool
  opcode.rs           // Opcode enum and encoding/decoding
  vm.rs               // The execution engine (main dispatch loop)
  frame.rs            // CallFrame: ip, base_pointer, function reference
  compiler.rs         // Bytecode builder API (or full compiler if ambitious)
  gc.rs               // Mark-and-sweep garbage collector
  upvalue.rs          // Upvalue representation (open vs closed)
  optimizer.rs        // Peephole optimization passes
  disassembler.rs     // Human-readable bytecode printer
  error.rs            // Runtime error types with location info
```

### Value Representation

The central design decision is how to represent values. Two main approaches:

**Tagged enum (simpler)**:
```rust
enum Value {
    Int(i64),
    Float(f64),
    Bool(bool),
    Nil,
    Obj(GcRef<Object>),  // GC-managed heap object
}

enum Object {
    String(String),
    Array(Vec<Value>),
    Closure(Closure),
    Upvalue(UpvalueObj),
}
```

**NaN-boxing (faster, harder)**:
Encode all values in a single `u64` using NaN-boxing. Floats use their normal IEEE 754
representation. Other types are encoded in the unused bits of NaN values. This makes the
value stack a simple `Vec<u64>` and avoids enum discriminant overhead.

Start with the tagged enum. Move to NaN-boxing only if you need the performance.

### GC Integration

The GC needs to know about all roots. One clean approach:

```rust
struct Gc {
    objects: Vec<Box<Object>>,   // All allocated objects
    bytes_allocated: usize,
    next_gc: usize,
}

impl Gc {
    fn alloc(&mut self, object: Object) -> GcRef<Object> { ... }
    fn collect(&mut self, roots: &[GcRef<Object>]) { ... }
}
```

The tricky part is that the VM owns the roots (stack, frames, globals) but the GC needs
to traverse them. Consider passing roots to `collect()` rather than giving the GC
permanent access to VM internals.

### The Dispatch Loop

Your main execution loop is a `loop` with a `match` on the current opcode:

```rust
loop {
    let opcode = self.read_byte();
    match opcode {
        OP_CONST => { ... }
        OP_ADD => { ... }
        // ... hundreds of lines ...
        OP_HALT => break,
        _ => return Err(RuntimeError::UnknownOpcode(opcode)),
    }
}
```

For performance, consider computed goto emulation using function pointer tables, but
start with a simple match.

### Key Resources

1. **"Crafting Interpreters" by Robert Nystrom** (craftinginterpreters.com) - Part III
   covers exactly this project in C. Your job is to do it in Rust with proper ownership.
2. **"Writing An Interpreter In Go" by Thorsten Ball** - A simpler version that may help
   with understanding the concepts before adding GC and closures.
3. **Lua 5.x source code** - A masterclass in compact, fast VM design. The upvalue
   mechanism is directly inspired by Lua.
4. **The original mark-and-sweep paper** by John McCarthy (1960) - Short and worth reading.

---

## Hints

### Hint 1: GC and Ownership

The biggest challenge in Rust is that the GC wants to own objects, but the VM holds
references to them. You cannot use `Rc<RefCell<T>>` because that is reference counting,
not mark-and-sweep.

One approach: use an arena-style allocator where objects are stored in a `Vec` and
referenced by index (`GcRef` is just a `usize`). This sidesteps Rust's borrow checker
because indices are `Copy` and do not borrow anything. The downside is you need to be
careful about dangling indices after a sweep.

A safer approach: use a generational index (index + generation counter) to detect use
of freed objects. The `slotmap` crate does this, or you can roll your own.

### Hint 2: Upvalue Closing

The upvalue closing mechanism is subtle. Here is the lifecycle:

1. When a closure is created, each captured variable becomes an upvalue
2. If the variable is still on the stack, the upvalue stores its stack index ("open")
3. When the variable's scope ends (e.g., enclosing function returns), the VM emits
   `OP_CLOSE_UPVALUE`
4. The close operation copies the value from the stack into the upvalue object itself
5. Now the upvalue is "closed" and contains the value directly
6. Multiple closures can share the same upvalue object

The key insight: you need a list of all open upvalues, sorted by stack index. When
closing, you walk this list and close all upvalues at or above the given stack index.

### Hint 3: Peephole Optimization Window

Peephole optimization works on a sliding window of instructions. The simplest approach:

1. Convert bytecode to a `Vec<Instruction>` (decoded form)
2. Slide a window of 2-4 instructions across the vector
3. Pattern-match against known optimization patterns
4. Replace matched patterns with optimized versions
5. Repeat until no more optimizations apply (fixed-point)
6. Re-encode back to bytecode

Watch out for jumps: you cannot optimize across jump targets. Build a set of all
jump targets first, and never optimize a window that contains a jump target.

### Hint 4: Debugging GC Bugs

GC bugs are the worst bugs. They are non-deterministic, manifest far from the cause,
and can corrupt memory silently. Defensive strategies:

1. **Stress mode**: Trigger GC on every allocation. This makes bugs deterministic.
2. **Poisoning**: After freeing an object, overwrite it with a sentinel value. If the
   VM reads this sentinel, you have a use-after-free.
3. **Root validation**: In debug mode, verify that every root is a valid GcRef before
   starting a collection.
4. **Allocation logging**: Log every alloc and free with object type and address. If
   an object is freed twice or never freed, you will see it.

### Hint 5: Performance

For the fibonacci(35) benchmark target:

- The naive implementation will be too slow if you are doing heap allocation for every
  integer. Keep integers unboxed (on the value stack, not behind a GcRef).
- Use a flat `Vec<u8>` for bytecode, not `Vec<Instruction>`. Decoding instructions from
  bytes is faster than matching enum variants due to cache effects.
- Avoid allocating in the hot loop. The arithmetic opcodes should touch only the value
  stack (a `Vec<Value>` with pre-allocated capacity).
- Profile with `cargo flamegraph`. The bottleneck is almost always the dispatch loop.

### Hint 6: Testing Strategy

Build your tests from the bottom up:

1. Test opcode encoding/decoding in isolation
2. Test individual instructions with hand-built bytecode chunks
3. Test function calls with known bytecode sequences
4. Test closures with carefully constructed upvalue scenarios
5. Test GC with stress mode on all previous tests
6. Test optimization by comparing execution results before and after optimization
7. Benchmark tests use `#[bench]` or criterion

Write a helper function that builds bytecode, runs it, and returns the final stack state.
This will be your most-used test utility.

### Hint 7: Error Handling

Use a dedicated error type with source location:

```rust
struct RuntimeError {
    message: String,
    ip: usize,                     // Instruction pointer where error occurred
    function_name: String,         // Function name (or "<script>")
    stack_trace: Vec<FrameInfo>,   // Call stack at time of error
}

struct FrameInfo {
    function_name: String,
    ip: usize,
}
```

Never use `.unwrap()` on user-visible operations. Every stack pop, every variable
access, every type check should return `Result`.

### Hint 8: Closure Edge Cases to Test

These are the cases that break naive implementations:

1. A closure that captures a variable from two scopes up (not the immediately enclosing one)
2. Two closures that capture the same variable, one modifies it, the other reads it
3. A closure returned from a function, called after the function has returned
4. A loop that creates closures, each capturing the loop variable (do they all see the
   same value or different values? Document your choice.)
5. A recursive closure (the closure calls itself through a captured variable)
6. A closure that captures a closure
