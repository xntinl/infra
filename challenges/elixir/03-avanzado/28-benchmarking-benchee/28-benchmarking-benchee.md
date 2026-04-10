# 28. Benchmarking Riguroso con Benchee

**Difficulty**: Avanzado

---

## Prerequisites

### Mastered
- Elixir: Map, Keyword List, String, IO.iodata
- Concurrencia: Task, GenServer, Process
- Mix: dependencias, entornos de desarrollo

### Familiarity with
- Conceptos de benchmarking: warm-up, varianza, outliers
- `{:benchee, "~> 1.0"}` — agregar a `mix.exs` en deps

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

- **Diseñar** experimentos de benchmarking que produzcan resultados estadísticamente válidos
- **Interpretar** métricas de Benchee: IPS, mean, median, std_dev y su significado real
- **Evaluar** estrategias de implementación con diferentes tamaños de input (scaling behavior)
- **Analizar** el throughput de sistemas concurrentes bajo carga variable

---

## Concepts

### ¿Por Qué el Microbenchmarking es Difícil?

Antes de ejecutar cualquier benchmark, es importante entender sus limitaciones:

```
Fuentes de ruido en benchmarks:
├── JIT warm-up: código no compilado se ejecuta más lento al inicio
├── GC pressure: una GC durante la medición distorsiona los resultados
├── CPU throttling: macbooks y servidores con thermal throttling
├── Context switches del OS: el thread del scheduler puede ser interrumpido
├── Branch predictor: el CPU "aprende" patterns en loops repetitivos
└── Cache effects: datos en L1/L2/L3 vs RAM tienen diferente latencia
```

Benchee mitiga estos problemas con warm-up, múltiples iteraciones y estadísticas descriptivas.

### Benchee API

```elixir
# mix.exs
defp deps do
  [
    {:benchee, "~> 1.0", only: :dev},
    {:benchee_html, "~> 1.0", only: :dev}  # para reportes HTML (opcional)
  ]
end
```

```elixir
# Ejemplo completo con todas las opciones relevantes
Benchee.run(
  %{
    "implementación A" => fn -> my_function_a(input) end,
    "implementación B" => fn -> my_function_b(input) end,
  },
  # Tiempo de calentamiento (segundos) — deja que el compilador JIT se estabilice
  warmup: 2,
  # Tiempo de medición por implementación (segundos)
  time: 5,
  # Tiempo para medir memoria (segundos) — 0 = no medir
  memory_time: 2,
  # Tiempo de reducción (operaciones) — 0 = no medir
  reduction_time: 1,
  # Formato de salida
  formatters: [
    Benchee.Formatters.Console,
    {Benchee.Formatters.HTML, file: "benchmarks/results.html"}
  ],
  # Inputs para probar con diferentes tamaños
  inputs: %{
    "small (100)"  => 100,
    "medium (10k)" => 10_000,
    "large (1M)"   => 1_000_000
  }
)
```

### Anatomía de los Resultados de Benchee

```
Name                  ips        average  deviation         median         99th %
implementation A    2.34 K      427.35 μs   ±12.34%       415.12 μs      891.23 μs
implementation B    1.12 K      892.14 μs   ±45.67%       650.00 μs     3456.78 μs

Comparison:
implementation A    2.34 K
implementation B    1.12 K  — 2.09x slower
```

- **ips** (iterations per second): cuántas veces por segundo se puede ejecutar la función
- **average**: media aritmética del tiempo — sensible a outliers
- **deviation** (std_dev %): variabilidad relativa — alto % = resultados poco confiables
- **median**: valor central — más robusto que la media ante outliers
- **99th %**: el percentil 99 — el "peor caso casi real"

### `inputs:` — Probar Comportamiento de Scaling

La feature más importante de Benchee para decisiones arquitectónicas:

