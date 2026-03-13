# Kubernetes Challenges & Exercises

> 276 practical Kubernetes exercises organized in 22 categories.
> From basic pod creation to building complete platforms — every topic covered from fundamentals to insane-level challenges.
> Each exercise includes learning objectives, YAML manifests, verification commands, and references.

**Difficulty Levels**:
- **Basic** (64) — Full step-by-step guidance, complete YAML, common mistakes
- **Intermediate** (87) — Guided steps with YAML, less hand-holding, spot-the-bug sections
- **Advanced** (82) — Architecture + suggested steps, deeper understanding required
- **Insane** (43) — Scenario + constraints + success criteria only, no code provided

**Requirements**:
- `kubectl` configured with a cluster (minikube, kind, k3d, or cloud)
- `helm` installed
- `metrics-server` deployed in the cluster (for scaling exercises)

**Convention**: Each exercise uses a clean namespace. Clean up with `kubectl delete namespace <ns>` when done.

---

### 01 — Pods & Pod Design

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Your First Pod](01-pods-and-pod-design/01-your-first-pod/01-your-first-pod.md) | Basic |
| 02 | [Pod Lifecycle and Restart Policies](01-pods-and-pod-design/02-pod-lifecycle-and-restart-policies/02-pod-lifecycle-and-restart-policies.md) | Basic |
| 03 | [Labels, Selectors, and Annotations](01-pods-and-pod-design/03-labels-selectors-and-annotations/03-labels-selectors-and-annotations.md) | Basic |
| 04 | [Working with Namespaces](01-pods-and-pod-design/04-working-with-namespaces/04-working-with-namespaces.md) | Basic |
| 05 | [Declarative vs Imperative Management](01-pods-and-pod-design/05-declarative-vs-imperative/05-declarative-vs-imperative.md) | Basic |
| 06 | [Multi-Container Patterns: Sidecar, Ambassador, Adapter](01-pods-and-pod-design/06-multi-container-patterns/06-multi-container-patterns.md) | Intermediate |
| 07 | [Init Containers for Pre-Flight Checks](01-pods-and-pod-design/07-init-containers/07-init-containers.md) | Intermediate |
| 08 | [Ephemeral Containers and kubectl debug](01-pods-and-pod-design/08-ephemeral-containers-and-debugging/08-ephemeral-containers-and-debugging.md) | Intermediate |
| 09 | [Pod Lifecycle Hooks: postStart and preStop](01-pods-and-pod-design/09-pod-lifecycle-hooks/09-pod-lifecycle-hooks.md) | Intermediate |
| 10 | [Downward API and Pod Metadata](01-pods-and-pod-design/10-downward-api-and-pod-metadata/10-downward-api-and-pod-metadata.md) | Intermediate |
| 11 | [Native Sidecar Containers (KEP-753)](01-pods-and-pod-design/11-native-sidecar-containers/11-native-sidecar-containers.md) | Advanced |
| 12 | [Pod Topology Spread Constraints](01-pods-and-pod-design/12-pod-topology-spread-constraints/12-pod-topology-spread-constraints.md) | Advanced |
| 13 | [Pod Priority and Preemption](01-pods-and-pod-design/13-pod-priority-and-preemption/13-pod-priority-and-preemption.md) | Advanced |
| 14 | [Projected Volumes and Combined Sources](01-pods-and-pod-design/14-projected-volumes/14-projected-volumes.md) | Advanced |
| 15 | [Multi-Container Debugging Challenge](01-pods-and-pod-design/15-multi-container-debugging-challenge/15-multi-container-debugging-challenge.md) | Insane |
| 16 | [Designing a Self-Healing Pod Architecture](01-pods-and-pod-design/16-self-healing-pod-architecture/16-self-healing-pod-architecture.md) | Insane |

### 02 — Deployments & Rollouts

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Your First Deployment](02-deployments-and-rollouts/01-your-first-deployment/01-your-first-deployment.md) | Basic |
| 02 | [Scaling and Self-Healing](02-deployments-and-rollouts/02-scaling-and-self-healing/02-scaling-and-self-healing.md) | Basic |
| 03 | [Declarative Deployment Updates](02-deployments-and-rollouts/03-declarative-deployment-updates/03-declarative-deployment-updates.md) | Basic |
| 04 | [Deployment Labels and Selectors](02-deployments-and-rollouts/04-deployment-labels-and-selectors/04-deployment-labels-and-selectors.md) | Basic |
| 05 | [Rolling Updates and Rollbacks](02-deployments-and-rollouts/05-rolling-updates-and-rollbacks/05-rolling-updates-and-rollbacks.md) | Intermediate |
| 06 | [Deployment Strategies: maxSurge and maxUnavailable](02-deployments-and-rollouts/06-maxsurge-and-maxunavailable/06-maxsurge-and-maxunavailable.md) | Intermediate |
| 07 | [ReplicaSets and Ownership](02-deployments-and-rollouts/07-replicasets-and-ownership/07-replicasets-and-ownership.md) | Intermediate |
| 08 | [Deployment Revision History and Management](02-deployments-and-rollouts/08-deployment-revision-history/08-deployment-revision-history.md) | Intermediate |
| 09 | [Canary Deployments with Label Selectors](02-deployments-and-rollouts/09-canary-deployments/09-canary-deployments.md) | Advanced |
| 10 | [A/B Testing with Header-Based Routing](02-deployments-and-rollouts/10-ab-testing-header-routing/10-ab-testing-header-routing.md) | Advanced |
| 11 | [Progressive Rollouts with Readiness Gates](02-deployments-and-rollouts/11-progressive-rollouts-readiness-gates/11-progressive-rollouts-readiness-gates.md) | Advanced |
| 12 | [Recreate vs Rolling Update Deep Dive](02-deployments-and-rollouts/12-recreate-vs-rolling-strategies/12-recreate-vs-rolling-strategies.md) | Advanced |
| 13 | [Blue-Green Deployments Without Native Support](02-deployments-and-rollouts/13-blue-green-deployments/13-blue-green-deployments.md) | Insane |
| 14 | [Multi-Cluster Deployment Orchestration](02-deployments-and-rollouts/14-multi-cluster-deployment-orchestration/14-multi-cluster-deployment-orchestration.md) | Insane |

