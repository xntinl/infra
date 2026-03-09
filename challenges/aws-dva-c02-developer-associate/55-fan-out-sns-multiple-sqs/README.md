# 55. Fan-Out Pattern: SNS with Multiple SQS Subscribers

<!--
difficulty: advanced
concepts: [fan-out-pattern, sns-sqs-fanout, filter-policies, sqs-queue-policy, raw-message-delivery, decoupled-architecture]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [47-sns-topics-subscriptions-filtering, 48-sns-message-filtering-policies]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates an SNS topic, three SQS queues, a Lambda publisher, and associated IAM resources. Cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a fan-out architecture where SNS distributes order events to three specialized SQS queues with filter policies
- **Configure** SQS queue policies that grant SNS the `sqs:SendMessage` permission required for message delivery
- **Evaluate** the tradeoffs between raw message delivery (message body only) and the default SNS notification envelope
- **Implement** a Go program that publishes order events with message attributes for filter-based routing
- **Diagnose** silent message delivery failures caused by missing SQS queue policies

## Why the Fan-Out Pattern

The fan-out pattern uses SNS as a message router that distributes events to multiple independent subscribers. When a customer places an order, the order service publishes a single event to SNS. Three downstream services -- payment processing, inventory management, and customer notification -- each receive a copy via their own SQS queue. Each service processes the event independently at its own pace.

This architecture eliminates tight coupling between services. The order service does not know how many downstream consumers exist or what they do with the event. Adding a fourth consumer (e.g., analytics) is a configuration change -- create a new SQS queue, subscribe it to the topic, and deploy the analytics service. No changes to the order service.

The DVA-C02 exam tests this pattern frequently. Key concepts: (1) each SQS subscriber needs a **queue policy** granting `sqs:SendMessage` to `sns.amazonaws.com` -- without it, messages are silently dropped; (2) **filter policies** route specific event types to specific queues -- payment events go to the payment queue, low-stock alerts go to inventory; (3) **raw message delivery** skips the SNS envelope and delivers only the message body to SQS, reducing parsing complexity.

## The Challenge

Build a fan-out system for an e-commerce order processing pipeline. An SNS topic receives all order events. Three SQS queues subscribe with filter policies:

- **Payment queue** -- receives events where `event_type` = `order_created` or `payment_required`
- **Inventory queue** -- receives events where `event_type` = `order_created` or `inventory_update`
- **Notification queue** -- receives all events (no filter policy)

### Requirements

| Requirement | Description |
|---|---|
| SNS Topic | Single order events topic |
| Payment Queue | Filter: `event_type` in [`order_created`, `payment_required`] |
| Inventory Queue | Filter: `event_type` in [`order_created`, `inventory_update`] |
| Notification Queue | No filter -- receives all events |
| Queue Policies | All three queues must allow SNS to send messages |
| Raw Delivery | Enable raw message delivery on the notification queue |
| Publisher | Go program that publishes events with message attributes |

### Architecture

```
                              +------------------+
                              |  Payment Queue   |
                     filter:  |                  |
                  order_created| Process payment |
               +--payment_req->|                  |
               |              +------------------+
               |
  +---------+  |              +------------------+
  |  SNS    +--+              |  Inventory Queue |
  |  Topic  |     filter:     |                  |
  |         +--order_created->| Update stock     |
  | orders  |  inventory_upd  |                  |
  |         |                 +------------------+
  +---------+
       |                      +------------------+
       |     no filter:       |  Notification Q  |
       +---all events-------->|                  |
                              | Email / push     |
                   raw_delivery| (raw body)       |
                              +------------------+
```

## Hints

<details>
<summary>Hint 1: SQS queue policy for SNS</summary>

Each SQS queue needs a resource-based policy that allows SNS to send messages. Without this, `sns:Publish` succeeds but the message never reaches the queue.

```hcl
data "aws_iam_policy_document" "payment_queue_policy" {
  statement {
    sid       = "AllowSNSSendMessage"
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment.arn]

    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }

    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "payment" {
  queue_url = aws_sqs_queue.payment.id
  policy    = data.aws_iam_policy_document.payment_queue_policy.json
}
```

