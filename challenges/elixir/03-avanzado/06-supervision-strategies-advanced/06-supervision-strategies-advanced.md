# 06 — Supervision Strategies Advanced

**Difficulty**: Avanzado  
**Estimated time**: 90–120 min  
**Topics**: OTP, Supervisors, Fault Tolerance, Production Patterns

---

## Context

Un supervisor en OTP no es un simple "re-lanzador de procesos". Es el mecanismo que define cómo falla tu sistema: si falla silenciosamente, si falla rápido, si propaga el fallo o lo contiene. La elección de estrategia determina el contrato de disponibilidad de tu aplicación.

Entender la diferencia entre `:one_for_one`, `:one_for_all` y `:rest_for_one` no es trivia: es la diferencia entre un sistema que se recupera solo y uno que derrumba producción en cascada.

---

## Concepts

### Supervision Strategies

#### `:one_for_one`
Solo el proceso que falla es reiniciado. Los demás no son afectados.

```elixir
children = [
  CacheWorker,    # si falla → solo CacheWorker se reinicia
  MetricsWorker,  # no se toca
  AuditWorker,    # no se toca
]
Supervisor.init(children, strategy: :one_for_one)
```

**Úsalo cuando**: los workers son completamente independientes. El fallo de uno no invalida el estado de los demás.

**Trampa**: si los workers comparten estado implícito (por ejemplo, un ETS table que el proceso A escribe y B lee), `:one_for_one` puede dejar B con datos corruptos invisibles.

---

#### `:one_for_all`
Si cualquier child falla, **todos** son terminados y reiniciados juntos.

```elixir
children = [
  DatabasePool,   # si falla → todos se reinician
  CacheLayer,     # depende de DB estar sana
  QueryRouter,    # depende de ambos
]
Supervisor.init(children, strategy: :one_for_all)
```

**Úsalo cuando**: los children tienen dependencias mutuas de estado. Si uno cae, el resto queda en un estado inconsistente de todas formas.

**Trampa**: un worker ruidoso que falla frecuentemente arrastrará a todos los demás con él repetidamente. El costo de un fallo es multiplicado por N children.

---

#### `:rest_for_one`
Si el child N falla, se reinician el child N y todos los que fueron iniciados **después** de él (índices > N). Los anteriores no se tocan.

```elixir
children = [
  ConnectionPool,   # índice 0: si falla → reinicia 0, 1, 2
  SessionManager,   # índice 1: si falla → reinicia 1, 2
  RequestHandler,   # índice 2: si falla → reinicia solo 2
]
Supervisor.init(children, strategy: :rest_for_one)
```

**Úsalo cuando**: existe una cadena de dependencia lineal. B depende de A, C depende de B. Si A cae, B y C quedan inválidos pero no al revés.

**Trampa**: la posición en la lista importa. Mover un child cambia la semántica de supervisión silenciosamente.

---

### Intensity and Period: `max_restarts` / `max_seconds`

```elixir
Supervisor.init(children,
  strategy: :one_for_one,
  max_restarts: 3,    # máximo 3 reinicios...
  max_seconds: 5      # ...en una ventana de 5 segundos
)
```

Si un child se reinicia más de `max_restarts` veces en `max_seconds` segundos, el supervisor **se rinde** y termina él mismo. Esto propaga el fallo hacia arriba en el árbol.

**Por qué existe**: un proceso que falla constantemente en un loop consume CPU y puede enmascarar un bug real. El supervisor se rinde para forzar visibilidad del problema.

**Valores por defecto**: `max_restarts: 3`, `max_seconds: 5` — relativamente agresivos. En producción con workers que pueden tener spikes legítimos, considera aumentarlos o reestructurar el árbol.

```
Ventana deslizante (no fija):
  t=0s → crash #1
  t=3s → crash #2
  t=5s → crash #3
  t=5.1s → supervisor termina (3 crashes en 5.1s < 5s... ¿o no?)

Cuidado: la ventana se mide desde el PRIMER crash reciente, no desde t=0.
```

---

### Cascading Failures

El patrón más peligroso: un proceso ruidoso que:
1. Falla repetidamente
2. Supera el threshold del supervisor
3. El supervisor termina
4. El supervisor padre reinicia el supervisor hijo
5. Si el padre también supera su threshold → todo el árbol cae

```
Application
└── TopSupervisor (max_restarts: 10, max_seconds: 60)
    ├── InfrastructureSupervisor (max_restarts: 3, max_seconds: 5)
    │   ├── DatabasePool
    │   └── CachePool
    └── BusinessSupervisor
        ├── OrderService
        └── PaymentService
```

