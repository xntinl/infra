# HTTP Client Testing with Bypass

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

`api_gateway` calls the payment provider's REST API to confirm charges. The client
module parses JSON responses, handles error status codes, and retries on transient
failures. Testing this without hitting the live API requires a real HTTP server that
can be programmed per-test. Bypass starts a TCP server in the test process, receives
real HTTP requests from the client, and responds with whatever the test configures.

Project structure at this point:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── payments/
│           └── provider_client.ex    # ← you implement this
├── test/
│   └── api_gateway/
│       └── payments/
│           └── provider_client_test.exs  # given tests
└── mix.exs
```

---

## The business problem

`ProviderClient` wraps the payment provider's HTTP API:

1. `confirm/2` — POST to `/charges/:id/confirm` — returns `{:ok, charge}` or error
2. `fetch/1`   — GET `/charges/:id` — returns `{:ok, charge}` or `{:error, :not_found}`

In production these go to `https://api.payments.example.com`. In tests, they must go to
`http://localhost:BYPASS_PORT` so Bypass intercepts them.

The tests verify:
- Correct JSON parsing from 200 responses
- Correct error mapping from 4xx / 5xx status codes
- That the client includes the `Authorization` header
- That the retry logic fires on 500 and stops at the configured maximum

---

## Bypass vs Mox for HTTP clients

| Question | Use Bypass | Use Mox |
|----------|-----------|--------|
| Does the test care about JSON parsing? | Yes → Bypass | No → Mox |
| Does the test care about headers sent? | Yes → Bypass | No → Mox |
| Does the test simulate TCP-level errors? | Yes → Bypass | No → Mox |
| Is HTTP an implementation detail? | No → Mox | Yes → Mox |
| Is network I/O acceptable in the test? | Yes → Bypass | No → Mox |

---

## Implementation

### Step 1: `mix.exs`

```elixir
defp deps do
  [
    {:req, "~> 0.4"},
    {:bypass, "~> 2.1", only: :test}
  ]
end
```

### Step 2: `lib/api_gateway/payments/provider_client.ex`

```elixir
defmodule ApiGateway.Payments.ProviderClient do
  @moduledoc """
  HTTP client for the payment provider REST API.

  The base URL is configurable to allow Bypass to intercept calls in tests:
    ProviderClient.confirm("ch_001", base_url: "http://localhost:#{bypass.port}")

  Retry policy:
    - 500 → retry up to max_retries times with exponential backoff
    - 429 → retry after the Retry-After header value
    - 4xx (other) → no retry (permanent client error)
    - Network error → retry up to max_retries times
  """

  @default_base_url "https://api.payments.example.com"
  @default_max_retries 3
  @base_backoff_ms 100

  @doc """
  Confirm a charge by ID.
  Returns {:ok, charge_map} | {:error, :not_found | :forbidden | :server_error | term()}
  """
  @spec confirm(String.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def confirm(charge_id, opts \\ []) do
    base_url    = Keyword.get(opts, :base_url, @default_base_url)
    token       = Keyword.get(opts, :token, nil)
    max_retries = Keyword.get(opts, :max_retries, @default_max_retries)

    url = "#{base_url}/charges/#{charge_id}/confirm"
    do_post(url, %{}, build_headers(token), 0, max_retries)
  end

  @doc """
  Fetch a charge by ID.
  Returns {:ok, charge_map} | {:error, :not_found | :forbidden | :server_error | term()}
  """
  @spec fetch(String.t(), keyword()) :: {:ok, map()} | {:error, term()}
  def fetch(charge_id, opts \\ []) do
    base_url    = Keyword.get(opts, :base_url, @default_base_url)
    token       = Keyword.get(opts, :token, nil)
    max_retries = Keyword.get(opts, :max_retries, @default_max_retries)

    url = "#{base_url}/charges/#{charge_id}"
    do_get(url, build_headers(token), 0, max_retries)
  end

  # ---------------------------------------------------------------------------
  # Private: HTTP execution with retry logic
  # ---------------------------------------------------------------------------

  defp do_get(url, headers, attempt, max_retries) do
    case Req.get(url, headers: headers) do
      {:ok, %{status: 200, body: body}} ->
        # TODO: parse the body using parse_charge/1 and return {:ok, charge}
        {:error, :not_implemented}

      {:ok, %{status: 404}} ->
        {:error, :not_found}

      {:ok, %{status: 403}} ->
        {:error, :forbidden}

      {:ok, %{status: 429, headers: resp_headers}} ->
        # TODO: extract Retry-After, retry if attempt < max_retries
        retry_after = get_retry_after(resp_headers)
        if attempt < max_retries do
          Process.sleep(retry_after)
          do_get(url, headers, attempt + 1, max_retries)
        else
          {:error, :rate_limited}
        end

      {:ok, %{status: 500}} ->
        # TODO: exponential backoff and retry if attempt < max_retries
        # backoff_ms = trunc(@base_backoff_ms * :math.pow(2, attempt))
        # return {:error, :server_error} when retries are exhausted
        {:error, :not_implemented}

      {:error, reason} ->
        {:error, {:network_error, reason}}
    end
  end

  defp do_post(url, body, headers, attempt, max_retries) do
    case Req.post(url, json: body, headers: headers) do
      {:ok, %{status: status, body: resp_body}} when status in [200, 201] ->
        # TODO: parse resp_body and return {:ok, charge}
        {:error, :not_implemented}

      {:ok, %{status: 404}} ->
        {:error, :not_found}

      {:ok, %{status: 422, body: resp_body}} ->
        {:error, {:validation_error, resp_body}}

      {:ok, %{status: 500}} ->
        # TODO: retry with backoff
        {:error, :not_implemented}

      {:error, reason} ->
        {:error, {:network_error, reason}}
    end
  end

  # ---------------------------------------------------------------------------
  # Private helpers
  # ---------------------------------------------------------------------------

  defp build_headers(nil),   do: [{"accept", "application/json"}]
  defp build_headers(token), do: [
    {"accept", "application/json"},
    {"authorization", "Bearer #{token}"}
  ]

  defp parse_charge(%{"id" => id, "amount" => amount, "status" => status}) do
    %{id: id, amount: amount, status: status}
  end
  defp parse_charge(body), do: body

  defp get_retry_after(headers) do
    case List.keyfind(headers, "retry-after", 0) do
      {_, value} -> String.to_integer(to_string(value))
      nil        -> 1_000
    end
  end
end
```

