# 6. Kubernetes client-go

<!--
difficulty: advanced
concepts: [client-go, clientset, kubeconfig, list-watch, informers, in-cluster-config]
tools: [go, kubectl, kind]
estimated_time: 40m
bloom_level: analyze
prerequisites: [interfaces, context, error-handling, json-encoding]
-->

## Prerequisites

- Go 1.22+ installed
- `kubectl` and a local Kubernetes cluster (kind or minikube)
- Understanding of Kubernetes resources (pods, deployments, namespaces)
- Familiarity with Go interfaces and context

## Learning Objectives

After completing this exercise, you will be able to:

- **Use** `client-go` to connect to a Kubernetes cluster and list resources
- **Analyze** the difference between in-cluster and out-of-cluster configuration
- **Implement** List, Get, and Watch operations on Kubernetes resources
- **Design** a program that uses Informers for efficient resource watching

## Why client-go Matters

`client-go` is the official Go client library for Kubernetes. Every controller, operator, and Kubernetes tool written in Go uses it. Understanding its authentication model, API patterns, and caching mechanisms (informers) is essential for building anything that interacts with a Kubernetes cluster programmatically.

The library provides three levels of abstraction: raw REST calls, typed clientsets (strongly typed methods per resource), and informers (cached, event-driven watches). Most production code uses informers for efficiency.

## The Problem

Build a CLI tool that connects to a Kubernetes cluster and provides information about running workloads. The tool should:

1. Authenticate using kubeconfig (out-of-cluster)
2. List pods in a namespace with their status
3. Watch for pod changes and print events in real time
4. Use informers for efficient caching

## Requirements

1. **Kubeconfig loading** -- use `clientcmd.BuildConfigFromFlags` for out-of-cluster, fall back to `rest.InClusterConfig` for in-cluster
2. **List pods** -- list all pods in a given namespace, print name, phase, and restart count
3. **Watch pods** -- use a Watch to stream pod events (ADDED, MODIFIED, DELETED)
4. **Informer** -- create a pod informer that caches pods and invokes event handlers on Add/Update/Delete
5. **Graceful shutdown** -- use context cancellation to stop the informer cleanly
6. **Tests** -- test the pod listing and filtering logic using fake clientset

## Hints

<details>
<summary>Hint 1: Creating a clientset</summary>

```go
import (
    "k8s.io/client-go/kubernetes"
    "k8s.io/client-go/tools/clientcmd"
    "k8s.io/client-go/util/homedir"
    "path/filepath"
)

func newClientset() (*kubernetes.Clientset, error) {
    kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
    config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
    if err != nil {
        return nil, err
    }
    return kubernetes.NewForConfig(config)
}
```

</details>

<details>
<summary>Hint 2: Listing pods</summary>

```go
pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
for _, pod := range pods.Items {
    fmt.Printf("%-40s %-12s %d\n", pod.Name, pod.Status.Phase, getRestartCount(pod))
}
```

</details>

<details>
<summary>Hint 3: Using informers</summary>

```go
import "k8s.io/client-go/informers"

factory := informers.NewSharedInformerFactoryWithOptions(
    clientset, 30*time.Second,
    informers.WithNamespace(namespace),
)

podInformer := factory.Core().V1().Pods().Informer()
podInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
    AddFunc:    func(obj interface{}) { /* handle add */ },
    UpdateFunc: func(old, new interface{}) { /* handle update */ },
    DeleteFunc: func(obj interface{}) { /* handle delete */ },
})

factory.Start(ctx.Done())
factory.WaitForCacheSync(ctx.Done())
```

</details>

<details>
<summary>Hint 4: Fake clientset for testing</summary>

```go
import "k8s.io/client-go/kubernetes/fake"

clientset := fake.NewSimpleClientset(
    &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{Name: "test-pod", Namespace: "default"},
        Status:     corev1.PodStatus{Phase: corev1.PodRunning},
    },
)
```

</details>

## Verification

```bash
# Ensure you have a local cluster
kind create cluster --name test 2>/dev/null || true

# Run the pod lister
go run main.go -namespace kube-system

# Run tests with fake clientset
go test -v -race ./...
```

Your program should:
- List pods in the specified namespace with name, phase, and restart count
- Watch mode prints ADDED/MODIFIED/DELETED events as they occur
- Tests pass using fake clientset without a real cluster
- Graceful shutdown on SIGINT/SIGTERM

## What's Next

Continue to [07 - Kubernetes Controller](../07-kubernetes-controller/07-kubernetes-controller.md) to build a reconciliation-loop controller using controller-runtime.

## Summary

- `client-go` is the official Go client for Kubernetes, used by all Go-based controllers and operators
- Use `clientcmd.BuildConfigFromFlags` for out-of-cluster and `rest.InClusterConfig` for in-cluster
- Typed clientsets provide strongly typed methods like `CoreV1().Pods(ns).List()`
- Informers cache resources locally and trigger event handlers, avoiding repeated API calls
- Use `fake.NewSimpleClientset` to test Kubernetes interactions without a real cluster

## Reference

- [client-go documentation](https://pkg.go.dev/k8s.io/client-go)
- [client-go examples](https://github.com/kubernetes/client-go/tree/master/examples)
- [Informers design](https://pkg.go.dev/k8s.io/client-go/informers)
