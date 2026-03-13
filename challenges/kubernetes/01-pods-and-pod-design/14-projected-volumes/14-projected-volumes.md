# 14. Projected Volumes and Combined Sources

<!--
difficulty: advanced
concepts: [projected-volumes, configmap, secret, downward-api, service-account-token, volume-projection]
tools: [kubectl, minikube]
estimated_time: 35m
bloom_level: analyze
prerequisites: [01-10, 01-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 10 (Downward API and Pod Metadata)](../10-downward-api-and-pod-metadata/10-downward-api-and-pod-metadata.md) and [exercise 07 (Init Containers)](../07-init-containers/07-init-containers.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** projected volumes to combine ConfigMaps, Secrets, Downward API, and service account tokens into a single mount
- **Analyze** how projected volumes simplify container configuration when data comes from multiple sources
- **Evaluate** security considerations for projected service account tokens vs. default tokens

## Architecture

A projected volume merges data from multiple sources into a single directory. Instead of mounting three separate volumes for a ConfigMap, a Secret, and Downward API data, you mount one projected volume that contains files from all three. Sources available in a projected volume:

- `configMap` -- files from a ConfigMap
- `secret` -- files from a Secret
- `downwardAPI` -- Pod metadata as files
- `serviceAccountToken` -- a bound, time-limited JWT

## Steps

### 1. Create Supporting Resources

```yaml
# config-and-secret.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  database.conf: |
    host=postgres.default.svc
    port=5432
    pool_size=10
  feature-flags.conf: |
    enable_cache=true
    enable_metrics=true
---
apiVersion: v1
kind: Secret
metadata:
  name: app-credentials
type: Opaque
stringData:
  db-password: "s3cret-p4ssw0rd"
  api-key: "ak-12345-abcde"
```

```bash
kubectl apply -f config-and-secret.yaml
```

### 2. Create a Pod with a Projected Volume

```yaml
# projected-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: projected-demo
  labels:
    app: projected-demo
    version: v1
spec:
  serviceAccountName: default
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "echo '=== /etc/app-data contents ===' && find /etc/app-data -type f -exec echo '--- {} ---' \\; -exec cat {} \\; && sleep 3600"]
      volumeMounts:
        - name: all-in-one
          mountPath: /etc/app-data
          readOnly: true
  volumes:
    - name: all-in-one
      projected:
        sources:
          - configMap:
              name: app-config
              items:
                - key: database.conf
                  path: config/database.conf
                - key: feature-flags.conf
                  path: config/feature-flags.conf
          - secret:
              name: app-credentials
              items:
                - key: db-password
                  path: secrets/db-password
                - key: api-key
                  path: secrets/api-key
          - downwardAPI:
              items:
                - path: metadata/labels
                  fieldRef:
                    fieldPath: metadata.labels
                - path: metadata/name
                  fieldRef:
                    fieldPath: metadata.name
          - serviceAccountToken:
              path: token
              expirationSeconds: 3600
              audience: api
```

```bash
kubectl apply -f projected-pod.yaml
sleep 5
kubectl logs projected-demo
```

### 3. Explore the Projected Directory Structure

```bash
kubectl exec projected-demo -- find /etc/app-data -type f
```

Expected directory structure:

```
/etc/app-data/config/database.conf
/etc/app-data/config/feature-flags.conf
/etc/app-data/secrets/db-password
/etc/app-data/secrets/api-key
/etc/app-data/metadata/labels
/etc/app-data/metadata/name
/etc/app-data/token
```

All four sources merged into a single mount point with organized subdirectories.

### 4. Examine the Service Account Token

```bash
kubectl exec projected-demo -- cat /etc/app-data/token
```

This is a bound service account token (JWT). Unlike the legacy auto-mounted token, it has:
- A specific audience (`api`)
- An expiration time (3600 seconds)
- Automatic rotation before expiry

### 5. Verify Dynamic Updates

Change a label on the Pod:

```bash
kubectl label pod projected-demo version=v2 --overwrite
sleep 15
kubectl exec projected-demo -- cat /etc/app-data/metadata/labels
```

The labels file reflects the update. ConfigMap and Secret changes also propagate (with a delay of up to the kubelet sync period, typically ~60 seconds).

## Verify What You Learned

```bash
# All files present
kubectl exec projected-demo -- ls -la /etc/app-data/config/
kubectl exec projected-demo -- ls -la /etc/app-data/secrets/

# Secret content accessible
kubectl exec projected-demo -- cat /etc/app-data/secrets/db-password
# Expected: s3cret-p4ssw0rd

# Token is a JWT
kubectl exec projected-demo -- cat /etc/app-data/token | cut -d. -f1 | base64 -d 2>/dev/null
```

## Cleanup

```bash
kubectl delete pod projected-demo
kubectl delete configmap app-config
kubectl delete secret app-credentials
```

## Summary

- **Projected volumes** merge ConfigMaps, Secrets, Downward API, and service account tokens into one mount
- Use `items` with `path` to organize files into subdirectories within the projection
- Projected service account tokens are **bound**, **time-limited**, and **audience-scoped** -- more secure than legacy tokens
- ConfigMap, Secret, and Downward API data in projected volumes update dynamically
- Projected volumes reduce the number of `volumeMounts` needed per container

## Reference

- [Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/) — official concept documentation
- [Configure a Pod to Use a Projected Volume](https://kubernetes.io/docs/tasks/configure-pod-container/configure-projected-volume-storage/) — tutorial
- [Service Account Token Volume Projection](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/#service-account-token-volume-projection) — bound token docs
