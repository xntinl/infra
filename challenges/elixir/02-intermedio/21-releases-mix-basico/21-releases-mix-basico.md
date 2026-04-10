# 21. Releases con Mix

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de `mix.exs` y configuración de proyectos
- Comprensión de `config/runtime.exs` (ejercicio 20)
- Familiaridad con la estructura de aplicaciones OTP

## Learning Objectives
After completing this exercise, you will be able to:
- Configurar una release en `mix.exs` con opciones básicas
- Construir una release con `mix release`
- Arrancar, detener y reiniciar la aplicación desde el binario generado
- Conectarte a un nodo corriendo con el remote shell
- Configurar la release para leer variables de entorno en runtime

## Concepts

### ¿Qué es una Release?

Una release es un artefacto de despliegue autocontenido que incluye:
- Tu código compilado (BEAM files)
- Todas las dependencias
- El runtime de Erlang (ERTS)
- Scripts de arranque y control

A diferencia de desplegar el código fuente, una release **no requiere que Elixir o Erlang estén instalados** en el servidor de destino. Todo lo necesario para ejecutar la aplicación está dentro del directorio de la release.

```
_build/prod/rel/my_app/
├── bin/
│   ├── my_app          ← Script principal de control
│   └── my_app.bat      ← Script para Windows
├── erts-15.0/          ← Erlang Runtime System incluido
├── lib/
│   ├── my_app-0.1.0/   ← Tu código compilado
│   ├── jason-1.4.4/    ← Dependencias incluidas
│   └── ...
├── releases/
│   └── 0.1.0/
│       └── my_app.rel  ← Descriptor de la release
└── ...
```

### Configurar Release en mix.exs

La configuración de releases va en la función `project/0` de `mix.exs`:

```elixir
defmodule MyApp.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_app,
      version: "0.1.0",
      elixir: "~> 1.14",
      start_permanent: Mix.env() == :prod,  # Termina el SO si la app falla
      deps: deps(),
      releases: releases()   # ← Aquí va la configuración de releases
    ]
  end

  defp releases do
    [
      my_app: [
        # Incluir scripts de control para sistemas Unix
        include_executables_for: [:unix],

        # Incluir ERTS en la release (no requiere Erlang en el servidor)
        include_erts: true,

        # Aplicaciones a incluir (automático si no se especifica)
        applications: [runtime_tools: :permanent]
      ]
    ]
  end
end
```

### Construir una Release

```bash
# Limpiar compilación anterior (recomendado cuando se cambia configuración)
$ mix clean

# Compilar e instalar dependencias
$ MIX_ENV=prod mix deps.get
$ MIX_ENV=prod mix deps.compile

# Construir la release
$ MIX_ENV=prod mix release

# Output esperado:
# * assembling my_app-0.1.0 on MIX_ENV=prod
# * using config/runtime.exs
# * skipping elixir.bat for Windows (target is Unix)
# Release created at _build/prod/rel/my_app
#
# Start your app: _build/prod/rel/my_app/bin/my_app start

# También puedes especificar el nombre de la release
$ MIX_ENV=prod mix release my_app
```

### Comandos del Script Binario

El script `bin/my_app` es el punto de entrada para controlar la aplicación:

```bash
# Arrancar en foreground (útil para ver logs en consola)
$ _build/prod/rel/my_app/bin/my_app start

# Arrancar como daemon en background
$ _build/prod/rel/my_app/bin/my_app daemon

# Detener la aplicación (si está en daemon)
$ _build/prod/rel/my_app/bin/my_app stop

# Verificar si la aplicación está corriendo
$ _build/prod/rel/my_app/bin/my_app pid
# 12345  ← PID del proceso si está corriendo

# Evaluar una expresión Elixir en el nodo corriendo
$ _build/prod/rel/my_app/bin/my_app eval "IO.puts(:hello)"

# Remote shell — conectarse al nodo corriendo de forma interactiva
$ _build/prod/rel/my_app/bin/my_app remote
# Erlang/OTP 26 [erts-14.0] [64-bit]
# Interactive Elixir (1.15.7) - press Ctrl+C to exit
# iex(my_app@hostname)>
```

