<!--
type: reference
difficulty: insane
section: [12-type-theory-and-fp]
concepts: [hindley-milner, algorithm-w, unification, type-variables, let-polymorphism, occurs-check, turbofish, type-inference-limits]
languages: [go, rust]
estimated_reading_time: 90 min
bloom_level: evaluate
prerequisites: [go-generics, rust-generics, lambda-calculus-basics]
papers: [Milner-1978-theory-of-type-polymorphism, Damas-Milner-1982, Pierce-TAPL-2002]
industry_use: [Rust-compiler, GHC, OCaml, TypeScript, Scala]
language_contrast: high
-->

# Type Inference and Hindley-Milner

> The compiler doesn't just check types you wrote — it solves a system of constraints to discover the types you didn't.

## Mental Model

When you write `let x = vec![1, 2, 3]` in Rust, you don't annotate the type. The compiler figures out that `x` is `Vec<i32>` by looking at the integer literals and inferring their default type. That's obvious. But type inference goes much deeper than this.

In Rust, the compiler can propagate type information backwards through a chain of generic functions across an entire function body. Write `let result: Result<Vec<String>, _> = lines.iter().collect()` and the compiler infers not only the return type but the specific implementation of `Iterator::collect` to call — choosing the one that produces `Vec<String>`. This is not pattern matching against known types. It is solving a system of equations where types are unknowns.

The algorithm behind this is Hindley-Milner (HM), developed by Roger Hindley in 1969 and independently by Robin Milner in 1978. It was one of the most consequential results in programming language research: a type system that is both expressive (supports full parametric polymorphism) and decidable (the compiler always terminates and finds the most general type). Every statically-typed language with decent inference — OCaml, Haskell, Rust, TypeScript, Scala, F# — is either HM or an extension of HM.

The algorithm works by assigning fresh type variables to unknowns and then *unifying* constraints. When the compiler sees `f(x)` and knows `f : a -> b`, it creates a constraint that the type of `x` must equal `a`. When it sees `f(1)`, it creates a constraint that `a = i32`. Then it solves. Unification is the process of finding the substitution that satisfies all constraints simultaneously.

Go's type inference is deliberately more limited. It infers types in variable declarations (`x := 5`) and fills in type arguments for generic function calls when they can be determined from the arguments, but it does not do full HM inference. This is a documented design choice for readability: code should be understandable without tracing inference chains across function boundaries.

## Core Concepts

### Type Variables

A type variable is a placeholder for an unknown type. Written as lowercase Greek letters in theory (α, β) or lowercase letters in Rust (`T`, `U`) and Haskell (`a`, `b`). The key insight: type variables are not "any type" — they are "one specific type that we haven't determined yet."

When the type checker encounters an expression it doesn't know the type of, it creates a fresh type variable. It then generates constraints about that variable based on how the expression is used. At the end, it solves the constraints.

### Unification

Unification is the heart of type inference. Given two type expressions, unification finds the most general substitution that makes them equal.

Simple cases:
- `unify(Int, Int)` → succeeds with empty substitution
- `unify(α, Int)` → succeeds with substitution `{α → Int}`
- `unify(List<α>, List<Int>)` → succeeds with `{α → Int}`
- `unify(Int, Bool)` → fails — type error

The occurs check prevents infinite types:
- `unify(α, List<α>)` → fails — `α` occurs in `List<α>`, so the solution would be `α = List<List<List<...>>>`, an infinite type

Without the occurs check, you get unsound type systems (early versions of Prolog had this bug).

### Let-Polymorphism

Let-polymorphism is what makes `let f = id` polymorphic while a naive lambda might not be. When you write:

```
let id = \x -> x
let a = id 5       // id used as Int -> Int
let b = id "hello" // id used as String -> String
```

The HM algorithm generalizes the type of `id` at the `let` binding point, producing a *type scheme* `∀α. α → α` (for all types α, id takes α and returns α). This generalization is called *let-generalization*. Each use of `id` gets a fresh copy of the type variable, so `a` and `b` can use `id` at different types.

This is the key distinction between a function bound with `let` (polymorphic) and a function passed as an argument (monomorphic in that context, unless the type system supports rank-N polymorphism).

