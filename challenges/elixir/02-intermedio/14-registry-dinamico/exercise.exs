# =============================================================================
# Ejercicio 14: Registry Dinámico — Lookup de Procesos por Nombre
# Difficulty: Intermedio
# =============================================================================

# -----------------------------------------------------------------------------
# Prerequisites
# -----------------------------------------------------------------------------
# - GenServer básico (start_link, call, cast, handle_call)
# - Supervisors básicos
# - PIDs y procesos Elixir
# - ETS básico (ejercicio anterior, ya que Registry usa ETS internamente)

# -----------------------------------------------------------------------------
# Learning Objectives
# -----------------------------------------------------------------------------
# Al completar este ejercicio podrás:
# 1. Iniciar un Registry con Registry.start_link/1
# 2. Registrar procesos con nombres dinámicos usando Registry.register/3
# 3. Buscar procesos por nombre con Registry.lookup/2
# 4. Usar via_tuple para nombrar GenServers dinámicamente
# 5. Verificar que Registry limpia entradas automáticamente al morir un proceso

# -----------------------------------------------------------------------------
# Concepts
# -----------------------------------------------------------------------------
#
# ¿QUÉ ES REGISTRY?
# Registry es un módulo OTP incluido en Elixir (desde 1.4) que implementa
# un mapa de nombres a PIDs, con limpieza automática cuando el proceso muere.
#
# A diferencia de nombres globales con Process.register/2 (que solo acepta
# átomos y un nombre por proceso), Registry permite:
# - Nombres dinámicos: cualquier término (strings, tuplas, etc.)
# - Múltiples procesos con el mismo nombre en :duplicate mode
# - Metadatos asociados a cada registro
# - Cleanup automático: cuando el proceso muere, su entrada desaparece
#
# TIPOS DE REGISTRY:
#   :unique    — un solo PID por nombre (uno-a-uno)
#   :duplicate — múltiples PIDs por nombre (pub/sub, grupos)
#
# CREAR UN REGISTRY:
#   Registry.start_link(keys: :unique, name: MyRegistry)
#   # En un Supervisor:
#   children = [{Registry, keys: :unique, name: MyRegistry}]
#
# REGISTRAR UN PROCESO:
#   # Desde dentro del proceso que quiere registrarse:
#   Registry.register(MyRegistry, "nombre_clave", valor_meta)
#   # => {:ok, owner_pid}
#
# BUSCAR UN PROCESO:
#   Registry.lookup(MyRegistry, "nombre_clave")
#   # => [{pid, valor_meta}]  — lista vacía si no existe
#
# DESREGISTRAR:
#   Registry.unregister(MyRegistry, "nombre_clave")
#
# VIA TUPLE — nombrar GenServers dinámicamente:
#
#   defmodule MyServer do
#     def start_link(name) do
#       GenServer.start_link(__MODULE__, name, name: via_name(name))
#     end
#
#     defp via_name(name) do
#       {:via, Registry, {MyRegistry, name}}
#     end
#
#     # Llamar al servidor por nombre (sin conocer el PID):
#     def my_call(name, arg) do
#       GenServer.call(via_name(name), {:do_something, arg})
#     end
#   end
#
# Con via tuple, GenServer.call/cast/stop usan Registry para resolver
# el PID automáticamente.

# =============================================================================
# Setup — Registry que usaremos en los ejercicios
# =============================================================================
# NOTA: En un script .exs, iniciamos el Registry manualmente.
# En una aplicación OTP real, iría en el árbol de supervisión.

defmodule ExerciseRegistry do
  @registry_name :exercise_registry

  def start do
    case Registry.start_link(keys: :unique, name: @registry_name) do
      {:ok, pid} -> {:ok, pid}
      {:error, {:already_started, pid}} -> {:ok, pid}
    end
  end

  def name, do: @registry_name
end

# =============================================================================
# Exercise 1: Operaciones básicas de Registry
# =============================================================================
#
# Completa el módulo RegistryBasic con funciones que envuelven las
# operaciones de Registry.
#
# `register/2`: registra el proceso actual bajo `name` con metadata vacía
# `lookup/1`: busca el PID registrado bajo `name`
#             Retorna {:ok, pid} o :not_found
# `unregister/1`: desregistra el nombre del proceso actual
#
# NOTA: Registry.register solo puede ser llamado desde el proceso que
# quiere registrarse (asocia el nombre al proceso llamante).

