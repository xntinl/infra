# =============================================================================
# Ejercicio 16: ETS Avanzado — Concurrencia y Optimizaciones
# Difficulty: Avanzado
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - ETS básico: :ets.new/2, insert/2, lookup/2, delete/2
# - Procesos y concurrencia en Elixir
# - Task y Task.async_stream
# - Conceptos de contención de recursos (lock contention)

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Configurar ETS con :read_concurrency y :write_concurrency para cargas mixtas
# 2. Escribir match specs con :ets.fun2ms/1 para queries complejas
# 3. Usar select, select_delete, select_count para operaciones bulk
# 4. Implementar tablas ETS con heir para sobrevivir al crash del dueño
# 5. Diseñar un partitioned counter para eliminar contención en escrituras

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# ETS (Erlang Term Storage) es un store de datos en memoria nativo del runtime
# BEAM. A diferencia de los procesos, ETS es un recurso compartido que múltiples
# procesos pueden leer y escribir concurrentemente.
#
# OPCIONES DE CONCURRENCIA:
#
#   :read_concurrency — optimiza para muchos lectores simultáneos.
#   Reduce la contención en :ets.lookup. Tiene overhead en escritura.
#   Úsalo cuando lecturas >> escrituras.
#
#   :write_concurrency — permite escrituras concurrentes en distintos buckets.
#   Sin esta opción, hay un lock global por tabla. Con ella, hay locks por segmento.
#   Úsalo cuando hay muchas escrituras simultáneas.
#
#   :write_concurrency, :auto — desde OTP 25, ajusta automáticamente el número
#   de locks según la carga detectada.
#
# MATCH SPECS:
#
#   ETS no soporta SQL, pero tiene un mecanismo de queries poderoso: Match Specs.
#   Un match spec es una estructura de datos Erlang que describe un patrón,
#   una guardia, y un resultado.
#
#   La forma más legible de generarlas es con :ets.fun2ms/1:
#
#     match_spec = :ets.fun2ms(fn {_key, age, role} when age > 30 and role == :admin ->
#       true
#     end)
#
#   IMPORTANTE: :ets.fun2ms/1 debe recibir una función literal (no una variable).
#   Solo funciona en iex/scripts donde el parse transform de ms_transform está activo.
#   En módulos compilados, usar :ets.fun2ms en un atributo de módulo o construir
#   la match spec manualmente.
#
# HEIR (tabla huérfana):
#
#   Por defecto, cuando el proceso dueño de una tabla ETS muere, la tabla se
#   destruye. Con :heir, puedes designar un proceso sucesor que hereda la tabla:
#
#     :ets.new(:mi_tabla, [:named_table, :public, {:heir, pid_sucesor, :herencia_info}])
#
#   Cuando el dueño muere, el sucesor recibe:
#     {:ETS-TRANSFER, tabla, pid_dueño_fallido, :herencia_info}
#
# PARTITIONED ETS (Sharding):
#
#   El problema con un único contador global en ETS es la contención: aunque
#   :write_concurrency ayuda, sigue habiendo contención por el mismo key.
#
#   La solución: crear N tablas (una por scheduler BEAM), cada una con su propio
#   contador. Para leer el total, sumar todos los shards.
#
#     schedulers = System.schedulers_online()
#     tables = Enum.map(1..schedulers, fn i ->
#       :ets.new(:"shard_#{i}", [:public, :write_concurrency])
#     end)
#
#   Para escribir, seleccionar el shard según el scheduler actual:
#     shard_idx = rem(:erlang.system_info(:scheduler_id), schedulers) + 1

# =============================================================================
# Módulos de apoyo
# =============================================================================

defmodule ETSHelpers do
  @doc """
  Inicializa una tabla ETS con opciones de concurrencia.
  Retorna el table reference.
  """
  def new_concurrent_table(name, opts \\ []) do
    default_opts = [:named_table, :public, :read_concurrency, :write_concurrency]
    :ets.new(name, Keyword.merge(default_opts, opts))
  end

  @doc """
  Suma un campo numérico en posición `pos` para todos los registros de la tabla.
  Útil para agregar counters de múltiples shards.
  """
  def sum_field(table, pos) do
    :ets.foldl(fn tuple, acc -> acc + elem(tuple, pos) end, 0, table)
  end
