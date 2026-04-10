# 31 — Ports & External Processes

**Nivel**: Avanzado  
**Tema**: Comunicar con procesos externos via Ports

---

## Contexto

A veces necesitas integrar con código que no puede vivir en la BEAM: scripts Python,
binarios C, herramientas de sistema. Elixir ofrece varios mecanismos:

| Mecanismo | Cuándo usarlo |
|---|---|
| `System.cmd/3` | Proceso externo que corre y termina. Bloqueante. Simple. |
| `Port.open/2` | Comunicación bidireccional continua. El proceso vive mientras el Port vive. |
| NIFs | Código nativo que corre **dentro** de la VM (siguiente ejercicio) |
| C Nodes | Procesos Erlang completos escritos en C. Muy avanzado. |

Un **Port** es un proceso Erlang que actúa como puente entre la BEAM y un proceso OS.
Se comporta como cualquier proceso: te envías mensajes, lo monitoreás con `Port.monitor/1`.

### Ciclo de vida de un Port

```
Port.open(:spawn, "cmd")
       │
       ▼
  [Port process] ←──── os process ("cmd")
       │
       │  send(port, {self(), {:command, data}})
       ▼
  os process recibe data en stdin
       │
       │  proceso os escribe en stdout
       ▼
  Elixir recibe: {port, {:data, bytes}}
```

### Opciones de Port.open/2

```elixir
# Spawn por path de ejecutable (más seguro, sin shell)
port = Port.open({:spawn_executable, "/usr/bin/python3"}, [
  :binary,            # data como binaries en vez de charlists
  :exit_status,       # recibir {:exit_status, code} cuando el proceso termina
  args: ["script.py"],
  packet: 4           # prefijo de 4 bytes con la longitud del mensaje
])

# Spawn via shell (conveniente pero peligroso con input externo)
port = Port.open({:spawn, "cat"}, [:binary])

# line mode: recibir línea por línea
port = Port.open({:spawn, "tail -f /var/log/syslog"}, [:binary, line: 1024])
```

### Protocolo de comunicación

```elixir
# Enviar dato al proceso externo (lo recibe en stdin)
send(port, {self(), {:command, "hola\n"}})

# Recibir respuesta (pattern match en receive o handle_info)
receive do
  {^port, {:data, data}}       -> IO.puts("Recibido: #{data}")
  {^port, {:exit_status, 0}}   -> IO.puts("Proceso terminó OK")
  {^port, {:exit_status, code}} -> IO.puts("Error: #{code}")
  {:DOWN, _, :port, ^port, reason} -> IO.puts("Port cerrado: #{reason}")
end
```

### Cerrar un Port limpiamente

```elixir
# Opción 1: cerrar stdin del proceso externo
send(port, {self(), :close})

# Opción 2: matar el OS process directamente
Port.close(port)

# Opción 3: si el proceso Elixir muere, el Port muere automáticamente
# (el OS process también termina si leyó stdin y stdin se cierra)
```

### Port vs System.cmd — cuándo elegir cada uno

```elixir
# System.cmd: simple, bloqueante, result cuando termina
{output, 0} = System.cmd("python3", ["script.py", "--input", "data"])

# Port: bidireccional, streaming, no bloqueante
port = Port.open({:spawn_executable, path}, [:binary, :exit_status])
send(port, {self(), {:command, data}})
# ... sigue ejecutando Elixir mientras Python procesa
```

---

## Ejercicio 1 — Comunicación con Script Python via JSON

Escribe un GenServer `PythonWorker` que mantenga un proceso Python vivo y le
envíe tareas via JSON, recibiendo respuestas JSON. El script Python lee líneas
de stdin y escribe respuestas en stdout.

### Script Python que recibirás (no lo escribes tú, sólo el cliente Elixir)

```python
#!/usr/bin/env python3
import sys
import json

for line in sys.stdin:
    req = json.loads(line.strip())
    action = req.get("action")
    if action == "uppercase":
        result = {"result": req["value"].upper()}
    elif action == "reverse":
        result = {"result": req["value"][::-1]}
    elif action == "length":
        result = {"result": len(req["value"])}
    else:
        result = {"error": f"unknown action: {action}"}
    print(json.dumps(result), flush=True)
```

### Requisitos

- `start_link/1` acepta el path al script Python
- `call/3` — `call(pid, action, value)` — envía tarea, espera respuesta JSON
- Usa `Jason` para encode/decode (o `:json` en Elixir 1.18+)
- El GenServer mantiene el Port abierto entre llamadas (no spawn por request)
- Maneja el caso donde Python retorna `{"error": "..."}` — propagar como `{:error, reason}`
- Timeout de 5 segundos en `call` — si Python no responde, `{:error, :timeout}`

### Uso esperado

