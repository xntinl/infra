# 52. SNS FIFO with SQS FIFO for Strictly Ordered Pub/Sub

<!--
difficulty: intermediate
concepts: [sns-fifo, sqs-fifo, message-group-id, message-deduplication, content-based-dedup, fifo-naming, ordered-messaging]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply
prerequisites: [47-sns-topics-subscriptions-filtering, 48-sns-message-filtering-policies]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates an SNS FIFO topic and SQS FIFO queues. FIFO pricing is $0.50 per million SNS requests and $0.40 per million SQS requests. Cost is approximately $0.01/hr for testing. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** an SNS FIFO topic and SQS FIFO queue subscription for strictly ordered message delivery
- **Apply** message group IDs to partition ordered message streams within a FIFO topic
- **Differentiate** between content-based deduplication and explicit deduplication IDs
- **Explain** the `.fifo` suffix naming requirement for FIFO topics and queues
- **Verify** that messages within a message group are delivered in the exact order they were published

## Why SNS FIFO with SQS FIFO

Standard SNS topics provide at-least-once delivery with best-effort ordering. For many use cases (notifications, alerts), this is sufficient. But when processing financial transactions, inventory updates, or state machine transitions, message ordering is critical. Processing an "order shipped" event before "order created" causes data corruption.

SNS FIFO topics combined with SQS FIFO queues guarantee strict ordering within a message group and exactly-once processing via deduplication. The DVA-C02 exam tests several specifics: (1) FIFO topic and queue names must end with `.fifo` -- creating one without the suffix fails; (2) every message must include a `MessageGroupId` that determines the ordering partition; (3) deduplication can use content-based hashing (enabled on the topic) or explicit `MessageDeduplicationId` per message -- at least one must be configured; (4) SNS FIFO topics only support SQS FIFO queues as subscribers (not standard SQS, Lambda, or HTTP endpoints).

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
  default     = "fifo-demo"
}
```

### `events.tf`

```hcl
# =======================================================
# TODO 1 -- Create an SNS FIFO Topic (events.tf)
# =======================================================
# Requirements:
#   - Create an aws_sns_topic with fifo_topic = true
#   - Name MUST end with ".fifo" (e.g., "fifo-demo-orders.fifo")
#   - Enable content_based_deduplication = true
#     (uses SHA-256 hash of message body for dedup)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic
# IMPORTANT: The .fifo suffix is mandatory. Without it,
#   Terraform returns InvalidParameterException.


# =======================================================
# TODO 2 -- Create SQS FIFO Queues for Subscribers (events.tf)
# =======================================================
# Requirements:
#   - Create two aws_sqs_queue resources with fifo_queue = true
#   - Names MUST end with ".fifo"
#   - Queue 1: "${var.project_name}-payments.fifo" (payment processor)
#   - Queue 2: "${var.project_name}-inventory.fifo" (inventory updater)
#   - Set content_based_deduplication = true on both


# =======================================================
# TODO 3 -- Create SQS Queue Policies (events.tf)
# =======================================================
# Requirements:
#   - Grant sqs:SendMessage to sns.amazonaws.com for each queue
#   - Condition: aws:SourceArn = the SNS FIFO topic ARN


# =======================================================
# TODO 4 -- Subscribe SQS FIFO Queues to SNS FIFO Topic (events.tf)
# =======================================================
# Requirements:
#   - Create aws_sns_topic_subscription for each queue
#   - Protocol: "sqs"
#   - Endpoint: the queue ARN
#
# Note: SNS FIFO topics ONLY support SQS FIFO queue subscriptions.
# Lambda, HTTP, email, and standard SQS are NOT supported.
```

### `outputs.tf`

```hcl
output "topic_arn" {
  value = aws_sns_topic.orders.arn
}

output "payments_queue_url" {
  value = aws_sqs_queue.payments.url
}

