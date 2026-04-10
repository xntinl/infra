# 09 — Supervision Tree Design

**Difficulty**: Avanzado  
**Estimated time**: 90–120 min  
**Topics**: Application Architecture, Dependency Ordering, Failure Domains, Production Design

---

## Context

Diseñar un árbol de supervisión no es rellenar una lista de `children`. Es modelar explícitamente las dependencias, los dominios de fallo, y los contratos de disponibilidad de tu sistema.

Un árbol mal diseñado tiene dos síntomas clásicos: o bien los fallos se propagan demasiado (un servicio auxiliar derriba toda la aplicación), o bien los fallos son ignorados (un componente crítico cae y el resto sigue funcionando con datos inválidos).

Este ejercicio trabaja con sistemas reales de la industria. Los principios son los mismos independientemente del tamaño del sistema.

---

## Concepts

### Dependency Ordering en children

Los children de un supervisor se inician **en orden** y se terminan **en orden inverso**. Este comportamiento no es opcional — es fundamental para gestionar dependencias:

```elixir
children = [
  MyApp.DatabasePool,   # 1º en iniciar, último en terminar
  MyApp.CachePool,      # 2º — puede asumir que DB está disponible
  MyApp.OrderService,   # 3º — puede asumir que DB y Cache están disponibles
]
```

Si `DatabasePool` falla en su `start_link`, los children posteriores nunca se inician. El supervisor falla y el error se propaga hacia arriba. Esto es comportamiento correcto: no quieres `OrderService` corriendo sin base de datos.

**En el shutdown**: `OrderService` termina primero (último iniciado), luego `CachePool`, finalmente `DatabasePool`. Esto permite que `OrderService` cierre requests en vuelo antes de que las conexiones DB desaparezcan.

---

### Grouping by Failure Domain

Un failure domain es el conjunto de componentes que deben fallar o recuperarse juntos. Componentes en el mismo failure domain deben estar en el mismo supervisor. Componentes en dominios diferentes deben estar en supervisores separados.

```
Failure Domain A: Infraestructura crítica
  → DatabasePool, CachePool
  → Si cualquiera falla, los servicios de negocio son inútiles

Failure Domain B: Servicios de negocio
  → OrderService, PaymentService
  → Pueden fallar sin derribar la infraestructura

Failure Domain C: Servicios opcionales
  → MetricsReporter, HealthChecker, AuditLogger
  → Su fallo no afecta el núcleo del sistema
```

**La regla**: dos componentes pertenecen al mismo failure domain si el fallo de uno hace que el otro opere en estado inválido o inconsistente.

---

### Infrastructure vs Business Logic

Convención de diseño que aparece en prácticamente todos los sistemas Elixir/Erlang de producción:

```
Application
├── InfrastructureSupervisor      ← inicia primero
│   ├── DatabasePool
│   ├── CachePool
│   └── MessageBrokerConnection
│
├── BusinessSupervisor            ← inicia cuando infra está lista
│   ├── OrderService
│   ├── PaymentService
│   └── InventoryService
│
└── AuxiliarySupervisor           ← puede iniciar antes o después
    ├── MetricsReporter
    ├── HealthChecker
    └── BackgroundJobs
```

**Por qué**: la infraestructura es un prerrequisito. El negocio es el propósito. Los auxiliares son nice-to-haves. Esta separación hace explícita la prioridad de cada componente.

---

### Lazy vs Eager Child Initialization

**Eager** (default): el proceso se inicia cuando el supervisor se inicia. Si falla, el supervisor lo reintenta.

**Lazy**: el proceso se inicia bajo demanda, típicamente cuando llega la primera request.

```elixir
# Eager — inicia en el boot de la aplicación
{MyApp.ConnectionPool, [size: 10]}

# Lazy — usando DynamicSupervisor para iniciar bajo demanda
defmodule MyApp.LazyRegistry do
  def get_or_create(key) do
    case Registry.lookup(MyApp.Registry, key) do
      [{pid, _}] -> pid
      [] ->
        {:ok, pid} = DynamicSupervisor.start_child(
          MyApp.DynamicSupervisor,
          {MyApp.Worker, key}
        )
        pid
    end
  end
end
```

