# 13 — ETS Básico

## Prerequisites

- Procesos en Elixir (spawn, send, receive)
- GenServer básico (Ejercicio 04)
- Comprensión de concurrencia y estado compartido
- Pattern matching sobre tuplas

---

## Learning Objectives

Al terminar este ejercicio serás capaz de:

1. Crear y configurar tablas ETS con distintos tipos (`:set`, `:bag`, `:ordered_set`)
2. Insertar, buscar, actualizar y eliminar registros con las operaciones fundamentales de `:ets`
3. Distinguir entre acceso público, protegido y privado
4. Entender el ciclo de vida de una tabla ETS y su relación con el proceso propietario
5. Implementar patrones comunes: caché, rate limiting y leaderboard
6. Razonar sobre las garantías de atomicidad de `update_counter/3`

---

## Concepts

### ¿Qué es ETS?

ETS (Erlang Term Storage) es un almacén de términos en memoria, incorporado en la VM de Erlang/Elixir. A diferencia de los procesos con estado (Agent, GenServer), ETS permite que **múltiples procesos lean y escriban concurrentemente** en la misma tabla sin mensajes de por medio.

Características clave:
- Acceso O(1) para `:set` y `:bag` (tabla hash)
- Acceso O(log N) para `:ordered_set` (árbol ordenado)
- Atómico por fila (no por transacción)
- Destruido cuando muere el proceso propietario (a menos que se transfiera)

### Crear una tabla

```elixir
# Forma básica
table = :ets.new(:mi_tabla, [:set, :public])

# Con nombre para acceso global por átomo
table = :ets.new(:cache, [:set, :named_table, :public])

# Opciones de tipo de tabla:
# :set          — clave única, orden indefinido (más común)
# :ordered_set  — clave única, ordenada por clave (útil para leaderboards)
# :bag          — clave duplicable, valores únicos por clave
# :duplicate_bag — clave y valor pueden repetirse

# Opciones de acceso:
# :public    — cualquier proceso puede leer y escribir
# :protected — cualquier proceso puede leer; solo el propietario escribe (default)
# :private   — solo el propietario puede leer y escribir

# Opciones adicionales:
# :named_table — permite acceder por nombre (:cache) en lugar de ref
# {:keypos, N} — usa la posición N del tuple como clave (default: 1)
```

### Operaciones CRUD

```elixir
# INSERT — inserta una tupla; la primera posición es la clave
:ets.insert(:cache, {"user:1", %{name: "Ana", age: 30}})
:ets.insert(:cache, {"user:2", %{name: "Bob", age: 25}})

# INSERT — múltiples registros en una sola llamada (atómica)
:ets.insert(:cache, [{"a", 1}, {"b", 2}, {"c", 3}])

# LOOKUP — retorna lista de tuplas que coinciden con la clave
:ets.lookup(:cache, "user:1")
# => [{"user:1", %{name: "Ana", age: 30}}]

:ets.lookup(:cache, "no-existe")
# => []

# Patrón común: extraer el valor directamente
case :ets.lookup(:cache, key) do
  [{^key, value}] -> {:ok, value}
  []              -> :miss
end

# DELETE — elimina todas las entradas con esa clave
:ets.delete(:cache, "user:1")

# DELETE completo — elimina la tabla
:ets.delete(:cache)

# UPDATE_COUNTER — incremento atómico (no requiere read-modify-write)
# :ets.update_counter(tabla, clave, {posicion, incremento})
:ets.insert(:counters, {:page_views, 0})
:ets.update_counter(:counters, :page_views, {2, 1})
# => 1  (retorna el nuevo valor)
```

### Consultas con match_object y select

