<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [ssa-form, phi-nodes, dominance-frontiers, reaching-definitions, live-variables, use-def-chains, dataflow-analysis]
languages: [go, rust]
estimated_reading_time: 75-90 min
bloom_level: analyze
prerequisites: [ast-and-ir-design, control-flow-graphs, graph-algorithms-dfs]
papers: [Cytron-1991-SSA, Braun-2013-SSA]
industry_use: [llvm, go-compiler, rustc, v8, graalvm]
language_contrast: medium
-->

# SSA Form and Dataflow Analysis

> SSA's defining rule — every variable is defined exactly once — transforms the hard problem of "where does this value come from?" into a trivially answerable lookup, enabling every major compiler optimization.

## Mental Model

In ordinary code, a variable like `x` can be assigned in multiple places: at the top of a function, inside an `if` branch, inside a loop. When a compiler wants to ask "what value does `x` have at this point?", it must trace all possible assignment paths. This is the "reaching definitions" problem, and solving it requires a full dataflow analysis — a global computation over the entire control flow graph.

SSA (Static Single Assignment) form eliminates this problem by construction. When you convert a program to SSA, each variable is split into a set of versioned names: `x1`, `x2`, `x3`, one per assignment site. A use of `x` at a given point in the program is rewritten to use the specific version that reaches that point. The "where does this value come from?" question becomes: look at the subscript.

The challenge is control flow. When two branches of an `if` both define `x` — one defines `x1`, the other defines `x2` — and code after the `if` uses `x`, which version should it use? The answer is a φ (phi) node: `x3 = φ(x1, x2)`. The phi node is a notational device that says "take the value from whichever predecessor block we arrived from." It is not an instruction that executes; it is a statement about the program's data flow structure.

The payoff of SSA is enormous. With every variable defined once:
- **Constant propagation** is trivial: if `x1 = 5`, replace all uses of `x1` with `5`. No aliasing concerns because `x1` never changes.
- **Dead code elimination** is trivial: if no use of `x1` exists, the defining instruction is dead.
- **Register allocation** sees use-def chains directly: `x1`'s live range is exactly from its definition to its last use.
- **Loop optimizations** identify induction variables (variables that increment by a constant per iteration) structurally via their phi nodes.

This is why every serious compiler — LLVM, Go's `cmd/compile`, rustc's MIR, V8's TurboFan, GraalVM — uses SSA form internally.

## Core Concepts

### Dominance and Dominance Frontiers

A block `A` **dominates** block `B` if every path from the function's entry to `B` passes through `A`. The entry block dominates all blocks; every block dominates itself. The **dominator tree** is the tree where each block's parent is its immediate dominator (the closest dominator that is not the block itself).

The **dominance frontier** of block `A` is the set of blocks that `A` does not dominate but whose immediate predecessors `A` does dominate. Informally, the dominance frontier is "where A's definition stops being the only definition."

Phi nodes must be inserted exactly at the dominance frontier of each definition site. This is the Cytron et al. insight: if a variable is defined in block `A`, a phi node is needed at every block in `DF(A)` — and at blocks in `DF(DF(A))` if those blocks also need phi nodes, and so on (iterated dominance frontier).

### SSA Construction Algorithm (Braun et al., 2013)

Braun's algorithm is simpler than Cytron's and works well for educational purposes:

1. Process blocks in dominator-tree order (top-down)
2. For each variable use, look up its current definition by walking up the dominator tree
3. If the current block has multiple predecessors, insert a phi node (or reuse an existing one)
4. Phi nodes are simplified: if a phi node has all the same arguments, replace it with a direct use

The key data structure is a per-block map: `block → {var_name → current_ssa_version}`. When entering a block, inherit the parent's map. When processing an assignment `x = ...`, add `x → x_n` (fresh version) to the current block's map.

### Dataflow Analysis Framework

Dataflow analysis solves equations of the form:

```
OUT[B] = GEN[B] ∪ (IN[B] − KILL[B])
IN[B]  = ∪ OUT[P]   for all predecessors P of B   (for forward analysis)
```

This is a **worklist algorithm**: initialize all `OUT` to ∅ (or the appropriate lattice bottom), add all blocks to the worklist, and repeatedly process blocks until no `OUT` changes. The analysis converges because:
1. `OUT` values grow monotonically (∅ → full set)
2. The set of definitions is finite

