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
| 01 | [Your First Pod](01-pods-and-pod-design/01-your-first-pod/) | Basic |
| 02 | [Pod Lifecycle and Restart Policies](01-pods-and-pod-design/02-pod-lifecycle-and-restart-policies/) | Basic |
| 03 | [Labels, Selectors, and Annotations](01-pods-and-pod-design/03-labels-selectors-and-annotations/) | Basic |
| 04 | [Working with Namespaces](01-pods-and-pod-design/04-working-with-namespaces/) | Basic |
| 05 | [Declarative vs Imperative Management](01-pods-and-pod-design/05-declarative-vs-imperative/) | Basic |
| 06 | [Multi-Container Patterns: Sidecar, Ambassador, Adapter](01-pods-and-pod-design/06-multi-container-patterns/) | Intermediate |
| 07 | [Init Containers for Pre-Flight Checks](01-pods-and-pod-design/07-init-containers/) | Intermediate |
| 08 | [Ephemeral Containers and kubectl debug](01-pods-and-pod-design/08-ephemeral-containers-and-debugging/) | Intermediate |
| 09 | [Pod Lifecycle Hooks: postStart and preStop](01-pods-and-pod-design/09-pod-lifecycle-hooks/) | Intermediate |
| 10 | [Downward API and Pod Metadata](01-pods-and-pod-design/10-downward-api-and-pod-metadata/) | Intermediate |
| 11 | [Native Sidecar Containers (KEP-753)](01-pods-and-pod-design/11-native-sidecar-containers/) | Advanced |
| 12 | [Pod Topology Spread Constraints](01-pods-and-pod-design/12-pod-topology-spread-constraints/) | Advanced |
| 13 | [Pod Priority and Preemption](01-pods-and-pod-design/13-pod-priority-and-preemption/) | Advanced |
| 14 | [Projected Volumes and Combined Sources](01-pods-and-pod-design/14-projected-volumes/) | Advanced |
| 15 | [Multi-Container Debugging Challenge](01-pods-and-pod-design/15-multi-container-debugging-challenge/) | Insane |
| 16 | [Designing a Self-Healing Pod Architecture](01-pods-and-pod-design/16-self-healing-pod-architecture/) | Insane |

### 02 — Deployments & Rollouts

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Your First Deployment](02-deployments-and-rollouts/01-your-first-deployment/) | Basic |
| 02 | [Scaling and Self-Healing](02-deployments-and-rollouts/02-scaling-and-self-healing/) | Basic |
| 03 | [Declarative Deployment Updates](02-deployments-and-rollouts/03-declarative-deployment-updates/) | Basic |
| 04 | [Deployment Labels and Selectors](02-deployments-and-rollouts/04-deployment-labels-and-selectors/) | Basic |
| 05 | [Rolling Updates and Rollbacks](02-deployments-and-rollouts/05-rolling-updates-and-rollbacks/) | Intermediate |
| 06 | [Deployment Strategies: maxSurge and maxUnavailable](02-deployments-and-rollouts/06-maxsurge-and-maxunavailable/) | Intermediate |
| 07 | [ReplicaSets and Ownership](02-deployments-and-rollouts/07-replicasets-and-ownership/) | Intermediate |
| 08 | [Deployment Revision History and Management](02-deployments-and-rollouts/08-deployment-revision-history/) | Intermediate |
| 09 | [Canary Deployments with Label Selectors](02-deployments-and-rollouts/09-canary-deployments/) | Advanced |
| 10 | [A/B Testing with Header-Based Routing](02-deployments-and-rollouts/10-ab-testing-header-routing/) | Advanced |
| 11 | [Progressive Rollouts with Readiness Gates](02-deployments-and-rollouts/11-progressive-rollouts-readiness-gates/) | Advanced |
| 12 | [Recreate vs Rolling Update Deep Dive](02-deployments-and-rollouts/12-recreate-vs-rolling-strategies/) | Advanced |
| 13 | [Blue-Green Deployments Without Native Support](02-deployments-and-rollouts/13-blue-green-deployments/) | Insane |
| 14 | [Multi-Cluster Deployment Orchestration](02-deployments-and-rollouts/14-multi-cluster-deployment-orchestration/) | Insane |