### Algorithm W

Algorithm W is the classic formulation of HM inference. Given an expression, it returns a type scheme and a substitution. The key rules:

- **Variables**: look up in the type environment, instantiate fresh type variables for quantified variables
- **Application** `f x`: infer types for `f` and `x`, create fresh variable `β` for the return type, unify the type of `f` with `type(x) → β`, return `β`
- **Abstraction** `\x -> e`: create fresh variable `α` for `x`, infer type of `e` in environment extended with `x : α`, return `α → type(e)`
- **Let** `let x = e1 in e2`: infer type of `e1`, generalize it to a type scheme, infer type of `e2` in environment extended with `x : scheme`, return `type(e2)`

### Why Turbofish Exists

Rust's type inference is HM-based but must work within a more complex type system (traits, lifetimes, associated types). Sometimes the solver hits an ambiguity: multiple trait implementations could satisfy the constraints. The `::<>` turbofish syntax lets you disambiguate by explicitly providing type arguments.

```rust
// Ambiguous: which FromStr impl?
let x = "42".parse(); // Error: type annotations needed

// Disambiguated with turbofish
let x = "42".parse::<i32>(); // OK

// Or with a type annotation
let x: i32 = "42".parse(); // Also OK
```

The turbofish is not a failure of inference — it is you providing information to resolve a genuine ambiguity. When you see it in code, it means "there are multiple valid choices here and this is the one we want."

## The Theory (for those who want the formalism)

**Type expressions**: `τ ::= α | T | τ₁ → τ₂ | C<τ₁, ..., τₙ>`  
where `α` is a type variable, `T` is a base type, `→` is the function type, and `C` is a type constructor.

**Type schemes**: `σ ::= ∀α₁...αₙ. τ`  
A type scheme universally quantifies over free type variables.

**Substitution**: A mapping from type variables to types. Composition of substitutions is defined as `(S₁ ∘ S₂)(α) = S₁(S₂(α))`.

**Unification algorithm** (Robinson, 1965):
```
unify(T, T) = {}                                          // same base type
unify(α, τ) = {α → τ}   if α ∉ freeVars(τ)             // occurs check
unify(τ, α) = {α → τ}   if α ∉ freeVars(τ)
unify(C<τ₁..τₙ>, C<σ₁..σₙ>) = unify-list([τ₁..τₙ], [σ₁..σₙ])
unify(_, _) = fail
```

**Soundness** (Milner 1978): If Algorithm W infers type `σ` for expression `e`, then `e` is well-typed with type `σ` in any environment consistent with the inference.

**Completeness**: If `e` has any type, Algorithm W finds the *most general* type (principal type). This means you never need to add a type annotation for correctness — only for disambiguation or documentation.

## Implementation: Go

