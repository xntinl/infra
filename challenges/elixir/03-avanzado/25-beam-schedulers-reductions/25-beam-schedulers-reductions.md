# 25. BEAM Schedulers y Reductions

**Difficulty**: Avanzado

---

## Prerequisites

### Mastered
- Concurrencia con procesos Elixir: spawn, send, receive
- GenServer: handle_call, handle_cast, handle_info
- Supervisión y árboles de supervisión
- Task y Task.Supervisor

### Familiarity with
- Conceptos básicos de sistemas operativos: scheduling, time slices
- `:erlang` módulo y funciones de introspección
- Observer (`:observer.start/0`) para visualización del sistema

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

- **Analizar** cómo el scheduler de BEAM distribuye trabajo entre schedulers y por qué esto impacta la latencia del sistema
- **Evaluar los trade-offs** entre ejecutar código CPU-intensivo en schedulers normales vs dirty schedulers
- **Diseñar** sistemas concurrentes que no monopolicen el scheduler y degraden la latencia de otros procesos
- **Diagnosticar** problemas de scheduler starvation en producción usando herramientas de introspección de BEAM

---

## Concepts

### El Scheduler de BEAM: Preemptive Multitasking basado en Reductions

El scheduler de BEAM es fundamentalmente diferente a los schedulers de lenguajes como Go o Java. En lugar de usar time slices basados en tiempo de CPU, BEAM usa **reductions** como unidad de trabajo.

Una **reduction** es aproximadamente una llamada a función. Cada proceso recibe un quantum de **~2000 reductions** antes de ser desalojado del scheduler. Esto tiene consecuencias importantes:

```elixir
# Este loop NO monopoliza el scheduler — cada iteración consume reductions
# y eventualmente BEAM desaloja el proceso
defmodule SafeLoop do
  def run(0), do: :done
  def run(n) do
    # ~1 reduction por llamada recursiva
    run(n - 1)
  end
end

# Este loop SÍ puede causar problemas si invoca NIFs largos
# porque los NIFs por defecto no consumen reductions ni son preemptibles
defmodule DangerousNIF do
  # Un NIF que tarda 100ms bloquea el scheduler entero durante ese tiempo
  def expensive_nif(), do: :my_nif.long_computation()
end
```

### Anatomía del Scheduler de BEAM

```
Schedulers Online (generalmente = número de cores)
├── Scheduler 1 (thread OS dedicado)
│   ├── Run Queue: [Pid1, Pid2, Pid3, ...]
│   └── Current: ejecutando Pid1 (hasta agotar ~2000 reductions)
├── Scheduler 2
│   ├── Run Queue: [Pid4, Pid5, ...]
│   └── Current: ejecutando Pid4
└── ...

Dirty Schedulers (separados de los normales)
├── Dirty CPU Schedulers (por defecto: N schedulers online)
│   └── Para código CPU-bound que no debe desalojarse
└── Dirty IO Schedulers (por defecto: 10)
    └── Para operaciones IO bloqueantes
```

### Funciones de Introspección

```elixir
# Número de schedulers disponibles
:erlang.system_info(:schedulers)
#=> 8

# Schedulers actualmente online (pueden ser menos si el sistema los durmió)
:erlang.system_info(:schedulers_online)
#=> 8

# ID del scheduler que ejecuta el proceso actual (1..N)
:erlang.system_info(:scheduler_id)
#=> 3

# Dirty schedulers
:erlang.system_info(:dirty_cpu_schedulers)
:erlang.system_info(:dirty_io_schedulers)

# Reductions del proceso actual
{:reductions, r} = :erlang.process_info(self(), :reductions)

# Estadísticas globales del scheduler
:erlang.statistics(:scheduler_wall_time)
# Retorna: [{scheduler_id, active_time, total_time}, ...]
# Requiere habilitar primero: :erlang.system_flag(:scheduler_wall_time, true)
```

### Process Priority

BEAM permite ajustar la prioridad de procesos individuales. Las prioridades son relativas, no absolutas:

```elixir
# Niveles de prioridad (de menor a mayor):
# :low -> :normal -> :high -> :max

# :max está reservado para procesos internos de BEAM
# No usar :max en código de aplicación

Process.flag(:priority, :high)   # Este proceso se ejecuta más frecuentemente
Process.flag(:priority, :low)    # Procesos de background, no críticos

# Ejemplo práctico: un proceso de heartbeat que debe responder rápido
defmodule Heartbeat do
  use GenServer

  def init(_) do
    Process.flag(:priority, :high)
    {:ok, %{}}
  end
end
```

### Trade-offs: Normal vs Dirty Schedulers

| Aspecto | Scheduler Normal | Dirty CPU Scheduler | Dirty IO Scheduler |
|---------|-----------------|--------------------|--------------------|
| **Preemptible** | Sí (~2000 red.) | No | No |
| **Impacto en latencia** | Bajo (otros procesos continúan) | Alto si abusa | Bajo (IO no bloquea CPU) |
| **Caso de uso** | Todo el código Elixir normal | NIFs CPU-intensivos largos | NIFs con IO bloqueante |
| **Límite** | N schedulers | N dirty CPU schedulers | 10 dirty IO schedulers |
| **Overhead** | Mínimo | Cambio de contexto extra | Cambio de contexto extra |

### Yielding Voluntario

```elixir
# En casos extremos, un proceso puede ceder el scheduler voluntariamente
# Útil en loops que generan work units grandes sin llamadas a funciones
:erlang.yield()

# Equivalente más idiomático: receive con timeout 0
# (cede el scheduler pero vuelve inmediatamente si no hay mensajes)
receive do
  msg -> handle(msg)
after
  0 -> :continue
end
```

### Scheduler Binding (`+sbt` flags)

Los flags de VM permiten controlar cómo los schedulers se "atan" a cores físicos:

```bash
# Sin binding (defecto): el OS decide en qué core corre cada scheduler thread
iex --erl "+sbt unbound"

# Binding por core (mejor para NUMA): cada scheduler thread se ancla a un core
iex --erl "+sbt db"  # db = default bind

# Útil en servidores NUMA donde memoria local es más rápida
```

---

## Exercises

### Exercise 1: Scheduler Explorer — Visualizar Distribución de Trabajo

**Problem**

Escribe un módulo `SchedulerExplorer` que:

1. Spawne N procesos (donde N = número de schedulers online × 4)
2. Cada proceso reporta en qué scheduler está ejecutándose usando `:erlang.system_info(:scheduler_id)`
3. El módulo recoja todos los reportes y muestre una distribución de cuántos procesos cayeron en cada scheduler
4. Luego mida el **scheduler wall time** antes y después de ejecutar trabajo real: calcula el porcentaje de utilización de cada scheduler durante 2 segundos de carga

El output esperado debe verse así:
```
Scheduler distribution (N processes):
  Scheduler 1: ████████ 12 processes (15.0%)
  Scheduler 2: ██████ 10 processes (12.5%)
  ...

Scheduler utilization (2s window):
  Scheduler 1: 87.3% busy
  Scheduler 2: 91.2% busy
  ...
```

**Hints**

- Habilita wall time antes de medir: `:erlang.system_flag(:scheduler_wall_time, true)`
- Para calcular utilización: `utilization = (active2 - active1) / (total2 - total1) * 100`
- Los procesos recién spawneados pueden migrar entre schedulers — mide el scheduler_id justo antes de que el proceso termine
- Usa `Task.async_stream` o `Task.await_many` para recolectar resultados

**One possible solution**