```elixir
{:ok, pid} = PythonWorker.start_link("/path/to/worker.py")

PythonWorker.call(pid, "uppercase", "hello")  #=> {:ok, "HELLO"}
PythonWorker.call(pid, "reverse", "elixir")   #=> {:ok, "rixile"}
PythonWorker.call(pid, "length", "beam")      #=> {:ok, 4}
PythonWorker.call(pid, "unknown", "x")        #=> {:error, "unknown action: unknown"}
```

### Hints

<details>
<summary>Hint 1 — Inicializar el Port y el estado</summary>

```elixir
defmodule PythonWorker do
  use GenServer

  defstruct [:port, :pending]
  # pending: map de ref → {from, timer_ref} para requests en vuelo

  def init(script_path) do
    python = System.find_executable("python3") || raise "python3 not found"
    port = Port.open({:spawn_executable, python}, [
      :binary,
      :exit_status,
      {:line, 4096},          # recibir línea a línea
      args: [script_path]
    ])
    {:ok, %__MODULE__{port: port, pending: %{}}}
  end
end
```

Con `{:line, N}` el port te entrega una línea completa en cada mensaje.
El formato recibido será `{port, {:data, {:eol, "json_line"}}}`.
</details>

<details>
<summary>Hint 2 — Enviar y recibir con correlación</summary>

El problema: `GenServer.call` es síncrono pero el Port es asíncrono.
Estrategia: usar `handle_call` que retorna `{:noreply, state}` y luego
responde con `GenServer.reply/2` cuando llega la respuesta del Port.

```elixir
def handle_call({:call, action, value}, from, state) do
  payload = Jason.encode!(%{action: action, value: value})
  send(state.port, {self(), {:command, payload <> "\n"}})

  # Guardar {from} para responder cuando llegue la data
  # Problema: no hay ID de correlación, así que asumimos un request a la vez
  {:noreply, %{state | pending: from}}
end

def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
  response = Jason.decode!(line)
  case response do
    %{"result" => v} -> GenServer.reply(state.pending, {:ok, v})
    %{"error" => e}  -> GenServer.reply(state.pending, {:error, e})
  end
  {:noreply, %{state | pending: nil}}
end
```

Para producción, necesitas correlación de IDs si quieres múltiples requests concurrentes.
</details>

<details>
<summary>Hint 3 — Limpiar el Port cuando el GenServer termina</summary>

```elixir
def terminate(_reason, state) do
  Port.close(state.port)
end
```

También considera `Port.monitor/1` en `init` para detectar si Python muere inesperadamente:

```elixir
Port.monitor(port)

# En handle_info:
def handle_info({:DOWN, _, :port, _port, reason}, state) do
  # Python murió — reiniciar, log, alertar
  {:stop, {:port_died, reason}, state}
end
```
</details>

---

## Ejercicio 2 — Streaming Port: Tail de Archivo

Implementa un módulo `FileTailer` que abra un proceso `tail -f` via Port y
notifique a uno o más subscribers Elixir cada vez que llegue una nueva línea.

### Requisitos

- `start_link/2` — `start_link(file_path, subscriber_pid)`
- Soporte para múltiples subscribers: `subscribe/2`, `unsubscribe/2`
- Cada línea nueva se envía como `{:new_line, line}` a todos los subscribers
- Cuando un subscriber muere, se limpia automáticamente de la lista (monitorear)
- `stop/1` — cierra el Port y termina el proceso limpiamente
- El Port usa `line: 4096` para recibir línea a línea

### Uso esperado

```elixir
{:ok, pid} = FileTailer.start_link("/var/log/app.log", self())

# En otro proceso:
FileTailer.subscribe(pid, other_pid)

# Cuando el archivo recibe nuevas líneas, ambos reciben:
receive do
  {:new_line, line} -> IO.puts("Nueva línea: #{line}")
end

FileTailer.stop(pid)
```

### Hints

<details>
<summary>Hint 1 — Abrir tail -f como Port</summary>

```elixir
def init({file_path, subscriber}) do
  tail = System.find_executable("tail") || raise "tail not found"
  port = Port.open({:spawn_executable, tail}, [
    :binary,
    {:line, 4096},
    args: ["-f", file_path]
  ])

  # Monitorear al subscriber inicial
  ref = Process.monitor(subscriber)
  state = %{
    port: port,
    subscribers: %{subscriber => ref}  # pid => monitor_ref
  }
  {:ok, state}
end
```
</details>

<details>
<summary>Hint 2 — Dispatch a subscribers y cleanup de muertos</summary>

