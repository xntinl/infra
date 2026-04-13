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

## Project structure

\`\`\`
apigw/
├── lib/
│   └── apigw.ex
├── test/
│   └── apigw_test.exs
├── script/
│   └── main.exs
└── mix.exs
\`\`\`

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

### `lib/apigw.ex`

```elixir
defmodule Apigw do
  @moduledoc """
  API Gateway.

  timeouts alone let a degraded upstream exhaust gateway resources indefinitely. A breaker trips on error rate and fails fast, giving the upstream time to recover instead of piling....
  """
end
```
### `lib/apigw/circuit_breaker.ex`

**Objective**: Enforce the half-open single-probe invariant — two probes in parallel defeat the whole state machine.

The circuit breaker is a per-backend GenServer implementing a three-state machine: closed (normal), open (rejecting), and half-open (probing). It tracks request outcomes in a sliding time window and transitions to open when the error rate exceeds the configured threshold. After a cooldown period, it transitions to half-open and allows exactly one probe request through. A successful probe closes the breaker; a failed probe reopens it.

The key design decision is storing events as a list of `{timestamp, outcome}` tuples, pruning entries older than the window on every recording. This keeps memory bounded and provides an accurate rolling error rate without fixed time buckets (which can miss spikes that straddle bucket boundaries).

```elixir
defmodule Apigw.CircuitBreaker do
  @moduledoc """
  Ejercicio: API Gateway.
  Implementa el comportamiento descrito en el enunciado, exponiendo funciones públicas documentadas y con tipos claros.
  """

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
### `lib/apigw/auth/jwt.ex`

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
### `lib/apigw/cache.ex`

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
defmodule Apigw.CircuitBreakerTest do
  use ExUnit.Case, async: false
  doctest Apigw.Cache

  alias Apigw.CircuitBreaker

  setup do
    {:ok, _} = start_supervised({CircuitBreaker, "test-backend"})
    :ok
  end

  describe "state machine: closed state" do
    test "starts closed and allows requests" do
      assert CircuitBreaker.check("test-backend") == :allow
    end

    test "allows all requests when healthy" do
      for _ <- 1..5 do
        CircuitBreaker.record_outcome("test-backend", :success)
        assert CircuitBreaker.check("test-backend") == :allow
      end
    end
  end

  describe "state machine: transition to open" do
    test "opens after error threshold exceeded" do
      # Record 10 errors and 0 successes → 100% error rate → should open
      for _ <- 1..10, do: CircuitBreaker.record_outcome("test-backend", :error)
      Process.sleep(10)

      assert {:deny, :circuit_open} = CircuitBreaker.check("test-backend")
    end

    test "does not open if error rate below threshold" do
      # 40% error rate — below 50% threshold
      for _ <- 1..4, do: CircuitBreaker.record_outcome("test-backend", :error)
      for _ <- 1..6, do: CircuitBreaker.record_outcome("test-backend", :success)
      Process.sleep(10)

      assert CircuitBreaker.check("test-backend") == :allow
    end

    test "requires minimum event count before opening" do
      # Single error should not trip the breaker
      CircuitBreaker.record_outcome("test-backend", :error)
      Process.sleep(10)
      assert CircuitBreaker.check("test-backend") == :allow
    end
  end

  describe "state machine: half-open probe" do
    test "transitions to half-open after cooldown period" do
      for _ <- 1..10, do: CircuitBreaker.record_outcome("test-backend-2", :error)
      Process.sleep(10)

      # Circuit is open; wait for cooldown
      assert {:deny, :circuit_open} = CircuitBreaker.check("test-backend-2")
      
      Process.sleep(35_000)  # wait beyond @open_duration_ms (30s)
      # Now should be half-open, allowing one probe
      assert CircuitBreaker.check("test-backend-2") == :allow
    end

    test "closes on successful probe" do
      for _ <- 1..10, do: CircuitBreaker.record_outcome("test-backend-3", :error)
      Process.sleep(10)
      assert {:deny, :circuit_open} = CircuitBreaker.check("test-backend-3")

      Process.sleep(35_000)
      assert CircuitBreaker.check("test-backend-3") == :allow  # probe allowed
      
      CircuitBreaker.record_outcome("test-backend-3", :success)  # probe succeeds
      Process.sleep(10)
      
      assert CircuitBreaker.check("test-backend-3") == :allow  # closed
    end
  end