```elixir
# Sin inputs: solo ves el comportamiento con UN tamaño de dato
# Con inputs: ves cómo escala cada implementación

Benchee.run(
  %{
    "Enum.find" => fn list -> Enum.find(list, &(&1 == :target)) end,
    "MapSet.member?" => fn set -> MapSet.member?(set, :target) end
  },
  inputs: %{
    "10 elements"      => generate_data(10),
    "1_000 elements"   => generate_data(1_000),
    "100_000 elements" => generate_data(100_000)
  }
)
# Con 10 elements: ambos son ~igual de rápidos
# Con 100_000: Enum.find es O(n), MapSet.member? es O(1) — diferencia clara
```

### `before_scenario` y `after_scenario` — Setup por Input

```elixir
Benchee.run(
  %{
    "with ETS" => fn table ->
      :ets.lookup(table, :key)
    end
  },
  inputs: %{
    "small" => 1_000,
    "large" => 1_000_000
  },
  before_scenario: fn size ->
    # Se ejecuta UNA VEZ por input — setup costoso aquí, no en el benchmark
    table = :ets.new(:bench_table, [:set])
    Enum.each(1..size, fn i -> :ets.insert(table, {i, :data}) end)
    table
  end,
  after_scenario: fn table ->
    :ets.delete(table)
  end
)
```

### Memory Benchmarking

```elixir
Benchee.run(
  %{
    "lista" => fn n -> Enum.to_list(1..n) end,
    "mapset" => fn n -> MapSet.new(1..n) end
  },
  memory_time: 2,  # medir memoria durante 2 segundos
  inputs: %{"10k" => 10_000}
)

# Output incluye:
# Memory usage statistics:
# Name             average  deviation      median         99th %
# lista          156.25 KB    ±0.00%    156.25 KB      156.25 KB
# mapset         312.50 KB    ±0.00%    312.50 KB      312.50 KB
```

### Reduction Time

```elixir
Benchee.run(
  %{
    "recursiva" => fn -> recursive_sum(10_000) end,
    "Enum.sum"  => fn -> Enum.sum(1..10_000) end
  },
  reduction_time: 1  # medir reductions durante 1 segundo
)
# Reduction time correlaciona con cuánto trabajo hace BEAM
# No necesariamente con el tiempo wall-clock (depende de GC, IO, etc.)
```

---

## Exercises

### Exercise 1: Map vs Keyword List — Benchmark de Lookup con Scaling

**Problem**

Implementa un benchmark que compare `Map` vs `Keyword List` para operaciones de lookup con diferentes tamaños y diferentes patrones de acceso.

El benchmark debe probar:

- **Lookup por key existente** — `Map.get(map, key)` vs `Keyword.get(kw, key)`
- **Lookup por key inexistente** — mismo comparativo
- **Tamaños**: 5 keys, 20 keys, 100 keys, 500 keys

Además, incluye una función `generate_data/1` que construya ambas estructuras con el mismo contenido para comparación justa, y un `before_scenario` que prepare los datos sin contaminar el tiempo medido.

Expectativa de aprendizaje: ver en qué punto el Map supera al Keyword List y entender por qué.

```elixir
# Resultado esperado (aproximado):
# ##### With input: 5 keys #####
# Keyword.get (found):  12.45 M ops/s
# Map.get (found):       8.23 M ops/s  ← Map es más lento para N pequeño
#
# ##### With input: 100 keys #####
# Map.get (found):       9.11 M ops/s
# Keyword.get (found):   1.23 M ops/s  ← Keyword degrada linealmente
```

**Hints**

- Un Keyword List es una lista enlazada — el lookup es O(n)
- Un Map es un HAMT (Hash Array Mapped Trie) — el lookup es O(log n) con constante pequeña
- Para el caso de "key inexistente", el Keyword List debe recorrer toda la lista — el peor caso
- Usa `before_scenario` para construir las estructuras una vez por input size
- Benchee retorna un `%Benchee.Suite{}` — puedes usarlo para análisis posterior

