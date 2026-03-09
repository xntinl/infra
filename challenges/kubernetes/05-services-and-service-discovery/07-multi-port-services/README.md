# 7. Multi-Port Services and Named Ports

<!--
difficulty: intermediate
concepts: [multi-port-service, named-ports, targetPort-by-name, port-naming]
tools: [kubectl, minikube]
estimated_time: 25m
bloom_level: apply
prerequisites: [05-02, 05-04]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 04 (Service Selectors and Endpoint Objects)](../04-service-selectors-and-endpoints/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** Services exposing multiple ports with required naming conventions
- **Apply** named targetPort references for version-safe port mapping
- **Differentiate** between numeric and named targetPort behavior during rolling updates

## The Challenge

Build a multi-port Service that exposes both HTTP (port 80) and metrics (port 9090) endpoints. Use named ports in the pod spec so the Service references port names rather than numbers, enabling safe container port changes during rolling updates.

### Step 1: Deploy an Application with Multiple Ports

```yaml
# app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: multi-port-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: multi-port-app
  template:
    metadata:
      labels:
        app: multi-port-app
    spec:
      containers:
        - name: app
          image: nginx:1.27
          ports:
            - containerPort: 80
              name: http               # Named port in the pod spec
            - containerPort: 9090
              name: metrics            # Second named port
          command:
            - /bin/sh
            - -c
            - |
              cat > /etc/nginx/conf.d/metrics.conf << 'CONF'
              server {
                  listen 9090;
                  location /metrics {
                      return 200 'app_requests_total 42\n';
                      add_header Content-Type text/plain;
                  }
              }
              CONF
              nginx -g "daemon off;"
```

```bash
kubectl apply -f app.yaml
```

### Step 2: Create a Multi-Port Service

When a Service has multiple ports, each **must** have a `name`:

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: multi-port-svc
spec:
  selector:
    app: multi-port-app
  ports:
    - name: http                   # Required when multiple ports exist
      port: 80
      targetPort: http             # References the pod's named port, not a number
      protocol: TCP
    - name: metrics
      port: 9090
      targetPort: metrics          # Also by name -- pod can change port number safely
      protocol: TCP
```

```bash
kubectl apply -f service.yaml
```

### Step 3: Test Both Ports

```bash
# Test HTTP port
kubectl run test-http --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://multi-port-svc:80

# Test metrics port
kubectl run test-metrics --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://multi-port-svc:9090/metrics
```

### Step 4: Understand Named targetPort Benefits

With numeric targetPort, changing the container port requires updating both the Deployment and the Service simultaneously. With named targetPort, the Service always resolves the name to whatever port number the pod currently declares.

<details>
<summary>Why does this matter during rolling updates?</summary>

During a rolling update, old and new pods coexist. If the new version changes a container port (e.g., metrics moves from 9090 to 9091), a numeric targetPort would only work for one version at a time. A named targetPort resolves to 9090 for old pods and 9091 for new pods simultaneously, ensuring zero downtime.

</details>

### Step 5: Inspect Endpoints with Multiple Ports

```bash
kubectl describe endpoints multi-port-svc
```

Each subset shows multiple port entries, one for each named port. The Endpoints controller tracks both independently.

## Spot the Bug

A teammate defines a two-port Service but forgets to name the ports:

```yaml
spec:
  ports:
    - port: 80
      targetPort: 80
    - port: 9090
      targetPort: 9090
```

<details>
<summary>Explanation</summary>

When a Service has more than one port, every port must have a `name` field. Without names, Kubernetes cannot distinguish between the ports in Endpoints, and the API server rejects the manifest with a validation error: `spec.ports: Required value: port name is required when there is more than one port`.

</details>

## Verify What You Learned

```bash
# Service shows two ports
kubectl get svc multi-port-svc

# Both ports have endpoints
kubectl get endpoints multi-port-svc

# HTTP responds
kubectl run v1 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://multi-port-svc:80

# Metrics responds
kubectl run v2 --image=busybox:1.37 --rm -it --restart=Never -- wget -qO- http://multi-port-svc:9090/metrics
```

## Cleanup

```bash
kubectl delete deployment multi-port-app
kubectl delete svc multi-port-svc
```

## What's Next

In [exercise 08 (ExternalName Services)](../08-externalname-services/), you will learn how to create a Service that acts as a DNS alias for an external hostname, enabling seamless migration from external to internal services.

## Summary

- Multi-port Services **require** a `name` for each port entry.
- **Named targetPort** references the pod spec's port name, decoupling the Service from numeric port values.
- Named targetPort is critical for **rolling updates** where old and new pods may listen on different port numbers.
- Endpoints objects track multiple ports independently per subset.

## Reference

- [Multi-Port Services](https://kubernetes.io/docs/concepts/services-networking/service/#multi-port-services)
- [Pod Ports](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/pod-v1/#ports)

## Additional Resources

- [Services - Defining a Service](https://kubernetes.io/docs/concepts/services-networking/service/#defining-a-service)
- [Kubernetes API: ServicePort](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/service-v1/#ServicePort)
