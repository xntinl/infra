# 8. ExternalName Services for External Access

<!--
difficulty: intermediate
concepts: [externalname, cname, dns-alias, external-services, migration]
tools: [kubectl, minikube, nslookup]
estimated_time: 25m
bloom_level: apply
prerequisites: [05-05]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured
- Completion of [exercise 05 (DNS and Service Discovery)](../05-dns-and-service-discovery/)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** ExternalName Services that act as DNS aliases for external hostnames
- **Apply** the migration pattern from external services to in-cluster services
- **Explain** the limitations and security considerations of ExternalName

## The Challenge

Create ExternalName Services to provide in-cluster DNS names for external services. Then demonstrate the migration pattern: start with an ExternalName pointing to an external database, then swap it to a regular ClusterIP Service when the database moves in-cluster -- without any application code changes.

### Step 1: Create an ExternalName Service

```yaml
# external-db.yaml
apiVersion: v1
kind: Service
metadata:
  name: database
spec:
  type: ExternalName
  externalName: db.production.example.com   # External hostname to alias
  # No selector, no ports, no clusterIP
```

```bash
kubectl apply -f external-db.yaml
```

Pods can now connect to `database` and DNS will return a CNAME record pointing to `db.production.example.com`.

### Step 2: Verify the CNAME Resolution

```bash
kubectl run dns-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup database
```

The output shows `database.default.svc.cluster.local` has a canonical name of `db.production.example.com`.

### Step 3: Create Multiple ExternalName Services

```yaml
# external-services.yaml
apiVersion: v1
kind: Service
metadata:
  name: payment-gateway
spec:
  type: ExternalName
  externalName: api.stripe.com
---
apiVersion: v1
kind: Service
metadata:
  name: email-service
spec:
  type: ExternalName
  externalName: smtp.sendgrid.net
```

```bash
kubectl apply -f external-services.yaml
```

### Step 4: Simulate the Migration Pattern

Deploy an in-cluster database to simulate migration:

```yaml
# in-cluster-db.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: postgres
spec:
  replicas: 1
  selector:
    matchLabels:
      app: postgres
  template:
    metadata:
      labels:
        app: postgres
    spec:
      containers:
        - name: postgres
          image: postgres:16
          ports:
            - containerPort: 5432
          env:
            - name: POSTGRES_PASSWORD
              value: "changeme"
```

```bash
kubectl apply -f in-cluster-db.yaml
```

Now swap the ExternalName Service to a ClusterIP Service by deleting and recreating it:

```bash
kubectl delete svc database
```

```yaml
# database-clusterip.yaml
apiVersion: v1
kind: Service
metadata:
  name: database                     # Same name -- applications do not change
spec:
  type: ClusterIP
  selector:
    app: postgres
  ports:
    - port: 5432
      targetPort: 5432
      name: postgres
```

```bash
kubectl apply -f database-clusterip.yaml
```

Applications connecting to `database:5432` now reach the in-cluster postgres without any code or config changes.

<details>
<summary>Why delete before recreating?</summary>

You cannot patch a Service to change its `type` from ExternalName to ClusterIP because ExternalName Services have `clusterIP: None` implicitly. The type and clusterIP fields interact in ways that require deletion and recreation. The Service name stays the same, so DNS resolution is seamless.

</details>

### Step 5: Verify the Migration

```bash
kubectl run verify --image=busybox:1.37 --rm -it --restart=Never -- nslookup database
```

Now you should see an A record with a ClusterIP instead of a CNAME.

## Spot the Bug

A teammate creates an ExternalName Service with an IP address:

```yaml
spec:
  type: ExternalName
  externalName: "10.0.0.50"
```

<details>
<summary>Explanation</summary>

ExternalName creates a DNS CNAME record, which must point to a hostname, not an IP address. While Kubernetes may accept this manifest, DNS resolution will fail because CNAME targets must be valid DNS names. For external IPs, use a selector-less Service with manual Endpoints instead.

</details>

## Verify What You Learned

```bash
# ExternalName Services resolve to CNAME
kubectl run t1 --image=busybox:1.37 --rm -it --restart=Never -- nslookup payment-gateway
kubectl run t2 --image=busybox:1.37 --rm -it --restart=Never -- nslookup email-service

# Migrated Service resolves to ClusterIP
kubectl get svc database
kubectl get endpoints database
```

## Cleanup

```bash
kubectl delete deployment postgres
kubectl delete svc database payment-gateway email-service
```

## What's Next

You have now covered all four Service types. In [exercise 09 (EndpointSlices)](../09-endpointslices/), you will learn about EndpointSlices -- the scalable successor to Endpoints objects -- and how topology-aware routing optimizes traffic in multi-zone clusters.

## Summary

- ExternalName Services create **DNS CNAME** records pointing to external hostnames.
- They require no selectors, ports, or ClusterIP -- they are purely a DNS alias.
- The **migration pattern**: start with ExternalName, then swap to ClusterIP when the service moves in-cluster.
- ExternalName targets must be **valid DNS hostnames**, not IP addresses.
- Migrating from ExternalName to ClusterIP requires delete-and-recreate due to type restrictions.

## Reference

- [ExternalName Service](https://kubernetes.io/docs/concepts/services-networking/service/#externalname)
- [Services Without Selectors](https://kubernetes.io/docs/concepts/services-networking/service/#services-without-selectors)

## Additional Resources

- [DNS for Services and Pods](https://kubernetes.io/docs/concepts/services-networking/dns-pod-service/)
- [Service API Reference](https://kubernetes.io/docs/reference/kubernetes-api/service-resources/service-v1/)
