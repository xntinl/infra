# 19. Dependencias con Hex y Mix

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de la estructura de un proyecto Mix
- Familiaridad con `mix.exs` y su función `deps/0`
- Comprensión básica de semantic versioning

## Learning Objectives
After completing this exercise, you will be able to:
- Agregar, actualizar y eliminar dependencias en `mix.exs`
- Entender las restricciones de versión: `~>`, `>=`, `==`
- Separar dependencias por ambiente con `only: :dev` y `only: :test`
- Usar y committear correctamente el `mix.lock`
- Referenciar dependencias locales con `path:` durante desarrollo

## Concepts

### Hex: El Package Manager de Elixir

Hex es el gestor de paquetes oficial del ecosistema Erlang/Elixir, similar a npm para Node.js o PyPI para Python. Cuando agregas una dependencia a `mix.exs`, Mix la descarga desde hex.pm.

```elixir
# mix.exs — sección deps
defp deps do
  [
    # {nombre_del_paquete, "restricción_de_versión"}
    {:jason, "~> 1.4"},
    {:req, "~> 0.5"},
    {:phoenix, "~> 1.7"}
  ]
end
```

```bash
# Descargar todas las dependencias definidas en mix.exs
$ mix deps.get

# Ver el estado de todas las dependencias
$ mix deps

# Actualizar una dependencia específica a la versión más reciente compatible
$ mix deps.update jason

# Actualizar todas las dependencias
$ mix deps.update --all

# Eliminar archivos compilados de las dependencias
$ mix deps.clean --all
```

### Restricciones de Versión

Elixir usa semantic versioning (MAJOR.MINOR.PATCH) y ofrece varios operadores para restringir qué versiones son aceptables.

```elixir
# ~> (optimistic) — La más común. Permite actualizaciones compatibles.
# ~> 1.4    equivale a  >= 1.4.0 and < 2.0.0
# ~> 1.4.2  equivale a  >= 1.4.2 and < 1.5.0

# La diferencia crítica entre ~> 1.4 y ~> 1.4.2:
{:jason, "~> 1.4"}    # Acepta 1.4.0, 1.5.0, 1.9.0, pero NO 2.0.0
{:jason, "~> 1.4.2"}  # Acepta 1.4.2, 1.4.9, pero NO 1.5.0

# >= — Mínimo, sin máximo. Raramente usado porque puede incluir breaking changes.
{:some_lib, ">= 1.0.0"}   # Acepta cualquier versión >= 1.0.0, incluyendo 2.0.0, 3.0.0

# == — Versión exacta. Solo para reproducibilidad máxima o durante debug.
{:my_dep, "== 1.4.3"}    # Solo acepta exactamente 1.4.3

# >= con < para rango explícito
{:some_dep, ">= 1.0.0 and < 2.0.0"}   # Equivalente manual a ~> 1.0
```

### Dependencias por Ambiente

No todas las dependencias necesitan estar disponibles en producción. Las herramientas de testing, análisis estático, y documentación deben ser solo para desarrollo.

```elixir
defp deps do
  [
    # Disponible en todos los ambientes (dev, test, prod)
    {:jason, "~> 1.4"},
    {:req, "~> 0.5"},

    # Solo en desarrollo — no se incluye en el release de producción
    {:dialyxir, "~> 1.0", only: :dev, runtime: false},
    {:ex_doc, "~> 0.31", only: :dev, runtime: false},

    # Solo en testing
    {:mock, "~> 0.3", only: :test},
    {:bypass, "~> 2.1", only: :test},

    # En desarrollo Y testing, pero no en producción
    {:faker, "~> 0.18", only: [:dev, :test]},

    # runtime: false — compilado en dev pero no iniciado como app en runtime
    # Úsalo con herramientas que solo se usan desde la línea de comandos
    {:credo, "~> 1.7", only: [:dev, :test], runtime: false}
  ]
end
```

### mix.lock — El Lockfile

`mix.lock` registra las versiones exactas resueltas de todas las dependencias (incluyendo las transitivas — las dependencias de tus dependencias). Es el equivalente de `package-lock.json` en npm o `Cargo.lock` en Rust.

