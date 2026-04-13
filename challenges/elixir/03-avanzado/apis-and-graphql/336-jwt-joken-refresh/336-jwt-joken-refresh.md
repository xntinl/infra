# JWT Authentication with Joken and Refresh Token Rotation

**Project**: `auth_api` — a Phoenix API that issues short-lived JWT access tokens and rotating refresh tokens with replay detection.

## Project context

`auth_api` serves as the identity layer for a SaaS product. The access control model is standard: clients exchange credentials for an access token (15 min) and a refresh token (30 days). Access tokens are JWTs, self-contained, and validated stateless. Refresh tokens are opaque strings stored server-side so they can be revoked and their use detected if leaked.

This exercise delivers the critical security mechanics: signed JWTs with `Joken`, a rotation strategy with **replay detection** (if a refresh token is used twice, invalidate the entire family), and a plug that authenticates every protected request. The senior discipline is to treat refresh rotation as the primary line of defense against stolen tokens — short access TTLs are not enough on their own.

```
auth_api/
├── lib/
│   ├── auth_api/
│   │   ├── application.ex
│   │   ├── repo.ex
│   │   ├── accounts.ex
│   │   ├── accounts/user.ex
│   │   ├── tokens/
│   │   │   ├── access_token.ex          # Joken module
│   │   │   ├── refresh_token.ex         # opaque, server-side stored
│   │   │   └── family.ex                # rotation + replay detection
│   └── auth_api_web/
│       ├── router.ex
│       ├── plugs/require_auth.ex
│       └── controllers/
│           └── session_controller.ex
├── test/
│   ├── auth_api/tokens/access_token_test.exs
│   └── auth_api/tokens/family_test.exs
└── mix.exs
```

## Why JWT for access and opaque for refresh

- **Access token = JWT**: the API verifies it with just a public key; no DB hit. Scales horizontally. TTL is short (5–15 min) so leakage has bounded impact.
- **Refresh token = opaque random string hashed server-side**: long-lived, must be revocable. A JWT refresh token cannot be revoked without a blocklist, which defeats the "stateless" win.

Putting a long TTL on a JWT because "JWT is simpler" is how tokens end up valid for 30 days after a user logs out.

## Why rotation with replay detection

The standard refresh flow is:

```
client: POST /refresh {refresh_token}
server: returns new access + new refresh, invalidates old refresh
```

If an attacker steals a refresh token and uses it before the legitimate client does, the server issues new tokens to the attacker. The victim's next refresh fails. **That failure is the signal**: a legitimate client presenting an already-rotated token means the token was stolen. Revoke the entire token family immediately.

This is the OAuth 2 "Refresh Token Rotation with Automatic Reuse Detection" pattern (RFC 6749 + BCP draft).

## Core concepts

### 1. Token family
A family is all refresh tokens descended from one login. Each rotation supersedes the previous. Detecting reuse of any ancestor kills every descendant.

### 2. Hash at rest
Refresh tokens are random 32-byte strings encoded as URL-safe base64. The DB stores `sha256(token)`, not the token. A DB dump does not yield usable tokens.

### 3. JWT claims minimum
`iss`, `sub`, `aud`, `exp`, `iat`, `jti`. Include a `typ: "access"` so a misconfigured verifier cannot accept a refresh as an access token.

## Design decisions

- **Option A — HS256 symmetric key**: pros: simplest; cons: every service that verifies needs the signing key. Leaks compound.
- **Option B — RS256 / EdDSA asymmetric**: pros: verifiers only need the public key; cons: key management overhead.
→ We pick **EdDSA** (`Ed25519`). Fast, small signatures, and asymmetric — verifiers get a public key only. HS256 is acceptable only for a monolith.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:ecto_sql, "~> 3.11"},
    {:postgrex, "~> 0.17"},
    {:joken, "~> 2.6"},
    {:joken_jwks, "~> 1.6"},     # for JWKS verification if you host public keys
    {:argon2_elixir, "~> 4.0"},
    {:plug_crypto, "~> 2.0"}
  ]
