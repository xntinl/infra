# =============================================================================
# Ejercicio 17: DETS — Almacenamiento Persistente en Disco
# Difficulty: Avanzado
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - ETS básico (Ejercicio 16)
# - Procesos, spawn, send/receive
# - File system: File.exists?/1, File.rm/1
# - Conceptos de durabilidad y consistencia en storage

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Abrir, usar y cerrar correctamente una tabla DETS
# 2. Implementar una cache persistente que sobrevive reinicios de nodo
# 3. Simular y recuperar de un crash de DETS
# 4. Implementar el patrón write-through ETS+DETS (hot cache + durable storage)
# 5. Entender las limitaciones de DETS vs ETS vs base de datos

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# DETS (Disk Erlang Term Storage) es la versión persistente de ETS.
# Los datos se guardan en un archivo binario en disco y sobreviven reinicios.
#
# API BÁSICA:
#
#   :dets.open_file(:mi_tabla, [{:file, ~c"path/a/archivo.dets"}, {:type, :set}])
#   :dets.insert(:mi_tabla, {key, value})
#   :dets.lookup(:mi_tabla, key)   # retorna lista de tuplas: [{key, value}]
#   :dets.delete(:mi_tabla, key)
#   :dets.close(:mi_tabla)
#
# TIPOS SOPORTADOS:
#
#   :set      — un registro por key (default). Como Map.
#   :bag      — múltiples registros por key (duplicados permitidos). Como lista.
#   :duplicate_bag — como bag pero permite duplicados exactos.
#
# DETS vs ETS:
#
#   | Característica    | ETS              | DETS                     |
#   |-------------------|------------------|--------------------------|
#   | Velocidad         | ~ns              | ~ms (disk I/O)           |
#   | Persistencia      | No               | Sí (survives restarts)   |
#   | Tamaño máximo     | RAM disponible   | 2GB por archivo          |
#   | Transacciones     | No               | Básicas (sync writes)    |
#   | Concurrencia      | Alta (locks ETS) | Baja (un proceso a vez)  |
#
# DETS CORRUPTION:
#
#   DETS usa un formato binario propio. Si el proceso muere mientras escribe,
#   el archivo puede quedar en estado inconsistente (dirty).
#
#   Al reabrir un archivo corrupto, DETS intenta recuperar automáticamente.
#   Si falla, retorna {:error, {:need_repair, path}}.
#
#   Forzar reparación:
#     :dets.open_file(:mi_tabla, [{:file, path}, {:repair, true}])
#
#   Reparación forzada puede tardar si el archivo es grande (escanea todo).
#
# COMPACTION:
#
#   DETS no reclama espacio inmediatamente al borrar registros.
#   El espacio libre se reutiliza pero el archivo no se achica.
#   Para reducir el tamaño en disco: abrir, crear tabla nueva, copiar, reemplazar.
#
# PATRÓN ETS+DETS (Write-Through):
#
#   El patrón más común en producción:
#   1. Al startup: cargar DETS → ETS (warm-up)
#   2. En cada lectura: buscar en ETS (rápido)
#   3. En cada escritura: escribir en ETS Y DETS simultáneamente (write-through)
#
#   Garantiza que ETS y DETS están siempre sincronizados.
#   Si el nodo reinicia, el warm-up restaura ETS desde DETS.

