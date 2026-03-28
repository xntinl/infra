# 24. Type Checker for Simple Language -- Solution

## Architecture Overview

The type checker operates in three phases over an AST (reusing concepts from challenge 23):

1. **AST definition** -- extended with type annotations on declarations
2. **Type representation** -- an enum capturing all types in the language
3. **Type checker** -- walks the AST, maintaining a scope chain of symbol tables, and verifies or infers types

```
Source --> Lexer --> Parser --> AST (with type annotations)
                                  |
                                  v
                            Type Checker
                              |     |
                     scope chain   error list
                              |
                              v
                      Typed AST (every expr has a resolved type)
```

### Language Syntax

```
program     = declaration*
declaration = struct_decl | fun_decl | var_decl | statement
struct_decl = "struct" IDENT "{" (IDENT ":" type ",")* "}"
fun_decl    = "fn" IDENT "(" typed_params ")" (":" type)? block
var_decl    = "let" IDENT (":" type)? "=" expression ";"
type        = "int" | "float" | "bool" | "string" | "void"
            | IDENT                          -- struct name
            | IDENT "<" type ">"             -- generic
            | "(" type_list ")" "->" type    -- function type
```

## Complete Solution (Rust)

### Cargo.toml

```toml
[package]
name = "type-checker"
version = "0.1.0"
edition = "2021"
```

### src/lib.rs

```rust
pub mod types;
pub mod ast;
pub mod checker;

pub use types::Type;
pub use checker::{TypeChecker, TypeError};
```

### src/types.rs

```rust
use std::fmt;

#[derive(Debug, Clone, PartialEq)]
pub enum Type {
    Int,
    Float,
    Bool,
    String,
    Void,
    Function {
        params: Vec<Type>,
        ret: Box<Type>,
    },
    Struct {
        name: String,
        fields: Vec<(String, Type)>,
    },
    Generic {
        name: String,
        type_arg: Box<Type>,
    },
    TypeParam(String),
    Unknown,
}

impl Type {
    pub fn is_numeric(&self) -> bool {
        matches!(self, Type::Int | Type::Float)
    }

    pub fn is_compatible_with(&self, other: &Type) -> bool {
        if self == other {
            return true;
        }
        // int -> float implicit widening
        if *self == Type::Int && *other == Type::Float {
            return true;
        }
        if *self == Type::Float && *other == Type::Int {
            return true;
        }
        false
    }

    pub fn arithmetic_result(left: &Type, right: &Type) -> Option<Type> {
        match (left, right) {
            (Type::Int, Type::Int) => Some(Type::Int),
            (Type::Float, Type::Float) => Some(Type::Float),
            (Type::Int, Type::Float) | (Type::Float, Type::Int) => Some(Type::Float),
            _ => None,
        }
    }

    pub fn substitute_type_param(&self, param_name: &str, concrete: &Type) -> Type {
        match self {
            Type::TypeParam(name) if name == param_name => concrete.clone(),
            Type::Generic { name, type_arg } => Type::Generic {
                name: name.clone(),
                type_arg: Box::new(type_arg.substitute_type_param(param_name, concrete)),
            },
            Type::Function { params, ret } => Type::Function {
                params: params.iter().map(|p| p.substitute_type_param(param_name, concrete)).collect(),
                ret: Box::new(ret.substitute_type_param(param_name, concrete)),
            },
            other => other.clone(),
        }
    }
}

impl fmt::Display for Type {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Type::Int => write!(f, "int"),
            Type::Float => write!(f, "float"),
            Type::Bool => write!(f, "bool"),
            Type::String => write!(f, "string"),
            Type::Void => write!(f, "void"),
            Type::Function { params, ret } => {
                let ps: Vec<String> = params.iter().map(|p| p.to_string()).collect();
                write!(f, "({}) -> {}", ps.join(", "), ret)
            }
            Type::Struct { name, .. } => write!(f, "{}", name),
            Type::Generic { name, type_arg } => write!(f, "{}<{}>", name, type_arg),
            Type::TypeParam(name) => write!(f, "{}", name),
            Type::Unknown => write!(f, "unknown"),
        }
    }
}
```

### src/ast.rs