**Reaching definitions**: Which assignments might reach a given use? Forward analysis. `GEN[B]` = definitions in B; `KILL[B]` = all other definitions of the same variable.

**Live variables**: Which variables are "live" (will be used again) at a given point? Backward analysis. A variable is live at the end of block B if it is used in B before being redefined, or if it is live at the start of any successor of B.

**Available expressions**: Which expressions have been computed and not invalidated? Forward analysis. Used by Common Subexpression Elimination (CSE).

## Implementation: Go

```go
package main

import (
	"fmt"
	"sort"
	"strings"
)

// A minimal SSA implementation for a toy CFG.
// We represent the CFG directly (not generated from source) to focus
// on the SSA algorithms.

// --- CFG Representation ---

type BlockID int

type Instr struct {
	Dst  string   // "" for instructions without destinations (branch)
	Op   string   // "const", "add", "sub", "phi", "br", "cbr", "ret"
	Args []string // operands
	// For "const": Args[0] is the constant value as a string
	// For "phi": Args are alternating (value, block) pairs
	// For "br": Args[0] is the target block id
	// For "cbr": Args[0] is cond, Args[1] is true target, Args[2] is false target
}

func (i *Instr) String() string {
	if i.Dst != "" {
		return fmt.Sprintf("  %s = %s %s", i.Dst, i.Op, strings.Join(i.Args, ", "))
	}
	return fmt.Sprintf("  %s %s", i.Op, strings.Join(i.Args, ", "))
}

type Block struct {
	ID     BlockID
	Name   string
	Instrs []*Instr
	Preds  []BlockID
	Succs  []BlockID
}

type CFG struct {
	Blocks  map[BlockID]*Block
	Entry   BlockID
	NumVars int
}

func NewCFG() *CFG {
	return &CFG{Blocks: make(map[BlockID]*Block)}
}

func (c *CFG) AddBlock(id BlockID, name string) *Block {
	b := &Block{ID: id, Name: name}
	c.Blocks[id] = b
	return b
}

func (c *CFG) AddEdge(from, to BlockID) {
	c.Blocks[from].Succs = append(c.Blocks[from].Succs, to)
	c.Blocks[to].Preds = append(c.Blocks[to].Preds, from)
}

// --- Dominance Tree Computation (Lengauer-Tarjan, simplified) ---
// For small CFGs, a simple O(n^2) algorithm suffices.

// dom[b] = immediate dominator of b (-1 for entry)
func computeIDom(cfg *CFG) map[BlockID]BlockID {
	idom := make(map[BlockID]BlockID)
	idom[cfg.Entry] = cfg.Entry

	// BFS order for iteration stability
	var order []BlockID
	visited := make(map[BlockID]bool)
	queue := []BlockID{cfg.Entry}
	for len(queue) > 0 {
		b := queue[0]
		queue = queue[1:]
		if visited[b] {
			continue
		}
		visited[b] = true
		order = append(order, b)
		for _, s := range cfg.Blocks[b].Succs {
			queue = append(queue, s)
		}
	}

	// Simple O(n²) dominance: dom[b] = intersection of dom[all preds]
	// dom[b] = set of blocks that dominate b
	doms := make(map[BlockID]map[BlockID]bool)
	for _, bid := range order {
		doms[bid] = make(map[BlockID]bool)
		for _, ob := range order {
			doms[bid][ob] = true
		}
	}
	doms[cfg.Entry] = map[BlockID]bool{cfg.Entry: true}

	changed := true
	for changed {
		changed = false
		for _, bid := range order[1:] {
			b := cfg.Blocks[bid]
			newDom := make(map[BlockID]bool)
			for _, ob := range order {
				newDom[ob] = true
			}
			for _, pred := range b.Preds {
				for ob := range newDom {
					if !doms[pred][ob] {
						delete(newDom, ob)
					}
				}
			}
			newDom[bid] = true
			if len(newDom) != len(doms[bid]) {
				doms[bid] = newDom
				changed = true
			}
		}
	}

	// Extract immediate dominator: largest proper dominator
	for _, bid := range order[1:] {
		propDoms := []BlockID{}
		for d := range doms[bid] {
			if d != bid {
				propDoms = append(propDoms, d)
			}
		}
		// idom = the one that all other proper dominators dominate (closest to bid)
		var best BlockID = -1
		for _, d := range propDoms {
			dominated := true
			for _, other := range propDoms {
				if other != d && !doms[d][other] {
					dominated = false
					break
				}
			}
			if dominated {
				best = d
			}
		}
		idom[bid] = best
	}

	return idom
}

// dominanceFrontier computes DF[b] for each block.
// DF[b] = {y : b dominates a predecessor of y, but b does not strictly dominate y}
func dominanceFrontier(cfg *CFG, idom map[BlockID]BlockID) map[BlockID][]BlockID {
	df := make(map[BlockID][]BlockID)
	for bid := range cfg.Blocks {
		df[bid] = nil
	}
	for bid, b := range cfg.Blocks {
		if len(b.Preds) < 2 {
			continue
		}
		// bid is a join point: check each predecessor
		for _, pred := range b.Preds {
			runner := pred
			for runner != idom[bid] {
				df[runner] = append(df[runner], bid)
				runner = idom[runner]
			}
		}
	}
	return df
}

// --- Live Variable Analysis ---

// LivenessResult[b] = (LiveIn[b], LiveOut[b])
type LivenessResult struct {
	LiveIn  map[BlockID]map[string]bool
	LiveOut map[BlockID]map[string]bool
}

// computeLiveness performs backward dataflow analysis for live variables.
func computeLiveness(cfg *CFG) *LivenessResult {
	liveIn := make(map[BlockID]map[string]bool)
	liveOut := make(map[BlockID]map[string]bool)
	for bid := range cfg.Blocks {
		liveIn[bid] = make(map[string]bool)
		liveOut[bid] = make(map[string]bool)
	}

	// Compute USE and DEF for each block.
	// USE[b] = variables used before defined in b
	// DEF[b] = variables defined in b
	use := make(map[BlockID]map[string]bool)
	def := make(map[BlockID]map[string]bool)
	for bid, b := range cfg.Blocks {
		use[bid] = make(map[string]bool)
		def[bid] = make(map[string]bool)
		for _, instr := range b.Instrs {
			for _, arg := range instr.Args {
				// Args that look like variable names (start with letter, not a number)
				if len(arg) > 0 && arg[0] >= 'a' && arg[0] <= 'z' && !def[bid][arg] {
					use[bid][arg] = true
				}
			}
			if instr.Dst != "" {
				def[bid][instr.Dst] = true
			}
		}
	}

	// Worklist: backward analysis — start from all blocks.
	worklist := make(map[BlockID]bool)
	for bid := range cfg.Blocks {
		worklist[bid] = true
	}

	for len(worklist) > 0 {
		// Pick any block from worklist
		var bid BlockID
		for b := range worklist {
			bid = b
			break
		}
		delete(worklist, bid)
		b := cfg.Blocks[bid]

		// LiveOut[b] = union of LiveIn[s] for all successors s
		newOut := make(map[string]bool)
		for _, s := range b.Succs {
			for v := range liveIn[s] {
				newOut[v] = true
			}
		}

		// LiveIn[b] = USE[b] ∪ (LiveOut[b] − DEF[b])
		newIn := make(map[string]bool)
		for v := range use[bid] {
			newIn[v] = true
		}
		for v := range newOut {
			if !def[bid][v] {
				newIn[v] = true
			}
		}

		// If LiveIn changed, add predecessors to worklist
		if !mapsEqual(newIn, liveIn[bid]) {
			liveIn[bid] = newIn
			for _, pred := range b.Preds {
				worklist[pred] = true
			}
		}
		liveOut[bid] = newOut
	}

	return &LivenessResult{LiveIn: liveIn, LiveOut: liveOut}
}

func mapsEqual(a, b map[string]bool) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if !b[k] {
			return false
		}
	}
	return true
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func main() {
	// Build a simple CFG for:
	//   entry: x = 1
	//   b1:    if cond → b2, b3
	//   b2:    y = x + 2
	//   b3:    y = x + 3
	//   exit:  z = y + x
	//
	// After SSA, `y` at exit becomes φ(y_b2, y_b3)
	// Live variable analysis tells us: at entry, `cond` and `x` are live.

	cfg := NewCFG()
	entry := cfg.AddBlock(0, "entry")
	b1 := cfg.AddBlock(1, "b1")
	b2 := cfg.AddBlock(2, "b2")
	b3 := cfg.AddBlock(3, "b3")
	exit := cfg.AddBlock(4, "exit")
	cfg.Entry = 0

	entry.Instrs = []*Instr{
		{Dst: "x", Op: "const", Args: []string{"1"}},
		{Op: "br", Args: []string{"1"}},
	}
	b1.Instrs = []*Instr{
		{Op: "cbr", Args: []string{"cond", "2", "3"}},
	}
	b2.Instrs = []*Instr{
		{Dst: "y", Op: "add", Args: []string{"x", "2"}},
		{Op: "br", Args: []string{"4"}},
	}
	b3.Instrs = []*Instr{
		{Dst: "y", Op: "add", Args: []string{"x", "3"}},
		{Op: "br", Args: []string{"4"}},
	}
	// In SSA form, y at exit is a phi node: y3 = phi(y1 from b2, y2 from b3)
	exit.Instrs = []*Instr{
		{Dst: "y3", Op: "phi", Args: []string{"y", "2", "y", "3"}}, // simplified
		{Dst: "z", Op: "add", Args: []string{"y3", "x"}},
		{Op: "ret", Args: []string{"z"}},
	}

	cfg.AddEdge(0, 1)
	cfg.AddEdge(1, 2)
	cfg.AddEdge(1, 3)
	cfg.AddEdge(2, 4)
	cfg.AddEdge(3, 4)

	fmt.Println("=== CFG ===")
	for _, bid := range []BlockID{0, 1, 2, 3, 4} {
		b := cfg.Blocks[bid]
		fmt.Printf("Block %d (%s): preds=%v succs=%v\n", bid, b.Name, b.Preds, b.Succs)
		for _, i := range b.Instrs {
			fmt.Println(i)
		}
	}

	idom := computeIDom(cfg)
	fmt.Println("\n=== Immediate Dominators ===")
	for _, bid := range []BlockID{0, 1, 2, 3, 4} {
		fmt.Printf("  idom(%s) = %s\n", cfg.Blocks[bid].Name, cfg.Blocks[idom[bid]].Name)
	}

	df := dominanceFrontier(cfg, idom)
	fmt.Println("\n=== Dominance Frontiers ===")
	for _, bid := range []BlockID{0, 1, 2, 3, 4} {
		names := make([]string, len(df[bid]))
		for i, did := range df[bid] {
			names[i] = cfg.Blocks[did].Name
		}
		fmt.Printf("  DF(%s) = {%s}\n", cfg.Blocks[bid].Name, strings.Join(names, ", "))
	}

	lr := computeLiveness(cfg)
	fmt.Println("\n=== Live Variables ===")
	for _, bid := range []BlockID{0, 1, 2, 3, 4} {
		name := cfg.Blocks[bid].Name
		liveIn := sortedKeys(lr.LiveIn[bid])
		liveOut := sortedKeys(lr.LiveOut[bid])
		fmt.Printf("  %s: LiveIn={%s}  LiveOut={%s}\n", name, strings.Join(liveIn, ","), strings.Join(liveOut, ","))
	}

	fmt.Println("\n=== Phi Node Placement ===")
	// Variables defined in multiple blocks need phi nodes at dominance frontiers.
	// 'y' is defined in b2 (id=2) and b3 (id=3).
	// DF(b2) = {exit(4)}, DF(b3) = {exit(4)}.
	// Therefore, phi node for 'y' is placed at exit.
	fmt.Println("  Variable 'y' is defined in blocks: b2, b3")
	fmt.Println("  DF(b2) ∪ DF(b3) = {exit}")
	fmt.Println("  → Insert phi node: y3 = φ(y_from_b2, y_from_b3) at exit")
}
```

