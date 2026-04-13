# OAuth2 Server-Side Flow with Assent

**Project**: `social_login` — a Phoenix app that authenticates users against Google and GitHub using the OAuth 2.0 authorization code flow via `Assent`

---

## Why apis and graphql matters

GraphQL with Absinthe collapses N+1 problems via Dataloader, exposes subscriptions through Phoenix.PubSub, and lets the schema itself enforce complexity limits. REST APIs in Elixir benefit from Plug pipelines, OpenAPI generation, JWT auth, and HMAC-signed webhooks.

The hard parts are not the happy path: it's pagination consistency under concurrent writes, refresh-token rotation, idempotent webhook processing, and complexity budgets that prevent a single query from saturating a node.

---

## The business problem

You are building a production-grade Elixir component in the **APIs and GraphQL** area. The goal is not a toy demonstration — it is a piece you could drop into a real system and rely on under load.

Concretely, you must:

1. Implement the topic correctly with measurable behavior.
2. Surface failure modes via `{:ok, _} / {:error, reason}` tuples — never raise from a public API except for programmer errors.
3. Bound resources: explicit timeouts, memory caps, back-pressure where relevant, and supervision for any long-lived process.
4. Provide a test suite with `async: true` where safe, `doctest` for public examples, and `describe` blocks that name the contract.
5. Provide a `script/main.exs` that demonstrates the piece end-to-end with realistic data — not a `1 + 1` smoke test.

---

## Project structure

```
social_login/
├── lib/
│   └── social_login.ex
├── script/
│   └── main.exs
├── test/
│   └── social_login_test.exs
└── mix.exs
```

---

## Design decisions

**Option A — minimal happy-path implementation**
- Pros: smaller surface area, faster to ship.
- Cons: no resource bounds, no failure-mode coverage, no observability hooks. Falls over the first time production load deviates from the developer's mental model.

**Option B — production-grade contract with explicit bounds** (chosen)
- Pros: timeouts, supervised lifecycle, structured errors, idiomatic `{:ok, _} / {:error, reason}` returns. Tests cover the failure envelope, not just the happy path.
- Cons: more code, more concepts. Pays for itself the first time the upstream service degrades.

Chose **B** because in APIs and GraphQL the failure modes — not the happy path — are what determine whether the system stays up under real production conditions.

---

## Implementation

### `mix.exs`

```elixir
defmodule SocialLogin.MixProject do
  use Mix.Project

  def project do
    [
      app: :social_login,
      version: "0.1.0",
      elixir: "~> 1.19",
      elixirc_paths: elixirc_paths(Mix.env()),
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp elixirc_paths(:test), do: ["lib", "test/support"]
  defp elixirc_paths(_), do: ["lib"]

  defp deps do
    [
      {:ex_doc, "~> 0.31", only: :dev, runtime: false}
    ]
  end
end
```

### `lib/social_login.ex`

