# 7. Behaviours y Callbacks: Contratos de Módulo

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 04-genserver-basico (ya usaste behaviours como GenServer)
- Understanding of modules, functions, and pattern matching
- Familiarity with `@impl` from GenServer exercises

## Learning Objectives
After completing this exercise, you will be able to:
- Define a behaviour contract with `@callback` in a module
- Implement a behaviour with `@behaviour` and `@impl`
- Write functions genéricas que acepten cualquier módulo que implemente un behaviour
- Understand how Elixir uses behaviours for polymorphism
- Receive compile-time warnings when a callback implementation is missing

## Concepts

### Qué es un behaviour
Un "behaviour" en Elixir (y Erlang) es un contrato que define qué funciones debe implementar un módulo. Es el equivalente a una interfaz en Java o TypeScript, o una abstract base class en Python. La diferencia clave: en Elixir los behaviours son estructurales y verificados en tiempo de compilación.

Ya has trabajado con behaviours sin saberlo: `GenServer`, `Supervisor`, y `Application` son todos behaviours. Cuando escribes `use GenServer` y defines `init/1`, `handle_call/3`, etc., estás implementando el behaviour GenServer.

La utilidad principal de los behaviours es permitir polimorfismo: puedes escribir código que funcione con cualquier módulo que implemente el contrato, sin importar los detalles internos de cada implementación.

```elixir
# Define el contrato
defmodule Notificador do
  @callback notificar(mensaje :: String.t()) :: :ok | {:error, term()}
  @callback canal() :: atom()
end

# Implementa el contrato
defmodule NotificadorEmail do
  @behaviour Notificador

  @impl Notificador
  def notificar(mensaje), do: enviar_email(mensaje)

  @impl Notificador
  def canal, do: :email
end
```

### @callback: definir el contrato
`@callback` declara una función que DEBE ser implementada por cualquier módulo que declare `@behaviour MiModulo`. La sintaxis es similar a una typespec: nombre, argumentos con tipos, y tipo de retorno.

```elixir
defmodule SerializadorBehaviour do
  # @callback nombre(arg :: tipo) :: tipo_retorno
  @callback serializar(data :: term()) :: binary()
  @callback deserializar(binary :: binary()) :: {:ok, term()} | {:error, String.t()}

  # @optional_callbacks: callbacks que NO son obligatorios
  @optional_callbacks [validar: 1]
  @callback validar(data :: term()) :: boolean()
end
```

### @impl: verificación en compile time
Cuando usas `@impl NombreDelBehaviour` antes de una función, el compilador verifica que esa función existe en el behaviour y que tiene la aridad correcta. Si cometes un typo o la aridad no coincide, obtienes un error de compilación — no un error en runtime.

```elixir
defmodule MiImplementacion do
  @behaviour SerializadorBehaviour

  @impl SerializadorBehaviour
  def serializar(data), do: Jason.encode!(data)   # ✓

  @impl SerializadorBehaviour
  def deserializar(bin), do: Jason.decode(bin)    # ✓

  # Si olvidas implementar deserializar/1, el compilador emite una advertencia:
  # warning: function deserializar/1 required by behaviour SerializadorBehaviour
  #          is not implemented (in module MiImplementacion)
end
```

### Funciones genéricas con behaviours
La verdadera potencia de los behaviours es poder escribir código que funciona con cualquier implementación:

```elixir
defmodule SistemaAlertas do
  # Esta función no sabe nada sobre Email, SMS, o Slack
  # Solo sabe que el módulo que recibe implementa Notificador
  def enviar_alerta(notificador_module, mensaje) do
    notificador_module.notificar(mensaje)
  end

  # Puede usarse con cualquier implementación:
  # SistemaAlertas.enviar_alerta(NotificadorEmail, "Servidor caído")
  # SistemaAlertas.enviar_alerta(NotificadorSMS, "Servidor caído")
  # SistemaAlertas.enviar_alerta(NotificadorSlack, "Servidor caído")
end
```

## Exercises

### Exercise 1: Definir el behaviour Notificador
```elixir
defmodule Notificador do
  # Este módulo define el CONTRATO que deben implementar todos los notificadores

  # TODO: Define estos 3 callbacks:
  # 1. `notificar/1` — recibe un String.t(), retorna :ok o {:error, term()}
  # 2. `canal/0` — sin argumentos, retorna un atom() (nombre del canal)
  # 3. `disponible?/0` — sin argumentos, retorna boolean() (si el canal está activo)
  @callback notificar(mensaje :: String.t()) :: :ok | {:error, term()}
  # TODO: @callback canal() :: atom()
  # TODO: @callback disponible?() :: boolean()

  # Función helper: verifica si un módulo implementa este behaviour
  def implementa?(modulo) do
    behaviours = modulo.module_info(:attributes)[:behaviour] || []
    __MODULE__ in behaviours
  end
end

# Test it:
# Notificador.implementa?(NotificadorEmail)   # => true (después del exercise 2)
# Notificador.implementa?(String)             # => false
```

