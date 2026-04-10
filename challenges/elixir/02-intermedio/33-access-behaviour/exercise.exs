# =============================================================================
# Ejercicio 33: Access Behaviour — Indexación Custom de Estructuras
# Nivel: Intermedio
# =============================================================================
#
# El behaviour Access define cómo una estructura puede ser accedida e
# indexada usando las funciones del kernel: get_in, put_in, update_in, etc.
# Implementarlo permite que tus tipos se integren con el ecosistema de
# navegación de datos anidados de Elixir.
#
# Conceptos clave:
#   - Access.fetch/2 — obtener un valor por clave
#   - Access.get_and_update/3 — leer y modificar atómicamente
#   - Access.pop/2 — eliminar y retornar un valor
#   - Uso con get_in, put_in, update_in, pop_in
#
# Para correr: elixir exercise.exs
# =============================================================================

# =============================================================================
# SECCIÓN 1: Access.fetch/2 — la función más importante
# =============================================================================
#
# fetch/2 es el corazón del behaviour Access.
# Recibe (container, key) y retorna:
#   {:ok, value}   — si la clave existe
#   :error         — si la clave no existe
#
# Todas las demás funciones de Access pueden derivarse de fetch.
# get_in/2, Kernel.get_in/2 usan fetch internamente.

IO.puts("=== Sección 1: Access.fetch ===\n")

defmodule PathMap do
  @moduledoc """
  Un mapa que acepta tanto atoms como strings como claves,
  normalizando internamente a atoms.
  """

  defstruct data: %{}

  def new(pairs \\ []) do
    data = Map.new(pairs, fn {k, v} -> {normalize_key(k), v} end)
    %__MODULE__{data: data}
  end

  def put(%__MODULE__{data: data} = pm, key, value) do
    %{pm | data: Map.put(data, normalize_key(key), value)}
  end

  defp normalize_key(key) when is_atom(key),   do: key
  defp normalize_key(key) when is_binary(key), do: String.to_existing_atom(key)

  # TODO 1: Implementa el behaviour Access para PathMap
  #   fetch/2 debe:
  #     - Normalizar la key (atom o string → atom)
  #     - Buscar en data con Map.fetch/2
  #     - Retornar {:ok, value} o :error
  #
  #   get_and_update/3 recibe (map, key, fun) donde fun es fn val -> {get, update} end
  #   pop/2 elimina la clave y retorna {valor_eliminado, map_sin_clave}
  #
  # Pista:
  #   @behaviour Access
  #
  #   @impl Access
  #   def fetch(%PathMap{data: data}, key) do
  #     Map.fetch(data, normalize_key(key))
  #   end
  #
  #   @impl Access
  #   def get_and_update(%PathMap{data: data} = pm, key, fun) do
  #     atom_key = normalize_key(key)
  #     current = Map.get(data, atom_key)
  #     case fun.(current) do
  #       {get, update} -> {get, %{pm | data: Map.put(data, atom_key, update)}}
  #       :pop          -> {current, %{pm | data: Map.delete(data, atom_key)}}
  #     end
  #   end
  #
  #   @impl Access
  #   def pop(%PathMap{data: data} = pm, key) do
  #     atom_key = normalize_key(key)
  #     {Map.get(data, atom_key), %{pm | data: Map.delete(data, atom_key)}}
  #   end
  #
  # Tu código aquí dentro del defmodule PathMap (agrega las funciones):
end

# Nota: En un archivo real agregarías las funciones dentro del defmodule.
# Para este ejercicio, define un PathMap2 con Access implementado:

defmodule PathMap2 do
  defstruct data: %{}

  def new(pairs \\ []) do
    data = Map.new(pairs, fn {k, v} -> {to_atom(k), v} end)
    %__MODULE__{data: data}
  end

  def put(%__MODULE__{data: data} = pm, key, value),
    do: %{pm | data: Map.put(data, to_atom(key), value)}

  defp to_atom(k) when is_atom(k),   do: k
  defp to_atom(k) when is_binary(k), do: String.to_atom(k)

  @behaviour Access

  # TODO 1: Implementa fetch, get_and_update, pop
  # Tu código aquí:

  # --- FIN TODO 1 ---
end

pm = PathMap2.new([{:name, "Alice"}, {:age, 30}, {:city, "CDMX"}])

IO.puts("get con atom key: #{pm[:name]}")
IO.puts("get con string key: #{pm["age"]}")
IO.puts("Access.fetch: #{inspect(Access.fetch(pm, :city))}")
IO.puts("Access.fetch missing: #{inspect(Access.fetch(pm, :missing))}")

# get_in con PathMap2 anidado
nested = PathMap2.new([
  {:user, PathMap2.new([{:name, "Bob"}, {:role, :admin}])},
  {:settings, PathMap2.new([{:theme, :dark}, {:lang, :es}])}
])

