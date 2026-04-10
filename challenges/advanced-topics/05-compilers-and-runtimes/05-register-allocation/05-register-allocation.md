<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [graph-coloring, linear-scan, interference-graph, live-intervals, spilling, ssa-destruction, calling-convention]
languages: [go, rust]
estimated_reading_time: 70-85 min
bloom_level: analyze
prerequisites: [ssa-form-and-dataflow, optimization-passes]
papers: [Poletto-Sarkar-1999-linear-scan, Chaitin-1981-graph-coloring]
industry_use: [llvm, go-compiler, graalvm, cranelift]
language_contrast: medium
-->

# Register Allocation

> The difference between a good and mediocre compiler backend is almost entirely register allocation — it determines whether your variables live in registers (fast) or on the stack (slow), and no amount of optimization above the register allocator compensates for getting it wrong.

## Mental Model

After optimization passes, the IR has an unlimited number of virtual registers. The code generator must map these virtual registers to the limited set of physical registers (16 general-purpose on x86-64: `rax`, `rbx`, `rcx`, `rdx`, `rsi`, `rdi`, `rsp`, `rbp`, `r8`–`r15`). When there are more live values than registers, some must be "spilled" to the stack.

The register allocation problem is, in its general form, NP-complete (it is equivalent to graph k-coloring, which is NP-complete for k ≥ 3). Production compilers use heuristics that produce good results in practice:

- **Graph coloring** (Chaitin, 1981): Build an interference graph where each virtual register is a node and edges connect registers that are simultaneously live. Color the graph with k colors (physical registers). If no k-coloring exists, spill some variables to the stack and retry. Produces near-optimal code but is slow.
- **Linear scan** (Poletto & Sarkar, 1999): Sort live intervals by start position, sweep through them in order, allocating the earliest-expiring register first. O(n log n) in the number of intervals. Used in JIT compilers where allocation speed matters. Produces slightly worse code than graph coloring but is significantly faster to run.

Understanding register allocation lets you write code that the allocator can handle efficiently:
- Fewer simultaneously live variables = less register pressure = fewer spills
- Short live ranges = variables expire quickly, freeing registers for others
- Calling conventions: function calls kill caller-saved registers (`rax`, `rcx`, `rdx`, `rsi`, `rdi`, `r8`–`r11`). A function call inside a tight loop forces everything in caller-saved registers to be spilled and reloaded around the call.

## Core Concepts

### Live Intervals

A live interval for virtual register `v` is `[def, last_use]` — the range of instruction indices from where `v` is defined to its last use. Two registers **interfere** if their live intervals overlap. Registers that do not interfere can share a physical register.

In SSA form, each virtual register has exactly one definition, making live interval computation straightforward. After SSA destruction (removing phi nodes), live intervals may extend across blocks.

### Interference Graph

The interference graph has one node per virtual register and an edge between any two registers whose live intervals overlap. Graph coloring then becomes: assign physical registers (colors) to nodes such that no two adjacent nodes have the same color. If k colors are not sufficient, some nodes must be spilled.

**Spilling**: When a variable must be spilled, its value is stored to a stack slot. Each use is preceded by a load from the stack slot, and the result is a fresh temporary used only for that instruction. This splits the original long-range interval into many short-range intervals, each of which can be colored.

**Coalescing**: If a copy instruction `v2 = v1` exists and `v1` and `v2` do not interfere, they can be coalesced — assigned the same physical register, eliminating the copy. This is why you see `%rax` passed as the return value: the callee returns in `%rax`, and if the caller assigns the result to a variable, coalescing merges them.

### Linear Scan Algorithm

The linear scan algorithm:

1. Compute live intervals for all virtual registers
2. Sort intervals by start point
3. Maintain an "active" set of currently live intervals
4. For each interval in order:
   - Expire old intervals (remove those whose end < current start)
   - If free registers exist: assign the first free register
   - Else: spill the interval with the latest end point (or the current interval if it ends earlier)