### Remote Shell — Debugging en Producción

El remote shell te conecta a un nodo Elixir que ya está corriendo, sin detenerlo ni reiniciarlo. Es invaluable para debugging en producción.

```elixir
# Conectado vía: bin/my_app remote

# Inspeccionar el estado de la aplicación
iex> Application.get_env(:my_app, :timeout)
5000

# Ver todos los procesos corriendo
iex> length(Process.list())
157

# Inspeccionar un GenServer específico (si tu app los tiene)
iex> :sys.get_state(MyApp.Cache)
%{entries: %{}, max_size: 100}

# Ejecutar código arbitrario en producción (con cuidado)
iex> MyApp.Config.reload!()

# Salir del remote shell SIN detener el nodo
iex> System.halt()    # ← NO uses esto — mata el nodo
# En su lugar:
# Ctrl+C dos veces   ← Sale del remote shell sin afectar el nodo corriendo
# O:
iex> exit(:normal)   # ← También sale sin matar el nodo
```

### Variables de Entorno en Releases

El flujo completo para configuración de releases:

```elixir
# config/runtime.exs — se evalúa cuando la release arranca
import Config

# Las variables de entorno se leen cuando la app inicia, no cuando se compila
port = String.to_integer(System.get_env("PORT", "4000"))
config :my_app, MyApp.Endpoint, port: port

if config_env() == :prod do
  config :my_app, :database_url,
    System.get_env("DATABASE_URL") ||
      raise "environment variable DATABASE_URL is missing."
end
```

```bash
# Arrancar la release con variables de entorno
$ DATABASE_URL="postgres://user:pass@localhost/mydb" \
  SECRET_KEY_BASE="my_secret" \
  PORT="8080" \
  _build/prod/rel/my_app/bin/my_app start
```

## Exercises

### Exercise 1: Configurar Release en mix.exs

Agrega la configuración de release a un proyecto Mix existente.

```elixir
# mix.exs — ANTES (sin configuración de release)
defmodule MyCounter.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_counter,
      version: "0.1.0",
      elixir: "~> 1.14",
      start_permanent: Mix.env() == :prod,
      deps: deps()
    ]
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {MyCounter.Application, []}
    ]
  end

  defp deps do
    []
  end
end

# mix.exs — DESPUÉS
defmodule MyCounter.MixProject do
  use Mix.Project

  def project do
    [
      app: :my_counter,
      version: "0.1.0",
      elixir: "~> 1.14",
      start_permanent: Mix.env() == :prod,
      deps: deps(),
      # TODO: Agrega releases: releases() aquí
    ]
  end

  # TODO: Define la función releases/0 privada que retorne:
  # [
  #   my_counter: [
  #     include_executables_for: [:unix],
  #     include_erts: true
  #   ]
  # ]
  defp releases do
    # Tu configuración aquí
  end

  def application do
    [
      extra_applications: [:logger],
      mod: {MyCounter.Application, []}
    ]
  end

  defp deps, do: []
end
```

```bash
# Verifica que la configuración de release es válida
$ MIX_ENV=prod mix release --dry-run
# * assembling my_counter-0.1.0 on MIX_ENV=prod
# (dry run — no crea archivos)
```

Expected output:
```bash
$ MIX_ENV=prod mix release --dry-run
* assembling my_counter-0.1.0 on MIX_ENV=prod
* using config/runtime.exs
Release created at _build/prod/rel/my_counter (dry run)
```

---

### Exercise 2: Construir la Release y Explorar la Estructura

Construye la release real y examina los directorios y archivos generados.

```elixir
# lib/my_counter/application.ex — App mínima para el ejercicio
defmodule MyCounter.Application do
  use Application

  def start(_type, _args) do
    children = [
      {Agent, fn -> 0 end}  # Contador simple usando Agent
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: MyCounter.Supervisor)
  end
end

# lib/my_counter.ex
defmodule MyCounter do
  # Usa el nombre del proceso para encontrarlo
  def increment, do: Agent.update(agent_pid(), &(&1 + 1))
  def decrement, do: Agent.update(agent_pid(), &(&1 - 1))
  def value, do: Agent.get(agent_pid(), & &1)
  def reset, do: Agent.update(agent_pid(), fn _ -> 0 end)

  defp agent_pid do
    # El Agent fue registrado como primer hijo del supervisor
    [{pid, _}] = Supervisor.which_children(MyCounter.Supervisor)
    pid
  end
end
```

