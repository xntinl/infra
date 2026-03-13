# Conftest + Terraform Plans

## Prerequisites

- OPA CLI installed (`opa version`)
- Conftest installed (`conftest --version`)
- Completed exercises 07-01 (Conftest + Dockerfiles) and 07-02 (Conftest + Kubernetes)
- Familiarity with Terraform plan output (helpful but not required)

## Learning Objectives

After completing this exercise, you will be able to:

- Validate a Terraform plan JSON against Rego policies using Conftest
- Write helper functions that filter `resource_changes` by type and action
- Build a three-step CI pipeline pattern: `terraform plan`, export to JSON, `conftest test`
- Distinguish between hard policy violations (`deny`) and soft advisories (`warn`) for infrastructure changes

## Why This Matters

You already know how to write Terraform policies with OPA (section 04). The question now is: how do you integrate that into a real pipeline? The answer is a three-step flow:

```
terraform plan -out=tfplan.binary
    |
terraform show -json tfplan.binary > tfplan.json
    |
conftest test tfplan.json
```

Terraform generates a binary plan, you convert it to JSON with `show -json`, and Conftest evaluates it against your policies. The JSON plan has a specific structure -- the important data lives in `resource_changes`, an array where each element describes a resource that will change. Each change has `type` (the resource kind), `change.after` (the state after apply), and `change.actions` (create, update, delete).

The advantage of Conftest over raw `opa eval` is the CI integration: automatic exit codes, readable output, and the `policy/` convention that standardizes where rules live. But under the hood it is the same OPA engine, so all the Rego policies you have already written work with minimal adjustments (mainly changing the package to `main`).

## Practice

We are going to simulate a Terraform plan JSON. In a real pipeline, `terraform show -json` would generate this, but for practice we create one by hand.

Create `tfplan.json`:

```json
{
  "resource_changes": [
    {
      "address": "aws_s3_bucket.data",
      "type": "aws_s3_bucket",
      "change": {
        "actions": ["create"],
        "after": {
          "bucket": "my-data-bucket",
          "tags": null
        }
      }
    },
    {
      "address": "aws_s3_bucket_versioning.data",
      "type": "aws_s3_bucket_versioning",
      "change": {
        "actions": ["create"],
        "after": {
          "bucket": "my-data-bucket",
          "versioning_configuration": [
            {
              "status": "Disabled"
            }
          ]
        }
      }
    },
    {
      "address": "aws_instance.web",
      "type": "aws_instance",
      "change": {
        "actions": ["create"],
        "after": {
          "ami": "ami-12345678",
          "instance_type": "t3.2xlarge",
          "monitoring": false,
          "tags": {
            "Name": "web-server"
          }
        }
      }
    },
    {
      "address": "aws_security_group_rule.allow_all",
      "type": "aws_security_group_rule",
      "change": {
        "actions": ["create"],
        "after": {
          "type": "ingress",
          "cidr_blocks": ["0.0.0.0/0"],
          "from_port": 0,
          "to_port": 65535,
          "protocol": "tcp"
        }
      }
    }
  ]
}
```

This plan has four resources, and each one violates at least one rule: an S3 bucket with no tags, versioning disabled, an oversized EC2 instance with monitoring off, and a security group open to the entire internet.

Now the policy.

Create `policy/policy.rego`:

```rego
package main

import rego.v1

# Helper: resources that will be created or updated
resources_changed := [rc |
    some rc in input.resource_changes
    rc.change.actions[_] in {"create", "update"}
]

# Helper: filter by resource type
resources_of_type(t) := [rc |
    some rc in resources_changed
    rc.type == t
]

# --- DENY: S3 buckets must have tags ---
deny contains msg if {
    some rc in resources_of_type("aws_s3_bucket")
    not rc.change.after.tags
    msg := sprintf("S3 bucket '%s' does not have tags -- all resources must be tagged", [rc.address])
}

# --- DENY: S3 versioning must be enabled ---
deny contains msg if {
    some rc in resources_of_type("aws_s3_bucket_versioning")
    some vc in rc.change.after.versioning_configuration
    vc.status != "Enabled"
    msg := sprintf("S3 versioning on '%s' is not enabled -- must be 'Enabled'", [rc.address])
}

# --- DENY: no security groups open to the world ---
deny contains msg if {
    some rc in resources_of_type("aws_security_group_rule")
    rc.change.after.type == "ingress"
    some cidr in rc.change.after.cidr_blocks
    cidr == "0.0.0.0/0"
    msg := sprintf("Security group '%s' allows ingress from 0.0.0.0/0 -- restrict the CIDRs", [rc.address])
}

# --- DENY: EC2 instances should not be too large ---
deny contains msg if {
    some rc in resources_of_type("aws_instance")
    instance_type := rc.change.after.instance_type
    contains(instance_type, "2xlarge")
    msg := sprintf("EC2 '%s' uses instance type '%s' -- approval required for XL instances", [rc.address, instance_type])
}

# --- WARN: EC2 instances must have monitoring enabled ---
warn contains msg if {
    some rc in resources_of_type("aws_instance")
    rc.change.after.monitoring == false
    msg := sprintf("EC2 '%s' does not have detailed monitoring enabled", [rc.address])
}
```

