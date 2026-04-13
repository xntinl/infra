# Custom Authentication Plug — `Guardian` vs `Joken` vs Hand-Rolled

**Project**: `plug_auth_custom` — production-grade bearer auth for an internal API.

---

## Project context

`plug_auth_custom` is the authentication plug for an internal platform API. The API
is called by other services inside the same cluster (service-to-service JWTs) and, via
the BFF, by browser clients (short-lived cookies + refresh tokens). The plug has to:

1. Validate the `Authorization: Bearer <token>` header.
2. Handle both JWT-signed tokens (from the OAuth provider) **and** `Phoenix.Token`-signed
   cookies (from the BFF).
3. Enforce revocation via a small ETS revocation set populated by a PubSub listener.
4. Be testable without touching the real signing keys.

The Elixir ecosystem offers two well-known JWT libraries — **Guardian** and **Joken** —
plus the first-party `Phoenix.Token`. Each solves a different problem. This exercise
compares them head-to-head, then builds a production plug that picks the right tool per
token type.

```
plug_auth_custom/
├── lib/
│   └── plug_auth_custom/
│       ├── application.ex
│       ├── revocation.ex
│       ├── tokens/
│       │   ├── access_token.ex
│       │   └── session_token.ex
│       └── plugs/
│           ├── authenticate.ex
│           └── require_scope.ex
├── test/
│   └── plug_auth_custom/
│       └── plugs/
│           ├── authenticate_test.exs
│           └── require_scope_test.exs
└── mix.exs
```

---

## Why custom auth plug and not a library

Auth libraries are the right default, but their assumptions leak. When the model is 'verify a token from a header, look up a user, assign to conn', a plug is 30 lines with no mystery.

---

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
### 1. Bearer extraction at the plug boundary

Every piece of auth code that reads the `Authorization` header reimplements the same
four lines. Extract them once:

```elixir
defp extract_bearer(conn) do
  case Plug.Conn.get_req_header(conn, "authorization") do
    ["Bearer " <> token] when byte_size(token) > 0 -> {:ok, token}
    _ -> {:error, :missing}
  end
end
```

Pattern-matching on the exact prefix (note the space) avoids the classic bug where
`"bearer"` lowercase or `"Bearer  "` with two spaces slips through. Compare: `String.starts_with?`
is more permissive and also much slower for malicious inputs.

### 2. `Phoenix.Token` vs JWT — what the libraries actually do

| | `Phoenix.Token` | `Joken` | `Guardian` |
|-|-----------------|---------|------------|
| Encodes | Any Erlang term | JSON claims | JSON claims |
| Signature | HMAC-SHA256 on binary | JWS (HS, RS, ES, EdDSA) | JWS (via Joken) |
| Expiration | `max_age:` on verify | `exp` claim | `exp` claim |
| Issuer/audience | No | Hooks | Built-in config |
| Hooks | None | Signer modules, callbacks | Pipelines, `before_*` |
| JWT-compliant | No | Yes | Yes |
| Good for | Same-app cookies | Library of primitives | Opinionated plug stack |

Rules of thumb:

- **Same-app producer and consumer** → `Phoenix.Token`. Sub-microsecond sign/verify, no
  claim juggling, uses `secret_key_base`.
- **External API consuming a third-party JWT** → `Joken`. Low-level, composable,
  directly maps to JOSE primitives. Write your own plug.
- **You want a full auth pipeline with hooks for login/logout/refresh and remember-me**
  → `Guardian`. It's a framework; accept that.

### 3. The revocation problem

Signed tokens are stateless. You can't "un-sign" them. Two options:

- **Short expiry + refresh**: Access token TTL of 5–15 minutes. Refresh token (opaque,
  stored server-side) issues new access tokens. Revoking the refresh breaks the chain
  within one TTL window. This is the OAuth2 answer.
- **Revocation list**: A Set of `jti` (JWT ID) values that `verify` must reject, even
  when the signature is valid. Stored in ETS for speed, replicated via PubSub.
  This is the "kill switch" pattern.

In practice, production systems use both. This exercise implements the revocation list.

### 4. Why a Plug, not a library

