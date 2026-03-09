# 5. DNS and Service Discovery

<!--
difficulty: intermediate
concepts: [coredns, service-dns, pod-dns, ndots, resolv-conf, fqdn]
tools: [kubectl, minikube, nslookup, dig]
estimated_time: 35m
bloom_level: apply
prerequisites: [05-01, 05-04]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 04 (Service Selectors and Endpoint Objects)](../04-service-selectors-and-endpoints/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** DNS resolution to discover Services across namespaces
- **Analyze** how resolv.conf and ndots settings affect DNS queries
- **Differentiate** between ClusterIP DNS, Headless DNS, and ExternalName DNS behavior

## The Challenge

Deploy Services in two separate namespaces and use DNS to discover them from a utility pod. You will examine how CoreDNS resolves short names vs FQDNs, inspect the pod's resolv.conf to understand search domains, and compare the DNS responses of regular Services, Headless Services, and ExternalName Services.

### Step 1: Create Namespaces and Applications

```yaml
# namespaces.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: ns-alpha
---
apiVersion: v1
kind: Namespace
metadata:
  name: ns-beta
```

```yaml
# app-alpha.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-alpha
  namespace: ns-alpha
spec:
  replicas: 2
  selector:
    matchLabels:
      app: app-alpha
  template:
    metadata:
      labels:
        app: app-alpha
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
  name: svc-alpha
  namespace: ns-alpha
spec:
  type: ClusterIP
  selector:
    app: app-alpha
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```yaml
# app-beta.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-beta
  namespace: ns-beta
spec:
  replicas: 2
  selector:
    matchLabels:
      app: app-beta
  template:
    metadata:
      labels:
        app: app-beta
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
  name: svc-beta
  namespace: ns-beta
spec:
  type: ClusterIP
  selector:
    app: app-beta
  ports:
    - port: 80
      targetPort: 80
      name: http
```

### Step 2: Create a Headless Service and ExternalName Service

```yaml
# headless-alpha.yaml
apiVersion: v1
kind: Service
metadata:
  name: svc-alpha-headless
  namespace: ns-alpha
spec:
  clusterIP: None                    # Headless -- no virtual IP assigned
  selector:
    app: app-alpha
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```yaml
# externalname.yaml
apiVersion: v1
kind: Service
metadata:
  name: external-api
  namespace: ns-alpha
spec:
  type: ExternalName
  externalName: api.example.com      # DNS CNAME target
```

### Step 3: Deploy a DNS Utility Pod

```yaml
# dns-utils.yaml
apiVersion: v1
kind: Pod
metadata:
  name: dns-utils
  namespace: ns-alpha
spec:
  containers:
    - name: dnsutils
      image: registry.k8s.io/e2e-test-images/jessie-dnsutils:1.3
      command: ["sleep", "3600"]
```

### Step 4: Apply Everything

```bash
kubectl apply -f namespaces.yaml
kubectl apply -f app-alpha.yaml
kubectl apply -f app-beta.yaml
kubectl apply -f headless-alpha.yaml
kubectl apply -f externalname.yaml
kubectl apply -f dns-utils.yaml
```

### Step 5: Explore DNS Resolution

Wait for the utility pod to be ready:

```bash
kubectl wait --for=condition=Ready pod/dns-utils -n ns-alpha --timeout=60s
```

Resolve a Service in the same namespace using a short name:

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup svc-alpha
```

Resolve a Service in a different namespace using the FQDN:

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup svc-beta.ns-beta.svc.cluster.local
```

The short cross-namespace form also works thanks to search domains:

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup svc-beta.ns-beta
```

### Step 6: Examine resolv.conf

```bash
kubectl exec -n ns-alpha dns-utils -- cat /etc/resolv.conf
```

You will see `search ns-alpha.svc.cluster.local svc.cluster.local cluster.local` and `ndots:5`. The ndots setting means any name with fewer than 5 dots gets the search domains appended before trying it as an absolute name.

### Step 7: Compare DNS Response Types

Regular Service (single A record with ClusterIP):

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup svc-alpha
```

Headless Service (multiple A records, one per pod):

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup svc-alpha-headless
```

ExternalName Service (CNAME record):

```bash
kubectl exec -n ns-alpha dns-utils -- nslookup external-api
```

Verify CoreDNS is running:

```bash
kubectl get pods -n kube-system -l k8s-app=kube-dns
```

## Verify What You Learned

```bash
# Same-namespace resolution
kubectl exec -n ns-alpha dns-utils -- nslookup svc-alpha

# Cross-namespace resolution
kubectl exec -n ns-alpha dns-utils -- nslookup svc-beta.ns-beta.svc.cluster.local

# Headless returns multiple A records
kubectl exec -n ns-alpha dns-utils -- nslookup svc-alpha-headless

# ExternalName returns a CNAME
kubectl exec -n ns-alpha dns-utils -- nslookup external-api
```

## Cleanup

```bash
kubectl delete namespace ns-alpha ns-beta
```

## What's Next

You have seen that Headless Services return individual pod IPs. In [exercise 06 (Headless Services and StatefulSet DNS)](../06-headless-services-and-statefulset-dns/), you will combine Headless Services with StatefulSets to get stable, predictable DNS names for each pod -- a pattern essential for databases and distributed systems.

## Summary

- CoreDNS resolves Service names using the pattern `<svc>.<ns>.svc.cluster.local`.
- Short names work within the same namespace due to search domains in resolv.conf.
- `ndots:5` causes names with fewer than 5 dots to be searched with appended domains first.
- Regular Services return a single A record (ClusterIP); Headless Services return one A record per pod.
- ExternalName Services return a CNAME record pointing to an external DNS name.

## Reference

- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
- [Debugging DNS Resolution](https://kubernetes.io/docs/tasks/administer-cluster/dns-debugging-resolution/)

## Additional Resources

- [Customizing DNS Service](https://kubernetes.io/docs/tasks/administer-cluster/dns-custom-nameservers/)
- [CoreDNS Manual](https://coredns.io/manual/toc/)