defmodule RegistryBasic do
  @registry ExerciseRegistry.name()

  def register(name, meta \\ %{}) do
    # TODO: Registry.register(@registry, name, meta)
    # Retorna {:ok, owner_pid} en éxito
  end

  def lookup(name) do
    # TODO:
    # case Registry.lookup(@registry, name) do
    #   [{pid, _meta}] -> {:ok, pid}
    #   []             -> :not_found
    # end
  end

  def unregister(name) do
    # TODO: Registry.unregister(@registry, name)
  end
end

# =============================================================================
# Exercise 2: Registrar con metadata — enriquecer el registro
# =============================================================================
#
# Registry permite asociar metadatos arbitrarios al registrar.
# Esto es útil para almacenar info sobre el proceso sin un lookup adicional.
#
# Completa MetaRegistry:
#
# `register_worker/2`: registra el proceso bajo `name` con meta = %{role: role, started_at: now}
#   donde `now` es System.monotonic_time(:second)
#
# `lookup_with_meta/1`: retorna {:ok, pid, meta} o :not_found
#   (incluye la metadata en el resultado)
#
# `list_all/0`: retorna una lista de {name, pid, meta} para todos los registros
#   Tip: Registry.select/2 — pero también puedes usar Registry.lookup en lista conocida
#   O más sencillo: guarda los nombres en un ETS auxiliar al registrar

defmodule MetaRegistry do
  @registry ExerciseRegistry.name()

  def register_worker(name, role) do
    meta = %{role: role, started_at: System.monotonic_time(:second)}
    # TODO: Registry.register(@registry, name, meta)
  end

  def lookup_with_meta(name) do
    # TODO:
    # case Registry.lookup(@registry, name) do
    #   [{pid, meta}] -> {:ok, pid, meta}
    #   []            -> :not_found
    # end
  end
end

# =============================================================================
# Exercise 3: Lookup básico — verificar existencia
# =============================================================================
#
# Completa ProcessChecker:
#
# `alive?/1`: retorna true si hay un proceso registrado bajo `name`
#
# `registered_names/0`: dado que en :unique registry no hay una función
#   directa para listar todos los nombres, usa la siguiente aproximación:
#   Registry.select(@registry, [{{:"$1", :"$2", :"$3"}, [], [{{:"$1", :"$2"}}]}])
#   Esto retorna [{name, pid}] para todas las entradas.
#
# `count/0`: cuántos procesos están registrados actualmente

defmodule ProcessChecker do
  @registry ExerciseRegistry.name()

  def alive?(name) do
    # TODO: Registry.lookup(@registry, name) != []
  end

  def registered_names do
    # TODO: Registry.select(@registry, [{{:"$1", :"$2", :"$3"}, [], [:"$1"]}])
    # Retorna lista de nombres registrados
  end

  def count do
    # TODO: Registry.count(@registry)
  end
end

# =============================================================================
# Exercise 4: Via tuple — GenServer nombrado dinámicamente
# =============================================================================
#
# Implementa un GenServer simple (Counter) que se inicia con un nombre
# dinámico y se accede usando via_tuple de Registry.
#
# `start_link/1`: inicia el GenServer registrado bajo `name` en el Registry
# `increment/1`: incrementa el contador del GenServer con ese nombre
# `value/1`: retorna el valor actual del contador
# `via/1`: retorna el via_tuple para el nombre dado (función privada o pública)
#
# La clave es que NUNCA necesitas el PID — usas el nombre para todas las ops.
#
# Ejemplo de uso:
#   Counter.start_link("room_42")
#   Counter.increment("room_42")
#   Counter.increment("room_42")
#   Counter.value("room_42")  # => 2

defmodule Counter do
  use GenServer

  @registry ExerciseRegistry.name()

  # --- Client API ---

  def start_link(name) do
    # TODO: GenServer.start_link(__MODULE__, 0, name: via(name))
  end

  def increment(name) do
    # TODO: GenServer.cast(via(name), :increment)
  end

  def value(name) do
    # TODO: GenServer.call(via(name), :value)
  end

  # TODO: definir via/1 como función privada
  # defp via(name), do: {:via, Registry, {@registry, name}}

  # --- Server Callbacks ---

  @impl true
  def init(initial_value) do
    {:ok, initial_value}
  end

  @impl true
  def handle_cast(:increment, count) do
    # TODO: {:noreply, count + 1}
  end

  @impl true
  def handle_call(:value, _from, count) do
    # TODO: {:reply, count, count}
  end
end