# =============================================================================
# Exercise 1: Persistent Cache
# =============================================================================
#
# Implementa un módulo `PersistentCache` que almacene datos en DETS.
# Los datos deben sobrevivir si el proceso que los escribió termina.
#
# 1. `open(path)` — abre (o crea si no existe) una tabla DETS en el path dado.
#    Retorna :ok o {:error, reason}.
#    Nombre de la tabla: :persistent_cache
#    Tipo: :set
#    Hint: el path en DETS debe ser una charlist (~c"path"), no un String binario.
#    Usar: String.to_charlist(path) o ~c"path"
#
# 2. `put(key, value)` — inserta {key, value} en DETS.
#    Retorna :ok o {:error, reason}.
#
# 3. `get(key)` — busca la clave.
#    Retorna {:ok, value} si existe, :miss si no.
#
# 4. `close()` — cierra la tabla DETS correctamente.
#    CRÍTICO: siempre cerrar antes de que el proceso termine, o el archivo
#    quedará en estado "dirty" y necesitará reparación.
#
# 5. `simulate_persistence(path)` — función de demostración:
#    a) Abre DETS en path
#    b) Escribe 5 entradas
#    c) Cierra DETS
#    d) Reabre DETS (simula restart)
#    e) Lee las 5 entradas y verifica que están presentes
#    f) Retorna {:ok, n_recovered} donde n_recovered es cuántas entradas se leyeron bien
#
# Trade-off clave:
#   DETS usa sync writes por defecto: cada :dets.insert/2 fuerza un fsync.
#   Esto garantiza durabilidad pero es LENTO (~1-5ms por write vs ~ns en ETS).
#   Para escrituras masivas, usar :dets.insert_new/2 en batch o deshabilitar
#   sync con {:sync, false} (riesgo de pérdida de datos en crash).

# TODO: Implementa PersistentCache
# defmodule PersistentCache do
#   @table :persistent_cache
#
#   def open(path) do
#     case :dets.open_file(@table, [
#       {:file, String.to_charlist(path)},
#       {:type, :set}
#     ]) do
#       {:ok, @table} -> :ok
#       {:error, reason} -> {:error, reason}
#     end
#   end
#
#   def put(key, value) do
#     ...
#   end
#
#   def get(key) do
#     case :dets.lookup(@table, key) do
#       [{^key, value}] -> {:ok, value}
#       [] -> :miss
#     end
#   end
#
#   def close do
#     :dets.close(@table)
#   end
#
#   def simulate_persistence(path) do
#     open(path)
#     Enum.each(1..5, fn i -> put(i, "valor_#{i}") end)
#     close()
#
#     # Simular restart: reabrir
#     open(path)
#     n_recovered = Enum.count(1..5, fn i ->
#       get(i) == {:ok, "valor_#{i}"}
#     end)
#     close()
#
#     {:ok, n_recovered}
#   end
# end

# =============================================================================
# Exercise 2: Recovery tras Crash
# =============================================================================
#
# Implementa `DETSRecovery` que demuestra cómo manejar un archivo DETS
# que podría estar en estado inconsistente.
#
# 1. `open_with_repair(path)` — abre DETS con recuperación automática.
#    Intenta abrir normalmente. Si falla con :need_repair o :badarg,
#    intenta de nuevo con {:repair, true}.
#    Retorna {:ok, :opened_clean} | {:ok, :repaired} | {:error, reason}
#
# 2. `safe_write_batch(entries)` — escribe una lista de {key, value} con
#    verificación post-write. Para cada entry, verifica que se puede leer
#    inmediatamente después de escribir.
#    Retorna {:ok, n_written} donde n_written es cuántas se verificaron.
#
# 3. `simulate_dirty_close(path)` — demuestra qué pasa con un cierre brusco:
#    a) Abre DETS
#    b) Escribe datos
#    c) USA spawn para crear un proceso hijo que abre la MISMA tabla
#    d) El proceso hijo muere de inmediato (sin cerrar)
#    e) Intenta reabrir con open_with_repair/1
#    f) Retorna {:recovered, n_entries} o {:error, reason}
#
# Nota sobre la simulación:
#   En DETS, un proceso puede "olvidar" cerrar la tabla (ej: crash, Process.exit).
#   DETS detecta esto al reabrir: el flag "dirty" en el header del archivo.
#   Con {:repair, true}, DETS escanea el archivo completo y reconstruye los índices.
#   En archivos grandes, esto puede tardar minutos.
#
# Strategy de recovery en producción:
#   1. Siempre usar {:repair, true} al abrir — overhead mínimo si el archivo está clean
#   2. Monitorear el tiempo de open: si tarda >1s, hubo reparación → alertar
#   3. Para datos críticos, considerar backups regulares con :dets.to_ets y dumpear

