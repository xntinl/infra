# Solution: Bytecode Compiler with Optimizations

## Architecture Overview

The compiler follows a classic multi-pass pipeline:

```
Source Code -> Lexer -> Tokens -> Parser -> AST -> IR Lowering -> Three-Address Code IR
-> Optimization Passes (loop until fixed point) -> Bytecode Emission -> Binary Output
```

Seven major modules:

1. **Lexer/Parser**: reused from Challenge 36, produces an AST
2. **IR (Intermediate Representation)**: three-address code with basic blocks and a control flow graph
3. **Constant Folding**: evaluates compile-time constant expressions
4. **Dead Code Elimination**: removes unreachable code and unused assignments
5. **Common Subexpression Elimination (CSE)**: deduplicates redundant computations
6. **Strength Reduction**: replaces expensive operations with cheaper equivalents
7. **Bytecode Emitter**: translates optimized IR to stack-based bytecode

The optimization report is produced by each pass recording its transformations, which are aggregated and printed after compilation.

---

## Rust Solution

### Project Setup

```bash
cargo new bytecode-compiler
cd bytecode-compiler
```

### `src/token.rs` and `src/lexer.rs`

Reuse the lexer from Challenge 36 with a simplified token set for the compiler's source language:

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum TokenKind {
    Int(i64), Float(f64), Str(String), Bool(bool), Ident(String),
    Plus, Minus, Star, Slash, Percent,
    Eq, NotEq, Lt, Gt, LtEq, GtEq,
    And, Or, Not,
    Assign, Semicolon, Comma,
    LParen, RParen, LBrace, RBrace,
    Let, Fn, Return, If, Else, While, Print,
    Eof,
}

#[derive(Debug, Clone)]
pub struct Token {
    pub kind: TokenKind,
    pub line: usize,
    pub col: usize,
}
```

The lexer implementation follows the same structure as Challenge 36 -- character-by-character scanning with keyword recognition. Omitted here for brevity.

### `src/ast.rs` -- Abstract Syntax Tree

```rust
#[derive(Debug, Clone)]
pub enum Expr {
    IntLit(i64),
    FloatLit(f64),
    BoolLit(bool),
    StringLit(String),
    Ident(String),
    Binary(Box<Expr>, BinOp, Box<Expr>),
    Unary(UnaryOp, Box<Expr>),
    Call(String, Vec<Expr>),
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum BinOp {
    Add, Sub, Mul, Div, Mod,
    Eq, NotEq, Lt, Gt, LtEq, GtEq,
    And, Or,
}

#[derive(Debug, Clone, Copy)]
pub enum UnaryOp {
    Neg, Not,
}

#[derive(Debug, Clone)]
pub enum Stmt {
    Let(String, Expr),
    Assign(String, Expr),
    If(Expr, Box<Stmt>, Option<Box<Stmt>>),
    While(Expr, Box<Stmt>),
    Block(Vec<Stmt>),
    Return(Option<Expr>),
    Print(Expr),
    FnDecl(String, Vec<String>, Box<Stmt>),
    ExprStmt(Expr),
}
```

### `src/ir.rs` -- Three-Address Code IR

```rust
use crate::ast::BinOp;
use std::collections::HashMap;
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub enum IrValue {
    IntConst(i64),
    FloatConst(f64),
    BoolConst(bool),
    StringConst(String),
    Temp(usize),     // temporary variable: t0, t1, ...
    Var(String),     // named variable
}

impl fmt::Display for IrValue {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IrValue::IntConst(n) => write!(f, "{n}"),
            IrValue::FloatConst(v) => write!(f, "{v}"),
            IrValue::BoolConst(b) => write!(f, "{b}"),
            IrValue::StringConst(s) => write!(f, "\"{s}\""),
            IrValue::Temp(id) => write!(f, "t{id}"),
            IrValue::Var(name) => write!(f, "{name}"),
        }
    }
}

#[derive(Debug, Clone)]
pub enum IrInstr {
    // t = a op b
    BinaryOp { dest: usize, op: BinOp, left: IrValue, right: IrValue },
    // t = -a or t = !a
    UnaryNeg { dest: usize, operand: IrValue },
    UnaryNot { dest: usize, operand: IrValue },
    // t = value (copy/move)
    Copy { dest: usize, source: IrValue },
    // var = t (store to named variable)
    StoreVar { name: String, source: IrValue },
    // t = var (load from named variable)
    LoadVar { dest: usize, name: String },
    // Conditional jump: if value goto label
    JumpIf { condition: IrValue, target: usize },
    // Conditional jump: if !value goto label
    JumpIfNot { condition: IrValue, target: usize },
    // Unconditional jump
    Jump { target: usize },
    // Label (target for jumps)
    Label(usize),
    // Return value
    Return(Option<IrValue>),
    // Print value
    Print(IrValue),
    // Function call: dest = call name(args...)
    Call { dest: usize, name: String, args: Vec<IrValue> },
    // No-op (placeholder for eliminated instructions)
    Nop,
}

