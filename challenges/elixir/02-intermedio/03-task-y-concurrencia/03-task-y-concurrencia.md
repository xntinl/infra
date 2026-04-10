# 3. Task y Concurrencia

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 01-procesos-spawn-send-receive
- Completed 02-agent-basico
- Understanding of Enum, anonymous functions, and basic error handling

## Learning Objectives
After completing this exercise, you will be able to:
- Execute concurrent work with `Task.async/1` and collect results with `Task.await/2`
- Launch fire-and-forget tasks with `Task.start/1`
- Process collections concurrently with `Task.async_stream/3`
- Handle task timeouts and failures gracefully
- Decide when to use `Task` versus `spawn` versus `Agent`

## Concepts

### Task: concurrencia de alto nivel
`Task` está diseñado para un caso de uso específico y muy común: lanzar trabajo asíncrono y eventualmente recolectar el resultado. Es la respuesta de Elixir a "quiero hacer X en paralelo y esperar el resultado después". Internamente usa `spawn_link`, pero agrega estructura y manejo de errores.

La diferencia clave con `spawn` desnudo: `Task.async/1` retorna una struct `%Task{}` que después puedes pasar a `Task.await/2` para obtener el resultado. Con `spawn`, tienes que manejar manualmente el protocolo de mensajes. Con `Task`, todo ese boilerplate desaparece.

```elixir
# Sin Task: manual y verbose
padre = self()
spawn(fn ->
  resultado = calcular_algo()
  send(padre, {:resultado, resultado})
end)
receive do
  {:resultado, val} -> val
end

# Con Task: limpio y directo
task = Task.async(fn -> calcular_algo() end)
resultado = Task.await(task)   # Bloquea hasta que el task termine
```

### async + await: el patrón fundamental
`Task.async/1` lanza el trabajo en un proceso separado inmediatamente y retorna una struct `%Task{}`. El proceso principal continúa ejecutándose. `Task.await/2` bloquea el proceso actual hasta que el task complete, luego retorna el valor que retornó la función del task.

El patrón más poderoso es lanzar múltiples tasks y luego hacer await de todos — trabajo verdaderamente paralelo:

```elixir
# Estas tres llamadas ocurren en PARALELO (no secuencialmente)
t1 = Task.async(fn -> fetch_data(:servicio_a) end)
t2 = Task.async(fn -> fetch_data(:servicio_b) end)
t3 = Task.async(fn -> fetch_data(:servicio_c) end)

# Recolectamos resultados — el tiempo total es el del más lento, no la suma
[r1, r2, r3] = Enum.map([t1, t2, t3], &Task.await/1)
```

### Task.async_stream: concurrencia sobre colecciones
Cuando tienes una lista y quieres procesar cada elemento concurrentemente con un límite de concurrencia máxima, `Task.async_stream/3` es la herramienta correcta. Devuelve un stream de resultados en el mismo orden que la entrada.

```elixir
urls = ["url1", "url2", "url3", "url4", "url5"]

# max_concurrency: máximo de tasks simultáneos
resultados =
  Task.async_stream(urls, fn url -> descargar(url) end, max_concurrency: 3)
  |> Enum.to_list()

# Cada elemento es {:ok, valor} o {:exit, reason}
```

### Task.start vs Task.async
- `Task.async/1`: lanza y retorna `%Task{}`. DEBES hacer `await` después o el proceso padre recibirá un mensaje residual. Úsalo cuando necesitas el resultado.
- `Task.start/1`: lanza y retorna `{:ok, pid}`. No hay forma de hacer await. Úsalo para trabajo fire-and-forget (enviar un email, escribir un log).

## Exercises

