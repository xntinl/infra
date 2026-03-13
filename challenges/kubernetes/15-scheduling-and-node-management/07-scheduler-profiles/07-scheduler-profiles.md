<!--
difficulty: intermediate
concepts: [scheduler-profiles, scheduler-plugins, scoring, filtering, kubescheduler-config, scheduling-framework]
tools: [kubectl, kubeadm]
estimated_time: 35m
bloom_level: apply
prerequisites: [node-affinity-and-taints, topology-spread-constraints, pod-affinity-anti-affinity]
-->

# 15.07 - Scheduler Profiles and Plugins

## Why This Matters

The default scheduler works well for most workloads, but you may need to customize its behavior. Maybe you want to disable the inter-pod affinity plugin for performance, or configure different scoring weights for different workload types. Scheduler profiles let you define multiple scheduling behaviors within a single scheduler process, each selectable by name in the pod spec.

## What You Will Learn

- How the scheduling framework is organized into extension points (Filter, Score, Reserve, Bind)
- How to configure scheduler profiles in `KubeSchedulerConfiguration`
- How plugins can be enabled, disabled, or reordered per profile
- How pods select a scheduler profile via `spec.schedulerName`

## Guide

### 1. KubeSchedulerConfiguration with Two Profiles

```yaml
# scheduler-config.yaml
apiVersion: kubescheduler.config.k8s.io/v1
kind: KubeSchedulerConfiguration
profiles:
  - schedulerName: default-scheduler    # the default profile
    plugins:
      score:
        enabled:
          - name: NodeResourcesFit
            weight: 2                   # double the weight of resource fitting
          - name: InterPodAffinity
            weight: 1
          - name: NodeAffinity
            weight: 1
        disabled:
          - name: ImageLocality         # disable image locality scoring
  - schedulerName: batch-scheduler      # profile optimized for batch workloads
    plugins:
      score:
        enabled:
          - name: NodeResourcesFit
            weight: 5                   # heavily favor resource packing
        disabled:
          - name: InterPodAffinity      # skip pod affinity for performance
          - name: NodeAffinity
          - name: ImageLocality
      filter:
        disabled:
          - name: InterPodAffinity      # skip pod affinity filtering too
    pluginConfig:
      - name: NodeResourcesFit
        args:
          scoringStrategy:
            type: MostAllocated         # pack pods onto busy nodes (bin packing)
```

### 2. Pod Using the Default Profile

```yaml
# pod-default-scheduler.yaml
apiVersion: v1
kind: Pod
metadata:
  name: default-scheduled-pod
spec:
  # schedulerName is omitted, so it uses "default-scheduler"
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 3. Pod Using the Batch Profile

```yaml
# pod-batch-scheduler.yaml
apiVersion: v1
kind: Pod
metadata:
  name: batch-scheduled-pod
spec:
  schedulerName: batch-scheduler       # explicitly select the batch profile
  containers:
    - name: busybox
      image: busybox:1.37
      command: ["sleep", "3600"]
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

### 4. Viewing Available Plugins

The scheduler framework has these extension points (in order):

| Extension Point | Purpose |
|----------------|---------|
| PreFilter | Validate pod before filtering |
| Filter | Eliminate ineligible nodes |
| PostFilter | Handle unschedulable pods |
| PreScore | Pre-process information for scoring |
| Score | Rank eligible nodes |
| Reserve | Reserve resources on the chosen node |
| Permit | Approve/deny/wait before binding |
| PreBind | Pre-binding actions (e.g., volume attachment) |
| Bind | Bind pod to node |
| PostBind | Post-binding cleanup |

### 5. Applying the Configuration

```bash
# For kubeadm clusters, update the scheduler manifest
# /etc/kubernetes/manifests/kube-scheduler.yaml
# Add --config=/etc/kubernetes/scheduler-config.yaml
# Mount the config file into the static pod

# For managed clusters (EKS, GKE, AKS), scheduler profiles
# are configured through the cloud provider's API
```

### Apply

```bash
kubectl apply -f pod-default-scheduler.yaml
kubectl apply -f pod-batch-scheduler.yaml
```

## TODO

The following scheduler configuration has an issue. Fix it so that the `high-availability` profile spreads pods as evenly as possible.

```yaml
profiles:
  - schedulerName: high-availability
    pluginConfig:
      - name: NodeResourcesFit
        args:
          scoringStrategy:
            type: MostAllocated
```

<details>
<summary>Show Answer</summary>

`MostAllocated` packs pods onto the most utilized nodes, which is the opposite of spreading for high availability. Change `type: MostAllocated` to `type: LeastAllocated` to prefer nodes with the most available resources, spreading workloads across the cluster.

</details>

## Verify

```bash
# 1. Check which scheduler handled the pod
kubectl get pod default-scheduled-pod -o jsonpath='{.spec.schedulerName}'
kubectl get pod batch-scheduled-pod -o jsonpath='{.spec.schedulerName}'

# 2. Verify both pods are Running
kubectl get pods default-scheduled-pod batch-scheduled-pod

# 3. Check scheduler events
kubectl get events --field-selector reason=Scheduled

# 4. If a pod uses a nonexistent scheduler name, it stays Pending
kubectl run orphan --image=nginx:1.27 --overrides='{"spec":{"schedulerName":"nonexistent"}}'
kubectl get pod orphan
kubectl delete pod orphan
```

## Cleanup

```bash
kubectl delete pod default-scheduled-pod batch-scheduled-pod
```

## What's Next

Scheduler profiles let you customize the built-in scheduler. The next exercise goes further by deploying a completely separate custom scheduler alongside the default: [15.08 - Custom Schedulers: Deploying a Second Scheduler](../08-custom-schedulers/08-custom-schedulers.md).

## Summary

- Scheduler profiles define named configurations within a single scheduler binary
- Pods select a profile via `spec.schedulerName` (defaults to `default-scheduler`)
- Plugins can be enabled, disabled, or have their weights adjusted per profile
- `MostAllocated` scoring packs pods (bin packing); `LeastAllocated` spreads pods
- Extension points define the scheduling pipeline: Filter, Score, Reserve, Bind