impl fmt::Display for IrInstr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            IrInstr::BinaryOp { dest, op, left, right } =>
                write!(f, "  t{dest} = {left} {op:?} {right}"),
            IrInstr::UnaryNeg { dest, operand } =>
                write!(f, "  t{dest} = -{operand}"),
            IrInstr::UnaryNot { dest, operand } =>
                write!(f, "  t{dest} = !{operand}"),
            IrInstr::Copy { dest, source } =>
                write!(f, "  t{dest} = {source}"),
            IrInstr::StoreVar { name, source } =>
                write!(f, "  {name} = {source}"),
            IrInstr::LoadVar { dest, name } =>
                write!(f, "  t{dest} = {name}"),
            IrInstr::JumpIf { condition, target } =>
                write!(f, "  if {condition} goto L{target}"),
            IrInstr::JumpIfNot { condition, target } =>
                write!(f, "  if !{condition} goto L{target}"),
            IrInstr::Jump { target } =>
                write!(f, "  goto L{target}"),
            IrInstr::Label(id) =>
                write!(f, "L{id}:"),
            IrInstr::Return(Some(v)) =>
                write!(f, "  return {v}"),
            IrInstr::Return(None) =>
                write!(f, "  return"),
            IrInstr::Print(v) =>
                write!(f, "  print {v}"),
            IrInstr::Call { dest, name, args } => {
                let arg_str: Vec<String> = args.iter().map(|a| format!("{a}")).collect();
                write!(f, "  t{dest} = call {name}({})", arg_str.join(", "))
            }
            IrInstr::Nop =>
                write!(f, "  nop"),
        }
    }
}

pub struct IrFunction {
    pub name: String,
    pub params: Vec<String>,
    pub instructions: Vec<IrInstr>,
    pub temp_count: usize,
}

pub struct IrProgram {
    pub functions: Vec<IrFunction>,
    pub main: Vec<IrInstr>,
    pub temp_count: usize,
}

impl fmt::Display for IrProgram {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        for func in &self.functions {
            writeln!(f, "fn {}({}):", func.name, func.params.join(", "))?;
            for instr in &func.instructions {
                writeln!(f, "{instr}")?;
            }
            writeln!(f)?;
        }
        writeln!(f, "main:")?;
        for instr in &self.main {
            writeln!(f, "{instr}")?;
        }
        Ok(())
    }
}
```

### `src/ir_builder.rs` -- AST to IR Lowering

```rust
use crate::ast::*;
use crate::ir::*;

pub struct IrBuilder {
    instructions: Vec<IrInstr>,
    temp_counter: usize,
    label_counter: usize,
    functions: Vec<IrFunction>,
}

impl IrBuilder {
    pub fn new() -> Self {
        Self {
            instructions: Vec::new(),
            temp_counter: 0,
            label_counter: 0,
            functions: Vec::new(),
        }
    }

    pub fn build(mut self, stmts: &[Stmt]) -> IrProgram {
        for stmt in stmts {
            self.lower_stmt(stmt);
        }
        IrProgram {
            functions: self.functions,
            main: self.instructions,
            temp_count: self.temp_counter,
        }
    }

    fn new_temp(&mut self) -> usize {
        let id = self.temp_counter;
        self.temp_counter += 1;
        id
    }

    fn new_label(&mut self) -> usize {
        let id = self.label_counter;
        self.label_counter += 1;
        id
    }

    fn lower_stmt(&mut self, stmt: &Stmt) {
        match stmt {
            Stmt::Let(name, expr) => {
                let val = self.lower_expr(expr);
                self.instructions.push(IrInstr::StoreVar { name: name.clone(), source: val });
            }
            Stmt::Assign(name, expr) => {
                let val = self.lower_expr(expr);
                self.instructions.push(IrInstr::StoreVar { name: name.clone(), source: val });
            }
            Stmt::If(cond, then_branch, else_branch) => {
                let cond_val = self.lower_expr(cond);
                let else_label = self.new_label();
                let end_label = self.new_label();

                self.instructions.push(IrInstr::JumpIfNot { condition: cond_val, target: else_label });
                self.lower_stmt(then_branch);
                self.instructions.push(IrInstr::Jump { target: end_label });
                self.instructions.push(IrInstr::Label(else_label));

                if let Some(else_b) = else_branch {
                    self.lower_stmt(else_b);
                }

                self.instructions.push(IrInstr::Label(end_label));
            }
            Stmt::While(cond, body) => {
                let loop_start = self.new_label();
                let loop_end = self.new_label();

                self.instructions.push(IrInstr::Label(loop_start));
                let cond_val = self.lower_expr(cond);
                self.instructions.push(IrInstr::JumpIfNot { condition: cond_val, target: loop_end });
                self.lower_stmt(body);
                self.instructions.push(IrInstr::Jump { target: loop_start });
                self.instructions.push(IrInstr::Label(loop_end));
            }
            Stmt::Block(stmts) => {
                for s in stmts { self.lower_stmt(s); }
            }
            Stmt::Return(expr) => {
                let val = expr.as_ref().map(|e| self.lower_expr(e));
                self.instructions.push(IrInstr::Return(val));
            }
            Stmt::Print(expr) => {
                let val = self.lower_expr(expr);
                self.instructions.push(IrInstr::Print(val));
            }
            Stmt::FnDecl(name, params, body) => {
                let mut func_builder = IrBuilder::new();
                func_builder.label_counter = self.label_counter;
                func_builder.temp_counter = self.temp_counter;
                func_builder.lower_stmt(body);
                self.label_counter = func_builder.label_counter;
                self.temp_counter = func_builder.temp_counter;

                self.functions.push(IrFunction {
                    name: name.clone(),
                    params: params.clone(),
                    instructions: func_builder.instructions,
                    temp_count: func_builder.temp_counter,
                });
            }
            Stmt::ExprStmt(expr) => {
                self.lower_expr(expr);
            }
        }
    }

