# 56. EventBridge Archive and Replay

<!--
difficulty: intermediate
concepts: [eventbridge-archive, event-replay, event-retention, archive-filter, replay-destination, event-reprocessing]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply
prerequisites: [49-eventbridge-rules-event-patterns]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates EventBridge archives, rules, and an SQS queue. Archive storage costs $0.023/GB/month. Cost is approximately $0.01/hr for minimal event volumes. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** an EventBridge archive with retention period and event pattern filtering
- **Implement** event replay to re-process stored events from a specific time window
- **Design** archive strategies that store all events versus filtered subsets for cost optimization
- **Diagnose** the common mistake of replaying events without a matching rule on the destination bus
- **Explain** how replayed events have the same `source`, `detail-type`, and `detail` as originals but include a `replay-name` field

## Why EventBridge Archive and Replay

EventBridge archives store events for later retrieval and replay. This solves two critical problems: disaster recovery and bug fixing. When a processing Lambda has a bug that corrupts data for 2 hours before detection, you need to fix the bug and reprocess those 2 hours of events. Without an archive, those events are gone. With an archive, you fix the Lambda, start a replay for the affected time window, and the corrected Lambda processes the events again.

The DVA-C02 exam tests three concepts. First, archives store events based on an optional event pattern filter -- you can archive all events or only specific types (e.g., only `OrderCreated` events). Second, replay sends events back to an event bus, where they are matched against rules just like live events. If no rule matches the replayed events, they are ignored -- this is the most common exam trap. Third, archives have a configurable retention period (0 = indefinite). Events older than the retention period are automatically deleted.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
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
  default     = "archive-demo"
}
```

### `events.tf`

```hcl
# -------------------------------------------------------
# SQS Queue (target for processed events)
# -------------------------------------------------------
resource "aws_sqs_queue" "processed" {
  name                      = "${var.project_name}-processed"
  message_retention_seconds = 86400
}

data "aws_cloudwatch_event_bus" "default" {
  name = "default"
}

# =======================================================
# TODO 1 -- Create an EventBridge Archive
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_event_archive
#   - Set event_source_arn to the default event bus ARN
#     (use data source: data.aws_cloudwatch_event_bus.default.arn)
#   - Set retention_days to 7
#   - Add an event_pattern to only archive events with
#     source = "myapp.orders"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_archive


# =======================================================
# TODO 2 -- Create a Rule on the Default Bus
# =======================================================
# Requirements:
#   - Match events with source = "myapp.orders"
#   - Target: the SQS queue
#   - This rule processes BOTH live events AND replayed events
#   - Without this rule, replayed events are ignored
#
# IMPORTANT: Replayed events are sent to the event bus and
#   matched against rules just like live events. If no rule
#   matches, replayed events go nowhere.


# =======================================================
# TODO 3 -- SQS Queue Policy and Event Target
# =======================================================
# Requirements:
#   - Allow EventBridge to send messages to the SQS queue
#   - Create an aws_cloudwatch_event_target connecting
#     the rule to the SQS queue
```

### `outputs.tf`

```hcl
output "archive_name" {
  value = aws_cloudwatch_event_archive.orders.name
}

output "queue_url" {
  value = aws_sqs_queue.processed.url
}
```

After deploying, complete the CLI tasks:

```bash
# =======================================================
# TODO 4 -- Publish events and verify archival
# =======================================================
# Requirements:
#   - Publish 5 order events to the default bus
#   - Wait and verify they are archived
#   - Use aws events describe-archive to check event count
#
# Example:
#   aws events put-events --entries '[{
#     "Source": "myapp.orders",
#     "DetailType": "OrderCreated",
#     "Detail": "{\"order_id\":\"o-001\",\"total\":49.99}"
#   }]'


