# 17.08 Istio Traffic Mirroring and Shadowing

<!--
difficulty: advanced
concepts: [traffic-mirroring, shadow-traffic, dark-launch, virtualservice-mirror, production-testing]
tools: [kubectl, istioctl]
estimated_time: 25m
bloom_level: analyze
prerequisites: [istio-traffic-management, virtualservice, destinationrule]
-->

## Architecture

```
                    +------------------+
                    | Client           |
                    +--------+---------+
                             |
                     request |
                             v
                    +------------------+      response
                    | Envoy Proxy      | -----------------> Client
                    +--------+---------+
                             |
              +--------------+--------------+
              |                             |
        real traffic                mirror (fire-and-forget)
              |                             |
              v                             v
     +------------------+         +------------------+
     | httpbin v1        |         | httpbin v2        |
     | (production)      |         | (shadow)          |
     +------------------+         +------------------+
```

Traffic mirroring (shadowing) copies live production traffic to a second
service. The mirrored requests are fire-and-forget -- responses are discarded.
This lets you test new versions against real traffic without affecting users.

## What You Will Build

- Two versions of a service (v1 production, v2 shadow)
- A VirtualService that routes all traffic to v1 and mirrors to v2
- Verification that v2 receives mirrored traffic
- Verification that v2 responses do not affect clients

## Suggested Steps

1. Create namespace `mirror-lab` with Istio injection.

2. Deploy v1 and v2 of httpbin:

```yaml
# services.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin-v1
  namespace: mirror-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
      version: v1
  template:
    metadata:
      labels:
        app: httpbin
        version: v1
    spec:
      containers:
        - name: httpbin
          image: kennethreitz/httpbin:latest
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin-v2
  namespace: mirror-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: httpbin
      version: v2
  template:
    metadata:
      labels:
        app: httpbin
        version: v2
    spec:
      containers:
        - name: httpbin
          image: kennethreitz/httpbin:latest
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: mirror-lab
spec:
  selector:
    app: httpbin
  ports:
    - name: http
      port: 80
```

3. Create DestinationRule with subsets:

```yaml
# destination-rule.yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: httpbin
  namespace: mirror-lab
spec:
  host: httpbin
  subsets:
    - name: v1
      labels:
        version: v1
    - name: v2
      labels:
        version: v2
```

4. Create VirtualService with mirroring:

```yaml
# virtual-service-mirror.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: mirror-lab
spec:
  hosts:
    - httpbin
  http:
    - route:
        - destination:
            host: httpbin
            subset: v1
          weight: 100             # all real traffic goes to v1
      mirror:
        host: httpbin
        subset: v2                # mirror a copy to v2
      mirrorPercentage:
        value: 100.0              # mirror 100% of traffic
```

5. Deploy a client, send requests, and verify both versions receive traffic.

6. Check that v2 logs show mirrored requests but clients only see v1 responses.

## Verify

```bash
# Both versions running
kubectl get pods -n mirror-lab -L version

# Send traffic
kubectl exec -n mirror-lab deploy/sleep -c sleep -- \
  curl -s http://httpbin/get

# Check v1 logs (should show access log)
kubectl logs -n mirror-lab deploy/httpbin-v1 -c httpbin --tail=5

# Check v2 logs (should also show access log from mirrored traffic)
kubectl logs -n mirror-lab deploy/httpbin-v2 -c httpbin --tail=5

# Verify mirror header -- mirrored requests have "-shadow" appended to Host
kubectl logs -n mirror-lab deploy/httpbin-v2 -c istio-proxy --tail=10 | grep "shadow"

# Envoy stats show mirrored requests
kubectl exec -n mirror-lab deploy/sleep -c istio-proxy -- \
  pilot-agent request GET stats | grep "mirror"
```

## Cleanup

```bash
kubectl delete namespace mirror-lab
```

## References

- [Istio Traffic Mirroring](https://istio.io/latest/docs/tasks/traffic-management/mirroring/)
- [VirtualService -- HTTPRoute mirror](https://istio.io/latest/docs/reference/config/networking/virtual-service/#HTTPRoute)
- [Dark Launching with Istio](https://istio.io/latest/blog/2017/0.1-canary/)
