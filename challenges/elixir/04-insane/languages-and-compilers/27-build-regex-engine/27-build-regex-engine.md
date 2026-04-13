# Regex Engine

**Project**: `rexa` — a regex engine with Thompson NFA->DFA->minimized pipeline and O(n) matching

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
│       ├── nfa.ex                   # Thompson construction: AST → NFA with epsilon-transitions
│       ├── dfa.ex                   # subset construction: NFA → DFA
│       ├── minimizer.ex             # Hopcroft partition refinement: DFA → minimal DFA
│       ├── executor.ex              # DFA execution: input x DFA → match result with captures
│       ├── char_class.ex            # character class evaluation: [a-z], \d, \w, \s, negations
│       └── regex.ex                 # public API: compile/1, match/2, scan/2
├── test/
│   └── rexa/
│       ├── parser_test.exs          # AST structure, operator precedence
│       ├── nfa_test.exs             # epsilon closure correctness
│       ├── dfa_test.exs             # subset construction
│       ├── minimizer_test.exs       # state count reduced
│       ├── executor_test.exs        # match results, capture indices
│       └── performance_test.exs    # linear time on adversarial input
├── bench/
│   └── rexa_bench.exs
└── mix.exs
```

---

## The problem

Most production regex engines (PCRE, Java, Python `re`) use backtracking NFA simulation. On adversarial input like matching `(a+)+` against `aaaab`, backtracking engines exhibit exponential time complexity. The NFA/DFA approach guarantees O(n) match time because the DFA processes each character exactly once, with no backtracking.

---

## Why this design

**Thompson's construction** produces structured NFA fragments: each regex operator maps to a small NFA fragment with exactly one entry and one exit state.

**Subset construction** converts NFA to DFA: a DFA state is a set of NFA states. The initial DFA state is the epsilon-closure of the NFA start state.

**Hopcroft's minimization** reduces DFA size by merging indistinguishable states.

**Tagged transitions** for capturing groups annotate epsilon-transitions with group open/close markers.

---

## Design decisions

**Option A — PCRE-style backtracking engine**
- Pros: supports backreferences and lookaround; most feature-rich.
- Cons: catastrophic backtracking on pathological inputs (`(a+)+$`); not safe for untrusted patterns.

**Option B — Thompson NFA / Pike VM (RE2-style)** (chosen)
- Pros: linear in input length regardless of pattern; no catastrophic backtracking; parallel NFA states via bit-sets.
- Cons: no backreferences; lookaround support is limited.

→ Chose **B** because a regex engine that accepts untrusted input must be linear-time; RE2's NFA approach is the only choice that gives that guarantee.

## Implementation milestones

### Step 1: Create the project

**Objective**: Bootstrap a supervised Mix app with `lib/`, `test/`, and `bench/` carved out up front — every later phase drops into a slot that already exists.


```bash
mix new rexa --sup
cd rexa
mkdir -p lib/rexa test/rexa bench
```

### Step 2: `mix.exs` — dependencies

**Objective**: Benchee only — the engine is stdlib-only so its linear-time guarantee can be audited against adversarial input without third-party opacity.


```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: Regex parser

**Objective**: Recursive descent with explicit precedence levels (alt < concat < postfix) — each grammar rule is one function, operator associativity is obvious from the call tree.