# =======================================================
# TODO 5 -- Start a replay
# =======================================================
# Requirements:
#   - Use aws events start-replay to replay archived events
#   - Set --event-source-arn to the archive ARN
#   - Set --destination to the default event bus ARN
#   - Set --event-start-time and --event-end-time to
#     cover the events from TODO 4
#   - Monitor replay status with aws events describe-replay
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/events/start-replay.html
# IMPORTANT: The destination bus must have a rule that
#   matches the replayed events, or they will be ignored.
```

## Spot the Bug

A developer archives all order events and needs to replay them after fixing a bug. They start the replay but the processing Lambda never fires.

```hcl
# Archive on the default bus
resource "aws_cloudwatch_event_archive" "orders" {
  name             = "order-events-archive"
  event_source_arn = data.aws_cloudwatch_event_bus.default.arn
  retention_days   = 30
  event_pattern = jsonencode({
    source = ["myapp.orders"]
  })
}

# Rule on a CUSTOM bus (not the default bus)
resource "aws_cloudwatch_event_bus" "processing" {
  name = "processing-bus"
}

resource "aws_cloudwatch_event_rule" "orders" {
  name           = "process-orders"
  event_bus_name = aws_cloudwatch_event_bus.processing.name
  event_pattern = jsonencode({
    source = ["myapp.orders"]
  })
}
```

```bash
# Replay to the default bus
aws events start-replay \
  --name "bugfix-replay" \
  --event-source-arn "arn:aws:events:us-east-1:123456789012:archive/order-events-archive" \
  --destination '{"Arn": "arn:aws:events:us-east-1:123456789012:event-bus/default"}' \
  --event-start-time "2025-01-01T00:00:00Z" \
  --event-end-time "2025-01-02T00:00:00Z"
```

<details>
<summary>Explain the bug</summary>

The replay sends events to the **default event bus**, but the rule that matches `myapp.orders` events is on the **custom bus** (`processing-bus`). Replayed events land on the default bus where no rule matches them, so they are silently discarded.

**Fix -- either move the rule to the default bus or replay to the custom bus:**

**Option A -- rule on the default bus:**

```hcl
resource "aws_cloudwatch_event_rule" "orders" {
  name = "process-orders"
  # event_bus_name defaults to "default"
  event_pattern = jsonencode({
    source = ["myapp.orders"]
  })
}
```

**Option B -- replay to the custom bus (if the archive is on the custom bus):**

Note: archives are associated with a specific event bus. You can only replay archived events to the same bus they were archived from, unless you use cross-bus rules.

On the exam, "replay succeeds but events are not processed" means either: (1) no rule matches on the destination bus, or (2) the rule is on a different bus than where events are replayed.

</details>

## Verify What You Learned

```bash
# Verify archive exists with correct settings
aws events describe-archive --archive-name archive-demo-orders \
  --query "{State:State,RetentionDays:RetentionDays,EventCount:EventCount}" --output json
```

Expected: `State=ENABLED`, `RetentionDays=7`, `EventCount > 0`.

```bash
# Publish events and verify archival
aws events put-events --entries '[{
  "Source": "myapp.orders",
  "DetailType": "OrderCreated",
  "Detail": "{\"order_id\":\"verify-001\",\"total\":10.00}"
}]'

sleep 10

aws events describe-archive --archive-name archive-demo-orders \
  --query "EventCount" --output text
```

Expected: event count increases.

```bash
# Verify SQS received the live event
aws sqs receive-message --queue-url $(terraform output -raw queue_url) \
  --max-number-of-messages 1 --wait-time-seconds 5 \
  --query "Messages[0].Body" --output text | jq .
```

Expected: event JSON with `source: myapp.orders`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- EventBridge Archive (`events.tf`)</summary>

```hcl
resource "aws_cloudwatch_event_archive" "orders" {
  name             = "${var.project_name}-orders"
  event_source_arn = data.aws_cloudwatch_event_bus.default.arn
  retention_days   = 7

  event_pattern = jsonencode({
    source = ["myapp.orders"]
  })
}
```

</details>

<details>
<summary>TODO 2 -- EventBridge Rule (`events.tf`)</summary>

```hcl
resource "aws_cloudwatch_event_rule" "orders" {
  name = "${var.project_name}-process-orders"

  event_pattern = jsonencode({
    source = ["myapp.orders"]
  })
}
```

</details>

<details>
<summary>TODO 3 -- SQS Policy and Target (`events.tf`)</summary>

```hcl
data "aws_iam_policy_document" "sqs_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.processed.arn]
    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "processed" {
  queue_url = aws_sqs_queue.processed.id
  policy    = data.aws_iam_policy_document.sqs_policy.json
}