### Step 3: Given tests — must pass without modification

```elixir
# test/api_gateway/payments/provider_client_test.exs
defmodule ApiGateway.Payments.ProviderClientTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Payments.ProviderClient

  setup do
    bypass = Bypass.open()
    {:ok, bypass: bypass}
  end

  describe "fetch/2" do
    test "parses 200 response into a charge map", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/charges/ch_001", fn conn ->
        body = Jason.encode!(%{
          "id"     => "ch_001",
          "amount" => 9900,
          "status" => "succeeded"
        })
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(200, body)
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:ok, charge} = ProviderClient.fetch("ch_001", base_url: url)
      assert charge.id     == "ch_001"
      assert charge.amount == 9900
      assert charge.status == "succeeded"
    end

    test "returns {:error, :not_found} on 404", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/charges/missing", fn conn ->
        Plug.Conn.resp(conn, 404, ~s({"error":"not found"}))
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:error, :not_found} =
        ProviderClient.fetch("missing", base_url: url, max_retries: 0)
    end

    test "includes Authorization header when token is provided", %{bypass: bypass} do
      Bypass.expect_once(bypass, "GET", "/charges/ch_auth", fn conn ->
        assert ["Bearer test_token_42"] =
          Plug.Conn.get_req_header(conn, "authorization")

        body = Jason.encode!(%{"id" => "ch_auth", "amount" => 0, "status" => "pending"})
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(200, body)
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:ok, _} =
        ProviderClient.fetch("ch_auth", base_url: url, token: "test_token_42")
    end

    test "retries on 500 and returns :server_error when retries are exhausted",
         %{bypass: bypass} do
      counter = :counters.new(1, [])

      Bypass.expect(bypass, "GET", "/charges/flaky", fn conn ->
        :counters.add(counter, 1, 1)
        Plug.Conn.resp(conn, 500, "Internal Server Error")
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:error, :server_error} =
        ProviderClient.fetch("flaky", base_url: url, max_retries: 2)

      # 1 initial attempt + 2 retries = 3 total
      assert :counters.get(counter, 1) == 3
    end

    test "does NOT retry on 404 (permanent error)", %{bypass: bypass} do
      counter = :counters.new(1, [])

      Bypass.expect_once(bypass, "GET", "/charges/gone", fn conn ->
        :counters.add(counter, 1, 1)
        Plug.Conn.resp(conn, 404, "not found")
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:error, :not_found} =
        ProviderClient.fetch("gone", base_url: url, max_retries: 3)

      assert :counters.get(counter, 1) == 1
    end
  end

  describe "confirm/2" do
    test "parses 200/201 response into a charge map", %{bypass: bypass} do
      Bypass.expect_once(bypass, "POST", "/charges/ch_002/confirm", fn conn ->
        body = Jason.encode!(%{
          "id"     => "ch_002",
          "amount" => 5000,
          "status" => "succeeded"
        })
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(201, body)
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:ok, charge} = ProviderClient.confirm("ch_002", base_url: url)
      assert charge.status == "succeeded"
    end

    test "returns {:error, {:validation_error, _}} on 422", %{bypass: bypass} do
      Bypass.expect_once(bypass, "POST", "/charges/ch_bad/confirm", fn conn ->
        body = Jason.encode!(%{"error" => "charge already confirmed"})
        conn
        |> Plug.Conn.put_resp_content_type("application/json")
        |> Plug.Conn.resp(422, body)
      end)

      url = "http://localhost:#{bypass.port}"
      assert {:error, {:validation_error, _}} =
        ProviderClient.confirm("ch_bad", base_url: url)
    end

    test "connection refused returns network error", %{bypass: bypass} do
      Bypass.down(bypass)

      url = "http://localhost:#{bypass.port}"
      result = ProviderClient.confirm("ch_003", base_url: url, max_retries: 0)
      assert {:error, _} = result
    end
  end
end
```