A plug is *data in, data out* (`%Plug.Conn{}` → `%Plug.Conn{}`). It composes in the
endpoint pipeline, in a scoped Phoenix pipeline, or in a plain `Plug.Router`. A library
that wraps authentication into an `@before_compile` macro is convenient until you need
to skip it for one specific controller action — and the only escape hatch is a flag in
the socket options. Stay at the Plug layer; you keep composition and testability.

### 5. Late-binding the token issuer

The plug receives two token types (JWT access, Phoenix.Token session) and has to route
them to the right verifier. You could inspect header fields (`alg`, `kid`) to guess,
but a simpler rule works: JWTs have three base64url segments separated by `.`,
Phoenix.Tokens have two segments separated by `.`. Inspect the segment count:

```elixir
defp token_type(token) do
  case :binary.matches(token, ".") |> length() do
    2 -> :jwt
    1 -> :session
    _ -> :unknown
  end
end
```

Crude but reliable. An adversary can't forge their token type — they'd have to produce
a valid signature under the other verifier to trick it, which is the point.

---

## Design decisions

**Option A — library like Guardian or Pow**
- Pros: batteries included; well-tested.
- Cons: pulls in opinions you may not share; customization fights the library.

**Option B — custom auth plug** (chosen)
- Pros: exactly the auth model you need; no dependency surface.
- Cons: all auth subtleties (timing attacks, token rotation, session fixation) are yours.

→ Chose **B** because when the auth model is non-standard, a custom plug is shorter than bending a library.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pull in `plug_crypto` and `joken` so both Phoenix.Token session tokens and HS256 JWT access tokens can coexist in one pipeline.

```elixir
defmodule PlugAuthCustom.MixProject do
  use Mix.Project

  def project do
    [app: :plug_auth_custom, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [mod: {PlugAuthCustom.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:plug, "~> 1.16"},
      {:plug_crypto, "~> 2.0"},
      {:phoenix, "~> 1.7"},
      {:jason, "~> 1.4"},
      {:joken, "~> 2.6"}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defp extract_bearer(conn) do
  case Plug.Conn.get_req_header(conn, "authorization") do
    ["Bearer " <> token] when byte_size(token) > 0 -> {:ok, token}
    _ -> {:error, :missing}
  end
end
```

Pattern-matching on the exact prefix (note the space) avoids the classic bug where
`"bearer"` lowercase or `"Bearer  "` with two spaces slips through. Compare: `String.starts_with?`
is more permissive and also much slower for malicious inputs.

### 2. `Phoenix.Token` vs JWT — what the libraries actually do

| | `Phoenix.Token` | `Joken` | `Guardian` |
|-|-----------------|---------|------------|
| Encodes | Any Erlang term | JSON claims | JSON claims |
| Signature | HMAC-SHA256 on binary | JWS (HS, RS, ES, EdDSA) | JWS (via Joken) |
| Expiration | `max_age:` on verify | `exp` claim | `exp` claim |
| Issuer/audience | No | Hooks | Built-in config |
| Hooks | None | Signer modules, callbacks | Pipelines, `before_*` |
| JWT-compliant | No | Yes | Yes |
| Good for | Same-app cookies | Library of primitives | Opinionated plug stack |

Rules of thumb:

- **Same-app producer and consumer** → `Phoenix.Token`. Sub-microsecond sign/verify, no
  claim juggling, uses `secret_key_base`.
- **External API consuming a third-party JWT** → `Joken`. Low-level, composable,
  directly maps to JOSE primitives. Write your own plug.
- **You want a full auth pipeline with hooks for login/logout/refresh and remember-me**
  → `Guardian`. It's a framework; accept that.

### 3. The revocation problem

Signed tokens are stateless. You can't "un-sign" them. Two options:

- **Short expiry + refresh**: Access token TTL of 5–15 minutes. Refresh token (opaque,
  stored server-side) issues new access tokens. Revoking the refresh breaks the chain
  within one TTL window. This is the OAuth2 answer.
- **Revocation list**: A Set of `jti` (JWT ID) values that `verify` must reject, even
  when the signature is valid. Stored in ETS for speed, replicated via PubSub.
  This is the "kill switch" pattern.

In practice, production systems use both. This exercise implements the revocation list.

