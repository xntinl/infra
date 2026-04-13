# REV-MIX: Auditoria de bloques `mix.exs`

**Fecha**: 2026-04-13
**Total archivos auditados**: 554

## Resumen Ejecutivo

- **REV-MIX: 349/554 con mix.exs completo y correcto**
- Archivos sin heading `### \`mix.exs\``: 82
- Heading sin bloque de codigo: 2
- Con bloque de codigo: 470
- Completos (16/16 checks): 349 (62%)

## Fallos por Check

| Check | Archivos con fallo |
|-------|-------------------:|
| `project_has_start_permanent` | 132 (23%) |
| `has_def_application` | 118 (21%) |
| `app_has_extra_apps_logger` | 118 (21%) |
| `project_has_deps_ref` | 116 (20%) |
| `has_def_project` | 115 (20%) |
| `project_has_app` | 115 (20%) |
| `project_has_version` | 115 (20%) |
| `project_has_elixir` | 115 (20%) |
| `has_defmodule` | 110 (19%) |
| `has_use_mix_project` | 110 (19%) |
| `anti_orphan_defp_deps` | 95 (17%) |
| `has_defp_deps` | 17 (3%) |
| `defp_deps_returns_list` | 17 (3%) |
| `has_closing_end` | 14 (2%) |
| `anti_no_do` | 0 (0%) |
| `anti_brackets_ok` | 0 (0%) |
| `anti_no_duplicate_funcs` | 0 (0%) |

## Distribucion de Scores

| Score | Archivos |
|-------|---------:|
| 4/16 | 14 |
| 5/16 | 1 |
| 6/16 | 95 |
| 9/16 | 3 |
| 11/16 | 2 |
| 12/16 | 1 |
| 13/16 | 1 |
| 14/16 | 4 |
| 16/16 | 349 |
| Sin heading | 82 |
| Sin codigo | 2 |

## TOP 10 Peor Calidad (con bloque presente)

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/ets-dets-mnesia/374-ets-match-specs/374-ets-match-specs.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/ets-dets-mnesia/377-mnesia-transactions-indices/377-mnesia-transactions-indices.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/otp-advanced/33-gen-statem-state-machine/33-gen-statem-state-machine.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/phoenix-ecosystem/353-liveview-js-interactions/353-liveview-js-interactions.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/phoenix-ecosystem/358-tailwind-esbuild-pipeline/358-tailwind-esbuild-pipeline.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/phoenix-ecosystem/359-endpoint-custom-plug-pipeline/359-endpoint-custom-plug-pipeline.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/phoenix-ecosystem/57-plug-pipeline-middleware/57-plug-pipeline-middleware.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/phoenix-ecosystem/58-plug-router-api/58-plug-router-api.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/resilience-patterns/45-build-api-client-wrapper/45-build-api-client-wrapper.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end

### [4/16] `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/resilience-patterns/70-req-finch-http-clients/70-req-finch-http-clients.md`

- Falta defmodule X.MixProject do
- Falta use Mix.Project
- Falta def project do
- project sin app:
- project sin version:
- project sin elixir: ~>
- project sin deps: deps()
- Falta def application do
- application sin extra_applications: [:logger]
- Falta defp deps do
- defp deps no devuelve lista
- No termina con end


## Archivos Sin Heading `### \`mix.exs\`` (82)

- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/11-enum-module-and-immutability/11-enum-module-and-immutability.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/17-ranges-and-streams/17-ranges-and-streams.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/19-comprehensions-for/19-comprehensions-for.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/44-enum-reduce-patterns/44-enum-reduce-patterns.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/45-stream-resource-infinite/45-stream-resource-infinite.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/46-mapset-operations/46-mapset-operations.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/collections/47-keyword-dsl-building/47-keyword-dsl-building.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/20-guards-and-custom-guards/20-guards-and-custom-guards.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/21-with-keyword-flow/21-with-keyword-flow.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/56-case-deep-patterns/56-case-deep-patterns.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/57-cond-vs-if-choices/57-cond-vs-if-choices.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/58-unless-else-guards/58-unless-else-guards.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/control-flow/59-try-catch-after-cleanup/59-try-catch-after-cleanup.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/15-structs-and-basic-validation/15-structs-and-basic-validation.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/60-enforce-keys-structs/60-enforce-keys-structs.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/61-protocols-polymorphism/61-protocols-polymorphism.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/62-behaviours-contracts/62-behaviours-contracts.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/data-structures/63-typespecs-dialyzer/63-typespecs-dialyzer.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/25-error-handling-try-rescue/25-error-handling-try-rescue.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/29-exceptions-try-rescue/29-exceptions-try-rescue.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/30-error-tuples-vs-raise/30-error-tuples-vs-raise.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/64-exit-vs-raise-vs-throw/64-exit-vs-raise-vs-throw.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/65-trap-exit-linked/65-trap-exit-linked.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/66-custom-exception-modules/66-custom-exception-modules.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/error-handling/67-bang-vs-safe-apis/67-bang-vs-safe-apis.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/08-functions-and-arity/08-functions-and-arity.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/14-modules-and-function-visibility/14-modules-and-function-visibility.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/52-function-capture-ampersand/52-function-capture-ampersand.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/53-default-arguments-patterns/53-default-arguments-patterns.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/54-multi-clause-functions/54-multi-clause-functions.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/functions-and-modules/55-module-import-alias-require/55-module-import-alias-require.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/01-setup-and-mix.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/01-setup-and-mix/json_validator/README.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/02-atoms-and-symbols/02-atoms-and-symbols.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/36-booleans-nil-truthiness/36-booleans-nil-truthiness.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/37-variable-rebinding-and-scope/37-variable-rebinding-and-scope.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/fundamentals/39-module-attributes-constants/39-module-attributes-constants.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/40-pin-operator-deep/40-pin-operator-deep.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/41-map-pattern-matching/41-map-pattern-matching.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/42-nested-destructuring/42-nested-destructuring.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/pattern-matching/43-case-with-guards/43-case-with-guards.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/31-process-spawn-and-send/31-process-spawn-and-send.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/32-receive-and-mailbox/32-receive-and-mailbox.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/33-task-async-await/33-task-async-await.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/34-agent-state-basics/34-agent-state-basics.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/35-registry-and-named-processes/35-registry-and-named-processes.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/72-process-link-exit/72-process-link-exit.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/73-process-monitor/73-process-monitor.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/74-process-send-after/74-process-send-after.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/processes-basics/75-process-dict-when-not/75-process-dict-when-not.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/23-io-and-stdio/23-io-and-stdio.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/24-file-io-and-streams/24-file-io-and-streams.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/25-path-and-filesystem/25-path-and-filesystem.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/26-datetime-naivedatetime/26-datetime-naivedatetime.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/27-keyword-vs-map-opts/27-keyword-vs-map-opts.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/68-uri-parsing/68-uri-parsing.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/69-base-encoding/69-base-encoding.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/70-system-env-vars/70-system-env-vars.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/stdlib-essentials/71-code-eval-sandboxed/71-code-eval-sandboxed.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/04-strings-and-binaries-basics/04-strings-and-binaries-basics.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/16-sigils-and-regex/16-sigils-and-regex.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/18-charlists-vs-strings/18-charlists-vs-strings.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/22-string-chars-protocol/22-string-chars-protocol.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/24-bitstrings-binarios-avanzados/24-bitstrings-binarios-avanzados.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/28-binaries-bit-syntax/28-binaries-bit-syntax.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/48-unicode-normalization/48-unicode-normalization.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/49-string-split-trim-edge/49-string-split-trim-edge.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/50-binary-construction/50-binary-construction.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/01-basico/text-and-binaries/51-base64-url-encoding/51-base64-url-encoding.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/metaprogramming/389-dsl-macro-prewalk-special-forms/389-dsl-macro-prewalk-special-forms.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/otp-advanced/01-genserver-hibernation-state-compaction/01-genserver-hibernation-state-compaction.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/otp-advanced/03-genserver-timeouts-and-heartbeats/03-genserver-timeouts-and-heartbeats.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/04-insane/storage-engines/README-v3-AMPLIFICATION.md`

## Archivos con Heading pero Sin Bloque de Codigo (2)

- `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/resilience-patterns/71-rate-limiter-ets/71-rate-limiter-ets.md`
- `/Users/consulting/Documents/consulting/infra/challenges/elixir/03-avanzado/supervision-advanced/07-partition-supervisor/07-partition-supervisor.md`

## Analisis de Fallos Sistematicos

No se detectan fallos sistematicos (ningun check falla en >30% de archivos).
