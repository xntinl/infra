# 20 — ETS Counters y Atomics

## Prerequisites

- ETS avanzado con `write_concurrency` y `decentralized_counters` (ejercicio 16)
- Conceptos de concurrencia: race conditions, mutex, lock-free
- BEAM schedulers y procesos (ejercicio 25 como lectura complementaria)
- OTP 23+ para `:counters` (verificar con `:erlang.system_info(:otp_release)`)

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Usar `:ets.update_counter/4` para contadores atómicos en ETS
2. Crear y operar arrays de enteros con `:atomics` para contadores lock-free
3. Usar `:counters` (OTP 23+) para contadores eficientes con semántica configurable
4. Comparar el performance de ETS, `:atomics`, `:counters` y GenServer en escenarios reales
5. Implementar un sliding window counter con `:atomics`
6. Diseñar un sistema de métricas distribuidas con gossip de contadores

---

## Concepts

### 1. `:ets.update_counter/4` — contadores atómicos en ETS

`update_counter` es la operación más eficiente de ETS para contadores: modifica un entero en un record atómicamente sin race conditions:

```elixir
table = :ets.new(:counters, [:set, :public, {:write_concurrency, true}])

# Syntax: update_counter(table, key, {position, increment}, default)
# position: índice del campo numérico en el tuple (1-based, 1 = key)
# default: valor inicial si la clave no existe

# Incrementar en 1, valor inicial 0 si no existe
:ets.update_counter(table, :requests, {2, 1}, {:requests, 0})
# => 1 (nuevo valor)

:ets.update_counter(table, :requests, {2, 1}, {:requests, 0})
# => 2

# Decrementar con límite inferior (no puede bajar de 0)
:ets.update_counter(table, :available_slots, {2, -1, 0, 0})
# {position, increment, threshold, set_value_if_below_threshold}
# Si el nuevo valor < threshold, lo setea a set_value_if_below_threshold

# Múltiples updates atómicos en un solo call
:ets.update_counter(table, :stats, [
  {2, 1},    # incrementar requests
  {3, bytes} # incrementar bytes_transferred
], {:stats, 0, 0})
```

**Cuándo usar ETS update_counter vs otros mecanismos:**
- ETS: cuando el contador es parte de un record más amplio o necesitas agrupar múltiples contadores por clave
- `:atomics`: cuando necesitas arrays de contadores y el mínimo overhead de memoria
- `:counters`: cuando necesitas semántica de "no rollover" y el API más ergonómico (OTP 23+)

### 2. `:atomics` — arrays de enteros atómicos

`:atomics` es un módulo de bajo nivel (OTP 21.2+) que provee arrays de enteros con operaciones atómicas garantizadas. Es más rápido que ETS para contadores puros porque evita el overhead del hash table:

```elixir
# Crear array de 8 contadores de 64-bit, con semántica signed
ref = :atomics.new(8, signed: true)
# Para contadores que nunca son negativos:
ref = :atomics.new(8, signed: false)

# Operaciones básicas (1-indexed)
:atomics.add(ref, 1, 1)        # ref[1] += 1
:atomics.add(ref, 1, 100)      # ref[1] += 100
:atomics.get(ref, 1)           # => 101
:atomics.put(ref, 1, 0)        # ref[1] = 0 (reset)

# add_get: atómico, retorna el nuevo valor
new_val = :atomics.add_get(ref, 1, 1)

# Compare and swap: para operaciones condicionales atómicas
# atomics.compare_exchange(ref, index, expected, desired)
# Si ref[index] == expected, setea a desired y retorna :ok
# Si no, retorna el valor actual
case :atomics.compare_exchange(ref, 1, 0, 1) do
  :ok -> IO.puts("CAS success: 0 -> 1")
  actual -> IO.puts("CAS failed, current: #{actual}")
end

# Info del array
:atomics.info(ref)
# => %{max: 9223372036854775807, memory: 96, min: -9223372036854775808, size: 8}
```

**Semántica de desbordamiento**: con `signed: false`, al llegar al máximo (2^64-1) el siguiente incremento vuelve a 0 (wraps around). Con `signed: true`, el rango es [-2^63, 2^63-1].

