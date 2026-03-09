# Block NodePort Services

## Learning Objectives

- Write a policy that targets Service resources instead of Pods
- Understand that Gatekeeper can enforce rules on any Kubernetes resource type

## Why This Policy

NodePort Services expose a port on every node in the cluster. In many environments, this is undesirable -- it bypasses load balancers, complicates firewall rules, and exposes services directly to the network. This policy blocks Services of type `NodePort`.

---

## The Policy

Create `block-nodeport.rego`:

```rego
package blocknodeport

import rego.v1

violation contains {"msg": msg} if {
    input.review.object.spec.type == "NodePort"
    msg := sprintf(
        "Service '%s' uses type NodePort which is not allowed; use ClusterIP or LoadBalancer instead",
        [input.review.object.metadata.name],
    )
}
```

This is a policy on a **Service**, not a Pod. In a real Gatekeeper deployment, the Constraint's `match.kinds` would target `Service` in the core API group. The Rego itself is simple -- a single string comparison on `spec.type`.

---

## Test 1: A NodePort Service (Should Violate)

Create `input-nodeport-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Service",
            "metadata": {
                "name": "my-service",
                "namespace": "production"
            },
            "spec": {
                "type": "NodePort",
                "ports": [
                    {"port": 80, "targetPort": 8080, "nodePort": 30080}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-nodeport.rego -i input-nodeport-bad.json "data.blocknodeport.violation"
```

Expected output:

```json
[
  {
    "msg": "Service 'my-service' uses type NodePort which is not allowed; use ClusterIP or LoadBalancer instead"
  }
]
```

---

## Test 2: A ClusterIP Service (Should Pass)

Create `input-nodeport-clusterip.json`:

```json
{
    "review": {
        "object": {
            "kind": "Service",
            "metadata": {
                "name": "my-service",
                "namespace": "production"
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [
                    {"port": 80, "targetPort": 8080}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-nodeport.rego -i input-nodeport-clusterip.json "data.blocknodeport.violation"
```

Expected output:

```json
[]
```

---

## Test 3: A LoadBalancer Service (Should Pass)

Create `input-nodeport-lb.json`:

```json
{
    "review": {
        "object": {
            "kind": "Service",
            "metadata": {
                "name": "my-service",
                "namespace": "production"
            },
            "spec": {
                "type": "LoadBalancer",
                "ports": [
                    {"port": 443, "targetPort": 8443}
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-nodeport.rego -i input-nodeport-lb.json "data.blocknodeport.violation"
```

Expected output:

```json
[]
```

LoadBalancer is allowed. Only `NodePort` triggers the violation.

---

## Verify

```bash
opa eval --format pretty -d block-nodeport.rego -i input-nodeport-bad.json "count(data.blocknodeport.violation)"
# Expected: 1
```

## What's Next

Continue to [06-block-host-ns](../06-block-host-ns/) to write a policy with multiple rules checking boolean fields in the pod spec.
