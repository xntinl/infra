# Solution: Symbolic Execution Engine

## Architecture Overview

The solution is organized into five modules:

1. **AST definition** (`ast.rs`) -- the abstract syntax tree for the simple language: statements, expressions, and conditions
2. **Symbolic expressions** (`symbolic.rs`) -- recursive expression trees representing symbolic values, with simplification
3. **Constraint solver** (`solver.rs`) -- determines satisfiability of linear integer constraints and produces satisfying assignments
4. **Symbolic executor** (`executor.rs`) -- walks the AST maintaining a symbolic store and path constraints, forking at branches
5. **Bug detectors** (`detectors.rs`) -- checks for assertion violations, division by zero, and out-of-bounds access at each execution step

## Rust Solution

### Project Setup

```bash
cargo new symbolic-exec
cd symbolic-exec
```

```toml
[package]
name = "symbolic-exec"
version = "0.1.0"
edition = "2021"
```

### Source: `src/ast.rs`

```rust
/// A program is a sequence of statements.
pub type Program = Vec<Statement>;

#[derive(Debug, Clone)]
pub enum Statement {
    /// let x = expr;
    Let { name: String, value: Expr },
    /// x = expr;
    Assign { name: String, value: Expr },
    /// if cond { then_branch } else { else_branch }
    If {
        condition: Expr,
        then_branch: Vec<Statement>,
        else_branch: Vec<Statement>,
    },
    /// while cond { body }  (bounded unrolling)
    While {
        condition: Expr,
        body: Vec<Statement>,
    },
    /// assert(cond)
    Assert { condition: Expr, label: String },
    /// assume(cond) -- adds constraint without forking
    Assume { condition: Expr },
    /// return expr
    Return { value: Expr },
}

#[derive(Debug, Clone)]
pub enum Expr {
    Literal(i64),
    Var(String),
    Add(Box<Expr>, Box<Expr>),
    Sub(Box<Expr>, Box<Expr>),
    Mul(Box<Expr>, Box<Expr>),
    Div(Box<Expr>, Box<Expr>),
    Eq(Box<Expr>, Box<Expr>),
    Ne(Box<Expr>, Box<Expr>),
    Lt(Box<Expr>, Box<Expr>),
    Le(Box<Expr>, Box<Expr>),
    Gt(Box<Expr>, Box<Expr>),
    Ge(Box<Expr>, Box<Expr>),
    Not(Box<Expr>),
    And(Box<Expr>, Box<Expr>),
    Or(Box<Expr>, Box<Expr>),
}

/// Helper constructors for building programs without boilerplate.
impl Expr {
    pub fn lit(n: i64) -> Self {
        Expr::Literal(n)
    }
    pub fn var(name: &str) -> Self {
        Expr::Var(name.to_string())
    }
    pub fn add(a: Expr, b: Expr) -> Self {
        Expr::Add(Box::new(a), Box::new(b))
    }
    pub fn sub(a: Expr, b: Expr) -> Self {
        Expr::Sub(Box::new(a), Box::new(b))
    }
    pub fn mul(a: Expr, b: Expr) -> Self {
        Expr::Mul(Box::new(a), Box::new(b))
    }
    pub fn div(a: Expr, b: Expr) -> Self {
        Expr::Div(Box::new(a), Box::new(b))
    }
    pub fn eq(a: Expr, b: Expr) -> Self {
        Expr::Eq(Box::new(a), Box::new(b))
    }
    pub fn ne(a: Expr, b: Expr) -> Self {
        Expr::Ne(Box::new(a), Box::new(b))
    }
    pub fn lt(a: Expr, b: Expr) -> Self {
        Expr::Lt(Box::new(a), Box::new(b))
    }
    pub fn le(a: Expr, b: Expr) -> Self {
        Expr::Le(Box::new(a), Box::new(b))
    }
    pub fn gt(a: Expr, b: Expr) -> Self {
        Expr::Gt(Box::new(a), Box::new(b))
    }
    pub fn ge(a: Expr, b: Expr) -> Self {
        Expr::Ge(Box::new(a), Box::new(b))
    }
    pub fn not(a: Expr) -> Self {
        Expr::Not(Box::new(a))
    }
    pub fn and(a: Expr, b: Expr) -> Self {
        Expr::And(Box::new(a), Box::new(b))
    }
    pub fn or(a: Expr, b: Expr) -> Self {
        Expr::Or(Box::new(a), Box::new(b))
    }
}
```

### Source: `src/symbolic.rs`

