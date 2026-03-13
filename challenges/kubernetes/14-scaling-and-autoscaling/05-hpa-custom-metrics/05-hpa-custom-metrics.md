<!--
difficulty: intermediate
concepts: [custom-metrics, prometheus-adapter, servicemonitor, hpa-pods-metric, promql, keda]
tools: [kubectl, prometheus, prometheus-adapter]
estimated_time: 40m
bloom_level: apply
prerequisites: [hpa-cpu-memory-autoscaling, prometheus-basics]
-->

# 14.05 - HPA with Custom Metrics from Prometheus

## Why This Matters

CPU and memory utilization are lagging indicators. A web service might be running at 20% CPU but have a 5-second response time because it is waiting on database queries. Scaling on **application-level metrics** -- requests per second, queue depth, latency percentiles -- lets the HPA react to the signals that actually matter to your users.

## What You Will Learn

- How Prometheus Adapter bridges Prometheus metrics to the Kubernetes custom metrics API
- How to write adapter rules that convert PromQL results into per-pod metrics
- How to configure an HPA with `type: Pods` custom metrics
- How KEDA provides an alternative with its ScaledObject approach

## Guide

### 1. Create Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: custom-metrics-lab
```

### 2. Deploy an Application That Exposes Prometheus Metrics

```yaml
# app-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: metrics-app
  namespace: custom-metrics-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: metrics-app
  template:
    metadata:
      labels:
        app: metrics-app
      annotations:
        prometheus.io/scrape: "true"
        prometheus.io/port: "8080"
        prometheus.io/path: "/metrics"
    spec:
      containers:
        - name: app
          image: quay.io/brancz/prometheus-example-app:v0.5.0
          ports:
            - name: http
              containerPort: 8080
          resources:
            requests:
              cpu: 100m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: metrics-app
  namespace: custom-metrics-lab
  labels:
    app: metrics-app
spec:
  selector:
    app: metrics-app
  ports:
    - name: http
      port: 8080
      targetPort: 8080
```

### 3. ServiceMonitor for Prometheus Operator

```yaml
# service-monitor.yaml
apiVersion: monitoring.coreos.com/v1
kind: ServiceMonitor
metadata:
  name: metrics-app-monitor
  namespace: custom-metrics-lab
  labels:
    app: metrics-app
spec:
  selector:
    matchLabels:
      app: metrics-app
  endpoints:
    - port: http
      interval: 15s
      path: /metrics
```

### 4. Prometheus Adapter Configuration

```yaml
# prometheus-adapter-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: prometheus-adapter-config
  namespace: monitoring
data:
  config.yaml: |
    rules:
      - seriesQuery: 'http_requests_total{namespace!="",pod!=""}'
        resources:
          overrides:
            namespace:
              resource: namespace
            pod:
              resource: pod
        name:
          matches: "^(.*)_total$"
          as: "${1}_per_second"
        metricsQuery: 'sum(rate(<<.Series>>{<<.LabelMatchers>>}[2m])) by (<<.GroupBy>>)'
```

### 5. Prometheus Adapter Deployment

```yaml
# prometheus-adapter-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: prometheus-adapter
  namespace: monitoring
spec:
  replicas: 1
  selector:
    matchLabels:
      app: prometheus-adapter
  template:
    metadata:
      labels:
        app: prometheus-adapter
    spec:
      serviceAccountName: prometheus-adapter
      containers:
        - name: prometheus-adapter
          image: registry.k8s.io/prometheus-adapter/prometheus-adapter:v0.11.2
          args:
            - --prometheus-url=http://prometheus.monitoring.svc:9090
            - --metrics-relist-interval=30s
            - --config=/etc/adapter/config.yaml
            - --secure-port=6443
          ports:
            - containerPort: 6443
          volumeMounts:
            - name: config
              mountPath: /etc/adapter
              readOnly: true
      volumes:
        - name: config
          configMap:
            name: prometheus-adapter-config
---
apiVersion: v1
kind: Service
metadata:
  name: prometheus-adapter
  namespace: monitoring
spec:
  selector:
    app: prometheus-adapter
  ports:
    - port: 443
      targetPort: 6443
```

### 6. Register the APIService

```yaml
# apiservice.yaml
apiVersion: apiregistration.k8s.io/v1
kind: APIService
metadata:
  name: v1beta1.custom.metrics.k8s.io
