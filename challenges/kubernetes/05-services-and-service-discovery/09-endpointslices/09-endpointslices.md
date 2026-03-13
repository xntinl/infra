# 9. EndpointSlices and Topology-Aware Routing

<!--
difficulty: advanced
concepts: [endpointslices, topology-aware-routing, zone-hints, endpoint-conditions]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: analyze
prerequisites: [05-04, 05-05]
-->

## Prerequisites

- A running Kubernetes cluster (preferably multi-zone or simulated with node labels)
- `kubectl` installed and configured
- Completion of [exercise 04](../04-service-selectors-and-endpoints/04-service-selectors-and-endpoints.md) and [exercise 05](../05-dns-and-service-discovery/05-dns-and-service-discovery.md)

## Learning Objectives

After completing this exercise, you will be able to:

- **Analyze** how EndpointSlices improve scalability over Endpoints objects
- **Configure** topology-aware routing to prefer same-zone backends
- **Inspect** EndpointSlice conditions (ready, serving, terminating)

## Architecture

```
                    ┌─────────────────────────────────────┐
                    │         Service: web-app             │
                    └─────────────────────────────────────┘
                                    │
                    ┌───────────────┴───────────────┐
                    │                               │
           EndpointSlice A                 EndpointSlice B
           (up to 100 endpoints)           (up to 100 endpoints)
           ┌─────────────┐                ┌─────────────┐
           │ pod-1 (zone-a)│              │ pod-4 (zone-b)│
           │ pod-2 (zone-a)│              │ pod-5 (zone-b)│
           │ pod-3 (zone-a)│              │ pod-6 (zone-b)│
           └─────────────┘                └─────────────┘

           Topology hints:                Topology hints:
           zone-a → prefer local          zone-b → prefer local
```

EndpointSlices replace the older Endpoints API for scalability. While Endpoints store all pod IPs in a single object (problematic at 1000+ pods), EndpointSlices split them into chunks of up to 100. They also carry per-endpoint conditions and topology hints.

## The Challenge

### Step 1: Label Nodes to Simulate Zones

If running a single-node cluster, add zone labels to simulate multi-zone topology:

```bash
kubectl label nodes --all topology.kubernetes.io/zone=zone-a
```

For multi-node clusters, label nodes with different zones:

```bash
# Example for multi-node:
# kubectl label node node-1 topology.kubernetes.io/zone=zone-a
# kubectl label node node-2 topology.kubernetes.io/zone=zone-b
```

### Step 2: Deploy a Multi-Replica Application

```yaml
# deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web-app
spec:
  replicas: 6
  selector:
    matchLabels:
      app: web-app
  template:
    metadata:
      labels:
        app: web-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
```

### Step 3: Create a Service with Topology-Aware Routing

```yaml
# service.yaml
apiVersion: v1
kind: Service
metadata:
  name: web-app
  annotations:
    service.kubernetes.io/topology-mode: Auto    # Enable topology-aware routing
spec:
  selector:
    app: web-app
  ports:
    - port: 80
      targetPort: 80
      name: http
```

Apply both manifests:

```bash
kubectl apply -f deployment.yaml
kubectl apply -f service.yaml
```

### Step 4: Inspect EndpointSlices

List EndpointSlices for the Service:

```bash
kubectl get endpointslices -l kubernetes.io/service-name=web-app
```

Examine a specific EndpointSlice in detail:

```bash
kubectl get endpointslices -l kubernetes.io/service-name=web-app -o yaml
```

Key fields to analyze:

- `endpoints[].addresses` -- pod IP addresses
- `endpoints[].conditions.ready` -- whether the pod is ready to serve traffic
- `endpoints[].conditions.serving` -- whether the pod is serving (may differ from ready during termination)
- `endpoints[].conditions.terminating` -- whether the pod is being terminated
- `endpoints[].zone` -- the topology zone of the node running this pod
- `endpoints[].hints` -- topology hints for routing (populated when topology mode is Auto)

### Step 5: Compare with Legacy Endpoints

```bash
# Legacy Endpoints object (still created for backward compatibility)
kubectl get endpoints web-app -o yaml

# EndpointSlices (richer information)
kubectl get endpointslices -l kubernetes.io/service-name=web-app -o yaml
```

Notice how EndpointSlices carry zone information and per-endpoint conditions that Endpoints lack.

### Step 6: Observe Endpoint Conditions During Scale-Down

Watch EndpointSlices while scaling down:

```bash
kubectl get endpointslices -l kubernetes.io/service-name=web-app --watch &
kubectl scale deployment web-app --replicas=2
```

Endpoints transition through serving=true/terminating=true before removal.

## Verify What You Learned

```bash
# EndpointSlices exist for the Service
kubectl get endpointslices -l kubernetes.io/service-name=web-app

# Each endpoint has conditions and zone information
kubectl get endpointslices -l kubernetes.io/service-name=web-app -o jsonpath='{range .items[*].endpoints[*]}{.addresses[0]}{"\t"}{.conditions.ready}{"\t"}{.zone}{"\n"}{end}'

# Service has topology annotation
kubectl get svc web-app -o jsonpath='{.metadata.annotations}'
```

## Cleanup

```bash
kubectl delete deployment web-app
kubectl delete svc web-app
kubectl label nodes --all topology.kubernetes.io/zone-
```

## What's Next

In [exercise 10 (Session Affinity and Traffic Policies)](../10-session-affinity-and-traffic-policy/10-session-affinity-and-traffic-policy.md), you will learn how to control traffic distribution with session affinity and traffic policies that determine whether requests stick to the same backend or spread evenly.

## Summary

- EndpointSlices split endpoints into chunks of 100 for scalability (replacing the monolithic Endpoints object).
- Each endpoint carries individual **conditions**: ready, serving, and terminating.
- **Topology-aware routing** uses zone hints to prefer backends in the same zone, reducing cross-zone latency and cost.
- The annotation `service.kubernetes.io/topology-mode: Auto` enables topology hints.
- Legacy Endpoints objects are still created for backward compatibility but lack zone and condition granularity.

## Reference

- [EndpointSlices](https://kubernetes.io/docs/concepts/services-networking/endpoint-slices/)
- [Topology Aware Routing](https://kubernetes.io/docs/concepts/services-networking/topology-aware-routing/)

## Additional Resources

- [EndpointSlice API Reference](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/endpoint-slice-v1/)
- [KEP-2433: Topology Aware Routing](https://github.com/kubernetes/enhancements/tree/master/keps/sig-network/2433-topology-aware-hints)
