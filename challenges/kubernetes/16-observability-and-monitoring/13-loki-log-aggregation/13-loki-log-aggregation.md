# 16.13 Loki Log Aggregation and LogQL

<!--
difficulty: advanced
concepts: [loki, logql, promtail, log-aggregation, label-based-indexing, grafana-explore]
tools: [kubectl, helm]
estimated_time: 35m
bloom_level: analyze
prerequisites: [logging-with-fluentbit-daemonset, prometheus-grafana-stack]
-->

## Architecture

```
+-----------+    tail logs    +----------+    push     +-----------+    query    +-----------+
| Container |  /var/log/...   | Promtail | --------->  |   Loki    | <--------  | Grafana   |
| stdout/   |  ------------> | DaemonSet |             | (storage) |            | Explore   |
| stderr    |                 +----------+             +-----------+            +-----------+
+-----------+                                           index by                  LogQL
                                                        labels only
```

Loki indexes logs by labels (namespace, pod, container) rather than full-text,
making it orders of magnitude cheaper to run than Elasticsearch at scale.
Promtail collects logs from nodes and pushes them to Loki. Grafana Explore
queries Loki using LogQL.

## What You Will Build

- Loki deployed via Helm in single-binary mode
- Promtail DaemonSet for log collection
- Grafana data source configured to query Loki
- LogQL queries for filtering, aggregation, and pattern matching

## Suggested Steps

1. Create namespace `loki-lab`.

2. Install Loki + Promtail via Helm:

```bash
helm repo add grafana https://grafana.github.io/helm-charts
helm repo update

helm install loki grafana/loki-stack \
  --namespace loki-lab --create-namespace \
  --set loki.persistence.enabled=false \
  --set promtail.enabled=true \
  --set grafana.enabled=true \
  --set grafana.adminPassword=admin \
  --wait
```

3. Deploy a log-generating application in a separate namespace:

```yaml
# log-app.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: log-app
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: log-app
  namespace: log-app
spec:
  replicas: 3
  selector:
    matchLabels:
      app: log-app
  template:
    metadata:
      labels:
        app: log-app
        tier: backend
    spec:
      containers:
        - name: app
          image: busybox:1.37
          command: ["sh", "-c"]
          args:
            - |
              i=0
              while true; do
                i=$((i + 1))
                ts=$(date -u +%Y-%m-%dT%H:%M:%SZ)
                echo "{\"ts\":\"$ts\",\"level\":\"info\",\"msg\":\"request processed\",\"id\":$i,\"latency_ms\":$((RANDOM % 500))}"
                if [ $((i % 7)) -eq 0 ]; then
                  echo "{\"ts\":\"$ts\",\"level\":\"error\",\"msg\":\"upstream timeout\",\"id\":$i}" >&2
                fi
                sleep 2
              done
```

4. Access Grafana and add Loki as a data source (URL: `http://loki:3100`).

5. Run LogQL queries in Grafana Explore:

```logql
# All logs from the log-app namespace
{namespace="log-app"}

# Only error-level logs
{namespace="log-app"} |= "error"

# JSON parsing with field filter
{namespace="log-app"} | json | level="error"

# Log rate over time (requests per second)
rate({namespace="log-app"} | json | level="info" [1m])

# Top pods by error count
topk(5, count_over_time({namespace="log-app"} |= "error" [5m]))

# Latency percentile from structured logs
quantile_over_time(0.95, {namespace="log-app"} | json | unwrap latency_ms [5m]) by (pod)
```

6. Verify Promtail is scraping logs from all nodes.

## Verify

```bash
# Loki and Promtail Pods running
kubectl get pods -n loki-lab

# Promtail is collecting logs
kubectl logs -n loki-lab -l app=promtail --tail=10

# Access Grafana
kubectl port-forward -n loki-lab svc/loki-grafana 3000:80 &
echo "Grafana: http://localhost:3000 (admin/admin)"

# Query Loki API directly
kubectl port-forward -n loki-lab svc/loki 3100:3100 &
curl -s 'http://localhost:3100/loki/api/v1/query?query={namespace="log-app"}&limit=5' | python3 -m json.tool

# Check Loki labels
curl -s http://localhost:3100/loki/api/v1/labels | python3 -m json.tool
```

## Cleanup

```bash
helm uninstall loki -n loki-lab
kubectl delete namespace loki-lab
kubectl delete namespace log-app
```

## References

- [Loki Documentation](https://grafana.com/docs/loki/latest/)
- [LogQL Reference](https://grafana.com/docs/loki/latest/query/)
- [Promtail Configuration](https://grafana.com/docs/loki/latest/send-data/promtail/)
- [Grafana Explore](https://grafana.com/docs/grafana/latest/explore/)
