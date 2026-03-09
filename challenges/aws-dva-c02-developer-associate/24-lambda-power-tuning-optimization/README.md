# 24. Lambda Power Tuning Optimization

<!--
difficulty: advanced
concepts: [lambda-power-tuning, step-functions, memory-optimization, cost-performance-curve, state-machine, performance-profiling]
tools: [terraform, aws-cli, aws-sam]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [exercise-17]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise deploys the AWS Lambda Power Tuning Step Function, a test Lambda function, and runs multiple invocations across memory configurations. Total cost is approximately $0.02/hr. Remember to run cleanup steps when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- SAM CLI installed (`sam --version`) for deploying the Power Tuning tool
- Exercise 17 completed (understanding of Lambda memory and CPU scaling)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** the cost/performance tradeoff for a Lambda function across multiple memory configurations using empirical data
- **Deploy** the AWS Lambda Power Tuning Step Function using SAM or CloudFormation
- **Analyze** the power tuning output to determine the optimal memory setting for cost, speed, or balanced optimization
- **Design** a performance testing strategy that accounts for cold starts, I/O-bound vs CPU-bound workloads, and statistical significance
- **Interpret** the cost/performance curve to justify memory allocation decisions to stakeholders

## Why This Matters

In Exercise 17, you manually compared two memory settings. In production, you need systematic testing across the full memory range (128 MB to 10,240 MB) with statistical significance. The AWS Lambda Power Tuning tool automates this: it runs your function at each memory setting N times, collects duration and cost data, and produces a visualization URL showing the cost/performance curve.

The exam tests your understanding of Lambda memory optimization principles. The Power Tuning tool is referenced in AWS documentation and whitepapers as a best practice for right-sizing Lambda functions. Understanding how to interpret the results -- when the curve flattens (diminishing returns), where the cost minimum occurs, and why the fastest configuration is not always the cheapest -- demonstrates the kind of operational thinking the DVA-C02 exam expects.

## The Challenge

Deploy the AWS Lambda Power Tuning Step Function, create a test function that performs CPU-intensive and I/O-bound work, and run the power tuning state machine to find the optimal memory configuration. Analyze the results to understand the cost/performance curve.

### Requirements

| Requirement | Description |
|---|---|
| Test Function | Go Lambda that performs CPU-intensive work (hash computation) with configurable intensity |
| Power Tuning Tool | Deploy via SAM from the official AWS repository |
| Memory Range | Test across 128, 256, 512, 1024, 1769, 2048, 3008 MB |
| Invocations | 20 invocations per memory configuration for statistical significance |
| Analysis | Identify the optimal memory for cost, speed, and balanced optimization |
| Comparison | Run tuning for both CPU-bound and I/O-bound workloads |

### Architecture

```
  +----------------------------------------------------------+
  |              Step Functions State Machine                 |
  |  (AWS Lambda Power Tuning)                               |
  |                                                          |
  |  +-----------+    +----------+    +----------+          |
  |  | Initialize|    |  Execute |    |  Analyze |          |
  |  | (set mem  |--->| (invoke  |--->| (compare |          |
  |  |  configs) |    |  N times)|    |  results)|          |
  |  +-----------+    +----------+    +----------+          |
  |       |                |               |                 |
  |       v                v               v                 |
  |  [128, 256,      Lambda function   Cost/duration         |
  |   512, 1024,     invoked 20x at    per config +          |
  |   1769, 2048,    each memory       visualization         |
  |   3008]          setting            URL                  |
  +----------------------------------------------------------+
```

## Hints

<details>
<summary>Hint 1: Deploying the Power Tuning tool</summary>

The AWS Lambda Power Tuning tool is an open-source Step Functions state machine. The easiest deployment method is via the AWS Serverless Application Repository or SAM:

```bash
# Option 1: Deploy from Serverless Application Repository (easiest)
aws serverlessrepo create-cloud-formation-change-set \
  --application-id arn:aws:serverlessrepo:us-east-1:451282441545:applications/aws-lambda-power-tuning \
  --stack-name lambda-power-tuning \
  --capabilities CAPABILITY_IAM

# Then execute the change set from the CloudFormation console or CLI

# Option 2: Deploy via SAM CLI
git clone https://github.com/alexcasalboni/aws-lambda-power-tuning.git
cd aws-lambda-power-tuning
sam deploy --guided
```

