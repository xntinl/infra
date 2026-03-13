# 2. Scaling and Self-Healing

<!--
difficulty: basic
concepts: [scaling, self-healing, replica-count, kubectl-scale]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [02-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Deployment)](../01-your-first-deployment/01-your-first-deployment.md) or equivalent understanding of Deployments and ReplicaSets

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** imperative and declarative scaling to adjust the number of Pod replicas
- **Analyze** how self-healing works by observing Pod replacement after deletion and container crashes
- **Understand** the relationship between the Deployment's desired state and the ReplicaSet's reconciliation loop

## Why Scaling and Self-Healing?

Traffic to your application varies throughout the day, week, and year. A fixed number of replicas may be too few during peak hours and too many during quiet periods. Manual scaling lets you adjust capacity on demand by changing the replica count. Kubernetes handles the rest: scheduling new Pods onto nodes with available resources or gracefully terminating excess Pods when you scale down.

Self-healing is the other side of the coin. Containers crash, nodes fail, and processes run out of memory. Without self-healing, you would need an operator watching dashboards around the clock, manually restarting failed instances. The ReplicaSet controller eliminates that burden. It continuously compares the actual number of running Pods against the desired count. When the numbers diverge — whether because a Pod was manually deleted, a container exited, or a node went offline — the controller creates or removes Pods to restore the target.

Understanding both mechanisms is a prerequisite for Horizontal Pod Autoscaling (HPA), which automates scaling decisions based on CPU, memory, or custom metrics. Before you can trust an autoscaler, you need to see manual scaling and self-healing work with your own eyes. That is what this exercise covers.

## Step 1: Deploy the Application

Create `deployment.yaml` (the same Deployment from exercise 01, provided here for convenience):

```yaml
# deployment.yaml
apiVersion: apps/v1              # Deployments live in the apps API group
kind: Deployment                 # Resource type
metadata:
  name: nginx-deployment         # Name of the Deployment object
  labels:
    app: nginx                   # Labels on the Deployment itself
spec:
  replicas: 3                    # Starting with 3 replicas
  selector:
    matchLabels:
      app: nginx                 # Must match template labels exactly
  template:
    metadata:
      labels:
        app: nginx               # Pods receive this label for selection
    spec:
      containers:
        - name: nginx            # Container name for logs and exec
          image: nginx:1.27      # Pinned version for reproducibility
          ports:
            - containerPort: 80  # Informational — documents the listening port
```

Apply the manifest:

```bash
kubectl apply -f deployment.yaml
```

Wait for all 3 Pods to be ready:

```bash
kubectl get deployment nginx-deployment
```

Expected: `READY 3/3`.

## Part 1: Manual Scaling

### Scale Up Imperatively

Use `kubectl scale` to increase replicas from 3 to 5:

```bash
kubectl scale deployment nginx-deployment --replicas=5
```

Watch the new Pods appear:

```bash
kubectl get pods -l app=nginx
```

You should see 5 Pods. Two will briefly show `ContainerCreating` before moving to `Running`.

Verify the Deployment reflects the change:

```bash
kubectl get deployment nginx-deployment
```

Expected: `READY 5/5`.

### Scale Down Imperatively

Scale down to 2 replicas:

```bash
kubectl scale deployment nginx-deployment --replicas=2
```

Watch the termination:

```bash
kubectl get pods -l app=nginx -w
```

Press `Ctrl+C` when you see only 2 Pods in `Running` status. Kubernetes selects Pods to terminate based on several criteria, including which Pods are newest and which are not yet ready.

### Scale Declaratively

Edit `deployment.yaml` to change `replicas: 3` to `replicas: 4`:

```yaml
  replicas: 4                    # Changed from 3 to 4
```

Apply the updated manifest:

```bash
kubectl apply -f deployment.yaml
```

Verify 4 Pods are running:

```bash
kubectl get pods -l app=nginx
```

The declarative approach is preferred because the YAML file remains the source of truth and can be version-controlled.

## Part 2: Self-Healing

### Pod Deletion Recovery

List the current Pods and pick one to delete:

```bash
kubectl get pods -l app=nginx
```

Delete one Pod by name:

```bash
POD_NAME=$(kubectl get pods -l app=nginx -o jsonpath='{.items[0].metadata.name}')
echo "Deleting $POD_NAME"
kubectl delete pod "$POD_NAME" &
kubectl get pods -l app=nginx -w
```

Press `Ctrl+C` after you see the replacement Pod reach `Running`. Notice the new Pod has a different name. The ReplicaSet detected the count dropped below 4 and immediately created a replacement.

### Container Crash Recovery

Simulate a container crash by killing the main process (PID 1) inside a Pod:

```bash
POD_NAME=$(kubectl get pods -l app=nginx -o jsonpath='{.items[0].metadata.name}')
echo "Crashing container in $POD_NAME"
kubectl exec "$POD_NAME" -- kill 1
```

Watch the Pod restart:

```bash
kubectl get pods -l app=nginx
```

The Pod name stays the same, but the `RESTARTS` column increments. Unlike deletion (which replaces the Pod entirely), a container crash triggers an in-place restart managed by the kubelet. The Pod object persists.