### Go-specific considerations

**`go tool compile -S`**: Running `go tool compile -S file.go` dumps the assembly with SSA annotations. Each basic block is labeled `b1`, `b2`, etc. Phi nodes appear as `v12 = Phi <int> v8 v11` in the SSA dump. The `GOSSAFUNC=funcname go build` approach gives you the full HTML visualization with every pass.

**Reading Go's SSA source**: `go/src/cmd/compile/internal/ssa/` contains the SSA IR types. `Value` is an SSA value (an instruction result). `Block` is a basic block. `Func` contains the complete SSA representation of a function. The key files: `value.go` (instruction types), `block.go` (block kinds: `BlockPlain`, `BlockIf`, `BlockRet`), `dom.go` (dominance computation).

**Where phi nodes go in Go SSA**: Go's SSA inserts phi nodes during the `ssa.go`'s construction pass. When a variable is assigned in multiple predecessors, a phi node is synthesized. The phi nodes are cleaned up by the `copyelim` pass (which removes trivial phi nodes of the form `v = phi(v)`) and `nilcheckelim` (which removes redundant nil checks by tracking which values were already checked along a path).

## Implementation: Rust

```rust
// SSA construction and liveness analysis in Rust.
// We use a compact block representation and demonstrate
// the worklist-based dataflow algorithm.

use std::collections::{HashMap, HashSet, VecDeque};

type BlockId = usize;
type VarName = String;
type SsaName = String;

// --- CFG ---

#[derive(Debug, Clone)]
pub struct Instr {
    pub dst: Option<SsaName>,
    pub op: String,
    pub args: Vec<String>,
}

impl std::fmt::Display for Instr {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match &self.dst {
            Some(d) => write!(f, "  {} = {} {}", d, self.op, self.args.join(", ")),
            None    => write!(f, "  {} {}", self.op, self.args.join(", ")),
        }
    }
}

#[derive(Debug)]
pub struct Block {
    pub id: BlockId,
    pub name: String,
    pub instrs: Vec<Instr>,
    pub preds: Vec<BlockId>,
    pub succs: Vec<BlockId>,
}

#[derive(Debug)]
pub struct CFG {
    pub blocks: Vec<Block>,
    pub entry: BlockId,
}

impl CFG {
    pub fn new() -> Self {
        CFG { blocks: Vec::new(), entry: 0 }
    }

    pub fn add_block(&mut self, name: &str) -> BlockId {
        let id = self.blocks.len();
        self.blocks.push(Block { id, name: name.to_string(), instrs: Vec::new(), preds: Vec::new(), succs: Vec::new() });
        id
    }

    pub fn add_edge(&mut self, from: BlockId, to: BlockId) {
        self.blocks[from].succs.push(to);
        self.blocks[to].preds.push(from);
    }
}

// --- Liveness Analysis ---

#[derive(Debug)]
pub struct Liveness {
    pub live_in:  Vec<HashSet<VarName>>,
    pub live_out: Vec<HashSet<VarName>>,
}

fn is_var(s: &str) -> bool {
    s.chars().next().map(|c| c.is_ascii_alphabetic()).unwrap_or(false)
}

pub fn compute_liveness(cfg: &CFG) -> Liveness {
    let n = cfg.blocks.len();
    let mut live_in:  Vec<HashSet<VarName>> = vec![HashSet::new(); n];
    let mut live_out: Vec<HashSet<VarName>> = vec![HashSet::new(); n];

    // Compute USE and DEF per block.
    let mut uses: Vec<HashSet<VarName>> = vec![HashSet::new(); n];
    let mut defs: Vec<HashSet<VarName>> = vec![HashSet::new(); n];

    for b in &cfg.blocks {
        for instr in &b.instrs {
            for arg in &instr.args {
                if is_var(arg) && !defs[b.id].contains(arg) {
                    uses[b.id].insert(arg.clone());
                }
            }
            if let Some(d) = &instr.dst {
                defs[b.id].insert(d.clone());
            }
        }
    }

    // Backward worklist.
    let mut worklist: VecDeque<BlockId> = (0..n).collect();

    while let Some(bid) = worklist.pop_front() {
        let b = &cfg.blocks[bid];

        // LiveOut[b] = union LiveIn[s] for s in succs
        let new_out: HashSet<VarName> = b.succs.iter()
            .flat_map(|&s| live_in[s].iter().cloned())
            .collect();

        // LiveIn[b] = USE[b] ∪ (LiveOut[b] − DEF[b])
        let new_in: HashSet<VarName> = uses[bid].iter().cloned()
            .chain(new_out.iter().filter(|v| !defs[bid].contains(*v)).cloned())
            .collect();

        if new_in != live_in[bid] {
            live_in[bid] = new_in;
            for &pred in &b.preds {
                if !worklist.contains(&pred) {
                    worklist.push_back(pred);
                }
            }
        }
        live_out[bid] = new_out;
    }

    Liveness { live_in, live_out }
}

// --- Dominance (Simple O(n^2) for illustration) ---

pub fn compute_idom(cfg: &CFG) -> Vec<BlockId> {
    let n = cfg.blocks.len();
    // doms[b] = set of block ids that dominate b
    let all: HashSet<BlockId> = (0..n).collect();
    let mut doms: Vec<HashSet<BlockId>> = vec![all.clone(); n];
    doms[cfg.entry] = std::iter::once(cfg.entry).collect();

    let mut changed = true;
    while changed {
        changed = false;
        for bid in 0..n {
            if bid == cfg.entry { continue; }
            let b = &cfg.blocks[bid];
            if b.preds.is_empty() { continue; }
            // Intersection of pred dominators
            let mut new_dom: HashSet<BlockId> = doms[b.preds[0]].clone();
            for &pred in &b.preds[1..] {
                new_dom = new_dom.intersection(&doms[pred]).cloned().collect();
            }
            new_dom.insert(bid);
            if new_dom != doms[bid] {
                doms[bid] = new_dom;
                changed = true;
            }
        }
    }

    // idom[b] = the proper dominator of b that dominates all other proper dominators
    let mut idom = vec![0usize; n];
    for bid in 0..n {
        let proper: Vec<BlockId> = doms[bid].iter().copied().filter(|&d| d != bid).collect();
        // idom = element of proper_doms dominated by all others
        idom[bid] = proper.iter().copied().find(|&d| {
            proper.iter().all(|&other| other == d || doms[d].contains(&other))
        }).unwrap_or(cfg.entry);
    }
    idom
}

pub fn dominance_frontier(cfg: &CFG, idom: &[BlockId]) -> Vec<Vec<BlockId>> {
    let n = cfg.blocks.len();
    let mut df: Vec<Vec<BlockId>> = vec![Vec::new(); n];
    for bid in 0..n {
        let b = &cfg.blocks[bid];
        if b.preds.len() >= 2 {
            for &pred in &b.preds {
                let mut runner = pred;
                while runner != idom[bid] {
                    df[runner].push(bid);
                    runner = idom[runner];
                }
            }
        }
    }
    df
}

fn main() {
    let mut cfg = CFG::new();
    let entry = cfg.add_block("entry");
    let b1    = cfg.add_block("b1");
    let b2    = cfg.add_block("b2");
    let b3    = cfg.add_block("b3");
    let exit  = cfg.add_block("exit");

    cfg.blocks[entry].instrs = vec![
        Instr { dst: Some("x".into()), op: "const".into(), args: vec!["1".into()] },
        Instr { dst: None, op: "br".into(), args: vec!["b1".into()] },
    ];
    cfg.blocks[b1].instrs = vec![
        Instr { dst: None, op: "cbr".into(), args: vec!["cond".into(), "b2".into(), "b3".into()] },
    ];
    cfg.blocks[b2].instrs = vec![
        Instr { dst: Some("y".into()), op: "add".into(), args: vec!["x".into(), "2".into()] },
        Instr { dst: None, op: "br".into(), args: vec!["exit".into()] },
    ];
    cfg.blocks[b3].instrs = vec![
        Instr { dst: Some("y".into()), op: "add".into(), args: vec!["x".into(), "3".into()] },
        Instr { dst: None, op: "br".into(), args: vec!["exit".into()] },
    ];
    // After SSA construction: y3 = phi(y from b2, y from b3)
    cfg.blocks[exit].instrs = vec![
        Instr { dst: Some("y3".into()), op: "phi".into(), args: vec!["y".into(), "b2".into(), "y".into(), "b3".into()] },
        Instr { dst: Some("z".into()), op: "add".into(), args: vec!["y3".into(), "x".into()] },
        Instr { dst: None, op: "ret".into(), args: vec!["z".into()] },
    ];

    cfg.add_edge(entry, b1);
    cfg.add_edge(b1, b2);
    cfg.add_edge(b1, b3);
    cfg.add_edge(b2, exit);
    cfg.add_edge(b3, exit);

    println!("=== CFG ===");
    for b in &cfg.blocks {
        println!("Block {} ({}): preds={:?} succs={:?}", b.id, b.name, b.preds, b.succs);
        for i in &b.instrs { println!("{}", i); }
    }

    let idom = compute_idom(&cfg);
    println!("\n=== Immediate Dominators ===");
    for (bid, &id) in idom.iter().enumerate() {
        println!("  idom({}) = {}", cfg.blocks[bid].name, cfg.blocks[id].name);
    }

    let df = dominance_frontier(&cfg, &idom);
    println!("\n=== Dominance Frontiers ===");
    for (bid, frontier) in df.iter().enumerate() {
        let names: Vec<&str> = frontier.iter().map(|&id| cfg.blocks[id].name.as_str()).collect();
        println!("  DF({}) = {:?}", cfg.blocks[bid].name, names);
    }

    let lr = compute_liveness(&cfg);
    println!("\n=== Live Variables ===");
    for (bid, b) in cfg.blocks.iter().enumerate() {
        let mut li: Vec<&str> = lr.live_in[bid].iter().map(|s| s.as_str()).collect();
        let mut lo: Vec<&str> = lr.live_out[bid].iter().map(|s| s.as_str()).collect();
        li.sort(); lo.sort();
        println!("  {}: LiveIn={:?}  LiveOut={:?}", b.name, li, lo);
    }
}
```

