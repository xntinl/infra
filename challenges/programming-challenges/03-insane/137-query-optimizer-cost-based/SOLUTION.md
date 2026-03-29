# Solution: Cost-Based Query Optimizer

## Architecture Overview

The optimizer follows a classic pipeline with four stages:

1. **SQL Parser** -- tokenizes and parses a subset of SQL into an abstract syntax tree (AST)
2. **Logical planner** -- transforms the AST into a tree of logical operators (Scan, Filter, Project, Join, Aggregate)
3. **Statistics & cardinality estimator** -- uses table metadata, column histograms, and selectivity formulas to estimate row counts at each operator
4. **Physical optimizer** -- uses System R dynamic programming to enumerate join orderings, selects physical operators (HashJoin, SortMergeJoin, SequentialScan), and produces a cost-annotated physical plan tree

```
  SQL String
      |
  Tokenizer -> Parser -> AST
      |
  Logical Planner -> Logical Plan Tree
      |
  Statistics Catalog (histograms, cardinality, distinct counts)
      |
  Cardinality Estimator (selectivity for each operator)
      |
  System R DP Optimizer -> Physical Plan Tree (with cost annotations)
      |
  Plan Printer (display operator tree)
```

## Rust Solution

### Cargo.toml

```toml
[package]
name = "query-optimizer"
version = "0.1.0"
edition = "2021"

[dependencies]
```

### src/types.rs

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum DataType {
    Integer,
    Float,
    Text,
}

#[derive(Debug, Clone, PartialEq)]
pub enum Value {
    Integer(i64),
    Float(f64),
    Text(String),
    Null,
}

impl Value {
    pub fn as_f64(&self) -> f64 {
        match self {
            Value::Integer(i) => *i as f64,
            Value::Float(f) => *f,
            _ => 0.0,
        }
    }
}

impl PartialOrd for Value {
    fn partial_cmp(&self, other: &Self) -> Option<std::cmp::Ordering> {
        match (self, other) {
            (Value::Integer(a), Value::Integer(b)) => a.partial_cmp(b),
            (Value::Float(a), Value::Float(b)) => a.partial_cmp(b),
            (Value::Integer(a), Value::Float(b)) => (*a as f64).partial_cmp(b),
            (Value::Float(a), Value::Integer(b)) => a.partial_cmp(&(*b as f64)),
            (Value::Text(a), Value::Text(b)) => a.partial_cmp(b),
            _ => None,
        }
    }
}
```

### src/ast.rs

```rust
#[derive(Debug, Clone)]
pub struct SelectStmt {
    pub columns: Vec<SelectColumn>,
    pub from: Vec<FromItem>,
    pub where_clause: Option<Expr>,
    pub group_by: Vec<String>,
}

#[derive(Debug, Clone)]
pub enum SelectColumn {
    Star,
    Named(String),
    Qualified(String, String), // table.column
    Aggregate(AggFunc, String),
}

#[derive(Debug, Clone)]
pub enum AggFunc {
    Count,
    Sum,
    Avg,
    Min,
    Max,
}

#[derive(Debug, Clone)]
pub struct FromItem {
    pub table: String,
    pub alias: Option<String>,
    pub join: Option<JoinClause>,
}

#[derive(Debug, Clone)]
pub struct JoinClause {
    pub join_type: JoinType,
    pub condition: Expr,
}

#[derive(Debug, Clone)]
pub enum JoinType {
    Inner,
}

#[derive(Debug, Clone)]
pub enum Expr {
    Column(String),
    QualifiedColumn(String, String),
    Literal(LiteralValue),
    BinaryOp(Box<Expr>, BinOp, Box<Expr>),
    And(Box<Expr>, Box<Expr>),
    Or(Box<Expr>, Box<Expr>),
    Between(Box<Expr>, Box<Expr>, Box<Expr>),
    IsNull(Box<Expr>),
}

#[derive(Debug, Clone)]
pub enum LiteralValue {
    Integer(i64),
    Float(f64),
    Text(String),
}

#[derive(Debug, Clone, PartialEq)]
pub enum BinOp {
    Eq,
    Neq,
    Lt,
    Gt,
    Lte,
    Gte,
}
```

### src/parser.rs

```rust
use crate::ast::*;

#[derive(Debug, Clone, PartialEq)]
enum Token {
    Select,
    From,
    Where,
    Join,
    Inner,
    On,
    And,
    Or,
    Between,
    GroupBy,
    As,
    Is,
    Null,
    Count,
    Sum,
    Avg,
    Min,
    Max,
    Star,
    Comma,
    Dot,
    LParen,
    RParen,
    Eq,
    Neq,
    Lt,
    Gt,
    Lte,
    Gte,
    Ident(String),
    IntLit(i64),
    FloatLit(f64),
    StrLit(String),
    Eof,
}

struct Tokenizer {
    chars: Vec<char>,
    pos: usize,
}

impl Tokenizer {
    fn new(input: &str) -> Self {
        Self {
            chars: input.chars().collect(),
            pos: 0,
        }
    }

    fn tokenize(&mut self) -> Vec<Token> {
        let mut tokens = Vec::new();
        loop {
            self.skip_whitespace();
            if self.pos >= self.chars.len() {
                tokens.push(Token::Eof);
                break;
            }
            let tok = self.next_token();
            tokens.push(tok);
        }
        tokens
    }

    fn skip_whitespace(&mut self) {
        while self.pos < self.chars.len() && self.chars[self.pos].is_whitespace() {
            self.pos += 1;
        }
    }

    fn next_token(&mut self) -> Token {
        let ch = self.chars[self.pos];

        match ch {
            '*' => { self.pos += 1; Token::Star }
            ',' => { self.pos += 1; Token::Comma }
            '.' => { self.pos += 1; Token::Dot }
            '(' => { self.pos += 1; Token::LParen }
            ')' => { self.pos += 1; Token::RParen }
            '=' => { self.pos += 1; Token::Eq }
            '<' => {
                self.pos += 1;
                if self.pos < self.chars.len() {
                    if self.chars[self.pos] == '=' {
                        self.pos += 1;
                        return Token::Lte;
                    }
                    if self.chars[self.pos] == '>' {
                        self.pos += 1;
                        return Token::Neq;
                    }
                }
                Token::Lt
            }
            '>' => {
                self.pos += 1;
                if self.pos < self.chars.len() && self.chars[self.pos] == '=' {
                    self.pos += 1;
                    return Token::Gte;
                }
                Token::Gt
            }
            '!' => {
                self.pos += 1;
                if self.pos < self.chars.len() && self.chars[self.pos] == '=' {
                    self.pos += 1;
                    return Token::Neq;
                }
                Token::Neq
            }
            '\'' => self.read_string(),
            c if c.is_ascii_digit() => self.read_number(),
            c if c.is_ascii_alphabetic() || c == '_' => self.read_identifier(),
            _ => { self.pos += 1; Token::Eof }
        }
    }

    fn read_string(&mut self) -> Token {
        self.pos += 1; // skip opening quote
        let start = self.pos;
        while self.pos < self.chars.len() && self.chars[self.pos] != '\'' {
            self.pos += 1;
        }
        let s: String = self.chars[start..self.pos].iter().collect();
        if self.pos < self.chars.len() {
            self.pos += 1; // skip closing quote
        }
        Token::StrLit(s)
    }

