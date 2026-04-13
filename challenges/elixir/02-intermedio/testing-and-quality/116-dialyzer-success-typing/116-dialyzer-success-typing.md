# Dialyxir, PLTs, and interpreting Dialyzer warnings

**Project**: `dialyzer_demo` — a `Billing` module with deliberately planted
type errors, wired up to Dialyxir so you can read real warnings and fix
them one at a time.

---

## Project context

Dialyzer is Erlang/OTP's static analyzer. In Elixir we use it through
Dialyxir. The first run feels painful: it takes minutes and prints
cryptic messages. Past that wall, it's the cheapest guard you have
against `nil`-creep, wrong-arity calls, and contract violations — and
it's free, on by default, and ships with OTP.

This exercise plants three classic mistakes — a `nil` return that
violates the spec, a function that can never match, and a
contract-violating call site — then walks you through reading the
warning and fixing it.

Project structure:

```
dialyzer_demo/
├── lib/
│   ├── billing.ex
│   └── reports.ex
├── test/
│   ├── billing_test.exs
│   └── test_helper.exs
└── mix.exs
```

---

## Why Dialyzer and not TypeScript-style gradual typing

Elixir es dinámicamente tipado por diseño. Dialyzer aplica *success
typing*: solo marca código que **no puede ser correcto** para ningún
input. Eso minimiza falsos positivos al costo de perder algunos bugs
reales. Es la única opción compatible con código sin `@spec` y con
las features dinámicas del lenguaje.

---

## Core concepts

### 1. Success typing in one paragraph

Dialyzer does not try to prove your code is correct. It proves your code
is **not wrong**. If a call site cannot succeed for any input type, it
warns. If it *might* succeed, Dialyzer stays quiet. This minimizes false
positives at the cost of missing some real bugs. In practice, most real
bugs Dialyzer catches are high-signal.

### 2. PLT — the Persistent Lookup Table

Dialyzer caches its analysis of OTP and your dependencies in a PLT file.
First run builds it (minutes). Subsequent runs only re-analyze what
changed (seconds). In CI, **cache the PLT** — it's the difference between
a 5-minute and a 30-second Dialyzer step.

### 3. The four warnings you'll see most

- **`pattern_match`**: a clause can never match because of the types.
- **`call_to_missing`**: you're calling a function that doesn't exist in
  that module/arity.
- **`contract_supertype`** / **`contract_subtype`**: your `@spec` is
  wider or narrower than the inferred type.
- **`no_return`**: Dialyzer proved this function always raises or loops.
  Often a sign of an unreachable code path.

### 4. `.dialyzer_ignore.exs` for legacy

When introducing Dialyzer to an existing codebase, you'll get hundreds of
warnings. Don't fix them all before shipping — ignore them in
`.dialyzer_ignore.exs` and fix incrementally. New code should be
warning-free from day one.

---

## Design decisions

**Option A — Dialyzer solo localmente**
- Pros: Sin costo en CI.
- Cons: Warnings se acumulan; nadie los ve hasta que explotan.

**Option B — Dialyzer en CI con PLT cacheado** (elegida)
- Pros: Gate de calidad continuo; caché amortiza el costo.
- Cons: Un job CI más.

→ Elegida **B** porque el valor de Dialyzer depende de que no se
acumulen warnings.

---

### Dependencies (`mix.exs`)

```elixir
def deps do
  [
    {dialyxir},
    {exunit},
    {no_warn},
  ]
end
```
## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.


```bash
mix new dialyzer_demo
cd dialyzer_demo
```

Add Dialyxir in `mix.exs`:

```elixir
defp deps do
  [{:dialyxir, "~> 1.4", only: [:dev, :test], runtime: false}]
end

defp project do
  [
    app: :dialyzer_demo,
    version: "0.1.0",
    elixir: "~> 1.15",
    deps: deps(),
    dialyzer: [
      plt_add_apps: [:mix],
      plt_file: {:no_warn, "priv/plts/dialyzer.plt"},
      # Use the flags you want for your project — these are sensible defaults.
      flags: [:error_handling, :underspecs, :unknown]
    ]
  ]
end
```

### Step 2: `lib/billing.ex` — the clean version