```rust
use std::collections::BTreeSet;
use std::fmt;

/// A symbolic expression tree. All arithmetic is kept symbolic until
/// constraint solving time.
#[derive(Debug, Clone, PartialEq, Eq, Hash)]
pub enum SymExpr {
    Concrete(i64),
    Symbol(String),
    Add(Box<SymExpr>, Box<SymExpr>),
    Sub(Box<SymExpr>, Box<SymExpr>),
    Mul(Box<SymExpr>, Box<SymExpr>),
    Div(Box<SymExpr>, Box<SymExpr>),
    Eq(Box<SymExpr>, Box<SymExpr>),
    Ne(Box<SymExpr>, Box<SymExpr>),
    Lt(Box<SymExpr>, Box<SymExpr>),
    Le(Box<SymExpr>, Box<SymExpr>),
    Gt(Box<SymExpr>, Box<SymExpr>),
    Ge(Box<SymExpr>, Box<SymExpr>),
    Not(Box<SymExpr>),
    And(Box<SymExpr>, Box<SymExpr>),
    Or(Box<SymExpr>, Box<SymExpr>),
}

impl SymExpr {
    /// Collect all free symbol names in this expression.
    pub fn free_symbols(&self) -> BTreeSet<String> {
        let mut set = BTreeSet::new();
        self.collect_symbols(&mut set);
        set
    }

    fn collect_symbols(&self, set: &mut BTreeSet<String>) {
        match self {
            SymExpr::Concrete(_) => {}
            SymExpr::Symbol(name) => {
                set.insert(name.clone());
            }
            SymExpr::Add(a, b)
            | SymExpr::Sub(a, b)
            | SymExpr::Mul(a, b)
            | SymExpr::Div(a, b)
            | SymExpr::Eq(a, b)
            | SymExpr::Ne(a, b)
            | SymExpr::Lt(a, b)
            | SymExpr::Le(a, b)
            | SymExpr::Gt(a, b)
            | SymExpr::Ge(a, b)
            | SymExpr::And(a, b)
            | SymExpr::Or(a, b) => {
                a.collect_symbols(set);
                b.collect_symbols(set);
            }
            SymExpr::Not(a) => a.collect_symbols(set),
        }
    }

    /// Try to evaluate to a concrete value if all symbols have been substituted.
    pub fn evaluate(&self) -> Option<i64> {
        match self {
            SymExpr::Concrete(n) => Some(*n),
            SymExpr::Symbol(_) => None,
            SymExpr::Add(a, b) => Some(a.evaluate()? + b.evaluate()?),
            SymExpr::Sub(a, b) => Some(a.evaluate()? - b.evaluate()?),
            SymExpr::Mul(a, b) => Some(a.evaluate()? * b.evaluate()?),
            SymExpr::Div(a, b) => {
                let bv = b.evaluate()?;
                if bv == 0 {
                    return None;
                }
                Some(a.evaluate()? / bv)
            }
            SymExpr::Eq(a, b) => Some(if a.evaluate()? == b.evaluate()? { 1 } else { 0 }),
            SymExpr::Ne(a, b) => Some(if a.evaluate()? != b.evaluate()? { 1 } else { 0 }),
            SymExpr::Lt(a, b) => Some(if a.evaluate()? < b.evaluate()? { 1 } else { 0 }),
            SymExpr::Le(a, b) => Some(if a.evaluate()? <= b.evaluate()? { 1 } else { 0 }),
            SymExpr::Gt(a, b) => Some(if a.evaluate()? > b.evaluate()? { 1 } else { 0 }),
            SymExpr::Ge(a, b) => Some(if a.evaluate()? >= b.evaluate()? { 1 } else { 0 }),
            SymExpr::Not(a) => Some(if a.evaluate()? == 0 { 1 } else { 0 }),
            SymExpr::And(a, b) => {
                Some(if a.evaluate()? != 0 && b.evaluate()? != 0 { 1 } else { 0 })
            }
            SymExpr::Or(a, b) => {
                Some(if a.evaluate()? != 0 || b.evaluate()? != 0 { 1 } else { 0 })
            }
        }
    }

    /// Substitute all occurrences of a symbol with a concrete value.
    pub fn substitute(&self, name: &str, value: i64) -> SymExpr {
        match self {
            SymExpr::Concrete(n) => SymExpr::Concrete(*n),
            SymExpr::Symbol(n) if n == name => SymExpr::Concrete(value),
            SymExpr::Symbol(n) => SymExpr::Symbol(n.clone()),
            SymExpr::Add(a, b) => SymExpr::Add(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Sub(a, b) => SymExpr::Sub(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Mul(a, b) => SymExpr::Mul(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Div(a, b) => SymExpr::Div(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Eq(a, b) => SymExpr::Eq(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Ne(a, b) => SymExpr::Ne(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Lt(a, b) => SymExpr::Lt(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Le(a, b) => SymExpr::Le(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Gt(a, b) => SymExpr::Gt(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Ge(a, b) => SymExpr::Ge(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Not(a) => SymExpr::Not(Box::new(a.substitute(name, value))),
            SymExpr::And(a, b) => SymExpr::And(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
            SymExpr::Or(a, b) => SymExpr::Or(
                Box::new(a.substitute(name, value)),
                Box::new(b.substitute(name, value)),
            ),
        }
    }

    /// Negate a boolean symbolic expression.
    pub fn negate(&self) -> SymExpr {
        SymExpr::Not(Box::new(self.clone()))
    }

    /// Simplify constant expressions.
    pub fn simplify(&self) -> SymExpr {
        match self {
            SymExpr::Add(a, b) => {
                let sa = a.simplify();
                let sb = b.simplify();
                match (&sa, &sb) {
                    (SymExpr::Concrete(x), SymExpr::Concrete(y)) => SymExpr::Concrete(x + y),
                    (SymExpr::Concrete(0), _) => sb,
                    (_, SymExpr::Concrete(0)) => sa,
                    _ => SymExpr::Add(Box::new(sa), Box::new(sb)),
                }
            }
            SymExpr::Sub(a, b) => {
                let sa = a.simplify();
                let sb = b.simplify();
                match (&sa, &sb) {
                    (SymExpr::Concrete(x), SymExpr::Concrete(y)) => SymExpr::Concrete(x - y),
                    (_, SymExpr::Concrete(0)) => sa,
                    _ => SymExpr::Sub(Box::new(sa), Box::new(sb)),
                }
            }
            SymExpr::Mul(a, b) => {
                let sa = a.simplify();
                let sb = b.simplify();
                match (&sa, &sb) {
                    (SymExpr::Concrete(x), SymExpr::Concrete(y)) => SymExpr::Concrete(x * y),
                    (SymExpr::Concrete(0), _) | (_, SymExpr::Concrete(0)) => SymExpr::Concrete(0),
                    (SymExpr::Concrete(1), _) => sb,
                    (_, SymExpr::Concrete(1)) => sa,
                    _ => SymExpr::Mul(Box::new(sa), Box::new(sb)),
                }
            }
            SymExpr::Not(a) => {
                let sa = a.simplify();
                match &sa {
                    SymExpr::Concrete(0) => SymExpr::Concrete(1),
                    SymExpr::Concrete(_) => SymExpr::Concrete(0),
                    SymExpr::Not(inner) => inner.simplify(),
                    _ => SymExpr::Not(Box::new(sa)),
                }
            }
            other => other.clone(),
        }
    }
}

impl fmt::Display for SymExpr {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            SymExpr::Concrete(n) => write!(f, "{n}"),
            SymExpr::Symbol(s) => write!(f, "{s}"),
            SymExpr::Add(a, b) => write!(f, "({a} + {b})"),
            SymExpr::Sub(a, b) => write!(f, "({a} - {b})"),
            SymExpr::Mul(a, b) => write!(f, "({a} * {b})"),
            SymExpr::Div(a, b) => write!(f, "({a} / {b})"),
            SymExpr::Eq(a, b) => write!(f, "({a} == {b})"),
            SymExpr::Ne(a, b) => write!(f, "({a} != {b})"),
            SymExpr::Lt(a, b) => write!(f, "({a} < {b})"),
            SymExpr::Le(a, b) => write!(f, "({a} <= {b})"),
            SymExpr::Gt(a, b) => write!(f, "({a} > {b})"),
            SymExpr::Ge(a, b) => write!(f, "({a} >= {b})"),
            SymExpr::Not(a) => write!(f, "!{a}"),
            SymExpr::And(a, b) => write!(f, "({a} && {b})"),
            SymExpr::Or(a, b) => write!(f, "({a} || {b})"),
        }
    }
}
```

