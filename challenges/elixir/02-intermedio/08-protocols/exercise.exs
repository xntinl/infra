# =============================================================================
# Ejercicio 08: Protocols — Polimorfismo en Runtime
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - Módulos y funciones en Elixir (nivel básico)
# - Structs: defstruct, acceso a campos
# - Pattern matching básico
# - Conceptos de polimorfismo

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Definir un protocol con defprotocol y sus callbacks
# 2. Implementar un protocol para tipos nativos (Integer, BitString)
# 3. Implementar un protocol para structs personalizados
# 4. Usar @fallback_to_any true para implementaciones genéricas
# 5. Entender cómo Elixir despacha calls de protocol en runtime

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# PROTOCOLS: La respuesta de Elixir al polimorfismo ad-hoc.
#
# Un protocol define una interfaz (un contrato de funciones) que distintos
# tipos pueden implementar de manera independiente.
#
# Sintaxis:
#
#   defprotocol MyProtocol do
#     @doc "Descripción del callback"
#     def my_function(term)
#   end
#
# Implementación para un tipo concreto:
#
#   defimpl MyProtocol, for: SomeType do
#     def my_function(term) do
#       # implementación específica para SomeType
#     end
#   end
#
# Tipos nativos disponibles: Integer, Float, BitString, Atom, List, Map,
# Tuple, Function, PID, Port, Reference
#
# FALLBACK TO ANY:
#
#   defprotocol MyProtocol do
#     @fallback_to_any true
#     def my_function(term)
#   end
#
#   defimpl MyProtocol, for: Any do
#     def my_function(term), do: inspect(term)
#   end
#
# Cuando ninguna implementación específica existe para un tipo, Elixir
# usa la implementación de Any.
#
# PROTOCOL CONSOLIDATION:
# En producción (mix compile), Elixir consolida los protocols en un
# único dispatch table para rendimiento óptimo. En IEx/scripts, el
# dispatch es dinámico.

# -----------------------------------------------------------------------------
# Structs de apoyo (necesarios para los ejercicios)
# -----------------------------------------------------------------------------

defmodule User do
  @enforce_keys [:name, :email]
  defstruct [:name, :email, role: :viewer, age: nil]
end

defmodule Product do
  @enforce_keys [:name, :price]
  defstruct [:name, :price, :sku, in_stock: true]
end

# =============================================================================
# Exercise 1: Define el protocol Describable
# =============================================================================
#
# Define un protocol llamado `Describable` con:
# - @fallback_to_any true  (para el ejercicio 5)
# - Un callback `describe(term)` que retorna un String con descripción
#   legible por humanos del valor recibido.
#
# Tip: los callbacks de protocol llevan solo la declaración, sin cuerpo.

# TODO: Define el protocol Describable aquí
# defprotocol Describable do
#   ...
# end

# =============================================================================
# Exercise 2: Implementar Describable para Integer
# =============================================================================
#
# Implementa Describable para el tipo Integer.
# La función describe/1 debe retornar un string con el formato:
#
#   "El número entero 42"
#   "El número entero -7"
#
# Tip: usa Integer.to_string/1 o string interpolation #{...}

# TODO: Implementa Describable para Integer
# defimpl Describable, for: Integer do
#   def describe(n) do
#     ...
#   end
# end

# =============================================================================
# Exercise 3: Implementar Describable para BitString (strings)
# =============================================================================
#
# Implementa Describable para el tipo BitString (que incluye los binarios
# de Elixir, es decir, los strings normales entre comillas dobles).
#
# La función describe/1 debe retornar:
#
#   "El texto \"hola mundo\" (11 caracteres)"
#
# Tip: String.length/1 para la longitud, String.graphemes/1 para caracteres

# TODO: Implementa Describable para BitString
# defimpl Describable, for: BitString do
#   def describe(s) do
#     ...
#   end
# end

# =============================================================================
# Exercise 4: Implementar Describable para structs User y Product
# =============================================================================
#
# Implementa Describable para User y Product por separado.
#
# Para User, retornar:
#   "Usuario: Alice <alice@example.com> [admin]"
#
# Para Product, retornar:
#   "Producto: Widget Pro (SKU: WP-001) — $29.99 — EN STOCK"
#   "Producto: Old Item (SKU: N/A) — $5.00 — AGOTADO"
#
# Tip: el campo :sku puede ser nil, usa || "N/A" para el fallback
#      el campo :in_stock es un boolean

# TODO: Implementa Describable para User
# defimpl Describable, for: User do
#   def describe(%User{name: name, email: email, role: role}) do
#     ...
#   end
# end

# TODO: Implementa Describable para Product
# defimpl Describable, for: Product do
#   def describe(%Product{name: name, price: price, sku: sku, in_stock: in_stock}) do
#     stock_label = ...
#     sku_label = ...
#     ...
#   end
# end

# =============================================================================
# Exercise 5: Fallback Any implementation
# =============================================================================
#
# Implementa Describable para Any. Esta implementación se usará para
# cualquier tipo que no tenga una implementación específica (ej: Float,
# List, Map, Atom, Tuple, etc.).
#
# La función describe/1 debe retornar:
#   "Valor desconocido: <inspect del término>"
#
# Esto solo funciona si el protocol tiene @fallback_to_any true (Ejercicio 1)

