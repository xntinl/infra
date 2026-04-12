# Match specs with `:ets.select/2` and generating them via `:ets.fun2ms/1`

**Project**: `select_ms_demo` — write queries as ordinary Elixir funs,
compile them to match specs with `:ets.fun2ms/1`, and run them against ETS
with `:ets.select/2`.

---

## Project context

Match specs are the full query language of ETS.
They support guards, projections, expression bodies — everything. But
writing them by hand is error-prone and unreadable. The OTP team's answer is
`:ets.fun2ms/1`: a **parse-transform** (Erlang) / **special form** (Elixir via
`:ets.fun2ms`) that takes a fun and turns it into the equivalent match spec
at compile time.

You write:
```elixir
require Ex2ms
Ex2ms.fun do {id, name, age} when age >= 18 -> {id, name} end
```

…or directly in Erlang:
```erlang
ets:fun2ms(fun({Id, Name, Age}) when Age >= 18 -> {Id, Name} end).
```

…and you get the match spec out. In Elixir, since `fun2ms` needs a parse
transform, the idiomatic wrapper is the `Ex2ms` library, which provides a
`fun` macro that does the same job at compile time. This exercise uses both.

## Why `fun2ms` / Ex2ms and not hand-written specs

**Why not just always hand-write match specs?** Because the shape is
error-prone: double-braces for tuple literals, prefix operators
(`{:>=, x, y}` not `x >= y`), atoms quoted as `:"$1"`. A typo compiles and
fails at runtime with `:badarg`.

**Why not skip match specs and filter in Elixir?** Filtering after `select/2`
with no guards means copying every row out of ETS before rejection — and on
a million-row table that's the dominant cost. Guards in match specs filter
inside the engine.

**Why both in this exercise?** You need to **read** raw specs (they show up
in OTP internals, `:dbg` traces, library code) even when you **write** via
`Ex2ms` in your own code.

Project structure:

```
select_ms_demo/
├── lib/
│   └── select_ms_demo.ex
├── test/
│   └── select_ms_demo_test.exs
├── mix.exs
```

---

## Core concepts

### 1. The shape of a match spec, one more time

```elixir
[{match_head, guards, body}, ...]
```

- `match_head`: a pattern over the stored tuple, with `:"$1"`, `:"$2"`, `:_`.
- `guards`: a list of boolean expressions over those bindings, in prefix
  notation (`{:>=, :"$3", 18}` means `$3 >= 18`).
- `body`: a list of terms to return per match. Tuple literals must be
  double-braced: `[{{:"$1", :"$2"}}]` returns `{id, name}`.

### 2. `:ets.fun2ms/1` — generate the spec from a fun

You can call `:ets.fun2ms/1` **at compile time only** if you want the parse
transform to inspect the fun AST. At runtime it works too, but the fun must
be a literal anonymous function — it won't work on a fun passed in as a
variable. In Elixir, the cleanest path is the Ex2ms library's
`Ex2ms.fun/1` macro, which does the compile-time transform for us.

For this exercise we'll use both:

- Direct `:ets.fun2ms/1` via `:ets.fun2ms(fn ... end)` to demystify it.
- `Ex2ms.fun` (add `{:ex2ms, "~> 1.6"}` to deps) for the ergonomic flavor.

### 3. `:ets.select/2` vs `select/3`

- `select(table, ms)` returns all matches in one list. Fine for bounded result
  sets.
- `select(table, ms, limit)` returns `{matches, continuation}` — the streaming
  variant. Use this when the result could be large.

### 4. Match spec guard vocabulary

A short, useful subset:

- Comparisons: `:==`, `:"=:="`, `:"/="`, `:<`, `:"=<"`, `:>`, `:">="`.
- Logical: `:andalso`, `:orelse`, `:not`.
- Type tests: `:is_integer`, `:is_atom`, `:is_binary`, `:is_map`, …
- Built-ins: `:element/2`, `:hd`, `:tl`, `:map_get/2`, `:map_size/1`.

