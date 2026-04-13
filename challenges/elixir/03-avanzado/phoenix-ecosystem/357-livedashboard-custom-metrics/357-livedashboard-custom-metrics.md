# Phoenix LiveDashboard Custom Metrics and Pages

**Project**: `observability_stack` — exposing business metrics (checkouts/sec, payment failure rate, queue depth) in Phoenix LiveDashboard via `Telemetry.Metrics` and a custom dashboard page.

## Project context

Your infra already exposes `/dashboard` for BEAM-level observability. The CEO wants one dashboard that also shows business metrics: "checkouts per minute", "payment failure rate", "Oban queue depth". You could add Grafana + Prometheus, but the team is 4 engineers and Grafana is a full ops burden. LiveDashboard supports custom metrics pages out of the box.

The key primitives: `Telemetry.Metrics` defines metric SHAPES (counter, last_value, summary, distribution, sum). `TelemetryMetricsPrometheus` or `LiveDashboard.MetricsHistoryBackend` sinks them. `Phoenix.LiveDashboard.PageBuilder` renders a custom page that reads the metric history.

```
observability_stack/
├── lib/
│   ├── observability_stack/
│   │   ├── application.ex
│   │   ├── telemetry.ex
│   │   └── payments.ex
│   └── observability_stack_web/
│       ├── endpoint.ex
│       ├── router.ex
│       └── dashboard_pages/
│           └── business_page.ex
├── test/
│   └── observability_stack/
│       └── telemetry_test.exs
└── mix.exs
```

## Why Telemetry.Metrics and not ad-hoc counters

`Counter.new/1`, `Agent.update`, `:ets.update_counter` all work for one metric on one node. They do not interoperate with the telemetry ecosystem — no Prometheus export, no LiveDashboard integration, no standard labels.

`:telemetry.execute/3` emits events; `Telemetry.Metrics` subscribes and aggregates. The same code can sink to LiveDashboard in dev, Prometheus in prod, and StatsD if an SRE asks for it — without changing the instrumentation points.

**Why not OpenTelemetry?** OTel is heavier (tracing + metrics + logs, B3/W3C context propagation). Use it when you need distributed tracing across services. For pure metrics on Elixir, `Telemetry.Metrics` is lighter and native.

## Core concepts

### 1. Metric shapes

- `counter/2` — ever-increasing count; useful for "requests".
- `sum/2` — add a value on each event (e.g., "total bytes transferred").
- `last_value/2` — replaces previous value; useful for gauges ("queue depth").
- `summary/2` — statistical summary (min, max, avg, percentiles) over a window.
- `distribution/2` — histogram buckets for latency SLOs.

### 2. Event name

Metrics subscribe to `[:app, :context, :action]` events. Emit with `:telemetry.execute([:payments, :charge, :stop], %{duration: 123}, %{status: :ok})`. The tuple `{event, measurement_key}` identifies a metric.

### 3. Tags

`tags: [:status]` splits a metric by a meta field. `tag_values: & &1 |> Map.take([:status])` transforms metadata before tagging. Low-cardinality tags only — every unique tag combination is a new time series.

### 4. LiveDashboard history backend

By default, LD shows only the current value. To plot history, set `metrics_history: {MyApp.HistoryBackend, :data, [:some_metric]}`. The `Phoenix.LiveDashboard.MetricsHistoryStore` provided by `telemetry_metrics_mnesia` or a hand-rolled ring buffer can back it.

## Design decisions

- **Option A — Prometheus scraping**: industry standard, needs Prometheus + Grafana.
- **Option B — StatsD + Datadog**: managed, cost per metric, black box.
- **Option C — `Telemetry.Metrics` + LiveDashboard custom page**: zero external deps, plots live.

Chosen: Option C for internal ops dashboards + Prometheus exporter in parallel for long-term retention. Small team, one pane of glass.

## Implementation

### Dependencies (`mix.exs`)

```elixir
defmodule ObservabilityStack.MixProject do
  use Mix.Project
  def project, do: [app: :observability_stack, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application do
    [mod: {ObservabilityStack.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_live_dashboard, "~> 0.8"},
      {:telemetry, "~> 1.2"},
      {:telemetry_metrics, "~> 1.0"},
      {:telemetry_poller, "~> 1.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"}
    ]
  end
end
```

### Dependencies (mix.exs)

```elixir
```elixir
defmodule ObservabilityStack.MixProject do
  use Mix.Project
  def project, do: [app: :observability_stack, version: "0.1.0", elixir: "~> 1.16", deps: deps()]

  def application do
    [mod: {ObservabilityStack.Application, []}, extra_applications: [:logger]]
  end

  defp deps do
    [
      {:phoenix, "~> 1.7.14"},
      {:phoenix_live_view, "~> 1.0"},
      {:phoenix_live_dashboard, "~> 0.8"},
      {:telemetry, "~> 1.2"},
      {:telemetry_metrics, "~> 1.0"},
      {:telemetry_poller, "~> 1.1"},
      {:jason, "~> 1.4"},
      {:plug_cowboy, "~> 2.7"}
    ]
  end