**Objective**: Edit `billing.ex` — the clean version, exposing the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule Billing do
  @moduledoc """
  Billing calculations — clean, fully specced. No Dialyzer warnings expected.
  """

  @type invoice :: %{id: String.t(), total: non_neg_integer(), status: :draft | :sent | :paid}

  @spec mark_paid(invoice()) :: invoice()
  def mark_paid(%{status: :draft}), do: raise("cannot pay a draft")
  def mark_paid(invoice), do: %{invoice | status: :paid}

  @spec total_of([invoice()]) :: non_neg_integer()
  def total_of([]), do: 0
  def total_of(invoices), do: Enum.reduce(invoices, 0, &(&1.total + &2))
end
```

### Step 3: `lib/reports.ex` — the buggy version (fix as an exercise)

**Objective**: Edit `reports.ex` — the buggy version (fix as an exercise), exposing the subject under test — shaped specifically to make the testing technique of this lab observable.


```elixir
defmodule Reports do
  @moduledoc """
  Deliberately buggy. Run `mix dialyzer` — you should see warnings for
  each numbered issue below. Fix them one at a time, re-running Dialyzer.
  """

  # ── Issue 1: spec says non_neg_integer() but function can return nil ──
  @spec grand_total([Billing.invoice()]) :: non_neg_integer()
  def grand_total([]), do: nil
  def grand_total(invoices), do: Billing.total_of(invoices)

  # ── Issue 2: unreachable pattern — a string can never be an atom ──
  @spec status_label(:draft | :sent | :paid) :: String.t()
  def status_label(:draft), do: "Draft"
  def status_label(:sent), do: "Sent"
  def status_label(:paid), do: "Paid"
  def status_label("unknown"), do: "Unknown"

  # ── Issue 3: calls a function with the wrong argument type ──
  @spec report([Billing.invoice()]) :: String.t()
  def report(invoices) do
    # `total_of/1` expects a list — calling it on a single invoice is wrong.
    sum = Billing.total_of(List.first(invoices))
    "Total: #{sum}"
  end
end
```

**The fixes, for reference:**

```elixir
# Fix 1: return 0, not nil (or widen the spec to include nil — but returning
# 0 is almost always the right thing for an empty-sum).
def grand_total([]), do: 0

# Fix 2: remove the unreachable clause, or widen the spec to allow strings.
# Deletion is correct here.

# Fix 3: pass the whole list, not just the first element.
sum = Billing.total_of(invoices)
```

### Step 4: `test/billing_test.exs`

**Objective**: Write `billing_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.


```elixir
defmodule BillingTest do
  use ExUnit.Case, async: true

  describe "mark_paid/1" do
    test "transitions a sent invoice to paid" do
      inv = %{id: "1", total: 1000, status: :sent}
      assert %{status: :paid} = Billing.mark_paid(inv)
    end

    test "raises on a draft invoice" do
      assert_raise RuntimeError, "cannot pay a draft", fn ->
        Billing.mark_paid(%{id: "1", total: 0, status: :draft})
      end
    end
  end

  describe "total_of/1" do
    test "sums invoice totals" do
      invs = [
        %{id: "1", total: 100, status: :paid},
        %{id: "2", total: 250, status: :paid}
      ]

      assert Billing.total_of(invs) == 350
    end

    test "empty list sums to zero" do
      assert Billing.total_of([]) == 0
    end
  end
end
```

### Step 5: Run and read the warnings

**Objective**: Run and read the warnings.


```bash
mix deps.get
mix dialyzer  # first run takes minutes, builds the PLT
```

You should see warnings for each of the three planted issues in `Reports`.
Read them, fix them, re-run until clean. Then commit the fixes.

### Why this works

Dialyzer construye un Persistent Lookup Table (PLT) con los tipos de
OTP + dependencias. Luego analiza tu código aplicando success typing:
cada call site contra el tipo inferido de la función destino. Si las
intersecciones de tipos demuestran imposibilidad, emite warning. Todo
es estático — el bytecode y runtime quedan intactos.

---

## Benchmark

<!-- benchmark N/A: Dialyzer no tiene código runtime. La medida es
tiempo de PLT build (minutos primera vez, segundos incremental) y
análisis por módulo (sub-segundo). Se mide con `time mix dialyzer`. -->

