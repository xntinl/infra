# 6. Headless Services and StatefulSet DNS

<!--
difficulty: intermediate
concepts: [headless-service, statefulset, stable-network-id, srv-records, dns-discovery]
tools: [kubectl, minikube, nslookup, dig]
estimated_time: 40m
bloom_level: apply
prerequisites: [05-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with a default StorageClass
- `kubectl` installed and configured
- Completion of [exercise 05 (DNS and Service Discovery)](../05-dns-and-service-discovery/05-dns-and-service-discovery.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a Headless Service backed by a StatefulSet with stable per-pod DNS names
- **Apply** SRV record queries for service discovery of named ports
- **Analyze** how pod deletion and recreation affects DNS resolution

## The Challenge

Create a StatefulSet simulating a database cluster with a Headless Service. Each pod gets a stable DNS name following the pattern `<pod-name>.<service-name>.<namespace>.svc.cluster.local`. Explore A records, SRV records, and verify that DNS names survive pod restarts.

### Step 1: Create the Namespace and Headless Service

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: statefulset-dns-demo
```

```yaml
# headless-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: db-headless
  namespace: statefulset-dns-demo
  labels:
    app: db-cluster
spec:
  clusterIP: None                    # Headless -- enables per-pod DNS
  publishNotReadyAddresses: true     # Resolve pods even before they pass readiness
  selector:
    app: db-cluster
  ports:
    - port: 5432
      targetPort: 5432
      name: db                      # Named port -- queryable via SRV records
    - port: 8080
      targetPort: 8080
      name: http
```

### Step 2: Create the StatefulSet

```yaml
# statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: db
  namespace: statefulset-dns-demo
spec:
  serviceName: db-headless           # Must reference the Headless Service name
  replicas: 3
  selector:
    matchLabels:
      app: db-cluster
  template:
    metadata:
      labels:
        app: db-cluster
    spec:
      containers:
        - name: db-sim
          image: nginx:1.27
          ports:
            - containerPort: 5432
              name: db
            - containerPort: 8080
              name: http
          command:
            - /bin/sh
            - -c
            - |
              cat > /etc/nginx/conf.d/default.conf << 'CONF'
              server {
                  listen 8080;
                  location / {
                      return 200 '{"pod":"MYHOSTNAME","role":"replica","port":5432}\n';
                      add_header Content-Type application/json;
                  }
              }
              CONF
              sed -i "s/MYHOSTNAME/$(hostname)/g" /etc/nginx/conf.d/default.conf
              nginx -g "daemon off;"
          readinessProbe:
            httpGet:
              path: /
              port: 8080
            initialDelaySeconds: 5
            periodSeconds: 5
  volumeClaimTemplates:
    - metadata:
        name: data
      spec:
        accessModes: ["ReadWriteOnce"]
        resources:
          requests:
            storage: 1Gi
```

### Step 3: Deploy DNS Utility Pod

```yaml
# dns-utils.yaml
apiVersion: v1
kind: Pod
metadata:
  name: dns-utils
  namespace: statefulset-dns-demo
spec:
  containers:
    - name: dnsutils
      image: registry.k8s.io/e2e-test-images/jessie-dnsutils:1.3
      command: ["sleep", "3600"]
```

### Step 4: Apply and Wait

```bash
kubectl apply -f namespace.yaml
kubectl apply -f headless-service.yaml
kubectl apply -f statefulset.yaml
kubectl apply -f dns-utils.yaml
kubectl get pods -n statefulset-dns-demo -w
```

Wait until db-0, db-1, db-2, and dns-utils are all Running.

### Step 5: Explore DNS Records

Resolve the Headless Service (returns all pod IPs):

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-headless.statefulset-dns-demo.svc.cluster.local
```

Resolve individual pods by their stable DNS name:

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-0.db-headless.statefulset-dns-demo.svc.cluster.local
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-1.db-headless.statefulset-dns-demo.svc.cluster.local
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-2.db-headless.statefulset-dns-demo.svc.cluster.local
```

Query SRV records for service discovery:

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- dig SRV db-headless.statefulset-dns-demo.svc.cluster.local +short
```

Query SRV records for a specific named port:

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- dig SRV _db._tcp.db-headless.statefulset-dns-demo.svc.cluster.local +short
```

### Step 6: Verify HTTP Access to Individual Pods

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- wget -qO- http://db-0.db-headless:8080/
kubectl exec -n statefulset-dns-demo dns-utils -- wget -qO- http://db-1.db-headless:8080/
```

### Step 7: Test Pod Deletion and DNS Recovery

Delete a pod and watch it get recreated with the same DNS name:

```bash
kubectl delete pod db-1 -n statefulset-dns-demo
kubectl get pods -n statefulset-dns-demo -w
```

After db-1 is recreated, verify DNS still resolves (IP may change):

```bash
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-1.db-headless.statefulset-dns-demo.svc.cluster.local
```

## Verify What You Learned

```bash
# Headless Service returns multiple A records
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-headless.statefulset-dns-demo.svc.cluster.local

# Individual pod DNS resolves
kubectl exec -n statefulset-dns-demo dns-utils -- nslookup db-0.db-headless.statefulset-dns-demo.svc.cluster.local

# SRV records show port and hostname
kubectl exec -n statefulset-dns-demo dns-utils -- dig SRV _db._tcp.db-headless.statefulset-dns-demo.svc.cluster.local +short

# HTTP access via DNS
kubectl exec -n statefulset-dns-demo dns-utils -- wget -qO- http://db-0.db-headless:8080/
```

## Cleanup

```bash
kubectl delete namespace statefulset-dns-demo
```

## What's Next

Services can expose multiple ports on a single ClusterIP. In [exercise 07 (Multi-Port Services)](../07-multi-port-services/07-multi-port-services.md), you will configure Services with multiple named ports and learn why port naming is required when a Service has more than one port.

## Summary

- Headless Services (`clusterIP: None`) return individual pod IPs instead of a virtual IP.
- StatefulSets combined with Headless Services provide stable DNS names: `<pod>.<service>.<ns>.svc.cluster.local`.
- SRV records enable discovery of named ports (e.g., `_db._tcp.<service>`).
- `publishNotReadyAddresses: true` makes DNS resolve pods even before readiness.
- Pod deletion and recreation preserves the DNS name; only the IP changes.

## Reference

- [StatefulSet - Stable Network ID](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#stable-network-id)
- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)

## Additional Resources

- [Headless Services](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services)
- [Debugging DNS Resolution](https://kubernetes.io/docs/tasks/administer-cluster/dns-debugging-resolution/)
