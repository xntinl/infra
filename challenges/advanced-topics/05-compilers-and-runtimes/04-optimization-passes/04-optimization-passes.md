<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [constant-folding, constant-propagation, sccp, dead-code-elimination, function-inlining, licm, strength-reduction, reading-asm-output]
languages: [go, rust]
estimated_reading_time: 75-90 min
bloom_level: analyze
prerequisites: [ssa-form-and-dataflow, ast-and-ir-design]
papers: [Wegman-Zadeck-1991-SCCP, Allen-Cocke-1972-optimization-taxonomy]
industry_use: [llvm, go-compiler, graalvm, v8-turbofan]
language_contrast: high
-->

# Optimization Passes

> The optimizer does not make your code faster — it makes the code the compiler actually executes match what you intended, eliminating the redundancies that programmers naturally write and machine code cannot afford.

## Mental Model

A compiler optimization is a transformation that preserves program semantics while changing the representation. Every optimization pass has a prerequisite (a property of the IR it requires to work correctly) and an output (a modified IR that is smaller, faster, or easier for subsequent passes). Passes compose: constant folding creates opportunities for dead code elimination; inlining creates opportunities for constant propagation through inlined arguments; LICM creates opportunities for strength reduction.

The key insight for writing code that optimizes well: **compilers can only optimize code they can reason about**. If the compiler cannot prove that `p` and `q` point to different memory locations, it cannot reorder loads and stores. If the compiler cannot see the body of a function (external, virtual call, function pointer), it cannot inline it. If a loop has an unknown trip count and potential aliasing, it cannot vectorize. Every piece of information you give the compiler — `const`, final types, `noalias` hints, sealed interfaces — expands the set of transformations it can apply.

The optimization passes described here are a core subset found in every production compiler. Understanding what each pass does (and when it fires) lets you read assembly output not as magic, but as the predictable result of a defined pipeline.

## Core Concepts

### Constant Folding and Propagation (SCCP)

**Constant folding**: Evaluate constant expressions at compile time. `2 + 3` → `5`. `true && false` → `false`. This is syntactically local — it requires only the current expression.

**Constant propagation**: If `x = 5` and `y = x + 1`, replace uses of `y` with `6`. This requires dataflow: knowing the value of `x` at the use point.

**SCCP (Sparse Conditional Constant Propagation)**: Combines constant propagation with branch folding. If the condition of an `if` evaluates to a constant, the dead branch is marked unreachable and constants do not propagate through it. "Sparse" means we only process instructions whose inputs change (using use-def chains from SSA), not all instructions. "Conditional" means we track whether branches are executable, not just whether values are constants.

SCCP uses a three-value lattice per SSA value:
- `Undef` (top): the value is not yet determined
- `Const(n)`: the value is definitely the constant `n`
- `OverDefined` (bottom): the value might be anything

A phi node becomes `Const(n)` only if all executable predecessors supply the same constant. Otherwise it becomes `OverDefined`.

### Dead Code Elimination (DCE)

An instruction is dead if its result is never used and it has no side effects (no stores, no calls with observable effects). DCE with SSA is simple: mark all instructions with side effects as live, then propagate liveness backward through use-def chains. Any instruction not marked live is dead and can be removed.

SCCP and DCE interact: SCCP marks branches to unreachable blocks, making the code in those blocks dead. DCE removes it. Together they eliminate entire dead branches, not just individual instructions.

### Function Inlining

Inlining replaces a call site with a copy of the callee's body. Benefits: eliminates call overhead (register setup, stack frame, return), exposes the callee's code to the caller's optimizations (constant propagation can now see the callee's constants), enables further inlining of the callee's callees.

Costs: code size growth (one copy per call site), increased register pressure (the callee's registers now compete with the caller's), longer compile time. Inlining heuristics balance these:
- **Size budget**: only inline functions below a threshold (Go: 80 nodes; LLVM: varies by optimization level)
- **Call graph**: prefer inlining hot paths (frequently called) over cold paths
- **Recursive functions**: inlining recursion requires unrolling — compilers typically refuse unless the depth is bounded and small

