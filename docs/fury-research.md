# Fury IDP — Investigación Completa

> **Fuente**: Investigación de fuentes públicas — blog de ingeniería de MercadoLibre (Medium),
> AWS re:Invent 2024 (DOP328), QCon SF 2024, PlatformCon, Datadog case studies, go.dev, GitHub.
> Suficiente para entender el sistema funcionalmente y construir algo equivalente.

---

## 1. Origen e Historia

Fury nació en **2015** como respuesta al caos posterior a la transición ~2010 de un monolito Ruby on Rails a microservicios en MercadoLibre.

El monolito se volvió inmanejable al pasar los ~100 desarrolladores: *"source code conflicts, high coupling, and chaotic deployments resulting in frequent downtime became prevalent issues."* Los microservicios inicialmente ayudaron, pero *"productivity started to drop as the complexity of the requirements and microservices plus the lack of standardization caused teams to spend more time operating infra and resolving incidents."*

Fury emergió para *"catalog services, establish centralized and controlled development and deployment mechanisms, standardize technology stacks, and enforce security policies and cost controls."*

El equipo de ingeniería creció de ~200 personas (2011) a 15,000+ (2024).

---

## 2. Qué es Fury

**Fury** es el Internal Developer Platform (IDP) / PaaS de Mercado Libre, creado en 2015.
Actúa como capa de abstracción entre los desarrolladores y los cloud providers (AWS + GCP).

> "Fury is a software-as-a-service (SaaS) product, also written in Go — a platform-as-a-service tool
> for building, deploying, monitoring, and managing services in a cloud-agnostic way."
> — go.dev/solutions/mercadolibre

**Objetivos originales (2015):**
1. Ahorrar dinero (governance de infraestructura)
2. Mejorar gobernanza y control (catalog, ownership)
3. Mejorar seguridad (políticas centralizadas)

---

## 3. Escala (números más recientes, 2024-2025)

| Métrica | Valor |
|---------|-------|
| Desarrolladores atendidos | 16,000+ |
| Microservicios en producción | 30,000–35,000+ |
| Instancias de cómputo | 100,000–130,000+ |
| Clusters Kubernetes | 140 |
| Instancias de data services | 60,000+ |
| Bases de datos | 17,000 |
| Pods en producción | 200,000+ |
| Requests por segundo (pico) | 25–50 millones (900M req/min) |
| Deployments por día | 10,000 (2022) → 30,000–36,000 (2024-2025) |
| Config deployments por día | ~2,000 |
| PRs mergeados por día | ~100,000 |
| Repositorios en GitHub | 26,000+ |
| Logs diarios | 40 TB |
| Spans de trazas por minuto | 200 millones |
| Métricas por minuto | 250 millones |
| Reducción de costos vs EC2 | 30%+ |

---

## 4. Arquitectura General

```
┌──────────────────────────────────────────────────────────────────┐
│                     FURY CONTROL PLANE                           │
│                                                                  │
│   Developer ──→ [Web UI / CLI / APIs]                            │
│                        │                                         │
│              [Fury APIs / Control Plane]                         │
│                        │                                         │
│        ┌───────────────┼───────────────┐                         │
│        ▼               ▼               ▼                         │
│  [Compute Module] [Config Service] [Data Services ACL]           │
│        │                                                         │
│  [Serverless Module]                                             │
│        │                                                         │
│  [Cluster API*]  ←── *Propio de MeLi, no el estándar K8s        │
│        │                                                         │
│  [Ingress ALB]                                                   │
│        │                                                         │
├────────┼─────────────────────────────────────────────────────────┤
│        ▼          KUBERNETES (EKS / GKE)                         │
│  [ReplicaSets + HPAs]                                            │
│  [Pods con sidecars: observability, config, mesh]                │
│  [Envoy sidecars] ←── gestionados por Mesh Controller            │
└──────────────────────────────────────────────────────────────────┘
```

**Capas principales:**

