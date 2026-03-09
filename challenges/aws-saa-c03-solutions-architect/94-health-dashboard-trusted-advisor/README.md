# 94. AWS Health Dashboard and Trusted Advisor

<!--
difficulty: intermediate
concepts: [aws-health, personal-health-dashboard, trusted-advisor, health-events, eventbridge-health, service-disruptions, five-categories, support-plan-tiers, proactive-notifications]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [93-eventbridge-operational-events]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates only EventBridge rules, SNS topics, and an S3 bucket. EventBridge rules are free. SNS within free tier. Total ~$0.01/hr. Trusted Advisor checks are read-only (no resources created). Note: full Trusted Advisor access requires Business or Enterprise Support plan ($100+/month). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 93 (EventBridge Operational Events) or equivalent knowledge
- Understanding of EventBridge rules and event patterns
- Awareness of AWS Support plan tiers (Basic, Developer, Business, Enterprise)

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** EventBridge rules that capture AWS Health events affecting your specific resources and services
2. **Analyze** the difference between AWS Health Dashboard (service-wide status) and Personal Health Dashboard (your resources)
3. **Evaluate** Trusted Advisor check categories and understand which checks are available on each Support plan tier
4. **Apply** proactive notification patterns that alert your team before service disruptions impact your workloads
5. **Design** an operational awareness strategy that combines Health events with Trusted Advisor recommendations

## Why This Matters

The SAA-C03 exam tests your understanding of proactive operational monitoring -- not just reacting to failures, but anticipating and preventing them. AWS Health Dashboard provides two views: the public Service Health Dashboard showing the status of all AWS services, and the Personal Health Dashboard (PHD) showing events that specifically affect your account's resources. The distinction matters: the public dashboard might show "EC2 operating normally" while your specific EC2 instances in us-east-1a are affected by a hardware degradation that only appears in your Personal Health Dashboard.

The exam also tests Trusted Advisor, which analyzes your account against best practices across five categories: cost optimization, performance, security, fault tolerance, and service limits. The critical exam trap is the Support plan requirement: on Basic and Developer plans, only 7 core checks are available (primarily security-related). The full 115+ checks require Business or Enterprise Support. When the exam asks "how do you identify underutilized EC2 instances for cost savings," Trusted Advisor is the answer -- but only if the customer has Business or Enterprise Support. Without it, you need CloudWatch metrics analysis or third-party tools.

Combining Health events with EventBridge enables automated responses to AWS infrastructure problems. When AWS schedules maintenance on an EC2 instance, a Health event fires days in advance. An EventBridge rule can trigger a Lambda function to proactively migrate the workload to a different instance, avoiding the maintenance window disruption entirely.

## Step 1 -- Create Health Event Notification Infrastructure

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
  default     = "saa-ex94"
}
```

### `main.tf`

```hcl
resource "aws_sns_topic" "health_alerts" {
  name = "${var.project_name}-health-alerts"
  tags = { Name = "${var.project_name}-health-alerts" }
}

data "aws_iam_policy_document" "sns_policy" {
  statement {
    sid        = "AllowEventBridgePublish"
    effect     = "Allow"
    principals { type = "Service", identifiers = ["events.amazonaws.com"] }
    actions    = ["sns:Publish"]
    resources  = [aws_sns_topic.health_alerts.arn]
  }
}

resource "aws_sns_topic_policy" "health_alerts" {
  arn    = aws_sns_topic.health_alerts.arn
  policy = data.aws_iam_policy_document.sns_policy.json
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.health_alerts.arn
  protocol  = "email"
  endpoint  = "ops-team@example.com"
}
```

## Step 2 -- Create EventBridge Rules for Health Events

### TODO 1: AWS Health Event Rule

Add the following to `events.tf`:

```hcl
# ------------------------------------------------------------------
# Rule: AWS Health Events
#
# AWS Health events notify you about service disruptions, scheduled
# maintenance, and account-specific issues BEFORE they impact your
# resources.
#
# Event structure:
# {
#   "source": "aws.health",
#   "detail-type": "AWS Health Event",
#   "detail": {
#     "service": "EC2",
#     "eventTypeCategory": "scheduledChange",
#     "eventTypeCode": "AWS_EC2_SYSTEM_MAINTENANCE_EVENT",
#     "affectedEntities": [
#       { "entityValue": "i-0abc123def456" }
#     ]
#   }
# }
#
# eventTypeCategory values:
#   - "issue": ongoing service issue
#   - "accountNotification": billing, limits, etc.
#   - "scheduledChange": upcoming maintenance
# ------------------------------------------------------------------