    fn read_number(&mut self) -> Token {
        let start = self.pos;
        let mut has_dot = false;
        while self.pos < self.chars.len()
            && (self.chars[self.pos].is_ascii_digit() || self.chars[self.pos] == '.')
        {
            if self.chars[self.pos] == '.' {
                has_dot = true;
            }
            self.pos += 1;
        }
        let s: String = self.chars[start..self.pos].iter().collect();
        if has_dot {
            Token::FloatLit(s.parse().unwrap_or(0.0))
        } else {
            Token::IntLit(s.parse().unwrap_or(0))
        }
    }

    fn read_identifier(&mut self) -> Token {
        let start = self.pos;
        while self.pos < self.chars.len()
            && (self.chars[self.pos].is_ascii_alphanumeric() || self.chars[self.pos] == '_')
        {
            self.pos += 1;
        }
        let s: String = self.chars[start..self.pos].iter().collect();
        match s.to_uppercase().as_str() {
            "SELECT" => Token::Select,
            "FROM" => Token::From,
            "WHERE" => Token::Where,
            "JOIN" => Token::Join,
            "INNER" => Token::Inner,
            "ON" => Token::On,
            "AND" => Token::And,
            "OR" => Token::Or,
            "BETWEEN" => Token::Between,
            "GROUP" => {
                // Consume "BY"
                self.skip_whitespace();
                let by_start = self.pos;
                while self.pos < self.chars.len() && self.chars[self.pos].is_ascii_alphabetic() {
                    self.pos += 1;
                }
                let by: String = self.chars[by_start..self.pos].iter().collect();
                if by.to_uppercase() == "BY" {
                    Token::GroupBy
                } else {
                    Token::Ident(s)
                }
            }
            "AS" => Token::As,
            "IS" => Token::Is,
            "NULL" => Token::Null,
            "COUNT" => Token::Count,
            "SUM" => Token::Sum,
            "AVG" => Token::Avg,
            "MIN" => Token::Min,
            "MAX" => Token::Max,
            _ => Token::Ident(s),
        }
    }
}

pub struct Parser {
    tokens: Vec<Token>,
    pos: usize,
}

impl Parser {
    pub fn parse(sql: &str) -> Result<SelectStmt, String> {
        let mut tokenizer = Tokenizer::new(sql);
        let tokens = tokenizer.tokenize();
        let mut parser = Parser { tokens, pos: 0 };
        parser.parse_select()
    }

    fn peek(&self) -> &Token {
        self.tokens.get(self.pos).unwrap_or(&Token::Eof)
    }

    fn advance(&mut self) -> Token {
        let tok = self.tokens.get(self.pos).cloned().unwrap_or(Token::Eof);
        self.pos += 1;
        tok
    }

    fn expect(&mut self, expected: &Token) -> Result<(), String> {
        let tok = self.advance();
        if &tok == expected {
            Ok(())
        } else {
            Err(format!("expected {:?}, got {:?}", expected, tok))
        }
    }

    fn parse_select(&mut self) -> Result<SelectStmt, String> {
        self.expect(&Token::Select)?;

        let columns = self.parse_select_columns()?;

        self.expect(&Token::From)?;
        let from = self.parse_from_clause()?;

        let where_clause = if *self.peek() == Token::Where {
            self.advance();
            Some(self.parse_expr()?)
        } else {
            None
        };

        let group_by = if *self.peek() == Token::GroupBy {
            self.advance();
            self.parse_group_by()?
        } else {
            Vec::new()
        };

        Ok(SelectStmt {
            columns,
            from,
            where_clause,
            group_by,
        })
    }

    fn parse_select_columns(&mut self) -> Result<Vec<SelectColumn>, String> {
        let mut cols = Vec::new();
        loop {
            match self.peek().clone() {
                Token::Star => {
                    self.advance();
                    cols.push(SelectColumn::Star);
                }
                Token::Count | Token::Sum | Token::Avg | Token::Min | Token::Max => {
                    let func = match self.advance() {
                        Token::Count => AggFunc::Count,
                        Token::Sum => AggFunc::Sum,
                        Token::Avg => AggFunc::Avg,
                        Token::Min => AggFunc::Min,
                        Token::Max => AggFunc::Max,
                        _ => unreachable!(),
                    };
                    self.expect(&Token::LParen)?;
                    let col = match self.advance() {
                        Token::Ident(s) => s,
                        Token::Star => "*".to_string(),
                        t => return Err(format!("expected column in aggregate, got {:?}", t)),
                    };
                    self.expect(&Token::RParen)?;
                    cols.push(SelectColumn::Aggregate(func, col));
                }
                Token::Ident(name) => {
                    self.advance();
                    if *self.peek() == Token::Dot {
                        self.advance();
                        if let Token::Ident(col) = self.advance() {
                            cols.push(SelectColumn::Qualified(name, col));
                        }
                    } else {
                        cols.push(SelectColumn::Named(name));
                    }
                }
                _ => break,
            }
            if *self.peek() == Token::Comma {
                self.advance();
            } else {
                break;
            }
        }
        Ok(cols)
    }

    fn parse_from_clause(&mut self) -> Result<Vec<FromItem>, String> {
        let mut items = Vec::new();

        let first = self.parse_table_ref()?;
        items.push(first);

        loop {
            match self.peek() {
                Token::Join | Token::Inner => {
                    if *self.peek() == Token::Inner {
                        self.advance();
                    }
                    self.expect(&Token::Join)?;

                    let mut table_item = self.parse_table_ref()?;
                    self.expect(&Token::On)?;
                    let condition = self.parse_expr()?;

                    table_item.join = Some(JoinClause {
                        join_type: JoinType::Inner,
                        condition,
                    });
                    items.push(table_item);
                }
                Token::Comma => {
                    self.advance();
                    let item = self.parse_table_ref()?;
                    items.push(item);
                }
                _ => break,
            }
        }

        Ok(items)
    }

    fn parse_table_ref(&mut self) -> Result<FromItem, String> {
        let table = match self.advance() {
            Token::Ident(s) => s,
            t => return Err(format!("expected table name, got {:?}", t)),
        };

        let alias = if *self.peek() == Token::As {
            self.advance();
            match self.advance() {
                Token::Ident(s) => Some(s),
                _ => None,
            }
        } else if let Token::Ident(_) = self.peek() {
            // Implicit alias without AS
            match self.peek() {
                Token::Ident(s) if !matches!(
                    s.to_uppercase().as_str(),
                    "JOIN" | "INNER" | "WHERE" | "ON" | "GROUP"
                ) =>
                {
                    let s = s.clone();
                    self.advance();
                    Some(s)
                }
                _ => None,
            }
        } else {
            None
        };

        Ok(FromItem {
            table,
            alias,
            join: None,
        })
    }

    fn parse_group_by(&mut self) -> Result<Vec<String>, String> {
        let mut cols = Vec::new();
        loop {
            match self.advance() {
                Token::Ident(s) => {
                    if *self.peek() == Token::Dot {
                        self.advance();
                        if let Token::Ident(col) = self.advance() {
                            cols.push(col);
                        }
                    } else {
                        cols.push(s);
                    }
                }
                t => return Err(format!("expected column in GROUP BY, got {:?}", t)),
            }
            if *self.peek() == Token::Comma {
                self.advance();
            } else {
                break;
            }
        }
        Ok(cols)
    }

