# 5. Rolling Updates and Rollbacks

<!--
difficulty: intermediate
concepts: [rolling-update, rollback, strategy, maxSurge, maxUnavailable, revision-history]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [02-01, 02-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completion of [exercise 01 (Your First Deployment)](../01-your-first-deployment/01-your-first-deployment.md) and [exercise 02 (Scaling and Self-Healing)](../02-scaling-and-self-healing/02-scaling-and-self-healing.md)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** a RollingUpdate strategy with maxSurge and maxUnavailable constraints
- **Analyze** a rolling update in progress and understand how Kubernetes transitions between ReplicaSets
- **Apply** rollback commands to revert a Deployment to a previous revision

## Why Rolling Updates and Rollbacks?

Deploying a new version of your application should not mean downtime. In production, users expect uninterrupted service even while you ship changes multiple times a day. The RollingUpdate strategy solves this by replacing pods incrementally: Kubernetes creates new pods running the updated version while gradually terminating old ones. At no point does the total number of available pods drop below a threshold you control.

But what happens when the new version has a bug? You need to revert quickly. Kubernetes keeps a revision history for every Deployment, letting you roll back to any previous state with a single command. The combination of controlled rollouts and instant rollbacks gives you a safety net that makes frequent deployments practical. Understanding the `maxSurge` and `maxUnavailable` parameters is essential because they directly control how fast your rollout proceeds and how much capacity you sacrifice during the transition.

## Step 1: Fill in the Strategy and Deploy

Create `deployment.yaml` with the following content. The `strategy` section is left as a TODO for you to complete:

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-rolling
spec:
  replicas: 4
  selector:
    matchLabels:
      app: nginx-rolling
  # TODO: Add a strategy section
  # Requirements:
  #   - Type: RollingUpdate
  #   - Maximum 1 extra pod during update (maxSurge)
  #   - Maximum 1 unavailable pod during update (maxUnavailable)
  # Docs: https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy
  template:
    metadata:
      labels:
        app: nginx-rolling
    spec:
      containers:
        - name: nginx
          image: nginx:1.24
          ports:
            - containerPort: 80
```

Once you have filled in the strategy block, apply the manifest:

```bash
kubectl apply -f deployment.yaml
```

Verify all 4 replicas are running before continuing:

```bash
kubectl get deployment nginx-rolling
```

Expected output:

```
NAME            READY   UP-TO-DATE   AVAILABLE   AGE
nginx-rolling   4/4     4            4           30s
```

## Step 2: Trigger a Rolling Update

Update the nginx image from 1.24 to 1.25 using `kubectl set image`. Record the change cause with an annotation so it appears in the rollout history:

```bash
kubectl set image deployment/nginx-rolling nginx=nginx:1.25
kubectl annotate deployment/nginx-rolling kubernetes.io/change-cause="Update nginx to 1.25"
```

Immediately watch the rolling update in progress:

```bash
kubectl rollout status deployment/nginx-rolling
```

Expected output:

```
Waiting for deployment "nginx-rolling" rollout to finish: 2 out of 4 new replicas have been updated...
Waiting for deployment "nginx-rolling" rollout to finish: 3 out of 4 new replicas have been updated...
Waiting for deployment "nginx-rolling" rollout to finish: 3 of 4 updated replicas are available...
deployment "nginx-rolling" successfully rolled out
```

While the update is in progress, open a second terminal and observe the pods transitioning:

```bash
kubectl get pods -l app=nginx-rolling -w
```

You will see new pods created (up to `maxSurge` extra) and old pods terminating (respecting `maxUnavailable`). At most 5 pods exist simultaneously (4 replicas + 1 maxSurge), and at least 3 are always available (4 replicas - 1 maxUnavailable).

Verify the update completed:

```bash
kubectl describe deployment nginx-rolling | grep Image
```

Expected output:

```
    Image:        nginx:1.25
```

## Step 3: Inspect Rollout History

Check the revision history to see all recorded changes:

```bash
kubectl rollout history deployment/nginx-rolling
```

Expected output:

```
deployment.apps/nginx-rolling
REVISION  CHANGE-CAUSE
1         <none>
2         Update nginx to 1.25
```

You can inspect the details of any specific revision:

```bash
kubectl rollout history deployment/nginx-rolling --revision=1
```

## Step 4: Roll Back to the Previous Version

Undo the last rollout to revert to revision 1 (nginx:1.24):

```bash
kubectl rollout undo deployment/nginx-rolling
```

Watch the rollback complete:

```bash
kubectl rollout status deployment/nginx-rolling
```

Expected output:

```
deployment "nginx-rolling" successfully rolled out
```

## Step 5: Verify the Rollback

Confirm the image is back to nginx:1.24:

```bash
kubectl describe deployment nginx-rolling | grep Image
```

Expected output:

```
    Image:        nginx:1.24
```

Check the rollout history again. Notice that revision 1 is gone and a new revision 3 was created from it:

```bash
kubectl rollout history deployment/nginx-rolling
```

Expected output:

```
deployment.apps/nginx-rolling
REVISION  CHANGE-CAUSE
2         Update nginx to 1.25
3         <none>
```

Kubernetes does not duplicate revision entries. When you roll back to revision 1, it becomes revision 3.

## Spot the Bug

A teammate proposes this strategy to achieve "zero risk" deployments:

```yaml
spec:
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 0
      maxUnavailable: 0
```

**What happens when you apply this and trigger an update? Why?**

Think about it before expanding the answer.

<details>
<summary>Explanation</summary>

This configuration creates a **deadlock**. Kubernetes cannot create any new pods because `maxSurge: 0` means no extra pods beyond the desired replica count. Simultaneously, it cannot remove any old pods because `maxUnavailable: 0` means all current replicas must remain available. With no way to create new pods or remove old ones, the rollout hangs indefinitely.

The Deployment controller needs at least one of these values to be greater than zero to make progress. A common safe configuration is `maxSurge: 1, maxUnavailable: 0` which adds one new pod first, waits for it to become ready, then removes one old pod, and repeats until the rollout is complete.

</details>

## Verify What You Learned

Run the following checks to confirm everything worked:

Deployment has 4 ready replicas:

```bash
kubectl get deployment nginx-rolling
```

Expected output:

```
NAME            READY   UP-TO-DATE   AVAILABLE   AGE
nginx-rolling   4/4     4            4           5m
```

Image is back to 1.24 after rollback:

```bash
kubectl get deployment nginx-rolling -o jsonpath='{.spec.template.spec.containers[0].image}'
```

Expected output:

```
nginx:1.24
```

Multiple revisions exist in history:

```bash
kubectl rollout history deployment/nginx-rolling --revision=3
```

Expected output includes:

```
  Containers:
   nginx:
    Image:	nginx:1.24
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete deployment nginx-rolling
```

Verify nothing remains:

```bash
kubectl get deployment nginx-rolling
```

Expected output:

```
Error from server (NotFound): deployments.apps "nginx-rolling" not found
```

## What's Next

Now that you understand rolling updates and rollbacks, the next exercise dives deeper into the `maxSurge` and `maxUnavailable` parameters and their interaction. Continue to [exercise 06 (Deployment Strategies: maxSurge and maxUnavailable)](../06-maxsurge-and-maxunavailable/06-maxsurge-and-maxunavailable.md).

## Summary

- **RollingUpdate** replaces pods incrementally, controlled by `maxSurge` (how many extra pods) and `maxUnavailable` (how many pods can be down).
- `kubectl set image` triggers an update; `kubectl rollout status` watches its progress.
- `kubectl rollout history` shows past revisions; `kubectl rollout undo` reverts to the previous one.
- Setting both `maxSurge: 0` and `maxUnavailable: 0` creates an unresolvable deadlock.
- Rollbacks are instant because Kubernetes keeps old ReplicaSets and simply scales them back up.

## Reference

- [Deployments: Rolling Update Strategy](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#strategy) — official strategy documentation
- [Deployments: Rolling Back](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#rolling-back-a-deployment) — rollback commands and behavior
- [kubectl rollout](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_rollout/) — CLI reference for rollout subcommands

## Additional Resources

- [Kubernetes API Reference: Deployment v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/)
- [Performing a Rolling Update](https://kubernetes.io/docs/tutorials/kubernetes-basics/update/update-intro/) — interactive tutorial
- [Canary Deployments](https://kubernetes.io/docs/concepts/cluster-administration/manage-deployment/#canary-deployments) — alternative update strategy

---

<details>
<summary>TODO Solution: Strategy Block</summary>

```yaml
  strategy:
    type: RollingUpdate
    rollingUpdate:
      maxSurge: 1
      maxUnavailable: 1
```

Place this block between `selector` and `template` in the Deployment spec.

</details>
