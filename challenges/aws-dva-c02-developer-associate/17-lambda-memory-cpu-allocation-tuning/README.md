# 17. Lambda Memory and CPU Allocation Tuning

<!--
difficulty: basic
concepts: [lambda-memory, lambda-cpu-scaling, cloudwatch-metrics, cost-per-invocation, performance-tuning]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates two Lambda functions with different memory configurations. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the relationship between Lambda memory allocation (128-10240 MB) and proportional CPU power
- **Describe** how increasing memory from 256 MB to 1024 MB affects both execution duration and cost per invocation
- **Explain** the cost formula: `(memory_in_GB * duration_in_ms * price_per_GB_ms)` and how it determines whether more memory is cheaper overall
- **Verify** function performance using CloudWatch metrics (`Duration`, `MaxMemoryUsed`, `BilledDuration`)
- **Construct** two Lambda functions with different memory settings and compare their execution characteristics using Terraform

## Why Lambda Memory and CPU Allocation Tuning

Lambda does not let you configure CPU directly. Instead, CPU power scales linearly with memory. At 1,769 MB you get one full vCPU; at 10,240 MB you get six vCPUs. This means a function with 128 MB gets roughly 1/14th of a vCPU -- enough for simple I/O-bound tasks but painfully slow for anything CPU-intensive like JSON parsing, image processing, or cryptographic operations.

The counterintuitive result is that increasing memory can actually reduce cost. A function at 128 MB that takes 3,000 ms to run costs more than the same function at 1024 MB that finishes in 200 ms, because you pay for `memory * time`. The exam tests this concept directly: given a function's memory setting and duration, can you calculate the cost-per-invocation and determine whether right-sizing the memory would be cheaper? Understanding the 1 ms billing granularity (introduced in 2021, replacing the old 100 ms ceiling) is also critical for cost questions.

## Step 1 -- Create the Lambda Function Code

### `lambda/main.go`

This function performs CPU-intensive work (computing SHA-256 hashes in a loop) so that the difference between memory tiers is measurable:

```go
package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	start := time.Now()

	// CPU-intensive work: compute 100,000 SHA-256 hashes
	data := []byte("lambda-memory-tuning-benchmark")
	for i := 0; i < 100000; i++ {
		hash := sha256.Sum256(data)
		data = hash[:]
	}

	duration := time.Since(start)

	memorySize := os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE")
	functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")

	return map[string]interface{}{
		"function_name":     functionName,
		"configured_memory": memorySize,
		"compute_duration":  duration.Milliseconds(),
		"hash_iterations":   100000,
		"message":           fmt.Sprintf("Completed in %dms with %s MB memory", duration.Milliseconds(), memorySize),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Project Files

Create the following files in your exercise directory. This deploys two identical functions with different memory settings so you can compare performance:

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
  default     = "memory-tuning"
}
```

### `build.tf`

```hcl
# -- Build the Go binary for Lambda (linux/arm64) --
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

### `iam.tf`

```hcl
# -- IAM role shared by both functions --
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-demo-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -- Lambda function with 256 MB memory --
resource "aws_lambda_function" "small" {
  function_name    = "${var.project_name}-256mb"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 60

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.small]
}