```rust
use crate::types::Type;

#[derive(Debug, Clone, Copy)]
pub struct Span {
    pub line: usize,
    pub col: usize,
}

impl std::fmt::Display for Span {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}:{}", self.line, self.col)
    }
}

#[derive(Debug)]
pub enum Decl {
    Var {
        span: Span,
        name: String,
        type_ann: Option<Type>,
        init: Expr,
    },
    Fun {
        span: Span,
        name: String,
        params: Vec<(String, Type)>,
        return_type: Option<Type>,
        body: Vec<Stmt>,
    },
    Struct {
        span: Span,
        name: String,
        type_params: Vec<String>,
        fields: Vec<(String, Type)>,
    },
}

#[derive(Debug)]
pub enum Stmt {
    Decl(Decl),
    Expr { span: Span, expr: Expr },
    Print { span: Span, expr: Expr },
    If {
        span: Span,
        condition: Expr,
        then_body: Vec<Stmt>,
        else_body: Option<Vec<Stmt>>,
    },
    While {
        span: Span,
        condition: Expr,
        body: Vec<Stmt>,
    },
    Return { span: Span, value: Option<Expr> },
    Block { span: Span, stmts: Vec<Stmt> },
}

#[derive(Debug)]
pub enum Expr {
    IntLit { span: Span, value: i64 },
    FloatLit { span: Span, value: f64 },
    BoolLit { span: Span, value: bool },
    StringLit { span: Span, value: String },
    Ident { span: Span, name: String },
    Binary {
        span: Span,
        left: Box<Expr>,
        op: BinOp,
        right: Box<Expr>,
    },
    Unary {
        span: Span,
        op: UnaryOp,
        expr: Box<Expr>,
    },
    Call {
        span: Span,
        callee: Box<Expr>,
        args: Vec<Expr>,
    },
    FieldAccess {
        span: Span,
        object: Box<Expr>,
        field: String,
    },
    StructLit {
        span: Span,
        name: String,
        type_arg: Option<Type>,
        fields: Vec<(String, Expr)>,
    },
    ArrayLit {
        span: Span,
        elem_type: Type,
        elements: Vec<Expr>,
    },
    Index {
        span: Span,
        object: Box<Expr>,
        index: Box<Expr>,
    },
    Assign {
        span: Span,
        name: String,
        value: Box<Expr>,
    },
}

impl Expr {
    pub fn span(&self) -> Span {
        match self {
            Expr::IntLit { span, .. } | Expr::FloatLit { span, .. } |
            Expr::BoolLit { span, .. } | Expr::StringLit { span, .. } |
            Expr::Ident { span, .. } | Expr::Binary { span, .. } |
            Expr::Unary { span, .. } | Expr::Call { span, .. } |
            Expr::FieldAccess { span, .. } | Expr::StructLit { span, .. } |
            Expr::ArrayLit { span, .. } | Expr::Index { span, .. } |
            Expr::Assign { span, .. } => *span,
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub enum BinOp {
    Add, Sub, Mul, Div,
    Eq, Neq, Lt, Gt, LtEq, GtEq,
    And, Or,
}

impl std::fmt::Display for BinOp {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            BinOp::Add => write!(f, "+"), BinOp::Sub => write!(f, "-"),
            BinOp::Mul => write!(f, "*"), BinOp::Div => write!(f, "/"),
            BinOp::Eq => write!(f, "=="), BinOp::Neq => write!(f, "!="),
            BinOp::Lt => write!(f, "<"), BinOp::Gt => write!(f, ">"),
            BinOp::LtEq => write!(f, "<="), BinOp::GtEq => write!(f, ">="),
            BinOp::And => write!(f, "and"), BinOp::Or => write!(f, "or"),
        }
    }
}

#[derive(Debug, Clone, Copy)]
pub enum UnaryOp {
    Neg, Not,
}
```

### src/checker.rs

