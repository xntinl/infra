# Exercise 10: User Impersonation and Audit Logging

<!--
difficulty: advanced
concepts: [impersonation, audit-policy, audit-log, audit-webhook, user-identity, rbac-escalation]
tools: [kubectl, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings, 11-rbac-and-authentication/06-rbac-debugging]
-->

## Introduction

Kubernetes **impersonation** lets a privileged user act as another user, group, or ServiceAccount without needing their credentials. Combined with **audit logging**, it provides both a debugging tool and a security trail. Audit policies control which API events are recorded and at what level of detail.

## Architecture

```
Admin User
    |
    |  --as=developer --as-group=team-a
    v
kube-apiserver
    |
    +-- Impersonation check: does admin have impersonate verb?
    |       Subject: admin user
    |       Resource: users, groups, serviceaccounts
    |       Verb: impersonate
    |
    +-- RBAC check: does "developer" in "team-a" have the requested permission?
    |
    +-- Audit log: records both the real identity and the impersonated identity
    v
Audit Backend (log file / webhook)
```

## Suggested Steps

1. **Create impersonation RBAC.** Grant a ServiceAccount the ability to impersonate specific users and groups:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: impersonator
rules:
  - apiGroups: [""]
    resources: ["users", "groups", "serviceaccounts"]
    verbs: ["impersonate"]
  - apiGroups: ["authentication.k8s.io"]
    resources: ["userextras/scopes"]
    verbs: ["impersonate"]
```

To restrict which identities can be impersonated, use `resourceNames`:

```yaml
rules:
  - apiGroups: [""]
    resources: ["users"]
    resourceNames: ["developer@example.com", "qa@example.com"]
    verbs: ["impersonate"]
```

2. **Configure an audit policy.** Create a policy that records RequestResponse for Secrets and Metadata for everything else:

```yaml
# audit-policy.yaml
apiVersion: audit.k8s.io/v1
kind: Policy
rules:
  - level: RequestResponse
    resources:
      - group: ""
        resources: ["secrets"]
  - level: Metadata
    resources:
      - group: ""
        resources: ["pods", "services", "configmaps"]
      - group: "apps"
        resources: ["deployments"]
  - level: None
    resources:
      - group: ""
        resources: ["endpoints", "events"]
    verbs: ["get", "list", "watch"]
  - level: Metadata
    omitStages:
      - RequestReceived
```

3. **Enable audit logging in the API server.** For a kind cluster:

```yaml
# kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    kubeadmConfigPatches:
      - |
        kind: ClusterConfiguration
        apiServer:
          extraArgs:
            audit-policy-file: /etc/kubernetes/audit/audit-policy.yaml
            audit-log-path: /var/log/kubernetes/audit.log
            audit-log-maxage: "7"
            audit-log-maxbackup: "3"
            audit-log-maxsize: "100"
          extraVolumes:
            - name: audit-policy
              hostPath: /etc/kubernetes/audit
              mountPath: /etc/kubernetes/audit
              readOnly: true
            - name: audit-log
              hostPath: /var/log/kubernetes
              mountPath: /var/log/kubernetes
    extraMounts:
      - hostPath: ./audit-policy.yaml
        containerPath: /etc/kubernetes/audit/audit-policy.yaml
        readOnly: true
```

4. **Test impersonation and review audit logs:**

```bash
# Impersonate a user
kubectl get pods -n default \
  --as=developer@example.com \
  --as-group=team-a

# Impersonate a ServiceAccount
kubectl get pods -n kube-system \
  --as=system:serviceaccount:default:my-sa

# Review audit logs (inside the kind node)
docker exec -it kind-control-plane \
  cat /var/log/kubernetes/audit.log | tail -20
```

## Verify

```bash
# Confirm impersonation works
kubectl auth can-i list pods --as=developer@example.com -n default

# Check audit log contains impersonation events
# Look for "impersonatedUser" field in audit entries
docker exec -it kind-control-plane \
  grep "impersonatedUser" /var/log/kubernetes/audit.log | tail -5

# Verify audit policy is active
kubectl get pod -n kube-system -l component=kube-apiserver -o yaml | \
  grep audit-policy-file
```

## Cleanup

```bash
kubectl delete clusterrole impersonator 2>/dev/null
kubectl delete clusterrolebinding impersonator-binding 2>/dev/null
# If using kind: kind delete cluster
```

## What's Next

The next exercise is **Multi-Tenant RBAC Platform Design** -- an insane-level challenge where you design a complete multi-tenant RBAC system.
