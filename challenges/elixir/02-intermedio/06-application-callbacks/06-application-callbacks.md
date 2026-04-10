# 6. Application: El Punto de Entrada de tu App

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 04-genserver-basico
- Completed 05-supervisor-basico
- Familiarity with `mix.exs` and Mix project structure

## Learning Objectives
After completing this exercise, you will be able to:
- Implement the `Application` behaviour with `use Application`
- Define `start/2` para inicializar el árbol de supervisión al arranque
- Leer configuración desde `config.exs` con `Application.get_env/3`
- Registrar el módulo Application en `mix.exs` con la clave `:mod`
- Verificar el estado de las aplicaciones con `Application.started_applications/0`
- Implementar `stop/1` para cleanup de recursos al cerrar

## Concepts

### Application: el behaviour de inicialización
En Elixir, una "aplicación" (en el sentido OTP) es una unidad autocontenida de código que puede iniciarse y detenerse como un todo. El behaviour `Application` define los callbacks que OTP llama cuando inicia o detiene tu app. El callback más importante es `start/2` — aquí lanzas tu árbol de supervisión principal.

Cuando ejecutas `mix run` o `iex -S mix`, Mix detecta el módulo Application de tu proyecto (definido en `mix.exs` bajo la clave `:mod`) y llama `start/2` automáticamente. Esto significa que todos tus workers, supervisores y servicios arrancan automáticamente sin que tengas que iniciarlos manualmente.

```elixir
defmodule MiApp.Application do
  use Application

  @impl Application
  def start(_type, _args) do
    children = [
      MiApp.Repo,          # base de datos
      MiApp.Cache,         # sistema de cache
      {MiApp.Worker, []},  # worker background
    ]
    opts = [strategy: :one_for_one, name: MiApp.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Configuración con Application.get_env
`Application.get_env/3` permite leer valores de configuración definidos en `config/config.exs` (o `config/runtime.exs` para configuración en tiempo de ejecución). Esta es la forma estándar de parametrizar tu app sin hardcodear valores.

```elixir
# En config/config.exs:
config :mi_app, :puerto, 4000
config :mi_app, MiApp.Repo,
  host: "localhost",
  database: "mi_app_dev"

# En tu código:
puerto = Application.get_env(:mi_app, :puerto, 8080)   # 8080 es el default
repo_config = Application.get_env(:mi_app, MiApp.Repo)
```

La clave de la configuración siempre empieza con el nombre de la app (atom), que en `mix.exs` está definido bajo la clave `app:`.

### Registrar el módulo en mix.exs
Para que OTP sepa qué módulo tiene los callbacks de Application, debes registrarlo en `mix.exs`:

```elixir
def application do
  [
    mod: {MiApp.Application, []},   # módulo + args que recibirá start/2
    extra_applications: [:logger]    # dependencias OTP adicionales
  ]
