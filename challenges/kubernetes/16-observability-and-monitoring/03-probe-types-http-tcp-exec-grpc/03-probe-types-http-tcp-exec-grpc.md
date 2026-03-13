# 16.03 Probe Types: HTTP, TCP, Exec, and gRPC

<!--
difficulty: basic
concepts: [httpGet-probe, tcpSocket-probe, exec-probe, grpc-probe, container-health-checks]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [pods, probes-basics]
-->

## What You Will Learn

In this exercise you will configure each of the four probe mechanisms Kubernetes
supports: HTTP GET, TCP socket, command execution (exec), and gRPC. You will
understand when to use each type and observe their behavior when probes pass
and fail.

## Why It Matters

Different applications expose health in different ways. A web server returns
HTTP status codes, a database accepts TCP connections, a legacy process writes
a file, and a gRPC service implements the health protocol. Choosing the right
probe type ensures Kubernetes accurately reflects the real state of your
container.

## Step-by-Step

### 1 -- Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: probe-types-lab
```

### 2 -- HTTP GET Probe

The most common type. Kubernetes sends an HTTP GET request and expects a 2xx or
3xx response.

```yaml
# http-probe.yaml
apiVersion: v1
kind: Pod
metadata:
  name: http-probe
  namespace: probe-types-lab
  labels:
    probe: http
spec:
  containers:
    - name: web
      image: nginx:1.27-alpine
      ports:
        - containerPort: 80
      livenessProbe:
        httpGet:
          path: /              # the URL path to check
          port: 80             # must match a container port
          httpHeaders:         # optional custom headers
            - name: X-Health-Check
              value: "true"
        initialDelaySeconds: 3
        periodSeconds: 5
        failureThreshold: 3
```

### 3 -- TCP Socket Probe

Kubernetes opens a TCP connection to the specified port. If the connection
succeeds, the probe passes. Ideal for databases or services without an HTTP
endpoint.

```yaml
# tcp-probe.yaml
apiVersion: v1
kind: Pod
metadata:
  name: tcp-probe
  namespace: probe-types-lab
  labels:
    probe: tcp
spec:
  containers:
    - name: db
      image: redis:7
      ports:
        - containerPort: 6379
      livenessProbe:
        tcpSocket:
          port: 6379           # TCP connection attempt to this port
        initialDelaySeconds: 5
        periodSeconds: 10
        failureThreshold: 3
      readinessProbe:
        tcpSocket:
          port: 6379
        initialDelaySeconds: 3
        periodSeconds: 5
```

### 4 -- Exec Probe

Kubernetes runs a command inside the container. Exit code 0 means healthy,
anything else means failure. Useful for checking files, processes, or running
a CLI health command.

```yaml
# exec-probe.yaml
apiVersion: v1
kind: Pod
metadata:
  name: exec-probe
  namespace: probe-types-lab
  labels:
    probe: exec
spec:
  containers:
    - name: app
      image: busybox:1.37
      command: ["sh", "-c"]
      args:
        - |
          # Create a health file on startup
          touch /tmp/healthy
          echo "App running..."
          sleep 3600
      livenessProbe:
        exec:
          command:             # command to run inside the container
            - cat
            - /tmp/healthy     # succeeds (exit 0) if file exists
        initialDelaySeconds: 5
        periodSeconds: 5
        failureThreshold: 3
```

### 5 -- gRPC Probe (Kubernetes 1.27+)

Kubernetes calls the gRPC Health Checking Protocol on the specified port. The
container must implement `grpc.health.v1.Health/Check`.

```yaml
# grpc-probe.yaml
apiVersion: v1
kind: Pod
metadata:
  name: grpc-probe
  namespace: probe-types-lab
  labels:
    probe: grpc
spec:
  containers:
    - name: grpc-server
      image: registry.k8s.io/e2e-test-images/agnhost:2.40
      command: ["/agnhost", "grpc-health-checking"]
      ports:
        - containerPort: 5000
      livenessProbe:
        grpc:
          port: 5000           # port running gRPC health service
          service: ""          # empty = check overall server health
        initialDelaySeconds: 5
        periodSeconds: 10
        failureThreshold: 3
```

### 6 -- Apply Everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f http-probe.yaml
kubectl apply -f tcp-probe.yaml
kubectl apply -f exec-probe.yaml
kubectl apply -f grpc-probe.yaml
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Using `httpGet` for a service that only speaks TCP | Probe always fails because there is no HTTP response |
| Pointing `tcpSocket` at a port the process does not listen on | Connection refused every check, container keeps restarting |
| Using `exec` with a long-running command | Probe times out (default 1s) and reports failure |
| Forgetting that gRPC probes require the health protocol | The container must implement `grpc.health.v1.Health` |

## Verify

```bash
# 1. All four Pods should be Running
kubectl get pods -n probe-types-lab

# 2. Describe each Pod to see probe configuration and events
kubectl describe pod -n probe-types-lab http-probe
kubectl describe pod -n probe-types-lab tcp-probe
kubectl describe pod -n probe-types-lab exec-probe
kubectl describe pod -n probe-types-lab grpc-probe

# 3. Simulate exec probe failure by removing the health file
kubectl exec -n probe-types-lab exec-probe -- rm /tmp/healthy
sleep 20
kubectl get pod -n probe-types-lab exec-probe
# Expect: RESTARTS >= 1

# 4. Watch events across the namespace
kubectl get events -n probe-types-lab --sort-by='.lastTimestamp'
```

## Cleanup

```bash
kubectl delete namespace probe-types-lab
```

## What's Next

Continue to [16.04 Kubernetes Events](../04-kubernetes-events/04-kubernetes-events.md) to learn how
to monitor and export the events that probes and other components generate.

## Summary

- **httpGet** sends an HTTP request and checks the status code (2xx/3xx = healthy).
- **tcpSocket** attempts a TCP connection -- success means the port is open.
- **exec** runs a command inside the container -- exit code 0 means healthy.
- **grpc** calls the standard gRPC Health Checking Protocol (Kubernetes 1.27+).
- Choose the probe type that matches how your application exposes health.
- All four types support `initialDelaySeconds`, `periodSeconds`, `failureThreshold`, and `timeoutSeconds`.

## References

- [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Pod Lifecycle -- Types of Probe](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#types-of-probe)

## Additional Resources

- [gRPC Health Checking Protocol](https://github.com/grpc/grpc/blob/master/doc/health-checking.md)
- [Probe v1 API Reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#Probe)
