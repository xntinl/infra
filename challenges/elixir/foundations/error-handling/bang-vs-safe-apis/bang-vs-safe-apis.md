# Bang (!) vs Safe APIs

**Project**: `config_loader` — a config loader exposing both `load/1` and `load!/1`.

---

## Project structure

```
config_loader/
├── lib/
│   └── config_loader.ex
├── config/
│   └── example.exs         # sample config file used by tests
├── script/
│   └── main.exs
├── test/
│   └── config_loader_test.exs
└── mix.exs
```

---

## The business problem

Elixir convention: a function ending in `!` (the "bang") raises on failure. Its non-bang
counterpart returns `{:ok, value} | {:error, reason}`. Callers pick based on how they
want to react:

- **Bang version** for code where failure is a bug that should crash loudly (e.g. loading critical config at boot).
- **Safe version** for code where failure is a branch to handle (e.g. optional feature flags).

Offering both costs you ~5 lines. Offering only one forces every caller into the wrong
shape half the time.

---

## Core concepts

### The convention

- `fun(args)` — returns `{:ok, value}` or `{:error, reason}`.
- `fun!(args)` — returns `value` directly, or raises on failure.

`!` versions are thin wrappers. The safe version contains the real logic; the bang
version pattern-matches and unwraps.

### When to expose both

- **Loading/parsing**: `File.read/1` vs `File.read!/1`, `Jason.decode/1` vs `Jason.decode!/1`. Both shapes are legitimate.
- **Lookup that might miss**: `Map.fetch/2` vs `Map.fetch!/2`. Same.
- **Pure computation on valid input**: no need for a bang — either it succeeds or it is a bug (raise directly).

### When NOT to expose both

If the bang version always succeeds (no failure mode), do not add it — it is noise.
If the safe version always succeeds, the `{:ok, _}` wrapping is noise — return the value.

---

## Why mirror and not choose one

**Option A — expose only the safe API; callers who want to crash wrap in `case`+`raise`**
- Pros: single API surface; no risk of confusion about which to call.
- Cons: every call site that "knows" the call must succeed grows a 5-line `case` that reports bad stack traces and no structured context.

**Option B — expose only the bang API; callers who want tuples wrap in `try/rescue`**
- Pros: ergonomic at confident call sites.
- Cons: `try/rescue` allocates; every caller that wants branching pays 10×; code becomes exception-driven.

**Option C — expose both; bang delegates to safe** (chosen)
- Pros: each call site opts in; bang is a one-line delegate so there is no duplication; matches stdlib patterns (`File.read` / `File.read!`).
- Cons: API size doubles.

→ Chose **C** because call sites genuinely differ. The cost of mirror pairs is negligible (one-line delegate); the cost of forcing every caller to adapt is borne forever.

---

## Design decisions

**Option A — safe returns `value | nil`, bang raises on `nil`**
- Pros: concise; no tuple boxing.
- Cons: `nil` is ambiguous — a config key whose value is `nil` is indistinguishable from "missing"; callers cannot differentiate.

**Option B — safe returns `{:ok, value} | {:error, reason}`, bang unwraps or raises** (chosen)
- Pros: disambiguates "found `nil`" from "not found"; `reason` carries structured context; `with` composition is direct.
- Cons: more verbose at the call site; the `{:ok, _}` wrapping is redundant in the common success case.

→ Chose **B** because config loaders serve as the source of truth for other components. Ambiguity about "missing vs. nil" causes production bugs that are hard to trace back.

---

## Implementation

