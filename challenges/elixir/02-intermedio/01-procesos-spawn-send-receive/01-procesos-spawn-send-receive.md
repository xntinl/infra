# 1. Procesos: spawn, send y receive

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Understanding of anonymous functions (`fn -> end`)
- Familiarity with pattern matching and basic modules
- Comfort with IEx interactive shell

## Learning Objectives
After completing this exercise, you will be able to:
- Create lightweight Elixir processes with `spawn/1` and `spawn_link/1`
- Send messages between processes using `send/2`
- Receive and pattern-match messages with `receive/1`
- Handle message timeouts using the `after` clause
- Understand process isolation and what happens when linked processes crash
- Build simple concurrent communication patterns

## Concepts

### Procesos en Elixir: ligeros y aislados
En Elixir, los procesos no son hilos del sistema operativo ni procesos del SO. Son procesos de la máquina virtual BEAM, extremadamente ligeros: puedes crear millones de ellos en una sola máquina. Cada proceso tiene su propio heap de memoria, su propia pila de ejecución, y su propio mailbox (buzón de mensajes). No comparten memoria entre sí — la única forma de comunicarse es enviando mensajes.

Esta arquitectura tiene una consecuencia crucial: si un proceso falla (lanza una excepción), ese fallo está completamente aislado. El resto de los procesos continúan ejecutándose sin verse afectados. Esta es la base del principio "let it crash" de Erlang/Elixir.

Cada proceso tiene un identificador único llamado PID (Process Identifier). Puedes obtener el PID del proceso actual con `self/0`. Necesitas conocer el PID de un proceso para enviarle mensajes.

```elixir
# El proceso actual siempre tiene un PID
my_pid = self()
IO.inspect(my_pid)   # => #PID<0.110.0>

# spawn crea un proceso nuevo y devuelve su PID
pid = spawn(fn ->
  IO.puts("Soy un proceso nuevo, mi PID es #{inspect(self())}")
end)

IO.inspect(pid)      # => #PID<0.111.0>
```

### send/2 y receive/1: el protocolo de mensajes
`send/2` envía un mensaje al buzón (mailbox) de un proceso. El mensaje puede ser cualquier término Elixir: átomos, tuplas, listas, mapas. `send` siempre retorna inmediatamente — no bloquea al proceso que envía.

`receive/1` bloquea al proceso actual hasta que llegue un mensaje que coincida con alguna de sus cláusulas. Funciona exactamente como `case`, pero sobre el mailbox. Los mensajes que no coinciden con ninguna cláusula permanecen en el mailbox para la próxima llamada a `receive`.

```elixir
# Enviamos un mensaje a nosotros mismos para practicar
send(self(), {:saludo, "Hola mundo"})

# receive bloquea hasta que llega un mensaje que hace match
resultado = receive do
  {:saludo, texto} -> "Recibí: #{texto}"
  {:error, razon}  -> "Error: #{razon}"
end

IO.puts(resultado)   # => "Recibí: Hola mundo"
```

### after: timeouts en receive
Sin cláusula `after`, `receive` espera indefinidamente. Esto puede causar que un proceso se quede bloqueado para siempre si el mensaje nunca llega. La cláusula `after` especifica un timeout en milisegundos:

```elixir
receive do
  {:respuesta, valor} -> {:ok, valor}
after
  5_000 ->
    # Se ejecuta si pasan 5 segundos sin mensaje
    {:error, :timeout}
end
```

### spawn_link/1: procesos enlazados
`spawn_link/1` crea un proceso y establece un "enlace" bidireccional entre el proceso padre y el hijo. Si cualquiera de los dos falla o termina anormalmente, el otro recibirá una señal de salida y también terminará (a menos que esté "trapping exits"). Esta es la base de la tolerancia a fallos en OTP — los supervisores usan este mecanismo para detectar cuando un proceso hijo muere.

```elixir
# Con spawn_link, si el hijo falla, el padre también cae
pid = spawn_link(fn ->
  raise "¡Algo salió mal!"
end)

# El proceso padre recibirá una señal de exit y también terminará
# (en IEx esto significa que IEx se reinicia, pero sigue funcionando)
```

## Exercises