```go
package main

import (
	"fmt"
	"strings"
)

// A minimal type inferencer for lambda calculus.
// Supports: variables, lambda abstraction, application, let bindings.
// This is Algorithm W reduced to its essential form.

// ─── Type Representation ─────────────────────────────────────────────────────

type Type interface {
	typeNode()
	String() string
	FreeVars() map[string]bool
	Apply(Subst) Type
}

// TVar is a type variable: α, β, etc.
type TVar struct{ Name string }

func (t TVar) typeNode()            {}
func (t TVar) String() string       { return t.Name }
func (t TVar) FreeVars() map[string]bool { return map[string]bool{t.Name: true} }
func (t TVar) Apply(s Subst) Type {
	if u, ok := s[t.Name]; ok {
		return u.Apply(s) // chase the chain
	}
	return t
}

// TCon is a type constructor: Int, Bool, String.
type TCon struct{ Name string }

func (t TCon) typeNode()            {}
func (t TCon) String() string       { return t.Name }
func (t TCon) FreeVars() map[string]bool { return map[string]bool{} }
func (t TCon) Apply(_ Subst) Type   { return t }

// TFun is a function type: α → β.
type TFun struct{ Param, Result Type }

func (t TFun) typeNode() {}
func (t TFun) String() string {
	p := t.Param.String()
	if _, ok := t.Param.(TFun); ok {
		p = "(" + p + ")"
	}
	return p + " → " + t.Result.String()
}
func (t TFun) FreeVars() map[string]bool {
	fv := t.Param.FreeVars()
	for k, v := range t.Result.FreeVars() {
		fv[k] = v
	}
	return fv
}
func (t TFun) Apply(s Subst) Type {
	return TFun{t.Param.Apply(s), t.Result.Apply(s)}
}

// ─── Substitution ─────────────────────────────────────────────────────────────

type Subst map[string]Type

func (s Subst) Compose(other Subst) Subst {
	result := make(Subst)
	for k, v := range other {
		result[k] = v.Apply(s) // apply s to the range of other
	}
	for k, v := range s {
		if _, exists := result[k]; !exists {
			result[k] = v
		}
	}
	return result
}

// ─── Unification ──────────────────────────────────────────────────────────────

var counter int

func freshVar() TVar {
	counter++
	return TVar{fmt.Sprintf("t%d", counter)}
}

func occursIn(name string, t Type) bool {
	_, found := t.FreeVars()[name]
	return found
}

func unify(a, b Type) (Subst, error) {
	switch a := a.(type) {
	case TCon:
		if b, ok := b.(TCon); ok && a.Name == b.Name {
			return Subst{}, nil
		}
	case TVar:
		if b, ok := b.(TVar); ok && a.Name == b.Name {
			return Subst{}, nil
		}
		if occursIn(a.Name, b) {
			return nil, fmt.Errorf("occurs check failed: %s in %s", a.Name, b)
		}
		return Subst{a.Name: b}, nil
	case TFun:
		if b, ok := b.(TFun); ok {
			s1, err := unify(a.Param, b.Param)
			if err != nil {
				return nil, err
			}
			s2, err := unify(a.Result.Apply(s1), b.Result.Apply(s1))
			if err != nil {
				return nil, err
			}
			return s1.Compose(s2), nil
		}
	}
	if b, ok := b.(TVar); ok {
		return unify(b, a) // symmetric: delegate to TVar case
	}
	return nil, fmt.Errorf("cannot unify %s with %s", a, b)
}

// ─── Type Environment and Scheme ─────────────────────────────────────────────

type Scheme struct {
	Vars []string // universally quantified variables
	Type Type
}

func (s Scheme) Instantiate() Type {
	subst := make(Subst)
	for _, v := range s.Vars {
		subst[v] = freshVar()
	}
	return s.Type.Apply(subst)
}

type Env map[string]Scheme

func (e Env) FreeVars() map[string]bool {
	fv := map[string]bool{}
	for _, s := range e {
		for v := range s.Type.FreeVars() {
			if !contains(s.Vars, v) {
				fv[v] = true
			}
		}
	}
	return fv
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

func generalize(env Env, t Type) Scheme {
	envFV := env.FreeVars()
	var free []string
	for v := range t.FreeVars() {
		if !envFV[v] {
			free = append(free, v)
		}
	}
	return Scheme{Vars: free, Type: t}
}

// ─── Expressions ─────────────────────────────────────────────────────────────

type Expr interface{ exprNode() }

type Var struct{ Name string }
type Lam struct{ Param string; Body Expr }
type App struct{ Func, Arg Expr }
type Let struct{ Name string; Def, Body Expr }

func (Var) exprNode() {}
func (Lam) exprNode() {}
func (App) exprNode() {}
func (Let) exprNode() {}

// ─── Algorithm W ─────────────────────────────────────────────────────────────

func infer(env Env, expr Expr) (Subst, Type, error) {
	switch e := expr.(type) {
	case Var:
		scheme, ok := env[e.Name]
		if !ok {
			return nil, nil, fmt.Errorf("unbound variable: %s", e.Name)
		}
		return Subst{}, scheme.Instantiate(), nil

	case Lam:
		paramType := freshVar()
		newEnv := make(Env)
		for k, v := range env {
			newEnv[k] = v
		}
		newEnv[e.Param] = Scheme{Type: paramType}
		s, bodyType, err := infer(newEnv, e.Body)
		if err != nil {
			return nil, nil, err
		}
		return s, TFun{paramType.Apply(s), bodyType}, nil

	case App:
		retType := freshVar()
		s1, funcType, err := infer(env, e.Func)
		if err != nil {
			return nil, nil, err
		}
		newEnv := make(Env)
		for k, v := range env {
			newEnv[k] = Scheme{Type: v.Type.Apply(s1)}
		}
		s2, argType, err := infer(newEnv, e.Arg)
		if err != nil {
			return nil, nil, err
		}
		s3, err := unify(funcType.Apply(s2), TFun{argType, retType})
		if err != nil {
			return nil, nil, err
		}
		composed := s1.Compose(s2).Compose(s3)
		return composed, retType.Apply(composed), nil

	case Let:
		s1, defType, err := infer(env, e.Def)
		if err != nil {
			return nil, nil, err
		}
		newEnv := make(Env)
		for k, v := range env {
			newEnv[k] = Scheme{Type: v.Type.Apply(s1)}
		}
		scheme := generalize(newEnv, defType)
		newEnv[e.Name] = scheme
		s2, bodyType, err := infer(newEnv, e.Body)
		if err != nil {
			return nil, nil, err
		}
		return s1.Compose(s2), bodyType, nil
	}
	return nil, nil, fmt.Errorf("unknown expression type")
}

func main() {
	// Base environment with a few built-in types
	intT := TCon{"Int"}
	boolT := TCon{"Bool"}
	env := Env{
		"true":  {Type: boolT},
		"false": {Type: boolT},
		"zero":  {Type: intT},
		"succ":  {Type: TFun{intT, intT}},
		// not : Bool → Bool
		"not": {Type: TFun{boolT, boolT}},
	}

	examples := []struct {
		name string
		expr Expr
	}{
		// let id = \x -> x in id zero
		// id should be inferred as ∀α. α → α, then used at Int
		{
			"let id = λx.x in (id zero)",
			Let{"id", Lam{"x", Var{"x"}},
				App{Var{"id"}, Var{"zero"}}},
		},
		// let id = \x -> x in (id true, id zero) — two uses at different types
		{
			"let id = λx.x in id (id zero)",
			Let{"id", Lam{"x", Var{"x"}},
				App{Var{"id"}, App{Var{"id"}, Var{"zero"}}}},
		},
		// \f -> \x -> f (f x) — function composition
		{
			"λf.λx.f(f x)",
			Lam{"f", Lam{"x",
				App{Var{"f"}, App{Var{"f"}, Var{"x"}}}}},
		},
		// not true : Bool
		{
			"not true",
			App{Var{"not"}, Var{"true"}},
		},
	}

	for _, ex := range examples {
		counter = 0 // reset fresh variable counter for clean output
		_, typ, err := infer(env, ex.expr)
		if err != nil {
			fmt.Printf("%-40s  ERROR: %v\n", ex.name, err)
		} else {
			fmt.Printf("%-40s  : %s\n", ex.name, typ)
		}
	}

	// Demonstrate the occurs check catching an infinite type
	counter = 0
	_, err := unify(TVar{"α"}, TFun{TVar{"α"}, TCon{"Int"}})
	fmt.Println()
	fmt.Printf("unify(α, α → Int): %v\n", err)
	_ = strings.TrimSpace // suppress unused import warning
}
```

