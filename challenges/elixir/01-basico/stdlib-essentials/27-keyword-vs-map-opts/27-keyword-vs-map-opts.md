# Keyword vs Map for Options: Building a Typed HTTP Client

**Project**: `http_opts` — a tiny HTTP client wrapper with validated, typed options

---

## Why the options shape matters for a senior developer

Every non-trivial Elixir API takes options. The shape you pick is a
contract with your callers for years. Two idiomatic choices:

- **Keyword lists** (`[timeout: 5_000, retries: 3]`) — ordered, allow
  duplicate keys, cheap to pattern match, the dominant convention for
  public library APIs (`GenServer.start_link/3`, `Ecto.Query`, `Plug`).
- **Maps** (`%{timeout: 5_000, retries: 3}`) — unique keys, O(log n)
  lookups, ideal for internal state and large configuration blobs.

Mixing them without thinking causes subtle bugs: `Keyword.get/3` on a
map raises, `Map.get/3` on a keyword returns `nil` for valid keys.

Senior code validates options at the entry of the public function using
`Keyword.validate!/2` (stdlib, Elixir 1.13+) for small APIs, or the
`NimbleOptions` pattern for larger specs with types, defaults, and
documentation generated from a schema.

---

## Why a keyword public API and a map internal state (and not one shape end-to-end)

Picking keyword lists everywhere looks consistent but punishes hot paths: every `Keyword.get/3` walks the list, and a request handler that reads five options repeats that O(n) scan five times per call. Picking maps everywhere breaks the community convention — no one writes `HTTPoison.get("url", %{timeout: 5_000})`; the ecosystem expects keyword lists at API boundaries so `|> Keyword.merge(extra)` and compile-time typo detection via `Keyword.validate!/2` remain available. The senior pattern is to accept the idiomatic keyword list at the boundary, validate and convert to a map once, and then use the map internally — pay the conversion once so every later lookup is O(log n).

---

## The business problem

You are wrapping a shared HTTP client for your team. Every team that
copies `:httpc` code gets something slightly wrong: timeouts in ms vs
seconds, retries without backoff, missing User-Agent headers. Centralise
the rules:

1. A `request/2` function that takes a URL and a keyword list of options
2. Validate options: only allow known keys, enforce types
3. Sensible defaults (30 s timeout, 0 retries, fixed User-Agent)
4. Convert to an internal map for the actual execution so handlers can
   look up values in O(log n)
5. A documentation macro so the option schema is the single source of
   truth for both validation and docs

We are not writing a real HTTP client — we stub the transport so tests
do not touch the network.

---

## Project structure

```
http_opts/
├── lib/
│   └── http_opts/
│       ├── options.ex
│       ├── transport.ex
│       └── client.ex
├── test/
│   └── http_opts/
│       ├── options_test.exs
│       └── client_test.exs
├── .formatter.exs
└── mix.exs
```

---

## Design decisions

**Option A — accept a keyword list, use `Keyword.get/3` everywhere internally**
- Pros: one shape end-to-end; no conversion; trivial to destructure with `[{:timeout, t} | _]`.
- Cons: O(n) lookup repeated in every handler; `Keyword.get` with a default is easy to misuse (`opts[:flag] || true` coerces `false` to `true`); no compile-time type checks.

**Option B — schema-driven validator returning a map; public API is keyword, internals are map** (chosen)
- Pros: single source of truth drives both validation and doc generation; unknown keys fail at the boundary (`unknown options: [:timout]`); types are enforced (`timeout: 0` rejected as non-positive); internal code sees `opts.timeout` with a guaranteed integer.
- Cons: extra module; reimplements a subset of `NimbleOptions` (which is the production answer); adds a keyword-to-map conversion per call.

Chose **B** because options are the contract with callers for years; silently ignoring typos (`timout: 5_000`) is the class of bug that destroys trust in a shared library. The validation cost is microseconds — negligible next to any real HTTP call.

