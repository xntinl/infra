# 27. Lambda Recursive Invocation Detection

<!--
difficulty: advanced
concepts: [recursive-invocation-detection, infinite-loop-prevention, event-driven-loops, s3-lambda-trigger, lambda-recursion-controls, aws-lambda-eventsourcearn]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, S3 buckets, and SQS queues. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). The recursive loop detection prevents runaway costs. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Basic understanding of S3 event notifications and Lambda triggers

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** how Lambda's recursive invocation detection mechanism identifies and stops infinite loops in event-driven architectures
- **Design** event-driven architectures that avoid recursive patterns by separating input and output resources
- **Analyze** common recursive loop scenarios: S3-Lambda-S3, SQS-Lambda-SQS, and SNS-Lambda-SNS patterns
- **Implement** recursive loop detection controls using the Lambda console or API configuration
- **Differentiate** between legitimate recursive patterns (intentional retry with different data) and accidental infinite loops (same event re-triggering the same function)

## Why This Matters

One of the most expensive mistakes in serverless architecture is an accidental infinite loop. A Lambda function triggered by S3 writes a file back to the same bucket, which triggers the function again, which writes another file, and so on. Before recursive detection existed, this pattern could generate millions of invocations in minutes, resulting in bills of thousands of dollars.

AWS introduced recursive invocation detection in 2023. When Lambda detects that a function is invoking itself recursively through a supported AWS service (SQS, Lambda, or SNS), it stops the recursive loop after approximately 16 invocations and sends the event to a dead-letter queue (if configured) or drops it. For S3 triggers, Lambda added a separate mechanism: if the function writes to the same bucket that triggered it, Lambda logs a warning but does not automatically stop the loop (you must use prefix/suffix filters or separate buckets).

The DVA-C02 exam tests recursive invocation scenarios as "gotcha" questions. When you see a question about Lambda writing to the same S3 bucket that triggers it, the answer involves either using a different output bucket, using prefix filters to separate input and output, or leveraging the recursive detection feature.

## The Challenge

Build three scenarios that demonstrate recursive invocation risks and their mitigations:

1. **S3 recursive loop**: Lambda triggered by S3 uploads, processes a file, and writes the result back to the same bucket (without prefix filtering -- the dangerous pattern)
2. **SQS recursive loop**: Lambda triggered by SQS sends a message back to the same queue
3. **Safe architecture**: Lambda triggered by S3 writes output to a different bucket

### Requirements

| Requirement | Description |
|---|---|
| Dangerous S3 Pattern | S3 trigger on bucket -> Lambda writes back to same bucket (demonstrate the problem) |
| Safe S3 Pattern | S3 trigger on input bucket -> Lambda writes to output bucket (correct architecture) |
| SQS Loop Detection | SQS trigger -> Lambda sends to same queue -> automatic detection stops it |
| Recursive Controls | Configure recursive invocation detection settings on the Lambda function |
| Monitoring | CloudWatch metrics showing recursive invocation detection events |

### Architecture -- Dangerous Pattern (Do NOT Use in Production)

```
  +----------+     +---------+     +----------+
  | S3 Bucket|---->| Lambda  |---->| S3 Bucket|
  | (uploads)|     |(process)|     | (SAME!)  |
  +----------+     +---------+     +----------+
       ^                                |
       |________________________________|
       INFINITE LOOP! Each write triggers
       another Lambda invocation.
```

### Architecture -- Safe Pattern

```
  +----------+     +---------+     +----------+
  |S3 Input  |---->| Lambda  |---->| S3 Output|
  |  Bucket  |     |(process)|     |  Bucket  |
  +----------+     +---------+     +----------+
  (trigger)                        (no trigger)
```

## Hints

<details>
<summary>Hint 1: The dangerous S3-Lambda-S3 pattern</summary>

This is the classic infinite loop. A Lambda function triggered by S3 ObjectCreated events writes its output back to the same bucket:

```go
// DANGEROUS -- writes back to triggering bucket
func handler(ctx context.Context, event events.S3Event) error {
    cfg, _ := config.LoadDefaultConfig(ctx)
    s3Client := s3.NewFromConfig(cfg)

    for _, record := range event.Records {
        bucket := record.S3.Bucket.Name
        key := record.S3.Object.Key

        // Process the file...
        processedData := process(key)

        // DANGER: Writing back to the SAME bucket triggers another invocation
        _, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
            Bucket: aws.String(bucket),               // Same bucket!
            Key:    aws.String("processed-" + key),    // Different key, but same bucket
            Body:   bytes.NewReader(processedData),
        })
        if err != nil {
            return err
        }
    }
    return nil
}
```

Even though the key is different (`processed-file.txt` vs `file.txt`), the S3 event notification triggers on ANY ObjectCreated event in the bucket, causing an infinite loop.

Mitigations:
1. **Use separate buckets** (best): input bucket triggers Lambda, output goes to a different bucket
2. **Use prefix filters**: trigger only on `input/` prefix, write to `output/` prefix
3. **Use suffix filters**: trigger only on `.raw` suffix, write with `.processed` suffix

</details>

<details>
<summary>Hint 2: Lambda recursive invocation detection for SQS</summary>

Lambda automatically detects recursive loops through SQS, Lambda, and SNS. When detected, it stops processing after ~16 recursive invocations:

```go
// This creates a recursive loop through SQS
func handler(ctx context.Context, event events.SQSEvent) (events.SQSEventResponse, error) {
    cfg, _ := config.LoadDefaultConfig(ctx)
    sqsClient := sqs.NewFromConfig(cfg)

    for _, record := range event.Records {
        // Process the message...
        fmt.Printf("Processing: %s\n", record.Body)

        // RECURSIVE: Sending back to the same queue
        queueURL := os.Getenv("QUEUE_URL")
        _, err := sqsClient.SendMessage(ctx, &sqs.SendMessageInput{
            QueueUrl:    aws.String(queueURL),
            MessageBody: aws.String("re-processed: " + record.Body),
        })
        if err != nil {
            return events.SQSEventResponse{}, err
        }
    }

    return events.SQSEventResponse{}, nil
}
```

Lambda detects this pattern by tracking the `aws:lambda:EventSourceArn` header that propagates through the invocation chain. When it sees the same function being invoked repeatedly by the same event source, it stops the loop.

</details>

<details>
<summary>Hint 3: Configuring recursive loop detection</summary>

Recursive loop detection is enabled by default for Lambda functions. You can configure the behavior using the AWS CLI or Terraform:

```bash
# Check current recursive loop detection setting
aws lambda get-function-recursion-config \
  --function-name my-function

# Set to terminate recursive loops (default)
aws lambda put-function-recursion-config \
  --function-name my-function \
  --recursive-loop "Terminate"

# Set to allow recursive loops (NOT recommended)
aws lambda put-function-recursion-config \
  --function-name my-function \
  --recursive-loop "Allow"
```

In Terraform (AWS provider 5.x):

```hcl
resource "aws_lambda_function_recursion_config" "this" {
  function_name  = aws_lambda_function.this.function_name
  recursive_loop = "Terminate"
}
```

When a recursive loop is terminated:
- The function invocation is stopped
- A `RecursiveInvocationException` is logged to CloudWatch
- The CloudWatch metric `RecursiveInvocationsDropped` is incremented
- If a DLQ is configured, the event is sent to the DLQ

</details>

<details>
<summary>Hint 4: Safe architecture using separate buckets</summary>

The correct architecture uses separate input and output buckets:

```hcl
resource "aws_s3_bucket" "input" {
  bucket        = "recursive-demo-input-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket" "output" {
  bucket        = "recursive-demo-output-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

# Trigger ONLY on the input bucket
resource "aws_s3_bucket_notification" "input" {
  bucket = aws_s3_bucket.input.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.processor.arn
    events              = ["s3:ObjectCreated:*"]
  }
}

# No trigger on the output bucket -- safe!
```