### Loop Invariant Code Motion (LICM)

An expression is loop-invariant if its value does not change across iterations. LICM hoists loop-invariant computations to the loop's pre-header (a block before the loop that always executes before the loop body).

```
// Before LICM
for i := 0; i < n; i++ {
    x[i] = a * b   // a * b is loop-invariant
}
// After LICM
tmp := a * b
for i := 0; i < n; i++ {
    x[i] = tmp
}
```

LICM requires the hoisted expression to have no side effects and to dominate the loop's exit (so moving it out of the loop does not cause it to execute when it shouldn't have). The first condition is easy for pure arithmetic; the second requires loop-closed SSA form.

### Strength Reduction

Replace expensive operations with cheaper equivalents. Classic example: replace multiplication by a power of two with a shift (`x * 8` → `x << 3`). In loops, replace multiplication by the loop index with accumulated addition:

```
// Before: index requires a multiply each iteration
for i := 0; i < n; i++ {
    ptr[i * stride] = ...
}
// After: accumulated by stride each iteration (strength reduction)
offset := 0
for i := 0; i < n; i++ {
    ptr[offset] = ...
    offset += stride
}
```

## Implementation: Go

```go
package main

import (
	"fmt"
	"math"
)

// We implement constant folding and a basic dead-code pass on
// the TAC IR from section 02. Then we show how to read the Go
// compiler's own output to verify these passes fired.

// --- TAC from section 02 (abbreviated types) ---

type TACOp int

const (
	TACConst TACOp = iota
	TACAdd
	TACSub
	TACMul
	TACDiv
	TACCopy
	TACNeg
	TACLabel
	TACJump
	TACJumpIfNot
	TACReturn
)

type TACInstr struct {
	Op    TACOp
	Dst   string
	Src1  string
	Src2  string
	Label string
	Const int64
}

func (i *TACInstr) String() string {
	switch i.Op {
	case TACConst:
		return fmt.Sprintf("  %s = %d", i.Dst, i.Const)
	case TACAdd:
		return fmt.Sprintf("  %s = %s + %s", i.Dst, i.Src1, i.Src2)
	case TACSub:
		return fmt.Sprintf("  %s = %s - %s", i.Dst, i.Src1, i.Src2)
	case TACMul:
		return fmt.Sprintf("  %s = %s * %s", i.Dst, i.Src1, i.Src2)
	case TACDiv:
		return fmt.Sprintf("  %s = %s / %s", i.Dst, i.Src1, i.Src2)
	case TACCopy:
		return fmt.Sprintf("  %s = %s", i.Dst, i.Src1)
	case TACNeg:
		return fmt.Sprintf("  %s = -%s", i.Dst, i.Src1)
	case TACLabel:
		return fmt.Sprintf("%s:", i.Label)
	case TACJump:
		return fmt.Sprintf("  goto %s", i.Label)
	case TACJumpIfNot:
		return fmt.Sprintf("  ifnot %s goto %s", i.Src1, i.Label)
	case TACReturn:
		return fmt.Sprintf("  return %s", i.Src1)
	}
	return "  <unknown>"
}

// --- Constant Folding Pass ---
// Tracks known constant values for each temp.
// When a binary op has two constant inputs, replaces the instruction with a TACConst.

func constantFolding(instrs []*TACInstr) []*TACInstr {
	consts := make(map[string]int64) // temp → known constant value
	result := make([]*TACInstr, 0, len(instrs))

	for _, instr := range instrs {
		switch instr.Op {
		case TACConst:
			consts[instr.Dst] = instr.Const
			result = append(result, instr)

		case TACAdd, TACSub, TACMul, TACDiv:
			v1, ok1 := consts[instr.Src1]
			v2, ok2 := consts[instr.Src2]
			if ok1 && ok2 {
				// Both operands are constants — fold the operation.
				var folded int64
				switch instr.Op {
				case TACAdd:
					folded = v1 + v2
				case TACSub:
					folded = v1 - v2
				case TACMul:
					folded = v1 * v2
				case TACDiv:
					if v2 == 0 {
						// Do not fold division by zero — preserve the runtime panic.
						result = append(result, instr)
						continue
					}
					folded = v1 / v2
				}
				consts[instr.Dst] = folded
				result = append(result, &TACInstr{Op: TACConst, Dst: instr.Dst, Const: folded})
			} else {
				result = append(result, instr)
			}

		case TACNeg:
			if v, ok := consts[instr.Src1]; ok {
				consts[instr.Dst] = -v
				result = append(result, &TACInstr{Op: TACConst, Dst: instr.Dst, Const: -v})
			} else {
				result = append(result, instr)
			}

		case TACCopy:
			if v, ok := consts[instr.Src1]; ok {
				consts[instr.Dst] = v
				result = append(result, &TACInstr{Op: TACConst, Dst: instr.Dst, Const: v})
			} else {
				result = append(result, instr)
			}

		default:
			result = append(result, instr)
		}
	}

	return result
}

// --- Dead Code Elimination Pass ---
// Mark instructions whose results are used; remove unmarked non-terminal instructions.

func deadCodeElimination(instrs []*TACInstr) []*TACInstr {
	// 1. Collect used temps: any temp that appears as Src1, Src2, or in JumpIfNot.
	used := make(map[string]bool)
	for _, instr := range instrs {
		if instr.Src1 != "" {
			used[instr.Src1] = true
		}
		if instr.Src2 != "" {
			used[instr.Src2] = true
		}
	}

	// 2. Remove instructions whose Dst is not used and have no side effects.
	// Side effects: labels, jumps, returns — always keep.
	result := make([]*TACInstr, 0, len(instrs))
	for _, instr := range instrs {
		switch instr.Op {
		case TACLabel, TACJump, TACJumpIfNot, TACReturn:
			result = append(result, instr) // always keep
		default:
			if instr.Dst == "" || used[instr.Dst] {
				result = append(result, instr)
			}
			// otherwise: result is unused — drop it
		}
	}
	return result
}

// --- Strength Reduction: multiply by power-of-two → shift ---

func strengthReduction(instrs []*TACInstr) []*TACInstr {
	// New op: shift left (we model it as "shl")
	// We reuse TACInstr with Op=TACMul but rewrite the Src2 to the shift amount.
	// In real compilers this would emit SHL instructions. Here we print the intent.
	result := make([]*TACInstr, 0, len(instrs))
	for _, instr := range instrs {
		if instr.Op == TACMul {
			// Check if Src2 is a known power of two (from prior const folding).
			// For simplicity, detect literal "2", "4", "8", "16" patterns.
			shift, ok := isPow2Const(instr.Src2)
			if ok {
				fmt.Printf("  [strength reduce] %s = %s * %s → %s = %s << %d\n",
					instr.Dst, instr.Src1, instr.Src2, instr.Dst, instr.Src1, shift)
				// In a real compiler: emit SHL instruction.
			}
		}
		result = append(result, instr)
	}
	return result
}

func isPow2Const(s string) (int, bool) {
	var v int64
	if _, err := fmt.Sscanf(s, "%d", &v); err != nil || v <= 0 {
		return 0, false
	}
	if v&(v-1) == 0 {
		return int(math.Log2(float64(v))), true
	}
	return 0, false
}

func printInstrs(title string, instrs []*TACInstr) {
	fmt.Printf("\n=== %s ===\n", title)
	for _, i := range instrs {
		fmt.Println(i)
	}
}

func main() {
	// Program: result = 2 * 4 + (3 - 3)
	// After constant folding: result = 8 + 0 → result = 8
	// Dead code: the original 2*4 and 3-3 temporaries become dead.
	original := []*TACInstr{
		{Op: TACConst, Dst: "t1", Const: 2},
		{Op: TACConst, Dst: "t2", Const: 4},
		{Op: TACMul, Dst: "t3", Src1: "t1", Src2: "t2"},
		{Op: TACConst, Dst: "t4", Const: 3},
		{Op: TACConst, Dst: "t5", Const: 3},
		{Op: TACSub, Dst: "t6", Src1: "t4", Src2: "t5"},
		{Op: TACAdd, Dst: "result", Src1: "t3", Src2: "t6"},
		{Op: TACReturn, Src1: "result"},
	}
	printInstrs("Original", original)

	folded := constantFolding(original)
	printInstrs("After Constant Folding", folded)

	dce := deadCodeElimination(folded)
	printInstrs("After Dead Code Elimination", dce)

	// Separate demonstration of strength reduction:
	fmt.Println("\n=== Strength Reduction Demo ===")
	srProgram := []*TACInstr{
		{Op: TACConst, Dst: "t1", Const: 8}, // literal 8
		{Op: TACMul, Dst: "t2", Src1: "x", Src2: "8"},
	}
	strengthReduction(srProgram)

	// Demonstrating what the Go compiler actually does:
	// Compile this file with: go build -gcflags='-m=2' to see inlining decisions.
	// Compile with: GOSSAFUNC=main go build to see SSA IR with optimizations applied.
	fmt.Println("\n=== Verifying Go Optimizations ===")
	fmt.Println("Compile with: GOSSAFUNC=main go build 04-optimization-passes.go")
	fmt.Println("This generates ssa.html showing each optimization pass applied.")
	fmt.Println("Look for 'opt' and 'dse' passes — these are DCE and store elimination.")

	// Go's inlining: functions below ~80 AST nodes are inlined.
	// The -gcflags='-m' flag shows which functions are inlined.
	result := computeWithInlinableFunc(10, 20)
	fmt.Printf("computeWithInlinableFunc(10, 20) = %d\n", result)
}

// This function is simple enough to be inlined by the Go compiler.
// Run: go build -gcflags='-m' to confirm "inlining call to add"
func add(a, b int) int {
	return a + b
}

func computeWithInlinableFunc(x, y int) int {
	// The Go compiler will inline `add` here.
	// After inlining: effectively `return x + y` with no function call overhead.
	return add(x, y)
}
```