| Capa | Descripción |
|------|-------------|
| **Developer Interface** | Web UI + CLI + APIs REST |
| **Fury Control Plane** | Orquesta todos los subsistemas |
| **Anti-Corruption Layer (ACL)** | Previene lock-in; devs nunca llaman directamente a AWS/GCP APIs |
| **Compute / Serverless Module** | Gestiona workloads en K8s |
| **Cluster API** (propio) | Traduce peticiones de Fury → primitivas K8s |
| **Mesh Controller** | Service mesh interno basado en Envoy (no Istio/Linkerd) |
| **Config Service** | Gestión de configuración con sidecars |
| **Data Services** | SQL, KV, search, queues, object storage — todo vía SDK |
| **Observability** | OTel + Datadog auto-wired en cada app |
| **Automatrix** | Plataforma de lifecycle de clusters K8s |

---

## 5. Modelo de Aplicaciones ("Apps" y "Scopes")

### Applications

La entidad core de Fury. Cada aplicación tiene:
- Un repositorio GitHub (auto-creado con permisos de equipo)
- Un equipo mantenedor
- Recursos de cómputo (si aplica)
- Data services y/o messaging asociados

**10 tipos de aplicaciones soportados:** web backend, frontend, mobile, libraries, ML models, microservicios, etc.

**11 lenguajes de programación** soportados. Los principales son **Go y Java**. También Node.js, Ruby, Python, Grails.

### Scopes

Los Scopes son la abstracción de entornos (environments) dentro de una aplicación.

> "Scopes serve as a means of managing application versions, deployments, and segmentation,
> representing a simplified version of Fury within cloud provider containers like AWS or GCP."

- Una aplicación puede tener múltiples scopes (dev, test, demo, prod, por región, por criticidad)
- Cada scope es **propiedad exclusiva de una aplicación**
- Al crear un scope, Fury auto-provisiona toda la infraestructura:
  load balancer, autoscaling, instancias, logs, métricas, alertas de monitoring
- La progresión estándar: `dev scope` → `test/demo scope` → `production scope`

---

## 6. Flujo del Desarrollador (NoOps en 4 pasos)

El modelo **NoOps** significa que los desarrolladores se enfocan 100% en lógica de negocio.
No escriben YAML de Kubernetes, no configuran AWS, no tocan redes.

### Paso 1: Crear la aplicación (UI o CLI)
- Desarrollador selecciona lenguaje/framework de una lista aprobada
- Fury auto-genera:
  - Repositorio GitHub con código scaffolded
  - Docker image configurada
  - Pipeline CI/CD
  - Permisos del equipo
- Todo listo "en pocos minutos"

### Paso 2: Configurar el web scope
- Desarrollador selecciona (con sugerencias automáticas de la plataforma):
  - Estrategia de autoscaling
  - Infraestructura (AWS vs GCP, región)
  - Estrategia de tráfico (blue-green, canary, etc.)
  - Logging
- Fury auto-provisiona toda la infraestructura del scope

### Paso 3: Abrir un Pull Request
- PR abierto dispara el **Release Process** automáticamente
- 7 validaciones de calidad corren en paralelo sobre los 26,000+ repos:
  - Dependencias
  - Branching model
  - CI/CD configuration
  - Code coverage
  - Credentials hardcodeadas (via GitHub Advanced Security)
  - Otros checks de calidad
- **Si cualquier check falla, el flujo completo se bloquea**
- Al merge del PR → `fury-core-ci` GitHub Action crea un tag semántico automático:
  - Branch `release/*` → bump de versión **major**
  - Branch `feature/*` → bump de versión **minor**
  - Branch `fix/*` → bump de versión **patch**
  - Comentar `#tag` en el PR → crea release candidate

### Paso 4: Deploy y monitoreo
- Desarrollador elige la estrategia de deployment desde el Fury UI o CLI
- Monitorea el deploy en dashboards de Fury (métricas, logs, estado del rollout)
- La plataforma recomienda automáticamente si continuar o hacer rollback

---

## 7. El Compute Module — Cómo Corren los Workloads

### Evolución histórica

**Standard Module (2015–~2020):** Basado en EC2 + GCE. Relación 1:1 instancia por réplica.
MercadoLibre construyó autoscaling propio (sin AWS ASGs) para soportar multi-cloud.
Baja eficiencia. Migración a Kubernetes resultó en **30% de reducción de costos**.