The function reads from the input bucket and writes to the output bucket. No recursive loop is possible because the output bucket has no Lambda trigger.

</details>

## Spot the Bug

A developer uses prefix filtering to prevent recursive loops, but the configuration is wrong:

```hcl
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.data.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.processor.arn
    events              = ["s3:ObjectCreated:*"]
    filter_prefix       = "uploads/"
  }
}
```

The Lambda function writes output files to `uploads/processed/`:

```go
_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(bucketName),
    Key:    aws.String("uploads/processed/" + fileName),  // Still under uploads/ prefix!
})
```

<details>
<summary>Explain the bug</summary>

The S3 notification filter uses `filter_prefix = "uploads/"`, and the Lambda function writes to `uploads/processed/file.txt`. Since `uploads/processed/file.txt` starts with `uploads/`, the S3 notification fires again, creating a recursive loop.

The prefix filter only prevents recursion if the output path does NOT match the filter prefix. Writing to `uploads/processed/` still matches `uploads/`.

Two fixes:

**Fix 1 -- Use a non-overlapping output prefix:**

```go
_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(bucketName),
    Key:    aws.String("output/processed/" + fileName),  // output/ != uploads/
})
```

**Fix 2 -- Use a separate output bucket (safest):**

```go
_, err := s3Client.PutObject(ctx, &s3.PutObjectInput{
    Bucket: aws.String(outputBucketName),  // Different bucket entirely
    Key:    aws.String("processed/" + fileName),
})
```

This is a common exam trick: the prefix filter looks correct at first glance, but the output path still matches the filter because it is a subdirectory of the filtered prefix.

</details>

## Verify What You Learned

```bash
# Verify recursive loop detection is enabled
aws lambda get-function-recursion-config \
  --function-name recursive-demo-safe \
  --query "RecursiveLoop" --output text
```

Expected: `Terminate`

```bash
# Upload a file to the safe (input) bucket
aws s3 cp test.txt s3://$(terraform output -raw input_bucket)/test.txt

# Verify the processed file appears in the output bucket
sleep 10
aws s3 ls s3://$(terraform output -raw output_bucket)/
```

Expected: a processed file in the output bucket, no infinite loop.

```bash
# Check CloudWatch for recursive invocation metrics
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name RecursiveInvocationsDropped \
  --dimensions Name=FunctionName,Value=recursive-demo-safe \
  --start-time $(date -u -v-1H +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 3600 --statistics Sum \
  --query "Datapoints[0].Sum" --output text
```

Expected: `0` (no recursive invocations dropped for the safe architecture).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
aws s3 rm s3://$(terraform output -raw input_bucket) --recursive
aws s3 rm s3://$(terraform output -raw output_bucket) --recursive
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You learned how to prevent and detect recursive invocation loops. In the next exercise, you will explore **Lambda Function URLs**, a simpler alternative to API Gateway for exposing Lambda functions over HTTPS.

## Summary

- **Recursive invocation detection** automatically stops Lambda functions that invoke themselves through SQS, Lambda, or SNS after ~16 iterations
- The detection uses the **`aws:lambda:EventSourceArn`** header to track the invocation chain
- **S3-Lambda-S3 loops** are the most common recursive pattern -- always use separate input/output buckets or non-overlapping prefix filters
- Prefix filters can still cause recursion if the **output path is a subdirectory** of the filtered prefix
- Recursive loop detection is **enabled by default** (`Terminate` mode) since 2023
- When a loop is terminated: invocation stops, `RecursiveInvocationsDropped` metric increments, event goes to DLQ if configured
- You can set `recursive_loop = "Allow"` to disable detection (NOT recommended)
- The safest architecture uses **separate resources** for input and output (different S3 buckets, different SQS queues)

Key exam patterns:
- "Lambda writes to the same S3 bucket that triggers it" = recursive loop risk
- "Lambda sends to the same SQS queue it reads from" = recursive loop, auto-detected
- "How to prevent recursive Lambda invocations?" = separate input/output resources, prefix filters, or recursive detection

