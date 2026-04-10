# =============================================================================
# Ejercicio 09: Pattern Matching Avanzado
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Pattern matching básico (= operator, case/cond)
# - Funciones multi-cláusula básicas
# - Structs y Maps
# - Listas y operador cons [head | tail]

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Usar guards (when) para refinar el matching por valor o tipo
# 2. Ordenar correctamente las cláusulas de más a menos específica
# 3. Aplicar el pin operator (^) para comparar contra variables existentes
# 4. Hacer destructuring complejo en Maps, Structs y tuplas anidadas
# 5. Combinar guards con múltiples condiciones (and, or)

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# GUARDS (when):
#
#   def my_fn(x) when is_integer(x) and x > 0 do ... end
#
# Funciones permitidas en guards (no todas las funciones son válidas):
#   - Comparación: ==, !=, <, >, <=, >=
#   - Tipo: is_integer/1, is_float/1, is_binary/1, is_atom/1,
#           is_list/1, is_map/1, is_nil/1, is_boolean/1, is_pid/1
#   - Aritmética: +, -, *, div/2, rem/2, abs/1
#   - Acceso: map_size/1, length/1, tuple_size/1, byte_size/1
#   - Lógica: and, or, not, in (para listas literales)
#   - Rangos: x in 1..10
#
# Nota: Las funciones propias NO son válidas en guards (se evalúan
# en tiempo de compilación, no en runtime).
#
# ORDENAMIENTO DE CLÁUSULAS:
# Elixir evalúa las cláusulas en orden de definición. La PRIMERA que
# matchea gana. Regla: más específica → más general.
#
#   def process([]),      do: :empty          # más específica
#   def process([_]),     do: :one_element    # menos específica
#   def process([_|_]),   do: :many           # aún menos específica
#
# PIN OPERATOR (^):
# Sin pin: la variable se reasigna (binding)
#   x = 5; {x, y} = {10, 20}  → x = 10 (rebind)
#
# Con pin: la variable se usa como valor fijo para comparar
#   x = 5; {^x, y} = {10, 20}  → MatchError! (10 ≠ 5)
#   x = 5; {^x, y} = {5, 20}   → y = 20 (OK)
#
# DESTRUCTURING EN MAPS:
#   %{key: value} = my_map   # solo necesita que :key exista
#   %{a: a, b: b} = my_map   # extrae múltiples keys
#
# DESTRUCTURING EN STRUCTS:
#   %User{name: name} = user          # extrae name
#   %User{role: :admin} = user        # verifica que role == :admin

# -----------------------------------------------------------------------------
# Structs de apoyo
# -----------------------------------------------------------------------------

defmodule User do
  defstruct [:name, :email, role: :viewer]
end

# =============================================================================
# Exercise 1: Guards numéricos — clasificar un número
# =============================================================================
#
# Define el módulo NumberClassifier con la función `classify/1`.
# Debe retornar:
#   :negative  — cuando n < 0
#   :zero      — cuando n == 0
#   :small     — cuando 1 <= n <= 9
#   :large     — cuando n >= 10
#
# Usa múltiples cláusulas con guards `when`.
# IMPORTANTE: El orden importa. Define primero los casos más específicos.
#
# Ejemplo:
#   NumberClassifier.classify(-5)  # => :negative
#   NumberClassifier.classify(0)   # => :zero
#   NumberClassifier.classify(7)   # => :small
#   NumberClassifier.classify(100) # => :large

defmodule NumberClassifier do
  # TODO: Implementa classify/1 con 4 cláusulas y guards when
  #
  # def classify(n) when ... do
  #   ...
  # end
  #
  # def classify(n) when ... do
  #   ...
  # end
  # ...
end

# =============================================================================
# Exercise 2: List matching — procesar listas de distintos tamaños
# =============================================================================
#
# Define el módulo ListProcessor con la función `process/1`.
# Debe retornar:
#   {:empty}                          — para lista vacía []
#   {:single, elem}                   — para lista de un elemento
#   {:pair, first, second}            — para lista de exactamente dos elementos
#   {:many, first, rest}              — para lista de 3 o más elementos
#                                       donde rest es la cola (lista)
#
# IMPORTANTE: Ordena las cláusulas de más específica a más general.
#
# Ejemplo:
#   ListProcessor.process([])          # => {:empty}
#   ListProcessor.process([42])        # => {:single, 42}
#   ListProcessor.process([:a, :b])    # => {:pair, :a, :b}
#   ListProcessor.process([1, 2, 3])   # => {:many, 1, [2, 3]}

defmodule ListProcessor do
  # TODO: Implementa process/1 con 4 cláusulas
  #
  # def process([]) do
  #   ...
  # end
  #
  # def process([single]) do
  #   ...
  # end
  #
  # ...
end

