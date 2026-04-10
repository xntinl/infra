# 08. Protocols

**Difficulty**: Intermedio

---

## Prerequisites

- Módulos y structs en Elixir
- Pattern matching básico
- Comprensión de polimorfismo
- Ejercicio 07: Behaviours y Callbacks

---

## Learning Objectives

1. Definir un protocol con `defprotocol` y entender su contrato
2. Implementar un protocol con `defimpl` para tipos nativos y structs propios
3. Comprender el mecanismo de dispatch dinámico basado en tipo
4. Usar `@fallback_to_any true` para implementaciones por defecto
5. Distinguir cuándo usar protocols vs behaviours
6. Implementar protocols para múltiples tipos sin modificar el código fuente original

---

## Concepts

### ¿Qué es un Protocol?

Un protocol define un conjunto de funciones que pueden ser implementadas por distintos tipos. A diferencia de los behaviours (que son para módulos que siguen un contrato), los protocols permiten el **polimorfismo ad-hoc**: añadir comportamiento a tipos existentes sin modificarlos.

```elixir
# Definición del protocol
defprotocol Serializable do
  @doc "Serializa el valor a una cadena JSON"
  def to_json(value)
end

# Implementación para Integer
defimpl Serializable, for: Integer do
  def to_json(value), do: Integer.to_string(value)
end

# Implementación para List
defimpl Serializable, for: List do
  def to_json(values) do
    inner = values |> Enum.map(&Serializable.to_json/1) |> Enum.join(", ")
    "[#{inner}]"
  end
end

# Uso — el dispatch es automático por tipo
Serializable.to_json(42)        # "42"
Serializable.to_json([1, 2])    # "[1, 2]"
```

### defprotocol

```elixir
defprotocol MiProtocol do
  # Las funciones aquí son el contrato. No tienen cuerpo.
  @doc "Documentación de la función"
  def funcion_requerida(value)

  # Puedes declarar múltiples funciones
  def otra_funcion(value, opts \\ [])
end
```

### defimpl

```elixir
defimpl MiProtocol, for: MiStruct do
  def funcion_requerida(%MiStruct{campo: v}), do: v
  def otra_funcion(value, _opts), do: value
end

# Tipos nativos disponibles: Integer, Float, String, Atom,
# List, Map, Tuple, Function, PID, Port, Reference, BitString, Any
```

### @fallback_to_any

Cuando ninguna implementación específica existe para un tipo, Elixir lanza `Protocol.UndefinedError`. Con `@fallback_to_any true`, el protocol cae a la implementación de `Any`:

```elixir
defprotocol Describible do
  @fallback_to_any true
  def describe(value)
end

defimpl Describible, for: Any do
  def describe(value), do: "Valor desconocido: #{inspect(value)}"
end

defimpl Describible, for: Integer do
  def describe(n), do: "Entero: #{n}"
end

Describible.describe(42)        # "Entero: 42"
Describible.describe(:ok)       # "Valor desconocido: :ok"
Describible.describe(%{a: 1})   # "Valor desconocido: %{a: 1}"
```

### Implementar para Structs propios

Las structs pertenecen al módulo que las define. Puedes implementar un protocol en el mismo módulo o en un `defimpl` separado:

```elixir
defmodule Usuario do
  defstruct [:nombre, :email, :edad]
end

defimpl Serializable, for: Usuario do
  def to_json(%Usuario{nombre: n, email: e, edad: edad}) do
    ~s({"nombre": "#{n}", "email": "#{e}", "edad": #{edad}})
  end
end

usuario = %Usuario{nombre: "Ana", email: "ana@example.com", edad: 30}
Serializable.to_json(usuario)
# {"nombre": "Ana", "email": "ana@example.com", "edad": 30}
```

### Dispatch automático

El protocolo selecciona la implementación correcta en runtime basándose en el tipo del primer argumento:

```elixir
def procesar(valor) do
  # No importa el tipo — el protocol lo resuelve
  Serializable.to_json(valor)
end

procesar(1)           # delega a Integer
procesar("hola")      # delega a BitString
procesar([1, 2, 3])   # delega a List
```

### Protocols vs Behaviours