The state machine ARN is output after deployment. You will need it to start executions.

</details>

<details>
<summary>Hint 2: Creating the test function</summary>

Create a Go Lambda function that performs configurable CPU work:

```go
// main.go
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

type TuningEvent struct {
	Iterations int `json:"iterations"`
}

func handler(ctx context.Context, event TuningEvent) (map[string]interface{}, error) {
	iterations := event.Iterations
	if iterations == 0 {
		iterations = 100000
	}

	start := time.Now()

	data := []byte("power-tuning-benchmark")
	for i := 0; i < iterations; i++ {
		hash := sha256.Sum256(data)
		data = hash[:]
	}

	duration := time.Since(start)

	return map[string]interface{}{
		"memory_mb":       os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"),
		"duration_ms":     duration.Milliseconds(),
		"iterations":      iterations,
		"function_name":   os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Deploy with Terraform:

```hcl
resource "aws_lambda_function" "test" {
  function_name    = "power-tuning-test"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256  # Starting point; Power Tuning overrides this
  timeout          = 60
}
```

</details>

<details>
<summary>Hint 3: Running the power tuning state machine</summary>

Start the Step Functions execution with your function ARN and memory configurations:

```bash
FUNCTION_ARN=$(aws lambda get-function --function-name power-tuning-test --query "Configuration.FunctionArn" --output text)
STATE_MACHINE_ARN=$(aws stepfunctions list-state-machines --query "stateMachines[?contains(name,'powerTuning')].stateMachineArn" --output text)

aws stepfunctions start-execution \
  --state-machine-arn "$STATE_MACHINE_ARN" \
  --input "{
    \"lambdaARN\": \"$FUNCTION_ARN\",
    \"powerValues\": [128, 256, 512, 1024, 1769, 2048, 3008],
    \"num\": 20,
    \"payload\": {\"iterations\": 100000},
    \"parallelInvocation\": true,
    \"strategy\": \"cost\"
  }"
```

The `strategy` parameter controls the optimization target:
- `"cost"` -- find the cheapest configuration
- `"speed"` -- find the fastest configuration
- `"balanced"` -- find the best cost/speed tradeoff

Wait for the execution to complete (~2-3 minutes), then retrieve the results:

```bash
EXECUTION_ARN=$(aws stepfunctions list-executions \
  --state-machine-arn "$STATE_MACHINE_ARN" \
  --status-filter SUCCEEDED \
  --query "executions[0].executionArn" --output text)

aws stepfunctions describe-execution \
  --execution-arn "$EXECUTION_ARN" \
  --query "output" --output text | jq .
