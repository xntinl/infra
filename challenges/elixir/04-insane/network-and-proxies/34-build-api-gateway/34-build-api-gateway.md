# API Gateway

**Project**: `apigw` — a production API gateway with discovery, auth, rate limiting, circuit breaking, and observability

---

## Project context

You are building `apigw`, the single ingress point for a microservices platform. Client traffic arrives at the gateway, gets authenticated, rate-limited, routed to the correct backend, protected by a circuit breaker, possibly cached, and observed. No request reaches a backend without passing through all of these layers.

Project structure:

```
apigw/
├── lib/
│   └── apigw/
│       ├── application.ex
│       ├── registry/
│       │   ├── server.ex            # ← backend registration + health polling
│       │   └── store.ex             # ← ETS-backed backend list
│       ├── lb/
│       │   ├── round_robin.ex       # ← stateless algorithm
│       │   ├── least_connections.ex # ← :atomics connection counter per backend
│       │   ├── weighted.ex          # ← weighted round-robin with current weights
│       │   └── ip_hash.ex           # ← consistent hash for sticky sessions
│       ├── auth/
│       │   ├── jwt.ex               # ← JWT verify (HS256 + RS256)
│       │   └── api_key.ex           # ← key registry + validation
│       ├── rate_limiter.ex          # ← sliding window, lease-based, ETS
│       ├── circuit_breaker.ex       # ← per-backend state machine
│       ├── cache.ex                 # ← GET response cache with ETag
│       ├── transformer.ex           # ← request/response mutation rules
│       ├── proxy.ex                 # ← HTTP reverse proxy (Mint connection pool)
│       └── telemetry.ex             # ← per-request metrics + trace spans
├── test/
│   └── apigw/
│       ├── registry_test.exs
│       ├── circuit_breaker_test.exs
│       ├── rate_limiter_test.exs
│       ├── auth_test.exs
│       └── cache_test.exs
├── bench/
│   └── proxy_bench.exs
└── mix.exs
```

---

## Why circuit breakers per upstream and not per-request timeouts only

timeouts alone let a degraded upstream exhaust gateway resources indefinitely. A breaker trips on error rate and fails fast, giving the upstream time to recover instead of piling on retries.

## Design decisions

**Option A — per-request process spawn for upstream calls**
- Pros: isolated failures, trivial cancellation
- Cons: process-creation overhead dominates under high RPS

**Option B — pooled upstream connections + async Task supervision** (chosen)
- Pros: connection reuse, bounded resource usage
- Cons: more complex failure-isolation model

→ Chose **B** because a gateway fronts millions of requests/day; connection reuse is non-negotiable for upstream health.

## The business problem

A misconfigured client is retrying a failing request 1000 times per second against the payments service. Other clients are degraded. The platform team needs a gateway that isolates this impact without touching any backend: rate limit the offending API key, open the circuit breaker when the payments service error rate spikes, and cache the responses that were healthy before the spike.

These are not independent features — they must compose:

```
request → [auth] → [rate limiter] → [circuit breaker check] → [cache check]
                                                               → [proxy] → [cache write] → response
```

If any layer short-circuits (auth failure, rate limited, circuit open, cache hit), the subsequent layers never execute.

---

## Why lease-based rate limiting scales across nodes

A centralized rate limiter serializes all requests through one process. Under 50k req/s this becomes a bottleneck. A lease-based approach distributes the limit:

- Each gateway node acquires a lease of `quota / N` tokens per window from a central coordinator (or directly from Redis/ETS if single-node).
- Locally, the node enforces the lease with an `:atomics` counter.
- No cross-node coordination per request — only per lease renewal (every few seconds).

The trade-off: a node that crashes forfeits its unused lease tokens for that window. The limit may be slightly over-served during the inter-lease period. This is acceptable for most rate limiting use cases.

---

## Why the circuit breaker needs a half-open state

A two-state breaker (open/closed) that tests the backend by re-opening it immediately on timeout expiry would cause request storms: N gateway instances simultaneously probe a recovering backend with their first request, potentially overwhelming it again.

