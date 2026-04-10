# Ejercicio 57: Plug Pipeline y Middleware

## Objetivo

Dominar la construcción de middleware HTTP en Elixir usando Plug. El patrón central es
el pipeline: cada plug recibe un `Plug.Conn`, lo transforma (o lo cortocircuita), y lo
pasa al siguiente. Entender esto te permite construir desde autenticación hasta rate
limiting sin depender de Phoenix.

## Conceptos clave

| Concepto | Rol |
|---|---|
| `Plug.Conn` | Struct inmutable que representa el estado completo del request/response |
| `Plug.Builder` | Macro para componer plugs en pipeline declarativo |
| `Plug.Router` | Router que es él mismo un plug |
| Plug function | `fn conn, opts -> conn end` — plug sin estado |
| Plug module | `init/1` + `call/2` — plug con configuración |
| `halt/1` | Marca la conn como `halted: true`, detiene el pipeline |
| `assign/3` | Añade datos al mapa `conn.assigns` (contexto del request) |
| `put_resp_header/3` | Añade header de respuesta |
| `send_resp/3` | Envía respuesta y cierra la conn |

## Setup del proyecto

```bash
mix new plug_middleware --sup
cd plug_middleware
```

`mix.exs`:
```elixir
defp deps do
  [
    {:plug_cowboy, "~> 2.7"},
    {:jason, "~> 1.4"}
  ]
end
```

```bash
mix deps.get
```

---

## Ejercicio 1: Auth Middleware — JWT Validation Plug

### Contexto

Quieres proteger rutas de tu API. Toda request que llegue sin un `Authorization: Bearer
<token>` válido debe ser rechazada con 401 antes de llegar al handler. Así el handler
nunca ve requests no autorizadas — la seguridad está en el pipeline.

### El código

`lib/plug_middleware/plugs/auth_plug.ex`:
```elixir
defmodule PlugMiddleware.Plugs.AuthPlug do
  @moduledoc """
  Valida JWT en el header Authorization.
  Si el token es válido, asigna los claims en conn.assigns.current_user.
  Si no, responde 401 y hace halt del pipeline.
  """
  import Plug.Conn

  def init(opts), do: opts

  def call(conn, _opts) do
    conn
    |> get_req_header("authorization")
    |> extract_token()
    |> verify_and_assign(conn)
  end

  # -- privado --

  defp extract_token(["Bearer " <> token | _]), do: {:ok, token}
  defp extract_token(_), do: {:error, :missing_token}

  defp verify_and_assign({:ok, token}, conn) do
    case verify_jwt(token) do
      {:ok, claims} ->
        assign(conn, :current_user, claims)

      {:error, reason} ->
        conn
        |> put_resp_content_type("application/json")
        |> send_resp(401, Jason.encode!(%{error: "Unauthorized", reason: reason}))
        |> halt()
    end
  end

  defp verify_and_assign({:error, :missing_token}, conn) do
    conn
    |> put_resp_content_type("application/json")
    |> send_resp(401, Jason.encode!(%{error: "Missing Authorization header"}))
    |> halt()
  end

  # Simulación — en producción usarías JOSE o Joken
  defp verify_jwt("valid-token-" <> user_id) do
    {:ok, %{user_id: user_id, role: "user"}}
  end

  defp verify_jwt("admin-token") do
    {:ok, %{user_id: "1", role: "admin"}}
  end

  defp verify_jwt(_token) do
    {:error, "invalid_token"}
  end
end
```

### Prueba en IEx

```elixir
iex -S mix

# Simula una conn con token válido
alias Plug.Test
alias PlugMiddleware.Plugs.AuthPlug

conn = Test.conn(:get, "/")
conn = Test.put_req_header(conn, "authorization", "Bearer valid-token-42")
result = AuthPlug.call(conn, [])

result.assigns.current_user
# => %{user_id: "42", role: "user"}
result.halted
# => false

# Sin token
conn2 = Test.conn(:get, "/")
result2 = AuthPlug.call(conn2, [])
result2.status
# => 401
result2.halted
# => true
```

### Por qué `halt/1` es importante

Sin `halt/1`, el pipeline continúa aunque ya enviaste una respuesta 401. El siguiente
plug intentaría operar sobre una conn ya respondida, causando errores o respuestas
duplicadas. `halt/1` solo pone `conn.halted = true` — es `Plug.Builder` quien revisa
esa flag y para la cadena.

```
request → AuthPlug → [HALT] → handler NO ejecutado
request → AuthPlug → assign(:current_user) → handler ejecutado
```

