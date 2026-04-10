# 36. Circuit Breaker Patterns para Resilience entre Servicios

**Difficulty**: Avanzado

## Prerequisites
- Dominio de GenServer y OTP supervision trees
- Experiencia con GenStateMachine o `:gen_statem`
- Comprensión de patrones de resilience (retry, timeout, fallback)
- Familiaridad con llamadas a APIs externas y manejo de errores HTTP

## Learning Objectives
After completing this exercise, you will be able to:
- Integrar la librería `Fuse` para proteger llamadas a servicios externos
- Implementar un circuit breaker custom con `GenStateMachine` y sus tres estados
- Entender las transiciones entre `:closed`, `:open` y `:half_open`
- Implementar fallback values cuando el circuito está abierto
- Aplicar el patrón bulkhead para limitar concurrencia hacia servicios externos
- Diagnosticar y configurar correctamente thresholds de failure y recovery timeout

## Concepts

### El problema: cascading failures

Sin circuit breaker, cuando un servicio externo falla:
1. Tus llamadas esperan el timeout completo (ej: 30 segundos)
2. Tienes N llamadas concurrentes, todas esperando → N * 30s de threads bloqueados
3. Tu servicio se queda sin workers → todo tu sistema cae también

El circuit breaker rompe esta cascada:

```
Estado :closed (normal):
  llamada → servicio externo
  Si N fallos en T segundos → ABRIR circuito

Estado :open (circuito cortado):
  llamada → retornar {:error, :circuit_open} INMEDIATAMENTE
  Después de recovery_timeout → pasar a :half_open

Estado :half_open (prueba):
  UNA llamada de prueba → servicio externo
  Si éxito → cerrar circuito (:closed)
  Si fallo → volver a abrir (:open)
```

### Fuse: circuit breaker battle-tested para Erlang/Elixir

```elixir
# mix.exs
{:fuse, "~> 2.4"}

# Inicializar un fuse con nombre
:fuse.install(:my_service, {{:standard, 5, 10_000}, {:reset, 60_000}})
# ^5 fallos en 10 segundos → abre; 60 segundos → prueba half_open

# Usar el fuse
case :fuse.ask(:my_service, :sync) do
  :ok ->
    # Circuito cerrado — hacer la llamada
    result = call_external_service()
    # Si falló, registrar el fallo
    case result do
      {:error, _} -> :fuse.melt(:my_service)
      _ -> result
    end

  :blown ->
    # Circuito abierto — retornar fallback
    {:error, :service_unavailable}
end
```

`fuse.ask/2` consulta el estado; `fuse.melt/1` registra un fallo.

### GenStateMachine: implementación custom

Para lógica más compleja o cuando no quieres depender de Fuse, implementa el circuit breaker como una máquina de estados:

```elixir
defmodule CircuitBreaker do
  use GenStateMachine, callback_mode: :state_functions

  # Estados: :closed, :open, :half_open
  # Datos: %{failures: 0, threshold: 5, reset_timeout: 30_000}

  def closed(:cast, {:call_result, :error}, data) do
    new_data = Map.update!(data, :failures, & &1 + 1)
    if new_data.failures >= data.threshold do
      schedule_reset(data.reset_timeout)
      {:next_state, :open, %{new_data | failures: 0}}
    else
      {:keep_state, new_data}
    end
  end

  def open(:info, :reset_timeout, data) do
    {:next_state, :half_open, data}
  end

  defp schedule_reset(timeout) do
    Process.send_after(self(), :reset_timeout, timeout)
  end
end
```

### Bulkhead Pattern: limitar concurrencia

El bulkhead evita que un servicio lento agote todos los workers. La idea: mantener un pool de "permisos" de concurrencia.

```elixir
# Con Poolboy (pool de workers):
:poolboy.transaction(:my_pool, fn worker ->
  Worker.call(worker, request)
end, timeout)

# Manual con semáforo basado en ETS counters:
defmodule Bulkhead do
  def acquire(name, max) do
    current = :ets.update_counter(:bulkhead, name, {2, 1, max, max})
    if current <= max, do: :ok, else: {:error, :at_capacity}
  end

  def release(name) do
    :ets.update_counter(:bulkhead, name, {2, -1, 0, 0})
  end
end
```