```rust
use std::collections::HashMap;
use crate::ast::*;
use crate::types::Type;

#[derive(Debug, Clone)]
pub struct TypeError {
    pub message: String,
    pub span: Span,
}

impl std::fmt::Display for TypeError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "Type error at {}: {}", self.span, self.message)
    }
}

struct Scope {
    symbols: HashMap<String, Type>,
    parent: Option<Box<Scope>>,
}

impl Scope {
    fn new(parent: Option<Box<Scope>>) -> Self {
        Scope { symbols: HashMap::new(), parent }
    }

    fn define(&mut self, name: String, ty: Type) {
        self.symbols.insert(name, ty);
    }

    fn lookup(&self, name: &str) -> Option<&Type> {
        self.symbols.get(name)
            .or_else(|| self.parent.as_ref().and_then(|p| p.lookup(name)))
    }
}

pub struct TypeChecker {
    scope: Scope,
    errors: Vec<TypeError>,
    struct_defs: HashMap<String, Vec<(String, Type)>>,
    struct_type_params: HashMap<String, Vec<String>>,
    current_return_type: Option<Type>,
}

impl TypeChecker {
    pub fn new() -> Self {
        TypeChecker {
            scope: Scope::new(None),
            errors: Vec::new(),
            struct_defs: HashMap::new(),
            struct_type_params: HashMap::new(),
            current_return_type: None,
        }
    }

    pub fn check_program(&mut self, stmts: &[Stmt]) -> Vec<TypeError> {
        // First pass: register all struct and function declarations
        for stmt in stmts {
            if let Stmt::Decl(decl) = stmt {
                self.register_decl(decl);
            }
        }
        // Second pass: type-check everything
        for stmt in stmts {
            self.check_stmt(stmt);
        }
        self.errors.clone()
    }

    fn register_decl(&mut self, decl: &Decl) {
        match decl {
            Decl::Struct { name, fields, type_params, .. } => {
                self.struct_defs.insert(name.clone(), fields.clone());
                self.struct_type_params.insert(name.clone(), type_params.clone());
            }
            Decl::Fun { name, params, return_type, .. } => {
                let param_types: Vec<Type> = params.iter().map(|(_, t)| t.clone()).collect();
                let ret = return_type.clone().unwrap_or(Type::Void);
                self.scope.define(name.clone(), Type::Function {
                    params: param_types,
                    ret: Box::new(ret),
                });
            }
            _ => {}
        }
    }

    fn push_scope(&mut self) {
        let old = std::mem::replace(&mut self.scope, Scope::new(None));
        self.scope = Scope::new(Some(Box::new(old)));
    }

    fn pop_scope(&mut self) {
        if let Some(parent) = self.scope.parent.take() {
            self.scope = *parent;
        }
    }

    fn error(&mut self, span: Span, msg: impl Into<String>) {
        self.errors.push(TypeError { message: msg.into(), span });
    }

    fn check_stmt(&mut self, stmt: &Stmt) {
        match stmt {
            Stmt::Decl(decl) => self.check_decl(decl),
            Stmt::Expr { expr, .. } => { self.synth_expr(expr); }
            Stmt::Print { expr, .. } => { self.synth_expr(expr); }
            Stmt::If { condition, then_body, else_body, span } => {
                let cond_ty = self.synth_expr(condition);
                if cond_ty != Type::Bool && cond_ty != Type::Unknown {
                    self.error(*span, format!(
                        "Condition must be bool, found {}", cond_ty
                    ));
                }
                self.push_scope();
                for s in then_body { self.check_stmt(s); }
                self.pop_scope();
                if let Some(els) = else_body {
                    self.push_scope();
                    for s in els { self.check_stmt(s); }
                    self.pop_scope();
                }
            }
            Stmt::While { condition, body, span } => {
                let cond_ty = self.synth_expr(condition);
                if cond_ty != Type::Bool && cond_ty != Type::Unknown {
                    self.error(*span, format!(
                        "While condition must be bool, found {}", cond_ty
                    ));
                }
                self.push_scope();
                for s in body { self.check_stmt(s); }
                self.pop_scope();
            }
            Stmt::Return { value, span } => {
                let ret_ty = match value {
                    Some(expr) => self.synth_expr(expr),
                    None => Type::Void,
                };
                if let Some(expected) = &self.current_return_type {
                    if !ret_ty.is_compatible_with(expected) && ret_ty != Type::Unknown {
                        self.error(*span, format!(
                            "Return type mismatch: expected {}, found {}", expected, ret_ty
                        ));
                    }
                }
            }
            Stmt::Block { stmts, .. } => {
                self.push_scope();
                for s in stmts { self.check_stmt(s); }
                self.pop_scope();
            }
        }
    }

    fn check_decl(&mut self, decl: &Decl) {
        match decl {
            Decl::Var { span, name, type_ann, init } => {
                let init_type = self.synth_expr(init);
                let var_type = if let Some(ann) = type_ann {
                    if init_type != Type::Unknown && !init_type.is_compatible_with(ann) {
                        self.error(*span, format!(
                            "Cannot assign {} to variable '{}' of type {}",
                            init_type, name, ann
                        ));
                    }
                    ann.clone()
                } else {
                    // Type inference: use the initializer's type
                    if init_type == Type::Unknown {
                        self.error(*span, format!(
                            "Cannot infer type for '{}': initializer has unknown type", name
                        ));
                    }
                    init_type
                };
                self.scope.define(name.clone(), var_type);
            }
            Decl::Fun { span, name, params, return_type, body } => {
                let ret = return_type.clone().unwrap_or(Type::Void);
                let param_types: Vec<Type> = params.iter().map(|(_, t)| t.clone()).collect();
                self.scope.define(name.clone(), Type::Function {
                    params: param_types,
                    ret: Box::new(ret.clone()),
                });

                let prev_return = self.current_return_type.take();
                self.current_return_type = Some(ret.clone());

                self.push_scope();
                for (pname, ptype) in params {
                    self.scope.define(pname.clone(), ptype.clone());
                }
                for s in body {
                    self.check_stmt(s);
                }
                self.pop_scope();

                self.current_return_type = prev_return;
                let _ = span; // span used for error reporting if needed
            }
            Decl::Struct { .. } => {
                // Already registered in first pass
            }
        }
    }

    fn synth_expr(&mut self, expr: &Expr) -> Type {
        match expr {
            Expr::IntLit { .. } => Type::Int,
            Expr::FloatLit { .. } => Type::Float,
            Expr::BoolLit { .. } => Type::Bool,
            Expr::StringLit { .. } => Type::String,

            Expr::Ident { span, name } => {
                match self.scope.lookup(name) {
                    Some(ty) => ty.clone(),
                    None => {
                        self.error(*span, format!("Undefined variable '{}'", name));
                        Type::Unknown
                    }
                }
            }

            Expr::Assign { span, name, value } => {
                let val_type = self.synth_expr(value);
                match self.scope.lookup(name) {
                    Some(var_type) => {
                        let var_type = var_type.clone();
                        if !val_type.is_compatible_with(&var_type) && val_type != Type::Unknown {
                            self.error(*span, format!(
                                "Cannot assign {} to variable '{}' of type {}",
                                val_type, name, var_type
                            ));
                        }
                        var_type
                    }
                    None => {
                        self.error(*span, format!("Undefined variable '{}'", name));
                        Type::Unknown
                    }
                }
            }

            Expr::Binary { span, left, op, right } => {
                let left_ty = self.synth_expr(left);
                let right_ty = self.synth_expr(right);

                match op {
                    BinOp::Add | BinOp::Sub | BinOp::Mul | BinOp::Div => {
                        // String concatenation
                        if *op == BinOp::Add && left_ty == Type::String && right_ty == Type::String {
                            return Type::String;
                        }
                        match Type::arithmetic_result(&left_ty, &right_ty) {
                            Some(t) => t,
                            None => {
                                if left_ty != Type::Unknown && right_ty != Type::Unknown {
                                    self.error(*span, format!(
                                        "Cannot apply '{}' to {} and {}", op, left_ty, right_ty
                                    ));
                                }
                                Type::Unknown
                            }
                        }
                    }
                    BinOp::Lt | BinOp::Gt | BinOp::LtEq | BinOp::GtEq => {
                        if !left_ty.is_numeric() || !right_ty.is_numeric() {
                            if left_ty != Type::Unknown && right_ty != Type::Unknown {
                                self.error(*span, format!(
                                    "Cannot compare {} and {} with '{}'", left_ty, right_ty, op
                                ));
                            }
                        }
                        Type::Bool
                    }
                    BinOp::Eq | BinOp::Neq => {
                        if !left_ty.is_compatible_with(&right_ty) {
                            if left_ty != Type::Unknown && right_ty != Type::Unknown {
                                self.error(*span, format!(
                                    "Cannot compare {} and {} for equality", left_ty, right_ty
                                ));
                            }
                        }
                        Type::Bool
                    }
                    BinOp::And | BinOp::Or => {
                        if left_ty != Type::Bool && left_ty != Type::Unknown {
                            self.error(left.span(), format!(
                                "Expected bool for '{}', found {}", op, left_ty
                            ));
                        }
                        if right_ty != Type::Bool && right_ty != Type::Unknown {
                            self.error(right.span(), format!(
                                "Expected bool for '{}', found {}", op, right_ty
                            ));
                        }
                        Type::Bool
                    }
                }
            }

            Expr::Unary { span, op, expr } => {
                let inner = self.synth_expr(expr);
                match op {
                    UnaryOp::Neg => {
                        if !inner.is_numeric() && inner != Type::Unknown {
                            self.error(*span, format!("Cannot negate {}", inner));
                        }
                        inner
                    }
                    UnaryOp::Not => {
                        if inner != Type::Bool && inner != Type::Unknown {
                            self.error(*span, format!("Cannot apply 'not' to {}", inner));
                        }
                        Type::Bool
                    }
                }
            }

            Expr::Call { span, callee, args } => {
                let callee_ty = self.synth_expr(callee);
                match callee_ty {
                    Type::Function { params, ret } => {
                        if args.len() != params.len() {
                            self.error(*span, format!(
                                "Expected {} argument(s), found {}", params.len(), args.len()
                            ));
                        }
                        for (i, (arg, param_ty)) in args.iter().zip(params.iter()).enumerate() {
                            let arg_ty = self.synth_expr(arg);
                            if !arg_ty.is_compatible_with(param_ty) && arg_ty != Type::Unknown {
                                self.error(arg.span(), format!(
                                    "Argument {} type mismatch: expected {}, found {}",
                                    i + 1, param_ty, arg_ty
                                ));
                            }
                        }
                        *ret
                    }
                    Type::Unknown => Type::Unknown,
                    other => {
                        self.error(*span, format!("Cannot call non-function type {}", other));
                        Type::Unknown
                    }
                }
            }

            Expr::FieldAccess { span, object, field } => {
                let obj_ty = self.synth_expr(object);
                match &obj_ty {
                    Type::Struct { name, fields } => {
                        match fields.iter().find(|(f, _)| f == field) {
                            Some((_, ty)) => ty.clone(),
                            None => {
                                self.error(*span, format!(
                                    "Struct '{}' has no field '{}'", name, field
                                ));
                                Type::Unknown
                            }
                        }
                    }
                    Type::Unknown => Type::Unknown,
                    other => {
                        self.error(*span, format!(
                            "Cannot access field '{}' on type {}", field, other
                        ));
                        Type::Unknown
                    }
                }
            }

            Expr::StructLit { span, name, type_arg, fields } => {
                let struct_fields = match self.struct_defs.get(name) {
                    Some(f) => f.clone(),
                    None => {
                        self.error(*span, format!("Undefined struct '{}'", name));
                        return Type::Unknown;
                    }
                };
                let type_params = self.struct_type_params.get(name).cloned().unwrap_or_default();

                let resolved_fields = if let (Some(ta), Some(param)) = (type_arg, type_params.first()) {
                    struct_fields.iter().map(|(n, t)| {
                        (n.clone(), t.substitute_type_param(param, ta))
                    }).collect::<Vec<_>>()
                } else {
                    struct_fields.clone()
                };

                for (fname, fexpr) in fields {
                    match resolved_fields.iter().find(|(n, _)| n == fname) {
                        Some((_, expected_ty)) => {
                            let actual_ty = self.synth_expr(fexpr);
                            if !actual_ty.is_compatible_with(expected_ty) && actual_ty != Type::Unknown {
                                self.error(fexpr.span(), format!(
                                    "Field '{}' expects {}, found {}", fname, expected_ty, actual_ty
                                ));
                            }
                        }
                        None => {
                            self.error(fexpr.span(), format!(
                                "Struct '{}' has no field '{}'", name, fname
                            ));
                        }
                    }
                }

                Type::Struct { name: name.clone(), fields: resolved_fields }
            }

            Expr::ArrayLit { span, elem_type, elements } => {
                for elem in elements {
                    let ty = self.synth_expr(elem);
                    if !ty.is_compatible_with(elem_type) && ty != Type::Unknown {
                        self.error(elem.span(), format!(
                            "Array element type mismatch: expected {}, found {}",
                            elem_type, ty
                        ));
                    }
                }
                Type::Generic {
                    name: "Array".to_string(),
                    type_arg: Box::new(elem_type.clone()),
                }
            }

            Expr::Index { span, object, index } => {
                let obj_ty = self.synth_expr(object);
                let idx_ty = self.synth_expr(index);
                if idx_ty != Type::Int && idx_ty != Type::Unknown {
                    self.error(index.span(), format!(
                        "Array index must be int, found {}", idx_ty
                    ));
                }
                match obj_ty {
                    Type::Generic { name, type_arg } if name == "Array" => *type_arg,
                    Type::Unknown => Type::Unknown,
                    other => {
                        self.error(*span, format!("Cannot index into type {}", other));
                        Type::Unknown
                    }
                }
            }
        }
    }
}

impl Default for TypeChecker {
    fn default() -> Self {
        Self::new()
    }
}
```