The `ArnEquals` condition restricts which SNS topic can send to this queue, preventing unauthorized message injection.

</details>

<details>
<summary>Hint 2: Filter policies for selective routing</summary>

Filter policies match on message attributes (not the message body by default):

```hcl
resource "aws_sns_topic_subscription" "payment" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment.arn

  filter_policy = jsonencode({
    event_type = ["order_created", "payment_required"]
  })
}
```

The notification queue has no filter policy, so it receives all messages:

```hcl
resource "aws_sns_topic_subscription" "notification" {
  topic_arn            = aws_sns_topic.orders.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.notification.arn
  raw_message_delivery = true
  # No filter_policy = receives everything
}
```

</details>

<details>
<summary>Hint 3: Raw message delivery</summary>

By default, SNS wraps the message in an envelope containing metadata (TopicArn, MessageId, Timestamp, etc.). With `raw_message_delivery = true`, only the raw message body is delivered to SQS:

Default delivery (envelope):
```json
{
  "Type": "Notification",
  "MessageId": "abc-123",
  "TopicArn": "arn:aws:sns:us-east-1:...:orders",
  "Message": "{\"order_id\":\"o-001\",\"total\":49.99}",
  "Timestamp": "2025-01-01T00:00:00Z"
}
```

Raw delivery (just the body):
```json
{"order_id":"o-001","total":49.99}
```

Raw delivery simplifies parsing but loses SNS metadata. Use it when the subscriber only needs the message content.

</details>

<details>
<summary>Hint 4: Go publisher with message attributes</summary>

```go
package main

import (
    "context"
    "encoding/json"
    "fmt"
    "log"

    "github.com/aws/aws-sdk-go-v2/aws"
    "github.com/aws/aws-sdk-go-v2/config"
    "github.com/aws/aws-sdk-go-v2/service/sns"
    "github.com/aws/aws-sdk-go-v2/service/sns/types"
)

func publishOrderEvent(ctx context.Context, client *sns.Client, topicARN string,
    eventType string, payload map[string]interface{}) error {

    body, _ := json.Marshal(payload)

    _, err := client.Publish(ctx, &sns.PublishInput{
        TopicArn: aws.String(topicARN),
        Message:  aws.String(string(body)),
        MessageAttributes: map[string]types.MessageAttributeValue{
            "event_type": {
                DataType:    aws.String("String"),
                StringValue: aws.String(eventType),
            },
        },
    })
    return err
}
```

The `event_type` message attribute is what filter policies match against. If you forget to include it, filtered subscriptions will not receive the message.

</details>

<details>
<summary>Hint 5: Verifying fan-out delivery</summary>

After publishing, check each queue to confirm correct routing:

```bash
TOPIC_ARN=$(terraform output -raw topic_arn)

# Publish an order_created event (should go to payment, inventory, and notification)
aws sns publish --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"o-001","total":49.99}' \
  --message-attributes '{"event_type":{"DataType":"String","StringValue":"order_created"}}'

# Publish a payment_required event (should go to payment and notification only)
aws sns publish --topic-arn "$TOPIC_ARN" \
  --message '{"order_id":"o-001","amount":49.99}' \
  --message-attributes '{"event_type":{"DataType":"String","StringValue":"payment_required"}}'

sleep 3

# Check each queue
for queue in payment inventory notification; do
  URL=$(terraform output -raw ${queue}_queue_url)
  COUNT=$(aws sqs get-queue-attributes --queue-url "$URL" \
    --attribute-names ApproximateNumberOfMessages \
    --query "Attributes.ApproximateNumberOfMessages" --output text)
  echo "$queue queue: $COUNT messages"
done
```

Expected: payment=2, inventory=1, notification=2.

</details>

## Spot the Bug

A developer deploys the fan-out architecture. Publishing to SNS succeeds (MessageId returned), but the payment SQS queue is always empty. The inventory and notification queues receive messages correctly.