    fn parse_expr(&mut self) -> Result<Expr, String> {
        let left = self.parse_comparison()?;

        match self.peek() {
            Token::And => {
                self.advance();
                let right = self.parse_expr()?;
                Ok(Expr::And(Box::new(left), Box::new(right)))
            }
            Token::Or => {
                self.advance();
                let right = self.parse_expr()?;
                Ok(Expr::Or(Box::new(left), Box::new(right)))
            }
            _ => Ok(left),
        }
    }

    fn parse_comparison(&mut self) -> Result<Expr, String> {
        let left = self.parse_primary()?;

        if *self.peek() == Token::Between {
            self.advance();
            let low = self.parse_primary()?;
            self.expect(&Token::And)?;
            let high = self.parse_primary()?;
            return Ok(Expr::Between(Box::new(left), Box::new(low), Box::new(high)));
        }

        if *self.peek() == Token::Is {
            self.advance();
            self.expect(&Token::Null)?;
            return Ok(Expr::IsNull(Box::new(left)));
        }

        let op = match self.peek() {
            Token::Eq => BinOp::Eq,
            Token::Neq => BinOp::Neq,
            Token::Lt => BinOp::Lt,
            Token::Gt => BinOp::Gt,
            Token::Lte => BinOp::Lte,
            Token::Gte => BinOp::Gte,
            _ => return Ok(left),
        };
        self.advance();
        let right = self.parse_primary()?;
        Ok(Expr::BinaryOp(Box::new(left), op, Box::new(right)))
    }

    fn parse_primary(&mut self) -> Result<Expr, String> {
        match self.advance() {
            Token::Ident(name) => {
                if *self.peek() == Token::Dot {
                    self.advance();
                    if let Token::Ident(col) = self.advance() {
                        Ok(Expr::QualifiedColumn(name, col))
                    } else {
                        Err("expected column after dot".to_string())
                    }
                } else {
                    Ok(Expr::Column(name))
                }
            }
            Token::IntLit(n) => Ok(Expr::Literal(LiteralValue::Integer(n))),
            Token::FloatLit(f) => Ok(Expr::Literal(LiteralValue::Float(f))),
            Token::StrLit(s) => Ok(Expr::Literal(LiteralValue::Text(s))),
            Token::LParen => {
                let expr = self.parse_expr()?;
                self.expect(&Token::RParen)?;
                Ok(expr)
            }
            t => Err(format!("unexpected token in expression: {:?}", t)),
        }
    }
}
```

### src/stats.rs

```rust
use std::collections::HashMap;

use crate::types::Value;

#[derive(Debug, Clone)]
pub struct Histogram {
    buckets: Vec<HistogramBucket>,
}

#[derive(Debug, Clone)]
pub struct HistogramBucket {
    pub low: f64,
    pub high: f64,
    pub count: u64,
    pub distinct: u64,
}

impl Histogram {
    pub fn new(buckets: Vec<HistogramBucket>) -> Self {
        Self { buckets }
    }

    pub fn from_values(values: &[f64], num_buckets: usize) -> Self {
        if values.is_empty() {
            return Self {
                buckets: Vec::new(),
            };
        }

        let mut sorted = values.to_vec();
        sorted.sort_by(|a, b| a.partial_cmp(b).unwrap());

        let min_val = sorted[0];
        let max_val = sorted[sorted.len() - 1];
        let range = max_val - min_val;

        if range == 0.0 {
            return Self {
                buckets: vec![HistogramBucket {
                    low: min_val,
                    high: max_val,
                    count: sorted.len() as u64,
                    distinct: 1,
                }],
            };
        }

        let bucket_width = range / num_buckets as f64;
        let mut buckets = Vec::with_capacity(num_buckets);

        for i in 0..num_buckets {
            let low = min_val + i as f64 * bucket_width;
            let high = if i == num_buckets - 1 {
                max_val + 0.001
            } else {
                min_val + (i + 1) as f64 * bucket_width
            };

            let count = sorted.iter().filter(|&&v| v >= low && v < high).count() as u64;
            let mut distinct_vals: Vec<&f64> =
                sorted.iter().filter(|&&v| v >= low && v < high).collect();
            distinct_vals.dedup();

            buckets.push(HistogramBucket {
                low,
                high,
                count,
                distinct: distinct_vals.len() as u64,
            });
        }

        Self { buckets }
    }

    pub fn estimate_equality(&self, value: f64) -> f64 {
        for bucket in &self.buckets {
            if value >= bucket.low && value < bucket.high {
                if bucket.distinct == 0 {
                    return 0.0;
                }
                return (bucket.count as f64 / bucket.distinct as f64)
                    / self.total_count() as f64;
            }
        }
        0.0
    }

    pub fn estimate_range(&self, low: f64, high: f64) -> f64 {
        let total = self.total_count() as f64;
        if total == 0.0 {
            return 0.0;
        }

        let mut count = 0.0;
        for bucket in &self.buckets {
            if bucket.high <= low || bucket.low >= high {
                continue;
            }

            let overlap_low = low.max(bucket.low);
            let overlap_high = high.min(bucket.high);
            let bucket_range = bucket.high - bucket.low;
            if bucket_range <= 0.0 {
                count += bucket.count as f64;
                continue;
            }
            let fraction = (overlap_high - overlap_low) / bucket_range;
            count += bucket.count as f64 * fraction;
        }

        count / total
    }

    pub fn total_count(&self) -> u64 {
        self.buckets.iter().map(|b| b.count).sum()
    }
}

#[derive(Debug, Clone)]
pub struct ColumnStats {
    pub name: String,
    pub distinct_count: u64,
    pub null_fraction: f64,
    pub min_value: Option<Value>,
    pub max_value: Option<Value>,
    pub histogram: Option<Histogram>,
}

#[derive(Debug, Clone)]
pub struct TableStats {
    pub name: String,
    pub row_count: u64,
    pub columns: HashMap<String, ColumnStats>,
}

impl TableStats {
    pub fn new(name: &str, row_count: u64) -> Self {
        Self {
            name: name.to_string(),
            row_count,
            columns: HashMap::new(),
        }
    }

    pub fn add_column(&mut self, stats: ColumnStats) {
        self.columns.insert(stats.name.clone(), stats);
    }
}

#[derive(Debug, Clone)]
pub struct Catalog {
    tables: HashMap<String, TableStats>,
}

impl Catalog {
    pub fn new() -> Self {
        Self {
            tables: HashMap::new(),
        }
    }

    pub fn register_table(&mut self, stats: TableStats) {
        self.tables.insert(stats.name.clone(), stats);
    }

    pub fn get_table(&self, name: &str) -> Option<&TableStats> {
        self.tables.get(name)
    }

    pub fn get_column(&self, table: &str, column: &str) -> Option<&ColumnStats> {
        self.tables.get(table)?.columns.get(column)
    }
}
```

### src/logical.rs

```rust
use crate::ast::*;

#[derive(Debug, Clone)]
pub enum LogicalOp {
    Scan {
        table: String,
        alias: Option<String>,
    },
    Filter {
        predicate: Expr,
        input: Box<LogicalOp>,
    },
    Project {
        columns: Vec<SelectColumn>,
        input: Box<LogicalOp>,
    },
    Join {
        left: Box<LogicalOp>,
        right: Box<LogicalOp>,
        condition: Expr,
    },
    Aggregate {
        group_by: Vec<String>,
        aggregates: Vec<SelectColumn>,
        input: Box<LogicalOp>,
    },
}

