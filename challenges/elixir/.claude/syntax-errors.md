# Syntax Errors Requiring Manual Review

Generated: 2026-04-13 12:02:51.697202Z

**Total issues:** 4736

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `rsive list processing.

  Demonstrates head/tail pattern mat`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `d — O(n) total
1..10_000 |> Enum.reduce([], fn x, acc -> [x `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `list]

# O(n) — must traverse and copy the entire list
exist`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `dule Bench do
  def prepend_reverse(n) do
    Enum.reduce(1.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `ase, async: true

  doctest MdLite

  describe "headings" do`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `a subset of Markdown to HTML using recursive list processing`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** `rnal dependencies required
  ]
end
```


### `lib/md_lite.ex`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md
**Preview:** ` [head | tail] = [1, 2, 3, 4]
iex> head
1
iex> tail
[2, 3, 4`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `formations on immutable data.

  All functions are pure — th`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `.sum(Enum.map(events, & &1.amount))
max = Enum.max_by(events`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `
  |> Enum.map(&transform/1)      # Allocates new list
  |> `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** ` i <- 1..100_000 do
        %{id: i, user_id: rem(i, 500), t`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `xUnit.Case, async: true

  doctest Analytics

  @events [
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `t log analytics using Enum transformations on immutable data`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md
**Preview:** `ternal dependencies required
  ]
end
```


### `lib/analytic`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** ` the pipe operator.

  Each step in the ETL process is a fun`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `
|> save_to_database()          # Side effect — harder to te`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `im()
# Just write: result = String.trim(input)

# Bad — pipe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `.encode!()

# But if a library puts options first, wrap it:
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `cord), do: ...
def cast_type(record, field, type), do: ...
d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `=
      ["name,age,email"] ++
        for i <- 1..10_000 do
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** ```

Clearer intent, less variable rebinding.

### `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `name)
user = save(user)
```

Becomes:

```elixir
get_user(id`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** ` > 3))
```

This reads top-to-bottom like a Unix pipeline.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `xUnit.Case, async: true

  doctest Etl

  @sample_csv """
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `sform-Load pipeline using the pipe operator.

  Each step in`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `rnal dependencies required
    {:"jason", "~> 1.0"},
  ]
end`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `sed)
valid = filter_valid(typed)
encode_json(valid)
```
- Pr`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md
**Preview:** `thout pipes — read inside-out
String.trim(String.downcase(St`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `TxClient do
    @behaviour PageStream.ApiClient

    # 6 tra`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `per page, 1000 pages total
    pages =
      for page <- 1..`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `m` functions return streams (lazy), not lists. Transformatio`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `ts consumes memory. But a range is lazy—each elemen`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `.Case, async: true

  alias PageStream.Exporter

  defmodule`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `Unit.Case, async: true

  alias PageStream.Paginator

  defm`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `e paginated stream and writes a CSV.
  """

  alias PageStre`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `zy Stream of individual items.

  Uses Stream.resource/3 to `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `PI. A real client would use Req or Finch.
  We use a behavio`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** ` app: :page_stream,
      version: "0.1.0",
      elixir: "~`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `
```


### Step 1: Create the project

**Objective**: Split `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md
**Preview:** `e) do
      {items, new_state} -> {items, new_state}   # emi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `    test "produces a cartesian product filtered by business `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `ion
for {k, v} <- pairs, into: %{}, do: {k, v}
```

**4. Rel`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `n products

    for n <- sizes do
      {us, count} =
      `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** ` alias GridCombo.Pricing

  describe "price_table/0" do
    `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `true

  alias GridCombo.Catalog

  describe "all_variants/0"`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `ing `for ..., into: %{}`.
  """

  alias GridCombo.Catalog

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `mbinations using `for`
  comprehensions with multiple genera`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `id_combo,
      version: "0.1.0",
      elixir: "~> 1.17",
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `## Step 1: Create the project

**Objective**: Build minimal `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `--

## Design decisions

**Option A — chained `Enum.flat_map`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `t must implement `Collectable`. Built-ins include `%{}`, `Ma`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `generators BEFORE them. `rem(x, 2) == 0` short-circui`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md
**Preview:** `b}]
```

The rightmost generator varies fastest. With 3 gene`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `num.reduce/reduce_while.
  """

  @doc """
  Counts events g`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `.1_000_000 do
        %{type: Enum.random([:click, :purchase`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `ccumulator Must Include All State

If you need to track mu`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `escribe "count_by_type/1" do
    test "counts events by type`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `ns over event streams using Enum.reduce/reduce_while.
  """
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `ependencies required
  ]
