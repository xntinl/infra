# Elixir Challenges & Exercises

> 175 practical Elixir exercises organized in 4 difficulty levels.
> From first `mix new` to building distributed consensus algorithms, compilers, and full-stack systems on the BEAM VM.
> Each exercise includes learning objectives, runnable code, verification commands, and references.

**Difficulty Levels**:
- **Basico** (15) — Full step-by-step guidance, complete code, every term explained
- **Intermedio** (35) — Guided steps with TODO gaps, pattern vs anti-pattern comparisons, OTP fundamentals
- **Avanzado** (70) — Problem + hints + one solution, trade-off analysis, BEAM internals, production patterns
- **Insane** (55) — Problem statement + acceptance criteria only, no code provided

**Requirements**:
- Elixir installed via [asdf](https://asdf-vm.com) or [mise](https://mise.jdx.dev) (`elixir` + `erlang` plugins)
- `mix` and `iex` available
- A terminal and text editor (or Livebook for interactive exploration)

**Convention**: Each exercise uses `mix new` for a fresh project. Clean up with `rm -rf` when done.

---

## Por qué Elixir

Elixir corre sobre la BEAM, la máquina virtual de Erlang diseñada por Ericsson en los años 80 para sistemas de telecomunicaciones que no pueden fallar. La BEAM no es un runtime de propósito general adaptado a la concurrencia: fue construida desde cero para ello. Millones de procesos ligeros, aislamiento total de fallos, hot-code reloading en producción, y garbage collection por proceso son primitivas del runtime, no bibliotecas encima.

OTP (Open Telecom Platform) es el framework que viene con Erlang/Elixir para construir sistemas tolerantes a fallos. GenServer, Supervisor, Application, Registry, y las supervision trees no son patrones que debes implementar: son abstracciones probadas en producción por 30+ años en sistemas que requieren 99.9999999% de disponibilidad (nueve nueves). Entender OTP es entender cómo se diseñan sistemas que se curan solos.

Elixir añade sobre Erlang una sintaxis moderna y expresiva, metaprogramación poderosa vía macros sobre el AST, el ecosistema Phoenix para web de alta concurrencia, Ecto para base de datos, y una comunidad que valora la legibilidad y la correctitud. Si construyes sistemas que necesitan escala, resiliencia, o concurrencia masiva — APIs, juegos en tiempo real, pipelines de datos, IoT — Elixir y la BEAM son una de las mejores herramientas disponibles.

---

## Prerequisitos

- Familiaridad con algún lenguaje de programación (no necesariamente funcional)
- Conceptos básicos de terminal y línea de comandos
- Para niveles Avanzado e Insane: experiencia con sistemas concurrentes y distribuidos

---

## Estructura del Curriculum

| Nivel | Ejercicios | Descripción |
|-------|-----------|-------------|
| 01-Basico | 15 | Setup, tipos, pattern matching, funciones, módulos, structs |
| 02-Intermedio | 35 | Procesos, OTP básico, metaprogramming, tooling, protocols, Ecto |
| 03-Avanzado | 70 | OTP profundo, distribución, BEAM internals, Phoenix, Ecto, GraphQL, Nx, Nerves, Commanded, Ash |
| 04-Insane | 55 | Sistemas completos: Raft, compilers, databases, frameworks, distributed systems, AI agents |
| **Total** | **175** | |

---

## Nivel 01-Basico

| # | Ejercicio | Conceptos Principales | Dificultad |
|---|-----------|----------------------|-----------|
| 01 | [Setup and Mix](01-basico/01-setup-and-mix/01-setup-and-mix.md) | mix new, iex -S mix, Hex, proyecto estructura | Basico |
| 02 | [Atoms and Symbols](01-basico/02-atoms-and-symbols/02-atoms-and-symbols.md) | :atom, true/false/nil, pattern matching con atoms | Basico |
| 03 | [Numbers and Arithmetic](01-basico/03-numbers-and-arithmetic/03-numbers-and-arithmetic.md) | Integer, Float, operadores, div/rem, bignum nativo | Basico |
| 04 | [Strings and Binaries Basics](01-basico/04-strings-and-binaries-basics/04-strings-and-binaries-basics.md) | String UTF-8, binaries, String module, interpolación | Basico |
| 05 | [Tuples and Pattern Matching Intro](01-basico/05-tuples-and-pattern-matching-intro/05-tuples-and-pattern-matching-intro.md) | {ok, value}, {:error, reason}, match operator `=` | Basico |
| 06 | [Lists and Head-Tail Pattern](01-basico/06-lists-and-head-tail-pattern/06-lists-and-head-tail-pattern.md) | [h \| t], linked lists, List module, recursión básica | Basico |
| 07 | [Maps and Keyword Lists](01-basico/07-maps-and-keyword-lists/07-maps-and-keyword-lists.md) | %{}, keyword lists, acceso, actualización inmutable | Basico |
| 08 | [Functions and Arity](01-basico/08-functions-and-arity/08-functions-and-arity.md) | def, defp, arity, múltiples cláusulas, guards básicos | Basico |
| 09 | [Control Flow: if, case, cond](01-basico/09-control-flow-if-case-cond/09-control-flow-if-case-cond.md) | if/unless, case, cond, with, expresiones vs statements | Basico |
| 10 | [Recursion and Tail Call Optimization](01-basico/10-recursion-and-tail-call-optimization/10-recursion-and-tail-call-optimization.md) | TCO, acumulador pattern, factorial, fibonacci, suma lista | Basico |
| 11 | [Enum Module and Immutability](01-basico/11-enum-module-and-immutability/11-enum-module-and-immutability.md) | Enum.map/filter/reduce, immutability, data transformation | Basico |
| 12 | [Pipe Operator and Composition](01-basico/12-pipe-operator-and-composition/12-pipe-operator-and-composition.md) | \|>, function composition, legibilidad, pipelines | Basico |
| 13 | [Anonymous Functions and Closures](01-basico/13-anonymous-functions-and-closures/13-anonymous-functions-and-closures.md) | fn -> end, &capture, closures, higher-order functions | Basico |
| 14 | [Modules and Function Visibility](01-basico/14-modules-and-function-visibility/14-modules-and-function-visibility.md) | defmodule, def/defp, alias/import/use, namespacing | Basico |
| 15 | [Structs and Basic Validation](01-basico/15-structs-and-basic-validation/15-structs-and-basic-validation.md) | defstruct, %MyStruct{}, default values, pattern matching structs | Basico |

---

## Nivel 02-Intermedio

| # | Ejercicio | Conceptos Principales | Dificultad |
|---|-----------|----------------------|-----------|
| 01 | [Procesos: spawn, send, receive](02-intermedio/01-procesos-spawn-send-receive/01-procesos-spawn-send-receive.md) | spawn/1, send/2, receive, mailbox, process isolation | Intermedio |
| 02 | [Agent Básico](02-intermedio/02-agent-basico/02-agent-basico.md) | Agent.start_link, get, update, estado compartido simple | Intermedio |
| 03 | [Task y Concurrencia](02-intermedio/03-task-y-concurrencia/03-task-y-concurrencia.md) | Task.async, Task.await, Task.async_stream, timeouts | Intermedio |
| 04 | [GenServer Básico](02-intermedio/04-genserver-basico/04-genserver-basico.md) | handle_call, handle_cast, handle_info, init, state | Intermedio |
| 05 | [Supervisor Básico](02-intermedio/05-supervisor-basico/05-supervisor-basico.md) | Supervisor.start_link, child specs, restart strategies | Intermedio |
| 06 | [Application Callbacks](02-intermedio/06-application-callbacks/06-application-callbacks.md) | Application behaviour, start/2, supervision tree raíz | Intermedio |
| 07 | [Behaviours y Callbacks](02-intermedio/07-behaviours-y-callbacks/07-behaviours-y-callbacks.md) | @callback, @behaviour, @impl, contratos explícitos | Intermedio |
| 08 | [Protocols](02-intermedio/08-protocols/08-protocols.md) | defprotocol, defimpl, polymorphism sin herencia | Intermedio |
| 09 | [Pattern Matching Avanzado](02-intermedio/09-pattern-matching-avanzado/09-pattern-matching-avanzado.md) | guards, pin operator ^, nested patterns, destructuring | Intermedio |
| 10 | [Comprehensions Avanzadas](02-intermedio/10-comprehensions-avanzadas/10-comprehensions-avanzadas.md) | for, generators múltiples, filters, into, reduce | Intermedio |
| 11 | [Streams: Lazy Evaluation](02-intermedio/11-streams-lazy-evaluation/11-streams-lazy-evaluation.md) | Stream.map/filter/take, infinite streams, memory efficiency | Intermedio |
| 12 | [Macros y Quote/Unquote](02-intermedio/12-macros-y-quote-unquote/12-macros-y-quote-unquote.md) | defmacro, quote/unquote, AST manipulation, hygiene | Intermedio |
| 13 | [ETS Básico](02-intermedio/13-ets-basico/13-ets-basico.md) | :ets.new, insert, lookup, table types, concurrency básica | Intermedio |
| 14 | [Registry Dinámico](02-intermedio/14-registry-dinamico/14-registry-dinamico.md) | Registry.start_link, register, lookup, PubSub pattern | Intermedio |
| 15 | [Spec y Tipado](02-intermedio/15-spec-y-typado/15-spec-y-tipado.md) | @spec, @type, @typep, dialyzer basics, type annotations | Intermedio |
| 16 | [Testing con ExUnit](02-intermedio/16-testing-exunit/16-testing-exunit.md) | ExUnit.Case, assert, describe, setup, async: true, doctests | Intermedio |
| 17 | [Debugging con IO.inspect](02-intermedio/17-debugging-io-inspect/17-debugging-io-inspect.md) | IO.inspect/2, label:, :erlang.trace, dbg/1 (Elixir 1.14+) | Intermedio |
| 18 | [Mix Tasks Personalizadas](02-intermedio/18-mix-tasks-personalizadas/18-mix-tasks-personalizadas.md) | Mix.Task, @shortdoc, @moduledoc, mix run, custom scripts | Intermedio |
| 19 | [Dependencias Hex](02-intermedio/19-dependencias-hex/19-dependencias-hex.md) | mix.exs deps, hex.pm, mix deps.get, versioning, private hex | Intermedio |
| 20 | [Configuración: Config y Runtime](02-intermedio/20-configuracion-config-runtime/20-configuracion-config-runtime.md) | config.exs, runtime.exs, Application.get_env, env vars | Intermedio |
| 21 | [Releases con Mix](02-intermedio/21-releases-mix-basico/21-releases-mix-basico.md) | mix release, rel/, bin/myapp start, releases vs scripts | Intermedio |
| 22 | [Documentación con ExDoc](02-intermedio/22-documentacion-exdoc/22-documentacion-exdoc.md) | @doc, @moduledoc, @deprecated, mix docs, doctests | Intermedio |
| 23 | [Kernel y Builtins Avanzados](02-intermedio/23-kernel-y-builtins-avanzados/23-kernel-y-builtins-avanzados.md) | Kernel functions, Process, Node, self(), send/2, apply/3 | Intermedio |
| 24 | [Bitstrings y Binarios Avanzados](02-intermedio/24-bitstrings-binarios-avanzados/24-bitstrings-binarios-avanzados.md) | <<>>, bit syntax, binary pattern matching, parsing binario | Intermedio |
| 25 | [Error Handling: try/rescue](02-intermedio/25-error-handling-try-rescue/25-error-handling-try-rescue.md) | try/rescue/catch/after, raise, throw, exit, error vs throw | Intermedio |
| 26 | [Dynamic Supervisor](02-intermedio/26-dynamic-supervisor/26-dynamic-supervisor.md) | DynamicSupervisor, start_child, terminate_child, :one_for_one | Intermedio |
| 27 | [GenStage Básico](02-intermedio/27-genstage-basico/27-genstage-basico.md) | Producer, Consumer, ProducerConsumer, demand-driven backpressure | Intermedio |
| 28 | [Telemetry Básico](02-intermedio/28-telemetry-basico/28-telemetry-basico.md) | :telemetry.execute, attach, span, TelemetryMetrics basics | Intermedio |
| 29 | [Ecto Básico](02-intermedio/29-ecto-basico/29-ecto-basico.md) | Repo, Schema, Changeset, migrations, queries básicas | Intermedio |
| 30 | [Process Dictionary (Erlang)](02-intermedio/30-process-dictionary-erlang/30-process-dictionary-erlang.md) | Process.put/get, :erlang.get/put, cuándo usarlo vs no | Intermedio |
| 31 | [Concurrency Patterns: Fan-Out](02-intermedio/31-concurrency-patterns-fan-out/31-concurrency-patterns-fan-out.md) | Task.async_stream, map-reduce con procesos, timeouts colectivos | Intermedio |
| 32 | [String.Chars y Inspect Protocol](02-intermedio/32-string-chars-inspect-protocol/32-string-chars-inspect-protocol.md) | String.Chars, Inspect, to_string, inspect custom structs | Intermedio |
| 33 | [Access Behaviour](02-intermedio/33-access-behaviour/33-access-behaviour.md) | Access.get/put/pop, get_in/put_in/update_in, lenses básicas | Intermedio |
| 34 | [Collectable y Enumerable](02-intermedio/34-collectable-enumerable/34-collectable-enumerable.md) | Enumerable protocol, Collectable, custom data structures | Intermedio |
| 35 | [NimbleParsec: CSV Básico](02-intermedio/35-nimbleparsec-csv-basico/35-nimbleparsec-csv-basico.md) | NimbleParsec, parser combinators, CSV parsing, combinators básicos | Intermedio |

---

## Nivel 03-Avanzado

| # | Ejercicio | Conceptos Principales | Dificultad |
|---|-----------|----------------------|-----------|
| 01 | [GenServer: Hibernation y State Compaction](03-avanzado/01-genserver-hibernation-state-compaction/01-genserver-hibernation-state-compaction.md) | :hibernate, Process.hibernate, memory footprint, state trimming | Avanzado |
| 02 | [GenServer: handle_continue y Recovery](03-avanzado/02-genserver-handle-continue-recovery/02-genserver-handle-continue-recovery.md) | handle_continue/2, async init, deferred initialization patterns | Avanzado |
| 03 | [GenServer: Timeouts y Heartbeats](03-avanzado/03-genserver-timeouts-and-heartbeats/03-genserver-timeouts-and-heartbeats.md) | timeout en handle_call, send_after, :erlang.cancel_timer | Avanzado |
| 04 | [GenServer: Back-pressure Queue](03-avanzado/04-genserver-back-pressure-queue/04-genserver-back-pressure-queue.md) | mailbox overflow, demand-driven processing, queue bounded | Avanzado |
| 05 | [GenServer: Hot State Migration](03-avanzado/05-genserver-hot-state-migration/05-genserver-hot-state-migration.md) | code_change/3, hot code upgrade, state versioning | Avanzado |
| 06 | [Supervision Strategies Advanced](03-avanzado/06-supervision-strategies-advanced/06-supervision-strategies-advanced.md) | :one_for_all, :rest_for_one, max_restarts, max_seconds | Avanzado |
| 07 | [PartitionSupervisor](03-avanzado/07-partition-supervisor/07-partition-supervisor.md) | PartitionSupervisor, sharding por partición, escalado horizontal | Avanzado |
| 08 | [Task.Supervisor Dinámico](03-avanzado/08-task-supervisor-dynamic/08-task-supervisor-dynamic.md) | Task.Supervisor, async_nolink, supervised tasks, error isolation | Avanzado |
| 09 | [Diseño de Supervision Trees](03-avanzado/09-supervision-tree-design/09-supervision-tree-design.md) | árbol de supervisión, fault domains, child spec design, restart granularity | Avanzado |
| 10 | [Graceful Shutdown y Drain](03-avanzado/10-graceful-shutdown-drain/10-graceful-shutdown-drain.md) | System.stop/1, :init.stop, terminate/2 cleanup, drain pattern | Avanzado |
| 11 | [Distributed Erlang Clustering](03-avanzado/11-distributed-erlang-clustering/11-distributed-erlang-clustering.md) | Node.connect, :net_kernel, cookies, node() función, netsplit | Avanzado |
| 12 | [Global Process Registry](03-avanzado/12-global-process-registry/12-global-process-registry.md) | :global.register_name, :global.whereis_name, distributed registry | Avanzado |
| 13 | [Horde: Distributed Registry](03-avanzado/13-horde-distributed-registry/13-horde-distributed-registry.md) | Horde.Registry, Horde.DynamicSupervisor, CRDT-based clustering | Avanzado |
| 14 | [Phoenix PubSub Advanced](03-avanzado/14-phoenix-pubsub-advanced/14-phoenix-pubsub-advanced.md) | Phoenix.PubSub, broadcast, subscribe, distributed pubsub patterns | Avanzado |
| 15 | [RPC y Remote Calls](03-avanzado/15-rpc-and-remote-calls/15-rpc-and-remote-calls.md) | :rpc.call, :erpc.call, async RPC, multi-node operations | Avanzado |
| 16 | [ETS Avanzado: Concurrencia](03-avanzado/16-ets-advanced-concurrent/16-ets-advanced-concurrent.md) | :read_concurrency, :write_concurrency, decentralized_counters, ordered_set | Avanzado |
| 17 | [DETS: Persistent Storage](03-avanzado/17-dets-persistent-storage/17-dets-persistent-storage.md) | :dets.open_file, insert, lookup, match, disk-backed ETS | Avanzado |
| 18 | [Mnesia Basics](03-avanzado/18-mnesia-basics/18-mnesia-basics.md) | :mnesia.create_schema, create_table, transaction, dirty ops | Avanzado |
| 19 | [Cache Patterns con ETS](03-avanzado/19-cache-patterns-ets/19-cache-patterns-ets.md) | TTL cache, LRU eviction, cache stampede prevention, GenServer + ETS | Avanzado |
| 20 | [ETS Counters y Atomics](03-avanzado/20-ets-counter-and-atomics/20-ets-counter-and-atomics.md) | :ets.update_counter, :atomics module, lock-free counters BEAM | Avanzado |
| 21 | [Advanced Macros: AST](03-avanzado/21-advanced-macros-ast/21-advanced-macros-ast.md) | Macro.to_string, Macro.expand, __ENV__, __CALLER__, hygiene | Avanzado |
| 22 | [Custom DSL con Macros](03-avanzado/22-custom-dsl-with-macros/22-custom-dsl-with-macros.md) | DSL design, __using__, Module.register_attribute, accumulate | Avanzado |
| 23 | [Protocol Consolidation](03-avanzado/23-protocol-consolidation/23-protocol-consolidation.md) | Protocol.consolidate, dispatch performance, compile vs runtime | Avanzado |
| 24 | [Behaviours Custom Avanzados](03-avanzado/24-behaviours-custom-advanced/24-behaviours-custom-advanced.md) | optional callbacks, @impl true, behaviour introspection | Avanzado |
| 25 | [BEAM Schedulers y Reductions](03-avanzado/25-beam-schedulers-reductions/25-beam-schedulers-reductions.md) | :erlang.system_info(:schedulers), reductions, scheduler hints | Avanzado |
| 26 | [Memory Profiling con Recon](03-avanzado/26-memory-profiling-recon/26-memory-profiling-recon.md) | recon library, proc_count, bin_leak, memory pressure analysis | Avanzado |
| 27 | [Tracing: :sys y :dbg](03-avanzado/27-tracing-sys-dbg/27-tracing-sys-dbg.md) | :sys.trace, :dbg.tracer, :dbg.tp, production-safe tracing | Avanzado |
| 28 | [Benchmarking con Benchee](03-avanzado/28-benchmarking-benchee/28-benchmarking-benchee.md) | Benchee.run, formatters, inputs, profiling integrado | Avanzado |
| 29 | [Binary Matching Performance](03-avanzado/29-binary-matching-performance/29-binary-matching-performance.md) | binary split vs match, reference counting, sub-binary optimization | Avanzado |
| 30 | [Erlang Interop Avanzado](03-avanzado/30-erlang-interop-advanced/30-erlang-interop-advanced.md) | :lists, :maps, :queue, :ordsets, charlist vs binary, interop patterns | Avanzado |
| 31 | [Ports: External Processes](03-avanzado/31-ports-external-processes/31-ports-external-processes.md) | Port.open, {:packet, 4}, port driver, stdin/stdout protocol | Avanzado |
| 32 | [NIFs con Rustler](03-avanzado/32-nifs-basics-rustler/32-nifs-basics-rustler.md) | rustler crate, #[rustler::nif], cargo, dirty schedulers | Avanzado |
| 33 | [:gen_statem State Machine](03-avanzado/33-gen-statem-state-machine/33-gen-statem-state-machine.md) | :gen_statem, state_functions, handle_event_function, state data | Avanzado |
| 34 | [GenStage Avanzado](03-avanzado/34-genstage-advanced/34-genstage-advanced.md) | dispatcher, BufferedProducer, flow control, partitioning | Avanzado |
| 35 | [Broadway: Data Pipelines](03-avanzado/35-broadway-data-pipelines/35-broadway-data-pipelines.md) | Broadway, producers, processors, batchers, acknowledgment | Avanzado |
| 36 | [Circuit Breaker Patterns](03-avanzado/36-circuit-breaker-patterns/36-circuit-breaker-patterns.md) | Fuse library, :half_open, reset strategy, telemetry integration | Avanzado |
| 37 | [Rate Limiting Patterns](03-avanzado/37-rate-limiting-patterns/37-rate-limiting-patterns.md) | token bucket, leaky bucket, Hammer library, ETS counters | Avanzado |
| 38 | [Mox Testing](03-avanzado/38-mox-testing/38-mox-testing.md) | Mox.defmock, expect, verify_on_exit!, behaviour mocks | Avanzado |
| 39 | [Property-Based Testing](03-avanzado/39-property-based-testing/39-property-based-testing.md) | PropCheck / StreamData, property, shrinking, generators | Avanzado |
| 40 | [Bypass: HTTP Testing](03-avanzado/40-bypass-http-testing/40-bypass-http-testing.md) | Bypass.open, expect, Plug.Router, stubbing HTTP servers | Avanzado |
| 41 | [Concurrent Testing con ExUnit](03-avanzado/41-concurrent-testing-exunit/41-concurrent-testing-exunit.md) | async: true, test isolation, Ecto.Sandbox, process ownership | Avanzado |
| 42 | [Build: Event Bus](03-avanzado/42-build-event-bus/42-build-event-bus.md) | PubSub from scratch, Registry + GenServer, topic routing | Avanzado |
| 43 | [Build: Cache Server](03-avanzado/43-build-cache-server/43-build-cache-server.md) | TTL, LRU, ETS backend, GenServer wrapper, telemetry | Avanzado |
| 44 | [Build: Job Scheduler](03-avanzado/44-build-job-scheduler/44-build-job-scheduler.md) | cron-like scheduling, Task.Supervisor, persistence, retry | Avanzado |
| 45 | [Build: API Client Wrapper](03-avanzado/45-build-api-client-wrapper/45-build-api-client-wrapper.md) | Req / Tesla, middleware, retry, circuit breaker, telemetry | Avanzado |
| 46 | [Oban: Background Jobs](03-avanzado/46-oban-background-jobs/46-oban-background-jobs.md) | Oban.Worker, queues, scheduled jobs, unique jobs, pruning | Avanzado |
| 47 | [Umbrella Application](03-avanzado/47-umbrella-application/47-umbrella-application.md) | mix new --umbrella, apps/, shared deps, inter-app communication | Avanzado |
| 48 | [Mix Release Avanzado](03-avanzado/48-mix-release-advanced/48-mix-release-advanced.md) | overlays, vm.args, remote_console, eval, config providers | Avanzado |
| 49 | [Livebook Integration](03-avanzado/49-livebook-integration/49-livebook-integration.md) | Livebook, Smart Cells, Kino, data exploration, visualization | Avanzado |
| 50 | [Competitive Programming: Graphs](03-avanzado/50-competitive-programming-graphs/50-competitive-programming-graphs.md) | BFS, DFS, Dijkstra en Elixir, :digraph, immutable graphs | Avanzado |
| 51 | [Phoenix LiveView: Real-time Dashboard](03-avanzado/51-phoenix-liveview-real-time/51-phoenix-liveview-real-time.md) | mount/3, handle_event/3, handle_info/2, PubSub, HEEx, phx-change | Avanzado |
| 52 | [Phoenix LiveComponent](03-avanzado/52-phoenix-livecomponent/52-phoenix-livecomponent.md) | stateful components, phx-target, send_update/3, slots, phx-debounce | Avanzado |
| 53 | [Phoenix Channels](03-avanzado/53-phoenix-channels/53-phoenix-channels.md) | UserSocket, join/3, handle_in/3, intercept, push/3, ChannelTest | Avanzado |
| 54 | [Phoenix Presence en Channels](03-avanzado/54-phoenix-presence-channels/54-phoenix-presence-channels.md) | Presence.track, presence_diff, list/1, online users, typing indicators | Avanzado |
| 55 | [Ecto: Queries Avanzadas](03-avanzado/55-ecto-queries-avanzadas/55-ecto-queries-avanzadas.md) | window functions, dynamic/2, Ecto.Multi, Repo.stream, subquery | Avanzado |
| 56 | [Ecto: Schemas Avanzados](03-avanzado/56-ecto-schemas-avanzados/56-ecto-schemas-avanzados.md) | embeds, polymorphic associations, multi-tenancy, N+1 avoidance | Avanzado |
| 57 | [Plug: Pipeline y Middleware](03-avanzado/57-plug-pipeline-middleware/57-plug-pipeline-middleware.md) | Plug.Builder, halt/1, conn assigns, rate limit, request tracing | Avanzado |
| 58 | [Plug.Router: API sin Phoenix](03-avanzado/58-plug-router-api/58-plug-router-api.md) | REST API con Plug puro, streaming, WebSocket raw Cowboy | Avanzado |
| 59 | [Absinthe: GraphQL Schema](03-avanzado/59-absinthe-graphql-schema/59-absinthe-graphql-schema.md) | object/input_object/enum, resolvers, subscriptions, context, scalars | Avanzado |
| 60 | [Absinthe: Dataloader y Auth](03-avanzado/60-absinthe-dataloader-auth/60-absinthe-dataloader-auth.md) | N+1 con Dataloader.Ecto, middleware pipeline, complexity analysis | Avanzado |
| 61 | [Nx: Numerical Elixir](03-avanzado/61-nx-numerical-elixir/61-nx-numerical-elixir.md) | tensors, Nx.Defn, broadcasting, batch processing, GPU backend | Avanzado |
| 62 | [Axon: Neural Networks](03-avanzado/62-axon-neural-networks/62-axon-neural-networks.md) | model building, training loop, transfer learning, gradient accumulation | Avanzado |
| 63 | [Nerves: IoT Embedded](03-avanzado/63-nerves-iot-embedded/63-nerves-iot-embedded.md) | Circuits.GPIO, Circuits.I2C, NervesHub OTA, firmware lifecycle | Avanzado |
| 64 | [Nerves: Networking y Cloud](03-avanzado/64-nerves-networking-cloud/64-nerves-networking-cloud.md) | VintageNet, MQTT/TLS, offline buffer, fleet management, heartbeat | Avanzado |
| 65 | [Commanded: Aggregates y Commands](03-avanzado/65-commanded-aggregates-commands/65-commanded-aggregates-commands.md) | event sourcing, aggregate, execute/2, apply/2, ProcessManager | Avanzado |
| 66 | [Commanded: Projections y Read Models](03-avanzado/66-commanded-projections-read-models/66-commanded-projections-read-models.md) | Ecto projector, event handler, snapshots, replay, LiveView sync | Avanzado |
| 67 | [Ash Framework: Resources](03-avanzado/67-ash-framework-resources/67-ash-framework-resources.md) | Domain, Resource, actions, validations, Ash.Query, relationships | Avanzado |
| 68 | [Ash: Extensions y API](03-avanzado/68-ash-extensions-api/68-ash-extensions-api.md) | AshJsonApi, AshGraphql, calculations, aggregates, AshAuthentication | Avanzado |
| 69 | [Broadway + Kafka Pipelines](03-avanzado/69-broadway-kafka-pipelines/69-broadway-kafka-pipelines.md) | BroadwayKafka, batchers, DLQ, rate limiting, idempotency, telemetry | Avanzado |
| 70 | [Req y Finch: HTTP Avanzado](03-avanzado/70-req-finch-http-clients/70-req-finch-http-clients.md) | Req.new, retry, custom steps, streaming, OAuth2, pool tuning | Avanzado |

---

## Nivel 04-Insane

| # | Ejercicio | Conceptos Principales | Dificultad |
|---|-----------|----------------------|-----------|
| 01 | [Build: Distributed Raft Consensus](04-insane/01-build-distributed-raft-consensus/01-build-distributed-raft-consensus.md) | Raft, leader election, log replication, committed entries | Insane |
| 02 | [Build: Distributed Transaction Coordinator](04-insane/02-build-distributed-transaction-coordinator/02-build-distributed-transaction-coordinator.md) | 2PC, 3PC, saga pattern, distributed atomicity | Insane |
| 03 | [Build: Distributed Cache Redis-like](04-insane/03-build-distributed-cache-redis-like/03-build-distributed-cache-redis-like.md) | RESP protocol, sharding, replication, eviction policies | Insane |
| 04 | [Build: Distributed Scheduler](04-insane/04-build-distributed-scheduler/04-build-distributed-scheduler.md) | global job ownership, leader-based scheduling, failover | Insane |
| 05 | [Build: Gossip Membership Protocol](04-insane/05-build-gossip-membership-protocol/05-build-gossip-membership-protocol.md) | SWIM protocol, suspicion, failure detection, membership list | Insane |
| 06 | [Build: Distributed Tracing System](04-insane/06-build-distributed-tracing-system/06-build-distributed-tracing-system.md) | trace context propagation, span collection, OpenTelemetry-like | Insane |
| 07 | [Build: Distributed Event Log](04-insane/07-build-distributed-event-log/07-build-distributed-event-log.md) | append-only log, segment files, replication, consumer offsets | Insane |
| 08 | [Build: Viewstamped Replication](04-insane/08-build-viewstamped-replication/08-build-viewstamped-replication.md) | VSR protocol, view changes, recovery, state transfer | Insane |
| 09 | [Build: Consistent Hashing Rebalancer](04-insane/09-build-consistent-hashing-rebalancer/09-build-consistent-hashing-rebalancer.md) | consistent hashing ring, virtual nodes, rebalancing, hot spots | Insane |
| 10 | [Build: Custom GenServer Supervisor](04-insane/10-build-custom-genserver-supervisor/10-build-custom-genserver-supervisor.md) | OTP internals, :proc_lib, :gen, sys protocol from scratch | Insane |
| 11 | [Build: Custom Event Bus Registry](04-insane/11-build-custom-event-bus-registry/11-build-custom-event-bus-registry.md) | wildcard subscriptions, priority routing, back-pressure, dead letters | Insane |
| 12 | [Build: Custom Process Pool](04-insane/12-build-custom-process-pool/12-build-custom-process-pool.md) | worker pool, checkout/checkin, overflow, queuing, monitoring | Insane |
| 13 | [Build: Actor Framework Alternative](04-insane/13-build-actor-framework-alternative/13-build-actor-framework-alternative.md) | typed messages, selective receive, actor lifecycle, supervision | Insane |
| 14 | [Build: Behaviour Callback Validator](04-insane/14-build-behaviour-callback-validator/14-build-behaviour-callback-validator.md) | compile-time checks, __before_compile__, Module.behaviour_info | Insane |
| 15 | [Build: LSM Tree Storage Engine](04-insane/15-build-lsm-tree-storage-engine/15-build-lsm-tree-storage-engine.md) | MemTable, SSTable, compaction, bloom filters, WAL | Insane |
| 16 | [Build: In-Memory Database](04-insane/16-build-in-memory-database/16-build-in-memory-database.md) | SQL-like query engine, indexes, transactions, MVCC | Insane |
| 17 | [Build: Graph Database](04-insane/17-build-graph-database/17-build-graph-database.md) | property graph model, Cypher-like queries, traversal engine | Insane |
| 18 | [Build: Time Series Database](04-insane/18-build-time-series-database/18-build-time-series-database.md) | time-partitioned storage, downsampling, retention policies, queries | Insane |
| 19 | [Build: Search Engine Inverted Index](04-insane/19-build-search-engine-inverted-index/19-build-search-engine-inverted-index.md) | tokenizer, TF-IDF, posting lists, BM25, query parser | Insane |
| 20 | [Build: CRDT Data Structures](04-insane/20-build-crdt-data-structures/20-build-crdt-data-structures.md) | G-Counter, PN-Counter, OR-Set, LWW-Register, convergence | Insane |
| 21 | [Build: Message Broker AMQP-like](04-insane/21-build-message-broker-amqp-like/21-build-message-broker-amqp-like.md) | exchanges, queues, bindings, routing keys, acknowledgments, DLQ | Insane |
| 22 | [Build: Stream Processor Flink-like](04-insane/22-build-stream-processor-flink-like/22-build-stream-processor-flink-like.md) | windowing, watermarks, stateful operators, checkpointing | Insane |
| 23 | [Build: Job Queue with Retry](04-insane/23-build-job-queue-with-retry/23-build-job-queue-with-retry.md) | exponential backoff, dead letter queue, priority queues, persistence | Insane |
| 24 | [Build: Distributed Rate Limiter](04-insane/24-build-distributed-rate-limiter/24-build-distributed-rate-limiter.md) | token bucket distribuido, Redis-like sync, sliding window | Insane |
| 25 | [Build: Lisp Interpreter](04-insane/25-build-lisp-interpreter/25-build-lisp-interpreter.md) | lexer, parser, eval, environment, closures, tail call | Insane |
| 26 | [Build: Mini Language Compiler](04-insane/26-build-mini-language-compiler/26-build-mini-language-compiler.md) | AST, type checker, code gen, BEAM bytecode output | Insane |
| 27 | [Build: Regex Engine](04-insane/27-build-regex-engine/27-build-regex-engine.md) | NFA construction, Thompson's algorithm, capture groups | Insane |
| 28 | [Build: Log Aggregation System](04-insane/28-build-log-aggregation-system/28-build-log-aggregation-system.md) | log ingestion, structured parsing, indexing, query API | Insane |
| 29 | [Build: Metrics Collector Prometheus-like](04-insane/29-build-metrics-collector-prometheus-like/29-build-metrics-collector-prometheus-like.md) | counters, gauges, histograms, scraping, /metrics endpoint | Insane |
| 30 | [Build: Profiler Performance Analyzer](04-insane/30-build-profiler-performance-analyzer/30-build-profiler-performance-analyzer.md) | :eprof, :fprof, :cprof, flame graph output, recon_trace | Insane |
| 31 | [Build: Test Framework](04-insane/31-build-test-framework/31-build-test-framework.md) | test DSL macros, assertions, async runner, reporter, ExUnit-like | Insane |
| 32 | [Build: Web Framework Phoenix-like](04-insane/32-build-web-framework-phoenix-like/32-build-web-framework-phoenix-like.md) | Plug pipeline, router DSL, controller, view, conn struct | Insane |
| 33 | [Build: REST API Framework](04-insane/33-build-rest-api-framework/33-build-rest-api-framework.md) | resource routing, content negotiation, serialization, middleware | Insane |
| 34 | [Build: API Gateway](04-insane/34-build-api-gateway/34-build-api-gateway.md) | reverse proxy, auth, rate limit, circuit breaker, routing rules | Insane |
| 35 | [Build: Load Balancer](04-insane/35-build-load-balancer/35-build-load-balancer.md) | round-robin, least-connections, health checks, sticky sessions | Insane |
| 36 | [Build: Video Streaming Server](04-insane/36-build-video-streaming-server/36-build-video-streaming-server.md) | HLS segmentation, chunked transfer, range requests, transcoding | Insane |
| 37 | [Build: Blockchain Simulation](04-insane/37-build-blockchain-simulation/37-build-blockchain-simulation.md) | PoW, chain validation, merkle tree, transactions, p2p sync | Insane |
| 38 | [Build: P2P File Sharing](04-insane/38-build-p2p-file-sharing/38-build-p2p-file-sharing.md) | DHT, piece management, peer exchange, NAT traversal basics | Insane |
| 39 | [Build: Service Mesh Proxy](04-insane/39-build-service-mesh-proxy/39-build-service-mesh-proxy.md) | sidecar pattern, mTLS, traffic shaping, observability injection | Insane |
| 40 | [Build: WASM Interpreter](04-insane/40-build-wasm-interpreter/40-build-wasm-interpreter.md) | WASM binary format, stack machine, validation, memory model | Insane |
| 41 | [Build: Event Sourcing CQRS Framework](04-insane/41-build-event-sourcing-cqrs-framework/41-build-event-sourcing-cqrs-framework.md) | aggregate, event store, projections, snapshots, command bus | Insane |
| 42 | [Build: Custom Macro DSL System](04-insane/42-build-custom-macro-dsl-system/42-build-custom-macro-dsl-system.md) | multi-level macros, compile-time validation, custom syntax, codegen | Insane |
| 43 | [Build: AI Inference Engine](04-insane/43-build-ai-inference-engine/43-build-ai-inference-engine.md) | matrix ops en Elixir/Nx, ONNX runtime via NIF, inference pipeline | Insane |
| 44 | [Build: Full Stack Distributed System](04-insane/44-build-full-stack-distributed-system/44-build-full-stack-distributed-system.md) | Phoenix + LiveView + Oban + Horde + Telemetry + multi-node | Insane |
| 45 | [Build: Game Engine ECS](04-insane/45-build-game-engine-ecs/45-build-game-engine-ecs.md) | Entity-Component-System, systems loop, process per entity, rendering | Insane |
| 46 | [Build: LiveView Clone](04-insane/46-build-liveview-clone/46-build-liveview-clone.md) | WebSocket lifecycle, DOM diffing, compile-time template DSL, 10k connections | Insane |
| 47 | [Build: Workflow Engine (Temporal-like)](04-insane/47-build-workflow-engine-temporal-like/47-build-workflow-engine-temporal-like.md) | durable execution, event sourcing, deterministic replay, workflow versioning | Insane |
| 48 | [Build: Compile-time Type System](04-insane/48-build-compile-time-type-system/48-build-compile-time-type-system.md) | macro-based type inference, union types, generics, Dialyxir integration | Insane |
| 49 | [Build: Reactive Streams](04-insane/49-build-reactive-streams/49-build-reactive-streams.md) | Reactive Streams spec, backpressure, hot/cold streams, schedulers, TCK | Insane |
| 50 | [Build: Distributed Lock Service](04-insane/50-build-distributed-lock-service/50-build-distributed-lock-service.md) | fencing tokens, epoch numbers, split-brain detection, lease renewal | Insane |
| 51 | [Build: Production Job Queue (Oban-like)](04-insane/51-build-production-job-queue/51-build-production-job-queue.md) | PostgreSQL-backed, multi-queue, cron, retry/backoff, uniqueness, LISTEN/NOTIFY | Insane |
| 52 | [Build: Multi-tenant SaaS Framework](04-insane/52-build-multi-tenant-saas-framework/52-build-multi-tenant-saas-framework.md) | schema-per-tenant, row-level security, tenant routing, billing, isolation | Insane |
| 53 | [Build: Real-time Collaboration Engine](04-insane/53-build-real-time-collaboration-engine/53-build-real-time-collaboration-engine.md) | OT/CRDT for text, presence, cursor sync, conflict resolution, offline | Insane |
| 54 | [Build: AI Agents Framework](04-insane/54-build-ai-agents-framework/54-build-ai-agents-framework.md) | ReAct pattern, tool system, memory (short/long-term), multi-agent coordination | Insane |
| 55 | [Build: Distributed Saga Orchestrator](04-insane/55-build-distributed-saga-orchestrator/55-build-distributed-saga-orchestrator.md) | saga definition DSL, compensation, idempotency, parallel steps, recovery | Insane |

---

## Herramientas y Ecosystem

| Herramienta | Descripción | Uso Principal |
|-------------|-------------|---------------|
| `mix` | Build tool, task runner, dependency manager | Todo: new, deps, test, release |
| `iex` | Interactive Elixir shell, REPL con autocompletado | Exploración, debugging, iex -S mix |
| `hex.pm` | Package manager de Elixir/Erlang | Dependencias públicas y privadas |
| `ExDoc` | Generador de documentación HTML | mix docs |
| `Dialyxir` | Wrapper de Dialyzer, análisis de tipos estático | mix dialyzer, @spec checking |
| `Credo` | Linter y code quality analyzer | mix credo --strict |
| `Sobelow` | Security-focused static analysis para Phoenix | mix sobelow |
| `:observer` | GUI para monitoreo de procesos y memoria BEAM | :observer.start() en iex |
| `Livebook` | Jupyter-like notebooks interactivos para Elixir | Exploración, visualización, enseñanza |
| `Benchee` | Benchmarking con estadísticas y formatters | mix run bench/my_bench.exs |
| `Recon` | Producción-safe diagnostics para BEAM | Process, memory, tracing en prod |
| `ExUnit` | Test framework incluido en Elixir | mix test |
| `Mox` | Mock library basada en behaviours | Tests con dependencias externas |
| `PropCheck` / `StreamData` | Property-based testing | Tests generativos |
| `Telemetry` | Observability estándar del ecosistema | Métricas, spans, eventos |

---

## Recursos de Aprendizaje

**Libros**
- *Programming Elixir* — Dave Thomas (pragprog.com): el libro de referencia para aprender Elixir
- *Elixir in Action* — Saša Jurić: OTP profundo, producción-ready, muy recomendado
- *Designing Elixir Systems with OTP* — James Gray & Bruce Tate: design patterns con OTP
- *Programming Phoenix* — Chris McCord et al.: framework web de Elixir
- *Metaprogramming Elixir* — Chris McCord: macros y AST

**Documentación Oficial**
- [hexdocs.pm/elixir](https://hexdocs.pm/elixir): stdlib completa con ejemplos
- [hexdocs.pm/mix](https://hexdocs.pm/mix): build tool
- [hexdocs.pm/ex_unit](https://hexdocs.pm/ex_unit): testing framework
- [erlang.org/doc](https://www.erlang.org/doc): documentación OTP/BEAM (siempre útil)

**Cursos y Plataformas**
- [exercism.org/tracks/elixir](https://exercism.org/tracks/elixir): ejercicios curados con mentores
- [elixirschool.com](https://elixirschool.com): curriculum gratuito en múltiples idiomas
- [grox.io](https://grox.io): cursos en video de alta calidad

**Comunidad**
- [elixirforum.com](https://elixirforum.com): foro oficial, muy activo y útil
- [Slack Elixir](https://elixir-slackin.herokuapp.com): canal de Slack de la comunidad
- [elixir-lang.org](https://elixir-lang.org): sitio oficial con guías Getting Started

---

## Cómo Usar Este Curriculum

1. **Empieza siempre por 01-Basico**, incluso si tienes experiencia en otros lenguajes. La semántica de Elixir (immutability total, pattern matching como base, procesos como unidad de concurrencia) requiere reentrenar intuiciones.

2. **No te saltes OTP**. Los ejercicios 04-06 del nivel Intermedio (GenServer, Supervisor, Application) son el corazón de Elixir. Todo lo demás construye sobre eso.

3. **Usa `iex -S mix` mientras trabajas**. La retroalimentación inmediata del REPL es parte del flujo de trabajo, no un accesorio.

4. **Lee los errores completos**. El compilador y runtime de Elixir tienen mensajes de error excepcionalmente claros. Léelos antes de buscar ayuda.

5. **Para los ejercicios Insane**: no hay solución de referencia. Investiga, diseña, itera. La especificación es el contrato; la implementación es tu decisión.




```
1. Proyecto independiente por ejercicio con explicación corta de qué trata
  2. Estructura de proyecto al inicio (árbol de directorios completo)
  3. Código 100% resuelto, sin TODO, sin HINT
  4. Tests completos — solo los más necesarios
  5. Inglés en todo (variables, docs, prosa)
  6. Sin metadata (sin estrellas, sin tiempo estimado)
  7. Sin REPL standalone — todo dentro de proyecto Mix
  8. Trade-offs y errores comunes de producción
  9. Autocontenido — sin referencias a otros ejercicios
  10. Buenas prácticas con ejemplos reales enfocados en senior
      
      
      
      * solo ingles
⏺ 1. Proyecto independiente por ejercicio con explicación corta de qué trata
  2. Estructura de proyecto al inicio (árbol de directorios completo)
  3. Código 100% resuelto, sin TODO, sin HINT
  4. Tests completos — solo los más necesarios
  5. Inglés en todo (variables, docs, prosa)
  6. Sin metadata (sin estrellas, sin tiempo estimado)
  7. Sin REPL standalone — todo dentro de un proyecto real
  8. Trade-offs y errores comunes de producción
  9. Autocontenido — sin referencias a otros ejercicios
  10. Buenas prácticas con ejemplos reales enfocados en senior
  11. "Why X and not Y" — explicar alternativas antes de implementar
  12. "Why this works" — justificación después del código
  13. Secciones educativas extra — profundización en conceptos clave del tema
  14. Steps numerados — implementación paso a paso
  15. Dependencias explícitas — archivo de configuración del proyecto copiable
  16. Reflection question — pregunta de pensamiento crítico
  17. Benchmark con resultado esperado — código de medición + target concreto
  18. Diagramas ASCII — flujo de datos o arquitectura
  19. Tests organizados por escenario — agrupados temáticamente
  20. Design decisions inline — "Option A vs Option B" con pros/contras      
```