**Serverless Module (actual):** Basado en Kubernetes (EKS + GKE).
Los desarrolladores no saben que existe Kubernetes. El nombre "serverless" refleja que no
requieren conocimiento de servidores, no que usen Lambda/Functions.

### Flujo de un deployment (Serverless Module)

```
Developer trigger (UI/CLI)
    │
    ▼
Fury Serverless Module
    │
    ▼
Cluster API (propio) via Ingress ALB
    │
    ▼
Kubernetes Control Plane
    │
    ├── ReplicaSets (no Deployments — Fury ya gestiona las estrategias)
    └── HPAs (Horizontal Pod Autoscalers)
         │
         ▼
    Pods corriendo en el cluster
         │
         ▼
    Serverless API Controller (Kubernetes controller custom)
    ← observa pods/nodos, notifica al Serverless Module en tiempo real
         │
         ▼
    Little Monster (LM)
    ← monitoring continuo, maneja requests asíncronos, garantiza atomicidad
```

**Por qué ReplicaSets y no Deployments:**
> "MercadoLibre chose to use ReplicaSets instead of Kubernetes Deployments because their goal
> is to ensure the proper number of replicas without needing additional resources provided by
> Deployments, as such strategies were already part of the platform."

Las estrategias de deployment (blue-green, canary, etc.) son implementadas por la plataforma,
no por el Deployment de Kubernetes.

### Networking de Pods

> "Every pod inside Fury's clusters has unique and routable IPs from the VPC instead of VIPs
> (Virtual IP), which allows each pod to have direct network visibility, facilitating communication
> with other services and reducing the need for entry points per cluster."

### Autoscaling

3 modos implementados internamente (sin usar AWS ASGs):
1. **Predictive CPU scaling** — anticipa carga antes de que llegue
2. **Vertical scaling** — ajusta recursos de pods en ejecución
3. **Z-scaling** — dimensión custom de Fury (detalles no públicos)

A nivel de nodos: **Karpenter** (reemplaza Cluster Autoscaler basado en ASGs).

### Cluster Management — Automatrix

Plataforma interna para lifecycle de clusters K8s:
- Usa **CDKTF** (Cloud Development Kit for Terraform) + **CDK8s** para templates híbridos
- **GitOps nativo**: cada cambio en un cluster genera un PR que se valida antes de aplicar
- Clusters production-ready en minutos con seguridad y compliance incorporados
- Karpenter deployado vía este pipeline GitOps para consolidación continua de nodos
- 140 clusters segmentados por: región, provider, criticidad, tipo de workload

---

## 8. Service Mesh — Mesh Controller (Custom)

MercadoLibre **no usa Istio, Linkerd ni ningún service mesh de terceros**.

Construyeron el **Mesh Controller**:

```
Control Plane (Fury) detecta cambio de deployment
    │
    ▼
Mesh Controller sincroniza estado
    │
    ▼
Notifica al Control Plane API (Service Mesh)
    │
    ▼
Actualiza todos los Envoy sidecars de las aplicaciones
    │
    ▼
Envoy ajusta rutas → redirige tráfico a la nueva versión (candidate)
```

**Por qué lo construyeron custom:**
Cloud Run fue evaluado pero "didn't support sidecar containers at the time, which are central
to their system's design." Los sidecars son fundamentales para su arquitectura de observabilidad
y routing.

---

## 9. Estrategias de Deployment

| Estrategia | Descripción |
|------------|-------------|
| **All-in** | Reemplazo completo y directo |
| **Blue-Green** | Workload nuevo (Green) se crea en paralelo al viejo (Blue); Mesh Controller migra tráfico gradualmente |
| **Canary** | Shift de tráfico gradual por porcentaje |
| **Safe** | Como Blue-Green pero **más gradual**; monitoreo continuo de métricas; **rollback automático** si detecta anomalías |
| **Migration** | Mueve workloads entre providers (AWS↔GCP), regiones, o compute types (Standard↔Serverless) |