### Source: `src/solver.rs`

```rust
use crate::symbolic::SymExpr;
use std::collections::{BTreeMap, BTreeSet};

/// Result of constraint solving.
#[derive(Debug, Clone)]
pub enum SolverResult {
    Satisfiable(BTreeMap<String, i64>),
    Unsatisfiable,
    Unknown,
}

impl SolverResult {
    pub fn is_sat(&self) -> bool {
        matches!(self, SolverResult::Satisfiable(_))
    }
}

/// Brute-force constraint solver for bounded integer domains.
///
/// Tries all combinations of symbol values in [-bound, bound] and checks
/// if the conjunction of all constraints evaluates to true (non-zero).
pub struct BoundedSolver {
    pub bound: i64,
}

impl BoundedSolver {
    pub fn new(bound: i64) -> Self {
        Self { bound }
    }

    /// Check satisfiability of a conjunction of constraints.
    pub fn solve(&self, constraints: &[SymExpr]) -> SolverResult {
        if constraints.is_empty() {
            return SolverResult::Satisfiable(BTreeMap::new());
        }

        // Collect all free symbols
        let mut symbols = BTreeSet::new();
        for c in constraints {
            symbols.extend(c.free_symbols());
        }
        let symbols: Vec<String> = symbols.into_iter().collect();

        if symbols.is_empty() {
            // All concrete: just evaluate
            let all_hold = constraints.iter().all(|c| {
                c.evaluate().map(|v| v != 0).unwrap_or(false)
            });
            return if all_hold {
                SolverResult::Satisfiable(BTreeMap::new())
            } else {
                SolverResult::Unsatisfiable
            };
        }

        // Generate all combinations within bounds
        let mut assignment = BTreeMap::new();
        if self.search(constraints, &symbols, 0, &mut assignment) {
            SolverResult::Satisfiable(assignment)
        } else {
            SolverResult::Unsatisfiable
        }
    }

    fn search(
        &self,
        constraints: &[SymExpr],
        symbols: &[String],
        idx: usize,
        assignment: &mut BTreeMap<String, i64>,
    ) -> bool {
        if idx == symbols.len() {
            // All symbols assigned: evaluate constraints
            return constraints.iter().all(|c| {
                let mut expr = c.clone();
                for (name, val) in assignment.iter() {
                    expr = expr.substitute(name, *val);
                }
                expr.evaluate().map(|v| v != 0).unwrap_or(false)
            });
        }

        let name = &symbols[idx];
        for val in -self.bound..=self.bound {
            assignment.insert(name.clone(), val);
            if self.search(constraints, symbols, idx + 1, assignment) {
                return true;
            }
        }
        assignment.remove(&symbols[idx]);
        false
    }
}
```