### Exercise 1: async + await básico
```elixir
defmodule TaskBasico do
  # Simula una operación "lenta" (red, base de datos, etc.)
  defp operacion_lenta(nombre, duracion_ms) do
    :timer.sleep(duracion_ms)
    "Resultado de #{nombre}"
  end

  # TODO: Implementa `secuencial/0` que ejecuta 3 operaciones de forma SECUENCIAL:
  # operacion_lenta("A", 500), operacion_lenta("B", 300), operacion_lenta("C", 400)
  # Mide el tiempo con :timer.tc/1 (retorna {microsegundos, resultado})
  # Imprime cuánto tiempo tomó en total y los resultados
  def secuencial do
    {tiempo_us, resultados} = :timer.tc(fn ->
      # TODO: llamar las 3 operaciones secuencialmente y retornar lista de resultados
    end)
    IO.puts("Secuencial: #{tiempo_us / 1000}ms")
    IO.inspect(resultados)
  end

  # TODO: Implementa `paralelo/0` que ejecuta las MISMAS 3 operaciones en PARALELO:
  # 1. Lanza 3 Task.async, uno por operación
  # 2. Mide el tiempo total con :timer.tc/1
  # 3. Hace await de todos los tasks
  # 4. Imprime el tiempo (debería ser ~500ms en vez de ~1200ms)
  def paralelo do
    {tiempo_us, resultados} = :timer.tc(fn ->
      t1 = Task.async(fn -> operacion_lenta("A", 500) end)
      t2 = Task.async(fn ->
        # TODO: operacion_lenta("B", 300)
      end)
      t3 = Task.async(fn ->
        # TODO: operacion_lenta("C", 400)
      end)
      # TODO: hacer await de los 3 tasks y retornar lista de resultados
    end)
    IO.puts("Paralelo: #{tiempo_us / 1000}ms")
    IO.inspect(resultados)
  end
end

# Test it:
# TaskBasico.secuencial()    # ~1200ms
# TaskBasico.paralelo()      # ~500ms (el más lento de los 3)
```

### Exercise 2: Múltiples tasks sobre una colección
```elixir
defmodule FetchSimulado do
  # Simula descargar datos de una URL (en realidad solo espera y retorna)
  defp descargar(url) do
    # Simula latencia variable
    :timer.sleep(Enum.random(100..500))
    %{url: url, status: 200, body: "Contenido de #{url}"}
  end

  # TODO: Implementa `fetch_todas/1` que recibe una lista de URLs:
  # 1. Para cada URL, lanza un Task.async que llama descargar/1
  # 2. Recolecta todos los resultados con Task.await
  # 3. Retorna la lista de resultados
  # PISTA: Enum.map(urls, &Task.async(fn -> descargar(&1) end)) lanza todos en paralelo
  def fetch_todas(urls) do
    urls
    |> Enum.map(fn url ->
      # TODO: lanzar Task.async para esta URL
    end)
    |> Enum.map(fn task ->
      # TODO: hacer Task.await del task
    end)
  end

  # TODO: Implementa `fetch_todas_con_timeout/2` igual que arriba pero:
  # Task.await/2 acepta un timeout en ms como segundo argumento
  # Si algún task supera el timeout, Task.await lanza una excepción
  # Usa try/catch para manejar el caso de timeout y retornar {:error, :timeout}
  def fetch_todas_con_timeout(urls, timeout_ms) do
    urls
    |> Enum.map(&Task.async(fn -> descargar(&1) end))
    |> Enum.map(fn task ->
      try do
        # TODO: Task.await con timeout_ms
      catch
        :exit, _ -> {:error, :timeout}
      end
    end)
  end
end

# Test it:
# urls = ["https://api.a.com", "https://api.b.com", "https://api.c.com"]
# FetchSimulado.fetch_todas(urls)
# FetchSimulado.fetch_todas_con_timeout(urls, 200)   # algunos fallarán
```

