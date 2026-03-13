# 16.12 OpenTelemetry Collector: Pipelines and Processors

<!--
difficulty: advanced
concepts: [otel-collector, pipelines, processors, receivers, exporters, tail-sampling, attributes-processor]
tools: [kubectl]
estimated_time: 30m
bloom_level: analyze
prerequisites: [distributed-tracing-opentelemetry, configmaps, deployments]
-->

## Architecture

```
                         OpenTelemetry Collector
+-----------+     +--------------------------------------------------+     +-----------+
| App A     |     |  Receivers     Processors       Exporters        |     | Jaeger    |
| (OTLP)   |---->|  otlp     -->  memory_limiter                    |---->| (traces)  |
+-----------+     |               --> attributes                     |     +-----------+
                  |               --> filter                         |
+-----------+     |               --> batch        --> otlp/jaeger   |     +-----------+
| App B     |---->|                                --> prometheus    |---->| Prometheus|
| (OTLP)   |     |  prometheus -->  batch         --> prometheusrw  |     | (metrics) |
+-----------+     +--------------------------------------------------+     +-----------+
```

The Collector supports multiple pipelines (traces, metrics, logs), each with
independent receivers, processors, and exporters. Processors transform,
filter, enrich, and sample telemetry data in-flight.

## What You Will Build

- A Collector with multiple pipelines (traces + metrics)
- Processors: `attributes` (add/modify fields), `filter` (drop unwanted spans),
  `batch`, and `memory_limiter`
- Multiple exporters for different backends
- Verification that processors transform data correctly

## Suggested Steps

1. Create namespace `otel-pipelines`.

2. Deploy Jaeger for trace storage (same as exercise 16.11).

3. Create a Collector config with multiple processors:

```yaml
# collector-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: otel-collector-config
  namespace: otel-pipelines
data:
  config.yaml: |
    receivers:
      otlp:
        protocols:
          grpc:
            endpoint: 0.0.0.0:4317
          http:
            endpoint: 0.0.0.0:4318

    processors:
      memory_limiter:
        check_interval: 5s
        limit_mib: 256
        spike_limit_mib: 64

      batch:
        timeout: 5s
        send_batch_size: 512

      attributes/add-env:
        actions:
          - key: deployment.environment
            value: staging
            action: upsert
          - key: collector.version
            value: "0.100.0"
            action: insert

      filter/drop-health:
        error_mode: ignore
        traces:
          span:
            - 'name == "health-check"'
            - 'attributes["http.target"] == "/healthz"'

      resource/add-cluster:
        attributes:
          - key: k8s.cluster.name
            value: dev-cluster
            action: upsert

    exporters:
      otlp/jaeger:
        endpoint: jaeger.otel-pipelines.svc.cluster.local:4317
        tls:
          insecure: true

      debug:
        verbosity: normal

      prometheus:
        endpoint: 0.0.0.0:8889
        namespace: otel

    service:
      telemetry:
        metrics:
          address: 0.0.0.0:8888

      pipelines:
        traces:
          receivers: [otlp]
          processors: [memory_limiter, filter/drop-health, attributes/add-env, resource/add-cluster, batch]
          exporters: [otlp/jaeger, debug]

        metrics:
          receivers: [otlp]
          processors: [memory_limiter, resource/add-cluster, batch]
          exporters: [prometheus, debug]
```

4. Deploy the Collector Deployment and Service exposing ports 4317, 4318,
   8888 (internal metrics), and 8889 (Prometheus exporter).

5. Deploy a trace generator that sends both health-check spans (should be
   filtered) and real spans (should be enriched with environment and cluster
   attributes).

6. Verify the filter processor drops health-check spans.

7. Verify the attributes processor adds `deployment.environment` to all spans.

## Verify

```bash
# Collector is running
kubectl get pods -n otel-pipelines -l app=otel-collector

# Check Collector logs for debug output showing enriched spans
kubectl logs -n otel-pipelines -l app=otel-collector --tail=30

# Verify health-check spans are NOT in Jaeger
kubectl port-forward -n otel-pipelines svc/jaeger 16686:16686 &
curl -s "http://localhost:16686/api/traces?service=demo-service&limit=20" | python3 -m json.tool | grep "health-check"
# Should return empty

# Verify environment attribute is present
curl -s "http://localhost:16686/api/traces?service=demo-service&limit=1" | python3 -m json.tool | grep "deployment.environment"

# Check Collector own metrics
kubectl port-forward -n otel-pipelines svc/otel-collector 8888:8888 &
curl -s http://localhost:8888/metrics | grep otelcol_processor_accepted_spans

# Check exported Prometheus metrics
kubectl port-forward -n otel-pipelines svc/otel-collector 8889:8889 &
curl -s http://localhost:8889/metrics | head -20
```

## Cleanup

```bash
kubectl delete namespace otel-pipelines
```

## References

- [Collector Configuration](https://opentelemetry.io/docs/collector/configuration/)
- [Attributes Processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/attributesprocessor)
- [Filter Processor](https://github.com/open-telemetry/opentelemetry-collector-contrib/tree/main/processor/filterprocessor)
- [Batch Processor](https://github.com/open-telemetry/opentelemetry-collector/tree/main/processor/batchprocessor)