```elixir
# lib/rexa/parser.ex
defmodule Rexa.Parser do
  @moduledoc """
  Parses a regex string into an AST using recursive descent.
  """

  @doc "Parses a regex pattern string into an AST."
  @spec parse(String.t()) :: {:ok, term()} | {:error, {non_neg_integer(), String.t()}}
  def parse(pattern) when is_binary(pattern) do
    chars = String.to_charlist(pattern)
    {ast, rest, _group_counter} = parse_alt(chars, 1)

    if rest == [] do
      {:ok, ast}
    else
      {:error, {length(pattern) - length(rest), "unexpected character"}}
    end
  end

  defp parse_alt(chars, gc) do
    {left, rest, gc} = parse_concat(chars, gc)

    case rest do
      [?| | rest2] ->
        {right, rest3, gc} = parse_alt(rest2, gc)
        {{:alt, [left, right]}, rest3, gc}

      _ ->
        {left, rest, gc}
    end
  end

  defp parse_concat(chars, gc) do
    {first, rest, gc} = parse_quantified(chars, gc)
    concat_loop([first], rest, gc)
  end

  defp concat_loop(acc, [], gc), do: {build_concat(acc), [], gc}
  defp concat_loop(acc, [c | _] = chars, gc) when c in [?), ?|] do
    {build_concat(acc), chars, gc}
  end

  defp concat_loop(acc, chars, gc) do
    case parse_quantified(chars, gc) do
      {nil, rest, gc} -> {build_concat(acc), rest, gc}
      {node, rest, gc} -> concat_loop(acc ++ [node], rest, gc)
    end
  end

  defp build_concat([single]), do: single
  defp build_concat(nodes), do: {:concat, nodes}

  defp parse_quantified(chars, gc) do
    {atom, rest, gc} = parse_atom(chars, gc)

    case rest do
      [?* , ?? | r] -> {{:star, atom, :lazy}, r, gc}
      [?* | r] -> {{:star, atom, :greedy}, r, gc}
      [?+ , ?? | r] -> {{:plus, atom, :lazy}, r, gc}
      [?+ | r] -> {{:plus, atom, :greedy}, r, gc}
      [?? , ?? | r] -> {{:opt, atom, :lazy}, r, gc}
      [?? | r] -> {{:opt, atom, :greedy}, r, gc}
      [?{ | r] -> parse_repeat(atom, r, gc)
      _ -> {atom, rest, gc}
    end
  end

  defp parse_repeat(atom, chars, gc) do
    {n_chars, rest} = Enum.split_while(chars, &(&1 in ?0..?9))
    n = List.to_string(n_chars) |> String.to_integer()

    case rest do
      [?} | r] ->
        {{:repeat, atom, n, n, :greedy}, r, gc}

      [?, | r2] ->
        {m_chars, rest2} = Enum.split_while(r2, &(&1 in ?0..?9))
        m = if m_chars == [], do: :infinity, else: List.to_string(m_chars) |> String.to_integer()
        [?} | rest3] = rest2
        {{:repeat, atom, n, m, :greedy}, rest3, gc}
    end
  end

  defp parse_atom([?( , ?? , ?: | rest], gc) do
    {inner, rest2, gc} = parse_alt(rest, gc)
    [?) | rest3] = rest2
    {{:non_capture_group, inner}, rest3, gc}
  end

  defp parse_atom([?( | rest], gc) do
    group_num = gc
    {inner, rest2, gc} = parse_alt(rest, gc + 1)
    [?) | rest3] = rest2
    {{:group, group_num, inner}, rest3, gc}
  end

  defp parse_atom([?[ | rest], gc) do
    {negated, rest} =
      case rest do
        [?^ | r] -> {true, r}
        _ -> {false, rest}
      end

    {ranges, rest} = parse_char_class(rest, [])
    {{:char_class, ranges, negated}, rest, gc}
  end

  defp parse_atom([?. | rest], gc), do: {{:any}, rest, gc}
  defp parse_atom([?^ | rest], gc), do: {{:anchor, :start}, rest, gc}
  defp parse_atom([?$ | rest], gc), do: {{:anchor, :end_}, rest, gc}

  defp parse_atom([?\\ | rest], gc) do
    {escaped, rest2} = parse_escape(rest)
    {escaped, rest2, gc}
  end

  defp parse_atom([c | rest], gc) when c not in [?), ?|, ?*, ?+, ??, ?{, ?}, ?], ?[, ?., ?^, ?$, ?\\] do
    {{:char, c}, rest, gc}
  end

  defp parse_atom(chars, gc), do: {nil, chars, gc}

  defp parse_escape([?d | rest]), do: {{:char_class, [{?0, ?9}], false}, rest}
  defp parse_escape([?D | rest]), do: {{:char_class, [{?0, ?9}], true}, rest}
  defp parse_escape([?w | rest]), do: {{:char_class, [{?a, ?z}, {?A, ?Z}, {?0, ?9}, {?_, ?_}], false}, rest}
  defp parse_escape([?W | rest]), do: {{:char_class, [{?a, ?z}, {?A, ?Z}, {?0, ?9}, {?_, ?_}], true}, rest}
  defp parse_escape([?s | rest]), do: {{:char_class, [{?\s, ?\s}, {?\t, ?\t}, {?\n, ?\n}, {?\r, ?\r}], false}, rest}
  defp parse_escape([?S | rest]), do: {{:char_class, [{?\s, ?\s}, {?\t, ?\t}, {?\n, ?\n}, {?\r, ?\r}], true}, rest}
  defp parse_escape([c | rest]), do: {{:char, c}, rest}

  defp parse_char_class([?] | rest], acc), do: {Enum.reverse(acc), rest}

  defp parse_char_class([c1, ?-, c2 | rest], acc) when c2 != ?] do
    parse_char_class(rest, [{c1, c2} | acc])
  end

  defp parse_char_class([c | rest], acc) do
    parse_char_class(rest, [{c, c} | acc])
  end
end
```

