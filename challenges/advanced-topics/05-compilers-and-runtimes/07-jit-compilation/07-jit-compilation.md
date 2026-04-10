<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [jit-compilation, copy-and-patch, tracing-jit, baseline-jit, optimizing-jit, mmap-executable-memory, deoptimization, speculation]
languages: [go, rust]
estimated_reading_time: 75-90 min
bloom_level: analyze
prerequisites: [register-allocation, optimization-passes, x86-64-basics, mmap-syscall]
papers: [Aycock-2003-JIT-survey, Gal-2009-tracing-JIT]
industry_use: [v8, cpython-313, luajit, hotspot-jvm, wasmtime-cranelift]
language_contrast: medium
-->

# JIT Compilation

> A JIT compiler is a bet: the cost of compilation is justified only if the compiled code runs often enough to amortize it — and the only way to know which code is "hot enough" is to profile at runtime.

## Mental Model

Ahead-of-time (AOT) compilers like `go build` and `rustc` compile code once, before it runs. JIT (Just-In-Time) compilers compile code at runtime, while the program is executing. The motivation: code that has never run is not worth optimizing; code that runs in tight loops is worth spending significant time on.

The classic JIT architecture has two levels:
1. **Interpreter or baseline JIT**: execute code immediately, with minimal compilation. Collect profiling data (which functions are hot, what types flow through each call site).
2. **Optimizing JIT**: take hot functions (above a frequency threshold) and compile them with expensive optimization passes. Use the profiling data to make speculative optimizations (e.g., "this `+` always adds two integers — emit integer add, not a generic add handler").

The speculative optimizations are the key: an AOT compiler must generate code that handles all possible types. A JIT, knowing the types from runtime profiling, can generate code specialized for the observed types. This is why V8's optimized JavaScript can outperform naive C: the JIT's type-specialized code is equivalent to what a C programmer would write, but the JIT derives it automatically.

The downside is deoptimization: when a speculative assumption is violated (a type changes), the compiled code is discarded and execution falls back to the interpreter. This is a controlled process — the JIT maintains enough metadata to resume execution in the interpreter at the exact instruction where the deoptimization occurred.

**Copy-and-patch JIT** (used in CPython 3.13): a simpler approach than full JIT. Pre-compile template machine code for each bytecode operation, with "holes" for operands. At runtime, copy the template and patch the holes with actual values. No register allocation, no instruction selection — just memcpy + small fixups. Dramatically reduces JIT compiler complexity at the cost of generated code quality.

Understanding JIT is important not just for implementing VMs, but for using them:
- **JVM warmup**: HotSpot starts interpreted, profiles, then compiles hot methods. The first few seconds of a Java service have worse latency. Pre-warm critical paths in production JVMs.
- **V8 hidden classes**: adding properties to a JavaScript object in different orders creates different hidden classes (inline caches expect a specific layout). Code that respects consistent object shapes gets inline-cached calls; code that does not falls back to dictionary lookup.
- **WebAssembly Cranelift**: Wasmtime compiles Wasm bytecode to native code using Cranelift, a JIT-optimized code generator. Understanding Cranelift's trade-offs helps you write Wasm that compiles efficiently.

## Core Concepts

### Executable Memory via mmap

JIT compilers write machine code to memory and then execute it. Normal memory allocations are not executable (the NX bit is set for security). To create executable memory:

1. `mmap(NULL, size, PROT_READ|PROT_WRITE, MAP_PRIVATE|MAP_ANONYMOUS, -1, 0)` — allocate writeable memory
2. Write machine code bytes to the allocation
3. `mprotect(ptr, size, PROT_READ|PROT_EXEC)` — make it executable (removing write access)
4. Cast the pointer to a function pointer and call it

The two-step (write then protect) is a security requirement: W^X (Write XOR Execute) prevents pages from being both writable and executable simultaneously. This prevents code injection: an attacker who can write to memory cannot also execute that memory.

### Instruction Encoding (x86-64)

JIT compilation at the lowest level is encoding instructions as bytes. For x86-64:
- `MOVQ rax, imm64`: `0x48 0xB8` followed by 8 bytes (little-endian immediate)
- `ADDQ rax, rcx`: `0x48 0x01 0xC8`
- `MOVQ rax, [rsp+8]`: `0x48 0x8B 0x44 0x24 0x08` (ModRM encoding)
- `RET`: `0xC3`

