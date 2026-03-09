# 7. Init Containers for Pre-Flight Checks

<!--
difficulty: intermediate
concepts: [init-containers, dependency-gating, sequential-initialization, emptyDir, service-readiness]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-01, 01-06]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Your First Pod)](../01-your-first-pod/) and [exercise 06 (Multi-Container Patterns)](../06-multi-container-patterns/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** init containers to perform setup tasks before the main application starts
- **Analyze** how multiple init containers execute sequentially and how failures are handled
- **Apply** init containers for dependency gating (waiting for a service to become available)

## Why Init Containers?

Init containers run to completion before any main container starts. They are ideal for setup logic that does not belong in the application image: downloading configuration, running database migrations, waiting for upstream services, or populating shared volumes. Each init container must exit 0 before the next one begins. If one fails, the kubelet retries it according to the Pod restart policy.

## Step 1: Sequential Init Containers

Create a Pod with two init containers that prepare data for the main container:

```yaml
# init-sequence.yaml
apiVersion: v1
kind: Pod
metadata:
  name: init-sequence
spec:
  initContainers:
    - name: create-config
      image: busybox:1.37
      command: ["sh", "-c", "echo 'db_host=postgres' > /config/app.conf && echo 'Init 1: config created'"]
      volumeMounts:
        - name: config-vol
          mountPath: /config
    - name: validate-config
      image: busybox:1.37
      command: ["sh", "-c", "cat /config/app.conf && echo 'Init 2: config validated'"]
      volumeMounts:
        - name: config-vol
          mountPath: /config
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Main: using config:' && cat /config/app.conf && sleep 3600"]
      volumeMounts:
        - name: config-vol
          mountPath: /config
  volumes:
    - name: config-vol
      emptyDir: {}
```

```bash
kubectl apply -f init-sequence.yaml
kubectl get pod init-sequence -w
```

Watch the `Init:0/2` counter progress to `Init:1/2`, then `PodInitializing`, then `Running`. Press `Ctrl+C` when the Pod is running.

Verify the init containers ran in order:

```bash
kubectl logs init-sequence -c create-config
kubectl logs init-sequence -c validate-config
kubectl logs init-sequence -c app
```

## Step 2: Dependency Gating

A common pattern is an init container that waits for a dependency to be available before the main container starts. Create a Pod that waits for a Service called `mydb`:

```yaml
# wait-for-service.yaml
apiVersion: v1
kind: Pod
metadata:
  name: wait-for-db
spec:
  initContainers:
    - name: wait-for-mydb
      image: busybox:1.37
      command: ["sh", "-c", "until nslookup mydb.default.svc.cluster.local; do echo 'Waiting for mydb...'; sleep 2; done"]
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo 'Database is available!' && sleep 3600"]
```

```bash
kubectl apply -f wait-for-service.yaml
kubectl get pod wait-for-db
```

The Pod will be stuck in `Init:0/1` because the `mydb` Service does not exist yet. Check the init container logs:

```bash
kubectl logs wait-for-db -c wait-for-mydb
```

You should see repeated "Waiting for mydb..." messages. Now create the Service:

```yaml
# mydb-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: mydb
spec:
  selector:
    app: mydb
  ports:
    - port: 5432
---
apiVersion: v1
kind: Pod
metadata:
  name: mydb-pod
  labels:
    app: mydb
spec:
  containers:
    - name: postgres
      image: postgres:16
      env:
        - name: POSTGRES_PASSWORD
          value: "testpass"
```

```bash
kubectl apply -f mydb-service.yaml
```

Watch the waiting Pod transition:

```bash
kubectl get pod wait-for-db -w
```

The init container will resolve the DNS, exit 0, and the main container will start.

## Step 3: Init Container Failure Behavior

Create a Pod with an init container that fails:

```yaml
# init-failure.yaml
apiVersion: v1
kind: Pod
metadata:
  name: init-failure
spec:
  restartPolicy: Never
  initContainers:
    - name: will-fail
      image: busybox:1.37
      command: ["sh", "-c", "echo 'About to fail'; exit 1"]
  containers:
    - name: app
      image: nginx:1.27
```

```bash
kubectl apply -f init-failure.yaml
sleep 5
kubectl get pod init-failure
```

The Pod status shows `Init:Error`. With `restartPolicy: Never`, the failed init container is not retried and the main container never starts.

Inspect the failure:

```bash
kubectl describe pod init-failure | grep -A 5 "Init Containers"
```

## Spot the Bug

Why does this Pod never become Ready?

```yaml
initContainers:
  - name: setup
    image: busybox:1.37
    command: ["sh", "-c", "echo hello > /data/greeting"]
    volumeMounts:
      - name: shared
        mountPath: /data
containers:
  - name: app
    image: busybox:1.37
    command: ["sh", "-c", "cat /shared/greeting && sleep 3600"]
    volumeMounts:
      - name: shared
        mountPath: /shared
```

<details>
<summary>Explanation</summary>

The init container writes to `/data/greeting` on the volume, but the main container mounts the same volume at `/shared` and reads from `/shared/greeting`. Since both mount the same emptyDir volume, the file is at the volume root as `greeting`. The init container writes it relative to its mount path (`/data/greeting` -> volume path `/greeting`), and the main container reads from `/shared/greeting` which is the same volume path `/greeting`. This actually works correctly. The real bug to watch for would be if the volume names did not match -- always verify both containers reference the same volume name.

</details>

## Verify What You Learned

```bash
kubectl get pod init-sequence       # Running, 1/1
kubectl get pod wait-for-db         # Running, 1/1
kubectl get pod init-failure        # Init:Error, 0/1
```

Check the init container completion status:

```bash
kubectl get pod init-sequence -o jsonpath='{.status.initContainerStatuses[*].state}'
```

Both init containers should show `terminated` with `reason: Completed`.

## Cleanup

```bash
kubectl delete pod init-sequence wait-for-db init-failure mydb-pod
kubectl delete svc mydb
```

## What's Next

Init containers handle pre-startup logic, but sometimes you need to debug a running Pod without modifying its spec. In the next exercise, you will learn about ephemeral containers and `kubectl debug`. Continue to [exercise 08 (Ephemeral Containers and kubectl debug)](../08-ephemeral-containers-and-debugging/).

## Summary

- Init containers run **sequentially** before main containers; each must exit 0
- Use init containers for **dependency gating** (DNS lookups, port checks) and **data preparation**
- Failed init containers prevent main containers from starting
- Init containers share volumes with main containers via `emptyDir` or other volume types
- The Pod status shows `Init:X/Y` progress during initialization

## Reference

- [Init Containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) — official concept documentation
- [Init Containers in Practice](https://kubernetes.io/docs/tasks/configure-pod-container/configure-pod-initialization/) — tutorial
- [Pod Lifecycle](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/) — phases and conditions

## Additional Resources

- [Kubernetes API Reference: Pod v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/)
- [Debugging Init Containers](https://kubernetes.io/docs/tasks/debug/debug-application/debug-init-containers/)
- [Volumes - emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir)