**Uso de memoria**: `atomics.new(N)` ocupa ~64 bytes de overhead + 8 bytes por elemento. Extremadamente eficiente para contadores puros.

### 3. `:counters` (OTP 23+) — contadores por scheduler

`:counters` está diseñado específicamente para el caso de uso de contadores de alto throughput en aplicaciones Erlang. Internamente mantiene un contador por scheduler (similar a `decentralized_counters` en ETS) para minimizar contención:

```elixir
# Crear array de contadores
# :atomics_like: sin descentralización (como :atomics pero con API de counters)
# :write_concurrency: copia por scheduler (más rápido en escritura, lectura más lenta)
cref = :counters.new(5, [:write_concurrency])

# API
:counters.add(cref, 1, 1)         # cref[1] += 1
:counters.sub(cref, 1, 1)         # cref[1] -= 1
:counters.get(cref, 1)            # => valor actual
:counters.put(cref, 1, 0)         # reset

# Info
:counters.info(cref)
# => %{memory: 512, size: 5}
# Nota: memory es mayor con :write_concurrency porque hay una copia por scheduler
```

**`:atomics` vs `:counters`:**

| Característica | `:atomics` | `:counters` |
|----------------|-----------|------------|
| Disponible desde | OTP 21.2 | OTP 23 |
| Write throughput | Alto | Máximo (por scheduler) |
| Read de valor exacto | O(1) | O(schedulers) con :write_concurrency |
| CAS (compare-and-swap) | Sí | No |
| Semántica signed/unsigned | Configurable | Siempre signed |
| Uso de memoria | Mínimo | N × num_schedulers |

### 4. Lock-free vs mutex — diferencias concretas

```
Mutex (GenServer):
  Proceso A: call GenServer → enqueue → wait → process → reply
  Throughput: 1 / latencia_genserver

Lock-free (ETS/atomics):
  Proceso A: hardware CAS instruction → done en nanosegundos
  Proceso B: mismo, en paralelo, sin bloquear A
  Throughput: N × 1/latencia_cas × num_schedulers
```

Lock-free no significa "sin contención". Significa que los threads/procesos no se bloquean mutuamente — en el peor caso, retroceden y reintentanl (retry), pero nunca duermen esperando un lock.

En la BEAM, los procesos no comparten memoria, así que "lock-free" a nivel de proceso significa: no usar un proceso como intermediario. Las operaciones atómicas de ETS, `:atomics` y `:counters` son lock-free respecto al scheduler de la BEAM.

### 5. Contadores de sliding window

Un sliding window counter permite preguntas como "¿cuántas requests en los últimos 60 segundos?" sin mantener el historial completo. La aproximación más eficiente usa buckets de tiempo:

```
window = 60 segundos, granularidad = 1 segundo → 60 buckets
current_bucket = div(unix_seconds, 1)

Para cada request: increment bucket[current_bucket % 60]
Para leer el total: sum(todos los buckets)
Para limpiar: bucket[old_bucket] = 0 cuando se descarta
```

Con `:atomics`, cada bucket es un elemento del array:

```elixir
# Array de 60 enteros para 60 buckets de 1 segundo
ref = :atomics.new(60, signed: false)

def record(ref) do
  bucket = rem(div(System.os_time(:second), 1), 60)
  :atomics.add(ref, bucket + 1, 1)  # 1-indexed
end

def count_last_n_seconds(ref, n) when n <= 60 do
  now_bucket = rem(div(System.os_time(:second), 1), 60)
  Enum.sum(for i <- 0..(n-1) do
    bucket = rem(now_bucket - i + 60, 60)
    :atomics.get(ref, bucket + 1)
  end)
end
```

---

## Exercises

### Exercise 1 — High-throughput request counter

**Problem**

Implementa un sistema de métricas para un servidor HTTP ficticio. Necesita rastrear por ruta:
- Total de requests
- Total de bytes transferidos
- Errores (status >= 400)

Los contadores deben soportar al menos 100.000 ops/segundo bajo carga concurrente. Benchmarkea tres implementaciones:
1. `MetricsGenServer` — GenServer clásico con mapa
2. `MetricsETS` — ETS con `update_counter` y `decentralized_counters`
3. `MetricsAtomics` — `:atomics` con índice fijo por ruta