| | Protocol | Behaviour |
|---|---|---|
| Propósito | Polimorfismo por tipo de datos | Contrato para módulos |
| Extensibilidad | Cualquiera puede añadir implementaciones | Solo el módulo que usa `@behaviour` |
| Dispatch | Dinámico, por tipo del primer argumento | Estático, por módulo |
| Caso de uso | `Enum`, `String.Chars`, `Inspect` | `GenServer`, `Supervisor`, plugins |

---

## Exercises

### Exercise 1: Serializable Protocol (JSON)

Implementa el protocol `Serializable` que convierte distintos tipos de datos a su representación JSON.

```elixir
# Archivo: lib/serializable.ex

defprotocol Serializable do
  @doc """
  Convierte un valor a su representación JSON como string.
  """
  def to_json(value)
end

# --- Implementaciones para tipos nativos ---

defimpl Serializable, for: Integer do
  # TODO: Devolver el entero como string
  # Pista: Integer.to_string/1
  def to_json(value) do
  end
end

defimpl Serializable, for: Float do
  # TODO: Devolver el float como string
  # Pista: Float.to_string/1
  def to_json(value) do
  end
end

defimpl Serializable, for: BitString do
  # TODO: Envolver el string en comillas dobles
  # Ej: "hola" -> ~s("hola")
  def to_json(value) do
  end
end

defimpl Serializable, for: Atom do
  # TODO: nil -> "null", true/false -> "true"/"false"
  # Otros atoms -> string con comillas: :ok -> ~s("ok")
  def to_json(value) do
  end
end

defimpl Serializable, for: List do
  # TODO: Serializar cada elemento y unir con comas entre corchetes
  # Ej: [1, "a", true] -> "[1, \"a\", true]"
  # Pista: Enum.map + Enum.join + Serializable.to_json/1 (recursivo)
  def to_json(values) do
  end
end

defimpl Serializable, for: Map do
  # TODO: Serializar el mapa como objeto JSON
  # Ej: %{nombre: "Ana", edad: 30} -> {"nombre": "Ana", "edad": 30}
  # Pista: Enum.map_join sobre Map.to_list/1
  # Las claves atom se convierten a string sin comillas externas primero
  def to_json(map) do
  end
end

# --- Struct de dominio ---

defmodule Producto do
  defstruct [:id, :nombre, :precio, :disponible]
end

defimpl Serializable, for: Producto do
  # TODO: Serializar el struct como JSON con todos sus campos
  # Ej: %Producto{id: 1, nombre: "Silla", precio: 99.9, disponible: true}
  #     -> {"id": 1, "nombre": "Silla", "precio": 99.9, "disponible": true}
  def to_json(%Producto{id: id, nombre: n, precio: p, disponible: d}) do
  end
end
```

**Verificación esperada:**

```elixir
# En iex:
Serializable.to_json(42)            # "42"
Serializable.to_json(3.14)          # "3.14"
Serializable.to_json("hola")        # "\"hola\""
Serializable.to_json(nil)           # "null"
Serializable.to_json(true)          # "true"
Serializable.to_json([1, "x"])      # "[1, \"x\"]"
Serializable.to_json(%{a: 1})       # "{\"a\": 1}"

p = %Producto{id: 1, nombre: "Silla", precio: 99.9, disponible: true}
Serializable.to_json(p)
# {"id": 1, "nombre": "Silla", "precio": 99.9, "disponible": true}
```

---

### Exercise 2: Printable Protocol con colores ANSI

Implementa el protocol `Printable` que genera representaciones visuales con colores ANSI para la terminal.

