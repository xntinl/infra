# 1. Your First Pod

<!--
difficulty: basic
concepts: [pod, container, kubectl, declarative-vs-imperative]
tools: [kubectl, minikube]
estimated_time: 20m
bloom_level: understand
prerequisites: [none]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- `curl` available on your machine

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the purpose of a Pod as the smallest deployable unit in Kubernetes
- **Understand** the difference between imperative commands and declarative YAML manifests
- **Apply** kubectl commands to create, inspect, and delete Pods

## Why Pods?

Kubernetes does not run containers directly. Instead, it wraps every container in an abstraction called a Pod. A Pod is the smallest unit the scheduler can place onto a node. Even if you only need one container, Kubernetes still creates a Pod around it. This wrapper gives the platform a uniform interface for networking, storage, and lifecycle management regardless of which container runtime sits underneath.

Why not just run bare containers? Because Kubernetes needs to track health, assign IP addresses, attach volumes, and enforce resource limits at a consistent layer. The Pod provides that layer. Every container in a Pod shares the same network namespace (they see `localhost` as each other) and the same set of volumes. The kubelet on each node is responsible for pulling images, starting containers, restarting them on failure, and reporting status back to the API server.

Understanding Pods is the foundation for everything else. Deployments, StatefulSets, DaemonSets, and Jobs all create Pods under the hood. If you know how to inspect, debug, and reason about a single Pod, you can troubleshoot any higher-level workload. This exercise walks you through both the imperative (quick command) and declarative (YAML manifest) approaches so you understand the tradeoffs of each.

## Step 1: Create a Pod Imperatively

The imperative approach is useful for quick experiments. Run a single command to create a Pod:

```bash
kubectl run nginx-test --image=nginx:1.27 --port=80
```

Verify the Pod is running:

```bash
kubectl get pods
```

Check the container logs:

```bash
kubectl logs nginx-test
```

Clean up the imperative Pod before moving on:

```bash
kubectl delete pod nginx-test
```

The imperative approach is fast but leaves no record of what you did. In production, you always want a YAML file checked into version control. That is the declarative approach.

## Step 2: Create a Pod Declaratively

Create `pod.yaml`:

```yaml
# pod.yaml
apiVersion: v1              # Core API group — Pods live here, not in apps/v1
kind: Pod                   # The resource type we are creating
metadata:
  name: nginx-pod           # Unique name within the namespace
  labels:
    app: nginx              # Key-value pair used later by Services and selectors
spec:
  containers:
    - name: nginx           # Container name — used in logs and kubectl exec
      image: nginx:1.27     # Always pin a specific tag; avoid :latest in practice
      ports:
        - containerPort: 80 # Informational only — does NOT open or expose the port
```

Apply the manifest:

```bash
kubectl apply -f pod.yaml
```

Verify the Pod reaches `Running` status:

```bash
kubectl get pod nginx-pod
```

Check the logs to confirm nginx started:

```bash
kubectl logs nginx-pod
```

Use port-forward to reach the container from your machine:

```bash
kubectl port-forward nginx-pod 8080:80 &
curl -s localhost:8080 | head -4
```

Stop the port-forward when done:

```bash
kill %1
```

## Step 3: Explore the Full Pod Object

Kubernetes stores far more information about a Pod than what you put in your YAML. Retrieve the full object:

```bash
kubectl get pod nginx-pod -o yaml
```

Key fields to notice in the output:

- `status.phase` — the current lifecycle phase (`Pending`, `Running`, `Succeeded`, `Failed`)
- `status.podIP` — the cluster-internal IP address assigned to this Pod
- `spec.nodeName` — which node the scheduler placed the Pod on
- `status.conditions` — detailed readiness and scheduling conditions
- `metadata.uid` — the globally unique identifier for this specific Pod instance

You can also use `describe` for a human-readable summary:

```bash
kubectl describe pod nginx-pod
```

The `Events` section at the bottom shows the scheduler assigning a node, the kubelet pulling the image, and the container starting. This is the first place to look when troubleshooting.

## Common Mistakes

### Mistake 1: Wrong apiVersion for a Pod

Using `apps/v1` instead of `v1`:

```yaml
# WRONG
apiVersion: apps/v1
kind: Pod
metadata:
  name: nginx-pod
```

Error message:

```
error: resource mapping not found for name: "nginx-pod" namespace: "" from "pod.yaml":
no matches for kind "Pod" in version "apps/v1"
```

**Fix:** Pods belong to the core API group. Use `apiVersion: v1`. The `apps/v1` group is for Deployments, StatefulSets, and DaemonSets.

### Mistake 2: Thinking containerPort Opens the Port

Some users believe that removing `containerPort: 80` from the spec will prevent connections to port 80. It will not. The `containerPort` field is purely informational metadata. The port is opened by the nginx process inside the container, not by Kubernetes.

```yaml
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      # No ports section at all
```

Applying this manifest and running `kubectl port-forward` to port 80 still works exactly the same way. The field exists to document which ports your container listens on, and tools like `kubectl describe` display it, but it has no effect on networking behavior.

## Verify What You Learned

Confirm the Pod is running:

```bash
kubectl get pod nginx-pod
```

Expected output:

```
NAME        READY   STATUS    RESTARTS   AGE
nginx-pod   1/1     Running   0          30s
```

Confirm nginx is serving traffic:

```bash
kubectl port-forward nginx-pod 8080:80 &
curl -s localhost:8080 | head -4
```

Expected output:

```html
<!DOCTYPE html>
<html>
<head>
<title>Welcome to nginx!</title>
```

Stop the port-forward:

```bash
kill %1
```

Confirm node and IP assignment:

```bash
kubectl describe pod nginx-pod | grep -E "Status:|IP:|Node:"
```

Expected output (values will differ):

```
Status:       Running
IP:           10.244.0.5
Node:         minikube/192.168.49.2
```

## Cleanup

Remove all resources created in this exercise:

```bash
kubectl delete pod nginx-pod
```

Verify nothing remains:

```bash
kubectl get pods
```

## What's Next

Now that you understand Pods, the next step is learning how Pod lifecycle phases and restart policies determine what happens when containers exit. Continue to [exercise 02 (Pod Lifecycle and Restart Policies)](../02-pod-lifecycle-and-restart-policies/).

## Summary

- A **Pod** is the smallest schedulable unit in Kubernetes, wrapping one or more containers.
- **Imperative** commands (`kubectl run`) are fast for experiments; **declarative** YAML files are required for reproducibility and version control.
- **Labels** are key-value metadata used by selectors to group and target Pods.
- `containerPort` is documentation only and does not affect networking.
- `kubectl describe` and `kubectl get -o yaml` are your primary tools for inspecting Pod state.

## Reference

- [Pods](https://kubernetes.io/docs/concepts/workloads/pods/) — official concept documentation
- [Pod Lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/) — phases, conditions, and restart policies
- [kubectl Cheat Sheet](https://kubernetes.io/docs/reference/kubectl/cheatsheet/) — common commands

## Additional Resources

- [Kubernetes API Reference: Pod v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/)
- [Declarative Management of Kubernetes Objects Using Configuration Files](https://kubernetes.io/docs/tasks/manage-kubernetes-objects/declarative-config/)
- [Debugging Pods](https://kubernetes.io/docs/tasks/debug/debug-application/debug-pods/)
