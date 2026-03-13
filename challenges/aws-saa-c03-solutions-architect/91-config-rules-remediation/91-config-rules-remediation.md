# 91. AWS Config Rules and Remediation

<!--
difficulty: intermediate
concepts: [aws-config, config-recorder, managed-rules, custom-rules, auto-remediation, ssm-automation, compliance-evaluation, configuration-history, resource-timeline, aggregator]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [90-cloudtrail-api-auditing]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** AWS Config charges $0.003 per configuration item recorded and $0.001 per Config rule evaluation per region. For this exercise with a small number of resources and rules, costs are negligible (~$0.01/hr). SSM Automation executions are free for the first 100,000 steps/month. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 90 (CloudTrail) or equivalent knowledge of API auditing
- Understanding of IAM roles and S3 bucket policies
- Familiarity with SSM (Systems Manager) concepts

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** AWS Config with a configuration recorder, delivery channel, and managed rules using Terraform
2. **Analyze** the difference between Config's continuous recording and periodic evaluation of compliance rules
3. **Evaluate** which managed rules address specific compliance requirements (encryption, public access, multi-AZ)
4. **Apply** automatic remediation using SSM Automation documents to fix non-compliant resources
5. **Design** a Config-based compliance framework that detects drift and enforces standards across an AWS account

## Why This Matters

AWS Config answers the question "what is the current configuration of my resources, and does it comply with my policies?" while CloudTrail answers "who did what and when?" The SAA-C03 exam tests both, and a common mistake is confusing them. Config records the *state* of resources (is this S3 bucket public? does this EBS volume have encryption?), while CloudTrail records *actions* (who created this bucket? who changed this security group?).

The architectural power of Config lies in its rules engine and automatic remediation. Config continuously records resource configurations and evaluates them against rules. When a resource becomes non-compliant -- someone disables encryption on an S3 bucket, opens a security group to 0.0.0.0/0, or creates an RDS instance without Multi-AZ -- Config detects the violation within minutes. With auto-remediation, Config can automatically invoke an SSM Automation document to fix the violation: re-enable encryption, remove the insecure security group rule, or enable Multi-AZ.

The exam tests scenarios where Config rules catch configuration drift that CloudTrail alone cannot detect. CloudTrail shows that someone called `ModifyDBInstance`, but Config tells you that the resulting configuration is non-compliant with your organization's encryption policy. The combination of detection (Config) and enforcement (auto-remediation) is a core SAA-C03 architecture pattern.

## Step 1 -- Create Config Infrastructure

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" { region = var.region }
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "saa-ex91"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
resource "random_id" "suffix" { byte_length = 4 }

resource "aws_s3_bucket" "config" {
  bucket = "${var.project_name}-config-${random_id.suffix.hex}", force_destroy = true
  tags = { Name = "${var.project_name}-config" }
}

resource "aws_s3_bucket_public_access_block" "config" {
  bucket = aws_s3_bucket.config.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

data "aws_iam_policy_document" "config_bucket" {
  statement {
    effect     = "Allow"
    principals { type = "Service", identifiers = ["config.amazonaws.com"] }
    actions    = ["s3:GetBucketAcl"]
    resources  = [aws_s3_bucket.config.arn]
  }
  statement {
    effect     = "Allow"
    principals { type = "Service", identifiers = ["config.amazonaws.com"] }
    actions    = ["s3:PutObject"]
    resources  = ["${aws_s3_bucket.config.arn}/AWSLogs/${data.aws_caller_identity.current.account_id}/Config/*"]
    condition  { test = "StringEquals", variable = "s3:x-amz-acl", values = ["bucket-owner-full-control"] }
  }
}

resource "aws_s3_bucket_policy" "config" {
  bucket = aws_s3_bucket.config.id
  policy = data.aws_iam_policy_document.config_bucket.json
}
```

## Step 2 -- Create the Config Recorder and Delivery Channel

### `iam.tf`

```hcl
data "aws_iam_policy_document" "config_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["config.amazonaws.com"] }
  }
}