## Reference

- [Lambda Recursive Loop Detection](https://docs.aws.amazon.com/lambda/latest/dg/invocation-recursion.html)
- [S3 Event Notifications](https://docs.aws.amazon.com/AmazonS3/latest/userguide/NotificationHowTo.html)
- [Lambda with S3](https://docs.aws.amazon.com/lambda/latest/dg/with-s3.html)
- [Terraform aws_s3_bucket_notification](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_notification)

## Additional Resources

- [Avoiding Recursive Patterns (AWS Blog)](https://aws.amazon.com/blogs/compute/detecting-and-stopping-recursive-loops-in-aws-lambda-functions/) -- detailed walkthrough of recursive detection mechanism
- [Lambda Recursive Invocation Controls](https://docs.aws.amazon.com/lambda/latest/dg/invocation-recursion.html#invocation-recursion-controls) -- API for configuring loop detection behavior
- [S3 Event Notification Filtering](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-filtering.html) -- prefix and suffix filtering for event notifications
- [Lambda DLQ Configuration](https://docs.aws.amazon.com/lambda/latest/dg/invocation-async.html#invocation-dlq) -- configuring dead-letter queues for dropped recursive invocations

<details>
<summary>Full Solution</summary>

### File Structure

```
27-lambda-recursive-invocation-detection/
├── main.go
├── go.mod
└── main.tf
```

### `main.go`

```go
package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func handler(ctx context.Context, event events.S3Event) error {
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	s3Client := s3.NewFromConfig(cfg)
	outputBucket := os.Getenv("OUTPUT_BUCKET")

	for _, record := range event.Records {
		inputBucket := record.S3.Bucket.Name
		inputKey := record.S3.Object.Key

		fmt.Printf("Processing s3://%s/%s\n", inputBucket, inputKey)

		// Read the input file
		getResult, err := s3Client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(inputBucket),
			Key:    aws.String(inputKey),
		})
		if err != nil {
			return fmt.Errorf("failed to get object: %w", err)
		}
		defer getResult.Body.Close()

		data, err := io.ReadAll(getResult.Body)
		if err != nil {
			return fmt.Errorf("failed to read object: %w", err)
		}

		// Process (uppercase the content)
		processed := []byte(strings.ToUpper(string(data)))

		// Write to the OUTPUT bucket (safe -- no recursive trigger)
		outputKey := "processed/" + inputKey
		_, err = s3Client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(outputBucket),
			Key:    aws.String(outputKey),
			Body:   bytes.NewReader(processed),
		})
		if err != nil {
			return fmt.Errorf("failed to put object: %w", err)
		}

		fmt.Printf("Output written to s3://%s/%s\n", outputBucket, outputKey)
	}

	return nil
}

func main() {
	lambda.Start(handler)
}
```

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
  description = "Project name for resource naming"
  type        = string
  default     = "recursive-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
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

resource "aws_s3_bucket" "input" {
  bucket        = "${var.project_name}-input-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket" "output" {
  bucket        = "${var.project_name}-output-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_notification" "input" {
  bucket = aws_s3_bucket.input.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
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

data "aws_iam_policy_document" "s3_access" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.input.arn}/*"]
  }
  statement {
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.output.arn}/*"]
  }
}

resource "aws_iam_role_policy" "s3_access" {
  name   = "s3-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.s3_access.json
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-safe"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30

  environment {
    variables = {
      OUTPUT_BUCKET = aws_s3_bucket.output.bucket
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

resource "aws_lambda_permission" "s3" {
  statement_id  = "AllowS3Invoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "s3.amazonaws.com"
  source_arn    = aws_s3_bucket.input.arn
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}-safe"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "function_name"  { value = aws_lambda_function.this.function_name }
output "input_bucket"   { value = aws_s3_bucket.input.bucket }
output "output_bucket"  { value = aws_s3_bucket.output.bucket }
```

</details>
