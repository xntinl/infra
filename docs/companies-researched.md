# Empresas Investigadas — Stack Tecnológico

**Total: 110 empresas** distribuidas en 11 grupos temáticos.
Cada entrada incluye lenguajes, herramientas OSS creadas, infraestructura adoptada, decisiones notables y escala cuando está documentada públicamente.

---

## Grupo 1 — Big Tech

### 1. Google
- **Lenguajes:** C++, Java, Python, Go
- **OSS creado:** Kubernetes, Bazel (build system), gRPC, Envoy, Istio, Abseil (C++), OpenTelemetry (fusión de OpenCensus + OpenTracing)
- **Infra interna:** Borg (precursor de k8s), Spanner (NewSQL global), Zanzibar (authz, inspiró SpiceDB/OpenFGA), BorgMon (inspiró Prometheus)
- **Decisión notable:** Donó Kubernetes a la CNCF en 2016; deprecó OpenCensus fusionándolo en OpenTelemetry
- **Escala:** Busca global, YouTube, Gmail, Maps

### 2. Meta / Facebook
- **Lenguajes:** C++, Python, Hack/PHP, Java, Rust (herramientas)
- **OSS creado:** Apache Cassandra, RocksDB, Apache Thrift, Presto/Trino, PyTorch, React, Velox (motor vectorizado C++, 3–10x más eficiente), Buck2 (build en Rust), osquery, Infer (análisis estático)
- **Infra adoptada:** Apache Spark, Kubernetes
- **Decisión notable:** Velox como motor de ejecución compartido entre Presto, Spark y PyTorch; Buck2 reescrito en Rust
- **Escala:** 300 PB+ data warehouse, 3B+ usuarios

### 3. Netflix
- **Lenguajes:** Java, Kotlin, Python, Node.js
- **OSS creado:** Chaos Monkey/Simian Army, Spinnaker (CD multi-cloud), Conductor (workflow), Atlas (TSDB interno), Titus (contenedores, 3M containers/semana), Zuul, Eureka, Hystrix (deprecated)
- **Infra adoptada:** AWS, Kafka, gRPC
- **Decisión notable:** Hystrix/Ribbon/Eureka → maintenance mode ~2018; service meshes reemplazaron resiliencia client-side
- **Escala:** 3M containers/semana via Titus; 300M+ suscriptores

### 4. Uber
- **Lenguajes:** Go, Python, Java
- **OSS creado:** Jaeger (tracing, CNCF graduated), M3DB (6.6B series, 500M métricas/seg), Cadence (workflow, 12B ejecuciones/mes), Peloton (scheduler)
- **Infra adoptada:** Apache Kafka, Apache Spark, Presto
- **Decisión notable:** Los creadores de Cadence salieron de Uber y fundaron Temporal (fork comercial)
- **Escala:** 1,000+ microservicios, millones de viajes/día

### 5. Airbnb
- **Lenguajes:** Ruby, Python, Java
- **OSS creado:** Apache Airflow (data pipelines, 2014→Apache), Apache Superset (BI), Minerva (plataforma de métricas, 12k métricas+4k dimensiones), Lottie (animaciones), Chronon (ML feature platform, OSS abril 2024)
- **Infra adoptada:** Presto/Trino, Apache Spark, Apache Druid
- **Decisión notable:** Airflow y Superset donados a Apache; ambos se convirtieron en estándares de la industria
- **Escala:** Millones de listados, operación global

### 6. Twitter / X
- **Lenguajes:** Scala, Java, Python
- **OSS creado:** Finagle (RPC async JVM), Scalding (MapReduce en Scala), DistributedLog (log replicado sobre BookKeeper), Pants (build tool)
- **Infra adoptada:** Apache Kafka, Hadoop/HDFS
- **Decisión notable:** Twitter migró de Pants a Bazel; post-adquisición X aceleró migración fuera del stack JVM/Scala
- **Escala:** Cientos de millones de tweets/día

### 7. Spotify
- **Lenguajes:** Java, Python, Scala, Go
- **OSS creado:** Backstage (developer portal, 14k+ componentes, 2,700+ ingenieros, CNCF), Luigi (pipelines Python), Scio (Scala sobre Apache Beam)
- **Infra adoptada:** Apache Beam/Dataflow, Kubernetes, Kafka
- **Decisión notable:** Heroic (TSDB propio) → VictoriaMetrics por desalineación con Prometheus/OTel/Grafana; Luigi → Beam/Scio
- **Escala:** 600M+ usuarios, streams globales

### 8. Microsoft
- **Lenguajes:** C++, C#, TypeScript, Python
- **OSS creado:** TypeScript (5.x, migración nativa en progreso), VS Code, Playwright (e2e testing), Dapr (runtime distribuido, CNCF), ONNX Runtime, Fluid Framework (collaborative data structures)
- **Infra adoptada:** Kubernetes/AKS, Azure
- **Decisión notable:** TypeScript se convirtió en estándar de facto para frontend/backend; Dapr adoptado por 37% de sus usuarios en producción
- **Escala:** Azure: segunda nube pública, GitHub, Office 365, Xbox

### 9. Apple
- **Lenguajes:** Swift, C++, Objective-C
- **OSS creado:** Swift (server-side creciente), FoundationDB (KV transaccional con ACID fuerte, adquirido y open sourced 2018, powers iCloud), Pkl (config language con validación tipada, OSS feb 2024)
- **Infra adoptada:** LLVM/Clang (sponsor principal), WebKit
- **Decisión notable:** Pkl como alternativa tipada a YAML/JSON para config; FoundationDB potencia iCloud
- **Escala:** iCloud: 1B+ dispositivos Apple

### 10. Amazon / AWS
- **Lenguajes:** Java, Rust, Python, C++
- **OSS creado:** Firecracker (microVM Rust, <125ms boot, powers Lambda+Fargate), Bottlerocket (OS para contenedores en Rust), Cedar (policy language, CNCF sandbox), OpenSearch (fork de Elasticsearch), Smithy (IDL para APIs AWS), AWS CDK
- **Infra adoptada:** Kubernetes/EKS, Rust para sistemas de bajo nivel
- **Decisión notable:** Rust adoptado estratégicamente para infra de sistemas (Firecracker, Bottlerocket); Cedar usa razonamiento automatizado para verificación de políticas
- **Escala:** AWS: mayor nube pública; Lambda procesa billones de requests

---

## Grupo 2 — Cloud & Infraestructura

### 11. Cloudflare
- **Lenguajes:** Rust (networking), C/C++ (legacy), Go (Quicksilver), JavaScript/TypeScript (Workers)
- **OSS creado:** Pingora (proxy HTTP en Rust, ~1 quadrillón de requests), Oxy (framework proxy Rust sobre Pingora), workerd (runtime Workers, V8+C++), Quicksilver (KV distribuido en Go, 2.5T reads/día, 30M writes/día)
- **Productos edge:** R2 (object storage sin egress fees), D1 (SQLite serverless en edge)
- **Decisión notable:** Todo networking nuevo en Rust por memory safety; V8 isolates (no contenedores) para Workers elimina cold starts
- **Escala:** 330+ PoPs globales, millones de dominios, red que maneja fracción significativa del internet

### 12. HashiCorp
- **Lenguajes:** Go (todos los productos)
- **OSS creado:** Vault (Go, 35.2k★, secrets + PKI dinámico), Consul (Go, 29.8k★, service discovery + mTLS), Nomad (Go, 16.3k★, orquestador multi-workload validado a 10k+ nodos), Boundary (Go, acceso zero-trust), Terraform, Waypoint
- **Decisión notable:** Go elegido por binary único multiplataforma; Vault Integrated Storage (Raft) elimina dependencia de Consul para HA
- **Escala:** Productos usados por decenas de miles de organizaciones

### 13. Datadog
- **Lenguajes:** Go (Agent 71.6%), C (eBPF 20.8%), Python (4.2%), Rust (1%, Vector)
- **OSS creado:** Datadog Agent (Go+eBPF), Vector (Rust, adquirido de Timber.io — 86 MiB/s vs Logstash 3.1 MiB/s, 500 TB/día mayor usuario)
- **Infra:** eBPF para NPM/USM (detecta HTTP/gRPC inspeccionando syscalls sin reinicios ni agentes en app)
- **Decisión notable:** eBPF como plataforma estratégica para observabilidad zero-instrumentation; Rust para Vector por throughput crítico
- **Escala:** Miles de clientes enterprise, 100k descargas/día de Vector

### 14. Grafana Labs
- **Lenguajes:** Go (todos los productos: 91–96%)
- **OSS creado:** Loki (logs, Go, 27.8k★), Tempo (traces Go, solo object storage), Mimir (métricas Go, validado a 1B series), Beyla (eBPF+OTel, auto-instrumentation sin código), Pyroscope (profiling continuo)
- **Arquitectura:** Stack LGTM; todos usan S3/GCS/Azure Blob como única capa de persistencia — no stateful infrastructure propia
- **Decisión notable:** Object storage como única persistencia es el principio central; Beyla donado a CNCF como "OpenTelemetry eBPF Instrumentation"
- **Escala:** Mimir validado a 1B series activas en testing interno

### 15. Elastic
- **Lenguajes:** Java (Elasticsearch 99.4%), Go (Beats 93.2%)
- **OSS creado:** Elasticsearch (76.3k★), Elastic Beats (shippers ligeros Go: Filebeat, Metricbeat, Packetbeat), Elastic Agent (reemplaza múltiples Beats), ECS (fusionado con OTel Semantic Conventions 2023)
- **Decisión notable:** ECS fusionado con OpenTelemetry Semantic Conventions (2023) — movimiento estratégico para estandarizar campos de observabilidad; Beats en Go por tamaño binario mínimo
- **Escala:** 76k★ en GitHub, deployado en millones de instancias

### 16. MongoDB
- **Lenguajes:** C++ (69.1% core), JavaScript (Mongo Shell)
- **OSS creado:** MongoDB, Change Streams (CDC real-time sobre oplog), Realm Core (motor embedded móvil C++)
- **Productos:** Atlas Vector Search (RAG sin vector DB separado), Atlas (multi-cloud AWS/GCP/Azure)
- **Decisión notable:** Change Streams implementadas sobre oplog con resumability tokens (at-least-once garantizado); Atlas multi-cloud por diseño
- **Escala:** Atlas: miles de clientes, multi-región

### 17. Redis Labs
- **Lenguajes:** C (core Redis), Rust (RedisJSON 45.1%, RediSearch 23.2%), Python (RediSearch 27.6%)
- **OSS creado:** Redis (C), RediSearch (búsqueda full-text + vector KNN, "billions of documents across hundreds of servers"), RedisJSON (Rust, JSON nativo)
- **Decisión notable:** Redis 8 integra RediSearch y RedisJSON directamente en el binario principal eliminando módulos separados; core de RedisJSON reescrito en Rust por memory safety en tree manipulation
- **Escala:** Sub-millisecond latency; usado como cache primario en prácticamente toda la industria

### 18. Confluent
- **Lenguajes:** Java/Scala (Kafka, ksqlDB, Schema Registry)
- **OSS creado:** Apache Kafka (origen LinkedIn, stewardship Confluent), ksqlDB (SQL sobre Kafka), Schema Registry (versionado de schemas Avro/JSON/Protobuf)
- **Productos:** Confluent Cloud for Apache Flink (serverless, tablas Kafka como tablas Flink automáticamente)
- **Decisión notable:** Escrituras secuenciales en disco (no RAM random) son la insight fundacional de Kafka — I/O secuencial en discos HDD supera random-access en memoria a escala; zero-copy via `sendfile()`
- **Escala:** 80%+ Fortune 100, Criteo 30M msgs/seg, PayPal 100B msgs/día, Agoda trillones de eventos/día