```
# mix.lock (fragmento)
%{
  "jason": {:hex, :jason, "1.4.4",
    "b9226785a9aa77b6857ca22832cffa5d5150298a",
    [:mix], [{:decimal, "~> 1.0 or ~> 2.0", [hex: :decimal, optional: true]}],
    "hexpm", "..."},
  "req": {:hex, :req, "0.5.6",
    "...",
    ...},
}
```

**Regla crítica**: El `mix.lock` SIEMPRE debe committearse al repositorio. Sin el lockfile, distintos desarrolladores (o distintos servidores de CI) podrían resolver versiones diferentes de dependencias transitivas, causando bugs difíciles de reproducir.

```bash
# mix deps.get con lockfile — instala las versiones EXACTAS del lock
$ mix deps.get

# mix deps.update — actualiza y regenera el lock
$ mix deps.update jason

# Si el lock y mix.exs están desincronizados, Mix avisa
$ mix compile
# ** (Mix) Some dependencies failed to compile:
#   jason: lock file is out of date
```

### Dependencias Locales con path:

Durante el desarrollo de librerías que luego se publicarán en Hex, es útil referenciar una copia local en lugar de la versión publicada.

```elixir
defp deps do
  [
    # En desarrollo: usa la copia local
    {:my_shared_lib, path: "../my_shared_lib"},

    # En producción: usa la versión de Hex
    # {:my_shared_lib, "~> 1.2"},
  ]
end
```

```
# Estructura de directorios típica con dependencia local
projects/
├── my_app/
│   └── mix.exs  # {:my_shared_lib, path: "../my_shared_lib"}
└── my_shared_lib/
    └── mix.exs  # La librería que estás desarrollando
```

Las dependencias `path:` se recompilan automáticamente cuando cambias el código de la librería local — exactamente como si fueran parte de tu proyecto.

## Exercises

### Exercise 1: Agregar y Usar una Dependencia

Agrega `jason` (el encoder/decoder JSON más popular en Elixir) y úsalo en tu código.

```elixir
# mix.exs — ANTES
defp deps do
  []
end

# TODO: Modifica mix.exs para agregar {:jason, "~> 1.4"}
# mix.exs — DESPUÉS
defp deps do
  [
    # Agrega Jason aquí
  ]
end
```

```bash
# TODO: Ejecuta estos comandos después de modificar mix.exs
$ mix deps.get
# Resolving Hex dependencies...
# Dependency resolution completed...
# * Getting jason (Hex package)

$ mix compile
```

```elixir
# lib/json_example.ex
defmodule JsonExample do
  @doc """
  Convierte un mapa Elixir a JSON string.

  ## Examples

      iex> JsonExample.encode(%{name: "Alice", age: 30})
      {:ok, ~s({"age":30,"name":"Alice"})}

  """
  def encode(data) do
    # TODO: Usa Jason.encode/1 para convertir data a JSON
    # Retorna {:ok, json_string} o {:error, reason}
  end

  @doc """
  Convierte un JSON string a mapa Elixir.
  """
  def decode(json) do
    # TODO: Usa Jason.decode/1 para parsear el JSON
    # Retorna {:ok, map} o {:error, reason}
  end
end
```

Expected output:
```elixir
iex> JsonExample.encode(%{name: "Alice", age: 30})
{:ok, "{\"age\":30,\"name\":\"Alice\"}"}

iex> JsonExample.decode(~s({"name":"Alice","age":30}))
{:ok, %{"age" => 30, "name" => "Alice"}}
```

---

### Exercise 2: Restricciones de Versión — Entendiendo las Diferencias

Experimenta con las restricciones de versión en un proyecto limpio para entender qué versiones acepta cada una.

```elixir
# mix.exs — Experimenta con distintas restricciones
defp deps do
  [
    # Caso 1: ~> con dos dígitos (MAJOR.MINOR)
    {:plug, "~> 1.14"},
    # Pregunta: ¿Acepta 1.15.0? ¿Acepta 1.13.0? ¿Acepta 2.0.0?

    # Caso 2: ~> con tres dígitos (MAJOR.MINOR.PATCH)
    # {:plug, "~> 1.14.2"},
    # Pregunta: ¿Acepta 1.14.3? ¿Acepta 1.15.0? ¿Acepta 1.14.1?

    # Caso 3: >= sin límite superior
    # {:plug, ">= 1.0.0"},
    # ¿Por qué es peligroso en producción?

    # Caso 4: versión exacta
    # {:plug, "== 1.14.2"},
    # ¿Cuándo tiene sentido usar esto?
  ]
end
```