---

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"ecto", "~> 1.0"},
    {:"httpoison", "~> 1.0"},
    {:"plug", "~> 1.0"},
    {:"poison", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Keyword API boundary; map internals for O(log n) lookups; convert once, use many times prevents O(n²).

```bash
mix new http_opts
cd http_opts
```

### Step 2: `mix.exs`

**Objective**: Boilerplate; focus on schema-driven validation — Keyword.validate!/2 (1.13+) enforces types at boundary.

```elixir
defmodule HttpOpts.MixProject do
  use Mix.Project

  def project do
    [
      app: :http_opts,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  # No external deps. We implement the NimbleOptions-style validator
  # by hand so the pattern is fully visible. In production code use
  # the `:nimble_options` library — see the resources section.
  defp deps, do: []
end
```

### Step 3: `.formatter.exs`

**Objective**: Formatter is opinionated; configure inputs glob + line length once; format is hermetic (no env deps).

```elixir
[
  inputs: ["{mix,.formatter}.exs", "{config,lib,test}/**/*.{ex,exs}"],
  line_length: 98
]
```

### Step 4: `lib/http_opts/options.ex`

**Objective**: Keyword.validate!/2 fails fast on unknown keys (typo proof); returns map for internal O(1) lookups.

```elixir
defmodule HttpOpts.Options do
  @moduledoc """
  Schema-driven validator for public-API options.

  The schema is a keyword list mapping each option name to a map of
  `:type`, `:default`, and `:doc`. A single source of truth drives
  both validation and generated documentation, so they cannot drift.

  This is a minimal re-implementation of the NimbleOptions pattern,
  kept tiny so the moving parts are visible. In production use the
  `nimble_options` package.
  """

  @type type ::
          :boolean
          | :string
          | :pos_integer
          | :non_neg_integer
          | {:in, [atom()]}
          | {:list, type()}
  @type schema_entry :: [type: type(), default: term(), doc: String.t()]
  @type schema :: [{atom(), schema_entry()}]

  @doc """
  Validates `opts` against `schema`. Returns `{:ok, map}` with defaults
  filled in, or `{:error, reason}` on the first violation.

  We return a MAP (not a keyword list) because the caller uses the
  options for repeated lookups. Maps are O(log n); keyword lookup is
  O(n). Validation is the right place to pay the conversion cost once.
  """
  @spec validate(keyword(), schema()) :: {:ok, map()} | {:error, String.t()}
  def validate(opts, schema) when is_list(opts) do
    with :ok <- reject_unknown_keys(opts, schema),
         :ok <- check_types(opts, schema) do
      {:ok, with_defaults(opts, schema)}
    end
  end

  def validate(other, _schema),
    do: {:error, "options must be a keyword list, got: #{inspect(other)}"}

  @doc "Bang variant: raises ArgumentError on failure."
  @spec validate!(keyword(), schema()) :: map()
  def validate!(opts, schema) do
    case validate(opts, schema) do
      {:ok, map} -> map
      {:error, reason} -> raise ArgumentError, reason
    end
  end

  @doc """
  Builds a Markdown-ish doc block from a schema. The same schema that
  validates input also documents it; keeping them in sync is impossible
  because they are literally the same data structure.
  """
  @spec docs(schema()) :: String.t()
  def docs(schema) do
    schema
    |> Enum.map_join("\n", fn {name, spec} ->
      default = Keyword.get(spec, :default, "(required)")
      type = Keyword.fetch!(spec, :type)
      doc = Keyword.get(spec, :doc, "")
      "  * `:#{name}` (#{format_type(type)}, default #{inspect(default)}) — #{doc}"
    end)
  end

  # --- private ---

  defp reject_unknown_keys(opts, schema) do
    allowed = schema |> Keyword.keys() |> MapSet.new()
    keys = Keyword.keys(opts)

    case Enum.reject(keys, &MapSet.member?(allowed, &1)) do
      [] -> :ok
      extras -> {:error, "unknown options: #{inspect(extras)}"}
    end
  end

  defp check_types(opts, schema) do
    Enum.reduce_while(opts, :ok, fn {key, value}, :ok ->
      spec = Keyword.fetch!(schema, key)
      type = Keyword.fetch!(spec, :type)

      if valid?(value, type) do
        {:cont, :ok}
      else
        {:halt, {:error, "invalid value for :#{key}: expected #{format_type(type)}, got #{inspect(value)}"}}
      end
    end)
  end

  defp with_defaults(opts, schema) do
    # We convert to a map here — the public contract is keyword, the
    # internal representation is map.
    opts_map = Map.new(opts)

    Enum.reduce(schema, opts_map, fn {name, spec}, acc ->
      case {Map.has_key?(acc, name), Keyword.fetch(spec, :default)} do
        {true, _} -> acc
        {false, {:ok, default}} -> Map.put(acc, name, default)
        {false, :error} -> acc
      end
    end)
  end

  defp valid?(v, :boolean), do: is_boolean(v)
  defp valid?(v, :string), do: is_binary(v)
  defp valid?(v, :pos_integer), do: is_integer(v) and v > 0
  defp valid?(v, :non_neg_integer), do: is_integer(v) and v >= 0
  defp valid?(v, {:in, allowed}) when is_list(allowed), do: v in allowed
  defp valid?(v, {:list, inner}) when is_list(v), do: Enum.all?(v, &valid?(&1, inner))
  defp valid?(_, _), do: false

  defp format_type(:boolean), do: "boolean"
  defp format_type(:string), do: "string"
  defp format_type(:pos_integer), do: "positive integer"
  defp format_type(:non_neg_integer), do: "non-negative integer"
  defp format_type({:in, allowed}), do: "one of #{inspect(allowed)}"
  defp format_type({:list, inner}), do: "list of #{format_type(inner)}"
