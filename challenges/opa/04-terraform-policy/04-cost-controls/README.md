# Cost Controls: Instance Type Guardrails

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 04-03 (security-guardrails)

## Learning Objectives

After completing this exercise, you will be able to:

- Read Terraform variables from the plan JSON to determine the target environment
- Build environment-aware allowlists that restrict resource sizes per environment
- Generate structured summaries that show compliance at a glance

## Why This Matters

You do not need a $50,000-per-year FinOps platform to control cloud costs. Sometimes a simple question is enough: "Does someone in dev really need an m5.4xlarge?" The answer, 99% of the time, is no.

The trick is simple: instance types are a good proxy for cost. A `t3.micro` costs pennies; an `r5.4xlarge` costs dollars per hour. If you can control which instance types are used in each environment, you have already cut a major source of unnecessary spending.

As in the previous exercises, you will do this **data-driven**. The allowed instance types come from a configuration file, not hardcoded in the policy. That way the FinOps team can update the limits without touching Rego.

---

## The Per-Environment Strategy

The idea is to have a different allowlist per environment:

```
+-------------+------------------------------------+
| Environment | Allowed instance types             |
+-------------+------------------------------------+
| dev         | t3.micro, t3.small                 |
| staging     | t3.medium, t3.large                |
| prod        | m5.large, m5.xlarge, r5.large      |
+-------------+------------------------------------+
```

In dev you only want cheap instances -- if someone needs to test something heavy, they can do it in staging. In staging you open it up a bit. And in prod you allow the larger instances that you actually need to serve traffic.

This applies to both EC2 and RDS, because both use the concept of instance types (though RDS calls them `instance_class`).

---

## The Data

Create `config.json` with the instance type configuration per environment:

```json
{
  "allowed_instance_types": {
    "dev": ["t3.micro", "t3.small"],
    "staging": ["t3.medium", "t3.large"],
    "prod": ["m5.large", "m5.xlarge", "r5.large"]
  },
  "allowed_rds_instance_classes": {
    "dev": ["db.t3.micro", "db.t3.small"],
    "staging": ["db.t3.medium", "db.t3.large"],
    "prod": ["db.r5.large", "db.r5.xlarge", "db.m5.large"]
  }
}
```

EC2 and RDS are separated because RDS uses the `db.` prefix and the available families differ. You could combine them, but keeping them separate is clearer.

Now the Terraform plan. You will simulate a **dev** environment where someone tries to use oversized instances.

Create `tfplan.json`:

```json
{
  "format_version": "1.2",
  "terraform_version": "1.7.0",
  "variables": {
    "environment": {
      "value": "dev"
    }
  },
  "resource_changes": [
    {
      "address": "aws_instance.api",
      "type": "aws_instance",
      "name": "api",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "ami": "ami-0c55b159cbfafe1f0",
          "instance_type": "t3.micro",
          "tags": {
            "Environment": "dev",
            "Team": "backend",
            "Name": "api-server"
          }
        }
      }
    },
    {
      "address": "aws_instance.worker",
      "type": "aws_instance",
      "name": "worker",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "ami": "ami-0c55b159cbfafe1f0",
          "instance_type": "m5.xlarge",
          "tags": {
            "Environment": "dev",
            "Team": "backend",
            "Name": "worker-node"
          }
        }
      }
    },
    {
      "address": "aws_instance.cache",
      "type": "aws_instance",
      "name": "cache",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "ami": "ami-0c55b159cbfafe1f0",
          "instance_type": "r5.2xlarge",
          "tags": {
            "Environment": "dev",
            "Team": "platform",
            "Name": "redis-cache"
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
          "identifier": "app-db-dev",
          "engine": "postgres",
          "engine_version": "15.4",
          "instance_class": "db.t3.micro",
          "allocated_storage": 20,
          "storage_encrypted": true,
          "tags": {
            "Environment": "dev",
            "Team": "data"
          }
        }
      }
    },
    {
      "address": "aws_db_instance.analytics",
      "type": "aws_db_instance",
      "name": "analytics",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "identifier": "analytics-db-dev",
          "engine": "postgres",
          "engine_version": "15.4",
          "instance_class": "db.r5.xlarge",
          "allocated_storage": 500,
          "storage_encrypted": true,
          "tags": {
            "Environment": "dev",
            "Team": "data"
          }
        }
      }
    }
  ]
}
```

Five resources in a dev environment: a small EC2 instance (correct), a worker with `m5.xlarge` (too large for dev), a cache with `r5.2xlarge` (absurdly large for dev), a small RDS database (correct), and another RDS with `db.r5.xlarge` (too large for dev).

Notice that the environment comes from `input.variables.environment.value`. This is how Terraform stores variables in the plan JSON -- inside a `variables` object where each variable has a `value` property.

---

## The Policy

Create `policy.rego`:

```rego
package terraform.costs

import rego.v1

# ============================================================
# Helpers
# ============================================================

# The environment comes from the Terraform plan variables
environment := input.variables.environment.value

# Resources being created or updated
actionable_resources contains rc if {
	some rc in input.resource_changes
	some action in rc.change.actions
	action in {"create", "update"}
}

# Allowed EC2 instance types for this environment
allowed_ec2_types := data.config.allowed_instance_types[environment]

# Allowed RDS instance classes for this environment
allowed_rds_classes := data.config.allowed_rds_instance_classes[environment]

# ============================================================
# Deny: EC2 instance types
# ============================================================

deny contains msg if {
	some rc in actionable_resources
	rc.type == "aws_instance"

	instance_type := rc.change.after.instance_type
	not instance_type in allowed_ec2_types

	msg := sprintf(
		"EC2 instance '%s' uses instance type '%s', which is not allowed in the '%s' environment. Allowed types: %v.",
		[rc.address, instance_type, environment, allowed_ec2_types],
	)
}

# ============================================================
# Deny: RDS instance classes
# ============================================================

deny contains msg if {
	some rc in actionable_resources
	rc.type == "aws_db_instance"

	instance_class := rc.change.after.instance_class
	not instance_class in allowed_rds_classes

	msg := sprintf(
		"RDS database '%s' uses instance class '%s', which is not allowed in the '%s' environment. Allowed classes: %v.",
		[rc.address, instance_class, environment, allowed_rds_classes],
	)
}

# ============================================================
# Info: summary of what will be created
# ============================================================

cost_summary := {
	"environment": environment,
	"ec2_instances": ec2_summary,
	"rds_instances": rds_summary,
	"violations": count(deny),
}

ec2_summary contains info if {
	some rc in actionable_resources
	rc.type == "aws_instance"
	info := {
		"address": rc.address,
		"instance_type": rc.change.after.instance_type,
		"allowed": rc.change.after.instance_type in allowed_ec2_types,
	}
}

rds_summary contains info if {
	some rc in actionable_resources
	rc.type == "aws_db_instance"
	info := {
		"address": rc.address,
		"instance_class": rc.change.after.instance_class,
		"allowed": rc.change.after.instance_class in allowed_rds_classes,
	}
}
```

The policy is fairly straightforward. The interesting part is how you get the environment: `input.variables.environment.value`. This comes from the Terraform plan, not from the configuration. The configuration (`data.config`) tells you what is allowed in each environment, and the input (`input`) tells you which environment you are in.

Notice that the `cost_summary` also shows each instance with an `allowed` field. This is useful for reports -- you can see at a glance what is fine and what is not without reading the deny messages.

---

## Testing

Check the violations in the dev environment:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.costs.deny" --format pretty
```

Expected output:

```json
[
  "EC2 instance 'aws_instance.cache' uses instance type 'r5.2xlarge', which is not allowed in the 'dev' environment. Allowed types: [\"t3.micro\", \"t3.small\"].",
  "EC2 instance 'aws_instance.worker' uses instance type 'm5.xlarge', which is not allowed in the 'dev' environment. Allowed types: [\"t3.micro\", \"t3.small\"].",
  "RDS database 'aws_db_instance.analytics' uses instance class 'db.r5.xlarge', which is not allowed in the 'dev' environment. Allowed classes: [\"db.t3.micro\", \"db.t3.small\"]."
]
```

Three violations: the two oversized EC2 instances and the oversized RDS. The `api` instance with `t3.micro` and the `main` database with `db.t3.micro` pass without issue.

Now check the cost summary:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.costs.cost_summary" --format pretty
```

Expected output:

```json
{
  "ec2_instances": [
    {"address": "aws_instance.api", "allowed": true, "instance_type": "t3.micro"},
    {"address": "aws_instance.cache", "allowed": false, "instance_type": "r5.2xlarge"},
    {"address": "aws_instance.worker", "allowed": false, "instance_type": "m5.xlarge"}
  ],
  "environment": "dev",
  "rds_instances": [
    {"address": "aws_db_instance.analytics", "allowed": false, "instance_class": "db.r5.xlarge"},
    {"address": "aws_db_instance.main", "allowed": true, "instance_class": "db.t3.micro"}
  ],
  "violations": 3
}
```

At a glance you can see that `api` and `main` are fine, and the rest are not. This kind of summary is perfect for a PR comment or a Slack notification.

What would happen if the environment were `prod`? You can check which types are allowed in prod:

```bash
opa eval -d config.json "data.config.allowed_instance_types.prod" --format pretty
```

Expected output:

```json
[
  "m5.large",
  "m5.xlarge",
  "r5.large"
]
```

In prod, the worker's `m5.xlarge` would be allowed, but the cache's `r5.2xlarge` would still not be on the list.

---

## Verify What You Learned

**Command 1** -- Verify that the detected environment is "dev":

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "data.terraform.costs.environment" --format pretty
```

Expected output: `"dev"`

**Command 2** -- Count how many violations there are:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json "count(data.terraform.costs.deny)" --format pretty
```

Expected output: `3`

**Command 3** -- Verify that the `api` instance with `t3.micro` is allowed in dev:

```bash
opa eval -i tfplan.json -d policy.rego -d config.json \
  "{info.address | some info in data.terraform.costs.cost_summary.ec2_instances; info.allowed == true}" \
  --format pretty
```

Expected output: `["aws_instance.api"]`

---

## What's Next

You have environment-aware cost controls that prevent oversized resources from landing in the wrong place. In the next exercise you will tackle a supply-chain concern: restricting where Terraform modules can come from.

## Reference

- [Terraform Plan JSON: Variables](https://developer.hashicorp.com/terraform/internals/json-format#values-representation) -- how `input.variables` stores Terraform variables with their final values.
- [OPA Policy Reference: Membership](https://www.openpolicyagent.org/docs/latest/policy-reference/#membership-and-iteration) -- `in` is the idiomatic way in Rego v1 to check membership in a set or array.
- The data-driven pattern works perfectly here: change `config.json` to adjust the limits without touching the policy.
- In production, you could extend this with estimated costs using the output of tools like Infracost, which generates a JSON with per-resource cost estimates.

## Additional Resources

- [Infracost](https://www.infracost.io/) -- generates cost estimates from Terraform plans, can be combined with OPA policies.
- [AWS Instance Types](https://aws.amazon.com/ec2/instance-types/) -- reference for EC2 instance families and sizes.
- [AWS RDS Instance Classes](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.DBInstanceClass.html) -- reference for RDS instance families.
