# Req / Finch — HTTP Clients Deep Dive

**Project**: `http_clients_deep` — Req on top of Finch with named pools, HTTP/2 multiplexing, structured telemetry, and streaming downloads.

**Difficulty**: ★★★★☆
**Estimated time**: 4–6 hours

---

## Project context

Finch is the production HTTP client for the BEAM — `mint` + `nimble_pool`,
HTTP/1.1 and HTTP/2, per-host connection pools, zero-copy response streaming.
Req sits on top and gives you Plug-like middleware (retry, auth, decompression,
redirect) with an opinionated ergonomic API.

Most teams use Req without ever tuning Finch. That works until you hit one of:
a slow upstream starving the default pool, an HTTP/2 upstream where you expect
multiplexing but get one-request-per-connection latency, a large download that
loads the whole body in memory. This exercise builds five realistic
configurations and measures them — you leave knowing which knobs matter and
which are cargo cult.

```
http_clients_deep/
├── lib/
│   └── http_clients_deep/
│       ├── application.ex
│       ├── pools.ex                   # named Finch pools
│       ├── client.ex                  # Req-based client
│       ├── stream.ex                  # streaming download
│       └── telemetry_reporter.ex      # attaches handlers
├── test/
│   └── http_clients_deep/
│       ├── client_test.exs
│       └── stream_test.exs
├── bench/
│   └── pool_bench.exs
└── mix.exs
```

---

## Core concepts

### 1. The Finch layering

```
┌────────────────────────────────────────────────┐
│ Req         middleware (retry, auth, redirect) │
├────────────────────────────────────────────────┤
│ Finch       request dispatch, pool selection   │
├────────────────────────────────────────────────┤
│ NimblePool  checkout/checkin of connections    │
├────────────────────────────────────────────────┤
│ Mint        stateless HTTP/1.1 & HTTP/2 client │
└────────────────────────────────────────────────┘
```

- **Mint** is stateless: given a socket, encode/decode HTTP. It does not own
  sockets or pools.
- **NimblePool** is a generic resource pool — same pattern Broadway uses.
- **Finch** composes the two: one named pool per `{scheme, host, port}`
  with configurable size, connection timeout, and protocol.
- **Req** wraps Finch with middleware and an HTTP-verb API.

### 2. HTTP/2 multiplexing — why `count: 1` is often correct

Under HTTP/1.1, each connection serves one request at a time. `count: 50`
means 50 concurrent requests max per host.

Under HTTP/2, a single connection serves many concurrent streams (default
100). `count: 50` means you have 50 * 100 = 5,000 concurrent streams — almost
certainly far more than you need, wasting file descriptors and TLS handshakes.

For HTTP/2 upstreams (most modern APIs — Stripe, Google, GitHub), prefer
`protocols: [:http2], count: 1, conn_opts: [transport_opts: [keepalive: true]]`.

### 3. Pool-per-host, not pool-per-app

One global pool routed across every upstream means a slow partner starves
calls to your critical providers. Named pools per upstream decorrelate them:

```elixir
{Finch,
 name: MyFinch,
 pools: %{
   "https://api.stripe.com"  => [size: 25, protocols: [:http2], count: 1],
   "https://api.adyen.com"   => [size: 25, protocols: [:http2], count: 1],
   "https://legacy.bank.net" => [size: 10, protocols: [:http1], count: 10]
 }}
```

Finch matches the destination URL against these keys and picks the pool.

### 4. Streaming vs buffered

`Req.get!(url)` loads the full response into memory. For a 2GB CSV this is
fatal. `Finch.stream/4` hands you chunks as they arrive so you can pipe them
to disk or a parser:

```
Mint TCP ─▶ Finch.stream callback ─▶ chunk 1 ─▶ handler
                                  └─▶ chunk 2 ─▶ handler
                                  └─▶ chunk N ─▶ :done
```

The handler accumulates, writes to file, or pipes to `File.stream!/1` without
ever materializing the whole body.

### 5. Telemetry — what Finch already emits