end
```

**Why this works:**

- The schema is plain data. Tests can inspect it, docs generators can
  iterate over it, and a single misconfiguration is visible in one place.
- `validate/2` returns `{:ok, map}`. The caller gets the fast shape for
  repeated lookups without doing the conversion itself.
- `reduce_while/3` short-circuits on the first bad option so large
  option lists do not waste CPU collecting every error. If you want all
  errors at once, swap for `Enum.reduce/3` and accumulate.

### Step 5: `lib/http_opts/transport.ex`

**Objective**: Stub transport (not real HTTP) — tests don't touch network; protocol contract is testable in isolation.

```elixir
defmodule HttpOpts.Transport do
  @moduledoc """
  Transport boundary. A behaviour so tests can swap in a mock without
  touching the network or pulling in a test mocking framework.
  """

  @type response :: %{status: pos_integer(), body: binary(), headers: [{String.t(), String.t()}]}

  @callback request(
              method :: atom(),
              url :: String.t(),
              headers :: [{String.t(), String.t()}],
              body :: binary() | nil,
              opts :: map()
            ) :: {:ok, response()} | {:error, term()}
end

defmodule HttpOpts.Transport.Stub do
  @moduledoc """
  Default transport used in tests. Returns a canned response so we
  can verify the option-processing pipeline end-to-end without a
  real HTTP library.
  """

  @behaviour HttpOpts.Transport

  @impl true
  def request(method, url, headers, body, opts) do
    # The stub echoes the inputs back so tests can assert on how the
    # client translated options into transport parameters.
    {:ok,
     %{
       status: 200,
       body: :erlang.term_to_binary(%{method: method, url: url, headers: headers, body: body, opts: opts}),
       headers: [{"content-type", "application/x-erlang-term"}]
     }}
  end