### Step 4: Run the tests

```bash
mix test test/api_gateway/payments/provider_client_test.exs --trace
```

---

## Trade-off analysis

| | Bypass | Mox |
|--|--------|-----|
| Tests real HTTP serialization | Yes | No |
| Tests header/path/body format | Yes | No |
| Tests TCP-level errors (timeout, refused) | Yes | No |
| Test speed | Slower (TCP I/O) | Faster (no I/O) |
| Infrastructure required | None (localhost) | None |
| Isolation (async: true) | Yes (unique port per test) | Yes |
| Best for | HTTP client modules | Business logic that calls services |

| Bypass function | Use when |
|----------------|---------|
| `expect_once/4` | Exactly one call expected — fails if 0 or 2+ |
| `expect/4` | Multiple calls expected (e.g., retry logic) |
| `stub/4` | Variable number of calls — no count verification |
| `down/1` | Simulate server crash / connection refused |

---

## Common production mistakes

**1. Not making the base URL configurable**
If `ProviderClient` hardcodes `"https://api.payments.example.com"`, Bypass can never
intercept the calls. Every HTTP client that needs to be tested must accept a
`base_url` option (or be configured via `Application.get_env`).

**2. Using `expect_once` when the client retries**
Retry logic calls the same endpoint multiple times. `expect_once` fails on the second
call with "unexpectedly received request". Use `expect` (without `_once`) when retries
are part of the behavior under test.

**3. Mutable state in Bypass handlers without atomics**
Bypass handlers run in the Bypass server process, which may be concurrent. Variables
in the test process are immutable — a simple `count = count + 1` in the handler
captures the variable at closure time and is never updated. Use `:counters` (atomic)
or `Agent` to maintain mutable state across handler invocations.

**4. Not checking the path exactly**
Bypass matches paths by exact string equality. If the client calls `/charges/001/`
(trailing slash) and the test registers `/charges/001`, Bypass returns a 404 from its
default handler, and the client test fails with a confusing error. Verify the exact
path format the client produces.

**5. Sharing a Bypass instance across tests**
Each test in `setup do bypass = Bypass.open() end` gets its own server on a unique
random port. If you reuse a Bypass instance across tests (e.g., in `setup_all`), two
concurrent tests may conflict on endpoint expectations. Always open Bypass in `setup`,
not `setup_all`.

---

## Resources

- [Bypass — HexDocs](https://hexdocs.pm/bypass/Bypass.html)
- [Bypass — GitHub](https://github.com/PSPDFKit-labs/bypass)
- [Req — HexDocs](https://hexdocs.pm/req/Req.html)
- [Plug.Conn — HexDocs](https://hexdocs.pm/plug/Plug.Conn.html)
