# 10. Session Affinity and Traffic Policies

<!--
difficulty: advanced
concepts: [session-affinity, clientip, traffic-distribution, service-proxy, iptables]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [05-02, 05-04]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 02](../02-clusterip-service-fundamentals/02-clusterip-service-fundamentals.md) and [exercise 04](../04-service-selectors-and-endpoints/04-service-selectors-and-endpoints.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how session affinity affects traffic distribution across pods
- **Configure** `sessionAffinity: ClientIP` with timeout settings
- **Compare** default round-robin behavior with sticky session behavior

## Architecture

```
                  Client (10.244.0.5)
                         │
                         ▼
               ┌─────────────────┐
               │    Service      │
               │ sessionAffinity:│
               │   ClientIP      │
               └────────┬────────┘
                        │
            ┌───────────┤  (same client always
            │           │   hits same pod)
            ▼           ▼
         pod-A       pod-B       pod-C
         (sticky)    (idle)      (idle)
```

By default, kube-proxy distributes traffic across pods using round-robin (iptables mode) or random selection. Session affinity changes this behavior by remembering which pod served a given client IP and routing subsequent requests from that IP to the same pod for a configurable duration.

## The Challenge

### Step 1: Deploy a Backend that Reports Its Identity

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sticky-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: sticky-app
  template:
    metadata:
      labels:
        app: sticky-app
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
              echo "pod: $(hostname)" > /usr/share/nginx/html/index.html
              nginx -g "daemon off;"
```

### Step 2: Create Two Services -- One With Affinity, One Without

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: no-affinity
spec:
  selector:
    app: sticky-app
  sessionAffinity: None              # Default -- round-robin
  ports:
    - port: 80
      targetPort: 80
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: with-affinity
spec:
  selector:
    app: sticky-app
  sessionAffinity: ClientIP          # Stick to the same pod per client IP
  sessionAffinityConfig:
    clientIP:
      timeoutSeconds: 300            # Sticky for 5 minutes (default: 10800 = 3 hours)
  ports:
    - port: 80
      targetPort: 80
      name: http
```

Apply both:

```bash
kubectl apply -f deployment.yaml
kubectl apply -f services.yaml
```

### Step 3: Test Round-Robin Behavior

From a test pod, make multiple requests to the non-affinity Service:

```bash
kubectl run test-rr --image=busybox:1.37 --rm -it --restart=Never -- sh -c \
  'for i in $(seq 1 10); do wget -qO- http://no-affinity; done'
```

You should see responses from different pods (the hostname changes between requests).

### Step 4: Test Session Affinity Behavior

```bash
kubectl run test-sticky --image=busybox:1.37 --rm -it --restart=Never -- sh -c \
  'for i in $(seq 1 10); do wget -qO- http://with-affinity; done'
```

All 10 requests should return the same pod hostname because the client IP is consistent.

### Step 5: Verify Affinity Configuration

```bash
kubectl get svc with-affinity -o yaml | grep -A5 sessionAffinity
```

Inspect how kube-proxy implements this:

```bash
kubectl describe svc with-affinity
```

The `Session Affinity` field shows `ClientIP` and the timeout.

### Step 6: Analyze Edge Cases

Consider what happens when the target pod is deleted:

```bash
# Find which pod the sticky Service routes to
kubectl run identify --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://with-affinity

# Delete that specific pod
kubectl delete pod <pod-name>

# After the Deployment recreates it, test again
kubectl run identify2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://with-affinity
```

The affinity entry is invalidated when the target pod disappears. kube-proxy selects a new pod and creates a fresh affinity binding.

## Verify What You Learned

```bash
# No-affinity distributes across pods
kubectl run v1 --image=busybox:1.37 --rm -it --restart=Never -- sh -c \
  'for i in $(seq 1 6); do wget -qO- http://no-affinity; done'

# With-affinity sticks to one pod
kubectl run v2 --image=busybox:1.37 --rm -it --restart=Never -- sh -c \
  'for i in $(seq 1 6); do wget -qO- http://with-affinity; done'

# Session affinity config is visible
kubectl get svc with-affinity -o jsonpath='{.spec.sessionAffinityConfig}'
```

## Cleanup

```bash
kubectl delete deployment sticky-app
kubectl delete svc no-affinity with-affinity
```

## What's Next

Session affinity controls which pod handles repeat requests. In [exercise 11 (External and Internal Traffic Policy)](../11-external-traffic-policy/11-external-traffic-policy.md), you will learn how `externalTrafficPolicy` and `internalTrafficPolicy` control whether traffic is routed only to local pods or distributed cluster-wide.

## Summary

- `sessionAffinity: None` (default) distributes traffic via round-robin across all healthy pods.
- `sessionAffinity: ClientIP` routes all requests from the same client IP to the same pod.
- `timeoutSeconds` controls how long the affinity binding persists (default: 10800 seconds / 3 hours).
- Affinity bindings are invalidated when the target pod is deleted; kube-proxy selects a new pod.
- Session affinity is implemented at the kube-proxy level (iptables or IPVS), not in the application.

## Reference

- [Session Affinity](https://kubernetes.io/docs/reference/networking/virtual-ips/#session-affinity)
- [Service Configuration](https://kubernetes.io/docs/concepts/services-networking/service/#session-affinity)

## Additional Resources

- [Virtual IPs and Service Proxies](https://kubernetes.io/docs/reference/networking/virtual-ips/)
- [IPVS-Based Service Load Balancing](https://kubernetes.io/blog/2018/07/09/ipvs-based-in-cluster-load-balancing-deep-dive/)
