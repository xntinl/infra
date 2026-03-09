# Cluster Component Debugging: API Server, Scheduler, Controller Manager

<!--
difficulty: advanced
concepts: [api-server, kube-scheduler, kube-controller-manager, static-pods, control-plane-debugging, log-analysis]
tools: [kubectl, crictl, journalctl, systemctl]
estimated_time: 40m
bloom_level: analyze
prerequisites: [01-kubeadm-cluster-setup, 12-static-pods-and-manifests]
-->

## Overview

When a Kubernetes cluster misbehaves, the root cause often lies in one of the core control plane components: the API server, scheduler, or controller manager. This exercise teaches you to systematically diagnose failures in each component by reading logs, inspecting manifests, and understanding component dependencies.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    Control Plane Node                         │
│                                                               │
│  ┌──────────────┐   ┌──────────────┐   ┌──────────────────┐ │
│  │ kube-apiserver│   │kube-scheduler│   │kube-controller-  │ │
│  │              │◄──│              │   │manager           │ │
│  │   :6443      │◄──│   :10259     │   │    :10257        │ │
│  └──────┬───────┘   └──────────────┘   └──────────────────┘ │
│         │                                                     │
│  ┌──────▼───────┐   ┌──────────────┐                         │
│  │    etcd       │   │   kubelet     │ (manages static pods)  │
│  │   :2379      │   │   :10250     │                         │
│  └──────────────┘   └──────────────┘                         │
└─────────────────────────────────────────────────────────────┘
```

Component dependencies:
- **API Server** depends on etcd. If etcd is down, the API server cannot read or write state.
- **Scheduler** depends on the API server. It watches for unscheduled pods via the API.
- **Controller Manager** depends on the API server. It runs reconciliation loops for deployments, replicasets, endpoints, etc.
- **kubelet** manages the static pods for all control plane components.

## Suggested Steps

### 1. API Server Debugging

```bash
# Check if the API server pod is running
sudo crictl ps | grep kube-apiserver

# View API server logs
sudo crictl logs $(sudo crictl ps -q --name kube-apiserver) --tail 50

# Alternative: view via kubelet journal (if kubectl is unavailable)
sudo journalctl -u kubelet | grep apiserver | tail -30

# Check the static pod manifest for misconfigurations
sudo cat /etc/kubernetes/manifests/kube-apiserver.yaml

# Common issues:
# - Wrong --etcd-servers endpoint
# - Invalid certificate paths
# - Port conflicts
# - Insufficient memory (OOMKilled)

# Verify API server is listening
sudo ss -tlnp | grep 6443
curl -k https://localhost:6443/healthz
```

### 2. Scheduler Debugging

```bash
# Check scheduler status
sudo crictl ps | grep kube-scheduler
sudo crictl logs $(sudo crictl ps -q --name kube-scheduler) --tail 50

# Symptoms of a broken scheduler:
# - Pods stuck in Pending with no events
# - No "Scheduled" events in kubectl describe pod output

# Check scheduler health endpoint
curl -k https://localhost:10259/healthz

# Test: create a pod and see if it gets scheduled
kubectl run sched-test --image=busybox:1.37 --command -- sleep 3600
kubectl get pod sched-test -o wide  # NODE column should be populated
kubectl describe pod sched-test | grep -A 5 Events
kubectl delete pod sched-test
```

### 3. Controller Manager Debugging

```bash
# Check controller manager status
sudo crictl ps | grep kube-controller-manager
sudo crictl logs $(sudo crictl ps -q --name kube-controller-manager) --tail 50

# Symptoms of a broken controller manager:
# - Deployments do not create ReplicaSets
# - ReplicaSets do not create Pods
# - Endpoints are not updated for Services
# - Nodes stay in NotReady state longer than expected

# Check health
curl -k https://localhost:10257/healthz

# Test: create a deployment and verify the ReplicaSet is created
kubectl create deployment cm-test --image=nginx:1.27
kubectl get rs -l app=cm-test  # should show a ReplicaSet
kubectl delete deployment cm-test
```

### 4. Systematic Debugging Approach

When the cluster is broken and `kubectl` does not work:

```bash
# Step 1: Check kubelet (it manages everything on the node)
sudo systemctl status kubelet
sudo journalctl -u kubelet --no-pager -n 50

# Step 2: Check container runtime
sudo systemctl status containerd
sudo crictl ps -a  # shows all containers, including stopped ones

# Step 3: Check static pod manifests
ls -la /etc/kubernetes/manifests/
# Are all four files present? (apiserver, controller-manager, scheduler, etcd)

# Step 4: Check each component's logs
for comp in kube-apiserver kube-controller-manager kube-scheduler etcd; do
  echo "=== $comp ==="
  sudo crictl logs $(sudo crictl ps -aq --name $comp 2>/dev/null | head -1) --tail 5 2>/dev/null || echo "NOT RUNNING"
done

# Step 5: Check certificates
sudo kubeadm certs check-expiration

# Step 6: Check network connectivity
sudo ss -tlnp | grep -E '6443|2379|10250|10259|10257'
```

### 5. Common Failure Scenarios and Fixes

| Symptom | Likely Cause | Fix |
|---------|-------------|-----|
| `kubectl` connection refused | API server not running | Check manifest, certs, etcd connectivity |
| Pods stuck in Pending | Scheduler not running or misconfigured | Check scheduler logs and manifest |
| Deployments not scaling | Controller manager down | Check CM logs and leader election |
| Nodes NotReady | kubelet issues | Check kubelet journal, container runtime |
| API server CrashLoopBackOff | etcd unreachable or cert issues | Check etcd, verify cert paths |

## Verify

```bash
# All control plane components are Running
kubectl get pods -n kube-system -l tier=control-plane

# Health endpoints respond
curl -k https://localhost:6443/healthz    # API server
curl -k https://localhost:10259/healthz   # Scheduler
curl -k https://localhost:10257/healthz   # Controller Manager

# End-to-end: deploy, scale, and expose
kubectl create deployment debug-verify --image=nginx:1.27
kubectl scale deployment debug-verify --replicas=3
kubectl expose deployment debug-verify --port=80
kubectl get all -l app=debug-verify
kubectl delete deployment debug-verify
kubectl delete svc debug-verify
```

## Cleanup

Delete any test resources created during debugging.

```bash
kubectl delete pod sched-test --ignore-not-found
kubectl delete deployment cm-test --ignore-not-found
kubectl delete deployment debug-verify --ignore-not-found
kubectl delete svc debug-verify --ignore-not-found
```

## Reference

- [Troubleshooting Clusters](https://kubernetes.io/docs/tasks/debug/debug-cluster/)
- [Debugging Kubernetes Nodes With crictl](https://kubernetes.io/docs/tasks/debug/debug-cluster/crictl/)
- [Control Plane Components](https://kubernetes.io/docs/concepts/overview/components/#control-plane-components)
