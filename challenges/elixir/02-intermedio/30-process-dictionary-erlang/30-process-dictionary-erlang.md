# 30 - Process Dictionary (Erlang)

## Prerequisites

- Procesos Elixir: `spawn`, `send`, `receive`
- Agent básico (ejercicio 02)
- ETS básico (ejercicio 13)
- Comprensión del modelo de actores y aislamiento de procesos

---

## Learning Objectives

Al completar este ejercicio serás capaz de:

1. Usar `Process.put/2`, `Process.get/1`, `Process.delete/1` para estado por proceso
2. Entender la relación entre las funciones Elixir y las Erlang subyacentes (`:erlang.put/get`)
3. Identificar los casos legítimos (escasos) donde el process dictionary tiene sentido
4. Conocer sus limitaciones: invisibilidad, acoplamiento implícito, dificultad para testear
5. Refactorizar código que usa process dictionary hacia `Agent` o `ETS`
6. Inspeccionar el dictionary con `Process.info(self(), :dictionary)`

---

## Concepts

### Qué es el Process Dictionary

Cada proceso Erlang/Elixir tiene un diccionario privado de pares `{key, value}` que vive durante toda la vida del proceso. Es un mecanismo de estado mutable local, invisible desde fuera del proceso.

```elixir
# Almacenar un valor
Process.put(:contador, 0)

# Leer un valor (nil si no existe)
Process.get(:contador)     # 0

# Leer con valor por defecto
Process.get(:otro, :no_existe)  # :no_existe

# Eliminar una clave
Process.delete(:contador)  # devuelve el valor anterior: 0

# Leer todo el diccionario
Process.get()  # [{:contador, 0}, ...]
```

### La capa Erlang subyacente

Las funciones de Elixir son wrappers de las BIFs (Built-In Functions) de Erlang:

```elixir
# Equivalentes exactos
Process.put(:key, :value)   ==  :erlang.put(:key, :value)
Process.get(:key)           ==  :erlang.get(:key)
Process.delete(:key)        ==  :erlang.erase(:key)
Process.get()               ==  :erlang.get()

# :erlang.erase/0 borra TODO el diccionario
:erlang.erase()
```

### Inspección desde fuera

```elixir
# Ver el dictionary de OTRO proceso (observación, no modificación)
pid = spawn(fn ->
  Process.put(:secreto, 42)
  Process.sleep(5000)
end)

Process.info(pid, :dictionary)
# {:dictionary, [{:"$initial_call", {Kernel, :exec_fun, 3}}, {:secreto, 42}]}

# Nota: las claves que empiezan con $ son internas de OTP
```

### Cuándo usar el Process Dictionary (raramente)

Los casos donde es aceptable son muy específicos:

```elixir
# CASO 1: Acumular traza/log dentro de una operación compleja
# (patrón usado por Logger internamente)
defmodule Tracer do
  def start_trace(id) do
    Process.put(:trace_id, id)
    Process.put(:trace_events, [])
  end

  def record(event) do
    events = Process.get(:trace_events, [])
    Process.put(:trace_events, [event | events])
  end

  def finish_trace do
    id     = Process.get(:trace_id)
    events = Process.get(:trace_events, []) |> Enum.reverse()
    Process.delete(:trace_id)
    Process.delete(:trace_events)
    {id, events}
  end
end

# CASO 2: Cache de recursos costosos que se reusan en el mismo proceso
# (por ejemplo, conexiones de base de datos en pool workers)
defmodule ResourceCache do
  def get_or_create(key, create_fn) do
    case Process.get({:cache, key}) do
      nil ->
        resource = create_fn.()
        Process.put({:cache, key}, resource)
        resource
      resource ->
        resource
    end
  end
end
```

### Por qué preferir Agent o ETS

```elixir
# --- CON PROCESS DICTIONARY (problemático) ---
defmodule CounterBad do
  def increment do
    current = Process.get(:counter, 0)
    Process.put(:counter, current + 1)
  end

  def value, do: Process.get(:counter, 0)
end

# Problemas:
# 1. Solo funciona en el mismo proceso que lo inicializó
# 2. No hay interface clara: cualquier código puede leer/escribir :counter
# 3. Imposible de testear en aislamiento
# 4. Si el proceso muere, el estado desaparece silenciosamente

# --- CON AGENT (correcto) ---
defmodule CounterGood do
  def start_link(initial \\ 0) do
    Agent.start_link(fn -> initial end, name: __MODULE__)
  end

  def increment do
    Agent.update(__MODULE__, &(&1 + 1))
  end

  def value do
    Agent.get(__MODULE__, & &1)
  end
end
# Ventajas: interfaz explícita, compartible entre procesos, testeable, supervisable
```

