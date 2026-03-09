# 93. EventBridge Operational Events

<!--
difficulty: intermediate
concepts: [eventbridge, event-rules, event-patterns, ec2-state-change, ebs-snapshot, rds-failover, sns-target, lambda-target, auto-remediation, operational-events]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [92-systems-manager-session-patch]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** EventBridge rules are free. SNS topics and Lambda invocations fall within free tier for this exercise. No EC2 instances are created. Total cost ~$0.01/hr for S3 storage only. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 92 (Systems Manager) or equivalent knowledge
- Understanding of EventBridge concepts (events, rules, targets) from exercise 81
- Familiarity with SNS topics and Lambda functions

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** EventBridge rules that capture operational events (EC2 state changes, EBS snapshot completion, RDS failover) and route them to SNS and Lambda targets
2. **Analyze** EventBridge event patterns to correctly match AWS service events by source, detail-type, and detail fields
3. **Evaluate** which operational events warrant automated remediation versus notification-only responses
4. **Apply** input transformers to extract relevant fields from events before passing them to targets
5. **Distinguish** between event source names (e.g., `aws.ec2` vs `ec2`) and common pattern matching mistakes

## Why This Matters

EventBridge is the central nervous system for operational automation on AWS, and the SAA-C03 exam tests your ability to connect AWS service events to automated responses. Every AWS service emits events to EventBridge -- EC2 state changes, EBS snapshot completions, RDS failovers, GuardDuty findings, Health Dashboard notifications. The architectural question is: which events should trigger automated remediation, which should trigger notifications, and which can be safely ignored?

The exam presents scenarios like "EC2 instances are being terminated unexpectedly in production" and asks you to design the detection and response. The answer combines an EventBridge rule matching EC2 state change events (filtering for `terminated` state), an SNS notification to the operations team, and optionally a Lambda function that investigates the termination reason and creates a replacement instance if it was a spot interruption.

The most common mistake on the exam and in practice is getting the event pattern wrong. EventBridge sources use the format `aws.{service}` (e.g., `aws.ec2`, `aws.rds`, `aws.ebs`), not the service name alone. The `detail-type` field uses specific strings that vary by event type. A rule with the wrong source or detail-type silently matches nothing -- no errors, no events, no remediation. Understanding the exact event structure is essential.

## Step 1 -- Create the Notification Infrastructure

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
  default     = "saa-ex93"
}
```

### `main.tf`

```hcl
resource "aws_sns_topic" "ops_alerts" {
  name = "${var.project_name}-ops-alerts"
  tags = { Name = "${var.project_name}-ops-alerts" }
}

resource "aws_sns_topic_subscription" "email" {
  topic_arn = aws_sns_topic.ops_alerts.arn
  protocol  = "email"
  endpoint  = "ops-team@example.com"
}

# Allow EventBridge to publish to SNS (required, otherwise delivery fails silently)
data "aws_iam_policy_document" "sns_policy" {
  statement {
    sid        = "AllowEventBridgePublish"
    effect     = "Allow"
    principals { type = "Service", identifiers = ["events.amazonaws.com"] }
    actions    = ["sns:Publish"]
    resources  = [aws_sns_topic.ops_alerts.arn]
  }
}

resource "aws_sns_topic_policy" "ops_alerts" {
  arn    = aws_sns_topic.ops_alerts.arn
  policy = data.aws_iam_policy_document.sns_policy.json
}
```

## Step 2 -- Create EventBridge Rules for EC2 Events

### TODO 1: EC2 Instance State Change Rule

Add the following to `events.tf`:

```hcl
# ------------------------------------------------------------------
# Rule: EC2 Instance State Change Notification
#
# Matches when any EC2 instance transitions to "terminated" or
# "stopped" state. These events indicate potential issues:
# - Spot interruptions
# - Instance health check failures
# - Manual or automated terminations
#
# Event structure:
# {
#   "source": "aws.ec2",
#   "detail-type": "EC2 Instance State-change Notification",
#   "detail": {
#     "instance-id": "i-0abc123def456",
#     "state": "terminated"
#   }
# }
# ------------------------------------------------------------------

# TODO: Create the EventBridge rule
# Resource type: aws_cloudwatch_event_rule
# - name: "${var.project_name}-ec2-state-change"
# - description: "Detect EC2 instance terminated or stopped"
# - event_pattern: JSON matching source "aws.ec2",
#   detail-type "EC2 Instance State-change Notification",
#   and detail.state ["terminated", "stopped"]

