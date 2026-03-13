# Exercise 2: Service Accounts and Tokens

<!--
difficulty: basic
concepts: [serviceaccount, automountserviceaccounttoken, projected-volume, tokenrequest-api, bound-token]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [11-rbac-and-authentication/01-rbac-roles-and-bindings]
-->

## Introduction

Every pod in Kubernetes runs under a **ServiceAccount**. That ServiceAccount determines the pod's identity when it talks to the API server. Modern Kubernetes uses **bound service account tokens** -- short-lived JWTs that are automatically rotated -- instead of the legacy static tokens that never expired.

Key concepts:

- **automountServiceAccountToken** -- controls whether a token is automatically mounted into a pod
- **Projected volumes** -- let you mount a token with a specific audience and expiration
- **TokenRequest API** -- generates short-lived tokens on demand via `kubectl create token`

## Why This Matters

A pod that does not need API access should not carry a token at all. If that pod is compromised, the attacker gets a free credential. Understanding token projection and automount control is essential for reducing blast radius.

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: sa-lab
```

### 2. Create a ServiceAccount with automount disabled

```yaml
# serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: app-identity
  namespace: sa-lab
automountServiceAccountToken: false   # no token unless explicitly requested
```

### 3. Create RBAC for the ServiceAccount

```yaml
# rbac.yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pod-list-role
  namespace: sa-lab
rules:
  - apiGroups: [""]
    resources: ["pods"]
    verbs: ["get", "list"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pod-list-binding
  namespace: sa-lab
subjects:
  - kind: ServiceAccount
    name: app-identity
    namespace: sa-lab
roleRef:
  kind: Role
  name: pod-list-role
  apiGroup: rbac.authorization.k8s.io
```

### 4. Pod with no token (automount disabled)

```yaml
# pod-no-token.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-no-token
  namespace: sa-lab
  labels:
    app: no-token
spec:
  serviceAccountName: app-identity   # inherits automount: false
  containers:
    - name: curl
      image: curlimages/curl:8.5.0
      command: ["sleep", "3600"]
```

### 5. Pod with a projected token

```yaml
# pod-projected-token.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-projected-token
  namespace: sa-lab
  labels:
    app: projected-token
spec:
  serviceAccountName: app-identity
  automountServiceAccountToken: false   # still disabled at pod level
  containers:
    - name: curl
      image: curlimages/curl:8.5.0
      command: ["sleep", "3600"]
      volumeMounts:
        - name: sa-token
          mountPath: /var/run/secrets/tokens    # custom path for projected token
          readOnly: true
        - name: ca-cert
          mountPath: /var/run/secrets/kubernetes.io/ca
          readOnly: true
  volumes:
    - name: sa-token
      projected:
        sources:
          - serviceAccountToken:
              path: token
              expirationSeconds: 3600          # 1-hour lifetime, auto-rotated
              audience: api                     # intended audience claim
    - name: ca-cert
      projected:
        sources:
          - configMap:
              name: kube-root-ca.crt           # cluster CA, auto-created per namespace
              items:
                - key: ca.crt
                  path: ca.crt
```

### 6. Pod with automount overridden at pod level

```yaml
# pod-automount.yaml
apiVersion: v1
kind: Pod
metadata:
  name: pod-automount
  namespace: sa-lab
  labels:
    app: automount
spec:
  serviceAccountName: app-identity
  automountServiceAccountToken: true    # overrides the SA-level false
  containers:
    - name: curl
      image: curlimages/curl:8.5.0
      command: ["sleep", "3600"]
```

### 7. Apply everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f serviceaccount.yaml
kubectl apply -f rbac.yaml
kubectl apply -f pod-no-token.yaml
kubectl apply -f pod-projected-token.yaml
kubectl apply -f pod-automount.yaml
```

## Common Mistakes

1. **Assuming automount=false on the SA prevents all tokens** -- A pod can still override it with `automountServiceAccountToken: true` at the pod level. Both levels must be considered.
2. **Forgetting the CA certificate** -- When making HTTPS calls to the API server from inside a pod with a projected token, you need the cluster CA. Mount `kube-root-ca.crt`.
3. **Using legacy static tokens** -- Kubernetes 1.24+ no longer auto-creates Secret-based tokens. Always use the TokenRequest API or projected volumes.
4. **Setting expirationSeconds too low** -- The kubelet needs time to rotate the token before it expires. Minimum recommended is 600 seconds (10 minutes).

## Verify

```bash
# Confirm the ServiceAccount has automount disabled
kubectl get sa app-identity -n sa-lab -o yaml | grep automount

# pod-no-token should NOT have a token mounted
kubectl exec -n sa-lab pod-no-token -- \
  ls /var/run/secrets/kubernetes.io/serviceaccount/ 2>&1
# Expected: "No such file or directory"

# pod-projected-token should have the custom token
kubectl exec -n sa-lab pod-projected-token -- \
  cat /var/run/secrets/tokens/token

# Access the API server from inside the projected-token pod
kubectl exec -n sa-lab pod-projected-token -- sh -c '
  TOKEN=$(cat /var/run/secrets/tokens/token)
  CACERT=/var/run/secrets/kubernetes.io/ca/ca.crt
  curl -s --cacert $CACERT \
    -H "Authorization: Bearer $TOKEN" \
    https://kubernetes.default.svc/api/v1/namespaces/sa-lab/pods
'

# pod-automount should have the standard auto-mounted token
kubectl exec -n sa-lab pod-automount -- \
  cat /var/run/secrets/kubernetes.io/serviceaccount/token

# Generate a token via the TokenRequest API
kubectl create token app-identity \
  -n sa-lab \
  --audience=api \
  --duration=600s
```

## Cleanup

```bash
kubectl delete namespace sa-lab
```

## What's Next

In the next exercise you will learn about **RBAC Verbs, Resources, and API Groups** -- the building blocks of every RBAC rule.

## Summary

- Every pod runs under a ServiceAccount, which is its identity for API access.
- `automountServiceAccountToken: false` prevents automatic token mounting (set it on the SA or the pod).
- Projected volumes let you mount tokens with custom audience and expiration.
- The `TokenRequest API` (`kubectl create token`) generates short-lived tokens on demand.
- Pod-level `automountServiceAccountToken` overrides the ServiceAccount-level setting.
- Always disable automount for pods that do not need API access.

## Reference

- [Service Accounts](https://kubernetes.io/docs/concepts/security/service-accounts/)
- [Configure Service Accounts for Pods](https://kubernetes.io/docs/tasks/configure-pod-container/configure-service-account/)

## Additional Resources

- [TokenRequest API](https://kubernetes.io/docs/reference/kubernetes-api/authentication-resources/token-request-v1/)
- [Projected Volumes](https://kubernetes.io/docs/concepts/storage/projected-volumes/)
