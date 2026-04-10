# Ejercicio 70: Req y Finch — HTTP Clients de Producción

**Nivel**: Avanzado  
**Tiempo estimado**: 60–90 min  
**Módulo**: HTTP, networking y resiliencia  

---

## Contexto

Construyes la capa de integración de un sistema que consume múltiples APIs
externas: un proveedor de pagos, una API de geolocalización y un servicio
de almacenamiento de archivos. Necesitas:

- Connection pooling para no saturar los file descriptors del OS
- Retry automático con backoff exponencial para errores transitorios
- Streaming de archivos grandes sin cargar en memoria
- Telemetría para observar latencias por endpoint

**Finch** es el cliente HTTP de bajo nivel que gestiona los connection pools.
**Req** es el cliente de alto nivel que construye sobre Finch y proporciona
middleware, retry, autenticación y composición de requests.

---

## Setup

```bash
mix new http_integrations --sup
cd http_integrations
```

**`mix.exs`**:
```elixir
defp deps do
  [
    {:req, "~> 0.5"},
    {:finch, "~> 0.19"},
    {:jason, "~> 1.4"},
    {:telemetry, "~> 1.2"}
  ]
end
```

**`lib/http_integrations/application.ex`**:
```elixir
defmodule HttpIntegrations.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # Pool de conexiones para la API de pagos (HTTP/1.1, alta concurrencia)
      {Finch,
       name: PaymentsFinch,
       pools: %{
         "https://api.payments.com" => [
           size: 50,
           count: 5,
           protocol: :http1
         ]
       }},

      # Pool para API de archivos (HTTP/2 multiplexing)
      {Finch,
       name: StorageFinch,
       pools: %{
         "https://storage.example.com" => [
           size: 10,
           count: 1,
           protocol: :http2
         ]
       }}
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: HttpIntegrations.Supervisor)
  end
end
```

---

## Parte 1: API Client con Req y Middleware

Un cliente completo para una API REST con autenticación, retry y telemetría.

**`lib/http_integrations/payments_client.ex`**:
```elixir
defmodule HttpIntegrations.PaymentsClient do
  @moduledoc """
  Cliente para la API de pagos.

  Construye sobre Req con:
  - Auth Bearer automática
  - Retry en errores 429, 500, 502, 503, 504
  - Backoff exponencial entre reintentos
  - Timeout de 10 segundos
  - Telemetría por endpoint
  """

  @base_url "https://api.payments.com/v2"

  defp client do
    Req.new(
      base_url: @base_url,
      finch: PaymentsFinch,
      # Req maneja automáticamente JSON: encode body y decode response
      decode_body: true,
      # Timeout de conexión + lectura
      connect_options: [timeout: 5_000],
      receive_timeout: 10_000,
      # Auth en cada request
      auth: {:bearer, api_key()},
      # Retry en errores HTTP transitorios y errores de red
      retry: :transient,
      max_retries: 3,
      retry_delay: &backoff/1,
      # Headers comunes
      headers: [
        {"accept", "application/json"},
        {"content-type", "application/json"},
        {"x-client-version", "1.0"}
      ]
    )
  end

  # Backoff exponencial: 1s, 2s, 4s
  defp backoff(attempt), do: :timer.seconds(2 ** attempt)

  @doc "Crea un cargo. Devuelve `{:ok, charge}` o `{:error, reason}`."
  def create_charge(amount, currency, source_token) do
    body = %{
      amount: amount,
      currency: currency,
      source: source_token,
      # Clave de idempotencia para evitar dobles cobros en caso de retry
      idempotency_key: idempotency_key(amount, source_token)
    }

    case Req.post(client(), url: "/charges", json: body) do
      {:ok, %{status: 201, body: charge}} ->
        {:ok, charge}

      {:ok, %{status: 422, body: %{"error" => reason}}} ->
        # Error de validación — no hacer retry
        {:error, {:validation, reason}}

      {:ok, %{status: status, body: body}} ->
        {:error, {:unexpected_status, status, body}}

      {:error, reason} ->
        # Error de red tras agotar reintentos
        {:error, {:network, reason}}
    end
  end

  @doc "Obtiene el historial de cargos de un cliente con paginación automática."
  def list_charges(customer_id, opts \\ []) do
    limit = Keyword.get(opts, :limit, 100)

    case Req.get(client(), url: "/customers/#{customer_id}/charges", params: [limit: limit]) do
      {:ok, %{status: 200, body: %{"data" => charges}}} -> {:ok, charges}
      {:ok, %{status: 404}} -> {:error, :not_found}
      {:ok, resp} -> {:error, {:unexpected, resp}}
      {:error, reason} -> {:error, {:network, reason}}
    end
  end

  defp api_key, do: Application.fetch_env!(:http_integrations, :payments_api_key)

  defp idempotency_key(amount, token) do
    :crypto.hash(:sha256, "#{amount}-#{token}") |> Base.encode16(case: :lower)
  end
end
```