### Blue-Green en detalle

1. Se crea el nuevo workload (Green): Cluster API provisiona nuevos ReplicaSets + HPAs
2. Mesh Controller detecta el evento
3. Mesh Controller actualiza Envoy sidecars: rutas ajustadas → tráfico empieza a fluir a Green
4. Shift es progresivo hasta 100% en Green
5. Blue se termina

---

## 10. Configuración (Config Service)

Implementa Twelve-Factor App Factor III (separar config del código), con modificaciones:

- Usa **archivos de configuración** (no env vars) para manejar settings complejos
- Un **sidecar container** inicializa el entorno: descarga config files de un **storage bucket**
  y los guarda en un **shared volume** accesible al app container
- Apps arrancan sin necesidad de reconectar al Config Service (resilience pattern)
- Actualizaciones de config **sin reiniciar la aplicación**
- **AI agent** integrado: recomienda best practices (no guardar secrets, detectar duplicados,
  alertar sobre cambios repentinos en valores críticos)
- GitHub Advanced Security + secret scanning en todos los PRs
- **~2,000 config deployments por día**

---

## 11. Data Services (PaaS Catalog)

Fury expone managed services vía SDK. Los desarrolladores nunca llaman directamente a AWS/GCP.

**Catálogo (según fuentes públicas):**
- SQL databases
- Key-value stores (cache/Redis-like)
- Document databases
- Full-text search
- Messaging/queues
- Secrets storage
- Object storage
- Audit logs

**Números:**
- 17,000 bases de datos
- 60,000+ instancias de data services

**SDKs por lenguaje:**
- **go-meli-toolkit** (`go-core` + `go-platform`): bootstraps web servers, inicializa OTel,
  circuit breakers, HTTP clients con retries, tracing, métricas automáticas
- SDK Java
- SDK Python (parcialmente deprecado en versiones externas)
- **Fury Streams** (Java): streaming interno; 170 GB de datos de 35M req/min en tiempo real;
  migrado a Java 21 virtual threads → 50% menos platform threads, 20% menos memoria, 15% menos CPU

---

## 12. Observabilidad

### Auto-wiring

Cada nueva aplicación en Fury **tiene monitoring desde el primer minuto**, sin configuración manual.

> "Observability is embedded directly into the company's developer platform, FURY, ensuring that
> every new application includes monitoring from the start. Developers can define custom metrics,
> map critical flows, and track business-relevant signals without extra effort."

### Los 4 pilares en Fury

| Pilar | Implementación |
|-------|---------------|
| **Métricas** | Custom metrics via SDK + Datadog dashboards automáticos |
| **Logs** | 40 TB/día, herramienta central de troubleshooting |
| **Distributed Tracing** | OpenTelemetry + Envoy (zero-code instrumentation) |
| **Profiling** | Ad-hoc y continuous profiling disponibles |

### Distributed Tracing via Envoy (zero-code)

> "Thanks to their service mesh architecture at Mercado Libre, they instrumented distributed
> tracing with Envoy, preventing users from having to manually instrument their code. This allows
> them to treat each incoming request to a service as a span, enabling seamless instrumentation
> of the entire trace without requiring any code changes."

Cobertura: desde el backend layer hasta database query executions.

### OpenTelemetry como estándar

> "One of the most important decisions made was to focus efforts on standardizing telemetry data
> by adopting OpenTelemetry... taking ownership of the data generated to mitigate vendor lock-in
> risks, enhancing flexibility and extensibility for data manipulation and correlation."

### Datadog como backend

- **Serverless Monitoring, Infrastructure Monitoring, Container Monitoring**
- **Custom Metrics y Dashboards** (visibilidad por servicio, región, path)
- **Anomaly Detection ML**: identifica patrones irregulares automáticamente
- Semanas antes de Black Friday: análisis histórico + simulación de demanda para calibrar scaling
- Black Friday 2024: 900M req/min con 40% más carga, 100% uptime, cero incidentes críticos

---

## 13. Developer Portal / Fury UI

