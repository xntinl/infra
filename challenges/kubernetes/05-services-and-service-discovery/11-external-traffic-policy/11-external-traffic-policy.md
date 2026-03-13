# 11. External and Internal Traffic Policy

<!--
difficulty: advanced
concepts: [external-traffic-policy, internal-traffic-policy, source-ip-preservation, local-routing]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [05-03, 05-10]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 03](../03-nodeport-services/03-nodeport-services.md) and [exercise 10](../10-session-affinity-and-traffic-policy/10-session-affinity-and-traffic-policy.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how `externalTrafficPolicy` affects source IP preservation and load distribution
- **Configure** `internalTrafficPolicy: Local` to restrict internal traffic to node-local pods
- **Evaluate** the tradeoffs between `Cluster` and `Local` policies for different use cases

## Architecture

```
External Client (203.0.113.10)
         │
    ┌────┴────┐          ┌──────────┐
    │  Node A  │          │  Node B  │
    │ (has pod)│          │ (no pod) │
    └────┬────┘          └──────────┘
         │
  externalTrafficPolicy:
  ┌──────┴──────────────────────────┐
  │                                 │
  Cluster (default)              Local
  - SNAT to node IP             - No SNAT (source IP preserved)
  - Routes to any node's pod    - Only routes to local pods
  - Even distribution           - Uneven if pods are imbalanced
  - Source IP lost               - Returns 503 if no local pod
```

## The Challenge

### Step 1: Deploy an Application That Reports Source IP

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echoserver
spec:
  replicas: 2
  selector:
    matchLabels:
      app: echoserver
  template:
    metadata:
      labels:
        app: echoserver
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          command:
            - /bin/sh
            - -c
            - |
              cat > /etc/nginx/conf.d/default.conf << 'CONF'
              server {
                  listen 80;
                  location / {
                      return 200 'node: $hostname\nclient: $remote_addr\n';
                      add_header Content-Type text/plain;
                  }
              }
              CONF
              nginx -g "daemon off;"
```

### Step 2: Create Services with Different Traffic Policies

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: echo-cluster
spec:
  type: NodePort
  selector:
    app: echoserver
  externalTrafficPolicy: Cluster     # Default -- SNAT, cross-node routing
  ports:
    - port: 80
      targetPort: 80
      nodePort: 30081
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: echo-local
spec:
  type: NodePort
  selector:
    app: echoserver
  externalTrafficPolicy: Local       # No SNAT, local-only routing
  ports:
    - port: 80
      targetPort: 80
      nodePort: 30082
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: echo-internal-local
spec:
  selector:
    app: echoserver
  internalTrafficPolicy: Local       # ClusterIP traffic stays node-local
  ports:
    - port: 80
      targetPort: 80
      name: http
```

Apply everything:

```bash
kubectl apply -f deployment.yaml
kubectl apply -f services.yaml
```

### Step 3: Compare External Traffic Policies

Test with `Cluster` policy -- note the client IP is the node's internal IP (SNAT):

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
kubectl run test-cluster --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30081
```

Test with `Local` policy -- the client IP is preserved:

```bash
kubectl run test-local --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30082
```

### Step 4: Analyze Health Check Behavior

With `Local` policy, Kubernetes sets up health check node ports so external load balancers can detect which nodes have running pods:

```bash
kubectl get svc echo-local -o jsonpath='{.spec.healthCheckNodePort}'
```

Nodes without pods return HTTP 503 on this port, telling the load balancer to stop sending traffic to them.

### Step 5: Explore Internal Traffic Policy

`internalTrafficPolicy: Local` restricts ClusterIP traffic to pods on the same node. This is useful for node-local caching or DaemonSet-backed services:

```bash
kubectl get svc echo-internal-local -o yaml | grep internalTrafficPolicy
```

If a pod makes a request to this Service and no backend pod exists on the same node, the request fails rather than being routed cross-node.

## Verify What You Learned

```bash
# Services show different policies
kubectl get svc echo-cluster echo-local echo-internal-local

# Cluster policy: source IP is node IP (SNAT)
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
kubectl run v1 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30081

# Local policy: source IP is the client's actual IP
kubectl run v2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30082

# Health check node port exists for Local policy
kubectl get svc echo-local -o jsonpath='{.spec.healthCheckNodePort}'
```

## Cleanup

```bash
kubectl delete deployment echoserver
kubectl delete svc echo-cluster echo-local echo-internal-local
```

## What's Next

In [exercise 12 (Cross-Namespace Service Discovery Patterns)](../12-service-discovery-patterns/12-service-discovery-patterns.md), you will learn how to architect service discovery across namespaces, including delegation patterns and ExternalName proxies.

## Summary

- `externalTrafficPolicy: Cluster` (default) distributes traffic cluster-wide but loses the client source IP via SNAT.
- `externalTrafficPolicy: Local` preserves source IP but only routes to pods on the receiving node.
- `Local` policy creates a health check node port so load balancers skip nodes without pods.
- `internalTrafficPolicy: Local` restricts ClusterIP traffic to the same node, useful for DaemonSet-backed services.
- Choose `Cluster` for even distribution; choose `Local` when source IP preservation or locality matters.

## Reference

- [External Traffic Policy](https://kubernetes.io/docs/tasks/access-application-cluster/create-external-load-balancer/#preserving-the-client-source-ip)
- [Internal Traffic Policy](https://kubernetes.io/docs/concepts/services-networking/service-traffic-policy/)

## Additional Resources

- [Source IP for Services](https://kubernetes.io/docs/tutorials/services/source-ip/)
- [Virtual IPs and Service Proxies](https://kubernetes.io/docs/reference/networking/virtual-ips/)
