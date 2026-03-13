# 16.07 Logging with FluentBit DaemonSet

<!--
difficulty: intermediate
concepts: [fluentbit, daemonset, log-collection, kubernetes-filter, log-pipeline]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [daemonsets, configmaps, rbac]
-->

## What You Will Learn

In this exercise you will deploy FluentBit as a DaemonSet to collect container
logs from every node. You will configure the full pipeline: reading logs from
`/var/log/containers`, enriching them with Kubernetes metadata (pod name,
namespace, labels), and outputting to stdout for verification.

## Step-by-Step

### 1 -- Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: kube-logging
  labels:
    app.kubernetes.io/name: fluentbit
```

### 2 -- Create RBAC

FluentBit needs read access to Pods and Namespaces to enrich log entries.

```yaml
# rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: fluentbit
  namespace: kube-logging
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: fluentbit-read
rules:
  - apiGroups: [""]
    resources: [namespaces, pods]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: fluentbit-read
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: fluentbit-read
subjects:
  - kind: ServiceAccount
    name: fluentbit
    namespace: kube-logging
```

### 3 -- Create the FluentBit Configuration

```yaml
# fluentbit-configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: fluentbit-config
  namespace: kube-logging
  labels:
    app: fluentbit
data:
  fluent-bit.conf: |
    [SERVICE]
        Flush         5
        Log_Level     info
        Daemon        off
        Parsers_File  parsers.conf
        HTTP_Server   On
        HTTP_Listen   0.0.0.0
        HTTP_Port     2020

    [INPUT]
        Name              tail
        Tag               kube.*
        Path              /var/log/containers/*.log
        Parser            cri
        DB                /var/log/flb_kube.db
        Mem_Buf_Limit     5MB
        Skip_Long_Lines   On
        Refresh_Interval  10

    [FILTER]
        Name                kubernetes
        Match               kube.*
        Kube_URL            https://kubernetes.default.svc:443
        Kube_CA_File        /var/run/secrets/kubernetes.io/serviceaccount/ca.crt
        Kube_Token_File     /var/run/secrets/kubernetes.io/serviceaccount/token
        Kube_Tag_Prefix     kube.var.log.containers.
        Merge_Log           On
        Merge_Log_Key       log_processed
        Keep_Log            Off
        K8S-Logging.Parser  On
        K8S-Logging.Exclude On

    [FILTER]
        Name    grep
        Match   kube.*
        Exclude $kubernetes['namespace_name'] kube-system

    [OUTPUT]
        Name            stdout
        Match           kube.*
        Format          json_lines

  parsers.conf: |
    [PARSER]
        Name        cri
        Format      regex
        Regex       ^(?<time>[^ ]+) (?<stream>stdout|stderr) (?<logtag>[^ ]*) (?<log>.*)$
        Time_Key    time
        Time_Format %Y-%m-%dT%H:%M:%S.%L%z

    [PARSER]
        Name        json
        Format      json
        Time_Key    time
        Time_Format %d/%b/%Y:%H:%M:%S %z
```

### 4 -- Deploy the DaemonSet

```yaml
# fluentbit-daemonset.yaml
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: fluentbit
  namespace: kube-logging
  labels:
    app: fluentbit
spec:
  selector:
    matchLabels:
      app: fluentbit
  template:
    metadata:
      labels:
        app: fluentbit
    spec:
      serviceAccountName: fluentbit
      tolerations:
        - key: node-role.kubernetes.io/control-plane
          effect: NoSchedule
        - key: node-role.kubernetes.io/master
          effect: NoSchedule
      containers:
        - name: fluentbit
          image: fluent/fluent-bit:3.1
          ports:
            - containerPort: 2020
              name: http-metrics
          volumeMounts:
            - name: varlog
              mountPath: /var/log
              readOnly: true
            - name: varlibdockercontainers
              mountPath: /var/lib/docker/containers
              readOnly: true
            - name: config
              mountPath: /fluent-bit/etc/
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
      volumes:
        - name: varlog
          hostPath:
            path: /var/log
        - name: varlibdockercontainers
          hostPath:
            path: /var/lib/docker/containers
        - name: config
          configMap:
            name: fluentbit-config
```

### 5 -- Deploy a Log Generator

```yaml
# log-generator.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: log-demo
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: log-generator
  namespace: log-demo
spec:
  replicas: 2
  selector:
    matchLabels:
      app: log-generator
  template:
    metadata:
      labels:
        app: log-generator
    spec:
      containers:
        - name: logger
          image: busybox:1.37
          command: ["sh", "-c"]
          args:
            - |
              counter=0
              while true; do
                counter=$((counter + 1))
                echo "{\"level\":\"info\",\"msg\":\"Processing request\",\"request_id\":\"req-$counter\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}"
                if [ $((counter % 5)) -eq 0 ]; then
                  echo "{\"level\":\"error\",\"msg\":\"Connection timeout\",\"request_id\":\"req-$counter\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\"}" >&2
                fi
                sleep 3
              done
```

### 6 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f rbac.yaml
kubectl apply -f fluentbit-configmap.yaml
kubectl apply -f fluentbit-daemonset.yaml
kubectl apply -f log-generator.yaml
```

## Verify

```bash
# 1. DaemonSet should have one Pod per node
kubectl get daemonset -n kube-logging
kubectl get pods -n kube-logging -o wide

# 2. FluentBit logs should show no errors
kubectl logs -n kube-logging -l app=fluentbit --tail=20

# 3. Log generator should be emitting JSON
kubectl logs -n log-demo -l app=log-generator --tail=5

# 4. FluentBit should be processing log-generator output
kubectl logs -n kube-logging -l app=fluentbit --tail=50 | grep "log-generator"

# 5. Check FluentBit metrics endpoint
FLUENTBIT_POD=$(kubectl get pods -n kube-logging -l app=fluentbit -o jsonpath='{.items[0].metadata.name}')
kubectl exec -n kube-logging "$FLUENTBIT_POD" -- wget -qO- http://localhost:2020/api/v1/metrics
```

## Cleanup

```bash
kubectl delete namespace kube-logging
kubectl delete namespace log-demo
kubectl delete clusterrole fluentbit-read
kubectl delete clusterrolebinding fluentbit-read
```

## What's Next

Continue to [16.08 Prometheus ServiceMonitor and PodMonitor](../08-prometheus-servicemonitor/08-prometheus-servicemonitor.md)
to learn how to configure declarative metric scraping with Prometheus Operator CRDs.

## Summary

- FluentBit as a DaemonSet ensures one log collector per node.
- The pipeline is INPUT (tail) -> FILTER (kubernetes metadata) -> OUTPUT (destination).
- The Kubernetes filter enriches logs with pod name, namespace, and labels.
- ConfigMap-based configuration allows changes without rebuilding the image.
- FluentBit exposes its own metrics on port 2020 for monitoring the log pipeline.

## References

- [FluentBit Kubernetes Installation](https://docs.fluentbit.io/manual/installation/kubernetes)
- [Kubernetes Logging Architecture](https://kubernetes.io/docs/concepts/cluster-administration/logging/)
- [DaemonSet Concepts](https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/)
