# 45 — Build an API Client Wrapper (Capstone)

**Difficulty**: Avanzado  
**Tiempo estimado**: 5-7 horas  
**Área**: HTTP · Circuit Breaker · Telemetry · Finch · Concurrencia

---

## Contexto

Un cliente HTTP de producción es mucho más que `HTTPoison.get/1`. Necesita reintentos inteligentes,
un circuit breaker que evite cascadas de fallos, observabilidad via telemetry, connection pooling
eficiente y rate limiting para respetar los límites de las APIs externas. Este capstone integra
todos estos patrones en un cliente cohesivo y configurable.

---

## Arquitectura propuesta

```
┌────────────────────────────────────────────────────────────┐
│                   MyClient (facade)                        │
│   get/3  post/3  put/3  delete/2                           │
└────────────────┬───────────────────────────────────────────┘
                 │
┌────────────────▼───────────────────────────────────────────┐
│              RequestPipeline                               │
│                                                            │
│  1. RateLimiter.check(host)      — token bucket           │
│  2. CircuitBreaker.call(host, fn) — open/closed/half-open │
│  3. RetryWrapper.with_retry(fn)  — backoff on failure     │
│  4. Finch.request(...)           — HTTP real              │
│  5. Telemetry.emit(...)          — métricas               │
└────────────────────────────────────────────────────────────┘
         │             │              │
┌────────▼──┐  ┌───────▼──┐  ┌───────▼──────────┐
│ Finch     │  │ Circuit   │  │ RateLimiter      │
│ Pool      │  │ Breaker   │  │ (token bucket)   │
│ (per host)│  │ (ETS)    │  │ (ETS + timer)    │
└───────────┘  └──────────┘  └──────────────────┘
```

### Configuración del cliente

```elixir
# config/config.exs
config :my_client,
  pools: [
    default: [size: 10, count: 1],
    "api.stripe.com": [size: 25, count: 2]
  ],
  circuit_breaker: [
    failure_threshold: 5,    # abre después de 5 fallos
    recovery_timeout_ms: 30_000,
    half_open_max_calls: 3
  ],
  retry: [
    max_retries: 3,
    base_delay_ms: 100,
    max_delay_ms: 5_000,
    retryable_statuses: [429, 500, 502, 503, 504]
  ],
  rate_limit: [
    default: {100, :per_second},   # 100 req/s global
    "api.stripe.com": {25, :per_second}
  ]
```

---

## Ejercicio 1 — HTTP client base con Finch

Implementa el cliente HTTP sobre Finch con opciones configurables.

### Interfaz pública

```elixir
MyClient.get("https://api.example.com/users/1")
# => {:ok, %{status: 200, body: %{"id" => 1, "name" => "Alice"}, headers: [...]}}

MyClient.get("https://api.example.com/users/1",
  headers: [{"Authorization", "Bearer token"}],
  timeout_ms: 5_000,
  decode_json: true   # default true si Content-Type es application/json
)

MyClient.post("https://api.example.com/users",
  %{name: "Bob", email: "bob@example.com"},
  headers: [{"Authorization", "Bearer token"}]
)
# => {:ok, %{status: 201, body: %{...}}}

MyClient.delete("https://api.example.com/users/1")
# => {:ok, %{status: 204, body: nil}}
```

### Implementación base

```elixir
defmodule MyClient do
  @moduledoc "HTTP client con retry, circuit breaker y telemetry"

  def get(url, opts \\ []),           do: request(:get, url, nil, opts)
  def post(url, body, opts \\ []),    do: request(:post, url, body, opts)
  def put(url, body, opts \\ []),     do: request(:put, url, body, opts)
  def delete(url, opts \\ []),        do: request(:delete, url, nil, opts)

  defp request(method, url, body, opts) do
    uri = URI.parse(url)
    host = uri.host

    start_time = System.monotonic_time()

    result =
      with :ok <- MyClient.RateLimiter.check(host),
           {:ok, response} <- MyClient.CircuitBreaker.call(host, fn ->
             MyClient.RetryWrapper.with_retry(fn ->
               do_http_request(method, url, body, opts)
             end, opts)
           end) do
        {:ok, response}
      end

    duration = System.monotonic_time() - start_time
    emit_telemetry(method, host, result, duration)

    result
  end
end
```

### Requisitos

- Finch pool por host (pre-configurado o creado dinámicamente en primer uso)
- Decodificación JSON automática basada en `Content-Type: application/json`
- Timeout configurable por request (default 30s)
- Serialización automática del body a JSON si es un Map
- Tests con Bypass: simular respuestas HTTP sin servidor real