The half-open state allows exactly one probe request through. Success closes the breaker for all instances. Failure keeps it open and resets the timeout. This "single probe" invariant is the entire purpose of the half-open state.

---

## Implementation

### Step 1: Create the project

**Objective**: Pre-create `registry/`, `lb/`, `auth/` — each folder is an isolated middleware stage the pipeline composes in order.


```bash
mix new apigw --sup
cd apigw
mkdir -p lib/apigw/{registry,lb,auth,pagination}
mkdir -p test/apigw bench
```

### Step 2: `mix.exs`

**Objective**: Pick `:mint` over `:httpoison` — the gateway needs explicit connection pools and streamed bodies, not a blocking facade.


```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:mint, "~> 1.5"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Dependencies (mix.exs)

```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:mint, "~> 1.5"},
    {:jason, "~> 1.4"},
    {:benchee, "~> 1.3", only: :dev}
  ]
end
```

### Step 3: `lib/apigw/circuit_breaker.ex`

**Objective**: Enforce the half-open single-probe invariant — two probes in parallel defeat the whole state machine.


The circuit breaker is a per-backend GenServer implementing a three-state machine: closed (normal), open (rejecting), and half-open (probing). It tracks request outcomes in a sliding time window and transitions to open when the error rate exceeds the configured threshold. After a cooldown period, it transitions to half-open and allows exactly one probe request through. A successful probe closes the breaker; a failed probe reopens it.

The key design decision is storing events as a list of `{timestamp, outcome}` tuples, pruning entries older than the window on every recording. This keeps memory bounded and provides an accurate rolling error rate without fixed time buckets (which can miss spikes that straddle bucket boundaries).

```elixir
defmodule Apigw.CircuitBreaker do
  use GenServer

  @open_duration_ms 30_000
  @error_threshold 0.5   # 50% error rate triggers open
  @window_ms 60_000

  # States: :closed | :open | :half_open
  # Transitions:
  #   :closed → :open when error_rate > threshold in window
  #   :open → :half_open after @open_duration_ms
  #   :half_open → :closed on successful probe
  #   :half_open → :open on failed probe

  defstruct [
    :backend_id,
    state: :closed,
    events: [],
    opened_at: nil,
    half_open_probe_sent: false
  ]

  # ---------------------------------------------------------------------------
  # Public API
  # ---------------------------------------------------------------------------

  @doc """
  Returns :allow or {:deny, :circuit_open} for a request to backend_id.
  In half_open state, only one concurrent probe is allowed.
  """
  @spec check(String.t()) :: :allow | {:deny, :circuit_open}
  def check(backend_id) do
    GenServer.call(via(backend_id), :check)
  end

  @doc """
  Records the outcome of a request to backend_id.
  Must be called for every request that passed check/1.
  """
  @spec record_outcome(String.t(), :success | :error) :: :ok
  def record_outcome(backend_id, outcome) do
    GenServer.cast(via(backend_id), {:record, outcome, System.monotonic_time(:millisecond)})
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(backend_id) do
    GenServer.start_link(__MODULE__, backend_id, name: via(backend_id))
  end

  @impl true
  def init(backend_id) do
    {:ok, %__MODULE__{backend_id: backend_id}}
  end

  @impl true
  def handle_call(:check, _from, %{state: :closed} = state) do
    {:reply, :allow, state}
  end

  @impl true
  def handle_call(:check, _from, %{state: :open, opened_at: opened_at} = state) do
    now = System.monotonic_time(:millisecond)
    if now - opened_at >= @open_duration_ms do
      # Transition to half_open, allow one probe
      new_state = %{state | state: :half_open, half_open_probe_sent: true}
      {:reply, :allow, new_state}
    else
      {:reply, {:deny, :circuit_open}, state}
    end
  end

  @impl true
  def handle_call(:check, _from, %{state: :half_open, half_open_probe_sent: true} = state) do
    # Already sent one probe, deny until result comes back
    {:reply, {:deny, :circuit_open}, state}
  end

  @impl true
  def handle_cast({:record, outcome, ts}, state) do
    # Append the new event and prune entries older than the window.
    # Pruning on every record keeps the list bounded to events within @window_ms.
    cutoff = ts - @window_ms
    updated_events =
      [{ts, outcome} | state.events]
      |> Enum.filter(fn {event_ts, _} -> event_ts >= cutoff end)

    new_state = %{state | events: updated_events}

    case new_state.state do
      :closed ->
        # Calculate error rate and potentially transition to :open
        total = length(updated_events)
        error_count = Enum.count(updated_events, fn {_, o} -> o == :error end)

        # Require at least a few events before evaluating the threshold
        # to avoid flipping on a single error
        if total >= 5 and error_count / total > @error_threshold do
          {:noreply, %{new_state | state: :open, opened_at: ts, events: []}}
        else
          {:noreply, new_state}
        end

      :half_open ->
        # A probe result has arrived. Success closes; failure reopens.
        case outcome do
          :success ->
            {:noreply, %{new_state |
              state: :closed,
              events: [],
              half_open_probe_sent: false,
              opened_at: nil
            }}

          :error ->
            {:noreply, %{new_state |
              state: :open,
              opened_at: ts,
              events: [],
              half_open_probe_sent: false
            }}
        end

      :open ->
        # While open, we still record but don't transition (timer-based)
        {:noreply, new_state}
    end
  end

  defp via(backend_id) do
    {:via, Registry, {Apigw.CircuitBreaker.Registry, backend_id}}
  end
