# 4. Service Selectors and Endpoint Objects

<!--
difficulty: basic
concepts: [selectors, endpoints, readiness-probe, label-matching, services-without-selectors]
tools: [kubectl, minikube]
estimated_time: 30m
bloom_level: understand
prerequisites: [05-01, 05-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 02 (ClusterIP Service Fundamentals)](../02-clusterip-service-fundamentals/02-clusterip-service-fundamentals.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** how label selectors connect Services to Pods through Endpoints objects
- **Explain** how readiness probes affect Endpoints membership
- **Create** manual Endpoints for Services without selectors

## Why Selectors and Endpoints?

When you create a Service with a selector, Kubernetes automatically creates an Endpoints object with the same name. This Endpoints object contains the IP addresses and ports of every pod whose labels match the selector AND whose readiness probe is passing. The Endpoints controller continuously watches for pod changes (creation, deletion, readiness transitions) and updates the Endpoints object accordingly. kube-proxy then reads these Endpoints to program its forwarding rules.

Understanding this mechanism is critical for debugging. When a Service does not route traffic correctly, the problem is almost always in the Endpoints -- either the selector does not match, or pods are failing readiness probes.

## Step 1: Deploy Pods with Different Labels

```yaml
# pods.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-stable
spec:
  replicas: 2
  selector:
    matchLabels:
      app: myapp
      track: stable
  template:
    metadata:
      labels:
        app: myapp
        track: stable                    # Only pods with BOTH labels will be selected
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 2
            periodSeconds: 3
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: app-canary
spec:
  replicas: 1
  selector:
    matchLabels:
      app: myapp
      track: canary
  template:
    metadata:
      labels:
        app: myapp
        track: canary
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          readinessProbe:
            httpGet:
              path: /
              port: 80
            initialDelaySeconds: 2
            periodSeconds: 3
```

```bash
kubectl apply -f pods.yaml
```

## Step 2: Create Services with Different Selectors

```yaml
# services.yaml
apiVersion: v1
kind: Service
metadata:
  name: myapp-all                        # Targets ALL myapp pods
spec:
  selector:
    app: myapp                           # Broad selector -- matches stable AND canary
  ports:
    - port: 80
      targetPort: 80
      name: http
---
apiVersion: v1
kind: Service
metadata:
  name: myapp-stable                     # Targets only stable pods
spec:
  selector:
    app: myapp
    track: stable                        # Narrow selector -- stable only
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```bash
kubectl apply -f services.yaml
```

## Step 3: Inspect the Endpoints

```bash
# myapp-all should list 3 pod IPs (2 stable + 1 canary)
kubectl get endpoints myapp-all

# myapp-stable should list only 2 pod IPs (stable only)
kubectl get endpoints myapp-stable
```

For detailed information including pod references:

```bash
kubectl describe endpoints myapp-all
```

## Step 4: Observe Readiness Affecting Endpoints

Watch the endpoints in real-time while you break a pod's readiness:

```bash
# In one terminal, watch endpoints
kubectl get endpoints myapp-all --watch
```

In another terminal, exec into a stable pod and remove the index page to fail the readiness probe:

```bash
STABLE_POD=$(kubectl get pods -l track=stable -o jsonpath='{.items[0].metadata.name}')
kubectl exec $STABLE_POD -- rm /usr/share/nginx/html/index.html
```

Within a few seconds, the Endpoints object removes that pod's IP. The Service stops routing traffic to it. Restore the file to bring it back:

```bash
kubectl exec $STABLE_POD -- sh -c 'echo "OK" > /usr/share/nginx/html/index.html'
```

## Step 5: Create a Service Without Selectors

Services without selectors do not create Endpoints automatically. You must create the Endpoints object manually. This is useful for proxying to external services or databases:

```yaml
# manual-endpoint.yaml
apiVersion: v1
kind: Service
metadata:
  name: external-db
spec:
  # No selector -- Kubernetes will NOT create Endpoints automatically
  ports:
    - port: 5432
      targetPort: 5432
      name: postgres
---
apiVersion: v1
kind: Endpoints
metadata:
  name: external-db                      # Must match the Service name exactly
subsets:
  - addresses:
      - ip: 10.0.0.50                    # IP of your external database
    ports:
      - port: 5432
        name: postgres                   # Must match the port name in the Service
```

```bash
kubectl apply -f manual-endpoint.yaml
```

Now pods can connect to `external-db:5432` and traffic will be forwarded to `10.0.0.50:5432`.

## Common Mistakes

### Mistake 1: Endpoints Name Does Not Match Service Name

The Endpoints object must have exactly the same name as the Service. If they differ, kube-proxy will not associate them and the Service will have no backends.

### Mistake 2: Selector Labels Are Case-Sensitive

Labels `App: myapp` and `app: myapp` are different. A selector for `app` will not match pods labeled `App`.

### Mistake 3: Not Understanding Readiness Impact

Pods that fail readiness probes are removed from Endpoints but not killed. They remain Running but receive no traffic. This is by design -- readiness controls traffic routing, not pod lifecycle.

## Verify What You Learned

```bash
# myapp-all: 3 endpoints, myapp-stable: 2 endpoints
kubectl get endpoints myapp-all myapp-stable

# external-db: 1 manual endpoint
kubectl get endpoints external-db

# Traffic reaches pods through the narrow selector
kubectl run verify --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://myapp-stable
```

## Cleanup

```bash
kubectl delete deployment app-stable app-canary
kubectl delete svc myapp-all myapp-stable external-db
kubectl delete endpoints external-db
```

## What's Next

You now understand how Services discover pods internally. In [exercise 05 (DNS and Service Discovery)](../05-dns-and-service-discovery/05-dns-and-service-discovery.md), you will learn how CoreDNS makes Services reachable by name, including cross-namespace resolution and the resolv.conf configuration inside pods.

## Summary

- **Label selectors** in Services determine which pods receive traffic via **Endpoints** objects.
- Kubernetes automatically creates and updates Endpoints when pods match the selector.
- Pods failing **readiness probes** are removed from Endpoints but keep running.
- Services **without selectors** require manual Endpoints objects (useful for external services).
- The Endpoints object name must **exactly match** the Service name.
- **Multi-label selectors** enable fine-grained routing (e.g., stable vs canary pods).

## Reference

- [Service Selectors](https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors) -- selector and selector-less Services
- [Endpoints](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/endpoints-v1/) -- API reference

## Additional Resources

- [Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/)
- [Configure Liveness, Readiness and Startup Probes](https://kubernetes.io/docs/tasks/configure-pod-container/configure-liveness-readiness-startup-probes/)