The `0x48` prefix (REX.W) indicates 64-bit operand size. Without it, most instructions operate on 32-bit registers. Getting REX prefixes right is a major source of bugs in hand-rolled JIT compilers.

### Tracing JIT

A tracing JIT identifies hot loops (rather than hot functions) and records a linear trace of executed instructions through the loop body. The trace is then compiled and optimized as a single linear unit — branching instructions that were always-taken become unconditional, and guards are inserted for the assumptions that were not always true.

LuaJIT's tracing JIT is the canonical example. It achieves near-C performance on numeric Lua code by tracing loop bodies and specializing them for the observed types. The limitation: traces don't compose naturally — a trace for an inner loop does not automatically incorporate the outer loop's context.

### Baseline JIT vs Optimizing JIT

Modern runtimes (V8, HotSpot, Graal) use multiple tiers:
1. **Parser/decoder**: validate bytecode, build an IR
2. **Baseline (Liftoff in V8, C1 in HotSpot)**: compile each bytecode independently with template code. No optimization. Collect type feedback at call sites.
3. **Optimizing (TurboFan in V8, C2/Graal in HotSpot)**: compile hot functions using the type feedback. Full optimization pipeline (inlining, SCCP, loop opts, register allocation). Install deoptimization checkpoints.
4. **Deoptimizer**: when a speculative assumption fails, invalidate the compiled code and resume in baseline or interpreter.

The tiers allow the system to start fast (no JIT warmup) while eventually reaching peak performance (after enough profiling data is collected).

## Implementation: Go