### 03 — StatefulSets & DaemonSets

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [StatefulSets and Persistent Storage](03-statefulsets-and-daemonsets/01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md) | Intermediate |
| 02 | [StatefulSet Scaling and Pod Management Policy](03-statefulsets-and-daemonsets/02-statefulset-scaling-behavior/02-statefulset-scaling-behavior.md) | Intermediate |
| 03 | [Ordered vs Parallel Pod Management](03-statefulsets-and-daemonsets/03-statefulset-ordered-vs-parallel/03-statefulset-ordered-vs-parallel.md) | Basic |
| 04 | [Partition Rolling Updates](03-statefulsets-and-daemonsets/04-statefulset-partition-rolling-updates/04-statefulset-partition-rolling-updates.md) | Intermediate |
| 05 | [DaemonSets with Tolerations and Node Selection](03-statefulsets-and-daemonsets/05-daemonsets-with-tolerations/05-daemonsets-with-tolerations.md) | Advanced |
| 06 | [DaemonSet Update Strategies: Rolling vs OnDelete](03-statefulsets-and-daemonsets/06-daemonset-update-strategies/06-daemonset-update-strategies.md) | Intermediate |
| 07 | [Running DaemonSets on Control Plane Nodes](03-statefulsets-and-daemonsets/07-daemonset-on-control-plane/07-daemonset-on-control-plane.md) | Advanced |
| 08 | [StatefulSet DNS and Headless Service Patterns](03-statefulsets-and-daemonsets/08-statefulset-headless-dns-patterns/08-statefulset-headless-dns-patterns.md) | Basic |
| 09 | [PersistentVolumeClaim Retention Policies](03-statefulsets-and-daemonsets/09-pvc-retention-policy/09-pvc-retention-policy.md) | Advanced |
| 10 | [StatefulSet Data Migration Between Storage Backends](03-statefulsets-and-daemonsets/10-statefulset-data-migration/10-statefulset-data-migration.md) | Advanced |
| 11 | [Deploying a Distributed Database with StatefulSets](03-statefulsets-and-daemonsets/11-distributed-database-statefulset/11-distributed-database-statefulset.md) | Insane |
| 12 | [Zero-Downtime StatefulSet Migration with Data Continuity](03-statefulsets-and-daemonsets/12-zero-downtime-statefulset-migration/12-zero-downtime-statefulset-migration.md) | Insane |

### 04 — Jobs, CronJobs & Batch

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Jobs and CronJobs](04-jobs-cronjobs-and-batch/01-jobs-and-cronjobs/01-jobs-and-cronjobs.md) | Basic |
| 02 | [Parallel Jobs with Completions and Parallelism](04-jobs-cronjobs-and-batch/02-parallel-jobs-completions/02-parallel-jobs-completions.md) | Basic |
| 03 | [CronJob Schedules and Concurrency Policies](04-jobs-cronjobs-and-batch/03-cronjob-schedules-and-policies/03-cronjob-schedules-and-policies.md) | Basic |
| 04 | [Indexed Jobs for Work Queues](04-jobs-cronjobs-and-batch/04-indexed-jobs/04-indexed-jobs.md) | Intermediate |
| 05 | [Job Failure Handling: backoffLimit and activeDeadlineSeconds](04-jobs-cronjobs-and-batch/05-job-failure-handling/05-job-failure-handling.md) | Intermediate |
| 06 | [CronJob Suspend, Resume, and History Limits](04-jobs-cronjobs-and-batch/06-cronjob-suspend-and-history/06-cronjob-suspend-and-history.md) | Intermediate |
| 07 | [TTL Controller and Job Cleanup](04-jobs-cronjobs-and-batch/07-ttl-after-finished/07-ttl-after-finished.md) | Advanced |
| 08 | [Pod Failure Policy for Jobs](04-jobs-cronjobs-and-batch/08-job-pod-failure-policy/08-job-pod-failure-policy.md) | Advanced |
| 09 | [Building a Batch Processing Pipeline](04-jobs-cronjobs-and-batch/09-batch-processing-pipeline/09-batch-processing-pipeline.md) | Advanced |
| 10 | [Distributed Batch Processing with Work Queues](04-jobs-cronjobs-and-batch/10-distributed-batch-system/10-distributed-batch-system.md) | Insane |

### 05 — Services & Service Discovery

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Services: ClusterIP, NodePort, LoadBalancer](05-services-and-service-discovery/01-services-clusterip-nodeport-loadbalancer/01-services-clusterip-nodeport-loadbalancer.md) | Basic |
| 02 | [ClusterIP Service Fundamentals](05-services-and-service-discovery/02-clusterip-service-fundamentals/02-clusterip-service-fundamentals.md) | Basic |
| 03 | [NodePort Services and External Access](05-services-and-service-discovery/03-nodeport-services/03-nodeport-services.md) | Basic |
| 04 | [Service Selectors and Endpoint Objects](05-services-and-service-discovery/04-service-selectors-and-endpoints/04-service-selectors-and-endpoints.md) | Basic |
| 05 | [DNS and Service Discovery](05-services-and-service-discovery/05-dns-and-service-discovery/05-dns-and-service-discovery.md) | Intermediate |
| 06 | [Headless Services and StatefulSet DNS](05-services-and-service-discovery/06-headless-services-and-statefulset-dns/06-headless-services-and-statefulset-dns.md) | Intermediate |
| 07 | [Multi-Port Services and Named Ports](05-services-and-service-discovery/07-multi-port-services/07-multi-port-services.md) | Intermediate |
| 08 | [ExternalName Services for External Access](05-services-and-service-discovery/08-externalname-services/08-externalname-services.md) | Intermediate |
| 09 | [EndpointSlices and Topology-Aware Routing](05-services-and-service-discovery/09-endpointslices/09-endpointslices.md) | Advanced |
| 10 | [Session Affinity and Traffic Policies](05-services-and-service-discovery/10-session-affinity-and-traffic-policy/10-session-affinity-and-traffic-policy.md) | Advanced |
| 11 | [External and Internal Traffic Policy](05-services-and-service-discovery/11-external-traffic-policy/11-external-traffic-policy.md) | Advanced |
| 12 | [Cross-Namespace Service Discovery Patterns](05-services-and-service-discovery/12-service-discovery-patterns/12-service-discovery-patterns.md) | Advanced |
| 13 | [Building Service Mesh Patterns Without a Mesh](05-services-and-service-discovery/13-service-mesh-without-mesh/13-service-mesh-without-mesh.md) | Insane |
| 14 | [Multi-Cluster Service Discovery](05-services-and-service-discovery/14-multi-cluster-service-discovery/14-multi-cluster-service-discovery.md) | Insane |