Compara throughput y latencia p99 bajo 50 workers concurrentes.

**Hints**

- Para MetricsAtomics, pre-asigna índices a rutas conocidas en un mapa en tiempo de compilación
- `decentralized_counters: true` es la clave para ETS a alta concurrencia
- Para p99 de latencia, usa una lista de tiempos y `Enum.sort |> Enum.at(round(n * 0.99))`
- Pre-calienta cada implementación antes de medir (JIT del BEAM)

**One possible solution**

```elixir
defmodule MetricsGenServer do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  def init(state), do: {:ok, state}

  def record(route, bytes, status) do
    GenServer.cast(__MODULE__, {:record, route, bytes, status})
  end

  def get(route) do
    GenServer.call(__MODULE__, {:get, route})
  end

  def handle_cast({:record, route, bytes, status}, state) do
    entry = Map.get(state, route, %{requests: 0, bytes: 0, errors: 0})
    updated = %{
      requests: entry.requests + 1,
      bytes: entry.bytes + bytes,
      errors: if(status >= 400, do: entry.errors + 1, else: entry.errors)
    }
    {:noreply, Map.put(state, route, updated)}
  end

  def handle_call({:get, route}, _from, state) do
    {:reply, Map.get(state, route, %{requests: 0, bytes: 0, errors: 0}), state}
  end
end

defmodule MetricsETS do
  @table :metrics_ets

  def start do
    :ets.new(@table, [
      :set, :public, :named_table,
      {:write_concurrency, true},
      {:decentralized_counters, true}
    ])
  end

  # Record: tuple {route, requests, bytes, errors}
  # Posición:        1       2        3      4
  def record(route, bytes, status) do
    error_inc = if status >= 400, do: 1, else: 0
    :ets.update_counter(@table, route, [{2, 1}, {3, bytes}, {4, error_inc}], {route, 0, 0, 0})
  end

  def get(route) do
    case :ets.lookup(@table, route) do
      [{^route, req, bytes, errors}] -> %{requests: req, bytes: bytes, errors: errors}
      [] -> %{requests: 0, bytes: 0, errors: 0}
    end
  end
end

defmodule MetricsAtomics do
  # Índices fijos: requests=1, bytes=2, errors=3 (por ruta)
  # Cada ruta ocupa 3 slots en el array
  @routes ["/api/users", "/api/items", "/health", "/api/orders"]
  @route_index Map.new(Enum.with_index(@routes, 0), fn {route, i} -> {route, i * 3 + 1} end)
  @array_size length(@routes) * 3

  def start do
    ref = :atomics.new(@array_size, signed: false)
    :persistent_term.put(:metrics_atomics, ref)
    ref
  end

  def record(route, bytes, status) do
    case Map.get(@route_index, route) do
      nil -> :unknown_route
      base_idx ->
        ref = :persistent_term.get(:metrics_atomics)
        :atomics.add(ref, base_idx, 1)         # requests
        :atomics.add(ref, base_idx + 1, bytes) # bytes
        if status >= 400, do: :atomics.add(ref, base_idx + 2, 1)  # errors
    end
  end

  def get(route) do
    case Map.get(@route_index, route) do
      nil -> %{requests: 0, bytes: 0, errors: 0}
      base_idx ->
        ref = :persistent_term.get(:metrics_atomics)
        %{
          requests: :atomics.get(ref, base_idx),
          bytes: :atomics.get(ref, base_idx + 1),
          errors: :atomics.get(ref, base_idx + 2)
        }
    end
  end
end

defmodule MetricsBenchmark do
  @routes ["/api/users", "/api/items", "/health", "/api/orders"]
  @workers 50
  @ops_per_worker 2_000

  def run do
    IO.puts("Benchmarking metrics implementations...")
    IO.puts("#{@workers} workers × #{@ops_per_worker} ops = #{@workers * @ops_per_worker} total ops\n")

    {:ok, _} = MetricsGenServer.start_link([])
    bench("GenServer", &bench_genserver/0)

    MetricsETS.start()
    bench("ETS", &bench_ets/0)

    MetricsAtomics.start()
    bench("Atomics", &bench_atomics/0)
  end

  defp bench(name, fun) do
    # Warmup
    for _ <- 1..1_000, do: fun.()

    t0 = System.monotonic_time(:microsecond)

    latencies =
      1..@workers
      |> Task.async_stream(fn _ ->
        for _ <- 1..@ops_per_worker do
          t_start = System.monotonic_time(:microsecond)
          fun.()
          System.monotonic_time(:microsecond) - t_start
        end
      end, max_concurrency: @workers, timeout: 60_000)
      |> Enum.flat_map(fn {:ok, times} -> times end)

    t1 = System.monotonic_time(:microsecond)
    elapsed_us = t1 - t0
    total_ops = @workers * @ops_per_worker

    sorted = Enum.sort(latencies)
    p50 = Enum.at(sorted, round(length(sorted) * 0.50))
    p99 = Enum.at(sorted, round(length(sorted) * 0.99))

    IO.puts("#{name}:")
    IO.puts("  Throughput: #{round(total_ops / elapsed_us * 1_000_000)} ops/sec")
    IO.puts("  Latency p50: #{p50}μs")
    IO.puts("  Latency p99: #{p99}μs")
    IO.puts("")
  end

  defp bench_genserver do
    route = Enum.random(@routes)
    MetricsGenServer.record(route, :rand.uniform(1024), Enum.random([200, 200, 200, 404, 500]))
  end

  defp bench_ets do
    route = Enum.random(@routes)
    MetricsETS.record(route, :rand.uniform(1024), Enum.random([200, 200, 200, 404, 500]))
  end

  defp bench_atomics do
    route = Enum.random(@routes)
    MetricsAtomics.record(route, :rand.uniform(1024), Enum.random([200, 200, 200, 404, 500]))
  end
end

MetricsBenchmark.run()
```

