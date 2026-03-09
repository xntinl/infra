# Exercise 7: Kyverno Image Verification with cosign

<!--
difficulty: advanced
concepts: [kyverno, verifyimages, cosign, image-signature, attestation, notary, sbom-verification]
tools: [kubectl, cosign]
estimated_time: 40m
bloom_level: analyze
prerequisites: [13-policy-engines/02-kyverno-basics, 12-pod-security-and-hardening/09-supply-chain-security]
-->

## Introduction

Kyverno can verify container image signatures as part of the admission process. This ensures that only images signed by trusted parties run in your cluster. Kyverno supports **cosign** (Sigstore), **Notary v2**, and custom key verification. Beyond simple signature checks, Kyverno can also verify **attestations** -- signed metadata about the image such as vulnerability scan results, SBOM, and build provenance.

## Architecture

```
kubectl apply (Pod)
    |
    v
kube-apiserver
    |
    v
Kyverno Admission Webhook
    |
    +-- verifyImages rule
    |     |
    |     +-- Fetch image signature from registry
    |     +-- Verify signature against public key / keyless
    |     +-- (Optional) Verify attestations (vuln scan, SBOM)
    |     +-- Mutate image reference to use digest (immutable)
    |     |
    |     +-- PASS: allow pod creation
    |     +-- FAIL: reject with policy violation message
    |
    v
etcd (pod stored only if verification passes)
```

## Suggested Steps

1. **Create a key pair for signing:**

```bash
cosign generate-key-pair
# Outputs: cosign.key (private), cosign.pub (public)
```

2. **Sign a test image:**

```bash
# Push an image to a registry you control
docker tag nginx:1.27 myregistry.io/nginx:1.27
docker push myregistry.io/nginx:1.27

# Sign it
cosign sign --key cosign.key myregistry.io/nginx:1.27
```

3. **Create a Kyverno policy to verify signatures:**

```yaml
# policy-verify-images.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-image-signatures
spec:
  validationFailureAction: Enforce
  webhookTimeoutSeconds: 30
  rules:
    - name: verify-cosign-signature
      match:
        any:
          - resources:
              kinds:
                - Pod
      verifyImages:
        - imageReferences:
            - "myregistry.io/*"        # only check images from this registry
          attestors:
            - count: 1                  # at least 1 attestor must match
              entries:
                - keys:
                    publicKeys: |
                      -----BEGIN PUBLIC KEY-----
                      <paste-your-cosign.pub-content-here>
                      -----END PUBLIC KEY-----
          mutateDigest: true            # replace tag with digest after verification
          verifyDigest: true            # require digest-pinned references
```

4. **Verify keyless signatures** (using Sigstore's public Fulcio CA):

```yaml
# policy-verify-keyless.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-keyless-signatures
spec:
  validationFailureAction: Enforce
  rules:
    - name: verify-keyless
      match:
        any:
          - resources:
              kinds:
                - Pod
      verifyImages:
        - imageReferences:
            - "ghcr.io/myorg/*"
          attestors:
            - entries:
                - keyless:
                    issuer: "https://token.actions.githubusercontent.com"
                    subject: "https://github.com/myorg/*"
                    rekor:
                      url: "https://rekor.sigstore.dev"
```

5. **Verify attestations** (e.g., vulnerability scan results):

```yaml
# policy-verify-vuln-attestation.yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-vuln-scan
spec:
  validationFailureAction: Enforce
  rules:
    - name: check-vulnerability-attestation
      match:
        any:
          - resources:
              kinds:
                - Pod
      verifyImages:
        - imageReferences:
            - "myregistry.io/*"
          attestors:
            - entries:
                - keys:
                    publicKeys: |
                      -----BEGIN PUBLIC KEY-----
                      <your-key>
                      -----END PUBLIC KEY-----
          attestations:
            - type: https://cosign.sigstore.dev/attestation/vuln/v1
              conditions:
                - all:
                    - key: "{{ scanner }}"
                      operator: Equals
                      value: "trivy"
                    - key: "{{ critical_count }}"
                      operator: Equals
                      value: "0"
```

6. **Test the policies:**

```bash
# Signed image should be accepted
kubectl run signed --image=myregistry.io/nginx:1.27 --dry-run=server

# Unsigned image should be rejected
kubectl run unsigned --image=myregistry.io/untrusted:v1 --dry-run=server 2>&1
# Expected: image verification failed

# Images from non-matched registries are not checked
kubectl run external --image=nginx:1.27 --dry-run=server
# Expected: allowed (not matched by imageReferences)
```

## Verify

```bash
# Check policy status
kubectl get clusterpolicy verify-image-signatures -o yaml | grep -A10 status

# View policy reports for image verification
kubectl get policyreport -A

# Test with dry-run
kubectl run verify-test --image=myregistry.io/nginx:1.27 --dry-run=server -o yaml
# If signed: allowed, image reference mutated to digest
# If unsigned: rejected
```

## Cleanup

```bash
kubectl delete clusterpolicy verify-image-signatures \
  verify-keyless-signatures verify-vuln-scan 2>/dev/null
rm -f cosign.key cosign.pub
```

## What's Next

The next exercise covers **Policy Testing in CI/CD Pipelines** -- validating policies before they reach the cluster.
