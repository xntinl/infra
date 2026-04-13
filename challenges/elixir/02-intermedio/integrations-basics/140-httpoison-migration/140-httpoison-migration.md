# Migrating from HTTPoison to Req: a pragmatic side-by-side

**Project**: `http_migration` — a legacy client module written with
[`HTTPoison`](https://hexdocs.pm/httpoison/) and its modernised twin
written with [`Req`](https://hexdocs.pm/req/), both covered by the same
test suite. Shows the concrete translation table and why new code should
prefer Req.

---

## Project context

For the better part of a decade, `HTTPoison` was *the* HTTP client in
Elixir. It wraps `:hackney`, it's stable, it's ubiquitous — you'll find
it in most code written between 2015 and 2022. It still works and is
still maintained at v2.x.

But since 2022, the recommendation for **new** code is
[Req](https://hexdocs.pm/req/), for a few concrete reasons:

1. Built on `Finch`/`Mint` (pure Erlang/Elixir — no C NIF like hackney).
2. Automatic JSON encode/decode, compression, redirects, retries —
   HTTPoison leaves all of that to you.
3. First-class testability via `Req.Test` — no `Bypass`, no manual mocks.
4. Composable via *steps* — you can plug in auth, logging, retry policies
   without wrapping every call.
5. Maintained by José Valim and Dashbit; it's where the ecosystem is
   heading.

This exercise keeps both clients around so you can see exactly what
changes, and so a migration in your real codebase is a search-and-replace
guided by test parity.

Project structure:

```
http_migration/
├── lib/
│   ├── http_migration/
│   │   ├── legacy.ex      # HTTPoison version
│   │   └── modern.ex      # Req version
│   └── http_migration.ex
├── test/
│   ├── legacy_test.exs
│   └── modern_test.exs
└── mix.exs
```

---

## Core concepts

### 1. The translation table

| HTTPoison | Req | Notes |
|-----------|-----|-------|
| `HTTPoison.get(url, headers, opts)` | `Req.get(url: url, headers: headers)` | Req takes kw opts |
| `HTTPoison.post(url, body, headers)` | `Req.post(url, body: body)` or `json: map` | JSON automatic |
| `{:ok, %HTTPoison.Response{status_code: 200, body: b}}` | `{:ok, %Req.Response{status: 200, body: b}}` | field rename |
| `{:error, %HTTPoison.Error{reason: r}}` | `{:error, exception}` | exception struct, not custom |
| manual `Jason.decode!` on body | `resp.body` already decoded | |
| manual retry loop | `retry: :safe_transient` option | |
| `Bypass.open() + Bypass.expect_once` | `Req.Test.stub(Name, fn conn -> ... end)` | |

### 2. Error shapes

HTTPoison errors have `%HTTPoison.Error{reason: atom()}`; Req errors
usually carry a `Mint.TransportError` or similar exception. If your code
pattern-matches on `%HTTPoison.Error{reason: :timeout}`, the Req
equivalent is matching on `%Mint.TransportError{reason: :timeout}` or
simply `{:error, %{__exception__: true}}`.

### 3. Status field rename

The single most common friction point: `status_code` → `status`. Both are
integers; the name changed.

### 4. JSON is no longer manual

```elixir
# HTTPoison
{:ok, %HTTPoison.Response{body: raw}} = HTTPoison.post(url, Jason.encode!(payload),
                                                      [{"content-type", "application/json"}])
{:ok, data} = Jason.decode(raw)

# Req
{:ok, %Req.Response{body: data}} = Req.post(url, json: payload)
```

Two lines become one; JSON headers are set automatically.

---

## Design decisions

**Option A — "big bang" replace every `HTTPoison.*` call with `Req.*` across the codebase**
- Pros: one PR, done; no dual code paths to maintain; fewer deps after cutover.
- Cons: huge blast radius; error-shape mismatches (`%HTTPoison.Error{}` vs `Mint.TransportError`) surface everywhere at once; rollback is painful; mixed third-party deps still pull HTTPoison in anyway.

**Option B — introduce a domain adapter boundary, then swap implementations behind it (chosen)**
- Pros: callers see only domain atoms (`:timeout`, `:not_found`); legacy + modern implementations coexist while you migrate; each module's test suite unchanged; rollback is a one-line config flip.
- Cons: one extra layer of indirection; you have to maintain both implementations until cutover is complete.

→ Chose **B** because migrations that touch every file in a repo at once are how regressions escape review; the adapter boundary makes the change reversible and testable.

## Implementation

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Standard library: no external dependencies required
    {:"httpoison", "~> 1.0"},
    {:"jason", "~> 1.0"},
    {:"plug", "~> 1.0"},
    {:"poison", "~> 1.0"},
  ]
end
```


### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.


```bash
mix new http_migration
cd http_migration
```

Deps in `mix.exs`:

```elixir
defp deps do
  [
    {:httpoison, "~> 2.2"},
    {:req, "~> 0.5"},
    {:jason, "~> 1.4"},
    {:bypass, "~> 2.1", only: :test},
    {:plug, "~> 1.15", only: :test}
  ]
end
```

### Step 2: `lib/http_migration/legacy.ex` — HTTPoison

**Objective**: Edit `legacy.ex` — HTTPoison, exposing the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule HttpMigration.Legacy do
  @moduledoc """
  The original client. Manual JSON, manual retries, error shape tied to
  HTTPoison's structs. Representative of code written pre-2023.
  """

  @max_retries 2

  @spec get_user(String.t(), integer()) :: {:ok, map()} | {:error, term()}
  def get_user(base_url, id) do
    url = "#{base_url}/users/#{id}"
    do_get_with_retry(url, @max_retries)
  end

  @spec create_user(String.t(), map()) :: {:ok, map()} | {:error, term()}
  def create_user(base_url, attrs) do
    url = "#{base_url}/users"
    headers = [{"content-type", "application/json"}, {"accept", "application/json"}]

    case HTTPoison.post(url, Jason.encode!(attrs), headers) do
      {:ok, %HTTPoison.Response{status_code: s, body: body}} when s in 200..299 ->
        Jason.decode(body)

      {:ok, %HTTPoison.Response{status_code: s}} ->
        {:error, {:http, s}}

      {:error, %HTTPoison.Error{reason: reason}} ->
        {:error, reason}
    end
  end

  # ── Private ─────────────────────────────────────────────────────────────

  defp do_get_with_retry(_url, attempts) when attempts < 0,
    do: {:error, :retries_exhausted}

  defp do_get_with_retry(url, attempts) do
    case HTTPoison.get(url, [{"accept", "application/json"}]) do
      {:ok, %HTTPoison.Response{status_code: 200, body: body}} ->
        Jason.decode(body)

      {:ok, %HTTPoison.Response{status_code: 404}} ->
        {:error, :not_found}

      {:ok, %HTTPoison.Response{status_code: s}} when s >= 500 ->
        Process.sleep(backoff(@max_retries - attempts))
        do_get_with_retry(url, attempts - 1)

      {:ok, %HTTPoison.Response{status_code: s}} ->
        {:error, {:http, s}}

      {:error, %HTTPoison.Error{reason: :timeout}} ->
        Process.sleep(backoff(@max_retries - attempts))
        do_get_with_retry(url, attempts - 1)

      {:error, %HTTPoison.Error{reason: r}} ->
        {:error, r}
    end
  end

  defp backoff(attempt), do: trunc(:math.pow(2, attempt) * 100)
end
```

### Step 3: `lib/http_migration/modern.ex` — Req

**Objective**: Edit `modern.ex` — Req, exposing the integration seam where external protocol semantics meet Elixir domain code.


```elixir
defmodule HttpMigration.Modern do
  @moduledoc """
  Equivalent client written with `Req`. JSON, retries, and errors are
  all handled by the library. The code is roughly half the size and
  far more testable.
  """

  @spec get_user(String.t(), integer(), keyword()) :: {:ok, map()} | {:error, term()}
  def get_user(base_url, id, req_opts \\ []) do
    case Req.get(build(base_url, req_opts), url: "/users/#{id}") do
      {:ok, %Req.Response{status: 200, body: body}} -> {:ok, body}
      {:ok, %Req.Response{status: 404}} -> {:error, :not_found}
      {:ok, %Req.Response{status: status}} -> {:error, {:http, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  @spec create_user(String.t(), map(), keyword()) :: {:ok, map()} | {:error, term()}
  def create_user(base_url, attrs, req_opts \\ []) do
    case Req.post(build(base_url, req_opts), url: "/users", json: attrs) do
      {:ok, %Req.Response{status: s, body: body}} when s in 200..299 -> {:ok, body}
      {:ok, %Req.Response{status: status}} -> {:error, {:http, status}}
      {:error, reason} -> {:error, reason}
    end
  end

  defp build(base_url, extra_opts) do
    Req.new(
      [base_url: base_url, retry: :safe_transient, max_retries: 2] ++ extra_opts
    )
  end
end
```

### Step 4: Tests exercise identical behaviour

**Objective**: Tests exercise identical behaviour.


`test/legacy_test.exs` (HTTPoison + Bypass):

```elixir
defmodule HttpMigration.LegacyTest do
  use ExUnit.Case, async: true

  setup do
    bypass = Bypass.open()
    {:ok, bypass: bypass, base: "http://localhost:#{bypass.port}"}
  end

  test "get_user/2 returns the parsed user on 200", %{bypass: b, base: base} do
    Bypass.expect_once(b, "GET", "/users/1", fn conn ->
      Plug.Conn.resp(conn, 200, ~s({"id":1,"name":"Ada"}))
    end)

    assert {:ok, %{"id" => 1, "name" => "Ada"}} =
             HttpMigration.Legacy.get_user(base, 1)
  end

  test "get_user/2 maps 404 to :not_found", %{bypass: b, base: base} do
    Bypass.expect_once(b, "GET", "/users/999", fn conn ->
      Plug.Conn.resp(conn, 404, "")
    end)

    assert {:error, :not_found} = HttpMigration.Legacy.get_user(base, 999)
  end

  test "create_user/2 sends JSON and returns map", %{bypass: b, base: base} do
    Bypass.expect_once(b, "POST", "/users", fn conn ->
      {:ok, body, conn} = Plug.Conn.read_body(conn)
      assert {:ok, %{"name" => "Grace"}} = Jason.decode(body)
      Plug.Conn.resp(conn, 201, ~s({"id":42,"name":"Grace"}))
    end)

    assert {:ok, %{"id" => 42}} =
             HttpMigration.Legacy.create_user(base, %{name: "Grace"})
  end
end
```

`test/modern_test.exs` (Req + Req.Test):

```elixir
defmodule HttpMigration.ModernTest do
  use ExUnit.Case, async: true

  # Note: no Bypass required. Req.Test intercepts before the network.
  defp req_opts, do: [plug: {Req.Test, HttpMigration.ModernStub}]

  test "get_user/3 returns the parsed user on 200" do
    Req.Test.stub(HttpMigration.ModernStub, fn conn ->
      Req.Test.json(conn, %{"id" => 1, "name" => "Ada"})
    end)

    assert {:ok, %{"id" => 1, "name" => "Ada"}} =
             HttpMigration.Modern.get_user("http://x.io", 1, req_opts())
  end

  test "get_user/3 maps 404 to :not_found" do
    Req.Test.stub(HttpMigration.ModernStub, fn conn ->
      Plug.Conn.send_resp(conn, 404, "")
    end)

    assert {:error, :not_found} =
             HttpMigration.Modern.get_user("http://x.io", 999, req_opts())
  end

  test "create_user/3 sends JSON and returns the decoded body" do
    Req.Test.stub(HttpMigration.ModernStub, fn conn ->
      {:ok, body, conn} = Plug.Conn.read_body(conn)
      assert {:ok, %{"name" => "Grace"}} = Jason.decode(body)
      Req.Test.json(conn, %{"id" => 42, "name" => "Grace"})
    end)

    assert {:ok, %{"id" => 42, "name" => "Grace"}} =
             HttpMigration.Modern.create_user("http://x.io", %{name: "Grace"},
               req_opts())
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

HTTPoison is an older HTTP client; modern code should use Req instead. HTTPoison's API is simpler than Finch but less composable than Req. If you're maintaining HTTPoison code, understanding the migration path (to Req) is valuable. For legacy systems, maintain HTTPoison; for new code, use Req.

---

## Trade-offs and production gotchas

**1. Migration strategy: adapter boundary first**
Don't do a global search-and-replace. Introduce an HTTP-client module
inside your domain (`MyApp.HttpClient`) and have *every* call go through
it. Then swap the implementation from HTTPoison to Req behind that
boundary. Tests of domain modules don't change.

**2. Error shapes will break pattern matches**
Grep for `%HTTPoison.Error{` in your codebase. Each occurrence needs
updating. The safer migration is: translate errors at the adapter
boundary into domain atoms (`:timeout`, `:connection_refused`) so upstream
callers are library-agnostic.

**3. SSL config differs**
HTTPoison (via hackney) reads trust store from `:certifi` by default;
Req (via Finch/Mint) uses `:public_key.cacerts_get/0` on OTP 25+. If you
have custom CA bundles, test TLS explicitly before cutover.

**4. Connection pools don't translate 1:1**
HTTPoison pools via `:hackney_pool`; Req uses Finch pools. When you
switch, revisit pool sizes. The hackney defaults are
rarely the right Finch defaults.

**5. Async/stream APIs differ significantly**
`HTTPoison.AsyncResponse` and stream callbacks don't have a direct Req
equivalent. For streaming downloads, use `Req.get!` with `:into` (a
function or file) or drop to `Finch.stream/5`. Plan this migration
separately.

**6. When NOT to migrate**
- A mature codebase where HTTPoison works and no other change is
  pending — don't migrate for its own sake.
- Code targeting a platform where `Mint`'s TLS defaults don't work
  (very old OTP, constrained environments). Audit first.
- Dependencies that *themselves* pull in HTTPoison: migrating your code
  doesn't remove the dep, and having both is fine for a transition period.

Migrate when you're touching the client anyway, when you're adding
features that would be painful in HTTPoison (retries, telemetry, tests),
or when a fresh service can start on Req from day one.

---

## Benchmark

<!-- benchmark N/A: integration/configuration exercise -->

## Reflection

- After you migrate, HTTPoison may still live in your deps tree because a third-party library pins it. Does that undermine the reason for migrating (fewer deps, no C NIF), or is the real win elsewhere — and if elsewhere, what does that say about how you should justify the migration to a skeptical reviewer?

## Resources

- [Req on HexDocs](https://hexdocs.pm/req/Req.html)
- [HTTPoison on HexDocs](https://hexdocs.pm/httpoison/) — legacy reference
- [José Valim: Req — A batteries-included HTTP client](https://dashbit.co/blog/req-a-batteries-included-http-client-for-elixir)
- [Req.Test](https://hexdocs.pm/req/Req.Test.html) — the testing story you don't have in HTTPoison
- [Finch](https://hexdocs.pm/finch/) — the adapter Req uses by default
- [Bypass](https://hexdocs.pm/bypass/) — still useful for integration tests of code you haven't migrated