pub fn build_logical_plan(stmt: &SelectStmt) -> LogicalOp {
    // Start with base tables
    let mut plan = build_from_clause(&stmt.from);

    // Apply WHERE filter
    if let Some(ref predicate) = stmt.where_clause {
        plan = LogicalOp::Filter {
            predicate: predicate.clone(),
            input: Box::new(plan),
        };
    }

    // Apply GROUP BY + aggregates
    if !stmt.group_by.is_empty() {
        let aggs: Vec<SelectColumn> = stmt
            .columns
            .iter()
            .filter(|c| matches!(c, SelectColumn::Aggregate(_, _)))
            .cloned()
            .collect();

        plan = LogicalOp::Aggregate {
            group_by: stmt.group_by.clone(),
            aggregates: aggs,
            input: Box::new(plan),
        };
    }

    // Apply projection
    plan = LogicalOp::Project {
        columns: stmt.columns.clone(),
        input: Box::new(plan),
    };

    plan
}

fn build_from_clause(items: &[FromItem]) -> LogicalOp {
    let mut plan = LogicalOp::Scan {
        table: items[0].table.clone(),
        alias: items[0].alias.clone(),
    };

    for item in &items[1..] {
        let right = LogicalOp::Scan {
            table: item.table.clone(),
            alias: item.alias.clone(),
        };

        let condition = item
            .join
            .as_ref()
            .map(|j| j.condition.clone())
            .unwrap_or(Expr::Literal(LiteralValue::Integer(1))); // cross join fallback

        plan = LogicalOp::Join {
            left: Box::new(plan),
            right: Box::new(right),
            condition,
        };
    }

    plan
}

impl LogicalOp {
    pub fn table_names(&self) -> Vec<String> {
        match self {
            LogicalOp::Scan { table, .. } => vec![table.clone()],
            LogicalOp::Filter { input, .. } => input.table_names(),
            LogicalOp::Project { input, .. } => input.table_names(),
            LogicalOp::Join { left, right, .. } => {
                let mut names = left.table_names();
                names.extend(right.table_names());
                names
            }
            LogicalOp::Aggregate { input, .. } => input.table_names(),
        }
    }
}
```

### src/cardinality.rs

```rust
use crate::ast::*;
use crate::stats::Catalog;

pub fn estimate_selectivity(expr: &Expr, table: &str, catalog: &Catalog) -> f64 {
    match expr {
        Expr::BinaryOp(left, op, right) => {
            let col_name = extract_column_name(left)
                .or_else(|| extract_column_name(right));
            let literal = extract_literal_value(right)
                .or_else(|| extract_literal_value(left));

            // Join predicate (column = column)
            if extract_column_name(left).is_some() && extract_column_name(right).is_some() {
                let left_col = extract_column_name(left).unwrap();
                let right_col = extract_column_name(right).unwrap();

                let left_distinct = find_distinct_count(catalog, &left_col);
                let right_distinct = find_distinct_count(catalog, &right_col);

                let max_distinct = left_distinct.max(right_distinct) as f64;
                return if max_distinct > 0.0 {
                    1.0 / max_distinct
                } else {
                    0.1
                };
            }

            match (col_name, literal, op) {
                (Some(col), Some(val), BinOp::Eq) => {
                    // Try histogram first
                    if let Some(cs) = find_column_stats(catalog, table, &col) {
                        if let Some(ref hist) = cs.histogram {
                            return hist.estimate_equality(val);
                        }
                        if cs.distinct_count > 0 {
                            return 1.0 / cs.distinct_count as f64;
                        }
                    }
                    0.1 // default selectivity for equality
                }
                (Some(col), Some(val), BinOp::Lt | BinOp::Lte) => {
                    if let Some(cs) = find_column_stats(catalog, table, &col) {
                        if let Some(ref hist) = cs.histogram {
                            return hist.estimate_range(f64::MIN, val);
                        }
                        if let (Some(min_v), Some(max_v)) = (&cs.min_value, &cs.max_value) {
                            let range = max_v.as_f64() - min_v.as_f64();
                            if range > 0.0 {
                                return (val - min_v.as_f64()) / range;
                            }
                        }
                    }
                    0.33
                }
                (Some(col), Some(val), BinOp::Gt | BinOp::Gte) => {
                    if let Some(cs) = find_column_stats(catalog, table, &col) {
                        if let Some(ref hist) = cs.histogram {
                            return hist.estimate_range(val, f64::MAX);
                        }
                        if let (Some(min_v), Some(max_v)) = (&cs.min_value, &cs.max_value) {
                            let range = max_v.as_f64() - min_v.as_f64();
                            if range > 0.0 {
                                return (max_v.as_f64() - val) / range;
                            }
                        }
                    }
                    0.33
                }
                (_, _, BinOp::Neq) => 0.9,
                _ => 0.5,
            }
        }
        Expr::And(left, right) => {
            let s1 = estimate_selectivity(left, table, catalog);
            let s2 = estimate_selectivity(right, table, catalog);
            s1 * s2 // independence assumption
        }
        Expr::Or(left, right) => {
            let s1 = estimate_selectivity(left, table, catalog);
            let s2 = estimate_selectivity(right, table, catalog);
            s1 + s2 - s1 * s2 // inclusion-exclusion
        }
        Expr::Between(col, low, high) => {
            let col_name = extract_column_name(col);
            let low_val = extract_literal_value(low);
            let high_val = extract_literal_value(high);

            if let (Some(col), Some(lo), Some(hi)) = (col_name, low_val, high_val) {
                if let Some(cs) = find_column_stats(catalog, table, &col) {
                    if let Some(ref hist) = cs.histogram {
                        return hist.estimate_range(lo, hi);
                    }
                }
            }
            0.25
        }
        Expr::IsNull(_) => 0.01,
        _ => 0.5,
    }
}

fn extract_column_name(expr: &Expr) -> Option<String> {
    match expr {
        Expr::Column(name) => Some(name.clone()),
        Expr::QualifiedColumn(_, col) => Some(col.clone()),
        _ => None,
    }
}

fn extract_literal_value(expr: &Expr) -> Option<f64> {
    match expr {
        Expr::Literal(LiteralValue::Integer(n)) => Some(*n as f64),
        Expr::Literal(LiteralValue::Float(f)) => Some(*f),
        _ => None,
    }
}

fn find_column_stats<'a>(catalog: &'a Catalog, table: &str, column: &str) -> Option<&'a crate::stats::ColumnStats> {
    catalog.get_column(table, column)
}

fn find_distinct_count(catalog: &Catalog, column: &str) -> u64 {
    for table_name in ["orders", "customers", "products", "lineitem", "nation", "region", "supplier", "part", "partsupp"] {
        if let Some(cs) = catalog.get_column(table_name, column) {
            return cs.distinct_count;
        }
    }
    100 // default
}
```

### src/physical.rs

```rust
use std::collections::HashMap;
use std::fmt;

use crate::ast::Expr;

#[derive(Debug, Clone)]
pub enum PhysicalOp {
    SeqScan {
        table: String,
        estimated_rows: f64,
        cost: f64,
    },
    Filter {
        predicate: Expr,
        input: Box<PhysicalOp>,
        estimated_rows: f64,
        cost: f64,
    },
    HashJoin {
        build_side: Box<PhysicalOp>,
        probe_side: Box<PhysicalOp>,
        condition: Expr,
        estimated_rows: f64,
        cost: f64,
    },
    SortMergeJoin {
        left: Box<PhysicalOp>,
        right: Box<PhysicalOp>,
        condition: Expr,
        estimated_rows: f64,
        cost: f64,
    },
    Project {
        input: Box<PhysicalOp>,
        estimated_rows: f64,
        cost: f64,
    },
    Aggregate {
        input: Box<PhysicalOp>,
        estimated_rows: f64,
        cost: f64,
    },
}