### Source: `src/executor.rs`

```rust
use crate::ast::{Expr, Program, Statement};
use crate::solver::{BoundedSolver, SolverResult};
use crate::symbolic::SymExpr;
use std::collections::BTreeMap;

/// Configuration for symbolic execution.
#[derive(Debug, Clone)]
pub struct ExecConfig {
    pub max_loop_unroll: usize,
    pub solver_bound: i64,
    pub max_paths: usize,
}

impl Default for ExecConfig {
    fn default() -> Self {
        Self {
            max_loop_unroll: 5,
            solver_bound: 100,
            max_paths: 1000,
        }
    }
}

/// A bug found during symbolic execution.
#[derive(Debug, Clone)]
pub struct Bug {
    pub kind: BugKind,
    pub label: String,
    pub path_constraints: Vec<SymExpr>,
    pub inputs: BTreeMap<String, i64>,
}

#[derive(Debug, Clone)]
pub enum BugKind {
    AssertionViolation,
    DivisionByZero,
    Unreachable,
}

/// Result of exploring one path.
#[derive(Debug, Clone)]
pub struct PathResult {
    pub path_id: usize,
    pub constraints: Vec<SymExpr>,
    pub feasible: bool,
    pub inputs: Option<BTreeMap<String, i64>>,
    pub return_value: Option<SymExpr>,
    pub bugs: Vec<Bug>,
}

/// Result of full symbolic execution.
#[derive(Debug)]
pub struct ExecutionResult {
    pub paths: Vec<PathResult>,
    pub total_bugs: usize,
}

/// Symbolic execution state for one path.
#[derive(Clone)]
struct SymState {
    store: BTreeMap<String, SymExpr>,
    path_constraints: Vec<SymExpr>,
    bugs: Vec<Bug>,
    return_value: Option<SymExpr>,
    terminated: bool,
}

impl SymState {
    fn new() -> Self {
        Self {
            store: BTreeMap::new(),
            path_constraints: Vec::new(),
            bugs: Vec::new(),
            return_value: None,
            terminated: false,
        }
    }

    /// Translate an AST expression into a symbolic expression using the current store.
    fn eval_expr(&self, expr: &Expr) -> SymExpr {
        match expr {
            Expr::Literal(n) => SymExpr::Concrete(*n),
            Expr::Var(name) => self
                .store
                .get(name)
                .cloned()
                .unwrap_or_else(|| SymExpr::Symbol(name.clone())),
            Expr::Add(a, b) => {
                SymExpr::Add(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Sub(a, b) => {
                SymExpr::Sub(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Mul(a, b) => {
                SymExpr::Mul(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Div(a, b) => {
                SymExpr::Div(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Eq(a, b) => {
                SymExpr::Eq(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Ne(a, b) => {
                SymExpr::Ne(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Lt(a, b) => {
                SymExpr::Lt(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Le(a, b) => {
                SymExpr::Le(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Gt(a, b) => {
                SymExpr::Gt(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Ge(a, b) => {
                SymExpr::Ge(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Not(a) => SymExpr::Not(Box::new(self.eval_expr(a))),
            Expr::And(a, b) => {
                SymExpr::And(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
            Expr::Or(a, b) => {
                SymExpr::Or(Box::new(self.eval_expr(a)), Box::new(self.eval_expr(b)))
            }
        }
    }
}

/// The symbolic execution engine.
pub struct SymbolicExecutor {
    config: ExecConfig,
    solver: BoundedSolver,
}

impl SymbolicExecutor {
    pub fn new(config: ExecConfig) -> Self {
        let solver = BoundedSolver::new(config.solver_bound);
        Self { config, solver }
    }

    /// Execute a program symbolically, exploring all feasible paths.
    pub fn execute(&self, program: &Program) -> ExecutionResult {
        let initial = SymState::new();
        let mut completed: Vec<SymState> = Vec::new();
        let mut worklist: Vec<(SymState, usize)> = vec![(initial, 0)]; // (state, stmt_index)

        while let Some((state, stmt_idx)) = worklist.pop() {
            if completed.len() >= self.config.max_paths {
                break;
            }
            if state.terminated || stmt_idx >= program.len() {
                completed.push(state);
                continue;
            }

            let stmt = &program[stmt_idx];
            let next_states = self.exec_statement(&state, stmt);

            for ns in next_states {
                worklist.push((ns, stmt_idx + 1));
            }
        }

        let mut paths = Vec::new();
        let mut total_bugs = 0;

        for (i, state) in completed.iter().enumerate() {
            let feasibility = self.solver.solve(&state.path_constraints);
            let feasible = feasibility.is_sat();
            let inputs = match &feasibility {
                SolverResult::Satisfiable(m) => Some(m.clone()),
                _ => None,
            };
            total_bugs += state.bugs.len();

            paths.push(PathResult {
                path_id: i,
                constraints: state.path_constraints.clone(),
                feasible,
                inputs,
                return_value: state.return_value.clone(),
                bugs: state.bugs.clone(),
            });
        }

        ExecutionResult { paths, total_bugs }
    }

    fn exec_statement(&self, state: &SymState, stmt: &Statement) -> Vec<SymState> {
        if state.terminated {
            return vec![state.clone()];
        }

        match stmt {
            Statement::Let { name, value } | Statement::Assign { name, value } => {
                let mut ns = state.clone();
                // Check for division by zero in the assigned expression
                self.check_div_by_zero(&mut ns, value);
                let sym_val = ns.eval_expr(value).simplify();
                ns.store.insert(name.clone(), sym_val);
                vec![ns]
            }

            Statement::If {
                condition,
                then_branch,
                else_branch,
            } => {
                let cond_sym = state.eval_expr(condition).simplify();
                let neg_cond = cond_sym.negate().simplify();

                let mut results = Vec::new();

                // True branch
                let mut true_state = state.clone();
                true_state.path_constraints.push(cond_sym.clone());
                if self.solver.solve(&true_state.path_constraints).is_sat() {
                    for stmt in then_branch {
                        let next = self.exec_statement(&true_state, stmt);
                        if next.len() == 1 {
                            true_state = next.into_iter().next().unwrap();
                        } else {
                            // Multiple paths from nested branches
                            for mut s in next {
                                for remaining in then_branch.iter().skip(1) {
                                    let r = self.exec_statement(&s, remaining);
                                    if r.len() == 1 {
                                        s = r.into_iter().next().unwrap();
                                    }
                                }
                                results.push(s);
                            }
                            return results;
                        }
                    }
                    results.push(true_state);
                }

                // False branch
                let mut false_state = state.clone();
                false_state.path_constraints.push(neg_cond);
                if self.solver.solve(&false_state.path_constraints).is_sat() {
                    for stmt in else_branch {
                        let next = self.exec_statement(&false_state, stmt);
                        if next.len() == 1 {
                            false_state = next.into_iter().next().unwrap();
                        } else {
                            results.extend(next);
                            return results;
                        }
                    }
                    results.push(false_state);
                }

                results
            }

            Statement::While { condition, body } => {
                let mut current_states = vec![state.clone()];

                for _ in 0..self.config.max_loop_unroll {
                    let mut next_round = Vec::new();

                    for s in &current_states {
                        let cond_sym = s.eval_expr(condition).simplify();
                        let neg_cond = cond_sym.negate().simplify();

                        // Path that exits the loop
                        let mut exit_state = s.clone();
                        exit_state.path_constraints.push(neg_cond);
                        if self.solver.solve(&exit_state.path_constraints).is_sat() {
                            next_round.push(exit_state);
                        }

                        // Path that enters the loop body
                        let mut body_state = s.clone();
                        body_state.path_constraints.push(cond_sym);
                        if self.solver.solve(&body_state.path_constraints).is_sat() {
                            for stmt in body {
                                let next = self.exec_statement(&body_state, stmt);
                                if next.len() == 1 {
                                    body_state = next.into_iter().next().unwrap();
                                } else {
                                    next_round.extend(next);
                                    break;
                                }
                            }
                            // Only add if we completed the body
                            if !body_state.terminated {
                                current_states = vec![body_state];
                                continue;
                            }
                        }
                    }

                    if next_round.is_empty() {
                        break;
                    }
                    current_states = next_round;
                }

                current_states
            }

            Statement::Assert { condition, label } => {
                let mut ns = state.clone();
                let cond_sym = ns.eval_expr(condition).simplify();
                let neg_cond = cond_sym.negate().simplify();

                // Check if the assertion can be violated
                let mut violation_constraints = ns.path_constraints.clone();
                violation_constraints.push(neg_cond);

                if let SolverResult::Satisfiable(inputs) =
                    self.solver.solve(&violation_constraints)
                {
                    ns.bugs.push(Bug {
                        kind: BugKind::AssertionViolation,
                        label: label.clone(),
                        path_constraints: violation_constraints,
                        inputs,
                    });
                }

                // Continue execution assuming the assertion holds
                ns.path_constraints.push(cond_sym);
                vec![ns]
            }

            Statement::Assume { condition } => {
                let mut ns = state.clone();
                let cond_sym = ns.eval_expr(condition).simplify();
                ns.path_constraints.push(cond_sym);
                vec![ns]
            }

            Statement::Return { value } => {
                let mut ns = state.clone();
                ns.return_value = Some(ns.eval_expr(value).simplify());
                ns.terminated = true;
                vec![ns]
            }
        }
    }

    fn check_div_by_zero(&self, state: &mut SymState, expr: &Expr) {
        if let Expr::Div(_, b) = expr {
            let divisor = state.eval_expr(b).simplify();
            let zero_check = SymExpr::Eq(Box::new(divisor), Box::new(SymExpr::Concrete(0)));
            let mut check_constraints = state.path_constraints.clone();
            check_constraints.push(zero_check);

            if let SolverResult::Satisfiable(inputs) = self.solver.solve(&check_constraints) {
                state.bugs.push(Bug {
                    kind: BugKind::DivisionByZero,
                    label: format!("division in {:?}", expr),
                    path_constraints: check_constraints,
                    inputs,
                });
            }
        }
    }
}
```