---

### Exercise 2 — Sliding window counter con `:atomics`

**Problem**

Implementa un `SlidingWindowCounter` que responde: "¿cuántos eventos ocurrieron en los últimos N segundos?" usando `:atomics` como almacén de buckets.

Requisitos:
1. Resolución de 1 segundo, ventana máxima de 60 segundos
2. `record/1` — registra un evento ahora
3. `count/1` — cuenta eventos en los últimos N segundos (N <= 60)
4. `rate/1` — eventos por segundo promedio en la ventana
5. Thread-safe: 1000 goroutines (Tasks) registrando simultáneamente

El reto adicional: los buckets deben limpiarse automáticamente cuando "caducan" (cuando el segundo al que pertenecen sale de la ventana).

**Hints**

- El índice del bucket actual es `rem(unix_seconds, 60)`
- Para detectar que un bucket es "viejo" necesitas almacenar el timestamp del bucket junto con el count
- Usar dos arrays atomics: uno para counts, otro para timestamps de cada bucket
- El cleanup del bucket viejo ocurre en el siguiente `record` para ese bucket slot

**One possible solution**

```elixir
defmodule SlidingWindowCounter do
  @buckets 60
  # Índices en arrays atomics: 1..60 = counts, 61..120 = bucket timestamps

  def new do
    # counts: índices 1..60
    # timestamps: índices 1..60 (mismos índices en el segundo array)
    counts = :atomics.new(@buckets, signed: false)
    timestamps = :atomics.new(@buckets, signed: true)
    {counts, timestamps}
  end

  def record({counts, timestamps}) do
    now = System.os_time(:second)
    idx = bucket_index(now)

    # Verificar si el bucket es del segundo actual o es viejo
    stored_ts = :atomics.get(timestamps, idx)

    if stored_ts != now do
      # Bucket viejo — limpiar y reinicializar
      # CAS para evitar que múltiples procesos limpien simultáneamente
      case :atomics.compare_exchange(timestamps, idx, stored_ts, now) do
        :ok ->
          # Ganamos la limpieza — resetear el counter
          :atomics.put(counts, idx, 1)
        _current ->
          # Otro proceso limpió primero — simplemente incrementar
          :atomics.add(counts, idx, 1)
      end
    else
      :atomics.add(counts, idx, 1)
    end
  end

  def count({counts, timestamps}, seconds) when seconds <= @buckets do
    now = System.os_time(:second)
    cutoff = now - seconds + 1  # incluye el segundo actual

    Enum.sum(
      for s <- cutoff..now do
        idx = bucket_index(s)
        stored_ts = :atomics.get(timestamps, idx)
        # Solo contar si el bucket pertenece a este segundo específico
        if stored_ts == s, do: :atomics.get(counts, idx), else: 0
      end
    )
  end

  def rate({counts, timestamps} = ref, seconds) do
    total = count(ref, seconds)
    total / max(seconds, 1)
  end

  defp bucket_index(unix_second), do: rem(unix_second, @buckets) + 1  # 1-indexed
end

# Demo
counter = SlidingWindowCounter.new()

# Simular carga: 100 workers registrando eventos
1..100
|> Task.async_stream(fn _ ->
  for _ <- 1..50, do: SlidingWindowCounter.record(counter)
end, max_concurrency: 100, timeout: 10_000)
|> Enum.to_list()

IO.puts("Events in last 60s: #{SlidingWindowCounter.count(counter, 60)}")
IO.puts("Events in last 10s: #{SlidingWindowCounter.count(counter, 10)}")
IO.puts("Rate (60s avg): #{Float.round(SlidingWindowCounter.rate(counter, 60), 2)} events/sec")

# Verificar que el total se aproxima a 100 × 50 = 5000
expected = 5_000
actual = SlidingWindowCounter.count(counter, 60)
IO.puts("Expected ~#{expected}, got #{actual}")
IO.puts("Accuracy: #{Float.round(actual / expected * 100, 1)}%")
```

