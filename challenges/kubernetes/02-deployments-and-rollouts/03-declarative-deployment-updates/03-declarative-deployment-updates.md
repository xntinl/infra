# 3. Declarative Deployment Updates

<!--
difficulty: basic
concepts: [declarative-updates, kubectl-apply, pod-template-changes, replicaset-creation, image-update]
tools: [kubectl, minikube]
estimated_time: 20m
bloom_level: understand
prerequisites: [02-01, 02-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Deployment)](../01-your-first-deployment/01-your-first-deployment.md) and [exercise 02 (Scaling and Self-Healing)](../02-scaling-and-self-healing/02-scaling-and-self-healing.md)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** what happens when you change a Deployment's Pod template and re-apply
- **Apply** declarative updates to change the container image, environment variables, and resource limits
- **Analyze** how Kubernetes creates a new ReplicaSet and transitions Pods during an update

## Why Declarative Updates?

In exercise 02, you changed the replica count. That scales existing Pods but does not change what each Pod runs. When you modify the Pod template -- the image, environment variables, commands, resource limits, or any field under `spec.template` -- Kubernetes treats this as a new version. The Deployment creates a new ReplicaSet with the updated template and gradually transitions Pods from the old ReplicaSet to the new one.

This is the declarative workflow: you edit your YAML file, run `kubectl apply`, and let Kubernetes figure out what needs to change. The YAML file is always the source of truth. Understanding which changes trigger a rollout and which do not is essential for managing Deployments predictably.

Changes to `spec.template` trigger a new rollout. Changes to `spec.replicas`, `metadata.labels`, or `metadata.annotations` on the Deployment itself do not.

## Step 1: Create the Initial Deployment

Create `deployment.yaml`:

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: webapp
  labels:
    app: webapp
spec:
  replicas: 3
  selector:
    matchLabels:
      app: webapp
  template:
    metadata:
      labels:
        app: webapp
    spec:
      containers:
        - name: nginx
          image: nginx:1.25       # Starting version
          ports:
            - containerPort: 80
          env:
            - name: ENV_MODE
              value: "production"
```

Apply the manifest:

```bash
kubectl apply -f deployment.yaml
kubectl rollout status deployment/webapp
```

Note the current ReplicaSet:

```bash
kubectl get replicaset -l app=webapp
```

Record the ReplicaSet name. There should be exactly one.

## Step 2: Update the Container Image

Edit `deployment.yaml` and change the image version:

```yaml
          image: nginx:1.27       # Updated from 1.25 to 1.27
```

Apply the change:

```bash
kubectl apply -f deployment.yaml
```

Watch the rollout:

```bash
kubectl rollout status deployment/webapp
```

Now check the ReplicaSets:

```bash
kubectl get replicaset -l app=webapp
```

You should see two ReplicaSets: the old one scaled to 0 and the new one scaled to 3. Kubernetes keeps the old ReplicaSet around for rollback purposes.

Verify the image:

```bash
kubectl get deployment webapp -o jsonpath='{.spec.template.spec.containers[0].image}'
```

Expected output: `nginx:1.27`.

## Step 3: Update Environment Variables

Edit `deployment.yaml` to change the environment variable:

```yaml
          env:
            - name: ENV_MODE
              value: "staging"     # Changed from production to staging
```

Apply:

```bash
kubectl apply -f deployment.yaml
kubectl rollout status deployment/webapp
```

Check the ReplicaSets again:

```bash
kubectl get replicaset -l app=webapp
```

Now there are three ReplicaSets. Each Pod template change creates a new one.

Verify the environment variable inside a Pod:

```bash
POD_NAME=$(kubectl get pods -l app=webapp -o jsonpath='{.items[0].metadata.name}')
kubectl exec "$POD_NAME" -- env | grep ENV_MODE
```

Expected output: `ENV_MODE=staging`.

## Step 4: Non-Triggering Changes

Edit `deployment.yaml` to add a label to the Deployment metadata (not the template):

```yaml
metadata:
  name: webapp
  labels:
    app: webapp
    version: v3               # Added to Deployment metadata only
```

Apply:

```bash
kubectl apply -f deployment.yaml
```

Check the ReplicaSets:

```bash
kubectl get replicaset -l app=webapp
```

No new ReplicaSet was created. Changes outside `spec.template` do not trigger a rollout. The existing Pods continue running unchanged.

## Step 5: View the Update History

```bash
kubectl rollout history deployment/webapp
```

You should see three revisions corresponding to the initial creation, the image update, and the env var update.

## Common Mistakes

### Mistake 1: Editing the Wrong Section

Adding labels to `metadata.labels` on the Deployment and expecting Pods to get them:

```yaml
# WRONG — this labels the Deployment object, not the Pods
metadata:
  labels:
    app: webapp
    new-label: value
```

Pods only get labels from `spec.template.metadata.labels`. Labels on the Deployment itself are for organizing the Deployment object.

**Fix:** Add labels under `spec.template.metadata.labels` if you want them on Pods.

### Mistake 2: Expecting Instant Rollout

After applying a change, running `kubectl get pods` immediately shows a mix of old and new Pods. This is normal. The Deployment gradually transitions Pods according to its strategy. Use `kubectl rollout status` to wait for completion.

**Fix:** Always run `kubectl rollout status deployment/<name>` after an update to confirm it completed successfully.

## Verify What You Learned

Confirm the Deployment is running the latest version:

```bash
kubectl get deployment webapp
```

Expected output:

```
NAME     READY   UP-TO-DATE   AVAILABLE   AGE
webapp   3/3     3            3           5m
```

Confirm three ReplicaSets exist (two scaled to 0, one active):

```bash
kubectl get replicaset -l app=webapp
```

Expected: three ReplicaSets, only one with `DESIRED > 0`.

Confirm the current image and env:

```bash
kubectl get deployment webapp -o jsonpath='{.spec.template.spec.containers[0].image}'
# Expected: nginx:1.27

POD=$(kubectl get pods -l app=webapp -o jsonpath='{.items[0].metadata.name}')
kubectl exec "$POD" -- env | grep ENV_MODE
# Expected: ENV_MODE=staging
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete deployment webapp
```

Verify:

```bash
kubectl get deployment,replicaset,pods -l app=webapp
```

## What's Next

You now understand how declarative changes to the Pod template trigger rollouts. The next exercise focuses on the label and selector relationship between Deployments, ReplicaSets, and Pods. Continue to [exercise 04 (Deployment Labels and Selectors)](../04-deployment-labels-and-selectors/04-deployment-labels-and-selectors.md).

## Summary

- Changes to `spec.template` (image, env, resources, etc.) trigger a new **rollout** and create a new ReplicaSet.
- Changes outside `spec.template` (Deployment labels, annotations, replica count) do **not** trigger a rollout.
- Old ReplicaSets are preserved (scaled to 0) for rollback capability.
- `kubectl rollout status` waits for a rollout to complete.
- `kubectl rollout history` shows the revision history of all Pod template changes.

## Reference

- [Updating a Deployment](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#updating-a-deployment) — what triggers a rollout
- [Rollover (Multiple Updates In-Flight)](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/#rollover-aka-multiple-updates-in-flight) — handling concurrent updates
- [Declarative Management](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/) — kubectl apply behavior

## Additional Resources

- [Kubernetes API Reference: Deployment v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/)
- [kubectl apply Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_apply/)
- [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) — field ownership and conflicts
