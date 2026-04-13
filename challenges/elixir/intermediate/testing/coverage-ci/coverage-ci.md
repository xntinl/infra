# Test coverage: `mix test --cover`, ExCoveralls, and CI integration

**Project**: `coverage_ci` — a `Calculator` module instrumented with
ExCoveralls, producing HTML and JSON reports suitable for Codecov or
Coveralls.io CI integration.

---

## Why coverage ci matters

Coverage answers one question: *which lines of my code has my test suite
never run?* It does NOT tell you whether the tested lines are correct.
Treat coverage as a **smell detector**, not a goal. Chasing 100% leads
to tests that exist only to bump the number.

Elixir ships with `mix test --cover` (via OTP's `:cover` module). The
output is spartan. ExCoveralls wraps it with HTML reports, per-file
breakdowns, and direct integration with Coveralls.io and Codecov. For
any codebase past "toy", ExCoveralls is the default.

---

## Project structure

```
coverage_ci/
├── lib/
│   └── coverage_ci.ex
├── script/
│   └── main.exs
├── test/
│   └── coverage_ci_test.exs
└── mix.exs
```

---

## Why ExCoveralls and not just `mix test --cover`

`mix test --cover` funciona sin deps pero el output es plano y sin
integración con CI services. ExCoveralls envuelve `:cover` y agrega
reports HTML, JSON, y `mix coveralls.github` para upload directo
desde GitHub Actions. Para cualquier proyecto más grande que un toy,
ExCoveralls es el default.

---

## Core concepts

### 1. Built-in `--cover` — minimum viable coverage

```bash
mix test --cover
```

Reports per-module percentage in the console. Uses OTP's `:cover`. Works
with zero deps. Output is plain text, no HTML, no per-line gutter.

### 2. ExCoveralls — the production choice

`mix coveralls`, `mix coveralls.html`, `mix coveralls.json`,
`mix coveralls.github` — the aliases Dashbit introduced that most
Elixir projects now use. HTML is great for local inspection; JSON is
the format coverage services ingest.

### 3. `minimum_coverage` — a failing threshold

```elixir
# mix.exs
test_coverage: [tool: ExCoveralls],
preferred_cli_env: [coveralls: :test, "coveralls.html": :test]
```

```elixir
# coveralls.json
{"coverage_options": {"minimum_coverage": 80}}
```

Below 80% the task exits non-zero. Your PR fails CI. Set the threshold
where your team is *now*, not where you wish you were — ratchet up over
time.

### 4. Ignoring files

Some files shouldn't count toward coverage: generated code, top-level
Mix tasks, `application.ex`. Use `:skip_files` in `coveralls.json`.

### 5. GitHub Actions integration

`mix coveralls.github` reads `GITHUB_TOKEN` and posts the coverage
report to coveralls.io / Codecov automatically. No plugins, no YAML
gymnastics.

---

## Design decisions

**Option A — `minimum_coverage` alto como aspiración (90%+)**
- Pros: Forza al equipo a escribir tests.
- Cons: Incentiva tests triviales; impide mergear bugfixes por 1%
  de drop.

**Option B — `minimum_coverage` como gate anti-regresión** (elegida)
- Pros: Bloquea caídas accidentales; no presiona a tests malos.
- Cons: No empuja el número hacia arriba por sí solo.

→ Elegida **B** (nivel actual menos 1–2 puntos). El número sube con
reviews honestos, no con gates agresivos.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — this ensures every environment starts with a fresh state.

```bash
mix new coverage_ci
cd coverage_ci
```

### `mix.exs`
**Objective**: Declare dependencies and project config in `mix.exs`.

```elixir
defmodule CoverageCi.MixProject do
  use Mix.Project

  def project do
    [
      app: :coverage_ci,
      version: "0.1.0",
      elixir: "~> 1.19",
      deps: deps(),
      test_coverage: [tool: ExCoveralls],
      preferred_cli_env: [
        coveralls: :test,
        "coveralls.detail": :test,
        "coveralls.html": :test,
        "coveralls.json": :test,
        "coveralls.github": :test
      ]
    ]
  end

  defp deps do
    [{:excoveralls, "~> 0.18", only: :test}]
  end

  def application do
    [extra_applications: [:logger]]
  end

end
```

### Step 3: `coveralls.json` (project root)

**Objective**: Provide `coveralls.json` (project root) — these are the supporting fixtures the main module depends on to make its concept demonstrable.

```json
{
  "coverage_options": {
    "minimum_coverage": 80,
    "treat_no_relevant_lines_as_covered": true
  },
  "skip_files": [
    "test/",
    "lib/coverage_ci/application.ex"
  ]
}
```

### `lib/calc.ex`

**Objective**: Implement `calc.ex` — the subject under test — shaped specifically to make the testing technique of this lab observable.

```elixir
defmodule Calc do
  @moduledoc """
  A small module with deliberate uncovered branches so the coverage
  report has something interesting to show.
  """

  @doc "Adds result from a and b."
  @spec add(number(), number()) :: number()
  def add(a, b), do: a + b

  @doc "Returns sub result from a and b."
  @spec sub(number(), number()) :: number()
  def sub(a, b), do: a - b

  @doc "Returns safe div result from _a."
  @spec safe_div(number(), number()) :: {:ok, float()} | {:error, :division_by_zero}
  def safe_div(_a, 0), do: {:error, :division_by_zero}
  @doc "Returns safe div result from a and b."
  def safe_div(a, b), do: {:ok, a / b}

  @doc """
  Deliberately partially covered — only the `:paid` branch is tested in
  the sample test file. The other branches will show up as uncovered in
  the HTML report, which is the point.
  """
  @spec describe_status(atom()) :: String.t()
  def describe_status(:paid), do: "settled"
  @doc "Returns describe status result."
  def describe_status(:pending), do: "awaiting payment"
  @doc "Returns describe status result."
  def describe_status(:refunded), do: "refunded"
  @doc "Returns describe status result from _other."
  def describe_status(_other), do: "unknown"
end
```

### Step 5: `test/calc_test.exs`

**Objective**: Write `calc_test.exs` exercising the exact ExUnit feature under study — assertions should fail loudly if the technique is misused.

```elixir
defmodule CalcTest do
  use ExUnit.Case, async: true

  doctest Calc

  describe "add/2 and sub/2" do
    test "add" do
      assert Calc.add(2, 3) == 5
    end

    test "sub" do
      assert Calc.sub(10, 4) == 6
    end
  end

  describe "safe_div/2" do
    test "happy path" do
      assert {:ok, 2.5} = Calc.safe_div(5, 2)
    end

    test "division by zero" do
      assert {:error, :division_by_zero} = Calc.safe_div(5, 0)
    end
  end

  describe "describe_status/1" do
    test "paid — only this clause is covered, by design" do
      assert Calc.describe_status(:paid) == "settled"
    end

    # Deliberately NOT testing :pending, :refunded, or the fallback —
    # the coverage report will light those up red. Adding tests for them
    # to hit 100% is the reader's exercise.
  end
end
```

### Step 6: `.github/workflows/ci.yml`

**Objective**: Implement `ci.yml` — the subject under test — shaped specifically to make the testing technique of this lab observable.

```yaml
name: CI

on:
  push:
    branches: [main]
  pull_request:

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: erlef/setup-beam@v1
        with:
          elixir-version: "1.16"
          otp-version: "26"
      - name: Cache deps
        uses: actions/cache@v4
        with:
          path: deps
          key: deps-${{ runner.os }}-${{ hashFiles('mix.lock') }}
      - name: Install deps
        run: mix deps.get
      - name: Run tests with coverage
        run: mix coveralls.github
        env:
          GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
```

### Step 7: Run locally

**Objective**: Run locally.

```bash
mix deps.get

# Plain OTP cover, no HTML:
mix test --cover

# ExCoveralls console:
mix coveralls

# Per-file detail (shows the uncovered lines in red):
mix coveralls.detail

# Full HTML report in cover/excoveralls.html:
mix coveralls.html
open cover/excoveralls.html
```

The HTML report lights up the uncovered `describe_status/1` branches in
red. Push to GitHub and `coveralls.github` uploads the report on every
PR.

### Why this works

ExCoveralls hookea `:cover` de OTP vía `test_coverage` en `mix.exs`,
recibe cobertura por línea, y formatea para distintos consumidores.
`mix coveralls.github` lee `GITHUB_TOKEN` y postea al API de
Coveralls/Codecov — sin plugins, sin YAML manual.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule CoverageCi.MixProject do
    use Mix.Project

    def project do
      [
        app: :coverage_ci,
        version: "0.1.0",
        elixir: "~> 1.19",
        deps: deps(),
        test_coverage: [tool: ExCoveralls],
        preferred_cli_env: [
          coveralls: :test,
          "coveralls.detail": :test,
          "coveralls.html": :test,
          "coveralls.json": :test,
          "coveralls.github": :test
        ]
      ]
    end

    defp deps do
      [{:excoveralls, "~> 0.18", only: :test}]
    end
  end

  def main do
    IO.puts("=== Calculator Demo ===
  ")
  
    # Demo: Call some functions for coverage
  IO.puts("1. Calculator.add(5, 3): #{Calculator.add(5, 3)}")
  assert Calculator.add(5, 3) == 8

  IO.puts("2. Calculator.multiply(4, 7): #{Calculator.multiply(4, 7)}")
  assert Calculator.multiply(4, 7) == 28

  IO.puts("
  (Run with: mix test --cover)")
  IO.puts("✓ Coverage demo completed!")
  end

end

Main.main()
```

## Benchmark

<!-- benchmark N/A: coverage es instrumentación de test time. El
costo relevante es el overhead de `:cover` durante el suite (10–30%
más lento). Se mide comparando `mix test` vs `mix coveralls` con
`time`, no con microbenchmarks. -->

---

## Trade-offs and production gotchas

**1. Coverage is a smell, not a goal**
95% coverage of the wrong things is worse than 70% of the critical path.
Coverage tools are a **guide** for finding under-tested modules, not a
KPI for OKRs.

**2. `minimum_coverage` is an anti-regression gate, not a target**
Set it to your current level minus 1–2 points. It blocks accidental
drops without creating pressure to write bad tests.

**3. Coverage counts executed lines, not correctness**
A line that runs in a test without any `assert` still counts as covered.
Pair coverage with a **mutation testing** tool (e.g. `muzak`) for real
confidence.

**4. Async tests + `:cover` were historically flaky**
OTP's `:cover` module has had race conditions with `async: true`
modules on Elixir <1.14. Modern versions handle it, but if you see
weird missing lines, try `async: false` to rule it out.

**5. CI token choice**
`coveralls.github` uses `GITHUB_TOKEN` (built into Actions, no setup).
`coveralls.post` uses the `COVERALLS_REPO_TOKEN` from coveralls.io.
`coveralls.json` + Codecov uploader is another path. Pick one and
document it in your README.

**6. When NOT to enforce coverage**
For throwaway spikes, CLI tools with trivial logic, and UI glue code.
Enforce on libraries, domain modules, and anything with business rules.

---

## Reflection

- Tu coverage está estancado en 78% por seis meses. El equipo propone
  subir el `minimum_coverage` a 85% para "forzar mejora". ¿Aceptás?
  ¿Qué propondrías en su lugar que mejore el testing sin incentivar
  tests triviales?
- Un bug crítico vino de un archivo con 100% coverage. Los tests
  llamaban la función pero no verificaban el resultado. ¿Qué agregás
  al proceso (review, mutation testing, otra cosa) para que coverage
  deje de mentir?

## Resources

- [`mix test --cover` — Mix task docs](https://hexdocs.pm/mix/Mix.Tasks.Test.html)
- [ExCoveralls](https://hexdocs.pm/excoveralls/readme.html)
- [OTP `:cover` module](https://www.erlang.org/doc/man/cover.html)
- [Coveralls.io Elixir setup](https://docs.coveralls.io/elixir)
- [setup-beam GitHub Action](https://github.com/erlef/setup-beam)

## Key concepts
ExUnit testing in Elixir balances speed, isolation, and readability. The framework provides fixtures, setup hooks, and async mode to achieve both performance and determinism.

**ExUnit patterns and fixtures:**
`setup_all` runs once per module (module-scoped state); `setup` runs before each test. Returning `{:ok, map}` injects variables into the test context. For side-effectful setup (e.g., starting supervised processes), use `start_supervised` — it automatically stops the process when the test ends, ensuring cleanup.

**Async safety and isolation:**
Tests with `async: true` run in parallel, but they must be isolated. Shared resources (database, ETS tables, Registry) require careful locking. A common pattern: `setup :set_myflag` — a private setup that configures a unique state for that test. Avoid global state unless protected by locks.

**Mocking trade-offs:**
Libraries like `Mox` provide compile-time mock modules that behave like real modules but with controlled behavior. The benefit: you catch missing function implementations at test time. The trade-off: mocks don't catch runtime errors (e.g., a real function that crashes). For critical paths, complement mocks with integration tests against real dependencies. Dependency injection (passing modules as arguments) is more testable than direct calls.

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/coverage_ci_test.exs`

```elixir
defmodule CoverageCiTest do
  use ExUnit.Case, async: true

  doctest CoverageCi

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert CoverageCi.run(:noop) == :ok
    end
  end
end
```
