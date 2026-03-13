<!--
difficulty: insane
concepts: [multi-constraint-scheduling, topology-spread, affinity-rules, priority-preemption, resource-quotas, scheduler-tuning]
tools: [kubectl]
estimated_time: 60m
bloom_level: create
prerequisites: [topology-spread-constraints, pod-affinity-anti-affinity, node-affinity-and-taints, scheduler-profiles, taints-and-tolerations]
-->

# 15.11 - Advanced Multi-Constraint Scheduling Optimization

## Scenario

You are the platform engineer for a financial services firm running a Kubernetes cluster across 3 availability zones with 4 node types:

- **Standard nodes** (8 CPU, 32Gi) -- labeled `tier=standard` in all 3 zones
- **High-memory nodes** (8 CPU, 64Gi) -- labeled `tier=highmem` in zones a and b only
- **GPU nodes** (4 CPU, 16Gi, 1 GPU) -- labeled `tier=gpu`, tainted `accelerator=nvidia:NoSchedule`, in zone a only
- **Spot nodes** (4 CPU, 16Gi) -- labeled `tier=spot`, tainted `lifecycle=spot:PreferNoSchedule`, in all 3 zones

You must schedule the following workloads simultaneously, satisfying all constraints:

- **Trading engine** (6 replicas) -- must spread across all 3 zones with maxSkew 1, must run on standard nodes, must have pod anti-affinity (one per node), PriorityClass `critical` (1000000)
- **Risk calculator** (4 replicas) -- must run on high-memory nodes, preferred spread across zones, must be co-located with at least one trading engine pod (same zone), PriorityClass `high` (500000)
- **ML model server** (3 replicas) -- must run on GPU nodes (tolerate the taint), spread across GPU nodes, PriorityClass `standard` (100000)
- **Market data ingest** (8 replicas) -- can run on any node type including spot, prefer spot nodes (tolerate the taint), spread across zones with maxSkew 2, PriorityClass `low` (10000)
- **Log collector** (DaemonSet) -- must run on every node including GPU and spot (tolerate all taints)

## Constraints

1. Trading engine pods that cannot spread evenly across zones must fail scheduling (DoNotSchedule), not silently imbalance
2. Risk calculator must use `preferredDuringSchedulingIgnoredDuringExecution` for zone spread (soft) but `requiredDuringSchedulingIgnoredDuringExecution` for co-location with trading engine (hard, zone-level)
3. ML model server must use `nodeSelector` for GPU nodes AND tolerate `accelerator=nvidia:NoSchedule`
4. Market data ingest must prefer spot nodes with weight 80, but tolerate spot taint so it CAN run on spot nodes
5. All workloads must have resource requests defined; no pod may request more than 2 CPU or 4Gi memory
6. PriorityClasses must be created with correct preemption behavior: `critical` and `high` can preempt; `low` sets `preemptionPolicy: Never`
7. A ResourceQuota in the `trading` namespace must limit total CPU to 40 and memory to 128Gi
8. The log collector DaemonSet must use `operator: Exists` with no key to tolerate all taints
9. No workload may use `nodeName` (direct node assignment) -- all placement must go through the scheduler

## Success Criteria

1. Trading engine has exactly 2 pods per zone (6 replicas / 3 zones)
2. Risk calculator pods run only on high-memory nodes in zones a and b
3. Risk calculator pods share a zone with at least one trading engine pod
4. ML model server pods run only on GPU nodes in zone a
5. Market data ingest pods are distributed across zones with at most 2 pods difference
6. Log collector runs on every node (standard, highmem, gpu, spot)
7. When cluster resources are tight, low-priority market data pods are NOT preempting other pods
8. ResourceQuota prevents over-allocation in the trading namespace
9. All pods are Running (no Pending pods due to unsatisfiable constraints)

## Verification Commands

```bash
# Trading engine: check zone distribution
kubectl get pods -n trading -l app=trading-engine -o wide
kubectl get pods -n trading -l app=trading-engine -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | while read node; do kubectl get node "$node" -o jsonpath='{.metadata.labels.topology\.kubernetes\.io/zone}'; echo; done | sort | uniq -c

# Risk calculator: verify node tier and zone co-location with trading engine
kubectl get pods -n trading -l app=risk-calculator -o wide

# ML model server: verify GPU node placement
kubectl get pods -n ml -l app=model-server -o wide
kubectl get pods -n ml -l app=model-server -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | while read node; do kubectl get node "$node" -o jsonpath='{.metadata.labels.tier}'; echo; done

# Market data: check zone spread
kubectl get pods -n trading -l app=market-data -o wide

# Log collector: verify running on ALL nodes
kubectl get pods -n kube-system -l app=log-collector -o wide
kubectl get nodes --no-headers | wc -l
kubectl get pods -n kube-system -l app=log-collector --no-headers | wc -l

# PriorityClasses
kubectl get priorityclass

# ResourceQuota
kubectl describe resourcequota -n trading

# No pending pods
kubectl get pods -A --field-selector status.phase=Pending
```

## Cleanup

```bash
kubectl delete namespace trading ml
kubectl delete priorityclass critical high standard low
# Remove taints from nodes
kubectl taint nodes -l tier=gpu accelerator=nvidia:NoSchedule-
kubectl taint nodes -l tier=spot lifecycle=spot:PreferNoSchedule-
# Remove labels
kubectl label nodes --all tier-
```
