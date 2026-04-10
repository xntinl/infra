# 31 - Concurrencia: Fan-Out / Fan-In

## Prerequisites

- `Task` y concurrencia básica (ejercicio 03)
- Pattern matching y estructuras de datos
- Comprensión del modelo de actores y procesos Elixir
- Conocimiento básico de `Enum` y `Stream`

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Implementar el patrón fan-out/fan-in con `Task.async_stream/3`
2. Controlar la concurrencia con `max_concurrency`, timeouts y `on_timeout`
3. Distinguir entre resultados `:ordered` y `:unordered`
4. Manejar errores parciales sin abortar el pipeline completo
5. Aplicar timeouts globales con `Task.async` + `Task.yield_many/2`
6. Implementar map-reduce concurrente sobre colecciones grandes

---

## Concepts

### El patrón Fan-Out / Fan-In

Fan-out/fan-in es un patrón donde una tarea se divide en múltiples subtareas concurrentes (fan-out) y luego sus resultados se recopilan en un único punto (fan-in).

```
Input
  │
  ├──► Worker 1 ──┐
  ├──► Worker 2 ──┤
  ├──► Worker 3 ──┼──► Aggregator ──► Output
  └──► Worker N ──┘
```

### Task.async_stream: el mecanismo principal

`Task.async_stream/3` itera sobre una enumeración lanzando una tarea por elemento, con control sobre concurrencia y timeout.

```elixir
urls = ["https://example.com", "https://elixir-lang.org", "https://hex.pm"]

results =
  urls
  |> Task.async_stream(
    fn url -> fetch_url(url) end,
    max_concurrency: 4,    # máximo de tareas simultáneas (default: System.schedulers_online())
    timeout: 5_000,        # ms por tarea (default: 5_000)
    on_timeout: :kill_task # :kill_task | :exit (default: :exit)
  )
  |> Enum.to_list()

# Cada elemento de results es {:ok, valor} o {:exit, reason}
```

### Opciones clave

```elixir
# ordered: true (default) — los resultados mantienen el orden del input
# ordered: false — los resultados llegan en orden de finalización (más eficiente)
Task.async_stream(collection, fn item -> work(item) end, ordered: false)

# on_timeout: :kill_task — la tarea que excede el timeout devuelve {:exit, :timeout}
# on_timeout: :exit — el proceso llamador muere (comportamiento más agresivo)
Task.async_stream(collection, fn item -> work(item) end,
  timeout: 3_000,
  on_timeout: :kill_task
)

# zip_input_on_exit: true — incluye el input en el error para saber qué falló
Task.async_stream(items, fn item -> process(item) end,
  zip_input_on_exit: true  # Elixir 1.15+
)
# {:exit, {item, :timeout}} en lugar de {:exit, :timeout}
```

### Manejo de errores parciales

```elixir
defmodule FanOut do
  def process_all(items, work_fn, opts \\ []) do
    max_concurrency = Keyword.get(opts, :max_concurrency, System.schedulers_online())
    timeout         = Keyword.get(opts, :timeout, 5_000)

    items
    |> Task.async_stream(work_fn,
         max_concurrency: max_concurrency,
         timeout: timeout,
         on_timeout: :kill_task
       )
    |> Enum.reduce({[], []}, fn
      {:ok, result},     {ok, err} -> {[result | ok], err}
      {:exit, reason},   {ok, err} -> {ok, [{:error, reason} | err]}
    end)
    |> then(fn {ok, err} ->
      %{
        results: Enum.reverse(ok),
        errors:  Enum.reverse(err),
        total:   length(items)
      }
    end)
  end
end
```

### Timeout global con Task.yield_many

Cuando necesitas un timeout que cubra TODAS las tareas simultáneamente (no individual):

