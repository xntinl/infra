# 07 — PartitionSupervisor

**Difficulty**: Avanzado  
**Estimated time**: 90–120 min  
**Topics**: PartitionSupervisor, State Sharding, Concurrency, Benchmarking

---

## Context

Un solo GenServer serializa todas las operaciones que pasan por él. En sistemas de alto throughput, este serialization point se convierte en el cuello de botella de toda la aplicación.

`PartitionSupervisor` (introducido en Elixir 1.14) resuelve este problema elegantemente: supervisa N copias idénticas de un proceso y enruta cada request al proceso correcto mediante hash de una clave. El resultado es paralelismo real sin coordinación entre particiones.

El trade-off fundamental: sharding por hash garantiza que requests con la misma clave van siempre al mismo proceso (coherencia local), pero rompe la consistencia global. Entender cuándo esto es aceptable es la habilidad clave.

---

## Concepts

### PartitionSupervisor Basics

```elixir
# En Application.start/2
children = [
  {PartitionSupervisor,
    child_spec: MyApp.RateLimiter,
    name: MyApp.RateLimiter.Partitions,
    partitions: System.schedulers_online()  # una partición por scheduler
  }
]
```

`PartitionSupervisor` crea `partitions` copias de `child_spec`, cada una identificada por un índice 0..N-1. El nombre registrado de cada partición sigue la convención `{name, partition_index}`.

**Cuántos partitions usar**: la guía general es `System.schedulers_online()` (número de cores). Más particiones no siempre es mejor: hay overhead de scheduling y el hash puede no distribuir uniformemente keys pequeñas.

---

### Routing con `{:via, PartitionSupervisor, {name, key}}`

```elixir
# Enviar al proceso correcto para el usuario "user_123"
GenServer.call(
  {:via, PartitionSupervisor, {MyApp.RateLimiter.Partitions, "user_123"}},
  {:check_rate, "user_123"}
)
```

`PartitionSupervisor` calcula `hash(key) rem partitions` para determinar la partición. La misma key siempre va al mismo proceso — esto es la garantía de coherencia local que hace útil el sharding.

**Importante**: el `key` puede ser cualquier término Elixir. El hash se calcula con `:erlang.phash2/2`.

```elixir
# Internamente, PartitionSupervisor hace algo así:
defp partition_for(key, partitions) do
  :erlang.phash2(key, partitions)
end
```

---

### Cuándo usar PartitionSupervisor

| Situación | ¿Usar PartitionSupervisor? |
|-----------|---------------------------|
| Rate limiting por usuario | Sí — cada usuario tiene su estado |
| Contador global único | No — necesitas consistencia global |
| Cache con sharding por key | Sí — localidad de referencia |
| Saga coordinator por order_id | Sí — estado por entidad |
| Lock global compartido | No — imposible shardar |
| Queue de trabajo por tenant | Sí — aislamiento por tenant |

**Anti-patrón**: usar PartitionSupervisor cuando las keys tienen baja cardinalidad (e.g., solo 3 valores posibles) — la distribución será terrible.

---

### PartitionSupervisor con Registry

Para acceder a particiones individuales por nombre:

```elixir
children = [
  {PartitionSupervisor,
    child_spec: DynamicSupervisor,
    name: MyApp.DynamicSupervisors
  }
]

# Iniciar un proceso hijo bajo la partición correcta
DynamicSupervisor.start_child(
  {:via, PartitionSupervisor, {MyApp.DynamicSupervisors, key}},
  child_spec
)
```

Combinado con `Registry`, este patrón permite procesos dinámicos sharded — por ejemplo, un proceso por `{user_id, session_id}` distribuido entre particiones.

---

### Limitaciones y Trade-offs

**Sin consistencia entre particiones**: si necesitas un rate limit global de 1000 req/min entre todos los usuarios, no puedes sumarlo trivialmente desde N particiones sin coordinación.

