# Terraform Plan JSON: Anatomy of a Plan

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed section 03 (Rego fundamentals)
- Basic familiarity with Terraform concepts (resources, plans, apply)

## Learning Objectives

After completing this exercise, you will be able to:

- Understand the structure of a Terraform plan in JSON format
- Build reusable Rego helpers that group and filter resources by type and action
- Write deny rules that catch dangerous operations before they reach production

## Why This Matters

So far you have evaluated policies against JSON files you crafted by hand. That works for learning, but in real life your input comes from a tool. And the flagship tool of the infrastructure world is **Terraform**.

When you run `terraform plan`, Terraform calculates what changes it needs to make so your real infrastructure matches your code. Normally you see that plan in the terminal with colors and `+`/`-` signs. But there is a version that is far more useful for policy evaluation: the JSON version.

```
terraform plan -out=tfplan
terraform show -json tfplan > tfplan.json
```

That `tfplan.json` is the perfect input for OPA. It contains everything about what will happen to your infrastructure **before** it happens. You can validate security groups, encryption, tags, instance types, and any configuration **before the apply** -- shift-left for infrastructure.

---

## The Structure of the Plan JSON

A Terraform plan JSON file can be large, but for policy purposes you mainly care about two sections:

```
+-----------------------------------------------------+
|                   tfplan.json                        |
+-----------------------------------------------------+
|                                                      |
|  planned_values ------> Desired final state          |
|  (how everything would look if the plan is applied)  |
|                                                      |
|  resource_changes ----> List of changes              |
|  (what will be created, modified, deleted)            |
|                                                      |
|  configuration -------> Parsed source code           |
|  (modules, providers, variables from .tf)            |
|                                                      |
+-----------------------------------------------------+
```

The section you will use the most is `resource_changes`. It is an array where each element represents a resource that Terraform will touch. Each element looks like this:

```
+--------------------------------------------+
|         resource_changes[i]                 |
+--------------------------------------------+
|  address: "aws_s3_bucket.logs"              |
|  type:    "aws_s3_bucket"                   |
|  name:    "logs"                            |
|  change:                                    |
|    actions: ["create"]                      |
|    before:  null  (did not exist)            |
|    after:   { bucket: "my-logs", ... }      |
+--------------------------------------------+
```

The possible `actions` are:

| Action                  | Meaning                          |
|-------------------------|----------------------------------|
| `"create"`              | New resource                     |
| `"delete"`              | Will be destroyed                |
| `"update"`              | Modified in-place                |
| `"no-op"`               | No changes                       |
| `["delete","create"]`   | Replace (destroy and recreate)   |

Notice that `actions` is an **array**. A simple create is `["create"]`, but a replace is `["delete", "create"]`. This matters when you write your rules.

The difference between `planned_values` and `resource_changes` is subtle but important: `planned_values` tells you what everything would look like at the end (a snapshot of the future state), while `resource_changes` tells you what **actions** will be taken. For policies, you almost always want `resource_changes` because you care about what is changing, not just the final state.

---

## Hands On

You will work with a sample plan containing 4 resources: an S3 bucket, an EC2 instance, a security group, and an RDS database. Some are being created, one is being modified, and one is being deleted.

Create `tfplan.json`:

