# 26. DynamicSupervisor

**Difficulty**: Intermedio

## Prerequisites
- Completed exercises 01–25
- Strong understanding of GenServer (exercise 04)
- Understanding of Supervisor basics (exercise 05)
- Familiarity with OTP supervision trees

## Learning Objectives
After completing this exercise, you will be able to:
- Iniciar un `DynamicSupervisor` como parte de un árbol de supervisión
- Agregar procesos hijo en runtime con `DynamicSupervisor.start_child/2`
- Listar todos los procesos supervisados activos con `which_children/1`
- Terminar un proceso específico por PID con `terminate_child/2`
- Implementar patrones de escalado dinámico basados en carga
- Distinguir cuándo usar `Supervisor` vs `DynamicSupervisor`

## Concepts

### Supervisor vs DynamicSupervisor: cuándo usar cada uno

`Supervisor` gestiona un conjunto de procesos hijo fijo y conocido en tiempo de compilación. `DynamicSupervisor` gestiona un conjunto de procesos que se crea, elimina, y escala en runtime — el supervisor no conoce sus hijos de antemano.

```
Supervisor (estático):
  ├── WorkerA (siempre presente)
  ├── WorkerB (siempre presente)
  └── WorkerC (siempre presente)

DynamicSupervisor (dinámico):
  ├── Worker-1 (creado en runtime cuando llegó job 1)
  ├── Worker-2 (creado en runtime cuando llegó job 2)
  └── Worker-N (podría no existir todavía)
```

Casos de uso típicos de `DynamicSupervisor`: pools de workers que escalan según carga, procesos por conexión de usuario, workers por cada job encolado, procesos por cada entidad de dominio (una session por usuario).

### Iniciar un DynamicSupervisor

```elixir
# Como parte de una Application
defmodule MyApp.Application do
  use Application

  def start(_type, _args) do
    children = [
      {DynamicSupervisor, name: MyApp.WorkerSupervisor, strategy: :one_for_one}
    ]
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

# O de forma standalone
{:ok, sup} = DynamicSupervisor.start_link(name: MyWorkers, strategy: :one_for_one)
```

La única estrategia soportada por `DynamicSupervisor` es `:one_for_one`: cada hijo se reinicia de forma independiente. Esto tiene sentido porque los hijos son independientes entre sí.

### start_child/2: agregar workers en runtime

`start_child/2` toma el nombre/PID del supervisor y una especificación de hijo (child spec). La especificación puede ser un módulo (usa su `child_spec/1`) o un mapa explícito.

```elixir
# Usando el módulo del worker (llama a MyWorker.child_spec(%{id: 1}))
{:ok, pid} = DynamicSupervisor.start_child(MyApp.WorkerSupervisor, {MyWorker, %{id: 1}})

# Con child spec explícita
{:ok, pid} = DynamicSupervisor.start_child(MyApp.WorkerSupervisor, %{
  id: :my_worker,
  start: {MyWorker, :start_link, [%{id: 1}]},
  restart: :temporary   # :temporary no reinicia si termina normalmente
})
```

### which_children/1: inspeccionar el estado del supervisor

Retorna una lista de tuplas `{id, pid_o_undefined, type, modules}` para cada hijo activo. Útil para monitoring, debugging, y scaling logic.

```elixir
DynamicSupervisor.which_children(MyApp.WorkerSupervisor)
# => [
#   {:undefined, #PID<0.123.0>, :worker, [MyWorker]},
#   {:undefined, #PID<0.124.0>, :worker, [MyWorker]},
# ]

# Contar workers activos
count = DynamicSupervisor.count_children(MyApp.WorkerSupervisor)
# => %{active: 2, specs: 2, supervisors: 0, workers: 2}
```

### terminate_child/2: remover workers en runtime

Termina un worker específico por PID. El supervisor envía una señal de terminación al proceso y lo elimina de su lista de hijos.

