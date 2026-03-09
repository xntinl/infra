# Agent Teams Research Prompt

Copia y pega esto en Claude Code:

---

Eres el líder de un equipo de investigación. Tu misión es investigar exhaustivamente
las herramientas más demandadas en el mercado laboral 2025-2026 para un ingeniero
de infraestructura y backend que trabaja con Rust, Go, Terraform, Kubernetes y AWS.

El objetivo final es actualizar el archivo `learning.md` con herramientas reales,
agrupadas y relevantes. No inventes herramientas — solo incluye las que tengan
demanda real en ofertas de trabajo DevOps, Platform Engineer, SRE, Backend Engineer.

Cada agente debe buscar en internet, leer documentación oficial, analizar ofertas
de trabajo reales y reportar al menos 15-20 herramientas por área con evidencia
de demanda laboral. No te limites a las sugeridas — explora en profundidad.

Toma como referencia principal las tecnologías usadas en producción por las empresas
de ingeniería más avanzadas del mundo:

- **Netflix** — chaos engineering, observabilidad, streaming a escala (Spinnaker, Chaos Monkey, Atlas, Mantis)
- **Google** — Kubernetes nació aquí, Borg, SRE culture, Borgmon → Prometheus, gRPC, Protocol Buffers
- **Microsoft** — Azure, KEDA, Dapr, .NET + Rust en sistemas críticos, GitHub Actions
- **Anthropic** — Rust, Python, infraestructura de ML a escala, seguridad y confiabilidad
- **Meta** — PyTorch, Thrift, Presto, Rocksdb, sistemas distribuidos a escala masiva
- **Uber** — Go, microservices, Cadence/Temporal workflows, Kafka, M3 metrics
- **Cloudflare** — Rust en edge, Workers, eBPF, Pingora (reemplazo de nginx en Rust)
- **AWS** — Firecracker (Rust), Bottlerocket OS, contribuciones a Linux kernel
- **SAP** — Kyma (k8s runtime), CAP framework, BTP, OpenTelemetry adoption empresarial
- **HashiCorp / IBM** — Terraform, Vault, Consul, Nomad — stack completo de infraestructura
- **Datadog** — observabilidad, agentes en Go y Rust, integración con todo el ecosistema

Para cada herramienta investigada, indica si es usada por alguna de estas empresas.

No modifiques ningún archivo durante la investigación. Solo el líder escribe al final.

---

## Agente 1 — Cloud Native Foundation & Kubernetes

Investiga en profundidad las herramientas más demandadas en el ecosistema CNCF y Kubernetes:

- Todos los proyectos Graduated e Incubating de la CNCF landscape con demanda real
- Herramientas de networking (CNI): Cilium, Calico, Flannel, Weave, Multus
- Storage (CSI): Rook/Ceph, Longhorn, OpenEBS, AWS EBS, NFS provisioner
- Service mesh: Istio, Linkerd, Consul Connect, Kuma
- Operadores más usados en producción: cert-manager, external-dns, cluster-autoscaler, KEDA
- GitOps: ArgoCD, Flux, Weave GitOps
- Admission control y seguridad: Gatekeeper, Kyverno, Falco, Tetragon
- Builds y registries: Kaniko, Buildah, Harbor, Crane
- Multi-cluster y plataforma: Crossplane, Cluster API, Rancher, OpenShift
- Developer platforms: Backstage, Port, Kratix
- Certificaciones más pedidas: CKA, CKS, CKAD — qué cubren y por qué importan

Busca en: landscape.cncf.io, ofertas LinkedIn "Kubernetes Platform Engineer", "SRE k8s"

---

## Agente 2 — Linux, Bash, Observability & Security

Investiga en profundidad herramientas de sistemas, observabilidad y seguridad:

- Diagnóstico Linux avanzado: eBPF, bpftrace, perf, flamegraphs, strace, ltrace, ftrace
- Gestión de procesos y recursos: systemd, cgroups v2, namespaces, ulimit, ionice
- Networking Linux: tc, nftables, conntrack, ss, iproute2, tcpdump, Wireshark
- Bash scripting avanzado: shellcheck, shfmt, bats (testing), bashdb
- Stack de observabilidad completo: OpenTelemetry, Prometheus, Grafana, Loki, Tempo, Mimir
- APM y tracing: Jaeger, Zipkin, Datadog, New Relic, Honeycomb
- Alerting: Alertmanager, PagerDuty, OpsGenie integrations
- Seguridad en runtime: Falco, Tetragon, Sysdig, AppArmor, SELinux, seccomp
- Vulnerability scanning: Trivy, Grype, Snyk, Clair, Anchore
- SAST/DAST: Semgrep, Bandit, SonarQube
- Secrets management: HashiCorp Vault, SOPS, age, External Secrets Operator, Infisical
- Networking seguro: WireGuard, Tailscale, Nebula, Cloudflare Tunnel
- Compliance y auditoría: OpenSCAP, Lynis, kube-bench, CIS benchmarks