end

# =============================================================================
# Exercise 1: Concurrent Cache con :read_concurrency
# =============================================================================
#
# Implementa un módulo `ConcurrentCache` que:
#
# 1. `start/0` — crea una tabla ETS llamada `:concurrent_cache` con opciones:
#    - :named_table, :public
#    - :read_concurrency — la carga será 95% lecturas, 5% escrituras
#    - type: :set (default)
#    Retorna {:ok, table_ref}
#
# 2. `put(key, value)` — inserta {key, value} en la tabla.
#
# 3. `get(key)` — busca la clave. Retorna {:ok, value} si existe, :miss si no.
#
# 4. `benchmark(n_readers)` — lanza `n_readers` Tasks que cada uno hace
#    10_000 lecturas de claves aleatorias del 1 al 100.
#    Retorna el tiempo total en ms (usar :timer.tc).
#    Pre-pobla la tabla con 100 entries antes de lanzar los Tasks.
#
# Trade-off a entender:
#   :read_concurrency usa un "read lock" especial que múltiples readers pueden
#   adquirir simultáneamente (shared lock). La escritura sigue siendo exclusiva.
#   En tablas con >90% lecturas, esto da una mejora de 2-4x vs el default.
#
# Hint: Task.async_stream/3 con max_concurrency: n_readers

# TODO: Implementa ConcurrentCache
# defmodule ConcurrentCache do
#   def start do
#     ...
#   end
#
#   def put(key, value) do
#     ...
#   end
#
#   def get(key) do
#     ...
#   end
#
#   def benchmark(n_readers) do
#     # Pre-poblar
#     Enum.each(1..100, fn i -> put(i, "value_#{i}") end)
#
#     {micros, _result} = :timer.tc(fn ->
#       1..n_readers
#       |> Task.async_stream(fn _ ->
#         Enum.each(1..10_000, fn _ ->
#           key = :rand.uniform(100)
#           get(key)
#         end)
#       end, max_concurrency: n_readers, timeout: 30_000)
#       |> Stream.run()
#     end)
#
#     div(micros, 1000)  # retornar en ms
#   end
# end

# =============================================================================
# Exercise 2: Match Spec Queries
# =============================================================================
#
# Implementa un módulo `UserStore` que gestione una tabla ETS de usuarios:
#
# Schema del registro: {user_id, name, age, role}
# Ejemplo: {1, "Alice", 35, :admin}
#
# 1. `start/0` — crea tabla ETS :user_store con [:named_table, :public]
#
# 2. `insert(user_id, name, age, role)` — inserta el registro.
#
# 3. `find_senior_admins()` — retorna todos los usuarios donde
#    `age > 30 AND role == :admin`.
#    DEBE usar :ets.select/2 con una match spec generada por :ets.fun2ms/1.
#    Retorna lista de tuplas {user_id, name, age, role}.
#
# 4. `count_by_role(role)` — cuenta cuántos usuarios tienen ese role.
#    Usar :ets.select_count/2 con match spec.
#    Retorna un integer.
#
# 5. `delete_inactive(role)` — elimina todos los registros con ese role.
#    Usar :ets.select_delete/2 con match spec.
#    Retorna el número de registros eliminados.
#
# Hint para fun2ms:
#   La función anónima debe recibir el tuple COMPLETO como argumento:
#
#   :ets.fun2ms(fn {_id, _name, age, role} when age > 30 and role == :admin ->
#     true  # para select_count/select_delete
#   end)
#
#   Para select que retorna registros, el resultado puede ser la tupla entera
#   o solo los campos que necesitas.
#
# Importante: :ets.fun2ms/1 solo acepta funciones literales (no variables).
# Si necesitas un argumento dinámico (como el role en count_by_role),
# construye la match spec manualmente:
#
#   # Match spec manual para contar por role:
#   # [{pattern, guards, result}]
#   # pattern: {:"$1", :"$2", :"$3", :"$4"}
#   # guards: [{:"==", :"$4", role}]   <- role es el valor concreto aquí
#   # result: [true]
#
#   [{
#     {:"$1", :"$2", :"$3", :"$4"},
#     [{:"==", :"$4", role}],
#     [true]
#   }]