## Exercises

### Exercise 1: Fuse Integration — Proteger llamadas a API externa

Implementa un módulo que protege llamadas HTTP con Fuse circuit breaker, incluyendo fallback values y logging de transiciones.

```elixir
defmodule ExternalAPIClient do
  @moduledoc """
  Cliente HTTP protegido con Fuse circuit breaker.

  Configuración:
  - 3 fallos en 5 segundos → circuito abierto
  - 30 segundos → intento half_open
  - Fallback: valor cacheado o {:error, :circuit_open}
  """

  @fuse_name :external_api
  @fuse_options {
    {:standard, 3, 5_000},   # 3 fallos en 5 segundos → open
    {:reset, 30_000}          # 30 segundos → half_open
  }

  def install do
    # TODO: Instalar el fuse con :fuse.install/2
    # Manejar el caso en que ya esté instalado (retorna {:error, :already_installed})
    case :fuse.install(@fuse_name, @fuse_options) do
      :ok -> :ok
      {:error, :already_installed} -> :ok
    end
  end

  def get(url) do
    case :fuse.ask(@fuse_name, :sync) do
      :ok ->
        # TODO: Hacer la llamada HTTP real (o simulada)
        # Si éxito → retornar {:ok, response}
        # Si error → llamar :fuse.melt(@fuse_name), retornar {:error, reason}
        perform_request(url)

      :blown ->
        # TODO: Circuito abierto — retornar fallback
        # Loguear que el circuito está abierto
        IO.puts("[CircuitBreaker] Circuito #{@fuse_name} ABIERTO — usando fallback")
        {:error, :circuit_open}
    end
  end

  defp perform_request(url) do
    # Simulación de llamada HTTP que falla aleatoriamente
    case simulate_http_call(url) do
      {:ok, response} ->
        {:ok, response}

      {:error, reason} ->
        # TODO: Registrar el fallo en Fuse y retornar error
    end
  end

  # Simula una API que falla el 60% del tiempo
  defp simulate_http_call(url) do
    :timer.sleep(:rand.uniform(100))  # Latencia simulada
    if :rand.uniform(10) > 4 do
      {:error, :connection_refused}
    else
      {:ok, %{url: url, status: 200, body: "OK"}}
    end
  end

  def status do
    # TODO: Consultar estado del fuse con :fuse.circuit_state/1
    # Retornar :ok (closed), :blown (open/half_open) o {:error, reason}
    :fuse.circuit_state(@fuse_name)
  end
end

defmodule FuseDemo do
  def run do
    ExternalAPIClient.install()

    IO.puts("=== Fase 1: Llamadas normales (algunas fallará) ===")
    results = Enum.map(1..10, fn i ->
      result = ExternalAPIClient.get("https://api.example.com/item/#{i}")
      IO.puts("Llamada #{i}: #{inspect(result)}")
      result
    end)

    IO.puts("\nEstado del circuito: #{inspect(ExternalAPIClient.status())}")

    IO.puts("\n=== Fase 2: Forzar apertura del circuito ===")
    # Hacer muchas llamadas para trigger el circuit breaker
    Enum.each(1..20, fn i ->
      ExternalAPIClient.get("https://api.example.com/item/#{i}")
    end)

    IO.puts("\nEstado tras 20 llamadas: #{inspect(ExternalAPIClient.status())}")

    IO.puts("\n=== Fase 3: Llamadas con circuito abierto ===")
    Enum.each(1..3, fn i ->
      result = ExternalAPIClient.get("https://api.example.com/item/#{i}")
      IO.puts("Llamada con circuito abierto #{i}: #{inspect(result)}")
    end)

    results
  end
end

# Test it:
# FuseDemo.run()
```

**Hints**:
- `:fuse.melt/1` registra un fallo; llámalo SOLO cuando detectas un error real, no en éxito
- `:fuse.circuit_state/1` retorna `:ok` (closed), `:blown` (open) o `:tripped` (half_open)
- Para tests, `:fuse.reset/1` resetea el circuito manualmente