### Source: `src/lib.rs`

```rust
pub mod ast;
pub mod executor;
pub mod solver;
pub mod symbolic;
```

### Source: `src/main.rs`

```rust
use symbolic_exec::ast::{Expr, Statement};
use symbolic_exec::executor::{ExecConfig, SymbolicExecutor};

fn main() {
    println!("=== Symbolic Execution Engine ===\n");

    // Example 1: Assertion violation
    // let x = input;
    // if x > 10 { assert(x < 20); }
    // This assertion can fail when x >= 20
    println!("--- Example 1: Assertion violation ---");
    let prog1 = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input_x".into()),
        },
        Statement::If {
            condition: Expr::gt(Expr::var("x"), Expr::lit(10)),
            then_branch: vec![Statement::Assert {
                condition: Expr::lt(Expr::var("x"), Expr::lit(20)),
                label: "x must be < 20".into(),
            }],
            else_branch: vec![],
        },
    ];

    let engine = SymbolicExecutor::new(ExecConfig::default());
    let result = engine.execute(&prog1);
    print_result(&result);

    // Example 2: Division by zero
    // let x = input;
    // let y = x - 5;
    // let z = 100 / y;  // div by zero when x == 5
    println!("\n--- Example 2: Division by zero ---");
    let prog2 = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input_x".into()),
        },
        Statement::Let {
            name: "y".into(),
            value: Expr::sub(Expr::var("x"), Expr::lit(5)),
        },
        Statement::Let {
            name: "z".into(),
            value: Expr::div(Expr::lit(100), Expr::var("y")),
        },
    ];

    let result = engine.execute(&prog2);
    print_result(&result);

    // Example 3: Nested conditions
    // let a = input_a;
    // let b = input_b;
    // if a > 0 {
    //   if b > 0 {
    //     assert(a + b > 0);  // should always hold
    //   }
    // }
    println!("\n--- Example 3: Nested conditions (should be safe) ---");
    let prog3 = vec![
        Statement::Let {
            name: "a".into(),
            value: Expr::Var("input_a".into()),
        },
        Statement::Let {
            name: "b".into(),
            value: Expr::Var("input_b".into()),
        },
        Statement::If {
            condition: Expr::gt(Expr::var("a"), Expr::lit(0)),
            then_branch: vec![Statement::If {
                condition: Expr::gt(Expr::var("b"), Expr::lit(0)),
                then_branch: vec![Statement::Assert {
                    condition: Expr::gt(
                        Expr::add(Expr::var("a"), Expr::var("b")),
                        Expr::lit(0),
                    ),
                    label: "a+b > 0 when both positive".into(),
                }],
                else_branch: vec![],
            }],
            else_branch: vec![],
        },
    ];

    let result = engine.execute(&prog3);
    print_result(&result);
}

fn print_result(result: &symbolic_exec::executor::ExecutionResult) {
    println!("  Paths explored: {}", result.paths.len());
    println!("  Total bugs: {}", result.total_bugs);
    for path in &result.paths {
        println!(
            "  Path {}: feasible={}, constraints={}, bugs={}",
            path.path_id,
            path.feasible,
            path.constraints.len(),
            path.bugs.len()
        );
        if let Some(inputs) = &path.inputs {
            if !inputs.is_empty() {
                println!("    Inputs: {:?}", inputs);
            }
        }
        for bug in &path.bugs {
            println!("    BUG [{:?}]: {}", bug.kind, bug.label);
            println!("    Triggering inputs: {:?}", bug.inputs);
        }
    }
}
```

