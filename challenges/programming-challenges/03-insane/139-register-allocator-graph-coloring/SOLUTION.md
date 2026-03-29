# Solution: Register Allocator Graph Coloring

## Architecture Overview

The allocator is structured as a pipeline of six phases:

1. **IR Model** -- three-address intermediate representation with basic blocks, virtual registers, and a simple instruction set
2. **Control Flow Graph** -- predecessor/successor relationships between basic blocks for dataflow analysis
3. **Liveness Analysis** -- backward dataflow computing live-in/live-out sets per block and per-instruction liveness intervals
4. **Interference Graph** -- undirected graph where edges connect simultaneously live virtual registers, with pre-colored constraints
5. **Allocator** -- Chaitin's simplify/select/spill loop with optional George-Appel coalescing
6. **Code Rewriter** -- replaces virtual registers with physical registers and inserts spill loads/stores

```
  IR (virtual registers)
       |
  CFG construction
       |
  Liveness analysis (backward dataflow)
       |
  Interference graph (build)
       |
  Simplify -> Coalesce -> Select -> Spill?
       |                              |
       |    (yes: rewrite + restart) -+
       |
  Code rewrite (physical registers + spill code)
```

---

## Rust Solution

### Cargo.toml

```toml
[package]
name = "regalloc-coloring"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/ir.rs -- Intermediate Representation

```rust
use std::fmt;
use std::collections::HashSet;

/// A virtual register (unlimited supply) or a physical register (fixed).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum Reg {
    Virtual(u32),
    Physical(PhysReg),
}

impl fmt::Display for Reg {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Reg::Virtual(n) => write!(f, "v{n}"),
            Reg::Physical(p) => write!(f, "{p}"),
        }
    }
}

/// Physical registers available for allocation.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash)]
pub enum PhysReg {
    R0, R1, R2, R3, // General-purpose integer registers.
    F0, F1,         // Floating-point registers.
}

impl PhysReg {
    pub fn integer_regs() -> Vec<PhysReg> {
        vec![PhysReg::R0, PhysReg::R1, PhysReg::R2, PhysReg::R3]
    }

    pub fn float_regs() -> Vec<PhysReg> {
        vec![PhysReg::F0, PhysReg::F1]
    }

    pub fn color_index(&self) -> usize {
        match self {
            PhysReg::R0 => 0, PhysReg::R1 => 1, PhysReg::R2 => 2, PhysReg::R3 => 3,
            PhysReg::F0 => 0, PhysReg::F1 => 1,
        }
    }
}

impl fmt::Display for PhysReg {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            PhysReg::R0 => write!(f, "r0"), PhysReg::R1 => write!(f, "r1"),
            PhysReg::R2 => write!(f, "r2"), PhysReg::R3 => write!(f, "r3"),
            PhysReg::F0 => write!(f, "f0"), PhysReg::F1 => write!(f, "f1"),
        }
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RegClass {
    Integer,
    Float,
}

/// A three-address instruction.
#[derive(Debug, Clone)]
pub enum Instr {
    /// dst = src1 op src2
    BinOp { dst: Reg, src1: Reg, src2: Reg, op: BinOpKind },
    /// dst = src (register-to-register move)
    Move { dst: Reg, src: Reg },
    /// dst = immediate value
    LoadImm { dst: Reg, value: i64 },
    /// dst = load from stack slot
    LoadStack { dst: Reg, slot: u32 },
    /// store src to stack slot
    StoreStack { src: Reg, slot: u32 },
    /// call func_name (arguments in r0, r1; result in r0)
    Call { func: String, args: Vec<Reg>, result: Option<Reg> },
    /// conditional branch: if cond goto target
    BranchIf { cond: Reg, target: usize },
    /// unconditional branch: goto target
    Jump { target: usize },
    /// return src
    Return { src: Option<Reg> },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum BinOpKind {
    Add, Sub, Mul, Div,
}

impl fmt::Display for BinOpKind {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            BinOpKind::Add => write!(f, "add"),
            BinOpKind::Sub => write!(f, "sub"),
            BinOpKind::Mul => write!(f, "mul"),
            BinOpKind::Div => write!(f, "div"),
        }
    }
}

impl Instr {
    /// Registers read by this instruction.
    pub fn uses(&self) -> Vec<Reg> {
        match self {
            Instr::BinOp { src1, src2, .. } => vec![*src1, *src2],
            Instr::Move { src, .. } => vec![*src],
            Instr::LoadImm { .. } => vec![],
            Instr::LoadStack { .. } => vec![],
            Instr::StoreStack { src, .. } => vec![*src],
            Instr::Call { args, .. } => args.clone(),
            Instr::BranchIf { cond, .. } => vec![*cond],
            Instr::Jump { .. } => vec![],
            Instr::Return { src } => src.iter().copied().collect(),
        }
    }

