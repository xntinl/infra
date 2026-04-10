# 5. Supervisor: Tolerancia a Fallos

**Difficulty**: Intermedio

## Prerequisites
- Completed 01-basico exercises
- Completed 04-genserver-basico
- Understanding of processes, GenServer, and OTP behaviours
- Familiarity with child specs and module structure

## Learning Objectives
After completing this exercise, you will be able to:
- Define and start a `Supervisor` with a list of child specifications
- Understand and apply the `:one_for_one`, `:one_for_all`, and `:rest_for_one` strategies
- Configure `max_restarts` and `max_seconds` para controlar el límite de reinicios
- Register workers con nombres y accederlos sin PIDs
- Construir un árbol de supervisión simple con Supervisor e hijos GenServer

## Concepts

### Supervisor: el guardián de los procesos
Un Supervisor es un proceso especial cuya única responsabilidad es monitorear a sus procesos hijos y reiniciarlos si mueren inesperadamente. Esta es la materialización del principio "let it crash": en vez de escribir código defensivo que intenta anticipar todos los fallos posibles, dejas que el proceso falle y confías en que el Supervisor lo reiniciará en un estado conocido y limpio.

El Supervisor no necesita saber qué hizo mal el proceso hijo — simplemente lo reinicia. Si el problema era transitorio (un spike de carga, una conexión de red temporalmente caída), el proceso reiniciado funcionará bien. Si el problema es persistente y el proceso sigue fallando, el Supervisor lo detectará (a través de `max_restarts`) y se detendrá a sí mismo, escalando el fallo hacia arriba en el árbol de supervisión.

```elixir
defmodule MiApp.Supervisor do
  use Supervisor

  def start_link(_opts) do
    Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl Supervisor
  def init(:ok) do
    children = [
      MiApp.CounterServer,       # child spec via __MODULE__.child_spec/1
      {MiApp.KVStore, [tabla: :cache]},  # child spec con argumentos
    ]
    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

### Estrategias de reinicio
La estrategia define qué pasa con los otros hijos cuando uno falla:

**`:one_for_one`** (la más común): Solo se reinicia el hijo que falló. Los otros siguen sin verse afectados. Úsala cuando los hijos son independientes entre sí.

**`:one_for_all`**: Si un hijo falla, TODOS los hijos se reinician. Úsala cuando los hijos tienen dependencias fuertes entre sí y no tienen sentido corriendo sin el otro.

**`:rest_for_one`**: Si el hijo N falla, se reinician el hijo N y todos los hijos que fueron iniciados DESPUÉS de N. Úsala cuando hay una cadena de dependencias lineales (B depende de A, C depende de B).

```
Hijos en orden: [A, B, C, D]

:one_for_one  — si C falla: solo C se reinicia
:one_for_all  — si C falla: A, B, C y D se reinician
:rest_for_one — si C falla: C y D se reinician (A y B continúan)
```

### Child spec: cómo describir un hijo
Cada hijo del Supervisor se describe con una "child spec" — un mapa que dice cómo iniciarlo, cuándo reiniciarlo, y qué tipo de proceso es. Cuando usas `use GenServer`, el módulo automáticamente define `child_spec/1` con valores por defecto razonables.

```elixir
# Forma corta — usa el child_spec/1 del módulo con args vacíos
children = [MiWorker]

# Forma con argumentos
children = [{MiWorker, [host: "localhost", puerto: 5432]}]

