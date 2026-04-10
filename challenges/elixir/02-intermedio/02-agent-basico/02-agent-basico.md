# 2. Agent: Estado Mutable Seguro

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 01-procesos-spawn-send-receive
- Understanding of anonymous functions and closures
- Familiarity with maps and keyword lists

## Learning Objectives
After completing this exercise, you will be able to:
- Use `Agent` to hold and manage mutable state safely in a concurrent environment
- Start an agent with an initial state using `Agent.start_link/1`
- Read state with `Agent.get/2` without modifying it
- Update state atomically with `Agent.update/2`
- Combine read and write atomically with `Agent.get_and_update/2`
- Stop an agent gracefully with `Agent.stop/1`
- Register agents with names for global access

## Concepts

### Agent: abstracción sobre procesos con estado
En el ejercicio anterior vimos cómo crear procesos con `spawn` y mantener estado con recursión. `Agent` es exactamente eso, pero envuelto en una abstracción limpia. Un Agent es un proceso que almacena un valor (cualquier término Elixir) y expone una API síncrona para leerlo y modificarlo.

Internamente, Agent usa GenServer (que veremos en el próximo ejercicio). Lo que hace Agent es eliminar el boilerplate de implementar manualmente `handle_call` y `handle_cast` cuando todo lo que necesitas es guardar un valor y modificarlo de forma segura entre varios procesos concurrentes.

La seguridad viene del modelo de procesos de Elixir: como el estado vive en un proceso, solo un "cliente" puede modificarlo a la vez. No hay condiciones de carrera posibles sin usar ningún lock o mutex explícito.

```elixir
# Iniciar un agent con estado inicial 0
{:ok, pid} = Agent.start_link(fn -> 0 end)

# Leer el estado (no lo modifica)
Agent.get(pid, fn estado -> estado end)   # => 0

# Modificar el estado
Agent.update(pid, fn estado -> estado + 1 end)

# Leer de nuevo
Agent.get(pid, fn estado -> estado end)   # => 1
```

### get vs update vs get_and_update
Estos tres son los verbos fundamentales de Agent:

`Agent.get/2` ejecuta la función en el proceso del agent y devuelve su resultado al llamante. El estado no cambia. Se usa para consultas puras.

`Agent.update/2` ejecuta la función en el proceso del agent y reemplaza el estado con el valor que retorna la función. No devuelve el estado anterior. Se usa cuando solo necesitas cambiar el estado.

`Agent.get_and_update/2` ejecuta la función que debe retornar `{valor_a_devolver, nuevo_estado}`. Esto permite hacer la lectura y la escritura como una operación atómica — importante cuando el nuevo estado depende del estado anterior de forma que no puede haber una lectura separada antes de la escritura.

```elixir
{:ok, agent} = Agent.start_link(fn -> [1, 2, 3] end)

# get: lee sin modificar
Agent.get(agent, fn lista -> length(lista) end)   # => 3

# update: modifica, no retorna el estado
Agent.update(agent, fn lista -> [0 | lista] end)
# Estado ahora: [0, 1, 2, 3]

# get_and_update: pop atómico (saca el primero y actualiza)
Agent.get_and_update(agent, fn [h | t] -> {h, t} end)
# => 0 (el elemento sacado)
# Estado ahora: [1, 2, 3]
```

### Nombres de agents
Por defecto, un agent solo puede referenciarse por su PID. Si el PID cambia (por ejemplo, al reiniciarse bajo un supervisor), las referencias quedan obsoletas. Los nombres resuelven esto:

```elixir
# Registrar con nombre de módulo (el más común)
Agent.start_link(fn -> 0 end, name: MiContador)

# Ahora se puede usar el nombre directamente
Agent.get(MiContador, & &1)
Agent.update(MiContador, &(&1 + 1))
```

## Exercises