```elixir
# Archivo: lib/printable.ex

defmodule ANSI do
  # Códigos de escape ANSI básicos
  def reset, do: "\e[0m"
  def red(text),     do: "\e[31m#{text}\e[0m"
  def green(text),   do: "\e[32m#{text}\e[0m"
  def yellow(text),  do: "\e[33m#{text}\e[0m"
  def blue(text),    do: "\e[34m#{text}\e[0m"
  def cyan(text),    do: "\e[36m#{text}\e[0m"
  def bold(text),    do: "\e[1m#{text}\e[0m"
end

defprotocol Printable do
  @fallback_to_any true

  @doc """
  Devuelve una representación visual coloreada del valor.
  """
  def pretty(value)

  @doc """
  Imprime el valor en stdout con un salto de línea.
  """
  def print(value)
end

defimpl Printable, for: Any do
  # Implementación por defecto — usa inspect con color gris
  def pretty(value), do: "\e[90m#{inspect(value)}\e[0m"
  def print(value),  do: IO.puts(Printable.pretty(value))
end

defimpl Printable, for: Integer do
  # TODO: Mostrar enteros en color azul
  # Ej: pretty(42) -> ANSI.blue("42")
  def pretty(value) do
  end

  def print(value), do: IO.puts(pretty(value))
end

defimpl Printable, for: Float do
  # TODO: Mostrar floats en color cyan
  def pretty(value) do
  end

  def print(value), do: IO.puts(pretty(value))
end

defimpl Printable, for: BitString do
  # TODO: Mostrar strings en color verde, con comillas incluidas
  # Ej: pretty("hola") -> ANSI.green("\"hola\"")
  def pretty(value) do
  end

  def print(value), do: IO.puts(pretty(value))
end

defimpl Printable, for: Atom do
  # TODO: nil -> rojo, true/false -> amarillo, otros -> cyan con ":"
  # Ej: pretty(nil) -> ANSI.red("nil")
  #     pretty(:ok)  -> ANSI.cyan(":ok")
  def pretty(value) do
  end

  def print(value), do: IO.puts(pretty(value))
end

defimpl Printable, for: List do
  # TODO: Mostrar lista con cada elemento pretty-printeado, separados por ", "
  # y rodeados de "[" y "]" en negrita
  # Ej: pretty([1, "a"]) -> "[#{ANSI.blue("1")}, #{ANSI.green("\"a\"")}]"
  def pretty(values) do
  end

  def print(value), do: IO.puts(pretty(value))
end

# --- Struct propio ---

defmodule Alerta do
  defstruct [:tipo, :mensaje, :timestamp]
  # tipo: :error | :warning | :info
end

defimpl Printable, for: Alerta do
  # TODO: Mostrar según tipo:
  # :error   -> rojo con "[ERROR]"
  # :warning -> amarillo con "[WARN]"
  # :info    -> azul con "[INFO]"
  # Formato: "[TIPO] mensaje (timestamp)"
  def pretty(%Alerta{tipo: tipo, mensaje: msg, timestamp: ts}) do
  end

  def print(value), do: IO.puts(pretty(value))
end
```

**Verificación esperada:**

```elixir
# En iex — los colores se ven en terminal real
Printable.print(42)
Printable.print(3.14)
Printable.print("hola")
Printable.print(nil)
Printable.print(true)
Printable.print(:ok)
Printable.print([1, "x", nil])

alerta = %Alerta{tipo: :error, mensaje: "Fallo de conexión", timestamp: "2026-04-10"}
Printable.print(alerta)
# [ERROR] Fallo de conexión (2026-04-10)  <- en rojo

# Fallback para tipos sin implementación:
Printable.print({:tuple, :value})  # usa Any -> gris
```

---

### Exercise 3: Comparable Protocol con Ordering

Implementa el protocol `Comparable` que permite ordenar y comparar distintos tipos de datos con semántica personalizada.