**Rebalanceo**: si cambias el número de particiones, el hash de todas las keys cambia. No hay migración automática de estado. Para estado persistente, esto es un problema serio.

**Distribución desigual con pocas keys**: con 8 particiones y 3 keys distintas, algunas particiones recibirán 0 requests. El beneficio de sharding desaparece.

**Debug más complejo**: en lugar de un proceso para inspeccionar, tienes N. `Supervisor.which_children(MyApp.Partitions)` te devuelve todos, pero correlacionar un request con su partición requiere conocer el algoritmo de hash.

---

## Exercise 1 — Rate Limiter Particionado

### Problem

Implementa un rate limiter basado en token bucket donde cada usuario tiene su propio bucket. El sistema debe soportar miles de usuarios concurrentes sin que un GenServer global se convierta en bottleneck.

**Especificaciones**:
- Cada usuario tiene un bucket de `max_tokens` tokens
- Los tokens se regeneran a razón de `refill_rate` tokens/segundo
- Una request consume 1 token; si el bucket está vacío, la request es rechazada (`:rate_limited`)
- El rate limiter debe ser consultable vía `RateLimiter.check(user_id)` → `:ok | :rate_limited`

```elixir
defmodule MyApp.RateLimiter do
  use GenServer

  @max_tokens 10
  @refill_rate 2  # tokens por segundo

  # API pública
  def check(user_id) do
    GenServer.call(
      {:via, PartitionSupervisor, {MyApp.RateLimiter.Partitions, user_id}},
      {:check, user_id}
    )
  end

  # Implementa init/1 y handle_call/3 para {:check, user_id}
  # State: mapa de %{user_id => %{tokens: N, last_refill: monotonic_time}}
  def init(_opts) do
    # Tu implementación aquí
  end

  def handle_call({:check, user_id}, _from, state) do
    # Tu implementación aquí
    # 1. Calcular tokens regenerados desde last_refill
    # 2. Si tokens > 0: decrementar y responder :ok
    # 3. Si tokens == 0: responder :rate_limited
    # 4. Actualizar state con nuevo token count y timestamp
  end
end

defmodule MyApp.Application do
  use Application

  def start(_type, _args) do
    children = [
      {PartitionSupervisor,
        child_spec: MyApp.RateLimiter,
        name: MyApp.RateLimiter.Partitions,
        partitions: ???
      }
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: MyApp.Supervisor)
  end
end
```

**Tu tarea**:
1. Implementa `RateLimiter` completo con token bucket
2. Elige el número de particiones y justifica tu elección
3. Verifica que dos usuarios con el mismo hash van siempre al mismo proceso
4. Verifica que el rate limit se aplica por usuario, no globalmente

### Hints

<details>
<summary>Hint 1 — Estado por partición vs por usuario</summary>

Cada partición del `RateLimiter` maneja múltiples usuarios. El state del GenServer es un mapa `%{user_id => bucket_state}`. Cuando llega `{:check, user_id}`, buscas en el mapa el bucket de ese usuario específico. Si no existe, creas uno nuevo con tokens llenos.

</details>

<details>
<summary>Hint 2 — Calcular tokens regenerados</summary>

Usa `System.monotonic_time(:millisecond)` para timestamps. Los tokens regenerados son:
```elixir
elapsed_seconds = (now - last_refill) / 1000
new_tokens = min(max_tokens, current_tokens + floor(elapsed_seconds * refill_rate))
```
Si `elapsed_seconds < 1/refill_rate`, no se regenera ningún token todavía.

</details>

<details>
<summary>Hint 3 — Limpiar state de usuarios inactivos</summary>