### Exercise 3: Task.async_stream
```elixir
defmodule ProcesadorParalelo do
  # Simula procesamiento pesado de un elemento
  defp procesar(elemento) do
    :timer.sleep(100)
    elemento * 2
  end

  # TODO: Implementa `procesar_lista/1` usando Task.async_stream:
  # - max_concurrency: 4 (máximo 4 tasks simultáneos)
  # - ordered: true (mantener orden del input)
  # - Extrae solo los valores {:ok, valor} del stream resultante
  # - Retorna la lista de valores procesados
  def procesar_lista(lista) do
    lista
    |> Task.async_stream(
      fn elemento ->
        # TODO: llamar procesar/1
      end,
      max_concurrency: 4,
      ordered: true
    )
    |> Enum.map(fn
      {:ok, valor} ->
        # TODO: retornar valor
      {:exit, razon} ->
        # TODO: retornar {:error, razon}
    end)
  end

  # TODO: Implementa `procesar_con_timeout/2` usando async_stream con timeout:
  # Agrega la opción `timeout: timeout_ms` a Task.async_stream
  # Maneja {:exit, :timeout} en el Enum.map
  def procesar_con_timeout(lista, timeout_ms) do
    lista
    |> Task.async_stream(
      &procesar/1,
      max_concurrency: 4,
      ordered: true,
      timeout: timeout_ms
    )
    |> Enum.map(fn
      {:ok, valor} -> {:ok, valor}
      {:exit, :timeout} ->
        # TODO: retornar {:error, :timeout}
      {:exit, razon} ->
        # TODO: retornar {:error, razon}
    end)
  end

  # TODO: Implementa `benchmark/1` que compara secuencial vs paralelo:
  # 1. Mide tiempo de Enum.map(lista, &procesar/1) (secuencial)
  # 2. Mide tiempo de procesar_lista(lista) (paralelo)
  # 3. Imprime ambos tiempos
  def benchmark(lista) do
    {tiempo_sec, _} = :timer.tc(fn ->
      # TODO: Enum.map secuencial
    end)

    {tiempo_par, _} = :timer.tc(fn ->
      procesar_lista(lista)
    end)

    IO.puts("Secuencial: #{tiempo_sec / 1000}ms")
    IO.puts("Paralelo (max 4): #{tiempo_par / 1000}ms")
    IO.puts("Speedup: #{Float.round(tiempo_sec / tiempo_par, 2)}x")
  end
end

# Test it:
# ProcesadorParalelo.procesar_lista(1..20 |> Enum.to_list())
# ProcesadorParalelo.benchmark(1..20 |> Enum.to_list())
```

### Exercise 4: Manejo de errores en tasks
```elixir
defmodule TaskErrores do
  # TODO: Implementa `tarea_que_falla/1`:
  # Si valor es :error, lanza raise "¡Error en el task!"
  # Si no, retorna {:ok, valor}
  defp tarea_que_falla(valor) do
    case valor do
      :error ->
        # TODO: raise "¡Error en el task!"
      _ ->
        {:ok, valor}
    end
  end

  # TODO: Implementa `await_seguro/1` que recibe un task:
  # Usa try/catch para capturar excepciones de Task.await
  # Si el task termina bien: retorna {:ok, resultado}
  # Si el task falla (exit): retorna {:error, :task_failed}
  # Si hay timeout: retorna {:error, :timeout}
  def await_seguro(task, timeout_ms \\ 5000) do
    try do
      resultado = Task.await(task, timeout_ms)
      {:ok, resultado}
    catch
      :exit, {:timeout, _} ->
        # TODO: retornar {:error, :timeout}
      :exit, _ ->
        # TODO: retornar {:error, :task_failed}
    end
  end

  # TODO: Implementa `procesar_lista/1` que procesa una lista de valores:
  # Algunos pueden ser :error
  # Lanza un task por elemento, luego usa await_seguro para recolectar
  # Retorna lista de {:ok, val} o {:error, ...}
  def procesar_lista(valores) do
    valores
    |> Enum.map(fn v ->
      Task.async(fn ->
        # TODO: llamar tarea_que_falla(v)
      end)
    end)
    |> Enum.map(fn task ->
      # TODO: llamar await_seguro(task)
    end)
  end

  # TODO: Implementa `demo_timeout/0`:
  # Lanza un task que espera 2000ms
  # Hace await con timeout de 500ms
  # Imprime el resultado (debe ser {:error, :timeout})
  def demo_timeout do
    task = Task.async(fn ->
      :timer.sleep(2_000)
      "Nunca llegaré"
    end)
    resultado = await_seguro(task, 500)
    IO.puts("Demo timeout: #{inspect(resultado)}")
  end
end

# Test it:
# TaskErrores.procesar_lista([:ok, 1, :error, 2, :ok, :error])
# # => [{:ok, {:ok, :ok}}, {:ok, {:ok, 1}}, {:error, :task_failed}, ...]
# TaskErrores.demo_timeout()
```