# TODO: Implementa UserStore
# defmodule UserStore do
#   def start do
#     ...
#   end
#
#   def insert(user_id, name, age, role) do
#     ...
#   end
#
#   def find_senior_admins do
#     # Usa :ets.fun2ms/1 aquí
#     match_spec = :ets.fun2ms(fn {id, name, age, role}
#                                  when age > 30 and role == :admin ->
#       {id, name, age, role}
#     end)
#     :ets.select(:user_store, match_spec)
#   end
#
#   def count_by_role(role) do
#     # Match spec manual porque role es dinámico
#     match_spec = [{
#       {:"$1", :"$2", :"$3", :"$4"},
#       [{:"==", :"$4", role}],
#       [true]
#     }]
#     :ets.select_count(:user_store, match_spec)
#   end
#
#   def delete_inactive(role) do
#     ...
#   end
# end

# =============================================================================
# Exercise 3: Partitioned Counter
# =============================================================================
#
# Implementa un módulo `PartitionedCounter` que elimine contención en un
# contador global usando N tablas ETS (una por scheduler BEAM).
#
# 1. `start/0` — crea `System.schedulers_online()` tablas ETS, una por scheduler.
#    Cada tabla se llama :"counter_shard_N" (N = 1..schedulers).
#    Inicializa el contador en 0 en cada tabla con key :count.
#    Retorna {:ok, n_shards}
#
# 2. `increment/0` — incrementa atómicamente el contador en el shard
#    correspondiente al scheduler actual.
#    Usar: :erlang.system_info(:scheduler_id) para obtener el scheduler actual.
#    Usar: :ets.update_counter(table, :count, {2, 1}) para incremento atómico.
#    El shard se elige con: rem(scheduler_id - 1, n_shards)
#
# 3. `total/0` — suma los contadores de todos los shards.
#    Lee :ets.lookup(table, :count) de cada tabla y suma el campo.
#    Retorna el total como integer.
#
# 4. `benchmark_vs_single(n_increments)` — compara el rendimiento de:
#    a) Un único contador ETS global (sin sharding)
#    b) El PartitionedCounter
#    Lanza System.schedulers_online() Tasks, cada uno hace n_increments/n_tasks increments.
#    Retorna %{single_ms: _, partitioned_ms: _}
#
# Por qué funciona:
#   Cada scheduler BEAM tiene su propio thread del OS. Si todos incrementan
#   el mismo key en la misma tabla, hay contención aunque sea atómica.
#   Con un shard por scheduler, cada thread escribe en su propia tabla:
#   zero contention.
#
# Trade-off:
#   + Escrituras sin contención: escala linealmente con schedulers
#   - Lectura del total requiere sumar N tablas (más lento para reads frecuentes)
#   - Más memoria (N tablas en lugar de 1)
#   Ideal para métricas de alta frecuencia donde se lee el total raramente.