## Tests

### tests/checker_test.rs

```rust
use type_checker::ast::*;
use type_checker::types::Type;
use type_checker::checker::TypeChecker;

fn span(line: usize, col: usize) -> Span {
    Span { line, col }
}

fn check(stmts: Vec<Stmt>) -> Vec<String> {
    let mut checker = TypeChecker::new();
    let errors = checker.check_program(&stmts);
    errors.iter().map(|e| e.message.clone()).collect()
}

#[test]
fn var_decl_with_annotation_ok() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Int),
            init: Expr::IntLit { span: span(1, 14), value: 42 },
        }),
    ];
    assert!(check(stmts).is_empty());
}

#[test]
fn var_decl_type_mismatch() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Int),
            init: Expr::StringLit { span: span(1, 14), value: "hello".into() },
        }),
    ];
    let errs = check(stmts);
    assert_eq!(errs.len(), 1);
    assert!(errs[0].contains("Cannot assign string"));
}

#[test]
fn type_inference() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: None,
            init: Expr::IntLit { span: span(1, 10), value: 5 },
        }),
        Stmt::Decl(Decl::Var {
            span: span(2, 1), name: "y".into(),
            type_ann: None,
            init: Expr::FloatLit { span: span(2, 10), value: 3.14 },
        }),
        Stmt::Decl(Decl::Var {
            span: span(3, 1), name: "z".into(),
            type_ann: None,
            init: Expr::BoolLit { span: span(3, 10), value: true },
        }),
    ];
    assert!(check(stmts).is_empty());
}

#[test]
fn arithmetic_int_plus_int() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Int),
            init: Expr::Binary {
                span: span(1, 14),
                left: Box::new(Expr::IntLit { span: span(1, 14), value: 1 }),
                op: BinOp::Add,
                right: Box::new(Expr::IntLit { span: span(1, 18), value: 2 }),
            },
        }),
    ];
    assert!(check(stmts).is_empty());
}

#[test]
fn arithmetic_string_plus_int_error() {
    let stmts = vec![
        Stmt::Expr {
            span: span(1, 1),
            expr: Expr::Binary {
                span: span(1, 1),
                left: Box::new(Expr::StringLit { span: span(1, 1), value: "a".into() }),
                op: BinOp::Add,
                right: Box::new(Expr::IntLit { span: span(1, 7), value: 1 }),
            },
        },
    ];
    let errs = check(stmts);
    assert_eq!(errs.len(), 1);
    assert!(errs[0].contains("Cannot apply"));
}

#[test]
fn int_plus_float_yields_float() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Float),
            init: Expr::Binary {
                span: span(1, 16),
                left: Box::new(Expr::IntLit { span: span(1, 16), value: 1 }),
                op: BinOp::Add,
                right: Box::new(Expr::FloatLit { span: span(1, 20), value: 2.5 }),
            },
        }),
    ];
    assert!(check(stmts).is_empty());
}

#[test]
fn function_call_type_check() {
    let stmts = vec![
        Stmt::Decl(Decl::Fun {
            span: span(1, 1), name: "add".into(),
            params: vec![("a".into(), Type::Int), ("b".into(), Type::Int)],
            return_type: Some(Type::Int),
            body: vec![
                Stmt::Return {
                    span: span(2, 5),
                    value: Some(Expr::Binary {
                        span: span(2, 12),
                        left: Box::new(Expr::Ident { span: span(2, 12), name: "a".into() }),
                        op: BinOp::Add,
                        right: Box::new(Expr::Ident { span: span(2, 16), name: "b".into() }),
                    }),
                },
            ],
        }),
        Stmt::Expr {
            span: span(4, 1),
            expr: Expr::Call {
                span: span(4, 1),
                callee: Box::new(Expr::Ident { span: span(4, 1), name: "add".into() }),
                args: vec![
                    Expr::StringLit { span: span(4, 5), value: "a".into() },
                    Expr::IntLit { span: span(4, 10), value: 2 },
                ],
            },
        },
    ];
    let errs = check(stmts);
    assert!(!errs.is_empty());
    assert!(errs.iter().any(|e| e.contains("Argument 1")));
}

#[test]
fn wrong_argument_count() {
    let stmts = vec![
        Stmt::Decl(Decl::Fun {
            span: span(1, 1), name: "f".into(),
            params: vec![("x".into(), Type::Int)],
            return_type: Some(Type::Int),
            body: vec![Stmt::Return {
                span: span(2, 5),
                value: Some(Expr::Ident { span: span(2, 12), name: "x".into() }),
            }],
        }),
        Stmt::Expr {
            span: span(3, 1),
            expr: Expr::Call {
                span: span(3, 1),
                callee: Box::new(Expr::Ident { span: span(3, 1), name: "f".into() }),
                args: vec![
                    Expr::IntLit { span: span(3, 3), value: 1 },
                    Expr::IntLit { span: span(3, 6), value: 2 },
                ],
            },
        },
    ];
    let errs = check(stmts);
    assert!(errs.iter().any(|e| e.contains("Expected 1 argument")));
}

#[test]
fn struct_field_access() {
    let stmts = vec![
        Stmt::Decl(Decl::Struct {
            span: span(1, 1), name: "Point".into(),
            type_params: vec![],
            fields: vec![("x".into(), Type::Float), ("y".into(), Type::Float)],
        }),
        Stmt::Decl(Decl::Var {
            span: span(3, 1), name: "p".into(),
            type_ann: None,
            init: Expr::StructLit {
                span: span(3, 10), name: "Point".into(),
                type_arg: None,
                fields: vec![
                    ("x".into(), Expr::FloatLit { span: span(3, 20), value: 1.0 }),
                    ("y".into(), Expr::FloatLit { span: span(3, 27), value: 2.0 }),
                ],
            },
        }),
        Stmt::Expr {
            span: span(4, 1),
            expr: Expr::FieldAccess {
                span: span(4, 1),
                object: Box::new(Expr::Ident { span: span(4, 1), name: "p".into() }),
                field: "z".into(), // nonexistent field
            },
        },
    ];
    let errs = check(stmts);
    assert!(errs.iter().any(|e| e.contains("no field 'z'")));
}

#[test]
fn generic_array_type_check() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "nums".into(),
            type_ann: None,
            init: Expr::ArrayLit {
                span: span(1, 12),
                elem_type: Type::Int,
                elements: vec![
                    Expr::IntLit { span: span(1, 17), value: 1 },
                    Expr::IntLit { span: span(1, 20), value: 2 },
                    Expr::StringLit { span: span(1, 23), value: "three".into() },
                ],
            },
        }),
    ];
    let errs = check(stmts);
    assert!(errs.iter().any(|e| e.contains("Array element type mismatch")));
}

#[test]
fn undefined_variable() {
    let stmts = vec![
        Stmt::Expr {
            span: span(1, 1),
            expr: Expr::Ident { span: span(1, 1), name: "undefined_var".into() },
        },
    ];
    let errs = check(stmts);
    assert!(errs.iter().any(|e| e.contains("Undefined variable")));
}

#[test]
fn multiple_errors_reported() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Int),
            init: Expr::StringLit { span: span(1, 14), value: "wrong".into() },
        }),
        Stmt::Expr {
            span: span(2, 1),
            expr: Expr::Ident { span: span(2, 1), name: "y".into() },
        },
        Stmt::Expr {
            span: span(3, 1),
            expr: Expr::Binary {
                span: span(3, 1),
                left: Box::new(Expr::BoolLit { span: span(3, 1), value: true }),
                op: BinOp::Add,
                right: Box::new(Expr::IntLit { span: span(3, 8), value: 5 }),
            },
        },
    ];
    let errs = check(stmts);
    assert!(errs.len() >= 3, "Expected at least 3 errors, got {}: {:?}", errs.len(), errs);
}

#[test]
fn return_type_mismatch() {
    let stmts = vec![
        Stmt::Decl(Decl::Fun {
            span: span(1, 1), name: "bad".into(),
            params: vec![],
            return_type: Some(Type::Int),
            body: vec![
                Stmt::Return {
                    span: span(2, 5),
                    value: Some(Expr::StringLit { span: span(2, 12), value: "oops".into() }),
                },
            ],
        }),
    ];
    let errs = check(stmts);
    assert!(errs.iter().any(|e| e.contains("Return type mismatch")));
}

#[test]
fn scope_shadowing() {
    let stmts = vec![
        Stmt::Decl(Decl::Var {
            span: span(1, 1), name: "x".into(),
            type_ann: Some(Type::Int),
            init: Expr::IntLit { span: span(1, 14), value: 5 },
        }),
        Stmt::Block {
            span: span(2, 1),
            stmts: vec![
                Stmt::Decl(Decl::Var {
                    span: span(3, 5), name: "x".into(),
                    type_ann: Some(Type::String),
                    init: Expr::StringLit { span: span(3, 19), value: "shadow".into() },
                }),
            ],
        },
    ];
    // Should succeed: inner x shadows outer x with different type
    assert!(check(stmts).is_empty());
}
```

