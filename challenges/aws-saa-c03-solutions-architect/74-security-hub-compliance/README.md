# 74. Security Hub Compliance

<!--
difficulty: advanced
concepts: [security-hub, aggregated-findings, cis-benchmark, pci-dss, aws-foundational-best-practices, guardduty-integration, inspector-integration, macie-integration, config-rules, custom-actions, automated-remediation, eventbridge]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate, create
prerequisites: [71-guardduty-threat-detection, 72-shield-standard-vs-advanced, 73-waf-web-acl-rules]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** Security Hub charges per finding ingested ($0.00003/finding/month after free tier of 10,000 findings). Config rules charge per evaluation (~$0.001/evaluation). GuardDuty, Inspector, and Macie have separate charges if enabled. Total ~$0.02/hr for this exercise. Remember to run `terraform destroy` and disable services when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| AWS Config enabled in the account | `aws configservice describe-configuration-recorders` |
| Completed exercises 71-73 (security services) | Understanding of GuardDuty, Shield, WAF |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Evaluate** Security Hub as the central aggregation point for security findings across AWS services.
2. **Design** a compliance monitoring strategy using CIS, PCI DSS, and AWS Foundational Best Practices standards.
3. **Analyze** Security Hub findings to prioritize remediation based on severity and resource impact.
4. **Create** automated remediation workflows using Security Hub custom actions and EventBridge.
5. **Assess** the trade-offs between enabling all standards versus selectively enabling controls for cost and noise management.

---

## Why This Matters

Security Hub is the exam's answer to "how do you get a single view of your security posture?" Every individual security service -- GuardDuty (threat detection), Inspector (vulnerability scanning), Macie (data classification), Config (configuration compliance), Firewall Manager (firewall policy management) -- generates its own findings in its own console. Security Hub aggregates all of these into a single dashboard with a normalized finding format called AWS Security Finding Format (ASFF).

The SAA-C03 exam tests three key concepts. First, which services feed into Security Hub and what each detects -- GuardDuty finds threats, Inspector finds vulnerabilities, Macie finds sensitive data, Config finds misconfigurations. Second, compliance standards -- CIS AWS Foundations Benchmark provides security best practices, PCI DSS provides payment card industry requirements, AWS Foundational Security Best Practices is AWS's own curated set. Third, automated remediation -- Security Hub integrates with EventBridge to trigger Lambda functions that auto-fix findings (like making a public S3 bucket private).

The architectural trade-off is coverage versus noise. Enabling all standards generates thousands of findings, many of which may be irrelevant to your workload. The exam expects you to know that Security Hub exists as the aggregation layer, that it requires AWS Config to be enabled for most compliance checks, and that automated remediation is the pattern for operating at scale.

---

## The Challenge

You are the security architect for an organization that needs centralized visibility across all AWS security services. Design and implement a Security Hub deployment that enables compliance monitoring, aggregates findings from multiple sources, and provides automated remediation for critical findings.

### Requirements

1. Enable Security Hub with AWS Foundational Security Best Practices standard
2. Enable CIS AWS Foundations Benchmark
3. Configure Security Hub to receive findings from GuardDuty
4. Create a custom action for manual remediation triggers
5. Set up EventBridge rule for automated notification of critical findings

### Architecture

```
GuardDuty ─────┐
Inspector ──────┤
Macie ──────────┼──> Security Hub ──> EventBridge ──> SNS/Lambda
Config ─────────┤        │                              │
Firewall Mgr ──┘        │                              v
                   Compliance Standards          Auto-Remediation
                   - CIS Benchmark               - Block public S3
                   - PCI DSS                     - Revoke SG rules
                   - AWS Foundational             - Rotate credentials
```

---

## Hints

<details>
<summary>Hint 1: Enabling Security Hub</summary>

Security Hub is enabled per region. The Terraform resource:

```hcl
resource "aws_securityhub_account" "this" {
  enable_default_standards = false  # We'll enable standards explicitly
  auto_enable_controls     = true
}
```

Setting `enable_default_standards = false` gives you explicit control over which standards are enabled. If set to `true`, Security Hub enables CIS and AWS Foundational automatically.

Security Hub requires AWS Config to be enabled because most compliance checks rely on Config rules under the hood.

</details>

<details>
<summary>Hint 2: Enabling Compliance Standards</summary>

Each standard has a specific ARN pattern:

```hcl
resource "aws_securityhub_standards_subscription" "foundational" {
  depends_on    = [aws_securityhub_account.this]
  standards_arn = "arn:aws:securityhub:us-east-1::standards/aws-foundational-security-best-practices/v/1.0.0"
}

resource "aws_securityhub_standards_subscription" "cis" {
  depends_on    = [aws_securityhub_account.this]
  standards_arn = "arn:aws:securityhub:::ruleset/cis-aws-foundations-benchmark/v/1.4.0"
}
```

Note the different ARN formats: foundational includes the region, CIS does not. PCI DSS ARN: `arn:aws:securityhub:us-east-1::standards/pci-dss/v/3.2.1`.

</details>

<details>
<summary>Hint 3: GuardDuty Integration</summary>

GuardDuty automatically sends findings to Security Hub when both are enabled in the same account and region. No explicit integration resource is needed -- Security Hub discovers enabled GuardDuty detectors automatically.

To verify the integration:

```bash
aws securityhub list-enabled-products-for-import \
  --query 'ProductSubscriptions[*]'
```

If GuardDuty findings are not appearing, check that both services are enabled in the same region.

</details>

<details>
<summary>Hint 4: Custom Actions and EventBridge</summary>

Custom actions create buttons in the Security Hub console for manual remediation. When triggered, they emit an EventBridge event:

```hcl
resource "aws_securityhub_action_target" "remediate" {
  depends_on  = [aws_securityhub_account.this]
  name        = "RemediateNow"
  identifier  = "RemediateNow"
  description = "Send finding to remediation workflow"
}
```

The EventBridge event pattern to match:

```json
{
  "source": ["aws.securityhub"],
  "detail-type": ["Security Hub Findings - Custom Action"],
  "detail": {
    "actionName": ["RemediateNow"]
  }
}
```

For automated remediation without manual action, match on findings imported:

```json
{
  "source": ["aws.securityhub"],
  "detail-type": ["Security Hub Findings - Imported"],
  "detail": {
    "findings": {
      "Severity": { "Label": ["CRITICAL"] }
    }
  }
}
```

</details>

<details>
<summary>Hint 5: SNS Notification for Critical Findings</summary>

```hcl
resource "aws_sns_topic" "security_alerts" {
  name = "${var.project_name}-security-alerts"
}

resource "aws_cloudwatch_event_rule" "critical_findings" {
  name = "${var.project_name}-critical-findings"
  event_pattern = jsonencode({
    source      = ["aws.securityhub"]
    detail-type = ["Security Hub Findings - Imported"]
    detail = {
      findings = {
        Severity = { Label = ["CRITICAL", "HIGH"] }
      }
    }
  })
}

resource "aws_cloudwatch_event_target" "sns" {
  rule = aws_cloudwatch_event_rule.critical_findings.name
  arn  = aws_sns_topic.security_alerts.arn
}
```

You need an SNS topic policy allowing EventBridge to publish.

</details>

---

## Spot the Bug

The following Security Hub configuration deploys successfully but critical findings generate no alerts. Identify why before expanding the answer.

```hcl
resource "aws_securityhub_account" "this" {
  enable_default_standards = true
}

resource "aws_cloudwatch_event_rule" "critical" {
  name = "securityhub-critical"
  event_pattern = jsonencode({
    source      = ["aws.securityhub"]
    detail-type = ["Security Hub Findings - Imported"]
    detail = {
      findings = {
        Severity = { Label = ["CRITICAL"] }
      }
    }
  })
}

resource "aws_cloudwatch_event_target" "sns" {
  rule = aws_cloudwatch_event_rule.critical.name
  arn  = aws_sns_topic.alerts.arn
}

resource "aws_sns_topic" "alerts" {
  name = "security-alerts"
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.alerts.arn
  protocol  = "email"
  endpoint  = "security@example.com"
}
```

<details>
<summary>Explain the bug</summary>

**The SNS topic is missing a resource policy that allows EventBridge to publish to it.** By default, only the topic owner can publish to an SNS topic. EventBridge (the `events.amazonaws.com` service principal) needs explicit permission.

Without this policy, EventBridge receives the Security Hub finding event, matches the rule, attempts to publish to the SNS topic, and fails silently. The EventBridge rule shows "FailedInvocations" in its CloudWatch metrics, but no error appears in the Security Hub console.

**Fix:** Add an SNS topic policy:

```hcl
resource "aws_sns_topic_policy" "alerts" {
  arn = aws_sns_topic.alerts.arn
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowEventBridgePublish"
        Effect    = "Allow"
        Principal = { Service = "events.amazonaws.com" }
        Action    = "SNS:Publish"
        Resource  = aws_sns_topic.alerts.arn
      }
    ]
  })
}
```

