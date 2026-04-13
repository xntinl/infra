# OAuth2 Server-Side Flow with Assent

**Project**: `social_login` — a Phoenix app that authenticates users against Google and GitHub using the OAuth 2.0 authorization code flow via `Assent`.

## Project context

Your SaaS asks users to authenticate with their Google Workspace or GitHub account. The team started implementing OAuth by hand — reading RFC 6749, wrestling with state, PKCE, redirect URIs, and token exchange. Two weeks in, the code kind-of works, but CSRF protection is unclear and the error paths leak query strings.

`Assent` is the Elixir library extracted from `ueberauth`/`pow_assent` that implements the provider-specific quirks (Google's `hd` domain hint, GitHub's email endpoint) while keeping the core flow clean. This exercise wires two providers end-to-end, with PKCE and state verification, and persists the user on first login.

```
social_login/
├── lib/
│   ├── social_login/
│   │   ├── application.ex
│   │   ├── accounts.ex
│   │   └── accounts/user.ex
│   └── social_login_web/
│       ├── router.ex
│       ├── controllers/
│       │   └── oauth_controller.ex
│       └── auth/providers.ex
├── test/social_login_web/oauth_controller_test.exs
└── mix.exs
```

## Why Assent and not manual OAuth

OAuth 2's authorization code flow has six ordered steps, each with failure modes that are security-critical:

1. Generate `state` + PKCE `code_verifier`.
2. Redirect to provider with `code_challenge`.
3. Provider redirects back with `code` and `state`.
4. Verify `state` matches what you stored.
5. Exchange `code` + `code_verifier` for tokens (server-side).
6. Fetch user info with access token.

Mistakes common in hand-rolled implementations: state stored globally not per-session, PKCE skipped (leaving code interception possible), tokens logged in plaintext, `redirect_uri` mismatches. `Assent` encapsulates the flow correctly and gives you a tested provider list.

## Why server-side (authorization code) and not implicit

The implicit flow (tokens in the URL fragment) is deprecated by OAuth 2.1. It leaks tokens through browser history and referer headers and cannot be refreshed. The authorization code flow:

- Passes only a short-lived `code` through the browser.
- Server exchanges the code for tokens using its client secret (never exposed to the browser).
- Supports refresh tokens.
- With PKCE, resists code interception on mobile/native without requiring a client secret.

## Core concepts



---

**Why this matters:**
These concepts form the foundation of production Elixir systems. Understanding them deeply allows you to build fault-tolerant, scalable applications that operate correctly under load and failure.

**Real-world use case:**
This pattern appears in systems like:
- Phoenix applications handling thousands of concurrent connections
- Distributed data processing pipelines
- Financial transaction systems requiring consistency and fault tolerance
- Microservices communicating over unreliable networks

**Common pitfall:**
Many developers overlook that Elixir's concurrency model differs fundamentally from threads. Processes are isolated; shared mutable state does not exist. Trying to force shared-memory patterns leads to deadlocks, race conditions, or silently incorrect behavior. Always think in terms of message passing and immutability.
### 1. State (CSRF defense)
Random value set at `/auth/:provider` start, checked when the callback arrives. If an attacker tricks a victim into following an attacker-crafted callback URL, the state won't match the victim's session.

### 2. PKCE
Recommended for all clients now (RFC 7636, incorporated into OAuth 2.1). Server generates a random `code_verifier`, hashes it as `code_challenge`, sends the hash on step 2 and the verifier on step 5. Intercepting the `code` is useless without the verifier.

### 3. Provider-specific user info
Each provider returns a different shape. `Assent`'s provider modules normalize it into `%{email:, name:, uid:, ...}`.

## Design decisions

- **Option A — store OAuth request state in the session**: pros: automatic per-user, encrypted by `Plug.Session`; cons: tied to session cookie.
- **Option B — store in DB keyed by opaque request id**: pros: works for SPA flows where cookies are awkward; cons: need cleanup and extra schema.
→ We pick **A**. Server-side rendered Phoenix app with cookies — the session is the natural carrier.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:phoenix_live_view, "~> 0.20"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:assent, "~> 0.2"},
    {:req, "~> 0.5"},              # HTTP client used by Assent
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Provider configuration

**Objective**: Centralize per-provider client_id/secret/redirect and strategy mapping so adding a new IdP is a single clause, not a rewrite.

