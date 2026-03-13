# SQS Decoupling and Auto Scaling Integration

<!--
difficulty: intermediate
concepts: [sqs-standard, sqs-fifo, message-group-id, dead-letter-queue, redrive-policy, sqs-based-scaling, approximate-messages-visible]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** SQS free tier includes 1M requests/month. Lambda free tier includes 1M invocations/month. ASG t3.micro instances (~$0.0104/hr each). Total ~$0.05/hr during active testing. Destroy resources promptly after completing the exercise.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC available in target region | `aws ec2 describe-vpcs --filters Name=isDefault,Values=true` |
| jq installed for JSON parsing | `jq --version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** decoupled architectures using SQS queues with dead-letter queue paths for failure isolation.
2. **Justify** the choice between SQS Standard and FIFO queues based on ordering, throughput, and deduplication requirements.
3. **Implement** queue-depth-based auto scaling that provisions consumers proportional to backlog.
4. **Compare** SQS Standard (at-least-once, unlimited throughput) vs FIFO (exactly-once, 300 msg/s) trade-offs with concrete metrics.
5. **Evaluate** redrive policies and their impact on message lifecycle and operational visibility.

---

## Why This Matters

SQS is the foundational decoupling service on AWS and one of the most tested topics on the SAA-C03. The exam presents tightly coupled architectures -- synchronous API calls between services, shared databases, direct service-to-service HTTP -- and asks you to identify the decoupling mechanism that improves resilience. SQS is almost always the correct answer for asynchronous workloads, but the exam goes deeper: it asks whether you need Standard or FIFO, how to handle poison messages with dead-letter queues, and how to scale consumers based on queue depth rather than CPU utilization.

The queue-depth scaling pattern is particularly important because it represents a fundamentally different way of thinking about auto scaling. In Exercise 04, you scaled based on CPU utilization -- a reactive metric. Queue-depth scaling is proactive: the number of messages waiting tells you how far behind your consumers are, and you can scale before latency degrades. The "backlog per instance" metric (messages visible divided by number of consumers) is the key formula the exam tests. If you have 10,000 messages and 5 consumers, each consumer has a backlog of 2,000 -- and if each consumer processes 100 messages per minute, your backlog will take 20 minutes to clear. This exercise makes that math concrete.

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
  default     = "saa-ex07"
}
```

### `main.tf`

```hcl
# ---------- Data Sources ----------

data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}
```

### `events.tf`

```hcl
# ---------- SQS Standard Queue (Primary) ----------

resource "aws_sqs_queue" "primary" {
  name                       = "${var.project_name}-orders"
  delay_seconds              = 0
  max_message_size           = 262144
  message_retention_seconds  = 345600  # 4 days
  receive_wait_time_seconds  = 20      # long polling
  visibility_timeout_seconds = 60

  # ============================================================
  # TODO 1: Configure Dead Letter Queue with Redrive Policy
  # ============================================================
  # Add a redrive_policy that sends messages to the DLQ after
  # 3 failed processing attempts.
  #
  # Requirements:
  #   - Create an aws_sqs_queue resource for the DLQ named
  #     "${var.project_name}-orders-dlq"
  #   - Set message_retention_seconds = 1209600 (14 days) on DLQ
  #   - Add redrive_policy to THIS queue (primary):
  #     redrive_policy = jsonencode({
  #       deadLetterTargetArn = <DLQ ARN>
  #       maxReceiveCount     = 3
  #     })
  #   - maxReceiveCount = 3 means: if a message is received 3 times
  #     without being deleted (acknowledged), move it to the DLQ
  #
  # Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue
  # ============================================================
}

# ============================================================
# TODO 2: FIFO Queue for Ordered Processing
# ============================================================
# Create a FIFO queue path for order processing that requires
# strict ordering within each customer's orders.
#
# Requirements:
#   - Resource: aws_sqs_queue
#   - name MUST end in ".fifo" (e.g., "${var.project_name}-orders-fifo.fifo")
#   - fifo_queue = true
#   - content_based_deduplication = true
#   - deduplication_scope = "messageGroup"
#   - fifo_throughput_limit = "perMessageGroupId"
#   - Create a matching DLQ with ".fifo" suffix
#   - Add redrive_policy pointing to the FIFO DLQ
#
# Key concept: message_group_id (set per message, not per queue)
# determines ordering scope. Messages with the same group ID
# are processed in order. Different group IDs process in parallel.
#
# Example: Use customer_id as the message group ID. Orders for
# customer A are processed in order. Orders for customer B are
# also in order. But A and B are independent of each other.
#
# Docs: https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/FIFO-queues.html
# ============================================================
```