```bash
# TODO: Construye la release
$ MIX_ENV=prod mix release

# TODO: Explora la estructura generada con los siguientes comandos:
$ ls _build/prod/rel/my_counter/

# TODO: Examina el directorio bin/
$ ls _build/prod/rel/my_counter/bin/

# TODO: Examina qué versión de ERTS está incluida
$ ls _build/prod/rel/my_counter/erts-*/

# TODO: Verifica que tu código está compilado en la release
$ ls _build/prod/rel/my_counter/lib/my_counter-0.1.0/

# ¿Cuánto espacio ocupa la release completa?
$ du -sh _build/prod/rel/my_counter/
```

Expected output:
```bash
$ ls _build/prod/rel/my_counter/
bin  erts-15.0  lib  releases

$ ls _build/prod/rel/my_counter/bin/
my_counter  my_counter.bat

$ du -sh _build/prod/rel/my_counter/
28M     _build/prod/rel/my_counter/
```

---

### Exercise 3: Arrancar, Usar y Detener la Release

Practica el ciclo completo de vida de una release.

```bash
# Alias para abreviar — el path completo es largo
RELEASE="./_build/prod/rel/my_counter/bin/my_counter"

# TODO: Arrancar la release en foreground (Ctrl+C para detener)
$ $RELEASE start
# Erlang/OTP 26 [erts-15.0]
# [MyCounter] Application started

# TODO: En otra terminal, verifica que está corriendo
$ $RELEASE pid
# 54321  ← PID del proceso

# TODO: Evalúa una expresión en el nodo corriendo (sin remote shell)
$ $RELEASE eval "IO.puts(MyCounter.value())"
# 0

$ $RELEASE eval "MyCounter.increment(); MyCounter.increment(); IO.puts(MyCounter.value())"
# 2

# TODO: Detén la aplicación desde otra terminal (si está en daemon)
# Para daemon:
$ $RELEASE daemon   # Arrancar como daemon
$ $RELEASE stop     # Detener el daemon

# Para foreground: Ctrl+C en la terminal donde corre
```

Expected output:
```bash
$ $RELEASE eval "IO.puts(MyCounter.value())"
0

$ $RELEASE eval "MyCounter.increment(); IO.puts(MyCounter.value())"
1

$ $RELEASE eval "MyCounter.increment(); IO.puts(MyCounter.value())"
2
```

---

### Exercise 4: Remote Shell — Conectarse al Nodo Corriendo

Usa el remote shell para interactuar con la aplicación en vivo.

```bash
# Primero, arranca la release como daemon en background
$ $RELEASE daemon

# Verifica que está corriendo
$ $RELEASE pid
# 54321

# TODO: Abre el remote shell
$ $RELEASE remote

# Dentro del remote shell, ejecuta:
# TODO: Inspecciona el valor del contador
iex(my_counter@hostname)> MyCounter.value()
# 0

# TODO: Incrementa varias veces
iex(my_counter@hostname)> MyCounter.increment()
iex(my_counter@hostname)> MyCounter.increment()
iex(my_counter@hostname)> MyCounter.increment()

# TODO: Verifica el nuevo valor
iex(my_counter@hostname)> MyCounter.value()
# 3

# TODO: Inspecciona los procesos del supervisor
iex(my_counter@hostname)> Supervisor.which_children(MyCounter.Supervisor)

# TODO: Verifica la configuración de la aplicación
iex(my_counter@hostname)> Application.get_all_env(:my_counter)

# IMPORTANTE: Sal del remote shell SIN matar el nodo
# Presiona Ctrl+C dos veces
# O escribe: System.halt() ← esto SÍ mata el nodo, evítalo

# Después de salir del remote shell, verifica que el nodo sigue corriendo
$ $RELEASE pid
# 54321  ← El mismo PID, el nodo sobrevivió

# Detén el daemon
$ $RELEASE stop
```

