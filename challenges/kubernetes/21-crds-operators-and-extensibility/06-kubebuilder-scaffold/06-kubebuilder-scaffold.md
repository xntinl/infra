# Kubebuilder: Scaffold an Operator

<!--
difficulty: advanced
concepts: [kubebuilder, operator-sdk, scaffolding, api-types, controller, manager, rbac-markers]
tools: [kubebuilder, go, make, kubectl, docker]
estimated_time: 45m
bloom_level: analyze
prerequisites: [05-operator-pattern, 01-crd-basics]
-->

## Overview

Kubebuilder is the official Kubernetes project for building operators in Go. It scaffolds the boilerplate code for CRDs, controllers, webhooks, and RBAC, letting you focus on the reconciliation logic. This exercise walks through scaffolding a complete operator project, understanding the generated code, and running it locally.

## Architecture

```
┌──────────────────────────────────────────────────────────┐
│                  Kubebuilder Project                      │
│                                                            │
│  cmd/main.go                                               │
│  ├── Creates Manager (shared dependencies)                │
│  ├── Registers Controllers with Manager                   │
│  └── Starts Manager (blocking)                            │
│                                                            │
│  api/v1/                                                   │
│  ├── <kind>_types.go     ─── Go structs for CRD spec/status│
│  └── groupversion_info.go ── API group registration        │
│                                                            │
│  internal/controller/                                      │
│  └── <kind>_controller.go ── Reconcile logic               │
│                                                            │
│  config/                                                   │
│  ├── crd/         ── Generated CRD YAML                    │
│  ├── rbac/        ── RBAC manifests from markers            │
│  ├── manager/     ── Deployment for the operator            │
│  └── default/     ── Kustomize overlay combining all        │
└──────────────────────────────────────────────────────────┘
```

## Suggested Steps

### 1. Install Prerequisites

```bash
# Install Go (1.22+)
go version

# Install Kubebuilder
curl -L -o kubebuilder "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
chmod +x kubebuilder
sudo mv kubebuilder /usr/local/bin/

kubebuilder version
```

### 2. Initialize the Project

```bash
mkdir -p ~/operators/website-operator && cd ~/operators/website-operator

# Initialize with domain and repo
kubebuilder init --domain example.com --repo github.com/example/website-operator

# Examine the generated structure
ls -la
cat PROJECT
cat cmd/main.go
```

### 3. Create an API (CRD + Controller)

```bash
# Scaffold a new API resource with a controller
kubebuilder create api --group apps --version v1 --kind Website --resource --controller

# This generates:
# - api/v1/website_types.go       (CRD types)
# - internal/controller/website_controller.go  (reconcile logic)
# - config/crd/bases/              (CRD YAML)
# - config/rbac/                   (RBAC for the controller)
# - config/samples/                (sample CR YAML)
```

### 4. Define the API Types

Edit `api/v1/website_types.go` to define your custom resource spec and status:

```go
// api/v1/website_types.go
package v1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// WebsiteSpec defines the desired state of Website
type WebsiteSpec struct {
    // Domain is the FQDN for this website
    // +kubebuilder:validation:Required
    // +kubebuilder:validation:MinLength=1
    Domain string `json:"domain"`

    // Image is the container image to serve the website
    // +kubebuilder:validation:Required
    Image string `json:"image"`

    // Replicas is the desired number of pods
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=10
    // +kubebuilder:default=1
    Replicas int32 `json:"replicas,omitempty"`
}

// WebsiteStatus defines the observed state of Website
type WebsiteStatus struct {
    // Phase represents the current lifecycle phase
    // +kubebuilder:validation:Enum=Pending;Deploying;Running;Failed
    Phase string `json:"phase,omitempty"`

    // AvailableReplicas is the number of ready pods
    AvailableReplicas int32 `json:"availableReplicas,omitempty"`

    // URL is the endpoint where the website is accessible
    URL string `json:"url,omitempty"`

    // Conditions represent the latest observations
    Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Domain",type=string,JSONPath=`.spec.domain`
// +kubebuilder:printcolumn:name="Image",type=string,JSONPath=`.spec.image`
// +kubebuilder:printcolumn:name="Replicas",type=integer,JSONPath=`.spec.replicas`
// +kubebuilder:printcolumn:name="Status",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Website is the Schema for the websites API
type Website struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   WebsiteSpec   `json:"spec,omitempty"`
    Status WebsiteStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WebsiteList contains a list of Website
type WebsiteList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []Website `json:"items"`
}

func init() {
    SchemeBuilder.Register(&Website{}, &WebsiteList{})
}
```

### 5. Generate CRD Manifests and RBAC

```bash
# Regenerate CRD YAML, DeepCopy methods, and RBAC from markers
make generate
make manifests

# Examine the generated CRD
cat config/crd/bases/apps.example.com_websites.yaml

# Install the CRD into the cluster
make install

# Verify
kubectl get crd websites.apps.example.com
```

### 6. Run the Controller Locally

```bash
# Run the controller outside the cluster (connects via kubeconfig)
make run

# In another terminal, create a sample resource
kubectl apply -f config/samples/apps_v1_website.yaml

# Watch the controller logs for reconciliation
```

### 7. Build and Deploy to Cluster

```bash
# Build the container image
make docker-build IMG=website-operator:v0.1.0

# Load into kind (if using kind)
kind load docker-image website-operator:v0.1.0

# Deploy the operator to the cluster
make deploy IMG=website-operator:v0.1.0

# Verify the operator is running
kubectl get pods -n website-operator-system
```

## Verify

```bash
# CRD is installed
kubectl get crd websites.apps.example.com

# Operator pod is running
kubectl get pods -n website-operator-system

# Sample resource can be created
kubectl apply -f config/samples/apps_v1_website.yaml
kubectl get websites
```

## Cleanup

```bash
# Undeploy the operator
make undeploy

# Uninstall the CRD
make uninstall

# Remove the project directory
cd ~ && rm -rf ~/operators/website-operator
```

## Reference

- [Kubebuilder Book](https://book.kubebuilder.io/)
- [Kubebuilder Markers](https://book.kubebuilder.io/reference/markers)
- [controller-runtime](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
