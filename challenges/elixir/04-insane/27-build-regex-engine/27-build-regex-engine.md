# Regex Engine

**Project**: `rexa` — a regex engine with Thompson NFA→DFA→minimized pipeline and O(n) matching

---

## Project context

You are building `rexa`, a regex engine that compiles regular expressions to minimized DFAs and matches them against binary input in O(n) time with no backtracking. The engine implements the full pipeline: parse the regex into an AST, convert to an NFA via Thompson's construction, determinize via subset construction, minimize via Hopcroft's algorithm, then execute the DFA character by character. Capturing groups are tracked via tagged transitions.

Project structure:

```
rexa/
├── lib/
│   └── rexa/
│       ├── application.ex           # optional supervision
│       ├── parser.ex                # regex AST: concat, alt, star, plus, opt, group, char_class
│       ├── nfa.ex                   # Thompson construction: AST → NFA with ε-transitions
│       ├── dfa.ex                   # subset construction: NFA → DFA (state = MapSet of NFA states)
│       ├── minimizer.ex             # Hopcroft partition refinement: DFA → minimal DFA
│       ├── executor.ex              # DFA execution: input × DFA → match result with captures
│       ├── char_class.ex            # character class evaluation: [a-z], \d, \w, \s, negations
│       └── regex.ex                 # public API: compile/1, match/2, scan/2, named_captures/2
├── test/
│   └── rexa/
│       ├── parser_test.exs          # AST structure, operator precedence, error positions
│       ├── nfa_test.exs             # epsilon closure correctness, NFA state count
│       ├── dfa_test.exs             # subset construction, equivalent acceptance languages
│       ├── minimizer_test.exs       # state count reduced, language equivalence preserved
│       ├── executor_test.exs        # match results, capture indices, anchors
│       └── performance_test.exs    # linear time on adversarial input, no backtracking blowup
├── bench/
│   └── rexa_bench.exs
└── mix.exs
```

---

## The problem

Most production regex engines (PCRE, Java, Python `re`) use backtracking NFA simulation. On adversarial input like matching `(a+)+` against `aaaab`, backtracking engines exhibit exponential time complexity — doubling the input length squares the runtime. The NFA/DFA approach guarantees O(n) match time because the DFA processes each character exactly once, with no backtracking. The cost is compile time: constructing and minimizing the DFA is O(2^S) in the worst case for S NFA states, but typical patterns produce compact DFAs.

---

## Why this design

**Thompson's construction produces structured NFA fragments**: each regex operator maps to a small NFA fragment with exactly one entry and one exit state. Concatenation wires the exit of the left fragment to the entry of the right. This structural property makes the construction straightforward to implement recursively from the AST.

**Subset construction converts NFA to DFA**: a DFA state is a set of NFA states (hence "subset construction"). The initial DFA state is the ε-closure of the NFA start state. For each DFA state and each input character, the next DFA state is the ε-closure of all NFA states reachable on that character. DFA states are interned by their NFA-state-set identity.

**Hopcroft's minimization reduces DFA size**: many DFAs produced by subset construction have redundant states — states that are distinguishable from no other state. Hopcroft's algorithm partitions states into equivalence classes, merging indistinguishable ones. The result is the unique minimal DFA for the language.

**Tagged transitions for capturing groups**: pure DFAs cannot record which substrings matched capture groups. Tagged NFAs annotate ε-transitions with "open group N" and "close group N" tags. During execution, the engine tracks a position record alongside the current state, recording tag positions as they are traversed.

---

## Implementation milestones

### Step 1: Create the project

```bash
mix new rexa --sup
cd rexa
mkdir -p lib/rexa test/rexa bench
```

### Step 2: `mix.exs` — dependencies

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Regex parser

