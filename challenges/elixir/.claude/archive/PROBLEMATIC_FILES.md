# Archivos Problemáticos — Lista Detallada

## Resumen

- **CRÍTICO**: 490 instancias (87% missing "Project structure" + 1% stubs)
- **IMPORTANTE**: 62 instancias (11% missing "## Why")
- **MENOR**: 29 instancias (4% incomplete @moduledoc + 1% generic names)

---

## 1. Implementaciones Stub (7 archivos) — CRÍTICO

Estos archivos contienen `raise NotImplementedError` o `TODO` en lugar de implementaciones funcionales. No son aprendibles.

| Archivo | Categoría | Acción |
|---------|-----------|--------|
| `02-intermedio/testing-and-quality/111-exunit-tags/111-exunit-tags.md` | 02-intermedio | Escribir implementación funcional |
| `02-intermedio/tooling-and-ecosystem/126-credo-rules/126-credo-rules.md` | 02-intermedio | Escribir implementación funcional |
| `03-avanzado/metaprogramming/24-behaviours-custom-advanced/24-behaviours-custom-advanced.md` | 03-avanzado | Escribir implementación funcional |
| `03-avanzado/resilience-patterns/71-rate-limiter-ets/71-rate-limiter-ets.md` | 03-avanzado | Escribir implementación funcional |
| `01-basico/fundamentals/01-setup-and-mix/json_validator/README.md` | 01-basico | Escribir implementación funcional |

---

## 2. Sin Sección "## Why" (62 archivos) — IMPORTANTE

Estos archivos carecen de explicación conceptual del problema y su importancia.

### 02-intermedio (27 archivos — PEOR CATEGORÍA)

```
02-intermedio/integrations-basics/132-telemetry-span/132-telemetry-span.md
02-intermedio/integrations-basics/133-req-http/133-req-http.md
02-intermedio/integrations-basics/134-finch-pool/134-finch-pool.md
02-intermedio/integrations-basics/135-jason-vs-poison/135-jason-vs-poison.md
02-intermedio/integrations-basics/136-nimble-json-lite/136-nimble-json-lite.md
02-intermedio/integrations-basics/137-nimble-options/137-nimble-options.md
02-intermedio/integrations-basics/138-csv-streaming/138-csv-streaming.md
02-intermedio/integrations-basics/139-broadway-rabbit-basic/139-broadway-rabbit-basic.md
02-intermedio/integrations-basics/140-httpoison-migration/140-httpoison-migration.md
02-intermedio/integrations-basics/28-telemetry-basico/28-telemetry-basico.md
02-intermedio/integrations-basics/29-ecto-basico/29-ecto-basico.md
02-intermedio/integrations-basics/35-nimbleparsec-csv-basico/35-nimbleparsec-csv-basico.md
02-intermedio/protocols-and-behaviours/77-behaviour-strategy/77-behaviour-strategy.md
02-intermedio/protocols-and-behaviours/78-protocol-vs-behaviour/78-protocol-vs-behaviour.md
02-intermedio/protocols-and-behaviours/79-enumerable-custom-tree/79-enumerable-custom-tree.md
02-intermedio/protocols-and-behaviours/80-inspect-algebra-custom/80-inspect-algebra-custom.md
02-intermedio/streams-and-lazy/10-comprehensions-avanzadas/10-comprehensions-avanzadas.md
02-intermedio/streams-and-lazy/11-lazy-generators/11-lazy-generators.md
02-intermedio/streams-and-lazy/12-stream-cycle-repeat/12-stream-cycle-repeat.md
02-intermedio/streams-and-lazy/13-stream-transform-drop-take/13-stream-transform-drop-take.md
02-intermedio/streams-and-lazy/2-streams-vs-enum/2-streams-vs-enum.md
02-intermedio/streams-and-lazy/3-stream-chunking/3-stream-chunking.md
02-intermedio/streams-and-lazy/4-stream-filtering/4-stream-filtering.md
02-intermedio/streams-and-lazy/5-stream-mapping/5-stream-mapping.md
02-intermedio/streams-and-lazy/6-stream-take-drop/6-stream-take-drop.md
02-intermedio/streams-and-lazy/7-stream-reducing-custom/7-stream-reducing-custom.md
02-intermedio/streams-and-lazy/9-stream-resource-cleanup/9-stream-resource-cleanup.md
```

