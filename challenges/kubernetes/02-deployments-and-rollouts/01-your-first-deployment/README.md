# 1. Your First Deployment

<!--
difficulty: basic
concepts: [deployment, replicaset, desired-state, labels, selectors]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [01-your-first-pod]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01-01 (Your First Pod)](../../01-pods-and-pod-design/01-your-first-pod/) or equivalent understanding of Pods

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** why Deployments exist and the problem they solve over bare Pods
- **Apply** a Deployment manifest to create and manage a set of identical Pods
- **Analyze** the relationship between a Deployment, its ReplicaSet, and the resulting Pods

## Why Deployments?

A bare Pod has no controller watching over it. If the Pod crashes, the container exits, or the node goes down, nothing recreates it. Your application simply disappears. In production this is unacceptable. You need a mechanism that declares "I want 3 copies of this Pod running at all times" and a controller that continuously reconciles reality with that declaration.

That mechanism is a Deployment. You write a manifest stating the desired number of replicas and the Pod template. The Deployment controller (running inside the `kube-controller-manager`) creates a ReplicaSet, and the ReplicaSet creates the actual Pods. If a Pod is deleted, the ReplicaSet notices the count is below the desired number and immediately creates a replacement. This is the core of Kubernetes' **desired-state reconciliation** model: you declare what you want, and controllers make it happen.

The Deployment sits one level above the ReplicaSet for a reason. When you update the Pod template (for example, changing the image version), the Deployment creates a new ReplicaSet and gradually shifts traffic to the new Pods while draining the old ones. This gives you rolling updates and rollback capabilities. In this exercise we focus on the creation and self-healing aspects. Rolling updates come in exercise 05.

## Step 1: Create the Deployment

Create `deployment.yaml`:

```yaml
# deployment.yaml
apiVersion: apps/v1              # Deployments live in the apps API group
kind: Deployment                 # Resource type
metadata:
  name: nginx-deployment         # Name of the Deployment object
  labels:
    app: nginx                   # Labels on the Deployment itself (for filtering)
spec:
  replicas: 3                    # Desired number of identical Pods
  selector:
    matchLabels:
      app: nginx                 # The Deployment manages Pods with this label
  template:                      # Pod template — defines what each replica looks like
    metadata:
      labels:
        app: nginx               # Pods get this label; MUST match selector above
    spec:
      containers:
        - name: nginx            # Container name inside each Pod
          image: nginx:1.27      # Pinned image version for reproducibility
          ports:
            - containerPort: 80  # Informational — documents the listening port
```

Apply the manifest:

```bash
kubectl apply -f deployment.yaml
```

## Step 2: Verify the Deployment, ReplicaSet, and Pods

Check the Deployment:

```bash
kubectl get deployment nginx-deployment
```

You should see `READY 3/3` once all Pods are running.

Check the ReplicaSet that was automatically created:

```bash
kubectl get replicaset
```

Notice the name follows the pattern `nginx-deployment-<hash>`. The hash is derived from the Pod template, so any change to the template produces a new ReplicaSet.

Check the individual Pods:

```bash
kubectl get pods -l app=nginx
```

The `-l app=nginx` flag filters by label. You should see 3 Pods, each with a name like `nginx-deployment-<rs-hash>-<pod-hash>`.

## Step 3: Understand the Ownership Chain

Kubernetes objects form an ownership hierarchy. The Deployment owns the ReplicaSet, and the ReplicaSet owns the Pods. You can see this in the `ownerReferences` field.

Describe the ReplicaSet to see its owner:

```bash
kubectl describe replicaset -l app=nginx | grep -A 3 "Controlled By"
```

You should see `Controlled By: Deployment/nginx-deployment`.

Describe one of the Pods to see its owner:

```bash
POD_NAME=$(kubectl get pods -l app=nginx -o jsonpath='{.items[0].metadata.name}')
kubectl describe pod "$POD_NAME" | grep -A 3 "Controlled By"
```

You should see `Controlled By: ReplicaSet/nginx-deployment-<hash>`.

This chain means that when you delete the Deployment, it cascades: the ReplicaSet is deleted, which deletes all the Pods.

## Step 4: Observe Self-Healing

Delete one of the Pods manually and watch the Deployment replace it:

```bash
POD_NAME=$(kubectl get pods -l app=nginx -o jsonpath='{.items[0].metadata.name}')
echo "Deleting $POD_NAME"
kubectl delete pod "$POD_NAME"
```