# Forma completa y explícita (cuando necesitas personalizar)
children = [
  %{
    id: MiWorker,
    start: {MiWorker, :start_link, [[]]},
    restart: :permanent,     # :permanent | :temporary | :transient
    shutdown: 5000,          # ms para graceful shutdown
    type: :worker            # :worker | :supervisor
  }
]
```

### max_restarts y max_seconds
El Supervisor lleva la cuenta de cuántas veces se ha reiniciado cada hijo. Si un hijo se reinicia más de `max_restarts` veces dentro de `max_seconds` segundos, el Supervisor concluye que el problema no es transitorio y se detiene a sí mismo (propagando el fallo hacia arriba). Los valores por defecto son 3 reinicios en 5 segundos.

```elixir
Supervisor.init(children, strategy: :one_for_one, max_restarts: 5, max_seconds: 10)
```

## Exercises

### Exercise 1: Supervisor simple con un GenServer
```elixir
# Primero necesitamos un GenServer que podamos supervisar
defmodule ContadorWorker do
  use GenServer

  def start_link(opts \\ []) do
    nombre = Keyword.get(opts, :name, __MODULE__)
    GenServer.start_link(__MODULE__, 0, name: nombre)
  end

  def get(nombre \\ __MODULE__), do: GenServer.call(nombre, :get)
  def incrementar(nombre \\ __MODULE__), do: GenServer.cast(nombre, :inc)
  def crash!(nombre \\ __MODULE__), do: GenServer.cast(nombre, :crash)

  @impl GenServer
  def init(estado), do: {:ok, estado}

  @impl GenServer
  def handle_call(:get, _from, estado), do: {:reply, estado, estado}

  @impl GenServer
  def handle_cast(:inc, estado), do: {:noreply, estado + 1}

  @impl GenServer
  # Para demostrar reinicios del supervisor
  def handle_cast(:crash, _estado), do: raise "¡Crash intencional para demostrar reinicio!"
end