```elixir
{:ok, pid} = DynamicSupervisor.start_child(sup, {MyWorker, opts})

# Más tarde, cuando ya no se necesita
:ok = DynamicSupervisor.terminate_child(MyApp.WorkerSupervisor, pid)
```

### Estrategia de restart: :temporary vs :transient vs :permanent

Para workers dinámicos, la elección de `:restart` es crítica:

- `:permanent` — siempre reinicia (útil para servicios de larga vida)
- `:transient` — reinicia solo si terminó anormalmente (útil para jobs que pueden fallar)
- `:temporary` — nunca reinicia (útil para jobs one-shot)

```elixir
# Worker de job: no reiniciar si terminó normalmente (job completado)
%{
  id: make_ref(),
  start: {JobWorker, :start_link, [job]},
  restart: :temporary
}
```

### Patrones avanzados: pools y scaling

```elixir
defmodule WorkerPool do
  def scale_to(supervisor, target_count) do
    current = DynamicSupervisor.count_children(supervisor).active
    cond do
      current < target_count ->
        1..(target_count - current) |> Enum.each(fn _ ->
          DynamicSupervisor.start_child(supervisor, {Worker, []})
        end)
      current > target_count ->
        excess = current - target_count
        DynamicSupervisor.which_children(supervisor)
        |> Enum.take(excess)
        |> Enum.each(fn {_, pid, _, _} ->
          DynamicSupervisor.terminate_child(supervisor, pid)
        end)
      true -> :already_at_target
    end
  end
end
```

---

## Exercises

### Exercise 1: DynamicSupervisor en un árbol de supervisión

```elixir
# Primero, define el worker que será supervisado
defmodule PrintWorker do
  use GenServer

  def start_link(opts) do
    name = Keyword.get(opts, :name, :anonymous)
    GenServer.start_link(__MODULE__, name)
  end

  @impl true
  def init(name) do
    IO.puts("Worker #{name} started")
    {:ok, name}
  end

  @impl true
  def terminate(reason, name) do
    IO.puts("Worker #{name} stopping: #{inspect(reason)}")
  end

  def get_name(pid), do: GenServer.call(pid, :name)

  @impl true
  def handle_call(:name, _from, name), do: {:reply, name, name}
end

# TODO 1: Define la Application que inicia el DynamicSupervisor
defmodule MyApp.Application do
  use Application

  @impl true
  def start(_type, _args) do
    children = [
      # TODO: Agrega DynamicSupervisor con name: MyApp.WorkerSupervisor
      # y strategy: :one_for_one
      # PISTA: {DynamicSupervisor, name: ..., strategy: ...}
    ]

    # TODO: Inicia el supervisor con strategy: :one_for_one
    Supervisor.start_link(children, strategy: :one_for_one)
  end
end

# TODO 2: Inicia la Application y verifica que el DynamicSupervisor está corriendo
# En IEx o en un script:
{:ok, _app_pid} = MyApp.Application.start(:normal, [])

# Verifica que el supervisor existe y no tiene hijos aún
count = DynamicSupervisor.count_children(MyApp.WorkerSupervisor)
IO.inspect(count)   # => %{active: 0, specs: 0, supervisors: 0, workers: 0}
```

---

### Exercise 2: start_child — agregar workers dinámicamente

```elixir
defmodule DynamicWorkerDemo do
  def run do
    # Inicia el DynamicSupervisor
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)

    # TODO 1: Agrega un worker llamado :alpha
    # PISTA: DynamicSupervisor.start_child(sup, {PrintWorker, [name: :alpha]})
    # Almacena el PID retornado en `pid_alpha`
    {:ok, pid_alpha} = # TODO

    IO.inspect(Process.alive?(pid_alpha))   # => true
    IO.inspect(PrintWorker.get_name(pid_alpha))   # => :alpha

    # TODO 2: Agrega un segundo worker llamado :beta
    {:ok, pid_beta} = # TODO

    # TODO 3: Agrega un tercer worker llamado :gamma
    {:ok, pid_gamma} = # TODO

    # TODO 4: Verifica cuántos workers hay activos usando count_children
    count = # TODO
    IO.inspect(count.active)   # => 3

    # TODO 5: Prueba que si un worker crashea, DynamicSupervisor lo reinicia
    # (porque el restart default de GenServer es :permanent)
    # Mata el proceso alpha y espera un momento
    Process.exit(pid_alpha, :kill)
    :timer.sleep(100)

    new_count = DynamicSupervisor.count_children(sup)
    IO.inspect(new_count.active)   # => 3 (fue reiniciado automáticamente)

    # Nota: el nuevo PID de alpha será diferente
    # Para evitar reinicio, usa restart: :temporary en el child spec
  end
end

DynamicWorkerDemo.run()
```

