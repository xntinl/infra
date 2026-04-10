<!--
type: reference
difficulty: advanced
section: [05-compilers-and-runtimes]
concepts: [cst-vs-ast, ir-lowering, three-address-code, llvm-ir, go-ssa, rust-mir, visitor-pattern, basic-blocks]
languages: [go, rust]
estimated_reading_time: 70-90 min
bloom_level: analyze
prerequisites: [lexing-and-parsing, go-interfaces, rust-enums, tree-data-structures]
papers: [LLVM-lattner-2004]
industry_use: [llvm, go-compiler, rustc, gcc]
language_contrast: high
-->

# AST and IR Design

> The AST is for humans; the IR is for the compiler — and the reason there are multiple IR levels is that each transformation is easy in one representation and impossible in another.

## Mental Model

When a compiler parses source code, it builds a tree that mirrors the structure of the text. This is the Abstract Syntax Tree — "abstract" because it discards syntactically redundant information (parentheses, semicolons, whitespace) while keeping semantic structure. But the AST is still too high-level for most transformations. It preserves language-specific constructs (`for ... range`, pattern matching, closures) that have no direct counterpart in machine instructions.

The compiler's answer is a sequence of IR lowering passes. Each pass transforms the program into a representation that makes a specific class of analysis or optimization natural:

- **AST → High-Level IR**: Desugaring. `for range` becomes a `while` loop with explicit index. Named return values become regular variables. This IR still has language-specific semantics but fewer special cases.
- **High-Level IR → SSA IR**: Static Single Assignment form. Every variable is defined exactly once. φ (phi) nodes at join points merge values from different control flow paths. This representation makes dataflow analysis trivial.
- **SSA IR → Three-Address Code / Machine IR**: Lowered to an infinite-register machine model. No more SSA properties; phi nodes have been eliminated (destructed). Ready for register allocation.

LLVM IR is a public, stable SSA-form IR with an explicit type system. Go's SSA (in `cmd/compile/internal/ssa`) and Rust's MIR (Mid-level Intermediate Representation) are internal to their respective compilers but follow the same SSA principles. When you run `RUSTFLAGS="--emit=mir" cargo build` or `go tool compile -S file.go`, you are looking at these IRs in human-readable form. They are not academic curiosities — they are the actual artifact that optimization passes operate on.

The design of the AST node types has long-term consequences. If you use a single large struct with optional fields ("this `Node` struct has a `Condition` field that is only non-nil for `if` nodes"), you get a compact representation but lose type safety — callers must know which fields are valid for which node kinds. If you use one type per node kind (Go interfaces, Rust enums), you get compile-time safety but more indirection. Both approaches exist in real compilers, and the choice reflects the team's priorities around type safety vs. memory layout.

## Core Concepts

### Concrete Syntax Tree vs Abstract Syntax Tree

A CST (Concrete Syntax Tree, also called a parse tree) has one node per grammar production rule. If your grammar has `expr → term (('+' | '-') term)*`, a CST for `1 + 2` has nodes for `expr`, `term`, the `+` token, and another `term`. The AST collapses this: `BinopNode("+", IntNode(1), IntNode(2))`. The CST contains everything needed to reconstruct the source text verbatim. The AST discards what the compiler does not need.

tree-sitter produces CSTs (it needs them for syntax highlighting — every token must be in the tree). Most compilers produce ASTs directly during parsing (no intermediate CST). Rust's `syn` crate produces a CST-level AST (it preserves enough information to reconstruct source, enabling proc macros to transform code without losing comments or formatting).

### Three-Address Code

Three-address code (TAC) is the canonical low-level IR. Each instruction has at most three operands: a destination and up to two sources. Examples:

```
t1 = a + b
t2 = t1 * c
if t2 > 0 goto L1
goto L2
L1: x = t2
L2: ...
```

TAC is machine-independent but close enough to assembly that instruction selection is straightforward. LLVM IR is a typed, SSA-form variant of TAC. Go's `cmd/compile/internal/ssa` package represents each operation as a `Value` struct with an `Op` code and arguments — this is TAC with SSA properties.

### Basic Blocks and Control Flow Graphs

A basic block is a maximal sequence of instructions with:
- Exactly one entry point (the first instruction)
- Exactly one exit point (a branch or jump at the end)
- No internal branches or labels

The Control Flow Graph (CFG) has basic blocks as nodes and edges for possible control transfers. SSA IR is organized into basic blocks. Every function is a CFG. When you look at `go tool compile -S` output, you see labels like `b1:`, `b2:` — these are basic block boundaries.

