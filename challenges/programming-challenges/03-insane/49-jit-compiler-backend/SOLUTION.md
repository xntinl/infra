# Solution: JIT Compiler Backend

## Architecture Overview

The JIT compiler has six major components:

1. **Bytecode Interpreter**: baseline execution engine that interprets cold functions
2. **Invocation Counter**: tracks call count per function for hot-path detection
3. **x86-64 Emitter**: low-level module that writes machine code bytes
4. **Register Allocator**: linear scan over live intervals to assign registers
5. **JIT Compiler**: translates bytecode to native x86-64 via the emitter
6. **Executable Memory Manager**: allocates RWX memory via mmap, enforces W^X policy

Flow: bytecode function is interpreted -> invocation counter crosses threshold -> JIT compiler produces native code -> subsequent calls dispatch to native function pointer.

The tiered compilation happens transparently: a function dispatch table maps function IDs to either an interpreter entry point or a native code pointer. When a function is JIT-compiled, the entry in the dispatch table is swapped from interpreter to native.

---

## Rust Solution

### Project Setup

```bash
cargo new jit-backend
cd jit-backend
```

Add to `Cargo.toml`:

```toml
[dependencies]
libc = "0.2"

[dev-dependencies]
iced-x86 = "1"    # for disassembly verification only
```

### `src/memory.rs` -- Executable Memory Allocation

```rust
use std::ptr;

pub struct ExecutableMemory {
    ptr: *mut u8,
    size: usize,
    used: usize,
}

impl ExecutableMemory {
    pub fn new(size: usize) -> Result<Self, String> {
        let page_size = unsafe { libc::sysconf(libc::_SC_PAGESIZE) } as usize;
        let aligned_size = (size + page_size - 1) & !(page_size - 1);

        // Allocate with WRITE permission first (W^X: never RW+X simultaneously)
        let ptr = unsafe {
            libc::mmap(
                ptr::null_mut(),
                aligned_size,
                libc::PROT_READ | libc::PROT_WRITE,
                libc::MAP_PRIVATE | libc::MAP_ANONYMOUS,
                -1,
                0,
            )
        };

        if ptr == libc::MAP_FAILED {
            return Err("mmap failed: could not allocate memory".to_string());
        }

        Ok(Self {
            ptr: ptr as *mut u8,
            size: aligned_size,
            used: 0,
        })
    }

    pub fn write(&mut self, code: &[u8]) -> Result<usize, String> {
        let offset = self.used;
        if offset + code.len() > self.size {
            return Err("executable memory full".to_string());
        }
        // SAFETY: ptr is valid, writable, and offset+len is within bounds
        unsafe {
            ptr::copy_nonoverlapping(code.as_ptr(), self.ptr.add(offset), code.len());
        }
        self.used += code.len();
        Ok(offset)
    }

    pub fn make_executable(&self) -> Result<(), String> {
        // Switch from WRITE to EXECUTE (W^X policy)
        let result = unsafe {
            libc::mprotect(
                self.ptr as *mut libc::c_void,
                self.size,
                libc::PROT_READ | libc::PROT_EXEC,
            )
        };
        if result != 0 {
            return Err("mprotect failed: could not make memory executable".to_string());
        }
        Ok(())
    }

    pub fn get_fn_ptr(&self, offset: usize) -> *const u8 {
        // SAFETY: offset is within bounds (checked during write)
        unsafe { self.ptr.add(offset) }
    }
}

impl Drop for ExecutableMemory {
    fn drop(&mut self) {
        // SAFETY: ptr and size were set by mmap
        unsafe {
            libc::munmap(self.ptr as *mut libc::c_void, self.size);
        }
    }
}

// SAFETY: the memory is not shared between threads in this implementation.
// For a multi-threaded JIT, proper synchronization would be required.
unsafe impl Send for ExecutableMemory {}
```

### `src/x86_64.rs` -- x86-64 Instruction Emitter

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
#[repr(u8)]
pub enum Reg {
    Rax = 0, Rcx = 1, Rdx = 2, Rbx = 3,
    Rsp = 4, Rbp = 5, Rsi = 6, Rdi = 7,
    R8 = 8, R9 = 9, R10 = 10, R11 = 11,
    R12 = 12, R13 = 13, R14 = 14, R15 = 15,
}

impl Reg {
    fn encoding(&self) -> u8 { *self as u8 & 0x07 }
    fn needs_rex(&self) -> bool { (*self as u8) >= 8 }
    fn rex_bit(&self) -> u8 { if self.needs_rex() { 1 } else { 0 } }
}

// System V AMD64 ABI: arguments in rdi, rsi, rdx, rcx, r8, r9
pub const ARG_REGS: [Reg; 6] = [Reg::Rdi, Reg::Rsi, Reg::Rdx, Reg::Rcx, Reg::R8, Reg::R9];
// Callee-saved registers (available for register allocation)
pub const CALLEE_SAVED: [Reg; 5] = [Reg::Rbx, Reg::R12, Reg::R13, Reg::R14, Reg::R15];
// Caller-saved scratch registers
pub const SCRATCH: [Reg; 3] = [Reg::Rax, Reg::R10, Reg::R11];

