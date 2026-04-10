# 1. Setup and Mix

**Difficulty**: Basico

## Prerequisites
- Terminal / línea de comandos básica
- Ningún conocimiento previo de Elixir requerido

## Learning Objectives
After completing this exercise, you will be able to:
- Instalar Elixir y verificar que el entorno funciona correctamente
- Crear un proyecto nuevo con Mix y entender su estructura
- Compilar el proyecto y ejecutar código en IEx (shell interactivo)
- Gestionar dependencias con `mix deps.get`
- Navegar los comandos disponibles con `mix help`

## Concepts

### Mix: El Build Tool de Elixir
Mix es la herramienta oficial de construcción y gestión de proyectos en Elixir. Cumple un rol similar a `cargo` en Rust, `npm` en Node.js, o `gradle` en Java. Con un solo comando puedes crear proyectos, compilar código, ejecutar tests, formatear archivos y gestionar dependencias externas.

Mix no es una herramienta opcional — es parte del ecosistema central de Elixir. Todo proyecto real en Elixir se crea y gestiona con Mix. Entender Mix desde el principio te ahorrará confusión más adelante.

```elixir
# mix.exs es el archivo de configuración del proyecto
# Define el nombre, versión, y dependencias del proyecto
defmodule HelloElixir.MixProject do
  use Mix.Project

  def project do
    [
      app: :hello_elixir,
      version: "0.1.0",
      elixir: "~> 1.17",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [extra_applications: [:logger]]
  end

  defp deps do
    []
  end
end
```

### Estructura de un Proyecto Mix
Cuando ejecutas `mix new nombre_proyecto`, Mix genera una estructura estándar que todos los proyectos Elixir comparten. Esta convención sobre configuración permite a cualquier desarrollador Elixir orientarse rápidamente en cualquier proyecto.

```
hello_elixir/
├── lib/
│   └── hello_elixir.ex    # Código fuente principal
├── test/
│   ├── hello_elixir_test.exs  # Tests
│   └── test_helper.exs        # Configuración de tests
├── .formatter.exs         # Configuración del formateador
├── .gitignore
└── mix.exs                # Configuración del proyecto (dependencias, versión, etc.)
```

El directorio `lib/` contiene todo el código fuente. El directorio `test/` contiene los tests. Mix usa la extensión `.ex` para archivos compilados y `.exs` para scripts (incluyendo tests y configuración).

### IEx: El Shell Interactivo
IEx (Interactive Elixir) es el REPL de Elixir. Es una herramienta indispensable para explorar código, probar funciones, y depurar. La diferencia crítica entre `iex` e `iex -S mix` es que la segunda carga tu proyecto completo, haciendo disponibles todos tus módulos.

```elixir
# En IEx puedes evaluar cualquier expresión Elixir
iex> 1 + 1
2

iex> "Hello, " <> "Elixir!"
"Hello, Elixir!"

iex> Enum.map([1, 2, 3], fn x -> x * 2 end)
[2, 4, 6]

# Dentro de iex -S mix, tus módulos están disponibles
iex> HelloElixir.hello()
:world
```

### Comandos Mix Esenciales
Mix expone una colección de tasks. Cada task es una operación específica — compilar, testear, formatear, etc. Puedes incluso crear tus propias Mix tasks para automatizar tareas del proyecto.

```elixir
# mix new: crea un proyecto nuevo
# mix compile: compila el proyecto (también lo hace mix automaticamente cuando es necesario)
# mix test: ejecuta el suite de tests
# mix format: formatea el código según las reglas de Elixir
# mix deps.get: descarga las dependencias definidas en mix.exs
# mix deps.update --all: actualiza todas las dependencias
# mix help: lista todos los comandos disponibles
# mix help <task>: muestra documentación detallada de un task específico
```

## Exercises

### Exercise 1: Install Elixir and Verify the Environment
Antes de crear proyectos necesitas confirmar que Elixir está instalado correctamente y que las tres herramientas principales están disponibles: `elixir`, `mix`, e `iex`.

```bash
# Verificar versión de Elixir (runtime y compilador)
$ elixir --version

# Output esperado (versiones exactas pueden variar):
# Erlang/OTP 27 [erts-15.0] [source] [64-bit] [smp:8:8] [async-threads:1]
# Elixir 1.17.0 (compiled with Erlang/OTP 27)

# Verificar Mix
$ mix --version

# Output esperado:
# Mix 1.17.0 (compiled with Erlang/OTP 27)

# Verificar IEx
$ iex --version

# Output esperado:
# IEx 1.17.0 (compiled with Erlang/OTP 27)
```

Si alguno de los comandos falla con `command not found`, instala Elixir siguiendo la guía oficial en https://elixir-lang.org/install.html. Se recomienda usar `asdf` o `mise` para gestionar versiones de Elixir en el mismo sistema.

Expected output:
```
Erlang/OTP 27 [erts-15.0] [source] [64-bit]
Elixir 1.17.0 (compiled with Erlang/OTP 27)
```

