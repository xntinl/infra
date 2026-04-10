# 18. Mix Tasks Personalizadas

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento básico de módulos y behaviours en Elixir
- Familiaridad con Mix y la estructura de proyectos
- Pattern matching con listas y keyword lists

## Learning Objectives
After completing this exercise, you will be able to:
- Crear Mix tasks propias implementando el behaviour `Mix.Task`
- Usar `@shortdoc` y `@moduledoc` para documentar tasks
- Parsear argumentos de línea de comandos con `OptionParser`
- Invocar otras Mix tasks desde una task propia
- Organizar tasks relacionadas en namespaces

## Concepts

### Mix.Task: El Behaviour de las Tasks

Una Mix task es un módulo que implementa el behaviour `Mix.Task`. Solo requiere una función: `run/1`, que recibe la lista de argumentos de línea de comandos como strings.

El nombre de la task se deriva del nombre del módulo: `Mix.Tasks.Hello` → `mix hello`, `Mix.Tasks.Db.Seed` → `mix db.seed`.

```elixir
defmodule Mix.Tasks.Hello do
  use Mix.Task

  @shortdoc "Saluda al mundo"
  @moduledoc """
  Task de ejemplo que imprime un saludo.

  ## Usage

      mix hello
      mix hello --name Alice

  """

  @impl Mix.Task
  def run(args) do
    Mix.shell().info("Hello, World!")
    Mix.shell().info("Args received: #{inspect(args)}")
  end
end
```

```bash
$ mix hello
Hello, World!
Args received: []

$ mix hello --name Alice
Hello, World!
Args received: ["--name", "Alice"]
```

### Mix.shell() — I/O de Tasks

Las tasks usan `Mix.shell()` en lugar de `IO.puts` para output. Esto permite que el shell sea reemplazado en tests y mantiene consistencia con el ecosistema Mix.

```elixir
# Mensajes informativos (stdout normal)
Mix.shell().info("Operation completed successfully")

# Mensajes de error (stderr)
Mix.shell().error("Something went wrong: #{reason}")

# Preguntas interactivas al usuario
if Mix.shell().yes?("Proceed? [y/n]") do
  do_the_thing()
end

# Ejecutar un comando de sistema y mostrar output
Mix.shell().cmd("ls -la")
```

### OptionParser — Parseo de Argumentos

`OptionParser.parse/2` convierte la lista de strings de `run/1` en opciones estructuradas. Soporta flags booleanos, opciones con valor, y argumentos posicionales.

```elixir
def run(args) do
  {opts, remaining_args, _invalid} =
    OptionParser.parse(args,
      switches: [
        name: :string,      # --name Alice
        count: :integer,    # --count 5
        verbose: :boolean,  # --verbose (flag)
        output: :string     # --output report.txt
      ],
      aliases: [
        n: :name,    # -n Alice (shorthand)
        v: :verbose  # -v (shorthand)
      ]
    )

  # opts es una keyword list con las opciones parseadas
  name = Keyword.get(opts, :name, "World")  # default "World"
  verbose = Keyword.get(opts, :verbose, false)

  if verbose do
    Mix.shell().info("Running in verbose mode")
  end

  Mix.shell().info("Hello, #{name}!")
end
```

```bash
$ mix greet --name Alice --verbose
Running in verbose mode
Hello, Alice!

$ mix greet -n Bob -v
Running in verbose mode
Hello, Bob!

$ mix greet
Hello, World!
```

### Mix.Task.run/2 — Invocar Otras Tasks

Una task puede invocar otras tasks como dependencias. Esto es útil para encadenar operaciones o crear tareas de alto nivel que coordinan otras.

```elixir
defmodule Mix.Tasks.Setup do
  use Mix.Task

  @shortdoc "Configura el proyecto completo para desarrollo"

  @impl Mix.Task
  def run(_args) do
    Mix.shell().info("Setting up project...")

    # Invocar otras tasks en secuencia
    Mix.Task.run("deps.get", [])
    Mix.Task.run("compile", [])
    Mix.Task.run("ecto.create", [])
    Mix.Task.run("ecto.migrate", [])

    Mix.shell().info("Setup complete!")
  end
end
```

### Namespaced Tasks

Cuando tienes múltiples tasks relacionadas, agrúpalas bajo un namespace usando módulos anidados. El punto en el nombre del módulo se convierte en punto en el comando Mix.

```elixir
# mix db.seed
defmodule Mix.Tasks.Db.Seed do
  use Mix.Task
  @shortdoc "Inserta datos iniciales en la base de datos"
  def run(_), do: Mix.shell().info("Seeding database...")
end

# mix db.reset
defmodule Mix.Tasks.Db.Reset do
  use Mix.Task
  @shortdoc "Borra y recrea la base de datos"
  def run(_), do: Mix.shell().info("Resetting database...")
end

# mix db.stats
defmodule Mix.Tasks.Db.Stats do
  use Mix.Task
  @shortdoc "Muestra estadísticas de la base de datos"
  def run(_), do: Mix.shell().info("Showing stats...")
end
```