Si `CachePool` falla 4 veces en 5 segundos:
1. `InfrastructureSupervisor` termina
2. `TopSupervisor` reinicia `InfrastructureSupervisor`
3. Si `CachePool` sigue fallando → `TopSupervisor` puede terminar
4. La aplicación entera cae

**Solución**: aislar workers ruidosos en supervisores con thresholds apropiados, o usar `:temporary` restart type para workers que no necesitan sobrevivir.

---

### Restart Types en Child Spec

```elixir
def child_spec(opts) do
  %{
    id: __MODULE__,
    start: {__MODULE__, :start_link, [opts]},
    restart: :permanent,  # siempre reiniciar (default)
    # restart: :temporary, # nunca reiniciar (fire-and-forget)
    # restart: :transient, # reiniciar solo si terminó anormalmente
    shutdown: 5_000
  }
end
```

- `:permanent` — se reinicia siempre. Para workers críticos.
- `:temporary` — nunca se reinicia. Para tareas únicas.
- `:transient` — se reinicia solo si el proceso terminó con error (no si llamó `stop` normalmente). Para workers opcionales.

---

## Exercise 1 — Strategy Selection

### Problem

Diseña el árbol de supervisión para una aplicación web con los siguientes componentes:

- `DBPool` — pool de conexiones a PostgreSQL. Sin él nada funciona.
- `CachePool` — pool de conexiones a Redis. Usado por `RequestHandler` para caching. Si falla, el sistema puede operar sin caché (más lento, pero funcional).
- `HTTPServer` — servidor HTTP. Depende de `DBPool` para queries. `CachePool` es opcional.
- `MetricsReporter` — reporta métricas a Datadog cada 30s. Completamente independiente.
- `HealthChecker` — hace health checks contra servicios externos. Independiente.

**Tu tarea**:

1. Decide qué estrategia usar para cada nivel del árbol (puede haber múltiples supervisores)
2. Decide el `restart` type para cada worker
3. Justifica cada decisión con un trade-off explícito
4. Implementa el código del `Application` y los supervisores necesarios

```elixir
# Estructura esperada (incompleta — tú la completas):
defmodule MyApp.Application do
  use Application

  def start(_type, _args) do
    children = [
      # ¿Qué va aquí y en qué orden?
    ]

    opts = [strategy: ???, name: MyApp.Supervisor]
    Supervisor.start_link(children, opts)
  end
end
```

### Hints

<details>
<summary>Hint 1 — Dominios de fallo</summary>

No todo debe ir en un supervisor. Agrupa por "si esto falla, ¿qué más queda inválido?". `MetricsReporter` y `HealthChecker` son completamente independientes del core. Considéralos en un supervisor separado con `:temporary` o `:transient` restart.

</details>

<details>
<summary>Hint 2 — CachePool es opcional</summary>

Si `CachePool` falla y el sistema puede continuar sin él, ¿tiene sentido que su fallo derribe `HTTPServer`? Considera `:transient` restart para `CachePool` o aislarlo en un supervisor diferente al de `DBPool` + `HTTPServer`.

</details>

<details>
<summary>Hint 3 — Dependencia DB → HTTP</summary>

`HTTPServer` depende de `DBPool`. Si `DBPool` cae y se reinicia, ¿`HTTPServer` sigue apuntando a conexiones válidas? Depende de cómo implementes el pool. Si el pool usa un nombre global y el handler busca conexiones por nombre, `:one_for_one` puede funcionar. Si el handler recibe un PID al inicio (inyección), necesitas `:rest_for_one`.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
defmodule MyApp.Application do
  use Application

  def start(_type, _args) do
    children = [
      # Infraestructura crítica: DB debe iniciar antes que HTTP
      # rest_for_one porque HTTPServer depende del pool por nombre registrado
      {MyApp.InfrastructureSupervisor, []},

      # Servicios auxiliares: independientes, no afectan el core si fallan
      {MyApp.AuxiliarySupervisor, []},
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: MyApp.Supervisor)
  end
end

defmodule MyApp.InfrastructureSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      # DBPool primero: HTTPServer depende de él
      {MyApp.DBPool, []},
      # CachePool segundo: si falla, HTTPServer sigue funcionando (sin caché)
      # transient: no reiniciar si terminó normalmente (shutdown limpio)
      %{
        id: MyApp.CachePool,
        start: {MyApp.CachePool, :start_link, [[]]},
        restart: :transient
      },
      # HTTPServer último: depende de DBPool estar disponible
      {MyApp.HTTPServer, []},
    ]

    # rest_for_one: si DBPool cae, CachePool y HTTPServer se reinician también
    # Si CachePool cae (transient + fallo), solo HTTPServer se reinicia
    Supervisor.init(children, strategy: :rest_for_one)
  end