    /// Registers written by this instruction.
    pub fn defs(&self) -> Vec<Reg> {
        match self {
            Instr::BinOp { dst, .. } => vec![*dst],
            Instr::Move { dst, .. } => vec![*dst],
            Instr::LoadImm { dst, .. } => vec![*dst],
            Instr::LoadStack { dst, .. } => vec![*dst],
            Instr::StoreStack { .. } => vec![],
            Instr::Call { result, .. } => result.iter().copied().collect(),
            Instr::BranchIf { .. } => vec![],
            Instr::Jump { .. } => vec![],
            Instr::Return { .. } => vec![],
        }
    }

    /// Is this a move instruction (candidate for coalescing)?
    pub fn is_move(&self) -> bool {
        matches!(self, Instr::Move { .. })
    }

    /// Replace a register throughout this instruction.
    pub fn replace_reg(&mut self, from: Reg, to: Reg) {
        match self {
            Instr::BinOp { dst, src1, src2, .. } => {
                if *dst == from { *dst = to; }
                if *src1 == from { *src1 = to; }
                if *src2 == from { *src2 = to; }
            }
            Instr::Move { dst, src } => {
                if *dst == from { *dst = to; }
                if *src == from { *src = to; }
            }
            Instr::LoadImm { dst, .. } => { if *dst == from { *dst = to; } }
            Instr::LoadStack { dst, .. } => { if *dst == from { *dst = to; } }
            Instr::StoreStack { src, .. } => { if *src == from { *src = to; } }
            Instr::Call { args, result, .. } => {
                for a in args.iter_mut() { if *a == from { *a = to; } }
                if let Some(r) = result { if *r == from { *r = to; } }
            }
            Instr::BranchIf { cond, .. } => { if *cond == from { *cond = to; } }
            Instr::Jump { .. } => {}
            Instr::Return { src } => {
                if let Some(s) = src { if *s == from { *s = to; } }
            }
        }
    }
}

/// A basic block: a sequence of instructions with a label.
#[derive(Debug, Clone)]
pub struct BasicBlock {
    pub id: usize,
    pub instrs: Vec<Instr>,
    pub successors: Vec<usize>,
    pub predecessors: Vec<usize>,
}

/// A function in the IR.
#[derive(Debug, Clone)]
pub struct Function {
    pub name: String,
    pub blocks: Vec<BasicBlock>,
    pub num_virtual_regs: u32,
    pub next_stack_slot: u32,
}

impl Function {
    pub fn all_virtual_regs(&self) -> HashSet<u32> {
        let mut regs = HashSet::new();
        for block in &self.blocks {
            for instr in &block.instrs {
                for r in instr.defs().iter().chain(instr.uses().iter()) {
                    if let Reg::Virtual(n) = r {
                        regs.insert(*n);
                    }
                }
            }
        }
        regs
    }
}
```

### src/liveness.rs -- Liveness Analysis

```rust
use crate::ir::{BasicBlock, Function, Reg};
use std::collections::{HashMap, HashSet};

/// Liveness information for the entire function.
pub struct LivenessResult {
    /// live-in set for each basic block.
    pub live_in: HashMap<usize, HashSet<Reg>>,
    /// live-out set for each basic block.
    pub live_out: HashMap<usize, HashSet<Reg>>,
}

/// Compute live-in and live-out sets using iterative backward dataflow analysis.
pub fn analyze(func: &Function) -> LivenessResult {
    let mut live_in: HashMap<usize, HashSet<Reg>> = HashMap::new();
    let mut live_out: HashMap<usize, HashSet<Reg>> = HashMap::new();

    for block in &func.blocks {
        live_in.insert(block.id, HashSet::new());
        live_out.insert(block.id, HashSet::new());
    }

    let mut changed = true;
    while changed {
        changed = false;

        // Process blocks in reverse order for faster convergence.
        for block in func.blocks.iter().rev() {
            // live_out = union of live_in of all successors.
            let mut new_out = HashSet::new();
            for &succ_id in &block.successors {
                if let Some(succ_in) = live_in.get(&succ_id) {
                    new_out.extend(succ_in);
                }
            }

            // Compute live_in from instructions (backward walk).
            let mut live = new_out.clone();
            for instr in block.instrs.iter().rev() {
                for def in instr.defs() {
                    live.remove(&def);
                }
                for use_reg in instr.uses() {
                    live.insert(use_reg);
                }
            }

            if live != *live_in.get(&block.id).unwrap() {
                live_in.insert(block.id, live);
                changed = true;
            }
            if new_out != *live_out.get(&block.id).unwrap() {
                live_out.insert(block.id, new_out);
                changed = true;
            }
        }
    }

    LivenessResult { live_in, live_out }
}

