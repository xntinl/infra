# 1. Reading SSA Output

<!--
difficulty: advanced
concepts: [ssa, static-single-assignment, compiler-ir, ssa-html, optimization-phases, basic-blocks, phi-nodes]
tools: [go]
estimated_time: 35m
bloom_level: analyze
prerequisites: [functions, pointers, interfaces]
-->

## Prerequisites

- Go 1.22+ installed
- Solid understanding of Go functions, pointers, and interfaces
- Basic familiarity with compiler concepts (optional but helpful)

## Learning Objectives

After completing this exercise, you will be able to:

- **Generate** SSA output from the Go compiler using `GOSSAFUNC`
- **Interpret** SSA basic blocks, operations, and values
- **Trace** how a Go function is transformed through compiler optimization passes
- **Identify** key SSA operations: Phi nodes, memory operations, and control flow

## Why Reading SSA Output Matters

The Go compiler converts your source code into Static Single Assignment (SSA) form before generating machine code. SSA is the intermediate representation where most optimizations happen: dead code elimination, constant folding, bounds check elimination, inlining, and more. Being able to read SSA output lets you understand *why* the compiler generates the code it does, verify that expected optimizations fire, and diagnose performance issues at the compiler level.

## The Problem

Write several small Go functions, generate their SSA output, and trace the transformation from source code through optimization passes to final machine code. You will learn to read the SSA HTML visualization and identify key optimization decisions.

## Requirements

1. **Write a function `sumSlice`** that sums the elements of an integer slice:

```go
func sumSlice(nums []int) int {
    total := 0
    for _, n := range nums {
        total += n
    }
    return total
}
```

Generate SSA output:

```bash
GOSSAFUNC=sumSlice go build -o /dev/null main.go
```

Open the generated `ssa.html` in a browser and examine the phases.

2. **Write a function `conditionalAdd`** with a simple branch to observe Phi nodes:

```go
func conditionalAdd(a, b int, add bool) int {
    result := a
    if add {
        result = a + b
    }
    return result
}
```

3. **Write a function `interfaceCall`** that makes an interface method call to observe devirtualization opportunities:

```go
type Adder interface {
    Add(int) int
}

type IntAdder struct{ val int }

func (ia IntAdder) Add(n int) int { return ia.val + n }

func useAdder(a Adder, n int) int {
    return a.Add(n)
}
```

4. **Document your findings** for each function by answering:
   - How many basic blocks does the function have?
   - Where do Phi nodes appear and what do they merge?
   - Which optimization passes change the function? (Compare "start" vs "opt" vs "lower" phases)
   - What operations remain in the final "genssa" phase?

5. **Write a `main` function** that calls all functions to ensure they are not eliminated by dead code removal:

```go
func main() {
    fmt.Println(sumSlice([]int{1, 2, 3, 4, 5}))
    fmt.Println(conditionalAdd(10, 20, true))

    adder := IntAdder{val: 42}
    fmt.Println(useAdder(adder, 8))
}
```

## Hints

- `GOSSAFUNC=funcName go build` generates `ssa.html` in the current directory. Open it in a browser for an interactive view.
- SSA form means every variable is assigned exactly once. When two paths merge, a Phi node selects which value to use based on which path was taken.
- Basic blocks (b1, b2, ...) represent straight-line code sequences. Control flow edges connect blocks.
- The SSA phases progress from high-level (close to source) to low-level (close to machine code): `start` -> `opt` -> `lower` -> `regalloc` -> `genssa`.
- Memory operations in SSA are explicit: loads and stores carry a "memory" value that threads through the function, ensuring ordering.
- Click on a value in the SSA HTML to highlight all uses and definitions.

## Verification

```bash
GOSSAFUNC=sumSlice go build -o /dev/null main.go
# Open ssa.html in browser

GOSSAFUNC=conditionalAdd go build -o /dev/null main.go
# Open ssa.html in browser
```

Confirm that:
1. `ssa.html` is generated and shows multiple optimization phases
2. `sumSlice` shows a loop structure with induction variable and accumulator
3. `conditionalAdd` shows a Phi node merging the two possible values of `result`
4. The "genssa" phase shows the final machine instructions

## What's Next

Continue to [02 - Compiler Optimization Passes](../02-compiler-optimization-passes/02-compiler-optimization-passes.md) to explore specific optimization passes the Go compiler applies.

## Summary

- `GOSSAFUNC=funcName go build` generates an interactive HTML SSA visualization
- SSA (Static Single Assignment) is the compiler's intermediate representation where optimizations happen
- Each variable is assigned exactly once; Phi nodes merge values at control flow join points
- The SSA progresses through phases: start, opt, lower, regalloc, genssa
- Memory operations are explicit in SSA, ensuring correct ordering
- Reading SSA output is essential for understanding and verifying compiler optimizations

## Reference

- [Introduction to the Go Compiler SSA Backend](https://github.com/golang/go/blob/master/src/cmd/compile/internal/ssa/README.md)
- [Go Compiler Internals](https://go.dev/src/cmd/compile/README)
- [Static Single Assignment (Wikipedia)](https://en.wikipedia.org/wiki/Static_single-assignment_form)
