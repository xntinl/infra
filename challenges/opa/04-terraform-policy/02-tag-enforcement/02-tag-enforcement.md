# Tag Enforcement: Bringing Order to Your Infrastructure

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 04-01 (plan-json)

## Learning Objectives

After completing this exercise, you will be able to:

- Build data-driven policies where the rules come from a configuration file, not hardcoded Rego
- Handle the `tags: null` edge case that Terraform produces when tags are not set
- Separate violation detection from deny decisions to gain flexibility in filtering and reporting

## Why This Matters

If you have ever opened the AWS console and found 47 EC2 instances with no name, no team, and no indication of where they came from or who created them, you know the pain. Tags are the difference between an organized AWS account and a junk drawer.

But tags only help if they are **consistent**. It does no good when one team uses `env: production` and another uses `Environment: prod`. You need clear, automated rules.

The plan here is simple: use OPA to intercept the Terraform plan and verify that every taggable resource has the required tags with valid values. And you will do it in a **data-driven** way -- the required tags come from a configuration file, not baked into the policy.

---

## Why Data-Driven

You could write a rule that says "the Environment tag is required" directly in Rego. But then every time you want to add a required tag you have to modify the policy. It is the same problem as hardcoding configuration values in application code -- better to externalize them.

When the required tags come from a data file, you can update the configuration without touching the logic:

```
+--------------+     +--------------+     +-------------+
| config.json  |---->| policy.rego  |<----| tfplan.json |
| (which tags) |     | (how to      |     | (what       |
|              |     |  check)      |     |  exists)    |
+--------------+     +--------------+     +-------------+
```

The policy is the **how**, the configuration is the **what**, and the plan is the **input**. Three separate things, each with its own responsibility.

---

## Preparing the Data

Start with the tag configuration. Here you define which tags are required and what values are valid.

Create `config.json`:

```json
{
  "required_tags": {
    "Environment": {
      "allowed_values": ["dev", "staging", "prod"],
      "description": "Deployment environment"
    },
    "Team": {
      "allowed_values": ["platform", "backend", "frontend", "data", "security"],
      "description": "Responsible team"
    },
    "ManagedBy": {
      "allowed_values": ["terraform"],
      "description": "Management tool"
    }
  },
  "taggable_resource_prefixes": [
    "aws_s3_bucket",
    "aws_instance",
    "aws_db_instance",
    "aws_security_group",
    "aws_lambda_function",
    "aws_sqs_queue",
    "aws_sns_topic"
  ]
}
```

Notice that you also define `taggable_resource_prefixes`. Not every AWS resource supports tags (for example, `aws_iam_policy_attachment` does not). Instead of guessing, you explicitly list the types that must have tags.

Now the Terraform plan with several resources -- some correct, some with problems.

Create `tfplan.json`:

```json
{
  "format_version": "1.2",
  "terraform_version": "1.7.0",
  "resource_changes": [
    {
      "address": "aws_s3_bucket.app_data",
      "type": "aws_s3_bucket",
      "name": "app_data",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "bucket": "myapp-data-prod",
          "tags": {
            "Environment": "prod",
            "Team": "platform",
            "ManagedBy": "terraform"
          }
        }
      }
    },
    {
      "address": "aws_instance.web",
      "type": "aws_instance",
      "name": "web",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "ami": "ami-0c55b159cbfafe1f0",
          "instance_type": "t3.medium",
          "tags": {
            "Environment": "production",
            "Team": "backend"
          }
        }
      }
    },
    {
      "address": "aws_db_instance.main",
      "type": "aws_db_instance",
      "name": "main",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "identifier": "main-db",
          "engine": "postgres",
          "instance_class": "db.t3.medium",
          "tags": {
            "Environment": "prod",
            "Team": "data",
            "ManagedBy": "terraform"
          }
        }
      }
    },
    {
      "address": "aws_security_group.api",
      "type": "aws_security_group",
      "name": "api",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "name": "api-sg",
          "tags": {
            "Environment": "dev"
          }
        }
      }
    },
    {
      "address": "aws_lambda_function.processor",
      "type": "aws_lambda_function",
      "name": "processor",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "function_name": "data-processor",
          "runtime": "python3.12",
          "tags": null
        }
      }
    },
    {
      "address": "aws_iam_role.lambda_role",
      "type": "aws_iam_role",
      "name": "lambda_role",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "name": "lambda-execution-role",
          "assume_role_policy": "{...}"
        }
      }
    }
  ]
}
```