### Exercise 2: Implementar NotificadorEmail y NotificadorSMS
```elixir
defmodule NotificadorEmail do
  @behaviour Notificador

  # TODO: Declara @impl Notificador e implementa notificar/1:
  # Imprime "[Email] Enviando: #{mensaje}"
  # :timer.sleep(50) para simular latencia de red
  # Retorna :ok
  @impl Notificador
  def notificar(mensaje) do
    IO.puts("[Email] Enviando: #{mensaje}")
    :timer.sleep(50)
    # TODO: retornar :ok
  end

  # TODO: Declara @impl Notificador e implementa canal/0:
  # Retorna :email
  @impl Notificador
  def canal do
    # TODO: retornar :email
  end

  # TODO: Declara @impl Notificador e implementa disponible?/0:
  # Retorna true siempre (simula que el canal email siempre está disponible)
  @impl Notificador
  def disponible? do
    # TODO: retornar true
  end
end

defmodule NotificadorSMS do
  @behaviour Notificador

  # TODO: Implementa los 3 callbacks para SMS:
  # notificar/1: imprime "[SMS] Enviando: #{mensaje}", simula latencia 100ms
  # canal/0: retorna :sms
  # disponible?/0: simula disponibilidad aleatoria con Enum.random([true, false])
  @impl Notificador
  def notificar(mensaje) do
    IO.puts("[SMS] Enviando: #{mensaje}")
    :timer.sleep(100)
    # TODO: retornar :ok
  end

  @impl Notificador
  def canal do
    # TODO: retornar :sms
  end

  @impl Notificador
  def disponible? do
    Enum.random([true, true, false])   # 66% disponible
  end
end

# Test it:
# NotificadorEmail.notificar("Servidor caído")   # [Email] Enviando: Servidor caído
# NotificadorSMS.canal()                          # => :sms
# NotificadorSMS.disponible?()                    # => true o false
```

### Exercise 3: Implementar NotificadorSlack
```elixir
defmodule NotificadorSlack do
  # TODO: Declara que este módulo implementa el behaviour Notificador
  @behaviour Notificador

  # Simula configuración de webhook
  @webhook_url "https://hooks.slack.com/services/T000/B000/XXXX"
  @canal_slack "#alertas"

  # TODO: Implementa notificar/1:
  # Imprime "[Slack → #{@canal_slack}] #{mensaje}"
  # Simula latencia de 30ms
  # 10% de las veces retorna {:error, :webhook_timeout} (Enum.random)
  # El resto retorna :ok
  @impl Notificador
  def notificar(mensaje) do
    IO.puts("[Slack → #{@canal_slack}] #{mensaje}")
    :timer.sleep(30)
    case Enum.random(1..10) do
      10 -> {:error, :webhook_timeout}
      _  ->
        # TODO: retornar :ok
    end
  end

  # TODO: Implementa canal/0 — retorna :slack
  @impl Notificador
  def canal do
    # TODO
  end

  # TODO: Implementa disponible?/0 — retorna true siempre
  @impl Notificador
  def disponible? do
    # TODO
  end

  # Función extra, no parte del behaviour — válido tener funciones adicionales
  def webhook_url, do: @webhook_url
end

# Test it:
# NotificadorSlack.notificar("Deploy completado")
# NotificadorSlack.disponible?()    # => true
# NotificadorSlack.webhook_url()    # => "https://hooks.slack.com/..."
# Notificador.implementa?(NotificadorSlack)   # => true
```