Expected output:
```elixir
iex(my_counter@hostname)> MyCounter.value()
3

iex(my_counter@hostname)> Supervisor.which_children(MyCounter.Supervisor)
[{:undefined, #PID<0.234.0>, :worker, [Agent]}]
```

---

### Exercise 5: Variables de Entorno en la Release

Configura la release para que su comportamiento cambie según variables de entorno.

```elixir
# config/runtime.exs — Configuración que se lee al arrancar la release
import Config

# TODO: Lee START_VALUE del entorno para inicializar el contador
# Si no está definida, usa 0 como default
# String.to_integer(System.get_env("START_VALUE", "0"))
start_value = String.to_integer(System.get_env("START_VALUE", "0"))
config :my_counter, :start_value, start_value

# TODO: Lee MAX_VALUE — valor máximo que puede alcanzar el contador
# Default: 100
max_value = String.to_integer(System.get_env("MAX_VALUE", "100"))
config :my_counter, :max_value, max_value

# TODO: Lee LOG_LEVEL y configura el logger
log_level = System.get_env("LOG_LEVEL", "info") |> String.to_atom()
config :logger, level: log_level
```

```elixir
# lib/my_counter/application.ex — Actualizado para leer configuración
defmodule MyCounter.Application do
  use Application

  def start(_type, _args) do
    # TODO: Lee el valor inicial de la configuración
    start_value = Application.get_env(:my_counter, :start_value, 0)

    children = [
      # TODO: Inicializa el Agent con start_value en lugar de 0
      {Agent, fn -> start_value end}
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: MyCounter.Supervisor)
  end
end
```

```bash
# TODO: Reconstruye la release con los cambios
$ MIX_ENV=prod mix release

# TODO: Arranca con START_VALUE y MAX_VALUE configurados
$ START_VALUE=50 MAX_VALUE=200 $RELEASE start

# En otra terminal, verifica que el contador inicia en 50
$ $RELEASE eval "IO.puts(MyCounter.value())"
# 50  ← Inicializado desde la variable de entorno

# TODO: Arranca sin variables — debe usar los defaults
$ $RELEASE start
$ $RELEASE eval "IO.puts(MyCounter.value())"
# 0  ← Default
```

Expected output:
```bash
$ START_VALUE=50 $RELEASE start
# Application started with counter at: 50

$ $RELEASE eval "IO.puts(MyCounter.value())"
50
```

---

## Try It Yourself

Crea una release completa para una Counter app con configuración vía env vars y un startup script. Sin solución incluida.

```elixir
# Objetivo: Release production-ready para MyCounter con:
#
# 1. mix.exs configurado con:
#    - releases con nombre :counter
#    - include_executables_for: [:unix]
#    - version leída de una variable de módulo o archivo externo
#
# 2. runtime.exs que lea:
#    - START_VALUE (integer, default 0)
#    - MAX_VALUE (integer, default 100, usado para validación)
#    - LOG_LEVEL (atom, default :info)
#    - COUNTER_NAME (string, default "main")
#
# 3. MyCounter mejorado con:
#    - increment/0 que retorna {:ok, new_value} o {:error, :max_reached}
#    - decrement/0 que retorna {:ok, new_value} o {:error, :min_reached} (mínimo 0)
#    - reset/0
#    - value/0
#
# 4. Un script de startup (start.sh):
#!/bin/bash
# Valida que las variables obligatorias estén definidas
# (en este caso no hay obligatorias, pero muestra el patrón)
# export LOG_LEVEL="${LOG_LEVEL:-info}"
# export START_VALUE="${START_VALUE:-0}"
# exec _build/prod/rel/counter/bin/counter start
#
# 5. Un Makefile con targets:
# make build   → MIX_ENV=prod mix release
# make start   → ./start.sh
# make stop    → bin/counter stop
# make console → bin/counter remote
# make eval    → bin/counter eval "$(CMD)"

# Verifica que todo funciona:
# make build
# START_VALUE=10 MAX_VALUE=20 make start
# (en otra terminal) bin/counter eval "MyCounter.increment()"
# (debe retornar {:ok, 11})
# Incrementa 10 veces más — al llegar a 20 debe retornar {:error, :max_reached}
```