### 4. Why a Plug, not a library

A plug is *data in, data out* (`%Plug.Conn{}` → `%Plug.Conn{}`). It composes in the
endpoint pipeline, in a scoped Phoenix pipeline, or in a plain `Plug.Router`. A library
that wraps authentication into an `@before_compile` macro is convenient until you need
to skip it for one specific controller action — and the only escape hatch is a flag in
the socket options. Stay at the Plug layer; you keep composition and testability.

### 5. Late-binding the token issuer

The plug receives two token types (JWT access, Phoenix.Token session) and has to route
them to the right verifier. You could inspect header fields (`alg`, `kid`) to guess,
but a simpler rule works: JWTs have three base64url segments separated by `.`,
Phoenix.Tokens have two segments separated by `.`. Inspect the segment count:

```elixir
defp token_type(token) do
  case :binary.matches(token, ".") |> length() do
    2 -> :jwt
    1 -> :session
    _ -> :unknown
  end
end
```

Crude but reliable. An adversary can't forge their token type — they'd have to produce
a valid signature under the other verifier to trick it, which is the point.

---

## Design decisions

**Option A — library like Guardian or Pow**
- Pros: batteries included; well-tested.
- Cons: pulls in opinions you may not share; customization fights the library.

**Option B — custom auth plug** (chosen)
- Pros: exactly the auth model you need; no dependency surface.
- Cons: all auth subtleties (timing attacks, token rotation, session fixation) are yours.

→ Chose **B** because when the auth model is non-standard, a custom plug is shorter than bending a library.

---

## Implementation

### Step 1: `mix.exs`

**Objective**: Pull in `plug_crypto` and `joken` so both Phoenix.Token session tokens and HS256 JWT access tokens can coexist in one pipeline.

```elixir
defmodule PlugAuthCustom.MixProject do
  use Mix.Project

  def project do
    [app: :plug_auth_custom, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [mod: {PlugAuthCustom.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:plug, "~> 1.16"},
      {:plug_crypto, "~> 2.0"},
      {:phoenix, "~> 1.7"},
      {:jason, "~> 1.4"},
      {:joken, "~> 2.6"}
    ]
  end
end
```

### Step 2: `lib/plug_auth_custom/revocation.ex`

**Objective**: Keep revocations in a named ETS set so `revoked?/1` is O(1) and shareable across plug processes without a GenServer bottleneck.

```elixir
defmodule PlugAuthCustom.Revocation do
  @moduledoc """
  In-memory revocation list. Keyed on `jti` for JWTs, `{user_id, issued_at}`
  for session tokens. Replicated across the cluster by subscribing to the
  `revocations` PubSub topic.
  """

  @table :auth_revocations

  def start do
    :ets.new(@table, [:named_table, :public, :set, read_concurrency: true])
  end

  @spec revoke(term()) :: :ok
  def revoke(jti) do
    :ets.insert(@table, {jti, System.system_time(:second)})
    :ok
  end

  @spec revoked?(term()) :: boolean()
  def revoked?(jti), do: :ets.member(@table, jti)
end
```

### Step 3: `lib/plug_auth_custom/tokens/access_token.ex`

**Objective**: Issue HS256 JWTs with iss/aud/exp/jti/scp claims via Joken so verification is declarative and revocation hooks on the jti.