### Go-specific considerations

**Inlining budget and `-gcflags='-m'`**: Run `go build -gcflags='-m=1' ./...` to see every inlining decision. `-m=2` gives more detail including why a function was not inlined ("too complex: 85 nodes"). Functions with closures, deferred calls, or loops with large bodies are not inlined. The budget is approximately 80 AST nodes — not lines of code.

**GOSSAFUNC and the optimization pass pipeline**: `GOSSAFUNC=myFunc go build` generates `ssa.html`. Each column in the HTML is a pass. Key passes to look for:
- `opt`: combines constant propagation, dead code elimination, and several algebraic simplifications
- `dse`: dead store elimination (stores whose result is never loaded)
- `lowered`: platform-specific lowering (generic ops → x86 ADDQ, MOVQ, etc.)
- `regalloc`: register allocation (virtual regs → physical regs with spill/restore)

**Reading `go tool compile -S`**: The `-S` flag outputs pseudo-assembly. Look for `MOVQ $8, AX` (constant `8` directly loaded) to confirm constant folding worked. If you see `IMULQ` where you expected a shift, the strength reduction did not fire — check if the operand is actually a compile-time constant.

**Escape analysis interacting with DCE**: Go's escape analysis can mark entire allocations as dead if the allocated object's value is never read. `_ = &Foo{}` — the compiler eliminates the allocation entirely if it can prove no side effects.