Características conocidas del portal web:
- **Service catalog**: inventario de todas las apps con ownership y dependencias
- **Deploy dashboard**: estado de deployments en tiempo real
- **Cost dashboards**: cuánto gasta cada app/team (gasto vs desperdicio)
- **Cost estimates**: comparativa de costos entre estrategias de infraestructura
- **Performance recommendations**: sugerencias automáticas aplicables sin downtime
- **Metrics y logs**: integrados directamente en la vista de la app
- **Scope management**: creación y configuración de scopes desde el portal
- **Deployment strategy selector**: UI para elegir All-in, Blue-Green, Canary, Safe, Migration

---

## 14. CLI de Fury

El Fury CLI permite:
- Descargar la aplicación localmente
- Correrla y testearla localmente
- Crear versiones listas para deploy desde la terminal
- Configurar el laptop para interactuar con la plataforma

Los comandos exactos no están documentados públicamente. Inferibles:
- `fury create [app-name]` — crea nueva aplicación
- `fury deploy` — despliega versión a un scope
- `fury scope create` — crea nuevo scope
- `fury logs` — ver logs de la aplicación
- `fury status` — estado de la aplicación

---

## 15. Gobernanza y Multi-tenancy

**16,000 desarrolladores** gestionados con 4 dimensiones de gobernanza (Chin Dou, SRE MeLi):

1. **IT Asset Management (ITAM)**: visibilidad de todo lo que está operacional en la plataforma.
2. **Software Delivery Supply Chain (SDSC)**: supervisión del proceso completo desde source code hasta producción — quality gates, optimización de costos, change freezes, frictions por error budgets, updates a escala, automated PRs.
3. **Runtime Management**: observabilidad en tiempo real e intervenciones centralizadas — inyectar comportamientos y gestionar performance dinámicamente.
4. **Self-serve data environment**: acceso a datos sin tickets de ops.

> "Fury's strength lies in the observability it offers through the entire process, which allows
> measurement of two critical aspects of how software is delivered: quality/stability and velocity."

**Filosofía: "Controlled Freedom"**

> "Having complete freedom versus controlled freedom is an eternal trade-off that platform
> engineering must deal with."

Fury resuelve esto dejando a los desarrolladores elegir su estrategia de deployment y scope,
mientras la plataforma enforcea seguridad, costos y gobernanza centralmente.

**Off-Fury (~20%)**

~20% del infra se crea fuera de Fury (tecnologías no soportadas, usando IaC directamente).
*"We have developed Infrastructure as Code (IaC) based tools to handle this reality, maintain
excellent compliance, and understand user activities, preventing leaks, deviations, or
vulnerabilities."* Si algún servicio se usa off-platform frecuentemente, se prioriza su
implementación en Fury.

**Respuesta rápida a incidentes de seguridad:**
El log4j incident y cambios regulatorios de gobierno se implementaron platform-wide desde Fury,
sin necesitar coordinación manual con los 16,000 desarrolladores.

**Multi-cloud resilience:**
MercadoLibre permaneció online durante un **major Google Cloud outage** gracias a la estrategia
multi-cloud habilitada por la capa de abstracción de Fury.

---

## 16. Onboarding y Developer Experience

**Filosofía de adopción:** *"We did not impose the platform on users because we believed the
benefits offered would lead developers to use it voluntarily."*

La adopción fue orgánica: primero probaron con microservicios críticos para demostrar que
funcionaban bien, cambiando el mindset de los desarrolladores. Uno de los primeros quick wins:
reducir el setup del entorno de desarrollo de días a **"a few minutes."**

**"Más de 30 servicios self-service"** disponibles en Fury (citado en re:Invent 2024 DOP328).

**Infraestructura de soporte:**
- **Boot camps**: para aprender Fury desde cero
- **Acceleration tracks**: capacitaciones en capacidades específicas
- **Knowledge exchange events**: eventos de intercambio entre equipos
- **Subject Matter Experts (SMEs)**: soporte desde uso básico hasta diseño de arquitectura,
  selección del mejor servicio para un problema, o performance tuning
- **DevRel communities**: promueven recursos de la plataforma via foros y encuestas
- **Technical Account Managers**: para equipos más grandes

