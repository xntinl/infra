# Reporte de Auditoria Estructural - Documentacion Elixir

Fecha: 2026-04-13T13:04:53.802193Z
Raiz: `/Users/consulting/Documents/consulting/infra/challenges/elixir`

## Resumen ejecutivo

- **Total archivos auditados**: 552
- **Estructura 100% correcta**: 0/552 (0.0%)
- **Archivos con al menos 1 fallo**: 552/552 (100.0%)
- **Score promedio**: 12.53/17

### Desglose por item del checklist

| # | Item | Pass | Fail | % Pass |
|---|------|------|------|--------|
| 1 | # <Title> (H1) presente al inicio | 552 | 0 | 100.0% |
| 2 | **Project**: `<app_name>` — <description> | 550 | 2 | 99.6% |
| 3 | ## Why <topic> matters | 322 | 230 | 58.3% |
| 4 | ## The business problem | 377 | 175 | 68.3% |
| 5 | ## Project structure | 534 | 18 | 96.7% |
| 6 | ## Design decisions | 546 | 6 | 98.9% |
| 7 | ## Implementation | 512 | 40 | 92.8% |
| 8 | ### `mix.exs` (dentro de Implementation) | 398 | 154 | 72.1% |
| 9 | ### `lib/<app>.ex` (dentro de Implementation) | 166 | 386 | 30.1% |
| 10 | ### `test/<app>_test.exs` (dentro de Implementation) | 151 | 401 | 27.4% |
| 11 | ### `script/main.exs` (dentro de Implementation) | 349 | 203 | 63.2% |
| 12 | ## Key concepts (al final) | 34 | 518 | 6.2% |
| 13 | NO anti-patron: '### Dependencies (mix.exs)' | 440 | 112 | 79.7% |
| 14 | NO anti-patron: '### Tests' | 537 | 15 | 97.3% |
| 15 | NO anti-patron: '## Executable Example' | 392 | 160 | 71.0% |
| 16 | NO headings duplicados | 506 | 46 | 91.7% |
| 17 | NO jerarquia rota (H3 sin H2 padre) | 552 | 0 | 100.0% |

### Top 10 problemas mas comunes

- **518 archivos** (93.8%): ## Key concepts (al final)
- **401 archivos** (72.6%): ### `test/<app>_test.exs` (dentro de Implementation)
- **386 archivos** (69.9%): ### `lib/<app>.ex` (dentro de Implementation)
- **230 archivos** (41.7%): ## Why <topic> matters
- **203 archivos** (36.8%): ### `script/main.exs` (dentro de Implementation)
- **175 archivos** (31.7%): ## The business problem
- **160 archivos** (29.0%): NO anti-patron: '## Executable Example'
- **154 archivos** (27.9%): ### `mix.exs` (dentro de Implementation)
- **112 archivos** (20.3%): NO anti-patron: '### Dependencies (mix.exs)'
- **46 archivos** (8.3%): NO headings duplicados

## Score por directorio

| Directorio | Archivos | Perfectos | Avg /17 | % Perfectos |
|------------|----------|-----------|---------|-------------|
| `01-basico` | 78 | 0 | 10.4 | 0.0% |
| `02-intermedio` | 139 | 0 | 13.17 | 0.0% |
| `03-avanzado` | 280 | 0 | 13.54 | 0.0% |
| `04-insane` | 55 | 0 | 8.84 | 0.0% |

## Archivos problematicos (score < 17)

Total: 552

### `04-insane/concurrency-primitives/11-build-custom-event-bus-registry/11-build-custom-event-bus-registry.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Design decisions
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/concurrency-primitives/12-build-custom-process-pool/12-build-custom-process-pool.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Design decisions
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/concurrency-primitives/13-build-actor-framework-alternative/13-build-actor-framework-alternative.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Design decisions
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/01-build-distributed-raft-consensus/01-build-distributed-raft-consensus.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Design decisions
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/languages-and-compilers/25-build-lisp-interpreter/25-build-lisp-interpreter.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/languages-and-compilers/26-build-mini-language-compiler/26-build-mini-language-compiler.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/languages-and-compilers/27-build-regex-engine/27-build-regex-engine.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/network-and-proxies/21-build-message-broker-amqp-like/21-build-message-broker-amqp-like.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/network-and-proxies/22-build-stream-processor-flink-like/22-build-stream-processor-flink-like.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/observability/06-build-distributed-tracing-system/06-build-distributed-tracing-system.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/observability/28-build-log-aggregation-system/28-build-log-aggregation-system.md` — Score 7/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/text-and-binaries/24-bitstrings-binarios-avanzados/24-bitstrings-binarios-avanzados.md` — Score 8/17

