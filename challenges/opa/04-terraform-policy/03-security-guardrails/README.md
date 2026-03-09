# Security Guardrails: Preventing Dangerous Configurations

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 04-02 (tag-enforcement)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `net.cidr_contains` and `numbers.range` to analyze network rules
- Handle the difference between `null` and `false` in Terraform plan JSON for encryption fields
- Implement a tag-based exception mechanism for approved policy bypasses
- Separate deny (blocking) from warnings (informational) in a single policy

## Why This Matters

There are infrastructure mistakes that cost money, and there are mistakes that cost **headlines**. A security group open to the world on port 22, an S3 bucket without encryption, an RDS database storing data in plaintext -- these are the configurations that turn an ordinary Tuesday into a week of incidents.

The beauty of evaluating policies against the Terraform plan is that you can catch these things **before** they reach production. It is shift-left security: you detect the problem in the plan, not when the resource is already exposed.

---

## The Three Pillars

### 1. Open Security Groups

The golden rule: never open a security group to `0.0.0.0/0` on sensitive ports. For HTTP (80/443) it may make sense for a public load balancer, but for SSH (22), databases (3306, 5432), or Redis (6379), it is a disaster waiting to happen.

OPA has a perfect builtin for this: `net.cidr_contains`. It tells you whether one CIDR contains another:

```
net.cidr_contains("0.0.0.0/0", "10.0.1.5/32")    -> true   (0.0.0.0/0 contains everything)
net.cidr_contains("10.0.0.0/8", "10.0.1.5/32")    -> true   (private network)
net.cidr_contains("10.0.0.0/8", "203.0.113.5/32") -> false  (public IP, outside the range)
```

### 2. Encryption

S3, EBS, RDS -- everything should be encrypted. In Terraform, the absence of encryption configuration often shows up as `null` in the JSON, which is different from `false`. Both mean "not encrypted," but Rego treats them differently:

```
null == false    -> in Rego this does NOT match (undefined != false)
x := null; x    -> x is null, but `if x` does NOT hold (null is falsy)
```

This is a classic trap. You will handle it explicitly.

### 3. Exceptions

In the real world, you sometimes need to break the rules. Maybe there is an S3 bucket that intentionally has no encryption because it only stores public assets. Instead of disabling the entire policy, you use a tag-based exception mechanism: if the resource has the tag `exception:approved`, it is excluded from checks.

---

## The Data

Create `tfplan.json`:

```json
{
  "format_version": "1.2",
  "terraform_version": "1.7.0",
  "resource_changes": [
    {
      "address": "aws_security_group.bad_ssh",
      "type": "aws_security_group",
      "name": "bad_ssh",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "name": "allow-ssh-world",
          "description": "SSH open to the world - BAD",
          "ingress": [
            {
              "from_port": 22,
              "to_port": 22,
              "protocol": "tcp",
              "cidr_blocks": ["0.0.0.0/0"]
            }
          ],
          "tags": {
            "Environment": "prod",
            "Team": "backend"
          }
        }
      }
    },
    {
      "address": "aws_security_group.good_ssh",
      "type": "aws_security_group",
      "name": "good_ssh",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "name": "allow-ssh-vpn",
          "description": "SSH only from VPN",
          "ingress": [
            {
              "from_port": 22,
              "to_port": 22,
              "protocol": "tcp",
              "cidr_blocks": ["10.0.0.0/8"]
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
      "address": "aws_security_group.public_web",
      "type": "aws_security_group",
      "name": "public_web",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "name": "public-web",
          "description": "Public HTTP/HTTPS for ALB",
          "ingress": [
            {
              "from_port": 443,
              "to_port": 443,
              "protocol": "tcp",
              "cidr_blocks": ["0.0.0.0/0"]
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
      "address": "aws_s3_bucket.unencrypted",
      "type": "aws_s3_bucket",
      "name": "unencrypted",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "bucket": "myapp-logs-unencrypted",
          "server_side_encryption_configuration": null,
          "tags": {
            "Environment": "prod",
            "Team": "backend"
          }
        }
      }
    },
    {
      "address": "aws_s3_bucket.encrypted",
      "type": "aws_s3_bucket",
      "name": "encrypted",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "bucket": "myapp-data-encrypted",
          "server_side_encryption_configuration": {
            "rule": {
              "apply_server_side_encryption_by_default": {
                "sse_algorithm": "aws:kms"
              }
            }
          },
          "tags": {
            "Environment": "prod",
            "Team": "data"
          }
        }
      }
    },
    {
      "address": "aws_s3_bucket.excepted",
      "type": "aws_s3_bucket",
      "name": "excepted",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "bucket": "myapp-public-assets",
          "server_side_encryption_configuration": null,
          "tags": {
            "Environment": "prod",
            "Team": "frontend",
            "exception": "approved"
          }
        }
      }
    },
    {
      "address": "aws_db_instance.unencrypted",
      "type": "aws_db_instance",
      "name": "unencrypted",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "identifier": "app-db-main",
          "engine": "postgres",
          "instance_class": "db.t3.medium",
          "storage_encrypted": false,
          "tags": {
            "Environment": "prod",
            "Team": "data"
          }
        }
      }
    },
    {
      "address": "aws_db_instance.encrypted",
      "type": "aws_db_instance",
      "name": "encrypted",
      "change": {
        "actions": ["create"],
        "before": null,
        "after": {
          "identifier": "app-db-analytics",
          "engine": "postgres",
          "instance_class": "db.r5.large",
          "storage_encrypted": true,
          "tags": {
            "Environment": "prod",
            "Team": "data"
          }
        }
      }
    }
  ]
}
```