### Exercise 1: Counter agent
```elixir
defmodule ContadorAgent do
  @agent_name __MODULE__

  # TODO: Implementa `start/0` que:
  # 1. Llama Agent.start_link con estado inicial 0
  # 2. Usa `name: @agent_name` para registrarlo con nombre
  # 3. Retorna {:ok, pid} en caso de éxito
  def start do
    Agent.start_link(fn ->
      # TODO: estado inicial
    end, name: @agent_name)
  end

  # TODO: Implementa `get/0` que retorna el valor actual del contador
  # Usa @agent_name como referencia (no necesitas el PID)
  def get do
    Agent.get(@agent_name, fn estado ->
      # TODO: retornar el estado
    end)
  end

  # TODO: Implementa `incrementar/0` que suma 1 al contador
  def incrementar do
    Agent.update(@agent_name, fn estado ->
      # TODO: retornar estado + 1
    end)
  end

  # TODO: Implementa `decrementar/0` que resta 1 al contador
  def decrementar do
    Agent.update(@agent_name, fn estado ->
      # TODO: retornar estado - 1
    end)
  end

  # TODO: Implementa `reset/0` que vuelve el contador a 0
  def reset do
    Agent.update(@agent_name, fn _estado ->
      # TODO: retornar 0
    end)
  end

  def stop, do: Agent.stop(@agent_name)
end

# Test it:
# ContadorAgent.start()
# ContadorAgent.get()           # => 0
# ContadorAgent.incrementar()
# ContadorAgent.incrementar()
# ContadorAgent.get()           # => 2
# ContadorAgent.decrementar()
# ContadorAgent.get()           # => 1
# ContadorAgent.reset()
# ContadorAgent.get()           # => 0
# ContadorAgent.stop()
```

### Exercise 2: Stack agent con get_and_update
```elixir
defmodule StackAgent do
  # TODO: Implementa `start/0` que inicia el agent con una lista vacía como estado
  def start do
    Agent.start_link(fn ->
      # TODO: estado inicial (lista vacía)
    end)
  end

  # TODO: Implementa `push/2` que recibe el PID del agent y un valor:
  # Agrega el valor al frente de la lista (stack LIFO)
  # [nuevo | lista_actual]
  def push(agent, valor) do
    Agent.update(agent, fn lista ->
      # TODO: retornar [valor | lista]
    end)
  end

  # TODO: Implementa `pop/1` usando get_and_update — ATÓMICO:
  # Si la lista tiene elementos: retorna {:ok, primer_elemento} y actualiza estado a la cola
  # Si la lista está vacía: retorna {:error, :empty} y deja el estado sin cambios
  # PISTA: get_and_update espera que la función retorne {valor_devuelto, nuevo_estado}
  def pop(agent) do
    Agent.get_and_update(agent, fn
      [] ->
        # TODO: retornar {{:error, :empty}, []}
      [h | t] ->
        # TODO: retornar {{:ok, h}, t}
    end)
  end

  # TODO: Implementa `peek/1` que retorna el elemento en el tope sin sacarlo
  # Si está vacío, retorna {:error, :empty}
  def peek(agent) do
    Agent.get(agent, fn
      [] -> {:error, :empty}
      [h | _] ->
        # TODO: retornar {:ok, h}
    end)
  end

  # TODO: Implementa `size/1` que retorna cuántos elementos tiene el stack
  def size(agent) do
    Agent.get(agent, fn lista ->
      # TODO: retornar length(lista)
    end)
  end

  def stop(agent), do: Agent.stop(agent)
end

# Test it:
# {:ok, stack} = StackAgent.start()
# StackAgent.push(stack, :a)
# StackAgent.push(stack, :b)
# StackAgent.push(stack, :c)
# StackAgent.peek(stack)           # => {:ok, :c}
# StackAgent.pop(stack)            # => {:ok, :c}
# StackAgent.pop(stack)            # => {:ok, :b}
# StackAgent.size(stack)           # => 1
# StackAgent.pop(stack)            # => {:ok, :a}
# StackAgent.pop(stack)            # => {:error, :empty}
# StackAgent.stop(stack)
```