- FAIL: **Project**: `<app_name>` — <description>
- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `04-insane/concurrency-primitives/10-build-custom-genserver-supervisor/10-build-custom-genserver-supervisor.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/concurrency-primitives/14-build-behaviour-callback-validator/14-build-behaviour-callback-validator.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/data-and-integrations/20-build-crdt-data-structures/20-build-crdt-data-structures.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/data-and-integrations/43-build-ai-inference-engine/43-build-ai-inference-engine.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/data-and-integrations/47-build-workflow-engine-temporal-like/47-build-workflow-engine-temporal-like.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/distributed-systems/02-build-distributed-transaction-coordinator/02-build-distributed-transaction-coordinator.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/04-build-distributed-scheduler/04-build-distributed-scheduler.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/05-build-gossip-membership-protocol/05-build-gossip-membership-protocol.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/frameworks-and-engines/45-build-game-engine-ecs/45-build-game-engine-ecs.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/48-build-compile-time-type-system/48-build-compile-time-type-system.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/49-build-reactive-streams/49-build-reactive-streams.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/51-build-production-job-queue/51-build-production-job-queue.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/53-build-real-time-collaboration-engine/53-build-real-time-collaboration-engine.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/54-build-ai-agents-framework/54-build-ai-agents-framework.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/observability/29-build-metrics-collector-prometheus-like/29-build-metrics-collector-prometheus-like.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/observability/30-build-profiler-performance-analyzer/30-build-profiler-performance-analyzer.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## Project structure
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/03-build-distributed-cache-redis-like/03-build-distributed-cache-redis-like.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/15-build-lsm-tree-storage-engine/15-build-lsm-tree-storage-engine.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/16-build-in-memory-database/16-build-in-memory-database.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/17-build-graph-database/17-build-graph-database.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/18-build-time-series-database/18-build-time-series-database.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/storage-engines/19-build-search-engine-inverted-index/19-build-search-engine-inverted-index.md` — Score 8/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## Design decisions
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/74-process-send-after/74-process-send-after.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## Design decisions
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/16-sigils-and-regex/16-sigils-and-regex.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/22-string-chars-protocol/22-string-chars-protocol.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/48-unicode-normalization/48-unicode-normalization.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/49-string-split-trim-edge/49-string-split-trim-edge.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/50-binary-construction/50-binary-construction.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/51-base64-url-encoding/51-base64-url-encoding.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/01-genserver-hibernation-state-compaction/01-genserver-hibernation-state-compaction.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/03-genserver-timeouts-and-heartbeats/03-genserver-timeouts-and-heartbeats.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `04-insane/data-and-integrations/37-build-blockchain-simulation/37-build-blockchain-simulation.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## Project structure
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/distributed-systems/07-build-distributed-event-log/07-build-distributed-event-log.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/08-build-viewstamped-replication/08-build-viewstamped-replication.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/09-build-consistent-hashing-rebalancer/09-build-consistent-hashing-rebalancer.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/24-build-distributed-rate-limiter/24-build-distributed-rate-limiter.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/frameworks-and-engines/23-build-job-queue-with-retry/23-build-job-queue-with-retry.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/frameworks-and-engines/46-build-liveview-clone/46-build-liveview-clone.md` — Score 9/17

- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/frameworks-and-engines/52-build-multi-tenant-saas-framework/52-build-multi-tenant-saas-framework.md` — Score 9/17

- FAIL: ## The business problem
- FAIL: ## Implementation
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/network-and-proxies/35-build-load-balancer/35-build-load-balancer.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO headings duplicados

### `04-insane/network-and-proxies/39-build-service-mesh-proxy/39-build-service-mesh-proxy.md` — Score 9/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO headings duplicados

### `01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/19-comprehensions-for/19-comprehensions-for.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/46-mapset-operations/46-mapset-operations.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md` — Score 10/17

- FAIL: **Project**: `<app_name>` — <description>
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/33-task-async-await/33-task-async-await.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/73-process-monitor/73-process-monitor.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/68-uri-parsing/68-uri-parsing.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/69-base-encoding/69-base-encoding.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/70-system-env-vars/70-system-env-vars.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/71-code-eval-sandboxed/71-code-eval-sandboxed.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/04-strings-and-binaries-basics/04-strings-and-binaries-basics.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/18-charlists-vs-strings/18-charlists-vs-strings.md` — Score 10/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/text-and-binaries/28-binaries-bit-syntax/28-binaries-bit-syntax.md` — Score 10/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/124-mnesia-fragmented/124-mnesia-fragmented.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/17-dets-persistent-storage/17-dets-persistent-storage.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/18-mnesia-basics/18-mnesia-basics.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/161-rustler-binary/161-rustler-binary.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/162-rustler-dirty/162-rustler-dirty.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/163-nif-resources/163-nif-resources.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/164-port-driver/164-port-driver.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/30-erlang-interop-advanced/30-erlang-interop-advanced.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/323-zigler-nif-basics/323-zigler-nif-basics.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/329-embed-binary-resources/329-embed-binary-resources.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/330-system-cmd-timeout-wrapper/330-system-cmd-timeout-wrapper.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/133-attr-accumulate/133-attr-accumulate.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/134-defdelegate-custom/134-defdelegate-custom.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/135-use-with-opts/135-use-with-opts.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/136-quoted-as-literal/136-quoted-as-literal.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/142-dsl-schema-builder/142-dsl-schema-builder.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/143-dsl-router-builder/143-dsl-router-builder.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/144-spec-gen/144-spec-gen.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/22-custom-dsl-with-macros/22-custom-dsl-with-macros.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/24-behaviours-custom-advanced/24-behaviours-custom-advanced.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/389-dsl-macro-prewalk-special-forms/389-dsl-macro-prewalk-special-forms.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/04-genserver-back-pressure-queue/04-genserver-back-pressure-queue.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/51-phoenix-liveview-real-time/51-phoenix-liveview-real-time.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/phoenix-ecosystem/52-phoenix-livecomponent/52-phoenix-livecomponent.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/phoenix-ecosystem/54-phoenix-presence-channels/54-phoenix-presence-channels.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/306-timeout-hierarchies/306-timeout-hierarchies.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/307-fallback-chains-with/307-fallback-chains-with.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/310-graceful-degradation-feature-flags/310-graceful-degradation-feature-flags.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/47-umbrella-application/47-umbrella-application.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `04-insane/frameworks-and-engines/31-build-test-framework/31-build-test-framework.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `04-insane/languages-and-compilers/40-build-wasm-interpreter/40-build-wasm-interpreter.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/network-and-proxies/34-build-api-gateway/34-build-api-gateway.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `04-insane/network-and-proxies/36-build-video-streaming-server/36-build-video-streaming-server.md` — Score 10/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO headings duplicados

### `04-insane/network-and-proxies/38-build-p2p-file-sharing/38-build-p2p-file-sharing.md` — Score 10/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'

### `01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'