```elixir
defmodule PlugAuthCustom.Tokens.AccessToken do
  @moduledoc """
  JWT signer/verifier for service-to-service access tokens.

  Uses HS256 with a rotating shared secret. Claims:

      iss: "platform"
      aud: "internal-api"
      sub: "<user_id>"
      exp: <unix seconds>
      jti: <uuid>
      scp: ["read:reports", ...]
  """

  use Joken.Config

  @impl true
  def token_config do
    default_claims(
      iss: "platform",
      aud: "internal-api",
      default_exp: 15 * 60
    )
    |> add_claim("scp", nil, &is_list/1)
    |> add_claim("jti", fn -> UUID.uuid4() end, &is_binary/1)
  end

  @spec signer() :: Joken.Signer.t()
  def signer do
    secret = Application.fetch_env!(:plug_auth_custom, :access_token_secret)
    Joken.Signer.create("HS256", secret)
  end

  @spec sign(String.t(), [String.t()]) :: {:ok, String.t()}
  def sign(user_id, scopes) do
    {:ok, claims} = generate_claims(%{"sub" => user_id, "scp" => scopes})
    {:ok, jwt, _claims} = encode_and_sign(claims, signer())
    {:ok, jwt}
  end

  @spec verify(String.t()) :: {:ok, map()} | {:error, atom()}
  def verify(jwt) do
    with {:ok, claims} <- verify_and_validate(jwt, signer()) do
      cond do
        PlugAuthCustom.Revocation.revoked?(claims["jti"]) -> {:error, :revoked}
        true -> {:ok, claims}
      end
    end
  end
end

# Minimal UUID helper so we don't pull in the full uuid lib.
defmodule UUID do
  def uuid4 do
    <<a::32, b::16, c::16, d::16, e::48>> = :crypto.strong_rand_bytes(16)

    :io_lib.format(~c"~8.16.0b-~4.16.0b-4~3.16.0b-~4.16.0b-~12.16.0b", [
      a,
      b,
      Bitwise.band(c, 0x0FFF),
      Bitwise.bor(Bitwise.band(d, 0x3FFF), 0x8000),
      e
    ])
    |> IO.iodata_to_binary()
  end
end
```

### Step 4: `lib/plug_auth_custom/tokens/session_token.ex`

**Objective**: Sign `{user_id, issued_at}` via `Phoenix.Token` with a 12h max_age so BFF session cookies stay short-lived and revocation-aware.

```elixir
defmodule PlugAuthCustom.Tokens.SessionToken do
  @moduledoc """
  Phoenix.Token-based session tokens issued by the BFF layer.

  Payload shape:  `{user_id, issued_at_unix}`
  Max age:        `12h`
  Salt:           fixed per-purpose constant (see `@salt`)
  """

  @salt "plug_auth_custom session"
  @max_age 12 * 3_600

  @spec sign(module(), String.t()) :: String.t()
  def sign(endpoint_or_secret, user_id) do
    Phoenix.Token.sign(endpoint_or_secret, @salt, {user_id, System.system_time(:second)})
  end

  @spec verify(module(), String.t()) ::
          {:ok, %{user_id: String.t(), issued_at: integer()}} | {:error, atom()}
  def verify(endpoint_or_secret, token) do
    with {:ok, {user_id, issued_at}} <-
           Phoenix.Token.verify(endpoint_or_secret, @salt, token, max_age: @max_age) do
      cond do
        PlugAuthCustom.Revocation.revoked?({user_id, issued_at}) -> {:error, :revoked}
        true -> {:ok, %{user_id: user_id, issued_at: issued_at}}
      end
    else
      {:error, :expired} -> {:error, :expired}
      {:error, _} -> {:error, :invalid}
    end
  end
end
```

### Step 5: `lib/plug_auth_custom/plugs/authenticate.ex`

**Objective**: Dispatch bearer tokens to JWT or session verifiers based on dot-count so one plug authenticates both token families before `current_user` assigns.

```elixir
defmodule PlugAuthCustom.Plugs.Authenticate do
  @moduledoc """
  Authenticates the request by extracting the bearer token, dispatching to
  the right verifier, and populating `conn.assigns.current_user`.

  On failure returns `401` with a JSON body and halts the pipeline.

  Options:

    * `:endpoint` — module used by `Phoenix.Token` for session tokens
      (required). Usually `MyAppWeb.Endpoint`.
  """

  @behaviour Plug

  import Plug.Conn

  alias PlugAuthCustom.Tokens.{AccessToken, SessionToken}

  @impl true
  def init(opts) do
    endpoint = Keyword.fetch!(opts, :endpoint)
    %{endpoint: endpoint}
  end

  @impl true
  def call(conn, %{endpoint: endpoint}) do
    with {:ok, token} <- extract_bearer(conn),
         {:ok, identity} <- verify(token, endpoint) do
      conn
      |> assign(:current_user, identity)
      |> put_private(:auth_token, token)
    else
      {:error, reason} -> unauthorized(conn, reason)
    end
  end

  defp extract_bearer(conn) do
    case get_req_header(conn, "authorization") do
      ["Bearer " <> token] when byte_size(token) > 0 -> {:ok, token}
      _ -> {:error, :missing}
    end
  end

  defp verify(token, endpoint) do
    case token_type(token) do
      :jwt ->
        with {:ok, claims} <- AccessToken.verify(token) do
          {:ok, %{user_id: claims["sub"], scopes: claims["scp"] || [], kind: :access}}
        end

      :session ->
        with {:ok, %{user_id: user_id}} <- SessionToken.verify(endpoint, token) do
          {:ok, %{user_id: user_id, scopes: ["session"], kind: :session}}
        end

      :unknown ->
        {:error, :invalid}
    end
  end

  defp token_type(token) do
    case length(:binary.matches(token, ".")) do
      2 -> :jwt
      1 -> :session
      _ -> :unknown
    end
  end

  defp unauthorized(conn, reason) do
    body = Jason.encode!(%{error: "unauthorized", reason: reason})

    conn
    |> put_resp_content_type("application/json")
    |> send_resp(401, body)
    |> halt()
  end
end
```