```elixir
defmodule GlobalTimeout do
  def run_with_global_timeout(work_items, global_timeout_ms) do
    tasks = Enum.map(work_items, fn item ->
      Task.async(fn -> process(item) end)
    end)

    # yield_many espera hasta global_timeout_ms o hasta que todas terminen
    results = Task.yield_many(tasks, global_timeout_ms)

    Enum.map(results, fn {task, result} ->
      case result do
        {:ok, value}  -> {:ok, value}
        {:exit, reason} -> {:error, reason}
        nil ->
          # La tarea no terminó: matarla y marcar como timeout
          Task.shutdown(task, :brutal_kill)
          {:error, :global_timeout}
      end
    end)
  end

  defp process(item), do: item
end
```

### Map-Reduce concurrente

```elixir
defmodule ConcurrentMapReduce do
  @doc """
  Aplica `map_fn` en paralelo sobre `collection` y reduce con `reduce_fn`.
  """
  def map_reduce(collection, map_fn, reduce_fn, initial, opts \\ []) do
    collection
    |> Task.async_stream(map_fn, opts)
    |> Stream.filter(fn {status, _} -> status == :ok end)
    |> Stream.map(fn {:ok, value} -> value end)
    |> Enum.reduce(initial, reduce_fn)
  end
end

# Ejemplo: sumar longitudes de respuestas HTTP en paralelo
total_bytes =
  ConcurrentMapReduce.map_reduce(
    urls,
    fn url -> String.length(fetch_body(url)) end,
    fn size, acc -> acc + size end,
    0,
    max_concurrency: 10
  )
```

---

## Exercises

### Ejercicio 1: Scraper paralelo de URLs

Implementa un scraper que descarga múltiples URLs en paralelo, con manejo de errores y timeout por URL.

```elixir
defmodule ParallelScraper do
  @doc """
  Descarga una lista de URLs en paralelo.

  Opciones:
  - max_concurrency: integer (default: 10)
  - timeout_ms: integer por URL (default: 5_000)
  - extract_fn: función para extraer datos del body (default: &Function.identity/1)

  Devuelve %{ok: [{url, data}], errors: [{url, reason}]}
  """
  def scrape(urls, opts \\ []) do
    max_concurrency = Keyword.get(opts, :max_concurrency, 10)
    timeout_ms      = Keyword.get(opts, :timeout_ms, 5_000)
    extract_fn      = Keyword.get(opts, :extract_fn, &Function.identity/1)

    # TODO: usar Task.async_stream con ordered: false (los resultados llegan
    # en orden de finalización, más eficiente para scraping)
    # Cada tarea debe devolver {:ok, {url, data}} o {:error, {url, reason}}
    #
    # Dentro de la tarea:
    # result = fetch_url(url)
    # case result do
    #   {:ok, body}      -> {:ok, {url, extract_fn.(body)}}
    #   {:error, reason} -> {:error, {url, reason}}
    # end

    # TODO: separar resultados en ok y errors usando Enum.reduce o Enum.group_by
    # Retornar %{ok: [...], errors: [...]}
  end

  @doc """
  Simula descarga HTTP. En producción reemplazar por HTTPoison/Req/Finch.
  """
  def fetch_url(url) do
    # Simulación: algunas URLs fallan, otras son lentas
    case url do
      "http://timeout.example"  ->
        Process.sleep(10_000)  # excederá el timeout
        {:ok, ""}

      "http://error.example"    ->
        {:error, :connection_refused}

      _valid ->
        Process.sleep(:rand.uniform(200))  # latencia variable
        {:ok, "<html>Contenido de #{url}</html>"}
    end
  end
end
```

```elixir
# Verificación:
# urls = [
#   "http://site1.example",
#   "http://site2.example",
#   "http://error.example",
#   "http://timeout.example",
#   "http://site3.example"
# ]
#
# result = ParallelScraper.scrape(urls,
#   max_concurrency: 3,
#   timeout_ms: 1_000,
#   extract_fn: fn body -> Regex.run(~r/<html>(.*)<\/html>/, body, capture: :all_but_first) end
# )
#
# IO.inspect(result.ok,     label: "Exitosos")  # 3 URLs
# IO.inspect(result.errors, label: "Fallidos")  # 2 URLs (error + timeout)
# IO.puts("Total: #{length(result.ok) + length(result.errors)}/#{length(urls)}")
```