### 03 — StatefulSets & DaemonSets

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [StatefulSets and Persistent Storage](03-statefulsets-and-daemonsets/01-statefulsets-and-persistent-storage/) | Intermediate |
| 02 | [StatefulSet Scaling and Pod Management Policy](03-statefulsets-and-daemonsets/02-statefulset-scaling-behavior/) | Intermediate |
| 03 | [Ordered vs Parallel Pod Management](03-statefulsets-and-daemonsets/03-statefulset-ordered-vs-parallel/) | Basic |
| 04 | [Partition Rolling Updates](03-statefulsets-and-daemonsets/04-statefulset-partition-rolling-updates/) | Intermediate |
| 05 | [DaemonSets with Tolerations and Node Selection](03-statefulsets-and-daemonsets/05-daemonsets-with-tolerations/) | Advanced |
| 06 | [DaemonSet Update Strategies: Rolling vs OnDelete](03-statefulsets-and-daemonsets/06-daemonset-update-strategies/) | Intermediate |
| 07 | [Running DaemonSets on Control Plane Nodes](03-statefulsets-and-daemonsets/07-daemonset-on-control-plane/) | Advanced |
| 08 | [StatefulSet DNS and Headless Service Patterns](03-statefulsets-and-daemonsets/08-statefulset-headless-dns-patterns/) | Basic |
| 09 | [PersistentVolumeClaim Retention Policies](03-statefulsets-and-daemonsets/09-pvc-retention-policy/) | Advanced |
| 10 | [StatefulSet Data Migration Between Storage Backends](03-statefulsets-and-daemonsets/10-statefulset-data-migration/) | Advanced |
| 11 | [Deploying a Distributed Database with StatefulSets](03-statefulsets-and-daemonsets/11-distributed-database-statefulset/) | Insane |
| 12 | [Zero-Downtime StatefulSet Migration with Data Continuity](03-statefulsets-and-daemonsets/12-zero-downtime-statefulset-migration/) | Insane |

### 04 — Jobs, CronJobs & Batch

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Jobs and CronJobs](04-jobs-cronjobs-and-batch/01-jobs-and-cronjobs/) | Basic |
| 02 | [Parallel Jobs with Completions and Parallelism](04-jobs-cronjobs-and-batch/02-parallel-jobs-completions/) | Basic |
| 03 | [CronJob Schedules and Concurrency Policies](04-jobs-cronjobs-and-batch/03-cronjob-schedules-and-policies/) | Basic |
| 04 | [Indexed Jobs for Work Queues](04-jobs-cronjobs-and-batch/04-indexed-jobs/) | Intermediate |
| 05 | [Job Failure Handling: backoffLimit and activeDeadlineSeconds](04-jobs-cronjobs-and-batch/05-job-failure-handling/) | Intermediate |
| 06 | [CronJob Suspend, Resume, and History Limits](04-jobs-cronjobs-and-batch/06-cronjob-suspend-and-history/) | Intermediate |
| 07 | [TTL Controller and Job Cleanup](04-jobs-cronjobs-and-batch/07-ttl-after-finished/) | Advanced |
| 08 | [Pod Failure Policy for Jobs](04-jobs-cronjobs-and-batch/08-job-pod-failure-policy/) | Advanced |
| 09 | [Building a Batch Processing Pipeline](04-jobs-cronjobs-and-batch/09-batch-processing-pipeline/) | Advanced |
| 10 | [Distributed Batch Processing with Work Queues](04-jobs-cronjobs-and-batch/10-distributed-batch-system/) | Insane |

### 05 — Services & Service Discovery

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Services: ClusterIP, NodePort, LoadBalancer](05-services-and-service-discovery/01-services-clusterip-nodeport-loadbalancer/) | Basic |
| 02 | [ClusterIP Service Fundamentals](05-services-and-service-discovery/02-clusterip-service-fundamentals/) | Basic |
| 03 | [NodePort Services and External Access](05-services-and-service-discovery/03-nodeport-services/) | Basic |
| 04 | [Service Selectors and Endpoint Objects](05-services-and-service-discovery/04-service-selectors-and-endpoints/) | Basic |
| 05 | [DNS and Service Discovery](05-services-and-service-discovery/05-dns-and-service-discovery/) | Intermediate |
| 06 | [Headless Services and StatefulSet DNS](05-services-and-service-discovery/06-headless-services-and-statefulset-dns/) | Intermediate |
| 07 | [Multi-Port Services and Named Ports](05-services-and-service-discovery/07-multi-port-services/) | Intermediate |
| 08 | [ExternalName Services for External Access](05-services-and-service-discovery/08-externalname-services/) | Intermediate |
| 09 | [EndpointSlices and Topology-Aware Routing](05-services-and-service-discovery/09-endpointslices/) | Advanced |
| 10 | [Session Affinity and Traffic Policies](05-services-and-service-discovery/10-session-affinity-and-traffic-policy/) | Advanced |
| 11 | [External and Internal Traffic Policy](05-services-and-service-discovery/11-external-traffic-policy/) | Advanced |
| 12 | [Cross-Namespace Service Discovery Patterns](05-services-and-service-discovery/12-service-discovery-patterns/) | Advanced |
| 13 | [Building Service Mesh Patterns Without a Mesh](05-services-and-service-discovery/13-service-mesh-without-mesh/) | Insane |
| 14 | [Multi-Cluster Service Discovery](05-services-and-service-discovery/14-multi-cluster-service-discovery/) | Insane |

