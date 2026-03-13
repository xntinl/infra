# Controller-Runtime: Reconciliation Loop

<!--
difficulty: advanced
concepts: [controller-runtime, reconcile, client, scheme, owner-references, finalizers, requeue, event-filtering]
tools: [kubebuilder, go, kubectl]
estimated_time: 45m
bloom_level: analyze
prerequisites: [06-kubebuilder-scaffold, 05-operator-pattern]
-->

## Overview

The controller-runtime library provides the core building blocks for Kubernetes controllers in Go: the Manager, Controller, Reconciler, and Client. This exercise focuses on implementing a complete reconciliation loop that creates and manages child resources (Deployments and Services) based on a custom resource, handles updates, and cleans up on deletion using owner references and finalizers.

## Architecture

```
┌────────────────────────────────────────────────────────────────┐
│                    Controller-Runtime                           │
│                                                                  │
│  Manager                                                         │
│  ├── Shared Client (reads/writes to API server)                 │
│  ├── Shared Cache (informer-based, reduces API load)            │
│  ├── Scheme (type registry)                                      │
│  └── Controllers                                                 │
│      └── WebsiteReconciler                                       │
│          ├── Watches: Website (primary), Deployment (secondary) │
│          └── Reconcile(ctx, req) → (Result, error)              │
│                                                                  │
│  Reconcile Flow:                                                 │
│  1. Fetch the Website CR (Get)                                  │
│  2. If deleted → handle finalizer cleanup                       │
│  3. Create/Update Deployment (CreateOrUpdate)                   │
│  4. Create/Update Service (CreateOrUpdate)                      │
│  5. Read Deployment status                                       │
│  6. Update Website status (Status().Update)                     │
│  7. Return Result{} or Result{RequeueAfter: ...}               │
└────────────────────────────────────────────────────────────────┘
```

## Suggested Steps

### 1. Implement the Reconcile Function

Building on the Kubebuilder scaffold from Exercise 06, implement the complete reconciler:

```go
// internal/controller/website_controller.go
package controller

import (
    "context"
    "fmt"
    "time"

    appsv1 "k8s.io/api/apps/v1"
    corev1 "k8s.io/api/core/v1"
    "k8s.io/apimachinery/pkg/api/errors"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
    "k8s.io/apimachinery/pkg/runtime"
    "k8s.io/apimachinery/pkg/types"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
    "sigs.k8s.io/controller-runtime/pkg/log"

    appsv1alpha1 "github.com/example/website-operator/api/v1"
)

const websiteFinalizer = "apps.example.com/finalizer"

type WebsiteReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=apps.example.com,resources=websites,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.example.com,resources=websites/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=services,verbs=get;list;watch;create;update;patch;delete

func (r *WebsiteReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    logger := log.FromContext(ctx)

    // Step 1: Fetch the Website resource
    website := &appsv1alpha1.Website{}
    if err := r.Get(ctx, req.NamespacedName, website); err != nil {
        if errors.IsNotFound(err) {
            logger.Info("Website resource not found, skipping")
            return ctrl.Result{}, nil
        }
        return ctrl.Result{}, err
    }

    // Step 2: Handle deletion with finalizer
    if website.DeletionTimestamp != nil {
        if controllerutil.ContainsFinalizer(website, websiteFinalizer) {
            // Perform cleanup logic here (e.g., external resources)
            logger.Info("Running finalizer cleanup", "website", website.Name)

            controllerutil.RemoveFinalizer(website, websiteFinalizer)
            if err := r.Update(ctx, website); err != nil {
                return ctrl.Result{}, err
            }
        }
        return ctrl.Result{}, nil
    }

    // Add finalizer if not present
    if !controllerutil.ContainsFinalizer(website, websiteFinalizer) {
        controllerutil.AddFinalizer(website, websiteFinalizer)
        if err := r.Update(ctx, website); err != nil {
            return ctrl.Result{}, err
        }
    }

    // Step 3: Create or update the Deployment
    deployment := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      website.Name,
            Namespace: website.Namespace,
        },
    }

    result, err := controllerutil.CreateOrUpdate(ctx, r.Client, deployment, func() error {
        replicas := website.Spec.Replicas
        deployment.Spec = appsv1.DeploymentSpec{
            Replicas: &replicas,
            Selector: &metav1.LabelSelector{
                MatchLabels: map[string]string{"app": website.Name},
            },
            Template: corev1.PodTemplateSpec{
                ObjectMeta: metav1.ObjectMeta{
                    Labels: map[string]string{"app": website.Name},
                },
                Spec: corev1.PodSpec{
                    Containers: []corev1.Container{{
                        Name:  "web",
                        Image: website.Spec.Image,
                        Ports: []corev1.ContainerPort{{
                            ContainerPort: 80,
                        }},
                    }},
                },
            },
        }
        // Set owner reference for garbage collection
        return controllerutil.SetControllerReference(website, deployment, r.Scheme)
    })
    if err != nil {
        return ctrl.Result{}, err
    }
    logger.Info("Deployment reconciled", "result", result)

    // Step 4: Create or update the Service
    service := &corev1.Service{
        ObjectMeta: metav1.ObjectMeta{
            Name:      website.Name,
            Namespace: website.Namespace,
        },
    }

    _, err = controllerutil.CreateOrUpdate(ctx, r.Client, service, func() error {
        service.Spec = corev1.ServiceSpec{
            Selector: map[string]string{"app": website.Name},
            Ports: []corev1.ServicePort{{
                Port:     80,
                Protocol: corev1.ProtocolTCP,
            }},
        }
        return controllerutil.SetControllerReference(website, service, r.Scheme)
    })
    if err != nil {
        return ctrl.Result{}, err
    }

    // Step 5: Update status from Deployment
    currentDeployment := &appsv1.Deployment{}
    if err := r.Get(ctx, types.NamespacedName{Name: website.Name, Namespace: website.Namespace}, currentDeployment); err != nil {
        return ctrl.Result{}, err
    }

    website.Status.AvailableReplicas = currentDeployment.Status.AvailableReplicas
    if currentDeployment.Status.AvailableReplicas == website.Spec.Replicas {
        website.Status.Phase = "Running"
    } else {
        website.Status.Phase = "Deploying"
    }
    website.Status.URL = fmt.Sprintf("http://%s.%s.svc.cluster.local", website.Name, website.Namespace)

    if err := r.Status().Update(ctx, website); err != nil {
        return ctrl.Result{}, err
    }

    // Requeue after 30 seconds to refresh status
    return ctrl.Result{RequeueAfter: 30 * time.Second}, nil
}

// SetupWithManager registers watches for owned resources
func (r *WebsiteReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&appsv1alpha1.Website{}).        // Primary watch
        Owns(&appsv1.Deployment{}).          // Watch owned Deployments
        Owns(&corev1.Service{}).             // Watch owned Services
        Complete(r)
}
```