---

## Parte 2: Middleware Personalizado con Req.Steps

Req permite componer pasos (steps) que transforman el request antes de enviarlo
o la response después de recibirla. Implementa un step de telemetría y uno de caché.

**`lib/http_integrations/req_steps.ex`**:
```elixir
defmodule HttpIntegrations.ReqSteps do
  @moduledoc """
  Steps personalizados para Req.

  Un step es una función `{request_fn, response_fn}` donde:
  - `request_fn(req)` transforma el request antes de enviarlo
  - `response_fn({req, resp})` transforma la response al recibirla

  Se añaden con `Req.Request.prepend_request_steps/2` o
  `Req.Request.append_response_steps/2`.
  """

  require Logger

  @doc """
  Step de telemetría: emite eventos antes y después de cada request.

  Uso:
    Req.new() |> Req.Request.prepend_request_steps(telemetry: &ReqSteps.telemetry/1)
  """
  def telemetry(request) do
    start = System.monotonic_time()
    metadata = %{url: request.url, method: request.method}

    :telemetry.execute([:http_client, :request, :start], %{system_time: System.system_time()}, metadata)

    {request,
     fn {req, resp} ->
       duration = System.monotonic_time() - start

       :telemetry.execute(
         [:http_client, :request, :stop],
         %{duration: duration},
         Map.put(metadata, :status, resp.status)
       )

       {req, resp}
     end}
  end

  @doc """
  Step de caché ETS: cachea GETs exitosos por N segundos.

  Uso:
    Req.new() |> Req.Request.prepend_request_steps(cache: &ReqSteps.ets_cache/1)
  """
  def ets_cache(request) do
    if request.method == :get do
      key = cache_key(request)

      case :ets.lookup(:req_cache, key) do
        [{^key, response, expires_at}] when expires_at > System.monotonic_time(:second) ->
          # Cache hit: cortocircuitar el request
          {Req.Request.halt(request), response}

        _ ->
          # Cache miss: continuar y cachear la respuesta
          {request,
           fn {req, resp} ->
             if resp.status == 200 do
               ttl = Req.Request.get_option(req, :cache_ttl, 60)
               expires = System.monotonic_time(:second) + ttl
               :ets.insert(:req_cache, {cache_key(req), resp, expires})
             end

             {req, resp}
           end}
      end
    else
      request
    end
  end

  defp cache_key(request) do
    URI.to_string(request.url)
  end
end
```

---

## Parte 3: Streaming de Archivos Grandes

Descarga un archivo de múltiples GB sin cargarlo en memoria del proceso.
Req soporta streaming mediante el parámetro `into:`.

