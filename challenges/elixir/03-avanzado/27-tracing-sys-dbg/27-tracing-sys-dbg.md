# 27. Tracing en Producción: :sys, :dbg y :recon_trace

**Difficulty**: Avanzado

---

## Prerequisites

### Mastered
- GenServer: ciclo de vida completo, handle_call/cast/info
- OTP: supervisores, aplicaciones
- Procesos Elixir: spawn, send, receive, Process.monitor

### Familiarity with
- `:sys` module — GenServer internals
- `:recon` library (ejercicio 26)
- Erlang's pattern matching syntax (para los match specs de dbg)

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

- **Analizar** el comportamiento de GenServers en tiempo real sin reiniciarlos ni modificar código
- **Diseñar** estrategias de tracing que no degraden la performance del sistema en producción
- **Evaluar** el trade-off entre visibilidad y overhead para cada herramienta de tracing
- **Construir** un call graph básico de invocaciones de módulo usando match specs de `:dbg`

---

## Concepts

### ¿Por Qué Tracing en Lugar de Logging?

Logging requiere modificar el código y hacer deploy. Tracing es dinámico — lo activas en un nodo vivo, observas lo que necesitas, y lo desactivas. En sistemas de producción donde el problema desaparece al reiniciar, el tracing es la única opción viable.

```
Herramienta         Scope              Overhead    Seguridad en Prod
─────────────────────────────────────────────────────────────────────
:sys.trace          Un GenServer        Bajo        Sí (por proceso)
:sys.log            Un GenServer        Mínimo      Sí (ring buffer)
:dbg                Sistema completo    ALTO        NO sin límites
:recon_trace        Sistema completo    Controlado  Sí (con límites)
```

### `:sys` — Introspección de Procesos OTP

Cualquier proceso que siga el OTP behaviour protocol (GenServer, GenStateMachine, etc.) implementa la interfaz `:sys`. Es la forma más segura de inspeccionar un proceso OTP sin afectar otros.

```elixir
{:ok, pid} = MyGenServer.start_link([])

# Activar trace — imprime cada mensaje que recibe/envía el proceso
:sys.trace(pid, true)
# Al hacer GenServer.call(pid, :get), verás en stdout:
# *DBG* <0.123.0> got call get from <0.84.0>
# *DBG* <0.123.0> sent {ok, value} to <0.84.0>, new state: %{...}

# Desactivar
:sys.trace(pid, false)

# Ring buffer de los últimos N eventos (sin imprimir nada)
:sys.log(pid, 20)  # guardar hasta 20 eventos
# ... el proceso recibe llamadas ...
:sys.log_to_file(pid, "/tmp/genserver.log")  # volcar a archivo

# Ver el log acumulado
{:ok, events} = :sys.log(pid, :get)

# Obtener el estado actual del GenServer (llama a handle_system_msg)
:sys.get_state(pid)

# Reemplazar el estado (útil para debug — peligroso en producción)
:sys.replace_state(pid, fn state -> Map.put(state, :debug_mode, true) end)
```

### `:sys.log/2` — Ring Buffer de Eventos

```elixir
# Estructura de un evento del log:
# {:in, message}          — mensaje entrante (call, cast, info)
# {:out, reply, to_pid}   — respuesta saliente a un call
# {:noreply, new_state}   — tras un cast sin respuesta

# Ejemplo de uso para capturar sin interferir:
:sys.log(pid, 50)  # capturar últimos 50 eventos

# Esperar a que ocurran algunos mensajes...
Process.sleep(5_000)

# Recuperar y analizar
{:ok, events} = :sys.log(pid, :get)

Enum.each(events, fn event ->
  IO.inspect(event, label: "Event")
end)

# Siempre limpiar después
:sys.log(pid, false)
```

### `:dbg` — Tracing de Bajo Nivel

`:dbg` es poderoso pero **peligroso en producción sin límites**. Puede generar tal volumen de mensajes que degrade o mate el nodo.

```elixir
# Iniciar el tracer (procesa los eventos de trace)
:dbg.tracer()

# Tracing de un proceso específico — todos sus mensajes
:dbg.p(pid, [:m])  # m = messages

# Tracing de todas las llamadas a una función
:dbg.tp(MyModule, :my_function, [])  # tp = trace pattern

# Con match spec — solo trazar cuando el primer arg es > 100
:dbg.tp(MyModule, :my_function, [{[:"$1", :_], [{:>, :"$1", 100}], [{:return_trace}]}])

# Remover trace patterns
:dbg.ctpg()  # clear all trace patterns

# SIEMPRE parar el tracer cuando termines
:dbg.stop_clear()
```

