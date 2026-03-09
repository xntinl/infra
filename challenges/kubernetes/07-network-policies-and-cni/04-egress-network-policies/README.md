<!--
difficulty: intermediate
concepts: [egress-network-policy, dns-access, egress-port-filtering, kube-system-dns]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [default-deny-policies, ingress-network-policies]
-->

# 7.04 Egress Network Policies and DNS Access

## What You Will Learn

- How to write egress rules that restrict where pods can send traffic
- Why DNS (port 53) egress is almost always required
- How to target CoreDNS in kube-system with a namespace selector
- How to combine egress rules for different destinations

## Steps

### 1. Namespace with default deny

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: egress-demo
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: egress-demo
spec:
  podSelector: {}
  policyTypes:
    - Ingress
    - Egress
```

### 2. Allow DNS for all pods

Without DNS egress, pods cannot resolve any service name. This rule allows UDP and TCP port 53 to kube-system where CoreDNS runs.

```yaml
# allow-dns.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns
  namespace: egress-demo
spec:
  podSelector: {}                     # all pods in the namespace
  policyTypes:
    - Egress
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
      ports:
        - protocol: UDP
          port: 53
        - protocol: TCP
          port: 53
```

### 3. Deploy backend and database

```yaml
# services.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: egress-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: backend
  template:
    metadata:
      labels:
        app: backend
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 8080
          command: ["sh", "-c", "sed -i 's/listen  *80;/listen 8080;/' /etc/nginx/conf.d/default.conf && nginx -g 'daemon off;'"]
---
apiVersion: v1
kind: Service
metadata:
  name: backend
  namespace: egress-demo
spec:
  selector:
    app: backend
  ports:
    - port: 8080
---
apiVersion: v1
kind: Pod
metadata:
  name: database
  namespace: egress-demo
  labels:
    app: database
spec:
  containers:
    - name: redis
      image: redis:7
      ports:
        - containerPort: 6379
---
apiVersion: v1
kind: Service
metadata:
  name: database
  namespace: egress-demo
spec:
  selector:
    app: database
  ports:
    - port: 6379
```

### 4. Allow backend egress to database only

The backend should reach the database on port 6379 but nothing else (other than DNS, already allowed).

```yaml
# allow-backend-egress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-backend-to-database
  namespace: egress-demo
spec:
  podSelector:
    matchLabels:
      app: backend
  policyTypes:
    - Egress
  egress:
    - to:
        - podSelector:
            matchLabels:
              app: database
      ports:
        - protocol: TCP
          port: 6379
```

### 5. Allow database ingress from backend

```yaml
# allow-database-ingress.yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-database-from-backend
  namespace: egress-demo
spec:
  podSelector:
    matchLabels:
      app: database
  policyTypes:
    - Ingress
  ingress:
    - from:
        - podSelector:
            matchLabels:
              app: backend
      ports:
        - protocol: TCP
          port: 6379
```

### TODO: Add an egress rule that allows the backend to reach an external API on port 443

<details>
<summary>Hint</summary>

Use an `ipBlock` with a CIDR range for the external API endpoint, or use `0.0.0.0/0` with an `except` for internal ranges. Specify port 443 TCP.

</details>

<details>
<summary>Solution</summary>

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-backend-external-https
  namespace: egress-demo
spec:
  podSelector:
    matchLabels:
      app: backend
  policyTypes:
    - Egress
  egress:
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
            except:
              - 10.0.0.0/8
              - 172.16.0.0/12
              - 192.168.0.0/16
      ports:
        - protocol: TCP
          port: 443
```

</details>

## Verify

```bash
# DNS resolution works
kubectl exec -n egress-demo deploy/backend -- nslookup database

# Backend can reach database port 6379
kubectl exec -n egress-demo deploy/backend -- sh -c "echo PING | nc -w3 database 6379"

# Backend cannot reach an arbitrary external address
kubectl exec -n egress-demo deploy/backend -- wget -qO- --timeout=3 http://1.1.1.1 2>&1 || echo "Egress blocked"

# Database cannot initiate connections to backend
kubectl exec -n egress-demo database -- sh -c "echo | nc -w3 backend 8080" 2>&1 || echo "Egress blocked"
```

## Cleanup

```bash
kubectl delete namespace egress-demo
```

## What's Next

Continue to [7.05 IPBlock and CIDR-Based Network Policies](../05-ipblock-and-cidr-policies/) to learn how to filter traffic by IP address ranges.

## Summary

- Egress rules control outbound traffic from selected pods.
- DNS (UDP/TCP 53) must be explicitly allowed when using default-deny egress.
- Target `kube-system` by namespace label to reach CoreDNS.
- Combine pod selectors and port restrictions to limit egress to specific services.

## References

- [Network Policies - Egress](https://kubernetes.io/docs/concepts/services-networking/network-policies/)
- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
