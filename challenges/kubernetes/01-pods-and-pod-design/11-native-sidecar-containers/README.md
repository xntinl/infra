# 11. Native Sidecar Containers (KEP-753)

<!--
difficulty: advanced
concepts: [native-sidecar, KEP-753, restartPolicy-Always, init-container-lifecycle, sidecar-ordering]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [01-06, 01-07]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running **v1.29+** (native sidecar containers GA)
- `kubectl` installed and configured
- Completion of [exercise 06 (Multi-Container Patterns)](../06-multi-container-patterns/) and [exercise 07 (Init Containers)](../07-init-containers/)

Verify sidecar support:

```bash
kubectl version --short 2>/dev/null || kubectl version
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how native sidecar containers differ from regular init containers and regular containers
- **Apply** native sidecars for logging, proxying, and health-checking patterns
- **Evaluate** the startup and shutdown ordering guarantees of native sidecars

## Architecture

Native sidecars are defined in the `initContainers` array with `restartPolicy: Always`. This gives them unique lifecycle properties:

```
Startup order:    init-containers (sequential) -> native-sidecars (sequential) -> main containers (parallel)
Shutdown order:   main containers terminate -> native-sidecars terminate -> Pod complete
```

Unlike regular sidecars (two containers in the `containers` array), native sidecars:
1. Start before main containers, guaranteeing the sidecar is ready when the app starts
2. Stay running after main containers exit, enabling log flushing and connection draining
3. Are restarted by the kubelet if they crash, independent of the Pod restart policy

## Steps

### 1. Create a Native Sidecar for Log Shipping

Build a Pod where a native sidecar reads logs produced by the main container:

```yaml
# native-sidecar-logging.yaml
apiVersion: v1
kind: Pod
metadata:
  name: native-sidecar-logging
spec:
  initContainers:
    - name: log-shipper
      image: busybox:1.37
      restartPolicy: Always
      command: ["sh", "-c", "echo 'Log shipper started' && tail -F /var/log/app/app.log 2>/dev/null"]
      volumeMounts:
        - name: logs
          mountPath: /var/log/app
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "for i in $(seq 1 10); do echo \"$(date) - Event $i\" >> /var/log/app/app.log; sleep 2; done; echo 'App finished'"]
      volumeMounts:
        - name: logs
          mountPath: /var/log/app
  volumes:
    - name: logs
      emptyDir: {}
```

```bash
kubectl apply -f native-sidecar-logging.yaml
```

### 2. Observe Startup Ordering

```bash
kubectl get pod native-sidecar-logging -w
```

The log-shipper sidecar starts first (shown as `Init:0/1` briefly, then transitions). The main container starts after the sidecar is running.

### 3. Observe Shutdown Ordering

Wait for the main container to finish (about 20 seconds), then check:

```bash
sleep 25
kubectl get pod native-sidecar-logging
kubectl logs native-sidecar-logging -c log-shipper --tail=5
```

The sidecar continues running after the main container exits, capturing all remaining log lines. The Pod status should show the main container completed while the sidecar is still running.

### 4. Build a Sidecar Proxy Pattern

Create a Pod where a native sidecar provides an Envoy-like proxy that must start before the application:

```yaml
# sidecar-proxy.yaml
apiVersion: v1
kind: Pod
metadata:
  name: sidecar-proxy
spec:
  initContainers:
    - name: proxy
      image: busybox:1.37
      restartPolicy: Always
      command: ["sh", "-c", "echo 'Proxy accepting connections on :8080' && while true; do echo -e 'HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nOK' | nc -l -p 8080 -w 1; done"]
      ports:
        - containerPort: 8080
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c", "sleep 2 && wget -qO- http://localhost:8080 && echo ' - App got response from proxy' && sleep 3600"]
```

```bash
kubectl apply -f sidecar-proxy.yaml
sleep 10
kubectl logs sidecar-proxy -c app
```

The app container successfully connects to the proxy because the native sidecar started first.

### 5. Compare with Regular Multi-Container

If both were in the `containers` array, there would be a race condition: the app might try to connect before the proxy is ready. Native sidecars eliminate this race.

## Verify What You Learned

```bash
kubectl get pod native-sidecar-logging -o jsonpath='{.spec.initContainers[0].restartPolicy}'
# Expected: Always

kubectl get pod sidecar-proxy
# Expected: Running, 2/2

kubectl logs sidecar-proxy -c app | grep "proxy"
# Expected: line showing successful proxy connection
```

## Cleanup

```bash
kubectl delete pod native-sidecar-logging sidecar-proxy
```

## Summary

- Native sidecars use `restartPolicy: Always` in the `initContainers` array
- They start **before** main containers and stop **after** them
- The kubelet restarts crashed native sidecars independently of the Pod's restart policy
- Use native sidecars for proxies, log shippers, and any service the main app depends on at startup
- This eliminates the startup race condition inherent in regular multi-container Pods

## Reference

- [Sidecar Containers](https://kubernetes.io/docs/concepts/workloads/pods/sidecar-containers/) — official concept documentation
- [KEP-753: Sidecar Containers](https://github.com/kubernetes/enhancements/tree/master/keps/sig-node/753-sidecar-containers) — design proposal
- [Init Containers](https://kubernetes.io/docs/concepts/workloads/pods/init-containers/) — foundation for native sidecars