# TODO: Create the EventBridge target
# Resource type: aws_cloudwatch_event_target
# - rule: reference the rule above
# - target_id: "send-to-sns"
# - arn: reference the SNS topic ARN
#
# Add an input_transformer block to format the notification:
#   input_paths = {
#     instance = "$.detail.instance-id"
#     state    = "$.detail.state"
#     time     = "$.time"
#   }
#   input_template = "\"EC2 Alert: Instance <instance> changed to <state> at <time>\""
```

### TODO 2: EBS Snapshot Event Rule

```hcl
# ------------------------------------------------------------------
# Rule: EBS Snapshot Completion/Failure
#
# Matches when an EBS snapshot completes or fails.
# Failed snapshots need immediate attention -- the backup may be
# missing, leaving data unprotected.
#
# Event structure:
# {
#   "source": "aws.ec2",
#   "detail-type": "EBS Snapshot Notification",
#   "detail": {
#     "event": "createSnapshot",
#     "result": "failed",
#     "snapshot_id": "snap-0abc123",
#     "source": "vol-0abc123"
#   }
# }
# ------------------------------------------------------------------

# TODO: Create the EventBridge rule for EBS snapshot failures
# - name: "${var.project_name}-ebs-snapshot-failed"
# - event_pattern: source "aws.ec2", detail-type "EBS Snapshot Notification",
#   detail.result ["failed"]

# TODO: Create the EventBridge target for the SNS topic
```

### TODO 3: RDS Event Rule

```hcl
# ------------------------------------------------------------------
# Rule: RDS Instance Events
#
# Matches RDS failover events, which indicate a Multi-AZ failover
# has occurred. While Multi-AZ failover is automatic, the operations
# team should be notified to investigate the root cause.
#
# Event structure:
# {
#   "source": "aws.rds",
#   "detail-type": "RDS DB Instance Event",
#   "detail": {
#     "EventCategories": ["failover"],
#     "SourceType": "DB_INSTANCE",
#     "SourceIdentifier": "my-database"
#   }
# }
# ------------------------------------------------------------------

# TODO: Create the EventBridge rule for RDS failover
# - name: "${var.project_name}-rds-failover"
# - event_pattern: source "aws.rds", detail-type "RDS DB Instance Event",
#   detail.EventCategories ["failover"]

# TODO: Create the EventBridge target for the SNS topic
```

<details>
<summary>events.tf -- Solution: All EventBridge Rules and Targets</summary>

```hcl
# --- EC2 State Change ---
resource "aws_cloudwatch_event_rule" "ec2_state" {
  name        = "${var.project_name}-ec2-state-change"
  description = "Detect EC2 instance terminated or stopped"

  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EC2 Instance State-change Notification"]
    detail = {
      state = ["terminated", "stopped"]
    }
  })

  tags = { Name = "${var.project_name}-ec2-state-change" }
}

resource "aws_cloudwatch_event_target" "ec2_sns" {
  rule      = aws_cloudwatch_event_rule.ec2_state.name
  target_id = "send-to-sns"
  arn       = aws_sns_topic.ops_alerts.arn

  input_transformer {
    input_paths = {
      instance = "$.detail.instance-id"
      state    = "$.detail.state"
      time     = "$.time"
    }
    input_template = "\"EC2 Alert: Instance <instance> changed to <state> at <time>\""
  }
}

# --- EBS Snapshot Failed ---
resource "aws_cloudwatch_event_rule" "ebs_snapshot" {
  name        = "${var.project_name}-ebs-snapshot-failed"
  description = "Detect failed EBS snapshot creation"

  event_pattern = jsonencode({
    source      = ["aws.ec2"]
    detail-type = ["EBS Snapshot Notification"]
    detail = {
      result = ["failed"]
    }
  })

  tags = { Name = "${var.project_name}-ebs-snapshot-failed" }
}

resource "aws_cloudwatch_event_target" "ebs_sns" {
  rule      = aws_cloudwatch_event_rule.ebs_snapshot.name
  target_id = "send-to-sns"
  arn       = aws_sns_topic.ops_alerts.arn

  input_transformer {
    input_paths = {
      snapshot = "$.detail.snapshot_id"
      source   = "$.detail.source"
      time     = "$.time"
    }
    input_template = "\"EBS Alert: Snapshot <snapshot> FAILED for volume <source> at <time>. Investigate and retry.\""
  }
}

# --- RDS Failover ---
resource "aws_cloudwatch_event_rule" "rds_failover" {
  name        = "${var.project_name}-rds-failover"
  description = "Detect RDS Multi-AZ failover events"

  event_pattern = jsonencode({
    source      = ["aws.rds"]
    detail-type = ["RDS DB Instance Event"]
    detail = {
      EventCategories = ["failover"]
    }
  })

  tags = { Name = "${var.project_name}-rds-failover" }
}

resource "aws_cloudwatch_event_target" "rds_sns" {
  rule      = aws_cloudwatch_event_rule.rds_failover.name
  target_id = "send-to-sns"
  arn       = aws_sns_topic.ops_alerts.arn

  input_transformer {
    input_paths = {
      db   = "$.detail.SourceIdentifier"
      time = "$.time"
    }
    input_template = "\"RDS Alert: Database <db> completed Multi-AZ failover at <time>. Investigate root cause.\""
  }
}
```

</details>

## Step 3 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "sns_topic_arn" {
  value = aws_sns_topic.ops_alerts.arn
}

output "ec2_rule_arn" {
  value = aws_cloudwatch_event_rule.ec2_state.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Test the Rules

```bash
# Test by sending a custom event that simulates an EC2 state change
# (EventBridge allows custom events for testing)
aws events put-events --entries '[
  {
    "Source": "aws.ec2",
    "DetailType": "EC2 Instance State-change Notification",
    "Detail": "{\"instance-id\": \"i-test12345\", \"state\": \"terminated\"}"
  }
]'