**One possible solution** (sparse):
```elixir
# En perform_request para el caso de error:
{:error, reason} ->
  :fuse.melt(@fuse_name)
  IO.puts("[CircuitBreaker] Fallo registrado: #{reason}")
  {:error, reason}

# Status helper:
def status do
  :fuse.circuit_state(@fuse_name)
end
```

---

### Exercise 2: Custom Circuit Breaker con GenStateMachine

Implementa un circuit breaker completo sin librerías externas usando `GenStateMachine`.

```elixir
defmodule CircuitBreaker do
  @moduledoc """
  Circuit breaker implementado con GenStateMachine.

  Estados:
  - :closed — operación normal, cuenta fallos
  - :open — bloquea llamadas, espera recovery_timeout
  - :half_open — permite UNA llamada de prueba

  Transiciones:
  - :closed → :open: cuando failures >= threshold
  - :open → :half_open: después de recovery_timeout ms
  - :half_open → :closed: si la llamada de prueba tiene éxito
  - :half_open → :open: si la llamada de prueba falla
  """
  use GenStateMachine, callback_mode: :state_functions

  defstruct failures: 0,
            threshold: 5,
            reset_timeout: 30_000,
            name: nil

  # API pública
  def start_link(opts) do
    name    = Keyword.fetch!(opts, :name)
    config  = %__MODULE__{
      name:          name,
      threshold:     Keyword.get(opts, :threshold, 5),
      reset_timeout: Keyword.get(opts, :reset_timeout, 30_000)
    }
    GenStateMachine.start_link(__MODULE__, config, name: name)
  end

  def call(cb, fun) when is_function(fun, 0) do
    GenStateMachine.call(cb, {:execute, fun})
  end

  def state(cb) do
    GenStateMachine.call(cb, :get_state)
  end

  # Inicialización
  def init(data) do
    {:ok, :closed, data}
  end

  # ===== Estado: CLOSED =====
  def closed({:call, from}, {:execute, fun}, data) do
    # TODO: Ejecutar fun y capturar resultado
    # Si éxito → resetear contador de fallos, responder {:ok, result}
    # Si error → incrementar failures
    #   Si failures >= threshold → transición a :open
    #   Si no → seguir en :closed
    result = try_call(fun)
    case result do
      {:ok, _} ->
        # TODO: reset failures, responder al caller
        {:keep_state, %{data | failures: 0},
         [{:reply, from, result}]}

      {:error, _reason} ->
        new_failures = data.failures + 1
        if new_failures >= data.threshold do
          # TODO: Transición a :open, programar reset timer
          schedule_reset(data.reset_timeout)
          IO.puts("[CB #{inspect(data.name)}] ABRIENDO circuito (#{new_failures} fallos)")
          {:next_state, :open, %{data | failures: 0},
           [{:reply, from, result}]}
        else
          IO.puts("[CB #{inspect(data.name)}] Fallo #{new_failures}/#{data.threshold}")
          {:keep_state, %{data | failures: new_failures},
           [{:reply, from, result}]}
        end
    end
  end

  def closed({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, {:closed, data.failures}}]}
  end

  # ===== Estado: OPEN =====
  def open({:call, from}, {:execute, _fun}, _data) do
    # TODO: Rechazar inmediatamente — no ejecutar fun
    # Responder {:error, :circuit_open}
  end

  def open({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, :open}]}
  end

  def open(:info, :reset_timeout, data) do
    # TODO: Transición a :half_open después del timeout
    IO.puts("[CB #{inspect(data.name)}] Pasando a HALF_OPEN")
    {:next_state, :half_open, data}
  end

  # ===== Estado: HALF_OPEN =====
  def half_open({:call, from}, {:execute, fun}, data) do
    # TODO: Ejecutar la llamada de prueba
    # Si éxito → :closed (circuito recuperado)
    # Si error → :open de nuevo (programar otro reset)
    result = try_call(fun)
    case result do
      {:ok, _} ->
        IO.puts("[CB #{inspect(data.name)}] CERRANDO circuito (prueba exitosa)")
        {:next_state, :closed, %{data | failures: 0},
         [{:reply, from, result}]}

      {:error, _} ->
        IO.puts("[CB #{inspect(data.name)}] Prueba fallida — ABRIENDO de nuevo")
        # TODO: Programar otro reset, transición a :open
    end
  end

  def half_open({:call, from}, :get_state, data) do
    {:keep_state, data, [{:reply, from, :half_open}]}
  end

  # Helpers privados
  defp try_call(fun) do
    try do
      case fun.() do
        {:ok, _} = ok -> ok
        {:error, _} = err -> err
        other -> {:ok, other}
      end
    rescue
      e -> {:error, e}
    catch
      :exit, reason -> {:error, {:exit, reason}}
    end
  end

  defp schedule_reset(timeout) do
    Process.send_after(self(), :reset_timeout, timeout)
  end
end

defmodule CircuitBreakerDemo do
  def run do
    {:ok, cb} = CircuitBreaker.start_link(
      name: :demo_cb,
      threshold: 3,
      reset_timeout: 2_000  # 2 segundos para demo
    )

    IO.puts("=== Estado inicial: #{inspect(CircuitBreaker.state(cb))} ===\n")

    # Fase 1: éxitos
    IO.puts("--- Llamadas exitosas ---")
    Enum.each(1..2, fn i ->
      result = CircuitBreaker.call(cb, fn -> {:ok, "result_#{i}"} end)
      IO.puts("Llamada #{i}: #{inspect(result)}")
    end)

    # Fase 2: fallos hasta abrir
    IO.puts("\n--- Induciendo fallos ---")
    Enum.each(1..5, fn i ->
      result = CircuitBreaker.call(cb, fn -> {:error, :connection_refused} end)
      IO.puts("Fallo #{i}: #{inspect(result)}")
    end)

    IO.puts("\nEstado: #{inspect(CircuitBreaker.state(cb))}")

    # Fase 3: intentos con circuito abierto
    IO.puts("\n--- Circuito abierto ---")
    result = CircuitBreaker.call(cb, fn -> {:ok, "nunca se ejecuta"} end)
    IO.puts("Con circuito abierto: #{inspect(result)}")

    # Fase 4: esperar y probar half_open
    IO.puts("\n--- Esperando recovery_timeout (2s)... ---")
    :timer.sleep(2_500)

    IO.puts("Estado tras espera: #{inspect(CircuitBreaker.state(cb))}")

    IO.puts("\n--- Llamada de prueba (half_open) ---")
    result = CircuitBreaker.call(cb, fn -> {:ok, "servicio recuperado"} end)
    IO.puts("Prueba: #{inspect(result)}")

    IO.puts("Estado final: #{inspect(CircuitBreaker.state(cb))}")
  end
end

# Test it:
# CircuitBreakerDemo.run()
```