output "inventory_queue_url" {
  value = aws_sqs_queue.inventory.url
}
```

After deploying, publish messages and verify ordering:

```bash
# =======================================================
# TODO 5 -- Publish ordered messages with MessageGroupId
# =======================================================
# Requirements:
#   - Publish 5 messages to the FIFO topic
#   - All messages must include --message-group-id
#   - Use the same group ID for messages that must be ordered
#     (e.g., all events for order-001 use group "order-001")
#   - Content-based dedup is enabled, so each message body
#     must be unique (otherwise it is deduplicated/dropped)
#
# Example:
#   aws sns publish \
#     --topic-arn "$TOPIC_ARN" \
#     --message '{"order_id":"order-001","event":"created","seq":1}' \
#     --message-group-id "order-001"


# =======================================================
# TODO 6 -- Verify message ordering in SQS FIFO
# =======================================================
# Requirements:
#   - Receive messages from each queue
#   - Verify they arrive in the exact publish order
#   - Use --max-number-of-messages 10 to get all at once
#   - Messages within the same MessageGroupId are strictly ordered
```

## Spot the Bug

A developer creates an SNS FIFO topic but the `terraform apply` fails with `InvalidParameterException`.

```hcl
resource "aws_sns_topic" "orders" {
  name       = "order-events"
  fifo_topic = true
  content_based_deduplication = true
}
```

<details>
<summary>Explain the bug</summary>

The topic name `order-events` does not end with `.fifo`. SNS FIFO topic names **must** end with the `.fifo` suffix. This is a hard requirement enforced by the API, not just a convention.

**Fix -- add the `.fifo` suffix:**

```hcl
resource "aws_sns_topic" "orders" {
  name       = "order-events.fifo"
  fifo_topic = true
  content_based_deduplication = true
}
```

The same rule applies to SQS FIFO queues -- their names must also end with `.fifo`.

On the exam, when you see an error about creating a FIFO topic or queue, the first thing to check is the `.fifo` suffix. This is the most commonly tested FIFO gotcha.

</details>

## Verify What You Learned

```bash
# Verify FIFO topic exists
aws sns get-topic-attributes --topic-arn $(terraform output -raw topic_arn) \
  --query "Attributes.{FifoTopic:FifoTopic,ContentBasedDedup:ContentBasedDeduplication}" \
  --output table
```

Expected: `FifoTopic=true`, `ContentBasedDeduplication=true`.

```bash
# Verify subscriptions
aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw topic_arn) \
  --query "Subscriptions[*].{Protocol:Protocol,Endpoint:Endpoint}" --output table
```

Expected: two sqs subscriptions.

```bash
# Publish 3 ordered messages
TOPIC_ARN=$(terraform output -raw topic_arn)
for i in 1 2 3; do
  aws sns publish --topic-arn "$TOPIC_ARN" \
    --message "{\"order_id\":\"order-001\",\"event\":\"step-$i\",\"seq\":$i}" \
    --message-group-id "order-001"
  echo "Published message $i"
done

sleep 3

# Receive and verify order
PAYMENTS_URL=$(terraform output -raw payments_queue_url)
aws sqs receive-message --queue-url "$PAYMENTS_URL" \
  --max-number-of-messages 10 --wait-time-seconds 5 \
  --query "Messages[*].Body" --output text
```

Expected: messages in order (seq 1, 2, 3).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- SNS FIFO Topic</summary>

### `events.tf`

```hcl
resource "aws_sns_topic" "orders" {
  name                        = "${var.project_name}-orders.fifo"
  fifo_topic                  = true
  content_based_deduplication = true
}
```

</details>

<details>
<summary>TODO 2 -- SQS FIFO Queues</summary>

### `events.tf`

```hcl
resource "aws_sqs_queue" "payments" {
  name                        = "${var.project_name}-payments.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
}

