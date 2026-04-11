# Livebook: Operational Notebooks for api_gateway

**Project**: `api_gateway` — built incrementally across the advanced level

---

## Project context

The `api_gateway` umbrella is deployed to production. The ops team needs visibility into
the running system: which upstream services are circuit-broken, how the ETS rate-limiter
table looks under load, and whether the Oban queues are keeping up. You'll build Livebook
notebooks that connect to the running node and provide this visibility interactively.

Notebooks to create:

```
notebooks/
├── 01_process_inspector.livemd     # supervisor tree, memory, message queues
├── 02_gateway_metrics.livemd       # real-time telemetry with VegaLite
├── 03_oban_analysis.livemd         # job history analysis with Explorer
└── 04_diagnostic_dashboard.livemd  # interactive forms for operational tasks
```

---

## Why Livebook over a custom dashboard

Dashboards require deployment infrastructure — a web server, authentication, a frontend.
Livebook notebooks run on demand, connect to existing nodes, and require no additional
deployment. When production is on fire at 2am, a notebook that reads ETS tables and
renders charts is faster to build and easier to trust than a dashboard that might itself be
broken.

Notebooks are also version-controlled: the `.livemd` format is plain Markdown with embedded
code cells. Pull request review for diagnostic notebooks is straightforward.

---

## Setup

```bash
# Install Livebook globally
mix escript.install hex livebook
livebook server

# Or via Docker
docker run -p 8080:8080 -p 8081:8081 --pull always ghcr.io/livebook-dev/livebook

# Connecting to a remote production node:
# 1. In vm.args: -name api_gateway@10.0.0.1 -setcookie my-secret-cookie
# 2. In Livebook: "Runtime Settings" → "Attached Node"
# 3. Enter node name and cookie
```

---

## Notebook 1 — Process Inspector (`01_process_inspector.livemd`)

This notebook must pass as-is when opened in Livebook and executed top-to-bottom.

```elixir
# Cell: Setup
Mix.install([
  {:kino, "~> 0.12"},
  {:vega_lite, "~> 0.1"},
  {:kino_vega_lite, "~> 0.1"}
])
```

```elixir
# Cell: Top 20 processes by memory — implement this
# TODO:
# 1. Process.list() — get all PIDs
# 2. For each pid: Process.info(pid, [:registered_name, :memory, :message_queue_len, :reductions])
# 3. Build a list of maps with pid, name, memory_kb, msg_queue, reductions
# 4. Sort by memory_kb descending, take 20
# 5. Render with Kino.DataTable.new/2
#
# HINT: Process.info/2 returns nil if the process died between listing and inspection
# HINT: filter out nils before building maps
```

```elixir
# Cell: api_gateway supervisor tree
defmodule TreeInspector do
  def build(sup_pid) do
    children = Supervisor.which_children(sup_pid)
    %{
      name: registered_name(sup_pid),
      pid:  inspect(sup_pid),
      children: Enum.map(children, &child_entry/1)
    }
  rescue
    _ -> %{name: registered_name(sup_pid), pid: inspect(sup_pid), children: []}
  end

  defp child_entry({id, pid, :supervisor, _}) when is_pid(pid) do
    build(pid) |> Map.put(:id, id) |> Map.put(:type, :supervisor)
  end
  defp child_entry({id, pid, :worker, [mod | _]}) when is_pid(pid) do
    %{id: id, pid: inspect(pid), type: :worker, module: mod, name: registered_name(pid)}
  end
  defp child_entry({id, :undefined, _, _}) do
    %{id: id, type: :not_started}
  end

  defp registered_name(pid) do
    case Process.info(pid, :registered_name) do
      {:registered_name, name} when name != [] -> name
      _ -> :unnamed
    end
  end
end

# TODO: replace with the actual top-level supervisor of api_gateway
tree = TreeInspector.build(Process.whereis(ApiGateway.Supervisor))
Kino.Tree.new(tree)
```

```elixir
# Cell: ETS tables — view rate-limiter state
# TODO:
# 1. :ets.all() — list all ETS tables
# 2. For each table: :ets.info(table, :name), :ets.info(table, :size), :ets.info(table, :memory)
# 3. Render as Kino.DataTable
```

```elixir
# Cell: Auto-refresh process table every 5 seconds
# TODO:
# Use Kino.animate/2 to re-execute the process inspection cell every 5000ms
# HINT: Kino.animate(5_000, fn _ -> ... Kino.DataTable.new(...) end)
```

---