```bash
$ mix help | grep "db\."
mix db.reset      # Borra y recrea la base de datos
mix db.seed       # Inserta datos iniciales en la base de datos
mix db.stats      # Muestra estadísticas de la base de datos
```

## Exercises

### Exercise 1: Task Simple

Crea la Mix task más básica posible: un saludo desde la línea de comandos.

```elixir
# lib/mix/tasks/hello.ex
# TODO: Define el módulo Mix.Tasks.Hello
# TODO: Agrega `use Mix.Task`
# TODO: Define @shortdoc "Prints a greeting message"
# TODO: Implementa @impl Mix.Task
# TODO: Implementa run/1 que:
#   - Imprime "Hello from Mix!" con Mix.shell().info/1
#   - Imprime "Project: #{Mix.Project.config()[:app]}" para mostrar el nombre del proyecto
#   - Si recibe args, los imprime: "Arguments: #{inspect(args)}"

defmodule Mix.Tasks.Hello do
  # Tu implementación aquí
end
```

```bash
# Verificar que aparece en mix help
$ mix help | grep hello
mix hello         # Prints a greeting message

# Ejecutar la task
$ mix hello
Hello from Mix!
Project: my_app
Arguments: []

# Ejecutar con argumentos
$ mix hello foo bar
Hello from Mix!
Project: my_app
Arguments: ["foo", "bar"]
```

Expected output:
```bash
$ mix hello
Hello from Mix!
Project: my_app
Arguments: []
```

---

### Exercise 2: Task con Argumentos Nombrados

Crea una task que personaliza su comportamiento según los argumentos recibidos.

```elixir
# lib/mix/tasks/greet.ex
defmodule Mix.Tasks.Greet do
  use Mix.Task

  @shortdoc "Greets a person with a custom message"
  @moduledoc """
  Imprime un saludo personalizado.

  ## Usage

      mix greet --name Alice
      mix greet --name Bob --greeting Hola
      mix greet --name Charlie --greeting Bonjour --repeat 3

  ## Options

    * `--name` - Nombre de la persona (requerido)
    * `--greeting` - Saludo personalizado (default: "Hello")
    * `--repeat` - Número de veces a repetir (default: 1)

  """

  @impl Mix.Task
  def run(args) do
    # TODO: Usa OptionParser.parse/2 para parsear:
    # - --name como :string
    # - --greeting como :string
    # - --repeat como :integer

    # TODO: Extrae name del resultado (requerido)
    # Si name es nil, usa Mix.shell().error/1 para mostrar error y salir

    # TODO: Extrae greeting con default "Hello"
    # TODO: Extrae repeat con default 1

    # TODO: Usa Enum.each(1..repeat, ...) para imprimir el saludo N veces
    # Formato: "#{greeting}, #{name}!"
  end
end
```

Expected output:
```bash
$ mix greet --name Alice
Hello, Alice!

$ mix greet --name Bob --greeting Hola
Hola, Bob!

$ mix greet --name Charlie --greeting Hey --repeat 3
Hey, Charlie!
Hey, Charlie!
Hey, Charlie!

$ mix greet
Error: --name is required. Usage: mix greet --name <name>
```

---

### Exercise 3: OptionParser con Múltiples Opciones

Crea una task de generación de reportes que acepta múltiples flags de configuración.

```elixir
# lib/mix/tasks/report.ex
defmodule Mix.Tasks.Report do
  use Mix.Task

  @shortdoc "Generates a project stats report"
  @moduledoc """
  Genera un reporte de estadísticas del proyecto.

  ## Usage

      mix report
      mix report --verbose
      mix report --output report.txt
      mix report --format json --output data.json

  ## Options

    * `--verbose` / `-v` - Mostrar información detallada
    * `--output` / `-o` - Archivo de salida (default: stdout)
    * `--format` - Formato de salida: text | json (default: text)

  """

  @impl Mix.Task
  def run(args) do
    # TODO: Parsea los args con OptionParser.parse/2
    # Switches: verbose: :boolean, output: :string, format: :string
    # Aliases: v: :verbose, o: :output

    # TODO: Extrae cada opción con defaults apropiados

    # TODO: Si verbose, imprime "Generating report in #{format} format..."

    # TODO: Genera datos del reporte (puedes usar valores hardcodeados):
    # stats = %{
    #   total_modules: 5,
    #   total_functions: 23,
    #   test_count: 18,
    #   line_count: 450
    # }

    # TODO: Si format == "json", usa Jason.encode!/1 o simplemente inspect/1
    #       Si format == "text", formatea como tabla simple

    # TODO: Si output está definido, escribe a archivo con File.write!/2
    #       Si output es nil, imprime con Mix.shell().info/1
  end
end
```

