# 08 — Task.Supervisor: Dynamic Tasks

**Difficulty**: Avanzado  
**Estimated time**: 90–120 min  
**Topics**: Task.Supervisor, Concurrency, Error Isolation, Backpressure

---

## Context

`Task` es la abstracción de Elixir para computación concurrente de una sola vez: inicias una tarea, esperas su resultado, listo. Pero en producción, las tareas tienen más complejidad: pueden fallar, pueden colgar, pueden acumularse más rápido de lo que el sistema puede procesarlas.

`Task.Supervisor` añade la capa de supervisión que `Task` por sí solo no tiene: sus tareas están bajo un supervisor, se puede controlar la concurrencia máxima, y el crash de una tarea no mata al proceso que la lanzó.

La distinción entre `async`, `async_nolink`, y `async_stream` no es trivia de API — refleja tres modelos diferentes de relación entre el llamador y la tarea en cuanto a fallos.

---

## Concepts

### Task.Supervisor vs Task

```elixir
# Task nativo — el crash de la tarea mata al llamador
task = Task.async(fn -> do_something_risky() end)
result = Task.await(task, 5_000)

# Si do_something_risky() lanza una excepción:
# → la tarea muere
# → el llamador recibe el exit signal
# → el llamador también muere (si no tiene :trap_exit)
```

```elixir
# Task.Supervisor.async — la tarea está supervisada
# Pero el link sigue existiendo: crash de tarea aún mata al llamador
task = Task.Supervisor.async(MyApp.TaskSupervisor, fn -> do_something_risky() end)
result = Task.await(task, 5_000)
```

```elixir
# Task.Supervisor.async_nolink — sin link al llamador
# El crash de la tarea NO mata al llamador
# El supervisor reinicia la tarea (si es :permanent) o la ignora (si es :temporary)
task = Task.Supervisor.async_nolink(MyApp.TaskSupervisor, fn -> do_something_risky() end)

# Para obtener el resultado:
receive do
  {^task.ref, result} -> result
  {:DOWN, ^task.ref, :process, _pid, reason} -> {:error, reason}
after
  5_000 -> {:error, :timeout}
end
```

**La diferencia crítica**: `async` crea un link bidireccional. `async_nolink` no. Elige según si el crash de la tarea debe propagarse al llamador.

---

### async vs async_nolink: cuándo usar cada uno

| Escenario | Usar |
|-----------|------|
| Tarea que DEBE completarse para que el llamador continúe | `async` |
| El llamador depende del resultado de la tarea | `async` |
| Fire-and-forget (el llamador no necesita el resultado) | `async_nolink` |
| La tarea puede fallar sin afectar al llamador | `async_nolink` |
| Enviar emails, webhooks, notificaciones async | `async_nolink` |
| Cachear un resultado en background | `async_nolink` |

---

### Task.Supervisor.async_stream

```elixir
# Procesamiento paralelo de colecciones con backpressure
results =
  Task.Supervisor.async_stream(
    MyApp.TaskSupervisor,
    items,                          # enumerable de entrada
    fn item -> process(item) end,   # función a aplicar
    max_concurrency: 10,            # máximo 10 tareas concurrentes
    timeout: 30_000,                # timeout por tarea
    on_timeout: :kill_task          # matar la tarea si hace timeout
  )
  |> Enum.to_list()
```

**Resultado**: `[{:ok, result}, {:ok, result}, {:exit, reason}, ...]`

`async_stream` implementa backpressure real: solo lanza la siguiente tarea cuando hay un slot libre. Si `max_concurrency: 10` y hay 1000 items, en ningún momento habrá más de 10 tareas corriendo simultáneamente.

**`on_timeout`**:
- `:kill_task` — mata la tarea, el resultado es `{:exit, :timeout}`
- `:ignore` — el resultado es `{:exit, :timeout}` pero la tarea sigue corriendo en background (resource leak potencial)

---

### Configuración del Supervisor

