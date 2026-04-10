# =============================================================================
# Ejercicio 18: Mnesia — Base de Datos Distribuida del BEAM
# Difficulty: Avanzado
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - ETS básico (Ejercicio 16) y DETS (Ejercicio 17)
# - Conceptos de transacciones ACID
# - Nodos distribuidos de Erlang (Ejercicio 11)
# - Records de Erlang (Mnesia usa records, no mapas)

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Crear un schema y tabla Mnesia con tipos correctos
# 2. Ejecutar operaciones CRUD dentro de transacciones
# 3. Usar :mnesia.match_object y :mnesia.select para queries
# 4. Entender qué ofrece Mnesia que ETS/DETS no ofrecen
# 5. Saber cuándo Mnesia es la herramienta correcta (y cuándo no lo es)

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# MNESIA es la base de datos relacional distribuida integrada en OTP.
# Es la única base de datos con soporte nativo para clustering BEAM,
# transacciones distribuidas, y replicación automática entre nodos.
#
# CUÁNDO USAR CADA UNO:
#
#   ETS   → Caché en memoria. Alta velocidad. No persiste. Un nodo.
#   DETS  → Caché persistente. Un nodo. Datos simples. Hasta 2GB.
#   Mnesia → Estado crítico distribuido. Transacciones. Multi-nodo. Schema estricto.
#   Postgres/MySQL → Datos relacionales complejos. Volumen grande. SQL queries.
#
# RECORDS EN ERLANG/MNESIA:
#
#   Mnesia trabaja con Records de Erlang (equivalente a structs pero tuples).
#   En Elixir, los definimos con Record.defrecord:
#
#     require Record
#     Record.defrecord(:user, id: nil, name: nil, email: nil, role: :viewer)
#
#   Esto crea macros: user(), user(:id), user(id: 1), etc.
#   El primer elemento del record siempre es el nombre del record (el átomo).
#   Mnesia usa esto para saber a qué tabla pertenece cada registro.
#
# FLUJO DE SETUP:
#
#   1. :mnesia.create_schema([node()])    # crear schema en el nodo actual
#   2. :mnesia.start()                    # iniciar Mnesia
#   3. :mnesia.create_table(User, [...]) # crear tabla
#   4. :mnesia.transaction(fn -> ... end) # operar en transacción
#   5. :mnesia.stop()                     # detener (opcional)
#
# TIPOS DE TABLA:
#
#   :set          — una tupla por key. Key = primer campo después del nombre.
#   :bag          — múltiples tuples por key (para relaciones 1:N).
#   :ordered_set  — como :set pero ordenado. Útil para rangos y paginación.
#
# TRANSACCIONES:
#
#   :mnesia.transaction/1 ejecuta una función en una transacción atómica.
#   Si la función lanza una excepción, la transacción hace rollback.
#   Si el nodo crash a mitad de la transacción, Mnesia hace rollback al restart.
#
#   {:atomic, result} = :mnesia.transaction(fn ->
#     :mnesia.write({User, 1, "Alice", "alice@x.com", :admin})
#     :mnesia.read({User, 1})
#   end)
#
# QUERIES:
#
#   :mnesia.read({Table, Key})           # buscar por key exacto
#   :mnesia.match_object({Table, pattern}) # pattern matching simple
#   :mnesia.select(Table, MatchSpec)     # queries complejas (como ETS select)
#   :mnesia.index_read(Table, val, field) # requiere :mnesia.add_table_index/2
#
# DIRTY OPERATIONS (sin transacción, más rápido, menos seguro):
#
#   :mnesia.dirty_read({Table, Key})
#   :mnesia.dirty_write({Table, record})
#   :mnesia.dirty_delete({Table, Key})
#
#   Úsalas solo para datos no críticos donde velocidad > consistencia.

# =============================================================================
# Definición de Records (Mnesia los necesita)
# =============================================================================

require Record

# Record para la tabla de usuarios
Record.defrecord(:user_rec,
  id: nil,
  name: nil,
  email: nil,
  role: :viewer,
  age: 0
)

