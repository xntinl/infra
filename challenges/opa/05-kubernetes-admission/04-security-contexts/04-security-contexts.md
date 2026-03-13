# Security Contexts: Hardening Pods

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 05-03 (image-allowlist)

## Learning Objectives

After completing this exercise, you will be able to:

- Enforce SecurityContext fields (`runAsNonRoot`, `readOnlyRootFilesystem`, `allowPrivilegeEscalation`, capability drops)
- Require resource limits (CPU and memory) on every container
- Handle the Rego subtlety of distinguishing between a missing field and a field set to `false`

## Why This Matters

You have controlled the labels and the images. Now comes the most important part: **how** that container runs. A container can have the correct image from the correct registry with all the labels in the world, but if it runs as root with full filesystem access and the ability to escalate privileges, it is a critical security risk. The SecurityContext in Kubernetes is where you define the security restrictions, and your policy will make sure nobody skips them.

---

## Why Each Field Matters

The SecurityContext of a container controls the privileges of the process running inside:

**`runAsNonRoot: true`** -- Forces the process inside the container to not run as root (UID 0). If an attacker compromises the process, they do not have root privileges. The blast radius of a compromised container is limited to the permissions of the non-root user.

**`readOnlyRootFilesystem: true`** -- The container filesystem is read-only. The process cannot write files, install tools, download malware, or modify its own binary. If it needs to write, it uses explicit volumes (which you control).

**`allowPrivilegeEscalation: false`** -- Blocks mechanisms like `setuid` that allow a process to gain more privileges than it started with. Without this, a process that started without privileges could escalate to root.

**`drop ALL capabilities`** -- Linux capabilities are granular permissions (mounting filesystems, manipulating the network, changing file permissions). By default, containers have several enabled. `drop: ["ALL"]` removes them all. If you need one, you add it explicitly with `add` -- the principle of least privilege.

**Resource limits (CPU and memory)** -- Without limits, a container can consume all of a node's resources, affecting other pods. A crypto-miner hidden in a container without limits will eat all your CPU. Limits put a ceiling in place.

---

## The Policy

Create `policy.rego`:

```rego
package kubernetes.security

import rego.v1

# Collect all containers
all_containers contains c if {
    some c in input.review.object.spec.containers
}

all_containers contains c if {
    some c in input.review.object.spec.initContainers
}

# --- SecurityContext checks ---

# Must have runAsNonRoot: true
violation contains {"msg": msg} if {
    some c in all_containers
    not c.securityContext.runAsNonRoot
    msg := sprintf(
        "container '%s': must have securityContext.runAsNonRoot = true",
        [c.name],
    )
}

# Must have readOnlyRootFilesystem: true
violation contains {"msg": msg} if {
    some c in all_containers
    not c.securityContext.readOnlyRootFilesystem
    msg := sprintf(
        "container '%s': must have securityContext.readOnlyRootFilesystem = true",
        [c.name],
    )
}

# Must have allowPrivilegeEscalation: false
# (not false != true in OPA; must verify the field exists and is false)
violation contains {"msg": msg} if {
    some c in all_containers
    c.securityContext.allowPrivilegeEscalation != false
    msg := sprintf(
        "container '%s': must have securityContext.allowPrivilegeEscalation = false",
        [c.name],
    )
}

# allowPrivilegeEscalation not defined is also a violation
violation contains {"msg": msg} if {
    some c in all_containers
    not _has_key(c.securityContext, "allowPrivilegeEscalation")
    msg := sprintf(
        "container '%s': must have securityContext.allowPrivilegeEscalation = false",
        [c.name],
    )
}

# Must drop ALL capabilities
violation contains {"msg": msg} if {
    some c in all_containers
    not _drops_all_capabilities(c)
    msg := sprintf(
        "container '%s': must include securityContext.capabilities.drop = [\"ALL\"]",
        [c.name],
    )
}

# --- Resource limits checks ---

# Must define memory limits
violation contains {"msg": msg} if {
    some c in all_containers
    not c.resources.limits.memory
    msg := sprintf(
        "container '%s': must define resources.limits.memory",
        [c.name],
    )
}

# Must define CPU limits
violation contains {"msg": msg} if {
    some c in all_containers
    not c.resources.limits.cpu
    msg := sprintf(
        "container '%s': must define resources.limits.cpu",
        [c.name],
    )
}

# --- Helpers ---

_has_key(obj, key) if {
    _ = obj[key]
}

_drops_all_capabilities(c) if {
    some cap in c.securityContext.capabilities.drop
    cap == "ALL"
}
```