**One possible solution**

```elixir
# benchmarks/map_vs_keyword.exs
defmodule MapVsKeyword do
  def run do
    Benchee.run(
      %{
        "Map.get (found)" => fn {map, key, _} ->
          Map.get(map, key)
        end,
        "Keyword.get (found)" => fn {kw, key, _} ->
          Keyword.get(kw, key)
        end,
        "Map.get (missing)" => fn {map, _, missing_key} ->
          Map.get(map, missing_key, :default)
        end,
        "Keyword.get (missing)" => fn {kw, _, missing_key} ->
          Keyword.get(kw, missing_key, :default)
        end
      },
      inputs: %{
        "5 keys"   => 5,
        "20 keys"  => 20,
        "100 keys" => 100,
        "500 keys" => 500
      },
      before_scenario: fn size ->
        # Construir ambas estructuras con el mismo contenido
        pairs = Enum.map(1..size, fn i -> {:"key_#{i}", "value_#{i}"} end)
        kw_list = pairs
        map = Map.new(pairs)

        # Elegir una key existente (la del medio)
        existing_key = :"key_#{div(size, 2)}"
        missing_key = :nonexistent_key_xyz

        {map, kw_list, existing_key, missing_key}
      end,
      # TODO: transformar el input para cada benchmark fn
      # El before_scenario retorna {map, kw, existing, missing}
      # pero cada fn recibe ese mismo valor
      warmup: 2,
      time: 5
    )
  end
end

MapVsKeyword.run()
```

---

### Exercise 2: String Concatenation — ¿Qué Estrategia Escala Mejor?

**Problem**

Elixir tiene múltiples formas de construir strings. Cada una tiene características de performance muy diferentes dependiendo del número de piezas y su tamaño.

Benchmarkea estas estrategias para construir un string grande desde N fragmentos:

- **`<>` recursivo**: `Enum.reduce(parts, "", fn p, acc -> acc <> p end)`
- **`IO.iodata_to_binary`**: acumular una lista de strings y convertir al final
- **`String.join`**: `Enum.join(parts, "")`
- **`Enum.map |> Enum.join`**: cuando hay transformación previa
- **`:erlang.iolist_to_binary`**: versión Erlang de iodata — a veces más rápida

Prueba con: 10 partes, 100 partes, 1000 partes, 10000 partes.

El benchmark debe también medir **memory_time** para ver cuánta memoria intermedia genera cada estrategia.

```elixir
# Expectativa de aprendizaje:
# - <> recursivo: O(n²) — cada concatenación copia el string acumulado
# - iodata: O(n) — construye la lista sin copias, solo una al final
# - String.join: similar a iodata internamente
```

**Hints**

- `IO.iodata_to_binary/1` acepta listas anidadas de strings/binaries — no necesitas flatten
- Usa `before_scenario` para generar la lista de partes (strings pequeños de tamaño fijo)
- Cada parte debe tener el mismo tamaño para una comparación justa — ej: strings de 10 bytes
- El patrón iodata es: acumular en lista `[part | acc]` (prepend, O(1)) y convertir al final
- Mide con `memory_time: 2` — la diferencia de memoria entre `<>` e iodata es dramática

**One possible solution**

```elixir
# benchmarks/string_concat.exs
defmodule StringConcatBench do
  def run do
    Benchee.run(
      %{
        "<> recursivo" => fn parts ->
          Enum.reduce(parts, "", fn p, acc -> acc <> p end)
        end,
        "IO.iodata_to_binary" => fn parts ->
          # Construir iodata list y convertir al final
          iodata = Enum.reduce(parts, [], fn p, acc -> [acc | p] end)
          IO.iodata_to_binary(iodata)
        end,
        "String.join" => fn parts ->
          Enum.join(parts, "")
        end,
        ":erlang.iolist_to_binary" => fn parts ->
          :erlang.iolist_to_binary(parts)
        end
      },
      inputs: %{
        "10 parts"    => 10,
        "100 parts"   => 100,
        "1_000 parts" => 1_000,
        "10_000 parts" => 10_000
      },
      before_scenario: fn n ->
        # Generar lista de N strings de 10 bytes cada uno
        Enum.map(1..n, fn _ -> :crypto.strong_rand_bytes(10) |> Base.encode16() end)
      end,
      warmup: 2,
      time: 5,
      memory_time: 2,
      formatters: [Benchee.Formatters.Console]
    )
  end
end

StringConcatBench.run()
```

