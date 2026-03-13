# 36. Build an Interpreter with JIT

**Difficulty**: Insane

## The Challenge

Building an interpreter is a rite of passage for serious programmers, but building one with a just-in-time (JIT) compiler that dynamically compiles hot code paths into native machine code is an entirely different beast — one that sits at the intersection of language design, compiler engineering, runtime systems, and low-level machine architecture. Your task is to build a complete language implementation in Rust: a lexer, parser, bytecode compiler, stack-based virtual machine with garbage collection, a profiling system to identify hot loops, a JIT compiler that translates hot bytecode sequences into native x86_64 machine code (via Cranelift or raw code generation), and a deoptimization mechanism that falls back to the interpreter when the JIT makes assumptions that are later violated.

The language you implement should be dynamically typed with first-class functions (closures), mutable variables, control flow (if/else, while loops, for loops), basic data types (integers, floats, booleans, strings, arrays, hash maps), and at minimum prototype-based objects or simple classes. The lexer tokenizes source code, the parser builds an AST, and the bytecode compiler lowers the AST to a linear sequence of stack-machine instructions (PUSH, POP, ADD, CALL, JUMP, etc.). The VM executes bytecode by fetching, decoding, and dispatching instructions in a loop, maintaining a value stack, a call stack with frames for function calls, and a heap for dynamically allocated objects managed by a tracing garbage collector.

The JIT component is where this challenge reaches its true difficulty. The VM profiles execution, counting how many times each function or loop back-edge is traversed. When a counter exceeds a threshold (the "hot" threshold), the JIT compiler is invoked to translate the bytecode of that function or loop into native machine code. For a dynamically typed language, this means the JIT must speculate on types (e.g., "this addition always operates on integers") and insert guards that check those assumptions at runtime. When a guard fails — say, a variable that was always an integer suddenly holds a string — the JIT must deoptimize: transfer execution back to the interpreter at the exact point where the guard failed, reconstructing the interpreter's stack and local variables from the JIT's register allocation. This on-stack replacement (OSR) between JIT-compiled code and the interpreter is one of the most challenging aspects of runtime system implementation, requiring meticulous bookkeeping of the mapping between compiled code state and interpreter state.

## Acceptance Criteria

### Language Specification

- [ ] Define a **source language** with the following features:
  - **Primitive types**: integers (64-bit signed), floating-point numbers (64-bit), booleans, strings, nil/null
  - **Compound types**: arrays (dynamic, heterogeneous), hash maps (string keys, any values)
  - **Variables**: `let x = expr;` for declaration, `x = expr;` for assignment, lexical scoping
  - **Arithmetic operators**: `+`, `-`, `*`, `/`, `%` (with standard precedence)
  - **Comparison operators**: `==`, `!=`, `<`, `>`, `<=`, `>=`
  - **Logical operators**: `and`, `or`, `not` (short-circuit evaluation for `and`/`or`)
  - **String concatenation**: `+` operator when either operand is a string (implicit conversion)
  - **Control flow**: `if/else`, `while` loops, `for` loops (C-style or iterator-based)
  - **Functions**: `fn name(params) { body }`, first-class (can be assigned to variables, passed as arguments)
  - **Closures**: functions capture variables from enclosing scopes by reference
  - **Return values**: explicit `return expr;` or implicit last-expression return
  - **Print**: built-in `print(expr)` function for output
  - **Comments**: `//` single-line and `/* */` multi-line

- [ ] Write a **language grammar** in EBNF or similar notation
  - Document precedence and associativity of all operators
  - Document the scoping rules (lexical scoping with closure capture)
  - Include at least 10 example programs demonstrating all language features

### Lexer (Tokenizer)

- [ ] Implement a **hand-written lexer** (not regex-based)
  - Tokenize the full language: keywords, identifiers, literals (int, float, string with escape sequences), operators, delimiters
  - Track source position (line and column) for each token (for error messages)
  - Handle edge cases: unterminated strings, invalid escape sequences, numbers with multiple dots
  - Produce meaningful error messages with source location: `Error at line 5, col 12: unterminated string literal`
  - Support Unicode identifiers (optional but encouraged)
  - Performance: tokenize a 10,000-line file in < 10ms