end
```

### Step 1: Access token module

**Objective**: Mint 15-minute EdDSA access tokens with `typ=access` and a random `jti` so short TTLs cap blast radius and replay is auditable by id.

```elixir
defmodule AuthApi.Tokens.AccessToken do
  use Joken.Config, default_signer: :default_signer

  @impl true
  def token_config do
    default_claims(
      iss: "auth_api",
      aud: "auth_api",
      default_exp: 15 * 60   # 15 minutes
    )
    |> add_claim("typ", fn -> "access" end, &(&1 == "access"))
    |> add_claim("sub", nil, &is_binary/1)
    |> add_claim("jti", fn -> Ecto.UUID.generate() end, &is_binary/1)
  end

  def generate_for(user_id) do
    {:ok, token, _claims} = generate_and_sign(%{"sub" => user_id})
    token
  end
end
```

Config for the signer:

```elixir
# config/config.exs
config :joken,
  default_signer: [
    signer_alg: "EdDSA",
    key_openssh: System.get_env("JWT_PRIVATE_KEY") || File.read!("priv/keys/ed25519"),
    key_pem: System.get_env("JWT_PUBLIC_KEY")     # verifier public key
  ]
```

### Step 2: Refresh token storage + family rotation

**Objective**: Store refresh tokens as sha256(random) with family_id + replaces_id chain so reuse detection revokes lineages and ties rotation to access token minting.

```elixir
defmodule AuthApi.Tokens.RefreshToken do
  use Ecto.Schema
  import Ecto.Changeset

  schema "refresh_tokens" do
    field :token_hash, :binary        # sha256 of the raw token
    field :family_id, Ecto.UUID       # shared across rotations
    field :user_id, :binary_id
    field :replaces_id, :integer      # parent in the rotation chain
    field :revoked_at, :utc_datetime_usec
    field :expires_at, :utc_datetime_usec
    timestamps(type: :utc_datetime_usec)
  end

  def changeset(struct, attrs) do
    struct
    |> cast(attrs, [:token_hash, :family_id, :user_id, :replaces_id, :expires_at])
    |> validate_required([:token_hash, :family_id, :user_id, :expires_at])
    |> unique_constraint(:token_hash)
  end
end
```

```elixir
defmodule AuthApi.Tokens.Family do
  @moduledoc """
  Issues, rotates, and revokes refresh tokens with reuse detection.
  """
  import Ecto.Query
  alias AuthApi.Repo
  alias AuthApi.Tokens.RefreshToken

  @ttl_seconds 30 * 24 * 3600

  @spec issue(String.t()) :: {:ok, raw_token :: String.t(), record :: RefreshToken.t()}
  def issue(user_id) do
    raw = random_token()
    family_id = Ecto.UUID.generate()
    insert!(raw, family_id, user_id, nil)
    {:ok, raw, nil}
  end

  @spec rotate(String.t()) ::
          {:ok, new_raw :: String.t(), access_jwt :: String.t()}
          | {:error, :invalid}
          | {:error, :reuse_detected}
  def rotate(raw_token) do
    hash = hash(raw_token)

    case Repo.get_by(RefreshToken, token_hash: hash) do
      nil ->
        {:error, :invalid}

      %RefreshToken{revoked_at: %DateTime{}, family_id: fam} ->
        # Reuse of a revoked token — kill the whole family.
        revoke_family!(fam)
        {:error, :reuse_detected}

      %RefreshToken{} = current ->
        if expired?(current) do
          {:error, :invalid}
        else
          rotate!(current)
        end
    end
  end

  @spec revoke_family!(Ecto.UUID.t()) :: :ok
  def revoke_family!(family_id) do
    now = DateTime.utc_now()
    from(t in RefreshToken, where: t.family_id == ^family_id, where: is_nil(t.revoked_at))
    |> Repo.update_all(set: [revoked_at: now])
    :ok
  end

  # ---------------- private ----------------

  defp rotate!(current) do
    Repo.transaction(fn ->
      mark_revoked!(current)
      new_raw = random_token()
      insert!(new_raw, current.family_id, current.user_id, current.id)
      access = AuthApi.Tokens.AccessToken.generate_for(current.user_id)
      {new_raw, access}
    end)
    |> case do
      {:ok, {raw, access}} -> {:ok, raw, access}
      {:error, _} -> {:error, :invalid}
    end
  end

  defp insert!(raw, family_id, user_id, replaces_id) do
    %RefreshToken{}
    |> RefreshToken.changeset(%{
      token_hash: hash(raw),
      family_id: family_id,
      user_id: user_id,
      replaces_id: replaces_id,
      expires_at: DateTime.add(DateTime.utc_now(), @ttl_seconds, :second)
    })
    |> Repo.insert!()
  end

  defp mark_revoked!(%RefreshToken{} = r) do
    r
    |> Ecto.Changeset.change(revoked_at: DateTime.utc_now())
    |> Repo.update!()
  end

  defp expired?(%RefreshToken{expires_at: exp}), do: DateTime.compare(exp, DateTime.utc_now()) == :lt

  defp random_token, do: :crypto.strong_rand_bytes(32) |> Base.url_encode64(padding: false)
  defp hash(raw), do: :crypto.hash(:sha256, raw)
