# Require Health Probes

## Learning Objectives

- Iterate over `spec.containers` using `some container in ...`
- Check for the absence of nested fields with `not`
- Understand how a single rule body generates multiple violations across containers

## Why This Policy

Pods without health probes are invisible to Kubernetes orchestration. Without a `readinessProbe`, traffic is sent to pods that are not ready. Without a `livenessProbe`, stuck pods are never restarted. This policy requires both probes on every container.

---

## The Policy

Create `require-probes.rego`:

```rego
package requireprobes

import rego.v1

violation contains {"msg": msg} if {
    some container in input.review.object.spec.containers
    not container.readinessProbe
    msg := sprintf(
        "Container '%s' in Pod '%s' is missing a readinessProbe",
        [container.name, input.review.object.metadata.name],
    )
}

violation contains {"msg": msg} if {
    some container in input.review.object.spec.containers
    not container.livenessProbe
    msg := sprintf(
        "Container '%s' in Pod '%s' is missing a livenessProbe",
        [container.name, input.review.object.metadata.name],
    )
}
```

Two rules: one for readinessProbe, one for livenessProbe. They iterate over `spec.containers` with `some container in ...`. A container missing both probes triggers both rules. A pod with N containers missing both probes generates 2N violations.

---

## Test 1: A Pod With 2 Containers, No Probes (Should Produce 4 Violations)

Create `input-probes-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "api-pod",
                "namespace": "production"
            },
            "spec": {
                "containers": [
                    {
                        "name": "api",
                        "image": "myapp/api:1.0"
                    },
                    {
                        "name": "sidecar",
                        "image": "myapp/sidecar:1.0"
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d require-probes.rego -i input-probes-bad.json "data.requireprobes.violation"
```

Expected output (4 violations -- 2 containers x 2 missing probes):

```json
[
  {
    "msg": "Container 'api' in Pod 'api-pod' is missing a livenessProbe"
  },
  {
    "msg": "Container 'api' in Pod 'api-pod' is missing a readinessProbe"
  },
  {
    "msg": "Container 'sidecar' in Pod 'api-pod' is missing a livenessProbe"
  },
  {
    "msg": "Container 'sidecar' in Pod 'api-pod' is missing a readinessProbe"
  }
]
```

---

## Test 2: A Pod With Probes Configured (Should Pass)

Create `input-probes-good.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "api-pod",
                "namespace": "production"
            },
            "spec": {
                "containers": [
                    {
                        "name": "api",
                        "image": "myapp/api:1.0",
                        "readinessProbe": {
                            "httpGet": {"path": "/healthz", "port": 8080}
                        },
                        "livenessProbe": {
                            "httpGet": {"path": "/healthz", "port": 8080}
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d require-probes.rego -i input-probes-good.json "data.requireprobes.violation"
```

Expected output:

```json
[]
```

Both probes present. No violations.

---

## Verify

```bash
opa eval --format pretty -d require-probes.rego -i input-probes-bad.json "count(data.requireprobes.violation)"
# Expected: 4
```

## What's Next

Continue to [08-block-hostpath](../08-block-hostpath/) to write a policy that iterates over volumes and checks for the existence of a type-specific field.