```elixir
# match_object — busca por patrón con wildcards (:_)
:ets.match_object(:cache, {:"$1", %{age: :"$2"}})

# select con match spec — más expresivo
match_spec = [
  {
    {"user:$1", %{age: :"$2"}},   # patrón
    [{:>, :"$2", 25}],             # guardia: age > 25
    [:"$_"]                        # retornar el registro completo
  }
]
:ets.select(:cache, match_spec)

# tab2list — obtener TODOS los registros (evitar en tablas grandes)
:ets.tab2list(:cache)
```

### Ciclo de vida y transferencia

```elixir
# La tabla pertenece al proceso que la crea
# Si ese proceso muere, la tabla se destruye

# Para que la tabla sobreviva, transfiérela a otro proceso
:ets.give_away(table, new_owner_pid, gift_data)
# El nuevo propietario recibe: {'ETS-TRANSFER', table, old_owner, gift_data}

# Alternativa: hacer al proceso supervisor el propietario
# y que el GenServer solo opere sobre ella
```

### Patrón: GenServer como guardián de tabla ETS

El patrón más común es que un GenServer sea el propietario de la tabla y exponga una API limpia, mientras que las lecturas las hacen los clientes directamente (sin pasar mensajes al GenServer):

```elixir
defmodule Cache do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def get(key), do: # lectura directa desde ETS — no pasa por GenServer

  def put(key, value, ttl_ms), do: # escritura directa o vía GenServer

  def init(_opts) do
    table = :ets.new(:cache_table, [:set, :named_table, :public, read_concurrency: true])
    {:ok, %{table: table}}
  end
end
```

---

## Exercises

### Ejercicio 1 — Caché de resultados costosos con ETS

Implementa un módulo `ResultCache` que memoiza el resultado de funciones costosas usando ETS con soporte de TTL (time-to-live).

```elixir
# Archivo: lib/result_cache.ex

defmodule ResultCache do
  use GenServer

  @table :result_cache

  # --- API pública ---

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Ejecuta `fun` y cachea el resultado bajo `key` durante `ttl_ms` milisegundos.
  Si ya existe un valor válido en caché, retorna ese valor sin ejecutar `fun`.

      iex> ResultCache.start_link()
      iex> ResultCache.fetch("pi", 5_000, fn -> :math.pi() end)
      3.141592653589793
  """
  def fetch(key, ttl_ms, fun) do
    # TODO: Implementar la lógica de caché:
    #
    # 1. Buscar la clave en ETS con :ets.lookup(@table, key)
    # 2. Si existe, verificar si el timestamp de expiración es > System.monotonic_time(:millisecond)
    #    a. Si no ha expirado -> retornar el valor cacheado
    #    b. Si expiró -> calcular, insertar y retornar el nuevo valor
    # 3. Si no existe -> ejecutar fun.(), insertar en ETS y retornar el valor
    #
    # Estructura del registro ETS:
    # {key, value, expires_at_ms}
    # donde expires_at_ms = System.monotonic_time(:millisecond) + ttl_ms
  end

  @doc """
  Elimina manualmente una entrada del caché.
  """
  def invalidate(key) do
    # TODO: Eliminar la entrada con :ets.delete/2
  end

  @doc """
  Elimina todas las entradas del caché.
  """
  def flush do
    # TODO: Eliminar todos los registros con :ets.delete_all_objects/1
  end

  @doc """
  Retorna el número de entradas en el caché (incluyendo expiradas).
  """
  def size do
    # TODO: Usar :ets.info(@table, :size)
  end

  # --- Callbacks de GenServer ---

  def init(_opts) do
    # TODO: Crear la tabla ETS con las opciones apropiadas
    # La tabla debe ser accesible por cualquier proceso (lectura directa)
    # Usar :named_table para acceder por @table
    #
    # Programar limpieza periódica de entradas expiradas cada 60 segundos
    # con Process.send_after(self(), :cleanup, 60_000)
    #
    # {:ok, %{}}
  end

  def handle_info(:cleanup, state) do
    # TODO: Eliminar entradas expiradas
    # :ets.select_delete puede hacerlo de forma eficiente
    #
    # match_spec para borrar entradas donde expires_at < now:
    # [{{"_", "_", :"$1"}, [{:<, :"$1", now}], [true]}]
    #
    # Reprogramar el siguiente cleanup
    Process.send_after(self(), :cleanup, 60_000)
    {:noreply, state}
  end
end
```