Linear scan is the algorithm used by LLVM's "RegAllocFast" (for debug builds) and GraalVM's Graal JIT. For release builds, LLVM uses a more sophisticated greedy allocator.

### SSA Destruction

SSA phi nodes must be removed before register allocation. A phi node `x3 = φ(x1 from B1, x2 from B2)` is replaced by:
- In B1: `x3 = x1` (copy at the end of B1)
- In B2: `x3 = x2` (copy at the end of B2)

These copies are then coalesced (or spilled) by the register allocator. The challenge is that naively inserting copies can create additional interference. The "parallel copy" problem requires care: if two phi nodes in the same block swap variables (`x = φ(y, ...)` and `y = φ(x, ...)`), naive sequential copies produce wrong results. This requires a temporary for the swap.

## Implementation: Go

```go
package main

import (
	"fmt"
	"sort"
)

// Linear scan register allocator for a toy IR.
// Input: a linear sequence of instructions with virtual registers (vN)
// Output: allocation map (vN → physical reg or stack slot)

// Physical registers available (simplified x86-64 subset)
var physRegs = []string{"rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10"}

const numRegs = len(physRegs)

// --- Live Interval ---

type Interval struct {
	VReg  string // virtual register name
	Start int    // first instruction that defines it
	End   int    // last instruction that uses it
}

// --- Toy Instruction ---

type Instr struct {
	Def  string // virtual register defined (empty if none)
	Uses []string
	Op   string
}

func (i *Instr) String() string {
	if i.Def != "" {
		return fmt.Sprintf("  %-5s = %-8s %s", i.Def, i.Op, joinStrings(i.Uses))
	}
	return fmt.Sprintf("         %-8s %s", i.Op, joinStrings(i.Uses))
}

func joinStrings(ss []string) string {
	result := ""
	for j, s := range ss {
		if j > 0 {
			result += ", "
		}
		result += s
	}
	return result
}

// --- Compute Live Intervals ---
// For a single basic block (no control flow), live intervals are simple:
// start = instruction index of definition, end = last instruction index that uses it.

func computeIntervals(instrs []Instr) []Interval {
	defAt := make(map[string]int)  // vreg → index of definition
	lastUse := make(map[string]int) // vreg → index of last use

	for idx, instr := range instrs {
		for _, use := range instr.Uses {
			if len(use) > 0 && use[0] == 'v' { // virtual register
				lastUse[use] = idx
				if _, defined := defAt[use]; !defined {
					defAt[use] = idx // live-in: treat first use as start
				}
			}
		}
		if instr.Def != "" {
			if _, exists := defAt[instr.Def]; !exists {
				defAt[instr.Def] = idx
			}
		}
	}

	// Build interval list
	intervals := make([]Interval, 0)
	for vreg, start := range defAt {
		end := lastUse[vreg]
		if end < start {
			end = start // defined but never used: interval = [def, def]
		}
		intervals = append(intervals, Interval{VReg: vreg, Start: start, End: end})
	}

	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].Start < intervals[j].Start
	})
	return intervals
}

// --- Linear Scan Allocator ---

type Allocation struct {
	Reg      string // physical register, or "" if spilled
	Spilled  bool
	StackSlot int  // stack slot index if spilled
}

type Allocator struct {
	freeRegs  []string           // available physical registers
	active    []Interval          // currently active intervals (sorted by end)
	alloc     map[string]Allocation // vreg → allocation
	spillCount int
}

func NewAllocator() *Allocator {
	regs := make([]string, len(physRegs))
	copy(regs, physRegs)
	return &Allocator{
		freeRegs: regs,
		alloc:    make(map[string]Allocation),
	}
}

func (a *Allocator) popFreeReg() (string, bool) {
	if len(a.freeRegs) == 0 {
		return "", false
	}
	reg := a.freeRegs[0]
	a.freeRegs = a.freeRegs[1:]
	return reg, true
}

func (a *Allocator) releaseReg(reg string) {
	a.freeRegs = append(a.freeRegs, reg)
}

func (a *Allocator) expireOldIntervals(current Interval) {
	remaining := a.active[:0]
	for _, iv := range a.active {
		if iv.End >= current.Start {
			remaining = append(remaining, iv)
		} else {
			// This interval has ended: release its register.
			if alloc, ok := a.alloc[iv.VReg]; ok && !alloc.Spilled {
				a.releaseReg(alloc.Reg)
			}
		}
	}
	a.active = remaining
}

func (a *Allocator) spillAtInterval(current Interval) {
	// Spill the interval that ends latest (gives back the most useful register).
	// If the current interval ends earlier, spill it instead.
	if len(a.active) == 0 {
		a.spillCurrent(current)
		return
	}
	latest := a.active[len(a.active)-1]
	if latest.End > current.End {
		// Spill `latest`, give its register to `current`.
		latestAlloc := a.alloc[latest.VReg]
		a.alloc[current.VReg] = Allocation{Reg: latestAlloc.Reg}
		a.spillCurrent(latest)
		// Remove `latest` from active, add `current`.
		a.active = a.active[:len(a.active)-1]
		a.addActive(current)
	} else {
		a.spillCurrent(current)
	}
}

func (a *Allocator) spillCurrent(iv Interval) {
	a.alloc[iv.VReg] = Allocation{Spilled: true, StackSlot: a.spillCount}
	a.spillCount++
}

func (a *Allocator) addActive(iv Interval) {
	// Insert sorted by end point (for efficient expiry and spill decisions).
	pos := sort.Search(len(a.active), func(i int) bool {
		return a.active[i].End > iv.End
	})
	a.active = append(a.active, Interval{})
	copy(a.active[pos+1:], a.active[pos:])
	a.active[pos] = iv
}

func (a *Allocator) Allocate(intervals []Interval) map[string]Allocation {
	for _, iv := range intervals {
		a.expireOldIntervals(iv)
		reg, ok := a.popFreeReg()
		if !ok {
			a.spillAtInterval(iv)
		} else {
			a.alloc[iv.VReg] = Allocation{Reg: reg}
			a.addActive(iv)
		}
	}
	return a.alloc
}

func main() {
	// A linear basic block with virtual registers.
	// Models: a = b + c; d = a * e; f = d - g; return f
	instrs := []Instr{
		{Def: "v1", Op: "load",  Uses: []string{"b"}},    // 0: v1 = load b
		{Def: "v2", Op: "load",  Uses: []string{"c"}},    // 1: v2 = load c
		{Def: "v3", Op: "add",   Uses: []string{"v1", "v2"}}, // 2: v3 = v1 + v2
		{Def: "v4", Op: "load",  Uses: []string{"e"}},    // 3: v4 = load e
		{Def: "v5", Op: "mul",   Uses: []string{"v3", "v4"}}, // 4: v5 = v3 * v4
		{Def: "v6", Op: "load",  Uses: []string{"g"}},    // 5: v6 = load g
		{Def: "v7", Op: "sub",   Uses: []string{"v5", "v6"}}, // 6: v7 = v5 - v6
		{Op: "ret", Uses: []string{"v7"}},                 // 7: ret v7
	}

	fmt.Println("=== Instructions ===")
	for i, instr := range instrs {
		fmt.Printf("%d: %s\n", i, &instr)
	}

	intervals := computeIntervals(instrs)
	fmt.Println("\n=== Live Intervals ===")
	fmt.Printf("%-8s %-8s %-8s\n", "VReg", "Start", "End")
	for _, iv := range intervals {
		fmt.Printf("%-8s %-8d %-8d\n", iv.VReg, iv.Start, iv.End)
	}

	allocator := NewAllocator()
	alloc := allocator.Allocate(intervals)
	fmt.Println("\n=== Register Allocation ===")
	for vreg, a := range alloc {
		if a.Spilled {
			fmt.Printf("  %-8s → stack[%d]\n", vreg, a.StackSlot)
		} else {
			fmt.Printf("  %-8s → %s\n", vreg, a.Reg)
		}
	}

	fmt.Printf("\nSpill count: %d (out of %d virtual regs)\n",
		allocator.spillCount, len(intervals))
}
```

