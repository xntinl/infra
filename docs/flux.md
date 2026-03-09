# Flux

## Qué es Flux

Flux es una herramienta de GitOps para Kubernetes. Sincroniza automáticamente el estado del clúster con lo que está definido en un repositorio Git, de forma que Git actúa como la única fuente de verdad del sistema.

- Cualquier cambio en Git se aplica automáticamente al clúster
- Cualquier desviación del estado del clúster respecto a Git se corrige sola
- No hay `kubectl apply` manual en producción

## Arquitectura

Flux está compuesto por varios controladores que se ejecutan dentro del clúster:

### source-controller
Observa fuentes externas (repositorios Git, registros OCI, buckets S3, charts Helm) y descarga los artefactos. Es el punto de entrada de todos los recursos.

### kustomize-controller
Aplica manifiestos Kubernetes usando Kustomize. Lee los artefactos descargados por source-controller y los reconcilia con el estado del clúster.

### helm-controller
Gestiona releases de Helm dentro del clúster. Permite declarar `HelmRelease` como objetos Kubernetes y Flux se encarga del ciclo de vida completo (install, upgrade, rollback).

### notification-controller
Envía alertas y webhooks cuando ocurren eventos de reconciliación (Slack, Teams, correo, etc.). También recibe webhooks entrantes para disparar reconciliaciones inmediatas desde CI.

```
Git / OCI / Helm repo
        │
        ▼
source-controller  ─────► artefacto local
        │
        ├──► kustomize-controller  ──► kubectl apply
        │
        └──► helm-controller       ──► helm upgrade
                                        │
notification-controller ◄───────────────┘
```

## Flujo GitOps

1. El desarrollador hace push de un cambio (manifiestos, values de Helm, imagen tag) a Git
2. source-controller detecta el cambio y descarga el commit/chart nuevo
3. kustomize-controller o helm-controller calculan el diff y aplican los cambios al clúster
4. notification-controller emite un evento al canal de alertas configurado
5. Si el estado del clúster se desvía de Git (alguien aplica algo manual), Flux lo revertirá en el próximo ciclo de reconciliación

El intervalo de reconciliación es configurable (por defecto 1 min para Git, 5 min para Helm).

## Multi-ambiente (dev → qa → uat → prod)

### Estructura de repositorio recomendada

```
clusters/
  dev/
    flux-system/          # bootstrap de Flux en dev
    apps.yaml             # apunta al overlay dev de /apps
  qa/
    flux-system/
    apps.yaml
  uat/
    flux-system/
    apps.yaml
  prod/
    flux-system/
    apps.yaml

apps/
  base/                   # manifiestos base compartidos
    my-app/
      deployment.yaml
      service.yaml
      kustomization.yaml
  overlays/
    dev/
      kustomization.yaml  # patches para dev (réplicas, resources, imágenes)
    qa/
      kustomization.yaml
    uat/
      kustomization.yaml
    prod/
      kustomization.yaml
```

### Promoción entre ambientes

La promoción se hace mediante Pull Request: el tag de imagen o el valor que quieras promover se actualiza en el overlay del ambiente destino, se revisa, y al hacer merge Flux lo aplica automáticamente.

Flux Image Automation puede automatizar la actualización del tag en Git cuando se publica una imagen nueva, siguiendo políticas semver o de prefijo de tag.

## Integración con Helm

Se declara un `HelmRepository` (fuente) y un `HelmRelease` (release):

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: bitnami
  namespace: flux-system
spec:
  interval: 1h
  url: https://charts.bitnami.com/bitnami
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: redis
  namespace: default
spec:
  interval: 5m
  chart:
    spec:
      chart: redis
      version: ">=18.0.0 <19.0.0"
      sourceRef:
        kind: HelmRepository
        name: bitnami
        namespace: flux-system
  values:
    auth:
      enabled: false
    replica:
      replicaCount: 1
```

Los `values` se pueden sobreescribir por ambiente usando `valuesFrom` (referenciando un ConfigMap o Secret).

## Integración con Kustomize

Se declara un `GitRepository` (fuente) y una `Kustomization` (qué aplicar y desde dónde):

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: GitRepository
metadata:
  name: infra-repo
  namespace: flux-system
spec:
  interval: 1m
  url: https://github.com/org/infra
  ref:
    branch: main
---
apiVersion: kustomize.toolkit.fluxcd.io/v1
kind: Kustomization
metadata:
  name: apps-dev
  namespace: flux-system
spec:
  interval: 5m
  path: ./apps/overlays/dev
  prune: true          # elimina recursos que ya no están en Git
  sourceRef:
    kind: GitRepository
    name: infra-repo
  targetNamespace: dev
```