Full list: [erlang.org/doc/apps/erts/match_spec.html](https://www.erlang.org/doc/apps/erts/match_spec.html).

### 5. `fun2ms` catches most mistakes at compile time

If you write something inexpressible (e.g. call a user function in the
guard), `fun2ms` raises at compile time with a message. That's one of its
biggest wins over hand-written specs, which fail silently at runtime with
`:badarg`.

---

## Design decisions

**Option A — Use `:ets.fun2ms/1` directly**
- Pros: No library dependency; uses OTP's parse transform.
- Cons: Elixir doesn't run Erlang parse transforms, so `:ets.fun2ms` only
  works at runtime with a literal fun, which is fragile.

**Option B — Use `Ex2ms.fun` macro** (chosen)
- Pros: Compile-time AST transform; errors caught at compile time.
- Cons: Adds a tiny deps entry (`{:ex2ms, "~> 1.6"}`).

→ Chose **B** because compile-time validation of the spec is the whole point,
and the dependency is trivial.

---

## Implementation

### Step 1: Create the project

```bash
mix new select_ms_demo
cd select_ms_demo
```

Add `{:ex2ms, "~> 1.6"}` to `deps` in `mix.exs`, then `mix deps.get`.

### Step 2: `lib/select_ms_demo.ex`

```elixir
defmodule SelectMsDemo do
  @moduledoc """
  A small "people" table storing `{id, name, age}` and three query flavors
  that all compute the same thing:

    1. `adults_handwritten/1` — the match spec written by hand.
    2. `adults_fun2ms/1` — generated via `:ets.fun2ms/1` (literal fun only).
    3. `adults_ex2ms/1` — generated via the `Ex2ms.fun` macro at compile time.

  Run them all and confirm identical results — the point of the exercise is
  that (3) is the ergonomic choice in Elixir and is strictly equivalent.
  """

  require Ex2ms

  @type row :: {integer(), String.t(), non_neg_integer()}

  @doc "Creates and seeds a `:set` table with five people."
  @spec seed() :: :ets.tid()
  def seed do
    t = :ets.new(:people, [:set, :public])

    :ets.insert(t, [
      {1, "Alice", 30},
      {2, "Bob", 17},
      {3, "Carol", 42},
      {4, "Dan", 12},
      {5, "Eve", 25}
    ])

    t
  end

  @doc """
  Returns `{id, name}` for each person with `age >= 18`, using a match spec
  built by hand. Demonstrates what `fun2ms` is generating under the hood.
  """
  @spec adults_handwritten(:ets.tid()) :: [{integer(), String.t()}]
  def adults_handwritten(t) do
    match_spec = [
      {
        {:"$1", :"$2", :"$3"},          # id=$1, name=$2, age=$3
        [{:>=, :"$3", 18}],             # guard: $3 >= 18
        [{{:"$1", :"$2"}}]              # body: return {id, name}
      }
    ]

    :ets.select(t, match_spec)
  end

  @doc """
  Same query, but the spec is generated at compile time by `Ex2ms.fun`.
  The macro inspects the AST of the fun, validates it, and emits the same
  match spec structure as `adults_handwritten/1`.
  """
  @spec adults_ex2ms(:ets.tid()) :: [{integer(), String.t()}]
  def adults_ex2ms(t) do
    ms =
      Ex2ms.fun do
        {id, name, age} when age >= 18 -> {id, name}
      end

    :ets.select(t, ms)
  end

  @doc """
  A more interesting query: adults whose name starts with "A" or "C",
  returning the full row. Shows a compound guard in both flavors.
  """
  @spec acs_adults(:ets.tid()) :: [row()]
  def acs_adults(t) do
    ms =
      Ex2ms.fun do
        {id, name, age} = row
        when age >= 18 and
               (:binary.part(name, 0, 1) == "A" or :binary.part(name, 0, 1) == "C") ->
          row
      end

    :ets.select(t, ms)
  end
end
```

### Step 3: `test/select_ms_demo_test.exs`

```elixir
defmodule SelectMsDemoTest do
  use ExUnit.Case, async: true

  setup do
    t = SelectMsDemo.seed()
    on_exit(fn -> if :ets.info(t) != :undefined, do: :ets.delete(t) end)
    %{t: t}
  end

  describe "adults_handwritten/1 and adults_ex2ms/1 return the same thing" do
    test "both return only people with age >= 18", %{t: t} do
      handwritten = SelectMsDemo.adults_handwritten(t) |> Enum.sort()
      ex2ms = SelectMsDemo.adults_ex2ms(t) |> Enum.sort()

      expected = [{1, "Alice"}, {3, "Carol"}, {5, "Eve"}]

      assert handwritten == expected
      assert ex2ms == expected
    end
  end

  describe "acs_adults/1" do
    test "filters by age AND name-prefix", %{t: t} do
      # Adults are Alice (30), Carol (42), Eve (25). Of those, names starting
      # with A or C are Alice and Carol.
      result = SelectMsDemo.acs_adults(t) |> Enum.sort()
      assert result == [{1, "Alice", 30}, {3, "Carol", 42}]
    end
  end

  describe "select/3 — streaming" do
    test "limit + continuation walks the table in chunks", %{t: t} do
      ms = Ex2ms.fun do row = {_id, _name, _age} -> row end

      {first_batch, cont} = :ets.select(t, ms, 2)
      assert length(first_batch) == 2

      # Keep pulling until :"$end_of_table".
      all =
        Stream.unfold(cont, fn
          :"$end_of_table" -> nil
          c -> case :ets.select(c) do
                 {batch, next} -> {batch, next}
                 :"$end_of_table" -> nil
               end
        end)
        |> Enum.to_list()
        |> List.flatten()
        |> Kernel.++(first_batch)

      assert length(all) == 5
    end
  end
end
```

### Step 4: Run

```bash
mix deps.get
mix test
```

### Why this works

`:ets.select/2` accepts a match spec — a list of `{head, guards, body}`
triples. The engine scans tuples, tests each against the head pattern and
guards, and for matches constructs the body term. Because guards run inside
the ETS engine (in C), filtering is fast and only matching rows are copied
out to the caller's heap. `Ex2ms.fun` compiles a familiar `fn ... -> ... end`
into that triple-list at compile time, so you get BEAM-level error reporting
instead of runtime `:badarg`.

---

## Benchmark

```elixir
t = :ets.new(:p, [:set, :public])
for i <- 1..100_000, do: :ets.insert(t, {i, "name_#{i}", rem(i, 100)})

require Ex2ms
ms = Ex2ms.fun do {id, name, age} when age >= 50 -> {id, name} end

{us_spec, _} = :timer.tc(fn -> :ets.select(t, ms) end)
{us_elixir, _} = :timer.tc(fn ->
  :ets.tab2list(t) |> Enum.filter(fn {_, _, age} -> age >= 50 end)
end)

IO.puts("select+spec: #{us_spec}µs  tab2list+filter: #{us_elixir}µs")
:ets.delete(t)
```

Target esperado: el filtro con match spec debería ser 3–10× más rápido para
un ratio de match de ~50%, porque evita copiar las filas descartadas al
heap del caller.

---

## Trade-offs and production gotchas

**1. `fun2ms` / `Ex2ms.fun` is a compile-time transform, not a runtime function**
You can only pass a literal fun — not a variable pointing at a fun. The
macro inspects the AST. At runtime, the fun itself is never called; what
actually runs is the generated match spec. This surprises everyone once.

**2. Guards in match specs are a subset of Erlang guards**
Only BIFs marked "allowed in guards" in the Erlang reference are callable
from a match spec. User functions, regex, `String.*` — all off-limits in
the spec. If you need them, filter in Elixir after `select/2` (at the cost
of moving more data across the ETS boundary).

**3. `select/2` can return very large lists — prefer `select/3`**
Every returned term is copied from ETS into your heap. An unbounded
`select/2` on a million-row table can spike memory massively. Use
`select/3` with a reasonable chunk size (hundreds to low thousands) and
continuations.

**4. Match specs do not go through the ordinary compiler**
Spec compilation errors show up at runtime (`:badarg` from `:ets.select/2`)
unless you used `fun2ms`/`Ex2ms`. That's the biggest argument for the
macro: errors move left in time.

**5. `select` scans unless the key is pinned in the match head**
Even with a great guard on the age, if the match head is
`{:"$1", :"$2", :"$3"}`, the engine walks the whole table. Pinning the key
(`{42, :"$1", :"$2"}`) is the only way to get constant-time behavior — and
at that point, `lookup/2` is usually clearer.

**6. When NOT to use match specs**
- Key is known → `lookup/2`.
- You only need shape filter, no guards → `match_object/2`.
- Your filtering is complex or involves user functions → `:ets.foldl/3`
  with an Elixir function body. Slower in theory, saner to read and debug
  in practice for rare operations.

---

## Reflection

- Imagine you need to run the same `Ex2ms.fun` query with a different `age`
  threshold each time. The threshold must be a variable. How do you thread it
  into the match spec, and what are the options for runtime-parameterized
  specs?
- A match spec guard is hitting a complex business rule ("adults in tier-2
  tenants with verified email"). At what point does the readability cost
  outweigh the performance win, and what's your fallback pattern?

---

## Resources

- [`:ets.select/2`](https://www.erlang.org/doc/man/ets.html#select-2), [`select/3`](https://www.erlang.org/doc/man/ets.html#select-3), [`fun2ms/1`](https://www.erlang.org/doc/man/ets.html#fun2ms-1)
- [Erlang match spec reference](https://www.erlang.org/doc/apps/erts/match_spec.html)
- [Ex2ms — the Elixir macro for match specs](https://hexdocs.pm/ex2ms/)
- ["Learn You Some Erlang — ETS"](https://learnyousomeerlang.com/ets) — introductory walkthrough of match specs
- [Fred Hébert — "Erlang in Anger" ETS chapter](https://www.erlang-in-anger.com/) — how match specs interact with memory and performance