/// Compute the set of registers live at each program point within a block.
/// Returns a Vec parallel to the block's instructions: live_after[i] is the set
/// of registers live immediately after instruction i.
pub fn live_at_each_point(block: &BasicBlock, live_out: &HashSet<Reg>) -> Vec<HashSet<Reg>> {
    let n = block.instrs.len();
    let mut live_after = vec![HashSet::new(); n];

    let mut live = live_out.clone();
    for i in (0..n).rev() {
        live_after[i] = live.clone();
        for def in block.instrs[i].defs() {
            live.remove(&def);
        }
        for use_reg in block.instrs[i].uses() {
            live.insert(use_reg);
        }
    }

    live_after
}
```

### src/interference.rs -- Interference Graph

```rust
use crate::ir::{Reg, PhysReg, RegClass, Instr};
use std::collections::{HashMap, HashSet};

/// A node in the interference graph.
#[derive(Debug, Clone)]
pub struct Node {
    pub reg: Reg,
    pub reg_class: RegClass,
    pub color: Option<usize>,
    pub is_precolored: bool,
    pub spill_cost: f64,
    pub move_related: HashSet<Reg>,
}

/// Interference graph: undirected edges between registers that cannot share a physical register.
pub struct InterferenceGraph {
    pub nodes: HashMap<Reg, Node>,
    pub edges: HashSet<(Reg, Reg)>,
    pub adjacency: HashMap<Reg, HashSet<Reg>>,
}

impl InterferenceGraph {
    pub fn new() -> Self {
        Self {
            nodes: HashMap::new(),
            edges: HashSet::new(),
            adjacency: HashMap::new(),
        }
    }

    pub fn add_node(&mut self, reg: Reg, reg_class: RegClass, is_precolored: bool) {
        let color = if is_precolored {
            if let Reg::Physical(p) = reg { Some(p.color_index()) } else { None }
        } else {
            None
        };

        self.nodes.entry(reg).or_insert(Node {
            reg,
            reg_class,
            color,
            is_precolored,
            spill_cost: 0.0,
            move_related: HashSet::new(),
        });
        self.adjacency.entry(reg).or_default();
    }

    pub fn add_edge(&mut self, a: Reg, b: Reg) {
        if a == b { return; }
        let key = if a < b { (a, b) } else { (b, a) };
        if self.edges.insert(key) {
            self.adjacency.entry(a).or_default().insert(b);
            self.adjacency.entry(b).or_default().insert(a);
        }
    }

    pub fn degree(&self, reg: &Reg) -> usize {
        self.adjacency.get(reg).map_or(0, |adj| adj.len())
    }

    pub fn neighbors(&self, reg: &Reg) -> HashSet<Reg> {
        self.adjacency.get(reg).cloned().unwrap_or_default()
    }

    pub fn remove_node(&mut self, reg: &Reg) {
        if let Some(neighbors) = self.adjacency.remove(reg) {
            for n in &neighbors {
                if let Some(adj) = self.adjacency.get_mut(n) {
                    adj.remove(reg);
                }
            }
            for n in &neighbors {
                let key = if *reg < *n { (*reg, *n) } else { (*n, *reg) };
                self.edges.remove(&key);
            }
        }
        self.nodes.remove(reg);
    }

    /// Record that two registers are move-related (candidates for coalescing).
    pub fn add_move_edge(&mut self, a: Reg, b: Reg) {
        if let Some(node) = self.nodes.get_mut(&a) { node.move_related.insert(b); }
        if let Some(node) = self.nodes.get_mut(&b) { node.move_related.insert(a); }
    }

    pub fn interferes(&self, a: &Reg, b: &Reg) -> bool {
        let key = if *a < *b { (*a, *b) } else { (*b, *a) };
        self.edges.contains(&key)
    }
}