```go
//go:build linux || darwin

// A minimal JIT compiler for x86-64 on Linux/macOS.
// We emit machine code bytes directly into executable memory.
// Function: add(a, b int64) int64 — returns a + b.
// This demonstrates: mmap, instruction encoding, calling convention.

package main

import (
	"fmt"
	"syscall"
	"unsafe"
)

// x86-64 calling convention (System V AMD64 ABI):
// Integer arguments: rdi, rsi, rdx, rcx, r8, r9
// Return value: rax
// So: add(a, b) -> rdi=a, rsi=b, return rax

// Our function in assembly:
//   MOVQ rdi, rax    ; rax = a  (48 89 f8)
//   ADDQ rsi, rax    ; rax += b (48 01 f0)
//   RET              ; return   (c3)

var addFuncCode = []byte{
	0x48, 0x89, 0xF8, // MOVQ rdi, rax   (mov rax, rdi)
	0x48, 0x01, 0xF0, // ADDQ rsi, rax   (add rax, rsi)
	0xC3,             // RET
}

// mul(a, b int64) int64 — multiply a and b
// IMULQ rdi, rsi    ; rsi *= rdi  (48 0F AF F7)
// MOVQ rsi, rax     ; rax = rsi   (48 89 F0)
// RET               ; return      (C3)
var mulFuncCode = []byte{
	0x48, 0x0F, 0xAF, 0xF7, // IMULQ rdi, rsi   (imul rsi, rdi)
	0x48, 0x89, 0xF0,        // MOVQ rsi, rax
	0xC3,                    // RET
}

// fib(n int64) int64 — first 8 Fibonacci numbers (iterative, for demonstration)
// This is more complex: needs a loop, conditional branch.
//
// Encoded:
//   TEST rdi, rdi           ; if n <= 1
//   JLE  .base              ;   return n
//   MOVQ $0, rax            ; a = 0
//   MOVQ $1, rcx            ; b = 1
//   MOVQ rdi, rdx           ; counter = n
// .loop:
//   MOVQ rax, r8            ; tmp = a
//   ADDQ rcx, rax           ; a = a + b
//   MOVQ r8, rcx            ; b = tmp
//   DECQ rdx                ; counter--
//   CMPQ rdx, $1
//   JG   .loop              ; if counter > 1, loop
//   RET
// .base:
//   MOVQ rdi, rax           ; return n
//   RET
var fibFuncCode = []byte{
	// TEST rdi, rdi  (48 85 FF)
	0x48, 0x85, 0xFF,
	// JLE +17 (skip to base case)
	0x7E, 0x11,
	// MOVQ $0, rax  (48 C7 C0 00 00 00 00)
	0x48, 0xC7, 0xC0, 0x00, 0x00, 0x00, 0x00,
	// MOVQ $1, rcx  (48 C7 C1 01 00 00 00)
	0x48, 0xC7, 0xC1, 0x01, 0x00, 0x00, 0x00,
	// MOVQ rdi, rdx  (48 89 FA)
	0x48, 0x89, 0xFA,
	// .loop:
	// MOVQ rax, r8   (49 89 C0)
	0x49, 0x89, 0xC0,
	// ADDQ rcx, rax  (48 01 C8)
	0x48, 0x01, 0xC8,
	// MOVQ r8, rcx   (4C 89 C1)
	0x4C, 0x89, 0xC1,
	// DECQ rdx       (48 FF CA)
	0x48, 0xFF, 0xCA,
	// CMPQ rdx, $1   (48 83 FA 01)
	0x48, 0x83, 0xFA, 0x01,
	// JG -15         (7F F1 = JG with rel8 offset back to .loop)
	0x7F, 0xF1,
	// RET            (C3)
	0xC3,
	// .base:
	// MOVQ rdi, rax  (48 89 F8)
	0x48, 0x89, 0xF8,
	// RET            (C3)
	0xC3,
}

type int64Func func(int64, int64) int64
type int64UnaryFunc func(int64) int64

// jitAlloc allocates executable memory, writes code bytes, and returns a function pointer.
func jitAllocBinary(code []byte) int64Func {
	// Allocate writable memory
	mem, err := syscall.Mmap(-1, 0, len(code),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS,
	)
	if err != nil {
		panic(fmt.Sprintf("mmap failed: %v", err))
	}

	// Copy machine code bytes
	copy(mem, code)

	// Make executable (remove write, add execute)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		syscall.Munmap(mem)
		panic(fmt.Sprintf("mprotect failed: %v", err))
	}

	// Cast memory pointer to a Go function.
	// This is unsafe: we are telling Go that the memory at mem[0] is a function.
	// unsafe.Pointer bypasses Go's type system; *(*int64Func) dereferences as a function value.
	fnPtr := *(*int64Func)(unsafe.Pointer(&mem[0]))
	return fnPtr
}

func jitAllocUnary(code []byte) int64UnaryFunc {
	mem, err := syscall.Mmap(-1, 0, len(code),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS,
	)
	if err != nil {
		panic(fmt.Sprintf("mmap failed: %v", err))
	}
	copy(mem, code)
	if err := syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC); err != nil {
		syscall.Munmap(mem)
		panic(fmt.Sprintf("mprotect failed: %v", err))
	}
	fnPtr := *(*int64UnaryFunc)(unsafe.Pointer(&mem[0]))
	return fnPtr
}

func main() {
	fmt.Println("=== x86-64 JIT Compilation Demo ===\n")

	// Compile and execute add(a, b)
	jitAdd := jitAllocBinary(addFuncCode)
	for _, pair := range [][2]int64{{3, 4}, {100, 200}, {-5, 10}} {
		result := jitAdd(pair[0], pair[1])
		fmt.Printf("jit_add(%d, %d) = %d  (expected: %d)\n",
			pair[0], pair[1], result, pair[0]+pair[1])
	}

	// Compile and execute mul(a, b)
	jitMul := jitAllocBinary(mulFuncCode)
	for _, pair := range [][2]int64{{3, 4}, {7, 8}, {-2, 5}} {
		result := jitMul(pair[0], pair[1])
		fmt.Printf("jit_mul(%d, %d) = %d  (expected: %d)\n",
			pair[0], pair[1], result, pair[0]*pair[1])
	}

	// Compile and execute fib(n)
	fibCode := []byte{
		0x48, 0x85, 0xFF,
		0x7E, 0x11,
		0x48, 0xC7, 0xC0, 0x00, 0x00, 0x00, 0x00,
		0x48, 0xC7, 0xC1, 0x01, 0x00, 0x00, 0x00,
		0x48, 0x89, 0xFA,
		0x49, 0x89, 0xC0,
		0x48, 0x01, 0xC8,
		0x4C, 0x89, 0xC1,
		0x48, 0xFF, 0xCA,
		0x48, 0x83, 0xFA, 0x01,
		0x7F, 0xF1,
		0xC3,
		0x48, 0x89, 0xF8,
		0xC3,
	}

	type unaryFib func(int64) int64
	mem, _ := syscall.Mmap(-1, 0, len(fibCode),
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
	copy(mem, fibCode)
	syscall.Mprotect(mem, syscall.PROT_READ|syscall.PROT_EXEC)
	jitFib := *(*unaryFib)(unsafe.Pointer(&mem[0]))

	expected := []int64{0, 1, 1, 2, 3, 5, 8, 13}
	fmt.Println()
	for n := int64(0); n <= 7; n++ {
		result := jitFib(n)
		fmt.Printf("jit_fib(%d) = %d  (expected: %d)  [%s]\n",
			n, result, expected[n], statusStr(result == expected[n]))
	}

	fmt.Println("\n=== Machine Code Sizes ===")
	fmt.Printf("add function: %d bytes\n", len(addFuncCode))
	fmt.Printf("mul function: %d bytes\n", len(mulFuncCode))
	fmt.Printf("fib function: %d bytes\n", len(fibCode))
}

func statusStr(ok bool) string {
	if ok {
		return "OK"
	}
	return "FAIL"
}
```

