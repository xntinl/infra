# Admission Webhooks: Validating and Mutating

<!--
difficulty: advanced
concepts: [admission-webhooks, validating-webhook, mutating-webhook, webhook-configuration, cert-manager, admission-review]
tools: [kubebuilder, kubectl, openssl, cert-manager]
estimated_time: 45m
bloom_level: analyze
prerequisites: [06-kubebuilder-scaffold, 01-crd-basics]
-->

## Overview

Admission webhooks intercept API server requests before resources are persisted to etcd. Mutating webhooks modify resources (inject sidecars, add labels, set defaults), while validating webhooks reject invalid resources. Together they provide powerful policy enforcement that goes beyond what CRD schemas and CEL rules can express.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                  Admission Pipeline                          │
│                                                               │
│  API Request                                                  │
│      │                                                        │
│      ▼                                                        │
│  Authentication & Authorization                              │
│      │                                                        │
│      ▼                                                        │
│  ┌──────────────────┐    ┌──────────────────┐                │
│  │ Mutating Webhooks │ ──▶│Validating Webhooks│                │
│  │  (modify request) │    │ (accept/reject)   │                │
│  │                    │    │                    │                │
│  │ - Inject sidecar   │    │ - Enforce policies │                │
│  │ - Add labels       │    │ - Cross-resource   │                │
│  │ - Set defaults     │    │   validation       │                │
│  └──────────────────┘    └──────────────────┘                │
│      │                          │                              │
│      ▼                          ▼                              │
│  Schema Validation         Persist to etcd                    │
└─────────────────────────────────────────────────────────────┘
```

Key points:
- Mutating webhooks run first (they can modify the resource)
- Validating webhooks run after (they see the final mutated resource)
- Both receive an `AdmissionReview` request and return an `AdmissionReview` response
- Webhooks require TLS certificates (cert-manager simplifies this)

## Suggested Steps

### 1. Scaffold Webhooks with Kubebuilder

```bash
cd ~/operators/website-operator

# Create validating and mutating webhooks
kubebuilder create webhook --group apps --version v1 --kind Website \
  --defaulting --programmatic-validation

# This generates:
# - api/v1/website_webhook.go (webhook handlers)
# - config/webhook/ (webhook configuration manifests)
# - config/certmanager/ (cert-manager Certificate resources)
```

### 2. Implement the Mutating Webhook (Defaulting)

```go
// api/v1/website_webhook.go
package v1

import (
    "fmt"

    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    logf "sigs.k8s.io/controller-runtime/pkg/log"
    "sigs.k8s.io/controller-runtime/pkg/webhook"
    "sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var websitelog = logf.Log.WithName("website-webhook")

func (r *Website) SetupWebhookWithManager(mgr ctrl.Manager) error {
    return ctrl.NewWebhookManagedBy(mgr).
        For(r).
        Complete()
}

// +kubebuilder:webhook:path=/mutate-apps-example-com-v1-website,mutating=true,failurePolicy=fail,sideEffects=None,groups=apps.example.com,resources=websites,verbs=create;update,versions=v1,name=mwebsite.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &Website{}

// Default implements webhook.Defaulter
func (r *Website) Default() {
    websitelog.Info("mutating webhook called", "name", r.Name)

    // Set default replicas if not specified
    if r.Spec.Replicas == 0 {
        r.Spec.Replicas = 1
    }

    // Inject standard labels
    if r.Labels == nil {
        r.Labels = make(map[string]string)
    }
    r.Labels["managed-by"] = "website-operator"
    r.Labels["domain"] = r.Spec.Domain

    // Set a default annotation with the operator version
    if r.Annotations == nil {
        r.Annotations = make(map[string]string)
    }
    r.Annotations["apps.example.com/operator-version"] = "v0.1.0"
}
```

### 3. Implement the Validating Webhook

```go
// Continue in api/v1/website_webhook.go

// +kubebuilder:webhook:path=/validate-apps-example-com-v1-website,mutating=false,failurePolicy=fail,sideEffects=None,groups=apps.example.com,resources=websites,verbs=create;update;delete,versions=v1,name=vwebsite.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &Website{}

// ValidateCreate implements webhook.Validator
func (r *Website) ValidateCreate() (admission.Warnings, error) {
    websitelog.Info("validating create", "name", r.Name)

    if r.Spec.Replicas > 5 && r.Namespace == "default" {
        return nil, fmt.Errorf("cannot create Website with more than 5 replicas in the default namespace")
    }

    return nil, nil
}

// ValidateUpdate implements webhook.Validator
func (r *Website) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
    websitelog.Info("validating update", "name", r.Name)

    oldWebsite := old.(*Website)

    // Prevent domain changes (immutable field)
    if r.Spec.Domain != oldWebsite.Spec.Domain {
        return nil, fmt.Errorf("spec.domain is immutable; delete and recreate the Website to change it")
    }

    // Warn about large scale-ups
    var warnings admission.Warnings
    if r.Spec.Replicas > oldWebsite.Spec.Replicas+3 {
        warnings = append(warnings, "Scaling up by more than 3 replicas at once")
    }

    return warnings, nil
}

