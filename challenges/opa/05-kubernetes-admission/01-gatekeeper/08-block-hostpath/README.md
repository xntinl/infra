# Block hostPath Volumes

## Learning Objectives

- Iterate over `spec.volumes` and check for a type-specific field
- Understand how `volume.hostPath` being defined vs undefined distinguishes volume types in Rego

## Why This Policy

`hostPath` volumes mount a directory from the host node's filesystem into a pod. This is dangerous -- a pod with a `hostPath` mount to `/` can read and write the entire node. This policy blocks pods that use hostPath volumes.

---

## The Policy

Create `block-hostpath.rego`:

```rego
package blockhostpath

import rego.v1

violation contains {"msg": msg} if {
    some volume in input.review.object.spec.volumes
    volume.hostPath
    msg := sprintf(
        "Pod '%s' mounts hostPath volume '%s' which is not allowed",
        [input.review.object.metadata.name, volume.name],
    )
}
```

The rule iterates over `spec.volumes`. For each volume, `volume.hostPath` is only defined if the volume type is `hostPath`. If the volume is `emptyDir`, `configMap`, or any other type, `volume.hostPath` is undefined and the rule does not fire.

This pattern -- checking for the existence of a type-discriminator field -- is common in Kubernetes Rego policies. The JSON structure of a Volume uses different keys for different types, so checking whether a key exists tells you the type.

---

## Test 1: A Pod With a hostPath Volume (Should Violate)

Create `input-hostpath-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "miner-pod",
                "namespace": "default"
            },
            "spec": {
                "containers": [
                    {"name": "app", "image": "nginx:1.25"}
                ],
                "volumes": [
                    {
                        "name": "host-root",
                        "hostPath": {
                            "path": "/",
                            "type": "Directory"
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-hostpath.rego -i input-hostpath-bad.json "data.blockhostpath.violation"
```

Expected output:

```json
[
  {
    "msg": "Pod 'miner-pod' mounts hostPath volume 'host-root' which is not allowed"
  }
]
```

---

## Test 2: A Pod With an emptyDir Volume (Should Pass)

Create `input-hostpath-good.json`:

```json
{
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "cache-pod",
                "namespace": "production"
            },
            "spec": {
                "containers": [
                    {"name": "app", "image": "nginx:1.25"}
                ],
                "volumes": [
                    {
                        "name": "cache",
                        "emptyDir": {}
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-hostpath.rego -i input-hostpath-good.json "data.blockhostpath.violation"
```

Expected output:

```json
[]
```

The volume is `emptyDir`, not `hostPath`. The rule does not fire.

---

## Verify

```bash
opa eval --format pretty -d block-hostpath.rego -i input-hostpath-bad.json "count(data.blockhostpath.violation)"
# Expected: 1
```

## What's Next

Continue to [09-block-wildcard-ingress](../09-block-wildcard-ingress/) to write a policy for Ingress resources -- combining string comparison with field absence detection.