### 06 — Ingress & Gateway API

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Ingress Controller and Routing](06-ingress-and-gateway-api/01-ingress-controller-routing/) | Basic |
| 02 | [Ingress Host-Based and Path-Based Routing](06-ingress-and-gateway-api/02-ingress-host-and-path-routing/) | Basic |
| 03 | [Ingress TLS Termination](06-ingress-and-gateway-api/03-ingress-tls-termination/) | Basic |
| 04 | [Ingress Annotations: Rate Limiting, CORS, Rewrites](06-ingress-and-gateway-api/04-ingress-annotations/) | Intermediate |
| 05 | [Gateway API Fundamentals](06-ingress-and-gateway-api/05-gateway-api-fundamentals/) | Intermediate |
| 06 | [Gateway API: HTTPRoute Advanced Matching](06-ingress-and-gateway-api/06-gateway-api-httproute/) | Intermediate |
| 07 | [IngressClass and Multiple Controllers](06-ingress-and-gateway-api/07-ingress-class-multiple-controllers/) | Intermediate |
| 08 | [Gateway API Traffic Splitting and Canary](06-ingress-and-gateway-api/08-gateway-api-traffic-splitting/) | Advanced |
| 09 | [Gateway API: TLSRoute, TCPRoute, GRPCRoute](06-ingress-and-gateway-api/09-gateway-api-tls-tcp-grpc/) | Advanced |
| 10 | [Gateway API with Different Implementations](06-ingress-and-gateway-api/10-gateway-api-implementations/) | Advanced |
| 11 | [Zero-Downtime Ingress to Gateway API Migration](06-ingress-and-gateway-api/11-zero-downtime-ingress-migration/) | Insane |
| 12 | [Multi-Tenant Ingress Platform Design](06-ingress-and-gateway-api/12-multi-tenant-ingress-platform/) | Insane |

### 07 — Network Policies & CNI

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Network Policies: Pod Isolation](07-network-policies-and-cni/01-network-policies-pod-isolation/) | Basic |
| 02 | [Default Deny All Traffic](07-network-policies-and-cni/02-default-deny-policies/) | Basic |
| 03 | [Ingress Network Policies with Pod and Namespace Selectors](07-network-policies-and-cni/03-ingress-network-policies/) | Intermediate |
| 04 | [Egress Network Policies and DNS Access](07-network-policies-and-cni/04-egress-network-policies/) | Intermediate |
| 05 | [IPBlock and CIDR-Based Network Policies](07-network-policies-and-cni/05-ipblock-and-cidr-policies/) | Intermediate |
| 06 | [Network Policies: Zero Trust Architecture](07-network-policies-and-cni/06-network-policies-zero-trust/) | Intermediate |
| 07 | [Namespace Isolation Patterns](07-network-policies-and-cni/07-namespace-isolation-patterns/) | Advanced |
| 08 | [Cilium L7 Network Policies](07-network-policies-and-cni/08-cilium-l7-policies/) | Advanced |
| 09 | [Network Policy Debugging and Troubleshooting](07-network-policies-and-cni/09-network-policy-debugging/) | Advanced |
| 10 | [CNI Plugin Comparison: Calico vs Cilium vs Flannel](07-network-policies-and-cni/10-cni-comparison/) | Advanced |
| 11 | [Full Microsegmentation of a Microservices App](07-network-policies-and-cni/11-microsegmentation-challenge/) | Insane |
| 12 | [Multi-Tenant Network Isolation Platform](07-network-policies-and-cni/12-multi-tenant-network-isolation/) | Insane |

### 08 — Configuration (ConfigMaps, Secrets, Env)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [ConfigMaps: Environment Variables and File Mounts](08-configuration/01-configmaps-environment-and-files/) | Basic |
| 02 | [Secrets Management](08-configuration/02-secrets-management/) | Basic |
| 03 | [ConfigMaps from Files, Directories, and Env Files](08-configuration/03-configmap-from-files-and-directories/) | Basic |
| 04 | [Secret Types: Opaque, TLS, Docker Registry, SSH](08-configuration/04-secret-types/) | Basic |
| 05 | [ConfigMap Volume Updates and Propagation](08-configuration/05-configmap-volume-updates/) | Intermediate |
| 06 | [Immutable ConfigMaps and Secrets](08-configuration/06-immutable-configmaps-and-secrets/) | Intermediate |
| 07 | [SubPath Mounts for Selective File Injection](08-configuration/07-subpath-mounts/) | Intermediate |
| 08 | [Environment Variable Patterns: fieldRef, resourceFieldRef](08-configuration/08-environment-variable-patterns/) | Intermediate |
| 09 | [External Secrets Operator with Cloud Providers](08-configuration/09-external-secrets-operator/) | Intermediate |
| 10 | [Sealed Secrets for GitOps](08-configuration/10-sealed-secrets/) | Advanced |
| 11 | [HashiCorp Vault CSI Provider Integration](08-configuration/11-vault-csi-provider/) | Advanced |
| 12 | [SOPS Encryption for Kubernetes Secrets](08-configuration/12-sops-with-kubernetes/) | Advanced |
| 13 | [Dynamic Configuration Reload Without Restarts](08-configuration/13-dynamic-config-reload-system/) | Insane |
| 14 | [Multi-Environment Configuration Management Platform](08-configuration/14-multi-environment-config-management/) | Insane |

