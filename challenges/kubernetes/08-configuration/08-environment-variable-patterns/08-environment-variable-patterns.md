<!--
difficulty: intermediate
concepts: [fieldRef, resourceFieldRef, downward-api, dependent-env-vars, pod-metadata-injection]
tools: [kubectl]
estimated_time: 25m
bloom_level: apply
prerequisites: [configmaps-environment-and-files, secrets-management]
-->

# 8.08 Environment Variable Patterns: fieldRef, resourceFieldRef

## What You Will Learn

- How to inject pod metadata (name, namespace, IP, node name) using `fieldRef`
- How to inject resource limits and requests using `resourceFieldRef`
- How to reference other environment variables with `$(VAR_NAME)` syntax
- The full set of fields available through the Downward API

## Steps

### 1. Pod with fieldRef -- inject pod metadata

```yaml
# pod-fieldref.yaml
apiVersion: v1
kind: Pod
metadata:
  name: env-patterns
  namespace: default
  labels:
    app: demo
    version: v2
  annotations:
    description: "Environment variable patterns demo"
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Pod Metadata (fieldRef) ==="
          echo "POD_NAME=$POD_NAME"
          echo "POD_NAMESPACE=$POD_NAMESPACE"
          echo "POD_IP=$POD_IP"
          echo "POD_SERVICE_ACCOUNT=$POD_SERVICE_ACCOUNT"
          echo "NODE_NAME=$NODE_NAME"
          echo "HOST_IP=$HOST_IP"
          echo "POD_UID=$POD_UID"
          echo ""
          echo "=== Resource Limits (resourceFieldRef) ==="
          echo "CPU_REQUEST=$CPU_REQUEST"
          echo "CPU_LIMIT=$CPU_LIMIT"
          echo "MEMORY_REQUEST=$MEMORY_REQUEST"
          echo "MEMORY_LIMIT=$MEMORY_LIMIT"
          echo ""
          echo "=== Dependent Variables ==="
          echo "SERVICE_URL=$SERVICE_URL"
          echo "LOG_PREFIX=$LOG_PREFIX"
          sleep 3600
      resources:
        requests:
          cpu: "100m"
          memory: "64Mi"
        limits:
          cpu: "500m"
          memory: "128Mi"
      env:
        # Pod metadata via fieldRef
        - name: POD_NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: POD_NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        - name: POD_IP
          valueFrom:
            fieldRef:
              fieldPath: status.podIP
        - name: POD_SERVICE_ACCOUNT
          valueFrom:
            fieldRef:
              fieldPath: spec.serviceAccountName
        - name: NODE_NAME
          valueFrom:
            fieldRef:
              fieldPath: spec.nodeName
        - name: HOST_IP
          valueFrom:
            fieldRef:
              fieldPath: status.hostIP
        - name: POD_UID
          valueFrom:
            fieldRef:
              fieldPath: metadata.uid

        # Resource limits via resourceFieldRef
        - name: CPU_REQUEST
          valueFrom:
            resourceFieldRef:
              resource: requests.cpu
        - name: CPU_LIMIT
          valueFrom:
            resourceFieldRef:
              resource: limits.cpu
        - name: MEMORY_REQUEST
          valueFrom:
            resourceFieldRef:
              resource: requests.memory
        - name: MEMORY_LIMIT
          valueFrom:
            resourceFieldRef:
              resource: limits.memory

        # Dependent variables referencing other env vars
        - name: APP_PORT
          value: "8080"
        - name: SERVICE_URL
          value: "http://$(POD_IP):$(APP_PORT)"
        - name: LOG_PREFIX
          value: "[$(POD_NAMESPACE)/$(POD_NAME)]"
  restartPolicy: Never
```

### 2. Expose labels and annotations as files (Downward API volume)

Some fields (labels, annotations) cannot be exposed as env vars. Use a Downward API volume instead.

```yaml
# pod-downward-volume.yaml
apiVersion: v1
kind: Pod
metadata:
  name: downward-volume
  labels:
    app: demo
    version: v2
  annotations:
    description: "Downward API volume demo"
spec:
  containers:
    - name: app
      image: busybox:1.37
      command:
        - sh
        - -c
        - |
          echo "=== Labels ==="
          cat /etc/podinfo/labels
          echo ""
          echo "=== Annotations ==="
          cat /etc/podinfo/annotations
          sleep 3600
      volumeMounts:
        - name: podinfo
          mountPath: /etc/podinfo
  volumes:
    - name: podinfo
      downwardAPI:
        items:
          - path: labels
            fieldRef:
              fieldPath: metadata.labels
          - path: annotations
            fieldRef:
              fieldPath: metadata.annotations
  restartPolicy: Never
```

### TODO: Create a pod that builds a JDBC connection string from ConfigMap values and pod metadata

The connection string should be: `jdbc:postgresql://$(DB_HOST):$(DB_PORT)/$(DB_NAME)?currentSchema=$(POD_NAMESPACE)`

<details>
<summary>Solution</summary>

```yaml
env:
  - name: DB_HOST
    valueFrom:
      configMapKeyRef:
        name: db-config
        key: DB_HOST
  - name: DB_PORT
    valueFrom:
      configMapKeyRef:
        name: db-config
        key: DB_PORT
  - name: DB_NAME
    valueFrom:
      configMapKeyRef:
        name: db-config
        key: DB_NAME
  - name: POD_NAMESPACE
    valueFrom:
      fieldRef:
        fieldPath: metadata.namespace
  - name: JDBC_URL
    value: "jdbc:postgresql://$(DB_HOST):$(DB_PORT)/$(DB_NAME)?currentSchema=$(POD_NAMESPACE)"
```

</details>

## Verify

```bash
kubectl apply -f pod-fieldref.yaml
kubectl apply -f pod-downward-volume.yaml

# Check injected metadata
kubectl exec env-patterns -- env | grep POD_
kubectl exec env-patterns -- env | grep CPU_
kubectl exec env-patterns -- env | grep SERVICE_URL

# Check downward API volume
kubectl exec downward-volume -- cat /etc/podinfo/labels
kubectl exec downward-volume -- cat /etc/podinfo/annotations
```

## Cleanup

```bash
kubectl delete pod env-patterns downward-volume
```

## What's Next

Continue to [8.09 External Secrets Operator with Cloud Providers](../09-external-secrets-operator/09-external-secrets-operator.md) to learn how to sync secrets from AWS Secrets Manager, GCP Secret Manager, and Azure Key Vault.

## Summary

- `fieldRef` injects pod metadata: name, namespace, IP, node name, UID, service account.
- `resourceFieldRef` injects CPU and memory requests/limits as numeric values.
- `$(VAR_NAME)` syntax lets you build composite values from other env vars.
- Labels and annotations must be exposed via Downward API volumes, not env vars.

## References

- [Expose Pod Information via Environment Variables](https://kubernetes.io/docs/tasks/inject-data-application/environment-variable-expose-pod-information/)
- [Downward API](https://kubernetes.io/docs/concepts/workloads/pods/downward-api/)
