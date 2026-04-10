# 26. Memory Profiling y Detección de Leaks con Recon

**Difficulty**: Avanzado

---

## Prerequisites

### Mastered
- Procesos Elixir y GenServer
- ETS: creación de tablas, operaciones básicas
- Binaries: <<>> syntax, pattern matching en binarios
- Garbage collection: concepto general de GC generacional

### Familiarity with
- `:recon` library (agregar `{:recon, "~> 2.5"}` a mix.exs)
- `:erlang.memory/1` y `:erlang.process_info/2`
- Observer: `:observer.start()` para exploración visual

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

- **Analizar** el perfil de memoria de un sistema BEAM en producción usando herramientas no intrusivas
- **Detectar** leaks de binaries reference-counted antes de que causen OOM
- **Evaluar** el impacto de GC pressure en la latencia de procesos individuales
- **Diseñar** estructuras de datos y patrones de proceso que minimicen presión en el GC

---

## Concepts

### Anatomía de la Memoria en BEAM

La memoria en BEAM no es un heap monolítico. Está dividida en regiones con semánticas distintas:

```elixir
# Vista completa del uso de memoria del sistema
:erlang.memory()
# => [
#   total: 52_428_800,         # memoria total
#   processes: 8_388_608,      # suma de heaps de todos los procesos
#   processes_used: 7_340_032, # de esa suma, la parte "viva"
#   system: 44_040_192,        # todo lo que no es proceso (atoms, BIFs, etc.)
#   atom: 1_048_576,           # tabla de atoms
#   atom_used: 524_288,
#   binary: 2_097_152,         # ProcBin + binaries reference-counted
#   code: 16_777_216,          # módulos compilados
#   ets: 786_432               # todas las tablas ETS
# ]
```

### Tipos de Binaries y Por Qué Importa

BEAM tiene dos tipos de binaries, con semánticas de memoria muy diferentes:

```
Binary < 64 bytes (Heap Binary)
├── Almacenado DENTRO del heap del proceso
├── Se copia al hacer send/receive (como cualquier término)
└── GC normal del proceso lo libera

Binary ≥ 64 bytes (Reference-Counted Binary / ProcBin)
├── Almacenado en un heap GLOBAL compartido (off-process)
├── El proceso tiene un ProcBin (puntero + contador de refs)
├── Se libera solo cuando el contador de refs llega a 0
└── ⚠️ Si el proceso no hace GC, el contador nunca baja → LEAK
```

```elixir
# Demostración del tamaño umbral
small = "abc"           # << 64 bytes → heap binary, sin ref counting
large = :crypto.strong_rand_bytes(100)  # ≥ 64 bytes → ref-counted

# Ver el tipo
:erlang.process_info(self(), :binary)
# => {:binary, [{ref_id, size, ref_count}, ...]}
# Solo muestra los ProcBins (≥64 bytes), no los heap binaries
```

### `:recon` — Tu Herramienta de Profiling en Producción

`:recon` está diseñado para ser seguro en producción: tiene límites built-in y no detiene el sistema.

```elixir
# Top 10 procesos por memoria (bytes)
:recon.proc_count(:memory, 10)
# => [{pid, memory_bytes, [registered_name: ..., current_function: ..., ...]}, ...]

# Top 10 por message queue length — detecta procesos acumulando mensajes
:recon.proc_count(:message_queue_len, 10)

# Top 10 por reductions — procesos más activos
:recon.proc_count(:reductions, 10)

# Detectar leaks de binaries:
# Muestra los N procesos con mayor ratio de binary_memory / proc_memory
:recon.bin_leak(10)
# => [{pid, saved_bytes, [info...]}, ...]
# saved_bytes: bytes que se liberarían si el proceso hiciera GC ahora

# Breakdown de memoria por allocator (más granular que :erlang.memory)
:recon_alloc.memory(:usage)
:recon_alloc.memory(:allocated)
:recon_alloc.memory(:allocated_types)
```

### Process Heap: Generational GC de BEAM