### Match Specs — El Lenguaje de Filtrado de `:dbg`

Los match specs son tuplas que actúan como predicados. Son la forma de decirle a `:dbg` qué llamadas interceptar:

```elixir
# Formato: [{head_pattern, guard_conditions, actions}]

# Trazar todas las llamadas sin filtro:
[]  # o equivalentemente: [{'_', [], []}]

# Trazar solo cuando el primer argumento es el átomo :error:
[{[:error, :_], [], [:return_trace]}]

# Trazar y capturar el valor de retorno:
[{:_, [], [:return_trace]}]

# Trazar cuando el segundo arg es > 1000:
[{[:_, :"$1"], [{:>, :"$1", 1000}], [:return_trace]}]

# Elixir helper para construir match specs más legibles:
# :dbg.fun2ms(fn [x, y] when x > y -> :return_trace end)
```

### `:recon_trace` — Tracing Seguro para Producción

`:recon_trace` envuelve `:dbg` con garantías de seguridad críticas:

```elixir
# Limitar a máximo 100 calls totales — SIEMPRE usar un límite
:recon_trace.calls({MyModule, :my_function, :_}, 100)

# Con formatter personalizado
:recon_trace.calls(
  {MyModule, :my_function, :_},
  50,
  formatter: fn event -> IO.puts("Call: #{inspect(event)}") end
)

# Tracing con match spec — solo args específicos
match_spec = [{{:_, :_, :"$1"}, [{:>, :"$1", 1000}], []}]
:recon_trace.calls({MyModule, :my_function, match_spec}, 100)

# Parar explícitamente (o esperar a que llegue al límite)
:recon_trace.clear()
```

### Overhead de Tracing y Cómo Minimizarlo

| Factor | Impacto | Mitigación |
|--------|---------|-----------|
| Volumen de calls trazadas | Directo con throughput | Límite en `:recon_trace`, match specs selectivos |
| Formato de output | Serialización es cara | Formatters mínimos, escribir a buffer |
| `:sys.trace` | Solo un proceso | Seguro por naturaleza |
| `:dbg` sin límites | Puede matar el nodo | NUNCA en producción sin límites |
| Scope del trace | Todo el nodo vs un proceso | Preferir `:sys` para GenServers individuales |

---

## Exercises

### Exercise 1: :sys Trace — Auditoría de Mensajes de un GenServer

**Problem**

Implementa un módulo `ShoppingCart` como GenServer que gestiona un carrito de compras, y un módulo `CartAuditor` que use `:sys.log` para capturar la historia completa de mensajes de un carrito específico.

El `ShoppingCart` debe soportar:
- `add_item(pid, item, quantity)` — añadir item
- `remove_item(pid, item)` — eliminar item
- `get_total(pid)` — precio total
- `checkout(pid)` — finalizar (limpia el carrito)

El `CartAuditor` debe:
1. Activar el log con buffer de 50 eventos
2. Realizar una secuencia de operaciones
3. Recuperar el log y mostrar un audit trail legible
4. Calcular estadísticas: cuántos calls, cuántos casts, tiempo entre eventos

```
=== ShoppingCart Audit Trail ===
[00:00.000] CALL  add_item {:apple, 2}          -> :ok
[00:00.012] CALL  add_item {:banana, 1}         -> :ok
[00:00.024] CAST  log_event :items_added
[00:00.036] CALL  get_total                     -> 5.50
[00:00.048] CALL  checkout                      -> {:ok, receipt}

Summary: 4 calls, 1 cast, 48ms total
```

**Hints**

- `:sys.log(pid, N)` activa la captura; `:sys.log(pid, :get)` recupera los eventos
- Los eventos tienen formato `{:in, message}` o `{:out, reply, from_pid}`
- Para el timestamp relativo: guarda `:erlang.monotonic_time(:millisecond)` antes y después de cada evento
- `:sys.log(pid, false)` desactiva y limpia el buffer — hazlo al terminar
- El `pid` en `:sys` funciones puede ser también un nombre registrado: `:sys.log(MyServer, 50)`

**One possible solution**