### 06 — Ingress & Gateway API

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Ingress Controller and Routing](06-ingress-and-gateway-api/01-ingress-controller-routing/01-ingress-controller-routing.md) | Basic |
| 02 | [Ingress Host-Based and Path-Based Routing](06-ingress-and-gateway-api/02-ingress-host-and-path-routing/02-ingress-host-and-path-routing.md) | Basic |
| 03 | [Ingress TLS Termination](06-ingress-and-gateway-api/03-ingress-tls-termination/03-ingress-tls-termination.md) | Basic |
| 04 | [Ingress Annotations: Rate Limiting, CORS, Rewrites](06-ingress-and-gateway-api/04-ingress-annotations/04-ingress-annotations.md) | Intermediate |
| 05 | [Gateway API Fundamentals](06-ingress-and-gateway-api/05-gateway-api-fundamentals/05-gateway-api-fundamentals.md) | Intermediate |
| 06 | [Gateway API: HTTPRoute Advanced Matching](06-ingress-and-gateway-api/06-gateway-api-httproute/06-gateway-api-httproute.md) | Intermediate |
| 07 | [IngressClass and Multiple Controllers](06-ingress-and-gateway-api/07-ingress-class-multiple-controllers/07-ingress-class-multiple-controllers.md) | Intermediate |
| 08 | [Gateway API Traffic Splitting and Canary](06-ingress-and-gateway-api/08-gateway-api-traffic-splitting/08-gateway-api-traffic-splitting.md) | Advanced |
| 09 | [Gateway API: TLSRoute, TCPRoute, GRPCRoute](06-ingress-and-gateway-api/09-gateway-api-tls-tcp-grpc/09-gateway-api-tls-tcp-grpc.md) | Advanced |
| 10 | [Gateway API with Different Implementations](06-ingress-and-gateway-api/10-gateway-api-implementations/10-gateway-api-implementations.md) | Advanced |
| 11 | [Zero-Downtime Ingress to Gateway API Migration](06-ingress-and-gateway-api/11-zero-downtime-ingress-migration/11-zero-downtime-ingress-migration.md) | Insane |
| 12 | [Multi-Tenant Ingress Platform Design](06-ingress-and-gateway-api/12-multi-tenant-ingress-platform/12-multi-tenant-ingress-platform.md) | Insane |

### 07 — Network Policies & CNI

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Network Policies: Pod Isolation](07-network-policies-and-cni/01-network-policies-pod-isolation/01-network-policies-pod-isolation.md) | Basic |
| 02 | [Default Deny All Traffic](07-network-policies-and-cni/02-default-deny-policies/02-default-deny-policies.md) | Basic |
| 03 | [Ingress Network Policies with Pod and Namespace Selectors](07-network-policies-and-cni/03-ingress-network-policies/03-ingress-network-policies.md) | Intermediate |
| 04 | [Egress Network Policies and DNS Access](07-network-policies-and-cni/04-egress-network-policies/04-egress-network-policies.md) | Intermediate |
| 05 | [IPBlock and CIDR-Based Network Policies](07-network-policies-and-cni/05-ipblock-and-cidr-policies/05-ipblock-and-cidr-policies.md) | Intermediate |
| 06 | [Network Policies: Zero Trust Architecture](07-network-policies-and-cni/06-network-policies-zero-trust/06-network-policies-zero-trust.md) | Intermediate |
| 07 | [Namespace Isolation Patterns](07-network-policies-and-cni/07-namespace-isolation-patterns/07-namespace-isolation-patterns.md) | Advanced |
| 08 | [Cilium L7 Network Policies](07-network-policies-and-cni/08-cilium-l7-policies/08-cilium-l7-policies.md) | Advanced |
| 09 | [Network Policy Debugging and Troubleshooting](07-network-policies-and-cni/09-network-policy-debugging/09-network-policy-debugging.md) | Advanced |
| 10 | [CNI Plugin Comparison: Calico vs Cilium vs Flannel](07-network-policies-and-cni/10-cni-comparison/10-cni-comparison.md) | Advanced |
| 11 | [Full Microsegmentation of a Microservices App](07-network-policies-and-cni/11-microsegmentation-challenge/11-microsegmentation-challenge.md) | Insane |
| 12 | [Multi-Tenant Network Isolation Platform](07-network-policies-and-cni/12-multi-tenant-network-isolation/12-multi-tenant-network-isolation.md) | Insane |

### 08 — Configuration (ConfigMaps, Secrets, Env)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [ConfigMaps: Environment Variables and File Mounts](08-configuration/01-configmaps-environment-and-files/01-configmaps-environment-and-files.md) | Basic |
| 02 | [Secrets Management](08-configuration/02-secrets-management/02-secrets-management.md) | Basic |
| 03 | [ConfigMaps from Files, Directories, and Env Files](08-configuration/03-configmap-from-files-and-directories/03-configmap-from-files-and-directories.md) | Basic |
| 04 | [Secret Types: Opaque, TLS, Docker Registry, SSH](08-configuration/04-secret-types/04-secret-types.md) | Basic |
| 05 | [ConfigMap Volume Updates and Propagation](08-configuration/05-configmap-volume-updates/05-configmap-volume-updates.md) | Intermediate |
| 06 | [Immutable ConfigMaps and Secrets](08-configuration/06-immutable-configmaps-and-secrets/06-immutable-configmaps-and-secrets.md) | Intermediate |
| 07 | [SubPath Mounts for Selective File Injection](08-configuration/07-subpath-mounts/07-subpath-mounts.md) | Intermediate |
| 08 | [Environment Variable Patterns: fieldRef, resourceFieldRef](08-configuration/08-environment-variable-patterns/08-environment-variable-patterns.md) | Intermediate |
| 09 | [External Secrets Operator with Cloud Providers](08-configuration/09-external-secrets-operator/09-external-secrets-operator.md) | Intermediate |
| 10 | [Sealed Secrets for GitOps](08-configuration/10-sealed-secrets/10-sealed-secrets.md) | Advanced |
| 11 | [HashiCorp Vault CSI Provider Integration](08-configuration/11-vault-csi-provider/11-vault-csi-provider.md) | Advanced |
| 12 | [SOPS Encryption for Kubernetes Secrets](08-configuration/12-sops-with-kubernetes/12-sops-with-kubernetes.md) | Advanced |
| 13 | [Dynamic Configuration Reload Without Restarts](08-configuration/13-dynamic-config-reload-system/13-dynamic-config-reload-system.md) | Insane |
| 14 | [Multi-Environment Configuration Management Platform](08-configuration/14-multi-environment-config-management/14-multi-environment-config-management.md) | Insane |