end
```


### Step 1 — Create the p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md
**Preview:** `00_000_000},
  %{type: :purchase, user_id: 42, ts: 1_700_000`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md
**Preview:** `"

  @doc """
  Infinite lazy Fibonacci sequence starting at`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md
**Preview:** `ch_log_#{System.unique_integer([:positive])}.log")

    # Pr`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md
**Preview:** `onacci/0" do
    test "produces the expected sequence lazily`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md
**Preview:** `d Stream.unfold/2 and Stream.resource/3.
  """

  @doc """
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md
**Preview:** `endencies required
  ]
end
```


### Step 1 — Create the pro`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md
**Preview:** `algebra.
  """

  @type visitor_id :: integer() | String.t()`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md
**Preview:** `&(rem(&1, 80_000)))       # duplicates + overlap
    yesterd`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md
**Preview:** ` do
    # Raw events with duplicates (same user opens the ap`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md
**Preview:** `-visitor tracking with set algebra.
  """

  @type visitor_i`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md
**Preview:** `pendencies required
    {:"jason", "~> 1.0"},
  ]
end
```


`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** `s.
  """

  @valid_opts [:select, :where, :order_by, :limit]`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** `o
        %{name: "User#{i}", age: rem(i, 80) + 18, role: En`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** `%{name: "Ada", age: 36, role: :admin},
    %{name: "Bo", age`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** ` query DSL built on keyword-list options.
  """

  @valid_op`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** `ependencies required
    {:"ecto", "~> 1.0"},
    {:"phoenix`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md
**Preview:** `e],
  where: [role: :admin],
  order_by: :age,
  limit: 5
)
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `ration data using idiomatic Elixir control flow.

  Demonstr`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `= :ok, do: ...

# Good
case result do
  :ok -> ...
  {:error`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `00 do
      # representative call of validate_registration/1`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `lidate_password(params[:password]),
     :ok <- validate_use`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `l(params[:email]) do
  :ok ->
    case validate_password(par`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `est do
  use ExUnit.Case, async: true

  doctest Validator

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `lidates user registration data using idiomatic Elixir contro`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md
**Preview:** `external dependencies required
  ]
end
```


### `lib/valida`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `ker.

  Demonstrates tail recursion with accumulators, depth`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `    # representative call of walk of a 100k-file tree
      `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `sum(4), sum(3), sum(2), sum(1), sum(0)] — grows with input
d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `se, async: true

  @test_dir Path.join(System.tmp_dir!(), "t`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `system tree walker.

  Demonstrates tail recursion with accu`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** ` dependencies required
  ]
end
```


### `lib/tree.ex`

```e`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md
**Preview:** `T tail-recursive — multiplication happens AFTER the recursiv`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** ` resource.

  The decision is a function of four inputs:
   `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `authorization checks
      :ok
    end
  end)

IO.puts("Avg:`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `ultiple functions. This avoids repetition and makes`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `alias AuthPolicy.Engine

  @working_ctx %{user_id: 42, day_o`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `require AuthPolicy.Guards
  import AuthPolicy.Guards

  desc`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** ` may perform an action on a resource.

  The decision is a f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `s.

  Every guard defined here expands inline into a boolean`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `uth_policy,
      version: "0.1.0",
      elixir: "~> 1.17",`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md
**Preview:** `## Step 1: Create the project

**Objective**: Separate Guard`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `ipeline is the value proposition of this module: every step `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `t/1 over 100k carts
      :ok
    end
  end)

IO.puts("Avg: `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `: true

  alias CheckoutFlow.Checkout

  @today ~D[2026-04-1`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `}
  else
    {:error, {:out_of_stock, sku}} -> {:error, %{co`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `rofile) do
  {:ok, {user, profile}}
else
  error -> error
en`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `th`.

  The pipeline is the value proposition of this module`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `his would hit a carrier API.
  Here we use a simple table by`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `Coupon do
  @moduledoc false

  @type coupon :: %{code: Stri`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** ` absent (nil), present and valid,
  or present and rejected `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `B access, no HTTP calls.

  In a real system, stock would be`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `: :checkout_flow,
      version: "0.1.0",
      elixir: "~> `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `
### Step 1: Create the project

**Objective**: Organize car`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md
**Preview:** `ep_three(b) do
  {:ok, c}
else
  {:error, :reason_one} -> ha`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `to actionable categories.

  Input shapes:
    {:ok, status}`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `all of classify/1 over 1M responses
      :ok
    end
  end)`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `tpStatusClassifier.Classifier

  describe "2xx/3xx — :ok tup`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `"""
  Classifies HTTP-shaped results into actionable categor`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `ependencies required
  ]
end
```


### Step 1 — Create the p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md
**Preview:** `0..299 -> :success
  {:error, {:http, status}} when status i`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** `ier_discount - bulk_discount + tax.

  Tier discount (picks `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** ` call of apply_discount/1 over 1M cart totals
      :ok
    `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** `ngRuleEngine.Pricing

  describe "tier discount cascade (con`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** ` final price = subtotal - tier_discount - bulk_discount + ta`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** `ies required
  ]
end
```


### Step 1 — Create the project

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** ` true    -> :small
end
```

`if` is for a **single yes/no** `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md
**Preview:** ` two branches, one boolean
```

```elixir
cond do
  x > 100 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `te

  describe "privileged roles always pass (compound guard`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `esentative call of can_see?/2 over 1M users
      :ok
    en`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `tureGate.Gate

  describe "privileged roles always pass (com`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `er a user sees a feature.

  Rules (evaluated in order):
   `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `dencies required
  ]
end
```


### Step 1 — Create the proje`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `] and env != :prod, do: true
```

Guards let you combine mem`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md
**Preview:** `nt to
if not admin?, do: raise("no access")
```

Both are le`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `ssor

  @tmp_dir System.tmp_dir!()

  setup do
    path = Pa`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `tive call of process_file/1 on a 10MB file
      :ok
    end`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `ileProcessor.Processor

  @tmp_dir System.tmp_dir!()

  setu`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `le line by line and applies a transform. Guarantees the file`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `ies required
  ]
end
```


### Step 1 — Create the project

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md
**Preview:** `rror, Exception.message(e)}
catch
  :throw, value -> {:throw`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md
**Preview:** ` validated construction.

  Uses @enforce_keys for required `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md
**Preview:** `: "a@b.com", name: "Ana", age: 30, role: :user}

    {new_us`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md
**Preview:** `e ExUnit.Case, async: true

  doctest UserSchema

  describe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md
**Preview:** `safe user domain model with validated construction.

  Uses `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md
**Preview:** `rnal dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md
**Preview:** `d optional fields.

  Required: `:email`, `:password_hash`
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md
**Preview:** `l" => "a@b.com", "password_hash" => "x"}

    {enforce_us, _`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md
**Preview:** `  describe "struct creation" do
    test "succeeds with all `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md
**Preview:** `record with required and optional fields.

  Required: `:ema`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md
**Preview:** ` dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `42) == "42"
  end

  test "encodes strings with escaped quot`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `sh.User{id: 1, name: "Ana", email: "a@b.com"}
    ]

    {us`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `assert Jsonish.encode(42) == "42"
  end

  test "encodes str`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `

  @type t :: %__MODULE__{id: integer(), email: String.t(),`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `Integer module.
  def encode(n), do: Integer.to_string(n)
en`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `ring.

  Not RFC 8259 compliant — this is an exercise in pol`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md
**Preview:** `ed
    {:"jason", "~> 1.0"},
    {:"poison", "~> 1.0"},
  ]
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `th backends.
  # If the contract is respected, both pass ide`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** ` module via config or function argument. That is the whole p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `ainst both backends.
  # If the contract is respected, both `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `nder `dir`.

  Not concurrency-safe — for this exercise we a`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `od for tests."

  @behaviour KvStore.Backend

  @impl true
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `nd.

  Any module implementing this behaviour must handle `g`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md
**Preview:** `   {:"ecto", "~> 1.0"},
  ]
end
```


### Step 1: Create the`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** `  All operations are total on `number()` — we never return f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** `r, atom()}
```

Run `mix dialyzer` again. Dialyzer flags the`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** ` "basic ops" do
    assert {:ok, 5} = TypedCalc.apply_op(:ad`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** ` strict arithmetic types.

  All operations are total on `nu`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** `project do
    [
      app: :typed_calc,
      version: "0.1`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md
**Preview:** `ndencies required
    {:"ecto", "~> 1.0"},
  ]
end
```


###`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md
**Preview:** `n, _} = :timer.tc(fn ->
  for _ <- 1..1_000_000, do: handler`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md
**Preview:** `eue.ErrorHandlingTest do
  use ExUnit.Case, async: true

  a`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md
**Preview:** `a single job from the task queue.

  Wraps job execution in `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md
**Preview:** ` project do
    [
      app: :task_queue,
      version: "0.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md
**Preview:** `dencies required
    {:"jason", "~> 1.0"},
  ]