```elixir
# Archivo: test/result_cache_test.exs

defmodule ResultCacheTest do
  use ExUnit.Case

  setup do
    # Arrancar el caché antes de cada test y pararlo después
    {:ok, pid} = ResultCache.start_link()
    on_exit(fn -> Process.exit(pid, :kill) end)
    :ok
  end

  describe "fetch/3" do
    test "ejecuta la funcion y retorna su resultado" do
      result = ResultCache.fetch("key1", 5_000, fn -> 42 end)
      assert result == 42
    end

    test "no ejecuta la funcion dos veces si el TTL no ha expirado" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      expensive = fn ->
        Agent.update(counter, &(&1 + 1))
        :resultado
      end

      ResultCache.fetch("key2", 5_000, expensive)
      ResultCache.fetch("key2", 5_000, expensive)

      # La función solo se ejecutó una vez
      assert Agent.get(counter, & &1) == 1
      Agent.stop(counter)
    end

    test "re-ejecuta la funcion cuando el TTL ha expirado" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)

      expensive = fn ->
        Agent.update(counter, &(&1 + 1))
        :resultado
      end

      # TTL de 1ms — expira inmediatamente
      ResultCache.fetch("key3", 1, expensive)
      Process.sleep(5)
      ResultCache.fetch("key3", 1, expensive)

      assert Agent.get(counter, & &1) == 2
      Agent.stop(counter)
    end

    test "distintas claves se cachean independientemente" do
      assert ResultCache.fetch("a", 5_000, fn -> 1 end) == 1
      assert ResultCache.fetch("b", 5_000, fn -> 2 end) == 2
    end
  end

  describe "invalidate/1" do
    test "fuerza re-ejecucion en el siguiente fetch" do
      {:ok, counter} = Agent.start_link(fn -> 0 end)
      fun = fn -> Agent.get_and_update(counter, fn n -> {n, n + 1} end) end

      ResultCache.fetch("inv_key", 60_000, fun)
      ResultCache.invalidate("inv_key")
      ResultCache.fetch("inv_key", 60_000, fun)

      assert Agent.get(counter, & &1) == 2
      Agent.stop(counter)
    end
  end

  describe "size/0" do
    test "refleja el numero de entradas cacheadas" do
      assert ResultCache.size() == 0
      ResultCache.fetch("x", 5_000, fn -> :x end)
      ResultCache.fetch("y", 5_000, fn -> :y end)
      assert ResultCache.size() == 2
    end
  end
end
```

---

### Ejercicio 2 — Rate Limiter básico con ETS

Implementa un rate limiter de ventana deslizante que permite un máximo de N solicitudes por ventana de tiempo, usando `update_counter/3` para atomicidad.

