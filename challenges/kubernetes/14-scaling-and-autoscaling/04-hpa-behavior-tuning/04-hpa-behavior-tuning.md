<!--
difficulty: intermediate
concepts: [hpa-behavior, scale-up-policy, scale-down-policy, stabilization-window, flapping-prevention]
tools: [kubectl, metrics-server]
estimated_time: 30m
bloom_level: apply
prerequisites: [hpa-cpu-memory-autoscaling, hpa-basics-target-utilization]
-->

# 14.04 - HPA Behavior: Scale-Up and Scale-Down Policies

## Why This Matters

Default HPA behavior can be too aggressive or too conservative for your workload. A real-time bidding service needs instant scale-up but slow scale-down. A batch processor can afford gradual scale-up but needs fast scale-down when queues empty. The `behavior` field gives you precise control over both directions.

## What You Will Learn

- How `stabilizationWindowSeconds` dampens oscillation
- How `policies` control the rate of change (Pods vs Percent)
- How `selectPolicy` chooses between multiple policies (Max, Min, Disabled)
- How to design asymmetric scaling -- fast up, slow down

## Guide

### 1. Create Namespace and Deployment

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: hpa-behavior
```

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-server
  namespace: hpa-behavior
spec:
  replicas: 2
  selector:
    matchLabels:
      app: api-server
  template:
    metadata:
      labels:
        app: api-server
    spec:
      containers:
        - name: app
          image: registry.k8s.io/hpa-example
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 250m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: api-server
  namespace: hpa-behavior
spec:
  selector:
    app: api-server
  ports:
    - port: 80
      targetPort: 80
```

### 2. Aggressive Scale-Up, Conservative Scale-Down

```yaml
# hpa-asymmetric.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-hpa
  namespace: hpa-behavior
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0       # scale up immediately when needed
      policies:
        - type: Percent
          value: 100                      # double the replicas
          periodSeconds: 15
        - type: Pods
          value: 4                        # or add 4 pods
          periodSeconds: 15
      selectPolicy: Max                   # whichever adds more
    scaleDown:
      stabilizationWindowSeconds: 300     # wait 5 min after load drops
      policies:
        - type: Pods
          value: 1                        # remove 1 pod at a time
          periodSeconds: 60
      selectPolicy: Min                   # most conservative removal
```

### 3. Disabled Scale-Down (Manual Scale-Down Only)

```yaml
# hpa-no-scaledown.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-hpa-no-scaledown
  namespace: hpa-behavior
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60
  behavior:
    scaleDown:
      selectPolicy: Disabled              # HPA will never scale down
```

### 4. Rate-Limited Scale-Up

```yaml
# hpa-rate-limited.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: api-hpa-rate-limited
  namespace: hpa-behavior
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 2
  maxReplicas: 20
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 60
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 60
      policies:
        - type: Pods
          value: 2                        # at most 2 new pods per 60s window
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 120
      policies:
        - type: Percent
          value: 10                       # remove at most 10% per 60s window
          periodSeconds: 60
```

### 5. Load Generator

```yaml
# load-generator.yaml
apiVersion: v1
kind: Pod
metadata:
  name: load-generator
  namespace: hpa-behavior
spec:
  containers:
    - name: busybox
      image: busybox:1.37
      command:
        - /bin/sh
        - -c
        - "while true; do wget -q -O- http://api-server.hpa-behavior.svc.cluster.local; done"
```

### Apply and Test

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
kubectl apply -f hpa-asymmetric.yaml

# Watch the HPA
kubectl get hpa -n hpa-behavior --watch

# In another terminal, start load
kubectl apply -f load-generator.yaml

# Observe scale-up speed
# Then delete load generator and observe slow scale-down
kubectl delete pod load-generator -n hpa-behavior
```

## Spot the Bug

This HPA configuration is not doing what the author intended. Can you find the problem?

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: buggy-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: api-server
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: 50
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 300
      policies:
        - type: Pods
          value: 1
          periodSeconds: 300
    scaleDown:
      stabilizationWindowSeconds: 0
      policies:
        - type: Percent
          value: 100
          periodSeconds: 15
```

<details>
<summary>Show Answer</summary>

The scale-up and scale-down policies are reversed. The HPA has a 5-minute stabilization window and adds only 1 pod every 5 minutes for scale-up (very slow), but removes up to 100% of pods every 15 seconds for scale-down (extremely aggressive). In most production systems you want the opposite: fast scale-up, slow scale-down.

</details>

## Verify

```bash
# 1. Check HPA details including behavior
kubectl describe hpa api-hpa -n hpa-behavior

# 2. Generate load and time how fast scale-up happens
kubectl apply -f load-generator.yaml
kubectl get hpa -n hpa-behavior --watch

# 3. Stop load and time how slow scale-down happens
kubectl delete pod load-generator -n hpa-behavior
kubectl get hpa -n hpa-behavior --watch

# 4. Check events for scaling decisions
kubectl get events -n hpa-behavior --sort-by='.lastTimestamp'
```

## Cleanup

```bash
kubectl delete namespace hpa-behavior
```

## What's Next

Next, you will move beyond CPU and memory to scale based on custom application metrics from Prometheus: [14.05 - HPA with Custom Metrics from Prometheus](../05-hpa-custom-metrics/05-hpa-custom-metrics.md).

## Summary

- `stabilizationWindowSeconds` prevents oscillation by requiring metrics to sustain a direction
- `selectPolicy: Max` picks the most aggressive policy; `Min` picks the most conservative
- `selectPolicy: Disabled` turns off scaling in that direction entirely
- Asymmetric behavior (fast up, slow down) is the most common production pattern
- Multiple policies can be defined; the HPA evaluates all and applies `selectPolicy`