```
Proceso en BEAM tiene dos heaps:
┌─────────────────────────────────────┐
│  Old Heap (generación vieja)        │
│  Objetos que sobrevivieron un GC   │
│  → GC costoso (fullsweep)           │
├─────────────────────────────────────┤
│  Young Heap (generación joven)      │
│  Nuevas asignaciones                │
│  → GC barato y frecuente            │
└─────────────────────────────────────┘
```

```elixir
# Ver info detallada del heap de un proceso
:erlang.process_info(pid, :garbage_collection)
# => {:garbage_collection, [
#   max_heap_size: %{...},
#   min_heap_size: 233,
#   fullsweep_after: 65535,  # ← cuántos GC parciales antes de uno completo
#   min_bin_vheap_size: 46422,
#   minor_gcs: 12            # ← número de GC menores desde inicio del proceso
# ]}

# Forzar un GC completo del proceso (útil para debug, NO en producción hot paths)
:erlang.garbage_collect(pid)

# Estadísticas globales de GC
:erlang.statistics(:garbage_collection)
# => {NumberOfGCs, WordsReclaimed, 0}
```

### ETS Memory

```elixir
# Ver cuánta memoria usa una tabla ETS
:ets.info(:my_table, :memory)  # en words (multiply by word size for bytes)
# => 12345

# Convertir a bytes (word size varía: 4 bytes en 32bit, 8 bytes en 64bit)
words = :ets.info(:my_table, :memory)
bytes = words * :erlang.system_info(:wordsize)

# Info completa de la tabla
:ets.info(:my_table)
# => [id: ..., name: :my_table, type: :set, size: N, memory: M, ...]
```

### Trade-offs de Estructuras de Datos y Memoria

| Estructura | Memoria por elemento | GC impact | Sharing |
|------------|---------------------|-----------|---------|
| **List** | ~2 words por nodo | Alto (muchos terms) | No compartida |
| **Tuple** | 1 word header + N | Medio | No compartida |
| **Map (small, <32)** | ~2N words | Medio | Partial (updates copy) |
| **ETS** | Overhead de tabla + datos | Bajo (fuera del heap) | Sí, global |
| **Binary ≥64B** | Off-heap + ProcBin | Riesgo de leak | Ref-counted |
| **Binary <64B** | En heap | Normal GC | Copiada en send |

---

## Exercises

### Exercise 1: Memory Baseline — Radiografía del Sistema

**Problem**

Implementa un módulo `MemoryProfiler` con una función `snapshot/0` que genere un reporte completo del estado de memoria del sistema BEAM en ese momento. El reporte debe incluir:

1. **Total memory breakdown**: atom, binary, code, ets, processes (en MB con porcentajes)
2. **Top 5 procesos por memoria**: pid, nombre registrado si existe, memoria en KB, función actual
3. **ETS tables summary**: nombre/id de cada tabla, número de registros, memoria en KB
4. **Alerta** si algún proceso supera el 5% del total de memoria de procesos

```
=== Memory Snapshot (2026-04-10 14:32:01) ===

System Memory Breakdown:
  Atom:      1.2 MB  (2.3%)
  Binary:    5.8 MB  (11.1%)
  Code:     18.4 MB  (35.2%)
  ETS:       2.1 MB  (4.0%)
  Processes: 8.2 MB  (15.7%)
  Other:    16.6 MB  (31.7%)
  ─────────────────────────
  Total:    52.3 MB

Top 5 Processes by Memory:
  1. #PID<0.123.0> (Elixir.MyApp.Cache)    2,048 KB   waiting
  2. #PID<0.84.0>  (code_server)             512 KB   receive
  ...

ETS Tables:
  :my_cache          1,024 records   512 KB
  :inet_cache           23 records     4 KB
  ...
```

**Hints**