# Record para la tabla de posts
Record.defrecord(:post_rec,
  id: nil,
  user_id: nil,
  title: nil,
  content: nil,
  published: false
)

# =============================================================================
# Exercise 1: Basic CRUD con Transacciones
# =============================================================================
#
# Implementa `MnesiaCRUD` con operaciones completas sobre Mnesia.
#
# 1. `setup/0` — inicializa Mnesia:
#    a) Detener Mnesia si está corriendo: :mnesia.stop()
#    b) Crear schema: :mnesia.create_schema([node()])
#       Puede retornar {:error, {_, {already_exists, _}}} si ya existe — ignorar.
#    c) Iniciar: :mnesia.start()
#    d) Crear tabla :user_rec con:
#       - attributes: [:id, :name, :email, :role, :age]
#         (deben coincidir con los campos del Record, SIN el nombre del record)
#       - type: :set
#       - disc_copies: [node()]  (persistente en disco Y en memoria)
#    e) Esperar a que las tablas estén listas: :mnesia.wait_for_tables/2
#    Retorna :ok o {:error, reason}
#
# 2. `create_user(id, name, email, role, age)` — inserta un usuario en transacción.
#    Usa: :mnesia.write(user_rec(id: id, name: name, ...))
#    Nota: user_rec(...) es la macro que genera el tuple correcto.
#    Retorna :ok o {:error, reason}
#
# 3. `get_user(id)` — lee un usuario por id en transacción.
#    Usa: :mnesia.read({:user_rec, id})
#    Retorna {:ok, %{id, name, email, role, age}} (convertir a mapa) o :not_found
#
# 4. `update_user(id, attrs)` — actualiza campos de un usuario.
#    Patrón: read → merge → write (en una sola transacción).
#    `attrs` es un keyword list: [name: "New Name"] o [role: :admin]
#    Si el usuario no existe, retorna {:error, :not_found}
#    Retorna :ok
#
# 5. `delete_user(id)` — elimina un usuario.
#    Usa: :mnesia.delete({:user_rec, id})
#    Retorna :ok
#
# 6. `list_all_users/0` — retorna todos los usuarios como lista de mapas.
#    Usa: :mnesia.match_object(user_rec(id: :_, name: :_, email: :_, role: :_, age: :_))
#    El átomo :_ es el wildcard en Mnesia.
#
# Conversión Record → Mapa:
#   def record_to_map(rec) do
#     %{
#       id: user_rec(rec, :id),
#       name: user_rec(rec, :name),
#       email: user_rec(rec, :email),
#       role: user_rec(rec, :role),
#       age: user_rec(rec, :age)
#     }
#   end
#
# Cómo ejecutar transacciones y extraer resultado:
#   case :mnesia.transaction(fn -> :mnesia.write(...) end) do
#     {:atomic, result} -> {:ok, result}
#     {:aborted, reason} -> {:error, reason}
#   end