### 01-basico (3 archivos)

```
01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md
01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md
01-basico/fundamentals/01-setup-and-mix/json_validator/README.md
```

### 03-avanzado (23 archivos)

```
03-avanzado/advanced-testing/...
03-avanzado/apis-and-graphql/...
(ver reporte JSON para lista completa)
```

### 04-insane (6 archivos)

```
(ver reporte JSON para lista completa)
```

**Acción**: Agregar sección "## Why" en cada archivo:
- Explicar qué problema resuelve
- Por qué es importante
- Objetivos de aprendizaje

---

## 3. Nombres Genéricos "defmodule Solution" (5 archivos) — MENOR

Usan nombre genérico en lugar de descriptivo. No enseña convenciones reales.

| Archivo | Cambiar a |
|---------|-----------|
| `02-intermedio/macros-intro/91-macro-guard-friendly/91-macro-guard-friendly.md` | Nombre descriptivo (ej: `GuardHelper`) |
| `02-intermedio/streams-and-lazy/103-stream-unfold-fibonacci/103-stream-unfold-fibonacci.md` | `FibonacciStream` |
| `02-intermedio/streams-and-lazy/104-stream-zip-timelines/104-stream-zip-timelines.md` | `TimelineZipper` |
| `02-intermedio/streams-and-lazy/109-flow-parallel-word-count/109-flow-parallel-word-count.md` | `WordCounter` |
| `02-intermedio/tooling-and-ecosystem/120-mix-generator/120-mix-generator.md` | `ProjectGenerator` |

---

## 4. @moduledoc Genérico/Incompleto (24 archivos) — MENOR

Documentación de módulo muy breve o genérica. Expandir a 2-3 líneas descriptivas.

Acción: Mejorar @moduledoc para que explique:
- Propósito del módulo
- Funciones principales
- Cuándo/por qué usarlo

---

## 5. Missing "Project structure" (483 archivos) — CRÍTICO

**TODOS** los 483 archivos en 4 categorías carecen de sección "Project structure".

- **01-basico**: 79/79 archivos (100%)
- **02-intermedio**: 137/137 archivos (100%)
- **03-avanzado**: 280/280 archivos (100%)
- **04-insane**: 56/56 archivos (100%)

Solo 69 archivos tienen esta sección.

### Solución: Template Consistente

Crear template único y aplicarlo a todos:

```markdown
## Project structure

```
project_name/
├── lib/
│   └── module_name.ex
├── test/
├── script/
│   └── main.exs
├── test/
│   └── module_name_test.exs
└── mix.exs
```

Where:
- `lib/module_name.ex` — Main implementation
- `test/module_name_test.exs` — Tests
- `mix.exs` — Project configuration
```

Luego:
1. Crear script para inserción en bulk
2. Ejecutar en 01-basico primero
3. Ir por categoría

---

## Why Archivos Problemáticos — Lista Detallada matters

Understanding Archivos Problemáticos — Lista Detallada is essential for building production-grade Elixir applications. This concept is foundational for writing code that is maintainable, performant, and resilient.

When you work on real systems, you'll encounter scenarios where Archivos Problemáticos — Lista Detallada directly impacts your application's behavior. Ignoring this concept leads to subtle bugs, performance degradation, and architectural issues that become expensive to fix later.

This challenge teaches you the fundamentals that unlock advanced patterns and best practices. Master it, and you'll write more confident, production-ready Elixir code.


## Resumen de Acciones

| Prioridad | Problema | Count | Esfuerzo | Impacto |
|-----------|----------|-------|----------|---------|
| P1 | Project structure | 483 | Alto | Crítico |
| P2 | Stubs | 7 | Medio | Crítico |
| P3 | Missing Why | 62 | Medio | Alto |
| P4 | Generic names | 5 | Bajo | Medio |
| P5 | Generic @moduledoc | 24 | Bajo-Medio | Bajo |

