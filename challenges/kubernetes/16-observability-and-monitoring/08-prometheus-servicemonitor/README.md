# 16.08 Prometheus ServiceMonitor and PodMonitor

<!--
difficulty: intermediate
concepts: [servicemonitor, podmonitor, prometheus-operator, label-selectors, scrape-config]
tools: [kubectl, helm]
estimated_time: 25m
bloom_level: apply
prerequisites: [services, labels, prometheus-basics]
-->

## What You Will Learn

In this exercise you will use the Prometheus Operator CRDs -- `ServiceMonitor`
and `PodMonitor` -- to declaratively configure Prometheus scrape targets. You
will deploy an application with a metrics endpoint, create both monitor types,
and verify that Prometheus discovers and scrapes them automatically.

## Step-by-Step

### 1 -- Prerequisites

This exercise requires the Prometheus Operator. If you have not already
installed it, use the kube-prometheus-stack Helm chart:

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update
helm install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring --create-namespace \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
  --wait
```

### 2 -- Deploy an Application with Metrics

```yaml
# app.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: monitor-lab
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
  namespace: monitor-lab
spec:
  replicas: 2
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
        version: v1
    spec:
      containers:
        - name: nginx
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
              name: http
        - name: exporter
          image: nginx/nginx-prometheus-exporter:1.1
          args: ["-nginx.scrape-uri=http://localhost/stub_status"]
          ports:
            - containerPort: 9113
              name: metrics
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-config
  namespace: monitor-lab
data:
  default.conf: |
    server {
      listen 80;
      location /stub_status { stub_status; allow 127.0.0.1; deny all; }
      location / { return 200 'ok\n'; }
    }
---
apiVersion: v1
kind: Service
metadata:
  name: web-app
  namespace: monitor-lab
  labels:
    app: web-app
spec:
  selector:
    app: web-app
  ports:
    - name: http
      port: 80
      targetPort: 80
    - name: metrics
      port: 9113
      targetPort: 9113
```

### 3 -- Create a ServiceMonitor

A ServiceMonitor selects Services by label and tells Prometheus which port and
path to scrape.

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: web-app
  namespace: monitor-lab
  labels:
    app: web-app
spec:
  selector:
    matchLabels:
      app: web-app           # must match Service labels
  endpoints:
    - port: metrics           # must match the Service port name
      interval: 15s
      path: /metrics
  namespaceSelector:
    matchNames:
      - monitor-lab
```

### 4 -- Create a PodMonitor

A PodMonitor selects Pods directly, bypassing Services. Useful for batch jobs
or sidecars.

```yaml
# podmonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: PodMonitor
metadata:
  name: web-app-pods
  namespace: monitor-lab
  labels:
    app: web-app
spec:
  selector:
    matchLabels:
      app: web-app           # must match Pod labels
  podMetricsEndpoints:
    - port: metrics
      interval: 30s
      path: /metrics
  namespaceSelector:
    matchNames:
      - monitor-lab
```

### 5 -- Apply

```bash
kubectl apply -f app.yaml
kubectl apply -f servicemonitor.yaml
kubectl apply -f podmonitor.yaml
```

### 6 -- Spot the Bug

This ServiceMonitor never gets scraped. Why?

```yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: broken-monitor
  namespace: monitor-lab
spec:
  selector:
    matchLabels:
      app: web-app
  endpoints:
    - port: http              # TODO: wrong port name -- should be "metrics"
      interval: 15s
```

<details>
<summary>Answer</summary>

The `port: http` points to port 80 (the nginx web server), which does not serve
`/metrics`. The correct port name is `metrics` (port 9113). The ServiceMonitor
itself will appear in Prometheus targets, but every scrape returns an error.
</details>

## Verify

```bash
# 1. Check that monitors were created
kubectl get servicemonitors -n monitor-lab
kubectl get podmonitors -n monitor-lab

# 2. Port-forward to Prometheus and check targets
kubectl port-forward -n monitoring svc/kube-prometheus-prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep "monitor-lab"

# 3. Query a metric from the exporter
curl -s 'http://localhost:9090/api/v1/query?query=nginx_connections_active' | python3 -m json.tool
```

## Cleanup

```bash
kubectl delete namespace monitor-lab
# Optionally remove the monitoring stack:
# helm uninstall kube-prometheus -n monitoring
# kubectl delete namespace monitoring
```

## What's Next

Continue to [16.09 Prometheus and Grafana Stack](../09-prometheus-grafana-stack/)
to explore the full monitoring stack including dashboards and alerting.

## Summary

- `ServiceMonitor` discovers scrape targets via Service labels and port names.
- `PodMonitor` discovers targets directly from Pod labels, no Service required.
- Both CRDs are managed by the Prometheus Operator and auto-configure Prometheus.
- The `selector.matchLabels` must match the target's labels exactly.
- The `port` field references a named port, not a number.

## References

- [Prometheus Operator -- ServiceMonitor](https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.ServiceMonitor)
- [Prometheus Operator -- PodMonitor](https://prometheus-operator.dev/docs/api-reference/api/#monitoring.coreos.com/v1.PodMonitor)