```elixir
defmodule SchedulerExplorer do
  def distribution do
    n_schedulers = :erlang.system_info(:schedulers_online)
    n_processes = n_schedulers * 4

    # Spawner N procesos y recoger en qué scheduler cayó cada uno
    results =
      1..n_processes
      |> Task.async_stream(
        fn _i ->
          # Hacer algo mínimo para que el proceso sea asignado a un scheduler
          _work = Enum.sum(1..100)
          :erlang.system_info(:scheduler_id)
        end,
        max_concurrency: n_processes,
        ordered: false
      )
      |> Enum.map(fn {:ok, scheduler_id} -> scheduler_id end)

    # Agrupar por scheduler_id
    distribution =
      results
      |> Enum.group_by(& &1)
      |> Enum.map(fn {id, list} -> {id, length(list)} end)
      |> Enum.sort_by(fn {id, _} -> id end)

    total = length(results)

    IO.puts("\nScheduler distribution (#{n_processes} processes):")

    Enum.each(distribution, fn {id, count} ->
      pct = Float.round(count / total * 100, 1)
      bar = String.duplicate("█", div(count * 20, total))
      IO.puts("  Scheduler #{id}: #{bar} #{count} processes (#{pct}%)")
    end)

    distribution
  end

  def utilization(duration_ms \\ 2_000) do
    :erlang.system_flag(:scheduler_wall_time, true)

    # TODO: implementar la captura de wall time snapshot 1
    # snapshot_1 = :erlang.statistics(:scheduler_wall_time)

    # TODO: generar carga durante duration_ms
    # _load_pids = spawn_load_workers(...)

    # TODO: capturar snapshot 2 y calcular delta
    # snapshot_2 = ...

    # TODO: retornar mapa %{scheduler_id => utilization_pct}
    :not_implemented
  end

  defp spawn_load_workers(_n) do
    # TODO: spawnear procesos que hagan trabajo CPU continuo
    # Cada uno debe ejecutar un loop de cálculos por duration_ms
    :not_implemented
  end
end
```

---

### Exercise 2: Reduction Budget Meter

**Problem**

Crea un módulo `ReductionMeter` que permita medir cuántas reductions consume una operación dada. La API debe ser:

```elixir
# Medir las reductions de cualquier función
{result, reductions} = ReductionMeter.measure(fn ->
  Enum.sum(1..10_000)
end)

# Medir múltiples operaciones y comparar
ReductionMeter.compare([
  {"Enum.sum 10k", fn -> Enum.sum(1..10_000) end},
  {"List.foldl 10k", fn -> List.foldl(Enum.to_list(1..10_000), 0, &+/2) end},
  {"Recursive sum 10k", fn -> recursive_sum(10_000) end}
])
```

Además, crea una función `budget_warning/2` que ejecute una función y emita un log de warning si la función consume más de un threshold de reductions:

```elixir
ReductionMeter.budget_warning(fn -> heavy_computation() end, max_reductions: 5_000)
# => [WARNING] Function consumed 12_450 reductions (threshold: 5_000)
```

**Hints**

- `:erlang.process_info(self(), :reductions)` retorna `{:reductions, N}` — llámalo antes y después
- Las reductions incluyen el overhead de llamar a `process_info` misma (~5-10 reductions)
- Para `compare/1`, ordena los resultados por reductions ascendente y muestra una tabla
- El conteo de reductions es por proceso — no se ve afectado por otros procesos corriendo en paralelo

**One possible solution**

```elixir
defmodule ReductionMeter do
  require Logger

  def measure(fun) when is_function(fun, 0) do
    {:reductions, before_r} = :erlang.process_info(self(), :reductions)
    result = fun.()
    {:reductions, after_r} = :erlang.process_info(self(), :reductions)

    # Restar overhead de process_info (aproximado)
    reductions = after_r - before_r

    {result, reductions}
  end

  def compare(named_funs) when is_list(named_funs) do
    results =
      Enum.map(named_funs, fn {name, fun} ->
        {_result, reductions} = measure(fun)
        {name, reductions}
      end)

    # TODO: ordenar por reductions
    # TODO: encontrar el mínimo para calcular el ratio relativo
    # TODO: imprimir tabla formateada con columnas: Name | Reductions | Ratio

    IO.puts("\nReduction comparison:")
    IO.puts(String.duplicate("-", 60))
    # ... implementar tabla
    results
  end

  def budget_warning(fun, opts \\ []) do
    max = Keyword.get(opts, :max_reductions, 2_000)
    {result, reductions} = measure(fun)

    if reductions > max do
      Logger.warning(
        "Function consumed #{reductions} reductions (threshold: #{max})"
      )
    end

    # TODO: retornar {:ok, result} o {:budget_exceeded, result, reductions}
    result
  end
end
```

