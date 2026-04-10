# 49 — Livebook Integration (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-6 horas  
**Área**: Livebook · Kino · VegaLite · Explorer · Observabilidad

---

## Contexto

Livebook es el entorno de notebooks interactivo de Elixir. Va mucho más allá de Jupyter: permite
conectarse a nodos Elixir en producción, explorar estado de GenServers en tiempo real, visualizar
métricas con VegaLite y analizar DataFrames con Explorer. En este capstone construirás notebooks
que sirven como herramientas de diagnóstico y análisis para sistemas en producción.

---

## Setup del entorno

```bash
# Instalar Livebook globalmente
mix escript.install hex livebook
livebook server

# O via Docker
docker run -p 8080:8080 -p 8081:8081 --pull always ghcr.io/livebook-dev/livebook

# Conectar a un nodo de producción remoto (read-only)
# En el nodo de producción, asegurarse que acepta conexiones distribuidas:
# vm.args: -name prod@10.0.0.1 -setcookie my-secret-cookie
# En Livebook: Hub → Attached Node → configurar nombre y cookie
```

---

## Ejercicio 1 — Notebook de exploración del sistema

Crea un notebook que inspecciona el estado de la aplicación en tiempo real.

### Estructura del notebook (cells)

```elixir
# Cell 1: Setup — conectar dependencias
Mix.install([
  {:kino, "~> 0.12"},
  {:vega_lite, "~> 0.1"},
  {:kino_vega_lite, "~> 0.1"},
  {:explorer, "~> 0.8"}
])

alias VegaLite, as: Vl
alias Explorer.DataFrame
alias Explorer.Series
```

```elixir
# Cell 2: Inspeccionar procesos del nodo
processes =
  Process.list()
  |> Enum.map(fn pid ->
    info = Process.info(pid, [:registered_name, :memory, :message_queue_len, :reductions])
    %{
      pid:       inspect(pid),
      name:      info[:registered_name] |> to_string(),
      memory_kb: div(info[:memory] || 0, 1024),
      msg_queue: info[:message_queue_len] || 0,
      reductions: info[:reductions] || 0
    }
  end)
  |> Enum.sort_by(& &1.memory_kb, :desc)
  |> Enum.take(20)

# Mostrar como tabla interactiva
Kino.DataTable.new(processes,
  name: "Top 20 Processes by Memory",
  keys: [:name, :memory_kb, :msg_queue, :reductions]
)
```

```elixir
# Cell 3: Árbol de supervisión
defmodule TreeInspector do
  def supervisor_tree(sup_pid) do
    children = Supervisor.which_children(sup_pid)
    %{
      pid:      inspect(sup_pid),
      name:     process_name(sup_pid),
      children: Enum.map(children, &child_info/1)
    }
  rescue
    _ -> %{pid: inspect(sup_pid), name: process_name(sup_pid), children: []}
  end

  defp child_info({id, pid, :supervisor, _}) when is_pid(pid) do
    supervisor_tree(pid) |> Map.put(:id, id) |> Map.put(:type, :supervisor)
  end
  defp child_info({id, pid, :worker, mods}) when is_pid(pid) do
    %{id: id, pid: inspect(pid), type: :worker, module: hd(mods), name: process_name(pid)}
  end
  defp child_info({id, :undefined, _, _}), do: %{id: id, status: :not_started}

  defp process_name(pid) do
    case Process.info(pid, :registered_name) do
      {:registered_name, name} when name != [] -> name
      _ -> :unnamed
    end
  end
end

tree = TreeInspector.supervisor_tree(MyApp.Supervisor)
Kino.Tree.new(tree)
```

### Requisitos

- El notebook funciona tanto en nodo local como en nodo remoto adjunto
- `Kino.DataTable` con sorting y filtrado en la tabla de procesos
- Árbol de supervisión navegable con `Kino.Tree`
- Cell con refresh periódico: `Kino.animate/2` para actualizar cada N segundos
- Documentar en markdown cells el propósito de cada sección