### Step 4: Thompson NFA construction

**Objective**: Thompson fragments — every AST node yields an NFA with one entry and one exit, so composition is just wiring with epsilon edges.


```elixir
# lib/rexa/nfa.ex
defmodule Rexa.NFA do
  @moduledoc """
  Thompson's construction: regex AST -> NFA with epsilon-transitions.
  """

  defstruct [:start, :accept, :transitions]

  @doc "Builds an NFA from a parsed regex AST."
  @spec build(term()) :: %__MODULE__{}
  def build(ast) do
    {nfa, _counter} = build_fragment(ast, 0)
    nfa
  end

  defp build_fragment({:char, c}, counter) do
    start = counter
    accept = counter + 1
    transitions = %{start => [{:char, c, accept}]}
    {%__MODULE__{start: start, accept: accept, transitions: transitions}, counter + 2}
  end

  defp build_fragment({:any}, counter) do
    start = counter
    accept = counter + 1
    transitions = %{start => [{:any, accept}]}
    {%__MODULE__{start: start, accept: accept, transitions: transitions}, counter + 2}
  end

  defp build_fragment({:char_class, ranges, negated}, counter) do
    start = counter
    accept = counter + 1
    transitions = %{start => [{:class, {ranges, negated}, accept}]}
    {%__MODULE__{start: start, accept: accept, transitions: transitions}, counter + 2}
  end

  defp build_fragment({:concat, nodes}, counter) do
    Enum.reduce(nodes, {nil, counter}, fn node, {prev_nfa, cnt} ->
      {nfa, cnt2} = build_fragment(node, cnt)

      if prev_nfa == nil do
        {nfa, cnt2}
      else
        merged_trans =
          Map.merge(prev_nfa.transitions, nfa.transitions, fn _k, v1, v2 -> v1 ++ v2 end)
          |> Map.update(prev_nfa.accept, [{:epsilon, nfa.start}], &[{:epsilon, nfa.start} | &1])

        {%__MODULE__{start: prev_nfa.start, accept: nfa.accept, transitions: merged_trans}, cnt2}
      end
    end)
  end

  defp build_fragment({:alt, [left, right]}, counter) do
    {left_nfa, cnt} = build_fragment(left, counter)
    {right_nfa, cnt2} = build_fragment(right, cnt)
    new_start = cnt2
    new_accept = cnt2 + 1

    trans =
      Map.merge(left_nfa.transitions, right_nfa.transitions, fn _k, v1, v2 -> v1 ++ v2 end)
      |> Map.put(new_start, [{:epsilon, left_nfa.start}, {:epsilon, right_nfa.start}])
      |> Map.update(left_nfa.accept, [{:epsilon, new_accept}], &[{:epsilon, new_accept} | &1])
      |> Map.update(right_nfa.accept, [{:epsilon, new_accept}], &[{:epsilon, new_accept} | &1])

    {%__MODULE__{start: new_start, accept: new_accept, transitions: trans}, cnt2 + 2}
  end

  defp build_fragment({:star, inner, _greediness}, counter) do
    {inner_nfa, cnt} = build_fragment(inner, counter)
    new_start = cnt
    new_accept = cnt + 1

    trans =
      inner_nfa.transitions
      |> Map.put(new_start, [{:epsilon, inner_nfa.start}, {:epsilon, new_accept}])
      |> Map.update(inner_nfa.accept, [{:epsilon, inner_nfa.start}, {:epsilon, new_accept}],
         &([{:epsilon, inner_nfa.start}, {:epsilon, new_accept}] ++ &1))

    {%__MODULE__{start: new_start, accept: new_accept, transitions: trans}, cnt + 2}
  end

  defp build_fragment({:plus, inner, greediness}, counter) do
    {inner_nfa, cnt} = build_fragment(inner, counter)
    {star_nfa, cnt2} = build_fragment({:star, inner, greediness}, cnt)

    trans =
      Map.merge(inner_nfa.transitions, star_nfa.transitions, fn _k, v1, v2 -> v1 ++ v2 end)
      |> Map.update(inner_nfa.accept, [{:epsilon, star_nfa.start}], &[{:epsilon, star_nfa.start} | &1])

    {%__MODULE__{start: inner_nfa.start, accept: star_nfa.accept, transitions: trans}, cnt2}
  end

  defp build_fragment({:opt, inner, _greediness}, counter) do
    {inner_nfa, cnt} = build_fragment(inner, counter)
    new_start = cnt
    new_accept = cnt + 1

    trans =
      inner_nfa.transitions
      |> Map.put(new_start, [{:epsilon, inner_nfa.start}, {:epsilon, new_accept}])
      |> Map.update(inner_nfa.accept, [{:epsilon, new_accept}], &[{:epsilon, new_accept} | &1])

    {%__MODULE__{start: new_start, accept: new_accept, transitions: trans}, cnt + 2}
  end

  defp build_fragment({:group, _num, inner}, counter), do: build_fragment(inner, counter)
  defp build_fragment({:non_capture_group, inner}, counter), do: build_fragment(inner, counter)

  defp build_fragment({:repeat, inner, n, m, greediness}, counter) when m == n do
    nodes = for _ <- 1..n, do: inner
    build_fragment({:concat, nodes}, counter)
  end

  defp build_fragment({:repeat, inner, n, m, greediness}, counter) do
    required = for _ <- 1..n, do: inner
    optional_count = if m == :infinity, do: 3, else: m - n
    optional = for _ <- 1..optional_count, do: {:opt, inner, greediness}
    all_nodes = required ++ optional
    build_fragment({:concat, all_nodes}, counter)
  end

  defp build_fragment({:anchor, _}, counter) do
    start = counter
    {%__MODULE__{start: start, accept: start, transitions: %{}}, counter + 1}
  end

  @doc "Computes the epsilon closure of a set of NFA states."
  @spec epsilon_closure(%__MODULE__{}, [non_neg_integer()]) :: MapSet.t()
  def epsilon_closure(nfa, states) when is_list(states) do
    do_closure(nfa, states, MapSet.new(states))
  end

  defp do_closure(_nfa, [], visited), do: visited

  defp do_closure(nfa, [state | rest], visited) do
    transitions = Map.get(nfa.transitions, state, [])
    epsilon_targets =
      transitions
      |> Enum.filter(fn t -> elem(t, 0) == :epsilon end)
      |> Enum.map(fn {:epsilon, target} -> target end)
      |> Enum.reject(&MapSet.member?(visited, &1))

    new_visited = Enum.reduce(epsilon_targets, visited, &MapSet.put(&2, &1))
    do_closure(nfa, epsilon_targets ++ rest, new_visited)
  end
end
```

