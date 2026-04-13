# Taxonomía profesional para challenges de Elixir — Investigación y propuesta

Documento de investigación para reorganizar ~557 archivos `.md` bajo
`/Users/consulting/Documents/consulting/infra/challenges/elixir/`. No se
modifica nada del repo aquí; este archivo es la base de decisión.

---

## 1. Resumen de convenciones encontradas

| Fuente | Estructura / agrupación | Naming | Numeración | Ejemplo |
|---|---|---|---|---|
| [awesome-elixir](https://github.com/h4cc/awesome-elixir) | ~84 categorías por dominio funcional (OTP, Testing, HTTP, JSON, Macros, Release Management, Caching, Authentication, etc.) | Títulos humanos, URL kebab-case | Sin números | `Actors`, `OTP`, `Release Management`, `ORM and Datamapping` |
| [Elixir School](https://elixirschool.com/en) | 8 secciones top: Basics, Intermediate, Advanced, Testing, Data Processing, Ecto, Storage, Miscellaneous | Nombres cortos | Lecciones numeradas con nombre: `1. Basics`, `2. Collections` | `Basics / Pattern Matching`, `Advanced / OTP Supervisors` |
| [Exercism Elixir](https://github.com/exercism/elixir) | Dos buckets: `concept/` y `practice/` | Puro kebab-case, 1–3 palabras, nombre-problema en vez de descripción técnica | Sin prefijo numérico; el orden curricular lo define `config.json` | `lasagna`, `pacman-rules`, `guessing-game`, `rpg-character-sheet`, `roman-numerals` |
| [Hexdocs Elixir stdlib](https://hexdocs.pm/elixir/api-reference.html) | Agrupación por dominio: Kernel & Language, Data Structures, Collections & Iteration, Protocols, Date & Time, Concurrency & Processes, I/O & System, Utilities, Exceptions | `PascalCase` módulo | Sin números | `Enum`, `Stream`, `GenServer`, `DynamicSupervisor`, `Registry` |
| [Elixir Style Guide](https://github.com/christopheradams/elixir_style_guide) | N/A (guía, no taxonomía) | Módulos `CamelCase`; archivos y atoms `snake_case`; predicados con `?`; bang `!` | — | `MyApp.UserRegistry`, `is_even?/1` |
| [Go by Example](https://gobyexample.com/) | Lista plana ordenada curricularmente | Títulos humanos cortos | Sin número visible; orden lo define el índice | `Hello World`, `Closures`, `Goroutines`, `Channel Buffering` |
| [Rust by Example](https://doc.rust-lang.org/rust-by-example/) | 24 capítulos por dominio | Títulos cortos, mdBook define el orden | Sin número visible | `Primitives`, `Custom Types`, `Flow of Control`, `Error handling` |
| [LeetCode](https://leetcode.com) / [Codewars](https://docs.codewars.com/authoring/guidelines/kata/) | Catálogo plano + tags + dificultad | Slug kebab-case derivado del título, 1–5 palabras | ID numérico interno (oculto) separado del slug | `two-sum`, `longest-substring-without-repeating-characters` |

Patrón dominante: **kebab-case, inglés, 1–4 palabras, agrupado por dominio
técnico (no por dificultad en el path), sin números como identidad**. Si hay
orden, se expresa fuera del nombre (índice, config, frontmatter).

---

## 2. Principios recomendados de naming

1. **Idioma único: inglés.** La stdlib, awesome-elixir, hexdocs, Exercism y
   Elixir School son inglés. Evita el code-switching actual
   (`09-pattern-matching-avanzado`, `01-procesos-spawn-send-receive`).
2. **kebab-case** para directorios y archivos (consistente con Exercism,
   awesome-elixir y URLs).
3. **Dominio primero, técnica después**: el nombre debe identificar el tema,
   no el tipo de ejercicio. `genserver-bounded-buffer` > `36-genserver-bounded-buffer`.
4. **Longitud objetivo: 2–4 palabras** (máx 5). Si necesita más, probablemente
   son dos challenges.
5. **Sin verbos genéricos** (`use-`, `do-`, `make-`). Permitidos solo cuando
   aportan: `build-` para proyectos grandes estilo "insane" (consistente con
   lo que ya usas: `build-distributed-raft-consensus`).
6. **Sin redundancia con el padre**: dentro de `otp/genservers/` el archivo no
   debe llamarse `genserver-bounded-buffer`, solo `bounded-buffer`. El path
   provee el contexto.
7. **Variantes con sufijo explícito**: `-basics`, `-advanced`, `-deep`. Evitar
   números para marcar progresión (`genservers-01`, `genservers-02`). Preferir
   `genservers/basics/`, `genservers/advanced/`.
8. **Unicidad global**: actualmente hay duplicados cross-level (`upsert-on-conflict`
   aparece en `243-` y `274-`, `window-functions` en `233-` y `278-`). Tras el
   rename, un nombre debe ser único dentro de su subcategoría.
9. **Sin caracteres especiales** (acentos, `ñ`, apóstrofes). `basico` → `foundations`.

---

## 3. Propuesta de taxonomía top-level

Cuatro niveles actuales (`01-basico`/`02-intermedio`/`03-avanzado`/`04-insane`)
siguen a Elixir School, pero con dos problemas: (a) están en español, (b) mezclan
dominio con dificultad. Propuesta (elige una):

### Opción A — mantener 4 tiers curriculares (mínimo impacto)
```
foundations/     (ex 01-basico)
intermediate/    (ex 02-intermedio)
advanced/        (ex 03-avanzado)
expert/          (ex 04-insane)  # "insane" es informal; "expert" o "mastery"
```
Pros: preserva la mental model actual; rename barato.
Contras: el mismo dominio (OTP, Ecto, macros) aparece en varios tiers.

### Opción B — taxonomía por dominio + dificultad como tag (recomendada)
```
language/        syntax, types, pattern-matching, guards, control-flow
stdlib/          enum, stream, string, io, file, calendar
collections/     lists, maps, keyword, mapset, ranges
processes/       spawn, send-receive, links, monitors
otp/             genservers, supervisors, agents, tasks, registries
concurrency/     backpressure, flow, rate-limiting, pools
metaprogramming/ macros, ast, protocols, behaviours, dsl
ecto/            queries, schemas, migrations, multi, multitenancy
phoenix/         channels, liveview, plug, pubsub
testing/         exunit, property-based, mocks, integration
observability/   telemetry, logger, tracing, metrics
resilience/      circuit-breakers, retries, timeouts, bulkheads
distributed/     clustering, consensus, gossip, crdt
interop/         nifs, ports, erlang-interop, rust
tooling/         mix, releases, umbrella, deploy
projects/        (equivalente a 04-insane: "build-X" proyectos grandes)
```
Dificultad se expresa dentro del dominio: `otp/genservers/basics/`,
`otp/genservers/advanced/`, `otp/genservers/deep/`.

Pros: alineado con awesome-elixir y hexdocs; sin duplicados entre tiers; más
escalable. Contras: migración mayor.

### Opción C — híbrida
Tier curricular top + dominio segundo, ambos sin números:
```
foundations/language/, foundations/collections/, ...
intermediate/otp/, intermediate/testing/, ...
advanced/metaprogramming/, advanced/ecto/, ...
expert/distributed/, expert/storage-engines/, ...
```
Pros: compromiso razonable; preserva la narrativa de progresión. Contras:
sigue habiendo duplicación de dominios entre tiers.

**Recomendación**: Opción C si el repo es un curso lineal; Opción B si es un
catálogo de referencia consultable. La estructura actual ya es mitad-C.

---

## 4. Propuesta de subcategorías por tier (asumiendo Opción C)

### foundations/ (ex 01-basico)
```
language/          # sintaxis, tipos, operadores, atoms, binaries
pattern-matching/  # destructuring, pin, guards intro
control-flow/      # if, case, cond, with, unless, try-rescue
functions/         # anon, capture, default args, multi-clause
modules/           # alias, import, require, use, defdelegate
collections/       # lists, maps, keyword, tuples, ranges, mapset
enum-stream/       # Enum + Stream (separada de collections)
strings-binaries/  # String, Regex, codepoints, bitstrings
errors/            # try/rescue/catch, raise, custom exceptions
processes/         # spawn, send, receive, link, monitor (intro)
mix-setup/         # mix new, deps, config, IEx
```

### intermediate/ (ex 02-intermedio)
```
otp/
  genservers/
  agents-tasks/
  supervisors/
  registries/
protocols-behaviours/
macros-intro/
streams-lazy/
ets/               # basics solamente; avanzado va en advanced/
testing/           # ExUnit, Mox, fixtures
tooling/           # mix tasks, releases, umbrella
integrations/      # HTTP clients, JSON, env config
```

### advanced/ (ex 03-avanzado)
```
otp-advanced/      # gen_statem, hot code reload, proc_lib
supervision/       # strategies, restarts, shutdown
metaprogramming/   # macros, AST, quote/unquote, DSL
ecto/              # queries, multi, multitenancy, zero-downtime
phoenix/           # plug, channels, pubsub, liveview
ets-dets-mnesia/
data-pipelines/    # Flow, GenStage, Broadway
apis/              # REST, GraphQL, OpenAPI
distribution/      # libcluster, :global, :rpc
resilience/        # circuit breakers, retries
observability/     # telemetry, tracing, metrics
beam-performance/  # profiling, benchmarks, schedulers
interop/           # NIFs, ports, Rustler
testing-advanced/  # property-based, stateful, integration
```

### expert/ (ex 04-insane)
```
concurrency-primitives/  # semaphores, actors, STM
distributed-systems/     # Raft, gossip, CRDT, saga
storage-engines/         # LSM, KV, graph, TSDB, inverted index
frameworks-and-engines/  # mini-Phoenix, mini-Ecto
languages-compilers/     # lexers, parsers, VMs
network-proxies/         # L7 proxy, gRPC, QUIC
observability-platforms/ # APM, distributed tracing backends
data-integrations/       # stream processors, ETL engines
```

---

## 5. Reglas de naming para challenges individuales

Estructura: `{subcategoria}/{challenge-slug}/{challenge-slug}.md`

### Formato del slug
- **kebab-case**, lowercase, solo ASCII
- **2–4 palabras** (hard max: 5)
- **Sustantivo-primero**: el primer token es el concepto principal
- **Sin números de prefijo** en el nombre (ver sección 6)
- **Sin repetir el nombre del padre**: dentro de `otp/genservers/` el slug es
  `bounded-buffer`, no `genserver-bounded-buffer`
- **Sin artículos** (`the-`, `a-`, `an-`)
- **Sin verbos débiles** como primera palabra: evitar `using-`, `doing-`,
  `making-`. Permitidos con intención:
  - `build-` → proyectos grandes tipo expert (`build-raft-consensus`)
  - `compare-` → challenges de tradeoffs (`compare-call-vs-cast`)

### Variantes / progresión dentro de un mismo tema
- Sufijo funcional: `-basics`, `-advanced`, `-deep`, `-patterns`
- Sufijo de caso: `-timers`, `-cleanup`, `-validation`, `-debug`
- Ejemplos:
  - `bounded-buffer` (versión estándar)
  - `bounded-buffer-backpressure` (variante con backpressure)

### Reglas de contenido del `.md`
- Frontmatter YAML al inicio con: `title`, `difficulty` (1–4), `order` (entero),
  `tags`, `concepts`. Esto reemplaza el número del directorio para ordenar.
- Título H1 legible (humano), no el slug.

Ejemplo de frontmatter propuesto:
```yaml
---
title: "GenServer: Bounded Buffer"
difficulty: 2
order: 37
tags: [otp, genserver, backpressure]
concepts: [handle_call, handle_cast, state]
---
```

---

## 6. Ordenamiento curricular: tres opciones

### (a) Sin números — orden alfabético puro
Pros: más limpio visualmente; cada slug es estable; funciona bien en catálogos
tipo Exercism. Contras: pierdes la progresión pedagógica; el lector no sabe
por dónde empezar; el orden alfabético puede ser pedagógicamente peor
(`advanced-macros` antes de `basics`).

### (b) Prefijo numérico `01-` (solo para orden)
Pros: el FS ordena "gratis"; es lo que tienes hoy. Contras: el número se
vuelve parte de la identidad — si insertas un challenge entre 05 y 06 tienes
que renumerar (justo el dolor actual con duplicados 243 y 274). Además el
número aparece en imports, grep, git log y URLs.

### (c) `INDEX.md` (o `index.yaml`) que defina el orden
Pros: identidad (slug) separada del orden; insertar en el medio es cambiar
una línea del índice, no renombrar carpetas; soporta múltiples "vistas"
(ruta recomendada, ruta rápida, por tags). Esto es exactamente lo que
Exercism hace con `config.json` y mdBook con `SUMMARY.md`. Contras: requiere
mantener el índice sincronizado; herramientas de FS no reflejan el orden.

**Recomendación**: **(c) INDEX.md + frontmatter `order`**. Es la práctica
estándar (Exercism, mdBook, Docusaurus, Jekyll collections) y resuelve
precisamente los dolores actuales (duplicados 243/274, saltos de numeración,
dificultad para insertar). Un script de validación puede chequear que cada
challenge figure en exactamente un índice.

Si (c) se siente pesado, el compromiso razonable es **(b) con dos dígitos y
renumeraciones dispersas** (`10`, `20`, `30`…) para permitir inserciones sin
conflicto, más **slug estable sin el número** cuando el archivo se importa o
referencia.

---

## 7. Ejemplos concretos de renames (20 casos)

Asumiendo Opción C (tier + dominio) + opción (c) de ordenamiento
(INDEX.md + frontmatter).

| # | Actual | Propuesto |
|---|---|---|
| 1  | `01-basico/collections/06-lists-and-head-tail-pattern/` | `foundations/collections/lists-head-tail/` |
| 2  | `01-basico/collections/11-enum-module-and-immutability/` | `foundations/enum-stream/enum-immutability/` |
| 3  | `01-basico/collections/12-pipe-operator-and-composition/` | `foundations/language/pipe-composition/` |
| 4  | `01-basico/collections/17-ranges-and-streams/` | `foundations/enum-stream/ranges-streams/` |
| 5  | `01-basico/collections/19-comprehensions-for/` | `foundations/enum-stream/comprehensions/` |
| 6  | `01-basico/collections/47-keyword-dsl-building/` | `foundations/collections/keyword-dsl/` |
| 7  | `01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/` | `foundations/pattern-matching/tuples-intro/` |
| 8  | `01-basico/pattern-matching/09-pattern-matching-avanzado/` | `foundations/pattern-matching/advanced/` |
| 9  | `01-basico/pattern-matching/40-pin-operator-deep/` | `foundations/pattern-matching/pin-operator/` |
| 10 | `01-basico/error-handling/25-error-handling-try-rescue/` | `foundations/errors/try-rescue/` |
| 11 | `01-basico/error-handling/30-error-tuples-vs-raise/` | `foundations/errors/tuples-vs-raise/` |
| 12 | `01-basico/error-handling/65-trap-exit-linked/` | `foundations/processes/trap-exit/` |
| 13 | `02-intermedio/otp-genservers/01-procesos-spawn-send-receive/` | `foundations/processes/spawn-send-receive/` (reubicar al tier correcto) |
| 14 | `02-intermedio/otp-genservers/37-genserver-bounded-buffer/` | `intermediate/otp/genservers/bounded-buffer/` |
| 15 | `02-intermedio/otp-genservers/39-call-vs-cast-tradeoffs/` | `intermediate/otp/genservers/call-vs-cast/` |
| 16 | `02-intermedio/ets-basics/13-ets-basico/` | `intermediate/ets/basics/` |
| 17 | `03-avanzado/metaprogramming/140-ast-walker/` | `advanced/metaprogramming/ast-walker/` |
| 18 | `03-avanzado/metaprogramming/142-dsl-schema-builder/` | `advanced/metaprogramming/dsl-schema-builder/` |
| 19 | `03-avanzado/ecto-advanced/233-window-functions/` + `278-window-functions-select-merge/` | `advanced/ecto/window-functions/` + `advanced/ecto/window-functions-select-merge/` (elimina duplicado manteniendo el contenido más completo) |
| 20 | `04-insane/distributed-systems/01-build-distributed-raft-consensus/` | `expert/distributed-systems/raft-consensus/` (el verbo `build-` pasa a ser convención del tier: *todos* los expert son proyectos, no hace falta el prefijo) |

Notas sobre los renames:
- Caso 13: `01-procesos-spawn-send-receive` está en `otp-genservers` pero
  pedagógicamente es `foundations/processes/`. Clasificación actual tiene
  ruido que esta migración es oportunidad de limpiar.
- Caso 19: hay duplicación real de `window-functions` en `233-` y `278-`.
  Requiere decisión humana: ¿son el mismo ejercicio (consolidar) o dos
  variantes (mantener ambos con slugs distintos)?
- Caso 20: si el tier `expert/` es por definición "construye X", el prefijo
  `build-` es redundante dentro del path y se elimina.

---

## Decisiones clave que debes tomar

1. **¿Opción A, B o C para la taxonomía top-level?** (Recomendado: C)
2. **¿Cómo manejar el orden curricular?** (Recomendado: 6-c, INDEX.md +
   frontmatter `order`; alternativa aceptable: 6-b con números dispersos
   10/20/30).
3. **Idioma**: confirmar migración total a inglés (implica renombrar
   `basico`/`intermedio`/`avanzado`/`insane` y slugs españoles como
   `procesos-spawn-send-receive`, `ecto-basico`, `pattern-matching-avanzado`).
4. **Duplicados cross-tier** (p.ej. `window-functions` en 233 y 278,
   `upsert-on-conflict` en 243 y 274): ¿consolidar manualmente o auto-deduplicar
   por hash de contenido antes del rename?
5. **Profundidad máxima**: ¿aceptas 3 niveles de directorio
   (`intermediate/otp/genservers/bounded-buffer/`) o prefieres aplanar a 2
   (`intermediate/otp-genservers/bounded-buffer/`)? Elixir School usa 2; tu
   repo actual ya tiene 2; opción C pediría 3 solo para OTP/Ecto.

---

## Fuentes

- [awesome-elixir](https://github.com/h4cc/awesome-elixir)
- [Elixir School](https://elixirschool.com/en)
- [Exercism Elixir track](https://github.com/exercism/elixir)
- [Hexdocs Elixir API Reference](https://hexdocs.pm/elixir/api-reference.html)
- [Elixir Style Guide (christopheradams)](https://github.com/christopheradams/elixir_style_guide)
- [Go by Example](https://gobyexample.com/)
- [Rust by Example](https://doc.rust-lang.org/rust-by-example/)
- [Codewars Kata Authoring Guidelines](https://docs.codewars.com/authoring/guidelines/kata/)
