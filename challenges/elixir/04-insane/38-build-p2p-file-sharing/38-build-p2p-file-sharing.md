# 38. Build a P2P File Sharing System

**Difficulty**: Insane

## Prerequisites

- TCP sockets con `:gen_tcp`, binary protocol design
- Hashing con `:crypto` (SHA-1, SHA-256)
- Distributed Hash Tables conceptualmente (Kademlia)
- Concurrencia avanzada: GenServer, Task, Registry, DynamicSupervisor
- Binary pattern matching para parseo de protocolos
- Rate limiting y control de flujo (backpressure)
- Comprensión básica de redes P2P y NAT traversal

## Problem Statement

Implementa un sistema de compartición de archivos P2P inspirado en BitTorrent. El sistema debe poder fragmentar archivos en piezas, distribuirlas entre múltiples peers, y permitir que un cliente descargue el archivo completo desde múltiples fuentes simultáneamente.

La red de peers se organiza mediante una DHT Kademlia simplificada: cada nodo tiene un ID de 160 bits y los peers se descubren mediante lookups en el espacio de claves, sin necesidad de un tracker central. Cada archivo se identifica por su `info_hash`, que es el SHA-1 del conjunto de metadatos del archivo (nombre, tamaño, piezas, piece_length).

El protocolo entre peers incluye negociación de qué piezas tiene cada uno (bitfield), solicitud de piezas específicas, y transferencia de bloques de datos. El algoritmo de selección de piezas prioriza las piezas más raras en la red (rarest-first) para maximizar la disponibilidad total. El choking algorithm controla a qué peers se les envían datos para maximizar la reciprocidad (unchoke a los que más te envían a ti).

Cuando quedan pocas piezas por descargar (endgame), el cliente las solicita simultáneamente a múltiples peers para evitar que una pieza lenta retrase la descarga completa.

## Acceptance Criteria

- [ ] DHT Kademlia: cada nodo tiene un ID de 160 bits aleatorio; implementa `find_node/2` y `find_value/2` siguiendo el algoritmo de lookup iterativo de Kademlia; la k-bucket table se actualiza correctamente al conocer nuevos nodos; peers se descubren sin tracker central
- [ ] Metadata: dada una lista de archivos (o un archivo), genera un mapa de metadatos con `info_hash`, lista de piezas con su SHA-1 individual, `piece_length` configurable (256KB–4MB), y nombre/tamaño total; el `info_hash` es SHA-1 del bencoding del info dict
- [ ] Piece selection: implementa rarest-first seleccionando la pieza con menos availabilidad entre los peers conectados; en caso de empate, selección aleatoria; las piezas solicitadas pero no recibidas en 30s se re-solicitan a otro peer
- [ ] Peer protocol: handshake de 68 bytes (pstr, reserved, info_hash, peer_id); mensajes: `bitfield` al conectar, `have` al completar pieza, `request(index, begin, length)` para pedir bloque, `piece(index, begin, data)` para enviar bloque; `choke`/`unchoke` para control de flujo
- [ ] Choking: cada 10 segundos, unchoke a los 4 peers que más datos han enviado (tit-for-tat); un slot adicional de "optimistic unchoke" rotado cada 30s para descubrir nuevos peers rápidos; peers chokeados no reciben bloques pero sí `have` y `bitfield`
- [ ] Endgame: cuando quedan menos del 5% de piezas, solicita cada pieza pendiente a todos los peers que la tienen; cancela las solicitudes duplicadas en cuanto se recibe la primera respuesta; la descarga completa verifica SHA-1 de cada pieza antes de marcarla como válida
- [ ] Bandwidth management: rate limiting configurable por peer (KB/s upload y download) y global; implementado con token bucket; las descargas no deben exceder el límite configurado en ventana de 1 segundo
- [ ] Integrity: al recibir una pieza completa, verifica su SHA-1 contra los metadatos; si no coincide, descarta la pieza, penaliza al peer (desconexión temporal) y la re-solicita; el archivo final se ensambla ordenando las piezas y verificando el SHA-1 global

## What You Will Learn

- Diseño e implementación de protocolos binarios de bajo nivel en Elixir
- Distributed Hash Tables: el algoritmo Kademlia y por qué funciona
- Algoritmos de optimización de distribución (rarest-first, tit-for-tat)
- Gestión de concurrencia a gran escala: muchas conexiones TCP simultáneas
- Rate limiting con token bucket para control de ancho de banda
- Verificación de integridad criptográfica en transferencias por red

## Hints

- Los peers en la simulación pueden ser procesos Erlang locales que se comunican via mensaje en lugar de TCP real; usa una capa de abstracción `Transport` que funcione con ambos
- Kademlia simplificado: implementa solo `find_node` y `store/find_value`; omite la republishing y el node eviction complejo en primera iteración
- El bitfield se puede representar como un `MapSet` de piezas disponibles; para el protocolo de red, serializa como bitstring con `for i <- 0..(num_pieces-1), into: <<>>, do: <<if(i in available, do: 1, else: 0)::1>>`
- Para rarest-first: mantén un mapa `%{piece_index => count_of_peers_with_it}` actualizado con cada `have` recibido
- El token bucket: usa un GenServer con un contador de tokens que se recarga cada segundo; `consume(n)` bloquea si no hay suficientes tokens
- En endgame, usa `Task.async_stream/3` para solicitar la pieza a múltiples peers y toma el primer resultado con `Task.yield_many/2`

## Reference Material

- BitTorrent protocol specification: http://www.bittorrent.org/beps/bep_0003.html
- BEP 5 — DHT Protocol: http://www.bittorrent.org/beps/bep_0005.html
- Kademlia paper: Maymounkov, P. & Mazières, D. (2002). "Kademlia: A Peer-to-peer Information System Based on the XOR Metric"
- "BitTorrent Economics Paper" — Bram Cohen (choking algorithm rationale)
- BEP 12 — Multitracker Metadata Extension (para contexto adicional)

## Difficulty Rating ★★★★★★★

Este ejercicio requiere implementar tres sistemas complejos independientes que interactúan: la DHT para descubrimiento, el protocolo de transferencia entre peers, y los algoritmos de optimización (rarest-first, choking). La dificultad está en que cada uno de estos sistemas tiene sus propios casos de borde y la integración entre ellos multiplica la complejidad.

## Estimated Time

35–50 horas
