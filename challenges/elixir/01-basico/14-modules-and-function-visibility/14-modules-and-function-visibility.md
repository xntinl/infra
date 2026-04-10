# 14. Modules and Function Visibility

**Difficulty**: Basico

---

## Prerequisites

- Funciones anónimas y closures (ejercicio 13)
- Pattern matching básico (ejercicio 05)
- Atoms (ejercicio 02)

---

## Learning Objectives

- Definir módulos con `defmodule` para organizar código relacionado
- Distinguir funciones públicas (`def`) de privadas (`defp`)
- Usar module attributes `@attr` para constantes y metadata
- Escribir documentación con `@moduledoc` y `@doc`
- Entender módulos anidados y la convención de nombres
- Usar `alias` para abreviar nombres de módulos largos
- Conocer `import` y `require` a nivel introductorio

---

## Concepts

### `defmodule`: el contenedor de código

Un módulo es un namespace que agrupa funciones relacionadas. El nombre es
un atom que empieza en mayúscula.

```elixir
defmodule Greeting do
  def hello(name) do
    "Hello, #{name}!"
  end
end

Greeting.hello("Alice")
# => "Hello, Alice!"
```

Los módulos se compilan. En IEx puedes definirlos directamente. En proyectos
Mix van en archivos `.ex`.

### `def` vs `defp`: visibilidad

`def` define una función **pública** — accesible desde cualquier módulo.
`defp` define una función **privada** — solo accesible dentro del mismo módulo.

```elixir
defmodule Formatter do
  # Pública: parte de la API del módulo
  def format_name(first, last) do
    "#{capitalize_word(first)} #{capitalize_word(last)}"
  end

  # Privada: detalle de implementación, no expuesta
  defp capitalize_word(word) do
    String.capitalize(word)
  end
end

Formatter.format_name("alice", "smith")
# => "Alice Smith"

Formatter.capitalize_word("alice")
# ** (UndefinedFunctionError) function Formatter.capitalize_word/1 is undefined
```

Regla: expón solo lo mínimo necesario. Las funciones privadas son detalles
de implementación que puedes cambiar sin romper contratos externos.

### Module attributes: `@attr`

Los module attributes son valores de **compile time**. Se evalúan cuando el
módulo se compila, no cuando las funciones se ejecutan.

```elixir
defmodule Config do
  @version "1.0.0"
  @max_retries 3
  @default_timeout 5_000  # 5 segundos en milisegundos

  def version, do: @version
  def max_retries, do: @max_retries
  def timeout, do: @default_timeout
end

Config.version()    # => "1.0.0"
Config.max_retries  # => 3
```

Los attributes con nombres especiales tienen comportamiento específico:
- `@moduledoc` — documentación del módulo
- `@doc` — documentación de la función siguiente
- `@spec` — especificación de tipos (Dialyzer)
- `@behaviour` — declarar que el módulo implementa un behaviour

### Documentación con `@moduledoc` y `@doc`

```elixir
defmodule MathUtils do
  @moduledoc """
  Funciones matemáticas de uso general.

  Todas las funciones son puras — sin side effects.
  """

  @doc """
  Calcula la suma de una lista de números.

  ## Ejemplos

      iex> MathUtils.sum([1, 2, 3])
      6

      iex> MathUtils.sum([])
      0
  """
  def sum(numbers) do
    Enum.sum(numbers)
  end
end
```

En IEx puedes consultar la documentación:
```elixir
h MathUtils
h MathUtils.sum
```

### Módulos anidados

Elixir usa el punto para crear namespaces jerárquicos.

```elixir
defmodule MyApp.User do
  def new(name, email) do
    %{name: name, email: email}
  end
end

defmodule MyApp.User.Auth do
  def valid_password?(password) do
    String.length(password) >= 8
  end
end

MyApp.User.new("Alice", "alice@example.com")
MyApp.User.Auth.valid_password?("secret123")
```

Los módulos anidados son simples convenciones de nombres — en Elixir no hay
"módulos dentro de módulos" como en otros lenguajes OO. Son todos módulos
independientes con nombres que comparten un prefijo.

### `alias`: abreviar nombres

```elixir
defmodule MyApp.Reports do
  alias MyApp.User          # ahora puedes usar User en lugar de MyApp.User
  alias MyApp.User.Auth     # y Auth en lugar de MyApp.User.Auth

  def active_users(all_users) do
    Enum.filter(all_users, fn u ->
      Auth.valid_password?(u.password)  # en lugar de MyApp.User.Auth
    end)
  end
end
```

Con alias personalizado:
```elixir
alias MyApp.User, as: U
U.new("Alice", "alice@example.com")
```

### `import`: traer funciones al scope

```elixir
defmodule MyModule do
  import Enum, only: [map: 2, filter: 2]

  def process(list) do
    list
    |> filter(&(&1 > 0))   # sin el prefijo Enum.
    |> map(&(&1 * 2))
  end
end
```

