# 4. Partition Rolling Updates

<!--
difficulty: intermediate
concepts: [statefulset-update-strategy, partition, rolling-update, canary-statefulset]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: apply
prerequisites: [03-01, 03-03]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of exercises [01](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md) and [03](../03-statefulset-ordered-vs-parallel/03-statefulset-ordered-vs-parallel.md)

## Learning Objectives

- **Understand** how StatefulSet `RollingUpdate` differs from Deployment rolling updates
- **Apply** the `partition` parameter to stage updates across pod subsets
- **Analyze** how partition-based canary deployments work for stateful workloads

## Why Partition Rolling Updates?

Deployment rolling updates replace pods at random. StatefulSet rolling updates are ordinal-aware: they update pods from the highest ordinal down to the partition value. By setting `partition: 2` on a 3-replica StatefulSet, only pod-2 gets the update while pods 0 and 1 remain on the old version. This is a built-in canary mechanism for stateful workloads where you need to verify the update on one replica before committing.

## Step 1: Deploy a StatefulSet

```yaml
# partition-demo.yaml
apiVersion: v1
kind: Service
metadata:
  name: partition-svc
spec:
  clusterIP: None
  selector:
    app: partition-demo
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: partition-demo
spec:
  serviceName: partition-svc
  replicas: 3
  updateStrategy:
    type: RollingUpdate
    rollingUpdate:
      partition: 0            # Start with no partition — all pods update
  selector:
    matchLabels:
      app: partition-demo
  template:
    metadata:
      labels:
        app: partition-demo
        version: v1           # Track the current version
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          env:
            - name: VERSION
              value: "v1"
```

```bash
kubectl apply -f partition-demo.yaml
kubectl rollout status statefulset/partition-demo
```

Verify all pods are running v1:

```bash
kubectl get pods -l app=partition-demo -o custom-columns=NAME:.metadata.name,IMAGE:.spec.containers[0].image,VERSION:.spec.containers[0].env[0].value
```

## Step 2: Set Partition and Update

Set the partition to 2, meaning only pods with ordinal >= 2 will receive updates:

```bash
kubectl patch statefulset partition-demo -p '{"spec":{"updateStrategy":{"rollingUpdate":{"partition":2}}}}'
```

Now update the pod template (change the VERSION env var to simulate a new release):

```bash
kubectl patch statefulset partition-demo --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/env/0/value","value":"v2"},{"op":"replace","path":"/spec/template/metadata/labels/version","value":"v2"}]'
```

Watch the rollout:

```bash
kubectl get pods -l app=partition-demo -w
```

Expected behavior: Only `partition-demo-2` restarts with the new template. Pods 0 and 1 remain unchanged.

Verify:

```bash
kubectl get pods -l app=partition-demo -o custom-columns=NAME:.metadata.name,VERSION:.spec.containers[0].env[0].value
```

Expected output:

```
NAME                VERSION
partition-demo-0    v1
partition-demo-1    v1
partition-demo-2    v2
```

## Step 3: Lower the Partition to Roll Out Further

After verifying pod-2 is healthy, lower the partition to 1:

```bash
kubectl patch statefulset partition-demo -p '{"spec":{"updateStrategy":{"rollingUpdate":{"partition":1}}}}'
kubectl get pods -l app=partition-demo -w
```

Now pod-1 updates to v2. Pod-0 still runs v1.

Lower to 0 to complete the rollout:

```bash
kubectl patch statefulset partition-demo -p '{"spec":{"updateStrategy":{"rollingUpdate":{"partition":0}}}}'
kubectl get pods -l app=partition-demo -w
```

Verify all pods now run v2:

```bash
kubectl get pods -l app=partition-demo -o custom-columns=NAME:.metadata.name,VERSION:.spec.containers[0].env[0].value
```

Expected output:

```
NAME                VERSION
partition-demo-0    v2
partition-demo-1    v2
partition-demo-2    v2
```

## Step 4: Understand Rollback

If the canary pod (pod-2) had shown problems, you could roll back by reverting the pod template. Pods below the partition value are never touched, so they act as a stable baseline.

```bash
# To roll back, revert the template:
kubectl patch statefulset partition-demo --type='json' -p='[{"op":"replace","path":"/spec/template/spec/containers/0/env/0/value","value":"v1"}]'
# Only pods >= partition would revert
```

## Verify What You Learned

```bash
# All pods should be on v2 now
kubectl get pods -l app=partition-demo -o jsonpath='{range .items[*]}{.metadata.name}: {.spec.containers[0].env[0].value}{"\n"}{end}'

# Partition should be 0
kubectl get statefulset partition-demo -o jsonpath='{.spec.updateStrategy.rollingUpdate.partition}'
# Expected: 0
```

## Cleanup

```bash
kubectl delete statefulset partition-demo
kubectl delete svc partition-svc
```

## What's Next

You have explored StatefulSet update strategies. In [exercise 05 (DaemonSets with Tolerations and Node Selection)](../05-daemonsets-with-tolerations/05-daemonsets-with-tolerations.md), you will shift to DaemonSets and learn how they ensure one pod per node.

## Summary

- StatefulSet `RollingUpdate` updates pods from highest ordinal down to the `partition` value
- Setting `partition: N` means only pods with ordinal >= N receive the update
- This provides a built-in canary mechanism for stateful workloads
- Pods below the partition are never modified, acting as a stable baseline
- Lower the partition progressively to roll out to more pods after verification

## Reference

- [StatefulSet Update Strategies](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#update-strategies)
- [Partitioned Rolling Updates](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#partitions)