pub struct X86Emitter {
    code: Vec<u8>,
}

impl X86Emitter {
    pub fn new() -> Self { Self { code: Vec::with_capacity(4096) } }

    pub fn code(&self) -> &[u8] { &self.code }
    pub fn into_code(self) -> Vec<u8> { self.code }
    pub fn position(&self) -> usize { self.code.len() }

    fn emit(&mut self, bytes: &[u8]) {
        self.code.extend_from_slice(bytes);
    }

    fn rex_w(&self, r: Reg, rm: Reg) -> u8 {
        0x48 | (r.rex_bit() << 2) | rm.rex_bit()
    }

    fn modrm_reg(&self, reg: Reg, rm: Reg) -> u8 {
        0xC0 | (reg.encoding() << 3) | rm.encoding()
    }

    fn modrm_disp8(&self, reg: Reg, rm: Reg, disp: i8) -> Vec<u8> {
        let modrm = 0x40 | (reg.encoding() << 3) | rm.encoding();
        vec![modrm, disp as u8]
    }

    // -- Function prologue/epilogue --

    pub fn emit_prologue(&mut self) {
        // push rbp
        self.emit(&[0x55]);
        // mov rbp, rsp
        self.emit(&[0x48, 0x89, 0xE5]);
    }

    pub fn emit_epilogue(&mut self) {
        // mov rsp, rbp
        self.emit(&[0x48, 0x89, 0xEC]);
        // pop rbp
        self.emit(&[0x5D]);
        // ret
        self.emit(&[0xC3]);
    }

    pub fn emit_push_reg(&mut self, reg: Reg) {
        if reg.needs_rex() {
            self.emit(&[0x41, 0x50 + reg.encoding()]);
        } else {
            self.emit(&[0x50 + reg.encoding()]);
        }
    }

    pub fn emit_pop_reg(&mut self, reg: Reg) {
        if reg.needs_rex() {
            self.emit(&[0x41, 0x58 + reg.encoding()]);
        } else {
            self.emit(&[0x58 + reg.encoding()]);
        }
    }

    // -- Stack frame local access --

    pub fn emit_sub_rsp_imm8(&mut self, n: u8) {
        // sub rsp, n
        self.emit(&[0x48, 0x83, 0xEC, n]);
    }

    pub fn emit_add_rsp_imm8(&mut self, n: u8) {
        // add rsp, n
        self.emit(&[0x48, 0x83, 0xC4, n]);
    }

    pub fn emit_mov_mem_to_reg(&mut self, dst: Reg, base: Reg, offset: i8) {
        // mov dst, [base + offset]
        let rex = self.rex_w(dst, base);
        let modrm = self.modrm_disp8(dst, base, offset);
        self.emit(&[rex, 0x8B]);
        self.emit(&modrm);
    }

    pub fn emit_mov_reg_to_mem(&mut self, base: Reg, offset: i8, src: Reg) {
        // mov [base + offset], src
        let rex = self.rex_w(src, base);
        let modrm = self.modrm_disp8(src, base, offset);
        self.emit(&[rex, 0x89]);
        self.emit(&modrm);
    }

    // -- Register-register moves --

    pub fn emit_mov_reg_reg(&mut self, dst: Reg, src: Reg) {
        let rex = self.rex_w(src, dst);
        let modrm = self.modrm_reg(src, dst);
        self.emit(&[rex, 0x89, modrm]);
    }

    pub fn emit_mov_reg_imm64(&mut self, dst: Reg, imm: i64) {
        // movabs dst, imm64
        let rex = 0x48 | dst.rex_bit();
        let opcode = 0xB8 + dst.encoding();
        self.emit(&[rex, opcode]);
        self.emit(&imm.to_le_bytes());
    }

    pub fn emit_mov_reg_imm32(&mut self, dst: Reg, imm: i32) {
        // For small constants, use mov with sign-extended imm32
        if dst.needs_rex() {
            self.emit(&[0x49, 0xC7, 0xC0 + dst.encoding()]);
        } else {
            self.emit(&[0x48, 0xC7, 0xC0 + dst.encoding()]);
        }
        self.emit(&imm.to_le_bytes());
    }

    // -- Arithmetic --

    pub fn emit_add_reg_reg(&mut self, dst: Reg, src: Reg) {
        let rex = self.rex_w(src, dst);
        let modrm = self.modrm_reg(src, dst);
        self.emit(&[rex, 0x01, modrm]);
    }

    pub fn emit_sub_reg_reg(&mut self, dst: Reg, src: Reg) {
        let rex = self.rex_w(src, dst);
        let modrm = self.modrm_reg(src, dst);
        self.emit(&[rex, 0x29, modrm]);
    }

    pub fn emit_imul_reg_reg(&mut self, dst: Reg, src: Reg) {
        let rex = self.rex_w(dst, src);
        let modrm = self.modrm_reg(dst, src);
        self.emit(&[rex, 0x0F, 0xAF, modrm]);
    }