end
```

### Step 1: Telemetry supervisor — `lib/observability_stack/telemetry.ex`

**Objective**: Centralize metric definitions and the telemetry_poller so LiveDashboard has a single module to consume and periodic VM/business measurements run under supervision.

```elixir
defmodule ObservabilityStack.Telemetry do
  use Supervisor
  import Telemetry.Metrics

  def start_link(arg), do: Supervisor.start_link(__MODULE__, arg, name: __MODULE__)

  @impl true
  def init(_arg) do
    children = [
      {:telemetry_poller, measurements: periodic_measurements(), period: 10_000, name: __MODULE__}
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end

  @doc "Consumed by Phoenix.LiveDashboard"
  def metrics do
    [
      # --- Phoenix built-ins ------------------------------------------------
      summary("phoenix.endpoint.stop.duration",
        unit: {:native, :millisecond},
        tags: [:route]
      ),

      # --- Business metrics -------------------------------------------------
      counter("payments.charge.stop.count", tags: [:status]),
      summary("payments.charge.stop.duration",
        unit: {:native, :millisecond},
        tags: [:status]
      ),
      distribution("payments.charge.stop.duration",
        unit: {:native, :millisecond},
        reporter_options: [buckets: [10, 50, 100, 500, 1000, 5000]]
      ),

      # --- Runtime periodic -------------------------------------------------
      last_value("vm.memory.total", unit: {:byte, :megabyte}),
      last_value("vm.total_run_queue_lengths.total"),
      last_value("observability_stack.queue.depth", tags: [:queue])
    ]
  end

  defp periodic_measurements do
    [
      {ObservabilityStack.Telemetry, :dispatch_queue_depth, []}
    ]
  end

  def dispatch_queue_depth do
    for queue <- [:default, :payments] do
      depth = :rand.uniform(200)
      :telemetry.execute([:observability_stack, :queue, :depth], %{value: depth}, %{queue: queue})
    end
  end
end
```

### Step 2: Emit events from business code — `lib/observability_stack/payments.ex`

**Objective**: Emit `:start`/`:stop` telemetry spans from the domain so metrics decouple from the observability backend, keeping the business module ignorant of LiveDashboard or Prometheus.

```elixir
defmodule ObservabilityStack.Payments do
  @event_prefix [:payments, :charge]

  def charge(amount, _card) do
    start = System.monotonic_time()
    :telemetry.execute(@event_prefix ++ [:start], %{system_time: System.system_time()}, %{})

    status = if :rand.uniform() > 0.05, do: :ok, else: :declined

    duration = System.monotonic_time() - start

    :telemetry.execute(
      @event_prefix ++ [:stop],
      %{duration: duration},
      %{status: status, amount: amount}
    )

    {status, amount}
  end
end
```

### Step 3: Custom dashboard page — `lib/observability_stack_web/dashboard_pages/business_page.ex`

**Objective**: Extend LiveDashboard via `PageBuilder` so domain-specific metrics live alongside Phoenix/VM panels, reusing LD's styling and live updates without building a separate UI.

```elixir
defmodule ObservabilityStackWeb.DashboardPages.BusinessPage do
  use Phoenix.LiveDashboard.PageBuilder

  @impl true
  def menu_link(_, _), do: {:ok, "Business"}

  @impl true
  def render_page(_assigns) do
    columns(components: [
      [
        row(components: [
          {:summary,
           metric: Telemetry.Metrics.counter("payments.charge.stop.count", tags: [:status]),
           title: "Charges by status"}
        ])
      ]
    ])
  end
end
```

### Step 4: Router mount — `lib/observability_stack_web/router.ex`

**Objective**: Mount `live_dashboard` behind the browser pipeline and register the custom page so the metrics module and business page are both wired to a single `/dashboard` entry point.

```elixir
defmodule ObservabilityStackWeb.Router do
  use Phoenix.Router
  import Phoenix.LiveDashboard.Router

  pipeline :browser do
    plug :accepts, ["html"]
    plug :fetch_session
    plug :protect_from_forgery
  end

  scope "/" do
    pipe_through :browser

    live_dashboard "/dashboard",
      metrics: ObservabilityStack.Telemetry,
      additional_pages: [
        business: ObservabilityStackWeb.DashboardPages.BusinessPage
      ]
  end
end
```

## Why this works

`:telemetry.execute/3` is a local `:ets` lookup + function call per handler — O(1) emit cost. `Telemetry.Metrics` attaches one handler per metric. LiveDashboard subscribes as another handler, storing values in an in-memory ring buffer (default 60 samples). When the dashboard LV renders, it reads that buffer — no recompute, no query.

The custom page is a `Phoenix.LiveDashboard.PageBuilder` behaviour; its `render_page/1` uses LD's own components (`columns`, `row`, `{:summary, ...}`). This reuses LD's styling and live-reload.

## Tests — `test/observability_stack/telemetry_test.exs`

```elixir
defmodule ObservabilityStack.TelemetryTest do
  use ExUnit.Case, async: false

  describe "metrics/0" do
    test "includes payments.charge counter with :status tag" do
      metrics = ObservabilityStack.Telemetry.metrics()

      assert Enum.any?(metrics, fn
               %Telemetry.Metrics.Counter{name: [:payments, :charge, :stop, :count]} = m ->
                 :status in m.tags

               _ ->
                 false
             end)
    end

    test "distribution has explicit buckets for SLO monitoring" do
      metrics = ObservabilityStack.Telemetry.metrics()
      dist = Enum.find(metrics, &match?(%Telemetry.Metrics.Distribution{}, &1))
      buckets = dist.reporter_options |> Keyword.fetch!(:buckets)
      assert 100 in buckets
    end
  end

  describe "Payments.charge/2" do
    test "emits :stop event with :status metadata" do
      ref = :telemetry_test.attach_event_handlers(self(), [[:payments, :charge, :stop]])
      ObservabilityStack.Payments.charge(9_99, "tok_test")
      assert_receive {[:payments, :charge, :stop], ^ref, measurements, meta}
      assert is_integer(measurements.duration)
      assert meta.status in [:ok, :declined]
    end
  end
end
```

## Benchmark

```elixir
# bench/telemetry_bench.exs
Benchee.run(
  %{
    "telemetry.execute (no handler)" => fn ->
      :telemetry.execute([:bench, :noop], %{x: 1}, %{})
    end,
    "telemetry.execute (1 handler)" => fn ->
      :telemetry.execute([:bench, :attached], %{x: 1}, %{})
    end
  },
  before_scenario: fn _ ->
    :telemetry.attach("handler", [:bench, :attached], fn _, _, _, _ -> :ok end, nil)
  end,
  time: 2
)
```

**Expected**: no-handler ~200ns, single-handler ~1µs. 1000 emits/sec is well under 1% of one core.

## Advanced Considerations: LiveView Real-Time Patterns and Pubsub Scale

LiveView bridges the browser and BEAM via WebSocket, allowing server-side renders to push incremental DOM diffs to the client. A LiveView process is long-lived, receiving events (clicks, form submissions) and broadcasting updates. For real-time features (collaborative editing, live notifications), LiveView processes subscribe to PubSub topics and receive broadcast messages.

Phoenix.PubSub partitions topics across a pool of processes, allowing horizontal scaling. By default, `:local` mode uses in-memory ETS; `:redis` mode distributes across nodes via Redis. At scale (thousands of concurrent LiveViews), topic fanout can bottleneck: broadcasting to a million subscribers means delivering one million messages. The BEAM handles this, but the network cost matters on multi-node deployments.

`Presence` module tracks which users are viewing which pages, syncing state via PubSub. A presence join/leave is broadcast to all nodes, allowing real-time "who's online" updates. Under partition, presence state can diverge; the library uses unique presence keys to detect and reconcile. Operationally, watching presence on every page load can amplify server load if users are flaky (mobile networks, browser reloads). Consider presence only for features where it's user-facing (collaborative editors, live sports scoreboards).

---


## Deep Dive: Phoenix Patterns and Production Implications

Phoenix's conn struct represents an HTTP request/response in flight, accumulating transformations through middleware and handler code. Testing a Phoenix endpoint end-to-end (not just the controller) catches middleware order bugs, header mismatches, and plug composition issues. The trade-off is that full integration tests are slower and harder to parallelize than unit tests. Production bugs in auth, CORS, or session handling are often due to middleware assumptions that live tests reveal.

---

## Trade-offs and production gotchas

**1. High-cardinality tags destroy memory.** Tagging `user_id` creates one time series per user. At 1M users, your metrics backend OOMs. Tag with bounded dimensions only (status, route template, queue name).

**2. `:telemetry.attach/4` blocks on the emitting process.** A slow handler slows every caller. Wrap the handler in `Task.start` if it does I/O.

**3. LD's in-memory history is node-local.** In a cluster, each node shows its own view. For aggregated dashboards, export to Prometheus or use a distributed backend.

**4. `periodic_measurements` run on the poller process.** Exceptions crash it. `try/rescue` around anything that can raise.

**5. Renaming an event breaks grafana dashboards silently.** Metric names become a public contract. Version them (`:v1` suffix) or emit BOTH old and new during a deprecation window.

**6. When NOT to use LiveDashboard for metrics.** Cross-service correlation, alerting, long-term (> 1 week) retention — use Prometheus + Grafana + Alertmanager. LD is for live ops insight, not audit history.

## Reflection

You want an "error rate per route" metric with alerting. Outline the shape: which `Telemetry.Metrics` type, what tags, what backend for alerting. Why is `counter` alone insufficient?

## Resources

- [Phoenix.LiveDashboard — hexdocs](https://hexdocs.pm/phoenix_live_dashboard/)
- [Telemetry.Metrics — hexdocs](https://hexdocs.pm/telemetry_metrics/)
- [LiveDashboard custom pages guide](https://hexdocs.pm/phoenix_live_dashboard/custom_page.html)
- [telemetry_metrics_prometheus](https://github.com/beam-telemetry/telemetry_metrics_prometheus)
