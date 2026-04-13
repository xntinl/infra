# Custom Authentication Plug — `Guardian` vs `Joken` vs Hand-Rolled

**Project**: `plug_auth_custom` — production-grade bearer auth for an internal API.

---

## The business problem

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

## Project structure

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
├── script/
│   └── main.exs
└── mix.exs
```

---

## Why custom auth plug and not a library

Auth libraries are the right default, but their assumptions leak. When the model is 'verify a token from a header, look up a user, assign to conn', a plug is 30 lines with no mystery.

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

### `mix.exs`
```elixir
defmodule PlugAuthCustom.MixProject do
  use Mix.Project

  def project do
    [
      app: :plug_auth_custom,
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

Signed tokens are stateless. You can't "un-sign" them. Two options:

- **Short expiry + refresh**: Access token TTL of 5–15 minutes. Refresh token (opaque,
  stored server-side) issues new access tokens. Revoking the refresh breaks the chain
  within one TTL window. This is the OAuth2 answer.
- **Revocation list**: A Set of `jti` (JWT ID) values that `verify` must reject, even
  when the signature is valid. Stored in ETS for speed, replicated via PubSub.
  This is the "kill switch" pattern.

In practice, production systems use both. This exercise implements the revocation list.

A plug is *data in, data out* (`%Plug.Conn{}` → `%Plug.Conn{}`). It composes in the
endpoint pipeline, in a scoped Phoenix pipeline, or in a plain `Plug.Router`. A library
that wraps authentication into an `@before_compile` macro is convenient until you need
to skip it for one specific controller action — and the only escape hatch is a flag in
the socket options. Stay at the Plug layer; you keep composition and testability.

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

### `script/main.exs`
```elixir
defmodule PlugAuthCustom.MixProject do
  use Mix.Project

  def project do
    [app: :plug_auth_custom, version: "0.1.0", elixir: "~> 1.19", deps: deps()]
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

Signed tokens are stateless. You can't "un-sign" them. Two options:

- **Short expiry + refresh**: Access token TTL of 5–15 minutes. Refresh token (opaque,
  stored server-side) issues new access tokens. Revoking the refresh breaks the chain
  within one TTL window. This is the OAuth2 answer.
- **Revocation list**: A Set of `jti` (JWT ID) values that `verify` must reject, even
  when the signature is valid. Stored in ETS for speed, replicated via PubSub.
  This is the "kill switch" pattern.

In practice, production systems use both. This exercise implements the revocation list.

A plug is *data in, data out* (`%Plug.Conn{}` → `%Plug.Conn{}`). It composes in the
endpoint pipeline, in a scoped Phoenix pipeline, or in a plain `Plug.Router`. A library
that wraps authentication into an `@before_compile` macro is convenient until you need
to skip it for one specific controller action — and the only escape hatch is a flag in
the socket options. Stay at the Plug layer; you keep composition and testability.

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

---

## Why Custom Authentication Plug — `Guardian` vs `Joken` vs Hand-Rolled matters

Mastering **Custom Authentication Plug — `Guardian` vs `Joken` vs Hand-Rolled** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `lib/plug_auth_custom.ex`

```elixir
defmodule PlugAuthCustom do
  @moduledoc """
  Reference implementation for Custom Authentication Plug — `Guardian` vs `Joken` vs Hand-Rolled.

  See the sections above for design rationale, trade-offs, and the business
  problem this module addresses.
  """

  @doc """
  Entry point for the plug_auth_custom module. Replace the body with the real
  implementation once you have worked through the exercise.

  ## Examples

      iex> PlugAuthCustom.run(:noop)
      :ok

  """
  @spec run(term()) :: :ok
  def run(_input) do
    :ok
  end
end
```

### `test/plug_auth_custom_test.exs`

```elixir
defmodule PlugAuthCustomTest do
  use ExUnit.Case, async: true

  doctest PlugAuthCustom

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert PlugAuthCustom.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts

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
