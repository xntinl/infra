# 25. Stack-Based Virtual Machine

<!--
difficulty: advanced
category: compilers-virtual-machines
languages: [go, rust]
concepts: [stack-machine, bytecode, call-frames, instruction-set, disassembly, serialization]
estimated_time: 8-12 hours
bloom_level: evaluate
prerequisites: [stack-data-structure, binary-encoding, function-call-conventions, enums-pattern-matching]
-->

## Languages

- Go (1.22+)
- Rust (1.75+ stable)

## Prerequisites

- Stack data structures and their use in expression evaluation
- Binary encoding: big-endian, little-endian, variable-length integers
- How function call conventions work at the machine level (call stack, frames, return addresses)
- Rust enums with data, pattern matching, Vec as stack
- Go interfaces, slices, switch statements

## Learning Objectives

- **Design** a bytecode instruction set that balances expressiveness with implementation simplicity
- **Implement** a fetch-decode-execute cycle that processes a stream of typed bytecode instructions
- **Analyze** how call frames, local variables, and return addresses interact during function calls
- **Evaluate** trade-offs between stack-based and register-based VM architectures
- **Create** a bytecode serialization format and a human-readable disassembler for debugging

## The Challenge

Every high-level language eventually becomes a sequence of primitive operations executed one at a time. Python, Java, C#, Lua, Erlang -- they all compile to bytecode that runs on a virtual machine. The VM is the engine beneath the abstraction.

A stack-based VM uses an operand stack instead of named registers. Operations pop their arguments from the stack and push their results back. This makes the instruction set simpler (no register allocation needed) at the cost of more instructions per operation. It is the architecture behind the JVM, CPython, the CLR, and WebAssembly's execution model.

Your task is to build a stack-based virtual machine that executes a custom bytecode instruction set. The VM must support arithmetic, control flow, function calls with local variables, and a bytecode serialization format that can be loaded from disk. You must also build a disassembler that translates raw bytecode back to human-readable form for debugging.

## Requirements

