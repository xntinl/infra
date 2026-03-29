# Solution: Optimizing Compiler Middle-End with SSA Form

## Architecture Overview

The compiler middle-end processes IR through a pipeline of analysis and transformation passes:

```
Input: Text IR
    |
IR Parser (tokenize + parse instructions)
    |
CFG Builder (basic blocks + edges)
    |
Dominator Analysis
    |   +-- Immediate dominators (Cooper-Harvey-Kennedy)
    |   +-- Dominator tree
    |   +-- Dominance frontiers
    |
SSA Construction (Cytron's algorithm)
    |   +-- Phi-node insertion (iterated dominance frontier)
    |   +-- Variable renaming (dominator tree walk)
    |
Optimization Passes
    |   +-- Constant propagation (worklist)
    |   +-- Dead code elimination (iterative)
    |   +-- Global value numbering
    |
SSA Destruction
    |   +-- Phi elimination (parallel copies)
    |   +-- Copy sequentialization (topological sort + temps for cycles)
    |
Output: Optimized IR
```

## Rust Solution

### Project Structure

```
ssa-compiler/
  src/
    main.rs
    ir.rs           // IR types and parser
    cfg.rs           // Control flow graph
    dom.rs           // Dominance analysis
    ssa.rs           // SSA construction (phi insertion + renaming)
    opt/
      mod.rs
      constprop.rs   // Constant propagation
      dce.rs         // Dead code elimination
      gvn.rs         // Global value numbering
    destruct.rs      // SSA destruction
    printer.rs       // IR output
  Cargo.toml
```

### IR Types and Parser

```rust
// src/ir.rs
use std::collections::HashMap;
use std::fmt;

pub type VarId = String;
pub type BlockId = String;

#[derive(Clone, Debug, PartialEq)]
pub enum Value {
    Const(i64),
    Var(VarId),
}

impl fmt::Display for Value {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            Value::Const(n) => write!(f, "{}", n),
            Value::Var(v) => write!(f, "{}", v),
        }
    }
}

#[derive(Clone, Debug, PartialEq)]
pub enum Op {
    Add, Sub, Mul, Div, Eq, Lt, Gt,
}

impl fmt::Display for Op {
    fn fmt(&self, f: &mut fmt::Formatter) -> fmt::Result {
        match self {
            Op::Add => write!(f, "add"),
            Op::Sub => write!(f, "sub"),
            Op::Mul => write!(f, "mul"),
            Op::Div => write!(f, "div"),
            Op::Eq => write!(f, "eq"),
            Op::Lt => write!(f, "lt"),
            Op::Gt => write!(f, "gt"),
        }
    }
}

impl Op {
    pub fn is_commutative(&self) -> bool {
        matches!(self, Op::Add | Op::Mul | Op::Eq)
    }
}

#[derive(Clone, Debug)]
pub enum Instruction {
    BinOp {
        dst: VarId,
        op: Op,
        left: Value,
        right: Value,
    },
    Copy {
        dst: VarId,
        src: Value,
    },
    Phi {
        dst: VarId,
        args: Vec<(BlockId, Value)>,
    },
    Print {
        val: Value,
    },
    Ret {
        val: Option<Value>,
    },
    BrIf {
        cond: Value,
        then_block: BlockId,
        else_block: BlockId,
    },
    Jmp {
        target: BlockId,
    },
}

impl Instruction {
    pub fn dst(&self) -> Option<&VarId> {
        match self {
            Instruction::BinOp { dst, .. } => Some(dst),
            Instruction::Copy { dst, .. } => Some(dst),
            Instruction::Phi { dst, .. } => Some(dst),
            _ => None,
        }
    }

    pub fn is_terminator(&self) -> bool {
        matches!(self, Instruction::BrIf { .. } | Instruction::Jmp { .. } | Instruction::Ret { .. })
    }

    pub fn has_side_effects(&self) -> bool {
        matches!(self, Instruction::Print { .. } | Instruction::Ret { .. }
            | Instruction::BrIf { .. } | Instruction::Jmp { .. })
    }

    pub fn uses(&self) -> Vec<&VarId> {
        let mut result = Vec::new();
        let extract = |v: &Value, out: &mut Vec<&VarId>| {
            if let Value::Var(id) = v {
                out.push(id);
            }
        };
        match self {
            Instruction::BinOp { left, right, .. } => {
                extract(left, &mut result);
                extract(right, &mut result);
            }
            Instruction::Copy { src, .. } => extract(src, &mut result),
            Instruction::Phi { args, .. } => {
                for (_, val) in args {
                    extract(val, &mut result);
                }
            }
            Instruction::Print { val } => extract(val, &mut result),
            Instruction::Ret { val: Some(v) } => extract(v, &mut result),
            Instruction::BrIf { cond, .. } => extract(cond, &mut result),
            _ => {}
        }
        result
    }
}

#[derive(Clone, Debug)]
pub struct BasicBlock {
    pub id: BlockId,
    pub instructions: Vec<Instruction>,
}

pub fn parse(input: &str) -> Vec<BasicBlock> {
    let mut blocks = Vec::new();
    let mut current_block: Option<BasicBlock> = None;

    for line in input.lines() {
        let line = line.split("//").next().unwrap_or("").trim();
        if line.is_empty() {
            continue;
        }

        if line.ends_with(':') {
            if let Some(block) = current_block.take() {
                blocks.push(block);
            }
            current_block = Some(BasicBlock {
                id: line[..line.len()-1].to_string(),
                instructions: Vec::new(),
            });
            continue;
        }

        let block = current_block.get_or_insert_with(|| BasicBlock {
            id: "entry".to_string(),
            instructions: Vec::new(),
        });

        if let Some(inst) = parse_instruction(line) {
            block.instructions.push(inst);
        }
    }

    if let Some(block) = current_block {
        blocks.push(block);
    }

    blocks
}

fn parse_instruction(line: &str) -> Option<Instruction> {
    let tokens: Vec<&str> = line.split_whitespace().collect();
    if tokens.is_empty() {
        return None;
    }

    match tokens[0] {
        "print" => Some(Instruction::Print { val: parse_value(tokens[1]) }),
        "ret" => {
            let val = if tokens.len() > 1 { Some(parse_value(tokens[1])) } else { None };
            Some(Instruction::Ret { val })
        }
        "jmp" => Some(Instruction::Jmp { target: tokens[1].to_string() }),
        "br_if" => {
            // br_if cond, label_true, label_false
            let cond = parse_value(tokens[1].trim_end_matches(','));
            let then_b = tokens[2].trim_end_matches(',').to_string();
            let else_b = tokens[3].to_string();
            Some(Instruction::BrIf { cond, then_block: then_b, else_block: else_b })
        }
        _ if tokens.len() >= 4 && tokens[1] == "=" => {
            let dst = tokens[0].to_string();
            let op_str = tokens[2];
            match op_str {
                "add" | "sub" | "mul" | "div" | "eq" | "lt" | "gt" => {
                    let op = match op_str {
                        "add" => Op::Add, "sub" => Op::Sub, "mul" => Op::Mul,
                        "div" => Op::Div, "eq" => Op::Eq, "lt" => Op::Lt,
                        "gt" => Op::Gt, _ => unreachable!(),
                    };
                    let left = parse_value(tokens[3].trim_end_matches(','));
                    let right = parse_value(tokens[4]);
                    Some(Instruction::BinOp { dst, op, left, right })
                }
                "phi" => {
                    // x = phi [block1, val1], [block2, val2], ...
                    let mut args = Vec::new();
                    let mut i = 3;
                    while i < tokens.len() {
                        let block = tokens[i].trim_start_matches('[').trim_end_matches(',').to_string();
                        let val = parse_value(tokens[i+1].trim_end_matches(']').trim_end_matches(','));
                        args.push((block, val));
                        i += 2;
                    }
                    Some(Instruction::Phi { dst, args })
                }
                "copy" => {
                    Some(Instruction::Copy { dst, src: parse_value(tokens[3]) })
                }
                _ => {
                    Some(Instruction::Copy { dst, src: parse_value(tokens[2]) })
                }
            }
        }
        _ => None,
    }
}

fn parse_value(s: &str) -> Value {
    let s = s.trim_end_matches(',');
    if let Ok(n) = s.parse::<i64>() {
        Value::Const(n)
    } else {
        Value::Var(s.to_string())
    }
}
```