resource "aws_iam_role" "config" {
  name               = "${var.project_name}-config-role"
  assume_role_policy = data.aws_iam_policy_document.config_assume.json
}

resource "aws_iam_role_policy_attachment" "config" {
  role       = aws_iam_role.config.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWS_ConfigRole"
}

data "aws_iam_policy_document" "config_s3" {
  statement {
    actions   = ["s3:PutObject", "s3:GetBucketAcl"]
    resources = [aws_s3_bucket.config.arn, "${aws_s3_bucket.config.arn}/*"]
  }
}

resource "aws_iam_role_policy" "config_s3" {
  name = "s3-delivery", role = aws_iam_role.config.id
  policy = data.aws_iam_policy_document.config_s3.json
}
```

### `config.tf`

```hcl
resource "aws_config_configuration_recorder" "this" {
  name     = "${var.project_name}-recorder"
  role_arn = aws_iam_role.config.arn
  recording_group { all_supported = true, include_global_resource_types = true }
}

resource "aws_config_delivery_channel" "this" {
  name           = "${var.project_name}-channel"
  s3_bucket_name = aws_s3_bucket.config.id
  snapshot_delivery_properties { delivery_frequency = "Three_Hours" }
  depends_on = [aws_config_configuration_recorder.this]
}

# MUST explicitly enable -- defining the recorder alone does not start recording
resource "aws_config_configuration_recorder_status" "this" {
  name       = aws_config_configuration_recorder.this.name
  is_enabled = true
  depends_on = [aws_config_delivery_channel.this]
}
```

## Step 3 -- Add Config Rules

### TODO 1: Create Managed Config Rules

Add the following to `config.tf`:

```hcl
# Managed rules use owner="AWS" + predefined source_identifier
resource "aws_config_config_rule" "s3_public_read" {
  # TODO: name="${var.project_name}-s3-public-read-prohibited"
  # source: owner="AWS", source_identifier="S3_BUCKET_PUBLIC_READ_PROHIBITED"
  depends_on = [aws_config_configuration_recorder_status.this]
}

resource "aws_config_config_rule" "encrypted_volumes" {
  # TODO: name="${var.project_name}-encrypted-volumes"
  # source: owner="AWS", source_identifier="ENCRYPTED_VOLUMES"
  depends_on = [aws_config_configuration_recorder_status.this]
}

resource "aws_config_config_rule" "rds_multi_az" {
  # TODO: name="${var.project_name}-rds-multi-az"
  # source: owner="AWS", source_identifier="RDS_MULTI_AZ_SUPPORT"
  # scope: compliance_resource_types=["AWS::RDS::DBInstance"]
  depends_on = [aws_config_configuration_recorder_status.this]
}
```

### TODO 2: Add Auto-Remediation for S3 Public Access

```hcl
# When NON_COMPLIANT, invoke SSM Automation to block public access
resource "aws_config_remediation_configuration" "s3_public_access" {
  # TODO: config_rule_name=s3_public_read.name, target_type="SSM_DOCUMENT"
  # target_id="AWS-DisableS3BucketPublicReadWrite", automatic=true
  # maximum_automatic_attempts=3, retry_attempt_seconds=60
  # parameter { name="S3BucketName", resource_value="RESOURCE_ID" }
  # execution_controls { ssm_controls { concurrent_execution_rate_percentage=10, error_percentage=10 } }
}
```

<details>
<summary>config.tf -- Solution: Complete Config Rules and Remediation</summary>

```hcl
resource "aws_config_config_rule" "s3_public_read" {
  name = "${var.project_name}-s3-public-read-prohibited"

  source {
    owner             = "AWS"
    source_identifier = "S3_BUCKET_PUBLIC_READ_PROHIBITED"
  }

  depends_on = [aws_config_configuration_recorder_status.this]
}

resource "aws_config_config_rule" "encrypted_volumes" {
  name = "${var.project_name}-encrypted-volumes"

  source {
    owner             = "AWS"
    source_identifier = "ENCRYPTED_VOLUMES"
  }

  depends_on = [aws_config_configuration_recorder_status.this]
}