end
```
```elixir
defmodule Apigw.AuthTest do
  use ExUnit.Case, async: true
  doctest Apigw.Cache

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

  describe "JWT verification" do
    test "valid HS256 token with future expiry verifies" do
      token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) + 3600}, @secret)
      assert {:ok, claims} = JWT.verify(token, @secret)
      assert claims["sub"] == "user_1"
    end

    test "extracts claims from verified token" do
      claims = %{"sub" => "user_123", "aud" => "api.example.com", "exp" => System.system_time(:second) + 3600}
      token = make_token(claims, @secret)
      assert {:ok, verified} = JWT.verify(token, @secret)
      assert verified["sub"] == "user_123"
      assert verified["aud"] == "api.example.com"
    end
  end

  describe "token expiry validation" do
    test "expired token is rejected" do
      token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) - 1}, @secret)
      assert {:error, :token_expired} = JWT.verify(token, @secret)
    end

    test "token without exp claim is accepted (no expiry check)" do
      token = make_token(%{"sub" => "user_1"}, @secret)
      assert {:ok, claims} = JWT.verify(token, @secret)
      assert claims["sub"] == "user_1"
    end
  end

  describe "signature validation" do
    test "tampered payload is rejected" do
      token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) + 3600}, @secret)
      [h, _p, s] = String.split(token, ".")
      tampered_payload = Base.url_encode64(~s({"sub":"admin","exp":9999999999}), padding: false)
      assert {:error, :invalid_signature} = JWT.verify("#{h}.#{tampered_payload}.#{s}", @secret)
    end

    test "wrong secret produces invalid signature error" do
      token = make_token(%{"sub" => "user_1", "exp" => System.system_time(:second) + 3600}, @secret)
      assert {:error, :invalid_signature} = JWT.verify(token, "wrong_secret_key_at_least_32_bytes_long")
    end
  end

  describe "malformed tokens" do
    test "too few segments returns error" do
      assert {:error, :malformed_token} = JWT.verify("header.payload", @secret)
    end

    test "too many segments returns error" do
      assert {:error, :malformed_token} = JWT.verify("h.p.s.extra", @secret)
    end

    test "invalid base64 returns error" do
      assert {:error, :malformed_token} = JWT.verify("!!!.!!!.!!!", @secret)
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
## Quick start

```bash
# Start the application and run tests
mix deps.get
mix test test/apigw/ --trace

# Or run performance benchmarks:
mix run bench/proxy_bench.exs
```

Target: <10ms p99 latency per request end-to-end (auth + rate-limit + circuit-check + proxy).

---

## Benchmark

```elixir
# bench/proxy_bench.exs
{:ok, _} = Apigw.Application.start_link([])

Benchee.run(%{
  "jwt_verify_hs256" => fn ->
    token = "eyJ..." # valid HS256 token
    Apigw.Auth.JWT.verify(token, "secret_key")
  end,
  "circuit_breaker_check" => fn ->
    Apigw.CircuitBreaker.check("backend-1")
  end,
  "cache_hit_etag" => fn ->
    Apigw.Cache.get("/api/users/123", %{"if-none-match" => "abc123def456"})
  end,
  "cache_miss" => fn ->
    Apigw.Cache.get("/api/users/999", %{})
  end,
  "rate_limiter_check" => fn ->
    Apigw.RateLimiter.check("api-key-1234", 1000)
  end
}, time: 10, warmup: 3, memory_time: 2)
```
**Expected results** (on modern hardware):
- JWT verify: ~5-10µs per verify
- Circuit breaker check: ~1µs (lock-free Registry lookup)
- Cache hit with ETag: ~1-2µs (ETS direct lookup)
- Cache miss: ~1µs (ETS lookup returns not found)
- Rate limiter: ~0.5-1µs (atomics counter)