### Exercise 5: Task vs spawn — cuándo usar cada uno
```elixir
defmodule TaskVsSpawn do
  # CASO 1: Necesitas el resultado → usa Task.async + await
  def caso_con_resultado do
    task = Task.async(fn ->
      :timer.sleep(100)
      42
    end)
    # Más trabajo aquí mientras el task corre...
    resultado = Task.await(task)
    IO.puts("Resultado: #{resultado}")
  end

  # CASO 2: Fire-and-forget, no necesitas resultado → usa Task.start
  # TODO: Implementa `enviar_notificacion/1` que:
  # Usa Task.start para lanzar en background: IO.puts("Notificación enviada: #{msg}")
  # con un :timer.sleep(50) antes para simular latencia
  # No espera ni le importa el resultado
  # Retorna :ok inmediatamente
  def enviar_notificacion(msg) do
    Task.start(fn ->
      :timer.sleep(50)
      # TODO: imprimir "Notificación enviada: #{msg}"
    end)
    :ok   # Retorna inmediatamente sin esperar
  end

  # CASO 3: Trabajo supervisado de larga vida → usa GenServer (no Task)
  # Task es para trabajo puntual (una computación, una petición HTTP)
  # No para loops infinitos o estado persistente

  # TODO: Implementa `comparar/0` que:
  # 1. Llama caso_con_resultado/0 y mide el tiempo con :timer.tc
  # 2. Llama enviar_notificacion("test") 3 veces y mide el tiempo
  # 3. Imprime la diferencia (enviar_notificacion debe ser ~0ms, caso_con_resultado ~100ms)
  def comparar do
    {t1, _} = :timer.tc(fn -> caso_con_resultado() end)
    {t2, _} = :timer.tc(fn ->
      enviar_notificacion("uno")
      enviar_notificacion("dos")
      enviar_notificacion("tres")
      # TODO: esperar un poco para que los tasks de background terminen (200ms)
    end)
    IO.puts("Con resultado (await): #{t1 / 1000}ms")
    IO.puts("Fire-and-forget (start): #{t2 / 1000}ms")
  end
end

# Test it:
# TaskVsSpawn.caso_con_resultado()
# TaskVsSpawn.enviar_notificacion("Alerta!")
# TaskVsSpawn.comparar()
```

### Try It Yourself
Construye un simulador de web scraper paralelo que procese 10 "URLs" concurrentemente.

Requisitos:
- Define una lista de 10 URLs ficticias (strings)
- Cada "descarga" simula latencia aleatoria entre 200ms y 800ms con `:timer.sleep`
- Usa `Task.async_stream` con `max_concurrency: 3` (simula límite de conexiones)
- Retorna una lista de mapas `%{url: url, tiempo_ms: tiempo, tamanio_bytes: tamanio}` donde `tamanio` es un número aleatorio entre 1000 y 50000
- Imprime un resumen al final: tiempo total, promedio de tiempo por URL, URL más lenta

```elixir
defmodule WebScraperSimulado do
  @urls [
    "https://ejemplo.com/pagina/1",
    "https://ejemplo.com/pagina/2",
    # ... hasta 10 URLs
  ]

  def scrape_todo do
    # Tu implementación aquí
  end

  defp scrape_url(url) do
    # Simula latencia y retorna datos ficticios
  end

  defp imprimir_resumen(resultados) do
    # Imprime estadísticas
  end
end
```

## Common Mistakes

