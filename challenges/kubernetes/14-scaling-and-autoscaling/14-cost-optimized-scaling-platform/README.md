<!--
difficulty: insane
concepts: [cost-optimization, spot-instances, karpenter, keda, scaling-policies, resource-quotas, priority-classes]
tools: [kubectl, helm, karpenter, keda]
estimated_time: 75m
bloom_level: create
prerequisites: [autoscaling-stack-integration, karpenter, keda-advanced-triggers, cluster-autoscaler]
-->

# 14.14 - Cost-Optimized Autoscaling Platform

## Scenario

Your company runs a multi-tenant Kubernetes platform with strict cost targets. The CTO has mandated a 40% reduction in compute costs without sacrificing reliability. The platform hosts three workload classes:

- **Critical services** (payment, auth) -- must run on on-demand instances with guaranteed resources
- **Standard services** (API, web) -- can tolerate brief disruptions, eligible for spot instances
- **Batch workloads** (data pipelines, ML training) -- fully interruptible, should use cheapest capacity

You must design a scaling platform that:
- Uses Karpenter with multiple NodePools for each workload class
- Uses KEDA with scale-to-zero for batch workloads
- Implements PriorityClasses to ensure critical workloads preempt batch workloads during resource contention
- Uses ResourceQuotas to prevent any single tenant from consuming all cluster resources

## Constraints

1. Create three Karpenter NodePools: `critical` (on-demand only, m5/m6i), `standard` (on-demand + spot, broad instance selection), `batch` (spot only, cheapest instances)
2. Critical NodePool must set `disruption.consolidationPolicy: WhenEmpty` (never consolidate running workloads)
3. Standard NodePool must use `consolidationPolicy: WhenEmptyOrUnderutilized` with a 60s `consolidateAfter`
4. Batch NodePool must allow Graviton (arm64) instances and use `consolidationPolicy: WhenEmptyOrUnderutilized` with 10s `consolidateAfter`
5. Create three PriorityClasses: `critical` (1000000), `standard` (100000), `batch` (10000, preemptionPolicy: Never)
6. Batch workloads must use KEDA ScaledObjects with `minReplicaCount: 0` (scale-to-zero when idle)
7. Each tenant namespace must have a ResourceQuota limiting CPU to 20 cores and memory to 40Gi
8. Standard services must have HPA with behavior tuning: 0s scale-up stabilization, 180s scale-down stabilization
9. All pods must have resource requests; batch pods must tolerate the taint `workload-type=batch:NoSchedule`
10. Karpenter total cluster limits: 200 CPU, 400Gi memory across all NodePools

## Success Criteria

1. Critical pods always run on on-demand instances (`kubectl get pods -o wide` + node labels)
2. Standard pods run on a mix of on-demand and spot instances
3. Batch pods run exclusively on spot/Graviton instances and scale to zero when idle
4. Scaling batch workloads from 0 triggers Karpenter to provision spot nodes within 60 seconds
5. ResourceQuota prevents a tenant from exceeding 20 CPU cores
6. PriorityClass preemption evicts batch pods (not critical or standard) during resource contention
7. When standard workload scales down, Karpenter consolidates underutilized nodes within 2 minutes
8. Batch KEDA ScaledObjects show `Active: False` when idle and `Active: True` when triggered

## Verification Commands

```bash
# Check NodePools
kubectl get nodepool
kubectl describe nodepool critical
kubectl describe nodepool standard
kubectl describe nodepool batch

# Check PriorityClasses
kubectl get priorityclass

# Check ResourceQuotas
kubectl get resourcequota -A

# Verify critical pods on on-demand nodes
kubectl get pods -n critical-ns -o wide
kubectl get nodes -l karpenter.sh/capacity-type=on-demand

# Verify batch pods on spot nodes
kubectl get pods -n batch-ns -o wide
kubectl get nodes -l karpenter.sh/capacity-type=spot

# Check KEDA scale-to-zero
kubectl get scaledobject -n batch-ns
kubectl get deployment -n batch-ns

# Test ResourceQuota enforcement
kubectl describe resourcequota -n tenant-a

# Check Karpenter consolidation logs
kubectl logs -n kube-system -l app.kubernetes.io/name=karpenter --tail=100 | grep consolidat

# Simulate resource contention and verify preemption
kubectl get events -A --sort-by='.lastTimestamp' | grep -i preempt
```

## Cleanup

```bash
kubectl delete namespace critical-ns standard-ns batch-ns tenant-a
kubectl delete nodepool critical standard batch
kubectl delete ec2nodeclass critical standard batch
kubectl delete priorityclass critical standard batch
```
