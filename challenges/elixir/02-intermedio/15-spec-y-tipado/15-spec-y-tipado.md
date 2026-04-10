# 15. Specs y Tipado con Dialyxir

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de módulos y funciones en Elixir
- Familiaridad con Mix y dependencias (ejercicio 19)
- Pattern matching y tipos básicos de Elixir

## Learning Objectives
After completing this exercise, you will be able to:
- Escribir `@spec` para documentar y validar tipos de funciones
- Definir tipos personalizados con `@type` y `@typep`
- Usar tipos union `|` para modelar retornos múltiples
- Integrar Dialyxir en tu proyecto para análisis estático
- Interpretar los errores que reporta `mix dialyzer`

## Concepts

### @spec: Contratos de Tipo

`@spec` es un atributo de módulo que documenta los tipos de entrada y salida de una función. No es ejecutado en runtime — Dialyxir lo usa en análisis estático para detectar inconsistencias antes de que lleguen a producción.

```elixir
defmodule Calculator do
  # @spec nombre_funcion(tipo_arg1, tipo_arg2) :: tipo_retorno
  @spec add(number(), number()) :: number()
  def add(a, b), do: a + b

  # Múltiples cláusulas comparten el mismo @spec
  @spec double(integer()) :: integer()
  def double(n), do: n * 2
end
```

El compilador no fuerza estos tipos, pero Dialyxir sí los verifica analizando el flujo del código. Esta separación es intencional: el análisis estático es una herramienta de desarrollo, no una restricción de runtime.

### @type y @typep: Tipos Personalizados

Cuando un tipo aparece en múltiples `@spec`, extraerlo como `@type` mejora la legibilidad y mantiene la consistencia. `@typep` es para tipos privados al módulo.

```elixir
defmodule UserService do
  # @type es público — visible desde otros módulos
  @type user :: %{
    id: pos_integer(),
    name: String.t(),
    email: String.t(),
    active: boolean()
  }

  # @typep es privado — solo dentro de este módulo
  @typep user_id :: pos_integer()

  @spec get_user(user_id()) :: user() | nil
  def get_user(id) do
    # ...
  end

  @spec create_user(String.t(), String.t()) :: {:ok, user()} | {:error, String.t()}
  def create_user(name, email) do
    # ...
  end
end
```

### Tipos Built-in de Elixir

Elixir incluye un conjunto rico de tipos predefinidos:

```elixir
# Tipos numéricos
integer()      # Cualquier entero
float()        # Número flotante
number()       # integer() | float()
pos_integer()  # Entero positivo (> 0)
non_neg_integer() # Entero no negativo (>= 0)

# Tipos de texto
String.t()     # Binary string (UTF-8)
atom()         # :ok, :error, :my_atom
binary()       # Secuencia de bytes

# Tipos de colección
list()             # Lista genérica
list(integer())    # Lista de enteros
[integer()]        # Shorthand equivalente
map()              # Mapa genérico
keyword()          # Keyword list
keyword(integer()) # Keyword list con valores enteros

# Tipos especiales
boolean()      # true | false
any()          # Cualquier tipo (evitar cuando sea posible)
none()         # Ningún valor — función que nunca retorna
no_return()    # Función que lanza excepción o loop infinito
term()         # Equivalente a any()
```

### Union Types con |

El operador `|` permite expresar que una función puede retornar (o aceptar) más de un tipo. Es el patrón estándar para resultados que pueden ser éxito o error.

```elixir
defmodule Parser do
  # Puede retornar {:ok, integer()} o {:error, String.t()}
  @spec parse_int(String.t()) :: {:ok, integer()} | {:error, String.t()}
  def parse_int(str) do
    case Integer.parse(str) do
      {n, ""} -> {:ok, n}
      _ -> {:error, "Cannot parse '#{str}' as integer"}
    end
  end

  # nil es un tipo válido en unions
  @spec find_first(list(), (any() -> boolean())) :: any() | nil
  def find_first(list, predicate) do
    Enum.find(list, predicate)
  end
end
```

### Dialyxir: Análisis Estático

Dialyxir es un wrapper de Dialyzer (herramienta de Erlang) que analiza el código en busca de inconsistencias de tipo sin ejecutarlo. Detecta:

- Llamadas a funciones con tipos incorrectos
- Cláusulas de pattern matching inalcanzables
- Funciones que nunca retornan el tipo que prometen en `@spec`

