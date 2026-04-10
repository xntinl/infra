# 45. Build a Game Engine with ECS Architecture

**Difficulty**: Insane

## Prerequisites

- Elixir processes, GenServer, ETS para estado mutable de alta frecuencia
- Pattern matching avanzado y comprehensions
- Manipulación de strings y ANSI escape codes para rendering en terminal
- Timing preciso con `:timer.tc/1` y `:erlang.monotonic_time/1`
- Comprensión de game loops: fixed timestep, interpolación, delta time
- Input no-bloqueante del terminal (`:io.get_chars/2`, raw mode)
- Álgebra vectorial 2D básica: posición, velocidad, aceleración, colisiones AABB

## Problem Statement

Implementa un game engine con arquitectura Entity-Component-System (ECS) y úsalo para crear un juego jugable en la terminal.

ECS es un patrón de arquitectura de software orientado a datos que separa: **Entities** (identificadores únicos sin datos ni lógica), **Components** (datos puros sin lógica: posición, velocidad, sprite), y **Systems** (lógica pura sin datos propios: physics system, render system, input system). La ventaja de ECS es que las entities son composición de components, no herencia, lo que permite combinaciones arbitrarias con máxima flexibilidad.

El engine tiene un `World` como contenedor central: almacena todas las entities, sus components, y los systems registrados. El game loop corre a 60 updates/segundo con timestep fijo: si el sistema va lento, acumula actualizaciones hasta ponerse al día. El renderer interpola entre el estado del último update y el estado actual para evitar movimiento entrecortado.

El renderer de terminal usa ANSI escape codes para: mover el cursor, colores de primer plano y fondo, y limpiar líneas específicas (sin limpiar pantalla completa, que produce flickering). El input se lee en modo raw para detectar teclas sin esperar Enter.

El juego demo debe ser completo y jugable: no un tech demo, sino un juego con mecánicas, condición de victoria/derrota, y feedback visual claro.

## Acceptance Criteria

- [ ] ECS: `Entity` es un entero (`entity_id`); `Component` es cualquier struct con un tipo identificable; `System` es un módulo con callback `update(world, delta_time)`; las entities se crean con `World.spawn_entity/1`, se destruyen con `World.despawn/2`; las components se añaden con `World.add_component/3` y se eliminan con `World.remove_component/3`
- [ ] World: contenedor que mantiene `%{entity_id => %{component_type => component_value}}` en ETS para acceso O(1); `World.query(world, [:position, :velocity])` retorna todos los entity_ids que tienen todos esos component types, junto con los valores de cada component; `World.query_one(world, entity_id, [:position])` retorna los components de una entity específica
- [ ] Query: la query es el corazón del ECS; debe ser eficiente (no iterar todas las entities para cada system); implementa un índice por component type en ETS: `{component_type => MapSet.of_entity_ids}`; la intersección de índices da las entities candidatas; actualiza los índices al añadir/eliminar components
- [ ] Physics: `PhysicsSystem` actualiza `%Position{x, y}` aplicando `%Velocity{vx, vy}` × delta_time; aplica `%Gravity{g}` sumando a la velocidad Y; detecta colisiones AABB entre entities con `%Collider{width, height}` y `%Position{}`; en colisión, invierte la velocidad del eje correspondiente o aplica el callback de colisión definido en `%Collider{on_collision}`
- [ ] Rendering: `RenderSystem` dibuja entities con `%Sprite{char, fg_color, bg_color}` en su `%Position{}`; usa ANSI escape codes para posicionar el cursor (`\e[{row};{col}H`) y aplicar colores (`\e[3{n}m`); solo redibuja las celdas que cambiaron respecto al frame anterior (dirty tracking); el renderer mantiene un buffer doble para comparación
- [ ] Game loop: `Engine.run/1` inicia el loop con timestep fijo de 16.67ms (60 FPS); si un frame tarda más, acumula el tiempo y ejecuta múltiples `update` antes del siguiente `render`; el render muestra el tiempo real entre frames (actual FPS); el loop termina limpiamente al recibir señal de salida (`q` o Ctrl-C)
- [ ] Input: `InputSystem` lee teclas del terminal en modo raw (sin bloqueo, sin esperar Enter) usando `:io.get_chars/2` con timeout 0; mapea secuencias de escape ANSI para teclas de flecha (`\e[A`, `\e[B`, `\e[C`, `\e[D`); publica eventos de input a entities suscritas con `%InputListener{keys: [:arrow_up, :arrow_down]}`
- [ ] Demo game: un juego jugable completo que use el engine; opciones válidas: Snake (serpiente que crece al comer, game over al chocar con sí misma o los bordes), Pong (dos jugadores o vs CPU con IA simple), o Platformer básico (gravedad, plataformas, salto, coleccionables); el juego tiene: pantalla de inicio, gameplay, pantalla de game over con puntuación, y opción de reiniciar sin reiniciar el proceso