```elixir
# En Application.start/2
children = [
  {Task.Supervisor, name: MyApp.TaskSupervisor}
]

# O con opciones:
{Task.Supervisor,
  name: MyApp.TaskSupervisor,
  max_children: 100,    # límite de tareas concurrentes (opcional)
  restart: :temporary   # las tareas no se reinician (default correcto para tareas)
}
```

**`max_children`**: previene que el sistema se sature con demasiadas tareas concurrentes. Si el límite está alcanzado, `start_child` retorna `{:error, :max_children}`. En `async_stream`, esto es manejado automáticamente por `max_concurrency`.

---

### Error Handling con Task

```elixir
# Task.await lanza excepción si la tarea falla
try do
  result = Task.Supervisor.async(supervisor, fn ->
    raise "oops"
  end) |> Task.await()
rescue
  e in RuntimeError -> handle_error(e)
end

# Task.yield es más suave — retorna nil si timeout, {:ok, result} si ok
case Task.yield(task, 5_000) do
  {:ok, result} -> handle_result(result)
  nil ->
    Task.shutdown(task)  # cancelar la tarea
    handle_timeout()
end
```

---

## Exercise 1 — Supervised HTTP Requests

### Problem

Implementa un fetcher que descarga contenido de una lista de URLs en paralelo, con control de concurrencia máxima, timeout por request, y manejo correcto de errores parciales.

El sistema debe:
- Procesar hasta `max_concurrency` URLs simultáneamente
- Aplicar un timeout de 10s por request
- Si una URL falla (error HTTP, timeout, red), el resultado es `{:error, url, reason}` — no interrumpe las demás
- Retornar `%{ok: [...], errors: [...]}` al final

```elixir
defmodule MyApp.UrlFetcher do
  @max_concurrency 5
  @timeout 10_000

  def fetch_all(urls) when is_list(urls) do
    Task.Supervisor.async_stream(
      MyApp.TaskSupervisor,
      urls,
      &fetch_one/1,
      max_concurrency: @max_concurrency,
      timeout: @timeout,
      on_timeout: :kill_task
    )
    |> Enum.zip(urls)
    |> Enum.reduce(%{ok: [], errors: []}, fn {result, url}, acc ->
      case result do
        {:ok, body} ->
          # Tu implementación aquí
        {:exit, reason} ->
          # Tu implementación aquí
      end
    end)
  end

  defp fetch_one(url) do
    # Usa :httpc (stdlib) o simula con Process.sleep + resultado mock
    case :httpc.request(:get, {String.to_charlist(url), []}, [{:timeout, @timeout}], []) do
      {:ok, {{_version, 200, _reason}, _headers, body}} ->
        {:ok, IO.chardata_to_string(body)}
      {:ok, {{_version, status, _reason}, _headers, _body}} ->
        {:error, {:http_error, status}}
      {:error, reason} ->
        {:error, reason}
    end
  end
end
```

**Tu tarea**:
1. Completa `fetch_all/1` con el reduce correcto
2. Añade logging para cada request (éxito y fallo)
3. ¿Qué pasa si la lista de URLs es vacía? Asegúrate de que retorne `%{ok: [], errors: []}` limpiamente
4. Implementa una variante `fetch_all!/1` que lanza excepción si hay cualquier error

### Hints

<details>
<summary>Hint 1 — Zip de resultados con URLs</summary>

`async_stream` devuelve resultados en el mismo orden que el enumerable de entrada. Puedes hacer `Enum.zip(results, urls)` para correlacionar cada resultado con su URL original. Pero cuidado: el zip se hace sobre el stream perezoso, evalúalo con `Enum.to_list()` primero o trabaja con índices.

</details>

<details>
<summary>Hint 2 — on_timeout: :kill_task vs :ignore</summary>

Con `:kill_task`, la tarea que hace timeout es terminada. El resultado es `{:exit, :timeout}`. Esto es correcto para HTTP requests: no quieres connections colgadas en background consumiendo file descriptors.