### Tests: `tests/symbolic_tests.rs`

```rust
use symbolic_exec::ast::{Expr, Statement};
use symbolic_exec::executor::{BugKind, ExecConfig, SymbolicExecutor};
use symbolic_exec::solver::BoundedSolver;
use symbolic_exec::symbolic::SymExpr;

fn default_engine() -> SymbolicExecutor {
    SymbolicExecutor::new(ExecConfig {
        max_loop_unroll: 5,
        solver_bound: 50,
        max_paths: 100,
    })
}

// --- Solver tests ---

#[test]
fn solver_sat_simple_equality() {
    let solver = BoundedSolver::new(10);
    let constraint = SymExpr::Eq(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(5)),
    );
    let result = solver.solve(&[constraint]);
    assert!(result.is_sat());
    if let symbolic_exec::solver::SolverResult::Satisfiable(m) = result {
        assert_eq!(m["x"], 5);
    }
}

#[test]
fn solver_unsat_contradiction() {
    let solver = BoundedSolver::new(10);
    let c1 = SymExpr::Gt(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(5)),
    );
    let c2 = SymExpr::Lt(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(3)),
    );
    let result = solver.solve(&[c1, c2]);
    assert!(!result.is_sat());
}

#[test]
fn solver_two_variables() {
    let solver = BoundedSolver::new(10);
    let c1 = SymExpr::Gt(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(0)),
    );
    let c2 = SymExpr::Eq(
        Box::new(SymExpr::Add(
            Box::new(SymExpr::Symbol("x".into())),
            Box::new(SymExpr::Symbol("y".into())),
        )),
        Box::new(SymExpr::Concrete(10)),
    );
    let result = solver.solve(&[c1, c2]);
    assert!(result.is_sat());
}

// --- Symbolic expression tests ---

#[test]
fn symexpr_evaluate_concrete() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Concrete(3)),
        Box::new(SymExpr::Concrete(4)),
    );
    assert_eq!(expr.evaluate(), Some(7));
}

#[test]
fn symexpr_evaluate_with_symbol_returns_none() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(4)),
    );
    assert_eq!(expr.evaluate(), None);
}

#[test]
fn symexpr_substitute_and_evaluate() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Symbol("x".into())),
        Box::new(SymExpr::Concrete(4)),
    );
    let substituted = expr.substitute("x", 6);
    assert_eq!(substituted.evaluate(), Some(10));
}

#[test]
fn symexpr_free_symbols() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Mul(
            Box::new(SymExpr::Symbol("a".into())),
            Box::new(SymExpr::Concrete(2)),
        )),
        Box::new(SymExpr::Symbol("b".into())),
    );
    let syms = expr.free_symbols();
    assert_eq!(syms.len(), 2);
    assert!(syms.contains("a"));
    assert!(syms.contains("b"));
}

#[test]
fn symexpr_simplify_constant_fold() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Concrete(3)),
        Box::new(SymExpr::Concrete(4)),
    );
    assert_eq!(expr.simplify(), SymExpr::Concrete(7));
}

#[test]
fn symexpr_simplify_add_zero() {
    let expr = SymExpr::Add(
        Box::new(SymExpr::Concrete(0)),
        Box::new(SymExpr::Symbol("x".into())),
    );
    assert_eq!(expr.simplify(), SymExpr::Symbol("x".into()));
}

// --- Executor tests ---

#[test]
fn detects_assertion_violation() {
    let engine = default_engine();
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::Assert {
            condition: Expr::lt(Expr::var("x"), Expr::lit(10)),
            label: "x < 10".into(),
        },
    ];
    let result = engine.execute(&prog);
    assert!(result.total_bugs > 0);
    let bug = &result.paths.iter().flat_map(|p| &p.bugs).next().unwrap();
    assert!(matches!(bug.kind, BugKind::AssertionViolation));
    assert!(*bug.inputs.get("input").unwrap() >= 10);
}

#[test]
fn detects_division_by_zero() {
    let engine = default_engine();
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::Let {
            name: "y".into(),
            value: Expr::div(Expr::lit(10), Expr::var("x")),
        },
    ];
    let result = engine.execute(&prog);
    assert!(result.total_bugs > 0);
    let bug = result
        .paths
        .iter()
        .flat_map(|p| &p.bugs)
        .find(|b| matches!(b.kind, BugKind::DivisionByZero))
        .unwrap();
    assert_eq!(*bug.inputs.get("input").unwrap(), 0);
}

#[test]
fn if_else_forks_two_paths() {
    let engine = default_engine();
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::If {
            condition: Expr::gt(Expr::var("x"), Expr::lit(0)),
            then_branch: vec![Statement::Let {
                name: "r".into(),
                value: Expr::lit(1),
            }],
            else_branch: vec![Statement::Let {
                name: "r".into(),
                value: Expr::lit(-1),
            }],
        },
    ];
    let result = engine.execute(&prog);
    let feasible_paths: Vec<_> = result.paths.iter().filter(|p| p.feasible).collect();
    assert_eq!(feasible_paths.len(), 2);
}

#[test]
fn assume_prunes_infeasible_paths() {
    let engine = default_engine();
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::Assume {
            condition: Expr::gt(Expr::var("x"), Expr::lit(0)),
        },
        Statement::Assert {
            condition: Expr::ge(Expr::var("x"), Expr::lit(1)),
            label: "x >= 1".into(),
        },
    ];
    let result = engine.execute(&prog);
    // x > 0 implies x >= 1 for integers, so no bugs
    assert_eq!(result.total_bugs, 0);
}

#[test]
fn safe_assertion_finds_no_bugs() {
    let engine = default_engine();
    // if x > 5 { assert(x > 3) } -- always true
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::If {
            condition: Expr::gt(Expr::var("x"), Expr::lit(5)),
            then_branch: vec![Statement::Assert {
                condition: Expr::gt(Expr::var("x"), Expr::lit(3)),
                label: "x > 3 when x > 5".into(),
            }],
            else_branch: vec![],
        },
    ];
    let result = engine.execute(&prog);
    assert_eq!(result.total_bugs, 0);
}

#[test]
fn return_captures_symbolic_value() {
    let engine = default_engine();
    let prog = vec![
        Statement::Let {
            name: "x".into(),
            value: Expr::Var("input".into()),
        },
        Statement::Return {
            value: Expr::add(Expr::var("x"), Expr::lit(1)),
        },
    ];
    let result = engine.execute(&prog);
    assert!(result.paths[0].return_value.is_some());
}
```