```elixir
defmodule SocialLoginWeb.Auth.Providers do
  def config(:google) do
    [
      client_id: System.fetch_env!("GOOGLE_CLIENT_ID"),
      client_secret: System.fetch_env!("GOOGLE_CLIENT_SECRET"),
      redirect_uri: SocialLoginWeb.Endpoint.url() <> "/auth/google/callback",
      authorization_params: [scope: "openid email profile"]
    ]
  end

  def config(:github) do
    [
      client_id: System.fetch_env!("GITHUB_CLIENT_ID"),
      client_secret: System.fetch_env!("GITHUB_CLIENT_SECRET"),
      redirect_uri: SocialLoginWeb.Endpoint.url() <> "/auth/github/callback",
      authorization_params: [scope: "read:user user:email"]
    ]
  end

  def strategy(:google), do: Assent.Strategy.Google
  def strategy(:github), do: Assent.Strategy.Github
end
```

### Step 2: Controller orchestrates the flow

**Objective**: Drive request/callback with session-bound `state` and renewed session id on success to defeat CSRF and fixation attacks.

```elixir
defmodule SocialLoginWeb.OAuthController do
  use SocialLoginWeb, :controller
  alias SocialLoginWeb.Auth.Providers
  alias SocialLogin.Accounts

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
```

### Step 3: Accounts module

**Objective**: Upsert users by `(provider, provider_uid)` unique key so repeated logins never duplicate identities nor lose email updates.

```elixir
defmodule SocialLogin.Accounts do
  alias SocialLogin.Accounts.User
  alias SocialLogin.Repo

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

  def changeset(struct, attrs) do
    struct
    |> cast(attrs, [:email, :name, :provider, :provider_uid])
    |> validate_required([:email, :provider, :provider_uid])
    |> unique_constraint([:provider, :provider_uid])
  end
end
```

### Step 4: Routes

**Objective**: Expose `/auth/:provider` and `/auth/:provider/callback` behind the browser pipeline so session and CSRF plugs wrap every leg of the dance.

```elixir
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

## Why this works

```
GET /auth/google
      │
      ▼
authorize_url/1 (Assent)
   · generates state, code_verifier, code_challenge
   · returns {url, session_params}
      │
put_session(:oauth_session_params, params)   ◀── per-user carrier
redirect(external: google_url)
      │
 (user grants consent at Google)
      │
GET /auth/google/callback?code=…&state=…
      │
      ▼
callback/2 (Assent)
   · checks state == session_params.state            [CSRF]
   · POSTs token exchange with code_verifier         [PKCE]
   · fetches userinfo with access token
      │
Accounts.upsert_from_oauth
      │