## Notebook 2 — Gateway Metrics (`02_gateway_metrics.livemd`)

```elixir
# Cell: Setup
Mix.install([
  {:kino, "~> 0.12"},
  {:vega_lite, "~> 0.1"},
  {:kino_vega_lite, "~> 0.1"}
])

alias VegaLite, as: Vl
```

```elixir
# Cell: Telemetry collector GenServer
# TODO: implement a GenServer that:
# 1. Attaches to [:api_gateway, :http_client, :request] telemetry events
# 2. Stores the last 1000 samples as a list of maps with event, duration_ms, status, host
# 3. Exposes get_samples/0 for reading
# 4. Starts with GenServer.start_link without supervision (notebook-local process)
#
# HINT: see the MetricsCollector pattern from the original exercise 49
```

```elixir
# Cell: Real-time HTTP request latency chart (line chart)
# TODO:
# 1. Create a VegaLite chart spec: x = timestamp (temporal), y = duration_ms (quantitative)
# 2. Wrap it with Kino.VegaLite.new/1
# 3. Use Kino.animate/2 to push new data every 2 seconds
#
# HINT: Kino.VegaLite.push_many/2 appends data points without rebuilding the chart
```

```elixir
# Cell: Circuit breaker status per host
# TODO: read from ETS table :circuit_breaker
# Show a table with: host, state (:closed/:open/:half_open), opened_at (if open)
# Render as Kino.DataTable
```

```elixir
# Cell: HTTP status distribution (pie chart)
# TODO:
# 1. Get samples from MetricsCollector
# 2. Group by status code bucket (2xx, 4xx, 5xx, circuit_open)
# 3. Render as VegaLite arc/pie mark
```

---

## Notebook 3 — Oban Analysis (`03_oban_analysis.livemd`)

```elixir
# Cell: Setup
Mix.install([
  {:kino, "~> 0.12"},
  {:vega_lite, "~> 0.1"},
  {:kino_vega_lite, "~> 0.1"},
  {:explorer, "~> 0.8"},
  {:kino_explorer, "~> 0.1"}
])

alias VegaLite, as: Vl
alias Explorer.DataFrame
alias Explorer.Series
```

```elixir
# Cell: Load Oban job history from DB as a DataFrame
import Ecto.Query

# TODO:
# 1. Query Oban.Job for jobs in the last 7 days
#    Select: worker, queue, state, attempt, inserted_at, completed_at
# 2. Calculate duration_ms from completed_at - attempted_at
# 3. Convert to Explorer.DataFrame
#
# HINT:
# from(j in Oban.Job,
#   where: j.inserted_at > ago(7, "day"),
#   select: %{worker: j.worker, queue: j.queue, state: j.state, ...}
# ) |> GatewayCore.Repo.all()
```

```elixir
# Cell: Worker performance summary table
# TODO:
# df
# |> DataFrame.group_by(["worker", "state"])
# |> DataFrame.summarise(
#     count:        count(col("worker")),
#     avg_duration: mean(col("duration_ms")),
#     p95_duration: quantile(col("duration_ms"), 0.95)
#    )
# |> DataFrame.sort_by(desc: col("count"))
# |> Kino.DataTable.new(name: "Worker Performance (last 7 days)")
```

```elixir
# Cell: Failed jobs bar chart
# TODO:
# 1. Filter df where state == "discarded"
# 2. Group by worker, count failures
# 3. Render as VegaLite bar chart with red fill, sorted by count descending
```

```elixir
# Cell: Job throughput heatmap (hour of day × day of week)
# TODO:
# 1. DataFrame.mutate to extract hour and weekday from inserted_at
# 2. Group by [hour, weekday], count jobs
# 3. Render as VegaLite rect mark (heatmap)
```

```elixir
# Cell: Export analysis to CSV
# TODO:
# summary = ... (the worker performance DataFrame from above)
# DataFrame.to_csv!(summary, "/tmp/oban_analysis_#{Date.utc_today()}.csv")
# Kino.Markdown.new("Saved to /tmp/oban_analysis_*.csv")
```

---

## Notebook 4 — Diagnostic Dashboard (`04_diagnostic_dashboard.livemd`)

```elixir
# Cell: Setup
Mix.install([{:kino, "~> 0.12"}])
```