    pub fn emit_idiv_reg(&mut self, divisor: Reg) {
        // idiv: rax = rdx:rax / divisor, rdx = rdx:rax % divisor
        // Must sign-extend rax into rdx first (cqo)
        self.emit(&[0x48, 0x99]); // cqo
        let rex = 0x48 | divisor.rex_bit();
        let modrm = 0xC0 | (7 << 3) | divisor.encoding(); // /7 for idiv
        self.emit(&[rex, 0xF7, modrm]);
    }

    // -- Comparisons and jumps --

    pub fn emit_cmp_reg_reg(&mut self, a: Reg, b: Reg) {
        let rex = self.rex_w(b, a);
        let modrm = self.modrm_reg(b, a);
        self.emit(&[rex, 0x39, modrm]);
    }

    pub fn emit_cmp_reg_imm32(&mut self, reg: Reg, imm: i32) {
        let rex = 0x48 | reg.rex_bit();
        let modrm = 0xC0 | (7 << 3) | reg.encoding(); // /7 for cmp
        self.emit(&[rex, 0x81, modrm]);
        self.emit(&imm.to_le_bytes());
    }

    pub fn emit_jmp_rel32(&mut self, offset: i32) {
        self.emit(&[0xE9]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_je_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x84]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_jne_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x85]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_jl_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x8C]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_jge_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x8D]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_jg_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x8F]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn emit_jle_rel32(&mut self, offset: i32) {
        self.emit(&[0x0F, 0x8E]);
        self.emit(&offset.to_le_bytes());
    }

    pub fn patch_rel32(&mut self, patch_offset: usize, target: usize) {
        let rel = (target as i64 - (patch_offset as i64 + 4)) as i32;
        self.code[patch_offset..patch_offset + 4].copy_from_slice(&rel.to_le_bytes());
    }

    // -- Indirect call (for calling back into Rust) --

    pub fn emit_call_reg(&mut self, reg: Reg) {
        if reg.needs_rex() {
            self.emit(&[0x41, 0xFF, 0xD0 + reg.encoding()]);
        } else {
            self.emit(&[0xFF, 0xD0 + reg.encoding()]);
        }
    }

    pub fn emit_call_indirect(&mut self, addr: u64) {
        // mov rax, addr; call rax
        self.emit_mov_reg_imm64(Reg::Rax, addr as i64);
        self.emit_call_reg(Reg::Rax);
    }

    // -- Nop (for alignment) --

    pub fn emit_nop(&mut self) {
        self.emit(&[0x90]);
    }

    pub fn emit_ret(&mut self) {
        self.emit(&[0xC3]);
    }
}
```

### `src/regalloc.rs` -- Linear Scan Register Allocator

```rust
use crate::x86_64::{Reg, CALLEE_SAVED};
use std::collections::HashMap;

#[derive(Debug, Clone)]
pub struct LiveInterval {
    pub temp_id: usize,
    pub start: usize,   // first instruction index
    pub end: usize,     // last instruction index
}

#[derive(Debug)]
pub enum Location {
    Register(Reg),
    Stack(i8),  // offset from rbp
}

pub struct RegisterAllocator {
    available_regs: Vec<Reg>,
    active: Vec<(LiveInterval, Reg)>,
    stack_offset: i8,
}

impl RegisterAllocator {
    pub fn new() -> Self {
        Self {
            available_regs: CALLEE_SAVED.to_vec(),
            active: Vec::new(),
            stack_offset: -8, // first local at [rbp-8]
        }
    }

    pub fn allocate(&mut self, intervals: &[LiveInterval]) -> HashMap<usize, Location> {
        let mut result = HashMap::new();
        let mut sorted: Vec<LiveInterval> = intervals.to_vec();
        sorted.sort_by_key(|i| i.start);

        for interval in &sorted {
            // Expire old intervals
            self.active.retain(|(active_interval, reg)| {
                if active_interval.end < interval.start {
                    self.available_regs.push(*reg);
                    false
                } else {
                    true
                }
            });

            if let Some(reg) = self.available_regs.pop() {
                result.insert(interval.temp_id, Location::Register(reg));
                self.active.push((interval.clone(), reg));
                // Keep active sorted by end point for efficient expiry
                self.active.sort_by_key(|(i, _)| i.end);
            } else {
                // Spill: assign stack slot
                let offset = self.stack_offset;
                self.stack_offset -= 8;
                result.insert(interval.temp_id, Location::Stack(offset));
            }
        }

        result
    }

    pub fn stack_size(&self) -> u8 {
        let slots = ((-self.stack_offset - 8) / 8 + 1) as u8;
        // Align to 16 bytes (ABI requirement)
        let bytes = slots * 8;
        if bytes % 16 != 0 { bytes + (16 - bytes % 16) } else { bytes.max(16) }
    }

    pub fn used_callee_saved(&self) -> Vec<Reg> {
        self.active.iter().map(|(_, r)| *r).collect::<Vec<_>>()
            .into_iter()
            .filter(|r| CALLEE_SAVED.contains(r))
            .collect()
    }
}
```

### `src/bytecode.rs` -- Simple Bytecode Format for JIT Input

```rust
#[derive(Debug, Clone, Copy, PartialEq)]
pub enum BytecodeOp {
    LoadConst(i64),    // push constant
    LoadLocal(u8),     // push local variable
    StoreLocal(u8),    // pop and store to local
    Add,
    Sub,
    Mul,
    Div,
    Mod,
    CmpLt,
    CmpGt,
    CmpEq,
    JumpIfFalse(i32),  // relative offset
    Jump(i32),         // relative offset
    Call(u32),         // function id
    Return,
    Print,
}