### 09 — Storage & Persistent Volumes

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Persistent Volumes and Claims](09-storage-and-persistent-volumes/01-persistent-volumes-and-claims/) | Basic |
| 02 | [Storage Classes and Dynamic Provisioning](09-storage-and-persistent-volumes/02-storage-classes-dynamic-provisioning/) | Basic |
| 03 | [Volume Basics: emptyDir and hostPath](09-storage-and-persistent-volumes/03-volume-basics-emptydir-hostpath/) | Basic |
| 04 | [Ephemeral Volumes: emptyDir and Projected](09-storage-and-persistent-volumes/04-ephemeral-volumes-emptydir-projected/) | Intermediate |
| 05 | [PVC Access Modes and Volume Binding](09-storage-and-persistent-volumes/05-pvc-access-modes-and-binding/) | Intermediate |
| 06 | [Volume Expansion and Resize](09-storage-and-persistent-volumes/06-volume-expansion/) | Intermediate |
| 07 | [Volume Snapshots and Restore](09-storage-and-persistent-volumes/07-volume-snapshots/) | Intermediate |
| 08 | [CSI Drivers: EBS, EFS, Local Path](09-storage-and-persistent-volumes/08-csi-drivers/) | Advanced |
| 09 | [Volume Cloning and Data Migration](09-storage-and-persistent-volumes/09-volume-cloning-and-migration/) | Advanced |
| 10 | [ReadWriteMany Shared Storage Patterns](09-storage-and-persistent-volumes/10-rwx-shared-storage/) | Advanced |
| 11 | [Namespace Backup and Restore with Velero](09-storage-and-persistent-volumes/11-velero-backup-restore/) | Insane |
| 12 | [Cross-Cloud Storage Platform Design](09-storage-and-persistent-volumes/12-storage-platform-design/) | Insane |

### 10 — Resource Management & QoS

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Resource Requests and Limits](10-resource-management-and-qos/01-resource-requests-and-limits/) | Basic |
| 02 | [QoS Classes: Guaranteed, Burstable, BestEffort](10-resource-management-and-qos/02-qos-classes/) | Basic |
| 03 | [LimitRanges: Default and Constraints Per Namespace](10-resource-management-and-qos/03-limitranges/) | Intermediate |
| 04 | [ResourceQuotas: CPU, Memory, and Object Count](10-resource-management-and-qos/04-resource-quotas/) | Intermediate |
| 05 | [Pod Disruption Budgets](10-resource-management-and-qos/05-pod-disruption-budgets/) | Intermediate |
| 06 | [Resource Right-Sizing with VPA Recommendations](10-resource-management-and-qos/06-resource-right-sizing/) | Intermediate |
| 07 | [Priority Classes and Preemption](10-resource-management-and-qos/07-priority-classes-and-preemption/) | Advanced |
| 08 | [Pod Overhead and Runtime Classes](10-resource-management-and-qos/08-pod-overhead-and-runtime-classes/) | Advanced |
| 09 | [Resource Monitoring and Optimization Workflow](10-resource-management-and-qos/09-resource-monitoring-and-optimization/) | Advanced |
| 10 | [Multi-Tenant Resource Governance Platform](10-resource-management-and-qos/10-multi-tenant-resource-governance/) | Insane |

### 11 — RBAC & Authentication

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [RBAC: Roles and Bindings](11-rbac-and-authentication/01-rbac-roles-and-bindings/) | Basic |
| 02 | [Service Accounts and Tokens](11-rbac-and-authentication/02-service-accounts-and-tokens/) | Basic |
| 03 | [RBAC Verbs, Resources, and API Groups](11-rbac-and-authentication/03-rbac-verbs-and-resources/) | Basic |
| 04 | [Namespace-Scoped RBAC for Teams](11-rbac-and-authentication/04-namespace-scoped-rbac/) | Intermediate |
| 05 | [ClusterRoles and Aggregated ClusterRoles](11-rbac-and-authentication/05-clusterroles-and-aggregation/) | Intermediate |
| 06 | [RBAC Debugging with kubectl auth can-i](11-rbac-and-authentication/06-rbac-debugging/) | Intermediate |
| 07 | [Service Account Token Projection and Rotation](11-rbac-and-authentication/07-service-account-token-projection/) | Intermediate |
| 08 | [RBAC for CI/CD Service Accounts](11-rbac-and-authentication/08-rbac-for-cicd-pipelines/) | Advanced |
| 09 | [OIDC Authentication Integration](11-rbac-and-authentication/09-oidc-authentication/) | Advanced |
| 10 | [User Impersonation and Audit Logging](11-rbac-and-authentication/10-impersonation-and-audit/) | Advanced |
| 11 | [Multi-Tenant RBAC Platform Design](11-rbac-and-authentication/11-multi-tenant-rbac-platform/) | Insane |
| 12 | [Zero-Trust API Access Architecture](11-rbac-and-authentication/12-zero-trust-api-access/) | Insane |

