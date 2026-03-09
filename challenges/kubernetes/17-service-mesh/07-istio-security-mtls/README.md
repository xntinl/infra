# 17.07 Istio Security: mTLS and AuthorizationPolicy

<!--
difficulty: advanced
concepts: [mtls, peerauthentication, authorizationpolicy, strict-mtls, zero-trust, service-identity]
tools: [kubectl, istioctl]
estimated_time: 30m
bloom_level: analyze
prerequisites: [istio-installation-and-injection, istio-traffic-management]
-->

## Architecture

```
+-------------------+   mTLS (automatic)   +-------------------+
| Service A         | ------------------> | Service B         |
| (SPIFFE identity: |   encrypted +        | (SPIFFE identity: |
|  sa-a.ns.cluster) |   authenticated      |  sa-b.ns.cluster) |
+-------------------+                      +-------------------+
         |                                          |
         | AuthorizationPolicy                      | PeerAuthentication
         | "allow only GET from sa-a"               | "require STRICT mTLS"
```

Istio provides transport security (mTLS) and access control (AuthorizationPolicy)
at the mesh level. Workload identities are based on Kubernetes ServiceAccounts
and encoded as SPIFFE IDs in X.509 certificates automatically rotated by istiod.

## What You Will Build

- PeerAuthentication policy enforcing STRICT mTLS across a namespace
- AuthorizationPolicy controlling which services can communicate
- Verification that non-meshed clients are rejected
- Verification that unauthorized services are denied

## Suggested Steps

1. Create two namespaces: `secure-lab` (meshed) and `outside` (not meshed).

2. Deploy services in both namespaces:

```yaml
# secure-ns.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: secure-lab
  labels:
    istio-injection: enabled
---
apiVersion: v1
kind: Namespace
metadata:
  name: outside
  # No istio-injection label -- not meshed
```

3. Deploy httpbin and sleep in `secure-lab`, and a sleep client in `outside`:

```yaml
# services.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
  namespace: secure-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
  template:
    metadata:
      labels:
        app: httpbin
    spec:
      serviceAccountName: httpbin-sa
      containers:
        - name: httpbin
          image: kennethreitz/httpbin:latest
          ports:
            - containerPort: 80
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: httpbin-sa
  namespace: secure-lab
---
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: secure-lab
spec:
  selector:
    app: httpbin
  ports:
    - name: http
      port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-allowed
  namespace: secure-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep-allowed
  template:
    metadata:
      labels:
        app: sleep-allowed
    spec:
      serviceAccountName: allowed-sa
      containers:
        - name: sleep
          image: curlimages/curl:8.7.1
          command: ["sleep", "3600"]
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: allowed-sa
  namespace: secure-lab
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-denied
  namespace: secure-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep-denied
  template:
    metadata:
      labels:
        app: sleep-denied
    spec:
      serviceAccountName: denied-sa
      containers:
        - name: sleep
          image: curlimages/curl:8.7.1
          command: ["sleep", "3600"]
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: denied-sa
  namespace: secure-lab
```

4. Enforce STRICT mTLS for the namespace:

```yaml
# peer-authentication.yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: strict-mtls
  namespace: secure-lab
spec:
  mtls:
    mode: STRICT    # reject any non-mTLS traffic
```

5. Create an AuthorizationPolicy allowing only `allowed-sa` to call httpbin:

```yaml
# authz-policy.yaml
apiVersion: security.istio.io/v1
kind: AuthorizationPolicy
metadata:
  name: httpbin-policy
  namespace: secure-lab
spec:
  selector:
    matchLabels:
      app: httpbin
  action: ALLOW
  rules:
    - from:
        - source:
            principals:
              - "cluster.local/ns/secure-lab/sa/allowed-sa"
      to:
        - operation:
            methods: ["GET"]
            paths: ["/get", "/headers", "/status/*"]
```

6. Deploy a client in the `outside` namespace (no sidecar):

```yaml
# outside-client.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep-outside
  namespace: outside
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep-outside
  template:
    metadata:
      labels:
        app: sleep-outside
    spec:
      containers:
        - name: sleep
          image: curlimages/curl:8.7.1
          command: ["sleep", "3600"]
```

7. Test each scenario and observe the results.

## Verify

```bash
# 1. mTLS is enforced
istioctl authn tls-check httpbin.secure-lab.svc.cluster.local -n secure-lab

# 2. Allowed client succeeds (GET /get)
kubectl exec -n secure-lab deploy/sleep-allowed -c sleep -- \
  curl -s -o /dev/null -w "%{http_code}" http://httpbin/get
# Expect: 200

# 3. Allowed client fails on POST (not in policy)
kubectl exec -n secure-lab deploy/sleep-allowed -c sleep -- \
  curl -s -o /dev/null -w "%{http_code}" -X POST http://httpbin/post
# Expect: 403

# 4. Denied client is rejected (wrong ServiceAccount)
kubectl exec -n secure-lab deploy/sleep-denied -c sleep -- \
  curl -s -o /dev/null -w "%{http_code}" http://httpbin/get
# Expect: 403

# 5. Outside client is rejected (no mTLS)
kubectl exec -n outside deploy/sleep-outside -- \
  curl -s -o /dev/null -w "%{http_code}" http://httpbin.secure-lab/get
# Expect: connection refused or reset (STRICT mTLS rejects plaintext)
```

## Cleanup

```bash
kubectl delete namespace secure-lab
kubectl delete namespace outside
```

## References

- [Istio Security](https://istio.io/latest/docs/concepts/security/)
- [PeerAuthentication](https://istio.io/latest/docs/reference/config/security/peer_authentication/)
- [AuthorizationPolicy](https://istio.io/latest/docs/reference/config/security/authorization-policy/)
- [Mutual TLS Migration](https://istio.io/latest/docs/tasks/security/authentication/mtls-migration/)