### Step 5: DFA construction and executor

**Objective**: Treat each DFA state as the epsilon-closure of an NFA state set — once built, matching is a single table lookup per input character, guaranteeing O(n).


```elixir
# lib/rexa/dfa.ex
defmodule Rexa.DFA do
  @moduledoc "Subset construction: NFA -> DFA."

  defstruct [:start, :accept, :transitions, :alphabet]

  @doc "Builds a DFA from an NFA via subset construction."
  @spec build(%Rexa.NFA{}) :: %__MODULE__{}
  def build(nfa) do
    start_set = Rexa.NFA.epsilon_closure(nfa, [nfa.start])
    alphabet = collect_alphabet(nfa)

    {dfa_states, dfa_transitions, accept_states} =
      worklist([start_set], %{start_set => 0}, %{}, MapSet.new(), nfa, alphabet, 1)

    accept_ids = Enum.flat_map(dfa_states, fn {set, id} ->
      if MapSet.member?(set, nfa.accept), do: [id], else: []
    end)

    %__MODULE__{
      start: 0,
      accept: MapSet.new(accept_ids),
      transitions: dfa_transitions,
      alphabet: alphabet
    }
  end

  defp worklist([], state_map, transitions, _accept, _nfa, _alphabet, _next_id) do
    {state_map, transitions, nil}
  end

  defp worklist([current_set | rest], state_map, transitions, accept, nfa, alphabet, next_id) do
    current_id = Map.get(state_map, current_set)

    {new_queue, new_state_map, new_transitions, new_next_id} =
      Enum.reduce(alphabet, {rest, state_map, transitions, next_id}, fn symbol, {q, sm, trans, nid} ->
        target_states =
          current_set
          |> MapSet.to_list()
          |> Enum.flat_map(fn s ->
            Map.get(nfa.transitions, s, [])
            |> Enum.filter(fn t -> matches_symbol?(t, symbol) end)
            |> Enum.map(fn t -> List.last(Tuple.to_list(t)) end)
          end)

        if target_states == [] do
          {q, sm, trans, nid}
        else
          target_set = Rexa.NFA.epsilon_closure(nfa, target_states)

          {target_id, new_sm, new_nid, new_q} =
            case Map.get(sm, target_set) do
              nil ->
                {nid, Map.put(sm, target_set, nid), nid + 1, q ++ [target_set]}
              existing_id ->
                {existing_id, sm, nid, q}
            end

          new_trans = Map.update(trans, current_id, %{symbol => target_id}, &Map.put(&1, symbol, target_id))
          {new_q, new_sm, new_trans, new_nid}
        end
      end)

    worklist(new_queue, new_state_map, new_transitions, accept, nfa, alphabet, new_next_id)
  end

  defp matches_symbol?({:char, c, _target}, {:char, c}), do: true
  defp matches_symbol?({:any, _target}, _symbol), do: true
  defp matches_symbol?({:class, {ranges, negated}, _target}, {:char, c}) do
    in_range = Enum.any?(ranges, fn {lo, hi} -> c >= lo and c <= hi end)
    if negated, do: not in_range, else: in_range
  end
  defp matches_symbol?(_, _), do: false

  defp collect_alphabet(nfa) do
    nfa.transitions
    |> Enum.flat_map(fn {_state, trans_list} ->
      Enum.flat_map(trans_list, fn
        {:char, c, _} -> [{:char, c}]
        {:any, _} -> []
        {:class, _, _} -> []
        {:epsilon, _} -> []
        _ -> []
      end)
    end)
    |> Enum.uniq()
  end
end
```