# =============================================================================
# Exercise 5: Cleanup automático — Registry y procesos que mueren
# =============================================================================
#
# Una de las ventajas clave de Registry sobre ETS manual es que cuando
# un proceso muere, su entrada se elimina AUTOMÁTICAMENTE.
#
# Completa ProcessLifecycle para demostrar este comportamiento:
#
# `spawn_registered/1`: spawnea un proceso que:
#   1. Llama a Registry.register para registrarse bajo `name`
#   2. Espera un mensaje :stop (receive do :stop -> :ok end)
#   Retorna el PID del proceso spawneado.
#
# `kill_and_verify_cleanup/2`: dado `name` y `pid`:
#   1. Verifica que el nombre está registrado (assert)
#   2. Mata el proceso con Process.exit(pid, :kill)
#   3. Espera un momento para que el Registry procese la muerte
#   4. Verifica que el nombre YA NO está registrado
#   Retorna {:ok, :cleaned_up} o {:error, :still_registered}

defmodule ProcessLifecycle do
  @registry ExerciseRegistry.name()

  def spawn_registered(name) do
    # TODO: spawn un proceso que se registra y espera :stop
    # spawn(fn ->
    #   Registry.register(@registry, name, %{})
    #   receive do
    #     :stop -> :ok
    #   end
    # end)
  end

  def kill_and_verify_cleanup(name, pid) do
    # Verificar que está registrado antes de matar
    pre_check = Registry.lookup(@registry, name) != []

    # Matar el proceso
    Process.exit(pid, :kill)

    # TODO: Esperar a que el Registry procese la muerte del proceso
    # Process.sleep(50) — suficiente para que el monitor de Registry actúe

    # TODO: Verificar que ya no está registrado
    # post_check = Registry.lookup(@registry, name) == []
    # case {pre_check, post_check} do
    #   {true, true}  -> {:ok, :cleaned_up}
    #   {true, false} -> {:error, :still_registered}
    #   _             -> {:error, :unexpected_state}
    # end
  end
end

# =============================================================================
# Verification — Ejecuta con: elixir exercise.exs
# =============================================================================

defmodule RegistryTests do
  def run do
    # Iniciar el Registry global para los ejercicios
    {:ok, _} = ExerciseRegistry.start()

    IO.puts("\n=== Verificación: Registry Dinámico ===\n")

    # Ejercicio 1: Básico
    IO.puts("  Ejercicio 1 — Operaciones básicas:")
    worker1_pid = spawn(fn ->
      RegistryBasic.register("worker:1")
      receive do :stop -> :ok end
    end)
    Process.sleep(20)

    check("lookup existente",    RegistryBasic.lookup("worker:1"),   {:ok, worker1_pid})
    check("lookup no existente", RegistryBasic.lookup("worker:99"),  :not_found)

    send(worker1_pid, :stop)
    Process.sleep(30)
    check("lookup tras muerte proceso", RegistryBasic.lookup("worker:1"), :not_found)

    IO.puts("")

    # Ejercicio 2: Metadata
    IO.puts("  Ejercicio 2 — Registro con metadata:")
    _w2 = spawn(fn ->
      MetaRegistry.register_worker("worker:meta", :processor)
      receive do :stop -> :ok end
    end)
    Process.sleep(20)

    result2 = MetaRegistry.lookup_with_meta("worker:meta")
    check("lookup_with_meta retorna 3 elementos", match?({:ok, _, %{role: :processor}}, result2), true)
    check("metadata tiene campo role", elem(result2, 2)[:role], :processor)
    check("metadata tiene campo started_at", Map.has_key?(elem(result2, 2), :started_at), true)

    IO.puts("")

    # Ejercicio 3: ProcessChecker
    IO.puts("  Ejercicio 3 — Verificar existencia:")
    _w3 = spawn(fn ->
      Registry.register(ExerciseRegistry.name(), "alive_check", %{})
      receive do :stop -> :ok end
    end)
    Process.sleep(20)

    check("alive? true para registrado", ProcessChecker.alive?("alive_check"), true)
    check("alive? false para no registrado", ProcessChecker.alive?("no_existe"), false)
    check("count >= 1", ProcessChecker.count() >= 1, true)

    IO.puts("")

    # Ejercicio 4: Via tuple GenServer
    IO.puts("  Ejercicio 4 — Via tuple GenServer:")
    {:ok, _} = Counter.start_link("counter_a")
    {:ok, _} = Counter.start_link("counter_b")

    Counter.increment("counter_a")
    Counter.increment("counter_a")
    Counter.increment("counter_a")
    Counter.increment("counter_b")

    check("counter_a value = 3", Counter.value("counter_a"), 3)
    check("counter_b value = 1", Counter.value("counter_b"), 1)
    check("counters son independientes", Counter.value("counter_a") != Counter.value("counter_b"), true)

    IO.puts("")

    # Ejercicio 5: Cleanup automático
    IO.puts("  Ejercicio 5 — Cleanup automático:")
    short_lived_pid = ProcessLifecycle.spawn_registered("short_lived")
    Process.sleep(20)

    check("proceso registrado antes de kill",
          Registry.lookup(ExerciseRegistry.name(), "short_lived") != [], true)

    result5 = ProcessLifecycle.kill_and_verify_cleanup("short_lived", short_lived_pid)
    check("cleanup automático tras muerte", result5, {:ok, :cleaned_up})

    IO.puts("\n=== Verificación completada ===")
  end

  defp check(label, actual, expected) do
    if actual == expected do
      IO.puts("  ✓ #{label}")
    else
      IO.puts("  ✗ #{label}")
      IO.puts("    Esperado: #{inspect(expected)}")
      IO.puts("    Obtenido: #{inspect(actual)}")
    end
  end