# TODO: Implementa DETSRecovery
# defmodule DETSRecovery do
#   @table :dets_recovery
#
#   def open_with_repair(path) do
#     charpath = String.to_charlist(path)
#     case :dets.open_file(@table, [{:file, charpath}, {:type, :set}]) do
#       {:ok, @table} ->
#         {:ok, :opened_clean}
#       {:error, {:needs_repair, _}} ->
#         case :dets.open_file(@table, [{:file, charpath}, {:repair, true}]) do
#           {:ok, @table} -> {:ok, :repaired}
#           {:error, reason} -> {:error, reason}
#         end
#       {:error, reason} ->
#         {:error, reason}
#     end
#   end
#
#   def safe_write_batch(entries) do
#     n_written = Enum.count(entries, fn {key, value} ->
#       :ok == :dets.insert(@table, {key, value}) and
#         :dets.lookup(@table, key) == [{key, value}]
#     end)
#     {:ok, n_written}
#   end
#
#   def simulate_dirty_close(path) do
#     ...
#   end
# end

# =============================================================================
# Exercise 3: DETS + ETS Combo (Write-Through Cache)
# =============================================================================
#
# Implementa `WriteThroughCache` que mantiene ETS como capa rápida y
# DETS como capa durable. Ambas siempre en sync.
#
# 1. `start(dets_path)` — inicializa:
#    a) Crea tabla ETS :wt_ets con [:named_table, :public, :read_concurrency]
#    b) Abre DETS en dets_path con nombre :wt_dets
#    c) Warm-up: copia TODOS los registros de DETS a ETS
#       (usar :dets.foldl/3 o :dets.to_list/1)
#    Retorna {:ok, n_loaded} donde n_loaded es cuántos registros se cargaron a ETS.
#
# 2. `put(key, value)` — write-through:
#    a) Escribe en ETS primero (rápido, en memoria)
#    b) Escribe en DETS después (lento, persistente)
#    Si DETS falla, hace rollback en ETS (delete el key) y retorna {:error, reason}.
#    Retorna :ok o {:error, reason}.
#
# 3. `get(key)` — solo lee de ETS (rápido).
#    Si hay un miss en ETS, NO va a DETS (DETS siempre debe estar en sync con ETS).
#    Retorna {:ok, value} | :miss
#
# 4. `delete(key)` — elimina de ambas capas.
#    Retorna :ok.
#
# 5. `stop()` — cierra DETS correctamente.
#    NUNCA llames :ets.delete aquí — ETS se destruye con el proceso dueño.
#
# 6. `stats()` — retorna %{ets_size: n, dets_size: n, in_sync: bool}
#    ETS: :ets.info(:wt_ets, :size)
#    DETS: :dets.info(:wt_dets, :size)
#    in_sync: true si ambos tamaños son iguales (aproximación, no verificación exacta)
#
# Por qué warm-up importa:
#   Sin warm-up, después de un restart, ETS empieza vacía.
#   Los primeros reads son todos misses aunque los datos estén en DETS.
#   El warm-up garantiza que ETS está lista desde el primer request.
#   En producción, el warm-up ocurre en GenServer.init/1 y puede tardar.
#   Para tablas grandes, considera lazy loading (leer de DETS en cache miss).