---

### Exercise 3: Concurrent Benchmark — Throughput de GenServer bajo Carga Paralela

**Problem**

Un GenServer serializa todas las operaciones en un mailbox. Esto es correcto para estado compartido, pero tiene implicaciones de throughput. Este benchmark mide cómo escala el throughput de un GenServer cuando el número de clientes aumenta.

Implementa:

1. `CounterServer` — GenServer simple que incrementa un contador
2. Un benchmark que prueba throughput con **1, 10, 100 clientes concurrentes**
3. Compara con una alternativa sin serialización: usar `:atomics` (array de enteros atómicos de BEAM)

La métrica clave no es IPS individual sino **throughput total** (operaciones/segundo del sistema completo).

```elixir
# Resultado esperado (aproximado):
# 1 client:
#   GenServer: 125k ops/s  — cuello de botella: mailbox serializado
#   :atomics:  890k ops/s
#
# 10 clients:
#   GenServer: 130k ops/s  — ¡apenas sube! El GenServer es el cuello
#   :atomics: 4.2M ops/s   — escala linealmente con clientes
#
# 100 clients:
#   GenServer: 128k ops/s  — meseta total
#   :atomics: 12.1M ops/s
```

**Hints**

- Para el benchmark de N clientes: spawna N Tasks que ejecuten M operaciones cada una y mide el tiempo total
- La métrica a comparar es `total_ops / elapsed_ms * 1000` (ops/segundo)
- `:atomics.new(1, signed: false)` crea un array de 1 entero atómico no signado
- `:atomics.add(ref, 1, 1)` incrementa atómicamente la posición 1 del array
- Benchee no tiene soporte nativo para este tipo de benchmark "wall clock total" — usa `:timer.tc` manualmente o un `before_scenario`/`after_scenario` para medir el tiempo de todo el grupo

**One possible solution**

```elixir
defmodule CounterServer do
  use GenServer

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, 0, opts)

  def increment(pid), do: GenServer.call(pid, :inc)
  def get(pid), do: GenServer.call(pid, :get)

  def init(count), do: {:ok, count}

  def handle_call(:inc, _from, count), do: {:reply, count + 1, count + 1}
  def handle_call(:get, _from, count), do: {:reply, count, count}
end

defmodule ConcurrentBench do
  @ops_per_client 1_000

  def run do
    IO.puts("=== Concurrent Throughput Benchmark ===\n")

    for n_clients <- [1, 10, 100] do
      gs_throughput = bench_genserver(n_clients)
      atomic_throughput = bench_atomics(n_clients)

      IO.puts("#{n_clients} clients:")
      IO.puts("  GenServer: #{format_ops(gs_throughput)} ops/s")
      IO.puts("  :atomics:  #{format_ops(atomic_throughput)} ops/s\n")
    end
  end

  defp bench_genserver(n_clients) do
    {:ok, pid} = CounterServer.start_link()

    {elapsed_us, _} = :timer.tc(fn ->
      tasks = Enum.map(1..n_clients, fn _ ->
        Task.async(fn ->
          Enum.each(1..@ops_per_client, fn _ ->
            CounterServer.increment(pid)
          end)
        end)
      end)
      Task.await_many(tasks, :infinity)
    end)

    GenServer.stop(pid)
    total_ops = n_clients * @ops_per_client
    total_ops / elapsed_us * 1_000_000
  end

  defp bench_atomics(n_clients) do
    # TODO: crear :atomics ref, benchmarkear con N tasks concurrentes
    # Cada task hace @ops_per_client llamadas a :atomics.add/3
    _ = n_clients
    0.0
  end

  defp format_ops(ops_per_sec) do
    cond do
      ops_per_sec >= 1_000_000 -> "#{Float.round(ops_per_sec / 1_000_000, 1)}M"
      ops_per_sec >= 1_000 -> "#{Float.round(ops_per_sec / 1_000, 1)}k"
      true -> "#{round(ops_per_sec)}"
    end
  end
end

ConcurrentBench.run()
```