end
```


###`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `
    _ -> :ok
  end
end

{us_ok, _}    = :timer.tc(fn -> for`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `dle_runtime_error(e)
end
```

Different exceptions trigger d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `  alias RetryHttp.Client

  describe "get/3 — happy path" do`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `n-recoverable-error semantics.
  """

  alias RetryHttp.Erro`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `sts.

  In production this would wrap `:httpc`, `Finch`, or `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `fexception` generates a struct with a `:message` field plus `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** `http,
      version: "0.1.0",
      elixir: "~> 1.17",
     `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md
**Preview:** ` Step 1: Create the project

**Objective**: Organize errors/`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `do: ValidateLib.Safe.validate_signup(good) end)
{us_bang_ok,`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `d code. Use `with` — it's built for exactly this.

**5. Forg`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `g.ValidationError

  describe "validate_signup!/1" do
    te`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `lidate_signup/1 — success" do
    test "returns {:ok, map} f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `a is a bug, not a
  runtime condition — fixture loaders, tes`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `rs and user-facing code use.

  Every function returns `{:ok`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `s `{:ok, value} | {:error, reason}`.

  The `:error` reason `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `date_lib,
      version: "0.1.0",
      elixir: "~> 1.17",
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md
**Preview:** `1.0"},
  ]
end
```


### Step 1: Create the project

**Objec`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md
**Preview:** `row_path = fn ->
  try do
    throw(:stop)
  catch
    :thro`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md
