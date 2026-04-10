# 37. Rate Limiting de Alta Performance

**Difficulty**: Avanzado

## Prerequisites
- Sólido dominio de ETS (`:ets`) y operaciones atómicas
- Comprensión de concurrencia en BEAM (múltiples procesos simultáneos)
- Experiencia con GenServer y supervisión OTP
- Familiaridad con distributed Erlang (`:rpc`, clustering)

## Learning Objectives
After completing this exercise, you will be able to:
- Implementar token bucket con ETS para rate limiting de alta performance sin bottleneck
- Construir sliding window log para conteo exacto de requests en ventana deslizante
- Implementar sliding window counter para eficiencia a escala
- Entender los trade-offs entre exactitud y performance en cada algoritmo
- Extender rate limiting a clusters multi-nodo con coordinación distribuida
- Diagnosticar y tunear rate limiters en producción bajo alta carga

## Concepts

### Los cuatro algoritmos de rate limiting

**Fixed Window**: Cuenta requests en ventanas fijas (ej: 100 req/min). Problema: el "boundary burst" — 100 requests al final de la ventana + 100 al inicio de la siguiente = 200 en 2 segundos reales.

**Token Bucket**: Hay un bucket con N tokens. Cada request consume tokens. El bucket se recarga a rate R tokens/segundo. Permite bursts hasta N (tamaño del bucket).

```
bucket = {tokens: N, last_refill: now}
consume(cost):
  refill tokens based on elapsed time
  if tokens >= cost → tokens -= cost; allow
  else → deny
```

**Sliding Window Log**: Registrar timestamp de cada request. Para cada nueva request, contar cuántas hay en los últimos T segundos. Exacto pero costoso en memoria (O(requests)).

**Sliding Window Counter**: Aproximación eficiente del sliding window. Usa dos fixed windows (actual y anterior) y aplica un factor de peso basado en qué fracción de la ventana anterior está dentro de la ventana deslizante.

```
estimate = requests_prev * ((window_size - elapsed) / window_size) + requests_current
```

### ETS para rate limiting: por qué no GenServer

Un GenServer es un bottleneck para rate limiting: todas las requests pasan por él en serie. Con ETS, múltiples procesos leen y escriben concurrentemente:

```elixir
# Crear tabla ETS concurrent-friendly
:ets.new(:rate_limiter, [:named_table, :public, :set,
  read_concurrency: true,   # Múltiples lectores simultáneos
  write_concurrency: true   # Múltiples escritores simultáneos
])

# Operación atómica: incrementar y obtener nuevo valor
:ets.update_counter(:rate_limiter, key, {position, increment, threshold, default})
```

### Rate limiting distribuido

En un cluster, cada nodo tiene su propio ETS. Para coordinar:

**Estrategia local + sincronización periódica**: cada nodo limita localmente (limit/N para N nodos) y sincroniza contadores periódicamente con `:rpc.multicall`.

**Estrategia con nodo coordinador**: un nodo es el "master" de rate limiting; los demás le consultan. Más exacto pero con latencia de red en cada check.

**Redis como shared state**: `Redix` + Lua scripts atómicos para rate limiting exacto en cluster.

## Exercises

### Exercise 1: Token Bucket con ETS — check_and_consume/2

Implementa un rate limiter token bucket respaldado por ETS, sin GenServer, para máxima concurrencia.

