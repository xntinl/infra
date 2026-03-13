# 5. Declarative vs Imperative Management

<!--
difficulty: basic
concepts: [declarative, imperative, kubectl-apply, kubectl-create, kubectl-run, drift, gitops]
tools: [kubectl, minikube]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-01, 01-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Pod)](../01-your-first-pod/01-your-first-pod.md) and [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/03-labels-selectors-and-annotations.md)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the distinction between imperative commands, imperative object configuration, and declarative object configuration
- **Understand** why declarative management is preferred for production workloads
- **Apply** both approaches and observe how they interact with the Kubernetes API

## Why Does This Distinction Matter?

Kubernetes supports three management approaches. **Imperative commands** (`kubectl run`, `kubectl create`) tell the API server exactly what to do. **Imperative object configuration** (`kubectl create -f`) sends a file but still says "create this." **Declarative object configuration** (`kubectl apply -f`) says "make reality match this file" without specifying the verb.

In production, declarative management wins because it supports version control, code review, and automation. When you run `kubectl apply`, Kubernetes compares the desired state in your file with the live state in the cluster and computes the minimal diff. If nothing changed, nothing happens. This idempotency makes declarative manifests safe to run repeatedly in CI/CD pipelines.

The tradeoff is that imperative commands are faster for quick experiments. Knowing when to use each approach -- and how they can conflict with each other -- prevents common operational mistakes.

## Step 1: Imperative Commands

Create a Pod with a single command:

```bash
kubectl run quick-pod --image=nginx:1.27 --labels="app=quick,method=imperative"
```

Create a Service imperatively:

```bash
kubectl expose pod quick-pod --port=80 --name=quick-svc --type=ClusterIP
```

These commands are convenient but leave no file behind. There is no YAML to commit to Git, no record of what flags you used, and no way to reproduce the exact same state later.

Verify what was created:

```bash
kubectl get pod quick-pod --show-labels
kubectl get svc quick-svc
```

## Step 2: Generate YAML from Imperative Commands

The `--dry-run=client -o yaml` flags let you use imperative commands to generate YAML without creating anything:

```bash
kubectl run generated-pod --image=nginx:1.27 --port=80 --labels="app=generated" \
  --dry-run=client -o yaml > generated-pod.yaml
```

Inspect the generated file:

```bash
cat generated-pod.yaml
```

This is a useful technique: use imperative commands to scaffold the YAML, then edit and apply it declaratively.

## Step 3: Imperative Object Configuration

`kubectl create -f` sends the file to the API server with a "create" verb:

```bash
kubectl create -f generated-pod.yaml
```

Verify:

```bash
kubectl get pod generated-pod
```

Now try running the same command again:

```bash
kubectl create -f generated-pod.yaml
```

Error message:

```
Error from server (AlreadyExists): error when creating "generated-pod.yaml": pods "generated-pod" already exists
```

`kubectl create` is not idempotent. If the resource already exists, it fails. This makes it unsuitable for automated pipelines where you might re-run the same deployment.

## Step 4: Declarative Object Configuration

Delete the generated Pod and recreate it with `apply`:

```bash
kubectl delete pod generated-pod
kubectl apply -f generated-pod.yaml
```

Now run apply again:

```bash
kubectl apply -f generated-pod.yaml
```

No error. The output says `pod/generated-pod unchanged`. `kubectl apply` is idempotent: it computes the diff between the file and the live object, and if they match, it does nothing.

Edit `generated-pod.yaml` to add a new label:

```yaml
metadata:
  labels:
    app: generated
    version: v2              # Added this label
```

Apply the change:

```bash
kubectl apply -f generated-pod.yaml
```

Output: `pod/generated-pod configured`. Only the label was added; nothing else was touched.

Verify:

```bash
kubectl get pod generated-pod --show-labels
```

## Step 5: Observe Configuration Drift

Imperative changes made after an `apply` create drift between the live state and the file on disk:

```bash
kubectl label pod generated-pod extra-label=drifted
kubectl get pod generated-pod --show-labels
```

The live object now has a label (`extra-label=drifted`) that is not in `generated-pod.yaml`. If you run `kubectl apply -f generated-pod.yaml` again, the extra label is preserved because `apply` uses a three-way merge (last-applied, live, desired).

To see the last-applied configuration:

```bash
kubectl get pod generated-pod -o jsonpath='{.metadata.annotations.kubectl\.kubernetes\.io/last-applied-configuration}' | python3 -m json.tool
```

This annotation is how `apply` tracks what was in the file during the last apply, enabling the three-way merge.

## Step 6: When to Use Each Approach

| Scenario | Approach | Command |
|----------|----------|---------|
| Quick experiment in dev | Imperative command | `kubectl run` |
| Scaffolding a manifest | Imperative + dry-run | `kubectl run --dry-run=client -o yaml` |
| One-time setup (rare) | Imperative config | `kubectl create -f` |
| Production deployment | Declarative config | `kubectl apply -f` |
| GitOps pipeline | Declarative config | `kubectl apply -f` |

## Common Mistakes

### Mistake 1: Mixing create and apply

```bash
kubectl create -f pod.yaml      # Creates the resource
kubectl apply -f pod.yaml       # Warning: resource was created with create, not apply
```

You will see a warning about missing `last-applied-configuration` annotation. While this works, it means the first apply cannot compute a proper three-way diff.

**Fix:** Start with `kubectl apply -f` from the beginning. If you must use `create` first, add the annotation: `kubectl apply -f pod.yaml --force-conflict`.

### Mistake 2: Editing Live Objects Instead of Files

```bash
kubectl edit pod generated-pod   # Changes the live object only
```

This change is not reflected in your YAML file. The next `kubectl apply -f generated-pod.yaml` may overwrite your live edit or silently ignore it depending on the three-way merge.

**Fix:** Always edit the YAML file and re-apply. Treat the file as the source of truth.

## Verify What You Learned

Confirm that `apply` is idempotent:

```bash
kubectl apply -f generated-pod.yaml
```

Expected output: `pod/generated-pod unchanged`.

Confirm the last-applied-configuration annotation exists:

```bash
kubectl get pod generated-pod -o jsonpath='{.metadata.annotations}' | grep last-applied
```

Expected: a JSON string containing the Pod spec.

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete pod quick-pod generated-pod
kubectl delete svc quick-svc
rm -f generated-pod.yaml
```

Verify nothing remains:

```bash
kubectl get pods
```

## What's Next

You have now completed the five basic exercises in the Pods & Pod Design category. The next exercise introduces multi-container patterns -- sidecar, ambassador, and adapter -- which let you compose multiple containers within a single Pod. Continue to [exercise 06 (Multi-Container Patterns)](../06-multi-container-patterns/06-multi-container-patterns.md).

## Summary

- **Imperative commands** (`kubectl run`, `kubectl create`) are fast but leave no audit trail and are not idempotent.
- **Declarative management** (`kubectl apply`) computes diffs, is idempotent, and integrates with version control.
- Use `--dry-run=client -o yaml` to scaffold manifests from imperative commands.
- `kubectl apply` uses a **three-way merge** (last-applied annotation, live state, desired state) to compute changes.
- Always treat the YAML file as the **source of truth** in production workflows.

## Reference

- [Declarative Management](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/) — official guide to kubectl apply
- [Imperative Management](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/imperative-command/) — imperative command patterns
- [Object Management](https://kubernetes.io/docs/concepts/overview/working-with-objects/object-management/) — comparing approaches

## Additional Resources

- [kubectl apply Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_apply/) — full command documentation
- [Server-Side Apply](https://kubernetes.io/docs/reference/using-api/server-side-apply/) — the newer apply mechanism
- [GitOps Principles](https://opengitops.dev/) — declarative management in practice