**Additional issue:** The email subscription requires confirmation. The subscriber must click the confirmation link in the email sent by SNS. Until confirmed, the subscription status is `PendingConfirmation` and no notifications are delivered. This is not a Terraform issue -- it is an operational step that must be completed manually.

</details>

---

## Verify What You Learned

After implementing the Security Hub configuration, verify with these commands:

```bash
# Verify Security Hub is enabled
aws securityhub describe-hub \
  --query '{HubArn:HubArn,SubscribedAt:SubscribedAt}' \
  --output table
```

Expected: Hub ARN and subscription timestamp.

```bash
# Verify enabled standards
aws securityhub get-enabled-standards \
  --query 'StandardsSubscriptions[*].{Standard:StandardsArn,Status:StandardsStatus}' \
  --output table
```

Expected: AWS Foundational and CIS standards with status `READY`.

```bash
# View findings summary by severity
aws securityhub get-findings \
  --query 'Findings[*].{Title:Title,Severity:Severity.Label,Status:Workflow.Status}' \
  --max-items 10 \
  --output table
```

Expected: Findings with severity labels (CRITICAL, HIGH, MEDIUM, LOW, INFORMATIONAL).

```bash
# Verify EventBridge rule
aws events describe-rule --name saa-ex74-critical-findings \
  --query '{Name:Name,State:State}' --output table
```

Expected: Rule in `ENABLED` state.

```bash
# Verify custom action exists
aws securityhub describe-action-targets \
  --query 'ActionTargets[*].{Name:Name,Arn:ActionTargetArn}' \
  --output table
```

Expected: `RemediateNow` action target.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Security Hub must be explicitly disabled if not managed by Terraform:

```bash
# If needed, disable Security Hub manually
aws securityhub disable-security-hub
```

Verify:

```bash
aws securityhub describe-hub 2>&1 | grep -q "not subscribed" && echo "Security Hub disabled" || echo "Still enabled"
```

---

## What's Next

Exercise 75 covers **Amazon Inspector for vulnerability assessment**, where you will enable Inspector v2 for automated scanning of EC2 instances, ECR container images, and Lambda functions. Inspector generates findings that feed directly into Security Hub, so the aggregation you configured in this exercise will display vulnerability data alongside the compliance findings.

---

## Summary

- **Security Hub** aggregates findings from GuardDuty, Inspector, Macie, Config, Firewall Manager, and third-party tools into a single dashboard
- **AWS Security Finding Format (ASFF)** normalizes findings across all sources into a consistent schema
- **Compliance standards** provide automated checks: CIS (security best practices), PCI DSS (payment card), AWS Foundational (AWS-curated)
- **AWS Config is required** for most Security Hub compliance checks -- Config rules evaluate resource configurations under the hood
- **Custom actions** create console buttons that emit EventBridge events for manual remediation workflows
- **Automated remediation** uses EventBridge rules matching finding severity/type to trigger Lambda or SSM Automation for auto-fix
- **Multi-account aggregation** uses Security Hub administrator account to aggregate findings from member accounts via AWS Organizations
- **Cross-region aggregation** requires enabling Security Hub in each region and designating an aggregation region
- **Cost management**: each finding ingested costs $0.00003/month -- enabling all standards in many accounts generates high volumes; selectively disable irrelevant controls
- **EventBridge + SNS** requires an explicit SNS topic policy allowing the `events.amazonaws.com` service principal to publish

## Reference

- [AWS Security Hub User Guide](https://docs.aws.amazon.com/securityhub/latest/userguide/what-is-securityhub.html)
- [Security Hub Standards](https://docs.aws.amazon.com/securityhub/latest/userguide/standards-reference.html)
- [Terraform aws_securityhub_account](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/securityhub_account)
- [Security Hub Pricing](https://aws.amazon.com/security-hub/pricing/)

## Additional Resources

- [ASFF Syntax](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-findings-format-syntax.html) -- the complete finding format specification
- [Automated Remediation Examples](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-cloudwatch-events.html) -- EventBridge patterns for common remediation scenarios
- [Multi-Account Management](https://docs.aws.amazon.com/securityhub/latest/userguide/securityhub-accounts.html) -- administrator and member account setup via Organizations
- [Disabling Specific Controls](https://docs.aws.amazon.com/securityhub/latest/userguide/controls-findings-create-update.html) -- reducing noise by disabling irrelevant compliance checks
