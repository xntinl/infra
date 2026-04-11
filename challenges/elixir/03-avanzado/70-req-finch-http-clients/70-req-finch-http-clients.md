# Req + Finch — HTTP Clients for Upstream Calls

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

You're building `api_gateway`. The gateway proxies requests to upstream services.
Until now, upstream calls were fire-and-forget with no connection pooling, no retry,
and no timeout enforcement. The SRE team reported three production incidents caused
by upstream services returning 503s: the gateway exhausted OS file descriptors
(no connection pool), requests hung indefinitely (no timeout), and a thundering herd
of retries made the upstream recovery slower (no backoff).

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       ├── upstream/
│       │   ├── client.ex           # ← you implement this
│       │   ├── req_steps.ex        # ← you implement this (Req middleware steps)
│       │   └── pool_supervisor.ex  # ← you implement this (Finch pool config)
│       └── application.ex         # already exists — add Finch to supervision tree
├── test/
│   └── api_gateway/
│       └── upstream/
│           └── client_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The upstream client needs to satisfy four requirements:

1. **Connection pooling**: limit the number of open TCP connections per upstream host
   so the OS does not exhaust file descriptors under burst load.
2. **Retry with backoff**: transient upstream errors (502, 503, 504, connection refused)
   should be retried automatically with exponential backoff, without burdening the
   caller with retry logic.
3. **Request tracing**: every upstream call must emit a telemetry event with duration,
   status code, and upstream host so the metrics pipeline can track latency per service.
4. **Streaming**: some upstreams return large response bodies (audit log exports,
   metrics dumps). The client must stream the response to disk without loading the
   full body into the BEAM process heap.

---

## Why Finch manages pools instead of letting Hackney or httpc do it

`httpc` (Erlang stdlib) creates a new TCP connection for every request unless you
configure persistent connections manually — which is undocumented and brittle.
`hackney` has a connection pool but it is global and shared across all callers.
There is no way to give the billing service a separate pool from the auth service.

Finch is designed around named pools. You start one `{Finch, name: BillingFinch, pools: ...}`
per upstream domain with explicit `size` and `count` parameters. The pool is
supervised independently. If the billing upstream's pool is exhausted, auth
requests continue unaffected.

`Req` is built on top of Finch and adds the request/response middleware layer:
composable steps for retry, authentication, JSON encode/decode, telemetry, and caching.
Req and Finch are separate concerns: Finch manages TCP connections; Req manages
the request lifecycle.

---

## Why retry belongs in a Req step and not in the caller

If the caller handles retries:

```elixir
# caller code — retry logic leaks everywhere
case Client.get("/health") do
  {:error, :service_unavailable} -> Client.get("/health")  # retry once
  other -> other
end
```

Every caller duplicates the retry logic, and each caller may choose different
retry counts, backoff strategies, or retryable status codes. When the SRE team
wants to change the retry policy, they touch every call site.

A Req step centralises the retry policy. The caller calls `Client.get/1` and receives
either a successful response or `{:error, :max_retries_exceeded}`. The retry
strategy is declared once in `client/0` and applies uniformly.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
{:req, "~> 0.5"},
{:finch, "~> 0.19"}
```

### Step 2: Pool supervisor — `lib/api_gateway/upstream/pool_supervisor.ex`

```elixir
defmodule ApiGateway.Upstream.PoolSupervisor do
  @moduledoc """
  Starts one named Finch pool per upstream service.

  Pool sizing rules:
  - size: concurrent connections per pool worker process
  - count: number of pool worker processes
  - Total max connections = size * count

  HTTP/2 upstreams: size: 1, count: N (one connection multiplexes N streams)
  HTTP/1.1 upstreams: size: N, count: 1 (N connections, each serves one request)
  """

  def child_spec(_opts) do
    upstreams = Application.get_env(:api_gateway, :upstreams, [])

    pools =
      Enum.reduce(upstreams, %{}, fn {_name, config}, acc ->
        url = Keyword.fetch!(config, :base_url)
        protocol = Keyword.get(config, :protocol, :http1)
        pool_size = Keyword.get(config, :pool_size, 25)
        pool_count = Keyword.get(config, :pool_count, 2)

        Map.put(acc, url, [
          size: pool_size,
          count: pool_count,
          protocol: protocol
        ])
      end)

    %{
      id: ApiGateway.Upstream.Finch,
      start: {Finch, :start_link, [[name: ApiGateway.Upstream.Finch, pools: pools]]}
    }
  end