# TODO: Implementa MnesiaCRUD
# defmodule MnesiaCRUD do
#   require Record
#   import Record, only: [defrecord: 2]
#
#   def setup do
#     :mnesia.stop()
#     case :mnesia.create_schema([node()]) do
#       :ok -> :ok
#       {:error, {_, {already_exists, _}}} -> :ok
#       {:error, reason} -> raise "Schema creation failed: #{inspect(reason)}"
#     end
#     :mnesia.start()
#
#     case :mnesia.create_table(:user_rec, [
#       {:attributes, [:id, :name, :email, :role, :age]},
#       {:type, :set},
#       {:disc_copies, [node()]}
#     ]) do
#       {:atomic, :ok} -> :ok
#       {:aborted, {:already_exists, :user_rec}} -> :ok
#       {:aborted, reason} -> {:error, reason}
#     end
#
#     :mnesia.wait_for_tables([:user_rec], 5_000)
#   end
#
#   def create_user(id, name, email, role, age) do
#     record = user_rec(id: id, name: name, email: email, role: role, age: age)
#     case :mnesia.transaction(fn -> :mnesia.write(record) end) do
#       {:atomic, :ok} -> :ok
#       {:aborted, reason} -> {:error, reason}
#     end
#   end
#
#   def get_user(id) do
#     case :mnesia.transaction(fn -> :mnesia.read({:user_rec, id}) end) do
#       {:atomic, [rec]} -> {:ok, record_to_map(rec)}
#       {:atomic, []} -> :not_found
#       {:aborted, reason} -> {:error, reason}
#     end
#   end
#
#   def update_user(id, attrs) do
#     ...
#   end
#
#   def delete_user(id) do
#     ...
#   end
#
#   def list_all_users do
#     pattern = user_rec(id: :_, name: :_, email: :_, role: :_, age: :_)
#     {:atomic, records} = :mnesia.transaction(fn ->
#       :mnesia.match_object(pattern)
#     end)
#     Enum.map(records, &record_to_map/1)
#   end
#
#   defp record_to_map(rec) do
#     %{
#       id: user_rec(rec, :id),
#       name: user_rec(rec, :name),
#       email: user_rec(rec, :email),
#       role: user_rec(rec, :role),
#       age: user_rec(rec, :age)
#     }
#   end
# end

# =============================================================================
# Exercise 2: Queries con match_object y select
# =============================================================================
#
# Implementa `MnesiaQueries` con queries complejas sobre la tabla :user_rec.
# Asume que MnesiaCRUD.setup/0 ya fue llamado.
#
# 1. `find_by_role(role)` — retorna todos los usuarios con ese role.
#    Usar :mnesia.match_object/1 con :_ en los campos que no importan.
#    El campo del role debe ser el valor concreto (no :_).
#    Retorna lista de mapas.
#
# 2. `find_senior_by_role(role, min_age)` — usuarios donde role == X AND age >= min_age.
#    Usar :mnesia.select/2 con match spec:
#
#    match_spec = [
#      {user_rec(id: :"$1", name: :"$2", email: :"$3", role: role, age: :"$5"),
#       [{:>=, :"$5", min_age}],
#       [:"$$"]}  # :"$$" retorna todas las variables como lista
#    ]
#
#    Retorna lista de records (no mapas — el caller decide cómo convertir).
#
# 3. `count_by_role/0` — retorna un mapa %{role => count} para todos los roles.
#    Obtén todos los usuarios con list_all_users/0 (o match_object)
#    y agrupa con Enum.group_by/2 + Enum.map para contar.
#    Retorna mapa: %{admin: 2, viewer: 3, moderator: 1}
#
# 4. `transaction_demo/0` — demuestra atomicidad:
#    Crea una transacción que:
#    a) Inserta user con id: 9999
#    b) Lanza una excepción (throw :rollback_test)
#    Verifica que el usuario 9999 NO existe después del throw.
#    Retorna {:ok, :rolled_back} si la transacción rollbackeó correctamente.
#
# Nota sobre :"$1", :"$2" etc:
#   Son variables de match spec. :"$1" captura el primer campo,
#   :"$2" el segundo, etc. Se usan en las guardas [{:>=, :"$5", min_age}].
#   :"$$" retorna todas las variables como lista [val1, val2, ...].
#   :"$_" retorna el record completo (útil para select que retorna records).

# TODO: Implementa MnesiaQueries
# defmodule MnesiaQueries do
#   require Record
#
#   def find_by_role(role) do
#     pattern = user_rec(id: :_, name: :_, email: :_, role: role, age: :_)
#     {:atomic, records} = :mnesia.transaction(fn ->
#       :mnesia.match_object(pattern)
#     end)
#     # Convertir a mapas
#     Enum.map(records, fn rec ->
#       %{
#         id: user_rec(rec, :id),
#         name: user_rec(rec, :name),
#         role: user_rec(rec, :role),
#         age: user_rec(rec, :age)
#       }
#     end)
#   end
#
#   def find_senior_by_role(role, min_age) do
#     match_spec = [
#       {user_rec(id: :"$1", name: :"$2", email: :"$3", role: role, age: :"$5"),
#        [{:>=, :"$5", min_age}],
#        [:"$_"]}
#     ]
#     {:atomic, records} = :mnesia.transaction(fn ->
#       :mnesia.select(:user_rec, match_spec)
#     end)
#     records
#   end
#
#   def count_by_role do
#     ...
#   end
#
#   def transaction_demo do
#     ...
#   end
# end

