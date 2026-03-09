# 8. Ephemeral Containers and kubectl debug

<!--
difficulty: intermediate
concepts: [ephemeral-containers, kubectl-debug, distroless-debugging, process-namespace-sharing]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-01, 01-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running **v1.25+** (ephemeral containers GA)
- `kubectl` installed and configured
- Completion of [exercise 01 (Your First Pod)](../01-your-first-pod/) and [exercise 07 (Init Containers)](../07-init-containers/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** `kubectl debug` to attach ephemeral containers to running Pods
- **Analyze** issues in distroless or minimal images that lack debugging tools
- **Apply** process namespace sharing to inspect processes in other containers

## Why Ephemeral Containers?

Production images should be minimal -- no shell, no package manager, no debugging tools. But when something goes wrong, you need to inspect the running container. Ephemeral containers solve this paradox: they let you inject a temporary debugging container into a running Pod without restarting it. The ephemeral container shares the Pod's network namespace and can optionally share the process namespace of another container.

## Step 1: Deploy a Minimal Application

Create a Pod running a distroless image:

```yaml
# minimal-app.yaml
apiVersion: v1
kind: Pod
metadata:
  name: minimal-app
spec:
  containers:
    - name: app
      image: registry.k8s.io/pause:3.9
```

```bash
kubectl apply -f minimal-app.yaml
kubectl get pod minimal-app
```

Try to exec into it:

```bash
kubectl exec -it minimal-app -- sh
```

This fails because the pause image has no shell. In production, many images are similarly minimal (distroless, scratch-based, etc.).

## Step 2: Attach an Ephemeral Debug Container

Use `kubectl debug` to inject a debug container:

```bash
kubectl debug -it minimal-app --image=busybox:1.37 --target=app -- sh
```

The `--target=app` flag enables process namespace sharing with the `app` container. Inside the debug shell, you can now:

```bash
# See processes from the target container
ps aux

# Check the network (shared with the Pod)
ifconfig
wget -qO- http://localhost 2>&1 || echo "No web server"

# Exit the debug session
exit
```

Verify the ephemeral container was added:

```bash
kubectl describe pod minimal-app | grep -A 10 "Ephemeral Containers"
```

The ephemeral container is recorded in the Pod spec but cannot be removed. It stays until the Pod is deleted.

## Step 3: Debug by Copying a Pod

Sometimes you want to debug a copy of the Pod with different settings rather than modifying the live one:

```yaml
# nginx-prod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: nginx-prod
spec:
  containers:
    - name: nginx
      image: nginx:1.27
```

```bash
kubectl apply -f nginx-prod.yaml
```

Create a debug copy with a different image:

```bash
kubectl debug nginx-prod -it --copy-to=nginx-debug --image=busybox:1.37 --share-processes -- sh
```

Inside the copied Pod, you have a busybox shell with access to the shared process namespace. List processes:

```bash
ps aux
exit
```

The original Pod (`nginx-prod`) is untouched. The debug copy (`nginx-debug`) is a separate Pod.

```bash
kubectl get pods nginx-prod nginx-debug
```

## Step 4: Debug a Node

`kubectl debug` can also create a debugging Pod on a specific node:

```bash
NODE_NAME=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl debug node/$NODE_NAME -it --image=busybox:1.37 -- sh
```

Inside the debug shell, the host filesystem is mounted at `/host`:

```bash
ls /host/etc/kubernetes
exit
```

Clean up the node debug Pod:

```bash
kubectl get pods | grep node-debugger
kubectl delete pod $(kubectl get pods -o name | grep node-debugger)
```

## Spot the Bug

A developer runs `kubectl debug -it my-pod --image=busybox:1.37 -- sh` but cannot see the processes of the main container. Why?

<details>
<summary>Explanation</summary>

The `--target` flag was omitted. Without `--target=<container-name>`, process namespace sharing is not enabled, and the ephemeral container runs in its own process namespace. Add `--target=app` (or whatever the main container name is) to see its processes.

</details>

## Verify What You Learned

```bash
kubectl describe pod minimal-app | grep "Ephemeral Containers" -A 5
```

Expected: shows the busybox ephemeral container that was attached.

```bash
kubectl get pod nginx-debug
```

Expected: the debug copy exists alongside the original.

## Cleanup

```bash
kubectl delete pod minimal-app nginx-prod nginx-debug
```

## What's Next

You have learned how to debug running Pods without modifying their spec. The next exercise covers Pod lifecycle hooks -- `postStart` and `preStop` -- which let you run commands at container startup and shutdown. Continue to [exercise 09 (Pod Lifecycle Hooks)](../09-pod-lifecycle-hooks/).

## Summary

- **Ephemeral containers** are injected into running Pods for debugging without restarts
- Use `--target=<container>` to enable process namespace sharing with a specific container
- `kubectl debug --copy-to` creates a separate debug copy of the Pod
- `kubectl debug node/<name>` creates a debug Pod with host filesystem access
- Ephemeral containers cannot be removed from a Pod once added

## Reference

- [Ephemeral Containers](https://kubernetes.io/docs/concepts/workloads/pods/ephemeral-containers/) — official concept documentation
- [Debugging with Ephemeral Containers](https://kubernetes.io/docs/tasks/debug/debug-application/debug-running-pod/#ephemeral-container) — practical guide
- [kubectl debug Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_debug/) — full command documentation

## Additional Resources

- [Share Process Namespace](https://kubernetes.io/docs/tasks/configure-pod-container/share-process-namespace/) — how process sharing works
- [Distroless Container Images](https://github.com/GoogleContainerTools/distroless) — why minimal images matter
- [KEP-277: Ephemeral Containers](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/277-ephemeral-containers)
