<!--
difficulty: insane
concepts: [dynamic-reload, config-watcher, sidecar-pattern, inotify, signal-based-reload, rolling-hash]
tools: [kubectl, docker]
estimated_time: 90m
bloom_level: create
prerequisites: [configmap-volume-updates, subpath-mounts, environment-variable-patterns]
-->

# 8.13 Dynamic Configuration Reload Without Restarts

## The Scenario

Your company runs a high-traffic API that cannot tolerate restarts. Configuration changes (feature flags, rate limits, log levels) must take effect within 30 seconds without any pod restarts, rolling updates, or dropped connections. You must design and implement a configuration reload system that works with any application.

The system must handle three types of configuration:

1. **Feature flags** stored in a ConfigMap -- toggled frequently by the product team
2. **Rate limits** stored in a ConfigMap -- adjusted by the platform team during traffic spikes
3. **Database credentials** stored in a Secret -- rotated quarterly by the security team

## Constraints

1. No pod restarts or rolling updates when configuration changes
2. Configuration must propagate to all replicas within 30 seconds
3. The application container must not need to understand Kubernetes APIs
4. The solution must work with applications that reload config by reading a file (not just env vars)
5. Applications that support signal-based reload (e.g., SIGHUP for nginx) must receive the signal automatically
6. A sidecar or init container pattern must be used -- no modifications to the main application image
7. The system must handle both ConfigMap and Secret sources
8. Failed configuration must not crash the application -- validation before reload
9. A configuration change log must be maintained (what changed, when, which version)
10. Health checks must report configuration version and last reload timestamp

## Success Criteria

1. A ConfigMap change (e.g., feature flag toggle) is reflected in the application response within 30 seconds
2. A Secret change (e.g., rotated database password) is picked up without a restart
3. The nginx configuration reload is triggered automatically via SIGHUP when its ConfigMap changes
4. A malformed configuration is rejected and the previous valid config remains active
5. `GET /config/status` returns the current config version, last reload time, and reload count
6. At least 3 replicas all serve the updated configuration after a change
7. No requests are dropped during the configuration reload (verify with a load test)
8. The change log shows timestamped entries for each config update

## Verification Commands

```bash
# Check current config version
kubectl exec deploy/api-server -c app -- wget -qO- http://localhost:8080/config/status

# Update a feature flag
kubectl patch configmap feature-flags --type merge -p '{"data":{"DARK_MODE":"true"}}'

# Wait and verify propagation (all replicas)
for pod in $(kubectl get pods -l app=api-server -o name); do
  echo "--- $pod ---"
  kubectl exec $pod -c app -- wget -qO- http://localhost:8080/config/status
done

# Verify nginx picks up config changes
kubectl patch configmap nginx-config --type merge -p '{"data":{"rate-limit.conf":"limit_req_zone $binary_remote_addr zone=api:10m rate=100r/s;\n"}}'
kubectl exec deploy/api-server -c sidecar -- cat /var/log/config-changes.log | tail -5

# Test with malformed config (should be rejected)
kubectl patch configmap feature-flags --type merge -p '{"data":{"__INVALID__":"{{broken json"}}'
kubectl exec deploy/api-server -c app -- wget -qO- http://localhost:8080/config/status
# Should still show previous valid version

# Load test during config change (no dropped requests)
kubectl run loadtest --image=busybox:1.37 --rm -it -- sh -c '
  for i in $(seq 1 100); do
    wget -qO- --timeout=2 http://api-server:8080/health || echo "DROPPED: $i"
    sleep 0.1
  done
'
```

## Cleanup

```bash
kubectl delete deployment api-server
kubectl delete configmap feature-flags rate-limits nginx-config
kubectl delete secret db-credentials
kubectl delete service api-server
```