### Exercise 3: Config store agent
```elixir
defmodule ConfigAgent do
  # Un agent que funciona como un key-value store en memoria
  # El estado es un mapa %{clave => valor}

  # TODO: Implementa `start/1` que acepta una keyword list de configuración inicial
  # Convierte la keyword list a mapa con Enum.into(%{}) o Map.new/1
  # Registra el agent con name: __MODULE__
  def start(config_inicial \\ []) do
    estado_inicial = Map.new(config_inicial)
    Agent.start_link(fn ->
      # TODO: retornar estado_inicial
    end, name: __MODULE__)
  end

  # TODO: Implementa `get/1` que retorna el valor de una clave (o nil si no existe)
  def get(clave) do
    Agent.get(__MODULE__, fn mapa ->
      # TODO: retornar Map.get(mapa, clave)
    end)
  end

  # TODO: Implementa `get/2` que acepta un valor por defecto
  def get(clave, default) do
    Agent.get(__MODULE__, fn mapa ->
      # TODO: retornar Map.get(mapa, clave, default)
    end)
  end

  # TODO: Implementa `put/2` que guarda una clave-valor
  def put(clave, valor) do
    Agent.update(__MODULE__, fn mapa ->
      # TODO: retornar Map.put(mapa, clave, valor)
    end)
  end

  # TODO: Implementa `delete/1` que elimina una clave
  def delete(clave) do
    Agent.update(__MODULE__, fn mapa ->
      # TODO: retornar Map.delete(mapa, clave)
    end)
  end

  # TODO: Implementa `all/0` que retorna todo el mapa de configuración
  def all do
    Agent.get(__MODULE__, fn mapa ->
      # TODO: retornar mapa
    end)
  end

  def stop, do: Agent.stop(__MODULE__)
end

# Test it:
# ConfigAgent.start(host: "localhost", port: 4000, debug: false)
# ConfigAgent.get(:host)                    # => "localhost"
# ConfigAgent.get(:timeout, 5000)           # => 5000 (no existe, devuelve default)
# ConfigAgent.put(:timeout, 3000)
# ConfigAgent.get(:timeout)                 # => 3000
# ConfigAgent.all()                         # => %{host: "localhost", port: 4000, debug: false, timeout: 3000}
# ConfigAgent.delete(:debug)
# ConfigAgent.all()                         # => %{host: "localhost", port: 4000, timeout: 3000}
# ConfigAgent.stop()
```

### Exercise 4: Múltiples agents coordinados
```elixir
defmodule BancoSimple do
  # Dos agents: uno para saldo de cuenta A, otro para cuenta B
  # Esto demuestra cómo coordinar múltiples agents

  # TODO: Implementa `abrir_cuentas/2` que:
  # 1. Inicia un agent para la cuenta A con saldo inicial saldo_a
  # 2. Inicia un agent para la cuenta B con saldo inicial saldo_b
  # 3. Retorna {:ok, pid_a, pid_b}
  def abrir_cuentas(saldo_a, saldo_b) do
    {:ok, cuenta_a} = Agent.start_link(fn -> saldo_a end)
    {:ok, cuenta_b} = Agent.start_link(fn ->
      # TODO: estado inicial
    end)
    {:ok, cuenta_a, cuenta_b}
  end

  # TODO: Implementa `saldo/1` que retorna el saldo de una cuenta (por PID)
  def saldo(cuenta) do
    Agent.get(cuenta, fn s ->
      # TODO: retornar s
    end)
  end

  # TODO: Implementa `transferir/3` que transfiere `monto` de cuenta_origen a cuenta_destino:
  # 1. Verifica que cuenta_origen tenga saldo suficiente
  # 2. Si sí: debita origen, acredita destino, retorna {:ok, nuevo_saldo_origen}
  # 3. Si no: retorna {:error, :saldo_insuficiente}
  # IMPORTANTE: esto NO es atómico entre dos agents — es una limitación de este enfoque
  def transferir(origen, destino, monto) do
    saldo_origen = saldo(origen)
    if saldo_origen >= monto do
      # TODO: restar monto del origen
      Agent.update(origen, fn s -> s - monto end)
      # TODO: sumar monto al destino
      Agent.update(destino, fn s ->
        # TODO: retornar s + monto
      end)
      {:ok, saldo(origen)}
    else
      {:error, :saldo_insuficiente}
    end
  end

  def cerrar(cuenta_a, cuenta_b) do
    Agent.stop(cuenta_a)
    Agent.stop(cuenta_b)
  end
end

# Test it:
# {:ok, a, b} = BancoSimple.abrir_cuentas(1000, 500)
# BancoSimple.saldo(a)                    # => 1000
# BancoSimple.saldo(b)                    # => 500
# BancoSimple.transferir(a, b, 300)       # => {:ok, 700}
# BancoSimple.saldo(a)                    # => 700
# BancoSimple.saldo(b)                    # => 800
# BancoSimple.transferir(a, b, 1000)      # => {:error, :saldo_insuficiente}
# BancoSimple.cerrar(a, b)
```

