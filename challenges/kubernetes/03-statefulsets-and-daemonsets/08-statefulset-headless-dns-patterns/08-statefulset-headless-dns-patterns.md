# 8. StatefulSet DNS and Headless Service Patterns

<!--
difficulty: basic
concepts: [headless-service, dns-resolution, statefulset-dns, srv-records, pod-dns]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [03-01]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 01 (StatefulSets and Persistent Storage)](../01-statefulsets-and-persistent-storage/01-statefulsets-and-persistent-storage.md)

## Learning Objectives

- **Remember** the DNS record format for StatefulSet pods: `{pod}.{service}.{namespace}.svc.cluster.local`
- **Understand** how headless Services differ from ClusterIP Services in DNS behavior
- **Apply** DNS lookups to discover individual pods and resolve SRV records

## Why Headless Service DNS Patterns?

When a client queries a regular ClusterIP Service, DNS returns a single virtual IP that load-balances across all pods. This is perfect for stateless workloads, but useless for stateful ones. A database replica needs to connect to the specific primary pod, not a random endpoint. Headless Services (`clusterIP: None`) solve this by returning individual pod IPs as A records and creating per-pod DNS entries. Understanding these DNS patterns is fundamental to building any system where pods need to find each other by identity.

StatefulSet pods get predictable DNS names following the pattern `{pod-name}.{service-name}.{namespace}.svc.cluster.local`. The headless Service also supports SRV records that include port information, enabling service discovery without external tools.

## Step 1: Create a StatefulSet with a Headless Service

```yaml
# dns-demo.yaml
apiVersion: v1
kind: Service
metadata:
  name: dns-demo-svc
spec:
  clusterIP: None
  selector:
    app: dns-demo
  ports:
    - port: 80
      name: http
---
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: dns-demo
spec:
  serviceName: dns-demo-svc
  replicas: 3
  selector:
    matchLabels:
      app: dns-demo
  template:
    metadata:
      labels:
        app: dns-demo
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
              name: http
```

```bash
kubectl apply -f dns-demo.yaml
kubectl rollout status statefulset/dns-demo
```

## Step 2: Query A Records for Individual Pods

Each pod gets its own A record. Test with a temporary pod:

```bash
kubectl run dns-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup dns-demo-0.dns-demo-svc
```

Expected output:

```
Name:      dns-demo-0.dns-demo-svc
Address 1: 10.244.x.x dns-demo-0.dns-demo-svc.default.svc.cluster.local
```

Test all three:

```bash
for i in 0 1 2; do
  kubectl run dns-test-$i --image=busybox:1.37 --rm -it --restart=Never -- nslookup dns-demo-$i.dns-demo-svc 2>/dev/null | grep "Address 1"
done
```

Each pod resolves to a different IP address.

## Step 3: Query the Service Name (All Pod IPs)

Querying the headless Service name returns ALL pod IPs:

```bash
kubectl run dns-all --image=busybox:1.37 --rm -it --restart=Never -- nslookup dns-demo-svc
```

Expected output shows multiple addresses:

```
Name:      dns-demo-svc
Address 1: 10.244.x.x dns-demo-0.dns-demo-svc.default.svc.cluster.local
Address 2: 10.244.x.y dns-demo-1.dns-demo-svc.default.svc.cluster.local
Address 3: 10.244.x.z dns-demo-2.dns-demo-svc.default.svc.cluster.local
```

This is the key difference from a ClusterIP Service, which would return a single virtual IP.

## Step 4: Explore SRV Records

SRV records include port information. Query them:

```bash
kubectl run srv-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup -type=srv _http._tcp.dns-demo-svc.default.svc.cluster.local
```

SRV records follow the format `_port-name._protocol.service.namespace.svc.cluster.local` and return hostname and port for each pod.

## Step 5: Test DNS Stability Across Pod Deletion

Delete a pod and verify DNS updates:

```bash
kubectl delete pod dns-demo-1
```

Wait for the replacement:

```bash
kubectl get pods -l app=dns-demo -w
```

Query the DNS again:

```bash
kubectl run dns-after --image=busybox:1.37 --rm -it --restart=Never -- nslookup dns-demo-1.dns-demo-svc
```

The hostname `dns-demo-1.dns-demo-svc` still resolves, but the IP may have changed. The DNS name is stable; the IP behind it updates when the pod is rescheduled.

## Common Mistakes

### Mistake 1: Expecting DNS to Work Before Pod Is Ready

DNS records are only populated for pods that pass their readiness probe. If a pod is starting up, its DNS record does not exist yet. Configure `publishNotReadyAddresses: true` on the Service if you need DNS during initialization:

```yaml
spec:
  clusterIP: None
  publishNotReadyAddresses: true   # Register DNS even for non-ready pods
```

### Mistake 2: Confusing Headless Service DNS with ClusterIP Service DNS

A ClusterIP Service always returns one IP (the virtual IP). A headless Service returns one IP per ready pod. Using the wrong Service type breaks StatefulSet discovery patterns.

## Verify What You Learned

```bash
# Service is headless
kubectl get svc dns-demo-svc -o jsonpath='{.spec.clusterIP}'
# Expected: None

# All pods running
kubectl get pods -l app=dns-demo

# Individual DNS resolution works
kubectl run verify-dns --image=busybox:1.37 --rm -it --restart=Never -- nslookup dns-demo-0.dns-demo-svc
```

## Cleanup

```bash
kubectl delete statefulset dns-demo
kubectl delete svc dns-demo-svc
```

## What's Next

StatefulSet PVCs persist even after scale-down or StatefulSet deletion. In [exercise 09 (PersistentVolumeClaim Retention Policies)](../09-pvc-retention-policy/09-pvc-retention-policy.md), you will learn how to control this behavior with retention policies.

## Summary

- Headless Services (`clusterIP: None`) return individual pod IPs instead of a single virtual IP
- StatefulSet pods get DNS names in the format `{pod}.{service}.{namespace}.svc.cluster.local`
- Querying the Service name returns all pod IPs; querying a pod name returns one specific IP
- SRV records provide port information alongside hostnames for service discovery
- DNS records are only created for Ready pods unless `publishNotReadyAddresses: true` is set
- Pod DNS names are stable across rescheduling; only the IP behind them changes

## Reference

- [Headless Services](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services)
- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)

## Additional Resources

- [Debugging DNS Resolution](https://kubernetes.io/docs/tasks/administer-cluster/dns-debugging-resolution/)
- [StatefulSet Basics: Stable Network ID](https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/#stable-network-id)
- [Service and Pod DNS Records](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/#services)