Usar `import` con moderación — puede hacer el código ambiguo si dos módulos
exportan funciones con el mismo nombre.

---

## Exercises

### Ejercicio 1: Definir un módulo y llamarlo

```elixir
defmodule Greeting do
  def hello(name) do
    "Hello, #{name}!"
  end

  def hello do
    "Hello, World!"
  end
end

IO.puts(Greeting.hello("Alice"))
IO.puts(Greeting.hello())
```

**Expected output:**
```
Hello, Alice!
Hello, World!
```

---

### Ejercicio 2: Funciones públicas y privadas

```elixir
defmodule Formatter do
  @moduledoc "Formatea nombres y textos."

  # Pública: forma parte de la API
  def format_full_name(first, last) do
    "#{capitalize(first)} #{capitalize(last)}"
  end

  # Pública: forma parte de la API
  def format_initials(first, last) do
    "#{String.first(capitalize(first))}.#{String.first(capitalize(last))}."
  end

  # Privada: detalle de implementación
  defp capitalize(word) do
    String.capitalize(word)
  end
end

IO.puts(Formatter.format_full_name("alice", "smith"))
IO.puts(Formatter.format_initials("alice", "smith"))

# Esto fallaría con UndefinedFunctionError:
# Formatter.capitalize("alice")
```

**Expected output:**
```
Alice Smith
A.S.
```

---

### Ejercicio 3: Module attributes como constantes

```elixir
defmodule AppConfig do
  @version "2.1.0"
  @app_name "MyElixirApp"
  @max_connections 100

  def version, do: @version
  def app_name, do: @app_name
  def max_connections, do: @max_connections

  def banner do
    "#{@app_name} v#{@version} (max #{@max_connections} connections)"
  end
end

IO.puts(AppConfig.version())
IO.puts(AppConfig.app_name())
IO.inspect(AppConfig.max_connections())
IO.puts(AppConfig.banner())
```

**Expected output:**
```
2.1.0
MyElixirApp
100
MyElixirApp v2.1.0 (max 100 connections)
```

---

### Ejercicio 4: Documentación con `@doc` y `@moduledoc`

```elixir
defmodule MathUtils do
  @moduledoc """
  Funciones matemáticas de utilidad general.

  Todas las funciones son puras y sin side effects.
  """

  @doc """
  Retorna true si el número es par.

  ## Ejemplos

      iex> MathUtils.even?(4)
      true

      iex> MathUtils.even?(3)
      false
  """
  def even?(n) when is_integer(n) do
    rem(n, 2) == 0
  end

  @doc """
  Calcula el factorial de n.

  ## Ejemplos

      iex> MathUtils.factorial(5)
      120
  """
  def factorial(0), do: 1
  def factorial(n) when n > 0, do: n * factorial(n - 1)
end

IO.inspect(MathUtils.even?(4))
IO.inspect(MathUtils.even?(7))
IO.inspect(MathUtils.factorial(5))
IO.inspect(MathUtils.factorial(0))

# En IEx puedes ver la documentación con:
# h MathUtils
# h MathUtils.even?
```

**Expected output:**
```
true
false
120
1
```

---

### Ejercicio 5: Módulos anidados

```elixir
defmodule MyApp.User do
  @moduledoc "Representa un usuario del sistema."

  def new(name, email) when is_binary(name) and is_binary(email) do
    %{name: name, email: email, active: true}
  end

  def display(%{name: name, email: email}) do
    "#{name} <#{email}>"
  end
end

defmodule MyApp.User.Auth do
  @moduledoc "Autenticación de usuarios."

  @min_password_length 8

  def valid_password?(password) when is_binary(password) do
    String.length(password) >= @min_password_length
  end

  def password_strength(password) do
    cond do
      String.length(password) < 6  -> :weak
      String.length(password) < 12 -> :medium
      true                          -> :strong
    end
  end
end

user = MyApp.User.new("Alice", "alice@example.com")
IO.puts(MyApp.User.display(user))
IO.inspect(MyApp.User.Auth.valid_password?("secret"))
IO.inspect(MyApp.User.Auth.valid_password?("securepassword"))
IO.inspect(MyApp.User.Auth.password_strength("abc"))
IO.inspect(MyApp.User.Auth.password_strength("securepassword!"))
```

**Expected output:**
```
Alice <alice@example.com>
false
true
:weak
:strong
```

---

### Ejercicio 6: `alias` para simplificar nombres

```elixir
defmodule MyApp.Reports.UserReport do
  alias MyApp.User
  alias MyApp.User.Auth

  def generate(users) do
    Enum.map(users, fn u ->
      display = User.display(u)
      # En lugar de: MyApp.User.display(u)
      "#{display} — activo: #{u.active}"
    end)
  end
end

# Para probar en IEx necesitaríamos definir todo. Aquí lo simplificamos:
defmodule Demo.Alias do
  # alias crea un shorthand local al módulo
  alias String, as: S

  def process(text) do
    text
    |> S.trim()
    |> S.upcase()
    |> S.replace(" ", "_")
  end
end

IO.puts(Demo.Alias.process("  hello world  "))
```