Finch emits `[:finch, :request, :start]`, `[:finch, :request, :stop]`,
`[:finch, :request, :exception]`, plus `[:finch, :queue, :start]/:stop`
(how long checkout waits), `[:finch, :connect, :start]/:stop`, and
`[:finch, :recv, :start]/:stop`. Attaching handlers to these gives you
checkout queue time (the #1 symptom of an undersized pool) for free.

---

## Implementation

### Step 1: mix.exs

```elixir
defp deps do
  [
    {:finch, "~> 0.18"},
    {:req, "~> 0.5"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"},
    {:bypass, "~> 2.1", only: :test}
  ]
end
```

### Step 2: `lib/http_clients_deep/pools.ex`

```elixir
defmodule HttpClientsDeep.Pools do
  @moduledoc """
  Finch pool specification. Keyed by normalized scheme://host[:port].

  Pool sizing rules of thumb:

    * HTTP/2 upstreams: `count: 1, size: 25` (25 multiplexed connections total).
    * HTTP/1.1 upstreams: `count: N, size: N` where N ≈ p99 concurrency you expect.
    * size is the MAX; NimblePool lazily creates connections as demand rises.
  """

  @spec child_spec(keyword()) :: Supervisor.child_spec()
  def child_spec(opts) do
    name = Keyword.get(opts, :name, HttpClientsDeep.Finch)

    {Finch,
     name: name,
     pools: %{
       :default => [size: 25, protocols: [:http1], count: 1],
       "https://api.stripe.com" => [
         size: 25,
         protocols: [:http2],
         count: 1,
         conn_opts: [transport_opts: [timeout: 5_000]]
       ],
       "https://legacy.bank.net" => [size: 10, protocols: [:http1], count: 10]
     }}
  end
end
```

### Step 3: `lib/http_clients_deep/client.ex`

```elixir
defmodule HttpClientsDeep.Client do
  @moduledoc """
  Req-based client that routes through the named Finch instance, with retry,
  redirect, decompression, and default timeouts wired in.
  """

  @finch HttpClientsDeep.Finch

  @spec request(Req.Request.method(), String.t(), keyword()) ::
          {:ok, Req.Response.t()} | {:error, Exception.t()}
  def request(method, url, opts \\ []) do
    req =
      Req.new(
        method: method,
        url: url,
        finch: @finch,
        receive_timeout: Keyword.get(opts, :receive_timeout, 15_000),
        connect_options: [timeout: Keyword.get(opts, :connect_timeout, 5_000)],
        retry: :safe_transient,
        max_retries: 3,
        retry_delay: fn n -> (:rand.uniform(100) + round(:math.pow(2, n) * 100)) end
      )

    Req.request(req, opts)
  end

  def get(url, opts \\ []), do: request(:get, url, opts)
  def post(url, opts \\ []), do: request(:post, url, opts)
end
```

### Step 4: Streaming download — `lib/http_clients_deep/stream.ex`

```elixir
defmodule HttpClientsDeep.Stream do
  @moduledoc """
  Stream a response body to disk without buffering in memory. Safe for
  multi-GB files.
  """

  @spec download(String.t(), Path.t()) :: {:ok, pos_integer()} | {:error, term()}
  def download(url, path) do
    req = Finch.build(:get, url)
    {:ok, file} = File.open(path, [:write, :binary, :raw])

    try do
      case Finch.stream(req, HttpClientsDeep.Finch, 0, &stream_handler(&1, &2, file)) do
        {:ok, bytes_written} -> {:ok, bytes_written}
        {:error, _} = err -> err
      end
    after
      File.close(file)
    end
  end

  defp stream_handler({:status, status}, _acc, _file) when status not in 200..299,
    do: raise("unexpected status #{status}")

  defp stream_handler({:status, _status}, acc, _file), do: acc
  defp stream_handler({:headers, _headers}, acc, _file), do: acc

  defp stream_handler({:data, chunk}, acc, file) do
    :ok = :file.write(file, chunk)
    acc + byte_size(chunk)
  end
end
```

### Step 5: Telemetry reporter

```elixir
defmodule HttpClientsDeep.TelemetryReporter do
  require Logger

  @events [
    [:finch, :request, :stop],
    [:finch, :queue, :stop],
    [:finch, :connect, :stop]
  ]

  def attach do
    :telemetry.attach_many(
      "http-clients-deep",
      @events,
      &__MODULE__.handle/4,
      nil
    )
  end

  def handle([:finch, :request, :stop], %{duration: d}, meta, _cfg) do
    Logger.info(
      "finch req done host=#{meta.request.host} status=#{inspect(meta.result)} " <>
        "dur_ms=#{System.convert_time_unit(d, :native, :millisecond)}"
    )
  end

  def handle([:finch, :queue, :stop], %{duration: d}, meta, _cfg) do
    ms = System.convert_time_unit(d, :native, :millisecond)

    if ms > 50 do
      Logger.warning(
        "finch pool checkout slow host=#{meta.pool} wait_ms=#{ms} — pool likely undersized"
      )
    end
  end

  def handle([:finch, :connect, :stop], %{duration: d}, meta, _cfg) do
    Logger.debug(
      "finch connected host=#{meta.host} dur_ms=" <>
        "#{System.convert_time_unit(d, :native, :millisecond)}"
    )
  end
end
```

### Step 6: `lib/http_clients_deep/application.ex`

```elixir
defmodule HttpClientsDeep.Application do
  use Application

  @impl true
  def start(_type, _args) do
    HttpClientsDeep.TelemetryReporter.attach()

    children = [
      HttpClientsDeep.Pools
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: HttpClientsDeep.Supervisor)
  end
end
```

### Step 7: Tests with Bypass

```elixir
defmodule HttpClientsDeep.ClientTest do
  use ExUnit.Case, async: false
  alias HttpClientsDeep.Client

  setup do
    bypass = Bypass.open()
    %{bypass: bypass, url: "http://localhost:#{bypass.port}/api"}
  end

  test "GET 200 returns body", %{bypass: bypass, url: url} do
    Bypass.expect(bypass, "GET", "/api", fn conn ->
      Plug.Conn.resp(conn, 200, ~s({"ok": true}))
    end)

    assert {:ok, %Req.Response{status: 200, body: %{"ok" => true}}} = Client.get(url)
  end

  test "503 triggers safe_transient retry", %{bypass: bypass, url: url} do
    counter = :counters.new(1, [])

    Bypass.expect(bypass, "GET", "/api", fn conn ->
      :counters.add(counter, 1, 1)

      if :counters.get(counter, 1) < 3 do
        Plug.Conn.resp(conn, 503, "")
      else
        Plug.Conn.resp(conn, 200, "ok")
      end
    end)

    assert {:ok, %Req.Response{status: 200}} = Client.get(url)
    assert :counters.get(counter, 1) == 3
  end

  test "connect_timeout trips on unreachable host" do
    assert {:error, %Mint.TransportError{reason: _}} =
             Client.get("http://10.255.255.1:81/never", connect_timeout: 50)
  end
end

defmodule HttpClientsDeep.StreamTest do
  use ExUnit.Case, async: false
  alias HttpClientsDeep.Stream, as: DStream

  setup do
    bypass = Bypass.open()
    path = Path.join(System.tmp_dir!(), "http_clients_deep_stream_test.bin")
    on_exit(fn -> File.rm(path) end)
    %{bypass: bypass, path: path, url: "http://localhost:#{bypass.port}/file"}
  end

  test "writes chunked body to file", %{bypass: bypass, url: url, path: path} do
    payload = :crypto.strong_rand_bytes(1_000_000)

    Bypass.expect(bypass, "GET", "/file", fn conn ->
      conn = Plug.Conn.send_chunked(conn, 200)

      Enum.reduce_while(:binary.bin_to_list(payload) |> Enum.chunk_every(8192), conn, fn
        chunk, conn ->
          case Plug.Conn.chunk(conn, :binary.list_to_bin(chunk)) do
            {:ok, conn} -> {:cont, conn}
            _ -> {:halt, conn}
          end
      end)
    end)

    assert {:ok, bytes} = DStream.download(url, path)
    assert bytes == byte_size(payload)
    assert File.read!(path) == payload
  end
end
```

### Step 8: Pool benchmark

```elixir
# bench/pool_bench.exs
alias HttpClientsDeep.Client

# Assumes HttpClientsDeep started and an Nginx at localhost:8080 returning 200.
urls = for _ <- 1..1_000, do: "http://localhost:8080/hello"

Benchee.run(
  %{
    "sequential" => fn ->
      Enum.each(urls, fn u -> Client.get(u) end)
    end,
    "parallel 25" => fn ->
      urls
      |> Task.async_stream(&Client.get/1, max_concurrency: 25)
      |> Stream.run()
    end,
    "parallel 200" => fn ->
      urls
      |> Task.async_stream(&Client.get/1, max_concurrency: 200)
      |> Stream.run()
    end
  },
  time: 10,
  memory_time: 2
)
```

Watch the `finch.queue.stop` logs. If `wait_ms` consistently exceeds 50ms at
`parallel 200` but is near-zero at `parallel 25`, the pool is the bottleneck —
raise `size` or increase `count` (HTTP/1.1) until queue time drops.

---

## Trade-offs and production gotchas

**1. HTTP/2 with `count: 10` is almost always wrong.** You end up with 10 TLS
handshakes where 1 would suffice, burning CPU and warm-up latency. Use
`count: 1, size: <multiplexing factor>` unless you measured otherwise.

**2. Default pool (`:default`) is a trap.** Any URL not matched by a named
pool key falls through. In production we've seen a typo in the key cause all
traffic to a 10-connection default pool, capping throughput at 10 rps.

**3. `receive_timeout` vs `connect_options[:timeout]`.** Different knobs:
connect is the TCP + TLS handshake, receive is between bytes of the response.
Default receive is 15s; for idempotent requests behind a breaker, set it
shorter (2–3s).

**4. Req's `retry: :safe_transient` only retries idempotent methods.** POSTs
are not retried by default — that's correct for most cases. If you need to
retry POSTs (with an `Idempotency-Key` header), pass
`retry: :transient` and own the idempotency guarantee yourself.

**5. Streaming and back-pressure.** `Finch.stream/4`'s callback runs in the
calling process. If the callback is slow, the TCP socket stops reading and
the upstream slows down (back-pressure is automatic). Don't spawn the
callback's work — let it block so back-pressure works.

**6. Decompression in Req eats CPU.** Req auto-decodes gzip/br. For binary
downloads you've already committed to stream raw, pass `decode_body: false`
to keep the raw bytes on the wire.

**7. Pool warm-up on deploy.** After a fresh deploy, the first N requests pay
the TLS handshake latency. For hot paths, warm the pool in application start
with a few concurrent HEAD requests to the critical upstreams.

**8. When NOT to use Req.** For binary transports (gRPC, MQTT), or when you
need custom frame-level control (WebSockets — use Mint.WebSocket), Req is the
wrong layer. For high-frequency internal RPC inside a cluster, skip HTTP
entirely and use `:erpc` or a cluster-local GenServer protocol.

---

## Performance notes

On a 2023 M2 Pro against a local Nginx returning `200 OK` (empty body):

| Config | RPS (single process) | RPS (200 parallel tasks) |
|--------|----------------------|--------------------------|
| HTTP/1.1 size=25 count=1 | ~1,200 | ~11,000 |
| HTTP/1.1 size=25 count=10 | ~1,200 | ~24,000 |
| HTTP/2 size=25 count=1 | ~1,800 | ~32,000 |

Numbers are illustrative — always measure against your own upstream.
Key insight: HTTP/2 multiplexing + `count: 1` beats HTTP/1.1 `count: 10` once
you have any concurrency at all.

---

## Resources

- [Finch hexdocs](https://hexdocs.pm/finch/Finch.html) — pool configuration reference
- [Req hexdocs](https://hexdocs.pm/req/Req.html) — middleware catalog and examples
- [Mint hexdocs](https://hexdocs.pm/mint/Mint.html) — stateless HTTP client underneath
- [NimblePool hexdocs](https://hexdocs.pm/nimble_pool/NimblePool.html) — the pool semantics
- [Dashbit — What's new in Finch 0.5](https://dashbit.co/blog/announcing-finch-0-5) — HTTP/2 rollout details
- [José Valim on Mint/Finch design](https://www.youtube.com/watch?v=ZG3Ip7SLG5Y) — "The Soul of Erlang and Elixir" talk context
- [RFC 7540 — HTTP/2](https://www.rfc-editor.org/rfc/rfc7540) — stream multiplexing semantics