```elixir
def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
  Enum.each(state.subscribers, fn {pid, _ref} ->
    send(pid, {:new_line, line})
  end)
  {:noreply, state}
end

def handle_info({:DOWN, ref, :process, pid, _reason}, state) do
  # Un subscriber murió — limpiar sin afectar a los demás
  new_subs = Map.reject(state.subscribers, fn {p, r} -> p == pid and r == ref end)
  {:noreply, %{state | subscribers: new_subs}}
end
```
</details>

<details>
<summary>Hint 3 — subscribe/unsubscribe y stop</summary>

```elixir
def handle_call({:subscribe, pid}, _from, state) do
  ref = Process.monitor(pid)
  {:reply, :ok, put_in(state.subscribers[pid], ref)}
end

def handle_call({:unsubscribe, pid}, _from, state) do
  case Map.pop(state.subscribers, pid) do
    {nil, _}  -> {:reply, {:error, :not_subscribed}, state}
    {ref, rest} ->
      Process.demonitor(ref, [:flush])
      {:reply, :ok, %{state | subscribers: rest}}
  end
end

def terminate(_reason, state) do
  Port.close(state.port)
end
```
</details>

---

## Ejercicio 3 — Port Crash Handling y Reinicio Limpio

Extiende `PythonWorker` del Ejercicio 1 para manejar el caso donde el proceso
externo muere inesperadamente. El GenServer debe:

1. Detectar la muerte del proceso externo
2. Fallar las requests pendientes con `{:error, :worker_crashed}`
3. Reintentar reconectar automáticamente (max 3 veces con backoff exponencial)
4. Si supera los reintentos, el GenServer termina con razón `{:exhausted_retries, n}`

### Requisitos

- `Port.monitor/1` para detectar la muerte del Port (no polling)
- Backoff exponencial: 100ms → 200ms → 400ms entre reintentos
- Durante el reinicio, nuevas llamadas reciben `{:error, :restarting}`
- Contador de reintentos se resetea cuando el worker lleva más de 30 segundos estable
- Usar `Process.send_after/3` para implementar el delay de reintento

### Uso esperado

```elixir
{:ok, pid} = PythonWorker.start_link("/path/to/worker.py")
PythonWorker.call(pid, "uppercase", "hello")  #=> {:ok, "HELLO"}

# Si el proceso Python muere:
# - PythonWorker detecta la muerte via Port monitor
# - Intenta reconectar después de 100ms
# - Logs: "Worker crashed, attempt 1/3, retrying in 100ms"
# - Reconecta y continúa sirviendo requests

# Si falla 3 veces:
# - GenServer termina con {:exhausted_retries, 3}
# - El Supervisor lo puede reiniciar
```

### Hints

<details>
<summary>Hint 1 — Estado extendido y monitor al arrancar</summary>

```elixir
defstruct [:port, :port_monitor, :script_path, :pending,
           :retries, :status]
# status: :running | :restarting

def init(script_path) do
  {:ok, state} = open_port(script_path, %__MODULE__{
    script_path: script_path,
    retries: 0,
    status: :running,
    pending: nil
  })
  {:ok, state}
end

defp open_port(script_path, state) do
  python = System.find_executable("python3")
  port = Port.open({:spawn_executable, python}, [
    :binary, :exit_status, {:line, 4096},
    args: [script_path]
  ])
  mon = Port.monitor(port)
  {:ok, %{state | port: port, port_monitor: mon, status: :running}}
end
```
</details>

<details>
<summary>Hint 2 — Detectar crash del Port</summary>

```elixir
# Port.monitor envía {:DOWN, ref, :port, port, reason} cuando el OS process muere
def handle_info({:DOWN, ref, :port, _port, reason}, %{port_monitor: ref} = state) do
  # Fallar request pendiente si la hay
  if state.pending do
    GenServer.reply(state.pending, {:error, :worker_crashed})
  end

  attempt_restart(%{state | pending: nil, status: :restarting})
end

defp attempt_restart(%{retries: n}) when n >= 3 do
  {:stop, {:exhausted_retries, 3}, %{}}
end
defp attempt_restart(%{retries: n} = state) do
  delay = trunc(100 * :math.pow(2, n))
  Process.send_after(self(), :do_restart, delay)
  {:noreply, %{state | retries: n + 1}}
end
```
</details>

<details>
<summary>Hint 3 — Reinicio y reset del contador</summary>

```elixir
def handle_info(:do_restart, state) do
  case open_port(state.script_path, state) do
    {:ok, new_state} ->
      # Resetear contador después de 30s estable
      Process.send_after(self(), :reset_retries, 30_000)
      {:noreply, new_state}
    {:error, reason} ->
      attempt_restart(state)  # cuenta como un reintento más
  end
end

def handle_info(:reset_retries, state) do
  {:noreply, %{state | retries: 0}}
end

# Rechazar calls durante el reinicio
def handle_call({:call, _, _}, _from, %{status: :restarting} = state) do
  {:reply, {:error, :restarting}, state}
end
```
</details>

