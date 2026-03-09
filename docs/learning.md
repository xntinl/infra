# Learning Roadmap

## Languages
- **Rust** — systems programming, memory safety without GC
- **Go** — simple, fast, great for CLIs and services

## Architecture
- **Hexagonal architecture** — ports & adapters, domain isolated from infrastructure
- **Event-driven** — async communication via events
- **Serverless** — functions as a service (Lambda)
- **Microservices** — small, independent services

## Infrastructure as Code
- **Terraform** — infrastructure provisioning as code
- **Ansible** — configuration management and server automation
- **Pulumi** — IaC using real programming languages (Go, Rust, Python)

## Containers & Orchestration
- **Docker** — containerization and image building
- **Kubernetes (k8s)** — container orchestration at scale
- **Helm** — k8s package manager and release management
- **Kustomize** — k8s config layering without templates
- **Cilium** — eBPF-based networking, security, and observability for k8s
- **Falco** — runtime threat detection for containers and k8s
- **Trivy** — vulnerability scanner for images, IaC, and repos
- **Kubescape** — k8s security posture and compliance

## Policy as Code
- **OPA** (Open Policy Agent) — general-purpose policy engine
- **Rego** — declarative language for writing OPA policies
- **Gatekeeper** — OPA admission controller for k8s
- **Kyverno** — k8s-native policy engine (YAML-based)

## CI/CD
- **GitHub Actions** — automated pipelines (build, test, deploy)
- **Earthly** — reproducible builds, Dockerfile-like syntax for CI
- **Dagger** — portable CI pipelines as code (Go/Rust SDK)
- **ArgoCD** — GitOps continuous delivery for k8s
- **just** — task runner for local workflows

## Observability
- **OpenTelemetry** — tracing, metrics, and logs standard
- **Prometheus** — metrics collection and alerting
- **Grafana** — dashboards and visualization
- **Jaeger** — distributed tracing UI
- **Loki** — log aggregation (Grafana stack)

## Networking & Security
- **TLS/mTLS** — encrypted and mutual-authenticated communication
- **HashiCorp Vault** — secrets management, dynamic credentials, PKI
- **WireGuard** — modern VPN protocol
- **Istio / Linkerd** — service mesh, traffic management, mTLS between services
- **Falco** — kernel-level threat detection via eBPF

## Linux & Server
- **systemd** — service lifecycle and process management
- **eBPF** — kernel-level observability, networking, and security
- **iptables / nftables** — firewall and packet filtering
- **cgroups / namespaces** — Linux primitives behind containers
- **strace / perf** — system call tracing and performance profiling

## Databases & Storage
- **PostgreSQL** — relational database, ACID compliance
- **PgBouncer** — connection pooler for PostgreSQL, reduces connection overhead at scale
- **Redis** — in-memory cache and pub/sub
- **CockroachDB** — distributed SQL, Postgres-compatible

## Messaging & Events
- **Kafka** — high-throughput distributed event streaming
- **NATS** — lightweight, cloud-native messaging and pub/sub

## AWS
- **Lambda** — serverless functions
- **API Gateway** — HTTP API management
- **Step Functions** — workflow orchestration
- **EventBridge** — event bus
- **SQS / SNS** — queues and pub/sub notifications
- **S3** — object storage
- **DynamoDB** — serverless NoSQL database
- **IAM** — identity and access management
- **VPC** — network isolation
- **SSM / Secrets Manager** — configuration and secrets
- **CloudWatch** — logs and metrics

## Rust Ecosystem
- **axum** — HTTP framework
- **tokio** — async runtime
- **serde** — serialization/deserialization
- **sqlx** — async SQL queries
- **tower** — middleware and service abstractions
- **tracing** — structured logging and spans
- **lambda_http** — AWS Lambda HTTP adapter

## Go Ecosystem
- **net/http** — standard HTTP library
- **chi / gin** — lightweight HTTP routers
- **cobra** — CLI framework
- **wire** — dependency injection

---

## Validado en Producción (Top Companies)

Herramientas que aparecen repetidamente en producción real, con evidencia de blogs y repos públicos.

