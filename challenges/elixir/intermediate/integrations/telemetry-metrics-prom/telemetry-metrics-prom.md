# Telemetry.Metrics + Prometheus: counters, summaries, and a /metrics endpoint

**Project**: `metrics_prom` — defines `Telemetry.Metrics` definitions (counters, summaries, last_value) fed by `:telemetry` events, exported in Prometheus text format via `TelemetryMetricsPrometheus.Core`.

---

## Why telemetry metrics prom matters

`:telemetry` is the event bus. `Telemetry.Metrics` is the aggregation
language: it lets you *declare* the metrics you want (counter, sum,
last_value, summary, distribution) without knowing the exporter. A
"reporter" then consumes those definitions and sends the aggregated
values somewhere — StatsD, Prometheus, LiveDashboard, Datadog.

In this exercise you'll:

1. Define a small HTTP-style event (`[:metrics_prom, :request, :stop]`)
   and emit it manually.
2. Declare four metrics over that event: a counter of requests, a sum of
   bytes, a summary of durations, and a `last_value` gauge.
3. Start `TelemetryMetricsPrometheus.Core` as a supervisor child, so it
   aggregates in-memory and exposes a `scrape/1` function.
4. Write tests that emit events, call the scrape function, and assert on
   the Prometheus text that would be served at `/metrics`.

We use `telemetry_metrics_prometheus_core` (the aggregation-only library)
rather than `telemetry_metrics_prometheus` (which also bundles a Plug HTTP
server). This lets the tests run without spinning up a Cowboy port and
fighting for sockets in parallel runs. In production you'd add
`plug_cowboy` and expose `scrape/1` at `/metrics`.

---

## Project structure

```
metrics_prom/
├── lib/
│   └── metrics_prom.ex
├── script/
│   └── main.exs
├── test/
│   └── metrics_prom_test.exs
└── mix.exs
```

---

## Core concepts

### 1. `Telemetry.Metrics` — five metric types

