# 99. S3 Event Notifications with Lambda

<!--
difficulty: basic
concepts: [s3-event-notifications, lambda-trigger, s3-object-created, lambda-permission, event-filtering, prefix-suffix]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates an S3 bucket, a Lambda function, and an S3 event notification. S3 and Lambda costs are negligible for testing. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** S3 event notifications to trigger a Lambda function on object creation events
- **Construct** the required `aws_lambda_permission` that allows S3 to invoke the Lambda function
- **Verify** that uploading a file to S3 triggers the Lambda function and the event payload contains bucket and key information
- **Explain** the difference between S3 event notification targets: Lambda, SQS, SNS, and EventBridge
- **Describe** how prefix and suffix filters control which objects trigger notifications

## Why S3 Event Notifications with Lambda

S3 event notifications enable serverless file processing pipelines. When a user uploads an image, S3 notifies a Lambda function that generates thumbnails. When a CSV is dropped into a data lake bucket, Lambda triggers an ETL pipeline. When a log file arrives, Lambda parses and indexes it. No polling, no cron jobs, no servers.

S3 supports four notification targets: **Lambda** (synchronous processing), **SQS** (buffered processing), **SNS** (fan-out to multiple subscribers), and **EventBridge** (advanced routing with rules). For the DVA-C02 exam, Lambda is the most commonly tested target.

The critical detail the exam tests is the **Lambda resource-based policy**. S3 cannot invoke a Lambda function unless the function's resource-based policy explicitly grants `lambda:InvokeFunction` permission to the S3 service principal. Without this `aws_lambda_permission` resource, Terraform creates the notification configuration but S3 receives `AccessDeniedException` when it tries to invoke the function. This is a silent failure -- the object uploads succeed but the Lambda never fires.

Event filtering with **prefix** and **suffix** rules controls which objects trigger notifications. Setting `filter_prefix = "uploads/"` and `filter_suffix = ".jpg"` triggers the function only for JPEG files uploaded to the `uploads/` prefix. Without filters, every object creation in the entire bucket triggers the function.

## Step 1 -- Create the Lambda Function Code

### `main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

var s3Client *s3.Client

func init() {
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	s3Client = s3.NewFromConfig(cfg)
}

func handler(ctx context.Context, event events.S3Event) error {
	funcName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")

	for _, record := range event.Records {
		bucket := record.S3.Bucket.Name
		key := record.S3.Object.Key
		size := record.S3.Object.Size
		eventName := record.EventName
		eventTime := record.EventTime

		fmt.Printf("[%s] Event: %s\n", funcName, eventName)
		fmt.Printf("  Bucket: %s\n", bucket)
		fmt.Printf("  Key: %s\n", key)
		fmt.Printf("  Size: %d bytes\n", size)
		fmt.Printf("  Time: %s\n", eventTime)

		// Get object metadata
		headOutput, err := s3Client.HeadObject(ctx, &s3.HeadObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if err != nil {
			fmt.Printf("  Error getting metadata: %v\n", err)
			continue
		}

		fmt.Printf("  Content-Type: %s\n", aws.ToString(headOutput.ContentType))

		// Log the processing result as structured JSON
		result := map[string]interface{}{
			"event":        eventName,
			"bucket":       bucket,
			"key":          key,
			"size":         size,
			"content_type": aws.ToString(headOutput.ContentType),
			"processed":    true,
		}
		resultJSON, _ := json.Marshal(result)
		fmt.Printf("  Result: %s\n", string(resultJSON))
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Configuration

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
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
  default     = "s3-events-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

# S3 Event Notification
# Triggers Lambda on any object creation in the "uploads/" prefix
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "uploads/"
  }

  depends_on = [aws_lambda_permission.s3]
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "s3_read" {
  statement {
    actions   = ["s3:GetObject", "s3:HeadObject"]
    resources = ["${aws_s3_bucket.this.arn}/*"]
  }
}

resource "aws_iam_role_policy" "s3_read" {
  name   = "s3-read"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.s3_read.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# Lambda Permission for S3
# This is CRITICAL: without this permission, S3 cannot invoke the Lambda
# function. The notification configuration will exist but S3 will receive
# AccessDeniedException when trying to invoke the function.
resource "aws_lambda_permission" "s3" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.this.function_name
  principal      = "s3.amazonaws.com"
  source_arn     = aws_s3_bucket.this.arn
  source_account = data.aws_caller_identity.current.account_id
}
```

### `outputs.tf`

```hcl
output "bucket_name" {
  description = "S3 bucket name"
  value       = aws_s3_bucket.this.id
}

output "function_name" {
  description = "Lambda function name"
  value       = aws_lambda_function.this.function_name
}
```

## Step 3 -- Build and Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

## Step 4 -- Upload Files and Verify Lambda Execution

Upload a file to the `uploads/` prefix (triggers the notification):

```bash
BUCKET=$(terraform output -raw bucket_name)

echo '{"name": "test-file", "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'"}' > /tmp/test.json
aws s3 cp /tmp/test.json "s3://${BUCKET}/uploads/test.json"
```

Upload a file outside the `uploads/` prefix (does NOT trigger the notification):

```bash
aws s3 cp /tmp/test.json "s3://${BUCKET}/other/test.json"
```

Wait for the Lambda to execute:

```bash
sleep 10
```

Check Lambda logs:

```bash
aws logs filter-log-events \
  --log-group-name "/aws/lambda/s3-events-demo" \
  --filter-pattern "Bucket" \
  --query "events[].message" --output text
```

Expected: log entries showing the object from `uploads/test.json` but NOT from `other/test.json`.

## Common Mistakes

### 1. Missing Lambda permission for S3

The most common error. Without `aws_lambda_permission`, S3 gets `AccessDeniedException` when trying to invoke the function. Objects upload successfully but Lambda never fires.

**Wrong -- no permission resource:**

```hcl
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id
  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
  }
  # Missing: depends_on = [aws_lambda_permission.s3]
}
# Missing: aws_lambda_permission resource entirely
```

**What happens:** `terraform apply` may succeed, but S3 cannot invoke the Lambda. No error is visible unless you check S3 event notification delivery metrics.

**Fix -- add the permission and dependency:**

```hcl
resource "aws_lambda_permission" "s3" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "s3.amazonaws.com"
  source_arn    = aws_s3_bucket.this.arn
}

resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id
  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
  }
  depends_on = [aws_lambda_permission.s3]
}
```

### 2. Circular dependency between bucket and notification

Putting the notification configuration inside the bucket resource creates a circular dependency with the Lambda permission.

**Wrong -- notification inside bucket resource (older Terraform pattern):**

```hcl
resource "aws_s3_bucket" "this" {
  bucket = "my-bucket"
  # Don't put notification here -- it creates circular deps
}
```

**Fix -- use a separate `aws_s3_bucket_notification` resource** (shown in the storage.tf above).

### 3. Recursive invocation from reading and writing to the same prefix

If the Lambda function writes output back to the same prefix that triggers the notification, it creates an infinite loop.

**Wrong -- Lambda reads from and writes to `uploads/`:**

```hcl
resource "aws_s3_bucket_notification" "this" {
  lambda_function {
    events        = ["s3:ObjectCreated:*"]
    filter_prefix = "uploads/"   # Triggers on uploads/
  }
}
```

```go
// Lambda writes output back to the SAME prefix
s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(bucket),
    Key:    aws.String("uploads/processed-" + key),  // Triggers another invocation!
})
```

**Fix -- write output to a DIFFERENT prefix:**

```go
s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(bucket),
    Key:    aws.String("processed/" + key),  // Different prefix, no re-trigger
})
```

## Verify What You Learned

```bash
aws s3api get-bucket-notification-configuration \
  --bucket $(terraform output -raw bucket_name) \
  --query "LambdaFunctionConfigurations[0].{Events:Events,Filter:Filter,Lambda:LambdaFunctionArn}" \
  --output json
```

Expected: `s3:ObjectCreated:*` event with `uploads/` prefix filter and the Lambda ARN.

```bash
aws lambda get-policy --function-name $(terraform output -raw function_name) \
  --query "Policy" --output text | jq '.Statement[0].Condition'
```

Expected: condition with `ArnLike` matching the S3 bucket ARN.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
# Empty the bucket first
aws s3 rm "s3://$(terraform output -raw bucket_name)" --recursive

terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured S3 event notifications to trigger Lambda on file uploads. In the next exercise, you will generate **S3 pre-signed URLs** for temporary upload and download access without requiring AWS credentials on the client.

## Summary

- **S3 event notifications** trigger Lambda, SQS, SNS, or EventBridge when objects are created, deleted, or modified
- The `aws_lambda_permission` resource is **mandatory** -- without it, S3 cannot invoke the Lambda function (silent failure)
- **Prefix and suffix filters** control which objects trigger notifications (e.g., `filter_prefix = "uploads/"`, `filter_suffix = ".jpg"`)
- Watch for **recursive invocation**: if Lambda writes back to the same prefix that triggers the notification, it creates an infinite loop
- S3 event payload includes bucket name, object key, size, and event type -- the Lambda function must parse `events.S3Event`
- Use `depends_on = [aws_lambda_permission.s3]` on the notification resource to ensure proper creation order
- `source_account` in the Lambda permission prevents cross-account confused deputy attacks

## Reference

- [S3 Event Notifications](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventNotifications.html)
- [S3 Supported Event Types](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-event-types-and-destinations.html)
- [Terraform aws_s3_bucket_notification](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_notification)
- [Terraform aws_lambda_permission](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission)

## Additional Resources

- [S3 Event Message Structure](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-content-structure.html) -- JSON structure of S3 event records
- [S3 to EventBridge](https://docs.aws.amazon.com/AmazonS3/latest/userguide/EventBridge.html) -- using EventBridge for advanced event filtering and routing
- [Lambda Recursive Invocation Detection](https://docs.aws.amazon.com/lambda/latest/dg/invocation-recursion.html) -- Lambda's built-in loop detection
- [S3 Event Notification Best Practices](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-filtering.html) -- prefix/suffix filtering strategies