### 19. Databricks
- **Lenguajes:** Scala/Java (Spark, Delta Lake), Python (MLflow, PySpark)
- **OSS creado:** Delta Lake (ACID sobre object storage S3/GCS), MLflow (ML lifecycle, 24.6k★), Unity Catalog (governance multi-formato, LF AI&Data Foundation), Apache Spark (co-mantenido)
- **Arquitectura Lakehouse:** ACID en object storage barato = confiabilidad de data warehouse sin lock-in propietario; transaction log son archivos JSON en mismo bucket, sin metadata service separado
- **Decisión notable:** Unity Catalog compatible con Iceberg REST API — Snowflake, Trino, DuckDB pueden consultar el mismo catálogo
- **Escala:** 9,400+ clientes Snowflake; Spark: motor batch/streaming dominante en data engineering

### 20. PlanetScale
- **Lenguajes:** Go (Vitess 94.5%)
- **OSS creado:** Vitess (Go, 20.8k★, origen YouTube 2011 — VTGate proxy + VTTablet; multiplexing miles de conexiones app → pool pequeño MySQL; online DDL via gh-ost sin locks), Neki (Postgres sharding desde cero)
- **Database branching:** Schema changes como PRs con non-blocking DDL — ningún otro MySQL managed lo hace
- **Decisión notable:** Online DDL que sobrevive reinicios de proceso (progreso en tablas transaccionales auxiliares); construyeron Neki "from first principles" sin fork de Citus
- **Escala:** Vitess: YouTube (2011), PlanetScale, Shopify, Block/Square, Slack, JD.com

---

## Grupo 3 — Enterprise & Fintech Tradicional

### 21. Stripe
- **Lenguajes:** Ruby (15M+ líneas), Go (servicios PCI), TypeScript (migración 3.7M líneas JS→TS en un PR 2022)
- **OSS creado:** Sorbet (type checker Ruby, 2019), Veneur (Go, agregación de métricas distribuida pre-enviando a Datadog — reduce costos a escala), Markdoc (framework docs con sistema de tipos sobre Markdown)
- **Infra adoptada:** Bazel (builds unificados Go/Java/TS/Ruby/Python/Scala/Terraform), Envoy (service mesh), Firecracker (sandboxing builds, reemplazó gVisor)
- **Decisión notable:** gVisor → Firecracker por overhead inaceptable en filesystem para compilación Ruby/Java; JS (Flow types) → TypeScript en un único PR
- **Escala:** Procesa cientos de miles de millones en pagos/año

### 22. Shopify
- **Lenguajes:** Ruby on Rails (2.8M líneas, "herramienta de 100 años"), Rust (YJIT)
- **OSS creado:** YJIT (JIT compiler para Ruby en Rust, integrado en CRuby 3.1 — 15% mejora en producción), Packwerk (análisis estático de límites entre módulos Rails)
- **Infra adoptada:** Vitess (MySQL sharding para Shop app), GKE/Kubernetes, Redis/Memcached por pod (tras incidente "Redismageddon")
- **Decisión notable:** Monolito modular Rails ~37 componentes vía Engines+Packwerk+Sorbet en lugar de microservicios; YJIT contribuido upstream a Ruby
- **Escala:** Procesa ~10% del e-commerce en USA

### 23. Square / Block
- **Lenguajes:** Go (microservicios backend), Kotlin/Java (Android + server), Rust (LDK)
- **OSS creado:** LeakCanary (detección memory leaks Android, tool más usado para esto), SQLDelight (genera Kotlin type-safe APIs desde SQL, Kotlin Multiplatform), LDK (Lightning Development Kit Bitcoin en Rust), co-creador de gRPC
- **Infra adoptada:** gRPC, Kubernetes
- **Decisión notable:** Rust para código safety-critical de Bitcoin/Lightning; LDK como capa de abstracción para Lightning nodes
- **Escala:** Millones de merchants, procesamiento de pagos global

### 24. PayPal
- **Lenguajes:** Java (legacy), Node.js (migración landmark 2013 — 2x más rápido, 33% menos código), TypeScript
- **OSS creado:** Kraken.js (framework Node.js enterprise sobre Express), Nemo.js (Selenium wrapper para Node.js)
- **Infra adoptada:** Apollo GraphQL (50+ productos en unified graph), Cosmos.AI (plataforma ML interna con Spark + k8s batch + GPU serving)
- **Decisión notable:** Java→Node.js (2013) se convirtió en el caso de estudio canónico de Node.js en enterprise; GraphQL federation: Apollo Studio con field-level instrumentation
- **Escala:** 400M+ cuentas activas, miles de millones de transacciones/año

### 25. Bloomberg
- **Lenguajes:** C++ (BDE — todas las librerías internas), Python (team tiene devs de CPython core), JavaScript (ingenieros en TC39)
- **OSS creado:** BDE/BSL (Bloomberg C++ Standard Library, namespace `bsl::`), Comdb2 (RDBMS distribuido propio con concurrencia optimista, 2004→OSS), Memray (profiler Python con core C++, 7k★ en días de lanzamiento), PyStack (inspector de stacks Python para debug), KServe (k8s ML serving, co-desarrollado, donado a LF AI&Data)
- **Decisión notable:** Open source first — Bloomberg requiere participación en upstreams, no solo consumo; sponsors CPython
- **Escala:** Terminal Bloomberg: decenas de miles de traders globales

### 26. Goldman Sachs
- **Lenguajes:** Java, PURE (lenguaje funcional interno→OSS), Python
- **OSS creado:** Legend/PURE (plataforma de modelado y gobierno de datos, OSS via FINOS octubre 2020 — 5 módulos: Studio, Engine, SDLC, Shared, PURE language); piloto con Deutsche Bank, Morgan Stanley, RBC
- **Infra adoptada:** Databricks Lakehouse (integración con Legend), FINOS (miembro platinum)
- **Decisión notable:** Convertir tooling interno en estándar cross-industry via FINOS — interoperabilidad entre bancos como objetivo arquitectónico
- **Escala:** Billones en activos gestionados, trading global

### 27. JPMorgan Chase
- **Lenguajes:** Python (Athena: 35M líneas, plataforma de trading cross-asset), Java (Quartz), Go (kallisti), TypeScript (Perspective)
- **OSS creado:** Perspective (visualización WebAssembly interactiva, FINOS), salt-ds (React UI accesible), kallisti (chaos engineering Go), Jupyter extensions (jupyter-fs, nbcelltests), bt (backtesting Python)
- **Infra adoptada:** FINOS (miembro platinum), CDM (Common Domain Model) — primer banco US en implementarlo para reporting regulatorio (2023)
- **Decisión notable:** Python 2→3 en Athena: fallo documentado públicamente por escala (35M líneas) — caso de advertencia de la industria
- **Escala:** 1ª institución financiera mundial por activos (~$4T)

### 28. Revolut
- **Lenguajes:** Kotlin (backend, Ktor + coroutines), Java 21
- **OSS creado:** Ninguno significativo (Trino adoptado no creado)
- **Infra adoptada:** GKE, Trino (SQL analítico), PostgreSQL (base primaria OLTP via jOOQ), Redis, Flyway (migraciones), Kafka (rechazado → PostgreSQL como event store)
- **Decisión notable:** Rechazó Kafka conscientemente — construyó event streaming sobre PostgreSQL (SQL-queryable, más simple de auditar para fintech); CQRS + Clean Architecture + DDD
- **Escala:** 50M+ clientes, presencia en 35+ países

### 29. N26
- **Lenguajes:** Kotlin (60%+ microservicios, migración desde Java), Java (resto)
- **OSS creado:** Ninguno significativo
- **Infra adoptada:** Kubernetes (80%+ de 230 microservicios), Spring Boot, GitHub Actions (CI), ArgoCD (CD GitOps)
- **Decisión notable:** Jenkins → GitHub Actions + ArgoCD: pipelines de 1.5h → algunos bajo 15min; migración 3 generaciones: orquestación propia → Nomad → Kubernetes; Java→Kotlin service-by-service
- **Escala:** 8M+ clientes, neobank europeo

### 30. SAP
- **Lenguajes:** ABAP (legacy S/4HANA), Java (CAP Spring Boot), Node.js (CAP runtime), TypeScript (Fiori UI)
- **OSS creado:** Kyma (runtime k8s para BTP: Istio + NATS JetStream + Prometheus/Grafana), CAP (Cloud Application Programming Model, open source core)
- **Infra adoptada:** Multi-cloud BTP sobre AWS/Azure/GCP; SAP HANA Cloud (DB primaria); NATS JetStream (eventing en Kyma)
- **Decisión notable:** Estrategia "Clean Core": ABAP on-premise → BTP ABAP Environment via abapGit; clientes migran código custom usando ATC + Quick Fixes
- **Escala:** 400M+ usuarios SAP, presencia en 180 países

---

## Grupo 4 — AI & Emerging Tech

### 31. Anthropic
- **Lenguajes:** Python, JAX (inference sobre TPUs)
- **OSS creado:** Model Context Protocol (MCP, nov 2024) — protocolo estándar para conectar agentes con herramientas y datos; adoptado masivamente en la industria
- **Infra:** Multi-cloud: AWS Trainium + Google TPUv7 Ironwood + NVIDIA GPU; compilador XLA para TPUs; compromiso ~$52B en 1M chips TPUv7
- **Decisión notable:** XLA/JAX sobre CUDA — inusual; mayoría de labs son CUDA-first. Postmortem de septiembre 2025 reveló bug de miscompilación en `top-k` XLA:TPU con aritmética mixta bf16/fp32
- **Escala:** Claude: millones de usuarios, API usada por miles de empresas

### 32. OpenAI
- **Lenguajes:** Python, C++ (Triton compiler), CUDA
- **OSS creado:** Triton GPU Compiler (DSL Python para kernels GPU sin CUDA, integrado en `torch.compile`, soporte NVIDIA Blackwell + AMD)
- **Infra:** Kubernetes a 7,500 nodos (un solo cluster — inusual, mayoría fragmentan en 1k-2k); EndpointSlices eliminó explosión N² en Endpoints watches (reducción 1000x de carga API server); gang scheduling via coscheduling plugin para jobs GPU
- **Decisión notable:** Cluster k8s unificado de 7,500 nodos para que research teams compartan recursos sin cambios de código; Flannel → Azure networking nativo a 200k+ IPs
- **Escala:** ChatGPT: 100M+ usuarios activos semanales; GPT-4 API: miles de empresas

### 33. Mistral
- **Lenguajes:** Python, C++ (kernels)
- **OSS creado:** mistral-inference (Python, referencia oficial), vllm-release (artifacts certificados para modelos Mistral)
- **Infra adoptada:** vLLM (upstream, no fork — colaboración activa), TensorRT-LLM, SGLang; arquitectura MoE (41B parámetros activos / 675B total)
- **Decisión notable:** Lean team → apoya ecosystem abierto (vLLM) en lugar de infra de serving propia; MoE requiere infrastructure consciente de sparse routing
- **Escala:** Mistral Large 3, Mistral Compute (cloud inference propio)

