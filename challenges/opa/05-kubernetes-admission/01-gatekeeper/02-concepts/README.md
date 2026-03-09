# Gatekeeper Concepts

## Learning Objectives

After completing this section, you will be able to:

- Choose between fail-open and fail-closed failure policies
- Use enforcement actions (deny, warn, dryrun) for safe rollout
- Configure match fields to scope policies to specific resources
- Explain how the audit controller catches pre-existing violations
- Compare Pod Security Admission with Gatekeeper

---

## Fail-Open vs Fail-Closed

What happens if Gatekeeper itself is down -- crashed, overloaded, or unreachable?

The webhook configuration has a `failurePolicy` field that controls this:

- **`Ignore`** (fail-open): If Gatekeeper cannot be reached, the API server ignores the webhook and allows the request. This is the default.
- **`Fail`** (fail-closed): If Gatekeeper cannot be reached, the API server rejects the request. Nothing gets through without policy evaluation.

**Analogy**: Think of an electronic security door. Fail-open means the door unlocks when the power goes out -- people can leave (safety). Fail-closed means the door stays locked when the power goes out -- nothing gets in (security). Neither is universally correct; it depends on your priorities.

**Fail-open** is the safer default for availability. If Gatekeeper crashes, the cluster keeps working. The risk is that resources can be created without policy checks during the outage.

**Fail-closed** is the safer choice for security-critical clusters. Nothing bypasses policy. The risk is that if Gatekeeper goes down, the entire cluster becomes unable to create or modify resources -- including system components in `kube-system`.

Mitigations for fail-closed:

- Run 3 or more Gatekeeper replicas behind a Service for high availability.
- Exempt `kube-system` and other critical namespaces using the webhook's `namespaceSelector`.
- Configure proper health checks and pod disruption budgets.
- Use `matchExpressions` in the webhook to exclude resources Gatekeeper itself needs.

---

## Enforcement Actions

Not every policy needs to block resources immediately. Gatekeeper supports three enforcement actions:

| Action   | Blocks request? | Shows warnings? | Visible in audit? |
|----------|:-:|:-:|:-:|
| `deny`   | Yes | Yes (as errors) | Yes |
| `warn`   | No  | Yes (as warnings in kubectl output) | Yes |
| `dryrun` | No  | No  | Yes |

**`deny`** is the default. The request is rejected and the user sees the violation messages as errors.

**`warn`** allows the request but includes the violation messages as warnings in the API response. Users see them in `kubectl` output. Useful for soft rollouts -- teams see what would break without being blocked.

**`dryrun`** is completely silent to the user. Violations are only recorded internally and visible via the audit controller (see next section). Useful for measuring impact before enabling enforcement.

The recommended rollout pattern for a new policy:

1. Deploy with `enforcementAction: dryrun`. Wait for the audit controller to scan existing resources.
2. Check `.status.violations` on the Constraint to see what would fail.
3. Promote to `enforcementAction: warn`. Users start seeing warnings.
4. Once teams have fixed their resources, promote to `enforcementAction: deny`.

Setting the enforcement action in a Constraint:

```yaml
apiVersion: constraints.gatekeeper.sh/v1beta1
kind: K8sRequiredLabels
metadata:
  name: require-app-label
spec:
  enforcementAction: warn    # <-- dryrun | warn | deny
  match:
    kinds:
      - apiGroups: [""]
        kinds: ["Pod"]
  parameters:
    labels: ["app"]
```

---

## Constraint Match Fields

The `match` section of a Constraint determines which resources the policy applies to. Available fields:

**`kinds`**: List of API group + kind pairs.

```yaml
match:
  kinds:
    - apiGroups: [""]          # core API group
      kinds: ["Pod"]
    - apiGroups: ["apps"]
      kinds: ["Deployment"]
```

**`namespaces` / `excludedNamespaces`**: Whitelist or blacklist specific namespaces.

```yaml
match:
  namespaces: ["production", "staging"]
  # OR
  excludedNamespaces: ["kube-system", "gatekeeper-system"]
```

**`labelSelector`**: Only match resources with specific labels.

```yaml
match:
  labelSelector:
    matchLabels:
      environment: production
```

**`namespaceSelector`**: Only match resources in namespaces with specific labels.

```yaml
match:
  namespaceSelector:
    matchLabels:
      policy-enforced: "true"
```

**`scope`**: `Cluster` (cluster-scoped resources only), `Namespaced` (namespaced resources only), or `*` (both). Default is `*`.

All match fields are combined with AND. A resource must satisfy every specified field to be evaluated. If you specify `kinds: Pod` and `namespaces: ["production"]`, only Pods in the production namespace are checked.