### Exercise 4: Función genérica que usa el behaviour
```elixir
defmodule SistemaAlertas do
  # Este módulo trabaja con CUALQUIER implementación de Notificador
  # sin conocer los detalles internos de cada uno

  # TODO: Implementa `enviar/2` que recibe (modulo_notificador, mensaje):
  # 1. Verifica si el módulo implementa Notificador con Notificador.implementa?/1
  # 2. Si no implementa: retorna {:error, :not_a_notificador}
  # 3. Verifica si el canal está disponible con modulo.disponible?()
  # 4. Si no disponible: retorna {:error, {:no_disponible, modulo.canal()}}
  # 5. Si disponible: llama modulo.notificar(mensaje) y retorna el resultado
  def enviar(modulo, mensaje) do
    cond do
      not Notificador.implementa?(modulo) ->
        {:error, :not_a_notificador}
      not modulo.disponible?() ->
        # TODO: retornar {:error, {:no_disponible, modulo.canal()}}
      true ->
        modulo.notificar(mensaje)
    end
  end

  # TODO: Implementa `enviar_a_todos/2` que recibe (lista_de_modulos, mensaje):
  # Para cada módulo, llama enviar/2
  # Retorna un mapa %{canal => resultado} donde canal es el atom retornado por canal/0
  # PISTA: Enum.into/3 puede construir el mapa
  def enviar_a_todos(modulos, mensaje) do
    Enum.into(modulos, %{}, fn modulo ->
      canal = modulo.canal()
      resultado = enviar(modulo, mensaje)
      # TODO: retornar {canal, resultado}
    end)
  end

  # TODO: Implementa `canales_disponibles/1` que filtra solo los módulos disponibles:
  # Recibe lista de módulos notificadores
  # Retorna lista de atoms con los canales disponibles
  def canales_disponibles(modulos) do
    modulos
    |> Enum.filter(fn modulo ->
      # TODO: filtrar por modulo.disponible?()
    end)
    |> Enum.map(fn modulo ->
      # TODO: retornar modulo.canal()
    end)
  end
end

# Test it:
# notificadores = [NotificadorEmail, NotificadorSMS, NotificadorSlack]
# SistemaAlertas.enviar(NotificadorEmail, "Test")
# # => :ok
# SistemaAlertas.enviar(String, "Test")
# # => {:error, :not_a_notificador}
# SistemaAlertas.enviar_a_todos(notificadores, "Sistema caído")
# # => %{email: :ok, sms: :ok, slack: :ok | {:error, :webhook_timeout}}
# SistemaAlertas.canales_disponibles(notificadores)
# # => [:email, :sms] o [:email, :sms, :slack] dependiendo de disponibilidad
```

### Exercise 5: @impl y advertencias del compilador
```elixir
# Este ejercicio demuestra el valor de @impl para seguridad en compile time

defmodule ProcesadorPago do
  # Behaviour para procesar pagos
  @callback cobrar(monto :: float(), cliente_id :: String.t()) ::
    {:ok, transaction_id :: String.t()} | {:error, term()}

  @callback reembolsar(transaction_id :: String.t(), monto :: float()) ::
    :ok | {:error, term()}

  @callback saldo_disponible?(cliente_id :: String.t()) ::
    boolean()
end

defmodule ProcesadorMock do
  # Implementación para tests — no hace llamadas reales
  @behaviour ProcesadorPago

  # TODO: Implementa cobrar/2 con @impl:
  # Simula una transacción exitosa
  # Genera un transaction_id falso: "txn_#{:rand.uniform(999999)}"
  # Retorna {:ok, transaction_id}
  @impl ProcesadorPago
  def cobrar(monto, cliente_id) do
    IO.puts("[Mock] Cobrando $#{monto} a #{cliente_id}")
    txn_id = "txn_#{:rand.uniform(999_999)}"
    # TODO: retornar {:ok, txn_id}
  end

  # TODO: Implementa reembolsar/2 con @impl:
  # Imprime "[Mock] Reembolsando $#{monto} de txn #{transaction_id}"
  # Retorna :ok
  @impl ProcesadorPago
  def reembolsar(transaction_id, monto) do
    IO.puts("[Mock] Reembolsando $#{monto} de transacción #{transaction_id}")
    # TODO: retornar :ok
  end

  # TODO: Implementa saldo_disponible?/1 con @impl:
  # Siempre retorna true en el mock
  @impl ProcesadorPago
  def saldo_disponible?(_cliente_id) do
    # TODO: retornar true
  end
end

# Para ver la advertencia del compilador, intenta:
# defmodule ProcesadorIncompleto do
#   @behaviour ProcesadorPago
#   # Solo implementa cobrar pero le falta reembolsar y saldo_disponible?
#   @impl ProcesadorPago
#   def cobrar(_monto, _cliente), do: {:ok, "txn_fake"}
#   # El compilador advertirá:
#   # warning: function reembolsar/2 required by behaviour ProcesadorPago
#   #          is not implemented
# end

# Función genérica que usa el behaviour:
defmodule ServicioPagos do
  # TODO: Implementa `procesar_compra/3` que recibe (procesador, cliente_id, monto):
  # 1. Verifica saldo_disponible?(cliente_id) — si no: {:error, :saldo_insuficiente}
  # 2. Llama cobrar(monto, cliente_id) — si error: propaga el error
  # 3. Si ok: retorna {:ok, %{transaction_id: txn_id, monto: monto, cliente: cliente_id}}
  def procesar_compra(procesador, cliente_id, monto) do
    if procesador.saldo_disponible?(cliente_id) do
      case procesador.cobrar(monto, cliente_id) do
        {:ok, txn_id} ->
          # TODO: retornar {:ok, %{transaction_id: txn_id, monto: monto, cliente: cliente_id}}
        {:error, razon} ->
          # TODO: retornar {:error, razon}
      end
    else
      {:error, :saldo_insuficiente}
    end
  end
end

# Test it:
# ProcesadorMock.cobrar(29.99, "cliente_123")
# # => {:ok, "txn_847293"}
# ServicioPagos.procesar_compra(ProcesadorMock, "cliente_123", 49.99)
# # => {:ok, %{transaction_id: "txn_...", monto: 49.99, cliente: "cliente_123"}}
```