## Implementation: Rust

```rust
// Optimization passes on TAC IR.
// Demonstrates: constant folding, DCE, and how to verify LLVM optimizations fired.
// Also shows: reading rustc's assembly output to confirm optimizations.

use std::collections::{HashMap, HashSet};

#[derive(Debug, Clone, PartialEq)]
enum TACOp {
    Const(i64),
    Add, Sub, Mul, Div,
    Neg,
    Copy,
    JumpIfNot(String), // label
    Jump(String),
    Label(String),
    Return,
}

#[derive(Debug, Clone)]
struct TACInstr {
    dst:  Option<String>,
    op:   TACOp,
    src1: Option<String>,
    src2: Option<String>,
}

impl TACInstr {
    fn const_instr(dst: &str, val: i64) -> Self {
        TACInstr { dst: Some(dst.into()), op: TACOp::Const(val), src1: None, src2: None }
    }
    fn binop(dst: &str, op: TACOp, s1: &str, s2: &str) -> Self {
        TACInstr { dst: Some(dst.into()), op, src1: Some(s1.into()), src2: Some(s2.into()) }
    }
    fn ret(src: &str) -> Self {
        TACInstr { dst: None, op: TACOp::Return, src1: Some(src.into()), src2: None }
    }
}

impl std::fmt::Display for TACInstr {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        let dst = self.dst.as_deref().unwrap_or("");
        match &self.op {
            TACOp::Const(v) => write!(f, "  {} = {}", dst, v),
            TACOp::Add      => write!(f, "  {} = {} + {}", dst, self.src1.as_deref().unwrap(), self.src2.as_deref().unwrap()),
            TACOp::Sub      => write!(f, "  {} = {} - {}", dst, self.src1.as_deref().unwrap(), self.src2.as_deref().unwrap()),
            TACOp::Mul      => write!(f, "  {} = {} * {}", dst, self.src1.as_deref().unwrap(), self.src2.as_deref().unwrap()),
            TACOp::Div      => write!(f, "  {} = {} / {}", dst, self.src1.as_deref().unwrap(), self.src2.as_deref().unwrap()),
            TACOp::Neg      => write!(f, "  {} = -{}", dst, self.src1.as_deref().unwrap()),
            TACOp::Copy     => write!(f, "  {} = {}", dst, self.src1.as_deref().unwrap()),
            TACOp::Return   => write!(f, "  return {}", self.src1.as_deref().unwrap_or("")),
            TACOp::Jump(l)  => write!(f, "  goto {}", l),
            TACOp::JumpIfNot(l) => write!(f, "  ifnot {} goto {}", self.src1.as_deref().unwrap(), l),
            TACOp::Label(l) => write!(f, "{}:", l),
        }
    }
}

// --- Constant Folding ---

fn constant_folding(instrs: &[TACInstr]) -> Vec<TACInstr> {
    let mut consts: HashMap<String, i64> = HashMap::new();
    let mut result = Vec::with_capacity(instrs.len());

    for instr in instrs {
        match &instr.op {
            TACOp::Const(v) => {
                if let Some(d) = &instr.dst { consts.insert(d.clone(), *v); }
                result.push(instr.clone());
            }
            TACOp::Add | TACOp::Sub | TACOp::Mul | TACOp::Div => {
                let s1 = instr.src1.as_deref().unwrap();
                let s2 = instr.src2.as_deref().unwrap();
                let v1 = consts.get(s1).copied();
                let v2 = consts.get(s2).copied();
                if let (Some(a), Some(b)) = (v1, v2) {
                    let folded = match &instr.op {
                        TACOp::Add => a + b,
                        TACOp::Sub => a - b,
                        TACOp::Mul => a * b,
                        TACOp::Div if b != 0 => a / b,
                        _ => { result.push(instr.clone()); continue; }
                    };
                    if let Some(d) = &instr.dst {
                        consts.insert(d.clone(), folded);
                        result.push(TACInstr::const_instr(d, folded));
                    }
                } else {
                    result.push(instr.clone());
                }
            }
            TACOp::Neg => {
                let s = instr.src1.as_deref().unwrap();
                if let Some(v) = consts.get(s).copied() {
                    if let Some(d) = &instr.dst {
                        consts.insert(d.clone(), -v);
                        result.push(TACInstr::const_instr(d, -v));
                        continue;
                    }
                }
                result.push(instr.clone());
            }
            _ => result.push(instr.clone()),
        }
    }
    result
}

// --- Dead Code Elimination ---

fn dead_code_elimination(instrs: &[TACInstr]) -> Vec<TACInstr> {
    let mut used: HashSet<String> = HashSet::new();
    for instr in instrs {
        if let Some(s) = &instr.src1 { used.insert(s.clone()); }
        if let Some(s) = &instr.src2 { used.insert(s.clone()); }
    }

    instrs.iter().filter(|instr| {
        // Always keep side-effect instructions
        matches!(instr.op, TACOp::Return | TACOp::Jump(_) | TACOp::JumpIfNot(_) | TACOp::Label(_))
        || instr.dst.as_ref().map_or(true, |d| used.contains(d))
    }).cloned().collect()
}

fn print_instrs(title: &str, instrs: &[TACInstr]) {
    println!("\n=== {} ===", title);
    for i in instrs { println!("{}", i); }
}

fn main() {
    // Program: result = 2 * 4 + (3 - 3)
    let original = vec![
        TACInstr::const_instr("t1", 2),
        TACInstr::const_instr("t2", 4),
        TACInstr::binop("t3", TACOp::Mul, "t1", "t2"),
        TACInstr::const_instr("t4", 3),
        TACInstr::const_instr("t5", 3),
        TACInstr::binop("t6", TACOp::Sub, "t4", "t5"),
        TACInstr::binop("result", TACOp::Add, "t3", "t6"),
        TACInstr::ret("result"),
    ];

    print_instrs("Original", &original);

    let folded = constant_folding(&original);
    print_instrs("After Constant Folding", &folded);

    let dce = dead_code_elimination(&folded);
    print_instrs("After Dead Code Elimination", &dce);

    // Expected result: only `result = 8` and `return result` remain.
    println!("\nExpected after both passes:");
    println!("  result = 8");
    println!("  return result");

    // --- Verifying LLVM optimizations in Rust ---
    // Run: RUSTFLAGS="--emit=asm" cargo build --release
    // Look for the output .s file in target/release/deps/
    // For a function like add_constants below, you should see only:
    //   movl $8, %eax
    //   retq
    // No actual addition instruction — constant folding happened.
    let v = add_constants();
    println!("\nadd_constants() = {} (compiler should fold to 8)", v);

    // For inlining: run cargo build --release with RUSTFLAGS="-C inline-threshold=200"
    // to increase the inlining threshold and verify more functions get inlined.
    let sum = add_two(3, 4);
    println!("add_two(3, 4) = {} (should be inlined)", sum);
}

// LLVM will constant-fold this to return 8 at opt-level >= 1.
// Verify: RUSTFLAGS="--emit=asm" cargo build --release
// grep -A 5 "add_constants" target/release/deps/*.s
#[inline(never)] // prevent inlining so we can see the function in asm
fn add_constants() -> i32 {
    let a: i32 = 2;
    let b: i32 = 4;
    a * b  // LLVM folds to 8
}

// This function is small enough to be inlined.
// With #[inline] hint it is always inlined; without it, LLVM decides.
fn add_two(a: i32, b: i32) -> i32 {
    a + b
}
```