IO.puts("\nget_in anidado: #{get_in(nested, [:user, :name])}")
IO.puts("get_in: #{get_in(nested, [:settings, :theme])}\n")

# =============================================================================
# SECCIÓN 2: get_and_update — lectura y modificación atómica
# =============================================================================
#
# get_and_update/3 permite leer y modificar en una sola operación.
# La función callback recibe el valor actual y retorna {get_value, new_value}.
# Esto es lo que usa update_in internamente.

IO.puts("=== Sección 2: get_and_update ===\n")

# TODO 2: Usando el PathMap2 implementado arriba, demuestra:
#
#   A) update_in con PathMap2 anidado:
#      Actualiza el nombre del usuario de "Bob" a "Bobby"
#      usando update_in(nested, [:user, :name], &String.upcase/1)
#
#   B) Access.get_and_update/3 directamente:
#      {viejo_valor, nuevo_pm} = Access.get_and_update(pm, :age, fn age -> {age, age + 1} end)
#      Muestra el viejo y nuevo valor
#
#   C) pop_in con PathMap2:
#      {valor, nuevo_pm} = pop_in(pm, [:city])
#      Muestra lo que se eliminó y el PathMap resultante
#
# Tu código aquí:

# --- FIN TODO 2 ---

# =============================================================================
# SECCIÓN 3: Acceso anidado con get_in/put_in
# =============================================================================
#
# get_in/2 navega estructuras anidadas usando una lista de claves.
# Cada clave puede ser: atom (para maps/structs), integer (para listas),
# o cualquier cosa que implemente Access.fetch/2.

IO.puts("=== Sección 3: Acceso Anidado ===\n")

# Datos de ejemplo: un config anidado
config = %{
  database: %{
    host: "localhost",
    port: 5432,
    pool: %{size: 10, timeout: 5000}
  },
  cache: %{
    host: "redis.local",
    port: 6379,
    ttl: 3600
  }
}

# Access built-in con maps
IO.puts("DB host: #{get_in(config, [:database, :host])}")
IO.puts("Pool size: #{get_in(config, [:database, :pool, :size])}")

# update_in
updated_config = update_in(config, [:database, :pool, :size], &(&1 * 2))
IO.puts("Pool size updated: #{get_in(updated_config, [:database, :pool, :size])}")

# put_in
new_config = put_in(config, [:cache, :ttl], 7200)
IO.puts("Cache TTL updated: #{get_in(new_config, [:cache, :ttl])}\n")

# TODO 3: Usando PathMap2, crea una configuración de aplicación anidada
#   y demuestra get_in/put_in/update_in con tus propias keys atom/string.
#
#   Crea un app_config con secciones: :api, :auth, :limits
#   Cada sección es un PathMap2 con sus propios keys.
#   Luego:
#     1. Lee app_config[:api][:base_url] con get_in
#     2. Actualiza app_config[:limits][:rate] con update_in
#     3. Agrega app_config[:api][:version] con put_in
#
# Tu código aquí:

# --- FIN TODO 3 ---

# =============================================================================
# SECCIÓN 4: Access.key, Access.at, Access.filter
# =============================================================================
#
# Elixir provee Access helpers para construir paths dinámicos:
#   Access.key(:field)    — accede a un campo de struct/map (con fallback)
#   Access.at(0)          — accede al elemento N de una lista
#   Access.filter(pred)   — filtra elementos de lista

IO.puts("=== Sección 4: Access Helpers ===\n")

users = [
  %{name: "Alice", age: 30, role: :admin},
  %{name: "Bob",   age: 25, role: :user},
  %{name: "Carol", age: 35, role: :admin},
  %{name: "Dave",  age: 28, role: :user}
]

# Access.at/1 — elemento por índice
IO.puts("Primer usuario: #{inspect(get_in(users, [Access.at(0)]))}")
IO.puts("Segundo usuario: #{inspect(get_in(users, [Access.at(1)]))}")

# Nombre del primer usuario
IO.puts("Nombre del primero: #{get_in(users, [Access.at(0), :name])}")

# Access.filter/1 — todos los que cumplan el predicado
admins = get_in(users, [Access.filter(&(&1.role == :admin))])
IO.puts("Admins: #{inspect(admins)}")

# update_in con filter
promoted = update_in(users, [Access.filter(&(&1.role == :user)), :role], fn _ -> :moderator end)
IO.puts("Después de promoción: #{inspect(Enum.map(promoted, & &1.role))}")

# Access.key con fallback
config_map = %{timeout: 5000}
IO.puts("Con key existente: #{get_in(config_map, [Access.key(:timeout, 3000)])}")
IO.puts("Con key ausente (fallback): #{get_in(config_map, [Access.key(:retries, 3)])}\n")

