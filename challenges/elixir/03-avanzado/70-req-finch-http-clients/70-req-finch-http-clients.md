# Req + Finch — HTTP Clients for Upstream Calls

## Project context

You are building `api_gateway`, an internal HTTP gateway. The gateway proxies requests to upstream services. Until now, upstream calls had no connection pooling, no retry, and no timeout enforcement. This exercise builds the upstream HTTP client with named Finch pools, Req middleware for retry and telemetry, and streaming support. All modules are defined from scratch.

Project structure:

```
api_gateway/
├── lib/
│   └── api_gateway/
│       └── upstream/
│           ├── client.ex           # HTTP client with retry and telemetry
│           ├── req_steps.ex        # custom Req middleware steps
│           └── pool_supervisor.ex  # Finch pool configuration
├── test/
│   └── api_gateway/
│       └── upstream/
│           └── client_test.exs     # given tests — must pass without modification
└── mix.exs
```

---

## The business problem

The upstream client needs to satisfy four requirements:

1. **Connection pooling**: limit TCP connections per upstream host so the OS does not exhaust file descriptors under burst load.
2. **Retry with backoff**: transient upstream errors (502, 503, 504) should be retried automatically with exponential backoff.
3. **Request tracing**: every upstream call must emit a telemetry event with duration, status code, and upstream host.
4. **Streaming**: some upstreams return large response bodies. The client must stream the response to disk without loading the full body into the BEAM process heap.

---

## Why Finch manages pools instead of letting Hackney or httpc do it

`httpc` creates a new TCP connection for every request unless you configure persistent connections manually. `hackney` has a global pool shared across all callers. Finch is designed around named pools: you start one `{Finch, name: ..., pools: ...}` per upstream domain with explicit `size` and `count` parameters. The pool is supervised independently. If one upstream's pool is exhausted, other upstream requests continue unaffected.

---

## Why retry belongs in a Req step and not in the caller

If the caller handles retries, every call site duplicates the retry logic with potentially different strategies. A Req step centralizes the retry policy. The caller calls `Client.get/1` and receives either a successful response or an error. The retry strategy is declared once.

---

## Implementation

### Step 1: `mix.exs` additions

```elixir
defp deps do
  [
    {:req, "~> 0.5"},
    {:finch, "~> 0.19"},
    {:jason, "~> 1.4"}
  ]
end
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

  @spec child_spec(keyword()) :: Supervisor.child_spec()
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
  (to short-circuit the pipeline).
  """

  require Logger

  @doc """
  Telemetry step: emits [:api_gateway, :upstream, :request, :start/:stop]
  events around every upstream HTTP request.
  """
  @spec telemetry(Req.Request.t()) :: {Req.Request.t(), (({Req.Request.t(), Req.Response.t()}) -> {Req.Request.t(), Req.Response.t()})}
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
  to the upstream call for distributed tracing.
  """
  @spec propagate_request_id(Req.Request.t()) :: Req.Request.t()
  def propagate_request_id(request) do
    request_id =
      Process.get(:request_id) ||
        (:crypto.strong_rand_bytes(8) |> Base.encode16(case: :lower))

    Req.Request.put_header(request, "x-request-id", request_id)
  end

  @doc """
  ETS response cache step: caches GET responses for configurable TTL.
  Cache key is the full URI string. Only 200 responses are cached.
  """
  @spec ets_cache(Req.Request.t()) :: Req.Request.t() | {Req.Request.t(), Req.Response.t()}
  def ets_cache(request) do
    if request.method == :get do
      key = URI.to_string(request.url)

      case :ets.lookup(:upstream_response_cache, key) do
        [{^key, cached_response, expires_at}]
        when expires_at > System.monotonic_time(:second) ->
          {Req.Request.halt(request), cached_response}

        _ ->
          {request,
           fn {req, resp} ->
             if resp.status == 200 do
               ttl = Map.get(req.options, :cache_ttl, 30)
               expires = System.monotonic_time(:second) + ttl
               :ets.insert(:upstream_response_cache, {key, resp, expires})
             end

             {req, resp}
           end}
      end
    else
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
  """

  alias ApiGateway.Upstream.ReqSteps

  @connect_timeout_ms 5_000
  @receive_timeout_ms 30_000
  @max_retries 3

  defp client(base_url) do
    Req.new(
      base_url: base_url,
      finch: ApiGateway.Upstream.Finch,
      connect_options: [timeout: @connect_timeout_ms],
      receive_timeout: @receive_timeout_ms,
      retry: :transient,
      max_retries: @max_retries,
      retry_delay: &backoff/1
    )
    |> Req.Request.prepend_request_steps(
      telemetry: &ReqSteps.telemetry/1,
      request_id: &ReqSteps.propagate_request_id/1
    )
  end

  # Backoff: 500ms -> 1s -> 2s
  defp backoff(attempt) do
    :timer.seconds(1) * Integer.pow(2, attempt - 1) |> div(2)
  end

  @doc """
  Makes a GET request to `path` on the given upstream `base_url`.
  Returns `{:ok, body}` on HTTP 2xx or `{:error, reason}` otherwise.
  """
  @spec get(String.t(), String.t(), keyword()) :: {:ok, term()} | {:error, term()}
  def get(base_url, path, opts \\ []) do
    case Req.get(client(base_url), url: path, params: Keyword.get(opts, :params, [])) do
      {:ok, %{status: status, body: body}} when status in 200..299 ->
        {:ok, body}

      {:ok, %{status: 404}} ->
        {:error, :not_found}

      {:ok, %{status: status, body: body}} ->
        {:error, {:upstream_error, status, body}}

      {:error, exception} ->
        {:error, {:network, exception}}
    end
  end

  @doc """
  Makes a POST request with a JSON body.
  Returns `{:ok, body}` on HTTP 2xx or `{:error, reason}`.
  """
  @spec post(String.t(), String.t(), map(), keyword()) :: {:ok, term()} | {:error, term()}
  def post(base_url, path, body, opts \\ []) do
    case Req.post(client(base_url), url: path, json: body) do
      {:ok, %{status: status, body: resp_body}} when status in 200..299 ->
        {:ok, resp_body}

      {:ok, %{status: 404}} ->
        {:error, :not_found}

      {:ok, %{status: status, body: resp_body}} ->
        {:error, {:upstream_error, status, resp_body}}

      {:error, exception} ->
        {:error, {:network, exception}}
    end
  end

  @doc """
  Streams a GET response body to `dest_path` on disk.
  Returns `{:ok, bytes_written}` or `{:error, reason}`.

  The response body is never loaded into the BEAM heap: Req writes each
  chunk directly to the file descriptor as it arrives from the socket.
  """
  @spec stream_to_file(String.t(), String.t(), String.t()) ::
          {:ok, non_neg_integer()} | {:error, term()}
  def stream_to_file(base_url, path, dest_path) do
    file_stream = File.stream!(dest_path, [:write, :binary])

    case Req.get(client(base_url), url: path, decode_body: false, into: file_stream) do
      {:ok, %{status: status}} when status in 200..299 ->
        %{size: bytes} = File.stat!(dest_path)
        {:ok, bytes}

      {:ok, %{status: status, body: body}} ->
        File.rm(dest_path)
        {:error, {:upstream_error, status, body}}

      {:error, exception} ->
        File.rm(dest_path)
        {:error, {:network, exception}}
    end
  end
end
```