```bash
# TODO: Para cada restricción, ejecuta:
$ mix deps.get

# Verifica qué versión fue instalada mirando el lockfile:
$ cat mix.lock | grep plug
# "plug": {:hex, :plug, "1.15.3", ...}

# O usa mix deps para ver el estado:
$ mix deps
# * plug 1.15.3 (Hex package) [ok]
```

```elixir
# Resuelve mentalmente (y verifica con mix):
# ¿Cuál es la diferencia entre estas dos restricciones?

# A: {:phoenix, "~> 1.7"}
# B: {:phoenix, "~> 1.7.0"}

# Respuesta esperada:
# A (~> 1.7) acepta: 1.7.0, 1.7.x, 1.8.0, 1.9.0, ... pero NO 2.0.0
# B (~> 1.7.0) acepta: 1.7.0, 1.7.1, 1.7.x, ... pero NO 1.8.0 ni 2.0.0
# La regla: el último segmento incluido determina el "techo" del incremento permitido
```

Expected output:
```bash
$ mix deps
* jason 1.4.4 (Hex package) [ok]
* plug 1.15.3 (Hex package) [ok]
```

---

### Exercise 3: Dependencias Solo para Desarrollo

Configura correctamente las dependencias que no deben ir a producción.

```elixir
# mix.exs — Configura el proyecto con dependencias separadas por ambiente
defmodule MyProject.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_project,
      version: "0.1.0",
      elixir: "~> 1.14",
      deps: deps()
    ]
  end

  defp deps do
    [
      # Dependencias de producción (todos los ambientes)
      {:jason, "~> 1.4"},

      # TODO: Agrega Dialyxir solo para :dev con runtime: false
      # {:dialyxir, "~> 1.0", ...}

      # TODO: Agrega ExDoc solo para :dev con runtime: false
      # {:ex_doc, "~> 0.31", ...}

      # TODO: Agrega una dependencia de mocking solo para :test
      # Busca en hex.pm una librería de mocking para Elixir (ej: "mox")
      # {:mox, "~> 1.0", ...}

      # TODO: Agrega una dependencia para :dev y :test (lista de ambientes)
      # {:faker, "~> 0.18", ...}
    ]
  end
end
```

```bash
# TODO: Verifica que en el ambiente :prod las deps de dev no están disponibles
$ MIX_ENV=prod mix deps
# Las dependencias con only: :dev NO deben aparecer aquí

$ MIX_ENV=dev mix deps
# Las dependencias de dev SÍ aparecen aquí

# ¿Por qué runtime: false para dialyxir y ex_doc?
# Porque son herramientas de CLI — no necesitan iniciar como aplicaciones OTP
# runtime: false significa "compilar pero no arrancar como app en el runtime"
```

Expected output:
```bash
$ MIX_ENV=prod mix deps
* jason 1.4.4 (Hex package) [ok]
# Solo jason aparece — dialyxir, ex_doc, mox, faker NO aparecen en prod
```

---

### Exercise 4: El Lockfile en Detalle

Inspecciona y comprende el `mix.lock` para entender por qué es crítico para la reproducibilidad.

```bash
# Después de mix deps.get, inspecciona el lockfile generado
$ cat mix.lock
```

```elixir
# mix.lock tiene este formato (fragmento real)
%{
  "jason": {:hex, :jason, "1.4.4",
    "b9226785a9aa77b6857ca22832cffa5d5150298a",
    [:mix], [{:decimal, "~> 1.0 or ~> 2.0", [hex: :decimal, optional: true]}],
    "hexpm",
    "82ce2b7648c57d4f72c0b7e55abf2b9b4f43a6e7..."},
}
```