### Go-specific considerations

**Go's register-based calling convention (Go 1.17+)**: Before Go 1.17, the Go compiler used a stack-based calling convention — all function arguments and return values were passed on the stack. This was simple but slow: every function call required memory writes and reads for parameters. Go 1.17 introduced a register-based calling convention for the first 9 integer arguments and first 9 floating-point arguments. The impact on register allocation: function arguments now live in specific registers (`AX`, `BX`, `CX`, etc.) at the call site, and the register allocator must honor these fixed assignments.

**Inspecting register allocation output**: `go tool compile -S file.go` shows the final assembly. Look for `MOVQ reg, disp(SP)` instructions — these are spills (storing a register to the stack). `MOVQ disp(SP), reg` — these are reloads. Many spill/reload pairs on a hot path indicate high register pressure; consider refactoring to reduce the number of simultaneously live variables.

**`//go:noescape` and register pressure**: Functions marked `//go:noescape` tell the compiler that none of the pointer arguments escape to the heap. This allows the compiler to keep pointed-to values in registers rather than loading them from heap addresses, improving register utilization.

## Implementation: Rust

```rust
// Linear scan register allocator in Rust.
// Same algorithm as the Go implementation, demonstrating Rust idioms:
// - Struct methods instead of free functions
// - Result type for error handling
// - Ownership-safe active set management

use std::collections::HashMap;

const PHYS_REGS: &[&str] = &["rax", "rcx", "rdx", "rsi", "rdi", "r8", "r9", "r10"];

#[derive(Debug, Clone)]
pub struct Interval {
    pub vreg: String,
    pub start: usize,
    pub end: usize,
}

#[derive(Debug, Clone)]
pub enum Allocation {
    Register(String),
    StackSlot(usize),
}

pub struct LinearScanAllocator {
    free_regs: Vec<String>,
    active: Vec<Interval>, // sorted by end point
    allocations: HashMap<String, Allocation>,
    spill_count: usize,
}

impl LinearScanAllocator {
    pub fn new() -> Self {
        LinearScanAllocator {
            free_regs: PHYS_REGS.iter().map(|s| s.to_string()).collect(),
            active: Vec::new(),
            allocations: HashMap::new(),
            spill_count: 0,
        }
    }

    fn expire_old(&mut self, current_start: usize) {
        let mut released = Vec::new();
        self.active.retain(|iv| {
            if iv.end < current_start {
                // Return register to free pool
                if let Some(Allocation::Register(r)) = self.allocations.get(&iv.vreg) {
                    released.push(r.clone());
                }
                false
            } else {
                true
            }
        });
        self.free_regs.extend(released);
    }

    fn add_active(&mut self, iv: Interval) {
        // Insert sorted by end (ascending)
        let pos = self.active.partition_point(|existing| existing.end <= iv.end);
        self.active.insert(pos, iv);
    }

    fn spill_at_interval(&mut self, current: Interval) {
        // Spill the interval with the latest end point (or current if it ends earlier)
        if let Some(latest) = self.active.last().cloned() {
            if latest.end > current.end {
                // Take the register from `latest`, give it to `current`
                if let Some(Allocation::Register(reg)) = self.allocations.get(&latest.vreg).cloned() {
                    self.allocations.insert(current.vreg.clone(), Allocation::Register(reg));
                    // Spill `latest`
                    let slot = self.spill_count;
                    self.spill_count += 1;
                    self.allocations.insert(latest.vreg.clone(), Allocation::StackSlot(slot));
                    self.active.pop();
                    self.add_active(current);
                    return;
                }
            }
        }
        // Spill current
        let slot = self.spill_count;
        self.spill_count += 1;
        self.allocations.insert(current.vreg.clone(), Allocation::StackSlot(slot));
    }

    pub fn allocate(&mut self, mut intervals: Vec<Interval>) -> &HashMap<String, Allocation> {
        // Sort by start point
        intervals.sort_by_key(|iv| iv.start);

        for iv in intervals {
            self.expire_old(iv.start);

            if self.free_regs.is_empty() {
                self.spill_at_interval(iv);
            } else {
                let reg = self.free_regs.remove(0);
                self.allocations.insert(iv.vreg.clone(), Allocation::Register(reg));
                self.add_active(iv);
            }
        }

        &self.allocations
    }
}

// --- Demonstrating register pressure in Rust ---
// The following shows how register pressure appears in asm output.

// To see register allocation decisions:
// RUSTFLAGS="--emit=asm" cargo build --release
// Look for "push"/"pop" and "mov ... [rsp+N]" — these are spills.

fn high_register_pressure(a: i64, b: i64, c: i64, d: i64, e: i64, f: i64) -> i64 {
    // All 6 arguments live simultaneously with intermediate results.
    // This creates high register pressure.
    let t1 = a + b;
    let t2 = c * d;
    let t3 = e - f;
    let t4 = t1 * t2; // t1, t2, t3 all live here
    let t5 = t4 + t3; // t3 still live
    t5
}

fn low_register_pressure(a: i64, b: i64, c: i64, d: i64, e: i64, f: i64) -> i64 {
    // Same computation but structured to minimize simultaneous live ranges.
    // Each intermediate dies as soon as it is used.
    (a + b) * (c * d) + (e - f)
}

fn main() {
    // Build live intervals for the same example as Go
    let intervals = vec![
        Interval { vreg: "v1".into(), start: 0, end: 2 },
        Interval { vreg: "v2".into(), start: 1, end: 2 },
        Interval { vreg: "v3".into(), start: 2, end: 4 },
        Interval { vreg: "v4".into(), start: 3, end: 4 },
        Interval { vreg: "v5".into(), start: 4, end: 6 },
        Interval { vreg: "v6".into(), start: 5, end: 6 },
        Interval { vreg: "v7".into(), start: 6, end: 7 },
    ];

    println!("=== Live Intervals ===");
    println!("{:<8} {:<8} {:<8}", "VReg", "Start", "End");
    for iv in &intervals {
        println!("{:<8} {:<8} {:<8}", iv.vreg, iv.start, iv.end);
    }

    let mut allocator = LinearScanAllocator::new();
    let alloc = allocator.allocate(intervals);

    println!("\n=== Register Allocation ===");
    let mut sorted_allocs: Vec<_> = alloc.iter().collect();
    sorted_allocs.sort_by_key(|(k, _)| k.as_str());
    for (vreg, a) in &sorted_allocs {
        match a {
            Allocation::Register(r) => println!("  {:<8} → {}", vreg, r),
            Allocation::StackSlot(n) => println!("  {:<8} → stack[{}]", vreg, n),
        }
    }
    println!("\nSpill count: {}", allocator.spill_count);

    // Demonstrate register pressure difference
    println!("\n--- Register Pressure Demo ---");
    let r1 = high_register_pressure(1, 2, 3, 4, 5, 6);
    let r2 = low_register_pressure(1, 2, 3, 4, 5, 6);
    println!("high_register_pressure: {} | low: {}", r1, r2);
    println!("(Check asm output: high_pressure may have spills, low_pressure should not)");
}
```