### Step 5: Config — `config/config.exs`

```elixir
config :api_gateway, :upstreams, [
  billing:  [base_url: "https://billing.internal",  protocol: :http1, pool_size: 20, pool_count: 2],
  auth:     [base_url: "https://auth.internal",     protocol: :http2, pool_size: 5,  pool_count: 1],
  catalog:  [base_url: "https://catalog.internal",  protocol: :http1, pool_size: 10, pool_count: 2]
]
```

### Step 6: Given tests — must pass without modification

```elixir
# test/api_gateway/upstream/client_test.exs
defmodule ApiGateway.Upstream.ClientTest do
  use ExUnit.Case, async: true

  alias ApiGateway.Upstream.Client

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

### Step 7: Run the tests

```bash
mix test test/api_gateway/upstream/ --trace
```

---

## Trade-off analysis

| Aspect | Req + Finch (named pools) | Hackney (global pool) | httpc (no pool) |
|--------|--------------------------|-----------------------|-----------------|
| Per-upstream pool isolation | Yes | No — shared global pool | No pools |
| HTTP/2 multiplexing | Yes (`protocol: :http2`) | Limited | No |
| Retry middleware | Built-in (`retry: :transient`) | Manual | Manual |
| Test stubbing | `Req.Test.stub/2` | Bypass or mock | Bypass or mock |
| Connection leak on crash | Pool supervised — cleaned up | Risk | Risk |
| Config per host | Yes (pools map) | Limited | No |

Reflection question: `Client.stream_to_file/3` writes response chunks directly to disk. The gateway process is blocked on the Finch receive loop until the download completes. For a 10GB file over a slow upstream, this process is tied up for minutes. How would you restructure the download to avoid blocking the caller?

---

## Common production mistakes

**1. One global Finch instance for all upstreams**
If all upstream calls share one pool and a slow upstream exhausts it, every other upstream call queues behind it. One pool per upstream base URL.

**2. `retry: :transient` on POST without an idempotency key**
A retry on 503 after the upstream committed but before responding creates a duplicate. Always include an idempotency key in POST headers when enabling retry.

**3. Not propagating `X-Request-ID` downstream**
Without request ID propagation, a failed upstream call is invisible in distributed traces.

**4. Using `receive_timeout` as wall-clock SLA**
`receive_timeout` is for receiving the first byte. A slow stream will not trigger it.

**5. Starting Finch outside the supervision tree**
If started with `Finch.start_link/1` instead of as a supervised child, pool workers that fail are never recovered.

---

## Resources

- [Req](https://hexdocs.pm/req) — request struct, steps, retry, streaming, Req.Test
- [Finch](https://hexdocs.pm/finch) — pool configuration, HTTP/2, telemetry events
- [Req.Test — HTTP stubs](https://hexdocs.pm/req/Req.Test.html) — stub/2, allow/3 for async tests
- [Finch Telemetry](https://hexdocs.pm/finch/Finch.html#module-telemetry) — request/connect/send/recv events
