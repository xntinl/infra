# Livebook: Operational Notebooks for api_gateway

## Overview

Build interactive Livebook notebooks for monitoring and diagnosing a running Elixir
application. The notebooks connect to a live BEAM node and provide real-time visibility
into process state, telemetry metrics, ETS tables, and background job health -- all
without deploying any additional infrastructure.

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

Dashboards require deployment infrastructure -- a web server, authentication, a frontend.
Livebook notebooks run on demand, connect to existing nodes, and require no additional
deployment. When production is on fire at 2am, a notebook that reads ETS tables and
renders charts is faster to build and easier to trust than a dashboard that might itself be
broken.

Notebooks are also version-controlled: the `.livemd` format is plain Markdown with embedded
code cells. Pull request review for diagnostic notebooks is straightforward.

---

## Setup

```bash
mix escript.install hex livebook
livebook server

# Or via Docker
docker run -p 8080:8080 -p 8081:8081 --pull always ghcr.io/livebook-dev/livebook

# Connecting to a remote production node:
# 1. In vm.args: -name api_gateway@10.0.0.1 -setcookie my-secret-cookie
# 2. In Livebook: "Runtime Settings" -> "Attached Node"
# 3. Enter node name and cookie
```

---

## Notebook 1 -- Process Inspector (`01_process_inspector.livemd`)

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
# Cell: Top 20 processes by memory
process_data =
  Process.list()
  |> Enum.map(fn pid ->
    case Process.info(pid, [:registered_name, :memory, :message_queue_len, :reductions]) do
      nil ->
        nil

      info ->
        name =
          case info[:registered_name] do
            [] -> inspect(pid)
            name -> inspect(name)
          end

        %{
          pid: inspect(pid),
          name: name,
          memory_kb: Float.round(info[:memory] / 1024, 1),
          msg_queue: info[:message_queue_len],
          reductions: info[:reductions]
        }
    end
  end)
  |> Enum.reject(&is_nil/1)
  |> Enum.sort_by(& &1.memory_kb, :desc)
  |> Enum.take(20)

Kino.DataTable.new(process_data, name: "Top 20 Processes by Memory")
```

```elixir
# Cell: Supervisor tree inspector
defmodule TreeInspector do
  @doc "Recursively walks a supervisor tree and builds an inspectable map."
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

# Replace with the actual supervisor name of your application
tree = TreeInspector.build(Process.whereis(ApiGateway.Supervisor))
Kino.Tree.new(tree)
```

```elixir
# Cell: ETS tables -- view all tables with size and memory usage
ets_data =
  :ets.all()
  |> Enum.map(fn table ->
    %{
      name: :ets.info(table, :name),
      size: :ets.info(table, :size),
      memory_kb: Float.round(:ets.info(table, :memory) * :erlang.system_info(:wordsize) / 1024, 1),
      type: :ets.info(table, :type),
      protection: :ets.info(table, :protection)
    }
  end)
  |> Enum.sort_by(& &1.memory_kb, :desc)

