# 50. Build a Distributed Lock Service (ZooKeeper-like)

**Difficulty**: Insane

## Prerequisites

- Distributed Elixir: `Node.connect/1`, `:rpc`, `:global`, `:net_kernel`
- Mnesia o ETS distribuido: replicación de estado entre nodos
- GenServer distribuido: `{:via, :global, name}`, process groups con `:pg`
- Quorum y consenso básico: mayoría simple, split-brain scenarios
- Monitors cross-node: `Process.monitor/1` en procesos remotos
- Comprensión de los problemas del tiempo distribuido: relojes, TTLs, stale locks

## Problem Statement

Construye un servicio de locks distribuidos que garantice exclusión mutua en un cluster de Elixir, con leader election, lease-based expiration, y watches al estilo ZooKeeper.

El servicio funciona en un cluster de 3 nodos. Para adquirir un lock, un proceso envía una solicitud al servicio y espera confirmación. Solo un proceso en todo el cluster puede tener el lock a la vez. La garantía de exclusión mutua debe mantenerse incluso si los mensajes se reordenan o si un nodo cae durante la adquisición.

Los locks son **lease-based**: al adquirir un lock, el holder recibe un lease con TTL (por defecto 30 segundos). El holder debe renovar el lease periódicamente enviando heartbeats. Si el holder muere o se particiona sin renovar, el lock expira automáticamente y puede ser adquirido por otro proceso. Esto resuelve el problema del holder muerto que mantiene el lock indefinidamente.

El sistema usa **quorum**: para adquirir un lock, el cliente debe recibir confirmación de la mayoría de nodos (2 de 3). Esto garantiza que aunque un nodo caiga, el sistema sigue funcionando. Para liberar un lock, el holder también notifica al quorum. Ningún nodo individual puede conceder locks unilateralmente.

**Leader election** es el uso principal del lock service: N candidatos compiten por adquirir un lock especial `"leader-election"`; quien lo obtiene se convierte en líder; si el líder muere, su lease expira y los restantes candidatos compiten de nuevo.

Los **watches** permiten que procesos se suscriban a eventos sobre un lock específico: notificación cuando se adquiere, cuando se libera, o cuando expira. Esto evita polling activo y es la base del leader election reactivo.

## Acceptance Criteria

- [ ] Exclusión mutua: `LockService.acquire("resource", ttl: 30_000)` retorna `{:ok, lease}` o `{:error, :locked}`; en ningún momento dos procesos en el cluster pueden tener el mismo lock simultáneamente; esto debe verificarse con tests que lancen N procesos concurrentes intentando adquirir el mismo lock y confirmar que exactamente 1 lo obtiene
- [ ] Lease y heartbeat: el `lease` retornado contiene `{lock_name, holder_id, expires_at}`; el holder llama a `LockService.renew(lease)` antes de que expire para extender el TTL; `LockService.release(lease)` libera inmediatamente; si el holder no renueva antes de `expires_at`, el lock se libera automáticamente y cualquier otro proceso puede adquirirlo
- [ ] Reentrant locks: si el mismo proceso llama a `acquire` sobre un lock que ya posee, recibe `{:ok, lease}` con el mismo lease (o un lease renovado) en lugar de `{:error, :locked}`; la identidad del holder se basa en `self()` combinado con el nodo; el contador de reentradas debe decrementarse con cada `release` — el lock se libera cuando el contador llega a cero
- [ ] Fair locking (FIFO): si el lock está ocupado, `acquire` encola al solicitante; cuando el lock se libera, lo obtiene el proceso que esperaba desde hace más tiempo (no uno aleatorio); `acquire(name, timeout: 5_000)` espera hasta 5 segundos en la cola antes de retornar `{:error, :timeout}`; el orden FIFO se mantiene incluso cuando los solicitantes están en distintos nodos
- [ ] Leader election: `LockService.elect_leader(candidates, resource)` hace que cada candidato compita por el lock `resource`; retorna `{:leader, pid}` al candidato que gana y `{:follower, leader_pid}` a los demás; cuando el líder muere (su proceso termina o su nodo cae), la elección se repite automáticamente entre los candidatos supervivientes; el cambio de liderazgo no requiere reiniciar el servicio
- [ ] Watches: `LockService.watch(lock_name, events: [:acquired, :released, :expired])` registra al proceso actual para recibir mensajes `{:lock_event, lock_name, event, metadata}` cuando ocurren esos eventos; `LockService.unwatch(lock_name)` cancela el watch; los watches sobreviven a la pérdida temporal de conexión con el nodo que tiene el lock (se entregan cuando la conexión se restaura o se notifica `{:lock_event, name, :node_down, node}` si el nodo nunca vuelve)
- [ ] Quorum distribuido: el lock service corre en los 3 nodos del cluster como procesos replicados; `acquire` requiere respuesta afirmativa de al menos 2 nodos antes de confirmar; si un nodo cae durante la adquisición, el cliente reintenta con los nodos restantes; el sistema tolera la caída de 1 nodo sin pérdida de funcionalidad; detecta split-brain (2 nodos que creen ser mayoría independientemente) y rechaza operaciones en el lado minoritario
- [ ] Recovery automático: `Process.monitor` en el proceso holder detecta su muerte cross-node; al detectar `{:DOWN, ...}` del holder, el lock se marca como expirado inmediatamente si el TTL no ha vencido aún; los procesos en la cola de espera reciben notificación y el primero en la cola intenta adquirir; si el nodo entero cae (no solo el proceso), el recovery ocurre cuando el TTL expira naturalmente o cuando los nodos restantes alcanzan quorum sobre el estado

