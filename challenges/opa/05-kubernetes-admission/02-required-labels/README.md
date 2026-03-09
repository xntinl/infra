# Required Labels: Metadata That Matters

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 05-01 (gatekeeper)

## Learning Objectives

After completing this exercise, you will be able to:

- Validate both the presence and the format of Kubernetes labels using `regex.match`
- Use parameterized policies where required labels and their patterns come from the input
- Write complementary violation rules that cover different failure modes without overlapping

## Why This Matters

In the previous exercise you validated that a pod had a specific label. But in real life it is not enough for the label to exist -- it needs a **valid format**. A label `team: "asdf123!!"` is useless. Here you will build a policy that validates multiple labels, each with its own regular expression.

---

## Labels vs Annotations

Before diving into code, a clarification that sometimes causes confusion:

- **Labels** are for **selection**. Kubernetes uses them actively: Services find pods by labels, ReplicaSets select pods by labels, NetworkPolicies filter by labels. Labels are how Kubernetes "finds" things.
- **Annotations** are for **information**. Metadata that humans or external tools read, but that Kubernetes does not use for selection. Things like `description`, `owner-email`, `documentation-url`.

That is why labels matter so much: if a pod does not have the correct label, a Service will not find it, your monitoring system will not classify it, and nobody will know which team owns it.

---

## Why Validate Format

Example: all pods must have a `team` label. Someone creates a pod with `team: "test"`. Technically it complies, but your ownership system expects values like `team: "platform"` or `team: "payments"` -- real team names, only lowercase letters. A label with an incorrect format breaks Grafana dashboards, PagerDuty alerts, and `kubectl` queries.

The solution: validate not just that the label exists, but that its value matches a regex pattern.

---

## Parameterizing the Labels

Hardcoding labels in the policy is fragile. If you need a new label tomorrow, you have to edit the Rego. Better to make it configurable: the required labels and their patterns come as parameters.

Create `policy.rego`:

```rego
package kubernetes.labels

import rego.v1

# violation generates a message for each missing label
violation contains {"msg": msg} if {
    some required in input.parameters.labels
    label_name := required.key
    not input.review.object.metadata.labels[label_name]
    msg := sprintf("required label '%s' is missing", [label_name])
}

# violation generates a message for each label with invalid format
violation contains {"msg": msg} if {
    some required in input.parameters.labels
    label_name := required.key
    label_value := input.review.object.metadata.labels[label_name]
    pattern := required.regex
    not regex.match(pattern, label_value)
    msg := sprintf(
        "label '%s' has value '%s' that does not match pattern '%s'",
        [label_name, label_value, pattern],
    )
}
```

There are two `violation` rules, and both feed the same set. The first catches missing labels. The second catches labels that exist but have an incorrect format. This gives you precise error messages for each case.

Notice the parameter structure you expect: a list of objects, each with `key` (label name) and `regex` (pattern to enforce).

---

## Scenario 1: Pod That Passes Everything

Create `input.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "app", "regex": "^[a-z][a-z0-9-]*$"},
            {"key": "team", "regex": "^[a-z]+$"},
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "api-server",
                "labels": {
                    "app": "api-server",
                    "team": "platform",
                    "env": "prod"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[]
```

All labels exist and match their patterns. No violations.

---

## Scenario 2: Missing a Label

Create `input-missing-label.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "app", "regex": "^[a-z][a-z0-9-]*$"},
            {"key": "team", "regex": "^[a-z]+$"},
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "labels": {
                    "app": "my-pod",
                    "env": "dev"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-missing-label.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[
  {
    "msg": "required label 'team' is missing"
  }
]
```

The pod has `app` and `env` but is missing `team`.

---

## Scenario 3: Label With Incorrect Format

Create `input-bad-format.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "app", "regex": "^[a-z][a-z0-9-]*$"},
            {"key": "team", "regex": "^[a-z]+$"},
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "my-pod",
                "labels": {
                    "app": "My-App",
                    "team": "platform",
                    "env": "production"
                }
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-bad-format.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[
  {
    "msg": "label 'app' has value 'My-App' that does not match pattern '^[a-z][a-z0-9-]*$'"
  },
  {
    "msg": "label 'env' has value 'production' that does not match pattern '^(dev|staging|prod)$'"
  }
]
```