# =============================================================================
# Exercise 3: Map guard — manejar roles de usuario
# =============================================================================
#
# Define el módulo AccessControl con la función `handle/1`.
# Recibe un mapa con al menos la clave :role.
# Debe retornar:
#   :full_access    — si role es :admin o :moderator  (usa `in`)
#   :read_only      — si role es :viewer
#   :unknown_role   — para cualquier otro rol
#
# Tip: usa `role in [:admin, :moderator]` en el guard.
#
# Ejemplo:
#   AccessControl.handle(%{role: :admin})      # => :full_access
#   AccessControl.handle(%{role: :moderator})  # => :full_access
#   AccessControl.handle(%{role: :viewer})     # => :read_only
#   AccessControl.handle(%{role: :guest})      # => :unknown_role

defmodule AccessControl do
  # TODO: Implementa handle/1 con 3 cláusulas
  #
  # def handle(%{role: role}) when role in [...] do
  #   ...
  # end
  #
  # ...
end

# =============================================================================
# Exercise 4: Pin operator — verificar valores esperados
# =============================================================================
#
# Define el módulo PinDemo con la función `verify_response/2`.
# Recibe `expected_status` (un átomo) y `response` (una tupla).
#
# La función debe:
# - Si response es {:ok, ^expected_status, _body}, retornar {:match, "Estado coincide"}
# - Si response es {:ok, other_status, _body},    retornar {:mismatch, other_status}
# - Si response es {:error, reason},               retornar {:error, reason}
#
# El pin operator asegura que el status de la respuesta sea EXACTAMENTE
# el esperado, no un rebind de la variable.
#
# Ejemplo:
#   PinDemo.verify_response(:created, {:ok, :created, "body"})
#   # => {:match, "Estado coincide"}
#
#   PinDemo.verify_response(:created, {:ok, :ok, "body"})
#   # => {:mismatch, :ok}
#
#   PinDemo.verify_response(:created, {:error, :timeout})
#   # => {:error, :timeout}

defmodule PinDemo do
  # TODO: Implementa verify_response/2 usando el pin operator (^)
  #
  # def verify_response(expected_status, response) do
  #   case response do
  #     {:ok, ^expected_status, _body} -> ...
  #     {:ok, other_status, _body}     -> ...
  #     {:error, reason}               -> ...
  #   end
  # end
end

# =============================================================================
# Exercise 5: Nested destructuring — extraer datos de estructuras complejas
# =============================================================================
#
# Define el módulo DataExtractor con la función `extract/1`.
# Recibe tuplas anidadas y extrae información según el patrón.
#
# Cláusulas (en orden):
#   1. {:ok, %User{name: name, role: :admin}} → {:admin_found, name}
#   2. {:ok, %User{name: name, role: role}}   → {:user_found, name, role}
#   3. {:error, %{code: code, message: msg}}  → {:error_detail, code, msg}
#   4. {:error, reason} cuando is_atom(reason) → {:simple_error, reason}
#   5. _anything_else                          → :unknown
#
# Ejemplo:
#   DataExtractor.extract({:ok, %User{name: "Alice", email: "a@x.com", role: :admin}})
#   # => {:admin_found, "Alice"}
#
#   DataExtractor.extract({:ok, %User{name: "Bob", email: "b@x.com", role: :viewer}})
#   # => {:user_found, "Bob", :viewer}
#
#   DataExtractor.extract({:error, %{code: 404, message: "Not found"}})
#   # => {:error_detail, 404, "Not found"}
#
#   DataExtractor.extract({:error, :timeout})
#   # => {:simple_error, :timeout}
#
#   DataExtractor.extract(:unexpected)
#   # => :unknown