---

## Trade-offs a considerar

### Port con `:line` vs `:packet N`

- **`:line`**: conveniente para protocolos texto (JSON lines, CSV). El OS process
  debe hacer `flush` en cada línea. Riesgo: líneas más largas que `N` se truncan.
- **`:packet N`**: el puerto agrega un header de N bytes con la longitud del mensaje.
  Permite mensajes binarios arbitrarios. Debes implementar el mismo protocolo en el
  proceso externo.

### Port vs NIF para performance

Los Ports agregan overhead de IPC (pipes del OS). Para llamadas frecuentes a código
nativo de alto rendimiento (< 1ms), ese overhead puede ser inaceptable. Los NIFs
(siguiente ejercicio) eliminan ese overhead pero tienen riesgos distintos.

### Muerte del OS process y clean up

Cuando un Port se cierra en Elixir, el proceso OS recibe `EOF` en stdin.
Si el proceso externo no lo maneja (no lee stdin o no monitorea su stdin),
puede quedar zombie. Diseña tus scripts externos para terminar limpiamente
cuando detecten `EOF`.

```python
# Python: terminar al detectar EOF en stdin
import sys
for line in sys.stdin:   # itera hasta EOF automáticamente
    process(line)
# Al salir del for, el proceso termina limpiamente
```

---

## One possible solution

<details>
<summary>Ver solución (spoiler)</summary>

```elixir
# Ejercicio 1: PythonWorker básico
defmodule PythonWorker do
  use GenServer

  defstruct [:port, :pending, :script_path]

  def start_link(script_path),      do: GenServer.start_link(__MODULE__, script_path)
  def call(pid, action, value, timeout \\ 5_000),
    do: GenServer.call(pid, {:call, action, value}, timeout)

  @impl true
  def init(script_path) do
    python = System.find_executable("python3") || raise "python3 not found"
    port = Port.open({:spawn_executable, python}, [
      :binary, :exit_status, {:line, 4096},
      args: [script_path]
    ])
    {:ok, %__MODULE__{port: port, script_path: script_path, pending: nil}}
  end

  @impl true
  def handle_call({:call, action, value}, from, state) do
    payload = Jason.encode!(%{action: action, value: value})
    send(state.port, {self(), {:command, payload <> "\n"}})
    {:noreply, %{state | pending: from}}
  end

  @impl true
  def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
    case Jason.decode!(line) do
      %{"result" => v} -> GenServer.reply(state.pending, {:ok, v})
      %{"error" => e}  -> GenServer.reply(state.pending, {:error, e})
    end
    {:noreply, %{state | pending: nil}}
  end

  def handle_info({port, {:exit_status, _}}, %{port: port} = state) do
    if state.pending, do: GenServer.reply(state.pending, {:error, :worker_crashed})
    {:stop, :port_exited, %{state | pending: nil}}
  end

  @impl true
  def terminate(_reason, state), do: Port.close(state.port)
end

# Ejercicio 2: FileTailer
defmodule FileTailer do
  use GenServer

  def start_link(path, sub),    do: GenServer.start_link(__MODULE__, {path, sub})
  def subscribe(pid, sub),      do: GenServer.call(pid, {:subscribe, sub})
  def unsubscribe(pid, sub),    do: GenServer.call(pid, {:unsubscribe, sub})
  def stop(pid),                do: GenServer.stop(pid)

  @impl true
  def init({path, sub}) do
    tail = System.find_executable("tail") || raise "tail not found"
    port = Port.open({:spawn_executable, tail}, [:binary, {:line, 4096}, args: ["-f", path]])
    ref  = Process.monitor(sub)
    {:ok, %{port: port, subscribers: %{sub => ref}}}
  end

  @impl true
  def handle_call({:subscribe, pid}, _from, state) do
    ref = Process.monitor(pid)
    {:reply, :ok, put_in(state.subscribers[pid], ref)}
  end
  def handle_call({:unsubscribe, pid}, _from, state) do
    {ref, rest} = Map.pop(state.subscribers, pid, nil)
    if ref, do: Process.demonitor(ref, [:flush])
    {:reply, :ok, %{state | subscribers: rest}}
  end

  @impl true
  def handle_info({port, {:data, {:eol, line}}}, %{port: port} = state) do
    Enum.each(state.subscribers, fn {pid, _} -> send(pid, {:new_line, line}) end)
    {:noreply, state}
  end
  def handle_info({:DOWN, ref, :process, pid, _}, state) do
    subs = Map.reject(state.subscribers, fn {p, r} -> p == pid and r == ref end)
    {:noreply, %{state | subscribers: subs}}
  end

  @impl true
  def terminate(_reason, %{port: port}), do: Port.close(port)
end
```

</details>