**`lib/http_integrations/file_downloader.ex`**:
```elixir
defmodule HttpIntegrations.FileDownloader do
  @moduledoc """
  Descarga archivos grandes con streaming directo a disco.

  Usa Finch con HTTP/2 para máximo throughput. El archivo nunca
  se almacena en memoria del proceso BEAM: los chunks van directamente
  del socket al file descriptor.
  """

  require Logger

  @doc """
  Descarga `url` a `dest_path` con seguimiento de progreso.

  Devuelve `{:ok, bytes_written}` o `{:error, reason}`.
  """
  def download(url, dest_path, opts \\ []) do
    timeout = Keyword.get(opts, :timeout, :timer.minutes(30))
    on_progress = Keyword.get(opts, :on_progress, fn _bytes -> :ok end)

    # File.stream! abre el archivo en modo :write y :binary.
    # Req escribe cada chunk directamente al stream sin bufferizar.
    dest_stream = File.stream!(dest_path, [:write, :binary])

    # Contador de bytes acumulado en closure mutable vía Agent
    {:ok, counter} = Agent.start_link(fn -> 0 end)

    collecting_stream =
      Stream.each(dest_stream, fn _chunk ->
        # El stream no nos da el chunk aquí — esto es para el progreso.
        # Req llama into: con cada chunk; ver más abajo.
        :ok
      end)

    # `into:` puede ser un Stream, un collectable, o una función.
    # Con una función, recibimos cada chunk y podemos acumular bytes.
    into_fn =
      fn {:data, chunk}, acc ->
        bytes = byte_size(chunk)
        total = Agent.get_and_update(counter, fn n -> {n + bytes, n + bytes} end)
        on_progress.(total)
        IO.binwrite(acc, chunk)
        {:cont, acc}
      end

    result =
      Req.get(
        url: url,
        finch: StorageFinch,
        receive_timeout: timeout,
        into: into_fn,
        # No decodificar: queremos los bytes en bruto
        decode_body: false
      )

    bytes_written = Agent.get(counter, & &1)
    Agent.stop(counter)

    case result do
      {:ok, %{status: 200}} ->
        Logger.info("Descarga completada", path: dest_path, bytes: bytes_written)
        {:ok, bytes_written}

      {:ok, %{status: status}} ->
        File.rm(dest_path)
        {:error, {:http_error, status}}

      {:error, reason} ->
        File.rm(dest_path)
        {:error, {:network, reason}}
    end
  end

  @doc """
  Descarga con reanudación (Range requests).

  Si el archivo existe parcialmente, continúa desde el byte que toca.
  """
  def resume_download(url, dest_path) do
    already_downloaded =
      case File.stat(dest_path) do
        {:ok, %{size: size}} -> size
        {:error, _} -> 0
      end

    headers =
      if already_downloaded > 0 do
        [{"range", "bytes=#{already_downloaded}-"}]
      else
        []
      end

    stream = File.stream!(dest_path, [:append, :binary])

    case Req.get(url: url, finch: StorageFinch, headers: headers, into: stream) do
      {:ok, %{status: status}} when status in [200, 206] ->
        :ok

      {:ok, %{status: 416}} ->
        # 416 = Range Not Satisfiable: el archivo ya está completo
        :ok

      {:ok, %{status: status}} ->
        {:error, {:http_error, status}}

      {:error, reason} ->
        {:error, reason}
    end
  end
end
```

---

## Parte 4: Pool Finch Avanzado con Métricas

Configura Finch con pools diferenciados por dominio y expón métricas
de conexiones activas mediante telemetría.

**`lib/http_integrations/finch_telemetry.ex`**:
```elixir
defmodule HttpIntegrations.FinchTelemetry do
  @moduledoc """
  Adjunta handlers de telemetría para métricas de Finch.

  Finch emite eventos en:
  - [:finch, :request, :start]
  - [:finch, :request, :stop]
  - [:finch, :request, :exception]
  - [:finch, :connect, :start / :stop]
  - [:finch, :send, :start / :stop]
  - [:finch, :recv, :start / :stop]
  """

  require Logger

  def attach do
    :telemetry.attach_many(
      "finch-pool-metrics",
      [
        [:finch, :request, :stop],
        [:finch, :request, :exception],
        [:finch, :connect, :stop]
      ],
      &handle_event/4,
      nil
    )
  end

  defp handle_event([:finch, :request, :stop], measurements, meta, _) do
    Logger.info("HTTP request completado",
      method: meta.method,
      url: URI.to_string(meta.url),
      status: meta.status,
      duration_ms: to_ms(measurements.duration)
    )
  end

  defp handle_event([:finch, :request, :exception], _measurements, meta, _) do
    Logger.error("HTTP request excepción",
      method: meta.method,
      url: URI.to_string(meta.url),
      reason: inspect(meta.reason)
    )
  end

  defp handle_event([:finch, :connect, :stop], measurements, meta, _) do
    Logger.debug("Conexión TCP establecida",
      host: meta.host,
      port: meta.port,
      duration_ms: to_ms(measurements.duration)
    )
  end

  defp to_ms(native), do: System.convert_time_unit(native, :native, :millisecond)
end
```