### Orquestación & Runtime
- **Kubernetes** — de facto estándar; OpenAI lo escala a 7,500 nodos, Netflix lo usa para Titus (3M containers/semana), Shopify en GKE, N26 (230+ microservicios) _(Google, Netflix, Shopify, Uber, OpenAI, N26, SAP)_
- **Firecracker** (Rust, AWS) — microVM que arranca en <125ms, <5MiB overhead; powers AWS Lambda, AWS Fargate, Fly.io (cada app en su propio microVM), Stripe (sandboxing de builds reemplazando gVisor) _(AWS, Fly.io, Stripe)_
- **Temporal / Cadence** — orquestación de workflows fault-tolerant; Cadence fue creado en Uber (12B ejecuciones/mes), Temporal es el fork comercial por los creadores originales _(Uber, Netflix Conductor alternativa, ampliamente adoptado)_
- **Nomad** (Go, HashiCorp) — orquestador que soporta contenedores, binarios y VMs sin requerir containerización; validado a 10,000+ nodos _(HashiCorp clientes en producción)_

### Observabilidad
- **Prometheus** — estándar de métricas; inspirado por BorgMon de Google; Uber lo extiende con M3DB (6.6B series, 500M métricas/seg) _(Google, Uber, prácticamente todos)_
- **OpenTelemetry** — convergencia de OpenCensus (Google) + OpenTracing; Elastic fusionó ECS con OTel Semantic Conventions (2023); Grafana Beyla genera señales OTel nativamente _(Google, Elastic, Grafana, industria entera)_
- **eBPF para observabilidad zero-code** — Datadog (NPM, USM: detecta HTTP/gRPC inspeccionando syscalls sin reinicios), Grafana Beyla (traces y métricas RED sin tocar código), Elastic Agent _(Datadog, Grafana, Elastic, Cloudflare)_
- **Vector** (Rust, Datadog) — pipeline de datos de observabilidad; 86 MiB/s vs Logstash's 3.1 MiB/s; usuario más grande procesa 500 TB/día; 100k descargas/día _(Datadog, ampliamente adoptado)_
- **Grafana LGTM stack** — Loki (logs, Go), Tempo (traces, Go, solo necesita object storage), Mimir (métricas, Go, validado a 1B series), Pyroscope (profiling continuo); todos usan S3 como única capa de persistencia _(Grafana Labs, cientos de organizaciones)_
- **Jaeger** — tracing distribuido creado en Uber; CNCF graduated _(Uber, CNCF ecosystem)_

### Bases de Datos & Storage
- **Vitess** (Go, origen YouTube) — proxy de sharding MySQL transparente, connection pooling (miles de conexiones app → pool pequeño en MySQL), online DDL sin locks; usado en YouTube desde 2011, PlanetScale lo ofrece como producto managed, Shopify lo adoptó para escalar Shop app _(YouTube/Google, PlanetScale, Shopify, Block/Square)_
- **RocksDB** (C++, Meta) — KV store embebido optimizado para flash storage; producción en Meta, LinkedIn, Yahoo _(Meta, LinkedIn, Yahoo)_
- **Delta Lake** (Databricks) — ACID transactions sobre object storage (S3/GCS); arquitectura Lakehouse: confiabilidad de data warehouse sin lock-in propietario _(Databricks, ampliamente adoptado en data engineering)_
- **PostgreSQL como event store** — Revolut rechazó Kafka explícitamente por complejidad operacional; construyó streaming de eventos sobre PostgreSQL: SQL-queryable, auditaría más simple; N26 también usa PostgreSQL como base primaria _(Revolut, N26)_
- **MLflow** (Databricks) — lifecycle management de ML: tracking de experimentos, model registry, trazabilidad de LLMs; 24.6k stars _(Databricks, ampliamente adoptado en MLOps)_

### Networking & Proxies
- **gRPC** — creado en Google (derivado de Stubby); co-creado con Square; estándar en Uber, Stripe, Block _(Google, Square, Uber, Stripe, prácticamente todos en microservicios)_
- **Envoy** — L7 proxy y service mesh data plane; CNCF graduated; Stripe migró a Envoy para su service mesh _(Google, Stripe, CNCF ecosystem)_
- **Pingora** (Rust, Cloudflare) — reemplazo de nginx; ha manejado casi 1 quadrillón de requests en la red global de Cloudflare; Oxy es el framework de más alto nivel construido sobre él _(Cloudflare)_
- **WireGuard** — VPN moderno; Fly.io lo usa como backhaul inter-datacenter y para 6PN (IPv6 Private Networking entre apps, equivalente a VPC) _(Fly.io, Tailscale construido sobre él)_

