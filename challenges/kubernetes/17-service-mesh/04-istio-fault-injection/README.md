# 17.04 Istio Fault Injection and Resilience Testing

<!--
difficulty: intermediate
concepts: [fault-injection, delay-injection, abort-injection, chaos-engineering, resilience-testing]
tools: [kubectl, istioctl]
estimated_time: 25m
bloom_level: apply
prerequisites: [istio-traffic-management, virtualservice, destinationrule]
-->

## What You Will Learn

In this exercise you will use Istio's fault injection capabilities to test
application resilience. You will inject HTTP delays and aborts into service
communication without modifying application code, and observe how upstream
services react to downstream failures.

## Step-by-Step

### 1 -- Setup

```bash
kubectl create namespace fault-lab
kubectl label namespace fault-lab istio-injection=enabled
```

### 2 -- Deploy Services

```yaml
# services.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: httpbin
  namespace: fault-lab
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
  namespace: fault-lab
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
  name: sleep
  namespace: fault-lab
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

### 3 -- Inject a Fixed Delay

Add a 3-second delay to 50% of requests:

```yaml
# fault-delay.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: fault-lab
spec:
  hosts:
    - httpbin
  http:
    - fault:
        delay:
          percentage:
            value: 50.0        # 50% of requests are delayed
          fixedDelay: 3s       # 3 second delay
      route:
        - destination:
            host: httpbin
```

### 4 -- Inject HTTP Aborts

Return 503 for 30% of requests:

```yaml
# fault-abort.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: fault-lab
spec:
  hosts:
    - httpbin
  http:
    - fault:
        abort:
          percentage:
            value: 30.0        # 30% of requests get an error
          httpStatus: 503      # HTTP 503 Service Unavailable
      route:
        - destination:
            host: httpbin
```

### 5 -- Combined: Delay + Abort

```yaml
# fault-combined.yaml
apiVersion: networking.istio.io/v1
kind: VirtualService
metadata:
  name: httpbin
  namespace: fault-lab
spec:
  hosts:
    - httpbin
  http:
    - fault:
        delay:
          percentage:
            value: 50.0
          fixedDelay: 2s
        abort:
          percentage:
            value: 20.0
          httpStatus: 500
      route:
        - destination:
            host: httpbin
```

### 6 -- Apply and Test

```bash
kubectl apply -f services.yaml

# Test delay injection
kubectl apply -f fault-delay.yaml
for i in $(seq 1 10); do
  time kubectl exec -n fault-lab deploy/sleep -c sleep -- curl -s -o /dev/null -w "%{http_code} %{time_total}s\n" http://httpbin/get
done

# Test abort injection
kubectl apply -f fault-abort.yaml
for i in $(seq 1 20); do
  kubectl exec -n fault-lab deploy/sleep -c sleep -- curl -s -o /dev/null -w "%{http_code}\n" http://httpbin/get
done | sort | uniq -c
```

## Verify

```bash
# VirtualService is applied
kubectl get virtualservice -n fault-lab

# Delay injection: some requests take ~3s
kubectl apply -f fault-delay.yaml
kubectl exec -n fault-lab deploy/sleep -c sleep -- curl -s -o /dev/null -w "time: %{time_total}s code: %{http_code}\n" http://httpbin/get

# Abort injection: some requests return 503
kubectl apply -f fault-abort.yaml
for i in $(seq 1 10); do
  kubectl exec -n fault-lab deploy/sleep -c sleep -- curl -s -o /dev/null -w "%{http_code}\n" http://httpbin/get
done | sort | uniq -c
# Expect ~70% 200, ~30% 503
```

## Cleanup

```bash
kubectl delete namespace fault-lab
```

## What's Next

Continue to [17.05 Istio Circuit Breaking](../05-istio-circuit-breaking/) to
learn how to protect services from cascading failures.

## Summary

- Istio injects faults at the Envoy proxy level -- no application changes needed.
- `fault.delay` adds latency to simulate slow dependencies.
- `fault.abort` returns error codes to simulate service failures.
- Both support percentage-based injection for gradual testing.
- Fault injection is essential for validating retry logic, timeouts, and
  circuit breakers in a microservices architecture.

## References

- [Istio Fault Injection](https://istio.io/latest/docs/tasks/traffic-management/fault-injection/)
- [HTTPFaultInjection Reference](https://istio.io/latest/docs/reference/config/networking/virtual-service/#HTTPFaultInjection)