### Exercise 5: Agent bajo supervisión
```elixir
defmodule ContadorSupervisado do
  # Un agent diseñado para correr bajo un Supervisor.
  # La clave: usar name: __MODULE__ y exponer start_link/1
  # con la firma correcta que el Supervisor espera.

  # TODO: Implementa `start_link/1` — la firma que los Supervisores esperan:
  # Acepta opts (keyword list, puede ignorarse por ahora)
  # Llama Agent.start_link con estado inicial 0 y name: __MODULE__
  def start_link(_opts \\ []) do
    Agent.start_link(fn ->
      # TODO: estado inicial
    end, name: __MODULE__)
  end

  # TODO: Implementa `child_spec/1` — la especificación para el Supervisor:
  # Retorna un mapa con:
  #   id: __MODULE__
  #   start: {__MODULE__, :start_link, [[]]}
  #   restart: :permanent   (siempre reiniciar si cae)
  #   type: :worker
  def child_spec(opts) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [opts]},
      # TODO: agregar restart: :permanent
      # TODO: agregar type: :worker
    }
  end

  def get, do: Agent.get(__MODULE__, & &1)
  def incrementar, do: Agent.update(__MODULE__, &(&1 + 1))
  def reset, do: Agent.update(__MODULE__, fn _ -> 0 end)

  # Para probar que sobrevive a reinicios:
  def crash!, do: Agent.update(__MODULE__, fn _ -> raise "¡Crash intencional!" end)
end

# Para usar bajo un Supervisor (en IEx):
# children = [ContadorSupervisado]
# {:ok, sup} = Supervisor.start_link(children, strategy: :one_for_one)
# ContadorSupervisado.incrementar()
# ContadorSupervisado.get()     # => 1
# El Supervisor lo reiniciará si falla

# Test it (sin supervisor):
# ContadorSupervisado.start_link()
# ContadorSupervisado.incrementar()
# ContadorSupervisado.incrementar()
# ContadorSupervisado.get()     # => 2
```

### Try It Yourself
Construye un carrito de compras usando Agent. El carrito debe soportar:

- `start/0` — inicia el carrito vacío
- `agregar_item/3` — agrega `{nombre, precio, cantidad}` al carrito
- `quitar_item/2` — elimina un item por nombre
- `actualizar_cantidad/3` — cambia la cantidad de un item existente
- `get_total/1` — calcula el total (`precio * cantidad` para cada item)
- `listar_items/1` — lista todos los items actuales
- `vaciar/1` — elimina todos los items

El estado interno puede ser un mapa `%{nombre => {precio, cantidad}}`.

```elixir
defmodule CarritoCompras do
  # Tu implementación aquí

  # Debería comportarse así:
  # {:ok, carrito} = CarritoCompras.start()
  # CarritoCompras.agregar_item(carrito, "manzana", 0.50, 4)
  # CarritoCompras.agregar_item(carrito, "pan", 1.20, 2)
  # CarritoCompras.listar_items(carrito)
  # # => %{"manzana" => {0.50, 4}, "pan" => {1.20, 2}}
  # CarritoCompras.get_total(carrito)    # => 4.40
  # CarritoCompras.actualizar_cantidad(carrito, "manzana", 2)
  # CarritoCompras.get_total(carrito)    # => 3.40
  # CarritoCompras.quitar_item(carrito, "pan")
  # CarritoCompras.get_total(carrito)    # => 1.00
end
```

## Common Mistakes