---

### Exercise 3: Dirty Scheduler — Impacto en Latencia del Sistema

**Problem**

Este ejercicio demuestra el problema central de los schedulers de BEAM: **una tarea CPU-intensiva en un scheduler normal puede degradar la latencia de todos los demás procesos**.

Implementa un benchmark que:

1. Inicie un proceso "canary" que responda a pings con latencia medida en microsegundos
2. Mientras el canary corre, ejecute una tarea CPU-intensiva (simula un NIF largo con `:timer.sleep/1` dentro de un NIF simulado)
3. Mida la latencia p50, p95 y p99 del canary **con y sin** la tarea interferente
4. Muestre cómo dirty schedulers aislan el impacto (simulado con `Task` en un proceso de baja prioridad vs alta prioridad)

```elixir
# Resultado esperado (valores aproximados, varían por máquina):
SchedulerLatency.run_experiment()

# Sin carga:
#   p50: 45μs  p95: 120μs  p99: 890μs

# Con carga en scheduler normal (priority: :normal):
#   p50: 180μs  p95: 2_400μs  p99: 15_000μs  ← degradación severa

# Con carga en proceso :low priority:
#   p50: 52μs  p95: 145μs  p99: 1_100μs  ← mucho mejor
```

**Hints**

- Usa `:timer.tc/1` para medir en microsegundos: `{microseconds, result} = :timer.tc(fn -> ... end)`
- El proceso canary debe ser un GenServer simple que responde a `{:ping, from}` con `{:pong, latency}`
- Para simular carga CPU sin un NIF real: `for _ <- 1..100_000, do: :crypto.strong_rand_bytes(16)`
- La diferencia de prioridad es más visible cuando hay muchos schedulers ocupados — intenta con `--erl "+S 2"` para limitar a 2 schedulers
- Calcula percentiles ordenando la lista de latencias y tomando el índice correspondiente

**One possible solution**

```elixir
defmodule SchedulerLatency do
  use GenServer

  # --- Canary GenServer ---

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def ping do
    start = :erlang.monotonic_time(:microsecond)
    :ok = GenServer.call(__MODULE__, :ping)
    :erlang.monotonic_time(:microsecond) - start
  end

  def init(_), do: {:ok, %{}}

  def handle_call(:ping, _from, state), do: {:reply, :ok, state}

  # --- Experiment ---

  def run_experiment do
    {:ok, _} = start_link([])

    IO.puts("=== Scheduler Latency Experiment ===\n")

    baseline = measure_latencies(1_000, nil)
    print_stats("Sin carga (baseline)", baseline)

    # Carga con prioridad normal
    load_pid = spawn_load(:normal)
    with_normal_load = measure_latencies(1_000, nil)
    Process.exit(load_pid, :kill)
    print_stats("Con carga :normal priority", with_normal_load)

    # TODO: repetir con Process.flag(:priority, :low) en el proceso de carga
    # TODO: mostrar diferencia porcentual en p99

    :ok
  end

  defp spawn_load(priority) do
    spawn(fn ->
      Process.flag(:priority, priority)
      # Loop infinito de trabajo CPU
      Stream.repeatedly(fn ->
        for _ <- 1..10_000, do: :crypto.strong_rand_bytes(16)
      end)
      |> Stream.run()
    end)
  end

  defp measure_latencies(n, _context) do
    # TODO: enviar n pings y recolectar latencias
    # Retornar lista de microsegundos
    Enum.map(1..n, fn _ ->
      # Pequeña pausa para no saturar el mailbox
      :timer.sleep(1)
      ping()
    end)
  end

  defp print_stats(label, latencies) do
    sorted = Enum.sort(latencies)
    p50 = percentile(sorted, 50)
    p95 = percentile(sorted, 95)
    p99 = percentile(sorted, 99)

    IO.puts("#{label}:")
    IO.puts("  p50: #{p50}μs  p95: #{p95}μs  p99: #{p99}μs\n")
  end

  defp percentile(sorted_list, p) do
    idx = round(length(sorted_list) * p / 100) - 1
    Enum.at(sorted_list, max(idx, 0))
  end
end
```

