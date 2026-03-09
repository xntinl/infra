# 53. S3 Event Notifications to Lambda

<!--
difficulty: intermediate
concepts: [s3-event-notifications, s3-object-created, s3-object-removed, lambda-permission, sqs-notification, sns-notification, eventbridge-s3, resource-based-policy]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [47-s3-storage-classes-lifecycle-policies, 52-s3-bucket-policies-acls]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Lambda invocations within the free tier (1M requests/month). SQS and SNS usage within free tier. EventBridge is free for AWS service events. Total cost negligible (~$0.01/hr). Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Understanding of S3 bucket operations | Exercise 47 or equivalent |
| Basic Lambda knowledge | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** S3 event notifications that trigger Lambda functions on object creation and deletion events.
2. **Analyze** the permission model for S3-to-Lambda invocation (resource-based policy on the Lambda function).
3. **Compare** direct S3 notifications (Lambda/SQS/SNS) with EventBridge for S3 event routing.
4. **Apply** event filter rules using prefix and suffix to limit which objects trigger notifications.
5. **Evaluate** when to use EventBridge over direct S3 notifications (multiple targets, content filtering, cross-account routing).

---

## Why This Matters

S3 event notifications are the backbone of event-driven architectures on AWS, and the SAA-C03 exam tests this extensively. Every time an object is uploaded, modified, or deleted, S3 can notify Lambda, SQS, SNS, or EventBridge. This pattern powers image processing pipelines (thumbnail generation on upload), data ingestion workflows (trigger ETL when new data arrives), and compliance systems (audit log when objects are deleted).

The critical architectural decision is direct notification vs EventBridge. Direct S3 notification supports only one destination per event type per prefix/suffix combination. EventBridge supports multiple targets, content-based filtering, cross-account delivery, archiving, and replay. The exam presents scenarios where the answer hinges on this distinction -- "multiple services need to process the same upload event" means EventBridge, not direct notification. Understanding the permission model is equally important: S3 needs a resource-based policy on the Lambda function (not an IAM role) to invoke it.

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
  default     = "saa-ex53"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ---------- S3 Bucket ----------

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-events-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ============================================================
# TODO 2: S3 Event Notification to Lambda
# ============================================================
# Configure S3 to notify the Lambda function when objects are
# created or deleted under the "uploads/" prefix.
#
# Requirements:
#   - Resource: aws_s3_bucket_notification
#   - lambda_function block with:
#     - lambda_function_arn = Lambda function ARN (from lambda.tf)
#     - events = ["s3:ObjectCreated:*", "s3:ObjectRemoved:*"]
#     - filter_prefix = "uploads/"
#     - filter_suffix = ".json"
#
# Note: S3 supports only ONE destination per event type per
# prefix+suffix combination. You cannot send the same event
# to both Lambda and SQS using direct S3 notifications.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_notification
# ============================================================


# ============================================================
# TODO 3: Enable EventBridge Integration
# ============================================================
# Enable EventBridge integration on the S3 bucket so that ALL
# S3 events are also sent to EventBridge. This enables:
#   - Multiple targets for the same event
#   - Content-based filtering (event detail fields)
#   - Cross-account event delivery
#   - Event archiving and replay
#
# Requirements:
#   - Add eventbridge = true to the aws_s3_bucket_notification
#     resource from TODO 2
#
# Note: EventBridge integration works alongside direct
# notifications (Lambda/SQS/SNS). You can have both active.
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventBridge.html
# ============================================================
```

### `lambda.tf`

```hcl
data "aws_caller_identity" "current" {}

# ---------- Lambda Function for Processing Uploads ----------

resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-processor-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_logs" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role_policy" "lambda_s3" {
  name = "${var.project_name}-s3-read"
  role = aws_iam_role.lambda.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = ["s3:GetObject"]
      Resource = "${aws_s3_bucket.this.arn}/*"
    }]
  })
}

# Build the Go binary before terraform apply:
#   cd lambda/processor && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip processor.zip bootstrap

resource "aws_lambda_function" "processor" {
  function_name    = "${var.project_name}-processor"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  filename         = "${path.module}/lambda/processor/processor.zip"
  source_code_hash = filebase64sha256("${path.module}/lambda/processor/processor.zip")
  timeout          = 30
}

# lambda/processor/main.go:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"fmt"
# 	"net/url"
#
# 	"github.com/aws/aws-lambda-go/events"
# 	"github.com/aws/aws-lambda-go/lambda"
# )
#
# func handler(ctx context.Context, s3Event events.S3Event) error {
# 	for _, record := range s3Event.Records {
# 		bucket := record.S3.Bucket.Name
# 		key, _ := url.QueryUnescape(record.S3.Object.Key)
# 		eventName := record.EventName
# 		size := record.S3.Object.Size
#
# 		log, _ := json.Marshal(map[string]interface{}{
# 			"action": "S3 Event Processed",
# 			"event":  eventName,
# 			"bucket": bucket,
# 			"key":    key,
# 			"size":   size,
# 		})
# 		fmt.Println(string(log))
# 	}
# 	return nil
# }
#
# func main() { lambda.Start(handler) }

