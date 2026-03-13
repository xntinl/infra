# 71. GuardDuty Threat Detection

<!--
difficulty: intermediate
concepts: [guardduty, threat-detection, vpc-flow-logs, cloudtrail-analysis, dns-logs, finding-types, severity-levels, eventbridge-integration, multi-account-guardduty, suppression-rules]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [65-iam-policies-identity-resource-scp]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** GuardDuty pricing is based on volume of data analyzed. The free trial covers 30 days. After the trial, costs are based on CloudTrail events ($4.00/million events), VPC Flow Logs ($1.00/GB), and DNS logs ($1.00/million queries). For this exercise with minimal activity, costs are negligible (~$0.01/hr). Remember to disable GuardDuty when finished if you do not want ongoing charges.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 65 (IAM policies) | Understanding of IAM and security concepts |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** GuardDuty with EventBridge integration for automated finding alerts using Terraform.
2. **Analyze** GuardDuty finding types: reconnaissance, instance compromise, account compromise, and their severity levels.
3. **Apply** EventBridge rules to route high-severity GuardDuty findings to SNS for real-time alerting.
4. **Evaluate** the data sources GuardDuty analyzes: VPC Flow Logs, CloudTrail management events, CloudTrail S3 data events, DNS logs, EKS audit logs.
5. **Design** a multi-region GuardDuty deployment strategy with centralized findings aggregation.

---

## Why This Matters

GuardDuty appears on the SAA-C03 exam as the primary threat detection service. The critical architectural insight is that GuardDuty analyzes data sources you already have (VPC Flow Logs, CloudTrail, DNS) using machine learning to identify threats -- it does not require you to configure or enable those data sources separately. GuardDuty creates its own internal copies of these logs at no additional cost for the log delivery.

The exam tests two patterns: (1) "How do you detect compromised EC2 instances communicating with known command-and-control servers?" -- GuardDuty analyzes VPC Flow Logs and DNS queries to identify this automatically; (2) "How do you alert the security team when a high-severity finding is detected?" -- EventBridge rule that matches GuardDuty findings with severity >= 7 and sends to SNS. The key design consideration is that GuardDuty is regional -- you must enable it in every region you want to monitor, and findings stay in the region where they are generated unless you configure multi-account aggregation.

---

## Building Blocks

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.region
}
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
  default     = "saa-ex71"
}

variable "alert_email" {
  type        = string
  description = "Email address for GuardDuty finding alerts"
  default     = "security@example.com"
}
```

### `main.tf`

```hcl
# ============================================================
# TODO 1: Enable GuardDuty
# ============================================================
# enable = true, finding_publishing_frequency = "FIFTEEN_MINUTES"
# Automatically analyzes VPC Flow Logs, CloudTrail, DNS logs.
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/guardduty_detector
# ============================================================


# ============================================================
# TODO 2: Create SNS Topic for Alerts
# ============================================================
# SNS topic + email subscription for finding notifications.
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic
# ============================================================


# ============================================================
# TODO 3: EventBridge Rule for High-Severity Findings
# ============================================================
# Match GuardDuty findings with severity >= 7, send to SNS.
# Severity: LOW (1-3.9), MEDIUM (4-6.9), HIGH (7-8.9), CRITICAL (9+)
# Docs: https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_findings-severity.html
# ============================================================


# ============================================================
# TODO 4: Generate Sample Findings (CLI)
# ============================================================
# aws guardduty create-sample-findings --detector-id "$DETECTOR_ID"
# Then list findings and verify EventBridge → SNS pipeline.
# Docs: https://docs.aws.amazon.com/guardduty/latest/ug/sample_findings.html
# ============================================================
```

### `outputs.tf`

```hcl
output "detector_id" {
  value = "Set after TODO 1 implementation"
}

output "sns_topic_arn" {
  value = "Set after TODO 2 implementation"
}
```

---

## GuardDuty Finding Categories

| Category | Example Finding Types | What It Detects |
|---|---|---|
| **Reconnaissance** | Recon:EC2/PortProbeUnprotectedPort | Port scanning, API enumeration |
| **Instance compromise** | Backdoor:EC2/C&CActivity.B | C&C communication, crypto mining, malware |
| **Account compromise** | UnauthorizedAccess:IAMUser/ConsoleLoginSuccess.B | Anomalous API calls, credential exfiltration |
| **S3 compromise** | Policy:S3/BucketBlockPublicAccessDisabled | Public access changes, suspicious data access |
| **Kubernetes** | CredentialAccess:Kubernetes/MaliciousIPCaller | Suspicious K8s API calls, container escapes |
| **Malware** | Execution:EC2/MaliciousFile | Known malware on EBS volumes |

---

## Spot the Bug

A security team enabled GuardDuty in their primary region (us-east-1) and configured EventBridge alerting. Six months later, they discover that an EC2 instance in eu-west-1 had been crypto-mining for weeks without any GuardDuty alert:

```hcl
# Enabled only in us-east-1
resource "aws_guardduty_detector" "this" {
  provider = aws.us_east_1
  enable   = true
}