# =============================================================================
# Exercise 3: Relación entre Tablas (Posts y Users)
# =============================================================================
#
# Implementa `MnesiaRelations` que maneja dos tablas relacionadas:
# - :user_rec (del Exercise 1)
# - :post_rec (posts con user_id como foreign key)
#
# 1. `setup_posts_table/0` — crea la tabla :post_rec:
#    attributes: [:id, :user_id, :title, :content, :published]
#    type: :set
#    disc_copies: [node()]
#    Retorna :ok
#
# 2. `create_post(id, user_id, title, content)` — inserta un post.
#    PRIMERO verifica que el user_id existe (en la misma transacción).
#    Si el usuario no existe, hace abort de la transacción:
#      :mnesia.abort({:error, :user_not_found})
#    Retorna :ok o {:error, :user_not_found}
#
# 3. `get_user_posts(user_id)` — retorna todos los posts de un usuario.
#    Usar match_object con user_id concreto y :_ en el resto.
#    Retorna lista de mapas: [%{id, title, content, published}]
#
# 4. `publish_post(post_id)` — actualiza published: true en el post.
#    Patrón read-modify-write en transacción.
#    Retorna :ok o {:error, :not_found}
#
# 5. `delete_user_cascade(user_id)` — elimina usuario Y todos sus posts.
#    En una sola transacción:
#    a) Lee todos los posts del usuario (match_object)
#    b) Borra cada post (:mnesia.delete/1)
#    c) Borra el usuario
#    Retorna {:ok, n_posts_deleted}
#
# Por qué esto importa:
#   Mnesia garantiza que el delete_user_cascade sea atómico.
#   Si crash a mitad del cascade, el rollback restaura el estado original.
#   Con ETS/DETS no tenemos esta garantía — el cascade sería eventual.