Kino.DataTable.new(ets_data, name: "ETS Tables")
```

```elixir
# Cell: Auto-refresh process table every 5 seconds
Kino.animate(5_000, fn _iteration ->
  data =
    Process.list()
    |> Enum.map(fn pid ->
      case Process.info(pid, [:registered_name, :memory, :message_queue_len]) do
        nil -> nil
        info ->
          name = case info[:registered_name] do
            [] -> inspect(pid)
            n -> inspect(n)
          end
          %{name: name, memory_kb: Float.round(info[:memory] / 1024, 1), msg_queue: info[:message_queue_len]}
      end
    end)
    |> Enum.reject(&is_nil/1)
    |> Enum.sort_by(& &1.memory_kb, :desc)
    |> Enum.take(10)

  Kino.DataTable.new(data, name: "Top 10 (live)")
end)
```

---

## Notebook 2 -- Gateway Metrics (`02_gateway_metrics.livemd`)

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
defmodule MetricsCollector do
  use GenServer

  @max_samples 1_000

  def start_link, do: GenServer.start_link(__MODULE__, [], name: __MODULE__)
  def get_samples, do: GenServer.call(__MODULE__, :get_samples)

  @impl true
  def init(_) do
    :telemetry.attach(
      "notebook-metrics-collector",
      [:api_gateway, :http_client, :request],
      &__MODULE__.handle_telemetry/4,
      nil
    )
    {:ok, %{samples: []}}
  end

  @impl true
  def handle_call(:get_samples, _from, state) do
    {:reply, state.samples, state}
  end

  @impl true
  def handle_cast({:sample, sample}, state) do
    samples = Enum.take([sample | state.samples], @max_samples)
    {:noreply, %{state | samples: samples}}
  end

  def handle_telemetry(_event, %{duration: dur}, meta, _config) do
    ms = System.convert_time_unit(dur, :native, :millisecond)
    sample = %{
      ts: DateTime.utc_now(),
      duration_ms: ms,
      status: meta[:status] || 0,
      host: meta[:host] || "unknown"
    }
    GenServer.cast(__MODULE__, {:sample, sample})
  end
end

{:ok, _} = MetricsCollector.start_link()
Kino.Markdown.new("MetricsCollector started. Collecting telemetry samples...")
```

```elixir
# Cell: Real-time HTTP request latency chart (line chart)
chart =
  Vl.new(width: 600, height: 300, title: "HTTP Request Latency (ms)")
  |> Vl.mark(:line)
  |> Vl.encode_field(:x, "ts", type: :temporal, title: "Time")
  |> Vl.encode_field(:y, "duration_ms", type: :quantitative, title: "Latency (ms)")
  |> Vl.encode_field(:color, "host", type: :nominal)
  |> Kino.VegaLite.new()

Kino.animate(2_000, fn _i ->
  samples = MetricsCollector.get_samples() |> Enum.take(50)

  data =
    Enum.map(samples, fn s ->
      %{"ts" => DateTime.to_iso8601(s.ts), "duration_ms" => s.duration_ms, "host" => s.host}
    end)

  unless data == [] do
    Kino.VegaLite.push_many(chart, data, window: 100)
  end

  chart
end)
```

```elixir
# Cell: Circuit breaker status per host
cb_data =
  case :ets.whereis(:circuit_breaker) do
    :undefined ->
      [%{host: "(table not found)", state: "n/a", opened_at: "n/a"}]

    _ref ->
      :ets.tab2list(:circuit_breaker)
      |> Enum.filter(fn entry -> is_binary(elem(entry, 0)) end)
      |> Enum.map(fn {host, state, meta} ->
        %{
          host: host,
          state: state,
          opened_at: if(state == :open, do: "#{System.monotonic_time(:millisecond) - meta}ms ago", else: "n/a")
        }
      end)
  end

Kino.DataTable.new(cb_data, name: "Circuit Breaker Status")
```

```elixir
# Cell: HTTP status distribution (pie chart)
samples = MetricsCollector.get_samples()

status_groups =
  samples
  |> Enum.group_by(fn s ->
    cond do
      s.status >= 200 and s.status < 300 -> "2xx"
      s.status >= 400 and s.status < 500 -> "4xx"
      s.status >= 500 -> "5xx"
      s.status == 0 -> "circuit_open"
      true -> "other"
    end
  end)
  |> Enum.map(fn {bucket, items} -> %{"bucket" => bucket, "count" => length(items)} end)

Vl.new(width: 300, height: 300, title: "Status Distribution")
|> Vl.data_from_values(status_groups)
|> Vl.mark(:arc, inner_radius: 50)
|> Vl.encode_field(:theta, "count", type: :quantitative)
|> Vl.encode_field(:color, "bucket", type: :nominal)
```

---

## Notebook 3 -- Oban Analysis (`03_oban_analysis.livemd`)

**Note**: This notebook requires a live DB connection. Attach to a node that has the Repo
started, or configure a standalone Repo connection.

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

