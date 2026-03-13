# Block Wildcard Ingress

## Learning Objectives

- Write a policy for Ingress resources (non-Pod, non-Service)
- Combine string comparison (`== "*"`) with field absence detection (`not rule.host`)
- Understand how two separate violation rules cover different failure modes

## Why This Policy

An Ingress with a wildcard host (`"*"`) or no host at all matches all incoming traffic. This is dangerous in shared clusters -- it can intercept traffic intended for other services. This policy blocks Ingress resources with wildcard or missing hosts.

---

## The Policy

Create `block-wildcard-ingress.rego`:

```rego
package blockwildcardingress

import rego.v1

violation contains {"msg": msg} if {
    some rule in input.review.object.spec.rules
    rule.host == "*"
    msg := sprintf(
        "Ingress '%s' uses wildcard host '*' which is not allowed",
        [input.review.object.metadata.name],
    )
}

violation contains {"msg": msg} if {
    some rule in input.review.object.spec.rules
    not rule.host
    msg := sprintf(
        "Ingress '%s' has a rule without a host which matches all traffic; specify an explicit host",
        [input.review.object.metadata.name],
    )
}
```

Two rules: one catches the explicit wildcard `"*"`, the other catches rules where `host` is absent (which also matches all traffic in Kubernetes). These are complementary -- a rule cannot trigger both because `rule.host == "*"` requires the field to exist, while `not rule.host` requires it to not exist.

This is a policy on an **Ingress** resource -- another example of Gatekeeper's ability to enforce policies on any resource type. The Constraint's `match.kinds` would target `Ingress` in the `networking.k8s.io` API group.

---

## Test 1: An Ingress With a Wildcard Host (Should Violate)

Create `input-ingress-bad.json`:

```json
{
    "review": {
        "object": {
            "kind": "Ingress",
            "metadata": {
                "name": "catch-all",
                "namespace": "default"
            },
            "spec": {
                "rules": [
                    {
                        "host": "*",
                        "http": {
                            "paths": [
                                {
                                    "path": "/",
                                    "pathType": "Prefix",
                                    "backend": {
                                        "service": {"name": "web", "port": {"number": 80}}
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-wildcard-ingress.rego -i input-ingress-bad.json "data.blockwildcardingress.violation"
```

Expected output:

```json
[
  {
    "msg": "Ingress 'catch-all' uses wildcard host '*' which is not allowed"
  }
]
```

---

## Test 2: An Ingress With a Specific Host (Should Pass)

Create `input-ingress-good.json`:

```json
{
    "review": {
        "object": {
            "kind": "Ingress",
            "metadata": {
                "name": "api-ingress",
                "namespace": "production"
            },
            "spec": {
                "rules": [
                    {
                        "host": "api.example.com",
                        "http": {
                            "paths": [
                                {
                                    "path": "/",
                                    "pathType": "Prefix",
                                    "backend": {
                                        "service": {"name": "api", "port": {"number": 80}}
                                    }
                                }
                            ]
                        }
                    }
                ]
            }
        }
    }
}
```

```bash
opa eval --format pretty -d block-wildcard-ingress.rego -i input-ingress-good.json "data.blockwildcardingress.violation"
```

Expected output:

```json
[]
```

The host is `api.example.com` -- specific, not a wildcard, and present. No violations.

---

## Verify

```bash
opa eval --format pretty -d block-wildcard-ingress.rego -i input-ingress-bad.json "count(data.blockwildcardingress.violation)"
# Expected: 1
```

## Summary

You have now written policies for Pods, Services, and Ingresses covering namespaces, network exposure, host isolation, health probes, volume security, and traffic routing. Each exercise introduced a different Rego pattern:

| Exercise | Resource | Rego Pattern |
|----------|----------|-------------|
| [03-first-policy](../03-first-policy/03-first-policy.md) | Any | Iteration over parameters, `not` for missing labels |
| [04-block-default-ns](../04-block-default-ns/04-block-default-ns.md) | Any | Simple field comparison on metadata |
| [05-block-nodeport](../05-block-nodeport/05-block-nodeport.md) | Service | Field comparison on spec |
| [06-block-host-ns](../06-block-host-ns/06-block-host-ns.md) | Pod | Multiple rules, explicit `== true` for booleans |
| [07-require-probes](../07-require-probes/07-require-probes.md) | Pod | Iteration over containers, `not` for nested fields |
| [08-block-hostpath](../08-block-hostpath/08-block-hostpath.md) | Pod | Iteration over volumes, field existence as type check |
| [09-block-wildcard-ingress](../09-block-wildcard-ingress/09-block-wildcard-ingress.md) | Ingress | String comparison + field absence detection |

## What's Next

Return to the [main section](../01-gatekeeper.md) to continue with the deeper exercises:

- **02-required-labels**: Label format validation with regex patterns
- **03-image-allowlist**: Container image registry and tag policies
- **04-security-contexts**: SecurityContext fields and resource limits

Each builds on the Rego patterns you practiced here but introduces new concepts and more complex rule logic.

## Reference

- [OPA Gatekeeper Documentation](https://open-policy-agent.github.io/gatekeeper/website/docs/) -- official Gatekeeper project docs.
- [ConstraintTemplate Howto](https://open-policy-agent.github.io/gatekeeper/website/docs/howto) -- how to write and deploy ConstraintTemplates.
- [Kubernetes Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) -- how admission controllers work in the Kubernetes API server.
- [Kubernetes API Concepts](https://kubernetes.io/docs/reference/using-api/api-concepts/) -- understanding API request flow.
- [OPA Playground](https://play.openpolicyagent.org/) -- browser-based Rego editor for quick experiments.
