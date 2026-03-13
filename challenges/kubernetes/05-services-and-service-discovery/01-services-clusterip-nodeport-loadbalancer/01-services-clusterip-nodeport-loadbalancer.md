# 1. Services: ClusterIP, NodePort, LoadBalancer

<!--
difficulty: basic
concepts: [clusterip, nodeport, loadbalancer, service-types, selector, endpoints]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster

Verify your cluster is ready:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see at least one node in `Ready` status.

## Learning Objectives

By the end of this exercise you will be able to:

- **Remember** the three main Service types and when to use each
- **Understand** how selectors connect Services to Pods via Endpoints
- **Apply** kubectl commands to create, test, and inspect all three Service types

## Why Services?

Pods are ephemeral. They get new IP addresses every time they restart, and there is no built-in DNS for individual pod IPs. A Service provides a stable virtual IP (ClusterIP) and DNS name that routes traffic to a set of pods matched by label selectors. Without Services, every consumer of your application would need to track pod IPs manually -- an impossible task in a dynamic environment.

Kubernetes offers three main Service types. **ClusterIP** is the default and provides an internal-only virtual IP reachable from within the cluster. **NodePort** builds on ClusterIP by also allocating a static port (30000-32767) on every node, allowing external traffic to reach the Service without a cloud load balancer. **LoadBalancer** builds on NodePort by additionally requesting an external load balancer from the cloud provider, giving you a public IP that distributes traffic across nodes.

Understanding these three types and how they layer on each other is fundamental. Every networking feature in Kubernetes -- Ingress, Gateway API, service meshes -- ultimately routes traffic to Services.

## Step 1: Create the Deployment

Create a Deployment that the Services will target:

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: nginx-app
  labels:
    app: nginx-app             # Labels on the Deployment itself (for organization)
spec:
  replicas: 3                  # Three pods for load distribution testing
  selector:
    matchLabels:
      app: nginx-app           # Must match the pod template labels
  template:
    metadata:
      labels:
        app: nginx-app         # These labels are what Services select on
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80 # Informational -- documents what the container listens on
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 3
            periodSeconds: 5
```

```bash
kubectl apply -f deployment.yaml
```

## Step 2: Create a ClusterIP Service

ClusterIP is the default type. It assigns an internal virtual IP reachable only from within the cluster:

```yaml
# svc-clusterip.yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-clusterip
spec:
  type: ClusterIP              # Default type -- can be omitted
  selector:
    app: nginx-app             # Matches pods with this label
  ports:
    - port: 80                 # Port the Service listens on
      targetPort: 80           # Port on the pod to forward to
      protocol: TCP
      name: http               # Named port -- required when a Service has multiple ports
  sessionAffinity: None        # Default -- requests are round-robined across pods
```

```bash
kubectl apply -f svc-clusterip.yaml
```

## Step 3: Create a NodePort Service

NodePort opens a static port on every node in the cluster:

```yaml
# svc-nodeport.yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-nodeport
spec:
  type: NodePort
  selector:
    app: nginx-app
  ports:
    - port: 80                 # ClusterIP port (still gets a ClusterIP too)
      targetPort: 80
      nodePort: 30080          # Static port on every node (range 30000-32767)
      protocol: TCP
      name: http
  externalTrafficPolicy: Local # Preserves client source IP; only routes to local pods
```

```bash
kubectl apply -f svc-nodeport.yaml
```

## Step 4: Create a LoadBalancer Service

LoadBalancer requests an external load balancer from the cloud provider. In local clusters (minikube, kind), the external IP will stay `<pending>` unless you use a tool like MetalLB:

```yaml
# svc-loadbalancer.yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-loadbalancer
spec:
  type: LoadBalancer
  selector:
    app: nginx-app
  ports:
    - port: 80
      targetPort: 80
      protocol: TCP
      name: http
  externalTrafficPolicy: Cluster # Default -- distributes traffic across all nodes
```

```bash
kubectl apply -f svc-loadbalancer.yaml
```

## Common Mistakes

### Mistake 1: Selector Does Not Match Pod Labels

The Service selector must exactly match the labels in the pod template. If the Deployment uses `app: nginx-app` but the Service selector says `app: nginx`, the Endpoints list will be empty and traffic will not reach any pod.

```bash
# Check if endpoints are populated
kubectl get endpoints nginx-clusterip
```

If you see `<none>` under ENDPOINTS, compare the Service selector against your pod labels.

### Mistake 2: Confusing port and targetPort

The `port` field is what clients connect to (the Service's own port). The `targetPort` is where traffic gets forwarded on the pod. If your container listens on port 8080 but you set `targetPort: 80`, connections will fail.

### Mistake 3: NodePort Outside Valid Range

NodePort values must be in the range 30000-32767. Specifying a value outside this range causes a validation error. If you omit `nodePort`, Kubernetes assigns one automatically.

## Verify What You Learned

Verify all three Services exist:

```bash
kubectl get svc nginx-clusterip nginx-nodeport nginx-loadbalancer
```

Expected output: three Services with types ClusterIP, NodePort, and LoadBalancer.

Check endpoints for each Service:

```bash
kubectl get endpoints nginx-clusterip nginx-nodeport nginx-loadbalancer
```

Expected output: each shows the IPs of the 3 pods.

Test ClusterIP access from inside the cluster:

```bash
kubectl run curl-test --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://nginx-clusterip
```

Expected output: the nginx welcome page HTML.

Test ClusterIP using the full DNS name:

```bash
kubectl run curl-test2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://nginx-clusterip.default.svc.cluster.local
```

Test NodePort access:

```bash
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
kubectl run curl-np --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://$NODE_IP:30080
```

Inspect Service details:

```bash
kubectl describe svc nginx-clusterip
kubectl describe svc nginx-nodeport
kubectl describe svc nginx-loadbalancer
```

## Cleanup

```bash
kubectl delete deployment nginx-app
kubectl delete svc nginx-clusterip nginx-nodeport nginx-loadbalancer
```

## What's Next

Now that you understand the three Service types, [exercise 02 (ClusterIP Service Fundamentals)](../02-clusterip-service-fundamentals/02-clusterip-service-fundamentals.md) takes a deeper look at ClusterIP -- the most commonly used type -- including how kube-proxy programs iptables rules and how virtual IPs actually work.

## Summary

- **ClusterIP** provides an internal-only virtual IP and is the default Service type.
- **NodePort** extends ClusterIP by opening a static port (30000-32767) on every node for external access.
- **LoadBalancer** extends NodePort by provisioning a cloud load balancer with a public IP.
- Services use **label selectors** to match pods; Kubernetes maintains an **Endpoints** object listing matched pod IPs.
- `externalTrafficPolicy: Local` preserves client source IP but only routes to pods on the receiving node.
- `sessionAffinity` controls whether repeated requests from a client stick to the same pod.

## Reference

- [Service](https://kubernetes.io/docs/concepts/services-networking/service/) -- types, selectors, and configuration
- [Virtual IPs and Service Proxies](https://kubernetes.io/docs/reference/networking/virtual-ips/) -- how kube-proxy implements Services

## Additional Resources

- [Connecting Applications with Services](https://kubernetes.io/docs/tutorials/services/connect-applications-service/)
- [Service Types](https://kubernetes.io/docs/concepts/services-networking/service/#publishing-services-service-types)
- [Debugging Services](https://kubernetes.io/docs/tasks/debug/debug-application/debug-service/)