### Rust-specific considerations

**LLVM's register allocator choices**: LLVM has three register allocators:
- `RegAllocFast`: used at `opt-level=0` (debug). Simple, fast, produces many spills.
- `RegAllocGreedy`: used at `opt-level >= 1`. Greedy graph-coloring-inspired algorithm. Splits live ranges to reduce spills. This is why release builds can have dramatically different register usage than debug builds.
- `RegAllocBasic`: the PBQP-based allocator (less common).

**Seeing spills in Rust asm**: Look for `movq %reg, N(%rsp)` (spill to stack) and `movq N(%rsp), %reg` (reload from stack). In the output of `RUSTFLAGS="--emit=asm" cargo build --release`, a function with many spills is a sign of high register pressure. Consider refactoring to reduce the number of simultaneously live values, or splitting the function into smaller pieces.

**Fat pointers and register pressure**: Rust's `dyn Trait` (trait objects) and slices use fat pointers — two-word values (pointer + vtable/length). Each fat pointer consumes two registers. A function that passes several `&[T]` slices has high register pressure from the fat pointer pairs. Consider passing `*const T, usize` separately in extremely hot paths to reduce pressure.

**Cranelift as an alternative**: Cranelift (used by Wasmtime and Wasmer) is a code generator written in Rust that implements a simplified register allocator called regalloc2. It is designed for JIT compilation — fast allocation with acceptable code quality. The algorithm is an SSA-based interference graph coloring with a fast heuristic for splitting live ranges.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Register allocator | Custom linear-scan-style in `cmd/compile/internal/regalloc` | LLVM RegAllocGreedy (release) / RegAllocFast (debug) |
| Calling convention | Register-based since Go 1.17 (9 int args in regs) | System V AMD64 ABI (6 int args in regs) |
| Spill visibility | `go tool compile -S` — look for `MOVQ SP+N` | `--emit=asm` — look for `movq %reg, N(%rsp)` |
| Coalescing | Done via copy elimination in Go SSA | LLVM coalesces during RA |
| Callee-saved regs | Go saves/restores `rbp`, others as needed | Standard callee-saved: rbp, rbx, r12–r15 |
| Goroutine stack growth | Go's stack growth requires saving all regs → spill to goroutine stack | No equivalent (stack is fixed-size by default) |

