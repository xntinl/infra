# 16.01 Probes: Liveness, Readiness, and Startup

<!--
difficulty: basic
concepts: [livenessProbe, readinessProbe, startupProbe, health-checks, pod-lifecycle]
tools: [kubectl]
estimated_time: 25m
bloom_level: understand
prerequisites: [pods, services, configmaps]
-->

## What You Will Learn

In this exercise you will deploy a Pod that exposes three health-check endpoints:
`/healthz` for liveness, `/ready` for readiness, and `/startup` for startup.
You will observe how Kubernetes restarts the container when the liveness probe
fails, removes it from Service endpoints when readiness fails, and protects it
during initial boot with the startup probe.

## Why It Matters

Without probes, Kubernetes has no way to know whether your application is
actually healthy. A container can be running but completely deadlocked, serving
errors, or still loading data. Probes give Kubernetes the information it needs
to route traffic only to healthy instances and automatically recover from
failures.

## Step-by-Step

### 1 -- Create the Namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: probes-lab
```

### 2 -- Create the ConfigMap

The ConfigMap holds a custom nginx configuration that serves three probe
endpoints, plus an init script that simulates slow startup.

```yaml
# configmap.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: probe-config
  namespace: probes-lab
data:
  nginx.conf: |
    events {}
    http {
      server {
        listen 8080;

        # Liveness endpoint -- returns 200 when /tmp/probes/healthz exists
        location /healthz {
          access_log off;
          default_type text/plain;
          root /tmp/probes;
          try_files /healthz =503;
        }

        # Readiness endpoint -- returns 200 when /tmp/probes/ready exists
        location /ready {
          access_log off;
          default_type text/plain;
          root /tmp/probes;
          try_files /ready =503;
        }

        # Startup endpoint -- returns 200 when /tmp/probes/startup exists
        location /startup {
          access_log off;
          default_type text/plain;
          root /tmp/probes;
          try_files /startup =503;
        }

        location / {
          default_type text/plain;
          return 200 'App is running\n';
        }
      }
    }
  init.sh: |
    #!/bin/sh
    mkdir -p /tmp/probes
    echo "ok" > /tmp/probes/healthz
    echo "ok" > /tmp/probes/ready
    # Simulate slow startup -- startup probe will wait
    sleep 5
    echo "ok" > /tmp/probes/startup
```

### 3 -- Create the Pod with All Three Probes

```yaml
# pod-with-probes.yaml
apiVersion: v1
kind: Pod
metadata:
  name: probe-demo
  namespace: probes-lab
  labels:
    app: probe-demo
spec:
  initContainers:
    - name: init-probes
      image: busybox:1.37
      command: ["sh", "/scripts/init.sh"]
      volumeMounts:
        - name: probe-files
          mountPath: /tmp/probes
        - name: scripts
          mountPath: /scripts
  containers:
    - name: app
      image: nginx:1.27-alpine
      ports:
        - containerPort: 8080
      volumeMounts:
        - name: nginx-config
          mountPath: /etc/nginx/nginx.conf
          subPath: nginx.conf          # mount single file, not the whole dir
        - name: probe-files
          mountPath: /tmp/probes
      # startupProbe -- disables liveness/readiness until it succeeds
      startupProbe:
        httpGet:
          path: /startup
          port: 8080
        initialDelaySeconds: 2         # wait 2s before first check
        periodSeconds: 3               # check every 3s
        failureThreshold: 10           # tolerate 10 failures (= 30s window)
        successThreshold: 1            # one success is enough
      # livenessProbe -- restarts container on failure
      livenessProbe:
        httpGet:
          path: /healthz
          port: 8080
        initialDelaySeconds: 0
        periodSeconds: 5
        failureThreshold: 3            # restart after 3 consecutive failures
        successThreshold: 1
      # readinessProbe -- removes Pod from Service endpoints on failure
      readinessProbe:
        httpGet:
          path: /ready
          port: 8080
        initialDelaySeconds: 0
        periodSeconds: 5
        failureThreshold: 2
        successThreshold: 1
  volumes:
    - name: nginx-config
      configMap:
        name: probe-config
        items:
          - key: nginx.conf
            path: nginx.conf
    - name: scripts
      configMap:
        name: probe-config
        items:
          - key: init.sh
            path: init.sh
        defaultMode: 0755              # make script executable
    - name: probe-files
      emptyDir: {}                     # shared writable volume