### Exercise 2: Create a New Project with Mix
`mix new` crea la estructura completa de un proyecto Elixir. El nombre del proyecto en Snake Case se convierte automáticamente en el nombre del módulo principal en CamelCase.

```bash
# Crear el proyecto en el directorio actual
$ mix new hello_elixir

# Output de mix new:
# * creating README.md
# * creating .formatter.exs
# * creating .gitignore
# * creating mix.exs
# * creating lib/
# * creating lib/hello_elixir.ex
# * creating test/
# * creating test/test_helper.exs
# * creating test/hello_elixir_test.exs
#
# Your Mix project was created successfully.
# You can use "mix" to compile it, test it, and more:
#
#     cd hello_elixir
#     mix test

# Entrar al directorio del proyecto
$ cd hello_elixir

# Explorar la estructura generada
$ ls -la
```

Abre `lib/hello_elixir.ex` en tu editor. Verás el módulo generado automáticamente:

```elixir
defmodule HelloElixir do
  @moduledoc """
  Documentation for `HelloElixir`.
  """

  @doc """
  Hello world.

  ## Examples

      iex> HelloElixir.hello()
      :world

  """
  def hello do
    :world
  end
end
```

Expected output:
```
* creating README.md
* creating .formatter.exs
* creating .gitignore
* creating mix.exs
* creating lib/hello_elixir.ex
* creating test/hello_elixir_test.exs
```

### Exercise 3: Compile and Run in IEx
Con el proyecto creado, compílalo y ábrelo en IEx para llamar a tu primera función Elixir.

```bash
# Compilar el proyecto (desde dentro del directorio hello_elixir)
$ mix compile

# Output:
# Compiling 1 file (.ex)
# Generated hello_elixir app

# Abrir IEx con el proyecto cargado
$ iex -S mix
```

```elixir
# Dentro de IEx, el módulo HelloElixir está disponible
iex> HelloElixir.hello()
:world

# También puedes inspeccionar el módulo
iex> h HelloElixir
# Muestra la documentación del módulo

iex> h HelloElixir.hello
# Muestra la documentación de la función hello/0
```

Expected output:
```
iex> HelloElixir.hello()
:world
```

### Exercise 4: Modify the Greeting and Recompile
Edita el módulo para que `hello/0` retorne un string en lugar del atom `:world`. Luego recarga el módulo en IEx sin salir.

```elixir
# Edita lib/hello_elixir.ex y cambia la función hello:
defmodule HelloElixir do
  @moduledoc """
  Mi primer módulo Elixir.
  """

  @doc """
  Retorna un saludo personalizado.

  ## Examples

      iex> HelloElixir.hello()
      "Hello, Elixir World!"

  """
  def hello do
    "Hello, Elixir World!"
  end
end
```

```elixir
# Dentro de IEx, recompila sin salir del shell
iex> recompile()
# Output: Compiling 1 file (.ex)
# :ok

# Ahora la función retorna el string nuevo
iex> HelloElixir.hello()
"Hello, Elixir World!"
```

`recompile()` es una función helper de IEx que recompila todos los archivos modificados y recarga los módulos. Es más rápido que salir de IEx y volver a entrar.

Expected output:
```
iex> recompile()
Compiling 1 file (.ex)
:ok
iex> HelloElixir.hello()
"Hello, Elixir World!"
```

### Exercise 5: Explore Mix Help
Mix tiene muchos comandos disponibles. Aprende a descubrirlos sin salir del terminal.

```bash
# Listar todos los tasks disponibles
$ mix help

# Output (fragmento):
# mix                   # Runs the default task (current: "mix run")
# mix app.config        # Reads and validates an application's config
# mix app.start         # Starts all registered apps
# mix app.tree          # Prints the application tree
# mix archive           # Lists installed archives
# mix clean             # Deletes generated application files
# mix compile           # Compiles source files
# mix deps              # Lists dependencies and their status
# mix deps.clean        # Deletes the given dependencies' files
# mix deps.compile      # Compiles dependencies
# mix deps.get          # Gets all out of date dependencies
# mix deps.tree         # Prints the dependency tree
# mix deps.unlock       # Unlocks the given dependencies
# mix deps.update       # Updates the given dependencies
# mix format            # Formats the given files/patterns
# mix help              # Prints help information for tasks
# mix test              # Runs a project's tests
# ...

# Ver documentación detallada de un task específico
$ mix help test

# Ejecutar los tests del proyecto
$ mix test
```

```bash
# Output de mix test con el proyecto recién creado:
# ..
# Finished in 0.03 seconds (0.03s on load, 0.00s async, 0.00s sync)
# 1 doctest, 1 test, 0 failures
```

Expected output:
```
..
Finished in 0.03 seconds
1 doctest, 1 test, 0 failures
```

