# 20. Configuración con Config y Runtime

**Difficulty**: Intermedio

## Prerequisites
- Conocimiento de la estructura de proyectos Mix
- Familiaridad con Application y OTP
- Entender qué son las variables de entorno del sistema operativo

## Learning Objectives
After completing this exercise, you will be able to:
- Usar `config/config.exs` para configuración base del proyecto
- Separar configuración por ambiente (`dev.exs`, `prod.exs`, `test.exs`)
- Leer variables de entorno en `config/runtime.exs` de forma segura
- Acceder a la configuración en runtime con `Application.get_env/3`
- Modificar configuración dinámicamente con `Application.put_env/4`

## Concepts

### El Sistema de Configuración de Elixir

Elixir separa la configuración en dos momentos distintos:

1. **Compile-time** (`config/*.exs`): Se evalúa cuando el proyecto se compila. Los valores son "baked in" en los BEAM files.
2. **Runtime** (`config/runtime.exs`): Se evalúa cuando la aplicación arranca. Puede leer variables de entorno del sistema.

Esta separación es importante para releases: las configuraciones de compile-time quedan fijas en el binario, mientras que las de runtime se evalúan cada vez que la aplicación inicia — lo que permite configurar la misma release para diferentes entornos.

```
config/
├── config.exs      # Base — valores por defecto para todos los ambientes
├── dev.exs         # Solo para MIX_ENV=dev
├── test.exs        # Solo para MIX_ENV=test
├── prod.exs        # Solo para MIX_ENV=prod
└── runtime.exs     # Variables de entorno — se evalúa al arrancar
```

### config.exs — La Base

`config.exs` define valores por defecto que aplican a todos los ambientes. Siempre incluye los archivos específicos de ambiente al final.

```elixir
# config/config.exs
import Config

# Configuración base que aplica a todos los ambientes
config :my_app, :timeout, 5000
config :my_app, :max_retries, 3
config :my_app, :log_level, :info

# Configuración de logger (aplicación de Erlang/OTP)
config :logger, level: :info

# Importar el archivo de configuración del ambiente actual
# MIX_ENV determina cuál se importa (dev, test, prod)
import_config "#{config_env()}.exs"
```

### Archivos por Ambiente

```elixir
# config/dev.exs — Configuración para desarrollo
import Config

config :my_app, :timeout, 10_000         # Más tiempo en dev para debugging
config :my_app, :log_level, :debug       # Logs verbosos en desarrollo
config :my_app, :database_url, "postgres://localhost/my_app_dev"

config :logger, level: :debug

# config/test.exs — Configuración para tests
import Config

config :my_app, :timeout, 1_000          # Timeout corto en tests para fallar rápido
config :my_app, :database_url, "postgres://localhost/my_app_test"
config :my_app, :async_workers, 1        # Single worker para tests determinísticos

config :logger, level: :warning          # Silenciar logs en tests

# config/prod.exs — Configuración para producción (compile-time)
import Config

config :my_app, :log_level, :warning
config :my_app, :max_retries, 5
# NUNCA poner secretos aquí — van en runtime.exs
```

### runtime.exs — Variables de Entorno

`config/runtime.exs` se ejecuta cuando la aplicación arranca, incluso en releases. Es el único lugar correcto para leer variables de entorno en producción.

```elixir
# config/runtime.exs
import Config

# En producción, las variables de entorno son obligatorias — falla explícitamente si faltan
if config_env() == :prod do
  database_url =
    System.get_env("DATABASE_URL") ||
      raise """
      environment variable DATABASE_URL is missing.
      For example: ecto://USER:PASS@HOST/DATABASE
      """

  config :my_app, MyApp.Repo,
    url: database_url,
    pool_size: String.to_integer(System.get_env("POOL_SIZE") || "10")

  secret_key_base =
    System.get_env("SECRET_KEY_BASE") ||
      raise "environment variable SECRET_KEY_BASE is missing."

  config :my_app, MyAppWeb.Endpoint,
    secret_key_base: secret_key_base
end

# En todos los ambientes: logging configurable via env var
log_level = System.get_env("LOG_LEVEL", "info") |> String.to_atom()
config :logger, level: log_level
```

### Application.get_env — Leer Configuración en Código

En el código de producción, lees la configuración con `Application.get_env/3`:

