# 16.11 Distributed Tracing with OpenTelemetry

<!--
difficulty: advanced
concepts: [opentelemetry, jaeger, traces, spans, otlp, collector]
tools: [kubectl, helm, curl]
estimated_time: 35m
bloom_level: analyze
prerequisites: [deployments, services, configmaps]
-->

## Architecture

```
+-------------------+     OTLP/HTTP     +-------------------+    OTLP/gRPC    +----------------+
| Trace Generator   |  ------------->   | OTel Collector    | ------------->  | Jaeger         |
| (curl + OTLP)     |                   | (Deployment)      |                 | (all-in-one)   |
+-------------------+                   | receivers:        |                 | UI :16686      |
                                        |   otlp            |                 +----------------+
                                        | processors:       |
                                        |   batch           |
                                        | exporters:        |
                                        |   otlp/jaeger     |
                                        +-------------------+
```

Applications send trace data via OTLP to the OpenTelemetry Collector, which
batches, processes, and exports to Jaeger for storage and visualization.

## What You Will Build

- Jaeger all-in-one for trace storage and UI
- OpenTelemetry Collector with receiver/processor/exporter pipeline
- A trace generator that sends OTLP spans
- Verification via Jaeger UI and API

## Suggested Steps

1. Create namespace `tracing`.

2. Deploy Jaeger all-in-one:

```yaml
# jaeger.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: jaeger
  namespace: tracing
  labels:
    app: jaeger
spec:
  replicas: 1
  selector:
    matchLabels:
      app: jaeger
  template:
    metadata:
      labels:
        app: jaeger
    spec:
      containers:
        - name: jaeger
          image: jaegertracing/all-in-one:1.57
          ports:
            - containerPort: 16686
              name: ui
            - containerPort: 4317
              name: otlp-grpc
            - containerPort: 4318
              name: otlp-http
          env:
            - name: COLLECTOR_OTLP_ENABLED
              value: "true"
          resources:
            requests:
              cpu: 100m
              memory: 256Mi
            limits:
              cpu: 500m
              memory: 512Mi
---
apiVersion: v1
kind: Service
metadata:
  name: jaeger
  namespace: tracing
spec:
  selector:
    app: jaeger
  ports:
    - name: ui
      port: 16686
      targetPort: 16686
    - name: otlp-grpc
      port: 4317
      targetPort: 4317
    - name: otlp-http
      port: 4318
      targetPort: 4318
```

3. Configure the OpenTelemetry Collector:

```yaml
# otel-collector-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: otel-collector-config
  namespace: tracing
data:
  otel-collector-config.yaml: |
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318

    processors:
      batch:
        timeout: 5s
        send_batch_size: 1024
      memory_limiter:
        check_interval: 5s
        limit_mib: 256
        spike_limit_mib: 64

    exporters:
      otlp/jaeger:
        endpoint: jaeger.tracing.svc.cluster.local:4317
        tls:
          insecure: true
      debug:
        verbosity: detailed

    service:
      pipelines:
        traces:
          receivers: [otlp]
          processors: [memory_limiter, batch]
          exporters: [otlp/jaeger, debug]
```

4. Deploy the Collector:

```yaml
# otel-collector.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: otel-collector
  namespace: tracing
  labels:
    app: otel-collector
spec:
  replicas: 1
  selector:
    matchLabels:
      app: otel-collector
  template:
    metadata:
      labels:
        app: otel-collector
    spec:
      containers:
        - name: collector
          image: otel/opentelemetry-collector-contrib:0.100.0
          args: ["--config=/etc/otel/otel-collector-config.yaml"]
          ports:
            - containerPort: 4317
              name: otlp-grpc
            - containerPort: 4318
              name: otlp-http
            - containerPort: 8888
              name: metrics
          volumeMounts:
            - name: config
              mountPath: /etc/otel
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
            limits:
              cpu: 500m
              memory: 256Mi
      volumes:
        - name: config
          configMap:
            name: otel-collector-config
---
apiVersion: v1
kind: Service
metadata:
  name: otel-collector
  namespace: tracing
spec:
  selector:
    app: otel-collector
  ports:
    - name: otlp-grpc
      port: 4317
    - name: otlp-http
      port: 4318
    - name: metrics
      port: 8888
```

5. Deploy a trace generator:

```yaml
# trace-generator.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: trace-generator
  namespace: tracing
spec:
  replicas: 1
  selector:
    matchLabels:
      app: trace-generator
  template:
    metadata:
      labels:
        app: trace-generator
    spec:
      containers:
        - name: generator
          image: curlimages/curl:8.7.1
          command: ["sh", "-c"]
          args:
            - |
              while true; do
                TRACE_ID=$(cat /dev/urandom | tr -dc 'a-f0-9' | head -c 32)
                SPAN_ID=$(cat /dev/urandom | tr -dc 'a-f0-9' | head -c 16)
                TIMESTAMP=$(date +%s)000000000
                curl -s -X POST http://otel-collector.tracing.svc.cluster.local:4318/v1/traces \
                  -H "Content-Type: application/json" \
                  -d "{
                    \"resourceSpans\": [{
                      \"resource\": {
                        \"attributes\": [{
                          \"key\": \"service.name\",
                          \"value\": { \"stringValue\": \"demo-service\" }
                        }]
                      },
                      \"scopeSpans\": [{
                        \"scope\": { \"name\": \"demo-tracer\", \"version\": \"1.0.0\" },
                        \"spans\": [{
                          \"traceId\": \"$TRACE_ID\",
                          \"spanId\": \"$SPAN_ID\",
                          \"name\": \"HTTP GET /api/data\",
                          \"kind\": 2,
                          \"startTimeUnixNano\": \"$TIMESTAMP\",
                          \"endTimeUnixNano\": \"$((TIMESTAMP + 150000000))\",
                          \"attributes\": [
                            { \"key\": \"http.method\", \"value\": { \"stringValue\": \"GET\" } },
                            { \"key\": \"http.url\", \"value\": { \"stringValue\": \"/api/data\" } },
                            { \"key\": \"http.status_code\", \"value\": { \"intValue\": \"200\" } }
                          ],
                          \"status\": { \"code\": 1 }
                        }]
                      }]
                    }]
                  }" || true
                echo "[$(date)] Trace sent: $TRACE_ID"
                sleep 5
              done
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 50m
              memory: 32Mi
```

6. Apply all manifests and wait for Pods to be ready.

## Verify

```bash
# All Pods running
kubectl get pods -n tracing

# Collector receiving traces
kubectl logs -n tracing -l app=otel-collector --tail=20

# Generator sending traces
kubectl logs -n tracing -l app=trace-generator --tail=5

# Access Jaeger UI
kubectl port-forward -n tracing svc/jaeger 16686:16686 &
curl -s "http://localhost:16686/api/services" | python3 -m json.tool

# Query traces
curl -s "http://localhost:16686/api/traces?service=demo-service&limit=5" | python3 -m json.tool

# Collector internal metrics
kubectl exec -n tracing deploy/otel-collector -- wget -qO- http://localhost:8888/metrics 2>/dev/null | grep otelcol_receiver_accepted_spans
```

## Cleanup

```bash
kubectl delete namespace tracing
```

## References

- [OpenTelemetry Collector](https://opentelemetry.io/docs/collector/)
- [OpenTelemetry Traces Concepts](https://opentelemetry.io/docs/concepts/signals/traces/)
- [Jaeger Getting Started](https://www.jaegertracing.io/docs/getting-started/)
- [Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