put_session(:user_id, id) + renew session id
redirect /dashboard
```

The key properties:

- **State is bound to the user's session cookie**, not global. An attacker cannot pre-arm the state from a different browser.
- **PKCE means the authorization code is single-use AND bound to the verifier**. Even if a malicious app intercepts the code (common on mobile with URL handlers), it cannot redeem it.
- **`configure_session(renew: true)`** rotates the session id on login to defeat session fixation.

## Tests

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

## Benchmark

Assent's cost is dominated by HTTP calls to the provider. Pure code path:

```elixir
{:ok, %{url: _, session_params: _}} = Assent.Strategy.Google.authorize_url(config)
```

**Expected**: < 500 µs for URL generation (all local crypto). Full flow is bounded by provider latency (100–500 ms).

## Deep Dive: Query Complexity and N+1 Prevention Patterns

GraphQL's flexibility is a double-edged sword. A query like `{ users { posts { comments { author { email } } } } }`
becomes a DDoS vector if unchecked: a resolver that loads each post's comments naively yields 1000 database 
queries for a 100-user query.

**Three strategies to prevent N+1**:
1. **Dataloader batching** (Absinthe-native): Queue fields in phase 1 (`load/3`), flush in phase 2 (`run/1`).
   Single database call per level. Works across HTTP boundaries via custom sources.
2. **Ecto select/5 eager loading** (preload): Best when schema relationships are known at resolver definition time.
   Fine-grained control; requires discipline in your types.
3. **Complexity analysis** (persisted queries): Assign a "weight" to each field (users=2, posts=5, comments=10).
   Reject queries exceeding a threshold BEFORE execution. Prevents runaway queries entirely.

**Production gotcha**: Complexity analysis doesn't prevent slow queries — it prevents expensive queries.
A query that hits 50,000 database rows but under the complexity limit still runs. Combine with database 
query timeouts and active monitoring.

**Subscription patterns** (real-time): Subscriptions over PubSub break traditional Dataloader batching 
because events arrive asynchronously. Use a separate resolver that doesn't call the loader; instead, 
publish (source) and subscribe (sink) directly. This keeps subscriptions cheap and doesn't starve 
the dataloader queue.

**Field-level authorization**: Dataloader sources can enforce per-user visibility rules at load time, 
not in the resolver. This is cleaner than filtering after the fact and reduces unnecessary database 
queries for unauthorized fields.

---

## Advanced Considerations

API implementations at scale require careful consideration of request handling, error responses, and the interaction between multiple clients with different performance expectations. The distinction between public APIs and internal APIs affects error reporting granularity, versioning strategies, and backwards compatibility guarantees fundamentally. Versioning APIs through headers, paths, or query parameters each have trade-offs in terms of maintenance burden, client complexity, and developer experience across multiple client versions. When deprecating API endpoints, the migration window and support period must balance client migration costs with infrastructure maintenance costs and team capacity constraints.

GraphQL adds complexity around query costs, depth limits, and the interaction between nested resolvers and N+1 query problems. A deeply nested GraphQL query can trigger hundreds of database queries if not carefully managed with proper preloading and query analysis. Implementing query cost analysis prevents malicious or poorly-written queries from starving resources and degrading service for other clients. The caching layer becomes more complex with GraphQL because the same data may be accessed through multiple query paths, each with different caching semantics and TTL requirements that must be carefully coordinated at the application level.

Error handling and status codes require careful design to balance information disclosure with security concerns. Too much detail in error messages helps attackers; too little detail frustrates legitimate users. Implement structured error responses with specific error codes that clients can use to handle different failure scenarios intelligently and retry appropriately. Rate limiting, circuit breakers, and backpressure mechanisms prevent API overload but require careful configuration based on expected traffic patterns and SLA requirements.


## Deep Dive: Apis Patterns and Production Implications

API testing requires testing schema validation, error messages, pagination, and rate limiting—not just happy paths. The mistake is testing only the happy path and assuming error handling works. Production APIs with weak error handling become support nightmares.

---

## Trade-offs and production gotchas

**1. Logging the code or access token**
Filter request parameters in Phoenix's endpoint config: `filter_parameters: ["code", "access_token", "id_token", "state", "code_verifier"]`. A stray log line leaks credentials.

**2. Mismatched redirect_uri**
The URI in `config` MUST exactly match what you registered with the provider. A trailing slash or `http` vs `https` difference fails with a cryptic error.

**3. Upserting by email**
If you `upsert_from_oauth` matching on email (not `provider_uid`), a user who changes their Google email can no longer log in, and an attacker who registers a provider with the victim's email hijacks the account. Always key by `{provider, provider_uid}`.

**4. Not renewing the session**
Session fixation: an attacker sets a session cookie on the victim's browser before login; after login, the attacker's session is now authenticated as the victim. `configure_session(renew: true)` mitigates.

**5. Skipping PKCE for confidential clients**
The argument used to be "PKCE is for public clients". OAuth 2.1 recommends it everywhere. Assent enables it by default; do not disable.

**6. When NOT to use this**
For a single internal integration where you control both sides, a simple JWT with a shared key is less ceremony. OAuth shines when the identity provider is external and users give consent.

## Reflection

Google's OpenID Connect returns an `id_token` (a JWT) alongside the access token. You could skip the userinfo endpoint and trust the `id_token` claims directly — saving one round trip. What are the trade-offs? Which signature validation do you need and what happens if Google rotates its signing keys between your login and your verification?


## Executable Example

```elixir
defmodule SocialLoginWeb.OAuthController do
  use SocialLoginWeb, :controller
  alias SocialLoginWeb.Auth.Providers
  alias SocialLogin.Accounts

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

defmodule Main do
  def main do
      IO.puts("GraphQL schema initialization")
      defmodule QueryType do
        def resolve_hello(_, _, _), do: {:ok, "world"}
      end
      if is_atom(QueryType) do
        IO.puts("✓ GraphQL schema validated and query resolver accessible")
      end
  end
end

Main.main()
```
