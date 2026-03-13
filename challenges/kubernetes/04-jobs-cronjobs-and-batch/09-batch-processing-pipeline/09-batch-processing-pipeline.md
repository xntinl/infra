# 9. Building a Batch Processing Pipeline

<!--
difficulty: advanced
concepts: [multi-stage-pipeline, init-containers, configmap-coordination, job-chaining, batch-architecture]
tools: [kubectl, minikube]
estimated_time: 45m
bloom_level: analyze
prerequisites: [04-02, 04-04, 04-05]
-->

## Prerequisites

- A running Kubernetes cluster with a default StorageClass
- `kubectl` installed and configured
- Completion of exercises [02](../02-parallel-jobs-completions/02-parallel-jobs-completions.md), [04](../04-indexed-jobs/04-indexed-jobs.md), and [05](../05-job-failure-handling/05-job-failure-handling.md)

## Learning Objectives

- **Analyze** patterns for chaining batch Jobs into multi-stage pipelines
- **Evaluate** coordination strategies: shared PVC, ConfigMap signals, and init container waits
- **Create** a 3-stage batch pipeline with data handoff between stages

## Architecture

A batch processing pipeline consists of sequential stages where each stage's output is the next stage's input:

```
Stage 1: Extract     Stage 2: Transform     Stage 3: Load
(Indexed Job)   -->  (Parallel Job)    -->  (Single Job)
Write to PVC         Read/write PVC         Read from PVC
```

Kubernetes has no built-in Job orchestration (unlike Argo Workflows or Tekton). You must coordinate stages using one of these patterns:

1. **Shared PVC**: All stages mount the same PVC. Use init containers or scripts that poll for predecessor output files.
2. **Manual chaining**: Use `kubectl wait --for=condition=complete` between Job submissions.
3. **ConfigMap signals**: Stage N writes a ConfigMap when done; Stage N+1's init container polls for it.

## The Challenge

Build a 3-stage ETL pipeline:

1. **Extract stage**: An Indexed Job with 3 completions writes data files (`data-0.txt`, `data-1.txt`, `data-2.txt`) to a shared PVC at `/pipeline/raw/`
2. **Transform stage**: A parallel Job reads from `/pipeline/raw/`, processes each file, writes results to `/pipeline/processed/`
3. **Load stage**: A single Job reads all files from `/pipeline/processed/`, aggregates them, and writes a summary to `/pipeline/output/summary.txt`

Requirements:
- All stages share a single PVC (5Gi, ReadWriteOnce)
- The transform stage uses an init container that polls for the existence of all 3 raw data files before starting
- The load stage waits for all processed files using a similar init container
- Each stage has appropriate `backoffLimit` and `activeDeadlineSeconds`
- The extract stage uses Indexed completion mode

<details>
<summary>Hint 1: Shared PVC</summary>

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pipeline-data
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 5Gi
```

Mount in each Job's pod template:

```yaml
volumeMounts:
  - name: pipeline
    mountPath: /pipeline
volumes:
  - name: pipeline
    persistentVolumeClaim:
      claimName: pipeline-data
```

</details>

<details>
<summary>Hint 2: Init container polling for predecessor</summary>

```yaml
initContainers:
  - name: wait-for-extract
    image: busybox:1.37
    command:
      - sh
      - -c
      - |
        echo "Waiting for extract stage..."
        until [ -f /pipeline/raw/data-0.txt ] && \
              [ -f /pipeline/raw/data-1.txt ] && \
              [ -f /pipeline/raw/data-2.txt ]; do
          sleep 2
        done
        echo "All input files ready"
    volumeMounts:
      - name: pipeline
        mountPath: /pipeline
```

</details>

<details>
<summary>Hint 3: Manual job chaining</summary>

```bash
kubectl apply -f extract-job.yaml
kubectl wait --for=condition=complete job/extract --timeout=120s

kubectl apply -f transform-job.yaml
kubectl wait --for=condition=complete job/transform --timeout=120s

kubectl apply -f load-job.yaml
kubectl wait --for=condition=complete job/load --timeout=120s
```

</details>

## Verify What You Learned

```bash
# All three Jobs completed
kubectl get jobs -l pipeline=etl
# Expected: 3 Jobs, all Complete

# Final output exists
kubectl run verify --image=busybox:1.37 --rm -it --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"pipeline","persistentVolumeClaim":{"claimName":"pipeline-data"}}],"containers":[{"name":"verify","image":"busybox:1.37","command":["cat","/pipeline/output/summary.txt"],"volumeMounts":[{"name":"pipeline","mountPath":"/pipeline"}]}]}}' \
  -- cat /pipeline/output/summary.txt

# Raw and processed files exist
kubectl run ls-check --image=busybox:1.37 --rm -it --restart=Never \
  --overrides='{"spec":{"volumes":[{"name":"pipeline","persistentVolumeClaim":{"claimName":"pipeline-data"}}],"containers":[{"name":"ls","image":"busybox:1.37","command":["sh","-c","ls /pipeline/raw/ /pipeline/processed/ /pipeline/output/"],"volumeMounts":[{"name":"pipeline","mountPath":"/pipeline"}]}]}}'
```

## Cleanup

```bash
kubectl delete job -l pipeline=etl
kubectl delete pvc pipeline-data
```

## What's Next

This pipeline runs on a single node due to ReadWriteOnce PVC constraints. In [exercise 10 (Distributed Batch Processing with Work Queues)](../10-distributed-batch-system/10-distributed-batch-system.md), you will build a distributed batch system using message-based coordination instead of shared storage.

## Summary

- Kubernetes has no built-in Job orchestration; coordination requires shared PVCs, polling, or external tools
- Init containers can poll for predecessor output files to create stage dependencies
- `kubectl wait --for=condition=complete` enables scripted Job chaining
- Indexed Jobs map naturally to fan-out extract stages
- Shared PVCs with ReadWriteOnce limit the pipeline to a single node
- Always set `backoffLimit` and `activeDeadlineSeconds` on each stage for resilience

## Reference

- [Indexed Jobs](https://kubernetes.io/docs/concepts/workloads/controllers/job/#completion-mode)
- [Init Containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/)
- [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/)