**Hints**:
- `callback_mode: :state_functions` en GenStateMachine significa que defines una función por estado (`:closed/3`, `:open/3`, etc.) en lugar del callback genérico `handle_event/4`
- Las acciones `{:reply, from, response}` se pasan en la lista de acciones del return; no uses `GenStateMachine.reply/2` directamente
- `try_call/1` es crítico: captura excepciones Y salidas de proceso para no crashear el circuit breaker

**One possible solution** (sparse):
```elixir
# Estado :open rechazar:
def open({:call, from}, {:execute, _fun}, data) do
  {:keep_state, data, [{:reply, from, {:error, :circuit_open}}]}
end

# Estado :half_open fallo:
{:error, _} ->
  schedule_reset(data.reset_timeout)
  {:next_state, :open, data,
   [{:reply, from, result}]}
```

---

### Exercise 3: Bulkhead Pattern — Semáforo de concurrencia

Implementa un bulkhead que limita cuántas llamadas concurrentes pueden hacerse a un servicio externo, protegiendo contra sobrecarga.

```elixir
defmodule Bulkhead do
  @moduledoc """
  Semáforo de concurrencia para limitar llamadas simultáneas a servicios externos.

  Implementado con un GenServer que mantiene un contador de slots disponibles.
  Si no hay slots disponibles, la llamada falla rápido (fail fast) en lugar
  de hacer queuing que puede producir latencia acumulada.
  """
  use GenServer

  defstruct [:name, :max_concurrent, :current, :waiters]

  def start_link(opts) do
    name = Keyword.fetch!(opts, :name)
    max  = Keyword.get(opts, :max_concurrent, 10)
    GenServer.start_link(__MODULE__, {name, max}, name: name)
  end

  # Adquirir un slot (fail fast si no hay disponibles)
  def acquire(name) do
    GenServer.call(name, :acquire)
  end

  # Liberar un slot después de terminar la llamada
  def release(name) do
    GenServer.cast(name, :release)
  end

  # Ejecutar una función dentro del bulkhead
  def run(name, fun) when is_function(fun, 0) do
    case acquire(name) do
      :ok ->
        try do
          fun.()
        after
          release(name)
        end

      {:error, :at_capacity} ->
        {:error, :at_capacity}
    end
  end

  def stats(name) do
    GenServer.call(name, :stats)
  end

  # Callbacks
  def init({name, max_concurrent}) do
    state = %__MODULE__{
      name: name,
      max_concurrent: max_concurrent,
      current: 0,
      waiters: []
    }
    {:ok, state}
  end

  def handle_call(:acquire, _from, %{current: current, max_concurrent: max} = state)
      when current < max do
    # TODO: Incrementar current, responder :ok
    {:reply, :ok, %{state | current: current + 1}}
  end

  def handle_call(:acquire, _from, state) do
    # TODO: Slots agotados — responder {:error, :at_capacity}
    # No encolar el caller; fail fast
  end

  def handle_call(:stats, _from, state) do
    stats = %{
      max_concurrent: state.max_concurrent,
      current_in_use: state.current,
      available: state.max_concurrent - state.current
    }
    {:reply, stats, state}
  end

  def handle_cast(:release, %{current: current} = state) do
    # TODO: Decrementar current (mínimo 0)
    {:noreply, %{state | current: max(0, current - 1)}}
  end
end

defmodule BulkheadDemo do
  def run do
    {:ok, _bh} = Bulkhead.start_link(name: :payment_service, max_concurrent: 3)

    IO.puts("=== Bulkhead: max 3 concurrent ===\n")
    IO.puts("Stats iniciales: #{inspect(Bulkhead.stats(:payment_service))}")

    # Lanzar 10 Tasks concurrentes que intentan usar el bulkhead
    tasks = Enum.map(1..10, fn i ->
      Task.async(fn ->
        result = Bulkhead.run(:payment_service, fn ->
          IO.puts("  → Ejecutando llamada #{i}")
          :timer.sleep(500)  # Simula llamada HTTP
          {:ok, "result_#{i}"}
        end)

        case result do
          {:ok, _}              -> IO.puts("  ✓ Llamada #{i} completada")
          {:error, :at_capacity} -> IO.puts("  ✗ Llamada #{i} rechazada (bulkhead lleno)")
        end

        {i, result}
      end)
    end)

    results = Task.await_many(tasks, 5_000)

    successes = Enum.count(results, fn {_, r} -> match?({:ok, _}, r) end)
    rejected  = Enum.count(results, fn {_, r} -> r == {:error, :at_capacity} end)

    IO.puts("\nResultados:")
    IO.puts("  Exitosas:  #{successes}")
    IO.puts("  Rechazadas: #{rejected}")
    IO.puts("  Stats finales: #{inspect(Bulkhead.stats(:payment_service))}")
  end
end

# Test it:
# BulkheadDemo.run()
# Esperado: exactamente 3 llamadas en paralelo, el resto rechazadas (at_capacity)
```