### Try It Yourself
Implementa un sistema de almacenamiento con comportamiento intercambiable:

Define el behaviour `Almacenamiento` con estos callbacks:
- `guardar/2` — recibe `(clave :: String.t(), valor :: term())`, retorna `:ok | {:error, term()}`
- `obtener/1` — recibe `(clave :: String.t())`, retorna `{:ok, term()} | {:error, :not_found}`
- `eliminar/1` — recibe `(clave :: String.t())`, retorna `:ok`
- `listar_claves/0` — sin argumentos, retorna `[String.t()]`
- `nombre/0` — sin argumentos, retorna `String.t()` (nombre del backend)

Implementa dos módulos:
- `AlmacenamientoMemoria` — usa un Agent internamente para almacenar en memoria
- `AlmacenamientoMock` — implementación stub para tests (valores hardcodeados, sin persistencia real)

Escribe una función genérica `Repositorio.ejecutar/2` que acepta cualquier módulo que implemente `Almacenamiento` y una función que recibe el módulo, demostrando el uso polimórfico.

```elixir
defmodule Almacenamiento do
  @callback guardar(clave :: String.t(), valor :: term()) :: :ok | {:error, term()}
  @callback obtener(clave :: String.t()) :: {:ok, term()} | {:error, :not_found}
  @callback eliminar(clave :: String.t()) :: :ok
  @callback listar_claves() :: [String.t()]
  @callback nombre() :: String.t()
end

defmodule AlmacenamientoMemoria do
  @behaviour Almacenamiento
  # Tu implementación aquí usando Agent
end

defmodule AlmacenamientoMock do
  @behaviour Almacenamiento
  # Tu implementación stub aquí
end

defmodule Repositorio do
  def ejecutar(modulo_almacenamiento, funcion) do
    # Tu implementación aquí
  end
end

# Debe funcionar así:
# AlmacenamientoMemoria.start_link()
# Repositorio.ejecutar(AlmacenamientoMemoria, fn backend ->
#   backend.guardar("usuario:1", %{nombre: "Ana"})
#   backend.guardar("usuario:2", %{nombre: "Luis"})
#   IO.inspect(backend.listar_claves())    # => ["usuario:1", "usuario:2"]
#   IO.inspect(backend.obtener("usuario:1"))  # => {:ok, %{nombre: "Ana"}}
# end)
```

## Common Mistakes

### Mistake 1: @behaviour sin @impl
```elixir
# ❌ Sin @impl, el compilador no verifica si implementas el callback correctamente
defmodule MiNotificador do
  @behaviour Notificador

  def notificar(msg), do: IO.puts(msg)   # Sin @impl — el compilador no valida
  def canal, do: :email
  # Si olvidas disponible?/0, no hay advertencia
end

# ✓ Siempre usar @impl para cada callback
defmodule MiNotificador do
  @behaviour Notificador

  @impl Notificador
  def notificar(msg), do: IO.puts(msg)

  @impl Notificador
  def canal, do: :email

  @impl Notificador
  def disponible?, do: true
  # Si olvidas cualquiera, el compilador advierte en compile time
end
```

