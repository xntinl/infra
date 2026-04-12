# Contract Testing Between Services — a Pact-Style Approach in Elixir

**Project**: `payments_consumer` and `payments_provider` — two Elixir services whose compatibility is enforced by consumer-driven contracts stored as JSON pacts and verified on both sides.

## Project context

A fintech has two services: `payments_consumer` (a Phoenix app calling the payments API)
and `payments_provider` (the Phoenix service exposing it). They evolve independently —
different teams, different release cadence. Every few months a prod incident happens:
the provider renamed a field or the consumer added a required param without coordination.

End-to-end tests across both services in CI are slow, flaky, and only run on rare
integration branches. **Consumer-driven contracts** (Pact) are the right tool: the
consumer declares "when I send X I expect Y", the contract is saved to a file (a
"pact"), and the provider's CI verifies it can still satisfy that contract on every
commit.

No mature Pact library exists natively in Elixir (`elixir-pact` prototypes exist but
are not production-ready). This exercise demonstrates the **principles** with a
lightweight hand-rolled contract format that both sides consume. The same design
scales to integrate with the official Pact Broker once a mature library emerges.

```
payments_consumer/
├── lib/
│   └── payments_consumer/
│       └── payment_client.ex
├── test/
│   └── payments_consumer/
│       └── contract_test.exs        # generates pacts/payments-v1.json
└── mix.exs

payments_provider/
├── lib/
│   └── payments_provider_web/
│       ├── router.ex
│       └── controllers/payment_controller.ex
├── test/
│   └── payments_provider_web/
│       └── contract_verification_test.exs   # reads pacts/payments-v1.json, verifies
└── mix.exs

pacts/
└── payments-v1.json                # the source of truth, committed to git or a broker
```

## Why consumer-driven contracts over schema alone

- **OpenAPI / JSON Schema**: describes the *shape* of the provider but not the consumer's
  actual usage. The provider can break the consumer even when the schema is valid if the
  consumer depends on an optional field that the provider removed.
- **E2E tests**: slow, flaky, requires both services running.
- **Consumer contracts**: each consumer describes the requests it actually makes and the
  response shape it depends on. The provider verifies it satisfies every consumer. Fast
  (one HTTP roundtrip per contract entry), decoupled (no co-deployment), and the
  contract is a file, not a flaky integration.

## Core concepts

### 1. Interaction
One request-and-response pair: method, path, request body, expected response status and
body.

### 2. Pact
A collection of interactions for one consumer-provider pair. Usually JSON.

### 3. Consumer side
The consumer's test suite simulates the provider with the expected response (a stub),
calls the real consumer code, and records the interaction into the pact file.

### 4. Provider side
The provider's test suite reads the pact file, replays each recorded request against
the real provider endpoint, and asserts the response matches what the consumer expects.

## Design decisions

- **Option A — hand-rolled JSON + Bypass on consumer + live endpoint on provider**:
  lightweight, tool-free, good for in-house services.
- **Option B — Pact CLI + broker**: industry-standard but the Elixir ecosystem lacks a
  mature client; you have to bridge to the Ruby CLI.
- **Option C — Pactflow or Pact Broker OSS**: valuable at 10+ services but overkill for
  two.

Chosen: **Option A** for this exercise. The format can later be exchanged for Pact JSON
format to publish to a broker.

## Implementation

### Dependencies (`mix.exs`)

Consumer:

```elixir
defp deps do
  [
    {:req, "~> 0.5"},
    {:jason, "~> 1.4"},
    {:bypass, "~> 2.1", only: :test}
  ]
end
```

Provider:

```elixir
defp deps do
  [
    {:phoenix, "~> 1.7"},
    {:jason, "~> 1.4"},
    {:plug_cowboy, "~> 2.7"}
  ]
end
```

### Step 1: the consumer's HTTP client

```elixir
# payments_consumer/lib/payments_consumer/payment_client.ex
defmodule PaymentsConsumer.PaymentClient do
  @moduledoc "Charges a payment against the provider's POST /v1/charges endpoint."

  @spec charge(String.t(), pos_integer()) ::
          {:ok, %{id: String.t(), status: String.t()}} | {:error, term()}
  def charge(customer_id, amount_cents) do
    body = %{customer_id: customer_id, amount_cents: amount_cents}

    case Req.post("#{base_url()}/v1/charges", json: body) do
      {:ok, %{status: 201, body: %{"id" => id, "status" => status}}} ->
        {:ok, %{id: id, status: status}}

      {:ok, %{status: 422, body: body}} ->
        {:error, {:validation, body}}

      {:ok, %{status: s}} ->
        {:error, {:http_error, s}}

      {:error, reason} ->
        {:error, {:transport_error, reason}}
    end
  end

  defp base_url, do: Application.fetch_env!(:payments_consumer, :payments_base_url)
end
```

