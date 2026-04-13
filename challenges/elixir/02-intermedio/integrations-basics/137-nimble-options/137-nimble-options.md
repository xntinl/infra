# Validating library options with NimbleOptions

**Project**: `nimble_opts_demo` — a small HTTP-client library skeleton
whose `start_link/1` validates its options with
[NimbleOptions](https://hexdocs.pm/nimble_options/), generating
documentation from the schema and raising clear errors on misuse.

---

## Project context

Elixir libraries idiomatically take options as keyword lists:
`start_link(name: MyServer, timeout: 5_000, retries: 3)`. Without
validation, a typo like `timout: 5000` silently ignores the setting or
surfaces hours later as a weird runtime error.

`NimbleOptions` is the Elixir Core Team's answer. It's a tiny dependency
(no transitive deps), used by Broadway, Nx, Ecto, Phoenix LiveView, and
many others to:

1. **Validate** a keyword list at startup — fail fast with a clear message.
2. **Generate documentation** from the schema so your `@moduledoc` stays
   in sync with the actual accepted options.

This exercise is quick — the library is small — but the pattern is used
everywhere.

Project structure:

```
nimble_opts_demo/
├── lib/
│   └── nimble_opts_demo.ex
├── test/
│   └── nimble_opts_demo_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Schemas are keyword lists

```elixir
schema = [
  name: [type: :atom, required: true, doc: "Registered process name."],
  timeout: [type: :pos_integer, default: 5_000, doc: "Request timeout, ms."]
]
```

Each entry is `key: [opts]`. Common opts: `:type`, `:required`, `:default`,
`:doc`, `:deprecated`, `:keys` (for nested maps/keyword lists).

### 2. `validate/2` and `validate!/2`

```elixir
NimbleOptions.validate(opts, schema)
# => {:ok, opts_with_defaults_applied} | {:error, %NimbleOptions.ValidationError{}}

NimbleOptions.validate!(opts, schema)  # raises on error, returns opts_with_defaults
```

The returned opts have defaults filled in, so downstream code can
`Keyword.fetch!/2` without checking presence.

### 3. Rich type system

- Scalars: `:atom`, `:string`, `:boolean`, `:integer`, `:pos_integer`,
  `:non_neg_integer`, `:float`.
- Collections: `{:list, subtype}`, `{:map, key_type, value_type}`,
  `:keyword_list`, `:non_empty_keyword_list`.
- Compound: `{:or, [type1, type2]}`, `{:in, [:a, :b, :c]}`,
  `{:tuple, [types]}`.
- Escape hatch: `{:custom, Mod, fun, args}` — your validator.

### 4. `NimbleOptions.docs/2`

Generates a Markdown table of options from the schema, which you embed in
`@moduledoc` via `@schema |> NimbleOptions.docs()`. Your docs can never
drift from your validation.

---

## Design decisions

**Option A — hand-rolled `Keyword.fetch!/2` + `case` validations per option**
- Pros: no dependency; totally explicit; easy to step through in a debugger.
- Cons: typos like `timout:` silently ignored; docs drift from code; every library reinvents the same shape; error messages are ad hoc.

**Option B — declare a schema and call `NimbleOptions.validate!/2` (chosen)**
- Pros: fail-fast on unknown keys and wrong types; auto-generate docs from the same schema via `NimbleOptions.docs/1`; consistent error messages across libraries; tiny dep with no transitive deps.
- Cons: one more thing to learn; schema vocabulary (`{:or, ...}`, `{:list, {:tuple, ...}}`) can get dense for complex configs.

→ Chose **B** because every library in the Dashbit orbit (Broadway, Nx, LiveView) uses it, and the docs-stay-in-sync guarantee alone pays for itself.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
    {:"phoenix", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new nimble_opts_demo
cd nimble_opts_demo
```

Deps in `mix.exs`:

```elixir
defp deps do
  [{:nimble_options, "~> 1.1"}]
end
```

### Step 2: `lib/nimble_opts_demo.ex`

**Objective**: Implement `nimble_opts_demo.ex` — the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule NimbleOptsDemo do
  @moduledoc """
  A tiny HTTP-client-like façade that validates options using
  `NimbleOptions`. Highlights:

    * schema lives at the top of the module
    * `@moduledoc` includes auto-generated option docs
    * `start_link/1` raises with a clear message on misuse

  ## Options

  #{NimbleOptions.docs(
    [
      name: [
        type: :atom,
        required: true,
        doc: "Registered process name for this client."
      ],
      base_url: [
        type: :string,
        required: true,
        doc: "Base URL to prepend to all requests."
      ],
      timeout: [
        type: :pos_integer,
        default: 5_000,
        doc: "Request timeout in milliseconds."
      ],
      retries: [
        type: :non_neg_integer,
        default: 2,
        doc: "How many times to retry on transient failures."
      ],
      strategy: [
        type: {:in, [:linear, :exponential]},
        default: :exponential,
        doc: "Retry backoff strategy."
      ],
      headers: [
        type: {:list, {:tuple, [:string, :string]}},
        default: [],
        doc: "Extra headers sent with every request."
      ],
      adapter: [
        type: {:or, [:atom, {:tuple, [:atom, :keyword_list]}]},
        default: :finch,
        doc: "Adapter backend. Either an atom or `{adapter, opts}`."
      ]
    ]
  )}
  """

  # Duplicate the schema as module attribute so validate/1 can reference
  # it at runtime. (You can also keep only one copy and derive docs from
  # the module attribute.)
  @schema [
    name: [type: :atom, required: true],
    base_url: [type: :string, required: true],
    timeout: [type: :pos_integer, default: 5_000],
    retries: [type: :non_neg_integer, default: 2],
    strategy: [type: {:in, [:linear, :exponential]}, default: :exponential],
    headers: [type: {:list, {:tuple, [:string, :string]}}, default: []],
    adapter: [
      type: {:or, [:atom, {:tuple, [:atom, :keyword_list]}]},
      default: :finch
    ]
  ]

  @spec start_link(keyword()) :: {:ok, map()} | no_return()
  def start_link(opts) do
    # validate! raises a NimbleOptions.ValidationError with a good message.
    validated = NimbleOptions.validate!(opts, @schema)
    {:ok, Map.new(validated)}
  end

  @doc "Returns the compiled schema for external introspection."
  def schema, do: @schema
end
```

### Step 3: `test/nimble_opts_demo_test.exs`

**Objective**: Write `nimble_opts_demo_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.


```elixir
defmodule NimbleOptsDemoTest do
  use ExUnit.Case, async: true

  describe "start_link/1 — happy path" do
    test "accepts a minimal valid set and applies defaults" do
      {:ok, cfg} =
        NimbleOptsDemo.start_link(name: :my_client, base_url: "https://x.io")

      assert cfg.timeout == 5_000
      assert cfg.retries == 2
      assert cfg.strategy == :exponential
      assert cfg.headers == []
      assert cfg.adapter == :finch
    end

    test "accepts the adapter as {atom, keyword}" do
      {:ok, cfg} =
        NimbleOptsDemo.start_link(
          name: :a,
          base_url: "https://x.io",
          adapter: {:httpc, [profile: :default]}
        )

      assert cfg.adapter == {:httpc, [profile: :default]}
    end

    test "accepts headers as list of {string, string} tuples" do
      {:ok, cfg} =
        NimbleOptsDemo.start_link(
          name: :a,
          base_url: "https://x.io",
          headers: [{"authorization", "Bearer x"}, {"accept", "application/json"}]
        )

      assert length(cfg.headers) == 2
    end
  end

  describe "start_link/1 — validation errors" do
    test "missing required :name" do
      assert_raise NimbleOptions.ValidationError,
                   ~r/required :name option not found/,
                   fn ->
                     NimbleOptsDemo.start_link(base_url: "https://x.io")
                   end
    end

    test "wrong type for :timeout" do
      assert_raise NimbleOptions.ValidationError,
                   ~r/expected positive integer/i,
                   fn ->
                     NimbleOptsDemo.start_link(
                       name: :a,
                       base_url: "https://x.io",
                       timeout: -1
                     )
                   end
    end

    test "value outside :in set for :strategy" do
      assert_raise NimbleOptions.ValidationError,
                   ~r/invalid value.*strategy/i,
                   fn ->
                     NimbleOptsDemo.start_link(
                       name: :a,
                       base_url: "https://x.io",
                       strategy: :aggressive
                     )
                   end
    end

    test "unknown option is rejected" do
      assert_raise NimbleOptions.ValidationError, ~r/unknown options/, fn ->
        NimbleOptsDemo.start_link(
          name: :a,
          base_url: "https://x.io",
          timout: 5_000
        )
      end
    end
  end

  describe "schema/0" do
    test "returns a keyword list usable by NimbleOptions.docs/1" do
      doc = NimbleOptsDemo.schema() |> NimbleOptions.docs()
      assert is_binary(doc)
      assert String.contains?(doc, "base_url")
    end
  end
end
```

Run:

```bash
mix deps.get
mix test
```

---


## Key Concepts

External integrations in Elixir split across multiple patterns: Ecto for relational databases with changesets and migrations; Telemetry for metrics and observability; HTTP libraries like Req or Finch for REST APIs; and specialized parsers like Jason, NimbleCSV, and NimbleParsec for data formats. Choosing the right tool avoids the trap of one library solving everything poorly.

Ecto is the de facto standard for databases because changesets encode validation before queries, migrations manage schema evolution, and the Repo pattern separates query logic from business logic. Migrations are version-controlled SQL, ensuring reproducible deployments. For integrating external services, Req is the modern HTTP client with built-in retry, redirect, and error handling policies.

Telemetry decouples metrics collection from application code: you emit events and let listeners subscribe. This separation keeps business logic clean and metrics infrastructure pluggable. Use metrics, not print statements, in production.

## Key Concepts

NimbleOptions validates option lists using a simple schema. Instead of manually checking required/optional keys, you define a schema and call `NimbleOptions.validate/2`. This centralizes validation and provides clear error messages. It's valuable for library code where options come from users; for internal functions, keyword list contracts are often clearer.

---

## Trade-offs and production gotchas

**1. Validate once, at the boundary**
Run `NimbleOptions.validate!/2` at the public API entry — `start_link/1`,
`new/1`. Internal functions should receive already-validated data. Don't
validate the same options twice on every call.

**2. Defaults are filled in — downstream code can `Keyword.fetch!/2`**
A common early-stage bug: you validate but then do `Keyword.get(opts,
:timeout, 1_000)` downstream with a *different* default. Always fetch
from the validated keyword list — the schema is the single source of truth.

**3. `{:custom, M, f, a}` for domain validation**
For rules NimbleOptions can't express (e.g., "URL must be https"), use
a custom validator that returns `{:ok, value}` or `{:error, reason}`.

**4. Schemas are not types**
NimbleOptions does not generate typespecs. For public APIs add `@spec`
manually; consider `@type opts :: keyword()` or a struct.

**5. Docs generation is not free**
`NimbleOptions.docs/2` formats Markdown at compile time if embedded in
`@moduledoc` via interpolation. Huge schemas can slow compile; for very
large configs, generate docs into a separate `.md` file.

**6. When NOT to use NimbleOptions**
- For runtime *user* input (HTTP request bodies) — use
  [Ecto.Changeset](https://hexdocs.pm/ecto/Ecto.Changeset.html) or
  [`Peri`](https://hexdocs.pm/peri/) schemas; NimbleOptions is for
  *developer* input (library config).
- For CLI parsing — use `OptionParser` (stdlib).
- For deeply polymorphic config trees — you'll end up fighting the
  schema. Split into smaller schemas per component.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- `NimbleOptions` rejects unknown keys, which protects against typos but also breaks forward compatibility: a new version that adds an option fails old callers who pass it in advance. How do Broadway and LiveView reconcile that tension, and what would it cost you to adopt the same pattern in a library you own?

## Resources

- [NimbleOptions on HexDocs](https://hexdocs.pm/nimble_options/NimbleOptions.html)
- [Broadway's options schema](https://github.com/dashbitco/broadway/blob/main/lib/broadway/options.ex) — reference for a large real-world schema
- [`Ecto.Changeset`](https://hexdocs.pm/ecto/Ecto.Changeset.html) — for validating user input vs library options
- [OptionParser (stdlib)](https://hexdocs.pm/elixir/OptionParser.html) — for CLI parsing