```elixir
defmodule ShoppingCart do
  use GenServer

  @prices %{apple: 1.50, banana: 0.75, orange: 2.00}

  def start_link(opts \\ []),
    do: GenServer.start_link(__MODULE__, %{}, opts)

  def add_item(pid, item, qty),
    do: GenServer.call(pid, {:add_item, item, qty})

  def remove_item(pid, item),
    do: GenServer.call(pid, {:remove_item, item})

  def get_total(pid),
    do: GenServer.call(pid, :get_total)

  def checkout(pid),
    do: GenServer.call(pid, :checkout)

  def init(_), do: {:ok, %{items: %{}, started_at: DateTime.utc_now()}}

  def handle_call({:add_item, item, qty}, _from, %{items: items} = state) do
    updated = Map.update(items, item, qty, &(&1 + qty))
    {:reply, :ok, %{state | items: updated}}
  end

  def handle_call({:remove_item, item}, _from, %{items: items} = state) do
    {:reply, :ok, %{state | items: Map.delete(items, item)}}
  end

  def handle_call(:get_total, _from, %{items: items} = state) do
    total =
      Enum.reduce(items, 0.0, fn {item, qty}, acc ->
        acc + Map.get(@prices, item, 0.0) * qty
      end)

    {:reply, Float.round(total, 2), state}
  end

  def handle_call(:checkout, _from, %{items: items} = state) do
    receipt = %{
      items: items,
      total: calculate_total(items),
      timestamp: DateTime.utc_now()
    }
    {:reply, {:ok, receipt}, %{state | items: %{}}}
  end

  defp calculate_total(items) do
    Enum.reduce(items, 0.0, fn {item, qty}, acc ->
      acc + Map.get(@prices, item, 0.0) * qty
    end)
    |> Float.round(2)
  end
end

defmodule CartAuditor do
  def audit(pid, operations_fn) do
    # Activar captura de log
    :sys.log(pid, 50)

    start_ts = :erlang.monotonic_time(:millisecond)

    # Ejecutar operaciones
    operations_fn.()

    end_ts = :erlang.monotonic_time(:millisecond)

    # Recuperar eventos
    {:ok, events} = :sys.log(pid, :get)
    :sys.log(pid, false)

    IO.puts("\n=== ShoppingCart Audit Trail ===")

    # TODO: formatear cada evento con tipo (call/cast/info) y contenido
    # TODO: calcular tiempo relativo entre eventos
    Enum.each(events, fn event ->
      IO.inspect(event, label: "Event")
    end)

    calls = Enum.count(events, fn e -> match?({:in, {:"$gen_call", _, _}}, e) end)
    casts = Enum.count(events, fn e -> match?({:in, {:"$gen_cast", _}}, e) end)
    elapsed = end_ts - start_ts

    IO.puts("\nSummary: #{calls} calls, #{casts} casts, #{elapsed}ms total")

    {:ok, events}
  end
end
```

---

### Exercise 2: :recon_trace en Producción — Trace Limitado y Seguro

**Problem**

Simula un escenario de producción donde necesitas diagnosticar por qué ciertos requests están siendo lentos. Tienes un módulo `SlowAPI` que ocasionalmente tarda más de lo esperado, y necesitas capturar exactamente cuándo y con qué argumentos ocurre.

Implementa:

1. `SlowAPI` — módulo con una función `handle_request/2` que introduce latencia aleatoria
2. `ProductionTracer` — módulo que usa `:recon_trace` para:
   - Trazar solo las llamadas donde el segundo argumento supera un threshold
   - Limitar a máximo 50 traces totales
   - Formatear el output de forma legible
   - Medir el tiempo de cada call usando `:return_trace`

```elixir
# Usar en producción:
ProductionTracer.trace_slow_requests(threshold_ms: 100, max_traces: 50)

# Output:
# [14:32:01.234] SlowAPI.handle_request(:user_profile, 1234) → 342ms ← SLOW
# [14:32:01.891] SlowAPI.handle_request(:user_feed, 5678) → 89ms
# [14:32:02.103] SlowAPI.handle_request(:recommendations, 9999) → 891ms ← SLOW
# Captured 50 calls. Stopping trace.
```

**Hints**

- `:recon_trace.calls({Module, :function, match_spec}, max_calls)` — siempre con límite
- Para `:return_trace` en recon: usa `[{:_, [], [:return_trace]}]` como match spec
- El formatter recibe un term con la info del trace — inspecciona su estructura con `IO.inspect/2`
- Genera carga con `Task.async_stream` para ver múltiples calls concurrentes
- `:recon_trace.clear()` detiene todos los traces activos — siempre en el `after` de un `try`

**One possible solution**

