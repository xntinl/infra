# 44. Build a Regex Engine

**Difficulty**: Insane

## The Challenge

Regular expressions are one of the most widely used abstractions in computing, yet few programmers understand how they actually work under the hood. Most regex engines in production (Perl, Python, Java, JavaScript) use backtracking, which is simple to implement but has a catastrophic flaw: pathological patterns like `(a?){n}a{n}` take exponential time O(2^n). Ken Thompson showed in 1968 that this is entirely unnecessary — a regex can be converted to a nondeterministic finite automaton (NFA) and simulated in O(n*m) time where n is the pattern length and m is the input length. Russ Cox's seminal article series "Regular Expression Matching Can Be Simple And Fast" demonstrated that Thompson's approach, largely forgotten by the mainstream, can be orders of magnitude faster than backtracking on adversarial inputs.

Your task is to build a complete regex engine from scratch in Rust, implementing the full classical pipeline: parsing regex syntax into an abstract syntax tree, Thompson's construction to convert the AST to an NFA, NFA simulation for guaranteed-linear-time matching, subset construction (the powerset algorithm) to convert the NFA to a DFA for maximum throughput, and Hopcroft's algorithm to minimize the DFA to its smallest equivalent form. You will support the core regex operators — concatenation, alternation (`|`), Kleene star (`*`), one-or-more (`+`), optional (`?`), character classes (`[a-z]`, `[^0-9]`), the dot wildcard (`.`), and anchors (`^`, `$`) — as well as counted repetition (`{n}`, `{n,m}`), escape sequences (`\d`, `\w`, `\s`), and non-greedy quantifiers.

This is not merely an academic exercise. Your engine must be fast enough to benchmark meaningfully against the `regex` crate, which is itself based on Thompson's approach with extensive optimizations. You will measure throughput on realistic workloads (log parsing, DNA sequence matching, email validation) and adversarial workloads (catastrophic backtracking patterns). You will implement multiple matching strategies — NFA simulation, DFA execution, and optionally a bytecode VM — and compare their performance characteristics. Along the way, you will discover why DFAs can explode exponentially in state count, why lazy DFA construction (building states on demand) is the practical solution, and why the `regex` crate's hybrid approach is so effective.

---

## Acceptance Criteria

### Regex Parser
- [ ] Implement a recursive descent parser (or Pratt parser) that converts a regex string into an AST
- [ ] The AST supports: `Literal(char)`, `Concat(Vec<AST>)`, `Alternation(Vec<AST>)`, `Star(AST)`, `Plus(AST)`, `Optional(AST)`, `CharClass(ranges)`, `Dot`, `Anchor(Start|End)`
- [ ] Support character classes: `[abc]`, `[a-z]`, `[^0-9]`, nested ranges like `[a-zA-Z0-9_]`
- [ ] Support escape sequences: `\d` (digits), `\w` (word chars), `\s` (whitespace), `\D`, `\W`, `\S` (negated), `\\`, `\[`, `\(`, etc.
- [ ] Support counted repetition: `a{3}` (exactly 3), `a{2,5}` (2 to 5), `a{3,}` (3 or more)
- [ ] Support non-greedy quantifiers: `*?`, `+?`, `??`, `{n,m}?`
- [ ] Support grouping with parentheses: `(ab|cd)*`
- [ ] Support non-capturing groups: `(?:ab|cd)*`
- [ ] Produce clear error messages for invalid regex: unmatched parentheses, empty alternation branches, invalid escape sequences, invalid repetition ranges
- [ ] Handle operator precedence correctly: Kleene star binds tighter than concatenation, which binds tighter than alternation