---

## Ejercicio 2 — Análisis de métricas con VegaLite

Visualiza métricas de telemetry y estadísticas del sistema en gráficos interactivos.

### Recolección de métricas `:telemetry`

```elixir
# Cell: Capturar eventos de telemetry en tiempo real
defmodule MetricsCollector do
  use GenServer

  def start_link, do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def get_samples, do: GenServer.call(__MODULE__, :get_samples)

  def init(_) do
    :telemetry.attach_many(
      "livebook-collector",
      [
        [:phoenix, :endpoint, :stop],
        [:my_app, :repo, :query],
        [:oban, :job, :stop]
      ],
      &handle_event/4,
      nil
    )
    {:ok, []}
  end

  def handle_event(event, measurements, metadata, _) do
    sample = %{
      event:     Enum.join(event, "."),
      timestamp: DateTime.utc_now(),
      duration_ms: System.convert_time_unit(
        measurements[:duration] || 0, :native, :millisecond
      ),
      metadata: metadata
    }
    GenServer.cast(__MODULE__, {:add, sample})
  end

  def handle_cast({:add, sample}, samples) do
    {:noreply, [sample | Enum.take(samples, 999)]}  # últimas 1000 muestras
  end

  def handle_call(:get_samples, _, samples), do: {:reply, samples, samples}
end

{:ok, _} = MetricsCollector.start_link()
```

```elixir
# Cell: Gráfico de latencia HTTP en tiempo real
# Actualiza automáticamente cada 2 segundos

widget = Vl.new(width: 700, height: 300, title: "HTTP Request Latency (ms)")
|> Vl.mark(:line)
|> Vl.encode_field(:x, "timestamp", type: :temporal, title: "Time")
|> Vl.encode_field(:y, "duration_ms", type: :quantitative, title: "Latency (ms)")
|> Kino.VegaLite.new()

Kino.animate(widget, 2_000, fn _ ->
  samples =
    MetricsCollector.get_samples()
    |> Enum.filter(&(&1.event == "phoenix.endpoint.stop"))
    |> Enum.take(100)

  Kino.VegaLite.push_many(widget, samples)
  widget
end)
```

### Histograma de latencia de queries DB

```elixir
db_samples = MetricsCollector.get_samples()
|> Enum.filter(&(&1.event == "my_app.repo.query"))
|> Enum.map(&%{duration_ms: &1.duration_ms})

Vl.new(width: 600, height: 300, title: "DB Query Latency Distribution")
|> Vl.mark(:bar)
|> Vl.encode_field(:x, "duration_ms",
    type: :quantitative,
    bin: %{step: 10},
    title: "Duration (ms)")
|> Vl.encode_field(:y, "count(*)",
    aggregate: :count,
    type: :quantitative,
    title: "Count")
|> Vl.data_from_values(db_samples)
|> Kino.render()
```

### Requisitos

- Al menos 3 gráficos distintos: line chart (latencia en tiempo real), bar chart (histograma), pie chart (distribución de estados HTTP)
- `Kino.animate/2` para actualización automática de al menos un gráfico
- Datos reales de `:telemetry` — no datos hardcodeados
- Gráficos con títulos, ejes etiquetados y colores apropiados
- Cell con slider de Kino para configurar el intervalo de tiempo del gráfico

---

## Ejercicio 3 — Análisis de datos con Explorer (DataFrame)

Usa Explorer para analizar logs y datos de la aplicación como si fuera pandas.

### Análisis de logs de Oban

```elixir
# Cell: Cargar historial de jobs de Oban como DataFrame
import Ecto.Query

jobs_data =
  from(j in Oban.Job,
    where: j.state in ["completed", "discarded"],
    where: j.attempted_at > ago(7, "day"),
    select: %{
      worker:      j.worker,
      queue:       j.queue,
      state:       j.state,
      attempt:     j.attempt,
      duration_ms: fragment("EXTRACT(EPOCH FROM (? - ?)) * 1000",
                    j.completed_at, j.attempted_at),
      inserted_at: j.inserted_at
    }
  )
  |> MyApp.Repo.all()

df = DataFrame.new(jobs_data)
```