```elixir
# Archivo: lib/comparable.ex

defprotocol Comparable do
  @doc """
  Compara dos valores del mismo tipo.
  Devuelve: :lt (menor), :eq (igual), :gt (mayor)
  """
  def compare(a, b)

  @doc """
  Devuelve true si a < b
  """
  def less_than?(a, b)

  @doc """
  Devuelve true si a > b
  """
  def greater_than?(a, b)
end

# Módulo helper para implementaciones repetitivas
defmodule Comparable.Helpers do
  # TODO: Implementar less_than?/2 usando compare/2
  # Pista: Comparable.compare(a, b) == :lt
  def less_than?(a, b) do
  end

  # TODO: Implementar greater_than?/2 usando compare/2
  def greater_than?(a, b) do
  end
end

defimpl Comparable, for: Integer do
  # TODO: Comparar dos enteros
  # Pista: usar cond o guards para devolver :lt | :eq | :gt
  def compare(a, b) do
  end

  def less_than?(a, b),    do: Comparable.Helpers.less_than?(a, b)
  def greater_than?(a, b), do: Comparable.Helpers.greater_than?(a, b)
end

defimpl Comparable, for: Float do
  # TODO: Comparar dos floats (misma lógica que Integer)
  def compare(a, b) do
  end

  def less_than?(a, b),    do: Comparable.Helpers.less_than?(a, b)
  def greater_than?(a, b), do: Comparable.Helpers.greater_than?(a, b)
end

defimpl Comparable, for: BitString do
  # TODO: Comparar strings alfabéticamente
  # Pista: String.compare/2 o simplemente < > ==
  def compare(a, b) do
  end

  def less_than?(a, b),    do: Comparable.Helpers.less_than?(a, b)
  def greater_than?(a, b), do: Comparable.Helpers.greater_than?(a, b)
end

# --- Structs de dominio con semántica de ordenación propia ---

defmodule Temperatura do
  @moduledoc """
  Temperatura en cualquier unidad. La comparación normaliza a Celsius.
  """
  defstruct [:valor, :unidad]  # unidad: :celsius | :fahrenheit | :kelvin

  def to_celsius(%Temperatura{valor: v, unidad: :celsius}),    do: v
  def to_celsius(%Temperatura{valor: v, unidad: :fahrenheit}), do: (v - 32) * 5 / 9
  def to_celsius(%Temperatura{valor: v, unidad: :kelvin}),     do: v - 273.15
end

defimpl Comparable, for: Temperatura do
  # TODO: Comparar temperaturas normalizando a Celsius primero
  # Pista: Temperatura.to_celsius/1
  def compare(a, b) do
  end

  def less_than?(a, b),    do: Comparable.Helpers.less_than?(a, b)
  def greater_than?(a, b), do: Comparable.Helpers.greater_than?(a, b)
end

defmodule Version do
  @moduledoc """
  Versión semántica simplificada: major.minor.patch
  """
  defstruct [:major, :minor, :patch]

  def parse(str) do
    [major, minor, patch] = str |> String.split(".") |> Enum.map(&String.to_integer/1)
    %Version{major: major, minor: minor, patch: patch}
  end
end

defimpl Comparable, for: Version do
  # TODO: Comparar versiones: primero major, luego minor, luego patch
  # Si major es distinto, ese determina el resultado
  # Pista: puedes usar múltiples cond o pattern matching en tuplas
  def compare(%Version{} = a, %Version{} = b) do
  end

  def less_than?(a, b),    do: Comparable.Helpers.less_than?(a, b)
  def greater_than?(a, b), do: Comparable.Helpers.greater_than?(a, b)
end

# --- Función de ordenación genérica ---

defmodule Sorter do
  @doc """
  Ordena una lista de elementos Comparable usando merge sort.
  """
  # TODO: Implementar sort/1 usando Enum.sort/2 con Comparable.less_than?/2
  # Pista: Enum.sort(lista, fn a, b -> Comparable.less_than?(a, b) end)
  def sort(lista) do
  end

  @doc """
  Devuelve el mínimo de una lista.
  """
  # TODO: Usar Enum.min_by/2 con Comparable.compare
  def min(lista) do
  end

  @doc """
  Devuelve el máximo de una lista.
  """
  # TODO: Usar Enum.max_by/2 con Comparable.compare
  def max(lista) do
  end
end
```

**Verificación esperada:**

