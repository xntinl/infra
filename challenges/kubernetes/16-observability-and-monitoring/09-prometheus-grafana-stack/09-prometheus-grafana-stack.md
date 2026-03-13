# 16.09 Prometheus and Grafana Stack

<!--
difficulty: intermediate
concepts: [kube-prometheus-stack, prometheus-operator, grafana, promql, servicemonitor, prometheusrule]
tools: [kubectl, helm]
estimated_time: 35m
bloom_level: apply
prerequisites: [services, helm-basics, prometheus-servicemonitor]
-->

## What You Will Learn

In this exercise you will install the full kube-prometheus-stack, which includes
Prometheus Operator, Grafana with pre-built dashboards, node-exporter,
kube-state-metrics, and Alertmanager. You will deploy an application with
custom metrics, create a ServiceMonitor, write a PrometheusRule with an alert
and a recording rule, and explore dashboards in Grafana.

## Step-by-Step

### 1 -- Install kube-prometheus-stack

```bash
helm repo add prometheus-community https://prometheus-community.github.io/helm-charts
helm repo update

kubectl create namespace monitoring

helm install kube-prometheus prometheus-community/kube-prometheus-stack \
  --namespace monitoring \
  --set prometheus.prometheusSpec.serviceMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.podMonitorSelectorNilUsesHelmValues=false \
  --set prometheus.prometheusSpec.ruleSelectorNilUsesHelmValues=false \
  --wait
```

### 2 -- Deploy an Application with Metrics

```yaml
# app-namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: demo-app
```

```yaml
# app-with-metrics.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-app
  namespace: demo-app
  labels:
    app: metrics-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: metrics-app
  template:
    metadata:
      labels:
        app: metrics-app
    spec:
      containers:
        - name: app
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
              name: http
          volumeMounts:
            - name: nginx-config
              mountPath: /etc/nginx/conf.d/
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
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
            limits:
              cpu: 50m
              memory: 32Mi
      volumes:
        - name: nginx-config
          configMap:
            name: nginx-config
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nginx-config
  namespace: demo-app
data:
  default.conf: |
    server {
        listen 80;
        location / {
            return 200 'Hello from metrics-app\n';
            add_header Content-Type text/plain;
        }
        location /stub_status {
            stub_status;
            allow 127.0.0.1;
            deny all;
        }
    }
```

```yaml
# app-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: metrics-app
  namespace: demo-app
  labels:
    app: metrics-app
spec:
  selector:
    app: metrics-app
  ports:
    - name: http
      port: 80
      targetPort: 80
    - name: metrics
      port: 9113
      targetPort: 9113
```

### 3 -- Create a ServiceMonitor

```yaml
# servicemonitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: metrics-app
  namespace: demo-app
  labels:
    app: metrics-app
spec:
  selector:
    matchLabels:
      app: metrics-app
  endpoints:
    - port: metrics
      interval: 15s
      path: /metrics
  namespaceSelector:
    matchNames:
      - demo-app
```

### 4 -- Create a PrometheusRule

```yaml
# prometheusrule.yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: metrics-app-alerts
  namespace: demo-app
  labels:
    app: metrics-app
spec:
  groups:
    - name: metrics-app.rules
      rules:
        - alert: NginxHighConnectionCount
          expr: nginx_connections_active > 100
          for: 2m
          labels:
            severity: warning
          annotations:
            summary: "High active connections on nginx"
            description: "Pod {{ $labels.pod }} has {{ $value }} active connections."
        - alert: MetricsAppDown
          expr: up{job="metrics-app"} == 0
          for: 1m
          labels:
            severity: critical
          annotations:
            summary: "metrics-app target is down"
            description: "Target {{ $labels.instance }} has been down for over 1 minute."
        - record: nginx:connections:rate5m
          expr: rate(nginx_connections_accepted[5m])
```

### 5 -- Apply

```bash
kubectl apply -f app-namespace.yaml
kubectl apply -f app-with-metrics.yaml
kubectl apply -f app-service.yaml
kubectl apply -f servicemonitor.yaml
kubectl apply -f prometheusrule.yaml
```

## Verify

```bash
# 1. All monitoring components running
kubectl get pods -n monitoring

# 2. Access Prometheus UI
kubectl port-forward -n monitoring svc/kube-prometheus-prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep "metrics-app"

# 3. Run a PromQL query
curl -s 'http://localhost:9090/api/v1/query?query=nginx_connections_active' | python3 -m json.tool

# 4. Verify alert rule loaded
curl -s http://localhost:9090/api/v1/rules | python3 -m json.tool | grep "NginxHighConnectionCount"

# 5. Access Grafana
kubectl port-forward -n monitoring svc/kube-prometheus-grafana 3000:80 &
echo "Grafana: http://localhost:3000  user: admin"
kubectl get secret -n monitoring kube-prometheus-grafana -o jsonpath='{.data.admin-password}' | base64 -d; echo

# 6. List all ServiceMonitors and PrometheusRules
kubectl get servicemonitors -A
kubectl get prometheusrules -A
```

## Cleanup

```bash
kubectl delete namespace demo-app
helm uninstall kube-prometheus -n monitoring
kubectl delete namespace monitoring
```

## What's Next

Continue to [16.10 Custom Application Metrics](../10-custom-app-metrics/10-custom-app-metrics.md)
to learn how to instrument your own application with a `/metrics` endpoint.

## Summary

- kube-prometheus-stack provides a complete monitoring solution out of the box.
- ServiceMonitor and PrometheusRule are Kubernetes-native CRDs that configure
  Prometheus declaratively.
- Grafana ships with dashboards for node, pod, namespace, and cluster metrics.
- PromQL queries power both dashboards and alert conditions.
- Recording rules pre-compute expensive queries for dashboard performance.

## References

- [kube-prometheus-stack Chart](https://github.com/prometheus-community/helm-charts/tree/main/charts/kube-prometheus-stack)
- [Prometheus Operator](https://prometheus-operator.dev/)
- [PromQL Basics](https://prometheus.io/docs/prometheus/latest/querying/basics/)
