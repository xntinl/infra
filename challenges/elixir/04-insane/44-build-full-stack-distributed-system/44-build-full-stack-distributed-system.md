# 44. Build a Full-Stack Distributed System

**Difficulty**: Insane

## Prerequisites

- Todos los ejercicios anteriores del nivel Insane (01-43) como base conceptual
- Erlang distribution: `Node.connect/1`, `:rpc`, `:global`
- HTTP servers, TCP sockets, binary protocols
- LSM-tree storage, ETS, DETS
- Raft consensus, leader election
- Telemetry y observabilidad avanzada
- Benchmarking con `:timer`, estadísticas de percentiles
- CLI con `OptionParser` y `Mix.Task`

## Problem Statement

Diseña e implementa un sistema distribuido completo de producción que integre todos los componentes de un backend moderno: API Gateway, coordinador distribuido con consenso Raft, capa de almacenamiento persistente, sistema de colas de jobs, procesador de streams en tiempo real, telemetría completa, y simulación multi-región.

Este es el ejercicio final integrador: no hay atajos ni componentes que puedas obviar. Cada componente debe estar diseñado con calidad de producción: manejo de errores exhaustivo, recuperación automática ante fallos, métricas detalladas, y documentación del protocolo entre componentes.

El sistema procesa el siguiente flujo: un cliente envía una petición al API Gateway → el Gateway autentica y aplica rate limiting → enruta al servicio correcto → el servicio usa el Storage o la Queue según el tipo de operación → los eventos de escritura se publican al Stream Processor → las métricas fluyen a Telemetry. Todo esto debe ocurrir con latencia P99 menor de 50ms a 50,000 requests/segundo en hardware de laptop (simulado con carga local).

La simulación multi-región añade latencia artificial de 50-150ms entre regiones y requiere que el sistema tome decisiones de routing geográfico: leer de la región más cercana, escribir en la primaria.

## Acceptance Criteria

- [ ] API Gateway: entrada única con autenticación JWT (verificación de firma, no generación); rate limiting por API key con token bucket en ETS; routing basado en path prefix a servicios internos; circuit breaker por servicio downstream; health endpoint que agrega el estado de todos los componentes
- [ ] Coordinator: cluster de 3 nodos con Raft-based leader election; el líder distribuye tareas a workers; si el líder cae, un follower es elegido en menos de 5 segundos; las tareas en vuelo del líder caído son re-asignadas; el coordinator expone `assign_task/2` y `get_task_status/1` como API
- [ ] Storage layer: LSM-tree para writes persistentes con WAL; ETS como L1 cache con TTL; lecturas sirven primero de cache, luego de LSM; `write_batch/2` es atómica (todo o nada); compaction background que no bloquea reads/writes; el storage soporta range scans por key prefix
- [ ] Queue: sistema de jobs con prioridades (high/normal/low); retry automático con backoff exponencial hasta N intentos; dead letter queue para jobs que fallan todos los reintentos; `enqueue/3`, `dequeue/1`, `ack/1`, `nack/1`; jobs no confirmados en T segundos vuelven a la queue (visibility timeout)
- [ ] Stream processor: ventana deslizante sobre eventos con window size configurable (time-based o count-based); operadores: `filter/2`, `map/2`, `reduce/3`, `join/3` (dos streams por key en ventana de tiempo); output a un sink configurable (función de callback o proceso); late events manejados con watermarks
- [ ] Metrics: `:telemetry` events en todos los componentes críticos; agregación en ventanas de 1s, 10s, 1m; cálculo de percentiles P50/P95/P99 con reservoir sampling; endpoint `/metrics` en formato Prometheus text exposition; alertas cuando P99 supera threshold configurable
- [ ] Tracing: trace distribuido a través de todos los componentes; cada request tiene un `trace_id` generado en el Gateway; cada componente añade un span con `{component, operation, duration_ms, status}`; endpoint `/traces/{trace_id}` muestra el árbol completo de spans; overhead del tracing menor del 5% del tiempo total
- [ ] CLI: `mix system.status` muestra estado de todos los componentes; `mix system.benchmark --rps 1000 --duration 30s` ejecuta la carga y reporta resultados; `mix system.region add --name eu-west --latency 100ms` añade una región; `mix system.drain` termina el sistema gracefully esperando que los jobs en vuelo completen
- [ ] Multi-region: dos "regiones" simuladas como grupos de procesos con latencia artificial entre ellas (`Process.sleep/1` en el middleware de comunicación inter-región); el Gateway hace geo-routing por un header `X-Region`; las escrituras van a la región primaria y se replican asíncronamente; las lecturas pueden ir a la región local (eventual consistency)
- [ ] Benchmark: un test automatizado envía 50,000 requests/segundo durante 30 segundos al sistema completo (Gateway → Storage → Queue en mezcla 70/30); el P99 de latencia end-to-end es menor de 50ms; el sistema no pierde ninguna request (0% error rate); el benchmark reporta throughput real, latencias y error rate

## What You Will Learn

- Arquitectura de sistemas distribuidos reales: cómo los componentes se integran y qué contratos tienen entre sí
- Operabilidad: métricas, tracing y CLI como ciudadanos de primera clase, no añadidos posteriores
- Geo-distribution: los tradeoffs de latency vs consistency en sistemas multi-región
- Benchmarking riguroso: medir latencias de percentiles correctamente, evitar el "coordinated omission" problem
- Supervisión de errores a escala: qué hacer cuando un componente falla y los demás continúan
- Por qué "distributed systems are hard": los casos de borde que solo aparecen bajo carga real

## Hints

- Empieza con un skeleton de todos los componentes corriendo (aunque no hagan nada) y añade funcionalidad incrementalmente; verifica el benchmark en cada etapa
- El Gateway puede ser un proceso Plug/Bandit simple; el rate limiting usa ETS con `update_counter` atómico
- Para el Raft simplificado, reutiliza conceptos del ejercicio 01 (distributed-raft-consensus) de este nivel
- El LSM-tree del ejercicio 15 (build-lsm-tree-storage-engine) es la base del Storage layer
- Para percentiles: usa reservoir sampling (mantén 1000 muestras aleatorias); ordena para calcular el percentil; `:array.set/3` de Erlang es bueno para el reservoir
- El "coordinated omission" en benchmarks: no midas solo el tiempo de respuesta; mide también el tiempo desde que la solicitud debería haberse enviado (incluye tiempo de espera en cola del cliente)
- Multi-región: define `defmodule Region.Transport` con `call(dest_region, message, timeout)`; la implementación de desarrollo añade el sleep; en producción sería una conexión TCP real

## Reference Material

- "Designing Data-Intensive Applications" — Martin Kleppmann (el libro de referencia para este ejercicio)
- "Building Microservices" — Sam Newman (arquitectura de sistemas distribuidos)
- Prometheus data model y text format: https://prometheus.io/docs/instrumenting/exposition_formats/
- W3C TraceContext specification (para el tracing distribuido)
- "How NOT to Measure Latency" — Gil Tene (sobre el coordinated omission problem)
- Raft paper: Ongaro & Ousterhout (2014). "In Search of an Understandable Consensus Algorithm"

## Difficulty Rating ★★★★★★★

Este es el ejercicio más difícil del curriculum. No por la complejidad de ningún componente individual (todos aparecen en ejercicios previos) sino por la integración: los componentes tienen dependencias circulares en sus responsabilidades, los fallos en un componente se propagan de formas inesperadas, y alcanzar el benchmark de 50k RPS con P99 < 50ms requiere optimización sistemática de todos los cuellos de botella.

## Estimated Time

60–100 horas
