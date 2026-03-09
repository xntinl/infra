# Exercise 5: Seccomp Profiles: RuntimeDefault and Custom

<!--
difficulty: intermediate
concepts: [seccomp, runtimedefault, localhost-profile, syscall-filtering, security-profile-operator]
tools: [kubectl, kind]
estimated_time: 30m
bloom_level: apply
prerequisites: [12-pod-security-and-hardening/01-security-contexts-and-pod-security, 12-pod-security-and-hardening/04-linux-capabilities]
-->

## Introduction

Seccomp (Secure Computing Mode) filters the system calls a container can make. Even if a container has the right Linux capability, seccomp can block the underlying syscall. Kubernetes supports three seccomp profile types:

- **RuntimeDefault** -- the container runtime's default profile (blocks ~40 dangerous syscalls)
- **Localhost** -- a custom JSON profile stored on the node
- **Unconfined** -- no seccomp filtering (not recommended)

The Restricted Pod Security Standard requires at least `RuntimeDefault`.

## Step-by-Step

### 1. Create a namespace

```yaml
# namespace.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: seccomp-lab
```

### 2. Pod with RuntimeDefault seccomp

```yaml
# pod-runtimedefault.yaml
apiVersion: v1
kind: Pod
metadata:
  name: runtime-default
  namespace: seccomp-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: RuntimeDefault       # uses the container runtime's default profile
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
```

### 3. Custom seccomp profile (for kind clusters)

Save this profile on the node at `/var/lib/kubelet/seccomp/profiles/audit.json`:

```json
{
  "defaultAction": "SCMP_ACT_LOG",
  "architectures": ["SCMP_ARCH_X86_64", "SCMP_ARCH_AARCH64"],
  "syscalls": [
    {
      "names": [
        "accept4", "bind", "clone", "close", "connect",
        "epoll_create1", "epoll_ctl", "epoll_wait", "execve",
        "exit", "exit_group", "fcntl", "fstat", "futex",
        "getdents64", "getpid", "getsockname", "ioctl",
        "listen", "lseek", "mmap", "mprotect", "nanosleep",
        "openat", "poll", "read", "recvfrom", "rt_sigaction",
        "rt_sigprocmask", "sendto", "set_tid_address",
        "socket", "stat", "write"
      ],
      "action": "SCMP_ACT_ALLOW"
    },
    {
      "names": ["ptrace", "reboot", "mount", "umount2"],
      "action": "SCMP_ACT_ERRNO",
      "errnoRet": 1
    }
  ]
}
```

For kind, mount the profile:

```yaml
# kind-config.yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
  - role: control-plane
    extraMounts:
      - hostPath: ./seccomp-profiles
        containerPath: /var/lib/kubelet/seccomp/profiles
```

### 4. Pod with custom Localhost profile

```yaml
# pod-custom-seccomp.yaml
apiVersion: v1
kind: Pod
metadata:
  name: custom-seccomp
  namespace: seccomp-lab
spec:
  securityContext:
    runAsNonRoot: true
    runAsUser: 1000
    seccompProfile:
      type: Localhost
      localhostProfile: profiles/audit.json   # path relative to /var/lib/kubelet/seccomp/
  containers:
    - name: app
      image: busybox:1.37
      command: ["sleep", "3600"]
      securityContext:
        allowPrivilegeEscalation: false
        capabilities:
          drop: ["ALL"]
```

### 5. Apply

```bash
kubectl apply -f namespace.yaml
kubectl apply -f pod-runtimedefault.yaml
# kubectl apply -f pod-custom-seccomp.yaml  # only if you have the profile on the node
```

## TODO Exercise

Create a seccomp profile that allows only the syscalls needed for `nginx:1.27` to run. Start with `SCMP_ACT_LOG` as the default action, check the audit log to see which syscalls nginx uses, then switch to `SCMP_ACT_ERRNO` for everything not explicitly allowed.

<details>
<summary>Hint</summary>

Use `aupd` or check `/var/log/syslog` for seccomp audit messages. Each log line includes the syscall number. Map numbers to names with `ausyscall --dump`. Common nginx syscalls include: `accept4`, `bind`, `clone`, `close`, `connect`, `epoll_ctl`, `epoll_wait`, `fstat`, `futex`, `getdents64`, `listen`, `mmap`, `openat`, `read`, `recvfrom`, `sendto`, `socket`, `write`.

</details>

## Verify

```bash
# RuntimeDefault pod should be running
kubectl get pod runtime-default -n seccomp-lab

# Check the seccomp profile applied to the pod
kubectl get pod runtime-default -n seccomp-lab \
  -o jsonpath='{.spec.securityContext.seccompProfile}'

# Verify dangerous syscalls are blocked (ptrace should fail)
kubectl exec -n seccomp-lab runtime-default -- \
  sh -c 'echo $$; ls /proc/self/status' 2>&1

# Check node-level seccomp status
kubectl exec -n seccomp-lab runtime-default -- \
  cat /proc/1/status | grep Seccomp
# Expected: Seccomp: 2 (SECCOMP_MODE_FILTER)
```

## Cleanup

```bash
kubectl delete namespace seccomp-lab
```

## What's Next

The next exercise covers **Secrets Encryption at Rest** -- protecting Secrets stored in etcd from direct access.