```elixir
# Archivo: lib/rate_limiter.ex

defmodule RateLimiter do
  use GenServer

  @table :rate_limiter

  # --- API pública ---

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Verifica si `client_id` puede realizar una solicitud.

  Retorna:
  - `{:ok, remaining}` si se permite, donde `remaining` es el número de solicitudes restantes
  - `{:error, :rate_limited, reset_at_ms}` si se ha excedido el límite

  ## Parámetros
  - `client_id` — identificador único del cliente (string o atom)
  - `limit` — máximo de solicitudes permitidas en la ventana
  - `window_ms` — duración de la ventana en milisegundos
  """
  def check(client_id, limit, window_ms) do
    # TODO: Implementar la lógica de rate limiting:
    #
    # El key en ETS es el client_id.
    # El registro ETS almacena: {client_id, count, window_start_ms}
    #
    # Algoritmo:
    # 1. Buscar el registro del cliente en ETS
    # 2a. Si no existe: insertar {client_id, 1, now} y retornar {:ok, limit - 1}
    # 2b. Si existe y la ventana ha expirado (now > window_start + window_ms):
    #     Resetear: insertar {client_id, 1, now}, retornar {:ok, limit - 1}
    # 2c. Si existe y la ventana es válida:
    #     - Si count < limit: incrementar count con update_counter, retornar {:ok, remaining}
    #     - Si count >= limit: retornar {:error, :rate_limited, window_start + window_ms}
    #
    # IMPORTANTE: update_counter/3 es atómica para el incremento:
    # :ets.update_counter(@table, client_id, {2, 1})  — incrementa posición 2 en 1
  end

  @doc """
  Resetea el contador de un cliente específico.
  """
  def reset(client_id) do
    # TODO: Eliminar el registro del cliente
  end

  # --- Callbacks ---

  def init(_opts) do
    # TODO: Crear tabla ETS apropiada
    # La tabla debe soportar acceso concurrente eficiente
    # Considera: read_concurrency: true, write_concurrency: true
    #
    # {:ok, %{}}
  end
end
```

```elixir
# Archivo: test/rate_limiter_test.exs

defmodule RateLimiterTest do
  use ExUnit.Case

  setup do
    {:ok, pid} = RateLimiter.start_link()
    on_exit(fn -> Process.exit(pid, :kill) end)
    :ok
  end

  describe "check/3" do
    test "permite solicitudes dentro del limite" do
      assert {:ok, 2} = RateLimiter.check("client_a", 3, 60_000)
      assert {:ok, 1} = RateLimiter.check("client_a", 3, 60_000)
      assert {:ok, 0} = RateLimiter.check("client_a", 3, 60_000)
    end

    test "rechaza solicitudes que exceden el limite" do
      RateLimiter.check("client_b", 2, 60_000)
      RateLimiter.check("client_b", 2, 60_000)

      assert {:error, :rate_limited, _reset_at} = RateLimiter.check("client_b", 2, 60_000)
    end

    test "resetea el contador cuando expira la ventana" do
      RateLimiter.check("client_c", 1, 10)
      # Primera solicitud usa el límite
      assert {:error, :rate_limited, _} = RateLimiter.check("client_c", 1, 10)

      # Esperar a que expire la ventana
      Process.sleep(20)

      # Ahora debe permitir de nuevo
      assert {:ok, 0} = RateLimiter.check("client_c", 1, 10)
    end

    test "distintos clientes tienen contadores independientes" do
      RateLimiter.check("client_x", 1, 60_000)
      assert {:error, :rate_limited, _} = RateLimiter.check("client_x", 1, 60_000)

      # client_y no se ve afectado por client_x
      assert {:ok, 0} = RateLimiter.check("client_y", 1, 60_000)
    end

    test "reset_at es posterior al momento actual" do
      RateLimiter.check("client_d", 1, 60_000)
      {:error, :rate_limited, reset_at} = RateLimiter.check("client_d", 1, 60_000)

      assert reset_at > System.monotonic_time(:millisecond)
    end
  end

  describe "reset/1" do
    test "permite solicitudes inmediatamente despues del reset" do
      RateLimiter.check("client_e", 1, 60_000)
      assert {:error, :rate_limited, _} = RateLimiter.check("client_e", 1, 60_000)

      RateLimiter.reset("client_e")
      assert {:ok, 0} = RateLimiter.check("client_e", 1, 60_000)
    end
  end
end
```

---

### Ejercicio 3 — Leaderboard ordenado con `:ordered_set`

Implementa un leaderboard (tabla de clasificación) que mantiene jugadores ordenados por puntuación, usando `:ordered_set` de ETS cuya clave compuesta garantiza el orden.