resource "aws_cloudwatch_event_rule" "guardduty_alert" {
  provider = aws.us_east_1
  name     = "guardduty-high-severity"

  event_pattern = jsonencode({
    source      = ["aws.guardduty"]
    detail-type = ["GuardDuty Finding"]
    detail      = { severity = [{ numeric = [">=", 7] }] }
  })
}
```

<details>
<summary>Explain the bug</summary>

**GuardDuty is a regional service -- it must be enabled in every region you want to monitor.** Enabling GuardDuty in us-east-1 only monitors resources in us-east-1. The EC2 instance in eu-west-1 had no GuardDuty monitoring, so the crypto-mining activity was never detected.

This is a common architecture gap: teams enable GuardDuty in their "primary" region and assume it covers all regions. It does not. Each region independently generates and stores findings.

**Fix:** Enable GuardDuty in all regions, including regions where you do not currently have resources (attackers can launch resources in any region):

```hcl
# Define all regions
locals {
  all_regions = [
    "us-east-1", "us-east-2", "us-west-1", "us-west-2",
    "eu-west-1", "eu-west-2", "eu-west-3", "eu-central-1",
    "ap-southeast-1", "ap-southeast-2", "ap-northeast-1"
    # ... all enabled regions
  ]
}

# Create a provider for each region
# (In practice, use a module with provider aliases or AWS Organizations
# delegated administrator with auto-enable)

# Best approach: Use AWS Organizations GuardDuty delegation
# 1. Designate a security account as delegated administrator
# 2. Enable "Auto-enable GuardDuty for new accounts" in the admin
# 3. GuardDuty automatically enables in all regions for all accounts
```

Additionally, aggregate findings to a central security account using GuardDuty multi-account management:

```
Security Account (delegated administrator)
  |-- Receives findings from ALL member accounts
  |-- Receives findings from ALL regions
  |-- Central EventBridge rules for alerting
  |-- Central dashboard for investigation
```

Combine with SCPs to prevent anyone from disabling GuardDuty:

```json
{
  "Effect": "Deny",
  "Action": [
    "guardduty:DeleteDetector",
    "guardduty:DisassociateFromMasterAccount",
    "guardduty:UpdateDetector"
  ],
  "Resource": "*"
}
```

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify GuardDuty is enabled:**
   ```bash
   DETECTOR_ID=$(aws guardduty list-detectors --query 'DetectorIds[0]' --output text)
   aws guardduty get-detector --detector-id "$DETECTOR_ID" \
     --query '{Status:Status,FindingPublishingFrequency:FindingPublishingFrequency,DataSources:DataSources}' \
     --output json
   ```
   Expected: Status = `ENABLED`.

3. **Generate sample findings:**
   ```bash
   aws guardduty create-sample-findings --detector-id "$DETECTOR_ID"
   ```
   Expected: No output (success).

4. **List high-severity findings:**
   ```bash
   aws guardduty list-findings --detector-id "$DETECTOR_ID" \
     --finding-criteria '{"Criterion":{"severity":{"Gte":7}}}' \
     --query 'FindingIds[:5]' --output json
   ```
   Expected: List of finding IDs.

5. **Get finding details:**
   ```bash
   FINDING_ID=$(aws guardduty list-findings --detector-id "$DETECTOR_ID" \
     --finding-criteria '{"Criterion":{"severity":{"Gte":7}}}' \
     --query 'FindingIds[0]' --output text)

   aws guardduty get-findings --detector-id "$DETECTOR_ID" \
     --finding-ids "$FINDING_ID" \
     --query 'Findings[0].{Type:Type,Severity:Severity,Title:Title,Description:Description}' \
     --output json
   ```
   Expected: Finding with severity >= 7.

6. **Verify EventBridge rule:**
   ```bash
   aws events describe-rule \
     --name saa-ex71-guardduty-high-severity \
     --query '{State:State,EventPattern:EventPattern}' \
     --output json
   ```
   Expected: State = `ENABLED`.

7. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Enable GuardDuty (main.tf)</summary>

```hcl
resource "aws_guardduty_detector" "this" {
  enable                       = true
  finding_publishing_frequency = "FIFTEEN_MINUTES"

  datasources {
    s3_logs {
      enable = true
    }
    malware_protection {
      scan_ec2_instance_with_findings {
        ebs_volumes {
          enable = true
        }
      }
    }
  }

  tags = { Name = "${var.project_name}-detector" }
}