### 09 — Storage & Persistent Volumes

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Persistent Volumes and Claims](09-storage-and-persistent-volumes/01-persistent-volumes-and-claims/01-persistent-volumes-and-claims.md) | Basic |
| 02 | [Storage Classes and Dynamic Provisioning](09-storage-and-persistent-volumes/02-storage-classes-dynamic-provisioning/02-storage-classes-dynamic-provisioning.md) | Basic |
| 03 | [Volume Basics: emptyDir and hostPath](09-storage-and-persistent-volumes/03-volume-basics-emptydir-hostpath/03-volume-basics-emptydir-hostpath.md) | Basic |
| 04 | [Ephemeral Volumes: emptyDir and Projected](09-storage-and-persistent-volumes/04-ephemeral-volumes-emptydir-projected/04-ephemeral-volumes-emptydir-projected.md) | Intermediate |
| 05 | [PVC Access Modes and Volume Binding](09-storage-and-persistent-volumes/05-pvc-access-modes-and-binding/05-pvc-access-modes-and-binding.md) | Intermediate |
| 06 | [Volume Expansion and Resize](09-storage-and-persistent-volumes/06-volume-expansion/06-volume-expansion.md) | Intermediate |
| 07 | [Volume Snapshots and Restore](09-storage-and-persistent-volumes/07-volume-snapshots/07-volume-snapshots.md) | Intermediate |
| 08 | [CSI Drivers: EBS, EFS, Local Path](09-storage-and-persistent-volumes/08-csi-drivers/08-csi-drivers.md) | Advanced |
| 09 | [Volume Cloning and Data Migration](09-storage-and-persistent-volumes/09-volume-cloning-and-migration/09-volume-cloning-and-migration.md) | Advanced |
| 10 | [ReadWriteMany Shared Storage Patterns](09-storage-and-persistent-volumes/10-rwx-shared-storage/10-rwx-shared-storage.md) | Advanced |
| 11 | [Namespace Backup and Restore with Velero](09-storage-and-persistent-volumes/11-velero-backup-restore/11-velero-backup-restore.md) | Insane |
| 12 | [Cross-Cloud Storage Platform Design](09-storage-and-persistent-volumes/12-storage-platform-design/12-storage-platform-design.md) | Insane |

### 10 — Resource Management & QoS

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Resource Requests and Limits](10-resource-management-and-qos/01-resource-requests-and-limits/01-resource-requests-and-limits.md) | Basic |
| 02 | [QoS Classes: Guaranteed, Burstable, BestEffort](10-resource-management-and-qos/02-qos-classes/02-qos-classes.md) | Basic |
| 03 | [LimitRanges: Default and Constraints Per Namespace](10-resource-management-and-qos/03-limitranges/03-limitranges.md) | Intermediate |
| 04 | [ResourceQuotas: CPU, Memory, and Object Count](10-resource-management-and-qos/04-resource-quotas/04-resource-quotas.md) | Intermediate |
| 05 | [Pod Disruption Budgets](10-resource-management-and-qos/05-pod-disruption-budgets/05-pod-disruption-budgets.md) | Intermediate |
| 06 | [Resource Right-Sizing with VPA Recommendations](10-resource-management-and-qos/06-resource-right-sizing/06-resource-right-sizing.md) | Intermediate |
| 07 | [Priority Classes and Preemption](10-resource-management-and-qos/07-priority-classes-and-preemption/07-priority-classes-and-preemption.md) | Advanced |
| 08 | [Pod Overhead and Runtime Classes](10-resource-management-and-qos/08-pod-overhead-and-runtime-classes/08-pod-overhead-and-runtime-classes.md) | Advanced |
| 09 | [Resource Monitoring and Optimization Workflow](10-resource-management-and-qos/09-resource-monitoring-and-optimization/09-resource-monitoring-and-optimization.md) | Advanced |
| 10 | [Multi-Tenant Resource Governance Platform](10-resource-management-and-qos/10-multi-tenant-resource-governance/10-multi-tenant-resource-governance.md) | Insane |

### 11 — RBAC & Authentication

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [RBAC: Roles and Bindings](11-rbac-and-authentication/01-rbac-roles-and-bindings/01-rbac-roles-and-bindings.md) | Basic |
| 02 | [Service Accounts and Tokens](11-rbac-and-authentication/02-service-accounts-and-tokens/02-service-accounts-and-tokens.md) | Basic |
| 03 | [RBAC Verbs, Resources, and API Groups](11-rbac-and-authentication/03-rbac-verbs-and-resources/03-rbac-verbs-and-resources.md) | Basic |
| 04 | [Namespace-Scoped RBAC for Teams](11-rbac-and-authentication/04-namespace-scoped-rbac/04-namespace-scoped-rbac.md) | Intermediate |
| 05 | [ClusterRoles and Aggregated ClusterRoles](11-rbac-and-authentication/05-clusterroles-and-aggregation/05-clusterroles-and-aggregation.md) | Intermediate |
| 06 | [RBAC Debugging with kubectl auth can-i](11-rbac-and-authentication/06-rbac-debugging/06-rbac-debugging.md) | Intermediate |
| 07 | [Service Account Token Projection and Rotation](11-rbac-and-authentication/07-service-account-token-projection/07-service-account-token-projection.md) | Intermediate |
| 08 | [RBAC for CI/CD Service Accounts](11-rbac-and-authentication/08-rbac-for-cicd-pipelines/08-rbac-for-cicd-pipelines.md) | Advanced |
| 09 | [OIDC Authentication Integration](11-rbac-and-authentication/09-oidc-authentication/09-oidc-authentication.md) | Advanced |
| 10 | [User Impersonation and Audit Logging](11-rbac-and-authentication/10-impersonation-and-audit/10-impersonation-and-audit.md) | Advanced |
| 11 | [Multi-Tenant RBAC Platform Design](11-rbac-and-authentication/11-multi-tenant-rbac-platform/11-multi-tenant-rbac-platform.md) | Insane |
| 12 | [Zero-Trust API Access Architecture](11-rbac-and-authentication/12-zero-trust-api-access/12-zero-trust-api-access.md) | Insane |