### IR Lowering: Go's SSA and Rust's MIR

Go compiler pipeline (simplified):
```
Syntax AST
  └─→ typecheck AST (name resolution, type annotation)
       └─→ walk AST (desugar: for-range, closures, interface conversions)
            └─→ SSA IR (cmd/compile/internal/ssa)
                 └─→ machine-specific lowering
                      └─→ register allocation
                           └─→ object code
```

Rust compiler pipeline (simplified):
```
Syntax AST (rustc_ast)
  └─→ HIR (High-level IR: after macro expansion and name resolution)
       └─→ THIR (Typed HIR: after type inference)
            └─→ MIR (Mid-level IR: after borrow checking and monomorphization)
                 └─→ LLVM IR (via rustc_codegen_llvm)
                      └─→ LLVM optimization passes
                           └─→ machine code
```

MIR is the level where the borrow checker runs, after type inference but before code generation. This placement is intentional: borrow checking needs types (to know the lifetimes of references) but should not have to deal with the complexity of full AST structure.

## Implementation: Go

```go
package main

import (
	"fmt"
	"strings"
)

// A complete AST for a simple statement language.
// Statements: let, return, if/else, while, block.
// Expressions: int, bool, identifier, binary op, unary op, call.

// --- Expression nodes ---

type Expr interface {
	exprNode()
	String() string
}

type IntLit struct{ Value int64 }
type BoolLit struct{ Value bool }
type Ident struct{ Name string }
type BinOp struct {
	Op    string
	Left  Expr
	Right Expr
}
type UnaryOp struct {
	Op      string
	Operand Expr
}
type Call struct {
	Func string
	Args []Expr
}

func (*IntLit) exprNode()  {}
func (*BoolLit) exprNode() {}
func (*Ident) exprNode()   {}
func (*BinOp) exprNode()   {}
func (*UnaryOp) exprNode() {}
func (*Call) exprNode()    {}

func (n *IntLit) String() string  { return fmt.Sprintf("%d", n.Value) }
func (n *BoolLit) String() string { return fmt.Sprintf("%v", n.Value) }
func (n *Ident) String() string   { return n.Name }
func (n *BinOp) String() string   { return fmt.Sprintf("(%s %s %s)", n.Left, n.Op, n.Right) }
func (n *UnaryOp) String() string { return fmt.Sprintf("(%s%s)", n.Op, n.Operand) }
func (n *Call) String() string {
	args := make([]string, len(n.Args))
	for i, a := range n.Args {
		args[i] = a.String()
	}
	return fmt.Sprintf("%s(%s)", n.Func, strings.Join(args, ", "))
}

// --- Statement nodes ---

type Stmt interface {
	stmtNode()
	String() string
}

type LetStmt struct {
	Name  string
	Value Expr
}
type ReturnStmt struct{ Value Expr }
type IfStmt struct {
	Cond     Expr
	Then     *Block
	Else     *Block // nil if no else branch
}
type WhileStmt struct {
	Cond Expr
	Body *Block
}
type ExprStmt struct{ Expr Expr }
type Block struct{ Stmts []Stmt }

func (*LetStmt) stmtNode()    {}
func (*ReturnStmt) stmtNode() {}
func (*IfStmt) stmtNode()     {}
func (*WhileStmt) stmtNode()  {}
func (*ExprStmt) stmtNode()   {}
func (*Block) stmtNode()      {} // Block is also a Stmt

func (n *LetStmt) String() string    { return fmt.Sprintf("let %s = %s", n.Name, n.Value) }
func (n *ReturnStmt) String() string { return fmt.Sprintf("return %s", n.Value) }
func (n *ExprStmt) String() string   { return n.Expr.String() }
func (n *Block) String() string {
	lines := make([]string, len(n.Stmts))
	for i, s := range n.Stmts {
		lines[i] = "  " + s.String()
	}
	return "{\n" + strings.Join(lines, "\n") + "\n}"
}
func (n *IfStmt) String() string {
	s := fmt.Sprintf("if %s %s", n.Cond, n.Then)
	if n.Else != nil {
		s += " else " + n.Else.String()
	}
	return s
}
func (n *WhileStmt) String() string {
	return fmt.Sprintf("while %s %s", n.Cond, n.Body)
}

// --- Visitor pattern ---
// The Visitor pattern is the standard way to add operations to an AST without
// modifying node types. The tradeoff: adding a new operation = add a new visitor.
// Adding a new node type = update every visitor (fragile).

type Visitor interface {
	VisitIntLit(n *IntLit)
	VisitBoolLit(n *BoolLit)
	VisitIdent(n *Ident)
	VisitBinOp(n *BinOp)
	VisitUnaryOp(n *UnaryOp)
	VisitCall(n *Call)
	VisitLetStmt(n *LetStmt)
	VisitReturnStmt(n *ReturnStmt)
	VisitIfStmt(n *IfStmt)
	VisitWhileStmt(n *WhileStmt)
	VisitExprStmt(n *ExprStmt)
	VisitBlock(n *Block)
}

func WalkExpr(v Visitor, e Expr) {
	switch n := e.(type) {
	case *IntLit:
		v.VisitIntLit(n)
	case *BoolLit:
		v.VisitBoolLit(n)
	case *Ident:
		v.VisitIdent(n)
	case *BinOp:
		v.VisitBinOp(n)
	case *UnaryOp:
		v.VisitUnaryOp(n)
	case *Call:
		v.VisitCall(n)
	}
}

func WalkStmt(v Visitor, s Stmt) {
	switch n := s.(type) {
	case *LetStmt:
		v.VisitLetStmt(n)
	case *ReturnStmt:
		v.VisitReturnStmt(n)
	case *IfStmt:
		v.VisitIfStmt(n)
	case *WhileStmt:
		v.VisitWhileStmt(n)
	case *ExprStmt:
		v.VisitExprStmt(n)
	case *Block:
		v.VisitBlock(n)
	}
}

// --- IR Lowering: AST → Three-Address Code ---
// We lower the AST to a linear list of TAC instructions.
// This is a simplified version of what a real compiler does.

type TACOp int

const (
	TACCopy TACOp = iota // dst = src
	TACAdd               // dst = a + b
	TACSub               // dst = a - b
	TACMul               // dst = a * b
	TACDiv               // dst = a / b
	TACLt                // dst = a < b
	TACGt                // dst = a > b
	TACEq                // dst = a == b
	TACNeg               // dst = -a
	TACJump              // goto label
	TACJumpIf            // if cond goto label
	TACJumpIfNot         // if !cond goto label
	TACLabel             // label:
	TACCall              // dst = call func(args...)
	TACReturn            // return val
	TACConst             // dst = constant
)

type TACInstr struct {
	Op    TACOp
	Dst   string
	Src1  string
	Src2  string
	Label string // for Jump, Label, JumpIf
	Const int64  // for TACConst
}

func (i *TACInstr) String() string {
	switch i.Op {
	case TACConst:
		return fmt.Sprintf("  %s = %d", i.Dst, i.Const)
	case TACCopy:
		return fmt.Sprintf("  %s = %s", i.Dst, i.Src1)
	case TACAdd:
		return fmt.Sprintf("  %s = %s + %s", i.Dst, i.Src1, i.Src2)
	case TACSub:
		return fmt.Sprintf("  %s = %s - %s", i.Dst, i.Src1, i.Src2)
	case TACMul:
		return fmt.Sprintf("  %s = %s * %s", i.Dst, i.Src1, i.Src2)
	case TACDiv:
		return fmt.Sprintf("  %s = %s / %s", i.Dst, i.Src1, i.Src2)
	case TACLt:
		return fmt.Sprintf("  %s = %s < %s", i.Dst, i.Src1, i.Src2)
	case TACGt:
		return fmt.Sprintf("  %s = %s > %s", i.Dst, i.Src1, i.Src2)
	case TACEq:
		return fmt.Sprintf("  %s = %s == %s", i.Dst, i.Src1, i.Src2)
	case TACNeg:
		return fmt.Sprintf("  %s = -%s", i.Dst, i.Src1)
	case TACJump:
		return fmt.Sprintf("  goto %s", i.Label)
	case TACJumpIf:
		return fmt.Sprintf("  if %s goto %s", i.Src1, i.Label)
	case TACJumpIfNot:
		return fmt.Sprintf("  ifnot %s goto %s", i.Src1, i.Label)
	case TACLabel:
		return fmt.Sprintf("%s:", i.Label)
	case TACReturn:
		return fmt.Sprintf("  return %s", i.Src1)
	case TACCall:
		return fmt.Sprintf("  %s = call %s(%s)", i.Dst, i.Label, i.Src1)
	}
	return "  (unknown)"
}

type IRGen struct {
	instrs  []*TACInstr
	tmpCnt  int
	lblCnt  int
	env     map[string]string // variable name → TAC name
}

func NewIRGen() *IRGen {
	return &IRGen{env: make(map[string]string)}
}

func (g *IRGen) newTemp() string {
	g.tmpCnt++
	return fmt.Sprintf("t%d", g.tmpCnt)
}

func (g *IRGen) newLabel() string {
	g.lblCnt++
	return fmt.Sprintf("L%d", g.lblCnt)
}

func (g *IRGen) emit(i *TACInstr) {
	g.instrs = append(g.instrs, i)
}

// GenExpr lowers an expression to TAC and returns the temp that holds the result.
func (g *IRGen) GenExpr(e Expr) string {
	switch n := e.(type) {
	case *IntLit:
		t := g.newTemp()
		g.emit(&TACInstr{Op: TACConst, Dst: t, Const: n.Value})
		return t
	case *BoolLit:
		t := g.newTemp()
		v := int64(0)
		if n.Value {
			v = 1
		}
		g.emit(&TACInstr{Op: TACConst, Dst: t, Const: v})
		return t
	case *Ident:
		// Identifier: look up the SSA name in our environment.
		if name, ok := g.env[n.Name]; ok {
			return name
		}
		panic("undefined variable: " + n.Name)
	case *BinOp:
		l := g.GenExpr(n.Left)
		r := g.GenExpr(n.Right)
		t := g.newTemp()
		op := map[string]TACOp{"+": TACAdd, "-": TACSub, "*": TACMul, "/": TACDiv,
			"<": TACLt, ">": TACGt, "==": TACEq}[n.Op]
		g.emit(&TACInstr{Op: op, Dst: t, Src1: l, Src2: r})
		return t
	case *UnaryOp:
		operand := g.GenExpr(n.Operand)
		t := g.newTemp()
		g.emit(&TACInstr{Op: TACNeg, Dst: t, Src1: operand})
		return t
	case *Call:
		argTemps := make([]string, len(n.Args))
		for i, a := range n.Args {
			argTemps[i] = g.GenExpr(a)
		}
		t := g.newTemp()
		g.emit(&TACInstr{Op: TACCall, Dst: t, Label: n.Func, Src1: strings.Join(argTemps, ",")})
		return t
	}
	panic("unknown expr type")
}

// GenStmt lowers a statement to TAC instructions.
func (g *IRGen) GenStmt(s Stmt) {
	switch n := s.(type) {
	case *LetStmt:
		val := g.GenExpr(n.Value)
		g.env[n.Name] = val
	case *ReturnStmt:
		val := g.GenExpr(n.Value)
		g.emit(&TACInstr{Op: TACReturn, Src1: val})
	case *ExprStmt:
		g.GenExpr(n.Expr)
	case *Block:
		for _, stmt := range n.Stmts {
			g.GenStmt(stmt)
		}
	case *IfStmt:
		// Pattern: eval cond → jumpifnot to else_label → then block → jump to end → else_label → else block → end_label
		cond := g.GenExpr(n.Cond)
		elseLabel := g.newLabel()
		endLabel := g.newLabel()
		g.emit(&TACInstr{Op: TACJumpIfNot, Src1: cond, Label: elseLabel})
		g.GenStmt(n.Then)
		g.emit(&TACInstr{Op: TACJump, Label: endLabel})
		g.emit(&TACInstr{Op: TACLabel, Label: elseLabel})
		if n.Else != nil {
			g.GenStmt(n.Else)
		}
		g.emit(&TACInstr{Op: TACLabel, Label: endLabel})
	case *WhileStmt:
		// Pattern: top_label → eval cond → jumpifnot end → body → jump top → end_label
		topLabel := g.newLabel()
		endLabel := g.newLabel()
		g.emit(&TACInstr{Op: TACLabel, Label: topLabel})
		cond := g.GenExpr(n.Cond)
		g.emit(&TACInstr{Op: TACJumpIfNot, Src1: cond, Label: endLabel})
		g.GenStmt(n.Body)
		g.emit(&TACInstr{Op: TACJump, Label: topLabel})
		g.emit(&TACInstr{Op: TACLabel, Label: endLabel})
	}
}

func main() {
	// AST for:
	//   let x = 10
	//   let y = x * 2 + 3
	//   if y > 5 {
	//     return y
	//   } else {
	//     return -1
	//   }
	ast := &Block{
		Stmts: []Stmt{
			&LetStmt{Name: "x", Value: &IntLit{Value: 10}},
			&LetStmt{Name: "y", Value: &BinOp{
				Op:    "+",
				Left:  &BinOp{Op: "*", Left: &Ident{Name: "x"}, Right: &IntLit{Value: 2}},
				Right: &IntLit{Value: 3},
			}},
			&IfStmt{
				Cond: &BinOp{Op: ">", Left: &Ident{Name: "y"}, Right: &IntLit{Value: 5}},
				Then: &Block{Stmts: []Stmt{&ReturnStmt{Value: &Ident{Name: "y"}}}},
				Else: &Block{Stmts: []Stmt{&ReturnStmt{Value: &UnaryOp{Op: "-", Operand: &IntLit{Value: 1}}}}},
			},
		},
	}

	fmt.Println("=== AST ===")
	fmt.Println(ast)

	gen := NewIRGen()
	gen.GenStmt(ast)

	fmt.Println("\n=== Three-Address Code IR ===")
	for _, instr := range gen.instrs {
		fmt.Println(instr)
	}
}
```