// ValidateDelete implements webhook.Validator
func (r *Website) ValidateDelete() (admission.Warnings, error) {
    websitelog.Info("validating delete", "name", r.Name)

    // Prevent deletion of production websites during business hours
    // (simplified example)
    if r.Labels["environment"] == "production" {
        return admission.Warnings{"Deleting a production Website"}, nil
    }

    return nil, nil
}
```

### 4. Deploy with cert-manager

Webhooks require TLS. cert-manager automates certificate provisioning.

```bash
# Install cert-manager
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=120s

# Enable cert-manager in the Kubebuilder config
# Uncomment cert-manager sections in config/default/kustomization.yaml

# Build and deploy
make manifests generate
make docker-build IMG=website-operator:v0.1.0
make deploy IMG=website-operator:v0.1.0
```

### 5. Test the Webhooks

```bash
# Test mutating webhook: create without labels
kubectl apply -f - <<'EOF'
apiVersion: apps.example.com/v1
kind: Website
metadata:
  name: test-mutation
spec:
  domain: test.example.com
  image: nginx:1.27
EOF

# Verify labels were injected
kubectl get website test-mutation -o jsonpath='{.metadata.labels}'
# Should include managed-by: website-operator

# Test validating webhook: try to change domain
kubectl patch website test-mutation --type=merge -p '{"spec":{"domain":"changed.example.com"}}'
# Expected: rejected -- spec.domain is immutable

# Test validating webhook: too many replicas in default namespace
kubectl apply -f - <<'EOF'
apiVersion: apps.example.com/v1
kind: Website
metadata:
  name: test-validation
spec:
  domain: big.example.com
  image: nginx:1.27
  replicas: 10
EOF
# Expected: rejected -- cannot create with more than 5 replicas in default namespace
```

## Verify

```bash
# Webhook configurations are registered
kubectl get mutatingwebhookconfigurations
kubectl get validatingwebhookconfigurations

# Mutated resource has injected labels
kubectl get website test-mutation -o yaml | grep "managed-by"

# Validation rejects invalid changes
kubectl patch website test-mutation --type=merge -p '{"spec":{"domain":"new.com"}}' 2>&1 | grep "immutable"
```

## Cleanup

```bash
kubectl delete websites --all
make undeploy
make uninstall
kubectl delete -f https://github.com/cert-manager/cert-manager/releases/download/v1.14.0/cert-manager.yaml
```

## Reference

- [Admission Webhooks](https://kubernetes.io/docs/reference/access-authn-authz/extensible-admission-controllers/)
- [Kubebuilder Webhook Guide](https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation)
- [cert-manager](https://cert-manager.io/docs/)
