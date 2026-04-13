# Error Tuples vs Raise: A Validation Library with Two APIs

**Project**: `validate_lib` — a tiny validation library offering both safe (tuple-based) and bang (raising) APIs

---

## Core concepts in this exercise

1. **The `{:ok, value} | {:error, reason}` convention** — why idiomatic Elixir libraries
   shape their results as 2-tuples and how to compose them.
2. **Bang functions (`!`)** — the companion convention where the same operation raises
   instead of returning an error tuple, and when each is appropriate.

---

## Why this matters for a senior developer

Every library in the Elixir standard library follows this pair:

```
File.read/1          → {:ok, content} | {:error, posix}
File.read!/1         → content | (raises)

Map.fetch/2          → {:ok, value} | :error
Map.fetch!/2         → value | (raises KeyError)

Jason.decode/1       → {:ok, term} | {:error, %Jason.DecodeError{}}
Jason.decode!/1      → term | (raises)
```

Offering both is not duplication — it's respecting two legitimate call styles:

- **Safe**: the caller expects failure frequently and must handle it (user input,
  parsing external data, I/O).
- **Bang**: the caller knows the input is valid and a failure means the system is
  broken (loading a config file that must exist, parsing a constant during boot).

Designing a clean dual API is a senior skill. Get the convention wrong and every
downstream consumer pays the cost.

---

## Project structure

```
validate_lib/
├── lib/
│   └── validate_lib/
│       ├── safe.ex
│       ├── bang.ex
│       └── rules.ex
├── test/
│   └── validate_lib/
│       ├── safe_test.exs
│       └── bang_test.exs
└── mix.exs
```

---

## The business problem

Your team maintains a user signup endpoint (safe API, errors routed to the UI) and
a fixture loader for integration tests (bang API, any failure should crash the
test run immediately).

Both must share the same validation rules. You'll build the rules once and expose
them through two thin facades.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"jason", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Organize Rules, Safe, Bang into separate modules so safe/bang facades stay thin wrappers over shared validation.

```bash
mix new validate_lib
cd validate_lib
```

### Step 2: `mix.exs`

**Objective**: Use zero dependencies so dual API pattern (safe/bang) is visible without framework abstractions obscuring it.

```elixir
defmodule ValidateLib.MixProject do
  use Mix.Project

  def project do
    [
      app: :validate_lib,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: []
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end
end
```

### Step 3: `lib/validate_lib/rules.ex`

**Objective**: Return structured errors like {:too_short, min} so callers can i18n without string parsing.

```elixir
defmodule ValidateLib.Rules do
  @moduledoc """
  Pure validation rules. Every function returns `{:ok, value} | {:error, reason}`.

  The `:error` reason is a structured tuple like `{:too_short, min}` so callers can
  localize messages without parsing strings. Never return bare strings from a
  library function — the consumer owns the presentation layer.
  """

  @type result(v) :: {:ok, v} | {:error, term()}

  @doc """
  Ensures `value` is a non-empty binary.
  """
  @spec required(term()) :: result(binary())
  def required(value) when is_binary(value) and byte_size(value) > 0, do: {:ok, value}
  def required(_), do: {:error, :required}

  @doc """
  Ensures a binary length is at least `min`.
  """
  @spec min_length(binary(), pos_integer()) :: result(binary())
  def min_length(value, min) when is_binary(value) and byte_size(value) >= min do
    {:ok, value}
  end

  def min_length(value, min) when is_binary(value) do
    # Returning the parameter in the error makes i18n trivial: the caller has
    # enough structured data to render a localized message.
    {:error, {:too_short, min}}
  end

  def min_length(_, _), do: {:error, :not_a_string}

  @doc """
  Ensures a binary looks like an email. Not a real parser — adequate for a demo.
  """
  @spec email(binary()) :: result(binary())
  def email(value) when is_binary(value) do
    if Regex.match?(~r/^[^@\s]+@[^@\s]+\.[^@\s]+$/, value) do
      {:ok, value}
    else
      {:error, :bad_email}
    end
  end

  def email(_), do: {:error, :not_a_string}
end
```

### Step 4: `lib/validate_lib/safe.ex`

**Objective**: Tag each failure with its field name at the facade boundary so Rules stays oblivious to which form slot it is validating.