### Go-specific considerations

**`//go:build linux || darwin`**: The `syscall.MAP_ANONYMOUS` constant has different values on Linux vs macOS (0x20 vs 0x1000). On macOS, use `syscall.MAP_ANON`. The build tag restricts compilation to supported platforms.

**Go's internal JIT**: Go itself does not have a JIT for user code. The Go compiler is AOT only. However, Go's runtime includes a small amount of "code generation" for goroutine stacks (runtime assembly stubs), interface calls (itable lookups), and the goroutine scheduler. These are not traditional JIT compilation but demonstrate similar techniques.

**Alternatives to raw mmap**: The `goja` library (a JavaScript engine written in Go) implements a full JIT pipeline entirely in Go without using `syscall.Mmap` directly — it uses `mmap` through `os.File` on macOS. `wasmtime-go` wraps Wasmtime (a WebAssembly JIT) using CGo.

**Safety of function pointer casting**: The `*(*func)(unsafe.Pointer(&mem[0]))` pattern works because Go function values are represented as a pointer to a code pointer (two levels of indirection). `mem[0]` is the start of the machine code; casting the pointer to `*func` and dereferencing gives the function value that the Go runtime can call. This is inherently unsafe and machine-specific.

## Implementation: Rust

```rust
// JIT compilation in Rust using raw mmap and x86-64 instruction encoding.
// Also demonstrates Cranelift as a higher-level alternative.
//
// Build: cargo build --target x86_64-unknown-linux-gnu (or x86_64-apple-darwin)
//
// Safety note: this code uses unsafe Rust to write and execute machine code.
// The JIT security model requires W^X: write, then mprotect to exec-only.

use std::io;

// --- Low-level JIT: raw mmap + instruction bytes ---

// Instruction encoder: collects bytes for x86-64 instructions.
pub struct Assembler {
    buf: Vec<u8>,
}

impl Assembler {
    pub fn new() -> Self {
        Assembler { buf: Vec::with_capacity(64) }
    }

    // REX.W prefix for 64-bit operations
    fn rex_w(&mut self) { self.buf.push(0x48); }

    // MOV rax, rdi  (48 89 F8)
    pub fn mov_rax_rdi(&mut self) {
        self.rex_w();
        self.buf.extend_from_slice(&[0x89, 0xF8]);
    }

    // ADD rax, rsi  (48 01 F0)
    pub fn add_rax_rsi(&mut self) {
        self.rex_w();
        self.buf.extend_from_slice(&[0x01, 0xF0]);
    }

    // IMUL rsi, rdi  (48 0F AF F7)
    pub fn imul_rsi_rdi(&mut self) {
        self.rex_w();
        self.buf.extend_from_slice(&[0x0F, 0xAF, 0xF7]);
    }

    // MOV rax, rsi  (48 89 F0)
    pub fn mov_rax_rsi(&mut self) {
        self.rex_w();
        self.buf.extend_from_slice(&[0x89, 0xF0]);
    }

    // MOV rax, imm64  (48 B8 <8 bytes>)
    pub fn mov_rax_imm64(&mut self, imm: i64) {
        self.rex_w();
        self.buf.push(0xB8);
        self.buf.extend_from_slice(&imm.to_le_bytes());
    }

    // RET  (C3)
    pub fn ret(&mut self) {
        self.buf.push(0xC3);
    }

    pub fn code(&self) -> &[u8] {
        &self.buf
    }
}

// JIT memory allocation: mmap + mprotect
#[cfg(any(target_os = "linux", target_os = "macos"))]
mod jit_mem {
    use std::io;

    pub struct JitMemory {
        ptr: *mut u8,
        len: usize,
    }

    impl JitMemory {
        pub fn new(code: &[u8]) -> io::Result<Self> {
            let len = (code.len() + 4095) & !4095; // round up to page size

            let ptr = unsafe {
                let ptr = libc_mmap(len)?;
                std::ptr::copy_nonoverlapping(code.as_ptr(), ptr, code.len());
                libc_mprotect_rx(ptr, len)?;
                ptr
            };

            Ok(JitMemory { ptr, len })
        }

        // Returns a function pointer to the JIT code.
        // SAFETY: The caller must ensure the code at ptr has the expected signature.
        pub unsafe fn as_fn_i64_i64(&self) -> unsafe extern "C" fn(i64, i64) -> i64 {
            std::mem::transmute(self.ptr)
        }

        pub unsafe fn as_fn_i64(&self) -> unsafe extern "C" fn(i64) -> i64 {
            std::mem::transmute(self.ptr)
        }
    }

    impl Drop for JitMemory {
        fn drop(&mut self) {
            unsafe {
                libc_munmap(self.ptr, self.len);
            }
        }
    }

    #[cfg(target_os = "linux")]
    unsafe fn libc_mmap(len: usize) -> io::Result<*mut u8> {
        use libc::{mmap, MAP_ANONYMOUS, MAP_PRIVATE, PROT_READ, PROT_WRITE};
        let ptr = mmap(
            std::ptr::null_mut(),
            len,
            PROT_READ | PROT_WRITE,
            MAP_PRIVATE | MAP_ANONYMOUS,
            -1,
            0,
        );
        if ptr == libc::MAP_FAILED {
            Err(io::Error::last_os_error())
        } else {
            Ok(ptr as *mut u8)
        }
    }

    #[cfg(target_os = "macos")]
    unsafe fn libc_mmap(len: usize) -> io::Result<*mut u8> {
        use libc::{mmap, MAP_ANON, MAP_PRIVATE, PROT_READ, PROT_WRITE};
        let ptr = mmap(
            std::ptr::null_mut(),
            len,
            PROT_READ | PROT_WRITE,
            MAP_PRIVATE | MAP_ANON,
            -1,
            0,
        );
        if ptr == libc::MAP_FAILED {
            Err(io::Error::last_os_error())
        } else {
            Ok(ptr as *mut u8)
        }
    }

    unsafe fn libc_mprotect_rx(ptr: *mut u8, len: usize) -> io::Result<()> {
        use libc::{mprotect, PROT_EXEC, PROT_READ};
        if mprotect(ptr as *mut libc::c_void, len, PROT_READ | PROT_EXEC) != 0 {
            Err(io::Error::last_os_error())
        } else {
            Ok(())
        }
    }

    unsafe fn libc_munmap(ptr: *mut u8, len: usize) {
        libc::munmap(ptr as *mut libc::c_void, len);
    }
}

#[cfg(any(target_os = "linux", target_os = "macos"))]
use jit_mem::JitMemory;

fn main() {
    #[cfg(any(target_os = "linux", target_os = "macos"))]
    {
        println!("=== Rust x86-64 JIT Demo ===\n");

        // Compile add(a, b) -> a + b
        let mut asm = Assembler::new();
        asm.mov_rax_rdi();   // rax = a (first arg)
        asm.add_rax_rsi();   // rax += b (second arg)
        asm.ret();

        let mem = JitMemory::new(asm.code()).expect("JIT memory allocation failed");
        let jit_add = unsafe { mem.as_fn_i64_i64() };

        for (a, b) in [(3i64, 4), (100, 200), (-5, 10)] {
            let result = unsafe { jit_add(a, b) };
            println!("jit_add({}, {}) = {}  (expected: {})", a, b, result, a + b);
        }

        // Compile mul(a, b) -> a * b
        let mut asm2 = Assembler::new();
        asm2.imul_rsi_rdi();  // rsi *= rdi (a * b, result in rsi)
        asm2.mov_rax_rsi();   // rax = rsi (return value)
        asm2.ret();

        let mem2 = JitMemory::new(asm2.code()).expect("JIT memory allocation failed");
        let jit_mul = unsafe { mem2.as_fn_i64_i64() };

        println!();
        for (a, b) in [(3i64, 4), (7, 8), (-2, 5)] {
            let result = unsafe { jit_mul(a, b) };
            println!("jit_mul({}, {}) = {}  (expected: {})", a, b, result, a * b);
        }

        // Show machine code bytes
        println!("\n=== Machine Code Bytes ===");
        print!("add: ");
        for b in asm.code() { print!("{:02X} ", b); }
        println!("({} bytes)", asm.code().len());

        print!("mul: ");
        for b in asm2.code() { print!("{:02X} ", b); }
        println!("({} bytes)", asm2.code().len());
    }

    #[cfg(not(any(target_os = "linux", target_os = "macos")))]
    {
        println!("JIT demo requires Linux or macOS (x86-64).");
        println!("For Windows, use VirtualAlloc + VirtualProtect instead of mmap.");
    }

    println!("\n=== JIT Strategy Comparison ===");
    println!("Copy-and-patch (CPython 3.13):");
    println!("  - Pre-compile templates for each bytecode");
    println!("  - At runtime: memcpy template, patch operand holes");
    println!("  - No register allocation, no instruction selection");
    println!("  - Compilation cost: O(bytecodes) with small constant");
    println!();
    println!("Tracing JIT (LuaJIT):");
    println!("  - Record hot loop traces at runtime");
    println!("  - Specialize trace for observed types");
    println!("  - Full register allocation + instruction selection per trace");
    println!("  - Compilation cost: higher, but only for hot loops");
    println!();
    println!("Cranelift (Wasmtime):");
    println!("  - Full SSA-based IR → machine code pipeline");
    println!("  - Optimized for JIT speed (< 1ms per function)");
    println!("  - Linear scan register allocation");
    println!("  - No speculative optimization (deterministic Wasm semantics)");
}
```