```hcl
resource "aws_sqs_queue" "payment" {
  name = "fanout-demo-payment"
}

# Queue policy for inventory
resource "aws_sqs_queue_policy" "inventory" {
  queue_url = aws_sqs_queue.inventory.id
  policy    = data.aws_iam_policy_document.inventory_policy.json
}

# Queue policy for notification
resource "aws_sqs_queue_policy" "notification" {
  queue_url = aws_sqs_queue.notification.id
  policy    = data.aws_iam_policy_document.notification_policy.json
}

# NOTE: No queue policy for the payment queue!

resource "aws_sns_topic_subscription" "payment" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment.arn
  filter_policy = jsonencode({
    event_type = ["order_created", "payment_required"]
  })
}
```

<details>
<summary>Explain the bug</summary>

The payment queue is **missing its SQS queue policy**. Without a policy granting `sqs:SendMessage` to `sns.amazonaws.com`, SNS cannot deliver messages to that queue. The subscription is created successfully, SNS attempts delivery, but SQS denies the request. Messages are **silently dropped** -- no error is returned to the publisher.

This is particularly insidious because:
1. `sns:Publish` returns a MessageId (suggesting success)
2. The subscription shows as `Confirmed` in the console
3. The other queues work fine (they have policies)
4. No error appears in CloudWatch Logs (SNS delivery failure is an internal SNS concern)

**Fix -- add the missing queue policy:**

```hcl
data "aws_iam_policy_document" "payment_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "payment" {
  queue_url = aws_sqs_queue.payment.id
  policy    = data.aws_iam_policy_document.payment_policy.json
}
```

To diagnose this in production, check the SNS delivery status logging (NumberOfNotificationsDelivered vs NumberOfNotificationsFailed metrics in CloudWatch). On the exam, "SNS publish succeeds but SQS subscriber doesn't receive messages" almost always means a missing queue policy.

</details>

## Verify What You Learned

```bash
# Verify all three subscriptions exist
aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw topic_arn) \
  --query "Subscriptions[*].{Protocol:Protocol,Endpoint:Endpoint}" --output table
```

Expected: three sqs subscriptions.

```bash
# Verify filter policies
for sub_arn in $(aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw topic_arn) \
  --query "Subscriptions[*].SubscriptionArn" --output text); do
  echo "=== $sub_arn ==="
  aws sns get-subscription-attributes --subscription-arn "$sub_arn" \
    --query "Attributes.{FilterPolicy:FilterPolicy,RawDelivery:RawMessageDelivery}" --output json
done
```

Expected: two subscriptions with filter policies, one without; one with raw delivery enabled.

```bash
# Test fan-out routing
TOPIC_ARN=$(terraform output -raw topic_arn)

aws sns publish --topic-arn "$TOPIC_ARN" \
  --message '{"test":"inventory_only"}' \
  --message-attributes '{"event_type":{"DataType":"String","StringValue":"inventory_update"}}'

sleep 3

PAYMENT_URL=$(terraform output -raw payment_queue_url)
INVENTORY_URL=$(terraform output -raw inventory_queue_url)
NOTIFICATION_URL=$(terraform output -raw notification_queue_url)

echo "Payment: $(aws sqs get-queue-attributes --queue-url "$PAYMENT_URL" \
  --attribute-names ApproximateNumberOfMessages --query 'Attributes.ApproximateNumberOfMessages' --output text)"
echo "Inventory: $(aws sqs get-queue-attributes --queue-url "$INVENTORY_URL" \
  --attribute-names ApproximateNumberOfMessages --query 'Attributes.ApproximateNumberOfMessages' --output text)"
echo "Notification: $(aws sqs get-queue-attributes --queue-url "$NOTIFICATION_URL" \
  --attribute-names ApproximateNumberOfMessages --query 'Attributes.ApproximateNumberOfMessages' --output text)"
```

Expected: Payment=0, Inventory=1 (or more), Notification=1 (or more).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

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

You built a fan-out architecture with SNS distributing events to multiple SQS subscribers with filter policies. In the next exercise, you will configure **EventBridge archive and replay** -- storing events for later re-processing and replaying them to recover from processing failures.

## Summary