### Parser

- [ ] Implement a **recursive descent parser** (Pratt parser for expressions)
  - Parse the full language grammar into an AST
  - Use Pratt parsing (operator precedence parsing) for expressions to correctly handle precedence and associativity
  - Produce a typed AST where each node carries its source span (for error reporting)
  - Handle parsing errors gracefully: report the first error with context, attempt to recover and report additional errors (synchronization at statement boundaries)
  - Detect and report common mistakes: missing semicolons, unmatched braces, assignment in condition (warning)

- [ ] Define AST node types:
  - Expressions: `Literal`, `Identifier`, `Binary`, `Unary`, `Call`, `Index`, `FieldAccess`, `Lambda`, `Array`, `Map`
  - Statements: `Let`, `Assignment`, `ExprStmt`, `If`, `While`, `For`, `Return`, `Block`, `FnDecl`
  - Each node has a `Span` field recording the source range

### Bytecode Compiler

- [ ] Define a **bytecode instruction set** (at least 30 opcodes)
  - Stack manipulation: `PUSH_CONST`, `POP`, `DUP`, `SWAP`
  - Arithmetic: `ADD`, `SUB`, `MUL`, `DIV`, `MOD`, `NEG`
  - Comparison: `EQ`, `NE`, `LT`, `GT`, `LE`, `GE`
  - Logic: `NOT`, `AND`, `OR` (but note: short-circuit evaluation requires jumps, not stack ops)
  - Variables: `GET_LOCAL`, `SET_LOCAL`, `GET_UPVALUE`, `SET_UPVALUE`, `GET_GLOBAL`, `SET_GLOBAL`
  - Control flow: `JUMP`, `JUMP_IF_FALSE`, `LOOP` (backward jump)
  - Functions: `CALL`, `RETURN`, `CLOSURE` (create closure from function prototype + captured upvalues)
  - Objects: `ARRAY_NEW`, `ARRAY_GET`, `ARRAY_SET`, `MAP_NEW`, `MAP_GET`, `MAP_SET`
  - Other: `PRINT`, `HALT`
  - Each instruction is a single byte opcode followed by 0-3 bytes of operands

- [ ] Compile the AST into **function prototypes** (chunks of bytecode)
  - Each function (including the top-level script) is compiled into a `FunctionProto` containing: bytecode `Vec<u8>`, constant pool `Vec<Value>`, upvalue descriptors, arity, name, source map (bytecode offset -> source line)
  - Local variables are resolved at compile time to stack slot indices
  - Upvalues are captured at closure creation time using `GET_UPVALUE`/`SET_UPVALUE`
  - Closures reference their enclosing scope's locals or upvalues

- [ ] Implement **constant folding** in the compiler
  - Evaluate constant expressions at compile time (e.g., `2 + 3` -> `PUSH_CONST 5`)
  - Fold boolean constants in conditions (e.g., `if true { ... }` -> unconditional block)

- [ ] Generate a **source map** from bytecode offsets to source lines
  - Used for runtime error messages: `Runtime error at line 42: cannot add string and integer`
  - Used for the debugger (if implemented)

### Stack-Based Virtual Machine

- [ ] Implement the **VM execution loop**
  - Fetch-decode-dispatch loop processing one instruction at a time
  - Maintain a value stack (contiguous `Vec<Value>`) with a configurable maximum depth
  - Maintain a call stack of `CallFrame` structs (function, instruction pointer, stack base)
  - Implement all opcodes defined in the instruction set
  - Handle runtime type errors gracefully: `TypeError: cannot subtract string from integer at line 7`

- [ ] Implement **Value representation** using NaN-boxing or tagged unions
  - Option A (NaN-boxing): exploit the NaN space of 64-bit IEEE 754 floats to store integers, booleans, nil, and object pointers in a single 8-byte value (no heap allocation for primitives)
  - Option B (Tagged enum): `enum Value { Int(i64), Float(f64), Bool(bool), Nil, Object(GcRef) }` — simpler but larger (16 bytes per value)
  - Document which representation you chose and the trade-offs
  - Ensure that equality comparison works correctly for all type combinations