---

## Ejercicio 2: Rate Limiting con ETS

### Contexto

Necesitas limitar cuántas requests puede hacer una IP en una ventana de tiempo. Usas
ETS como contador compartido entre procesos (sin GenServer extra). El plug añade headers
`X-RateLimit-*` informativos y bloquea con 429 si se supera el límite.

### El código

`lib/plug_middleware/plugs/rate_limit_plug.ex`:
```elixir
defmodule PlugMiddleware.Plugs.RateLimitPlug do
  @moduledoc """
  Rate limiting por IP usando ETS.
  Ventana deslizante simple: N requests por M segundos.
  """
  import Plug.Conn

  @table :rate_limit_counters

  def init(opts) do
    limit = Keyword.get(opts, :limit, 100)
    window_seconds = Keyword.get(opts, :window_seconds, 60)
    ensure_table_exists()
    {limit, window_seconds}
  end

  def call(conn, {limit, window_seconds}) do
    ip = get_client_ip(conn)
    now = System.system_time(:second)
    window_start = now - window_seconds

    count = increment_and_clean(ip, now, window_start)
    remaining = max(0, limit - count)

    conn = conn
    |> put_resp_header("x-ratelimit-limit", Integer.to_string(limit))
    |> put_resp_header("x-ratelimit-remaining", Integer.to_string(remaining))
    |> put_resp_header("x-ratelimit-reset", Integer.to_string(now + window_seconds))

    if count > limit do
      conn
      |> put_resp_content_type("application/json")
      |> send_resp(429, Jason.encode!(%{error: "Rate limit exceeded", retry_after: window_seconds}))
      |> halt()
    else
      conn
    end
  end

  # -- privado --

  defp ensure_table_exists do
    case :ets.whereis(@table) do
      :undefined -> :ets.new(@table, [:named_table, :public, :bag])
      _ -> @table
    end
  end

  defp get_client_ip(conn) do
    # Respeta X-Forwarded-For si hay proxy
    case get_req_header(conn, "x-forwarded-for") do
      [forwarded | _] -> forwarded |> String.split(",") |> List.first() |> String.trim()
      [] -> conn.remote_ip |> :inet.ntoa() |> to_string()
    end
  end

  defp increment_and_clean(ip, now, window_start) do
    # Elimina entradas fuera de la ventana
    :ets.match_delete(@table, {ip, :"$1", :"$2"})

    # Cuenta solo las entradas dentro de la ventana
    old_entries = :ets.lookup(@table, ip)
    valid_entries = Enum.filter(old_entries, fn {_ip, ts} -> ts >= window_start end)

    # Limpia y reinserta solo las válidas + la nueva
    :ets.delete(@table, ip)
    Enum.each(valid_entries, fn entry -> :ets.insert(@table, entry) end)
    :ets.insert(@table, {ip, now})

    length(valid_entries) + 1
  end
end
```

### Prueba en IEx

```elixir
iex -S mix

alias Plug.Test
alias PlugMiddleware.Plugs.RateLimitPlug

opts = RateLimitPlug.init(limit: 3, window_seconds: 60)

# Simula 4 requests desde la misma IP
conn = %{Test.conn(:get, "/") | remote_ip: {127, 0, 0, 1}}

Enum.reduce(1..4, nil, fn i, _ ->
  result = RateLimitPlug.call(Test.conn(:get, "/") |> Map.put(:remote_ip, {127, 0, 0, 1}), opts)
  IO.puts("Request #{i}: status=#{if result.halted, do: result.status, else: "ok"}, remaining=#{Plug.Conn.get_resp_header(result, "x-ratelimit-remaining")}")
  result
end)

# Request 1: ok, remaining=2
# Request 2: ok, remaining=1
# Request 3: ok, remaining=0
# Request 4: status=429, remaining=0
```

---

## Ejercicio 3: Request ID y Structured Logging

### Contexto

En producción necesitas correlacionar logs de una misma request. El patrón estándar:
generar un UUID en la entrada, ponerlo en Logger metadata para que aparezca en todos los
logs del proceso, y devolverlo en el header de respuesta para que el cliente pueda
reportar errores.

### El código

