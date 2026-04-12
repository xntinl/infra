# Phoenix.Token for Secure Signing and Verification

**Project**: `share_links` — a service that generates tamper-proof short-lived URLs (password reset, magic login, document share) using `Phoenix.Token`, with key rotation and constant-time verification.

## Project context

Your team keeps reinventing "signed URL" handling: HMAC-SHA256 wrappers, base64url encoders, ad-hoc expiry checks. Each reinvention has a subtle bug — one used `==` on signatures (timing attack), one forgot to include expiry in the signed payload (replay), one hardcoded the secret in the repo.

`Phoenix.Token` bundles all of this correctly: HMAC signing with the endpoint's secret key base, embedded `signed_at`, configurable TTL, key derivation via `Plug.Crypto.KeyGenerator`, constant-time comparison via `Plug.Crypto.secure_compare/2`. It is the canonical primitive in the Phoenix ecosystem.

```
share_links/
├── lib/
│   ├── share_links/
│   │   ├── application.ex
│   │   └── tokens.ex
│   └── share_links_web/
│       ├── endpoint.ex
│       └── controllers/
│           └── share_controller.ex
├── test/
│   └── share_links/
│       └── tokens_test.exs
├── bench/
│   └── token_bench.exs
└── mix.exs
```

## Why Phoenix.Token and not JWT

JWT requires picking an algorithm (RS256 vs HS256), parsing JOSE headers, dealing with `alg: none` attacks, and library bugs (`jose` had three CVEs in 2020–2023). It also serializes JSON, which is larger than binary.

`Phoenix.Token` is:
- HMAC-SHA256 (fixed; no algorithm confusion attacks).
- Uses the endpoint `secret_key_base` via `KeyGenerator.generate/3` (per-purpose key derivation — "user socket" ≠ "password reset").
- Embedded `signed_at` as monotonic-safe `System.os_time(:second)`.
- Binary, base64url-encoded; smaller than JWT.

**Why not `Plug.Crypto.MessageVerifier` directly?** `Phoenix.Token` wraps it. Use `MessageVerifier` only if you must ship tokens without the `signed_at`/TTL frame.

## Core concepts

### 1. Salt (namespace)

The second argument to `Phoenix.Token.sign/3` is a **salt string** — it derives a unique key per-purpose. `"password reset"` and `"user socket"` produce different keys from the same `secret_key_base`. If a password-reset token is leaked, an attacker cannot replay it as a socket auth token.

Treat the salt as a namespace, NOT as a secret. It can be public. Its purpose is key separation, not secrecy.

### 2. `signed_at` and `max_age`

`sign/3` embeds `System.os_time(:second)`. `verify/4` compares against `signed_at + max_age`. If you set `max_age: :infinity`, the token never expires — only use this for long-lived use cases like remember-me (and even then, rotate on every login).

### 3. `key_iterations`, `key_length`, `key_digest`

`KeyGenerator.generate/3` uses PBKDF2. Defaults (`1000` iterations, `32` bytes, `:sha256`) are fine for server-side verification. Higher iterations do NOT increase security here — the secret is already high entropy; PBKDF2 matters more for password-derived keys.

### 4. Constant-time compare

`Plug.Crypto.secure_compare/2` runs in constant time regardless of where the first differing byte is. This defeats timing attacks that would otherwise leak signature bits one-by-one.

## Design decisions

- **Option A — roll your own HMAC**: reinvent timing-safety, base64 encoding, expiry, key derivation. Security review becomes a nightmare.
- **Option B — JWT (`joken` or `jose`)**: more complex, algorithm confusion risks, larger tokens.
- **Option C — `Phoenix.Token`**: fixed algorithm, purpose-scoped keys, safe defaults.

