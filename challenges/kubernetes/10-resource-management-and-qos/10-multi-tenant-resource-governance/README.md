# 10. Multi-Tenant Resource Governance Platform

<!--
difficulty: insane
concepts: [multi-tenancy, resource-governance, namespace-isolation, quota-hierarchy, admission-control, cost-allocation]
tools: [kubectl, helm, minikube]
estimated_time: 120m
bloom_level: create
prerequisites: [10-01, 10-02, 10-03, 10-04, 10-05, 10-06, 10-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` and `helm` installed and configured
- Completion of all previous exercises in this category

## The Scenario

You are the platform engineer for a company with four product teams sharing a single Kubernetes cluster. Each team has different resource budgets, SLA requirements, and workload profiles. You must design a resource governance platform that enforces fair sharing, prevents noisy neighbors, provides cost visibility, and ensures critical workloads always have resources.

Teams:
- **Platform** (critical infrastructure): 30% of cluster budget, Guaranteed QoS required
- **Backend** (API services): 30% of cluster, Burstable QoS allowed, PDB mandatory
- **Data** (batch processing): 25% of cluster, BestEffort allowed for batch jobs
- **Experiments** (development): 15% of cluster, strict limits, lowest priority

## Constraints

1. Create four namespaces (one per team) with appropriate labels for team, tier, and cost-center.
2. Configure ResourceQuotas per namespace that enforce the budget split. Assume the cluster has 8 CPU cores and 16Gi memory total. Platform gets 2.4 CPU / 4.8Gi, Backend gets 2.4 CPU / 4.8Gi, Data gets 2 CPU / 4Gi, Experiments gets 1.2 CPU / 2.4Gi.
3. Create LimitRanges per namespace: Platform requires requests == limits (Guaranteed only). Backend allows 2x burst ratio. Data allows 4x burst. Experiments has strict low maximums.
4. Create PriorityClasses: `platform-critical` (10000), `backend-standard` (5000), `data-batch` (1000), `experiment-low` (100). Pods in each namespace must use the corresponding PriorityClass.
5. Create PodDisruptionBudgets for all Deployments in the Platform and Backend namespaces (minAvailable: 50%).
6. Deploy representative workloads in each namespace: Platform runs a 3-replica nginx with Guaranteed QoS. Backend runs a 3-replica redis with Burstable QoS. Data runs a Job with 5 parallel workers. Experiments runs a single-pod deployment.
7. Create a governance audit CronJob that runs every 5 minutes and reports: quota usage per namespace, pods violating QoS expectations, pods without PDBs in Platform/Backend, and total cluster utilization.
8. Demonstrate the preemption hierarchy: when Experiments tries to exceed its quota, Platform pods are never affected.

## Success Criteria

1. All four namespaces exist with correct ResourceQuotas and LimitRanges.
2. Attempting to create a BestEffort pod in the Platform namespace fails (LimitRange enforces Guaranteed).
3. Attempting to exceed the Backend quota returns a clear error message.
4. The Data namespace successfully runs batch Jobs with low-priority pods.
5. When cluster resources are scarce, Experiment pods are preempted before any other team's pods.
6. The audit CronJob produces a readable report covering all governance checks.
7. PDBs prevent more than 50% of Platform and Backend pods from being evicted simultaneously.

## Hints

<details>
<summary>Hint 1: Enforcing Guaranteed QoS via LimitRange</summary>

Set `maxLimitRequestRatio` to `1` for both CPU and memory in the Platform namespace LimitRange. This forces requests == limits, which produces Guaranteed QoS.

</details>

<details>
<summary>Hint 2: Governance audit CronJob</summary>

Use `bitnami/kubectl` as the container image. The script should iterate over target namespaces, check quota usage with `kubectl describe resourcequota`, list pods and their QoS classes, and verify PDB existence for Deployments.

</details>

<details>
<summary>Hint 3: Demonstrating preemption hierarchy</summary>

Fill the Experiments namespace to its quota limit, then create a Platform pod that exceeds available cluster resources. The scheduler should preempt Experiment pods (priority 100) to make room for Platform pods (priority 10000).

</details>

## Verification Commands

```bash
kubectl get resourcequota -A
kubectl get limitrange -A
kubectl get priorityclass
kubectl get pdb -A

for ns in platform backend data experiments; do
  echo "=== $ns ==="
  kubectl describe resourcequota -n team-$ns
  kubectl get pods -n team-$ns -o custom-columns=NAME:.metadata.name,QOS:.status.qosClass,PRIORITY:.spec.priority
done

kubectl logs $(kubectl get pod -l job-name=governance-audit -o name --sort-by=.metadata.creationTimestamp | tail -1)

# Verify QoS enforcement
kubectl run test-besteffort --namespace=team-platform --image=busybox:1.37 --command -- sleep 10
# Should fail because LimitRange forces resource specs
```

## Cleanup

```bash
kubectl delete namespace team-platform team-backend team-data team-experiments --ignore-not-found
kubectl delete priorityclass platform-critical backend-standard data-batch experiment-low --ignore-not-found
kubectl delete cronjob governance-audit --ignore-not-found
```