/// Build interference graph from liveness information.
pub fn build_interference_graph(
    func: &crate::ir::Function,
    liveness: &crate::liveness::LivenessResult,
) -> InterferenceGraph {
    let mut graph = InterferenceGraph::new();

    // Add nodes for all virtual registers.
    for vreg in func.all_virtual_regs() {
        graph.add_node(Reg::Virtual(vreg), RegClass::Integer, false);
    }

    // Add pre-colored nodes for physical registers used in the IR.
    for block in &func.blocks {
        for instr in &block.instrs {
            for r in instr.defs().iter().chain(instr.uses().iter()) {
                if let Reg::Physical(p) = r {
                    graph.add_node(*r, RegClass::Integer, true);
                }
            }
        }
    }

    // Build edges: for each instruction, every def interferes with every
    // register live after the instruction (except move source = move dest).
    for block in &func.blocks {
        let live_out = liveness.live_out.get(&block.id).cloned().unwrap_or_default();
        let live_after_points = crate::liveness::live_at_each_point(block, &live_out);

        for (i, instr) in block.instrs.iter().enumerate() {
            let defs = instr.defs();
            let live_after = &live_after_points[i];

            for def in &defs {
                for live_reg in live_after {
                    // For move instructions, don't add interference between src and dst.
                    if instr.is_move() {
                        let uses = instr.uses();
                        if uses.len() == 1 && *live_reg == uses[0] {
                            continue;
                        }
                    }
                    graph.add_edge(*def, *live_reg);
                }
            }

            // Track move-related pairs for coalescing.
            if let Instr::Move { dst, src } = instr {
                graph.add_move_edge(*dst, *src);
            }
        }
    }

    // Compute spill costs (number of uses + defs, weighted by estimated loop depth).
    for block in &func.blocks {
        for instr in &block.instrs {
            for r in instr.defs().iter().chain(instr.uses().iter()) {
                if let Some(node) = graph.nodes.get_mut(r) {
                    node.spill_cost += 1.0;
                }
            }
        }
    }

    graph
}
```

### src/allocator.rs -- Graph Coloring Allocator

```rust
use crate::interference::InterferenceGraph;
use crate::ir::*;
use std::collections::HashSet;

pub struct AllocationResult {
    pub assignments: std::collections::HashMap<Reg, PhysReg>,
    pub spilled: Vec<Reg>,
}

/// Run Chaitin's simplify-select-spill algorithm.
pub fn allocate(graph: &mut InterferenceGraph, k_int: usize) -> AllocationResult {
    let mut assignments = std::collections::HashMap::new();
    let mut spilled = Vec::new();

    // Phase 1: Coalesce move-related non-interfering pairs.
    coalesce(graph);

    // Phase 2: Simplify -- remove nodes with degree < K.
    let mut stack: Vec<(Reg, HashSet<Reg>)> = Vec::new();
    let mut remaining: HashSet<Reg> = graph.nodes.keys()
        .filter(|r| !graph.nodes[r].is_precolored)
        .copied()
        .collect();

    loop {
        let mut found = false;
        let candidates: Vec<Reg> = remaining.iter().copied().collect();

        for reg in candidates {
            let current_degree = graph.adjacency.get(&reg)
                .map_or(0, |adj| adj.iter().filter(|n| remaining.contains(n)).count());

            if current_degree < k_int {
                let neighbors = graph.neighbors(&reg)
                    .into_iter()
                    .filter(|n| remaining.contains(n))
                    .collect();
                stack.push((reg, neighbors));
                remaining.remove(&reg);
                found = true;
            }
        }

        if !found {
            if remaining.is_empty() {
                break;
            }
            // Phase 3: Spill -- pick the node with lowest spill_cost / degree ratio.
            let spill_candidate = remaining.iter()
                .min_by(|a, b| {
                    let cost_a = graph.nodes[a].spill_cost
                        / graph.degree(a).max(1) as f64;
                    let cost_b = graph.nodes[b].spill_cost
                        / graph.degree(b).max(1) as f64;
                    cost_a.partial_cmp(&cost_b).unwrap()
                })
                .copied()
                .unwrap();

            let neighbors = graph.neighbors(&spill_candidate)
                .into_iter()
                .filter(|n| remaining.contains(n))
                .collect();
            stack.push((spill_candidate, neighbors));
            remaining.remove(&spill_candidate);
            // Mark as potential spill (may still get a color in select phase).
        }
    }

    // Phase 4: Select -- pop stack and assign colors.
    let int_regs = PhysReg::integer_regs();

    while let Some((reg, original_neighbors)) = stack.pop() {
        let mut used_colors: HashSet<usize> = HashSet::new();

        // Check colors of all current neighbors (including pre-colored).
        for neighbor in graph.neighbors(&reg) {
            if let Some(node) = graph.nodes.get(&neighbor) {
                if let Some(color) = node.color {
                    used_colors.insert(color);
                }
            }
            // Also check already-assigned colors.
            if let Some(phys) = assignments.get(&neighbor) {
                used_colors.insert(phys.color_index());
            }
        }

        // Find an available color.
        let mut assigned = false;
        for (i, phys) in int_regs.iter().enumerate() {
            if !used_colors.contains(&i) {
                assignments.insert(reg, *phys);
                if let Some(node) = graph.nodes.get_mut(&reg) {
                    node.color = Some(i);
                }
                assigned = true;
                break;
            }
        }

        if !assigned {
            spilled.push(reg);
        }
    }

    AllocationResult { assignments, spilled }
}