Chosen: Option C. Reach for JWT only when you need cross-vendor interop (third-party SSO, open IDs).

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ShareLinks.MixProject do
  use Mix.Project
  def project, do: [app: :share_links, version: "0.1.0", elixir: "~> 1.16", deps: deps()]
  def application, do: [mod: {ShareLinks.Application, []}, extra_applications: [:logger, :crypto]]

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:plug_crypto, "~> 2.0"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"},
      {:benchee, "~> 1.3", only: :dev}
    ]
  end
end
```

### Step 1: Endpoint config (`config/config.exs` excerpt)

```elixir
# config/config.exs
import Config

config :share_links, ShareLinksWeb.Endpoint,
  url: [host: "localhost"],
  secret_key_base:
    System.get_env("SECRET_KEY_BASE") ||
      "fallback_64_byte_devonly_secret_XXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXXX"
```

**Production note**: generate with `mix phx.gen.secret`; 64 bytes minimum; store in a secret manager, never in the repo.

### Step 2: Token module — `lib/share_links/tokens.ex`

```elixir
defmodule ShareLinks.Tokens do
  @moduledoc """
  Purpose-scoped signed tokens.

  Each public function has its own salt so keys cannot be cross-used.
  A password-reset token is useless as a share-link token.
  """

  alias ShareLinksWeb.Endpoint

  @password_reset_salt "password reset v1"
  @share_salt "document share v1"
  @magic_login_salt "magic login v1"

  @password_reset_max_age 3600
  @share_max_age 7 * 86_400
  @magic_login_max_age 900

  # ---- Password reset ------------------------------------------------------

  @spec password_reset(integer()) :: binary()
  def password_reset(user_id) when is_integer(user_id) do
    Phoenix.Token.sign(Endpoint, @password_reset_salt, user_id)
  end

  @spec verify_password_reset(binary()) :: {:ok, integer()} | {:error, atom()}
  def verify_password_reset(token) when is_binary(token) do
    Phoenix.Token.verify(Endpoint, @password_reset_salt, token, max_age: @password_reset_max_age)
  end

  # ---- Document share ------------------------------------------------------

  @spec document_share(integer(), integer()) :: binary()
  def document_share(doc_id, recipient_id) do
    Phoenix.Token.sign(Endpoint, @share_salt, %{doc_id: doc_id, for: recipient_id})
  end

  def verify_document_share(token) do
    Phoenix.Token.verify(Endpoint, @share_salt, token, max_age: @share_max_age)
  end

  # ---- Magic login --------------------------------------------------------

  def magic_login(email) do
    Phoenix.Token.sign(Endpoint, @magic_login_salt, email)
  end

  def verify_magic_login(token) do
    Phoenix.Token.verify(Endpoint, @magic_login_salt, token, max_age: @magic_login_max_age)
  end
end
```

### Step 3: Controller — `lib/share_links_web/controllers/share_controller.ex`

```elixir
defmodule ShareLinksWeb.ShareController do
  use Phoenix.Controller, formats: [:html, :json]
  alias ShareLinks.Tokens

  def show(conn, %{"token" => token}) do
    case Tokens.verify_document_share(token) do
      {:ok, %{doc_id: id, for: recipient}} ->
        json(conn, %{doc_id: id, for: recipient})

      {:error, :expired} ->
        conn |> put_status(410) |> json(%{error: "link expired"})

      {:error, :invalid} ->
        conn |> put_status(403) |> json(%{error: "invalid signature"})

      {:error, :missing} ->
        conn |> put_status(400) |> json(%{error: "token required"})
    end
  end
