# 2. ClusterIP Service Fundamentals

<!--
difficulty: basic
concepts: [clusterip, kube-proxy, iptables, virtual-ip, service-ports]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: understand
prerequisites: [05-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (Services: ClusterIP, NodePort, LoadBalancer)](../01-services-clusterip-nodeport-loadbalancer/)

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** how ClusterIP provides a stable virtual IP for internal communication
- **Explain** the role of kube-proxy in programming iptables/ipvs rules
- **Create** ClusterIP Services with different port mappings and verify connectivity

## Why ClusterIP Matters

ClusterIP is the foundation of all Service types. NodePort and LoadBalancer both include a ClusterIP internally. When you create a ClusterIP Service, Kubernetes assigns a virtual IP from the Service CIDR range. This IP does not belong to any network interface -- it exists only in iptables (or IPVS) rules programmed by kube-proxy on every node. When a packet is destined for this virtual IP, kube-proxy intercepts it and forwards it to one of the healthy pod IPs listed in the Endpoints object.

This exercise focuses on building intuition for how ClusterIP Services work, how port mapping behaves, and how to verify that traffic reaches the correct pods.

## Step 1: Deploy Two Different Applications

Create two applications with different labels so you can target them with separate Services:

```yaml
# apps.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-frontend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
      tier: frontend
  template:
    metadata:
      labels:
        app: web
        tier: frontend                     # Multiple labels for fine-grained selection
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-backend
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web
      tier: backend
  template:
    metadata:
      labels:
        app: web
        tier: backend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
```

```bash
kubectl apply -f apps.yaml
```

## Step 2: Create ClusterIP Services with Port Mapping

The Service port does not have to match the container port. Map external-facing port 8080 to container port 80:

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: frontend-svc
spec:
  type: ClusterIP                          # Explicit, though this is the default
  selector:
    app: web
    tier: frontend                         # Selects only frontend pods
  ports:
    - port: 8080                           # Clients connect to this port
      targetPort: 80                       # Traffic forwarded to container port 80
      protocol: TCP
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: backend-svc
spec:
  selector:
    app: web
    tier: backend                          # Selects only backend pods
  ports:
    - port: 80                             # Same port mapping here
      targetPort: 80
      protocol: TCP
      name: http
```

```bash
kubectl apply -f services.yaml
```

## Step 3: Observe the Virtual IP and Endpoints

Examine the ClusterIP assigned to each Service:

```bash
kubectl get svc frontend-svc backend-svc
```

Note the CLUSTER-IP column. These IPs come from the cluster's Service CIDR (typically `10.96.0.0/12`).

View the Endpoints that Kubernetes automatically created:

```bash
kubectl get endpoints frontend-svc backend-svc
```

Each Endpoints object lists the pod IPs and target ports. When pods scale up or down, Kubernetes updates this list automatically.

## Step 4: Test Connectivity and Port Mapping

Verify the frontend Service responds on port 8080 (not 80):

```bash
kubectl run test-pod --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://frontend-svc:8080
```

Verify the backend Service responds on port 80:

```bash
kubectl run test-pod2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://backend-svc
```

Try connecting to the frontend on the wrong port -- this should fail:

```bash
kubectl run test-pod3 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- --timeout=3 http://frontend-svc:80
```

This timeout confirms that the Service only listens on the port you defined (8080), not on the container's port (80).

## Common Mistakes

### Mistake 1: Omitting the Selector

A Service without a `selector` field will not automatically create Endpoints. It is valid (for manual Endpoints or ExternalName), but if you intended to route to pods, the Service will accept connections and immediately reset them.

### Mistake 2: Using the Container Port Instead of the Service Port

Clients must connect to the Service's `port`, not the `targetPort`. If your Service maps `port: 8080` to `targetPort: 80`, you connect to 8080. This is the most common source of "connection refused" errors with Services.

### Mistake 3: Selector Matches Too Many Pods

If your selector is too broad (e.g., `app: web` without `tier`), the Service will load-balance across both frontend and backend pods, producing inconsistent responses.

## Verify What You Learned

```bash
# Both Services should show ClusterIP addresses
kubectl get svc frontend-svc backend-svc

# Each Endpoints object should list 2 pod IPs
kubectl get endpoints frontend-svc backend-svc

# Frontend responds on port 8080
kubectl run verify --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://frontend-svc:8080
```

Expected: nginx welcome page from frontend pods.

## Cleanup

```bash
kubectl delete deployment web-frontend web-backend
kubectl delete svc frontend-svc backend-svc
```

## What's Next

ClusterIP provides internal access only. In [exercise 03 (NodePort Services)](../03-nodeport-services/), you will learn how to expose Services externally using NodePort, including port range constraints and traffic routing behavior.

## Summary

- ClusterIP assigns a **virtual IP** from the Service CIDR that exists only in iptables/IPVS rules.
- **kube-proxy** on every node programs forwarding rules so any pod can reach any Service.
- The `port` field is what clients connect to; `targetPort` is where traffic lands on the pod.
- Selectors with **multiple labels** enable fine-grained routing to specific pod subsets.
- Endpoints are automatically maintained by the Endpoints controller as pods come and go.
- ClusterIP is the default type and the foundation that NodePort and LoadBalancer build upon.

## Reference

- [Service - ClusterIP](https://kubernetes.io/docs/concepts/services-networking/service/#type-clusterip) -- ClusterIP specifics
- [Virtual IPs and Service Proxies](https://kubernetes.io/docs/reference/networking/virtual-ips/) -- iptables and IPVS modes

## Additional Resources

- [Debugging Services](https://kubernetes.io/docs/tasks/debug/debug-application/debug-service/)
- [Service API Reference](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/service-v1/)
