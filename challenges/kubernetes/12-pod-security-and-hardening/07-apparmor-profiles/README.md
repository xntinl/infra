# Exercise 7: AppArmor Profiles for Pods

<!--
difficulty: advanced
concepts: [apparmor, mandatory-access-control, profile-loading, runtime-default, container-security]
tools: [kubectl, kind]
estimated_time: 35m
bloom_level: analyze
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security, 12-pod-security-and-hardening/05-seccomp-profiles]
-->

## Introduction

AppArmor is a Linux Security Module (LSM) that provides Mandatory Access Control (MAC). Unlike discretionary access controls (file permissions), AppArmor profiles cannot be overridden by the process itself, even if it runs as root. Kubernetes supports AppArmor profiles starting with v1.30 via the `securityContext.appArmorProfile` field (GA).

AppArmor complements seccomp: seccomp filters *which syscalls* a process can make, while AppArmor restricts *which files, network operations, and capabilities* a process can use.

## Architecture

```
Container Process
    |
    +-- seccomp: filters syscalls (kernel level)
    |
    +-- AppArmor: restricts file access, network, capabilities (LSM level)
    |
    +-- Linux capabilities: fine-grained privilege control
    |
    +-- Security context: runAsNonRoot, readOnlyRootFilesystem
    v
Defense in Depth
```

## Suggested Steps

1. **Verify AppArmor is available** on your nodes:

```bash
# Check if AppArmor is enabled
kubectl get nodes -o jsonpath='{range .items[*]}{.metadata.name}: {.status.nodeInfo.operatingSystem}{"\n"}{end}'

# On the node itself:
cat /sys/module/apparmor/parameters/enabled
# Expected: Y

# List loaded profiles
cat /sys/kernel/security/apparmor/profiles | head -20
```

2. **Create a custom AppArmor profile.** Save on each node at `/etc/apparmor.d/k8s-deny-write`:

```
#include <tunables/global>

profile k8s-deny-write flags=(attach_disconnected,mediate_deleted) {
  #include <abstractions/base>

  # Allow read access everywhere
  file,

  # Deny write access to sensitive paths
  deny /etc/** w,
  deny /usr/** w,
  deny /bin/** w,
  deny /sbin/** w,

  # Allow writes only to /tmp and /var/run
  /tmp/** rw,
  /var/run/** rw,

  # Allow network access
  network,
}
```

3. **Load the profile** on the node:

```bash
sudo apparmor_parser -r /etc/apparmor.d/k8s-deny-write
# Verify it is loaded
sudo aa-status | grep k8s-deny-write
```

4. **Create a pod with the AppArmor profile** (Kubernetes 1.30+ GA syntax):

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: apparmor-pod
  namespace: apparmor-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        appArmorProfile:
          type: Localhost
          localhostProfile: k8s-deny-write
        capabilities:
          drop: ["ALL"]
```

5. **Create a pod with RuntimeDefault** AppArmor profile:

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: apparmor-runtime-default
  namespace: apparmor-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        appArmorProfile:
          type: RuntimeDefault
        capabilities:
          drop: ["ALL"]
```

6. **Test the profile enforcement:**

```bash
# Writing to /tmp should succeed
kubectl exec -n apparmor-lab apparmor-pod -- touch /tmp/test

# Writing to /etc should fail
kubectl exec -n apparmor-lab apparmor-pod -- touch /etc/test 2>&1
# Expected: Permission denied
```

## Verify

```bash
# Check that the pod is running with the AppArmor profile
kubectl get pod apparmor-pod -n apparmor-lab -o yaml | grep appArmor

# Verify from inside the container
kubectl exec -n apparmor-lab apparmor-pod -- cat /proc/1/attr/current
# Expected: k8s-deny-write (enforce)

# Test write restrictions
kubectl exec -n apparmor-lab apparmor-pod -- touch /tmp/allowed
kubectl exec -n apparmor-lab apparmor-pod -- touch /usr/blocked 2>&1
```

## Cleanup

```bash
kubectl delete namespace apparmor-lab
# On the node: sudo apparmor_parser -R /etc/apparmor.d/k8s-deny-write
```

## What's Next

The next exercise covers **Image Scanning and Admission Webhooks** -- preventing vulnerable images from running in your cluster.