**Unified DevEx:**
Independientemente de si un desarrollador usa gitflow o trunk-based development, Java o Go,
la experiencia de Fury es unificada. *"Achieving a Unified DevEx across all teams."*

---

## 17. Equipo de Fury

- **~1,000 ingenieros** dedicados a construir y mantener Fury
- Equivale al **7% del total de ingeniería** de MercadoLibre
- Liderado por **Lucia Brizuela** (Sr. Tech Director, Platform & Knowledge Management; en MeLi desde 2009)

**Personas clave públicas:**

| Persona | Rol | Contribución pública |
|---------|-----|---------------------|
| Lucia Brizuela | Sr. Tech Director | QCon SF 2022, Humanitec webinar |
| Juliano Marcos Martins | Technical Manager, Cloud & Platform | QCon SF 2024, artículos Medium sobre K8s |
| Marcelo Quadros | Software Expert | QCon SF 2024, Komodor podcast |
| Chin Dou | Site Reliability Engineer | "The True Power of an IDP" (Medium) |
| Javi Cardoso | Platform team | "Safe but flexible" (Medium) |
| Nilson Hiraoka | Platform team | "Twelve-Factor App in Fury" (Medium) |
| Jordan Montielo | Platform team | "Fury Streams Virtual Threads" (Medium) |

---

## 18. ML Platform — Fury Data Apps (FDA)

Sub-plataforma de ML construida encima de Fury:
- **700+ modelos ML** entrenados diariamente
- **20,000 tasks/día** (orquestación de pipelines)
- **270,000 job executions/día** para data pipelines
- **1 millón de data artifacts** en el catálogo de datos

---

## 19. Stack Tecnológico de Fury (Internals)

| Componente | Tecnología |
|------------|-----------|
| **Fury core** | Go (confirmado: "also written in Go" — go.dev) |
| **SDK principal** | go-meli-toolkit (Go) |
| **Streaming** | Java (Fury Streams, ahora con virtual threads de Java 21) |
| **Test helpers** | Ruby (fury-little_monster-gem) |
| **CI/CD actions** | TypeScript (fury-core-ci GitHub Actions) |
| **Cluster provisioning** | CDKTF + CDK8s (en Automatrix) |
| **Service mesh** | Envoy sidecars + Mesh Controller custom |
| **K8s primitivas** | ReplicaSets + HPAs (no Deployments) |
| **Trazabilidad** | OpenTelemetry |
| **Observability backend** | Datadog |
| **Node autoscaling** | Karpenter |
| **Source control** | GitHub + GitHub Advanced Security |
| **Container runtime** | Docker (desde creación de app) |
| **Multi-cloud K8s** | EKS (AWS) + GKE (GCP) |
| **Cluster lifecycle** | Automatrix (CDKTF + CDK8s + GitOps) |

---

## 20. Build vs. Buy — Por Qué Construyeron Fury

Fury fue construido en 2015, **antes de que existiera Backstage** (Spotify lo publicó en 2020).

El razonamiento implícito de sus publicaciones:
- La escala única (15,000+ engineers, 30,000+ microservices, multi-cloud) demandaba integración profunda imposible con herramientas externas
- Requisitos específicos: ACL multi-cloud, autoscaling K8s custom, DevEx unificado para 11 lenguajes y 10 tipos de apps, gobernanza del SDLC completo
- *"Building a capable platform is one of the best options for striking a balance between flexibility and standardization."*
- *"We cannot imagine achieving the same level of growth without it."*

**Hoy (2024+):** Si se construyera desde cero, usar **Backstage como base del developer portal**
sería razonable — cubre service catalog, software templates, y plugin system.
El compute engine, deployment strategies y mesh controller seguirían siendo custom.

---

## 21. Repositorios Públicos de Fury