**Hints**:
- El pattern matching en `handle_call(:acquire, ...)` con `when current < max` vs el segundo clause (sin guard) implementa el fail fast cleanly
- En `handle_cast(:release, ...)`, `max(0, current - 1)` previene que el contador se vuelva negativo por condiciones de carrera (aunque con GenServer serializado no debería pasar)
- En producción usa `Semaphore` (hex package) o `Poolboy` para pool de workers; este ejercicio muestra el mecanismo subyacente

**One possible solution** (sparse):
```elixir
# handle_call :acquire cuando at_capacity:
def handle_call(:acquire, _from, state) do
  {:reply, {:error, :at_capacity}, state}
end

# handle_cast :release:
def handle_cast(:release, %{current: current} = state) do
  {:noreply, %{state | current: max(0, current - 1)}}
end
```

## Common Mistakes

### Mistake 1: Registrar fallos ANTES de ejecutar la llamada
```elixir
# ❌ El fuse se abre antes de que haya habido un error real
:fuse.melt(:my_service)
result = call_external()

# ✓ Solo melting si la llamada falla
case call_external() do
  {:ok, _} = r -> r
  {:error, _} = e ->
    :fuse.melt(:my_service)
    e
end
```

### Mistake 2: Circuit breaker sin cleanup del timer al cerrar
```elixir
# ❌ Si el circuito se cierra vía half_open antes de que el timer expire,
# el timer enviará :reset_timeout al proceso que ya está en :closed
# → transición inválida, potencialmente silenciosa

# ✓ Manejar el mensaje :reset_timeout en todos los estados
def closed(:info, :reset_timeout, data) do
  # Ignorar — ya estamos cerrados
  {:keep_state, data}
end
```

