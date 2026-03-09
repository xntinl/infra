# 15. Multi-Container Debugging Challenge

<!--
difficulty: insane
concepts: [multi-container-debugging, init-containers, sidecar, volume-mounts, process-inspection, log-analysis]
tools: [kubectl, minikube]
estimated_time: 60m
bloom_level: create
prerequisites: [01-06, 01-07, 01-08]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running **v1.29+**
- `kubectl` installed and configured
- Completion of [exercise 06 (Multi-Container Patterns)](../06-multi-container-patterns/), [exercise 07 (Init Containers)](../07-init-containers/), and [exercise 08 (Ephemeral Containers)](../08-ephemeral-containers-and-debugging/)

## The Scenario

Your team inherited a microservice that runs as a multi-container Pod with two init containers and three runtime containers (including a native sidecar). The Pod is stuck and will not reach `Running` status. The original developer left the company. No documentation exists. The only artifacts are the YAML manifests below.

Your job: deploy the broken Pod, diagnose every issue using only `kubectl` commands (logs, describe, exec, debug), fix all problems, and produce a working Pod. You may not redesign the architecture -- the multi-container pattern and the overall structure must be preserved.

## Constraints

1. The Pod must use **exactly** the structure defined below: 2 init containers, 1 native sidecar, and 2 main containers.
2. You may only modify the YAML to fix bugs -- do not change the container names, the overall command intent, or the volume names.
3. All fixes must be explainable: for each bug you find, write a one-sentence description of what was wrong and why.
4. You must use `kubectl debug`, `kubectl logs`, and `kubectl describe` as your primary diagnostic tools.
5. The final working Pod must have all containers in `Running` state (or `Terminated: Completed` for init containers) and produce the expected output.

## The Broken Manifest

Deploy this manifest and begin debugging:

```yaml
# broken-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: broken-multi
spec:
  initContainers:
    - name: config-generator
      image: busybox:1.37
      command: ["sh", "-c", "echo 'BACKEND_URL=http://localhost:8080' > /config/env.conf && echo 'LOG_LEVEL=debug' >> /config/env.conf"]
      volumeMounts:
        - name: config
          mountPath: /settings
    - name: data-seeder
      image: busybox:1.37
      command: ["sh", "-c", "mkdir -p /data/cache && echo '{\"seeded\": true}' > /data/cache/seed.json"]
      volumeMounts:
        - name: data-vol
          mountPath: /data
    - name: log-collector
      image: busybox:1.37
      restartPolicy: Always
      command: ["sh", "-c", "tail -F /logs/app.log"]
      volumeMounts:
        - name: log-vol
          mountPath: /logs
  containers:
    - name: backend
      image: busybox:1.37
      command: ["sh", "-c", "source /config/env.conf && echo \"Backend started with $BACKEND_URL\" >> /logs/app.log && while true; do echo \"$(date) heartbeat\" >> /logs/app.log; sleep 5; done"]
      volumeMounts:
        - name: config
          mountPath: /config
        - name: data-vol
          mountPath: /data
        - name: log-vol
          mountPath: /logs
    - name: frontend
      image: busybox:1.37
      command: ["sh", "-c", "cat /data/cache/seed.json >> /logs/app.log && while true; do echo \"$(date) frontend alive\" >> /logs/app.log; sleep 5; done"]
      volumeMounts:
        - name: data-vol
          mountPath: /data
        - name: log-vol
          mountPath: /logs
  volumes:
    - name: config
      emptyDir: {}
    - name: data-vol
      emptyDir: {}
    - name: log-vol
      emptyDir: {}
```

## Success Criteria

1. All init containers complete successfully (exit 0).
2. The native sidecar (`log-collector`) is running and streaming logs.
3. The `backend` container is running, sourcing the config, and writing heartbeat messages.
4. The `frontend` container is running, reading the seed data, and writing alive messages.
5. `kubectl logs broken-multi -c log-collector --tail=10` shows interleaved heartbeat and alive messages.
6. `kubectl exec broken-multi -c backend -- cat /config/env.conf` shows the config values.
7. `kubectl exec broken-multi -c frontend -- cat /data/cache/seed.json` shows the seeded data.

## Verification Commands

```bash
kubectl get pod broken-multi
# Expected: Running, 3/3

kubectl logs broken-multi -c log-collector --tail=5
# Expected: timestamped heartbeat and frontend alive messages

kubectl exec broken-multi -c backend -- cat /config/env.conf
# Expected: BACKEND_URL and LOG_LEVEL entries

kubectl exec broken-multi -c frontend -- cat /data/cache/seed.json
# Expected: {"seeded": true}
```

## Cleanup

```bash
kubectl delete pod broken-multi
```