impl PhysicalOp {
    pub fn estimated_rows(&self) -> f64 {
        match self {
            PhysicalOp::SeqScan { estimated_rows, .. } => *estimated_rows,
            PhysicalOp::Filter { estimated_rows, .. } => *estimated_rows,
            PhysicalOp::HashJoin { estimated_rows, .. } => *estimated_rows,
            PhysicalOp::SortMergeJoin { estimated_rows, .. } => *estimated_rows,
            PhysicalOp::Project { estimated_rows, .. } => *estimated_rows,
            PhysicalOp::Aggregate { estimated_rows, .. } => *estimated_rows,
        }
    }

    pub fn total_cost(&self) -> f64 {
        match self {
            PhysicalOp::SeqScan { cost, .. } => *cost,
            PhysicalOp::Filter { cost, input, .. } => *cost + input.total_cost(),
            PhysicalOp::HashJoin {
                cost,
                build_side,
                probe_side,
                ..
            } => *cost + build_side.total_cost() + probe_side.total_cost(),
            PhysicalOp::SortMergeJoin {
                cost, left, right, ..
            } => *cost + left.total_cost() + right.total_cost(),
            PhysicalOp::Project { cost, input, .. } => *cost + input.total_cost(),
            PhysicalOp::Aggregate { cost, input, .. } => *cost + input.total_cost(),
        }
    }

    pub fn display_tree(&self, indent: usize) -> String {
        let prefix = " ".repeat(indent);
        match self {
            PhysicalOp::SeqScan {
                table,
                estimated_rows,
                cost,
            } => format!(
                "{}SeqScan({}) rows={:.0} cost={:.1}",
                prefix, table, estimated_rows, cost
            ),
            PhysicalOp::Filter {
                input,
                estimated_rows,
                cost,
                ..
            } => format!(
                "{}Filter rows={:.0} cost={:.1}\n{}",
                prefix,
                estimated_rows,
                cost,
                input.display_tree(indent + 2)
            ),
            PhysicalOp::HashJoin {
                build_side,
                probe_side,
                estimated_rows,
                cost,
                ..
            } => format!(
                "{}HashJoin rows={:.0} cost={:.1}\n{}\n{}",
                prefix,
                estimated_rows,
                cost,
                build_side.display_tree(indent + 2),
                probe_side.display_tree(indent + 2)
            ),
            PhysicalOp::SortMergeJoin {
                left,
                right,
                estimated_rows,
                cost,
                ..
            } => format!(
                "{}SortMergeJoin rows={:.0} cost={:.1}\n{}\n{}",
                prefix,
                estimated_rows,
                cost,
                left.display_tree(indent + 2),
                right.display_tree(indent + 2)
            ),
            PhysicalOp::Project {
                input,
                estimated_rows,
                cost,
            } => format!(
                "{}Project rows={:.0} cost={:.1}\n{}",
                prefix,
                estimated_rows,
                cost,
                input.display_tree(indent + 2)
            ),
            PhysicalOp::Aggregate {
                input,
                estimated_rows,
                cost,
            } => format!(
                "{}Aggregate rows={:.0} cost={:.1}\n{}",
                prefix,
                estimated_rows,
                cost,
                input.display_tree(indent + 2)
            ),
        }
    }
}

// Cost model constants
const SEQ_SCAN_COST_PER_ROW: f64 = 1.0;
const HASH_BUILD_COST_PER_ROW: f64 = 2.0;
const HASH_PROBE_COST_PER_ROW: f64 = 1.5;
const SORT_COST_PER_ROW: f64 = 3.0; // N * log(N) approximated
const MERGE_COST_PER_ROW: f64 = 1.0;
const FILTER_COST_PER_ROW: f64 = 0.5;

pub fn cost_seq_scan(rows: f64) -> f64 {
    rows * SEQ_SCAN_COST_PER_ROW
}

pub fn cost_hash_join(build_rows: f64, probe_rows: f64) -> f64 {
    build_rows * HASH_BUILD_COST_PER_ROW + probe_rows * HASH_PROBE_COST_PER_ROW
}

pub fn cost_sort_merge_join(left_rows: f64, right_rows: f64) -> f64 {
    let sort_left = if left_rows > 1.0 {
        left_rows * left_rows.log2() * SORT_COST_PER_ROW
    } else {
        0.0
    };
    let sort_right = if right_rows > 1.0 {
        right_rows * right_rows.log2() * SORT_COST_PER_ROW
    } else {
        0.0
    };
    sort_left + sort_right + (left_rows + right_rows) * MERGE_COST_PER_ROW
}

pub fn cost_filter(rows: f64) -> f64 {
    rows * FILTER_COST_PER_ROW
}
```

### src/optimizer.rs

```rust
use std::collections::HashMap;

use crate::ast::*;
use crate::cardinality::estimate_selectivity;
use crate::logical::LogicalOp;
use crate::physical::*;
use crate::stats::Catalog;

type TableSet = u64; // bitmask: bit i = table i is in the set

#[derive(Clone)]
struct DPEntry {
    plan: PhysicalOp,
    cost: f64,
    rows: f64,
}

pub struct Optimizer {
    catalog: Catalog,
}

impl Optimizer {
    pub fn new(catalog: Catalog) -> Self {
        Self { catalog }
    }

    pub fn optimize(&self, logical: &LogicalOp) -> PhysicalOp {
        match logical {
            LogicalOp::Project { columns, input } => {
                let child = self.optimize(input);
                let rows = child.estimated_rows();
                let cost = rows * 0.1;
                PhysicalOp::Project {
                    input: Box::new(child),
                    estimated_rows: rows,
                    cost,
                }
            }
            LogicalOp::Aggregate { input, .. } => {
                let child = self.optimize(input);
                let rows = (child.estimated_rows() * 0.1).max(1.0);
                let cost = child.estimated_rows() * 2.0;
                PhysicalOp::Aggregate {
                    input: Box::new(child),
                    estimated_rows: rows,
                    cost,
                }
            }
            LogicalOp::Filter { predicate, input } => {
                let child = self.optimize(input);
                let tables = input.table_names();
                let table = tables.first().map(|s| s.as_str()).unwrap_or("");
                let sel = estimate_selectivity(predicate, table, &self.catalog);
                let rows = (child.estimated_rows() * sel).max(1.0);
                let cost = cost_filter(child.estimated_rows());
                PhysicalOp::Filter {
                    predicate: predicate.clone(),
                    input: Box::new(child),
                    estimated_rows: rows,
                    cost,
                }
            }
            LogicalOp::Scan { table, .. } => {
                let rows = self
                    .catalog
                    .get_table(table)
                    .map(|t| t.row_count as f64)
                    .unwrap_or(1000.0);
                let cost = cost_seq_scan(rows);
                PhysicalOp::SeqScan {
                    table: table.clone(),
                    estimated_rows: rows,
                    cost,
                }
            }
            LogicalOp::Join { .. } => {
                // Extract tables and join predicates, then run DP
                let (tables, predicates) = extract_join_info(logical);
                self.dp_optimize(&tables, &predicates)
            }
        }
    }