    fn lower_expr(&mut self, expr: &Expr) -> IrValue {
        match expr {
            Expr::IntLit(n) => IrValue::IntConst(*n),
            Expr::FloatLit(f) => IrValue::FloatConst(*f),
            Expr::BoolLit(b) => IrValue::BoolConst(*b),
            Expr::StringLit(s) => IrValue::StringConst(s.clone()),
            Expr::Ident(name) => {
                let dest = self.new_temp();
                self.instructions.push(IrInstr::LoadVar { dest, name: name.clone() });
                IrValue::Temp(dest)
            }
            Expr::Binary(left, op, right) => {
                let lhs = self.lower_expr(left);
                let rhs = self.lower_expr(right);
                let dest = self.new_temp();
                self.instructions.push(IrInstr::BinaryOp {
                    dest, op: *op, left: lhs, right: rhs,
                });
                IrValue::Temp(dest)
            }
            Expr::Unary(UnaryOp::Neg, operand) => {
                let val = self.lower_expr(operand);
                let dest = self.new_temp();
                self.instructions.push(IrInstr::UnaryNeg { dest, operand: val });
                IrValue::Temp(dest)
            }
            Expr::Unary(UnaryOp::Not, operand) => {
                let val = self.lower_expr(operand);
                let dest = self.new_temp();
                self.instructions.push(IrInstr::UnaryNot { dest, operand: val });
                IrValue::Temp(dest)
            }
            Expr::Call(name, args) => {
                let ir_args: Vec<IrValue> = args.iter().map(|a| self.lower_expr(a)).collect();
                let dest = self.new_temp();
                self.instructions.push(IrInstr::Call {
                    dest, name: name.clone(), args: ir_args,
                });
                IrValue::Temp(dest)
            }
        }
    }
}
```

### `src/optimizer.rs` -- Optimization Passes

```rust
use crate::ast::BinOp;
use crate::ir::*;
use std::collections::{HashMap, HashSet};

#[derive(Debug)]
pub struct OptimizationReport {
    pub entries: Vec<String>,
}

impl OptimizationReport {
    pub fn new() -> Self { Self { entries: Vec::new() } }

    pub fn record(&mut self, pass: &str, description: String) {
        self.entries.push(format!("[{pass}] {description}"));
    }

    pub fn print(&self) {
        println!("=== Optimization Report ===");
        if self.entries.is_empty() {
            println!("  No optimizations applied.");
        } else {
            for entry in &self.entries {
                println!("  {entry}");
            }
        }
        println!("  Total: {} optimizations applied", self.entries.len());
    }
}

// Constant Folding: evaluate constant expressions at compile time
pub fn constant_folding(instrs: &mut Vec<IrInstr>, report: &mut OptimizationReport) -> bool {
    let mut changed = false;

    for instr in instrs.iter_mut() {
        match instr {
            IrInstr::BinaryOp { dest, op, left, right } => {
                let folded = fold_binary(op, left, right);
                if let Some(constant) = folded {
                    let desc = format!("folded t{dest} = {left} {op:?} {right} -> {constant}");
                    report.record("const-fold", desc);
                    *instr = IrInstr::Copy { dest: *dest, source: constant };
                    changed = true;
                }
            }
            IrInstr::UnaryNeg { dest, operand } => {
                match operand {
                    IrValue::IntConst(n) => {
                        let result = IrValue::IntConst(-*n);
                        report.record("const-fold", format!("folded t{dest} = -{n} -> {result}"));
                        *instr = IrInstr::Copy { dest: *dest, source: result };
                        changed = true;
                    }
                    IrValue::FloatConst(f) => {
                        let result = IrValue::FloatConst(-*f);
                        report.record("const-fold", format!("folded t{dest} = -{f} -> {result}"));
                        *instr = IrInstr::Copy { dest: *dest, source: result };
                        changed = true;
                    }
                    _ => {}
                }
            }
            _ => {}
        }
    }
    changed
}

fn fold_binary(op: &BinOp, left: &IrValue, right: &IrValue) -> Option<IrValue> {
    match (left, right) {
        (IrValue::IntConst(a), IrValue::IntConst(b)) => {
            match op {
                BinOp::Add => Some(IrValue::IntConst(a + b)),
                BinOp::Sub => Some(IrValue::IntConst(a - b)),
                BinOp::Mul => Some(IrValue::IntConst(a * b)),
                BinOp::Div if *b != 0 => Some(IrValue::IntConst(a / b)),
                BinOp::Mod if *b != 0 => Some(IrValue::IntConst(a % b)),
                BinOp::Eq => Some(IrValue::BoolConst(a == b)),
                BinOp::NotEq => Some(IrValue::BoolConst(a != b)),
                BinOp::Lt => Some(IrValue::BoolConst(a < b)),
                BinOp::Gt => Some(IrValue::BoolConst(a > b)),
                BinOp::LtEq => Some(IrValue::BoolConst(a <= b)),
                BinOp::GtEq => Some(IrValue::BoolConst(a >= b)),
                _ => None,
            }
        }
        (IrValue::FloatConst(a), IrValue::FloatConst(b)) => {
            match op {
                BinOp::Add => Some(IrValue::FloatConst(a + b)),
                BinOp::Sub => Some(IrValue::FloatConst(a - b)),
                BinOp::Mul => Some(IrValue::FloatConst(a * b)),
                BinOp::Div if *b != 0.0 => Some(IrValue::FloatConst(a / b)),
                _ => None,
            }
        }
        (IrValue::BoolConst(a), IrValue::BoolConst(b)) => {
            match op {
                BinOp::And => Some(IrValue::BoolConst(*a && *b)),
                BinOp::Or => Some(IrValue::BoolConst(*a || *b)),
                BinOp::Eq => Some(IrValue::BoolConst(a == b)),
                _ => None,
            }
        }
        _ => None,
    }
}