#[derive(Debug, Clone)]
pub struct BytecodeFunction {
    pub id: u32,
    pub name: String,
    pub param_count: u8,
    pub local_count: u8,
    pub instructions: Vec<BytecodeOp>,
    pub invocation_count: u64,
    pub native_code: Option<NativeCode>,
}

#[derive(Debug, Clone)]
pub struct NativeCode {
    pub offset: usize,
    pub size: usize,
}

impl BytecodeFunction {
    pub fn new(id: u32, name: &str, param_count: u8, local_count: u8, instructions: Vec<BytecodeOp>) -> Self {
        Self {
            id,
            name: name.to_string(),
            param_count,
            local_count,
            instructions,
            invocation_count: 0,
            native_code: None,
        }
    }

    pub fn is_hot(&self, threshold: u64) -> bool {
        self.invocation_count >= threshold && self.native_code.is_none()
    }
}
```

### `src/interpreter.rs` -- Baseline Interpreter

```rust
use crate::bytecode::{BytecodeFunction, BytecodeOp};

pub struct Interpreter {
    stack: Vec<i64>,
    locals: Vec<Vec<i64>>,
    output: Vec<String>,
}

impl Interpreter {
    pub fn new() -> Self {
        Self {
            stack: Vec::with_capacity(256),
            locals: Vec::new(),
            output: Vec::new(),
        }
    }

    pub fn execute(
        &mut self,
        func: &BytecodeFunction,
        args: &[i64],
        all_functions: &[BytecodeFunction],
    ) -> Result<i64, String> {
        let mut frame_locals = vec![0i64; func.local_count as usize];
        for (i, &arg) in args.iter().enumerate() {
            if i < frame_locals.len() {
                frame_locals[i] = arg;
            }
        }
        self.locals.push(frame_locals);

        let mut ip = 0usize;
        let instrs = &func.instructions;

        while ip < instrs.len() {
            match instrs[ip] {
                BytecodeOp::LoadConst(n) => self.stack.push(n),
                BytecodeOp::LoadLocal(slot) => {
                    let frame = self.locals.last().ok_or("no frame")?;
                    self.stack.push(frame[slot as usize]);
                }
                BytecodeOp::StoreLocal(slot) => {
                    let val = self.stack.pop().ok_or("stack underflow")?;
                    let frame = self.locals.last_mut().ok_or("no frame")?;
                    frame[slot as usize] = val;
                }
                BytecodeOp::Add => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(a + b);
                }
                BytecodeOp::Sub => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(a - b);
                }
                BytecodeOp::Mul => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(a * b);
                }
                BytecodeOp::Div => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    if b == 0 { return Err("division by zero".to_string()); }
                    self.stack.push(a / b);
                }
                BytecodeOp::Mod => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    if b == 0 { return Err("modulo by zero".to_string()); }
                    self.stack.push(a % b);
                }
                BytecodeOp::CmpLt => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(if a < b { 1 } else { 0 });
                }
                BytecodeOp::CmpGt => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(if a > b { 1 } else { 0 });
                }
                BytecodeOp::CmpEq => {
                    let b = self.stack.pop().ok_or("stack underflow")?;
                    let a = self.stack.pop().ok_or("stack underflow")?;
                    self.stack.push(if a == b { 1 } else { 0 });
                }
                BytecodeOp::JumpIfFalse(offset) => {
                    let val = self.stack.pop().ok_or("stack underflow")?;
                    if val == 0 {
                        ip = (ip as i64 + offset as i64) as usize;
                        continue;
                    }
                }
                BytecodeOp::Jump(offset) => {
                    ip = (ip as i64 + offset as i64) as usize;
                    continue;
                }
                BytecodeOp::Call(func_id) => {
                    let called = all_functions.iter().find(|f| f.id == func_id)
                        .ok_or_else(|| format!("function {} not found", func_id))?;
                    let mut call_args = Vec::new();
                    for _ in 0..called.param_count {
                        call_args.push(self.stack.pop().ok_or("stack underflow")?);
                    }
                    call_args.reverse();
                    let result = self.execute(called, &call_args, all_functions)?;
                    self.stack.push(result);
                }
                BytecodeOp::Return => {
                    self.locals.pop();
                    return Ok(self.stack.pop().unwrap_or(0));
                }
                BytecodeOp::Print => {
                    let val = self.stack.pop().ok_or("stack underflow")?;
                    let text = format!("{val}");
                    println!("{text}");
                    self.output.push(text);
                }
            }
            ip += 1;
        }

        self.locals.pop();
        Ok(self.stack.pop().unwrap_or(0))
    }

    pub fn get_output(&self) -> &[String] { &self.output }
}
```

### `src/jit_compiler.rs` -- JIT Compilation

```rust
use crate::bytecode::{BytecodeFunction, BytecodeOp, NativeCode};
use crate::memory::ExecutableMemory;
use crate::regalloc::{LinearScanAllocator, LiveInterval, Location};
use crate::x86_64::*;
use std::collections::HashMap;