jobs =
  from(j in "oban_jobs",
    where: j.inserted_at > ago(7, "day"),
    select: %{
      worker: j.worker,
      queue: j.queue,
      state: j.state,
      attempt: j.attempt,
      inserted_at: j.inserted_at,
      completed_at: j.completed_at,
      attempted_at: j.attempted_at
    }
  )
  |> GatewayCore.Repo.all()
  |> Enum.map(fn row ->
    duration_ms =
      if row.completed_at && row.attempted_at do
        DateTime.diff(row.completed_at, row.attempted_at, :millisecond)
      else
        nil
      end

    Map.put(row, :duration_ms, duration_ms)
  end)

df = DataFrame.new(jobs)
Kino.DataTable.new(df, name: "Oban Jobs (last 7 days)")
```

```elixir
# Cell: Worker performance summary table
summary =
  df
  |> DataFrame.group_by(["worker", "state"])
  |> DataFrame.summarise(
    count: count(col("worker")),
    avg_duration: mean(col("duration_ms")),
    p95_duration: quantile(col("duration_ms"), 0.95)
  )
  |> DataFrame.sort_by(desc: col("count"))

Kino.DataTable.new(summary, name: "Worker Performance (last 7 days)")
```

```elixir
# Cell: Failed jobs bar chart
failed =
  df
  |> DataFrame.filter(col("state") == "discarded")
  |> DataFrame.group_by("worker")
  |> DataFrame.summarise(failures: count(col("worker")))
  |> DataFrame.sort_by(desc: col("failures"))
  |> DataFrame.to_rows()

Vl.new(width: 500, height: 300, title: "Failed Jobs by Worker")
|> Vl.data_from_values(failed)
|> Vl.mark(:bar, color: "#e74c3c")
|> Vl.encode_field(:x, "worker", type: :nominal, sort: "-y", title: "Worker")
|> Vl.encode_field(:y, "failures", type: :quantitative, title: "Failures")
```

```elixir
# Cell: Job throughput heatmap (hour of day x day of week)
heatmap_data =
  df
  |> DataFrame.mutate(
    hour: Explorer.Series.hour(col("inserted_at")),
    weekday: Explorer.Series.day_of_week(col("inserted_at"))
  )
  |> DataFrame.group_by(["hour", "weekday"])
  |> DataFrame.summarise(job_count: count(col("worker")))
  |> DataFrame.to_rows()

Vl.new(width: 500, height: 300, title: "Job Throughput Heatmap")
|> Vl.data_from_values(heatmap_data)
|> Vl.mark(:rect)
|> Vl.encode_field(:x, "hour", type: :ordinal, title: "Hour of Day")
|> Vl.encode_field(:y, "weekday", type: :ordinal, title: "Day of Week")
|> Vl.encode_field(:color, "job_count", type: :quantitative, title: "Jobs")
```

```elixir
# Cell: Export analysis to CSV
filename = "/tmp/oban_analysis_#{Date.utc_today()}.csv"
DataFrame.to_csv!(summary, filename)
Kino.Markdown.new("Saved to `#{filename}`")
```

---

## Notebook 4 -- Diagnostic Dashboard (`04_diagnostic_dashboard.livemd`)

```elixir
# Cell: Setup
Mix.install([{:kino, "~> 0.12"}])
```

```elixir
# Cell: GenServer state inspector form
form = Kino.Control.form(
  [
    module: Kino.Input.text("Module name", default: "ApiGateway.Cache.Server"),
    include_state: Kino.Input.checkbox("Include full state (may be large)")
  ],
  submit: "Inspect"
)

Kino.listen(form, fn %{data: %{module: module_name, include_state: include_state}} ->
  try do
    mod = String.to_existing_atom("Elixir." <> module_name)
    pid = Process.whereis(mod)

    if pid do
      info = Process.info(pid, [:memory, :message_queue_len, :reductions, :status])

      result = %{
        module: module_name,
        pid: inspect(pid),
        memory_kb: Float.round(info[:memory] / 1024, 1),
        message_queue: info[:message_queue_len],
        reductions: info[:reductions],
        status: info[:status]
      }

      result =
        if include_state do
          state = :sys.get_state(pid)
          Map.put(result, :state, state)
        else
          result
        end

      Kino.Tree.new(result)
    else
      Kino.Markdown.new("**Process `#{module_name}` not found** (not registered or not started)")
    end
  rescue
    ArgumentError ->
      Kino.Markdown.new("**Module `#{module_name}` does not exist** (atom not in table)")
  end
end)

