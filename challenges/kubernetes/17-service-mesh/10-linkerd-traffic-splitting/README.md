# 17.10 Linkerd Traffic Splitting and Service Profiles

<!--
difficulty: advanced
concepts: [traffic-split, service-profile, canary-deployment, retry-budget, timeout, linkerd-smi]
tools: [kubectl, linkerd]
estimated_time: 30m
bloom_level: analyze
prerequisites: [linkerd-basics, deployments, services]
-->

## Architecture

```
+----------+       TrafficSplit (SMI)       +-----------+
| Client   | ---->  backend (Service)  ---> | backend-v1| (weight: 90)
+----------+              |                 +-----------+
                          |
                          +---------------> +-----------+
                                            | backend-v2| (weight: 10)
                                            +-----------+

ServiceProfile (per-route config)
  GET /api  --> timeout: 500ms, retries: 2
  POST /api --> timeout: 2s, retries: 0
```

Linkerd uses the SMI TrafficSplit CRD for canary deployments and ServiceProfile
for per-route policies (timeouts, retries, response classes). Unlike Istio's
VirtualService, Linkerd keeps traffic management and per-route configuration
as separate concerns.

## What You Will Build

- Two versions of a backend service
- A TrafficSplit resource for weighted canary routing
- A ServiceProfile with per-route timeouts, retries, and response classes
- Verification using `linkerd viz stat` and `linkerd viz routes`

## Suggested Steps

1. Install Linkerd and create namespace `split-lab` with injection annotation.

2. Deploy two versions of a backend:

```yaml
# backend.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-v1
  namespace: split-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend
      version: v1
  template:
    metadata:
      labels:
        app: backend
        version: v1
    spec:
      containers:
        - name: backend
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend-v2
  namespace: split-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
      version: v2
  template:
    metadata:
      labels:
        app: backend
        version: v2
    spec:
      containers:
        - name: backend
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
# Primary Service (apex service)
apiVersion: v1
kind: Service
metadata:
  name: backend
  namespace: split-lab
spec:
  selector:
    app: backend
  ports:
    - name: http
      port: 80
---
# Leaf Services (one per version)
apiVersion: v1
kind: Service
metadata:
  name: backend-v1
  namespace: split-lab
spec:
  selector:
    app: backend
    version: v1
  ports:
    - name: http
      port: 80
---
apiVersion: v1
kind: Service
metadata:
  name: backend-v2
  namespace: split-lab
spec:
  selector:
    app: backend
    version: v2
  ports:
    - name: http
      port: 80
```

3. Create a TrafficSplit:

```yaml
# traffic-split.yaml
apiVersion: split.smi-spec.io/v1alpha2
kind: TrafficSplit
metadata:
  name: backend-split
  namespace: split-lab
spec:
  service: backend             # the apex Service clients call
  backends:
    - service: backend-v1      # leaf Service for v1
      weight: 900              # 90%
    - service: backend-v2      # leaf Service for v2
      weight: 100              # 10%
```

4. Create a ServiceProfile with per-route configuration:

```yaml
# service-profile.yaml
apiVersion: linkerd.io/v1alpha2
kind: ServiceProfile
metadata:
  name: backend.split-lab.svc.cluster.local
  namespace: split-lab
spec:
  routes:
    - name: GET /
      condition:
        method: GET
        pathRegex: /
      timeout: 500ms
      isRetryable: true
    - name: GET /api
      condition:
        method: GET
        pathRegex: /api/.*
      timeout: 1s
      isRetryable: true
    - name: POST /api
      condition:
        method: POST
        pathRegex: /api/.*
      timeout: 2s
      isRetryable: false
  retryBudget:
    retryRatio: 0.2            # max 20% of requests can be retries
    minRetriesPerSecond: 10
    ttl: 10s
```

5. Deploy a traffic generator and observe the split in action.

6. Gradually shift weight from v1 to v2 to simulate a canary rollout.

## Verify

```bash
# TrafficSplit created
kubectl get trafficsplits -n split-lab

# ServiceProfile created
kubectl get serviceprofiles -n split-lab

# Per-route metrics
linkerd viz routes -n split-lab deploy/client --to service/backend

# Traffic distribution matches weights
linkerd viz stat -n split-lab deploy --from deploy/client

# Shift traffic: 50/50
kubectl patch trafficsplit backend-split -n split-lab --type=merge \
  -p '{"spec":{"backends":[{"service":"backend-v1","weight":500},{"service":"backend-v2","weight":500}]}}'

# Full cutover: 0/100
kubectl patch trafficsplit backend-split -n split-lab --type=merge \
  -p '{"spec":{"backends":[{"service":"backend-v1","weight":0},{"service":"backend-v2","weight":1000}]}}'

# Verify shift
linkerd viz stat -n split-lab deploy
```

## Cleanup

```bash
kubectl delete namespace split-lab
```

## References

- [Linkerd Traffic Splitting](https://linkerd.io/2/features/traffic-split/)
- [SMI TrafficSplit Spec](https://github.com/servicemeshinterface/smi-spec/blob/main/apis/traffic-split/v1alpha2/traffic-split.md)
- [Linkerd Service Profiles](https://linkerd.io/2/features/service-profiles/)
- [Linkerd Retries and Timeouts](https://linkerd.io/2/features/retries-and-timeouts/)