```elixir
defmodule ValidateLib.Safe do
  @moduledoc """
  The tuple-returning API. This is what HTTP handlers and user-facing code use.

  Every function returns `{:ok, value}` on success or `{:error, reason}`.
  Composition uses `with`, which short-circuits on the first error.
  """

  alias ValidateLib.Rules

  @doc """
  Validates a signup payload: %{email: _, password: _}.

  Returns the input map on success for easy piping into the next step.
  """
  @spec validate_signup(map()) :: {:ok, map()} | {:error, {atom(), term()}}
  def validate_signup(params) when is_map(params) do
    # `with` short-circuits: if any clause fails to match `{:ok, _}`, the else
    # branch runs. We wrap the error in a tuple that names the offending field —
    # the caller needs to know WHICH validation failed, not just that SOMETHING did.
    with {:ok, email} <- Rules.required(Map.get(params, :email)),
         {:ok, email} <- Rules.email(email),
         {:ok, password} <- Rules.required(Map.get(params, :password)),
         {:ok, password} <- Rules.min_length(password, 8) do
      {:ok, %{email: email, password: password}}
    else
      # Tagging the field at the boundary. `Rules` stays dumb — it doesn't know
      # which form field it's validating.
      {:error, reason} -> {:error, tag_error(reason, params)}
    end
  end

  def validate_signup(_), do: {:error, {:payload, :not_a_map}}

  # Pattern-matching on the specific errors to tag the field name. Written as
  # small clauses instead of a big `case` for readability.
  defp tag_error(:required, params) do
    cond do
      Map.get(params, :email) in [nil, ""] -> {:email, :required}
      true -> {:password, :required}
    end
  end

  defp tag_error(:bad_email, _), do: {:email, :bad_email}
  defp tag_error({:too_short, min}, _), do: {:password, {:too_short, min}}
  defp tag_error(other, _), do: {:unknown, other}
end
```

### Step 5: `lib/validate_lib/bang.ex`

**Objective**: Make Bang a three-line wrapper over Safe so the two APIs physically cannot drift in the validation rules they enforce.

```elixir
defmodule ValidateLib.Bang do
  @moduledoc """
  The raising API. Used by code paths where invalid data is a bug, not a
  runtime condition — fixture loaders, tests, config parsers at boot time.

  Convention: every function here mirrors a `Safe.*` counterpart, has a `!`
  suffix, returns the value directly, and raises `ValidateLib.ValidationError`
  on failure.
  """

  alias ValidateLib.Safe

  defmodule ValidationError do
    # Carrying the structured reason lets the rescuer still pattern-match,
    # even though we expect this exception to propagate and crash the process.
    defexception [:reason, message: "validation failed"]

    @impl true
    def exception(reason) do
      %__MODULE__{reason: reason, message: "validation failed: #{inspect(reason)}"}
    end
  end

  @doc """
  Like `Safe.validate_signup/1` but raises on failure. Returns the map directly.
  """
  @spec validate_signup!(map()) :: map()
  def validate_signup!(params) do
    case Safe.validate_signup(params) do
      {:ok, valid} -> valid
      {:error, reason} -> raise ValidationError, reason
    end
  end
end
```

**Why this pattern:**

- `Bang` is a one-line wrapper around `Safe`. No duplicated validation logic.
- The exception carries the structured `:reason` so tests can assert specifically
  (`assert_raise ValidationError, ~r/email/`). A string-only exception would force
  brittle regex matching.
- `Safe.*` is the source of truth. `Bang.*` is the facade. If both modules had
  their own rules, they would inevitably drift.

### Step 6: Tests — `test/validate_lib/safe_test.exs`

**Objective**: Pin the exact `{:field, reason}` shape so i18n and telemetry callers can rely on the tags rather than message strings.

```elixir
defmodule ValidateLib.SafeTest do
  use ExUnit.Case, async: true

  alias ValidateLib.Safe

  describe "validate_signup/1 — success" do
    test "returns {:ok, map} for a valid payload" do
      assert {:ok, %{email: "a@b.co", password: "longenough"}} =
               Safe.validate_signup(%{email: "a@b.co", password: "longenough"})
    end
  end

  describe "validate_signup/1 — field errors" do
    test "flags missing email" do
      assert {:error, {:email, :required}} =
               Safe.validate_signup(%{email: "", password: "longenough"})
    end

    test "flags bad email format" do
      assert {:error, {:email, :bad_email}} =
               Safe.validate_signup(%{email: "not-an-email", password: "longenough"})
    end

    test "flags missing password" do
      assert {:error, {:password, :required}} =
               Safe.validate_signup(%{email: "a@b.co", password: ""})
    end

    test "flags short password with the minimum in the reason" do
      assert {:error, {:password, {:too_short, 8}}} =
               Safe.validate_signup(%{email: "a@b.co", password: "abc"})
    end
  end

  describe "validate_signup/1 — payload shape errors" do
    test "rejects non-map payloads" do
      assert {:error, {:payload, :not_a_map}} = Safe.validate_signup("nope")
      assert {:error, {:payload, :not_a_map}} = Safe.validate_signup(nil)
    end
  end
end
```