```elixir
# Archivo: lib/leaderboard.ex

defmodule Leaderboard do
  use GenServer

  @table :leaderboard

  # --- API pública ---

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  @doc """
  Registra o actualiza la puntuación de un jugador.
  Si el jugador ya existe, actualiza su puntuación.

      iex> Leaderboard.record("ana", 1500)
      :ok
  """
  def record(player_id, score) do
    GenServer.call(__MODULE__, {:record, player_id, score})
  end

  @doc """
  Retorna los N mejores jugadores en orden descendente de puntuación.

      iex> Leaderboard.top(3)
      [{"ana", 2000}, {"bob", 1800}, {"carlos", 1200}]
  """
  def top(n) do
    # TODO: Implementar consulta de top N
    #
    # Con :ordered_set, el orden es ASCENDENTE por clave.
    # Si la clave es {score, player_id}, los registros más altos están al final.
    # Usa :ets.select con un match_spec o :ets.tab2list + sort para obtener
    # los top N en orden descendente.
    #
    # Pista de diseño: almacena con clave {-score, player_id} para que
    # :ordered_set los ordene de mayor a menor naturalmente.
    # El registro ETS sería: {{-score, player_id}, player_id, score}
    #
    # Para el top N, basta con :ets.select con limit:
    # :ets.select(@table, match_spec, n) => {results, continuation}
  end

  @doc """
  Retorna la posición (rank) de un jugador específico (1-indexed).
  Retorna nil si el jugador no existe.

      iex> Leaderboard.rank("bob")
      2
  """
  def rank(player_id) do
    GenServer.call(__MODULE__, {:rank, player_id})
  end

  @doc """
  Retorna la puntuación de un jugador. nil si no existe.
  """
  def score(player_id) do
    # TODO: Leer directamente desde ETS
    # El desafío: el índice primario es {-score, player_id}.
    # Necesitas un índice secundario o un lookup distinto.
    #
    # Opción A: Mantener tabla secundaria {player_id -> score}
    # Opción B: :ets.match_object(@table, {{:"$1", player_id}, player_id, :"$2"})
  end

  @doc """
  Elimina a un jugador del leaderboard.
  """
  def remove(player_id) do
    GenServer.call(__MODULE__, {:remove, player_id})
  end

  # --- Callbacks ---

  def init(_opts) do
    # TODO: Crear dos tablas ETS:
    # 1. @table (:ordered_set) — índice primario por {-score, player_id}
    # 2. :leaderboard_players (:set) — índice secundario player_id -> score
    #    para lookups eficientes por nombre
    #
    # {:ok, %{}}
  end

  def handle_call({:record, player_id, score}, _from, state) do
    # TODO: Implementar insert/update:
    #
    # 1. Buscar si el jugador ya existe en el índice secundario
    # 2. Si existe, eliminar la entrada antigua del índice primario
    #    (la clave es {-old_score, player_id})
    # 3. Insertar nueva entrada en el índice primario: {{-score, player_id}, player_id, score}
    # 4. Actualizar el índice secundario: {player_id, score}
    #
    # {:reply, :ok, state}
  end

  def handle_call({:rank, player_id}, _from, state) do
    # TODO: Calcular el rank del jugador
    #
    # Estrategia: el rank es la posición en el ordered_set.
    # Con :ets.select_count puedes contar cuántos jugadores tienen
    # mejor puntuación (clave < {-score, player_id} en términos de score).
    #
    # Rank = número de jugadores con score > score_del_jugador + 1
    #
    # {:reply, rank, state}
  end

  def handle_call({:remove, player_id}, _from, state) do
    # TODO: Eliminar de ambas tablas
    # {:reply, :ok, state}
  end
end
```

