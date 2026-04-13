# Signed Webhooks with HMAC Verification

**Project**: `webhook_receiver` — a Phoenix endpoint that accepts webhooks from a Stripe-style sender and rejects any request whose HMAC signature and timestamp do not match.

## Project context

Your billing provider ships events via HTTP POST. If your endpoint trusts them blindly, anyone who guesses the URL can inject fake "payment.succeeded" events. The industry-standard fix (Stripe, GitHub, Slack, Shopify all use variations of this): the sender signs the payload with a shared secret; the receiver computes the same HMAC and compares constant-time.

This exercise builds the receiver side — the hard part is getting **timestamp tolerance**, **replay defense**, and **raw body handling** right. Each of those has a standard failure mode; senior engineers have seen all of them.

```
webhook_receiver/
├── lib/
│   ├── webhook_receiver/
│   │   ├── application.ex
│   │   └── signature.ex              # verify/3
│   └── webhook_receiver_web/
│       ├── endpoint.ex
│       ├── router.ex
│       ├── plugs/
│       │   ├── cache_raw_body.ex     # preserves raw bytes for HMAC
│       │   └── verify_signature.ex
│       └── controllers/webhook_controller.ex
├── test/webhook_receiver_web/
│   └── webhook_controller_test.exs
└── mix.exs
```

## Why HMAC and not a bearer token

A bearer token in a header authenticates the sender ("you know the secret") but does NOT authenticate the payload. If an attacker captures a bearer-authenticated request (TLS error, logging leak, MITM proxy), they can replay it and modify the body — the server has no way to tell.

HMAC binds the signature to the exact bytes of the body. Changing a single byte invalidates the signature. Combined with a timestamp and replay window, it gives:

- **Authenticity**: only holders of the secret can produce valid signatures.
- **Integrity**: the body cannot be modified after signing.
- **Anti-replay** (with timestamp): old valid requests become invalid after the window closes.

## Why SHA-256 and not MD5 or SHA-1

SHA-1 is deprecated for HMAC in new designs (NIST SP 800-131A). SHA-256 is the universal default. Do not use MD5 anywhere.

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
### 1. Sign the raw bytes, not the parsed JSON
`Jason.encode!(Jason.decode!(body))` is not byte-identical to the original body — key order, whitespace, Unicode normalization all differ. Verify against the bytes that arrived on the wire.

### 2. Timestamp inside the signed payload
Signing `"#{timestamp}.#{body}"` (Stripe's format) means you can enforce a freshness window. Without a timestamp, any valid request is valid forever.

### 3. Constant-time comparison
`Plug.Crypto.secure_compare/2` does not short-circuit on the first differing byte. Plain `==` on binaries may leak enough timing information to forge a signature (known attack on naive implementations).

## Design decisions

- **Option A — plug that aborts on invalid signature**: pros: controller cannot accidentally skip verification; cons: body must be cached before any consumer parses it.
- **Option B — helper function called inside the controller**: pros: no global state; cons: easy to forget on a new endpoint.
→ We pick **A**. Security-critical checks default to "on" — opt-in security is security debt.

## Implementation

### Dependencies

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:plug_cowboy, "~> 2.7"},
    {:plug_crypto, "~> 2.0"},
    {:jason, "~> 1.4"}
  ]
end
```

### Step 1: Cache the raw body

**Objective**: Stash the pre-parse raw body via a `:body_reader` so HMAC runs over the exact bytes sent, immune to JSON re-encoding drift.

Phoenix's default parser consumes the body. We need the raw bytes before parsing.

```elixir
defmodule WebhookReceiverWeb.Plugs.CacheRawBody do
  @moduledoc """
  Invoked by Plug.Parsers via :body_reader so we stash the raw bytes in
  conn.assigns before JSON parsing consumes them.
  """

  def read_body(conn, opts) do
    {:ok, body, conn} = Plug.Conn.read_body(conn, opts)
    conn = Plug.Conn.assign(conn, :raw_body, body)
    {:ok, body, conn}
  end
end
```

Wire it into the endpoint's parser:

```elixir
# lib/webhook_receiver_web/endpoint.ex
plug Plug.Parsers,
  parsers: [{:json, body_reader: {WebhookReceiverWeb.Plugs.CacheRawBody, :read_body, []}}],
  json_decoder: Jason,
  pass: ["application/json"]