```bash
# En mix.exs, agregar en deps:
{:dialyxir, "~> 1.0", only: :dev, runtime: false}

# Después de mix deps.get:
$ mix dialyzer

# Ejemplo de error que Dialyxir detecta:
# lib/my_module.ex:15:2: The call MyModule.add("hello", 1)
# breaks the contract (number(), number()) :: number()
```

## Exercises

### Exercise 1: Basic @spec — Tipos Simples

Agrega `@spec` a las funciones existentes. El TODO indica dónde añadir cada spec.

```elixir
defmodule MathUtils do
  @moduledoc "Utilidades matemáticas con specs documentados."

  # TODO: Agrega @spec para add/2
  # Acepta dos number(), retorna number()
  def add(a, b), do: a + b

  # TODO: Agrega @spec para multiply/2
  # Acepta dos integer(), retorna integer()
  def multiply(a, b), do: a * b

  # TODO: Agrega @spec para greet/1
  # Acepta String.t(), retorna String.t()
  def greet(name), do: "Hello, #{name}!"

  # TODO: Agrega @spec para is_adult/1
  # Acepta non_neg_integer(), retorna boolean()
  def is_adult(age), do: age >= 18

  # TODO: Agrega @spec para to_string_list/1
  # Acepta list(integer()), retorna list(String.t())
  def to_string_list(nums), do: Enum.map(nums, &Integer.to_string/1)
end
```

Expected output:
```elixir
# Dialyxir no debe reportar errores con los specs correctos
$ mix dialyzer
# Starting Dialyzer
# ...
# done in 0m12.33s
# done (passed successfully)
```

---

### Exercise 2: Custom Types con @type

Define tipos personalizados para un módulo de usuarios. Los tipos hacen el código más expresivo y los specs más legibles.

```elixir
defmodule UserManager do
  @moduledoc "Gestión de usuarios con tipos personalizados."

  # TODO: Define @type user :: %{name: String.t(), age: non_neg_integer(), email: String.t()}
  # Este tipo representa la estructura completa de un usuario

  # TODO: Define @type user_id :: pos_integer()
  # IDs son enteros positivos

  # TODO: Define @typep validation_result :: :ok | {:error, String.t()}
  # Privado — solo usado internamente en este módulo

  # TODO: Agrega @spec usando los tipos definidos arriba
  # create_user/3 acepta (String.t(), non_neg_integer(), String.t()) y retorna user()
  def create_user(name, age, email) do
    %{name: name, age: age, email: email}
  end

  # TODO: Agrega @spec usando user_id() y user()
  # find_user/1 acepta user_id() y retorna user() | nil
  def find_user(id) do
    # Simulación — en real consultaría base de datos
    if id == 1 do
      %{name: "Alice", age: 30, email: "alice@example.com"}
    else
      nil
    end
  end

  # TODO: Agrega @spec usando user() y validation_result()
  # validate_user/1 acepta user() y retorna validation_result()
  defp validate_user(%{name: name, age: age}) do
    cond do
      String.length(name) == 0 -> {:error, "Name cannot be empty"}
      age < 0 -> {:error, "Age cannot be negative"}
      true -> :ok
    end
  end
end
```

Expected output:
```elixir
iex> UserManager.create_user("Alice", 30, "alice@example.com")
%{name: "Alice", age: 30, email: "alice@example.com"}

iex> UserManager.find_user(1)
%{name: "Alice", age: 30, email: "alice@example.com"}

iex> UserManager.find_user(999)
nil
```

---

### Exercise 3: Union Types para Resultados Múltiples

Los union types modelan funciones que pueden retornar resultados diferentes según el input. Este es el patrón más común en Elixir.

```elixir
defmodule DataParser do
  @moduledoc "Parser de datos con manejo explícito de errores via tipos."

  # TODO: Agrega @spec para parse_integer/1
  # Acepta String.t(), retorna {:ok, integer()} | {:error, String.t()}
  def parse_integer(str) do
    case Integer.parse(str) do
      {n, ""} -> {:ok, n}
      {_, _} -> {:error, "Trailing characters after number in '#{str}'"}
      :error -> {:error, "Cannot parse '#{str}' as integer"}
    end
  end

  # TODO: Agrega @spec para parse_float/1
  # Acepta String.t(), retorna {:ok, float()} | {:error, String.t()}
  def parse_float(str) do
    case Float.parse(str) do
      {f, ""} -> {:ok, f}
      _ -> {:error, "Cannot parse '#{str}' as float"}
    end
  end

  # TODO: Agrega @spec para classify_number/1
  # Acepta number() y retorna :positive | :negative | :zero
  def classify_number(n) when n > 0, do: :positive
  def classify_number(n) when n < 0, do: :negative
  def classify_number(0), do: :zero

  # TODO: Agrega @spec para safe_divide/2
  # Acepta dos number(), retorna {:ok, float()} | {:error, :division_by_zero}
  def safe_divide(_, 0), do: {:error, :division_by_zero}
  def safe_divide(a, b), do: {:ok, a / b}
end
```

