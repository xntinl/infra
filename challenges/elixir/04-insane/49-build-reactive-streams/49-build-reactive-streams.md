# 49. Build a Reactive Streams Implementation

**Difficulty**: Insane

## Prerequisites

- GenServer, GenStage y el modelo de backpressure demand-driven de Elixir
- Procesos y mensajes: `send/receive`, monitors, links, `Process.flag/2`
- Comprensión de la Reactive Streams specification (reactive-streams.org)
- Concurrencia estructurada: supervisores, pools de procesos, scheduling
- Streams de Elixir y Enumerable protocol para entender qué se está reemplazando
- Manejo de errores en sistemas asíncronos: señales, traps, propagación

## Problem Statement

Implementa la especificación Reactive Streams completa en Elixir puro, sin depender de GenStage ni Flow, y demuestra su corrección pasando una suite de tests de compatibilidad inspirada en el Reactive Streams Technology Compatibility Kit (TCK).

Reactive Streams define cuatro interfaces: `Publisher` (produce datos), `Subscriber` (los consume), `Subscription` (el canal entre ambos con control de demanda), y `Processor` (es ambos). La pieza central es el **backpressure**: el subscriber controla cuántos elementos recibe llamando a `Subscription.request(n)`. El publisher nunca envía más elementos de los solicitados. Esto previene que un producer rápido destruya un consumer lento.

En Elixir, cada entidad es un proceso. El `Publisher` es un GenServer que mantiene una cola de subscribers y sus demandas pendientes. La `Subscription` es el par `{publisher_pid, subscriber_pid}`; `request(n)` envía un mensaje al publisher aumentando la demanda de ese subscriber. El publisher despacha elementos solo hasta agotar la demanda del subscriber receptor.

Los operadores son `Processor`s encadenados: `map` crea un proceso intermedio que solicita elementos al upstream, los transforma, y los reenvía al downstream cuando este lo demanda. El backpressure se propaga automáticamente hacia arriba: si el downstream no pide más, el operador no pide más al upstream.

Las observables **frías** (cold) inician su producción solo cuando hay un subscriber, y cada subscriber obtiene la secuencia completa desde el inicio. Las **calientes** (hot) producen independientemente y los subscribers ven solo los elementos emitidos desde que se suscribieron. Los `ConnectableObservable` son hot observables que se activan explícitamente con `connect/1` y permiten multicasting: múltiples subscribers reciben los mismos elementos.

## Acceptance Criteria

- [ ] Publisher: módulo con callback `subscribe(subscriber)` que inicia la `Subscription`; el publisher solo emite elementos cuando el subscriber ha solicitado demanda via `request(n)`; nunca envía más de N elementos por request; respeta la regla RS 1.1: `on_next` se llama a lo sumo `n` veces por `request(n)`; el publisher es un proceso GenServer que gestiona el estado de cada subscription de forma independiente
- [ ] Subscriber: callbacks `on_subscribe(subscription)`, `on_next(element)`, `on_error(reason)`, `on_complete()`; el subscriber llama a `Subscription.request(n)` dentro de `on_subscribe` para iniciar el flujo; no recibe ningún elemento hasta no llamar a `request`; `on_error` y `on_complete` son terminales — no se reciben más elementos después; el subscriber puede llamar a `Subscription.cancel()` en cualquier momento para detener el flujo
- [ ] Subscription: entidad que representa el enlace entre un publisher y un subscriber; `request(n)` incrementa la demanda del subscriber en el publisher (acumulación: si pide 3 y luego 5, el publisher puede emitir hasta 8 en total); `cancel()` hace que el publisher deje de enviar elementos a ese subscriber; la subscription debe ser thread-safe cuando múltiples procesos interactúan con ella
- [ ] Operadores: `map(publisher, f)`, `filter(publisher, pred)`, `flat_map(publisher, f)` (donde f retorna un publisher), `zip(pub_a, pub_b)`, `merge(list_of_publishers)`, `take(publisher, n)`, `drop(publisher, n)`; cada operador es un `Processor` (publisher + subscriber); los operadores preservan el backpressure — si el downstream pide 1, el upstream recibe demanda de 1
- [ ] Hot vs cold: `from_list(list)` y `from_range(range)` son cold — cada subscriber obtiene todos los elementos; `from_interval(ms)` es hot — emite ticks independientemente de los subscribers; `publish(publisher)` convierte cualquier publisher en conectable (hot multicast); `connect(connectable)` inicia la emisión; `auto_connect(n)` inicia cuando llegan N subscribers; `ref_count(connectable)` detiene cuando el último subscriber cancela
- [ ] Error handling: `on_error_return(publisher, default)` — si el publisher emite error, emite `default` y completa; `on_error_resume_next(publisher, fallback_publisher)` — en error, se suscribe al fallback y continúa; `retry(publisher, n)` — en error, re-suscribe al publisher hasta N veces; `retry_when(publisher, f)` — f recibe el error y retorna un publisher que controla cuándo reintentar
- [ ] Schedulers: `observe_on(publisher, scheduler)` — los callbacks `on_next/on_complete/on_error` se ejecutan en el scheduler especificado; `subscribe_on(publisher, scheduler)` — la suscripción y producción ocurren en el scheduler; schedulers disponibles: `Scheduler.immediate` (proceso actual), `Scheduler.new_process` (spawn por elemento), `Scheduler.pool(n)` (pool de N workers via Task.Supervisor)
- [ ] TCK compliance: suite de tests que verifican las reglas de la RS spec; mínimo obligatorio: publisher no envía más elementos de los solicitados, subscriber no recibe elementos antes de `request`, `on_complete` y `on_error` son mutuamente exclusivos y terminales, `cancel` detiene el flujo sin errores, la acumulación de demand es correcta, operadores preservan backpressure end-to-end