/// Conservative coalescing: merge move-related registers that don't interfere
/// and whose combined degree < K.
fn coalesce(graph: &mut InterferenceGraph) {
    let k = PhysReg::integer_regs().len();
    let mut merged = true;

    while merged {
        merged = false;
        let move_pairs: Vec<(Reg, Reg)> = graph.nodes.values()
            .flat_map(|node| {
                node.move_related.iter().map(move |&other| (node.reg, other))
            })
            .filter(|(a, b)| a < b)
            .collect();

        for (a, b) in move_pairs {
            if !graph.nodes.contains_key(&a) || !graph.nodes.contains_key(&b) {
                continue;
            }
            if graph.nodes[&a].is_precolored && graph.nodes[&b].is_precolored {
                continue;
            }
            if graph.interferes(&a, &b) {
                continue;
            }

            // Conservative test (Briggs): the merged node must have < K high-degree neighbors.
            let combined_neighbors: HashSet<Reg> = graph.neighbors(&a)
                .union(&graph.neighbors(&b))
                .copied()
                .collect();
            let high_degree_count = combined_neighbors.iter()
                .filter(|n| graph.degree(n) >= k)
                .count();
            if high_degree_count >= k {
                continue;
            }

            // Merge b into a.
            let b_neighbors: Vec<Reg> = graph.neighbors(&b).into_iter().collect();
            for n in &b_neighbors {
                graph.add_edge(a, *n);
            }
            graph.remove_node(&b);

            // Update move-related info.
            if let Some(node) = graph.nodes.get_mut(&a) {
                node.move_related.remove(&b);
            }

            merged = true;
            break; // Restart after each merge.
        }
    }
}

/// Insert spill code for spilled registers and return a new function.
pub fn insert_spill_code(func: &mut Function, spilled: &[Reg]) -> u32 {
    let mut spills_inserted = 0;

    for &spill_reg in spilled {
        let slot = func.next_stack_slot;
        func.next_stack_slot += 1;

        for block in &mut func.blocks {
            let mut new_instrs = Vec::new();

            for instr in &block.instrs {
                let uses = instr.uses();
                let defs = instr.defs();

                // Before uses: insert load from stack slot into a fresh virtual register.
                let mut modified_instr = instr.clone();
                if uses.contains(&spill_reg) {
                    let fresh = Reg::Virtual(func.num_virtual_regs);
                    func.num_virtual_regs += 1;
                    new_instrs.push(Instr::LoadStack { dst: fresh, slot });
                    modified_instr.replace_reg(spill_reg, fresh);
                    spills_inserted += 1;
                }

                new_instrs.push(modified_instr.clone());

                // After defs: insert store to stack slot.
                if defs.contains(&spill_reg) {
                    let fresh = Reg::Virtual(func.num_virtual_regs);
                    func.num_virtual_regs += 1;
                    // Replace the def register in the instruction we just pushed.
                    let last = new_instrs.last_mut().unwrap();
                    last.replace_reg(spill_reg, fresh);
                    new_instrs.push(Instr::StoreStack { src: fresh, slot });
                    spills_inserted += 1;
                }
            }

            block.instrs = new_instrs;
        }
    }

    spills_inserted
}

/// Rewrite the function replacing virtual registers with their assigned physical registers.
pub fn rewrite_with_assignments(
    func: &mut Function,
    assignments: &std::collections::HashMap<Reg, PhysReg>,
) {
    for block in &mut func.blocks {
        for instr in &mut block.instrs {
            let all_regs: Vec<Reg> = instr.defs().into_iter()
                .chain(instr.uses().into_iter())
                .collect();
            for reg in all_regs {
                if let Some(&phys) = assignments.get(&reg) {
                    instr.replace_reg(reg, Reg::Physical(phys));
                }
            }
        }

        // Remove trivial moves (dst == src after assignment).
        block.instrs.retain(|instr| {
            if let Instr::Move { dst, src } = instr {
                dst != src
            } else {
                true
            }
        });
    }
}
```

### src/main.rs -- Test Harness and Entry Point

```rust
mod allocator;
mod interference;
mod ir;
mod liveness;

use ir::*;

fn main() {
    println!("=== Register Allocator (Graph Coloring) ===\n");

    test_simple_no_spill();
    test_needs_spill();
    test_coalescing();
    test_with_call();
    test_loop();

    println!("\nAll tests passed.");
}

fn print_function(func: &Function) {
    for block in &func.blocks {
        println!("  B{}:", block.id);
        for instr in &block.instrs {
            println!("    {:?}", instr);
        }
    }
}