### Step 7: Tests — `test/validate_lib/bang_test.exs`

**Objective**: Assert the exception's `:reason` field equals the Safe tuple so the two APIs stay provably equivalent for observability.

```elixir
defmodule ValidateLib.BangTest do
  use ExUnit.Case, async: true

  alias ValidateLib.Bang
  alias ValidateLib.Bang.ValidationError

  describe "validate_signup!/1" do
    test "returns the value directly on success" do
      assert %{email: "a@b.co", password: "longenough"} =
               Bang.validate_signup!(%{email: "a@b.co", password: "longenough"})
    end

    test "raises ValidationError with a structured reason" do
      err =
        assert_raise ValidationError, fn ->
          Bang.validate_signup!(%{email: "bad", password: "longenough"})
        end

      # The exception's :reason field matches what Safe would have returned.
      # This is what makes the bang API testable without parsing messages.
      assert err.reason == {:email, :bad_email}
    end

    test "raises for non-map input" do
      assert_raise ValidationError, fn ->
        Bang.validate_signup!("not a map")
      end
    end
  end
end
```

### Step 8: Run and verify

**Objective**: Run with warnings-as-errors to catch unused `Rules` or unreachable pattern clauses that would hide API drift.

```bash
mix test --trace
mix compile --warnings-as-errors
```

All 8 tests must pass.

### Why this works

Both facades delegate to the same `Rules` module, so there is exactly one implementation of "what counts as a valid email". The `Safe` facade is a pure function of input → `{:ok, value} | {:error, reason}`, which composes with `with`. The `Bang` facade is a thin wrapper that calls `Safe` and raises `ValidationError` on `{:error, _}` — it never duplicates logic, never drifts from `Safe`. The bang version's exception carries `reason:` that equals the tuple's reason, so tests and observability can assert on the same key in both worlds.

---



---
## Key Concepts

### 1. Use Tuples for Expected Failures

Expected failures are part of normal business logic: validation fails, a query returns no rows, a network request times out. Handle these with `{:ok, value}` / `{:error, reason}`. This pattern threads errors through your code explicitly and prevents silent failures.

### 2. Raise Only on Programmer Errors

Programmer errors are bugs—violations of preconditions that should never happen in production. You raise because this is a failure of the programmer's contract, not a failure of the system.

### 3. Exception Handling is Expensive

Raising and catching involves stack unwinding. For frequently-occurring failures (validation), use tuples. For rare, unexpected errors, raising is fine. In a loop processing 1 million items where validation fails on 10%, tuples are much faster.

---
## Design decisions

**Option A — one facade with a `raise?: true` option**
- Pros: single entry point; fewer functions to document.
- Cons: the return type depends on a runtime flag; dialyzer cannot narrow it; the bang convention (`!`) is a machine-checkable signal, and you throw it away.

**Option B — two facades (`Safe` and `Bang`) over one shared `Rules`** (chosen)
- Pros: each facade has a single return type; callers opt in at call site; dialyzer and tooling understand the contract; follows the stdlib pattern (`Map.fetch` vs `Map.fetch!`).
- Cons: two modules; someone must keep them in sync (trivial here because `Bang` is 3 lines per function).

→ Chose **B** because it is the idiom the whole stdlib is built on. Your callers already know how `!` behaves; do not surprise them.

---

## Why two APIs and not just the safe one

**Option A — only the safe API; callers who want to crash can pattern-match and raise themselves**
- Pros: minimum surface; no duplication.
- Cons: every boot script and test fixture now carries a `case … do {:ok, v} -> v; {:error, r} -> raise "#{inspect r}" end` five-line wrapper. Stack traces point at the wrapper, not the caller.

**Option B — expose both; bang delegates to safe** (chosen)
- Pros: ergonomic "assert this succeeds" at boot/test sites; stack traces point at the real call; bang is trivial to write because it reuses safe.
- Cons: two functions per rule; API surface doubles.

→ Chose **B** because the two call sites (UI validation vs. boot-time assertion) have genuinely different error ergonomics, and emulating bang at every caller is strictly worse than writing it once.

---

## Trade-off analysis

| Aspect                        | Tuple API (`Safe`)                          | Bang API (`Bang`)                        |
|-------------------------------|---------------------------------------------|------------------------------------------|
| Call site ergonomics          | `with` chains or explicit `case`            | Looks like any pure function             |
| Failure handling              | Every caller must handle                    | Unhandled = process crash                |
| Where it belongs              | Controllers, external I/O, user input       | Boot scripts, test fixtures, invariants  |
| Pipe-friendliness             | Awkward inside `|>` (you need wrappers)     | Perfect — the value is the return        |
| Debugging a failure           | You see the error tuple in logs             | You see a full stack trace               |
| Cost of unexpected failure    | Ignored tuple → silent data loss            | Process dies → supervisor restarts       |