### Developer Platforms & Build
- **Backstage** (Spotify) — portal interno de desarrolladores; gestiona 14,000+ componentes de software para 2,700+ ingenieros en Spotify; CNCF incubating; adoptado por cientos de empresas _(Spotify, CNCF, ampliamente adoptado)_
- **Bazel** (Google) — build system hermético y reproducible; derivado de Blaze interno; Stripe lo usa como sistema de builds unificado para Go, Java, TypeScript, Ruby, Python, Scala y Terraform _(Google, Stripe, Meta usa Buck2 que es su fork en Rust)_
- **Apache Airflow** (Airbnb) — orquestación de pipelines de datos como DAGs Python; creado en Airbnb 2014, donado a Apache; de facto estándar en data engineering _(Airbnb, prácticamente toda empresa con data pipelines)_
- **Apache Superset** (Airbnb) — BI y visualización de datos; creado en Airbnb, donado a Apache _(Airbnb, ampliamente adoptado)_

### Rust en Producción (casos documentados)
- **Cloudflare**: Pingora (proxy global), Oxy (Zero Trust Gateway, iCloud Private Relay)
- **AWS**: Firecracker (Lambda/Fargate), Bottlerocket (OS para contenedores)
- **Neon**: storage engine completo (Pageserver + Safekeeper con consenso Paxos)
- **Fly.io**: fly-proxy (proxy edge en cada servidor)
- **HuggingFace**: Safetensors (serialización de modelos, 13x más rápido que pickle), TGI router
- **Vercel**: Turborepo (migración documentada de Go a Rust), Turbopack (bundler)
- **Redis**: RedisJSON (core en Rust), RediSearch (23% Rust)
- **Shopify**: YJIT (JIT compiler para Ruby, contribuido upstream a CRuby 3.1)
- **Block/Square**: LDK (Lightning Development Kit para Bitcoin)
- **Datadog**: Vector (pipeline de observabilidad, adquirido)

### Go en Producción (infraestructura)
HashiCorp (Vault, Consul, Nomad, Boundary), Grafana Labs (Loki, Tempo, Mimir, Beyla), Datadog Agent, Vitess (PlanetScale/YouTube), Fly.io (LiteFS), Cloudflare (Quicksilver: 2.5T reads/día), Uber (Jaeger, Peloton)

### Herramientas que Empresas Abandonaron (y por qué)
- **Hystrix, Ribbon, Eureka** → service meshes (Envoy/Istio): Netflix los puso en maintenance mode ~2018; los service meshes reemplazaron la necesidad de resiliencia client-side _(Netflix)_
- **Heroic** (TSDB propio) → **VictoriaMetrics**: Spotify deprecó su TSDB interno por dificultad de mantenimiento y desalineación con el ecosistema Prometheus/OTel/Grafana _(Spotify)_
- **Luigi** → Apache Beam/Scio: Spotify migró internamente a pipelines sobre Beam _(Spotify)_
- **OpenCensus** → **OpenTelemetry**: Google deprecó su proyecto propio, fusionándolo con OpenTracing para crear el estándar CNCF _(Google)_
- **Kafka** (no adoptado) → **PostgreSQL event store**: Revolut rechazó Kafka conscientemente por complejidad operacional; los eventos sobre PostgreSQL son SQL-queryable y más fáciles de auditar _(Revolut)_
- **Go** → **Rust** (Turborepo): Vercel migró completamente, documentado en 3 posts; razón principal: CGO y FFI más limpio en Rust, compartir código con Turbopack _(Vercel)_
- **Rust** → **Go** (Railpack): Railway hizo la migración inversa para su nuevo builder; integración con BuildKit LLB ecosystem y tooling de Go más adecuado _(Railway)_
- **gVisor** → **Firecracker**: Stripe reemplazó gVisor por overhead inaceptable en filesystem para compilación Ruby/Java _(Stripe)_
