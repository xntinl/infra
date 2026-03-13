# API Server Audit Logging

<!--
difficulty: intermediate
concepts: [audit-logging, audit-policy, api-server, security, compliance, static-pods]
tools: [kubectl, kubeadm]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-kubeadm-cluster-setup, 12-static-pods-and-manifests]
-->

## Overview

Kubernetes API server audit logging records requests made to the API server. Audit logs answer "who did what, when, and to which resources" -- essential for security, compliance, and debugging. This exercise covers creating an audit policy, configuring the API server, and analyzing audit events.

## Why This Matters

In production clusters, audit logs are a compliance requirement (SOC 2, HIPAA, PCI-DSS) and a key forensic tool for security incidents. Without them, you have no record of who created, modified, or deleted resources.

## Step-by-Step Instructions

### Step 1 -- Create an Audit Policy

The audit policy defines which events to record and at what level.

```yaml
# /etc/kubernetes/audit-policy.yaml
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  # Do not log requests to the following read-only endpoints
  - level: None
    nonResourceURLs:
      - /healthz*
      - /readyz*
      - /livez*

  # Do not log kube-system service account token requests
  - level: None
    users: ["system:serviceaccount:kube-system:*"]
    verbs: ["get"]
    resources:
      - group: ""
        resources: ["configmaps"]

  # Log Secret access at Metadata level (do not log request/response bodies)
  - level: Metadata
    resources:
      - group: ""
        resources: ["secrets"]

  # Log changes to pods, deployments, services at RequestResponse level
  - level: RequestResponse
    verbs: ["create", "update", "patch", "delete"]
    resources:
      - group: ""
        resources: ["pods", "services", "configmaps"]
      - group: "apps"
        resources: ["deployments", "statefulsets", "daemonsets"]

  # Log everything else at Request level
  - level: Request
    resources:
      - group: ""
      - group: "apps"
      - group: "batch"
```

```bash
sudo cp audit-policy.yaml /etc/kubernetes/audit-policy.yaml
```

### Step 2 -- Configure the API Server

Edit the kube-apiserver static pod manifest to enable audit logging.

```bash
sudo cp /etc/kubernetes/manifests/kube-apiserver.yaml /etc/kubernetes/manifests/kube-apiserver.yaml.bak

# Add the following flags to the kube-apiserver command
# Edit /etc/kubernetes/manifests/kube-apiserver.yaml and add under spec.containers[0].command:
```

Add these flags to the API server command:

```yaml
    - --audit-policy-file=/etc/kubernetes/audit-policy.yaml
    - --audit-log-path=/var/log/kubernetes/audit/audit.log
    - --audit-log-maxage=30          # retain audit logs for 30 days
    - --audit-log-maxbackup=10       # keep 10 rotated log files
    - --audit-log-maxsize=100        # rotate after 100 MB
```

Add volume mounts for the audit policy and log directory:

```yaml
    volumeMounts:
      # Add to existing volumeMounts list:
      - name: audit-policy
        mountPath: /etc/kubernetes/audit-policy.yaml
        readOnly: true
      - name: audit-logs
        mountPath: /var/log/kubernetes/audit/
  volumes:
    # Add to existing volumes list:
    - name: audit-policy
      hostPath:
        path: /etc/kubernetes/audit-policy.yaml
        type: File
    - name: audit-logs
      hostPath:
        path: /var/log/kubernetes/audit/
        type: DirectoryOrCreate
```

```bash
# Create the log directory
sudo mkdir -p /var/log/kubernetes/audit/

# The API server will restart automatically when the manifest changes
# Wait for it to come back
sleep 30
kubectl get nodes
```

### Step 3 -- Generate Audit Events

```bash
# Create a test namespace and resources
kubectl create namespace audit-test
kubectl create secret generic db-password -n audit-test --from-literal=password=secret123
kubectl create deployment audit-app --image=nginx:1.27 -n audit-test
kubectl get secrets -n audit-test
kubectl delete deployment audit-app -n audit-test
```

### Step 4 -- Analyze Audit Logs

```bash
# View recent audit events
sudo tail -20 /var/log/kubernetes/audit/audit.log | jq .

# Find secret access events
sudo cat /var/log/kubernetes/audit/audit.log \
  | jq 'select(.objectRef.resource == "secrets")' | head -40

# Find delete operations
sudo cat /var/log/kubernetes/audit/audit.log \
  | jq 'select(.verb == "delete")' | head -40

# Find events by a specific user
sudo cat /var/log/kubernetes/audit/audit.log \
  | jq 'select(.user.username == "kubernetes-admin")' | head -40
```

Audit event key fields:

| Field | Description |
|-------|-------------|
| `verb` | The API verb (get, create, update, delete) |
| `user.username` | Who made the request |
| `objectRef.resource` | The resource type |
| `objectRef.name` | The resource name |
| `objectRef.namespace` | The resource namespace |
| `responseStatus.code` | The HTTP response code |
| `requestReceivedTimestamp` | When the request was received |

## Verify

```bash
# API server is running with audit flags
kubectl get pod -n kube-system -l component=kube-apiserver \
  -o jsonpath='{.items[0].spec.containers[0].command}' | tr ',' '\n' | grep audit

# Audit log file exists and has content
sudo ls -la /var/log/kubernetes/audit/audit.log

# Secret access was logged at Metadata level (no requestObject/responseObject)
sudo cat /var/log/kubernetes/audit/audit.log \
  | jq 'select(.objectRef.resource == "secrets" and .verb == "create") | {verb, level: .level, user: .user.username, name: .objectRef.name}' \
  | head -10
```

## Cleanup

```bash
kubectl delete namespace audit-test

# To remove audit logging, restore the backup manifest:
# sudo cp /etc/kubernetes/manifests/kube-apiserver.yaml.bak /etc/kubernetes/manifests/kube-apiserver.yaml
```

## Reference

- [Auditing](https://kubernetes.io/docs/tasks/debug/debug-cluster/audit/)
- [Audit Policy](https://kubernetes.io/docs/reference/config-api/apiserver-audit.v1/)
