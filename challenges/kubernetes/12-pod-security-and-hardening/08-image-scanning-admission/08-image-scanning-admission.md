# Exercise 8: Image Scanning and Admission Webhooks

<!--
difficulty: advanced
concepts: [validating-admission-webhook, image-scanning, trivy, image-policy, webhook-configuration]
tools: [kubectl, trivy, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security]
-->

## Introduction

Preventing vulnerable or untrusted images from running in your cluster is a critical supply chain security control. This exercise combines **image scanning** (with Trivy) and **ValidatingAdmissionWebhooks** to automatically reject pods that reference images with critical vulnerabilities or that come from untrusted registries.

## Architecture

```
kubectl apply (Pod)
    |
    v
kube-apiserver
    |
    +-- Mutating Admission Webhooks (modify the request)
    |
    +-- Validating Admission Webhooks
    |       |
    |       +-- Image Policy Webhook
    |       |     - Check registry allowlist
    |       |     - Check image tag (reject :latest)
    |       |     - Require digest pinning
    |       |
    |       +-- Vulnerability Scanner Webhook
    |             - Scan image with Trivy
    |             - Reject if critical CVEs found
    |
    v
etcd (pod stored only if all webhooks approve)
```

## Suggested Steps

1. **Scan images locally with Trivy** to understand the baseline:

```bash
# Scan an image for vulnerabilities
trivy image nginx:1.27

# Scan with severity filter
trivy image --severity CRITICAL,HIGH nginx:1.27

# Scan and output JSON for automation
trivy image --format json --output results.json nginx:1.27
```

2. **Create a ValidatingWebhookConfiguration** that enforces image policies. The webhook service checks:
   - Images must come from an approved registry prefix (e.g., `docker.io/library/`, `gcr.io/myproject/`)
   - Images must not use the `:latest` tag
   - Images should be pinned by digest

```yaml
# webhook-config.yaml
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingWebhookConfiguration
metadata:
  name: image-policy-webhook
webhooks:
  - name: image-policy.example.com
    admissionReviewVersions: ["v1"]
    sideEffects: None
    clientConfig:
      service:
        name: image-policy-service
        namespace: webhook-system
        path: /validate
      caBundle: <CA_BUNDLE_BASE64>
    rules:
      - operations: ["CREATE", "UPDATE"]
        apiGroups: [""]
        apiVersions: ["v1"]
        resources: ["pods"]
    failurePolicy: Fail            # reject if webhook is unavailable
    matchPolicy: Equivalent
    namespaceSelector:
      matchExpressions:
        - key: image-policy.example.com/skip
          operator: DoesNotExist   # opt-out label for system namespaces
```

3. **Build a simple webhook server** (or use an existing solution like OPA Gatekeeper or Kyverno). The webhook should:
   - Parse the AdmissionReview request
   - Extract container image references
   - Validate against the policy
   - Return an AdmissionReview response with `allowed: true/false`

4. **Deploy the webhook service** with proper TLS certificates:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: image-policy-service
  namespace: webhook-system
spec:
  replicas: 2
  selector:
    matchLabels:
      app: image-policy-service
  template:
    metadata:
      labels:
        app: image-policy-service
    spec:
      containers:
        - name: webhook
          image: your-registry/image-policy-webhook:v1
          ports:
            - containerPort: 8443
          volumeMounts:
            - name: tls
              mountPath: /etc/webhook/certs
              readOnly: true
      volumes:
        - name: tls
          secret:
            secretName: image-policy-tls
```

5. **Test the webhook** with various images:

```bash
# Should be accepted (approved registry, tagged)
kubectl run test-good --image=nginx:1.27 --dry-run=server

# Should be rejected (latest tag)
kubectl run test-latest --image=nginx:latest --dry-run=server

# Should be rejected (unknown registry)
kubectl run test-bad --image=evil.registry.io/backdoor:v1 --dry-run=server
```

## Verify

```bash
# Check webhook configuration
kubectl get validatingwebhookconfiguration image-policy-webhook

# Check webhook service is running
kubectl get pods -n webhook-system

# Test with dry-run
kubectl run test --image=nginx:1.27 -n default --dry-run=server -o yaml

# Check webhook logs for decisions
kubectl logs -n webhook-system -l app=image-policy-service --tail=20
```

## Cleanup

```bash
kubectl delete validatingwebhookconfiguration image-policy-webhook
kubectl delete namespace webhook-system
```

## What's Next

The next exercise covers **Supply Chain Security** -- verifying image signatures, generating SBOMs, and implementing SLSA provenance.