`prune: true` es clave: si se elimina un manifiesto de Git, Flux borra el recurso del clúster.

## Comandos principales

```bash
# Bootstrap: instala Flux en el clúster y crea el repo de configuración en GitHub
flux bootstrap github \
  --owner=org \
  --repository=fleet \
  --branch=main \
  --path=clusters/dev \
  --personal

# Ver el estado de todos los recursos de Flux
flux get all

# Ver solo fuentes (GitRepository, HelmRepository)
flux get sources git
flux get sources helm

# Ver kustomizations
flux get kustomizations

# Ver helm releases
flux get helmreleases

# Forzar reconciliación inmediata (sin esperar al intervalo)
flux reconcile source git infra-repo
flux reconcile kustomization apps-dev

# Reconciliar un HelmRelease con su chart remoto
flux reconcile helmrelease redis --with-source

# Ver logs de los controladores
flux logs --all-namespaces

# Suspender/reanudar reconciliación (útil para mantenimiento)
flux suspend kustomization apps-dev
flux resume kustomization apps-dev

# Exportar el estado actual de recursos Flux como YAML
flux export source git --all
flux export kustomization --all
```

## Comparación con Argo CD

| | Flux | Argo CD |
|---|---|---|
| **UI** | No incluida (hay Weave GitOps) | UI web integrada |
| **Modelo** | Controladores CRDs independientes | Servidor centralizado |
| **Multitenancy** | Nativo por diseño (namespaces) | RBAC + proyectos |
| **Helm** | HelmRelease CRD | App of Apps / ApplicationSet |
| **Kustomize** | Kustomization CRD | Nativo en Application |
| **Image automation** | Integrada (Image Reflector/Automation) | Requiere plugin externo |
| **Notificaciones** | notification-controller nativo | Nativo |
| **Curva de aprendizaje** | Mayor (sin UI) | Menor (UI intuitiva) |
| **Multi-cluster** | A través de kubeconfig secrets | Nativo, mejor soporte |
| **CNCF** | Graduated | Graduated |

**Cuándo preferir Flux:**
- Equipos que prefieren una filosofía 100% CLI y GitOps pura
- Casos donde la UI no es un requerimiento
- Proyectos que ya usan Helm y Kustomize intensivamente
- Entornos donde la automatización de imágenes es importante

**Cuándo preferir Argo CD:**
- Equipos que necesitan visibilidad gráfica del estado del clúster
- Organizaciones con múltiples clústeres y necesidad de vista centralizada
- Cuando se requiere aprobación manual de syncs (sync windows)

## Casos de uso

### Despliegue continuo de aplicaciones
Push a `main` → Flux detecta el cambio → aplica el deployment actualizado → la aplicación nueva está en producción en segundos.

### Gestión de infraestructura del clúster
Flux no solo sirve para aplicaciones. Puede gestionar los propios componentes del clúster: cert-manager, ingress-nginx, Prometheus stack, políticas OPA. Todo declarado en Git.

### Ambientes efímeros
Con Kustomize overlays se pueden crear ambientes por rama o por PR que Flux gestiona automáticamente. Al cerrar el PR, el overlay desaparece y Flux (`prune: true`) elimina los recursos.

### Automatización de imagen tags
Flux Image Automation monitoriza un registro de imágenes (ECR, Docker Hub, GCR) y actualiza automáticamente el tag en Git cuando se publica una imagen nueva que cumple una política semver.

### Auditoría y compliance
Toda operación queda registrada en Git (quién cambió qué, cuándo y por qué vía el mensaje del commit). El historial de Git es el log de auditoría del clúster.

## Cuándo elegir Flux

Flux es una buena elección cuando:

- Se quiere una solución GitOps puramente declarativa sin servidor adicional
- El equipo está cómodo trabajando desde la línea de comandos
- Se necesita image automation integrada
- El modelo de multitenancy basado en namespaces encaja con la organización
- Se busca una herramienta CNCF graduated con una comunidad activa y mantenimiento a largo plazo
