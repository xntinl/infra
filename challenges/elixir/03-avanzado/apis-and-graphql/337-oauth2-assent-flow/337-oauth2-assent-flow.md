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

## Resources

- [RFC 6749 — OAuth 2.0](https://www.rfc-editor.org/rfc/rfc6749)
- [RFC 7636 — PKCE](https://www.rfc-editor.org/rfc/rfc7636)
- [Assent hexdocs](https://hexdocs.pm/assent/)
- [OAuth 2.1 draft](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-v2-1)
- [OWASP — Authentication cheatsheet](https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html)