### Exercise 6: Add Module Documentation with @moduledoc
Elixir tiene documentación de primera clase integrada en el lenguaje. Agrega documentación real a tu módulo usando los atributos `@moduledoc` y `@doc`.

```elixir
# lib/hello_elixir.ex — versión con documentación completa
defmodule HelloElixir do
  @moduledoc """
  HelloElixir es el módulo principal de mi primer proyecto Elixir.

  Contiene funciones de demostración para aprender los conceptos
  básicos del lenguaje.
  """

  @doc """
  Retorna un saludo de bienvenida.

  ## Examples

      iex> HelloElixir.hello()
      "Hello, Elixir World!"

  """
  def hello do
    "Hello, Elixir World!"
  end

  @doc """
  Retorna un saludo personalizado con el nombre dado.

  ## Parameters
  - name: String con el nombre a saludar

  ## Examples

      iex> HelloElixir.greet("Alice")
      "Hello, Alice! Welcome to Elixir."

  """
  def greet(name) do
    "Hello, #{name}! Welcome to Elixir."
  end
end
```

```bash
# Dentro de IEx, la documentación es accesible con h/1
$ iex -S mix
```

```elixir
iex> recompile()
:ok

iex> HelloElixir.greet("Alice")
"Hello, Alice! Welcome to Elixir."

iex> h HelloElixir
# Muestra el @moduledoc formateado

iex> h HelloElixir.greet
# Muestra el @doc de la función greet/1
```

Expected output:
```
iex> HelloElixir.greet("Alice")
"Hello, Alice! Welcome to Elixir."
```

## Common Mistakes

### Mistake 1: Olvidar mix deps.get después de editar mix.exs
**Wrong:**
```bash
# Agregas una dependencia a mix.exs y vas directo a compilar
$ mix compile
```
**Error:** `** (Mix) No such file or directory "deps/jason"`
**Why:** Mix no descarga dependencias automáticamente. Necesitas ejecutar `mix deps.get` cada vez que agregas o cambias dependencias en `mix.exs`.
**Fix:**
```bash
# Siempre ejecuta deps.get después de modificar mix.exs
$ mix deps.get
$ mix compile
```

### Mistake 2: Usar iex sin -S mix
**Wrong:**
```bash
# Abrir IEx simple, sin cargar el proyecto
$ iex
```
```elixir
iex> HelloElixir.hello()
# ** (UndefinedFunctionError) function HelloElixir.hello/0 is undefined
# (module HelloElixir is not available)
```
**Error:** `UndefinedFunctionError` — el módulo no está disponible porque el proyecto no fue cargado.
**Why:** `iex` abre una sesión Elixir vacía sin ningún proyecto. `iex -S mix` compila y carga tu proyecto antes de abrir la sesión interactiva.
**Fix:**
```bash
# Siempre usar iex -S mix para trabajar con tu proyecto
$ iex -S mix
```

### Mistake 3: No recompilar después de editar código en IEx
**Wrong:**
```elixir
# Editas lib/hello_elixir.ex pero no recargas en IEx
iex> HelloElixir.hello()
"Hello, Elixir World!"  # Retorna el valor anterior, no el nuevo
```
**Error:** No hay error, pero los cambios no se reflejan — IEx mantiene la versión compilada anterior en memoria.
**Why:** IEx carga los módulos compilados al iniciar. Los cambios en archivos fuente no se propagan automáticamente.
**Fix:**
```elixir
# Recompila dentro de IEx para recargar los módulos modificados
iex> recompile()
Compiling 1 file (.ex)
:ok
iex> HelloElixir.hello()
"Nuevo valor"
```

## Verification
```bash
$ cd hello_elixir
$ iex -S mix
iex> HelloElixir.hello()
"Hello, Elixir World!"
iex> HelloElixir.greet("Alice")
"Hello, Alice! Welcome to Elixir."
```

```bash
$ mix test
..
Finished in 0.03 seconds
1 doctest, 1 test, 0 failures
```

## Summary
- **Key concepts**: Mix como build tool, estructura de proyecto, IEx como REPL, `@moduledoc` y `@doc`
- **What you practiced**: Crear proyecto con `mix new`, compilar con `mix compile`, explorar en `iex -S mix`, recompilar con `recompile()`, gestionar dependencias con `mix deps.get`
- **Important to remember**: `iex -S mix` carga tu proyecto; `iex` no. Usa `recompile()` en IEx para recargar cambios sin reiniciar la sesión.

## What's Next
En el siguiente ejercicio **02-atoms-and-symbols** aprenderás sobre atoms — los identificadores únicos e inmutables de Elixir, y el patrón `{:ok, value}` / `{:error, reason}` que verás en todo el ecosistema.

## Resources
- [The Elixir Getting Started Guide](https://elixir-lang.org/getting-started)
- [Mix Documentation](https://hexdocs.pm/mix/Mix.html)
- [IEx Documentation](https://hexdocs.pm/iex/IEx.html)
- [Installing Elixir](https://elixir-lang.org/install.html)