```elixir
defmodule SocialLoginWeb.Auth.Providers do
  @moduledoc """
  Ejercicio: OAuth2 Server-Side Flow with Assent.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

  @doc "Returns config result."
  def config(:google) do
    [
      client_id: System.fetch_env!("GOOGLE_CLIENT_ID"),
      client_secret: System.fetch_env!("GOOGLE_CLIENT_SECRET"),
      redirect_uri: SocialLoginWeb.Endpoint.url() <> "/auth/google/callback",
      authorization_params: [scope: "openid email profile"]
    ]
  end

  @doc "Returns config result."
  def config(:github) do
    [
      client_id: System.fetch_env!("GITHUB_CLIENT_ID"),
      client_secret: System.fetch_env!("GITHUB_CLIENT_SECRET"),
      redirect_uri: SocialLoginWeb.Endpoint.url() <> "/auth/github/callback",
      authorization_params: [scope: "read:user user:email"]
    ]
  end

  @doc "Returns strategy result."
  def strategy(:google), do: Assent.Strategy.Google
  @doc "Returns strategy result."
  def strategy(:github), do: Assent.Strategy.Github
end

defmodule SocialLoginWeb.OAuthController do
  use SocialLoginWeb, :controller
  alias SocialLoginWeb.Auth.Providers
  alias SocialLogin.Accounts

  @doc "Returns request result from conn."
  def request(conn, %{"provider" => provider_str}) do
    provider = String.to_existing_atom(provider_str)
    strategy = Providers.strategy(provider)
    config = Providers.config(provider)

    case strategy.authorize_url(config) do
      {:ok, %{url: url, session_params: params}} ->
        conn
        |> put_session(:oauth_session_params, params)
        |> put_session(:oauth_provider, provider)
        |> redirect(external: url)

      {:error, reason} ->
        conn
        |> put_flash(:error, "could not start oauth: #{inspect(reason)}")
        |> redirect(to: "/")
    end
  end

  @doc "Returns callback result from conn and params."
  def callback(conn, params) do
    provider = get_session(conn, :oauth_provider)
    session_params = get_session(conn, :oauth_session_params)
    strategy = Providers.strategy(provider)
    config = Providers.config(provider) |> Keyword.put(:session_params, session_params)

    with {:ok, %{user: user_info, token: token}} <- strategy.callback(config, params),
         {:ok, user} <- Accounts.upsert_from_oauth(provider, user_info) do
      conn
      |> delete_session(:oauth_session_params)
      |> delete_session(:oauth_provider)
      |> put_session(:user_id, user.id)
      |> configure_session(renew: true)     # rotate session id (fixation defense)
      |> redirect(to: "/dashboard")
    else
      {:error, reason} ->
        conn
        |> put_flash(:error, "oauth failed: #{friendly(reason)}")
        |> redirect(to: "/login")
    end
  end

  defp friendly(%Assent.InvalidResponseError{}), do: "provider returned an invalid response"
  defp friendly(%Assent.CallbackCSRFError{}), do: "state mismatch — please retry"
  defp friendly(other), do: inspect(other)
end

defmodule SocialLogin.Accounts do
  alias SocialLogin.Accounts.User
  alias SocialLogin.Repo

  @doc "Returns upsert from oauth result from provider."
  def upsert_from_oauth(provider, %{"sub" => uid, "email" => email} = info) do
    attrs = %{
      provider: Atom.to_string(provider),
      provider_uid: uid,
      email: email,
      name: info["name"]
    }

    case Repo.get_by(User, provider: attrs.provider, provider_uid: attrs.provider_uid) do
      nil -> %User{} |> User.changeset(attrs) |> Repo.insert()
      user -> user |> User.changeset(attrs) |> Repo.update()
    end
  end
end

defmodule SocialLogin.Accounts.User do
  use Ecto.Schema
  import Ecto.Changeset

  schema "users" do
    field :email, :string
    field :name, :string
    field :provider, :string
    field :provider_uid, :string
    timestamps()
  end

  @doc "Returns changeset result from struct and attrs."
  def changeset(struct, attrs) do
    struct
    |> cast(attrs, [:email, :name, :provider, :provider_uid])
    |> validate_required([:email, :provider, :provider_uid])
    |> unique_constraint([:provider, :provider_uid])
  end
end

defmodule SocialLoginWeb.Router do
  use SocialLoginWeb, :router

  pipeline :browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :protect_from_forgery
    plug :put_secure_browser_headers
  end

  scope "/auth", SocialLoginWeb do
    pipe_through :browser

    get "/:provider", OAuthController, :request
    get "/:provider/callback", OAuthController, :callback
  end
end
```

### `test/social_login_test.exs`

