# Test coverage: `mix test --cover`, ExCoveralls, and CI integration

**Project**: `coverage_ci` — a `Calculator` module instrumented with
ExCoveralls, producing HTML and JSON reports suitable for Codecov or
Coveralls.io CI integration.

**Difficulty**: ★★★☆☆
**Estimated time**: 2–3 hours

---

## Project context

Coverage answers one question: *which lines of my code has my test suite
never run?* It does NOT tell you whether the tested lines are correct.
Treat coverage as a **smell detector**, not a goal. Chasing 100% leads
to tests that exist only to bump the number.

Elixir ships with `mix test --cover` (via OTP's `:cover` module). The
output is spartan. ExCoveralls wraps it with HTML reports, per-file
breakdowns, and direct integration with Coveralls.io and Codecov. For
any codebase past "toy", ExCoveralls is the default.

Project structure:

```
coverage_ci/
├── lib/
│   └── calc.ex
├── test/
│   ├── calc_test.exs
│   └── test_helper.exs
├── .github/
│   └── workflows/
│       └── ci.yml
└── mix.exs
```

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

## Implementation

### Step 1: Create the project

```bash
mix new coverage_ci
cd coverage_ci
```

### Step 2: `mix.exs`

```elixir
defmodule CoverageCi.MixProject do
  use Mix.Project

  def project do
    [
      app: :coverage_ci,
      version: "0.1.0",
      elixir: "~> 1.15",
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
```

### Step 3: `coveralls.json` (project root)

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

### Step 4: `lib/calc.ex`

```elixir
defmodule Calc do
  @moduledoc """
  A small module with deliberate uncovered branches so the coverage
  report has something interesting to show.
  """

  @spec add(number(), number()) :: number()
  def add(a, b), do: a + b

  @spec sub(number(), number()) :: number()
  def sub(a, b), do: a - b

  @spec safe_div(number(), number()) :: {:ok, float()} | {:error, :division_by_zero}
  def safe_div(_a, 0), do: {:error, :division_by_zero}
  def safe_div(a, b), do: {:ok, a / b}

  @doc """
  Deliberately partially covered — only the `:paid` branch is tested in
  the sample test file. The other branches will show up as uncovered in
  the HTML report, which is the point.
  """
  @spec describe_status(atom()) :: String.t()
  def describe_status(:paid), do: "settled"
  def describe_status(:pending), do: "awaiting payment"
  def describe_status(:refunded), do: "refunded"
  def describe_status(_other), do: "unknown"
end
```

### Step 5: `test/calc_test.exs`

```elixir
defmodule CalcTest do
  use ExUnit.Case, async: true

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

## Resources

- [`mix test --cover` — Mix task docs](https://hexdocs.pm/mix/Mix.Tasks.Test.html)
- [ExCoveralls](https://hexdocs.pm/excoveralls/readme.html)
- [OTP `:cover` module](https://www.erlang.org/doc/man/cover.html)
- [Coveralls.io Elixir setup](https://docs.coveralls.io/elixir)
- [setup-beam GitHub Action](https://github.com/erlef/setup-beam)