```elixir
defmodule TokenBucketLimiter do
  @moduledoc """
  Token bucket rate limiter con ETS.

  Cada usuario tiene su propio bucket:
    {user_id, tokens, last_refill_timestamp}

  La recarga ocurre lazymente en cada check (no hay proceso de recarga).
  Performance: O(1) amortizado, sin bottleneck de GenServer.
  """

  @table :token_bucket_limiter

  # Configuración por defecto
  @default_capacity    100   # tokens máximos en el bucket
  @default_refill_rate 10    # tokens por segundo

  def start do
    :ets.new(@table, [
      :named_table,
      :public,
      :set,
      read_concurrency: true,
      write_concurrency: true
    ])
    :ok
  end

  @doc """
  Verifica si el usuario puede hacer una request y consume los tokens.

  Retorna:
    {:ok, tokens_remaining} — permitido
    {:error, :rate_limited, retry_after_ms} — bloqueado
  """
  def check_and_consume(user_id, cost \\ 1, opts \\ []) do
    capacity    = Keyword.get(opts, :capacity, @default_capacity)
    refill_rate = Keyword.get(opts, :refill_rate, @default_refill_rate)

    now = System.monotonic_time(:millisecond)

    case :ets.lookup(@table, user_id) do
      [] ->
        # Primera vez: crear bucket con capacidad máxima, consumir cost
        initial_tokens = capacity - cost
        :ets.insert(@table, {user_id, initial_tokens, now})
        {:ok, initial_tokens}

      [{^user_id, current_tokens, last_refill}] ->
        # Calcular cuántos tokens se han regenerado desde last_refill
        elapsed_ms     = now - last_refill
        refilled       = trunc(elapsed_ms * refill_rate / 1_000)
        tokens_after_refill = min(capacity, current_tokens + refilled)

        if tokens_after_refill >= cost do
          # TODO: Actualizar ETS con nuevos tokens y timestamp
          # tokens_after_consume = tokens_after_refill - cost
          # Actualizar el timestamp de last_refill si hubo recarga real
          new_tokens    = tokens_after_refill - cost
          new_last_refill = if refilled > 0, do: now, else: last_refill
          :ets.insert(@table, {user_id, new_tokens, new_last_refill})
          {:ok, new_tokens}
        else
          # TODO: Calcular cuántos ms hasta que haya suficientes tokens
          # tokens_needed = cost - tokens_after_refill
          # ms_needed = ceil(tokens_needed / refill_rate * 1_000)
          tokens_needed = cost - tokens_after_refill
          ms_needed     = ceil(tokens_needed / refill_rate * 1_000)
          {:error, :rate_limited, ms_needed}
        end
    end
  end

  def reset(user_id) do
    :ets.delete(@table, user_id)
    :ok
  end

  def inspect_bucket(user_id) do
    case :ets.lookup(@table, user_id) do
      []                               -> :not_found
      [{^user_id, tokens, last_refill}] -> %{tokens: tokens, last_refill: last_refill}
    end
  end
end

defmodule TokenBucketDemo do
  def run do
    TokenBucketLimiter.start()

    user = "user_alice"

    IO.puts("=== Token Bucket: capacity=10, refill=2/seg ===\n")

    IO.puts("--- Consumo normal ---")
    Enum.each(1..12, fn i ->
      result = TokenBucketLimiter.check_and_consume(user, 1,
        capacity: 10, refill_rate: 2)
      IO.puts("Request #{i}: #{inspect(result)}")
    end)

    IO.puts("\n--- Esperando recarga (1.5 segundos)... ---")
    :timer.sleep(1_500)

    IO.puts("\n--- Tras recarga ---")
    result = TokenBucketLimiter.check_and_consume(user, 1,
      capacity: 10, refill_rate: 2)
    IO.puts("Después de espera: #{inspect(result)}")

    IO.puts("\n--- Test de concurrencia: 50 procesos simultáneos ---")
    tasks = Enum.map(1..50, fn i ->
      Task.async(fn ->
        TokenBucketLimiter.check_and_consume("concurrent_user", 1,
          capacity: 20, refill_rate: 5)
      end)
    end)
    results = Task.await_many(tasks, 5_000)
    allowed  = Enum.count(results, &match?({:ok, _}, &1))
    blocked  = Enum.count(results, &match?({:error, :rate_limited, _}, &1))
    IO.puts("Permitidas: #{allowed}, Bloqueadas: #{blocked}")
  end
end

# Test it:
# TokenBucketDemo.run()
```

**Hints**:
- `System.monotonic_time(:millisecond)` es más seguro que `DateTime.utc_now()` para medir intervalos — no se ve afectado por ajustes del reloj del sistema
- El update de ETS no es atómico (read-compute-write puede tener races con múltiples procesos): para producción de alta concurrencia, usa `:ets.update_counter/3` para operaciones atómicas o acepta ligeras inexactitudes
- `trunc/1` en la recarga evita decimales; `min(capacity, current + refilled)` garantiza que no sobrepases la capacidad

**One possible solution** (sparse):
```elixir
# El cálculo central ya está dado; la parte a completar es el insert tras permitir:
new_tokens    = tokens_after_refill - cost
new_last_refill = if refilled > 0, do: now, else: last_refill
:ets.insert(@table, {user_id, new_tokens, new_last_refill})
{:ok, new_tokens}

# Para el caso bloqueado:
tokens_needed = cost - tokens_after_refill
ms_needed     = ceil(tokens_needed / refill_rate * 1_000)
{:error, :rate_limited, ms_needed}
```

---

### Exercise 2: Sliding Window — Conteo exacto de requests en los últimos 60 segundos