```

Update `outputs.tf`:

```hcl
output "detector_id" {
  value = aws_guardduty_detector.this.id
}
```

</details>

<details>
<summary>TODO 2 -- SNS Topic (main.tf)</summary>

```hcl
resource "aws_sns_topic" "guardduty_alerts" {
  name = "${var.project_name}-guardduty-alerts"

  tags = { Name = "${var.project_name}-guardduty-alerts" }
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.guardduty_alerts.arn
  protocol  = "email"
  endpoint  = var.alert_email
}

```

Update `outputs.tf`:

```hcl
output "sns_topic_arn" {
  value = aws_sns_topic.guardduty_alerts.arn
}
```

</details>

<details>
<summary>TODO 3 -- EventBridge Rule (main.tf)</summary>

```hcl
resource "aws_cloudwatch_event_rule" "guardduty_high" {
  name        = "${var.project_name}-guardduty-high-severity"
  description = "Route high-severity GuardDuty findings to SNS"

  event_pattern = jsonencode({
    source      = ["aws.guardduty"]
    detail-type = ["GuardDuty Finding"]
    detail = {
      severity = [{ numeric = [">=", 7] }]
    }
  })

  tags = { Name = "${var.project_name}-guardduty-rule" }
}

resource "aws_cloudwatch_event_target" "sns" {
  rule = aws_cloudwatch_event_rule.guardduty_high.name
  arn  = aws_sns_topic.guardduty_alerts.arn
}

resource "aws_sns_topic_policy" "guardduty" {
  arn = aws_sns_topic.guardduty_alerts.arn

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowEventBridgePublish"
        Effect    = "Allow"
        Principal = { Service = "events.amazonaws.com" }
        Action    = "sns:Publish"
        Resource  = aws_sns_topic.guardduty_alerts.arn
      }
    ]
  })
}
```

The EventBridge rule matches GuardDuty findings with severity >= 7 (HIGH and CRITICAL). The SNS topic policy allows EventBridge to publish to the topic. Without this policy, EventBridge silently fails to deliver findings to SNS.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: Disabling GuardDuty deletes all existing findings. If you want to keep findings, export them first.

Verify:

```bash
aws guardduty list-detectors --query 'DetectorIds' --output json
```

Expected: Empty list `[]`.

---

## What's Next

Exercise 72 covers **AWS Shield Standard vs Shield Advanced** for DDoS protection. You will understand the free L3/L4 protection that Shield Standard provides automatically and evaluate when Shield Advanced ($3,000/month) is justified for its L7 protection, DDoS Response Team access, and cost protection.

---

## Summary

- **GuardDuty** uses ML to analyze VPC Flow Logs, CloudTrail, and DNS logs for threats -- no agents, no log configuration required
- **Finding severity levels:** LOW (1-3.9, informational), MEDIUM (4-6.9, suspicious), HIGH (7-8.9, compromised), CRITICAL (9+, immediate action)
- **GuardDuty is regional** -- must be enabled in every region you want to monitor, including regions without current resources
- **EventBridge integration** enables automated response: route findings to SNS, Lambda, or Security Hub for alerting and remediation
- **Sample findings** allow testing the full alerting pipeline without actual threats
- **Multi-account management** with AWS Organizations delegated administrator centralizes findings in a security account
- **Data sources** include CloudTrail management events, VPC Flow Logs, DNS logs, S3 data events, EKS audit logs, and EBS malware scanning
- **30-day free trial** covers all data sources -- evaluate costs before committing to production
- **Suppression rules** reduce alert fatigue by filtering known-benign findings (e.g., internal scanners, expected traffic patterns)
- **SCPs should prevent disabling** GuardDuty in member accounts -- attackers who compromise an account will try to disable monitoring first

## Reference

- [GuardDuty User Guide](https://docs.aws.amazon.com/guardduty/latest/ug/what-is-guardduty.html)
- [GuardDuty Finding Types](https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_finding-types-active.html)
- [GuardDuty with EventBridge](https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_findings_cloudwatch.html)
- [Terraform aws_guardduty_detector](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/guardduty_detector)

## Additional Resources

- [GuardDuty Multi-Account Management](https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_accounts.html) -- centralized findings with AWS Organizations delegation
- [GuardDuty Remediation](https://docs.aws.amazon.com/guardduty/latest/ug/guardduty_remediate.html) -- automated remediation patterns with Lambda
- [Security Hub Integration](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-internal-providers.html#securityhub-internal-providers-guardduty) -- aggregate GuardDuty findings with other security services
- [GuardDuty Pricing Calculator](https://aws.amazon.com/guardduty/pricing/) -- estimate costs based on your CloudTrail, Flow Log, and DNS volume