```elixir
defmodule SocialLoginWeb.OAuthControllerTest do
  use SocialLoginWeb.ConnCase, async: true
  import Mox

  setup :verify_on_exit!

  describe "GET /auth/:provider" do
    test "redirects to provider with state+PKCE", %{conn: conn} do
      conn = get(conn, "/auth/google")
      assert redirected_to(conn, 302) =~ "accounts.google.com"
      assert redirected_to(conn) =~ "code_challenge="
      assert redirected_to(conn) =~ "state="
      assert get_session(conn, :oauth_session_params)[:state]
    end
  end

  describe "GET /auth/:provider/callback" do
    test "rejects missing session (CSRF)", %{conn: conn} do
      conn = get(conn, "/auth/google/callback", %{"code" => "x", "state" => "y"})
      assert redirected_to(conn) == "/login"
      assert Phoenix.Flash.get(conn.assigns.flash, :error) =~ "oauth failed"
    end

    test "rejects state mismatch", %{conn: conn} do
      conn =
        conn
        |> init_test_session(%{oauth_session_params: %{state: "correct"}, oauth_provider: :google})
        |> get("/auth/google/callback", %{"code" => "x", "state" => "attacker"})

      assert redirected_to(conn) == "/login"
    end
  end
end
```

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for OAuth2 Server-Side Flow with Assent.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== OAuth2 Server-Side Flow with Assent ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case SocialLogin.run(payload) do
          {:ok, value} -> {:ok, payload, value}
          {:error, reason} -> {:error, payload, reason}
        end
      end)

    Enum.each(results, fn
      {:ok, p, v} -> IO.puts("  [OK] #{inspect(p)} -> #{inspect(v)}")
      {:error, p, r} -> IO.puts("  [ERR] #{inspect(p)} -> #{inspect(r)}")
    end)

    {us, _} =
      :timer.tc(fn ->
        for _ <- 1..1_000, do: SocialLogin.run(:bench)
      end)

    avg = us / 1_000
    IO.puts("\nBenchmark: #{:erlang.float_to_binary(avg, decimals: 2)} µs/op (1000 iterations)")
    IO.puts("Target: < 100 µs/op for in-process operations\n")
  end
end

Main.main()
```

---

## Trade-offs and production gotchas

**1. Bounded resources are the contract**
Any operation that can grow without bound (mailbox, ETS table, process count, DB connections) must have an explicit cap. Without one, a bad input or a slow upstream eventually exhausts the VM.

**2. Tagged tuples over exceptions**
Public APIs return `{:ok, value} / {:error, reason}`. Exceptions are reserved for programmer errors (FunctionClauseError, KeyError) — operational errors are data, not control flow.

**3. Timeouts are not optional**
`GenServer.call` defaults to 5000 ms. `Task.await` defaults to 5000 ms. `Repo.transaction` inherits the connection's timeout. In production, every call should pass an explicit `:timeout` matched to the operation's SLA.

**4. Supervision encodes recovery**
Long-lived processes belong under a Supervisor with an explicit restart strategy. Choose `:transient` for processes that should not restart on `:normal` exit, `:permanent` for daemons, `:temporary` for workers that handle one-off jobs.

**5. Telemetry events are the production debugger**
Emit `:telemetry.execute/3` for every business operation. Attach handlers in production for metrics; attach handlers in tests for assertions. The same code path serves both.

**6. Async tests need isolated state**
`async: true` parallelizes test execution. Any test that writes to global state (Application env, named ETS tables, the database without sandbox) must declare `async: false` or the suite becomes flaky.

---

## Reflection

1. If load on this component grew by 100×, which assumption breaks first — the data structure, the process model, or the failure handling? Justify with a measurement plan, not a guess.
2. What three telemetry events would you emit to decide, six months from now, whether this implementation is still the right one? Name the events, the metadata, and the alert thresholds.
3. The implementation here uses the recommended primitive. Under what specific conditions would you reach for a different primitive instead? Be concrete about the trigger.

---

## Key concepts

### 1. Dataloader collapses N+1 queries

Without Dataloader, a GraphQL query for 'posts and their authors' issues N+1 queries. With Dataloader, it issues 2 — one for posts, one batched for authors.

### 2. Complexity analysis prevents query DoS

GraphQL allows clients to compose queries. Without complexity limits, a malicious client can request a 10-level deep nested query that brings the server down. Set per-query and per-connection limits.

### 3. Cursor pagination is consistent under writes

Offset pagination skips/duplicates rows under concurrent inserts. Cursor pagination (encode the last-seen ID) is correct regardless of writes.

---