Implementa un sliding window log que mantiene el timestamp de cada request y cuenta exactamente cuántas hubo en los últimos 60 segundos.

```elixir
defmodule SlidingWindowLimiter do
  @moduledoc """
  Sliding window log rate limiter.

  Para cada usuario, mantiene una lista de timestamps de requests.
  En cada check, elimina los timestamps fuera de la ventana y cuenta los restantes.

  Exactitud: 100% (no aproximación)
  Trade-off: memoria O(requests_por_ventana) por usuario

  Implementado con ETS: cada fila es {user_id, [timestamp1, timestamp2, ...]}
  """

  @table :sliding_window_limiter

  def start do
    :ets.new(@table, [:named_table, :public, :set])
    :ok
  end

  @doc """
  Verifica si el usuario puede hacer una request.

  opts:
    limit: número máximo de requests en la ventana (default: 100)
    window_ms: tamaño de la ventana en ms (default: 60_000)

  Retorna:
    {:ok, count_in_window} — permitido
    {:error, :rate_limited, oldest_expires_in_ms} — bloqueado
  """
  def check_and_record(user_id, opts \\ []) do
    limit     = Keyword.get(opts, :limit, 100)
    window_ms = Keyword.get(opts, :window_ms, 60_000)

    now        = System.monotonic_time(:millisecond)
    cutoff     = now - window_ms

    # Obtener timestamps actuales del usuario
    current_timestamps = case :ets.lookup(@table, user_id) do
      []                          -> []
      [{^user_id, timestamps}]    -> timestamps
    end

    # TODO: Filtrar timestamps fuera de la ventana (menores que cutoff)
    # Contar cuántas requests hay en la ventana actual
    in_window = Enum.filter(current_timestamps, fn ts -> ts > cutoff end)
    count     = length(in_window)

    if count < limit do
      # TODO: Agregar timestamp actual a la lista y guardar en ETS
      new_timestamps = [now | in_window]
      :ets.insert(@table, {user_id, new_timestamps})
      {:ok, count + 1}
    else
      # TODO: Calcular cuándo expira el timestamp más antiguo de la ventana
      # El más antiguo + window_ms - now = tiempo hasta que haya espacio
      oldest       = Enum.min(in_window)
      expires_in   = oldest + window_ms - now
      {:error, :rate_limited, expires_in}
    end
  end

  def window_count(user_id, window_ms \\ 60_000) do
    now    = System.monotonic_time(:millisecond)
    cutoff = now - window_ms
    case :ets.lookup(@table, user_id) do
      []                       -> 0
      [{^user_id, timestamps}] -> Enum.count(timestamps, & &1 > cutoff)
    end
  end
end

defmodule SlidingWindowDemo do
  def run do
    SlidingWindowLimiter.start()

    user = "user_bob"

    IO.puts("=== Sliding Window: 5 requests / 2 segundos ===\n")

    IO.puts("--- Llenando la ventana ---")
    Enum.each(1..6, fn i ->
      result = SlidingWindowLimiter.check_and_record(user, limit: 5, window_ms: 2_000)
      IO.puts("Request #{i}: #{inspect(result)}")
    end)

    IO.puts("\nConteo actual: #{SlidingWindowLimiter.window_count(user, 2_000)}")

    IO.puts("\n--- Esperando que expire la ventana (2.1s)... ---")
    :timer.sleep(2_100)

    IO.puts("\n--- Después de expiración de ventana ---")
    result = SlidingWindowLimiter.check_and_record(user, limit: 5, window_ms: 2_000)
    IO.puts("Request tras ventana: #{inspect(result)}")
    IO.puts("Conteo actual: #{SlidingWindowLimiter.window_count(user, 2_000)}")

    IO.puts("\n=== Demostración de exactitud vs Fixed Window ===")
    IO.puts("Las requests que expiraron de la ventana quedan disponibles inmediatamente")
    IO.puts("(sin el boundary burst del fixed window)")
  end
end

# Test it:
# SlidingWindowDemo.run()
```

**Hints**:
- Almacenar timestamps en orden descendente (`[now | in_window]`) hace que `Enum.min/1` busque el más antiguo, lo cual es O(n) — para producción con listas grandes, considera una cola FIFO o una estructura más eficiente
- El cleaneo de timestamps viejos (`Enum.filter`) ocurre en cada request: es O(n) pero evita memory leaks de timestamps expirados
- Para ventanas muy largas con muchas requests (ej: 10.000 req/hora), sliding window log puede consumir demasiada memoria; considera sliding window counter como alternativa