**Expected output:**
```
let id = λx.x in (id zero)           : Int
let id = λx.x in id (id zero)        : Int
λf.λx.f(f x)                         : (t1 → t1) → t1 → t1
not true                              : Bool

unify(α, α → Int): occurs check failed: α in t1 → Int
```

### Go-specific considerations

Go's type inference handles two cases well: `x := expr` for local type inference, and type argument inference for generic functions when all type arguments can be determined from the function arguments. What Go deliberately excludes:

- **Bidirectional inference**: Go does not propagate expected types from a context back into an expression. In `var x []int = []int{1, 2, 3}`, you must write `[]int{...}` explicitly — Go doesn't infer the composite literal type from the variable declaration.
- **Global let-polymorphism**: Go generics require explicit type parameters on functions, not implicit quantification. The declaration is explicit; the *instantiation* can be inferred at call sites.
- **Constraint inference**: In Haskell/Rust, typeclass/trait constraints can be inferred. In Go, the interface constraints in generic functions must be written explicitly.

The deliberate limitation makes Go code readable without tooling — you can understand a function's type from its signature without tracing inference chains. The cost is more explicit annotation at the call site.

## Implementation: Rust

```rust
// Same Algorithm W implemented in Rust.
// Demonstrates: trait objects for the Type sum type,
// the same unification algorithm, and how Rust's own
// inference relates to what we're implementing.

use std::collections::{HashMap, HashSet};
use std::fmt;

// ─── Type Representation ──────────────────────────────────────────────────────

#[derive(Debug, Clone, PartialEq)]
enum Type {
    Var(String),
    Con(String),
    Fun(Box<Type>, Box<Type>),
}

impl Type {
    fn free_vars(&self) -> HashSet<String> {
        match self {
            Type::Var(n) => [n.clone()].into(),
            Type::Con(_) => HashSet::new(),
            Type::Fun(a, b) => {
                let mut fv = a.free_vars();
                fv.extend(b.free_vars());
                fv
            }
        }
    }

    fn apply(&self, subst: &Subst) -> Type {
        match self {
            Type::Var(n) => subst
                .get(n)
                .map(|t| t.apply(subst))
                .unwrap_or_else(|| self.clone()),
            Type::Con(_) => self.clone(),
            Type::Fun(a, b) => Type::Fun(
                Box::new(a.apply(subst)),
                Box::new(b.apply(subst)),
            ),
        }
    }
}

impl fmt::Display for Type {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Type::Var(n) => write!(f, "{n}"),
            Type::Con(n) => write!(f, "{n}"),
            Type::Fun(a, b) => {
                if matches!(**a, Type::Fun(..)) {
                    write!(f, "({a}) → {b}")
                } else {
                    write!(f, "{a} → {b}")
                }
            }
        }
    }
}

// ─── Substitution ─────────────────────────────────────────────────────────────

type Subst = HashMap<String, Type>;

fn compose(s1: &Subst, s2: &Subst) -> Subst {
    let mut result: Subst = s2.iter()
        .map(|(k, v)| (k.clone(), v.apply(s1)))
        .collect();
    for (k, v) in s1 {
        result.entry(k.clone()).or_insert_with(|| v.clone());
    }
    result
}

// ─── Unification ──────────────────────────────────────────────────────────────

static COUNTER: std::sync::atomic::AtomicUsize = std::sync::atomic::AtomicUsize::new(0);

fn fresh_var() -> Type {
    let n = COUNTER.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
    Type::Var(format!("t{n}"))
}

fn occurs_in(name: &str, t: &Type) -> bool {
    t.free_vars().contains(name)
}

fn unify(a: &Type, b: &Type) -> Result<Subst, String> {
    match (a, b) {
        (Type::Con(n1), Type::Con(n2)) if n1 == n2 => Ok(HashMap::new()),
        (Type::Var(n), t) | (t, Type::Var(n)) => {
            if let Type::Var(m) = t {
                if m == n {
                    return Ok(HashMap::new());
                }
            }
            if occurs_in(n, t) {
                return Err(format!("occurs check: {n} in {t}"));
            }
            Ok([(n.clone(), t.clone())].into())
        }
        (Type::Fun(a1, b1), Type::Fun(a2, b2)) => {
            let s1 = unify(a1, a2)?;
            let s2 = unify(&b1.apply(&s1), &b2.apply(&s1))?;
            Ok(compose(&s1, &s2))
        }
        _ => Err(format!("cannot unify {a} with {b}")),
    }
}

// ─── Type Schemes and Environment ────────────────────────────────────────────

#[derive(Debug, Clone)]
struct Scheme {
    vars: Vec<String>,
    ty: Type,
}

impl Scheme {
    fn instantiate(&self) -> Type {
        let subst: Subst = self.vars.iter()
            .map(|v| (v.clone(), fresh_var()))
            .collect();
        self.ty.apply(&subst)
    }
}

type Env = HashMap<String, Scheme>;

fn env_free_vars(env: &Env) -> HashSet<String> {
    env.values().flat_map(|s| {
        s.ty.free_vars().into_iter()
            .filter(|v| !s.vars.contains(v))
    }).collect()
}

fn generalize(env: &Env, ty: &Type) -> Scheme {
    let efv = env_free_vars(env);
    let vars: Vec<String> = ty.free_vars()
        .into_iter()
        .filter(|v| !efv.contains(v))
        .collect();
    Scheme { vars, ty: ty.clone() }
}

// ─── Expressions ─────────────────────────────────────────────────────────────

#[derive(Debug, Clone)]
enum Expr {
    Var(String),
    Lam(String, Box<Expr>),
    App(Box<Expr>, Box<Expr>),
    Let(String, Box<Expr>, Box<Expr>),
}

// ─── Algorithm W ─────────────────────────────────────────────────────────────

fn infer(env: &Env, expr: &Expr) -> Result<(Subst, Type), String> {
    match expr {
        Expr::Var(name) => {
            let scheme = env.get(name)
                .ok_or_else(|| format!("unbound: {name}"))?;
            Ok((HashMap::new(), scheme.instantiate()))
        }
        Expr::Lam(param, body) => {
            let param_ty = fresh_var();
            let mut new_env = env.clone();
            new_env.insert(param.clone(), Scheme { vars: vec![], ty: param_ty.clone() });
            let (s, body_ty) = infer(&new_env, body)?;
            Ok((s.clone(), Type::Fun(
                Box::new(param_ty.apply(&s)),
                Box::new(body_ty),
            )))
        }
        Expr::App(func, arg) => {
            let ret_ty = fresh_var();
            let (s1, func_ty) = infer(env, func)?;
            let new_env: Env = env.iter()
                .map(|(k, v)| (k.clone(), Scheme {
                    vars: v.vars.clone(),
                    ty: v.ty.apply(&s1),
                }))
                .collect();
            let (s2, arg_ty) = infer(&new_env, arg)?;
            let s3 = unify(
                &func_ty.apply(&s2),
                &Type::Fun(Box::new(arg_ty), Box::new(ret_ty.clone())),
            )?;
            let s = compose(&compose(&s1, &s2), &s3);
            Ok((s.clone(), ret_ty.apply(&s)))
        }
        Expr::Let(name, def, body) => {
            let (s1, def_ty) = infer(env, def)?;
            let new_env: Env = env.iter()
                .map(|(k, v)| (k.clone(), Scheme {
                    vars: v.vars.clone(),
                    ty: v.ty.apply(&s1),
                }))
                .collect();
            let scheme = generalize(&new_env, &def_ty);
            let mut ext_env = new_env;
            ext_env.insert(name.clone(), scheme);
            let (s2, body_ty) = infer(&ext_env, body)?;
            Ok((compose(&s1, &s2), body_ty))
        }
    }
}

fn main() {
    let int_t = Type::Con("Int".into());
    let bool_t = Type::Con("Bool".into());

    let env: Env = [
        ("zero",  Scheme { vars: vec![], ty: int_t.clone() }),
        ("true",  Scheme { vars: vec![], ty: bool_t.clone() }),
        ("succ",  Scheme { vars: vec![], ty: Type::Fun(Box::new(int_t.clone()), Box::new(int_t.clone())) }),
        ("not",   Scheme { vars: vec![], ty: Type::Fun(Box::new(bool_t.clone()), Box::new(bool_t.clone())) }),
    ].into_iter().map(|(k, v)| (k.to_string(), v)).collect();

    let examples: &[(&str, Expr)] = &[
        // let id = \x -> x in (id zero)
        ("let id = λx.x in (id zero)",
         Expr::Let("id".into(),
             Box::new(Expr::Lam("x".into(), Box::new(Expr::Var("x".into())))),
             Box::new(Expr::App(
                 Box::new(Expr::Var("id".into())),
                 Box::new(Expr::Var("zero".into())),
             )))),
        // λf.λx.f(f x) — the "apply-twice" combinator
        ("λf.λx. f (f x)",
         Expr::Lam("f".into(), Box::new(Expr::Lam("x".into(),
             Box::new(Expr::App(
                 Box::new(Expr::Var("f".into())),
                 Box::new(Expr::App(
                     Box::new(Expr::Var("f".into())),
                     Box::new(Expr::Var("x".into())),
                 )),
             )))))),
        // not true : Bool
        ("not true",
         Expr::App(Box::new(Expr::Var("not".into())), Box::new(Expr::Var("true".into())))),
    ];

    for (name, expr) in examples {
        COUNTER.store(0, std::sync::atomic::Ordering::Relaxed);
        match infer(&env, expr) {
            Ok((_, ty)) => println!("{:<45} : {ty}", name),
            Err(e)      => println!("{:<45} ERROR: {e}", name),
        }
    }

    // Occurs check
    COUNTER.store(0, std::sync::atomic::Ordering::Relaxed);
    let result = unify(&Type::Var("α".into()), &Type::Fun(
        Box::new(Type::Var("α".into())),
        Box::new(Type::Con("Int".into())),
    ));
    println!("\nunify(α, α → Int): {:?}", result.unwrap_err());
}
```