There are several rules, but each one checks exactly one aspect. If any condition is not met, a message is added to the `violation` set. At the end you get a complete report of everything the pod has wrong.

Notice the special case of `allowPrivilegeEscalation`. You need two rules: one for when the field exists but is not `false`, and one for when the field does not exist at all. This is because `not something.field` in Rego is `true` both when `field` does not exist and when it is `false` -- and you do not want to confuse the absence of the field with having the correct value.

---

## The Insecure Pod: Everything Wrong

Create `input.json` -- a pod that violates absolutely everything:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "insecure-pod"
            },
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "nginx:latest",
                        "securityContext": {},
                        "resources": {}
                    },
                    {
                        "name": "sidecar",
                        "image": "busybox:latest"
                    }
                ]
            }
        }
    }
}
```

The first container has an empty securityContext and empty resources. The second does not even have those fields. Check how many violations this generates:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.kubernetes.security.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'app': must define resources.limits.cpu"
  },
  {
    "msg": "container 'app': must define resources.limits.memory"
  },
  {
    "msg": "container 'app': must include securityContext.capabilities.drop = [\"ALL\"]"
  },
  {
    "msg": "container 'app': must have securityContext.allowPrivilegeEscalation = false"
  },
  {
    "msg": "container 'app': must have securityContext.readOnlyRootFilesystem = true"
  },
  {
    "msg": "container 'app': must have securityContext.runAsNonRoot = true"
  },
  {
    "msg": "container 'sidecar': must define resources.limits.cpu"
  },
  {
    "msg": "container 'sidecar': must define resources.limits.memory"
  },
  {
    "msg": "container 'sidecar': must include securityContext.capabilities.drop = [\"ALL\"]"
  },
  {
    "msg": "container 'sidecar': must have securityContext.allowPrivilegeEscalation = false"
  },
  {
    "msg": "container 'sidecar': must have securityContext.readOnlyRootFilesystem = true"
  },
  {
    "msg": "container 'sidecar': must have securityContext.runAsNonRoot = true"
  }
]
```

Twelve violations. Six per container. The policy is thorough -- nothing slips through.

---

## The Secure Pod: Everything Right

Now test with a pod that satisfies all rules.

Create `input-secure.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "secure-pod"
            },
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "gcr.io/myproject/api:v1.0.0",
                        "securityContext": {
                            "runAsNonRoot": true,
                            "readOnlyRootFilesystem": true,
                            "allowPrivilegeEscalation": false,
                            "capabilities": {
                                "drop": ["ALL"]
                            }
                        },
                        "resources": {
                            "limits": {
                                "memory": "128Mi",
                                "cpu": "100m"
                            },
                            "requests": {
                                "memory": "64Mi",
                                "cpu": "50m"
                            }
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-secure.json "data.kubernetes.security.violation"
```

Expected output:

```json
[]
```

No violations. This pod would pass the admission controller.

---

## The Partially Secure Pod

In practice it is common to find pods that have some things right but not all. Here is one.

Create `input-partial.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "partial-pod"
            },
            "spec": {
                "containers": [
                    {
                        "name": "app",
                        "image": "gcr.io/myproject/api:v1.0.0",
                        "securityContext": {
                            "runAsNonRoot": true,
                            "readOnlyRootFilesystem": true,
                            "allowPrivilegeEscalation": true,
                            "capabilities": {
                                "drop": ["NET_RAW"]
                            }
                        },
                        "resources": {
                            "limits": {
                                "memory": "128Mi"
                            }
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-partial.json "data.kubernetes.security.violation"
```

Expected output:

```json
[
  {
    "msg": "container 'app': must define resources.limits.cpu"
  },
  {
    "msg": "container 'app': must include securityContext.capabilities.drop = [\"ALL\"]"
  },
  {
    "msg": "container 'app': must have securityContext.allowPrivilegeEscalation = false"
  }
]
```

Three violations out of six possible. `runAsNonRoot` and `readOnlyRootFilesystem` are fine. But `allowPrivilegeEscalation` is `true` (should be `false`), the capabilities drop `NET_RAW` but not `ALL`, and the CPU limit is missing (memory is set but CPU is not). The policy tells you exactly what needs to be fixed.

---

## A Deliberate Mistake: Dropping Some but Not All Capabilities

Notice how the partial pod drops `NET_RAW` instead of `ALL`. This is a common real-world scenario. A developer knows they should drop capabilities, so they drop a few specific ones. But the policy requires `ALL` because the principle of least privilege says you should start with nothing and add back only what you need. Dropping individual capabilities is better than nothing, but it leaves other capabilities enabled that could be exploited.

---

## With initContainers