end
```

### Step 3: Authentication plug

**Objective**: Extract 'Bearer JWT' from Authorization header and verify typ=access + exp in-flight so protected routes never touch the DB for token validation.

```elixir
defmodule AuthApiWeb.Plugs.RequireAuth do
  @behaviour Plug
  import Plug.Conn
  alias AuthApi.Tokens.AccessToken

  @impl true
  def init(opts), do: opts

  @impl true
  def call(conn, _opts) do
    with ["Bearer " <> jwt] <- get_req_header(conn, "authorization"),
         {:ok, %{"sub" => user_id, "typ" => "access"}} <- AccessToken.verify_and_validate(jwt) do
      assign(conn, :current_user_id, user_id)
    else
      _ -> conn |> send_resp(401, "") |> halt()
    end
  end
end
```

### Step 4: Session controller

**Objective**: Expose POST /session for login (mint pair) and POST /session/refresh for rotation (detect reuse, revoke family) so clients implement RFC 6749 token endpoint flows.

```elixir
defmodule AuthApiWeb.SessionController do
  use AuthApiWeb, :controller
  alias AuthApi.{Accounts, Tokens.Family, Tokens.AccessToken}

  def create(conn, %{"email" => email, "password" => password}) do
    case Accounts.authenticate(email, password) do
      {:ok, user} ->
        {:ok, refresh, _} = Family.issue(user.id)
        access = AccessToken.generate_for(user.id)
        json(conn, %{access_token: access, refresh_token: refresh})

      :error ->
        conn |> put_status(401) |> json(%{error: "invalid_credentials"})
    end
  end

  def refresh(conn, %{"refresh_token" => token}) do
    case Family.rotate(token) do
      {:ok, new_refresh, access} ->
        json(conn, %{access_token: access, refresh_token: new_refresh})

      {:error, :reuse_detected} ->
        conn |> put_status(401) |> json(%{error: "reuse_detected", action: "reauthenticate"})

      {:error, :invalid} ->
        conn |> put_status(401) |> json(%{error: "invalid_refresh"})
    end
  end
end
```

## Why this works

```
login ─▶ issue access (15m) + refresh_1 (30d, family F)
                                  │
time passes (access expires)      │
                                  ▼
POST /refresh refresh_1 ─▶ rotate
                          ├─ revoke refresh_1
                          ├─ issue refresh_2 (family F)
                          └─ return access + refresh_2

attacker replays refresh_1 ─▶ lookup finds it revoked
                          ▼
              revoke_family!(F) — refresh_2, refresh_3… all killed
              victim's next refresh returns :reuse_detected
              client MUST re-authenticate