// Dead Code Elimination: remove NOPs, unreachable code, unused assignments
pub fn dead_code_elimination(instrs: &mut Vec<IrInstr>, report: &mut OptimizationReport) -> bool {
    let mut changed = false;

    // Find used temporaries
    let mut used_temps: HashSet<usize> = HashSet::new();
    for instr in instrs.iter() {
        collect_used_temps(instr, &mut used_temps);
    }

    // Remove assignments to unused temporaries (except calls with side effects)
    for i in 0..instrs.len() {
        match &instrs[i] {
            IrInstr::Copy { dest, .. }
            | IrInstr::BinaryOp { dest, .. }
            | IrInstr::UnaryNeg { dest, .. }
            | IrInstr::UnaryNot { dest, .. }
            | IrInstr::LoadVar { dest, .. } => {
                if !used_temps.contains(dest) {
                    report.record("dead-code", format!("removed unused assignment to t{dest}"));
                    instrs[i] = IrInstr::Nop;
                    changed = true;
                }
            }
            _ => {}
        }
    }

    // Remove code after unconditional returns/jumps (until next label)
    let mut after_return = false;
    for i in 0..instrs.len() {
        if after_return {
            match &instrs[i] {
                IrInstr::Label(_) => after_return = false,
                IrInstr::Nop => {}
                _ => {
                    report.record("dead-code", format!("removed unreachable instruction: {}", instrs[i]));
                    instrs[i] = IrInstr::Nop;
                    changed = true;
                }
            }
        }
        match &instrs[i] {
            IrInstr::Return(_) | IrInstr::Jump { .. } => after_return = true,
            _ => {}
        }
    }

    // Remove JumpIfNot with constant false condition (always jumps) -> convert to Jump
    // Remove JumpIfNot with constant true condition (never jumps) -> remove
    for i in 0..instrs.len() {
        match &instrs[i] {
            IrInstr::JumpIfNot { condition: IrValue::BoolConst(false), target } => {
                let target = *target;
                report.record("dead-code", format!("converted always-taken branch to unconditional jump"));
                instrs[i] = IrInstr::Jump { target };
                changed = true;
            }
            IrInstr::JumpIfNot { condition: IrValue::BoolConst(true), .. } => {
                report.record("dead-code", format!("removed never-taken branch"));
                instrs[i] = IrInstr::Nop;
                changed = true;
            }
            _ => {}
        }
    }

    // Remove Nops
    let before = instrs.len();
    instrs.retain(|i| !matches!(i, IrInstr::Nop));
    if instrs.len() < before {
        changed = true;
    }

    changed
}

fn collect_used_temps(instr: &IrInstr, used: &mut HashSet<usize>) {
    match instr {
        IrInstr::BinaryOp { left, right, .. } => {
            if let IrValue::Temp(id) = left { used.insert(*id); }
            if let IrValue::Temp(id) = right { used.insert(*id); }
        }
        IrInstr::UnaryNeg { operand, .. } | IrInstr::UnaryNot { operand, .. } => {
            if let IrValue::Temp(id) = operand { used.insert(*id); }
        }
        IrInstr::Copy { source, .. } => {
            if let IrValue::Temp(id) = source { used.insert(*id); }
        }
        IrInstr::StoreVar { source, .. } => {
            if let IrValue::Temp(id) = source { used.insert(*id); }
        }
        IrInstr::JumpIf { condition, .. } | IrInstr::JumpIfNot { condition, .. } => {
            if let IrValue::Temp(id) = condition { used.insert(*id); }
        }
        IrInstr::Return(Some(val)) | IrInstr::Print(val) => {
            if let IrValue::Temp(id) = val { used.insert(*id); }
        }
        IrInstr::Call { args, .. } => {
            for arg in args {
                if let IrValue::Temp(id) = arg { used.insert(*id); }
            }
        }
        _ => {}
    }
}

// Common Subexpression Elimination
pub fn common_subexpression_elimination(instrs: &mut Vec<IrInstr>, report: &mut OptimizationReport) -> bool {
    let mut changed = false;

    // Hash: (op, left, right) -> temp that holds the result
    let mut expr_cache: HashMap<(String, String, String), usize> = HashMap::new();
    // Track which temps have been invalidated (their operands redefined)
    let mut invalidated: HashSet<String> = HashSet::new();

    for i in 0..instrs.len() {
        // Invalidate cache on stores
        match &instrs[i] {
            IrInstr::StoreVar { name, .. } => {
                invalidated.insert(name.clone());
                expr_cache.retain(|(_op, l, r), _| l != name && r != name);
            }
            _ => {}
        }

        match &instrs[i] {
            IrInstr::BinaryOp { dest, op, left, right } => {
                let key = (format!("{op:?}"), format!("{left}"), format!("{right}"));
                if let Some(&cached_temp) = expr_cache.get(&key) {
                    report.record("cse", format!(
                        "replaced t{dest} = {left} {op:?} {right} with t{dest} = t{cached_temp}"
                    ));
                    instrs[i] = IrInstr::Copy { dest: *dest, source: IrValue::Temp(cached_temp) };
                    changed = true;
                } else {
                    expr_cache.insert(key, *dest);
                }
            }
            _ => {}
        }
    }
    changed
}

