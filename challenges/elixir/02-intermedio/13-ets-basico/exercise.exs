# =============================================================================
# Ejercicio 13: ETS Básico — Erlang Term Storage
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Procesos Elixir básicos (PIDs, spawn)
# - Tuplas y pattern matching
# - Módulos y funciones
# - Conceptos básicos de concurrencia en BEAM

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Crear y configurar tablas ETS con :ets.new/2
# 2. Insertar y leer datos con :ets.insert/2 y :ets.lookup/2
# 3. Diferenciar entre tipos de tabla: :set, :bag, :ordered_set
# 4. Acceder a tablas por nombre con :named_table
# 5. Implementar el patrón de caché con ETS y compute-on-miss

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# ¿QUÉ ES ETS?
# ETS (Erlang Term Storage) es una estructura de datos en memoria que
# permite almacenar grandes cantidades de datos de forma eficiente y
# accesible desde múltiples procesos simultáneamente.
#
# A diferencia del estado de un GenServer (privado al proceso), ETS
# es una tabla COMPARTIDA — cualquier proceso con acceso puede leer/escribir.
#
# CREACIÓN:
#   :ets.new(:my_table, [:set, :public, :named_table])
#   :ets.new(:my_table, [:bag, :protected])
#
# Opciones de tipo:
#   :set         — una sola entrada por key (comportamiento por defecto)
#   :ordered_set — como :set pero ordenado por key (más lento en escritura)
#   :bag         — múltiples entradas con la misma key, valores distintos
#   :duplicate_bag — múltiples entradas duplicadas permitidas
#
# Opciones de acceso:
#   :public    — cualquier proceso puede leer y escribir
#   :protected — solo el proceso dueño puede escribir; todos pueden leer
#   :private   — solo el proceso dueño puede leer y escribir (default)
#
# Opción de nombre:
#   :named_table — permite acceder por nombre de átomo en lugar de por referencia
#
# OPERACIONES BÁSICAS:
#   :ets.insert(table, {key, value})           # inserta o reemplaza
#   :ets.insert(table, [{k1, v1}, {k2, v2}])   # inserción múltiple
#   :ets.lookup(table, key)                    # retorna [] o [{key, value}]
#   :ets.delete(table, key)                    # elimina la entrada
#   :ets.delete(table)                         # elimina la tabla completa
#   :ets.match(table, pattern)                 # búsqueda por patrón
#   :ets.tab2list(table)                       # todas las entradas como lista
#   :ets.info(table)                           # metadatos de la tabla
#
# FORMATO DE DATOS:
# ETS almacena tuplas. El primer elemento es la clave por defecto.
#   :ets.insert(table, {:user_1, "Alice", :admin})
#   :ets.lookup(table, :user_1)
#   # => [{:user_1, "Alice", :admin}]  — siempre retorna una lista
#
# PROPIEDAD Y CICLO DE VIDA:
# La tabla ETS pertenece al proceso que la creó. Cuando ese proceso muere,
# la tabla se destruye automáticamente (a menos que uses :heir).

# =============================================================================
# Exercise 1: Operaciones básicas — insert, lookup, delete
# =============================================================================
#
# Completa el módulo ETSBasic con las funciones que hacen operaciones
# elementales sobre una tabla ETS de tipo :set.
#
# `setup/0`: crea una tabla :set, :public y retorna el table ref
# `put/3`: inserta {key, value} en la tabla
# `get/2`: busca por key, retorna el valor o :not_found si no existe
# `remove/2`: elimina la entrada con la key dada
#
# Tip: :ets.lookup retorna [] o [{key, value}], desestructura con pattern matching

defmodule ETSBasic do
  def setup do
    # TODO: :ets.new(:basic_cache, [:set, :public])
  end

  def put(table, key, value) do
    # TODO: :ets.insert(table, {key, value})
    # Nota: insert/2 retorna true si tiene éxito
  end

  def get(table, key) do
    # TODO: usa :ets.lookup(table, key) y pattern match:
    # [{^key, value}] -> value
    # []              -> :not_found
  end

  def remove(table, key) do
    # TODO: :ets.delete(table, key)
  end
end

# =============================================================================
# Exercise 2: Named table — acceso por nombre de átomo
# =============================================================================
#
# Las named tables permiten acceder a la tabla por nombre (átomo) desde
# cualquier proceso sin necesitar la referencia original.
#
# `init/0`: crea la tabla :session_store con :named_table
#          Si ya existe, no falla (usa try/rescue)
# `store_session/2`: guarda {session_id, data} en :session_store
# `get_session/1`: busca por session_id en :session_store
# `invalidate/1`: elimina una sesión por session_id

