# 9. Canary Deployments with Label Selectors

<!--
difficulty: advanced
concepts: [canary-deployment, label-selectors, traffic-splitting, gradual-rollout, service-routing]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [02-04, 02-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 04 (Deployment Labels and Selectors)](../04-deployment-labels-and-selectors/) and [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** canary deployments using two Deployments with a shared Service selector
- **Analyze** traffic distribution based on Pod replica ratios
- **Evaluate** when to promote a canary to full production or roll it back

## Architecture

A canary deployment runs a small number of Pods with the new version alongside the full fleet of the current version. A single Service routes traffic to both, with the traffic split determined by the ratio of Pod replicas.

```
Service (selector: app=webapp)
  |
  +-- Deployment "webapp-stable" (9 replicas, version=v1)
  +-- Deployment "webapp-canary" (1 replica, version=v2)
```

With this setup, approximately 10% of traffic goes to the canary (1 out of 10 total Pods).

## Steps

### 1. Deploy the Stable Version

```yaml
# stable.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp-stable
spec:
  replicas: 9
  selector:
    matchLabels:
      app: webapp
      track: stable
  template:
    metadata:
      labels:
        app: webapp
        track: stable
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html
      initContainers:
        - name: setup
          image: busybox:1.37
          command: ["sh", "-c", "echo 'Version: v1 (stable)' > /html/index.html"]
          volumeMounts:
            - name: html
              mountPath: /html
      volumes:
        - name: html
          emptyDir: {}
---
apiVersion: v1
kind: Service
metadata:
  name: webapp
spec:
  selector:
    app: webapp              # Matches BOTH stable and canary Pods
  ports:
    - port: 80
      targetPort: 80
```

```bash
kubectl apply -f stable.yaml
kubectl rollout status deployment/webapp-stable
```

### 2. Verify Baseline Traffic

```bash
kubectl port-forward svc/webapp 8080:80 &
for i in $(seq 1 10); do curl -s localhost:8080; done
kill %1
```

All 10 requests return "Version: v1 (stable)".

### 3. Deploy the Canary

```yaml
# canary.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp-canary
spec:
  replicas: 1
  selector:
    matchLabels:
      app: webapp
      track: canary
  template:
    metadata:
      labels:
        app: webapp
        track: canary
        version: v2
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: html
              mountPath: /usr/share/nginx/html
      initContainers:
        - name: setup
          image: busybox:1.37
          command: ["sh", "-c", "echo 'Version: v2 (canary)' > /html/index.html"]
          volumeMounts:
            - name: html
              mountPath: /html
      volumes:
        - name: html
          emptyDir: {}
```

```bash
kubectl apply -f canary.yaml
kubectl rollout status deployment/webapp-canary
```

### 4. Observe Traffic Split

```bash
kubectl port-forward svc/webapp 8080:80 &
for i in $(seq 1 20); do curl -s localhost:8080; done | sort | uniq -c
kill %1
```

Expected: approximately 18 requests to v1 and 2 to v2 (90/10 split based on 9:1 Pod ratio).

### 5. Gradually Promote the Canary

Increase canary traffic to ~30%:

```bash
kubectl scale deployment webapp-stable --replicas=7
kubectl scale deployment webapp-canary --replicas=3
```

Verify the new ratio:

```bash
kubectl get pods -l app=webapp -o custom-columns='NAME:.metadata.name,TRACK:.metadata.labels.track' | sort -k2
```

### 6. Full Promotion or Rollback

**To promote**: scale canary to the full replica count and delete stable:

```bash
kubectl scale deployment webapp-canary --replicas=10
kubectl delete deployment webapp-stable
```

**To rollback**: delete the canary:

```bash
kubectl delete deployment webapp-canary
kubectl scale deployment webapp-stable --replicas=10
```

## Verify What You Learned

```bash
kubectl get deployments -l app=webapp
# Expected: both webapp-stable and webapp-canary with their respective replica counts

kubectl get pods -l app=webapp --show-labels | grep -c track=canary
kubectl get pods -l app=webapp --show-labels | grep -c track=stable
# Expected: counts matching the Deployment replicas

kubectl get svc webapp -o jsonpath='{.spec.selector}'
# Expected: {"app":"webapp"} — matches both tracks
```

## Cleanup

```bash
kubectl delete deployment webapp-stable webapp-canary
kubectl delete svc webapp
```

## Summary

- Canary deployments use **two Deployments** with a shared Service selector label (`app: webapp`)
- Traffic split is controlled by the **ratio of Pod replicas** between stable and canary
- Each Deployment uses a distinct `track` label for identification (not in the Service selector)
- Promotion means scaling canary up and deleting stable; rollback means deleting canary
- This approach provides coarse-grained traffic splitting (Pod-ratio-based, not percentage-based)

## Reference

- [Canary Deployments](https://kubernetes.io/docs/concepts/cluster-administration/manage-deployment/#canary-deployments) — official documentation
- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) — selector mechanics
- [Services](https://kubernetes.io/docs/concepts/services-networking/service/) — how Services route to Pods