# TODO: Implementa PartitionedCounter
# defmodule PartitionedCounter do
#   @n_shards System.schedulers_online()
#
#   def start do
#     Enum.each(1..@n_shards, fn i ->
#       table = :"counter_shard_#{i}"
#       :ets.new(table, [:named_table, :public, :write_concurrency])
#       :ets.insert(table, {:count, 0})
#     end)
#     {:ok, @n_shards}
#   end
#
#   def increment do
#     scheduler_id = :erlang.system_info(:scheduler_id)
#     shard_idx = rem(scheduler_id - 1, @n_shards) + 1
#     table = :"counter_shard_#{shard_idx}"
#     :ets.update_counter(table, :count, {2, 1})
#   end
#
#   def total do
#     Enum.reduce(1..@n_shards, 0, fn i, acc ->
#       table = :"counter_shard_#{i}"
#       [{:count, val}] = :ets.lookup(table, :count)
#       acc + val
#     end)
#   end
#
#   def benchmark_vs_single(n_increments) do
#     ...
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule ETSConcurrentTests do
  def run do
    IO.puts("\n=== Verificación: ETS Avanzado — Concurrencia ===\n")

    test_concurrent_cache()
    test_user_store()
    test_partitioned_counter()

    IO.puts("\n=== Verificación completada ===")
  end

  defp test_concurrent_cache do
    IO.puts("--- Exercise 1: Concurrent Cache ---")

    # Evitar tabla ya existente de runs previos
    if :ets.whereis(:concurrent_cache) != :undefined do
      :ets.delete(:concurrent_cache)
    end

    {:ok, _table} = ConcurrentCache.start()

    ConcurrentCache.put(:user_1, %{name: "Alice"})
    ConcurrentCache.put(:user_2, %{name: "Bob"})

    check("get existente", ConcurrentCache.get(:user_1), {:ok, %{name: "Alice"}})
    check("get miss", ConcurrentCache.get(:unknown), :miss)

    ms = ConcurrentCache.benchmark(50)
    IO.puts("  [benchmark] 50 readers × 10k lecturas: #{ms}ms")
    check("benchmark completado", is_integer(ms) and ms > 0, true)
  end

  defp test_user_store do
    IO.puts("\n--- Exercise 2: Match Spec Queries ---")

    if :ets.whereis(:user_store) != :undefined do
      :ets.delete(:user_store)
    end

    UserStore.start()
    UserStore.insert(1, "Alice", 35, :admin)
    UserStore.insert(2, "Bob", 28, :admin)
    UserStore.insert(3, "Carol", 42, :admin)
    UserStore.insert(4, "Dave", 33, :viewer)
    UserStore.insert(5, "Eve", 25, :viewer)

    senior_admins = UserStore.find_senior_admins()
    ids = Enum.map(senior_admins, fn {id, _, _, _} -> id end) |> Enum.sort()
    check("senior admins (age>30 AND admin)", ids, [1, 3])

    check("count viewers", UserStore.count_by_role(:viewer), 2)
    check("count admins", UserStore.count_by_role(:admin), 3)

    deleted = UserStore.delete_inactive(:viewer)
    check("delete viewers", deleted, 2)
    check("count tras delete", UserStore.count_by_role(:viewer), 0)
  end

  defp test_partitioned_counter do
    IO.puts("\n--- Exercise 3: Partitioned Counter ---")

    # Limpiar shards previos
    n = System.schedulers_online()
    Enum.each(1..n, fn i ->
      table = :"counter_shard_#{i}"
      if :ets.whereis(table) != :undefined, do: :ets.delete(table)
    end)

    {:ok, n_shards} = PartitionedCounter.start()
    check("n_shards == schedulers", n_shards, System.schedulers_online())

    check("total inicial", PartitionedCounter.total(), 0)

    # 1000 increments concurrentes
    1..1000
    |> Task.async_stream(fn _ -> PartitionedCounter.increment() end,
       max_concurrency: 100, timeout: 10_000)
    |> Stream.run()

    check("total tras 1000 increments", PartitionedCounter.total(), 1000)

    result = PartitionedCounter.benchmark_vs_single(100_000)
    IO.puts("  [benchmark] single: #{result.single_ms}ms | partitioned: #{result.partitioned_ms}ms")
    check("benchmark retorna mapa con claves", Map.has_key?(result, :single_ms), true)
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
# ERROR 1: :ets.fun2ms/1 con una variable en lugar de función literal
#
#   f = fn {_id, _name, age, _role} when age > 30 -> true end
#   :ets.fun2ms(f)  # ← CRASH: ms_transform solo procesa funciones literales
#
#   Solución: pasar la función directamente como literal, nunca como variable.
#
# ERROR 2: Olvidar que update_counter requiere un registro previo
#
#   :ets.update_counter(:mi_tabla, :count, {2, 1})
#   # ← badarg si :count no existe aún en la tabla
#
#   Solución: insertar {:count, 0} antes de la primera llamada.
#
# ERROR 3: scheduler_id comienza en 1, no en 0
#
#   shard = rem(:erlang.system_info(:scheduler_id), n_shards)
#   # ← Cuando scheduler_id == n_shards, rem da 0, que no es un índice válido
#
#   Solución: rem(scheduler_id - 1, n_shards) + 1
#
# ERROR 4: :read_concurrency NO mejora escrituras
#
#   Si tu carga es 50% lecturas / 50% escrituras, :read_concurrency puede
#   EMPEORAR el rendimiento porque tiene overhead en el handshake de lock.
#   Solo úsalo cuando lecturas >> escrituras.
#
# ERROR 5: Olvidar que :named_table hace la tabla global por nombre
#
#   :ets.new(:mi_tabla, [:named_table]) llamado dos veces lanza:
#   ** (ArgumentError) argument error — la tabla ya existe
#
#   Solución: verificar con :ets.whereis(:mi_tabla) != :undefined antes de crear.