```elixir
defmodule MyApp.Client do
  # Leer configuración con valor por defecto
  def timeout do
    Application.get_env(:my_app, :timeout, 5_000)
    # Equivalente a: Application.get_env(:my_app, :timeout) || 5_000
    # Pero get_env/3 es más explícito y maneja nil vs false correctamente
  end

  # Leer configuración de subsistema anidado
  def db_url do
    Application.get_env(:my_app, MyApp.Repo)[:url]
  end

  # Pattern completo para módulo configurable
  defp config, do: Application.get_env(:my_app, __MODULE__, [])
  defp get_opt(key, default), do: Keyword.get(config(), key, default)

  def max_connections, do: get_opt(:max_connections, 10)
  def base_url, do: get_opt(:base_url, "http://localhost")
end
```

### Application.put_env — Modificar en Runtime

`Application.put_env/4` modifica la configuración en memoria durante el runtime. Es útil en tests y para feature flags dinámicos.

```elixir
# En tests — cambiar configuración para un test específico
setup do
  original = Application.get_env(:my_app, :feature_flag)
  Application.put_env(:my_app, :feature_flag, true)

  on_exit(fn ->
    # Restaurar el valor original al terminar el test
    Application.put_env(:my_app, :feature_flag, original)
  end)

  :ok
end

# En el código principal — para feature flags dinámicos
def enable_feature(name) do
  current_flags = Application.get_env(:my_app, :feature_flags, [])
  Application.put_env(:my_app, :feature_flags, [name | current_flags])
end
```

## Exercises

### Exercise 1: config.exs Básico con Application.get_env

Configura valores básicos en `config.exs` y accede a ellos desde el código.

```elixir
# config/config.exs
import Config

# TODO: Configura los siguientes valores para :my_app:
# - :timeout con valor 5_000
# - :max_retries con valor 3
# - :app_name con valor "My Elixir App"
# - :debug_mode con valor false
# Usa config :my_app, :key, value para cada uno
```

```elixir
# lib/my_app/config_reader.ex
defmodule MyApp.ConfigReader do
  @moduledoc "Lee y expone la configuración de la aplicación."

  def timeout do
    # TODO: Usa Application.get_env(:my_app, :timeout, 5_000)
    # El tercer argumento es el default si la clave no existe
  end

  def max_retries do
    # TODO: Usa Application.get_env/3 para leer :max_retries con default 3
  end

  def app_name do
    # TODO: Lee :app_name de la configuración
  end

  def debug_mode? do
    # TODO: Lee :debug_mode de la configuración
  end

  def all do
    # TODO: Retorna un mapa con todos los valores de configuración
    %{
      timeout: timeout(),
      max_retries: max_retries(),
      app_name: app_name(),
      debug_mode: debug_mode?()
    }
  end
end
```

Expected output:
```elixir
iex> MyApp.ConfigReader.all()
%{
  app_name: "My Elixir App",
  debug_mode: false,
  max_retries: 3,
  timeout: 5000
}

iex> MyApp.ConfigReader.timeout()
5000
```

---

### Exercise 2: Configuración por Ambiente

Crea archivos separados para dev, test y prod con valores distintos para las mismas claves.

```elixir
# config/config.exs — Valores base (todos los ambientes)
import Config

config :my_app, :log_level, :info
config :my_app, :timeout, 5_000
config :my_app, :database, %{
  host: "localhost",
  port: 5432,
  name: "my_app"
}

# TODO: Agrega import_config "#{config_env()}.exs" al final del archivo
# Esto importa dev.exs, test.exs, o prod.exs según MIX_ENV
```

```elixir
# config/dev.exs — Sobreescribe valores para desarrollo
import Config

# TODO: Sobreescribe :log_level con :debug
# TODO: Sobreescribe :timeout con 15_000 (más tiempo para debugging)
# TODO: Sobreescribe :database con %{host: "localhost", port: 5432, name: "my_app_dev"}
# config :my_app, :log_level, :debug
# config :my_app, :timeout, ...
```

```elixir
# config/test.exs — Valores optimizados para testing
import Config

# TODO: Configura valores para tests:
# - :log_level -> :warning (silenciar logs innecesarios)
# - :timeout -> 1_000 (fallar rápido en tests)
# - :database -> nombre "my_app_test"
```