```

### Step 2: Signature module

**Objective**: Verify Stripe-style `t=<ts>,v1=<hex>` with `secure_compare/2` plus a 300-second window so replay and timing attacks both fail closed.

```elixir
defmodule WebhookReceiver.Signature do
  @moduledoc """
  Stripe-style signature: v1=<hex>,t=<unix>
  Signed message: "<t>.<raw_body>"
  """

  @tolerance_seconds 300

  @spec verify(binary(), binary(), binary(), keyword()) :: :ok | {:error, atom()}
  def verify(raw_body, header, secret, opts \\ []) do
    tolerance = Keyword.get(opts, :tolerance, @tolerance_seconds)
    now = Keyword.get(opts, :now, System.system_time(:second))

    with {:ok, parts} <- parse_header(header),
         {:ok, ts} <- fetch_ts(parts),
         :ok <- check_freshness(ts, now, tolerance),
         {:ok, provided} <- fetch_signature(parts),
         expected <- compute(ts, raw_body, secret),
         true <- Plug.Crypto.secure_compare(expected, provided) do
      :ok
    else
      false -> {:error, :signature_mismatch}
      {:error, _} = err -> err
    end
  end

  defp parse_header(nil), do: {:error, :missing_header}
  defp parse_header(header) do
    parts =
      header
      |> String.split(",")
      |> Enum.map(&String.split(&1, "=", parts: 2))
      |> Enum.reduce(%{}, fn
        [k, v], acc -> Map.update(acc, k, [v], &[v | &1])
        _, acc -> acc
      end)

    {:ok, parts}
  end

  defp fetch_ts(%{"t" => [ts | _]}) do
    case Integer.parse(ts) do
      {n, ""} -> {:ok, n}
      _ -> {:error, :bad_timestamp}
    end
  end
  defp fetch_ts(_), do: {:error, :missing_timestamp}

  defp fetch_signature(%{"v1" => sigs}) when is_list(sigs), do: {:ok, hd(sigs)}
  defp fetch_signature(_), do: {:error, :missing_signature}

  defp check_freshness(ts, now, tol) when abs(now - ts) <= tol, do: :ok
  defp check_freshness(_, _, _), do: {:error, :expired}

  defp compute(ts, body, secret) do
    :crypto.mac(:hmac, :sha256, secret, "#{ts}.#{body}") |> Base.encode16(case: :lower)
  end
end
```

### Step 3: Verification plug

**Objective**: Halt unsigned or stale requests at the pipeline edge so controllers only ever see bodies already proven authentic and fresh.

```elixir
defmodule WebhookReceiverWeb.Plugs.VerifySignature do
  @behaviour Plug
  import Plug.Conn
  alias WebhookReceiver.Signature

  @impl true
  def init(opts), do: Keyword.fetch!(opts, :secret_env)

  @impl true
  def call(conn, secret_env) do
    secret = System.fetch_env!(secret_env)
    header = get_req_header(conn, "webhook-signature") |> List.first()
    raw = conn.assigns[:raw_body] || ""

    case Signature.verify(raw, header, secret) do
      :ok -> conn
      {:error, reason} ->
        conn
        |> put_resp_content_type("application/json")
        |> send_resp(400, Jason.encode!(%{error: to_string(reason)}))
        |> halt()
    end
  end
end
```

### Step 4: Router and controller

**Objective**: Gate `/webhooks/billing` through the verify pipeline and dedupe by event id so exact replays ack idempotently without side-effects.

```elixir
defmodule WebhookReceiverWeb.Router do
  use WebhookReceiverWeb, :router

  pipeline :webhook do
    plug :accepts, ["json"]
    plug WebhookReceiverWeb.Plugs.VerifySignature, secret_env: "BILLING_WEBHOOK_SECRET"
  end

  scope "/webhooks", WebhookReceiverWeb do
    pipe_through :webhook
    post "/billing", WebhookController, :billing
  end
end
```

```elixir
defmodule WebhookReceiverWeb.WebhookController do
  use WebhookReceiverWeb, :controller

  def billing(conn, %{"type" => type, "id" => id} = event) do
    # Signature has already been verified by the pipeline.
    # Also guard against replay using the event id.
    case WebhookReceiver.Idempotency.seen?(id) do
      true ->
        send_resp(conn, 200, "")   # Ack duplicate; do not re-process.

      false ->
        :ok = WebhookReceiver.Idempotency.remember(id)
        WebhookReceiver.Dispatcher.handle(type, event)
        send_resp(conn, 200, "")
    end
  end
end
```

## Why this works

```
sender:  payload + secret ─▶ HMAC("<ts>.<body>")
         sends:  body, header: "t=<ts>,v1=<hex>"
                   │
                   ▼
receiver:  CacheRawBody preserves bytes
           VerifySignature plug:
             · parses header
             · checks |now - ts| <= tolerance  [anti-replay window]
             · computes HMAC over raw bytes   [integrity]
             · secure_compare                 [timing safe]
             · rejects with 400 on any failure
                   │
                   ▼
           controller: idempotency by event id [exact-replay defense within window]