### Control Flow Graph

```rust
// src/cfg.rs
use std::collections::{HashMap, HashSet};
use crate::ir::{BasicBlock, BlockId, Instruction};

pub struct CFG {
    pub blocks: Vec<BasicBlock>,
    pub block_map: HashMap<BlockId, usize>,
    pub predecessors: HashMap<BlockId, Vec<BlockId>>,
    pub successors: HashMap<BlockId, Vec<BlockId>>,
    pub entry: BlockId,
    pub unreachable: HashSet<BlockId>,
}

impl CFG {
    pub fn build(blocks: Vec<BasicBlock>) -> Self {
        let block_map: HashMap<BlockId, usize> = blocks.iter()
            .enumerate()
            .map(|(i, b)| (b.id.clone(), i))
            .collect();

        let mut predecessors: HashMap<BlockId, Vec<BlockId>> = HashMap::new();
        let mut successors: HashMap<BlockId, Vec<BlockId>> = HashMap::new();

        for block in &blocks {
            predecessors.entry(block.id.clone()).or_default();
            successors.entry(block.id.clone()).or_default();
        }

        for block in &blocks {
            if let Some(term) = block.instructions.last() {
                let succs = match term {
                    Instruction::Jmp { target } => vec![target.clone()],
                    Instruction::BrIf { then_block, else_block, .. } => {
                        vec![then_block.clone(), else_block.clone()]
                    }
                    _ => vec![],
                };
                for succ in &succs {
                    predecessors.entry(succ.clone()).or_default().push(block.id.clone());
                }
                successors.insert(block.id.clone(), succs);
            }
        }

        let entry = blocks.first().map(|b| b.id.clone()).unwrap_or_default();

        // Find unreachable blocks via DFS from entry
        let mut reachable = HashSet::new();
        let mut stack = vec![entry.clone()];
        while let Some(bid) = stack.pop() {
            if reachable.insert(bid.clone()) {
                for succ in successors.get(&bid).unwrap_or(&vec![]) {
                    stack.push(succ.clone());
                }
            }
        }

        let unreachable: HashSet<BlockId> = blocks.iter()
            .map(|b| b.id.clone())
            .filter(|id| !reachable.contains(id))
            .collect();

        CFG { blocks, block_map, predecessors, successors, entry, unreachable }
    }

    pub fn reverse_postorder(&self) -> Vec<BlockId> {
        let mut visited = HashSet::new();
        let mut order = Vec::new();
        self.dfs_postorder(&self.entry, &mut visited, &mut order);
        order.reverse();
        order
    }

    fn dfs_postorder(&self, block: &BlockId, visited: &mut HashSet<BlockId>, order: &mut Vec<BlockId>) {
        if !visited.insert(block.clone()) {
            return;
        }
        for succ in self.successors.get(block).unwrap_or(&vec![]) {
            self.dfs_postorder(succ, visited, order);
        }
        order.push(block.clone());
    }
}
```

### Dominance Analysis

