# 41. Build an Event Sourcing + CQRS Framework

**Difficulty**: Insane

## Prerequisites

- GenServer avanzado, Registry, DynamicSupervisor
- Diseño de domain models con Elixir structs
- Persistencia con DETS o archivos (para el event store)
- Pattern matching exhaustivo y guard clauses
- Comprensión de Domain-Driven Design (Aggregate, Command, Event)
- Concurrencia y ordering garantías (mailbox de GenServer)
- JSON o Erlang term serialization para eventos

## Problem Statement

Diseña e implementa un framework de Event Sourcing y CQRS (Command Query Responsibility Segregation) en Elixir que permita construir sistemas orientados a eventos con consistencia fuerte en writes y eventual consistency en reads.

En Event Sourcing, el estado del sistema no se almacena directamente: en su lugar, se almacena la secuencia de eventos que llevaron a ese estado. Para obtener el estado actual de un aggregate, se reproducen todos sus eventos desde el inicio. El event store es un log append-only particionado por aggregate ID.

CQRS separa las operaciones de escritura (Commands) de las de lectura (Queries). Los Commands pasan por el Command Handler que los valida, carga el aggregate, aplica el comando, y emite eventos. Las Queries leen directamente de los Read Models (projections), que son vistas desnormalizadas y optimizadas para lectura, actualizadas asincrónicamente cuando llegan nuevos eventos.

Los Process Managers (Sagas) coordinan flujos de negocio que involucran múltiples aggregates: reaccionan a eventos y emiten nuevos commands, implementando workflows de larga duración con compensación en caso de fallo.

El framework debe ser genérico (usable con distintos dominios de negocio) y proveer macros o behaviours para simplificar la definición de aggregates, commands, events y projections.

## Acceptance Criteria

- [ ] Event store: append-only log de eventos persistido (DETS o archivo binario); cada evento tiene `{stream_id, sequence_number, event_type, payload, timestamp, metadata}`; `append/3` garantiza que el sequence_number es consecutivo (optimistic locking: si el expected_version no coincide con el actual, devuelve `{:error, :version_conflict}`); `read_stream/2` retorna eventos en orden
- [ ] Aggregate: behaviour con callbacks `init/0`, `apply/2` (state × event → state), `handle/2` (state × command → {:ok, [events]} | {:error, reason}); el framework carga el aggregate desde el event store, reproduce los eventos sobre el estado inicial, y llama a `handle/2`; el estado no se persiste directamente
- [ ] Command handler: recibe un command struct, resuelve el aggregate ID, carga el aggregate (replay de eventos), delega a `Aggregate.handle/2`, guarda los nuevos eventos en el event store (con version check), y publica los eventos al event bus para notificar projections y process managers
- [ ] Process manager: behaviour que reacciona a eventos y puede emitir commands; mantiene su propio estado persistido en el event store (como un aggregate especial); implementa compensating commands para rollback en caso de fallo parcial; el ejemplo debe incluir una saga de "Order fulfillment" (orden → reservar stock → cobrar → confirmar)
- [ ] Projections: behaviour que suscribe a tipos de eventos y actualiza un read model (mapa en ETS o GenServer); las projections son idempotentes (procesar el mismo evento dos veces produce el mismo estado); el framework garantiza at-least-once delivery con deduplication por `{stream_id, sequence_number}`
- [ ] Snapshots: cuando un aggregate tiene más de N eventos (configurable), el framework guarda un snapshot `{state, version}` en un store separado; la próxima carga del aggregate lee el snapshot y solo reproduce eventos posteriores al snapshot; el número de eventos reproducidos en cada carga es O(eventos desde último snapshot), no O(total eventos)
- [ ] Event versioning: mecanismo de upcasting; al leer un evento versión 1, si existe un upcast definido para `{:event_type, 1}`, se transforma al schema versión 2 antes de pasarlo al aggregate; los aggregates solo conocen la versión actual; ejemplo: `UserEmailChanged` v1 con campo `email`, v2 con `email` + `previous_email`
- [ ] Eventual consistency: las projections no se actualizan en la misma transacción del command; un `ProjectionSupervisor` ejecuta las projections asíncronamente; el framework ofrece `wait_for_projection/2` para tests que necesiten consistencia fuerte; en producción, el cliente acepta leer datos potencialmente desactualizados
- [ ] Replay: función `rebuild_projection/1` que descarta el estado actual de una projection y reproduce todos los eventos del store desde el principio; se ejecuta en background sin bloquear el sistema; mientras el rebuild está en curso, las queries devuelven el estado anterior (stale read)

## What You Will Learn

- Event Sourcing como alternativa al CRUD: ventajas (audit log, time travel debugging) y desventajas (complejidad de projections)
- CQRS: por qué separar reads y writes tiene sentido en sistemas con carga asimétrica
- Optimistic locking sin transacciones de base de datos: version vectors en el event store
- Sagas como patrón para mantener consistencia en sistemas distribuidos sin 2PC
- Event schema evolution: el desafío de cambiar el formato de eventos en un sistema en producción
- Eventual consistency en la práctica: qué garantías ofrecer al cliente, cuándo es aceptable

## Hints

- El event store por aggregate puede ser un archivo DETS por stream_id o un único DETS con `{stream_id, seq}` como key
- Para el optimistic locking: `append(stream_id, events, expected_version)` — lee el version actual del stream, si coincide con `expected_version` añade; la operación debe ser atómica (GenServer serializa esto)
- Los aggregates como GenServer: carga lazy al primer acceso, timeout para descargar del memory tras inactividad; un `DynamicSupervisor` gestiona el ciclo de vida
- Para el behaviour de Aggregate: `use EventSourcing.Aggregate` genera las funciones boilerplate; el módulo solo define `init/0`, `apply/2` y `handle/2`
- El event bus puede ser `Phoenix.PubSub` o un Registry propio; las projections se suscriben a topics `event:{EventType}`
- Para snapshots: guarda en un store separado `{stream_id => {snapshot_state, snapshot_version}}`; al cargar, comprueba si hay snapshot antes de hacer el full replay

## Reference Material

- "Implementing Domain-Driven Design" — Vaughn Vernon (capítulos sobre Aggregates y Events)
- Greg Young — CQRS and Event Sourcing documentation: https://cqrs.files.wordpress.com/2010/11/cqrs_documents.pdf
- EventStore database documentation (para entender los garantías del append-only log)
- "Versioning in an Event Sourced System" — Greg Young (e-book gratuito)
- "Domain Modeling Made Functional" — Scott Wlaschin (capítulos relevantes en F# pero aplicable a Elixir)

## Difficulty Rating ★★★★★★★

El framework debe ser genérico y expresivo sin sacrificar correctness. La combinación de optimistic locking, eventual consistency, sagas con compensación, y event versioning crea una matriz de casos de borde que son individualmente manejables pero colectivamente desafiantes. La implementación de snapshots de forma transparente al aggregate es el criterio técnicamente más sutil.

## Estimated Time

35–50 horas