end

defmodule MyApp.AuxiliarySupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      # Completamente independientes entre sí
      %{
        id: MyApp.MetricsReporter,
        start: {MyApp.MetricsReporter, :start_link, [[]]},
        restart: :permanent  # métricas deben seguir funcionando
      },
      %{
        id: MyApp.HealthChecker,
        start: {MyApp.HealthChecker, :start_link, [[]]},
        restart: :permanent
      },
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end
```

**Trade-offs de esta solución**:
- `rest_for_one` en Infrastructure garantiza que si DB cae, HTTP se reinicia y obtiene conexiones frescas
- `CachePool` con `:transient` significa que si Redis no está disponible y el pool termina limpiamente (`:normal`), no se intenta reconectar en loop
- Separar en dos supervisores aísla fallo de auxiliares del core: si `MetricsReporter` explota repetidamente, no afecta `DBPool` ni `HTTPServer`

</details>

---

## Exercise 2 — Threshold Tuning

### Problem

Tienes un supervisor con configuración por defecto (`max_restarts: 3, max_seconds: 5`) supervisando un worker que simula trabajo con fallos esporádicos.

```elixir
defmodule MyApp.UnreliableWorker do
  use GenServer

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts)

  def init(_opts) do
    # Simula trabajo que tarda en inicializarse
    Process.send_after(self(), :do_work, 1_000)
    {:ok, %{count: 0}}
  end

  def handle_info(:do_work, state) do
    new_count = state.count + 1

    if rem(new_count, 4) == 0 do
      # Falla cada 4ta iteración
      raise "Simulated transient error at count #{new_count}"
    end

    Process.send_after(self(), :do_work, 500)
    {:noreply, %{state | count: new_count}}
  end
end
```

**Tu tarea**:

1. Configura el supervisor con los defaults y observa cuándo termina
2. Calcula manualmente: con este patrón de fallo (cada 4ta iteración, cada 500ms), ¿cuánto tiempo tarda el supervisor en rendirse?
3. Ajusta `max_restarts` y `max_seconds` para que el supervisor tolere este patrón de fallo sin rendirse
4. ¿Cuándo es una mala idea aumentar los thresholds indefinidamente?

```elixir
defmodule MyApp.WorkerSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [MyApp.UnreliableWorker]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: ???,
      max_seconds: ???
    )
  end
end
```

### Hints

<details>
<summary>Hint 1 — Calcula el tiempo hasta el crash</summary>

El worker hace trabajo cada 500ms. Falla en la iteración 4 (t≈2s desde inicio). Se reinicia, arranca en 1s, hace trabajo en 500ms... ¿cuántas veces puede caer en 5 segundos? Haz el cálculo antes de correr el código.

</details>

<details>
<summary>Hint 2 — La ventana es deslizante</summary>

`max_seconds` no es una ventana fija que se resetea cada N segundos. Es una ventana deslizante: el supervisor mantiene timestamps de los últimos `max_restarts` reinicios y compara si el más antiguo ocurrió hace menos de `max_seconds` segundos.

</details>

<details>
<summary>Hint 3 — Threshold alto no es gratis</summary>

Si aumentas `max_restarts` a 1000, el supervisor nunca se rinde — pero un worker que crashea en loop consume CPU en reinicios constantes, llena logs, y puede enmascarar un bug que debería ser detectado. El threshold es una señal de alarma, no una solución al problema subyacente.

</details>

### One Possible Solution

<details>
<summary>Ver análisis (intenta calcularlo primero)</summary>

**Cálculo del tiempo hasta el crash con defaults (max_restarts: 3, max_seconds: 5)**:

```
Inicio: t=0s
  Worker init: 1s de delay
  t=1.0s: iteración 1 (ok)
  t=1.5s: iteración 2 (ok)
  t=2.0s: iteración 3 (ok)
  t=2.5s: iteración 4 → CRASH #1

Reinicio #1: supervisor reinicia worker
  t=2.5s + init 1s = t=3.5s: iteración 1 (ok)
  t=4.0s: iteración 2 (ok)
  t=4.5s: iteración 3 (ok)
  t=5.0s: iteración 4 → CRASH #2

Reinicio #2:
  t=5.0s + 1s = t=6.0s: iteración 1 (ok)
  t=6.5s: iteración 2 (ok)
  t=7.0s: iteración 3 (ok)
  t=7.5s: iteración 4 → CRASH #3