# TODO: Implementa MnesiaRelations
# defmodule MnesiaRelations do
#   require Record
#
#   def setup_posts_table do
#     case :mnesia.create_table(:post_rec, [
#       {:attributes, [:id, :user_id, :title, :content, :published]},
#       {:type, :set},
#       {:disc_copies, [node()]}
#     ]) do
#       {:atomic, :ok} -> :ok
#       {:aborted, {:already_exists, :post_rec}} -> :ok
#       {:aborted, reason} -> {:error, reason}
#     end
#   end
#
#   def create_post(id, user_id, title, content) do
#     case :mnesia.transaction(fn ->
#       # Verificar que el usuario existe
#       case :mnesia.read({:user_rec, user_id}) do
#         [] -> :mnesia.abort({:error, :user_not_found})
#         [_user] ->
#           record = post_rec(id: id, user_id: user_id, title: title,
#                             content: content, published: false)
#           :mnesia.write(record)
#       end
#     end) do
#       {:atomic, :ok} -> :ok
#       {:aborted, {:error, :user_not_found}} -> {:error, :user_not_found}
#       {:aborted, reason} -> {:error, reason}
#     end
#   end
#
#   def get_user_posts(user_id) do
#     ...
#   end
#
#   def publish_post(post_id) do
#     ...
#   end
#
#   def delete_user_cascade(user_id) do
#     ...
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule MnesiaTests do
  def run do
    IO.puts("\n=== Verificación: Mnesia — Base de Datos Distribuida ===\n")

    # Setup global de Mnesia
    IO.puts("Inicializando Mnesia...")
    :ok = MnesiaCRUD.setup()
    IO.puts("Mnesia lista.\n")

    test_crud()
    test_queries()
    test_relations()

    :mnesia.stop()
    IO.puts("\n=== Verificación completada ===")
  end

  defp test_crud do
    IO.puts("--- Exercise 1: CRUD con Transacciones ---")

    # Limpiar datos de runs previos
    :mnesia.transaction(fn ->
      Enum.each([1, 2, 3, 4, 5], fn id ->
        :mnesia.delete({:user_rec, id})
      end)
    end)

    :ok = MnesiaCRUD.create_user(1, "Alice", "alice@x.com", :admin, 35)
    :ok = MnesiaCRUD.create_user(2, "Bob", "bob@x.com", :viewer, 28)
    :ok = MnesiaCRUD.create_user(3, "Carol", "carol@x.com", :admin, 42)

    check("get existente", match?({:ok, %{name: "Alice"}}, MnesiaCRUD.get_user(1)), true)
    check("get no existente", MnesiaCRUD.get_user(999), :not_found)

    :ok = MnesiaCRUD.update_user(2, name: "Robert", role: :moderator)
    {:ok, bob} = MnesiaCRUD.get_user(2)
    check("update name", bob.name, "Robert")
    check("update role", bob.role, :moderator)

    :ok = MnesiaCRUD.delete_user(3)
    check("delete user", MnesiaCRUD.get_user(3), :not_found)

    all = MnesiaCRUD.list_all_users()
    check("list_all count", length(all), 2)
  end

  defp test_queries do
    IO.puts("\n--- Exercise 2: Queries ---")

    # Agregar más usuarios para queries
    :ok = MnesiaCRUD.create_user(4, "Dave", "dave@x.com", :admin, 31)
    :ok = MnesiaCRUD.create_user(5, "Eve", "eve@x.com", :viewer, 45)

    admins = MnesiaQueries.find_by_role(:admin)
    admin_names = Enum.map(admins, & &1.name) |> Enum.sort()
    check("find_by_role admins", admin_names, ["Alice", "Dave"])

    senior = MnesiaQueries.find_senior_by_role(:admin, 33)
    check("senior admins age >= 33", length(senior), 1)

    counts = MnesiaQueries.count_by_role()
    check("count admins", counts[:admin], 2)
    check("count viewers", counts[:viewer], 1)

    check("transaction rollback", MnesiaQueries.transaction_demo(), {:ok, :rolled_back})
    check("user 9999 no existe", MnesiaCRUD.get_user(9999), :not_found)
  end

  defp test_relations do
    IO.puts("\n--- Exercise 3: Relaciones entre Tablas ---")

    :ok = MnesiaRelations.setup_posts_table()

    # Limpiar posts de runs previos
    :mnesia.transaction(fn ->
      Enum.each([101, 102, 103, 104], fn id ->
        :mnesia.delete({:post_rec, id})
      end)
    end)

    :ok = MnesiaRelations.create_post(101, 1, "Intro to Elixir", "Content...", )
    :ok = MnesiaRelations.create_post(102, 1, "Advanced ETS", "Content...")
    check("create post usuario válido", match?(:ok, MnesiaRelations.create_post(103, 1, "Mnesia", "Content...")), true)
    check("create post usuario inválido", MnesiaRelations.create_post(104, 999, "Hack", "x"), {:error, :user_not_found})

    posts = MnesiaRelations.get_user_posts(1)
    check("get_user_posts count", length(posts), 3)

    :ok = MnesiaRelations.publish_post(101)
    posts_after = MnesiaRelations.get_user_posts(1)
    published = Enum.find(posts_after, fn p -> p.id == 101 end)
    check("publish_post", published.published, true)

    {:ok, n_deleted} = MnesiaRelations.delete_user_cascade(1)
    check("cascade n posts deleted", n_deleted, 3)
    check("cascade user deleted", MnesiaCRUD.get_user(1), :not_found)
    check("cascade posts deleted", MnesiaRelations.get_user_posts(1), [])
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
# ERROR 1: No hacer create_schema antes de start
#
#   :mnesia.start()  # sin create_schema → trabaja solo en RAM (no disc_copies)
#   :mnesia.create_table(:tabla, [{:disc_copies, [node()]}])
#   # Funciona pero los datos no persisten — disc_copies requiere schema en disco
#
#   Solución: siempre create_schema → start → create_table.
#
# ERROR 2: Confundir el nombre del record con el nombre de la tabla
#
#   Record.defrecord(:user_rec, ...)  # el nombre es :user_rec
#   :mnesia.create_table(:User, ...)  # ← INCORRECTO — nombre diferente
#
#   Mnesia infiere el nombre de la tabla del primer elemento del tuple (el record).
#   El nombre del record DEBE coincidir con el nombre de la tabla.
#
# ERROR 3: Usar :mnesia.read/1 en lugar de :mnesia.read/2
#
#   :mnesia.read(:user_rec, id)           # ← INCORRECTO (2 args separados)
#   :mnesia.read({:user_rec, id})         # ← CORRECTO (tuple)
#
# ERROR 4: Olvidar que :mnesia.abort/1 es la forma de hacer rollback
#
#   En una transacción, throw/raise NO hace rollback automáticamente en todos los casos.
#   Para rollback explícito: :mnesia.abort(reason)
#   Mnesia lo captura y retorna {:aborted, reason}.
#
# ERROR 5: Wildcard :_ vs :"_"
#
#   En match specs de Mnesia, el wildcard es el átomo :_ (underscore).
#   No confundir con el string "_" ni con la variable _ en pattern matching.
#   :mnesia.match_object(user_rec(id: :_, name: "Alice", ...))
#            # ↑ :_ = "cualquier valor para id"