### Exercise 1: Spawn básico y PID
```elixir
defmodule ProcesosBasico do
  # TODO: Implementa `saludar/0` que:
  # 1. Imprime el PID del proceso actual con inspect(self())
  # 2. Usa spawn/1 para crear un proceso nuevo
  # 3. Dentro del proceso nuevo, imprime "Hola desde proceso hijo" y su propio PID
  # 4. Retorna el PID del proceso hijo
  def saludar do
    IO.puts("Proceso padre: #{inspect(self())}")
    # TODO: spawn un proceso que imprima su PID
    pid = spawn(fn ->
      # tu código aquí
    end)
    # Dale un momento al proceso para ejecutarse antes de que el padre termine
    :timer.sleep(100)
    pid
  end

  # TODO: Implementa `cuantos_procesos/0` que:
  # 1. Llama a `length(Process.list())` antes de crear procesos
  # 2. Crea 5 procesos con spawn que hagan :timer.sleep(500)
  # 3. Llama a `length(Process.list())` después
  # 4. Imprime cuántos procesos había antes y cuántos hay ahora
  def cuantos_procesos do
    antes = # TODO: obtener cantidad de procesos vivos
    # TODO: crear 5 procesos que duerman 500ms
    despues = # TODO: obtener cantidad de procesos vivos
    IO.puts("Antes: #{antes}, Después: #{despues}")
  end
end

# Test it:
# ProcesosBasico.saludar()
# ProcesosBasico.cuantos_procesos()
```

### Exercise 2: Echo server — enviar y recibir
```elixir
defmodule EchoServer do
  # TODO: Implementa `start/0` que:
  # 1. Usa spawn/1 para iniciar un proceso que corra `loop/0`
  # 2. Retorna el PID del proceso echo
  def start do
    spawn(fn -> loop() end)
  end

  # TODO: Implementa `loop/0` — el corazón del echo server:
  # Debe hacer receive esperando el patrón {:echo, sender_pid, message}
  # Cuando lo recibe, envía de vuelta {:respuesta, message} al sender_pid
  # Luego se llama a sí mismo recursivamente para seguir escuchando
  defp loop do
    receive do
      {:echo, from, message} ->
        # TODO: enviar {:respuesta, message} de vuelta a `from`
        # TODO: llamar loop() recursivamente
    end
  end

  # TODO: Implementa `send_echo/2` que recibe un pid de echo server y un mensaje:
  # 1. Envía {:echo, self(), message} al echo server
  # 2. Espera la respuesta con receive (timeout de 1000ms)
  # 3. Retorna el mensaje recibido o {:error, :timeout}
  def send_echo(server_pid, message) do
    send(server_pid, {:echo, self(), message})
    receive do
      # TODO: hacer match de {:respuesta, msg} y retornar msg
    after
      1000 -> {:error, :timeout}
    end
  end
end

# Test it:
# pid = EchoServer.start()
# EchoServer.send_echo(pid, "Hola")       # => "Hola"
# EchoServer.send_echo(pid, :ping)        # => :ping
# EchoServer.send_echo(pid, {1, 2, 3})    # => {1, 2, 3}
```

### Exercise 3: Timeout con after
```elixir
defmodule TimeoutDemo do
  # TODO: Implementa `esperar_mensaje/1` que recibe un timeout en ms:
  # Intenta recibir cualquier mensaje con {:dato, valor}
  # Si llega dentro del timeout, retorna {:ok, valor}
  # Si no llega, retorna {:error, :timeout}
  def esperar_mensaje(timeout_ms) do
    receive do
      # TODO: match {:dato, valor} y retornar {:ok, valor}
    after
      timeout_ms ->
        # TODO: retornar {:error, :timeout}
    end
  end

  # TODO: Implementa `demo_timeout/0` que:
  # 1. En un proceso separado, espera 2000ms y luego envía {:dato, "tarde"} al padre
  # 2. El proceso principal llama esperar_mensaje(500)
  # 3. Imprime qué pasó (debería ser timeout porque 500 < 2000)
  def demo_timeout do
    padre = self()
    spawn(fn ->
      :timer.sleep(2_000)
      # TODO: enviar {:dato, "tarde"} al padre
    end)
    resultado = esperar_mensaje(500)
    IO.puts("Resultado con timeout corto: #{inspect(resultado)}")
  end

  # TODO: Implementa `demo_exito/0` que:
  # 1. En un proceso separado, espera 100ms y envía {:dato, "a tiempo"} al padre
  # 2. El proceso principal llama esperar_mensaje(1000)
  # 3. Imprime el resultado (debería recibir el mensaje)
  def demo_exito do
    padre = self()
    spawn(fn ->
      :timer.sleep(100)
      # TODO: enviar {:dato, "a tiempo"} al padre
    end)
    resultado = esperar_mensaje(1_000)
    IO.puts("Resultado con tiempo suficiente: #{inspect(resultado)}")
  end
end

# Test it:
# TimeoutDemo.demo_timeout()   # => Resultado con timeout corto: {:error, :timeout}
# TimeoutDemo.demo_exito()     # => Resultado con tiempo suficiente: {:ok, "a tiempo"}
```