resource "aws_cloudwatch_event_target" "sqs" {
  rule = aws_cloudwatch_event_rule.orders.name
  arn  = aws_sqs_queue.processed.arn
}
```

</details>

<details>
<summary>TODO 4 + TODO 5 -- Publish and Replay</summary>

```bash
# Publish events
for i in $(seq 1 5); do
  aws events put-events --entries "[{
    \"Source\": \"myapp.orders\",
    \"DetailType\": \"OrderCreated\",
    \"Detail\": \"{\\\"order_id\\\":\\\"o-00$i\\\",\\\"total\\\":$((i * 10))}\"
  }]"
done

# Wait for archival
sleep 30

# Check archive event count
aws events describe-archive --archive-name archive-demo-orders \
  --query "EventCount" --output text

# Drain the SQS queue first (to distinguish replay from live)
QUEUE_URL=$(terraform output -raw queue_url)
aws sqs purge-queue --queue-url "$QUEUE_URL"
sleep 5

# Start replay
START_TIME=$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ 2>/dev/null || date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%SZ)
END_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)

aws events start-replay \
  --name "test-replay-$(date +%s)" \
  --event-source-arn "$(aws events describe-archive --archive-name archive-demo-orders --query 'ArchiveArn' --output text)" \
  --destination "{\"Arn\": \"$(aws events describe-event-bus --name default --query 'Arn' --output text)\"}" \
  --event-start-time "$START_TIME" \
  --event-end-time "$END_TIME"

# Wait for replay to complete
sleep 30

# Verify replayed events arrived in SQS
aws sqs get-queue-attributes --queue-url "$QUEUE_URL" \
  --attribute-names ApproximateNumberOfMessages \
  --query "Attributes.ApproximateNumberOfMessages" --output text
# Expected: 5 (the replayed events)
```

</details>

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured EventBridge archive and replay for event recovery and reprocessing. In the next exercise, you will set up **Secrets Manager with automatic rotation** -- storing database credentials and configuring a Lambda rotation function for 30-day automatic credential rotation.

## Summary

- **EventBridge archives** store events from an event bus with optional pattern filtering and configurable retention
- **Replay** sends archived events back to an event bus where they are matched against rules like live events
- Replayed events are **ignored** if no rule on the destination bus matches them
- Archives have a **retention period** (in days) -- set to 0 for indefinite retention
- Archives can filter events using the same **event pattern** syntax as rules (source, detail-type, detail)
- Replayed events include the original `source`, `detail-type`, and `detail` but can be identified via replay metadata
- Archive storage costs **$0.023/GB/month** -- use event pattern filters to archive only relevant events
- Each replay covers a **time window** (start time to end time) -- you cannot replay individual events

## Reference

- [EventBridge Archive and Replay](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-archive.html)
- [AWS CLI start-replay](https://docs.aws.amazon.com/cli/latest/reference/events/start-replay.html)
- [Terraform aws_cloudwatch_event_archive](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_archive)
- [EventBridge Event Patterns](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-patterns.html)

## Additional Resources

- [Replay Best Practices](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-replay.html) -- managing replay state, monitoring progress, and handling duplicate processing
- [Archive Pricing](https://aws.amazon.com/eventbridge/pricing/) -- storage and ingestion costs for archived events
- [Event-Driven Disaster Recovery](https://aws.amazon.com/blogs/compute/disaster-recovery-for-serverless-workloads-with-amazon-eventbridge/) -- using archives for DR scenarios
- [Idempotent Event Processing](https://docs.aws.amazon.com/lambda/latest/operatorguide/dedup-function.html) -- ensuring replayed events are processed safely without duplicates