### 12 — Pod Security & Hardening

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Security Contexts and Pod Security Standards](12-pod-security-and-hardening/01-security-contexts-and-pod-security/) | Basic |
| 02 | [Pod Security Admission Controller](12-pod-security-and-hardening/02-pod-security-admission/) | Basic |
| 03 | [runAsNonRoot and Read-Only Root Filesystem](12-pod-security-and-hardening/03-runasnonroot-and-readonly-filesystem/) | Intermediate |
| 04 | [Linux Capabilities: Add and Drop](12-pod-security-and-hardening/04-linux-capabilities/) | Intermediate |
| 05 | [Seccomp Profiles: RuntimeDefault and Custom](12-pod-security-and-hardening/05-seccomp-profiles/) | Intermediate |
| 06 | [Secrets Encryption at Rest](12-pod-security-and-hardening/06-secrets-encryption-at-rest/) | Intermediate |
| 07 | [AppArmor Profiles for Pods](12-pod-security-and-hardening/07-apparmor-profiles/) | Advanced |
| 08 | [Image Scanning and Admission Webhooks](12-pod-security-and-hardening/08-image-scanning-admission/) | Advanced |
| 09 | [Supply Chain Security: cosign, SBOM, SLSA](12-pod-security-and-hardening/09-supply-chain-security/) | Advanced |
| 10 | [Certificate Management with cert-manager](12-pod-security-and-hardening/10-certificate-management/) | Advanced |
| 11 | [Full CIS Benchmark Cluster Hardening](12-pod-security-and-hardening/11-full-cluster-hardening/) | Insane |
| 12 | [Runtime Threat Detection and Response](12-pod-security-and-hardening/12-runtime-threat-detection/) | Insane |

### 13 — Policy Engines (Gatekeeper, Kyverno)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [OPA Gatekeeper Installation and Basics](13-policy-engines/01-opa-gatekeeper-basics/) | Basic |
| 02 | [Kyverno Installation and Validate Policies](13-policy-engines/02-kyverno-basics/) | Basic |
| 03 | [Gatekeeper ConstraintTemplates and Constraints](13-policy-engines/03-gatekeeper-constraint-templates/) | Intermediate |
| 04 | [Kyverno Mutate Policies: Defaults and Injection](13-policy-engines/04-kyverno-mutate-policies/) | Intermediate |
| 05 | [Kyverno Generate Policies: Auto-Create Resources](13-policy-engines/05-kyverno-generate-policies/) | Intermediate |
| 06 | [Gatekeeper Mutations and Auto-Injection](13-policy-engines/06-gatekeeper-mutations/) | Advanced |
| 07 | [Kyverno Image Verification with cosign](13-policy-engines/07-kyverno-image-verification/) | Advanced |
| 08 | [Policy Testing in CI/CD Pipelines](13-policy-engines/08-policy-testing-and-cicd/) | Advanced |
| 09 | [Enterprise Policy Framework Design](13-policy-engines/09-enterprise-policy-framework/) | Insane |
| 10 | [Policy Migration: Gatekeeper to Kyverno](13-policy-engines/10-policy-migration-gatekeeper-to-kyverno/) | Insane |

### 14 — Scaling & Autoscaling

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [HPA: CPU and Memory Autoscaling](14-scaling-and-autoscaling/01-hpa-cpu-memory-autoscaling/) | Basic |
| 02 | [HPA Basics and Target Utilization](14-scaling-and-autoscaling/02-hpa-basics-target-utilization/) | Basic |
| 03 | [Manual Scaling: replicas, kubectl scale](14-scaling-and-autoscaling/03-manual-scaling-patterns/) | Basic |
| 04 | [HPA Behavior: Scale-Up and Scale-Down Policies](14-scaling-and-autoscaling/04-hpa-behavior-tuning/) | Intermediate |
| 05 | [HPA with Custom Metrics from Prometheus](14-scaling-and-autoscaling/05-hpa-custom-metrics/) | Intermediate |
| 06 | [HPA with External Metrics (SQS, CloudWatch)](14-scaling-and-autoscaling/06-hpa-external-metrics/) | Intermediate |
| 07 | [Vertical Pod Autoscaler (VPA)](14-scaling-and-autoscaling/07-vertical-pod-autoscaler/) | Intermediate |
| 08 | [KEDA: Event-Driven Autoscaling](14-scaling-and-autoscaling/08-keda-basics/) | Advanced |
| 09 | [KEDA Advanced: Multiple Triggers and Fallback](14-scaling-and-autoscaling/09-keda-advanced-triggers/) | Advanced |
| 10 | [Cluster Autoscaler: Node Scaling](14-scaling-and-autoscaling/10-cluster-autoscaler/) | Advanced |
| 11 | [Karpenter: Intelligent Node Provisioning](14-scaling-and-autoscaling/11-karpenter/) | Advanced |
| 12 | [Descheduler: Rebalancing Workloads](14-scaling-and-autoscaling/12-descheduler/) | Advanced |
| 13 | [Full Autoscaling Stack: HPA + VPA + Cluster Autoscaler](14-scaling-and-autoscaling/13-autoscaling-stack-integration/) | Insane |
| 14 | [Cost-Optimized Autoscaling Platform](14-scaling-and-autoscaling/14-cost-optimized-scaling-platform/) | Insane |