Immediately check the Pods:

```bash
kubectl get pods -l app=nginx
```

You will see a new Pod being created (status `ContainerCreating`) while the old one terminates. Within seconds, the count is back to 3. The ReplicaSet detected the discrepancy and acted.

## Common Mistakes

### Mistake 1: selector.matchLabels Does Not Match template.metadata.labels

```yaml
# WRONG — selector says "app: nginx" but template labels say "app: web"
spec:
  replicas: 3
  selector:
    matchLabels:
      app: nginx
  template:
    metadata:
      labels:
        app: web       # Does not match selector
```

Error message:

```
The Deployment "nginx-deployment" is invalid: spec.template.metadata.labels:
Invalid value: map[string]string{"app":"web"}: `selector` does not match
template `labels`
```

**Fix:** The `selector.matchLabels` must exactly match `template.metadata.labels`. This is how the Deployment finds the Pods it owns. Change either the selector or the template labels so they are identical.

### Mistake 2: Omitting spec.selector Entirely

```yaml
# WRONG — missing the required selector field
spec:
  replicas: 3
  template:
    metadata:
      labels:
        app: nginx
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

Error message:

```
error: error validating "deployment.yaml": error validating data:
ValidationError(Deployment.spec): missing required field "selector"
in io.k8s.api.apps.v1.DeploymentSpec
```

**Fix:** `spec.selector` is a required field for Deployments in `apps/v1`. Add the `selector` block with `matchLabels` that correspond to your Pod template labels. Unlike older API versions, `apps/v1` does not infer the selector from the template.

## Verify What You Learned

Confirm the Deployment shows 3 ready replicas:

```bash
kubectl get deployment nginx-deployment
```

Expected output:

```
NAME               READY   UP-TO-DATE   AVAILABLE   AGE
nginx-deployment   3/3     3            3           60s
```

Confirm the ReplicaSet exists and manages 3 Pods:

```bash
kubectl get replicaset -l app=nginx
```

Expected output:

```
NAME                          DESIRED   CURRENT   READY   AGE
nginx-deployment-<hash>       3         3         3       60s
```

Confirm the Deployment references its ReplicaSet:

```bash
kubectl describe deployment nginx-deployment | grep "NewReplicaSet"
```

Expected output:

```
NewReplicaSet:   nginx-deployment-<hash> (3/3 replicas created)
```

Confirm all 3 Pods are running:

```bash
kubectl get pods -l app=nginx
```

Expected output (names will vary):

```
NAME                                READY   STATUS    RESTARTS   AGE
nginx-deployment-5d8f6b7c4-abc12   1/1     Running   0          60s
nginx-deployment-5d8f6b7c4-def34   1/1     Running   0          60s
nginx-deployment-5d8f6b7c4-ghi56   1/1     Running   0          60s
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete deployment nginx-deployment
```

Verify everything is gone (the cascading delete removes the ReplicaSet and Pods):

```bash
kubectl get deployment,replicaset,pods -l app=nginx
```

## What's Next

Now that you have a Deployment managing Pods, the next step is learning how to adjust the replica count up and down and observing the self-healing behavior in greater depth. Continue to [exercise 02 (Scaling and Self-Healing)](../02-scaling-and-self-healing/).

## Summary

- A **Deployment** declares a desired state (replica count + Pod template) and a controller reconciles reality to match.
- A **ReplicaSet** is the intermediary that actually creates and tracks the Pods. You rarely interact with it directly.
- The ownership chain is **Deployment -> ReplicaSet -> Pods**, visible in `ownerReferences`.
- `selector.matchLabels` must exactly match `template.metadata.labels` — this is how the Deployment identifies its Pods.
- Deleting a Pod managed by a Deployment triggers automatic replacement, maintaining the desired count.

## Reference

- [Deployments](https://kubernetes.io/docs/concepts/workloads/controllers/deployment/) — official concept documentation
- [ReplicaSet](https://kubernetes.io/docs/concepts/workloads/controllers/replicaset/) — how ReplicaSets work under a Deployment
- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) — the matching mechanism

## Additional Resources

- [Kubernetes API Reference: Deployment v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/deployment-v1/)
- [Managing Resources](https://kubernetes.io/docs/concepts/cluster-administration/manage-deployment/) — operational patterns for Deployments
- [Garbage Collection and Owner References](https://kubernetes.io/docs/concepts/architecture/garbage-collection/) — cascading deletion behavior
