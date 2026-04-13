# Property-based testing with StreamData

**Project**: `stream_data_intro` — a `Sort` module tested with
StreamData properties (idempotence, preservation of elements, and the
classic `Enum.sort` equivalence).

---

## Project context

Example-based tests only cover what you thought to test. Property-based
tests ask "what must be true for *any* input?" and StreamData generates
hundreds of inputs to try to break your assumption. When a property
fails, StreamData **shrinks** the counterexample to the smallest input
that still fails — so instead of `[5, 91, -3, 42, 0, 17, 88, 4]`, you
get `[1, 0]`.

Three properties cover more than a dozen example tests:
1. `sort(sort(xs)) == sort(xs)` (idempotence)
2. `sort(xs)` has the same elements as `xs` (no loss)
3. `sort(xs) == Enum.sort(xs)` (equivalence to the stdlib)

Project structure:

```
stream_data_intro/
├── lib/
│   └── sort.ex
├── test/
│   ├── sort_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why property testing and not exhaustive example tests

Examples solo cubren lo que te acordaste. Properties preguntan "¿qué
tiene que ser verdad para *cualquier* input?" y StreamData genera
cientos tratando de romper tu asunción. Shrinking convierte un
contraejemplo grande en el mínimo que aún falla — inmediatamente
debuggeable. Examples cubren corner cases conocidos; properties
cubren invariantes.

---

## Core concepts

### 1. `check all` runs your assertion against generated values

```elixir
use ExUnitProperties

property "reversing twice is identity" do
  check all list <- list_of(integer()) do
    assert list |> Enum.reverse() |> Enum.reverse() == list
  end
end
```

By default StreamData runs 100 iterations per property, increasing the
size of inputs as it goes.

### 2. Generators compose

```elixir
integer()              # any integer
positive_integer()     # > 0
list_of(integer())     # [int]
tuple({atom(), integer()})
map_of(string(:ascii), integer())
```

You can combine them with `bind`, `filter`, and `resize`.

### 3. Shrinking is the killer feature

When a property fails, StreamData tries progressively smaller inputs
until it finds the minimal one that still breaks the property. This
turns "it failed on some list of 60 integers" into "it failed on `[0, 0]`",
which is immediately debuggable.

### 4. Properties complement, don't replace, examples

Use examples for corner cases you *know* matter (empty input, nil
handling, the specific bug that once shipped). Use properties for the
invariants that should hold for all inputs.

---

## Design decisions

**Option A — Reemplazar todos los example tests por properties**
- Pros: Mayor cobertura de inputs.
- Cons: Properties tardan más; corner cases conocidos pueden quedar
  fuera del rango del generador.

**Option B — Properties + examples selectivos** (elegida)
- Pros: Examples dan feedback rápido; properties cubren el espacio
  entre ellos.
- Cons: Suite ligeramente más grande.

→ Elegida **B** porque ambos se complementan: examples son
regresiones documentadas, properties son invariantes.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {exunit},
    {exunitproperties},
    {stream_data},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new stream_data_intro
cd stream_data_intro
```

Add StreamData in `mix.exs`:

```elixir
defp deps do
  [{:stream_data, "~> 1.1", only: [:test]}]
end
```

Then `mix deps.get`.

### Step 2: `lib/sort.ex`

**Objective**: Implement `sort.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule Sort do
  @moduledoc """
  A didactic merge sort implementation — deliberately written from scratch
  so we can verify it against `Enum.sort/1` via properties.
  """

  @spec sort([number()]) :: [number()]
  def sort([]), do: []
  def sort([x]), do: [x]

  def sort(list) do
    {left, right} = Enum.split(list, div(length(list), 2))
    merge(sort(left), sort(right))
  end

  defp merge([], right), do: right
  defp merge(left, []), do: left
  defp merge([l | lt] = left, [r | rt] = right) do
    if l <= r do
      [l | merge(lt, right)]
    else
      [r | merge(left, rt)]
    end
  end
end
```

### Step 3: `test/sort_test.exs`

**Objective**: Write `sort_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule SortTest do
  use ExUnit.Case, async: true
  use ExUnitProperties

  # ── Example tests: fast feedback on known-interesting cases ──────────────

  describe "example-based" do
    test "empty list" do
      assert Sort.sort([]) == []
    end

    test "single element" do
      assert Sort.sort([42]) == [42]
    end

    test "already sorted" do
      assert Sort.sort([1, 2, 3]) == [1, 2, 3]
    end

    test "reverse sorted" do
      assert Sort.sort([3, 2, 1]) == [1, 2, 3]
    end
  end

  # ── Properties: invariants that must hold for ANY input ──────────────────

  describe "properties" do
    property "output is sorted in non-decreasing order" do
      check all list <- list_of(integer()) do
        sorted = Sort.sort(list)

        assert Enum.chunk_every(sorted, 2, 1, :discard)
               |> Enum.all?(fn [a, b] -> a <= b end)
      end
    end

    property "sort is idempotent: sort(sort(xs)) == sort(xs)" do
      check all list <- list_of(integer()) do
        once = Sort.sort(list)
        twice = Sort.sort(once)
        assert twice == once
      end
    end

    property "sort preserves elements (same multiset)" do
      check all list <- list_of(integer()) do
        assert Enum.sort(Sort.sort(list)) == Enum.sort(list)
        assert length(Sort.sort(list)) == length(list)
      end
    end

    property "equivalent to Enum.sort/1" do
      check all list <- list_of(integer()) do
        assert Sort.sort(list) == Enum.sort(list)
      end
    end

    property "works on floats too" do
      check all list <- list_of(float()) do
        assert Sort.sort(list) == Enum.sort(list)
      end
    end

    property "sorting a list of identical elements returns that list" do
      check all x <- integer(),
                n <- integer(0..50) do
        dup = List.duplicate(x, n)
        assert Sort.sort(dup) == dup
      end
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
mix test --seed 0   # pin StreamData's seed for reproducibility
```

