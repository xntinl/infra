# Service Mesh Sidecar Proxy

**Project**: `meshex` — a sidecar proxy implementing mTLS, circuit breaking, retries, and distributed tracing

---

## Project context

You are building `meshex`, a sidecar proxy the platform team will deploy alongside every service in their microservices cluster. The proxy intercepts all inbound and outbound traffic, enforces mTLS identity verification, applies circuit breaking and retry policies, shapes traffic for canary deployments, and injects distributed trace context. Services communicate only through the proxy — they have no awareness of the mesh.

Project structure:

```
meshex/
├── lib/
│   └── meshex/
│       ├── application.ex
│       ├── proxy/
│       │   ├── inbound.ex          # ← accepts mTLS connections from other sidecars
│       │   └── outbound.ex         # ← intercepts outbound calls, applies policy
│       ├── registry.ex             # ← service discovery: name → [{host, port, weight, healthy}]
│       ├── mtls/
│       │   ├── cert_generator.ex   # ← generates X.509 certs with SPIFFE SANs
│       │   └── verifier.ex         # ← peer cert validation + identity extraction
│       ├── circuit_breaker.ex      # ← per (source, destination) pair state machine
│       ├── retry.ex                # ← exponential backoff + retry budget
│       ├── traffic_shaper.ex       # ← weighted random routing for canary deployments
│       ├── tracing.ex              # ← W3C TraceContext header propagation
│       └── stats.ex                # ← per-route metrics aggregation
├── test/
│   └── meshex/
│       ├── mtls_test.exs
│       ├── circuit_breaker_test.exs
│       ├── retry_test.exs
│       ├── traffic_shaper_test.exs
│       └── tracing_test.exs
├── bench/
│   └── proxy_overhead_bench.exs
└── mix.exs
```

---

## The business problem

The security team requires mutual TLS for all service-to-service communication. The SRE team needs circuit breaking to prevent cascading failures. The product team wants canary deployments — 10% of traffic to v2 before full rollout. The observability team wants distributed traces with no code changes to services.

A sidecar proxy satisfies all four requirements without touching service code. The proxy is the policy enforcement point; services remain simple HTTP servers.

Two design constraints shape the entire implementation:

1. **Transparent interception** — services connect to `localhost:outbound_port`; they never know the real destination or that mTLS is in use.
2. **Ephemeral certificates** — certificates are generated at startup and expire. There are no long-lived secrets on disk.

---

## Why per-(source, destination) circuit breakers

A single circuit breaker per destination means that one slow endpoint breaking affects all callers, even services with healthy interaction histories with that endpoint. A circuit breaker per `(source, destination)` pair isolates failure domains: if `service-A → service-B` has a high error rate but `service-C → service-B` is healthy, service-C is not penalized.

The cost: O(source × destination) circuit breaker GenServers. For a mesh with 20 services, that is up to 400 circuit breakers. Each is a lightweight GenServer — 400 is trivial on the BEAM.

---

## Why retry budget prevents amplification

Naive retries amplify failures: if 100% of requests fail and each retries 3 times, the backend receives 4x the original load — making recovery harder. A retry budget limits retries to X% of total traffic. When failures are widespread and many requests are retrying, new retries are blocked (they fail fast) to give the backend room to recover.

Implement as a sliding window counter in ETS: `{retries_in_window} / {total_in_window} <= budget`.

---

## Implementation

### Step 1: Create the project

```bash
mix new meshex --sup
cd meshex
mkdir -p lib/meshex/{proxy,mtls}
mkdir -p test/meshex bench
```

### Step 2: `mix.exs`

```elixir
defp deps do
  [
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/meshex/mtls/cert_generator.ex`