### Step 6: `lib/plug_auth_custom/plugs/require_scope.ex`

**Objective**: Enforce scope membership after Authenticate so missing scopes return 403, not 401, preserving correct semantics for client retry logic.

```elixir
defmodule PlugAuthCustom.Plugs.RequireScope do
  @moduledoc """
  Requires that the authenticated user carries a specific scope.

  Must run AFTER `Authenticate`. Returns `403 Forbidden` when the scope is
  missing.
  """

  @behaviour Plug
  import Plug.Conn

  @impl true
  def init(scope) when is_binary(scope), do: scope

  @impl true
  def call(%Plug.Conn{assigns: %{current_user: %{scopes: scopes}}} = conn, scope) do
    if scope in scopes do
      conn
    else
      body = Jason.encode!(%{error: "forbidden", required_scope: scope})

      conn
      |> put_resp_content_type("application/json")
      |> send_resp(403, body)
      |> halt()
    end
  end

  def call(conn, _scope), do: conn |> send_resp(401, "") |> halt()
end
```

### Step 7: `lib/plug_auth_custom/application.ex`

**Objective**: Define the OTP application and supervision tree in `lib/plug_auth_custom/application.ex`.

```elixir
defmodule PlugAuthCustom.Application do
  use Application

  @impl true
  def start(_type, _args) do
    PlugAuthCustom.Revocation.start()
    Supervisor.start_link([], strategy: :one_for_one, name: PlugAuthCustom.Supervisor)
  end
end
```

### Step 8: Tests

**Objective**: Add tests that cover the expected behavior and edge cases.

```elixir
# test/plug_auth_custom/plugs/authenticate_test.exs
defmodule PlugAuthCustom.Plugs.AuthenticateTest do
  use ExUnit.Case, async: false
  use Plug.Test

  alias PlugAuthCustom.Plugs.Authenticate
  alias PlugAuthCustom.Revocation
  alias PlugAuthCustom.Tokens.{AccessToken, SessionToken}

  # A dummy endpoint module for Phoenix.Token signing. The only thing
  # Phoenix.Token needs from it is `config(:secret_key_base, _)`.
  defmodule FakeEndpoint do
    def config(:secret_key_base),
      do: "01234567890123456789012345678901234567890123456789012345678901234567890123456789"

    def config(:secret_key_base, _default), do: config(:secret_key_base)
  end

  @opts Authenticate.init(endpoint: FakeEndpoint)

  setup do
    Application.put_env(:plug_auth_custom, :access_token_secret, "test-hs256-secret-32-chars-ok!!")
    :ets.delete_all_objects(:auth_revocations)
    :ok
  end

  describe "missing/garbage headers" do
    test "401 when no authorization header" do
      conn = conn(:get, "/") |> Authenticate.call(@opts)

      assert conn.status == 401
      assert conn.halted
      assert %{"reason" => "missing"} = Jason.decode!(conn.resp_body)
    end

    test "401 on garbage token" do
      conn =
        conn(:get, "/")
        |> put_req_header("authorization", "Bearer xxx.yyy.zzz")
        |> Authenticate.call(@opts)

      assert conn.status == 401
    end
  end

  describe "JWT access token" do
    test "assigns current_user on valid JWT" do
      {:ok, jwt} = AccessToken.sign("u_42", ["read:reports"])

      conn =
        conn(:get, "/")
        |> put_req_header("authorization", "Bearer " <> jwt)
        |> Authenticate.call(@opts)

      refute conn.halted
      assert conn.assigns.current_user.user_id == "u_42"
      assert conn.assigns.current_user.scopes == ["read:reports"]
      assert conn.assigns.current_user.kind == :access
    end

    test "401 when jti has been revoked" do
      {:ok, jwt} = AccessToken.sign("u_42", [])
      {:ok, claims} = AccessToken.verify(jwt)
      Revocation.revoke(claims["jti"])

      conn =
        conn(:get, "/")
        |> put_req_header("authorization", "Bearer " <> jwt)
        |> Authenticate.call(@opts)

      assert conn.status == 401
    end
  end

  describe "Phoenix.Token session" do
    test "assigns current_user on valid session token" do
      session = SessionToken.sign(FakeEndpoint, "u_7")

      conn =
        conn(:get, "/")
        |> put_req_header("authorization", "Bearer " <> session)
        |> Authenticate.call(@opts)

      refute conn.halted
      assert conn.assigns.current_user.user_id == "u_7"
      assert conn.assigns.current_user.kind == :session
    end
  end
end
```