**Cuándo usar lazy**: procesos por entidad (por user_id, por session_id) que no puedes pre-crear porque el conjunto es dinámico o desconocido al boot.

---

### Circular Dependency Detection

Las dependencias circulares en supervisión son un error de diseño que Elixir no detecta automáticamente:

```
A necesita que B esté listo
B necesita que A esté listo
→ Deadlock en el startup
```

Señales de dependencia circular:
- Proceso A espera en `GenServer.call` a proceso B durante su `init/1`
- Proceso B espera en `GenServer.call` a proceso A durante su `init/1`
- El sistema se queda colgado en el boot sin error explícito

**Solución**: usar `handle_continue/2` para diferir la inicialización que requiere otros procesos:

```elixir
def init(opts) do
  # init retorna inmediatamente — el supervisor puede continuar con otros children
  {:ok, %{ready: false}, {:continue, :connect}}
end

def handle_continue(:connect, state) do
  # Aquí los demás children ya están iniciados
  # Podemos hacer GenServer.call a otros procesos sin riesgo de deadlock
  conn = MyApp.DatabasePool.checkout()
  {:noreply, %{state | conn: conn, ready: true}}
end
```

---

## Exercise 1 — E-Commerce Supervision Tree

### Problem

Diseña e implementa el árbol de supervisión completo para una aplicación de e-commerce con los siguientes componentes:

**Componentes**:
- `DBPool` — pool de conexiones PostgreSQL. Todos los servicios dependen de él.
- `Cache` — caché Redis. Opcional para `OrderService` y `InventoryService`, obligatoria para `ProductCatalog`.
- `OrderService` — gestiona creación y estado de órdenes. Depende de `DBPool` y `Cache`.
- `PaymentService` — procesa pagos. Depende de `DBPool`. Absolutamente crítico.
- `NotificationService` — envía emails/SMS. Depende de `DBPool` (para logs). Su fallo es tolerable.
- `ProductCatalog` — sirve el catálogo de productos. Depende de `Cache` (fuertemente: sin caché no puede operar).
- `MetricsCollector` — recoge métricas internas. Independiente de todo.
- `BackgroundJobRunner` — ejecuta tareas programadas. Depende de `DBPool`.

**Dependencias explícitas**:
```
DBPool ←── OrderService
DBPool ←── PaymentService
DBPool ←── NotificationService (opcional)
DBPool ←── BackgroundJobRunner
Cache  ←── OrderService (opcional: degrada gracefully sin cache)
Cache  ←── ProductCatalog (obligatorio)
```

**Tu tarea**:
1. Dibuja el árbol antes de codificarlo (en comentarios en el código)
2. Implementa todos los módulos Supervisor necesarios
3. Justifica cada estrategia elegida (`one_for_one`, `one_for_all`, `rest_for_one`)
4. Determina el `restart` type correcto para cada componente
5. ¿Qué pasa si `Cache` cae y `ProductCatalog` depende fuertemente de ella?

```elixir
defmodule ECommerce.Application do
  use Application

  def start(_type, _args) do
    children = [
      # Tu árbol aquí
    ]

    Supervisor.start_link(children, strategy: :one_for_one, name: ECommerce.Supervisor)
  end
end
```

### Hints

<details>
<summary>Hint 1 — Empieza por los failure domains</summary>

Identifica los grupos antes de escribir código:
- ¿Qué componentes son absolutamente críticos? (sistema no opera sin ellos)
- ¿Qué componentes son optativos? (sistema degrada pero no cae)
- ¿Qué componentes son de monitoreo? (auxiliares, independientes)

Cada grupo debería ser un supervisor separado.

</details>

<details>
<summary>Hint 2 — ProductCatalog y Cache</summary>

`ProductCatalog` depende fuertemente de `Cache`. Si `Cache` cae, `ProductCatalog` debe reiniciarse también (pierde sus conexiones). Esto sugiere `:rest_for_one` si están en el mismo supervisor, o `:one_for_all` en un subsupervisor que los agrupe.

