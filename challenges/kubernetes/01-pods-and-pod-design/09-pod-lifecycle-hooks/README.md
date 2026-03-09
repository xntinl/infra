# 9. Pod Lifecycle Hooks: postStart and preStop

<!--
difficulty: intermediate
concepts: [lifecycle-hooks, postStart, preStop, graceful-shutdown, terminationGracePeriodSeconds]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-02, 01-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 02 (Pod Lifecycle and Restart Policies)](../02-pod-lifecycle-and-restart-policies/) and [exercise 07 (Init Containers)](../07-init-containers/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** postStart and preStop lifecycle hooks to containers
- **Analyze** the ordering guarantees and failure behavior of lifecycle hooks
- **Apply** preStop hooks for graceful shutdown patterns

## Why Lifecycle Hooks?

Containers sometimes need to run logic at startup or shutdown that does not belong in the main application process. A `postStart` hook runs immediately after the container is created (but not necessarily before the main command). A `preStop` hook runs before the container is terminated, giving it time to drain connections, save state, or deregister from a service registry. Without preStop hooks, a container receiving SIGTERM may not have enough time to finish in-flight requests before Kubernetes kills it.

## Step 1: postStart Hook

Create a Pod with a postStart hook that writes a file:

```yaml
# poststart-demo.yaml
apiVersion: v1
kind: Pod
metadata:
  name: poststart-demo
spec:
  containers:
    - name: app
      image: nginx:1.27
      lifecycle:
        postStart:
          exec:
            command: ["sh", "-c", "echo 'postStart executed at $(date)' > /usr/share/nginx/html/hook-status.txt"]
```

```bash
kubectl apply -f poststart-demo.yaml
sleep 5
kubectl exec poststart-demo -- cat /usr/share/nginx/html/hook-status.txt
```

Expected output: `postStart executed at <timestamp>`.

The postStart hook runs in parallel with the container's main process. There is no guarantee it completes before the container's entrypoint runs. If the hook fails, the container is killed.

## Step 2: preStop Hook for Graceful Shutdown

Create a Pod that uses a preStop hook to drain gracefully:

```yaml
# prestop-demo.yaml
apiVersion: v1
kind: Pod
metadata:
  name: prestop-demo
spec:
  terminationGracePeriodSeconds: 30
  containers:
    - name: app
      image: nginx:1.27
      lifecycle:
        preStop:
          exec:
            command: ["sh", "-c", "echo 'Draining...' >> /tmp/shutdown.log && sleep 5 && echo 'Done draining' >> /tmp/shutdown.log && nginx -s quit"]
```

```bash
kubectl apply -f prestop-demo.yaml
kubectl wait --for=condition=Ready pod/prestop-demo
```

Now delete the Pod and observe the graceful shutdown:

```bash
kubectl delete pod prestop-demo &
sleep 2
kubectl get pod prestop-demo
```

The Pod status shows `Terminating`. The preStop hook runs for 5 seconds before sending `nginx -s quit`. After the hook completes, Kubernetes sends SIGTERM. The `terminationGracePeriodSeconds: 30` gives the entire shutdown sequence 30 seconds before a SIGKILL.

Wait for the deletion to complete:

```bash
wait
```

## Step 3: HTTP Hook

Lifecycle hooks can also use `httpGet` instead of `exec`:

```yaml
# http-hook.yaml
apiVersion: v1
kind: Pod
metadata:
  name: http-hook-demo
spec:
  containers:
    - name: app
      image: nginx:1.27
      ports:
        - containerPort: 80
      lifecycle:
        postStart:
          httpGet:
            path: /
            port: 80
```

```bash
kubectl apply -f http-hook.yaml
sleep 5
kubectl describe pod http-hook-demo | grep -A 3 "PostStart"
```

The httpGet hook sends a GET request to the specified path and port. A 2xx or 3xx response means success. Any other response or a connection failure means the hook failed.

## Step 4: Hook Failure Behavior

Create a Pod with a postStart hook that fails:

```yaml
# hook-failure.yaml
apiVersion: v1
kind: Pod
metadata:
  name: hook-failure
spec:
  containers:
    - name: app
      image: nginx:1.27
      lifecycle:
        postStart:
          exec:
            command: ["sh", "-c", "exit 1"]
```

```bash
kubectl apply -f hook-failure.yaml
sleep 10
kubectl get pod hook-failure
kubectl describe pod hook-failure | grep -A 5 "Events"
```

The Pod may show `CrashLoopBackOff` or repeated restarts because the failed postStart hook causes the container to be killed and restarted.

## Spot the Bug

A developer sets `terminationGracePeriodSeconds: 5` but the preStop hook does `sleep 30`. What happens?

<details>
<summary>Explanation</summary>

The termination grace period is a hard deadline for the entire shutdown sequence (preStop hook + SIGTERM handling). After 5 seconds, Kubernetes sends SIGKILL regardless of whether the preStop hook finished. The hook effectively gets truncated. Always ensure your preStop hook duration plus any SIGTERM handling time fits within `terminationGracePeriodSeconds`.

</details>

## Verify What You Learned

```bash
kubectl get pod poststart-demo           # Running
kubectl get pod http-hook-demo           # Running
kubectl get pod hook-failure             # CrashLoopBackOff or restarting
```

Verify the postStart hook created the file:

```bash
kubectl exec poststart-demo -- cat /usr/share/nginx/html/hook-status.txt
```

## Cleanup

```bash
kubectl delete pod poststart-demo http-hook-demo hook-failure
```

## What's Next

Lifecycle hooks let you run logic at container boundaries. The next exercise explores the Downward API, which lets containers access Pod metadata (name, namespace, labels, resource limits) without calling the Kubernetes API. Continue to [exercise 10 (Downward API and Pod Metadata)](../10-downward-api-and-pod-metadata/).

## Summary

- **postStart** runs after container creation, in parallel with the main process; failure kills the container
- **preStop** runs before SIGTERM during termination; use it for graceful drain and cleanup
- Hooks support `exec` (run a command) and `httpGet` (send an HTTP request)
- `terminationGracePeriodSeconds` is the hard deadline for the entire shutdown sequence
- Hook execution is "at least once" -- there is no guarantee against duplicate execution

## Reference

- [Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/) — official concept documentation
- [Termination of Pods](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination) — shutdown sequence
- [Configure Liveness, Readiness, and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/) — related lifecycle mechanisms

## Additional Resources

- [Kubernetes API Reference: Container Lifecycle](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#lifecycle-1)
- [Graceful Shutdown in Kubernetes](https://learnk8s.io/graceful-shutdown)
- [Pod Disruption Budgets](https://kubernetes.io/docs/concepts/workloads/pods/disruptions/) — controlling voluntary disruptions
