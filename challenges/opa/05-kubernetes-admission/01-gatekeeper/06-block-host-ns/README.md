# Block Host Namespaces

## Learning Objectives

- Write multiple violation rules in a single policy
- Compare boolean fields explicitly with `== true` to handle undefined fields correctly

## Why This Policy

A pod with `hostNetwork: true`, `hostPID: true`, or `hostIPC: true` shares the corresponding Linux namespace with the host node. This breaks container isolation -- the pod can see host network interfaces, processes, or inter-process communication. These fields should be blocked in most workloads.

---

## The Policy

Create `block-host-ns.rego`:

```rego
package blockhostns

import rego.v1

violation contains {"msg": msg} if {
    input.review.object.spec.hostNetwork == true
    msg := sprintf(
        "Pod '%s' has hostNetwork enabled which is not allowed",
        [input.review.object.metadata.name],
    )
}

violation contains {"msg": msg} if {
    input.review.object.spec.hostPID == true
    msg := sprintf(
        "Pod '%s' has hostPID enabled which is not allowed",
        [input.review.object.metadata.name],
    )
}

violation contains {"msg": msg} if {
    input.review.object.spec.hostIPC == true
    msg := sprintf(
        "Pod '%s' has hostIPC enabled which is not allowed",
        [input.review.object.metadata.name],
    )
}
```

Each field gets its own rule so violations report exactly which host namespace is enabled.

Note the explicit `== true` comparison. If the field is absent from the pod spec, it is `undefined` in Rego -- the comparison fails and no violation fires. This is the correct behavior because absent means the default (`false`). If you wrote just `input.review.object.spec.hostNetwork` without `== true`, it would also work for present-and-true values, but the explicit comparison makes the intent clearer and avoids surprises with non-boolean values.

---

## Test 1: A Pod With hostNetwork and hostPID (Should Produce 2 Violations)

Create `input-hostns-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "debug-pod",
                "namespace": "default"
            },
            "spec": {
                "hostNetwork": true,
                "hostPID": true,
                "containers": [
                    {"name": "debug", "image": "busybox:1.36"}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-host-ns.rego -i input-hostns-bad.json "data.blockhostns.violation"
```

Expected output (2 violations):

```json
[
  {
    "msg": "Pod 'debug-pod' has hostNetwork enabled which is not allowed"
  },
  {
    "msg": "Pod 'debug-pod' has hostPID enabled which is not allowed"
  }
]
```

---

## Test 2: A Pod Without Host Namespaces (Should Pass)

Create `input-hostns-good.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "normal-pod",
                "namespace": "production"
            },
            "spec": {
                "containers": [
                    {"name": "app", "image": "nginx:1.25"}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-host-ns.rego -i input-hostns-good.json "data.blockhostns.violation"
```

Expected output:

```json
[]
```

No `hostNetwork`, `hostPID`, or `hostIPC` fields present. All three rules evaluate to undefined, so no violations.

---

## Verify

```bash
opa eval --format pretty -d block-host-ns.rego -i input-hostns-bad.json "count(data.blockhostns.violation)"
# Expected: 2
```

## What's Next

Continue to [07-require-probes](../07-require-probes/) to write a policy that iterates over containers and checks for nested fields.