```elixir
# Cell: Análisis de throughput por worker
df
|> DataFrame.group_by(["worker", "state"])
|> DataFrame.summarise(
    count:       count(col("worker")),
    avg_duration: mean(col("duration_ms")),
    p95_duration: quantile(col("duration_ms"), 0.95)
  )
|> DataFrame.sort_by(desc: col("count"))
|> Kino.DataTable.new(name: "Worker Performance Summary")
```

```elixir
# Cell: Jobs fallidos — análisis de errores
failed =
  df
  |> DataFrame.filter(col("state") == "discarded")
  |> DataFrame.group_by("worker")
  |> DataFrame.summarise(failures: count(col("worker")))
  |> DataFrame.sort_by(desc: col("failures"))

# Visualizar como bar chart
Vl.new(width: 500, height: 300, title: "Failed Jobs by Worker (last 7 days)")
|> Vl.mark(:bar, color: "#e45756")
|> Vl.encode_field(:x, "worker", type: :nominal, title: "Worker", sort: "-y")
|> Vl.encode_field(:y, "failures", type: :quantitative, title: "Failure Count")
|> Vl.data_from_values(DataFrame.to_rows(failed))
|> Kino.render()
```

### Análisis de patrones de uso

```elixir
# Cell: Heatmap de actividad por hora del día y día de semana
df
|> DataFrame.mutate(
    hour:    extract_hour(col("inserted_at")),
    weekday: extract_weekday(col("inserted_at"))
  )
|> DataFrame.group_by(["hour", "weekday"])
|> DataFrame.summarise(job_count: count(col("worker")))
|> then(fn summary ->
  Vl.new(width: 600, height: 200, title: "Job Activity Heatmap")
  |> Vl.mark(:rect)
  |> Vl.encode_field(:x, "hour", type: :ordinal, title: "Hour of Day")
  |> Vl.encode_field(:y, "weekday", type: :ordinal, title: "Day of Week")
  |> Vl.encode_field(:color, "job_count", type: :quantitative, title: "Jobs")
  |> Vl.data_from_values(DataFrame.to_rows(summary))
  |> Kino.render()
end)
```

### Requisitos

- DataFrame cargado desde Ecto query real (no CSV fake)
- Al menos 4 operaciones de Explorer: `group_by`, `summarise`, `filter`, `mutate`, `sort_by`
- Al menos 2 visualizaciones con VegaLite integradas al análisis
- `Kino.DataTable` para mostrar DataFrames interactivos
- Exports: cell que exporta el análisis a CSV con `DataFrame.to_csv/2`

---

## Ejercicio 4 — Smart Cells y notebook como herramienta de diagnóstico

Usa Smart Cells de Kino para interfaces interactivas de diagnóstico.

### Form interactivo de diagnóstico

```elixir
# Cell: Smart Cell para explorar estado de un GenServer específico
form = Kino.Control.form(
  [
    module: Kino.Input.text("GenServer module", default: "MyApp.Cache.Server"),
    include_state: Kino.Input.checkbox("Include full state", default: false)
  ],
  submit: "Inspect"
)

Kino.listen(form, fn %{data: data} ->
  module = String.to_existing_atom("Elixir." <> data.module)
  pid = Process.whereis(module)

  output = if pid do
    info = Process.info(pid, [:memory, :message_queue_len, :reductions, :status])
    state = if data.include_state, do: :sys.get_state(pid), else: :hidden

    %{
      pid:        inspect(pid),
      memory_kb:  div(info[:memory], 1024),
      msg_queue:  info[:message_queue_len],
      reductions: info[:reductions],
      status:     info[:status],
      state:      state
    }
  else
    %{error: "Process #{data.module} not found"}
  end

  Kino.Tree.new(output)
  |> Kino.render()
end)

form
```

### Dashboard de salud del sistema