Busca en: ofertas "SRE Linux", "Security Engineer DevOps", blog.cloudflare.com, netflixtechblog.com

---

## Agente 3 — Rust, Go & Backend Infrastructure

Investiga en profundidad el ecosistema de lenguajes para infraestructura y backend:

**Rust:**
- Web frameworks: axum, actix-web, warp, rocket — cuál lidera en producción
- Async runtime: tokio, async-std, smol
- Serialización: serde, prost (protobuf), flatbuffers
- Base de datos: sqlx, diesel, sea-orm, redis-rs
- Networking: hyper, reqwest, tonic (gRPC), rumqttc (MQTT)
- Observabilidad: tracing, opentelemetry-rust, metrics
- CLI: clap, argh, owo-colors, indicatif
- Testing: cargo-nextest, mockall, wiremock-rs, testcontainers-rs
- AWS SDK: aws-sdk-rust, lambda_http, lambda_runtime
- Seguridad: rustls, ring, argon2, jwt-simple

**Go:**
- Web: chi, gin, echo, fiber, gorilla/mux — comparativa de demanda laboral
- gRPC: google.golang.org/grpc, connectrpc
- Database: pgx, sqlc, ent, gorm
- CLI: cobra, bubbletea, lipgloss
- Testing: testify, gomock, testcontainers-go
- Observabilidad: opentelemetry-go, zap, zerolog, slog
- DI y arquitectura: wire, fx (Uber)
- Kubernetes operators: controller-runtime, kubebuilder, operator-sdk
- Infra tools escritos en Go que ingenieros usan: Terraform, kubectl, k9s, Helm, Hugo

**Patrones compartidos:**
- Protocol Buffers y gRPC — buf, protoc, grpc-gateway
- REST vs gRPC vs GraphQL en backends de infraestructura
- Patrones de testing para servicios distribuidos

Busca en: crates.io trending, pkg.go.dev popular, ofertas "Rust backend engineer", "Go platform engineer"

---

## Agente 4 — Terraform, IaC, CI/CD & Policy as Code

Investiga en profundidad el ecosistema de infraestructura como código y pipelines:

**Terraform / OpenTofu:**
- Providers más usados: AWS, GCP, Azure, Kubernetes, Helm, Vault, Datadog
- Módulos de la Terraform Registry más descargados
- Terragrunt — patrones DRY para módulos a escala
- Atlantis — automatización de PR con plan/apply
- OpenTofu — fork open source, compatibilidad y adopción
- Terraform Cloud / Spacelift / env0 — plataformas de gestión

**Testing de IaC:**
- Terratest — tests de integración en Go
- Checkov — análisis estático de seguridad
- tfsec / Trivy IaC — scanning de vulnerabilidades
- Infracost — estimación de costos antes de apply
- terraform-docs — documentación automática

**CI/CD:**
- GitHub Actions — actions más usadas en infraestructura
- Dagger — pipelines como código (Go/Rust/Python SDK)
- Earthly — builds reproducibles
- Tekton — pipelines nativos de k8s
- Jenkins (legacy) vs sistemas modernos
- Buildkite, CircleCI — demanda en empresas

**Policy as Code:**
- OPA + Rego — casos de uso reales: k8s admission, API authorization, Terraform
- Gatekeeper vs Kyverno — cuándo usar cada uno
- HashiCorp Sentinel — Terraform Cloud policy enforcement
- Conftest — testing de configuraciones con OPA

**GitOps y delivery:**
- ArgoCD — aplicaciones, app-of-apps, ApplicationSets
- Flux v2 — kustomize y helm controllers
- Crossplane — infraestructura como recursos de k8s

**Gestión de secrets en pipelines:**
- SOPS + age/GPG — encriptación de archivos
- External Secrets Operator — sincronización desde Vault/AWS/GCP
- Sealed Secrets — secrets encriptados en git

Busca en: ofertas "DevOps engineer Terraform", "Platform engineer GitOps", registry.terraform.io/browse/modules

---

## Instrucciones al líder al finalizar

Una vez que los 4 agentes terminen su investigación:

1. Consolida todos los hallazgos eliminando duplicados
2. Agrupa las herramientas por categoría con descripción corta (máx 10 palabras)
3. Marca con `*` las que aparecen frecuentemente en ofertas de trabajo reales
4. Ordena dentro de cada categoría de más a menos demandada
5. Escribe el resultado final actualizando `/Users/sentinel/Documents/projects/infra/learning.md`
6. Mantén el formato Markdown existente del archivo
7. No incluyas herramientas simples de productividad personal
8. Agrega una sección `## Certificaciones` con las más valoradas por área