defmodule SessionStore do
  @table_name :session_store

  def init do
    # TODO: crear tabla named con try/rescue para idempotencia
    # try do
    #   :ets.new(@table_name, [:set, :public, :named_table])
    # rescue
    #   ArgumentError -> @table_name  # tabla ya existe
    # end
  end

  def store_session(session_id, data) do
    # TODO: :ets.insert(@table_name, {session_id, data})
    # Usar el átomo @table_name directamente (no necesita referencia)
  end

  def get_session(session_id) do
    # TODO: lookup y extrae data o retorna :not_found
  end

  def invalidate(session_id) do
    # TODO: :ets.delete(@table_name, session_id)
  end
end

# =============================================================================
# Exercise 3: Bag table — múltiples valores por key
# =============================================================================
#
# Una tabla :bag permite varias tuplas con la misma key (siempre que sean
# distintas en algún otro elemento). Útil para eventos, tags, logs.
#
# `setup/0`: crea tabla :bag, :public
# `add_event/3`: agrega {user_id, event_type, timestamp} a la tabla
# `get_events/2`: retorna todos los eventos de un user_id
# `count_events/2`: cuenta cuántos eventos tiene un user_id

defmodule EventLog do
  def setup do
    # TODO: :ets.new(:event_log, [:bag, :public])
  end

  def add_event(table, user_id, event_type) do
    timestamp = System.monotonic_time(:millisecond)
    # TODO: :ets.insert(table, {user_id, event_type, timestamp})
  end

  def get_events(table, user_id) do
    # TODO: :ets.lookup(table, user_id) retorna lista de tuplas
    # [{user_id, event_type, timestamp}, ...]
    # Transforma a lista de %{type: event_type, at: timestamp}
  end

  def count_events(table, user_id) do
    # TODO: length(:ets.lookup(table, user_id))
  end
end

# =============================================================================
# Exercise 4: Ordered set — traversal en orden
# =============================================================================
#
# Una tabla :ordered_set mantiene las entradas ordenadas por key.
# Útil para leaderboards, logs con timestamp, o rangos.
#
# `setup/0`: crea tabla :ordered_set, :public
# `insert_score/3`: guarda {user_id, score} — usa score como key para ordenar
#                   Tip: invierte el score (1000 - score) para orden descendente
#                   O usa score directamente para orden ascendente
# `top_n/2`: retorna los N usuarios con mayor score
#            Tip: :ets.tab2list/1 + Enum.sort + Enum.take

defmodule Leaderboard do
  def setup do
    # TODO: :ets.new(:leaderboard, [:ordered_set, :public])
  end

  def insert_score(table, user_id, score) do
    # TODO: :ets.insert(table, {score, user_id})
    # Nota: usamos {score, user_id} con score como clave para aprovechar el orden
    # Si scores duplicados son posibles, usa {score, user_id} como clave compuesta
  end

  def top_n(table, n) do
    # TODO:
    # :ets.tab2list(table)
    # |> Enum.sort_by(fn {score, _} -> score end, :desc)
    # |> Enum.take(n)
    # |> Enum.map(fn {score, user_id} -> %{user: user_id, score: score} end)
  end
end

# =============================================================================
# Exercise 5: Cache pattern — get o compute
# =============================================================================
#
# El patrón más común de ETS en producción: cache con compute-on-miss.
# Si la clave existe en caché → retorna el valor cacheado.
# Si NO existe → ejecuta la función costosa, guarda el resultado, lo retorna.
#
# `new_cache/0`: crea una tabla cache fresh
# `get_or_compute/3`: recibe (table, key, compute_fn)
#   - Si key existe en table → retorna valor cacheado
#   - Si NO existe → llama compute_fn.(), guarda result, retorna result
# `invalidate/2`: elimina una entrada del caché
# `clear/1`: vacía toda la tabla sin destruirla
#
# Tip: :ets.delete_all_objects/1 para limpiar sin destruir