### Step 2: consumer test — records the pact

```elixir
# payments_consumer/test/payments_consumer/contract_test.exs
defmodule PaymentsConsumer.ContractTest do
  use ExUnit.Case, async: false

  alias PaymentsConsumer.PaymentClient

  @pact_path Path.expand("../../pacts/payments-v1.json", __DIR__)

  setup do
    bypass = Bypass.open()
    Application.put_env(:payments_consumer, :payments_base_url, "http://localhost:#{bypass.port}")
    {:ok, bypass: bypass, interactions: :ets.new(:interactions, [:set, :public])}
  end

  describe "charge/2 — successful path interaction" do
    test "records the expected request/response shape", %{bypass: bypass, interactions: tbl} do
      Bypass.expect_once(bypass, "POST", "/v1/charges", fn conn ->
        {:ok, body, conn} = Plug.Conn.read_body(conn)
        request = Jason.decode!(body)

        # Validate the request shape the consumer actually sent
        assert request == %{"customer_id" => "c_42", "amount_cents" => 1_500}

        # Stubbed provider response — this is what the consumer relies on
        response_body = %{"id" => "ch_1", "status" => "succeeded"}

        :ets.insert(tbl, {:success, request, 201, response_body})

        Plug.Conn.resp(
          conn,
          201,
          Jason.encode!(response_body)
        )
      end)

      assert {:ok, %{id: "ch_1", status: "succeeded"}} = PaymentClient.charge("c_42", 1_500)

      write_pact(tbl)
    end
  end

  describe "charge/2 — validation error interaction" do
    test "records 422 response shape", %{bypass: bypass, interactions: tbl} do
      Bypass.expect_once(bypass, "POST", "/v1/charges", fn conn ->
        {:ok, body, conn} = Plug.Conn.read_body(conn)
        request = Jason.decode!(body)

        response_body = %{"error" => "amount_cents must be positive"}
        :ets.insert(tbl, {:validation_error, request, 422, response_body})

        Plug.Conn.resp(conn, 422, Jason.encode!(response_body))
      end)

      assert {:error, {:validation, _}} = PaymentClient.charge("c_42", 0)

      write_pact(tbl)
    end
  end

  # ---------------------------------------------------------------------------
  # Writes all collected interactions to the shared pact file.
  # For this minimal example, the last writer wins per key — in a real system
  # you would merge across test files.
  # ---------------------------------------------------------------------------
  defp write_pact(tbl) do
    interactions =
      tbl
      |> :ets.tab2list()
      |> Enum.map(fn {name, req, status, resp} ->
        %{
          "description" => Atom.to_string(name),
          "request" => %{
            "method" => "POST",
            "path" => "/v1/charges",
            "body" => req
          },
          "response" => %{
            "status" => status,
            "body" => resp
          }
        }
      end)

    File.mkdir_p!(Path.dirname(@pact_path))
    File.write!(@pact_path, Jason.encode!(%{
      "consumer" => "payments_consumer",
      "provider" => "payments_provider",
      "version" => "1",
      "interactions" => interactions
    }, pretty: true))
  end
end
```

### Step 3: provider — controller and router

```elixir
# payments_provider/lib/payments_provider_web/controllers/payment_controller.ex
defmodule PaymentsProviderWeb.PaymentController do
  use PaymentsProviderWeb, :controller

  def create(conn, %{"customer_id" => cid, "amount_cents" => amount})
      when is_binary(cid) and is_integer(amount) and amount > 0 do
    # In reality this would insert into a DB and call a gateway.
    conn
    |> put_status(:created)
    |> json(%{id: "ch_#{:rand.uniform(9999)}", status: "succeeded"})
  end

  def create(conn, _bad) do
    conn
    |> put_status(:unprocessable_entity)
    |> json(%{error: "amount_cents must be positive"})
  end
end
```

### Step 4: provider verification test — replays pacts

