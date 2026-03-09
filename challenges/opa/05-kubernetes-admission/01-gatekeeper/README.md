# OPA Gatekeeper: Policies in Your Cluster

This section covers Gatekeeper -- the OPA-based admission controller for Kubernetes. It is divided into sub-sections that progress from theory to hands-on exercises.

## Sections

### Theory

| Section | Description |
|---------|-------------|
| [01-introduction](01-introduction/) | What admission controllers are, the full admission flow, and how Gatekeeper integrates as a ValidatingAdmissionWebhook |
| [02-concepts](02-concepts/) | Fail-open vs fail-closed, enforcement actions, constraint match fields, the audit controller, data replication, PSA vs Gatekeeper, and the gator CLI |

### Hands-On

| Section | Description |
|---------|-------------|
| [03-first-policy](03-first-policy/) | ConstraintTemplate vs Constraint, extracting Rego for local testing, and 4 scenarios with required-labels |
| [04-block-default-ns](04-block-default-ns/) | Block resources deployed to the `default` namespace |
| [05-block-nodeport](05-block-nodeport/) | Block Services of type NodePort |
| [06-block-host-ns](06-block-host-ns/) | Block pods using hostNetwork, hostPID, or hostIPC |
| [07-require-probes](07-require-probes/) | Require readinessProbe and livenessProbe on every container |
| [08-block-hostpath](08-block-hostpath/) | Block pods mounting hostPath volumes |
| [09-block-wildcard-ingress](09-block-wildcard-ingress/) | Block Ingress resources with wildcard or missing hosts |

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed section 04 (Terraform policy)
- Basic familiarity with Kubernetes concepts (pods, API server, YAML manifests)

## What's Next

After completing all sections here, continue with the remaining exercises in `05-kubernetes-admission/`:

- **02-required-labels**: Label format validation with regex patterns
- **03-image-allowlist**: Container image registry and tag policies
- **04-security-contexts**: SecurityContext fields and resource limits