### 2. Understand Key Concepts

**Owner References**: Setting `controllerutil.SetControllerReference()` means when the Website CR is deleted, Kubernetes garbage-collects the Deployment and Service automatically.

**CreateOrUpdate**: This function fetches the resource, calls your mutate function, and either creates or updates it. The mutate function must be idempotent.

**Finalizers**: The finalizer prevents the CR from being deleted until cleanup logic runs. This is essential when the operator manages external resources (databases, DNS records, cloud resources).

**RequeueAfter**: Returns control to the work queue and re-triggers reconciliation after a delay. Use this for polling external state or periodic status refreshes.

### 3. Test the Reconciliation Loop

```bash
make run  # run locally

# Create a Website
kubectl apply -f - <<'EOF'
apiVersion: apps.example.com/v1
kind: Website
metadata:
  name: test-site
spec:
  domain: test.example.com
  image: nginx:1.27
  replicas: 2
EOF

# Verify child resources are created
kubectl get deployment test-site
kubectl get service test-site
kubectl get website test-site

# Update replicas -- controller should update the deployment
kubectl patch website test-site --type=merge -p '{"spec":{"replicas":4}}'
sleep 5
kubectl get deployment test-site

# Delete -- finalizer runs, then garbage collection removes children
kubectl delete website test-site
kubectl get deployment test-site  # should be gone
```

## Verify

```bash
# Deployment has owner reference pointing to Website
kubectl get deployment test-site -o jsonpath='{.metadata.ownerReferences[0].kind}'
# Expected: Website

# Status is updated
kubectl get website test-site -o jsonpath='{.status.phase}'

# Service exists with correct selector
kubectl get service test-site -o jsonpath='{.spec.selector}'
```

## Cleanup

```bash
kubectl delete websites --all
make undeploy
make uninstall
```

## Reference

- [controller-runtime Client](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/client)
- [controllerutil](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/controller/controllerutil)
- [Owner References](https://kubernetes.io/docs/concepts/overview/working-with-objects/owners-dependents/)
- [Finalizers](https://kubernetes.io/docs/concepts/overview/working-with-objects/finalizers/)