### Thompson's NFA Construction
- [ ] Convert the AST to an NFA using Thompson's construction
- [ ] Each AST node produces a fragment with one start state and one accept state
- [ ] `Literal(c)`: two states connected by a transition on character `c`
- [ ] `Concat(a, b)`: connect a's accept state to b's start state via epsilon transition
- [ ] `Alternation(a, b)`: new start state with epsilon transitions to both a and b start states; both accept states connect via epsilon to new accept state
- [ ] `Star(a)`: epsilon from new start to a's start and to new accept; epsilon from a's accept back to a's start and to new accept
- [ ] `Plus(a)` and `Optional(a)`: derived from Star with appropriate epsilon transitions
- [ ] `CharClass(ranges)`: single transition that matches any character in the specified ranges
- [ ] `Dot`: transition that matches any character (or any character except `\n`, configurable)
- [ ] Anchors `^` and `$` are represented as zero-width assertions, not character transitions
- [ ] The resulting NFA has at most 2n states for a regex of length n (Thompson's guarantee)
- [ ] Implement epsilon closure computation: given a set of states, compute all states reachable via epsilon transitions

### NFA Simulation
- [ ] Implement Thompson's NFA simulation: maintain a set of "current states" and advance all of them in parallel on each input character
- [ ] The simulation runs in O(n * m) time where n is the number of NFA states and m is the input length
- [ ] `is_match(input)`: returns true if the entire input matches the regex
- [ ] `find(input)`: returns the leftmost match (start, end) in the input
- [ ] `find_all(input)`: returns all non-overlapping matches
- [ ] Use a bitset (or similar compact representation) for the current state set to avoid allocation per step
- [ ] Demonstrate that the NFA simulation handles pathological patterns in polynomial time: `(a?){100}a{100}` on input `"a".repeat(100)` should complete in milliseconds, not years
- [ ] Implement the anchored vs. unanchored distinction: for unanchored search, add an implicit `.*?` prefix (or start each step by adding the initial state to the current set)

### NFA to DFA: Subset Construction
- [ ] Implement the powerset/subset construction algorithm to convert an NFA to a DFA
- [ ] Each DFA state is a set of NFA states (the epsilon closure of the reachable NFA states)
- [ ] The DFA start state is the epsilon closure of the NFA start state
- [ ] A DFA state is accepting if it contains any NFA accepting state
- [ ] For each DFA state and each possible input character, compute the next DFA state by: taking all NFA transitions from the current state set on that character, then computing the epsilon closure
- [ ] Use a hash map from state sets to DFA state IDs to avoid creating duplicate states
- [ ] Detect and handle DFA state explosion: if the DFA exceeds a configurable state limit (e.g., 10,000 states), abort and fall back to NFA simulation
- [ ] The resulting DFA is a `Vec<[Option<StateId>; 256]>` or similar transition table indexed by byte value
- [ ] Implement a dead state: if a DFA state has no outgoing transitions on any character (or all transitions lead to the dead state), mark it so the matcher can short-circuit and stop scanning
- [ ] Log the number of DFA states generated and the time taken for construction, to help users understand when subset construction is too expensive for their pattern

### DFA Minimization (Hopcroft's Algorithm)
- [ ] Implement Hopcroft's DFA minimization algorithm
- [ ] Initial partition: accepting states and non-accepting states
- [ ] Iteratively refine partitions by splitting groups whose members transition to different groups on some input
- [ ] Use a worklist of (group, character) pairs to drive refinement
- [ ] The algorithm runs in O(n * k * log n) time where n is the number of DFA states and k is the alphabet size
- [ ] Verify that the minimized DFA accepts exactly the same language as the original DFA
- [ ] Output the minimized DFA as a new transition table with potentially far fewer states
- [ ] Demonstrate on concrete examples: `(a|b)*a(a|b)` should minimize to a small DFA (4 states)

### Lazy DFA Construction
- [ ] Implement a lazy (on-demand) DFA that constructs states only when they are first visited during matching
- [ ] Use a cache (hash map) of NFA state sets to DFA states, starting with only the initial state
- [ ] When a transition leads to an unknown DFA state, compute it from the NFA and cache it
- [ ] Implement cache eviction: when the cache exceeds a size limit, flush it and continue (the NFA simulation provides the fallback)
- [ ] Benchmark the lazy DFA against the full DFA on inputs that exercise a small fraction of the DFA's state space
- [ ] The lazy DFA should be the default execution engine for large patterns where full DFA construction is impractical
- [ ] Track cache hit rate and report it as a diagnostic metric: a high hit rate means the lazy DFA is working well; a low hit rate suggests the pattern may be better served by pure NFA simulation
- [ ] Implement a "warm-up" mode that pre-populates the lazy DFA cache by running the NFA simulation on a representative sample of input

### Matching Modes and Features
- [ ] Case-insensitive matching: `(?i)` flag or configuration option
- [ ] Multiline mode: `^` and `$` match at line boundaries, not just input boundaries
- [ ] Dot-all mode: `.` matches `\n`
- [ ] `find_all` returns non-overlapping leftmost-longest matches (or leftmost-shortest for non-greedy)
- [ ] Implement submatch extraction (capturing groups) via the NFA simulation with tagged transitions or parallel tracking of group boundaries
- [ ] Support at least the `\b` word boundary assertion
- [ ] Handle Unicode categories (at minimum, `\p{L}` for letters) or document why you scope to ASCII only
- [ ] Implement `replace(input, pattern, replacement)` with backreferences in the replacement string (`$1`, `$2`)
- [ ] Support named capture groups: `(?P<name>...)` and backreferences via `$name`
- [ ] Implement `split(input, pattern)` that splits the input at every match of the pattern

### Performance and Benchmarking
- [ ] Benchmark against the `regex` crate on at least 5 different patterns and inputs
- [ ] Benchmark categories: (a) simple literal match, (b) complex alternation, (c) character class heavy, (d) catastrophic backtracking pattern, (e) real-world log parsing
- [ ] Report throughput in MB/s for each benchmark
- [ ] The NFA simulation should handle adversarial inputs without degradation: `(a?){n}a{n}` for n=100 should be under 10ms
- [ ] The DFA execution should achieve at least 100 MB/s throughput on simple patterns
- [ ] Compare memory usage: NFA (compact), lazy DFA (moderate), full DFA (potentially huge)
- [ ] Profile and optimize hot paths: epsilon closure computation, DFA state lookup, transition table access
- [ ] Implement literal prefix optimization: if the regex starts with a fixed string, use `memchr` or byte-string search to skip to potential match positions before engaging the automaton
- [ ] Benchmark compilation time: how long does it take to parse, build the NFA, and optionally construct the DFA for patterns of various sizes
- [ ] Report the number of NFA states, DFA states (before and after minimization), and lazy DFA cache utilization for each benchmark pattern

### Testing
- [ ] Test each regex operator in isolation: literal, star, plus, optional, alternation, concat
- [ ] Test operator combinations: `(a|b)*abb`, `[a-z]+@[a-z]+\.[a-z]{2,4}`, `\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}`
- [ ] Test edge cases: empty regex, empty input, regex that matches empty string (`a*`), anchored patterns
- [ ] Test character class edge cases: `[]]` (closing bracket as member), `[-]` (hyphen as member), `[^]` (negated empty class)
- [ ] Compare results against the `regex` crate on 1,000 randomly generated regex/input pairs
- [ ] Test that NFA simulation and DFA execution produce identical results on all test cases
- [ ] Test the minimized DFA produces identical results to the unminimized DFA
- [ ] Fuzz the parser with random byte sequences to ensure it never panics (returns errors gracefully)
- [ ] Test Unicode handling: at minimum, multibyte UTF-8 characters as literals
- [ ] Test submatch extraction: verify that capturing groups return correct start/end positions for nested and alternated groups
- [ ] Test replace functionality: verify backreferences `$1`, `$2` in replacement strings work correctly
- [ ] Test the lazy DFA cache eviction: use a pattern that produces many DFA states, verify that cache eviction does not affect correctness
- [ ] Test with real-world regex patterns: Apache log parsing, email validation (RFC 5322 subset), URL matching, CSV field extraction

---

## Starting Points

These are real resources to study before and during implementation:

1. **Russ Cox - "Regular Expression Matching Can Be Simple And Fast"** (https://swtch.com/~rsc/regexp/regexp1.html) — The single most important resource for this challenge. Explains Thompson's NFA construction and simulation with clear C code. Follow up with parts 2, 3, and 4 of the series for DFA construction, submatch extraction, and the RE2 design.

2. **Ken Thompson - "Regular Expression Search Algorithm"** (Communications of the ACM, 1968) — The original paper. Only 3 pages. Describes the NFA construction using machine code generation on an IBM 7094, which is essentially the same algorithm used today.

3. **The `regex` crate source code** (https://github.com/rust-lang/regex) — Study `regex-automata/src/nfa/thompson/` for the NFA construction, `regex-automata/src/dfa/` for the DFA implementation, and `regex-automata/src/hybrid/` for the lazy DFA. The `regex-syntax` crate handles parsing.

4. **Hopcroft, Motwani, Ullman - "Introduction to Automata Theory, Languages, and Computation"** — Chapter 3 covers NFAs, DFAs, subset construction, and DFA minimization. The textbook treatment is rigorous and includes correctness proofs.

5. **Alfred Aho - "Algorithms for Finding Patterns in Strings"** (Handbook of Theoretical Computer Science, 1990) — Comprehensive survey including Thompson's construction, subset construction, and the relationship between regex, NFAs, and DFAs.

6. **Andrew Gallant (BurntSushi) - "regex-automata" design docs** — The `regex` crate maintainer has written extensively about the design decisions, including why the hybrid NFA/DFA approach was chosen, how the lazy DFA cache works, and the performance characteristics of each strategy.

7. **Hopcroft's Algorithm - "An n log n Algorithm for Minimizing States in a Finite Automaton"** (1971) — The original DFA minimization paper. The algorithm is surprisingly subtle; study the worklist-based formulation carefully.

8. **Cox - "Regular Expression Matching: the Virtual Machine Approach"** (https://swtch.com/~rsc/regexp/regexp2.html) — Describes the Pike VM (used in RE2 and Go's regex) for submatch extraction. Each NFA thread carries its own capture group state, enabling O(n*m) submatch extraction without backtracking.

9. **Owens, Reppy, Turon - "Regular-expression Derivatives Re-examined"** (JFP, 2009) — An alternative approach to regex matching using Brzozowski derivatives. Elegant and concise but less efficient in practice. Worth studying as a contrast to Thompson's approach.

10. **The RE2 source code** (https://github.com/google/re2) — Google's C++ regex library, directly based on Russ Cox's work. Study `re2/nfa.cc` for the NFA simulation, `re2/dfa.cc` for the lazy DFA, and `re2/compile.cc` for the NFA construction.

---

## Hints

1. Start with the parser. Use a recursive descent approach: `parse_alternation()` calls `parse_concat()`, which calls `parse_repetition()`, which calls `parse_atom()`. Each function consumes characters from the input and returns an AST node. Track position with a `usize` index into the pattern bytes. This naturally handles operator precedence.

2. For Thompson's construction, represent the NFA as a list of states where each state has either: (a) a character transition to one other state, (b) an epsilon transition to one or two other states, or (c) is the accept state. Use an arena-style allocation: `Vec<State>` with `StateId = usize`. Build fragments bottom-up from the AST, connecting them by patching "dangling" transitions.

3. The epsilon closure is the most performance-critical part of the NFA simulation. Precompute it: for each state, store the set of states reachable via epsilon transitions. Use a bitset (e.g., `bitvec` crate or a manual `Vec<u64>`) to represent state sets. The epsilon closure of a set of states is the union of the precomputed closures.

4. For the NFA simulation, the algorithm is: (a) `current_states = epsilon_closure({start})`; (b) for each input character c: `next_states = epsilon_closure(union of {transition(s, c) for s in current_states})`; set `current_states = next_states`; (c) after all input, check if any state in `current_states` is accepting. This is the entire algorithm. It is simple but correct.

5. For unanchored search (finding a match anywhere in the input, not just at the start), you have two options: (a) add the start state to `current_states` at every step (simulating the implicit `.*?` prefix), or (b) run the simulation from each starting position. Option (a) is more efficient — it finds the leftmost match in a single pass. To find the leftmost-longest match, track the last position where an accepting state was in `current_states`.

6. Subset construction can produce exponentially many DFA states. The classic worst case is `.*a.{n}` which produces O(2^n) DFA states because the DFA must track all positions where the `a` might have occurred in the last n characters. Set a state limit (e.g., 10,000) and fall back to the NFA simulation if exceeded. Log when this happens to understand which patterns cause blowup.

7. For Hopcroft's minimization, the key optimization is to always pick the smaller of two split groups to add to the worklist. This gives the O(n * k * log n) bound. The naive approach of always processing every group gives O(n^2 * k). Represent partitions using a union-find structure or simply as sets of state IDs.

8. The lazy DFA is the most practically useful execution mode. It combines the throughput of a DFA (one transition table lookup per input byte) with the memory efficiency of the NFA (only states actually encountered are materialized). Implement it as a cache: `HashMap<NFAStateSet, DFAState>` where each `DFAState` contains a partial transition table. On a cache miss, compute the new DFA state from the NFA and insert it.

9. For capturing groups (submatch extraction), the NFA simulation must be extended. Each "thread" (current state) carries its own vector of capture group boundaries. When a thread reaches a capturing group start, it records the current input position; at the group end, it records the end position. When threads merge (at the same NFA state), keep only the one with the leftmost-longest match. This is the Pike VM approach.

10. To benchmark fairly against the `regex` crate, use `criterion` and compile in release mode with LTO. Warm up the regex crate's lazy DFA by running the pattern once before timing. For adversarial inputs, try `(a?){30}a{30}` against a string of 30 `a`s — a backtracking engine takes billions of steps, while your NFA simulation takes ~900 steps (30 states * 30 characters).

11. For the transition table, you have a design choice: (a) dense table `[Option<StateId>; 256]` per state — fast lookup, 2KB per state; (b) sparse representation using a sorted array of `(byte, StateId)` pairs — compact but requires binary search; (c) compressed row storage. For the full DFA, use dense tables. For the lazy DFA, use dense tables but limit the cache size in number of states.

12. Handle the `$` anchor by treating end-of-input as a special "character" in the NFA simulation. When you reach the end of input, check if any current state has a `$` transition. Alternatively, represent `$` as a zero-width assertion that succeeds only when the next character is end-of-input (or `\n` in multiline mode). The same approach works for `^` and `\b`.

13. For counted repetition `a{3,5}`, expand it during AST construction to `aaaa?a?` (three required `a`s followed by two optional `a`s). This is simple and correct but can produce large NFAs for large counts. An optimization: use a single NFA loop with a counter, but this breaks the finite automaton model (it is now a "counting automaton"). For this challenge, expansion is sufficient.

14. Test with these specific patterns known to cause issues in various engines: `(a*)*b` on input `"aaa"` (catastrophic backtracking in naive engines), `(a|aa)*b` on input `"aaa"` (exponential NFA ambiguity), `[a-z]*[a-z]*[a-z]*[a-z]*[a-z]*b` on input `"aaa...a"` (O(n^5) in some engines). Your NFA simulation should handle all of these in linear time.

15. The parser must handle nested quantifiers: `(a*)*` is valid regex (though vacuous). It must also reject invalid constructs: `*a` (quantifier with no operand), `{3}a` (same), `[z-a]` (reversed range), `(abc` (unmatched paren). Use Rust's `Result` type for error handling throughout the parser, with descriptive error types that include the position in the pattern.

16. For a stretch goal, implement a bytecode VM as a third execution mode. Compile the NFA into a sequence of bytecodes: `Byte(c)` (match a byte), `Split(pc1, pc2)` (fork execution), `Jump(pc)` (unconditional jump), `Match` (accept), `Save(slot)` (record position for capturing group). Execute using Thompson's multi-thread simulation or Spencer's backtracking (with a note that the latter has exponential worst case). This is how RE2 and the `regex` crate's PikeVM work internally.

17. When building the epsilon closure, watch out for cycles in the NFA. Epsilon transitions can form loops (e.g., from `(a*)*`). Your closure computation must use a visited set to avoid infinite loops. A simple DFS or BFS with a bitset for visited states handles this correctly. Pre-computing closures at NFA construction time avoids re-discovering cycles at match time.

18. For the DFA transition table, consider alphabet reduction: instead of 256 columns (one per byte value), compute equivalence classes of bytes that behave identically across all NFA states. For example, if a regex only distinguishes between `a`, `b`, and "everything else," the transition table needs only 3 columns. This dramatically reduces memory usage for the full DFA. The `regex` crate uses this technique extensively.

19. Non-greedy quantifiers (`*?`, `+?`) affect match semantics but not the language accepted by the automaton. In the NFA, the difference is the priority of epsilon transitions: greedy prefers to continue matching (the loop branch), while non-greedy prefers to exit (the skip branch). In the DFA, this distinction is lost — DFAs can only report match/no-match, not which match to prefer. Submatch semantics (leftmost-shortest vs leftmost-longest) must be handled at a higher level, typically in the NFA simulation or Pike VM.

20. Consider implementing a `Debug` formatter for each stage of the pipeline: print the AST as an indented tree, print the NFA as a state transition table, print the DFA as a transition table with accepting states marked, and print the minimized DFA. This makes debugging dramatically easier. For the NFA, also implement a DOT graph output that can be visualized with Graphviz — seeing the automaton's structure visually catches many construction bugs immediately.

21. The relationship between regex features and automaton expressiveness is important to understand. True regular expressions (concatenation, alternation, Kleene star) correspond exactly to finite automata. Backreferences (`\1`) make the language context-sensitive and cannot be handled by any finite automaton — they require backtracking. Lookahead and lookbehind are also beyond regular languages. For this challenge, focus on the true regular expression subset and document which features would require a fundamentally different engine.

22. For the `replace` functionality, parse the replacement string separately to extract backreferences (`$1`, `$2`, `$name`). During replacement, run the NFA simulation with submatch tracking to get capture group boundaries, then construct the output by concatenating literal parts of the replacement with the corresponding captured substrings. Handle edge cases: `$0` is the entire match, `$$` is a literal dollar sign, and a reference to a non-existent group is an error.

23. When implementing `find_all`, you need to handle overlapping and empty matches carefully. After finding a match at position [start, end), the next search should start at position `end` (for non-overlapping). But if the match was empty (start == end, e.g., the regex `a*` matching at a position with no `a`), advance by one character to avoid an infinite loop. The `regex` crate handles this by advancing one byte in this case.

24. For a deeper understanding of the DFA minimization result, implement a function that prints the Myhill-Nerode equivalence classes. Two strings x and y are in the same class if for all suffixes z, xz is in the language iff yz is in the language. Each equivalence class corresponds to one state in the minimal DFA. This connects the implementation to the underlying automata theory.

25. Consider implementing a simple optimization pass on the AST before NFA construction: collapse nested alternations (`(a|(b|c))` becomes `(a|b|c)`), remove redundant groups, and sort character class ranges. These optimizations reduce NFA size slightly and simplify later stages. More importantly, they normalize the AST so that equivalent regexes produce identical NFAs, which aids testing.