- [ ] Implement **function calls and closures**
  - `CALL` pushes a new `CallFrame` with the function's bytecode and a fresh stack base
  - `RETURN` pops the frame and leaves the return value on the stack
  - Closures capture upvalues: mutable references to variables in enclosing scopes
  - When a captured variable goes out of scope (its stack frame is popped), the upvalue is "closed over" — its value is moved from the stack to the heap
  - Multiple closures can share the same upvalue (mutations are visible to all)

- [ ] Implement **built-in functions**
  - `print(value)` — print to stdout
  - `len(array_or_string)` — return the length
  - `type(value)` — return a string describing the type
  - `clock()` — return the current time in seconds (for benchmarking)
  - `push(array, value)` — append to an array
  - `keys(map)` — return an array of map keys

### Garbage Collector

- [ ] Implement a **tracing garbage collector** (mark-and-sweep or mark-compact)
  - All heap-allocated objects (strings, arrays, maps, closures, upvalues) are managed by the GC
  - The GC maintains a list of all allocated objects
  - **Mark phase**: starting from roots (value stack, call stack, global variables, open upvalues), recursively mark all reachable objects
  - **Sweep phase**: iterate all objects; free unmarked objects, clear marks on marked objects
  - Trigger GC when total allocated bytes exceed a threshold (adaptive: threshold grows after each GC cycle)

- [ ] Implement **GC-safe object headers**
  - Each heap object has a header containing: GC mark bit, object type tag, size
  - Use a `GcRef` smart pointer (index or pointer) that is traceable by the GC
  - Ensure that temporary `GcRef` values on the Rust stack are also visible to the GC (use a shadow stack or handle-based approach)

- [ ] Test GC correctness
  - Allocate many short-lived objects in a loop, verify memory is reclaimed
  - Create circular references (via closures or arrays), verify the GC handles them (mark-and-sweep does this naturally)
  - Stress test: allocate 1 million objects, trigger 100+ GC cycles, verify no crashes or leaks
  - Verify that live objects are never collected (especially upvalues captured by closures)

- [ ] Implement GC **statistics and tuning**
  - Track: total allocated bytes, GC cycle count, time spent in GC, objects freed per cycle
  - Expose tuning parameters: initial threshold, growth factor, minimum threshold
  - Optional: implement a generational GC for better performance (young generation collected more frequently)

### Profiling and Hot Path Detection

- [ ] Implement **invocation counting** for functions
  - Each `FunctionProto` has an atomic counter incremented on every `CALL`
  - When the counter exceeds a configurable threshold (e.g., 1000), mark the function as "hot"

- [ ] Implement **loop back-edge counting**
  - Each `LOOP` instruction (backward jump) has an associated counter
  - When a loop's counter exceeds the threshold, mark the loop as "hot"
  - This detects hot inner loops even in functions that are called only once

- [ ] Implement a **tier system** for compilation
  - Tier 0: interpreter (all code starts here)
  - Tier 1: JIT-compiled (hot functions/loops are compiled to native code)
  - Optionally, Tier 0.5: "baseline JIT" that does a quick-and-dirty compilation without optimizations (faster compile time, slower code) before the optimizing JIT

- [ ] The profiling system must have **low overhead**
  - Counter increments should be a single atomic fetch-add
  - The check against the threshold should be a single comparison
  - Total profiling overhead: < 5% of interpreter execution time

### JIT Compiler

- [ ] Implement a **JIT compiler** that translates bytecode to native x86_64 machine code
  - Option A: Use the `cranelift-jit` crate to generate machine code from Cranelift IR
  - Option B: Emit raw x86_64 machine code directly into an mmap'd executable buffer
  - Document which approach you chose and why