---

## Trade-offs and production gotchas

**1. First-run cost is real — cache the PLT in CI**
```yaml
# GitHub Actions example:
- uses: actions/cache@v4
  with:
    path: priv/plts
    key: plt-${{ runner.os }}-${{ hashFiles('mix.lock') }}
```
Without this, every PR build pays the 3+ minute PLT cost.

**2. `any()` silences Dialyzer and destroys its value**
A spec with `any()` is Dialyzer's "I don't know" signal — it will propagate
through every caller and silence real bugs. If you need breadth, use
union types (`integer() | :error`), not `any()`.

**3. Dialyzer warnings are *advisory* — not all are bugs**
Some warnings are over-eager (especially `unmatched_return`). Read the
warning, decide, and either fix or add to `.dialyzer_ignore.exs` with a
comment explaining why.

**4. `mix dialyzer --format dialyxir` is much more readable**
The default format is terse. Dialyxir ships a friendlier formatter — use
it locally.

**5. When NOT to invest in Dialyzer**
Pure scripts, one-off Mix tasks, prototypes. The PLT cost and spec
discipline aren't worth it until the codebase is stable. Turn it on when
the codebase reaches ~5k lines or you start a library.

---

## Reflection

- Heredás un codebase con 400 warnings de Dialyzer. ¿Arreglás
  incrementalmente o escribís un `.dialyzer_ignore.exs` gigante y
  decretás "warnings-clean desde hoy"? Justificá en términos de
  riesgo de regresión.
- Dialyzer marca una cláusula como "pattern_match_cov". El equipo
  insiste en falso positivo porque "nunca llega ese input". ¿Cómo
  resolvés: con test de integración, refactor, o aceptando el
  warning?

---

## Executable Example

Copy the code below into a file (e.g., `solution.exs`) and run with `elixir solution.exs`:

```elixir
defmodule Main do
  defmodule Reports do
    @moduledoc """
    Deliberately buggy. Run `mix dialyzer` — you should see warnings for
    each numbered issue below. Fix them one at a time, re-running Dialyzer.
    """

    # ── Issue 1: spec says non_neg_integer() but function can return nil ──
    @spec grand_total([Billing.invoice()]) :: non_neg_integer()
    def grand_total([]), do: nil
    def grand_total(invoices), do: Billing.total_of(invoices)

    # ── Issue 2: unreachable pattern — a string can never be an atom ──
    @spec status_label(:draft | :sent | :paid) :: String.t()
    def status_label(:draft), do: "Draft"
    def status_label(:sent), do: "Sent"
    def status_label(:paid), do: "Paid"
    def status_label("unknown"), do: "Unknown"

    # ── Issue 3: calls a function with the wrong argument type ──
    @spec report([Billing.invoice()]) :: String.t()
    def report(invoices) do
      # `total_of/1` expects a list — calling it on a single invoice is wrong.
      sum = Billing.total_of(List.first(invoices))
      "Total: #{sum}"
    end
  end

  def main do
    IO.puts("=== TypeChecker Demo ===
  ")
  
    # Demo: Dialyzer type checking
  IO.puts("1. safe_divide(10, 2): #{TypeChecker.safe_divide(10, 2)}")
  assert TypeChecker.safe_divide(10, 2) == {:ok, 5}

  IO.puts("2. safe_divide(10, 0): #{inspect(TypeChecker.safe_divide(10, 0))}")
  assert TypeChecker.safe_divide(10, 0) == {:error, :zero_division}

  IO.puts("
  ✓ Dialyzer demo completed!")
  end

end

Main.main()
```


## Resources

- [Dialyxir](https://hexdocs.pm/dialyxir/readme.html)
- [Erlang Dialyzer manual](https://www.erlang.org/doc/man/dialyzer.html)
- ["Success Typings for Erlang" — Lindahl & Sagonas, 2006](https://user.it.uu.se/~kostis/Papers/succ_types.pdf)
- [Learn You Some Erlang: Dialyzer](https://learnyousomeerlang.com/dialyzer)
- ["Making Dialyzer Play Nice with Your CI" — Dashbit blog](https://dashbit.co/blog)


## Key Concepts

ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.