**Trade-off analysis**: Este sliding window usa buckets de 1 segundo, lo que introduce un error de hasta ±1 segundo en el conteo (si estás en el segundo 59.9 y pides "últimos 10 segundos", el resultado incluye el segundo 50-60 completo). Para mayor precisión, usa buckets más finos (100ms = 600 buckets para una ventana de 60s), pero con mayor uso de memoria.

---

### Exercise 3 — Distributed counter con gossip

**Problem**

En un sistema multi-nodo, los contadores centralizados crean un cuello de botella de red. Implementa un contador distribuido con arquitectura gossip:

1. Cada nodo mantiene sus propios contadores locales en `:atomics`
2. Periódicamente, cada nodo envía sus deltas a un subconjunto aleatorio de nodos (gossip)
3. `global_count/1` retorna la suma de los valores de todos los nodos conectados

Este ejercicio es una simulación con múltiples procesos (uno por "nodo ficticio"), no requiere un cluster Erlang real.

**Hints**

- Cada "nodo" es un proceso GenServer con un ref de `:atomics`
- El gossip interval puede ser 500ms para el demo
- `global_count` hace un `GenServer.call` a todos los "nodos" y suma
- Para simular un cluster real, usa `Node.list()` y RPC en producción

**One possible solution**

```elixir
defmodule GossipNode do
  use GenServer

  @gossip_interval 500
  @gossip_fanout 2  # cuántos nodos reciben el gossip

  def start_link(name, peers) do
    GenServer.start_link(__MODULE__, {name, peers}, name: name)
  end

  def increment(node_name, key, amount \\ 1) do
    GenServer.cast(node_name, {:increment, key, amount})
  end

  def local_count(node_name, key) do
    GenServer.call(node_name, {:local_count, key})
  end

  def receive_gossip(node_name, deltas) do
    GenServer.cast(node_name, {:gossip, deltas})
  end

  def init({name, peers}) do
    # array de contadores: índices fijos por clave
    local = :atomics.new(10, signed: false)
    received = :atomics.new(10, signed: false)  # deltas recibidos de otros nodos

    schedule_gossip()

    {:ok, %{name: name, peers: peers, local: local, received: received}}
  end

  def handle_cast({:increment, key, amount}, state) do
    idx = key_to_index(key)
    :atomics.add(state.local, idx, amount)
    {:noreply, state}
  end

  def handle_cast({:gossip, deltas}, state) do
    Enum.each(deltas, fn {key, value} ->
      idx = key_to_index(key)
      # Merge: tomamos el máximo (G-counter semantics — monotónico)
      current = :atomics.get(state.received, idx)
      if value > current do
        :atomics.put(state.received, idx, value)
      end
    end)
    {:noreply, state}
  end

  def handle_call({:local_count, key}, _from, state) do
    idx = key_to_index(key)
    local = :atomics.get(state.local, idx)
    received = :atomics.get(state.received, idx)
    {:reply, local + received, state}
  end

  def handle_info(:gossip, state) do
    # Calcular deltas locales para enviar
    deltas = for key <- known_keys() do
      idx = key_to_index(key)
      {key, :atomics.get(state.local, idx)}
    end

    # Seleccionar fanout nodos aleatorios
    targets = state.peers |> Enum.shuffle() |> Enum.take(@gossip_fanout)

    Enum.each(targets, fn peer ->
      if Process.whereis(peer) != nil do
        GossipNode.receive_gossip(peer, deltas)
      end
    end)

    schedule_gossip()
    {:noreply, state}
  end

  defp schedule_gossip, do: Process.send_after(self(), :gossip, @gossip_interval)

  defp known_keys, do: [:requests, :errors, :bytes]

  defp key_to_index(key) do
    %{requests: 1, errors: 2, bytes: 3}
    |> Map.fetch!(key)
  end
end

defmodule GossipCluster do
  def start(node_count) do
    nodes = for i <- 1..node_count, do: :"gossip_node_#{i}"

    # Cada nodo conoce a todos los demás (en prod, esto sería parcial)
    for node_name <- nodes do
      peers = List.delete(nodes, node_name)
      GossipNode.start_link(node_name, peers)
    end

    nodes
  end

  def global_count(nodes, key) do
    # En un cluster real: Enum.map(Node.list(), fn n -> :rpc.call(n, ...) end)
    # Aquí: sumar el local count de cada nodo (ellos ya suman sus received)
    # Para G-counters correctos: sumar solo los locales de cada nodo
    Enum.sum(Enum.map(nodes, fn node ->
      # Obtener directamente el local del atomics — sin el received (que es de otros nodos)
      GenServer.call(node, {:local_count, key})
    end))
  end
end

# Demo
nodes = GossipCluster.start(4)

# Registrar eventos en distintos nodos
GossipNode.increment(:"gossip_node_1", :requests, 100)
GossipNode.increment(:"gossip_node_2", :requests, 200)
GossipNode.increment(:"gossip_node_3", :requests, 150)
GossipNode.increment(:"gossip_node_4", :requests, 50)

expected_total = 500

IO.puts("Initial (before gossip): #{GossipCluster.global_count(nodes, :requests)}")
# May not be 500 yet if we sum local_count (which includes received)

IO.puts("Waiting for gossip convergence...")
Process.sleep(2_000)  # 4 gossip rounds

IO.puts("After gossip convergence: #{GossipCluster.global_count(nodes, :requests)}")
IO.puts("Expected: #{expected_total}")
```