# TODO: Implementa WriteThroughCache
# defmodule WriteThroughCache do
#   @ets_table :wt_ets
#   @dets_table :wt_dets
#
#   def start(dets_path) do
#     # Crear ETS
#     :ets.new(@ets_table, [:named_table, :public, :set, :read_concurrency])
#
#     # Abrir DETS
#     :dets.open_file(@dets_table, [
#       {:file, String.to_charlist(dets_path)},
#       {:type, :set}
#     ])
#
#     # Warm-up: DETS -> ETS
#     n_loaded = :dets.foldl(fn record, acc ->
#       :ets.insert(@ets_table, record)
#       acc + 1
#     end, 0, @dets_table)
#
#     {:ok, n_loaded}
#   end
#
#   def put(key, value) do
#     :ets.insert(@ets_table, {key, value})
#     case :dets.insert(@dets_table, {key, value}) do
#       :ok -> :ok
#       {:error, reason} ->
#         :ets.delete(@ets_table, key)
#         {:error, reason}
#     end
#   end
#
#   def get(key) do
#     case :ets.lookup(@ets_table, key) do
#       [{^key, value}] -> {:ok, value}
#       [] -> :miss
#     end
#   end
#
#   def delete(key) do
#     :ets.delete(@ets_table, key)
#     :dets.delete(@dets_table, key)
#     :ok
#   end
#
#   def stop do
#     :dets.close(@dets_table)
#   end
#
#   def stats do
#     ets_size = :ets.info(@ets_table, :size)
#     dets_size = :dets.info(@dets_table, :size)
#     %{ets_size: ets_size, dets_size: dets_size, in_sync: ets_size == dets_size}
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule DETSTests do
  @tmp_dir System.tmp_dir!()

  def run do
    IO.puts("\n=== Verificación: DETS — Almacenamiento Persistente ===\n")

    test_persistent_cache()
    test_recovery()
    test_write_through()

    IO.puts("\n=== Verificación completada ===")
  end

  defp persistent_path, do: Path.join(@tmp_dir, "test_persistent_cache.dets")
  defp recovery_path, do: Path.join(@tmp_dir, "test_dets_recovery.dets")
  defp wt_path, do: Path.join(@tmp_dir, "test_write_through.dets")

  defp test_persistent_cache do
    IO.puts("--- Exercise 1: Persistent Cache ---")
    path = persistent_path()
    File.rm(path)

    :ok = PersistentCache.open(path)
    :ok = PersistentCache.put(:session_1, %{user: "alice", token: "abc123"})
    :ok = PersistentCache.put(:session_2, %{user: "bob", token: "xyz789"})
    check("put y get basic", PersistentCache.get(:session_1), {:ok, %{user: "alice", token: "abc123"}})
    check("miss key", PersistentCache.get(:unknown), :miss)
    PersistentCache.close()

    # Simular restart
    {:ok, n} = PersistentCache.simulate_persistence(path)
    check("persistencia tras restart", n, 5)

    PersistentCache.close()
    File.rm(path)
  end

  defp test_recovery do
    IO.puts("\n--- Exercise 2: Recovery tras Crash ---")
    path = recovery_path()
    File.rm(path)

    # Abrir limpio
    {:ok, status} = DETSRecovery.open_with_repair(path)
    check("open limpio", status, :opened_clean)

    entries = Enum.map(1..10, fn i -> {i, "data_#{i}"} end)
    {:ok, n} = DETSRecovery.safe_write_batch(entries)
    check("batch write verificado", n, 10)

    :dets.close(:dets_recovery)
    File.rm(path)
  end

  defp test_write_through do
    IO.puts("\n--- Exercise 3: Write-Through ETS+DETS ---")

    # Limpiar estado previo
    if :ets.whereis(:wt_ets) != :undefined, do: :ets.delete(:wt_ets)
    :dets.close(:wt_dets)
    path = wt_path()
    File.rm(path)

    # Primera sesión: escribir datos
    {:ok, 0} = WriteThroughCache.start(path)
    :ok = WriteThroughCache.put(:product_1, %{name: "Widget", price: 29.99})
    :ok = WriteThroughCache.put(:product_2, %{name: "Gadget", price: 49.99})
    :ok = WriteThroughCache.put(:product_3, %{name: "Doohickey", price: 9.99})
    check("get desde ETS", WriteThroughCache.get(:product_1), {:ok, %{name: "Widget", price: 29.99}})

    stats = WriteThroughCache.stats()
    check("stats in_sync", stats.in_sync, true)
    check("stats ets_size", stats.ets_size, 3)

    :ok = WriteThroughCache.delete(:product_2)
    check("delete consistente", WriteThroughCache.get(:product_2), :miss)
    WriteThroughCache.stop()

    # Segunda sesión: verificar warm-up
    if :ets.whereis(:wt_ets) != :undefined, do: :ets.delete(:wt_ets)
    {:ok, n_loaded} = WriteThroughCache.start(path)
    check("warm-up desde DETS", n_loaded, 2)  # 3 insertados - 1 eliminado
    check("dato persiste tras restart", WriteThroughCache.get(:product_1), {:ok, %{name: "Widget", price: 29.99}})
    WriteThroughCache.stop()

    File.rm(path)
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
# ERROR 1: Pasar String binario como path en lugar de charlist
#
#   :dets.open_file(:tabla, [{:file, "/tmp/data.dets"}])  # ← String binario
#   # ** (ArgumentError) bad argument
#
#   Solución: String.to_charlist("/tmp/data.dets") o ~c"/tmp/data.dets"
#
# ERROR 2: No cerrar DETS antes de terminar el proceso
#
#   Si el proceso dueño termina sin :dets.close/1, el archivo queda "dirty".
#   La próxima apertura detectará el flag y hará repair automático.
#   En archivos grandes: repair puede tardar minutos.
#   Siempre usar try/after o GenServer.terminate/2 para garantizar el close.
#
# ERROR 3: Asumir que DETS es transaccional como una DB relacional
#
#   DETS no tiene ACID completo. Múltiples inserts no son atómicos entre sí.
#   Si el proceso crash después del 3er insert de 10, los primeros 3 persisten.
#   Para atomicidad, usar :mnesia (Ejercicio 18) o una base de datos externa.
#
# ERROR 4: Olvidar que el mismo nombre de tabla DETS persiste entre aperturas
#
#   Si usas :dets.open_file(:mi_tabla, ...) dos veces sin cerrar, la segunda
#   llamada retorna {:ok, :mi_tabla} (re-usa la apertura existente).
#   Esto puede causar confusión si cambias el path entre runs.
#
# ERROR 5: Confundir :dets.lookup/2 retorna lista, no valor directo
#
#   :dets.lookup(:tabla, :key)  # retorna [{:key, value}] o []
#   # NO retorna {:ok, value} directamente
#   # Debes hacer pattern matching sobre la lista