```elixir
# lib/rexa/parser.ex
defmodule Rexa.Parser do
  @moduledoc """
  Parses a regex string into an AST.

  Grammar (simplified):
    expr    := alt
    alt     := concat ('|' concat)*
    concat  := quantified+
    quantified := atom quantifier?
    quantifier := '*' | '+' | '?' | '*?' | '+?' | '??' | '{n}' | '{n,m}'
    atom    := '(' expr ')'           -- capturing group
             | '(?:' expr ')'         -- non-capturing group
             | '[' char_class ']'     -- character class
             | '.'                    -- any (except newline)
             | '\' special            -- escape: \d, \w, \s, \D, \W, \S, \b
             | char

  AST node types:
    {:concat, [nodes]}
    {:alt, [nodes]}
    {:star, node, :greedy | :lazy}
    {:plus, node, :greedy | :lazy}
    {:opt,  node, :greedy | :lazy}
    {:repeat, node, n, m, :greedy | :lazy}
    {:group, group_num, node}
    {:non_capture_group, node}
    {:char, codepoint}
    {:char_class, ranges, negated?}
    {:any}
    {:anchor, :start | :end | :word_boundary}
  """

  def parse(pattern) when is_binary(pattern) do
    # TODO: recursive descent; track group counter for numbering
    # TODO: return {:ok, ast} or {:error, {col, message}}
    # HINT: convert pattern to charlist for positional tracking
    # HINT: parse_alt → parse_concat → parse_quantified → parse_atom
  end
end
```

### Step 4: Thompson NFA construction

```elixir
# lib/rexa/nfa.ex
defmodule Rexa.NFA do
  @moduledoc """
  Thompson's construction: regex AST → NFA.

  NFA representation:
    %{
      start:  state_id,
      accept: state_id,
      transitions: %{state_id => [{:char, c, to} | {:epsilon, to} | {:class, pred, to} | {:tag, tag, to}]}
    }

  Thompson fragments:
    char c:     start --c--> accept
    concat A·B: A.accept --ε--> B.start
    alt A|B:    new_start --ε--> A.start
                new_start --ε--> B.start
                A.accept  --ε--> new_accept
                B.accept  --ε--> new_accept
    star A*:    new_start --ε--> A.start, new_accept
                A.accept  --ε--> A.start, new_accept
  """

  def build(ast) do
    # TODO: recursive build/2 with a state counter (use :counters or pass integer)
    # TODO: return an NFA struct
    # HINT: each fragment returns {nfa, new_counter}
    # HINT: merge transition maps with Map.merge/3 when wiring fragments together
  end

  def epsilon_closure(nfa, states) when is_list(states) do
    # TODO: BFS/DFS from `states` following only :epsilon transitions
    # TODO: return MapSet of all reachable states (including start states)
  end
end
```

### Step 5: Subset construction (NFA → DFA)

```elixir
# lib/rexa/dfa.ex
defmodule Rexa.DFA do
  @moduledoc """
  Subset construction: NFA → DFA.

  A DFA state is a MapSet of NFA states.
  The DFA start state is epsilon_closure(nfa, [nfa.start]).
  For each DFA state S and each possible input character c:
    next = epsilon_closure(nfa, states_reachable_on_c_from_S)
  DFA states are memoized by their NFA-state-set (MapSet).
  A DFA state is accepting if it contains nfa.accept.

  Returns:
    %{
      start:       dfa_state_id,
      accept:      MapSet.t(dfa_state_id),
      transitions: %{dfa_state_id => %{char_or_class => dfa_state_id}},
      alphabet:    MapSet.t(char_or_class)
    }
  """

  def build(nfa) do
    # TODO: worklist algorithm; begin with {start: epsilon_closure(nfa, [nfa.start])}
    # TODO: for each unmarked DFA state, compute transitions on all alphabet symbols
    # TODO: intern DFA states by their NFA-state-set identity using a Map
    # HINT: the alphabet must include all characters referenced in the NFA transitions
    # HINT: for character classes, represent the transition key as {:class, pred}
  end
end
```

### Step 6: Hopcroft DFA minimization

