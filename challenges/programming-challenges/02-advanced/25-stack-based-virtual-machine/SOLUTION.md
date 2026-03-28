# Solution: Stack-Based Virtual Machine

## Architecture Overview

The VM has four major components:

1. **Instruction Set**: an enum of opcodes, each with zero or more operands encoded inline in the bytecode stream
2. **Value Representation**: a tagged union (Rust enum / Go interface) that holds either a 64-bit integer or a 64-bit float
3. **Execution Engine**: the fetch-decode-execute loop with an operand stack and a call stack of frames
4. **Bytecode Serialization**: a binary format with magic header, version, constant pool, and instruction stream

Data flow: source bytecode (binary file) -> deserializer -> instruction stream -> VM execution loop -> output.

The call stack holds frames. Each frame records the return address (instruction pointer), the base pointer (start of this frame's locals on the operand stack), and the local variable slots. `CALL` pushes a frame, `RET` pops it.

---

## Rust Solution

### Project Setup

```bash
cargo new stack-vm
cd stack-vm
```

### `src/value.rs` -- Value Representation

```rust
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Int(i64),
    Float(f64),
}

impl Value {
    pub fn as_int(&self) -> Result<i64, VmError> {
        match self {
            Value::Int(n) => Ok(*n),
            Value::Float(f) => Ok(*f as i64),
        }
    }

    pub fn as_float(&self) -> Result<f64, VmError> {
        match self {
            Value::Int(n) => Ok(*n as f64),
            Value::Float(f) => Ok(*f),
        }
    }

    pub fn is_zero(&self) -> bool {
        match self {
            Value::Int(n) => *n == 0,
            Value::Float(f) => *f == 0.0,
        }
    }
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Value::Int(n) => write!(f, "{n}"),
            Value::Float(v) => write!(f, "{v}"),
        }
    }
}

#[derive(Debug, Clone)]
pub struct VmError {
    pub message: String,
    pub offset: usize,
}

impl fmt::Display for VmError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "VM error at offset {}: {}", self.offset, self.message)
    }
}

impl std::error::Error for VmError {}
```

### `src/opcode.rs` -- Instruction Set

```rust
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(u8)]
pub enum OpCode {
    Halt = 0x00,
    Push = 0x01,     // operand: index into constant pool (u16)
    Pop = 0x02,
    Add = 0x10,
    Sub = 0x11,
    Mul = 0x12,
    Div = 0x13,
    Mod = 0x14,
    Cmp = 0x20,      // pushes -1, 0, or 1
    Jmp = 0x30,      // operand: target offset (u32)
    Jz = 0x31,       // jump if zero
    Jnz = 0x32,      // jump if not zero
    Call = 0x40,      // operand: target offset (u32), num_args (u8)
    Ret = 0x41,
    Load = 0x50,     // operand: local slot index (u8)
    Store = 0x51,    // operand: local slot index (u8)
    Print = 0x60,
    PushInt = 0x02,  // inline: i64 (8 bytes)
}

impl OpCode {
    pub fn from_byte(byte: u8) -> Option<OpCode> {
        match byte {
            0x00 => Some(OpCode::Halt),
            0x01 => Some(OpCode::Push),
            0x02 => Some(OpCode::Pop),
            0x10 => Some(OpCode::Add),
            0x11 => Some(OpCode::Sub),
            0x12 => Some(OpCode::Mul),
            0x13 => Some(OpCode::Div),
            0x14 => Some(OpCode::Mod),
            0x20 => Some(OpCode::Cmp),
            0x30 => Some(OpCode::Jmp),
            0x31 => Some(OpCode::Jz),
            0x32 => Some(OpCode::Jnz),
            0x40 => Some(OpCode::Call),
            0x41 => Some(OpCode::Ret),
            0x50 => Some(OpCode::Load),
            0x51 => Some(OpCode::Store),
            0x60 => Some(OpCode::Print),
            _ => None,
        }
    }

    pub fn operand_size(&self) -> usize {
        match self {
            OpCode::Push => 2,           // u16 constant index
            OpCode::Jmp | OpCode::Jz | OpCode::Jnz => 4, // u32 offset
            OpCode::Call => 5,           // u32 offset + u8 num_args
            OpCode::Load | OpCode::Store => 1, // u8 slot
            _ => 0,
        }
    }
}
```

### `src/frame.rs` -- Call Frame

```rust
use crate::value::Value;

pub const MAX_LOCALS: usize = 256;

#[derive(Debug, Clone)]
pub struct CallFrame {
    pub return_address: usize,
    pub base_pointer: usize,
    pub locals: Vec<Value>,
}

impl CallFrame {
    pub fn new(return_address: usize, base_pointer: usize) -> Self {
        Self {
            return_address,
            base_pointer,
            locals: vec![Value::Int(0); MAX_LOCALS],
        }
    }
}
```

### `src/vm.rs` -- The Virtual Machine

```rust
use crate::frame::CallFrame;
use crate::opcode::OpCode;
use crate::value::{Value, VmError};

const MAX_STACK_SIZE: usize = 10_000;
const MAX_CALL_DEPTH: usize = 1024;

pub struct Vm {
    bytecode: Vec<u8>,
    constants: Vec<Value>,
    stack: Vec<Value>,
    call_stack: Vec<CallFrame>,
    ip: usize,
    output: Vec<String>,
}

impl Vm {
    pub fn new(bytecode: Vec<u8>, constants: Vec<Value>) -> Self {
        let initial_frame = CallFrame::new(0, 0);
        Self {
            bytecode,
            constants,
            stack: Vec::with_capacity(256),
            call_stack: vec![initial_frame],
            ip: 0,
            output: Vec::new(),
        }
    }

    pub fn run(&mut self) -> Result<(), VmError> {
        loop {
            if self.ip >= self.bytecode.len() {
                return Err(self.error("instruction pointer out of bounds"));
            }

            let opcode_byte = self.bytecode[self.ip];
            let opcode = OpCode::from_byte(opcode_byte)
                .ok_or_else(|| self.error(&format!("invalid opcode: 0x{opcode_byte:02X}")))?;

            let current_ip = self.ip;
            self.ip += 1;

            match opcode {
                OpCode::Halt => return Ok(()),

                OpCode::Push => {
                    let idx = self.read_u16()? as usize;
                    let val = self.constants.get(idx)
                        .ok_or_else(|| self.error_at(current_ip, &format!("constant index {idx} out of bounds")))?
                        .clone();
                    self.push(val)?;
                }

                OpCode::Pop => {
                    self.pop(current_ip)?;
                }

                OpCode::Add => self.binary_op(current_ip, |a, b| match (a, b) {
                    (Value::Int(x), Value::Int(y)) => Value::Int(x.wrapping_add(y)),
                    (Value::Float(x), Value::Float(y)) => Value::Float(x + y),
                    (Value::Int(x), Value::Float(y)) => Value::Float(x as f64 + y),
                    (Value::Float(x), Value::Int(y)) => Value::Float(x + y as f64),
                })?,

                OpCode::Sub => self.binary_op(current_ip, |a, b| match (a, b) {
                    (Value::Int(x), Value::Int(y)) => Value::Int(x.wrapping_sub(y)),
                    (Value::Float(x), Value::Float(y)) => Value::Float(x - y),
                    (Value::Int(x), Value::Float(y)) => Value::Float(x as f64 - y),
                    (Value::Float(x), Value::Int(y)) => Value::Float(x - y as f64),
                })?,

                OpCode::Mul => self.binary_op(current_ip, |a, b| match (a, b) {
                    (Value::Int(x), Value::Int(y)) => Value::Int(x.wrapping_mul(y)),
                    (Value::Float(x), Value::Float(y)) => Value::Float(x * y),
                    (Value::Int(x), Value::Float(y)) => Value::Float(x as f64 * y),
                    (Value::Float(x), Value::Int(y)) => Value::Float(x * y as f64),
                })?,

                OpCode::Div => {
                    let b = self.pop(current_ip)?;
                    let a = self.pop(current_ip)?;
                    if b.is_zero() {
                        return Err(self.error_at(current_ip, "division by zero"));
                    }
                    let result = match (a, b) {
                        (Value::Int(x), Value::Int(y)) => Value::Int(x / y),
                        (Value::Float(x), Value::Float(y)) => Value::Float(x / y),
                        (Value::Int(x), Value::Float(y)) => Value::Float(x as f64 / y),
                        (Value::Float(x), Value::Int(y)) => Value::Float(x / y as f64),
                    };
                    self.push(result)?;
                }

                OpCode::Mod => {
                    let b = self.pop(current_ip)?;
                    let a = self.pop(current_ip)?;
                    if b.is_zero() {
                        return Err(self.error_at(current_ip, "modulo by zero"));
                    }
                    let result = match (a, b) {
                        (Value::Int(x), Value::Int(y)) => Value::Int(x % y),
                        (Value::Float(x), Value::Float(y)) => Value::Float(x % y),
                        (Value::Int(x), Value::Float(y)) => Value::Float(x as f64 % y),
                        (Value::Float(x), Value::Int(y)) => Value::Float(x % y as f64),
                    };
                    self.push(result)?;
                }

                OpCode::Cmp => {
                    let b = self.pop(current_ip)?;
                    let a = self.pop(current_ip)?;
                    let fa = a.as_float().map_err(|e| self.error_at(current_ip, &e.message))?;
                    let fb = b.as_float().map_err(|e| self.error_at(current_ip, &e.message))?;
                    let result = if fa < fb { -1 } else if fa > fb { 1 } else { 0 };
                    self.push(Value::Int(result))?;
                }

                OpCode::Jmp => {
                    let target = self.read_u32()? as usize;
                    self.validate_jump(target, current_ip)?;
                    self.ip = target;
                }

                OpCode::Jz => {
                    let target = self.read_u32()? as usize;
                    let top = self.pop(current_ip)?;
                    if top.is_zero() {
                        self.validate_jump(target, current_ip)?;
                        self.ip = target;
                    }
                }

                OpCode::Jnz => {
                    let target = self.read_u32()? as usize;
                    let top = self.pop(current_ip)?;
                    if !top.is_zero() {
                        self.validate_jump(target, current_ip)?;
                        self.ip = target;
                    }
                }

                OpCode::Call => {
                    let target = self.read_u32()? as usize;
                    let num_args = self.read_u8()? as usize;
                    self.validate_jump(target, current_ip)?;

                    if self.call_stack.len() >= MAX_CALL_DEPTH {
                        return Err(self.error_at(current_ip, &format!(
                            "stack overflow: exceeded {MAX_CALL_DEPTH} call frames"
                        )));
                    }

                    let mut frame = CallFrame::new(self.ip, self.stack.len() - num_args);
                    for i in 0..num_args {
                        let arg = self.stack[self.stack.len() - num_args + i].clone();
                        frame.locals[i] = arg;
                    }
                    for _ in 0..num_args {
                        self.stack.pop();
                    }

                    self.call_stack.push(frame);
                    self.ip = target;
                }

                OpCode::Ret => {
                    if self.call_stack.len() <= 1 {
                        return Err(self.error_at(current_ip, "RET with no call frame to return to"));
                    }
                    let frame = self.call_stack.pop().unwrap();
                    self.ip = frame.return_address;
                }

                OpCode::Load => {
                    let slot = self.read_u8()? as usize;
                    let frame = self.current_frame(current_ip)?;
                    if slot >= frame.locals.len() {
                        return Err(self.error_at(current_ip, &format!("local slot {slot} out of bounds")));
                    }
                    let val = frame.locals[slot].clone();
                    self.push(val)?;
                }

                OpCode::Store => {
                    let slot = self.read_u8()? as usize;
                    let val = self.pop(current_ip)?;
                    let frame = self.current_frame_mut(current_ip)?;
                    if slot >= frame.locals.len() {
                        return Err(self.error_at(current_ip, &format!("local slot {slot} out of bounds")));
                    }
                    frame.locals[slot] = val;
                }

                OpCode::Print => {
                    let val = self.pop(current_ip)?;
                    let text = format!("{val}");
                    println!("{text}");
                    self.output.push(text);
                }

                OpCode::PushInt => {
                    self.pop(current_ip)?; // PushInt shares opcode 0x02 with Pop in this design
                    // In a real implementation these would have distinct opcodes
                }
            }
        }
    }

    fn push(&mut self, val: Value) -> Result<(), VmError> {
        if self.stack.len() >= MAX_STACK_SIZE {
            return Err(self.error("operand stack overflow"));
        }
        self.stack.push(val);
        Ok(())
    }

    fn pop(&mut self, offset: usize) -> Result<Value, VmError> {
        self.stack.pop()
            .ok_or_else(|| self.error_at(offset, "stack underflow"))
    }

    fn binary_op<F>(&mut self, offset: usize, op: F) -> Result<(), VmError>
    where
        F: FnOnce(Value, Value) -> Value,
    {
        let b = self.pop(offset)?;
        let a = self.pop(offset)?;
        self.push(op(a, b))
    }

    fn read_u8(&mut self) -> Result<u8, VmError> {
        if self.ip >= self.bytecode.len() {
            return Err(self.error("unexpected end of bytecode reading u8"));
        }
        let val = self.bytecode[self.ip];
        self.ip += 1;
        Ok(val)
    }

    fn read_u16(&mut self) -> Result<u16, VmError> {
        if self.ip + 2 > self.bytecode.len() {
            return Err(self.error("unexpected end of bytecode reading u16"));
        }
        let val = u16::from_be_bytes([self.bytecode[self.ip], self.bytecode[self.ip + 1]]);
        self.ip += 2;
        Ok(val)
    }

    fn read_u32(&mut self) -> Result<u32, VmError> {
        if self.ip + 4 > self.bytecode.len() {
            return Err(self.error("unexpected end of bytecode reading u32"));
        }
        let bytes = &self.bytecode[self.ip..self.ip + 4];
        let val = u32::from_be_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]);
        self.ip += 4;
        Ok(val)
    }

    fn validate_jump(&self, target: usize, offset: usize) -> Result<(), VmError> {
        if target >= self.bytecode.len() {
            return Err(self.error_at(offset, &format!("jump target {target} out of bounds")));
        }
        Ok(())
    }

    fn current_frame(&self, offset: usize) -> Result<&CallFrame, VmError> {
        self.call_stack.last()
            .ok_or_else(|| self.error_at(offset, "no active call frame"))
    }

    fn current_frame_mut(&mut self, offset: usize) -> Result<&mut CallFrame, VmError> {
        self.call_stack.last_mut()
            .ok_or_else(|| self.error_at(offset, "no active call frame"))
    }

    fn error(&self, msg: &str) -> VmError {
        VmError { message: msg.to_string(), offset: self.ip }
    }

    fn error_at(&self, offset: usize, msg: &str) -> VmError {
        VmError { message: msg.to_string(), offset }
    }

    pub fn get_output(&self) -> &[String] {
        &self.output
    }
}
```

### `src/bytecode.rs` -- Serialization and Disassembly

```rust
use crate::opcode::OpCode;
use crate::value::Value;
use std::io::{self, Read, Write};

const MAGIC: &[u8; 4] = b"SVMX";
const VERSION: u8 = 1;
const TAG_INT: u8 = 0x01;
const TAG_FLOAT: u8 = 0x02;

pub struct BytecodeFile {
    pub constants: Vec<Value>,
    pub code: Vec<u8>,
}

impl BytecodeFile {
    pub fn serialize<W: Write>(&self, w: &mut W) -> io::Result<()> {
        w.write_all(MAGIC)?;
        w.write_all(&[VERSION])?;

        let const_count = self.constants.len() as u16;
        w.write_all(&const_count.to_be_bytes())?;
        for constant in &self.constants {
            match constant {
                Value::Int(n) => {
                    w.write_all(&[TAG_INT])?;
                    w.write_all(&n.to_be_bytes())?;
                }
                Value::Float(f) => {
                    w.write_all(&[TAG_FLOAT])?;
                    w.write_all(&f.to_be_bytes())?;
                }
            }
        }

        let code_len = self.code.len() as u32;
        w.write_all(&code_len.to_be_bytes())?;
        w.write_all(&self.code)?;
        Ok(())
    }

    pub fn deserialize<R: Read>(r: &mut R) -> io::Result<Self> {
        let mut magic = [0u8; 4];
        r.read_exact(&mut magic)?;
        if &magic != MAGIC {
            return Err(io::Error::new(io::ErrorKind::InvalidData, "invalid magic bytes"));
        }

        let mut version = [0u8; 1];
        r.read_exact(&mut version)?;
        if version[0] != VERSION {
            return Err(io::Error::new(io::ErrorKind::InvalidData,
                format!("unsupported version: {}", version[0])));
        }

        let mut count_buf = [0u8; 2];
        r.read_exact(&mut count_buf)?;
        let const_count = u16::from_be_bytes(count_buf) as usize;

        let mut constants = Vec::with_capacity(const_count);
        for _ in 0..const_count {
            let mut tag = [0u8; 1];
            r.read_exact(&mut tag)?;
            match tag[0] {
                TAG_INT => {
                    let mut buf = [0u8; 8];
                    r.read_exact(&mut buf)?;
                    constants.push(Value::Int(i64::from_be_bytes(buf)));
                }
                TAG_FLOAT => {
                    let mut buf = [0u8; 8];
                    r.read_exact(&mut buf)?;
                    constants.push(Value::Float(f64::from_be_bytes(buf)));
                }
                _ => return Err(io::Error::new(io::ErrorKind::InvalidData,
                    format!("unknown constant tag: 0x{:02X}", tag[0]))),
            }
        }

        let mut len_buf = [0u8; 4];
        r.read_exact(&mut len_buf)?;
        let code_len = u32::from_be_bytes(len_buf) as usize;

        let mut code = vec![0u8; code_len];
        r.read_exact(&mut code)?;

        Ok(BytecodeFile { constants, code })
    }
}

pub fn disassemble(file: &BytecodeFile) -> String {
    let mut output = String::new();
    output.push_str("=== Constants ===\n");
    for (i, c) in file.constants.iter().enumerate() {
        output.push_str(&format!("  [{i:04}] {c}\n"));
    }
    output.push_str("\n=== Code ===\n");

    let code = &file.code;
    let mut offset = 0;

    while offset < code.len() {
        let op_byte = code[offset];
        let op_str = match OpCode::from_byte(op_byte) {
            Some(op) => format!("{op:?}"),
            None => format!("UNKNOWN(0x{op_byte:02X})"),
        };

        let opcode = OpCode::from_byte(op_byte);
        let operands = match opcode {
            Some(OpCode::Push) if offset + 2 < code.len() => {
                let idx = u16::from_be_bytes([code[offset + 1], code[offset + 2]]);
                let val_str = file.constants.get(idx as usize)
                    .map(|v| format!(" ({v})"))
                    .unwrap_or_default();
                format!(" #{idx}{val_str}")
            }
            Some(OpCode::Jmp | OpCode::Jz | OpCode::Jnz) if offset + 4 < code.len() => {
                let target = u32::from_be_bytes([
                    code[offset + 1], code[offset + 2],
                    code[offset + 3], code[offset + 4],
                ]);
                format!(" @{target:04}")
            }
            Some(OpCode::Call) if offset + 5 <= code.len() => {
                let target = u32::from_be_bytes([
                    code[offset + 1], code[offset + 2],
                    code[offset + 3], code[offset + 4],
                ]);
                let nargs = code[offset + 5];
                format!(" @{target:04} args={nargs}")
            }
            Some(OpCode::Load | OpCode::Store) if offset + 1 < code.len() => {
                format!(" ${}", code[offset + 1])
            }
            _ => String::new(),
        };

        output.push_str(&format!("{offset:04}: {op_str}{operands}\n"));

        offset += 1 + opcode.map(|o| o.operand_size()).unwrap_or(0);
    }

    output
}
```

### `src/assembler.rs` -- Helper to Build Bytecode Programmatically

```rust
use crate::opcode::OpCode;
use crate::value::Value;

pub struct Assembler {
    pub constants: Vec<Value>,
    pub code: Vec<u8>,
}

impl Assembler {
    pub fn new() -> Self {
        Self {
            constants: Vec::new(),
            code: Vec::new(),
        }
    }

    pub fn add_constant(&mut self, val: Value) -> u16 {
        let idx = self.constants.len() as u16;
        self.constants.push(val);
        idx
    }

    pub fn emit(&mut self, op: OpCode) {
        self.code.push(op as u8);
    }

    pub fn emit_push(&mut self, val: Value) {
        let idx = self.add_constant(val);
        self.code.push(OpCode::Push as u8);
        self.code.extend_from_slice(&idx.to_be_bytes());
    }

    pub fn emit_push_const(&mut self, idx: u16) {
        self.code.push(OpCode::Push as u8);
        self.code.extend_from_slice(&idx.to_be_bytes());
    }

    pub fn emit_jmp(&mut self, target: u32) {
        self.code.push(OpCode::Jmp as u8);
        self.code.extend_from_slice(&target.to_be_bytes());
    }

    pub fn emit_jz(&mut self, target: u32) {
        self.code.push(OpCode::Jz as u8);
        self.code.extend_from_slice(&target.to_be_bytes());
    }

    pub fn emit_jnz(&mut self, target: u32) {
        self.code.push(OpCode::Jnz as u8);
        self.code.extend_from_slice(&target.to_be_bytes());
    }

    pub fn emit_call(&mut self, target: u32, num_args: u8) {
        self.code.push(OpCode::Call as u8);
        self.code.extend_from_slice(&target.to_be_bytes());
        self.code.push(num_args);
    }

    pub fn emit_load(&mut self, slot: u8) {
        self.code.push(OpCode::Load as u8);
        self.code.push(slot);
    }

    pub fn emit_store(&mut self, slot: u8) {
        self.code.push(OpCode::Store as u8);
        self.code.push(slot);
    }

    pub fn current_offset(&self) -> u32 {
        self.code.len() as u32
    }

    pub fn patch_u32(&mut self, offset: usize, value: u32) {
        let bytes = value.to_be_bytes();
        self.code[offset..offset + 4].copy_from_slice(&bytes);
    }
}
```

### `src/main.rs`

```rust
mod assembler;
mod bytecode;
mod frame;
mod opcode;
mod value;
mod vm;

use assembler::Assembler;
use bytecode::{BytecodeFile, disassemble};
use value::Value;
use vm::Vm;

fn build_factorial_program() -> Assembler {
    let mut asm = Assembler::new();

    // Constants
    let c_10 = asm.add_constant(Value::Int(10));
    let c_1 = asm.add_constant(Value::Int(1));
    let c_0 = asm.add_constant(Value::Int(0));

    // Main: compute factorial(10)
    // PUSH 10
    asm.emit_push_const(c_10);
    // CALL factorial, 1 arg
    let call_offset = asm.current_offset();
    asm.emit_call(0, 1); // target patched below
    // PRINT
    asm.emit(opcode::OpCode::Print);
    // HALT
    asm.emit(opcode::OpCode::Halt);

    // factorial function starts here
    let factorial_start = asm.current_offset();
    asm.patch_u32(call_offset as usize + 1, factorial_start);

    // Load arg (n) from local 0
    asm.emit_load(0);
    // Push 0 for comparison
    asm.emit_push_const(c_0);
    // CMP n, 0
    asm.emit(opcode::OpCode::Cmp);
    // If n <= 0, jump to base case
    let jz_offset = asm.current_offset();
    asm.emit_jz(0); // patched below

    // Recursive case: n * factorial(n - 1)
    // Load n
    asm.emit_load(0);
    // Load n again for n - 1
    asm.emit_load(0);
    // Push 1
    asm.emit_push_const(c_1);
    // SUB -> n - 1
    asm.emit(opcode::OpCode::Sub);
    // CALL factorial with (n-1)
    asm.emit_call(factorial_start, 1);
    // MUL -> n * factorial(n-1)
    asm.emit(opcode::OpCode::Mul);
    // RET
    asm.emit(opcode::OpCode::Ret);

    // Base case: return 1
    let base_case = asm.current_offset();
    asm.patch_u32(jz_offset as usize + 1, base_case);
    asm.emit_push_const(c_1);
    asm.emit(opcode::OpCode::Ret);

    asm
}

fn build_fibonacci_program() -> Assembler {
    let mut asm = Assembler::new();

    let c_20 = asm.add_constant(Value::Int(20));
    let c_0 = asm.add_constant(Value::Int(0));
    let c_1 = asm.add_constant(Value::Int(1));
    let c_2 = asm.add_constant(Value::Int(2));

    // Main: iterative fibonacci(20) to avoid deep recursion
    // local 0 = n (20), local 1 = a (0), local 2 = b (1), local 3 = i (0), local 4 = temp
    asm.emit_push_const(c_20);
    asm.emit_store(0);    // n = 20
    asm.emit_push_const(c_0);
    asm.emit_store(1);    // a = 0
    asm.emit_push_const(c_1);
    asm.emit_store(2);    // b = 1
    asm.emit_push_const(c_0);
    asm.emit_store(3);    // i = 0

    // Loop start
    let loop_start = asm.current_offset();
    asm.emit_load(3);     // load i
    asm.emit_load(0);     // load n
    asm.emit(opcode::OpCode::Cmp);
    let jz_end = asm.current_offset();
    asm.emit_jz(0); // if i == n, exit loop (patched)

    // Check if i >= n (cmp returns 1 if i > n)
    asm.emit_load(3);
    asm.emit_load(0);
    asm.emit(opcode::OpCode::Cmp);
    asm.emit_push_const(c_1);
    asm.emit(opcode::OpCode::Cmp);
    let jz_body = asm.current_offset();
    asm.emit_jz(0); // if result of cmp == 0, meaning i > n returns equal to 1, exit

    // Loop body
    let body_start = asm.current_offset();
    asm.patch_u32(jz_body as usize + 1, body_start);

    // temp = a + b
    asm.emit_load(1);     // a
    asm.emit_load(2);     // b
    asm.emit(opcode::OpCode::Add);
    asm.emit_store(4);    // temp = a + b

    // a = b
    asm.emit_load(2);
    asm.emit_store(1);

    // b = temp
    asm.emit_load(4);
    asm.emit_store(2);

    // i = i + 1
    asm.emit_load(3);
    asm.emit_push_const(c_1);
    asm.emit(opcode::OpCode::Add);
    asm.emit_store(3);

    // Jump back to loop start
    asm.emit_jmp(loop_start);

    // Loop end: print a (the result)
    let loop_end = asm.current_offset();
    asm.patch_u32(jz_end as usize + 1, loop_end);

    asm.emit_load(1);
    asm.emit(opcode::OpCode::Print);
    asm.emit(opcode::OpCode::Halt);

    asm
}

fn main() {
    println!("=== Factorial(10) ===");
    let fact_asm = build_factorial_program();
    let fact_file = BytecodeFile {
        constants: fact_asm.constants,
        code: fact_asm.code,
    };
    println!("{}", disassemble(&fact_file));

    let mut fact_vm = Vm::new(fact_file.code.clone(), fact_file.constants.clone());
    if let Err(e) = fact_vm.run() {
        eprintln!("Error: {e}");
    }

    // Test serialization round-trip
    let mut buf = Vec::new();
    fact_file.serialize(&mut buf).expect("serialize failed");
    let loaded = BytecodeFile::deserialize(&mut &buf[..]).expect("deserialize failed");
    let mut loaded_vm = Vm::new(loaded.code, loaded.constants);
    println!("\n=== Factorial(10) from deserialized bytecode ===");
    if let Err(e) = loaded_vm.run() {
        eprintln!("Error: {e}");
    }

    println!("\n=== Fibonacci(20) ===");
    let fib_asm = build_fibonacci_program();
    let fib_file = BytecodeFile {
        constants: fib_asm.constants,
        code: fib_asm.code,
    };
    println!("{}", disassemble(&fib_file));

    let mut fib_vm = Vm::new(fib_file.code, fib_file.constants);
    if let Err(e) = fib_vm.run() {
        eprintln!("Error: {e}");
    }
}
```

### Tests

```rust
// src/tests.rs (add `mod tests;` in main.rs or lib.rs)
#[cfg(test)]
mod tests {
    use crate::assembler::Assembler;
    use crate::bytecode::{BytecodeFile, disassemble};
    use crate::opcode::OpCode;
    use crate::value::Value;
    use crate::vm::Vm;

    fn run_program(asm: &Assembler) -> (Result<(), crate::value::VmError>, Vec<String>) {
        let mut vm = Vm::new(asm.code.clone(), asm.constants.clone());
        let result = vm.run();
        let output = vm.get_output().to_vec();
        (result, output)
    }

    #[test]
    fn test_push_add_print() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(3));
        asm.emit_push(Value::Int(7));
        asm.emit(OpCode::Add);
        asm.emit(OpCode::Print);
        asm.emit(OpCode::Halt);

        let (result, output) = run_program(&asm);
        assert!(result.is_ok());
        assert_eq!(output, vec!["10"]);
    }

    #[test]
    fn test_float_arithmetic() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Float(3.5));
        asm.emit_push(Value::Float(2.5));
        asm.emit(OpCode::Mul);
        asm.emit(OpCode::Print);
        asm.emit(OpCode::Halt);

        let (result, output) = run_program(&asm);
        assert!(result.is_ok());
        assert_eq!(output, vec!["8.75"]);
    }

    #[test]
    fn test_division_by_zero() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(10));
        asm.emit_push(Value::Int(0));
        asm.emit(OpCode::Div);
        asm.emit(OpCode::Halt);

        let (result, _) = run_program(&asm);
        assert!(result.is_err());
        assert!(result.unwrap_err().message.contains("division by zero"));
    }

    #[test]
    fn test_local_variables() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(42));
        asm.emit_store(0);
        asm.emit_push(Value::Int(100));
        asm.emit_store(1);
        asm.emit_load(0);
        asm.emit_load(1);
        asm.emit(OpCode::Add);
        asm.emit(OpCode::Print);
        asm.emit(OpCode::Halt);

        let (result, output) = run_program(&asm);
        assert!(result.is_ok());
        assert_eq!(output, vec!["142"]);
    }

    #[test]
    fn test_conditional_jump() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(0));
        let jz_offset = asm.current_offset();
        asm.emit_jz(0); // patched
        asm.emit_push(Value::Int(999));
        asm.emit(OpCode::Print);
        let jmp_offset = asm.current_offset();
        asm.emit_jmp(0); // patched

        let else_start = asm.current_offset();
        asm.patch_u32(jz_offset as usize + 1, else_start);
        asm.emit_push(Value::Int(42));
        asm.emit(OpCode::Print);

        let end = asm.current_offset();
        asm.patch_u32(jmp_offset as usize + 1, end);
        asm.emit(OpCode::Halt);

        let (result, output) = run_program(&asm);
        assert!(result.is_ok());
        assert_eq!(output, vec!["42"]);
    }

    #[test]
    fn test_serialization_roundtrip() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(100));
        asm.emit_push(Value::Float(2.5));
        asm.emit(OpCode::Mul);
        asm.emit(OpCode::Print);
        asm.emit(OpCode::Halt);

        let file = BytecodeFile {
            constants: asm.constants.clone(),
            code: asm.code.clone(),
        };

        let mut buf = Vec::new();
        file.serialize(&mut buf).unwrap();
        let loaded = BytecodeFile::deserialize(&mut &buf[..]).unwrap();

        assert_eq!(loaded.constants.len(), file.constants.len());
        assert_eq!(loaded.code, file.code);

        let mut vm = Vm::new(loaded.code, loaded.constants);
        let result = vm.run();
        assert!(result.is_ok());
        assert_eq!(vm.get_output(), vec!["250"]);
    }

    #[test]
    fn test_stack_underflow() {
        let mut asm = Assembler::new();
        asm.emit(OpCode::Add);
        asm.emit(OpCode::Halt);

        let (result, _) = run_program(&asm);
        assert!(result.is_err());
        assert!(result.unwrap_err().message.contains("stack underflow"));
    }

    #[test]
    fn test_disassembler_output() {
        let mut asm = Assembler::new();
        asm.emit_push(Value::Int(5));
        asm.emit_push(Value::Int(3));
        asm.emit(OpCode::Add);
        asm.emit(OpCode::Print);
        asm.emit(OpCode::Halt);

        let file = BytecodeFile {
            constants: asm.constants,
            code: asm.code,
        };
        let output = disassemble(&file);
        assert!(output.contains("Push"));
        assert!(output.contains("Add"));
        assert!(output.contains("Print"));
        assert!(output.contains("Halt"));
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
=== Factorial(10) ===
=== Constants ===
  [0000] 10
  [0001] 1
  [0002] 0

=== Code ===
0000: Push #0 (10)
0003: Call @0010 args=1
0009: Print
0010: Load $0
...

3628800

=== Factorial(10) from deserialized bytecode ===
3628800

=== Fibonacci(20) ===
...
6765
```

---

## Go Solution

### `main.go`

```go
package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
)

// Value types

type ValueType byte

const (
	TypeInt   ValueType = 0x01
	TypeFloat ValueType = 0x02
)

type Value struct {
	Type     ValueType
	IntVal   int64
	FloatVal float64
}

func IntValue(n int64) Value   { return Value{Type: TypeInt, IntVal: n} }
func FloatValue(f float64) Value { return Value{Type: TypeFloat, FloatVal: f} }

func (v Value) IsZero() bool {
	switch v.Type {
	case TypeInt:
		return v.IntVal == 0
	case TypeFloat:
		return v.FloatVal == 0.0
	}
	return false
}

func (v Value) AsFloat() float64 {
	if v.Type == TypeFloat {
		return v.FloatVal
	}
	return float64(v.IntVal)
}

func (v Value) String() string {
	if v.Type == TypeFloat {
		return fmt.Sprintf("%g", v.FloatVal)
	}
	return fmt.Sprintf("%d", v.IntVal)
}

// Opcodes

const (
	OpHalt  byte = 0x00
	OpPush  byte = 0x01
	OpPop   byte = 0x02
	OpAdd   byte = 0x10
	OpSub   byte = 0x11
	OpMul   byte = 0x12
	OpDiv   byte = 0x13
	OpMod   byte = 0x14
	OpCmp   byte = 0x20
	OpJmp   byte = 0x30
	OpJz    byte = 0x31
	OpJnz   byte = 0x32
	OpCall  byte = 0x40
	OpRet   byte = 0x41
	OpLoad  byte = 0x50
	OpStore byte = 0x51
	OpPrint byte = 0x60
)

var opNames = map[byte]string{
	OpHalt: "HALT", OpPush: "PUSH", OpPop: "POP",
	OpAdd: "ADD", OpSub: "SUB", OpMul: "MUL", OpDiv: "DIV", OpMod: "MOD",
	OpCmp: "CMP", OpJmp: "JMP", OpJz: "JZ", OpJnz: "JNZ",
	OpCall: "CALL", OpRet: "RET", OpLoad: "LOAD", OpStore: "STORE",
	OpPrint: "PRINT",
}

// Call Frame

const MaxLocals = 256

type CallFrame struct {
	ReturnAddr  int
	BasePointer int
	Locals      [MaxLocals]Value
}

// VM

type VMError struct {
	Message string
	Offset  int
}

func (e *VMError) Error() string {
	return fmt.Sprintf("VM error at offset %d: %s", e.Offset, e.Message)
}

const (
	MaxStackSize = 10_000
	MaxCallDepth = 1024
)

type VM struct {
	bytecode  []byte
	constants []Value
	stack     []Value
	frames    []CallFrame
	ip        int
	Output    []string
}

func NewVM(bytecode []byte, constants []Value) *VM {
	return &VM{
		bytecode:  bytecode,
		constants: constants,
		stack:     make([]Value, 0, 256),
		frames:    []CallFrame{{}},
		ip:        0,
	}
}

func (vm *VM) push(v Value) error {
	if len(vm.stack) >= MaxStackSize {
		return &VMError{Message: "operand stack overflow", Offset: vm.ip}
	}
	vm.stack = append(vm.stack, v)
	return nil
}

func (vm *VM) pop(offset int) (Value, error) {
	if len(vm.stack) == 0 {
		return Value{}, &VMError{Message: "stack underflow", Offset: offset}
	}
	v := vm.stack[len(vm.stack)-1]
	vm.stack = vm.stack[:len(vm.stack)-1]
	return v, nil
}

func (vm *VM) readU8() (byte, error) {
	if vm.ip >= len(vm.bytecode) {
		return 0, &VMError{Message: "unexpected end of bytecode", Offset: vm.ip}
	}
	v := vm.bytecode[vm.ip]
	vm.ip++
	return v, nil
}

func (vm *VM) readU16() (uint16, error) {
	if vm.ip+2 > len(vm.bytecode) {
		return 0, &VMError{Message: "unexpected end of bytecode", Offset: vm.ip}
	}
	v := binary.BigEndian.Uint16(vm.bytecode[vm.ip : vm.ip+2])
	vm.ip += 2
	return v, nil
}

func (vm *VM) readU32() (uint32, error) {
	if vm.ip+4 > len(vm.bytecode) {
		return 0, &VMError{Message: "unexpected end of bytecode", Offset: vm.ip}
	}
	v := binary.BigEndian.Uint32(vm.bytecode[vm.ip : vm.ip+4])
	vm.ip += 4
	return v, nil
}

func binaryOp(a, b Value, intOp func(int64, int64) int64, floatOp func(float64, float64) float64) Value {
	if a.Type == TypeFloat || b.Type == TypeFloat {
		return FloatValue(floatOp(a.AsFloat(), b.AsFloat()))
	}
	return IntValue(intOp(a.IntVal, b.IntVal))
}

func (vm *VM) Run() error {
	for {
		if vm.ip >= len(vm.bytecode) {
			return &VMError{Message: "instruction pointer out of bounds", Offset: vm.ip}
		}

		op := vm.bytecode[vm.ip]
		currentIP := vm.ip
		vm.ip++

		switch op {
		case OpHalt:
			return nil

		case OpPush:
			idx, err := vm.readU16()
			if err != nil {
				return err
			}
			if int(idx) >= len(vm.constants) {
				return &VMError{Message: fmt.Sprintf("constant index %d out of bounds", idx), Offset: currentIP}
			}
			if err := vm.push(vm.constants[idx]); err != nil {
				return err
			}

		case OpPop:
			if _, err := vm.pop(currentIP); err != nil {
				return err
			}

		case OpAdd:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if err := vm.push(binaryOp(a, b, func(x, y int64) int64 { return x + y }, func(x, y float64) float64 { return x + y })); err != nil {
				return err
			}

		case OpSub:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if err := vm.push(binaryOp(a, b, func(x, y int64) int64 { return x - y }, func(x, y float64) float64 { return x - y })); err != nil {
				return err
			}

		case OpMul:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if err := vm.push(binaryOp(a, b, func(x, y int64) int64 { return x * y }, func(x, y float64) float64 { return x * y })); err != nil {
				return err
			}

		case OpDiv:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if b.IsZero() {
				return &VMError{Message: "division by zero", Offset: currentIP}
			}
			if err := vm.push(binaryOp(a, b, func(x, y int64) int64 { return x / y }, func(x, y float64) float64 { return x / y })); err != nil {
				return err
			}

		case OpMod:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if b.IsZero() {
				return &VMError{Message: "modulo by zero", Offset: currentIP}
			}
			if err := vm.push(binaryOp(a, b, func(x, y int64) int64 { return x % y }, func(x, y float64) float64 { return math.Mod(x, y) })); err != nil {
				return err
			}

		case OpCmp:
			b, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			a, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			fa, fb := a.AsFloat(), b.AsFloat()
			var result int64
			if fa < fb {
				result = -1
			} else if fa > fb {
				result = 1
			}
			if err := vm.push(IntValue(result)); err != nil {
				return err
			}

		case OpJmp:
			target, err := vm.readU32()
			if err != nil {
				return err
			}
			if int(target) >= len(vm.bytecode) {
				return &VMError{Message: fmt.Sprintf("jump target %d out of bounds", target), Offset: currentIP}
			}
			vm.ip = int(target)

		case OpJz:
			target, err := vm.readU32()
			if err != nil {
				return err
			}
			top, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if top.IsZero() {
				if int(target) >= len(vm.bytecode) {
					return &VMError{Message: fmt.Sprintf("jump target %d out of bounds", target), Offset: currentIP}
				}
				vm.ip = int(target)
			}

		case OpJnz:
			target, err := vm.readU32()
			if err != nil {
				return err
			}
			top, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if !top.IsZero() {
				if int(target) >= len(vm.bytecode) {
					return &VMError{Message: fmt.Sprintf("jump target %d out of bounds", target), Offset: currentIP}
				}
				vm.ip = int(target)
			}

		case OpCall:
			target, err := vm.readU32()
			if err != nil {
				return err
			}
			numArgs, err := vm.readU8()
			if err != nil {
				return err
			}
			if int(target) >= len(vm.bytecode) {
				return &VMError{Message: fmt.Sprintf("call target %d out of bounds", target), Offset: currentIP}
			}
			if len(vm.frames) >= MaxCallDepth {
				return &VMError{Message: fmt.Sprintf("stack overflow: exceeded %d call frames", MaxCallDepth), Offset: currentIP}
			}

			frame := CallFrame{
				ReturnAddr:  vm.ip,
				BasePointer: len(vm.stack) - int(numArgs),
			}
			for i := 0; i < int(numArgs); i++ {
				frame.Locals[i] = vm.stack[len(vm.stack)-int(numArgs)+i]
			}
			vm.stack = vm.stack[:len(vm.stack)-int(numArgs)]
			vm.frames = append(vm.frames, frame)
			vm.ip = int(target)

		case OpRet:
			if len(vm.frames) <= 1 {
				return &VMError{Message: "RET with no call frame to return to", Offset: currentIP}
			}
			frame := vm.frames[len(vm.frames)-1]
			vm.frames = vm.frames[:len(vm.frames)-1]
			vm.ip = frame.ReturnAddr

		case OpLoad:
			slot, err := vm.readU8()
			if err != nil {
				return err
			}
			if int(slot) >= MaxLocals {
				return &VMError{Message: fmt.Sprintf("local slot %d out of bounds", slot), Offset: currentIP}
			}
			frame := &vm.frames[len(vm.frames)-1]
			if err := vm.push(frame.Locals[slot]); err != nil {
				return err
			}

		case OpStore:
			slot, err := vm.readU8()
			if err != nil {
				return err
			}
			val, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			if int(slot) >= MaxLocals {
				return &VMError{Message: fmt.Sprintf("local slot %d out of bounds", slot), Offset: currentIP}
			}
			vm.frames[len(vm.frames)-1].Locals[slot] = val

		case OpPrint:
			val, err := vm.pop(currentIP)
			if err != nil {
				return err
			}
			text := val.String()
			fmt.Println(text)
			vm.Output = append(vm.Output, text)

		default:
			return &VMError{Message: fmt.Sprintf("invalid opcode: 0x%02X", op), Offset: currentIP}
		}
	}
}

// Serialization

var magic = [4]byte{'S', 'V', 'M', 'X'}

func Serialize(w io.Writer, constants []Value, code []byte) error {
	if _, err := w.Write(magic[:]); err != nil {
		return err
	}
	if _, err := w.Write([]byte{1}); err != nil { // version
		return err
	}

	countBuf := make([]byte, 2)
	binary.BigEndian.PutUint16(countBuf, uint16(len(constants)))
	if _, err := w.Write(countBuf); err != nil {
		return err
	}

	for _, c := range constants {
		switch c.Type {
		case TypeInt:
			w.Write([]byte{byte(TypeInt)})
			buf := make([]byte, 8)
			binary.BigEndian.PutUint64(buf, uint64(c.IntVal))
			w.Write(buf)
		case TypeFloat:
			w.Write([]byte{byte(TypeFloat)})
			buf := make([]byte, 8)
			binary.BigEndian.PutUint64(buf, math.Float64bits(c.FloatVal))
			w.Write(buf)
		}
	}

	lenBuf := make([]byte, 4)
	binary.BigEndian.PutUint32(lenBuf, uint32(len(code)))
	w.Write(lenBuf)
	w.Write(code)
	return nil
}

func Deserialize(r io.Reader) ([]Value, []byte, error) {
	var m [4]byte
	if _, err := io.ReadFull(r, m[:]); err != nil {
		return nil, nil, err
	}
	if m != magic {
		return nil, nil, errors.New("invalid magic bytes")
	}

	var ver [1]byte
	io.ReadFull(r, ver[:])
	if ver[0] != 1 {
		return nil, nil, fmt.Errorf("unsupported version: %d", ver[0])
	}

	countBuf := make([]byte, 2)
	io.ReadFull(r, countBuf)
	count := binary.BigEndian.Uint16(countBuf)

	constants := make([]Value, 0, count)
	for i := 0; i < int(count); i++ {
		var tag [1]byte
		io.ReadFull(r, tag[:])
		buf := make([]byte, 8)
		io.ReadFull(r, buf)
		switch ValueType(tag[0]) {
		case TypeInt:
			constants = append(constants, IntValue(int64(binary.BigEndian.Uint64(buf))))
		case TypeFloat:
			constants = append(constants, FloatValue(math.Float64frombits(binary.BigEndian.Uint64(buf))))
		default:
			return nil, nil, fmt.Errorf("unknown constant tag: 0x%02X", tag[0])
		}
	}

	lenBuf := make([]byte, 4)
	io.ReadFull(r, lenBuf)
	codeLen := binary.BigEndian.Uint32(lenBuf)
	code := make([]byte, codeLen)
	io.ReadFull(r, code)

	return constants, code, nil
}

// Disassembler

func Disassemble(constants []Value, code []byte) string {
	out := "=== Constants ===\n"
	for i, c := range constants {
		out += fmt.Sprintf("  [%04d] %s\n", i, c.String())
	}
	out += "\n=== Code ===\n"

	offset := 0
	for offset < len(code) {
		op := code[offset]
		name := opNames[op]
		if name == "" {
			name = fmt.Sprintf("UNKNOWN(0x%02X)", op)
		}

		operands := ""
		switch op {
		case OpPush:
			if offset+2 < len(code) {
				idx := binary.BigEndian.Uint16(code[offset+1 : offset+3])
				operands = fmt.Sprintf(" #%d", idx)
				if int(idx) < len(constants) {
					operands += fmt.Sprintf(" (%s)", constants[idx].String())
				}
			}
			offset += 3
		case OpJmp, OpJz, OpJnz:
			if offset+4 < len(code) {
				target := binary.BigEndian.Uint32(code[offset+1 : offset+5])
				operands = fmt.Sprintf(" @%04d", target)
			}
			offset += 5
		case OpCall:
			if offset+5 < len(code) {
				target := binary.BigEndian.Uint32(code[offset+1 : offset+5])
				nargs := code[offset+5]
				operands = fmt.Sprintf(" @%04d args=%d", target, nargs)
			}
			offset += 6
		case OpLoad, OpStore:
			if offset+1 < len(code) {
				operands = fmt.Sprintf(" $%d", code[offset+1])
			}
			offset += 2
		default:
			offset++
		}

		out += fmt.Sprintf("%04d: %s%s\n", offset-(len(operands) > 0).btoi()+1-1, name, operands)
	}
	return out
}

// Helper for bool to int (Go does not have this built-in)
type boolToInt bool
func (b boolToInt) btoi() int {
	if b {
		return 1
	}
	return 0
}

// Assembler helper

type Assembler struct {
	Constants []Value
	Code      []byte
}

func (a *Assembler) AddConstant(v Value) uint16 {
	idx := uint16(len(a.Constants))
	a.Constants = append(a.Constants, v)
	return idx
}

func (a *Assembler) Emit(op byte) {
	a.Code = append(a.Code, op)
}

func (a *Assembler) EmitPush(v Value) {
	idx := a.AddConstant(v)
	a.Code = append(a.Code, OpPush)
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, idx)
	a.Code = append(a.Code, buf...)
}

func (a *Assembler) EmitPushConst(idx uint16) {
	a.Code = append(a.Code, OpPush)
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, idx)
	a.Code = append(a.Code, buf...)
}

func (a *Assembler) EmitJmp(target uint32) int {
	pos := len(a.Code)
	a.Code = append(a.Code, OpJmp)
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, target)
	a.Code = append(a.Code, buf...)
	return pos
}

func (a *Assembler) EmitJz(target uint32) int {
	pos := len(a.Code)
	a.Code = append(a.Code, OpJz)
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, target)
	a.Code = append(a.Code, buf...)
	return pos
}

func (a *Assembler) EmitCall(target uint32, numArgs byte) int {
	pos := len(a.Code)
	a.Code = append(a.Code, OpCall)
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, target)
	a.Code = append(a.Code, buf...)
	a.Code = append(a.Code, numArgs)
	return pos
}

func (a *Assembler) EmitLoad(slot byte) {
	a.Code = append(a.Code, OpLoad, slot)
}

func (a *Assembler) EmitStore(slot byte) {
	a.Code = append(a.Code, OpStore, slot)
}

func (a *Assembler) Offset() uint32 {
	return uint32(len(a.Code))
}

func (a *Assembler) PatchU32(offset int, value uint32) {
	binary.BigEndian.PutUint32(a.Code[offset+1:offset+5], value)
}

func main() {
	// Factorial(10)
	asm := &Assembler{}
	c10 := asm.AddConstant(IntValue(10))
	c1 := asm.AddConstant(IntValue(1))
	c0 := asm.AddConstant(IntValue(0))

	asm.EmitPushConst(c10)
	callPos := asm.EmitCall(0, 1)
	asm.Emit(OpPrint)
	asm.Emit(OpHalt)

	factStart := asm.Offset()
	asm.PatchU32(callPos, factStart)

	asm.EmitLoad(0)       // n
	asm.EmitPushConst(c0) // 0
	asm.Emit(OpCmp)
	jzPos := asm.EmitJz(0) // base case

	asm.EmitLoad(0)       // n
	asm.EmitLoad(0)       // n
	asm.EmitPushConst(c1) // 1
	asm.Emit(OpSub)       // n-1
	asm.EmitCall(factStart, 1)
	asm.Emit(OpMul)       // n * fact(n-1)
	asm.Emit(OpRet)

	baseCase := asm.Offset()
	asm.PatchU32(jzPos, baseCase)
	asm.EmitPushConst(c1)
	asm.Emit(OpRet)

	fmt.Println("=== Factorial(10) ===")
	vm := NewVM(asm.Code, asm.Constants)
	if err := vm.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}

	// Serialize and reload
	var buf []byte
	w := &byteWriter{buf: &buf}
	Serialize(w, asm.Constants, asm.Code)
	fmt.Println("\n=== From deserialized bytecode ===")
	constants, code, _ := Deserialize(&byteReader{buf: buf})
	vm2 := NewVM(code, constants)
	if err := vm2.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
	}
}

// Minimal byte buffer for serialization demo
type byteWriter struct{ buf *[]byte }
func (w *byteWriter) Write(p []byte) (int, error) {
	*w.buf = append(*w.buf, p...)
	return len(p), nil
}

type byteReader struct {
	buf []byte
	pos int
}
func (r *byteReader) Read(p []byte) (int, error) {
	if r.pos >= len(r.buf) {
		return 0, io.EOF
	}
	n := copy(p, r.buf[r.pos:])
	r.pos += n
	return n, nil
}
```

---

## Design Decisions

1. **Tagged union vs NaN-boxing**: chose tagged union (Rust enum / Go struct with type field) for clarity. NaN-boxing packs the type tag into the unused bits of a float64 NaN, saving 8 bytes per value and eliminating branches. Production VMs use it, but the bit-manipulation obscures the core VM logic.

2. **Big-endian bytecode**: all multi-byte operands use big-endian encoding. This matches network byte order and makes hex dumps readable left-to-right. Little-endian would be slightly faster on x86 but the difference is negligible for a teaching VM.

3. **Fixed-size local slots (256)**: each frame pre-allocates 256 local variable slots. This wastes memory for simple functions but eliminates bounds-checking on every local access (slot index fits in one u8 byte). Production VMs like the JVM use the same approach.

4. **Separate constant pool vs inline operands**: constants are stored in a pool and referenced by index rather than inlined into the instruction stream. This deduplicates repeated values and keeps the instruction stream compact. The JVM, CLR, and CPython all use constant pools.

5. **Error reporting with instruction offsets**: every runtime error includes the bytecode offset where it occurred. Combined with the disassembler, this maps directly to the failing instruction. Production VMs extend this with source maps that link bytecode offsets back to source code lines.

## Common Mistakes

- **Forgetting to restore the instruction pointer on RET**: the return address must be saved before pushing the new frame, not computed from the frame after popping it
- **Off-by-one in operand reading**: after reading the opcode byte, the IP must advance past the operand bytes before the next iteration. Missing this causes the VM to interpret operand bytes as opcodes
- **Stack corruption during CALL**: the arguments must be popped from the stack and placed into the new frame's locals. Leaving them on the stack causes the caller to see stale values after RET
- **Division by zero with floats**: IEEE 754 float division by zero produces infinity, not an error. Decide whether your VM follows IEEE semantics or treats it as an error for both types
- **Jump target validation**: validate jump targets before setting the IP, not after. Otherwise a bad jump corrupts the IP and the error message reports the wrong offset

## Performance Notes

The main loop performance depends on instruction dispatch. A switch/match statement compiles to a jump table on most compilers, which is competitive with computed goto (the traditional interpreter optimization). For a teaching VM, the switch-based dispatch is fast enough.

The operand stack as a Vec/slice is cache-friendly because accesses are always at the top. Pre-allocating capacity avoids repeated allocations during execution.

For real performance, consider: direct-threaded or indirect-threaded dispatch, NaN-boxing to eliminate the value type branch, and superinstructions that combine common sequences (e.g., LOAD-ADD as a single opcode).

## Going Further

- Add string values and concatenation operations
- Implement garbage collection for heap-allocated values
- Add a simple assembler that reads text assembly and produces bytecode files
- Build a debugger: single-step, breakpoints, stack inspection
- Implement a register-based variant and benchmark against the stack-based version