resource "aws_config_config_rule" "rds_multi_az" {
  name = "${var.project_name}-rds-multi-az"

  source {
    owner             = "AWS"
    source_identifier = "RDS_MULTI_AZ_SUPPORT"
  }

  scope {
    compliance_resource_types = ["AWS::RDS::DBInstance"]
  }

  depends_on = [aws_config_configuration_recorder_status.this]
}

resource "aws_config_remediation_configuration" "s3_public_access" {
  config_rule_name = aws_config_config_rule.s3_public_read.name
  target_type      = "SSM_DOCUMENT"
  target_id        = "AWS-DisableS3BucketPublicReadWrite"
  automatic        = true

  maximum_automatic_attempts = 3
  retry_attempt_seconds      = 60

  parameter {
    name           = "S3BucketName"
    resource_value = "RESOURCE_ID"
  }

  execution_controls {
    ssm_controls {
      concurrent_execution_rate_percentage = 10
      error_percentage                     = 10
    }
  }
}
```

</details>

## Step 4 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "config_bucket" {
  value = aws_s3_bucket.config.id
}

output "recorder_name" {
  value = aws_config_configuration_recorder.this.name
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 5 -- Evaluate Compliance

```bash
# Check compliance status for all rules
aws configservice describe-compliance-by-config-rule \
  --config-rule-names \
    "saa-ex91-s3-public-read-prohibited" \
    "saa-ex91-encrypted-volumes" \
    "saa-ex91-rds-multi-az" \
  --query 'ComplianceByConfigRules[*].{Rule:ConfigRuleName,Status:Compliance.ComplianceType}' \
  --output table

# Get detailed evaluation results for S3 public read rule
aws configservice get-compliance-details-by-config-rule \
  --config-rule-name "saa-ex91-s3-public-read-prohibited" \
  --compliance-types NON_COMPLIANT \
  --query 'EvaluationResults[*].{Resource:EvaluationResultIdentifier.EvaluationResultQualifier.ResourceId,Status:ComplianceType}' \
  --output table

# List all non-compliant resources across all rules
aws configservice get-compliance-summary-by-resource-type \
  --query 'ComplianceSummariesByResourceType[*].{Type:ResourceType,NonCompliant:ComplianceSummary.NonCompliantResourceCount.CappedCount}' \
  --output table
```

## Spot the Bug

A team sets up AWS Config with rules and remediation, but non-compliant resources are never fixed:

```hcl
resource "aws_config_configuration_recorder" "this" {
  name     = "company-recorder"
  role_arn = aws_iam_role.config.arn

  recording_group {
    all_supported                 = true
    include_global_resource_types = true
  }
}

resource "aws_config_delivery_channel" "this" {
  name           = "company-channel"
  s3_bucket_name = aws_s3_bucket.config.id
}

# Note: aws_config_configuration_recorder_status resource is MISSING

resource "aws_config_config_rule" "encrypted_volumes" {
  name = "encrypted-volumes"

  source {
    owner             = "AWS"
    source_identifier = "ENCRYPTED_VOLUMES"
  }

  depends_on = [aws_config_delivery_channel.this]
}

resource "aws_config_remediation_configuration" "encrypt_volumes" {
  config_rule_name = aws_config_config_rule.encrypted_volumes.name
  target_type      = "SSM_DOCUMENT"
  target_id        = "AWS-EnableEBSEncryptionByDefault"
  automatic        = true

  maximum_automatic_attempts = 5
  retry_attempt_seconds      = 60
}
```

The team notices:
1. Config rules are created but show "Evaluating..." indefinitely
2. Non-compliant EBS volumes are detected but never remediated
3. The SSM Automation document is never invoked

<details>
<summary>Explain the bug</summary>

**Bug 1: The configuration recorder is never started.**

The `aws_config_configuration_recorder` resource defines the recorder, but the `aws_config_configuration_recorder_status` resource (with `is_enabled = true`) is missing. Without explicitly enabling the recorder, Config is configured but not recording. Rules cannot evaluate resources that are not being recorded.

**Fix:** Add the recorder status resource:

```hcl
resource "aws_config_configuration_recorder_status" "this" {
  name       = aws_config_configuration_recorder.this.name
  is_enabled = true

  depends_on = [aws_config_delivery_channel.this]
}
```

**Bug 2: Wrong remediation document.** `AWS-EnableEBSEncryptionByDefault` only affects NEW volumes -- existing unencrypted volumes stay non-compliant. There is no single SSM document that can encrypt in-use volumes. Fix: use `AWS-EnableEBSEncryptionByDefault` to prevent future violations, and a custom SSM Automation for existing volumes (create encrypted snapshot, then new encrypted volume). This is a key Config limitation: auto-remediation works for configuration changes but is complex for resources requiring replacement.

</details>

## Verify What You Learned

```bash
RECORDER=$(terraform output -raw recorder_name)