---

## Exercises

### Ejercicio 1: Logger de solicitudes HTTP por proceso

Simula un middleware que acumula logs de una solicitud HTTP en el process dictionary durante el ciclo de vida de la request.

```elixir
defmodule RequestLogger do
  @doc """
  Inicializa el logger para una nueva solicitud.
  Guarda request_id y timestamp de inicio en el process dictionary.
  """
  def start_request(request_id) do
    # TODO: guardar en el process dictionary:
    # - {:request_id, request_id}
    # - {:request_start, :os.system_time(:millisecond)}
    # - {:request_logs, []}
  end

  @doc """
  Agrega una entrada de log a la solicitud actual.
  """
  def log(level, message) when level in [:info, :warn, :error] do
    # TODO: obtener logs actuales, agregar {level, message, timestamp},
    # guardar de vuelta en {:request_logs, ...}
  end

  @doc """
  Finaliza la solicitud. Devuelve un mapa con el resumen.
  Limpia el process dictionary.
  """
  def finish_request do
    # TODO: leer request_id, calcular duración (now - start),
    # recopilar logs, limpiar las claves del dictionary,
    # devolver %{request_id: ..., duration_ms: ..., logs: [...]}
  end

  @doc """
  Obtiene el request_id actual (útil para propagación a llamadas anidadas).
  """
  def current_request_id do
    # TODO: Process.get(:request_id)
  end
end
```

```elixir
# Uso esperado:
# pid = spawn(fn ->
#   RequestLogger.start_request("req-001")
#   RequestLogger.log(:info, "Procesando usuario")
#   Process.sleep(50)
#   RequestLogger.log(:warn, "Cache miss")
#   result = RequestLogger.finish_request()
#   IO.inspect(result)
#   # %{request_id: "req-001", duration_ms: ~50, logs: [
#   #   {:info, "Procesando usuario", ...},
#   #   {:warn, "Cache miss", ...}
#   # ]}
# end)
# Process.sleep(200)
#
# Demostrar que OTRO proceso no ve los logs:
# spawn(fn -> IO.inspect(RequestLogger.current_request_id()) end)
# # nil — el dictionary es privado por proceso
```

---

### Ejercicio 2: Counter acumulador en proceso

Implementa un módulo que usa process dictionary para acumular métricas durante una operación batch, luego refactorízalo hacia Agent.

```elixir
defmodule BatchMetrics do
  @doc """
  Versión 1: usando process dictionary.

  Procesa una lista de items, incrementando contadores por resultado.
  Al finalizar, devuelve las métricas y limpia el diccionario.
  """
  def run_with_dict(items, process_fn) do
    # TODO: inicializar contadores en process dictionary
    # {:processed, 0}, {:errors, 0}, {:skipped, 0}

    Enum.each(items, fn item ->
      case process_fn.(item) do
        :ok      -> # TODO: incrementar :processed
        :error   -> # TODO: incrementar :errors
        :skip    -> # TODO: incrementar :skipped
      end
    end)

    # TODO: leer métricas, limpiarlas, devolver mapa
    # %{processed: N, errors: N, skipped: N}
  end

  @doc """
  Versión 2: refactorizada hacia Agent.

  Misma interfaz, pero el estado vive en un Agent supervisable.
  """
  def run_with_agent(items, process_fn) do
    {:ok, agent} = Agent.start_link(fn ->
      # TODO: estado inicial del agent
      %{processed: 0, errors: 0, skipped: 0}
    end)

    Enum.each(items, fn item ->
      case process_fn.(item) do
        :ok    ->
          # TODO: Agent.update/2 para incrementar :processed
        :error ->
          # TODO: Agent.update/2 para incrementar :errors
        :skip  ->
          # TODO: Agent.update/2 para incrementar :skipped
      end
    end)

    metrics = Agent.get(agent, & &1)
    Agent.stop(agent)
    metrics
  end
end
```