```

### 4 -- Create the Service

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: probe-demo-svc
  namespace: probes-lab
spec:
  selector:
    app: probe-demo
  ports:
    - port: 80
      targetPort: 8080
      protocol: TCP
```

### 5 -- Apply Everything

```bash
kubectl apply -f namespace.yaml
kubectl apply -f configmap.yaml
kubectl apply -f pod-with-probes.yaml
kubectl apply -f service.yaml
```

### 6 -- Simulate Failures

Remove the readiness file to pull the Pod from endpoints:

```bash
kubectl exec -n probes-lab probe-demo -- rm /tmp/probes/ready
```

Remove the liveness file to trigger a container restart:

```bash
kubectl exec -n probes-lab probe-demo -- rm /tmp/probes/healthz
```

## Common Mistakes

| Mistake | Why It Fails |
|---------|-------------|
| Putting a liveness probe on a path that depends on external services | Cascading restarts -- if the database is down, every Pod restarts |
| Setting `failureThreshold` too low on startup probe | Slow apps get killed before they finish booting |
| Using the same endpoint for liveness and readiness | You lose the ability to stop traffic without triggering a restart |
| Forgetting `initialDelaySeconds` when there is no startup probe | The liveness probe fires before the app is ready and kills it |

## Verify

```bash
# 1. Watch the Pod come up and pass startup probe
kubectl get pod -n probes-lab probe-demo -w

# 2. Confirm the Pod IP appears in Service endpoints
kubectl get endpoints -n probes-lab probe-demo-svc

# 3. Simulate readiness failure and check endpoints disappear
kubectl exec -n probes-lab probe-demo -- rm /tmp/probes/ready
sleep 15
kubectl get endpoints -n probes-lab probe-demo-svc

# 4. Restore readiness
kubectl exec -n probes-lab probe-demo -- sh -c 'echo ok > /tmp/probes/ready'
sleep 10
kubectl get endpoints -n probes-lab probe-demo-svc

# 5. Simulate liveness failure and watch restart
kubectl exec -n probes-lab probe-demo -- rm /tmp/probes/healthz
sleep 20
kubectl get pod -n probes-lab probe-demo
kubectl describe pod -n probes-lab probe-demo | grep -A 10 "Events"

# 6. Check restart count
kubectl get pod -n probes-lab probe-demo -o jsonpath='{.status.containerStatuses[0].restartCount}'
```

## Cleanup

```bash
kubectl delete namespace probes-lab
```

## What's Next

Continue to [16.02 Metrics Server and kubectl top](../02-metrics-server-and-kubectl-top/) to
learn how Kubernetes exposes resource usage metrics for nodes and pods.

## Summary

- **livenessProbe** restarts a container when it becomes unresponsive or deadlocked.
- **readinessProbe** removes a Pod from Service endpoints so it stops receiving traffic.
- **startupProbe** protects slow-starting containers by disabling liveness and readiness until boot completes.
- Each probe type supports `httpGet`, `exec`, `tcpSocket`, and `grpc` mechanisms.
- Tuning `failureThreshold`, `periodSeconds`, and `initialDelaySeconds` prevents false positives.
- Always use separate endpoints for liveness and readiness to avoid cascading restarts.

## References

- [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
- [Pod Lifecycle -- Container Probes](https://kubernetes.io/docs/concepts/workloads/pods/pod-lifecycle/#container-probes)

## Additional Resources

- [Kubernetes Probes API Reference](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#Probe)
- [Health Check Best Practices (Google Cloud Blog)](https://cloud.google.com/blog/products/containers-kubernetes/kubernetes-best-practices-setting-up-health-checks-with-readiness-and-liveness-probes)
