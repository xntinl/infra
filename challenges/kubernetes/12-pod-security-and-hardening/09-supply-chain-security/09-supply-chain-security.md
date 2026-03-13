# Exercise 9: Supply Chain Security: cosign, SBOM, SLSA

<!--
difficulty: advanced
concepts: [cosign, image-signing, sbom, slsa-provenance, sigstore, image-digest, oci-artifacts]
tools: [kubectl, cosign, syft, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [12-pod-security-and-hardening/08-image-scanning-admission]
-->

## Introduction

Supply chain security ensures that the software running in your cluster is exactly what was built by your CI/CD pipeline, with no tampering between build and deploy. Three key practices:

- **Image signing with cosign** -- cryptographically sign container images so Kubernetes can verify authenticity before running them
- **SBOM (Software Bill of Materials)** -- a machine-readable inventory of all dependencies in an image, enabling vulnerability tracking
- **SLSA (Supply-chain Levels for Software Artifacts)** -- a framework for build provenance, proving *how* and *where* an artifact was built

## Architecture

```
CI/CD Pipeline
    |
    +-- Build image
    +-- Generate SBOM (syft)
    +-- Sign image (cosign sign)
    +-- Attach SBOM as OCI artifact (cosign attach)
    +-- Generate SLSA provenance attestation
    +-- Push to registry
    |
    v
Kubernetes Cluster
    |
    +-- Admission webhook verifies cosign signature
    +-- Rejects unsigned or tampered images
    +-- (Optional) Verifies SBOM and provenance
    v
Pod runs only if all checks pass
```

## Suggested Steps

1. **Generate a cosign key pair:**

```bash
cosign generate-key-pair
# Creates cosign.key (private) and cosign.pub (public)
```

2. **Sign a container image:**

```bash
# Tag and push your image
docker tag myapp:latest myregistry.io/myapp:v1
docker push myregistry.io/myapp:v1

# Sign the image
cosign sign --key cosign.key myregistry.io/myapp:v1
```

3. **Verify the signature:**

```bash
cosign verify --key cosign.pub myregistry.io/myapp:v1
```

4. **Generate an SBOM with syft:**

```bash
# Generate SBOM in SPDX format
syft myregistry.io/myapp:v1 -o spdx-json > sbom.spdx.json

# Attach the SBOM to the image as an OCI artifact
cosign attach sbom --sbom sbom.spdx.json myregistry.io/myapp:v1
```

5. **Use image digests instead of tags** for immutable references:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: pinned-image
spec:
  containers:
    - name: app
      # Digest-pinned: immutable, cannot be replaced
      image: myregistry.io/myapp@sha256:abc123def456...
```

6. **Enforce signature verification with Kyverno:**

```yaml
apiVersion: kyverno.io/v1
kind: ClusterPolicy
metadata:
  name: verify-image-signatures
spec:
  validationFailureAction: Enforce
  rules:
    - name: verify-cosign-signature
      match:
        any:
          - resources:
              kinds: ["Pod"]
      verifyImages:
        - imageReferences: ["myregistry.io/*"]
          attestors:
            - entries:
                - keys:
                    publicKeys: |
                      -----BEGIN PUBLIC KEY-----
                      <your-cosign-public-key>
                      -----END PUBLIC KEY-----
```

7. **SLSA provenance:** Build with a SLSA-compliant builder (e.g., GitHub Actions with slsa-github-generator) to produce a provenance attestation that proves the build environment and inputs.

## Verify

```bash
# Verify image signature
cosign verify --key cosign.pub myregistry.io/myapp:v1

# List attestations
cosign tree myregistry.io/myapp:v1

# Verify SBOM attachment
cosign verify-attestation --key cosign.pub \
  --type spdxjson myregistry.io/myapp:v1

# Test Kyverno policy enforcement
kubectl run unsigned --image=myregistry.io/unsigned-app:v1 --dry-run=server 2>&1
# Expected: rejected by policy

kubectl run signed --image=myregistry.io/myapp:v1 --dry-run=server 2>&1
# Expected: allowed
```

## Cleanup

```bash
kubectl delete clusterpolicy verify-image-signatures 2>/dev/null
rm -f cosign.key cosign.pub sbom.spdx.json
```

## What's Next

The next exercise covers **Certificate Management with cert-manager** -- automated TLS certificate provisioning and renewal for your cluster.