# TODO: Implementa el supervisor
defmodule SupervisorSimple do
  use Supervisor

  # TODO: Implementa `start_link/1` que inicia el supervisor con nombre
  def start_link(_opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl Supervisor
  # TODO: Implementa `init/1`:
  # Define children = [ContadorWorker]
  # Llama Supervisor.init(children, strategy: :one_for_one)
  def init(:ok) do
    children = [
      # TODO: agregar ContadorWorker como hijo
    ]
    Supervisor.init(children, strategy: :one_for_one)
  end
end

# Test it (en IEx):
# SupervisorSimple.start_link()
# ContadorWorker.incrementar()
# ContadorWorker.incrementar()
# ContadorWorker.get()                  # => 2
# ContadorWorker.crash!()               # El proceso muere
# :timer.sleep(100)                     # Esperar al reinicio
# ContadorWorker.get()                  # => 0 (reiniciado con estado fresco)
# Supervisor.which_children(SupervisorSimple)   # Ver estado de los hijos
```

### Exercise 2: Múltiples workers independientes
```elixir
defmodule AppSupervisor do
  use Supervisor

  def start_link(_opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl Supervisor
  # TODO: Implementa `init/1` con 3 workers independientes:
  # 1. Un ContadorWorker con nombre :contador_a
  # 2. Un ContadorWorker con nombre :contador_b
  # 3. Un ContadorWorker con nombre :contador_c
  # PISTA: Para pasar argumentos a un worker se usa {Modulo, argumentos}
  # ContadorWorker.start_link acepta [name: :nombre_atom]
  # Cuando dos workers son del mismo módulo, el :id debe ser diferente:
  # %{id: :contador_a, start: {ContadorWorker, :start_link, [[name: :contador_a]]}}
  def init(:ok) do
    children = [
      # TODO: tres workers con diferentes nombres
      # Recuerda: ids únicos para workers del mismo módulo
    ]
    Supervisor.init(children, strategy: :one_for_one)
  end

  # Helper para inspeccionar el estado del supervisor
  def estado do
    Supervisor.which_children(__MODULE__)
  end
end

# Test it:
# AppSupervisor.start_link()
# ContadorWorker.incrementar(:contador_a)
# ContadorWorker.incrementar(:contador_a)
# ContadorWorker.get(:contador_a)       # => 2
# ContadorWorker.incrementar(:contador_b)
# ContadorWorker.get(:contador_b)       # => 1
# ContadorWorker.crash!(:contador_a)    # Solo contador_a se reinicia
# :timer.sleep(100)
# ContadorWorker.get(:contador_a)       # => 0 (reiniciado)
# ContadorWorker.get(:contador_b)       # => 1 (no afectado con :one_for_one)
# AppSupervisor.estado()
```

### Exercise 3: Estrategia :one_for_all
```elixir
defmodule WorkerDependiente do
  # Un worker que "depende" de una referencia compartida
  # Cuando uno falla, todos necesitan reiniciarse para re-sincronizarse
  use GenServer

  def start_link(opts) do
    nombre = Keyword.fetch!(opts, :name)
    GenServer.start_link(__MODULE__, nombre, name: nombre)
  end

  def get(nombre), do: GenServer.call(nombre, :get)
  def crash!(nombre), do: GenServer.cast(nombre, :crash)

  @impl GenServer
  def init(nombre) do
    IO.puts("#{nombre} iniciado")
    {:ok, %{nombre: nombre, estado: :activo}}
  end

  @impl GenServer
  def handle_call(:get, _from, estado), do: {:reply, estado, estado}

  @impl GenServer
  def handle_cast(:crash, estado) do
    IO.puts("#{estado.nombre} fallando...")
    raise "Fallo en #{estado.nombre}"
  end

  @impl GenServer
  def terminate(reason, estado) do
    IO.puts("#{estado.nombre} terminando (reason: #{inspect(reason)})")
    :ok
  end
end

defmodule SupervisorOneForAll do
  use Supervisor

  def start_link(_opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl Supervisor
  # TODO: Implementa `init/1` con 3 WorkerDependiente bajo estrategia :one_for_all:
  # workers con nombres :dep_a, :dep_b, :dep_c
  # OBSERVA: cuando uno falla, todos se reinician (verás los mensajes de init)
  def init(:ok) do
    children = [
      # TODO: tres WorkerDependiente con nombres :dep_a, :dep_b, :dep_c
    ]
    # TODO: Supervisor.init con strategy: :one_for_all
  end
end

# Test it (observa los mensajes de init y terminate):
# SupervisorOneForAll.start_link()
# WorkerDependiente.crash!(:dep_b)   # Todos se reinician (no solo :dep_b)
# :timer.sleep(200)
# WorkerDependiente.get(:dep_a)      # Reiniciado también (estado fresco)
```

### Exercise 4: max_restarts — supervisor que se detiene
```elixir
defmodule WorkerInestable do
  # Un worker que SIEMPRE falla al iniciarse (para demostrar max_restarts)
  use GenServer

  @intentos_globales :contador_intentos

  def start_link(_opts \\ []) do
    GenServer.start_link(__MODULE__, :ok)
  end

  @impl GenServer
  def init(:ok) do
    contador = :persistent_term.get(@intentos_globales, 0) + 1
    :persistent_term.put(@intentos_globales, contador)
    IO.puts("Intento de inicio ##{contador}...")
    # Falla siempre para demostrar que el supervisor se agota
    {:stop, :siempre_fallo}
  end
end

defmodule SupervisorLimiteReinicios do
  use Supervisor

  def start_link(max_restarts, max_seconds) do
    Supervisor.start_link(__MODULE__, {max_restarts, max_seconds}, name: __MODULE__)
  end

  @impl Supervisor
  # TODO: Implementa `init/1` que recibe {max_restarts, max_seconds}:
  # Usa un WorkerInestable como hijo
  # Configura max_restarts y max_seconds en Supervisor.init/2
  # OBSERVA: el supervisor intenta reiniciar N veces y luego se rinde
  def init({max_restarts, max_seconds}) do
    children = [
      # TODO: WorkerInestable como hijo
    ]
    # TODO: Supervisor.init con strategy, max_restarts, y max_seconds
  end
end

# Test it:
# {:error, reason} = SupervisorLimiteReinicios.start_link(3, 5)
# IO.puts("El supervisor falló: #{inspect(reason)}")
# (Verás 3-4 intentos de inicio antes de que el supervisor se rinda)
```

### Exercise 5: Workers con nombre explícito en child spec
```elixir
defmodule PoolSupervisor do
  # Un supervisor que gestiona un "pool" de 3 workers idénticos
  # Cada uno tiene un ID y nombre únicos para poder accederlos individualmente
  use Supervisor

  @worker_count 3

  def start_link(_opts \\ []) do
    Supervisor.start_link(__MODULE__, :ok, name: __MODULE__)
  end

  @impl Supervisor
  # TODO: Implementa `init/1` que crea @worker_count ContadorWorker:
  # Genera dinámicamente los child specs con nombres :worker_1, :worker_2, :worker_3
  # Usa Enum.map(1..@worker_count, fn n -> child_spec_para(n) end) para generarlos
  def init(:ok) do
    children = Enum.map(1..@worker_count, fn n ->
      # TODO: construir child spec para worker N
      # El ID debe ser :"worker_#{n}"
      # El nombre del proceso también debe ser :"worker_#{n}"
      # PISTA: %{id: :"worker_#{n}", start: {ContadorWorker, :start_link, [[name: :"worker_#{n}"]]}}
    end)
    Supervisor.init(children, strategy: :one_for_one)
  end

  # Helper: obtener el nombre de un worker por índice
  def worker_name(n), do: :"worker_#{n}"

  # Helper: incrementar el worker N
  def incrementar(n), do: ContadorWorker.incrementar(worker_name(n))

  # Helper: get del worker N
  def get(n), do: ContadorWorker.get(worker_name(n))

  # TODO: Implementa `estado_todos/0` que retorna el estado de todos los workers:
  # Retorna %{worker_1: v1, worker_2: v2, worker_3: v3}
  def estado_todos do
    Enum.into(1..@worker_count, %{}, fn n ->
      # TODO: {worker_name(n), ContadorWorker.get(worker_name(n))}
    end)
  end
end

# Test it:
# PoolSupervisor.start_link()
# PoolSupervisor.incrementar(1)
# PoolSupervisor.incrementar(1)
# PoolSupervisor.incrementar(2)
# PoolSupervisor.estado_todos()    # => %{worker_1: 2, worker_2: 1, worker_3: 0}
# ContadorWorker.crash!(PoolSupervisor.worker_name(2))
# :timer.sleep(100)
# PoolSupervisor.estado_todos()    # => %{worker_1: 2, worker_2: 0, worker_3: 0}
```

### Try It Yourself
Construye un árbol de supervisión de dos niveles: un Supervisor raíz que supervisa otros Supervisors (sub-supervisores), cada uno gestionando sus propios workers.

Estructura:
```
AppRootSupervisor (:one_for_one)
├── WorkersSupervisor (:one_for_all, 2 ContadorWorker)
└── ServicesSupervisor (:one_for_one, 1 KVStore)
```

Requisitos:
- `AppRootSupervisor` supervisa a `WorkersSupervisor` y `ServicesSupervisor`
- `WorkersSupervisor` usa `:one_for_all` con 2 `ContadorWorker` (`:worker_a`, `:worker_b`)
- `ServicesSupervisor` usa `:one_for_one` con 1 `KVStore`
- El tipo de cada sub-supervisor en el child spec debe ser `:supervisor`, no `:worker`
- Demostrar que si crashea `:worker_a`, `:worker_b` también se reinicia (`:one_for_all`)
- Demostrar que crashear un worker no afecta a `KVStore` (supervisores separados)

```elixir
defmodule WorkersSupervisor do
  use Supervisor
  # Tu implementación aquí
end

defmodule ServicesSupervisor do
  use Supervisor
  # Tu implementación aquí
end

defmodule AppRootSupervisor do
  use Supervisor
  # Tu implementación aquí
  # Tip: el child spec de un Supervisor hijo debe incluir type: :supervisor
end
```

## Common Mistakes

### Mistake 1: Mismo :id para dos hijos del mismo módulo
```elixir
# ❌ Dos workers del mismo módulo tienen el mismo :id por defecto → error
children = [ContadorWorker, ContadorWorker]
# ** (ArgumentError) duplicate child id ContadorWorker

# ✓ Especificar :id único para cada hijo cuando hay duplicados del mismo módulo
children = [
  %{id: :contador_a, start: {ContadorWorker, :start_link, [[name: :contador_a]]}},
  %{id: :contador_b, start: {ContadorWorker, :start_link, [[name: :contador_b]]}}
]
```

### Mistake 2: Confundir restart: :permanent vs :temporary vs :transient
```elixir
# :permanent (defecto) — siempre reinicia, sin importar la razón del fallo
# :temporary — NUNCA reinicia (fire-and-forget tasks)
# :transient — solo reinicia si el proceso terminó anormalmente (no con :normal)

# Para un worker de background que no debe reiniciarse:
%{id: MiTask, start: {...}, restart: :temporary}
```

### Mistake 3: max_restarts demasiado alto enmascara bugs
```elixir
# ❌ Con max_restarts muy alto, un proceso buggy se reinicia cientos de veces
# antes de que el supervisor se dé por vencido
Supervisor.init(children, strategy: :one_for_one, max_restarts: 100)

# ✓ Valores conservadores para detectar fallos rápido en desarrollo
Supervisor.init(children, strategy: :one_for_one, max_restarts: 3, max_seconds: 5)
```

### Mistake 4: Usar :one_for_all cuando los procesos son independientes
```elixir
# ❌ Con :one_for_all, un fallo en WorkerA reinicia WorkerB aunque no esté relacionado
children = [WorkerA, WorkerB]
Supervisor.init(children, strategy: :one_for_all)

# ✓ Usar :one_for_one para workers independientes
Supervisor.init(children, strategy: :one_for_one)

# :one_for_all solo tiene sentido cuando los workers comparten estado o
# dependen fuertemente entre sí (ej: servidor de BD + pool de conexiones)
```

## Verification
```bash
$ iex
iex> SupervisorSimple.start_link()
{:ok, #PID<0.115.0>}
iex> ContadorWorker.get()
0
iex> ContadorWorker.crash!()
:ok
iex> :timer.sleep(100)
:ok
iex> ContadorWorker.get()
0   # Reiniciado automáticamente
iex> Supervisor.which_children(SupervisorSimple)
[{ContadorWorker, #PID<0.117.0>, :worker, [ContadorWorker]}]
```

Checklist de verificación:
- [ ] El Supervisor inicia y `Supervisor.which_children/1` muestra los hijos
- [ ] Cuando un hijo falla, el Supervisor lo reinicia automáticamente
- [ ] Con `:one_for_one`, solo el hijo que falló se reinicia
- [ ] Con `:one_for_all`, todos los hijos se reinician cuando uno falla
- [ ] `max_restarts` limita los reinicios y el Supervisor se detiene al agotarlos
- [ ] Workers con nombres diferentes del mismo módulo requieren `:id` único
- [ ] El árbol de dos niveles funciona: Supervisor supervisa Supervisors

## Summary
- El Supervisor monitorea y reinicia procesos hijos cuando fallan — implementa "let it crash"
- `:one_for_one` reinicia solo el hijo fallido (workers independientes)
- `:one_for_all` reinicia todos los hijos (workers interdependientes)
- `:rest_for_one` reinicia el hijo fallido y todos los iniciados después (cadena de dependencias)
- `max_restarts` + `max_seconds` evita reinicios infinitos y escala el fallo hacia arriba
- Child specs con `:id` único son obligatorias cuando hay múltiples hijos del mismo módulo
- Los Supervisors pueden supervisar otros Supervisors, formando árboles de supervisión

## What's Next
**06-application-callbacks**: Aprende el behaviour `Application` para inicializar la aplicación completa al arranque, leer configuración de `config.exs`, y gestionar el árbol de supervisión principal.

## Resources
- [Supervisor — HexDocs](https://hexdocs.pm/elixir/Supervisor.html)
- [Supervisor.init/2 — HexDocs](https://hexdocs.pm/elixir/Supervisor.html#init/2)
- [Mix and OTP: Supervisor](https://elixir-lang.org/getting-started/mix-otp/supervisor-and-application.html)
- [OTP Design Principles: Supervisors](https://www.erlang.org/doc/design_principles/sup_princ.html)