1. Define a bytecode instruction set with at least these operations: `PUSH`, `POP`, `ADD`, `SUB`, `MUL`, `DIV`, `MOD`, `CMP`, `JMP`, `JZ` (jump if zero), `JNZ` (jump if not zero), `CALL`, `RET`, `LOAD` (local variable), `STORE` (local variable), `PRINT`, `HALT`
2. Implement the fetch-decode-execute loop: read the next instruction from the bytecode stream, decode it, execute it, advance the instruction pointer
3. The operand stack must handle at least 64-bit integers and floating-point values (use a tagged value or union type)
4. Implement a call stack with call frames. Each frame tracks: return address (instruction pointer to resume after RET), base pointer (start of this frame's locals on the stack), local variable slots (at least 256 per frame)
5. `CALL` pushes a new frame with the return address and base pointer. `RET` pops the frame and restores the instruction pointer
6. `LOAD n` reads local variable at slot n in the current frame. `STORE n` writes the top of stack to slot n
7. `CMP` pops two values, pushes -1 (less), 0 (equal), or 1 (greater). Conditional jumps (`JZ`, `JNZ`) use this result
8. Implement a binary serialization format for bytecode: magic bytes, version, constant pool, instruction stream. Must be loadable from a file
9. Implement a disassembler that reads the binary format and prints human-readable assembly (one instruction per line with offsets and operand values)
10. The VM must detect and report runtime errors: stack overflow, stack underflow, division by zero, invalid opcode, out-of-bounds jump target, out-of-bounds local variable access
11. Implement both Go and Rust versions. They must execute the same bytecode format

## Hints

<details>
<summary>Hint 1: Start with the simplest possible VM</summary>

A stack-based VM is simpler than it appears. The core loop is a match/switch on the opcode byte. Start with just `PUSH`, `ADD`, `PRINT`, `HALT` and get those working end-to-end before adding control flow. Once you can execute `PUSH 3; PUSH 7; ADD; PRINT; HALT` and see `10`, you have the foundation. Everything else is adding more arms to the match.

```rust
loop {
    let opcode = bytecode[ip];
    ip += 1;
    match opcode {
        OP_PUSH => { let val = read_operand(); stack.push(val); }
        OP_ADD => { let b = stack.pop(); let a = stack.pop(); stack.push(a + b); }
        OP_HALT => break,
        _ => return Err("invalid opcode"),
    }
}
```
</details>

<details>
<summary>Hint 2: Value representation</summary>

Rust's enum is the natural choice: `enum Value { Int(i64), Float(f64) }`. In Go, use a struct with a type tag. NaN-boxing is a common optimization in production VMs (packing type info into the unused bits of IEEE 754 NaN values) but is not required here -- clarity over cleverness.

For arithmetic operations, decide on type promotion rules: what happens when you `ADD` an Int and a Float? The simplest correct approach is to promote to Float when the types differ, matching how most languages handle mixed arithmetic.
</details>

<details>
<summary>Hint 3: Call frames and local variables</summary>

Call frames are the hardest part. Think of them as a stack of stacks: each frame has its own region for local variables. The frame tracks:

- **Return address**: the instruction pointer to resume after `RET`
- **Base pointer**: where this frame's local variable slots start
- **Local variables**: an array of Value slots, indexed by the `LOAD`/`STORE` operand

When `CALL` executes, it saves the current IP as the return address, creates a new frame with a fresh set of locals, copies arguments into the first local slots, and jumps to the target address. When `RET` executes, it pops the frame and sets IP to the saved return address.

The key insight: arguments are on the operand stack before `CALL`. The CALL instruction must pop them and place them into the new frame's local variables.
</details>

<details>
<summary>Hint 4: Bytecode serialization format</summary>

Keep the format simple. A header with magic bytes and a version number lets you detect invalid files and handle future format changes. Then a constant pool (length-prefixed array of tagged values), followed by the instruction stream as raw bytes.

```
[4 bytes: magic "SVMX"]
[1 byte: version]
[2 bytes: constant count (big-endian u16)]
  [1 byte: type tag] [8 bytes: value] ... repeated
[4 bytes: code length (big-endian u32)]
  [code bytes...]
```

Each instruction is one opcode byte followed by zero or more operand bytes. The operand count is determined by the opcode: `PUSH` has a 2-byte constant pool index, `JMP` has a 4-byte target offset, `LOAD`/`STORE` have a 1-byte slot index, and most arithmetic ops have no operands.
</details>

<details>
<summary>Hint 5: Disassembler implementation</summary>

The disassembler reads the same binary format as the VM but prints instead of executing. Walk through the bytecode stream, decode each instruction, and print its offset, name, and operand values. For `PUSH` instructions, also print the constant value from the pool.

```
0000: PUSH #0 (42)
0003: PUSH #1 (3.14)
0006: ADD
0007: PRINT
0008: HALT
```

This is invaluable for debugging: when the VM reports an error at offset 0006, you can see exactly which instruction failed.
</details>

<details>
<summary>Hint 6: Error handling strategy</summary>

Validate at every step. Before popping the stack, check it is not empty (stack underflow). Before pushing, check it is not full (stack overflow). Before jumping, check the target is within bounds. Before dividing, check the divisor is not zero. Before accessing a local, check the slot is within range.

Every error should include the instruction offset where it occurred. This maps directly to the disassembler output and makes debugging feasible. In Rust, return `Result<(), VmError>` from the run loop. In Go, return an error from `Run()`.
</details>

## Acceptance Criteria

- [ ] All 17 instructions execute correctly in both Go and Rust
- [ ] Arithmetic operations handle both integer and float values
- [ ] Function calls and returns work correctly with nested calls (at least 3 levels deep)
- [ ] Local variables in different call frames are isolated from each other
- [ ] A recursive factorial program executes correctly for input 10 (result: 3628800)
- [ ] A Fibonacci program executes correctly for input 20 (result: 6765)
- [ ] The disassembler produces correct human-readable output for any valid bytecode file
- [ ] Bytecode can be serialized to a file and loaded back, producing identical execution
- [ ] Runtime errors produce clear messages with the instruction offset where the error occurred
- [ ] Stack overflow is detected before crashing (configurable max stack depth, default 1024 frames)
- [ ] Both implementations execute the same bytecode binary and produce identical output

## Research Resources

- [Crafting Interpreters, Chapter 14-15: Chunks of Bytecode and A Virtual Machine](https://craftinginterpreters.com/chunks-of-bytecode.html) -- Robert Nystrom's walk-through of building clox's stack VM, the single best resource for this challenge
- [WebAssembly Specification: Execution](https://webassembly.github.io/spec/core/exec/index.html) -- formal spec of a production stack-based VM; study the operand stack and call frame semantics
- [Lua 5.x VM Internals](https://www.lua.org/doc/jucs05.pdf) -- Lua's transition from stack-based to register-based; explains trade-offs between the two architectures
- [Java Virtual Machine Specification: Instruction Set](https://docs.oracle.com/javase/specs/jvms/se21/html/jvms-6.html) -- the JVM's stack-based instruction set for reference on opcode design
- [Write Your Own Virtual Machine (LC-3)](https://www.jmeiners.com/lc3-vm/) -- step-by-step tutorial building a register-based VM; useful contrast to the stack-based approach
- [NaN-boxing in SpiderMonkey](https://piotrduperas.com/posts/nan-boxing) -- how production VMs pack type tags into floating point NaN payloads
