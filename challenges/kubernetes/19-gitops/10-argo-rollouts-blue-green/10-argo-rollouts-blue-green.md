<!--
difficulty: advanced
concepts: [argo-rollouts, blue-green-deployment, active-preview-services, auto-promotion, scale-down-delay]
tools: [kubectl, kubectl-argo-rollouts-plugin]
estimated_time: 35m
bloom_level: analyze
prerequisites: [19-gitops/09-argo-rollouts-canary]
-->

# 19.10 - Argo Rollouts: Blue-Green with Promotion

## Architecture

```
Blue-Green Rollout
====================

  Before Promotion:
  ┌──────────────┐     ┌──────────────┐
  │ Active       │     │ Preview      │
  │ Service      │     │ Service      │
  │ (production  │     │ (testing the │
  │  traffic)    │     │  new version)│
  └──────┬───────┘     └──────┬───────┘
         │                    │
         ▼                    ▼
  ┌──────────────┐     ┌──────────────┐
  │ Blue (v1)    │     │ Green (v2)   │
  │ ReplicaSet   │     │ ReplicaSet   │
  │ (stable)     │     │ (preview)    │
  └──────────────┘     └──────────────┘

  After Promotion:
  Active Service ──► Green (v2) becomes stable
  Blue (v1) scaled down after scaleDownDelaySeconds
```

Blue-green deployments run two full environments simultaneously. The **active** Service points to the current stable version (blue), while the **preview** Service points to the new version (green). After validation, you promote the green version -- the active Service switches to green, and the old blue ReplicaSet is scaled down after a configurable delay.

## Suggested Steps

### 1. Install Argo Rollouts (If Not Already)

```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts \
  -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml
```

### 2. Create the Blue-Green Rollout

```yaml
# rollout-blue-green.yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-demo
  namespace: bluegreen-lab
spec:
  replicas: 3
  revisionHistoryLimit: 2
  selector:
    matchLabels:
      app: bluegreen-demo
  strategy:
    blueGreen:
      activeService: bluegreen-active         # production traffic
      previewService: bluegreen-preview       # testing traffic
      autoPromotionEnabled: false             # require manual promotion
      scaleDownDelaySeconds: 60               # keep old RS for 60s after switch
      previewReplicaCount: 3                  # preview runs same replica count
      antiAffinity:
        requiredDuringSchedulingIgnoredDuringExecution: {}
  template:
    metadata:
      labels:
        app: bluegreen-demo
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 5
            periodSeconds: 5
```

### 3. Create Active and Preview Services

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: bluegreen-active
  namespace: bluegreen-lab
spec:
  selector:
    app: bluegreen-demo
  ports:
    - port: 80
      targetPort: 80
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: bluegreen-preview
  namespace: bluegreen-lab
spec:
  selector:
    app: bluegreen-demo
  ports:
    - port: 80
      targetPort: 80
      name: http
```

### 4. Deploy and Trigger Blue-Green Switch

```bash
kubectl create namespace bluegreen-lab
kubectl apply -f services.yaml
kubectl apply -f rollout-blue-green.yaml

# Watch the rollout
kubectl argo rollouts get rollout bluegreen-demo -n bluegreen-lab --watch
```

### 5. Update the Image to Trigger Green Deployment

```bash
kubectl argo rollouts set image bluegreen-demo app=nginx:1.27 -n bluegreen-lab

# Both ReplicaSets are now running
kubectl get rs -n bluegreen-lab
kubectl get pods -n bluegreen-lab
```

### 6. Test the Preview

```bash
# Port-forward to preview service
kubectl port-forward svc/bluegreen-preview -n bluegreen-lab 8081:80 &

# Verify the preview is working
curl -s http://localhost:8081 | head -5
```

### 7. Promote or Abort

```bash
# Promote: switch active traffic to the green version
kubectl argo rollouts promote bluegreen-demo -n bluegreen-lab

# Or abort: discard the green version
# kubectl argo rollouts abort bluegreen-demo -n bluegreen-lab
```

### 8. Blue-Green with Auto-Promotion and Analysis

```yaml
# rollout-auto-promote.yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: bluegreen-auto
  namespace: bluegreen-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: bluegreen-auto
  strategy:
    blueGreen:
      activeService: bluegreen-auto-active
      previewService: bluegreen-auto-preview
      autoPromotionEnabled: true
      autoPromotionSeconds: 60               # auto-promote after 60s if healthy
      preAnalysis:                           # run analysis before promotion
        templates:
          - templateName: smoke-test
        args:
          - name: service-name
            value: bluegreen-auto-preview
  template:
    metadata:
      labels:
        app: bluegreen-auto
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
```

## Verify

```bash
# 1. Rollout status
kubectl argo rollouts status bluegreen-demo -n bluegreen-lab

# 2. Detailed rollout info shows active/preview
kubectl argo rollouts get rollout bluegreen-demo -n bluegreen-lab

# 3. Check which ReplicaSet each service points to
kubectl get svc bluegreen-active -n bluegreen-lab -o jsonpath='{.spec.selector}' && echo ""
kubectl get svc bluegreen-preview -n bluegreen-lab -o jsonpath='{.spec.selector}' && echo ""

# 4. After promotion, old RS scales down after delay
kubectl get rs -n bluegreen-lab --watch

# 5. Rollout history
kubectl argo rollouts history bluegreen-demo -n bluegreen-lab
```

## Cleanup

```bash
kubectl delete namespace bluegreen-lab
kubectl delete namespace argo-rollouts
```

## References

- [Argo Rollouts - Blue-Green](https://argo-rollouts.readthedocs.io/en/stable/features/bluegreen/)
- [Argo Rollouts Specification](https://argo-rollouts.readthedocs.io/en/stable/features/specification/)
- [Pre/Post Analysis](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/)