```elixir
defmodule SlowAPI do
  def handle_request(endpoint, _user_id) do
    # Simular latencia variable: 50% de las veces < 100ms, 50% > 100ms
    latency = if :rand.uniform() > 0.5, do: :rand.uniform(50), else: :rand.uniform(500) + 100
    :timer.sleep(latency)
    {:ok, %{endpoint: endpoint, latency_ms: latency}}
  end
end

defmodule ProductionTracer do
  require Logger

  def trace_slow_requests(opts \\ []) do
    max = Keyword.get(opts, :max_traces, 50)
    _threshold = Keyword.get(opts, :threshold_ms, 100)

    Logger.info("Starting production trace (max #{max} calls)...")

    try do
      # Match spec: trazar todas las calls con return_trace
      # En una implementación real, el threshold se filtraría en el match spec
      # o en el formatter comparando el tiempo de ejecución
      match_spec = [{:_, [], [:return_trace]}]

      :recon_trace.calls(
        {SlowAPI, :handle_request, match_spec},
        max,
        formatter: &format_trace_event/1
      )

      # Esperar a que llegue al límite o se cancele manualmente
      Process.sleep(30_000)
    after
      :recon_trace.clear()
      Logger.info("Trace stopped.")
    end
  end

  defp format_trace_event(event) do
    # La estructura del evento varía entre call y return
    # TODO: distinguir {:trace, pid, :call, {mod, fun, args}} de
    #       {:trace, pid, :return_from, {mod, fun, arity}, return_value}
    timestamp = DateTime.utc_now() |> DateTime.to_string()
    IO.puts("[#{timestamp}] #{inspect(event)}")
  end
end
```

---

### Exercise 3: Call Graph con :dbg — Construir un Grafo de Invocaciones

**Problem**

Usa `:dbg` para construir un call graph básico de un módulo: qué funciones llaman a qué otras funciones durante la ejecución real (no estática).

Implementa un `CallGraphBuilder` que:

1. Active trace en todas las funciones del módulo objetivo
2. Ejecute un workload de prueba
3. Capture las relaciones caller→callee usando `:return_trace`
4. Genere una representación del call graph en formato texto (Mermaid o dot)

```elixir
# Trazar las invocaciones durante el workload
CallGraphBuilder.trace_module(MyModule, fn ->
  MyModule.process_batch([1, 2, 3, 4, 5])
end)

# Output en formato Mermaid:
# graph TD
#   process_batch --> validate_item
#   process_batch --> transform_item
#   validate_item --> check_range
#   transform_item --> normalize
```

El módulo a trazar (`MyModule`) debe tener una jerarquía de al menos 4 funciones con llamadas entre sí.

**Hints**

- `:dbg.tp(Module, :_, [])` traza TODAS las funciones exportadas del módulo
- `:dbg.tpl(Module, :_, [])` traza también las funciones privadas (local)
- El tracer callback recibe `{:trace, pid, :call, {mod, fun, args}}` — extrae `{fun, arity}`
- Para capturar la relación caller→callee necesitas un stack por proceso — guárdalo en un Agent o ETS
- Siempre `:dbg.stop_clear()` al terminar — incluso si el workload falla
- Limita el trace a un solo PID para evitar capturar llamadas del resto del sistema: `:dbg.p(target_pid, :c)`

**One possible solution**

```elixir
defmodule MyModule do
  # Módulo de ejemplo con jerarquía de funciones
  def process_batch(items) do
    items
    |> Enum.filter(&validate_item/1)
    |> Enum.map(&transform_item/1)
  end

  def validate_item(item), do: check_range(item) && check_type(item)
  def transform_item(item), do: normalize(item) |> format()

  defp check_range(item), do: item > 0 && item < 1000
  defp check_type(item), do: is_integer(item)
  defp normalize(item), do: item / 100.0
  defp format(value), do: Float.round(value, 2)
end

defmodule CallGraphBuilder do
  def trace_module(module, workload_fn) do
    # Tabla ETS para acumular edges del grafo
    table = :ets.new(:call_graph, [:bag, :public])

    # Iniciar tracer que acumula en ETS
    :dbg.tracer(:process, {fn event, _state ->
      handle_trace_event(event, table)
    end, nil})

    # Trazar solo el proceso que ejecuta el workload
    target_pid = spawn(fn ->
      # TODO: el workload se ejecuta aquí
      workload_fn.()
    end)

    :dbg.p(target_pid, :c)
    :dbg.tpl(module, :_, [{:_, [], [:return_trace]}])

    # Esperar a que el proceso termine
    ref = Process.monitor(target_pid)
    receive do
      {:DOWN, ^ref, _, _, _} -> :ok
    end

    :dbg.stop_clear()

    # Construir y mostrar el grafo
    edges = :ets.tab2list(table)
    :ets.delete(table)

    print_mermaid(edges)
    edges
  end

  defp handle_trace_event({:trace, _pid, :call, {_mod, fun, args}}, table) do
    # TODO: mantener stack por proceso para conocer el caller
    # Por ahora, simplemente registrar la función
    :ets.insert(table, {:call, fun, length(args)})
  end

  defp handle_trace_event(_other, _table), do: :ok

  defp print_mermaid(edges) do
    IO.puts("\ngraph TD")

    edges
    |> Enum.uniq()
    |> Enum.each(fn
      {:edge, caller, callee} ->
        IO.puts("  #{caller} --> #{callee}")
      _ ->
        :ok
    end)
  end
end
```