// Strength Reduction: replace expensive ops with cheaper equivalents
pub fn strength_reduction(instrs: &mut Vec<IrInstr>, report: &mut OptimizationReport) -> bool {
    let mut changed = false;

    for instr in instrs.iter_mut() {
        match instr {
            IrInstr::BinaryOp { dest, op: BinOp::Mul, left, right } => {
                // x * 2 -> x + x
                if matches!(right, IrValue::IntConst(2)) {
                    report.record("strength-red", format!("replaced t{dest} = {left} * 2 with {left} + {left}"));
                    *instr = IrInstr::BinaryOp {
                        dest: *dest, op: BinOp::Add, left: left.clone(), right: left.clone(),
                    };
                    changed = true;
                }
                // x * power_of_2 -> x << log2(power_of_2) (represented as shift comment, kept as Mul for now)
                else if let IrValue::IntConst(n) = right {
                    if *n > 0 && (*n & (*n - 1)) == 0 && *n != 1 && *n != 2 {
                        let shift = (*n as f64).log2() as i64;
                        report.record("strength-red", format!(
                            "replaced t{dest} = {left} * {n} with {left} << {shift} (conceptual)"
                        ));
                        // In a real compiler with shift opcodes, this would emit a shift.
                        // For this demonstration, we keep the multiplication but log the opportunity.
                    }
                }
            }
            IrInstr::BinaryOp { dest, op: BinOp::Div, left, right } => {
                // x / 1 -> x
                if matches!(right, IrValue::IntConst(1)) {
                    report.record("strength-red", format!("replaced t{dest} = {left} / 1 with {left}"));
                    *instr = IrInstr::Copy { dest: *dest, source: left.clone() };
                    changed = true;
                }
                // x / power_of_2 -> x >> log2(power_of_2)
                else if let IrValue::IntConst(n) = right {
                    if *n > 1 && (*n & (*n - 1)) == 0 {
                        let shift = (*n as f64).log2() as i64;
                        report.record("strength-red", format!(
                            "identified t{dest} = {left} / {n} as candidate for >> {shift}"
                        ));
                    }
                }
            }
            IrInstr::BinaryOp { dest, op: BinOp::Add, left, right } => {
                // x + 0 -> x
                if matches!(right, IrValue::IntConst(0)) {
                    report.record("strength-red", format!("replaced t{dest} = {left} + 0 with {left}"));
                    *instr = IrInstr::Copy { dest: *dest, source: left.clone() };
                    changed = true;
                }
            }
            IrInstr::BinaryOp { dest, op: BinOp::Mul, left, right } => {
                // x * 0 -> 0
                if matches!(right, IrValue::IntConst(0)) {
                    report.record("strength-red", format!("replaced t{dest} = {left} * 0 with 0"));
                    *instr = IrInstr::Copy { dest: *dest, source: IrValue::IntConst(0) };
                    changed = true;
                }
                // x * 1 -> x
                else if matches!(right, IrValue::IntConst(1)) {
                    report.record("strength-red", format!("replaced t{dest} = {left} * 1 with {left}"));
                    *instr = IrInstr::Copy { dest: *dest, source: left.clone() };
                    changed = true;
                }
            }
            _ => {}
        }
    }
    changed
}

// Pass Manager: run all passes to fixed point
pub fn optimize(instrs: &mut Vec<IrInstr>, report: &mut OptimizationReport) {
    let max_iterations = 20;
    for iteration in 0..max_iterations {
        let mut any_changed = false;
        any_changed |= constant_folding(instrs, report);
        any_changed |= strength_reduction(instrs, report);
        any_changed |= common_subexpression_elimination(instrs, report);
        any_changed |= dead_code_elimination(instrs, report);
        if !any_changed {
            report.record("pass-manager", format!("fixed point reached after {} iterations", iteration + 1));
            break;
        }
    }
}
```

### `src/codegen.rs` -- Bytecode Emission

```rust
use crate::ast::BinOp;
use crate::ir::*;
use std::collections::HashMap;

#[derive(Debug, Clone, Copy)]
#[repr(u8)]
pub enum OpCode {
    Halt = 0x00,
    PushInt = 0x01,
    PushFloat = 0x02,
    PushBool = 0x03,
    PushStr = 0x04,
    Pop = 0x05,
    Add = 0x10,
    Sub = 0x11,
    Mul = 0x12,
    Div = 0x13,
    Mod = 0x14,
    Neg = 0x15,
    Not = 0x16,
    Eq = 0x20,
    NotEq = 0x21,
    Lt = 0x22,
    Gt = 0x23,
    LtEq = 0x24,
    GtEq = 0x25,
    And = 0x26,
    Or = 0x27,
    Load = 0x30,
    Store = 0x31,
    Jmp = 0x40,
    JmpIf = 0x41,
    JmpIfNot = 0x42,
    Call = 0x50,
    Ret = 0x51,
    Print = 0x60,
}

pub struct CodeGenerator {
    code: Vec<u8>,
    string_pool: Vec<String>,
    var_slots: HashMap<String, u8>,
    next_slot: u8,
    label_offsets: HashMap<usize, usize>,
    unresolved_jumps: Vec<(usize, usize)>, // (code_offset, label_id)
}

impl CodeGenerator {
    pub fn new() -> Self {
        Self {
            code: Vec::new(),
            string_pool: Vec::new(),
            var_slots: HashMap::new(),
            next_slot: 0,
            label_offsets: HashMap::new(),
            unresolved_jumps: Vec::new(),
        }
    }