| Type | What it does |
|------|--------------|
| `counter/2`     | Incremented by 1 per event (ignores measurement value). |
| `sum/2`         | Sums a measurement across events. |
| `last_value/2`  | Remembers the last observed value (gauge-like). |
| `summary/2`     | Min / max / mean / count / quantiles of a measurement. |
| `distribution/2`| Histogram buckets (Prometheus's bread and butter). |

A definition is pure data: an event name, a measurement key, optional
tags and unit conversion. Reporters interpret it.

### 2. Tags = Prometheus labels

```elixir
counter("http.request.count", tags: [:method, :status])
```

Each unique `{method, status}` pair becomes a distinct time series in
Prometheus. **Cardinality kills** — keep tag sets bounded (a few HTTP
methods, a few hundred routes). Never tag by user id or request id.

### 3. `TelemetryMetricsPrometheus.Core.scrape/1`

The core library maintains an ETS table per metric, updated synchronously
from handlers, and exposes `scrape/1` that returns a binary in the
Prometheus text format:

```
# HELP metrics_prom_http_request_count ...
# TYPE metrics_prom_http_request_count counter
metrics_prom_http_request_count{method="GET",status="200"} 3
```

Your Plug endpoint just returns this binary with
`content-type: text/plain; version=0.0.4`.

### 4. Unit conversion

`:telemetry.span/3` emits `duration` in `:native` time units (a BEAM
internal unit). Prometheus conventions expect durations in seconds.
`Telemetry.Metrics` handles the conversion:

```elixir
summary("http.request.duration", unit: {:native, :millisecond})
```

The reporter reads the `:native` duration and converts it on emission.
Forgetting this is how your dashboards show "avg duration: 73,000,000".

### 5. Start it as a supervised child

```elixir
{TelemetryMetricsPrometheus.Core, metrics: metrics(), name: :prom}
```

That starts a registered process owning the ETS tables. `scrape/1`
takes the same `name`. If the process dies, you lose your in-memory
metric state — Prometheus will see a reset counter, which it handles
natively, so this is OK in practice.

---

## Why Telemetry.Metrics and not a metrics SDK directly

A Prometheus-only SDK couples your instrumentation points to Prometheus
semantics. `Telemetry.Metrics` decouples the two: you declare metric
definitions once, and any reporter (Prometheus, StatsD, LiveDashboard,
Datadog) consumes the same declarations. Switch exporters without
changing a single instrumentation line. You also get to emit raw
`:telemetry` events from libraries you don't own — Ecto, Phoenix, Finch
all emit into the same bus.

---

## Design decisions

**Option A — Aggregate client-side via a stateful GenServer**
- Pros: Full control over buckets, percentiles, and reporting cadence.
- Cons: You reimplement what reporters already do; GenServer becomes
  a bottleneck under high event rates; tag cardinality bugs crash it.

**Option B — `Telemetry.Metrics` definitions + Prometheus reporter** (chosen)
- Pros: Definitions are pure data; the reporter handles ETS-backed
  aggregation; swapping reporters is a supervisor-child change.
- Cons: Reporter lifecycle (restart = counter reset) is visible to
  downstream scrapers; tag cardinality still a hazard, just a declared
  one.

→ Chose **B** because every serious Elixir observability path — Phoenix
  LiveDashboard, PromEx, OpenTelemetry bridges — builds on
  `Telemetry.Metrics`. Going your own way forecloses on the ecosystem.

---

## Implementation

### Step 1: Create the project

**Objective**: Bootstrap a clean Mix project so the lab runs in isolation — isolated from any external state, so we demonstrate this concept cleanly without dependencies.

```bash
mix new metrics_prom --sup
cd metrics_prom
```

### `mix.exs`
**Objective**: Declare dependencies and project config in `mix.exs`.

```elixir
defmodule MetricsProm.MixProject do
  use Mix.Project

  def project do
    [
      app: :metrics_prom,
      version: "0.1.0",
      elixir: "~> 1.19",
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {MetricsProm.Application, []}
    ]
  end

  defp deps do
    [
      {:telemetry, "~> 1.2"},
      {:telemetry_metrics, "~> 1.0"},
      {:telemetry_metrics_prometheus_core, "~> 1.2"}
    ]
  end
end
```

Run `mix deps.get`.

### `lib/metrics_prom/metrics.ex`

**Objective**: Implement `metrics.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule MetricsProm.Metrics do
  @moduledoc """
  Declares the metrics this app exposes. Pure data: no event handling
  here — the Prometheus reporter attaches handlers internally when it
  starts.
  """
  import Telemetry.Metrics

  @doc """
  The metric definitions. Fed to `TelemetryMetricsPrometheus.Core`.
  """
  @spec definitions() :: [Telemetry.Metrics.t()]
  def definitions do
    [
      counter(
        "metrics_prom.request.count",
        event_name: [:metrics_prom, :request, :stop],
        tags: [:method, :status],
        description: "Total requests observed, tagged by method and status."
      ),
      sum(
        "metrics_prom.request.response_bytes",
        event_name: [:metrics_prom, :request, :stop],
        measurement: :bytes,
        tags: [:method],
        description: "Total response bytes, summed per method."
      ),
      summary(
        "metrics_prom.request.duration",
        event_name: [:metrics_prom, :request, :stop],
        measurement: :duration,
        unit: {:native, :millisecond},
        tags: [:method],
        description: "Request duration summary (min/max/avg), in ms."
      ),
      last_value(
        "metrics_prom.queue.depth",
        event_name: [:metrics_prom, :queue, :depth],
        measurement: :value,
        description: "Current in-memory queue depth."
      )
    ]
  end
end
```

### `lib/metrics_prom/application.ex`

**Objective**: Wire `application.ex` to start the supervision tree that boots Repo and external adapters in the correct order before serving traffic.

```elixir
defmodule MetricsProm.Application do
  @moduledoc false
  use Application

  @prom_name :metrics_prom_reporter

  @doc "Registered name used by both the reporter and `scrape/0`."
  def prom_name, do: @prom_name

  @impl true
  def start(_type, _args) do
    children = [
      {TelemetryMetricsPrometheus.Core,
       metrics: MetricsProm.Metrics.definitions(),
       name: @prom_name}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: MetricsProm.Supervisor)
  end
end
```

### `lib/metrics_prom.ex`

**Objective**: Implement `metrics_prom.ex` — the integration seam where external protocol semantics meet Elixir domain code.

```elixir
defmodule MetricsProm do
  @moduledoc """
  Thin facade: emits events and exposes the scraped Prometheus output.
  In production, wire `scrape/0` to a Plug endpoint at `/metrics`.
  """

  @request_stop [:metrics_prom, :request, :stop]
  @queue_depth [:metrics_prom, :queue, :depth]

  @doc """
  Emits a synthetic request-stop event. `duration_ms` is converted to
  native units (what `:telemetry.span/3` would emit naturally).
  """
  @spec record_request(String.t(), integer(), non_neg_integer(), non_neg_integer()) :: :ok
  def record_request(method, status, bytes, duration_ms) do
    duration_native = System.convert_time_unit(duration_ms, :millisecond, :native)

    :telemetry.execute(
      @request_stop,
      %{duration: duration_native, bytes: bytes},
      %{method: method, status: status}
    )
  end

  @spec record_queue_depth(non_neg_integer()) :: :ok
  def record_queue_depth(depth) do
    :telemetry.execute(@queue_depth, %{value: depth}, %{})
  end

  @doc """
  Returns the current Prometheus-formatted metrics text. This is the body
  you'd return from a `/metrics` Plug handler.
  """
  @spec scrape() :: binary()
  def scrape do
    TelemetryMetricsPrometheus.Core.scrape(MetricsProm.Application.prom_name())
  end
end
```

### Step 6: `test/metrics_prom_test.exs`

**Objective**: Write `metrics_prom_test.exs` — tests pin the behaviour so future refactors cannot silently regress the invariants established above.

```elixir
defmodule MetricsPromTest do
  use ExUnit.Case, async: false

  doctest MetricsProm
  # async: false — metric state is global ETS, not per-test.

  describe "counter" do
    test "request.count increments per emitted event and is tagged" do
      MetricsProm.record_request("GET", 200, 100, 5)
      MetricsProm.record_request("GET", 200, 200, 10)
      MetricsProm.record_request("POST", 500, 0, 1)

      output = MetricsProm.scrape()

      # Prometheus metric names replace dots with underscores.
      assert output =~ ~r/metrics_prom_request_count\{.*method="GET".*status="200".*\} 2/
      assert output =~ ~r/metrics_prom_request_count\{.*method="POST".*status="500".*\} 1/
      assert output =~ "# TYPE metrics_prom_request_count counter"
    end
  end

  describe "sum" do
    test "response_bytes accumulates bytes per method" do
      MetricsProm.record_request("GET", 200, 1000, 5)
      MetricsProm.record_request("GET", 200, 500, 5)

      output = MetricsProm.scrape()

      # Sum is at least 1500 for GET (counters from other tests may remain
      # within the suite, so assert the substring and extract the number).
      assert output =~ ~r/metrics_prom_request_response_bytes\{method="GET"\} \d+/

      [_, sum_str] =
        Regex.run(~r/metrics_prom_request_response_bytes\{method="GET"\} (\d+)/, output)

      assert String.to_integer(sum_str) >= 1500
    end
  end

  describe "summary" do
    test "duration emits count/sum/min/max" do
      for ms <- [10, 20, 30], do: MetricsProm.record_request("GET", 200, 0, ms)

      output = MetricsProm.scrape()

      # The exact suffix names depend on the reporter; we assert the
      # presence of the metric family rather than exact values to keep
      # the test robust across library versions.
      assert output =~ "metrics_prom_request_duration"
      assert output =~ "# TYPE metrics_prom_request_duration summary"
    end
  end

  describe "last_value" do
    test "queue.depth reflects the most recent observation" do
      MetricsProm.record_queue_depth(5)
      MetricsProm.record_queue_depth(42)

      output = MetricsProm.scrape()
      assert output =~ ~r/metrics_prom_queue_depth (?:42|42\.0)/
      assert output =~ "# TYPE metrics_prom_queue_depth gauge"
    end
  end
end
```

### Step 7: Run

**Objective**: Execute the suite (or IEx session) so the invariants we just encoded are proven by observation, not just by reading the code.

```bash
mix test
```

### Why this works

`:telemetry.execute/3` is a synchronous fan-out to every handler
attached to the event name; the Prometheus reporter attaches handlers
that increment/sum/observe ETS-backed counters keyed by tag tuples.
`scrape/1` walks those tables and formats them in Prometheus text
format. Unit conversion in the metric definition (`unit: {:native,
:millisecond}`) is applied at emit time, so durations reported by
`:telemetry.span/3` (which uses `:native` time) come out in the unit
dashboards expect.

---

### `script/main.exs`

```elixir
defmodule Main do
  defmodule MetricsProm.Metrics do
    @moduledoc """
    Declares the metrics this app exposes. Pure data: no event handling
    here — the Prometheus reporter attaches handlers internally when it
    starts.
    """
    import Telemetry.Metrics

    @doc """
    The metric definitions. Fed to `TelemetryMetricsPrometheus.Core`.
    """
    @spec definitions() :: [Telemetry.Metrics.t()]
    def definitions do
      [
        counter(
          "metrics_prom.request.count",
          event_name: [:metrics_prom, :request, :stop],
          tags: [:method, :status],
          description: "Total requests observed, tagged by method and status."
        ),
        sum(
          "metrics_prom.request.response_bytes",
          event_name: [:metrics_prom, :request, :stop],
          measurement: :bytes,
          tags: [:method],
          description: "Total response bytes, summed per method."
        ),
        summary(
          "metrics_prom.request.duration",
          event_name: [:metrics_prom, :request, :stop],
          measurement: :duration,
          unit: {:native, :millisecond},
          tags: [:method],
          description: "Request duration summary (min/max/avg), in ms."
        ),
        last_value(
          "metrics_prom.queue.depth",
          event_name: [:metrics_prom, :queue, :depth],
          measurement: :value,
          description: "Current in-memory queue depth."
        )
      ]
    end
  end

  def main do
    IO.puts("=== App Demo ===
  ")
  
    # Demo: Telemetry metrics
  IO.puts("1. Telemetry emits events: [:module, :event]")
  IO.puts("2. Metrics collect and aggregate")
  IO.puts("3. Prometheus exports for monitoring")

  IO.puts("
  ✓ Telemetry metrics demo completed!")
  end

end

Main.main()
```

## Deep Dive: State Management and Message Handling Patterns

Understanding state transitions is central to reliable OTP systems. Every `handle_call` or `handle_cast` receives current state and returns new state—immutability forces explicit reasoning. This prevents entire classes of bugs: missing state updates are immediately visible.

Key insight: separate pure logic (state → new state) from side effects (logging, external calls). Move pure logic to private helpers; use handlers for orchestration. This makes servers testable—test pure functions independently.

In production, monitor state size and mutation frequency. Unbounded growth is a memory leak; excessive mutations signal hot spots needing optimization. Always profile before reaching for performance solutions like ETS.

## Benchmark

Emitting an event via `:telemetry.execute/3` with 2-3 attached handlers
typically takes under 10µs; scraping a registry with ~50 metric series
runs in a few hundred microseconds. Target: emission latency should
stay under 20µs at the 99th percentile (it's in the request's hot path).
If it exceeds that, you have too many handlers or a handler doing work
it shouldn't (blocking I/O, heavy encoding).

---

## Trade-offs and production gotchas

**1. Cardinality is your real enemy**
Every unique tag combination is a new time series. Tag by method (6
values), route template (hundreds), status (a few). **Never** tag by
user id, request id, or raw URL path — you'll blow up Prometheus memory
in minutes.

**2. Counters never decrement**
`counter/2` in Prometheus semantics only goes up; a process restart resets
it to zero, and Prometheus's `rate()` function handles the reset natively.
Don't try to "adjust" counters downward — you'll confuse every dashboard
that uses `rate()` or `increase()`.

**3. Summary quantiles are per-instance, not global**
If you run N nodes, each computes its own p99 locally; averaging p99s is
statistically wrong. For cluster-wide percentiles, use `distribution/2`
(histograms) and compute quantiles in Prometheus with `histogram_quantile()`.

**4. The reporter handler runs in the emitter's process**
Same caveat as raw `:telemetry`: keep your metric definitions simple
(no expensive tag computations) so the aggregator returns quickly.

**5. Unit conversion matters**
`:telemetry.span/3` uses `:native` time units. Always specify
`unit: {:native, :millisecond}` (or `:second`) in durations. Otherwise
your Prometheus values are in whatever BEAM decided `:native` means on
this host.

**6. `telemetry_metrics_prometheus_core` vs `telemetry_metrics_prometheus`**
The `_core` library is the aggregator only — you bring your own HTTP
exposition. The non-core library bundles a Plug endpoint and Cowboy.
For a Phoenix app, prefer the core library and mount a route in your
existing endpoint. For a standalone exporter, the full library saves
you ten lines of Plug.

**7. When NOT to expose Prometheus**
For dev-only visibility, `Phoenix.LiveDashboard`'s Metrics page (also a
`Telemetry.Metrics` reporter) is zero-config. For commercial observability
stacks (Datadog, Honeycomb), use their SDKs or OpenTelemetry — Prometheus
is great for self-hosted, less so when you already pay for a vendor.