Reinicio #3:
  t=7.5s + 1s = t=8.5s: iteración 1 (ok)
  t=9.0s: iteración 2 (ok)
  t=9.5s: iteración 3 (ok)
  t=10.0s: iteración 4 → CRASH #4

En t=10.0s el supervisor verifica: crash #4 ocurrió.
¿Los últimos 3 crashes (crashes #2, #3, #4) en t=5.0s, t=7.5s, t=10.0s?
Ventana: t=10.0 - t=5.0 = 5.0s — EXACTAMENTE en el límite.

Resultado: supervisor termina en ~t=10s.
```

**Configuración tolerante**:

```elixir
# Opción A: ampliar la ventana temporal
Supervisor.init(children,
  strategy: :one_for_one,
  max_restarts: 3,
  max_seconds: 30  # 3 crashes en 30 segundos está bien
)

# Opción B: usar :transient para que el worker no cuente si termina normalmente
# (no aplica aquí porque siempre termina con excepción)

# Opción C: separar la lógica de retry del worker
# El worker captura su propio error y espera antes de reintentar
# → no crashea el proceso, solo registra el error
defmodule MyApp.ResilientWorker do
  use GenServer

  def handle_info(:do_work, state) do
    new_count = state.count + 1

    result =
      try do
        if rem(new_count, 4) == 0, do: raise("error"), else: :ok
      rescue
        e ->
          Logger.error("Work failed: #{inspect(e)}")
          :error
      end

    # Continúa independientemente del resultado
    Process.send_after(self(), :do_work, 500)
    {:noreply, %{state | count: new_count}}
  end
end
```

**Cuándo NO aumentar thresholds**:
- El worker falla por un bug en el código (no por condición externa transitoria)
- El worker consume recursos en cada reinicio (conexiones, memoria)
- La tasa de fallo está aumentando (indicio de degradación progresiva)

</details>

---

## Exercise 3 — Cascading Failures

### Problem

Demuestra el problema de cascading failures y diseña una solución de contención.

Implementa este sistema:

```elixir
# Sistema inicial — PROBLEMÁTICO
defmodule MyApp.ProblematicApp do
  use Application

  def start(_type, _args) do
    children = [
      MyApp.DatabasePool,
      MyApp.CachePool,       # falla frecuentemente (Redis inestable)
      MyApp.OrderService,
      MyApp.PaymentService,
    ]

    # Con :one_for_all, si CachePool falla → TODO se reinicia
    Supervisor.start_link(children,
      strategy: :one_for_all,
      max_restarts: 3,
      max_seconds: 10,
      name: MyApp.Supervisor
    )
  end
end

defmodule MyApp.CachePool do
  use GenServer

  def start_link(_), do: GenServer.start_link(__MODULE__, [], name: __MODULE__)

  def init(_) do
    # Simula Redis inestable: falla al conectar el 70% de las veces
    if :rand.uniform(10) > 3 do
      {:stop, :redis_connection_failed}
    else
      {:ok, %{connected: true}}
    end
  end
end
```

**Tu tarea**:

1. Implementa el sistema problemático y observa cómo `CachePool` inestable derriba `OrderService` y `PaymentService`
2. Calcula en qué momento el supervisor raíz termina y por qué
3. Rediseña el sistema para que `CachePool` inestable **no afecte** a `OrderService` y `PaymentService`
4. Implementa un mecanismo de circuit-breaking manual: si `CachePool` falla N veces, el sistema opera sin caché en lugar de intentar reiniciar indefinidamente

### Hints

<details>
<summary>Hint 1 — Aislamiento en supervisor dedicado</summary>

El primer paso es sacar `CachePool` de bajo el supervisor que usa `:one_for_all`. Si `CachePool` tiene su propio supervisor, sus fallos no derribarán a `OrderService`.

</details>

<details>
<summary>Hint 2 — Restart type :temporary para cache</summary>

¿Necesitas que `CachePool` sea reiniciado indefinidamente? Si Redis está caído, el pool va a fallar en cada reinicio. Considera `:temporary` (no reiniciar nunca) con un mecanismo separado de reconexión bajo demanda.

</details>

<details>
<summary>Hint 3 — Estado del circuit breaker</summary>

Un circuit breaker simple puede vivir en un GenServer que registra fallos de `CachePool`. Las funciones que usan caché consultan si el circuit está "abierto" (fallando) o "cerrado" (ok) antes de intentar usar Redis. Si está abierto, van directo a DB.

</details>

---

## Common Mistakes

### Mistake 1 — Usar `:one_for_all` por defecto "porque es más seguro"

```elixir
# MAL: "más reinicios = más seguro"
Supervisor.init(children, strategy: :one_for_all)

# El resultado: un MetricsReporter que falla cada 30s
# derriba DBPool, CachePool, y HTTPServer en cada ciclo.
# Tu aplicación reinicia completamente cada 30 segundos.
```

**Regla**: `:one_for_all` es la estrategia más costosa. Solo úsala cuando puedas demostrar que los workers comparten estado que se invalida mutuamente.

---

### Mistake 2 — Ignorar el orden de children con `:rest_for_one`

```elixir
# MAL: HTTPServer antes de DBPool con :rest_for_one
children = [
  MyApp.HTTPServer,   # índice 0
  MyApp.DBPool,       # índice 1
]
Supervisor.init(children, strategy: :rest_for_one)

# Si HTTPServer falla → reinicia HTTPServer y DBPool (¡innecesario!)
# Si DBPool falla → reinicia solo DBPool (HTTPServer sigue con conexiones inválidas)
# El orden está al revés de lo que necesitas
```

---

### Mistake 3 — Thresholds demasiado permisivos como solución a bugs

```elixir
# MAL: el worker crashea → aumentamos el threshold para "arreglarlo"
Supervisor.init(children,
  strategy: :one_for_one,
  max_restarts: 1_000_000,
  max_seconds: 1
)

# Ahora el worker puede crashear un millón de veces por segundo
# sin que el supervisor se rinda. Tu log tiene 1M errores/s
# y el CPU está al 100% en reinicios.
```

---

### Mistake 4 — Confundir `max_seconds` con un timer de reseteo

```elixir
# Asunción incorrecta: "cada 5 segundos el contador de reinicios se resetea"
# Realidad: es una ventana DESLIZANTE basada en timestamps

# Con max_restarts: 3, max_seconds: 5:
# crash en t=0, t=4, t=8 → NO supera el threshold
# porque en t=8, el crash de t=0 ya está fuera de la ventana de 5s
# Solo se cuentan los crashes dentro de los últimos max_seconds segundos
```

---

## Production Patterns

### Pattern 1 — Supervisor Tree Depth para contención

```
Application
├── CoreSupervisor (max_restarts: 5, max_seconds: 30)
│   ├── DBPool          :permanent
│   └── HTTPServer      :permanent
└── OptionalSupervisor (max_restarts: 10, max_seconds: 10)
    ├── CachePool       :transient
    ├── MetricsReporter :permanent
    └── HealthChecker   :permanent
```

Si `OptionalSupervisor` se rinde, `CoreSupervisor` sobrevive. La aplicación degrada gracefully.

### Pattern 2 — Proceso Sentinel para detección de degradación

```elixir
defmodule MyApp.Sentinel do
  use GenServer

  # Monitorea otros procesos y reporta degradación
  def init(_) do
    :timer.send_interval(5_000, :check_health)
    {:ok, %{failure_counts: %{}}}
  end

  def handle_info(:check_health, state) do
    Supervisor.which_children(MyApp.OptionalSupervisor)
    |> Enum.each(fn {id, pid, _type, _modules} ->
      unless is_pid(pid) and Process.alive?(pid) do
        Logger.warning("Child #{id} is not running")
        # Alerta a sistema de monitoreo externo
      end
    end)
    {:noreply, state}
  end
end
```

### Pattern 3 — Backoff en reinicios con `handle_continue`

Los supervisores OTP no soportan backoff exponencial nativo. La solución es que el proceso mismo implemente el delay de inicialización:

```elixir
defmodule MyApp.BackoffWorker do
  use GenServer

  def init(opts) do
    attempt = Keyword.get(opts, :attempt, 1)
    delay = min(1_000 * attempt, 30_000)  # max 30s
    Process.send_after(self(), {:connect, attempt}, delay)
    {:ok, %{connected: false, attempt: attempt}}
  end

  def handle_info({:connect, attempt}, state) do
    case do_connect() do
      {:ok, conn} ->
        {:noreply, %{state | connected: true}}
      {:error, reason} ->
        Logger.warning("Connect attempt #{attempt} failed: #{inspect(reason)}")
        # Terminar con :normal para que :transient no lo reinicie en loop
        # O dejar que el supervisor lo reinicie con nuevo attempt en state
        {:stop, {:connection_failed, reason}, state}
    end
  end
end
```

---

## Resources

- [Erlang Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [Elixir Supervisor docs](https://hexdocs.pm/elixir/Supervisor.html)
- [The Zen of Erlang — Fred Hébert](https://ferd.ca/the-zen-of-erlang.html)
- [Designing for Failure in Distributed Systems](https://hexdocs.pm/elixir/design-anti-patterns.html)