```elixir
# Verificación: ambas versiones deben dar el mismo resultado
# process_fn = fn
#   n when rem(n, 3) == 0 -> :skip
#   n when rem(n, 7) == 0 -> :error
#   _                     -> :ok
# end
#
# items = Enum.to_list(1..100)
#
# result_dict  = BatchMetrics.run_with_dict(items, process_fn)
# result_agent = BatchMetrics.run_with_agent(items, process_fn)
#
# result_dict == result_agent  # true
# IO.inspect(result_dict)
# # %{processed: 76, errors: 10, skipped: 34}  (valores aproximados)
```

---

### Ejercicio 3: Refactor de process dict a Agent

Dado el siguiente módulo que usa process dictionary de forma inapropiada (estado global implícito), refactorízalo hacia un Agent con interfaz explícita.

```elixir
# CÓDIGO ORIGINAL — no modificar, solo leer para entender
defmodule SessionStoreLegacy do
  # Este módulo asume que siempre se llama desde el mismo proceso.
  # Usa el process dictionary como "base de datos" de sesiones.
  # PROBLEMA: si se llama desde procesos distintos, cada uno tiene su propio dict.

  def put_session(user_id, data) do
    sessions = :erlang.get(:sessions) || %{}
    :erlang.put(:sessions, Map.put(sessions, user_id, data))
  end

  def get_session(user_id) do
    sessions = :erlang.get(:sessions) || %{}
    Map.get(sessions, user_id)
  end

  def delete_session(user_id) do
    sessions = :erlang.get(:sessions) || %{}
    :erlang.put(:sessions, Map.delete(sessions, user_id))
  end

  def all_sessions do
    :erlang.get(:sessions) || %{}
  end
end
```

```elixir
# TU TAREA: implementar SessionStore usando Agent
defmodule SessionStore do
  @moduledoc """
  Store de sesiones basado en Agent.

  A diferencia del enfoque con process dictionary, este módulo:
  - Es accesible desde cualquier proceso
  - Tiene estado supervisable
  - Tiene interfaz explícita y testeable
  - Puede ser nombrado globalmente
  """

  # TODO: start_link/1 — inicia el Agent con estado inicial %{}
  # Acepta opts para pasar nombre (name: __MODULE__)
  def start_link(opts \\ []) do
  end

  # TODO: put/3 — guarda {user_id => data} en el Agent
  def put(server \\ __MODULE__, user_id, data) do
  end

  # TODO: get/2 — devuelve los datos del user_id, nil si no existe
  def get(server \\ __MODULE__, user_id) do
  end

  # TODO: delete/2 — elimina la sesión del user_id
  def delete(server \\ __MODULE__, user_id) do
  end

  # TODO: all/1 — devuelve el mapa completo de sesiones
  def all(server \\ __MODULE__) do
  end

  @doc """
  Demuestra la diferencia entre el enfoque legacy y el correcto.
  """
  def demo_isolation do
    # Con SessionStoreLegacy, esto falla:
    # Proceso 1 guarda sesión → Proceso 2 no la ve
    #
    # Con SessionStore (Agent), ambos procesos comparten el mismo estado.
    {:ok, _} = start_link(name: :demo_store)

    pid1 = spawn(fn ->
      SessionStore.put(:demo_store, "user-1", %{name: "Ana"})
      IO.puts("Proceso 1: sesión guardada")
    end)

    pid2 = spawn(fn ->
      Process.sleep(50)  # esperar a que pid1 termine
      session = SessionStore.get(:demo_store, "user-1")
      IO.inspect(session, label: "Proceso 2 ve la sesión")
    end)

    Process.sleep(200)
    :ok
  end
end
```

```elixir
# Verificación manual en iex:
# {:ok, _} = SessionStore.start_link(name: :session_store)
#
# SessionStore.put(:session_store, "u1", %{name: "Ana", role: :admin})
# SessionStore.put(:session_store, "u2", %{name: "Bob", role: :user})
#
# SessionStore.get(:session_store, "u1")   # %{name: "Ana", role: :admin}
# SessionStore.get(:session_store, "u99")  # nil
# SessionStore.all(:session_store)         # %{"u1" => ..., "u2" => ...}
# SessionStore.delete(:session_store, "u1")
# SessionStore.all(:session_store)         # %{"u2" => ...}
#
# # Demostrar que el Process.info no puede espiar el Agent fácilmente:
# Process.info(Process.whereis(:session_store), :dictionary)
# # {:dictionary, [{"$initial_call", ...}]}  — el estado está en el Agent, no en el dict!
```