```elixir
# payments_provider/test/payments_provider_web/contract_verification_test.exs
defmodule PaymentsProviderWeb.ContractVerificationTest do
  use PaymentsProviderWeb.ConnCase, async: true

  @pact_path Path.expand("../../../pacts/payments-v1.json", __DIR__)

  describe "contract verification against payments-v1.json" do
    test "every consumer interaction is satisfied by the current provider", %{conn: conn} do
      pact = @pact_path |> File.read!() |> Jason.decode!()

      for interaction <- pact["interactions"] do
        verify_interaction(conn, interaction)
      end
    end
  end

  defp verify_interaction(conn, %{
         "description" => desc,
         "request" => %{"method" => "POST", "path" => path, "body" => req_body},
         "response" => %{"status" => expected_status, "body" => expected_body}
       }) do
    conn =
      conn
      |> Plug.Conn.put_req_header("content-type", "application/json")
      |> post(path, req_body)

    # Status must match exactly
    assert conn.status == expected_status,
           "#{desc}: expected status #{expected_status}, got #{conn.status}"

    # Body keys must be a superset of what the consumer expected.
    # This is structural matching: the consumer declared the keys it reads;
    # the provider may return extra keys without breaking the contract.
    actual_body = json_response(conn, expected_status)

    for {key, expected_value} <- expected_body do
      assert Map.has_key?(actual_body, key),
             "#{desc}: provider response missing key '#{key}'"

      # For dynamic values like generated ids, expected value may be a "matcher"
      # in a production implementation. Here we check type only when the expected
      # value looks dynamic.
      assert value_matches?(expected_value, actual_body[key]),
             "#{desc}: value for '#{key}' does not satisfy contract " <>
               "(expected #{inspect(expected_value)}, got #{inspect(actual_body[key])})"
    end
  end

  # The consumer's "ch_1" is an opaque token — the provider can return a different id
  # as long as it has the same type. Real Pact uses matcher rules (e.g., "like"); we
  # fallback to type-level matching for strings.
  defp value_matches?(expected, actual) when is_binary(expected) and is_binary(actual), do: true
  defp value_matches?(expected, actual), do: expected == actual
end
```

## Why this works

- The **pact file** is the shared artifact. The consumer can refactor freely as long
  as what it declares in the pact still reflects its usage.
- The provider verification test reads the pact on every CI run. If the provider
  removes the `status` field, the test fails with a precise error message pointing at
  the interaction description.
- Structural matching (superset-of-keys) lets the provider evolve by **adding** fields
  without breaking the consumer. Removing or renaming is a breaking change, correctly.
- Bypass on the consumer side records the exact request the consumer sends — the
  contract is generated, not hand-written, so it cannot drift from the real call.

## Tests

See Step 2 (consumer) and Step 4 (provider).

## Benchmark

Each interaction verification is one in-process ConnTest call (~500µs). A pact with
100 interactions verifies in well under 1 second.

```elixir
{t, _} = :timer.tc(fn ->
  ExUnit.run()
end)
IO.puts("verification #{t / 1000}ms")
```

Target: 100 interactions verified in < 1 s.

## Trade-offs and production gotchas

**1. Consumer asserting on dynamic values (generated ids, timestamps)**
The consumer should declare "a string" or "an ISO-8601 datetime", not `"ch_1"`. Real
Pact has matchers; in our minimal version, implement `value_matches?/2` with type-
level matching for known-dynamic fields.

**2. Provider adding required fields to the request schema**
The consumer's pact does not include the new field. The provider will reject the
recorded request with 400. The pact verification FAILS — which is the desired
behaviour: the consumer must update before the provider ships.

**3. Merging pacts across many consumer tests**
In this exercise the ETS table is per-test; `write_pact/1` overwrites. In production
you want a consolidated pact file per consumer-provider pair. Accumulate interactions
in an `ExUnit.Callbacks.on_exit/1` that merges.

**4. Sharing the pact between repos**
For two separate repos, commit the pact to a shared broker (Pactflow, pact-broker OSS)
or a shared git submodule. Never copy-paste.

**5. Too many interactions**
Every consumer call is an interaction. Pact files with 500 interactions become hard to
review. Consider grouping by endpoint and rejecting PRs that add interactions without
business justification.

**6. When NOT to use this**
For public APIs with thousands of consumers, you cannot track each. Use versioned
OpenAPI + deprecation timelines. Contract testing shines for in-house, smaller-scale
service meshes.

## Reflection

The pact is an artifact that evolves with the consumer but is verified by the
provider. What failure modes appear when the pact file is stale in git (the consumer
evolved but forgot to regenerate), and what process (CI gating, pre-commit hook,
provider verification of a consumer-signed timestamp) best defends against them?

## Resources

- [Pact — foundational docs](https://docs.pact.io/)
- [Pact Broker OSS](https://github.com/pact-foundation/pact_broker)
- [Consumer-driven contracts — Martin Fowler](https://martinfowler.com/articles/consumerDrivenContracts.html)
- [Req](https://hexdocs.pm/req/Req.html) · [Bypass](https://github.com/PSPDFKit-labs/bypass)