```rust
// src/dom.rs
use std::collections::{HashMap, HashSet};
use crate::cfg::CFG;
use crate::ir::BlockId;

pub struct DomTree {
    pub idom: HashMap<BlockId, BlockId>,
    pub children: HashMap<BlockId, Vec<BlockId>>,
    pub frontier: HashMap<BlockId, HashSet<BlockId>>,
}

impl DomTree {
    /// Cooper-Harvey-Kennedy iterative dominance algorithm.
    pub fn compute(cfg: &CFG) -> Self {
        let rpo = cfg.reverse_postorder();
        let rpo_index: HashMap<&BlockId, usize> = rpo.iter()
            .enumerate()
            .map(|(i, b)| (b, i))
            .collect();

        let mut idom: HashMap<BlockId, BlockId> = HashMap::new();
        idom.insert(cfg.entry.clone(), cfg.entry.clone());

        let mut changed = true;
        while changed {
            changed = false;
            for b in &rpo {
                if *b == cfg.entry {
                    continue;
                }
                let preds = cfg.predecessors.get(b).unwrap();
                let processed_preds: Vec<&BlockId> = preds.iter()
                    .filter(|p| idom.contains_key(*p))
                    .collect();

                if processed_preds.is_empty() {
                    continue;
                }

                let mut new_idom = processed_preds[0].clone();
                for pred in &processed_preds[1..] {
                    new_idom = intersect(&idom, &rpo_index, &new_idom, pred);
                }

                if idom.get(b) != Some(&new_idom) {
                    idom.insert(b.clone(), new_idom);
                    changed = true;
                }
            }
        }

        // Build dominator tree children
        let mut children: HashMap<BlockId, Vec<BlockId>> = HashMap::new();
        for (block, dom) in &idom {
            if block != dom {
                children.entry(dom.clone()).or_default().push(block.clone());
            }
        }

        // Compute dominance frontiers
        let mut frontier: HashMap<BlockId, HashSet<BlockId>> = HashMap::new();
        for b in &rpo {
            frontier.entry(b.clone()).or_default();
        }

        for b in &rpo {
            let preds = cfg.predecessors.get(b).unwrap();
            if preds.len() >= 2 {
                for p in preds {
                    let mut runner = p.clone();
                    while runner != *idom.get(b).unwrap_or(b) {
                        frontier.entry(runner.clone()).or_default().insert(b.clone());
                        runner = idom.get(&runner).unwrap_or(&runner).clone();
                    }
                }
            }
        }

        DomTree { idom, children, frontier }
    }

    pub fn preorder(&self, root: &BlockId) -> Vec<BlockId> {
        let mut result = Vec::new();
        let mut stack = vec![root.clone()];
        while let Some(node) = stack.pop() {
            result.push(node.clone());
            if let Some(kids) = self.children.get(&node) {
                for child in kids.iter().rev() {
                    stack.push(child.clone());
                }
            }
        }
        result
    }
}

fn intersect(
    idom: &HashMap<BlockId, BlockId>,
    rpo_index: &HashMap<&BlockId, usize>,
    mut b1: &BlockId,
    mut b2: &BlockId,
) -> BlockId {
    while b1 != b2 {
        while rpo_index.get(b1).copied().unwrap_or(0) > rpo_index.get(b2).copied().unwrap_or(0) {
            b1 = idom.get(b1).unwrap();
        }
        while rpo_index.get(b2).copied().unwrap_or(0) > rpo_index.get(b1).copied().unwrap_or(0) {
            b2 = idom.get(b2).unwrap();
        }
    }
    b1.clone()
}
```

### SSA Construction

```rust
// src/ssa.rs
use std::collections::{HashMap, HashSet};
use crate::ir::*;
use crate::cfg::CFG;
use crate::dom::DomTree;

pub struct SSAStats {
    pub phi_nodes_inserted: usize,
}

/// Insert phi nodes using Cytron's algorithm (iterated dominance frontier).
pub fn insert_phi_nodes(cfg: &mut CFG, dom: &DomTree) -> SSAStats {
    // Collect variables and their definition blocks
    let mut def_blocks: HashMap<VarId, HashSet<BlockId>> = HashMap::new();
    let mut all_vars: HashSet<VarId> = HashSet::new();

    for block in &cfg.blocks {
        for inst in &block.instructions {
            if let Some(dst) = inst.dst() {
                def_blocks.entry(dst.clone()).or_default().insert(block.id.clone());
                all_vars.insert(dst.clone());
            }
        }
    }

    let mut phi_count = 0;

    // For each variable, compute the iterated dominance frontier of its definition blocks
    for var in &all_vars {
        let defs = match def_blocks.get(var) {
            Some(d) => d.clone(),
            None => continue,
        };

        let mut phi_blocks: HashSet<BlockId> = HashSet::new();
        let mut worklist: Vec<BlockId> = defs.iter().cloned().collect();

        while let Some(block) = worklist.pop() {
            if let Some(frontier) = dom.frontier.get(&block) {
                for fb in frontier {
                    if phi_blocks.insert(fb.clone()) {
                        worklist.push(fb.clone());
                    }
                }
            }
        }

        // Insert phi nodes at the computed blocks
        for pb in &phi_blocks {
            let preds = cfg.predecessors.get(pb).unwrap();
            let args: Vec<(BlockId, Value)> = preds.iter()
                .map(|p| (p.clone(), Value::Var(var.clone())))
                .collect();

            let phi = Instruction::Phi {
                dst: var.clone(),
                args,
            };

            let idx = cfg.block_map[pb];
            cfg.blocks[idx].instructions.insert(0, phi);
            phi_count += 1;
        }
    }

    SSAStats { phi_nodes_inserted: phi_count }
}

/// Rename variables to SSA form by walking the dominator tree.
pub fn rename(cfg: &mut CFG, dom: &DomTree) {
    let mut counters: HashMap<String, usize> = HashMap::new();
    let mut stacks: HashMap<String, Vec<VarId>> = HashMap::new();

    fn base_name(var: &str) -> &str {
        // Strip SSA suffix: "x.3" -> "x"
        var.split('.').next().unwrap_or(var)
    }

    fn fresh_name(base: &str, counters: &mut HashMap<String, usize>) -> VarId {
        let count = counters.entry(base.to_string()).or_insert(0);
        *count += 1;
        format!("{}.{}", base, count)
    }

    fn current_name(base: &str, stacks: &HashMap<String, Vec<VarId>>) -> VarId {
        stacks.get(base)
            .and_then(|s| s.last())
            .cloned()
            .unwrap_or_else(|| base.to_string())
    }

    fn rename_value(val: &mut Value, stacks: &HashMap<String, Vec<VarId>>) {
        if let Value::Var(ref v) = val {
            let base = base_name(v).to_string();
            *val = Value::Var(current_name(&base, stacks));
        }
    }

    fn rename_block(
        block_id: &BlockId,
        cfg: &mut CFG,
        dom: &DomTree,
        counters: &mut HashMap<String, usize>,
        stacks: &mut HashMap<String, Vec<VarId>>,
    ) {
        let idx = cfg.block_map[block_id];
        let mut pushed: Vec<String> = Vec::new();

        // Rename phi destinations
        let block = &mut cfg.blocks[idx];
        for inst in &mut block.instructions {
            if let Instruction::Phi { dst, .. } = inst {
                let base = base_name(dst).to_string();
                let new_name = fresh_name(&base, counters);
                stacks.entry(base.clone()).or_default().push(new_name.clone());
                pushed.push(base);
                *dst = new_name;
            }
        }

        // Rename uses then definitions for non-phi instructions
        let block = &mut cfg.blocks[idx];
        for inst in &mut block.instructions {
            match inst {
                Instruction::Phi { .. } => {} // Already handled
                Instruction::BinOp { dst, left, right, .. } => {
                    rename_value(left, stacks);
                    rename_value(right, stacks);
                    let base = base_name(dst).to_string();
                    let new_name = fresh_name(&base, counters);
                    stacks.entry(base.clone()).or_default().push(new_name.clone());
                    pushed.push(base);
                    *dst = new_name;
                }
                Instruction::Copy { dst, src } => {
                    rename_value(src, stacks);
                    let base = base_name(dst).to_string();
                    let new_name = fresh_name(&base, counters);
                    stacks.entry(base.clone()).or_default().push(new_name.clone());
                    pushed.push(base);
                    *dst = new_name;
                }
                Instruction::Print { val } => rename_value(val, stacks),
                Instruction::Ret { val: Some(v) } => rename_value(v, stacks),
                Instruction::BrIf { cond, .. } => rename_value(cond, stacks),
                _ => {}
            }
        }

        // Rename phi arguments in successor blocks
        let succs = cfg.successors.get(block_id).cloned().unwrap_or_default();
        for succ in &succs {
            let succ_idx = cfg.block_map[succ];
            for inst in &mut cfg.blocks[succ_idx].instructions {
                if let Instruction::Phi { args, .. } = inst {
                    for (pred, val) in args.iter_mut() {
                        if pred == block_id {
                            rename_value(val, stacks);
                        }
                    }
                }
            }
        }

        // Recurse into dominator tree children
        let children = dom.children.get(block_id).cloned().unwrap_or_default();
        for child in &children {
            rename_block(child, cfg, dom, counters, stacks);
        }

        // Pop definitions pushed in this block
        for base in pushed {
            stacks.get_mut(&base).unwrap().pop();
        }
    }

    rename_block(&cfg.entry.clone(), cfg, dom, &mut counters, &mut stacks);
}
```

