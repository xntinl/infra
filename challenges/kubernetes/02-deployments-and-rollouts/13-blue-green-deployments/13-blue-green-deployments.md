# 13. Blue-Green Deployments Without Native Support

<!--
difficulty: insane
concepts: [blue-green, service-selector, zero-downtime, deployment-strategy]
tools: [kubectl, minikube]
estimated_time: 60m
bloom_level: create
prerequisites: [02-05, 02-12]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- Completion of [exercise 05 (Rolling Updates and Rollbacks)](../05-rolling-updates-and-rollbacks/05-rolling-updates-and-rollbacks.md) and [exercise 12 (Recreate vs Rolling Update Deep Dive)](../12-recreate-vs-rolling-strategies/12-recreate-vs-rolling-strategies.md)

## The Scenario

Your team deploys a web application that processes financial transactions. Rolling updates are not acceptable because two different versions of the application must never handle traffic simultaneously -- even briefly -- due to schema differences in the transaction format. You need instant, atomic cutover from one version to another, and equally instant rollback if the new version misbehaves.

Implement a blue-green deployment system using only native Kubernetes resources. No service mesh, no Argo Rollouts, no Flagger, no custom controllers.

## Constraints

1. **Two Deployments** must run simultaneously: `app-blue` and `app-green`, each with 3 replicas. Use a simple HTTP server image (such as `hashicorp/http-echo` or `nginx` with different response bodies) so you can distinguish which version is serving.
2. **A single Service** called `app` must route traffic to the active deployment by matching a `version` label (e.g., `version: blue` or `version: green`). The Service selector must be the only thing that changes during a switch.
3. **Zero failed requests** during the switch. Because Kubernetes Service selector updates are atomic from the client's perspective, no request should receive an error or be routed to the wrong version during cutover.
4. **Warm standby**: the inactive deployment must remain running at full replica count. It is not scaled to zero.
5. **Health gate**: before switching traffic to the new version, you must verify that all pods in the target deployment are `Ready`. Do not switch until `kubectl rollout status` confirms the deployment is fully available.
6. **Single-command switch**: the actual traffic cutover must be achievable with one `kubectl patch` command that updates the Service selector.
7. **Rollback** must use the same mechanism -- a single `kubectl patch` command pointing the Service back to the previous version.

## Success Criteria

- Both `app-blue` and `app-green` Deployments exist with 3/3 ready replicas each.
- The `app` Service initially routes all traffic to the blue deployment.
- Running `curl` in a loop against the Service endpoint returns the blue version's response consistently.
- After the switch, the same `curl` loop returns the green version's response consistently.
- Rolling back returns to the blue version's response with no errors in between.
- At no point during either switch does `curl` return a connection error or a response from the wrong version.

## Verification Commands

Check both deployments are healthy:

```bash
kubectl get deployments -l app=app
kubectl rollout status deployment/app-blue
kubectl rollout status deployment/app-green
```

Confirm which version the Service is currently targeting:

```bash
kubectl get service app -o jsonpath='{.spec.selector}'
```

Run a continuous traffic test (adapt the URL to your cluster's Service access method -- `kubectl port-forward`, NodePort, or `minikube service`):

```bash
while true; do curl -s http://<your-service-endpoint>; sleep 0.5; done
```

During the switch, every response should come from the expected version. There should be no `curl: (7) Failed to connect` or mixed-version responses in the output.

## Cleanup

```bash
kubectl delete deployment app-blue app-green
kubectl delete service app
```
