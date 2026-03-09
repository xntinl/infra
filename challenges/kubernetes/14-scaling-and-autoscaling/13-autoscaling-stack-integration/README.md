<!--
difficulty: insane
concepts: [hpa-vpa-integration, cluster-autoscaler, multi-layer-scaling, resource-optimization, scaling-conflicts]
tools: [kubectl, metrics-server, vpa, cluster-autoscaler]
estimated_time: 60m
bloom_level: create
prerequisites: [hpa-cpu-memory-autoscaling, vertical-pod-autoscaler, cluster-autoscaler, hpa-behavior-tuning]
-->

# 14.13 - Full Autoscaling Stack: HPA + VPA + Cluster Autoscaler

## Scenario

You are the platform engineer for a SaaS company launching a new product. The application has three tiers:

- **API Gateway** -- receives all incoming HTTP traffic, CPU-bound, needs fast horizontal scaling
- **Worker Service** -- processes background jobs from a queue, memory-intensive, needs right-sized resources
- **Cache Layer** -- Redis StatefulSet, stable resource profile, must not be evicted during node scale-down

Your task is to design an autoscaling stack where:
- The API Gateway uses HPA for horizontal scaling based on CPU
- The Worker Service uses VPA to right-size memory requests (with HPA on a custom metric for horizontal scaling)
- The Cluster Autoscaler provisions and decommissions nodes as aggregate demand changes
- No autoscaler conflicts with another (HPA and VPA must not target the same resource)

## Constraints

1. The API Gateway HPA must target 60% CPU utilization with a minimum of 3 and maximum of 30 replicas
2. The Worker Service VPA must operate in `Auto` mode controlling only memory (not CPU)
3. The Worker Service HPA must scale on a custom metric (`jobs_pending`) with a threshold of 10 per pod
4. The Cache Layer must have `cluster-autoscaler.kubernetes.io/safe-to-evict: "false"` and a PDB with `minAvailable: 2`
5. Cluster Autoscaler must use `--expander=least-waste` and `--scale-down-utilization-threshold=0.5`
6. HPA behavior for the API Gateway: scale-up with zero stabilization, scale-down with 300s stabilization
7. All Deployments must have resource requests and limits defined
8. The Worker VPA must have `minAllowed` of 64Mi and `maxAllowed` of 4Gi for memory

## Success Criteria

1. Under load, the API Gateway HPA scales from 3 to at least 10 replicas
2. The Cluster Autoscaler adds at least one node to accommodate the new pods
3. The Worker VPA adjusts memory requests to match actual consumption (verify via `kubectl describe vpa`)
4. The Cache StatefulSet pods are never evicted during node scale-down
5. When load subsides, the API Gateway scales down to 3 replicas over 5+ minutes
6. The Cluster Autoscaler eventually removes the extra node
7. No HPA/VPA conflicts: `kubectl describe hpa` shows no error conditions related to VPA

## Verification Commands

```bash
# Check all autoscaler components are running
kubectl get pods -n kube-system | grep -E "(metrics-server|vpa|cluster-autoscaler)"

# Verify API Gateway HPA
kubectl get hpa api-gateway-hpa -n production
kubectl describe hpa api-gateway-hpa -n production

# Verify Worker VPA recommendations
kubectl get vpa worker-vpa -n production
kubectl describe vpa worker-vpa -n production

# Verify Worker HPA (custom metric)
kubectl get hpa worker-hpa -n production

# Verify Cache PDB
kubectl get pdb cache-pdb -n production

# Check node count
kubectl get nodes

# Verify no eviction of cache pods during scale-down
kubectl get events -n production --sort-by='.lastTimestamp' | grep cache

# Verify VPA only controls memory (not CPU)
kubectl get vpa worker-vpa -n production -o jsonpath='{.spec.resourcePolicy.containerPolicies[0].controlledResources}'

# Load test the API Gateway
kubectl run load-test --rm -i --image=busybox:1.37 -- sh -c \
  "while true; do wget -q -O- http://api-gateway.production.svc; done"
```

## Cleanup

```bash
kubectl delete namespace production
```