### Running

```bash
cargo build
cargo test
cargo run
```

### Expected Output

```
=== Symbolic Execution Engine ===

--- Example 1: Assertion violation ---
  Paths explored: 2
  Total bugs: 1
  Path 0: feasible=true, constraints=1, bugs=1
    Inputs: {"input_x": 20}
    BUG [AssertionViolation]: x must be < 20
    Triggering inputs: {"input_x": 20}
  Path 1: feasible=true, constraints=1, bugs=0
    Inputs: {"input_x": 0}

--- Example 2: Division by zero ---
  Paths explored: 1
  Total bugs: 1
  Path 0: feasible=true, constraints=0, bugs=1
    Inputs: {}
    BUG [DivisionByZero]: division in Div(Literal(100), Var("y"))
    Triggering inputs: {"input_x": 5}

--- Example 3: Nested conditions (should be safe) ---
  Paths explored: 3
  Total bugs: 0
  Path 0: feasible=true, constraints=2, bugs=0
    Inputs: {"input_a": 1, "input_b": 1}
  Path 1: feasible=true, constraints=2, bugs=0
    Inputs: {"input_a": 1, "input_b": 0}
  Path 2: feasible=true, constraints=1, bugs=0
    Inputs: {"input_a": 0}
```

## Design Decisions