end
```

### Step 4: `lib/apigw/auth/jwt.ex`

**Objective**: Verify JWTs in constant time and pin the algorithm — a signature-compare timing leak breaks the whole gateway.


JWT verification is implemented from scratch to control the algorithm surface. Only HS256 (HMAC-SHA256) and RS256 (RSA-SHA256) are supported. The `verify/2` function decodes the three base64url sections, checks the expiry claim, and verifies the cryptographic signature using Erlang's `:crypto` module.

The constant-time comparison for HS256 is critical: a timing side-channel on the signature comparison would let an attacker reconstruct the MAC byte by byte. Erlang's `:crypto.hash_equals/2` (available since OTP 25) provides this guarantee. For older OTP versions, we XOR all bytes and check the accumulator, which runs in constant time regardless of where the first difference occurs.

```elixir
defmodule Apigw.Auth.JWT do
  @moduledoc """
  Verifies JWT tokens without any library.

  A JWT is three base64url-encoded sections separated by ".":
    header.payload.signature

  Verification steps:
  1. Decode header -> check "alg" field (HS256 or RS256 supported)
  2. Decode payload -> check "exp" (expiry), "iss" (issuer), "aud" (audience)
  3. Verify signature:
     - HS256: HMAC-SHA256(base64(header) <> "." <> base64(payload), secret)
     - RS256: RSA-SHA256 verify with public key

  Why implement this without a library?
  JWT libraries introduce dependencies and often support legacy algorithms (RS512, PS256).
  A custom implementation supports exactly HS256 and RS256 -- no more. Reducing the
  algorithm surface reduces the attack surface.
  """

  @doc """
  Verifies a JWT token string.
  Returns {:ok, claims} or {:error, reason}.
  """
  @spec verify(String.t(), String.t() | binary()) :: {:ok, map()} | {:error, atom()}
  def verify(token, key) do
    with [header_b64, payload_b64, sig_b64] <- String.split(token, ".", parts: 3),
         {:ok, header} <- decode_json(header_b64),
         {:ok, payload} <- decode_json(payload_b64),
         :ok <- check_expiry(payload),
         :ok <- verify_signature(header["alg"], "#{header_b64}.#{payload_b64}", sig_b64, key) do
      {:ok, payload}
    else
      parts when is_list(parts) -> {:error, :malformed_token}
      {:error, reason} -> {:error, reason}
    end
  end

  defp decode_json(base64_string) do
    with {:ok, json} <- Base.url_decode64(base64_string, padding: false),
         {:ok, map} <- Jason.decode(json) do
      {:ok, map}
    else
      _ -> {:error, :malformed_token}
    end
  end

  defp check_expiry(%{"exp" => exp}) do
    if System.system_time(:second) < exp, do: :ok, else: {:error, :token_expired}
  end

  defp check_expiry(_), do: :ok

  defp verify_signature("HS256", signing_input, sig_b64, secret) do
    # Compute the expected MAC using HMAC-SHA256.
    # :crypto.mac/4 is the modern API (OTP 22+) for message authentication codes.
    computed_mac = :crypto.mac(:hmac, :sha256, secret, signing_input)
    computed_b64 = Base.url_encode64(computed_mac, padding: false)

    # Constant-time comparison prevents timing side-channel attacks.
    # An attacker measuring response times could otherwise determine how many
    # leading bytes of the signature match, reconstructing it byte by byte.
    if constant_time_compare(computed_b64, sig_b64) do
      :ok
    else
      {:error, :invalid_signature}
    end
  end

  defp verify_signature("RS256", signing_input, sig_b64, public_key_pem) do
    # Decode the PEM-encoded public key into Erlang's internal representation.
    # PEM files contain base64-encoded DER data wrapped in -----BEGIN/END----- markers.
    [pem_entry | _] = :public_key.pem_decode(public_key_pem)
    decoded_key = :public_key.pem_entry_decode(pem_entry)

    # Decode the signature from base64url
    case Base.url_decode64(sig_b64, padding: false) do
      {:ok, signature_bytes} ->
        # :public_key.verify/4 returns true/false indicating whether the RSA signature
        # over the signing_input is valid for the given public key.
        if :public_key.verify(signing_input, :sha256, signature_bytes, decoded_key) do
          :ok
        else
          {:error, :invalid_signature}
        end

      :error ->
        {:error, :malformed_token}
    end
  end

  defp verify_signature(_alg, _, _, _), do: {:error, {:unsupported_algorithm, nil}}

  # XOR-based constant-time comparison. Iterates all bytes regardless of where
  # the first difference occurs, accumulating differences in `acc`. The final
  # check `acc == 0` reveals only whether the strings matched, not where they differed.
  defp constant_time_compare(a, b) when byte_size(a) != byte_size(b), do: false

  defp constant_time_compare(a, b) do
    a_bytes = :binary.bin_to_list(a)
    b_bytes = :binary.bin_to_list(b)

    Enum.zip(a_bytes, b_bytes)
    |> Enum.reduce(0, fn {x, y}, acc -> Bitwise.bor(acc, Bitwise.bxor(x, y)) end)
    |> Kernel.==(0)
  end