---

## Common Mistakes

**1. Usar process dictionary como estado compartido entre procesos**

```elixir
# MAL: el proceso padre guarda, el proceso hijo no puede leer
Process.put(:shared, 42)
spawn(fn ->
  IO.inspect(Process.get(:shared))  # nil — el hijo tiene su propio dict vacío
end)

# BIEN: usa Agent, ETS, o paso de mensajes explícito
```

**2. No limpiar después de usarlo**

```elixir
# MAL: el valor persiste hasta que el proceso muere
# Si el proceso es un worker reusado (pool), contamina la siguiente request
Process.put(:request_id, "req-001")
# ... olvidar Process.delete(:request_id)

# BIEN: siempre limpiar al final de la operación
try do
  Process.put(:request_id, id)
  do_work()
after
  Process.delete(:request_id)
end
```

**3. Usar átomos dinámicos como claves**

```elixir
# MAL: los átomos son globales y no se garbage-collectan
user_id = get_user_id()
Process.put(String.to_atom("user_#{user_id}"), data)  # atom leak!

# BIEN: usar tuplas con la parte dinámica como string/integer
Process.put({:user_data, user_id}, data)
```

**4. Confiar en el process dictionary para lógica de negocio**

El process dictionary es un mecanismo de runtime de Erlang, no un componente de arquitectura. Si tu lógica de negocio depende de él, es una señal de diseño incorrecto.

---

## Verification

```elixir
# Test rápido en iex:

# 1. Básicos
Process.put(:test, "hola")
Process.get(:test)          # "hola"
Process.get(:inexistente, :default)  # :default
Process.delete(:test)       # "hola" (devuelve el valor anterior)
Process.get(:test)          # nil

# 2. Aislamiento entre procesos
Process.put(:aislado, 99)
spawn(fn ->
  IO.inspect(Process.get(:aislado), label: "hijo")  # nil
end)
Process.sleep(100)
IO.inspect(Process.get(:aislado), label: "padre")   # 99

# 3. Inspección externa
pid = spawn(fn ->
  Process.put(:mi_dato, :secreto)
  Process.sleep(1000)
end)
Process.sleep(50)
Process.info(pid, :dictionary)
# {:dictionary, [mi_dato: :secreto, ...]}

# 4. Agent como alternativa
{:ok, agent} = Agent.start_link(fn -> %{} end)
Agent.update(agent, &Map.put(&1, :key, :value))
Agent.get(agent, & &1)   # %{key: :value}
Agent.stop(agent)
```

---

## Summary

El process dictionary es un mecanismo de bajo nivel heredado de Erlang. Funciona como un mapa privado mutable por proceso, invisible desde el exterior.

| Característica | Process Dictionary | Agent | ETS |
|---|---|---|---|
| Visibilidad | Solo proceso propietario | Cualquier proceso (via PID/name) | Cualquier proceso |
| Supervisión | No | Sí (OTP) | No directamente |
| Persistencia | Hasta muerte del proceso | Hasta muerte del proceso | Configurable |
| Caso de uso | Tracing interno, cache local | Estado compartido simple | Estado masivo / concurrente |

**Regla práctica**: si dudas entre process dictionary y Agent, elige Agent.

---

## What's Next

- **31**: Concurrencia Fan-Out/Fan-In — `Task.async_stream` para paralelismo
- **ETS**: tabla de hash compartida entre procesos (ejercicio 13)
- **GenServer**: state machine con `handle_call`/`handle_cast`
- **:persistent_term`**: para datos inmutables globales de alta performance

---

## Resources

- [Process module docs](https://hexdocs.pm/elixir/Process.html)
- [Erlang process dictionary](https://www.erlang.org/doc/man/erlang.html#get-0)
- [Process.info/2](https://hexdocs.pm/elixir/Process.html#info/2)
- [Agent docs](https://hexdocs.pm/elixir/Agent.html)
- [When to use the process dictionary (Erlang forums)](https://erlangforums.com)