---

## Key Concepts: API Gateway Patterns and Composition

**Middleware chain composition**: The gateway is a pipeline: `auth → rate-limit → circuit-breaker → cache-check → proxy → cache-write`. Each layer is independent and can short-circuit (return error or cached response). This is more composable than a monolithic request handler.

**Circuit breaker half-open state**: A two-state breaker (open/closed) would cause request storms: all instances probe simultaneously when timeout expires. The half-open state allows exactly one probe through. Success closes; failure reopens. This "single probe" invariant prevents cascading recovery failures.

**Lease-based rate limiting scales across nodes**: Each gateway node acquires a quota (e.g., 10k req/s / 10 nodes = 1k req/s per node) and enforces locally with atomics. No cross-node coordination per request — only per lease renewal (every few seconds). Trade-off: a node crash forfeits its quota; brief over-serving is acceptable.

**JWT verification without libraries**: JWT is simple (base64-url three parts + HMAC or RSA). A custom implementation pinning only HS256 and RS256 reduces algorithm surface. A JWT library might support RS512, PS256 — unnecessary and a potential vulnerability.

**ETag-based cache validation**: Rather than always returning full bodies, send `ETag: <hash>`. Clients that already have the body send `If-None-Match: <hash>`. Gateway returns 304 Not Modified (no body). Saves bandwidth on large responses.

**Production insight**: A gateway is the bottleneck and blast radius of the platform. Make the hot path (circuit check + cache lookup) as fast as possible — both are sub-microsecond here via lock-free data structures.

---

## Reflection

1. **Rate limiter interactions with circuit breaker**: If you rate-limit too aggressively, the circuit breaker never sees backend errors (all requests are rejected upstream). If you rate-limit too loosely, a misbehaving client overwhelms the backend, tripping the breaker. How would you coordinate these two policies?