**One possible solution** (sparse):
```elixir
# Líneas clave (ya incluidas en el esqueleto):
in_window = Enum.filter(current_timestamps, fn ts -> ts > cutoff end)
new_timestamps = [now | in_window]
:ets.insert(@table, {user_id, new_timestamps})

# Para el cálculo de expires_in:
oldest     = Enum.min(in_window)
expires_in = oldest + window_ms - now
```

---

### Exercise 3: Distributed Rate Limiter — Coordinación en Cluster de 3 Nodos

Implementa un rate limiter que funciona correctamente en un cluster de 3 nodos BEAM, distribuyendo el límite entre nodos y sincronizando periódicamente.

```elixir
defmodule DistributedRateLimiter do
  @moduledoc """
  Rate limiter distribuido para cluster de N nodos.

  Estrategia: "token bucket particionado"
  - Cada nodo recibe limit/N tokens del global limit
  - Cada nodo hace rate limiting local con su cuota
  - Cada 5 segundos, los nodos sincronizan y redistribuyen tokens no usados
  - Trade-off: un nodo puede usar hasta limit/N antes de sincronizar
    (exactitud ±limit/N, no exactitud perfecta)

  Para exactitud perfecta: usar Redis con Lua scripts atómicos.
  Para sistemas Elixir puro: este approach es el estándar.
  """
  use GenServer

  defstruct [:user_id, :local_limit, :local_tokens, :global_limit, :refill_rate, :nodes]

  @sync_interval 5_000  # Sincronizar cada 5 segundos

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def check_and_consume(user_id, cost \\ 1) do
    GenServer.call(__MODULE__, {:check_and_consume, user_id, cost})
  end

  def global_status do
    GenServer.call(__MODULE__, :global_status)
  end

  def init(opts) do
    global_limit = Keyword.get(opts, :global_limit, 100)
    refill_rate  = Keyword.get(opts, :refill_rate, 10)
    nodes        = [node() | Node.list()]
    local_limit  = div(global_limit, max(length(nodes), 1))

    # Inicializar ETS local
    :ets.new(:dist_rate_limiter, [:named_table, :public, :set])

    # Programar sincronización periódica
    Process.send_after(self(), :sync, @sync_interval)

    state = %__MODULE__{
      global_limit: global_limit,
      local_limit:  local_limit,
      local_tokens: %{},  # user_id → tokens disponibles localmente
      refill_rate:  refill_rate,
      nodes:        nodes
    }

    {:ok, state}
  end

  def handle_call({:check_and_consume, user_id, cost}, _from, state) do
    local_tokens = Map.get(state.local_tokens, user_id, state.local_limit)

    if local_tokens >= cost do
      # TODO: Consumir tokens localmente
      new_tokens = local_tokens - cost
      new_state  = %{state | local_tokens: Map.put(state.local_tokens, user_id, new_tokens)}
      {:reply, {:ok, new_tokens}, new_state}
    else
      # TODO: Intentar obtener tokens de otros nodos
      # Llamar a get_tokens_from_peers/2
      case get_tokens_from_peers(user_id, cost - local_tokens, state.nodes) do
        {:ok, extra_tokens} ->
          total_available = local_tokens + extra_tokens
          new_tokens      = total_available - cost
          new_state       = %{state | local_tokens: Map.put(state.local_tokens, user_id, new_tokens)}
          {:reply, {:ok, new_tokens}, new_state}

        {:error, :no_tokens} ->
          {:reply, {:error, :rate_limited}, state}
      end
    end
  end

  def handle_call(:global_status, _from, state) do
    # Recopilar estado de todos los nodos
    local_info = %{node: node(), local_tokens: state.local_tokens}

    remote_infos = Enum.flat_map(state.nodes -- [node()], fn remote_node ->
      case :rpc.call(remote_node, __MODULE__, :local_status, [], 1_000) do
        {:badrpc, _}  -> []
        info          -> [info]
      end
    end)

    {:reply, [local_info | remote_infos], state}
  end

  def handle_info(:sync, state) do
    # TODO: Redistribuir tokens no usados entre nodos
    # 1. Recopilar tokens sobrantes de todos los nodos via :rpc.multicall
    # 2. Calcular pool total disponible
    # 3. Redistribuir equitativamente
    # (Simplificación: en demo solo reimprimimos estado)
    nodes = [node() | Node.list()]

    IO.puts("[DistRL] Sync en #{node()} — nodos activos: #{inspect(nodes)}")

    # Redistribuir el local_limit según nodos actuales
    new_local_limit = div(state.global_limit, max(length(nodes), 1))
    new_state       = %{state | local_limit: new_local_limit, nodes: nodes}

    Process.send_after(self(), :sync, @sync_interval)
    {:noreply, new_state}
  end

  # Función pública para llamadas RPC desde otros nodos
  def local_status do
    GenServer.call(__MODULE__, :local_status_internal)
  end

  def handle_call(:local_status_internal, _from, state) do
    {:reply, %{node: node(), local_tokens: state.local_tokens, local_limit: state.local_limit}, state}
  end

  # Intentar "robar" tokens de nodos vecinos que tengan exceso
  defp get_tokens_from_peers(user_id, needed, nodes) do
    peer_nodes = nodes -- [node()]

    Enum.reduce_while(peer_nodes, {:error, :no_tokens}, fn remote_node, _acc ->
      case :rpc.call(remote_node, __MODULE__, :donate_tokens, [user_id, needed], 500) do
        {:ok, donated}  -> {:halt, {:ok, donated}}
        _               -> {:cont, {:error, :no_tokens}}
      end
    end)
  end

  # Ceder tokens a otro nodo que los necesita
  def donate_tokens(user_id, amount) do
    GenServer.call(__MODULE__, {:donate_tokens, user_id, amount})
  end

  def handle_call({:donate_tokens, user_id, amount}, _from, state) do
    available = Map.get(state.local_tokens, user_id, state.local_limit)
    can_donate = min(available, amount)

    if can_donate > 0 do
      new_tokens = available - can_donate
      new_state  = %{state | local_tokens: Map.put(state.local_tokens, user_id, new_tokens)}
      {:reply, {:ok, can_donate}, new_state}
    else
      {:reply, {:error, :no_tokens}, state}
    end
  end
end

defmodule DistributedDemo do
  @moduledoc """
  Para ejecutar este demo necesitas un cluster de 3 nodos.

  Terminal 1:
    iex --sname node1@localhost --cookie secret -S mix
    > Node.connect(:"node2@localhost")
    > DistributedRateLimiter.start_link(global_limit: 30, refill_rate: 5)

  Terminal 2:
    iex --sname node2@localhost --cookie secret -S mix
    > Node.connect(:"node1@localhost")
    > DistributedRateLimiter.start_link(global_limit: 30, refill_rate: 5)

  Terminal 3 (orchestrator):
    iex --sname node3@localhost --cookie secret -S mix
    > Node.connect(:"node1@localhost")
    > Node.connect(:"node2@localhost")
  """

  def run_local_only do
    # Demo que funciona sin cluster (single node)
    IO.puts("=== Demo local de DistributedRateLimiter ===")

    {:ok, _pid} = DistributedRateLimiter.start_link(
      global_limit: 15,
      refill_rate: 3
    )

    IO.puts("Limit global: 15, por nodo (1 nodo): 15\n")

    results = Enum.map(1..20, fn i ->
      result = DistributedRateLimiter.check_and_consume("user_charlie")
      IO.puts("Request #{i}: #{inspect(result)}")
      result
    end)

    allowed = Enum.count(results, &match?({:ok, _}, &1))
    IO.puts("\nPermitidas: #{allowed}/20 (esperado: 15)")
    IO.puts("Status: #{inspect(DistributedRateLimiter.global_status())}")
  end
end

# Test it (single node):
# DistributedDemo.run_local_only()
```

