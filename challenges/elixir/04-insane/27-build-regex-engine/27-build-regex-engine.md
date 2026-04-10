# 27. Build a Regex Engine

**Difficulty**: Insane

---

## Prerequisites

- Automata theory: NFA, DFA, subset construction
- Graph algorithms: BFS, DFS, state minimization
- Elixir binary pattern matching and bitstring manipulation
- Understanding of formal language theory (regular languages, Kleene closure)
- Recursive descent parsing for regex syntax
- Complexity analysis: why backtracking engines have exponential worst cases

---

## Problem Statement

Build a regex engine from scratch that compiles regular expressions to minimized DFAs and matches them against binary input without backtracking. The engine must:

1. Parse a regular expression string into an AST that represents the regex structure
2. Convert the AST to an NFA using Thompson's construction algorithm
3. Convert the NFA to an equivalent DFA using the subset construction (powerset) algorithm
4. Minimize the DFA using Hopcroft's partition-refinement algorithm
5. Execute the minimized DFA against input strings in O(n) time relative to input length, with no backtracking
6. Support a practical set of character classes, quantifiers, capturing groups, and anchors
7. Match performance on the regex-redux benchmark competitive with standard backtracking engines on non-adversarial input

---

## Acceptance Criteria

- [ ] Thompson construction: converts a parsed regex AST to an NFA with epsilon transitions; each operator (concatenation, alternation, Kleene star) maps to a known NFA fragment
- [ ] NFA-to-DFA subset construction: converts the NFA to an equivalent DFA by tracking sets of NFA states as single DFA states; handles epsilon closures correctly
- [ ] Hopcroft DFA minimization: implements the partition-refinement algorithm; the resulting DFA has the minimum number of states for the accepted language
- [ ] Character classes: `[a-z]`, `[A-Z0-9_]`, `.` (any except newline), `\d` (digits), `\w` (word chars), `\s` (whitespace), and their negations (`\D`, `\W`, `\S`, `[^...]`)
- [ ] Quantifiers: `*` (zero or more), `+` (one or more), `?` (zero or one), `{n}` (exactly n), `{n,m}` (between n and m); both greedy and lazy (`*?`, `+?`, `??`) variants
- [ ] Alternation: `a|b` matches either `a` or `b`; correctly handles `(cat|dog)s?`
- [ ] Capturing groups: `(pattern)` records the start and end index of the matched substring; groups are numbered left-to-right by their opening parenthesis; non-capturing groups `(?:pattern)` are supported
- [ ] Anchors: `^` matches start of string (or line in multiline mode), `$` matches end of string (or line), `\b` matches a word boundary
- [ ] Benchmark: compile and execute the regex-redux benchmark suite; all patterns produce correct results; total runtime is within 3x of the Erlang built-in `:re` module on the same input

---

## What You Will Learn

- Thompson's construction: translating regex operators to NFA fragments
- Subset construction: why DFA states are sets of NFA states
- Hopcroft's algorithm: partition-based minimization with O(n log n) complexity
- Why NFA/DFA engines have guaranteed linear time while backtracking engines do not
- Elixir binary matching for efficient character-by-character DFA execution
- How capturing groups require augmenting the NFA with position-tracking states
- The trade-off between compile time (DFA construction) and match time (linear execution)

---

## Hints

- Read Russ Cox's essay "Regular Expression Matching Can Be Simple And Fast" before starting — it explains Thompson's construction clearly with diagrams
- Thompson's 1968 paper is short and readable; the NFA-to-DFA construction is in any automata theory textbook
- Research why lazy quantifiers require a modification to the subset construction (they change state priority)
- Anchors require the DFA to know whether it is at a string boundary — investigate how to encode this as input symbols
- Capturing groups break the pure NFA/DFA model — research "tagged NFA" (TNFA) or "submatch extraction"
- Look into the regex-redux benchmark at benchmarksgame.alioth.debian.org for your test suite

---

## Reference Material

- "Regular Expression Matching Can Be Simple And Fast" — Russ Cox (swtch.com)
- "Regular Expression Search Algorithm" — Ken Thompson, CACM 1968
- "Introduction to Automata Theory, Languages, and Computation" — Hopcroft, Motwani & Ullman
- "Regular Expression Matching with a Trigram Index" — Russ Cox
- Benchmark: benchmarksgame-team.pages.debian.net/benchmarksgame (regex-redux)

---

## Difficulty Rating ★★★★★★

Building a complete NFA→DFA→minimized pipeline with capturing groups, character classes, and lazy quantifiers requires rigorous automata theory applied in a practical language context.

---

## Estimated Time

50–80 hours