### 12 — Pod Security & Hardening

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Security Contexts and Pod Security Standards](12-pod-security-and-hardening/01-security-contexts-and-pod-security/01-security-contexts-and-pod-security.md) | Basic |
| 02 | [Pod Security Admission Controller](12-pod-security-and-hardening/02-pod-security-admission/02-pod-security-admission.md) | Basic |
| 03 | [runAsNonRoot and Read-Only Root Filesystem](12-pod-security-and-hardening/03-runasnonroot-and-readonly-filesystem/03-runasnonroot-and-readonly-filesystem.md) | Intermediate |
| 04 | [Linux Capabilities: Add and Drop](12-pod-security-and-hardening/04-linux-capabilities/04-linux-capabilities.md) | Intermediate |
| 05 | [Seccomp Profiles: RuntimeDefault and Custom](12-pod-security-and-hardening/05-seccomp-profiles/05-seccomp-profiles.md) | Intermediate |
| 06 | [Secrets Encryption at Rest](12-pod-security-and-hardening/06-secrets-encryption-at-rest/06-secrets-encryption-at-rest.md) | Intermediate |
| 07 | [AppArmor Profiles for Pods](12-pod-security-and-hardening/07-apparmor-profiles/07-apparmor-profiles.md) | Advanced |
| 08 | [Image Scanning and Admission Webhooks](12-pod-security-and-hardening/08-image-scanning-admission/08-image-scanning-admission.md) | Advanced |
| 09 | [Supply Chain Security: cosign, SBOM, SLSA](12-pod-security-and-hardening/09-supply-chain-security/09-supply-chain-security.md) | Advanced |
| 10 | [Certificate Management with cert-manager](12-pod-security-and-hardening/10-certificate-management/10-certificate-management.md) | Advanced |
| 11 | [Full CIS Benchmark Cluster Hardening](12-pod-security-and-hardening/11-full-cluster-hardening/11-full-cluster-hardening.md) | Insane |
| 12 | [Runtime Threat Detection and Response](12-pod-security-and-hardening/12-runtime-threat-detection/12-runtime-threat-detection.md) | Insane |

### 13 — Policy Engines (Gatekeeper, Kyverno)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [OPA Gatekeeper Installation and Basics](13-policy-engines/01-opa-gatekeeper-basics/01-opa-gatekeeper-basics.md) | Basic |
| 02 | [Kyverno Installation and Validate Policies](13-policy-engines/02-kyverno-basics/02-kyverno-basics.md) | Basic |
| 03 | [Gatekeeper ConstraintTemplates and Constraints](13-policy-engines/03-gatekeeper-constraint-templates/03-gatekeeper-constraint-templates.md) | Intermediate |
| 04 | [Kyverno Mutate Policies: Defaults and Injection](13-policy-engines/04-kyverno-mutate-policies/04-kyverno-mutate-policies.md) | Intermediate |
| 05 | [Kyverno Generate Policies: Auto-Create Resources](13-policy-engines/05-kyverno-generate-policies/05-kyverno-generate-policies.md) | Intermediate |
| 06 | [Gatekeeper Mutations and Auto-Injection](13-policy-engines/06-gatekeeper-mutations/06-gatekeeper-mutations.md) | Advanced |
| 07 | [Kyverno Image Verification with cosign](13-policy-engines/07-kyverno-image-verification/07-kyverno-image-verification.md) | Advanced |
| 08 | [Policy Testing in CI/CD Pipelines](13-policy-engines/08-policy-testing-and-cicd/08-policy-testing-and-cicd.md) | Advanced |
| 09 | [Enterprise Policy Framework Design](13-policy-engines/09-enterprise-policy-framework/09-enterprise-policy-framework.md) | Insane |
| 10 | [Policy Migration: Gatekeeper to Kyverno](13-policy-engines/10-policy-migration-gatekeeper-to-kyverno/10-policy-migration-gatekeeper-to-kyverno.md) | Insane |

### 14 — Scaling & Autoscaling

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [HPA: CPU and Memory Autoscaling](14-scaling-and-autoscaling/01-hpa-cpu-memory-autoscaling/01-hpa-cpu-memory-autoscaling.md) | Basic |
| 02 | [HPA Basics and Target Utilization](14-scaling-and-autoscaling/02-hpa-basics-target-utilization/02-hpa-basics-target-utilization.md) | Basic |
| 03 | [Manual Scaling: replicas, kubectl scale](14-scaling-and-autoscaling/03-manual-scaling-patterns/03-manual-scaling-patterns.md) | Basic |
| 04 | [HPA Behavior: Scale-Up and Scale-Down Policies](14-scaling-and-autoscaling/04-hpa-behavior-tuning/04-hpa-behavior-tuning.md) | Intermediate |
| 05 | [HPA with Custom Metrics from Prometheus](14-scaling-and-autoscaling/05-hpa-custom-metrics/05-hpa-custom-metrics.md) | Intermediate |
| 06 | [HPA with External Metrics (SQS, CloudWatch)](14-scaling-and-autoscaling/06-hpa-external-metrics/06-hpa-external-metrics.md) | Intermediate |
| 07 | [Vertical Pod Autoscaler (VPA)](14-scaling-and-autoscaling/07-vertical-pod-autoscaler/07-vertical-pod-autoscaler.md) | Intermediate |
| 08 | [KEDA: Event-Driven Autoscaling](14-scaling-and-autoscaling/08-keda-basics/08-keda-basics.md) | Advanced |
| 09 | [KEDA Advanced: Multiple Triggers and Fallback](14-scaling-and-autoscaling/09-keda-advanced-triggers/09-keda-advanced-triggers.md) | Advanced |
| 10 | [Cluster Autoscaler: Node Scaling](14-scaling-and-autoscaling/10-cluster-autoscaler/10-cluster-autoscaler.md) | Advanced |
| 11 | [Karpenter: Intelligent Node Provisioning](14-scaling-and-autoscaling/11-karpenter/11-karpenter.md) | Advanced |
| 12 | [Descheduler: Rebalancing Workloads](14-scaling-and-autoscaling/12-descheduler/12-descheduler.md) | Advanced |
| 13 | [Full Autoscaling Stack: HPA + VPA + Cluster Autoscaler](14-scaling-and-autoscaling/13-autoscaling-stack-integration/13-autoscaling-stack-integration.md) | Insane |
| 14 | [Cost-Optimized Autoscaling Platform](14-scaling-and-autoscaling/14-cost-optimized-scaling-platform/14-cost-optimized-scaling-platform.md) | Insane |