## Running

```bash
cargo test
```

## Expected Output

```
running 13 tests
test checker_test::var_decl_with_annotation_ok ... ok
test checker_test::var_decl_type_mismatch ... ok
test checker_test::type_inference ... ok
test checker_test::arithmetic_int_plus_int ... ok
test checker_test::arithmetic_string_plus_int_error ... ok
test checker_test::int_plus_float_yields_float ... ok
test checker_test::function_call_type_check ... ok
test checker_test::wrong_argument_count ... ok
test checker_test::struct_field_access ... ok
test checker_test::generic_array_type_check ... ok
test checker_test::undefined_variable ... ok
test checker_test::multiple_errors_reported ... ok
test checker_test::return_type_mismatch ... ok
test checker_test::scope_shadowing ... ok

test result: ok. 14 passed; 0 failed
```

## Design Decisions

1. **Two-pass checking**: The first pass registers all struct and function declarations, the second pass type-checks bodies. This allows forward references -- a function can call another function defined later in the file.

2. **`Type::Unknown` as error propagation**: When a sub-expression has a type error, it returns `Type::Unknown`. All type-checking operations treat `Unknown` as compatible with everything, preventing cascading errors. One mistake in a variable declaration does not trigger errors in every subsequent use.

3. **Scope as linked list**: Each scope is a `HashMap` linked to its parent. This naturally handles shadowing (inner scopes can redefine names) and nested function bodies. The lookup walks up the chain until it finds the name or reaches the root.

