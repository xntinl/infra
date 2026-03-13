<!--
difficulty: advanced
concepts: [cilium, l7-network-policy, http-filtering, cnp, cilium-network-policy]
tools: [kubectl, cilium-cli]
estimated_time: 40m
bloom_level: analyze
prerequisites: [network-policies-zero-trust, namespace-isolation-patterns]
-->

# 7.08 Cilium L7 Network Policies

## Architecture

Standard Kubernetes NetworkPolicies operate at L3/L4 (IP addresses and ports). Cilium extends this with CiliumNetworkPolicy (CNP) resources that can inspect and filter L7 (application layer) traffic -- HTTP methods, paths, headers, and even gRPC methods.

```
                        CiliumNetworkPolicy
                              |
  Client Pod ----[ L3/L4 filter ]----[ L7 HTTP filter ]----> API Pod
                   IP + Port OK?        GET /api/v1/* OK?
                                        POST /admin/* DENY
```

**Prerequisite**: A cluster running Cilium as its CNI plugin. If you are using kind, minikube, or EKS, install Cilium first.

## Suggested Steps

### 1. Verify Cilium is running

```bash
cilium status
kubectl get pods -n kube-system -l k8s-app=cilium
```

### 2. Deploy an HTTP API service

```yaml
# api-service.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api-service
  namespace: default
spec:
  replicas: 1
  selector:
    matchLabels:
      app: api-service
  template:
    metadata:
      labels:
        app: api-service
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: conf
              mountPath: /etc/nginx/conf.d
      volumes:
        - name: conf
          configMap:
            name: api-nginx-conf
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: api-nginx-conf
data:
  default.conf: |
    server {
        listen 80;
        location /api/v1/public  { return 200 '{"endpoint":"public"}\n'; }
        location /api/v1/data    { return 200 '{"endpoint":"data"}\n'; }
        location /admin          { return 200 '{"endpoint":"admin"}\n'; }
        location /health         { return 200 '{"status":"ok"}\n'; }
    }
---
apiVersion: v1
kind: Service
metadata:
  name: api-service
spec:
  selector:
    app: api-service
  ports:
    - port: 80
```

### 3. Create a CiliumNetworkPolicy with L7 HTTP rules

This policy allows GET requests to `/api/v1/.*` and `/health`, but denies everything else (including `/admin`).

```yaml
# l7-http-policy.yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: l7-http-filter
spec:
  endpointSelector:
    matchLabels:
      app: api-service
  ingress:
    - fromEndpoints:
        - matchLabels:
            role: client
      toPorts:
        - ports:
            - port: "80"
              protocol: TCP
          rules:
            http:
              - method: "GET"
                path: "/api/v1/.*"
              - method: "GET"
                path: "/health"
```

### 4. Deploy client pods

```bash
kubectl run allowed-client --image=busybox:1.37 -l role=client -- sh -c "sleep 3600"
kubectl run blocked-client --image=busybox:1.37 -l role=stranger -- sh -c "sleep 3600"
```

### 5. Test L7 filtering

- `allowed-client` requesting `GET /api/v1/public` -- should succeed
- `allowed-client` requesting `GET /admin` -- should be denied by the L7 rule (HTTP 403)
- `allowed-client` requesting `POST /api/v1/data` -- should be denied (only GET allowed)
- `blocked-client` requesting anything -- should be denied (wrong label)

### 6. Explore additional L7 capabilities

Cilium also supports:

- **gRPC filtering** -- allow specific gRPC service methods
- **Kafka topic filtering** -- restrict access to specific Kafka topics
- **DNS-aware policies** -- allow egress based on DNS names (FQDN)

Example DNS-aware egress policy:

```yaml
apiVersion: cilium.io/v2
kind: CiliumNetworkPolicy
metadata:
  name: allow-external-api
spec:
  endpointSelector:
    matchLabels:
      app: api-service
  egress:
    - toFQDNs:
        - matchName: "api.github.com"
      toPorts:
        - ports:
            - port: "443"
              protocol: TCP
```

## Verify

```bash
# Allowed client - public endpoint (expect 200)
kubectl exec allowed-client -- wget -qO- http://api-service/api/v1/public

# Allowed client - admin endpoint (expect 403 from Cilium)
kubectl exec allowed-client -- wget -qO- http://api-service/admin 2>&1

# Blocked client - any endpoint (expect connection denied)
kubectl exec blocked-client -- wget -qO- --timeout=3 http://api-service/api/v1/public 2>&1 || echo "Blocked"

# Check Cilium policy enforcement
cilium policy get
kubectl get cnp
```

## Cleanup

```bash
kubectl delete cnp l7-http-filter
kubectl delete deploy api-service
kubectl delete svc api-service
kubectl delete cm api-nginx-conf
kubectl delete pod allowed-client blocked-client
```

## What's Next

Continue to [7.09 Network Policy Debugging and Troubleshooting](../09-network-policy-debugging/09-network-policy-debugging.md) to learn how to diagnose when policies are not working as expected.

## Summary

- CiliumNetworkPolicy (CNP) extends Kubernetes NetworkPolicy with L7 inspection.
- HTTP rules can filter by method, path, and headers.
- Cilium also supports gRPC, Kafka, and DNS-aware (FQDN) policies.
- L7 policies return HTTP 403 for denied requests, unlike L3/L4 policies which time out or reset.

## References

- [Cilium L7 Policy](https://docs.cilium.io/en/stable/security/policy/language/#layer-7-examples)
- [CiliumNetworkPolicy API](https://docs.cilium.io/en/stable/security/policy/language/)
- [Cilium DNS-based Policy](https://docs.cilium.io/en/stable/security/dns/)