### Go-specific considerations

**Interface-based AST vs struct-based**: Go's `cmd/compile` uses interfaces for statement and expression nodes (`syntax.Node`, `syntax.Stmt`, `syntax.Expr`). The type switch (`switch n := s.(type)`) is the Go idiom for dispatch on node kind. The cost is one interface indirection per dispatch — acceptable for compiler throughput, noticeable in a tight loop.

**Viewing Go's actual SSA IR**: Compile any Go file with `GOSSAFUNC=main go build file.go`. This generates `ssa.html` in the current directory — an HTML file with every SSA pass visualized as a side-by-side diff. You can see the program before and after each optimization. This is the single most useful tool for understanding what the Go compiler is doing to your code.

**Escape analysis in the IR**: The Go SSA pass `escape` determines whether a value escapes to the heap. It annotates heap allocations with a synthetic `newobject` call in the IR. If you see `newobject` for a type you expected to be stack-allocated, that is your allocation bug.

## Implementation: Rust

```rust
// AST for a simple statement language.
// Demonstrates: enum-based AST, pattern matching, IR lowering.
// Rust's enum + match provides exhaustive dispatch — the compiler ensures
// every node kind is handled.

use std::collections::HashMap;
use std::fmt;

// --- AST ---

#[derive(Debug, Clone)]
pub enum Expr {
    Int(i64),
    Bool(bool),
    Ident(String),
    BinOp { op: String, left: Box<Expr>, right: Box<Expr> },
    UnaryOp { op: String, operand: Box<Expr> },
    Call { name: String, args: Vec<Expr> },
}

impl fmt::Display for Expr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Expr::Int(n) => write!(f, "{}", n),
            Expr::Bool(b) => write!(f, "{}", b),
            Expr::Ident(s) => write!(f, "{}", s),
            Expr::BinOp { op, left, right } => write!(f, "({} {} {})", left, op, right),
            Expr::UnaryOp { op, operand } => write!(f, "({}{})", op, operand),
            Expr::Call { name, args } => {
                let arg_strs: Vec<String> = args.iter().map(|a| a.to_string()).collect();
                write!(f, "{}({})", name, arg_strs.join(", "))
            }
        }
    }
}

#[derive(Debug, Clone)]
pub enum Stmt {
    Let { name: String, value: Expr },
    Return(Expr),
    If { cond: Expr, then_block: Vec<Stmt>, else_block: Option<Vec<Stmt>> },
    While { cond: Expr, body: Vec<Stmt> },
    Expr(Expr),
}

// --- IR (Three-Address Code) ---

#[derive(Debug, Clone)]
pub enum TACInstr {
    Const { dst: String, val: i64 },
    Copy  { dst: String, src: String },
    BinOp { dst: String, op: String, a: String, b: String },
    Unary { dst: String, op: String, a: String },
    Jump  { label: String },
    JumpIf { cond: String, label: String },
    JumpIfNot { cond: String, label: String },
    Label(String),
    Call { dst: String, func_name: String, args: Vec<String> },
    Return { val: String },
}

impl fmt::Display for TACInstr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            TACInstr::Const { dst, val } => write!(f, "  {} = {}", dst, val),
            TACInstr::Copy { dst, src }  => write!(f, "  {} = {}", dst, src),
            TACInstr::BinOp { dst, op, a, b } => write!(f, "  {} = {} {} {}", dst, a, op, b),
            TACInstr::Unary { dst, op, a }    => write!(f, "  {} = {}{}", dst, op, a),
            TACInstr::Jump { label }          => write!(f, "  goto {}", label),
            TACInstr::JumpIf { cond, label }  => write!(f, "  if {} goto {}", cond, label),
            TACInstr::JumpIfNot { cond, label } => write!(f, "  ifnot {} goto {}", cond, label),
            TACInstr::Label(l) => write!(f, "{}:", l),
            TACInstr::Call { dst, func_name, args } => {
                write!(f, "  {} = call {}({})", dst, func_name, args.join(", "))
            }
            TACInstr::Return { val } => write!(f, "  return {}", val),
        }
    }
}

// --- IR Generator ---

pub struct IRGen {
    instrs: Vec<TACInstr>,
    tmp_count: usize,
    lbl_count: usize,
    env: HashMap<String, String>, // var name → tac temp name
}

impl IRGen {
    pub fn new() -> Self {
        IRGen {
            instrs: Vec::new(),
            tmp_count: 0,
            lbl_count: 0,
            env: HashMap::new(),
        }
    }

    fn new_temp(&mut self) -> String {
        self.tmp_count += 1;
        format!("t{}", self.tmp_count)
    }

    fn new_label(&mut self) -> String {
        self.lbl_count += 1;
        format!("L{}", self.lbl_count)
    }

    fn emit(&mut self, i: TACInstr) {
        self.instrs.push(i);
    }

    pub fn gen_expr(&mut self, expr: &Expr) -> String {
        match expr {
            Expr::Int(n) => {
                let t = self.new_temp();
                self.emit(TACInstr::Const { dst: t.clone(), val: *n });
                t
            }
            Expr::Bool(b) => {
                let t = self.new_temp();
                self.emit(TACInstr::Const { dst: t.clone(), val: if *b { 1 } else { 0 } });
                t
            }
            Expr::Ident(name) => {
                self.env.get(name)
                    .cloned()
                    .unwrap_or_else(|| panic!("undefined variable: {}", name))
            }
            Expr::BinOp { op, left, right } => {
                let l = self.gen_expr(left);
                let r = self.gen_expr(right);
                let t = self.new_temp();
                self.emit(TACInstr::BinOp { dst: t.clone(), op: op.clone(), a: l, b: r });
                t
            }
            Expr::UnaryOp { op, operand } => {
                let a = self.gen_expr(operand);
                let t = self.new_temp();
                self.emit(TACInstr::Unary { dst: t.clone(), op: op.clone(), a });
                t
            }
            Expr::Call { name, args } => {
                let arg_temps: Vec<String> = args.iter().map(|a| self.gen_expr(a)).collect();
                let t = self.new_temp();
                self.emit(TACInstr::Call {
                    dst: t.clone(),
                    func_name: name.clone(),
                    args: arg_temps,
                });
                t
            }
        }
    }

    pub fn gen_stmt(&mut self, stmt: &Stmt) {
        match stmt {
            Stmt::Let { name, value } => {
                let t = self.gen_expr(value);
                self.env.insert(name.clone(), t);
            }
            Stmt::Return(expr) => {
                let t = self.gen_expr(expr);
                self.emit(TACInstr::Return { val: t });
            }
            Stmt::Expr(expr) => {
                self.gen_expr(expr);
            }
            Stmt::If { cond, then_block, else_block } => {
                let cond_t = self.gen_expr(cond);
                let else_label = self.new_label();
                let end_label = self.new_label();
                self.emit(TACInstr::JumpIfNot { cond: cond_t, label: else_label.clone() });
                for s in then_block { self.gen_stmt(s); }
                self.emit(TACInstr::Jump { label: end_label.clone() });
                self.emit(TACInstr::Label(else_label));
                if let Some(else_stmts) = else_block {
                    for s in else_stmts { self.gen_stmt(s); }
                }
                self.emit(TACInstr::Label(end_label));
            }
            Stmt::While { cond, body } => {
                let top_label = self.new_label();
                let end_label = self.new_label();
                self.emit(TACInstr::Label(top_label.clone()));
                let cond_t = self.gen_expr(cond);
                self.emit(TACInstr::JumpIfNot { cond: cond_t, label: end_label.clone() });
                for s in body { self.gen_stmt(s); }
                self.emit(TACInstr::Jump { label: top_label });
                self.emit(TACInstr::Label(end_label));
            }
        }
    }

    pub fn instructions(&self) -> &[TACInstr] {
        &self.instrs
    }
}

fn main() {
    // let x = 10
    // let y = x * 2 + 3
    // if y > 5 { return y } else { return -1 }
    let program = vec![
        Stmt::Let {
            name: "x".into(),
            value: Expr::Int(10),
        },
        Stmt::Let {
            name: "y".into(),
            value: Expr::BinOp {
                op: "+".into(),
                left: Box::new(Expr::BinOp {
                    op: "*".into(),
                    left: Box::new(Expr::Ident("x".into())),
                    right: Box::new(Expr::Int(2)),
                }),
                right: Box::new(Expr::Int(3)),
            },
        },
        Stmt::If {
            cond: Expr::BinOp {
                op: ">".into(),
                left: Box::new(Expr::Ident("y".into())),
                right: Box::new(Expr::Int(5)),
            },
            then_block: vec![Stmt::Return(Expr::Ident("y".into()))],
            else_block: Some(vec![
                Stmt::Return(Expr::UnaryOp {
                    op: "-".into(),
                    operand: Box::new(Expr::Int(1)),
                }),
            ]),
        },
    ];

    println!("=== AST ===");
    for s in &program {
        println!("{:?}", s);
    }

    let mut gen = IRGen::new();
    for s in &program {
        gen.gen_stmt(s);
    }

    println!("\n=== Three-Address Code IR ===");
    for instr in gen.instructions() {
        println!("{}", instr);
    }
}
```

