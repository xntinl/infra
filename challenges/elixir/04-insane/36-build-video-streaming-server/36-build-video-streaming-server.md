# 36. Build a Video Streaming Server

**Difficulty**: Insane

## Prerequisites

- Elixir processes, GenServer, Supervisor trees
- TCP/HTTP servers with `:gen_tcp` or Plug/Bandit
- Binary pattern matching y bitstring manipulation
- File I/O y streaming con `File.stream!/3`
- Understanding of multimedia containers and codecs at a conceptual level
- HTTP/1.1 range requests (RFC 7233)
- Concurrency patterns: Task.async_stream, GenStage o Flow

## Problem Statement

Implementa un servidor de streaming de video adaptativo (ABR — Adaptive Bitrate) compatible con el protocolo HLS (HTTP Live Streaming, RFC 8216).

El servidor debe recibir un archivo de video fuente y realizar la segmentación en tiempo real o pre-procesada, sirviendo los segmentos a clientes HTTP. Cada cliente debe ser capaz de solicitar una variante de distinta calidad (bitrate) y cambiar de calidad dinámicamente en función del ancho de banda disponible, que el servidor simulará con delays configurables por conexión.

El servidor gestiona además un modo de live streaming donde los segmentos se generan continuamente (simulando una fuente de video en directo) y una sliding window limita cuántos segmentos están disponibles en la playlist.

Una capa de caché CDN simulada distribuye los segmentos entre múltiples "nodos" en memoria; las peticiones se dirigen al nodo más cercano (round-robin con pesos) y, en caso de cache miss, el nodo recupera el segmento del origen.

El servidor expone un endpoint de métricas en tiempo real: número de viewers activos, bandwidth total servido por segundo, y tasa de buffer stall (veces que un cliente esperó más de 2 segundos por el siguiente segmento).

## Acceptance Criteria

- [ ] HLS segmentation: el servidor divide un archivo de video binario en segmentos `.ts` de duración configurable (2–10 segundos); cada segmento es un chunk de bytes válido con timestamps correctos relativo al inicio del stream
- [ ] M3U8 playlist: genera `master.m3u8` con al menos tres variantes de bitrate (`#EXT-X-STREAM-INF`) y una `media.m3u8` por variante con los `#EXTINF` correctos; un cliente que siga la spec puede parsearlas sin errores
- [ ] Adaptive bitrate: un cliente simulado cambia de variante cuando el tiempo de descarga del último segmento supera 0.8× la duración del segmento (buffer en riesgo); el cambio ocurre en el siguiente segmento sin cortes
- [ ] Byte-range requests: el servidor responde correctamente a `Range: bytes=N-M` con status 206 y headers `Content-Range`; permite seeking arbitrario a cualquier posición del segmento
- [ ] Live streaming: el servidor produce un nuevo segmento cada N segundos (configurable), actualiza la `media.m3u8` con `#EXT-X-MEDIA-SEQUENCE` incremental y mantiene solo los últimos K segmentos en la sliding window; los segmentos expirados se purgan automáticamente
- [ ] CDN simulation: al menos 3 nodos CDN en memoria; el master playlist devuelve URLs con el hostname del nodo asignado; los nodos se comunican entre sí via mensaje de proceso para replicar segmentos; un nodo caído no interrumpe el streaming (failover al origen)
- [ ] Metrics: endpoint `/metrics` devuelve JSON con `viewers_active`, `bandwidth_bps`, `buffer_stall_rate`, `segments_served` actualizados cada segundo; los valores son consistentes con el tráfico real generado por los clientes de prueba

## What You Will Learn

- Manipulación de binarios de gran tamaño de forma eficiente en Elixir (sin copias innecesarias)
- HTTP chunked transfer encoding y streaming de respuestas
- Diseño de sistemas con múltiples procesos que comparten estado de forma concurrente (ETS para caché de segmentos)
- Implementación de protocolos de industria a partir de sus especificaciones (HLS RFC 8216)
- Simulación de condiciones de red adversas para probar resiliencia del sistema
- Sliding window y expiración de recursos con limpieza automática

## Hints

- Usa `:binary.part/3` para extraer chunks del video sin copiar todo el binario; mantén el binario completo en un `Agent` o ETS
- HLS requiere que los segmentos `.ts` empiecen en un keyframe; en la simulación, trata cada chunk de tamaño fijo como si fuera un keyframe
- La `media.m3u8` debe tener `#EXT-X-TARGETDURATION` igual al máximo `EXTINF` redondeado hacia arriba
- Para live streaming, un `GenServer` con `:timer.send_interval/2` produce segmentos periódicamente
- Los nodos CDN pueden ser procesos `GenServer` registrados en un `Registry` local; el routing usa un módulo separado con lógica de selección
- Para métricas, acumula contadores en ETS (operaciones atómicas con `:ets.update_counter/3`) en lugar de usar un proceso serializado
- El cliente simulado debe correr en un proceso separado y medir el wall-clock time de cada descarga

## Reference Material

- RFC 8216 — HTTP Live Streaming: https://www.rfc-editor.org/rfc/rfc8216
- MPEG-DASH ISO/IEC 23009-1 (para comparar el enfoque alternativo)
- Apple HLS authoring specification: https://developer.apple.com/documentation/http-live-streaming
- FFmpeg HLS muxer documentation (para entender los parámetros de segmentación)
- "Video Streaming Technology" — capítulos de ABR en Coursera/edX

## Difficulty Rating ★★★★★★★

Este ejercicio combina manipulación de binarios de bajo nivel, diseño de protocolos multimedia, sistemas distribuidos en memoria y métricas en tiempo real. La dificultad principal reside en mantener la coherencia de la playlist mientras múltiples clientes acceden concurrentemente, y en implementar el algoritmo ABR de forma que se comporte de manera predecible y verificable.

## Estimated Time

25–40 horas
