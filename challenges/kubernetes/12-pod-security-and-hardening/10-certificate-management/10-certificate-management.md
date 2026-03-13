# Exercise 10: Certificate Management with cert-manager

<!--
difficulty: advanced
concepts: [cert-manager, issuer, clusterissuer, certificate, acme, self-signed, ca, tls-secrets]
tools: [kubectl, helm]
estimated_time: 40m
bloom_level: analyze
prerequisites: [12-pod-security-and-hardening/06-secrets-encryption-at-rest]
-->

## Introduction

cert-manager automates the lifecycle of TLS certificates in Kubernetes: provisioning, renewal, and revocation. It integrates with ACME (Let's Encrypt), HashiCorp Vault, Venafi, and self-signed CAs. Certificates are stored as Kubernetes Secrets and can be consumed by Ingress controllers, Pods, and webhooks.

## Architecture

```
cert-manager controller
    |
    +-- Watches Certificate resources
    +-- Creates CertificateRequest
    +-- Talks to Issuer (self-signed, CA, ACME, Vault)
    +-- Stores issued cert as TLS Secret
    +-- Monitors expiration, auto-renews
    |
    v
TLS Secret (tls.crt + tls.key)
    |
    +-- Consumed by Ingress
    +-- Consumed by Pod volumeMount
    +-- Consumed by webhook service
```

## Suggested Steps

1. **Install cert-manager:**

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml

# Wait for all pods to be ready
kubectl wait --for=condition=ready pod -l app.kubernetes.io/instance=cert-manager \
  -n cert-manager --timeout=120s
```

2. **Create a self-signed ClusterIssuer** (for testing and internal CAs):

```yaml
# self-signed-issuer.yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: self-signed
spec:
  selfSigned: {}
```

3. **Create a CA issuer** (internal PKI):

```yaml
# ca-certificate.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: internal-ca
  namespace: cert-manager
spec:
  isCA: true
  commonName: internal-ca
  secretName: internal-ca-secret
  duration: 87600h       # 10 years
  renewBefore: 720h      # renew 30 days before expiry
  privateKey:
    algorithm: ECDSA
    size: 256
  issuerRef:
    name: self-signed
    kind: ClusterIssuer
---
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: internal-ca-issuer
spec:
  ca:
    secretName: internal-ca-secret
```

4. **Issue a certificate for a workload:**

```yaml
# workload-cert.yaml
apiVersion: cert-manager.io/v1
kind: Certificate
metadata:
  name: web-app-tls
  namespace: default
spec:
  secretName: web-app-tls-secret
  duration: 2160h          # 90 days
  renewBefore: 360h        # renew 15 days before expiry
  commonName: web-app.default.svc.cluster.local
  dnsNames:
    - web-app.default.svc.cluster.local
    - web-app.default.svc
    - web-app
  privateKey:
    algorithm: RSA
    size: 2048
  issuerRef:
    name: internal-ca-issuer
    kind: ClusterIssuer
```

5. **Consume the certificate in a Pod:**

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: web-app
  namespace: default
spec:
  containers:
    - name: nginx
      image: nginx:1.27
      volumeMounts:
        - name: tls
          mountPath: /etc/tls
          readOnly: true
  volumes:
    - name: tls
      secret:
        secretName: web-app-tls-secret   # created by cert-manager
```

6. **Configure an ACME issuer** (Let's Encrypt) for production:

```yaml
apiVersion: cert-manager.io/v1
kind: ClusterIssuer
metadata:
  name: letsencrypt-prod
spec:
  acme:
    server: https://acme-v02.api.letsencrypt.org/directory
    email: admin@example.com
    privateKeySecretRef:
      name: letsencrypt-prod-key
    solvers:
      - http01:
          ingress:
            class: nginx
```

## Verify

```bash
# Check cert-manager is running
kubectl get pods -n cert-manager

# Check issuers
kubectl get clusterissuers

# Check certificate status
kubectl get certificates -A
kubectl describe certificate web-app-tls -n default

# Check the generated Secret
kubectl get secret web-app-tls-secret -n default -o yaml

# Verify the certificate content
kubectl get secret web-app-tls-secret -n default \
  -o jsonpath='{.data.tls\.crt}' | base64 -d | openssl x509 -text -noout

# Check certificate renewal timeline
kubectl get certificate web-app-tls -n default \
  -o jsonpath='{.status.renewalTime}'
```

## Cleanup

```bash
kubectl delete certificate web-app-tls -n default
kubectl delete secret web-app-tls-secret -n default
kubectl delete clusterissuer self-signed internal-ca-issuer letsencrypt-prod
kubectl delete certificate internal-ca -n cert-manager
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.16.3/cert-manager.yaml
```

## What's Next

The next exercise is **Full CIS Benchmark Cluster Hardening** -- an insane-level challenge where you harden an entire cluster against the CIS Kubernetes Benchmark.