```

</details>

<details>
<summary>Hint 4: Interpreting the results</summary>

The power tuning output contains:

```json
{
  "power": 1024,
  "cost": 0.0000025,
  "duration": 187.5,
  "stateMachine": {
    "executionCost": 0.00025,
    "lambdaCost": 0.0035,
    "visualization": "https://lambda-power-tuning.show/#..."
  }
}
```

Key fields:
- `power` -- the optimal memory configuration based on your chosen strategy
- `cost` -- the average cost per invocation at the optimal memory
- `duration` -- the average duration at the optimal memory
- `visualization` -- a URL showing the cost/performance curve (open in a browser)

The visualization shows:
- X-axis: memory configurations
- Y-axis (left): average duration in ms
- Y-axis (right): average cost per invocation
- The "knee" of the curve is where adding more memory provides diminishing returns

For CPU-bound workloads, expect the curve to show:
- Duration drops sharply from 128 MB to 1769 MB (proportional to CPU)
- Cost stays flat or decreases up to the optimal point
- Beyond the optimal point, cost increases while duration gains diminish

For I/O-bound workloads, expect:
- Duration stays relatively flat regardless of memory
- Cost increases linearly with memory (more memory = higher cost, same duration)
- Optimal memory is the minimum that provides enough RAM for the function

</details>

## Spot the Bug

A developer runs the power tuning tool and gets the result that 3008 MB is the "optimal" configuration with strategy `"speed"`. They set all production functions to 3008 MB. Three weeks later, the Lambda bill is 4x higher than expected.

```json
{
  "power": 3008,
  "cost": 0.0000089,
  "duration": 45.2,
  "strategy": "speed"
}
```

Their production function is I/O-bound (calling an external API that takes ~200ms):

```
128 MB:  duration=215ms, cost=$0.0000004
256 MB:  duration=210ms, cost=$0.0000007
3008 MB: duration=205ms, cost=$0.0000082
```

<details>
<summary>Explain the bug</summary>

The developer used `strategy: "speed"` on an I/O-bound function. The speed strategy picks the fastest memory configuration regardless of cost. For I/O-bound workloads, more memory provides almost no speed improvement (210ms vs 205ms) but increases cost 11x (from $0.0000007 to $0.0000082).

The speed strategy is only appropriate for latency-critical, CPU-bound workloads where every millisecond matters. For I/O-bound functions, use `strategy: "cost"` or `strategy: "balanced"`.

The correct approach:

1. Determine if your function is CPU-bound or I/O-bound by looking at the duration curve
2. If duration barely changes with memory (I/O-bound), use `strategy: "cost"` -- the optimal setting is the minimum memory that provides adequate performance
3. If duration drops significantly with memory (CPU-bound), use `strategy: "balanced"` to find the knee of the cost/performance curve
4. Only use `strategy: "speed"` when latency is the primary concern and cost is secondary

In this case, `strategy: "cost"` would have recommended 128 MB or 256 MB, saving 11x on Lambda costs.

</details>

## Verify What You Learned

```bash
# Verify the test function exists
aws lambda get-function-configuration --function-name power-tuning-test \
  --query "{Name:FunctionName,Memory:MemorySize,Runtime:Runtime}" --output json
```

Expected: JSON showing the function with its configuration.

```bash
# Verify the state machine exists
aws stepfunctions list-state-machines \
  --query "stateMachines[?contains(name,'powerTuning')].{Name:name,ARN:stateMachineArn}" \
  --output table
```

Expected: a state machine with "powerTuning" in the name.

```bash
# Check execution history
STATE_MACHINE_ARN=$(aws stepfunctions list-state-machines --query "stateMachines[?contains(name,'powerTuning')].stateMachineArn" --output text)
aws stepfunctions list-executions \
  --state-machine-arn "$STATE_MACHINE_ARN" \
  --query "executions[*].{Status:status,Start:startDate}" \
  --output table
```

Expected: at least one execution with status `SUCCEEDED`.

## Cleanup

Destroy all resources:

```bash
# Delete the Power Tuning stack
aws cloudformation delete-stack --stack-name lambda-power-tuning
aws cloudformation wait stack-delete-complete --stack-name lambda-power-tuning

