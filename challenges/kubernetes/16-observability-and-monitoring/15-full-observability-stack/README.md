# 16.15 Full Observability Stack: Metrics, Logs, Traces

<!--
difficulty: insane
concepts: [metrics, logs, traces, prometheus, loki, tempo, grafana, opentelemetry-collector, correlation, exemplars]
tools: [kubectl, helm]
estimated_time: 60m
bloom_level: create
prerequisites: [prometheus-grafana-stack, loki-log-aggregation, distributed-tracing-opentelemetry, opentelemetry-collector]
-->

## Scenario

Your team is adopting a unified observability platform. The CTO wants a single
Grafana instance where engineers can jump from a metric anomaly to the
correlated logs to the specific trace that caused it. You must deploy a
production-grade observability stack covering all three pillars -- metrics,
logs, and traces -- with cross-signal correlation.

## Constraints

1. Deploy Prometheus for metrics, Loki for logs, and Grafana Tempo for traces.
2. Deploy a single OpenTelemetry Collector that receives OTLP for all three
   signal types and routes them to the correct backend.
3. Configure Grafana with all three data sources and enable trace-to-logs and
   trace-to-metrics links.
4. Deploy a multi-service demo application (at least 2 services communicating
   over HTTP) that generates all three signal types.
5. Application logs must include `traceID` so Loki can correlate to Tempo.
6. Prometheus must have exemplars enabled to link metrics to specific traces.
7. All components must have resource requests/limits set.
8. The Collector must have `memory_limiter`, `batch`, and `resource` processors.

## Success Criteria

1. Grafana Explore shows metrics from Prometheus with functioning PromQL queries.
2. Grafana Explore shows logs from Loki with LogQL queries that filter by
   `traceID`.
3. Grafana Explore shows traces from Tempo with trace search by service name.
4. Clicking a trace ID in Loki jumps to the trace view in Tempo.
5. Clicking an exemplar on a Prometheus metric panel jumps to the associated trace.
6. The OTel Collector internal metrics show data flowing through all three
   pipelines (traces, metrics, logs).
7. Deleting a Pod in the demo application generates observable events across all
   three signals.

## Hints

- Use `grafana/tempo` Helm chart for Tempo in single-binary mode.
- Use `grafana/loki-stack` or `grafana/loki` for Loki.
- Use `prometheus-community/kube-prometheus-stack` for Prometheus and Grafana.
- Configure Grafana data source provisioning via Helm values to set up
  derived fields (Loki -> Tempo) and exemplar data source (Prometheus -> Tempo).
- The OTel Collector config needs three pipelines in the `service` block:

```yaml
service:
  pipelines:
    traces:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [otlp/tempo]
    metrics:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [prometheusremotewrite]
    logs:
      receivers: [otlp]
      processors: [memory_limiter, resource, batch]
      exporters: [loki]
```

## Verification Commands

```bash
# All Pods running
kubectl get pods -n observability
kubectl get pods -n demo-app

# Prometheus targets healthy
kubectl port-forward -n observability svc/prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/targets | python3 -m json.tool | grep -c '"health":"up"'

# Loki receiving logs
kubectl port-forward -n observability svc/loki 3100:3100 &
curl -s 'http://localhost:3100/loki/api/v1/query?query={namespace="demo-app"}&limit=3'

# Tempo receiving traces
kubectl port-forward -n observability svc/tempo 3200:3200 &
curl -s http://localhost:3200/api/search?q='{}'

# Grafana accessible
kubectl port-forward -n observability svc/grafana 3000:80 &
curl -s http://localhost:3000/api/health

# Collector pipeline metrics
kubectl exec -n observability deploy/otel-collector -- wget -qO- http://localhost:8888/metrics | grep otelcol_receiver_accepted
```

## Cleanup

```bash
kubectl delete namespace demo-app
helm uninstall prometheus -n observability 2>/dev/null
helm uninstall loki -n observability 2>/dev/null
helm uninstall tempo -n observability 2>/dev/null
kubectl delete namespace observability
```