### 34. Hugging Face
- **Lenguajes:** Python (ML), Rust (componentes críticos de latencia)
- **OSS creado:** Safetensors (Rust core ~900 líneas, 13x más rápido que pickle, auditoría de seguridad, sin ejecución de código), TGI/Text Generation Inference (router Rust + inference Python; Paged Attention, Flash Attention), Gradio (UI demos ML), Datasets, Transformers
- **Infra:** ZeroGPU (H200 compartido con asignación dinámica por función `@spaces.GPU`), Spaces (Git-backed deployments), Inference Endpoints (autoscaling GPU/TPU)
- **Decisión notable:** TGI = Rust para scheduling/batching (evita GIL Python) + Python para inferencia ML; split principiado. Safetensors: de facto estándar en Hub
- **Escala:** Hub: 1M+ modelos públicos, millones de descargas/día

### 35. Scale AI
- **Lenguajes:** Python, Node.js
- **OSS creado:** Nucleus (plataforma de curación de datos ML — no OSS, pero documentada)
- **Infra adoptada:** Pipelines de anotación multi-modal a escala enterprise
- **Decisión notable:** Posición única: vendor de datos + plataforma de infra ML simultáneamente; primero en contratos DoD para datos de AI
- **Escala:** $1.5B ARR (2024), 97% YoY growth

### 36. Vercel
- **Lenguajes:** Rust (Turborepo, Turbopack), TypeScript (Edge Runtime, Next.js)
- **OSS creado:** Turbopack (bundler Rust sobre Turbo engine — memoización incremental), Turborepo (migración Go→Rust documentada en 3 posts; Zig como cross-compiler durante transición)
- **Infra:** Edge Runtime (V8, no Node.js), Fluid Compute (serverless persistente entre invocaciones — 85% reducción de costos compute reportada)
- **Decisión notable:** Go→Rust en Turborepo: CGO para FFI nativo más limpio en Rust, compartir código con Turbopack; Zig como bridge durante migración incremental — técnica rarísima documentada públicamente
- **Escala:** Next.js: framework React más usado; millones de deployments/mes

### 37. Neon
- **Lenguajes:** Rust (storage engine completo), C (PostgreSQL core, no modificado significativamente)
- **OSS creado:** neon (Pageserver en Rust — reconstruye páginas desde layer files; Safekeeper en Rust — consenso Paxos para WAL; separación compute/storage)
- **Arquitectura:** Compute = PostgreSQL stateless efímero en k8s pods; Pageserver = cache inteligente sobre S3; Safekeeper = quorum WAL; scale-to-zero instantáneo; branching copy-on-write
- **Decisión notable:** WAL como interfaz entre compute y storage → PostgreSQL casi sin parchear; PITR continuo sin backup schedules; storage engine completo en Rust — uno de los deployments Rust más significativos en infra de bases de datos
- **Escala:** Serverless PostgreSQL, millones de branches creados

### 38. Fly.io
- **Lenguajes:** Rust (fly-proxy), Go (LiteFS), C++ (Firecracker — AWS-built)
- **OSS creado:** LiteFS (FUSE-based SQLite replicado con Consul leader election, formato LTX), fly-proxy (proxy edge Rust en cada servidor)
- **Infra adoptada:** Firecracker microVMs (cada app en su propio microVM), WireGuard (backhaul inter-datacenter + 6PN IPv6 Private Networking entre apps)
- **Decisión notable:** WireGuard para todo — backhaul Y networking privado — en lugar de encapsulación propietaria; SQLite como base de datos primaria (advocacy inusual para plataforma de hosting); fly-proxy Rust: uno de los pocos edge proxies en producción escritos en Rust a esta escala
- **Escala:** Decenas de miles de aplicaciones deployadas globalmente

### 39. Railway
- **Lenguajes:** Go (Railpack, nuevo), Rust (Nixpacks, deprecado)
- **OSS creado:** Nixpacks (Rust, detecta y buildea automáticamente — usado en 14M+ aplicaciones, ahora deprecado), Railpack (Go, genera BuildKit LLB — imágenes 38% más pequeñas Node.js, 77% Python)
- **Infra adoptada:** BuildKit LLB (Low-Level Build format — control fine-grained de layer caching imposible con Dockerfile)
- **Decisión notable:** Rust→Go (migración inversa a la tendencia): ecosistema BuildKit y tooling de Go más adecuado; BuildKit LLB adoptado por casi nadie fuera de BuildKit mismo — Railway tiene control único sobre layer graph
- **Escala:** Millones de aplicaciones buildadas con Nixpacks/Railpack

---

## Grupo 5 — E-commerce & Marketplace

### 40. Zalando
- **Lenguajes:** Java (servicios backend), Python, Scala, Go (tooling)
- **OSS creado:** Nakadi (event bus HTTP sobre Kafka), Skipper (ingress controller Go + OPA integrado), Patroni (HA controller PostgreSQL Python — adoptado por GitLab.com, IBM Cloud), Connexion (OpenAPI framework Python), RESTful API Guidelines
- **Infra adoptada:** AWS, Kubernetes (140+ clusters, cada microservicio en cuenta AWS aislada), Apache Kafka via Nakadi
- **Decisión notable:** 200+ equipos de ingeniería, cada uno con su propia cuenta AWS — isolación máxima en lugar de plataforma centralizada
- **Escala:** 1,000+ developers, 200+ proyectos OSS, 10k deploys/día

### 41. Etsy
- **Lenguajes:** PHP (monolito LAMP core), Go (nuevos servicios), Python (ML/data)
- **OSS creado:** feature (PHP feature-flagging API — una de las primeras implementaciones públicas del patrón)
- **Infra adoptada:** MySQL, Memcached, Redis, Elasticsearch, Kubernetes (migración cloud), Prometheus, Terraform
- **Decisión notable:** Migración cloud de monolito LAMP a GCP/Kubernetes completada en ~9 meses (objetivo: 12); mantuvo monolito intacto, añadió autoscaling; extrae servicios específicos gradualmente
- **Escala:** 50–60 deploys/día a producción, miles de millones de pageviews/mes

### 42. eBay
- **Lenguajes:** Java (core), Kotlin, Node.js (frontend/BFF), Python (ML)
- **OSS creado:** NuRaft (C++, upgrade del algoritmo Raft para sistemas distribuidos), Accelerator (procesamiento datos reproducible a gran escala)
- **Infra adoptada:** Apache Kafka, Elasticsearch/OpenSearch (plataforma Pronto ES-AAS), Kubernetes + Envoy (reemplazó OpenStack ~2018), MongoDB, Apache Spark
- **Decisión notable:** OpenStack→Kubernetes+Docker (2018); gestión lifecycle de clusters Elasticsearch via Pronto interno
- **Escala:** 134M compradores activos (Q4 2024), 2.3B listados activos

### 43. Mercado Libre
- **Lenguajes:** Go (~50% del tráfico), Java (otro 50%), Node.js
- **OSS creado:** Fury (IDP/PaaS interno cloud-agnostic sobre AWS+GCP+Datadog — no público)
- **Infra adoptada:** Kubernetes (30,000+ microservicios), AWS + GCP multi-cloud, Amazon Bedrock, Datadog
- **Decisión notable:** "NoOps" — 30,000 microservicios gestionados por 16,000 developers sin ops centralizado (QCon SF 2024); Go como lenguaje primario para nuevos servicios
- **Escala:** 25M RPS, 10,000 deploys/día, 16,000 desarrolladores

### 44. Booking.com
- **Lenguajes:** Perl (monolito legacy frontend), Java (microservicios nuevos), Python, Go, Vue.js
- **OSS creado:** Ninguno significativo público
- **Infra adoptada:** MySQL, Apache Airflow (Google Cloud Composer para AdTech), Kafka, Kubernetes
- **Decisión notable:** API de 14 años extraída de monolito Perl → nuevo servicio Java via strangler-fig: 30% menos latencia, 77% payloads más pequeños, 50% menos CPU; micro frontends + DORA metrics → doubled delivery performance
- **Escala:** Una de las mayores plataformas de viajes, millones de alojamientos

### 45. Expedia
- **Lenguajes:** Kotlin (primario, GraphQL services), Java (legacy), TypeScript/React
- **OSS creado:** graphql-kotlin (librerías GraphQL en Kotlin con Spring Boot autoconfiguration)
- **Infra adoptada:** Apollo Federation, Kubernetes + Helm, gRPC, Datadog, Kafka, Spark
- **Decisión notable:** REST → GraphQL unified API gateway; micro frontend flights: 52% mejora en "page usable time" (InfoQ, 2024)
- **Escala:** Expedia, Hotels.com, Vrbo, etc. — marketplace global de viajes

### 46. Grab
- **Lenguajes:** Go (primario, 1,000+ microservicios), Rust (servicios de alto rendimiento), Kotlin/Java
- **OSS creado:** Contribuciones a Strimzi, Istio, Kafka ecosystems
- **Infra adoptada:** Apache Kafka en k8s via Strimzi, Istio (migrado desde Consul), AWS+GCP híbrido, ScyllaDB, gRPC
- **Decisión notable:** Go→Rust en Counter Service: misma latencia P99, ~70% reducción infra (4.5 cores vs 20 cores a 1,000 RPS); monorepo 214 GB Go dividido en repos separados — commits -99.9%, storage -59%, replicación Gitaly de minutos a milisegundos
- **Escala:** Super-app más grande SE Asia, 1,000+ microservicios

### 47. DoorDash
- **Lenguajes:** Kotlin (todos los servicios backend), Go (infra/tooling), Python (ML/data)
- **OSS creado:** Riviera (framework de feature engineering real-time sobre Flink — interno)
- **Infra adoptada:** Apache Kafka (backbone de eventos), Apache Flink (220 TB/día al data lake), Apache Iceberg (reemplazó Delta Lake como lakehouse sink, 2024), Apache Pinot (analytics real-time), Trino, Apache Spark, Airflow, Snowflake
- **Decisión notable:** Amazon SQS+Kinesis → Apache Kafka+Flink (2022, caso de estudio ampliamente citado); Delta Lake → Iceberg (2024)
- **Escala:** Flink procesa 220 TB/día; cientos de ingenieros en Kotlin

### 48. Instacart
- **Lenguajes:** Ruby on Rails (monolito marketplace), Go (microservicios nuevos), Python (ML/data)
- **OSS creado:** Griffin (MLOps platform interno — no público)
- **Infra adoptada:** Apache Kafka (capa de datos primaria), Debezium+Kafka (CDC a Snowflake sobre miles de tablas), Snowflake, Temporal (workflow orchestration — 20M+ workflows/día), Kubernetes, AWS
- **Decisión notable:** Airflow+cron jobs → Temporal para orquestación de workflows complejos; Debezium CDC replicando miles de tablas a Snowflake
- **Escala:** 20M+ workflows/día en Temporal (Data Council 2025)

### 49. Lyft
- **Lenguajes:** Python (ML, data), Go (microservicios críticos), C++ (Envoy)
- **OSS creado:** Envoy Proxy (C++, CNCF graduated — co-creado en Lyft, data plane estándar de service meshes), Flyte (ML/data workflow, LF AI&Data), Amundsen (data catalog, LF AI&Data)
- **Infra adoptada:** Kubernetes (300k+ containers, 600+ microservicios, multi-cluster AWS), Kafka, Spark, Flink, Kinesis, Snowflake, AWS SageMaker (offline training, migrado recientemente)
- **Decisión notable:** Envoy surgió de necesidad interna en Lyft — ahora es el data plane de Istio, AWS App Mesh, etc.; ML platform rearquitectada (Dic 2025): SageMaker para training offline, k8s retenido para serving online
- **Escala:** 300,000+ containers en k8s multi-cluster, 600+ microservicios

---

## Grupo 6 — Gaming & Entertainment