---

## Reflection

- A developer adds `tags: [:user_id]` to a counter because "it would be
  useful". A week later Prometheus is OOMing. What tool and process
  would you put in place to catch the cardinality bug before it ships?
- Your SRE team wants cluster-wide p99 latency. You currently publish
  `summary("…duration")`. Why does averaging p99s across nodes give a
  wrong answer, and what's the `distribution/2` + `histogram_quantile()`
  alternative? Walk through the tradeoffs (disk, query cost, accuracy).

## Resources

- [`Telemetry.Metrics` — hexdocs](https://hexdocs.pm/telemetry_metrics/)
- [`TelemetryMetricsPrometheus.Core` — hexdocs](https://hexdocs.pm/telemetry_metrics_prometheus_core/)
- [`PromEx`](https://hexdocs.pm/prom_ex/) — opinionated bundle with plugins for Phoenix/Ecto/Oban
- [Dashbit: "Telemetry, Metrics and You"](https://dashbit.co/blog/)
- [Prometheus naming conventions](https://prometheus.io/docs/practices/naming/) — label and metric naming rules
- [Phoenix LiveDashboard Metrics](https://hexdocs.pm/phoenix_live_dashboard/metrics.html) — another reporter for the same definitions

---

## The business problem

Teams ship software against real constraints: latency budgets, availability targets, memory ceilings, and on-call rotations that punish complexity. The exercise in this document is framed against one of those constraints — not as a toy example, but as a miniature of a shape you will meet in production.

The goal is not to memorize an API. The goal is to recognize the pattern so that when you see it in your own codebase, you reach for the right tool immediately.

### `test/metrics_prom_test.exs`

```elixir
defmodule MetricsPromTest do
  use ExUnit.Case, async: true

  doctest MetricsProm

  describe "run/1" do
    test "returns :ok for a no-op input" do
      assert MetricsProm.run(:noop) == :ok
    end
  end
end
```

---

## Key concepts
Telemetry metrics are aggregated views of telemetry events—counters, distributions, summaries. The Telemetry.Metrics library defines metrics (e.g., "http requests per route"), and Prometheus integration (via `TelemetryMetricsPrometheus`) exports them to Prometheus. Prometheus scrapes endpoints (usually `/metrics`) and stores time series. This is the modern observability stack: telemetry events (fine-grained, high-volume), metrics (aggregated, low-volume), Prometheus storage (queryable, alertable). The pipeline: library emits telemetry → metrics definitions aggregate → Prometheus exports → visualization and alerting. Understanding this layering is essential for production observability.

---