4. **`is_compatible_with` vs strict equality**: Numeric widening (`int` to `float`) requires a compatibility check that is less strict than `==`. The method encapsulates this logic in one place, making it easy to extend (e.g., adding `int` to `double` promotion later).

5. **Tests construct ASTs directly**: Rather than parsing source code, tests build AST nodes manually. This isolates the type checker from parser bugs and makes test failures unambiguous about which component failed.

## Common Mistakes

1. **Cascading errors from `Unknown`**: Without an `Unknown` type, one undefined variable triggers errors in every expression that uses it. The `Unknown` type acts as a poison value that suppresses downstream errors.

2. **Forgetting forward declarations**: If functions can only reference previously declared functions, the checker rejects mutually recursive functions. The two-pass approach solves this.

3. **Implicit widening in both directions**: `int` to `float` is safe (no precision loss for reasonable values). `float` to `int` is lossy and should require an explicit cast. The `is_compatible_with` method must be asymmetric for assignment contexts.

4. **Generic type parameter substitution**: When checking `Array<int>`, you must substitute `T` with `int` in all field types before checking field assignments. Forgetting substitution causes phantom type errors on generic containers.

## Performance Notes

- The scope chain uses `HashMap` for each scope. For deeply nested scopes with few variables, a `Vec<(String, Type)>` with linear search would be faster due to cache locality.
- Type comparison allocates when printing error messages but is otherwise allocation-free (comparing enum variants and their data).
- For large programs, interning type representations (so that `Type::Int` is always the same pointer) would speed up comparisons from structural equality to pointer equality.

## Going Further

- Add union types (`int | string`) and type narrowing in conditionals
- Implement Hindley-Milner type inference for let-polymorphism (generalize function types automatically)
- Add trait/interface types with method dispatch checking
- Implement flow-sensitive typing: after `if x != null`, narrow `x` from `T?` to `T`
- Add a type-directed code generation pass that uses resolved types to emit efficient bytecode