# Destroy Terraform resources
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
aws cloudformation describe-stacks --stack-name lambda-power-tuning 2>&1 | head -1
```

Expected: empty state and stack not found.

## What's Next

You used the Lambda Power Tuning tool to empirically determine optimal memory settings. In the next exercise, you will explore **Lambda@Edge and CloudFront Functions**, learning when to use each for request/response manipulation at the edge.

## Summary

- The **AWS Lambda Power Tuning** tool is a Step Functions state machine that tests your function across multiple memory configurations
- It runs your function N times at each memory setting and collects **duration and cost** data
- Three optimization strategies: `cost` (cheapest), `speed` (fastest), `balanced` (best tradeoff)
- **CPU-bound workloads** show dramatic duration improvements with more memory; cost may stay flat or decrease
- **I/O-bound workloads** show minimal duration improvement; cost increases linearly with memory
- The **visualization URL** shows the cost/performance curve -- the "knee" is where diminishing returns begin
- Always match the **strategy** to the workload type: `cost` for I/O-bound, `balanced` for CPU-bound, `speed` only for latency-critical paths
- Run power tuning whenever you **change function code** significantly, as the optimal memory may shift

## Reference

- [AWS Lambda Power Tuning (GitHub)](https://github.com/alexcasalboni/aws-lambda-power-tuning)
- [Lambda Power Tuning in AWS Documentation](https://docs.aws.amazon.com/lambda/latest/operatorguide/profile-functions.html)
- [Lambda Memory and CPU](https://docs.aws.amazon.com/lambda/latest/dg/configuration-function-common.html)
- [Step Functions](https://docs.aws.amazon.com/step-functions/latest/dg/welcome.html)

## Additional Resources

- [Lambda Power Tuning Visualization](https://lambda-power-tuning.show/) -- web app for visualizing power tuning results
- [AWS Compute Optimizer for Lambda](https://docs.aws.amazon.com/compute-optimizer/latest/ug/view-lambda-recommendations.html) -- automated memory recommendations based on historical invocation data
- [Lambda Performance Optimization Whitepaper](https://docs.aws.amazon.com/lambda/latest/operatorguide/perf-optimize.html) -- comprehensive guide to Lambda performance tuning
- [Serverless Application Repository - Power Tuning](https://serverlessrepo.aws.amazon.com/applications/arn:aws:serverlessrepo:us-east-1:451282441545:applications~aws-lambda-power-tuning) -- one-click deployment via SAR

<details>
<summary>Full Solution</summary>

### File Structure

```
24-lambda-power-tuning-optimization/
├── main.go
├── go.mod
└── main.tf
```

### `main.go`

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

type TuningEvent struct {
	Iterations int `json:"iterations"`
}

func handler(ctx context.Context, event TuningEvent) (map[string]interface{}, error) {
	iterations := event.Iterations
	if iterations == 0 {
		iterations = 100000
	}

	start := time.Now()

	data := []byte("power-tuning-benchmark")
	for i := 0; i < iterations; i++ {
		hash := sha256.Sum256(data)
		data = hash[:]
	}

	duration := time.Since(start)

	return map[string]interface{}{
		"memory_mb":   os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"),
		"duration_ms": duration.Milliseconds(),
		"iterations":  iterations,
	}, nil
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
  default     = "power-tuning-test"
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
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "test" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 60

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.test.function_name }
output "function_arn"  { value = aws_lambda_function.test.arn }
```

### Deployment and Execution

```bash
# 1. Deploy the test function
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve

# 2. Deploy Power Tuning via SAR
aws serverlessrepo create-cloud-formation-change-set \
  --application-id arn:aws:serverlessrepo:us-east-1:451282441545:applications/aws-lambda-power-tuning \
  --stack-name lambda-power-tuning \
  --capabilities CAPABILITY_IAM

# Wait for change set, then execute it
CHANGE_SET_ID=$(aws cloudformation list-change-sets --stack-name lambda-power-tuning --query "Summaries[0].ChangeSetId" --output text)
aws cloudformation execute-change-set --change-set-name "$CHANGE_SET_ID"
aws cloudformation wait stack-create-complete --stack-name lambda-power-tuning

# 3. Run the power tuning
FUNCTION_ARN=$(terraform output -raw function_arn)
STATE_MACHINE_ARN=$(aws stepfunctions list-state-machines --query "stateMachines[?contains(name,'powerTuning')].stateMachineArn" --output text)

aws stepfunctions start-execution \
  --state-machine-arn "$STATE_MACHINE_ARN" \
  --input "{
    \"lambdaARN\": \"$FUNCTION_ARN\",
    \"powerValues\": [128, 256, 512, 1024, 1769, 2048, 3008],
    \"num\": 20,
    \"payload\": {\"iterations\": 100000},
    \"parallelInvocation\": true,
    \"strategy\": \"balanced\"
  }"

# 4. Wait and get results
sleep 180
EXECUTION_ARN=$(aws stepfunctions list-executions \
  --state-machine-arn "$STATE_MACHINE_ARN" \
  --status-filter SUCCEEDED \
  --query "executions[0].executionArn" --output text)

aws stepfunctions describe-execution \
  --execution-arn "$EXECUTION_ARN" \
  --query "output" --output text | jq .
```

</details>