Two violations. `My-App` has uppercase letters (the pattern only allows lowercase) and `production` is not in the `dev|staging|prod` list. Notice that both errors appear together -- the policy does not stop at the first one.

---

## Scenario 4: Everything Wrong

Create `input-no-labels.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "app", "regex": "^[a-z][a-z0-9-]*$"},
            {"key": "team", "regex": "^[a-z]+$"},
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "kind": "Pod",
            "metadata": {
                "name": "chaos-pod",
                "labels": {}
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-no-labels.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[
  {
    "msg": "required label 'app' is missing"
  },
  {
    "msg": "required label 'env' is missing"
  },
  {
    "msg": "required label 'team' is missing"
  }
]
```

No labels present. Three violations, one for each required label.

---

## How the Logic Works

It is worth pausing to see why the two rules do not conflict. The first rule has `not input.review.object.metadata.labels[label_name]` -- it only fires when the label **does not exist**. The second rule does `label_value := input.review.object.metadata.labels[label_name]` -- that assignment only succeeds when the label **does exist**. They complement each other perfectly: a label cannot be both absent and present at the same time.

And because both rules feed the same set `violation`, all messages accumulate. This is the power of incremental sets in Rego.

---

## A Deliberate Mistake: What If the Regex Is Wrong?

Suppose you accidentally write the pattern as `^[a-z+$` (missing the closing bracket). The `regex.match` builtin will return an error. Try it in your head: if the regex itself is malformed, OPA treats the expression as undefined, so the violation rule for format checking would never fire for that label. The label would silently pass even with a bad value. Always test your regex patterns independently before putting them into the policy parameters.

---

## Verify What You Learned

**Command 1** -- Confirm the compliant pod passes:

```bash
opa eval --format pretty -d policy.rego -i input.json "data.kubernetes.labels.violation"
```

Expected output: `[]`

**Command 2** -- A pod with a bad app value and missing team:

Create `input-bad-app-no-team.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "app", "regex": "^[a-z][a-z0-9-]*$"},
            {"key": "team", "regex": "^[a-z]+$"}
        ]
    },
    "review": {
        "object": {
            "metadata": {
                "name": "test",
                "labels": {"app": "123bad"}
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-bad-app-no-team.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[
  {
    "msg": "label 'app' has value '123bad' that does not match pattern '^[a-z][a-z0-9-]*$'"
  },
  {
    "msg": "required label 'team' is missing"
  }
]
```

**Command 3** -- A valid env label:

Create `input-valid-env.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "metadata": {
                "name": "test",
                "labels": {"env": "staging"}
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-valid-env.json "data.kubernetes.labels.violation"
```

Expected output: `[]`

**Command 4** -- An invalid env label:

Create `input-invalid-env.json`:

```json
{
    "parameters": {
        "labels": [
            {"key": "env", "regex": "^(dev|staging|prod)$"}
        ]
    },
    "review": {
        "object": {
            "metadata": {
                "name": "test",
                "labels": {"env": "testing"}
            }
        }
    }
}
```

```bash
opa eval --format pretty -d policy.rego -i input-invalid-env.json "data.kubernetes.labels.violation"
```

Expected output:

```json
[
  {
    "msg": "label 'env' has value 'testing' that does not match pattern '^(dev|staging|prod)$'"
  }
]
```

---

## What's Next

You can now enforce both the presence and format of labels. In the next exercise you will move from metadata to something more consequential: controlling which container images are allowed to run in your cluster.

## Reference

- [Kubernetes Labels and Selectors](https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/) -- how Kubernetes uses labels for selection and organization.
- [OPA Policy Reference: regex.match](https://www.openpolicyagent.org/docs/latest/policy-reference/#regex) -- the regex builtin used for pattern validation.
- [Gatekeeper Library: Required Labels](https://open-policy-agent.github.io/gatekeeper-library/website/validation/requiredlabels) -- the upstream Gatekeeper library template for required labels.

## Additional Resources

- [Kubernetes Recommended Labels](https://kubernetes.io/docs/concepts/overview/working-with-objects/common-labels/) -- standard label conventions from the Kubernetes project.
- [Regex101](https://regex101.com/) -- online regex tester useful for validating patterns before embedding them in policies.
- [OPA Playground](https://play.openpolicyagent.org/) -- browser-based Rego editor for quick experiments.