```elixir
# Cada entrada en mix.lock tiene:
# - Nombre del paquete: "jason"
# - Fuente: :hex (de hex.pm)
# - Nombre del package en hex: :jason
# - Versión exacta instalada: "1.4.4"
# - Hash SHA del tarball descargado (para verificación de integridad)
# - Dependencias del paquete (las transitivas)
# - Registro: "hexpm"
# - Hash del contenido del paquete

# TODO: Responde estas preguntas examinando tu mix.lock:
# 1. ¿Qué versión exacta de jason está instalada?
# 2. ¿Jason tiene alguna dependencia transitiva?
# 3. ¿Qué pasa si borras mix.lock y ejecutas mix deps.get?
#    (Pruébalo: mv mix.lock mix.lock.bak && mix deps.get)
#    ¿Obtienes las mismas versiones?

# TODO: Simula el escenario donde un colega no tiene mix.lock:
# rm mix.lock
# mix deps.get
# git diff mix.lock    # ¿Qué cambió? ¿Es lo mismo?
```

Expected output:
```bash
$ cat mix.lock
%{
  "jason": {:hex, :jason, "1.4.4",
    "b9226785a9aa77b6857ca22832cffa5d5150298a",
    [:mix], [{:decimal, "~> 1.0 or ~> 2.0", [hex: :decimal, optional: true]}],
    "hexpm", "..."},
}
# El lockfile garantiza que TODOS los desarrolladores instalan la misma versión exacta
```

---

### Exercise 5: Dependencia Local con path:

Simula el desarrollo de una librería compartida referenciándola localmente desde otro proyecto.

```bash
# Crea dos proyectos separados
$ mix new my_utils --module MyUtils
$ mix new my_app --module MyApp

# Estructura resultante:
# workspace/
# ├── my_utils/     ← La librería que desarrollas
# └── my_app/       ← La app que usa la librería
```

```elixir
# my_utils/lib/my_utils.ex
defmodule MyUtils do
  @doc "Formatea un número como moneda."
  def format_currency(amount, currency \\ "USD") do
    # TODO: Implementa formateo básico: "$1,234.56"
    # Hint: usa :erlang.float_to_binary(amount, decimals: 2)
    "#{currency} #{amount}"
  end

  @doc "Capitaliza cada palabra de un string."
  def title_case(str) do
    str
    |> String.split()
    |> Enum.map(&String.capitalize/1)
    |> Enum.join(" ")
  end
end
```

```elixir
# my_app/mix.exs — Referencia la librería local
defp deps do
  [
    # TODO: Agrega {:my_utils, path: "../my_utils"} como dependencia local
    # El path es relativo al directorio de my_app
  ]
end
```

```bash
# En my_app
$ mix deps.get
# * Getting my_utils (../my_utils)  ← No descarga de hex, usa el path local

$ iex -S mix
```

```elixir
# En IEx de my_app — my_utils ya está disponible
iex> MyUtils.title_case("hello world from elixir")
"Hello World From Elixir"

iex> MyUtils.format_currency(1234.56)
"USD 1234.56"

# TODO: Modifica MyUtils.format_currency/2 en my_utils para retornar "$1,234.56"
# Luego en el IEx de my_app:
iex> recompile()
# Recompila my_utils y my_app automáticamente
iex> MyUtils.format_currency(1234.56)
"$1,234.56"   # El cambio se refleja sin reinstalar nada
```

Expected output:
```elixir
iex> MyUtils.title_case("the quick brown fox")
"The Quick Brown Fox"
```

---

## Try It Yourself

Crea un proyecto que use `{:req, "~> 0.5"}` para hacer un HTTP request y `{:jason, "~> 1.4"}` para parsear el JSON. Sin solución incluida.

```elixir
# Proyecto: weather_client
# Objetivo: Consultar la API pública https://wttr.in/?format=j1
#           y mostrar temperatura y descripción del tiempo

defmodule WeatherClient do
  @moduledoc """
  Cliente HTTP para consultar temperatura actual.
  Usa Req para HTTP y Jason para parsear el JSON de respuesta.
  """

  # La URL retorna JSON con el tiempo actual para una ciudad
  @base_url "https://wttr.in"

  @doc """
  Consulta el tiempo para una ciudad dada.
  Retorna {:ok, weather_info} o {:error, reason}.
  """
  def get_weather(city \\ "London") do
    # Implementa usando:
    # 1. Req.get/1 para hacer el GET request a "#{@base_url}/#{city}?format=j1"
    # 2. El body ya viene parseado (Req lo hace automáticamente si Jason está instalado)
    # 3. Extrae current_condition -> temp_C y weatherDesc del JSON
    # 4. Retorna {:ok, %{city: city, temp_c: temp, description: desc}}
    raise "Not implemented"
  end

  @doc """
  Imprime el tiempo de forma legible.
  """
  def print_weather(city \\ "London") do
    case get_weather(city) do
      {:ok, info} ->
        IO.puts("Weather in #{info.city}: #{info.temp_c}°C — #{info.description}")
      {:error, reason} ->
        IO.puts("Error: #{inspect(reason)}")
    end
  end
end

# En IEx:
# WeatherClient.print_weather("Madrid")
# Weather in Madrid: 22°C — Partly cloudy
```

