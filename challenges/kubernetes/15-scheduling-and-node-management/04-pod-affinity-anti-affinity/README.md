<!--
difficulty: intermediate
concepts: [pod-affinity, pod-anti-affinity, topology-key, co-location, high-availability]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [node-affinity-and-taints, labels-and-selectors, deployments]
-->

# 15.04 - Pod Affinity and Anti-Affinity

## Why This Matters

Sometimes placement depends not on node properties but on where *other pods* are running. A web frontend should run on the same node as its cache (affinity) to minimize network latency. Database replicas should run on different nodes (anti-affinity) to survive node failures. Pod affinity and anti-affinity rules express these relationships.

## What You Will Learn

- How `podAffinity` co-locates pods on the same topology domain
- How `podAntiAffinity` separates pods across topology domains
- How `topologyKey` defines the scope: per-node (`kubernetes.io/hostname`) or per-zone (`topology.kubernetes.io/zone`)
- The performance implications of `required` vs `preferred` pod affinity

## Guide

### 1. Create a Cache Deployment

```yaml
# cache-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: cache
  namespace: affinity-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: cache
  template:
    metadata:
      labels:
        app: cache
        tier: cache
    spec:
      containers:
        - name: redis
          image: redis:7
          ports:
            - containerPort: 6379
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 200m
              memory: 256Mi
```

### 2. Web Frontend with Pod Affinity (Co-locate with Cache)

```yaml
# web-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: affinity-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
  template:
    metadata:
      labels:
        app: web
        tier: frontend
    spec:
      affinity:
        podAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - cache
              topologyKey: kubernetes.io/hostname   # same node as cache
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 3. Database with Pod Anti-Affinity (Spread Replicas)

```yaml
# database-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: database
  namespace: affinity-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: database
  template:
    metadata:
      labels:
        app: database
        tier: data
    spec:
      affinity:
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - database
              topologyKey: kubernetes.io/hostname   # one replica per node
      containers:
        - name: postgres
          image: postgres:16
          env:
            - name: POSTGRES_PASSWORD
              value: example
          ports:
            - containerPort: 5432
          resources:
            requests:
              cpu: 200m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
```

### 4. Zone-Level Anti-Affinity

```yaml
# zone-spread-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: zone-spread
  namespace: affinity-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: zone-spread
  template:
    metadata:
      labels:
        app: zone-spread
    spec:
      affinity:
        podAntiAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 100
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app
                      operator: In
                      values:
                        - zone-spread
                topologyKey: topology.kubernetes.io/zone   # prefer different zones
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
```

### 5. Combined: Affinity + Anti-Affinity

```yaml
# combined-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: affinity-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      affinity:
        podAffinity:
          preferredDuringSchedulingIgnoredDuringExecution:
            - weight: 80
              podAffinityTerm:
                labelSelector:
                  matchExpressions:
                    - key: app
                      operator: In
                      values:
                        - cache
                topologyKey: kubernetes.io/hostname   # prefer same node as cache
        podAntiAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            - labelSelector:
                matchExpressions:
                  - key: app
                    operator: In
                    values:
                      - api
              topologyKey: kubernetes.io/hostname       # require different nodes
      containers:
        - name: nginx
          image: nginx:1.27
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
```

### Apply

```bash
kubectl create namespace affinity-lab
kubectl apply -f cache-deployment.yaml
kubectl apply -f web-deployment.yaml
kubectl apply -f database-deployment.yaml
kubectl apply -f zone-spread-deployment.yaml
kubectl apply -f combined-deployment.yaml
```

## Spot the Bug

This pod affinity is not doing what the author intended. Can you see why?

```yaml
affinity:
  podAffinity:
    requiredDuringSchedulingIgnoredDuringExecution:
      - labelSelector:
          matchExpressions:
            - key: app
              operator: In
              values:
                - cache
        topologyKey: topology.kubernetes.io/zone
```

<details>
<summary>Show Answer</summary>

The `topologyKey` is `topology.kubernetes.io/zone`, not `kubernetes.io/hostname`. This means "schedule in the same **zone** as a cache pod" -- not the same **node**. If the goal is to minimize network latency by co-locating on the same node, the `topologyKey` should be `kubernetes.io/hostname`.

</details>

## Verify

```bash
# 1. Check cache pod placement
kubectl get pods -n affinity-lab -l app=cache -o wide

# 2. Verify web pods are on the same nodes as cache pods
kubectl get pods -n affinity-lab -l app=web -o wide

# 3. Verify database pods are on different nodes
kubectl get pods -n affinity-lab -l app=database -o wide

# 4. Check zone spread
kubectl get pods -n affinity-lab -l app=zone-spread -o wide

# 5. Verify combined: API pods near cache but spread across nodes
kubectl get pods -n affinity-lab -l app=api -o wide
```

## Cleanup

```bash
kubectl delete namespace affinity-lab
```

## What's Next

Pod affinity and anti-affinity give you powerful placement control, but they can be complex. The next exercise covers `topologySpreadConstraints` -- a more declarative and Kubernetes-native way to distribute pods evenly: [15.05 - Topology Spread Constraints](../05-topology-spread-constraints/).

## Summary

- `podAffinity` co-locates pods within the same topology domain as target pods
- `podAntiAffinity` ensures pods land in different topology domains from target pods
- `topologyKey` defines the domain: `kubernetes.io/hostname` (node), `topology.kubernetes.io/zone` (zone)
- `required` rules make scheduling fail if unsatisfiable; `preferred` rules are best-effort
- Pod affinity has O(n^2) scheduling cost at scale -- use `preferred` or topology spread constraints for large clusters