### `lambda.tf`

```hcl
resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-lambda-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy" "lambda_sqs" {
  name = "sqs-consumer-policy"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect = "Allow"
        Action = [
          "sqs:ReceiveMessage",
          "sqs:DeleteMessage",
          "sqs:GetQueueAttributes"
        ]
        Resource = [
          aws_sqs_queue.primary.arn
        ]
      },
      {
        Effect   = "Allow"
        Action   = "logs:*"
        Resource = "arn:aws:logs:*:*:*"
      }
    ]
  })
}

data "archive_file" "consumer" {
  type        = "zip"
  output_path = "${path.module}/consumer.zip"

  source {
    content = <<-GO
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.SQSEvent) error {
	for _, record := range event.Records {
		var body map[string]interface{}
		if err := json.Unmarshal([]byte(record.Body), &body); err != nil {
			return fmt.Errorf("failed to parse message body: %w", err)
		}

		bodyJSON, _ := json.Marshal(body)
		fmt.Printf("Processing order: %s\n", string(bodyJSON))

		// Simulate processing time
		time.Sleep(time.Duration(100+rand.Intn(400)) * time.Millisecond)

		// Simulate occasional failure (10% chance)
		if rand.Float64() < 0.1 {
			orderID, _ := body["order_id"].(string)
			if orderID == "" {
				orderID = "unknown"
			}
			return fmt.Errorf("failed to process order %s", orderID)
		}

		orderID, _ := body["order_id"].(string)
		if orderID == "" {
			orderID = "unknown"
		}
		fmt.Printf("Successfully processed order: %s\n", orderID)
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
    GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "consumer" {
  function_name    = "${var.project_name}-consumer"
  filename         = data.archive_file.consumer.output_path
  source_code_hash = data.archive_file.consumer.output_base64sha256
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 60
  memory_size      = 128
}

resource "aws_lambda_event_source_mapping" "sqs_trigger" {
  event_source_arn = aws_sqs_queue.primary.arn
  function_name    = aws_lambda_function.consumer.arn
  batch_size       = 10
  enabled          = true
}
```

### `monitoring.tf`

```hcl
# ============================================================
# TODO 3: CloudWatch Alarm on Queue Depth
# ============================================================
# Create a CloudWatch alarm that fires when the queue has too
# many messages waiting (indicating consumers are falling behind).
#
# Requirements:
#   - Resource: aws_cloudwatch_metric_alarm
#   - alarm_name = "${var.project_name}-queue-depth-alarm"
#   - metric_name = "ApproximateNumberOfMessagesVisible"
#   - namespace = "AWS/SQS"
#   - comparison_operator = "GreaterThanThreshold"
#   - threshold = 1000
#   - evaluation_periods = 3
#   - period = 60
#   - statistic = "Average"
#   - dimensions = { QueueName = queue name }
#   - alarm_description explaining the operational response
#
# This metric is the backbone of queue-depth scaling. When
# messages accumulate faster than consumers process them,
# the backlog grows and this alarm fires.
#
# Docs: https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-available-cloudwatch-metrics.html
# ============================================================
```

### `compute.tf`