```elixir
defmodule Meshex.MTLS.CertGenerator do
  @moduledoc """
  Generates ephemeral X.509 certificates with SPIFFE SVIDs (Subject Alternative Names).

  SPIFFE URI format: spiffe://trust-domain/ns/namespace/sa/service-name
  Example: spiffe://mesh.local/ns/default/sa/payment-service

  The certificate lifecycle:
  1. Generate RSA or EC key pair
  2. Build a certificate signing request (CSR) with the SPIFFE URI as a SAN extension
  3. Sign with the mesh CA (also generated at startup)
  4. Use the signed certificate for all TLS connections in this sidecar instance

  Why ephemeral certificates?
  Long-lived certificates require certificate revocation infrastructure (CRL, OCSP).
  Short-lived certificates (hours or days) expire naturally — no revocation needed.
  The security model trades revocation complexity for rotation frequency.
  """

  @doc """
  Generates a self-signed CA certificate and key for the mesh.
  Used to sign service certificates.
  """
  @spec generate_ca(String.t()) :: {:ok, {cert :: binary(), key :: binary()}}
  def generate_ca(common_name) do
    # TODO: generate EC key pair: :public_key.generate_key({:namedCurve, :secp256r1})
    # TODO: build self-signed CA certificate with:
    #   - Subject: CN=common_name
    #   - basicConstraints: CA:true
    #   - keyUsage: keyCertSign, cRLSign
    # TODO: :public_key.pkix_sign(tbs_cert, private_key)
    # Return {:ok, {DER-encoded cert binary, private key}}
    {:error, :not_implemented}
  end

  @doc """
  Generates a service certificate signed by the given CA.
  Includes the SPIFFE URI in the SubjectAlternativeName extension.
  """
  @spec generate_service_cert(String.t(), {binary(), binary()}) ::
    {:ok, {cert :: binary(), key :: binary()}} | {:error, term()}
  def generate_service_cert(spiffe_uri, {ca_cert_der, ca_key}) do
    # TODO: generate EC key pair for the service
    # TODO: build TBS certificate with:
    #   - Subject: CN=spiffe_uri
    #   - subjectAltName extension with URI type: spiffe_uri
    #   - validity: 24 hours
    # TODO: sign with CA key
    # Return {:ok, {service_cert_der, service_key}}
    {:error, :not_implemented}
  end

  @doc "Extracts the SPIFFE URI from a peer certificate's SAN extension."
  @spec extract_spiffe_id(binary()) :: {:ok, String.t()} | {:error, :no_spiffe_id}
  def extract_spiffe_id(cert_der) do
    # TODO: :public_key.pkix_decode_cert(cert_der, :otp)
    # TODO: find the SubjectAltName extension
    # TODO: find the URI entry starting with "spiffe://"
    {:error, :not_implemented}
  end
end
```

### Step 4: `lib/meshex/retry.ex`

```elixir
defmodule Meshex.Retry do
  @moduledoc """
  Exponential backoff with jitter and a retry budget.

  Retry budget: the ratio of retried requests to total requests in a sliding window.
  If this ratio exceeds the configured budget (default: 20%), no new retries are allowed.
  This prevents retry storms from amplifying failures.

  Design: the retry budget is stored in ETS as two counters:
  - :total_requests (updated on every request, retry or not)
  - :retry_requests (updated on every retry attempt)
  These are reset every window_ms via a periodic GenServer.

  Why jitter?
  Without jitter, all clients retry at the same time (at 1s, 2s, 4s, etc.).
  This creates synchronized thundering herds. Full jitter: retry_at = random(0, backoff).
  """

  @default_max_attempts 3
  @default_initial_interval_ms 1_000
  @default_max_interval_ms 60_000
  @default_budget_ratio 0.20

  @doc """
  Executes fun/0 with retry on failure.
  Returns {:ok, result} or {:error, :max_retries_exceeded | :budget_exceeded | last_error}.
  """
  @spec with_retry((() -> {:ok, any()} | {:error, any()}), keyword()) ::
    {:ok, any()} | {:error, any()}
  def with_retry(fun, opts \\ []) do
    max_attempts = Keyword.get(opts, :max_attempts, @default_max_attempts)
    initial_ms = Keyword.get(opts, :initial_interval_ms, @default_initial_interval_ms)
    max_ms = Keyword.get(opts, :max_interval_ms, @default_max_interval_ms)
    idempotent = Keyword.get(opts, :idempotent, true)  # POST requests are not retried

    unless idempotent do
      # Non-idempotent requests: execute once, no retry
      fun.()
    else
      do_retry(fun, max_attempts, initial_ms, max_ms, 1)
    end
  end

  defp do_retry(_fun, max_attempts, _initial, _max, attempt) when attempt > max_attempts do
    {:error, :max_retries_exceeded}
  end

  defp do_retry(fun, max_attempts, initial_ms, max_ms, attempt) do
    case fun.() do
      {:ok, result} ->
        {:ok, result}

      {:error, _reason} = error ->
        if budget_exceeded?() do
          {:error, :budget_exceeded}
        else
          record_retry()
          backoff = compute_backoff(initial_ms, max_ms, attempt)
          Process.sleep(backoff)
          do_retry(fun, max_attempts, initial_ms, max_ms, attempt + 1)
        end
    end
  end

  defp compute_backoff(initial_ms, max_ms, attempt) do
    # Exponential backoff with full jitter
    base = min(initial_ms * :math.pow(2, attempt - 1), max_ms)
    # Full jitter: random value in [0, base]
    :rand.uniform(round(base))
  end

  defp budget_exceeded? do
    # TODO: read :total_requests and :retry_requests from ETS
    # TODO: return true if retry_requests / total_requests > @default_budget_ratio
    false
  end

  defp record_retry do
    # TODO: :ets.update_counter(:retry_budget, :retry_requests, 1)
    :ok
  end
end
```