## What You Will Learn

- Backpressure como contrato explícito: por qué los sistemas push sin control de demanda fallan bajo carga
- La dificultad de implementar specs formales: cada regla tiene implicaciones en la implementación del proceso
- Procesadores encadenados: cómo el backpressure se propaga automáticamente a través de una cadena de operadores
- Hot vs cold observables: dos modelos de producción fundamentalmente distintos y cuándo usar cada uno
- Schedulers y concurrencia controlada: mover trabajo entre procesos de forma predecible sin race conditions
- Diferencia entre GenStage (que ya implementa esto) y construirlo desde cero para entender los trade-offs

## Hints

- Empieza por la `Subscription` como par de pids y un contador de demanda en un Agent; el publisher consulta la demanda antes de emitir y la decrementa al enviar
- Para el backpressure en operadores: el `map` processor solo llama a `upstream.request(n)` cuando su propio subscriber llama a `self.request(n)` — la demanda fluye de abajo a arriba
- `flat_map` es el operador más difícil: debes gestionar múltiples subscripciones internas concurrentes y aplanar sus salidas mientras respetas la demanda del downstream
- Para los ConnectableObservable: usa un proceso "hub" que mantiene una lista de subscribers activos y replica cada `on_next` a todos; los subscribers se registran en el hub, no en el publisher original
- El TCK tiene reglas sutiles: por ejemplo, un publisher debe ser capaz de entregar Long.MAX_VALUE elementos si el subscriber los solicita — asegúrate de que el tipo de demanda no haga overflow con enteros grandes
- Para `retry`: no re-suscribas en el mismo proceso que recibió el error; usa un proceso coordinador que maneje la lógica de reintento e intermedie entre el publisher y el subscriber real

## Reference Material

- Reactive Streams Specification: https://www.reactive-streams.org (leer las reglas formales, no solo la intro)
- Reactive Streams TCK: https://github.com/reactive-streams/reactive-streams-jvm/tree/master/tck
- RxJava source: operadores en `io.reactivex.rxjava3.internal.operators`
- GenStage source code: cómo Elixir resuelve el mismo problema (para comparar, no copiar)
- "Reactive Design Patterns" — Roland Kuhn, Brian Hanafee, Jamie Allen
- "Your Mouse is a Database" — Erik Meijer (paper original sobre FRP y composición de eventos)

## Difficulty Rating ★★★★★★★

La dificultad está en la corrección formal: la RS spec tiene más de 40 reglas explícitas, y violar cualquiera genera comportamiento no determinista bajo carga. La gestión de demanda acumulada entre procesos concurrentes, los operadores que propagan backpressure correctamente, y los hot observables con multicasting correcto son cada uno sistemas no triviales. Integrarlos sin race conditions bajo alta concurrencia requiere pensar en invariantes de proceso, no solo en flujo de datos.

## Estimated Time

35–50 horas