### 15 — Scheduling & Node Management

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Node Affinity and Taints/Tolerations](15-scheduling-and-node-management/01-node-affinity-and-taints/) | Basic |
| 02 | [Node Selectors and Labels](15-scheduling-and-node-management/02-node-selectors/) | Basic |
| 03 | [Taints and Tolerations Deep Dive](15-scheduling-and-node-management/03-taints-and-tolerations/) | Basic |
| 04 | [Pod Affinity and Anti-Affinity](15-scheduling-and-node-management/04-pod-affinity-anti-affinity/) | Intermediate |
| 05 | [Topology Spread Constraints](15-scheduling-and-node-management/05-topology-spread-constraints/) | Intermediate |
| 06 | [Node Maintenance: cordon, drain, uncordon](15-scheduling-and-node-management/06-node-maintenance-cordon-drain/) | Intermediate |
| 07 | [Scheduler Profiles and Plugins](15-scheduling-and-node-management/07-scheduler-profiles/) | Intermediate |
| 08 | [Custom Schedulers: Deploying a Second Scheduler](15-scheduling-and-node-management/08-custom-schedulers/) | Advanced |
| 09 | [Kubelet Configuration and Resource Reservations](15-scheduling-and-node-management/09-kubelet-configuration/) | Advanced |
| 10 | [Node Problem Detector and Remediation](15-scheduling-and-node-management/10-node-problem-detector/) | Advanced |
| 11 | [Advanced Multi-Constraint Scheduling Optimization](15-scheduling-and-node-management/11-advanced-scheduling-optimization/) | Insane |
| 12 | [Heterogeneous Cluster Scheduling Platform](15-scheduling-and-node-management/12-heterogeneous-cluster-scheduling/) | Insane |

### 16 — Observability & Monitoring

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Probes: Liveness, Readiness, and Startup](16-observability-and-monitoring/01-probes-liveness-readiness-startup/) | Basic |
| 02 | [Metrics Server and kubectl top](16-observability-and-monitoring/02-metrics-server-and-kubectl-top/) | Basic |
| 03 | [Probe Types: HTTP, TCP, Exec, and gRPC](16-observability-and-monitoring/03-probe-types-http-tcp-exec-grpc/) | Basic |
| 04 | [Kubernetes Events: Monitoring and Exporting](16-observability-and-monitoring/04-kubernetes-events/) | Basic |
| 05 | [Probe Patterns for Slow-Starting Applications](16-observability-and-monitoring/05-probe-patterns-slow-startup/) | Intermediate |
| 06 | [Graceful Shutdown with preStop and Readiness](16-observability-and-monitoring/06-graceful-shutdown-probes/) | Intermediate |
| 07 | [Logging with FluentBit DaemonSet](16-observability-and-monitoring/07-logging-with-fluentbit-daemonset/) | Intermediate |
| 08 | [Prometheus ServiceMonitor and PodMonitor](16-observability-and-monitoring/08-prometheus-servicemonitor/) | Intermediate |
| 09 | [Prometheus and Grafana Stack](16-observability-and-monitoring/09-prometheus-grafana-stack/) | Intermediate |
| 10 | [Custom Application Metrics with /metrics Endpoint](16-observability-and-monitoring/10-custom-app-metrics/) | Advanced |
| 11 | [Distributed Tracing with OpenTelemetry](16-observability-and-monitoring/11-distributed-tracing-opentelemetry/) | Advanced |
| 12 | [OpenTelemetry Collector: Pipelines and Processors](16-observability-and-monitoring/12-opentelemetry-collector/) | Advanced |
| 13 | [Loki Log Aggregation and LogQL](16-observability-and-monitoring/13-loki-log-aggregation/) | Advanced |
| 14 | [OpenTelemetry Auto-Instrumentation with Operator](16-observability-and-monitoring/14-opentelemetry-auto-instrumentation/) | Advanced |
| 15 | [Full Observability Stack: Metrics, Logs, Traces](16-observability-and-monitoring/15-full-observability-stack/) | Insane |
| 16 | [SLO-Based Alerting Platform Design](16-observability-and-monitoring/16-slo-based-alerting-platform/) | Insane |

### 17 — Service Mesh (Istio, Linkerd, Cilium)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Istio Installation and Sidecar Injection](17-service-mesh/01-istio-installation-and-injection/) | Basic |
| 02 | [Linkerd Installation and Meshing](17-service-mesh/02-linkerd-basics/) | Basic |
| 03 | [Istio Traffic Management: VirtualService and DestinationRule](17-service-mesh/03-istio-traffic-management/) | Intermediate |
| 04 | [Istio Fault Injection and Resilience Testing](17-service-mesh/04-istio-fault-injection/) | Intermediate |
| 05 | [Istio Circuit Breaking and Outlier Detection](17-service-mesh/05-istio-circuit-breaking/) | Intermediate |
| 06 | [Istio Observability: Kiali, Jaeger, Prometheus](17-service-mesh/06-istio-observability/) | Intermediate |
| 07 | [Istio Security: mTLS and AuthorizationPolicy](17-service-mesh/07-istio-security-mtls/) | Advanced |
| 08 | [Istio Traffic Mirroring and Shadowing](17-service-mesh/08-istio-traffic-mirroring/) | Advanced |
| 09 | [Cilium Sidecar-Less Service Mesh](17-service-mesh/09-cilium-service-mesh/) | Advanced |
| 10 | [Linkerd Traffic Splitting and Service Profiles](17-service-mesh/10-linkerd-traffic-splitting/) | Advanced |
| 11 | [Service Mesh Migration: No-Mesh to Full Mesh](17-service-mesh/11-service-mesh-migration/) | Insane |
| 12 | [Multi-Cluster Service Mesh Federation](17-service-mesh/12-multi-mesh-federation/) | Insane |