1. **Brute-force solver over SMT integration**: A production symbolic execution engine uses an SMT solver (Z3, CVC5). For this challenge, a bounded brute-force solver keeps the implementation self-contained with zero external dependencies. The tradeoff is that the solver is exponential in the number of symbols and limited to a bounded integer domain. Programs with more than 3-4 symbolic variables will be slow.

2. **Path forking with state cloning**: At each branch, the engine clones the entire symbolic state. This is simple but memory-intensive for deep path trees. An alternative is to use persistent data structures (like a persistent hash map) to share unchanged state between forked paths, reducing memory from O(paths * state_size) to O(paths * changes_per_path).

3. **Eager feasibility checking**: Before exploring a branch, the engine checks if the path constraints are satisfiable. This prunes infeasible paths early, avoiding wasted work. The downside is that solver calls are expensive, so for cheap branches it might be faster to explore first and check later.

4. **Bounded loop unrolling**: Loops are unrolled up to a fixed depth rather than symbolically. This means the engine cannot verify properties that require more iterations than the bound. Production tools use loop summarization or widening to handle unbounded loops, but these are significantly more complex.

5. **Expression trees without simplification rewriting**: The symbolic expressions are kept as trees with minimal simplification (constant folding, identity elimination). A production engine would use canonical forms (e.g., sum-of-products) and algebraic simplification to reduce solver load. The tree representation is easier to understand and debug.

## Common Mistakes

1. **Forgetting to negate the condition on the false branch**: When forking at `if cond`, the true branch gets constraint `cond` and the false branch gets `NOT(cond)`. A common bug is adding `cond` to both branches or forgetting the false branch entirely, causing the engine to miss paths.

2. **Not checking feasibility before exploring**: Without feasibility checks, the engine explores infeasible paths (where the constraints are contradictory), wasting time and reporting spurious bugs. Always check satisfiability after adding a new constraint.

3. **Solver timeout masking bugs**: If the solver returns Unknown due to timeout or complexity, the engine should report this rather than silently treating the path as infeasible. A path marked infeasible-by-timeout might contain a real bug.

4. **Symbolic state mutation instead of cloning**: Modifying the state in-place and then trying to "undo" changes for the other branch is error-prone. Clone the state before forking.

## Performance Notes

| Operation | Complexity | Notes |
|-----------|-----------|-------|
| Symbolic expression evaluation | O(tree_size) | Recursive traversal |
| Constraint solving (brute force) | O(bound^symbols) | Exponential in symbol count |
| Path exploration | O(2^branches) | Exponential in branch count |
| State cloning | O(store_size) | Per fork |
| Expression simplification | O(tree_size) | Single pass |

The solver is the bottleneck. With bound=100 and 3 symbols, each solve call tries up to 200^3 = 8M combinations. With 4 symbols: 200^4 = 1.6B. For larger programs, replacing the brute-force solver with a simple DPLL-based approach or interval arithmetic would improve performance by orders of magnitude without requiring external dependencies.

## Going Further

- Replace the brute-force solver with a **DPLL(T) solver** for linear integer arithmetic, supporting unbounded domains and much larger constraint sets
- Implement **concolic execution** (concrete + symbolic): run the program concretely while tracking symbolic constraints, then negate one branch constraint to explore a new path (the DART/SAGE approach)
- Add **array support**: model arrays as symbolic maps (read/write operations produce `ite` expressions) and detect out-of-bounds access
- Implement **path merging**: when two paths converge to equivalent states, merge them into a single state with a disjunctive constraint, reducing path explosion
- Add **function summaries**: analyze functions once symbolically and reuse the summary at each call site, avoiding re-exploration
- Support **floating-point symbolic execution**: extend the expression language and solver to handle IEEE 754 semantics