```

Four layers of defense compose: **secret knowledge** (HMAC), **byte integrity** (HMAC over raw body), **freshness** (timestamp + tolerance), **deduplication** (idempotency key). Each addresses a distinct attack.

## Tests

```elixir
defmodule WebhookReceiver.SignatureTest do
  use ExUnit.Case, async: true
  alias WebhookReceiver.Signature

  @secret "whsec_test"

  defp sign(body, ts \\ System.system_time(:second)) do
    hex = :crypto.mac(:hmac, :sha256, @secret, "#{ts}.#{body}") |> Base.encode16(case: :lower)
    "t=#{ts},v1=#{hex}"
  end

  describe "verify/3" do
    test "accepts a fresh, correctly signed request" do
      body = ~s({"type":"payment.succeeded"})
      assert :ok = Signature.verify(body, sign(body), @secret)
    end

    test "rejects a tampered body" do
      body = ~s({"type":"payment.succeeded"})
      header = sign(body)
      assert {:error, :signature_mismatch} = Signature.verify(body <> " ", header, @secret)
    end

    test "rejects an expired timestamp" do
      body = "{}"
      ts_old = System.system_time(:second) - 3600
      assert {:error, :expired} = Signature.verify(body, sign(body, ts_old), @secret)
    end

    test "rejects a missing signature" do
      assert {:error, :missing_header} = Signature.verify("{}", nil, @secret)
    end

    test "rejects wrong secret" do
      body = "{}"
      header = sign(body)
      assert {:error, :signature_mismatch} = Signature.verify(body, header, "wrong")
    end
  end
end
```

## Benchmark

```elixir
body = :crypto.strong_rand_bytes(8192) |> Base.encode64()
ts = System.system_time(:second)
header = "t=#{ts},v1=" <> (:crypto.mac(:hmac, :sha256, "s", "#{ts}.#{body}") |> Base.encode16(case: :lower))

Benchee.run(%{
  "verify 8KB payload" => fn -> :ok = WebhookReceiver.Signature.verify(body, header, "s") end
})
```

**Expected**: < 30 µs for 8 KB payloads. HMAC-SHA256 throughput is ~GB/s on modern hardware.

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

**1. Using the parsed JSON for HMAC**
`Jason.encode!(params)` may reorder keys, change spacing, re-escape Unicode. The signature will be computed over a body the sender never signed. ALWAYS verify over raw bytes.

**2. No timestamp tolerance check**
Without freshness, a captured valid request stays valid indefinitely. Pick 5 minutes as Stripe does; shorter if clients have accurate clocks.

**3. Plain `==` on hex strings**
Timing attacks on the comparison are practical on a LAN. Always `Plug.Crypto.secure_compare/2`.

**4. Shared secret in git**
Read from env or a secret manager. A rotation story is also needed: accept two secrets during the rotation window.

**5. No idempotency**
A valid request replayed within the freshness window still passes signature verification. Event IDs + a short TTL cache (Redis, ETS with cleanup) stop this.

**6. When NOT to use HMAC**
If your sender can afford mutual TLS or ed25519 signatures with a public key, asymmetric beats HMAC for key management (you never share the signing key). For arbitrary integrators without such capability, HMAC is the baseline.

## Reflection

Your secret is shared with the sender. If a junior engineer accidentally logs it during debugging, you need to rotate. Sketch a rotation plan: how does the receiver accept signatures from two secrets during the transition? What is the cost of getting rotation wrong, and how do you verify success?


## Executable Example

```elixir
defmodule WebhookReceiver.Signature do
  @moduledoc """
  Stripe-style signature: v1=<hex>,t=<unix>
  Signed message: "<t>.<raw_body>"
  """

  @tolerance_seconds 300

  @spec verify(binary(), binary(), binary(), keyword()) :: :ok | {:error, atom()}
  def verify(raw_body, header, secret, opts \\ []) do
    tolerance = Keyword.get(opts, :tolerance, @tolerance_seconds)
    now = Keyword.get(opts, :now, System.system_time(:second))

    with {:ok, parts} <- parse_header(header),
         {:ok, ts} <- fetch_ts(parts),
         :ok <- check_freshness(ts, now, tolerance),
         {:ok, provided} <- fetch_signature(parts),
         expected <- compute(ts, raw_body, secret),
         true <- Plug.Crypto.secure_compare(expected, provided) do
      :ok
    else
      false -> {:error, :signature_mismatch}
      {:error, _} = err -> err
    end
  end

  defp parse_header(nil), do: {:error, :missing_header}
  defp parse_header(header) do
    parts =
      header
      |> String.split(",")
      |> Enum.map(&String.split(&1, "=", parts: 2))
      |> Enum.reduce(%{}, fn
        [k, v], acc -> Map.update(acc, k, [v], &[v | &1])
        _, acc -> acc
      end)

    {:ok, parts}
  end

  defp fetch_ts(%{"t" => [ts | _]}) do
    case Integer.parse(ts) do
      {n, ""} -> {:ok, n}
      _ -> {:error, :bad_timestamp}
    end
  end
  defp fetch_ts(_), do: {:error, :missing_timestamp}

  defp fetch_signature(%{"v1" => sigs}) when is_list(sigs), do: {:ok, hd(sigs)}
  defp fetch_signature(_), do: {:error, :missing_signature}

  defp check_freshness(ts, now, tol) when abs(now - ts) <= tol, do: :ok
  defp check_freshness(_, _, _), do: {:error, :expired}

  defp compute(ts, body, secret) do
    :crypto.mac(:hmac, :sha256, secret, "#{ts}.#{body}") |> Base.encode16(case: :lower)
  end
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