```hcl
# ---------- ASG Consumer Path (for queue-depth scaling) ----------

resource "aws_iam_role" "asg_instance" {
  name = "${var.project_name}-asg-instance-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "asg_ssm" {
  role       = aws_iam_role.asg_instance.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy" "asg_sqs" {
  name = "sqs-consumer"
  role = aws_iam_role.asg_instance.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"
      Action = [
        "sqs:ReceiveMessage",
        "sqs:DeleteMessage",
        "sqs:GetQueueAttributes",
        "sqs:GetQueueUrl"
      ]
      Resource = aws_sqs_queue.primary.arn
    }]
  })
}

resource "aws_iam_instance_profile" "asg_instance" {
  name = "${var.project_name}-asg-instance-profile"
  role = aws_iam_role.asg_instance.name
}

resource "aws_security_group" "asg_instance" {
  name   = "${var.project_name}-asg-sg"
  vpc_id = data.aws_vpc.default.id

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_launch_template" "consumer" {
  name_prefix   = "${var.project_name}-consumer-lt-"
  image_id      = data.aws_ami.amazon_linux.id
  instance_type = "t3.micro"

  iam_instance_profile {
    name = aws_iam_instance_profile.asg_instance.name
  }

  vpc_security_group_ids = [aws_security_group.asg_instance.id]
}

resource "aws_autoscaling_group" "consumers" {
  name                = "${var.project_name}-consumers"
  desired_capacity    = 1
  min_size            = 1
  max_size            = 5
  vpc_zone_identifier = data.aws_subnets.default.ids

  launch_template {
    id      = aws_launch_template.consumer.id
    version = "$Latest"
  }

  tag {
    key                 = "Name"
    value               = "${var.project_name}-consumer"
    propagate_at_launch = true
  }
}

# ============================================================
# TODO 4: ASG Scaling Policy Based on Queue Depth
# ============================================================
# Create a target tracking scaling policy that scales the ASG
# based on the "backlog per instance" metric.
#
# The backlog per instance = ApproximateNumberOfMessagesVisible
# divided by the number of running instances in the ASG.
#
# Requirements:
#   - Resource: aws_autoscaling_policy
#   - policy_type = "TargetTrackingScaling"
#   - target_tracking_configuration with customized_metric_specification:
#     - Use a CloudWatch math expression to compute
#       messages_visible / group_in_service_instances
#     - metric_name and namespace for each component metric
#     - Target value: if each instance processes 100 msg/min,
#       and acceptable latency is 5 min, target = 500 (backlog)
#
# Alternative simpler approach (if math expressions are complex):
#   - Use a step scaling policy instead:
#     - < 100 messages: 1 instance (min)
#     - 100-500 messages: 2 instances
#     - 500-2000 messages: 4 instances
#     - > 2000 messages: 5 instances (max)
#
# Docs: https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-using-sqs-queue.html
# ============================================================


# ============================================================
# TODO 5: Redrive Allow Policy on DLQ
# ============================================================
# Configure the DLQ to only accept messages from the primary
# queue (prevent other queues from using it as their DLQ).
#
# Requirements:
#   - Resource: aws_sqs_queue_redrive_allow_policy
#   - queue_url = DLQ URL
#   - redrive_allow_policy = jsonencode({
#       redrivePermission = "byQueue"
#       sourceQueueArns   = [primary queue ARN]
#     })
#
# This prevents accidental misconfiguration where someone
# points a different queue's redrive at your DLQ, mixing
# failure messages and making debugging impossible.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_redrive_allow_policy
# ============================================================
```

### `outputs.tf`

```hcl
output "queue_url" {
  value = aws_sqs_queue.primary.url
}

output "queue_arn" {
  value = aws_sqs_queue.primary.arn
}

output "lambda_function_name" {
  value = aws_lambda_function.consumer.function_name
}

output "asg_name" {
  value = aws_autoscaling_group.consumers.name
}
```

---

## Spot the Bug

The following Terraform configuration creates a FIFO queue but will fail on `terraform apply`. Identify the problem before expanding the answer.

```hcl
resource "aws_sqs_queue" "orders_fifo" {
  name                        = "orders-processing-queue"
  fifo_queue                  = true
  content_based_deduplication = true
  visibility_timeout_seconds  = 60
  message_retention_seconds   = 345600
}
```

<details>
<summary>Explain the bug</summary>

The queue name does not end in `.fifo`. AWS requires all FIFO queue names to have the `.fifo` suffix. Without it, the API returns `InvalidParameterValue` and Terraform apply fails with an error like:

```
Error: creating SQS Queue (orders-processing-queue): InvalidParameterValue:
  Can only set attribute FifoQueue on a queue with name ending in '.fifo'.
```

**The fix:**

```hcl
resource "aws_sqs_queue" "orders_fifo" {
  name                        = "orders-processing-queue.fifo"  # Must end in .fifo
  fifo_queue                  = true
  content_based_deduplication = true
  visibility_timeout_seconds  = 60
  message_retention_seconds   = 345600
}
```

This is a common exam distractor. The SAA-C03 may present a scenario where a FIFO queue is "not working" and the answer is simply that the name does not follow the `.fifo` naming convention. It is also a frequent real-world gotcha when renaming queues or generating names dynamically.