Expected output:
```elixir
iex> DataParser.parse_integer("42")
{:ok, 42}

iex> DataParser.parse_integer("abc")
{:error, "Cannot parse 'abc' as integer"}

iex> DataParser.safe_divide(10, 2)
{:ok, 5.0}

iex> DataParser.safe_divide(10, 0)
{:error, :division_by_zero}
```

---

### Exercise 4: Optional y Nullable con nil

`nil` en Elixir se expresa como `any() | nil` en los specs. El patrón correcto es ser explícito sobre cuándo una función puede retornar `nil`.

```elixir
defmodule Collection do
  @moduledoc "Operaciones de colecciones con retornos opcionales."

  # TODO: Agrega @spec para find_by_id/2
  # Acepta list(map()) e integer(), retorna map() | nil
  def find_by_id(list, id) do
    Enum.find(list, fn item -> item[:id] == id end)
  end

  # TODO: Agrega @spec para first_even/1
  # Acepta list(integer()), retorna integer() | nil
  def first_even(numbers) do
    Enum.find(numbers, fn n -> rem(n, 2) == 0 end)
  end

  # TODO: Agrega @spec para safe_head/1
  # Acepta list(any()), retorna any() | nil
  # Hint: una lista vacía retorna nil
  def safe_head([]), do: nil
  def safe_head([head | _]), do: head

  # TODO: Agrega @spec para get_field/2
  # Acepta map() y atom(), retorna any() | nil
  def get_field(map, key), do: Map.get(map, key)
end
```

Expected output:
```elixir
iex> users = [%{id: 1, name: "Alice"}, %{id: 2, name: "Bob"}]
iex> Collection.find_by_id(users, 1)
%{id: 1, name: "Alice"}

iex> Collection.find_by_id(users, 99)
nil

iex> Collection.safe_head([])
nil

iex> Collection.safe_head([1, 2, 3])
1
```

---

### Exercise 5: Configurar y Ejecutar Dialyxir

Integra Dialyxir en tu proyecto Mix para análisis estático automático.

```elixir
# mix.exs — ANTES (sin Dialyxir)
defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_app,
      version: "0.1.0",
      elixir: "~> 1.14",
      deps: deps()
    ]
  end

  defp deps do
    []  # TODO: Agrega {:dialyxir, "~> 1.0", only: :dev, runtime: false}
  end
end
```

```elixir
# mix.exs — DESPUÉS (con Dialyxir)
defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_app,
      version: "0.1.0",
      elixir: "~> 1.14",
      # TODO: Agrega dialyzer: [plt_add_apps: [:mix]] para incluir Mix en el análisis
      deps: deps()
    ]
  end

  defp deps do
    [
      {:dialyxir, "~> 1.0", only: :dev, runtime: false}
    ]
  end
end
```

```bash
# TODO: Ejecuta estos comandos en orden y observa el output:

# 1. Descargar Dialyxir
$ mix deps.get

# 2. Primera ejecución — construye el PLT (Persistent Lookup Table)
# Tarda varios minutos la primera vez
$ mix dialyzer

# 3. Agrega esta función con un tipo incorrecto intencionalmente:
# def bad_example do
#   add("not a number", 42)  # Viola el @spec de add/2
# end

# 4. Ejecuta dialyzer de nuevo y observa el error reportado
$ mix dialyzer
```

Expected output:
```bash
$ mix dialyzer
Starting Dialyzer
[
  check_plt: false,
  init_plt: '/Users/user/.mix/plts/elixir-1.15.7-erlang-26.1.2-my_app-...'
]
Compiling 1 file (.ex)
done in 0m15.36s
done (passed successfully)
```

---

## Try It Yourself

Escribe specs completos para un módulo de validación de formularios. Sin solución incluida — usa lo aprendido para diseñar los tipos tú mismo.

