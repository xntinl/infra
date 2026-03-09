# 11. Progressive Rollouts with Readiness Gates

<!--
difficulty: advanced
concepts: [readiness-gates, pod-conditions, custom-conditions, progressive-delivery, rollout-control]
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

- **Apply** readiness gates to add custom conditions that must be true before a Pod is considered Ready
- **Analyze** how readiness gates integrate with Deployment rollouts to control progression
- **Evaluate** readiness gates as a building block for progressive delivery

## Architecture

Kubernetes considers a Pod `Ready` when all containers pass their readiness probes AND all readiness gates are satisfied. A readiness gate is a custom condition (e.g., `www.example.com/feature-gate`) that starts as `False` and must be patched to `True` by an external controller or script.

During a rolling update, the Deployment controller waits for new Pods to become Ready before terminating old Pods. By adding a readiness gate, you can hold a rollout until an external check passes -- metric validation, load test completion, manual approval, or any custom logic.

```
New Pod created --> Containers Ready --> Readiness Gate pending --> External signal --> Gate satisfied --> Pod Ready --> Rollout continues
```

## Steps

### 1. Deploy with a Readiness Gate

```yaml
# gated-deploy.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: gated-app
spec:
  replicas: 3
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 0
  selector:
    matchLabels:
      app: gated-app
  template:
    metadata:
      labels:
        app: gated-app
    spec:
      readinessGates:
        - conditionType: "custom.io/approved"
      containers:
        - name: nginx
          image: nginx:1.25
          ports:
            - containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 2
            periodSeconds: 5
```

```bash
kubectl apply -f gated-deploy.yaml
```

### 2. Observe Pods Stuck as Not-Ready

```bash
kubectl get pods -l app=gated-app
```

Pods show `0/1 Ready` even though the container's readiness probe passes. The readiness gate `custom.io/approved` has not been set yet:

```bash
kubectl get pod -l app=gated-app -o jsonpath='{.items[0].status.conditions}' | python3 -m json.tool
```

You will see the container readiness is `True` but the Pod-level `Ready` condition is `False` because the gate is unsatisfied.

### 3. Satisfy the Readiness Gate

Patch each Pod to set the custom condition to True:

```bash
for pod in $(kubectl get pods -l app=gated-app -o jsonpath='{.items[*].metadata.name}'); do
  kubectl patch pod "$pod" --type=json -p='[{"op":"add","path":"/status/conditions/-","value":{"type":"custom.io/approved","status":"True","lastTransitionTime":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}}]' --subresource=status
done
```

```bash
kubectl get pods -l app=gated-app
```

Pods now show `1/1 Ready`.

### 4. Trigger a Rollout and Control Progression

```bash
kubectl set image deployment/gated-app nginx=nginx:1.27
```

Watch the rollout:

```bash
kubectl get pods -l app=gated-app -w
```

The new Pod is created but stays `0/1 Ready` because the readiness gate is not yet set. The rollout pauses -- with `maxUnavailable: 0`, no old Pod is terminated until the new Pod is Ready.

In a separate terminal, approve the new Pod:

```bash
NEW_POD=$(kubectl get pods -l app=gated-app --field-selector=status.phase=Running -o jsonpath='{.items[?(@.status.conditions[?(@.type=="custom.io/approved")].status!="True")].metadata.name}' 2>/dev/null | awk '{print $1}')
if [ -n "$NEW_POD" ]; then
  kubectl patch pod "$NEW_POD" --type=json -p='[{"op":"add","path":"/status/conditions/-","value":{"type":"custom.io/approved","status":"True","lastTransitionTime":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}}]' --subresource=status
fi
```

Repeat for each new Pod as the rollout creates them. Each approval allows the rollout to progress one step further.

### 5. Automate Gate Approval (Simulation)

In production, an external controller watches for new Pods and runs validation before setting the gate. Here is a simplified simulation:

```bash
# Run in background: auto-approve Pods after 10 seconds
while true; do
  for pod in $(kubectl get pods -l app=gated-app -o jsonpath='{.items[*].metadata.name}'); do
    approved=$(kubectl get pod "$pod" -o jsonpath='{.status.conditions[?(@.type=="custom.io/approved")].status}' 2>/dev/null)
    if [ "$approved" != "True" ]; then
      echo "Approving $pod after validation delay..."
      sleep 10
      kubectl patch pod "$pod" --type=json -p='[{"op":"add","path":"/status/conditions/-","value":{"type":"custom.io/approved","status":"True","lastTransitionTime":"'"$(date -u +%Y-%m-%dT%H:%M:%SZ)"'"}}]' --subresource=status 2>/dev/null
    fi
  done
  sleep 5
done
```

## Verify What You Learned

```bash
kubectl get deployment gated-app
# Expected: 3/3 Ready (if all gates are approved)

kubectl get pods -l app=gated-app -o jsonpath='{.items[0].status.conditions[?(@.type=="custom.io/approved")].status}'
# Expected: True
```

## Cleanup

```bash
kubectl delete deployment gated-app
```

## Summary

- **Readiness gates** add custom conditions that must be True before a Pod is considered Ready
- During a rolling update with `maxUnavailable: 0`, unsatisfied gates **pause** the rollout
- An external controller or script patches the Pod's status to satisfy the gate
- This enables **progressive delivery**: manual approval, metric-based validation, or canary analysis
- Readiness gates complement (not replace) container readiness probes

## Reference

- [Pod Readiness Gates](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-readiness-gate) — official documentation
- [Pod Conditions](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-conditions) — how conditions work
- [KEP-580: Pod Readiness Gates](https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/580-pod-readiness-gates) — design proposal
