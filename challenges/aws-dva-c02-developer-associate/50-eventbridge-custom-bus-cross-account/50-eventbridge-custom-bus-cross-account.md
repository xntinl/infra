# 50. EventBridge Custom Event Bus and Cross-Account Access

<!--
difficulty: intermediate
concepts: [custom-event-bus, event-bus-policy, resource-policy, cross-account-events, put-events-bus, event-bus-arn]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design
prerequisites: [49-eventbridge-rules-event-patterns]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates EventBridge custom event buses, rules, Lambda functions, and SQS queues. Cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |
| Completed exercise 49 | EventBridge rules fundamentals |

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a custom EventBridge event bus that isolates application events from AWS service events on the default bus
- **Configure** a resource-based policy on a custom event bus that grants cross-account `events:PutEvents` access
- **Implement** rules on a custom event bus that route events to Lambda and SQS targets
- **Differentiate** between the default event bus (receives AWS service events automatically) and custom event buses (receive only explicitly published events)
- **Diagnose** the common mistake of creating a rule on the default bus when events are published to a custom bus

## Why Custom Event Buses

The default EventBridge event bus receives events from AWS services (EC2 state changes, S3 notifications, etc.) and your custom events. As your application grows, mixing application events with AWS service events makes rules harder to manage. Custom event buses provide isolation: one bus for order events, another for inventory events, a third for partner integrations.

Custom buses also enable cross-account event routing. By attaching a resource-based policy to a custom bus, you allow another AWS account to publish events to your bus. This is how multi-account architectures share events without direct service-to-service calls. The DVA-C02 exam tests whether you understand: (1) events published to a custom bus are only matched by rules on that same bus -- rules on the default bus do not see them; (2) cross-account access requires a resource policy with `events:PutEvents` and the source account principal; (3) the `EventBusName` parameter in `PutEvents` must specify the custom bus ARN or name -- omitting it sends events to the default bus.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
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
  default     = "eb-custom-bus-demo"
}
```

### `events.tf`

```hcl
# Use your own account ID for the cross-account demo
data "aws_caller_identity" "current" {}

# =======================================================
# TODO 1 -- Create a Custom Event Bus (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_event_bus named "${var.project_name}-orders"
#   - This bus will receive application order events,
#     separate from AWS service events on the default bus
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_bus


# =======================================================
# TODO 2 -- Attach a Resource Policy for Cross-Account Access (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_event_bus_policy on the custom bus
#   - Use a data "aws_iam_policy_document" to define the policy
#   - Grant events:PutEvents to the current account
#     (in production, this would be a different account ID)
#   - Use the condition key "aws:PrincipalOrgID" or restrict
#     by account ID via principals
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_bus_policy
# Note: In production, replace data.aws_caller_identity.current.account_id
#       with the partner/source account ID


# =======================================================
# TODO 3 -- Create a Rule on the Custom Bus (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_event_rule on the CUSTOM bus
#     (not the default bus)
#   - Set event_bus_name to the custom bus name
#   - Match events with source = "partner.orders" and
#     detail-type = ["OrderReceived", "OrderUpdated"]
#
# IMPORTANT: Rules on the default bus do NOT see events
#   published to custom buses. The rule must be on the
#   same bus that receives the events.


# SQS Target for matched events
resource "aws_sqs_queue" "order_events" {
  name = "${var.project_name}-order-events"
}

# =======================================================
# TODO 4 -- Create an Event Target on the Custom Bus Rule (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_event_target pointing to
#     the SQS queue
#   - Set event_bus_name to the custom bus name
#   - The rule attribute must reference the rule from TODO 3
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_target