- [ ] Support JIT compilation of the following bytecode patterns:
  - Arithmetic operations on integers and floats
  - Local variable access (get/set)
  - Comparison and conditional jumps
  - Loop bodies (the primary target for JIT)
  - Function calls (at least calls to other JIT-compiled functions)

- [ ] Implement **type specialization**
  - During profiling, record the observed types of operands for each instruction
  - The JIT generates specialized code for the observed types (e.g., integer-only ADD is a single x86 `add` instruction)
  - Insert **type guards** before specialized code: check that the operand's type matches the expected type
  - If the guard fails, trigger deoptimization (see below)

- [ ] Implement **constant propagation and folding** in the JIT
  - If a variable is always assigned the same constant value in the profiling data, propagate it
  - Fold constant expressions in the generated code

- [ ] Implement **register allocation** for JIT-compiled code
  - If using Cranelift, it handles this automatically
  - If generating raw code, implement at least linear-scan register allocation
  - Map frequently accessed local variables to registers, spill infrequently accessed ones to the stack

- [ ] The JIT-compiled code must be **callable from the VM**
  - When the VM encounters a hot function, it calls the JIT-compiled version instead of interpreting bytecode
  - Arguments are passed via a calling convention (e.g., System V AMD64 ABI or a custom convention)
  - The return value is placed where the VM expects it

- [ ] JIT-compiled code can **call back into the VM**
  - When JIT-compiled code calls a non-JIT function, it must transition back to the interpreter
  - Implement a trampoline that saves JIT state and enters the interpreter loop
  - When the called function returns, the trampoline restores JIT state and returns to compiled code

### Deoptimization and On-Stack Replacement (OSR)

- [ ] Implement **deoptimization** when JIT type guards fail
  - When a type guard fails, execution must transfer back to the interpreter at the exact bytecode instruction where the guard was placed
  - The JIT compiler must emit metadata mapping each guard to: the corresponding bytecode offset, the mapping of registers/stack slots to local variable indices

- [ ] Implement **OSR (On-Stack Replacement)**
  - When deoptimizing, reconstruct the interpreter's `CallFrame` and value stack from the JIT's register state
  - The reconstructed state must be as if the interpreter had been executing all along
  - After reconstruction, the interpreter resumes execution from the bytecode offset of the failed guard
  - Test: a function that does 1000 iterations with integer addition, then on iteration 1001 switches an operand to a string — verify that deoptimization happens and the function completes correctly in the interpreter

- [ ] Implement **OSR entry** (interpreter to JIT mid-execution)
  - When a loop becomes hot while the interpreter is inside it, compile the loop body and transfer execution to the JIT-compiled version without leaving the loop
  - This requires mapping the current interpreter state (locals, stack) to the JIT's expected input format
  - This is the inverse of deoptimization and is equally challenging

- [ ] Track deoptimization frequency
  - If a function repeatedly deoptimizes, mark it as "polymorphic" and stop trying to JIT-compile it
  - Configurable threshold (e.g., 10 deoptimizations -> give up)

### Testing

- [ ] **Language tests**: a test suite of programs that exercise all language features
  - At least 50 test programs covering: arithmetic, string ops, variables, scoping, closures, control flow, functions, recursion, arrays, maps, error handling
  - Each test program has an expected output; the test runner compares actual vs. expected
  - Include programs that specifically test edge cases: integer overflow, deep recursion (stack overflow detection), division by zero, type errors

- [ ] **Bytecode compiler tests**: verify that specific AST patterns produce expected bytecode
  - Simple expression: `1 + 2` -> `PUSH_CONST 1, PUSH_CONST 2, ADD`
  - Variable access: `let x = 5; x` -> `PUSH_CONST 5, SET_LOCAL 0, GET_LOCAL 0`
  - Closure capture: verify upvalue instructions are emitted correctly

- [ ] **GC tests**: stress tests for garbage collection
  - Allocate and discard millions of objects, verify steady-state memory usage
  - Create deep object graphs, trigger GC, verify all reachable objects survive
  - Run the full language test suite with an artificially low GC threshold (trigger GC every 1 KB of allocation)