### Step 5: `lib/meshex/tracing.ex`

```elixir
defmodule Meshex.Tracing do
  @moduledoc """
  W3C TraceContext propagation.

  Format: traceparent: 00-{trace_id}-{parent_span_id}-{flags}

  Where:
  - trace_id: 16 random bytes, hex-encoded (32 chars)
  - parent_span_id: 8 random bytes, hex-encoded (16 chars)
  - flags: 8-bit flags field, hex-encoded (01 = sampled)

  The proxy:
  1. On receiving a request: extract traceparent (or create a new trace)
  2. Generate a new span_id for the proxy's own span
  3. Forward the request with: traceparent: 00-{trace_id}-{new_span_id}-{flags}
  4. Record the span: {trace_id, proxy_span_id, parent_span_id, start_ms, duration_ms, status}

  This creates a trace tree where each service leg is a child span of the proxy span.
  """

  defmodule Span do
    defstruct [:trace_id, :span_id, :parent_span_id, :start_ms, :end_ms, :status, :service]
  end

  @doc "Extracts or creates trace context from request headers."
  @spec extract_or_create(map()) :: {trace_id :: String.t(), parent_span_id :: String.t()}
  def extract_or_create(headers) do
    case Map.get(headers, "traceparent") do
      nil ->
        # New trace: generate fresh trace_id and span_id
        trace_id = random_hex(16)
        span_id = random_hex(8)
        {trace_id, span_id}

      traceparent ->
        parse_traceparent(traceparent)
    end
  end

  @doc "Builds a traceparent header value for the outgoing request."
  @spec build_traceparent(String.t(), String.t()) :: String.t()
  def build_traceparent(trace_id, span_id) do
    # TODO: "00-#{trace_id}-#{span_id}-01"
    ""
  end

  @doc "Parses a traceparent header value."
  @spec parse_traceparent(String.t()) :: {String.t(), String.t()}
  def parse_traceparent(traceparent) do
    case String.split(traceparent, "-") do
      ["00", trace_id, parent_span_id, _flags] ->
        {trace_id, parent_span_id}

      _ ->
        # Malformed header: start a new trace
        {random_hex(16), random_hex(8)}
    end
  end

  defp random_hex(bytes) do
    :crypto.strong_rand_bytes(bytes) |> Base.encode16(case: :lower)
  end
end
```

### Step 6: Given tests — must pass without modification

```elixir
# test/meshex/tracing_test.exs
defmodule Meshex.TracingTest do
  use ExUnit.Case, async: true

  alias Meshex.Tracing

  test "creates new trace when no traceparent header" do
    {trace_id, span_id} = Tracing.extract_or_create(%{})
    assert byte_size(trace_id) == 32  # 16 bytes hex = 32 chars
    assert byte_size(span_id) == 16   # 8 bytes hex = 16 chars
  end

  test "extracts existing trace from traceparent header" do
    existing = "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"
    {trace_id, parent_span_id} = Tracing.extract_or_create(%{"traceparent" => existing})
    assert trace_id == "4bf92f3577b34da6a3ce929d0e0e4736"
    assert parent_span_id == "00f067aa0ba902b7"
  end

  test "build_traceparent produces correct format" do
    tp = Tracing.build_traceparent("abc123def456abc123def456abc123de", "0102030405060708")
    assert String.starts_with?(tp, "00-abc123def456abc123def456abc123de-0102030405060708-")
  end

  test "malformed traceparent starts new trace" do
    {trace_id, _} = Tracing.extract_or_create(%{"traceparent" => "invalid"})
    assert byte_size(trace_id) == 32
  end
end
```

