# 17.12 Multi-Cluster Service Mesh Federation

<!--
difficulty: insane
concepts: [multi-cluster, mesh-federation, east-west-gateway, cross-cluster-discovery, shared-trust, remote-secrets]
tools: [kubectl, istioctl, kind]
estimated_time: 90m
bloom_level: create
prerequisites: [istio-installation-and-injection, istio-security-mtls, istio-traffic-management]
-->

## Scenario

Your company runs workloads across two Kubernetes clusters for high availability.
Services in cluster-1 must discover and call services in cluster-2, and vice
versa, with full mTLS across the cluster boundary. You need to set up Istio
multi-cluster federation with a shared root CA, east-west gateways, and
cross-cluster service discovery.

## Constraints

1. Create two separate Kubernetes clusters (use kind with two distinct configs).
2. Both clusters must share the same root CA for mutual trust (generate a
   shared CA certificate and configure both Istio installations to use it).
3. Install Istio on both clusters with multi-cluster configuration:
   - Cluster-1: `network=network1`, `cluster=cluster1`, `meshID=mesh1`
   - Cluster-2: `network=network2`, `cluster=cluster2`, `meshID=mesh1`
4. Deploy east-west gateways on both clusters to handle cross-cluster traffic.
5. Exchange remote secrets so each cluster's Istio can discover services in the
   other cluster.
6. Deploy Service-A in cluster-1 only and Service-B in cluster-2 only.
7. Verify that Service-A in cluster-1 can call Service-B in cluster-2 (and
   vice versa) with full mTLS.
8. Verify that cross-cluster calls show up in both clusters' telemetry.
9. Simulate a failure: take down Service-B in cluster-2 and verify that the
   call from cluster-1 fails gracefully (no routing to a phantom endpoint).

## Success Criteria

1. `istioctl remote-clusters` on both clusters shows the remote cluster as
   connected.
2. `kubectl get pods -n istio-system` on both clusters shows the east-west
   gateway running.
3. Service-A in cluster-1 can resolve and call Service-B in cluster-2:
   `curl http://service-b.ns.svc.cluster.local/get` returns 200.
4. `istioctl proxy-config endpoint` on the Service-A Pod shows Service-B
   endpoints from cluster-2.
5. mTLS is active on the cross-cluster call (verified with
   `istioctl authn tls-check`).
6. Deleting Service-B in cluster-2 causes Service-A's endpoint list to update
   within 30 seconds.
7. Traffic metrics in Prometheus on cluster-1 show requests to Service-B with
   `destination_cluster=cluster2`.

## Hints

- Create two kind clusters:

```bash
kind create cluster --name cluster1 --config kind-cluster1.yaml
kind create cluster --name cluster2 --config kind-cluster2.yaml
```

- Generate a shared root CA:

```bash
mkdir -p certs
openssl req -x509 -sha256 -nodes -days 365 -newkey rsa:4096 \
  -subj '/O=mesh/CN=root-ca' \
  -keyout certs/root-key.pem -out certs/root-cert.pem

# Generate intermediate CAs for each cluster from the shared root
# See Istio docs: plug-in CA certificates
```

- Install Istio on each cluster with the shared CA:

```bash
# Cluster 1
istioctl install -f cluster1-config.yaml \
  --set values.pilot.env.EXTERNAL_ISTIOD=false

# Cluster 2
istioctl install -f cluster2-config.yaml \
  --set values.pilot.env.EXTERNAL_ISTIOD=false
```

- Example cluster config (IstioOperator):

```yaml
apiVersion: install.istio.io/v1alpha1
kind: IstioOperator
spec:
  values:
    global:
      meshID: mesh1
      multiCluster:
        clusterName: cluster1
      network: network1
  meshConfig:
    defaultConfig:
      proxyMetadata:
        ISTIO_META_DNS_CAPTURE: "true"
        ISTIO_META_DNS_AUTO_ALLOCATE: "true"
```

- Install east-west gateway:

```bash
samples/multicluster/gen-eastwest-gateway.sh --mesh mesh1 --cluster cluster1 --network network1 | istioctl install -y -f -
```

- Exchange remote secrets:

```bash
istioctl create-remote-secret --name=cluster1 --context=kind-cluster1 | kubectl apply -f - --context=kind-cluster2
istioctl create-remote-secret --name=cluster2 --context=kind-cluster2 | kubectl apply -f - --context=kind-cluster1
```

## Verification Commands

```bash
# Remote cluster connectivity
istioctl remote-clusters --context=kind-cluster1
istioctl remote-clusters --context=kind-cluster2

# East-west gateways running
kubectl get pods -n istio-system --context=kind-cluster1 | grep eastwest
kubectl get pods -n istio-system --context=kind-cluster2 | grep eastwest

# Cross-cluster service discovery
kubectl exec -n demo --context=kind-cluster1 deploy/service-a -c service-a -- \
  curl -s http://service-b.demo.svc.cluster.local/get

# Endpoints include remote cluster
istioctl proxy-config endpoint deploy/service-a -n demo --context=kind-cluster1 | grep service-b

# mTLS verification
istioctl authn tls-check service-b.demo.svc.cluster.local -n demo --context=kind-cluster1

# Cross-cluster metrics
kubectl port-forward -n istio-system svc/prometheus 9090:9090 --context=kind-cluster1 &
curl -s 'http://localhost:9090/api/v1/query?query=istio_requests_total{destination_cluster="cluster2"}' | python3 -m json.tool

# Failure simulation
kubectl delete deploy service-b -n demo --context=kind-cluster2
sleep 30
istioctl proxy-config endpoint deploy/service-a -n demo --context=kind-cluster1 | grep service-b
# Should show no healthy endpoints
```

## Cleanup

```bash
kind delete cluster --name cluster1
kind delete cluster --name cluster2
rm -rf certs/
```