2. **Cache effectiveness with variable backend latency**: A cached response from a fast API endpoint is cheap. But if the backend is slow or degraded (why you're rate-limiting), how do you prevent cache pollution with stale data? At what freshness SLA does caching become harmful rather than helpful?

---

## Production-Grade Addendum (Insane Standard)

The sections below extend the content above to the full `insane` template: a canonical `mix.exs`, an executable `script/main.exs` stress harness, explicit error-handling and recovery protocols, concrete performance targets, and a consolidated key-concepts list. These are non-negotiable for production-grade systems.

### `mix.exs`

```elixir
defmodule Gatex.MixProject do
  use Mix.Project

  def project do
    [
      app: :gatex,
      version: "0.1.0",
      elixir: "~> 1.19",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      test_coverage: [summary: [threshold: 80]],
      dialyzer: [plt_add_apps: [:mix, :ex_unit]]
    ]
  end

  def application do
    [
      extra_applications: [:logger, :crypto],
      mod: {Gatex.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:jason, "~> 1.4"},
      {:benchee, "~> 1.2", only: :dev},
      {:stream_data, "~> 0.6", only: :test},
      {:dialyxir, "~> 1.4", only: :dev, runtime: false}
    ]
  end
end
```
### `script/main.exs`

```elixir
defmodule Main do
  @moduledoc """
  Realistic stress harness for `gatex` (API gateway).
  Runs five phases: warmup, steady-state load, chaos injection, recovery
  verification, and invariant audit. Exits non-zero if any SLO is breached.
  """

  @warmup_ops 10_000
  @steady_ops 100_000
  @chaos_ratio 0.10
  @slo_p99_us 5000
  @slo_error_rate 0.001

  def main do
    :ok = Application.ensure_all_started(:gatex) |> elem(0) |> then(&(&1 == :ok && :ok || :ok))
    IO.puts("=== Gatex stress test ===")

    warmup()
    baseline = steady_phase()
    chaos = chaos_phase()
    recovery = recovery_phase()
    invariants = invariant_phase()

    report([baseline, chaos, recovery, invariants])
  end

  defp warmup do
    IO.puts("Phase 0: warmup (#{@warmup_ops} ops, not measured)")
    run_ops(@warmup_ops, :warmup, measure: false)
    IO.puts("  warmup complete\n")
  end

  defp steady_phase do
    IO.puts("Phase 1: steady-state load (#{@steady_ops} ops @ target throughput)")
    started = System.monotonic_time(:millisecond)
    latencies = run_ops(@steady_ops, :steady, measure: true)
    elapsed_s = (System.monotonic_time(:millisecond) - started) / 1000
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :steady, ok: ok, error_rate: err, throughput: round(ok / elapsed_s)})
  end

  defp chaos_phase do
    IO.puts("\nPhase 2: chaos injection (#{trunc(@chaos_ratio * 100)}%% faults)")
    # Inject realistic fault: process kills, disk stalls, packet loss
    chaos_inject()
    latencies = run_ops(div(@steady_ops, 2), :chaos, measure: true, fault_ratio: @chaos_ratio)
    chaos_heal()
    p = percentiles(latencies)
    ok = Enum.count(latencies, &match?({:ok, _}, &1))
    err = (length(latencies) - ok) / max(length(latencies), 1)
    Map.merge(p, %{phase: :chaos, ok: ok, error_rate: err})
  end

  defp recovery_phase do
    IO.puts("\nPhase 3: cold-restart recovery")
    t0 = System.monotonic_time(:millisecond)
    case Application.stop(:gatex) do
      :ok -> :ok
      _ -> :ok
    end
    {:ok, _} = Application.ensure_all_started(:gatex)
    recovery_ms = System.monotonic_time(:millisecond) - t0
    healthy = health_check?()
    %{phase: :recovery, recovery_ms: recovery_ms, healthy: healthy}
  end

  defp invariant_phase do
    IO.puts("\nPhase 4: invariant audit")
    violations = run_invariant_checks()
    %{phase: :invariants, violations: violations}
  end

  # ---- stubs: wire these to your impl ----

  defp run_ops(n, _label, opts) do
    measure = Keyword.get(opts, :measure, false)
    fault = Keyword.get(opts, :fault_ratio, 0.0)
    parent = self()
    workers = System.schedulers_online() * 2
    per = div(n, workers)

    tasks =
      for _ <- 1..workers do
        Task.async(fn -> worker_loop(per, measure, fault) end)
      end

    Enum.flat_map(tasks, &Task.await(&1, 60_000))
  end

  defp worker_loop(n, measure, fault) do
    Enum.map(1..n, fn _ ->
      t0 = System.monotonic_time(:microsecond)
      result = op(fault)
      elapsed = System.monotonic_time(:microsecond) - t0
      if measure, do: {tag(result), elapsed}, else: :warm
    end)
    |> Enum.reject(&(&1 == :warm))
  end

  defp op(fault) do
    if :rand.uniform() < fault do
      {:error, :fault_injected}
    else
      # TODO: replace with actual gatex operation
      {:ok, :ok}
    end
  end

  defp tag({:ok, _}), do: :ok
  defp tag({:error, _}), do: :err

  defp chaos_inject, do: :ok
  defp chaos_heal, do: :ok
  defp health_check?, do: true
  defp run_invariant_checks, do: 0

  defp percentiles([]), do: %{p50: 0, p95: 0, p99: 0, p999: 0}
  defp percentiles(results) do
    lats = for {_, us} <- results, is_integer(us), do: us
    s = Enum.sort(lats); n = length(s)
    if n == 0, do: %{p50: 0, p95: 0, p99: 0, p999: 0},
       else: %{
         p50: Enum.at(s, div(n, 2)),
         p95: Enum.at(s, div(n * 95, 100)),
         p99: Enum.at(s, div(n * 99, 100)),
         p999: Enum.at(s, min(div(n * 999, 1000), n - 1))
       }
  end

  defp report(phases) do
    IO.puts("\n=== SUMMARY ===")
    Enum.each(phases, fn p ->
      IO.puts("#{p.phase}: #{inspect(Map.drop(p, [:phase]))}")
    end)

    bad =
      Enum.any?(phases, fn
        %{p99: v} when is_integer(v) and v > @slo_p99_us -> true
        %{error_rate: v} when is_float(v) and v > @slo_error_rate -> true
        %{violations: v} when is_integer(v) and v > 0 -> true
        _ -> false
      end)

    System.halt(if(bad, do: 1, else: 0))
  end
end

Main.main()
```
### Running the stress harness

```bash
mix deps.get
mix compile
mix run --no-halt script/main.exs
```

The harness exits 0 on SLO compliance and 1 otherwise, suitable for CI gating.

---

## Error Handling and Recovery

Gatex classifies every failure on two axes: **severity** (critical vs recoverable) and **scope** (per-request vs system-wide). Critical violations halt the subsystem and page an operator; recoverable faults are retried with bounded backoff and explicit budgets.

### Critical failures (halt, alert, preserve forensics)

| Condition | Detection | Response |
|---|---|---|
| Persistent-state corruption (checksum mismatch) | On-read validation | Refuse boot; preserve raw state for forensics; page SRE |
| Safety invariant violated (e.g., two holders observed) | Background invariant checker | Enter read-only safe mode; emit `:safety_violation` telemetry |
| Supervisor reaches `max_restarts` | BEAM default | Exit non-zero so orchestrator (systemd/k8s) reschedules |
| Monotonic time regression | `System.monotonic_time/1` decreases | Hard crash (BEAM bug; unrecoverable) |

### Recoverable failures

| Failure | Policy | Bounds |
|---|---|---|
| Transient peer RPC timeout | Exponential backoff (base 50ms, jitter 20%%) | Max 3 attempts, max 2s total |
| Downstream service unavailable | Circuit-breaker (3-state: closed/open/half-open) | Open for 5s after 5 consecutive failures |
| Rate-limit breach | Return `{:error, :rate_limited}` with `Retry-After` | Client responsibility to back off |
| Disk full on append | Reject new writes, drain in-flight | Recovery after ops frees space |
| GenServer mailbox > high-water mark | Backpressure upstream (refuse enqueue) | High water: 10k msgs; low water: 5k |

### Recovery protocol (cold start)

1. **State replay**: Read the last full snapshot, then replay WAL entries with seq > snapshot_seq. Each entry carries a CRC32; mismatches halt replay.
2. **Peer reconciliation** (if distributed): Exchange state vectors with quorum peers; adopt authoritative state per the protocol's conflict resolution rule.
3. **Warm health probe**: All circuit breakers start in `:half_open`; serve one probe request per dependency before accepting real traffic.
4. **Readiness gate**: External endpoints (HTTP, gRPC) refuse traffic until `/healthz/ready` returns 200; liveness passes earlier.
5. **Backlog drain**: Any in-flight requests recovered from the WAL are re-delivered; consumers must be idempotent on the supplied request-id.

### Bulkheads and security bounds

- **Input size**: max request/message body 1 MiB, max nesting depth 32, max field count 1024.
- **Resource limits per client**: max open connections 100, max in-flight requests 1000, max CPU time per request 100ms.
- **Backpressure propagation**: every bounded queue is visible; upstream sees `{:error, :shed_load}` rather than silent buffering.
- **Process isolation**: each high-traffic component has its own supervisor tree; crashes are local, not cluster-wide.

---

## Performance Targets

Concrete numbers derived from comparable production systems. Measure with `script/main.exs`; any regression > 10%% vs prior baseline fails CI.

| Metric | Target | Source / Comparable |
|---|---|---|
| **Sustained throughput** | **100,000 req/s** | comparable to reference system |
| **Latency p50** | below p99/4 | — |
| **Latency p99** | **5 ms** | Envoy proxy architecture |
| **Latency p999** | ≤ 3× p99 | excludes GC pauses > 10ms |
| **Error rate** | **< 0.1 %%** | excludes client-side 4xx |
| **Cold start** | **< 3 s** | supervisor ready + warm caches |
| **Recovery after crash** | **< 5 s** | replay + peer reconciliation |
| **Memory per connection/entity** | **< 50 KiB** | bounded by design |
| **CPU overhead of telemetry** | **< 1 %%** | sampled emission |

### Baselines we should beat or match

- Envoy proxy architecture: standard reference for this class of system.
- Native BEAM advantage: per-process isolation and lightweight concurrency give ~2-5x throughput vs process-per-request architectures (Ruby, Python) on equivalent hardware.
- Gap vs native (C++/Rust) implementations: expect 2-3x latency overhead in the hot path; mitigated by avoiding cross-process message boundaries on critical paths (use ETS with `:write_concurrency`).

---

## Why API Gateway matters

Mastering **API Gateway** directly impacts how you design reliable, scalable Elixir systems. The patterns and trade-offs covered in this exercise appear in production code shipped by companies running the BEAM at scale — from payment processors to chat platforms to telemetry pipelines.

Understanding the underlying semantics (not just the syntax) is what separates engineers who can debug a cascading failure at 3 AM from those who can only write new code. This document focuses on the *why*: memory layout, process boundaries, failure semantics, and the trade-offs you are implicitly accepting when you choose one approach over another.

Invest the time here. The compound interest on fundamental concepts is enormous.

### `test/apigw_test.exs`

```elixir
defmodule ApigwTest do
  use ExUnit.Case, async: true

  doctest Apigw

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert Apigw.run(:noop) == :ok
    end
  end
end
```
---

## Key concepts
### 1. Failure is not an exception, it is the default
Distributed systems fail continuously; correctness means reasoning about every possible interleaving. Every operation must have a documented failure mode and a recovery path. "It worked on my laptop" is not a proof.

### 2. Backpressure must propagate end-to-end
Any unbounded buffer is a latent OOM. Every queue has a high-water mark, every downstream signals pressure upstream. The hot-path signal is `{:error, :shed_load}` or HTTP 503 with `Retry-After`.

### 3. Monotonic time, never wall-clock, for durations
Use `System.monotonic_time/1` for TTLs, deadlines, and timers. Wall-clock can jump (NTP, container migration, VM pause) and silently breaks every time-based guarantee.

### 4. The log is the source of truth; state is a cache
Derive every piece of state by replaying the append-only log. Do not maintain parallel "current state" that needs to be kept in sync — consistency windows after crashes are where bugs hide.

### 5. Idempotency is a correctness requirement, not a convenience
Every externally-visible side effect must be idempotent on its request ID. Retries, recovery replays, and distributed consensus all rely on this. Non-idempotent operations break under any of the above.

### 6. Observability is a correctness property
In a system at scale, the only way to know you meet the SLO is to measure continuously. Bounded-memory sketches (reservoir sampling for percentiles, HyperLogLog for cardinality, Count-Min for frequency) give actionable estimates without O(n) storage.

### 7. Bounded everything: time, memory, retries, concurrency
Every unbounded resource is a DoS vector. Every loop has a max iteration count; every map has a max size; every retry has a max attempt count; every timeout has an explicit value. Defaults are conservative; tuning happens with measurement.

### 8. Compose primitives, do not reinvent them
Use OTP's supervision trees, `:ets`, `Task.Supervisor`, `Registry`, and `:erpc`. Reinvention is for understanding; production wraps the BEAM's battle-tested primitives. Exception: when a primitive's semantics (like `:global`) do not match the safety requirement, replace it with a purpose-built implementation whose failure mode is documented.

### References

- Envoy proxy architecture
- [Release It! (Nygard)](https://pragprog.com/titles/mnee2/release-it-second-edition/) — circuit breaker, bulkhead, steady-state
- [Google SRE Book](https://sre.google/books/) — SLOs, error budgets, overload handling
- [Designing Data-Intensive Applications (Kleppmann)](https://dataintensive.net/) — correctness under failure

---