---

## Common Mistakes

### 1. Confundir "schedulers" con "threads del OS"

BEAM crea un thread del OS por scheduler, pero los procesos de Elixir son multiplexados encima de esos threads. **No hay una relación 1:1** entre procesos Elixir y threads del OS. Millones de procesos Elixir corren en N threads (donde N = schedulers).

### 2. Usar Process.flag(:priority, :max)

`:max` está reservado para uso interno de BEAM (emulator processes). Usarlo en código de aplicación puede causar starvation de procesos del sistema e inestabilidad. Usa `:high` como máximo en código de usuario.

### 3. Asumir que más schedulers = más performance

En workloads IO-bound, agregar schedulers no ayuda. La contención de locks internos de BEAM puede incluso hacer que más schedulers empeore el rendimiento. Benchmark siempre antes de ajustar `+S`.

### 4. Ignorar el overhead de dirty schedulers

Dispatch a un dirty scheduler tiene overhead (cambio de contexto, sincronización). Para operaciones cortas (<1ms), el overhead puede superar el beneficio. Solo vale la pena para operaciones de varios milisegundos.

### 5. No deshabilitar scheduler_wall_time después de medir

```elixir
# MAL: dejar wall time habilitado en producción
:erlang.system_flag(:scheduler_wall_time, true)
# ... medición ...

# BIEN: deshabilitar cuando ya no necesitas
:erlang.system_flag(:scheduler_wall_time, false)
```

### 6. Medir scheduler_id una sola vez y asumir que es fijo

Los procesos pueden migrar entre schedulers. El scheduler_id puede cambiar entre llamadas, especialmente si el proceso hace receives que lo suspenden. Para un mapeo estable, ancla el proceso con `:erlang.process_flag(:scheduler, N)` (Erlang 24+) o diseña sin asumir localidad.

---

## Summary

El scheduler de BEAM implementa preemptividad justa basada en reductions, no en tiempo. Esto garantiza que **ningún proceso puede monopolizar la CPU indefinidamente** sin cooperar, porque BEAM lo desaloja automáticamente. Las consecuencias prácticas son:

- Código Elixir puro siempre es preemptible — no necesitas `yield()` explícito
- NIFs son la excepción: bloquean el scheduler a menos que uses dirty schedulers
- Las prioridades de proceso son una herramienta para gestionar latencia, no para garantizar tiempo real
- Scheduler wall time es la herramienta correcta para detectar hotspots de CPU en producción

---

## What's Next

- **Ejercicio 26**: Memory Profiling con recon — detectar leaks de binaries y GC pressure
- **Ejercicio 27**: Tracing en producción — ver qué hace tu sistema sin detenerlo
- Investiga `:erlang.process_flag(:fullsweep_after, N)` para controlar la GC de procesos individuales
- Lee el whitepaper de Joe Armstrong sobre el diseño del scheduler de BEAM

---

## Resources

- [BEAM Book — Schedulers chapter](https://happi.github.io/theBeamBook/#_schedulers)
- [Erlang docs: erl flags +S, +sbt, +SP](https://www.erlang.org/doc/man/erl.html)
- [Recon library documentation](https://ferd.github.io/recon/)
- `:erlang.system_info/1` — documentación oficial de todas las keys disponibles
- [Jesper Louis Andersen — "On Erlang's Scheduler"](https://jlouisramblings.blogspot.com/2013/01/how-erlang-does-scheduling.html)
