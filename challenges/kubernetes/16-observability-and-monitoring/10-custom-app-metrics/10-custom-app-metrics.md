# 16.10 Custom Application Metrics with /metrics Endpoint

<!--
difficulty: advanced
concepts: [prometheus-exposition-format, counter, gauge, histogram, custom-metrics, instrumentation]
tools: [kubectl, curl]
estimated_time: 30m
bloom_level: analyze
prerequisites: [prometheus-grafana-stack, servicemonitor, deployments]
-->

## Architecture

```
+------------------+       +-----------------+       +------------------+
| Application Pod  |       | Prometheus       |       | Grafana          |
|                  |       |                  |       |                  |
| /metrics  :8080 ------>  | scrape via      ------>  | dashboard with   |
| counter, gauge,  |       | ServiceMonitor   |       | custom panels    |
| histogram        |       |                  |       |                  |
+------------------+       +-----------------+       +------------------+
```

An application exposes metrics in Prometheus exposition format on a `/metrics`
endpoint. Prometheus scrapes it via a ServiceMonitor. You query the metrics
with PromQL and build Grafana panels.

## What You Will Build

- A ConfigMap-based "application" that serves Prometheus-format metrics using a
  shell script and netcat
- A ServiceMonitor to scrape those metrics
- PromQL queries demonstrating counters, gauges, and histograms
- A Grafana dashboard JSON (optional import)

## Suggested Steps

1. Create a namespace `custom-metrics-lab`.

2. Deploy a Pod that exposes a `/metrics` endpoint returning Prometheus text format:

```yaml
# metrics-app.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: metrics-script
  namespace: custom-metrics-lab
data:
  serve.sh: |
    #!/bin/sh
    COUNTER=0
    while true; do
      COUNTER=$((COUNTER + 1))
      UPTIME=$(awk '{print $1}' /proc/uptime 2>/dev/null || echo "0")
      BODY="# HELP app_requests_total Total number of requests.
    # TYPE app_requests_total counter
    app_requests_total{method=\"GET\",path=\"/api\"} $COUNTER
    app_requests_total{method=\"POST\",path=\"/api\"} $((COUNTER / 3))
    # HELP app_active_connections Current active connections.
    # TYPE app_active_connections gauge
    app_active_connections $((RANDOM % 50))
    # HELP app_request_duration_seconds Request latency histogram.
    # TYPE app_request_duration_seconds histogram
    app_request_duration_seconds_bucket{le=\"0.05\"} $((COUNTER * 8))
    app_request_duration_seconds_bucket{le=\"0.1\"} $((COUNTER * 9))
    app_request_duration_seconds_bucket{le=\"0.25\"} $((COUNTER * 10))
    app_request_duration_seconds_bucket{le=\"+Inf\"} $((COUNTER * 10))
    app_request_duration_seconds_sum $((COUNTER * 2))
    app_request_duration_seconds_count $((COUNTER * 10))
    # HELP app_uptime_seconds Application uptime.
    # TYPE app_uptime_seconds gauge
    app_uptime_seconds $UPTIME"
      RESPONSE="HTTP/1.1 200 OK\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Length: $(echo "$BODY" | wc -c)\r\n\r\n$BODY"
      echo -e "$RESPONSE" | nc -l -p 8080 -q 1 > /dev/null 2>&1
    done
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-metrics-app
  namespace: custom-metrics-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: custom-metrics-app
  template:
    metadata:
      labels:
        app: custom-metrics-app
    spec:
      containers:
        - name: app
          image: busybox:1.37
          command: ["sh", "/scripts/serve.sh"]
          ports:
            - containerPort: 8080
              name: metrics
          volumeMounts:
            - name: scripts
              mountPath: /scripts
          resources:
            requests:
              cpu: 20m
              memory: 16Mi
            limits:
              cpu: 100m
              memory: 32Mi
      volumes:
        - name: scripts
          configMap:
            name: metrics-script
            defaultMode: 0755
---
apiVersion: v1
kind: Service
metadata:
  name: custom-metrics-app
  namespace: custom-metrics-lab
  labels:
    app: custom-metrics-app
spec:
  selector:
    app: custom-metrics-app
  ports:
    - name: metrics
      port: 8080
      targetPort: 8080
```

3. Create a ServiceMonitor:

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: custom-metrics-app
  namespace: custom-metrics-lab
spec:
  selector:
    matchLabels:
      app: custom-metrics-app
  endpoints:
    - port: metrics
      interval: 10s
      path: /metrics
```

4. Apply and wait for Prometheus to discover the target.

5. Run PromQL queries to analyze the custom metrics:

```bash
# Rate of requests per second over the last 5 minutes
curl -s 'http://localhost:9090/api/v1/query?query=rate(app_requests_total[5m])'

# Current active connections
curl -s 'http://localhost:9090/api/v1/query?query=app_active_connections'

# 95th percentile latency from histogram
curl -s 'http://localhost:9090/api/v1/query?query=histogram_quantile(0.95,rate(app_request_duration_seconds_bucket[5m]))'
```

## Verify

```bash
# Target is discovered
kubectl port-forward -n monitoring svc/kube-prometheus-prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep "custom-metrics"

# Metrics are being scraped
curl -s 'http://localhost:9090/api/v1/query?query=app_requests_total' | python3 -m json.tool

# Raw metrics from the Pod
kubectl exec -n custom-metrics-lab deploy/custom-metrics-app -- wget -qO- http://localhost:8080/metrics
```

## Cleanup

```bash
kubectl delete namespace custom-metrics-lab
```

## References

- [Prometheus Exposition Formats](https://prometheus.io/docs/instrumenting/exposition_formats/)
- [Metric Types](https://prometheus.io/docs/concepts/metric_types/)
- [PromQL Functions](https://prometheus.io/docs/prometheus/latest/querying/functions/)