# =======================================================
# TODO 5 -- SQS Queue Policy for EventBridge (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_sqs_queue_policy granting
#     sqs:SendMessage to events.amazonaws.com
#   - Condition: aws:SourceArn = the rule ARN from TODO 3
#
# Without this policy, EventBridge cannot deliver events to SQS
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "handler" {
  name               = "${var.project_name}-handler-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "handler_basic" {
  role       = aws_iam_role.handler.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `outputs.tf`

```hcl
output "custom_bus_name" {
  value = aws_cloudwatch_event_bus.orders.name
}

output "custom_bus_arn" {
  value = aws_cloudwatch_event_bus.orders.arn
}

output "queue_url" {
  value = aws_sqs_queue.order_events.url
}
```

## Spot the Bug

A developer creates a custom event bus for partner events and a rule to process them. Events are published successfully but the Lambda target never fires.

```hcl
# Custom bus
resource "aws_cloudwatch_event_bus" "partner" {
  name = "partner-events"
}

# Rule on the DEFAULT bus (bug!)
resource "aws_cloudwatch_event_rule" "partner_orders" {
  name = "partner-order-rule"
  # event_bus_name is NOT set -- defaults to the default bus

  event_pattern = jsonencode({
    source      = ["partner.orders"]
    detail-type = ["OrderReceived"]
  })
}

resource "aws_cloudwatch_event_target" "lambda" {
  rule = aws_cloudwatch_event_rule.partner_orders.name
  arn  = aws_lambda_function.processor.arn
}
```

```bash
# Publishing to the custom bus
aws events put-events --entries '[{
  "Source": "partner.orders",
  "DetailType": "OrderReceived",
  "Detail": "{\"order_id\":\"p-001\"}",
  "EventBusName": "partner-events"
}]'
```

<details>
<summary>Explain the bug</summary>

The rule is on the **default** event bus (because `event_bus_name` is not set), but events are published to the **custom** bus (`partner-events`). Rules on the default bus never see events published to custom buses. The event is delivered to the custom bus successfully, but no rule matches it there.

**Fix -- create the rule on the custom bus:**

```hcl
resource "aws_cloudwatch_event_rule" "partner_orders" {
  name           = "partner-order-rule"
  event_bus_name = aws_cloudwatch_event_bus.partner.name  # Must match the bus

  event_pattern = jsonencode({
    source      = ["partner.orders"]
    detail-type = ["OrderReceived"]
  })
}

resource "aws_cloudwatch_event_target" "lambda" {
  rule           = aws_cloudwatch_event_rule.partner_orders.name
  event_bus_name = aws_cloudwatch_event_bus.partner.name  # Also required on target
  arn            = aws_lambda_function.processor.arn
}
```

Both the rule and the target must specify `event_bus_name`. On the exam, when you see "events are published to a custom bus but the rule doesn't trigger," check whether the rule is on the same bus.

</details>

## Verify What You Learned

```bash
# Verify custom bus exists
aws events list-event-buses --query "EventBuses[?Name!='default'].Name" --output text
```

Expected: `eb-custom-bus-demo-orders`

```bash
# Verify rule is on the custom bus
aws events list-rules --event-bus-name eb-custom-bus-demo-orders \
  --query "Rules[*].{Name:Name,State:State}" --output table
```

Expected: one rule in ENABLED state.

```bash
# Publish event to custom bus and verify SQS delivery
aws events put-events --entries "[{
  \"Source\": \"partner.orders\",
  \"DetailType\": \"OrderReceived\",
  \"Detail\": \"{\\\"order_id\\\":\\\"test-001\\\"}\",
  \"EventBusName\": \"eb-custom-bus-demo-orders\"
}]"

sleep 5

aws sqs receive-message --queue-url $(terraform output -raw queue_url) \
  --max-number-of-messages 1 --wait-time-seconds 5 \
  --query "Messages[0].Body" --output text | jq .
```

Expected: event JSON with `source: partner.orders` and `detail-type: OrderReceived`.

```bash
# Verify resource policy on the custom bus
aws events describe-event-bus --name eb-custom-bus-demo-orders \
  --query "Policy" --output text | jq .
```

Expected: JSON policy document with `events:PutEvents` permission.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- Custom Event Bus</summary>

### `events.tf`

```hcl
resource "aws_cloudwatch_event_bus" "orders" {
  name = "${var.project_name}-orders"
}
```

</details>

<details>
<summary>TODO 2 -- Resource Policy for Cross-Account Access</summary>

### `events.tf`

```hcl
data "aws_iam_policy_document" "bus_policy" {
  statement {
    sid     = "AllowCrossAccountPutEvents"
    effect  = "Allow"
    actions = ["events:PutEvents"]
    resources = [aws_cloudwatch_event_bus.orders.arn]

    principals {
      type        = "AWS"
      identifiers = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]
    }
  }
}

resource "aws_cloudwatch_event_bus_policy" "orders" {
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  policy         = data.aws_iam_policy_document.bus_policy.json
}
```

</details>

<details>
<summary>TODO 3 -- Rule on the Custom Bus</summary>

### `events.tf`

```hcl
resource "aws_cloudwatch_event_rule" "partner_orders" {
  name           = "${var.project_name}-partner-orders"
  event_bus_name = aws_cloudwatch_event_bus.orders.name

  event_pattern = jsonencode({
    source      = ["partner.orders"]
    detail-type = ["OrderReceived", "OrderUpdated"]
  })
}
```

</details>

<details>
<summary>TODO 4 -- Event Target on Custom Bus</summary>

### `events.tf`

```hcl
resource "aws_cloudwatch_event_target" "sqs" {
  rule           = aws_cloudwatch_event_rule.partner_orders.name
  event_bus_name = aws_cloudwatch_event_bus.orders.name
  arn            = aws_sqs_queue.order_events.arn
}
```

</details>

<details>
<summary>TODO 5 -- SQS Queue Policy</summary>

### `events.tf`

```hcl
data "aws_iam_policy_document" "sqs_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.order_events.arn]

    principals {
      type        = "Service"
      identifiers = ["events.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_cloudwatch_event_rule.partner_orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "order_events" {
  queue_url = aws_sqs_queue.order_events.id
  policy    = data.aws_iam_policy_document.sqs_policy.json
}
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

You created a custom event bus with cross-account resource policies and rules that route events to SQS targets. In the next exercise, you will configure **EventBridge Scheduler** -- creating one-time and recurring schedules with rate expressions, cron expressions, and flexible time windows.

## Summary

- **Custom event buses** isolate application events from AWS service events on the default bus
- Rules on the default bus **do not see** events published to custom buses -- rules must be on the same bus
- Both `aws_cloudwatch_event_rule` and `aws_cloudwatch_event_target` must set `event_bus_name` for custom buses
- **Resource-based policies** on custom buses grant `events:PutEvents` to other accounts for cross-account event routing
- The `EventBusName` parameter in `PutEvents` must be set to the custom bus name or ARN -- omitting it publishes to the default bus
- SQS targets require a **queue policy** granting `sqs:SendMessage` to `events.amazonaws.com`
- Custom buses support up to **300 rules** per bus (same as the default bus)

## Reference

- [EventBridge Event Buses](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-event-bus.html)
- [EventBridge Resource-Based Policies](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-use-resource-based.html)
- [Terraform aws_cloudwatch_event_bus](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_bus)
- [Terraform aws_cloudwatch_event_bus_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_event_bus_policy)

## Additional Resources

- [Cross-Account Event Delivery](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-cross-account.html) -- patterns for multi-account event routing
- [EventBridge Organization-Level Policies](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-use-resource-based.html#eb-use-resource-based-organizations) -- using `aws:PrincipalOrgID` for organization-wide access
- [EventBridge Quotas](https://docs.aws.amazon.com/eventbridge/latest/userguide/eb-quota.html) -- limits on custom buses, rules, and targets per account
- [Multi-Account EventBridge Patterns](https://docs.aws.amazon.com/prescriptive-guidance/latest/patterns/send-and-receive-amazon-eventbridge-events-between-aws-accounts.html) -- architectural guidance for enterprise event routing