### Rust-specific considerations

**`RUSTFLAGS="--emit=asm" cargo build --release`**: This emits `.s` assembly files to `target/release/deps/`. Look for your function name (mangled) and check what instructions are present. For `add_constants()` above with `-C opt-level=2` or higher, you should see only `movl $8, %eax; retq` — no `IMUL` or `ADD`.

**LLVM opt levels**: `opt-level=0` (debug): no optimization. `opt-level=1`: basic opts (DCE, constant folding, mem2reg which converts stack slots to SSA). `opt-level=2` (release): full optimization pipeline including inlining, loop opts, vectorization. `opt-level=3`: same plus more aggressive inlining. `opt-level="s"` and `"z"`: optimize for size.

**`#[inline]`, `#[inline(always)]`, `#[inline(never)]`**: These are hints, not guarantees (except `always` and `never`). LLVM has its own inlining heuristics that can override `#[inline]`. If you need to measure the impact of inlining, use `#[inline(never)]` on a function and compare the assembly to a version without it.

**Viewing LLVM pass results**: `RUSTFLAGS="-C llvm-args=-print-after-all" cargo build 2>&1 | less`. This prints the LLVM IR after every optimization pass — very verbose but shows exactly what each pass does. A more targeted approach: `-C llvm-args=-debug-only=inline` to see only inlining decisions.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| Optimization backend | Custom SSA passes + platform lowering | Full LLVM pipeline |
| Inlining threshold | ~80 AST nodes; `-gcflags='-m'` to inspect | LLVM's cost model; `#[inline(always/never)]` as hints |
| Constant folding | Go SSA `opt` pass | LLVM `instcombine` + `constprop` passes |
| Loop vectorization | Limited (Go has partial SIMD support) | LLVM auto-vectorization (`-C opt-level=3`) |
| Reading optimizer output | `GOSSAFUNC=f go build` → ssa.html | `--emit=asm`, `--emit=llvm-ir` |
| Link-time optimization | `go build` does whole-program | `RUSTFLAGS="-C lto=fat"` for full LTO |
| Debug vs release build | Same compiler flags, less optimization | Significant difference: `cargo build` vs `cargo build --release` |