Con miles de usuarios, el mapa de state crece indefinidamente. Considera un mecanismo de limpieza: por ejemplo, `Process.send_after(self(), :cleanup, 60_000)` que elimina entradas donde `last_refill` fue hace más de 5 minutos. Es una práctica de producción importante.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
defmodule MyApp.RateLimiter do
  use GenServer
  require Logger

  @max_tokens 10
  @refill_rate 2         # tokens/segundo
  @cleanup_interval 60_000  # limpiar inactivos cada 60s
  @inactivity_ttl 300_000   # eliminar si inactivo 5min

  def check(user_id) do
    GenServer.call(
      {:via, PartitionSupervisor, {MyApp.RateLimiter.Partitions, user_id}},
      {:check, user_id}
    )
  end

  def init(_opts) do
    Process.send_after(self(), :cleanup, @cleanup_interval)
    {:ok, %{}}
  end

  def handle_call({:check, user_id}, _from, state) do
    now = System.monotonic_time(:millisecond)
    bucket = Map.get(state, user_id, %{tokens: @max_tokens, last_refill: now})

    elapsed_ms = now - bucket.last_refill
    elapsed_s = elapsed_ms / 1000
    regenerated = floor(elapsed_s * @refill_rate)
    current_tokens = min(@max_tokens, bucket.tokens + regenerated)

    {result, new_tokens} =
      if current_tokens > 0 do
        {:ok, current_tokens - 1}
      else
        {:rate_limited, 0}
      end

    new_state =
      Map.put(state, user_id, %{
        tokens: new_tokens,
        last_refill: if(regenerated > 0, do: now, else: bucket.last_refill)
      })

    {:reply, result, new_state}
  end

  def handle_info(:cleanup, state) do
    now = System.monotonic_time(:millisecond)

    new_state =
      Map.reject(state, fn {_user_id, bucket} ->
        now - bucket.last_refill > @inactivity_ttl
      end)

    Logger.debug("RateLimiter cleanup: #{map_size(state)} → #{map_size(new_state)} entries")
    Process.send_after(self(), :cleanup, @cleanup_interval)
    {:noreply, new_state}
  end
end
```

**Número de particiones**: `System.schedulers_online()` es el punto de partida. Si el sistema tiene 8 cores, 8 particiones significa que en teoría 8 requests a usuarios diferentes pueden procesarse en paralelo real. Más de `schedulers_online * 4` raramente aporta beneficio.

</details>

---

## Exercise 2 — Counter Farm

### Problem

Implementa un sistema de contadores sharded para tracking de eventos en tiempo real. El sistema recibe eventos etiquetados con una `key` (e.g., `"page_view:home"`, `"click:buy_button"`) y debe mantener un contador por key.

```elixir
# API objetivo:
MyApp.EventCounter.increment("page_view:home")
MyApp.EventCounter.increment("page_view:home")
MyApp.EventCounter.increment("click:buy_button")
MyApp.EventCounter.get("page_view:home")  # → 2
MyApp.EventCounter.get_all()              # → problema: requiere coordinar particiones
```

**El reto**: `get_all/0` necesita agregar contadores de todas las particiones. Implementa una estrategia eficiente para esto.

```elixir
defmodule MyApp.EventCounter do
  use GenServer

  def increment(key) do
    # Enrutar al proceso correcto por hash(key)
  end

  def get(key) do
    # Enrutar al proceso correcto y retornar el valor
  end

  def get_all() do
    # ¿Cómo obtienes datos de TODAS las particiones?
    # Opción A: consultar cada partición secuencialmente
    # Opción B: consultar todas en paralelo con Task.async_stream
    # Opción C: mantener un agregador separado
  end
end
```

**Tu tarea**:
1. Implementa `increment/1` y `get/1` con routing por partición
2. Implementa `get_all/0` con Task.async_stream para consulta paralela
3. ¿Qué garantías tiene `get_all/0`? ¿Es linearizable? ¿Por qué importa esto?
4. Añade `reset/1` para resetear un contador específico

### Hints

<details>
<summary>Hint 1 — Iterar sobre particiones</summary>

```elixir
# PartitionSupervisor.partitions/1 devuelve el número de particiones
n = PartitionSupervisor.partitions(MyApp.EventCounter.Partitions)