form
```

```elixir
# Cell: ETS table inspector
form = Kino.Control.form(
  [table_name: Kino.Input.text("ETS Table Name", default: "rate_limiter_windows")],
  submit: "Inspect"
)

Kino.listen(form, fn %{data: %{table_name: table_name}} ->
  try do
    table_atom = String.to_existing_atom(table_name)

    case :ets.whereis(table_atom) do
      :undefined ->
        Kino.Markdown.new("**Table `:#{table_name}` not found**")

      _ref ->
        entries = :ets.tab2list(table_atom) |> Enum.take(100)

        data = Enum.map(entries, fn entry ->
          %{entry: inspect(entry, limit: 200)}
        end)

        Kino.DataTable.new(data, name: "First 100 entries from :#{table_name}")
    end
  rescue
    ArgumentError ->
      Kino.Markdown.new("**Atom `:#{table_name}` does not exist**")
  end
end)

form
```

```elixir
# Cell: System health snapshot (refresh button)
button = Kino.Control.button("Refresh")

Kino.listen(button, fn _event ->
  {uptime_ms, _} = :erlang.statistics(:wall_clock)
  memory = :erlang.memory()
  run_queue = :erlang.statistics(:run_queue)

  snapshot = %{
    node: node(),
    uptime_seconds: div(uptime_ms, 1000),
    process_count: length(Process.list()),
    memory: %{
      total_mb: Float.round(memory[:total] / 1_048_576, 1),
      processes_mb: Float.round(memory[:processes] / 1_048_576, 1),
      ets_mb: Float.round(memory[:ets] / 1_048_576, 1),
      binary_mb: Float.round(memory[:binary] / 1_048_576, 1)
    },
    run_queue: run_queue,
    schedulers_online: :erlang.system_info(:schedulers_online)
  }

  Kino.Tree.new(snapshot)
end)

button
```

---

## Verification

Livebook notebooks don't use ExUnit, but each notebook must satisfy these checks:

- Opening the notebook in Livebook and pressing "Run all cells" completes without errors
- `Kino.DataTable` renders a visible, sortable table with at least one row of data
- `Kino.animate/2` cells update visibly after the configured interval
- The form in Notebook 4 responds to input within 500ms
- Notebook 3's DataFrame cells require a live DB connection -- documented in a Markdown cell

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

---

## Common production mistakes

**1. `String.to_atom/1` with user input**
In the form cells, always use `String.to_existing_atom/1`. The atom table has a fixed size;
flooding it with arbitrary strings is a denial-of-service vector.

**2. Calling `:sys.get_state/1` on a high-traffic GenServer**
`:sys.get_state/1` sends a synchronous message to the GenServer, pausing it while it
serializes state. On a busy GenServer, this can block for seconds. Use only on light-load
GenServers, or read ETS directly.

**3. Accumulating telemetry samples without a size limit**
The MetricsCollector keeps the last 1000 samples. Without this limit, running the notebook
on a busy gateway for hours fills the notebook process memory. Always cap the sample buffer.

**4. Not guarding against process death in Process.info/2**
Between `Process.list()` and `Process.info(pid, ...)`, a process can die. `Process.info`
returns `nil` for dead processes. Filter nils before mapping.

**5. Connecting Livebook to production without a read-only constraint**
Livebook code cells can call any function on the connected node -- including destructive ones.
Use a separate read-only user or restrict Livebook to a staging node for exploratory work.

---

## Resources

- [Livebook documentation](https://livebook.dev) -- setup, runtime connections, smart cells
- [Kino](https://hexdocs.pm/kino/Kino.html) -- interactive widgets, DataTable, VegaLite integration
- [Explorer](https://hexdocs.pm/explorer/Explorer.html) -- DataFrame API reference
- [VegaLite](https://hexdocs.pm/vega_lite/VegaLite.html) -- chart specification DSL
- [Connecting Livebook to a remote node](https://news.livebook.dev/announcing-livebook-0.6-2RnYHg) -- secure attachment guide