end
```

### Step 3: Req steps — `lib/api_gateway/upstream/req_steps.ex`

```elixir
defmodule ApiGateway.Upstream.ReqSteps do
  @moduledoc """
  Custom Req steps for the api_gateway upstream client.

  A Req step is a function that receives a `Req.Request` struct and returns
  either the request (to continue the pipeline) or a `{request, response}` tuple
  (to short-circuit the pipeline — used for caching and retries).

  Steps are composed with:
    Req.Request.prepend_request_steps/2 — runs before sending
    Req.Request.append_response_steps/2 — runs after receiving

  Or the shorthand Req.new(steps: [...]) accepts {name, fun} pairs.
  """

  require Logger

  @doc """
  Telemetry step: emits [:api_gateway, :upstream, :request, :start/:stop]
  events around every upstream HTTP request.

  Attaches upstream host and HTTP method to the event metadata so the
  metrics pipeline can aggregate latency per service.
  """
  def telemetry(request) do
    start_time = System.monotonic_time()

    metadata = %{
      host: request.url.host,
      method: request.method,
      path: request.url.path
    }

    :telemetry.execute(
      [:api_gateway, :upstream, :request, :start],
      %{system_time: System.system_time()},
      metadata
    )

    # Return {request, response_step_fn}
    # The response_step_fn is called after the response arrives.
    {request,
     fn {req, resp} ->
       duration = System.monotonic_time() - start_time

       :telemetry.execute(
         [:api_gateway, :upstream, :request, :stop],
         %{duration: duration},
         Map.merge(metadata, %{status: resp.status})
       )

       {req, resp}
     end}
  end

  @doc """
  Request ID propagation step: attaches the current request's X-Request-ID
  (from the process dictionary or generates a new one) to the upstream call.

  This allows distributed tracing across gateway → upstream service boundaries.
  """
  def propagate_request_id(request) do
    # TODO: get the current request ID from Process.get(:request_id)
    # If nil, generate one with :crypto.strong_rand_bytes(8) |> Base.encode16()
    # Add it as a header: {"x-request-id", request_id}
    # Return the modified request struct using Req.Request.put_header/3
    request
  end

  @doc """
  ETS response cache step: caches GET responses for `cache_ttl` seconds.

  Cache key: the full URI string.
  Cache table: :upstream_response_cache (must be created at application start).

  A cache hit short-circuits the pipeline — the upstream is not called.
  A cache miss stores the response after a successful (status 200) upstream call.
  """
  def ets_cache(request) do
    if request.method == :get do
      key = URI.to_string(request.url)

      case :ets.lookup(:upstream_response_cache, key) do
        [{^key, cached_response, expires_at}]
        when expires_at > System.monotonic_time(:second) ->
          # TODO: short-circuit with Req.Request.halt(request) and return the cached response
          # Return {Req.Request.halt(request), cached_response}
          request

        _ ->
          {request,
           fn {req, resp} ->
             if resp.status == 200 do
               ttl = Map.get(req.options, :cache_ttl, 30)
               expires = System.monotonic_time(:second) + ttl

               # TODO: insert {key, resp, expires} into :upstream_response_cache
             end

             {req, resp}
           end}
      end
    else
      # Non-GET requests are never cached
      request
    end
  end