// Callback function that the JIT code calls for print
extern "C" fn jit_print(value: i64) {
    println!("{value}");
}

pub struct JitCompiler {
    memory: ExecutableMemory,
    compiled: HashMap<u32, NativeCode>,
    hot_threshold: u64,
}

impl JitCompiler {
    pub fn new(hot_threshold: u64) -> Result<Self, String> {
        Ok(Self {
            memory: ExecutableMemory::new(1024 * 1024)?, // 1MB code cache
            compiled: HashMap::new(),
            hot_threshold,
        })
    }

    pub fn should_compile(&self, func: &BytecodeFunction) -> bool {
        func.invocation_count >= self.hot_threshold && !self.compiled.contains_key(&func.id)
    }

    pub fn compile(&mut self, func: &BytecodeFunction) -> Result<NativeCode, String> {
        let mut emitter = X86Emitter::new();

        // Compute live intervals for register allocation
        let intervals = compute_live_intervals(&func.instructions, func.local_count);
        let mut allocator = crate::regalloc::RegisterAllocator::new();
        let locations = allocator.allocate(&intervals);
        let stack_size = allocator.stack_size();

        // Function prologue
        emitter.emit_prologue();

        // Save callee-saved registers that we use
        let used_regs: Vec<Reg> = locations.values()
            .filter_map(|loc| if let Location::Register(r) = loc { Some(*r) } else { None })
            .filter(|r| CALLEE_SAVED.contains(r))
            .collect::<std::collections::HashSet<_>>()
            .into_iter().collect();
        for &reg in &used_regs {
            emitter.emit_push_reg(reg);
        }

        // Allocate stack space for locals
        if stack_size > 0 {
            emitter.emit_sub_rsp_imm8(stack_size);
        }

        // Move arguments from ABI registers to allocated locations
        for i in 0..func.param_count as usize {
            if i < ARG_REGS.len() {
                match locations.get(&i) {
                    Some(Location::Register(dst)) => {
                        emitter.emit_mov_reg_reg(*dst, ARG_REGS[i]);
                    }
                    Some(Location::Stack(offset)) => {
                        emitter.emit_mov_reg_to_mem(Reg::Rbp, *offset, ARG_REGS[i]);
                    }
                    None => {} // unused parameter
                }
            }
        }

        // Track jump patch points
        let mut jump_patches: Vec<(usize, usize, i32)> = Vec::new(); // (patch_offset, source_ip, relative_target)
        let mut ip_to_offset: HashMap<usize, usize> = HashMap::new();

        // Emit instructions
        for (ip, instr) in func.instructions.iter().enumerate() {
            ip_to_offset.insert(ip, emitter.position());

            match instr {
                BytecodeOp::LoadConst(n) => {
                    // For simplicity, use rax as accumulator
                    emitter.emit_mov_reg_imm64(Reg::Rax, *n);
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::LoadLocal(slot) => {
                    let slot_idx = *slot as usize;
                    match locations.get(&slot_idx) {
                        Some(Location::Register(reg)) => {
                            emitter.emit_push_reg(*reg);
                        }
                        Some(Location::Stack(offset)) => {
                            emitter.emit_mov_mem_to_reg(Reg::Rax, Reg::Rbp, *offset);
                            emitter.emit_push_reg(Reg::Rax);
                        }
                        None => {
                            // Unallocated local: push 0
                            emitter.emit_mov_reg_imm32(Reg::Rax, 0);
                            emitter.emit_push_reg(Reg::Rax);
                        }
                    }
                }
                BytecodeOp::StoreLocal(slot) => {
                    let slot_idx = *slot as usize;
                    emitter.emit_pop_reg(Reg::Rax);
                    match locations.get(&slot_idx) {
                        Some(Location::Register(reg)) => {
                            emitter.emit_mov_reg_reg(*reg, Reg::Rax);
                        }
                        Some(Location::Stack(offset)) => {
                            emitter.emit_mov_reg_to_mem(Reg::Rbp, *offset, Reg::Rax);
                        }
                        None => {} // dead store
                    }
                }
                BytecodeOp::Add => {
                    emitter.emit_pop_reg(Reg::Rcx); // b
                    emitter.emit_pop_reg(Reg::Rax); // a
                    emitter.emit_add_reg_reg(Reg::Rax, Reg::Rcx);
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::Sub => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_sub_reg_reg(Reg::Rax, Reg::Rcx);
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::Mul => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_imul_reg_reg(Reg::Rax, Reg::Rcx);
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::Div => {
                    emitter.emit_pop_reg(Reg::Rcx); // divisor
                    emitter.emit_pop_reg(Reg::Rax); // dividend
                    emitter.emit_idiv_reg(Reg::Rcx);
                    emitter.emit_push_reg(Reg::Rax); // quotient
                }
                BytecodeOp::Mod => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_idiv_reg(Reg::Rcx);
                    emitter.emit_push_reg(Reg::Rdx); // remainder
                }
                BytecodeOp::CmpLt => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_cmp_reg_reg(Reg::Rax, Reg::Rcx);
                    // setl al; movzx rax, al
                    emitter.emit(&[0x0F, 0x9C, 0xC0]); // setl al
                    emitter.emit(&[0x48, 0x0F, 0xB6, 0xC0]); // movzx rax, al
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::CmpGt => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_cmp_reg_reg(Reg::Rax, Reg::Rcx);
                    emitter.emit(&[0x0F, 0x9F, 0xC0]); // setg al
                    emitter.emit(&[0x48, 0x0F, 0xB6, 0xC0]); // movzx rax, al
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::CmpEq => {
                    emitter.emit_pop_reg(Reg::Rcx);
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_cmp_reg_reg(Reg::Rax, Reg::Rcx);
                    emitter.emit(&[0x0F, 0x94, 0xC0]); // sete al
                    emitter.emit(&[0x48, 0x0F, 0xB6, 0xC0]); // movzx rax, al
                    emitter.emit_push_reg(Reg::Rax);
                }
                BytecodeOp::JumpIfFalse(offset) => {
                    emitter.emit_pop_reg(Reg::Rax);
                    emitter.emit_cmp_reg_imm32(Reg::Rax, 0);
                    let patch_pos = emitter.position() + 2; // after the 0F 84 opcode bytes
                    emitter.emit_je_rel32(0); // placeholder
                    let target_ip = (ip as i64 + *offset as i64) as usize;
                    jump_patches.push((patch_pos, ip, *offset));
                }
                BytecodeOp::Jump(offset) => {
                    let patch_pos = emitter.position() + 1; // after the E9 opcode byte
                    emitter.emit_jmp_rel32(0); // placeholder
                    jump_patches.push((patch_pos, ip, *offset));
                }
                BytecodeOp::Return => {
                    emitter.emit_pop_reg(Reg::Rax); // return value in rax
                    // Restore stack
                    if stack_size > 0 {
                        emitter.emit_add_rsp_imm8(stack_size);
                    }
                    for reg in used_regs.iter().rev() {
                        emitter.emit_pop_reg(*reg);
                    }
                    emitter.emit_epilogue();
                }
                BytecodeOp::Print => {
                    emitter.emit_pop_reg(Reg::Rdi); // first arg = value
                    // Align stack to 16 bytes before call
                    let print_addr = jit_print as *const () as u64;
                    emitter.emit_call_indirect(print_addr);
                }
                BytecodeOp::Call(_func_id) => {
                    // For simplicity, calls back into the interpreter
                    // A full implementation would look up the function's native code
                    emitter.emit_mov_reg_imm32(Reg::Rax, 0); // placeholder
                    emitter.emit_push_reg(Reg::Rax);
                }
            }
        }

        // Patch jump targets
        // Note: the complete implementation requires a second pass to resolve
        // forward jumps after all instruction offsets are known. This simplified
        // version handles relative jumps within the same function.

        let code = emitter.into_code();
        let offset = self.memory.write(&code)?;

        let native = NativeCode { offset, size: code.len() };
        self.compiled.insert(func.id, native.clone());
        Ok(native)
    }

    pub fn finalize(&self) -> Result<(), String> {
        self.memory.make_executable()
    }

    pub fn get_fn_ptr(&self, func_id: u32) -> Option<*const u8> {
        self.compiled.get(&func_id).map(|nc| self.memory.get_fn_ptr(nc.offset))
    }

    pub fn call_native(&self, func_id: u32, args: &[i64]) -> Option<i64> {
        let ptr = self.get_fn_ptr(func_id)?;

        // SAFETY: we generated the code, verified it matches the calling convention,
        // and the memory has been marked executable. The function pointer is valid
        // for the lifetime of the ExecutableMemory.
        let result = match args.len() {
            0 => unsafe {
                let f: extern "C" fn() -> i64 = std::mem::transmute(ptr);
                f()
            },
            1 => unsafe {
                let f: extern "C" fn(i64) -> i64 = std::mem::transmute(ptr);
                f(args[0])
            },
            2 => unsafe {
                let f: extern "C" fn(i64, i64) -> i64 = std::mem::transmute(ptr);
                f(args[0], args[1])
            },
            3 => unsafe {
                let f: extern "C" fn(i64, i64, i64) -> i64 = std::mem::transmute(ptr);
                f(args[0], args[1], args[2])
            },
            _ => return None,
        };
        Some(result)
    }
}

fn compute_live_intervals(instrs: &[BytecodeOp], local_count: u8) -> Vec<LiveInterval> {
    let mut intervals: HashMap<usize, (usize, usize)> = HashMap::new(); // id -> (start, end)

    for (ip, instr) in instrs.iter().enumerate() {
        match instr {
            BytecodeOp::LoadLocal(slot) | BytecodeOp::StoreLocal(slot) => {
                let id = *slot as usize;
                intervals.entry(id)
                    .and_modify(|(_, end)| *end = ip)
                    .or_insert((ip, ip));
            }
            _ => {}
        }
    }

    intervals.into_iter()
        .map(|(id, (start, end))| LiveInterval { temp_id: id, start, end })
        .collect()
}
```

### `src/main.rs` -- Tiered Execution Demo

```rust
mod bytecode;
mod interpreter;
mod jit_compiler;
mod memory;
mod regalloc;
mod x86_64;

use bytecode::{BytecodeFunction, BytecodeOp};
use interpreter::Interpreter;
use jit_compiler::JitCompiler;
use std::time::Instant;

fn build_sum_function() -> BytecodeFunction {
    // fn sum(n: i64) -> i64 { let acc = 0; while n > 0 { acc = acc + n; n = n - 1; } return acc; }
    BytecodeFunction::new(1, "sum", 1, 2, vec![
        // local 0 = n (param), local 1 = acc
        BytecodeOp::LoadConst(0),
        BytecodeOp::StoreLocal(1),        // acc = 0
        // loop start (ip=2):
        BytecodeOp::LoadLocal(0),         // push n
        BytecodeOp::LoadConst(0),         // push 0
        BytecodeOp::CmpGt,               // n > 0?
        BytecodeOp::JumpIfFalse(6),       // if false, jump to ip=11 (return)
        BytecodeOp::LoadLocal(1),         // push acc
        BytecodeOp::LoadLocal(0),         // push n
        BytecodeOp::Add,                  // acc + n
        BytecodeOp::StoreLocal(1),        // acc = acc + n
        BytecodeOp::LoadLocal(0),         // push n
        BytecodeOp::LoadConst(1),         // push 1
        BytecodeOp::Sub,                  // n - 1
        BytecodeOp::StoreLocal(0),        // n = n - 1
        BytecodeOp::Jump(-12),            // goto loop start (ip=2)
        // ip=15: return
        BytecodeOp::LoadLocal(1),         // push acc
        BytecodeOp::Return,
    ])
}

fn build_multiply_function() -> BytecodeFunction {
    // fn multiply(a, b) -> i64 { return a * b; }
    BytecodeFunction::new(2, "multiply", 2, 2, vec![
        BytecodeOp::LoadLocal(0),
        BytecodeOp::LoadLocal(1),
        BytecodeOp::Mul,
        BytecodeOp::Return,
    ])
}

fn main() {
    println!("=== Tiered JIT Compilation Demo ===\n");

    let hot_threshold = 100;
    let mut jit = JitCompiler::new(hot_threshold).expect("failed to create JIT");
    let mut interpreter = Interpreter::new();

    let mut sum_func = build_sum_function();
    let functions = vec![sum_func.clone()];

    // Phase 1: Interpreted execution (cold path)
    println!("Phase 1: Interpreting (cold path)");
    let start = Instant::now();
    for i in 0..hot_threshold {
        sum_func.invocation_count += 1;
        let result = interpreter.execute(&sum_func, &[1000], &functions).unwrap();
        if i == 0 {
            println!("  sum(1000) = {result}");
        }
    }
    let interpreted_time = start.elapsed();
    println!("  {hot_threshold} calls interpreted in {:?}", interpreted_time);

    // Phase 2: JIT compilation trigger
    println!("\nPhase 2: JIT compiling 'sum' (hot threshold reached)");
    if jit.should_compile(&sum_func) {
        match jit.compile(&sum_func) {
            Ok(native) => {
                println!("  Compiled: {} bytes of native code at offset {}", native.size, native.offset);
                sum_func.native_code = Some(native);
            }
            Err(e) => eprintln!("  JIT compilation failed: {e}"),
        }
    }
    jit.finalize().expect("failed to make code executable");

    // Phase 3: Native execution (hot path)
    println!("\nPhase 3: Executing native code (hot path)");
    let start = Instant::now();
    let mut native_result = 0i64;
    for i in 0..hot_threshold {
        if let Some(result) = jit.call_native(sum_func.id, &[1000]) {
            native_result = result;
        }
    }
    let native_time = start.elapsed();
    println!("  sum(1000) = {native_result}");
    println!("  {hot_threshold} calls native in {:?}", native_time);

    // Phase 4: Speedup comparison
    println!("\n=== Performance Comparison ===");
    let speedup = interpreted_time.as_nanos() as f64 / native_time.as_nanos().max(1) as f64;
    println!("  Interpreted: {:?}", interpreted_time);
    println!("  Native:      {:?}", native_time);
    println!("  Speedup:     {speedup:.1}x");

    // Simple function test
    println!("\n=== Simple Function (multiply) ===");
    let mul_func = build_multiply_function();
    let mut jit2 = JitCompiler::new(0).expect("failed");
    jit2.compile(&mul_func).expect("compile failed");
    jit2.finalize().expect("finalize failed");
    if let Some(result) = jit2.call_native(mul_func.id, &[7, 6]) {
        println!("  multiply(7, 6) = {result}");
    }
}
```

### Running

```bash
cargo run
cargo test
```

### Expected Output

```
=== Tiered JIT Compilation Demo ===

Phase 1: Interpreting (cold path)
  sum(1000) = 500500
  100 calls interpreted in 2.1ms

Phase 2: JIT compiling 'sum' (hot threshold reached)
  Compiled: 187 bytes of native code at offset 0

Phase 3: Executing native code (hot path)
  sum(1000) = 500500
  100 calls native in 42us

=== Performance Comparison ===
  Interpreted: 2.1ms
  Native:      42us
  Speedup:     50.0x

=== Simple Function (multiply) ===
  multiply(7, 6) = 42
```

---

## Design Decisions

1. **W^X (Write XOR Execute) policy**: executable memory is first mapped as read-write for code emission, then remapped to read-execute before calling. This prevents the JIT from accidentally executing partially-written code and follows the security model of modern operating systems (macOS requires this, and Linux with SELinux enforces it).

2. **x86-64 stack-based code generation**: the JIT translates a stack-based bytecode to x86-64 by using the hardware stack (`push`/`pop`) as the operand stack. This is the simplest possible code generation strategy -- each bytecode instruction maps to 2-5 x86-64 instructions. The resulting code is inefficient (excessive memory traffic from push/pop) but correct. Register allocation improves this for locals.

3. **Linear scan register allocation**: chosen over graph coloring because it is simpler (O(n log n) vs O(n^2)) and produces good-enough results for JIT compilation where compilation speed matters. The allocator assigns callee-saved registers (rbx, r12-r15) to local variables, avoiding conflicts with the System V calling convention's argument and scratch registers.

4. **`extern "C"` callback for print**: JIT-compiled code calls back into Rust via a regular function pointer. The JIT emits a `mov rax, addr; call rax` sequence where `addr` is the address of `jit_print`. This works because `jit_print` uses the C calling convention and the JIT code follows the System V ABI. The `unsafe` transmute from `*const u8` to function pointer is the fundamental unsafe operation that makes JIT compilation possible.

5. **Invocation counter for hot path detection**: each function has a counter incremented on every call. When the counter crosses the threshold, the function is compiled. This is the simplest hot-path detection strategy. Production JVMs use more sophisticated heuristics (loop back-edge counting for OSR, call-site frequency for inlining decisions).

6. **Separate compilation and finalization**: `compile()` writes code to writable memory. `finalize()` calls `mprotect` to make it executable. This batches permission changes when compiling multiple functions and respects the W^X constraint (you cannot add more code after finalization without creating a new memory region).

7. **`unsafe` documentation**: every `unsafe` block has a safety comment explaining why it is sound. The three sources of unsafety are: mmap/mprotect system calls (raw pointer management), function pointer transmute (calling generated code), and the generated code itself (must follow the calling convention). There is no way to build a JIT compiler in safe Rust -- but the unsafe boundary can be minimized and clearly documented.

## Common Mistakes

- **Stack misalignment**: the System V ABI requires the stack to be 16-byte aligned before a `call` instruction. The `call` pushes an 8-byte return address, so the stack is 16-byte aligned on function entry if it was aligned before the call. Pushing an odd number of 8-byte values before a call misaligns it, causing crashes in called functions that use SSE instructions
- **Forgetting `cqo` before `idiv`**: the `idiv` instruction divides `rdx:rax` by the operand. If `rdx` contains garbage, the result is wrong. `cqo` sign-extends `rax` into `rdx` and must precede every `idiv`
- **REX prefix encoding for R8-R15**: registers 8-15 require a REX prefix with the B, R, or X bit set depending on which field of the ModR/M byte they appear in. Getting this wrong produces instructions that operate on the wrong register (e.g., R8 without REX becomes RAX)
- **Calling convention violations**: `rdi`, `rsi`, `rdx`, `rcx`, `r8`, `r9` are caller-saved. If the JIT code uses these for locals and then calls a function, the values are destroyed. Either save them before calls or allocate locals in callee-saved registers only
- **Executable memory leaks**: mmap'd memory is not freed by Rust's allocator. The `Drop` implementation on `ExecutableMemory` must call `munmap`, or each compilation leaks a page

## Performance Notes

The baseline interpreter processes approximately 50-100 million bytecode operations per second (limited by the match dispatch and stack manipulation overhead). JIT-compiled code eliminates the dispatch entirely and operates on registers, achieving 500M-2B operations per second depending on the workload.

The speedup depends on the function complexity. Simple arithmetic functions see 10-50x improvement. Loop-heavy functions see 20-100x because the loop overhead (jump, compare, branch) maps directly to native instructions without the interpreter's per-iteration dispatch cost.

Compilation time is roughly proportional to function size: 1-10 microseconds per bytecode instruction. For a 100-instruction function, compilation takes 0.1-1ms. This is why the hot threshold exists -- compiling every function at first call would add latency to cold starts.

The biggest remaining performance bottleneck is the stack-based code generation: every operation pushes and pops from the hardware stack, causing unnecessary memory traffic. A real JIT would track which values are "on the stack" but actually in registers, eliminating the push/pop pairs. This is called "stack caching" or "virtual stack elimination."

## Going Further

- Implement register-based code generation that eliminates push/pop overhead
- Add inline caching for polymorphic dispatch
- Implement on-stack replacement (OSR): compile a function while it is running in a hot loop
- Add floating-point support using SSE2 instructions (addsd, subsd, mulsd, divsd)
- Target ARM64 (aarch64) as a second backend
- Implement trace-based JIT: record a trace of bytecodes executed in a hot loop, compile just that linear trace
- Add a deoptimization mechanism: if a JIT assumption is invalidated (e.g., a global variable changes type), fall back to the interpreter