- The **fan-out pattern** uses SNS to distribute events to multiple SQS subscribers, each processing independently
- Every SQS subscriber needs a **queue policy** granting `sqs:SendMessage` to `sns.amazonaws.com` -- missing policies cause silent message drops
- **Filter policies** route specific event types to specific queues based on message attributes
- Subscriptions **without** filter policies receive **all** messages published to the topic
- **Raw message delivery** (`raw_message_delivery = true`) skips the SNS envelope and delivers only the message body
- Publishing returns a `MessageId` even if downstream delivery fails -- it only confirms SNS received the message
- Adding a new consumer is a **configuration change**: create queue, add policy, subscribe -- no publisher changes needed
- Use `NumberOfNotificationsDelivered` and `NumberOfNotificationsFailed` CloudWatch metrics to monitor delivery health

## Reference

- [SNS Fan-Out Pattern](https://docs.aws.amazon.com/sns/latest/dg/sns-common-scenarios.html)
- [SNS Raw Message Delivery](https://docs.aws.amazon.com/sns/latest/dg/sns-large-payload-raw-message-delivery.html)
- [Terraform aws_sns_topic_subscription](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic_subscription)
- [Terraform aws_sqs_queue_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_policy)

## Additional Resources

- [SNS Delivery Status Logging](https://docs.aws.amazon.com/sns/latest/dg/sns-topic-attributes.html) -- configuring delivery status logging to diagnose failed deliveries
- [SNS SQS Subscription Best Practices](https://docs.aws.amazon.com/sns/latest/dg/subscribe-sqs-queue-to-sns-topic.html) -- queue policy requirements and cross-account considerations
- [Fan-Out Architecture Patterns](https://aws.amazon.com/blogs/compute/enriching-event-driven-architectures-with-aws-event-fork-pipelines/) -- advanced patterns including event forks and replays
- [SNS Message Delivery Retries](https://docs.aws.amazon.com/sns/latest/dg/sns-message-delivery-retries.html) -- how SNS retries failed SQS deliveries

<details>
<summary>Full Solution</summary>

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
  default     = "fanout-demo"
}
```

### `events.tf`

```hcl
resource "aws_sns_topic" "orders" {
  name = "${var.project_name}-orders"
}

resource "aws_sqs_queue" "payment" {
  name = "${var.project_name}-payment"
}

resource "aws_sqs_queue" "inventory" {
  name = "${var.project_name}-inventory"
}

resource "aws_sqs_queue" "notification" {
  name = "${var.project_name}-notification"
}

# -- Queue Policies (grant SNS permission to send) --

data "aws_iam_policy_document" "payment_queue_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payment.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "payment" {
  queue_url = aws_sqs_queue.payment.id
  policy    = data.aws_iam_policy_document.payment_queue_policy.json
}

data "aws_iam_policy_document" "inventory_queue_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.inventory.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "inventory" {
  queue_url = aws_sqs_queue.inventory.id
  policy    = data.aws_iam_policy_document.inventory_queue_policy.json
}

data "aws_iam_policy_document" "notification_queue_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.notification.arn]
    principals {
      type        = "Service"
      identifiers = ["sns.amazonaws.com"]
    }
    condition {
      test     = "ArnEquals"
      variable = "aws:SourceArn"
      values   = [aws_sns_topic.orders.arn]
    }
  }
}

resource "aws_sqs_queue_policy" "notification" {
  queue_url = aws_sqs_queue.notification.id
  policy    = data.aws_iam_policy_document.notification_queue_policy.json
}

# -- Subscriptions --

resource "aws_sns_topic_subscription" "payment" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payment.arn

  filter_policy = jsonencode({
    event_type = ["order_created", "payment_required"]
  })
}

resource "aws_sns_topic_subscription" "inventory" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.inventory.arn

  filter_policy = jsonencode({
    event_type = ["order_created", "inventory_update"]
  })
}

resource "aws_sns_topic_subscription" "notification" {
  topic_arn            = aws_sns_topic.orders.arn
  protocol             = "sqs"
  endpoint             = aws_sqs_queue.notification.arn
  raw_message_delivery = true
}
```

### `outputs.tf`

```hcl
output "topic_arn" {
  value = aws_sns_topic.orders.arn
}

output "payment_queue_url" {
  value = aws_sqs_queue.payment.url
}

output "inventory_queue_url" {
  value = aws_sqs_queue.inventory.url
}

output "notification_queue_url" {
  value = aws_sqs_queue.notification.url
}
```

</details>
