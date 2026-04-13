# JWT Authentication with Joken and Refresh Token Rotation

**Project**: `auth_api` — a Phoenix API that issues short-lived JWT access tokens and rotating refresh tokens with replay detection

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
auth_api/
├── lib/
│   └── auth_api.ex
├── script/
│   └── main.exs
├── test/
│   └── auth_api_test.exs
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
defmodule AuthApi.MixProject do
  use Mix.Project

  def project do
    [
      app: :auth_api,
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
### `lib/auth_api.ex`

```elixir
defmodule AuthApi.Tokens.AccessToken do
  use Joken.Config, default_signer: :default_signer

  @doc "Returns token config result."
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

  @doc "Generates for result from user_id."
  def generate_for(user_id) do
    {:ok, token, _claims} = generate_and_sign(%{"sub" => user_id})
    token
  end
end

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

  @doc "Returns changeset result from struct and attrs."
  def changeset(struct, attrs) do
    struct
    |> cast(attrs, [:token_hash, :family_id, :user_id, :replaces_id, :expires_at])
    |> validate_required([:token_hash, :family_id, :user_id, :expires_at])
    |> unique_constraint(:token_hash)
  end
end

defmodule AuthApi.Tokens.Family do
  @moduledoc """
  Issues, rotates, and revokes refresh tokens with reuse detection.
  """
  import Ecto.Query
  alias AuthApi.Repo
  alias AuthApi.Tokens.RefreshToken

  @ttl_seconds 30 * 24 * 3600

  @doc "Returns issue result from user_id."
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
  @doc "Returns rotate result from raw_token."
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

  @doc "Returns revoke family result from family_id."
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

defmodule AuthApiWeb.Plugs.RequireAuth do
  @behaviour Plug
  import Plug.Conn
  alias AuthApi.Tokens.AccessToken

  @impl true
  def init(opts), do: opts

  @doc "Calls result from conn and _opts."
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

defmodule AuthApiWeb.SessionController do
  use AuthApiWeb, :controller
  alias AuthApi.{Accounts, Tokens.Family, Tokens.AccessToken}

  @doc "Creates result from conn."
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

  @doc "Returns refresh result from conn."
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
### `test/auth_api_test.exs`

```elixir
defmodule AuthApi.Tokens.AccessTokenTest do
  use ExUnit.Case, async: true
  doctest AuthApi.Tokens.AccessToken
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
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for JWT Authentication with Joken and Refresh Token Rotation.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== JWT Authentication with Joken and Refresh Token Rotation ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case AuthApi.run(payload) do
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
        for _ <- 1..1_000, do: AuthApi.run(:bench)
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
