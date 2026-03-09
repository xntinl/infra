# 9. Resource Monitoring and Optimization Workflow

<!--
difficulty: advanced
concepts: [metrics-server, kubectl-top, resource-analysis, optimization, capacity-planning, prometheus]
tools: [kubectl, minikube, helm]
estimated_time: 50m
bloom_level: analyze
prerequisites: [10-01, 10-02, 10-03, 10-04, 10-06]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with metrics-server
- `kubectl` installed and configured
- `helm` installed
- Completion of exercises [01](../01-resource-requests-and-limits/), [02](../02-qos-classes/), [03](../03-limitranges/), [04](../04-resource-quotas/), and [06](../06-resource-right-sizing/)

Enable metrics-server:

```bash
minikube addons enable metrics-server
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** cluster resource utilization using metrics-server, kubectl top, and custom scripts
- **Create** a resource optimization workflow that identifies waste and generates right-sizing recommendations
- **Evaluate** the gap between requested resources and actual usage across namespaces

## Architecture

A resource monitoring and optimization workflow has four stages:

```
1. COLLECT           2. ANALYZE            3. RECOMMEND          4. APPLY
+---------------+   +----------------+    +----------------+    +---------------+
| metrics-server|   | Compare actual |    | Generate patch |    | Apply changes |
| kubectl top   |-->| vs requested   |--->| commands for   |--->| Verify no     |
| Prometheus    |   | Identify waste |    | right-sizing   |    | regressions   |
+---------------+   +----------------+    +----------------+    +---------------+
```

## The Challenge

### Task 1: Deploy a Mixed Workload

Create a namespace with workloads that exhibit different resource patterns:

```yaml
# namespace-monitoring.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: monitoring-lab
```

```yaml
# deployment-over-provisioned.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-over-provisioned
  namespace: monitoring-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: web-over
  template:
    metadata:
      labels:
        app: web-over
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: "500m"           # Nginx idles at ~5m
              memory: "512Mi"       # Nginx uses ~30Mi
            limits:
              cpu: "1"
              memory: "1Gi"
```

```yaml
# deployment-under-provisioned.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cpu-bound-app
  namespace: monitoring-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cpu-bound
  template:
    metadata:
      labels:
        app: cpu-bound
    spec:
      containers:
        - name: stress
          image: busybox:1.37
          command:
            - sh
            - -c
            - |
              while true; do
                i=0; while [ $i -lt 1000000 ]; do i=$((i+1)); done
                sleep 1
              done
          resources:
            requests:
              cpu: "50m"            # Likely too low for the workload
              memory: "32Mi"
            limits:
              cpu: "100m"
              memory: "64Mi"
```

```yaml
# deployment-right-sized.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: redis-cache
  namespace: monitoring-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: redis-cache
  template:
    metadata:
      labels:
        app: redis-cache
    spec:
      containers:
        - name: redis
          image: redis:7
          resources:
            requests:
              cpu: "100m"
              memory: "128Mi"
            limits:
              cpu: "200m"
              memory: "256Mi"
```

### Task 2: Collect Resource Metrics

After pods run for a few minutes, gather utilization data:

```bash
kubectl top pods -n monitoring-lab
kubectl top pods -n monitoring-lab --containers
kubectl top nodes
```

### Task 3: Build an Analysis Script

Create a Job that compares requested versus actual resource usage:

```yaml
# job-resource-audit.yaml
apiVersion: batch/v1
kind: Job
metadata:
  name: resource-audit
  namespace: monitoring-lab
spec:
  template:
    spec:
      serviceAccountName: default
      containers:
        - name: auditor
          image: bitnami/kubectl:1.29
          command:
            - sh
            - -c
            - |
              echo "=== Resource Audit Report ==="
              echo "Date: $(date)"
              echo ""
              echo "=== Pod Resource Usage vs Requests ==="
              kubectl top pods -n monitoring-lab --no-headers | while read name cpu mem; do
                req_cpu=$(kubectl get pod $name -n monitoring-lab -o jsonpath='{.spec.containers[0].resources.requests.cpu}' 2>/dev/null)
                req_mem=$(kubectl get pod $name -n monitoring-lab -o jsonpath='{.spec.containers[0].resources.requests.memory}' 2>/dev/null)
                echo "Pod: $name"
                echo "  CPU: actual=$cpu requested=$req_cpu"
                echo "  MEM: actual=$mem requested=$req_mem"
                echo ""
              done
              echo "=== Node Capacity ==="
              kubectl top nodes --no-headers
      restartPolicy: Never
  backoffLimit: 1
```

### Task 4: Identify Optimization Opportunities

Based on the audit, categorize each workload:

- **Over-provisioned**: actual usage < 20% of requests -> reduce requests
- **Under-provisioned**: actual usage > 80% of limits -> increase limits
- **Right-sized**: actual usage between 40-70% of requests -> no changes needed

### Task 5: Create a Namespace Resource Summary

```bash
kubectl describe namespace monitoring-lab
kubectl get resourcequota -n monitoring-lab
```

Calculate the total requested vs allocatable ratio for the namespace.

## Suggested Steps

1. Deploy all workloads and wait for them to stabilize (2-3 minutes)
2. Collect metrics with `kubectl top`
3. Run the resource audit Job
4. Classify each workload as over/under/right-provisioned
5. Generate recommended patches for over-provisioned workloads
6. Apply recommendations and verify workloads remain healthy
7. Set up a ResourceQuota based on actual usage patterns

## Verify What You Learned

```bash
kubectl top pods -n monitoring-lab
kubectl top nodes
kubectl logs job/resource-audit -n monitoring-lab
kubectl get pods -n monitoring-lab -o custom-columns=NAME:.metadata.name,CPU_REQ:.spec.containers[0].resources.requests.cpu,MEM_REQ:.spec.containers[0].resources.requests.memory,QOS:.status.qosClass
```

## Cleanup

```bash
kubectl delete namespace monitoring-lab
```

## What's Next

You now have the skills to monitor and optimize individual workloads. The final exercise challenges you to build a comprehensive multi-tenant resource governance platform. Continue to [exercise 10 (Multi-Tenant Resource Governance Platform)](../10-multi-tenant-resource-governance/).

## Summary

- **metrics-server** provides real-time CPU and memory usage for pods and nodes via `kubectl top`
- Comparing **actual usage vs requested resources** reveals over-provisioned and under-provisioned workloads
- A systematic **audit workflow** (collect, analyze, recommend, apply) prevents resource waste
- Most workloads are **over-provisioned by 3-10x**, making this the highest-impact optimization
- Combine monitoring data with **LimitRanges and ResourceQuotas** to enforce good resource hygiene

## Reference

- [Resource Metrics Pipeline](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/)
- [Tools for Monitoring Resources](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-usage-monitoring/)
- [Resource Management for Pods and Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