## What You Will Learn

- Exclusión mutua distribuida: por qué es fundamentalmente más difícil que en un solo proceso y las garantías que se pueden (y no se pueden) ofrecer
- Quorum y sus trade-offs: por qué la mayoría (no todos) es el umbral correcto y qué pasa en split-brain
- Lease-based locking vs. locks eternos: el rol del tiempo como árbitro de último recurso en sistemas distribuidos
- El problema del proceso holder muerto: monitors cross-node y los límites de la detección de fallos en redes particionadas
- Implementación de watches: de polling a push, y cómo gestionar el estado de subscripción en un sistema distribuido
- Fair locking distribuido: por qué una cola FIFO en un sistema distribuido requiere coordinación explícita de orden

## Hints

- Para el quorum: cada nodo tiene un `LockManager` GenServer; `acquire` envía `{:try_acquire, lock_name, holder, lease}` a los 3 nodos y espera respuesta con timeout; solo confirma si recibe `{:ok}` de 2 o más; si recibe conflicto de cualquiera, aborta y devuelve `{:error, :locked}`
- El mayor riesgo es el "dual grant": dos nodos aceptan independientemente si hay partición de red durante la fase de votación; mitígalo con épocas (epoch numbers) — cada nodo mantiene un epoch; un lock de epoch anterior es inválido
- Para la cola FIFO: mantén una lista ordenada de `{timestamp, caller_ref, holder_info}` en el estado del LockManager; usa timestamps de Erlang monotónico (`System.monotonic_time`) para el orden; cuando liberas, notifica al primero de la cola
- Los reentrant locks: guarda `{holder_pid, node, count}` como valor del lock; `acquire` desde el mismo `{self(), node()}` incrementa `count`; `release` decrementa; el lock solo se libera cuando `count == 0`
- Para watches cross-node: cuando un proceso remoto se suscribe a un watch, guarda `{remote_pid, remote_node}` y usa `Process.send/3` con opción `[:noconnect]`; si el nodo remoto no está disponible, encola el evento para entrega posterior o notifica el `node_down`
- Split-brain detection: cada nodo trackea cuántos nodos del cluster conoce; si el nodo ve menos de 2 nodos (incluyéndose a sí mismo), rechaza operaciones de adquisición para evitar ser minoría aceptando locks

## Reference Material

- ZooKeeper paper: "ZooKeeper: Wait-free coordination for Internet-scale systems" (Hunt et al., 2010)
- "How to do distributed locking" — Martin Kleppmann: https://martin.kleppmann.com/2016/02/08/how-to-do-distributed-locking.html
- Chubby paper: "The Chubby lock service for loosely-coupled distributed systems" (Burrows, Google, 2006)
- "Designing Data-Intensive Applications" — Martin Kleppmann, capítulos 8 y 9 (problemas del tiempo en sistemas distribuidos)
- Erlang `:global` module source: implementación de referencia de locks distribuidos en la BEAM
- "Distributed Systems for Fun and Profit" — Mikito Takada: http://book.mixu.net/distsys/

## Difficulty Rating ★★★★★★★★

La dificultad está en las garantías: es fácil construir un lock distribuido que funciona el 99% del tiempo; la dificultad está en el 1% — particiones de red durante adquisición, nodos que caen exactamente durante el quorum vote, relojes que divergen afectando los TTLs. Cada caso de esquina requiere una decisión de diseño consciente con trade-offs documentados entre disponibilidad y consistencia. El sistema completo debe tener invariantes formales verificables, no solo "parece funcionar".

## Estimated Time

40–60 horas