### `01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/116-ets-sharding/116-ets-sharding.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/121-mnesia-ram/121-mnesia-ram.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/122-mnesia-disc-transactions/122-mnesia-disc-transactions.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/123-mnesia-dirty-ops/123-mnesia-dirty-ops.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/125-mnesia-vs-postgres/125-mnesia-vs-postgres.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/126-dets-vs-ets/126-dets-vs-ets.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/127-cache-lru/127-cache-lru.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/16-ets-advanced-concurrent/16-ets-advanced-concurrent.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/ets-dets-mnesia/19-cache-patterns-ets/19-cache-patterns-ets.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/20-ets-counter-and-atomics/20-ets-counter-and-atomics.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/ets-dets-mnesia/43-build-cache-server/43-build-cache-server.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/31-ports-external-processes/31-ports-external-processes.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/319-dirty-nif-cpu-bound/319-dirty-nif-cpu-bound.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/32-nifs-basics-rustler/32-nifs-basics-rustler.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/interop-and-native/320-port-driver-streaming/320-port-driver-streaming.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/321-port-open-subprocess/321-port-open-subprocess.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/322-erlexec-resource-limits/322-erlexec-resource-limits.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/324-cnode-integration/324-cnode-integration.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/326-ets-shared-nif-readers/326-ets-shared-nif-readers.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/327-erlport-python-bridge/327-erlport-python-bridge.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/interop-and-native/328-port-nodejs-json-framing/328-port-nodejs-json-framing.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/131-unquote-fragment/131-unquote-fragment.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/132-compile-tracer/132-compile-tracer.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/137-protocol-derive/137-protocol-derive.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/138-fallback-any/138-fallback-any.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/139-optional-callbacks/139-optional-callbacks.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/140-ast-walker/140-ast-walker.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/141-macro-expand-debug/141-macro-expand-debug.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/metaprogramming/21-advanced-macros-ast/21-advanced-macros-ast.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/metaprogramming/23-protocol-consolidation/23-protocol-consolidation.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/metaprogramming/388-compile-time-config-validation-on-load/388-compile-time-config-validation-on-load.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/02-genserver-handle-continue-recovery/02-genserver-handle-continue-recovery.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/385-proc-lib-sys-handrolled-behaviour/385-proc-lib-sys-handrolled-behaviour.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/387-genserver-hibernation-idle-memory/387-genserver-hibernation-idle-memory.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/72-gen-statem-retry/72-gen-statem-retry.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/73-selective-receive/73-selective-receive.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/80-special-process-impl/80-special-process-impl.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/81-call-from-self-deadlock/81-call-from-self-deadlock.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/82-reply-async/82-reply-async.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/otp-advanced/83-timers-send-after-vs-schedule/83-timers-send-after-vs-schedule.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/218-channels-binary/218-channels-binary.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/219-channels-auth/219-channels-auth.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/220-channels-intercept/220-channels-intercept.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/221-presence-metas/221-presence-metas.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/222-plug-auth-custom/222-plug-auth-custom.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `03-avanzado/phoenix-ecosystem/53-phoenix-channels/53-phoenix-channels.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/304-bulkhead-process-pools/304-bulkhead-process-pools.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/305-retry-exponential-backoff-jitter/305-retry-exponential-backoff-jitter.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/308-bounded-queue-genstage-backpressure/308-bounded-queue-genstage-backpressure.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/309-load-shedding-token-bucket/309-load-shedding-token-bucket.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/311-deadline-propagation-processes/311-deadline-propagation-processes.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/312-chaos-engineering-suspend-kill/312-chaos-engineering-suspend-kill.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/313-idempotency-keys-ets-ttl/313-idempotency-keys-ets-ttl.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/314-dead-letter-queue/314-dead-letter-queue.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/315-saga-compensating-actions/315-saga-compensating-actions.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/316-hedged-requests/316-hedged-requests.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/317-adaptive-concurrency-limits/317-adaptive-concurrency-limits.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/resilience-patterns/37-rate-limiting-patterns/37-rate-limiting-patterns.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/390-rest-for-one-custom-restart-intensity/390-rest-for-one-custom-restart-intensity.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/391-partition-supervisor-lock-contention/391-partition-supervisor-lock-contention.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/87-partition-sup-sharded/87-partition-sup-sharded.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/90-bulkhead-supervisors/90-bulkhead-supervisors.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/91-umbrella-boot-order/91-umbrella-boot-order.md` — Score 11/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/96-sup-metrics-telemetry/96-sup-metrics-telemetry.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/supervision-advanced/97-adoption-patterns/97-adoption-patterns.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `03-avanzado/supervision-advanced/98-app-start-phases/98-app-start-phases.md` — Score 11/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'
- FAIL: NO headings duplicados