**Expected output:**
```
HELLO_WORLD
```

---

## Common Mistakes

### Error 1: Llamar una función privada desde afuera del módulo

```elixir
# WRONG — defp no es accesible desde fuera
defmodule Calculator do
  def add(a, b), do: a + b
  defp multiply(a, b), do: a * b
end

Calculator.multiply(3, 4)
```

```
** (UndefinedFunctionError) function Calculator.multiply/2 is undefined or private
```

**Why**: `defp` es privada — solo puede ser llamada por otras funciones **dentro**
del mismo módulo. Es intencional: `defp` es un detalle de implementación.

**Fix**: Si necesitas `multiply` desde afuera, cámbiala a `def`.
Si no, úsala solo internamente:
```elixir
defmodule Calculator do
  def add(a, b), do: a + b
  def square(x), do: multiply(x, x)   # uso interno válido
  defp multiply(a, b), do: a * b
end
```

---

### Error 2: Confundir module attributes con variables de runtime

```elixir
# WRONG — pensar que @counter cambia en runtime
defmodule Counter do
  @count 0

  def increment do
    @count = @count + 1   # esto no compila
    @count
  end
end
```

```
** (CompileError) cannot invoke remote function @count/0 inside a match
```

**Why**: Los module attributes son valores de **compile time** — se evalúan
cuando el módulo se compila, no cuando se ejecutan las funciones. No son
variables mutables de runtime.

**Fix**: Para estado mutable en Elixir, usa procesos (GenServer, Agent).
Para constantes, usa attributes correctamente:
```elixir
defmodule Config do
  @default_count 0        # constante de compile time — correcto

  def default_count, do: @default_count  # retorna el valor

  # Para estado mutable: usa Agent o GenServer
end
```

---

### Error 3: El nombre del módulo es un Atom

```elixir
# El nombre del módulo en realidad es un atom especial
IO.inspect(MyApp.User == :"Elixir.MyApp.User")
```

```
true
```

**Why**: Todos los módulos en Elixir son atoms con el prefijo `"Elixir."`.
Esto raramente importa en código normal, pero puede sorprender al usar
`String.to_atom/1` o al trabajar con módulos dinámicamente.

```elixir
# Puedes crear módulos dinámicamente con Module.concat
mod = Module.concat(MyApp, User)
IO.inspect(mod)
# => MyApp.User
```

---

### Error 4: `import` demasiado amplio genera ambigüedad

```elixir
# WRONG — importar todo puede causar conflictos
defmodule MyModule do
  import List         # importa ALL de List
  import Enum         # importa ALL de Enum

  # ¿flatten viene de List o de Enum?
  def process(nested), do: flatten(nested)
end
```

**Why**: Ambos módulos tienen `flatten/1`. El compilador puede advertir o
comportarse de forma inesperada.

**Fix**: Importa solo lo que necesitas:
```elixir
defmodule MyModule do
  import List, only: [flatten: 1]

  def process(nested), do: flatten(nested)
end
```

---

## Verification

```bash
iex
```

```elixir
# Definir y usar un módulo básico
defmodule Demo do
  def greet(name), do: "Hello, #{name}!"
  defp secret, do: "no accesible desde afuera"

  def show_secret, do: secret()
end

Demo.greet("Alice")
# => "Hello, Alice!"

Demo.show_secret()
# => "no accesible desde afuera"

# Demo.secret()   # UndefinedFunctionError

# Module attributes
defmodule V do
  @version "1.0"
  def v, do: @version
end

V.v()
# => "1.0"

# El módulo es un atom
Demo == :"Elixir.Demo"
# => true
```

---

## Summary

- `defmodule Name do ... end` agrupa funciones relacionadas en un namespace.
- `def` = función pública; `defp` = función privada al módulo.
- Los module attributes (`@attr`) son valores de compile time — no son variables.
- `@moduledoc` y `@doc` documentan módulos y funciones, accesibles con `h` en IEx.
- Los módulos anidados (`MyApp.User.Auth`) son convenciones de nombre, no herencia.
- `alias` crea un shorthand local para módulos con nombres largos.
- `import` trae funciones al scope sin el prefijo del módulo — usar con moderación.
- El nombre del módulo es un atom: `MyApp.User == :"Elixir.MyApp.User"`.

---

## What's Next

- **Ejercicio 15**: Structs — data containers tipados definidos dentro de módulos
- Behaviours y callbacks — contratos entre módulos
- Protocols — polimorfismo en Elixir

---

## Resources

- [Modules — Elixir Getting Started](https://elixir-lang.org/getting-started/modules-and-functions.html)
- [Module attributes — Elixir docs](https://elixir-lang.org/getting-started/module-attributes.html)
- [Alias, require, import — Elixir docs](https://elixir-lang.org/getting-started/alias-require-and-import.html)
- [ExDoc — generate documentation](https://github.com/elixir-lang/ex_doc)