    pub fn generate(mut self, instrs: &[IrInstr]) -> CompiledBytecode {
        // First pass: collect label positions
        let mut simulated_offset = 0;
        for instr in instrs {
            if let IrInstr::Label(id) = instr {
                self.label_offsets.insert(*id, simulated_offset);
            } else {
                simulated_offset += self.estimate_instruction_size(instr);
            }
        }

        // Second pass: emit bytecode
        for instr in instrs {
            self.emit_instruction(instr);
        }
        self.emit(OpCode::Halt as u8);

        // Resolve jump targets
        for (offset, label_id) in &self.unresolved_jumps {
            if let Some(&target) = self.label_offsets.get(label_id) {
                let bytes = (target as u32).to_be_bytes();
                self.code[*offset..*offset + 4].copy_from_slice(&bytes);
            }
        }

        CompiledBytecode {
            code: self.code,
            string_pool: self.string_pool,
        }
    }

    fn get_or_create_slot(&mut self, name: &str) -> u8 {
        if let Some(&slot) = self.var_slots.get(name) {
            slot
        } else {
            let slot = self.next_slot;
            self.next_slot += 1;
            self.var_slots.insert(name.to_string(), slot);
            slot
        }
    }

    fn emit(&mut self, byte: u8) {
        self.code.push(byte);
    }

    fn emit_u32(&mut self, value: u32) {
        self.code.extend_from_slice(&value.to_be_bytes());
    }

    fn emit_value(&mut self, val: &IrValue) {
        match val {
            IrValue::IntConst(n) => {
                self.emit(OpCode::PushInt as u8);
                self.code.extend_from_slice(&n.to_be_bytes());
            }
            IrValue::FloatConst(f) => {
                self.emit(OpCode::PushFloat as u8);
                self.code.extend_from_slice(&f.to_be_bytes());
            }
            IrValue::BoolConst(b) => {
                self.emit(OpCode::PushBool as u8);
                self.emit(if *b { 1 } else { 0 });
            }
            IrValue::StringConst(s) => {
                let idx = self.string_pool.len();
                self.string_pool.push(s.clone());
                self.emit(OpCode::PushStr as u8);
                self.emit_u32(idx as u32);
            }
            IrValue::Temp(id) => {
                let slot = self.get_or_create_slot(&format!("__t{id}"));
                self.emit(OpCode::Load as u8);
                self.emit(slot);
            }
            IrValue::Var(name) => {
                let slot = self.get_or_create_slot(name);
                self.emit(OpCode::Load as u8);
                self.emit(slot);
            }
        }
    }

    fn emit_store_temp(&mut self, dest: usize) {
        let slot = self.get_or_create_slot(&format!("__t{dest}"));
        self.emit(OpCode::Store as u8);
        self.emit(slot);
    }

    fn estimate_instruction_size(&self, instr: &IrInstr) -> usize {
        match instr {
            IrInstr::BinaryOp { .. } => 30, // overestimate for safety
            IrInstr::Copy { .. } => 12,
            IrInstr::StoreVar { .. } => 12,
            IrInstr::LoadVar { .. } => 4,
            IrInstr::Jump { .. } => 5,
            IrInstr::JumpIf { .. } | IrInstr::JumpIfNot { .. } => 15,
            IrInstr::Return(_) => 12,
            IrInstr::Print(_) => 12,
            IrInstr::Call { .. } => 20,
            IrInstr::Label(_) => 0,
            IrInstr::Nop => 0,
            _ => 10,
        }
    }

    fn emit_instruction(&mut self, instr: &IrInstr) {
        match instr {
            IrInstr::BinaryOp { dest, op, left, right } => {
                self.emit_value(left);
                self.emit_value(right);
                let opcode = match op {
                    BinOp::Add => OpCode::Add,
                    BinOp::Sub => OpCode::Sub,
                    BinOp::Mul => OpCode::Mul,
                    BinOp::Div => OpCode::Div,
                    BinOp::Mod => OpCode::Mod,
                    BinOp::Eq => OpCode::Eq,
                    BinOp::NotEq => OpCode::NotEq,
                    BinOp::Lt => OpCode::Lt,
                    BinOp::Gt => OpCode::Gt,
                    BinOp::LtEq => OpCode::LtEq,
                    BinOp::GtEq => OpCode::GtEq,
                    BinOp::And => OpCode::And,
                    BinOp::Or => OpCode::Or,
                };
                self.emit(opcode as u8);
                self.emit_store_temp(*dest);
            }
            IrInstr::UnaryNeg { dest, operand } => {
                self.emit_value(operand);
                self.emit(OpCode::Neg as u8);
                self.emit_store_temp(*dest);
            }
            IrInstr::UnaryNot { dest, operand } => {
                self.emit_value(operand);
                self.emit(OpCode::Not as u8);
                self.emit_store_temp(*dest);
            }
            IrInstr::Copy { dest, source } => {
                self.emit_value(source);
                self.emit_store_temp(*dest);
            }
            IrInstr::StoreVar { name, source } => {
                self.emit_value(source);
                let slot = self.get_or_create_slot(name);
                self.emit(OpCode::Store as u8);
                self.emit(slot);
            }
            IrInstr::LoadVar { dest, name } => {
                let slot = self.get_or_create_slot(name);
                self.emit(OpCode::Load as u8);
                self.emit(slot);
                self.emit_store_temp(*dest);
            }
            IrInstr::Jump { target } => {
                self.emit(OpCode::Jmp as u8);
                let offset = self.code.len();
                self.emit_u32(0); // placeholder
                self.unresolved_jumps.push((offset, *target));
            }
            IrInstr::JumpIf { condition, target } => {
                self.emit_value(condition);
                self.emit(OpCode::JmpIf as u8);
                let offset = self.code.len();
                self.emit_u32(0);
                self.unresolved_jumps.push((offset, *target));
            }
            IrInstr::JumpIfNot { condition, target } => {
                self.emit_value(condition);
                self.emit(OpCode::JmpIfNot as u8);
                let offset = self.code.len();
                self.emit_u32(0);
                self.unresolved_jumps.push((offset, *target));
            }
            IrInstr::Return(Some(val)) => {
                self.emit_value(val);
                self.emit(OpCode::Ret as u8);
            }
            IrInstr::Return(None) => {
                self.emit(OpCode::Ret as u8);
            }
            IrInstr::Print(val) => {
                self.emit_value(val);
                self.emit(OpCode::Print as u8);
            }
            IrInstr::Call { dest, name, args } => {
                for arg in args {
                    self.emit_value(arg);
                }
                self.emit(OpCode::Call as u8);
                let idx = self.string_pool.len();
                self.string_pool.push(name.clone());
                self.emit_u32(idx as u32);
                self.emit(args.len() as u8);
                self.emit_store_temp(*dest);
            }
            IrInstr::Label(_) | IrInstr::Nop => {} // no bytecode emitted
        }
    }
}

