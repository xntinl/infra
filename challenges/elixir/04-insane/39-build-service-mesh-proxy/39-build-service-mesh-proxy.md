# 39. Build a Service Mesh Proxy

**Difficulty**: Insane

## Prerequisites

- TCP proxy con `:gen_tcp` y manejo de conexiones concurrentes
- TLS/SSL con `:ssl` module de Erlang
- Generación de certificados X.509 con `:public_key`
- Circuit breaker pattern a nivel de implementación
- HTTP/1.1 parsing y forwarding
- Service discovery patterns (Registry, DNS-like)
- Observabilidad: métricas, tracing headers (W3C TraceContext)
- Configuración dinámica sin reiniciar procesos

## Problem Statement

Implementa un sidecar proxy que actúe como data plane de un service mesh. El proxy intercepta todo el tráfico de entrada y salida de un servicio, aplica políticas de red, y reporta telemetría detallada.

El proxy corre como un proceso Elixir que escucha en dos puertos: uno para tráfico de salida (outbound, interceptado de la aplicación local) y otro para tráfico de entrada (inbound, recibido de otros servicios). Cada conexión TCP es manejada por un proceso dedicado que aplica las políticas configuradas.

El mTLS (mutual TLS) se implementa generando certificados efímeros por servicio usando la API de `:public_key`. Cada servicio tiene su propio certificado firmado por una CA raíz del mesh; la identidad del servicio (SPIFFE-inspired) se codifica en el campo `Subject Alternative Name` del certificado.

El traffic shaping permite configurar rutas con pesos distintos (canary deployments): por ejemplo, 90% del tráfico va a `v1` y 10% a `v2` del mismo servicio. El proxy lleva registro de las decisiones de routing para verificar que los pesos se respetan estadísticamente.

## Acceptance Criteria

- [ ] TCP proxy: el proxy escucha en puerto configurable y hace forwarding bidireccional de bytes entre cliente y upstream; soporta conexiones concurrentes sin degradación; cada conexión tiene su propio proceso que muere cuando la conexión se cierra
- [ ] Service discovery: el proxy consulta un Registry (GenServer o ETS) para resolver `service_name` a una lista de `{host, port, weight, healthy}` endpoints; el Registry se actualiza dinámicamente sin reiniciar el proxy; endpoints no saludables se excluyen del balanceo
- [ ] Load balancing: round-robin weighted entre endpoints saludables; health checking activo (TCP probe cada 5s); un endpoint que falla N veces consecutivas se marca como unhealthy y se excluye hasta que pase el health check
- [ ] mTLS: genera certificado X.509 para cada servicio usando `:public_key`; el certificado incluye SAN con SPIFFE URI `spiffe://mesh.local/ns/default/sa/{service_name}`; el proxy presenta su certificado al conectar con otro servicio del mesh y verifica el del peer; conexiones sin certificado válido se rechazan
- [ ] Circuit breaker: por cada par `(source, destination)`, mantiene estado `open | closed | half_open`; en `closed`, pasa el tráfico; tras N fallos en ventana de T segundos, abre; en `open`, rechaza conexiones inmediatamente con error; tras timeout, pasa a `half_open` y permite una prueba; si tiene éxito, vuelve a `closed`
- [ ] Retries: en caso de fallo de conexión o timeout, reintenta hasta M veces con backoff exponencial; el retry budget limita los reintentos a máximo el X% del tráfico total (si hay muchos fallos, no amplifiques con reintentos); las solicitudes no-idempotentes (POST) no se reintentan por defecto
- [ ] Observability: por cada conexión, registra `{timestamp, source, destination, duration_ms, status, bytes_sent, bytes_received}`; propaga headers `traceparent` y `tracestate` (W3C TraceContext) añadiendo un span propio; endpoint `/stats` devuelve métricas agregadas por ruta
- [ ] Traffic shaping: configuración de rutas con pesos `[{endpoint_v1, 90}, {endpoint_v2, 10}]`; el proxy selecciona el destino usando weighted random sampling; al cambiar la configuración en caliente, los pesos se ajustan sin cortar conexiones existentes; las métricas confirman que la distribución real converge a los pesos configurados

## What You Will Learn

- Implementación de proxy TCP bidireccional eficiente con control de backpressure
- TLS mutuo y gestión de certificados X.509 programáticamente
- Circuit breaker como patrón de resiliencia: implementación correcta de la máquina de estados
- Distributed tracing: propagación de context headers entre servicios
- Traffic shaping y canary deployments a nivel de infraestructura
- Service mesh data plane vs control plane: separación de responsabilidades

## Hints

- Para el proxy TCP bidireccional: dos `Task.async` que leen de cada socket y escriben en el opuesto; cuando uno termina, envía señal al otro para cerrar
- `:ssl.connect/3` con `[certfile: path, keyfile: path, cacertfile: path, verify: :verify_peer]`; para generar certifcados en memoria usa `:public_key.pkix_sign/2`
- El circuit breaker es un GenServer por par `(source, destination)`; los estados se implementan como pattern matching en `handle_call`
- Para retry budget: usa un contador en ETS de "requests retried" vs "requests total" en ventana deslizante de 1 segundo; si el ratio supera el umbral, rechaza nuevos reintentos
- Weighted random: `Enum.random(Enum.flat_map(routes, fn {ep, w} -> List.duplicate(ep, w) end))` es simple pero costoso; para pesos grandes usa sum-of-weights + random
- W3C TraceContext: `traceparent: 00-{trace_id}-{parent_span_id}-{flags}`; genera `trace_id` de 16 bytes aleatorios y `span_id` de 8 bytes; propaga hacia upstream añadiendo tu span como `parent_span_id`

## Reference Material

- Envoy Proxy architecture documentation: https://www.envoyproxy.io/docs/envoy/latest/intro/arch_overview
- Istio data plane architecture: https://istio.io/latest/docs/ops/deployment/architecture/
- W3C TraceContext specification: https://www.w3.org/TR/trace-context/
- SPIFFE specification: https://spiffe.io/docs/latest/spiffe-about/spiffe-concept/
- "Service Mesh Patterns" — Christian Posta & Rinor Maloku (O'Reilly)
- RFC 8446 — TLS 1.3 (para contexto sobre el handshake mTLS)

## Difficulty Rating ★★★★★★★

La complejidad emerge de la integración de múltiples sistemas de seguridad y resiliencia que deben funcionar coordinadamente sin overhead perceptible. El mTLS con certificados generados programáticamente y el circuit breaker por par de servicios son especialmente difíciles de implementar correctamente.

## Estimated Time

30–45 horas
