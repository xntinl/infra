<!--
difficulty: advanced
concepts: [keda, scaledobject, scaledjob, triggers, scale-to-zero, event-driven-autoscaling]
tools: [kubectl, helm, keda]
estimated_time: 40m
bloom_level: analyze
prerequisites: [hpa-custom-metrics, hpa-external-metrics]
-->

# 14.08 - KEDA: Event-Driven Autoscaling

## Architecture

```
                    +-----------------+
                    |  Event Source   |
                    | (Queue, DB,    |
                    |  Prometheus)    |
                    +--------+--------+
                             |
                    polls every N sec
                             |
                    +--------v--------+
                    |  KEDA Operator  |
                    |  (Metrics      |
                    |   Server)       |
                    +--------+--------+
                             |
                  creates/manages HPA
                             |
                    +--------v--------+
                    |      HPA        |
                    | (autoscaling/v2)|
                    +--------+--------+
                             |
                    scales replicas
                             |
              +--------------v--------------+
              |   Deployment / StatefulSet  |
              |   (or Job via ScaledJob)    |
              +-----------------------------+
```

KEDA sits between your event sources and the Kubernetes HPA. It polls external systems, translates their state into metrics, and manages an HPA resource that drives the actual scaling. KEDA adds two key capabilities the native HPA lacks: **scale-to-zero** and **60+ built-in trigger types**.

## What You Will Learn

- How to install KEDA and understand its components (Operator, Metrics Server, Admission Webhooks)
- How ScaledObject maps triggers to Deployments/StatefulSets
- How ScaledJob maps triggers to Kubernetes Jobs
- How to configure polling intervals, cooldown periods, and idle replica counts

## Suggested Steps

1. Install KEDA via Helm into the `keda` namespace
2. Deploy a simple application in a new namespace
3. Create a ScaledObject with a `cron` trigger to observe time-based scaling
4. Create a ScaledObject with a `prometheus` trigger for metric-based scaling
5. Test scale-to-zero by configuring `minReplicaCount: 0`
6. Deploy a ScaledJob that spawns Jobs based on a trigger
7. Observe the HPA resources KEDA creates behind the scenes

### Install KEDA

```bash
helm repo add kedacore https://kedacore.github.io/charts
helm repo update
helm install keda kedacore/keda --namespace keda --create-namespace
```

### ScaledObject with Cron Trigger

```yaml
# scaledobject-cron.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: cron-scaler
  namespace: keda-lab
spec:
  scaleTargetRef:
    name: web-app
  pollingInterval: 15
  cooldownPeriod: 60
  minReplicaCount: 0               # scale to zero outside business hours
  maxReplicaCount: 10
  triggers:
    - type: cron
      metadata:
        timezone: America/New_York
        start: 0 8 * * 1-5         # 8 AM weekdays
        end: 0 18 * * 1-5          # 6 PM weekdays
        desiredReplicas: "5"
```

### ScaledObject with Prometheus Trigger

```yaml
# scaledobject-prometheus.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: prometheus-scaler
  namespace: keda-lab
spec:
  scaleTargetRef:
    name: web-app
  pollingInterval: 15
  cooldownPeriod: 120
  minReplicaCount: 1
  maxReplicaCount: 20
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.monitoring.svc:9090
        metricName: http_requests_per_second
        query: sum(rate(http_requests_total{namespace="keda-lab"}[2m]))
        threshold: "100"
```

### ScaledJob

```yaml
# scaledjob.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledJob
metadata:
  name: queue-processor
  namespace: keda-lab
spec:
  jobTargetRef:
    template:
      spec:
        containers:
          - name: processor
            image: busybox:1.37
            command:
              - /bin/sh
              - -c
              - "echo 'Processing batch...'; sleep 30; echo 'Done'"
        restartPolicy: Never
  pollingInterval: 10
  maxReplicaCount: 10
  triggers:
    - type: prometheus
      metadata:
        serverAddress: http://prometheus.monitoring.svc:9090
        metricName: pending_jobs
        query: pending_jobs_total{namespace="keda-lab"}
        threshold: "1"
```

## Verify

```bash
# 1. Confirm KEDA is running
kubectl get pods -n keda

# 2. Check ScaledObject status
kubectl get scaledobject -n keda-lab
kubectl describe scaledobject prometheus-scaler -n keda-lab

# 3. Check the HPA that KEDA created
kubectl get hpa -n keda-lab

# 4. Verify scale-to-zero works (deployment should have 0 replicas when idle)
kubectl get deployment web-app -n keda-lab

# 5. Check ScaledJob status
kubectl get scaledjob -n keda-lab
kubectl get jobs -n keda-lab

# 6. Watch scaling events
kubectl get events -n keda-lab --sort-by='.lastTimestamp'
```

## Cleanup

```bash
kubectl delete namespace keda-lab
helm uninstall keda -n keda
kubectl delete namespace keda
```

## What's Next

Now that you understand basic KEDA triggers, the next exercise covers advanced KEDA patterns including multiple triggers, fallback behavior, and trigger authentication: [14.09 - KEDA Advanced: Multiple Triggers and Fallback](../09-keda-advanced-triggers/).

## Summary

- KEDA extends the native HPA with 60+ trigger types and scale-to-zero capability
- ScaledObject targets Deployments/StatefulSets; ScaledJob targets Jobs
- KEDA creates and manages HPA resources automatically
- `pollingInterval` controls how often KEDA checks the trigger source
- `cooldownPeriod` is the delay before scaling to `minReplicaCount` after the last trigger activation