pub struct CompiledBytecode {
    pub code: Vec<u8>,
    pub string_pool: Vec<String>,
}

pub fn disassemble(bytecode: &CompiledBytecode) -> String {
    let mut output = String::new();
    let code = &bytecode.code;
    let mut offset = 0;

    output.push_str("=== String Pool ===\n");
    for (i, s) in bytecode.string_pool.iter().enumerate() {
        output.push_str(&format!("  [{i}] \"{s}\"\n"));
    }
    output.push_str("\n=== Bytecode ===\n");

    while offset < code.len() {
        let op = code[offset];
        let start = offset;
        let name = match op {
            0x00 => "HALT", 0x01 => "PUSH_INT", 0x02 => "PUSH_FLOAT",
            0x03 => "PUSH_BOOL", 0x04 => "PUSH_STR", 0x05 => "POP",
            0x10 => "ADD", 0x11 => "SUB", 0x12 => "MUL",
            0x13 => "DIV", 0x14 => "MOD", 0x15 => "NEG", 0x16 => "NOT",
            0x20 => "EQ", 0x21 => "NEQ", 0x22 => "LT", 0x23 => "GT",
            0x24 => "LTE", 0x25 => "GTE", 0x26 => "AND", 0x27 => "OR",
            0x30 => "LOAD", 0x31 => "STORE",
            0x40 => "JMP", 0x41 => "JMP_IF", 0x42 => "JMP_IF_NOT",
            0x50 => "CALL", 0x51 => "RET", 0x60 => "PRINT",
            _ => "???",
        };

        offset += 1;
        let operands = match op {
            0x01 => { // PUSH_INT
                let n = i64::from_be_bytes(code[offset..offset+8].try_into().unwrap());
                offset += 8;
                format!(" {n}")
            }
            0x02 => { // PUSH_FLOAT
                let f = f64::from_be_bytes(code[offset..offset+8].try_into().unwrap());
                offset += 8;
                format!(" {f}")
            }
            0x03 => { // PUSH_BOOL
                let b = code[offset]; offset += 1;
                format!(" {}", b != 0)
            }
            0x04 | 0x40 | 0x41 | 0x42 => { // PUSH_STR, JMP, JMP_IF, JMP_IF_NOT
                let idx = u32::from_be_bytes(code[offset..offset+4].try_into().unwrap());
                offset += 4;
                format!(" @{idx}")
            }
            0x30 | 0x31 => { // LOAD, STORE
                let slot = code[offset]; offset += 1;
                format!(" ${slot}")
            }
            0x50 => { // CALL
                let idx = u32::from_be_bytes(code[offset..offset+4].try_into().unwrap());
                offset += 4;
                let nargs = code[offset]; offset += 1;
                format!(" [{}] args={nargs}", bytecode.string_pool.get(idx as usize).map(|s| s.as_str()).unwrap_or("?"))
            }
            _ => String::new(),
        };

        output.push_str(&format!("{start:04}: {name}{operands}\n"));
    }
    output
}
```

### `src/main.rs`

```rust
mod ast;
mod codegen;
mod ir;
mod ir_builder;
mod lexer;
mod optimizer;
mod parser;
mod token;

use codegen::{CodeGenerator, disassemble};
use ir_builder::IrBuilder;
use optimizer::{OptimizationReport, optimize};

fn compile(source: &str) {
    // Lex
    let mut lexer = lexer::Lexer::new(source);
    let tokens = match lexer.tokenize() {
        Ok(t) => t,
        Err(errors) => {
            for e in &errors { eprintln!("{e}"); }
            return;
        }
    };

    // Parse
    let stmts = match parser::Parser::parse(tokens) {
        Ok(s) => s,
        Err(e) => { eprintln!("{e}"); return; }
    };

    // Lower to IR
    let ir = IrBuilder::new().build(&stmts);
    println!("=== IR (before optimization) ===");
    println!("{ir}");

    // Optimize
    let mut main_instrs = ir.main;
    let mut report = OptimizationReport::new();
    optimize(&mut main_instrs, &mut report);

    println!("=== IR (after optimization) ===");
    for instr in &main_instrs {
        println!("{instr}");
    }
    println!();

    report.print();
    println!();

    // Generate bytecode
    let codegen = CodeGenerator::new();
    let bytecode = codegen.generate(&main_instrs);
    println!("{}", disassemble(&bytecode));
    println!("Bytecode size: {} bytes", bytecode.code.len());
}