You have a good variety: good and bad security groups, encrypted and unencrypted buckets, encrypted and unencrypted databases, and a bucket with an approved exception.

---

## The Policy

Create `policy.rego`:

```rego
package terraform.security

import rego.v1

# ============================================================
# Helpers
# ============================================================

# Resources being created or updated (ignore deletes and no-ops)
actionable_resources contains rc if {
	some rc in input.resource_changes
	some action in rc.change.actions
	action in {"create", "update"}
}

# Extracts tags handling null
get_tags(rc) := tags if {
	tags := rc.change.after.tags
	tags != null
} else := {}

# A resource has an approved exception if it has the tag exception:approved
has_exception(rc) if {
	tags := get_tags(rc)
	tags.exception == "approved"
}

# Sensitive ports that should never be open to 0.0.0.0/0
sensitive_ports := {22, 3306, 5432, 6379, 27017, 9200}

# ============================================================
# Deny: Security Groups open to the world
# ============================================================

deny contains msg if {
	some rc in actionable_resources
	rc.type == "aws_security_group"
	not has_exception(rc)

	some rule in rc.change.after.ingress
	some cidr in rule.cidr_blocks

	# Check if the CIDR is "the entire world"
	net.cidr_contains("0.0.0.0/0", cidr)
	cidr == "0.0.0.0/0"

	# And if any port in the range is sensitive
	some port in numbers.range(rule.from_port, rule.to_port)
	port in sensitive_ports

	msg := sprintf(
		"Security group '%s' opens port %d to 0.0.0.0/0. This is a critical security risk.",
		[rc.address, port],
	)
}

# ============================================================
# Deny: S3 without encryption
# ============================================================

deny contains msg if {
	some rc in actionable_resources
	rc.type == "aws_s3_bucket"
	not has_exception(rc)

	# In Terraform, if you don't configure encryption, this field is null
	not rc.change.after.server_side_encryption_configuration

	msg := sprintf(
		"S3 bucket '%s' does not have server-side encryption configured. All buckets must use encryption.",
		[rc.address],
	)
}

# ============================================================
# Deny: RDS without encryption
# ============================================================

deny contains msg if {
	some rc in actionable_resources
	rc.type == "aws_db_instance"
	not has_exception(rc)

	# storage_encrypted can be false or null
	not rc.change.after.storage_encrypted

	msg := sprintf(
		"RDS database '%s' does not have storage encryption enabled. All databases must be encrypted.",
		[rc.address],
	)
}

# ============================================================
# Warnings: things that don't block but deserve attention
# ============================================================

warnings contains msg if {
	some rc in actionable_resources
	rc.type == "aws_security_group"

	some rule in rc.change.after.ingress
	some cidr in rule.cidr_blocks
	cidr == "0.0.0.0/0"

	# If the port is NOT sensitive but is open to the world,
	# it's a warning (e.g., HTTP 80 open may be intentional)
	every port in numbers.range(rule.from_port, rule.to_port) {
		not port in sensitive_ports
	}

	msg := sprintf(
		"Security group '%s' opens ports %d-%d to 0.0.0.0/0. Verify this is intentional (e.g., public ALB).",
		[rc.address, rule.from_port, rule.to_port],
	)
}

# Resources that were excepted
warnings contains msg if {
	some rc in actionable_resources
	has_exception(rc)

	msg := sprintf(
		"Resource '%s' has an approved exception. Exceptions should be reviewed periodically.",
		[rc.address],
	)
}

# ============================================================
# Report
# ============================================================

report := {
	"denied": deny,
	"warnings": warnings,
	"pass": count(deny) == 0,
}
```