```elixir
# test/meshex/circuit_breaker_test.exs
defmodule Meshex.CircuitBreakerTest do
  use ExUnit.Case, async: false

  alias Meshex.CircuitBreaker

  setup do
    {:ok, _} = start_supervised({CircuitBreaker, {"svc-a", "svc-b"}})
    :ok
  end

  test "starts closed" do
    assert CircuitBreaker.check({"svc-a", "svc-b"}) == :allow
  end

  test "opens after error threshold" do
    for _ <- 1..10, do: CircuitBreaker.record_outcome({"svc-a", "svc-b"}, :error)
    Process.sleep(10)
    assert {:deny, :circuit_open} = CircuitBreaker.check({"svc-a", "svc-b"})
  end

  test "different service pairs are independent" do
    {:ok, _} = start_supervised({CircuitBreaker, {"svc-a", "svc-c"}})
    for _ <- 1..10, do: CircuitBreaker.record_outcome({"svc-a", "svc-b"}, :error)
    Process.sleep(10)

    # svc-a → svc-c should still be closed
    assert CircuitBreaker.check({"svc-a", "svc-c"}) == :allow
  end
end
```

### Step 7: Run the tests

```bash
mix test test/meshex/ --trace
```

---

## Trade-off analysis

| Aspect | Sidecar proxy (your impl) | Library-based (e.g., Plug middleware) | Network policy |
|--------|--------------------------|--------------------------------------|----------------|
| Language/runtime agnostic | yes | no (per-language library) | yes |
| Code changes required | none | add library + configure | none |
| mTLS enforcement | transparent | optional per-service | transparent |
| Operational complexity | high (deploy sidecar) | low | medium |
| Debug visibility | excellent (proxy sees all) | depends on logging | limited |
| Latency overhead | ~1ms per hop | <0.1ms | ~0.1ms |

Reflection: a sidecar proxy adds ~1ms of latency per service call. In a microservices architecture where a single user request fans out to 10 service calls in series, total added latency is 10ms. At what call depth does this become unacceptable, and how would you optimize the proxy to minimize latency without removing functionality?

---

## Common production mistakes

**1. Certificate generated with wrong key usage**
A certificate used for TLS authentication must have `keyUsage: digitalSignature` and `extendedKeyUsage: clientAuth, serverAuth`. Without these, the TLS handshake will be rejected by strict TLS implementations even if the certificate is otherwise valid.

**2. Retry budget computed globally, not per service pair**
A global retry budget means a flood of failures from one service pair can exhaust retries for all pairs. Budget tracking should be per `(source, destination)` pair, matching the circuit breaker granularity.

**3. Retry on connection timeout treated as non-idempotent**
Connection timeouts (the server never received the request) are always safe to retry, even for POST requests. Only retry-unsafe are requests where the server received and partially processed the request before the connection failed. Distinguish these two cases in your retry logic.

**4. TraceContext not propagated to async operations**
If a service spawns a background task during request processing, the task's outbound calls won't carry the trace context unless it's explicitly passed. The sidecar can only propagate what it receives — ensure internal async calls also propagate `traceparent`.

**5. Weighted random selection allocating new list on every request**
`Enum.flat_map(routes, fn {ep, w} -> List.duplicate(ep, w) end)` creates a new list on every selection. For routes with `{v1: 90, v2: 10}` this creates a 100-element list per request. Use the sum-of-weights algorithm: `rand_val = :rand.uniform(total_weight)` and iterate through weights cumulatively.

---

## Resources

- [Envoy Proxy Architecture](https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview/intro/arch_overview) — the reference sidecar proxy; xDS API, circuit breaking, and retry policy documentation
- [Istio Data Plane Architecture](https://istio.io/latest/docs/ops/deployment/architecture/) — how sidecar injection and mTLS work in production
- [SPIFFE Specification](https://spiffe.io/docs/latest/spiffe-about/spiffe-concept/) — the SPIFFE SVID (Secure Production Identity Framework for Everyone) standard your certificate SANs must follow
- [W3C Trace Context Specification](https://www.w3.org/TR/trace-context/) — the `traceparent` and `tracestate` header specifications
- [RFC 8446 — TLS 1.3](https://www.rfc-editor.org/rfc/rfc8446) — sections 4.4 (certificates) and 4.2.8 (key share) for understanding the mTLS handshake