```elixir
defmodule FormValidator do
  @moduledoc """
  Validador de formularios de registro de usuario.
  Diseña y agrega todos los @type y @spec necesarios.
  """

  # Necesitas definir @type para:
  # - field_error: un error específico de campo {atom(), String.t()}
  # - validation_errors: lista de field_error()
  # - validation_result: {:ok, map()} | {:error, validation_errors()}

  # Implementa y agrega specs a estas 5 funciones públicas:

  # 1. validate_form/1 — acepta un mapa con datos del formulario
  #    retorna validation_result()
  def validate_form(params) do
    # Valida name, email, age, password, confirm_password
    # Si hay errores, retorna {:error, lista_de_errores}
    # Si todo es válido, retorna {:ok, params_limpios}
    raise "Not implemented"
  end

  # 2. validate_email/1 — valida formato básico de email
  def validate_email(email) do
    raise "Not implemented"
  end

  # 3. validate_age/1 — valida que sea entero entre 18 y 120
  def validate_age(age) do
    raise "Not implemented"
  end

  # 4. validate_password/2 — valida contraseña y su confirmación
  def validate_password(password, confirmation) do
    raise "Not implemented"
  end

  # 5. format_errors/1 — convierte lista de errores a lista de strings legibles
  def format_errors(errors) do
    raise "Not implemented"
  end
end
```

**Objetivo**: Antes de implementar, diseña todos los `@type` y `@spec`. Ejecuta `mix dialyzer` para verificar que los tipos son consistentes con la implementación.

---

## Common Mistakes

### Mistake 1: Usar any() donde se puede ser más específico

**Wrong:**
```elixir
@spec process(any()) :: any()
def process(data), do: do_something(data)
```
**Why:** `any()` elimina el beneficio del análisis de tipos. Dialyxir no puede detectar errores de tipo si todo es `any()`.
**Fix:**
```elixir
@spec process(map()) :: {:ok, map()} | {:error, String.t()}
def process(data), do: do_something(data)
```

### Mistake 2: @spec después de la función

**Wrong:**
```elixir
def add(a, b), do: a + b
@spec add(number(), number()) :: number()  # Demasiado tarde
```
**Error:** El compilador no asocia el `@spec` con la función correcta.
**Fix:**
```elixir
@spec add(number(), number()) :: number()
def add(a, b), do: a + b  # @spec siempre ANTES de def
```

### Mistake 3: Confundir String.t() con binary()

**Wrong:**
```elixir
@spec greet(binary()) :: binary()
def greet(name), do: "Hello, #{name}!"
```
**Why:** Aunque `String.t()` es técnicamente un `binary()`, la convención Elixir es usar `String.t()` para texto UTF-8 legible. `binary()` se reserva para datos binarios arbitrarios (imágenes, archivos, etc.).
**Fix:**
```elixir
@spec greet(String.t()) :: String.t()
def greet(name), do: "Hello, #{name}!"
```

---

## Verification

```bash
# Verificar que los specs compilan sin warnings
$ mix compile --warnings-as-errors

# Ejecutar análisis completo con Dialyxir
$ mix dialyzer

# Ejecutar tests para verificar que el comportamiento es correcto
$ mix test
```

```elixir
# En IEx verificar los tipos en acción
iex> DataParser.parse_integer("42")
{:ok, 42}

iex> DataParser.parse_integer("not_a_number")
{:error, "Cannot parse 'not_a_number' as integer"}

iex> Collection.safe_head([])
nil
```

## Summary
- **Key concepts**: `@spec`, `@type`, `@typep`, tipos built-in, union types `|`, Dialyxir
- **What you practiced**: Documentar contratos de tipo, definir tipos personalizados, modelar retornos múltiples con `|`, integrar análisis estático en el flujo de desarrollo
- **Important to remember**: `@spec` va ANTES de `def`. Los specs son para Dialyxir y documentación — no afectan el runtime. Usa `String.t()` para texto, no `binary()`.

## What's Next
En el siguiente ejercicio **16-testing-exunit** aprenderás a escribir tests robustos con ExUnit — el framework de testing incluido en Elixir que usa los specs como contratos verificables en runtime.

## Resources
- [Typespecs — Elixir Docs](https://hexdocs.pm/elixir/typespecs.html)
- [Dialyxir on Hex](https://hex.pm/packages/dialyxir)
- [Built-in Types Reference](https://hexdocs.pm/elixir/typespecs.html#built-in-types)
- [Erlang Dialyzer User Guide](https://www.erlang.org/doc/apps/dialyzer/dialyzer_chapter.html)