```elixir
# lib/rexa/minimizer.ex
defmodule Rexa.Minimizer do
  @moduledoc """
  Hopcroft's partition refinement algorithm.

  Initial partition: {accepting states, non-accepting states}
  Refinement: for each partition block B and each input symbol c,
    split any block P into:
      P ∩ { states that transition on c into B }
      P \\ { states that transition on c into B }
  Repeat until no block is split.
  Each final block becomes one state in the minimal DFA.
  """

  def minimize(dfa) do
    # TODO: initial partition
    # TODO: worklist of (block, symbol) pairs to check
    # TODO: merge states within same block; pick representative per block
    # TODO: remap transitions to use block representatives
    # TODO: return a new DFA struct with minimized states
  end
end
```

### Step 7: DFA executor

```elixir
# lib/rexa/executor.ex
defmodule Rexa.Executor do
  @moduledoc """
  Executes a minimized DFA against binary input.

  Match semantics:
    - match/3: attempt to match at a given offset; returns {:match, captures} or :no_match
    - scan/2: find all non-overlapping matches in the input

  Captures: a list of {start_byte, end_byte} tuples, one per group (group 0 = whole match).

  Execution: process input bytes one at a time, following DFA transitions.
  If no transition exists for the current byte: :no_match.
  If the DFA reaches an accepting state: record the match position.
  """

  def match(dfa, input, offset \\ 0) when is_binary(input) do
    # TODO: walk input from offset, byte by byte, following transitions
    # TODO: track current state, update capture positions on tag transitions
    # TODO: return {:match, [{start, len}]} on acceptance or :no_match
  end

  def scan(dfa, input) when is_binary(input) do
    # TODO: advance offset past each match; collect all results
  end
end
```

### Step 8: Given tests — must pass without modification

```elixir
# test/rexa/performance_test.exs
defmodule Rexa.PerformanceTest do
  use ExUnit.Case, async: true

  test "adversarial input does not cause exponential blowup" do
    # Pattern (a+)+ is catastrophic for backtracking engines on "aaaa...b"
    {:ok, dfa} = Rexa.compile("(a+)+b")
    input = String.duplicate("a", 30) <> "b"

    {time_us, result} = :timer.tc(fn -> Rexa.match(dfa, input) end)

    assert {:match, _} = result
    # Must complete well under 1 second; backtracking would take minutes
    assert time_us < 100_000, "match took #{time_us}μs — possible exponential blowup"
  end

  test "linear time scaling with input length" do
    {:ok, dfa} = Rexa.compile("a*b")

    times = for n <- [100, 1_000, 10_000] do
      input = String.duplicate("a", n) <> "b"
      {t, _} = :timer.tc(fn -> Rexa.match(dfa, input) end)
      t
    end

    [t1, t2, t3] = times
    # Each 10x input increase should be < 20x time increase (allowing generous constant factor)
    assert t2 < t1 * 20, "scaling from 100→1000 chars: #{t1}μs → #{t2}μs (expected ~10x)"
    assert t3 < t2 * 20, "scaling from 1000→10000 chars: #{t2}μs → #{t3}μs (expected ~10x)"
  end
end
```

```elixir
# test/rexa/executor_test.exs
defmodule Rexa.ExecutorTest do
  use ExUnit.Case, async: true

  test "basic literal match" do
    {:ok, dfa} = Rexa.compile("hello")
    assert {:match, [{0, 5}]} = Rexa.match(dfa, "hello world")
  end

  test "alternation" do
    {:ok, dfa} = Rexa.compile("cat|dog")
    assert {:match, _} = Rexa.match(dfa, "my cat")
    assert {:match, _} = Rexa.match(dfa, "my dog")
    assert :no_match   = Rexa.match(dfa, "my fish")
  end

  test "capturing group returns correct byte offsets" do
    {:ok, dfa} = Rexa.compile("(\\d+)-(\\d+)")
    {:match, captures} = Rexa.match(dfa, "order 12-345 here")
    # captures: [{whole_start, whole_len}, {group1_start, group1_len}, {group2_start, group2_len}]
    assert length(captures) == 3
  end

  test "character class [a-z]+" do
    {:ok, dfa} = Rexa.compile("[a-z]+")
    {:match, [{start, len}]} = Rexa.match(dfa, "123abc456")
    assert binary_part("123abc456", start, len) == "abc"
  end
end
```