# =============================================================================
# One Possible Solution (sparse)
# =============================================================================
#
# ConcurrentCache.start/0:
#   :ets.new(:concurrent_cache, [:named_table, :public, :set,
#     :read_concurrency, :write_concurrency])
#
# ConcurrentCache.get/1:
#   case :ets.lookup(:concurrent_cache, key) do
#     [{^key, value}] -> {:ok, value}
#     [] -> :miss
#   end
#
# UserStore.find_senior_admins/0:
#   spec = :ets.fun2ms(fn {id, name, age, role}
#                          when age > 30 and role == :admin ->
#     {id, name, age, role}
#   end)
#   :ets.select(:user_store, spec)
#
# PartitionedCounter.increment/0:
#   shard_idx = rem(:erlang.system_info(:scheduler_id) - 1, @n_shards) + 1
#   :ets.update_counter(:"counter_shard_#{shard_idx}", :count, {2, 1})
#
# PartitionedCounter.total/0:
#   Enum.reduce(1..@n_shards, 0, fn i, acc ->
#     [{:count, v}] = :ets.lookup(:"counter_shard_#{i}", :count)
#     acc + v
#   end)

# =============================================================================
# Trade-offs: :read_concurrency vs :write_concurrency vs ambos
# =============================================================================
#
# | Configuración          | Lecturas    | Escrituras  | Uso recomendado        |
# |------------------------|-------------|-------------|------------------------|
# | ninguno (default)      | OK          | OK          | Baja concurrencia      |
# | :read_concurrency      | Excelente   | Peor        | Cache read-heavy       |
# | :write_concurrency     | OK          | Bueno       | Contadores, eventos    |
# | ambos                  | Excelente   | Bueno       | Mixto con alta carga   |
# | :write_concurrency :auto | OK        | Adaptativo  | OTP 25+, preferido     |

# =============================================================================
# Summary
# =============================================================================
#
# ETS es una herramienta fundamental para performance en BEAM:
#
# - :read_concurrency: shared locks para lectores — escala con cores
# - :write_concurrency: lock striping para escritores — reduce contención
# - Match specs: queries complejas sin sacar datos a Elixir (in-process en BEAM)
# - :ets.fun2ms/1: syntactic sugar legible para generar match specs
# - Heir: tablas que sobreviven al crash del dueño — fault tolerance
# - Partitioned ETS: sharding manual que elimina contención total

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 17: DETS — persistencia a disco
# - Ejercicio 19: Patrones de cache de producción con ETS (TTL, LRU, single-flight)
# - Ejercicio 20: :atomics y :counters como alternativas de alta performance

# =============================================================================
# Resources
# =============================================================================
# - https://www.erlang.org/doc/man/ets.html
# - https://www.erlang.org/doc/man/ets.html#fun2ms-1
# - https://www.erlang.org/doc/apps/stdlib/ms_transform.html
# - Erlang/OTP in Action — Cap. 7: ETS and Mnesia

ETSConcurrentTests.run()
