# 33. Custom Regex Engine (NFA to DFA)

<!--
difficulty: advanced
category: compilers-automata
languages: [rust]
concepts: [thompson-construction, subset-construction, nfa, dfa, finite-automata, regex-parsing]
estimated_time: 10-14 hours
bloom_level: evaluate
prerequisites: [graph-theory-basics, set-operations, enums-pattern-matching, recursion, iterators]
-->

## Languages

- Rust (1.75+ stable)

## Prerequisites

- Graph theory: directed graphs, traversal, reachability
- Set operations: union, intersection, membership testing
- Recursive descent parsing or any top-down parsing technique
- Rust enums, pattern matching, HashSet, HashMap, BTreeSet
- Understanding of what regular expressions match (not just how to use them)

## Learning Objectives

- **Implement** Thompson's construction to convert a parsed regex AST into an NFA with epsilon transitions
- **Implement** the subset construction algorithm to convert an NFA to an equivalent DFA
- **Analyze** the trade-offs between NFA simulation (time per character proportional to states) and DFA execution (constant time per character, exponential state space)
- **Evaluate** the performance characteristics of your engine against Rust's `regex` crate on pathological inputs
- **Design** a regex AST that cleanly separates parsing from compilation

## The Challenge

Most programmers use regular expressions daily without understanding the machinery underneath. The theoretical foundation is finite automata: every regular expression corresponds to a Nondeterministic Finite Automaton (NFA), and every NFA can be converted to a Deterministic Finite Automaton (DFA) that processes input in constant time per character.

The standard construction pipeline is: parse the regex string into an AST, convert the AST to an NFA using Thompson's construction, then optionally convert the NFA to a DFA using the subset construction (also called the powerset construction). NFA simulation follows all possible paths simultaneously. DFA execution follows exactly one path but may require exponentially more states.

This is how production regex engines like RE2 and Rust's `regex` crate guarantee linear-time matching -- they avoid the backtracking trap that makes naive engines vulnerable to ReDoS (Regular expression Denial of Service).

Build a regex engine that implements both NFA simulation and DFA execution. The engine must parse a regex string, construct the automaton, and match input strings. You must benchmark both approaches and demonstrate when each is superior.

## Requirements

1. Implement a regex parser that produces an AST. Supported syntax: concatenation (implicit), alternation (`|`), Kleene star (`*`), one-or-more (`+`), optional (`?`), character classes (`[a-z]`, `[^abc]`), wildcard (`.`), anchors (`^`, `$`), grouping with parentheses, and escaped characters (`\d`, `\w`, `\s`, `\.`)
2. Implement Thompson's construction: convert the AST into an NFA. Each NFA state has zero or more epsilon transitions and zero or more character transitions. The resulting NFA has exactly one start state and one accept state
3. Implement NFA simulation: given an input string, track all possible active states simultaneously using epsilon-closure. The input matches if any active state after consuming all characters is an accept state
4. Implement the subset construction (powerset construction): convert the NFA into a DFA where each DFA state is a set of NFA states. The DFA has no epsilon transitions and exactly one transition per character per state
5. Implement DFA execution: process one character at a time following the single deterministic transition. The input matches if the final state is an accept state
6. Implement DFA state minimization using Hopcroft's algorithm or partition refinement
7. Character classes must be handled efficiently -- do not create one NFA state per character in a class. Use character ranges
8. Provide a `match_full(pattern, input) -> bool` function (entire string must match) and a `find(pattern, input) -> Option<(usize, usize)>` function (find first match position)
9. The parser must produce clear error messages for malformed regex patterns with the position of the error
10. Write benchmarks comparing your NFA simulation, your DFA execution, and Rust's `regex` crate on: simple patterns, patterns with many alternations, and pathological patterns like `a?^n a^n` matching `a^n`

## Hints

<details>
<summary>Hint 1: Parser grammar and precedence</summary>

Start with the parser. A regex is a small language and a recursive descent parser handles it well. Define the grammar with proper precedence, from lowest to highest:

1. **Alternation** (`|`): `expr = concat ('|' concat)*`
2. **Concatenation** (implicit): `concat = quantifier+`
3. **Quantifiers** (`*`, `+`, `?`): `quantifier = atom ('*' | '+' | '?')?`
4. **Atoms**: literals, character classes, groups, escapes

Each precedence level is a function that calls the next level. This ensures that `ab|cd` parses as `(ab)|(cd)` and `ab*` parses as `a(b*)`.

```rust
fn parse_alternation(&mut self) -> Result<Regex, Error> {
    let mut left = self.parse_concat()?;
    while self.peek() == Some('|') {
        self.advance();
        let right = self.parse_concat()?;
        left = Regex::Alternation(Box::new(left), Box::new(right));
    }
    Ok(left)
}
```
</details>

<details>
<summary>Hint 2: Thompson's construction fragments</summary>

Each regex AST node produces a small NFA fragment with exactly one start state and one accept state. The construction rules are:

- **Literal `c`**: start --c--> accept (one character transition)
- **Concatenation `AB`**: start_A ... accept_A --epsilon--> start_B ... accept_B
- **Alternation `A|B`**: new_start --epsilon--> start_A and start_B; accept_A and accept_B --epsilon--> new_accept
- **Kleene star `A*`**: new_start --epsilon--> start_A and new_accept; accept_A --epsilon--> start_A and new_accept