- `:erlang.memory()` retorna keywords en bytes — divide por `1024 * 1024` para MB
- Para el nombre de un proceso: `:erlang.process_info(pid, :registered_name)` — puede retornar `{:registered_name, []}` si no tiene nombre
- Para listar todos los procesos del sistema: `Process.list()`
- Para listar todas las tablas ETS: `:ets.all()` retorna lista de table IDs
- `:recon.proc_count(:memory, 5)` ya te da el top 5 — úsalo como base

**One possible solution**

```elixir
defmodule MemoryProfiler do
  def snapshot do
    timestamp = DateTime.utc_now() |> DateTime.to_string()
    IO.puts("\n=== Memory Snapshot (#{timestamp}) ===\n")

    mem = :erlang.memory()
    total = Keyword.fetch!(mem, :total)

    print_breakdown(mem, total)
    print_top_processes(5)
    print_ets_tables()
    check_alerts(mem)
  end

  defp print_breakdown(mem, total) do
    IO.puts("System Memory Breakdown:")

    keys = [:atom, :binary, :code, :ets, :processes]
    other = total - Enum.reduce(keys, 0, fn k, acc -> acc + Keyword.get(mem, k, 0) end)

    Enum.each(keys, fn key ->
      bytes = Keyword.get(mem, key, 0)
      mb = Float.round(bytes / (1024 * 1024), 1)
      pct = Float.round(bytes / total * 100, 1)
      label = key |> to_string() |> String.capitalize() |> String.pad_trailing(12)
      IO.puts("  #{label} #{mb} MB  (#{pct}%)")
    end)

    # TODO: imprimir "Other" y "Total"
    # TODO: separador con ─
    _ = other
  end

  defp print_top_processes(n) do
    IO.puts("\nTop #{n} Processes by Memory:")

    :recon.proc_count(:memory, n)
    |> Enum.with_index(1)
    |> Enum.each(fn {{pid, mem_bytes, info}, idx} ->
      name = get_process_name(pid, info)
      kb = div(mem_bytes, 1024)
      # TODO: obtener current_function de info y formatear
      IO.puts("  #{idx}. #{inspect(pid)} (#{name})  #{kb} KB")
    end)
  end

  defp get_process_name(pid, info) do
    case Keyword.get(info, :registered_name) do
      name when is_atom(name) and name != [] -> to_string(name)
      _ ->
        # Intentar obtener el nombre del proceso si es un GenServer
        case :erlang.process_info(pid, :dictionary) do
          {:dictionary, dict} -> Keyword.get(dict, :"$initial_call", inspect(pid))
          _ -> inspect(pid)
        end
    end
  end

  defp print_ets_tables do
    IO.puts("\nETS Tables:")

    :ets.all()
    |> Enum.map(fn table ->
      info = :ets.info(table)
      name = Keyword.get(info, :name, table)
      size = Keyword.get(info, :size, 0)
      words = Keyword.get(info, :memory, 0)
      kb = div(words * :erlang.system_info(:wordsize), 1024)
      {name, size, kb}
    end)
    |> Enum.sort_by(fn {_, _, kb} -> -kb end)
    |> Enum.each(fn {name, size, kb} ->
      name_str = "#{name}" |> String.pad_trailing(24)
      IO.puts("  #{name_str} #{size} records  #{kb} KB")
    end)
  end

  defp check_alerts(mem) do
    proc_total = Keyword.get(mem, :processes, 0)
    threshold = trunc(proc_total * 0.05)

    # TODO: iterar sobre Process.list() y alertar si alguno supera threshold
    _ = threshold
    :ok
  end
end
```

---

### Exercise 2: Binary Leak Detection — Detectar el Leak Antes del OOM

**Problem**

Los leaks de binaries reference-counted son uno de los problemas de memoria más comunes en sistemas Elixir de larga duración. Son difíciles de detectar porque no aparecen en el heap del proceso hasta el próximo GC.

Tu tarea:

1. Implementa un `BinaryLeaker` — un GenServer que intencionalmente acumula referencias a binaries grandes sin hacer GC
2. Implementa un `BinaryLeakDetector` que use `:recon.bin_leak/1` para detectar el leak
3. Implementa la corrección: forzar GC periódico en el proceso que acumula