---

## Common Mistakes

### Mistake 1: Construir la release con MIX_ENV=dev

**Wrong:**
```bash
$ mix release   # Sin MIX_ENV — usa :dev por default
```
**Why:** La release incluye las dependencias de dev (dialyxir, ex_doc, etc.) innecesariamente. Además, el código se compila sin optimizaciones de producción. `start_permanent: Mix.env() == :prod` tampoco se activa.
**Fix:**
```bash
$ MIX_ENV=prod mix release   # Siempre especificar :prod para releases de producción
```

### Mistake 2: Usar System.halt() en el remote shell

**Wrong:**
```elixir
# Dentro del remote shell
iex(my_app@hostname)> System.halt()
# ← ESTO MATA EL NODO COMPLETO EN PRODUCCIÓN
```
**Why:** `System.halt/0` termina el proceso de Erlang VM, incluyendo el nodo al que estás conectado — es decir, mata tu aplicación de producción.
**Fix:**
```elixir
# Para salir del remote shell sin matar el nodo:
# Opción 1: Ctrl+C dos veces
# Opción 2:
iex(my_app@hostname)> exit(:normal)
# Desconecta el remote shell, el nodo sigue corriendo
```

### Mistake 3: Olvidar que los cambios de código requieren reconstruir la release

**Wrong:**
```bash
# Editas lib/my_module.ex
# Reiniciar el daemon SIN reconstruir
$ bin/my_app restart  # Arranca el mismo binario antiguo
```
**Why:** Una release es un binario compilado. Editar el código fuente no cambia el binario — el nodo sigue ejecutando el código anterior.
**Fix:**
```bash
# Reconstruir la release después de cada cambio de código
$ MIX_ENV=prod mix release
$ bin/my_app stop
$ bin/my_app daemon  # Ahora sí ejecuta el código nuevo
```

---

## Verification

```bash
# Construir la release
$ MIX_ENV=prod mix release
# * assembling my_counter-0.1.0 on MIX_ENV=prod
# Release created at _build/prod/rel/my_counter

# Verificar la estructura
$ ls _build/prod/rel/my_counter/bin/

# Arrancar y verificar
$ _build/prod/rel/my_counter/bin/my_counter daemon
$ _build/prod/rel/my_counter/bin/my_counter pid
# 12345

# Evaluar código en el nodo
$ _build/prod/rel/my_counter/bin/my_counter eval "IO.puts(MyCounter.value())"
# 0

# Conectar remote shell
$ _build/prod/rel/my_counter/bin/my_counter remote
iex(my_counter@hostname)> MyCounter.increment()
:ok
iex(my_counter@hostname)> MyCounter.value()
1

# Detener
$ _build/prod/rel/my_counter/bin/my_counter stop
```

## Summary
- **Key concepts**: `mix release`, estructura de release, `bin/app start/daemon/stop/remote/eval`, `config/runtime.exs`, variables de entorno en releases
- **What you practiced**: Configurar releases en `mix.exs`, construir y explorar la estructura generada, arrancar/detener/usar la release, conectarse con remote shell, configurar via env vars
- **Important to remember**: Siempre `MIX_ENV=prod` para releases. Nunca `System.halt()` en el remote shell de producción. Los cambios de código requieren reconstruir la release. `runtime.exs` es lo que hace la release configurable sin recompilar.

## What's Next
Has completado el nivel intermedio. Los conceptos de este nivel — specs, testing, debugging, automatización con Mix tasks, gestión de dependencias, configuración por ambiente, y releases — son la base del trabajo profesional con Elixir. El siguiente nivel cubrirá Phoenix Framework, LiveView, y sistemas distribuidos.

## Resources
- [Mix.Tasks.Release](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Releases — Elixir Guides](https://elixir-lang.org/getting-started/mix-otp/config-and-releases.html)
- [Deploying with Releases](https://hexdocs.pm/phoenix/releases.html)
- [Runtime Configuration](https://hexdocs.pm/elixir/Config.html)