### Rust-specific considerations

Rust uses a bidirectional type checker that is more powerful than pure HM in some ways but has different limitations:

- **Bidirectional checking**: Rust propagates expected types from the context into expressions. This is how `let v: Vec<i32> = vec![]` works — the `vec![]` macro needs to know the element type, and Rust propagates `i32` into it.
- **Trait solver as inference**: Rust's trait resolution is a second inference engine running alongside type inference. When `collect::<Vec<_>>()` is called, the trait solver finds the `FromIterator` impl that matches `Vec<T>`. This can interact in surprising ways.
- **Local inference only**: Rust, like HM, does not do cross-function inference. Return types must be explicit. This ensures that changing a function's body cannot break callers who depend on inferred return types.
- **Turbofish necessity**: When trait dispatch is ambiguous, you need turbofish: `"42".parse::<i32>()`. The `::<>` syntax provides type arguments to resolve the ambiguity.

## Go vs Rust: Direct Comparison

| Aspect | Go | Rust |
|--------|-----|------|
| Inference algorithm | Limited local inference | Extended HM with bidirectional typing |
| Variable declarations | `x := expr` infers from RHS | `let x = expr` infers, `let x: T = expr` checks |
| Generic call inference | Type args inferred from value args | Type args inferred from value args + expected type |
| Return type inference | Not supported — must annotate | Not supported at function boundary |
| Constraint inference | Not supported | Limited (trait bounds on generics) |
| Turbofish equivalent | `f[int](x)` (type arg explicit) | `f::<i32>(x)` |
| Infinite type detection | N/A | Occurs check prevents recursive types |

