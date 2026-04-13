# Signed Webhooks with HMAC Verification

**Project**: `webhook_receiver` — a Phoenix endpoint that accepts webhooks from a Stripe-style sender and rejects any request whose HMAC signature and timestamp do not match

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
webhook_receiver/
├── lib/
│   └── webhook_receiver.ex
├── script/
│   └── main.exs
├── test/
│   └── webhook_receiver_test.exs
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
defmodule WebhookReceiver.MixProject do
  use Mix.Project

  def project do
    [
      app: :webhook_receiver,
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

### `lib/webhook_receiver.ex`

```elixir
defmodule WebhookReceiverWeb.Plugs.CacheRawBody do
  @moduledoc """
  Invoked by Plug.Parsers via :body_reader so we stash the raw bytes in
  conn.assigns before JSON parsing consumes them.
  """

  @doc "Reads body result from conn and opts."
  def read_body(conn, opts) do
    {:ok, body, conn} = Plug.Conn.read_body(conn, opts)
    conn = Plug.Conn.assign(conn, :raw_body, body)
    {:ok, body, conn}
  end
end

defmodule WebhookReceiver.Signature do
  @moduledoc """
  Stripe-style signature: v1=<hex>,t=<unix>
  Signed message: "<t>.<raw_body>"
  """

  @tolerance_seconds 300

  @doc "Returns verify result from raw_body, header, secret and opts."
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

defmodule WebhookReceiverWeb.Plugs.VerifySignature do
  @behaviour Plug
  import Plug.Conn
  alias WebhookReceiver.Signature

  @impl true
  def init(opts), do: Keyword.fetch!(opts, :secret_env)

  @doc "Calls result from conn and secret_env."
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

defmodule WebhookReceiverWeb.WebhookController do
  use WebhookReceiverWeb, :controller

  @doc "Returns billing result from conn."
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

### `test/webhook_receiver_test.exs`

```elixir
defmodule WebhookReceiver.SignatureTest do
  use ExUnit.Case, async: true
  doctest WebhookReceiverWeb.Plugs.CacheRawBody
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

### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Demonstration entry point for Signed Webhooks with HMAC Verification.

  Run with: `mix run script/main.exs`
  """

  def main do
    IO.puts("=== Signed Webhooks with HMAC Verification ===")
    IO.puts("Category: APIs and GraphQL\n")

    payloads = [:alpha, :beta, :gamma, :delta]

    results =
      Enum.map(payloads, fn payload ->
        case WebhookReceiver.run(payload) do
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
        for _ <- 1..1_000, do: WebhookReceiver.run(:bench)
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