end
```

## Why this works

`Phoenix.Token.sign/3` serializes the payload with `:erlang.term_to_binary/2` under a version header, HMACs it with the derived purpose key, and base64url-encodes. `verify/4` reverses that atomically: decode, compare signature in constant time, deserialize, check `signed_at + max_age > now`. A tampered payload fails the HMAC compare. An expired token fails the age check. A token from another salt fails the HMAC (different derived key).

## Tests — `test/share_links/tokens_test.exs`

```elixir
defmodule ShareLinks.TokensTest do
  use ExUnit.Case, async: true
  alias ShareLinks.Tokens

  describe "password reset" do
    test "valid token round-trips" do
      token = Tokens.password_reset(42)
      assert {:ok, 42} = Tokens.verify_password_reset(token)
    end

    test "tampered token is rejected" do
      token = Tokens.password_reset(42)
      tampered = String.replace_prefix(token, String.at(token, 0), "X")
      assert {:error, :invalid} = Tokens.verify_password_reset(tampered)
    end

    test "expired token is rejected" do
      token = Tokens.password_reset(42)
      # Sleep beyond max_age is impractical; simulate by calling with tiny max_age.
      Process.sleep(1_100)
      result =
        Phoenix.Token.verify(ShareLinksWeb.Endpoint, "password reset v1", token, max_age: 1)
      assert {:error, :expired} = result
    end
  end

  describe "purpose separation" do
    test "a password-reset token cannot be used as a document-share token" do
      token = Tokens.password_reset(42)
      assert {:error, :invalid} = Tokens.verify_document_share(token)
    end
  end

  describe "payload integrity" do
    test "structured payload round-trips" do
      token = Tokens.document_share(17, 99)
      assert {:ok, %{doc_id: 17, for: 99}} = Tokens.verify_document_share(token)
    end
  end
end
```

## Benchmark — `bench/token_bench.exs`

```elixir
Application.ensure_all_started(:share_links)
alias ShareLinks.Tokens

Benchee.run(
  %{
    "sign" => fn -> Tokens.document_share(1, 2) end,
    "verify" => fn t -> Tokens.verify_document_share(t) end
  },
  inputs: %{"token" => Tokens.document_share(1, 2)},
  before_scenario: fn input -> input end,
  time: 3,
  warmup: 1
)
```

**Expected**: `sign` ~40µs, `verify` ~50µs on modern hardware. Both dominated by PBKDF2 key derivation. Enable `:persistent_term` key caching (Plug.Crypto 2.0+ does this automatically) to drop to ~6µs.

## Trade-offs and production gotchas

**1. `secret_key_base` rotation is not free.** Rotating the key invalidates every outstanding token. Support two keys in parallel during the rotation window: verify against both, sign only with the new.

**2. Salt is not a secret.** Putting the salt in a public GitHub file is fine. Putting it in an env var "for extra safety" costs complexity without adding security.

**3. Token size grows with payload.** A `%{doc_id: 1}` token is ~140 bytes. A payload containing a list of 1000 ids is ~10 KB — the URL breaks. Keep payloads to opaque IDs.

**4. `max_age: :infinity` is a foot-gun.** If the URL is ever leaked (email forwarding, DLP logs), it is valid forever. Always set a finite age.

**5. Clock skew.** If one server is 10 minutes behind, tokens it issues look 10 minutes in the future to peers. `verify/4` handles this gracefully, but if you compare `signed_at` to your own clock manually, you can reject legitimate tokens. Use NTP.

**6. When NOT to use `Phoenix.Token`.** Interop with external services (OAuth2 IdPs, Auth0, Cognito) needs JWT/JWS. Use `Joken` for that, keeping `Phoenix.Token` for your internal purposes.

## Reflection

A reviewer suggests embedding the user's current password hash inside the password-reset token so the token auto-invalidates when the password is changed. Pros and cons of that design? Is there a simpler scheme that achieves the same guarantee?

## Resources

- [Phoenix.Token — hexdocs](https://hexdocs.pm/phoenix/Phoenix.Token.html)
- [Plug.Crypto.MessageVerifier source](https://github.com/elixir-plug/plug_crypto/blob/main/lib/plug/crypto/message_verifier.ex)
- [Plug.Crypto.KeyGenerator (PBKDF2)](https://github.com/elixir-plug/plug_crypto/blob/main/lib/plug/crypto/key_generator.ex)
- [Timing attacks on HMAC comparison — Crosby 2009](https://codahale.com/a-lesson-in-timing-attacks/)