`lib/plug_middleware/plugs/request_id_plug.ex`:
```elixir
defmodule PlugMiddleware.Plugs.RequestIdPlug do
  @moduledoc """
  Genera o propaga X-Request-ID.
  Lo inyecta en Logger metadata para correlación de logs.
  Lo devuelve en la respuesta.
  """
  import Plug.Conn
  require Logger

  @header "x-request-id"

  def init(opts), do: opts

  def call(conn, _opts) do
    request_id = get_or_generate_id(conn)

    # Inyecta en metadata del proceso actual — todos los Logger.* calls
    # dentro de este request lo incluirán automáticamente
    Logger.metadata(request_id: request_id)

    Logger.info("Request started",
      method: conn.method,
      path: conn.request_path,
      remote_ip: format_ip(conn.remote_ip)
    )

    conn
    |> assign(:request_id, request_id)
    |> put_resp_header(@header, request_id)
    |> register_before_send(&log_response(&1, request_id))
  end

  # -- privado --

  defp get_or_generate_id(conn) do
    case get_req_header(conn, @header) do
      [existing_id | _] when byte_size(existing_id) > 0 -> existing_id
      _ -> generate_id()
    end
  end

  # Base64 URL-safe de 16 bytes random — más corto que UUID completo
  defp generate_id do
    :crypto.strong_rand_bytes(16)
    |> Base.url_encode64(padding: false)
  end

  defp log_response(conn, request_id) do
    Logger.info("Request completed",
      request_id: request_id,
      status: conn.status,
      method: conn.method,
      path: conn.request_path
    )
    conn
  end

  defp format_ip(nil), do: "unknown"
  defp format_ip(ip), do: ip |> :inet.ntoa() |> to_string()
end
```

### Pipeline completo

`lib/plug_middleware/pipeline.ex`:
```elixir
defmodule PlugMiddleware.Pipeline do
  use Plug.Builder

  # El orden importa — cada plug ve la conn que el anterior devolvió
  plug PlugMiddleware.Plugs.RequestIdPlug
  plug PlugMiddleware.Plugs.RateLimitPlug, limit: 100, window_seconds: 60
  plug PlugMiddleware.Plugs.AuthPlug
  plug :dispatch

  defp dispatch(conn, _opts) do
    Plug.Conn.send_resp(conn, 200, Jason.encode!(%{
      user: conn.assigns.current_user,
      request_id: conn.assigns.request_id
    }))
  end
end
```

Iniciar el servidor:

```elixir
# lib/plug_middleware/application.ex
def start(_type, _args) do
  children = [
    {Plug.Cowboy, scheme: :http, plug: PlugMiddleware.Pipeline, options: [port: 4000]}
  ]
  Supervisor.start_link(children, strategy: :one_for_one)
end
```

### Prueba con curl

```bash
mix run --no-halt

# Sin token
curl -i http://localhost:4000/
# HTTP/1.1 401
# x-request-id: <uuid-generado>

# Con token válido
curl -i -H "Authorization: Bearer valid-token-42" http://localhost:4000/
# HTTP/1.1 200
# x-request-id: <uuid>
# x-ratelimit-remaining: 99
# {"user":{"user_id":"42","role":"user"},"request_id":"<uuid>"}

# Propaga tu propio request ID
curl -i \
  -H "Authorization: Bearer valid-token-42" \
  -H "x-request-id: my-trace-abc123" \
  http://localhost:4000/
# x-request-id: my-trace-abc123  ← el tuyo, no uno generado
```

---

## Preguntas para reflexión

1. `halt/1` no lanza una excepción — solo pone un flag. ¿Por qué es mejor este diseño
   que lanzar un error?

2. En el rate limiter, ¿qué problema tiene el enfoque de ETS con `:bag` si hay dos
   procesos OS manejando la misma request simultáneamente? ¿Cómo lo resolverías?

3. `register_before_send/2` recibe una función que se llama justo antes de enviar la
   respuesta. ¿Cuándo es esto útil versus hacer el log directamente en `call/2`?

4. ¿Qué ocurre si un plug lanza una excepción y no está capturada? ¿Qué hace Cowboy?

5. ¿Por qué `Plug.Builder` es más conveniente que encadenar manualmente `call/2` de
   cada módulo?

---

## Puntos clave

- `Plug.Conn` es inmutable — cada plug devuelve una nueva struct transformada
- `halt/1` es cooperativo: `Plug.Builder` chequea `conn.halted` entre plugs
- `register_before_send/2` permite post-procesamiento sin romper la inmutabilidad del flujo principal
- ETS es accesible desde cualquier proceso sin lock — ideal para contadores compartidos
- `Logger.metadata/1` afecta solo al proceso actual — cada request tiene su propio contexto de log