| Repo | Descripción |
|------|-------------|
| [fury-core-ci](https://github.com/mercadolibre/fury-core-ci) | GitHub Actions para semantic versioning automático (TypeScript) |
| [fury-little_monster-gem](https://github.com/mercadolibre/fury-little_monster-gem) | RSpec helpers para testing de job-based architectures (Ruby) |
| [fury_mobile-ios-ui](https://github.com/mercadolibre/fury_mobile-ios-ui) | iOS UI component library |

El core de Fury **no es open-source** — es un sistema interno propietario.

---

## 22. Lo Que No Es Público

| Aspecto | Estado |
|---------|--------|
| Sintaxis exacta del CLI | No documentada |
| Schema del "app manifest" (equivalente a Dockerfile/Helm) | No publicado |
| Catálogo exacto de data services | Parcialmente conocido |
| Mecanismo de multi-tenancy en clusters (namespaces, etc.) | No detallado |
| Z-scaling — definición exacta | No publicada |
| Secrets management (Vault? AWS Secrets Manager? Propio?) | No detallado |
| Si el deploy a prod es GitOps automático o siempre manual | Indica que es developer-triggered |

---

## 23. Cómo Construir Algo Equivalente

### Componentes Core a Implementar

```
1. Control Plane API
   └── REST/gRPC API que acepta: create app, create scope, deploy, rollback
   └── State management (qué corre dónde, con qué config)

2. Application Registry (Service Catalog)
   └── Catálogo de apps con ownership, lenguaje, dependencias, scopes
   └── API + UI para explorar y gestionar

3. Scope Manager
   └── Abstracción de "environment" con infraestructura auto-provisionada
   └── Mapeo Scope → K8s Namespace + cloud resources

4. Compute Abstraction (el "Serverless Module")
   └── Traduce "deploy versión X a scope Y" → ReplicaSet + HPA en K8s
   └── Custom controller K8s (como el Serverless API Controller)
   └── Elimina Deployments nativos, gestiona estrategias en la plataforma

5. Deployment Engine
   └── Blue-Green: provisiona Green, Mesh Controller migra tráfico, termina Blue
   └── Canary: shift gradual de % de tráfico
   └── Safe: igual que Blue-Green + metric-based auto-rollback

6. Mesh Controller
   └── Controller que gestiona Envoy sidecars en todos los pods
   └── Actualiza routing rules según deployment strategy activa
   └── Evento-driven: responde al Control Plane

7. Config Service
   └── API de configuración + init sidecar container
   └── Sidecar descarga config del storage bucket al shared volume
   └── Actualizaciones sin restart

8. Observability Auto-wiring
   └── OTel collector sidecar en cada pod (zero-code instrumentation)
   └── Envoy reporta traces automáticamente
   └── Dashboard por app auto-generado

9. CI/CD Release Process
   └── GitHub App que valida PRs (7 checks)
   └── Semantic versioning automático por branch naming
   └── Bloqueo de merge si checks fallan

10. Developer Portal (UI)
    └── Service catalog + deploy UI + cost dashboards
    └── Deployment strategy selector + monitoring integrado

11. Cluster Manager (Automatrix simplificado)
    └── GitOps para lifecycle de clusters K8s
    └── CDKTF o Terraform + Helm para provisioning
    └── Karpenter para node autoscaling

12. Data Services Abstraction
    └── ACL / SDK que wrappea managed services (RDS, ElastiCache, etc.)
    └── Provisioning automático al asociar un servicio a un scope
```

### Stack Recomendado para Implementar

| Componente | Tecnología recomendada |
|------------|----------------------|
| Control Plane | Go + gRPC/REST |
| K8s controllers | Go + controller-runtime |
| Service mesh | Envoy + xDS API custom |
| Config sidecar | Go (init container) |
| OTel collector | OpenTelemetry Collector (YAML config) |
| CI/CD | GitHub Actions (TypeScript o Go) |
| Developer Portal | Next.js o Backstage como base |
| Cluster provisioning | CDKTF o Terraform + ArgoCD (GitOps) |
| Node autoscaling | Karpenter |
| Observability backend | Grafana LGTM stack o Datadog |
| Source of truth | PostgreSQL o etcd |

### Alternativa: Construir sobre Backstage

Backstage (Spotify) es open-source y cubre:
- Service catalog con ownership
- Developer portal extensible
- Software templates (equivalente al scaffolding de Fury)
- Plugin system para CI/CD, observabilidad, cloud costs

Fury está construido custom porque Backstage no existía en 2015.
Hoy sería razonable **usar Backstage como developer portal** y construir el
compute/deployment engine custom encima.

---

## 24. Fuentes Primarias

| Recurso | URL |
|---------|-----|
| MercadoLibre Tech Blog (Medium) | https://medium.com/mercadolibre-tech |
| Todos los artículos de Fury | https://medium.com/mercadolibre-tech/tagged/fury |
| Technological evolution (Feb 2024) | https://medium.com/mercadolibre-tech/the-technological-evolution-at-mercado-libre-fb269776a4e8 |
| Kubernetes at MercadoLibre (Nov 2024) | https://medium.com/mercadolibre-tech/kubernetes-at-mercado-libre-ec331bea1866 |
| How Kubernetes became right fit (Mar 2025) | https://medium.com/mercadolibre-tech/how-kubernetes-became-the-right-fit-for-mercado-libres-internal-developer-platform-fb02df289def |
| Karpenter + GitOps (Oct 2025) | https://medium.com/mercadolibre-tech/scaling-kubernetes-at-mercado-libre-with-karpenter-and-gitops-2c792a7403c5 |
| 30,000 deployments/day (Oct 2025) | https://medium.com/mercadolibre-tech/30-000-deployments-per-day-heres-how-we-operate-without-losing-our-minds-0eddc0480fb9 |
| Config Service + 12-Factor | https://medium.com/mercadolibre-tech/the-twelve-factor-app-in-practice-managing-app-configurations-in-fury-62d032a23078 |
| DevEx code management culture | https://medium.com/mercadolibre-tech/safe-but-flexible-the-devex-based-code-management-culture-at-mercado-libre-9b1dfde6f1b6 |
| Observability ecosystem (Dec 2024) | https://medium.com/mercadolibre-tech/building-a-large-scale-observability-ecosystem-1edf654b249e |
| OpenTelemetry tracing | https://medium.com/mercadolibre-tech/enabling-opentelemetry-based-distributed-tracing-ba276ad2523a |
| Fury Streams virtual threads | https://medium.com/mercadolibre-tech/migrating-mercado-libres-fury-streams-to-virtual-threads-1dabe01291ee |
| The True Power of an IDP | https://medium.com/mercadolibre-tech/the-true-power-of-an-internal-developer-platform-ade8e88626f4 |
| Go at MercadoLibre (go.dev) | https://go.dev/solutions/mercadolibre |
| AWS Case Study (30% cost reduction) | https://aws.amazon.com/solutions/case-studies/mercado_livre_fury/ |
| Datadog Case Study (Black Friday) | https://www.datadoghq.com/case-studies/mercado-libre/ |
| AWS re:Invent 2024 DOP328 (AntStack) | https://www.antstack.com/talks/reinvent24/how-mercado-libre-engineers-achieve-a-noops-experience-with-amazon-eks-dop328/ |
| QCon SF 2024 | https://qconsf.com/presentation/nov2024/scaling-innovation-noops-how-mercado-libre-manages-30000-microservices-and-25 |
| PlatformEngineering.org — IDP journey | https://platformengineering.org/blog/unveiling-the-secrets-of-a-successful-journey-mercado-libres-internal-developer-platform |
| PlatformEngineering.org — Scaling talk | https://platformengineering.org/talks-library/scaling-from-2k-engineers-to-12k-engineers-and-10k-deploys-per-day |
| Humanitec webinar | https://humanitec.com/events/scaling-from-2k-to-12k-engineers-and-10k-deploys-per-day |
| fury-core-ci (GitHub Actions, TypeScript) | https://github.com/mercadolibre/fury-core-ci |
| fury-little_monster-gem (Ruby test) | https://github.com/mercadolibre/fury-little_monster-gem |
| MercadoLibre GitHub customer story | https://github.com/customer-stories/mercado-libre |
| Fury at DevOps Conf 2015 (original slides) | https://www.slideshare.net/slideshow/fury-devops-conf-1/55267222 |