Con `:ignore`, la tarea sigue corriendo después del timeout. El resultado del stream es `{:exit, :timeout}`, pero la tarea sigue viva — esto puede causar leaks de conexiones si usas pools de HTTP.

</details>

<details>
<summary>Hint 3 — Task.Supervisor en Application</summary>

No olvides añadir `{Task.Supervisor, name: MyApp.TaskSupervisor}` a los children de tu Application. Sin esto, `async_stream` falla con un error críptico de proceso no encontrado.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
defmodule MyApp.UrlFetcher do
  require Logger

  @max_concurrency 5
  @timeout 10_000

  def fetch_all([]), do: %{ok: [], errors: []}

  def fetch_all(urls) when is_list(urls) do
    results =
      Task.Supervisor.async_stream(
        MyApp.TaskSupervisor,
        urls,
        &fetch_one/1,
        max_concurrency: @max_concurrency,
        timeout: @timeout,
        on_timeout: :kill_task
      )
      |> Enum.to_list()

    urls
    |> Enum.zip(results)
    |> Enum.reduce(%{ok: [], errors: []}, fn {url, result}, acc ->
      case result do
        {:ok, {:ok, body}} ->
          Logger.info("Fetched #{url}: #{byte_size(body)} bytes")
          %{acc | ok: [{url, body} | acc.ok]}

        {:ok, {:error, reason}} ->
          Logger.warning("HTTP error for #{url}: #{inspect(reason)}")
          %{acc | errors: [{url, reason} | acc.errors]}

        {:exit, :timeout} ->
          Logger.warning("Timeout fetching #{url}")
          %{acc | errors: [{url, :timeout} | acc.errors]}

        {:exit, reason} ->
          Logger.error("Unexpected exit for #{url}: #{inspect(reason)}")
          %{acc | errors: [{url, reason} | acc.errors]}
      end
    end)
    |> then(fn result ->
      %{result | ok: Enum.reverse(result.ok), errors: Enum.reverse(result.errors)}
    end)
  end

  def fetch_all!(urls) do
    result = fetch_all(urls)

    unless result.errors == [] do
      raise "Fetch errors: #{inspect(result.errors)}"
    end

    result.ok
  end

  defp fetch_one(url) do
    case :httpc.request(:get, {String.to_charlist(url), []}, [{:timeout, @timeout}], []) do
      {:ok, {{_, 200, _}, _, body}} -> {:ok, IO.chardata_to_string(body)}
      {:ok, {{_, status, _}, _, _}} -> {:error, {:http_status, status}}
      {:error, reason} -> {:error, reason}
    end
  end
end
```

</details>

---

## Exercise 2 — Fire-and-Forget con async_nolink

### Problem

Implementa un sistema de notificaciones asíncrono donde enviar una notificación nunca bloquea al llamador y nunca lo mata si falla.

Escenarios de uso:
- Enviar emails de confirmación de orden
- Disparar webhooks a sistemas externos
- Actualizar una caché en background

```elixir
defmodule MyApp.Notifier do
  require Logger

  # Fire-and-forget: el llamador no espera ni le importa el resultado
  def notify_async(event, payload) do
    task = Task.Supervisor.async_nolink(
      MyApp.TaskSupervisor,
      fn -> do_notify(event, payload) end
    )

    # ¿Debes hacer algo con `task`? ¿O simplemente ignorarla?
    # Spoiler: ignorarla tiene consecuencias. Investígalas.
    :ok
  end

  # Variante: fire-and-forget CON callback de resultado
  def notify_async_with_callback(event, payload, callback) when is_function(callback, 1) do
    # El llamador provee una función que se llama con el resultado
    # Implementa esto sin bloquear al llamador
  end

  defp do_notify(:email, %{to: to, subject: subject} = payload) do
    # Simula envío de email (puede fallar)
    if :rand.uniform(10) > 8 do
      raise "SMTP server unavailable"
    end

    Logger.info("Email sent to #{to}: #{subject}")
    {:ok, :email_sent}
  end

  defp do_notify(:webhook, %{url: url} = payload) do
    # Simula webhook (puede hacer timeout)
    Process.sleep(:rand.uniform(2_000))
    Logger.info("Webhook delivered to #{url}")
    {:ok, :webhook_delivered}
  end
