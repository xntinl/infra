# Cluster Networking Debugging: Pod-to-Pod, Service, DNS

<!--
difficulty: advanced
concepts: [cni, pod-networking, service-networking, dns, coredns, kube-proxy, iptables, network-troubleshooting]
tools: [kubectl, crictl, iptables, nslookup, dig, tcpdump, ss]
estimated_time: 45m
bloom_level: analyze
prerequisites: [01-kubeadm-cluster-setup, services, networking-basics]
-->

## Overview

Kubernetes networking has multiple layers: pod-to-pod communication (CNI), service abstraction (kube-proxy/iptables), and DNS resolution (CoreDNS). When connectivity breaks, you need to isolate the layer where the failure occurs. This exercise provides a systematic approach to debugging networking issues at each layer.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│                   Kubernetes Networking Layers                    │
│                                                                   │
│  Layer 3: DNS Resolution                                         │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  CoreDNS (kube-system)                                    │   │
│  │  service-name.namespace.svc.cluster.local → ClusterIP     │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                   │
│  Layer 2: Service Networking                                     │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  kube-proxy → iptables/IPVS rules                         │   │
│  │  ClusterIP:port → Pod endpoints (round-robin)             │   │
│  └──────────────────────────────────────────────────────────┘   │
│                                                                   │
│  Layer 1: Pod Networking (CNI)                                   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │  Pod IP ←→ Pod IP (flat network, no NAT)                  │   │
│  │  Managed by CNI plugin (Calico, Flannel, Cilium, etc.)    │   │
│  └──────────────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────┘
```

## Suggested Steps

### 1. Set Up Test Environment

```bash
kubectl create namespace net-debug

# Deploy server pods on different nodes
kubectl apply -n net-debug -f - <<'EOF'
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-server
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web-server
  template:
    metadata:
      labels:
        app: web-server
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: Service
metadata:
  name: web-svc
spec:
  selector:
    app: web-server
  ports:
    - port: 80
      targetPort: 80
---
apiVersion: v1
kind: Pod
metadata:
  name: debug-pod
  labels:
    app: debug
spec:
  containers:
    - name: debug
      image: busybox:1.37
      command: ["sleep", "3600"]
EOF

kubectl wait -n net-debug --for=condition=ready pod -l app=web-server --timeout=60s
kubectl wait -n net-debug --for=condition=ready pod/debug-pod --timeout=60s
```

### 2. Debug Pod-to-Pod Connectivity (Layer 1)

```bash
# Get pod IPs
kubectl get pods -n net-debug -o wide

# Test direct pod-to-pod connectivity
SERVER_IP=$(kubectl get pod -n net-debug -l app=web-server -o jsonpath='{.items[0].status.podIP}')
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=5 http://$SERVER_IP

# If this fails, the CNI plugin is likely misconfigured
# Check CNI pods
kubectl get pods -n kube-system -l k8s-app=calico-node  # or flannel, cilium
kubectl logs -n kube-system -l k8s-app=calico-node --tail=20

# Check node-level networking
# On the node, verify the CNI configuration
ls /etc/cni/net.d/
cat /etc/cni/net.d/*.conflist

# Check pod interfaces
kubectl exec -n net-debug debug-pod -- ip addr
kubectl exec -n net-debug debug-pod -- ip route
```

### 3. Debug Service Connectivity (Layer 2)

```bash
# Test service via ClusterIP
SVC_IP=$(kubectl get svc web-svc -n net-debug -o jsonpath='{.spec.clusterIP}')
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=5 http://$SVC_IP

# If this fails but pod-to-pod works, check kube-proxy
kubectl get pods -n kube-system -l k8s-app=kube-proxy
kubectl logs -n kube-system -l k8s-app=kube-proxy --tail=20

# Check iptables rules for the service
sudo iptables -t nat -L KUBE-SERVICES | grep web-svc

# Verify endpoints exist
kubectl get endpoints web-svc -n net-debug
# If endpoints are empty, the selector does not match any pods

# Check kube-proxy mode
kubectl get configmap kube-proxy -n kube-system -o yaml | grep mode
```

### 4. Debug DNS Resolution (Layer 3)

```bash
# Test DNS resolution
kubectl exec -n net-debug debug-pod -- nslookup web-svc.net-debug.svc.cluster.local

# If DNS fails, check CoreDNS
kubectl get pods -n kube-system -l k8s-app=kube-dns
kubectl logs -n kube-system -l k8s-app=kube-dns --tail=20

# Check CoreDNS ConfigMap
kubectl get configmap coredns -n kube-system -o yaml

# Check pod DNS configuration
kubectl exec -n net-debug debug-pod -- cat /etc/resolv.conf
# nameserver should point to the kube-dns service IP (usually 10.96.0.10)

# Test external DNS
kubectl exec -n net-debug debug-pod -- nslookup google.com
```

### 5. Systematic Debugging Checklist

```bash
# 1. Can pods reach their own loopback?
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=2 http://127.0.0.1 || echo "FAIL"

# 2. Can pods reach other pods by IP? (tests CNI)
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=2 http://$SERVER_IP || echo "FAIL"

# 3. Can pods reach services by ClusterIP? (tests kube-proxy)
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=2 http://$SVC_IP || echo "FAIL"

# 4. Can pods resolve service DNS? (tests CoreDNS)
kubectl exec -n net-debug debug-pod -- nslookup web-svc.net-debug.svc.cluster.local || echo "FAIL"

# 5. Can pods reach external IPs? (tests node networking/NAT)
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=2 http://1.1.1.1 || echo "FAIL"

# 6. Can pods resolve external DNS? (tests CoreDNS upstream)
kubectl exec -n net-debug debug-pod -- nslookup google.com || echo "FAIL"
```

## Verify

```bash
# All connectivity tests pass
kubectl exec -n net-debug debug-pod -- wget -qO- --timeout=5 http://web-svc.net-debug.svc.cluster.local
# Should return the nginx welcome page HTML

# DNS resolution works
kubectl exec -n net-debug debug-pod -- nslookup kubernetes.default.svc.cluster.local
```

## Cleanup

```bash
kubectl delete namespace net-debug
```

## Reference

- [Debugging DNS Resolution](https://kubernetes.io/docs/tasks/administer-cluster/dns-debugging-resolution/)
- [Debug Services](https://kubernetes.io/docs/tasks/debug/debug-application/debug-service/)
- [Cluster Networking](https://kubernetes.io/docs/concepts/cluster-administration/networking/)
