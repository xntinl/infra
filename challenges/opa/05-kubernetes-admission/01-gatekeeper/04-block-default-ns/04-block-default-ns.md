# Block the Default Namespace

## Learning Objectives

- Write a policy that checks `metadata.namespace`
- Understand how Gatekeeper applies governance across any resource kind

## Why This Policy

Using the `default` namespace in production is a common anti-pattern. Resources in `default` lack isolation and are easy to lose track of. This policy rejects any resource deployed to the `default` namespace.

---

## The Policy

Create `block-default-ns.rego`:

```rego
package blockdefaultns

import rego.v1

violation contains {"msg": msg} if {
    input.review.object.metadata.namespace == "default"
    msg := sprintf(
        "%s '%s' cannot be deployed to the 'default' namespace",
        [input.review.object.kind, input.review.object.metadata.name],
    )
}
```

The rule checks `metadata.namespace`. If it equals `"default"`, the resource is rejected regardless of its kind. Notice how the violation message includes the resource kind dynamically -- this same policy works for Pods, Deployments, Services, or any namespaced resource.

---

## Test 1: A Pod in the Default Namespace (Should Violate)

Create `input-default-ns-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "namespace": "default"
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-default-ns.rego -i input-default-ns-bad.json "data.blockdefaultns.violation"
```

Expected output:

```json
[
  {
    "msg": "Pod 'my-pod' cannot be deployed to the 'default' namespace"
  }
]
```

---

## Test 2: A Pod in the Production Namespace (Should Pass)

Create `input-default-ns-good.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "namespace": "production"
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-default-ns.rego -i input-default-ns-good.json "data.blockdefaultns.violation"
```

Expected output:

```json
[]
```

No violations. The namespace is not `default`, so the policy is satisfied.

---

## Verify

```bash
opa eval --format pretty -d block-default-ns.rego -i input-default-ns-bad.json "count(data.blockdefaultns.violation)"
# Expected: 1
```

## What's Next

Continue to [05-block-nodeport](../05-block-nodeport/05-block-nodeport.md) to write a policy for Services -- your first non-Pod policy.