```elixir
# test/plug_auth_custom/plugs/require_scope_test.exs
defmodule PlugAuthCustom.Plugs.RequireScopeTest do
  use ExUnit.Case, async: true
  use Plug.Test

  alias PlugAuthCustom.Plugs.RequireScope

  test "403 when scope missing" do
    conn =
      conn(:get, "/")
      |> Plug.Conn.assign(:current_user, %{user_id: "u_1", scopes: ["other"]})
      |> RequireScope.call("admin")

    assert conn.status == 403
    assert conn.halted
  end

  test "passthrough when scope present" do
    conn =
      conn(:get, "/")
      |> Plug.Conn.assign(:current_user, %{user_id: "u_1", scopes: ["admin"]})
      |> RequireScope.call("admin")

    refute conn.halted
  end

  test "401 when no current_user" do
    conn = conn(:get, "/") |> RequireScope.call("admin")
    assert conn.status == 401
    assert conn.halted
  end
end
```

### Why this works

A Plug is a module with `init/1` and `call/2`. The auth plug pulls the token, verifies it (Phoenix.Token or JWT), looks up the user, and `assign/3`s them to the conn. Downstream plugs read `conn.assigns[:current_user]`.

---

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. Don't use `Phoenix.Token` for third-party clients.** It's Erlang-term-serialized;
no JS library parses it. Use JWT.

**2. Don't use JWT for cookies inside your own app.** `Phoenix.Token` is simpler,
faster, and does not leak claim-name/shape into a client that has no use for it.

**3. Revocation is not free.** Every `verify` does an ETS lookup. That's ~100ns, fine,
but the revocation table replication cost is not — if you PubSub every revocation and
have thousands per minute, you're paying cluster bandwidth. Batch revocations or
prefer short TTLs plus a refresh flow.

**4. Scope is NOT role.** Guardian conflates them; don't. A scope is a capability
granted to a token (`read:reports`); a role is an attribute of the user (`admin`).
A user with role admin whose token lacks `read:reports` cannot read reports. Keep
them separate to avoid "my admin bypass token still worked after I removed admin"
bugs.

**5. HS256 key rotation is painful.** HMAC shares the same key between signer and
verifier. If you rotate, both producers and consumers must be updated simultaneously.
Use RS256 (asymmetric) if the signer and verifier live in different deployment units
— it lets you rotate the private signing key independently.

**6. `halt/1` vs not halting.** Every failure path must call `halt/1`. Forgetting it
means subsequent plugs in the pipeline still run — potentially inserting the
unauthorized request into an audit log as if it were authenticated. Lint for this.

**7. Never log the token.** Structured logging on `conn.private[:auth_token]` is an
exfiltration vector. Log the `jti` or `user_id` after verification instead.