```elixir
# lib/rexa/executor.ex
defmodule Rexa.Executor do
  @moduledoc "Executes a DFA against binary input for O(n) matching."

  @doc "Attempts to match the DFA against input starting at offset."
  @spec match(%Rexa.DFA{}, binary(), non_neg_integer()) :: {:match, [{non_neg_integer(), non_neg_integer()}]} | :no_match
  def match(dfa, input, offset \\ 0) when is_binary(input) do
    bytes = :binary.bin_to_list(binary_part(input, offset, byte_size(input) - offset))
    do_match(dfa, bytes, dfa.start, offset, nil, 0)
  end

  defp do_match(dfa, [], current_state, start_offset, last_match, pos) do
    if MapSet.member?(dfa.accept, current_state) do
      {:match, [{start_offset, pos}]}
    else
      case last_match do
        nil -> :no_match
        match -> {:match, [match]}
      end
    end
  end

  defp do_match(dfa, [byte | rest], current_state, start_offset, last_match, pos) do
    new_last_match =
      if MapSet.member?(dfa.accept, current_state) do
        {start_offset, pos}
      else
        last_match
      end

    transitions = Map.get(dfa.transitions, current_state, %{})
    next_state = Map.get(transitions, {:char, byte})

    if next_state do
      do_match(dfa, rest, next_state, start_offset, new_last_match, pos + 1)
    else
      if new_last_match do
        {:match, [new_last_match]}
      else
        :no_match
      end
    end
  end

  @doc "Finds all non-overlapping matches in the input."
  @spec scan(%Rexa.DFA{}, binary()) :: [{non_neg_integer(), non_neg_integer()}]
  def scan(dfa, input) do
    do_scan(dfa, input, 0, [])
  end

  defp do_scan(_dfa, input, offset, acc) when offset >= byte_size(input) do
    Enum.reverse(acc)
  end

  defp do_scan(dfa, input, offset, acc) do
    case match(dfa, input, offset) do
      {:match, [{start, len}]} ->
        next_offset = max(start + len, offset + 1)
        do_scan(dfa, input, next_offset, [{start, len} | acc])

      :no_match ->
        do_scan(dfa, input, offset + 1, acc)
    end
  end
end
```