Alternativa: `ProductCatalog` detecta que `Cache` está caída y retorna un error 503 hasta que se recupere, en lugar de hacer crash. Esto elimina la dependencia de supervisión pero requiere lógica de circuit-breaking.

</details>

<details>
<summary>Hint 3 — PaymentService como componente crítico aislado</summary>

`PaymentService` es el componente más crítico de negocio: su fallo representa pérdida directa de revenue. Considera aislarlo en su propio supervisor con `max_restarts` generoso para que se reinicie agresivamente sin comprometer otros servicios.

</details>

### One Possible Solution

<details>
<summary>Ver solución (intenta resolverlo primero)</summary>

```elixir
# Árbol diseñado:
#
# ECommerce.Supervisor (one_for_one)
# ├── ECommerce.InfrastructureSupervisor (rest_for_one)
# │   ├── ECommerce.DBPool                    :permanent
# │   └── ECommerce.Cache                     :permanent
# │
# ├── ECommerce.CoreBusinessSupervisor (one_for_one)
# │   ├── ECommerce.PaymentService            :permanent  ← aislado, crítico
# │   └── ECommerce.CatalogSupervisor (rest_for_one)
# │       ├── ECommerce.Cache (ref)            — ya iniciado, solo garantiza orden
# │       └── ECommerce.ProductCatalog         :permanent
# │
# ├── ECommerce.ExtendedBusinessSupervisor (one_for_one)
# │   ├── ECommerce.OrderService              :permanent
# │   └── ECommerce.BackgroundJobRunner       :permanent
# │
# └── ECommerce.AuxiliarySupervisor (one_for_one)
#     ├── ECommerce.NotificationService       :transient
#     └── ECommerce.MetricsCollector          :permanent

defmodule ECommerce.Application do
  use Application

  def start(_type, _args) do
    children = [
      # 1º: infraestructura — todo depende de esto
      ECommerce.InfrastructureSupervisor,
      # 2º: negocio core — depende de infra
      ECommerce.CoreBusinessSupervisor,
      # 3º: negocio extendido — depende de infra, tolera fallo de cache
      ECommerce.ExtendedBusinessSupervisor,
      # 4º: auxiliares — independientes, su fallo no afecta al core
      ECommerce.AuxiliarySupervisor,
    ]

    Supervisor.start_link(children,
      strategy: :one_for_one,  # dominios aislados entre sí
      name: ECommerce.Supervisor
    )
  end
end

defmodule ECommerce.InfrastructureSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      # rest_for_one: si DB cae, Cache también se reinicia
      # (Cache puede depender de DB para warmup o configuración)
      {ECommerce.DBPool, []},
      {ECommerce.Cache, []},
    ]

    Supervisor.init(children,
      strategy: :rest_for_one,
      max_restarts: 5,
      max_seconds: 30
    )
  end
end

defmodule ECommerce.CoreBusinessSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      # PaymentService: misión crítica, máxima tolerancia a reinicios
      %{
        id: ECommerce.PaymentService,
        start: {ECommerce.PaymentService, :start_link, [[]]},
        restart: :permanent,
      },
      # ProductCatalog depende fuertemente de Cache
      # Si Cache cae (gestionado en InfrastructureSupervisor), ProductCatalog
      # detectará la desconexión y hará crash → su supervisor lo reiniciará
      {ECommerce.ProductCatalog, []},
    ]

    # one_for_one: PaymentService y ProductCatalog son independientes entre sí
    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 10,
      max_seconds: 60
    )
  end
end

defmodule ECommerce.ExtendedBusinessSupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      {ECommerce.OrderService, []},
      {ECommerce.BackgroundJobRunner, []},
    ]

    Supervisor.init(children, strategy: :one_for_one)
  end
end

defmodule ECommerce.AuxiliarySupervisor do
  use Supervisor

  def start_link(opts) do
    Supervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    children = [
      %{
        id: ECommerce.NotificationService,
        start: {ECommerce.NotificationService, :start_link, [[]]},
        restart: :transient,  # no reiniciar si terminó normalmente (e.g., sin SMTP config)
      },
      {ECommerce.MetricsCollector, []},
    ]

    Supervisor.init(children,
      strategy: :one_for_one,
      max_restarts: 20,  # auxiliares pueden ser más ruidosos sin problema
      max_seconds: 60
    )
  end
end
```