### `04-insane/frameworks-and-engines/33-build-rest-api-framework/33-build-rest-api-framework.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/frameworks-and-engines/41-build-event-sourcing-cqrs-framework/41-build-event-sourcing-cqrs-framework.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `04-insane/frameworks-and-engines/42-build-custom-macro-dsl-system/42-build-custom-macro-dsl-system.md` — Score 11/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '### Tests'

### `01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'

### `01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `02-intermedio/integrations-basics/129-ecto-migrations/129-ecto-migrations.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/133-req-http/133-req-http.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/134-finch-pool/134-finch-pool.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/136-nimble-json-lite/136-nimble-json-lite.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/137-nimble-options/137-nimble-options.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/138-csv-streaming/138-csv-streaming.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/140-httpoison-migration/140-httpoison-migration.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/integrations-basics/29-ecto-basico/29-ecto-basico.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/macros-intro/12-macros-y-quote-unquote/12-macros-y-quote-unquote.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO headings duplicados

### `02-intermedio/tooling-and-ecosystem/122-hex-publish-private/122-hex-publish-private.md` — Score 12/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/374-ets-match-specs/374-ets-match-specs.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/375-ordered-set-range-queries/375-ordered-set-range-queries.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/376-dets-persistence/376-dets-persistence.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/377-mnesia-transactions-indices/377-mnesia-transactions-indices.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/378-mnesia-distributed-replicas/378-mnesia-distributed-replicas.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/ets-dets-mnesia/379-ets-shard-pattern/379-ets-shard-pattern.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/interop-and-native/318-rustler-nif-typed/318-rustler-nif-typed.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/interop-and-native/325-nif-resource-env/325-nif-resource-env.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/05-genserver-hot-state-migration/05-genserver-hot-state-migration.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/otp-advanced/33-gen-statem-state-machine/33-gen-statem-state-machine.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/otp-advanced/384-gen-statem-state-functions-vs-handle-event/384-gen-statem-state-functions-vs-handle-event.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/otp-advanced/386-hot-code-upgrades-release-handler/386-hot-code-upgrades-release-handler.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/77-sys-replace-state-live/77-sys-replace-state-live.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/78-sys-suspend-resume/78-sys-suspend-resume.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/79-proc-lib-start-link/79-proc-lib-start-link.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/otp-advanced/84-trap-exit-semantics/84-trap-exit-semantics.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/phoenix-ecosystem/351-liveview-stateful-streams/351-liveview-stateful-streams.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/352-liveview-uploads-progress/352-liveview-uploads-progress.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/353-liveview-js-interactions/353-liveview-js-interactions.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/354-channels-presence/354-channels-presence.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/355-pubsub-local-vs-distributed/355-pubsub-local-vs-distributed.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/356-phoenix-token-signing/356-phoenix-token-signing.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/357-livedashboard-custom-metrics/357-livedashboard-custom-metrics.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/358-tailwind-esbuild-pipeline/358-tailwind-esbuild-pipeline.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/359-endpoint-custom-plug-pipeline/359-endpoint-custom-plug-pipeline.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/57-plug-pipeline-middleware/57-plug-pipeline-middleware.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/resilience-patterns/303-circuit-breaker-states-ets/303-circuit-breaker-states-ets.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/resilience-patterns/36-circuit-breaker-patterns/36-circuit-breaker-patterns.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/resilience-patterns/45-build-api-client-wrapper/45-build-api-client-wrapper.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/resilience-patterns/70-req-finch-http-clients/70-req-finch-http-clients.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/resilience-patterns/71-rate-limiter-ets/71-rate-limiter-ets.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/06-supervision-strategies-advanced/06-supervision-strategies-advanced.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/07-partition-supervisor/07-partition-supervisor.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/08-task-supervisor-dynamic/08-task-supervisor-dynamic.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/09-supervision-tree-design/09-supervision-tree-design.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/10-graceful-shutdown-drain/10-graceful-shutdown-drain.md` — Score 12/17

