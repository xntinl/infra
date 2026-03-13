# Exercise 7: Service Account Token Projection and Rotation

<!--
difficulty: intermediate
concepts: [projected-volume, token-request-api, audience, expiration, token-rotation, bound-token]
tools: [kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [11-rbac-and-authentication/02-service-accounts-and-tokens]
-->

## Introduction

Kubernetes projected service account tokens are JWTs with configurable audience and expiration. The kubelet automatically rotates them before they expire. This exercise explores how to configure tokens for different audiences (e.g., the Kubernetes API vs. an external vault), inspect their claims, and understand the rotation lifecycle.

## Step-by-Step

### 1. Create namespace and ServiceAccount

```yaml
# setup.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: token-lab
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: multi-audience-sa
  namespace: token-lab
automountServiceAccountToken: false
```

### 2. Pod with multiple projected tokens for different audiences

```yaml
# pod-multi-token.yaml
apiVersion: v1
kind: Pod
metadata:
  name: multi-token-pod
  namespace: token-lab
spec:
  serviceAccountName: multi-audience-sa
  automountServiceAccountToken: false
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      volumeMounts:
        - name: k8s-token
          mountPath: /var/run/secrets/tokens/k8s
          readOnly: true
        - name: vault-token
          mountPath: /var/run/secrets/tokens/vault
          readOnly: true
        - name: ca-cert
          mountPath: /var/run/secrets/kubernetes.io/ca
          readOnly: true
  volumes:
    - name: k8s-token
      projected:
        sources:
          - serviceAccountToken:
              path: token
              expirationSeconds: 3600     # 1 hour
              audience: api               # intended for the Kubernetes API
    - name: vault-token
      projected:
        sources:
          - serviceAccountToken:
              path: token
              expirationSeconds: 600      # 10 minutes, shorter for vault
              audience: vault             # intended for HashiCorp Vault
    - name: ca-cert
      projected:
        sources:
          - configMap:
              name: kube-root-ca.crt
              items:
                - key: ca.crt
                  path: ca.crt
```

### 3. Generate tokens via TokenRequest API

```bash
kubectl apply -f setup.yaml
kubectl apply -f pod-multi-token.yaml

# Generate a token for a custom audience
kubectl create token multi-audience-sa \
  -n token-lab \
  --audience=vault \
  --duration=300s

# Generate a token for the default Kubernetes API audience
kubectl create token multi-audience-sa \
  -n token-lab \
  --duration=3600s
```

### 4. Inspect token claims

```bash
# Decode the k8s token JWT payload (base64 middle segment)
kubectl exec -n token-lab multi-token-pod -- \
  cat /var/run/secrets/tokens/k8s/token | \
  cut -d. -f2 | base64 -d 2>/dev/null

# Decode the vault token -- note the different audience claim
kubectl exec -n token-lab multi-token-pod -- \
  cat /var/run/secrets/tokens/vault/token | \
  cut -d. -f2 | base64 -d 2>/dev/null
```

Look for these claims in the decoded JWT:
- `aud` -- the audience(s) the token is valid for
- `exp` -- the expiration timestamp
- `iat` -- the issued-at timestamp
- `sub` -- the subject (system:serviceaccount:namespace:name)

## Spot the Bug

This projected volume configuration is not working. The pod starts but the token file is empty. Why?

```yaml
volumes:
  - name: token
    projected:
      sources:
        - serviceAccountToken:
            path: token
            expirationSeconds: 60
            audience: api
```

<details>
<summary>Answer</summary>

`expirationSeconds: 60` is below the minimum allowed (600 seconds / 10 minutes). The kubelet requires at least 10 minutes to ensure it can rotate the token before expiry. The API server will reject or the kubelet will fail to mount the token.

</details>

## Verify

```bash
# Both token files should exist
kubectl exec -n token-lab multi-token-pod -- ls -la /var/run/secrets/tokens/k8s/token
kubectl exec -n token-lab multi-token-pod -- ls -la /var/run/secrets/tokens/vault/token

# Tokens should be valid JWTs (three dot-separated base64 segments)
kubectl exec -n token-lab multi-token-pod -- \
  cat /var/run/secrets/tokens/k8s/token | grep -c '\.'
# Expected: the token string contains 2 dots (3 segments)

# TokenReview: verify a token with the API server
TOKEN=$(kubectl create token multi-audience-sa -n token-lab --audience=api --duration=600s)
kubectl create -f - <<EOF
apiVersion: authentication.k8s.io/v1
kind: TokenReview
spec:
  token: "$TOKEN"
  audiences: ["api"]
EOF
```

## Cleanup

```bash
kubectl delete namespace token-lab
```

## What's Next

The next exercise covers **RBAC for CI/CD Service Accounts** -- designing permissions for automated pipelines that deploy to your cluster.
