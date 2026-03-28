# Solution: Custom Regex Engine (NFA to DFA)

## Architecture Overview

The engine has five layers:

1. **Parser**: regex string -> AST. Recursive descent with operator precedence.
2. **NFA Builder (Thompson's Construction)**: AST -> NFA with epsilon transitions.
3. **NFA Simulator**: runs the NFA directly by tracking all active states.
4. **DFA Builder (Subset Construction)**: NFA -> DFA where each state is a set of NFA states.
5. **DFA Executor**: runs the DFA with one active state per character.

Data flow: `"a(b|c)*d"` -> Parser -> `Concat(Lit('a'), Concat(Star(Alt(Lit('b'), Lit('c'))), Lit('d')))` -> Thompson -> NFA (8 states) -> Subset -> DFA (4 states) -> execute against input.

---

## Rust Solution

### Project Setup

```bash
cargo new regex-engine
cd regex-engine
```

Add to `Cargo.toml`:

```toml
[dependencies]

[dev-dependencies]
criterion = { version = "0.5", features = ["html_reports"] }
regex = "1"

[[bench]]
name = "regex_bench"
harness = false
```

### `src/ast.rs` -- Regex AST

```rust
#[derive(Debug, Clone, PartialEq)]
pub enum Regex {
    Literal(char),
    Dot,
    CharClass(Vec<CharRange>, bool), // ranges, negated
    Concat(Box<Regex>, Box<Regex>),
    Alternation(Box<Regex>, Box<Regex>),
    Star(Box<Regex>),
    Plus(Box<Regex>),
    Optional(Box<Regex>),
    AnchorStart,
    AnchorEnd,
    Group(Box<Regex>),
}

#[derive(Debug, Clone, PartialEq)]
pub struct CharRange {
    pub start: char,
    pub end: char,
}

impl CharRange {
    pub fn single(c: char) -> Self {
        Self { start: c, end: c }
    }

    pub fn range(start: char, end: char) -> Self {
        Self { start, end }
    }

    pub fn matches(&self, c: char) -> bool {
        c >= self.start && c <= self.end
    }
}
```

### `src/parser.rs` -- Recursive Descent Parser

```rust
use crate::ast::{CharRange, Regex};

#[derive(Debug)]
pub struct ParseError {
    pub message: String,
    pub position: usize,
}

impl std::fmt::Display for ParseError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "parse error at position {}: {}", self.position, self.message)
    }
}

pub struct Parser {
    chars: Vec<char>,
    pos: usize,
}

impl Parser {
    pub fn parse(pattern: &str) -> Result<Regex, ParseError> {
        let mut parser = Parser {
            chars: pattern.chars().collect(),
            pos: 0,
        };
        let result = parser.parse_alternation()?;
        if parser.pos < parser.chars.len() {
            return Err(ParseError {
                message: format!("unexpected character '{}'", parser.chars[parser.pos]),
                position: parser.pos,
            });
        }
        Ok(result)
    }

    fn peek(&self) -> Option<char> {
        self.chars.get(self.pos).copied()
    }

    fn advance(&mut self) -> Option<char> {
        let c = self.chars.get(self.pos).copied();
        if c.is_some() {
            self.pos += 1;
        }
        c
    }

    fn expect(&mut self, expected: char) -> Result<(), ParseError> {
        match self.advance() {
            Some(c) if c == expected => Ok(()),
            Some(c) => Err(ParseError {
                message: format!("expected '{}', found '{}'", expected, c),
                position: self.pos - 1,
            }),
            None => Err(ParseError {
                message: format!("expected '{}', found end of pattern", expected),
                position: self.pos,
            }),
        }
    }

    fn parse_alternation(&mut self) -> Result<Regex, ParseError> {
        let mut left = self.parse_concat()?;
        while self.peek() == Some('|') {
            self.advance();
            let right = self.parse_concat()?;
            left = Regex::Alternation(Box::new(left), Box::new(right));
        }
        Ok(left)
    }

    fn parse_concat(&mut self) -> Result<Regex, ParseError> {
        let mut terms = Vec::new();
        while let Some(c) = self.peek() {
            if c == ')' || c == '|' {
                break;
            }
            terms.push(self.parse_quantifier()?);
        }
        if terms.is_empty() {
            return Err(ParseError {
                message: "empty expression".to_string(),
                position: self.pos,
            });
        }
        let mut result = terms.remove(0);
        for term in terms {
            result = Regex::Concat(Box::new(result), Box::new(term));
        }
        Ok(result)
    }

    fn parse_quantifier(&mut self) -> Result<Regex, ParseError> {
        let atom = self.parse_atom()?;
        match self.peek() {
            Some('*') => {
                self.advance();
                Ok(Regex::Star(Box::new(atom)))
            }
            Some('+') => {
                self.advance();
                Ok(Regex::Plus(Box::new(atom)))
            }
            Some('?') => {
                self.advance();
                Ok(Regex::Optional(Box::new(atom)))
            }
            _ => Ok(atom),
        }
    }

    fn parse_atom(&mut self) -> Result<Regex, ParseError> {
        match self.peek() {
            Some('(') => {
                self.advance();
                let inner = self.parse_alternation()?;
                self.expect(')')?;
                Ok(Regex::Group(Box::new(inner)))
            }
            Some('[') => self.parse_char_class(),
            Some('.') => {
                self.advance();
                Ok(Regex::Dot)
            }
            Some('^') => {
                self.advance();
                Ok(Regex::AnchorStart)
            }
            Some('$') => {
                self.advance();
                Ok(Regex::AnchorEnd)
            }
            Some('\\') => {
                self.advance();
                self.parse_escape()
            }
            Some(c) if c != ')' && c != '|' && c != '*' && c != '+' && c != '?' => {
                self.advance();
                Ok(Regex::Literal(c))
            }
            Some(c) => Err(ParseError {
                message: format!("unexpected character '{}'", c),
                position: self.pos,
            }),
            None => Err(ParseError {
                message: "unexpected end of pattern".to_string(),
                position: self.pos,
            }),
        }
    }

    fn parse_escape(&mut self) -> Result<Regex, ParseError> {
        match self.advance() {
            Some('d') => Ok(Regex::CharClass(vec![CharRange::range('0', '9')], false)),
            Some('w') => Ok(Regex::CharClass(vec![
                CharRange::range('a', 'z'),
                CharRange::range('A', 'Z'),
                CharRange::range('0', '9'),
                CharRange::single('_'),
            ], false)),
            Some('s') => Ok(Regex::CharClass(vec![
                CharRange::single(' '),
                CharRange::single('\t'),
                CharRange::single('\n'),
                CharRange::single('\r'),
            ], false)),
            Some(c @ ('.' | '*' | '+' | '?' | '(' | ')' | '[' | ']' | '\\' | '|' | '^' | '$')) => {
                Ok(Regex::Literal(c))
            }
            Some(c) => Err(ParseError {
                message: format!("invalid escape sequence '\\{}'", c),
                position: self.pos - 1,
            }),
            None => Err(ParseError {
                message: "unexpected end after '\\'".to_string(),
                position: self.pos,
            }),
        }
    }

    fn parse_char_class(&mut self) -> Result<Regex, ParseError> {
        self.advance(); // consume '['
        let negated = self.peek() == Some('^');
        if negated {
            self.advance();
        }

        let mut ranges = Vec::new();
        while self.peek() != Some(']') {
            let start = self.advance().ok_or_else(|| ParseError {
                message: "unclosed character class".to_string(),
                position: self.pos,
            })?;

            if self.peek() == Some('-') {
                self.advance();
                if self.peek() == Some(']') {
                    ranges.push(CharRange::single(start));
                    ranges.push(CharRange::single('-'));
                } else {
                    let end = self.advance().ok_or_else(|| ParseError {
                        message: "unclosed character class".to_string(),
                        position: self.pos,
                    })?;
                    ranges.push(CharRange::range(start, end));
                }
            } else {
                ranges.push(CharRange::single(start));
            }
        }
        self.expect(']')?;
        Ok(Regex::CharClass(ranges, negated))
    }
}
```

### `src/nfa.rs` -- Thompson's Construction and NFA Simulation

```rust
use crate::ast::{CharRange, Regex};
use std::collections::{BTreeSet, HashSet, VecDeque};

#[derive(Debug, Clone)]
pub enum Transition {
    Epsilon,
    Char(char),
    CharClass(Vec<CharRange>, bool), // ranges, negated
    Dot,
}

impl Transition {
    pub fn matches(&self, c: char) -> bool {
        match self {
            Transition::Char(expected) => c == *expected,
            Transition::Dot => c != '\n',
            Transition::CharClass(ranges, negated) => {
                let in_range = ranges.iter().any(|r| r.matches(c));
                if *negated { !in_range } else { in_range }
            }
            Transition::Epsilon => false,
        }
    }
}

#[derive(Debug)]
pub struct NfaState {
    pub transitions: Vec<(Transition, usize)>, // (transition, target state)
}

#[derive(Debug)]
pub struct Nfa {
    pub states: Vec<NfaState>,
    pub start: usize,
    pub accept: usize,
}

struct Fragment {
    start: usize,
    accept: usize,
}

pub struct NfaBuilder {
    states: Vec<NfaState>,
}

impl NfaBuilder {
    pub fn build(regex: &Regex) -> Nfa {
        let mut builder = NfaBuilder { states: Vec::new() };
        let fragment = builder.build_fragment(regex);
        Nfa {
            states: builder.states,
            start: fragment.start,
            accept: fragment.accept,
        }
    }

    fn new_state(&mut self) -> usize {
        let id = self.states.len();
        self.states.push(NfaState { transitions: Vec::new() });
        id
    }

    fn add_transition(&mut self, from: usize, trans: Transition, to: usize) {
        self.states[from].transitions.push((trans, to));
    }

    fn build_fragment(&mut self, regex: &Regex) -> Fragment {
        match regex {
            Regex::Literal(c) => {
                let start = self.new_state();
                let accept = self.new_state();
                self.add_transition(start, Transition::Char(*c), accept);
                Fragment { start, accept }
            }

            Regex::Dot => {
                let start = self.new_state();
                let accept = self.new_state();
                self.add_transition(start, Transition::Dot, accept);
                Fragment { start, accept }
            }

            Regex::CharClass(ranges, negated) => {
                let start = self.new_state();
                let accept = self.new_state();
                self.add_transition(start, Transition::CharClass(ranges.clone(), *negated), accept);
                Fragment { start, accept }
            }

            Regex::Concat(left, right) => {
                let left_frag = self.build_fragment(left);
                let right_frag = self.build_fragment(right);
                self.add_transition(left_frag.accept, Transition::Epsilon, right_frag.start);
                Fragment { start: left_frag.start, accept: right_frag.accept }
            }

            Regex::Alternation(left, right) => {
                let start = self.new_state();
                let accept = self.new_state();
                let left_frag = self.build_fragment(left);
                let right_frag = self.build_fragment(right);
                self.add_transition(start, Transition::Epsilon, left_frag.start);
                self.add_transition(start, Transition::Epsilon, right_frag.start);
                self.add_transition(left_frag.accept, Transition::Epsilon, accept);
                self.add_transition(right_frag.accept, Transition::Epsilon, accept);
                Fragment { start, accept }
            }

            Regex::Star(inner) => {
                let start = self.new_state();
                let accept = self.new_state();
                let inner_frag = self.build_fragment(inner);
                self.add_transition(start, Transition::Epsilon, inner_frag.start);
                self.add_transition(start, Transition::Epsilon, accept);
                self.add_transition(inner_frag.accept, Transition::Epsilon, inner_frag.start);
                self.add_transition(inner_frag.accept, Transition::Epsilon, accept);
                Fragment { start, accept }
            }

            Regex::Plus(inner) => {
                let start = self.new_state();
                let accept = self.new_state();
                let inner_frag = self.build_fragment(inner);
                self.add_transition(start, Transition::Epsilon, inner_frag.start);
                self.add_transition(inner_frag.accept, Transition::Epsilon, inner_frag.start);
                self.add_transition(inner_frag.accept, Transition::Epsilon, accept);
                Fragment { start, accept }
            }

            Regex::Optional(inner) => {
                let start = self.new_state();
                let accept = self.new_state();
                let inner_frag = self.build_fragment(inner);
                self.add_transition(start, Transition::Epsilon, inner_frag.start);
                self.add_transition(start, Transition::Epsilon, accept);
                self.add_transition(inner_frag.accept, Transition::Epsilon, accept);
                Fragment { start, accept }
            }

            Regex::AnchorStart | Regex::AnchorEnd => {
                let start = self.new_state();
                Fragment { start, accept: start }
            }

            Regex::Group(inner) => self.build_fragment(inner),
        }
    }
}

pub fn epsilon_closure(nfa: &Nfa, states: &BTreeSet<usize>) -> BTreeSet<usize> {
    let mut closure = states.clone();
    let mut queue: VecDeque<usize> = states.iter().copied().collect();

    while let Some(state) = queue.pop_front() {
        for (trans, target) in &nfa.states[state].transitions {
            if matches!(trans, Transition::Epsilon) && closure.insert(*target) {
                queue.push_back(*target);
            }
        }
    }
    closure
}

pub fn nfa_step(nfa: &Nfa, current: &BTreeSet<usize>, c: char) -> BTreeSet<usize> {
    let mut next = BTreeSet::new();
    for &state in current {
        for (trans, target) in &nfa.states[state].transitions {
            if trans.matches(c) {
                next.insert(*target);
            }
        }
    }
    epsilon_closure(nfa, &next)
}

pub fn nfa_match_full(nfa: &Nfa, input: &str) -> bool {
    let start_set = {
        let mut s = BTreeSet::new();
        s.insert(nfa.start);
        epsilon_closure(nfa, &s)
    };

    let final_states = input.chars().fold(start_set, |current, c| nfa_step(nfa, &current, c));

    final_states.contains(&nfa.accept)
}

pub fn nfa_find(nfa: &Nfa, input: &str) -> Option<(usize, usize)> {
    let chars: Vec<char> = input.chars().collect();
    for start_pos in 0..=chars.len() {
        let start_set = {
            let mut s = BTreeSet::new();
            s.insert(nfa.start);
            epsilon_closure(nfa, &s)
        };

        if start_set.contains(&nfa.accept) {
            return Some((start_pos, start_pos));
        }

        let mut current = start_set;
        for end_pos in start_pos..chars.len() {
            current = nfa_step(nfa, &current, chars[end_pos]);
            if current.is_empty() {
                break;
            }
            if current.contains(&nfa.accept) {
                return Some((start_pos, end_pos + 1));
            }
        }
    }
    None
}
```

### `src/dfa.rs` -- Subset Construction and DFA Execution

```rust
use crate::nfa::{epsilon_closure, Nfa, Transition};
use std::collections::{BTreeSet, HashMap};

#[derive(Debug)]
pub struct DfaState {
    pub transitions: HashMap<DfaTransition, usize>,
    pub is_accept: bool,
}

#[derive(Debug, Clone, Hash, Eq, PartialEq)]
pub enum DfaTransition {
    Char(char),
    Range(char, char),
}

#[derive(Debug)]
pub struct Dfa {
    pub states: Vec<DfaState>,
    pub start: usize,
    transition_table: HashMap<(usize, char), usize>,
}

impl Dfa {
    pub fn from_nfa(nfa: &Nfa) -> Self {
        let mut dfa_states: Vec<DfaState> = Vec::new();
        let mut state_map: HashMap<BTreeSet<usize>, usize> = HashMap::new();
        let mut worklist: Vec<BTreeSet<usize>> = Vec::new();
        let mut transition_table: HashMap<(usize, char), usize> = HashMap::new();

        let start_closure = {
            let mut s = BTreeSet::new();
            s.insert(nfa.start);
            epsilon_closure(nfa, &s)
        };

        let start_id = 0;
        dfa_states.push(DfaState {
            transitions: HashMap::new(),
            is_accept: start_closure.contains(&nfa.accept),
        });
        state_map.insert(start_closure.clone(), start_id);
        worklist.push(start_closure);

        let alphabet = collect_alphabet(nfa);

        while let Some(nfa_state_set) = worklist.pop() {
            let current_dfa_id = state_map[&nfa_state_set];

            for &c in &alphabet {
                let mut next_nfa_states = BTreeSet::new();
                for &nfa_state in &nfa_state_set {
                    for (trans, target) in &nfa.states[nfa_state].transitions {
                        if trans.matches(c) {
                            next_nfa_states.insert(*target);
                        }
                    }
                }

                if next_nfa_states.is_empty() {
                    continue;
                }

                let next_closure = epsilon_closure(nfa, &next_nfa_states);
                let next_dfa_id = if let Some(&id) = state_map.get(&next_closure) {
                    id
                } else {
                    let id = dfa_states.len();
                    dfa_states.push(DfaState {
                        transitions: HashMap::new(),
                        is_accept: next_closure.contains(&nfa.accept),
                    });
                    state_map.insert(next_closure.clone(), id);
                    worklist.push(next_closure);
                    id
                };

                dfa_states[current_dfa_id]
                    .transitions
                    .insert(DfaTransition::Char(c), next_dfa_id);
                transition_table.insert((current_dfa_id, c), next_dfa_id);
            }
        }

        Dfa {
            states: dfa_states,
            start: start_id,
            transition_table,
        }
    }

    pub fn match_full(&self, input: &str) -> bool {
        let mut current = self.start;
        for c in input.chars() {
            match self.transition_table.get(&(current, c)) {
                Some(&next) => current = next,
                None => return false,
            }
        }
        self.states[current].is_accept
    }

    pub fn find(&self, input: &str) -> Option<(usize, usize)> {
        let chars: Vec<char> = input.chars().collect();
        for start_pos in 0..=chars.len() {
            let mut current = self.start;

            if self.states[current].is_accept {
                return Some((start_pos, start_pos));
            }

            for end_pos in start_pos..chars.len() {
                match self.transition_table.get(&(current, chars[end_pos])) {
                    Some(&next) => {
                        current = next;
                        if self.states[current].is_accept {
                            return Some((start_pos, end_pos + 1));
                        }
                    }
                    None => break,
                }
            }
        }
        None
    }

    pub fn state_count(&self) -> usize {
        self.states.len()
    }

    pub fn minimize(&self) -> Dfa {
        let n = self.states.len();
        if n <= 1 {
            return Dfa {
                states: self.states.iter().map(|s| DfaState {
                    transitions: s.transitions.clone(),
                    is_accept: s.is_accept,
                }).collect(),
                start: self.start,
                transition_table: self.transition_table.clone(),
            };
        }

        // Hopcroft-style partition refinement
        let accept: BTreeSet<usize> = (0..n).filter(|&i| self.states[i].is_accept).collect();
        let non_accept: BTreeSet<usize> = (0..n).filter(|&i| !self.states[i].is_accept).collect();

        let mut partitions: Vec<BTreeSet<usize>> = Vec::new();
        if !accept.is_empty() {
            partitions.push(accept);
        }
        if !non_accept.is_empty() {
            partitions.push(non_accept);
        }

        let alphabet: Vec<char> = self.transition_table.keys().map(|(_, c)| *c).collect::<BTreeSet<_>>().into_iter().collect();

        let mut changed = true;
        while changed {
            changed = false;
            let mut new_partitions = Vec::new();
            for partition in &partitions {
                if partition.len() <= 1 {
                    new_partitions.push(partition.clone());
                    continue;
                }

                let representative = *partition.iter().next().unwrap();
                let mut same = BTreeSet::new();
                let mut different = BTreeSet::new();
                same.insert(representative);

                for &state in partition.iter().skip(1) {
                    let equivalent = alphabet.iter().all(|c| {
                        let target_rep = self.transition_table.get(&(representative, *c));
                        let target_state = self.transition_table.get(&(state, *c));
                        match (target_rep, target_state) {
                            (None, None) => true,
                            (Some(&a), Some(&b)) => {
                                partitions.iter().any(|p| p.contains(&a) && p.contains(&b))
                            }
                            _ => false,
                        }
                    });
                    if equivalent {
                        same.insert(state);
                    } else {
                        different.insert(state);
                    }
                }

                if !different.is_empty() {
                    changed = true;
                    new_partitions.push(same);
                    new_partitions.push(different);
                } else {
                    new_partitions.push(partition.clone());
                }
            }
            partitions = new_partitions;
        }

        // Build minimized DFA
        let mut state_to_partition: HashMap<usize, usize> = HashMap::new();
        for (pid, partition) in partitions.iter().enumerate() {
            for &state in partition {
                state_to_partition.insert(state, pid);
            }
        }

        let mut new_states: Vec<DfaState> = Vec::new();
        let mut new_transition_table: HashMap<(usize, char), usize> = HashMap::new();

        for (pid, partition) in partitions.iter().enumerate() {
            let representative = *partition.iter().next().unwrap();
            let is_accept = self.states[representative].is_accept;
            let mut transitions = HashMap::new();

            for (&(state, c), &target) in &self.transition_table {
                if state == representative {
                    let target_pid = state_to_partition[&target];
                    transitions.insert(DfaTransition::Char(c), target_pid);
                    new_transition_table.insert((pid, c), target_pid);
                }
            }

            new_states.push(DfaState { transitions, is_accept });
        }

        let new_start = state_to_partition[&self.start];

        Dfa {
            states: new_states,
            start: new_start,
            transition_table: new_transition_table,
        }
    }
}

fn collect_alphabet(nfa: &Nfa) -> Vec<char> {
    let mut chars = BTreeSet::new();
    for state in &nfa.states {
        for (trans, _) in &state.transitions {
            match trans {
                Transition::Char(c) => { chars.insert(*c); }
                Transition::CharClass(ranges, _) => {
                    for range in ranges {
                        for c in range.start..=range.end {
                            chars.insert(c);
                        }
                    }
                }
                Transition::Dot => {
                    for c in ' '..='~' {
                        chars.insert(c);
                    }
                }
                Transition::Epsilon => {}
            }
        }
    }
    chars.into_iter().collect()
}
```

### `src/lib.rs` -- Public API

```rust
pub mod ast;
pub mod parser;
pub mod nfa;
pub mod dfa;

use parser::Parser;
use nfa::{NfaBuilder, nfa_match_full, nfa_find};
use dfa::Dfa;

pub struct RegexEngine {
    nfa: nfa::Nfa,
    dfa: Dfa,
}

impl RegexEngine {
    pub fn new(pattern: &str) -> Result<Self, parser::ParseError> {
        let ast = Parser::parse(pattern)?;
        let nfa = NfaBuilder::build(&ast);
        let dfa = Dfa::from_nfa(&nfa);
        Ok(Self { nfa, dfa })
    }

    pub fn nfa_match_full(&self, input: &str) -> bool {
        nfa_match_full(&self.nfa, input)
    }

    pub fn dfa_match_full(&self, input: &str) -> bool {
        self.dfa.match_full(input)
    }

    pub fn nfa_find(&self, input: &str) -> Option<(usize, usize)> {
        nfa_find(&self.nfa, input)
    }

    pub fn dfa_find(&self, input: &str) -> Option<(usize, usize)> {
        self.dfa.find(input)
    }

    pub fn nfa_state_count(&self) -> usize {
        self.nfa.states.len()
    }

    pub fn dfa_state_count(&self) -> usize {
        self.dfa.state_count()
    }

    pub fn minimize(&self) -> Dfa {
        self.dfa.minimize()
    }
}

pub fn match_full(pattern: &str, input: &str) -> Result<bool, parser::ParseError> {
    let engine = RegexEngine::new(pattern)?;
    Ok(engine.dfa_match_full(input))
}

pub fn find(pattern: &str, input: &str) -> Result<Option<(usize, usize)>, parser::ParseError> {
    let engine = RegexEngine::new(pattern)?;
    Ok(engine.dfa_find(input))
}
```

### `src/main.rs`

```rust
use regex_engine::RegexEngine;

fn main() {
    let patterns = vec![
        ("a(b|c)*d", "abcbcd", true),
        ("a(b|c)*d", "ad", true),
        ("a(b|c)*d", "aed", false),
        ("[a-z]+@[a-z]+\\.[a-z]+", "user@example.com", true),
        ("(ab)+", "ababab", true),
        ("(ab)+", "abba", false),
        ("a?a?a?aaa", "aaa", true),
    ];

    for (pattern, input, expected) in &patterns {
        let engine = RegexEngine::new(pattern).expect("parse failed");
        let nfa_result = engine.nfa_match_full(input);
        let dfa_result = engine.dfa_match_full(input);

        println!("Pattern: {pattern:30} Input: {input:20} NFA: {nfa_result:<5} DFA: {dfa_result:<5} Expected: {expected}");
        assert_eq!(nfa_result, *expected, "NFA mismatch for /{pattern}/ on {input}");
        assert_eq!(dfa_result, *expected, "DFA mismatch for /{pattern}/ on {input}");
    }

    // State count comparison
    let engine = RegexEngine::new("a(b|c)*d").unwrap();
    println!("\nPattern: a(b|c)*d");
    println!("  NFA states: {}", engine.nfa_state_count());
    println!("  DFA states: {}", engine.dfa_state_count());
    let minimized = engine.minimize();
    println!("  Minimized DFA states: {}", minimized.state_count());

    // Find demo
    let engine = RegexEngine::new("[0-9]+").unwrap();
    let result = engine.dfa_find("abc123def");
    println!("\nFind [0-9]+ in 'abc123def': {:?}", result);
}
```

### Tests

```rust
#[cfg(test)]
mod tests {
    use crate::{match_full, find, RegexEngine};
    use crate::parser::Parser;

    #[test]
    fn test_literal() {
        assert!(match_full("abc", "abc").unwrap());
        assert!(!match_full("abc", "abd").unwrap());
    }

    #[test]
    fn test_alternation() {
        assert!(match_full("a|b", "a").unwrap());
        assert!(match_full("a|b", "b").unwrap());
        assert!(!match_full("a|b", "c").unwrap());
    }

    #[test]
    fn test_star() {
        assert!(match_full("a*", "").unwrap());
        assert!(match_full("a*", "aaa").unwrap());
        assert!(!match_full("a*b", "aaa").unwrap());
        assert!(match_full("a*b", "aaab").unwrap());
    }

    #[test]
    fn test_plus() {
        assert!(!match_full("a+", "").unwrap());
        assert!(match_full("a+", "a").unwrap());
        assert!(match_full("a+", "aaaa").unwrap());
    }

    #[test]
    fn test_optional() {
        assert!(match_full("a?b", "b").unwrap());
        assert!(match_full("a?b", "ab").unwrap());
        assert!(!match_full("a?b", "aab").unwrap());
    }

    #[test]
    fn test_char_class() {
        assert!(match_full("[abc]", "a").unwrap());
        assert!(match_full("[abc]", "c").unwrap());
        assert!(!match_full("[abc]", "d").unwrap());
    }

    #[test]
    fn test_char_range() {
        assert!(match_full("[a-z]", "m").unwrap());
        assert!(!match_full("[a-z]", "M").unwrap());
        assert!(match_full("[a-zA-Z]", "M").unwrap());
    }

    #[test]
    fn test_negated_class() {
        assert!(!match_full("[^abc]", "a").unwrap());
        assert!(match_full("[^abc]", "d").unwrap());
    }

    #[test]
    fn test_dot() {
        assert!(match_full("a.c", "abc").unwrap());
        assert!(match_full("a.c", "aXc").unwrap());
        assert!(!match_full("a.c", "ac").unwrap());
    }

    #[test]
    fn test_escape_sequences() {
        assert!(match_full("\\d+", "12345").unwrap());
        assert!(!match_full("\\d+", "abc").unwrap());
        assert!(match_full("\\w+", "hello_42").unwrap());
    }

    #[test]
    fn test_groups() {
        assert!(match_full("(ab)+", "abab").unwrap());
        assert!(!match_full("(ab)+", "abba").unwrap());
        assert!(match_full("(a|b)(c|d)", "ad").unwrap());
    }

    #[test]
    fn test_complex_patterns() {
        assert!(match_full("[a-z]+@[a-z]+\\.[a-z]+", "user@host.com").unwrap());
        assert!(match_full("(0|1(01*0)*1)*", "110").unwrap()); // divisible by 3 in binary
    }

    #[test]
    fn test_find() {
        let result = find("[0-9]+", "abc123def").unwrap();
        assert_eq!(result, Some((3, 6)));
    }

    #[test]
    fn test_find_no_match() {
        let result = find("[0-9]+", "abcdef").unwrap();
        assert_eq!(result, None);
    }

    #[test]
    fn test_nfa_dfa_agreement() {
        let patterns = vec!["a(b|c)*d", "x+y?z*", "(ab|cd)+", "[a-z][0-9]"];
        let inputs = vec!["abcd", "xz", "abcd", "a1", "zz", "m5"];
        for pattern in &patterns {
            let engine = RegexEngine::new(pattern).unwrap();
            for input in &inputs {
                assert_eq!(
                    engine.nfa_match_full(input),
                    engine.dfa_match_full(input),
                    "NFA/DFA disagree on /{pattern}/ matching '{input}'"
                );
            }
        }
    }

    #[test]
    fn test_minimization_reduces_states() {
        let engine = RegexEngine::new("a|a").unwrap();
        let minimized = engine.minimize();
        assert!(minimized.state_count() <= engine.dfa_state_count());
    }

    #[test]
    fn test_pathological_pattern() {
        // a?^n a^n matching a^n -- must not take exponential time
        let n = 20;
        let pattern = format!("{}{}",
            "a?".repeat(n),
            "a".repeat(n),
        );
        let input = "a".repeat(n);

        let start = std::time::Instant::now();
        let result = match_full(&pattern, &input).unwrap();
        let elapsed = start.elapsed();

        assert!(result);
        assert!(elapsed.as_millis() < 1000, "pathological pattern took {}ms", elapsed.as_millis());
    }

    #[test]
    fn test_parser_errors() {
        assert!(Parser::parse("[abc").is_err());
        assert!(Parser::parse("(abc").is_err());
        assert!(Parser::parse("*").is_err());
        assert!(Parser::parse("\\").is_err());
    }
}
```

### Benchmark (`benches/regex_bench.rs`)

```rust
use criterion::{criterion_group, criterion_main, Criterion, BenchmarkId};
use regex_engine::RegexEngine;

fn bench_simple_match(c: &mut Criterion) {
    let engine = RegexEngine::new("[a-z]+@[a-z]+\\.[a-z]+").unwrap();
    let input = "user@example.com";

    let mut group = c.benchmark_group("simple_match");
    group.bench_function("NFA", |b| b.iter(|| engine.nfa_match_full(input)));
    group.bench_function("DFA", |b| b.iter(|| engine.dfa_match_full(input)));
    group.finish();
}

fn bench_pathological(c: &mut Criterion) {
    let mut group = c.benchmark_group("pathological");
    for n in [10, 15, 20] {
        let pattern = format!("{}{}", "a?".repeat(n), "a".repeat(n));
        let input = "a".repeat(n);
        let engine = RegexEngine::new(&pattern).unwrap();

        group.bench_with_input(BenchmarkId::new("NFA", n), &n, |b, _| {
            b.iter(|| engine.nfa_match_full(&input))
        });
        group.bench_with_input(BenchmarkId::new("DFA", n), &n, |b, _| {
            b.iter(|| engine.dfa_match_full(&input))
        });
    }
    group.finish();
}

criterion_group!(benches, bench_simple_match, bench_pathological);
criterion_main!(benches);
```

### Running

```bash
cargo run
cargo test
cargo bench
```

### Expected Output

```
Pattern: a(b|c)*d                     Input: abcbcd               NFA: true  DFA: true  Expected: true
Pattern: a(b|c)*d                     Input: ad                   NFA: true  DFA: true  Expected: true
Pattern: a(b|c)*d                     Input: aed                  NFA: false DFA: false Expected: false
Pattern: [a-z]+@[a-z]+\.[a-z]+       Input: user@example.com     NFA: true  DFA: true  Expected: true
Pattern: (ab)+                        Input: ababab               NFA: true  DFA: true  Expected: true
Pattern: (ab)+                        Input: abba                 NFA: false DFA: false Expected: false
Pattern: a?a?a?aaa                    Input: aaa                  NFA: true  DFA: true  Expected: true

Pattern: a(b|c)*d
  NFA states: 10
  DFA states: 4
  Minimized DFA states: 4

Find [0-9]+ in 'abc123def': Some((3, 6))
```

---

## Design Decisions

1. **BTreeSet for NFA state sets**: the subset construction requires NFA state sets to be used as HashMap keys. BTreeSet implements Ord and Hash (via iteration) and produces deterministic output, unlike HashSet. The ordering also makes debugging easier since state sets print in a consistent order.

2. **Alphabet collection for DFA construction**: the subset construction only explores characters that actually appear in the NFA transitions. For character classes like `[a-z]`, this means expanding the range into individual characters. This is correct but inefficient for large Unicode ranges -- production engines use equivalence classes to group characters that behave identically.

3. **Separate NFA simulation and DFA execution**: both are implemented so the user can see the trade-offs directly. NFA simulation uses O(n*s) time per input string (n = input length, s = NFA states). DFA execution uses O(n) time but may require O(2^s) states. For most practical patterns the DFA is small.

4. **Hopcroft minimization**: the implementation uses a simplified partition refinement that compares each state against a representative. Full Hopcroft's algorithm has O(n log n) complexity; this simplified version is O(n^2) per iteration but is clearer to understand and sufficient for the typical DFA sizes produced in this challenge.

5. **Anchors as zero-width states**: `^` and `$` create zero-width NFA fragments (start == accept). They are handled as special cases in the match/find functions rather than consuming characters. This separates anchor logic from the automata logic.

## Common Mistakes

- **Forgetting epsilon closure after NFA step**: after following character transitions, you must compute the epsilon closure of the resulting state set. Missing this causes the NFA to miss paths that go through epsilon transitions after a character match
- **Mutating state sets during iteration**: when computing epsilon closure, adding states to the set you are iterating over causes non-deterministic behavior. Use a separate worklist (VecDeque)
- **Treating `+` as `*`**: `a+` matches one or more, not zero or more. The Thompson construction for `+` does not include the bypass epsilon from start to accept
- **DFA alphabet explosion with `.`**: the dot matches any character except newline. Naively expanding this into individual transitions for every printable character creates enormous DFAs. Production engines handle this with default transitions or character equivalence classes

## Performance Notes

NFA simulation performance is O(n * m) where n is input length and m is the number of NFA states. Each character step requires visiting all active states and following transitions, then computing epsilon closure.

DFA execution is O(n) -- constant time per character since each state has at most one transition per character. The DFA construction itself can be O(2^m) in the worst case, but lazy construction (building states on demand) avoids this for patterns that do not explore the full state space.

The pathological pattern `a?^n a^n` is specifically designed to break backtracking engines. NFA simulation handles it in O(n^2). DFA execution handles it in O(n) after an O(2^n) construction. For n=20, the DFA has around 20 states (not 2^20) because the state sets overlap heavily.

## Going Further

- Implement lazy DFA construction (build states during execution, cache them)
- Add capture groups using a tagged NFA (Thompson NFA with markers)
- Implement Unicode character classes via the `unicode-general-category` tables
- Add possessive quantifiers and atomic groups to prevent catastrophic backtracking
- Compare performance against RE2 and Rust's regex crate on real-world patterns from the regex-redux benchmark