### Exercise 4: Receive loop con múltiples mensajes
```elixir
defmodule Acumulador do
  # TODO: Implementa `start/0` que lanza un proceso acumulador.
  # El proceso mantiene una lista interna (empieza vacía).
  # Acepta estos mensajes:
  #   {:agregar, valor}      — agrega valor a la lista
  #   {:obtener, from}       — envía {:lista, lista_actual} a `from`
  #   :limpiar               — reinicia la lista a []
  #   :detener               — termina el proceso (no llama loop recursivamente)
  def start do
    spawn(fn -> loop([]) end)
  end

  # TODO: Implementa el loop recursivo con estado (la lista acumulada)
  # Cada cláusula del receive maneja uno de los mensajes descritos arriba
  defp loop(lista) do
    receive do
      {:agregar, valor} ->
        # TODO: llamar loop con lista ++ [valor]

      {:obtener, from} ->
        # TODO: enviar {:lista, lista} a from, luego loop(lista)

      :limpiar ->
        # TODO: loop con lista vacía

      :detener ->
        # No llamar loop — el proceso termina aquí
        :ok
    end
  end

  # TODO: Implementa `obtener/1` que pide la lista al proceso y espera respuesta
  def obtener(pid) do
    send(pid, {:obtener, self()})
    receive do
      {:lista, lista} -> lista
    after
      1000 -> {:error, :timeout}
    end
  end
end

# Test it:
# pid = Acumulador.start()
# send(pid, {:agregar, 1})
# send(pid, {:agregar, 2})
# send(pid, {:agregar, 3})
# Acumulador.obtener(pid)    # => [1, 2, 3]
# send(pid, :limpiar)
# Acumulador.obtener(pid)    # => []
# send(pid, :detener)
```

### Exercise 5: spawn_link y propagación de fallos
```elixir
defmodule EnlaceDemo do
  # TODO: Implementa `spawn_sin_link/0` que:
  # 1. Crea un proceso con spawn/1 (sin link) que lanza raise "¡Fallo!"
  # 2. Espera 200ms
  # 3. Imprime "El padre sigue vivo" — el padre no debe verse afectado
  def spawn_sin_link do
    spawn(fn ->
      :timer.sleep(50)
      raise "¡Fallo sin link!"
    end)
    :timer.sleep(200)
    IO.puts("El padre sigue vivo (sin link) — el fallo del hijo fue aislado")
  end

  # TODO: Implementa `spawn_con_link/0` para demostración en IEx:
  # ADVERTENCIA: Este proceso CRASHEARÁ el proceso que lo llame.
  # En IEx, esto reinicia el shell pero IEx sobrevive.
  # 1. Crea un proceso con spawn_link/1 que espera 50ms y luego hace raise
  # 2. Imprime un mensaje indicando que el proceso fue creado
  # (El proceso actual morirá cuando el hijo falle)
  def spawn_con_link do
    IO.puts("Creando proceso enlazado que fallará en 50ms...")
    IO.puts("El proceso padre también morirá cuando el hijo falle")
    spawn_link(fn ->
      :timer.sleep(50)
      # TODO: lanzar un error con raise "¡Fallo del hijo enlazado!"
    end)
    :timer.sleep(200)
    IO.puts("Esta línea nunca se ejecutará si hay link")
  end

  # TODO: Implementa `proceso_robusto/0` que:
  # Usa Process.flag(:trap_exit, true) para capturar señales de exit en vez de morir
  # Luego hace spawn_link de un proceso que falla
  # Recibe el mensaje {:EXIT, pid, reason} que llegará al mailbox
  # Imprime "Capturé el exit: #{inspect(reason)}"
  def proceso_robusto do
    Process.flag(:trap_exit, true)
    spawn_link(fn ->
      raise "Fallo capturado"
    end)
    receive do
      {:EXIT, _pid, reason} ->
        # TODO: imprimir el reason capturado
    after
      1000 -> IO.puts("No llegó señal de exit")
    end
  end
end

# Test it (ejecutar en IEx):
# EnlaceDemo.spawn_sin_link()    # El padre sobrevive
# EnlaceDemo.proceso_robusto()   # Captura el exit en vez de morir
#
# ADVERTENCIA — solo en IEx (reinicia el shell):
# EnlaceDemo.spawn_con_link()
```

### Try It Yourself
Construye un sistema "ping-pong" entre dos procesos que intercambien el mensaje exactamente 5 rondas, luego terminen limpiamente.