# TODO: Create EventBridge rule for all Health events
# Resource type: aws_cloudwatch_event_rule
# - name: "${var.project_name}-health-events"
# - description: "Capture all AWS Health events affecting this account"
# - event_pattern: match source "aws.health" and
#   detail-type "AWS Health Event"

# TODO: Create target sending to SNS topic with input transformer
# Extract: service, eventTypeCategory, eventTypeCode, time
# Template: "Health Alert: <service> - <category> - <code> at <time>"
```

### TODO 2: Health Event Rule for Specific Services

```hcl
# ------------------------------------------------------------------
# Rule: EC2 and RDS Health Events Only
#
# For production environments, you may want separate alerting for
# critical services vs informational notifications.
# ------------------------------------------------------------------

# TODO: Create a targeted rule for EC2 and RDS health events only
# - name: "${var.project_name}-critical-health"
# - event_pattern: source "aws.health", detail-type "AWS Health Event",
#   detail.service ["EC2", "RDS", "VPC"]
# - Target: same SNS topic
```

<details>
<summary>events.tf -- Solution: Health Event Rules</summary>

```hcl
resource "aws_cloudwatch_event_rule" "all_health" {
  name        = "${var.project_name}-health-events"
  description = "Capture all AWS Health events affecting this account"

  event_pattern = jsonencode({
    source      = ["aws.health"]
    detail-type = ["AWS Health Event"]
  })

  tags = { Name = "${var.project_name}-health-events" }
}

resource "aws_cloudwatch_event_target" "all_health_sns" {
  rule      = aws_cloudwatch_event_rule.all_health.name
  target_id = "health-to-sns"
  arn       = aws_sns_topic.health_alerts.arn

  input_transformer {
    input_paths = {
      service  = "$.detail.service"
      category = "$.detail.eventTypeCategory"
      code     = "$.detail.eventTypeCode"
      time     = "$.time"
    }
    input_template = "\"Health Alert: <service> - <category> - <code> at <time>\""
  }
}

resource "aws_cloudwatch_event_rule" "critical_health" {
  name        = "${var.project_name}-critical-health"
  description = "EC2, RDS, and VPC health events for critical infrastructure"

  event_pattern = jsonencode({
    source      = ["aws.health"]
    detail-type = ["AWS Health Event"]
    detail = {
      service = ["EC2", "RDS", "VPC"]
    }
  })

  tags = { Name = "${var.project_name}-critical-health" }
}