defmodule DataExtractor do
  # TODO: Implementa extract/1 con 5 cláusulas de pattern matching
  #
  # def extract({:ok, %User{name: name, role: :admin}}) do
  #   ...
  # end
  #
  # def extract({:ok, %User{name: name, role: role}}) do
  #   ...
  # end
  #
  # ...
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule PatternMatchingTests do
  def run do
    IO.puts("\n=== Verificación: Pattern Matching Avanzado ===\n")

    # Ejercicio 1: Guards numéricos
    check("classify(-5) => :negative",  NumberClassifier.classify(-5),  :negative)
    check("classify(0)  => :zero",      NumberClassifier.classify(0),   :zero)
    check("classify(7)  => :small",     NumberClassifier.classify(7),   :small)
    check("classify(10) => :large",     NumberClassifier.classify(10),  :large)
    check("classify(99) => :large",     NumberClassifier.classify(99),  :large)

    IO.puts("")

    # Ejercicio 2: List matching
    check("process([])       => {:empty}",         ListProcessor.process([]),          {:empty})
    check("process([42])     => {:single, 42}",    ListProcessor.process([42]),        {:single, 42})
    check("process([:a, :b]) => {:pair, :a, :b}",  ListProcessor.process([:a, :b]),    {:pair, :a, :b})
    check("process([1,2,3])  => {:many, 1, [2,3]}", ListProcessor.process([1, 2, 3]), {:many, 1, [2, 3]})

    IO.puts("")

    # Ejercicio 3: Map guard
    check("handle(:admin)     => :full_access",  AccessControl.handle(%{role: :admin}),     :full_access)
    check("handle(:moderator) => :full_access",  AccessControl.handle(%{role: :moderator}), :full_access)
    check("handle(:viewer)    => :read_only",    AccessControl.handle(%{role: :viewer}),    :read_only)
    check("handle(:guest)     => :unknown_role", AccessControl.handle(%{role: :guest}),     :unknown_role)

    IO.puts("")

    # Ejercicio 4: Pin operator
    check("verify_response match",    PinDemo.verify_response(:created, {:ok, :created, "body"}), {:match, "Estado coincide"})
    check("verify_response mismatch", PinDemo.verify_response(:created, {:ok, :ok, "body"}),      {:mismatch, :ok})
    check("verify_response error",    PinDemo.verify_response(:created, {:error, :timeout}),      {:error, :timeout})

    IO.puts("")

    # Ejercicio 5: Nested destructuring
    admin = %User{name: "Alice", email: "a@x.com", role: :admin}
    viewer = %User{name: "Bob", email: "b@x.com", role: :viewer}

    check("extract admin",        DataExtractor.extract({:ok, admin}),                              {:admin_found, "Alice"})
    check("extract user",         DataExtractor.extract({:ok, viewer}),                             {:user_found, "Bob", :viewer})
    check("extract error detail", DataExtractor.extract({:error, %{code: 404, message: "Not found"}}), {:error_detail, 404, "Not found"})
    check("extract simple error", DataExtractor.extract({:error, :timeout}),                        {:simple_error, :timeout})
    check("extract unknown",      DataExtractor.extract(:unexpected),                               :unknown)

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
# ERROR 1: Orden de cláusulas incorrecto (la específica DESPUÉS de la general)
#
#   def extract({:ok, %User{name: name, role: role}}), do: {:user_found, name, role}
#   def extract({:ok, %User{name: name, role: :admin}}), do: {:admin_found, name}  # NUNCA llega aquí
#
#   Causa: la segunda cláusula nunca matchea porque la primera ya captura todo.
#   Solución: :admin primero, luego la cláusula genérica de role.
#
# ERROR 2: Usar funciones propias en guards
#
#   def classify(n) when my_custom_check(n), do: ...  # CompileError
#
#   Causa: los guards solo permiten funciones de la lista permitida.
#   Solución: mover la lógica al cuerpo de la función con case/cond/if.
#
# ERROR 3: Olvidar el pin operator y obtener rebind silencioso
#
#   expected = :ok
#   case response do
#     {:ok, expected} -> "siempre matchea y rebindea expected"  # bug sutil
#   end
#
#   Solución: usar ^expected para fijar el valor.
#
# ERROR 4: Map pattern matching falla si la clave no existe
#
#   def handle(%{role: role, name: name}), do: ...
#   handle(%{role: :admin})  # MatchError — :name no existe
#
#   Solución: solo incluir en el patrón las claves que necesitas.
#
# ERROR 5: guard `and` vs `when` anidados
#
#   def f(x) when x > 0 and x < 10   # correcto
#   def f(x) when x > 0 when x < 10  # dos guards separados (OR semántica)

# =============================================================================
# Summary
# =============================================================================
#
# - Guards (when) refinan el matching con condiciones adicionales
# - El orden de las cláusulas determina cuál gana: más específica primero
# - El pin operator (^) fija el valor de una variable en el match
# - El destructuring funciona con maps (parcial), structs y tuplas anidadas
# - Solo funciones de la biblioteca estándar son válidas en guards

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 10: Comprehensions avanzadas
# - Explorar: with/1 para chaining de pattern matching
# - Explorar: binary pattern matching (<<segment::size>>)

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/patterns-and-guards.html
# - https://hexdocs.pm/elixir/guards.html
# - Programming Elixir >= 1.6, Cap. 6 — Pattern Matching

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Implementa la función `parse_response/1` que maneja 5 formas distintas
# de respuesta HTTP (simuladas como tuplas anidadas):
#
#   1. {:http, 200, body} cuando body no es nil       → {:success, body}
#   2. {:http, 201, body}                             → {:created, body}
#   3. {:http, code, _} cuando code in 400..499       → {:client_error, code}
#   4. {:http, code, _} cuando code in 500..599       → {:server_error, code}
#   5. {:http, code, body} (cualquier otro código)    → {:unexpected, code, body}
#
# Bonus: agrega una cláusula para cuando la respuesta no es una tupla
# con formato HTTP válido → :invalid_response

PatternMatchingTests.run()