```json
{
  "format_version": "1.2",
  "terraform_version": "1.7.0",
  "planned_values": {
    "root_module": {
      "resources": [
        {
          "address": "aws_s3_bucket.app_data",
          "type": "aws_s3_bucket",
          "name": "app_data",
          "values": {
            "bucket": "myapp-data-prod",
            "tags": {
              "Environment": "prod",
              "Team": "platform"
            }
          }
        }
      ]
    }
  },
  "resource_changes": [
    {
      "address": "aws_s3_bucket.app_data",
      "type": "aws_s3_bucket",
      "name": "app_data",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "bucket": "myapp-data-prod",
          "force_destroy": false,
          "tags": {
            "Environment": "prod",
            "Team": "platform"
          }
        }
      }
    },
    {
      "address": "aws_instance.web",
      "type": "aws_instance",
      "name": "web",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "ami": "ami-0c55b159cbfafe1f0",
          "instance_type": "t3.large",
          "tags": {
            "Environment": "prod",
            "Team": "backend",
            "Name": "web-server"
          }
        }
      }
    },
    {
      "address": "aws_security_group.web_sg",
      "type": "aws_security_group",
      "name": "web_sg",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["update"],
        "before": {
          "name": "web-sg",
          "description": "Web security group",
          "ingress": [
            {
              "from_port": 443,
              "to_port": 443,
              "protocol": "tcp",
              "cidr_blocks": ["10.0.0.0/8"]
            }
          ]
        },
        "after": {
          "name": "web-sg",
          "description": "Web security group",
          "ingress": [
            {
              "from_port": 443,
              "to_port": 443,
              "protocol": "tcp",
              "cidr_blocks": ["10.0.0.0/8"]
            },
            {
              "from_port": 80,
              "to_port": 80,
              "protocol": "tcp",
              "cidr_blocks": ["0.0.0.0/0"]
            }
          ],
          "tags": {
            "Environment": "prod",
            "Team": "platform"
          }
        }
      }
    },
    {
      "address": "aws_db_instance.legacy",
      "type": "aws_db_instance",
      "name": "legacy",
      "provider_name": "registry.terraform.io/hashicorp/aws",
      "change": {
        "actions": ["delete"],
        "before": {
          "identifier": "legacy-db",
          "engine": "mysql",
          "instance_class": "db.t3.medium",
          "storage_encrypted": false,
          "tags": {
            "Environment": "prod",
            "Team": "data"
          }
        },
        "after": null
      }
    }
  ]
}
```

There are four resources with different actions: the bucket and the instance are being **created**, the security group is being **updated** (an ingress rule is added), and the database is being **deleted**. This is a realistic plan -- the kind of thing you see in an ordinary pull request.

Now for the policy. The idea is to build **reusable helpers** that you can use in any Terraform policy. You define them once and import them into every policy that needs to analyze a plan.

Create `policy.rego`:

```rego
package terraform.plan

import rego.v1

# ============================================================
# Reusable helpers
# ============================================================

# Groups resources by type.
# resources_by_type["aws_s3_bucket"] returns all
# resource_changes of type S3.
resources_by_type[resource_type] contains rc if {
	some rc in input.resource_changes
	resource_type := rc.type
}

# Filters resources by action.
# Internally, actions is an array. A "create" is ["create"],
# a replace is ["delete","create"]. We use `some` to search
# within the array.
resources_by_action[action] contains rc if {
	some rc in input.resource_changes
	some action in rc.change.actions
}

# Shortcuts for the most common actions
created_resources contains rc if {
	some rc in resources_by_action["create"]
}

deleted_resources contains rc if {
	some rc in resources_by_action["delete"]
}

updated_resources contains rc if {
	some rc in resources_by_action["update"]
}

# ============================================================
# Summary: a quick overview of the plan
# ============================================================

summary := {
	"total": count(input.resource_changes),
	"creates": count(created_resources),
	"updates": count(updated_resources),
	"deletes": count(deleted_resources),
	"resource_types": resource_types,
}

resource_types contains rc.type if {
	some rc in input.resource_changes
}

# ============================================================
# Deny rules
# ============================================================

# Do not allow deleting databases without review
deny contains msg if {
	some rc in deleted_resources
	rc.type == "aws_db_instance"
	msg := sprintf(
		"DANGER: database '%s' is about to be deleted. This requires manual approval.",
		[rc.address],
	)
}

# Do not allow creating more than 5 resources in a single plan
# (to avoid giant plans that nobody reviews)
deny contains msg if {
	count(created_resources) > 5
	msg := sprintf(
		"The plan creates %d resources. Maximum allowed: 5. Split the change into smaller parts.",
		[count(created_resources)],
	)
}

# Alert if resources are being deleted
deny contains msg if {
	count(deleted_resources) > 0
	some rc in deleted_resources
	rc.type != "aws_db_instance" # already covered above with a more specific message
	msg := sprintf(
		"Resource '%s' (type: %s) is about to be deleted. Verify this is intentional.",
		[rc.address, rc.type],
	)
}
```