# ============================================================
# TODO 1: Lambda Permission for S3 Invocation
# ============================================================
# Grant S3 permission to invoke the Lambda function. Without
# this resource-based policy, S3 cannot call the function even
# if the function's IAM role has S3 permissions.
#
# Requirements:
#   - Resource: aws_lambda_permission
#   - statement_id = "AllowS3Invoke"
#   - action = "lambda:InvokeFunction"
#   - function_name = Lambda function name
#   - principal = "s3.amazonaws.com"
#   - source_arn = S3 bucket ARN
#   - source_account = current account ID
#
# Why source_account? Prevents confused deputy attacks where
# another account's bucket with the same name could invoke
# your Lambda.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission
# ============================================================
```

### `events.tf`

```hcl
# ============================================================
# TODO 4: EventBridge Rule for S3 Events
# ============================================================
# Create an EventBridge rule that captures S3 object creation
# events and routes them to an SQS queue. This demonstrates
# the advantage of EventBridge: multiple targets for the same
# event type.
#
# Requirements:
#   a) aws_sqs_queue for receiving events
#   b) aws_sqs_queue_policy allowing events.amazonaws.com to
#      send messages
#   c) aws_cloudwatch_event_rule with event_pattern matching:
#      - source = "aws.s3"
#      - detail-type = "Object Created"
#      - detail.bucket.name = bucket name
#   d) aws_cloudwatch_event_target routing to the SQS queue
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/ev-events.html
# ============================================================
```

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "lambda_function_name" {
  value = aws_lambda_function.processor.function_name
}
```

---

## Direct Notifications vs EventBridge

| Feature | Direct S3 Notification | EventBridge |
|---|---|---|
| **Targets** | 1 per event type per prefix/suffix | Multiple targets per event |
| **Filtering** | Prefix and suffix only | Content-based (any event field) |
| **Cross-account** | Limited | Native cross-account delivery |
| **Archive & replay** | No | Yes |
| **Cost** | Free | Free for AWS service events |
| **Latency** | Seconds | Seconds |
| **Event types** | All S3 events | All S3 events |
| **When to choose** | Simple single-target workflows | Multi-target, complex routing |

---

## Spot the Bug

The following S3 notification configuration silently fails. No Lambda invocation occurs when objects are uploaded. Identify the problem before expanding the answer.

```hcl
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.processor.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "uploads/"
  }
}

# Note: no aws_lambda_permission resource exists
```

<details>
<summary>Explain the bug</summary>

**The Lambda function is missing a resource-based policy granting S3 permission to invoke it.** Without an `aws_lambda_permission` resource, S3 cannot call the Lambda function. The notification configuration may be accepted by the S3 API, but when an object is uploaded, S3's invocation attempt is rejected by Lambda's permission check.

This is different from IAM role permissions. The Lambda function's execution role (IAM role) controls what the function CAN DO (read S3 objects, write to DynamoDB, etc.). The resource-based policy (Lambda permission) controls who CAN INVOKE the function. S3 needs explicit permission to invoke -- it does not use IAM roles for invocation.

**Fix:** Add a Lambda permission:

```hcl
resource "aws_lambda_permission" "s3_invoke" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.processor.function_name
  principal      = "s3.amazonaws.com"
  source_arn     = aws_s3_bucket.this.arn
  source_account = data.aws_caller_identity.current.account_id
}
```

The `source_account` condition prevents confused deputy attacks. Without it, any S3 bucket with the same name in any account could potentially invoke your function.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Upload a JSON file to trigger the notification:**
   ```bash
   BUCKET=$(terraform output -raw bucket_name)
   echo '{"event":"test","data":"hello"}' | \
     aws s3 cp - "s3://$BUCKET/uploads/test-event.json"
   ```

3. **Check Lambda execution logs:**
   ```bash
   FUNCTION=$(terraform output -raw lambda_function_name)
   aws logs tail "/aws/lambda/$FUNCTION" --since 1m --format short
   ```
   Expected: Log entry showing `S3 Event Processed` with the file key.

4. **Upload a non-JSON file (should NOT trigger, due to suffix filter):**
   ```bash
   echo "plain text" | aws s3 cp - "s3://$BUCKET/uploads/readme.txt"
   ```

5. **Upload outside the prefix (should NOT trigger):**
   ```bash
   echo '{"data":"test"}' | aws s3 cp - "s3://$BUCKET/other/data.json"
   ```

6. **Delete an object (should trigger the removal event):**
   ```bash
   aws s3 rm "s3://$BUCKET/uploads/test-event.json"
   aws logs tail "/aws/lambda/$FUNCTION" --since 1m --format short
   ```
   Expected: Log entry with `ObjectRemoved` event.

7. **Check EventBridge SQS queue for events:**
   ```bash
   QUEUE_URL=$(aws sqs get-queue-url --queue-name saa-ex53-s3-events \
     --query 'QueueUrl' --output text)
   aws sqs receive-message --queue-url "$QUEUE_URL" \
     --max-number-of-messages 5 --wait-time-seconds 5
   ```

8. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Lambda Permission for S3 (lambda.tf)</summary>

```hcl
resource "aws_lambda_permission" "s3_invoke" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.processor.function_name
  principal      = "s3.amazonaws.com"
  source_arn     = aws_s3_bucket.this.arn
  source_account = data.aws_caller_identity.current.account_id
}
```

</details>

<details>
<summary>TODO 2 -- S3 Event Notification to Lambda (storage.tf)</summary>

```hcl
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.processor.arn
    events              = ["s3:ObjectCreated:*", "s3:ObjectRemoved:*"]
    filter_prefix       = "uploads/"
    filter_suffix        = ".json"
  }

  depends_on = [aws_lambda_permission.s3_invoke]
}
```

The `depends_on` is critical: the Lambda permission must exist before S3 tries to validate it can invoke the function.

</details>

<details>
<summary>TODO 3 -- EventBridge Integration (storage.tf)</summary>

Update the `aws_s3_bucket_notification` resource:

```hcl
resource "aws_s3_bucket_notification" "this" {
  bucket      = aws_s3_bucket.this.id
  eventbridge = true

  lambda_function {
    lambda_function_arn = aws_lambda_function.processor.arn
    events              = ["s3:ObjectCreated:*", "s3:ObjectRemoved:*"]
    filter_prefix       = "uploads/"
    filter_suffix        = ".json"
  }

  depends_on = [aws_lambda_permission.s3_invoke]
}
```

With `eventbridge = true`, ALL S3 events (not just those matching the prefix/suffix filter) are also sent to EventBridge. The direct Lambda notification and EventBridge operate independently.

</details>

<details>
<summary>TODO 4 -- EventBridge Rule to SQS (events.tf)</summary>

```hcl
resource "aws_sqs_queue" "s3_events" {
  name = "${var.project_name}-s3-events"
}

resource "aws_sqs_queue_policy" "s3_events" {
  queue_url = aws_sqs_queue.s3_events.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "events.amazonaws.com" }
      Action    = "sqs:SendMessage"
      Resource  = aws_sqs_queue.s3_events.arn
    }]
  })
}

resource "aws_cloudwatch_event_rule" "s3_created" {
  name = "${var.project_name}-s3-object-created"

  event_pattern = jsonencode({
    source      = ["aws.s3"]
    detail-type = ["Object Created"]
    detail = {
      bucket = {
        name = [aws_s3_bucket.this.id]
      }
    }
  })
}

resource "aws_cloudwatch_event_target" "sqs" {
  rule = aws_cloudwatch_event_rule.s3_created.name
  arn  = aws_sqs_queue.s3_events.arn
}
```

Now the same S3 upload event triggers both the Lambda function (via direct notification) and the SQS queue (via EventBridge). This is the key advantage of EventBridge: multiple targets from a single event source.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 54 covers **Glacier Vault Lock and archival strategies**, where you will implement immutable vault policies with the 24-hour confirmation window, configure lifecycle rules for automatic archival from S3 to Glacier, and compare retrieval tiers (Expedited, Standard, Bulk) with their cost and time trade-offs.

---

## Summary

- **S3 event notifications** trigger Lambda, SQS, or SNS when objects are created, deleted, restored, or replicated
- **Lambda resource-based policy** (`aws_lambda_permission`) is required for S3 to invoke the function -- IAM roles alone are insufficient
- **source_account** in Lambda permission prevents confused deputy attacks from cross-account bucket name collisions
- **Prefix and suffix filters** limit which objects trigger notifications, but only one destination per event type per filter combination
- **EventBridge integration** (`eventbridge = true`) sends ALL S3 events to EventBridge alongside direct notifications
- **EventBridge advantages**: multiple targets, content-based filtering, cross-account delivery, archiving, and replay
- **EventBridge event types** differ from direct notification event names: `Object Created` instead of `s3:ObjectCreated:*`
- Direct notifications and EventBridge are **not mutually exclusive** -- both can be active on the same bucket simultaneously
- **depends_on** the Lambda permission when creating the S3 notification -- S3 validates invocation permissions at configuration time

## Reference

- [S3 Event Notifications](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventNotifications.html)
- [Using EventBridge with S3](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventBridge.html)
- [Terraform aws_s3_bucket_notification](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_notification)
- [Terraform aws_lambda_permission](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission)

## Additional Resources

- [S3 Event Notification Types](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-event-types-and-destinations.html) -- complete list of S3 event types and supported destinations
- [EventBridge S3 Event Types](https://docs.aws.amazon.com/AmazonS3/latest/userguide/ev-events.html) -- EventBridge-specific event detail structure for S3
- [Lambda Async Invocation](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html) -- S3 invokes Lambda asynchronously with retry behavior
- [Fan-Out Pattern with SNS and SQS](https://docs.aws.amazon.com/sns/latest/dg/sns-sqs-as-subscriber.html) -- alternative to EventBridge for simple fan-out scenarios
