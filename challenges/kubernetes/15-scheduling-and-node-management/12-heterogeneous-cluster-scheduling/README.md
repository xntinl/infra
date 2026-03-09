<!--
difficulty: insane
concepts: [heterogeneous-cluster, multi-architecture, numa-topology, device-plugins, runtime-classes, scheduling-platform]
tools: [kubectl, kubelet, device-plugins]
estimated_time: 75m
bloom_level: create
prerequisites: [advanced-scheduling-optimization, custom-schedulers, kubelet-configuration, node-problem-detector, karpenter]
-->

# 15.12 - Heterogeneous Cluster Scheduling Platform

## Scenario

Your organization is building a next-generation AI/ML platform on Kubernetes. The cluster consists of radically different hardware:

- **x86 compute nodes** -- Intel Xeon, 32 CPU, 128Gi RAM, standard NVMe disks
- **ARM compute nodes** -- AWS Graviton3 (arm64), 16 CPU, 64Gi RAM, standard NVMe disks
- **GPU training nodes** -- 8 CPU, 64Gi RAM, 4x NVIDIA A100 GPUs, NUMA topology matters
- **FPGA inference nodes** -- 16 CPU, 32Gi RAM, 2x Xilinx Alveo FPGAs, custom device plugin

Each hardware class requires different container images (multi-arch builds), different scheduling strategies, and different resource management. Your platform must automatically route workloads to the correct hardware, handle device plugin registration, manage NUMA-aware scheduling for GPU nodes, and provide graceful degradation when specialized hardware is unavailable.

## Constraints

1. Create four node pools with appropriate labels: `hardware=x86-compute`, `hardware=arm-compute`, `hardware=gpu-training`, `hardware=fpga-inference`
2. GPU nodes must be tainted with `nvidia.com/gpu=present:NoSchedule` and have Topology Manager set to `single-numa-node`
3. FPGA nodes must be tainted with `fpga=xilinx:NoSchedule` and expose `xilinx.com/fpga` as an extended resource
4. Deploy a multi-architecture workload (nginx:1.27 supports amd64 and arm64) that runs on both x86 and ARM nodes with topology spread across architectures
5. Deploy a GPU training job that requests `nvidia.com/gpu: 2` and is NUMA-aligned (single NUMA node)
6. Deploy an FPGA inference service that requests `xilinx.com/fpga: 1` with pod anti-affinity to spread across FPGA nodes
7. Implement a **fallback scheduler profile** called `flexible-scheduler` that uses `LeastAllocated` scoring and disables `InterPodAffinity` for batch workloads that should go wherever capacity exists
8. Create RuntimeClasses for workloads needing different container runtimes: `standard` (runc), `gpu` (nvidia-container-runtime)
9. Implement node auto-repair: NPD monitors all nodes, and nodes with `ReadonlyFilesystem` or `KernelDeadlock` conditions are automatically cordoned
10. All workloads must define resource requests and limits; GPU requests must not exceed 4 per pod; FPGA requests must not exceed 2 per pod
11. PriorityClasses: `gpu-training` (900000), `fpga-inference` (800000), `compute` (500000), `batch` (100000, preemptionPolicy: Never)
12. ResourceQuotas per team namespace: `ml-team` gets 16 GPUs and 256Gi memory; `inference-team` gets 4 FPGAs and 64Gi memory; `platform-team` gets 64 CPU and 256Gi memory (no GPU/FPGA)

## Success Criteria

1. Multi-arch workload has pods on both amd64 and arm64 nodes (verify with `kubectl get pods -o wide` and node arch labels)
2. GPU training pods are bound to GPU nodes with exactly 2 GPUs allocated per pod (verify via `kubectl describe pod`)
3. FPGA inference pods run only on FPGA nodes with 1 FPGA each (verify via extended resource allocation)
4. The `flexible-scheduler` profile successfully schedules batch pods even when affinity constraints would block the default scheduler
5. RuntimeClass `gpu` is used by GPU training pods (verify via `kubectl get pod -o jsonpath='{.spec.runtimeClassName}'`)
6. NPD reports node conditions correctly on all node types
7. ResourceQuota prevents `ml-team` from requesting more than 16 GPUs
8. PriorityClass preemption evicts `batch` workloads to make room for `gpu-training` or `fpga-inference` when resources are scarce
9. Topology Manager ensures GPU pods land on a single NUMA node (verify via kubelet logs)
10. When GPU/FPGA nodes are unavailable, workloads stay Pending with clear events (no silent misplacement on wrong hardware)

## Verification Commands

```bash
# Multi-arch distribution
kubectl get pods -n platform -l app=multi-arch -o wide
kubectl get pods -n platform -l app=multi-arch -o jsonpath='{range .items[*]}{.spec.nodeName}{"\n"}{end}' | while read node; do kubectl get node "$node" -o jsonpath='{.metadata.labels.kubernetes\.io/arch}'; echo; done | sort | uniq -c

# GPU training verification
kubectl describe pod -n ml-team -l app=gpu-training | grep -A5 "nvidia.com/gpu"
kubectl get pods -n ml-team -l app=gpu-training -o wide

# FPGA inference verification
kubectl describe pod -n inference-team -l app=fpga-inference | grep -A5 "xilinx.com/fpga"
kubectl get pods -n inference-team -l app=fpga-inference -o wide

# RuntimeClass verification
kubectl get pods -n ml-team -l app=gpu-training -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.runtimeClassName}{"\n"}{end}'

# Scheduler profile verification
kubectl get pods -n platform -l tier=batch -o jsonpath='{range .items[*]}{.metadata.name}{"\t"}{.spec.schedulerName}{"\n"}{end}'

# ResourceQuota enforcement
kubectl describe resourcequota -n ml-team
kubectl describe resourcequota -n inference-team
kubectl describe resourcequota -n platform-team

# NPD conditions on all node types
for node in $(kubectl get nodes -o name); do
  echo "=== $node ==="
  kubectl describe "$node" | grep -A5 "KernelDeadlock\|ReadonlyFilesystem\|DiskProblem"
done

# PriorityClasses
kubectl get priorityclass

# No misplaced pods (batch on GPU, compute on FPGA, etc.)
kubectl get pods -A -o wide | grep -E "Pending|Error"

# Topology Manager (check kubelet logs on GPU nodes)
# ssh to GPU node: journalctl -u kubelet | grep "topology"
```

## Cleanup

```bash
kubectl delete namespace ml-team inference-team platform-team platform
kubectl delete priorityclass gpu-training fpga-inference compute batch
kubectl delete runtimeclass standard gpu
# Remove taints
kubectl taint nodes -l hardware=gpu-training nvidia.com/gpu=present:NoSchedule-
kubectl taint nodes -l hardware=fpga-inference fpga=xilinx:NoSchedule-
# Remove labels
kubectl label nodes --all hardware-
```