---

## Ejercicio 2 — Retry con backoff y retryable conditions

Implementa retry inteligente que distingue errores recuperables de no recuperables.

### Condiciones de retry

```elixir
# Reintentar:
# - Timeout de conexión
# - HTTP 429 (Too Many Requests) — respetar Retry-After header
# - HTTP 500, 502, 503, 504
# - Errores de red (connection refused, DNS failure)

# NO reintentar:
# - HTTP 400 (Bad Request)
# - HTTP 401, 403 (Auth errors)
# - HTTP 404 (Not Found)
# - HTTP 422 (Unprocessable Entity)
```

### Implementación del wrapper

```elixir
defmodule MyClient.RetryWrapper do
  def with_retry(fun, opts \\ []) do
    max_retries = Keyword.get(opts, :max_retries, 3)
    do_retry(fun, 0, max_retries)
  end

  defp do_retry(fun, attempt, max) when attempt >= max do
    fun.()  # último intento sin capturar
  end

  defp do_retry(fun, attempt, max) do
    case fun.() do
      {:ok, %{status: status}} = result when status in [200, 201, 204] ->
        result
      {:ok, %{status: 429, headers: headers}} ->
        delay = extract_retry_after(headers) || backoff(attempt)
        Process.sleep(delay)
        do_retry(fun, attempt + 1, max)
      {:ok, %{status: status}} when status in [500, 502, 503, 504] ->
        Process.sleep(backoff(attempt))
        do_retry(fun, attempt + 1, max)
      {:error, _} = err when attempt < max ->
        Process.sleep(backoff(attempt))
        do_retry(fun, attempt + 1, max)
      result ->
        result
    end
  end

  defp backoff(attempt), do: min(100 * :math.pow(2, attempt) |> round(), 5_000)

  defp extract_retry_after(headers) do
    case List.keyfind(headers, "retry-after", 0) do
      {_, seconds} -> String.to_integer(seconds) * 1_000
      nil          -> nil
    end
  end
end
```

### Requisitos

- `Retry-After` header tiene prioridad sobre backoff calculado
- Jitter en el backoff para evitar thundering herd
- El número máximo de reintentos es configurable por request y globalmente
- Tests con Bypass: simular 2 fallos 503 seguidos de 200, verificar 3 intentos totales
- Tests: 429 con Retry-After, no-retry en 400/401/404

---

## Ejercicio 3 — Circuit Breaker

Implementa el patrón circuit breaker para proteger el sistema de fallos en cascada.

### Estados del circuit breaker

```
CLOSED (normal)
  ↓ failure_count >= threshold
OPEN (rechaza requests)
  ↓ después de recovery_timeout
HALF_OPEN (prueba si el servicio recuperó)
  ↓ N llamadas exitosas consecutivas
CLOSED (recuperado)
```

### Implementación con ETS

```elixir
defmodule MyClient.CircuitBreaker do
  @table :circuit_breaker_state

  def init do
    :ets.new(@table, [:set, :public, :named_table, read_concurrency: true])
  end

  def call(host, fun) do
    case get_state(host) do
      :closed    -> execute_and_track(host, fun)
      :open      -> check_if_ready_to_probe(host, fun)
      :half_open -> execute_half_open(host, fun)
    end
  end

  defp get_state(host) do
    case :ets.lookup(@table, host) do
      [{^host, :open, opened_at}] ->
        now = System.monotonic_time(:millisecond)
        if now - opened_at >= recovery_timeout_ms() do
          :half_open
        else
          :open
        end
      [{^host, state, _}] -> state
      [] -> :closed
    end
  end

  defp execute_and_track(host, fun) do
    case fun.() do
      {:ok, _} = result ->
        reset_failure_count(host)
        result
      {:error, _} = err ->
        increment_failure_count(host)
        maybe_open_circuit(host)
        err
    end
  end
end
```

### Requisitos

- Estado en ETS (`[:set, :public, :named_table]`) para acceso concurrente sin GenServer
- `HALF_OPEN`: permite pasar `half_open_max_calls` para probar recuperación
- Si en `HALF_OPEN` falla → vuelve a `OPEN` con timestamp renovado
- `CircuitBreaker.status(host)` retorna `{:open, remaining_ms}` o `:closed` o `:half_open`
- Tests: secuencia failure_threshold fallos → estado open → timeout → half_open → éxito → closed

---

## Ejercicio 4 — Rate Limiting y Telemetry

Token bucket rate limiter y eventos telemetry para observabilidad.