### 18 — Helm, Kustomize & Packaging

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Helm Chart Basics](18-helm-kustomize-and-packaging/01-helm-chart-basics/) | Basic |
| 02 | [Helm Values and Templates](18-helm-kustomize-and-packaging/02-helm-values-and-templates/) | Basic |
| 03 | [Kustomize Basics: Base and Overlays](18-helm-kustomize-and-packaging/03-kustomize-basics/) | Basic |
| 04 | [Helm Subcharts and Dependencies](18-helm-kustomize-and-packaging/04-helm-subcharts-and-dependencies/) | Intermediate |
| 05 | [Kustomize Overlays for Multi-Environment](18-helm-kustomize-and-packaging/05-kustomize-overlays/) | Intermediate |
| 06 | [Helm Hooks: Pre/Post Install and Upgrade](18-helm-kustomize-and-packaging/06-helm-hooks/) | Intermediate |
| 07 | [Kustomize Components and Replacements](18-helm-kustomize-and-packaging/07-kustomize-components-and-replacements/) | Intermediate |
| 08 | [Helm Chart Testing with ct and helm test](18-helm-kustomize-and-packaging/08-helm-testing/) | Advanced |
| 09 | [Helm Library Charts for Shared Templates](18-helm-kustomize-and-packaging/09-helm-library-charts/) | Advanced |
| 10 | [Kustomize with Helm: HelmChartInflationGenerator](18-helm-kustomize-and-packaging/10-kustomize-helm-integration/) | Advanced |
| 11 | [Helm Chart Repository and Release Platform](18-helm-kustomize-and-packaging/11-chart-repository-platform/) | Insane |
| 12 | [Migration from Helm to Kustomize (or Vice Versa)](18-helm-kustomize-and-packaging/12-packaging-strategy-migration/) | Insane |

### 19 — GitOps (ArgoCD, Flux, Argo Rollouts)

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [ArgoCD: GitOps Deployment](19-gitops/01-argocd-gitops-deployment/) | Basic |
| 02 | [ArgoCD Sync Policies and Auto-Sync](19-gitops/02-argocd-sync-policies/) | Basic |
| 03 | [Flux v2: GitRepository and Kustomization](19-gitops/03-flux-basics/) | Basic |
| 04 | [ArgoCD Application Management and Health Checks](19-gitops/04-argocd-application-management/) | Intermediate |
| 05 | [ArgoCD: App of Apps Pattern](19-gitops/05-argocd-app-of-apps/) | Intermediate |
| 06 | [ArgoCD ApplicationSets: Generator Types](19-gitops/06-argocd-applicationsets/) | Intermediate |
| 07 | [Flux HelmRelease and HelmRepository](19-gitops/07-flux-helmrelease/) | Intermediate |
| 08 | [ArgoCD Multi-Cluster Deployment](19-gitops/08-argocd-multi-cluster/) | Advanced |
| 09 | [Argo Rollouts: Canary with Analysis](19-gitops/09-argo-rollouts-canary/) | Advanced |
| 10 | [Argo Rollouts: Blue-Green with Promotion](19-gitops/10-argo-rollouts-blue-green/) | Advanced |
| 11 | [Flux Image Automation: Auto-Update Images in Git](19-gitops/11-flux-image-automation/) | Advanced |
| 12 | [GitOps Secrets Management: SOPS + Sealed Secrets](19-gitops/12-gitops-secrets-management/) | Advanced |
| 13 | [Full GitOps Platform: Multi-Env, Multi-Cluster](19-gitops/13-full-gitops-platform/) | Insane |
| 14 | [GitOps Disaster Recovery and Drift Detection](19-gitops/14-gitops-disaster-recovery/) | Insane |