```elixir
# Enteros
Comparable.compare(1, 2)   # :lt
Comparable.compare(2, 2)   # :eq
Comparable.compare(3, 1)   # :gt

# Strings
Comparable.compare("a", "b")   # :lt
Comparable.compare("z", "a")   # :gt

# Temperaturas — comparación por valor físico real
t1 = %Temperatura{valor: 100, unidad: :celsius}     # 100°C
t2 = %Temperatura{valor: 212, unidad: :fahrenheit}  # también 100°C
t3 = %Temperatura{valor: 373.15, unidad: :kelvin}   # también 100°C
Comparable.compare(t1, t2)   # :eq
Comparable.compare(t1, t3)   # :eq

# Versiones
v1 = Version.parse("1.2.3")
v2 = Version.parse("1.10.0")
v3 = Version.parse("2.0.0")
Comparable.compare(v1, v2)   # :lt  (minor: 2 < 10)
Comparable.compare(v2, v3)   # :lt  (major: 1 < 2)
Comparable.compare(v3, v3)   # :eq

# Ordenación genérica
versiones = [v3, v1, v2]
Sorter.sort(versiones)
# [%Version{major: 1, minor: 2, patch: 3},
#  %Version{major: 1, minor: 10, patch: 0},
#  %Version{major: 2, minor: 0, patch: 0}]
```

---

## Common Mistakes

### 1. Confundir Protocol con Behaviour

```elixir
# MAL — usar @behaviour cuando quieres polimorfismo por tipo
defmodule MiModulo do
  @behaviour Serializable  # No funciona así
end

# BIEN — usar defimpl para extender un protocol a un tipo
defimpl Serializable, for: MiStruct do
  def to_json(%MiStruct{} = s), do: ...
end
```

### 2. Olvidar @fallback_to_any

```elixir
# MAL — el protocol falla en runtime para tipos no implementados
defprotocol MiProtocol do
  def hacer(value)
end

MiProtocol.hacer(:some_atom)  # ** (Protocol.UndefinedError)

# BIEN — con fallback
defprotocol MiProtocol do
  @fallback_to_any true
  def hacer(value)
end

defimpl MiProtocol, for: Any do
  def hacer(value), do: {:error, "Tipo no soportado: #{inspect(value)}"}
end
```

### 3. Pattern matching incompleto en defimpl

```elixir
# MAL — no desempaqueta el struct, usa el valor entero directamente
defimpl Serializable, for: Producto do
  def to_json(p) do
    # Acceder a p.nombre funciona pero es menos idiomático
    ~s({"nombre": "#{p.nombre}"})
  end
end

# BIEN — desempaquetar en la cabeza de función
defimpl Serializable, for: Producto do
  def to_json(%Producto{nombre: n, precio: p}) do
    ~s({"nombre": "#{n}", "precio": #{p}})
  end
end
```

### 4. Implementar para Map cuando se quiere Struct

```elixir
# MAL — los structs NO son Map a efectos de protocols
defimpl MiProtocol, for: Map do
  def hacer(%MiStruct{} = s), do: ...  # Nunca se llama para MiStruct
end

# BIEN — implementar explícitamente para el struct
defimpl MiProtocol, for: MiStruct do
  def hacer(%MiStruct{} = s), do: ...
end
```

---

## Verification

```bash
# Compilar el proyecto
mix compile

# Ejecutar tests
mix test

# Explorar en consola interactiva
iex -S mix
```

```elixir
# Smoke test rápido en iex:
Serializable.to_json(42)
Serializable.to_json([1, 2, 3])
Serializable.to_json(%{a: 1, b: true})

Printable.print(42)
Printable.print(%Alerta{tipo: :error, mensaje: "Test", timestamp: "now"})

Comparable.compare(Version.parse("1.0.0"), Version.parse("2.0.0"))
```

---

## Summary

Los protocols en Elixir son el mecanismo de polimorfismo más potente del lenguaje: permiten extender el comportamiento de cualquier tipo — incluyendo tipos nativos y de librerías externas — sin modificar su código fuente. La clave está en el dispatch dinámico que selecciona la implementación correcta según el tipo del primer argumento en runtime. El uso de `@fallback_to_any true` proporciona un comportamiento por defecto seguro para tipos no contemplados, evitando errores inesperados en producción.

## What's Next

**09. Pattern Matching Avanzado** — guards con `when`, el operador pin `^`, y nested matching en estructuras complejas.

## Resources

- [Elixir Docs — Protocols](https://hexdocs.pm/elixir/Protocol.html)
- [Protocol Guide](https://elixir-lang.org/getting-started/protocols.html)
- [Enumerable Protocol](https://hexdocs.pm/elixir/Enumerable.html)
- [String.Chars Protocol](https://hexdocs.pm/elixir/String.Chars.html)