El ciclo completo debe verse así:
```elixir
# Iniciar el leaker
{:ok, pid} = BinaryLeaker.start_link()

# Alimentar binaries grandes
for _ <- 1..1000, do: BinaryLeaker.store(:crypto.strong_rand_bytes(1024))

# Detectar el leak
BinaryLeakDetector.report()
# => [
#   {pid, 1_024_000, [registered_name: BinaryLeaker, ...]},
#   ...
# ]

# Corregir: forzar GC
BinaryLeaker.force_gc()

# Verificar que el leak desapareció
BinaryLeakDetector.report()
# => []  (o significativamente menos)
```

**Hints**

- Un proceso acumula refs a ProcBins simplemente guardándolos en su state
- `:recon.bin_leak/1` hace: por cada proceso, mide `binary_memory`, fuerza GC, mide de nuevo. Si la diferencia es grande → hay refs sueltas
- El segundo argumento de `:recon.bin_leak/1` es el número de procesos a mostrar
- `Process.flag(:fullsweep_after, 0)` fuerza un fullsweep GC en el próximo GC del proceso — útil para procesos long-lived con mucho state
- Para forzar GC inmediatamente: `:erlang.garbage_collect(pid)`

**One possible solution**

```elixir
defmodule BinaryLeaker do
  use GenServer

  # Almacena binaries en el state → mantiene referencias vivas
  # El GC del proceso no libera los ProcBins porque siguen referenciados
  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, [], Keyword.put(opts, :name, __MODULE__))

  def store(binary) when is_binary(binary),
    do: GenServer.cast(__MODULE__, {:store, binary})

  def force_gc do
    pid = Process.whereis(__MODULE__)
    # Forzar un GC completo (fullsweep)
    :erlang.garbage_collect(pid, type: :major)
    :ok
  end

  def binary_count, do: GenServer.call(__MODULE__, :count)

  def init([]), do: {:ok, []}

  def handle_cast({:store, binary}, state) do
    # Acumulamos la referencia → el ProcBin no se libera
    {:noreply, [binary | state]}
  end

  def handle_call(:count, _from, state), do: {:reply, length(state), state}
end

defmodule BinaryLeakDetector do
  def report(top_n \\ 10) do
    IO.puts("\n=== Binary Leak Report ===")

    results = :recon.bin_leak(top_n)

    case results do
      [] ->
        IO.puts("No significant binary leaks detected.")

      leaks ->
        Enum.each(leaks, fn {pid, saved_bytes, info} ->
          name = Keyword.get(info, :registered_name, inspect(pid))
          kb = div(saved_bytes, 1024)
          IO.puts("  #{inspect(pid)} (#{name}): #{kb} KB would be freed by GC")
        end)
    end

    results
  end

  def watch(interval_ms \\ 5_000, top_n \\ 5) do
    # TODO: iniciar un proceso que llame a report/1 cada interval_ms
    # Útil para monitoreo continuo en staging
    spawn(fn ->
      Stream.repeatedly(fn ->
        :timer.sleep(interval_ms)
        report(top_n)
      end)
      |> Stream.run()
    end)
  end
end
```

---

### Exercise 3: GC Pressure Analysis — Medir el Costo de la Garbage Collection

**Problem**

Un proceso con state que crece continuamente puede experimentar GC pressure severa: el GC se vuelve más frecuente, más costoso, y eventualmente causa pause times notables.

Implementa un experimento que:

1. Cree un `GrowingState` GenServer cuyo state crece en cada operación
2. Mida la **frecuencia de GC** y **palabras reclamadas** usando `:erlang.statistics(:garbage_collection)`
3. Compare GC pressure con tres estrategias de state:
   - **Estrategia A**: acumular todo en una lista (máxima presión)
   - **Estrategia B**: mantener solo los últimos N elementos (presión controlada)
   - **Estrategia C**: delegar datos al ETS (mínima presión en el proceso)
4. Muestre el número de GC runs y el heap size final de cada proceso