A few points worth examining.

For security groups, `numbers.range(rule.from_port, rule.to_port)` generates every port in the range. If a rule says `from_port: 0, to_port: 65535` (all ports), that would include 22, 3306, and so on. Comparing `from_port` directly is not enough -- you need to check whether any port in the range is sensitive.

For S3, `not rc.change.after.server_side_encryption_configuration` covers both `null` and total absence of the field. In Rego, `not X` is true when `X` is undefined or falsy. Since `null` is falsy, this works for both cases.

For the exception, you simply check for a tag. In production you would probably want something more robust (a field in an external system, an expiration date), but the pattern is the same.

---

## Testing

Evaluate the violations:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.security.deny" --format pretty
```

Expected output:

```json
[
  "RDS database 'aws_db_instance.unencrypted' does not have storage encryption enabled. All databases must be encrypted.",
  "S3 bucket 'aws_s3_bucket.unencrypted' does not have server-side encryption configured. All buckets must use encryption.",
  "Security group 'aws_security_group.bad_ssh' opens port 22 to 0.0.0.0/0. This is a critical security risk."
]
```

Three violations. Notice that the `excepted` bucket does not appear even though it has no encryption -- the exception works. And the `public_web` security group does not appear in deny because ports 80 and 443 are not in `sensitive_ports`.

Now check the warnings:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.security.warnings" --format pretty
```

Expected output:

```json
[
  "Resource 'aws_s3_bucket.excepted' has an approved exception. Exceptions should be reviewed periodically.",
  "Security group 'aws_security_group.public_web' opens ports 80-80 to 0.0.0.0/0. Verify this is intentional (e.g., public ALB).",
  "Security group 'aws_security_group.public_web' opens ports 443-443 to 0.0.0.0/0. Verify this is intentional (e.g., public ALB)."
]
```

Warnings do not block the plan but appear in the report. The excepted bucket generates a reminder, and the public ports on the web security group generate a notice for someone to confirm.

And the full report:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.security.report.pass" --format pretty
```

Expected output: `false` -- the plan does not pass because deny rules fired.

---

## Verify What You Learned

**Command 1** -- Count how many critical violations (deny) there are:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.security.deny)" --format pretty
```

Expected output: `3`

**Command 2** -- Verify that the good security group (good_ssh) does not appear in any violation:

```bash
opa eval -i tfplan.json -d policy.rego \
  "{msg | some msg in data.terraform.security.deny; contains(msg, \"good_ssh\")}" \
  --format pretty
```

Expected output: `[]`

**Command 3** -- Count how many warnings there are:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.security.warnings)" --format pretty
```

Expected output: `3`

**Command 4** -- Verify that the exception works by searching for the excepted bucket in deny:

```bash
opa eval -i tfplan.json -d policy.rego \
  "{msg | some msg in data.terraform.security.deny; contains(msg, \"excepted\")}" \
  --format pretty
```

Expected output: `[]` -- it does not appear in deny because it has the approved exception.

---

## What's Next

You have security guardrails that block dangerous configurations and a warning system for things that deserve attention but are not necessarily wrong. In the next exercise you will tackle cost controls -- making sure people are not spinning up oversized instances in the wrong environment.

## Reference

- [OPA Policy Reference: Net](https://www.openpolicyagent.org/docs/latest/policy-reference/#net) -- `net.cidr_contains` and other network builtins.
- [OPA Policy Reference: Numbers](https://www.openpolicyagent.org/docs/latest/policy-reference/#numbers) -- `numbers.range` for generating integer sequences.
- In Terraform JSON, the absence of encryption typically shows up as `null`. `not null` in Rego is `true`, so `not rc.change.after.storage_encrypted` covers both `false` and `null`.
- The tag-based exception pattern is simple and transparent -- the tag is visible in the Terraform code and in the AWS console.

## Additional Resources

- [AWS Security Group Best Practices](https://docs.aws.amazon.com/vpc/latest/userguide/security-group-rules.html) -- AWS guidance on security group rules.
- [S3 Encryption Documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-encryption.html) -- how S3 server-side encryption works.
- [CIS AWS Foundations Benchmark](https://www.cisecurity.org/benchmark/amazon_web_services) -- industry-standard security benchmarks for AWS.