end
```

### Step 4: Upstream client — `lib/api_gateway/upstream/client.ex`

```elixir
defmodule ApiGateway.Upstream.Client do
  @moduledoc """
  HTTP client for upstream service calls.

  Builds a Req.Request with:
  - Named Finch pool (ApiGateway.Upstream.Finch) for connection reuse
  - Retry on transient errors (502, 503, 504, connection errors)
  - Exponential backoff: 500ms, 1s, 2s
  - Telemetry step for latency tracking
  - Request ID propagation for distributed tracing
  - 5 second connect timeout, 30 second read timeout

  All upstream calls go through `request/3`. Callers do not interact with
  Req or Finch directly.
  """

  alias ApiGateway.Upstream.ReqSteps

  @connect_timeout_ms 5_000
  @receive_timeout_ms 30_000
  @max_retries 3

  # Backoff: 500ms → 1s → 2s
  defp backoff(attempt), do: :timer.seconds(1) * Integer.pow(2, attempt - 1) |> div(2)

  defp client(base_url) do
    # TODO: build a Req.Request with:
    #   base_url: base_url
    #   finch: ApiGateway.Upstream.Finch
    #   connect_options: [timeout: @connect_timeout_ms]
    #   receive_timeout: @receive_timeout_ms
    #   retry: :transient
    #   max_retries: @max_retries
    #   retry_delay: &backoff/1
    #   decode_body: true

    # TODO: prepend the telemetry step using Req.Request.prepend_request_steps/2
    # Step name: :telemetry, function: &ReqSteps.telemetry/1

    # TODO: prepend the propagate_request_id step
    # Step name: :request_id, function: &ReqSteps.propagate_request_id/1

    Req.new(base_url: base_url)
  end

  @doc """
  Makes a GET request to `path` on the given upstream `base_url`.
  Returns `{:ok, body}` on HTTP 2xx or `{:error, reason}` otherwise.
  """
  def get(base_url, path, opts \\ []) do
    # TODO: call Req.get(client(base_url), url: path, params: Keyword.get(opts, :params, []))
    # Map the result:
    #   {:ok, %{status: s, body: body}} when s in 200..299 → {:ok, body}
    #   {:ok, %{status: 404}} → {:error, :not_found}
    #   {:ok, %{status: s, body: body}} → {:error, {:upstream_error, s, body}}
    #   {:error, exception} → {:error, {:network, exception}}
    {:error, :not_implemented}
  end

  @doc """
  Makes a POST request with a JSON body.
  Returns `{:ok, body}` on HTTP 2xx or `{:error, reason}`.
  """
  def post(base_url, path, body, opts \\ []) do
    # TODO: call Req.post(client(base_url), url: path, json: body)
    # Same response mapping as get/3
    {:error, :not_implemented}
  end

  @doc """
  Streams a GET response body to `dest_path` on disk.
  Returns `{:ok, bytes_written}` or `{:error, reason}`.

  The response body is never loaded into the BEAM heap: Req writes each
  chunk directly to the file descriptor as it arrives from the socket.
  """
  def stream_to_file(base_url, path, dest_path) do
    # TODO: open dest_path with File.stream!(dest_path, [:write, :binary])
    # TODO: call Req.get(client(base_url), url: path, decode_body: false, into: file_stream)
    # Count bytes written using an Agent accumulator and return {:ok, bytes_written}
    # or {:error, reason} on failure.
    # Clean up (File.rm/1) if the request fails after partially writing the file.
    {:error, :not_implemented}
  end
end
```

### Step 5: Application supervision — `lib/api_gateway/application.ex`

Add Finch and the ETS cache table to the supervision tree:

```elixir
# In ApiGateway.Application.start/2, add to children before the endpoint:
ApiGateway.Upstream.PoolSupervisor

# Also create the ETS cache table during application start (before supervision tree):
:ets.new(:upstream_response_cache, [:named_table, :public, :set, read_concurrency: true])
```

### Step 6: Config — `config/config.exs`

```elixir
config :api_gateway, :upstreams, [
  billing:  [base_url: "https://billing.internal",  protocol: :http1, pool_size: 20, pool_count: 2],
  auth:     [base_url: "https://auth.internal",     protocol: :http2, pool_size: 5,  pool_count: 1],
  catalog:  [base_url: "https://catalog.internal",  protocol: :http1, pool_size: 10, pool_count: 2]
]
```

### Step 7: Given tests — must pass without modification

```elixir
# test/api_gateway/upstream/client_test.exs
defmodule ApiGateway.Upstream.ClientTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Upstream.Client

  # Req.Test stubs intercept Finch calls in tests — no real HTTP needed.
  # See https://hexdocs.pm/req/Req.Test.html

  setup do
    Req.Test.stub(ApiGateway.Upstream.Finch, fn conn ->
      case {conn.method, conn.request_path} do
        {"GET", "/health"} ->
          Plug.Conn.send_resp(conn, 200, Jason.encode!(%{status: "ok"}))

        {"GET", "/missing"} ->
          Plug.Conn.send_resp(conn, 404, Jason.encode!(%{error: "not found"}))

        {"POST", "/echo"} ->
          {:ok, body, conn} = Plug.Conn.read_body(conn)
          Plug.Conn.send_resp(conn, 201, body)

        {"GET", "/error"} ->
          Plug.Conn.send_resp(conn, 503, Jason.encode!(%{error: "service unavailable"}))
      end
    end)

    :ok
  end

  test "get/3 returns {:ok, body} on 200" do
    assert {:ok, %{"status" => "ok"}} =
             Client.get("https://billing.internal", "/health")
  end

  test "get/3 returns {:error, :not_found} on 404" do
    assert {:error, :not_found} =
             Client.get("https://billing.internal", "/missing")
  end

  test "get/3 returns {:error, {:upstream_error, 503, _body}} on 503" do
    assert {:error, {:upstream_error, 503, _body}} =
             Client.get("https://billing.internal", "/error")
  end

  test "post/4 returns {:ok, body} on 2xx" do
    payload = %{event: "test", value: 42}

    assert {:ok, received} = Client.post("https://billing.internal", "/echo", payload)
    assert received["event"] == "test"
    assert received["value"] == 42
  end