### Rust-specific considerations

**`rustc --emit=mir`**: Run `rustc --emit=mir file.rs` to see MIR output. Each function shows its basic blocks (`bb0`, `bb1`, ...), statements within blocks (`StorageLive`, assignments, drops), and terminators (`SwitchInt`, `Call`, `Return`). MIR is where the borrow checker (the "NLL region inference") operates — it analyzes lifetimes as ranges of MIR locations.

**Why Rust's MIR is also SSA**: Technically MIR is not in strict SSA form (Rust uses "places" rather than pure SSA names), but it has SSA-like properties: each variable has a single dominant assignment in normal code. Phi-like merging happens implicitly through the MIR's variable-access model combined with the region inference.

**LLVM IR from Rust**: `RUSTFLAGS="--emit=llvm-ir" cargo build` produces `.ll` files. These are full LLVM SSA IR with explicit phi nodes. You can open these in LLVM's optimizer (`opt`) to run passes manually. A key insight: rustc generates conservative LLVM IR (e.g., lots of explicit bounds checks, many noalias attributes) and relies on LLVM's optimizer to clean it up. The `-C opt-level=3` flag controls how aggressively LLVM optimizes.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| SSA form | Custom SSA in `cmd/compile/internal/ssa` | MIR (Rust-level) + LLVM IR (machine-level) |
| Phi nodes | `ssa.OpPhi` values in Go SSA | Explicit `phi` in LLVM IR; implicit in MIR |
| Dominance computation | `dom.go` in ssa package | LLVM's `DominatorTree` (in LLVM backend) |
| Dataflow analysis target | Go SSA passes | MIR passes (borrow checker) + LLVM passes |
| Viewing IR | `GOSSAFUNC=f go build` → ssa.html | `--emit=mir`, `--emit=llvm-ir` |
| Borrow checking via SSA | N/A | MIR is the substrate for NLL borrow checking |