### 15 — Scheduling & Node Management

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Node Affinity and Taints/Tolerations](15-scheduling-and-node-management/01-node-affinity-and-taints/01-node-affinity-and-taints.md) | Basic |
| 02 | [Node Selectors and Labels](15-scheduling-and-node-management/02-node-selectors/02-node-selectors.md) | Basic |
| 03 | [Taints and Tolerations Deep Dive](15-scheduling-and-node-management/03-taints-and-tolerations/03-taints-and-tolerations.md) | Basic |
| 04 | [Pod Affinity and Anti-Affinity](15-scheduling-and-node-management/04-pod-affinity-anti-affinity/04-pod-affinity-anti-affinity.md) | Intermediate |
| 05 | [Topology Spread Constraints](15-scheduling-and-node-management/05-topology-spread-constraints/05-topology-spread-constraints.md) | Intermediate |
| 06 | [Node Maintenance: cordon, drain, uncordon](15-scheduling-and-node-management/06-node-maintenance-cordon-drain/06-node-maintenance-cordon-drain.md) | Intermediate |
| 07 | [Scheduler Profiles and Plugins](15-scheduling-and-node-management/07-scheduler-profiles/07-scheduler-profiles.md) | Intermediate |
| 08 | [Custom Schedulers: Deploying a Second Scheduler](15-scheduling-and-node-management/08-custom-schedulers/08-custom-schedulers.md) | Advanced |
| 09 | [Kubelet Configuration and Resource Reservations](15-scheduling-and-node-management/09-kubelet-configuration/09-kubelet-configuration.md) | Advanced |
| 10 | [Node Problem Detector and Remediation](15-scheduling-and-node-management/10-node-problem-detector/10-node-problem-detector.md) | Advanced |
| 11 | [Advanced Multi-Constraint Scheduling Optimization](15-scheduling-and-node-management/11-advanced-scheduling-optimization/11-advanced-scheduling-optimization.md) | Insane |
| 12 | [Heterogeneous Cluster Scheduling Platform](15-scheduling-and-node-management/12-heterogeneous-cluster-scheduling/12-heterogeneous-cluster-scheduling.md) | Insane |

### 16 — Observability & Monitoring

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Probes: Liveness, Readiness, and Startup](16-observability-and-monitoring/01-probes-liveness-readiness-startup/01-probes-liveness-readiness-startup.md) | Basic |
| 02 | [Metrics Server and kubectl top](16-observability-and-monitoring/02-metrics-server-and-kubectl-top/02-metrics-server-and-kubectl-top.md) | Basic |
| 03 | [Probe Types: HTTP, TCP, Exec, and gRPC](16-observability-and-monitoring/03-probe-types-http-tcp-exec-grpc/03-probe-types-http-tcp-exec-grpc.md) | Basic |
| 04 | [Kubernetes Events: Monitoring and Exporting](16-observability-and-monitoring/04-kubernetes-events/04-kubernetes-events.md) | Basic |
| 05 | [Probe Patterns for Slow-Starting Applications](16-observability-and-monitoring/05-probe-patterns-slow-startup/05-probe-patterns-slow-startup.md) | Intermediate |
| 06 | [Graceful Shutdown with preStop and Readiness](16-observability-and-monitoring/06-graceful-shutdown-probes/06-graceful-shutdown-probes.md) | Intermediate |
| 07 | [Logging with FluentBit DaemonSet](16-observability-and-monitoring/07-logging-with-fluentbit-daemonset/07-logging-with-fluentbit-daemonset.md) | Intermediate |
| 08 | [Prometheus ServiceMonitor and PodMonitor](16-observability-and-monitoring/08-prometheus-servicemonitor/08-prometheus-servicemonitor.md) | Intermediate |
| 09 | [Prometheus and Grafana Stack](16-observability-and-monitoring/09-prometheus-grafana-stack/09-prometheus-grafana-stack.md) | Intermediate |
| 10 | [Custom Application Metrics with /metrics Endpoint](16-observability-and-monitoring/10-custom-app-metrics/10-custom-app-metrics.md) | Advanced |
| 11 | [Distributed Tracing with OpenTelemetry](16-observability-and-monitoring/11-distributed-tracing-opentelemetry/11-distributed-tracing-opentelemetry.md) | Advanced |
| 12 | [OpenTelemetry Collector: Pipelines and Processors](16-observability-and-monitoring/12-opentelemetry-collector/12-opentelemetry-collector.md) | Advanced |
| 13 | [Loki Log Aggregation and LogQL](16-observability-and-monitoring/13-loki-log-aggregation/13-loki-log-aggregation.md) | Advanced |
| 14 | [OpenTelemetry Auto-Instrumentation with Operator](16-observability-and-monitoring/14-opentelemetry-auto-instrumentation/14-opentelemetry-auto-instrumentation.md) | Advanced |
| 15 | [Full Observability Stack: Metrics, Logs, Traces](16-observability-and-monitoring/15-full-observability-stack/15-full-observability-stack.md) | Insane |
| 16 | [SLO-Based Alerting Platform Design](16-observability-and-monitoring/16-slo-based-alerting-platform/16-slo-based-alerting-platform.md) | Insane |