### Rust-specific considerations

**Enum-based AST vs interface-based**: Rust's `enum` + `match` is exhaustive — if you add a new `Expr` variant, every `match` on `Expr` that does not have a wildcard arm fails to compile. This is the opposite of Go's interface approach, where adding a new type silently passes through type switches that do not handle it. For a compiler AST, exhaustive matching is a safety net: it ensures every optimization pass handles every node kind.

**`Box<Expr>` in recursive variants**: `Box::new(Expr::BinOp {...})` allocates on the heap. In a large codebase where the compiler creates millions of AST nodes, this becomes measurable. The `bumpalo` crate (arena allocator) can reduce allocation overhead to near-zero for AST nodes: all nodes are allocated from a single contiguous buffer and freed at once when the compilation unit completes.

**Viewing Rust's MIR**: Compile with `RUSTFLAGS="--emit=mir" cargo build`. The `.mir` file is in `target/debug/deps/`. MIR is surprisingly readable — it looks like the TAC we generated above. Each function is a list of basic blocks, each block is a list of statements, and each statement is a three-address assignment or a terminator (branch/return). The borrow checker operates on this representation.

**Why MIR exists (not in the spec but important)**: Before MIR (introduced in Rust 1.x), the borrow checker operated on the AST. This made it extremely conservative — it could not prove safe code safe because the AST structure confused it. Moving to MIR (a simpler, normalized form) allowed non-lexical lifetimes (NLL) to be implemented, which made the borrow checker significantly more permissive for safe patterns.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|----|------|
| AST node types | Interfaces (`Stmt`, `Expr`) | Enums with variants |
| Dispatch mechanism | Type switch (runtime) | `match` (compile-time checked) |
| Exhaustiveness | Not enforced — type switch falls through | Enforced — missing variant = compile error |
| Recursive types | Pointer fields: `Left Expr` (interface = pointer + type) | Explicit `Box<Expr>` |
| IR level | SSA IR in `cmd/compile/internal/ssa` | MIR in `rustc_middle::mir` |
| Optimizer backend | Custom SSA passes + platform-specific lowering | LLVM (full optimization pipeline) |
| Viewing IR | `GOSSAFUNC=func go build` → ssa.html | `--emit=mir`, `--emit=llvm-ir` |
| Borrow checking level | N/A | MIR (after desugaring, before codegen) |