### 50. Epic Games
- **Lenguajes:** C++ (Unreal Engine, game servers), Java/Scala (backend microservicios), C# (tooling), Verse (nuevo scripting UEFN)
- **OSS creado:** UnrealEngine (source en GitHub, requiere cuenta Epic), EOS SDK samples
- **Infra adoptada:** AWS (EKS para fleet de Fortnite — autoscala hasta 30x en picos), Kafka+Flink+S3+Redshift (analytics pipeline), EOS (crossplatform matchmaking, voice, anti-cheat)
- **Decisión notable:** Kubernetes autoscaling en tiempo real para game servers de Fortnite; Verse (2023) — lenguaje propio inspirado en Haskell para UEFN reemplazando Blueprint scripting
- **Escala:** 350M+ usuarios registrados, 15.3M concurrentes peak (concierto Travis Scott 2020)

### 51. Riot Games
- **Lenguajes:** Java (backend — LoL Loot Service, etc.), Go (tooling operacional), Python, C++ (clientes/servidores de juego)
- **OSS creado:** cloud-inquisitor (Python, enforcement de ownership de recursos AWS), vault-go-client (Go, cliente HashiCorp Vault), LoRDeckCodes
- **Infra adoptada:** AWS (migración completa desde 14 data centers), 246 EKS clusters vía Terraform+Karpenter (ahorro ~$10M/año), AWS Local Zones (SLA <35ms latencia para Valorant), HashiCorp Vault
- **Decisión notable:** Riot construyó rCluster (Docker+SDN orquestador propio) antes de migrar a EKS — uno de los primeros grandes gaming migrations a contenedores
- **Escala:** 150M+ jugadores registrados, SLA <35ms latencia para Valorant globalmente

### 52. Roblox
- **Lenguajes:** C++ (engine), Luau (scripting de juegos — fork tipado de Lua, OSS 2021), Go, Python (backend)
- **OSS creado:** Luau (VM rápida, sistema de tipos gradual, abierto 2021 — usado más allá de Roblox)
- **Infra adoptada:** HashiCorp Nomad (orquestador — eligió Nomad sobre k8s para bare metal, 4 SREs gestionan 11,000+ nodos), CockroachDB, MongoDB, InfluxDB, Elasticsearch, GitLab + Jenkins (CI/CD), Ray (inference distribuido)
- **Decisión notable:** Bare metal + Nomad vs k8s en cloud — redujo costos, doubled densidad de game servers en mismo hardware; octubre 2021 outage → mejoras de resiliencia documentadas
- **Escala:** 88M+ DAU (2024), 1B+ requests de personalización/día

### 53. Discord
- **Lenguajes:** Rust (servicios críticos de datos), Elixir/Erlang BEAM (WebSocket gateway), Python (API monolito original, ML), C++ (audio/video cliente)
- **OSS creado:** Contribuciones activas al ecosistema Rust (tokio, prost)
- **Infra adoptada:** ScyllaDB (migración desde Cassandra 2023 — 177 nodos Cassandra → 72 ScyllaDB, p99 latencia 40-125ms → 15ms), Elixir gateway (5M usuarios WebSocket concurrentes, 26M eventos/seg)
- **Decisión notable:** "Why Discord is switching from Go to Rust" (2020) — uno de los posts de ingeniería más leídos; Go→Rust para Read States: eliminó GC pauses completamente; Cassandra→ScyllaDB migración de trillones de mensajes en 9 días (migrador Rust propio con checkpointing SQLite)
- **Escala:** 19M+ servidores, miles de millones de mensajes/día, 26M WebSocket events/seg

### 54. Twitch
- **Lenguajes:** Go (core backend microservicios, chat, edge systems), C/C++ (transcode pipeline), Python (data/ML), TypeScript (frontend)
- **OSS creado:** Librerías IRC/EventSub SDK oficiales; contribuciones al ecosistema Go
- **Infra adoptada:** CDN propio (~100 PoPs), backbone de red privado, transcode cluster (C/C++→HLS multi-bitrate), AWS hosted
- **Decisión notable:** Ruby/Rails monolith → microservicios Go (2015–2018) — una de las mayores migraciones Go documentadas públicamente en producción; arquitectura de ingestion de video en vivo documentada como referencia
- **Escala:** 35M+ DAU, millones de streams concurrentes en eventos pico

### 55. Valve
- **Lenguajes:** C++ (Source 2 engine, Steam, juegos), Python (tooling), Go (algunos servicios)
- **OSS creado:** GameNetworkingSockets (UDP confiable con NAT traversal y AES-GCM-256, usado en CS2 y Dota 2), Proton (Wine+DXVK para juegos Windows en Linux/Steam Deck), SteamOS (Arch Linux-based), contribuciones a DXVK
- **Infra interna:** Dota 2: 25 clusters globales, ~160 máquinas/cluster, ~112 instancias de juego/máquina; Valve SDR (Steam Datagram Relay) — red de relay propietaria anti-DDoS
- **Decisión notable:** GameNetworkingSockets open sourced (2019) — antes solo disponible via Steam SDK; Steam Deck (2022) como plataforma de referencia para Vulkan translation layer
- **Escala:** 132M+ cuentas Steam registradas, 30M+ peak concurrentes Steam (2024)

### 56. King (Candy Crush)
- **Lenguajes:** Java (backend), Kotlin (servicios nuevos), C++ (game engine/cliente), JavaScript/TypeScript (web)
- **OSS creado:** Contribuciones limitadas; partnerships con vendors
- **Infra adoptada:** Google Cloud (migración post-Activision Blizzard), BigQuery (analytics), Vertex AI/Gemini (ML), GKE, Elasticsearch, Kafka, HashiCorp (Vault, Terraform)
- **Decisión notable:** Migración core a Google Cloud con BigQuery+Vertex AI; BAIT (Bot AI Testing) para QA automatizado de Candy Crush
- **Escala:** Candy Crush: 240M+ MAU, King: 250M+ MAU