### 17 — Service Mesh (Istio, Linkerd, Cilium)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Istio Installation and Sidecar Injection](17-service-mesh/01-istio-installation-and-injection/01-istio-installation-and-injection.md) | Basic |
| 02 | [Linkerd Installation and Meshing](17-service-mesh/02-linkerd-basics/02-linkerd-basics.md) | Basic |
| 03 | [Istio Traffic Management: VirtualService and DestinationRule](17-service-mesh/03-istio-traffic-management/03-istio-traffic-management.md) | Intermediate |
| 04 | [Istio Fault Injection and Resilience Testing](17-service-mesh/04-istio-fault-injection/04-istio-fault-injection.md) | Intermediate |
| 05 | [Istio Circuit Breaking and Outlier Detection](17-service-mesh/05-istio-circuit-breaking/05-istio-circuit-breaking.md) | Intermediate |
| 06 | [Istio Observability: Kiali, Jaeger, Prometheus](17-service-mesh/06-istio-observability/06-istio-observability.md) | Intermediate |
| 07 | [Istio Security: mTLS and AuthorizationPolicy](17-service-mesh/07-istio-security-mtls/07-istio-security-mtls.md) | Advanced |
| 08 | [Istio Traffic Mirroring and Shadowing](17-service-mesh/08-istio-traffic-mirroring/08-istio-traffic-mirroring.md) | Advanced |
| 09 | [Cilium Sidecar-Less Service Mesh](17-service-mesh/09-cilium-service-mesh/09-cilium-service-mesh.md) | Advanced |
| 10 | [Linkerd Traffic Splitting and Service Profiles](17-service-mesh/10-linkerd-traffic-splitting/10-linkerd-traffic-splitting.md) | Advanced |
| 11 | [Service Mesh Migration: No-Mesh to Full Mesh](17-service-mesh/11-service-mesh-migration/11-service-mesh-migration.md) | Insane |
| 12 | [Multi-Cluster Service Mesh Federation](17-service-mesh/12-multi-mesh-federation/12-multi-mesh-federation.md) | Insane |

### 18 — Helm, Kustomize & Packaging

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Helm Chart Basics](18-helm-kustomize-and-packaging/01-helm-chart-basics/01-helm-chart-basics.md) | Basic |
| 02 | [Helm Values and Templates](18-helm-kustomize-and-packaging/02-helm-values-and-templates/02-helm-values-and-templates.md) | Basic |
| 03 | [Kustomize Basics: Base and Overlays](18-helm-kustomize-and-packaging/03-kustomize-basics/03-kustomize-basics.md) | Basic |
| 04 | [Helm Subcharts and Dependencies](18-helm-kustomize-and-packaging/04-helm-subcharts-and-dependencies/04-helm-subcharts-and-dependencies.md) | Intermediate |
| 05 | [Kustomize Overlays for Multi-Environment](18-helm-kustomize-and-packaging/05-kustomize-overlays/05-kustomize-overlays.md) | Intermediate |
| 06 | [Helm Hooks: Pre/Post Install and Upgrade](18-helm-kustomize-and-packaging/06-helm-hooks/06-helm-hooks.md) | Intermediate |
| 07 | [Kustomize Components and Replacements](18-helm-kustomize-and-packaging/07-kustomize-components-and-replacements/07-kustomize-components-and-replacements.md) | Intermediate |
| 08 | [Helm Chart Testing with ct and helm test](18-helm-kustomize-and-packaging/08-helm-testing/08-helm-testing.md) | Advanced |
| 09 | [Helm Library Charts for Shared Templates](18-helm-kustomize-and-packaging/09-helm-library-charts/09-helm-library-charts.md) | Advanced |
| 10 | [Kustomize with Helm: HelmChartInflationGenerator](18-helm-kustomize-and-packaging/10-kustomize-helm-integration/10-kustomize-helm-integration.md) | Advanced |
| 11 | [Helm Chart Repository and Release Platform](18-helm-kustomize-and-packaging/11-chart-repository-platform/11-chart-repository-platform.md) | Insane |
| 12 | [Migration from Helm to Kustomize (or Vice Versa)](18-helm-kustomize-and-packaging/12-packaging-strategy-migration/12-packaging-strategy-migration.md) | Insane |

### 19 — GitOps (ArgoCD, Flux, Argo Rollouts)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [ArgoCD: GitOps Deployment](19-gitops/01-argocd-gitops-deployment/01-argocd-gitops-deployment.md) | Basic |
| 02 | [ArgoCD Sync Policies and Auto-Sync](19-gitops/02-argocd-sync-policies/02-argocd-sync-policies.md) | Basic |
| 03 | [Flux v2: GitRepository and Kustomization](19-gitops/03-flux-basics/03-flux-basics.md) | Basic |
| 04 | [ArgoCD Application Management and Health Checks](19-gitops/04-argocd-application-management/04-argocd-application-management.md) | Intermediate |
| 05 | [ArgoCD: App of Apps Pattern](19-gitops/05-argocd-app-of-apps/05-argocd-app-of-apps.md) | Intermediate |
| 06 | [ArgoCD ApplicationSets: Generator Types](19-gitops/06-argocd-applicationsets/06-argocd-applicationsets.md) | Intermediate |
| 07 | [Flux HelmRelease and HelmRepository](19-gitops/07-flux-helmrelease/07-flux-helmrelease.md) | Intermediate |
| 08 | [ArgoCD Multi-Cluster Deployment](19-gitops/08-argocd-multi-cluster/08-argocd-multi-cluster.md) | Advanced |
| 09 | [Argo Rollouts: Canary with Analysis](19-gitops/09-argo-rollouts-canary/09-argo-rollouts-canary.md) | Advanced |
| 10 | [Argo Rollouts: Blue-Green with Promotion](19-gitops/10-argo-rollouts-blue-green/10-argo-rollouts-blue-green.md) | Advanced |
| 11 | [Flux Image Automation: Auto-Update Images in Git](19-gitops/11-flux-image-automation/11-flux-image-automation.md) | Advanced |
| 12 | [GitOps Secrets Management: SOPS + Sealed Secrets](19-gitops/12-gitops-secrets-management/12-gitops-secrets-management.md) | Advanced |
| 13 | [Full GitOps Platform: Multi-Env, Multi-Cluster](19-gitops/13-full-gitops-platform/13-full-gitops-platform.md) | Insane |
| 14 | [GitOps Disaster Recovery and Drift Detection](19-gitops/14-gitops-disaster-recovery/14-gitops-disaster-recovery.md) | Insane |