defmodule ETSCache do
  def new_cache do
    # TODO: :ets.new(:app_cache, [:set, :public])
  end

  def get_or_compute(table, key, compute_fn) do
    # TODO:
    # case :ets.lookup(table, key) do
    #   [{^key, cached_value}] ->
    #     cached_value
    #   [] ->
    #     value = compute_fn.()
    #     :ets.insert(table, {key, value})
    #     value
    # end
  end

  def invalidate(table, key) do
    # TODO: :ets.delete(table, key)
  end

  def clear(table) do
    # TODO: :ets.delete_all_objects(table)
  end
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule ETSTests do
  def run do
    IO.puts("\n=== Verificación: ETS Básico ===\n")

    # Ejercicio 1: Básico
    IO.puts("  Ejercicio 1 — Operaciones básicas:")
    table = ETSBasic.setup()
    ETSBasic.put(table, :user_1, %{name: "Alice"})
    ETSBasic.put(table, :user_2, %{name: "Bob"})

    check("get existente",       ETSBasic.get(table, :user_1),    %{name: "Alice"})
    check("get no existente",    ETSBasic.get(table, :missing),   :not_found)

    ETSBasic.remove(table, :user_1)
    check("get después de remove", ETSBasic.get(table, :user_1), :not_found)
    check("get otro tras remove",  ETSBasic.get(table, :user_2), %{name: "Bob"})

    :ets.delete(table)
    IO.puts("")

    # Ejercicio 2: Named table
    IO.puts("  Ejercicio 2 — Named table:")
    SessionStore.init()
    SessionStore.store_session("sess_abc", %{user: "Alice", expires: 9999})
    SessionStore.store_session("sess_xyz", %{user: "Bob", expires: 1111})

    result2 = SessionStore.get_session("sess_abc")
    check("get_session existente",    result2,                        %{user: "Alice", expires: 9999})
    check("get_session no existente", SessionStore.get_session("nope"), :not_found)

    SessionStore.invalidate("sess_abc")
    check("get tras invalidate",      SessionStore.get_session("sess_abc"), :not_found)
    check("otra sesión intacta",      SessionStore.get_session("sess_xyz"), %{user: "Bob", expires: 1111})

    IO.puts("")

    # Ejercicio 3: Bag table
    IO.puts("  Ejercicio 3 — Bag table:")
    ev_table = EventLog.setup()
    EventLog.add_event(ev_table, "user_1", :login)
    EventLog.add_event(ev_table, "user_1", :view_page)
    EventLog.add_event(ev_table, "user_1", :logout)
    EventLog.add_event(ev_table, "user_2", :login)

    check("count_events user_1", EventLog.count_events(ev_table, "user_1"), 3)
    check("count_events user_2", EventLog.count_events(ev_table, "user_2"), 1)

    events = EventLog.get_events(ev_table, "user_1")
    check("get_events retorna lista",  is_list(events), true)
    check("get_events tiene 3 items",  length(events), 3)
    check("evento tiene campo :type",  Map.has_key?(hd(events), :type), true)

    :ets.delete(ev_table)
    IO.puts("")

    # Ejercicio 4: Ordered set
    IO.puts("  Ejercicio 4 — Ordered set (Leaderboard):")
    lb = Leaderboard.setup()
    Leaderboard.insert_score(lb, "player_a", 1500)
    Leaderboard.insert_score(lb, "player_b", 3200)
    Leaderboard.insert_score(lb, "player_c", 800)
    Leaderboard.insert_score(lb, "player_d", 2700)

    top2 = Leaderboard.top_n(lb, 2)
    check("top_n retorna 2 elementos",    length(top2), 2)
    check("top_n primero tiene score",    Map.has_key?(hd(top2), :score), true)
    [first | _] = top2
    check("top 1 tiene mayor score", first.score >= 3200, true)

    :ets.delete(lb)
    IO.puts("")

    # Ejercicio 5: Cache pattern
    IO.puts("  Ejercicio 5 — Cache pattern:")
    cache = ETSCache.new_cache()
    call_count = :ets.new(:cc, [:set, :public])
    :ets.insert(call_count, {:n, 0})

    expensive_fn = fn ->
      [{:n, n}] = :ets.lookup(call_count, :n)
      :ets.insert(call_count, {:n, n + 1})
      "resultado_costoso"
    end

    r1 = ETSCache.get_or_compute(cache, :my_key, expensive_fn)
    r2 = ETSCache.get_or_compute(cache, :my_key, expensive_fn)
    r3 = ETSCache.get_or_compute(cache, :my_key, expensive_fn)

    [{:n, calls}] = :ets.lookup(call_count, :n)

    check("primer call retorna resultado",       r1, "resultado_costoso")
    check("segundo call retorna mismo resultado", r2, "resultado_costoso")
    check("compute_fn solo se llama 1 vez",      calls, 1)

    ETSCache.invalidate(cache, :my_key)
    ETSCache.get_or_compute(cache, :my_key, expensive_fn)
    [{:n, calls2}] = :ets.lookup(call_count, :n)
    check("tras invalidate, compute_fn vuelve a ejecutarse", calls2, 2)

    :ets.delete(cache)
    :ets.delete(call_count)

    IO.puts("\n=== Verificación completada ===")
  end

  defp check(label, actual, expected) do
    if actual == expected do
      IO.puts("  ✓ #{label}")
    else
      IO.puts("  ✗ #{label}")
      IO.puts("    Esperado: #{inspect(expected)}")
      IO.puts("    Obtenido: #{inspect(actual)}")
    end
  end
