<!--
difficulty: basic
concepts: [horizontal-pod-autoscaler, target-utilization, average-value, hpa-algorithm, desired-replicas]
tools: [kubectl, metrics-server]
estimated_time: 25m
bloom_level: understand
prerequisites: [deployments, resource-requests-and-limits, hpa-cpu-memory-autoscaling]
-->

# 14.02 - HPA Basics and Target Utilization

## Why This Matters

Knowing *that* the HPA scales pods is not enough -- you need to understand *how* it decides the target replica count. A misconfigured target can lead to under-scaling (outages) or over-scaling (wasted resources). The HPA algorithm is deterministic: once you know the formula, you can predict exactly how many replicas it will request.

## What You Will Learn

- The HPA replica calculation formula: `desiredReplicas = ceil(currentReplicas * (currentMetric / desiredMetric))`
- The difference between `Utilization`, `AverageValue`, and `Value` target types
- How the 10% tolerance band prevents unnecessary scaling
- How `--horizontal-pod-autoscaler-tolerance` affects decisions

## Step-by-Step Guide

### 1. Create the Namespace and Deployment

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: hpa-basics
```

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: compute-app
  namespace: hpa-basics
spec:
  replicas: 2                   # start with 2 replicas to observe the formula
  selector:
    matchLabels:
      app: compute-app
  template:
    metadata:
      labels:
        app: compute-app
    spec:
      containers:
        - name: app
          image: registry.k8s.io/hpa-example
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 100m         # each pod requests 100m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: compute-app
  namespace: hpa-basics
spec:
  selector:
    app: compute-app
  ports:
    - port: 80
      targetPort: 80
```

### 2. HPA with Utilization Target

```yaml
# hpa-utilization.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: compute-hpa-util
  namespace: hpa-basics
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: compute-app
  minReplicas: 1
  maxReplicas: 8
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization          # percentage of requests
          averageUtilization: 50     # target: 50% of 100m = 50m per pod
```

With 2 replicas each using 80m CPU (80% utilization), the formula yields:
`ceil(2 * (80 / 50)) = ceil(3.2) = 4` replicas.

### 3. HPA with AverageValue Target

```yaml
# hpa-averagevalue.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: compute-hpa-avg
  namespace: hpa-basics
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: compute-app
  minReplicas: 1
  maxReplicas: 8
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: AverageValue         # absolute value per pod, not percentage
          averageValue: 50m          # target: 50 millicores per pod
```

`AverageValue` is useful when you want a fixed per-pod target regardless of what `requests` is set to.

### 4. Generate Load

```yaml
# load-generator.yaml
apiVersion: v1
kind: Pod
metadata:
  name: load-generator
  namespace: hpa-basics
spec:
  containers:
    - name: busybox
      image: busybox:1.37
      command:
        - /bin/sh
        - -c
        - "while true; do wget -q -O- http://compute-app.hpa-basics.svc.cluster.local; done"
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
kubectl apply -f hpa-utilization.yaml
```

## Common Mistakes

1. **Confusing Utilization with AverageValue** -- `Utilization: 50` means 50% of requests (a percentage). `AverageValue: 50m` means 50 millicores absolute. They produce different replica counts.
2. **Not accounting for the tolerance band** -- The HPA will not scale if the ratio `currentMetric / desiredMetric` is within 0.9-1.1 (the default 10% tolerance).
3. **Expecting instant scaling** -- The HPA evaluates every 15 seconds by default (`--horizontal-pod-autoscaler-sync-period`). Combined with stabilization windows, changes are not immediate.
4. **Forgetting that multiple metrics take the max** -- When specifying both CPU and memory metrics, the HPA uses whichever metric demands the most replicas.

## Verify

```bash
# 1. Check the HPA status
kubectl get hpa -n hpa-basics

# 2. Watch the HPA in real time
kubectl get hpa -n hpa-basics --watch

# 3. Generate load and observe
kubectl apply -f load-generator.yaml

# 4. After 1-2 min, check the calculated replicas
kubectl describe hpa compute-hpa-util -n hpa-basics

# 5. Verify the algorithm output in events
kubectl get events -n hpa-basics --sort-by='.lastTimestamp'

# 6. Stop load and observe scale-down
kubectl delete pod load-generator -n hpa-basics
kubectl get hpa -n hpa-basics --watch
```

## Cleanup

```bash
kubectl delete namespace hpa-basics
```

## What's Next

Now that you understand how the HPA calculates replicas, the next exercise covers basic manual scaling patterns using `kubectl scale` and declarative replica management: [14.03 - Manual Scaling: replicas, kubectl scale](../03-manual-scaling-patterns/03-manual-scaling-patterns.md).

## Summary

- The HPA formula is `desiredReplicas = ceil(currentReplicas * (currentMetric / desiredMetric))`
- `Utilization` expresses the target as a percentage of `resources.requests`
- `AverageValue` expresses the target as an absolute value per pod
- The HPA includes a 10% tolerance band to avoid unnecessary scaling
- The HPA evaluates metrics every 15 seconds by default
- When multiple metrics are specified, the highest desired replica count wins

## References

- [HPA Algorithm Details](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#algorithm-details)
- [Horizontal Pod Autoscaling](https://kubernetes.io/docs/concepts/workloads/autoscaling/)

## Additional Resources

- [autoscaling/v2 API Reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/horizontal-pod-autoscaler-v2/)
- [HPA Walkthrough](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/)