### Rate Limiter (token bucket)

```elixir
defmodule MyClient.RateLimiter do
  # Token bucket por host
  # Configuración: {max_tokens, refill_rate, refill_interval_ms}

  def check(host) do
    case consume_token(host) do
      :ok              -> :ok
      {:error, :empty} -> {:error, :rate_limited}
    end
  end

  defp consume_token(host) do
    # ETS atomic operations para thread safety
    rate = get_rate(host)
    now  = System.monotonic_time(:millisecond)

    case :ets.lookup(:rate_limiter, host) do
      [{^host, tokens, last_refill}] ->
        refilled = calculate_refill(tokens, last_refill, now, rate)
        if refilled >= 1 do
          :ets.insert(:rate_limiter, {host, refilled - 1, now})
          :ok
        else
          {:error, :empty}
        end
      [] ->
        :ets.insert(:rate_limiter, {host, rate.max_tokens - 1, now})
        :ok
    end
  end
end
```

### Telemetry events

```elixir
# Emitido en cada request completado:
:telemetry.execute(
  [:my_client, :request, :stop],
  %{duration: duration_ns, status: status_code},
  %{method: method, host: host, url: url, retry_count: retries}
)

# Emitido cuando el circuit breaker cambia de estado:
:telemetry.execute(
  [:my_client, :circuit_breaker, :state_change],
  %{},
  %{host: host, from: :closed, to: :open}
)

# Emitido cuando rate limited:
:telemetry.execute(
  [:my_client, :rate_limiter, :throttled],
  %{},
  %{host: host}
)
```

### Handler de telemetry para logging

```elixir
defmodule MyClient.TelemetryHandler do
  def attach do
    :telemetry.attach_many(
      "my-client-logger",
      [
        [:my_client, :request, :stop],
        [:my_client, :circuit_breaker, :state_change],
        [:my_client, :rate_limiter, :throttled]
      ],
      &handle_event/4,
      nil
    )
  end

  def handle_event([:my_client, :request, :stop], measurements, metadata, _config) do
    duration_ms = System.convert_time_unit(measurements.duration, :native, :millisecond)
    require Logger
    Logger.info("HTTP #{metadata.method} #{metadata.host} #{metadata.status} #{duration_ms}ms")
  end
end
```

### Requisitos

- Token bucket implementado con operaciones atómicas en ETS (no GenServer)
- Rate por host configurable; default configurable globalmente
- Telemetry emitido incluso en errores (status: 0 si no hubo respuesta HTTP)
- `duration` en nanosegundos (convención de `:telemetry`)
- Tests de telemetry: usar `:telemetry.attach` en setup del test, verificar eventos emitidos

### Estructura del proyecto

```
lib/
├── my_client/
│   ├── application.ex          # Inicia Finch + inicializa ETS tables
│   ├── request_pipeline.ex     # Orquesta rate limit → CB → retry → HTTP
│   ├── circuit_breaker.ex      # ETS-based circuit breaker
│   ├── rate_limiter.ex         # Token bucket en ETS
│   ├── retry_wrapper.ex        # Backoff + retryable conditions
│   └── telemetry_handler.ex    # Attach + handle telemetry events
test/
├── my_client/
│   ├── client_test.exs         # Tests de integración con Bypass
│   ├── circuit_breaker_test.exs
│   ├── rate_limiter_test.exs
│   ├── retry_wrapper_test.exs
│   └── telemetry_test.exs
```

---

## Criterios de aceptación

- [ ] `get/post/put/delete` funcionan correctamente con Finch
- [ ] JSON decode automático basado en Content-Type
- [ ] Retry no ocurre en 400/401/404; sí ocurre en 429/500-504
- [ ] `Retry-After` header es respetado en respuestas 429
- [ ] Circuit breaker abre después de `failure_threshold` fallos consecutivos
- [ ] Circuit breaker entra en `HALF_OPEN` después de `recovery_timeout_ms`
- [ ] Rate limiter bloquea requests que superan el límite configurado
- [ ] Telemetry emite `[:my_client, :request, :stop]` en cada request
- [ ] Tests usan Bypass para simular todas las condiciones sin servidor real

---

## Retos adicionales (opcional)

- Caché de respuestas: cachear respuestas GET con ETag/Last-Modified
- Mock automático en tests: `MyClient.Mock` que implementa la misma interfaz
- Métricas en Prometheus via `telemetry_metrics` + `telemetry_poller`
- Adaptive concurrency: reducir automáticamente concurrencia cuando latencia sube