## Production War Stories

**The NLL borrow checker migration**: Rust's old borrow checker operated on the HIR (High-level IR). It was notoriously conservative: the checker thought a borrow lasted until the end of the lexical scope, not until the last use. This meant valid code was rejected. After 3 years of work, NLL (Non-Lexical Lifetimes) moved borrow checking to MIR, which has explicit control flow. The result: code like `let mut v = vec![1]; let x = &v[0]; v.push(2); println!("{}", x)` is now correctly rejected (the push invalidates the reference) with an error that points to the exact use — not the end of the scope. This required SSA-level control flow to prove.

**Go's escape analysis false negatives**: Go's escape analysis is conservative. If the compiler cannot prove a value stays on stack, it conservatively allocates on the heap. Common triggers: storing a local's address in a data structure that might escape; passing a pointer to a function the compiler cannot see through (external functions, interface calls). The SSA-level escape analysis pass (`src/cmd/compile/internal/escape/`) uses the same dataflow framework described above but for points-to analysis.

**V8's Turbofan SSA**: V8's Turbofan JIT uses a "sea of nodes" representation (similar to SSA but edges represent both data and control dependencies). When Turbofan was deployed (Chrome 41, 2015), it initially introduced performance regressions on specific benchmarks because the sea-of-nodes approach was harder to schedule efficiently than the old Crankshaft compiler's basic-block structure. The lesson: SSA form simplifies analysis but complicates scheduling; instruction schedulers on SSA IR are non-trivial.