This is a sampler of problems:

- `aws_s3_bucket.app_data` -- perfect, all 3 tags with valid values.
- `aws_instance.web` -- has `Environment: "production"` instead of `"prod"`, and is missing `ManagedBy`.
- `aws_db_instance.main` -- perfect as well.
- `aws_security_group.api` -- missing `Team` and `ManagedBy`.
- `aws_lambda_function.processor` -- has `tags: null` (the classic Terraform omission).
- `aws_iam_role.lambda_role` -- has no tags, but it is an IAM role not in the taggable resources list, so it should pass.

---

## The Policy

Create `policy.rego`:

```rego
package terraform.tags

import rego.v1

# ============================================================
# Helpers
# ============================================================

# Resources that will be created or updated
# (no point validating tags on something being deleted)
actionable_resources contains rc if {
	some rc in input.resource_changes
	some action in rc.change.actions
	action in {"create", "update"}
}

# Determines if a resource type is "taggable"
# by checking the prefix list in the configuration.
is_taggable(resource_type) if {
	some prefix in data.config.taggable_resource_prefixes
	startswith(resource_type, prefix)
}

# Extracts tags from a resource, handling the null case.
# In Terraform, if you don't set tags, the JSON shows tags: null.
# We convert it to an empty object so we don't have
# to check for null in every rule.
get_tags(rc) := tags if {
	tags := rc.change.after.tags
	tags != null
} else := {}

# ============================================================
# Violations
# ============================================================

# Missing required tag
violations contains msg if {
	some rc in actionable_resources
	is_taggable(rc.type)

	some tag_name, _ in data.config.required_tags
	tags := get_tags(rc)
	not tags[tag_name]

	msg := sprintf(
		"Resource '%s': missing required tag '%s'.",
		[rc.address, tag_name],
	)
}

# Tag with disallowed value
violations contains msg if {
	some rc in actionable_resources
	is_taggable(rc.type)

	some tag_name, tag_config in data.config.required_tags
	tags := get_tags(rc)
	tag_value := tags[tag_name]

	not tag_value in tag_config.allowed_values

	msg := sprintf(
		"Resource '%s': tag '%s' has value '%s', but only %v are allowed.",
		[rc.address, tag_name, tag_value, tag_config.allowed_values],
	)
}

# ============================================================
# Deny (wrapper over violations)
# ============================================================

deny contains msg if {
	some msg in violations
}

# ============================================================
# Report
# ============================================================

report := {
	"total_resources": count(actionable_resources),
	"taggable_resources": count(taggable_checked),
	"violations_count": count(violations),
	"compliant": count(violations) == 0,
}

taggable_checked contains rc if {
	some rc in actionable_resources
	is_taggable(rc.type)
}
```

There are a few interesting things here. First, `get_tags` handles the `null` case. In Terraform, when a resource has no `tags` block, the JSON shows `"tags": null`. If you try to access `null.Environment`, Rego fails silently (the rule does not match). With `get_tags` you convert null to `{}`, and then `not tags[tag_name]` correctly detects the missing tag.

Second, `violations` is separated from `deny`. This lets you query violations without the deny wrapper, or add additional logic (like exceptions) before converting them to deny.

---

## Testing the Policy