**Preview:** `runs all steps to completion" do
    steps = [
      fn x ->`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md
**Preview:** ` of step functions. Each step may return {:ok, value}, raise`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md
**Preview:** `dencies required
  ]
end
```


### Step 1: Create the projec`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md
**Preview:** `e in RuntimeError -> {:raised, e}
catch
  :throw, value -> {`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md
**Preview:** `trap_exit, true)
    pid = spawn_link(fn -> :ok end)
    rec`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md
**Preview:** `est "captures normal return value" do
    assert {:ok, 42} =`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md
**Preview:** `ty function in a linked process and captures its exit reason`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md
**Preview:** `dencies required
  ]
end
```


### Step 1: Create the projec`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md
**Preview:** `age(e), do: "invalid email: #{e.address}"
end

raise_path = `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md
**Preview:** `  alias DomainErrors.{InvalidEmail, InvalidAmount, PaymentDe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md
**Preview:** `specific exceptions for a payments module.

  These exceptio`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md
**Preview:** `al dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md
**Preview:** `r.load(good_path) end)
{us_bang, _} = :timer.tc(fn -> for _ `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md
**Preview:** `fig/example.exs", __DIR__)

  describe "load/1 — safe versio`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md
**Preview:** `s shape.

  Public API:

    * `load/1`  — safe version, ret`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md
**Preview:** `file/1.
%{
  database_url: "postgres://localhost/app",
  poo`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md
**Preview:** `  {:"jason", "~> 1.0"},
  ]
end
```


### Step 1: Create the`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `peline.Middlewares.rate_limit(1_000)
stamp = &Pipeline.Middl`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `o: # ...
def process(data, opts) when is_list(data), do: # .`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `_email/3

def connect(host, port \\ 5432, timeout \\ 5000)
#`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `ELLO"

# Capture a local function
defmodule Example do
  def`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `ync: true

  doctest Pipeline
  doctest Pipeline.Middlewares`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** ` functions demonstrating multi-arity patterns,
  default arg`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `eline inspired by Plug.

  Each middleware is a function tha`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `quired
    {:"plug", "~> 1.0"},
  ]
end
```


### `lib/pipel`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md
**Preview:** `e are TWO DIFFERENT functions
def process(data), do: process`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `d the VALUE 1
```

**3. Capturing the wrong arity**
`&String`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `    RulesEngine.max_age_rule(70),
    fn user -> user.verifi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `= fn a, b -> a + b end
add.(1, 2)  # => 3

# Named — defined`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `e ExUnit.Case, async: true

  doctest RulesEngine

  @applic`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `ules engine using anonymous functions and closures.

  Rules`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `endencies required
  ]
end
```


### `lib/rules_engine.ex`

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `n_age is "closed over" — captured at creation time
def min_a`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md
**Preview:** `va needs: interface, classes, constructor injection
# Elixir`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md
**Preview:** `t)  # instead of PayAdapter.Receipt.format_charge(result)

#`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md
**Preview:** `ue

  doctest PayAdapter
  doctest PayAdapter.Receipt

  des`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md
**Preview:** `isplay.

  Demonstrates `alias` and how child modules organi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md
**Preview:** `a clean public API.

  The public functions (charge, refund,`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md
**Preview:** `ed
    {:"ecto", "~> 1.0"},
    {:"httpoison", "~> 1.0"},
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `")]
stages_fn = [fn s -> String.trim(s) end, fn s -> String.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `mposer.{Pipeline, Stages}

  describe "capture of named func`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `f unary functions against an input.

  Stages are captured f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `ipeline. Each is a plain `fun/1` so it can be captured
  as `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `p 1 — Create the project

**Objective**: Split stages from`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `ired
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md
**Preview:** `&Stages.normalize/1

# These are also equivalent:
fn x -> x `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `e, but if the default is expensive, precompute it.

**3. Gua`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `f greet(name, greeting),   do: "#{greeting}, #{name}!"
```

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `,           do: "#{greeting}, boss."
```

You must declare a`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `fn _ -> ApiClientDefaults.Client.request("/ping") end)
end)
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `as ApiClientDefaults.Client

  describe "request/2 defaults"`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** ` API client illustrating default arguments.

  We do NOT per`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** ```

### Step 1 — Create the project

**Objective**: Create`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `dencies required
  ]
end
```


### Dependencies (`mix.exs`)
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `request(path, timeout_ms), do: do_request(path, timeout_ms)
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md
**Preview:** `equest(path, timeout_ms)
```