## Production War Stories

**Go's calling convention change (Go 1.17)**: The switch from stack-based to register-based calling convention was a multi-year effort. Benchmarks showed 5–15% overall improvement in CPU time, with some benchmarks improving 30–40%. The tricky part: goroutines can be moved between OS threads, and the garbage collector must find all pointer-typed values in registers during a GC stop (called "stack scanning"). The compiler had to generate precise register maps at every GC safe point showing which registers hold live pointers.

**LLVM's "impossible graph" register allocation**: In 2013, a bug was reported where LLVM's greedy register allocator entered an infinite loop on a specific function with an extremely high-degree interference graph. The function had 200+ simultaneously live values and the allocator's "live range splitting" heuristic kept splitting and re-splitting without making progress. The fix was to add a fallback path that simply spills everything above a threshold. The lesson: register allocation heuristics can have pathological cases; production allocators need fallback strategies.

**The JVM and elimination of interpreted calls**: HotSpot's JIT uses a graph-coloring register allocator (C2 compiler) that is sophisticated enough to elide stack frame allocation for leaf methods. When a Java method calls no other methods and fits in registers, the JIT allocates no stack frame — the function is essentially "free" in terms of call overhead. This is why tight Java inner loops can be competitive with C after warmup.

**GraalVM's register allocator and LLVM IR**: GraalVM's compiler uses a linear scan allocator (Wimmer & Franz, 2010) that operates on SSA form and handles phi-induced copies efficiently. When compared to C2 (HotSpot's graph-coloring allocator), the linear scan allocator sometimes produces slightly more spills but compiles 3–5x faster. For JIT compilation, allocation speed matters — you cannot afford to spend 100ms doing graph coloring if you need to start executing compiled code in 10ms.

## Complexity Analysis

| Algorithm | Time | Spill Quality |
|----------|------|---------------|
| Graph coloring (Chaitin) | O(n² · k) where n=vars, k=regs | Near-optimal |
| Linear scan (Poletto-Sarkar) | O(n log n) | Good (10–20% more spills than graph coloring) |
| RegAllocGreedy (LLVM) | O(n log n) amortized | Near graph-coloring quality |
| RegAllocFast (LLVM) | O(n) | Poor (debug builds) |

## Common Pitfalls

**1. Ignoring calling convention register kills.** A function call kills all caller-saved registers (`rax`, `rcx`, `rdx`, `rsi`, `rdi`, `r8`–`r11` on x86-64 System V). Any virtual register whose live interval spans a call must either be spilled (expensive) or placed in a callee-saved register (`rbx`, `rbp`, `r12`–`r15`). In a tight loop with a function call, this is a significant source of register pressure.

**2. Long live ranges from unnecessary variable retention.** In Rust, dropping a value at the end of a scope (not immediately when last used) extends its live range. This can increase register pressure. Rust's `drop()` function can force early drop. In Go, the same issue arises with `defer` — all values referenced by deferred functions are kept alive until the deferred call executes.

**3. Register pressure from large structs.** Passing a large struct by value creates a copy and keeps multiple fields simultaneously live. Passing by reference (pointer) collapses to a single register. In hot paths, prefer `*T` over `T` for large types.

**4. Phi node copies and register pressure at join points.** After SSA destruction, phi nodes become copies. If two branches both define 8 variables and all 8 merge at a join point via phi nodes, the join point has all 16 versions live simultaneously (8 sources + 8 destinations for the copies). This is a known cause of high register pressure at loop headers.

**5. Assuming the allocator sees across function boundaries.** Without LTO (Link-Time Optimization), the register allocator cannot coalesce across function boundaries. A function that takes 6 integer arguments gets them in the calling-convention order (`rdi`, `rsi`, `rdx`, `rcx`, `r8`, `r9` on System V), regardless of what registers the caller had them in. LTO enables cross-function optimization but significantly increases compile time.

## Exercises

**Exercise 1** (30 min): Extend the live interval computation in Go to handle multiple basic blocks. A variable's live interval should span from its definition block to its last-use block, with the interval extended through any block on a path between the two. Test on the CFG from section 03.

**Exercise 2** (2–4h): Add register coalescing to the linear scan allocator. After allocation, scan for `v_dst = copy v_src` instructions where `v_dst` and `v_src` have the same physical register assignment (or one is unallocated). Mark these copies as eliminated. Count how many copies you can eliminate.

**Exercise 3** (4–8h): Implement graph-coloring register allocation for the same toy IR. Build the interference graph (edges between intervals with overlapping ranges). Use a greedy coloring algorithm: process nodes in order of decreasing degree, assign the first available color. Compare the spill count with linear scan on a set of test programs with varying register pressure.

**Exercise 4** (8–15h): Implement SSA destruction with parallel copy resolution. Given a basic block with multiple phi nodes at the top, replace each phi with copies at the end of predecessor blocks. Handle the swap case: if two phis in the same block form a cycle (each phi's argument is another phi's result from the same block), use a temporary to break the cycle. Verify correctness by running an interpreter before and after SSA destruction.

## Further Reading

### Foundational Papers
- **Chaitin et al., 1981** — "Register Allocation via Coloring." The original graph-coloring register allocation paper. The algorithm is still the basis for most production allocators.
- **Poletto & Sarkar, 1999** — "Linear Scan Register Allocation." The O(n log n) algorithm used in JIT compilers. Simpler and faster than graph coloring.
- **Wimmer & Franz, 2010** — "Linear Scan Register Allocation on SSA Form." An improvement to linear scan that exploits SSA properties to reduce spills.

### Books
- **Engineering a Compiler** (Cooper & Torczon) — Chapter 13: Register Allocation.
- **Dragon Book** (Aho et al.) — Chapter 8.8: Register Allocation and Assignment.

### Production Code to Read
- `go/src/cmd/compile/internal/regalloc/regalloc.go` — Go's register allocator
- `llvm-project/llvm/lib/CodeGen/RegAllocGreedy.cpp` — LLVM's greedy allocator (3000+ lines)
- `cranelift/codegen/src/regalloc/` — Cranelift's regalloc2 implementation
- `graal/compiler/src/jdk.graal.compiler/src/jdk/graal/compiler/lir/alloc/` — GraalVM's linear scan allocator

### Talks
- **"Register Allocation: What GCC Does"** — Diego Novillo, GCC Summit 2007.
- **"LLVM's New Register Allocator"** — Andrew Trick, LLVM Developers' Meeting 2011. Explains the greedy allocator and live range splitting.
- **"Cranelift: A New Code Generator for WebAssembly"** — Benjamin Bouvier, WebAssembly Summit 2020.
