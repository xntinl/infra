# 16.06 Graceful Shutdown with preStop and Readiness

<!--
difficulty: intermediate
concepts: [preStop-hook, graceful-shutdown, terminationGracePeriodSeconds, SIGTERM, connection-draining]
tools: [kubectl]
estimated_time: 20m
bloom_level: apply
prerequisites: [probes-liveness-readiness-startup, deployments, services]
-->

## What You Will Learn

In this exercise you will configure a Pod to drain in-flight requests before
terminating. You will combine a `preStop` lifecycle hook with a readiness probe
to ensure the Pod is removed from Service endpoints before the process shuts
down, preventing connection errors during rolling updates.

## Step-by-Step

### 1 -- Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: graceful-lab
```

### 2 -- Deploy an Application with Graceful Shutdown

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: graceful-app
  namespace: graceful-lab
spec:
  replicas: 3
  selector:
    matchLabels:
      app: graceful-app
  template:
    metadata:
      labels:
        app: graceful-app
    spec:
      terminationGracePeriodSeconds: 60    # total time allowed for shutdown
      containers:
        - name: app
          image: nginx:1.27-alpine
          ports:
            - containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: 80
            periodSeconds: 2
            failureThreshold: 1            # remove from endpoints immediately
          lifecycle:
            preStop:
              exec:
                command:
                  - sh
                  - -c
                  # 1. Signal nginx to stop accepting new connections
                  # 2. Sleep to allow kube-proxy to update iptables
                  # 3. Then let nginx drain gracefully
                  - |
                    nginx -s quit
                    sleep 15
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 100m
              memory: 128Mi
---
apiVersion: v1
kind: Service
metadata:
  name: graceful-app
  namespace: graceful-lab
spec:
  selector:
    app: graceful-app
  ports:
    - port: 80
      targetPort: 80
```

### 3 -- Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f deployment.yaml
```

### 4 -- Observe the Shutdown Sequence

Open two terminals. In the first, watch endpoints:

```bash
kubectl get endpoints -n graceful-lab graceful-app -w
```

In the second, trigger a rolling update:

```bash
kubectl set image deployment/graceful-app -n graceful-lab app=nginx:1.27-alpine --record
# Or simply delete a Pod
kubectl delete pod -n graceful-lab -l app=graceful-app --wait=false
```

Observe that the Pod IP is removed from endpoints before the container exits.

### 5 -- Understand the Shutdown Timeline

```text
1. API server sets Pod status to Terminating
2. Endpoints controller removes Pod from Service (readiness stops)
3. kubelet runs preStop hook (sleep 15 + nginx -s quit)
4. kubelet sends SIGTERM to PID 1
5. Container has terminationGracePeriodSeconds (60s) to exit
6. If still running, kubelet sends SIGKILL
```

## Verify

```bash
# 1. All Pods should be Running
kubectl get pods -n graceful-lab

# 2. Delete a Pod and observe graceful transition
kubectl delete pod -n graceful-lab -l app=graceful-app --wait=false
kubectl get events -n graceful-lab --sort-by='.lastTimestamp' | tail -10

# 3. Confirm no 502/503 during rolling update
kubectl rollout restart deployment/graceful-app -n graceful-lab
kubectl rollout status deployment/graceful-app -n graceful-lab
```

## Cleanup

```bash
kubectl delete namespace graceful-lab
```

## What's Next

Continue to [16.07 Logging with FluentBit DaemonSet](../07-logging-with-fluentbit-daemonset/07-logging-with-fluentbit-daemonset.md)
to set up centralized log collection across the cluster.

## Summary

- `preStop` hooks run before SIGTERM, giving time for connection draining.
- The sleep in preStop accounts for the delay between endpoint removal and
  kube-proxy iptables updates.
- `terminationGracePeriodSeconds` is the total budget for preStop + SIGTERM handling.
- Set readiness `failureThreshold: 1` for fast endpoint removal during shutdown.
- Without preStop, clients may hit a terminating Pod during rolling updates.

## References

- [Container Lifecycle Hooks](https://kubernetes.io/docs/concepts/containers/container-lifecycle-hooks/)
- [Pod Termination](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#pod-termination)