---

### Exercise 3: which_children — inspeccionar workers activos

```elixir
defmodule WorkerInspector do
  def run do
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)

    # Arranca 4 workers
    pids = for name <- [:w1, :w2, :w3, :w4] do
      {:ok, pid} = DynamicSupervisor.start_child(sup, {PrintWorker, [name: name]})
      pid
    end

    # TODO 1: Llama which_children y almacena el resultado en `children`
    children = # TODO
    IO.inspect(length(children))   # => 4

    # TODO 2: Los elementos de which_children tienen forma:
    # {:undefined, pid, :worker, [PrintWorker]}
    # Extrae solo los PIDs de la lista children usando Enum.map
    worker_pids = # TODO (Enum.map sobre children, extrae el segundo elemento)
    IO.inspect(length(worker_pids))   # => 4

    # TODO 3: Verifica que los PIDs en worker_pids son los mismos que en pids
    # (el orden puede variar, usa MapSet para comparación)
    same = MapSet.equal?(MapSet.new(worker_pids), MapSet.new(pids))
    IO.inspect(same)   # => true

    # TODO 4: Usa count_children para obtener el resumen
    # Inspecciona qué campos tiene el mapa retornado
    summary = # TODO
    IO.inspect(summary)
    # => %{active: 4, specs: 4, supervisors: 0, workers: 4}

    # TODO 5: Filtra children para encontrar solo los que tienen PID vivo
    # (en condiciones normales, todos deberían estar vivos)
    alive_count = children
      |> Enum.count(fn
        # TODO: pattern match la tupla y usa Process.alive?(pid)
      end)
    IO.inspect(alive_count)   # => 4
  end
end

WorkerInspector.run()
```

---

### Exercise 4: terminate_child — remover workers específicos

```elixir
defmodule DynamicTermination do
  def run do
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)

    # Inicia 5 workers con restart: :temporary (no se reinician al terminar)
    pids = for i <- 1..5 do
      spec = %{
        id: make_ref(),
        start: {PrintWorker, :start_link, [[name: :"worker_#{i}"]]},
        restart: :temporary   # importante: no reiniciar
      }
      {:ok, pid} = DynamicSupervisor.start_child(sup, spec)
      pid
    end

    IO.inspect(DynamicSupervisor.count_children(sup).active)   # => 5

    # TODO 1: Termina el primer worker (pids |> hd) usando terminate_child
    first_pid = hd(pids)
    result = # TODO: DynamicSupervisor.terminate_child(sup, first_pid)
    IO.inspect(result)   # => :ok

    IO.inspect(DynamicSupervisor.count_children(sup).active)   # => 4
    IO.inspect(Process.alive?(first_pid))   # => false

    # TODO 2: Termina el último worker (pids |> List.last)
    # TODO

    IO.inspect(DynamicSupervisor.count_children(sup).active)   # => 3

    # TODO 3: Implementa terminate_all/1 que termina todos los workers activos
    terminate_all(sup)
    IO.inspect(DynamicSupervisor.count_children(sup).active)   # => 0

    # TODO 4: ¿Qué retorna terminate_child si el PID ya no existe?
    # Prueba llamando terminate_child con un PID ya terminado
    result2 = DynamicSupervisor.terminate_child(sup, first_pid)
    IO.inspect(result2)   # => {:error, :not_found}
  end

  # TODO: Implementa usando which_children + terminate_child para cada hijo
  def terminate_all(supervisor) do
    # PISTA: usa DynamicSupervisor.which_children(supervisor)
    # extrae el PID de cada tupla y llama terminate_child
  end
end

DynamicTermination.run()
```