resource "aws_sqs_queue" "inventory" {
  name                        = "${var.project_name}-inventory.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
}
```

</details>

<details>
<summary>TODO 3 -- SQS Queue Policies</summary>

### `events.tf`

```hcl
data "aws_iam_policy_document" "payments_policy" {
  statement {
    actions   = ["sqs:SendMessage"]
    resources = [aws_sqs_queue.payments.arn]
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

resource "aws_sqs_queue_policy" "payments" {
  queue_url = aws_sqs_queue.payments.id
  policy    = data.aws_iam_policy_document.payments_policy.json
}

data "aws_iam_policy_document" "inventory_policy" {
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
  policy    = data.aws_iam_policy_document.inventory_policy.json
}
```

</details>

<details>
<summary>TODO 4 -- SNS Subscriptions</summary>

### `events.tf`

```hcl
resource "aws_sns_topic_subscription" "payments" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.payments.arn
}

resource "aws_sns_topic_subscription" "inventory" {
  topic_arn = aws_sns_topic.orders.arn
  protocol  = "sqs"
  endpoint  = aws_sqs_queue.inventory.arn
}
```

</details>

<details>
<summary>TODO 5 + TODO 6 -- Publish and Verify</summary>

```bash
TOPIC_ARN=$(terraform output -raw topic_arn)

# Publish 5 ordered messages for the same order
for i in 1 2 3 4 5; do
  aws sns publish --topic-arn "$TOPIC_ARN" \
    --message "{\"order_id\":\"order-001\",\"event\":\"step-$i\",\"seq\":$i}" \
    --message-group-id "order-001"
done

sleep 5

# Verify ordering in payments queue
PAYMENTS_URL=$(terraform output -raw payments_queue_url)
echo "=== Payments Queue ==="
aws sqs receive-message --queue-url "$PAYMENTS_URL" \
  --max-number-of-messages 10 --wait-time-seconds 5 \
  --query "Messages[*].Body" --output json | \
  jq -r '.[] | fromjson | .Message | fromjson | "seq=\(.seq) event=\(.event)"'

# Verify ordering in inventory queue
INVENTORY_URL=$(terraform output -raw inventory_queue_url)
echo "=== Inventory Queue ==="
aws sqs receive-message --queue-url "$INVENTORY_URL" \
  --max-number-of-messages 10 --wait-time-seconds 5 \
  --query "Messages[*].Body" --output json | \
  jq -r '.[] | fromjson | .Message | fromjson | "seq=\(.seq) event=\(.event)"'
```

Expected: both queues show messages in order (seq 1, 2, 3, 4, 5).

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

You built a strictly ordered pub/sub system with SNS FIFO and SQS FIFO. In the next exercise, you will implement the **request-reply pattern with SQS temporary queues** -- creating virtual queues for correlating requests with responses in a microservices architecture.

## Summary

- **SNS FIFO topics** guarantee strict message ordering within a message group and exactly-once delivery
- Topic and queue names **must** end with `.fifo` -- this is enforced by the API, not optional
- Every published message **must** include a `MessageGroupId` -- ordering is per-group, not per-topic
- **Deduplication** uses either content-based hashing (SHA-256 of body) or explicit `MessageDeduplicationId` -- at least one must be configured
- SNS FIFO topics **only** support SQS FIFO queue subscriptions -- Lambda, HTTP, email, and standard SQS are not supported
- Messages with **different** MessageGroupIds can be processed in parallel; same group ID enforces sequential processing
- FIFO throughput is **300 messages/second per message group** (3,000 with batching and high throughput mode)

## Reference

- [SNS FIFO Topics](https://docs.aws.amazon.com/sns/latest/dg/sns-fifo-topics.html)
- [SQS FIFO Queues](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/FIFO-queues.html)
- [Terraform aws_sns_topic fifo_topic](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sns_topic#fifo_topic)
- [Terraform aws_sqs_queue fifo_queue](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue#fifo_queue)

## Additional Resources

- [FIFO Message Deduplication](https://docs.aws.amazon.com/sns/latest/dg/fifo-message-dedup.html) -- content-based vs explicit deduplication strategies
- [FIFO Throughput Quotas](https://docs.aws.amazon.com/sns/latest/dg/fifo-topic-code-examples.html) -- high throughput mode and batching for increased performance
- [SNS FIFO Fan-Out](https://docs.aws.amazon.com/sns/latest/dg/sns-fifo-topics.html#fifo-topic-fan-out) -- delivering ordered messages to multiple FIFO queues
- [Message Group ID Best Practices](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/using-messagegroupid-property.html) -- choosing granularity for message groups