Do not forget initContainers -- they must comply with the same rules.

Create `input-secure-with-init.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "pod-init"},
            "spec": {
                "initContainers": [
                    {
                        "name": "init-migrate",
                        "image": "gcr.io/myproject/migrator:v1",
                        "securityContext": {
                            "runAsNonRoot": true,
                            "readOnlyRootFilesystem": true,
                            "allowPrivilegeEscalation": false,
                            "capabilities": {"drop": ["ALL"]}
                        },
                        "resources": {
                            "limits": {"memory": "256Mi", "cpu": "200m"}
                        }
                    }
                ],
                "containers": [
                    {
                        "name": "app",
                        "image": "gcr.io/myproject/api:v1",
                        "securityContext": {
                            "runAsNonRoot": true,
                            "readOnlyRootFilesystem": true,
                            "allowPrivilegeEscalation": false,
                            "capabilities": {"drop": ["ALL"]}
                        },
                        "resources": {
                            "limits": {"memory": "128Mi", "cpu": "100m"}
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-secure-with-init.json "data.kubernetes.security.violation"
```

Expected output:

```json
[]
```

Both containers (init and regular) satisfy all the rules. Clean.

---

## Verify What You Learned

**Command 1** -- Count the violations on the fully insecure pod:

```bash
opa eval --format pretty -d policy.rego -i input.json "count(data.kubernetes.security.violation)"
```

Expected output: `12`

**Command 2** -- Confirm the secure pod passes:

```bash
opa eval --format pretty -d policy.rego -i input-secure.json "data.kubernetes.security.violation"
```

Expected output: `[]`

**Command 3** -- Count violations on the partially secure pod:

```bash
opa eval --format pretty -d policy.rego -i input-partial.json "count(data.kubernetes.security.violation)"
```

Expected output: `3`

**Command 4** -- A pod with everything right except the CPU limit:

Create `input-almost-secure.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {"name": "test"},
            "spec": {
                "containers": [{
                    "name": "almost",
                    "image": "app:v1",
                    "securityContext": {
                        "runAsNonRoot": true,
                        "readOnlyRootFilesystem": true,
                        "allowPrivilegeEscalation": false,
                        "capabilities": {"drop": ["ALL"]}
                    },
                    "resources": {
                        "limits": {"memory": "128Mi"}
                    }
                }]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-almost-secure.json "count(data.kubernetes.security.violation)"
```

Expected output: `1`

(One violation: missing `resources.limits.cpu`.)

---

## Section Summary

Across these four exercises you built a complete Kubernetes admission policy toolkit:

1. **Gatekeeper** -- You learned the ConstraintTemplate/Constraint architecture and how to extract Rego for local testing.
2. **Required Labels** -- You enforced label presence and format validation using parameterized regex patterns.
3. **Image Allowlist** -- You restricted container images to trusted registries, caught `:latest` tags and bare images, and covered initContainers.
4. **Security Contexts** -- You enforced non-root execution, read-only filesystems, privilege escalation blocking, capability drops, and resource limits.

Together with the Terraform policies from the previous section, you now have a policy-as-code practice that covers infrastructure provisioning (Terraform plans) and runtime workload security (Kubernetes admission). The same Rego language and OPA engine power both. The only difference is the input format: `resource_changes` for Terraform, `review.object` for Kubernetes.

## What's Next

You have the foundational skills for writing OPA policies across two major domains. From here you can explore writing unit tests with `opa test`, integrating policies into CI/CD pipelines, or extending Gatekeeper with external data sources.

## Reference

- [Kubernetes Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) -- the official baseline, restricted, and privileged profiles.
- [SecurityContext API Reference](https://kubernetes.io/docs/reference/generated/kubernetes-api/v1.28/#securitycontext-v1-core) -- full list of SecurityContext fields.
- [Linux Capabilities](https://man7.org/linux/man-pages/man7/capabilities.7.html) -- man page explaining all Linux capabilities.
- [Gatekeeper Library: Pod Security](https://open-policy-agent.github.io/gatekeeper-library/website/) -- pre-built templates for common security policies.

## Additional Resources

- [Kubernetes Resource Management](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/) -- how to set requests and limits for CPU and memory.
- [Kyverno](https://kyverno.io/) -- an alternative Kubernetes policy engine that uses YAML instead of Rego.
- [OPA Test Framework](https://www.openpolicyagent.org/docs/latest/policy-testing/) -- writing unit tests for Rego policies.
- [Gatekeeper Policy Manager](https://github.com/sighupio/gatekeeper-policy-manager) -- UI for managing Gatekeeper policies in a cluster.