# =============================================================================
# One Possible Solution (sparse)
# =============================================================================
#
# MnesiaCRUD.update_user/2:
#   :mnesia.transaction(fn ->
#     case :mnesia.read({:user_rec, id}) do
#       [] -> :mnesia.abort({:error, :not_found})
#       [rec] ->
#         updated = Enum.reduce(attrs, rec, fn
#           {:name, v}, r -> user_rec(r, name: v)
#           {:role, v}, r -> user_rec(r, role: v)
#           {:age, v}, r -> user_rec(r, age: v)
#           _, r -> r
#         end)
#         :mnesia.write(updated)
#     end
#   end)
#
# MnesiaRelations.delete_user_cascade/1:
#   :mnesia.transaction(fn ->
#     posts = :mnesia.match_object(
#       post_rec(id: :_, user_id: user_id, title: :_, content: :_, published: :_))
#     Enum.each(posts, fn p -> :mnesia.delete({:post_rec, post_rec(p, :id)}) end)
#     :mnesia.delete({:user_rec, user_id})
#     length(posts)
#   end)

# =============================================================================
# Trade-offs: ETS vs DETS vs Mnesia
# =============================================================================
#
# | Criterio             | ETS        | DETS        | Mnesia            |
# |----------------------|------------|-------------|-------------------|
# | Velocidad lectura    | ns         | ms          | μs (in-memory)    |
# | Velocidad escritura  | ns         | ms (fsync)  | μs + overhead tx  |
# | Persistencia         | No         | Sí          | Sí (disc_copies)  |
# | Distribución         | No         | No          | Sí (multi-nodo)   |
# | Transacciones ACID   | No         | Parcial     | Sí                |
# | Tamaño máximo        | RAM libre  | 2 GB        | RAM + disco       |
# | Schema               | Libre      | Libre       | Estricto (attrs)  |
# | Setup complejidad    | Mínima     | Mínima      | Media-Alta        |

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 19: Patrones de cache avanzados con ETS
# - Ejercicio 20: :atomics y :counters para métricas de alta frecuencia
# - Explorar: Mnesia replicación entre nodos (requiere cluster del Ejercicio 11)

# =============================================================================
# Resources
# =============================================================================
# - https://www.erlang.org/doc/apps/mnesia/mnesia_chap1.html
# - https://hexdocs.pm/elixir/Record.html
# - Erlang/OTP in Action — Cap. 8: Introducing Mnesia
# - Learn You Some Erlang — Mnesia chapter

MnesiaTests.run()