**Additional FIFO constraints to remember:**
- Maximum 300 messages/second without batching (3,000 with batching and high throughput mode)
- Message group IDs are required for ordering -- without them, FIFO ordering is across the entire queue
- FIFO queues do not support per-message delays (only queue-level `delay_seconds`)

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Send test messages to the Standard queue:**
   ```bash
   QUEUE_URL=$(terraform output -raw queue_url)

   for i in $(seq 1 20); do
     aws sqs send-message \
       --queue-url "$QUEUE_URL" \
       --message-body "{\"order_id\": \"ORD-$i\", \"customer\": \"CUST-$((i % 5))\", \"amount\": $((RANDOM % 500 + 10))}"
   done
   ```

3. **Check queue attributes (messages visible, in flight):**
   ```bash
   aws sqs get-queue-attributes \
     --queue-url "$QUEUE_URL" \
     --attribute-names All \
     --query 'Attributes.{Visible:ApproximateNumberOfMessages,InFlight:ApproximateNumberOfMessagesNotVisible,Delayed:ApproximateNumberOfMessagesDelayed}'
   ```

4. **Watch Lambda process messages:**
   ```bash
   FUNCTION=$(terraform output -raw lambda_function_name)
   aws logs tail "/aws/lambda/$FUNCTION" --follow --since 5m
   ```

5. **Check DLQ for failed messages (after Lambda retries):**
   ```bash
   DLQ_URL=$(aws sqs get-queue-url --queue-name saa-ex07-orders-dlq --query 'QueueUrl' --output text)
   aws sqs get-queue-attributes \
     --queue-url "$DLQ_URL" \
     --attribute-names ApproximateNumberOfMessages
   ```

6. **Send messages to FIFO queue with message group IDs:**
   ```bash
   FIFO_URL=$(aws sqs get-queue-url --queue-name saa-ex07-orders-fifo.fifo --query 'QueueUrl' --output text)

   for i in $(seq 1 10); do
     CUSTOMER="CUST-$((i % 3))"
     aws sqs send-message \
       --queue-url "$FIFO_URL" \
       --message-body "{\"order_id\": \"FIFO-$i\", \"customer\": \"$CUSTOMER\"}" \
       --message-group-id "$CUSTOMER"
   done
   ```

7. **Flood the queue to trigger scaling:**
   ```bash
   for i in $(seq 1 500); do
     aws sqs send-message \
       --queue-url "$QUEUE_URL" \
       --message-body "{\"order_id\": \"FLOOD-$i\", \"amount\": $i}" &
   done
   wait
   echo "Sent 500 messages"

   # Watch queue depth
   watch -n 5 "aws sqs get-queue-attributes --queue-url '$QUEUE_URL' \
     --attribute-names ApproximateNumberOfMessages --output text"
   ```

---

## Standard vs FIFO Decision Framework

| Criterion | SQS Standard | SQS FIFO |
|---|---|---|
| **Ordering** | Best-effort | Strict (per message group) |
| **Delivery** | At-least-once (rare duplicates) | Exactly-once |
| **Throughput** | Unlimited | 300 msg/s (3,000 with batching) |
| **Deduplication** | None (application must handle) | 5-minute dedup window |
| **Use cases** | Log processing, fan-out, async tasks | Financial transactions, inventory |
| **Cost** | $0.40/1M requests | $0.50/1M requests |
| **DLQ support** | Yes | Yes (DLQ must also be FIFO) |

### Queue-Depth Scaling Formula

```
backlog_per_instance = ApproximateNumberOfMessagesVisible / GroupInServiceInstances

acceptable_backlog = acceptable_latency_seconds / avg_processing_time_per_message

target_instances = messages_visible / acceptable_backlog
```

**Example:** 10,000 messages visible, each takes 0.5s to process, acceptable latency is 5 minutes (300s):
- Acceptable backlog per instance = 300 / 0.5 = 600 messages
- Target instances = 10,000 / 600 = 17 instances

---

## Solutions

<details>
<summary>events.tf -- TODO 1: Dead Letter Queue with Redrive Policy</summary>

Create the DLQ resource:

```hcl
resource "aws_sqs_queue" "dlq" {
  name                      = "${var.project_name}-orders-dlq"
  message_retention_seconds = 1209600  # 14 days
}
```

