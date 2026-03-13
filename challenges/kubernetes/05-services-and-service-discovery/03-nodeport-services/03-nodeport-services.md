# 3. NodePort Services and External Access

<!--
difficulty: basic
concepts: [nodeport, external-access, port-range, node-ip, service-types]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [05-01, 05-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 02 (ClusterIP Service Fundamentals)](../02-clusterip-service-fundamentals/02-clusterip-service-fundamentals.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** how NodePort allocates a static port on every node
- **Explain** the relationship between nodePort, port, and targetPort
- **Create** NodePort Services with both automatic and manual port assignment

## Why NodePort?

NodePort is the simplest way to expose a Service externally without a cloud load balancer. It opens a port on every node's IP address, so external clients can reach your application at `<NodeIP>:<NodePort>`. This makes it useful for development environments, bare-metal clusters, or situations where you bring your own load balancer. NodePort builds on ClusterIP -- every NodePort Service automatically gets a ClusterIP too.

The port range 30000-32767 is reserved for NodePort allocations. You can let Kubernetes pick a port automatically or specify one explicitly. Understanding the three-port relationship (nodePort, port, targetPort) is essential because misconfiguring any one of them leads to connection failures.

## Step 1: Create a Deployment

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: echo-server
spec:
  replicas: 3
  selector:
    matchLabels:
      app: echo-server
  template:
    metadata:
      labels:
        app: echo-server
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
```

```bash
kubectl apply -f deployment.yaml
```

## Step 2: Create a NodePort Service with Auto-Assigned Port

When you omit `nodePort`, Kubernetes picks an available port from the range:

```yaml
# svc-auto.yaml
apiVersion: v1
kind: Service
metadata:
  name: echo-auto-np
spec:
  type: NodePort
  selector:
    app: echo-server
  ports:
    - port: 80                  # ClusterIP port -- used for internal access
      targetPort: 80            # Container port
      protocol: TCP
      name: http
      # nodePort is omitted -- Kubernetes assigns one automatically
```

```bash
kubectl apply -f svc-auto.yaml
```

Check which port was assigned:

```bash
kubectl get svc echo-auto-np
```

The PORT(S) column will show something like `80:31234/TCP`. The number after the colon is the auto-assigned nodePort.

## Step 3: Create a NodePort Service with Fixed Port

Specifying a nodePort gives you a predictable external port:

```yaml
# svc-fixed.yaml
apiVersion: v1
kind: Service
metadata:
  name: echo-fixed-np
spec:
  type: NodePort
  selector:
    app: echo-server
  ports:
    - port: 80
      targetPort: 80
      nodePort: 30080            # Fixed port -- must be in range 30000-32767
      protocol: TCP
      name: http
```

```bash
kubectl apply -f svc-fixed.yaml
```

## Step 4: Test External Access

Get your node IP and test both Services:

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# Auto-assigned port
AUTO_PORT=$(kubectl get svc echo-auto-np -o jsonpath='{.spec.ports[0].nodePort}')
kubectl run test-auto --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:$AUTO_PORT

# Fixed port
kubectl run test-fixed --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30080
```

Both should return the nginx welcome page.

## Step 5: Understand the Three-Port Model

Inspect the Service to see all three ports:

```bash
kubectl get svc echo-fixed-np -o yaml | grep -A5 "ports:"
```

The three ports serve different purposes:
- **nodePort** (30080): external clients connect to `<NodeIP>:30080`
- **port** (80): internal clients connect to `<ClusterIP>:80`
- **targetPort** (80): traffic arrives at the pod on this port

Internal pods can still reach this Service via its ClusterIP on port 80, just like a regular ClusterIP Service.

## Common Mistakes

### Mistake 1: NodePort Outside Valid Range

```yaml
  ports:
    - nodePort: 8080   # WRONG -- must be 30000-32767
```

This produces a validation error. The default range is 30000-32767 unless the API server was started with a custom `--service-node-port-range`.

### Mistake 2: Conflicting NodePorts

Two Services cannot use the same nodePort value. If you try to assign 30080 to a second Service, Kubernetes rejects it with a conflict error.

### Mistake 3: Forgetting NodePort Also Has a ClusterIP

NodePort is a superset of ClusterIP. If you only need internal access, do not use NodePort -- it unnecessarily opens ports on every node.

## Verify What You Learned

```bash
# Both Services should show NodePort type
kubectl get svc echo-auto-np echo-fixed-np

# Endpoints should list 3 pod IPs
kubectl get endpoints echo-auto-np echo-fixed-np

# External access via node IP and port
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
kubectl run verify --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30080
```

## Cleanup

```bash
kubectl delete deployment echo-server
kubectl delete svc echo-auto-np echo-fixed-np
```

## What's Next

Services use label selectors to find pods, but how exactly does Kubernetes track which pods are healthy and ready? In [exercise 04 (Service Selectors and Endpoint Objects)](../04-service-selectors-and-endpoints/04-service-selectors-and-endpoints.md), you will explore the mechanics of selectors, Endpoints objects, and what happens when pods fail readiness probes.

## Summary

- **NodePort** opens a static port (30000-32767) on every node's IP address for external access.
- Every NodePort Service also gets a **ClusterIP** for internal cluster communication.
- The three-port model: **nodePort** (external), **port** (ClusterIP), **targetPort** (pod).
- Omitting `nodePort` lets Kubernetes **auto-assign** an available port.
- Two Services cannot share the same nodePort value on a cluster.
- Use NodePort for development, bare-metal, or when you manage your own load balancer.

## Reference

- [Service - NodePort](https://kubernetes.io/docs/concepts/services-networking/service/#type-nodeport) -- NodePort configuration
- [Connecting Applications with Services](https://kubernetes.io/docs/tutorials/services/connect-applications-service/)

## Additional Resources

- [Accessing Services Running on Clusters](https://kubernetes.io/docs/tasks/access-application-cluster/access-cluster-services/)
- [Service API Reference](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/service-v1/)