</details>

---

## Exercise 2 — Startup Ordering con handle_continue

### Problem

`OrderService` necesita verificar al startup que la base de datos tiene las migraciones aplicadas antes de empezar a aceptar requests. Pero si hace un `GenServer.call` a `DBPool` en su `init/1`, puede crear un deadlock si `DBPool` aún no terminó de inicializarse.

```elixir
defmodule ECommerce.OrderService do
  use GenServer

  def start_link(opts) do
    GenServer.start_link(__MODULE__, opts, name: __MODULE__)
  end

  # PROBLEMA: si hacemos esto en init, puede deadlock
  def init(_opts) do
    # ¿Cómo verificamos que DB está lista SIN bloquear el supervisor?
    version = ECommerce.DBPool.migration_version()  # ← PELIGRO: puede deadlock
    if version < @required_version do
      {:stop, :migrations_pending}
    else
      {:ok, %{ready: true}}
    end
  end
end
```

**Tu tarea**:
1. Refactoriza `OrderService` para usar `handle_continue` y diferir la verificación de DB
2. El proceso debe quedar en estado `%{ready: false}` hasta que la verificación pase
3. Si la verificación falla (migraciones pendientes), el proceso debe terminar con error informativo
4. Implementa un `handle_call` que rechace requests mientras `ready: false`

```elixir
defmodule ECommerce.OrderService do
  use GenServer
  require Logger

  def start_link(opts), do: GenServer.start_link(__MODULE__, opts, name: __MODULE__)

  def create_order(params) do
    GenServer.call(__MODULE__, {:create_order, params})
  end

  def init(_opts) do
    # Retorna inmediatamente — supervisor puede continuar
    # Difiere la inicialización a handle_continue
    {:ok, %{ready: false}, {:continue, ???}}
  end

  def handle_continue(???, state) do
    # Tu implementación aquí
  end

  def handle_call({:create_order, params}, _from, %{ready: false} = state) do
    # Tu implementación aquí — rechazar si no está listo
  end

  def handle_call({:create_order, params}, _from, %{ready: true} = state) do
    # Tu implementación aquí — procesar la orden
  end
end
```

### Hints

<details>
<summary>Hint 1 — handle_continue ejecuta después de init</summary>

`{:ok, state, {:continue, message}}` retornado desde `init/1` (o `handle_call`, `handle_cast`, `handle_info`) hace que el GenServer procese `:message` inmediatamente después, antes de procesar mensajes de la mailbox. Para el momento en que `handle_continue` ejecuta, todos los children previos en la lista del supervisor ya están iniciados.

</details>

<details>
<summary>Hint 2 — Comunicar estado "not ready" al llamador</summary>

```elixir
def handle_call(_request, _from, %{ready: false} = state) do
  {:reply, {:error, :service_not_ready}, state}
end
```

Esto permite que los llamadores reciban un error claro en lugar de un timeout. En un endpoint HTTP, esto se traduce en un 503 Service Unavailable.

</details>

<details>
<summary>Hint 3 — Terminar limpiamente si las verificaciones fallan</summary>

```elixir
def handle_continue(:verify_db, state) do
  case check_migrations() do
    :ok ->
      Logger.info("OrderService ready")
      {:noreply, %{state | ready: true}}

    {:error, reason} ->
      Logger.error("OrderService startup failed: #{inspect(reason)}")
      {:stop, {:startup_failed, reason}, state}
  end
end
```

Cuando el proceso termina con `{:stop, reason, state}`, el supervisor lo verá como un crash y lo reiniciará (si es `:permanent`). Si las migraciones no están aplicadas, esto causará un loop de reinicios — lo que es correcto: el sistema debería quedarse "caído" hasta que las migraciones se apliquen.