If a property fails, StreamData prints the **shrunk counterexample** —
the minimal input that breaks the assertion — along with the original
failing input.

### Why this works

`check all list <- list_of(integer()), do: ...` genera N valores (100
por defecto), ejecuta la property con cada uno, y si falla comienza
el shrinking: progresivamente reduce el input mientras la property
siga fallando. El resultado final es el contraejemplo mínimo.

---

## Benchmark

```elixir
# mix test test/sort_test.exs
# Target: 100 iteraciones × propiedad × tamaño 10 elementos
# ~200ms por property en hardware moderno para algoritmos triviales.
```

Target esperado: propiedades simples 50–500ms cada una; >5s indica
generador mal escalado o costo cuadrático en el código bajo test.

---

## Trade-offs and production gotchas

**1. Properties are slower than examples**
100 iterations per property add up. A suite with 30 properties running
100 iterations each is 3,000 assertions. Fine for CI; annoying for
watch-mode TDD. Scope with `--only property` or
`ExUnitProperties.check all ..., max_runs: 25` during development.

**2. Bad generators hide bugs**
`list_of(integer())` defaults to integers in a moderate range. If your
bug only shows at `Integer.max()` or at the empty list, you might miss
it. Pair properties with a handful of explicit examples.

**3. "Found a counterexample" isn't always your fault**
StreamData might exercise a code path you genuinely don't support (e.g.
a list with `nil` elements). Either constrain the generator or narrow
the property's precondition.

**4. Shrinking has limits**
For complex nested generators, shrinking can take seconds or fail to
converge. Small, composable generators shrink best.

**5. When NOT to use property-based testing**
For simple CRUD code, for pure pattern-matching dispatch, and for IO
boundaries. Properties shine for **data structures**, **parsers**,
**serializers**, **stateful models** (via `StreamData` + state machines),
and **numerical code**.

---

## Reflection

- Escribís una property para un parser de CSV:
  `parse(serialize(x)) == x`. StreamData encuentra un contraejemplo
  con comillas anidadas. ¿El bug está en `parse`, `serialize`, o en
  tu definición de "x válido"?
- El equipo propone medir "coverage" de las properties igual que los
  tests. ¿Tiene sentido el mismo número? ¿Qué métrica alternativa
  mediría la calidad de una property-based suite?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Sort do
    @moduledoc """
    A didactic merge sort implementation — deliberately written from scratch
    so we can verify it against `Enum.sort/1` via properties.
    """

    @spec sort([number()]) :: [number()]
    def sort([]), do: []
    def sort([x]), do: [x]

    def sort(list) do
      {left, right} = Enum.split(list, div(length(list), 2))
      merge(sort(left), sort(right))
    end

    defp merge([], right), do: right
    defp merge(left, []), do: left
    defp merge([l | lt] = left, [r | rt] = right) do
      if l <= r do
        [l | merge(lt, right)]
      else
        [r | merge(left, rt)]
      end
    end
  end

  def main do
    IO.puts("=== PropertyTest Demo ===
  ")
  
    # Demo: Property-based test properties
  IO.puts("1. Testing string reverse idempotence...")
  IO.puts("   reverse(reverse(str)) == str")
  assert PropertyTest.test_double_reverse_identity("hello")

  IO.puts("2. Testing list length...")
  IO.puts("   length(list ++ list) == 2 * length(list)")
  assert PropertyTest.test_list_append_length([1, 2, 3])

  IO.puts("
  ✓ PropertyTest demos completed!")
  end

end

Main.main()
```


## Resources

- [StreamData — HexDocs](https://hexdocs.pm/stream_data/StreamData.html)
- [`ExUnitProperties`](https://hexdocs.pm/stream_data/ExUnitProperties.html)
- ["Property-based testing is a mindset" — Fred Hebert](https://ferd.ca/you-reap-what-you-code.html)
- [QuickCheck paper (Claessen & Hughes, 2000)](https://www.cs.tufts.edu/~nr/cs257/archive/john-hughes/quick.pdf) — the origin of property-based testing


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