### `mix.exs`
```elixir
defmodule ConfigLoader.MixProject do
  use Mix.Project

  def project do
    [
      app: :config_loader,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Step 1: Create the project

**Objective**: scaffold a new Mix project and set up the directory layout for the exercise.

```bash
mix new config_loader
cd config_loader
mkdir -p config
```

### Step 2: `config/example.exs` — a sample config for tests

**Objective**: provide a sample config file used as a fixture for the loader tests.

```elixir
# This file is used by tests. It is a plain Elixir file evaluated by Code.eval_file/1.
%{
  database_url: "postgres://localhost/app",
  pool_size: 10,
  feature_flags: %{new_ui: true}
}
```

### `lib/config_loader.ex`

**Objective**: implement config_loader — loads a config file and validates its shape.

```elixir
defmodule ConfigLoader do
  @moduledoc """
  Loads a config file and validates its shape.

  Public API:

    * `load/1`  — safe version, returns {:ok, config} | {:error, reason}
    * `load!/1` — bang version, returns config or raises
  """

  @type config :: map()
  @type reason :: :enoent | :not_a_map | {:eval_error, Exception.t()}

  @doc """
  Loads and evaluates the config at `path`.

  Returns a tagged tuple — prefer this in code paths that can recover
  (e.g. falling back to defaults when an optional config is missing).
  """
  @spec load(Path.t()) :: {:ok, config()} | {:error, reason()}
  def load(path) when is_binary(path) do
    with {:ok, _} <- stat(path),
         {:ok, term} <- eval(path),
         :ok <- ensure_map(term) do
      {:ok, term}
    end
  end

  @doc """
  Same as `load/1` but unwraps on success and raises on failure.

  Use at boot-time for mandatory config: if this fails, the app cannot
  start — raising is the correct response.
  """
  @spec load!(Path.t()) :: config()
  def load!(path) when is_binary(path) do
    # The bang version is a 3-line wrapper. All real logic lives in load/1.
    # If you fix a bug in load/1, load!/1 inherits the fix automatically —
    # that is the whole reason we keep them symmetric.
    case load(path) do
      {:ok, cfg} ->
        cfg

      {:error, reason} ->
        # We raise a structured error so callers CAN rescue if they must.
        # Message includes path — boot-time logs need enough context to debug.
        raise ArgumentError, "failed to load config at #{inspect(path)}: #{inspect(reason)}"
    end
  end

  # -------------------------------------------------------------------------
  # private helpers — intentionally small so `load/1` reads like a checklist
  # -------------------------------------------------------------------------

  defp stat(path) do
    case File.stat(path) do
      {:ok, _} -> {:ok, path}
      {:error, :enoent} -> {:error, :enoent}
      {:error, other} -> {:error, other}
    end
  end

  defp eval(path) do
    # Code.eval_file is dangerous with untrusted input — config files are
    # trusted source here. For untrusted input, parse as TOML/JSON instead.
    {term, _bindings} = Code.eval_file(path)
    {:ok, term}
  rescue
    e in RuntimeError -> {:error, {:eval_error, e}}
  end

  defp ensure_map(term) when is_map(term), do: :ok
  defp ensure_map(_), do: {:error, :not_a_map}
end
```

### Step 4: `test/config_loader_test.exs`

**Objective**: cover config_loader_test with ExUnit tests for the public API and representative edge cases.

```elixir
defmodule ConfigLoaderTest do
  use ExUnit.Case, async: true
  doctest ConfigLoader

  @example_path Path.expand("../config/example.exs", __DIR__)

  describe "load/1 — safe version" do
    test "returns {:ok, config} on success" do
      assert {:ok, cfg} = ConfigLoader.load(@example_path)
      assert cfg.database_url == "postgres://localhost/app"
      assert cfg.pool_size == 10
    end

    test "returns {:error, :enoent} for a missing file" do
      assert {:error, :enoent} = ConfigLoader.load("does/not/exist.exs")
    end

    test "returns {:error, :not_a_map} if the file evaluates to a non-map" do
      path = Path.join(System.tmp_dir!(), "bad_config_#{System.unique_integer([:positive])}.exs")
      File.write!(path, "[1, 2, 3]")
      on_exit(fn -> File.rm(path) end)

      assert {:error, :not_a_map} = ConfigLoader.load(path)
    end
  end

  describe "load!/1 — bang version" do
    test "unwraps on success" do
      cfg = ConfigLoader.load!(@example_path)
      # The bang version returns the bare value — no {:ok, _} to destructure.
      assert is_map(cfg)
      assert cfg.pool_size == 10
    end

    test "raises ArgumentError with path in message on failure" do
      assert_raise ArgumentError, ~r/does\/not\/exist/, fn ->
        ConfigLoader.load!("does/not/exist.exs")
      end
    end
  end

  test "bang and safe versions agree on success" do
    # This invariant is why we implement one on top of the other.
    {:ok, safe} = ConfigLoader.load(@example_path)
    bang = ConfigLoader.load!(@example_path)
    assert safe == bang
  end
end
```

### Step 5: Run tests

**Objective**: run the test suite and confirm all tests pass.

```bash
mix test
```

### Why this works

`load!/1` delegates to `load/1` and raises on `{:error, reason}`. There is one implementation of "what a valid config looks like" and one implementation of "what errors can occur". The bang variant exists purely to pick the error-handling *strategy* (crash vs. branch). Because bang reuses safe, the two APIs cannot drift — a new rule added to `load/1` is automatically enforced by `load!/1`. The cost of mirroring is one line per function; the payoff is that every caller can pick the ergonomics it needs without sacrificing consistency.

### `script/main.exs`

```elixir
defmodule Main do
  def main do
    IO.puts("=== ConfigLoader: demo ===\n")

    result_1 = Mix.env()
    IO.puts("Demo 1: #{inspect(result_1)}")

    result_2 = Exception.t()
    IO.puts("Demo 2: #{inspect(result_2)}")
    IO.puts("\n=== Done ===")
  end