The two APIs are complementary, not redundant. Offer both; document both; let the
caller choose.

---

## Common production mistakes

**1. A bang function that returns `{:error, _}`**
By convention, `foo!` never returns an error tuple. If it can fail, it raises.
Returning `{:error, _}` from a bang function violates the contract every Elixir
programmer expects from the `!` suffix. They'll forget to handle it.

**2. A non-bang function that raises on "normal" failures**
If `parse/1` raises on invalid input, callers have to wrap every call in `try`.
That defeats the point of the tuple convention. Raise only on programmer errors.

**3. `{:error, "Email is invalid"}` — strings as reasons**
Strings kill composability. Callers can't pattern-match; i18n is impossible without
regex; logging and metrics have no structured key. Always return atoms or tuples.

**4. Nested `case` instead of `with`**
```elixir
case Rules.required(email) do
  {:ok, e} ->
    case Rules.email(e) do
      {:ok, e2} -> ...
      ...
    end
  ...
end
```
Five-deep pyramid code. Use `with` — it's built for exactly this.

**5. Forgetting the `else` clause in `with`**
`with` without `else` returns whatever the first non-matching clause produced —
which may leak internal types. Add `else` to normalize errors at the boundary.

**6. Duplicating validation logic in `Safe` and `Bang`**
The bang wrapper should be literally `case Safe.x/1 do {:ok, v} -> v; {:error, r} -> raise ...`.
Copy-pasting rules is how the two APIs drift.

---

## When NOT to offer a bang version

- The operation fails frequently in normal use (parsing user input). Don't even
  tempt callers to skip error handling.
- The error carries essential business meaning the UI needs (`:email_taken`).
  Forcing callers through the tuple path keeps the business logic visible.
- The call is inside a pipeline where the next step depends on the error type.
  Tuples make that routing trivial; exceptions obscure it.

---

## Benchmark

Compare safe vs. bang on the happy path, then compare the cost of the failing bang (raise + rescue) against the failing safe:

```elixir
good = %{email: "a@b.co", password: "longenough"}
bad  = %{email: "x", password: "short"}

{us_safe_ok, _} = :timer.tc(fn -> for _ <- 1..100_000, do: ValidateLib.Safe.validate_signup(good) end)
{us_bang_ok, _} = :timer.tc(fn -> for _ <- 1..100_000, do: ValidateLib.Bang.validate_signup!(good) end)

{us_safe_err, _} = :timer.tc(fn -> for _ <- 1..100_000, do: ValidateLib.Safe.validate_signup(bad) end)

{us_bang_err, _} = :timer.tc(fn ->
  for _ <- 1..100_000 do
    try do
      ValidateLib.Bang.validate_signup!(bad)
    rescue
      _ -> :ok
    end
  end
end)

IO.inspect({us_safe_ok, us_bang_ok, us_safe_err, us_bang_err})
```

Target esperado: safe and bang indistinguishable on the happy path (<0.5 µs); on the failing path, bang is 10–20× more expensive than safe because the raise allocates an exception struct and unwinds the stack. That is the quantitative reason bang is wrong in a hot validation loop.

---

## Reflection

- A teammate proposes adding a `validate_signup/2` that takes `mode: :safe | :bang` instead of two functions. What do you lose in static analysis (dialyzer) and IDE affordances? How would you explain the loss without invoking "because the stdlib does it that way"?
- If the `Rules` module grows to 40 checks, the tuple API accumulates `{:field, :reason}` atoms that must be translated for UI. Do you invent a `Reason` struct, return `{:error, [%{field:, code:, message:}]}`, or keep the flat atoms and translate at the edge? What's the blast radius if you need to add localisation later?

---

## Resources

- [Elixir `Kernel.SpecialForms.with/1`](https://hexdocs.pm/elixir/Kernel.SpecialForms.html#with/1) — the canonical tool for composing `{:ok, _} | {:error, _}`
- [Elixir Style Guide — return values](https://github.com/christopheradams/elixir_style_guide#return-values-naming) — the community convention for `!` and `?`
- [`File` module source](https://github.com/elixir-lang/elixir/blob/main/lib/elixir/lib/file.ex) — read `read/1` and `read!/1` side by side, they are the textbook example
- [Elixir in Action, 2nd ed. — Ch. 8: Error Handling](https://www.manning.com/books/elixir-in-action-second-edition) — extended treatment