### Mistake 1: Hacer lógica compleja dentro del agent
```elixir
# ❌ Si la función tarda mucho, bloquea a todos los que esperan acceso al agent
Agent.update(agent, fn estado ->
  resultado = hacer_peticion_http(estado.url)   # ¡Bloquea el agent!
  %{estado | cache: resultado}
end)

# ✓ Computar fuera del agent, solo pasar el resultado
resultado = hacer_peticion_http(url)
Agent.update(agent, fn estado -> %{estado | cache: resultado} end)
```

### Mistake 2: Confundir get con get_and_update para operaciones atómicas
```elixir
# ❌ NO atómico — otro proceso puede modificar el estado entre get y update
valor = Agent.get(agent, & &1)
Agent.update(agent, fn _ -> valor + 1 end)

# ✓ Atómico — todo ocurre en una sola operación
Agent.get_and_update(agent, fn estado -> {estado, estado + 1} end)
# o simplemente:
Agent.update(agent, &(&1 + 1))
```

### Mistake 3: No usar nombres en agents de larga vida
```elixir
# ❌ Si el agent se reinicia, el PID cambia y la referencia queda inválida
{:ok, pid} = Agent.start_link(fn -> 0 end)
# ... más tarde, si el agent cayó y fue reiniciado ...
Agent.get(pid, & &1)   # ** (exit) no process

# ✓ Usar nombre para que el Supervisor pueda reiniciarlo y sea encontrable
Agent.start_link(fn -> 0 end, name: MiAgent)
Agent.get(MiAgent, & &1)   # Funciona aunque se haya reiniciado
```

### Mistake 4: Olvidar que Agent.stop/1 es síncrono
```elixir
# Agent.stop/1 espera a que el proceso termine antes de retornar
# Si el agent está procesando algo, stop espera a que termine
Agent.stop(agent)   # Seguro — el estado final se preserva hasta aquí
```

## Verification
```bash
$ iex
iex> ContadorAgent.start()
{:ok, #PID<0.115.0>}
iex> ContadorAgent.incrementar()
:ok
iex> ContadorAgent.incrementar()
:ok
iex> ContadorAgent.get()
2
iex> ContadorAgent.reset()
:ok
iex> ContadorAgent.get()
0
iex> ContadorAgent.stop()
:ok
```

Checklist de verificación:
- [ ] `Agent.start_link` con estado inicial funciona correctamente
- [ ] `Agent.get` retorna el estado sin modificarlo
- [ ] `Agent.update` modifica el estado y retorna `:ok`
- [ ] `Agent.get_and_update` es atómico — lee y escribe en una sola operación
- [ ] El stack agent hace pop atómico correctamente
- [ ] El config agent soporta todas las operaciones CRUD
- [ ] `start_link/1` tiene la firma correcta para ser usado por un Supervisor
- [ ] `child_spec/1` retorna el mapa correcto

## Summary
- `Agent` es una abstracción sobre procesos para gestionar estado mutable de forma segura
- El estado vive en un proceso propio — no hay condiciones de carrera sin locks explícitos
- `get/2` lee, `update/2` escribe, `get_and_update/2` hace ambas operaciones atómicamente
- Para usar con Supervisores, implementar `start_link/1` con firma estándar y `child_spec/1`
- Mantener las funciones pasadas al agent simples y rápidas — bloquean el acceso de otros procesos
- Registrar agents con nombre cuando deben sobrevivir reinicios bajo un Supervisor

## What's Next
**03-task-y-concurrencia**: Aprende a usar `Task` para ejecutar trabajo en paralelo y recolectar resultados de forma asíncrona, ideal para I/O concurrente y procesamiento en batch.

## Resources
- [Agent — HexDocs](https://hexdocs.pm/elixir/Agent.html)
- [Agent.get/2 — HexDocs](https://hexdocs.pm/elixir/Agent.html#get/3)
- [Agent.get_and_update/2 — HexDocs](https://hexdocs.pm/elixir/Agent.html#get_and_update/3)
- [Mix and OTP: Agent](https://elixir-lang.org/getting-started/mix-otp/agent.html)
