# 16.16 SLO-Based Alerting Platform Design

<!--
difficulty: insane
concepts: [slo, sli, error-budget, burn-rate-alerts, multi-window-alerting, prometheusrule, recording-rules, grafana-dashboards]
tools: [kubectl, helm]
estimated_time: 60m
bloom_level: create
prerequisites: [prometheus-grafana-stack, custom-app-metrics, prometheusrule]
-->

## Scenario

Your organization is moving from threshold-based alerting ("CPU > 80%") to
SLO-based alerting. You are tasked with building a platform that defines
Service Level Indicators (SLIs), sets Service Level Objectives (SLOs), tracks
error budgets, and fires alerts based on burn rate -- not raw thresholds. The
system must handle a multi-service environment with different SLOs per service.

## Constraints

1. Deploy at least two services, each exposing request count and latency metrics.
2. Define SLIs using Prometheus metrics:
   - **Availability SLI**: ratio of successful requests (non-5xx) to total requests.
   - **Latency SLI**: ratio of requests served under 300ms to total requests.
3. Set SLOs per service (e.g., Service A: 99.9% availability, 95% latency;
   Service B: 99.5% availability, 90% latency).
4. Create PrometheusRule recording rules that pre-compute:
   - SLI values over 5m, 30m, 1h, 6h, 1d, 3d windows.
   - Error budget remaining (1 - SLI / SLO over the compliance window).
   - Burn rate (rate of error budget consumption).
5. Create multi-window burn-rate alerts following Google SRE guidelines:
   - Page: 14.4x burn rate over 1h AND 14.4x over 5m.
   - Ticket: 6x burn rate over 6h AND 6x over 30m.
   - Low: 1x burn rate over 3d AND 1x over 6h.
6. Build a Grafana dashboard showing per-service: current SLI, SLO target,
   error budget remaining (%), burn rate trend, and alert status.
7. All recording rules and alert rules must be PrometheusRule CRDs, not manual
   Prometheus config.

## Success Criteria

1. `kubectl get prometheusrules -A` shows recording rules for both services.
2. PromQL query `slo:availability:ratio_rate5m{service="service-a"}` returns data.
3. PromQL query `slo:error_budget_remaining{service="service-a"}` returns a
   value between 0 and 1.
4. Under normal load, no burn-rate alerts fire.
5. Injecting 5xx errors into Service A causes the error budget to decrease and
   eventually fires the page-level burn-rate alert.
6. The Grafana dashboard correctly visualizes SLI, SLO line, error budget, and
   burn rate for both services.
7. Stopping the error injection shows the burn rate returning to normal without
   human intervention.

## Hints

- Multi-window burn-rate alert formula:

```promql
# Page alert: 14.4x burn rate (consumes 2% budget in 1h)
(
  1 - (sum(rate(http_requests_total{status!~"5.."}[1h])) / sum(rate(http_requests_total[1h])))
) / (1 - 0.999) > 14.4
AND
(
  1 - (sum(rate(http_requests_total{status!~"5.."}[5m])) / sum(rate(http_requests_total[5m])))
) / (1 - 0.999) > 14.4
```

- Recording rule pattern for SLI:

```yaml
- record: slo:availability:ratio_rate5m
  expr: |
    sum(rate(http_requests_total{status!~"5.."}[5m])) by (service)
    /
    sum(rate(http_requests_total[5m])) by (service)
```

- Error budget remaining:

```yaml
- record: slo:error_budget_remaining
  expr: |
    1 - (
      (1 - slo:availability:ratio_rate30d)
      /
      (1 - 0.999)
    )
```

## Verification Commands

```bash
# Recording rules are loaded
kubectl port-forward -n monitoring svc/prometheus 9090:9090 &
curl -s http://localhost:9090/api/v1/rules | python3 -m json.tool | grep "slo:"

# SLI values available
curl -s 'http://localhost:9090/api/v1/query?query=slo:availability:ratio_rate5m' | python3 -m json.tool

# Error budget remaining
curl -s 'http://localhost:9090/api/v1/query?query=slo:error_budget_remaining' | python3 -m json.tool

# Burn rate
curl -s 'http://localhost:9090/api/v1/query?query=slo:burn_rate:1h' | python3 -m json.tool

# Alert rules registered
curl -s http://localhost:9090/api/v1/rules | python3 -m json.tool | grep "BurnRate"

# Inject errors and watch burn rate rise
kubectl exec -n demo deploy/service-a -- sh -c 'while true; do curl -s http://localhost:8080/error; sleep 0.1; done' &
sleep 120
curl -s 'http://localhost:9090/api/v1/query?query=slo:burn_rate:1h{service="service-a"}' | python3 -m json.tool

# Grafana dashboard accessible
kubectl port-forward -n monitoring svc/grafana 3000:80 &
curl -s http://localhost:3000/api/health
```

## Cleanup

```bash
kubectl delete namespace demo
helm uninstall kube-prometheus -n monitoring 2>/dev/null
kubectl delete namespace monitoring
```