# =============================================================================
# One Possible Solution (sparse)
# =============================================================================
#
# PersistentCache.open/1:
#   :dets.open_file(:persistent_cache, [
#     {:file, String.to_charlist(path)},
#     {:type, :set}
#   ])
#   |> case do
#     {:ok, :persistent_cache} -> :ok
#     {:error, r} -> {:error, r}
#   end
#
# WriteThroughCache.start/1 warm-up:
#   n_loaded = :dets.foldl(fn record, acc ->
#     :ets.insert(:wt_ets, record)
#     acc + 1
#   end, 0, :wt_dets)
#
# WriteThroughCache.put/2 con rollback:
#   :ets.insert(:wt_ets, {key, value})
#   case :dets.insert(:wt_dets, {key, value}) do
#     :ok -> :ok
#     {:error, r} ->
#       :ets.delete(:wt_ets, key)
#       {:error, r}
#   end

# =============================================================================
# Summary
# =============================================================================
#
# DETS completa el trio de storage nativo en BEAM:
#
# - ETS: velocidad máxima, en memoria, no persiste
# - DETS: persistencia en disco, mismo API que ETS, lento para escrituras
# - Mnesia: distribuido, transaccional, más complejo (Ejercicio 18)
#
# El patrón ETS+DETS write-through es el estándar para:
# - Caches que deben sobrevivir reinicios
# - Configuración o estado que cambia raramente
# - Datos de referencia que son costosos de regenerar

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 18: Mnesia — base de datos distribuida del BEAM
# - Ejercicio 19: Patrones de cache avanzados con ETS

# =============================================================================
# Resources
# =============================================================================
# - https://www.erlang.org/doc/man/dets.html
# - https://learnyousomeerlang.com/ets — sección DETS
# - Programming Erlang 2nd Ed — Cap. 16: Interfacing Techniques

DETSTests.run()