### Mistake 1: No hacer await de Task.async (mensaje residual)
```elixir
# ❌ Si no haces await, el resultado queda en el mailbox como mensaje residual
task = Task.async(fn -> calcular() end)
# ... olvidaste hacer Task.await(task) ...
# El proceso padre acumula mensajes sin leer

# ✓ Siempre hacer await, o usar Task.start si no necesitas el resultado
task = Task.async(fn -> calcular() end)
resultado = Task.await(task)   # Siempre hacer esto

# ✓ Si no necesitas el resultado, usa Task.start en su lugar
Task.start(fn -> calcular() end)
```

### Mistake 2: await con timeout demasiado corto en producción
```elixir
# ❌ El timeout por defecto de Task.await es 5000ms — puede no ser suficiente
resultado = Task.await(task)   # 5 segundos, quizás poco para I/O externo

# ✓ Especificar timeout explícito según las necesidades
resultado = Task.await(task, 30_000)   # 30 segundos para operaciones lentas
resultado = Task.await(task, :infinity)   # Sin timeout (usar con cuidado)
```

### Mistake 3: Task.async_stream sin manejar {:exit, reason}
```elixir
# ❌ Si algún task falla, {:exit, reason} en el stream puede romper el pipeline
resultados = Task.async_stream(lista, &procesar/1) |> Enum.to_list()
# Si un task falla: ** (exit) ...

# ✓ Manejar ambos casos del resultado del stream
Task.async_stream(lista, &procesar/1)
|> Enum.map(fn
  {:ok, val} -> {:ok, val}
  {:exit, reason} -> {:error, reason}
end)
```

### Mistake 4: Usar Task para trabajo de larga vida
```elixir
# ❌ Task no está diseñado para loops infinitos
task = Task.async(fn ->
  loop_infinito()   # Nunca termina — await también espera infinitamente
end)
Task.await(task)   # Se cuelga para siempre

# ✓ Para trabajo de larga vida, usa GenServer o Agent
# Task es para computaciones puntuales que terminan
```

## Verification
```bash
$ iex
iex> TaskBasico.secuencial()
# Secuencial: ~1200ms
iex> TaskBasico.paralelo()
# Paralelo: ~500ms
iex> ProcesadorParalelo.benchmark(1..20 |> Enum.to_list())
# Speedup: ~4x (4 workers)
iex> TaskErrores.demo_timeout()
# Demo timeout: {:error, :timeout}
```

Checklist de verificación:
- [ ] `Task.async` lanza trabajo en paralelo y retorna una struct `%Task{}`
- [ ] `Task.await` bloquea hasta obtener el resultado del task
- [ ] Múltiples tasks lanzados antes del primer await corren verdaderamente en paralelo
- [ ] `Task.async_stream` respeta `max_concurrency`
- [ ] Los errores en tasks se capturan con try/catch en await
- [ ] `Task.start` es fire-and-forget, no requiere await
- [ ] El speedup con 4 workers sobre 20 elementos es ~4x vs secuencial

## Summary
- `Task.async/1` + `Task.await/2` es el patrón estándar para trabajo concurrente con resultado
- Lanzar N tasks antes del primer await es el secreto del verdadero paralelismo
- `Task.async_stream/3` procesa colecciones con límite de concurrencia configurable
- `Task.start/1` es para fire-and-forget cuando no necesitas el resultado
- Los tasks enlazados con `async` propagan errores — manejar con try/catch en await
- Task es para computaciones puntuales; para estado persistente usa Agent o GenServer

## What's Next
**04-genserver-basico**: Aprende GenServer, la abstracción más poderosa de OTP para procesos con estado, API clara, y ciclo de vida completo (init, handle_call, handle_cast, handle_info, terminate).

## Resources
- [Task — HexDocs](https://hexdocs.pm/elixir/Task.html)
- [Task.async/1 — HexDocs](https://hexdocs.pm/elixir/Task.html#async/1)
- [Task.async_stream/3 — HexDocs](https://hexdocs.pm/elixir/Task.html#async_stream/3)
- [Mix and OTP: Task and gen_tcp](https://elixir-lang.org/getting-started/mix-otp/task-and-gen-tcp.html)