end
```

### Step 5: `lib/apigw/cache.ex`

**Objective**: Read directly from ETS, write through a GenServer — reads stay lock-free while ETag writes serialize cleanly.


The cache stores HTTP responses in ETS keyed by request path, with TTL-based expiration and ETag support. Reads go directly to ETS (no GenServer call), making them lock-free and concurrent. Writes go through a GenServer cast to serialize ETag computation and entry insertion.

The cleanup process runs periodically via `Process.send_after`, scanning ETS for expired entries using `:ets.select_delete/2` with a match specification. This is more efficient than iterating all entries in Elixir because the selection runs inside the ERTS scheduler.

ETag values are derived from the first 16 hex characters of the SHA-256 hash of the response body. When a client sends `If-None-Match` with a matching ETag, the cache returns a 304 Not Modified with no body, saving bandwidth.

```elixir
defmodule Apigw.Cache do
  use GenServer

  @table :apigw_cache
  @default_ttl_s 300

  # ---------------------------------------------------------------------------
  # Public API  (reads go directly to ETS -- no GenServer call)
  # ---------------------------------------------------------------------------

  @doc """
  Checks the cache for a GET request.
  Returns {:hit, response} or :miss.
  Also handles conditional requests via ETag.
  """
  @spec get(String.t(), map()) :: {:hit, map()} | {:miss}
  def get(cache_key, request_headers) do
    case :ets.lookup(@table, cache_key) do
      [{^cache_key, entry}] ->
        if entry_expired?(entry) do
          :miss
        else
          etag = entry.etag
          if_none_match = Map.get(request_headers, "if-none-match")

          if if_none_match == etag do
            {:hit, %{status: 304, headers: [{"etag", etag}], body: ""}}
          else
            {:hit, entry.response}
          end
        end

      [] ->
        :miss
    end
  end

  @doc """
  Stores a response in the cache.
  Computes ETag from response body hash.
  """
  @spec put(String.t(), map(), pos_integer()) :: :ok
  def put(cache_key, response, ttl_s \\ @default_ttl_s) do
    GenServer.cast(__MODULE__, {:put, cache_key, response, ttl_s})
  end

  # ---------------------------------------------------------------------------
  # GenServer
  # ---------------------------------------------------------------------------

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @impl true
  def init(_opts) do
    :ets.new(@table, [:named_table, :public, :set])
    schedule_cleanup()
    {:ok, %{}}
  end

  @impl true
  def handle_cast({:put, key, response, ttl_s}, state) do
    etag = compute_etag(response.body)
    entry = %{
      response: Map.put(response, :headers, [{"etag", etag} | response.headers]),
      etag: etag,
      expires_at: System.monotonic_time(:second) + ttl_s
    }
    :ets.insert(@table, {key, entry})
    {:noreply, state}
  end

  @impl true
  def handle_info(:cleanup, state) do
    # Delete all entries where expires_at is in the past.
    # Because the value is a map stored as element 2 of the {key, value} tuple,
    # we iterate and delete manually for clarity and correctness.
    now = System.monotonic_time(:second)

    :ets.foldl(
      fn {key, entry}, _acc ->
        if entry.expires_at < now, do: :ets.delete(@table, key)
        nil
      end,
      nil,
      @table
    )

    schedule_cleanup()
    {:noreply, state}
  end

  defp entry_expired?(%{expires_at: exp}) do
    System.monotonic_time(:second) > exp
  end

  defp compute_etag(body) do
    # SHA-256 hash of the body, truncated to 16 hex characters.
    # This provides sufficient uniqueness for cache validation while keeping
    # the ETag header compact. Collisions at 16 hex chars (64 bits) are
    # astronomically unlikely for distinct response bodies.
    Base.encode16(:crypto.hash(:sha256, body), case: :lower) |> String.slice(0, 16)
  end

  defp schedule_cleanup, do: Process.send_after(self(), :cleanup, 60_000)