## Complexity Analysis

| Analysis | Time | Space |
|---------|------|-------|
| Dominance tree (simple) | O(n²) iterations | O(n²) dominator sets |
| Dominance tree (Lengauer-Tarjan) | O(n · α(n)) | O(n) |
| Dominance frontiers | O(n²) edges in worst case | O(n) per block |
| Liveness analysis | O(n · |vars|) per iteration, O(n) iterations | O(n · |vars|) |
| SSA construction (Braun) | O(n · |vars|) | O(n · |vars|) for phi nodes |
| Reaching definitions | Same as liveness | Same as liveness |

## Common Pitfalls

**1. Misunderstanding phi node semantics.** A phi node `x3 = φ(x1, x2)` does not execute both branches — it takes the value from the predecessor block actually executed. Optimizations that treat phi arguments as simultaneously available (like hoisting code that uses phi inputs) are incorrect.

**2. Iterating the worklist in forward order for backward analysis.** Backward liveness analysis converges faster when blocks are processed in reverse postorder (from exits toward entry). Processing in arbitrary order still converges but requires more iterations.

**3. Not handling back edges in loops.** A loop `while (c) { x = x + 1 }` has a back edge from the loop body to the loop header. Liveness analysis must propagate along back edges — `x` is live in the body because it is used in the next iteration's condition. The worklist algorithm handles this naturally if you re-add predecessors when liveness changes.