Expected output:
```bash
$ mix report
=== Project Report ===
Total Modules: 5
Total Functions: 23
Test Count: 18
Line Count: 450

$ mix report --verbose --format json
Generating report in json format...
{"line_count":450,"test_count":18,"total_functions":23,"total_modules":5}

$ mix report --output report.txt
# Crea el archivo report.txt con el reporte
Report written to report.txt
```

---

### Exercise 4: Task que Invoca Otras Tasks

Crea una task `mix setup` que orqueste múltiples tasks en secuencia.

```elixir
# lib/mix/tasks/setup.ex
defmodule Mix.Tasks.Setup do
  use Mix.Task

  @shortdoc "Configures the project for development"
  @moduledoc """
  Configura el entorno de desarrollo completo.

  Ejecuta en orden:
  1. deps.get — descarga dependencias
  2. compile — compila el proyecto
  3. format — formatea el código

  ## Usage

      mix setup
      mix setup --skip-format

  """

  @impl Mix.Task
  def run(args) do
    # TODO: Parsea --skip-format como boolean flag

    Mix.shell().info("=== Project Setup ===")

    # TODO: Paso 1 — Ejecuta Mix.Task.run("deps.get", [])
    # Imprime "Step 1/3: Getting dependencies..." antes de ejecutar

    # TODO: Paso 2 — Ejecuta Mix.Task.run("compile", [])
    # Imprime "Step 2/3: Compiling project..." antes de ejecutar

    # TODO: Paso 3 — Si NO skip_format, ejecuta Mix.Task.run("format", [])
    # Imprime "Step 3/3: Formatting code..." antes de ejecutar
    # Si skip_format es true, imprime "Step 3/3: Skipping format (--skip-format)"

    Mix.shell().info("=== Setup complete! ===")
  end
end
```

Expected output:
```bash
$ mix setup
=== Project Setup ===
Step 1/3: Getting dependencies...
# output de deps.get...
Step 2/3: Compiling project...
# output de compile...
Step 3/3: Formatting code...
# output de format...
=== Setup complete! ===

$ mix setup --skip-format
=== Project Setup ===
Step 1/3: Getting dependencies...
Step 2/3: Compiling project...
Step 3/3: Skipping format (--skip-format)
=== Setup complete! ===
```

---

### Exercise 5: Namespaced Tasks — Db Namespace

Crea un conjunto de tasks relacionadas bajo el namespace `db`.

```elixir
# lib/mix/tasks/db/seed.ex
defmodule Mix.Tasks.Db.Seed do
  use Mix.Task

  @shortdoc "Seeds the database with initial data"

  @impl Mix.Task
  def run(_args) do
    # TODO: Imprime "Seeding database..."
    # TODO: Simula insertar 3 registros con un Enum.each sobre una lista de nombres
    # Para cada nombre, imprime "  Inserted user: #{name}"
    # TODO: Imprime "Done. 3 records inserted."
  end
end

# lib/mix/tasks/db/reset.ex
defmodule Mix.Tasks.Db.Reset do
  use Mix.Task

  @shortdoc "Drops and recreates the database"

  @impl Mix.Task
  def run(_args) do
    # TODO: Usa Mix.shell().yes?/1 para confirmar: "This will DELETE all data. Continue? [y/n]"
    # Si el usuario dice sí:
    #   - Imprime "Dropping database..."
    #   - Imprime "Creating database..."
    #   - Invoca Mix.Task.run("db.seed", []) para poblar con datos iniciales
    # Si dice no:
    #   - Imprime "Aborted."
  end
end

# lib/mix/tasks/db/stats.ex
defmodule Mix.Tasks.Db.Stats do
  use Mix.Task

  @shortdoc "Shows database statistics"

  @impl Mix.Task
  def run(_args) do
    # TODO: Imprime una tabla de estadísticas simuladas:
    # === Database Stats ===
    # Users: 1,234
    # Orders: 5,678
    # Products: 89
    # Total records: 7,001
  end
end
```

Expected output:
```bash
$ mix help | grep "db\."
mix db.reset       # Drops and recreates the database
mix db.seed        # Seeds the database with initial data
mix db.stats       # Shows database statistics

$ mix db.seed
Seeding database...
  Inserted user: Alice
  Inserted user: Bob
  Inserted user: Charlie
Done. 3 records inserted.

$ mix db.stats
=== Database Stats ===
Users: 1,234
Orders: 5,678
Products: 89
Total records: 7,001
```

---

## Try It Yourself

Crea la task `mix report --from 2024-01-01 --to 2024-12-31` que genera un reporte de actividad simulado. Sin solución incluida.

