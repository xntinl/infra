# 17.05 Istio Circuit Breaking and Outlier Detection

<!--
difficulty: intermediate
concepts: [circuit-breaking, outlier-detection, connection-pooling, destinationrule, cascading-failure-prevention]
tools: [kubectl, istioctl]
estimated_time: 25m
bloom_level: apply
prerequisites: [istio-traffic-management, destinationrule]
-->

## What You Will Learn

In this exercise you will configure Istio circuit breaking to prevent cascading
failures. You will set connection pool limits and outlier detection rules in a
DestinationRule, then observe how Istio ejects unhealthy endpoints and limits
concurrent connections.

## Step-by-Step

### 1 -- Setup

```bash
kubectl create namespace circuit-lab
kubectl label namespace circuit-lab istio-injection=enabled
```

### 2 -- Deploy the Target Service

```yaml
# httpbin.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
  namespace: circuit-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: httpbin
  template:
    metadata:
      labels:
        app: httpbin
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
  namespace: circuit-lab
spec:
  selector:
    app: httpbin
  ports:
    - name: http
      port: 80
```

### 3 -- Configure Circuit Breaking

```yaml
# circuit-breaker.yaml
apiVersion: networking.istio.io/v1
kind: DestinationRule
metadata:
  name: httpbin
  namespace: circuit-lab
spec:
  host: httpbin
  trafficPolicy:
    connectionPool:
      tcp:
        maxConnections: 1              # max 1 TCP connection per host
      http:
        h2UpgradePolicy: DEFAULT
        http1MaxPendingRequests: 1     # max 1 pending request
        http2MaxRequests: 1            # max 1 concurrent request
        maxRequestsPerConnection: 1    # close connection after each request
    outlierDetection:
      consecutive5xxErrors: 3          # eject after 3 consecutive 5xx
      interval: 10s                    # check every 10 seconds
      baseEjectionTime: 30s           # eject for at least 30 seconds
      maxEjectionPercent: 100          # allow ejecting all endpoints
```

### 4 -- Deploy a Load Generator

```yaml
# fortio.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: fortio
  namespace: circuit-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: fortio
  template:
    metadata:
      labels:
        app: fortio
    spec:
      containers:
        - name: fortio
          image: fortio/fortio:latest
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: fortio
  namespace: circuit-lab
spec:
  selector:
    app: fortio
  ports:
    - name: http
      port: 8080
```

### 5 -- Apply and Trigger the Circuit Breaker

```bash
kubectl apply -f httpbin.yaml
kubectl apply -f circuit-breaker.yaml
kubectl apply -f fortio.yaml

# Single request works fine
kubectl exec -n circuit-lab deploy/fortio -c fortio -- \
  fortio curl http://httpbin/get

# Concurrent requests trigger circuit breaking
# 3 concurrent connections, 30 requests
kubectl exec -n circuit-lab deploy/fortio -c fortio -- \
  fortio load -c 3 -qps 0 -n 30 -loglevel Warning http://httpbin/get

# Check for "overflow" errors in the output
# Code 503 = circuit breaker tripped
```

### 6 -- Observe Outlier Detection

```bash
# Check which endpoints are ejected
istioctl proxy-config endpoint deploy/fortio -n circuit-lab | grep httpbin

# Check Envoy stats for circuit breaker metrics
kubectl exec -n circuit-lab deploy/fortio -c istio-proxy -- \
  pilot-agent request GET stats | grep httpbin | grep overflow
```

## Verify

```bash
# DestinationRule applied
kubectl get destinationrule -n circuit-lab

# Load test shows some 503 responses (circuit breaker tripped)
kubectl exec -n circuit-lab deploy/fortio -c fortio -- \
  fortio load -c 5 -qps 0 -n 50 -loglevel Warning http://httpbin/get 2>&1 | grep "Code 503"

# Envoy overflow counter > 0
kubectl exec -n circuit-lab deploy/fortio -c istio-proxy -- \
  pilot-agent request GET stats | grep "upstream_rq_pending_overflow"
```

## Cleanup

```bash
kubectl delete namespace circuit-lab
```

## What's Next

Continue to [17.06 Istio Observability](../06-istio-observability/06-istio-observability.md) to explore
Kiali, Jaeger, and Prometheus integration with Istio.

## Summary

- Circuit breaking prevents cascading failures by limiting connections and requests.
- `connectionPool` sets limits on TCP connections and HTTP requests.
- `outlierDetection` ejects endpoints that return consecutive errors.
- When the circuit trips, Envoy returns 503 immediately instead of queuing.
- Use `istioctl proxy-config` and Envoy stats to monitor circuit breaker state.

## References

- [Istio Circuit Breaking](https://istio.io/latest/docs/tasks/traffic-management/circuit-breaking/)
- [DestinationRule -- TrafficPolicy](https://istio.io/latest/docs/reference/config/networking/destination-rule/#TrafficPolicy)
- [Outlier Detection](https://istio.io/latest/docs/reference/config/networking/destination-rule/#OutlierDetection)