# TODO: Implementa Describable para Any
# defimpl Describable, for: Any do
#   def describe(x) do
#     ...
#   end
# end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule ProtocolTests do
  def run do
    IO.puts("\n=== Verificación: Protocols ===\n")

    # Ejercicio 2: Integer
    result_int = Describable.describe(42)
    expected_int = "El número entero 42"
    check("Integer positivo", result_int, expected_int)

    result_neg = Describable.describe(-7)
    expected_neg = "El número entero -7"
    check("Integer negativo", result_neg, expected_neg)

    # Ejercicio 3: BitString
    result_str = Describable.describe("hola mundo")
    expected_str = ~s(El texto "hola mundo" (11 caracteres))
    check("BitString", result_str, expected_str)

    # Ejercicio 4: User
    user = %User{name: "Alice", email: "alice@example.com", role: :admin}
    result_user = Describable.describe(user)
    expected_user = "Usuario: Alice <alice@example.com> [admin]"
    check("User struct", result_user, expected_user)

    # Ejercicio 4: Product en stock
    product = %Product{name: "Widget Pro", price: 29.99, sku: "WP-001", in_stock: true}
    result_prod = Describable.describe(product)
    expected_prod = "Producto: Widget Pro (SKU: WP-001) — $29.99 — EN STOCK"
    check("Product en stock", result_prod, expected_prod)

    # Ejercicio 4: Product agotado sin SKU
    product2 = %Product{name: "Old Item", price: 5.0, sku: nil, in_stock: false}
    result_prod2 = Describable.describe(product2)
    expected_prod2 = "Producto: Old Item (SKU: N/A) — $5.0 — AGOTADO"
    check("Product agotado", result_prod2, expected_prod2)

    # Ejercicio 5: Any fallback
    result_float = Describable.describe(3.14)
    check("Any fallback (Float)", String.starts_with?(result_float, "Valor desconocido:"), true)

    result_list = Describable.describe([1, 2, 3])
    check("Any fallback (List)", String.starts_with?(result_list, "Valor desconocido:"), true)

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
# ERROR 1: Protocol.UndefinedError
#
#   ** (Protocol.UndefinedError) protocol Describable not implemented for 3.14
#
#   Causa: No existe implementación para Float y @fallback_to_any es false
#   (o no está definido). Por defecto es false.
#   Solución: Agregar @fallback_to_any true al protocol Y definir for: Any
#
# ERROR 2: Confundir BitString con String
#
#   defimpl Describable, for: String  # ← ¡INCORRECTO!
#
#   En Elixir, los strings (binarios UTF-8) son del tipo BitString.
#   El átomo String no es un tipo válido en defimpl.
#   Solución: defimpl Describable, for: BitString
#
# ERROR 3: Olvidar que Any requiere @fallback_to_any true
#
#   Si defines defimpl Describable, for: Any pero olvidas @fallback_to_any true
#   en el protocol, Elixir ignorará la implementación de Any y lanzará error.
#
# ERROR 4: Implementar el protocol dentro del mismo módulo del struct
#
#   defmodule User do
#     defimpl Describable, for: User do  # funciona pero no es idiomático
#       ...
#     end
#   end
#
#   Es válido pero dificulta la separación de responsabilidades.
#   Preferir defimpl fuera del módulo del struct.
#
# ERROR 5: Devolver átomos en vez de strings
#
#   def describe(_), do: :unknown  # ← incorrecto, debe ser un String

# =============================================================================
# Summary
# =============================================================================
#
# Los protocols en Elixir son la forma idiomática de lograr polimorfismo:
#
# - defprotocol define el contrato (qué funciones deben existir)
# - defimpl define la implementación concreta para cada tipo
# - @fallback_to_any true + for: Any proveen un fallback genérico
# - El dispatch es automático basado en el tipo del primer argumento
# - Funcionan con tipos nativos (Integer, BitString...) y structs propios

# =============================================================================
# What's Next
# =============================================================================
# - Ejercicio 09: Pattern Matching Avanzado (guards, multi-clause, pin)
# - Explorar: Enumerable protocol (cómo Enum funciona con cualquier colección)
# - Explorar: String.Chars protocol (cómo to_string/1 es polimórfico)

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/Protocol.html
# - https://elixir-lang.org/getting-started/protocols.html
# - Elixir in Action, Cap. 4 — Data abstractions

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Implementa un protocol `Serializable` con el callback `to_json(term)`
# que retorne un string con la representación JSON del valor.
#
# Implementa para:
# - User: {"name": "Alice", "email": "alice@example.com", "role": "admin"}
# - Product: {"name": "Widget", "price": 29.99, "in_stock": true}
# - List: un array JSON con los elementos serializados recursivamente
#         (pista: usa Enum.map + Enum.join para unirlos con comas)
#
# Ejemplo:
#   Serializable.to_json(%User{name: "Bob", email: "bob@x.com", role: :viewer})
#   # => ~s({"name": "Bob", "email": "bob@x.com", "role": "viewer"})
#
#   Serializable.to_json([%User{...}, %Product{...}])
#   # => "[{...}, {...}]"

ProtocolTests.run()