# TODO 4: Crea una lista de PathMap2 y usa Access.filter para:
#   1. Filtrar los que tienen un campo :active == true
#   2. Actualizar todos los filtrados con un nuevo valor
#   Muestra los resultados antes y después.
#
# Tu código aquí:

# --- FIN TODO 4 ---

# =============================================================================
# SECCIÓN 5: Dynamic key — atom o string como key
# =============================================================================
#
# Un caso común es cuando la key puede venir como atom o string
# (p.ej., parámetros de formulario o JSON). Access permite manejar
# esto elegantemente con una función custom.

IO.puts("=== Sección 5: Dynamic Key Access ===\n")

# TODO 5: Implementa FlexibleMap, un mapa que acepta atom O string como key.
#   La clave se guarda internamente como string.
#   Access.fetch busca primero como string, luego como atom (convertido a string).
#
#   Ejemplo:
#     fm = FlexibleMap.new(%{"name" => "Alice", "age" => 30})
#     fm[:name]     → "Alice"
#     fm["name"]    → "Alice"
#     fm["missing"] → nil
#     get_in(fm, ["nested", "key"]) → nil (no error)
#
# Tu código aquí:

defmodule FlexibleMap do
  defstruct data: %{}

  def new(map \\ %{}) do
    data = Map.new(map, fn {k, v} ->
      string_key = if is_atom(k), do: Atom.to_string(k), else: k
      {string_key, v}
    end)
    %__MODULE__{data: data}
  end

  def put(%__MODULE__{data: data} = fm, key, value) do
    string_key = if is_atom(key), do: Atom.to_string(key), else: key
    %{fm | data: Map.put(data, string_key, value)}
  end

  @behaviour Access

  # Tu implementación de fetch, get_and_update, pop:

  # --- FIN TODO 5 ---
end

fm = FlexibleMap.new(%{"name" => "Alice", "score" => 95, "active" => true})

IO.puts("Atom key: #{fm[:name]}")
IO.puts("String key: #{fm["name"]}")
IO.puts("Score: #{fm["score"]}")
IO.puts("Missing: #{inspect(fm["missing"])}")
IO.puts("Fetch existing: #{inspect(Access.fetch(fm, :score))}")
IO.puts("Fetch missing: #{inspect(Access.fetch(fm, "unknown"))}\n")

# =============================================================================
# SECCIÓN 6: Implementar Access para una Struct de Configuración
# =============================================================================
#
# Un patrón común: Config struct con defaults y acceso amigable.
# El comportamiento puede retornar defaults cuando una clave no existe.

IO.puts("=== Sección 6: Config con Access ===\n")

defmodule AppConfig do
  defstruct [
    :env,
    :debug,
    :log_level,
    :db_url,
    :secret_key
  ]

  @defaults %{
    env: :dev,
    debug: false,
    log_level: :info,
    db_url: "postgres://localhost/myapp",
    secret_key: "change_me_in_production"
  }

  def new(overrides \\ %{}) do
    fields = Map.merge(@defaults, overrides)
    struct(__MODULE__, fields)
  end

  @behaviour Access

  @impl Access
  def fetch(config, key) do
    case Map.fetch(Map.from_struct(config), key) do
      {:ok, nil} -> Map.fetch(@defaults, key)
      result     -> result
    end
  end

  @impl Access
  def get_and_update(config, key, fun) do
    current = config[key]
    case fun.(current) do
      {get, update} -> {get, Map.put(config, key, update)}
      :pop          -> {current, Map.put(config, key, nil)}
    end
  end

  @impl Access
  def pop(config, key) do
    {config[key], Map.put(config, key, nil)}
  end
end

cfg = AppConfig.new(%{env: :prod, debug: true})
IO.puts("Env: #{cfg[:env]}")
IO.puts("Debug: #{cfg[:debug]}")
IO.puts("Log level: #{cfg[:log_level]}")

updated_cfg = update_in(cfg, [:log_level], fn _ -> :warn end)
IO.puts("Updated log level: #{updated_cfg[:log_level]}\n")

# =============================================================================
# SECCIÓN 7: pop_in — eliminar valores anidados
# =============================================================================

IO.puts("=== Sección 7: pop_in ===\n")

settings = %{
  user: %{name: "Eve", role: :admin, temp_token: "abc123"},
  prefs: %{theme: :dark, lang: :es}
}

# Eliminar campo sensible antes de serializar
{token, clean_settings} = pop_in(settings, [:user, :temp_token])
IO.puts("Token eliminado: #{token}")
IO.puts("Settings limpios: #{inspect(clean_settings)}")
IO.puts("")

# =============================================================================
# SECCIÓN 8: Access en listas con at/1 y all/0
# =============================================================================

IO.puts("=== Sección 8: Access en Listas ===\n")