## Production War Stories

**Go's inliner and `bytes.Equal`**: Go's `bytes.Equal` function was historically not inlined because of a call to a runtime assembly function inside it. Benchmarks showed that the function call overhead for comparing small byte slices (2-16 bytes) dominated the comparison cost. After the compiler was enhanced to inline calls to intrinsics, `bytes.Equal` on short slices became competitive with hand-rolled assembly. The lesson: function call overhead matters when the function body is trivially small relative to the call setup cost.

**LLVM miscompilation via undefined behavior**: In 2011-2015, several C/C++ projects discovered that LLVM's optimizer was miscompiling code that relied on signed integer overflow being defined behavior. LLVM treats signed overflow as undefined behavior (per the C standard) and uses this to justify aggressive transformations — including removing bounds checks. The lesson: LLVM's constant folding and algebraic transformations are only correct under the language's defined semantics. Rust avoids this by using wrapping arithmetic (`wrapping_add`) in hot paths and emitting explicit overflow checks in debug builds.

**V8's SCCP and function type feedback**: V8's TurboFan uses a form of SCCP that incorporates type feedback from the interpreter. If the feedback says "this `+` always adds two integers", TurboFan constant-propagates the specialization into the SSA graph and generates `ADDQ` instead of a generic `+` handler. When the type assumption is violated at runtime, TurboFan deoptimizes: throws away the compiled code and falls back to interpreted execution. This is "speculative constant propagation" — SCCP extended with runtime speculation.