---

## Ejercicios Propuestos

### Ejercicio A: Autenticación OAuth2 con refresh automático

Implementa un step Req que almacene el token de acceso en un GenServer.
Cuando la respuesta sea 401, el step debe:
1. Llamar al endpoint de refresh token
2. Actualizar el GenServer con el nuevo token
3. Reintentar el request original con el nuevo token

El step debe usar `Req.Request.halt/1` para cortocircuitar el pipeline
y `Req.Request.prepend_request_steps/2` para modificar el request reintentado.

### Ejercicio B: Pool adaptativo por latencia

Crea un GenServer que monitorice los eventos de telemetría `[:finch, :request, :stop]`
y ajuste el tamaño del pool Finch dinámicamente. Si el percentil 95 de latencia
supera 1 segundo, duplica el pool. Si baja del percentil 50 a menos de 100ms,
reduce el pool a la mitad. Investiga `Finch.request/3` y `Finch` internals.

### Ejercicio C: Upload multiparte con progreso

Implementa un upload de archivo grande usando multipart form-data.
Req soporta `multipart:` como opción. El challenge es emitir eventos
de progreso durante el upload (no solo la descarga). Investiga
`Req.Request` y cómo iterar sobre un stream de lectura de archivo
para calcular bytes enviados.

---

## Tests

```elixir
defmodule HttpIntegrations.PaymentsClientTest do
  use ExUnit.Case, async: true

  import Req.Test

  setup do
    # Stub HTTP en tests: Req.Test intercepta las llamadas
    stub(PaymentsFinch, fn conn ->
      case conn.request_path do
        "/v2/charges" ->
          Plug.Conn.send_resp(conn, 201, Jason.encode!(%{id: "ch_123", status: "succeeded"}))

        "/v2/customers/999/charges" ->
          Plug.Conn.send_resp(conn, 404, Jason.encode!(%{error: "not found"}))
      end
    end)

    :ok
  end

  test "crea un cargo exitosamente" do
    assert {:ok, %{"id" => "ch_123"}} =
             HttpIntegrations.PaymentsClient.create_charge(1000, "eur", "tok_visa")
  end

  test "devuelve :not_found para cliente inexistente" do
    assert {:error, :not_found} =
             HttpIntegrations.PaymentsClient.list_charges(999)
  end
end
```

---

## Preguntas de Comprensión

1. ¿Por qué se crean dos instancias Finch separadas (`PaymentsFinch` y `StorageFinch`)
   en lugar de una sola? ¿Qué pasaría si las APIs compartieran el mismo pool?

2. El `retry: :transient` de Req reintenta en errores 429, 500, 502, 503, 504
   y errores de red. ¿Por qué NO se debería reintentar un 422 automáticamente?
   ¿Qué riesgo introduce reintentar un POST sin `idempotency_key`?

3. En `FileDownloader.download/3`, el `into:` recibe cada chunk del body.
   ¿Qué ocurre con la memoria si el chunk de Finch es de 64KB pero
   el archivo es de 10GB? ¿Por qué esto es seguro?

4. HTTP/2 multiplexing permite múltiples requests simultáneos sobre
   una sola conexión TCP. ¿Por qué configuramos `size: 10, count: 1`
   para `StorageFinch` en lugar de `size: 1, count: 10`?

---

## Recursos

- [Req Docs](https://hexdocs.pm/req)
- [Finch Docs](https://hexdocs.pm/finch)
- [Req.Test — HTTP mocking](https://hexdocs.pm/req/Req.Test.html)
- [Finch Telemetry events](https://hexdocs.pm/finch/Finch.html#module-telemetry)
- [HTTP/2 vs HTTP/1.1 — pooling strategies](https://httpwg.org/specs/rfc7540.html)