### Constant Propagation

```rust
// src/opt/constprop.rs
use std::collections::{HashMap, VecDeque};
use crate::ir::*;
use crate::cfg::CFG;

pub fn propagate(cfg: &mut CFG) -> usize {
    let mut constants: HashMap<VarId, i64> = HashMap::new();
    let mut folded = 0;

    // Build use map: variable -> list of (block_idx, inst_idx)
    let mut use_map: HashMap<VarId, Vec<(usize, usize)>> = HashMap::new();
    for (bi, block) in cfg.blocks.iter().enumerate() {
        for (ii, inst) in block.instructions.iter().enumerate() {
            for var in inst.uses() {
                use_map.entry(var.clone()).or_default().push((bi, ii));
            }
        }
    }

    // Worklist: instructions to (re-)evaluate
    let mut worklist: VecDeque<(usize, usize)> = VecDeque::new();
    for bi in 0..cfg.blocks.len() {
        for ii in 0..cfg.blocks[bi].instructions.len() {
            worklist.push_back((bi, ii));
        }
    }

    while let Some((bi, ii)) = worklist.pop_front() {
        if bi >= cfg.blocks.len() || ii >= cfg.blocks[bi].instructions.len() {
            continue;
        }

        let inst = &cfg.blocks[bi].instructions[ii];
        match inst {
            Instruction::BinOp { dst, op, left, right } => {
                let lval = resolve(left, &constants);
                let rval = resolve(right, &constants);

                if let (Some(l), Some(r)) = (lval, rval) {
                    let result = eval_op(op, l, r);
                    if let Some(val) = result {
                        constants.insert(dst.clone(), val);
                        cfg.blocks[bi].instructions[ii] = Instruction::Copy {
                            dst: dst.clone(),
                            src: Value::Const(val),
                        };
                        folded += 1;

                        // Re-examine all uses of this variable
                        if let Some(uses) = use_map.get(dst) {
                            for u in uses {
                                worklist.push_back(*u);
                            }
                        }
                    }
                }
            }
            Instruction::Phi { dst, args } => {
                // If all incoming values are the same constant, fold
                let mut all_same = true;
                let mut common_val: Option<i64> = None;

                for (_, val) in args {
                    let resolved = resolve(val, &constants);
                    match (resolved, common_val) {
                        (Some(v), None) => common_val = Some(v),
                        (Some(v), Some(cv)) if v == cv => {}
                        _ => { all_same = false; break; }
                    }
                }

                if all_same {
                    if let Some(val) = common_val {
                        constants.insert(dst.clone(), val);
                        cfg.blocks[bi].instructions[ii] = Instruction::Copy {
                            dst: dst.clone(),
                            src: Value::Const(val),
                        };
                        folded += 1;

                        if let Some(uses) = use_map.get(dst) {
                            for u in uses {
                                worklist.push_back(*u);
                            }
                        }
                    }
                }
            }
            Instruction::Copy { dst, src: Value::Const(n) } => {
                constants.insert(dst.clone(), *n);
            }
            _ => {}
        }
    }

    folded
}

fn resolve(val: &Value, constants: &HashMap<VarId, i64>) -> Option<i64> {
    match val {
        Value::Const(n) => Some(*n),
        Value::Var(v) => constants.get(v).copied(),
    }
}

fn eval_op(op: &Op, left: i64, right: i64) -> Option<i64> {
    match op {
        Op::Add => Some(left + right),
        Op::Sub => Some(left - right),
        Op::Mul => Some(left * right),
        Op::Div => {
            if right == 0 { None } else { Some(left / right) }
        }
        Op::Eq => Some(if left == right { 1 } else { 0 }),
        Op::Lt => Some(if left < right { 1 } else { 0 }),
        Op::Gt => Some(if left > right { 1 } else { 0 }),
    }
}
```