### Rust-specific considerations

**The `libc` crate**: Rust's `libc` crate provides raw C function bindings including `mmap`, `mprotect`, `munmap`. All calls are `unsafe`. The Cargo.toml needs `libc = "0.2"` as a dependency.

**`std::mem::transmute` for function pointers**: `transmute(ptr)` converts a raw pointer to a function pointer of the specified type. This is safe only if:
1. The memory at `ptr` contains valid machine code with the expected calling convention
2. The function signature matches exactly (argument count, types, return type)
3. The memory is readable and executable (mprotect succeeded)

Any mismatch causes undefined behavior — typically a segfault or incorrect results.

**Cranelift as a safe JIT backend**: The `cranelift-codegen` crate provides a full JIT code generation pipeline:
```toml
# Cargo.toml
cranelift-codegen = "0.110"
cranelift-frontend = "0.110"
cranelift-jit = "0.110"
```
Cranelift handles instruction selection, register allocation, and mmap management. You provide a Cranelift IR function (similar to LLVM IR), and it produces executable machine code. `wasmtime` is built on Cranelift. The advantage over raw instruction encoding: portability (supports x86-64, aarch64, s390x), correctness, and a stable API.

**`dynasm-rs`**: A proc-macro that allows writing x86 assembly syntax in Rust source code:
```rust
dynasm!(ops
    ; mov rax, rdi
    ; add rax, rsi
    ; ret
);
```
The proc-macro expands this to the correct byte sequences at compile time. Eliminates encoding errors while keeping the low-level control.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| User-space JIT support | `syscall.Mmap` + unsafe pointer cast | `libc::mmap` + `std::mem::transmute`; or Cranelift |
| Higher-level JIT library | `goja` (JS), `tinygo` | `cranelift-jit`, `wasmer-compiler-cranelift` |
| Safety | Unsafe.Pointer unavoidable | `unsafe {}` block required |
| JIT warmup concern | N/A (Go is AOT) | Wasmtime Wasm JIT has warmup; pure Rust does not |
| Platform support | mmap on Unix; no equivalent on Windows in stdlib | `libc` + platform-specific; Cranelift handles portability |
| Instruction encoding | Manual byte arrays | Manual bytes, or `dynasm-rs` proc-macro |