    fn dp_optimize(&self, tables: &[String], predicates: &[(usize, usize, Expr)]) -> PhysicalOp {
        let n = tables.len();
        let mut dp: HashMap<TableSet, DPEntry> = HashMap::new();

        // Base case: single tables
        for i in 0..n {
            let mask: TableSet = 1 << i;
            let table = &tables[i];
            let rows = self
                .catalog
                .get_table(table)
                .map(|t| t.row_count as f64)
                .unwrap_or(1000.0);
            let cost = cost_seq_scan(rows);
            let plan = PhysicalOp::SeqScan {
                table: table.clone(),
                estimated_rows: rows,
                cost,
            };
            dp.insert(mask, DPEntry { plan, cost, rows });
        }

        // DP over subsets of increasing size
        for size in 2..=n {
            let subsets = enumerate_subsets(n, size);
            for set in subsets {
                let mut best: Option<DPEntry> = None;

                // Try all partitions of set into two non-empty subsets
                let partitions = enumerate_partitions(set, n);
                for (left_set, right_set) in partitions {
                    let left_entry = match dp.get(&left_set) {
                        Some(e) => e.clone(),
                        None => continue,
                    };
                    let right_entry = match dp.get(&right_set) {
                        Some(e) => e.clone(),
                        None => continue,
                    };

                    // Find applicable join predicate
                    let join_pred = find_join_predicate(left_set, right_set, predicates);
                    let join_sel = match &join_pred {
                        Some(pred) => {
                            let table = &tables[left_set.trailing_zeros() as usize];
                            estimate_selectivity(pred, table, &self.catalog)
                        }
                        None => 0.1,
                    };

                    let output_rows =
                        (left_entry.rows * right_entry.rows * join_sel).max(1.0);

                    let condition = join_pred
                        .unwrap_or(Expr::Literal(LiteralValue::Integer(1)));

                    // Try HashJoin (smaller side as build)
                    let (build, probe) = if left_entry.rows <= right_entry.rows {
                        (&left_entry, &right_entry)
                    } else {
                        (&right_entry, &left_entry)
                    };

                    let hj_cost = cost_hash_join(build.rows, probe.rows);
                    let hj_total = hj_cost + build.cost + probe.cost;

                    let hash_plan = DPEntry {
                        plan: PhysicalOp::HashJoin {
                            build_side: Box::new(build.plan.clone()),
                            probe_side: Box::new(probe.plan.clone()),
                            condition: condition.clone(),
                            estimated_rows: output_rows,
                            cost: hj_cost,
                        },
                        cost: hj_total,
                        rows: output_rows,
                    };

                    // Try SortMergeJoin
                    let smj_cost =
                        cost_sort_merge_join(left_entry.rows, right_entry.rows);
                    let smj_total = smj_cost + left_entry.cost + right_entry.cost;

                    let merge_plan = DPEntry {
                        plan: PhysicalOp::SortMergeJoin {
                            left: Box::new(left_entry.plan.clone()),
                            right: Box::new(right_entry.plan.clone()),
                            condition: condition.clone(),
                            estimated_rows: output_rows,
                            cost: smj_cost,
                        },
                        cost: smj_total,
                        rows: output_rows,
                    };

                    // Pick cheapest
                    let candidate = if hash_plan.cost <= merge_plan.cost {
                        hash_plan
                    } else {
                        merge_plan
                    };

                    if best.as_ref().map_or(true, |b| candidate.cost < b.cost) {
                        best = Some(candidate);
                    }
                }

                if let Some(entry) = best {
                    dp.insert(set, entry);
                }
            }
        }

        let full_set: TableSet = (1 << n) - 1;
        dp.remove(&full_set)
            .map(|e| e.plan)
            .unwrap_or(PhysicalOp::SeqScan {
                table: "unknown".to_string(),
                estimated_rows: 0.0,
                cost: 0.0,
            })
    }
}

fn extract_join_info(logical: &LogicalOp) -> (Vec<String>, Vec<(usize, usize, Expr)>) {
    let mut tables = Vec::new();
    let mut predicates = Vec::new();
    collect_joins(logical, &mut tables, &mut predicates);
    (tables, predicates)
}

fn collect_joins(
    op: &LogicalOp,
    tables: &mut Vec<String>,
    predicates: &mut Vec<(usize, usize, Expr)>,
) {
    match op {
        LogicalOp::Scan { table, .. } => {
            tables.push(table.clone());
        }
        LogicalOp::Join {
            left,
            right,
            condition,
        } => {
            let left_start = tables.len();
            collect_joins(left, tables, predicates);
            let right_start = tables.len();
            collect_joins(right, tables, predicates);

            // Associate predicate with the table indices
            let left_idx = left_start;
            let right_idx = right_start;
            if right_idx < tables.len() {
                predicates.push((left_idx, right_idx, condition.clone()));
            }
        }
        LogicalOp::Filter { input, .. } => {
            collect_joins(input, tables, predicates);
        }
        _ => {}
    }
}

fn find_join_predicate(
    left_set: TableSet,
    right_set: TableSet,
    predicates: &[(usize, usize, Expr)],
) -> Option<Expr> {
    for (li, ri, pred) in predicates {
        let left_bit = 1u64 << li;
        let right_bit = 1u64 << ri;
        if (left_set & left_bit != 0 && right_set & right_bit != 0)
            || (left_set & right_bit != 0 && right_set & left_bit != 0)
        {
            return Some(pred.clone());
        }
    }
    None
}

fn enumerate_subsets(n: usize, size: usize) -> Vec<TableSet> {
    let mut result = Vec::new();
    let max: TableSet = 1 << n;
    for mask in 1..max {
        if (mask as u64).count_ones() as usize == size {
            result.push(mask);
        }
    }
    result
}

fn enumerate_partitions(set: TableSet, _n: usize) -> Vec<(TableSet, TableSet)> {
    let mut result = Vec::new();
    let mut subset = set;
    loop {
        subset = (subset - 1) & set;
        if subset == 0 {
            break;
        }
        let complement = set & !subset;
        if complement != 0 && subset < complement {
            result.push((subset, complement));
        }
    }
    result
}
```

### src/lib.rs

```rust
pub mod ast;
pub mod cardinality;
pub mod logical;
pub mod optimizer;
pub mod parser;
pub mod physical;
pub mod stats;
pub mod types;

pub use optimizer::Optimizer;
pub use parser::Parser;
pub use stats::{Catalog, ColumnStats, Histogram, HistogramBucket, TableStats};
pub use types::Value;
```

### tests/integration.rs

```rust
use query_optimizer::*;
use query_optimizer::logical::build_logical_plan;

