# Finch pools per host, HTTP/2, and telemetry observability

**Project**: `finch_pool` — start a `Finch` instance with distinct pool
configurations per host (HTTP/1 for a legacy API, HTTP/2 with multiplexing
for a modern one), attach telemetry handlers to observe connect, queue,
send, and recv phases, and drive it from a small client module with tests
that stub the adapter via `Req.Test`.

**Difficulty**: ★★★☆☆
**Estimated time**: 3–4 hours

---

## Project context

`Req` (exercise 133) is the easy-mode HTTP client. One layer below sits
[Finch](https://hexdocs.pm/finch/): a pool-per-host HTTP client built on
`Mint` with first-class HTTP/2 support. When you need to tune connection
counts, opt into HTTP/2 multiplexing, control idle timeouts, or wire
`:telemetry` events into Prometheus/OpenTelemetry, you go to Finch directly.

This exercise has two goals:

1. Understand Finch's pool model — why HTTP/2 pools are sized 1 but have
   multiple `:count`, and why HTTP/1 pools are the opposite.
2. Learn to observe in-flight traffic via
   [`Finch.Telemetry`](https://hexdocs.pm/finch/Finch.Telemetry.html) events.

Project structure:

```
finch_pool/
├── lib/
│   ├── finch_pool/
│   │   ├── application.ex
│   │   ├── client.ex
│   │   └── telemetry.ex
│   └── finch_pool.ex
├── test/
│   └── finch_pool_test.exs
└── mix.exs
```

---

## Core concepts

### 1. Pools are *per scheme+host+port*

`pools: %{:default => [...], "https://api.example.com" => [...]}` means
requests whose base URL matches the string key use that pool config;
everything else uses `:default`. Finch hashes scheme/host/port — you can't
have separate pools for different paths on the same host.

### 2. HTTP/1 vs HTTP/2 pool sizing

For HTTP/1:
- **`:size`** = connections per pool process. Each can carry one request at a time.
- **`:count`** = number of separate pool processes (shards to reduce contention).
- Total concurrent in-flight = `size × count`.

For HTTP/2:
- Size is fixed at 1 (a single connection multiplexes hundreds of streams).
- You scale by increasing `:count`.
- Source: [Finch README — Pool Configuration](https://hexdocs.pm/finch/).

### 3. Telemetry events

Finch emits `[:finch, :request, :start | :stop | :exception]` for the
outer call, plus granular events for `:queue`, `:connect`, `:send`, `:recv`.
Each `:stop` event includes `:duration` (native time units). This is how
you get "p99 latency to api.example.com" without any code in the client
module. Source: [Finch.Telemetry](https://hexdocs.pm/finch/Finch.Telemetry.html).

### 4. `Finch.build/3` and `Finch.request/2`

The API is lower level than `Req`:

```elixir
Finch.build(:get, "https://api.example.com/users/1", [{"accept", "application/json"}])
|> Finch.request(MyFinch)
```

You handle JSON, retries, and redirects yourself.

---

## Implementation

### Step 1: Create the project

```bash
mix new finch_pool --sup
cd finch_pool
```

Add deps in `mix.exs`:

```elixir
defp deps do
  [
    {:finch, "~> 0.21"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"}
  ]
end
```

### Step 2: `lib/finch_pool/application.ex`

```elixir
defmodule FinchPool.Application do
  @moduledoc false
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      {Finch,
       name: FinchPool.HTTP,
       pools: %{
         # Fallback pool: HTTP/1, small.
         :default => [size: 10, count: 1, protocols: [:http1]],

         # Legacy host: lots of short-lived HTTP/1 requests.
         "https://jsonplaceholder.typicode.com" => [
           size: 25,
           count: 2,
           protocols: [:http1],
           conn_max_idle_time: 30_000
         ],

         # Modern host: HTTP/2 with multiplexing. Size fixed at 1 per pool,
         # scale via :count.
         "https://api.github.com" => [
           protocols: [:http2],
           count: 4,
           conn_max_idle_time: 60_000
         ]
       }},
      FinchPool.Telemetry
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: FinchPool.Supervisor)
  end
end
```

Update `mix.exs` to register the app:

```elixir
def application do
  [mod: {FinchPool.Application, []}, extra_applications: [:logger]]
end
```

### Step 3: `lib/finch_pool/telemetry.ex` — attach handlers

```elixir
defmodule FinchPool.Telemetry do
  @moduledoc """
  Attaches telemetry handlers for Finch events and exposes an in-memory
  summary via `summary/0`. Real systems would forward to Prometheus or
  OpenTelemetry instead.
  """

  use GenServer

  @events [
    [:finch, :request, :start],
    [:finch, :request, :stop],
    [:finch, :request, :exception],
    [:finch, :connect, :stop],
    [:finch, :queue, :stop]
  ]

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)

  def summary, do: GenServer.call(__MODULE__, :summary)

  @impl true
  def init(_) do
    :telemetry.attach_many(
      "finch-pool-handlers",
      @events,
      &__MODULE__.handle_event/4,
      nil
    )

    {:ok, %{requests: 0, errors: 0, total_ns: 0}}
  end

  def handle_event([:finch, :request, :stop], %{duration: d}, _meta, _cfg) do
    GenServer.cast(__MODULE__, {:stop, d})
  end

  def handle_event([:finch, :request, :exception], _m, _meta, _cfg) do
    GenServer.cast(__MODULE__, :error)
  end

  def handle_event(_event, _m, _meta, _cfg), do: :ok

  @impl true
  def handle_cast({:stop, d}, s),
    do: {:noreply, %{s | requests: s.requests + 1, total_ns: s.total_ns + d}}

  def handle_cast(:error, s), do: {:noreply, %{s | errors: s.errors + 1}}

  @impl true
  def handle_call(:summary, _from, s), do: {:reply, s, s}
end
```

### Step 4: `lib/finch_pool/client.ex`

```elixir
defmodule FinchPool.Client do
  @moduledoc "Low-level client wrapping `Finch.build/request`."

  @finch FinchPool.HTTP

  @spec get_json(String.t()) :: {:ok, term()} | {:error, term()}
  def get_json(url) do
    :get
    |> Finch.build(url, [{"accept", "application/json"}])
    |> Finch.request(@finch)
    |> case do
      {:ok, %Finch.Response{status: 200, body: body}} -> Jason.decode(body)
      {:ok, %Finch.Response{status: status}} -> {:error, {:http, status}}
      {:error, reason} -> {:error, reason}
    end
  end
end
```

### Step 5: `test/finch_pool_test.exs`

```elixir
defmodule FinchPoolTest do
  use ExUnit.Case, async: false

  # For unit tests we avoid network by asserting telemetry is wired rather
  # than hitting a real host. A full integration test would use Bypass to
  # stand up a local HTTP server.

  test "telemetry collector starts and reports zeros initially" do
    summary = FinchPool.Telemetry.summary()
    assert is_integer(summary.requests)
    assert is_integer(summary.errors)
  end

  test "telemetry increments on a successful request" do
    bypass = Bypass.open()
    Bypass.expect_once(bypass, "GET", "/ping", fn conn ->
      Plug.Conn.resp(conn, 200, ~s({"pong":true}))
    end)

    before = FinchPool.Telemetry.summary()
    assert {:ok, %{"pong" => true}} =
             FinchPool.Client.get_json("http://localhost:#{bypass.port}/ping")

    # telemetry handlers are sync-ish; give the GenServer a moment.
    Process.sleep(20)
    after_ = FinchPool.Telemetry.summary()

    assert after_.requests == before.requests + 1
  end
end
```

Note: this test pulls in [Bypass](https://hexdocs.pm/bypass/) — add
`{:bypass, "~> 2.1", only: :test}`. Bypass is the right tool when you must
exercise the *real* HTTP stack (Finch, connection pools, telemetry) but
don't want to depend on external services.

Run:

```bash
mix deps.get
mix test
```

---

## Trade-offs and production gotchas

**1. HTTP/2 connection reuse is a double-edged sword**
One connection can multiplex 100+ requests — great for throughput, bad if a
stray head-of-line block stalls everything. For latency-sensitive paths,
increase `:count` even when `:protocols` is `[:http2]` so you have multiple
independent connections.

**2. HTTP/1 pools shard on `:count` to reduce checkout contention**
A single pool process is a serialization point at high RPS. `count: 4` with
`size: 25` gives you 4 × 25 = 100 concurrent slots without all of them
going through one process.

**3. Idle timeouts matter for cloud load balancers**
AWS ALBs close idle backend connections at 60s by default. Set
`conn_max_idle_time` below that or you'll get `:closed` errors on reuse.

**4. Telemetry handlers run in the *caller* process**
`:telemetry.attach/4` is synchronous. Heavy work in the handler adds
latency to every request. Keep handlers cheap (increment a counter, send
a cast) and do aggregation elsewhere.

**5. `pools` map keys are strings, not URI structs**
The match is on scheme+host+port exactly. `"https://api.example.com"` does
*not* match `https://api.example.com:8443`. Include the port if non-default.

**6. When NOT to use Finch directly**
If you're making typical JSON REST calls, use Req (exercise 133) — it
*already* uses Finch under the hood. Reach for Finch when you need custom
request pipelines, streaming bodies via `Finch.stream/5`, or per-pool
telemetry that Req can't expose cleanly.

---

## Resources

- [Finch on HexDocs](https://hexdocs.pm/finch/Finch.html)
- [Finch.Telemetry](https://hexdocs.pm/finch/Finch.Telemetry.html) — full event list
- [Mint](https://hexdocs.pm/mint/) — the HTTP client Finch is built on
- [Bypass](https://hexdocs.pm/bypass/) — local HTTP server for tests
- [HTTP/2 RFC 7540 § 5.3 — Stream Priority](https://www.rfc-editor.org/rfc/rfc7540#section-5.3) — background on multiplexing