---

### Exercise 5: Scaling pattern — N workers según configuración

```elixir
defmodule WorkerPool do
  @moduledoc """
  A dynamic worker pool that scales based on demand.
  """

  # TODO 1: Implementa start/1 que inicia el DynamicSupervisor con nombre dado
  # Retorna {:ok, supervisor_pid}
  def start(name \\ __MODULE__) do
    # TODO: DynamicSupervisor.start_link(name: name, strategy: :one_for_one)
  end

  # TODO 2: Implementa spawn_workers/2 que crea exactamente N workers
  # en el supervisor dado. Retorna la lista de PIDs creados.
  def spawn_workers(supervisor, count) when count > 0 do
    # TODO: usa Enum.map sobre 1..count, crea un worker por iteración
    # Nombre cada worker :"worker_#{i}"
    # PISTA: para evitar conflictos de nombre, usa make_ref() como id en el spec
  end

  # TODO 3: Implementa current_count/1 que retorna el número de workers activos
  def current_count(supervisor) do
    # TODO: usa count_children
  end

  # TODO 4: Implementa scale_to/2 que ajusta el pool al número objetivo
  # - Si hay menos workers que target: crea los que faltan
  # - Si hay más: termina el exceso (los más recientes primero)
  # - Si hay exactamente target: no hace nada
  def scale_to(supervisor, target) when target >= 0 do
    current = current_count(supervisor)

    cond do
      current < target ->
        # TODO: crea (target - current) workers nuevos
      current > target ->
        # TODO: termina (current - target) workers
        # PISTA: which_children |> Enum.take(excess) |> Enum.each(terminate)
      true ->
        :ok
    end
  end

  # TODO 5: Implementa list_workers/1 que retorna los PIDs de todos los workers
  def list_workers(supervisor) do
    # TODO: which_children, extrae solo los PIDs
  end
end

# Demo de scaling
{:ok, sup} = WorkerPool.start(:my_pool)

WorkerPool.spawn_workers(:my_pool, 3)
IO.inspect(WorkerPool.current_count(:my_pool))   # => 3

WorkerPool.scale_to(:my_pool, 6)
IO.inspect(WorkerPool.current_count(:my_pool))   # => 6

WorkerPool.scale_to(:my_pool, 2)
IO.inspect(WorkerPool.current_count(:my_pool))   # => 2

WorkerPool.scale_to(:my_pool, 0)
IO.inspect(WorkerPool.current_count(:my_pool))   # => 0
```

---

## Common Mistakes

### Usar Supervisor cuando la lista de hijos es dinámica

```elixir
# MAL: Supervisor.start_link con una lista que crece en runtime
# Supervisor no tiene start_child (fue deprecado)
children = [worker1, worker2]
Supervisor.start_link(children, strategy: :one_for_one)
# No puedes agregar workers después de iniciar

# BIEN: DynamicSupervisor cuando los hijos son dinámicos
DynamicSupervisor.start_link(name: MyDynSup, strategy: :one_for_one)
DynamicSupervisor.start_child(MyDynSup, {MyWorker, opts})
```

### No especificar restart: :temporary para workers one-shot

```elixir
# MAL: el restart default es :permanent, el supervisor reinicia workers que terminan
# Si tu worker procesa un job y termina normalmente, será reiniciado innecesariamente
DynamicSupervisor.start_child(sup, {JobWorker, job})

# BIEN: usa :temporary para workers que deben terminar al completar su tarea
spec = %{id: make_ref(), start: {JobWorker, :start_link, [job]}, restart: :temporary}
DynamicSupervisor.start_child(sup, spec)
```

### Asumir que which_children mantiene orden de inserción