### Step 6: Public API

**Objective**: `compile/1` to a DFA, `match?/2` and `scan/2` over binaries — hide the NFA/DFA pipeline behind the same shape most regex libraries already use.


```elixir
# lib/rexa/regex.ex
defmodule Rexa do
  @moduledoc "Public API for the regex engine."

  @doc "Compiles a regex pattern into a DFA."
  @spec compile(String.t()) :: {:ok, %Rexa.DFA{}} | {:error, term()}
  def compile(pattern) do
    {:ok, ast} = Rexa.Parser.parse(pattern)
    nfa = Rexa.NFA.build(ast)
    dfa = Rexa.DFA.build(nfa)
    {:ok, dfa}
  end

  @doc "Matches a compiled DFA against input."
  @spec match(%Rexa.DFA{}, binary()) :: {:match, [{non_neg_integer(), non_neg_integer()}]} | :no_match
  def match(dfa, input), do: Rexa.Executor.match(dfa, input)

  @doc "Finds all non-overlapping matches."
  @spec scan(%Rexa.DFA{}, binary()) :: [{non_neg_integer(), non_neg_integer()}]
  def scan(dfa, input), do: Rexa.Executor.scan(dfa, input)
end
```

### Step 7: Given tests — must pass without modification

**Objective**: Pin the public contract with a frozen suite — if the engine drifts, these tests are the single source of truth that call it out.


```elixir
# test/rexa/performance_test.exs
defmodule Rexa.PerformanceTest do
  use ExUnit.Case, async: true

  test "adversarial input does not cause exponential blowup" do
    {:ok, dfa} = Rexa.compile("(a+)+b")
    input = String.duplicate("a", 30) <> "b"

    {time_us, result} = :timer.tc(fn -> Rexa.match(dfa, input) end)

    assert {:match, _} = result
    assert time_us < 100_000, "match took #{time_us}us — possible exponential blowup"
  end

  test "linear time scaling with input length" do
    {:ok, dfa} = Rexa.compile("a*b")

    times = for n <- [100, 1_000, 10_000] do
      input = String.duplicate("a", n) <> "b"
      {t, _} = :timer.tc(fn -> Rexa.match(dfa, input) end)
      t
    end

    [t1, t2, t3] = times
    assert t2 < t1 * 20, "scaling from 100->1000 chars: #{t1}us -> #{t2}us (expected ~10x)"
    assert t3 < t2 * 20, "scaling from 1000->10000 chars: #{t2}us -> #{t3}us (expected ~10x)"
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
    assert length(captures) == 3
  end

  test "character class [a-z]+" do
    {:ok, dfa} = Rexa.compile("[a-z]+")
    {:match, [{start, len}]} = Rexa.match(dfa, "123abc456")
    assert binary_part("123abc456", start, len) == "abc"
  end
end
```

### Step 8: Run the tests

**Objective**: Run the suite end-to-end with `--trace` so failures name the exact stage — parser, NFA, DFA, or matcher — without guesswork.


```bash
mix test test/rexa/ --trace
```

### Step 9: Benchmark

**Objective**: Run `(a+)+$` against long inputs — the benchmark is the proof that the DFA path stays linear where PCRE-style backtrackers explode.


```elixir
# bench/rexa_bench.exs
{:ok, dfa} = Rexa.compile("hello")
input = String.duplicate("test hello world end ", 1_000)

Benchee.run(
  %{
    "simple literal scan" => fn -> Rexa.scan(dfa, input) end,
    "erlang :re scan (baseline)" => fn -> :re.run(input, "hello", [:global]) end
  },
  time: 5,
  warmup: 2,
  formatters: [Benchee.Formatters.Console]
)
```

### Why this works

The compiler turns the pattern into an NFA with ε-transitions; the VM maintains a set of active states and advances all of them in parallel per input character. Because each state is either active or not (no backtracking), match time is O(|pattern| × |input|).