Now test each helper individually so you understand how it works.

First, the summary -- the most useful helper for getting a quick overview of the plan:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.plan.summary" --format pretty
```

Expected output:

```json
{
  "creates": 2,
  "deletes": 1,
  "resource_types": [
    "aws_db_instance",
    "aws_instance",
    "aws_s3_bucket",
    "aws_security_group"
  ],
  "total": 4,
  "updates": 1
}
```

Now look at resources grouped by type. This is extremely useful when you want to apply different policies to each type:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.plan.resources_by_type[\"aws_s3_bucket\"]" --format pretty
```

It returns a set with all resource_changes of type S3:

```json
[
  {
    "address": "aws_s3_bucket.app_data",
    "change": {
      "actions": [
        "create"
      ],
      "after": {
        "bucket": "myapp-data-prod",
        "force_destroy": false,
        "tags": {
          "Environment": "prod",
          "Team": "platform"
        }
      },
      "before": null
    },
    "name": "app_data",
    "provider_name": "registry.terraform.io/hashicorp/aws",
    "type": "aws_s3_bucket"
  }
]
```

Check the resources that will be created:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.plan.created_resources" --format pretty
```

This returns the S3 bucket and the EC2 instance -- the two with `"actions": ["create"]`.

And the most important part, the violations:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.plan.deny" --format pretty
```

Expected output:

```json
[
  "DANGER: database 'aws_db_instance.legacy' is about to be deleted. This requires manual approval."
]
```

Only one violation: the database about to be deleted. The plan creates 2 resources (under the limit of 5), so that rule does not fire. And there are no deleted resources of types other than `aws_db_instance`, so the third rule does not fire either.

---

## Verify What You Learned

**Command 1** -- Count how many distinct resource types are in the plan:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.plan.resource_types)" --format pretty
```

Expected output: `4`

**Command 2** -- Get the addresses of all resources being deleted:

```bash
opa eval -i tfplan.json -d policy.rego "{rc.address | some rc in data.terraform.plan.deleted_resources}" --format pretty
```

Expected output: `["aws_db_instance.legacy"]`

**Command 3** -- Verify there are exactly 2 new resources:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.plan.created_resources) == 2" --format pretty
```

Expected output: `true`

**Command 4** -- Query which resources are being updated:

```bash
opa eval -i tfplan.json -d policy.rego "{rc.address | some rc in data.terraform.plan.updated_resources}" --format pretty
```

Expected output: `["aws_security_group.web_sg"]`

**Command 5** -- Verify that the plan would be rejected (deny is not empty):

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.plan.deny) > 0" --format pretty
```

Expected output: `true`

---

## What's Next

You now have a foundation for reading Terraform plan JSON and building reusable helpers. In the next exercise you will use these patterns to enforce required tags on resources -- one of the most common and practical infrastructure policies.

## Reference

- [Terraform JSON Output Format](https://developer.hashicorp.com/terraform/internals/json-format) -- official documentation for the plan JSON structure.
- [OPA Terraform Tutorial](https://www.openpolicyagent.org/docs/latest/terraform/) -- official OPA guide for evaluating Terraform plans.
- `resource_changes[].change.actions` -- always an array. A replace is `["delete","create"]`, not a string.
- `change.before` is `null` for creates; `change.after` is `null` for deletes.
- `planned_values` shows the final state; `resource_changes` shows the transitions. For policies, use `resource_changes`.

## Additional Resources

- [Spacelift OPA Integration](https://docs.spacelift.io/concepts/policy/terraform-plan-policy) -- example of OPA plan policies in a CI/CD platform.
- [Conftest](https://www.conftest.dev/) -- a tool that wraps OPA for testing structured data like Terraform plans.
- [Terraform Plan JSON Specification](https://developer.hashicorp.com/terraform/internals/json-format#change-representation) -- detailed specification of the `change` object.