---

### Ejercicio 2: Image Thumbnail Generator concurrente

Simula un generador de thumbnails que procesa imágenes en paralelo con múltiples tamaños por imagen (fan-out de dos niveles).

```elixir
defmodule ThumbnailGenerator do
  @sizes [{:small, 150}, {:medium, 400}, {:large, 800}]

  @doc """
  Genera thumbnails para una lista de imágenes en paralelo.

  Para cada imagen, genera todos los tamaños de forma concurrente.
  Devuelve una lista de %{source: path, thumbnails: [%{size: atom, path: string}], errors: [...]}
  """
  def generate_all(image_paths, opts \\ []) do
    max_concurrency = Keyword.get(opts, :max_concurrency, System.schedulers_online())
    timeout_ms      = Keyword.get(opts, :timeout_ms, 10_000)

    # TODO: Fan-out de primer nivel: una tarea por imagen
    # Cada tarea llama a process_image/2
    image_paths
    |> Task.async_stream(
      fn path -> process_image(path, timeout_ms) end,
      max_concurrency: max_concurrency,
      timeout: timeout_ms + 1_000,  # timeout global ligeramente mayor
      on_timeout: :kill_task
    )
    |> Enum.zip(image_paths)
    |> Enum.map(fn
      {{:ok, result}, _path}       -> result
      {{:exit, reason}, path}      ->
        %{source: path, thumbnails: [], errors: [{:all, reason}]}
    end)
  end

  @doc """
  Procesa una imagen: genera todos los tamaños en paralelo (fan-out de segundo nivel).
  """
  def process_image(path, timeout_ms) do
    # TODO: Fan-out de segundo nivel: una tarea por tamaño
    # Para cada {size_name, width} en @sizes, llama a resize_image/3
    # Recopilar resultados separando éxitos de errores
    #
    # Retornar %{source: path, thumbnails: [...], errors: [...]}
  end

  @doc """
  Simula el redimensionado de una imagen.
  """
  def resize_image(source_path, size_name, width) do
    # Simular procesamiento con latencia variable
    Process.sleep(:rand.uniform(300))

    cond do
      String.ends_with?(source_path, ".corrupt") ->
        {:error, {:corrupt_file, source_path}}

      width > 600 and :rand.uniform(10) == 1 ->
        {:error, {:out_of_memory, size_name}}

      true ->
        output_path = "thumbnails/#{size_name}/#{Path.basename(source_path)}"
        {:ok, %{size: size_name, width: width, path: output_path}}
    end
  end
end
```

```elixir
# Verificación:
# images = [
#   "photos/beach.jpg",
#   "photos/mountain.jpg",
#   "photos/broken.corrupt",
#   "photos/city.jpg"
# ]
#
# results = ThumbnailGenerator.generate_all(images, max_concurrency: 2)
#
# Enum.each(results, fn r ->
#   IO.puts("\nImagen: #{r.source}")
#   IO.puts("  Thumbnails OK: #{length(r.thumbnails)}")
#   IO.puts("  Errores: #{length(r.errors)}")
# end)
#
# # La imagen .corrupt debe tener 0 thumbnails y errores
# corrupt = Enum.find(results, &String.ends_with?(&1.source, ".corrupt"))
# corrupt.thumbnails  # []
# corrupt.errors      # [{:small/:medium/:large, ...}]  o {:all, ...}
```

---

### Ejercicio 3: Aggregator con timeout global

Implementa un aggregator que consulta múltiples fuentes de datos con un timeout global (no por fuente individual).

