# Your First Gatekeeper Policy

## Learning Objectives

After completing this section, you will be able to:

- Distinguish between ConstraintTemplates (reusable logic) and Constraints (applied instances)
- Extract Rego from a ConstraintTemplate and test it locally without a cluster
- Write and evaluate a required-labels policy against different inputs

---

## ConstraintTemplate vs Constraint

Gatekeeper introduces two key concepts:

- **ConstraintTemplate** defines the Rego logic and the parameters it accepts. It is a CRD that acts as a reusable definition.
- **Constraint** is an instance of a ConstraintTemplate. It specifies the parameter values and the resources it applies to (via `match`).

A simplified ConstraintTemplate in YAML looks like this:

```yaml
apiVersion: templates.gatekeeper.sh/v1
kind: ConstraintTemplate
metadata:
  name: k8srequiredlabels
spec:
  crd:
    spec:
      names:
        kind: K8sRequiredLabels
      validation:
        openAPIV3Schema:
          type: object
          properties:
            labels:
              type: array
              items:
                type: string
  targets:
    - target: admission.k8s.gatekeeper.sh
      rego: |
        package k8srequiredlabels

        import rego.v1

        violation contains {"msg": msg} if {
            some required_label in input.parameters.labels
            not input.review.object.metadata.labels[required_label]
            msg := sprintf("label '%s' is required", [required_label])
        }
```

The important part is inside `rego: |`. That is pure Rego, and it is what you will test locally.

Notice `input.review.object` -- that is the structure Gatekeeper passes to your policy. `review.object` is the Kubernetes resource that someone is trying to create or modify.

Now you know the full picture: Gatekeeper registers as a webhook, receives AdmissionReview requests, evaluates the Rego from matching ConstraintTemplates, and returns allowed/denied. The theory sections explained each piece. From here on, you will write and test the Rego locally.

---

## Extracting the Rego and Testing Locally

You do not need a cluster to test the logic. You extract the Rego from the template and create an `input.json` that simulates what Gatekeeper would pass.

Create `policy.rego`:

```rego
package k8srequiredlabels

import rego.v1

violation contains {"msg": msg} if {
    some required_label in input.parameters.labels
    not input.review.object.metadata.labels[required_label]
    msg := sprintf("label '%s' is required", [required_label])
}
```

The rule `violation` is a set. Each element is an object with a `msg` field. If the set is empty, the resource passes. If it has elements, Gatekeeper rejects the resource and displays the messages.

---

## Scenario 1: A Pod Missing a Required Label

Create `input.json` -- a pod without the `app` label:

```json
{
    "parameters": {
        "labels": ["app"]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "labels": {
                    "env": "dev"
                }
            }
        }
    }
}
```

Evaluate it:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.k8srequiredlabels.violation"
```

Expected output:

```json
[
  {
    "msg": "label 'app' is required"
  }
]
```

The pod has `env: dev` but is missing `app`. The violation fires.

---

## Scenario 2: A Pod With the Required Label

Now test with a pod that has the label.

Create `input-good-pod.json`:

```json
{
    "parameters": {
        "labels": ["app"]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "labels": {
                    "app": "backend",
                    "env": "dev"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-good-pod.json "data.k8srequiredlabels.violation"
```

Expected output:

```json
[]
```

Empty set. No violations. The admission controller allows the pod.

---

## Scenario 3: Multiple Required Labels, One Missing

Create `input-missing-team.json`:

```json
{
    "parameters": {
        "labels": ["app", "team"]
    },
    "review": {
        "object": {
            "metadata": {
                "name": "test",
                "labels": {
                    "app": "api"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-missing-team.json "data.k8srequiredlabels.violation"
```

Expected output:

```json
[
  {
    "msg": "label 'team' is required"
  }
]
```

The pod has `app` but is missing `team`. Only the missing label generates a violation -- the one that is present passes silently.

---

## Scenario 4: All Required Labels Present

Create `input-all-labels.json`:

```json
{
    "parameters": {
        "labels": ["app", "team"]
    },
    "review": {
        "object": {
            "metadata": {
                "name": "test",
                "labels": {
                    "app": "api",
                    "team": "platform"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-all-labels.json "data.k8srequiredlabels.violation"
```

Expected output:

```json
[]
```

Both labels present. Clean pass.

---

## Verify What You Learned

Run each command and confirm the output matches:

```bash
# Bad pod triggers violation
opa eval --format pretty -d policy.rego -i input.json "data.k8srequiredlabels.violation"
# Expected: [{"msg": "label 'app' is required"}]

# Good pod passes
opa eval --format pretty -d policy.rego -i input-good-pod.json "data.k8srequiredlabels.violation"
# Expected: []

# Count violations for a pod missing one of two required labels
opa eval --format pretty -d policy.rego -i input-missing-team.json "count(data.k8srequiredlabels.violation)"
# Expected: 1

# Zero violations when all labels present
opa eval --format pretty -d policy.rego -i input-all-labels.json "count(data.k8srequiredlabels.violation)"
# Expected: 0
```

## What's Next

You have the basic Gatekeeper pattern: extract Rego, simulate the input, test locally. Continue to [04-block-default-ns](../04-block-default-ns/) to apply this pattern to a governance policy on a different resource field.