**4. Confusing dominance with post-dominance.** A block A post-dominates B if every path from B to the exit passes through A (reverse direction). Post-dominance is used for control-dependence analysis (relevant for finding which branches a statement is "controlled by"). These are different trees.

**5. Treating SSA variables as regular variables after desctruction.** SSA destruction (converting SSA back to non-SSA for register allocation) replaces phi nodes with copy instructions along predecessor edges. After destruction, variables may be re-assigned — the SSA invariant no longer holds, and analyses written assuming SSA form will produce wrong results.

## Exercises

**Exercise 1** (30 min): Extend the liveness analysis to produce per-instruction live sets (not just per-block). For each instruction in a block, compute which variables are live immediately before that instruction. Print the live set alongside each instruction in the Go implementation.

**Exercise 2** (2–4h): Implement reaching definitions analysis (forward dataflow). For each use of a variable, print which assignment(s) can reach it. Test on the example CFG. Verify that in SSA form, every use has exactly one reaching definition.

**Exercise 3** (4–8h): Implement Braun's SSA construction algorithm on the TAC IR from section 02. Process blocks in dominator-tree order, maintain a `current_def` map per block and variable, and insert phi nodes at join points. Verify the output: every variable should have a unique subscript at each use, and every join point where a variable is defined along multiple paths should have a phi node.