```elixir
# lib/mix/tasks/report/generate.ex
defmodule Mix.Tasks.Report.Generate do
  use Mix.Task

  @shortdoc "Generates an activity report for a date range"
  @moduledoc """
  Genera un reporte de actividad para un rango de fechas.

  ## Usage

      mix report.generate --from 2024-01-01 --to 2024-12-31
      mix report.generate --from 2024-06-01 --to 2024-06-30 --format csv
      mix report.generate --from 2024-01-01 --to 2024-12-31 --output annual.txt

  ## Options

    * `--from` - Fecha de inicio (YYYY-MM-DD, requerido)
    * `--to` - Fecha de fin (YYYY-MM-DD, requerido)
    * `--format` - Formato de salida: text | csv (default: text)
    * `--output` - Archivo de salida (default: stdout)

  """

  @impl Mix.Task
  def run(args) do
    # Diseña e implementa:
    # 1. OptionParser para --from, --to (strings requeridos), --format, --output
    # 2. Validación de que --from y --to están presentes
    # 3. Validación de formato de fecha (básica: que contenga "-")
    # 4. Generación de datos simulados basados en el rango:
    #    - Número de días en el rango
    #    - Eventos simulados (usa :rand.uniform/1)
    # 5. Formateo: texto normal o CSV
    # 6. Output: stdout o archivo
    raise "Not implemented"
  end
end
```

**Objetivo**: La task debe validar inputs, generar datos simulados coherentes con el rango de fechas, y soportar los dos formatos de salida.

---

## Common Mistakes

### Mistake 1: Usar IO.puts en lugar de Mix.shell().info/1

**Wrong:**
```elixir
def run(_args) do
  IO.puts("Task complete!")   # No usa el shell de Mix
end
```
**Why:** `IO.puts` siempre escribe a stdout. `Mix.shell()` puede ser reemplazado en tests por un shell que captura output, lo que hace las tasks más testeables.
**Fix:**
```elixir
def run(_args) do
  Mix.shell().info("Task complete!")  # Testeable y consistente con Mix
end
```

### Mistake 2: Nombre de módulo incorrecto para el namespace

**Wrong:**
```elixir
# Queremos: mix db.seed
defmodule Mix.Tasks.DbSeed do  # Incorrecto — genera mix db_seed
  use Mix.Task
end
```
**Error:** `mix db.seed` no encuentra la task. `mix help` muestra `mix db_seed` (con underscore).
**Fix:**
```elixir
# El punto en el comando = módulo anidado
defmodule Mix.Tasks.Db.Seed do  # Correcto — genera mix db.seed
  use Mix.Task
end
```

### Mistake 3: Olvidar @shortdoc — la task no aparece en mix help

**Wrong:**
```elixir
defmodule Mix.Tasks.MyTask do
  use Mix.Task
  # Sin @shortdoc

  def run(_), do: Mix.shell().info("Done")
end
```
**Why:** Sin `@shortdoc`, la task no aparece en el listado de `mix help`. Solo es accesible si conoces el nombre exacto.
**Fix:**
```elixir
defmodule Mix.Tasks.MyTask do
  use Mix.Task
  @shortdoc "Does something useful"  # Aparece en mix help

  def run(_), do: Mix.shell().info("Done")
end
```

---

## Verification

```bash
# Verificar que las tasks aparecen en mix help
$ mix help | grep "hello\|greet\|db\."

# Ejecutar cada task
$ mix hello
$ mix greet --name Alice --greeting Hola
$ mix db.seed
$ mix db.stats

# Ver documentación de una task específica
$ mix help db.seed

# Verificar que mix test sigue pasando (las tasks no deben romper tests)
$ mix test
```

## Summary
- **Key concepts**: `Mix.Task` behaviour, `@shortdoc`, `@moduledoc`, `run/1`, `OptionParser.parse/2`, `Mix.shell().info/1`, `Mix.Task.run/2`, namespaces
- **What you practiced**: Crear tasks simples y con argumentos, parsear opciones de CLI, invocar tasks desde tasks, organizar en namespaces
- **Important to remember**: El nombre del módulo define el nombre del comando (`Mix.Tasks.Db.Seed` → `mix db.seed`). Siempre usa `Mix.shell()` en lugar de `IO`. Agrega `@shortdoc` para que aparezca en `mix help`.

## What's Next
En el siguiente ejercicio **19-dependencias-hex** aprenderás a gestionar dependencias externas con Hex — versioning, lockfiles, y dependencias solo para desarrollo.

## Resources
- [Mix.Task Documentation](https://hexdocs.pm/mix/Mix.Task.html)
- [OptionParser Documentation](https://hexdocs.pm/elixir/OptionParser.html)
- [Mix.Shell Documentation](https://hexdocs.pm/mix/Mix.Shell.html)
- [Writing Mix Tasks — Elixir School](https://elixirschool.com/en/lessons/intermediate/mix-tasks)