The key property: every fragment has exactly one start and one accept. This makes composition trivial. The resulting NFA may have many epsilon transitions but is always correct.
</details>

<details>
<summary>Hint 3: Epsilon closure is the core NFA operation</summary>

NFA simulation tracks a set of "active" states. After consuming each input character, you:
1. For each active state, follow all character transitions that match the input character
2. Compute the epsilon closure of the resulting states (follow all epsilon transitions recursively)

The epsilon closure is a BFS/DFS from a set of states, following only epsilon transitions, collecting all reachable states. This must be computed after every character step AND at the start (from the initial state).

Use `BTreeSet<usize>` for state sets: it implements `Ord` (needed as HashMap keys in the DFA builder), is deterministic, and makes debugging easier since states print in order.
</details>

<details>
<summary>Hint 4: Subset construction algorithm</summary>

The subset construction converts the NFA to a DFA. Each DFA state is a set of NFA states. The algorithm:

1. Start with the epsilon closure of the NFA start state -- this is DFA state 0
2. For each DFA state (set of NFA states), for each character in the alphabet:
   a. Compute the set of NFA states reachable by following character transitions from any state in the set
   b. Compute the epsilon closure of that set
   c. If this resulting set is new, create a new DFA state for it
   d. Add a DFA transition from the current state to the new state on that character
3. A DFA state is accepting if any NFA state in its set is the NFA accept state

The exponential blowup is theoretical: most practical patterns produce DFAs with far fewer states than the theoretical 2^N maximum. Lazy construction (build DFA states on demand during matching) avoids the blowup entirely for patterns that never explore the full state space.
</details>

<details>
<summary>Hint 5: DFA minimization via partition refinement</summary>

Hopcroft's algorithm minimizes a DFA by merging equivalent states. Two states are equivalent if, for every input string, they either both accept or both reject. The algorithm:

1. Start with two partitions: accepting states and non-accepting states
2. Repeat until no partition changes:
   - For each partition, check if all states in it behave identically (same transitions lead to the same partition)
   - If not, split the partition into groups that behave identically
3. Each final partition becomes one state in the minimized DFA

For patterns like `a|a`, the NFA produces distinct paths that create distinct DFA states. Minimization merges them into one, proving they were equivalent.
</details>

<details>
<summary>Hint 6: Benchmarking strategy</summary>

The pathological pattern `a?` repeated N times followed by `a` repeated N times, matched against N `a`s, is the classic test:

- **Backtracking engines** (PCRE, Java's Pattern): O(2^N) -- catastrophic backtracking
- **NFA simulation**: O(N * M) where M is the number of NFA states -- polynomial
- **DFA execution**: O(N) per input character after construction -- linear

Use Criterion for benchmarks. Test N = 10, 15, 20. Your NFA should handle N=20 in milliseconds. A backtracking engine would take hours.

Also benchmark common patterns on realistic input (email-like patterns, log line parsing) to show the absolute performance difference between NFA simulation and DFA execution.
</details>

## Acceptance Criteria

- [ ] Parser handles all required syntax elements and rejects invalid patterns with position-aware errors
- [ ] Thompson's construction produces correct NFAs (verify by checking NFA simulation results against expected matches)
- [ ] NFA simulation correctly matches and rejects strings for all supported syntax
- [ ] Subset construction produces a DFA that matches the same language as the NFA
- [ ] DFA minimization reduces state count (verify on patterns like `a|a` or `(ab|ab)`)
- [ ] `match_full` correctly matches entire strings
- [ ] `find` correctly locates the first match position in a string
- [ ] Character classes `[a-z]`, `[^0-9]`, `\d`, `\w`, `\s` all work correctly
- [ ] Anchors `^` and `$` work correctly in `find` mode
- [ ] Pathological pattern `a?^{20} a^{20}` matches `a^{20}` in under 1 millisecond
- [ ] Benchmarks show DFA is faster per character but uses more memory than NFA simulation
- [ ] No panics on any valid or invalid input

## Research Resources

- [Regular Expression Matching Can Be Simple And Fast](https://swtch.com/~rsc/regexp/regexp1.html) -- Russ Cox's seminal article on Thompson NFA vs backtracking, with C code and benchmarks. The foundational reference for this challenge
- [Russ Cox: Regular Expression Matching: the Virtual Machine Approach](https://swtch.com/~rsc/regexp/regexp2.html) -- second article covering Pike's VM approach, used by Go's regexp and Rust's regex
- [Thompson's Construction (Wikipedia)](https://en.wikipedia.org/wiki/Thompson%27s_construction) -- formal description of the NFA construction algorithm with diagrams
- [Subset Construction (Wikipedia)](https://en.wikipedia.org/wiki/Powerset_construction) -- the powerset construction algorithm for NFA-to-DFA conversion
- [Hopcroft's Algorithm (Wikipedia)](https://en.wikipedia.org/wiki/DFA_minimization#Hopcroft's_algorithm) -- DFA state minimization algorithm
- [Rust regex crate source](https://github.com/rust-lang/regex) -- production implementation; study the `regex-automata` sub-crate for NFA and DFA compilation
- [Introduction to Automata Theory (Hopcroft, Motwani, Ullman)](https://www-2.dc.uba.ar/staff/becher/Hopcroft-Motwani-Ullman-2741.pdf) -- the textbook treatment of formal language theory and automata