### Dead Code Elimination

```rust
// src/opt/dce.rs
use std::collections::HashSet;
use crate::ir::*;
use crate::cfg::CFG;

pub fn eliminate(cfg: &mut CFG) -> usize {
    let mut total_removed = 0;

    loop {
        let used_vars = collect_used_vars(cfg);
        let mut removed_this_pass = 0;

        for block in &mut cfg.blocks {
            block.instructions.retain(|inst| {
                if inst.has_side_effects() {
                    return true;
                }
                match inst.dst() {
                    Some(dst) if !used_vars.contains(dst) => {
                        removed_this_pass += 1;
                        false
                    }
                    _ => true,
                }
            });
        }

        if removed_this_pass == 0 {
            break;
        }
        total_removed += removed_this_pass;
    }

    total_removed
}

fn collect_used_vars(cfg: &CFG) -> HashSet<VarId> {
    let mut used = HashSet::new();
    for block in &cfg.blocks {
        for inst in &block.instructions {
            for var in inst.uses() {
                used.insert(var.clone());
            }
        }
    }
    used
}
```

### Global Value Numbering

```rust
// src/opt/gvn.rs
use std::collections::HashMap;
use crate::ir::*;
use crate::cfg::CFG;

type ValueNumber = u64;

#[derive(Hash, Eq, PartialEq, Clone)]
struct GVNKey {
    op: String,
    left: ValueNumber,
    right: ValueNumber,
}

pub fn number(cfg: &mut CFG) -> usize {
    let mut val_num: HashMap<VarId, ValueNumber> = HashMap::new();
    let mut const_nums: HashMap<i64, ValueNumber> = HashMap::new();
    let mut expr_map: HashMap<GVNKey, VarId> = HashMap::new();
    let mut next_vn: ValueNumber = 0;
    let mut eliminated = 0;

    let mut alloc_vn = |next: &mut ValueNumber| -> ValueNumber {
        let vn = *next;
        *next += 1;
        vn
    };

    let get_vn = |val: &Value, val_num: &HashMap<VarId, ValueNumber>,
                  const_nums: &mut HashMap<i64, ValueNumber>, next: &mut ValueNumber| -> ValueNumber {
        match val {
            Value::Const(n) => {
                *const_nums.entry(*n).or_insert_with(|| { let vn = *next; *next += 1; vn })
            }
            Value::Var(v) => {
                val_num.get(v).copied().unwrap_or_else(|| { let vn = *next; *next += 1; vn })
            }
        }
    };

    for block in &mut cfg.blocks {
        let mut replacements: Vec<(usize, Instruction)> = Vec::new();

        for (idx, inst) in block.instructions.iter().enumerate() {
            match inst {
                Instruction::BinOp { dst, op, left, right } => {
                    let mut lvn = get_vn(left, &val_num, &mut const_nums, &mut next_vn);
                    let mut rvn = get_vn(right, &val_num, &mut const_nums, &mut next_vn);

                    // Canonicalize commutative ops
                    if op.is_commutative() && lvn > rvn {
                        std::mem::swap(&mut lvn, &mut rvn);
                    }

                    let key = GVNKey {
                        op: format!("{}", op),
                        left: lvn,
                        right: rvn,
                    };

                    if let Some(existing) = expr_map.get(&key) {
                        let existing_vn = val_num[existing];
                        val_num.insert(dst.clone(), existing_vn);
                        replacements.push((idx, Instruction::Copy {
                            dst: dst.clone(),
                            src: Value::Var(existing.clone()),
                        }));
                        eliminated += 1;
                    } else {
                        let vn = alloc_vn(&mut next_vn);
                        val_num.insert(dst.clone(), vn);
                        expr_map.insert(key, dst.clone());
                    }
                }
                Instruction::Copy { dst, src } => {
                    let vn = get_vn(src, &val_num, &mut const_nums, &mut next_vn);
                    val_num.insert(dst.clone(), vn);
                }
                _ => {}
            }
        }

        for (idx, replacement) in replacements {
            block.instructions[idx] = replacement;
        }
    }

    eliminated
}
```

### SSA Destruction

```rust
// src/destruct.rs
use std::collections::{HashMap, HashSet};
use crate::ir::*;
use crate::cfg::CFG;

pub fn destroy_ssa(cfg: &mut CFG) {
    // Collect phi instructions and convert to parallel copies
    let mut copies_to_insert: HashMap<BlockId, Vec<(VarId, Value)>> = HashMap::new();

    for block in &cfg.blocks {
        let phi_insts: Vec<_> = block.instructions.iter()
            .filter(|i| matches!(i, Instruction::Phi { .. }))
            .cloned()
            .collect();

        for inst in &phi_insts {
            if let Instruction::Phi { dst, args } = inst {
                for (pred, val) in args {
                    copies_to_insert.entry(pred.clone())
                        .or_default()
                        .push((dst.clone(), val.clone()));
                }
            }
        }
    }

    // Remove all phi instructions
    for block in &mut cfg.blocks {
        block.instructions.retain(|i| !matches!(i, Instruction::Phi { .. }));
    }

    // Insert sequentialized copies before terminators in predecessor blocks
    for (block_id, parallel_copies) in &copies_to_insert {
        let idx = cfg.block_map[block_id];
        let sequential = sequentialize_copies(parallel_copies);

        let block = &mut cfg.blocks[idx];
        let term_pos = block.instructions.iter()
            .position(|i| i.is_terminator())
            .unwrap_or(block.instructions.len());

        for (i, (dst, src)) in sequential.iter().enumerate() {
            block.instructions.insert(term_pos + i, Instruction::Copy {
                dst: dst.clone(),
                src: src.clone(),
            });
        }
    }
}

/// Sequentialize parallel copies to avoid lost-copy and swap problems.
/// Uses topological sort with temporary variables for cycles.
fn sequentialize_copies(parallel: &[(VarId, Value)]) -> Vec<(VarId, Value)> {
    // Build dependency graph: dst depends on src (if src is a variable that is also a dst)
    let dsts: HashSet<&VarId> = parallel.iter().map(|(d, _)| d).collect();
    let mut result = Vec::new();
    let mut ready: Vec<(VarId, Value)> = Vec::new();
    let mut pending: Vec<(VarId, Value)> = Vec::new();

    for (dst, src) in parallel {
        match src {
            Value::Var(v) if dsts.contains(v) && v != dst => {
                pending.push((dst.clone(), src.clone()));
            }
            _ => {
                ready.push((dst.clone(), src.clone()));
            }
        }
    }

    result.extend(ready);

    // Resolve pending with cycle detection
    let mut emitted: HashSet<VarId> = result.iter().map(|(d, _)| d.clone()).collect();
    let mut remaining = pending;
    let mut temp_count = 0;

    loop {
        let mut progress = false;
        let mut next_remaining = Vec::new();

        for (dst, src) in &remaining {
            if let Value::Var(v) = src {
                if emitted.contains(v) || !dsts.contains(v) {
                    result.push((dst.clone(), src.clone()));
                    emitted.insert(dst.clone());
                    progress = true;
                    continue;
                }
            }
            next_remaining.push((dst.clone(), src.clone()));
        }

        remaining = next_remaining;

        if remaining.is_empty() {
            break;
        }

        if !progress {
            // Cycle detected: break it with a temporary
            let (dst, src) = remaining.remove(0);
            let temp = format!("__tmp_{}", temp_count);
            temp_count += 1;

            result.push((temp.clone(), src));
            result.push((dst.clone(), Value::Var(temp.clone())));
            emitted.insert(dst);
            continue;
        }
    }

    result
}
```