```elixir
GCPressureAnalysis.compare_strategies(operations: 10_000, keep_last: 100)

# Estrategia A (acumula todo):
#   GC runs: 847   Heap size: 8.2 MB   GC time: 234ms

# Estrategia B (ring buffer N=100):
#   GC runs: 12    Heap size: 48 KB    GC time: 3ms

# Estrategia C (ETS):
#   GC runs: 3     Heap size: 12 KB    GC time: <1ms
```

**Hints**

- `:erlang.statistics(:garbage_collection)` retorna `{n_gcs, words_reclaimed, 0}` — llámalo antes y después
- Para el heap size actual: `:erlang.process_info(pid, :total_heap_size)` — en words
- La diferencia entre minor GC y major GC importa: `:erlang.process_info(pid, :garbage_collection)` tiene `minor_gcs`
- ETS no está sujeto al GC del proceso — los datos viven fuera del heap del proceso dueño
- Para medir tiempo de GC: puedes instrumentar con `Process.flag(:garbage_collection, ...)` — pero es experimental. Alternativamente mide el tiempo total de las operaciones.

**One possible solution**

```elixir
defmodule GCPressureAnalysis do
  def compare_strategies(opts \\ []) do
    ops = Keyword.get(opts, :operations, 5_000)
    keep = Keyword.get(opts, :keep_last, 100)

    IO.puts("=== GC Pressure Analysis (#{ops} operations) ===\n")

    run_strategy("Estrategia A (acumula todo)", fn -> strategy_a(ops) end)
    run_strategy("Estrategia B (ring buffer N=#{keep})", fn -> strategy_b(ops, keep) end)
    run_strategy("Estrategia C (ETS)", fn -> strategy_c(ops) end)
  end

  defp run_strategy(name, fun) do
    {gc_before, _, _} = :erlang.statistics(:garbage_collection)

    {elapsed_us, pid} = :timer.tc(fun)

    {gc_after, words_reclaimed, _} = :erlang.statistics(:garbage_collection)
    gc_runs = gc_after - gc_before

    heap_info = if Process.alive?(pid) do
      {:total_heap_size, words} = :erlang.process_info(pid, :total_heap_size)
      words * :erlang.system_info(:wordsize)
    else
      0
    end

    IO.puts("#{name}:")
    IO.puts("  GC runs: #{gc_runs}")
    IO.puts("  Words reclaimed: #{words_reclaimed}")
    IO.puts("  Final heap: #{div(heap_info, 1024)} KB")
    IO.puts("  Elapsed: #{div(elapsed_us, 1000)}ms\n")
  end

  # Acumula todo en el state del proceso
  defp strategy_a(ops) do
    pid = spawn(fn ->
      Enum.reduce(1..ops, [], fn i, acc ->
        # Cada operación agrega datos al state
        [:crypto.strong_rand_bytes(64) | acc]
        |> tap(fn _ -> if rem(i, 100) == 0, do: receive do: (_ -> :ok), after: (0 -> :ok) end end)
      end)
      # Proceso termina — el GC final lo colecta todo
    end)

    # Esperar a que termine
    ref = Process.monitor(pid)
    receive do
      {:DOWN, ^ref, _, _, _} -> pid
    end
  end

  defp strategy_b(ops, keep) do
    pid = spawn(fn ->
      Enum.reduce(1..ops, [], fn _i, acc ->
        new_entry = :crypto.strong_rand_bytes(64)
        # Mantener solo los últimos `keep` elementos
        [new_entry | acc] |> Enum.take(keep)
      end)
    end)

    ref = Process.monitor(pid)
    receive do: ({:DOWN, ^ref, _, _, _} -> pid)
  end

  defp strategy_c(ops) do
    table = :ets.new(:gc_pressure_test, [:set, :public])

    pid = spawn(fn ->
      Enum.each(1..ops, fn i ->
        # El dato va a ETS — fuera del heap del proceso
        :ets.insert(table, {i, :crypto.strong_rand_bytes(64)})
      end)
    end)

    ref = Process.monitor(pid)
    receive do: ({:DOWN, ^ref, _, _, _} -> :ok)
    :ets.delete(table)
    pid
  end
end
```

