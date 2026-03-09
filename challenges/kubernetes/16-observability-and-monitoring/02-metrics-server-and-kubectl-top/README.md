# 16.02 Metrics Server and kubectl top

<!--
difficulty: basic
concepts: [metrics-server, kubectl-top, resource-metrics-api, cpu-memory-monitoring]
tools: [kubectl, helm]
estimated_time: 20m
bloom_level: understand
prerequisites: [pods, deployments, resource-requests-limits]
-->

## What You Will Learn

In this exercise you will install the Kubernetes Metrics Server, verify the
`metrics.k8s.io` API is available, and use `kubectl top` to monitor real-time
CPU and memory consumption of nodes and pods. You will also compare actual
resource usage against declared requests and limits.

## Why It Matters

The Metrics Server is the foundation of Kubernetes resource awareness. Without
it, Horizontal Pod Autoscaler (HPA) and Vertical Pod Autoscaler (VPA) cannot
function, and you have no built-in way to see what your workloads actually
consume. Understanding observed usage versus declared requests is essential for
right-sizing applications.

## Step-by-Step

### 1 -- Install Metrics Server

```bash
# Option 1: Official manifest
kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml

# For local clusters (kind, minikube) add --kubelet-insecure-tls
kubectl patch deployment metrics-server -n kube-system \
  --type='json' \
  -p='[{"op": "add", "path": "/spec/template/spec/containers/0/args/-", "value": "--kubelet-insecure-tls"}]'
```

```bash
# Option 2: Helm
helm repo add metrics-server https://kubernetes-sigs.github.io/metrics-server/
helm repo update
helm install metrics-server metrics-server/metrics-server \
  --namespace kube-system \
  --set args[0]="--kubelet-insecure-tls"
```

### 2 -- Create the Namespace and a Deployment with Resource Declarations

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: metrics-lab
```

```yaml
# deployment-with-resources.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: resource-demo
  namespace: metrics-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: resource-demo
  template:
    metadata:
      labels:
        app: resource-demo
    spec:
      containers:
        - name: app
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m        # 50 millicores requested
              memory: 64Mi    # 64 MiB requested
            limits:
              cpu: 200m       # hard cap at 200 millicores
              memory: 128Mi   # OOMKilled above 128 MiB
```

### 3 -- Deploy a CPU Stress Pod

```yaml
# cpu-stress.yaml
apiVersion: v1
kind: Pod
metadata:
  name: cpu-stress
  namespace: metrics-lab
spec:
  containers:
    - name: stress
      image: busybox:1.37
      command: ["sh", "-c"]
      args:
        - |
          while true; do
            dd if=/dev/zero of=/dev/null bs=1M count=100 2>/dev/null
          done
      resources:
        requests:
          cpu: 100m
          memory: 32Mi
        limits:
          cpu: 250m
          memory: 64Mi
```

### 4 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment-with-resources.yaml
kubectl apply -f cpu-stress.yaml
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Running `kubectl top` immediately after installing Metrics Server | Metrics take 60-90 seconds to become available |
| Forgetting `--kubelet-insecure-tls` on local clusters | Metrics Server cannot reach kubelets with self-signed certs |
| Confusing `requests` with actual usage | Requests are scheduling hints, not measured consumption |
| Assuming Metrics Server stores history | It only provides current point-in-time values |

## Verify

```bash
# 1. Confirm Metrics Server is running
kubectl get deployment metrics-server -n kube-system
kubectl get pods -n kube-system -l k8s-app=metrics-server

# 2. Check the metrics API is registered
kubectl get apiservices | grep metrics

# 3. View node metrics (wait 1-2 min after install)
kubectl top nodes

# 4. View pod metrics sorted by CPU
kubectl top pods -n metrics-lab --sort-by=cpu

# 5. View per-container metrics
kubectl top pods -n metrics-lab --containers

# 6. Compare real usage vs declared requests
echo "--- Actual usage ---"
kubectl top pods -n metrics-lab
echo ""
echo "--- Declared requests/limits ---"
kubectl get pods -n metrics-lab -o custom-columns=\
"NAME:.metadata.name,CPU_REQ:.spec.containers[*].resources.requests.cpu,MEM_REQ:.spec.containers[*].resources.requests.memory,CPU_LIM:.spec.containers[*].resources.limits.cpu,MEM_LIM:.spec.containers[*].resources.limits.memory"

# 7. Query the raw metrics API
kubectl get --raw "/apis/metrics.k8s.io/v1beta1/namespaces/metrics-lab/pods" | python3 -m json.tool
```

## Cleanup

```bash
kubectl delete namespace metrics-lab
```

## What's Next

Continue to [16.03 Probe Types: HTTP, TCP, Exec, and gRPC](../03-probe-types-http-tcp-exec-grpc/)
to explore the four probe mechanisms Kubernetes supports.

## Summary

- Metrics Server aggregates CPU and memory data from kubelets and exposes it
  via the `metrics.k8s.io` API.
- `kubectl top nodes` and `kubectl top pods` show real-time resource consumption.
- The `--containers` flag breaks metrics down per container inside a pod.
- Metrics Server is a prerequisite for HPA and VPA.
- Actual usage often differs significantly from declared requests -- monitoring
  both is key to right-sizing.

## References

- [Resource Metrics Pipeline](https://kubernetes.io/docs/tasks/debug/debug-cluster/resource-metrics-pipeline/)
- [Metrics Server GitHub](https://github.com/kubernetes-sigs/metrics-server)

## Additional Resources

- [Managing Resources for Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
- [kubectl top Reference](https://kubernetes.io/docs/reference/kubectl/generated/kubectl_top/)
- [API Aggregation Layer](https://kubernetes.io/docs/concepts/extend-kubernetes/api-extension/apiserver-aggregation/)