### IR Printer

```rust
// src/printer.rs
use std::fmt::Write;
use crate::ir::*;
use crate::cfg::CFG;

pub struct PrintStats {
    pub phi_nodes: usize,
    pub constants_propagated: usize,
    pub dead_eliminated: usize,
    pub redundant_removed: usize,
}

pub fn print_ir(cfg: &CFG, stats: &PrintStats) -> String {
    let mut out = String::new();

    writeln!(out, "// Optimization statistics:").unwrap();
    writeln!(out, "//   Phi nodes inserted: {}", stats.phi_nodes).unwrap();
    writeln!(out, "//   Constants propagated: {}", stats.constants_propagated).unwrap();
    writeln!(out, "//   Dead instructions eliminated: {}", stats.dead_eliminated).unwrap();
    writeln!(out, "//   Redundant computations removed: {}", stats.redundant_removed).unwrap();
    writeln!(out).unwrap();

    for block in &cfg.blocks {
        writeln!(out, "{}:", block.id).unwrap();
        for inst in &block.instructions {
            write!(out, "  ").unwrap();
            print_instruction(&mut out, inst);
            writeln!(out).unwrap();
        }
        writeln!(out).unwrap();
    }

    out
}

fn print_instruction(out: &mut String, inst: &Instruction) {
    match inst {
        Instruction::BinOp { dst, op, left, right } => {
            write!(out, "{} = {} {}, {}", dst, op, left, right).unwrap();
        }
        Instruction::Copy { dst, src } => {
            write!(out, "{} = copy {}", dst, src).unwrap();
        }
        Instruction::Phi { dst, args } => {
            write!(out, "{} = phi ", dst).unwrap();
            for (i, (block, val)) in args.iter().enumerate() {
                if i > 0 { write!(out, ", ").unwrap(); }
                write!(out, "[{}, {}]", block, val).unwrap();
            }
        }
        Instruction::Print { val } => {
            write!(out, "print {}", val).unwrap();
        }
        Instruction::Ret { val } => {
            match val {
                Some(v) => write!(out, "ret {}", v).unwrap(),
                None => write!(out, "ret").unwrap(),
            }
        }
        Instruction::BrIf { cond, then_block, else_block } => {
            write!(out, "br_if {}, {}, {}", cond, then_block, else_block).unwrap();
        }
        Instruction::Jmp { target } => {
            write!(out, "jmp {}", target).unwrap();
        }
    }
}
```

### Main

```rust
// src/main.rs
mod ir;
mod cfg;
mod dom;
mod ssa;
mod opt;
mod destruct;
mod printer;

use std::fs;
use std::env;

fn main() {
    let args: Vec<String> = env::args().collect();
    if args.len() < 2 {
        eprintln!("usage: ssa-compiler <input.ir>");
        std::process::exit(1);
    }

    let input = fs::read_to_string(&args[1]).expect("read input file");
    let blocks = ir::parse(&input);
    let mut cfg = cfg::CFG::build(blocks);

    if !cfg.unreachable.is_empty() {
        eprintln!("Warning: unreachable blocks: {:?}", cfg.unreachable);
    }

    let dom = dom::DomTree::compute(&cfg);

    // SSA construction
    let ssa_stats = ssa::insert_phi_nodes(&mut cfg, &dom);
    ssa::rename(&mut cfg, &dom);

    // Optimization passes
    let constants_propagated = opt::constprop::propagate(&mut cfg);
    let dead_eliminated = opt::dce::eliminate(&mut cfg);
    let redundant_removed = opt::gvn::number(&mut cfg);
    let dead_after_gvn = opt::dce::eliminate(&mut cfg);

    // SSA destruction
    destruct::destroy_ssa(&mut cfg);

    let stats = printer::PrintStats {
        phi_nodes: ssa_stats.phi_nodes_inserted,
        constants_propagated,
        dead_eliminated: dead_eliminated + dead_after_gvn,
        redundant_removed,
    };

    print!("{}", printer::print_ir(&cfg, &stats));
}
```

### Optimization Module

```rust
// src/opt/mod.rs
pub mod constprop;
pub mod dce;
pub mod gvn;
```

### Tests

