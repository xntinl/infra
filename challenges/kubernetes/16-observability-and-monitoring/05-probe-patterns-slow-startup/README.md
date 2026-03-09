# 16.05 Probe Patterns for Slow-Starting Applications

<!--
difficulty: intermediate
concepts: [startupProbe, slow-startup, failureThreshold, periodSeconds, jvm-warmup, database-migration]
tools: [kubectl]
estimated_time: 20m
bloom_level: apply
prerequisites: [probes-liveness-readiness-startup, deployments]
-->

## What You Will Learn

In this exercise you will configure probes for applications that take a long
time to start -- JVM warm-up, database migrations, large model loading, or
cache pre-warming. You will learn how `startupProbe` parameters interact to
create a boot window, and how to combine them with liveness and readiness
probes without false restarts.

## Step-by-Step

### 1 -- Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: slow-start-lab
```

### 2 -- Simulate a Slow-Starting Application

This Pod takes 45 seconds to "boot" before it starts serving health checks.

```yaml
# slow-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: slow-app
  namespace: slow-start-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: slow-app
  template:
    metadata:
      labels:
        app: slow-app
    spec:
      containers:
        - name: app
          image: busybox:1.37
          command: ["sh", "-c"]
          args:
            - |
              echo "Starting long init (45s)..."
              sleep 45
              echo "ok" > /tmp/started
              echo "ok" > /tmp/healthy
              echo "ok" > /tmp/ready
              echo "App ready, serving..."
              while true; do sleep 5; done
          # startupProbe creates a 120s boot window (periodSeconds * failureThreshold)
          startupProbe:
            exec:
              command: ["cat", "/tmp/started"]
            periodSeconds: 5           # check every 5 seconds
            failureThreshold: 24       # 5 * 24 = 120s max boot time
            successThreshold: 1
          # livenessProbe only activates after startupProbe succeeds
          livenessProbe:
            exec:
              command: ["cat", "/tmp/healthy"]
            periodSeconds: 10
            failureThreshold: 3
          # readinessProbe only activates after startupProbe succeeds
          readinessProbe:
            exec:
              command: ["cat", "/tmp/ready"]
            periodSeconds: 5
            failureThreshold: 2
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
```

### 3 -- A Bad Pattern (Spot the Bug)

This Deployment tries to handle slow startup with only `initialDelaySeconds`.
What goes wrong?

```yaml
# bad-pattern.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: bad-pattern
  namespace: slow-start-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: bad-pattern
  template:
    metadata:
      labels:
        app: bad-pattern
    spec:
      containers:
        - name: app
          image: busybox:1.37
          command: ["sh", "-c"]
          args:
            - |
              sleep 45
              echo "ok" > /tmp/healthy
              while true; do sleep 5; done
          livenessProbe:
            exec:
              command: ["cat", "/tmp/healthy"]
            initialDelaySeconds: 30    # BUG: too short for 45s boot
            periodSeconds: 5
            failureThreshold: 3        # fails 3 times = restart at ~45s
```

<details>
<summary>Why This Fails</summary>

The liveness probe starts at 30 seconds, but the app needs 45 seconds. After
three failures (at 30s, 35s, 40s), Kubernetes restarts the container at ~40s.
The app never finishes booting, causing a crash loop.

**Fix:** Use a `startupProbe` instead of `initialDelaySeconds`. The startup
probe disables liveness entirely until boot completes, regardless of how long
it takes (up to `periodSeconds * failureThreshold`).
</details>

### 4 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f slow-app.yaml
kubectl apply -f bad-pattern.yaml
```

### 5 -- Observe the Difference

```bash
# Watch the good pattern boot successfully after ~45s
kubectl get pods -n slow-start-lab -l app=slow-app -w

# Watch the bad pattern enter CrashLoopBackOff
kubectl get pods -n slow-start-lab -l app=bad-pattern -w

# Compare events
kubectl get events -n slow-start-lab --sort-by='.lastTimestamp'
```

## Verify

```bash
# 1. slow-app Pods should be Running with 0 restarts
kubectl get pods -n slow-start-lab -l app=slow-app

# 2. bad-pattern Pod should show restarts or CrashLoopBackOff
kubectl get pods -n slow-start-lab -l app=bad-pattern

# 3. Check startup probe timeline
kubectl describe pod -n slow-start-lab -l app=slow-app | grep -A 5 "startup"
```

## Cleanup

```bash
kubectl delete namespace slow-start-lab
```

## What's Next

Continue to [16.06 Graceful Shutdown with preStop and Readiness](../06-graceful-shutdown-probes/)
to learn how to drain connections before a Pod terminates.

## Summary

- Use `startupProbe` instead of large `initialDelaySeconds` for slow-starting apps.
- The boot window equals `periodSeconds * failureThreshold`.
- Liveness and readiness probes remain disabled until the startup probe passes.
- Calculate your boot window generously -- include worst-case times for migrations
  or cache loading.
- `initialDelaySeconds` on liveness only delays the first check; it does not
  protect the container from subsequent failures during boot.

## References

- [Define Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/#define-startup-probes)
- [Pod Lifecycle -- Container Probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes)