matrix = [[1, 2, 3], [4, 5, 6], [7, 8, 9]]

IO.puts("Fila 0: #{inspect(get_in(matrix, [Access.at(0)]))}")
IO.puts("Elemento [1][2]: #{get_in(matrix, [Access.at(1), Access.at(2)])}")

# Actualizar un elemento específico
updated_matrix = update_in(matrix, [Access.at(1), Access.at(1)], &(&1 * 100))
IO.puts("Matrix con [1][1]*100: #{inspect(updated_matrix)}")

# Access.all() aplica la operación a TODOS los elementos
doubled_first_col = update_in(matrix, [Access.all(), Access.at(0)], &(&1 * 2))
IO.puts("Primera columna duplicada: #{inspect(doubled_first_col)}\n")

# =============================================================================
# SECCIÓN 9: TRY IT YOURSELF
# =============================================================================
#
# Implementa Access para un árbol binario.
#
# El árbol se representa como:
#   %BinaryTree{value: 5, left: %BinaryTree{...}, right: %BinaryTree{...}}
#   O nil para nodos vacíos.
#
# El acceso por path usa :left, :right y :value como keys:
#   tree[:value]                     → 5
#   tree[:left][:value]              → 3
#   tree[:right][:right][:value]     → 9
#   get_in(tree, [:left, :value])    → 3
#   get_in(tree, [:right, :left, :value]) → 7
#
# put_in y update_in deben funcionar para modificar nodos:
#   update_in(tree, [:left, :value], &(&1 * 2)) → nodo izquierdo con value*2
#
# Implementa también:
#   BinaryTree.new/1         → crea hoja con valor
#   BinaryTree.insert/2      → inserta valor (BST)
#   BinaryTree.to_list/1     → inorder traversal como lista
#
# Árbol de prueba:
#       5
#      / \
#     3   7
#    / \ / \
#   2  4 6  9

IO.puts("=== SECCIÓN 9: Try It Yourself ===\n")
IO.puts("Implementa BinaryTree con Access abajo:\n")

defmodule BinaryTree do
  defstruct [:value, :left, :right]

  def new(value), do: %__MODULE__{value: value, left: nil, right: nil}

  def insert(nil, value),  do: new(value)
  def insert(%__MODULE__{value: v, left: l} = tree, value) when value < v,
    do: %{tree | left: insert(l, value)}
  def insert(%__MODULE__{value: v, right: r} = tree, value) when value > v,
    do: %{tree | right: insert(r, value)}
  def insert(tree, _value), do: tree  # duplicado, ignorar

  def to_list(nil), do: []
  def to_list(%__MODULE__{value: v, left: l, right: r}),
    do: to_list(l) ++ [v] ++ to_list(r)

  @behaviour Access

  # Tu implementación de fetch, get_and_update, pop:
  # Recuerda: las keys son :value, :left, :right

  # --- FIN de tu implementación ---
end

tree =
  BinaryTree.new(5)
  |> BinaryTree.insert(3)
  |> BinaryTree.insert(7)
  |> BinaryTree.insert(2)
  |> BinaryTree.insert(4)
  |> BinaryTree.insert(6)
  |> BinaryTree.insert(9)

IO.puts("--- Tests de BinaryTree ---")
IO.puts("Valor raíz: #{tree[:value]}")
IO.puts("Valor izquierdo: #{tree[:left][:value]}")
IO.puts("Valor derecho: #{tree[:right][:value]}")
IO.puts("get_in deep: #{get_in(tree, [:right, :right, :value])}")
IO.puts("Inorder: #{inspect(BinaryTree.to_list(tree))}")

# Modificar un nodo
updated_tree = update_in(tree, [:left, :value], &(&1 * 10))
IO.puts("Izquierda multiplicada: #{updated_tree[:left][:value]}")

# =============================================================================
# ERRORES COMUNES
# =============================================================================
IO.puts("\n=== Errores Comunes ===\n")
IO.puts("""
1. No implementar los 3 callbacks: fetch, get_and_update, pop son TODOS requeridos.
   El compilador dará warning si falta alguno.

2. get_and_update debe manejar :pop:
   La función callback puede retornar {get, update} O :pop.
   Si retorna :pop, se debe eliminar la clave (como hace pop/2).
   Olvidar este caso causa un FunctionClauseError.

3. Confundir Access.fetch con Map.get:
   Access.fetch retorna {:ok, val} o :error (nunca nil).
   Map.get retorna val o default (nil por defecto).
   get_in usa Access.fetch internamente.

4. No retornar el struct actualizado en get_and_update:
   Debe retornar {get_value, updated_container}, no solo el valor.

5. Usar get_in con structs sin Access implementado:
   Los structs son mapas internamente, pero get_in con listas de claves
   requiere el behaviour Access. Para structs sin Access, usa Map.get/3.
""")
