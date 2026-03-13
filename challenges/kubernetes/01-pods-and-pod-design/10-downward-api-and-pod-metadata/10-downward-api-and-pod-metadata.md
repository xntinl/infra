# 10. Downward API and Pod Metadata

<!--
difficulty: intermediate
concepts: [downward-api, environment-variables, volume-projection, pod-metadata, resource-limits]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [01-01, 01-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Your First Pod)](../01-your-first-pod/01-your-first-pod.md) and [exercise 03 (Labels, Selectors, and Annotations)](../03-labels-selectors-and-annotations/03-labels-selectors-and-annotations.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** the Downward API to expose Pod metadata as environment variables
- **Apply** the Downward API to project Pod metadata into files via a downwardAPI volume
- **Analyze** which fields are available through the Downward API and when to use each approach

## Why the Downward API?

Containers often need to know about themselves: their Pod name (for logging), namespace (for API calls), resource limits (for tuning), or labels (for routing decisions). The Downward API exposes this information without requiring the container to call the Kubernetes API server. This avoids granting RBAC permissions and removes a runtime dependency on API server availability.

## Step 1: Pod Metadata as Environment Variables

```yaml
# downward-env.yaml
apiVersion: v1
kind: Pod
metadata:
  name: downward-env
  labels:
    app: demo
    version: v1
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "env | grep MY_ && sleep 3600"]
      resources:
        requests:
          memory: "64Mi"
          cpu: "250m"
        limits:
          memory: "128Mi"
          cpu: "500m"
      env:
        - name: MY_POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: MY_POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: MY_POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        - name: MY_NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: MY_CPU_LIMIT
          valueFrom:
            resourceFieldRef:
              containerName: app
              resource: limits.cpu
        - name: MY_MEM_LIMIT
          valueFrom:
            resourceFieldRef:
              containerName: app
              resource: limits.memory
```

```bash
kubectl apply -f downward-env.yaml
sleep 5
kubectl logs downward-env
```

Expected output (values will vary):

```
MY_POD_NAME=downward-env
MY_POD_NAMESPACE=default
MY_POD_IP=10.244.0.12
MY_NODE_NAME=minikube
MY_CPU_LIMIT=1
MY_MEM_LIMIT=134217728
```

Note: CPU limits are expressed in cores (integer), memory in bytes.

## Step 2: Pod Metadata as Files (Volume Projection)

Environment variables are set at container start and do not update. For labels and annotations that might change, use a downwardAPI volume:

```yaml
# downward-volume.yaml
apiVersion: v1
kind: Pod
metadata:
  name: downward-volume
  labels:
    app: demo
    version: v2
  annotations:
    build: "2026-03-09"
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "while true; do echo '--- Labels ---'; cat /etc/podinfo/labels; echo; echo '--- Annotations ---'; cat /etc/podinfo/annotations; sleep 10; done"]
      volumeMounts:
        - name: podinfo
          mountPath: /etc/podinfo
  volumes:
    - name: podinfo
      downwardAPI:
        items:
          - path: "labels"
            fieldRef:
              fieldPath: metadata.labels
          - path: "annotations"
            fieldRef:
              fieldPath: metadata.annotations
          - path: "name"
            fieldRef:
              fieldPath: metadata.name
          - path: "namespace"
            fieldRef:
              fieldPath: metadata.namespace
```

```bash
kubectl apply -f downward-volume.yaml
sleep 5
kubectl logs downward-volume --tail=10
```

The labels and annotations appear as file contents. Now update a label:

```bash
kubectl label pod downward-volume version=v3 --overwrite
sleep 15
kubectl exec downward-volume -- cat /etc/podinfo/labels
```

The volume content reflects the updated label. This is the key advantage over environment variables: files update dynamically.

## Step 3: Available Fields

The Downward API supports these field paths:

| Field Path | Available Via | Notes |
|-----------|--------------|-------|
| `metadata.name` | env, volume | Pod name |
| `metadata.namespace` | env, volume | Pod namespace |
| `metadata.uid` | env, volume | Pod UID |
| `metadata.labels` | volume only | All labels |
| `metadata.annotations` | volume only | All annotations |
| `spec.nodeName` | env | Node name |
| `spec.serviceAccountName` | env | Service account |
| `status.podIP` | env | Pod IP address |
| `status.hostIP` | env | Node IP address |
| `limits.cpu` | env (resourceFieldRef) | CPU limit |
| `limits.memory` | env (resourceFieldRef) | Memory limit |
| `requests.cpu` | env (resourceFieldRef) | CPU request |
| `requests.memory` | env (resourceFieldRef) | Memory request |

Note that `metadata.labels` and `metadata.annotations` are only available as volume projections, not as environment variables (because they are maps, not strings).

## Spot the Bug

Why does this Pod fail validation?

```yaml
env:
  - name: MY_LABELS
    valueFrom:
      fieldRef:
        fieldPath: metadata.labels
```

<details>
<summary>Explanation</summary>

`metadata.labels` is a map type and cannot be exposed as a single environment variable. Environment variables must be scalar values. Use a downwardAPI volume to project labels as a file, or reference a specific label with `metadata.labels['app']` (supported in Kubernetes 1.28+).

</details>

## Verify What You Learned

```bash
kubectl exec downward-env -- printenv MY_POD_NAME
# Expected: downward-env

kubectl exec downward-volume -- cat /etc/podinfo/name
# Expected: downward-volume
```

## Cleanup

```bash
kubectl delete pod downward-env downward-volume
```

## What's Next

You have now completed all five intermediate exercises. The next exercise introduces native sidecar containers (KEP-753), a Kubernetes 1.28+ feature that provides first-class sidecar lifecycle management. Continue to [exercise 11 (Native Sidecar Containers)](../11-native-sidecar-containers/11-native-sidecar-containers.md).

## Summary

- The **Downward API** exposes Pod metadata to containers without Kubernetes API server calls
- **Environment variables** (`fieldRef`, `resourceFieldRef`) are set at startup and are static
- **downwardAPI volumes** project metadata as files that update dynamically when labels/annotations change
- Labels and annotations as a whole are only available via volume projection, not environment variables
- Resource fields (CPU, memory) use `resourceFieldRef` with the container name specified

## Reference

- [Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/) — official concept documentation
- [Expose Pod Information via Environment Variables](https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/) — tutorial
- [Expose Pod Information via Files](https://kubernetes.io/docs/tasks/inject-data-application/downward-api-volume-expose-pod-information/) — volume tutorial

## Additional Resources

- [Kubernetes API Reference: EnvVarSource](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#environment-variables)
- [Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/) — combining multiple sources
- [Service Account Token Volume Projection](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection)