## Production War Stories

**LLVM IR as a stability contract**: LLVM IR is the public interface between Clang/rustc and LLVM's backend. This means the frontend can evolve independently of the backend. When LLVM 14 changed the default behavior of `undef` values (switching to `poison` semantics to enable more aggressive optimizations), several codebases that relied on `undef` for intentional uninitialized reads broke silently. The lesson: LLVM IR has well-defined semantics, and relying on undefined behavior in IR is just as dangerous as in C.

**Rust's MIR and the 2018 NLL rollout**: The borrow checker rewrite onto MIR (non-lexical lifetimes) was a multi-year effort. The old AST-based borrow checker rejected code like:
```rust
let mut v = vec![1, 2, 3];
let x = &v[0]; // borrow starts here
v.push(4);     // OLD: rejected — borrow still "active"
println!("{}", x); // new: borrow actually ends here per NLL
```
NLL proved that the borrow ended before the push. This required the expressiveness of MIR's control flow representation to analyze properly.

**The Go SSA IR and escape analysis bugs**: Go's escape analysis sometimes fails to prove that a value stays on the stack, forcing a heap allocation. For example, any interface conversion of a value type causes the value to escape (it must be boxed). If you convert `int` to `interface{}` in a hot path, the compiler allocates a new `int` on the heap every time. The fix is to avoid the interface conversion, or use sync.Pool. The SSA IR shows this as a `newobject` instruction — visible in `GOSSAFUNC` output.