```elixir
# which_children NO garantiza ningún orden en particular
# No confíes en que el primero de la lista es el más antiguo

# Si necesitas el worker más reciente, trackea los PIDs tú mismo:
pids = []
{:ok, pid} = DynamicSupervisor.start_child(sup, spec)
pids = [pid | pids]   # mantén tu propia lista ordenada
```

### Usar terminate_child con PID de un proceso ya muerto

```elixir
# terminate_child con PID inexistente retorna {:error, :not_found}
# No es un crash — pero deberías manejarlo
case DynamicSupervisor.terminate_child(sup, dead_pid) do
  :ok -> :terminated
  {:error, :not_found} -> :already_gone
end
```

---

## Try It Yourself

Implementa un worker pool dinámico que se autoescala según la carga. Cuando la "queue" supera un umbral, agrega workers; cuando baja, los elimina.

```elixir
defmodule AutoScalingPool do
  use GenServer

  @min_workers 1
  @max_workers 10
  @scale_up_threshold 5    # escala arriba si queue > 5
  @scale_down_threshold 2  # escala abajo si queue < 2

  def start_link(opts \\ []) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # API pública
  def enqueue(job), do: GenServer.cast(__MODULE__, {:enqueue, job})
  def queue_size, do: GenServer.call(__MODULE__, :queue_size)
  def worker_count, do: GenServer.call(__MODULE__, :worker_count)

  @impl true
  def init(_opts) do
    # TODO: inicia el DynamicSupervisor para los workers
    {:ok, sup} = DynamicSupervisor.start_link(strategy: :one_for_one)

    # TODO: inicia con @min_workers workers
    # Programa un tick periódico para revisar la carga: Process.send_after(self(), :check_load, 1000)

    state = %{
      supervisor: sup,
      queue: [],
      worker_count: 0
    }

    # TODO: inicia @min_workers workers y actualiza state.worker_count
    {:ok, state}
  end

  @impl true
  def handle_cast({:enqueue, job}, state) do
    new_queue = state.queue ++ [job]
    # TODO: actualiza el state con la nueva queue
    {:noreply, %{state | queue: new_queue}}
  end

  @impl true
  def handle_call(:queue_size, _from, state) do
    {:reply, length(state.queue), state}
  end

  @impl true
  def handle_call(:worker_count, _from, state) do
    {:reply, DynamicSupervisor.count_children(state.supervisor).active, state}
  end

  @impl true
  def handle_info(:check_load, state) do
    queue_len = length(state.queue)
    current_workers = DynamicSupervisor.count_children(state.supervisor).active

    new_state = cond do
      queue_len > @scale_up_threshold and current_workers < @max_workers ->
        # TODO: agrega un worker, actualiza state
        state
      queue_len < @scale_down_threshold and current_workers > @min_workers ->
        # TODO: termina un worker, actualiza state
        state
      true ->
        state
    end

    # Reprograma el siguiente check
    Process.send_after(self(), :check_load, 1000)
    {:noreply, new_state}
  end
end

# Demo
{:ok, _} = AutoScalingPool.start_link()
IO.inspect(AutoScalingPool.worker_count())   # => 1 (mínimo)

# Llena la queue
for i <- 1..10, do: AutoScalingPool.enqueue("job_#{i}")
IO.inspect(AutoScalingPool.queue_size())     # => 10

:timer.sleep(1500)   # espera un tick de check_load
IO.inspect(AutoScalingPool.worker_count())   # => > 1 (escaló)
```

**Checklist**:
- [ ] DynamicSupervisor inicia correctamente como parte del estado del GenServer
- [ ] `enqueue/1` agrega trabajos a la queue
- [ ] `handle_info(:check_load)` escala arriba cuando queue > threshold
- [ ] `handle_info(:check_load)` escala abajo cuando queue < threshold
- [ ] Nunca supera `@max_workers` ni baja de `@min_workers`
- [ ] El tick se reprograma automáticamente con `Process.send_after`