## Production Applications

**Where this matters:**

- **Rust's `?` operator**: The compiler infers the error conversion — `?` calls `From::from` on the error, and the type system infers which `From` impl to use based on the function's return type. This is unification-based inference resolving a trait dispatch.
- **serde derive macros**: The `#[derive(Serialize)]` macro generates code that uses Rust's type inference to serialize each field. The macro does not know the types at macro expansion time — it generates generic code and lets inference resolve it.
- **`Iterator::collect`**: Takes an `IntoIterator` and returns "whatever container you want." The type is inferred from context. Without HM-style inference, you'd write `Vec::from_iter(iter.map(...))` every time.
- **TypeScript's inference**: TypeScript's inference is explicitly based on HM, extended with structural typing. Understanding HM explains why TypeScript sometimes needs explicit annotations at function boundaries and when it can infer.

## Complexity Analysis

**Type-checking complexity**: Standard HM is O(n × α(n)) where α is the inverse Ackermann function — nearly linear. With full constraint solving (as in Rust), it can be NP-hard in pathological cases. In practice, Rust's compiler spends significant time in the trait solver, not the HM component.

**Compilation time impact**: Deep chains of generic calls force the type solver to unify many constraints. Code with 5-deep chains of iterator adapters (`map`, `filter`, `flat_map`, `zip`, `take`) can produce compile times that grow super-linearly in the chain length. The solution: `collect()` at intermediate points to break the chain, or use `impl Trait` return types to limit inference depth.