Two helper functions at the top keep the deny/warn rules clean. `resources_changed` filters to only resources being created or updated (we do not care about deletes for these rules). `resources_of_type` further filters by resource type. This pattern scales well -- adding a new resource type check only requires a new deny/warn rule, not new filtering logic.

Now run Conftest. In a real CI pipeline, this would be the final step after `terraform plan` and `terraform show -json`:

```bash
# In a real pipeline:
# terraform plan -out=tfplan.binary
# terraform show -json tfplan.binary > tfplan.json
# conftest test tfplan.json

# For this exercise, we already have tfplan.json:
conftest test tfplan.json
```

Expected output:

```
FAIL - tfplan.json - main - S3 bucket 'aws_s3_bucket.data' does not have tags -- all resources must be tagged
FAIL - tfplan.json - main - S3 versioning on 'aws_s3_bucket_versioning.data' is not enabled -- must be 'Enabled'
FAIL - tfplan.json - main - Security group 'aws_security_group_rule.allow_all' allows ingress from 0.0.0.0/0 -- restrict the CIDRs
FAIL - tfplan.json - main - EC2 'aws_instance.web' uses instance type 't3.2xlarge' -- approval required for XL instances
WARN - tfplan.json - main - EC2 'aws_instance.web' does not have detailed monitoring enabled

5 tests, 0 passed, 1 warnings, 4 failures
```

Four hard failures and one warning. Conftest returns exit code 1, which blocks the pipeline.

### Intermediate Verification

Let's confirm the same results with `opa eval`:

```bash
opa eval --format pretty -i tfplan.json -d policy/ "data.main.deny"
```

You should see the same four deny messages as a set.

### Example CI Integration

A GitHub Actions step for this pattern would look like:

```yaml
- name: Validate Terraform plan
  run: |
    terraform plan -out=tfplan.binary
    terraform show -json tfplan.binary > tfplan.json
    conftest test tfplan.json --policy policy/
```

If Conftest returns exit code 1 (at least one `deny` fires), the step fails and the PR stays blocked. Clean and automatic.

## Verify What You Learned

**1.** Confirm there are 4 failures in the plan:

```bash
conftest test tfplan.json 2>&1 | grep "FAIL" | wc -l
```

Expected output:

```
4
```

**2.** Use `opa eval` directly to count the violations:

```bash
opa eval --format pretty -i tfplan.json -d policy/ "count(data.main.deny)"
```

Expected output:

```
4
```

**3.** Verify that the security group rule specifically catches the 0.0.0.0/0 violation:

```bash
opa eval --format pretty -i tfplan.json -d policy/ "data.main.deny" | grep "0.0.0.0/0"
```

Expected output (the line containing the security group message):

```
  "Security group 'aws_security_group_rule.allow_all' allows ingress from 0.0.0.0/0 -- restrict the CIDRs"
```

## Section 07 Summary

Across these three exercises, you integrated OPA policies into CI/CD workflows using Conftest:

1. **Dockerfiles** (exercise 01): caught `:latest` tags, missing `USER` instructions, `ADD` misuse, missing `HEALTHCHECK`, and excessive `RUN` layers
2. **Kubernetes manifests** (exercise 02): validated image pinning, resource limits, security contexts, and health probes -- then fixed the manifest and confirmed a clean pass
3. **Terraform plans** (exercise 03): enforced tagging, S3 versioning, security group restrictions, instance size limits, and monitoring -- using helper functions for clean, scalable policy code

The pattern across all three is identical: parse a configuration file into structured data, evaluate Rego rules against it, and use exit codes to gate the pipeline. Conftest handles the parsing and exit code conventions; you focus on writing the rules. Whether you are validating a Dockerfile, a Kubernetes Deployment, or a Terraform plan, the workflow is the same: `conftest test <file>`.

## What's Next

You now have a complete toolkit: OPA for policy logic, Rego for expressing rules, and Conftest for CI/CD integration. From here, you can extend these patterns to additional configuration formats (Helm charts, CloudFormation templates, general JSON/YAML configs), build policy libraries shared across teams, or integrate OPA as an admission controller directly in your Kubernetes cluster with Gatekeeper.

## Reference

- [Conftest -- testing Terraform](https://www.conftest.dev/examples/#terraform)
- [Terraform plan JSON format](https://developer.hashicorp.com/terraform/internals/json-format)
- [Conftest in GitHub Actions](https://www.conftest.dev/usage/#docker)

## Additional Resources

- [OPA Gatekeeper](https://open-policy-agent.github.io/gatekeeper/) -- OPA as a Kubernetes admission controller for runtime enforcement
- [Hashicorp Sentinel](https://www.hashicorp.com/sentinel) -- Terraform's native policy framework, for comparison with the OPA/Conftest approach
- [Conftest policy library examples](https://github.com/open-policy-agent/conftest/tree/master/examples) -- community-maintained example policies for various formats