## Complexity Analysis

| Transformation | Time | Space |
|---------------|------|-------|
| AST construction | O(n) in token count | O(n) tree nodes |
| IR lowering (AST → TAC) | O(n) in AST nodes | O(n) TAC instructions |
| SSA construction | O(n · α(n)) with path compression | O(n) for phi nodes |
| Basic block partition | O(n) in instruction count | O(n) for block list |

## Common Pitfalls

**1. One struct for all node types with optional fields.** A `Node` struct with 20 fields where 18 are nil for most node kinds wastes memory and makes code brittle. Every field access becomes a potential nil dereference. Use typed nodes.

**2. Not preserving source spans in the IR.** Optimization passes can eliminate or merge instructions. If the IR does not carry source positions, error messages for optimized code point to wrong locations. Store position metadata as a side table indexed by instruction ID.

**3. Mutating the AST during traversal.** Visitors that modify the tree structure while iterating it cause bugs that are difficult to reproduce. Prefer two-pass: collect modifications in one pass, apply them in a second pass.

**4. Generating IR for dead code.** If a `let` binding is never used, generating TAC for its initializer wastes IR space and time. A quick liveness pre-pass before IR generation avoids this.

**5. Using the AST as the optimization target.** AST-level constant folding (`1 + 2` → `3` during parsing) is tempting but couples optimization to parsing. The clean separation: all optimizations happen on the IR, where the simpler structure makes them easier to reason about.

