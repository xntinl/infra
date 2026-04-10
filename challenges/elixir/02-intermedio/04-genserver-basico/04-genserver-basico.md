# 4. GenServer: Procesos con Estado y API Clara

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 01-procesos-spawn-send-receive
- Completed 02-agent-basico
- Understanding of modules, behaviours, and pattern matching

## Learning Objectives
After completing this exercise, you will be able to:
- Implement a GenServer with `use GenServer` and define callbacks
- Initialize state with `init/1`
- Handle synchronous requests with `handle_call/3` (caller blocks and waits reply)
- Handle asynchronous casts with `handle_cast/2` (caller doesn't block)
- Handle raw messages with `handle_info/2` (timers, monitor signals)
- Clean up resources with `terminate/2`
- Register a GenServer with a name for global access

## Concepts

### GenServer: el bloque básico de OTP
GenServer (Generic Server) es el behaviour más fundamental de OTP. Encapsula el patrón de un proceso que mantiene estado y responde a mensajes, pero de forma estructurada y con semántica clara. Todo lo que puedes hacer con Agent o con procesos manuales, puedes hacerlo con GenServer — pero con mucho más control.

La arquitectura de GenServer divide la responsabilidad en dos capas: la API pública (funciones que llaman tus clientes) y los callbacks (funciones que el proceso ejecuta internamente). Esta separación es fundamental — los callbacks siempre corren en el proceso del GenServer, los clientes llaman a las funciones API desde sus propios procesos.

```elixir
defmodule MiServidor do
  use GenServer

  # --- API pública (corre en el proceso del CLIENTE) ---
  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  def obtener, do: GenServer.call(__MODULE__, :get)
  def poner(val), do: GenServer.cast(__MODULE__, {:put, val})

  # --- Callbacks (corren en el proceso del SERVIDOR) ---
  @impl GenServer
  def init(_opts), do: {:ok, %{}}

  @impl GenServer
  def handle_call(:get, _from, state), do: {:reply, state, state}

  @impl GenServer
  def handle_cast({:put, val}, state), do: {:noreply, Map.put(state, :val, val)}
end
```

### call vs cast: síncrono vs asíncrono
`GenServer.call/2` es síncrono: el proceso que lo llama se bloquea hasta que el GenServer retorna una respuesta. Úsalo cuando necesitas el resultado. `handle_call/3` recibe `(request, from, state)` y debe retornar `{:reply, respuesta, nuevo_estado}`.

`GenServer.cast/2` es asíncrono: el proceso que lo llama continúa inmediatamente sin esperar. Úsalo cuando no necesitas confirmación ni resultado. `handle_cast/2` recibe `(request, state)` y debe retornar `{:noreply, nuevo_estado}`.

```elixir
# call — el cliente espera la respuesta
resultado = GenServer.call(servidor, :calcular_algo)

# cast — el cliente no espera, no sabe si llegó
:ok = GenServer.cast(servidor, {:log, "mensaje"})
```

### handle_info: mensajes del sistema
`handle_info/2` recibe cualquier mensaje que llegue al proceso del GenServer que NO vino de `call` o `cast`. Esto incluye mensajes de timers (`:timer.send_interval`), señales de procesos monitoreados, y cualquier `send` directo al PID del GenServer.

```elixir
@impl GenServer
def handle_info(:tick, state) do
  IO.puts("Timer tick!")
  {:noreply, state}
end

@impl GenServer
def handle_info({:DOWN, ref, :process, pid, reason}, state) do
  # Un proceso que monitoreábamos murió
  {:noreply, actualizar_estado(state, pid, reason)}
end
```

### terminate/2: limpieza al cerrar
`terminate/2` se llama cuando el GenServer va a detenerse (por error, por `GenServer.stop`, o por shutdown del supervisor). Úsalo para cerrar conexiones, liberar recursos, o hacer flush de buffers. Solo se llama si el GenServer está en un árbol de supervisión.

## Exercises

### Exercise 1: Counter GenServer
```elixir
defmodule ContadorGenServer do
  use GenServer

  # --- API pública ---

  # TODO: Implementa `start_link/1`:
  # Llama GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  # El estado inicial es 0
  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, 0, name: __MODULE__)
  end

  # TODO: Implementa `get/0` usando GenServer.call para obtener el valor actual
  def get do
    GenServer.call(__MODULE__, :get)
  end

  # TODO: Implementa `incrementar/0` usando GenServer.cast (no necesita respuesta)
  def incrementar do
    GenServer.cast(__MODULE__, :incrementar)
  end

  # TODO: Implementa `decrementar/0` usando GenServer.cast
  def decrementar do
    GenServer.cast(__MODULE__, :decrementar)
  end

  # TODO: Implementa `reset/0` usando GenServer.cast
  def reset do
    GenServer.cast(__MODULE__, :reset)
  end

  def stop, do: GenServer.stop(__MODULE__)

  # --- Callbacks ---

  @impl GenServer
  def init(estado_inicial) do
    # TODO: retornar {:ok, estado_inicial}
  end

  @impl GenServer
  # TODO: handle_call para :get — retornar {:reply, estado, estado}
  def handle_call(:get, _from, estado) do
  end

  @impl GenServer
  # TODO: handle_cast para :incrementar — retornar {:noreply, estado + 1}
  def handle_cast(:incrementar, estado) do
  end

  @impl GenServer
  # TODO: handle_cast para :decrementar
  def handle_cast(:decrementar, estado) do
  end

  @impl GenServer
  # TODO: handle_cast para :reset — retornar {:noreply, 0}
  def handle_cast(:reset, _estado) do
  end
end

# Test it:
# ContadorGenServer.start_link()
# ContadorGenServer.get()           # => 0
# ContadorGenServer.incrementar()
# ContadorGenServer.incrementar()
# ContadorGenServer.incrementar()
# ContadorGenServer.get()           # => 3
# ContadorGenServer.decrementar()
# ContadorGenServer.get()           # => 2
# ContadorGenServer.reset()
# ContadorGenServer.get()           # => 0
# ContadorGenServer.stop()
```

### Exercise 2: Key-Value Store GenServer
```elixir
defmodule KVStore do
  use GenServer

  # --- API pública ---

  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  end

  # TODO: Implementa `get/1` — call síncrono que retorna el valor o nil
  def get(clave) do
    GenServer.call(__MODULE__, {:get, clave})
  end

  # TODO: Implementa `get/2` — con valor por defecto si la clave no existe
  def get(clave, default) do
    GenServer.call(__MODULE__, {:get, clave, default})
  end

  # TODO: Implementa `put/2` — cast asíncrono para guardar clave-valor
  def put(clave, valor) do
    GenServer.cast(__MODULE__, {:put, clave, valor})
  end

  # TODO: Implementa `delete/1` — cast asíncrono para eliminar clave
  def delete(clave) do
    GenServer.cast(__MODULE__, {:delete, clave})
  end

  # TODO: Implementa `keys/0` — call síncrono que retorna lista de claves
  def keys do
    GenServer.call(__MODULE__, :keys)
  end

  # TODO: Implementa `all/0` — call síncrono que retorna todo el mapa
  def all do
    GenServer.call(__MODULE__, :all)
  end

  def stop, do: GenServer.stop(__MODULE__)

  # --- Callbacks ---

  @impl GenServer
  def init(estado), do: {:ok, estado}

  @impl GenServer
  def handle_call({:get, clave}, _from, estado) do
    valor = Map.get(estado, clave)
    # TODO: retornar {:reply, valor, estado}
  end

  @impl GenServer
  def handle_call({:get, clave, default}, _from, estado) do
    valor = Map.get(estado, clave, default)
    # TODO: retornar {:reply, valor, estado}
  end

  @impl GenServer
  # TODO: handle_call para :keys — retornar Map.keys(estado)
  def handle_call(:keys, _from, estado) do
  end

  @impl GenServer
  # TODO: handle_call para :all — retornar el mapa completo
  def handle_call(:all, _from, estado) do
  end

  @impl GenServer
  def handle_cast({:put, clave, valor}, estado) do
    nuevo_estado = Map.put(estado, clave, valor)
    # TODO: retornar {:noreply, nuevo_estado}
  end

  @impl GenServer
  def handle_cast({:delete, clave}, estado) do
    nuevo_estado = Map.delete(estado, clave)
    # TODO: retornar {:noreply, nuevo_estado}
  end
end

# Test it:
# KVStore.start_link()
# KVStore.put(:nombre, "Elixir")
# KVStore.put(:version, 1.17)
# KVStore.get(:nombre)              # => "Elixir"
# KVStore.get(:faltante, "N/A")     # => "N/A"
# KVStore.keys()                    # => [:nombre, :version]
# KVStore.delete(:version)
# KVStore.all()                     # => %{nombre: "Elixir"}
# KVStore.stop()
```

### Exercise 3: handle_info con timer periódico
```elixir
defmodule TareaPeriodicaGS do
  use GenServer

  # Estado: %{contador: n, intervalo_ms: ms, timer_ref: ref | nil}

  def start_link(intervalo_ms \\ 1000) do
    GenServer.start_link(__MODULE__, intervalo_ms, name: __MODULE__)
  end

  def get_contador, do: GenServer.call(__MODULE__, :get_contador)
  def detener_timer, do: GenServer.cast(__MODULE__, :detener_timer)
  def stop, do: GenServer.stop(__MODULE__)

  @impl GenServer
  def init(intervalo_ms) do
    # TODO: Usar :timer.send_interval(intervalo_ms, self(), :tick) para enviar
    # un mensaje :tick a este proceso cada `intervalo_ms` milisegundos
    # :timer.send_interval retorna {:ok, timer_ref}
    {:ok, timer_ref} = :timer.send_interval(intervalo_ms, self(), :tick)

    estado_inicial = %{
      contador: 0,
      intervalo_ms: intervalo_ms,
      timer_ref: timer_ref
    }
    # TODO: retornar {:ok, estado_inicial}
  end

  @impl GenServer
  def handle_call(:get_contador, _from, estado) do
    # TODO: retornar {:reply, estado.contador, estado}
  end

  @impl GenServer
  def handle_cast(:detener_timer, estado) do
    # :timer.cancel cancela el timer periódico
    :timer.cancel(estado.timer_ref)
    # TODO: retornar {:noreply, %{estado | timer_ref: nil}}
  end

  @impl GenServer
  # TODO: handle_info para el mensaje :tick:
  # Incrementa el contador e imprime "Tick #N"
  # Retorna {:noreply, nuevo_estado}
  def handle_info(:tick, estado) do
    nuevo_contador = estado.contador + 1
    IO.puts("Tick ##{nuevo_contador}")
    # TODO: retornar {:noreply, %{estado | contador: nuevo_contador}}
  end

  @impl GenServer
  # Manejar mensajes no reconocidos sin crashear
  def handle_info(msg, estado) do
    IO.puts("Mensaje no esperado: #{inspect(msg)}")
    {:noreply, estado}
  end
end

# Test it (en IEx):
# TareaPeriodicaGS.start_link(500)   # tick cada 500ms
# :timer.sleep(2500)
# TareaPeriodicaGS.get_contador()    # => 5
# TareaPeriodicaGS.detener_timer()
# :timer.sleep(1000)
# TareaPeriodicaGS.get_contador()    # => 5 (no siguió contando)
# TareaPeriodicaGS.stop()
```

### Exercise 4: terminate/2 para cleanup
```elixir
defmodule ConexionSimulada do
  use GenServer
  require Logger

  # Simula un GenServer que mantiene una "conexión" a un recurso externo

  def start_link(config) do
    GenServer.start_link(__MODULE__, config, name: __MODULE__)
  end

  def ejecutar_query(sql), do: GenServer.call(__MODULE__, {:query, sql})
  def stop, do: GenServer.stop(__MODULE__)

  @impl GenServer
  def init(config) do
    Logger.info("Abriendo conexión a #{config[:host]}:#{config[:port]}")
    # Simula abrir una conexión
    conexion = %{
      host: config[:host],
      port: config[:port],
      activa: true,
      queries_ejecutadas: 0
    }
    # TODO: retornar {:ok, conexion}
  end

  @impl GenServer
  def handle_call({:query, sql}, _from, conexion) do
    Logger.debug("Ejecutando: #{sql}")
    # Simula ejecutar una query
    resultado = %{filas: [], sql: sql, tiempo_ms: Enum.random(5..50)}
    nuevo_estado = %{conexion | queries_ejecutadas: conexion.queries_ejecutadas + 1}
    # TODO: retornar {:reply, {:ok, resultado}, nuevo_estado}
  end

  @impl GenServer
  # TODO: Implementa terminate/2:
  # Se llama cuando el GenServer va a detenerse
  # reason puede ser: :normal, :shutdown, {:shutdown, term}, o cualquier otro término
  # Imprime "Cerrando conexión (reason: #{inspect(reason)}). Queries ejecutadas: #{n}"
  # Retorna :ok (el valor de retorno de terminate es ignorado)
  def terminate(reason, estado) do
    Logger.info(
      "Cerrando conexión (reason: #{inspect(reason)}). " <>
      "Queries ejecutadas: #{estado.queries_ejecutadas}"
    )
    # Simula cerrar la conexión limpiamente
    # TODO: retornar :ok
  end
end

# Test it:
# ConexionSimulada.start_link(host: "db.ejemplo.com", port: 5432)
# ConexionSimulada.ejecutar_query("SELECT * FROM users")
# ConexionSimulada.ejecutar_query("SELECT count(*) FROM orders")
# ConexionSimulada.stop()
# (debería imprimir el mensaje de terminate con queries_ejecutadas: 2)
```

### Exercise 5: GenServer con nombre y acceso global
```elixir
defmodule RegistroGlobal do
  use GenServer

  # Un GenServer registrado con un nombre para ser accesible desde cualquier módulo
  # sin necesidad de pasar el PID explícitamente

  # TODO: Implementa `start_link/1` registrando el proceso con `name: __MODULE__`
  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  end

  # TODO: Implementa `registrar/2` que guarda {nombre, pid_actual} en el registro
  # Usa GenServer.call para retornar :ok o {:error, :ya_registrado} si ya existe
  def registrar(nombre, pid \\ self()) do
    GenServer.call(__MODULE__, {:registrar, nombre, pid})
  end

  # TODO: Implementa `buscar/1` que retorna {:ok, pid} o {:error, :no_encontrado}
  def buscar(nombre) do
    GenServer.call(__MODULE__, {:buscar, nombre})
  end

  # TODO: Implementa `eliminar/1` que elimina un nombre del registro
  def eliminar(nombre) do
    GenServer.cast(__MODULE__, {:eliminar, nombre})
  end

  # TODO: Implementa `listar/0` que retorna todos los nombres registrados
  def listar do
    GenServer.call(__MODULE__, :listar)
  end

  def stop, do: GenServer.stop(__MODULE__)

  # --- Callbacks ---

  @impl GenServer
  def init(estado), do: {:ok, estado}

  @impl GenServer
  def handle_call({:registrar, nombre, pid}, _from, estado) do
    case Map.has_key?(estado, nombre) do
      true ->
        # TODO: retornar {:reply, {:error, :ya_registrado}, estado}
      false ->
        nuevo_estado = Map.put(estado, nombre, pid)
        # TODO: retornar {:reply, :ok, nuevo_estado}
    end
  end

  @impl GenServer
  def handle_call({:buscar, nombre}, _from, estado) do
    resultado =
      case Map.get(estado, nombre) do
        nil -> {:error, :no_encontrado}
        pid -> {:ok, pid}
      end
    # TODO: retornar {:reply, resultado, estado}
  end

  @impl GenServer
  # TODO: handle_call para :listar — retornar Map.keys(estado)
  def handle_call(:listar, _from, estado) do
  end

  @impl GenServer
  def handle_cast({:eliminar, nombre}, estado) do
    # TODO: retornar {:noreply, Map.delete(estado, nombre)}
  end
end

# Test it:
# RegistroGlobal.start_link()
# RegistroGlobal.registrar(:worker_1, self())       # => :ok
# RegistroGlobal.registrar(:worker_1, self())       # => {:error, :ya_registrado}
# RegistroGlobal.buscar(:worker_1)                  # => {:ok, #PID<...>}
# RegistroGlobal.buscar(:worker_99)                 # => {:error, :no_encontrado}
# RegistroGlobal.listar()                           # => [:worker_1]
# RegistroGlobal.eliminar(:worker_1)
# RegistroGlobal.listar()                           # => []
# RegistroGlobal.stop()
```

### Try It Yourself
Implementa un Rate Limiter GenServer que controle cuántas llamadas puede hacer un cliente en una ventana de tiempo.

Comportamiento esperado:
- `check/1` — recibe un client_id, retorna `{:ok, restantes}` si puede hacer más llamadas, o `{:error, :rate_limited}` si alcanzó el límite
- El límite por defecto es 5 llamadas por 10 segundos por cliente
- La ventana se reinicia después de cada período de 10 segundos (o puedes usar un contador simple que se resetea con un timer)
- `reset/1` — resetea el contador de un cliente específico
- `stats/0` — retorna un mapa con los contadores actuales de todos los clientes

```elixir
defmodule RateLimiter do
  use GenServer

  @limite_llamadas 5
  @ventana_ms 10_000

  # Estado: %{cliente_id => %{conteo: n, desde: timestamp}}

  def start_link(_opts \\ []), do: GenServer.start_link(__MODULE__, %{}, name: __MODULE__)
  def check(client_id), do: GenServer.call(__MODULE__, {:check, client_id})
  def reset(client_id), do: GenServer.cast(__MODULE__, {:reset, client_id})
  def stats, do: GenServer.call(__MODULE__, :stats)

  # Tu implementación de los callbacks aquí
end
```

## Common Mistakes

### Mistake 1: Olvidar @impl GenServer en los callbacks
```elixir
# ❌ Sin @impl, el compilador no verifica que el callback existe
def handle_call(:algo, _from, state), do: {:reply, :ok, state}

# ✓ Con @impl, el compilador advierte si el nombre está mal escrito
@impl GenServer
def handle_call(:algo, _from, state), do: {:reply, :ok, state}
```

### Mistake 2: Hacer trabajo lento en handle_call (bloquea a los clientes)
```elixir
# ❌ Todos los clientes que llamen call quedan bloqueados durante 5 segundos
@impl GenServer
def handle_call(:datos, _from, state) do
  resultado = peticion_http_lenta()   # 5 segundos — bloquea el GenServer
  {:reply, resultado, state}
end

# ✓ Para trabajo lento, usar Task y responder con GenServer.reply
@impl GenServer
def handle_call(:datos, from, state) do
  Task.start(fn ->
    resultado = peticion_http_lenta()
    GenServer.reply(from, resultado)   # Responder cuando esté listo
  end)
  {:noreply, state}   # No bloquear al GenServer
end
```

### Mistake 3: call que resulta en timeout porque el servidor murió
```elixir
# ❌ Si el GenServer no está corriendo, call lanza una excepción
GenServer.call(ServidorMuerto, :get)
# ** (exit) exited in: GenServer.call(ServidorMuerto, :get, 5000)

# ✓ Manejar con try/catch o verificar que está corriendo antes
case GenServer.whereis(MiServidor) do
  nil -> {:error, :server_down}
  _pid -> GenServer.call(MiServidor, :get)
end
```

### Mistake 4: Confundir el proceso del cliente con el del servidor
```elixir
# Las funciones de la API pública corren en el proceso del CLIENTE
def mi_funcion do
  self()   # PID del cliente que llamó mi_funcion
end

# Los callbacks corren en el proceso del SERVIDOR (el GenServer)
def handle_call(:quien_soy, _from, state) do
  self()   # PID del GenServer, no del cliente
end
```

## Verification
```bash
$ iex
iex> ContadorGenServer.start_link()
{:ok, #PID<0.115.0>}
iex> ContadorGenServer.incrementar()
:ok
iex> ContadorGenServer.get()
1
iex> KVStore.start_link()
{:ok, #PID<0.120.0>}
iex> KVStore.put(:test, 42)
:ok
iex> KVStore.get(:test)
42
iex> TareaPeriodicaGS.start_link(1000)
# Tick #1 (cada segundo)
# Tick #2
```

Checklist de verificación:
- [ ] `use GenServer` y `@impl GenServer` en todos los callbacks
- [ ] `init/1` retorna `{:ok, estado_inicial}`
- [ ] `handle_call/3` retorna `{:reply, respuesta, nuevo_estado}`
- [ ] `handle_cast/2` retorna `{:noreply, nuevo_estado}`
- [ ] `handle_info/2` maneja `:tick` del timer periódico
- [ ] `terminate/2` imprime el mensaje de cierre al detener el servidor
- [ ] El GenServer con nombre funciona sin pasar PID explícitamente
- [ ] Los callbacks están decorados con `@impl GenServer`

## Summary
- GenServer es el behaviour OTP fundamental para procesos con estado y ciclo de vida
- `handle_call` es síncrono (cliente bloquea), `handle_cast` es asíncrono (cliente no espera)
- `handle_info` recibe mensajes del sistema: timers, señales de monitor, y sends directos
- `terminate/2` permite cleanup al detener el servidor — crucial para liberar recursos
- Registrar con `name: __MODULE__` permite acceso global sin pasar PIDs
- Nunca hacer trabajo lento en `handle_call` — usa `Task` + `GenServer.reply/2` para trabajo async

## What's Next
**05-supervisor-basico**: Aprende a usar `Supervisor` para monitorear GenServers, reiniciarlos automáticamente cuando fallan, y construir sistemas tolerantes a fallos.

## Resources
- [GenServer — HexDocs](https://hexdocs.pm/elixir/GenServer.html)
- [GenServer.call/3 — HexDocs](https://hexdocs.pm/elixir/GenServer.html#call/3)
- [GenServer.cast/2 — HexDocs](https://hexdocs.pm/elixir/GenServer.html#cast/2)
- [Mix and OTP: GenServer](https://elixir-lang.org/getting-started/mix-otp/genserver.html)