end
```

**Tu tarea**:
1. Explica qué pasa si ignoramos el `task` retornado por `async_nolink` completamente
2. Implementa `notify_async/2` de forma que los mensajes de resultado sean descartados limpiamente sin generar warnings
3. Implementa `notify_async_with_callback/3` donde el llamador recibe el resultado vía callback en su propio proceso
4. ¿Qué pasa si el proceso llamador muere antes de que la tarea termine? ¿Hay resource leak?

### Hints

<details>
<summary>Hint 1 — Task.Supervisor.start_child para verdadero fire-and-forget</summary>

`async_nolink` retorna una `Task` struct. Si no la guardas, los mensajes del resultado flotarán en la mailbox del proceso llamador indefinidamente (memory leak leve).

Para verdadero fire-and-forget sin contaminar la mailbox, usa:
```elixir
Task.Supervisor.start_child(MyApp.TaskSupervisor, fn ->
  do_notify(event, payload)
end)
```

`start_child` devuelve `{:ok, pid}` y no crea ningún mecanismo de respuesta. El proceso muere cuando termina y nadie recibe ningún mensaje.

</details>

<details>
<summary>Hint 2 — callback en el proceso correcto</summary>

Para `notify_async_with_callback`, el callback debe ejecutarse en el contexto correcto. Si el callback modifica state del GenServer llamador, debe ejecutarse vía `send` o `GenServer.cast`, no directamente desde la tarea (estaría ejecutando en el proceso de la tarea, sin acceso al state del GenServer).

```elixir
caller_pid = self()
Task.Supervisor.async_nolink(supervisor, fn ->
  result = do_notify(event, payload)
  send(caller_pid, {:notification_result, result})
end)
# El llamador debe tener handle_info para {:notification_result, result}
```

</details>

<details>
<summary>Hint 3 — Resource leak cuando el caller muere</summary>

Si el llamador muere antes de que la tarea complete, la tarea sigue corriendo (no hay link). Cuando la tarea intenta enviar el resultado al caller muerto, el mensaje se pierde. No hay leak de proceso — la tarea termina normalmente. El "leak" sería en recursos externos que la tarea está usando (conexiones HTTP, etc.) si el timeout del supervisor no los limpia.

</details>

---

## Exercise 3 — Error Handling con Task.Supervisor.async

### Problem

`Task.Supervisor.async/3` (con link) lanza una excepción si la tarea falla cuando llamas `Task.await/2`. Pero en sistemas reales, necesitas manejar estos errores de forma granular según el tipo de fallo.

```elixir
defmodule MyApp.DataProcessor do
  @doc """
  Procesa una lista de items en paralelo.
  Retorna {:ok, results} si TODOS los items se procesaron correctamente.
  Retorna {:partial, %{ok: results, errors: errors}} si algunos fallaron.
  Retorna {:error, reason} si el fallo fue catastrófico.
  """
  def process_batch(items) do
    tasks =
      Enum.map(items, fn item ->
        Task.Supervisor.async(MyApp.TaskSupervisor, fn ->
          process_item(item)
        end)
      end)

    # ¿Cómo recoges resultados cuando ALGUNOS pueden fallar?
    # Task.await_many/2 lanza excepción al primer fallo — no sirve aquí
    # Necesitas una estrategia diferente
  end

  defp process_item(item) when item < 0, do: raise "Negative item: #{item}"
  defp process_item(item), do: item * 2