### 20 — Cluster Administration & Troubleshooting

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [kubeadm Cluster Bootstrap](20-cluster-administration/01-kubeadm-cluster-setup/) | Basic |
| 02 | [kubectl Mastery: Advanced Commands and Output](20-cluster-administration/02-kubectl-mastery/) | Basic |
| 03 | [Namespace Management and Lifecycle](20-cluster-administration/03-namespace-management/) | Basic |
| 04 | [kubeadm Minor Version Upgrades](20-cluster-administration/04-kubeadm-upgrades/) | Intermediate |
| 05 | [etcd Backup and Restore](20-cluster-administration/05-etcd-backup-restore/) | Intermediate |
| 06 | [Certificate Expiry and Rotation](20-cluster-administration/06-certificate-expiry-rotation/) | Intermediate |
| 07 | [Node Maintenance: cordon, drain, uncordon with PDB](20-cluster-administration/07-node-maintenance-workflow/) | Intermediate |
| 08 | [API Server Audit Logging](20-cluster-administration/08-api-server-audit-logging/) | Intermediate |
| 09 | [etcd Disaster Recovery: Multi-Node](20-cluster-administration/09-etcd-disaster-recovery/) | Advanced |
| 10 | [Cluster Component Debugging: API Server, Scheduler, Controller Manager](20-cluster-administration/10-cluster-component-debugging/) | Advanced |
| 11 | [Kubelet Troubleshooting and Configuration](20-cluster-administration/11-kubelet-troubleshooting/) | Advanced |
| 12 | [Static Pods and Manifest Management](20-cluster-administration/12-static-pods-and-manifests/) | Advanced |
| 13 | [Cluster Networking Debugging: Pod-to-Pod, Service, DNS](20-cluster-administration/13-cluster-networking-debugging/) | Advanced |
| 14 | [Full Cluster Recovery from etcd Snapshot](20-cluster-administration/14-full-cluster-recovery/) | Insane |
| 15 | [Multi-Node Cluster Troubleshooting Challenge](20-cluster-administration/15-multi-node-troubleshooting/) | Insane |
| 16 | [Cluster Migration: kubeadm to Managed Kubernetes](20-cluster-administration/16-cluster-migration/) | Insane |

### 21 — CRDs, Operators & Extensibility

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Custom Resource Definitions: Basics](21-crds-operators-and-extensibility/01-crd-basics/) | Basic |
| 02 | [CRD Structural Schemas and Validation](21-crds-operators-and-extensibility/02-crd-structural-schemas/) | Basic |
| 03 | [CRD Printer Columns and Short Names](21-crds-operators-and-extensibility/03-crd-printer-columns/) | Intermediate |
| 04 | [CRD Validation with CEL Expressions](21-crds-operators-and-extensibility/04-crd-cel-validation/) | Intermediate |
| 05 | [The Operator Pattern: Concepts and Architecture](21-crds-operators-and-extensibility/05-operator-pattern/) | Intermediate |
| 06 | [Kubebuilder: Scaffold an Operator](21-crds-operators-and-extensibility/06-kubebuilder-scaffold/) | Advanced |
| 07 | [Controller-Runtime: Reconciliation Loop](21-crds-operators-and-extensibility/07-controller-runtime-reconciliation/) | Advanced |
| 08 | [Admission Webhooks: Validating and Mutating](21-crds-operators-and-extensibility/08-admission-webhooks/) | Advanced |
| 09 | [Build a Complete Operator from Scratch](21-crds-operators-and-extensibility/09-build-complete-operator/) | Insane |
| 10 | [Crossplane: Compositions and Cloud Resources](21-crds-operators-and-extensibility/10-crossplane-compositions/) | Insane |

### 22 — CI/CD & Developer Tools

| # | Exercise | Difficulty |
|---|----------|------------|
| 01 | [Skaffold: Kubernetes Dev Loop](22-cicd-and-developer-tools/01-skaffold-dev-loop/) | Basic |
| 02 | [Tilt: Local Kubernetes Development](22-cicd-and-developer-tools/02-tilt-local-development/) | Basic |
| 03 | [Tekton: Tasks and Pipelines](22-cicd-and-developer-tools/03-tekton-tasks-and-pipelines/) | Intermediate |
| 04 | [GitHub Actions for Kubernetes Deployment](22-cicd-and-developer-tools/04-github-actions-k8s-deploy/) | Intermediate |
| 05 | [Telepresence: Remote Debugging in Kubernetes](22-cicd-and-developer-tools/05-telepresence-remote-debugging/) | Intermediate |
| 06 | [Tekton Triggers and Event-Driven Pipelines](22-cicd-and-developer-tools/06-tekton-triggers/) | Advanced |
| 07 | [Kaniko: In-Cluster Container Image Builds](22-cicd-and-developer-tools/07-kaniko-in-cluster-builds/) | Advanced |
| 08 | [Dagger: Pipelines as Code with Go/Rust SDK](22-cicd-and-developer-tools/08-dagger-pipeline-sdk/) | Advanced |
| 09 | [Full CI/CD Platform on Kubernetes](22-cicd-and-developer-tools/09-full-cicd-platform/) | Insane |
| 10 | [Developer Platform with Backstage and K8s](22-cicd-and-developer-tools/10-developer-platform-with-backstage/) | Insane |

---

## Statistics

| Difficulty | Count | Percentage |
|-----------|-------|------------|
| Basic | 64 | 23% |
| Intermediate | 87 | 32% |
| Advanced | 82 | 30% |
| Insane | 43 | 16% |
| **Total** | **276** | **100%** |