end

# =============================================================================
# Common Mistakes
# =============================================================================
#
# ERROR 1: :ets.lookup retorna siempre una LISTA (nunca el valor directo)
#
#   :ets.insert(t, {:key, "value"})
#   :ets.lookup(t, :key)   # => [{:key, "value"}]  ← lista, no "value"
#
#   Siempre desestructura: [{:key, value}] = :ets.lookup(t, :key)
#   O usa case para manejar el caso vacío.
#
# ERROR 2: Confundir :set con :bag al insertar duplicados
#
#   :ets.insert(:set_table, {:key, "primero"})
#   :ets.insert(:set_table, {:key, "segundo"})
#   :ets.lookup(:set_table, :key)
#   # => [{:key, "segundo"}]  ← el primero fue REEMPLAZADO (comportamiento :set)
#
#   Con :bag, ambos coexistirían.
#
# ERROR 3: Tabla destruida cuando el proceso dueño muere
#
#   spawn(fn -> :ets.new(:my_table, [:named_table, :public]) end)
#   Process.sleep(100)
#   :ets.lookup(:my_table, :key)  # ArgumentError: tabla ya no existe
#
#   Solución: crear la tabla en un proceso de larga vida (GenServer, supervisor).
#
# ERROR 4: :named_table no es idempotente sin protección
#
#   :ets.new(:my_table, [:named_table])   # OK primera vez
#   :ets.new(:my_table, [:named_table])   # ArgumentError: ya existe
#
#   Solución: usar try/rescue o verificar con :ets.whereis(:my_table).
#
# ERROR 5: :ets.delete/1 destruye la tabla; :ets.delete/2 borra una entrada
#
#   :ets.delete(table)         # elimina TODA la tabla
#   :ets.delete(table, :key)   # elimina solo la entrada con esa key

# =============================================================================
# Summary
# =============================================================================
#
# - ETS es una tabla en memoria compartida entre procesos de la BEAM
# - :set → una entrada por clave, :bag → múltiples entradas por clave
# - :named_table permite acceso por nombre de átomo
# - :ets.lookup siempre retorna una lista (puede ser vacía)
# - La tabla vive mientras vive el proceso dueño
# - El patrón cache (get_or_compute) es el uso más común en producción

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 14: Registry dinámico para lookup de procesos por nombre
# - Explorar: :ets.select/2 con match specifications para queries complejas
# - Explorar: :ets.update_counter/3 para contadores atómicos sin race conditions
# - Explorar: DETS (Disk-based ETS) para persistencia simple

# =============================================================================
# Resources
# =============================================================================
# - https://www.erlang.org/doc/man/ets.html
# - https://elixir-lang.org/getting-started/mix-otp/ets.html
# - Elixir in Action, Cap. 10 — Beyond GenServer

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Implementa un rate limiter usando ETS:
#
#   RateLimiter.check_rate(user_id, limit, window_ms)
#   # => :ok      — si el usuario está dentro del límite
#   # => :exceeded — si superó el número de requests permitidos
#
# La función debe:
# 1. Guardar en ETS los timestamps de las llamadas por user_id
#    Usa una tabla :bag para múltiples entradas por clave.
# 2. En cada llamada:
#    a. Obtener todos los timestamps del user_id
#    b. Filtrar solo los que están dentro de la ventana de tiempo
#    c. Si hay >= limit timestamps dentro de la ventana → :exceeded
#    d. Si hay < limit → insertar el timestamp actual y retornar :ok
# 3. Opcionalmente: limpiar entradas viejas para no crecer indefinidamente
#
# Tip: System.monotonic_time(:millisecond) para timestamps
#      :ets.select/2 con match specifications para filtrar por rango de tiempo

ETSTests.run()