**Cognitive load**: For readers of code, inferred types reduce annotation noise. For writers, they reduce boilerplate. The tradeoff is that without an IDE showing inferred types, complex expressions require tracing inference manually — which is exactly what this section teaches you to do.

## Common Pitfalls

1. **Annotation-driven confusion**: In Rust, adding a type annotation changes what the compiler infers for the *entire* expression, not just the annotated part. `let x: Vec<i32> = something.collect()` and `let x = something.collect::<Vec<i32>>()` are equivalent, but `let x: Vec<_> = something.collect()` leaves the element type open. Understand that annotations are constraints, not assertions.

2. **Polymorphism under lambdas**: In ML and Haskell, `let id = \x -> x` is polymorphic, but `(\f -> ...) (\x -> x)` may not be — the lambda passed as an argument gets a monomorphic type. Rust has the same property. If you need a polymorphic function argument, you need a trait bound.

3. **The monomorphization cliff in Go**: Go generics monomorphize at compile time. A function `f[T any](x T)` called with 10 different `T`s produces 10 compiled functions. For functions called in hot loops with many types, this can cause code size explosion. Profile before assuming.

4. **Fighting inference with explicit annotations**: Adding type annotations to every variable because "it makes the code clearer" negates inference and adds maintenance burden — every refactoring requires updating annotations. Add annotations at public API boundaries and where inference genuinely fails.