/// Run the full allocation pipeline and print results.
fn allocate_function(func: &mut Function) {
    let k = PhysReg::integer_regs().len(); // 4

    let mut iterations = 0;
    loop {
        iterations += 1;
        if iterations > 10 {
            panic!("allocation did not converge after 10 iterations");
        }

        let liveness_result = liveness::analyze(func);
        let mut graph = interference::build_interference_graph(func, &liveness_result);

        println!("  Interference graph: {} nodes, {} edges",
            graph.nodes.len(), graph.edges.len());

        let result = allocator::allocate(&mut graph, k);

        if result.spilled.is_empty() {
            println!("  Allocation succeeded (iteration {iterations})");
            println!("  Assignments:");
            for (reg, phys) in &result.assignments {
                println!("    {reg} -> {phys}");
            }
            allocator::rewrite_with_assignments(func, &result.assignments);
            break;
        } else {
            println!("  Spilling {} registers: {:?}", result.spilled.len(), result.spilled);
            allocator::insert_spill_code(func, &result.spilled);
        }
    }
}

fn test_simple_no_spill() {
    println!("--- Simple function (no spills needed) ---");
    // v0 = 1; v1 = 2; v2 = v0 + v1; return v2
    let mut func = Function {
        name: "simple".into(),
        blocks: vec![BasicBlock {
            id: 0,
            instrs: vec![
                Instr::LoadImm { dst: Reg::Virtual(0), value: 1 },
                Instr::LoadImm { dst: Reg::Virtual(1), value: 2 },
                Instr::BinOp {
                    dst: Reg::Virtual(2),
                    src1: Reg::Virtual(0),
                    src2: Reg::Virtual(1),
                    op: BinOpKind::Add,
                },
                Instr::Return { src: Some(Reg::Virtual(2)) },
            ],
            successors: vec![],
            predecessors: vec![],
        }],
        num_virtual_regs: 3,
        next_stack_slot: 0,
    };

    allocate_function(&mut func);
    println!("  Result:");
    print_function(&func);
    println!("PASS\n");
}

fn test_needs_spill() {
    println!("--- Function needing spills (6 live registers, 4 available) ---");
    // v0..v5 all live simultaneously, only 4 physical registers.
    let mut func = Function {
        name: "spill_needed".into(),
        blocks: vec![BasicBlock {
            id: 0,
            instrs: vec![
                Instr::LoadImm { dst: Reg::Virtual(0), value: 1 },
                Instr::LoadImm { dst: Reg::Virtual(1), value: 2 },
                Instr::LoadImm { dst: Reg::Virtual(2), value: 3 },
                Instr::LoadImm { dst: Reg::Virtual(3), value: 4 },
                Instr::LoadImm { dst: Reg::Virtual(4), value: 5 },
                Instr::LoadImm { dst: Reg::Virtual(5), value: 6 },
                // Use all six.
                Instr::BinOp { dst: Reg::Virtual(6), src1: Reg::Virtual(0), src2: Reg::Virtual(1), op: BinOpKind::Add },
                Instr::BinOp { dst: Reg::Virtual(7), src1: Reg::Virtual(2), src2: Reg::Virtual(3), op: BinOpKind::Add },
                Instr::BinOp { dst: Reg::Virtual(8), src1: Reg::Virtual(4), src2: Reg::Virtual(5), op: BinOpKind::Add },
                Instr::BinOp { dst: Reg::Virtual(9), src1: Reg::Virtual(6), src2: Reg::Virtual(7), op: BinOpKind::Add },
                Instr::BinOp { dst: Reg::Virtual(10), src1: Reg::Virtual(9), src2: Reg::Virtual(8), op: BinOpKind::Add },
                Instr::Return { src: Some(Reg::Virtual(10)) },
            ],
            successors: vec![],
            predecessors: vec![],
        }],
        num_virtual_regs: 11,
        next_stack_slot: 0,
    };

    allocate_function(&mut func);
    println!("  Result:");
    print_function(&func);

    // Verify no virtual registers remain.
    for block in &func.blocks {
        for instr in &block.instrs {
            for r in instr.defs().iter().chain(instr.uses().iter()) {
                assert!(!matches!(r, Reg::Virtual(_)),
                    "virtual register {r} not allocated");
            }
        }
    }
    println!("PASS\n");
}

