# 17.06 Istio Observability: Kiali, Jaeger, Prometheus

<!--
difficulty: intermediate
concepts: [kiali, jaeger, prometheus, istio-telemetry, service-graph, distributed-tracing]
tools: [kubectl, istioctl]
estimated_time: 30m
bloom_level: apply
prerequisites: [istio-installation-and-injection, istio-traffic-management]
-->

## What You Will Learn

In this exercise you will deploy Istio's observability addons -- Kiali for
service graph visualization, Jaeger for distributed tracing, and Prometheus for
metrics collection. You will generate traffic between services and explore the
telemetry that Istio produces automatically without any application
instrumentation.

## Step-by-Step

### 1 -- Install Observability Addons

```bash
# From the istio installation directory
kubectl apply -f samples/addons/prometheus.yaml
kubectl apply -f samples/addons/jaeger.yaml
kubectl apply -f samples/addons/kiali.yaml
kubectl apply -f samples/addons/grafana.yaml

# Or install individually
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/prometheus.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/jaeger.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/kiali.yaml
kubectl apply -f https://raw.githubusercontent.com/istio/istio/release-1.22/samples/addons/grafana.yaml

kubectl rollout status deployment/kiali -n istio-system --timeout=120s
```

### 2 -- Deploy a Multi-Service Application

```yaml
# microservices.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: observ-lab
  labels:
    istio-injection: enabled
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: observ-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frontend
  template:
    metadata:
      labels:
        app: frontend
    spec:
      containers:
        - name: frontend
          image: nginx:1.27-alpine
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
  name: frontend
  namespace: observ-lab
spec:
  selector:
    app: frontend
  ports:
    - name: http
      port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: backend
  namespace: observ-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: backend
  template:
    metadata:
      labels:
        app: backend
    spec:
      containers:
        - name: backend
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
  name: backend
  namespace: observ-lab
spec:
  selector:
    app: backend
  ports:
    - name: http
      port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: traffic-gen
  namespace: observ-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: traffic-gen
  template:
    metadata:
      labels:
        app: traffic-gen
    spec:
      containers:
        - name: gen
          image: curlimages/curl:8.7.1
          command: ["sh", "-c"]
          args:
            - |
              while true; do
                curl -s http://frontend/ > /dev/null
                curl -s http://backend/get > /dev/null
                curl -s http://backend/headers > /dev/null
                sleep 1
              done
```

### 3 -- Apply and Generate Traffic

```bash
kubectl apply -f microservices.yaml
sleep 30  # wait for traffic to accumulate
```

### 4 -- Access the Dashboards

```bash
# Kiali -- service graph
istioctl dashboard kiali &
# Or: kubectl port-forward -n istio-system svc/kiali 20001:20001 &

# Jaeger -- distributed tracing
istioctl dashboard jaeger &
# Or: kubectl port-forward -n istio-system svc/tracing 16686:80 &

# Grafana -- Istio metrics dashboards
istioctl dashboard grafana &
# Or: kubectl port-forward -n istio-system svc/grafana 3000:3000 &

# Prometheus -- raw metrics
kubectl port-forward -n istio-system svc/prometheus 9090:9090 &
```

### 5 -- Explore Istio Metrics in Prometheus

```bash
# Request count by service
curl -s 'http://localhost:9090/api/v1/query?query=istio_requests_total{destination_service_namespace="observ-lab"}' | python3 -m json.tool

# Request duration (p99)
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.99,sum(rate(istio_request_duration_milliseconds_bucket{destination_service_namespace="observ-lab"}[5m]))by(le,destination_service_name))' | python3 -m json.tool

# TCP bytes sent
curl -s 'http://localhost:9090/api/v1/query?query=istio_tcp_sent_bytes_total{destination_service_namespace="observ-lab"}' | python3 -m json.tool
```

## Verify

```bash
# Addons running
kubectl get pods -n istio-system | grep -E "kiali|jaeger|prometheus|grafana"

# Istio metrics available
curl -s 'http://localhost:9090/api/v1/query?query=istio_requests_total' | python3 -m json.tool | head -20

# Kiali service graph populated
curl -s "http://localhost:20001/kiali/api/namespaces/observ-lab/graph?graphType=versionedApp" | python3 -m json.tool | head -20

# Jaeger traces available
curl -s "http://localhost:16686/api/services" | python3 -m json.tool
```

## Cleanup

```bash
kubectl delete namespace observ-lab
kubectl delete -f samples/addons/ 2>/dev/null
```

## What's Next

Continue to [17.07 Istio Security: mTLS and AuthorizationPolicy](../07-istio-security-mtls/07-istio-security-mtls.md)
to learn how to enforce mutual TLS and access control policies.

## Summary

- Istio generates metrics, traces, and access logs from Envoy sidecars automatically.
- Kiali visualizes the service graph showing traffic flow, health, and configuration.
- Jaeger displays distributed traces across services without application instrumentation.
- Prometheus collects `istio_requests_total`, `istio_request_duration_milliseconds`, and TCP metrics.
- Grafana Istio dashboards show per-service, per-workload, and mesh-wide views.

## References

- [Istio Observability](https://istio.io/latest/docs/tasks/observability/)
- [Kiali Documentation](https://kiali.io/docs/)
- [Istio Standard Metrics](https://istio.io/latest/docs/reference/config/metrics/)