**8. When NOT to use this.** If your API is entirely behind a service mesh with mTLS
(Istio, Linkerd, Consul Connect), the mesh already authenticates every call and
passes identity via headers (`x-forwarded-client-cert` or `x-user-id`). Your plug
should just read those headers; adding a second auth layer is redundant ceremony.

---

## Benchmark

```elixir
# :timer.tc / Benchee measurement sketch
{time_us, _} = :timer.tc(fn -> :ok end)
IO.puts("elapsed: #{time_us} us")
```

Target: auth plug adds 100-500 us including token verification (dominated by crypto).

---

## Reflection

- Your auth plug now needs to support three token types. Does it stay one plug, or split into three? What is the composition story?
- A timing attack could distinguish valid from invalid tokens. Which parts of your plug leak timing, and what do you do about it?

---

## Executable Example

```elixir
defmodule PlugAuthCustom.MixProject do
  use Mix.Project

  def project do
    [app: :plug_auth_custom, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  end

  def application do
    [mod: {PlugAuthCustom.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:plug, "~> 1.16"},
      {:plug_crypto, "~> 2.0"},
      {:phoenix, "~> 1.7"},
      {:jason, "~> 1.4"},
      {:joken, "~> 2.6"}
    ]
  end
end



Pattern-matching on the exact prefix (note the space) avoids the classic bug where
`"bearer"` lowercase or `"Bearer  "` with two spaces slips through. Compare: `String.starts_with?`
is more permissive and also much slower for malicious inputs.

### 2. `Phoenix.Token` vs JWT — what the libraries actually do
  end

| | `Phoenix.Token` | `Joken` | `Guardian` |
|-|-----------------|---------|------------|
| Encodes | Any Erlang term | JSON claims | JSON claims |
| Signature | HMAC-SHA256 on binary | JWS (HS, RS, ES, EdDSA) | JWS (via Joken) |
| Expiration | `max_age:` on verify | `exp` claim | `exp` claim |
| Issuer/audience | No | Hooks | Built-in config |
| Hooks | None | Signer modules, callbacks | Pipelines, `before_*` |
| JWT-compliant | No | Yes | Yes |
| Good for | Same-app cookies | Library of primitives | Opinionated plug stack |

Rules of thumb:

- **Same-app producer and consumer** → `Phoenix.Token`. Sub-microsecond sign/verify, no
  claim juggling, uses `secret_key_base`.
- **External API consuming a third-party JWT** → `Joken`. Low-level, composable,
  directly maps to JOSE primitives. Write your own plug.
- **You want a full auth pipeline with hooks for login/logout/refresh and remember-me**
  → `Guardian`. It's a framework; accept that.

### 3. The revocation problem

Signed tokens are stateless. You can't "un-sign" them. Two options:

- **Short expiry + refresh**: Access token TTL of 5–15 minutes. Refresh token (opaque,
  stored server-side) issues new access tokens. Revoking the refresh breaks the chain
  within one TTL window. This is the OAuth2 answer.
- **Revocation list**: A Set of `jti` (JWT ID) values that `verify` must reject, even
  when the signature is valid. Stored in ETS for speed, replicated via PubSub.
  This is the "kill switch" pattern.

In practice, production systems use both. This exercise implements the revocation list.

### 4. Why a Plug, not a library

A plug is *data in, data out* (`%Plug.Conn{}` → `%Plug.Conn{}`). It composes in the
endpoint pipeline, in a scoped Phoenix pipeline, or in a plain `Plug.Router`. A library
that wraps authentication into an `@before_compile` macro is convenient until you need
to skip it for one specific controller action — and the only escape hatch is a flag in
the socket options. Stay at the Plug layer; you keep composition and testability.

### 5. Late-binding the token issuer

The plug receives two token types (JWT access, Phoenix.Token session) and has to route
them to the right verifier. You could inspect header fields (`alg`, `kid`) to guess,
but a simpler rule works: JWTs have three base64url segments separated by `.`,
Phoenix.Tokens have two segments separated by `.`. Inspect the segment count:

defmodule Main do
  def main do
      # Demonstrating 222-plug-auth-custom
      :ok
  end
end

Main.main()
```