Time to evaluate. Remember that `config.json` is passed with `-d` (as data), not with `-i`:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.tags.deny" --format pretty
```

Expected output:

```json
[
  "Resource 'aws_instance.web': missing required tag 'ManagedBy'.",
  "Resource 'aws_instance.web': tag 'Environment' has value 'production', but only [\"dev\", \"staging\", \"prod\"] are allowed.",
  "Resource 'aws_lambda_function.processor': missing required tag 'Environment'.",
  "Resource 'aws_lambda_function.processor': missing required tag 'ManagedBy'.",
  "Resource 'aws_lambda_function.processor': missing required tag 'Team'.",
  "Resource 'aws_security_group.api': missing required tag 'ManagedBy'.",
  "Resource 'aws_security_group.api': missing required tag 'Team'."
]
```

Seven violations. The S3 bucket and RDS pass cleanly. The IAM role does not appear because it is not in the taggable resources list -- exactly what you want.

Now check the summary report:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.tags.report" --format pretty
```

Expected output:

```json
{
  "compliant": false,
  "taggable_resources": 5,
  "total_resources": 6,
  "violations_count": 7
}
```

Six resources total, five are taggable (the IAM role is not), and there are 7 violations among the ones that fail.

You can also verify that a specific resource is compliant. For example, the S3 bucket:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json \
  "{msg | some msg in data.terraform.tags.violations; contains(msg, \"aws_s3_bucket\")}" \
  --format pretty
```

Expected output: `[]` -- no violations for the S3 bucket.

---

## A Common Mistake: Forgetting the Null Case

What happens if you skip the `get_tags` helper and access `rc.change.after.tags` directly? Try it mentally with the Lambda function, which has `tags: null`. The expression `null[tag_name]` is undefined in Rego, so the rule silently skips that resource. The Lambda would pass the policy even though it has zero tags. That is why the `get_tags` helper that converts `null` to `{}` is essential.

---

## Verify What You Learned

**Command 1** -- Count how many taggable resources are in the plan:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "count(data.terraform.tags.taggable_checked)" --format pretty
```

Expected output: `5`

**Command 2** -- Get only the violations for the security group:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json \
  "{msg | some msg in data.terraform.tags.violations; contains(msg, \"aws_security_group\")}" \
  --format pretty
```

Expected output:

```json
[
  "Resource 'aws_security_group.api': missing required tag 'ManagedBy'.",
  "Resource 'aws_security_group.api': missing required tag 'Team'."
]
```

**Command 3** -- Verify that the plan is NOT compliant:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.tags.report.compliant" --format pretty
```

Expected output: `false`

**Command 4** -- Count how many violations are for missing tags (contain "missing"):

```bash
opa eval -i tfplan.json -d policy.rego -d config.json \
  "count({msg | some msg in data.terraform.tags.violations; contains(msg, \"missing\")})" --format pretty
```

Expected output: `6`

---

## What's Next

You have a reusable, data-driven tag enforcement policy. In the next exercise you will move from organizational hygiene to security: preventing dangerous configurations like public S3 buckets and open security groups.

## Reference

- [Terraform JSON Output Format](https://developer.hashicorp.com/terraform/internals/json-format) -- plan JSON structure reference.
- [OPA Policy Reference: Strings](https://www.openpolicyagent.org/docs/latest/policy-reference/#strings) -- `startswith`, `contains`, and other string builtins.
- The `get_tags` trick with `else := {}` is essential. In Terraform, `tags: null` is different from `tags: {}`, and without this helper your rules silently ignore resources without tags.
- `data.config` is how you access JSON files loaded with `-d`. OPA mounts them under `data` using the filename structure.
- Separating `violations` from `deny` gives you flexibility to filter, count, or add exceptions before deciding whether the plan is rejected.

## Additional Resources

- [AWS Tagging Best Practices](https://docs.aws.amazon.com/whitepapers/latest/tagging-best-practices/tagging-best-practices.html) -- AWS guidelines for tagging strategies.
- [Conftest](https://www.conftest.dev/) -- tool for running OPA policies against structured data in CI/CD.
- [Gatekeeper Library](https://open-policy-agent.github.io/gatekeeper-library/website/) -- examples of reusable policy patterns.
