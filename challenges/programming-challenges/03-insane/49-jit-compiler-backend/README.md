# 49. JIT Compiler Backend

<!--
difficulty: insane
category: compilers-jit
languages: [rust]
concepts: [jit-compilation, x86-64, machine-code-generation, register-allocation, mmap, tiered-compilation]
estimated_time: 30-40 hours
bloom_level: create
prerequisites: [x86-64-basics, virtual-memory, bytecode-execution, unsafe-rust, function-pointers]
-->

## Languages

- Rust (1.75+ stable)

## The Challenge

Build a JIT compiler that compiles bytecode to native x86-64 machine code at runtime. No LLVM, no Cranelift, no external code generation libraries -- you are emitting raw bytes that the CPU executes directly.

The JIT must implement tiered compilation: cold functions are interpreted, and once a function's invocation count crosses a threshold, the JIT compiles it to native code. Subsequent calls execute the native version. This is how the JVM's HotSpot, LuaJIT, and V8's Sparkplug work.

You will write code that allocates executable memory with `mmap`, encodes x86-64 instructions as byte sequences, writes them into that memory, and calls the result as a function pointer. This is the lowest level of code generation possible without writing an assembler from scratch.

This challenge requires `unsafe` Rust by necessity -- there is no safe way to allocate executable memory, cast raw pointers to function pointers, or call generated machine code. Every `unsafe` block must have a safety comment explaining why it is sound. The goal is to minimize the unsafe surface area and contain it in well-tested modules.

The x86-64 instruction encoding is complex but you only need a small subset: register-register moves and arithmetic, immediate loads, memory loads/stores with displacement, conditional and unconditional jumps with 32-bit relative offsets, and the call/ret pair. Around 20 instruction forms cover the entire instruction set needed for this challenge.

## Acceptance Criteria

- [ ] Executable memory allocation via `mmap` (Unix) with `PROT_READ | PROT_WRITE | PROT_EXEC` (W^X: write first, then remap as read+exec)
- [ ] x86-64 code generation for: integer arithmetic (add, sub, mul, div, mod), comparisons and conditional jumps, unconditional jumps, function prologue/epilogue (push rbp, mov rbp rsp, ... pop rbp, ret), local variables via stack frame offsets, function calls (System V AMD64 ABI: rdi, rsi, rdx, rcx, r8, r9 for args)
- [ ] Register allocation using linear scan over live intervals
- [ ] Hot path detection: invocation counter per function, configurable threshold (default: 100 calls)
- [ ] Tiered compilation: interpret below threshold, JIT above it, seamless transition
- [ ] JIT-compiled functions produce identical results to interpreted execution for all inputs
- [ ] Correct handling of the System V AMD64 calling convention for calling into and out of JIT-compiled code
- [ ] Generated code can call back into Rust functions (for built-in operations like print)
- [ ] Memory management: compiled code is freed when no longer needed
- [ ] A benchmark showing measurable speedup of JIT-compiled code over interpretation (at least 5x on tight arithmetic loops)
- [ ] Disassembler that shows the generated x86-64 instructions (use the `iced-x86` crate for disassembly only, not generation)

## Starting Points

- **x86-64 encoding**: start with the simplest instructions. `mov rax, imm64` is `0x48 0xB8` followed by 8 bytes of the immediate value. `add rax, rbx` is `0x48 0x01 0xD8`. Build a small assembler layer that emits bytes for each instruction you need.
- **mmap for executable memory**: on Unix, `mmap(null, size, PROT_READ | PROT_WRITE, MAP_PRIVATE | MAP_ANONYMOUS, -1, 0)` gives you writable memory. Write your code there, then `mprotect` to `PROT_READ | PROT_EXEC` before calling it. This is W^X (Write XOR Execute) -- never have memory that is both writable and executable simultaneously.
- **Function pointers**: after writing machine code to mmap'd memory and setting it executable, cast the pointer to `extern "C" fn(...) -> ...` and call it. This is `unsafe` Rust territory by necessity.

## Hints

1. Build the x86-64 emitter as a separate module that knows nothing about your language or bytecode. It should have functions like `emit_mov_reg_imm64(&mut buf, reg, value)`, `emit_add_reg_reg(&mut buf, dst, src)`, etc. Test this layer independently by generating known instruction sequences and verifying the bytes against a reference (use `nasm` to assemble the same instructions and compare).

2. Linear scan register allocation needs live intervals: for each temporary, record the first and last instruction where it is used. Sort by start position, iterate forward, assign registers to intervals that are live, and spill to stack when all registers are occupied. Start with just the callee-saved registers (rbx, r12-r15) to avoid conflicts with the calling convention.

3. The transition from interpreter to JIT must be invisible to the program. When a function crosses the hot threshold, compile it. The next call dispatches to the native version. The native code must set up the same stack frame layout the interpreter expects so that mixing interpreted and compiled frames works.

## Going Further

- Implement on-stack replacement (OSR): JIT-compile a function while it is running (mid-loop), transferring local state from the interpreter to compiled code
- Add inline caching for polymorphic call sites
- Implement a simple garbage collector that cooperates with JIT-compiled code (safe points)
- Target ARM64 (aarch64) as a second backend
- Add trace-based JIT: record a trace of executed bytecodes in a hot loop, compile just that trace

## Resources

- [Adventures in JIT Compilation (Eli Bendersky)](https://eli.thegreenplace.net/2017/adventures-in-jit-compilation-part-1-an-interpreter/) -- four-part series building a JIT from scratch in C, excellent pedagogy
- [Intel x86-64 Software Developer Manual, Volume 2](https://www.intel.com/content/www/us/en/developer/articles/technical/intel-sdm.html) -- the instruction encoding reference; Volume 2A-2D cover every opcode
- [System V AMD64 ABI](https://refspecs.linuxbase.org/elf/x86_64-abi-0.99.pdf) -- the calling convention: which registers hold arguments, which are callee-saved, stack alignment requirements
- [Linear Scan Register Allocation (Poletto & Sarkar)](https://web.cs.ucla.edu/~palsberg/course/cs132/linearscan.pdf) -- the original linear scan paper
- [LuaJIT 2.0 Internals (Mike Pall)](http://wiki.luajit.org/NYI) -- architecture of a production trace-based JIT
- [Copy-and-Patch Compilation (Xu & Kjolstad)](https://fredrikbk.com/publications/copy-and-patch.pdf) -- a modern alternative to traditional JIT code generation using pre-compiled templates
- [Cranelift source](https://github.com/bytecodealliance/wasmtime/tree/main/cranelift) -- production Rust code generator; study for reference, not for use
- [x86-64 Instruction Encoding (OSDev Wiki)](https://wiki.osdev.org/X86-64_Instruction_Encoding) -- practical guide to ModR/M, SIB, REX prefix encoding
- [HotSpot JIT Architecture (Oracle)](https://openjdk.org/groups/hotspot/docs/HotSpotGlossary.html) -- how the JVM's tiered compilation works in production