**Hints**:
- `:rpc.call/4` y `:rpc.multicall/4` son las herramientas de distribución en Erlang/OTP; manejan automáticamente serialización de términos entre nodos
- El pattern `div(global_limit, max(length(nodes), 1))` evita división por cero si `Node.list()` está vacío
- En producción con exactitud estricta, usar Redis con el algoritmo GCRA (Generic Cell Rate Algorithm) via Lua scripts atómicos es la solución estándar; este ejercicio muestra los fundamentos del approach distribuido-local

**One possible solution** (sparse):
```elixir
# La estructura central ya está implementada.
# La clave del algoritmo está en get_tokens_from_peers y donate_tokens.

# En handle_info(:sync, ...) con redistribución real:
def handle_info(:sync, state) do
  nodes        = [node() | Node.list()]
  new_limit    = div(state.global_limit, max(length(nodes), 1))
  new_state    = %{state | local_limit: new_limit, nodes: nodes}
  Process.send_after(self(), :sync, @sync_interval)
  {:noreply, new_state}
end
```

## Common Mistakes

### Mistake 1: Token bucket con race condition en ETS
```elixir
# ❌ Read → compute → write no es atómico con múltiples procesos
tokens = read_tokens(user)
new_tokens = tokens - 1            # Entre read y write, otro proceso leyó
:ets.insert(:table, {user, new_tokens})  # Sobrescribe el update del otro proceso

# ✓ Opción A: :ets.update_counter para operaciones simples (atómico)
:ets.update_counter(:table, user, {2, -1, 0, 0})

# ✓ Opción B: Serializar via GenServer (introduce bottleneck)
# ✓ Opción C: Aceptar ligera inexactitud (común en rate limiting de alto throughput)
```