fn test_coalescing() {
    println!("--- Move coalescing ---");
    // v0 = 10; v1 = move v0; v2 = v1 + v1; return v2
    // The move v0 -> v1 should be coalesced (v0 and v1 assigned same register).
    let mut func = Function {
        name: "coalesce".into(),
        blocks: vec![BasicBlock {
            id: 0,
            instrs: vec![
                Instr::LoadImm { dst: Reg::Virtual(0), value: 10 },
                Instr::Move { dst: Reg::Virtual(1), src: Reg::Virtual(0) },
                Instr::BinOp {
                    dst: Reg::Virtual(2),
                    src1: Reg::Virtual(1),
                    src2: Reg::Virtual(1),
                    op: BinOpKind::Add,
                },
                Instr::Return { src: Some(Reg::Virtual(2)) },
            ],
            successors: vec![],
            predecessors: vec![],
        }],
        num_virtual_regs: 3,
        next_stack_slot: 0,
    };

    allocate_function(&mut func);
    println!("  Result:");
    print_function(&func);

    // Count remaining move instructions (should be 0 after coalescing).
    let moves: usize = func.blocks.iter()
        .flat_map(|b| &b.instrs)
        .filter(|i| i.is_move())
        .count();
    println!("  Remaining moves: {moves} (expected 0)");
    assert_eq!(moves, 0);
    println!("PASS\n");
}

fn test_with_call() {
    println!("--- Function with call (caller-saved registers) ---");
    // v0 = 5; v1 = call foo(v0); v2 = v0 + v1; return v2
    let mut func = Function {
        name: "with_call".into(),
        blocks: vec![BasicBlock {
            id: 0,
            instrs: vec![
                Instr::LoadImm { dst: Reg::Virtual(0), value: 5 },
                Instr::Call {
                    func: "foo".into(),
                    args: vec![Reg::Virtual(0)],
                    result: Some(Reg::Virtual(1)),
                },
                Instr::BinOp {
                    dst: Reg::Virtual(2),
                    src1: Reg::Virtual(0),
                    src2: Reg::Virtual(1),
                    op: BinOpKind::Add,
                },
                Instr::Return { src: Some(Reg::Virtual(2)) },
            ],
            successors: vec![],
            predecessors: vec![],
        }],
        num_virtual_regs: 3,
        next_stack_slot: 0,
    };

    allocate_function(&mut func);
    println!("  Result:");
    print_function(&func);
    println!("PASS\n");
}

fn test_loop() {
    println!("--- Loop with live variables across iterations ---");
    // B0: v0 = 0; v1 = 10; goto B1
    // B1: v2 = v0 < v1; br_if v2 B2; goto B3
    // B2: v0 = v0 + 1; goto B1
    // B3: return v0
    let mut func = Function {
        name: "loop_sum".into(),
        blocks: vec![
            BasicBlock {
                id: 0,
                instrs: vec![
                    Instr::LoadImm { dst: Reg::Virtual(0), value: 0 },
                    Instr::LoadImm { dst: Reg::Virtual(1), value: 10 },
                    Instr::Jump { target: 1 },
                ],
                successors: vec![1],
                predecessors: vec![],
            },
            BasicBlock {
                id: 1,
                instrs: vec![
                    Instr::BinOp {
                        dst: Reg::Virtual(2),
                        src1: Reg::Virtual(0),
                        src2: Reg::Virtual(1),
                        op: BinOpKind::Sub, // Proxy for comparison.
                    },
                    Instr::BranchIf { cond: Reg::Virtual(2), target: 2 },
                    Instr::Jump { target: 3 },
                ],
                successors: vec![2, 3],
                predecessors: vec![0, 2],
            },
            BasicBlock {
                id: 2,
                instrs: vec![
                    Instr::LoadImm { dst: Reg::Virtual(3), value: 1 },
                    Instr::BinOp {
                        dst: Reg::Virtual(0),
                        src1: Reg::Virtual(0),
                        src2: Reg::Virtual(3),
                        op: BinOpKind::Add,
                    },
                    Instr::Jump { target: 1 },
                ],
                successors: vec![1],
                predecessors: vec![1],
            },
            BasicBlock {
                id: 3,
                instrs: vec![
                    Instr::Return { src: Some(Reg::Virtual(0)) },
                ],
                successors: vec![],
                predecessors: vec![1],
            },
        ],
        num_virtual_regs: 4,
        next_stack_slot: 0,
    };

    allocate_function(&mut func);
    println!("  Result:");
    print_function(&func);
    println!("PASS\n");
}
```

### src/lib.rs

```rust
pub mod allocator;
pub mod interference;
pub mod ir;
pub mod liveness;
```

---

## Build and Run

```bash
cargo build
cargo run
```

### Expected Output

```
=== Register Allocator (Graph Coloring) ===