## Exercises

**Exercise 1** (30 min): Add a `Type` field to each `Expr` variant in the Rust implementation. Implement a type-checking pass that walks the AST and annotates each expression with its inferred type (`Int`, `Bool`, or `Unknown`). Emit an error if a `BinOp` mixes `Int` and `Bool` operands.

**Exercise 2** (2–4h): Implement a constant-folding pass on the TAC IR (not the AST). Walk the instruction list, track constants for each temp, and replace uses with the constant value. Then eliminate assignments to temps that are used exactly once (copy propagation). Measure how many instructions you eliminate on the example program.

**Exercise 3** (4–8h): Add basic block construction to the IR generator. After generating TAC, partition the instruction list into basic blocks and build the CFG (control flow graph). Represent the CFG as a directed graph where each edge is a `(source_block, target_label)` pair. Print the CFG in DOT format (Graphviz) so you can visualize it.

**Exercise 4** (8–15h): Implement SSA construction on your basic block CFG. Use the algorithm from Braun et al. (2013) — it is simpler than the classic Cytron et al. algorithm. For each variable, track which block defines it. Insert phi nodes at join points (blocks with multiple predecessors where different values of a variable reach). Rename all uses to the unique SSA name. Verify correctness by checking that every use has exactly one reaching definition.

## Further Reading