end

# =============================================================================
# Common Mistakes
# =============================================================================
#
# ERROR 1: Registry.register no puede llamarse desde el proceso equivocado
#
#   # Desde el proceso A:
#   pid_b = spawn(fn -> ... end)
#   Registry.register(MyRegistry, "name", %{})
#   # => Registra el proceso A, no el B
#
#   Registry.register siempre registra el proceso LLAMANTE (self()).
#   Para registrar B, B debe llamar register por sí mismo.
#
# ERROR 2: Olvidar iniciar el Registry antes de usarlo
#
#   Registry.lookup(MyRegistry, "key")
#   # => ** (ArgumentError) argument error  — Registry no iniciado
#
#   En scripts: Registry.start_link(...)
#   En aplicaciones: añadir al árbol de supervisión.
#
# ERROR 3: Nombre del Registry no es el atom/PID correcto
#
#   {:via, Registry, {MyRegistry, name}}  # MyRegistry debe ser el NOMBRE
#   # Si Registry se inició con: name: :my_reg
#   # Entonces: {:via, Registry, {:my_reg, name}}
#
# ERROR 4: No esperar después de matar un proceso para verificar cleanup
#
#   Process.exit(pid, :kill)
#   Registry.lookup(...)  # puede aún retornar el PID (race condition)
#
#   Añadir Process.sleep(50) o ref + monitor para esperar el DOWN signal.
#
# ERROR 5: Usar Registry como store de datos (no es para eso)
#
#   Registry es para NOMBRES de procesos, no para almacenar datos arbitrarios.
#   La metadata es auxiliar, pequeña. Para datos → ETS, GenServer, o BD.

# =============================================================================
# Summary
# =============================================================================
#
# - Registry provee lookup dinámico de procesos por nombre arbitrario
# - :unique → un PID por nombre; :duplicate → múltiples PIDs (pub/sub)
# - Registry.register asocia el proceso LLAMANTE al nombre
# - via_tuple integra Registry con GenServer.start_link/call/cast/stop
# - El cleanup es automático: el proceso muere → la entrada desaparece
# - Ideal para sistemas dinámicos: salas de chat, sesiones, workers pool

# =============================================================================
# What's Next
# =============================================================================
# - Explorar: Registry con keys: :duplicate para pub/sub básico
# - Explorar: DynamicSupervisor + Registry para workers con nombres
# - Explorar: Phoenix.PubSub (construido sobre Registry en algunos backends)
# - Siguiente nivel: GenServer avanzado, supervisión dinámica

# =============================================================================
# Resources
# =============================================================================
# - https://hexdocs.pm/elixir/Registry.html
# - https://elixir-lang.org/blog/2017/01/05/elixir-v1-4-0-released/ (Registry intro)
# - Elixir in Action, Cap. 12 — Building a distributed system

# =============================================================================
# Try It Yourself (sin solución)
# =============================================================================
#
# Implementa un sistema de chat rooms donde cada room es un GenServer
# nombrado dinámicamente vía Registry.
#
# El sistema debe tener:
#
#   ChatRoom.create(room_name)
#   # Inicia un GenServer registrado bajo room_name en el Registry
#
#   ChatRoom.join(room_name, user_name)
#   # Agrega user_name a la lista de miembros del room
#   # Retorna :ok o {:error, :room_not_found}
#
#   ChatRoom.send_message(room_name, user_name, message)
#   # Agrega {user_name, message, timestamp} al historial del room
#   # Retorna :ok o {:error, :user_not_in_room}
#
#   ChatRoom.history(room_name)
#   # Retorna el historial de mensajes del room
#
#   ChatRoom.leave(room_name, user_name)
#   # Elimina user_name de la lista de miembros
#
#   ChatRoom.close(room_name)
#   # Detiene el GenServer y libera el nombre en el Registry
#
# Demuestra que dos rooms distintas son GenServers completamente
# independientes con su propio estado.

RegistryTests.run()