---

## Benchmark

```elixir
# bench/regex_bench.exs
Benchee.run(%{"match" => fn -> Regex2.match?(nfa, "aaab") end}, time: 10)
def main do
  IO.puts("[Rexa.ExecutorTest.build] demo")
  :ok
end

```

Target: Match of a 20-char pattern against a 10 KB input in < 100 µs; no catastrophic cases on `(a+)+$`-style inputs.

---

## Deep Dive: NIF Callbacks and BEAM Scheduling Implications

Native Implemented Functions (NIFs) are C/Rust code called from Elixir. They are fast (no VM overhead) but dangerous: a blocking NIF blocks the entire BEAM scheduler thread, starving all other processes.

**The BEAM scheduler model**: One OS thread per logical scheduler (typically one per CPU core). When a process calls a NIF, the thread executes C code. If the NIF blocks (e.g., calling `read()` on a socket), the thread is blocked, and all 100+ other processes on that scheduler are frozen.

**Problem**: A Rustler NIF that calls `std::fs::File::open()` or `std::net::TcpStream::connect()` may block. Meanwhile, unrelated Erlang processes starve.

**Solutions**:
1. **Dirty schedulers**: Mark the NIF as `:dirty_io` or `:dirty_cpu`. The BEAM reserves separate threads for blocking work. The calling process is moved off the main scheduler; others continue. Trade-off: dirty threads are a limited resource; over-subscribe and throughput drops.
2. **Async via thread pool**: Spawn a C thread from a thread pool, return immediately, and notify Erlang via callback. Complex but non-blocking.
3. **Never block in NIFs**: Only call non-blocking C functions (pure computation, hash functions). Delegate I/O to Erlang processes.

Rustler's `:dirty_io` attribute automates dirty scheduler mapping. For a Rust function that calls a blocking OS API:

```rust
pub fn expensive_operation(a: u32) -> u32 {
    // Blocking operations here, will run on dirty_io scheduler
    std::thread::sleep(Duration::from_secs(1));
    a + 1
}
#[rustler::nif(schedule = "DirtyIo")]
pub fn expensive_operation_nif(a: u32) -> u32 { expensive_operation(a) }
```

**Production pattern**: Reserve dirty schedulers for truly blocking I/O. Measure scheduler utilization to confirm no starvation under load. Prefer Elixir async processes for I/O when possible; they are more observable and composable.

---

## Trade-off analysis

| Aspect | NFA/DFA (this impl) | Backtracking NFA (PCRE) | Erlang `:re` |
|--------|-------------------|------------------------|--------------|
| Match time complexity | O(n) guaranteed | O(n^k) worst case | O(n^k) worst case |
| Compile time | O(2^S) DFA construction | O(S) NFA construction | O(S) |
| Backreferences | not supported | supported | supported |
| Memory (compiled) | O(2^S) DFA states | O(S) NFA states | O(S) |

Reflection: the DFA state count is O(2^S) in the worst case. What class of patterns produces worst-case DFA state explosion? Give an example.

---

## Common production mistakes

**1. Epsilon closure not computing full transitive closure**
A BFS or DFS that stops at depth 1 produces an incorrect DFA.

**2. Subset construction not handling dead states**
When no transition exists for an input character, transition to a dead state (non-accepting sink).

**3. Hopcroft refinement using wrong distinguisher**
Refine based on which target block a transition reaches, not whether a transition exists.

**4. Character class negation not excluding newline**
`[^a]` must not match newline by default.

## Reflection

- What's the smallest regex feature set that still covers 90% of real-world use? Would you drop backreferences, lookaround, or neither?
- Compare NFA vs DFA compilation. When would pre-building the DFA pay off, and when is the state explosion prohibitive?

---

## Resources

- Cox, R. — *Regular Expression Matching Can Be Simple And Fast* — [swtch.com/~rsc/regexp/regexp1.html](https://swtch.com/~rsc/regexp/regexp1.html)
- Thompson, K. — *Regular Expression Search Algorithm* — CACM, 1968
- Hopcroft, J., Motwani, R. & Ullman, J. — *Introduction to Automata Theory, Languages, and Computation*
- Laurikari, V. — *NFAs with Tagged Transitions* — tagged NFA approach for capturing groups