### Mistake 2: Confundir behaviour con Protocol
```elixir
# Behaviours son para módulos (dispatch estático)
# Protocols son para tipos de datos (dispatch dinámico basado en el tipo del valor)

# ✓ Behaviour — quieres que diferentes MÓDULOS implementen la misma API
# Ejemplo: NotificadorEmail, NotificadorSMS — son módulos con implementación propia

# ✓ Protocol — quieres que diferentes TIPOS implementen la misma función
# Ejemplo: String.Chars, Enumerable — protocolo sobre tipos de datos existentes
```

### Mistake 3: Olvidar que @optional_callbacks requiere declaración explícita
```elixir
# ❌ Un callback opcional no declarado como tal genera advertencia si no se implementa
defmodule MiBehaviour do
  @callback requerido(t :: term()) :: :ok
  @callback opcional(t :: term()) :: :ok   # No marcado como opcional
end

# ✓ Declarar callbacks opcionales explícitamente
defmodule MiBehaviour do
  @callback requerido(t :: term()) :: :ok
  @callback opcional(t :: term()) :: :ok
  @optional_callbacks [opcional: 1]
end
```

### Mistake 4: Usar behaviours cuando basta con pasar funciones
```elixir
# ❌ Overhead innecesario de behaviour cuando solo necesitas una función
defmodule Transformador do
  @callback transformar(t :: term()) :: term()
end

# ✓ Para casos simples, pasar la función directamente es más idiomático en Elixir
def aplicar_transformacion(datos, transformar_fn) do
  Enum.map(datos, transformar_fn)
end

# Behaviours brillan cuando el módulo implementador tiene múltiples funciones
# relacionadas y estado propio — como GenServer, Notificador con 3+ callbacks, etc.
```

## Verification
```bash
$ iex
iex> NotificadorEmail.notificar("Test")
[Email] Enviando: Test
:ok
iex> SistemaAlertas.enviar(NotificadorEmail, "Alerta!")
[Email] Enviando: Alerta!
:ok
iex> SistemaAlertas.enviar(String, "No funciona")
{:error, :not_a_notificador}
iex> notificadores = [NotificadorEmail, NotificadorSMS, NotificadorSlack]
iex> SistemaAlertas.enviar_a_todos(notificadores, "Sistema caído")
# => %{email: :ok, sms: :ok, slack: :ok}
iex> Notificador.implementa?(NotificadorEmail)
true
iex> Notificador.implementa?(Integer)
false
```

Checklist de verificación:
- [ ] `@callback` define correctamente la firma con tipos de argumentos y retorno
- [ ] Cada implementación declara `@behaviour NombreDelBehaviour`
- [ ] Cada callback implementado está marcado con `@impl NombreDelBehaviour`
- [ ] El compilador emite advertencia si falta un callback (probar eliminando uno)
- [ ] `SistemaAlertas.enviar/2` funciona con cualquier módulo que implemente Notificador
- [ ] `SistemaAlertas.enviar_a_todos/2` retorna resultados de todos los canales
- [ ] `ProcesadorMock` implementa los 3 callbacks de `ProcesadorPago`
- [ ] `ServicioPagos.procesar_compra/3` usa el procesador de forma genérica

## Summary
- `@callback` en un módulo define el contrato que los implementadores deben cumplir
- `@behaviour NombreDelBehaviour` en un módulo declara que lo implementará
- `@impl NombreDelBehaviour` activa la verificación en compile time — el compilador avisa si falta un callback o si el nombre está mal escrito
- Los behaviours permiten polimorfismo: escribir código genérico que funciona con cualquier implementación
- `@optional_callbacks` marca callbacks que no son obligatorios implementar
- GenServer, Supervisor, Application, y Ecto.Repo son behaviours de la plataforma
- La diferencia con Protocols: behaviours son para módulos (dispatch estático), Protocols son para tipos de datos (dispatch dinámico)

## What's Next
¡Felicitaciones por completar el nivel Intermedio! Has dominado los conceptos clave de OTP: procesos, Agent, Task, GenServer, Supervisor, Application, y Behaviours. El siguiente nivel abordará:
- **Phoenix Framework**: web development con Elixir
- **Ecto**: interacción con bases de datos
- **LiveView**: interfaces reactivas en tiempo real
- **Testing avanzado**: ExUnit, mocking, property-based testing

## Resources
- [Behaviours — HexDocs](https://hexdocs.pm/elixir/behaviours.html)
- [Module @callback — HexDocs](https://hexdocs.pm/elixir/Module.html#module-behaviour)
- [Typespecs y @callback — HexDocs](https://hexdocs.pm/elixir/typespecs.html)
- [Elixir School: Behaviours](https://elixirschool.com/en/lessons/advanced/behaviours)
