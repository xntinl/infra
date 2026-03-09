# 16. Designing a Self-Healing Pod Architecture

<!--
difficulty: insane
concepts: [self-healing, liveness-probes, readiness-probes, startup-probes, init-containers, native-sidecar, graceful-degradation, circuit-breaker]
tools: [kubectl, minikube]
estimated_time: 75m
bloom_level: create
prerequisites: [01-06, 01-07, 01-09, 01-11]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) running **v1.29+**
- `kubectl` installed and configured
- Completion of exercises [06](../06-multi-container-patterns/), [07](../07-init-containers/), [09](../09-pod-lifecycle-hooks/), and [11](../11-native-sidecar-containers/)

## The Scenario

You are designing a critical data processing Pod that must survive cascading failures without human intervention. The system has four components:

1. **Config Loader** (init container): fetches configuration from a ConfigMap and writes it to a shared volume. The main app cannot start without valid configuration.
2. **Health Monitor** (native sidecar): a watchdog that continuously writes a heartbeat file. If the heartbeat file is stale (older than 10 seconds), the main app considers itself degraded.
3. **Data Processor** (main container): reads configuration, processes data in a loop, writes output to a log volume. Must have liveness, readiness, and startup probes.
4. **Log Exporter** (main container): tails the log volume and exposes a simple HTTP endpoint on port 9090 that returns the last 10 log lines.

The system must handle these failure modes automatically:
- Missing or corrupt configuration at startup (init container retries)
- Health monitor crash (native sidecar auto-restarts)
- Data processor hang (liveness probe restarts it)
- Data processor slow startup (startup probe prevents premature kills)
- Log exporter failure (readiness probe removes it from service endpoints)

## Constraints

1. You must use **one init container**, **one native sidecar**, and **two main containers**.
2. The init container must read from a ConfigMap named `processor-config` and write to a shared volume. If the ConfigMap does not exist, the init container must retry with a backoff loop (not exit with failure).
3. The native sidecar must write a timestamp to `/health/heartbeat` every 3 seconds.
4. The data processor must have all three probe types:
   - **startupProbe**: checks for the existence of `/tmp/started` (created after initialization), `failureThreshold: 30`, `periodSeconds: 2`
   - **livenessProbe**: checks that `/health/heartbeat` was modified within the last 10 seconds
   - **readinessProbe**: checks that the data processor has written at least one output line
5. The log exporter must expose an HTTP endpoint on port 9090 that returns log content, with a readiness probe on that endpoint.
6. A preStop hook on the data processor must flush remaining data and write a shutdown marker to the log.
7. `terminationGracePeriodSeconds` must be at least 15 seconds.
8. All containers must have resource requests and limits defined.

## Success Criteria

1. The Pod starts successfully when the `processor-config` ConfigMap exists.
2. If the ConfigMap is created after the Pod, the init container eventually picks it up and the Pod proceeds to Running.
3. Killing the health monitor sidecar (`kubectl exec ... -- kill 1`) results in automatic restart within seconds.
4. Creating a file `/tmp/hang` inside the data processor (simulating a hang by making the liveness check fail) triggers a container restart.
5. All probes report healthy under normal operation.
6. `kubectl logs <pod> -c log-exporter --tail=5` shows recent processor output.
7. Deleting the Pod triggers the preStop hook, visible in the log output.

## Verification Commands

```bash
# Pod healthy
kubectl get pod self-healing-pod
# Expected: Running, 3/3

# Health monitor running
kubectl exec self-healing-pod -c health-monitor -- cat /health/heartbeat

# Data processor output
kubectl exec self-healing-pod -c data-processor -- cat /var/log/processor/output.log | tail -5

# Log exporter endpoint
kubectl exec self-healing-pod -c log-exporter -- wget -qO- http://localhost:9090

# Probe status
kubectl describe pod self-healing-pod | grep -A 3 "Liveness\|Readiness\|Startup"

# Simulate health monitor crash
kubectl exec self-healing-pod -c health-monitor -- kill 1
sleep 5
kubectl get pod self-healing-pod
# Expected: still Running, RESTARTS incremented for sidecar
```

## Cleanup

```bash
kubectl delete pod self-healing-pod
kubectl delete configmap processor-config
```