end
```

### Step 6: Given tests — must pass without modification

**Objective**: Tests freeze the gateway's public surface — breaker states and cache headers must match exactly or clients regress.


```elixir
# test/apigw/circuit_breaker_test.exs
defmodule Apigw.CircuitBreakerTest do
  use ExUnit.Case, async: false

  alias Apigw.CircuitBreaker

  setup do
    {:ok, _} = start_supervised({CircuitBreaker, "test-backend"})
    :ok
  end


  describe "CircuitBreaker" do

  test "starts closed and allows requests" do
    assert CircuitBreaker.check("test-backend") == :allow
  end

  test "opens after error threshold exceeded" do
    # Record 10 errors and 0 successes → 100% error rate → should open
    for _ <- 1..10, do: CircuitBreaker.record_outcome("test-backend", :error)
    Process.sleep(10)

    assert {:deny, :circuit_open} = CircuitBreaker.check("test-backend")
  end

  test "closed if error rate below threshold" do
    # 40% error rate — below 50% threshold
    for _ <- 1..4, do: CircuitBreaker.record_outcome("test-backend", :error)
    for _ <- 1..6, do: CircuitBreaker.record_outcome("test-backend", :success)
    Process.sleep(10)

    assert CircuitBreaker.check("test-backend") == :allow
  end


  end