### Foundational Papers
- **LLVM: A Compilation Framework for Lifelong Program Analysis & Transformation** (Lattner & Adve, CGO 2004) — The original LLVM paper. Explains why a typed SSA IR is the right target for a compiler framework.
- **Braun et al., 2013** — "Simple and Efficient Construction of Static Single Assignment Form." A simpler SSA construction algorithm than Cytron et al. — start here.
- **Cytron et al., 1991** — "Efficiently Computing Static Single Assignment Form and the Control Dependence Graph." The canonical SSA construction algorithm. More complex but handles more cases.

### Books
- **Engineering a Compiler** (Cooper & Torczon) — Chapter 5: Intermediate Representations. Thorough treatment of TAC, SSA, and their tradeoffs.
- **Crafting Interpreters** — Chapters 17–25: bytecode VM implementation. Practical IR design in a teaching context.
- **The LLVM Project** — `https://llvm.org/docs/LangRef.html`. The LLVM IR reference is the definitive source for SSA IR design decisions.

### Production Code to Read
- `go/src/cmd/compile/internal/ir/` — Go's AST node definitions
- `go/src/cmd/compile/internal/ssa/` — Go's SSA IR, values, blocks, passes
- `compiler/rustc_middle/src/mir/` — Rust's MIR definitions
- `llvm-project/llvm/include/llvm/IR/` — LLVM IR C++ types (Instruction, BasicBlock, Function)

### Talks
- **"The Go compiler's SSA backend"** — David Chase, GopherCon 2016. Walks through `GOSSAFUNC` output.
- **"Rust's MIR"** — Felix Klock, RustConf 2016. Explains why MIR was introduced and what it enables.
- **"LLVM: A Modern, Open Compiler Infrastructure"** — Chris Lattner, LLVM Developers' Meeting. Historical context for LLVM IR design decisions.