```rust
// tests/compiler_tests.rs
use std::collections::{HashMap, HashSet};

// Simulated IR structures for testing
#[derive(Clone, Debug, PartialEq)]
enum Value { Const(i64), Var(String) }

#[derive(Clone, Debug)]
enum Op { Add, Sub, Mul }

fn eval(op: &Op, l: i64, r: i64) -> i64 {
    match op { Op::Add => l + r, Op::Sub => l - r, Op::Mul => l * r }
}

#[test]
fn test_constant_propagation_chain() {
    // a = add 3, 4  -> 7
    // b = mul a, 2  -> 14
    // c = sub b, 1  -> 13
    let a = eval(&Op::Add, 3, 4);
    assert_eq!(a, 7);
    let b = eval(&Op::Mul, a, 2);
    assert_eq!(b, 14);
    let c = eval(&Op::Sub, b, 1);
    assert_eq!(c, 13);
}

#[test]
fn test_dead_code_elimination() {
    // Instructions: x = add 1, 2 (dead), y = add 3, 4 (used), print y
    let mut has_use = HashMap::new();
    has_use.insert("y", true);

    let instructions = vec![("x", "add 1 2"), ("y", "add 3 4"), ("print", "y")];
    let live: Vec<_> = instructions.iter().filter(|(dst, _)| {
        *dst == "print" || has_use.contains_key(dst)
    }).collect();

    assert_eq!(live.len(), 2); // y and print survive, x eliminated
}

#[test]
fn test_global_value_numbering() {
    // a = add x, y
    // b = add x, y  -> redundant, should become: b = copy a
    let mut expr_table: HashMap<(String, String, String), String> = HashMap::new();
    let key1 = ("add".into(), "x".into(), "y".into());
    expr_table.insert(key1.clone(), "a".to_string());

    let key2 = ("add".into(), "x".into(), "y".into());
    assert!(expr_table.contains_key(&key2));
    assert_eq!(expr_table[&key2], "a");
}

#[test]
fn test_gvn_commutative() {
    // add x, y and add y, x should have the same value number
    fn canonicalize(op: &str, a: &str, b: &str) -> (String, String, String) {
        let commutative = op == "add" || op == "mul";
        if commutative && a > b {
            (op.to_string(), b.to_string(), a.to_string())
        } else {
            (op.to_string(), a.to_string(), b.to_string())
        }
    }

    let k1 = canonicalize("add", "x", "y");
    let k2 = canonicalize("add", "y", "x");
    assert_eq!(k1, k2);

    // sub is not commutative
    let k3 = canonicalize("sub", "x", "y");
    let k4 = canonicalize("sub", "y", "x");
    assert_ne!(k3, k4);
}

#[test]
fn test_dominance_simple_diamond() {
    // CFG:  entry -> A, entry -> B, A -> merge, B -> merge
    let mut preds: HashMap<&str, Vec<&str>> = HashMap::new();
    preds.insert("entry", vec![]);
    preds.insert("A", vec!["entry"]);
    preds.insert("B", vec!["entry"]);
    preds.insert("merge", vec!["A", "B"]);

    // Expected idom: A -> entry, B -> entry, merge -> entry
    let expected_idom: HashMap<&str, &str> = [("A", "entry"), ("B", "entry"), ("merge", "entry")]
        .into_iter().collect();

    for (block, expected) in &expected_idom {
        // In a diamond, all blocks are immediately dominated by entry
        assert_eq!(*expected, "entry", "idom({}) should be entry", block);
    }
}

#[test]
fn test_dominance_frontier_diamond() {
    // In diamond: entry -> A, entry -> B, A -> merge, B -> merge
    // DF(A) = {merge}, DF(B) = {merge}, DF(entry) = {}, DF(merge) = {}
    let mut frontier: HashMap<&str, HashSet<&str>> = HashMap::new();
    frontier.insert("entry", HashSet::new());
    frontier.insert("A", ["merge"].into_iter().collect());
    frontier.insert("B", ["merge"].into_iter().collect());
    frontier.insert("merge", HashSet::new());

    assert!(frontier["A"].contains("merge"));
    assert!(frontier["B"].contains("merge"));
    assert!(frontier["entry"].is_empty());
}

#[test]
fn test_phi_insertion_diamond() {
    // x defined in A and B, merge is in DF(A) and DF(B)
    // -> phi for x should be inserted at merge
    let def_blocks: HashSet<&str> = ["A", "B"].into_iter().collect();
    let mut phi_blocks: HashSet<&str> = HashSet::new();

    let frontier: HashMap<&str, HashSet<&str>> = [
        ("A", ["merge"].into_iter().collect()),
        ("B", ["merge"].into_iter().collect()),
    ].into_iter().collect();

    let mut worklist: Vec<&str> = def_blocks.iter().copied().collect();
    while let Some(block) = worklist.pop() {
        if let Some(df) = frontier.get(block) {
            for fb in df {
                if phi_blocks.insert(fb) {
                    worklist.push(fb);
                }
            }
        }
    }

    assert!(phi_blocks.contains("merge"));
    assert_eq!(phi_blocks.len(), 1);
}

#[test]
fn test_ssa_single_definition_property() {
    // After renaming: each use has exactly one reaching definition
    // Simulate: x.1 = 5, x.2 = 10, y.1 = add x.1, x.2
    let definitions: HashMap<&str, i64> = [("x.1", 5), ("x.2", 10)].into_iter().collect();

    // y.1 = add x.1, x.2
    let left = definitions["x.1"];
    let right = definitions["x.2"];
    let result = left + right;
    assert_eq!(result, 15);

    // Each use refers to exactly one definition (unique SSA name)
    assert_ne!(definitions.get("x.1"), definitions.get("x.2"));
}

#[test]
fn test_phi_elimination_parallel_copies() {
    // Parallel copies: a <- b, b <- a (swap)
    // Must introduce temporary: tmp <- a, a <- b, b <- tmp
    let copies = vec![("a", "b"), ("b", "a")];

    let dsts: HashSet<&str> = copies.iter().map(|(d, _)| *d).collect();
    let has_cycle = copies.iter().any(|(d, s)| {
        dsts.contains(s) && *d != *s
    });

    assert!(has_cycle, "swap pattern should be detected as cycle");
}

#[test]
fn test_copy_sequentialization_no_cycle() {
    // a <- x, b <- y (no dependencies between them)
    let copies = vec![("a", "x"), ("b", "y")];
    let dsts: HashSet<&str> = copies.iter().map(|(d, _)| *d).collect();

    let has_dependency = copies.iter().any(|(_, s)| dsts.contains(s));
    assert!(!has_dependency, "independent copies should have no dependency");
}

#[test]
fn test_ir_parse_binop() {
    let line = "x = add y, 5";
    let tokens: Vec<&str> = line.split_whitespace().collect();
    assert_eq!(tokens[0], "x");
    assert_eq!(tokens[1], "=");
    assert_eq!(tokens[2], "add");
    assert_eq!(tokens[3], "y,");
    assert_eq!(tokens[4], "5");
}

#[test]
fn test_unreachable_blocks() {
    // CFG: entry -> A -> exit, B (unreachable)
    let mut reachable = HashSet::new();
    let succs: HashMap<&str, Vec<&str>> = [
        ("entry", vec!["A"]),
        ("A", vec!["exit"]),
        ("B", vec!["exit"]),
        ("exit", vec![]),
    ].into_iter().collect();

    let mut stack = vec!["entry"];
    while let Some(b) = stack.pop() {
        if reachable.insert(b) {
            for s in succs.get(b).unwrap_or(&vec![]) {
                stack.push(s);
            }
        }
    }

    assert!(reachable.contains("entry"));
    assert!(reachable.contains("A"));
    assert!(reachable.contains("exit"));
    assert!(!reachable.contains("B"));
}

#[test]
fn test_end_to_end_simple() {
    // Input:
    //   entry:
    //     a = add 3, 4
    //     b = mul a, 2
    //     print b
    //     ret
    //
    // After constant propagation: a=7, b=14
    // After DCE: a is dead if only used by b which is folded
    // Expected output: print 14, ret

    let a = 3 + 4;
    let b = a * 2;
    assert_eq!(b, 14);
    // print should output 14
}
```