end
```

```elixir
# test/apigw/auth_test.exs
defmodule Apigw.AuthTest do
  use ExUnit.Case, async: true

  alias Apigw.Auth.JWT

  @secret "test_hmac_secret_key_minimum_32_bytes"

  defp make_token(claims, secret) do
    header = Base.url_encode64(~s({"alg":"HS256","typ":"JWT"}), padding: false)
    payload = Base.url_encode64(Jason.encode!(claims), padding: false)
    signing_input = "#{header}.#{payload}"
    mac = :crypto.mac(:hmac, :sha256, secret, signing_input)
    sig = Base.url_encode64(mac, padding: false)
    "#{signing_input}.#{sig}"
  end


  describe "Auth" do

  test "valid HS256 token with future expiry verifies" do
    token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) + 3600}, @secret)
    assert {:ok, claims} = JWT.verify(token, @secret)
    assert claims["sub"] == "user_1"
  end

  test "expired token is rejected" do
    token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) - 1}, @secret)
    assert {:error, :token_expired} = JWT.verify(token, @secret)
  end

  test "tampered payload is rejected" do
    token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) + 3600}, @secret)
    [h, _p, s] = String.split(token, ".")
    tampered_payload = Base.url_encode64(~s({"sub":"admin","exp":9999999999}), padding: false)
    assert {:error, _} = JWT.verify("#{h}.#{tampered_payload}.#{s}", @secret)
  end

  test "malformed token returns error" do
    assert {:error, :malformed_token} = JWT.verify("not.a.jwt.token.at.all", @secret)
  end


  end
end
```

### Step 7: Run the tests

**Objective**: Run with `--trace` so half-open probe ordering and cache eviction race across requests remain deterministic.


```bash
mix test test/apigw/ --trace
```

---

### Why this works

The design separates concerns along their real axes: what must be correct (the API gateway invariants), what must be fast (the hot path isolated from slow paths), and what must be evolvable (external contracts kept narrow). Each module has one job and fails loudly when given inputs outside its contract, so bugs surface near their source instead of as mysterious downstream symptoms. The tests exercise the invariants directly rather than implementation details, which keeps them useful across refactors.


## Main Entry Point

```elixir
def main do
  IO.puts("======== 34 build api gateway ========")
  IO.puts("Demonstrating core functionality")
  IO.puts("")
  
  IO.puts("Run: mix test")
end
```

## Benchmark

```elixir
# bench/apigw_bench.exs
{:ok, _cache} = Apigw.Cache.start_link()
{:ok, pool} = Apigw.Pool.start_link(
  backends: [
    %{id: "b1", host: "localhost", port: 8001, healthy: true, draining: false},
    %{id: "b2", host: "localhost", port: 8002, healthy: true, draining: false}
  ]
)

Benchee.run(%{
  "auth_verify_jwt" => fn ->
    token = make_test_jwt()
    Apigw.Auth.JWT.verify(token, "test_secret")
  end,
  "cache_hit_etag" => fn ->
    Apigw.Cache.get("/api/users", %{"if-none-match" => "abc123"})
  end,
  "circuit_breaker_check" => fn ->
    Apigw.CircuitBreaker.check("b1")
  end,
  "rate_limiter_consume" => fn ->
    {:ok, _} = Apigw.RateLimiter.consume("key_1", 1024)
  end
}, time: 10, warmup: 3)

defp make_test_jwt do
  header = Base.url_encode64(~s({"alg":"HS256"}), padding: false)
  payload = Base.url_encode64(~s({"sub":"user1"}), padding: false)
  sig = :crypto.mac(:hmac, :sha256, "secret", "#{header}.#{payload}") |> Base.url_encode64(padding: false)
  "#{header}.#{payload}.#{sig}"
end
```

## Quick start

```bash
# Start the application
mix deps.get
mix test