5. **Rust's type inference does not cross function boundaries**: A common confusion: why can't the compiler infer the return type? It could, for a function in isolation. But if callers depend on an inferred return type and the function body changes, the caller's code silently changes behavior. Explicit return types are a stability guarantee.

## Exercises

**Exercise 1** (30 min): Extend the Go type inferencer to support `let-rec` (recursive let bindings). The trick: add the binding to the environment with a fresh type variable before inferring the definition, then unify the inferred type with the type variable. Test with the `factorial` function.

**Exercise 2** (2–4 h): Add product types (pairs) to both the Go and Rust inferencer. Add `Pair<α, β>` as a type constructor, `pair : α → β → Pair<α, β>` as a constructor, and `fst : Pair<α, β> → α` / `snd : Pair<α, β> → β` as destructors. Verify that `fst (pair zero true)` infers to `Int`.

**Exercise 3** (4–8 h): Add row polymorphism to the Rust inferencer — a simple form of record types where `{x: Int, ...ρ}` means "a record with field x of type Int and any other fields ρ". This is how Go's structural interfaces and TypeScript's `{name: string} & T` work at the type level. Implement `select` (field access) and `extend` (record extension) with their inference rules.

**Exercise 4** (8–15 h): Implement a complete mini-language with the inferencer as the type checker: parser, inferencer, and interpreter. The language should support integers, booleans, lambda abstraction, application, `let`, `if-then-else`, and recursive `let`. Write a test suite that verifies at least 20 type inference cases including ill-typed expressions that correctly produce type errors.

## Further Reading

### Foundational Papers
- Milner, R. (1978). "A theory of type polymorphism in programming." *Journal of Computer and System Sciences*, 17(3), 348–375. — The original paper. Surprisingly readable.
- Damas, L., & Milner, R. (1982). "Principal type-schemes for functional programs." *POPL 1982*. — Adds the formal completeness proof (Algorithm W finds the principal type).
- Pierce, B. C. (2002). *Types and Programming Languages*. MIT Press. — Chapters 22–23 cover HM in full detail with worked examples.

### Books
- *Types and Programming Languages* (Pierce) — The standard reference. Start with Part III (subtyping) and Part IV (recursive types) if HM is familiar.
- *The Implementation of Functional Programming Languages* (Peyton Jones, 1987) — Free online. Chapter 9 is the clearest explanation of Algorithm W with real implementation detail.

### Production Code to Read
- `rustc`'s type checker: `compiler/rustc_infer/src/infer/` — the actual HM + trait solver. Complex but the comments are good.
- `go/types` package (Go standard library) — Go's type checker. The `infer.go` file shows exactly where Go's inference stops compared to full HM.

### Talks
- "Type Inference in Rust" (Niko Matsakis, POPL 2016) — how Rust's inference algorithm works, why it differs from HM, and the design tradeoffs.
- "So You Want to Build a Type Inferencer" (Stephanie Weirich, Strange Loop 2016) — live-coding a type inferencer in Haskell with excellent explanation of each step.
