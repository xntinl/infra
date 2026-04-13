# ExUnit basics: `test`, `setup`, `describe`, `assert`

**Project**: `exunit_basics` — a minimal `Calculator` module and a thorough
test suite that exercises the core ExUnit primitives.

---

## Project context

Every Elixir project ships with ExUnit. Before you reach for Mox, StreamData,
or CaptureLog, you need fluent use of the four basic primitives: `test/2`,
`assert/1`, `describe/2`, and `setup/1`. Everything else is built on top.

This is a deliberately small exercise. The module under test is a calculator
with division-by-zero handling — it exists only as a scaffold to demonstrate
ExUnit features: context maps, setup composition, `describe` grouping,
`assert_raise`, and the difference between `async: true` and `async: false`.

Project structure:

```
exunit_basics/
├── lib/
│   └── calculator.ex
├── test/
│   ├── calculator_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why ExUnit primitives and not a BDD framework

ExUnit cubre 95% de los casos con cuatro primitivas (`test`, `assert`,
`describe`, `setup`) sin dependencias ni DSL extra. Un framework BDD
agrega vocabulario y ceremonia que se paga en cada test. Para la
mayoría de suites, ExUnit sin adornos es más rápido de escribir y
más claro.

---

## Core concepts

### 1. `use ExUnit.Case, async: true`

`async: true` tells ExUnit to run this module's tests in parallel with other
async modules. Use it whenever the module doesn't touch shared mutable state
(named processes, the database, the filesystem). Parallelism is the cheapest
way to make your suite faster.

### 2. `describe/2` groups tests, `test/2` defines them

`describe` does not isolate state — it's purely organizational. Its main
benefit is that failures report as `"MyModule.function/arity ▸ describe name ▸ test name"`,
which makes large suites navigable.

### 3. `setup/1` returns context

`setup` runs before every test in the module (or `describe` block). It can
return `:ok` (no context) or `{:ok, map}` / a plain `map`, which ExUnit
merges into the test context. Tests then destructure the context in their
signature: `test "…", %{user: user} do ... end`.

### 4. `assert` is macro magic

`assert left == right` unrolls into a rich error that shows both sides,
the diff, and the file/line. You almost never need `assert_equal` or
`assert_eq` — just `assert a == b`, `assert a > b`, etc.

---

## Design decisions

**Option A — `setup` en cada `describe` con helpers locales**
- Pros: Cada bloque autónomo.
- Cons: Duplicación cuando varios `describe` necesitan el mismo
  fixture.

**Option B — `setup` a nivel módulo + override por `describe`** (elegida)
- Pros: Fixtures comunes en un lugar; overrides específicos donde
  importa.
- Cons: Lector debe mirar dos niveles.

→ Elegida **B** elimina duplicación sin sacrificar aislamiento.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {error},
    {exunit},
    {ok},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new exunit_basics
cd exunit_basics
```

### Step 2: `lib/calculator.ex`

**Objective**: Implement `calculator.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule Calculator do
  @moduledoc """
  A tiny pure module used to exercise ExUnit primitives.
  """

  @spec add(number(), number()) :: number()
  def add(a, b), do: a + b

  @spec sub(number(), number()) :: number()
  def sub(a, b), do: a - b

  @spec mul(number(), number()) :: number()
  def mul(a, b), do: a * b

  @doc "Integer division. Raises `ArithmeticError` if the divisor is zero."
  @spec div(integer(), integer()) :: integer()
  def div(_a, 0), do: raise(ArithmeticError, message: "division by zero")
  def div(a, b) when is_integer(a) and is_integer(b), do: Kernel.div(a, b)

  @doc "Safe division that returns a result tuple instead of raising."
  @spec safe_div(integer(), integer()) :: {:ok, integer()} | {:error, :division_by_zero}
  def safe_div(_a, 0), do: {:error, :division_by_zero}
  def safe_div(a, b), do: {:ok, Kernel.div(a, b)}
end
```

### Step 3: `test/calculator_test.exs`