### 57. Unity Technologies
- **Lenguajes:** C# (runtime, editor, APIs), C++ (engine internals, render pipelines), Rust (tooling emergente)
- **OSS creado:** com.unity.netcode.gameobjects (Netcode for GameObjects), com.unity.transport (UDP transport DOTS-compatible), DOTS packages: Entities, Physics, Burst Compiler (C#→LLVM nativo), Jobs System
- **Productos cloud:** Unity Gaming Services (UGS): Multiplay Hosting, Vivox (voice en Fortnite, PUBG, LoL), Relay, Lobby, Matchmaking
- **Decisión notable:** DOTS/ECS — pivot fundamental de OOP GameObject a data-oriented design para alto conteo de entidades; Burst Compiler compila subset C# (HPC#) a SIMD optimizado via LLVM
- **Escala:** 1.8M creators activos/mes, juegos Unity en 3.6B dispositivos

### 58. Electronic Arts (EA)
- **Lenguajes:** C++ (Frostbite engine, EASTL), C# (tooling), Java/Scala (data platform), Python (data/ML)
- **OSS creado:** EASTL (EA Standard Template Library — STL alternativo optimizado para gaming, ampliamente adoptado en la industria), EABase, EAThread, EAStdC, CnC Remastered source (GPL)
- **Infra adoptada:** Amazon EMR (migración desde Hadoop, reducción ~20% TCO, 2x volumen de datos), Amazon EKS+EFS (patch pipeline), AWS S3+S3 Glacier (archival)
- **Decisión notable:** Frostbite engine unificado entre DICE/BioWare/Criterion/Maxis; EASTL open sourced — referencia de STL con allocator hooks para gaming
- **Escala:** 670M+ cuentas EA registradas, 10M concurrentes en lanzamientos Battlefield

### 59. Zynga
- **Lenguajes:** PHP (legacy social web games), Java (backend post-mobile pivot), Kotlin/Swift (mobile), C++ (game engine mobile)
- **OSS creado:** zynga-hacklang-framework (deprecated), PlayScript (compilador ActionScript 3 cross-platform para migrar Flash a mobile), contribuciones a Membase/Couchbase
- **Infra:** Migró a zCloud propio (2011) → regresó a AWS (~2015) — caso de estudio en "build vs buy cloud" — Zynga concluyó que seguir el ritmo de innovación de AWS era inviable; Hadoop → AWS EMR+S3, Kafka, Redis/Memcached
- **Decisión notable:** zCloud (2011-2015): el caso más citado del "private cloud failure" — 230 nodos zCloud → 115 nodos EMR. Adquirida por Take-Two Interactive (2022)
- **Escala:** Peak (2012): 300M+ MAU (FarmVille); actual: 25-30M DAU

---

## Grupo 7 — Data & Analytics

### 60. Snowflake
- **Lenguajes:** Java, C++ (engine propietario), Python (Snowpark SDK)
- **OSS creado:** snowflake-connector-python, gosnowflake, terraform-provider-snowflake, ArcticTraining/ArcticInference (Rust/Python para LLMs, 2025)
- **Arquitectura:** Separación compute/storage (Virtual Warehouses independientes pueden leer mismo storage simultáneamente sin contención); micro-partitions columnar comprimidas en S3/GCS/Azure Blob; soporte nativo de tablas Apache Iceberg
- **Decisión notable:** Múltiples warehouses sobre mismo storage sin contención — diferenciación comercial clave vs Redshift; Snowpark Container Services permite GPU workloads dentro del perímetro Snowflake
- **Escala:** 9,400+ clientes incluyendo 590 de Forbes Global 2000

### 61. dbt Labs
- **Lenguajes:** Python (dbt-core, SQL+Jinja), Rust (dbt-fusion, nueva engine en beta)
- **OSS creado:** dbt-core (12,349★ — transformaciones SQL como modelos, DAG via `ref()`), MetricFlow (semantic layer, metrics-as-code), dbt-fusion (Rust, binario único sin Python/JVM, Apache Arrow end-to-end, DataFusion como IR), dbt-mcp (integración agentes AI)
- **Decisión notable:** dbt-fusion: reescritura completa en Rust con Apache Arrow/DataFusion — mismo IR que Polars; ADBC drivers para todas las warehouses; compilación per-microarchitectura de CPU
- **Escala:** Estándar de facto para transformaciones en el modern data stack; miles de empresas

### 62. Fivetran
- **Lenguajes:** Java (conectores internos), Python (SDK público)
- **OSS creado:** Connector SDK (Python), Activations Destination SDK; sin proyectos OSS significativos propios
- **Infra/Producto:** 700+ conectores pre-built, CDC via log-based replication (binlog MySQL, WAL PostgreSQL, redo logs Oracle); HVR (adquirido 2021): enterprise CDC para Oracle, SQL Server, SAP
- **Decisión notable:** Adquisición de HVR específicamente para capturar mercado enterprise CDC/SAP; propuesta de valor = fiabilidad gestionada + automatic schema migration
- **Escala:** Miles de millones de filas/mes procesadas

### 63. Airbyte
- **Lenguajes:** Python (plataforma principal), Kotlin (platform services), Go (CLI abctl)
- **OSS creado:** airbyte (20,842★, auto-hospedable), PyAirbyte (conectores en Python/notebooks), terraform-provider-airbyte
- **Arquitectura:** Conectores = Docker images conformando Airbyte Specification (protocolo JSON/JSONL agnóstico a lenguaje); Temporal para orquestación de workflows; k8s-native (cada sync en pod aislado); CDK en 3 tiers: Python full, Low-code YAML, Builder UI no-code
- **Decisión notable:** Docker/k8s por conector = sacrifica simplicidad operacional por extensibilidad y contribuciones de comunidad; 703+ conectores
- **Escala:** Ampliamente auto-hospedado; Airbyte Cloud como managed offering

### 64. Starburst (Trino)
- **Lenguajes:** Java (99.2%, 44,574 commits, 896 contribuidores)
- **OSS creado:** Trino (12,600★, fork de Presto por sus creadores originales de Facebook — Martin Traverso, Dain Sundstrom, David Phillips, Eric Hwang)
- **Arquitectura:** Query federation engine — no almacena datos; consulta en-place sobre fuentes heterogéneas (S3/Iceberg/Delta, RDBMS, NoSQL, SaaS); coordinator+worker MPP; cost-based optimizer con reordenación de joins
- **Starburst commercial:** Warp Speed (indexing propietario, +700% velocidad queries), Starburst Galaxy (cloud managed)
- **Escala:** Netflix (exabyte-scale), LinkedIn (hundreds of PB, millones queries/mes), Shopify (15 Gbps, 300M rows/seg), DiDi (1M+ queries/día)

### 65. ClickHouse
- **Lenguajes:** C++ (71.3%), Python (9.9%), Assembly (8.6%)
- **Origen:** Yandex (Yandex.Metrica — 13+ trillones de registros, 20B+ eventos/día), open sourced junio 2016; ClickHouse Inc. fundada 2021 ($250M Series B)
- **OSS:** github.com/ClickHouse/ClickHouse (46,200★, 8,200 forks, 1,910+ contributors, Apache 2.0)
- **Arquitectura única vs Redshift/BigQuery:** MergeTree (inserts crean parts inmutables + merge background, sin locks entre reads/writes), sparse primary index (1 entrada/8,192 filas), vectorized execution + SIMD (65,536 valores/instrucción CPU), 30+ implementaciones de hash table, codecs Delta+LZ4/ZSTD/Gorilla, sin JVM (no GC pauses)
- **Usuarios en producción:** Cloudflare (36 nodos, 6M HTTP req/seg analytics, "cientos de millones de filas/seg insertadas"), Uber, Ahrefs, Mixpanel, Amplitude, PostHog, Spotify, Microsoft, Meta, eBay, LangChain (LangSmith)
- **Escala:** Sub-second queries sobre miles de millones de filas; 46.2k★ GitHub

### 66. Amplitude
- **Lenguajes:** Python, Go, Java
- **OSS creado:** Ninguno significativo
- **Infra adoptada:** Apache Druid (históricamente, pre-aggregación y real-time ingestion), ClickHouse (migración progresiva para reducir complejidad operacional), Kafka (ingestion pipeline)
- **Decisión notable:** 1 trillion eventos/año procesados; migración Druid→ClickHouse por complejidad operacional — patrón común en product analytics
- **Escala:** Cientos de miles de millones de eventos/mes

### 67. Mixpanel
- **Lenguajes:** Python, Go
- **OSS creado:** Ninguno significativo
- **Infra propia:** ARB format (almacenamiento columnar propietario particionado por user_id/tiempo — construido antes de que OLAP databases estuvieran disponibles); sistema dinámico de merge de archivos (reducción 88% en total file count); Apache Arrow para procesamiento vectorizado en memoria sobre archivos ARB
- **Decisión notable:** Formato ARB propio (2009) predató ClickHouse/Druid disponibles; ahora mantiene ARB como store primario + ClickHouse para patrones de query específicos — costo de almacenamiento bespoke a escala
- **Escala:** Trillones de eventos, millones de clientes

### 68. Looker (Google)
- **Lenguajes:** Ruby (histórico, JRuby para JVM), Go (reescritura post-adquisición)
- **OSS creado:** LookML (DSL de modelado — spec pública pero no OSS); sin OSS significativo
- **Arquitectura:** Semantic layer sobre warehouse — LookML define dimensions/measures/explores/views; compila a SQL nativo del warehouse; queries via API programmatic (Looker como infraestructura, no solo BI tool)
- **Infra adoptada:** BigQuery (backend preferido post-adquisición Google), BigQuery BI Engine (caché en memoria para queries Looker)
- **Decisión notable:** LookML pionero de "metrics-as-code" 5 años antes de dbt MetricFlow — pero propietario. Adquirido por Google ($2.6B, 2020)
- **Escala:** Clientes enterprise: Etsy, NYT, Spotify, Walmart

### 69. Monte Carlo
- **Lenguajes:** Python (agent), Go (OTel collector fork)
- **OSS creado:** apollo-agent (Python containerizado, puente SaaS→infra cliente), data-downtime-challenge (educativo)
- **Arquitectura:** "At-rest monitoring" — ejecuta queries de metadata/estadísticas en el warehouse del cliente sin extraer datos raw; Apollo Agent en VPC del cliente; detección de anomalías ML sobre estadísticas históricas de tablas
- **Infra adoptada:** AWS S3/GCS/Azure, graph database internamente para column-level lineage
- **Decisión notable:** Privacy-first: SaaS nunca toca datos raw del cliente — agent proxy hace todo; popularizó "5 pillars of data observability" (freshness, volume, schema, distribution, lineage) como framework de industria
- **Escala:** Series C $135M; clientes: Fox, JetBlue, Nasdaq

### 70. Apache Flink / Ververica
- **Lenguajes:** Java (87.3%), Scala (8.2%), Python (2.8%)
- **Origen:** TU Berlin Stratosphere project → Flink (2014); Ververica fundada por creadores originales (Stephan Ewen, Kostas Tzoumas); adquirida por Alibaba (2019)
- **OSS:** apache/flink (25,800★, 1,339 contributors, 37,589 commits); apache/fluss (incubating — streaming storage columnar Arrow-native, reemplaza Kafka para analytics)
- **Arquitectura:** True streaming (un evento a la vez) vs micro-batching Spark; exactly-once via Chandy-Lamport snapshots distribuidos; state backends: RocksDB (terabytes de estado), HashMap (in-memory); Flink SQL con CDC integration (Debezium/Canal)
- **Usuarios en producción:** Alibaba (petabytes/día, Double 11), Uber (AthenaX SQL-on-Flink), Lyft (ML features), Pinterest, Capital One, Shopify (300M rows/seg), Klaviyo (1M+ eventos/seg dedup), Index Exchange (trillones de eventos/día)
- **Escala:** Amazon Managed Service for Apache Flink (ex-Kinesis Data Analytics)

### 70b. dbt Labs (ya incluida en #61) / Fivetran (#62) / Airbyte (#63)

---

## Grupo 8 — DevTools & Plataformas

### 71. GitHub
- **Lenguajes:** Ruby on Rails (monolito github/github), Go (tooling, CLI, servicios infra), TypeScript/React
- **OSS creado:** gh-ost (migraciones schema MySQL sin triggers — tails binary log como pseudo-réplica, migrations pausables y controlables), Scientist (Ruby — refactoring seguro ejecutando código viejo+nuevo en producción, comparando resultados), Linguist (detección de lenguajes), Octokit (SDKs API oficiales), fork de Vitess
- **Infra adoptada:** MySQL (1,200+ hosts) + Vitess (VTGate en k8s) + ProxySQL (connection pooling); Redis, Kafka, HAProxy
- **Decisión notable:** Mantiene monolito Rails en lugar de microservicios — escala via particionamiento DB y servicios Go para hot paths; particionó `mysql1` (950k queries/seg) en clusters distribuidos → 1.2M queries/seg combined, 50% menos carga por host; GitHub Actions: 23M→71M jobs/día en backend re-arquitectado
- **Escala:** Cientos de millones de repos, 71M CI jobs/día, 1.2M MySQL queries/seg

### 72. GitLab
- **Lenguajes:** Ruby on Rails (app principal), Go (Gitaly, Workhorse, Runner)
- **OSS creado:** Gitaly (Go gRPC service para operaciones Git — elimina NFS mounts, horizontal scaling sin filesystems compartidos; GitLab.com eliminó NFS en GitLab 11.5 2018), Workhorse (Go reverse proxy inteligente — maneja Git push/pull HTTP, uploads/downloads pesados, LFS sin tocar Rails), GitLab Runner
- **Infra adoptada:** PostgreSQL, Redis, S3 (artifacts, LFS), Gitaly Cluster (HA Git storage)
- **Decisión notable:** Comprometido con Ruby on Rails — escala via servicios Go para hot paths; producto completo open source (MIT/EE) — arquitectura completamente inspeccionable
- **Escala:** Una de las mayores deployments Rails del mundo; miles de empresas auto-hospedan

### 73. Atlassian
- **Lenguajes:** Java (Jira, Confluence core), Kotlin (servicios nuevos), Go, Node.js
- **OSS creado:** Atlaskit (React component library, open source)
- **Infra adoptada:** AWS-native (Marketplace), PostgreSQL/DynamoDB/Aurora, EKS (Kubernetes)
- **Decisión notable:** Jira/Confluence migrados a AWS Graviton4 (tras dos intentos fallidos con Graviton2 y Graviton3 por comportamiento L3 cache en JVM) — ~10% reducción costos cloud; JCMA re-arquitectado 6x más rápido con async processing; Data Center EOL: 28 marzo 2029 (fuerza migración a cloud)
- **Escala:** Decenas de millones de usuarios (Jira, Confluence, Bitbucket)

### 74. Figma
- **Lenguajes:** C++ (core editor compilado a WebAssembly), TypeScript/React (UI shell), Rust (tooling infra)
- **OSS creado:** Contribuciones al ecosistema WebAssembly; LiveGraph documentado públicamente
- **Infra adoptada:** PostgreSQL (shards múltiples), LiveGraph (replication stream de Postgres WAL → WebSocket a clientes en ms), WebGL/WebGPU (rendering canvas)
- **Decisión notable:** C++ compilado a WebAssembly → 3x reducción de load time vs renderer JavaScript previo (apuesta temprana antes de que Wasm fuera mainstream); multiplayer: servidor in-memory por documento (simple, baja latencia, desafíos de scaling); LiveGraph usa Postgres WAL (no event bus separado) para real-time subscriptions
- **Escala:** Millones de archivos de diseño activos, 3x crecimiento de sesiones desde 2021

### 75. Linear
- **Lenguajes:** TypeScript (frontend+backend), React, Node.js
- **OSS creado:** Ninguno significativo; sync engine documentado públicamente
- **Infra adoptada:** PostgreSQL (server-side), SQLite via IndexedDB (client-side por usuario), WebSockets (streaming de cambios real-time), MobX (reactive state client-side)
- **Decisión notable:** Sync engine local-first: bootstrap snapshot → SyncActions delta via WebSocket persistente → escrituras optimistas aplicadas a IndexedDB inmediatamente (UI instantánea); toda la arquitectura existe para hacer la UI sentir instantánea
- **Escala:** Equipos de ingeniería de alto crecimiento; ~50 ingenieros, eficiencia extrema

### 76. Notion
- **Lenguajes:** Node.js/TypeScript (backend API), React (frontend), Python (data engineering)
- **OSS creado:** Ninguno significativo; posts de sharding PostgreSQL son de los más detallados en la industria
- **Infra adoptada:** PostgreSQL (sharding a nivel aplicación — 480 logical shards en 32→96 physical databases; shard key: workspace_id; 480 elegido por tener muchos factores), Debezium+Kafka+Spark (data lake sobre EKS), Redis, S3
- **Decisión notable:** Application-level sharding (lógica de routing custom) en lugar de Vitess/Citus — máximo control aceptando costo de ingeniería; "The Great Re-shard" 2023: 480 shards en 96 instancias físicas con zero downtime; datos crecieron 20B→200B+ blocks en 3 años
- **Escala:** 200B+ block rows en PostgreSQL, crecimiento 2x cada 6-12 meses (2022-2024)

### 77. Postman
- **Lenguajes:** Node.js (backend microservicios + runtime), JavaScript/TypeScript (Electron desktop + web), React
- **OSS creado:** Newman (CLI Collection runner Node.js), Postman Runtime (motor de ejecución de requests), openapi-to-postman (converter OpenAPI)
- **Arquitectura:** V8 sandbox por request: cada script de test JavaScript corre en contexto V8 aislado (seguridad crítica cuando teams comparten collections con scripts); migración de Chrome extension a Electron — habilitó localhost APIs, certificados, filesystem (imposible en browser extension)
- **Decisión notable:** Chrome extension → Electron como decisión arquitectónica fundacional; sandbox V8 por request para manejar scripts no confiados de Collections compartidas
- **Escala:** 35M+ developers, mayor hub público de APIs

### 78. Sentry
- **Lenguajes:** Python/Django (app principal + API), TypeScript/React (frontend), Rust (Relay — proxy de ingestion de alto rendimiento)
- **OSS creado:** Snuba (query layer ClickHouse — SnQL language, query optimizer, Kafka-based ingestion consumers; 62x más rápido para unstructured data queries via bucketing schema), Relay (Rust — filtra, normaliza, scrubbing PII, rate-limiting antes de Kafka; Python no podía el throughput del hot path SDK-facing), SDK ecosystem para 30+ lenguajes
- **Infra adoptada:** ClickHouse (eventos time-series via Snuba), PostgreSQL (metadata relacional), Kafka, Redis, Django+Celery
- **Decisión notable:** Tagstore/TSDB→ClickHouse+Snuba: migración multi-año cambiando fundamentalmente el modelo de datos (write-time denormalization → read-time columnar queries); Relay en Rust para ingest hot path; Sentry es open source — arquitectura completamente inspeccionable
- **Escala:** Miles de millones de eventos, cientos de miles de organizaciones

### 79. LaunchDarkly
- **Lenguajes:** Go (backend, SDKs primarios)
- **OSS creado:** go-server-sdk, ld-relay (Go — Relay Proxy que multiplexes conexiones SDK en la infra del cliente; recomendado con DynamoDB para caché persistente de flags), SDKs para 30+ lenguajes
- **Arquitectura:** Flag Delivery Network (FDN, CDN propietaria con 100+ PoPs); evaluación local en SDK (SDK descarga ruleset completo y evalúa en-proceso con zero round-trips a LaunchDarkly por evaluación de flag — esto es lo que hace posible miles de millones de evaluaciones/día); 6 capas de failover (in-memory → fallback values → CDN → multi-region → SDK retry → Relay Proxy)
- **Decisión notable:** Client-side flag evaluation como decisión arquitectónica definitoria — competidores que evalúan server-side introducen latencia de red por flag check; LaunchDarkly añade latencia cero
- **Escala:** Miles de millones de evaluaciones de flags/día, sub-millisecond latencia de evaluación

### 80. PagerDuty
- **Lenguajes:** Ruby on Rails (monolito original core), Scala (Artemis — servicio extraído para notification scheduling), Elixir (stateful real-time processing — cada usuario asociado a partición Kafka), Go, Java/Kotlin
- **OSS creado:** go-pagerduty (Go client API), componentes del scheduler distribuido
- **Infra adoptada:** MySQL, Cassandra (WorkQueue distribuido + datos time-series), Kafka (event bus central — outage Kafka septiembre 2025 silenció alertas para miles de clientes), Redis, Akka+Kafka+Cassandra (distributed task scheduler)
- **Decisión notable:** Elixir adoptado para stateful stream processing por-usuario — modelo de actor OTP mapea bien a processing stateful por-entidad; "es poco probable que algo fluyendo por PagerDuty no sea tocado por código Elixir"
- **Escala:** Millones de alertas/notificaciones, decenas de miles de clientes enterprise

---

## Grupo 9 — Seguridad

### 81. CrowdStrike
- **Lenguajes:** C++ (kernel driver Windows — CSagent.sys), eBPF (Linux sensor, sin módulo kernel), Rust (ML inference via tf2rust), Go (gofalcon SDK, falcon-operator)
- **OSS creado:** tf2rust (convierte modelos TensorFlow a Rust puro — Dark Knight URL classifier: -79% memoria; Airen ransomware detector: -89% memoria, +74.2% velocidad), rusty-falcon (Rust SDK para Falcon APIs), gofalcon, kafka-replicator
- **Arquitectura kernel:** Windows: kernel-mode driver + ELAM (early boot); Linux: eBPF puro (sin kernel module). Outage julio 2024: bounds-check missing en interpreter C++ de Channel File 291 → out-of-bounds read en contexto kernel
- **Decisión notable:** Permanecer en Windows kernel (no eBPF que no está maduro en Windows) para tamper resistance y early boot timing — tradeoff: máxima visibilidad vs riesgo de crash si content updates son malformados
- **Escala:** Millones de endpoints protegidos globalmente

### 82. Palo Alto Networks
- **Lenguajes:** C++ (Cortex XDR agent core), Rust (Cross Platform team documentado en job listings), Python (pan-os-python SDK), Go (terraform-provider-panos)
- **OSS creado:** pan-os-python (388★), pan-os-ansible (228★), terraform-provider-panos (Go, 108★), Unit42-timely-threat-intel (456★)
- **Infra:** Cortex Cloud (unificación CNAPP+CDR+XDR, feb 2025); eBPF en Linux para kernel telemetry (dual mode: userspace + eBPF kernel mode)
- **Decisión notable:** Cortex XDR Cross Platform team usa Rust junto a C++ — Rust para memory safety en código de agent crítico
- **Escala:** Decenas de miles de clientes enterprise, global

### 83. SentinelOne
- **Lenguajes:** Go (backend platform), eBPF programs (Linux agent), Python (Purple AI/ML)
- **OSS creado:** Singularity Data Lake (via DataSet/Scalyr adquirido $155M 2021 — schema-free, petabyte-scale, 10x más rápido que SIEM tradicional)
- **Arquitectura eBPF:** Linux CWPP agent = eBPF exclusivamente (sin kernel module → sin riesgo de kernel panic, sin recompilación por kernel updates, un binario para 13+ distros); eBPF Maps transfieren datos kernel↔userspace; MITRE: 99-100% analytic coverage
- **Decisión notable:** Compromiso total con eBPF en Linux (vs kernel module) por estabilidad operacional y DevOps agility
- **Escala:** Millones de endpoints; DataSet: petabytes de security telemetry

### 84. Wiz
- **Lenguajes:** Go (plataforma completa)
- **OSS creado:** open-cvdb (open cloud vulnerability database, 376★), grammes (Go Gremlin/TinkerPop client)
- **Arquitectura:** Wiz Security Graph = Amazon Neptune (graph database managed); 100 billones de relaciones en Neptune; Gremlin/TinkerPop query layer; agentless CSPM (API read-only del cloud provider — sin agentes); Wiz Defend (CDR via Gem acquisition $350M) añade runtime detection
- **Decisión notable:** Graph-first para cloud security — cada recurso es nodo, cada relación (acceso, red, datos) es arista; Neptune traversal responde "¿cuál es el blast radius si esta credencial se compromete?" en segundos sobre 100B+ edges
- **Escala:** Cientos de miles de clientes cloud

### 85. Snyk
- **Lenguajes:** TypeScript (CLI v1, Node.js), Go (CLI v2, migración en progreso), Python (agent-scan AI)
- **OSS creado:** cli (TypeScript, 5,443★), driftctl (infrastructure drift detection Go, 2,600★), agent-scan (AI MCP security scanner Python, 1,800★), parlay (SBOM enrichment Go), zip-slip-vulnerability (referencia educativa)
- **Arquitectura dual CLI:** Binary wrapper TypeScript + backend Go para comandos migrados — transparente para usuario; cerró contribuciones externas agosto 2025 para centralizar QA
- **Decisión notable:** TypeScript→Go migration para CLI por performance; vulnerability DB: 3x más entradas que siguiente mayor DB pública, detecta vulnerabilidades 47 días antes que competidores en promedio
- **Escala:** Millones de scans/día, 13 lenguajes / 20+ package managers

### 86. Lacework (Fortinet FortiCNAPP)
- **Lenguajes:** Go (plataforma completa, agente ligero)
- **OSS creado:** Ninguno significativo post-adquisición
- **Infra:** ClickHouse (Polygraph Data Platform — time-series event storage para security analytics); Polygraph ML engine: behavioral baselines automáticos sin reglas (semanas/meses de baseline por entidad); Composite Alert: threat behaviors + IOC feeds + anomaly deviations
- **Nota:** Fortinet adquirió Lacework (2023, post-raise de $1.87B); rebrandizado como FortiCNAPP (oct 2024)
- **Decisión notable:** ML anomaly detection sin reglas manuales — apuesta contra detección basada en firmas; ClickHouse para mantener baselines temporales a escala
- **Escala:** Multi-cloud: AWS, Azure, GCP, Kubernetes simultáneamente

### 87. Orca Security
- **Lenguajes:** Go (plataforma completa)
- **OSS creado:** Ninguno significativo
- **SideScanning (patentado):** Lee block storage (snapshots de disco) out-of-band — zero agentes, zero paquetes de red enviados a workloads, zero código ejecutado en target; monta snapshots read-only; perfil de riesgo completo en <24h desde setup inicial; cubre VMs, contenedores, serverless, storage buckets, VPCs, KMS keys
- **Decisión notable:** Agentless-first como counter-positioning contra CrowdStrike/SentinelOne — tradeoff: sin live runtime behavioral detection vs zero overhead operacional y 100% cobertura de assets (incluyendo assets que no pueden ejecutar agentes)
- **Escala:** 5,500+ clientes enterprise (DoorDash, Forbes, Walmart Labs, Lululemon)

### 88. Tenable
- **Lenguajes:** C (Nessus scanner daemon), NASL (Nessus Attack Scripting Language — DSL propietario inspirado en C/Perl para plugins), Python (pyTenable), Go (Terrascan)
- **OSS creado:** Terrascan (IaC security scanner Terraform/k8s/Helm, Go, 5,200★), pyTenable (Python API client, 401★), KaiMonkey (IaC vulnerable training), EscalateGPT (AWS IAM privilege escalation con AI)
- **Arquitectura:** Nessus: client-server completamente userspace (no kernel driver), network-based scanner + agente local; NASL permite comunidad masiva de autores de plugins
- **Decisión notable:** Terrascan (Go) representa el shift-left de Tenable hacia IaC security; NASL como DSL permite proteger técnicas sensibles en forma compilada mientras la comunidad contribuye plugins
- **Escala:** Nessus: scanner de vulnerabilidades más deployado del mundo

### 89. 1Password
- **Lenguajes:** Rust (~70% de 1Password 7 Windows; headless core unificado multi-plataforma), Go (SRP server-side), TypeScript (VS Code extension, Connect SDK)
- **OSS creado:** typeshare (Rust→Swift/Kotlin/TypeScript type generation, 2,904★), srp (Go, RFC 2945/5054), passkey-rs (WebAuthn passkeys), zeroizing-alloc (heap zero-on-free seguro), shell-plugins (credential injection terminal, Go)
- **Arquitectura Rust:** Core headless único en Rust (cryptography via `ring`, DB access, server comms, sync); UIs nativas (macOS Swift, Windows, Android Kotlin) como thin shells; cross-language type safety via typeshare; protocolo SRP (prueba de conocimiento de contraseña sin transmitirla)
- **Decisión notable:** Rust para cryptography y manejo de secrets = elección explícita de memory safety — elimina clases enteras de bugs (use-after-free, buffer overflows) del componente que guarda secrets de clientes; typeshare existe porque el modelo Rust-first requiere sincronización de tipos en 5+ plataformas
- **Escala:** Millones de usuarios, miles de clientes business/enterprise

### 90. Tailscale
- **Lenguajes:** Go (95.5% codebase completo — `tailscaled` daemon, `tailscale` CLI, DERP relay), C (1.7%), TypeScript (1.1%)
- **OSS creado:** tailscale (cliente completo + CLI + DERP relay, 29.2k★, 303 contributors), tsnet (librería Go embebible para programas Go), wireguard-go mejorado con optimizaciones de throughput
- **Arquitectura:** Control plane centralizado (solo intercambia public keys, casi no lleva tráfico); data plane P2P mesh (WireGuard encriptado); NAT traversal via STUN/ICE; DERP relay fallback cuando UDP bloqueado; gVisor netstack embebido para tsnet
- **Performance medida:** AWS c6i.8xlarge: 7.32 Gbps (vs in-kernel WireGuard 2.67 Gbps); bare metal i5-12400: 13.0 Gbps (supera in-kernel WireGuard 11.8 Gbps); TSO/GRO/mmsg: 2.2x mejora de throughput; checksum loop: -57% tiempo
- **Decisión notable:** Go userspace WireGuard (no módulo kernel) → un codebase para Linux/Windows/macOS/FreeBSD/iOS/Android; eBPF deliberadamente evitado (usa TUN/TAP + kernel offloads estándar); rendimiento userspace ahora supera in-kernel en algunos hardware
- **Escala:** Millones de dispositivos en mesh, usada por miles de empresas

---

## Grupo 10 — Fintech & Crypto

### 91. Coinbase
- **Lenguajes:** Go (primario), Ruby (legacy), TypeScript (frontend), Python (data)
- **OSS creado:** Rosetta/Mesh (spec HTTP API para interoperabilidad blockchain, 20+ chains implementan), MPC cryptography library (multi-party computation para wallet key management), Base (Ethereum L2 sobre OP Stack, OSS octubre 2023)
- **Infra adoptada:** PostgreSQL en AWS RDS (migración desde MongoDB — 1B+ filas), DynamoDB, EKS (Graviton processors), Databricks (analytics), Snapchain (backup/deployment de nodos blockchain via EBS snapshots)
- **Decisión notable:** MongoDB→PostgreSQL (migración más grande interna, dual-write/dual-read phased); EKS Graviton — decoupling de host provisioning de service deployment
- **Escala:** 150M+ usuarios verificados, nodos blockchain para 60+ chains

### 92. Binance
- **Lenguajes:** C++ (matching engine core), Java, Python (ML), Go
- **OSS creado:** binance-java-api, AI trading prototype (OpenAI + Redis Pub/Sub)
- **Infra:** Matching engine C++ con arquitectura event-sourcing implícita (order book matching inherentemente append-only); Redis para Pub/Sub y persistencia
- **Decisión notable:** Upgrade del matching engine 2024: 10ms→5ms latencia → +15% trades diarios en una semana; arquitectura propietaria no detallada públicamente
- **Escala:** 1.4M órdenes/segundo, 150M+ usuarios

### 93. Robinhood
- **Lenguajes:** Python (Django, primario), Go (creciente), Rust (tooling)
- **OSS creado:** Faust (Python stream processing — port de Kafka Streams a Python, miles de millones de eventos/día), kafkaproxy (Rust sidecar — colapsa connection fan-in de gunicorn workers a Kafka, reduce N×M conexiones), kafkahood (Python Kafka client wrapper)
- **Infra adoptada:** PostgreSQL en AWS RDS, Amazon Redshift, EKS, ELK stack, Kafka+Flink (250+ aplicaciones Flink en producción), Diskless Kafka (Confluent, 2026 para log analytics)
- **Decisión notable:** Application-layer sharding: cada shard posee subset de usuarios con sus propios app servers+DB+deployment pipeline; kafkaproxy en Rust por performance de connection management
- **Escala:** 24M+ cuentas, 10+ TB de datos/día, 250+ apps Flink en producción

### 94. Plaid
- **Lenguajes:** Go, Python, TypeScript; SDKs en JavaScript, React, iOS, Android, Ruby
- **OSS creado:** Devenv (CLI tool para remote dev environments documentado)
- **Infra adoptada:** TiDB (migración desde Amazon Aurora 2023-2025 — 500k QPS ceiling de Aurora alcanzado, 800+ servidores Aurora consolidados), Terraform + Atlantis (infra automation via PRs), GitHub Enterprise self-hosted
- **Decisión notable:** Aurora→TiDB: la migración más significativa y mejor documentada — driven by 500K QPS limit hitting Aurora's ceiling; TiDB = MySQL-compatible distributed SQL
- **Escala:** 500M+ cuentas financieras conectadas, 500,000+ QPS (que forzó la migración)

### 95. Adyen
- **Lenguajes:** Java (backend primario), C++ (algunos componentes), Python (data/ML), SQL
- **OSS creado:** SDKs open source (Java, .NET, Node.js, Python, PHP, Go, Ruby); graph database custom sobre PostgreSQL+MyBatis+Java para routing de pagos
- **Arquitectura:** Full-stack acquirer — construyeron su propio banco (member principal de Visa y Mastercard), bypaseando intermediarios completamente; Java+PostgreSQL como core; HDFS+Spark para ML/big data; GCP+Kubernetes
- **Decisión notable:** "Tech stack is open source or built in-house" — sin commercial middleware; Design to Duty: ingenieros que diseñan sistemas están on-call para ellos
- **Escala:** €1,285.9B volumen procesado (2024), peak Black Friday 2024: 160,000 transacciones/minuto

### 96. Wise (TransferWise)
- **Lenguajes:** Java, Kotlin (1,000+ microservicios), Kotlin/Android+Compose (mobile), TypeScript
- **OSS creado:** Ninguno significativo (documentan stack via Engineering Medium blog — actualizaciones 2020, 2022, 2025)
- **Infra adoptada:** Kafka ("cientos de stream processing apps"), MariaDB/PostgreSQL/MongoDB (per-service choice), Spinnaker (CD, migración 50%+ completada), Kotlin 2.0/2.1 + Coroutines
- **Arquitectura financiera:** Modelo de netting multi-currency — reduce transacciones cross-border mediante flujos dentro de currency pools (innovación financiera central); event-driven microservicios con ownership delegado de producción de eventos
- **Decisión notable:** MySQL→polyglot DB por servicio; adopción temprana de Kotlin 2.0 documentada; deployment automatizado previno cientos de incidentes en 2024
- **Escala:** 1,000+ microservicios, 16M+ clientes, 13B USD transferidos/mes

### 97. Nubank
- **Lenguajes:** Clojure (backend), ClojureScript (frontend/web), Kotlin (Android), Swift (iOS)
- **OSS creado:** morse (data browser gráfico Clojure), workspaces (live dev environment ClojureScript), clj-github, mockfn, k8s-api (cliente Kubernetes Clojure), Pedestal (via Cognitect)
- **Infra adoptada:** Datomic (BD inmutable append-only con estado histórico completo — audit trails incorporados, aligns con funcional programming), AWS, Kafka (infraestructura compartida entre microservicios), 85+ clusters Kubernetes, 355,000 pods activos, 1 petabyte logs/día
- **Decisión notable:** Adquisición de Cognitect (2020) — creadores de Clojure (Rich Hickey) y Datomic; neobank adquiriendo el creador de su lenguaje primario y base de datos. Datomic = event sourcing a nivel de BD. Deployment time: 90min→15min. 700 deployments/semana
- **Escala:** 122M+ clientes (2024), 3,000+ microservicios, mayor neobank del mundo

### 98. Klarna
- **Lenguajes:** Erlang/BEAM (core de pagos, fault-tolerant), Java, Python, Node.js/TypeScript (microservicios via framework Steve), Scala
- **OSS creado:** Librerías Erlang (bec, etc.), Steve (Node.js/TypeScript microservices framework sobre InversifyJS IoC)
- **Infra adoptada:** AWS EC2+Lambda+Kubernetes, Neo4j (knowledge graph, adoptado ~2025 — reemplazó Salesforce+Workday con stack AI-native), Erlang/OTP+BEAM para sistemas de pago core
- **Decisión notable:** AI assistant reemplazó 700 FTE de customer service (-40% costo por transacción desde Q1 2023); reemplazó Salesforce+Workday con Neo4j como data layer central para GenAI; Saga pattern para transacciones distribuidas; BEAM/Erlang para "100% uptime" en sistemas de pago core
- **Escala:** 150M+ consumidores, 500,000+ merchants

### 99. Affirm
- **Lenguajes:** Python (Flask, primario), Kotlin (servicios JVM), Java, Go
- **OSS creado:** Ninguno significativo
- **Infra adoptada:** Amazon Aurora, EKS, Vitess (MySQL horizontal scaling para data platform), Kafka+Flink (streaming para fraud detection y credit decisioning real-time), Spark, Samza, gRPC+Envoy+Istio, Celery/RabbitMQ
- **Arquitectura financiera:** Real-time credit decisioning síncrono durante checkout (usuario espera mientras se underwrite individualmente cada transacción)
- **Decisión notable:** Arquitectura resiliente documentada (marzo 2024): multi-region, multi-AZ, graceful degradation para 99.99% uptime en Checkout Service; Vitess para escalar MySQL horizontalmente
- **Escala:** 99.99% uptime objetivo (4 nueves) para Checkout Service

### 100. Brex
- **Lenguajes:** Elixir (original primario, migración en curso), Kotlin (nuevo primario), TypeScript, Go, Python (legacy)
- **OSS creado:** Substation (Go — routing y normalización de security event/audit logs), librería Elixir de return-value handling
- **Infra adoptada:** PostgreSQL en AWS RDS, Kafka (async messaging) + CDC de PostgreSQL a Kafka, EKS, Bazel (monorepo polyglot Elixir+Go+TypeScript+Kotlin), gRPC+Protocol Buffers, RabbitMQ (Broadway+Broadway AMQP, event sourcing en Elixir)
- **Arquitectura financiera:** Transactional Event Publishing (TEP) — consistencia fuerte entre publicación de eventos async y transacciones DB (outbox pattern sobre PostgreSQL+Kafka CDC); event sourcing via shared Elixir library
- **Decisión notable:** Elixir→Kotlin (2021-2023): dynamic typing frenaba al equipo en crecimiento, Mix demasiado lento para monorepo, gRPC tooling Elixir inmaduro, JVM = ecosistema más rico + static typing + hiring más fácil; Bazel para builds polyglot reproducibles
- **Escala:** Corporate cards/expense management, escala fintech growth-stage

---

## Grupo 11 — Telecom, CDN & Proxies

### 101. Akamai
- **Lenguajes:** JavaScript/V8 (EdgeWorkers serverless en edge), C/C++ (core CDN), Lua (legacy scripting), Go (tooling interno)
- **OSS creado:** Ninguno significativo propio; adquirió assets de Edgio (2025)
- **Edge:** EdgeWorkers (JS en V8, cold start <5ms, escala automáticamente en cada nodo de edge); Akamai Functions (añade Wasm runtime sobre EdgeWorkers para Rust, Go, Python, JS compilado a Wasm)
- **Escala:** 4,200+ PoPs globales (vs Fastly ~90, Cloudflare ~330), 340,000+ servidores; fracción significativa del tráfico Internet global

### 102. Fastly
- **Lenguajes:** Rust (Compute platform, Lucet compiler), VCL (Varnish Configuration Language — fork de Varnish 2.1.5), Go/TypeScript (SDKs)
- **OSS creado:** Lucet (AOT Wasm compiler en Rust, donado a Bytecode Alliance — merged con Wasmtime), Wasmtime (JIT Wasm runtime, Bytecode Alliance, Fastly es founding member junto a Mozilla, Intel, Red Hat), Viceroy (runtime local testing Compute), compute-starter-kit-rust-default
- **Edge:** Compute@Edge = Wasm isolates por request (no contenedores, no VMs); 100,000+ isolates Wasm por CPU core; cold start ~35 microsegundos; VCL para CDN tradicional; Rust = lenguaje canonical first-class para Compute
- **Decisión notable:** Fastly founding member Bytecode Alliance; Lucet pioneered AOT para CDN workloads; VCL diverge de Varnish open source en v2.1
- **Escala:** 1.8 trillones de requests diarios (Q1 2025), throughput multi-terabit/seg

### 103. DigitalOcean
- **Lenguajes:** Go (servicios core, API backends, doctl CLI), Ruby (legacy), Python (tooling)
- **OSS creado:** doctl (CLI oficial Go para todos los servicios), DOKS (Managed k8s sobre Cluster API), CSI driver para DO Block Storage, cloud-controller-manager k8s, clusterlint (linter de clusters Go)
- **Infra:** DOKS usa Cilium para networking; Cluster API para control planes escalables; foco en developer experience sobre CDN performance tuning
- **Escala:** SMBs y startups primariamente; DOKS como oferta cloud-native principal

### 104. Cloudinary
- **Lenguajes:** Ruby (Rails-based, core), Node.js (SDKs, pipelines), C/C++ (ImageMagick, FFmpeg, codecs internos)
- **OSS creado:** SDKs open source: cloudinary-gem (Ruby), cloudinary_npm (Node.js), Python, Java, PHP, Go, .NET
- **Arquitectura:** URL-based transformation pipeline (cada transformación codificada en la URL de entrega → compute on-demand → cache en edge CDN global); format negotiation automático (WebP, AVIF); AI transformations (2025: generative fill, background removal) integradas en el pipeline CDN
- **Escala:** 30B+ assets under management, 5,500+ enterprise customers

### 105. Twilio
- **Lenguajes:** Java (microservicios core messaging/voice), Go (workloads de alta concurrencia), Ruby/Python (SDKs), Node.js (serverless + webhook-facing)
- **OSS creado:** SDKs oficiales open source en todos los lenguajes (github.com/twilio); Twilio CLI, Serverless Toolkit
- **Decisión notable:** Twilio Segment revirtió de 140+ microservicios a monolito para mejorar productividad de developer (caso público de "goodbye microservices")
- **Escala:** 6.99B mensajes procesados durante Cyber Week 2025; miles de millones de API calls/mes

### 106. SendGrid (Twilio Email)
- **Lenguajes:** Go (MTA — reescritura desde Perl; concurrencia nativa de Go eliminó callback hell), Python (SDK oficial, tooling data)
- **OSS creado:** sendgrid-python, sendgrid-go (SDKs oficiales)
- **Arquitectura:** MTA custom en Go en AWS; SGS (SendGrid Scheduler — "heap of heaps" sobre Ceph para dequeue justo entre senders/dominios receptores); 99.99% uptime SLA
- **Escala:** 190B+ emails/mes; 75.1B emails en Cyber Week 2025; benchmark: 15,000 transacciones/seg, latencia mediana de entrega 1.9 segundos

### 107. Deutsche Telekom
- **Lenguajes:** Python, Go, Java (cloud-native network functions), YAML/Helm/Terraform (automatización), C (5G stack de bajo nivel)
- **OSS creado:** OpenTelekomCloud tooling y Terraform providers (github.com/OpenTelekomCloud); contribuciones a ONAP (Open Network Automation Platform, LF Networking)
- **Infra:** Open RAN (O-RAN via ONAP para lifecycle management); Open Telekom Cloud (OTC) basada en OpenStack — sovereign European public cloud; O-Cloud PoC con Red Hat OpenShift como CaaS (validación multi-vendor + GitOps, completado en 6 meses)
- **Escala:** 245+ millones de clientes móviles en Europa, una de las mayores nubes públicas soberanas de Europa

### 108. NGINX / F5
- **Lenguajes:** C (core NGINX entero — event-driven, no-blocking, single-threaded por worker), Lua via LuaJIT (OpenResty para scripting en fases de request), JavaScript (njs — NGINX JavaScript)
- **OSS creado:** NGINX core (nginx.org, open source); NGINX Unit (polyglot app server open source — Ruby, PHP, Python, Perl, Go, JS simultáneamente, hot-config-reload sin restart); NGINX Ingress Controller (Apache 2.0, 150+ contributors, reemplaza community ingress-nginx que se retira marzo 2026); OpenResty lua-nginx-module (embeds Lua en NGINX — originado en Taobao/Alibaba 2009)
- **Decisión notable:** F5 adquirió NGINX ($670M, 2019); NGINX powers ~34% de todos los websites globalmente; OpenResty es Cloudflare's historical edge scripting (Lua a escala masiva)
- **Escala:** Servidor web más deployado del mundo; OpenResty: 10k–1M+ conexiones/servidor

### 109. HAProxy
- **Lenguajes:** C (codebase completo — mínimo y rápido intencionalmente), Lua (extensibilidad via LuaJIT), Rust (haproxy-api-rs — bindings Rust para HAProxy 2.8+ Lua API, emergente)
- **OSS creado:** haproxy/haproxy (mirror de git.haproxy.org, L4/L7 load balancer); HAProxy Technologies (entidad comercial con HAProxy Enterprise)
- **Arquitectura:** Single-process, event-driven, non-blocking; 9+ algoritmos de balanceo; QUIC/HTTP3 experimental (HAProxy 2.6+); usado por GitHub (con Consul+Consul-Template+Kube Service Exporter), Stack Overflow, Reddit, Twitter/X, Bitbucket
- **Decisión notable:** GitHub usa HAProxy en front de millones de requests/día; sub-millisecond latency para L4/L7 proxying
- **Escala:** Powers algunos de los sitios de mayor tráfico en Internet

### 110. Envoy Proxy
- **Lenguajes:** C++ (core entero, zero-copy networking), Rust (extensiones via Proxy-Wasm ABI + proxy-wasm-rust-sdk), Go/AssemblyScript/TinyGo (también soportados para Wasm filters)
- **OSS creado:** envoyproxy/envoy (CNCF Graduated, junto a k8s y Prometheus — uno de tres), proxy-wasm-rust-sdk (Rust SDK para Wasm filters), envoyproxy/gateway (Kubernetes Gateway API sobre Envoy)
- **Arquitectura:** Sidecar model — intercepts all inbound/outbound traffic; "universal data plane" — service discovery, LB, TLS, circuit breaking, retries, observability; Wasm extensibility (Proxy-Wasm ABI) con Rust como lenguaje dominante; xDS API para configuración dinámica; data plane de Istio service mesh
- **Adoptado por:** Airbnb, Google, Stripe, Lyft, Twilio, Verizon, Netflix, Microsoft, Pinterest, Salesforce, Booking.com, eBay, Square
- **Escala:** Maneja tráfico de algunos de los mayores deployments de microservicios en el mundo; 3,000+ commits de organizaciones diversas

---

## Resumen Ejecutivo

### Empresas investigadas
| Grupo | Empresas | Cantidad |
|---|---|---|
| Big Tech | Google, Meta, Netflix, Uber, Airbnb, Twitter/X, Spotify, Microsoft, Apple, Amazon/AWS | 10 |
| Cloud & Infraestructura | Cloudflare, HashiCorp, Datadog, Grafana Labs, Elastic, MongoDB, Redis Labs, Confluent, Databricks, PlanetScale | 10 |
| Enterprise & Fintech Tradicional | Stripe, Shopify, Square/Block, PayPal, Bloomberg, Goldman Sachs, JPMorgan, Revolut, N26, SAP | 10 |
| AI & Emerging Tech | Anthropic, OpenAI, Mistral, Hugging Face, Scale AI, Vercel, Neon, Fly.io, Railway | 9 |
| E-commerce & Marketplace | Zalando, Etsy, eBay, Mercado Libre, Booking.com, Expedia, Grab, DoorDash, Instacart, Lyft | 10 |
| Gaming & Entertainment | Epic Games, Riot Games, Roblox, Discord, Twitch, Valve, King, Unity, EA, Zynga | 10 |
| Data & Analytics | Snowflake, dbt Labs, Fivetran, Airbyte, Starburst, ClickHouse, Amplitude, Mixpanel, Looker, Monte Carlo, Apache Flink | 11 |
| DevTools & Plataformas | GitHub, GitLab, Atlassian, Figma, Linear, Notion, Postman, Sentry, LaunchDarkly, PagerDuty | 10 |
| Seguridad | CrowdStrike, Palo Alto Networks, SentinelOne, Wiz, Snyk, Lacework, Orca Security, Tenable, 1Password, Tailscale | 10 |
| Fintech & Crypto | Coinbase, Binance, Robinhood, Plaid, Adyen, Wise, Nubank, Klarna, Affirm, Brex | 10 |
| Telecom, CDN & Proxies | Akamai, Fastly, DigitalOcean, Cloudinary, Twilio, SendGrid, Deutsche Telekom, NGINX/F5, HAProxy, Envoy | 10 |
| **TOTAL** | | **110** |

### Tendencias Cross-Industry Identificadas

**Rust adoptado en producción:**
Cloudflare (Pingora, Oxy), AWS (Firecracker, Bottlerocket), Neon (storage engine), Fly.io (edge proxy), HuggingFace (Safetensors, TGI router), Vercel (Turborepo, Turbopack), Redis (RedisJSON, RediSearch), Shopify (YJIT→Ruby), Block (LDK Bitcoin), CrowdStrike (ML inference), 1Password (crypto + sync engine), Palo Alto Networks (Cortex XDR agent), Grab (Counter Service), dbt Labs (fusion engine), Fastly (Compute platform, Lucet)

**eBPF para observabilidad zero-code:**
Datadog (NPM/USM), Grafana Beyla (traces+métricas RED), Elastic Agent, Cilium (networking+security), CrowdStrike (Linux sensor), SentinelOne (CWPP), Palo Alto Networks (Cortex XDR cloud)

**ClickHouse como OLAP de facto:**
Cloudflare, Sentry, Lacework, Amplitude, Mixpanel, PostHog, LangChain, CrowdStrike (downstream via FDR), DoorDash (Pinot complementario), Uber

**Apache Iceberg como formato universal:**
Snowflake (tablas Iceberg nativas), Trino (engine principal), Flink (sink), dbt-fusion (target), DoorDash (migración desde Delta Lake 2024)

**Lenguajes inusuales en producción:**
Nubank (Clojure+Datomic, adquirieron Cognitect), Brex (Elixir→Kotlin documentado), Klarna (Erlang/BEAM core), Roblox (Luau — su propio Lua tipado), PagerDuty (Elixir para stateful processing por-usuario)

**Kafka rechazado conscientemente:**
Revolut (→PostgreSQL event store: SQL-queryable, más fácil de auditar para fintech)

**Monolitos mantenidos intencionalmente:**
GitHub (Rails monolith + particionamiento DB), GitLab (Rails + servicios Go para hot paths), Shopify (Rails modular con Packwerk), Instacart (Rails + microservicios Go para nuevos dominios)