Requisitos:
- Un proceso `ping` y un proceso `pong`
- Cada ronda: `ping` envía `{:ping, ronda}` a pong, pong responde `{:pong, ronda}` a ping
- Después de 5 rondas completas, ambos procesos terminan normalmente
- El proceso que lanza el juego (puede ser el proceso principal) imprime cada intercambio
- Usa `spawn_link/1` para ambos procesos

Pista: el proceso `pong` solo necesita un loop `receive`. El proceso `ping` controla las rondas y arranca el primer envío.

```elixir
defmodule PingPong do
  def jugar do
    # Tu implementación aquí
    # Debería imprimir algo como:
    # Ronda 1: ping → pong
    # Ronda 1: pong → ping
    # Ronda 2: ping → pong
    # ...
    # Ronda 5: pong → ping
    # ¡Juego terminado!
  end
end
```

## Common Mistakes

### Mistake 1: Olvidar que spawn es asíncrono
```elixir
# ❌ El proceso padre puede terminar antes de que el hijo imprima
spawn(fn -> IO.puts("Hijo") end)
# Si este es el final del programa, "Hijo" quizás nunca aparece

# ✓ Dar tiempo al proceso hijo, o usar sincronización con mensajes
spawn(fn -> IO.puts("Hijo") end)
:timer.sleep(100)   # Solo para demos — en producción usa mensajes
```

### Mistake 2: receive sin after en producción
```elixir
# ❌ Si el mensaje nunca llega, el proceso queda bloqueado para siempre
receive do
  {:resultado, val} -> val
end

# ✓ Siempre incluir after en código que puede esperar mensajes de terceros
receive do
  {:resultado, val} -> val
after
  5_000 -> {:error, :timeout}
end
```

### Mistake 3: Olvidar capturar el PID del padre antes de spawn
```elixir
# ❌ self() dentro del spawn devuelve el PID del HIJO, no del padre
spawn(fn ->
  send(self(), :mensaje)  # Se envía a sí mismo, no al padre
end)

# ✓ Capturar self() antes del spawn
padre = self()
spawn(fn ->
  send(padre, :mensaje)   # Ahora sí llega al padre
end)
```

### Mistake 4: spawn vs spawn_link en producción
```elixir
# ❌ En aplicaciones OTP, usar spawn desnudo ignora el árbol de supervisión
pid = spawn(fn -> Worker.run() end)

# ✓ Los workers deben estar bajo un Supervisor con spawn_link
# (En la práctica usarás GenServer + Supervisor, no spawn directo)
```

## Verification
```bash
# En IEx, cargar y probar:
$ iex
iex> c("ejercicios.exs")
iex> EchoServer.start() |> EchoServer.send_echo("test")
# => "test"
iex> TimeoutDemo.demo_timeout()
# => Resultado con timeout corto: {:error, :timeout}
iex> TimeoutDemo.demo_exito()
# => Resultado con tiempo suficiente: {:ok, "a tiempo"}
```

Checklist de verificación:
- [ ] `spawn/1` crea un proceso y retorna su PID
- [ ] `send/2` retorna inmediatamente (no bloquea)
- [ ] `receive` hace pattern matching sobre mensajes en el mailbox
- [ ] `after` en `receive` funciona como timeout
- [ ] El echo server responde correctamente a múltiples mensajes
- [ ] El acumulador mantiene estado entre mensajes
- [ ] `spawn_sin_link` no afecta al padre cuando el hijo falla
- [ ] `proceso_robusto` captura señales con `trap_exit`

## Summary
- Los procesos BEAM son extremadamente ligeros — puedes crear millones
- Cada proceso tiene su propio mailbox; los mensajes no se comparten
- `spawn/1` crea un proceso aislado; `spawn_link/1` crea uno enlazado
- `send/2` es no-bloqueante; `receive/1` bloquea hasta que llega un mensaje
- La cláusula `after` en `receive` previene bloqueos indefinidos
- Los procesos enlazados propagan fallos — la base de la tolerancia a fallos
- `Process.flag(:trap_exit, true)` convierte señales de exit en mensajes normales

## What's Next
**02-agent-basico**: Aprende a usar `Agent` como abstracción sobre procesos para manejar estado mutable de forma segura, sin gestionar manualmente el mailbox.

## Resources
- [Process module — HexDocs](https://hexdocs.pm/elixir/Process.html)
- [Kernel.spawn/1 — HexDocs](https://hexdocs.pm/elixir/Kernel.html#spawn/1)
- [Kernel.send/2 — HexDocs](https://hexdocs.pm/elixir/Kernel.html#send/2)
- [Getting Started: Processes](https://elixir-lang.org/getting-started/processes.html)