- [ ] **JIT tests**: verify that JIT-compiled code produces the same results as the interpreter
  - For every language test, run it twice: once with JIT disabled, once with JIT enabled (low threshold: 10 invocations)
  - Compare outputs; they must be identical
  - Specifically test functions that mix integer and float arithmetic (type specialization)
  - Test recursive functions (JIT must handle recursive calls correctly)

- [ ] **Deoptimization tests**: verify correct behavior when type guards fail
  - A function that loops with integer addition for N iterations, then switches to string concatenation
  - The output must be correct: the integers are added correctly, the strings are concatenated correctly, with the transition at the exact right iteration
  - A function that is called 1000 times with integer arguments, then once with a string argument — verify it deoptimizes and handles the string correctly

- [ ] **Performance tests**: verify that JIT compilation provides speedup
  - A tight loop computing fibonacci(35) recursively: JIT version should be at least 5x faster than interpreter
  - A loop with 10 million integer additions: JIT version should be at least 10x faster
  - Measure and report: interpreter time, JIT compile time, JIT execution time

### Performance Targets

- [ ] Interpreter (without JIT): execute fibonacci(30) in < 5 seconds
- [ ] JIT-compiled: execute fibonacci(30) in < 1 second
- [ ] Lexer + parser: parse a 10,000-line file in < 50ms
- [ ] Bytecode compiler: compile a 10,000-line file in < 50ms
- [ ] JIT compile time: compile a single function in < 5ms
- [ ] GC pause time: < 10ms for heaps up to 100 MB

### Code Organization

- [ ] Cargo workspace with crates:
  - `lang` — the main binary (REPL and file execution)
  - `lexer` — tokenization
  - `parser` — AST construction
  - `compiler` — bytecode generation
  - `vm` — virtual machine and GC
  - `jit` — JIT compiler and deoptimization
  - `common` — shared types (Value, opcodes, spans, errors)

- [ ] Implement a **REPL** (read-eval-print loop)
  - Interactive mode when no file argument is given
  - Supports multi-line input (detects incomplete expressions/blocks)
  - Prints results of expressions (not just statements)
  - Maintains state between inputs (variables persist)

- [ ] Implement a **disassembler** for debugging
  - `--disassemble` flag prints the bytecode for each compiled function
  - Human-readable format: `0000 PUSH_CONST 42`, `0002 ADD`, etc.
  - Show the constant pool, upvalue descriptors, and source map

- [ ] Implement a `--profile` flag that prints execution statistics
  - Number of bytecode instructions executed
  - Number of GC cycles and total GC pause time
  - Number of JIT compilations and total JIT compile time
  - Number of deoptimizations

## Starting Points

- **Crafting Interpreters** by Robert Nystrom (craftinginterpreters.com): The definitive guide to building an interpreter. Part II (tree-walk) and Part III (bytecode VM) cover exactly what you need for the lexer, parser, compiler, and VM. The book implements "Lox" in C; you will port the concepts to Rust and extend with a JIT.
- **LuaJIT Source Code**: Mike Pall's LuaJIT is one of the most impressive dynamic language JITs ever built. Study its trace-based JIT architecture, particularly how it detects hot traces, compiles them, and handles deoptimization (called "trace exits" in LuaJIT).
- **cranelift-jit crate**: Cranelift is a code generator designed for JIT use cases (github.com/bytecodealliance/wasmtime/tree/main/cranelift). The `cranelift-jit` crate provides the ability to allocate executable memory, emit Cranelift IR, compile it, and get a function pointer. This is significantly easier than emitting raw x86_64 bytes.
- **Simple JIT Compiler in Rust** (various blog posts): Search for "JIT compiler Rust" on blogs and GitHub for simplified examples of mmap + emit + execute patterns.
- **V8 TurboFan and Maglev**: Google's V8 JavaScript engine has well-documented blog posts about its tiered compilation, type specialization, and deoptimization strategy. Search for "V8 blog" posts by Mathias Bynens and others.
- **Writing a JIT Compiler in Rust** by Pratt (various talks): Conference talks that walk through building a simple JIT in Rust with Cranelift.
- **GC Handbook** by Jones, Hosking, and Moss: Comprehensive reference for garbage collection algorithms. Chapter 2 (mark-and-sweep) is sufficient for this project.