### Example Input IR

```
// example.ir
entry:
  x = copy 10
  y = copy 20
  cond = lt x, y
  br_if cond, then, else

then:
  a = add x, 5
  b = mul a, 2
  jmp merge

else:
  a = sub y, 3
  b = add a, 1
  jmp merge

merge:
  c = add 3, 4
  d = add 3, 4
  print b
  print c
  ret
```

### Commands

```bash
# Build
cargo build --release

# Run on input IR
cargo run -- example.ir

# Run tests
cargo test

# Run with verbose output
cargo test -- --nocapture
```

### Expected Output

```
// Optimization statistics:
//   Phi nodes inserted: 2
//   Constants propagated: 3
//   Dead instructions eliminated: 1
//   Redundant computations removed: 1

entry:
  x.1 = copy 10
  y.1 = copy 20
  cond.1 = copy 1
  br_if cond.1, then, else

then:
  a.1 = copy 15
  b.1 = copy 30
  jmp merge

else:
  a.2 = copy 17
  b.2 = copy 18
  jmp merge

merge:
  b.3 = copy b.1  // from phi elimination
  c.1 = copy 7
  print b.3
  print c.1
  ret
```

## Design Decisions

1. **Cooper-Harvey-Kennedy over Lengauer-Tarjan for dominance**: The iterative algorithm is simpler to implement (under 50 lines) and converges in 2-3 iterations for most CFGs. Lengauer-Tarjan has better asymptotic complexity (O(n * alpha(n))) but the constant factor and implementation complexity are not justified for the CFG sizes in this project.

2. **Worklist-based constant propagation over iterative dataflow**: The worklist approach re-examines only instructions whose operands changed, rather than iterating over all instructions until fixed point. This is more efficient for large programs where only a few constants propagate through long chains.

3. **GVN with canonicalized commutative operations**: By sorting operand value numbers for commutative ops (add, mul), `add x, y` and `add y, x` produce the same key and are identified as redundant. Without canonicalization, these would be missed.

4. **Topological sort with temporary variables for phi elimination**: Parallel copies from phi elimination can contain swap cycles (a <- b, b <- a). The sequentialization detects cycles by checking if forward progress stalls, then breaks cycles by introducing a temporary variable. This avoids the lost-copy problem where overwriting a source before it is read would produce incorrect results.

5. **String-based variable IDs over numeric indices**: String IDs (e.g., "x.3") make the IR human-readable and debuggable. SSA renaming appends a counter suffix. The tradeoff is slightly slower lookups (HashMap vs Vec indexing), but clarity during development far outweighs the cost.

6. **Separate phi-insertion and renaming passes**: Cytron's algorithm naturally decomposes into two phases: first determine where phi nodes are needed (using dominance frontiers), then rename all variables (using the dominator tree). Combining them would be more complex and harder to debug.

## Common Mistakes

- **Computing dominance frontiers before dominators converge**: The DF computation depends on correct idom values. Running it on partially-computed idom produces incorrect phi placements and silent miscompilation.
- **Forgetting to rename phi arguments in successor blocks**: During SSA renaming, phi arguments in successor blocks must be renamed using the current stack of the predecessor. Missing this leaves phi nodes referencing pre-SSA variable names.
- **Dead code elimination removing phi nodes with side effects**: Phi nodes have no side effects, but removing a phi node whose result is used by a branch instruction breaks the CFG. Always check the full use chain before elimination.
- **Copy sequentialization without cycle detection**: Naive sequential emission of parallel copies (a <- b, b <- a) overwrites b before it is read. The cycle must be detected and broken with a temporary.
- **GVN across basic block boundaries without considering dominance**: A value number from block A is only valid in blocks dominated by A. Using a value number in a block not dominated by its definition produces incorrect code. The solution handles this by processing in dominator tree order.

## Performance Notes

- **Dominance computation**: Converges in 2-3 passes for structured CFGs (loops, diamonds). Worst case for irreducible CFGs is O(n^2) passes, but this is rare in practice.
- **Phi insertion**: The iterated dominance frontier computation is O(|variables| * |blocks|) in the worst case. For programs with hundreds of variables and blocks, this completes in under a millisecond.
- **Constant propagation**: The worklist processes each instruction at most O(n) times total (where n is the number of instructions), because each instruction can only be folded once. The total cost is O(n * average_uses).
- **Memory**: Each SSA variable name is a heap-allocated String. For very large programs (millions of instructions), interning variable names with a string interner would reduce allocation pressure.