## Production War Stories

**LuaJIT: The Pinnacle of Tracing JIT**: Mike Pall's LuaJIT is consistently cited as the most sophisticated JIT compiler ever written by a single person. It runs numeric Lua code at speeds within 10–20% of optimized C. The key techniques: aggressive trace specialization, a custom SSA IR called "LIR" that maps closely to x86 instructions, and a register allocator that exploits x86's large FP register set for numeric code. LuaJIT's source is famously opaque but worth studying: `src/lj_trace.c`, `src/lj_asm.c`. The lesson: tracing JIT can match AOT performance for regular numeric loops with homogeneous types.

**CPython 3.13's copy-and-patch JIT**: CPython 3.11 introduced a "specializing adaptive interpreter" that patches bytecode in-place when a call site is observed to always call the same function. CPython 3.13 extended this with a copy-and-patch JIT that emits x86-64 machine code. The code quality is lower than a full JIT (no register allocation across bytecodes) but the compilation cost is extremely low. Benchmarks show 5–30% improvement on Python microbenchmarks. The lesson: a simpler JIT that ships is often better than a theoretically optimal one that never does.

**V8's Maglev (2023)**: V8 introduced a third JIT tier between Sparkplug (baseline) and TurboFan (optimizing). Maglev provides SSA-based optimization (better than Sparkplug) without TurboFan's full pipeline cost. The impetus: TurboFan's optimization overhead was measurable on short-lived code (e.g., React rendering). Maglev generates code in ~10ms per function; TurboFan takes ~50ms. For code that runs many times but not millions of times, Maglev is the sweet spot.