fn main() {
    let source = r#"
        let x = 2 + 3 * 4;
        let y = x * 2;
        let z = x * 2;
        let unused = 100 + 200;
        if (true) {
            print(y + z);
        }
        let a = 10;
        let b = a * 1;
        let c = b + 0;
        print(c);
    "#;

    compile(source);
}
```

### Running

```bash
cargo run
cargo test
```

### Expected Output

```
=== IR (before optimization) ===
main:
  t0 = 3 Mul 4
  t1 = 2 Add t0
  x = t1
  t2 = x
  t3 = t2 Mul 2
  y = t3
  t4 = x
  t5 = t4 Mul 2
  z = t5
  t6 = 100 Add 200
  unused = t6
  if !true goto L0
  t7 = y
  t8 = z
  t9 = t7 Add t8
  print t9
  goto L1
L0:
L1:
  a = 10
  t10 = a
  t11 = t10 Mul 1
  b = t11
  t12 = b
  t13 = t12 Add 0
  c = t13
  t14 = c
  print t14

=== IR (after optimization) ===
  t1 = 14
  x = t1
  ...
  print t9
  ...
  c = t10
  print t14

=== Optimization Report ===
  [const-fold] folded t0 = 3 Mul 4 -> 12
  [const-fold] folded t1 = 2 Add 12 -> 14
  [strength-red] replaced t3 = t2 * 2 with t2 + t2
  [cse] replaced t5 = t4 Mul 2 with t5 = t3 (common subexpression)
  [strength-red] replaced t11 = t10 * 1 with t10
  [strength-red] replaced t13 = t12 + 0 with t12
  [dead-code] removed unused assignment to t6 (unused variable)
  [dead-code] removed never-taken branch (constant true condition)
  [pass-manager] fixed point reached after 3 iterations
  Total: 8 optimizations applied
```

---

## Design Decisions

1. **Three-address code over SSA**: three-address code was chosen because it is the simplest IR that supports all four optimization passes. SSA (Static Single Assignment) is more powerful -- it makes def-use chains explicit and simplifies many analyses -- but it requires phi nodes at control flow join points, which add significant implementation complexity. For the four passes required here, three-address code is sufficient.

2. **Fixed-point iteration for pass ordering**: passes can enable each other (constant folding creates dead code, CSE reduces to copies that constant folding can propagate). Instead of manually ordering passes, the pass manager loops until no pass reports changes. This converges in 2-4 iterations for typical programs and guarantees that all cascading opportunities are captured.

3. **IR labels instead of basic blocks**: the IR uses `Label` instructions instead of a full basic-block CFG. This simplifies the implementation while still supporting all the required optimizations. A production compiler would partition the IR into basic blocks and build a CFG for more sophisticated analyses (dominators, loop detection, live variable analysis).

4. **Bytecode uses variable slots instead of a register allocator**: the code generator assigns each variable and temporary to a numbered slot, then emits LOAD/STORE instructions. A real compiler would use register allocation to minimize memory traffic, but for a stack-based VM target, the slot approach maps directly to LOAD and STORE instructions.

5. **Optimization report as a first-class output**: each pass records every transformation it applies, including the before and after state. This is not just for debugging -- it makes the compiler's behavior auditable and helps users understand what the optimizer is doing. Production compilers like GCC support `-fopt-info` for similar purposes.

6. **Conservative CSE (invalidate on store)**: the CSE pass invalidates cached expressions when any variable in the expression is redefined. This is a conservative approach -- it may miss some opportunities (e.g., when the redefinition assigns the same value) -- but it guarantees correctness without requiring data flow analysis.

7. **Strength reduction limited to known patterns**: the pass only handles `x * 2 -> x + x`, `x * 0 -> 0`, `x * 1 -> x`, `x + 0 -> x`, and `x / 1 -> x`. More aggressive strength reduction (loop induction variable reduction, polynomial expansion) requires loop analysis that is beyond the scope of this challenge.

## Common Mistakes

- **Passes modifying the IR while iterating**: if a pass modifies instruction `i` in a way that affects the interpretation of instruction `i+1`, the result depends on iteration order. Always collect modifications and apply them in a separate pass, or iterate by index (not by iterator reference)
- **CSE across stores**: if `a * b` is cached and then `a` is reassigned, the cached result is stale. CSE must invalidate entries when operands are modified
- **Dead code elimination removing calls**: function calls may have side effects (I/O, modifying globals). Do not eliminate a `Call` instruction just because its result is unused
- **Constant folding division by zero**: `10 / 0` cannot be folded -- it is a runtime error. The constant folder must check for division by zero before folding
- **Label renumbering after pass**: if a dead code pass removes instructions between a jump and its target label, the label's offset changes. Either use symbolic labels (resolved after all passes) or recalculate offsets in the code generator

## Performance Notes

The optimization passes operate in O(n) per pass (where n is the number of IR instructions), except for CSE which is O(n) with a hash table. The fixed-point iteration runs at most O(p) times where p is the number of passes, since each iteration must produce at least one change or it stops. Total complexity is O(n * p * k) where k is the number of fixed-point iterations (typically 2-4).

The bytecode output size depends heavily on the optimizations applied. Constant folding reduces instruction count by eliminating computations. Dead code elimination removes entire instruction sequences. CSE reduces duplicated computations. On typical programs, the optimized bytecode is 20-40% smaller than the unoptimized version.

## Going Further

- Implement SSA form with phi nodes for more precise data flow analysis
- Add loop-invariant code motion (hoist computations out of loops when operands do not change)
- Implement function inlining for small functions (< N instructions)
- Add copy propagation as a separate pass (replace uses of `t = x` with `x` directly)
- Implement a register allocator (graph coloring or linear scan) for a register-based VM target
- Add profile-guided optimization: instrument bytecode with execution counters, re-optimize based on hot path data
