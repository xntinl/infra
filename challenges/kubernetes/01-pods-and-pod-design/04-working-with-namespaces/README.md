# 4. Working with Namespaces

<!--
difficulty: basic
concepts: [namespaces, resource-isolation, default-namespace, kube-system, resource-quotas]
tools: [kubectl, minikube]
estimated_time: 20m
bloom_level: understand
prerequisites: [01-01, 01-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completed [exercise 01 (Your First Pod)](../01-your-first-pod/) and [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/)

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the purpose of namespaces and the built-in namespaces Kubernetes creates
- **Understand** how namespaces provide scope for resource names and isolation boundaries
- **Apply** kubectl commands to create namespaces, deploy resources into them, and switch contexts

## Why Namespaces?

A Kubernetes cluster is shared infrastructure. Multiple teams, projects, or environments may coexist on the same cluster. Without isolation, a developer on team A could accidentally delete a Pod belonging to team B, or resource names could collide. Namespaces solve this by providing a scope for names. A Pod named `nginx` in namespace `team-a` and a Pod named `nginx` in namespace `team-b` are completely separate objects.

Namespaces also serve as boundaries for resource quotas and RBAC policies. A cluster administrator can limit how much CPU and memory a namespace may consume, or restrict which users can create resources in which namespaces. In production, namespaces are the primary tool for multi-tenancy within a single cluster.

Every cluster starts with four namespaces: `default` (where your resources go if you do not specify one), `kube-system` (for Kubernetes system components), `kube-public` (readable by all users, rarely used), and `kube-node-lease` (for node heartbeat data). Understanding when and how to create your own namespaces is a foundational skill.

## Step 1: Explore Built-In Namespaces

List all namespaces in your cluster:

```bash
kubectl get namespaces
```

Expected output:

```
NAME              STATUS   AGE
default           Active   10d
kube-node-lease   Active   10d
kube-public       Active   10d
kube-system       Active   10d
```

Look at what runs in `kube-system`:

```bash
kubectl get pods -n kube-system
```

You will see system components like `coredns`, `etcd`, `kube-apiserver`, `kube-controller-manager`, and `kube-scheduler` (depending on your cluster type).

## Step 2: Create a Custom Namespace

Create a namespace imperatively:

```bash
kubectl create namespace dev-environment
```

Or declaratively:

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: staging-environment
  labels:
    team: platform
    purpose: staging
```

```bash
kubectl apply -f namespace.yaml
```

Verify both namespaces exist:

```bash
kubectl get namespaces
```

## Step 3: Deploy Resources into a Namespace

Create a Pod in the `dev-environment` namespace:

```yaml
# dev-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: dev-environment     # Explicitly targets this namespace
  labels:
    app: nginx
spec:
  containers:
    - name: nginx
      image: nginx:1.27
```

```bash
kubectl apply -f dev-pod.yaml
```

Create a Pod with the same name in the `staging-environment` namespace:

```yaml
# staging-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx
  namespace: staging-environment
  labels:
    app: nginx
spec:
  containers:
    - name: nginx
      image: nginx:1.27
```

```bash
kubectl apply -f staging-pod.yaml
```

Both Pods are named `nginx` but they live in different namespaces and do not conflict:

```bash
kubectl get pods -n dev-environment
kubectl get pods -n staging-environment
```

## Step 4: Query Across Namespaces

List Pods in a specific namespace:

```bash
kubectl get pods -n dev-environment
```

List Pods across all namespaces:

```bash
kubectl get pods --all-namespaces
```

Or use the shorthand `-A`:

```bash
kubectl get pods -A
```

## Step 5: Set a Default Namespace

Instead of typing `-n dev-environment` on every command, set a default namespace for your current context:

```bash
kubectl config set-context --current --namespace=dev-environment
```

Now commands target `dev-environment` by default:

```bash
kubectl get pods
```

Expected: shows the `nginx` Pod in `dev-environment`.

Switch back to `default` when done:

```bash
kubectl config set-context --current --namespace=default
```

## Step 6: Namespace Isolation and DNS

Resources in different namespaces can communicate via DNS. A Service `my-svc` in namespace `team-a` is reachable from namespace `team-b` at `my-svc.team-a.svc.cluster.local`. Within the same namespace, just the service name suffices.

This is important to understand: namespaces are not network firewalls. Without NetworkPolicies, Pods in different namespaces can still reach each other. Namespaces provide name scoping and organizational boundaries, not network isolation by default.

## Common Mistakes

### Mistake 1: Forgetting to Specify the Namespace

```bash
# You created a Pod in dev-environment but query the default namespace
kubectl get pod nginx
```

```
Error from server (NotFound): pods "nginx" not found
```

**Fix:** Either add `-n dev-environment` to the command or set your default namespace context. Always be aware which namespace your kubectl context is targeting.

### Mistake 2: Trying to Delete a Non-Empty Namespace

Deleting a namespace deletes ALL resources inside it. This is by design but can be surprising:

```bash
# This deletes the namespace AND every Pod, Service, etc. inside it
kubectl delete namespace dev-environment
```

**Fix:** This is not an error but a safety concern. Always verify what is inside a namespace before deleting it: `kubectl get all -n <namespace>`.

## Verify What You Learned

Confirm both namespaces have running Pods:

```bash
kubectl get pods -n dev-environment
kubectl get pods -n staging-environment
```

Expected: each shows one `nginx` Pod in `Running` status.

Confirm namespaces exist with labels:

```bash
kubectl get namespace staging-environment --show-labels
```

Expected output includes: `purpose=staging,team=platform`.

Confirm your context is back to default:

```bash
kubectl config view --minify -o jsonpath='{.contexts[0].context.namespace}'
```

Expected output: `default` (or empty, which means default).

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete namespace dev-environment staging-environment
```

Verify the namespaces are gone:

```bash
kubectl get namespaces
```

## What's Next

You now understand how namespaces organize resources and provide scope. The next exercise covers the differences between imperative and declarative resource management in depth. Continue to [exercise 05 (Declarative vs Imperative Management)](../05-declarative-vs-imperative/).

## Summary

- **Namespaces** provide scope for resource names and boundaries for quotas and RBAC.
- Four built-in namespaces exist: `default`, `kube-system`, `kube-public`, `kube-node-lease`.
- The same resource name can exist in different namespaces without conflict.
- Use `-n <namespace>` or set a default context namespace to target the right scope.
- Deleting a namespace cascades to all resources inside it.

## Reference

- [Namespaces](https://kubernetes.io/docs/concepts/overview/working-with-objects/namespaces/) — official concept documentation
- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/) — cross-namespace DNS resolution
- [Resource Quotas](https://kubernetes.io/docs/concepts/policy/resource-quotas/) — limiting resources per namespace

## Additional Resources

- [Namespaces Walkthrough](https://kubernetes.io/docs/tasks/administer-cluster/namespaces-walkthrough/) — step-by-step tutorial
- [Configure Default CPU/Memory Requests](https://kubernetes.io/docs/tasks/administer-cluster/manage-resources/memory-default-namespace/) — LimitRange per namespace
- [RBAC Authorization](https://kubernetes.io/docs/reference/access-authn-authz/rbac/) — namespace-scoped access control