</details>

---

## Exercise 3 — Failure Domains: Separar Crítico de No-Crítico

### Problem

Tu sistema actual tiene todos los servicios bajo un único supervisor. Un `MetricsCollector` buggy que crashea constantemente está derribando `PaymentService` cada vez que supera el `max_restarts` del supervisor compartido.

```elixir
# Situación actual — PROBLEMÁTICA
children = [
  ECommerce.DBPool,
  ECommerce.PaymentService,      # crítico
  ECommerce.OrderService,        # crítico
  ECommerce.MetricsCollector,    # buggy, crashea frecuentemente
  ECommerce.AuditLogger,         # auxiliar
]

Supervisor.init(children,
  strategy: :one_for_one,
  max_restarts: 3,     # cuando MetricsCollector crashea 3 veces en 5s...
  max_seconds: 5       # ...todo el supervisor se rinde
)
```

**Tu tarea**:
1. Refactoriza el árbol para que el fallo de `MetricsCollector` nunca afecte a `PaymentService`
2. Configura thresholds independientes para cada dominio
3. Implementa un mecanismo de observabilidad: cuando el supervisor auxiliar se rinde, loguea una alerta pero el sistema core sigue funcionando
4. Demuestra el comportamiento correcto con un proceso simulado que crashea frecuentemente

### Hints

<details>
<summary>Hint 1 — Supervisores como children de supervisores</summary>

Los supervisores pueden ser children de otros supervisores. El padre supervisa al hijo-supervisor, no a los workers directamente. Si el hijo-supervisor se rinde (supera su threshold), el padre lo reinicia.

```elixir
# El padre ve a InfrastructureSupervisor como un child más
children = [
  ECommerce.InfrastructureSupervisor,  # este a su vez tiene sus propios children
  ECommerce.AuxiliarySupervisor,
]
```

</details>

<details>
<summary>Hint 2 — Process.monitor para observabilidad</summary>

```elixir
# Un proceso sentinel puede monitorear al supervisor auxiliar
defmodule ECommerce.SupervisorSentinel do
  use GenServer

  def init(_) do
    Process.monitor(ECommerce.AuxiliarySupervisor)
    {:ok, %{}}
  end

  def handle_info({:DOWN, _ref, :process, _pid, reason}, state) do
    Logger.error("AuxiliarySupervisor went down: #{inspect(reason)}")
    # Alertar a PagerDuty, Sentry, etc.
    {:noreply, state}
  end
end
```

</details>

<details>
<summary>Hint 3 — Threshold diferenciado por supervisor</summary>

```elixir
# Supervisor crítico: thresholds conservadores (se rinde pronto → propaga alerta rápido)
Supervisor.init(critical_children,
  strategy: :one_for_one,
  max_restarts: 2,
  max_seconds: 10
)

# Supervisor auxiliar: thresholds permisivos (tolera workers ruidosos)
Supervisor.init(auxiliary_children,
  strategy: :one_for_one,
  max_restarts: 20,
  max_seconds: 60
)
```

</details>

---

## Common Mistakes

### Mistake 1 — Lista plana de children para sistemas complejos

```elixir
# MAL: todo en una lista
children = [
  DBPool, CachePool, OrderService, PaymentService,
  MetricsReporter, HealthChecker, BackgroundJobs,
  NotificationService, AuditLogger
]
Supervisor.init(children, strategy: :one_for_one, max_restarts: 3, max_seconds: 5)

# Un solo threshold gobierna todo.
# MetricsReporter buggy → todo el sistema se rinde.
# No hay dominios de fallo: todo está mezclado.
```

---

### Mistake 2 — Deps implícitas a través de nombres globales