```bash
# Verificar que la configuración cambia según el ambiente
$ MIX_ENV=dev iex -S mix
iex> Application.get_env(:my_app, :log_level)
:debug
iex> Application.get_env(:my_app, :timeout)
15000

$ MIX_ENV=test iex -S mix
iex> Application.get_env(:my_app, :log_level)
:warning
iex> Application.get_env(:my_app, :timeout)
1000
```

Expected output:
```bash
# MIX_ENV=dev
Application.get_env(:my_app, :timeout) => 15000
Application.get_env(:my_app, :log_level) => :debug

# MIX_ENV=test
Application.get_env(:my_app, :timeout) => 1000
Application.get_env(:my_app, :log_level) => :warning
```

---

### Exercise 3: runtime.exs — Variables de Entorno

Crea un `runtime.exs` que lea variables de entorno y falle explícitamente si las obligatorias no están definidas.

```elixir
# config/runtime.exs
import Config

# TODO: Lee la variable de entorno LOG_LEVEL
# Si no está definida, usa "info" como default
# Conviértela a átomo con String.to_atom/1
# Configura :logger con ese nivel

# TODO: Solo en producción (:prod), lee estas variables obligatorias:
# - DATABASE_URL: debe existir, si no: raise con mensaje claro
# - SECRET_KEY_BASE: debe existir, si no: raise con mensaje claro
# - PORT: opcional, default "4000", convertir a integer

# Estructura sugerida:
if config_env() == :prod do
  database_url =
    System.get_env("DATABASE_URL") ||
      raise """
      environment variable DATABASE_URL is missing.
      Example: postgres://user:pass@host/dbname
      """

  # TODO: Lee SECRET_KEY_BASE de la misma forma

  port = String.to_integer(System.get_env("PORT", "4000"))

  config :my_app, :database_url, database_url
  # TODO: Configura :secret_key_base y :port también
end

# TODO: Lee FEATURE_NEW_UI (boolean) con default false
# System.get_env retorna string "true" o nil — conviértelo a boolean
```

```bash
# Prueba que la validación funciona en producción:

# Sin las variables — debe fallar
$ MIX_ENV=prod mix run
# ** (RuntimeError) environment variable DATABASE_URL is missing.

# Con las variables — debe arrancar
$ DATABASE_URL="postgres://localhost/mydb" SECRET_KEY_BASE="my_secret" MIX_ENV=prod mix run
# OK

# Verificar que la variable de env está disponible en el código
$ LOG_LEVEL=debug iex -S mix
iex> Application.get_env(:logger, :level)
:debug
```

Expected output:
```bash
$ DATABASE_URL="ecto://user:pass@host/db" SECRET_KEY_BASE="abc123" MIX_ENV=prod mix run --no-halt
# Application starts without error

$ MIX_ENV=prod mix run --no-halt
# ** (RuntimeError) environment variable DATABASE_URL is missing.
```

---

### Exercise 4: Default Values y Valores Opcionales

Usa la forma `Application.get_env/3` para gestionar configuración opcional con valores por defecto sensatos.

```elixir
# lib/my_app/feature_flags.ex
defmodule MyApp.FeatureFlags do
  @moduledoc """
  Gestiona feature flags de la aplicación.
  Los flags se configuran en config y pueden sobreescribirse en runtime.
  """

  # TODO: Implementa enabled?/1 que:
  # - Lee Application.get_env(:my_app, :feature_flags, [])
  # - Verifica si el flag dado está en la lista
  # - Retorna true/false
  def enabled?(flag_name) do
    flags = Application.get_env(:my_app, :feature_flags, [])
    # TODO: Verifica si flag_name está en flags
  end

  # TODO: Implementa all/0 que retorna todos los feature flags activos
  def all do
    Application.get_env(:my_app, :feature_flags, [])
  end

  # TODO: Implementa rate_limit/0 que retorna el límite de requests por segundo
  # Lee :rate_limit de la configuración con default 100
  def rate_limit do
    Application.get_env(:my_app, :rate_limit, 100)
  end

  # TODO: Implementa cache_ttl/0 que retorna el TTL del caché en segundos
  # Lee :cache_ttl con default 300 (5 minutos)
  def cache_ttl do
    Application.get_env(:my_app, :cache_ttl, 300)
  end
end
```