resource "aws_cloudwatch_event_target" "critical_health_sns" {
  rule      = aws_cloudwatch_event_rule.critical_health.name
  target_id = "critical-health-to-sns"
  arn       = aws_sns_topic.health_alerts.arn

  input_transformer {
    input_paths = {
      service  = "$.detail.service"
      category = "$.detail.eventTypeCategory"
      code     = "$.detail.eventTypeCode"
      time     = "$.time"
    }
    input_template = "\"CRITICAL Health Alert: <service> - <category> - <code> at <time>. Immediate investigation required.\""
  }
}
```

</details>

## Step 3 -- Apply and Verify

### `outputs.tf`

```hcl
output "sns_topic_arn" {
  value = aws_sns_topic.health_alerts.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Explore Health Events via CLI

```bash
# List recent Health events for your account
# Note: This returns events from the Personal Health Dashboard,
# not the public Service Health Dashboard
aws health describe-events \
  --filter '{
    "eventStatusCodes": ["open", "upcoming", "closed"],
    "startTimes": [{"from": "'"$(date -u -v-30d +%Y-%m-%dT%H:%M:%SZ)"'"}]
  }' \
  --query 'events[*].{Service:service,Category:eventTypeCategory,Code:eventTypeCode,Status:statusCode,Region:region}' \
  --output table \
  --region us-east-1 2>/dev/null || echo "No health events or health API not available (requires us-east-1 endpoint)"

# Describe event details for a specific event
# aws health describe-event-details --event-arns "arn:aws:health:..."

# List affected entities for a specific event
# aws health describe-affected-entities --filter '{"eventArns": ["arn:aws:health:..."]}'
```

## Step 5 -- Explore Trusted Advisor

```bash
# ------------------------------------------------------------------
# Trusted Advisor: analyzes your account against AWS best practices.
#
# Five categories:
#   1. Cost Optimization: underutilized resources, savings opportunities
#   2. Performance: overutilized resources, bottlenecks
#   3. Security: open ports, missing MFA, public resources
#   4. Fault Tolerance: no backups, single-AZ, no Multi-AZ
#   5. Service Limits: approaching quota limits
#
# IMPORTANT: Full Trusted Advisor requires Business or Enterprise
# Support plan. Basic/Developer plans only get 7 core checks.
# ------------------------------------------------------------------

# List available Trusted Advisor checks
# Note: This may return limited results on Basic/Developer support plans
aws support describe-trusted-advisor-checks \
  --language "en" \
  --query 'checks[*].{Id:id,Name:name,Category:category}' \
  --output table \
  --region us-east-1 2>/dev/null || echo "Trusted Advisor API requires Business or Enterprise Support plan"
```

### Trusted Advisor Checks by Support Plan

| Support Plan | Available Checks | Monthly Cost |
|---|---|---|
| **Basic** | 7 core checks (security only) | Free |
| **Developer** | 7 core checks (security only) | $29+/month |
| **Business** | All checks (~115+) | $100+/month (or 10% of monthly spend) |
| **Enterprise On-Ramp** | All checks + TAM access | $5,500/month |
| **Enterprise** | All checks + dedicated TAM | $15,000/month |

### The 7 Core Checks (Available on All Plans)

| # | Check Name | Category |
|---|---|---|
| 1 | S3 Bucket Permissions | Security |
| 2 | Security Groups - Specific Ports Unrestricted | Security |
| 3 | IAM Use | Security |
| 4 | MFA on Root Account | Security |
| 5 | EBS Public Snapshots | Security |
| 6 | RDS Public Snapshots | Security |
| 7 | Service Limits | Service Limits |

### TODO 3: Complete the Trusted Advisor Decision Table

```
# An exam question asks: "How can you identify underutilized
# EC2 instances to reduce costs?"
#
# TODO: For each Support plan, identify the best approach:
#
# +--------------------+-------------------------------------------+
# | Support Plan       | Approach to Identify Underutilized EC2    |
# +--------------------+-------------------------------------------+
# | Basic/Developer    | ???                                       |
# | Business+          | ???                                       |
# +--------------------+-------------------------------------------+
#
# Options:
# A. Trusted Advisor "Low Utilization Amazon EC2 Instances" check
# B. CloudWatch CPU metrics + manual analysis
# C. AWS Cost Explorer rightsizing recommendations
# D. AWS Compute Optimizer recommendations
```

<details>
<summary>Solution: Decision Table</summary>

| Support Plan | Best Approach |
|---|---|
| **Basic/Developer** | **C or D** -- Cost Explorer rightsizing recommendations and Compute Optimizer are available on all plans without requiring Business Support. CloudWatch metrics (B) work but require manual analysis. Trusted Advisor (A) is NOT available. |
| **Business+** | **A** -- Trusted Advisor "Low Utilization Amazon EC2 Instances" check provides automated identification with recommendations. Also use Compute Optimizer (D) for more detailed ML-based recommendations. |

Key exam insight: When the question mentions Trusted Advisor for cost optimization, performance, or fault tolerance checks, the answer is only valid if the scenario specifies Business or Enterprise Support. If the Support plan is not mentioned, look for alternatives that work on all plans (Cost Explorer, Compute Optimizer, CloudWatch).

</details>

## Spot the Bug

A team sets up Health event monitoring but misses critical notifications:

```hcl
resource "aws_cloudwatch_event_rule" "health" {
  name = "health-monitoring"

  event_pattern = jsonencode({
    source      = ["aws.health"]
    detail-type = ["AWS Health Event"]
    detail = {
      eventTypeCategory = ["issue"]
    }
  })
}

resource "aws_cloudwatch_event_target" "health_sns" {
  rule      = aws_cloudwatch_event_rule.health.name
  target_id = "notify"
  arn       = aws_sns_topic.alerts.arn
}
```

The team receives notifications about ongoing service issues but is surprised by unannounced EC2 maintenance reboots.

<details>
<summary>Explain the bug</summary>

**Bug: The rule only matches `eventTypeCategory = "issue"` but ignores `"scheduledChange"` events.**

AWS Health events have three categories:
- **`issue`**: Ongoing service problems (outages, degradations)
- **`scheduledChange`**: Upcoming maintenance (EC2 reboots, hardware migrations, SSL certificate rotations)
- **`accountNotification`**: Account-level notifications (billing alerts, limit increases)

EC2 scheduled maintenance events use `eventTypeCategory = "scheduledChange"`, not `"issue"`. The team's rule only matches active issues, so they are blind to maintenance events that AWS announces days or weeks in advance.

**Fix:** Include all relevant categories: `eventTypeCategory = ["issue", "scheduledChange"]`, or remove the category filter entirely to catch all Health events.

**Why this matters:** Scheduled maintenance events are the most actionable. When AWS sends `AWS_EC2_SYSTEM_MAINTENANCE_EVENT`, you have days to proactively stop/start the instance (migrating to new hardware) on your schedule, rather than AWS rebooting during the maintenance window.

</details>

## Verify What You Learned

```bash
# Verify EventBridge rules exist
aws events list-rules \
  --name-prefix "saa-ex94" \
  --query 'Rules[*].{Name:Name,State:State}' \
  --output table
```

Expected: Two rules in `ENABLED` state.

```bash
# Verify targets are attached
aws events list-targets-by-rule \
  --rule "saa-ex94-health-events" \
  --query 'Targets[*].{Id:Id,Arn:Arn}' \
  --output table
```

Expected: One target pointing to the SNS topic.

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

Exercise 95 covers **Athena S3 Query Analysis**, where you will use serverless SQL queries to analyze data stored in S3 -- including the CloudTrail logs you learned about in exercise 90 and the operational data from this monitoring section -- transitioning from the monitoring domain to the analytics domain.

## Summary

- **AWS Health Dashboard** shows service-wide status (public) and account-specific events (Personal Health Dashboard)
- **Personal Health Dashboard** alerts you to events affecting YOUR specific resources -- scheduled maintenance, degradations, and issues
- **Health event categories**: `issue` (ongoing problems), `scheduledChange` (upcoming maintenance), `accountNotification` (account-level)
- **EventBridge + Health** enables proactive automation -- migrate EC2 instances before scheduled maintenance windows
- **Trusted Advisor** analyzes your account against best practices across 5 categories: cost, performance, security, fault tolerance, service limits
- **7 core checks** (security + service limits) are available on all Support plans including Basic
- **Full Trusted Advisor** (115+ checks) requires Business or Enterprise Support plan ($100+/month)
- **Cost optimization alternatives** for Basic/Developer plans: Cost Explorer rightsizing, Compute Optimizer, CloudWatch manual analysis
- **Organization Health** aggregates Health events across all accounts in AWS Organizations
- Health events are delivered to EventBridge in **us-east-1 only** for global events; regional events use the regional event bus

## Reference

- [AWS Health Dashboard](https://docs.aws.amazon.com/health/latest/ug/what-is-aws-health.html)
- [AWS Trusted Advisor](https://docs.aws.amazon.com/awssupport/latest/user/trusted-advisor.html)
- [Health Events via EventBridge](https://docs.aws.amazon.com/health/latest/ug/cloudwatch-events-health.html)
- [Trusted Advisor Check Reference](https://docs.aws.amazon.com/awssupport/latest/user/trusted-advisor-check-reference.html)

## Additional Resources

- [Automating EC2 Maintenance with Health Events](https://docs.aws.amazon.com/health/latest/ug/getting-started-health-dashboard.html) -- proactively handling scheduled EC2 maintenance
- [Trusted Advisor Best Practices](https://docs.aws.amazon.com/awssupport/latest/user/get-started-with-aws-trusted-advisor.html) -- interpreting and acting on Trusted Advisor recommendations
- [Organization View for Trusted Advisor](https://docs.aws.amazon.com/awssupport/latest/user/organizational-view.html) -- aggregating Trusted Advisor results across all accounts
- [AWS Support Plan Comparison](https://aws.amazon.com/premiumsupport/plans/) -- detailed feature comparison across all support tiers
