<!--
difficulty: advanced
concepts: [keda-multi-trigger, trigger-authentication, fallback, scaled-object-advanced, composite-scaling]
tools: [kubectl, helm, keda]
estimated_time: 45m
bloom_level: analyze
prerequisites: [keda-basics, hpa-custom-metrics]
-->

# 14.09 - KEDA Advanced: Multiple Triggers and Fallback

## Architecture

```
  +----------+    +----------+    +----------+
  | Trigger1 |    | Trigger2 |    | Trigger3 |
  | (SQS)    |    | (Prom)   |    | (Cron)   |
  +----+-----+    +----+-----+    +----+-----+
       |               |               |
       +-------+-------+-------+-------+
               |               |
        +------v------+  +----v------+
        | ScaledObject|  | Fallback  |
        | (picks max  |  | (static   |
        |  replicas)  |  |  count)   |
        +------+------+  +----+------+
               |               |
               +-------+-------+
                       |
                +------v------+
                |     HPA     |
                +------+------+
                       |
                +------v------+
                | Deployment  |
                +-------------+
```

When KEDA evaluates multiple triggers, it computes the desired replica count for each and takes the **maximum**. If a trigger source becomes unreachable, the `fallback` configuration provides a static replica count to prevent scaling to zero during outages.

## What You Will Learn

- How to combine multiple triggers in a single ScaledObject
- How TriggerAuthentication and ClusterTriggerAuthentication manage secrets
- How `fallback` protects against trigger source failures
- How `idleReplicaCount` differs from `minReplicaCount`

## Suggested Steps

1. Deploy a workload in a new namespace
2. Create a ScaledObject with both a Prometheus trigger and a cron trigger
3. Add TriggerAuthentication for a trigger that requires credentials
4. Configure `fallback` with a static replica count
5. Simulate a trigger source failure and observe the fallback
6. Test `idleReplicaCount` behavior (replicas when all triggers report zero)

### ScaledObject with Multiple Triggers and Fallback

```yaml
# scaledobject-advanced.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: multi-trigger-scaler
  namespace: keda-advanced
spec:
  scaleTargetRef:
    name: api-server
  pollingInterval: 15
  cooldownPeriod: 120
  idleReplicaCount: 1              # keep 1 replica when all triggers are at zero
  minReplicaCount: 2               # minimum during active scaling
  maxReplicaCount: 30
  fallback:
    failureThreshold: 3            # after 3 consecutive failures
    replicas: 5                    # hold at 5 replicas
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.monitoring.svc:9090
        metricName: http_rps
        query: sum(rate(http_requests_total{namespace="keda-advanced"}[2m]))
        threshold: "200"
    - type: cron
      metadata:
        timezone: UTC
        start: 0 9 * * 1-5
        end: 0 17 * * 1-5
        desiredReplicas: "10"
    - type: cpu
      metricType: Utilization
      metadata:
        value: "70"
```

### TriggerAuthentication with Secrets

```yaml
# trigger-auth.yaml
apiVersion: v1
kind: Secret
metadata:
  name: external-creds
  namespace: keda-advanced
type: Opaque
data:
  api-key: YXBpLWtleS12YWx1ZQ==          # base64 encoded
  endpoint: aHR0cHM6Ly9tZXRyaWNzLmV4YW1wbGUuY29t
---
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: external-auth
  namespace: keda-advanced
spec:
  secretTargetRef:
    - parameter: apiKey
      name: external-creds
      key: api-key
    - parameter: endpoint
      name: external-creds
      key: endpoint
```

### ClusterTriggerAuthentication (Cluster-Wide)

```yaml
# cluster-trigger-auth.yaml
apiVersion: keda.sh/v1alpha1
kind: ClusterTriggerAuthentication
metadata:
  name: global-aws-auth
spec:
  secretTargetRef:
    - parameter: awsAccessKeyID
      name: aws-creds
      key: AWS_ACCESS_KEY_ID
    - parameter: awsSecretAccessKey
      name: aws-creds
      key: AWS_SECRET_ACCESS_KEY
  env:
    - parameter: awsRegion
      name: AWS_DEFAULT_REGION
      containerName: keda-operator
```

### ScaledObject Using TriggerAuthentication

```yaml
# scaledobject-with-auth.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: authenticated-scaler
  namespace: keda-advanced
spec:
  scaleTargetRef:
    name: api-server
  pollingInterval: 30
  cooldownPeriod: 300
  minReplicaCount: 1
  maxReplicaCount: 15
  triggers:
    - type: metrics-api
      metadata:
        targetValue: "50"
        url: "https://metrics.example.com/api/v1/queue-depth"
        valueLocation: "data.depth"
      authenticationRef:
        name: external-auth
```

## Verify

```bash
# 1. Check ScaledObject health
kubectl get scaledobject -n keda-advanced
kubectl describe scaledobject multi-trigger-scaler -n keda-advanced

# 2. Verify the HPA KEDA created (should show multiple metrics)
kubectl get hpa -n keda-advanced -o yaml

# 3. Check TriggerAuthentication
kubectl get triggerauthentication -n keda-advanced

# 4. Simulate trigger failure by breaking the Prometheus endpoint
# KEDA should activate fallback after 3 failures
kubectl get deployment api-server -n keda-advanced

# 5. Check KEDA operator logs for fallback activation
kubectl logs -n keda -l app=keda-operator --tail=50

# 6. Watch events
kubectl get events -n keda-advanced --sort-by='.lastTimestamp'
```

## Cleanup

```bash
kubectl delete namespace keda-advanced
```

## What's Next

Scaling pods is only half the story. The next exercise covers scaling the cluster itself -- adding and removing nodes to match workload demand: [14.10 - Cluster Autoscaler: Node Scaling](../10-cluster-autoscaler/10-cluster-autoscaler.md).

## Summary

- Multiple triggers in a ScaledObject are evaluated independently; the maximum replica count wins
- `fallback` provides a safety net when trigger sources are unreachable
- `idleReplicaCount` is the replica count when all triggers report zero; `minReplicaCount` applies during active scaling
- TriggerAuthentication injects secrets into trigger configurations without embedding them in ScaledObject specs
- ClusterTriggerAuthentication works across all namespaces