```elixir
# config/dev.exs — Activa flags para desarrollo
config :my_app, :feature_flags, [:new_dashboard, :beta_api, :debug_panel]
config :my_app, :rate_limit, 1000    # Más permisivo en dev
config :my_app, :cache_ttl, 60       # Cache corto en dev para iterar rápido

# config/prod.exs
config :my_app, :feature_flags, [:new_dashboard]
config :my_app, :rate_limit, 100
config :my_app, :cache_ttl, 600
```

Expected output:
```elixir
# En MIX_ENV=dev
iex> MyApp.FeatureFlags.enabled?(:new_dashboard)
true

iex> MyApp.FeatureFlags.enabled?(:nonexistent_flag)
false

iex> MyApp.FeatureFlags.all()
[:new_dashboard, :beta_api, :debug_panel]

iex> MyApp.FeatureFlags.rate_limit()
1000

iex> MyApp.FeatureFlags.cache_ttl()
60
```

---

### Exercise 5: Application.put_env en Tests

Aprende a manipular la configuración en tests para aislar comportamientos.

```elixir
# lib/my_app/mailer.ex
defmodule MyApp.Mailer do
  @moduledoc "Servicio de email con configuración dinámica."

  def send_email(to, subject, body) do
    if Application.get_env(:my_app, :email_enabled, true) do
      adapter = Application.get_env(:my_app, :email_adapter, :smtp)
      do_send(adapter, to, subject, body)
    else
      {:ok, :skipped}
    end
  end

  defp do_send(:smtp, to, subject, _body) do
    # Simulación de envío SMTP
    {:ok, "Email sent to #{to}: #{subject}"}
  end

  defp do_send(:mock, to, subject, _body) do
    {:ok, "Mock sent to #{to}: #{subject}"}
  end
end

# test/my_app/mailer_test.exs
defmodule MyApp.MailerTest do
  use ExUnit.Case

  # TODO: En setup, usa Application.put_env para:
  # - Desactivar email (:email_enabled, false) en algunos tests
  # - Cambiar el adapter a :mock para evitar envíos reales
  # Usa on_exit para restaurar los valores originales

  setup do
    # TODO: Guarda los valores originales de :email_enabled y :email_adapter
    # TODO: En on_exit, restaura esos valores
    # TODO: Configura :email_adapter a :mock para los tests
    :ok
  end

  # TODO: Escribe test "skips sending when email disabled"
  # Configura :email_enabled a false con Application.put_env
  # Llama Mailer.send_email y verifica que retorna {:ok, :skipped}

  # TODO: Escribe test "sends via mock adapter"
  # Llama Mailer.send_email con el adapter :mock configurado
  # Verifica que retorna {:ok, mensaje_con_mock}
end
```

Expected output:
```bash
$ mix test test/my_app/mailer_test.exs
..
Finished in 0.05 seconds
2 tests, 0 failures
```

---

## Try It Yourself

Diseña la configuración completa para una aplicación web con database, secretos, y feature flags separada por ambiente. Sin solución incluida.

```elixir
# Configura una app llamada :shop_api con:
#
# config.exs (base):
# - :api_version -> "v1"
# - :pagination -> %{default_page_size: 20, max_page_size: 100}
# - :features -> [] (vacío por defecto)
#
# dev.exs:
# - :database_url -> URL local PostgreSQL
# - :log_level -> :debug
# - :features -> [:admin_panel, :dev_tools]
#
# test.exs:
# - :database_url -> URL de base de datos de test
# - :log_level -> :warning
# - :async -> false (para tests determinísticos)
#
# prod.exs:
# - :log_level -> :error
# - :features -> [:premium_checkout]
#
# runtime.exs:
# - DATABASE_URL (obligatorio en prod)
# - SECRET_KEY_BASE (obligatorio en prod)
# - REDIS_URL (opcional, default "redis://localhost:6379")
# - ENABLE_METRICS (boolean, default false)
# - MAX_CONNECTIONS (integer, default 10)
#
# Además, implementa un módulo ShopApi.Config con funciones:
# - database_url/0
# - secret_key/0
# - feature_enabled?/1
# - pagination_opts/0 -> retorna la config de paginación
# - max_connections/0

# Ejecuta en los tres ambientes y verifica que los valores son correctos:
# MIX_ENV=dev iex -S mix
# MIX_ENV=test mix test
# DATABASE_URL=... SECRET_KEY_BASE=... MIX_ENV=prod mix run
```