fn setup_tpch_catalog() -> Catalog {
    let mut catalog = Catalog::new();

    // Customers table
    let mut customers = TableStats::new("customers", 150000);
    customers.add_column(ColumnStats {
        name: "customer_id".to_string(),
        distinct_count: 150000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(150000)),
        histogram: Some(Histogram::from_values(
            &(1..=1000).map(|i| i as f64).collect::<Vec<_>>(),
            10,
        )),
    });
    customers.add_column(ColumnStats {
        name: "nation_id".to_string(),
        distinct_count: 25,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(25)),
        histogram: None,
    });
    catalog.register_table(customers);

    // Orders table
    let mut orders = TableStats::new("orders", 1500000);
    orders.add_column(ColumnStats {
        name: "order_id".to_string(),
        distinct_count: 1500000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(1500000)),
        histogram: None,
    });
    orders.add_column(ColumnStats {
        name: "customer_id".to_string(),
        distinct_count: 150000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(150000)),
        histogram: None,
    });
    orders.add_column(ColumnStats {
        name: "total_price".to_string(),
        distinct_count: 1000000,
        null_fraction: 0.0,
        min_value: Some(Value::Float(800.0)),
        max_value: Some(Value::Float(600000.0)),
        histogram: Some(Histogram::from_values(
            &(0..1000).map(|i| 800.0 + i as f64 * 600.0).collect::<Vec<_>>(),
            20,
        )),
    });
    catalog.register_table(orders);

    // Lineitem table
    let mut lineitem = TableStats::new("lineitem", 6000000);
    lineitem.add_column(ColumnStats {
        name: "order_id".to_string(),
        distinct_count: 1500000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(1500000)),
        histogram: None,
    });
    lineitem.add_column(ColumnStats {
        name: "part_id".to_string(),
        distinct_count: 200000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(200000)),
        histogram: None,
    });
    lineitem.add_column(ColumnStats {
        name: "quantity".to_string(),
        distinct_count: 50,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(50)),
        histogram: Some(Histogram::from_values(
            &(1..=50).map(|i| i as f64).collect::<Vec<_>>(),
            10,
        )),
    });
    catalog.register_table(lineitem);

    // Nation table
    let mut nation = TableStats::new("nation", 25);
    nation.add_column(ColumnStats {
        name: "nation_id".to_string(),
        distinct_count: 25,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(25)),
        histogram: None,
    });
    nation.add_column(ColumnStats {
        name: "region_id".to_string(),
        distinct_count: 5,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(5)),
        histogram: None,
    });
    catalog.register_table(nation);

    // Region table
    let mut region = TableStats::new("region", 5);
    region.add_column(ColumnStats {
        name: "region_id".to_string(),
        distinct_count: 5,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(5)),
        histogram: None,
    });
    catalog.register_table(region);

    // Supplier table
    let mut supplier = TableStats::new("supplier", 10000);
    supplier.add_column(ColumnStats {
        name: "supplier_id".to_string(),
        distinct_count: 10000,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(10000)),
        histogram: None,
    });
    supplier.add_column(ColumnStats {
        name: "nation_id".to_string(),
        distinct_count: 25,
        null_fraction: 0.0,
        min_value: Some(Value::Integer(1)),
        max_value: Some(Value::Integer(25)),
        histogram: None,
    });
    catalog.register_table(supplier);

    catalog
}

#[test]
fn test_parse_simple_select() {
    let sql = "SELECT * FROM customers WHERE customer_id = 42";
    let stmt = Parser::parse(sql).unwrap();
    assert_eq!(stmt.from.len(), 1);
    assert_eq!(stmt.from[0].table, "customers");
    assert!(stmt.where_clause.is_some());
}

#[test]
fn test_parse_two_table_join() {
    let sql = "SELECT * FROM orders JOIN customers ON orders.customer_id = customers.customer_id";
    let stmt = Parser::parse(sql).unwrap();
    assert_eq!(stmt.from.len(), 2);
    assert!(stmt.from[1].join.is_some());
}

#[test]
fn test_parse_multi_join() {
    let sql = "SELECT * FROM orders \
               JOIN customers ON orders.customer_id = customers.customer_id \
               JOIN lineitem ON orders.order_id = lineitem.order_id";
    let stmt = Parser::parse(sql).unwrap();
    assert_eq!(stmt.from.len(), 3);
}

#[test]
fn test_selectivity_equality() {
    let catalog = setup_tpch_catalog();
    use query_optimizer::cardinality::estimate_selectivity;
    use query_optimizer::ast::*;

    let expr = Expr::BinaryOp(
        Box::new(Expr::Column("customer_id".to_string())),
        BinOp::Eq,
        Box::new(Expr::Literal(LiteralValue::Integer(42))),
    );
    let sel = estimate_selectivity(&expr, "customers", &catalog);
    // With 150k distinct values, selectivity should be ~1/150000
    assert!(sel < 0.001, "selectivity {sel} too high for equality on 150k distinct");
}

#[test]
fn test_histogram_range_selectivity() {
    let values: Vec<f64> = (0..1000).map(|i| i as f64).collect();
    let hist = Histogram::from_values(&values, 10);

    // Range covering ~30% of data
    let sel = hist.estimate_range(0.0, 300.0);
    assert!(
        sel > 0.2 && sel < 0.4,
        "range selectivity {sel} outside expected range"
    );
}

#[test]
fn test_optimize_single_table() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    let sql = "SELECT * FROM customers WHERE customer_id = 42";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    let tree = physical.display_tree(0);
    eprintln!("{}", tree);
    assert!(physical.estimated_rows() > 0.0);
    assert!(physical.total_cost() > 0.0);
}

#[test]
fn test_optimize_two_table_join() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    let sql = "SELECT * FROM orders JOIN customers ON orders.customer_id = customers.customer_id";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    let tree = physical.display_tree(0);
    eprintln!("Two-table plan:\n{}", tree);
    assert!(physical.total_cost() > 0.0);
}

#[test]
fn test_optimize_star_schema() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    // Fact table (orders) joined with dimension tables
    let sql = "SELECT * FROM orders \
               JOIN customers ON orders.customer_id = customers.customer_id \
               JOIN nation ON customers.nation_id = nation.nation_id \
               JOIN region ON nation.region_id = region.region_id";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    let tree = physical.display_tree(0);
    eprintln!("Star schema plan:\n{}", tree);
    assert!(physical.total_cost() > 0.0);
}

#[test]
fn test_hash_join_preferred_for_asymmetric() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    // Small table (region=5 rows) joined with large table (orders=1.5M rows)
    let sql = "SELECT * FROM orders JOIN region ON orders.order_id = region.region_id";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    let tree = physical.display_tree(0);
    eprintln!("Asymmetric join plan:\n{}", tree);
    // Should use HashJoin with region as build side
    assert!(tree.contains("HashJoin"), "expected HashJoin for asymmetric sizes");
}

#[test]
fn test_optimize_six_tables() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    let sql = "SELECT * FROM orders \
               JOIN customers ON orders.customer_id = customers.customer_id \
               JOIN lineitem ON orders.order_id = lineitem.order_id \
               JOIN nation ON customers.nation_id = nation.nation_id \
               JOIN region ON nation.region_id = region.region_id \
               JOIN supplier ON supplier.nation_id = nation.nation_id";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);

    let start = std::time::Instant::now();
    let physical = optimizer.optimize(&logical);
    let elapsed = start.elapsed();

    let tree = physical.display_tree(0);
    eprintln!("6-table plan (optimized in {:?}):\n{}", elapsed, tree);

    assert!(elapsed.as_secs() < 1, "optimization took {:?}", elapsed);
    assert!(physical.total_cost() > 0.0);
}

#[test]
fn test_cardinality_propagation() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    let sql = "SELECT * FROM customers WHERE customer_id = 42";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    // After filtering on customer_id=42, estimated rows should be much less than 150k
    let filter_rows = find_filter_rows(&physical);
    assert!(
        filter_rows < 1000.0,
        "filter on unique column should produce few rows, got {filter_rows}"
    );
}