```elixir
defmodule DataAggregator do
  @moduledoc """
  Consulta múltiples fuentes de datos en paralelo.
  El timeout es GLOBAL: si alguna fuente es muy lenta,
  las que terminaron a tiempo se incluyen en el resultado.
  """

  @doc """
  Consulta todas las fuentes dentro de `global_timeout_ms`.

  Fuentes que no responden a tiempo se marcan como :timeout.
  Fuentes que fallan se marcan como {:error, reason}.

  Devuelve %{source_name => {:ok, data} | {:error, reason} | :timeout}
  """
  def aggregate(sources, global_timeout_ms \\ 3_000)
      when is_list(sources) and is_integer(global_timeout_ms) do
    # sources = [{:name, fetch_fn}, ...]

    # TODO: lanzar Task.async por cada fuente
    # Usar Task.yield_many/2 con global_timeout_ms
    # Mapear resultados:
    #   {:ok, value}  -> {:ok, value}
    #   {:exit, r}    -> {:error, r}
    #   nil           -> (hacer Task.shutdown + :timeout)

    # Retornar mapa %{name => result}
  end

  # --- Fuentes de datos simuladas ---

  def fetch_users do
    Process.sleep(200)
    {:ok, [%{id: 1, name: "Ana"}, %{id: 2, name: "Bob"}]}
  end

  def fetch_orders do
    Process.sleep(500)
    {:ok, [%{id: 100, total: 99.9}, %{id: 101, total: 49.5}]}
  end

  def fetch_inventory do
    Process.sleep(4_000)  # muy lento: excederá el timeout global
    {:ok, [%{sku: "ABC", stock: 5}]}
  end

  def fetch_recommendations do
    Process.sleep(100)
    if :rand.uniform(3) == 1 do
      raise "Recommendation service unavailable"
    end
    {:ok, ["Producto A", "Producto B"]}
  end
end
```

```elixir
# Verificación:
# sources = [
#   {:users,           &DataAggregator.fetch_users/0},
#   {:orders,          &DataAggregator.fetch_orders/0},
#   {:inventory,       &DataAggregator.fetch_inventory/0},
#   {:recommendations, &DataAggregator.fetch_recommendations/0}
# ]
#
# result = DataAggregator.aggregate(sources, 2_000)
#
# IO.inspect(result[:users])           # {:ok, [...]}     — rápido, OK
# IO.inspect(result[:orders])          # {:ok, [...]}     — rápido, OK
# IO.inspect(result[:inventory])       # :timeout         — demasiado lento
# IO.inspect(result[:recommendations]) # {:ok, ...} o {:error, ...}  — aleatorio
#
# # Verificar que el tiempo total es ~2_000ms, no 4_000+ms
# {time, _} = :timer.tc(fn -> DataAggregator.aggregate(sources, 2_000) end)
# IO.puts("Tiempo total: #{div(time, 1000)}ms")  # ~2000ms
```

---

## Common Mistakes

**1. Olvidar que `Task.async_stream` es lazy hasta `Enum.to_list` o similar**

```elixir
# MAL: el Stream no se evalúa, las tareas no se lanzan
stream = Task.async_stream(items, &work/1)
# ... horas después ...
Enum.to_list(stream)  # recién aquí se ejecuta

# BIEN: si necesitas materializar inmediatamente
results = items |> Task.async_stream(&work/1) |> Enum.to_list()
```

**2. Ignorar errores en el resultado**

```elixir
# MAL: si una tarea falla, el pattern match explota
results = items |> Task.async_stream(&work/1) |> Enum.map(fn {:ok, v} -> v end)
# MatchError si alguna tarea lanza excepción

# BIEN: manejar ambos casos
results = items
  |> Task.async_stream(&work/1, on_timeout: :kill_task)
  |> Enum.flat_map(fn
    {:ok, value}   -> [value]
    {:exit, _reason} -> []  # o loguear
  end)
```

**3. `max_concurrency` demasiado alto en operaciones I/O limitadas**

```elixir
# MAL: 1000 tareas simultáneas saturan el pool de conexiones HTTP
Task.async_stream(1..1000, &fetch_url/1, max_concurrency: 1000)

# BIEN: limitar según el recurso externo
Task.async_stream(1..1000, &fetch_url/1, max_concurrency: 20)
```

**4. Confundir timeout individual vs global**