Check the restart count:

```bash
kubectl describe pod "$POD_NAME" | grep -A 2 "Restart Count"
```

## Part 3: Explore Events

Every scaling and scheduling action is recorded as an event. View recent events sorted by time:

```bash
kubectl get events --sort-by=.lastTimestamp | tail -20
```

Look for entries like:
- `ScalingReplicaSet` — the Deployment adjusting the ReplicaSet
- `SuccessfulCreate` — the ReplicaSet creating a new Pod
- `Killing` — a Pod being terminated during scale-down
- `Started` — a container starting inside a Pod

These events are invaluable for understanding what happened and in what order.

## Common Mistakes

### Mistake 1: Editing the ReplicaSet Instead of the Deployment

You might try to scale by editing the ReplicaSet directly:

```bash
# WRONG — this change will be overridden
kubectl scale replicaset nginx-deployment-<hash> --replicas=1
```

Watch what happens:

```bash
kubectl get pods -l app=nginx
```

The count briefly drops to 1, then the Deployment controller notices the ReplicaSet no longer matches the Deployment's desired count and resets it back to 4. The Deployment always wins.

**Fix:** Always modify the Deployment, never the ReplicaSet. The Deployment is the owner and will override any direct changes to the ReplicaSet's replica count.

### Mistake 2: Expecting Deleted Pods to Keep Their Names

After deleting a Pod:

```bash
# Before deletion
nginx-deployment-5d8f6b7c4-abc12   1/1   Running
nginx-deployment-5d8f6b7c4-def34   1/1   Running
nginx-deployment-5d8f6b7c4-ghi56   1/1   Running
nginx-deployment-5d8f6b7c4-jkl78   1/1   Running

# Delete abc12
kubectl delete pod nginx-deployment-5d8f6b7c4-abc12

# After replacement — note the NEW name
nginx-deployment-5d8f6b7c4-def34   1/1   Running
nginx-deployment-5d8f6b7c4-ghi56   1/1   Running
nginx-deployment-5d8f6b7c4-jkl78   1/1   Running
nginx-deployment-5d8f6b7c4-mno90   1/1   Running   # New Pod, new name
```

**Fix:** This is not a mistake to fix but a behavior to understand. Pod names follow the pattern `<deployment>-<replicaset-hash>-<pod-hash>`. Each new Pod gets a unique random suffix. Never hardcode Pod names in scripts or configurations. Use labels and selectors instead.

## Verify What You Learned

Scale to 5 replicas, delete one Pod, and confirm the count returns to 5:

```bash
kubectl scale deployment nginx-deployment --replicas=5
kubectl get pods -l app=nginx
```

Expected: 5 Pods running.

```bash
POD_NAME=$(kubectl get pods -l app=nginx -o jsonpath='{.items[0].metadata.name}')
kubectl delete pod "$POD_NAME"
```

Wait a moment, then verify:

```bash
kubectl get pods -l app=nginx
```

Expected: 5 Pods running again (one will be newer than the rest).

Verify the Deployment shows the correct count:

```bash
kubectl get deployment nginx-deployment
```

Expected output:

```
NAME               READY   UP-TO-DATE   AVAILABLE   AGE
nginx-deployment   5/5     5            5           5m
```

Verify the ReplicaSet matches:

```bash
kubectl get replicaset -l app=nginx
```

Expected output:

```
NAME                          DESIRED   CURRENT   READY   AGE
nginx-deployment-<hash>       5         5         5       5m
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete deployment nginx-deployment
```

Verify everything is gone:

```bash
kubectl get deployment,replicaset,pods -l app=nginx
```

## What's Next

You now know how to manually adjust capacity and have seen the ReplicaSet controller keep your desired count intact. In [exercise 03 (Declarative Deployment Updates)](../03-declarative-deployment-updates/03-declarative-deployment-updates.md), you will learn how to make changes to a Deployment's Pod template declaratively and observe how Kubernetes handles the transition.

## Summary

- **Imperative scaling** with `kubectl scale` is fast for operational adjustments; **declarative scaling** by editing the YAML and re-applying is better for long-term management.
- The **ReplicaSet controller** continuously reconciles actual Pod count with the desired count, creating or removing Pods as needed.
- **Pod deletion** triggers replacement with a new Pod (new name, new IP). **Container crashes** trigger in-place restarts (same Pod, incremented restart count).
- Always scale the **Deployment**, never the ReplicaSet directly — the Deployment controller will override ReplicaSet changes.
- **Events** (`kubectl get events`) provide a chronological record of every scaling, scheduling, and lifecycle action.

## Reference

- [Deployments: Scaling](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#scaling-a-deployment) — official documentation on scaling
- [ReplicaSet](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/) — the controller responsible for maintaining Pod count
- [Pod Lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/) — restart policies and container states

## Additional Resources

- [Horizontal Pod Autoscaler](https://kubernetes.io/docs/tasks/run-application/horizontal-pod-autoscale/) — automate scaling based on metrics (future exercises)
- [kubectl scale Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_scale/) — full command documentation
- [Debugging Pods](https://kubernetes.io/docs/tasks/debug/debug-application/debug-pods/) — troubleshooting failed or crashing Pods