end
```

**Tu tarea**:
1. Implementa `process_batch/1` que tolere fallos parciales usando `Task.yield_many/2`
2. Distingue entre `:timeout`, `:exit` (crash), y `:ok` en los resultados
3. Para tasks que hicieron timeout, cancélalas explícitamente con `Task.shutdown/2`
4. Implementa una variante `process_batch!/1` que falla rápido al primer error

### Hints

<details>
<summary>Hint 1 — Task.yield_many/2</summary>

```elixir
# Task.yield_many espera hasta el timeout y retorna lo que tenga
results = Task.yield_many(tasks, 5_000)
# results :: [{Task.t(), {:ok, result} | {:exit, reason} | nil}]
# nil significa que la tarea no terminó en el timeout
```

Luego puedes cancelar las que devolvieron `nil`:
```elixir
Enum.each(results, fn
  {task, nil} -> Task.shutdown(task, :brutal_kill)
  _ -> :ok
end)
```

</details>

<details>
<summary>Hint 2 — Task.async con link y rescue</summary>

Si usas `async` (con link), un crash de la tarea propagará un exit signal al llamador. Para manejarlo sin morir, el llamador necesita `Process.flag(:trap_exit, true)` — pero esto cambia la semántica de todos los exits en ese proceso, lo cual es peligroso en GenServers.

La alternativa limpia: usar `async_nolink` + receive con pattern matching para el `{:DOWN, ref, ...}` message.

</details>

<details>
<summary>Hint 3 — Task.shutdown semantics</summary>

```elixir
Task.shutdown(task)             # envía :shutdown, espera 5s
Task.shutdown(task, :brutal_kill)  # SIGKILL inmediato
Task.shutdown(task, timeout)   # espera N ms antes de brutal kill
```

Siempre llama `shutdown` en tareas que no vas a `await`. Si no lo haces, la tarea puede seguir corriendo como proceso huérfano.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
defmodule MyApp.DataProcessor do
  require Logger

  @batch_timeout 10_000

  def process_batch(items) do
    tasks =
      Enum.map(items, fn item ->
        task = Task.Supervisor.async_nolink(MyApp.TaskSupervisor, fn ->
          process_item(item)
        end)
        {task, item}
      end)

    raw_results = Task.yield_many(Enum.map(tasks, &elem(&1, 0)), @batch_timeout)

    # Cancelar tareas que hicieron timeout
    Enum.each(raw_results, fn
      {task, nil} ->
        Logger.warning("Task timeout, shutting down")
        Task.shutdown(task, :brutal_kill)
      _ -> :ok
    end)

    {ok, errors} =
      Enum.zip(tasks, raw_results)
      |> Enum.reduce({[], []}, fn {{_task, item}, {_task, result}}, {ok_acc, err_acc} ->
        case result do
          {:ok, value} ->
            {[value | ok_acc], err_acc}
          {:exit, reason} ->
            Logger.error("Item #{inspect(item)} failed: #{inspect(reason)}")
            {ok_acc, [{item, reason} | err_acc]}
          nil ->
            {ok_acc, [{item, :timeout} | err_acc]}
        end
      end)

    cond do
      errors == [] ->
        {:ok, Enum.reverse(ok)}
      ok == [] ->
        {:error, :all_failed}
      true ->
        {:partial, %{ok: Enum.reverse(ok), errors: Enum.reverse(errors)}}
    end
  end

  def process_batch!(items) do
    case process_batch(items) do
      {:ok, results} -> results
      {:partial, %{errors: errors}} -> raise "Partial failure: #{inspect(errors)}"
      {:error, reason} -> raise "Batch failed: #{inspect(reason)}"
    end
  end

  defp process_item(item) when item < 0, do: raise "Negative item: #{item}"
  defp process_item(item), do: item * 2
end
```

</details>

---

## Common Mistakes

### Mistake 1 — No cancelar tareas huérfanas

```elixir
# MAL: lanzar tareas y no hacer cleanup si el llamador abandona
tasks = Enum.map(items, &Task.Supervisor.async(supervisor, fn -> process(&1) end))

# El llamador hace otra cosa y nunca hace await
# Las tareas siguen corriendo en background, nadie recogerá sus resultados
# Los mensajes de resultado se acumularán en la mailbox del llamador
```

**Regla**: si lanzas una tarea con `async`, siempre debes hacer `await`, `yield`, o `shutdown`. Sin excepción.

---