```elixir
# async_stream: timeout POR TAREA
Task.async_stream(items, &work/1, timeout: 5_000)
# Si hay 100 items, el tiempo total puede ser hasta 500_000ms (100 * 5_000)
# con max_concurrency: 1

# Para timeout GLOBAL: Task.yield_many
tasks = Enum.map(items, fn i -> Task.async(fn -> work(i) end) end)
Task.yield_many(tasks, 5_000)  # 5 segundos en total, no por tarea
```

**5. No hacer `Task.shutdown` en tareas no completadas**

```elixir
# MAL: las tareas siguen corriendo aunque ya no las necesitemos
results = Task.yield_many(tasks, 1_000)

# BIEN: matar las tareas que no terminaron
Task.yield_many(tasks, 1_000)
|> Enum.each(fn {task, result} ->
  if is_nil(result), do: Task.shutdown(task, :brutal_kill)
end)
```

---

## Verification

```elixir
# Test básico de fan-out/fan-in en iex:

# 1. async_stream básico
1..5
|> Task.async_stream(fn i ->
     Process.sleep(i * 100)
     i * i
   end, max_concurrency: 5)
|> Enum.map(fn {:ok, v} -> v end)
# [1, 4, 9, 16, 25]

# 2. Verificar ordered: false llega antes
:timer.tc(fn ->
  1..5
  |> Task.async_stream(
       fn i -> Process.sleep((6 - i) * 100); i end,
       ordered: false,
       max_concurrency: 5
     )
  |> Enum.to_list()
end)
# Con ordered: false, el elemento 5 (el más rápido) llega primero

# 3. Timeout individual
["rapido", "lento"]
|> Task.async_stream(fn url ->
     if url == "lento", do: Process.sleep(5_000)
     url
   end, timeout: 500, on_timeout: :kill_task)
|> Enum.to_list()
# [{:ok, "rapido"}, {:exit, :timeout}]

# 4. yield_many con timeout global
tasks = Enum.map(1..3, fn i ->
  Task.async(fn -> Process.sleep(i * 1_000); i end)
end)
Task.yield_many(tasks, 1_500)
# [{task1, {:ok, 1}}, {task2, {:ok, 2}}, {task3, nil}]
# task3 no completó en 1500ms
```

---

## Summary

Fan-out/fan-in es el patrón de concurrencia más común en Elixir para operaciones I/O:

| Herramienta | Caso de uso |
|---|---|
| `Task.async_stream` | Procesar colección en paralelo con límite de concurrencia |
| `ordered: false` | Cuando el orden no importa y se quiere máxima eficiencia |
| `on_timeout: :kill_task` | Tolerancia a fallos: continuar sin la tarea fallida |
| `Task.yield_many` | Timeout GLOBAL sobre múltiples tareas |
| `Task.shutdown` | Limpiar tareas que no terminaron en tiempo |

La clave del patrón es la composición con el pipe operator: la query se construye de forma declarativa y Elixir maneja el scheduling sobre los schedulers de la BEAM.

---

## What's Next

- **32**: String.Chars e Inspect Protocol — protocolos para representación de datos
- **GenStage**: backpressure en pipelines de datos (ejercicio 27)
- **Flow**: procesamiento paralelo sobre colecciones grandes (construido sobre GenStage)
- **Broadway**: procesamiento de mensajes (Kafka, SQS) con fan-out integrado

---

## Resources

- [Task.async_stream docs](https://hexdocs.pm/elixir/Task.html#async_stream/5)
- [Task.yield_many docs](https://hexdocs.pm/elixir/Task.html#yield_many/2)
- [Concurrent Data Processing in Elixir (Pragmatic Bookshelf)](https://pragprog.com/titles/sgdpelixir/concurrent-data-processing-in-elixir/)
- [The Soul of Erlang and Elixir — Sasa Juric (talk)](https://www.youtube.com/watch?v=JvBT4XBdoUE)
- [Flow library](https://hexdocs.pm/flow/Flow.html)