---

## Common Mistakes

### Mistake 1: Leer variables de entorno en config.exs en lugar de runtime.exs

**Wrong:**
```elixir
# config/config.exs — INCORRECTO
import Config
# System.get_env se evalúa en compile-time — no en runtime del release
config :my_app, :db_url, System.get_env("DATABASE_URL")
```
**Why:** En un release, `config.exs` se evalúa al compilar, no al arrancar. La variable de entorno puede no estar disponible en el momento de la compilación, y aunque lo esté, el valor queda fijo en el binario.
**Fix:**
```elixir
# config/runtime.exs — CORRECTO
import Config
# runtime.exs se evalúa cuando la aplicación arranca
config :my_app, :db_url, System.get_env("DATABASE_URL") || raise "missing DATABASE_URL"
```

### Mistake 2: No restaurar Application.put_env en tests

**Wrong:**
```elixir
test "with feature enabled" do
  Application.put_env(:my_app, :feature, true)
  # Test sin cleanup — el valor persiste para el siguiente test
  assert MyModule.feature_active?()
end

test "with feature disabled" do
  # Este test podría fallar por el valor dejado por el test anterior
  refute MyModule.feature_active?()
end
```
**Why:** `Application.put_env` modifica el estado global de la aplicación. Si no se restaura, contamina tests que se ejecutan después.
**Fix:**
```elixir
setup do
  original = Application.get_env(:my_app, :feature)
  on_exit(fn -> Application.put_env(:my_app, :feature, original) end)
  :ok
end
```

### Mistake 3: Hardcodear secretos en archivos de configuración

**Wrong:**
```elixir
# config/prod.exs — NUNCA hagas esto
config :my_app, :secret_key_base,
  "xvafzY4y01jYuzLm3ecJqo008dVnU3CN4f+MamNd28xtzOwGjC/1TK0oxca+Ra0="

config :my_app, :database_url,
  "postgres://admin:supersecretpassword@db.prod.example.com/myapp"
```
**Why:** Los archivos de configuración van al repositorio git. Los secretos hardcodeados quedan en el historial permanentemente, incluso si luego los eliminas.
**Fix:**
```elixir
# config/runtime.exs — leer de variables de entorno
config :my_app, :secret_key_base,
  System.get_env("SECRET_KEY_BASE") || raise "missing SECRET_KEY_BASE"

config :my_app, :database_url,
  System.get_env("DATABASE_URL") || raise "missing DATABASE_URL"
```

---

## Verification

```bash
# Verificar configuración en cada ambiente
$ MIX_ENV=dev iex -S mix
iex> Application.get_env(:my_app, :log_level)
:debug

$ MIX_ENV=test mix test
# Tests pasan con configuración de test

$ MIX_ENV=prod mix compile
# Compila sin error (runtime.exs no se evalúa en compile-time)

# Verificar que runtime.exs valida variables de entorno
$ MIX_ENV=prod mix run --no-halt
# ** (RuntimeError) environment variable DATABASE_URL is missing.

$ DATABASE_URL="postgres://..." MIX_ENV=prod mix run --no-halt
# Arranca correctamente
```

## Summary
- **Key concepts**: `config.exs`, `dev.exs`, `prod.exs`, `runtime.exs`, `Application.get_env/3`, `Application.put_env/4`, `config_env()`
- **What you practiced**: Configuración base y por ambiente, lectura de env vars en runtime, valores por defecto, feature flags, manipulación de config en tests
- **Important to remember**: `runtime.exs` es el único lugar correcto para leer env vars en producción. Nunca hardcodear secretos en archivos de config. Siempre restaurar `Application.put_env` en `on_exit` cuando se usa en tests.

## What's Next
En el siguiente ejercicio **21-releases-mix-basico** aprenderás a crear releases de producción con `mix release` — binarios autocontenidos que no requieren Elixir instalado en el servidor de destino.

## Resources
- [Config Module](https://hexdocs.pm/elixir/Config.html)
- [Application Module](https://hexdocs.pm/elixir/Application.html)
- [Releases and Config — Mix](https://hexdocs.pm/mix/Mix.Tasks.Release.html)
- [Runtime Configuration Guide](https://hexdocs.pm/phoenix/releases.html#runtime-configuration)