# Puedes enviar a una partición específica por índice:
GenServer.call(
  {:via, PartitionSupervisor, {MyApp.EventCounter.Partitions, partition_index}},
  :get_all_local
)
```

Cada partición responde con su mapa local. Luego agregas los mapas sumando valores por key.

</details>

<details>
<summary>Hint 2 — Consistencia de get_all</summary>

`get_all/0` que consulta N particiones en paralelo no es linearizable: las consultas a diferentes particiones llegan en momentos distintos. Entre la consulta a la partición 0 y la partición 7, pueden haber llegado nuevos incrementos a cualquiera.

Esto está bien para métricas de monitoring (toleran staleness), pero no para billing o límites de cuota. Documenta este trade-off en tu implementación.

</details>

<details>
<summary>Hint 3 — Merging de mapas</summary>

```elixir
# Fusionar varios mapas sumando valores:
maps |> Enum.reduce(%{}, fn map, acc ->
  Map.merge(acc, map, fn _key, v1, v2 -> v1 + v2 end)
end)
```

</details>

---

## Exercise 3 — Benchmark: 1 GenServer vs N Particiones

### Problem

Mide el impacto real de particionar en throughput. Implementa ambas versiones del rate limiter (una con un solo GenServer, otra con PartitionSupervisor) y benchmarkea con distintos niveles de concurrencia.

```elixir
defmodule MyApp.Benchmark do
  @users 100
  @requests_per_user 1_000

  def run() do
    # Benchmark 1: GenServer único
    single_time = benchmark_single()

    # Benchmark 2: PartitionSupervisor con System.schedulers_online() partitions
    partitioned_time = benchmark_partitioned()

    IO.puts("Single GenServer: #{single_time}ms")
    IO.puts("Partitioned (#{System.schedulers_online()} partitions): #{partitioned_time}ms")
    IO.puts("Speedup: #{Float.round(single_time / partitioned_time, 2)}x")
  end

  defp benchmark_single() do
    # Crear @users tasks, cada una enviando @requests_per_user mensajes
    # Medir tiempo total con :timer.tc
  end

  defp benchmark_partitioned() do
    # Mismo benchmark con PartitionSupervisor
  end
end
```

**Tu tarea**:
1. Implementa ambas versiones y el benchmark
2. Corre el benchmark con distintos valores de `@users` (10, 100, 1000)
3. ¿En qué punto el bottleneck del GenServer único se vuelve significativo?
4. ¿El speedup escala linealmente con el número de particiones? Explica por qué sí o no.

### Hints

<details>
<summary>Hint 1 — :timer.tc para medir</summary>

```elixir
{microseconds, result} = :timer.tc(fn ->
  # código a medir
end)
milliseconds = microseconds / 1000
```

</details>

<details>
<summary>Hint 2 — Task.async_stream para concurrencia real</summary>

```elixir
defp benchmark_single() do
  {time, _} = :timer.tc(fn ->
    1..@users
    |> Task.async_stream(fn user_id ->
      Enum.each(1..@requests_per_user, fn _ ->
        SingleGenServer.check("user_#{user_id}")
      end)
    end, max_concurrency: @users)
    |> Stream.run()
  end)

  div(time, 1000)  # ms
end
```

</details>

<details>
<summary>Hint 3 — Interpretación del resultado</summary>

El bottleneck de un GenServer único es la mailbox: todos los mensajes se encolan y procesan de uno en uno. Con 100 usuarios enviando 1000 mensajes c/u, el GenServer procesa 100,000 mensajes en serie.

Con N particiones, cada partición procesa ~100,000/N mensajes. El speedup teórico es N, pero en práctica es menor por: overhead del scheduler, phash2, y que no todas las particiones reciben el mismo load (distribución de hash).

</details>

---

## Common Mistakes

### Mistake 1 — Asumir distribución uniforme con pocas keys

```elixir
# Si solo tienes 3 tipos de eventos:
MyApp.EventCounter.increment("login")
MyApp.EventCounter.increment("logout")
MyApp.EventCounter.increment("purchase")