```elixir
# Archivo: test/leaderboard_test.exs

defmodule LeaderboardTest do
  use ExUnit.Case

  setup do
    {:ok, pid} = Leaderboard.start_link()
    on_exit(fn -> Process.exit(pid, :kill) end)
    :ok
  end

  describe "record/2 y top/1" do
    test "retorna jugadores en orden descendente de puntuacion" do
      Leaderboard.record("ana", 1500)
      Leaderboard.record("bob", 2000)
      Leaderboard.record("carlos", 1200)

      assert Leaderboard.top(3) == [
               {"bob", 2000},
               {"ana", 1500},
               {"carlos", 1200}
             ]
    end

    test "top N limita los resultados correctamente" do
      Leaderboard.record("p1", 100)
      Leaderboard.record("p2", 200)
      Leaderboard.record("p3", 300)
      Leaderboard.record("p4", 400)

      result = Leaderboard.top(2)
      assert length(result) == 2
      assert {"p4", 400} = hd(result)
    end

    test "actualizar puntuacion refleja el nuevo orden" do
      Leaderboard.record("ana", 1000)
      Leaderboard.record("bob", 2000)

      # Ana mejora su puntuación
      Leaderboard.record("ana", 3000)

      [first | _] = Leaderboard.top(2)
      assert {"ana", 3000} = first
    end

    test "top con mas N que jugadores retorna todos los jugadores" do
      Leaderboard.record("solo", 999)
      assert Leaderboard.top(10) == [{"solo", 999}]
    end
  end

  describe "rank/1" do
    test "el jugador con mayor puntuacion tiene rank 1" do
      Leaderboard.record("primero", 9999)
      Leaderboard.record("segundo", 5000)
      Leaderboard.record("tercero", 1000)

      assert Leaderboard.rank("primero") == 1
      assert Leaderboard.rank("segundo") == 2
      assert Leaderboard.rank("tercero") == 3
    end

    test "retorna nil para jugadores que no existen" do
      assert Leaderboard.rank("inexistente") == nil
    end

    test "el rank se actualiza cuando mejora la puntuacion" do
      Leaderboard.record("a", 100)
      Leaderboard.record("b", 200)

      assert Leaderboard.rank("a") == 2

      Leaderboard.record("a", 999)
      assert Leaderboard.rank("a") == 1
    end
  end

  describe "remove/1" do
    test "eliminar un jugador lo saca del leaderboard" do
      Leaderboard.record("temp", 500)
      Leaderboard.record("keep", 300)

      Leaderboard.remove("temp")

      result = Leaderboard.top(10)
      player_ids = Enum.map(result, fn {id, _} -> id end)
      refute "temp" in player_ids
    end

    test "el rank se recalcula tras eliminar un jugador" do
      Leaderboard.record("a", 100)
      Leaderboard.record("b", 200)
      Leaderboard.record("c", 300)

      assert Leaderboard.rank("a") == 3
      Leaderboard.remove("c")
      assert Leaderboard.rank("a") == 2
    end
  end
end
```

---

## Common Mistakes

### 1. Confundir la semántica de `:set` vs `:bag`

```elixir
# :set — insertar con clave existente REEMPLAZA el registro
:ets.new(:s, [:set, :public, :named_table])
:ets.insert(:s, {:a, 1})
:ets.insert(:s, {:a, 2})
:ets.lookup(:s, :a)   # => [{:a, 2}]  — el 1 fue reemplazado

# :bag — permite múltiples valores por clave SI son distintos
:ets.new(:b, [:bag, :public, :named_table])
:ets.insert(:b, {:a, 1})
:ets.insert(:b, {:a, 2})
:ets.lookup(:b, :a)   # => [{:a, 1}, {:a, 2}]
```

### 2. Acceso a tabla desde proceso no propietario sin `:public`

```elixir
# La tabla con acceso :protected (default) solo puede ser escrita por su propietario
table = :ets.new(:mi_tabla, [:set, :protected])

spawn(fn ->
  :ets.insert(table, {:key, :value})  # ** (ArgumentError) — acceso denegado
end)

# Solución: usar :public o encapsular escrituras en el proceso propietario
table = :ets.new(:mi_tabla, [:set, :public])
```

### 3. Asumir que la tabla sobrevive al proceso propietario