**Trade-off analysis**: El gossip counter usa semántica de G-counter (grow-only). No es idóneo para contadores que decrementan. La convergencia no es instantánea (depende del intervalo y fanout) y puede haber una ventana de inconsistencia. En producción, `:global.trans` o Phoenix.PubSub son alternativas más simples para contadores distribuidos que no necesitan survive particiones de red.

---

## Common Mistakes

**Usar `update_counter` sin default en tablas `set`**
Si la clave no existe y no provees un default, `update_counter` lanza `ArgumentError`. El cuarto argumento (default) es esencial para la primera escritura:

```elixir
# Incorrecto — falla si :key no existe
:ets.update_counter(:table, :key, {2, 1})

# Correcto — inserta {:key, 0} si no existe, luego incrementa
:ets.update_counter(:table, :key, {2, 1}, {:key, 0})
```

**Indexación 1-based en `:atomics` y `:counters`**
Ambos módulos usan índices 1-based (como las listas de Erlang). El índice 0 no existe y causa `ArgumentError`.

```elixir
ref = :atomics.new(3, [])
:atomics.get(ref, 0)  # ArgumentError!
:atomics.get(ref, 1)  # OK — primer elemento
```

**Usar `:write_concurrency` en `:counters` cuando necesitas lecturas frecuentes exactas**
Con `:write_concurrency`, `:counters.get/2` suma los parciales de todos los schedulers. Esto es O(num_schedulers) y puede ser lento si lees frecuentemente. Si necesitas leer con igual frecuencia que escribes, usa `:atomics_like`:

```elixir
# Alta escritura, lectura ocasional
cref = :counters.new(N, [:write_concurrency])

# Escritura y lectura equilibradas
cref = :counters.new(N, [:atomics_like])
```

**Asumir que `compare_exchange` en un loop es siempre eficiente**
CAS en loop (spin-wait) puede consumir CPU agresivamente si hay contención alta. Si muchos procesos compiten por el mismo índice, el spin-wait puede ser peor que un lock tradicional. Mide antes de optimizar.

**Olvidar que `:atomics` y `:counters` no persisten entre reinicios**
Son estructuras en memoria pura. Si el proceso que los creó muere, el ref deja de ser válido. Para persistencia, combina con DETS o Mnesia al shutdown.

---

## Verification

```elixir
# Verificar atomicidad de update_counter
table = :ets.new(:test, [:set, :public, {:write_concurrency, true}, {:decentralized_counters, true}])

1..1_000
|> Task.async_stream(fn _ ->
  :ets.update_counter(table, :count, {2, 1}, {:count, 0})
end, max_concurrency: 100, timeout: 10_000)
|> Enum.to_list()

[{:count, final}] = :ets.lookup(table, :count)
IO.puts("Expected: 1000, Got: #{final}")
# Si hay race conditions, el valor sería menor
^final = 1_000
IO.puts("Atomicity verified: OK")
```

```elixir
# Verificar sliding window
counter = SlidingWindowCounter.new()
for _ <- 1..100, do: SlidingWindowCounter.record(counter)

count = SlidingWindowCounter.count(counter, 1)
IO.puts("Recorded 100 events, count in last 1s: #{count}")
# Debe ser ~100 (puede haber leve variación por el segundo actual)
```

```elixir
# Verificar que :atomics es más rápido que ETS para contador simple
n = 1_000_000

t0 = System.monotonic_time(:microsecond)
ref = :atomics.new(1, signed: false)
for _ <- 1..n, do: :atomics.add(ref, 1, 1)
t1 = System.monotonic_time(:microsecond)

table = :ets.new(:t, [:set, :public])
for _ <- 1..n, do: :ets.update_counter(table, :k, {2, 1}, {:k, 0})
t2 = System.monotonic_time(:microsecond)

IO.puts("Atomics: #{t1 - t0}μs")
IO.puts("ETS:     #{t2 - t1}μs")
# Atomics debería ser ~2-5x más rápido para contadores simples en proceso único
```

---

## Summary

Los contadores lock-free son una de las optimizaciones más impactantes en sistemas de alta concurrencia. La jerarquía de velocidad para contadores simples:

1. `:atomics.add` — más rápido, sin overhead de tabla hash
2. `:counters.add` con `:write_concurrency` — máximo throughput bajo carga concurrente
3. `:ets.update_counter` con `decentralized_counters: true` — cuando el contador es parte de un registro mayor
4. GenServer — cuando necesitas lógica de negocio alrededor del contador

La regla de oro: mide antes de optimizar. GenServer es suficiente para la mayoría de los sistemas hasta decenas de miles de ops/segundo. Cuando el profiling muestra que el GenServer es el cuello de botella, la migración a ETS o `:atomics` es straightforward y el gain es típicamente 10-100x.

---

## What's Next

- **Ejercicio 25**: BEAM schedulers y reductions — entender qué hace el scheduler con estas operaciones
- **Ejercicio 28**: Benchmarking con Benchee — medir correctamente las diferencias
- **Ejercicio 37**: Rate limiting patterns — aplicar contadores a rate limiting de producción

---

## Resources

- [`:atomics` — Erlang docs](https://www.erlang.org/doc/man/atomics.html)
- [`:counters` — Erlang docs](https://www.erlang.org/doc/man/counters.html)
- [ETS `update_counter` — Erlang docs](https://www.erlang.org/doc/man/ets.html#update_counter-4)
- [Lock-free data structures in Erlang — Lukas Larsson, EUC 2019](https://www.youtube.com/watch?v=3VGYAevO9E4) — excelente charla sobre atomics
- ["The Erlang Runtime System" — Erik Stenman](https://www.oreilly.com/library/view/the-erlang-runtime/9781800560818/) — capítulo sobre memoria compartida