end

Main.main()
```

Run with: `elixir script/main.exs`

---

## Benchmark

Compare the two paths on the happy case to confirm the mirror pattern has no measurable cost:

```elixir
good_path = "test/fixtures/valid.exs"

{us_safe, _} = :timer.tc(fn -> for _ <- 1..10_000, do: ConfigLoader.load(good_path) end)
{us_bang, _} = :timer.tc(fn -> for _ <- 1..10_000, do: ConfigLoader.load!(good_path) end)

IO.puts("safe: #{us_safe / 10_000} µs")
IO.puts("bang: #{us_bang / 10_000} µs")
```

Target esperado: safe and bang within noise (<5% difference) on the happy path — bang is just an `{:ok, v} -> v` unwrap. On the failing path, bang pays 10–20× the cost because it raises, so reserve it for call sites where failure is a bug.

---

## Trade-offs

| Style | Best for |
|-------|----------|
| Safe only (no bang) | The failure is routine; callers always want to branch |
| Bang only (no safe) | Failure means a bug; callers should never "handle" it (rare) |
| Both | Uncertain — callers vary; let them pick |
| Neither, return value directly | Function cannot fail on valid input |

For a public library: when in doubt, expose **both**. The bang version is a one-line
delegate.

---

## Common production mistakes

**1. Implementing the bang version first, then wrapping in try/rescue for the safe version**
This leaks raise-based control flow into a supposedly safe API. Do the opposite:
implement safe, then the bang unwraps. Cleaner, faster, and the safe version never has
to construct a stack trace.

**2. Using `!` for "this is the preferred one"**
`!` means "raises on failure", not "use this one". Do not name the safe-and-slow version
`load/1` and the fast-but-raises version `load!/1` — the caller expects `!` = bang
semantics, not a performance hint.

**3. Swallowing too much in the safe version**
`rescue _ -> {:error, :unknown}` hides real bugs. Rescue only the specific exceptions
you expect (IO errors, parse errors) — let programmer errors surface.

**4. Inconsistent error shape between `fun/1` and `fun!/1`**
If `load/1` returns `{:error, :enoent}`, `load!/1` should raise an exception whose
`reason` field is `:enoent`, not a raw string. The two should be round-trippable.

**5. Bang function that returns a tagged tuple**
`load!(path) :: {:ok, map()}` defeats the whole point. Bang versions return the value.

---

## When NOT to use

- **No failure mode** — if the function cannot fail, no bang needed.
- **Internal helpers** — pick one style. Consistency inside a module matters more than completeness.
- **Expected failure in hot paths** — raise is slow (stack trace capture). Stick with tagged tuples where performance matters.

---

## Reflection

- Your library exposes `load/1` and `load!/1`. A year later, you add a `load/2` that takes defaults. Do you also add `load!/2`? What rule do you follow so the mirror never drifts, and how do you enforce it in CI?
- A caller writes `config = ConfigLoader.load!(path) || default`. This compiles but never triggers the default (bang raises, never returns `nil`). How would you surface this misuse — credo rule, dialyzer spec, or a `load_with_default/2`? Which is the least intrusive?

---

## Resources

- [Elixir docs — Naming conventions](https://hexdocs.pm/elixir/naming-conventions.html#trailing-bang-foo)
- [File module source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/file.ex) — the canonical example: `File.read/1` and `File.read!/1`
- [Jason source](https://github.com/michalmuskala/jason/blob/master/lib/jason.ex) — `decode/2` and `decode!/2` mirrored APIs

---

## Why Bang (!) vs Safe APIs matters

Mastering **Bang (!) vs Safe APIs** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/config_loader_test.exs`

```elixir
defmodule ConfigLoaderTest do
  use ExUnit.Case, async: true

  doctest ConfigLoader

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert ConfigLoader.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
### 1. Bang Functions (`!`) Raise on Failure; Safe Functions Return `{:ok, value}` / `{:error, reason}`
`File.read` returns error tuple. `File.read!` raises. Use bang functions when certain of success. Use safe functions when failure is possible.

### 2. When to Use Bang Functions
Use bang functions for configuration parsing and known-safe data. Use safe functions for user input, external systems, and file I/O.

### 3. Wrap Bang Functions for Predictability
In production code, wrap bang functions to handle errors without raising. This gives you flexibility without creating wrapper modules everywhere.

---