---

## Common Mistakes

### 1. Usar `:dbg` en producción sin límites

`:dbg` sin restricciones puede generar millones de eventos por segundo en un nodo con carga alta. El procesamiento del tracer se convierte en el cuello de botella y puede hacer que el nodo deje de responder. **Siempre usar `:recon_trace`** en producción.

### 2. Olvidar `:dbg.stop_clear()` después de un error

Si el workload falla con una excepción, `:dbg` sigue corriendo. Un `try/after` es obligatorio:

```elixir
try do
  :dbg.tracer()
  :dbg.tp(Module, :function, [])
  workload()
after
  :dbg.stop_clear()
end
```

### 3. Confundir `:sys.get_state` con `:sys.log`

- `:sys.get_state/1` retorna el estado actual del GenServer en este momento — una foto instantánea
- `:sys.log/2` captura el historial de mensajes en un ring buffer — es dinámico

### 4. Activar trace en producción sobre funciones de alto throughput

Trazar `Ecto.Repo.all/2` o `Phoenix.Router.dispatch/2` en un nodo con 10k req/s saturará el tracer inmediatamente. Usa match specs para filtrar antes de capturar:

```elixir
# MAL: traza absolutamente todo
:recon_trace.calls({Ecto.Repo, :all, :_}, 100)

# BIEN: solo cuando el segundo arg coincide con una query específica
match_spec = [{{:_, :"$1"}, [{:==, :"$1", {:query, :users}}], []}]
:recon_trace.calls({Ecto.Repo, :all, match_spec}, 100)
```

### 5. No considerar que `:sys` modifica la semántica de errores

Si el proceso tracedado muere, `:sys.log/2` puede retornar `{:error, :noproc}`. Siempre envuelve las llamadas `:sys` en un `try/rescue` cuando el proceso puede estar muerto o reiniciándose.

### 6. Asumir que el call graph dinámico coincide con el estático

El call graph capturado con `:dbg` refleja las llamadas reales durante el workload. Rutas condicionales, pattern matching en funciones con múltiples clauses, y macros pueden hacer que el grafo dinámico sea muy diferente al análisis estático del código.

---

## Summary

El ecosistema de tracing de BEAM ofrece herramientas para cada nivel de riesgo:

- **Desarrollo/Staging**: `:sys.trace`, `:dbg` con match specs, `:sys.log` para captura pasiva
- **Producción**: `:recon_trace` exclusivamente — tiene límites built-in y semántica de seguridad

La regla de oro: **siempre activa un límite de calls, siempre limpia en un `after`**. El tracing sin límites en producción es más peligroso que el bug que intentas diagnosticar.

---

## What's Next

- **Ejercicio 28**: Benchmarking riguroso con Benchee — cuantificar performance con rigor estadístico
- Investiga `:ttb` (Trace Tool Builder) para tracing distribuido entre nodos
- Lee sobre `Logger` backends y cómo construir un tracer que escriba a archivo en lugar de stdout
- Explora `:observer_backend` para entender cómo Observer obtiene los datos que muestra

---

## Resources

- [Recon Trace documentation](https://ferd.github.io/recon/recon_trace.html)
- [Fred Hébert — "Tracing Erlang Code"](https://ferd.ca/erlang-otp-21-s-new-logger.html)
- [:sys module — Erlang docs](https://www.erlang.org/doc/man/sys.html)
- [:dbg module — Erlang docs](https://www.erlang.org/doc/man/dbg.html)
- [Matching Specs — Erlang docs](https://www.erlang.org/doc/apps/erts/match_spec.html)