**JVM deoptimization storms**: HotSpot's JIT deoptimizes compiled code when a type assumption is violated. In a production JVM, a deoptimization chain can occur: TurboFan (sorry — C2) compiles method A assuming type X. Type Y appears. C2 deoptimizes A, recompiles with both types. But now method B (which calls A) assumed A would return type X... and B must also be deoptimized. In pathological cases, this creates a "deoptimization storm" where the JVM spends significant time recompiling the call graph. The fix: avoid creating new subtypes of performance-critical base classes in production code paths.

## Complexity Analysis

| JIT Strategy | Compilation Time | Code Quality | Memory Overhead |
|-------------|-----------------|-------------|----------------|
| Copy-and-patch | O(bytecodes) | Low (template quality) | Low |
| Baseline (per-bytecode template) | O(bytecodes) | Low–medium | Low |
| Tracing JIT | O(trace length) per trace | High (for the traced path) | Medium (trace cache) |
| Method JIT (SSA + full opts) | O(function size · opt passes) | High | High (compiled code cache) |
| Cranelift | O(IR size) — fast in practice | Medium–high | Medium |

## Common Pitfalls

**1. Writing executable and writable memory simultaneously.** Some systems (older Linux, macOS with MAP_JIT) allow W|X pages. This is a security vulnerability and is blocked on hardened systems (Apple Silicon). Always: write with W, then mprotect to R|X before executing.

**2. Stale instruction cache.** On x86-64, the instruction and data caches are kept coherent by hardware. On ARM (including Apple Silicon / M-series), they are not. After writing machine code on ARM, you must explicitly flush the instruction cache before executing: `__builtin___clear_cache(start, end)` in C, or `std::arch::asm!("ISB")` in Rust. Forgetting this causes random crashes on ARM JITs.