**Go 1.22 loop variable capture**: Go 1.22 changed the semantics of loop variable capture in goroutines. The compiler's escape analysis needed to be updated to correctly model the new per-iteration scoping. During the transition, the SSA optimization passes had to emit different code for pre-1.22 and post-1.22 semantics — the optimization passes are not independent of language semantics.

## Complexity Analysis

| Pass | Time | Iterations |
|------|------|-----------|
| Constant folding (per-block) | O(n) in instructions | 1 (no iteration needed) |
| SCCP (SSA) | O(n) using use-def chains | 1 (worklist, O(n) operations) |
| DCE (with SSA) | O(n) | 1 (mark live from roots, sweep) |
| Function inlining | O(n · size(callees)) | Bounded by inlining depth |
| LICM | O(n · |loop body|) | Per loop nest |
| Strength reduction | O(n) | 1 |
| Full LLVM pipeline | O(n²) in pathological cases | Multiple fixed-point iterations |

## Common Pitfalls

**1. Constant folding unsafe operations.** Folding `x / 0` at compile time changes a runtime trap into a compile error (or wrong behavior). Compilers must preserve division-by-zero behavior unless the divisor is provably non-zero.

**2. Inlining too aggressively causes instruction cache thrashing.** Aggressively inlined code is larger. On hot paths where the L1 instruction cache matters (inner loops), an inlined version may actually be slower than a call to a well-cached function. This is why LLVM has separate "hot" and "cold" inlining thresholds.