## Hints

1. **Build the interpreter first, completely, before touching the JIT.** You need a fully working interpreter as both the baseline execution engine and the fallback for deoptimization. The JIT is an optimization layer on top of a correct interpreter, not a replacement for it. Get all 50+ language tests passing with the interpreter before writing a single line of JIT code.

2. **NaN-boxing is elegant but tricky to get right in Rust.** The idea is that a 64-bit IEEE 754 float has a large "NaN space" (when the exponent bits are all 1 and the mantissa is non-zero), and you can use different NaN bit patterns to encode integers, booleans, nil, and object pointers. In Rust, you will need `unsafe` for the bit manipulation (`f64::to_bits()`, `u64::from_bits()`). If this sounds daunting, start with a tagged enum and optimize later.

3. **The upvalue mechanism for closures is subtle.** When a closure captures a local variable, it gets a reference to that variable's stack slot. When the function that owns the variable returns, the variable must be "closed over" — moved from the stack to the heap. Use an `Upvalue` struct that can be in two states: `Open(stack_index)` or `Closed(Value)`. When a function returns, iterate its open upvalues and close them. Multiple closures can share the same `Upvalue` object (via `Gc<RefCell<Upvalue>>`).

4. **For the GC, the hardest part is root scanning.** You must find every `GcRef` that is reachable from Rust's stack and the VM's data structures. Two approaches: (a) a "shadow stack" where every function that holds a `GcRef` explicitly pushes it onto a GC root list, or (b) make the VM's value stack, call stack, and globals the only roots, and ensure no `GcRef` escapes into Rust-only variables across a potential GC point. Approach (b) is simpler if you are careful.

5. **Use Cranelift for the JIT unless you specifically want to learn x86_64 encoding.** Cranelift handles instruction selection, register allocation, and code emission. You "just" need to translate your bytecode into Cranelift IR (which looks like a typed SSA form). The `cranelift-jit` crate gives you a `JITModule` that allocates executable memory and returns function pointers.

6. **Type specialization with guards is the key to JIT performance.** For a dynamically typed language, the interpreter must check types on every operation. The JIT eliminates these checks for the common case by specializing: if profiling shows that an ADD always operates on two integers, the JIT emits a native integer add preceded by a guard that checks both operands are integers. The guard is a comparison + conditional jump to a deoptimization stub.

7. **For deoptimization, think of it as "converting JIT state to interpreter state."** At each guard point, the JIT compiler records a "deoptimization map": which registers and stack slots hold which local variables, and what the corresponding bytecode offset is. When a guard fires, the deopt stub reads this map, fills in a `CallFrame` and the value stack, and jumps to the interpreter loop at the recorded bytecode offset.

8. **OSR entry (interpreter -> JIT mid-loop) is the most complex part.** You need to: (a) compile the loop body starting from the current bytecode offset, (b) generate a special entry point that expects the interpreter's locals as arguments, (c) at the back-edge of the hot loop in the interpreter, call this entry point passing the current locals, (d) when the JIT-compiled loop exits (via a non-back-edge branch), return the updated locals to the interpreter. Consider skipping OSR entry initially and only JIT-compiling entire functions.

9. **Test the JIT against the interpreter obsessively.** The single most effective testing strategy is: for every test program, run it with JIT disabled and JIT enabled (with a very low hot threshold, like 2 invocations), and assert the outputs are identical. Any divergence is a JIT bug. This catches type specialization errors, register allocation bugs, and deoptimization issues.

10. **Memory management for JIT-compiled code is its own challenge.** You need to allocate executable memory (mmap with PROT_EXEC on Unix), manage it (free code for functions that are deoptimized and recompiled), and ensure it doesn't leak. Cranelift's `JITModule` handles this for you. If going raw, create a simple "code arena" that allocates pages of executable memory and hands out chunks.