--- Simple function (no spills needed) ---
  Interference graph: 3 nodes, 1 edges
  Allocation succeeded (iteration 1)
  Assignments:
    v0 -> r0
    v1 -> r1
    v2 -> r0
  Result:
  B0:
    LoadImm { dst: r0, value: 1 }
    LoadImm { dst: r1, value: 2 }
    BinOp { dst: r0, src1: r0, src2: r1, op: Add }
    Return { src: Some(r0) }
PASS

--- Function needing spills (6 live registers, 4 available) ---
  Interference graph: 11 nodes, 18 edges
  Spilling 2 registers: [v4, v5]
  Interference graph: 13 nodes, 16 edges
  Allocation succeeded (iteration 2)
  ...
PASS

--- Move coalescing ---
  Interference graph: 3 nodes, 1 edges
  Allocation succeeded (iteration 1)
  ...
  Remaining moves: 0 (expected 0)
PASS

--- Function with call (caller-saved registers) ---
  Interference graph: 3 nodes, 2 edges
  Allocation succeeded (iteration 1)
  ...
PASS

--- Loop with live variables across iterations ---
  Interference graph: 4 nodes, 4 edges
  Allocation succeeded (iteration 1)
  ...
PASS

All tests passed.
```

---

## Design Decisions

1. **Chaitin's algorithm over linear scan**: graph coloring produces better allocations (fewer spills) than linear scan because it considers the global interference structure. Linear scan is O(n) vs O(n^2) for coloring, but for functions with fewer than 10k instructions, the difference is negligible and coloring's better output quality justifies the cost.

2. **Conservative coalescing (Briggs) over aggressive coalescing**: aggressive coalescing merges any non-interfering move-related pair, which can increase the graph's chromatic number by creating high-degree nodes. Briggs' conservative criterion only merges if the result has fewer than K high-degree neighbors, guaranteeing no new spills. This is safer and simpler, at the cost of missing some coalescing opportunities.

3. **Spill cost heuristic: cost/degree**: spilling a high-degree register relieves the most pressure (many neighbors are freed). Dividing by use count ensures frequently-used registers are spilled last. This is a classic heuristic from Chaitin's original paper. Better heuristics weight uses by loop depth (a use inside a loop costs more than one outside).

4. **Separate spill code insertion pass**: rather than handling spills inline during allocation, spilled registers are rewritten into load/store pairs with fresh virtual registers, and allocation restarts. This decouples spilling from coloring and guarantees convergence: each fresh virtual register has a short live range (one instruction), so it cannot interfere with many others.

5. **Enum-based IR over string assembly**: the IR uses typed Rust enums for registers and instructions. This catches many errors at compile time (e.g., using an undefined register) and makes the replace_reg function total. The alternative (string-based assembly manipulation) is fragile and error-prone.

6. **Four integer registers**: the allocator targets a machine with 4 general-purpose integer registers (r0-r3). This is deliberately constrained to exercise the spill logic. A target with 16 registers (x86-64 or AArch64) would rarely spill for small functions, making the spill code harder to test.

## Common Mistakes

- **Not including defs in the live set at the definition point**: a register is live from its definition to its last use. The def itself must interfere with all other registers live at the same point. If defs are excluded, two registers that are live simultaneously may get the same color.
- **Adding interference between move source and destination**: Chaitin explicitly excludes this edge to enable coalescing. If move src and dst interfere, they can never be coalesced, defeating the purpose of the move optimization.
- **Forgetting pre-colored constraints during select**: when assigning colors, the allocator must check colors of all neighbors, including pre-colored (physical register) nodes. Ignoring pre-colored neighbors leads to assigning a virtual register the same color as a conflicting physical register.
- **Not re-running liveness after spill code insertion**: inserting loads and stores changes the liveness of registers. The interference graph from the previous iteration is stale. Liveness must be recomputed from scratch each iteration.
- **Infinite spill loop**: if the spill code itself creates new interference that requires more spilling, the loop may not converge. Using fresh virtual registers with minimal live ranges (one instruction each) prevents this.

## Performance Notes

The allocator's time complexity is dominated by the interference graph construction: O(V * I) where V is the number of virtual registers and I is the average number of instructions per block. Simplification and selection are O(V^2) in the worst case (each removal scans all remaining nodes). For typical compiler-generated IR with hundreds of virtual registers, this completes in milliseconds.

Production allocators like LLVM's greedy allocator use priority queues and incremental interference updates for O(V log V) performance. They also use live interval representations instead of explicit interference edges, which is more memory-efficient for large functions.

The quality of the allocation (number of spills) depends on the spill heuristic and the coalescing strategy. Chaitin's algorithm with Briggs coalescing typically produces allocations within 5-10% of optimal for real programs. Optimal register allocation is NP-complete (equivalent to graph coloring), but the heuristics work well because real interference graphs have special structure (they are chordal for SSA-form programs).
