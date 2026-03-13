<!--
difficulty: basic
concepts: [horizontal-pod-autoscaler, cpu-metrics, memory-metrics, metrics-server, autoscaling-v2]
tools: [kubectl, metrics-server]
estimated_time: 30m
bloom_level: understand
prerequisites: [deployments, services, resource-requests-and-limits]
-->

# 14.01 - HPA: CPU and Memory Autoscaling

## Why This Matters

Applications in production face variable load. A checkout service might handle ten requests per minute overnight but thousands during a flash sale. The **HorizontalPodAutoscaler** (HPA) watches resource utilization and automatically adjusts replica counts so your application scales with demand without manual intervention or over-provisioning.

## What You Will Learn

- How the HPA control loop observes CPU and memory utilization
- How `averageUtilization` targets relate to resource requests
- How `behavior` policies control scale-up and scale-down velocity
- How to combine CPU and memory metrics in a single HPA

## Step-by-Step Guide

### 1. Verify metrics-server Is Running

The HPA depends on metrics-server to read CPU and memory usage from kubelets.

```bash
# Check that metrics-server is deployed
kubectl get deployment metrics-server -n kube-system

# If not installed, deploy it
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
```

### 2. Create a Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: hpa-lab
```

### 3. Deploy an Application with Resource Requests

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: hpa-lab
spec:
  replicas: 1                    # start with a single replica
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: web-app
          image: registry.k8s.io/hpa-example   # simple CPU-burning web server
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 200m          # HPA uses requests as the 100% baseline
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
---
apiVersion: v1
kind: Service
metadata:
  name: web-app
  namespace: hpa-lab
spec:
  selector:
    app: web-app
  ports:
    - port: 80
      targetPort: 80
```

### 4. Create an HPA Targeting CPU Utilization

```yaml
# hpa-cpu.yaml
apiVersion: autoscaling/v2       # v2 supports multiple metrics and behavior
kind: HorizontalPodAutoscaler
metadata:
  name: web-app-hpa
  namespace: hpa-lab
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web-app
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50   # scale when average CPU exceeds 50% of requests
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30    # wait 30s before another scale-up decision
      policies:
        - type: Pods
          value: 2                      # add at most 2 pods per period
          periodSeconds: 60
        - type: Percent
          value: 100                    # or double the current count
          periodSeconds: 60
      selectPolicy: Max                 # pick whichever policy adds more pods
    scaleDown:
      stabilizationWindowSeconds: 300   # wait 5 min to avoid flapping
      policies:
        - type: Pods
          value: 1                      # remove at most 1 pod per period
          periodSeconds: 60
      selectPolicy: Min                 # pick the most conservative removal
```

### 5. Create an HPA with CPU and Memory (Alternative)

```yaml
# hpa-cpu-memory.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: web-app-hpa-multi
  namespace: hpa-lab
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: web-app
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: 70   # memory target is typically higher than CPU
```

When multiple metrics are specified, the HPA calculates desired replicas for each and picks the **highest** value.

### 6. Generate Load

```yaml
# load-generator.yaml
apiVersion: v1
kind: Pod
metadata:
  name: load-generator
  namespace: hpa-lab
spec:
  containers:
    - name: busybox
      image: busybox:1.37
      command:
        - /bin/sh
        - -c
        - "while true; do wget -q -O- http://web-app.hpa-lab.svc.cluster.local; done"
```

### Apply Everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
kubectl apply -f hpa-cpu.yaml
```

## Common Mistakes

1. **Missing resource requests** -- The HPA cannot calculate utilization percentage without `resources.requests`. Pods without requests always report 0% or unknown utilization.
2. **Using autoscaling/v1 for memory metrics** -- The v1 API only supports CPU. You need `autoscaling/v2` for memory or custom metrics.
3. **Setting minReplicas to 0** -- Standard HPA does not support scaling to zero. The minimum is 1.
4. **Ignoring stabilization windows** -- Without `behavior.scaleDown.stabilizationWindowSeconds`, the HPA can rapidly oscillate between scaling up and down (flapping).

## Verify

```bash
# 1. Confirm metrics-server returns data
kubectl top nodes
kubectl top pods -n hpa-lab

# 2. Check initial HPA state -- TARGETS should show current%/50%
kubectl get hpa -n hpa-lab

# 3. Watch HPA in one terminal
kubectl get hpa -n hpa-lab --watch

# 4. In another terminal, start the load generator
kubectl apply -f load-generator.yaml

# 5. After 1-2 minutes, observe replicas increasing
kubectl get hpa -n hpa-lab
kubectl top pods -n hpa-lab
kubectl get pods -n hpa-lab

# 6. Inspect HPA events for scaling decisions
kubectl describe hpa web-app-hpa -n hpa-lab

# 7. Confirm Deployment replica count increased
kubectl get deployment web-app -n hpa-lab

# 8. Stop the load
kubectl delete pod load-generator -n hpa-lab

# 9. Watch scale-down (takes ~5 min due to stabilization window)
kubectl get hpa -n hpa-lab --watch

# 10. Final state should return to 1 replica
kubectl get hpa -n hpa-lab
kubectl get pods -n hpa-lab
```

## Cleanup

```bash
kubectl delete namespace hpa-lab
```

## What's Next

In the next exercise you will explore HPA target utilization types in more detail -- `Utilization` vs `AverageValue` vs `Value` -- and learn how the HPA algorithm calculates desired replicas: [14.02 - HPA Basics and Target Utilization](../02-hpa-basics-target-utilization/02-hpa-basics-target-utilization.md).

## Summary

- The HPA is a control loop that adjusts Deployment replicas based on observed metrics
- `averageUtilization` is calculated as a percentage of the pod's `resources.requests`
- `autoscaling/v2` supports multiple metrics; the HPA picks the metric that demands the most replicas
- `behavior` policies control how fast the HPA scales up and down
- `stabilizationWindowSeconds` prevents flapping by requiring metrics to stay above/below threshold for a period
- metrics-server must be running for Resource-type metrics to work

## References

- [Horizontal Pod Autoscaling](https://kubernetes.io/docs/concepts/workloads/autoscaling/)
- [HPA Walkthrough](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale-walkthrough/)

## Additional Resources

- [metrics-server](https://github.com/kubernetes-sigs/metrics-server)
- [HPA Algorithm Details](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/#algorithm-details)
- [autoscaling/v2 API Reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/horizontal-pod-autoscaler-v2/)