# Or run the benchmark:
mix run bench/apigw_bench.exs
```

Target: <500µs gateway overhead per request at p99 excluding upstream latency.

## Key Concepts: Gateway Routing and Rate Limiting Protocols

An API gateway is the composition of independent middleware layers. Each layer can short-circuit the request (auth failure → 401, rate limited → 429, circuit open → 503) without invoking subsequent layers.

**Sliding-window rate limiting**: A naive fixed-window counter allows a burst at boundaries. With limit=100 req/s and a 1-second window, the first 100 requests at 0.999s and the next 100 at 1.001s consume 100 req/sec over a 2ms window. Sliding-window counting avoids this: track a list of request timestamps and drop requests older than the window size when counting. More precise but higher memory cost.

**Lease-based rate limiting**: A single rate limiter process serializes all requests through a call. To scale, distribute the limit: each gateway node acquires a lease of `quota / N` from a central coordinator every few seconds. Within a lease period, the node enforces the limit locally with `:atomics`. Trade-off: short leases mean frequent coordinator hits; long leases mean a crashed node forfeits its tokens.

**Circuit breaker half-open state**: Two-state breakers (open/closed) re-open immediately on timeout, causing thundering herd. The half-open state allows exactly one probe request through. Success closes the breaker for all instances; failure keeps it open. This "single probe" invariant prevents overwhelming a recovering backend.

**Cache validation with ETags**: ETag headers allow resumable downloads and conditional GET. A client sends `If-None-Match: "abc"` and if the ETag matches, the server returns 304 (Not Modified) without the body. This saves bandwidth for large responses. ETags must be opaque to clients — any stable hash of the content is valid.

**Production insight**: Gateways are strict about HTTP compliance. Missing Rate-Limit headers on a 429 leaves clients guessing when to retry. Wrong Cache-Control directives break browser caching. Missing CORS headers block web clients. Test your gateway against real browsers and clients, not just curl.

---

## Trade-off analysis

| Concern | Gateway-level (your impl) | Backend-level | External service |
|---------|--------------------------|---------------|------------------|
| Auth | centralized, consistent | per-service (risk of gaps) | IdP like Auth0 |
| Rate limiting | per-key, no backend changes | harder to coordinate | Redis/Envoy |
| Circuit breaking | protects backend from storms | self-protection only | Hystrix, Resilience4j |
| Caching | transparent to backend | backend-aware cache | CDN (Cloudflare, etc.) |
| Tracing | single injection point | manual per service | OpenTelemetry auto-instr |
| Single point of failure | yes — must be HA | no | usually HA |

Reflection: the gateway is a single point of failure. What changes would you make to run `apigw` in a 3-node cluster with no shared state except ETS? Which components require distributed coordination and which are safe to be node-local?

---

## Common production mistakes

**1. Circuit breaker that never transitions to half-open**
If the `open_duration_ms` timer is reset on every incoming request (even rejected ones), the circuit never gets a chance to probe the backend. The timer must count from `opened_at`, not from the last rejected request.

**2. Rate limit headers missing on denied requests**
RFC 6585 and industry convention require `X-RateLimit-Limit`, `X-RateLimit-Remaining`, and `X-RateLimit-Reset` on ALL responses, not just successful ones. A 429 response without these headers leaves clients guessing when to retry.

**3. JWT algorithm confusion attack**
A token with `"alg": "none"` bypasses signature verification in naive implementations. Your `verify/2` must reject any algorithm not in an explicit allowlist (`["HS256", "RS256"]`). The `alg` field in the header is attacker-controlled.

**4. Cache key collision between different routes**
If two routes `/users?sort=name` and `/users?sort=email` map to the same cache key because query params are not normalized, one response pollutes the cache for the other. The cache key must include a canonical representation of the query string (sorted params, lowercase values).

**5. Least-connections count going negative**
When a backend dies mid-request, the connection count for that backend is never decremented if decrement happens in the response handler. Use `try/after` to guarantee decrement regardless of proxy outcome.

---

## Reflection

Your gateway sits between 200 microservices. How do you prevent a single misbehaving upstream from consuming all gateway goroutines/processes, and how does that policy interact with fairness across tenants?

## Resources

- [W3C Trace Context Specification](https://www.w3.org/TR/trace-context/) — the `traceparent` header format your gateway must propagate
- [Hystrix Design Document](https://github.com/Netflix/Hystrix/wiki/How-it-Works) — Netflix's original circuit breaker design; the half-open state rationale is here
- [JWT RFC 7519](https://www.rfc-editor.org/rfc/rfc7519) — sections 4 (claims) and 7 (validation) define what your verifier must check
- [Mint](https://hexdocs.pm/mint/Mint.HTTP.html) — the HTTP client for your reverse proxy; study connection reuse and streaming response handling
- ["Building Microservices"](https://www.oreilly.com/library/view/building-microservices-2nd/9781492034018/) — Sam Newman, O'Reilly — the API Gateway chapter covers patterns your implementation must handle