```elixir
# Cell: Panel de salud actualizable con botón
refresh_button = Kino.Control.button("Refresh")
output = Kino.Output.new()

Kino.listen(refresh_button, fn _ ->
  health = %{
    node:           node(),
    uptime_s:        :erlang.statistics(:wall_clock) |> elem(0) |> div(1000),
    process_count:   length(Process.list()),
    memory: %{
      total_mb:    div(:erlang.memory(:total), 1_048_576),
      processes_mb: div(:erlang.memory(:processes), 1_048_576),
      ets_mb:      div(:erlang.memory(:ets), 1_048_576),
      binary_mb:   div(:erlang.memory(:binary), 1_048_576)
    },
    schedulers:     System.schedulers_online(),
    run_queue:      :erlang.statistics(:run_queue)
  }

  Kino.render(Kino.Tree.new(health), to: output)
end)

Kino.Layout.grid([refresh_button, output], columns: 1)
```

### Notebook como reporte reproducible

```elixir
# Cell final: Exportar hallazgos como reporte Markdown

report_lines = [
  "# System Health Report",
  "Generated: #{DateTime.utc_now() |> to_string()}",
  "Node: #{node()}",
  "",
  "## Process Summary",
  "Total processes: #{length(Process.list())}",
  "Memory used (MB): #{div(:erlang.memory(:total), 1_048_576)}",
  "",
  "## Oban Jobs (last 7 days)",
  # ... datos del Ejercicio 3
]

report_md = Enum.join(report_lines, "\n")

# Guardar a archivo
File.write!("/tmp/system_report_#{Date.utc_today()}.md", report_md)
Kino.Markdown.new("Report saved to `/tmp/system_report_*.md`")
```

### Estructura de notebooks a crear

```
notebooks/
├── 01_system_overview.livemd      # Ejercicio 1: procesos y supervisor tree
├── 02_realtime_metrics.livemd     # Ejercicio 2: VegaLite en tiempo real
├── 03_oban_analysis.livemd        # Ejercicio 3: Explorer + DataFrames
└── 04_diagnostics_dashboard.livemd # Ejercicio 4: Smart cells + forms
```

### Formato .livemd

```markdown
# System Overview

## Setup

```elixir
Mix.install([{:kino, "~> 0.12"}, {:vega_lite, "~> 0.1"}])
```

## Process Inspector

<!-- livebook:{"output":true} -->

```elixir
# ... código del notebook
```

<!-- livebook:{"break_markdown":true} -->

### Interpretation

- **High message_queue_len**: indica proceso saturado, revisar back-pressure
- **High memory**: posible memory leak, revisar con `:recon.proc_count(:memory, 5)`
```

### Requisitos

- 4 notebooks `.livemd` creados y funcionales
- Cada notebook se puede abrir en Livebook y ejecutar de arriba a abajo sin errores
- Cells de Markdown explican el propósito y la interpretación de cada visualización
- El notebook de diagnóstico funciona tanto en local como conectado a nodo remoto
- Documentar en el README cómo conectar Livebook a un nodo de producción seguro

---

## Criterios de aceptación

- [ ] `Mix.install/1` en cada notebook instala dependencias correctamente
- [ ] `Kino.DataTable` con datos reales de procesos o DB
- [ ] Al menos un gráfico de VegaLite con `Kino.animate/2` (actualización automática)
- [ ] Explorer DataFrame con `group_by + summarise` sobre datos reales
- [ ] `Kino.Control.form` funcional con validación y output dinámico
- [ ] Los 4 notebooks `.livemd` son válidos (formato correcto)
- [ ] README explica setup de Livebook y cómo adjuntar nodo remoto

---

## Retos adicionales (opcional)

- Kino.Mermaid: diagramas de flujo dinámicos del estado del sistema
- Smart cells de Kino para queries SQL interactivas a la DB
- Livebook Teams: compartir notebooks privados con el equipo (conceptual)
- Notebook de performance: detectar automáticamente los 5 procesos con más memoria y sugerir diagnóstico