# -- Lambda function with 1024 MB memory --
resource "aws_lambda_function" "large" {
  function_name    = "${var.project_name}-1024mb"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 1024
  timeout          = 60

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.large]
}
```

### `monitoring.tf`

```hcl
# -- CloudWatch Log Groups --
resource "aws_cloudwatch_log_group" "small" {
  name              = "/aws/lambda/${var.project_name}-256mb"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "large" {
  name              = "/aws/lambda/${var.project_name}-1024mb"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "small_function_name" { value = aws_lambda_function.small.function_name }
output "large_function_name" { value = aws_lambda_function.large.function_name }
output "small_memory"        { value = aws_lambda_function.small.memory_size }
output "large_memory"        { value = aws_lambda_function.large.memory_size }
```

## Step 3 -- Build and Apply

Build the Go binary and deploy both functions:

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

Terraform will create 7 resources: the IAM role, IAM policy attachment, two CloudWatch Log Groups, and two Lambda functions.

### Intermediate Verification

Confirm the expected resource count:

```bash
terraform state list
```

You should see entries including:

```
aws_cloudwatch_log_group.large
aws_cloudwatch_log_group.small
aws_iam_role.this
aws_iam_role_policy_attachment.basic
aws_lambda_function.large
aws_lambda_function.small
```

## Step 4 -- Invoke Both Functions and Compare

Invoke the 256 MB function:

```bash
aws lambda invoke --function-name memory-tuning-256mb --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Invoke the 1024 MB function:

```bash
aws lambda invoke --function-name memory-tuning-1024mb --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Compare the `compute_duration` values. The 1024 MB function should complete significantly faster because it receives 4x the CPU power (1024/256 = 4x).

## Step 5 -- Analyze CloudWatch Metrics

Wait a few minutes for metrics to populate, then query the duration metrics:

```bash
# Duration for the 256 MB function
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Duration \
  --dimensions Name=FunctionName,Value=memory-tuning-256mb \
  --start-time $(date -u -v-15M +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Average \
  --query "Datapoints[0].Average" --output text

# Duration for the 1024 MB function
aws cloudwatch get-metric-statistics \
  --namespace AWS/Lambda \
  --metric-name Duration \
  --dimensions Name=FunctionName,Value=memory-tuning-1024mb \
  --start-time $(date -u -v-15M +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Average \
  --query "Datapoints[0].Average" --output text
```

## Step 6 -- Calculate Cost Per Invocation

Use the AWS Lambda pricing formula to compare costs. The arm64 price is $0.0000000017 per ms per MB (us-east-1):

```bash
# Example calculation (substitute your actual durations):
# 256 MB at 800ms:  0.256 GB * 800 ms * $0.0000000133/GB-ms = $0.00000272
# 1024 MB at 200ms: 1.024 GB * 200 ms * $0.0000000133/GB-ms = $0.00000272
#
# In this example, the cost is identical because the 4x memory increase
# produced a 4x speed improvement. In practice, the ratio depends on
# whether the workload is CPU-bound or I/O-bound.
```

The key insight: for CPU-bound workloads, increasing memory (and thus CPU) often keeps cost constant or even reduces it, while dramatically improving latency.

## Common Mistakes

### 1. Assuming more memory always costs more

Developers avoid increasing memory because they assume cost scales linearly. For CPU-bound work, the faster execution offsets the higher per-millisecond rate.

**Wrong assumption:**

```
128 MB at 3000ms = cheap
1024 MB at 3000ms = expensive
```

**Reality for CPU-bound work:**

```
128 MB at 3000ms  = 0.128 * 3000 * rate = 384 GB-ms
1024 MB at 375ms  = 1.024 * 375 * rate  = 384 GB-ms (same cost, 8x faster)
```

### 2. Not accounting for the 1 ms billing granularity

Before December 2020, Lambda billed in 100 ms increments. A 50 ms execution was billed as 100 ms. Now billing is per-millisecond, making right-sizing even more impactful for short-running functions.

**Wrong -- assuming 100 ms increments:**

```
"My function runs in 110ms, so I'm billed for 200ms"
```

**Correct -- 1 ms billing:**

```
"My function runs in 110ms, so I'm billed for 110ms"
```

### 3. Using memory_size below 256 MB for Go functions

Go's garbage collector and runtime overhead consume memory. At 128 MB, a Go function may spend significant time in GC pauses, making it appear slower than expected even for I/O-bound work.

**Recommendation:** Start at 256 MB minimum for Go Lambda functions and tune from there.

## Verify What You Learned

```bash
aws lambda get-function-configuration --function-name memory-tuning-256mb --query "MemorySize" --output text
```

Expected: `256`

```bash
aws lambda get-function-configuration --function-name memory-tuning-1024mb --query "MemorySize" --output text
```

Expected: `1024`

```bash
# Invoke both and compare durations
SMALL=$(aws lambda invoke --function-name memory-tuning-256mb --payload '{}' /dev/stdout 2>/dev/null | jq '.compute_duration')
LARGE=$(aws lambda invoke --function-name memory-tuning-1024mb --payload '{}' /dev/stdout 2>/dev/null | jq '.compute_duration')
echo "256MB duration: ${SMALL}ms | 1024MB duration: ${LARGE}ms"
```

Expected: The 1024 MB function should complete in roughly 1/4 the time of the 256 MB function.

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

You compared two memory configurations and measured the impact on duration and cost. In the next exercise, you will learn how to **publish Lambda versions and create aliases with weighted traffic shifting** to safely deploy new function code using a canary-style rollout.

## Summary

- Lambda **memory** ranges from 128 MB to 10,240 MB in 1 MB increments
- **CPU power scales linearly** with memory -- at 1,769 MB you get one full vCPU
- For CPU-bound workloads, increasing memory often **reduces both latency and cost** because faster execution offsets the higher per-millisecond rate
- Lambda bills at **1 ms granularity** (not 100 ms) since December 2020
- The cost formula is `memory_in_GB * billed_duration_ms * price_per_GB_ms`
- CloudWatch metrics (`Duration`, `MaxMemoryUsed`, `BilledDuration`) are essential for right-sizing decisions
- Go functions should start at **256 MB minimum** to avoid GC-related performance degradation

## Reference

- [Lambda Memory and CPU Configuration](https://docs.aws.amazon.com/lambda/latest/dg/configuration-function-common.html)
- [Lambda Pricing](https://aws.amazon.com/lambda/pricing/)
- [Terraform aws_lambda_function Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function)
- [CloudWatch Lambda Metrics](https://docs.aws.amazon.com/lambda/latest/dg/monitoring-metrics.html)

## Additional Resources

- [Operating Lambda: Performance Optimization](https://docs.aws.amazon.com/lambda/latest/operatorguide/perf-optimize.html) -- official guide to performance tuning including memory, concurrency, and architecture
- [Lambda Power Tuning Tool](https://docs.aws.amazon.com/lambda/latest/operatorguide/profile-functions.html) -- open-source tool for automated memory/cost optimization (covered in Exercise 24)
- [Understanding Lambda Function Scaling](https://docs.aws.amazon.com/lambda/latest/dg/lambda-concurrency.html) -- how memory affects burst concurrency and account limits
- [ARM64 vs x86_64 Lambda Pricing](https://aws.amazon.com/lambda/pricing/) -- arm64 (Graviton2) functions cost 20% less per GB-second than x86_64