# Verify recorder is enabled and recording
aws configservice describe-configuration-recorder-status \
  --configuration-recorder-names "$RECORDER" \
  --query 'ConfigurationRecordersStatus[0].{Recording:recording,LastStatus:lastStatus}' \
  --output table
```

Expected: `Recording = true`, `LastStatus = SUCCESS`.

```bash
# Verify Config rules exist
aws configservice describe-config-rules \
  --config-rule-names \
    "saa-ex91-s3-public-read-prohibited" \
    "saa-ex91-encrypted-volumes" \
    "saa-ex91-rds-multi-az" \
  --query 'ConfigRules[*].{Name:ConfigRuleName,State:ConfigRuleState}' \
  --output table
```

Expected: Three rules, all in `ACTIVE` state.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 92 covers **Systems Manager Session Manager and Patch Manager**, where you will configure SSM as a secure SSH replacement (no port 22, full audit trail) and set up automated patch baselines with maintenance windows -- building on the SSM Automation concept introduced in this exercise's remediation configuration.

## Summary

- **AWS Config** records resource configurations and evaluates them against rules for continuous compliance monitoring
- **Configuration Recorder** must be explicitly enabled (`aws_config_configuration_recorder_status`) -- defining it alone does not start recording
- **Managed rules** provide pre-built compliance checks (200+ rules) for common standards like encryption, public access, and multi-AZ
- **Custom rules** use Lambda functions for organization-specific compliance logic beyond what managed rules offer
- **Auto-remediation** invokes SSM Automation documents to automatically fix non-compliant resources
- **Config vs CloudTrail**: Config records resource *state* (is this bucket public?); CloudTrail records *actions* (who made it public?)
- **Evaluation types**: Change-triggered (evaluates when resource changes) and periodic (evaluates on a schedule)
- **Cost model**: $0.003 per configuration item recorded + $0.001 per rule evaluation -- costs scale with resource count and rule count
- **Remediation limitations**: Some non-compliance requires resource replacement (encrypted EBS volumes), not just configuration changes
- **Organization Config** aggregates compliance data across all accounts in an AWS Organization into a central view

## Reference

- [AWS Config Developer Guide](https://docs.aws.amazon.com/config/latest/developerguide/WhatIsConfig.html)
- [List of AWS Config Managed Rules](https://docs.aws.amazon.com/config/latest/developerguide/managed-rules-by-aws-config.html)
- [Terraform aws_config_config_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/config_config_rule)
- [Terraform aws_config_remediation_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/config_remediation_configuration)

## Additional Resources

- [Remediation with SSM Automation](https://docs.aws.amazon.com/config/latest/developerguide/remediation.html) -- setting up auto-remediation with SSM Automation documents
- [Config Aggregators](https://docs.aws.amazon.com/config/latest/developerguide/aggregate-data.html) -- aggregating compliance data from multiple accounts and regions
- [Conformance Packs](https://docs.aws.amazon.com/config/latest/developerguide/conformance-packs.html) -- pre-packaged collections of Config rules mapped to compliance frameworks (CIS, NIST, PCI DSS)
- [Config vs Security Hub](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-standards.html) -- Security Hub uses Config rules under the hood for its compliance standards