end
```

Sin la clave `:mod`, OTP no sabe que tienes callbacks de Application y no los llamará.

### Orden de arranque
OTP garantiza que las dependencias se inician antes que tu app. Si tu `mix.exs` lista `{:ecto_sql, "~> 3.10"}`, Ecto estará completamente iniciado antes de que se llame a tu `start/2`. Dentro de tu propio árbol de supervisión, los hijos se inician en orden de lista — el primero en la lista se inicia primero.

## Exercises

### Exercise 1: Módulo Application básico
Para este ejercicio, crea un proyecto Mix nuevo o usa un proyecto existente.

```bash
$ mix new mi_app_otp --sup
$ cd mi_app_otp
```

El flag `--sup` ya crea un módulo Application. Ábrelo:

```elixir
# lib/mi_app_otp/application.ex — generado por mix new --sup
defmodule MiAppOtp.Application do
  use Application

  @impl Application
  def start(_type, _args) do
    children = [
      # TODO: Agrega tus children aquí
    ]
    opts = [strategy: :one_for_one, name: MiAppOtp.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

```elixir
# TODO: Crea este módulo en lib/mi_app_otp/application.ex
# (si creaste el proyecto con --sup, edita el existente)
defmodule MiAppOtp.Application do
  use Application

  # TODO: Implementa `start/2`:
  # 1. Define children con al menos un worker: MiAppOtp.Contador
  # 2. Imprime "MiAppOtp iniciando..." al comienzo
  # 3. Llama Supervisor.start_link(children, strategy: :one_for_one, name: MiAppOtp.Supervisor)
  # 4. Retorna lo que retorna Supervisor.start_link
  @impl Application
  def start(_type, _args) do
    IO.puts("MiAppOtp iniciando...")
    children = [
      # TODO: MiAppOtp.Contador
    ]
    Supervisor.start_link(children, strategy: :one_for_one, name: MiAppOtp.Supervisor)
  end

  # TODO: Implementa `stop/1`:
  # Imprime "MiAppOtp detenida."
  # Retorna :ok
  @impl Application
  def stop(_state) do
    IO.puts("MiAppOtp detenida.")
    :ok
  end
end

# Worker simple para usar en el árbol de supervisión
defmodule MiAppOtp.Contador do
  use GenServer

  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  end

  def get, do: GenServer.call(__MODULE__, :get)
  def incrementar, do: GenServer.cast(__MODULE__, :inc)

  @impl GenServer
  def init(n), do: {:ok, n}

  @impl GenServer
  def handle_call(:get, _from, n), do: {:reply, n, n}

  @impl GenServer
  def handle_cast(:inc, n), do: {:noreply, n + 1}
end
```

```elixir
# mix.exs — asegúrate de tener la clave :mod en application/0
def application do
  [
    # TODO: agregar mod: {MiAppOtp.Application, []}
    mod: {MiAppOtp.Application, []},
    extra_applications: [:logger]
  ]
end
```

```bash
# Verificar que arranca correctamente:
# $ iex -S mix
# MiAppOtp iniciando...
# iex> MiAppOtp.Contador.get()
# 0
```

### Exercise 2: Leer configuración en start/2
```elixir
# config/config.exs — agrega esta configuración
# import Config

# config :mi_app_otp, :nombre_app, "MiApp Producción"
# config :mi_app_otp, :max_workers, 5
# config :mi_app_otp, :debug_mode, false

defmodule MiAppOtp.Application do
  use Application

  @impl Application
  # TODO: Implementa `start/2` que:
  # 1. Lee :nombre_app de la config con Application.get_env(:mi_app_otp, :nombre_app, "MiApp")
  # 2. Lee :debug_mode con Application.get_env(:mi_app_otp, :debug_mode, false)
  # 3. Si debug_mode es true, imprime "Modo debug activado"
  # 4. Imprime "Iniciando #{nombre_app}..."
  # 5. Inicia el árbol de supervisión normalmente
  def start(_type, _args) do
    nombre = Application.get_env(:mi_app_otp, :nombre_app, "MiApp")
    debug = Application.get_env(:mi_app_otp, :debug_mode, false)

    if debug do
      # TODO: imprimir "Modo debug activado"
    end

    IO.puts("Iniciando #{nombre}...")

    children = [MiAppOtp.Contador]
    Supervisor.start_link(children, strategy: :one_for_one, name: MiAppOtp.Supervisor)
  end

  @impl Application
  def stop(_state), do: :ok
end

# Para probar diferentes configuraciones en tiempo de ejecución (sin reiniciar):
# Application.put_env(:mi_app_otp, :debug_mode, true)
# Application.get_env(:mi_app_otp, :debug_mode)   # => true
```

### Exercise 3: Configuración por entorno en config.exs
```elixir
# Este ejercicio muestra la estructura estándar de configuración por entorno
# Crea o edita los siguientes archivos en un proyecto Mix:

# config/config.exs — configuración base (todos los entornos)
# import Config
#
# config :mi_app_otp,
#   nombre_app: "MiApp",
#   version: "1.0.0"
#
# # Importar configuración específica del entorno al final
# import_config "#{config_env()}.exs"

# config/dev.exs — solo en desarrollo
# import Config
#
# config :mi_app_otp,
#   debug_mode: true,
#   log_level: :debug

# config/prod.exs — solo en producción
# import Config
#
# config :mi_app_otp,
#   debug_mode: false,
#   log_level: :info

defmodule MiAppOtp.Config do
  # Módulo helper para acceder a la configuración de forma centralizada

  # TODO: Implementa `nombre_app/0` — retorna Application.get_env con default "MiApp"
  def nombre_app do
    Application.get_env(:mi_app_otp, :nombre_app, "MiApp")
  end

  # TODO: Implementa `debug_mode?/0` — retorna true si debug_mode está activo
  def debug_mode? do
    Application.get_env(:mi_app_otp, :debug_mode, false)
  end

  # TODO: Implementa `log_level/0` — retorna :debug, :info, :warning, o :error
  def log_level do
    Application.get_env(:mi_app_otp, :log_level, :info)
  end

  # TODO: Implementa `all/0` — retorna todas las claves de config de :mi_app_otp
  # PISTA: Application.get_all_env(:mi_app_otp) retorna un keyword list
  def all do
    Application.get_all_env(:mi_app_otp)
  end
end

# Test it (en IEx):
# MiAppOtp.Config.nombre_app()    # => valor de config.exs
# MiAppOtp.Config.debug_mode?()   # => true en dev, false en prod
# MiAppOtp.Config.all()           # => [nombre_app: "MiApp", debug_mode: true, ...]
```

### Exercise 4: Verificar aplicaciones iniciadas
```elixir
defmodule AppInspector do
  # Módulo de diagnóstico que inspecciona el estado de las aplicaciones OTP

  # TODO: Implementa `apps_iniciadas/0` que retorna la lista de aplicaciones corriendo:
  # Application.started_applications() retorna [{:app_name, description, version}, ...]
  # Extrae solo los nombres (:app_name) y retorna la lista
  def apps_iniciadas do
    Application.started_applications()
    |> Enum.map(fn {nombre, _desc, _vsn} ->
      # TODO: retornar nombre
    end)
  end

  # TODO: Implementa `esta_iniciada?/1` que recibe un nombre de app (atom):
  # Retorna true si la aplicación está en la lista de iniciadas
  def esta_iniciada?(nombre_app) do
    nombre_app in apps_iniciadas()
  end

  # TODO: Implementa `info_app/1` que retorna información de una aplicación específica:
  # Usa Application.spec/1 que retorna una keyword list con :description, :vsn, :modules, etc.
  # Retorna {:ok, spec} o {:error, :not_found} si la app no existe
  def info_app(nombre_app) do
    case Application.spec(nombre_app) do
      nil ->
        # TODO: retornar {:error, :not_found}
      spec ->
        # TODO: retornar {:ok, spec}
    end
  end

  # TODO: Implementa `resumen/0` que imprime un resumen legible:
  # Número de aplicaciones corriendo
  # Lista de nombres de las apps
  # Si :mi_app_otp está entre ellas
  def resumen do
    apps = apps_iniciadas()
    IO.puts("Aplicaciones OTP corriendo: #{length(apps)}")
    IO.puts("Lista: #{Enum.join(apps, ", ")}")
    IO.puts("mi_app_otp activa: #{esta_iniciada?(:mi_app_otp)}")
  end
end

# Test it (en IEx con -S mix):
# AppInspector.apps_iniciadas()
# # => [:mi_app_otp, :logger, :elixir, :compiler, :iex, ...]
# AppInspector.esta_iniciada?(:logger)      # => true
# AppInspector.esta_iniciada?(:no_existe)   # => false
# AppInspector.info_app(:elixir)
# AppInspector.resumen()
```

### Exercise 5: stop/1 para cleanup de recursos
```elixir
defmodule MiAppOtp.Application do
  use Application
  require Logger

  # Simula recursos que necesitan cleanup al cerrar (conexiones, archivos, etc.)
  @recursos_globales :recursos_abiertos

  @impl Application
  def start(_type, _args) do
    Logger.info("Iniciando MiAppOtp v#{Application.spec(:mi_app_otp, :vsn)}")

    # Simula abrir recursos al inicio
    :persistent_term.put(@recursos_globales, [:conexion_db, :cache_redis, :archivo_log])

    children = [MiAppOtp.Contador]
    resultado = Supervisor.start_link(children, strategy: :one_for_one, name: MiAppOtp.Supervisor)

    Logger.info("MiAppOtp iniciada correctamente")
    resultado
  end

  # TODO: Implementa `stop/1`:
  # 1. Recupera la lista de recursos de :persistent_term con @recursos_globales
  # 2. Para cada recurso, imprime "Cerrando recurso: #{inspect(recurso)}"
  # 3. Simula el cleanup con :timer.sleep(50) por recurso
  # 4. Imprime "Todos los recursos cerrados"
  # 5. Retorna :ok
  # NOTA: stop/1 recibe el `state` retornado por start/2 (normalmente se ignora)
  @impl Application
  def stop(_state) do
    recursos = :persistent_term.get(@recursos_globales, [])
    Logger.info("Iniciando shutdown, cerrando #{length(recursos)} recursos...")

    Enum.each(recursos, fn recurso ->
      # TODO: imprimir y simular cierre de recurso
    end)

    Logger.info("Todos los recursos cerrados")
    :ok
  end
end

# Para probar el stop en IEx:
# iex -S mix
# (ves los mensajes de inicio)
# Application.stop(:mi_app_otp)
# (ves los mensajes de stop/cleanup)
# Application.started_applications() |> Enum.member?({:mi_app_otp, ...})
```

### Try It Yourself
Construye una aplicación OTP completa que integra todo lo aprendido:

Estructura:
```
MiSistema.Application
└── MiSistema.RootSupervisor (:one_for_one)
    ├── MiSistema.CounterServer  (GenServer, nombre: :counter)
    └── MiSistema.LoggerAgent    (Agent, nombre: :logger_agent)
```

Requisitos:
- `MiSistema.Application.start/2` lee configuración:
  - `:max_contador` — valor máximo antes de resetear automáticamente (default: 100)
  - `:log_prefix` — prefijo para los mensajes de log (default: "[MiSistema]")
- `MiSistema.CounterServer` — GenServer con `get/0`, `incrementar/0`, `reset/0`. Si supera `:max_contador`, se resetea automáticamente (en `handle_cast(:inc, ...)`)
- `MiSistema.LoggerAgent` — Agent que almacena lista de eventos `[{timestamp, mensaje}]` con `log/1` y `get_logs/0`
- Registrar el módulo Application en `mix.exs`
- `mix run --no-halt` debe arrancar la app y mantenerla corriendo

```elixir
# config/config.exs
# config :mi_sistema, :max_contador, 10
# config :mi_sistema, :log_prefix, "[Demo]"

defmodule MiSistema.Application do
  use Application
  # Tu implementación aquí
end
```

## Common Mistakes

### Mistake 1: Olvidar registrar el módulo en mix.exs
```elixir
# ❌ Sin :mod, OTP no llama a start/2 y la app no inicializa
def application do
  [extra_applications: [:logger]]
  # Falta: mod: {MiApp.Application, []}
end

# ✓ Registrar el módulo Application
def application do
  [
    mod: {MiApp.Application, []},
    extra_applications: [:logger]
  ]
end
```

### Mistake 2: Usar Application.get_env en compile time
```elixir
# ❌ @attr se resuelve en tiempo de compilación — la config puede no estar cargada
@puerto Application.get_env(:mi_app, :puerto, 4000)

# ✓ Leer config en runtime (en funciones, no en atributos de módulo)
def puerto do
  Application.get_env(:mi_app, :puerto, 4000)
end

# ✓ O usar compile_env solo si la config NUNCA cambia en runtime
@puerto Application.compile_env(:mi_app, :puerto, 4000)
```

### Mistake 3: Hacer trabajo lento en start/2
```elixir
# ❌ start/2 no debe bloquearse — si tarda mucho, OTP considera que falló el inicio
@impl Application
def start(_type, _args) do
  cargar_datos_desde_db()   # Puede tardar 30 segundos — OTP timeout!
  Supervisor.start_link([], strategy: :one_for_one)
end

# ✓ El trabajo de inicialización debe hacerse en el init/1 de los GenServers/Workers
@impl Application
def start(_type, _args) do
  children = [MiApp.DataLoader]   # El DataLoader carga datos en su propio init/1
  Supervisor.start_link(children, strategy: :one_for_one)
end
```

### Mistake 4: No retornar {:ok, pid} desde start/2
```elixir
# ❌ start/2 DEBE retornar {:ok, pid} (lo que retorna Supervisor.start_link)
@impl Application
def start(_type, _args) do
  Supervisor.start_link([], strategy: :one_for_one)
  :ok   # ❌ Retornar :ok hace que OTP falle el inicio
end

# ✓ Retornar el resultado de Supervisor.start_link directamente
@impl Application
def start(_type, _args) do
  Supervisor.start_link(children, strategy: :one_for_one)
  # Retorna {:ok, #PID<...>} automáticamente
end
```

## Verification
```bash
$ cd mi_app_otp
$ iex -S mix
MiAppOtp iniciando...
iex> Application.started_applications() |> Enum.map(&elem(&1, 0))
# Debe incluir :mi_app_otp
iex> MiAppOtp.Contador.get()
0
iex> MiAppOtp.Contador.incrementar()
:ok
iex> Application.get_env(:mi_app_otp, :nombre_app)
"MiApp"
iex> Application.stop(:mi_app_otp)
MiAppOtp detenida.
:ok
```

Checklist de verificación:
- [ ] `use Application` y `@impl Application` en todos los callbacks
- [ ] `start/2` retorna `{:ok, pid}` (el resultado de `Supervisor.start_link`)
- [ ] La clave `:mod` está correctamente definida en `mix.exs`
- [ ] `Application.get_env/3` lee valores de `config.exs` en runtime
- [ ] Los valores por defecto se aplican cuando la clave no está en config
- [ ] `stop/1` imprime mensaje de cleanup y retorna `:ok`
- [ ] `Application.started_applications/0` muestra la app corriendo
- [ ] `iex -S mix` arranca la app automáticamente (verifica con el log de inicio)

## Summary
- `Application` es el behaviour de OTP para la inicialización y cierre de tu app
- `start/2` debe retornar `{:ok, pid}` — lanza el árbol de supervisión raíz aquí
- Registrar el módulo en `mix.exs` con `:mod` es obligatorio para que OTP lo use
- `Application.get_env/3` es la forma estándar de leer configuración en runtime
- La configuración se define en `config/config.exs` y sus variantes por entorno
- `stop/1` se usa para cleanup — cerrar conexiones, flushear buffers, etc.
- `Application.started_applications/0` permite introspección del estado OTP

## What's Next
**07-behaviours-y-callbacks**: Aprende a definir contratos de módulo con `@behaviour` y `@callback`, y a implementar esos contratos con `@impl`, la base del polimorfismo estructural en Elixir.

## Resources
- [Application — HexDocs](https://hexdocs.pm/elixir/Application.html)
- [Application.get_env/3 — HexDocs](https://hexdocs.pm/elixir/Application.html#get_env/3)
- [Config — HexDocs](https://hexdocs.pm/elixir/Config.html)
- [Mix and OTP: Application](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