**3. Hoisting non-idempotent operations out of loops.** LICM requires the hoisted expression to have no side effects. Hoisting a function call that modifies global state out of a loop changes program semantics. Production LICM passes check for `readnone`/`readonly` attributes on calls before hoisting.

**4. Assuming optimization levels produce the same code.** A `cargo build` (debug) and `cargo build --release` can produce dramatically different code — not just faster/slower, but different observable behavior if the code relies on undefined behavior that optimizations expose. Always test release builds, not just debug.

**5. Not accounting for loop-carried dependencies.** Strength reduction of `a[i * stride]` to an accumulated offset assumes `stride` is loop-invariant. If `stride` is modified inside the loop body, the transformation is incorrect. The compiler checks this; when writing loop code, making loop-invariant values explicitly `const` or immutable helps the compiler prove the property.

## Exercises

**Exercise 1** (30 min): Add copy propagation to the constant folding pass in Go. After constant folding, if `t7 = t6` and `t6 = 8`, replace all uses of `t7` with `8` and mark the copy instruction as dead. Measure how many additional instructions DCE eliminates.

**Exercise 2** (2–4h): Implement SCCP (Sparse Conditional Constant Propagation) on the TAC IR in Rust. Use the three-valued lattice (`Undef`, `Const(i64)`, `OverDefined`). Process only instructions whose inputs have changed (worklist). Handle phi nodes: a phi is constant only if all executable-predecessor arguments are the same constant.

**Exercise 3** (4–8h): Implement a basic inliner for the TAC IR. Given a map of function name → instruction list, replace `CALL` instructions with an inlined copy of the callee's body (with renamed temporaries to avoid name collisions). Implement a size threshold: only inline functions with ≤ N instructions. Test with mutual recursion and verify the inliner handles it safely (bounded depth).

**Exercise 4** (8–15h): Implement LICM for a loop CFG. Given a CFG with clearly identified loop headers (blocks that dominate their own predecessors via back edges), identify loop-invariant instructions (instructions whose operands are all loop-invariant or defined outside the loop). Hoist them to the loop pre-header. Verify correctness by running the CFG through an interpreter before and after LICM and checking that the output matches.

## Further Reading

### Foundational Papers
- **Wegman & Zadeck, 1991** — "Constant Propagation with Conditional Branches." The SCCP paper. Essential for understanding how branch folding interacts with constant propagation.
- **Allen & Cocke, 1972** — "A Catalogue of Optimizing Transformations." The foundational taxonomy of compiler optimizations. Still relevant.
- **Click & Cooper, 1995** — "Combining Analyses, Combining Optimizations." Explains how to combine multiple optimization passes into a single unified framework.

### Books
- **Engineering a Compiler** (Cooper & Torczon) — Chapter 8: Introduction to Optimization. Chapter 10: Loop Optimizations.
- **Dragon Book** (Aho et al.) — Chapter 9: Machine-Independent Optimizations. The canonical treatment of dataflow-based optimization.

### Production Code to Read
- `go/src/cmd/compile/internal/ssa/rewrite.go` — Go's SSA rewrite rules (algebraic simplifications)
- `go/src/cmd/compile/internal/ssa/opt.go` — Go's `opt` pass (combines several optimizations)
- `llvm-project/llvm/lib/Transforms/Scalar/` — LLVM's scalar optimization passes (GVN, DCE, LICM, SCCP)
- `llvm-project/llvm/lib/Transforms/IPO/Inliner.cpp` — LLVM's inliner

### Talks
- **"Understanding Compiler Optimizations"** — Chandler Carruth, CppCon 2015. Shows how LLVM's optimization pipeline processes real C++ code.
- **"LLVM's Analysis and Transform Infrastructure"** — Chris Lattner, LLVM Developers' Meeting 2008.
- **"Go's SSA Compiler Backend"** — David Chase, GopherCon 2016.
