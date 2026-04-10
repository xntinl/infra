# 37. Build a Blockchain Simulation

**Difficulty**: Insane

## Prerequisites

- Elixir GenServer, processes, message passing
- Criptografía: `:crypto` module (SHA-256, ECDSA)
- Concurrencia y coordinación entre nodos Erlang distribuidos
- Estructuras de datos inmutables y pattern matching avanzado
- Comprensión del modelo UTXO o account-based a nivel conceptual
- Base64 encoding/decoding, binary serialization

## Problem Statement

Implementa una simulación funcional de blockchain con mecanismo de consenso Proof of Work y una criptomoneda simple con wallets ECDSA.

El sistema consta de múltiples nodos (procesos Erlang en el mismo nodo o en nodos distribuidos) que mantienen cada uno su propia copia de la cadena. Los nodos se comunican peer-to-peer para compartir nuevos bloques y transacciones. Cuando un nodo mina un bloque válido, lo anuncia al resto; los demás lo validan y, si es correcto, lo añaden a su cadena. Los forks se resuelven con la regla "longest chain wins".

Cada bloque contiene un conjunto de transacciones del mempool. Las transacciones son transferencias de moneda entre wallets. Cada wallet tiene un par de claves ECDSA (secp256k1); las transacciones van firmadas por el remitente y los nodos verifican las firmas antes de añadirlas al mempool.

El minado ocurre en un proceso separado que itera nonces hasta encontrar un hash SHA-256 con N ceros iniciales. La dificultad N es configurable y se puede ajustar para que el tiempo de minado promedio sea aproximadamente 10 segundos por bloque.

## Acceptance Criteria

- [ ] Block structure: cada bloque contiene `%Block{index, timestamp, transactions, previous_hash, nonce, hash}` donde `hash = SHA256(SHA256(block_data))` y `previous_hash` referencia el bloque anterior; el bloque génesis tiene `previous_hash = "0000...0000"`
- [ ] Proof of Work: el hash del bloque (en hexadecimal) debe empezar con exactamente N caracteres `"0"` donde N es la dificultad actual; el sistema verifica esto antes de aceptar cualquier bloque de un peer
- [ ] Chain validation: una función `validate_chain/1` recorre la cadena completa verificando que cada bloque referencia correctamente al anterior y que su PoW es válido; devuelve `{:ok, chain}` o `{:error, reason, block_index}`
- [ ] Transaction pool: el mempool acepta transacciones nuevas, rechaza duplicados (por tx hash) y transacciones con firma inválida; las transacciones se seleccionan para incluir en bloques por fee (las de mayor fee primero)
- [ ] Mining: el proceso de minado corre en un proceso separado, puede cancelarse cuando llega un bloque nuevo de un peer (evitar trabajo inútil); al encontrar un nonce válido, anuncia el bloque a todos los peers conocidos
- [ ] Peer network: cada nodo mantiene una lista de peers; cuando recibe un bloque, lo valida y lo re-anuncia si es nuevo; la red converge al mismo estado tras N segundos de inactividad (verificable con una función `network_consistent?/1`)
- [ ] Consensus: cuando dos nodos minan bloques al mismo tiempo (fork), ambos propagan su versión; tras recibir la cadena más larga válida del peer, el nodo descarta su fork y adopta la cadena ganadora, devolviendo las transacciones huérfanas al mempool
- [ ] Wallet: genera par de claves ECDSA con `:crypto.generate_key/2` (curva `:secp256k1`); `sign_transaction/2` produce una firma DER; `verify_transaction/2` verifica la firma usando la clave pública del remitente; el address es el hash SHA-256 de la clave pública en hexadecimal

## What You Will Learn

- Uso práctico de criptografía asimétrica en Elixir con el módulo `:crypto`
- Diseño de sistemas distribuidos sin coordinador central (P2P)
- Estructuras de datos encadenadas con integridad criptográfica
- Resolución de conflictos y convergencia en sistemas eventualmente consistentes
- Serialización/deserialización de estructuras binarias para protocolos de red
- Proof of Work como mecanismo de coordinación sin confianza

## Hints

- `:crypto.hash(:sha256, data)` devuelve binario; usa `Base.encode16/1` para convertir a hex
- Para ECDSA, `:crypto.sign(:ecdsa, :sha256, data, [private_key, :secp256k1])` y `:crypto.verify/5`
- La dificultad de 4 ceros (N=4) tarda ~1-5 segundos en hardware moderno; empieza con N=2 para pruebas
- Usa `Task.async/1` para el proceso de minado y `Task.shutdown/2` para cancelarlo cuando llega un bloque
- Para simular la red sin nodos Erlang distribuidos, usa procesos registrados en un Registry local; el "broadcast" es un `Enum.each(peers, &send(&1, {:new_block, block}))`
- El mempool puede vivir en un GenServer separado del nodo; la sincronización del mempool entre peers es opcional pero realista
- Para detectar forks: compara `length(my_chain)` vs `length(peer_chain)`; si el peer tiene más bloques y todos son válidos, adopta su cadena

## Reference Material

- Bitcoin whitepaper: Nakamoto, S. (2008). "Bitcoin: A Peer-to-Peer Electronic Cash System"
- "Mastering Bitcoin" — Andreas M. Antonopoulos (capítulos 6-10 sobre consenso y mining)
- Ethereum Yellow Paper (para comparar el modelo de account-based vs UTXO)
- RFC 5480 — Elliptic Curve Cryptography (ECC) key structure
- Erlang `:crypto` module documentation — sección ECDSA

## Difficulty Rating ★★★★★★★

La mayor dificultad no está en el PoW sino en la coordinación entre nodos: detectar y resolver forks correctamente, gestionar el mempool durante reorganizaciones de cadena, y garantizar que la red converge. El comportamiento emergente de múltiples nodos minando concurrentemente genera casos de borde difíciles de anticipar.

## Estimated Time

30–45 horas