## What You Will Learn

- ECS como patrón de arquitectura orientado a datos y por qué es superior a la herencia para juegos
- Game loops correctos: el problema del timestep variable y por qué el timestep fijo es más predecible
- Rendering eficiente en terminal: ANSI codes, dirty tracking, double buffering en texto
- Input no-bloqueante: raw mode del terminal, secuencias de escape, event-driven input
- Física 2D básica: integración de Euler, detección de colisiones AABB, resolución de colisiones
- ETS como store de estado de alta frecuencia: por qué es mejor que un GenServer para acceso frecuente

## Hints

- Para el índice por component type: `ETS.insert(:component_index, {component_type, entity_id})`; query con `ETS.match(:component_index, {component_type, :"$1"})` devuelve todos los entity_ids con ese tipo; la intersección de múltiples resultados da las entities que tienen todos los components
- Terminal raw mode: `:io.setopts([:binary, {:encoding, :utf8}])`; para captura sin bloqueo, usa un proceso separado que llama a `:io.get_chars/2` con timeout y envía mensajes al InputSystem
- ANSI codes esenciales: `\e[2J` limpia pantalla, `\e[{r};{c}H` mueve cursor, `\e[0m` reset, `\e[3{n}m` color texto (0=negro, 1=rojo, 2=verde, 3=amarillo, 4=azul, 7=blanco)
- El dirty tracking: mantén `%{position => char}` del frame anterior; en el nuevo frame, solo emite ANSI codes para posiciones que cambiaron; esto elimina el flickering
- Timestep fijo: `accumulator = accumulator + elapsed`; `while accumulator >= FIXED_STEP, do: update(FIXED_STEP); accumulator -= FIXED_STEP`; luego `render(alpha: accumulator / FIXED_STEP)` para interpolación
- Para Snake: la serpiente es una lista de `{x, y}` donde la cabeza es el primero; mover = prepend nueva cabeza + drop last; comer = prepend nueva cabeza sin drop; colisión con uno mismo = `length(snake) != length(Enum.uniq(snake))`

## Reference Material

- "Game Programming Patterns" — Robert Nystrom (capítulo completo sobre ECS, libro gratuito online)
- CppCon 2018: Stoyan Nikolov — "OOP Is Dead, Long Live Data-Oriented Design"
- "Fix Your Timestep!" — Glenn Fiedler: https://gafferongames.com/post/fix_your_timestep/
- ANSI escape codes reference: https://en.wikipedia.org/wiki/ANSI_escape_code
- "Understanding Component-Entity-Systems" (blog post, T=Machine)
- Erlang `:io` module documentation para raw mode y get_chars

## Difficulty Rating ★★★★★★★

La dificultad no está en ningún componente individual sino en la integración correcta: el ECS con su sistema de queries indexadas, el game loop con timestep fijo y los problemas de timing en la BEAM, el rendering eficiente sin flickering en terminal, y el input no-bloqueante que funcione en distintos terminales. Hacer el juego genuinamente jugable y agradable añade un nivel de polish que requiere iteración continua.

## Estimated Time

30–45 horas
