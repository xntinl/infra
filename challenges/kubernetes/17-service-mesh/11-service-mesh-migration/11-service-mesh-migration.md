# 17.11 Service Mesh Migration: No-Mesh to Full Mesh

<!--
difficulty: insane
concepts: [mesh-migration, permissive-mtls, gradual-rollout, namespace-injection, sidecar-resource, traffic-validation]
tools: [kubectl, istioctl]
estimated_time: 60m
bloom_level: create
prerequisites: [istio-installation-and-injection, istio-traffic-management, istio-security-mtls]
-->

## Scenario

You are migrating a production microservices platform from no service mesh to
full Istio. The platform has 4 services across 2 namespaces. You cannot take
downtime and must migrate incrementally. Some services communicate with external
APIs and must retain plaintext egress. At each stage, existing service-to-service
communication must continue working.

## Constraints

1. Start with 4 services running without any mesh (no sidecars, no Istio).
2. Install Istio without disrupting existing traffic.
3. Migrate one namespace at a time using PERMISSIVE mTLS mode (accepts both
   plaintext and mTLS) so that meshed and non-meshed services can communicate.
4. For each namespace migration:
   a. Label the namespace for injection.
   b. Perform a rolling restart of all Deployments.
   c. Verify all service-to-service communication still works.
   d. Verify sidecar injection (2/2 containers).
5. After all namespaces are meshed, switch to STRICT mTLS.
6. Configure a ServiceEntry and DestinationRule for external API access
   (e.g., `httpbin.org`) to allow plaintext egress.
7. Create a Sidecar resource to limit the scope of Envoy configuration pushed
   to each namespace (reduce memory footprint).
8. All transitions must be verified with curl tests between services at each step.

## Success Criteria

1. Before migration: all 4 services communicate over plaintext (verified with
   `istioctl authn tls-check` showing no mTLS).
2. After namespace-1 migration: namespace-1 services have sidecars, can still
   communicate with non-meshed namespace-2 services (PERMISSIVE mode).
3. After namespace-2 migration: all services have sidecars, all communication
   uses mTLS in PERMISSIVE mode.
4. After STRICT mode: all intra-mesh communication uses mTLS. A plaintext
   client from outside the mesh is rejected.
5. External API access (`httpbin.org`) works through the ServiceEntry.
6. Sidecar resource limits egress scope so each namespace only has routes for
   its dependencies.
7. Zero service disruption throughout the migration (no 5xx errors between steps).

## Hints

- PERMISSIVE PeerAuthentication:

```yaml
apiVersion: security.istio.io/v1
kind: PeerAuthentication
metadata:
  name: permissive
  namespace: istio-system    # mesh-wide
spec:
  mtls:
    mode: PERMISSIVE
```

- Sidecar resource to limit scope:

```yaml
apiVersion: networking.istio.io/v1
kind: Sidecar
metadata:
  name: default
  namespace: namespace-1
spec:
  egress:
    - hosts:
        - "./*"                       # same namespace
        - "namespace-2/*"             # explicit dependency
        - "istio-system/*"            # mesh infrastructure
```

- ServiceEntry for external access:

```yaml
apiVersion: networking.istio.io/v1
kind: ServiceEntry
metadata:
  name: httpbin-ext
  namespace: namespace-1
spec:
  hosts:
    - httpbin.org
  ports:
    - number: 443
      name: tls
      protocol: TLS
  resolution: DNS
  location: MESH_EXTERNAL
```

## Verification Commands

```bash
# Step 0: Baseline -- no sidecars
kubectl get pods -n namespace-1 -o jsonpath='{.items[*].spec.containers[*].name}'
kubectl get pods -n namespace-2 -o jsonpath='{.items[*].spec.containers[*].name}'

# Step 1: After Istio install
istioctl version
kubectl get pods -n istio-system

# Step 2: After namespace-1 migration (PERMISSIVE)
kubectl get pods -n namespace-1  # should show 2/2
kubectl exec -n namespace-1 deploy/svc-a -- curl -s http://svc-c.namespace-2/get
# Should succeed (plaintext to non-meshed service)

# Step 3: After namespace-2 migration
kubectl get pods -n namespace-2  # should show 2/2
istioctl authn tls-check svc-c.namespace-2.svc.cluster.local

# Step 4: After STRICT mTLS
kubectl exec -n namespace-1 deploy/svc-a -- curl -s http://svc-c.namespace-2/get
# Should succeed (mTLS)

# Plaintext client rejected
kubectl run test --image=curlimages/curl:8.7.1 --rm -it -- curl -s http://svc-a.namespace-1/get
# Should fail (no sidecar = no mTLS)

# Step 5: External access
kubectl exec -n namespace-1 deploy/svc-a -c svc-a -- curl -s https://httpbin.org/get

# Step 6: Sidecar scope
istioctl proxy-config cluster deploy/svc-a -n namespace-1 | wc -l
# Should be limited compared to without Sidecar resource
```

## Cleanup

```bash
kubectl delete namespace namespace-1 namespace-2
istioctl uninstall --purge -y
kubectl delete namespace istio-system
```