### Mistake 3: Bulkhead con encolamiento sin timeout
```elixir
# ❌ Encolar callers cuando el bulkhead está lleno puede acumular latencia
# Si 100 callers esperan y el servicio sigue lento, el tiempo de espera
# acumulado puede ser peor que fallar rápido
waiters: :queue.in(from, state.waiters)  # Potencialmente problemático

# ✓ Fail fast es preferible para bulkhead — los callers deciden si reintentar
{:reply, {:error, :at_capacity}, state}
```

### Mistake 4: Threshold demasiado bajo en producción
```elixir
# ❌ Threshold de 1 o 2 fallos causa apertura frecuente por fallos transitorios
{:standard, 1, 5_000}  # Abre en el primer error — demasiado sensible

# ✓ Calibrar threshold con datos reales de tasa de error base del servicio
# Regla general: threshold > tasa_de_error_base * ventana_de_tiempo
{:standard, 10, 30_000}  # 10 fallos en 30 segundos → más robusto
```

## Verification
```bash
iex> c("36-circuit-breaker-patterns.exs")

# Exercise 1
iex> FuseDemo.run()
# Ver circuito abriéndose, respuestas :circuit_open, luego half_open

# Exercise 2
iex> CircuitBreakerDemo.run()
# Ver transiciones closed → open → half_open → closed

# Exercise 3
iex> BulkheadDemo.run()
# Exactamente 3 llamadas concurrentes, resto rechazadas
```

Checklist de verificación:
- [ ] Fuse: `melt` solo en fallos, no en éxitos
- [ ] Custom CB: las tres transiciones (closed→open, open→half_open, half_open→closed/open) funcionan
- [ ] Custom CB: el estado `:half_open` permite exactamente UNA llamada de prueba
- [ ] Bulkhead: el contador nunca supera `max_concurrent`
- [ ] Bulkhead: `release` siempre se llama (garantizado por `after` en `run/2`)

## Summary
- Circuit breakers protegen tu sistema de cascading failures: fallan rápido en lugar de esperar timeouts
- Los tres estados (:closed, :open, :half_open) y sus transiciones son el núcleo del patrón
- Fuse es la librería de referencia para Elixir/Erlang; GenStateMachine para implementaciones custom
- El bulkhead complementa al circuit breaker: limita concurrencia hacia servicios lentos
- En producción, combina ambos: circuit breaker para errores, bulkhead para latencia
- `threshold` y `reset_timeout` requieren calibración con datos reales de tasa de error del servicio

## What's Next
**37-rate-limiting-patterns**: Rate limiting controla CUÁNTAS requests haces (throughput), mientras bulkhead controla cuántas van en paralelo (concurrencia). Aprende las diferencias y algoritmos de alta performance.

## Resources
- [Fuse — GitHub](https://github.com/jlouis/fuse)
- [GenStateMachine — HexDocs](https://hexdocs.pm/gen_state_machine/GenStateMachine.html)
- [Circuit Breaker Pattern — Martin Fowler](https://martinfowler.com/bliki/CircuitBreaker.html)
- [Release It! — Michael Nygard](https://pragprog.com/titles/mnee2/release-it-second-edition/)