### Mistake 2: Sliding window log sin limpieza de timestamps
```elixir
# ❌ Sin limpiar timestamps expirados, la lista crece indefinidamente
new_timestamps = [now | all_timestamps]  # Acumula todos los timestamps históricos

# ✓ Filtrar siempre los timestamps fuera de la ventana
in_window = Enum.filter(all_timestamps, fn ts -> ts > cutoff end)
new_timestamps = [now | in_window]
```

### Mistake 3: Fixed window con boundary burst
```elixir
# ❌ Fixed window permite el doble del límite en el boundary
# A las 23:59:59 → 100 requests (fin de ventana)
# A las 00:00:00 → 100 requests (inicio de nueva ventana)
# = 200 requests en 1 segundo real

# ✓ Sliding window evita este problema por definición
# ✓ Token bucket con bucket_size == limit también lo evita
```

### Mistake 4: Rate limiter distribuido sin fallback a local
```elixir
# ❌ Si el nodo coordinador cae, todo el rate limiting falla
case :rpc.call(master_node, RateLimiter, :check, [user, cost], 100) do
  {:badrpc, _} -> raise "Rate limiter unavailable!"  # El sistema cae con el coordinador
  result -> result
end

# ✓ Fallback a rate limiting local cuando el coordinador no responde
case :rpc.call(master_node, RateLimiter, :check, [user, cost], 100) do
  {:badrpc, _} -> LocalRateLimiter.check(user, cost)  # Degradado pero funcional
  result -> result
end
```

## Verification
```bash
iex> c("37-rate-limiting-patterns.exs")

# Exercise 1
iex> TokenBucketDemo.run()
# Verificar: primeras N requests permitidas, el resto bloqueadas con retry_after

# Exercise 2
iex> SlidingWindowDemo.run()
# Verificar: ventana exacta, no hay boundary burst

# Exercise 3 (single node)
iex> DistributedDemo.run_local_only()
# Verificar: exactamente 15 permitidas (local_limit == global_limit en 1 nodo)
```

Checklist de verificación:
- [ ] Token bucket: tokens no superan `capacity` tras recarga
- [ ] Token bucket: `check_and_consume` devuelve `retry_after_ms` correcto
- [ ] Sliding window: timestamps expirados se eliminan en cada request
- [ ] Sliding window: después de la ventana completa, nuevas requests se permiten
- [ ] Distributed: `local_limit = global_limit / num_nodes`
- [ ] Distributed: `handle_info(:sync, ...)` programa el próximo sync

## Summary
- Token bucket: el algoritmo más versátil — permite bursts controlados, implementación O(1) con ETS
- Sliding window log: exactitud perfecta a costa de memoria O(requests_en_ventana)
- Sliding window counter: aproximación del sliding window con O(1) memoria — buena para alta escala
- ETS con `read_concurrency: true` elimina el bottleneck del GenServer para rate limiting
- Rate limiting distribuido: trade-off entre exactitud (Redis) y resiliencia (local + sync)
- Calibrar el algoritmo según el caso: ¿bursts son aceptables? ¿exactitud es crítica? ¿distributed o single node?

## What's Next
**38-mox-testing**: Ahora que has construido sistemas complejos con dependencias externas (HTTP clients, servicios externos), aprende a testearlos correctamente con mocks basados en behaviours.

## Resources
- [ETS — Erlang Docs](https://www.erlang.org/doc/man/ets.html)
- [Rate Limiting Algorithms — Stripe Blog](https://stripe.com/blog/rate-limiters)
- [An Introduction to Rate Limiting](https://www.figma.com/blog/an-introduction-to-rate-limiting/)
- [Hammer — Elixir rate limiting library](https://github.com/ExHammer/hammer)
