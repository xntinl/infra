# 6. Multi-Container Patterns: Init, Sidecar, and Ambassador

<!--
difficulty: advanced
concepts: [init-containers, sidecar, ambassador, emptyDir, shared-volumes]
tools: [kubectl, minikube]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [01-01, 01-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running **v1.29+** (for native sidecar support)
- `kubectl` installed and configured
- Completion of [exercise 01](../01-your-first-pod/) and [exercise 05](../05-declarative-vs-imperative/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** pods using init containers, native sidecars, and the ambassador pattern
- **Analyze** when each multi-container pattern is appropriate for a given use case
- **Evaluate** volume sharing between containers and common configuration pitfalls

## Why Multi-Container Patterns?

Real applications rarely run in isolation. A web server needs configuration fetched before startup (init), a log shipper running alongside (sidecar), or a proxy abstracting external dependencies (ambassador). Multi-container pods let you compose these concerns without modifying the main application image, following the single-responsibility principle at the container level.

## The Challenge

Requires Kubernetes v1.29+ for native sidecar support. Build three pods demonstrating different patterns:

### Pod 1: Init Container (`init-demo`)
- Init container (`alpine/git:2.43.0`) clones `https://github.com/nginxinc/NGINX-Demos.git` into a shared `emptyDir` at `/work`
- Main container (`nginx:1.25`) mounts the same volume at `/usr/share/nginx/html` to serve the content

### Pod 2: Native Sidecar (`sidecar-demo`)
- Native sidecar (`busybox:1.36`, defined in `initContainers` with `restartPolicy: Always`) runs `tail -F /var/log/app/output.log`
- Main container (`busybox:1.36`) writes `"$(date) - heartbeat"` to the same file every 2 seconds
- Both share an `emptyDir` at `/var/log/app`

### Pod 3: Ambassador (`ambassador-demo`)
- Main container (`redis:7`) connects to `localhost:6380`
- Ambassador container (`alpine/socat:1.8.0.0`) runs `socat TCP-LISTEN:6380,fork,reuseaddr TCP:redis-backend:6379`
- Also create a backing `redis-backend` Deployment (1 replica, `redis:7`) and ClusterIP Service

<details>
<summary>Hint 1: Init container syntax</summary>

Init containers go in a separate `initContainers` array. Each must exit 0 before the next starts.

</details>

<details>
<summary>Hint 2: emptyDir shared volume</summary>

```yaml
volumes:
  - name: shared
    emptyDir: {}
```

Mount it in both containers. The init container populates it; the main container reads it.

</details>

<details>
<summary>Hint 3: Native sidecar (v1.29+)</summary>

Define in `initContainers` with `restartPolicy: Always`. It starts before main containers but keeps running.

</details>

<details>
<summary>Hint 4: Ambassador with socat</summary>

The main container connects to localhost:6380. The socat ambassador forwards to `redis-backend:6379`.

</details>

<details>
<summary>Hint 5: Checking specific container logs</summary>

```bash
kubectl logs sidecar-demo -c log-streamer --tail=5
```

</details>

## Spot the Bug

The init container clones to `/data` and the main container mounts the volume at `/usr/share/nginx`. **Why does nginx serve the default page?**

```yaml
initContainers:
  - name: git-clone
    image: alpine/git:2.43.0
    command: ["git", "clone", "https://github.com/nginxinc/NGINX-Demos.git", "/data"]
    volumeMounts:
      - { name: content, mountPath: /data }
containers:
  - name: web
    image: nginx:1.25
    volumeMounts:
      - { name: content, mountPath: /usr/share/nginx }  # <-- BUG
```

<details>
<summary>Explanation</summary>

Nginx serves from `/usr/share/nginx/html`, not `/usr/share/nginx`. The volume mount at `/usr/share/nginx` overwrites the entire directory (including the default `html/` subfolder), and the cloned files sit at the volume root with no `html/index.html`. Fix: mount at `/usr/share/nginx/html` and clone to a matching path.

</details>

## Verify What You Learned

```bash
kubectl get pod init-demo                   # 1/1 Running
kubectl exec init-demo -- ls /usr/share/nginx/html/  # cloned files visible
```

```bash
kubectl get pod sidecar-demo                # 2/2 Running
kubectl logs sidecar-demo -c log-streamer --tail=3    # timestamped heartbeats
```

```bash
kubectl get pod ambassador-demo             # 2/2 Running
kubectl exec ambassador-demo -c main -- redis-cli -p 6380 ping  # PONG
```

## Cleanup

```bash
kubectl delete pod init-demo sidecar-demo ambassador-demo
kubectl delete deployment redis-backend
kubectl delete service redis-backend
```

## What's Next

In the next exercise, you will dive deeper into init containers and learn how to use them for sophisticated pre-flight checks and dependency gating. Continue to [exercise 07 (Init Containers for Pre-Flight Checks)](../07-init-containers/).

## Summary

- **Init containers** run sequentially before main containers — use them for setup tasks (cloning repos, migrations, waiting for dependencies)
- **Native sidecars** (v1.29+) are defined in `initContainers` with `restartPolicy: Always` — they start before and outlive main containers
- **Ambassador containers** proxy connections to external services, decoupling the main app from service discovery details
- All patterns rely on shared volumes (`emptyDir`) for inter-container communication

## Reference

- [Init Containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
- [Sidecar Containers](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/)
- [Volumes - emptyDir](https://kubernetes.io/docs/concepts/storage/volumes/#emptydir)

## Additional Resources

- [Multi-container Pod Design Patterns](https://kubernetes.io/blog/2015/06/the-distributed-system-toolkit-patterns/)
- [KEP-753: Sidecar Containers](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers)

---

<details>
<summary>Solution</summary>

Save as `all-patterns.yaml` (multi-document):

```yaml
# Pod 1: Init Container
apiVersion: v1
kind: Pod
metadata: { name: init-demo }
spec:
  initContainers:
    - name: git-clone
      image: alpine/git:2.43.0
      command: ["git", "clone", "https://github.com/nginxinc/NGINX-Demos.git", "/work"]
      volumeMounts: [{ name: content, mountPath: /work }]
  containers:
    - name: web
      image: nginx:1.25
      ports: [{ containerPort: 80 }]
      volumeMounts: [{ name: content, mountPath: /usr/share/nginx/html }]
  volumes: [{ name: content, emptyDir: {} }]
---
# Pod 2: Native Sidecar
apiVersion: v1
kind: Pod
metadata: { name: sidecar-demo }
spec:
  initContainers:
    - name: log-streamer
      image: busybox:1.36
      restartPolicy: Always
      command: ["sh", "-c", "tail -F /var/log/app/output.log"]
      volumeMounts: [{ name: logs, mountPath: /var/log/app }]
  containers:
    - name: main-app
      image: busybox:1.36
      command: ["sh", "-c", "while true; do echo \"$(date) - heartbeat\" >> /var/log/app/output.log; sleep 2; done"]
      volumeMounts: [{ name: logs, mountPath: /var/log/app }]
  volumes: [{ name: logs, emptyDir: {} }]
---
# Redis backend for ambassador pattern
apiVersion: apps/v1
kind: Deployment
metadata: { name: redis-backend }
spec:
  replicas: 1
  selector: { matchLabels: { app: redis-backend } }
  template:
    metadata: { labels: { app: redis-backend } }
    spec:
      containers:
        - { name: redis, image: "redis:7", ports: [{ containerPort: 6379 }] }
---
apiVersion: v1
kind: Service
metadata: { name: redis-backend }
spec:
  selector: { app: redis-backend }
  ports: [{ port: 6379, targetPort: 6379 }]
---
# Pod 3: Ambassador
apiVersion: v1
kind: Pod
metadata: { name: ambassador-demo }
spec:
  containers:
    - name: main
      image: redis:7
      command: ["sh", "-c", "sleep infinity"]
    - name: ambassador
      image: alpine/socat:1.8.0.0
      command: ["socat", "TCP-LISTEN:6380,fork,reuseaddr", "TCP:redis-backend:6379"]
```

```bash
kubectl apply -f all-patterns.yaml
kubectl wait --for=condition=available deployment/redis-backend --timeout=60s
```

</details>
