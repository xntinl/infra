# Static Pods and Manifest Management

<!--
difficulty: advanced
concepts: [static-pods, kubelet, manifests, control-plane, mirror-pods]
tools: [kubectl, systemctl, crictl]
estimated_time: 30m
bloom_level: analyze
prerequisites: [01-kubeadm-cluster-setup, 11-kubelet-troubleshooting]
-->

## Overview

Static pods are managed directly by the kubelet on a specific node, without the API server's involvement. The kubelet watches a manifest directory (typically `/etc/kubernetes/manifests/`) and ensures the defined pods are running. The Kubernetes control plane itself (API server, controller manager, scheduler, etcd) runs as static pods on kubeadm clusters.

## Architecture

```
┌──────────────────────────────────────────────────┐
│                     Node                          │
│                                                    │
│  kubelet                                           │
│    │                                               │
│    ├── watches /etc/kubernetes/manifests/          │
│    │   ├── kube-apiserver.yaml    ─► static pod    │
│    │   ├── kube-controller-manager.yaml            │
│    │   ├── kube-scheduler.yaml                     │
│    │   ├── etcd.yaml                               │
│    │   └── custom-static.yaml     ─► your pod      │
│    │                                               │
│    └── creates "mirror pods" in API server         │
│        (read-only representations visible via      │
│         kubectl, but cannot be deleted via API)    │
└──────────────────────────────────────────────────┘
```

Key characteristics of static pods:
- Managed by kubelet, not the API server
- Always run on the node where the manifest exists
- Cannot be managed by Deployments or ReplicaSets
- Mirror pods appear in `kubectl get pods` with a node-name suffix
- Deleting a mirror pod via kubectl has no effect -- kubelet recreates it

## Suggested Steps

### 1. Examine Existing Static Pods

```bash
# List static pod manifests
ls -la /etc/kubernetes/manifests/

# Examine the API server static pod
sudo cat /etc/kubernetes/manifests/kube-apiserver.yaml

# View mirror pods in the API server (note the node-name suffix)
kubectl get pods -n kube-system -l tier=control-plane
kubectl get pods -n kube-system -o wide | grep -E "apiserver|scheduler|controller|etcd"

# Check the kubelet's staticPodPath configuration
sudo cat /var/lib/kubelet/config.yaml | grep staticPodPath
```

### 2. Create a Custom Static Pod

```bash
# Create a static pod manifest
sudo tee /etc/kubernetes/manifests/static-nginx.yaml <<'EOF'
apiVersion: v1
kind: Pod
metadata:
  name: static-nginx
  namespace: default
  labels:
    role: static-web
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      ports:
        - containerPort: 80
      resources:
        requests:
          cpu: 50m
          memory: 64Mi
        limits:
          cpu: 100m
          memory: 128Mi
EOF

# Wait for kubelet to detect and start the pod
sleep 10

# The mirror pod should appear with a node-name suffix
kubectl get pods -l role=static-web
```

### 3. Understand Mirror Pod Behavior

```bash
# Try to delete the mirror pod -- kubelet will recreate it
kubectl delete pod static-nginx-$(hostname)
sleep 5
kubectl get pods -l role=static-web  # it reappears

# Modify the static pod manifest to change the image
sudo sed -i 's/nginx:1.27/nginx:1.26/' /etc/kubernetes/manifests/static-nginx.yaml
sleep 10
kubectl get pod static-nginx-$(hostname) -o jsonpath='{.spec.containers[0].image}'
# Should show nginx:1.26

# The ONLY way to delete a static pod is to remove its manifest
sudo rm /etc/kubernetes/manifests/static-nginx.yaml
sleep 10
kubectl get pods -l role=static-web  # gone
```

### 4. Troubleshoot Control Plane Static Pods

```bash
# If a control plane component is not starting, check:

# 1. Manifest syntax errors
sudo python3 -c "import yaml; yaml.safe_load(open('/etc/kubernetes/manifests/kube-apiserver.yaml'))"

# 2. Image pull issues
sudo crictl images | grep kube-apiserver

# 3. Container status
sudo crictl ps -a | grep kube-apiserver

# 4. Container logs
sudo crictl logs $(sudo crictl ps -aq --name kube-apiserver | head -1) 2>&1 | tail -20

# 5. Common issue: accidentally editing a manifest with syntax errors
# Kubelet will fail to parse it and the component stops
```

### 5. Change the Static Pod Path

```bash
# The staticPodPath can be changed in kubelet configuration
# Default: /etc/kubernetes/manifests

# To use a different path:
# 1. Edit /var/lib/kubelet/config.yaml
#    staticPodPath: /etc/kubernetes/custom-manifests
#
# 2. Move manifests to the new directory
#
# 3. Restart kubelet
# sudo systemctl restart kubelet
#
# WARNING: Do not do this on a production control plane without careful planning
```

## Verify

```bash
# Control plane static pods are running
kubectl get pods -n kube-system -l tier=control-plane

# Custom static pod was created and deleted successfully
kubectl get pods -l role=static-web  # should show no resources

# kubelet is healthy
sudo systemctl is-active kubelet
```

## Cleanup

```bash
# Remove any custom static pod manifests
sudo rm -f /etc/kubernetes/manifests/static-nginx.yaml
```

## Reference

- [Static Pods](https://kubernetes.io/docs/tasks/configure-pod-container/static-pod/)
- [Create Static Pods](https://kubernetes.io/docs/concepts/workloads/pods/#static-pods)
- [kubeadm Implementation Details](https://kubernetes.io/docs/reference/setup-tools/kubeadm/implementation-details/)