Add the redrive policy to the primary queue:

```hcl
resource "aws_sqs_queue" "primary" {
  name                       = "${var.project_name}-orders"
  delay_seconds              = 0
  max_message_size           = 262144
  message_retention_seconds  = 345600
  receive_wait_time_seconds  = 20
  visibility_timeout_seconds = 60

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.dlq.arn
    maxReceiveCount     = 3
  })
}
```

Key design decisions:
- `maxReceiveCount = 3`: a message gets 3 attempts before going to DLQ. Lower values catch failures faster but may discard messages that would succeed on retry. Higher values delay failure detection.
- DLQ retention of 14 days (maximum) gives the operations team time to investigate and redrive failed messages.
- The DLQ `visibility_timeout_seconds` should match or exceed the primary queue's value.

</details>

<details>
<summary>events.tf -- TODO 2: FIFO Queue for Ordered Processing</summary>

```hcl
resource "aws_sqs_queue" "fifo_dlq" {
  name                      = "${var.project_name}-orders-fifo-dlq.fifo"
  fifo_queue                = true
  message_retention_seconds = 1209600
}

resource "aws_sqs_queue" "fifo" {
  name                        = "${var.project_name}-orders-fifo.fifo"
  fifo_queue                  = true
  content_based_deduplication = true
  deduplication_scope         = "messageGroup"
  fifo_throughput_limit       = "perMessageGroupId"
  visibility_timeout_seconds  = 60
  message_retention_seconds   = 345600
  receive_wait_time_seconds   = 20

  redrive_policy = jsonencode({
    deadLetterTargetArn = aws_sqs_queue.fifo_dlq.arn
    maxReceiveCount     = 3
  })
}
```

Configuration choices explained:
- `content_based_deduplication = true`: SQS generates the dedup ID from a SHA-256 hash of the message body. Alternative: set `MessageDeduplicationId` per message.
- `deduplication_scope = "messageGroup"`: deduplication applies within each message group, not across the entire queue.
- `fifo_throughput_limit = "perMessageGroupId"`: enables high throughput mode (3,000 msg/s with batching) with the throughput limit applied per message group rather than per queue.

</details>

<details>
<summary>monitoring.tf -- TODO 3: CloudWatch Alarm on Queue Depth</summary>

```hcl
resource "aws_cloudwatch_metric_alarm" "queue_depth" {
  alarm_name          = "${var.project_name}-queue-depth-alarm"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 3
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Average"
  threshold           = 1000
  alarm_description   = "Queue backlog exceeds 1000 messages. Consumers may be failing or under-provisioned. Check DLQ for poison messages and verify consumer health."

  dimensions = {
    QueueName = aws_sqs_queue.primary.name
  }
}
```

Also consider a DLQ depth alarm to catch persistent failures:

```hcl
resource "aws_cloudwatch_metric_alarm" "dlq_not_empty" {
  alarm_name          = "${var.project_name}-dlq-not-empty"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Sum"
  threshold           = 0
  alarm_description   = "DLQ has messages. Investigate poison messages and consider redrive after fixing the root cause."

  dimensions = {
    QueueName = aws_sqs_queue.dlq.name
  }
}
```

</details>

<details>
<summary>compute.tf -- TODO 4: ASG Scaling Policy Based on Queue Depth</summary>

Using a step scaling approach (simpler than custom metric math):