spec:
  service:
    name: prometheus-adapter
    namespace: monitoring
  group: custom.metrics.k8s.io
  version: v1beta1
  insecureSkipTLSVerify: true
  groupPriorityMinimum: 100
  versionPriority: 100
```

### 7. HPA with Custom Metric

```yaml
# hpa-custom.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: metrics-app-hpa
  namespace: custom-metrics-lab
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: metrics-app
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: Pods
      pods:
        metric:
          name: http_requests_per_second
        target:
          type: AverageValue
          averageValue: "10"
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 30
      policies:
        - type: Pods
          value: 2
          periodSeconds: 60
    scaleDown:
      stabilizationWindowSeconds: 120
      policies:
        - type: Pods
          value: 1
          periodSeconds: 60
```

### 8. Alternative: KEDA ScaledObject

```yaml
# keda-scaledobject.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: metrics-app-scaledobject
  namespace: custom-metrics-lab
spec:
  scaleTargetRef:
    name: metrics-app
  pollingInterval: 15
  cooldownPeriod: 120
  minReplicaCount: 1
  maxReplicaCount: 10
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.monitoring.svc:9090
        metricName: http_requests_per_second
        query: sum(rate(http_requests_total{namespace="custom-metrics-lab",deployment="metrics-app"}[2m]))
        threshold: "10"
```

### 9. Load Generator

```yaml
# load-generator.yaml
apiVersion: v1
kind: Pod
metadata:
  name: load-generator
  namespace: custom-metrics-lab
spec:
  containers:
    - name: hey
      image: williamyeh/hey:latest
      command:
        - /bin/sh
        - -c
        - "while true; do hey -z 30s -c 50 -q 10 http://metrics-app:8080/; sleep 5; done"
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f app-deployment.yaml
kubectl apply -f service-monitor.yaml
kubectl apply -f prometheus-adapter-config.yaml
kubectl apply -f prometheus-adapter-deployment.yaml
kubectl apply -f apiservice.yaml

# Wait for the app to be ready
kubectl wait --for=condition=ready pod -l app=metrics-app -n custom-metrics-lab --timeout=120s

kubectl apply -f hpa-custom.yaml
```

## Verify

```bash
# 1. Confirm the app exposes metrics
kubectl port-forward -n custom-metrics-lab svc/metrics-app 8080:8080 &
curl -s http://localhost:8080/metrics | grep http_requests_total
kill %1

# 2. Verify the custom metrics API is available
kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1" | python3 -m json.tool

# 3. Query the custom metric
kubectl get --raw "/apis/custom.metrics.k8s.io/v1beta1/namespaces/custom-metrics-lab/pods/*/http_requests_per_second" | python3 -m json.tool

# 4. Check HPA status
kubectl get hpa -n custom-metrics-lab
kubectl describe hpa metrics-app-hpa -n custom-metrics-lab

# 5. Generate load
kubectl apply -f load-generator.yaml

# 6. Watch scaling (1-2 minutes)
kubectl get hpa -n custom-metrics-lab --watch

# 7. Check replicas
kubectl get pods -n custom-metrics-lab -l app=metrics-app
kubectl get deployment metrics-app -n custom-metrics-lab

# 8. Stop load and observe scale-down
kubectl delete pod load-generator -n custom-metrics-lab
kubectl get hpa -n custom-metrics-lab --watch
```

## Cleanup

```bash
kubectl delete namespace custom-metrics-lab
```

## What's Next

Custom metrics from Prometheus are powerful but require running Prometheus in-cluster. The next exercise covers scaling from external metrics sources like cloud provider queues and monitoring services: [14.06 - HPA with External Metrics (SQS, CloudWatch)](../06-hpa-external-metrics/06-hpa-external-metrics.md).

## Summary

- Prometheus Adapter translates PromQL queries into the Kubernetes custom.metrics.k8s.io API
- Adapter rules map Prometheus series to Kubernetes resources (namespace, pod)
- HPA `type: Pods` metrics use `AverageValue` targets for per-pod custom metrics
- KEDA provides an alternative that queries Prometheus directly via ScaledObject triggers
- Application-level metrics (RPS, latency) are better scaling signals than CPU utilization