### 20 — Cluster Administration & Troubleshooting

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [kubeadm Cluster Bootstrap](20-cluster-administration/01-kubeadm-cluster-setup/01-kubeadm-cluster-setup.md) | Basic |
| 02 | [kubectl Mastery: Advanced Commands and Output](20-cluster-administration/02-kubectl-mastery/02-kubectl-mastery.md) | Basic |
| 03 | [Namespace Management and Lifecycle](20-cluster-administration/03-namespace-management/03-namespace-management.md) | Basic |
| 04 | [kubeadm Minor Version Upgrades](20-cluster-administration/04-kubeadm-upgrades/04-kubeadm-upgrades.md) | Intermediate |
| 05 | [etcd Backup and Restore](20-cluster-administration/05-etcd-backup-restore/05-etcd-backup-restore.md) | Intermediate |
| 06 | [Certificate Expiry and Rotation](20-cluster-administration/06-certificate-expiry-rotation/06-certificate-expiry-rotation.md) | Intermediate |
| 07 | [Node Maintenance: cordon, drain, uncordon with PDB](20-cluster-administration/07-node-maintenance-workflow/07-node-maintenance-workflow.md) | Intermediate |
| 08 | [API Server Audit Logging](20-cluster-administration/08-api-server-audit-logging/08-api-server-audit-logging.md) | Intermediate |
| 09 | [etcd Disaster Recovery: Multi-Node](20-cluster-administration/09-etcd-disaster-recovery/09-etcd-disaster-recovery.md) | Advanced |
| 10 | [Cluster Component Debugging: API Server, Scheduler, Controller Manager](20-cluster-administration/10-cluster-component-debugging/10-cluster-component-debugging.md) | Advanced |
| 11 | [Kubelet Troubleshooting and Configuration](20-cluster-administration/11-kubelet-troubleshooting/11-kubelet-troubleshooting.md) | Advanced |
| 12 | [Static Pods and Manifest Management](20-cluster-administration/12-static-pods-and-manifests/12-static-pods-and-manifests.md) | Advanced |
| 13 | [Cluster Networking Debugging: Pod-to-Pod, Service, DNS](20-cluster-administration/13-cluster-networking-debugging/13-cluster-networking-debugging.md) | Advanced |
| 14 | [Full Cluster Recovery from etcd Snapshot](20-cluster-administration/14-full-cluster-recovery/14-full-cluster-recovery.md) | Insane |
| 15 | [Multi-Node Cluster Troubleshooting Challenge](20-cluster-administration/15-multi-node-troubleshooting/15-multi-node-troubleshooting.md) | Insane |
| 16 | [Cluster Migration: kubeadm to Managed Kubernetes](20-cluster-administration/16-cluster-migration/16-cluster-migration.md) | Insane |

### 21 — CRDs, Operators & Extensibility

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Custom Resource Definitions: Basics](21-crds-operators-and-extensibility/01-crd-basics/01-crd-basics.md) | Basic |
| 02 | [CRD Structural Schemas and Validation](21-crds-operators-and-extensibility/02-crd-structural-schemas/02-crd-structural-schemas.md) | Basic |
| 03 | [CRD Printer Columns and Short Names](21-crds-operators-and-extensibility/03-crd-printer-columns/03-crd-printer-columns.md) | Intermediate |
| 04 | [CRD Validation with CEL Expressions](21-crds-operators-and-extensibility/04-crd-cel-validation/04-crd-cel-validation.md) | Intermediate |
| 05 | [The Operator Pattern: Concepts and Architecture](21-crds-operators-and-extensibility/05-operator-pattern/05-operator-pattern.md) | Intermediate |
| 06 | [Kubebuilder: Scaffold an Operator](21-crds-operators-and-extensibility/06-kubebuilder-scaffold/06-kubebuilder-scaffold.md) | Advanced |
| 07 | [Controller-Runtime: Reconciliation Loop](21-crds-operators-and-extensibility/07-controller-runtime-reconciliation/07-controller-runtime-reconciliation.md) | Advanced |
| 08 | [Admission Webhooks: Validating and Mutating](21-crds-operators-and-extensibility/08-admission-webhooks/08-admission-webhooks.md) | Advanced |
| 09 | [Build a Complete Operator from Scratch](21-crds-operators-and-extensibility/09-build-complete-operator/09-build-complete-operator.md) | Insane |
| 10 | [Crossplane: Compositions and Cloud Resources](21-crds-operators-and-extensibility/10-crossplane-compositions/10-crossplane-compositions.md) | Insane |

### 22 — CI/CD & Developer Tools

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Skaffold: Kubernetes Dev Loop](22-cicd-and-developer-tools/01-skaffold-dev-loop/01-skaffold-dev-loop.md) | Basic |
| 02 | [Tilt: Local Kubernetes Development](22-cicd-and-developer-tools/02-tilt-local-development/02-tilt-local-development.md) | Basic |
| 03 | [Tekton: Tasks and Pipelines](22-cicd-and-developer-tools/03-tekton-tasks-and-pipelines/03-tekton-tasks-and-pipelines.md) | Intermediate |
| 04 | [GitHub Actions for Kubernetes Deployment](22-cicd-and-developer-tools/04-github-actions-k8s-deploy/04-github-actions-k8s-deploy.md) | Intermediate |
| 05 | [Telepresence: Remote Debugging in Kubernetes](22-cicd-and-developer-tools/05-telepresence-remote-debugging/05-telepresence-remote-debugging.md) | Intermediate |
| 06 | [Tekton Triggers and Event-Driven Pipelines](22-cicd-and-developer-tools/06-tekton-triggers/06-tekton-triggers.md) | Advanced |
| 07 | [Kaniko: In-Cluster Container Image Builds](22-cicd-and-developer-tools/07-kaniko-in-cluster-builds/07-kaniko-in-cluster-builds.md) | Advanced |
| 08 | [Dagger: Pipelines as Code with Go/Rust SDK](22-cicd-and-developer-tools/08-dagger-pipeline-sdk/08-dagger-pipeline-sdk.md) | Advanced |
| 09 | [Full CI/CD Platform on Kubernetes](22-cicd-and-developer-tools/09-full-cicd-platform/09-full-cicd-platform.md) | Insane |
| 10 | [Developer Platform with Backstage and K8s](22-cicd-and-developer-tools/10-developer-platform-with-backstage/10-developer-platform-with-backstage.md) | Insane |

---

## Statistics

| Difficulty | Count | Percentage |
|-----------|-------|------------|
| Basic | 64 | 23% |
| Intermediate | 87 | 32% |
| Advanced | 82 | 30% |
| Insane | 43 | 16% |
| **Total** | **276** | **100%** |
