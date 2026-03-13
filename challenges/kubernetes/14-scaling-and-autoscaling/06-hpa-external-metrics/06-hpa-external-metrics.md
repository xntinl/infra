<!--
difficulty: intermediate
concepts: [external-metrics, sqs-queue-depth, cloudwatch-metrics, hpa-external-type, metrics-api]
tools: [kubectl, aws-cli, keda]
estimated_time: 35m
bloom_level: apply
prerequisites: [hpa-custom-metrics, aws-basics]
-->

# 14.06 - HPA with External Metrics (SQS, CloudWatch)

## Why This Matters

Many workloads are driven by events outside the Kubernetes cluster: messages accumulating in an SQS queue, CloudWatch alarm metrics, or Datadog custom gauges. External metrics let the HPA react to these signals without requiring the metric to exist on a pod. A queue consumer should scale based on queue depth, not CPU usage.

## What You Will Learn

- How `type: External` metrics differ from `type: Pods` and `type: Resource`
- How to register an external metrics provider with the API server
- How to configure HPA to scale based on SQS `ApproximateNumberOfMessages`
- How KEDA simplifies external metric integration with built-in scalers

## Guide

### 1. Create Namespace and Consumer Deployment

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: external-metrics-lab
```

```yaml
# consumer-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: queue-consumer
  namespace: external-metrics-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: queue-consumer
  template:
    metadata:
      labels:
        app: queue-consumer
    spec:
      containers:
        - name: consumer
          image: busybox:1.37
          command:
            - /bin/sh
            - -c
            - "echo 'Processing messages...'; while true; do sleep 10; done"
          resources:
            requests:
              cpu: 50m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
```

### 2. HPA with External Metric (SQS Queue Depth)

```yaml
# hpa-external.yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: queue-consumer-hpa
  namespace: external-metrics-lab
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: queue-consumer
  minReplicas: 1
  maxReplicas: 20
  metrics:
    - type: External
      external:
        metric:
          name: sqs_queue_messages_visible
          selector:
            matchLabels:
              queue: my-work-queue
        target:
          type: AverageValue        # divide total metric by replica count
          averageValue: "5"         # 5 messages per consumer pod
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 15
      policies:
        - type: Pods
          value: 5
          periodSeconds: 30
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Pods
          value: 1
          periodSeconds: 60
```

### 3. KEDA with AWS SQS Scaler

KEDA has a built-in SQS scaler that handles IAM authentication and metric translation.

```yaml
# keda-sqs.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: queue-consumer-scaledobject
  namespace: external-metrics-lab
spec:
  scaleTargetRef:
    name: queue-consumer
  pollingInterval: 10
  cooldownPeriod: 300
  minReplicaCount: 0              # KEDA supports scale-to-zero
  maxReplicaCount: 20
  triggers:
    - type: aws-sqs-queue
      metadata:
        queueURL: https://sqs.us-east-1.amazonaws.com/123456789012/my-work-queue
        queueLength: "5"          # target messages per pod
        awsRegion: us-east-1
      authenticationRef:
        name: aws-credentials
---
apiVersion: keda.sh/v1alpha1
kind: TriggerAuthentication
metadata:
  name: aws-credentials
  namespace: external-metrics-lab
spec:
  secretTargetRef:
    - parameter: awsAccessKeyID
      name: aws-secret
      key: AWS_ACCESS_KEY_ID
    - parameter: awsSecretAccessKey
      name: aws-secret
      key: AWS_SECRET_ACCESS_KEY
```

### 4. KEDA with CloudWatch Metric

```yaml
# keda-cloudwatch.yaml
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: cloudwatch-scaledobject
  namespace: external-metrics-lab
spec:
  scaleTargetRef:
    name: queue-consumer
  pollingInterval: 30
  cooldownPeriod: 300
  minReplicaCount: 1
  maxReplicaCount: 15
  triggers:
    - type: aws-cloudwatch
      metadata:
        namespace: AWS/SQS
        dimensionName: QueueName
        dimensionValue: my-work-queue
        metricName: ApproximateNumberOfMessagesVisible
        targetMetricValue: "50"
        minMetricValue: "0"
        awsRegion: us-east-1
        metricStatPeriod: "60"
        metricStat: Average
      authenticationRef:
        name: aws-credentials
```

### Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f consumer-deployment.yaml

# Choose one approach:
# Option A: Native HPA with external metrics provider
kubectl apply -f hpa-external.yaml

# Option B: KEDA (install KEDA first: helm install keda kedacore/keda -n keda --create-namespace)
kubectl apply -f keda-sqs.yaml
```

## TODO

The following HPA external metric config has a mistake. Fix it so the HPA scales to handle 100 messages per consumer pod.

```yaml
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: fix-me-hpa
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: queue-consumer
  minReplicas: 1
  maxReplicas: 10
  metrics:
    - type: External
      external:
        metric:
          name: sqs_queue_messages_visible
        target:
          type: Value              # <-- is this right?
          value: "100"
```

<details>
<summary>Show Answer</summary>

The `type: Value` target means "scale so the total metric stays at 100." If the queue has 500 messages, that is `ceil(1 * 500/100) = 5` replicas. This works, but if the intent is 100 messages **per pod**, the target should be `type: AverageValue` with `averageValue: "100"`. With `AverageValue`, the HPA divides the total metric by the current replica count and compares per-pod.

</details>

## Verify

```bash
# 1. Check HPA status
kubectl get hpa -n external-metrics-lab

# 2. Describe to see metric values
kubectl describe hpa queue-consumer-hpa -n external-metrics-lab

# 3. If using KEDA, check the ScaledObject
kubectl get scaledobject -n external-metrics-lab
kubectl describe scaledobject queue-consumer-scaledobject -n external-metrics-lab

# 4. Check events for scaling decisions
kubectl get events -n external-metrics-lab --sort-by='.lastTimestamp'
```

## Cleanup

```bash
kubectl delete namespace external-metrics-lab
```

## What's Next

You have seen horizontal scaling from resource, custom, and external metrics. The next exercise covers **vertical** scaling -- automatically adjusting pod resource requests with the Vertical Pod Autoscaler: [14.07 - Vertical Pod Autoscaler (VPA)](../07-vertical-pod-autoscaler/07-vertical-pod-autoscaler.md).

## Summary

- `type: External` metrics come from outside the cluster (cloud queues, monitoring services)
- `AverageValue` divides the total metric by replica count; `Value` compares the raw total
- An external metrics provider (or KEDA) must be registered as an APIService
- KEDA supports scale-to-zero and has 60+ built-in scalers for cloud services
- Queue-based workloads should scale on queue depth, not CPU