The compiler generates:

```el`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** `   OrderStateMachine.Order.transition(:pending, :pay)
  end)`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** `StateMachine.Order

  describe "transition/2 — valid transit`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** ` pending -> paid -> shipped -> delivered.

  Also: pending -`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** `ep 1 — Create the project

**Objective**: Build minimal li`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** `quired
  ]
end
```


### Dependencies (`mix.exs`)

```elixir`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md
**Preview:** `et(name) when is_binary(name), do: "Hi, #{name}"
@doc "greet"
def greet(_`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `l("hello")
  end)
end)

{t_dynamic, _} = :timer.tc(fn ->
  E`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `atcher.Plugins.{Upcase, Reverse}

  describe "dispatch/2" do`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `` callback.
  """

  # `alias` — avoids writing PluginDispat`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** ` is_binary(input), do: String.reverse(input)
end
```

### St`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `name/0.
  use PluginDispatcher.Plugin

  @impl true
  def ca`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `se` macro that wires it up.
  """

  @callback name() :: ato`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** ` — Create the project

**Objective**: Split the dispatcher, `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md
**Preview:** `,
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `/ 1_000} µs/call")
```

Target: **1000 invocations under 10m`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `_args/1" do
    test "parses file path without options" do
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `r Path.join([__DIR__, "..", "fixtures"])

  setup_all do
   `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `nst structural rules.
  It does not read files or print outp`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `Parser and delegates
  to the validator. Handles exit codes `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `UIRED_KEYS", "name,version,type")
      |> String.split(",",`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `r,
    required_keys:
      Sys`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `r
# config/runtime.exs
import Config

if config_env() == :pr`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `alidator,
  verbose: false
```

```elixir
# config/prod.exs
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `}.exs"
```

```elixir
# config/dev.exs
import Config

config`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `[assert: 1, refute: 1]
]
```

The formatter ensures consiste`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `n: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md
**Preview:** `reate the project

**Objective**: Scaffold Mix project struc`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/json_validator/deps/excoveralls/README.md
**Preview:** `


config :excoveralls,
  http_options: [
    timeout: 10_00`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/json_validator/deps/jason/README.md
**Preview:** `


@derive {Jason.Encoder, only: [....]}
defstruct # ...`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** ` :cancelled]
  @transitions %{
    pending: [:confirmed, :ca`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** ` call of transition/2
      :ok
    end
  end)

IO.puts("Avg`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** `ok, :confirmed}
def parse_status(_), do: {:error, :unknown}
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** `o
  status = String.to_atom(status_string)  # Creates a NEW `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** ` async: true

  alias OrderFsm.Order

  describe "new/2" do
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** ` async: true

  alias OrderFsm.State

  describe "valid_tran`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** `te transitions.

  Each transition is validated against the `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** `transitions.

  States are atoms representing the order life`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md
**Preview:** `
end
```


### `lib/order_fsm/state.ex`

```elixir
defmodule`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `ing integer cents.

  All amounts are stored as integers rep`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `
      # representative call of split/2 over 10_000 iteratio`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `
iex> div(10, 3)   # Integer division — truncates toward zer`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `# Small integers (fits in a machine word) — fast, no allocat`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `xUnit.Case, async: true

  doctest Money

  describe "new/2"`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `onetary arithmetic using integer cents.

  All amounts are s`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `external dependencies required
  ]
end
```


### `lib/money.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** ` 10 + 20
30  # Exactly 30 cents. No rounding. No surprises.
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md
**Preview:** `x> 0.1 + 0.2
0.30000000000000004

iex> 0.1 + 0.2 == 0.3
fals`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** `o
      # representative call of deep_merge of two 5-level c`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** `ly works with atom keys, raises on missing key
config.server`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** `
      host: "localhost",
      port: 4000,
      pool_size:`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** ` do
  use ExUnit.Case, async: true

  doctest AppConfig

  d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** `Layered configuration system with deep merge and validation.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md
**Preview:** `o external dependencies required
  ]
end
```


### `lib/app_`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md
**Preview:** `    # representative call of evaluate/2 over 1M calls
      `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md
**Preview:** `on) do
    cond do
      user == nil -> :no_auth
      not i`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md
**Preview:** `nc: true

  alias FeatureFlagEvaluator

  describe "enabled?`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md
**Preview:** ` Evaluates feature flags using layered fallbacks.

  Priorit`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md
**Preview:** ` dependencies required
  ]
end
```


### `lib/feature_flag_e`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** `d)
result  # still :start
```
Always assign the `if` express`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** `-trace", "abc")  # Bug: return ignored
  conn  # returns unm`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** `r(amount) and amount >= 0 do
    %{amount: amount, status: :`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** ` async: true

  alias ConfigReloaderDemo

  describe "reload`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** `
  Demonstrates rebinding vs mutation and scope rules.

  Ev`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md