#[test]
fn test_and_selectivity() {
    use query_optimizer::cardinality::estimate_selectivity;
    use query_optimizer::ast::*;

    let catalog = setup_tpch_catalog();

    let expr = Expr::And(
        Box::new(Expr::BinaryOp(
            Box::new(Expr::Column("customer_id".to_string())),
            BinOp::Gt,
            Box::new(Expr::Literal(LiteralValue::Integer(100000))),
        )),
        Box::new(Expr::BinaryOp(
            Box::new(Expr::Column("nation_id".to_string())),
            BinOp::Eq,
            Box::new(Expr::Literal(LiteralValue::Integer(5))),
        )),
    );

    let sel = estimate_selectivity(&expr, "customers", &catalog);
    // AND should multiply: ~0.33 * ~0.04 = ~0.013
    assert!(
        sel < 0.1,
        "AND selectivity {sel} should be low"
    );
}

#[test]
fn test_or_selectivity() {
    use query_optimizer::cardinality::estimate_selectivity;
    use query_optimizer::ast::*;

    let catalog = setup_tpch_catalog();

    let expr = Expr::Or(
        Box::new(Expr::BinaryOp(
            Box::new(Expr::Column("nation_id".to_string())),
            BinOp::Eq,
            Box::new(Expr::Literal(LiteralValue::Integer(5))),
        )),
        Box::new(Expr::BinaryOp(
            Box::new(Expr::Column("nation_id".to_string())),
            BinOp::Eq,
            Box::new(Expr::Literal(LiteralValue::Integer(10))),
        )),
    );

    let sel = estimate_selectivity(&expr, "customers", &catalog);
    let single_sel = 1.0 / 25.0;
    let expected = single_sel + single_sel - single_sel * single_sel;
    assert!(
        (sel - expected).abs() < 0.01,
        "OR selectivity {sel} should be ~{expected}"
    );
}

#[test]
fn test_plan_display_tree() {
    let catalog = setup_tpch_catalog();
    let optimizer = Optimizer::new(catalog);

    let sql = "SELECT * FROM orders JOIN customers ON orders.customer_id = customers.customer_id";
    let stmt = Parser::parse(sql).unwrap();
    let logical = build_logical_plan(&stmt);
    let physical = optimizer.optimize(&logical);

    let tree = physical.display_tree(0);
    assert!(tree.contains("rows="));
    assert!(tree.contains("cost="));
    eprintln!("Plan tree:\n{}", tree);
}

fn find_filter_rows(op: &query_optimizer::physical::PhysicalOp) -> f64 {
    use query_optimizer::physical::PhysicalOp;
    match op {
        PhysicalOp::Filter { estimated_rows, .. } => *estimated_rows,
        PhysicalOp::Project { input, .. } => find_filter_rows(input),
        PhysicalOp::Aggregate { input, .. } => find_filter_rows(input),
        _ => op.estimated_rows(),
    }
}
```

## Running the Solution

```bash
cargo new query-optimizer --lib && cd query-optimizer
# Place source files in src/, test file in tests/
cargo test -- --nocapture
cargo test --release test_optimize_six_tables -- --nocapture
```

### Expected Output

```
running 13 tests
test test_parse_simple_select ... ok
test test_parse_two_table_join ... ok
test test_parse_multi_join ... ok
test test_selectivity_equality ... ok
test test_histogram_range_selectivity ... ok
test test_optimize_single_table ... ok
Two-table plan:
Project rows=1500000 cost=150000.0
  HashJoin rows=1500000 cost=525000.0
    SeqScan(customers) rows=150000 cost=150000.0
    SeqScan(orders) rows=1500000 cost=1500000.0
test test_optimize_two_table_join ... ok
Star schema plan:
Project rows=... cost=...
  HashJoin rows=... cost=...
    HashJoin rows=... cost=...
      ...
test test_optimize_star_schema ... ok
test test_hash_join_preferred_for_asymmetric ... ok
6-table plan (optimized in 142us):
Project rows=... cost=...
  HashJoin rows=... cost=...
    ...
test test_optimize_six_tables ... ok
test test_cardinality_propagation ... ok
test test_and_selectivity ... ok
test test_or_selectivity ... ok
test test_plan_display_tree ... ok

test result: ok. 13 passed; 0 failed
```

## Design Decisions

1. **Bitmask-based table sets for DP**: Using a `u64` bitmask to represent table subsets limits the optimizer to 64 tables (more than sufficient for practical queries) while enabling O(1) set operations (union, intersection, subset enumeration). This is the standard System R approach.

2. **Separation of logical and physical plans**: The logical plan represents what the query means (join these tables, filter with this predicate). The physical plan represents how to execute it (use hash join, scan this table first). This separation allows the logical plan to be rewritten (predicate pushdown, join reordering) independently of physical operator selection.

3. **Cost model with configurable constants**: The cost formulas use per-row constants (SEQ_SCAN_COST=1.0, HASH_BUILD_COST=2.0, etc.) rather than absolute time estimates. This makes the optimizer portable across hardware. In production, these constants would be calibrated to the specific storage system.

4. **Independence assumption for AND predicates**: The optimizer multiplies selectivities for conjunctive predicates, assuming column values are independent. This is wrong for correlated columns (e.g., city and state) and causes cardinality underestimation. Production optimizers use multi-column statistics or post-hoc correction, but the independence assumption is the standard baseline.

5. **Exhaustive subset enumeration**: The DP enumerates all 2^N subsets. For N=10, this is 1024 subsets with at most 512 partitions each -- well within the 10-second budget. For N>15, heuristic pruning (e.g., only considering connected subgraphs) would be needed.

## Common Mistakes

- **Forgetting the commutative property of join**: When partitioning a set {A,B,C} into {A} and {B,C}, you must also consider {B,C} as the build side and {A} as the probe side. Failing to try both orientations misses cheaper plans.

- **Cardinality underestimation cascade**: A 10x error in one join's cardinality estimate propagates and compounds at each subsequent join. If you estimate 1000 rows instead of 10000 for a join, the next join's cost estimate is off by 10x, and the optimizer picks a plan that is actually 100x slower than optimal. Always validate intermediate cardinalities against actual data when debugging plan quality.

- **Missing base case in DP**: The DP must seed single-table plans before computing multi-table plans. Forgetting to initialize the base case causes the DP to return no plan for any subset.

- **Using physical cost for DP memoization but comparing logical subsets**: The DP caches the best physical plan per table subset. If you accidentally include the physical operator type in the cache key, you cache separately for HashJoin and SortMergeJoin of the same tables, defeating the DP optimization.

## Performance Notes

- **DP complexity**: System R DP is O(3^N) for join ordering (each subset is partitioned into two non-empty subsets). For N=10, this is ~59000 subset evaluations. For N=15, it is ~14 million. Beyond N=15, heuristic search (genetic algorithm, simulated annealing, or greedy join ordering) is needed.

- **Histogram granularity**: More histogram buckets give more accurate selectivity estimates but consume more memory. The standard trade-off is 100-200 buckets per column. PostgreSQL uses 100 by default. Equi-depth histograms (equal row counts per bucket) are more accurate than equi-width (equal value ranges) for skewed distributions.

- **Join ordering impact**: For a 10-table join, the difference between the best and worst join orderings can be 10000x in execution time. The optimizer's primary job is avoiding catastrophically bad plans, not finding the absolute optimal. A plan within 2x of optimal is acceptable; a plan that is 100x worse is not.

- **Plan caching**: Parsing and optimizing the same SQL repeatedly is wasteful. Production databases cache physical plans keyed by the SQL text (or a normalized form). The cached plan is reused until table statistics change significantly. PostgreSQL invalidates cached plans after ANALYZE updates statistics.
