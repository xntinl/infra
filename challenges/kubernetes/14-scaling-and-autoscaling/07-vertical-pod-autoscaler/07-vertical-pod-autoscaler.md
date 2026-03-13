<!--
difficulty: intermediate
concepts: [vertical-pod-autoscaler, vpa-recommender, vpa-updater, update-policy, resource-policy, right-sizing]
tools: [kubectl, vpa]
estimated_time: 35m
bloom_level: apply
prerequisites: [deployments, resource-requests-and-limits]
-->

# 14.07 - Vertical Pod Autoscaler (VPA)

## Why This Matters

Setting accurate resource requests is one of the hardest tasks in Kubernetes. Over-request and you waste cluster capacity. Under-request and pods get OOMKilled or throttled. The **Vertical Pod Autoscaler** observes actual resource consumption over time and adjusts `requests` and `limits` per container -- essentially right-sizing your pods automatically.

## What You Will Learn

- The three VPA components: Recommender, Updater, and Admission Controller
- How `updateMode` controls whether recommendations are applied (`Off`, `Initial`, `Auto`)
- How `resourcePolicy.containerPolicies` constrain min/max resource bounds
- Why VPA and HPA should not target the same resource (CPU) simultaneously

## Guide

### 1. Install VPA

```bash
# Clone the autoscaler repository
git clone https://github.com/kubernetes/autoscaler.git
cd autoscaler/vertical-pod-autoscaler

# Install VPA components in the cluster
./hack/vpa-up.sh
```

### 2. Create Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: vpa-lab
```

### 3. Deploy an Application with Suboptimal Resources

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: resource-app
  namespace: vpa-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: resource-app
  template:
    metadata:
      labels:
        app: resource-app
    spec:
      containers:
        - name: app
          image: registry.k8s.io/hpa-example
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 500m            # deliberately over-provisioned
              memory: 512Mi        # deliberately over-provisioned
            limits:
              cpu: 1000m
              memory: 1Gi
          command:
            - /bin/sh
            - -c
            - "while true; do echo 'working'; sleep 1; done"
```

### 4. VPA in Off Mode (Recommendations Only)

```yaml
# vpa-off.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: resource-app-vpa
  namespace: vpa-lab
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: resource-app
  updatePolicy:
    updateMode: "Off"              # only generate recommendations, do not act
  resourcePolicy:
    containerPolicies:
      - containerName: app
        minAllowed:
          cpu: 50m
          memory: 64Mi
        maxAllowed:
          cpu: 2000m
          memory: 2Gi
        controlledResources:
          - cpu
          - memory
```

### 5. VPA in Auto Mode

```yaml
# vpa-auto.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: resource-app-vpa
  namespace: vpa-lab
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: resource-app
  updatePolicy:
    updateMode: "Auto"             # evict and recreate pods with new resources
  resourcePolicy:
    containerPolicies:
      - containerName: app
        minAllowed:
          cpu: 50m
          memory: 64Mi
        maxAllowed:
          cpu: 2000m
          memory: 2Gi
        controlledResources:
          - cpu
          - memory
```

### 6. VPA in Initial Mode

```yaml
# vpa-initial.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: resource-app-vpa
  namespace: vpa-lab
spec:
  targetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: resource-app
  updatePolicy:
    updateMode: "Initial"          # apply only when new pods are created
  resourcePolicy:
    containerPolicies:
      - containerName: app
        minAllowed:
          cpu: 50m
          memory: 64Mi
        maxAllowed:
          cpu: 2000m
          memory: 2Gi
        controlledResources:
          - cpu
          - memory
```

### 7. Load Generator

```yaml
# load-generator.yaml
apiVersion: v1
kind: Pod
metadata:
  name: load-generator
  namespace: vpa-lab
spec:
  containers:
    - name: busybox
      image: busybox:1.37
      command:
        - /bin/sh
        - -c
        - "while true; do wget -q -O- http://resource-app.vpa-lab.svc.cluster.local; done"
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml

# Wait for pods to be ready
kubectl wait --for=condition=ready pod -l app=resource-app -n vpa-lab --timeout=120s

# Start with Off mode to observe recommendations
kubectl apply -f vpa-off.yaml
```

## Verify

```bash
# 1. Confirm VPA components are running
kubectl get pods -n kube-system | grep vpa

# 2. Check the VPA resource
kubectl get vpa -n vpa-lab

# 3. View current pod resource settings
kubectl get pods -n vpa-lab -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].resources}{"\n"}{end}'

# 4. Wait a few minutes, then check recommendations
kubectl describe vpa resource-app-vpa -n vpa-lab

# 5. View recommendations in JSON
kubectl get vpa resource-app-vpa -n vpa-lab -o jsonpath='{.status.recommendation.containerRecommendations}' | python3 -m json.tool

# 6. Switch to Auto mode
kubectl apply -f vpa-auto.yaml

# 7. Watch pods being evicted and recreated
kubectl get pods -n vpa-lab --watch

# 8. Verify recreated pods have adjusted resources
kubectl get pods -n vpa-lab -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.containers[0].resources.requests}{"\n"}{end}'

# 9. Check VPA events
kubectl get events -n vpa-lab --sort-by='.lastTimestamp' | grep -i vpa
```

## Cleanup

```bash
kubectl delete namespace vpa-lab
```

## What's Next

Now that you understand both horizontal and vertical autoscaling, the next exercise introduces KEDA -- an event-driven autoscaler that extends the HPA with 60+ trigger types and scale-to-zero support: [14.08 - KEDA: Event-Driven Autoscaling](../08-keda-basics/08-keda-basics.md).

## Summary

- VPA has three components: Recommender (analyzes), Updater (evicts), Admission Controller (injects)
- `updateMode: Off` generates recommendations without acting -- useful for initial analysis
- `updateMode: Auto` evicts running pods and recreates them with adjusted resources
- `updateMode: Initial` only applies recommendations to newly created pods
- `resourcePolicy.containerPolicies` sets min/max bounds per container
- Do not use VPA and HPA on the same CPU metric; use VPA for memory and HPA for CPU if needed