```elixir
defmodule OrderService do
  def init(_) do
    # Asume que DBPool está disponible porque tiene un nombre global
    # Si DBPool no está iniciado, esto falla con :noproc en el peor momento
    pool = DBPool.checkout()
    {:ok, %{pool: pool}}
  end
end

# El orden en la lista del supervisor garantiza la disponibilidad,
# pero solo si OrderService está DESPUÉS de DBPool en children.
# Si alguien reordena la lista, la dependencia implícita se rompe silenciosamente.
```

Haz las dependencias explícitas en la documentación y en los health checks del startup.

---

### Mistake 3 — Business logic en init/1 que puede fallar por razones externas

```elixir
defmodule OrderService do
  def init(_) do
    # Consulta a una API externa durante el init
    {:ok, config} = ExternalConfigService.fetch_config()  # ← red puede estar caída
    {:ok, %{config: config}}
  end
end

# Si la API está caída al startup:
# → OrderService falla en init
# → El supervisor lo reintenta
# → Ciclo de reinicios hasta agotar max_restarts
# → El supervisor se rinde
# → La aplicación no arranca

# Solución: usa handle_continue para diferir calls externos,
# o arranca con config por defecto y actualiza en background
```

---

### Mistake 4 — Depender del orden de shutdown para cleanup

```elixir
# Los children se terminan en orden inverso al startup.
# Si OrderService necesita que DBPool esté disponible en su terminate/2:

def terminate(_reason, state) do
  # ¿DBPool está todavía disponible cuando esto ejecuta?
  # Depende del orden — y si alguien reordena children, se rompe
  DBPool.execute("INSERT INTO audit_log ...")
end

# Más seguro: hacer el cleanup de OrderService independiente de DBPool,
# o usar un timeout de shutdown generoso y gestionar la limpieza en el propio proceso
```

---

## Production Patterns

### Pattern 1 — Layered Architecture en el árbol de supervisión

```
Application
├── Layer 0: Foundation (Repo, Config, Telemetry)
├── Layer 1: Infrastructure (DBPool, Cache, MessageBroker)
├── Layer 2: Domain Services (OrderService, PaymentService)
├── Layer 3: Adapters (HTTPServer, GraphQL, gRPC)
└── Layer 4: Observability (Metrics, Tracing, HealthChecks)
```

Cada layer depende solo de las layers inferiores. El startup order es automáticamente correcto. El shutdown order es automáticamente el inverso.

### Pattern 2 — Child spec con dependencia explícita en opts

```elixir
defmodule OrderService do
  def child_spec(opts) do
    %{
      id: __MODULE__,
      start: {__MODULE__, :start_link, [opts]},
      restart: :permanent,
      # Documenta dependencias en el spec mismo:
      # depends_on: [DBPool, CachePool]  ← no es una opción real de OTP,
      # pero sirve como documentación
    }
  end
end
```

### Pattern 3 — Dynamic children con DynamicSupervisor para entidades

```elixir
defmodule ECommerce.OrderSupervisor do
  use DynamicSupervisor

  def start_link(opts) do
    DynamicSupervisor.start_link(__MODULE__, opts, name: __MODULE__)
  end

  def init(_opts) do
    DynamicSupervisor.init(strategy: :one_for_one)
  end

  def start_order_process(order_id) do
    spec = {ECommerce.OrderProcess, order_id}
    DynamicSupervisor.start_child(__MODULE__, spec)
  end

  def stop_order_process(order_id) do
    pid = ECommerce.OrderProcess.whereis(order_id)
    DynamicSupervisor.terminate_child(__MODULE__, pid)
  end
end
```

---

## Resources

- [OTP Design Principles — Supervisor Behaviour](https://www.erlang.org/doc/design_principles/sup_princ.html)
- [Elixir in Action, Ch. 8 — Fault Tolerance (Manning)](https://www.manning.com/books/elixir-in-action-third-edition)
- [Designing Elixir Systems with OTP — Pragmatic Programmers](https://pragprog.com/titles/jgotp/)
- [handle_continue — HexDocs](https://hexdocs.pm/elixir/GenServer.html#c:handle_continue/2)
- [DynamicSupervisor — HexDocs](https://hexdocs.pm/elixir/DynamicSupervisor.html)
