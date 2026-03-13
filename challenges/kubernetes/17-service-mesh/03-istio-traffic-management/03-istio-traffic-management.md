# 17.03 Istio Traffic Management: VirtualService and DestinationRule

<!--
difficulty: intermediate
concepts: [virtualservice, destinationrule, traffic-routing, canary, weighted-routing, header-based-routing]
tools: [kubectl, istioctl]
estimated_time: 30m
bloom_level: apply
prerequisites: [istio-installation-and-injection, deployments, services]
-->

## What You Will Learn

In this exercise you will use Istio's `VirtualService` and `DestinationRule` to
control traffic routing between service versions. You will implement weighted
traffic splitting (canary deployment), header-based routing, and subset
definitions.

## Step-by-Step

### 1 -- Prerequisites

Istio must be installed and a namespace labeled for injection:

```bash
kubectl create namespace traffic-lab
kubectl label namespace traffic-lab istio-injection=enabled
```

### 2 -- Deploy Two Versions of a Service

```yaml
# v1.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin-v1
  namespace: traffic-lab
spec:
  replicas: 2
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
          env:
            - name: VERSION
              value: "v1"
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin-v2
  namespace: traffic-lab
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
          env:
            - name: VERSION
              value: "v2"
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: httpbin
  namespace: traffic-lab
spec:
  selector:
    app: httpbin        # selects both v1 and v2
  ports:
    - name: http
      port: 80
      targetPort: 80
```

### 3 -- Define Subsets with DestinationRule

```yaml
# destination-rule.yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: httpbin
  namespace: traffic-lab
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

### 4 -- Weighted Traffic Split (90/10 Canary)

```yaml
# virtual-service-canary.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: traffic-lab
spec:
  hosts:
    - httpbin
  http:
    - route:
        - destination:
            host: httpbin
            subset: v1
          weight: 90
        - destination:
            host: httpbin
            subset: v2
          weight: 10
```

### 5 -- Header-Based Routing

Route requests with a specific header to v2, everything else to v1:

```yaml
# virtual-service-header.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: traffic-lab
spec:
  hosts:
    - httpbin
  http:
    - match:
        - headers:
            x-version:
              exact: "canary"
      route:
        - destination:
            host: httpbin
            subset: v2
    - route:
        - destination:
            host: httpbin
            subset: v1
```

### 6 -- Deploy a Client and Apply

```yaml
# sleep.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sleep
  namespace: traffic-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sleep
  template:
    metadata:
      labels:
        app: sleep
    spec:
      containers:
        - name: sleep
          image: curlimages/curl:8.7.1
          command: ["sleep", "3600"]
```

```bash
kubectl apply -f v1.yaml
kubectl apply -f destination-rule.yaml
kubectl apply -f virtual-service-canary.yaml
kubectl apply -f sleep.yaml
```

### 7 -- Test the Routing

```bash
# Send 20 requests and count responses from each version
for i in $(seq 1 20); do
  kubectl exec -n traffic-lab deploy/sleep -c sleep -- \
    curl -s http://httpbin/get | grep -o '"VERSION": "[^"]*"'
done | sort | uniq -c

# Switch to header-based routing
kubectl apply -f virtual-service-header.yaml

# Without header -> v1
kubectl exec -n traffic-lab deploy/sleep -c sleep -- curl -s http://httpbin/get | grep VERSION

# With header -> v2
kubectl exec -n traffic-lab deploy/sleep -c sleep -- curl -s -H "x-version: canary" http://httpbin/get | grep VERSION
```

## Verify

```bash
# VirtualService and DestinationRule created
kubectl get virtualservices,destinationrules -n traffic-lab

# Proxy configuration applied
istioctl proxy-config routes deploy/sleep -n traffic-lab | grep httpbin

# Traffic distribution matches expected weights
for i in $(seq 1 100); do
  kubectl exec -n traffic-lab deploy/sleep -c sleep -- \
    curl -s http://httpbin/get 2>/dev/null | grep -o '"VERSION": "[^"]*"'
done | sort | uniq -c
```

## Cleanup

```bash
kubectl delete namespace traffic-lab
```

## What's Next

Continue to [17.04 Istio Fault Injection](../04-istio-fault-injection/04-istio-fault-injection.md) to
learn how to inject failures and delays for resilience testing.

## Summary

- `DestinationRule` defines subsets by Pod labels (e.g., `version: v1`).
- `VirtualService` controls how traffic is routed to those subsets.
- Weighted routing enables canary deployments without changing Kubernetes resources.
- Header-based routing allows targeted testing of new versions.
- Both resources work together -- DestinationRule defines the "where",
  VirtualService defines the "how".

## References

- [Istio Traffic Management](https://istio.io/latest/docs/concepts/traffic-management/)
- [VirtualService Reference](https://istio.io/latest/docs/reference/config/networking/virtual-service/)
- [DestinationRule Reference](https://istio.io/latest/docs/reference/config/networking/destination-rule/)