---

## Common Mistakes

### 1. Confundir binary memory en `process_info` con el total real

`:erlang.process_info(pid, :binary)` muestra solo los ProcBins que referencia ese proceso, no el total de memoria binaria del sistema. Un mismo ProcBin puede aparecer en múltiples procesos si fue enviado por mensaje.

### 2. Usar `garbage_collect/1` en hot paths de producción

Forzar GC en producción puede causar pauses notables si el proceso tiene un heap grande. `:recon.bin_leak/1` lo hace internamente para medir — úsalo solo para diagnóstico, no como solución permanente.

### 3. Ignorar el `fullsweep_after` flag

El flag `fullsweep_after: 65535` (default) significa que un fullsweep GC ocurre solo cada 65535 minor GCs. Para procesos long-lived con mucho state que crece y se descarta, esto puede acumular memoria no reclamada durante mucho tiempo.

```elixir
# Para procesos que manejan datos temporales grandes:
Process.flag(:fullsweep_after, 10)  # fullsweep más frecuente
```

### 4. Asumir que ETS siempre es mejor para memoria

ETS tiene overhead por registro (metadatos, lookup cost) y su memoria no está limitada por el GC — puede crecer sin control. Sin estrategia de eviction explícita, una tabla ETS es un leak garantizado.

### 5. No considerar binary fragmentation

Muchas referencias a slices del mismo binary grande (sub-binaries) mantienen vivo el binary completo. Un binary de 1MB con 100 procesos que tienen slices de 1 byte cada uno impide que el 1MB se libere.

```elixir
# MAL: slice que mantiene vivo el binario original
<<_::binary-size(100), slice::binary-size(4), _::binary>> = huge_binary
store(slice)  # guarda 4 bytes pero mantiene vivo huge_binary

# BIEN: copiar para romper la referencia
slice_copy = :binary.copy(slice)
store(slice_copy)
```

### 6. No monitorear memoria de atoms

Los atoms son inmortales — nunca se recolectan. Crear atoms dinámicamente (ej: `String.to_atom/1` en inputs de usuario) es un leak garantizado. Usar `String.to_existing_atom/1` o mantener los atoms como strings.

---

## Summary

La memoria en BEAM está distribuida en múltiples regiones con semánticas distintas. Los problemas más comunes son:

- **Binary leaks**: ProcBins que no se liberan porque el proceso no hace GC — detectar con `:recon.bin_leak/1`
- **State acumulación**: GenServers que crecen sin límite — mitigar con ring buffers o delegación a ETS con TTL
- **GC pressure**: procesos con muchas allocations pequeñas y frecuentes — considerar pooling o batching

La herramienta clave para producción es `:recon` — diseñada específicamente para ser segura en nodos vivos sin afectar el servicio.

---

## What's Next

- **Ejercicio 27**: Tracing con `:sys` y `:recon_trace` — ver qué hace tu sistema en producción
- **Ejercicio 28**: Benchmarking con Benchee — medir performance de forma rigurosa
- Investiga `:recon_alloc.sbcs_to_mbcs/0` para entender fragmentación del allocator
- Lee sobre `max_heap_size` para poner límites a procesos individuales: `Process.flag(:max_heap_size, N)`

---

## Resources

- [Recon documentation](https://ferd.github.io/recon/)
- [Fred Hébert — "Erlang in Anger"](https://www.erlang-in-anger.com/) — Capítulos sobre memory y leaks
- [:erlang.process_info/2 — documentación completa](https://www.erlang.org/doc/man/erlang.html#process_info-2)
- [BEAM Book — Memory chapter](https://happi.github.io/theBeamBook/#_memory)
- [Lukas Larsson — "Memory Management in Erlang"](https://www.youtube.com/watch?v=gqYolfkvIhg)
