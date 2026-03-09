<!--
difficulty: advanced
concepts: [argo-rollouts, canary-deployment, analysis-template, rollout-crd, traffic-splitting, progressive-delivery]
tools: [kubectl, kubectl-argo-rollouts-plugin]
estimated_time: 40m
bloom_level: analyze
prerequisites: [deployments, services, 19-gitops/01-argocd-gitops-deployment]
-->

# 19.09 - Argo Rollouts: Canary with Analysis

## Architecture

```
Canary Rollout with Analysis
==============================

  Rollout Controller
  ┌──────────────────────────────────────────────┐
  │                                              │
  │  Step 1: setWeight 20%  ──► AnalysisRun     │
  │  Step 2: pause 5m       ──► (Prometheus      │
  │  Step 3: setWeight 50%       query checks    │
  │  Step 4: pause 5m            error rate)     │
  │  Step 5: setWeight 100%                      │
  │                                              │
  └──────────────────────────────────────────────┘

  Traffic Split:
  ┌──────────┐     ┌──────────┐     ┌──────────┐
  │  Service  │────►│ stable   │     │ canary   │
  │  (main)   │    │ ReplicaSet│    │ ReplicaSet│
  └──────────┘    │  80%      │    │  20%      │
                  └──────────┘    └──────────┘
```

**Argo Rollouts** replaces the standard Deployment with a `Rollout` CRD that supports canary and blue-green deployment strategies with automated analysis. During a canary rollout, traffic is gradually shifted from the stable to the canary version. An **AnalysisTemplate** defines success criteria (e.g., error rate < 1%) and automatically rolls back if the canary fails.

## Suggested Steps

### 1. Install Argo Rollouts

```bash
kubectl create namespace argo-rollouts
kubectl apply -n argo-rollouts \
  -f https://github.com/argoproj/argo-rollouts/releases/latest/download/install.yaml

kubectl wait --for=condition=Ready pods --all -n argo-rollouts --timeout=120s
```

Install the kubectl plugin:

```bash
# macOS
brew install argoproj/tap/kubectl-argo-rollouts

# Linux
curl -LO https://github.com/argoproj/argo-rollouts/releases/latest/download/kubectl-argo-rollouts-linux-amd64
chmod +x kubectl-argo-rollouts-linux-amd64 && sudo mv kubectl-argo-rollouts-linux-amd64 /usr/local/bin/kubectl-argo-rollouts
```

### 2. Create the AnalysisTemplate

```yaml
# analysis-template.yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: success-rate
  namespace: rollouts-demo
spec:
  metrics:
    - name: success-rate
      interval: 30s
      count: 5                             # run 5 measurements
      successCondition: result[0] >= 0.95  # require 95% success rate
      failureLimit: 2                      # allow up to 2 failures
      provider:
        prometheus:
          address: http://prometheus.monitoring:9090
          query: |
            sum(rate(http_requests_total{status=~"2.*",app="{{args.service-name}}"}[1m]))
            /
            sum(rate(http_requests_total{app="{{args.service-name}}"}[1m]))
  args:
    - name: service-name
```

For clusters without Prometheus, use a simple Job-based analysis:

```yaml
# analysis-template-job.yaml
apiVersion: argoproj.io/v1alpha1
kind: AnalysisTemplate
metadata:
  name: smoke-test
  namespace: rollouts-demo
spec:
  metrics:
    - name: smoke-test
      count: 1
      provider:
        job:
          spec:
            backoffLimit: 0
            template:
              spec:
                restartPolicy: Never
                containers:
                  - name: test
                    image: busybox:1.37
                    command:
                      - /bin/sh
                      - -c
                      - |
                        wget --spider --timeout=5 http://rollouts-demo-canary.rollouts-demo:80
                        exit $?
```

### 3. Create the Rollout

```yaml
# rollout.yaml
apiVersion: argoproj.io/v1alpha1
kind: Rollout
metadata:
  name: rollouts-demo
  namespace: rollouts-demo
spec:
  replicas: 5
  revisionHistoryLimit: 3
  selector:
    matchLabels:
      app: rollouts-demo
  strategy:
    canary:
      canaryService: rollouts-demo-canary    # Service for canary pods
      stableService: rollouts-demo-stable    # Service for stable pods
      analysis:
        templates:
          - templateName: smoke-test
        startingStep: 2                      # begin analysis at step 2
      steps:
        - setWeight: 20                      # 20% traffic to canary
        - pause: { duration: 30s }
        - setWeight: 50                      # 50% traffic to canary
        - pause: { duration: 30s }
        - setWeight: 80
        - pause: { duration: 30s }
  template:
    metadata:
      labels:
        app: rollouts-demo
    spec:
      containers:
        - name: rollouts-demo
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
```

### 4. Create Services

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: rollouts-demo-stable
  namespace: rollouts-demo
spec:
  selector:
    app: rollouts-demo
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: rollouts-demo-canary
  namespace: rollouts-demo
spec:
  selector:
    app: rollouts-demo
  ports:
    - port: 80
      targetPort: 80
```

### 5. Deploy and Trigger a Canary

```bash
kubectl create namespace rollouts-demo
kubectl apply -f analysis-template-job.yaml
kubectl apply -f services.yaml
kubectl apply -f rollout.yaml

# Watch the rollout
kubectl argo rollouts get rollout rollouts-demo -n rollouts-demo --watch

# Trigger a canary by updating the image
kubectl argo rollouts set image rollouts-demo rollouts-demo=nginx:1.27 -n rollouts-demo
```

### 6. Manual Promotion and Abort

```bash
# Promote canary to stable (skip remaining steps)
kubectl argo rollouts promote rollouts-demo -n rollouts-demo

# Or abort and rollback
kubectl argo rollouts abort rollouts-demo -n rollouts-demo

# Retry after abort
kubectl argo rollouts retry rollout rollouts-demo -n rollouts-demo
```

## Verify

```bash
# 1. Rollout status
kubectl argo rollouts status rollouts-demo -n rollouts-demo

# 2. Detailed rollout info
kubectl argo rollouts get rollout rollouts-demo -n rollouts-demo

# 3. Check ReplicaSets (stable + canary)
kubectl get rs -n rollouts-demo

# 4. Check AnalysisRuns
kubectl get analysisrun -n rollouts-demo

# 5. View rollout events
kubectl describe rollout rollouts-demo -n rollouts-demo
```

## Cleanup

```bash
kubectl delete namespace rollouts-demo
kubectl delete namespace argo-rollouts
```

## References

- [Argo Rollouts - Canary](https://argo-rollouts.readthedocs.io/en/stable/features/canary/)
- [Analysis & Progressive Delivery](https://argo-rollouts.readthedocs.io/en/stable/features/analysis/)
- [Argo Rollouts Getting Started](https://argo-rollouts.readthedocs.io/en/stable/getting-started/)
