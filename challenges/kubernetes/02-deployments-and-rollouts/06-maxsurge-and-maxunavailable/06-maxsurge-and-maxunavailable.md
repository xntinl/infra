# 6. Deployment Strategies: maxSurge and maxUnavailable

<!--
difficulty: intermediate
concepts: [maxSurge, maxUnavailable, rolling-update-tuning, deployment-strategy, capacity-planning]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [02-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/05-rolling-updates-and-rollbacks.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** different maxSurge and maxUnavailable combinations to control rollout speed and availability
- **Analyze** how each combination affects the number of Pods running during a transition
- **Evaluate** which strategy fits different production scenarios (cost-sensitive, availability-critical, fast-deploy)

## Why Tune maxSurge and maxUnavailable?

The defaults (`maxSurge: 25%`, `maxUnavailable: 25%`) work for most workloads, but production scenarios vary. A payment processing service cannot tolerate any unavailability, requiring `maxUnavailable: 0`. A batch processing cluster may want the fastest possible rollout with `maxSurge: 100%`. Understanding these parameters lets you balance speed, cost, and availability.

## Step 1: Baseline Deployment

```yaml
# baseline.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: strategy-demo
spec:
  replicas: 6
  selector:
    matchLabels:
      app: strategy-demo
  template:
    metadata:
      labels:
        app: strategy-demo
    spec:
      containers:
        - name: nginx
          image: nginx:1.25
          ports:
            - containerPort: 80
```

```bash
kubectl apply -f baseline.yaml
kubectl rollout status deployment/strategy-demo
```

## Step 2: Zero-Downtime Strategy (maxSurge: 1, maxUnavailable: 0)

This is the safest approach. No existing Pod is terminated until its replacement is Ready:

```bash
kubectl patch deployment strategy-demo -p '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":1,"maxUnavailable":0}}}}'
kubectl set image deployment/strategy-demo nginx=nginx:1.26
kubectl get pods -l app=strategy-demo -w
```

Observe: at any point, at least 6 Pods are Running. A 7th Pod (surge) is created, becomes Ready, then one old Pod is terminated. This repeats 6 times. Press `Ctrl+C` when done.

**Trade-off:** Slower rollout, higher peak resource usage (7 Pods briefly).

## Step 3: Fast Rollout Strategy (maxSurge: 3, maxUnavailable: 3)

Maximum speed at the cost of reduced capacity during transition:

```bash
kubectl patch deployment strategy-demo -p '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":3,"maxUnavailable":3}}}}'
kubectl set image deployment/strategy-demo nginx=nginx:1.27
kubectl get pods -l app=strategy-demo -w
```

Observe: up to 9 Pods exist simultaneously (6 + 3 surge), and as few as 3 are available (6 - 3 unavailable). The rollout completes much faster. Press `Ctrl+C` when done.

**Trade-off:** Faster rollout, but only 50% capacity during the transition.

## Step 4: Percentage-Based Values

maxSurge and maxUnavailable can be percentages:

```bash
kubectl patch deployment strategy-demo -p '{"spec":{"strategy":{"type":"RollingUpdate","rollingUpdate":{"maxSurge":"50%","maxUnavailable":"25%"}}}}'
kubectl set image deployment/strategy-demo nginx=nginx:1.25
kubectl rollout status deployment/strategy-demo
```

With 6 replicas:
- `maxSurge: 50%` = 3 extra Pods (ceil of 6 * 0.5)
- `maxUnavailable: 25%` = 1 unavailable (floor of 6 * 0.25, minimum 1)

Check the strategy:

```bash
kubectl get deployment strategy-demo -o jsonpath='{.spec.strategy.rollingUpdate}'
```

## Step 5: Fill in the TODO

Your team requires the following during rollouts:
- Minimum 5 Pods available at all times (out of 6 replicas)
- Maximum 8 Pods running at any time

Calculate the correct maxSurge and maxUnavailable values.

<details>
<summary>Solution</summary>

- `maxUnavailable: 1` (6 - 1 = 5 minimum available)
- `maxSurge: 2` (6 + 2 = 8 maximum Pods)

```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: 2
    maxUnavailable: 1
```

</details>

## Spot the Bug

A teammate configured this strategy for a 4-replica Deployment:

```yaml
strategy:
  type: RollingUpdate
  rollingUpdate:
    maxSurge: "10%"
    maxUnavailable: "10%"
```

The rollout hangs. Why?

<details>
<summary>Explanation</summary>

With 4 replicas, `maxSurge: 10%` rounds up to 1, and `maxUnavailable: 10%` rounds down to 0. This means the strategy is effectively `maxSurge: 1, maxUnavailable: 0`, which works fine. However, if the new Pods fail readiness checks, the rollout stalls because maxUnavailable is 0 and the new Pods never become Ready. Always verify that your new image passes health checks before relying on `maxUnavailable: 0`.

</details>

## Verify What You Learned

```bash
kubectl get deployment strategy-demo -o jsonpath='{.spec.strategy}' | python3 -m json.tool
```

Verify the deployment is healthy:

```bash
kubectl get deployment strategy-demo
# Expected: 6/6 Ready
```

## Cleanup

```bash
kubectl delete deployment strategy-demo
```

## What's Next

Now that you understand how to tune rollout parameters, the next exercise explores ReplicaSets in depth -- what they are, how they relate to Deployments, and when (if ever) you interact with them directly. Continue to [exercise 07 (ReplicaSets and Ownership)](../07-replicasets-and-ownership/07-replicasets-and-ownership.md).

## Summary

- `maxSurge` controls how many **extra** Pods can exist during a rollout (above desired replicas)
- `maxUnavailable` controls how many Pods can be **unavailable** during a rollout (below desired replicas)
- Setting `maxUnavailable: 0` ensures zero-downtime but slows the rollout
- Setting high values for both parameters maximizes rollout speed at the cost of availability
- Percentage values are rounded: `maxSurge` rounds up, `maxUnavailable` rounds down

## Reference

- [Deployment Strategy](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy) — maxSurge and maxUnavailable documentation
- [Rolling Update Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#rolling-update-deployment) — detailed behavior
- [Proportional Scaling](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#proportional-scaling) — how multiple ReplicaSets are scaled

## Additional Resources

- [Kubernetes API Reference: DeploymentStrategy](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/#DeploymentStrategy)
- [Performing a Rolling Update Tutorial](https://kubernetes.io/docs/tutorials/kubernetes-basics/update/update-intro/)
- [Pod Disruption Budgets](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/) — related availability controls