# Verify the rule matched (check invocation metrics)
aws cloudwatch get-metric-statistics \
  --namespace "AWS/Events" \
  --metric-name "Invocations" \
  --dimensions Name=RuleName,Value=saa-ex93-ec2-state-change \
  --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --period 300 \
  --statistics Sum \
  --output table
```

## Spot the Bug

An operations team creates an EventBridge rule to detect EC2 instance terminations, but the rule never matches any events:

```hcl
resource "aws_cloudwatch_event_rule" "ec2_terminated" {
  name = "detect-ec2-termination"

  event_pattern = jsonencode({
    source      = ["ec2"]
    detail-type = ["EC2 Instance State-change Notification"]
    detail = {
      state = ["terminated"]
    }
  })
}

resource "aws_cloudwatch_event_target" "notify" {
  rule      = aws_cloudwatch_event_rule.ec2_terminated.name
  target_id = "sns-alert"
  arn       = aws_sns_topic.alerts.arn
}
```

The team confirms EC2 instances are being terminated, but no SNS notifications arrive.

<details>
<summary>Explain the bug</summary>

**Bug: The event source is `"ec2"` but it should be `"aws.ec2"`.**

EventBridge uses `aws.{service}` for all AWS service event sources. The source field is case-sensitive: `"aws.ec2"` is correct; `"ec2"`, `"EC2"`, `"AWS.EC2"` are all wrong. Common sources: `aws.ec2`, `aws.rds`, `aws.s3`, `aws.autoscaling`, `aws.health`, `aws.guardduty`.

**Fix:** Change `source = ["ec2"]` to `source = ["aws.ec2"]`.

This is a silent failure -- EventBridge does not error on patterns that match nothing. The rule shows `ENABLED` but never triggers. Debug by checking CloudWatch metrics for `MatchedEvents` on the rule -- if 0, the pattern is wrong.

</details>

## Verify What You Learned

```bash
# Verify all EventBridge rules exist and are enabled
aws events list-rules \
  --name-prefix "saa-ex93" \
  --query 'Rules[*].{Name:Name,State:State}' \
  --output table
```

Expected: Three rules, all in `ENABLED` state.

```bash
# Verify targets are attached to each rule
for rule in saa-ex93-ec2-state-change saa-ex93-ebs-snapshot-failed saa-ex93-rds-failover; do
  echo "--- $rule ---"
  aws events list-targets-by-rule --rule "$rule" \
    --query 'Targets[*].{Id:Id,Arn:Arn}' --output table
done
```

Expected: Each rule has one SNS target.

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

Exercise 94 covers **AWS Health Dashboard and Trusted Advisor**, where you will configure EventBridge rules for Health events affecting your resources and explore Trusted Advisor checks across the five categories -- building on the EventBridge rule patterns from this exercise to create proactive notifications for service disruptions.

## Summary

- **EventBridge** captures operational events from all AWS services and routes them to targets for notification or remediation
- **Event patterns** must use the correct source format: `aws.ec2`, `aws.rds`, `aws.s3` (not `ec2`, `rds`, `s3`)
- **Event source names are case-sensitive** -- `aws.ec2` works, `AWS.EC2` does not
- **Input transformers** extract specific fields from events and format them into human-readable notifications
- **EC2 state change events** detect instance terminations, stops, and other lifecycle transitions
- **EBS snapshot events** detect failed snapshot creation for backup monitoring
- **RDS failover events** detect Multi-AZ failovers that need root cause investigation
- **Silent failures** are the biggest risk -- a wrong event pattern creates no errors but matches no events
- **CloudWatch metrics** (`MatchedEvents`, `Invocations`) are the primary debugging tool for EventBridge rules
- **SNS topic policies** must explicitly allow `events.amazonaws.com` to publish -- without this, delivery fails silently

## Reference

- [EventBridge Event Patterns](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns.html)
- [EC2 Events in EventBridge](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-instance-state-changes.html)
- [RDS Events in EventBridge](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-cloud-watch-events.html)
- [Terraform aws_cloudwatch_event_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_rule)

## Additional Resources

- [EventBridge Input Transformation](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-transform-target-input.html) -- formatting event data before delivery to targets
- [EventBridge Event Bus Policies](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-bus-perms.html) -- cross-account event sharing
- [Debugging EventBridge Rules](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-troubleshooting.html) -- common issues and resolution steps
- [AWS Service Events Reference](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-service-event.html) -- complete list of AWS service event sources and detail-types