**3. Incorrect REX prefix usage.** On x86-64, using 32-bit operations (`MOV eax, ...` instead of `MOV rax, ...`) implicitly zero-extends the register and clears the upper 32 bits. This is correct behavior but can cause bugs if you expect the upper bits to be preserved. REX.W prefix (`0x48`) ensures 64-bit operation.

**4. Stack alignment violations.** The x86-64 System V ABI requires the stack to be 16-byte aligned at function call boundaries. If your JIT-compiled code calls C functions (e.g., for helper routines), the stack must be aligned at the call site. Forgetting this causes mysterious segfaults in SSE operations, which require 16-byte alignment.

**5. Not handling guard failures in tracing JIT.** A trace records the "happy path" through a loop. When a guard fails (an assumed branch direction is wrong), execution must exit the trace and return to the interpreter at the correct bytecode position. Implementing this "side exit" correctly — preserving all local variable state in the format the interpreter expects — is the hardest part of tracing JIT.

## Exercises

**Exercise 1** (30 min): Extend the Assembler in Rust to emit code for `sub(a, b) = a - b` and `neg(a) = -a`. Verify by executing the JIT-compiled functions with test inputs.

**Exercise 2** (2–4h): Implement a simple copy-and-patch JIT for the TAC IR from section 02. For each TAC instruction type (`TACAdd`, `TACSub`, etc.), pre-define a template of x86-64 bytes with 8-byte holes for the operands. At JIT compile time, copy each template and patch the holes with the actual virtual register values (as offsets into a "register file" array on the stack). Execute and verify.

**Exercise 3** (4–8h): Using Cranelift (`cranelift-jit` crate), implement a JIT compiler for the expression evaluator from section 01. Parse an expression string, build a Cranelift function IR (using `cranelift_frontend::FunctionBuilder`), compile it, and execute it. Handle `+`, `-`, `*`, `/` and integer literals. Compare the generated asm (from `cranelift-codegen`'s debug output) to hand-encoded instructions.

**Exercise 4** (8–15h): Implement a minimal tracing JIT for the toy interpreter from section 02. Add a "hotness counter" to each loop back-edge: when a loop's counter exceeds 100, start recording a trace. The trace records executed instructions and their observed operand types. When the trace completes one loop iteration, compile it to machine code (using the Assembler or Cranelift). Add a guard at the trace's entry for the type assumptions. Measure the speedup on a tight loop like `while i < 1000000 { i = i + 1 }`.

## Further Reading

### Foundational Papers
- **Aycock, 2003** — "A Brief History of Just-In-Time." Survey of JIT techniques from LISP to Java. Good overview before diving into specifics.
- **Gal et al., 2009** — "Trace-Based Just-in-Time Type Specialization for Dynamic Languages." The TraceMonkey tracing JIT for Firefox's SpiderMonkey.
- **Wimmer & Mössenböck, 2005** — "Optimized Interval Splitting in a Linear Scan Register Allocator." Relevant for JIT-specific register allocation.
- **Pall, 2021** — LuaJIT source code comments and wiki. `https://wiki.luajit.org/` — Mike Pall's design notes.

### Books
- **Virtual Machines** (Smith & Nair) — Chapters 4–5: Dynamic Binary Optimization.
- **Crafting Interpreters** — Part 3: A Bytecode Virtual Machine. Useful background for JIT design.

### Production Code to Read
- `https://github.com/LuaJIT/LuaJIT/blob/v2.1/src/lj_asm.c` — LuaJIT's assembler backend
- `CPython/Python/jit.c` (CPython 3.13+) — Copy-and-patch JIT implementation
- `v8/src/compiler/backend/` — TurboFan's instruction selection and register allocation
- `wasmtime/cranelift/codegen/src/isa/x64/` — Cranelift's x86-64 backend

### Talks
- **"LuaJIT's Internals"** — Mike Pall, LuaJIT wiki. Essential reference.
- **"V8: Behind the Scenes"** — Benedikt Meurer, JSConf 2018. TurboFan architecture.
- **"Copy-and-Patch: A Fast Compilation Algorithm for High-level Languages and Bytecode"** — Xu et al., OOPSLA 2021. The CPython 3.13 JIT paper.
- **"Cranelift: A New Code Generator"** — Till Schneidereit, WebAssembly Summit 2020.
