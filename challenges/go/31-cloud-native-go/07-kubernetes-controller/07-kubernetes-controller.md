# 7. Kubernetes Controller

<!--
difficulty: advanced
concepts: [controller-runtime, reconciler, reconcile-loop, manager, custom-resource]
tools: [go, kubectl, kind]
estimated_time: 45m
bloom_level: evaluate
prerequisites: [kubernetes-client-go, interfaces, context, error-handling]
-->

## Prerequisites

- Go 1.22+ installed
- Completed Kubernetes client-go exercise
- A local Kubernetes cluster (kind or minikube)
- Understanding of the Kubernetes controller pattern (desired state vs actual state)

## Learning Objectives

After completing this exercise, you will be able to:

- **Implement** a Kubernetes controller using `controller-runtime`
- **Evaluate** reconciliation loop design: idempotency, requeue strategies, status updates
- **Build** a controller that watches one resource and manages another
- **Test** controllers using `envtest` or fake clients

## Why Kubernetes Controllers Matter

Controllers are the core pattern of Kubernetes. Every built-in resource (Deployments, Services, Ingresses) is managed by a controller running a reconciliation loop. The loop compares desired state (the spec) with actual state and makes changes to converge them.

`controller-runtime` is the library that powers kubebuilder and operator-sdk. Understanding how to write a `Reconciler`, set up watches, handle errors with requeue, and update status subresources is essential for building operators.

## The Problem

Build a controller that watches ConfigMaps with a specific label (`app.kubernetes.io/managed-by: configsync`) and ensures a corresponding Secret exists with the same data (base64-encoded values). When the ConfigMap changes, the Secret is updated. When the ConfigMap is deleted, the Secret is garbage collected via owner references.

## Requirements

1. **Reconciler** -- implement the `reconcile.Reconciler` interface with a `Reconcile(ctx, req) (reconcile.Result, error)` method
2. **Watch** -- watch ConfigMaps with a label selector filter
3. **Desired state** -- for each matching ConfigMap, ensure a Secret exists in the same namespace with the same name and data
4. **Owner references** -- set the ConfigMap as the owner of the Secret so deletion cascades
5. **Idempotency** -- the reconcile function must be safe to call multiple times with the same input
6. **Status** -- update the ConfigMap with an annotation indicating the last sync time
7. **Tests** -- use a fake client to test reconciliation logic

## Hints

<details>
<summary>Hint 1: Reconciler struct</summary>

```go
type ConfigSyncReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

func (r *ConfigSyncReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
    var cm corev1.ConfigMap
    if err := r.Get(ctx, req.NamespacedName, &cm); err != nil {
        return reconcile.Result{}, client.IgnoreNotFound(err)
    }
    // reconciliation logic
}
```

</details>

<details>
<summary>Hint 2: Creating or updating the Secret</summary>

```go
secret := &corev1.Secret{
    ObjectMeta: metav1.ObjectMeta{
        Name:      cm.Name,
        Namespace: cm.Namespace,
    },
}

_, err := ctrl.CreateOrUpdate(ctx, r.Client, secret, func() error {
    secret.Data = make(map[string][]byte)
    for k, v := range cm.Data {
        secret.Data[k] = []byte(v)
    }
    return ctrl.SetControllerReference(&cm, secret, r.Scheme)
})
```

</details>

<details>
<summary>Hint 3: Setting up the manager</summary>

```go
mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
    Scheme: scheme,
})

err = ctrl.NewControllerManagedBy(mgr).
    For(&corev1.ConfigMap{}).
    Owns(&corev1.Secret{}).
    WithEventFilter(predicate.NewPredicateFuncs(func(obj client.Object) bool {
        return obj.GetLabels()["app.kubernetes.io/managed-by"] == "configsync"
    })).
    Complete(&ConfigSyncReconciler{
        Client: mgr.GetClient(),
        Scheme: mgr.GetScheme(),
    })
```

</details>

<details>
<summary>Hint 4: Testing with fake client</summary>

```go
fakeClient := fake.NewClientBuilder().
    WithScheme(scheme).
    WithObjects(&corev1.ConfigMap{
        ObjectMeta: metav1.ObjectMeta{
            Name:      "test-config",
            Namespace: "default",
            Labels:    map[string]string{"app.kubernetes.io/managed-by": "configsync"},
        },
        Data: map[string]string{"key": "value"},
    }).
    Build()

reconciler := &ConfigSyncReconciler{Client: fakeClient, Scheme: scheme}
result, err := reconciler.Reconcile(ctx, reconcile.Request{
    NamespacedName: types.NamespacedName{Name: "test-config", Namespace: "default"},
})
```

</details>

## Verification

```bash
go test -v -race ./...
```

Your tests should confirm:
- Reconciling a labeled ConfigMap creates a Secret with matching data
- Reconciling again (idempotency) does not produce errors or duplicate resources
- Updating ConfigMap data triggers an update to the Secret
- The Secret has an owner reference pointing to the ConfigMap
- Reconciling a non-existent ConfigMap returns without error (not found is ignored)

## What's Next

Continue to [08 - Terraform Provider Skeleton](../08-terraform-provider-skeleton/08-terraform-provider-skeleton.md) to build a Terraform provider using the plugin framework.

## Summary

- Kubernetes controllers run reconciliation loops that converge actual state toward desired state
- `controller-runtime` provides the `Reconciler` interface, manager, and watch setup
- Use `ctrl.CreateOrUpdate` for idempotent create-or-update operations
- Owner references enable automatic garbage collection when the parent is deleted
- Predicate filters limit which resources trigger reconciliation
- Test controllers with fake clients to avoid needing a real cluster

## Reference

- [controller-runtime documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime)
- [Kubebuilder book](https://book.kubebuilder.io/)
- [Controller pattern](https://kubernetes.io/docs/concepts/architecture/controller/)