**Exercise 4** (8–15h): Implement SSA-based constant propagation (SCCP — Sparse Conditional Constant Propagation). Use a two-valued lattice per SSA value: `Unknown` (top), `Const(n)`, `NotConst` (bottom). Propagate: if all phi arguments are the same constant, the phi is constant. If a branch condition is constant, mark the dead branch as unreachable and propagate through only the live branch. Compare the results with and without branch-sensitivity.

## Further Reading

### Foundational Papers
- **Cytron et al., 1991** — "Efficiently Computing Static Single Assignment Form and the Control Dependence Graph." The original SSA construction paper. Required reading for anyone implementing SSA.
- **Braun et al., 2013** — "Simple and Efficient Construction of Static Single Assignment Form." A simpler algorithm that is easier to implement correctly. Start here.
- **Wegman & Zadeck, 1991** — "Constant Propagation with Conditional Branches." Introduces SCCP — the gold standard for constant propagation on SSA.

### Books
- **Engineering a Compiler** (Cooper & Torczon) — Chapter 8: SSA Form and Its Uses. Chapter 9: Scalar Optimizations. The best textbook treatment of SSA-based optimizations.
- **Dragon Book** — Chapter 9: Machine-Independent Optimizations. Covers dataflow analysis frameworks formally.

### Production Code to Read
- `go/src/cmd/compile/internal/ssa/dom.go` — Dominance computation in Go's compiler
- `go/src/cmd/compile/internal/ssa/likelyadjust.go` — Live variable analysis in Go SSA
- `llvm-project/llvm/include/llvm/Analysis/DominanceInfo.h` — LLVM's dominance tree
- `compiler/rustc_mir_dataflow/src/` — Rust's MIR dataflow analysis framework

### Talks
- **"SSA-Based Compiler Design"** — Philip Wadler, POPL. Theoretical foundations.
- **"How LLVM Optimizes a Function"** — Johannes Doerfert, LLVM Developers' Meeting 2019. Walks through LLVM's SSA-based optimization passes in order.
