# 16.04 Kubernetes Events: Monitoring and Exporting

<!--
difficulty: basic
concepts: [kubernetes-events, event-api, event-exporter, cluster-monitoring]
tools: [kubectl]
estimated_time: 20m
bloom_level: understand
prerequisites: [pods, namespaces]
-->

## What You Will Learn

In this exercise you will explore Kubernetes Events -- the built-in mechanism
that records what happens inside the cluster. You will query events by type,
namespace, and resource, understand their short retention window, and deploy an
event exporter to persist them beyond the default TTL.

## Why It Matters

Events are the first place to look when something goes wrong. They tell you why
a Pod failed to schedule, why an image pull was denied, or why a probe started
failing. However, events are ephemeral (default 1-hour TTL). Exporting them
gives you a durable audit trail for troubleshooting past incidents.

## Step-by-Step

### 1 -- Create a Namespace and a Failing Pod

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: events-lab
```

```yaml
# failing-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: bad-image
  namespace: events-lab
spec:
  containers:
    - name: app
      image: nginx:does-not-exist   # deliberately wrong tag
      ports:
        - containerPort: 80
```

```yaml
# good-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: healthy-app
  namespace: events-lab
spec:
  containers:
    - name: app
      image: nginx:1.27-alpine
      ports:
        - containerPort: 80
```

### 2 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f failing-pod.yaml
kubectl apply -f good-pod.yaml
```

### 3 -- Query Events

```bash
# All events in the namespace (most recent last)
kubectl get events -n events-lab --sort-by='.lastTimestamp'

# Only Warning events
kubectl get events -n events-lab --field-selector type=Warning

# Events for a specific Pod
kubectl get events -n events-lab --field-selector involvedObject.name=bad-image

# Cluster-wide events (all namespaces)
kubectl get events -A --sort-by='.metadata.creationTimestamp' | tail -20

# Watch events in real time
kubectl get events -n events-lab -w
```

### 4 -- Examine Event Structure

```bash
# Full event object in YAML
kubectl get events -n events-lab -o yaml | head -60

# Key fields: reason, message, type, count, firstTimestamp, lastTimestamp
kubectl get events -n events-lab \
  -o custom-columns='LAST SEEN:.lastTimestamp,TYPE:.type,REASON:.reason,OBJECT:.involvedObject.name,MESSAGE:.message'
```

### 5 -- Deploy an Event Exporter

The Kubernetes Event Exporter forwards events to external sinks (stdout, files,
webhooks, Elasticsearch). Here is a minimal deployment that logs events as JSON
to stdout.

```yaml
# event-exporter-rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: event-exporter
  namespace: events-lab
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: event-exporter
rules:
  - apiGroups: [""]
    resources: ["events"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: event-exporter
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: event-exporter
subjects:
  - kind: ServiceAccount
    name: event-exporter
    namespace: events-lab
```

```yaml
# event-exporter-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: event-exporter-config
  namespace: events-lab
data:
  config.yaml: |
    logLevel: info
    route:
      routes:
        - match:
            - receiver: "stdout"
    receivers:
      - name: "stdout"
        stdout:
          deDot: true
```

```yaml
# event-exporter.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: event-exporter
  namespace: events-lab
spec:
  replicas: 1
  selector:
    matchLabels:
      app: event-exporter
  template:
    metadata:
      labels:
        app: event-exporter
    spec:
      serviceAccountName: event-exporter
      containers:
        - name: exporter
          image: ghcr.io/resmoio/kubernetes-event-exporter:v1.7
          args: ["-conf", "/config/config.yaml"]
          volumeMounts:
            - name: config
              mountPath: /config
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
            limits:
              cpu: 100m
              memory: 64Mi
      volumes:
        - name: config
          configMap:
            name: event-exporter-config
```

### 6 -- Apply the Exporter

```bash
kubectl apply -f event-exporter-rbac.yaml
kubectl apply -f event-exporter-config.yaml
kubectl apply -f event-exporter.yaml
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Relying on events for long-term audit | Default TTL is 1 hour; events disappear |
| Not filtering by `type=Warning` | Normal events add noise when troubleshooting |
| Missing RBAC for the event exporter | Exporter gets 403 forbidden and logs nothing |
| Assuming events show container logs | Events are cluster-level notifications, not application output |

## Verify

```bash
# 1. Check events for the failing pod
kubectl get events -n events-lab --field-selector involvedObject.name=bad-image

# 2. Confirm event exporter is running
kubectl get pods -n events-lab -l app=event-exporter

# 3. Check exported events in exporter logs
kubectl logs -n events-lab -l app=event-exporter --tail=20

# 4. Create a new event source and watch it appear
kubectl run temp-pod --image=busybox:1.37 -n events-lab --command -- sleep 30
kubectl logs -n events-lab -l app=event-exporter --tail=5
kubectl delete pod temp-pod -n events-lab
```

## Cleanup

```bash
kubectl delete namespace events-lab
kubectl delete clusterrole event-exporter
kubectl delete clusterrolebinding event-exporter
```

## What's Next

Continue to [16.05 Probe Patterns for Slow-Starting Applications](../05-probe-patterns-slow-startup/)
to learn how to tune probes for applications with long initialization times.

## Summary

- Events are Kubernetes-native notifications about cluster activity (scheduling,
  pulling images, probe failures, scaling).
- Use `kubectl get events` with `--field-selector` and `--sort-by` to filter
  and sort efficiently.
- Events have a default 1-hour TTL -- they are not a durable store.
- An event exporter (e.g., kubernetes-event-exporter) forwards events to
  external systems for long-term retention.
- Always check events first when diagnosing Pod startup or runtime issues.

## References

- [Viewing Events](https://kubernetes.io/docs/tasks/debug/debug-application/debug-running-pod/#examine-events)
- [Event API Reference](https://kubernetes.io/docs/reference/kubernetes-api/cluster-resources/event-v1/)

## Additional Resources

- [kubernetes-event-exporter](https://github.com/resmoio/kubernetes-event-exporter)
- [Kubernetes Logging Architecture](https://kubernetes.io/docs/concepts/cluster-administration/logging/)