**Preview:** `rnal dependencies required
    {:"plug", "~> 1.0"},
  ]
end
`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `  # representative call of convert/3 over 100k invocations
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `EUR" => 0.85,
    "GBP" => 0.73,
    "JPY" => 110.0
  }

  d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `ase, async: true

  alias CurrencyPrecisionConverter, as: CC`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `oc """
  Converts money between currencies using integer min`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `
  use Mix.Project

  def project do
    [
      app: :curre`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md
**Preview:** `al dependencies required
    {:"ecto", "~> 1.0"},
  ]
end
```
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `uge_list  # rebuilds every call!
```
Fix: compute a `MapSet``
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `    # representative call of Registry.version/0 inlined vs f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `.example.com"
  @timeout_ms 5000
  @valid_endpoints ~w[users`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `ync: true

  alias VersionedSchemaRegistry, as: Registry

  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `Schema registry with compile-time version metadata.

  The c`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md
**Preview:** `endencies required
  ]
end
```


### `lib/versioned_schema_r`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `r, reason}, `with` returns it unchanged.
# Make sure the cal`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `on()  # MatchError if 3-element tuple
```
Always check the f`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `d — compiler sees the expected shape
{status, _headers, _bod`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `l of pattern match on `{:ok, body}` over 1M iterations
     `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `    case parse_json(body) do
      {:ok, data} -> {:ok, data`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `nc: true

  alias HttpClient.Api

  doctest HttpClient.Api

`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `.Case, async: true

  alias HttpClient.Response

  doctest H`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `with` for early exit on failure.

  Demonstrates how tagged `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `es into domain results.

  Follows the Elixir convention whe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md
**Preview:** `:"httpoison", "~> 1.0"},
    {:"jason", "~> 1.0"},
    {:"po`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `flection

Your team wrote a 50-line function with nested `ca`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** ` — asserts ref matches
receive do
  {:reply, ^ref, result} -`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `{priority: :critical}), do: CriticalHandler  # never reached`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `esentative call of multi-clause dispatch over 1M calls
     `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `error, reason}
```

Each clause is a separate pattern. Elixi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `, "method" => _}}) when is_binary(url) and byte_size(url) > `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `atchingTest do
  use ExUnit.Case, async: true

  alias TaskQ`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `s (from JSON, external APIs) into typed job payload structs.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `bs to the correct handler based on type, priority, and paylo`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md
**Preview:** `ired
  ]
end
```


### `lib/task_queue/job_router.ex`

```el`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `1..1_000 do
      # representative call of pinned match over`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `ected_type, user_id: expected_user} = event, ^expected_type,`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `t.Case, async: true

  alias PinnedEventDispatcher, as: Disp`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `c """
  Dispatches events by matching against pre-bound targ`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `ternal dependencies required
    {:"ecto", "~> 1.0"},
  ]
en`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md
**Preview:** `
  ^expected -> :ok           # matches only if response_cod`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `.1_000 do
      # representative call of route/1 over 100k w`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `person
```

You can extract and guard simultaneously, making`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** ``

Only keys you pattern-match on need to be presen`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `ush", "repository" => %{"full_name" => repo}}) do
    {:ok, `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `ase, async: true

  alias WebhookEventRouter, as: Router

  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `""
  Routes webhook payloads to handlers via partial map pat`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md
**Preview:** `ternal dependencies required
    {:"jason", "~> 1.0"},
  ]
e`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `n is_binary(email) <- user do
  {:ok, email}
else
  _ -> :er`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `.1_000 do
      # representative call of extract_author_emai`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `e both the full `data` and the extract`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `
```

Pattern matching works arbitrarily deep. You ext`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `ok, %{author: %{email: email}}}) when is_binary(email) do
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `ync: true

  alias JsonAstWalker, as: Walker

  describe "au`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `  Extracts fields from nested JSON-like structures using pat`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md
**Preview:** `external dependencies required
  ]
end
```