---

## Common Mistakes

### 1. No usar warm-up

Sin warm-up, las primeras iteraciones son más lentas por compilación lazy, cold caches, y inicialización del GC. Benchee hace warm-up por defecto (`warmup: 2`) — no lo pongas en 0 a menos que tengas una razón muy específica.

### 2. Incluir setup en la función benchmarked

```elixir
# MAL: la creación de datos forma parte del tiempo medido
%{"bad bench" => fn -> data = generate_data(1000); process(data) end}

# BIEN: datos preparados antes de la medición
%{"good bench" => fn data -> process(data) end}
# con inputs o before_scenario para generar data
```

### 3. Comparar implementaciones con diferentes cargas de trabajo

Si "implementación A" hace más trabajo real que "implementación B", el benchmark no compara lo que crees. Asegúrate de que ambas producen el mismo resultado para el mismo input.

### 4. Ignorar std_dev alto

Un `deviation` mayor al 15-20% indica que los resultados son poco confiables. Causas comunes: GC durante la medición, throttling térmico, o el benchmark hace IO. Investiga antes de concluir algo.

### 5. Concluir de microbenchmarks sin validar en el contexto real

Un benchmark de `Map.get` aislado puede mostrar que Map es 2x más rápido, pero en tu aplicación real el costo dominante puede ser la serialización de mensajes o el IO de base de datos. Siempre perfilar el sistema completo antes de optimizar.

### 6. No usar `inputs:` para encontrar el punto de inflexión

La pregunta más importante no es "¿qué es más rápido?" sino "¿a qué tamaño cambia el comportamiento?". Benchee con `inputs:` de diferentes órdenes de magnitud revela esta información.

---

## Summary

Benchee convierte el benchmarking de arte en ciencia: warm-up controlado, múltiples iteraciones, estadísticas descriptivas completas, y soporte para comparar comportamiento de scaling con `inputs:`. Las métricas más importantes son:

- **IPS y median**: rendimiento típico
- **std_dev**: confiabilidad del resultado
- **99th %**: comportamiento en el peor caso real
- **memory_time**: costo en allocations, no solo en tiempo

El benchmarking más valioso es el que responde "¿a qué escala cambia el ganador?" — usa siempre `inputs:` con órdenes de magnitud diferentes.

---

## What's Next

- **Ejercicio 29**: Binary matching performance — cuándo el matching supera al Regex
- Investiga `benchee_html` para reportes visuales con gráficas de percentiles
- Explora `:observer.start()` mientras corre un benchmark para ver el comportamiento del GC en tiempo real
- Lee sobre flame graphs en Elixir con `eflambe` — profiling del sistema completo, no solo funciones aisladas

---

## Resources

- [Benchee documentation](https://github.com/bencheeorg/benchee)
- [Benchee HTML formatter](https://github.com/bencheeorg/benchee_html)
- [Saša Jurić — "Writing Quality Benchmarks"](https://www.theerlangelist.com/)
- [:atomics module — Erlang docs](https://www.erlang.org/doc/man/atomics.html)
- [Erlang efficiency guide — Maps vs Proplists](https://www.erlang.org/doc/efficiency_guide/maps.html)