```

The key property: **the old token staying in the DB in a `revoked` state is what enables detection**. If you deleted it on rotation, reuse would look identical to an unknown token.

## Tests

```elixir
defmodule AuthApi.Tokens.AccessTokenTest do
  use ExUnit.Case, async: true
  alias AuthApi.Tokens.AccessToken

  describe "access token" do
    test "round-trips sub and typ claims" do
      jwt = AccessToken.generate_for("user_42")
      assert {:ok, %{"sub" => "user_42", "typ" => "access"}} = AccessToken.verify_and_validate(jwt)
    end

    test "rejects a refresh-typed token" do
      {:ok, bad, _} = Joken.generate_and_sign(AccessToken.token_config(), %{"sub" => "u", "typ" => "refresh"})
      assert {:error, _} = AccessToken.verify_and_validate(bad)
    end
  end
end

defmodule AuthApi.Tokens.FamilyTest do
  use AuthApi.DataCase, async: true
  alias AuthApi.Tokens.Family

  describe "rotation" do
    test "normal rotation yields a new refresh" do
      {:ok, r1, _} = Family.issue("u1")
      assert {:ok, r2, _access} = Family.rotate(r1)
      refute r1 == r2
    end

    test "second rotation of the same token is reuse" do
      {:ok, r1, _} = Family.issue("u1")
      {:ok, _r2, _} = Family.rotate(r1)
      assert {:error, :reuse_detected} = Family.rotate(r1)
    end

    test "reuse invalidates the whole family" do
      {:ok, r1, _} = Family.issue("u1")
      {:ok, r2, _} = Family.rotate(r1)
      {:error, :reuse_detected} = Family.rotate(r1)

      # r2 must now be unusable
      assert {:error, _} = Family.rotate(r2)
    end

    test "unknown token is :invalid, not :reuse_detected" do
      assert {:error, :invalid} = Family.rotate("clearly-not-a-token")
    end
  end
end
```

## Benchmark

```elixir
Benchee.run(%{
  "verify JWT"        => fn -> AccessToken.verify_and_validate(jwt) end,
  "rotate refresh"    => fn -> Family.rotate(raw) end
})
```

**Expected**: JWT verification < 200 µs (EdDSA). Refresh rotation bounded by DB latency (~1 ms localhost).

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

**1. Clock skew**
If your API nodes have drifting clocks, tokens may be rejected as expired or accepted after expiry. NTP is non-optional; add a 30 s `leeway` on `exp` validation.

**2. Storing raw refresh tokens**
Never. Always hash. A read-only DB leak of hashes is not a compromise; a leak of raw tokens is.

**3. `verify_and_validate` without a typ check**
If you sign access and refresh tokens with the same key and don't check `typ`, an attacker can present a refresh JWT to a protected endpoint and it passes. Always validate `typ`.

**4. JWT in URL**
Do not put access tokens in query strings. Browser history, referer headers, access logs — all leak the token. Bearer header only.

**5. Logout without revocation**
If "logout" just deletes the access token client-side, the refresh still works. Revoke the family on logout.

**6. When NOT to use JWT**
For first-party sessions in a Phoenix monolith, `Plug.Session` with encrypted cookies is simpler and safer. JWT shines when you have multiple services verifying tokens without sharing a session store.

## Reflection

A mobile client loses network during rotation: it sent the request, the server issued the new refresh, but the response never arrived. The client retries with the old refresh → reuse detected → family revoked → user forced to re-login. How do you mitigate this without weakening reuse detection? (Hint: think about idempotency keys, short grace windows, or distinguishing "network retry" from "token theft".)

## Resources

- [RFC 6749 — OAuth 2.0](https://www.rfc-editor.org/rfc/rfc6749)
- [OAuth 2 Security BCP — Refresh token rotation](https://datatracker.ietf.org/doc/html/draft-ietf-oauth-security-topics)
- [Joken docs](https://hexdocs.pm/joken/)
- [Auth0 — Refresh Token Rotation](https://auth0.com/docs/secure/tokens/refresh-tokens/refresh-token-rotation)

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    # Add dependencies here
  ]
end
```