**Objetivo**: Configurar las dependencias correctamente en `mix.exs`, ejecutar `mix deps.get`, y que el código funcione consultando una API real.

---

## Common Mistakes

### Mistake 1: Olvidar mix deps.get después de editar mix.exs

**Wrong:**
```bash
# Agregas {:jason, "~> 1.4"} a mix.exs y vas directo a compilar
$ mix compile
```
**Error:** `** (Mix) No such file or directory "deps/jason"`
**Why:** Mix no descarga dependencias automáticamente al compilar. Solo las usa si ya están descargadas en el directorio `deps/`.
**Fix:**
```bash
$ mix deps.get  # Siempre después de cambiar deps en mix.exs
$ mix compile
```

### Mistake 2: No committear mix.lock

**Wrong:**
```bash
# .gitignore incorrecto que ignora el lockfile
echo "mix.lock" >> .gitignore
git add .gitignore
git commit -m "ignore lock file"
```
**Why:** Sin el lockfile, distintos desarrolladores y entornos de CI instalan versiones diferentes de dependencias transitivas. Esto causa bugs que "solo ocurren en mi máquina".
**Fix:**
```bash
# mix.lock NUNCA debe estar en .gitignore
# Siempre committearlo junto con mix.exs
git add mix.exs mix.lock
git commit -m "add jason dependency"
```

### Mistake 3: Usar ~> con versión mayor que la disponible

**Wrong:**
```elixir
{:jason, "~> 2.0"}   # Si Jason 2.x no existe todavía
```
**Error:** `No matching version found: jason ~> 2.0`
**Fix:**
```bash
# Antes de agregar una dependencia, verifica en hex.pm qué versión es la latest
$ mix hex.info jason
# Latest: 1.4.4
# Versions: 1.4.4, 1.4.3, 1.4.2, ...

# Usar la versión correcta
{:jason, "~> 1.4"}
```

---

## Verification

```bash
# Verificar que las dependencias están instaladas
$ mix deps

# Verificar que el proyecto compila
$ mix compile

# Verificar que los tests pasan (las deps no deben romper tests)
$ mix test

# Verificar que el lockfile existe y está actualizado
$ ls -la mix.lock

# Verificar qué versión exacta de jason está instalada
$ cat mix.lock | grep jason
```

## Summary
- **Key concepts**: Hex package manager, `~>` vs `>=` vs `==`, `only: :dev`, `runtime: false`, `mix.lock`, dependencias `path:`
- **What you practiced**: Agregar dependencias, entender restricciones de versión, separar dependencias por ambiente, leer el lockfile, referenciar proyectos locales
- **Important to remember**: Siempre ejecuta `mix deps.get` después de cambiar `mix.exs`. Siempre committea `mix.lock`. Usa `~>` con dos dígitos para librerías estables, tres dígitos cuando quieres estabilidad máxima de patch.

## What's Next
En el siguiente ejercicio **20-configuracion-config-runtime** aprenderás a separar la configuración por ambiente y a leer variables de entorno en runtime de forma segura.

## Resources
- [Hex Package Manager](https://hex.pm)
- [Mix.Tasks.Deps](https://hexdocs.pm/mix/Mix.Tasks.Deps.html)
- [Version Constraints — Mix](https://hexdocs.pm/elixir/Version.html)
- [Req HTTP Client](https://hexdocs.pm/req/Req.html)
- [Jason JSON Library](https://hexdocs.pm/jason/Jason.html)