```elixir
# Cell: GenServer state inspector form
# TODO:
# 1. Kino.Control.form with:
#    - module: Kino.Input.text("Module name", default: "ApiGateway.Cache.Server")
#    - include_state: Kino.Input.checkbox("Include full state (may be large)")
# 2. Kino.listen/2 on the form:
#    - String.to_existing_atom("Elixir." <> data.module) to get the module
#    - Process.whereis/1 to get the PID
#    - Process.info/2 for [:memory, :message_queue_len, :reductions, :status]
#    - :sys.get_state/1 if include_state is checked
#    - Render with Kino.Tree.new/1
#
# DESIGN NOTE: String.to_existing_atom/1 is safe here — it only works for atoms
# already in the atom table (i.e., modules that have been loaded). Never use
# String.to_atom/1 with user input in production code.
```

```elixir
# Cell: Rate limiter — inspect entries for a specific client
form = Kino.Control.form(
  [client_id: Kino.Input.text("Client ID")],
  submit: "Inspect"
)

Kino.listen(form, fn %{data: %{client_id: client_id}} ->
  # TODO:
  # 1. :ets.lookup(:rate_limiter_windows, client_id) — from exercise 71
  # 2. Show all timestamps in the window as a table
  # 3. Show how many requests remain before the limit is reached
end)

form
```

```elixir
# Cell: System health snapshot (refresh button)
# TODO:
# 1. Kino.Control.button("Refresh")
# 2. Kino.listen/2: collect the following metrics and render as Kino.Tree:
#    - node()
#    - uptime: :erlang.statistics(:wall_clock) |> elem(0) |> div(1000)
#    - process_count: length(Process.list())
#    - memory: :erlang.memory() — total, processes, ets, binary (all in MB)
#    - scheduler_utilization: :scheduler.utilization(1) (if available)
#    - run_queue: :erlang.statistics(:run_queue)
```

---

## Given tests

Livebook notebooks don't use ExUnit, but each notebook must satisfy these manual checks:

- Opening the notebook in Livebook and pressing "Run all cells" completes without errors
- `Kino.DataTable` renders a visible, sortable table with at least one row of data
- `Kino.animate/2` cells update visibly after the configured interval
- The form in Notebook 4 responds to input within 500ms
- Notebook 3's DataFrame cells require a live DB connection — document this in a Markdown cell

---

## Trade-off analysis

| Aspect | Livebook notebooks | Phoenix LiveDashboard | Custom monitoring app |
|--------|-------------------|-----------------------|-----------------------|
| Setup time | none (connect to existing node) | add dependency + route | build from scratch |
| Deployment | standalone tool | baked into app | separate service |
| Version control | .livemd is plain text | N/A | code repo |
| Interactivity | full (forms, sliders, code cells) | limited | custom |
| Security surface | manual session management | Phoenix auth | custom |
| Reproducibility | cells are executable docs | static dashboard | varies |

Reflection: Livebook connects to production nodes using the Erlang distribution protocol.
What are the security implications? What controls would you put in place before doing this
in a PCI-DSS or SOC 2 environment?

---

## Common production mistakes

**1. `String.to_atom/1` with user input**
In the form cells, always use `String.to_existing_atom/1`. The atom table has a fixed size;
flooding it with arbitrary strings is a denial-of-service vector.

**2. Calling `:sys.get_state/1` on a high-traffic GenServer**
`:sys.get_state/1` sends a synchronous message to the GenServer, pausing it while it
serializes state. On a GenServer processing thousands of messages per second, this call can
block for seconds. Use it only on GenServers with light load, or read ETS directly.

**3. Accumulating telemetry samples without a size limit**
The MetricsCollector keeps the last 1000 samples. Without this limit, running the notebook
on a busy gateway for hours fills the notebook process memory. Always cap the sample buffer.

**4. Not guarding against process death in Process.info/2**
Between `Process.list()` and `Process.info(pid, ...)`, a process can die. `Process.info`
returns `nil` for dead processes. Filter nils before mapping.

**5. Connecting Livebook to production without a read-only constraint**
Livebook code cells can call any function on the connected node — including destructive ones.
Use a separate read-only user or restrict Livebook to a staging node for exploratory work.

---

## Resources

- [Livebook documentation](https://livebook.dev) — setup, runtime connections, smart cells
- [Kino](https://hexdocs.pm/kino/Kino.html) — interactive widgets, DataTable, VegaLite integration
- [Explorer](https://hexdocs.pm/explorer/Explorer.html) — DataFrame API reference
- [VegaLite](https://hexdocs.pm/vega_lite/VegaLite.html) — chart specification DSL
- [Connecting Livebook to a remote node](https://news.livebook.dev/announcing-livebook-0.6-2RnYHg) — secure attachment guide