Important: match filtering is handled by Gatekeeper, not your Rego. Your Rego only sees resources that already passed the match filter. You do not need to check `input.review.object.kind` inside your policy if `match.kinds` already restricts it.

---

## The Audit Controller

Gatekeeper's webhook only intercepts **new** requests. Resources that existed before a policy was deployed are not checked by the webhook.

The **audit controller** fills this gap. It runs inside Gatekeeper and periodically scans all resources in the cluster (default: every 60 seconds). For each resource, it evaluates all matching Constraints and records violations.

You can see audit results in the Constraint's status:

```bash
kubectl get k8srequiredlabels require-app-label -o yaml
```

```yaml
status:
  totalViolations: 3
  violations:
    - enforcementAction: deny
      kind: Pod
      name: legacy-service
      namespace: default
      message: "label 'app' is required"
```

This is why `dryrun` is valuable: deploy the policy, wait for the audit cycle, and inspect `.status.violations` to see every non-compliant resource in the cluster before enforcing.

**Analogy**: The webhook is the guard at the door checking everyone who enters. The audit controller is the guard who walks through the building checking that everyone inside has valid credentials. Both are needed -- one prevents new violations, the other catches existing ones.

---

## Data Replication

By default, your Rego policy only sees the resource being admitted (`input.review.object`). It cannot see other resources in the cluster.

For cross-resource policies (e.g., "reject this Ingress if another Ingress already uses this hostname"), Gatekeeper supports **data replication**. You configure a `Config` or `SyncSet` resource to tell Gatekeeper which resource types to cache. Cached resources become available in your Rego via `data.inventory`.

This is an advanced topic. The exercises in this section do not use data replication -- each policy only examines the single resource being admitted. Mentioning it here so you know it exists when you need it.

---

## Pod Security Admission vs Gatekeeper

Kubernetes 1.25+ includes a built-in admission controller called **Pod Security Admission (PSA)**. It is worth understanding when to use each.

| | Pod Security Admission | Gatekeeper |
|---|---|---|
| **Built-in** | Yes (K8s 1.25+) | No (separate install) |
| **Policy language** | 3 fixed profiles | Rego (arbitrary logic) |
| **Profiles** | Privileged, Baseline, Restricted | Unlimited custom policies |
| **Resource types** | Pods only | Any Kubernetes resource |
| **Mutation** | No | Yes (with assign/modify) |
| **Custom parameters** | No | Yes |
| **Offline testing** | No | Yes (`opa eval`, `gator test`) |
| **Audit** | No | Yes (audit controller) |

PSA is simple and zero-maintenance -- three profiles, applied per-namespace with a label. But it only covers pod security and cannot be customized.

Gatekeeper is more powerful but requires installation and maintenance. It handles any resource type, supports custom logic, and is testable offline.

They can coexist. A common pattern: use PSA as a baseline floor (e.g., enforce `restricted` profile cluster-wide), and Gatekeeper for everything beyond pod security -- label governance, image policies, network policies, naming conventions.

---

## Gatekeeper Library and gator CLI

The [Gatekeeper Library](https://github.com/open-policy-agent/gatekeeper-library) is an official collection of pre-built ConstraintTemplates. It covers common policies organized into categories like `pod-security-policy` and `general`. Before writing a policy from scratch, check if the library already has one.

The **gator CLI** is Gatekeeper's testing tool. It can evaluate a complete pipeline of ConstraintTemplates + Constraints + resources from YAML files, without a cluster:

```bash
gator test --filename=templates/ --filename=constraints/ --filename=resources/
```

The exercises in this section use `opa eval` for consistency with the rest of the OPA track. If you adopt Gatekeeper in production, `gator test` is the recommended tool for CI pipelines because it handles the full YAML-to-Rego wiring automatically.

---

## Reference

- [OPA Gatekeeper Documentation](https://open-policy-agent.github.io/gatekeeper/website/docs/) -- official Gatekeeper project docs.
- [Kubernetes Admission Controllers](https://kubernetes.io/docs/reference/access-authn-authz/admission-controllers/) -- how admission controllers work in the Kubernetes API server.
- [Pod Security Standards](https://kubernetes.io/docs/concepts/security/pod-security-standards/) -- the three built-in PSA profiles (Privileged, Baseline, Restricted).
- [gator CLI Reference](https://open-policy-agent.github.io/gatekeeper/website/docs/gator/) -- Gatekeeper's offline testing tool.
- [Gatekeeper Library](https://open-policy-agent.github.io/gatekeeper-library/website/) -- pre-built ConstraintTemplates for common policies.
- [Gatekeeper Library GitHub](https://github.com/open-policy-agent/gatekeeper-library) -- source code and examples for the library.

## What's Next

Continue to [03-first-policy](../03-first-policy/) to write and test your first Gatekeeper policy locally.