- FAIL: ## Why <topic> matters
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/supervision-advanced/88-drain-on-deploy/88-drain-on-deploy.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/89-significant-child/89-significant-child.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/92-overlays-assemble/92-overlays-assemble.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/93-dynamic-sup-autoscale/93-dynamic-sup-autoscale.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/94-failure-isolation/94-failure-isolation.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `03-avanzado/supervision-advanced/95-task-sup-rate-limited/95-task-sup-rate-limited.md` — Score 12/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '## Executable Example'

### `04-insane/frameworks-and-engines/32-build-web-framework-phoenix-like/32-build-web-framework-phoenix-like.md` — Score 12/17

- FAIL: ### `mix.exs` (dentro de Implementation)
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `01-basico/fundamentals/38-integer-float-precision/38-integer-float-precision.md` — Score 13/17

- FAIL: ## Why <topic> matters
- FAIL: ## Key concepts (al final)
- FAIL: NO anti-patron: '### Dependencies (mix.exs)'
- FAIL: NO anti-patron: '## Executable Example'

### `02-intermedio/applications-and-releases/06-application-callbacks/06-application-callbacks.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/20-configuracion-config-runtime/20-configuracion-config-runtime.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/21-releases-mix-basico/21-releases-mix-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/66-app-env-runtime/66-app-env-runtime.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/67-get-env-vs-compile-env/67-get-env-vs-compile-env.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/68-release-config-providers/68-release-config-providers.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/69-release-runtime-overlays/69-release-runtime-overlays.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/70-release-eval-rpc/70-release-eval-rpc.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/71-umbrella-intro/71-umbrella-intro.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/72-release-hot-upgrade-intro/72-release-hot-upgrade-intro.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/applications-and-releases/73-release-systemd-packaging/73-release-systemd-packaging.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/100-named-public-private/100-named-public-private.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/101-lookup-benchmark/101-lookup-benchmark.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/13-ets-basico/13-ets-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/95-set-vs-bag/95-set-vs-bag.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/96-ordered-set-range/96-ordered-set-range.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/97-match-object/97-match-object.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/98-select-match-spec/98-select-match-spec.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/ets-basics/99-ets-counters/99-ets-counters.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/128-ecto-changesets/128-ecto-changesets.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/130-ecto-repo-transactions/130-ecto-repo-transactions.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/131-telemetry-metrics-prom/131-telemetry-metrics-prom.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/132-telemetry-span/132-telemetry-span.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/135-jason-vs-poison/135-jason-vs-poison.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/139-broadway-rabbit-basic/139-broadway-rabbit-basic.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/28-telemetry-basico/28-telemetry-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/integrations-basics/35-nimbleparsec-csv-basico/35-nimbleparsec-csv-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/23-kernel-y-builtins-avanzados/23-kernel-y-builtins-avanzados.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/87-macro-unless/87-macro-unless.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/88-macro-trace-function/88-macro-trace-function.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/89-macro-compile-time-config/89-macro-compile-time-config.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/90-macro-pipeline-ops/90-macro-pipeline-ops.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/91-macro-guard-friendly/91-macro-guard-friendly.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/92-macro-safe-injection/92-macro-safe-injection.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/93-quote-bind-quoted/93-quote-bind-quoted.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/macros-intro/94-macro-hygiene-pitfalls/94-macro-hygiene-pitfalls.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/02-agent-basico/02-agent-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/03-task-y-concurrencia/03-task-y-concurrencia.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/31-concurrency-patterns-fan-out/31-concurrency-patterns-fan-out.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/48-agent-ttl-cache/48-agent-ttl-cache.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/49-agent-vs-genserver-tradeoff/49-agent-vs-genserver-tradeoff.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/50-task-await-many/50-task-await-many.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/51-task-async-stream-images/51-task-async-stream-images.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/52-task-sup-fire-forget/52-task-sup-fire-forget.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/53-task-yield-many-timeouts/53-task-yield-many-timeouts.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/54-task-shutdown-semantics/54-task-shutdown-semantics.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-agents-and-tasks/55-bounded-concurrency-pool/55-bounded-concurrency-pool.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/01-procesos-spawn-send-receive/01-procesos-spawn-send-receive.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/04-genserver-basico/04-genserver-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/30-process-dictionary-erlang/30-process-dictionary-erlang.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/36-genserver-counter-reset/36-genserver-counter-reset.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/37-genserver-bounded-buffer/37-genserver-bounded-buffer.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/38-genserver-rate-counter/38-genserver-rate-counter.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/39-call-vs-cast-tradeoffs/39-call-vs-cast-tradeoffs.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/40-handle-info-timers/40-handle-info-timers.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/41-monitor-external-process/41-monitor-external-process.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/42-terminate-cleanup/42-terminate-cleanup.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/43-state-validation/43-state-validation.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/44-stop-reasons/44-stop-reasons.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/45-format-status-debug/45-format-status-debug.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/46-proxy-delegator/46-proxy-delegator.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/47-testing-with-mox/47-testing-with-mox.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/10-comprehensions-avanzadas/10-comprehensions-avanzadas.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/102-stream-resource-file/102-stream-resource-file.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/103-stream-unfold-fibonacci/103-stream-unfold-fibonacci.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/104-stream-zip-timelines/104-stream-zip-timelines.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/105-stream-chunk-every/105-stream-chunk-every.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/106-stream-transform-stateful/106-stream-transform-stateful.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/107-genstage-producer-consumer/107-genstage-producer-consumer.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/108-genstage-buffer-demand/108-genstage-buffer-demand.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/109-flow-parallel-word-count/109-flow-parallel-word-count.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/11-streams-lazy-evaluation/11-streams-lazy-evaluation.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/streams-and-lazy/27-genstage-basico/27-genstage-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/05-supervisor-basico/05-supervisor-basico.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/26-dynamic-supervisor/26-dynamic-supervisor.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/56-one-for-one/56-one-for-one.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/57-one-for-all/57-one-for-all.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/58-rest-for-one/58-rest-for-one.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/59-max-restarts-intensity/59-max-restarts-intensity.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/60-child-spec-custom/60-child-spec-custom.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/61-dynamic-sup-worker-pool/61-dynamic-sup-worker-pool.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/62-shutdown-timeouts/62-shutdown-timeouts.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/63-nested-trees/63-nested-trees.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/64-transient-temporary-permanent/64-transient-temporary-permanent.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/supervision-basics/65-testing-restarts/65-testing-restarts.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/120-mix-generator/120-mix-generator.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/121-mix-aliases/121-mix-aliases.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/123-iex-helpers/123-iex-helpers.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/124-observer-inspection/124-observer-inspection.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/125-recon-intro/125-recon-intro.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/126-credo-rules/126-credo-rules.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/127-formatter-custom/127-formatter-custom.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/17-debugging-io-inspect/17-debugging-io-inspect.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/18-mix-tasks-personalizadas/18-mix-tasks-personalizadas.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/19-dependencias-hex/19-dependencias-hex.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/tooling-and-ecosystem/22-documentacion-exdoc/22-documentacion-exdoc.md` — Score 13/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `03-avanzado/phoenix-ecosystem/58-plug-router-api/58-plug-router-api.md` — Score 13/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ### `script/main.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/otp-genservers/48-hibernation-memory/48-hibernation-memory.md` — Score 14/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/protocols-and-behaviours/07-behaviours-y-callbacks/07-behaviours-y-callbacks.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/08-protocols/08-protocols.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/32-string-chars-inspect-protocol/32-string-chars-inspect-protocol.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/33-access-behaviour/33-access-behaviour.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/34-collectable-enumerable/34-collectable-enumerable.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/74-protocol-jsonable/74-protocol-jsonable.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/75-protocol-fallback-any/75-protocol-fallback-any.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/76-behaviour-adapter/76-behaviour-adapter.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/77-behaviour-strategy/77-behaviour-strategy.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/78-protocol-vs-behaviour/78-protocol-vs-behaviour.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/79-enumerable-custom-tree/79-enumerable-custom-tree.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/protocols-and-behaviours/80-inspect-algebra-custom/80-inspect-algebra-custom.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/14-registry-dinamico/14-registry-dinamico.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/81-registry-pubsub/81-registry-pubsub.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/82-registry-partitioned/82-registry-partitioned.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/83-registry-vs-global/83-registry-vs-global.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/84-via-tuple-patterns/84-via-tuple-patterns.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/85-process-groups-pg/85-process-groups-pg.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/86-naming-strategies-review/86-naming-strategies-review.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/registries-and-naming/87-via-hibernation-error-handling/87-via-hibernation-error-handling.md` — Score 14/17

- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)
- FAIL: ## Key concepts (al final)

### `02-intermedio/testing-and-quality/110-exunit-setup-contexts/110-exunit-setup-contexts.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/111-exunit-tags/111-exunit-tags.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/112-capture-log/112-capture-log.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/113-start-supervised/113-start-supervised.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/114-doctests-with-assertions/114-doctests-with-assertions.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/115-typespecs-opaque/115-typespecs-opaque.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/116-dialyzer-success-typing/116-dialyzer-success-typing.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/117-stream-data-basics/117-stream-data-basics.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/118-mox-verify-on-exit/118-mox-verify-on-exit.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/119-coverage-ci/119-coverage-ci.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/15-spec-y-tipado/15-spec-y-tipado.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `02-intermedio/testing-and-quality/16-testing-exunit/16-testing-exunit.md` — Score 14/17

- FAIL: ## The business problem
- FAIL: ### `lib/<app>.ex` (dentro de Implementation)
- FAIL: ### `test/<app>_test.exs` (dentro de Implementation)

### `03-avanzado/advanced-testing/200-mox-stub-many/200-mox-stub-many.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/288-stream-data-generators/288-stream-data-generators.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/289-mox-contracts-behaviours/289-mox-contracts-behaviours.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/290-bypass-http-tests/290-bypass-http-tests.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/291-ecto-sandbox-modes/291-ecto-sandbox-modes.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/292-genserver-telemetry-testing/292-genserver-telemetry-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/293-capture-log-assert-raise/293-capture-log-assert-raise.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/294-supervisor-restart-testing/294-supervisor-restart-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/295-async-tests-isolation/295-async-tests-isolation.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/296-phoenix-conn-integration/296-phoenix-conn-integration.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/297-doctest-side-effects/297-doctest-side-effects.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/298-benchmark-regression/298-benchmark-regression.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/299-ex-machina-factories/299-ex-machina-factories.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/300-peer-distributed-testing/300-peer-distributed-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/301-mutation-testing-elixir/301-mutation-testing-elixir.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/302-contract-testing-pact/302-contract-testing-pact.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/38-mox-testing/38-mox-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/39-property-based-testing/39-property-based-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/40-bypass-http-testing/40-bypass-http-testing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/advanced-testing/41-concurrent-testing-exunit/41-concurrent-testing-exunit.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/245-subscriptions-pubsub/245-subscriptions-pubsub.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/246-batching-n-plus-1/246-batching-n-plus-1.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/247-dataloader-ecto/247-dataloader-ecto.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/248-middleware-auth/248-middleware-auth.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/249-complexity-analysis/249-complexity-analysis.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/250-file-uploads/250-file-uploads.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/251-openapi-gen/251-openapi-gen.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/252-pagination-cursor/252-pagination-cursor.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/331-absinthe-dataloader/331-absinthe-dataloader.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/332-absinthe-subscriptions/332-absinthe-subscriptions.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/333-persisted-queries-complexity/333-persisted-queries-complexity.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/334-rest-api-versioning/334-rest-api-versioning.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/335-openapi-spex-generation/335-openapi-spex-generation.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/336-jwt-joken-refresh/336-jwt-joken-refresh.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/337-oauth2-assent-flow/337-oauth2-assent-flow.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/338-webhooks-hmac-signed/338-webhooks-hmac-signed.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/339-rate-limit-api-key-ets/339-rate-limit-api-key-ets.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/340-relay-cursor-pagination/340-relay-cursor-pagination.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/59-absinthe-graphql-schema/59-absinthe-graphql-schema.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/apis-and-graphql/60-absinthe-dataloader-auth/60-absinthe-dataloader-auth.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/152-recon-trace-prod/152-recon-trace-prod.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/153-recon-info-top/153-recon-info-top.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/154-dbg-tpl/154-dbg-tpl.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/155-flame-graph-eflame/155-flame-graph-eflame.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/156-benchee-parallel/156-benchee-parallel.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/157-benchee-memory/157-benchee-memory.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/25-beam-schedulers-reductions/25-beam-schedulers-reductions.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/26-memory-profiling-recon/26-memory-profiling-recon.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/27-tracing-sys-dbg/27-tracing-sys-dbg.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/28-benchmarking-benchee/28-benchmarking-benchee.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/29-binary-matching-performance/29-binary-matching-performance.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/360-scheduler-observation/360-scheduler-observation.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/361-reductions-budget-bump/361-reductions-budget-bump.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/362-process-heap-tuning/362-process-heap-tuning.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/363-gc-tuning-per-process/363-gc-tuning-per-process.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/364-binary-refc-leaks/364-binary-refc-leaks.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/365-jit-beamasm-considerations/365-jit-beamasm-considerations.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/366-recon-production-diagnosis/366-recon-production-diagnosis.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/48-mix-release-advanced/48-mix-release-advanced.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/beam-internals-and-perf/50-competitive-programming-graphs/50-competitive-programming-graphs.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/173-dispatcher-broadcast/173-dispatcher-broadcast.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/174-dispatcher-partition/174-dispatcher-partition.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/175-dispatcher-demand/175-dispatcher-demand.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/176-broadway-sqs/176-broadway-sqs.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/177-broadway-rabbit/177-broadway-rabbit.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/178-broadway-batcher/178-broadway-batcher.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/179-broadway-ack-custom/179-broadway-ack-custom.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/34-genstage-advanced/34-genstage-advanced.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/341-genstage-producer-consumer/341-genstage-producer-consumer.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/342-flow-parallel-mapreduce/342-flow-parallel-mapreduce.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/343-broadway-rabbitmq/343-broadway-rabbitmq.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/344-broadway-sqs-batchers/344-broadway-sqs-batchers.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/345-broadway-kafka-partition/345-broadway-kafka-partition.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/346-stream-gzip-composition/346-stream-gzip-composition.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/347-nimble-csv-huge-files/347-nimble-csv-huge-files.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/348-exactly-once-idempotency/348-exactly-once-idempotency.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/349-checkpointing-resumable/349-checkpointing-resumable.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/35-broadway-data-pipelines/35-broadway-data-pipelines.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/350-etl-http-flow-ecto/350-etl-http-flow-ecto.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/data-pipelines/69-broadway-kafka-pipelines/69-broadway-kafka-pipelines.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/101-libcluster-epmd/101-libcluster-epmd.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/106-pg2-to-pg/106-pg2-to-pg.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/107-pubsub-redis/107-pubsub-redis.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/108-erpc-vs-rpc/108-erpc-vs-rpc.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/109-net-kernel-tick/109-net-kernel-tick.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/11-distributed-erlang-clustering/11-distributed-erlang-clustering.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/110-ping-monitoring/110-ping-monitoring.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/111-split-brain-handling/111-split-brain-handling.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/112-horde-locks/112-horde-locks.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/12-global-process-registry/12-global-process-registry.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/13-horde-distributed-registry/13-horde-distributed-registry.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/14-phoenix-pubsub-advanced/14-phoenix-pubsub-advanced.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/15-rpc-and-remote-calls/15-rpc-and-remote-calls.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/367-libcluster-epmd-dns/367-libcluster-epmd-dns.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/368-horde-registry-dynamic-supervisor/368-horde-registry-dynamic-supervisor.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/369-phoenix-pubsub-distributed/369-phoenix-pubsub-distributed.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/370-node-monitoring/370-node-monitoring.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/371-global-locks/371-global-locks.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/372-distributed-rate-limiter-consistent-hashing/372-distributed-rate-limiter-consistent-hashing.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/distribution-and-clustering/373-split-brain-detection/373-split-brain-detection.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/268-nerves-ota/268-nerves-ota.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/269-nx-defn-autograd/269-nx-defn-autograd.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/270-axon-training-loop/270-axon-training-loop.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/271-axon-inference-server/271-axon-inference-server.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/380-ash-resource-actions/380-ash-resource-actions.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/381-commanded-aggregate-events-projections/381-commanded-aggregate-events-projections.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/382-cqrs-commanded-eventstore/382-cqrs-commanded-eventstore.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/383-event-sourcing-from-scratch-snapshots/383-event-sourcing-from-scratch-snapshots.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/42-build-event-bus/42-build-event-bus.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/44-build-job-scheduler/44-build-job-scheduler.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/46-oban-background-jobs/46-oban-background-jobs.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/49-livebook-integration/49-livebook-integration.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/61-nx-numerical-elixir/61-nx-numerical-elixir.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/62-axon-neural-networks/62-axon-neural-networks.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/63-nerves-iot-embedded/63-nerves-iot-embedded.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/64-nerves-networking-cloud/64-nerves-networking-cloud.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/65-commanded-aggregates-commands/65-commanded-aggregates-commands.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/66-commanded-projections-read-models/66-commanded-projections-read-models.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/67-ash-framework-resources/67-ash-framework-resources.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/domain-frameworks/68-ash-extensions-api/68-ash-extensions-api.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/230-ecto-multi/230-ecto-multi.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/231-preload-nested/231-preload-nested.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/272-multi-tenancy-prefix/272-multi-tenancy-prefix.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/273-ecto-multi-transactions/273-ecto-multi-transactions.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/274-upsert-on-conflict/274-upsert-on-conflict.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/275-custom-ecto-type-money/275-custom-ecto-type-money.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/276-polymorphic-associations/276-polymorphic-associations.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/277-preload-optimization/277-preload-optimization.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/278-window-functions-select-merge/278-window-functions-select-merge.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/279-ctes-recursive-queries/279-ctes-recursive-queries.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/280-subqueries-advanced/280-subqueries-advanced.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/281-schemaless-queries-etl/281-schemaless-queries-etl.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/282-zero-downtime-migrations/282-zero-downtime-migrations.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/283-row-level-locking/283-row-level-locking.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/284-soft-delete-global-filter/284-soft-delete-global-filter.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/285-full-text-search-tsvector/285-full-text-search-tsvector.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/286-embedded-schemas-nested-changesets/286-embedded-schemas-nested-changesets.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/287-custom-query-macros/287-custom-query-macros.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/55-ecto-queries-avanzadas/55-ecto-queries-avanzadas.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `03-avanzado/ecto-advanced/56-ecto-schemas-avanzados/56-ecto-schemas-avanzados.md` — Score 16/17

- FAIL: ## Key concepts (al final)

### `04-insane/distributed-systems/44-build-full-stack-distributed-system/44-build-full-stack-distributed-system.md` — Score 16/17

- FAIL: ## Why <topic> matters

### `04-insane/distributed-systems/50-build-distributed-lock-service/50-build-distributed-lock-service.md` — Score 16/17

- FAIL: ## Why <topic> matters

### `04-insane/distributed-systems/55-build-distributed-saga-orchestrator/55-build-distributed-saga-orchestrator.md` — Score 16/17

- FAIL: ## Why <topic> matters