**Objective**: Write `calculator_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule CalculatorTest do
  use ExUnit.Case, async: true

  # `setup` runs before *every* test in this module.
  # It returns a map that ExUnit merges into the test context.
  setup do
    {:ok, numbers: %{a: 10, b: 3}}
  end

  describe "add/2, sub/2, mul/2" do
    test "add returns the sum", %{numbers: %{a: a, b: b}} do
      assert Calculator.add(a, b) == 13
    end

    test "sub returns the difference", %{numbers: %{a: a, b: b}} do
      assert Calculator.sub(a, b) == 7
    end

    test "mul is commutative" do
      # You can ignore the context entirely if you don't need it.
      assert Calculator.mul(4, 5) == Calculator.mul(5, 4)
    end
  end

  describe "div/2 — raising API" do
    test "divides integers" do
      assert Calculator.div(10, 3) == 3
    end

    test "raises on zero divisor" do
      # `assert_raise` both asserts the exception type AND returns the
      # exception struct so you can inspect its fields if you want.
      assert_raise ArithmeticError, "division by zero", fn ->
        Calculator.div(10, 0)
      end
    end
  end

  describe "safe_div/2 — tuple API" do
    test "returns {:ok, result} for non-zero divisor" do
      assert {:ok, 5} = Calculator.safe_div(10, 2)
    end

    test "returns {:error, :division_by_zero} for zero divisor" do
      assert {:error, :division_by_zero} = Calculator.safe_div(10, 0)
    end
  end

  describe "describe-level setup" do
    # `describe` blocks can have their own setup that composes with the
    # module-level one. Both run; describe-level runs last.
    setup %{numbers: base} do
      # Extend the context with a derived value.
      {:ok, Map.put(base, :sum, base.a + base.b) |> then(&%{numbers: &1})}
    end

    test "context is augmented inside describe", %{numbers: numbers} do
      assert numbers.sum == 13
    end
  end
end
```

### Step 4: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.


```bash
mix test
mix test --trace   # shows every test name as it runs
mix test test/calculator_test.exs:45  # run only the test at line 45
```

### Why this works

`test/2` define una función que ExUnit detecta por macro. `setup/1`
registra un callback que corre antes de cada test en su propio
proceso, y su return se mergea en el context map. `describe/2` solo
agrupa — el aislamiento viene de que cada test corre en su propio
proceso, con `async: true` permitiendo paralelismo entre módulos.

---

## Benchmark

<!-- benchmark N/A: el overhead de ExUnit por test está dominado por
el setup del usuario (IO, creación de procesos). El framework en sí
agrega <100µs por test. Un microbenchmark del framework no aporta. -->

---

## Trade-offs and production gotchas

**1. `async: true` and shared state don't mix**
If your test uses a globally-named GenServer, a database connection pool
without sandboxing, or the filesystem under a fixed path — set `async: false`.
The race condition *will* show up eventually on CI.

**2. `setup` runs per test, `setup_all` runs once per module**
`setup_all` is for expensive one-time fixtures (seeding a read-only DB).
Anything mutable belongs in `setup` to keep tests isolated.

**3. `describe` doesn't nest**
Elixir intentionally does not allow nested `describe` blocks. If you feel
you need nesting, it's a signal your tests are too coupled — split the
module.

**4. Don't overuse `assert` on booleans**
`assert foo == :ok` is clearer than `assert foo === :ok` or `assert :ok = foo`
for most readers. Pattern-match (`assert {:ok, _} = result`) when you want
to *also* destructure.

**5. When NOT to write a test**
Trivial getters, one-line delegates, and generated boilerplate rarely need
dedicated tests — they'll break in integration tests if they break at all.
Test behavior, not plumbing.

---

## Reflection

- Tu suite tiene 200 tests y corre en 8s. Descubrís que 60% del
  tiempo está en `setup` con fixtures pesados. ¿Cómo decidís qué
  mover a `setup_all` sin romper `async: true`? Formulá una
  checklist de invariantes.
- Un test falla intermitentemente solo en CI. Sospechás race
  condition con `async: true`. ¿Qué señales mirás primero en el test
  y en el código bajo test para confirmar?

---

## Resources

- [`ExUnit` — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.html)
- [`ExUnit.Case` — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.Case.html)
- [`ExUnit.Callbacks` (setup/setup_all) — HexDocs](https://hexdocs.pm/ex_unit/ExUnit.Callbacks.html)
- ["Testing" — Elixir getting started guide](https://hexdocs.pm/elixir/introduction-to-mix.html#running-tests)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