### `lib/json_a`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `1..1_000 do
      # representative call of can?/3 over 1M au`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `(payment)
  %{amount: a} when a <= 0 -> reject(payment)
  _ `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `s) do
    case {role, action, status} do
      {:admin, _act`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `t.Case, async: true

  alias RolePermissionMatcher, as: RBAC`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `doc """
  Evaluates permission requests with `case` + guards`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md
**Preview:** `external dependencies required
  ]
end
```


### `lib/role_p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `'ll grab the wrong
reply. Always `make_ref/0` and match on t`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `atRelay.Coordinator.register(coord, :a, a)
:ok = ChatRelay.C`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `send(pid, {:message, message})
  end

  defp loop(messages) `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `atRelay.{Coordinator, Worker}

  describe "end-to-end relay"`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `elopes and accumulates them.

  Exposes a synchronous `inbox`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `s messages between workers.

  Runs as a plain process (no G`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `chat_relay,
      version: "0.1.0",
      elixir: "~> 1.17",`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `Create the project

**Objective**: Set up a plain Mix projec`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md
**Preview:** `oenix", "~> 1.0"},
  ]
end
```


### Dependencies (`mix.exs``
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `k, ^i} = ReqResp.Client.call(pid, i, 1_000) end)
end)

IO.pu`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `    {:call, value, caller_pid, ref} ->
        send(caller_p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `p.{Client, EchoServer, Mailbox}

  test "call/3 returns {:ok`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `as ReqResp.EchoServer

  test "spawns a live process" do
   `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** ``GenServer.call/3`: send a tagged request, block on the
  ma`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `ame payload.

  This is deliberately NOT a GenServer — the g`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `ning purposes.

  In production code you rarely inspect the `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `### Step 4: `lib/req_resp/mailbox.ex`

**Objective**: Expose`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `    version: "0.1.0",
      elixir: "~> 1.17",
      start_p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `**: Split mailbox helpers, server, and client in`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
**Preview:** `0"},
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
d`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `..100, &"u#{&1}")

{t_serial, _} = :timer.tc(fn -> Enum.map(`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** ` process and returns a task reference. `Task.await` blocks u`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `k = Task.async(fn -> fetch_posts(user_id) end)

    user = T`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `Http, StreamFetcher}

  setup do
    Http.Fake.start(%{
    `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `}

  setup do
    Http.Fake.start(%{
      "https://a" => {:`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `his over `Fetcher.fetch_all/3` when:
    * The input list is`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** ` the classic "spawn N tasks, await N results" pattern. It's `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** ` lets tests inject a deterministic
  in-memory fake. This is`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `4: `lib/url_fetch/http.ex`

**Objective**: Define a behaviou`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `ersion: "0.1.0",
      elixir: "~> 1.17",
      start_perman`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `Create the project

**Objective**: Scaffold the project so H`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md
**Preview:** `es (`mix.exs`)

```elixir
defp deps do
  # No HTTP client — `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `her caller can mutate the state. Always use
`get_and_update/`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `00, fn _ -> CountAgent.Visits.hit(a, "/home") end)
end)

{t_`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `increment(agent_pid) do
    Agent.update(agent_pid, &(&1 + 1`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `e

  alias CountAgent.RateLimit

  test "allows up to `limit`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `ias CountAgent.Visits

  setup do
    {:ok, pid} = Visits.st`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `.

  Contract: `check(agent, key)` returns `:ok` if the requ`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `ent`.

  State shape: `%{page_id => count}` (a plain map).
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `

### Step 4: `lib/count_agent/visits.ex`

**Objective**: Pr`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `gent,
      version: "0.1.0",
      elixir: "~> 1.17",
     `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** ` project

**Objective**: Establish a standard Mix layout so `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md
**Preview:** `1.0"},
  ]
end
```


### Dependencies (`mix.exs`)

```elixir`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `end

Process.sleep(50)

{t, _} = :timer.tc(fn ->
  Enum.each`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** ` start_worker(name) do
    pid = spawn(fn -> worker_loop() e`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** ` Subscriber}

  test "receives and records events from its t`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `xUnit.Case, async: false

  alias EvtPubsub.Bus

  test "sub`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `lates events in a list, and replies to a
  `{:dump, caller_r`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `ing does NOT go through a single process — every publisher l`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `kbone.

  NOTE: We are using `Application` only to boot the `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `Step 4: `lib/evt_pubsub/application.ex`

**Objective**: impl`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `    version: "0.1.0",
      elixir: "~> 1.17",
      start_p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `p 1: Create the project

**Objective**: scaffold a new Mix p`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md
**Preview:** `
  ]
end
```


### Dependencies (`mix.exs`)

```elixir
defp `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md
**Preview:** `rker done") end)
  end

  @doc "worker_crash"
  def worker_crash do
    spawn_link`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md
**Preview:** `on" do
    test "crashing A also kills B" do
      # The tes`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md
**Preview:** ` — no GenServer, no
  Supervisor, just spawn_link and Proces`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md
**Preview:** `ject

**Objective**: Create a bare Mix project so the link `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md
**Preview:** `## Dependencies (`mix.exs`)

```elixir
defp deps do
  # BEAM`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md
**Preview:** ` -> worker_loop(name) end)
    ref = Process.monitor(pid)
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md
**Preview:** `cribe "monitor semantics" do
    test "watcher survives work`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md
**Preview:** `, monitors each, and counts deaths. The watcher is independe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md
**Preview:** `ep 1: Create the project

**Objective**: Scaffo`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md
**Preview:** `ncies required
  ]
end
```


### Dependencies (`mix.exs`)

``
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `.100_000, fn _ ->
    Process.send_after(self(), :noop, 60_0`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `_after(self(), {:delayed, "hello"}, 200)
    IO.puts("Messag`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `ue

  describe "schedule/3 + cancel/1" do
    test "cancelle`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `chedules a deadline message and lets the caller cancel or in`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** ``


### Step 1: Create the project

**Objective`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `al dependencies required
  ]
end
```


### Dependencies (`mi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md
**Preview:** `:tick, 1_000)
  {:ok, state}
end

def handle_info(:tick, sta`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
**Preview:** `d, id)
  end

  def get_request_id do
    Process.get(:reque`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
**Preview:** `e
  # async: false — BadContext leaks into the test runner's`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
**Preview:** `implemented with an Agent.

  Key differences from BadContex`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
**Preview:** ` context implemented with the process dictionary.

  This "w`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
**Preview:** `rnal dependencies required
    {:"phoenix", "~> 1.0"},
  ]
e`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `un the code samples in iex")
```

## Key Concepts

### 1. `I`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `nt is a named singleton shared across tests.
  use ExUnit.Ca`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `ue

  alias CalcRepl.Evaluator

  describe "basic arithmetic`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** ` a straight recursion that terminates when the user types `q`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `s NOT use `Code.eval_string/1` —
  evaluating user input as `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** ` Implemented as an Agent because the
  REPL loop calls `push`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** ` start(_type, _args) do
    children = [
      CalcRepl.Hist`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `alc_repl,
      version: "0.1.0",
      elixir: "~> 1.17",
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** `


### Step 1: Create the project

**Objective**: Recursive `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md
**Preview:** ``

---

## Why IO.gets recursion and not IO.stream

A one-sh`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `v")
    rows = for i <- 1..500_000, into: "id,val\n", do: "#`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `nd, not preloaded. This is essential for processing large fi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** ` run the code samples in iex")
```

## Key Concepts

### 1. `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `true
  alias CsvStream.Pipeline

  @tmp_dir Path.join(System`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `
  alias CsvStream.Parser

  describe "parse_line/1" do
    `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `er, transform, write.

  All stages are lazy. The file is re`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `transformer that invokes
  a callback every N rows without b`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `s and escaped quotes.

  Kept separate from the pipeline so `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** ```

### Step 4: `lib/csv_stream/parser.ex`

**Objective**: M`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `stream,
      version: "0.1.0",
      elixir: "~> 1.17",
   `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md
**Preview:** `## Step 1: Create the project

**Objective**: Streams via Fi`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `he code samples in iex")
```

## Key Concepts

### 1. `Path``
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `ue
  alias ScaffoldGen.Generator

  @tmp_root Path.join(Syst`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `
  alias ScaffoldGen.Renderer

  describe "render/2" do
    `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `ilities:
    * Validate the target path is safe to write int`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `ntents.

  Pure string manipulation — no IO. Isolated so we `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `ld produces.

  A blueprint is a list of directories to crea`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** ```

### Step 4: `lib/scaffold_gen/blueprint.ex`

**Objective`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** `fold_gen,
      version: "0.1.0",
      elixir: "~> 1.17",
 `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md
**Preview:** ` Step 1: Create the project

**Objective**: Path is pure str`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `erica/New_York", "Asia/Tokyo"]

    meetings =
      for i <`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `")
```

## Key Concepts

### 1. NaiveDateTime Has No Timezon`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `g, Formatter}

  test "render shows local time for the viewe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `verlap}

  defp meeting(id, start_naive, tz, duration, atten`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `describe "new/6" do
    test "converts a local naive datetim`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `ting so alternate renderers (HTML, iCal)
  can be added with`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `sort by start, then sweep. This is
  enough for team calenda`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `lly everything is stored as a UTC DateTime. The organiser
  `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `hed/meeting.ex`

**Objective**: NaiveDateTime for input; con`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `Etc/UTC raises.
config :elixir, :time_zone_database, Tzdata.`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `0",
      elixir: "~> 1.17",
      start_permanent: Mix.env(`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md
**Preview:** `
**Objective**: DateTime in UTC + organiser zone as metadata`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `/1"]

    {us, _} =
      :timer.tc(fn ->
        Enum.each(`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** ` samples in iex")
```

## Key Concepts

### 1. Keyword Lists`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `lient

  # Decode the stub's echo body so we can assert on w`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `.Options

  @schema [
    mode: [type: {:in, [:fast, :slow]}`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `s

  #{HttpOpts.Options.docs([
    method: [type: {:in, [:ge`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `k without
  touching the network or pulling in a test mockin`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** ` schema is a keyword list mapping each option name to a map `
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `tep 4: `lib/http_opts/options.ex`

**Objective**: Keyword.va`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `   version: "0.1.0",
      elixir: "~> 1.17",
      start_pe`
**Reason:** Invalid syntax

## /Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md
**Preview:** `,
    {:"httpoison", "~> 1.0"},
    {:"plug", "~> 1.0"},
   `
**Reason:** Invalid syntax
