# 12. Recreate vs Rolling Update Deep Dive

<!--
difficulty: advanced
concepts: [recreate-strategy, rolling-update, downtime, strategy-selection, database-migrations, breaking-changes]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [02-05, 02-06]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/) and [exercise 06 (maxSurge and maxUnavailable)](../06-maxsurge-and-maxunavailable/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** both Recreate and RollingUpdate strategies and observe their behavior under load
- **Analyze** the downtime characteristics of each strategy
- **Evaluate** when Recreate is the correct choice despite its downtime cost

## Architecture

Kubernetes supports two Deployment strategies:

**RollingUpdate** (default): gradually replaces old Pods with new ones. Zero downtime if probes are configured correctly. Two versions coexist briefly during the transition.

**Recreate**: kills all old Pods first, then creates all new Pods. Guarantees that only one version runs at any time, but causes downtime during the gap.

```
RollingUpdate:  [v1 v1 v1] -> [v1 v1 v2] -> [v1 v2 v2] -> [v2 v2 v2]  (zero downtime)
Recreate:       [v1 v1 v1] -> [         ] -> [v2 v2 v2]                 (downtime gap)
```

## Steps

### 1. Deploy with RollingUpdate and Monitor Traffic

```yaml
# rolling-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: rolling-app
spec:
  replicas: 4
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 1
  selector:
    matchLabels:
      app: rolling-app
  template:
    metadata:
      labels:
        app: rolling-app
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.25
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: rolling-app
spec:
  selector:
    app: rolling-app
  ports:
    - port: 80
```

```bash
kubectl apply -f rolling-deploy.yaml
kubectl rollout status deployment/rolling-app
```

Start a continuous traffic test:

```bash
kubectl port-forward svc/rolling-app 8080:80 &
while true; do
  status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8080 2>/dev/null)
  echo "$(date +%H:%M:%S) - $status"
  sleep 0.5
done &
TRAFFIC_PID=$!
```

Trigger an update:

```bash
kubectl set image deployment/rolling-app nginx=nginx:1.27
kubectl rollout status deployment/rolling-app
```

Stop the traffic test:

```bash
kill $TRAFFIC_PID 2>/dev/null
kill %1 2>/dev/null
```

Observe: all responses should show `200`. No downtime during the transition.

### 2. Deploy with Recreate and Monitor Traffic

```yaml
# recreate-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: recreate-app
spec:
  replicas: 4
  strategy:
    type: Recreate
  selector:
    matchLabels:
      app: recreate-app
  template:
    metadata:
      labels:
        app: recreate-app
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.25
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: recreate-app
spec:
  selector:
    app: recreate-app
  ports:
    - port: 80
```

```bash
kubectl apply -f recreate-deploy.yaml
kubectl rollout status deployment/recreate-app
```

Start a continuous traffic test:

```bash
kubectl port-forward svc/recreate-app 8081:80 &
while true; do
  status=$(curl -s -o /dev/null -w "%{http_code}" http://localhost:8081 2>/dev/null)
  echo "$(date +%H:%M:%S) - $status"
  sleep 0.5
done &
TRAFFIC_PID=$!
```

Trigger an update:

```bash
kubectl set image deployment/recreate-app nginx=nginx:1.27
kubectl rollout status deployment/recreate-app
```

Stop the traffic test:

```bash
kill $TRAFFIC_PID 2>/dev/null
kill %1 2>/dev/null
```

Observe: you will see a gap of failed requests (connection refused or empty replies) during the transition. This is the Recreate downtime.

### 3. Compare ReplicaSet Behavior

```bash
kubectl get rs -l app=rolling-app
kubectl get rs -l app=recreate-app
```

RollingUpdate keeps old ReplicaSets (scaled to 0) for rollback. Recreate also keeps old ReplicaSets but transitions differently -- it scales the old to 0 completely before scaling the new to the desired count.

### 4. When to Use Recreate

Recreate is the correct choice when:

- **Database schema migrations** require the old version to be completely stopped before the new version starts
- **File locks** or **singleton resources** cannot be shared between versions
- **License servers** limit concurrent instances
- **Incompatible API versions** between old and new would corrupt data if both run simultaneously

Document your strategy choice:

```yaml
spec:
  strategy:
    type: Recreate
    # Reason: Database migration in v2 is incompatible with v1 schema.
    # Concurrent access from both versions would cause data corruption.
```

## Verify What You Learned

```bash
kubectl get deployment rolling-app -o jsonpath='{.spec.strategy.type}'
# Expected: RollingUpdate

kubectl get deployment recreate-app -o jsonpath='{.spec.strategy.type}'
# Expected: Recreate

kubectl get rs -l app=rolling-app | wc -l
kubectl get rs -l app=recreate-app | wc -l
# Both should show multiple ReplicaSets
```

## Cleanup

```bash
kubectl delete deployment rolling-app recreate-app
kubectl delete svc rolling-app recreate-app
```

## Summary

- **RollingUpdate** provides zero-downtime deployments with two versions coexisting briefly
- **Recreate** causes downtime but guarantees single-version operation at all times
- Use Recreate for database migrations, file locks, singletons, or breaking schema changes
- Both strategies maintain ReplicaSet history for rollback
- The Recreate strategy has no `maxSurge` or `maxUnavailable` parameters -- it is all-or-nothing

## Reference

- [Deployment Strategy](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy) — Recreate vs RollingUpdate
- [Recreate Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#recreate-deployment) — behavior details
- [Rolling Update Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#rolling-update-deployment) — parameter reference