```elixir
# Si el GenServer propietario muere y se reinicia, la tabla ETS se destruye
# y los datos se pierden. Esto es por diseño.
#
# Patrón para tabla persistente: crear la tabla en el Supervisor,
# pasarla al GenServer, y transferirla de vuelta al Supervisor si el GenServer muere.
# O simplemente aceptar que ETS es cache volátil, no estado persistente.
```

### 4. Race condition al hacer read-modify-write manual

```elixir
# INCORRECTO — no atómico, vulnerable a race conditions con múltiples procesos
[{key, count}] = :ets.lookup(:counters, :hits)
:ets.insert(:counters, {key, count + 1})

# CORRECTO — update_counter es atómica
:ets.update_counter(:counters, :hits, {2, 1})
```

### 5. Usar `:tab2list` en tablas grandes

```elixir
# tab2list carga TODA la tabla en memoria — evitar en producción
# con tablas de millones de registros
:ets.tab2list(:huge_table)  # puede agotar la memoria

# Preferir :ets.select con límite o :ets.foldl para procesar incrementalmente
:ets.foldl(fn record, acc -> [record | acc] end, [], :huge_table)
```

---

## Verification

```bash
# Crear proyecto
mix new ets_exercises --module EtsExercises
cd ets_exercises

# Ejecutar tests
mix test

# Tests individuales
mix test test/result_cache_test.exs
mix test test/rate_limiter_test.exs
mix test test/leaderboard_test.exs

# Inspeccionar tablas ETS en iex
iex -S mix
iex> ResultCache.start_link()
iex> ResultCache.fetch("demo", 5_000, fn -> :math.sqrt(144) end)
iex> :ets.tab2list(:result_cache)
iex> :ets.info(:result_cache)

# Ver todas las tablas ETS activas en la VM
iex> :ets.all()
iex> :ets.all() |> Enum.map(&:ets.info(&1, :name))
```

Salida esperada al pasar todos los tests:

```
Finished in 0.08 seconds
19 tests, 0 failures
```

---

## Summary

ETS es la solución de alta concurrencia para estado compartido en Elixir/Erlang sin el cuello de botella de un único proceso propietario del estado.

| Tipo de tabla | Semántica | Caso de uso |
|---|---|---|
| `:set` | Clave única, orden indefinido | Caché, diccionario |
| `:ordered_set` | Clave única, orden por clave | Leaderboards, índices |
| `:bag` | Multi-valor por clave, sin duplicados exactos | Tags, membresías |
| `:duplicate_bag` | Multi-valor por clave, con duplicados | Log de eventos |

| Operación | Complejidad | Nota |
|---|---|---|
| `insert/2` | O(1) para :set | Reemplaza en :set |
| `lookup/2` | O(1) para :set | Retorna lista |
| `delete/2` | O(1) | Por clave |
| `update_counter/3` | O(1) | Atómica |
| `select/2` | O(n) | Depende del match |

---

## What's Next

- **Ejercicio 14 — Registry Dinámico**: Para registro de procesos por nombre, ETS no es la herramienta; Registry proporciona semántica de proceso.
- **Ejercicio 26 — DynamicSupervisor**: Combinar DynamicSupervisor con ETS para caché con TTL supervisado.
- Explora `:mnesia` para tablas distribuidas y persistencia en disco.
- Investiga `Cachex` — biblioteca popular que usa ETS internamente con TTL, límites, y estadísticas.

---

## Resources

- [Erlang Docs — ETS](https://www.erlang.org/doc/man/ets.html)
- [Elixir School — ETS](https://elixirschool.com/en/lessons/storage/ets)
- [Saša Jurić — ETS: Erlang Term Storage](https://www.theerlangelist.com/article/ets_access)
- [Elixir in Action — Chapter 9: Storing data with ETS](https://www.manning.com/books/elixir-in-action-second-edition)
- [Cachex — librería de caché sobre ETS](https://hexdocs.pm/cachex)