end
```

### Step 8: Run the tests

```bash
mix test test/api_gateway/upstream/ --trace
```

---

## Trade-off analysis

| Aspect | Req + Finch (named pools) | Hackney (global pool) | httpc (no pool) |
|--------|--------------------------|-----------------------|-----------------|
| Per-upstream pool isolation | Yes — each service has its own pool | No — shared global pool | No pools |
| HTTP/2 multiplexing | Yes (`protocol: :http2`) | Limited | No |
| Retry middleware | Built-in (`retry: :transient`) | Manual | Manual |
| Test stubbing | `Req.Test.stub/2` | Bypass or mock | Bypass or mock |
| Connection leak on crash | Pool supervised — connections cleaned up | Risk | Risk |
| Backpressure on pool exhaustion | Queue + timeout | Queue + timeout | Unbounded |
| Config per host | Yes (pools map) | Limited | No |

Reflection question: `Client.stream_to_file/3` writes response chunks directly
to disk as they arrive. The gateway process that owns the download call is blocked
on the Finch receive loop until the download completes. For a 10GB file over a
slow upstream, this process is tied up for minutes. What are the implications for
the gateway's supervisor? How would you restructure the download to avoid blocking
the caller process while maintaining back-pressure on the Finch pool?

---

## Common production mistakes

**1. One global Finch instance for all upstreams**
If all upstream calls share one pool and a slow upstream exhausts it, every other
upstream call queues behind it. Pool isolation is the entire point of named Finch
instances. One instance per upstream base URL.

**2. `retry: :transient` on POST without an idempotency key**
`retry: :transient` retries on 503. If a POST creates a resource and the upstream
returns 503 after committing (before responding), a retry creates a duplicate.
Always include an idempotency key in POST headers when enabling retry.

**3. Not propagating `X-Request-ID` downstream**
Without request ID propagation, a failed upstream call is invisible in distributed
traces. The gateway logs show `503 from billing` but the billing service logs show
nothing — the request IDs do not match. Always propagate.

**4. Using `receive_timeout` as wall-clock SLA**
`receive_timeout` is the timeout for receiving the first byte of the response.
A slow upstream that streams a 10GB response at 1KB/s will not hit
`receive_timeout` — each chunk arrives quickly. Use stream chunk timeouts
or implement an overall transfer deadline with a `Task` and `Process.exit/2`.

**5. Starting Finch outside the supervision tree**
If you start Finch in `Application.start/2` with `Finch.start_link/1` instead of
adding it as a supervised child, it is not restarted when it crashes. Pool workers
that fail due to TLS errors or network partitions are never recovered.

---

## Resources

- [Req](https://hexdocs.pm/req) — request struct, steps, retry, streaming, Req.Test
- [Finch](https://hexdocs.pm/finch) — pool configuration, HTTP/2, telemetry events
- [Req.Test — HTTP stubs](https://hexdocs.pm/req/Req.Test.html) — stub/2, allow/3 for async tests
- [Finch Telemetry](https://hexdocs.pm/finch/Finch.html#module-telemetry) — request/connect/send/recv events
- [RFC 7540 — HTTP/2 Multiplexing](https://httpwg.org/specs/rfc7540.html#StreamsLayer) — why `count: 1` for HTTP/2 pools