end
```

### Step 6: `lib/http_opts/client.ex`

**Objective**: Client uses validated map opts; schema is single source of truth for validation + docs generation.

```elixir
defmodule HttpOpts.Client do
  @moduledoc """
  Thin HTTP client wrapper with validated options.

  ## Options

  #{HttpOpts.Options.docs([
    method: [type: {:in, [:get, :post, :put, :delete, :patch]}, default: :get,
             doc: "HTTP verb"],
    timeout: [type: :pos_integer, default: 30_000,
              doc: "request timeout in milliseconds"],
    retries: [type: :non_neg_integer, default: 0,
              doc: "number of retry attempts on transport error"],
    backoff_ms: [type: :pos_integer, default: 250,
                 doc: "initial backoff delay between retries; doubles per attempt"],
    headers: [type: {:list, :string}, default: [],
              doc: "extra headers as a list of \\"Name: Value\\" strings"],
    user_agent: [type: :string, default: "http_opts/0.1",
                 doc: "User-Agent header value"],
    follow_redirects: [type: :boolean, default: true,
                       doc: "whether to follow 3xx redirects"],
    transport: [type: :atom, default: HttpOpts.Transport.Stub,
                doc: "module implementing HttpOpts.Transport"]
  ])}
  """

  alias HttpOpts.Options

  # The schema lives in the module attribute so both @moduledoc (via
  # the docs/1 helper above, at compile time) and the validator use
  # the same data. Update once, propagates everywhere.
  @schema [
    method: [type: {:in, [:get, :post, :put, :delete, :patch]}, default: :get, doc: "HTTP verb"],
    timeout: [type: :pos_integer, default: 30_000, doc: "request timeout in ms"],
    retries: [type: :non_neg_integer, default: 0, doc: "retries on transport error"],
    backoff_ms: [type: :pos_integer, default: 250, doc: "initial backoff delay"],
    headers: [type: {:list, :string}, default: [], doc: "extra headers"],
    user_agent: [type: :string, default: "http_opts/0.1", doc: "User-Agent header value"],
    follow_redirects: [type: :boolean, default: true, doc: "follow 3xx redirects"],
    body: [type: :string, default: nil, doc: "request body (for POST/PUT/PATCH)"],
    transport: [
      type: {:in, [HttpOpts.Transport.Stub]},
      default: HttpOpts.Transport.Stub,
      doc: "transport module"
    ]
  ]

  @doc """
  Perform an HTTP request against `url` using `opts`.

  `opts` is a keyword list — the public conventional shape — validated
  against the schema. On success returns `{:ok, response}` where
  response is a map with `:status`, `:body`, `:headers`.
  """
  @spec request(String.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def request(url, opts \\ []) when is_binary(url) do
    with {:ok, validated} <- Options.validate(opts, @schema) do
      do_request(url, validated)
    end
  end

  @doc """
  Alternative entry point using `Keyword.validate!/2` for very small
  use cases where you only need key-name validation (no types, no
  docs generation). Shown here for comparison.
  """
  @spec quick_get(String.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def quick_get(url, opts \\ []) do
    # Keyword.validate!/2 (Elixir 1.13+) checks that only listed keys
    # are present and fills defaults. It does NOT check types.
    opts = Keyword.validate!(opts, timeout: 30_000, user_agent: "http_opts/0.1")
    request(url, [{:method, :get} | opts])
  end

  # --- private ---

  defp do_request(url, %{} = opts) do
    headers = build_headers(opts)
    transport = Map.fetch!(opts, :transport)

    attempt(transport, opts.method, url, headers, opts.body, opts, opts.retries, opts.backoff_ms)
  end

  defp attempt(transport, method, url, headers, body, opts, retries_left, backoff) do
    case transport.request(method, url, headers, body, opts) do
      {:ok, _resp} = ok ->
        ok

      {:error, _reason} when retries_left > 0 ->
        Process.sleep(backoff)
        attempt(transport, method, url, headers, body, opts, retries_left - 1, backoff * 2)

      {:error, _reason} = err ->
        err
    end
  end

  defp build_headers(opts) do
    base = [{"user-agent", opts.user_agent}]

    extra =
      Enum.map(opts.headers, fn raw ->
        [name, value] = String.split(raw, ":", parts: 2)
        {String.downcase(String.trim(name)), String.trim(value)}
      end)

    base ++ extra
  end
end
```

**Why this works:**

- Public API accepts a keyword list (`request(url, opts)`). That matches
  every other Elixir library and composes naturally with
  `opts |> Keyword.merge(extra)`.
- Internal functions (`do_request`, `attempt`, `build_headers`) take a
  map. No pattern will ever have to handle `nil` for a key with a
  default because the validator filled it in.
- Exponential backoff (`backoff * 2`) is trivial once the options are a
  map — `opts.backoff_ms` is always a number, never `nil`.

### Step 7: Tests

**Objective**: Test option validation (unknown keys fail, types enforced); defaults apply; client receives valid map.

```elixir
# test/http_opts/options_test.exs
defmodule HttpOpts.OptionsTest do
  use ExUnit.Case, async: true
  alias HttpOpts.Options

  @schema [
    mode: [type: {:in, [:fast, :slow]}, default: :fast, doc: "speed mode"],
    retries: [type: :non_neg_integer, default: 0, doc: "retries"],
    debug: [type: :boolean, default: false, doc: "verbose log"]
  ]

  describe "validate/2" do
    test "fills in defaults and returns a map" do
      assert {:ok, %{mode: :fast, retries: 0, debug: false}} = Options.validate([], @schema)
    end

    test "accepts valid values and overrides defaults" do
      assert {:ok, %{mode: :slow, retries: 3, debug: true}} =
               Options.validate([mode: :slow, retries: 3, debug: true], @schema)
    end

    test "rejects unknown keys" do
      assert {:error, msg} = Options.validate([rubbish: true], @schema)
      assert msg =~ "unknown options"
      assert msg =~ "rubbish"
    end

    test "rejects wrong type for pos/non-neg integer" do
      assert {:error, msg} = Options.validate([retries: -1], @schema)
      assert msg =~ ":retries"
    end

    test "rejects wrong type for boolean" do
      assert {:error, msg} = Options.validate([debug: "yes"], @schema)
      assert msg =~ ":debug"
    end

    test "rejects value outside an :in enum" do
      assert {:error, msg} = Options.validate([mode: :medium], @schema)
      assert msg =~ ":mode"
    end

    test "rejects non-keyword input" do
      assert {:error, msg} = Options.validate(%{mode: :fast}, @schema)
      assert msg =~ "must be a keyword list"
    end
  end

  describe "validate!/2" do
    test "raises on invalid options" do
      assert_raise ArgumentError, fn -> Options.validate!([retries: -1], @schema) end
    end
  end

  describe "docs/1" do
    test "mentions every option and its default" do
      output = Options.docs(@schema)
      assert output =~ ":mode"
      assert output =~ ":retries"
      assert output =~ ":debug"
      assert output =~ "default :fast"
    end
  end
end
```

```elixir
# test/http_opts/client_test.exs
defmodule HttpOpts.ClientTest do
  use ExUnit.Case, async: true
  alias HttpOpts.Client

  # Decode the stub's echo body so we can assert on what the client
  # passed downstream.
  defp decode(body), do: :erlang.binary_to_term(body)

  describe "request/2" do
    test "applies defaults for omitted options" do
      {:ok, resp} = Client.request("https://example.test")
      echo = decode(resp.body)

      assert echo.method == :get
      assert echo.opts.timeout == 30_000
      assert echo.opts.retries == 0
      assert echo.opts.follow_redirects == true
    end

    test "overrides defaults with caller values" do
      {:ok, resp} = Client.request("https://example.test", method: :post, timeout: 5_000)
      echo = decode(resp.body)

      assert echo.method == :post
      assert echo.opts.timeout == 5_000
    end

    test "adds User-Agent header from options" do
      {:ok, resp} = Client.request("https://example.test", user_agent: "my-agent/2")
      echo = decode(resp.body)

      assert {"user-agent", "my-agent/2"} in echo.headers
    end

    test "parses extra headers strings" do
      {:ok, resp} = Client.request("https://example.test", headers: ["X-Request-Id: abc"])
      echo = decode(resp.body)

      assert {"x-request-id", "abc"} in echo.headers
    end

    test "rejects unknown options" do
      assert {:error, msg} = Client.request("https://example.test", foo: 1)
      assert msg =~ "unknown options"
    end

    test "rejects invalid method" do
      assert {:error, msg} = Client.request("https://example.test", method: :teapot)
      assert msg =~ ":method"
    end

    test "rejects non-positive timeout" do
      assert {:error, msg} = Client.request("https://example.test", timeout: 0)
      assert msg =~ ":timeout"
    end
  end

  describe "quick_get/2 with Keyword.validate!/2" do
    test "accepts known keys and defaults the rest" do
      {:ok, resp} = Client.quick_get("https://example.test", timeout: 1_000)
      echo = decode(resp.body)
      assert echo.method == :get
      assert echo.opts.timeout == 1_000
    end

    test "raises on unknown keys" do
      assert_raise ArgumentError, fn ->
        Client.quick_get("https://example.test", typo: true)
      end
    end
  end
end
```

### Step 8: Run and verify

**Objective**: --warnings-as-errors catches unused option fields; test coverage validates validation rejects bad input.

```bash
mix deps.get
mix compile --warnings-as-errors
mix test --trace
mix format
```

### Why this works

The schema is plain data (a keyword list of `{name, [type: ..., default: ...]}`). `validate/2` walks it once to reject unknown keys, once to check types, and once to apply defaults — every call produces a fully-populated map so internal handlers never branch on "is this option set?". Because the schema is a module attribute, the same data drives the `@moduledoc` via `Options.docs/1`, so documentation cannot drift from validation. The internal transport is a behaviour, so tests swap the stub in without mocking libraries.

---


## Key Concepts

### 1. Keyword Lists for Function Options
Keyword lists are idiomatic for function options. They preserve order and allow duplicate keys.

### 2. Maps for Nested Data
For API responses, database records, and unstructured data, use maps.

### 3. Modern Elixir Prefers Maps for Options
Newer libraries use maps. But keyword lists remain for backward compatibility and tradition. Both are acceptable.

---
## Benchmark

```elixir
# bench.exs
defmodule Bench do
  def run do
    opts = [method: :post, timeout: 5_000, retries: 2, user_agent: "bench/1"]

    {us, _} =
      :timer.tc(fn ->
        Enum.each(1..100_000, fn _ -> HttpOpts.Client.request("https://example.test", opts) end)
      end)

    IO.puts("validated request x100k: #{us} µs (#{us / 100_000} µs/call)")
  end
end

Bench.run()
```

Target: under 10 µs per call (validation + stub transport). Validation alone is under 2 µs; anything beyond that is the transport echoing the payload.

---

## Trade-off analysis

| Aspect | Keyword list | Map |
|--------|-------------|-----|
| Public API convention | Yes (idiomatic) | Rare |
| Duplicate keys allowed | Yes | No |
| Order preserved | Yes | No |
| Lookup cost | O(n) | O(log n) |
| Pattern match on known shape | Awkward | Clean |
| Best role | Input boundary | Internal state |

| Validation approach | When |
|--------------------|------|
| Nothing (raw `Keyword.get/3`) | Prototypes, internal one-off functions |
| `Keyword.validate!/2` (stdlib) | Small, key-only validation, no types |
| Hand-rolled schema (this exercise) | Teaching / minimal dependency envs |
| `NimbleOptions` library | Production code. Types, defaults, docs, subsections |

---

## Common production mistakes

**1. `Keyword.get(opts, :timeout)` without a default**
If the caller omits `:timeout`, you get `nil`, which propagates to
`Process.sleep(nil)` and crashes far from the origin. Always specify a
default or validate up front.

**2. Mixing keyword and map in the same function**
```
def request(url, opts) do
  timeout = opts.timeout # breaks when opts is a keyword list
end
```
Pick one internally. Convert at the boundary.

**3. Using `opts[:retries] || 0` to default**
`Keyword.get(opts, :retries, 0)` is the idiomatic form and is not
tripped up by explicit `false` values (`opts[:flag] || true` defaults
`false` to `true` — a nasty bug).

**4. Silently accepting unknown keys**
Without validation, `timout: 5_000` (typo) is accepted and the real
`:timeout` default is used. `Keyword.validate!/2` catches this at the
call site.

**5. Baking a dozen options into the function signature**
```
def request(url, method, timeout, retries, backoff, headers, ...)
```
Adding a new option is a breaking change. A keyword list keeps the
arity stable.

**6. Schema and docs drifting apart**
Hand-written `@moduledoc` listing options rots the moment someone adds
a new option. Generate the docs from the schema as shown in `docs/1`.

---

## When NOT to use keyword lists

- **Large, deeply-nested configuration** — use maps or structs.
  Keyword lists with 50 entries and nested lists become unreadable.
- **Frequent lookups inside hot loops** — the O(n) cost adds up.
  Convert to a map once at the boundary.
- **Runtime-discovered keys** — keyword keys must be atoms known at
  compile time (or you risk atom-table exhaustion). Use string-keyed
  maps for dynamic config.
- **Serialisation to JSON** — keyword lists serialise as lists of
  two-element lists, not objects. Use maps.

---

## Reflection

1. `Options.validate/2` short-circuits on the first bad option. For a CLI that wants to report every error at once, would you switch to `Enum.reduce/3` and accumulate errors, keep short-circuit and document it, or expose a `validate_all/2` variant? What does each cost in implementation and user experience?
2. The schema lives in a module attribute. If a new feature needs options that depend on the `:method` (e.g. `:body` only valid for POST/PUT/PATCH), does the current shape still fit? Would you nest schemas per method, add a cross-field validator hook, or bite the bullet and migrate to `NimbleOptions` with its `:subsection` support?

---

## Resources

- [Keyword module — HexDocs](https://hexdocs.pm/elixir/Keyword.html)
- [Keyword.validate!/2 — HexDocs](https://hexdocs.pm/elixir/Keyword.html#validate!/2)
- [Map module — HexDocs](https://hexdocs.pm/elixir/Map.html)
- [NimbleOptions — schema-driven option validation](https://hexdocs.pm/nimble_options/NimbleOptions.html)
- [Elixir style guide — options convention](https://hexdocs.pm/elixir/naming-conventions.html)