### Mistake 2 — Usar async_stream sin limit de max_concurrency

```elixir
# MAL: procesar 100,000 URLs sin límite
Task.Supervisor.async_stream(supervisor, urls, &fetch/1)
# → lanza 100,000 tareas simultáneamente
# → saturación de file descriptors
# → crash del nodo por OOM
```

Siempre especifica `max_concurrency` basándote en el recurso limitante (connections HTTP, file descriptors, CPU).

---

### Mistake 3 — Confundir Task.Supervisor con supervisión de procesos long-running

```elixir
# MAL: usar Task.Supervisor para procesos que deben vivir indefinidamente
Task.Supervisor.start_child(supervisor, fn ->
  GenServer.start_link(MyLongRunningService, [])  # ← esto es un GenServer, no una task
end)

# Task está diseñado para computación acotada en tiempo.
# Para procesos long-running, usa Supervisor directamente.
```

---

### Mistake 4 — Ignorar el resultado de async_nolink

```elixir
# PROBLEMA SUTIL: async_nolink envía mensajes al llamador aunque no esperes
task = Task.Supervisor.async_nolink(supervisor, fn -> compute() end)
# No guardas `task`, no haces await, no haces shutdown

# Cuando compute() termina, el runtime envía a tu proceso:
#   {task.ref, result}   ← mensaje de resultado
#   {:DOWN, task.ref, :process, pid, reason}  ← DOWN notification

# Estos mensajes se quedan en la mailbox. En un GenServer,
# si no tienes handle_info para ellos, acumulas mensajes perdidos.
```

Si usas `async_nolink` y no te interesa el resultado, usa `Task.Supervisor.start_child/2` en su lugar.

---

## Production Patterns

### Pattern 1 — Pool de supervisores para distintos tipos de trabajo

```elixir
children = [
  {Task.Supervisor, name: MyApp.IOTaskSupervisor, max_children: 200},
  {Task.Supervisor, name: MyApp.CPUTaskSupervisor, max_children: System.schedulers_online()},
  {Task.Supervisor, name: MyApp.EmailTaskSupervisor, max_children: 10},
]

# IO-bound: puedes tener muchos concurrentes (la mayoría esperan respuesta de red)
# CPU-bound: limitado a schedulers para evitar context-switching excesivo
# Email: limitado para respetar rate limits del SMTP server
```

### Pattern 2 — async_stream con telemetría

```elixir
def process_with_telemetry(items, supervisor) do
  start_time = System.monotonic_time()

  results =
    Task.Supervisor.async_stream(supervisor, items, fn item ->
      :telemetry.span([:myapp, :item_processing], %{item: item}, fn ->
        result = process_item(item)
        {result, %{}}
      end)
    end, max_concurrency: 10, timeout: 30_000)
    |> Enum.to_list()

  duration = System.monotonic_time() - start_time

  :telemetry.execute([:myapp, :batch_processing, :complete], %{
    duration: duration,
    total: length(items),
    errors: Enum.count(results, fn {status, _} -> status == :exit end)
  })

  results
end
```

### Pattern 3 — Graceful degradation con Task.yield

```elixir
# Si la tarea no responde en X ms, usa un valor por defecto
def get_with_timeout(key, default_value) do
  task = Task.Supervisor.async(supervisor, fn -> fetch_from_slow_service(key) end)

  case Task.yield(task, 500) do
    {:ok, value} ->
      value

    nil ->
      # La tarea sigue corriendo, pero devolvemos el default
      Task.shutdown(task)  # cancelar para no desperdiciar recursos
      Logger.warning("Slow service timeout for #{key}, using default")
      default_value
  end
end
```

---

## Resources

- [Task.Supervisor — HexDocs](https://hexdocs.pm/elixir/Task.Supervisor.html)
- [Task — HexDocs](https://hexdocs.pm/elixir/Task.html)
- [Task.yield_many/2](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [Concurrent Data Processing in Elixir — Pragmatic Programmers](https://pragprog.com/titles/sgdpelixir/)