```hcl
resource "aws_autoscaling_policy" "queue_depth_scaling" {
  name                   = "${var.project_name}-queue-depth-scaling"
  autoscaling_group_name = aws_autoscaling_group.consumers.name
  policy_type            = "StepScaling"
  adjustment_type        = "ExactCapacity"

  step_adjustment {
    metric_interval_lower_bound = 0
    metric_interval_upper_bound = 400
    scaling_adjustment          = 2
  }

  step_adjustment {
    metric_interval_lower_bound = 400
    metric_interval_upper_bound = 1900
    scaling_adjustment          = 4
  }

  step_adjustment {
    metric_interval_lower_bound = 1900
    scaling_adjustment          = 5
  }
}

resource "aws_cloudwatch_metric_alarm" "queue_scaling_trigger" {
  alarm_name          = "${var.project_name}-queue-scaling-trigger"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 2
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Average"
  threshold           = 100
  alarm_actions       = [aws_autoscaling_policy.queue_depth_scaling.arn]

  dimensions = {
    QueueName = aws_sqs_queue.primary.name
  }
}

resource "aws_autoscaling_policy" "queue_empty_scale_in" {
  name                   = "${var.project_name}-queue-empty-scale-in"
  autoscaling_group_name = aws_autoscaling_group.consumers.name
  policy_type            = "StepScaling"
  adjustment_type        = "ExactCapacity"

  step_adjustment {
    metric_interval_upper_bound = 0
    scaling_adjustment          = 1
  }
}

resource "aws_cloudwatch_metric_alarm" "queue_empty" {
  alarm_name          = "${var.project_name}-queue-empty"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 5
  metric_name         = "ApproximateNumberOfMessagesVisible"
  namespace           = "AWS/SQS"
  period              = 60
  statistic           = "Average"
  threshold           = 10
  alarm_actions       = [aws_autoscaling_policy.queue_empty_scale_in.arn]

  dimensions = {
    QueueName = aws_sqs_queue.primary.name
  }
}
```

The step bounds are relative to the alarm threshold (100):
- 100-500 messages: 2 instances
- 500-2000 messages: 4 instances
- 2000+ messages: 5 instances (max)
- Below 10 messages for 5 consecutive minutes: scale to 1

</details>

<details>
<summary>events.tf -- TODO 5: Redrive Allow Policy on DLQ</summary>

```hcl
resource "aws_sqs_queue_redrive_allow_policy" "dlq" {
  queue_url = aws_sqs_queue.dlq.id

  redrive_allow_policy = jsonencode({
    redrivePermission = "byQueue"
    sourceQueueArns   = [aws_sqs_queue.primary.arn]
  })
}
```

The three permission modes:
- `"allowAll"`: any queue can use this DLQ (default, least secure)
- `"byQueue"`: only specified source queues can redrive to this DLQ
- `"denyAll"`: no queue can use this as a DLQ

Always use `"byQueue"` in production to prevent cross-application message mixing.

</details>

---

## Cleanup

```bash
# Purge queues before destroying (speeds up deletion)
QUEUE_URL=$(terraform output -raw queue_url)
aws sqs purge-queue --queue-url "$QUEUE_URL" 2>/dev/null || true

terraform destroy -auto-approve
```

Verify queues are deleted:

```bash
aws sqs get-queue-url --queue-name saa-ex07-orders 2>/dev/null && echo "Queue still exists!" || echo "Queue deleted"
```

---

## What's Next

Exercise 08 compares RDS PostgreSQL and Aurora PostgreSQL side by side. You will deploy both engines in the same VPC, run identical benchmarks with pgbench, and build a decision framework based on storage model, replication lag, failover time, and cost. The decoupling patterns from this exercise apply directly to database architecture -- Aurora's read replicas serve a similar "distribute the workload" function as SQS consumers.

---

## Summary

You built a decoupled processing pipeline with SQS Standard and FIFO queues, implemented dead-letter queue patterns for failure isolation, created CloudWatch alarms on queue depth for operational visibility, and configured ASG scaling policies driven by queue backlog. The key architectural insight is that queue-depth scaling fundamentally changes how you think about capacity: instead of reacting to CPU (a lagging indicator), you respond to backlog (a leading indicator). For the SAA-C03, remember the Standard vs FIFO decision matrix, the `.fifo` naming requirement, and the backlog-per-instance formula for scaling.

---

## Reference

- [SQS Developer Guide](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/welcome.html)
- [SQS FIFO Queues](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/FIFO-queues.html)
- [SQS Dead Letter Queues](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-dead-letter-queues.html)
- [Scaling Based on SQS](https://docs.aws.amazon.com/autoscaling/ec2/userguide/as-using-sqs-queue.html)
- [SQS CloudWatch Metrics](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-available-cloudwatch-metrics.html)

## Additional Resources

- [Terraform aws_sqs_queue](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue)
- [Terraform aws_sqs_queue_redrive_allow_policy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/sqs_queue_redrive_allow_policy)
- [SQS Best Practices](https://docs.aws.amazon.com/AWSSimpleQueueService/latest/SQSDeveloperGuide/sqs-best-practices.html)
- [Lambda with SQS Event Source](https://docs.aws.amazon.com/lambda/latest/dg/with-sqs.html)