### Step 9: Run the tests

```bash
mix test test/rexa/ --trace
```

### Step 10: Benchmark

```elixir
# bench/rexa_bench.exs
alias Rexa

patterns = [
  {"simple literal",    "hello world"},
  {"alternation",       "cat|dog|fish|bird"},
  {"char class repeat", "[a-zA-Z0-9_]+@[a-z]+\\.[a-z]{2,4}"},
  {"groups",            "(\\d{1,3}\\.){3}\\d{1,3}"}  # IPv4-like
]

compiled = for {label, pat} <- patterns do
  {:ok, dfa} = Rexa.compile(pat)
  {label, dfa}
end

input = String.duplicate("test hello world user@example.com 192.168.1.1 end ", 1_000)

Benchee.run(
  Map.new(compiled, fn {label, dfa} ->
    {label, fn -> Rexa.scan(dfa, input) end}
  end)
  |> Map.put("erlang :re scan (baseline)", fn ->
    :re.run(input, "hello", [:global])
  end),
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

---

## Trade-off analysis

| Aspect | NFA/DFA (this impl) | Backtracking NFA (PCRE, re2) | Erlang `:re` (PCRE) |
|--------|-------------------|------------------------------|---------------------|
| Match time complexity | O(n) — guaranteed | O(n^k) worst case | O(n^k) worst case |
| Compile time | O(2^S) DFA construction | O(S) NFA construction | O(S) NFA construction |
| Backreferences | not supported | supported | supported |
| Lookahead/lookbehind | not supported | supported | supported |
| Memory (compiled pattern) | O(2^S) DFA states | O(S) NFA states | O(S) NFA states |
| Suitable for | security-critical matching, untrusted patterns | general use, rich syntax | general use, OTP integration |

Reflection: the DFA state count is O(2^S) in the worst case for S NFA states. Most practical regexes produce compact DFAs. What class of patterns produces worst-case DFA state explosion? Give an example.

---

## Common production mistakes

**1. Epsilon closure not computing the full transitive closure**
The epsilon closure of a set of states must include all states reachable via any number of epsilon transitions, not just one hop. A BFS or DFS that stops at depth 1 produces an incorrect DFA that fails to match valid strings.

**2. Subset construction not handling dead states**
When a DFA state has no transition for a given input character, the correct behavior is to transition to a dead state (a non-accepting sink state) from which no accepting state is reachable. Omitting dead states causes incorrect "match found" results when the DFA falls off the transition table.

**3. Hopcroft refinement using the wrong distinguisher**
The algorithm refines partitions based on which block a state transitions into on each symbol. A common mistake is to refine based on whether a transition exists at all, rather than which target block it reaches. This produces over-merged states that accept incorrect strings.

**4. `#` wildcard in character class negation**
`[^a]` must not match newline by default in most engines (`.` also excludes newline by default). Forgetting the newline exclusion from `.` and `[^...]` classes causes matches to span lines unexpectedly.

---

## Resources

- Cox, R. — *Regular Expression Matching Can Be Simple And Fast* — [swtch.com/~rsc/regexp/regexp1.html](https://swtch.com/~rsc/regexp/regexp1.html) — essential reading before implementation
- Thompson, K. — *Regular Expression Search Algorithm* — CACM, 1968 — the original NFA construction paper; short and readable
- Hopcroft, J., Motwani, R. & Ullman, J. — *Introduction to Automata Theory, Languages, and Computation* — the DFA minimization algorithm reference
- Laurikari, V. — *NFAs with Tagged Transitions, their Conversion to Deterministic Automata and Application to Regular Expressions* — the tagged NFA approach for capturing groups
- Benchmark: [benchmarksgame-team.pages.debian.net/benchmarksgame](https://benchmarksgame-team.pages.debian.net/benchmarksgame/) — regex-redux benchmark suite for validation