# Con 8 particiones, el hash distribuirá estas 3 keys en 3 particiones.
# Las otras 5 particiones estarán completamente ociosas.
# No hay beneficio de sharding — hay overhead.
```

---

### Mistake 2 — State compartido entre particiones

```elixir
# MAL: usar ETS compartido en cada partición
defmodule MyApp.Counter do
  def init(_) do
    :ets.new(:shared_counters, [:public, :named_table])  # ← nombre global único
    {:ok, %{}}
  end
  # El segundo proceso que inicia esto CRASHEA porque :shared_counters ya existe
end
```

Cada partición es un proceso independiente. Si necesitas ETS, usa `:ets.new(:counters, [:public, :set])` sin `:named_table`, y pasa la referencia en el state.

---

### Mistake 3 — Olvidar que PartitionSupervisor no persiste el número de particiones

```elixir
# Si reinicias con más particiones:
{PartitionSupervisor, partitions: 16}  # antes tenías 8

# El hash cambia: :erlang.phash2("user_123", 16) ≠ :erlang.phash2("user_123", 8)
# Todo el estado almacenado en particiones se pierde semánticamente
# (el estado físico sigue en el mismo proceso, pero las keys ahora enrutan a particiones diferentes)
```

Cambiar el número de particiones en producción requiere un plan de migración de estado.

---

### Mistake 4 — Usar la misma key como routing key y como identificador interno

```elixir
# AMBIGUO: la partición recibe el user_id como routing key
# pero el GenServer no sabe cuál user_id está procesando en init
def init(_opts) do
  # ¿Cómo sé qué user_id soy?
  # No lo sé — PartitionSupervisor pasa opts, no la routing key
  {:ok, %{}}
end

# La routing key solo determina A QUÉ proceso se envía el mensaje.
# El proceso en sí no sabe su routing key a menos que se la pases en opts.
# Para rate limiters/counters, el user_id debe ir en el MENSAJE, no en init.
```

---

## Production Patterns

### Pattern 1 — Partitions como múltiplo de schedulers

```elixir
# Para máxima utilización de CPU:
partitions: System.schedulers_online() * 2

# x2 para reducir el impacto de distribución desigual de hashes:
# con el doble de particiones, la probabilidad de que una esté sobrecargada baja.
# x4 es el máximo recomendado — más particiones = más overhead de scheduling.
```

### Pattern 2 — Inspección de estado de todas las particiones

```elixir
defmodule MyApp.Diagnostics do
  def partition_stats(supervisor_name) do
    n = PartitionSupervisor.partitions(supervisor_name)

    0..(n - 1)
    |> Task.async_stream(fn i ->
      pid = GenServer.whereis({:via, PartitionSupervisor, {supervisor_name, i}})
      %{partition: i, pid: pid, message_queue_len: Process.info(pid, :message_queue_len)}
    end)
    |> Enum.map(fn {:ok, stats} -> stats end)
  end
end
```

### Pattern 3 — PartitionSupervisor con DynamicSupervisor para procesos por entidad

```elixir
# Patrón: un proceso por entidad (e.g., Order), sharded por partition
{PartitionSupervisor,
  child_spec: DynamicSupervisor,
  name: MyApp.OrderSupervisors
}

# Iniciar un proceso para un order específico en la partición correcta:
DynamicSupervisor.start_child(
  {:via, PartitionSupervisor, {MyApp.OrderSupervisors, order_id}},
  {MyApp.OrderProcess, order_id}
)
```

---

## Resources

- [PartitionSupervisor — HexDocs](https://hexdocs.pm/elixir/PartitionSupervisor.html)
- [Elixir 1.14 Release Notes — PartitionSupervisor](https://elixir-lang.org/blog/2022/09/01/elixir-v1-14-0-released/)
- [Process.info/2](https://hexdocs.pm/elixir/Process.html#info/2) — para inspeccionar message queue
- [:erlang.phash2/2](https://www.erlang.org/doc/man/erlang.html#phash2-2)
