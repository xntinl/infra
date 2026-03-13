# 91. CloudWatch Synthetics Canaries

<!--
difficulty: advanced
concepts: [cloudwatch-synthetics, canaries, synthetic-monitoring, api-health-checks, canary-runtime, artifact-bucket, vpc-canaries, visual-monitoring]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a CloudWatch Synthetics canary, an API Gateway endpoint, a Lambda function, and an S3 bucket for canary artifacts. Synthetics canaries cost $0.0012 per run. Running every 5 minutes = ~$0.35/month. Total cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Node.js 18+ installed (canary scripts use Node.js runtime)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when CloudWatch Synthetics canaries are more appropriate than simple health check alarms for proactive monitoring
- **Design** a canary that tests API endpoint availability, response correctness, and latency from the customer's perspective
- **Implement** a Synthetics canary with artifact storage, IAM permissions, and CloudWatch alarm integration
- **Analyze** canary runtime versions, scheduling options, and the difference between heartbeat and API canary blueprints
- **Configure** a CloudWatch alarm that triggers when a canary reports failures

## Why CloudWatch Synthetics Canaries

Traditional monitoring is reactive -- CloudWatch alarms fire after errors occur in production traffic. If your API receives no traffic at 3 AM and the database goes down, you only discover the outage when the first user tries to access the service at 8 AM. Five hours of downtime with zero alerts.

CloudWatch Synthetics canaries solve this by generating synthetic traffic on a schedule. A canary is a Lambda function (Node.js or Python) that runs every N minutes, sends requests to your endpoints, validates responses, and reports success or failure. If the canary detects a problem at 3:01 AM, you know before any real user is affected.

The DVA-C02 exam tests several aspects of Synthetics. First, the two canary blueprints: **heartbeat** canaries that simply check if an endpoint responds, and **API** canaries that validate response bodies, status codes, and headers. Second, canary **runtime versions** (e.g., `syn-nodejs-puppeteer-9.0`) -- using outdated runtimes is a common exam trap. Third, artifact storage: every canary run stores screenshots and logs in an S3 bucket, which must be configured with appropriate permissions. Fourth, VPC canaries that test internal endpoints not reachable from the public internet.

## The Challenge

Build an API endpoint and a CloudWatch Synthetics canary that monitors it. The canary should run every 5 minutes, validate the response, and trigger an alarm on failure.

### Requirements

| Requirement | Description |
|---|---|
| API Endpoint | Lambda behind API Gateway returning JSON health data |
| Canary | Synthetics canary checking the endpoint every 5 minutes |
| Validation | Canary validates HTTP 200 status and response body contains expected JSON fields |
| Artifact Bucket | S3 bucket for canary screenshots and logs |
| Alarm | CloudWatch alarm fires when canary success rate drops below 100% |
| IAM | Canary execution role with CloudWatch, S3, and Lambda permissions |

### Architecture

```
  CloudWatch Synthetics
  (runs every 5 minutes)
         |
         v
  [Canary Lambda] ---> [API Gateway] ---> [App Lambda]
         |
         v
  [S3: artifacts]    [CloudWatch Alarm]
  (screenshots,       (canary failure
   HAR files,          triggers alert)
   logs)
```

## Hints

<details>
<summary>Hint 1: Canary runtime and handler structure</summary>

CloudWatch Synthetics canaries use specific runtime versions. The latest Node.js-based runtime is `syn-nodejs-puppeteer-9.0`. The canary script must export a handler function that the Synthetics runtime calls.

For an API canary (no browser screenshots needed), use the Synthetics API testing pattern:

```javascript
// canary.js
const synthetics = require('Synthetics');
const log = require('SyntheticsLogger');

const apiCanaryBlueprint = async function() {
    const url = process.env.API_URL;
    log.info('Checking endpoint: ' + url);

    const response = await synthetics.executeHttpStep(
        'checkApiHealth',
        {
            hostname: new URL(url).hostname,
            path: new URL(url).pathname,
            port: 443,
            protocol: 'https:',
            method: 'GET',
        }
    );

    // Validate response
    const body = JSON.parse(response.body);
    if (body.status !== 'healthy') {
        throw new Error('API is not healthy: ' + JSON.stringify(body));
    }

    log.info('API health check passed');
};

exports.handler = async () => {
    return await apiCanaryBlueprint();
};
```

</details>

<details>
<summary>Hint 2: Terraform canary resource and S3 artifacts</summary>

The canary needs an S3 bucket for storing run artifacts:

```hcl
resource "aws_s3_bucket" "canary_artifacts" {
  bucket        = "synthetics-canary-artifacts-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_synthetics_canary" "api_health" {
  name                 = "api-health-check"
  artifact_s3_location = "s3://${aws_s3_bucket.canary_artifacts.id}/canary/"
  execution_role_arn   = aws_iam_role.canary.arn
  handler              = "canary.handler"
  runtime_version      = "syn-nodejs-puppeteer-9.0"
  zip_file             = data.archive_file.canary.output_path
  start_canary         = true

  schedule {
    expression = "rate(5 minutes)"
  }

  run_config {
    timeout_in_seconds = 60
    environment_variables = {
      API_URL = "${aws_api_gateway_stage.prod.invoke_url}/health"
    }
  }
}
```

Key fields:
- `runtime_version`: Must be a valid Synthetics runtime (not a Lambda runtime)
- `start_canary`: Set to `true` to start the canary immediately after creation
- `artifact_s3_location`: Must include a path prefix (e.g., `/canary/`)
- `handler`: Format is `filename.functionName` (e.g., `canary.handler`)

</details>

<details>
<summary>Hint 3: IAM role for canary execution</summary>

The canary execution role needs permissions for CloudWatch, S3, Lambda, and CloudWatch Logs:

```hcl
data "aws_iam_policy_document" "canary_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "canary" {
  name               = "synthetics-canary-role"
  assume_role_policy = data.aws_iam_policy_document.canary_assume.json
}

data "aws_iam_policy_document" "canary_policy" {
  statement {
    actions = [
      "s3:PutObject",
      "s3:GetObject",
    ]
    resources = ["${aws_s3_bucket.canary_artifacts.arn}/*"]
  }
  statement {
    actions = [
      "s3:GetBucketLocation",
      "s3:ListBucket",
    ]
    resources = [aws_s3_bucket.canary_artifacts.arn]
  }
  statement {
    actions = [
      "logs:CreateLogStream",
      "logs:CreateLogGroup",
      "logs:PutLogEvents",
    ]
    resources = ["arn:aws:logs:*:*:log-group:/aws/lambda/cwsyn-*"]
  }
  statement {
    actions = [
      "cloudwatch:PutMetricData",
    ]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["CloudWatchSynthetics"]
    }
  }
}

resource "aws_iam_role_policy" "canary" {
  name   = "canary-permissions"
  role   = aws_iam_role.canary.id
  policy = data.aws_iam_policy_document.canary_policy.json
}
```

Note: Synthetics canaries are internally implemented as Lambda functions, so the assume role principal is `lambda.amazonaws.com`, not a Synthetics-specific principal.

</details>

<details>
<summary>Hint 4: CloudWatch alarm on canary failures</summary>

Synthetics publishes metrics to the `CloudWatchSynthetics` namespace. The key metric is `SuccessPercent`:

```hcl
resource "aws_cloudwatch_metric_alarm" "canary_failure" {
  alarm_name          = "canary-api-health-failure"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 1
  metric_name         = "SuccessPercent"
  namespace           = "CloudWatchSynthetics"
  period              = 300
  statistic           = "Average"
  threshold           = 100
  treat_missing_data  = "breaching"

  dimensions = {
    CanaryName = aws_synthetics_canary.api_health.name
  }
}
```

Setting `treat_missing_data = "breaching"` ensures the alarm fires if the canary stops running entirely (no data = assumed failure).

</details>

<details>
<summary>Hint 5: VPC canaries for internal endpoints</summary>

To test endpoints inside a VPC (e.g., an ALB in private subnets), configure the canary with VPC settings:

```hcl
resource "aws_synthetics_canary" "internal_check" {
  # ... other config ...

  vpc_config {
    subnet_ids         = [aws_subnet.private.id]
    security_group_ids = [aws_security_group.canary.id]
  }
}
```

VPC canaries need a NAT gateway or VPC endpoint for CloudWatch and S3 access, similar to Lambda VPC networking. The security group must allow outbound traffic to the target endpoint.

</details>

## Spot the Bug

A developer deploys a canary but the alarm never fires even though the API is returning 500 errors. They check the canary runs and see all runs are marked as "PASSED".

```javascript
// canary.js
const synthetics = require('Synthetics');
const log = require('SyntheticsLogger');
const http = require('https');

exports.handler = async () => {
    const url = process.env.API_URL;

    // BUG: Using raw http module instead of synthetics.executeHttpStep
    return new Promise((resolve, reject) => {
        http.get(url, (res) => {
            log.info('Status code: ' + res.statusCode);
            // BUG: Never checks if statusCode indicates failure
            // A 500 response is still treated as a successful canary run
            resolve('Canary completed');
        }).on('error', reject);
    });
};
```

<details>
<summary>Explain the bug</summary>

Two issues cause the canary to report success even when the API returns errors:

1. **Using raw `http` module instead of `synthetics.executeHttpStep`**: The `executeHttpStep` method automatically marks the step as failed for non-2xx status codes and records timing metrics. Using the raw `http` module bypasses this validation.

2. **No status code validation**: The handler receives the response (even a 500) and resolves the promise without checking `res.statusCode`. From the Synthetics runtime's perspective, the canary function completed without throwing an error, so the run is marked as PASSED.

**Fix -- use `synthetics.executeHttpStep` with validation:**

```javascript
const synthetics = require('Synthetics');
const log = require('SyntheticsLogger');

exports.handler = async () => {
    const url = process.env.API_URL;
    const parsedUrl = new URL(url);

    const response = await synthetics.executeHttpStep(
        'checkApiHealth',
        {
            hostname: parsedUrl.hostname,
            path: parsedUrl.pathname,
            port: 443,
            protocol: 'https:',
            method: 'GET',
        }
    );

    // executeHttpStep automatically fails on non-2xx status codes
    // Add additional validation for response body
    const body = JSON.parse(response.body);
    if (body.status !== 'healthy') {
        throw new Error('API returned unhealthy status: ' + body.status);
    }
};
```

`executeHttpStep` provides: automatic status code validation, step timing metrics in CloudWatch, HAR file generation for debugging, and proper failure reporting to the Synthetics runtime.

</details>

## Verify What You Learned

After deploying, run these verification commands:

```bash
# Verify the canary exists and is running
aws synthetics describe-canaries \
  --query "Canaries[].{Name:Name,Status:Status.State,Runtime:RuntimeVersion,Schedule:Schedule.Expression}" \
  --output table
```

Expected: canary with state `RUNNING`.

```bash
# Check canary run results (wait at least 5 minutes after deployment)
aws synthetics get-canary-runs \
  --name api-health-check \
  --query "CanaryRuns[0:3].{Status:Status.State,Start:Timeline.Started}" \
  --output table
```

Expected: recent runs with status `PASSED`.

```bash
# Verify S3 artifact bucket has canary artifacts
BUCKET=$(terraform output -raw artifact_bucket 2>/dev/null || echo "check terraform output")
aws s3 ls "s3://${BUCKET}/canary/" --recursive | head -5
```

Expected: artifact files from canary runs.

```bash
# Verify the CloudWatch alarm exists
aws cloudwatch describe-alarms \
  --alarm-names "canary-api-health-failure" \
  --query "MetricAlarms[0].{Name:AlarmName,State:StateValue,Metric:MetricName,Namespace:Namespace}" \
  --output table
```

Expected: alarm monitoring `SuccessPercent` in `CloudWatchSynthetics` namespace.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
# Stop the canary first (required before deletion)
aws synthetics stop-canary --name api-health-check 2>/dev/null
sleep 10

terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

Note: Canary Lambda functions and log groups created by Synthetics may take a few minutes to clean up. If `terraform destroy` fails on the canary resource, wait 60 seconds and retry.

## What's Next

You built proactive synthetic monitoring with canaries. In the next exercise, you will explore **CloudWatch ServiceLens and Application Signals** for unified observability combining traces, metrics, and logs into a single service-centric view.

## Summary

- **CloudWatch Synthetics canaries** are scheduled Lambda functions that generate synthetic traffic to test endpoint availability proactively
- Two blueprints: **heartbeat** (simple availability) and **API** (response validation with body checks)
- Canary **runtime versions** (e.g., `syn-nodejs-puppeteer-9.0`) are different from Lambda runtimes -- using an outdated version is a common configuration error
- Artifacts (screenshots, HAR files, logs) are stored in an **S3 bucket** configured on the canary
- Use `synthetics.executeHttpStep` for proper status code validation, timing metrics, and failure reporting -- raw HTTP calls bypass canary validation
- Canaries publish `SuccessPercent` metrics to the `CloudWatchSynthetics` namespace for alarm integration
- **VPC canaries** test internal endpoints by running inside VPC subnets with appropriate security groups
- Set `treat_missing_data = "breaching"` on canary alarms so stopped canaries trigger alerts

## Reference

- [CloudWatch Synthetics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries.html)
- [Synthetics Runtime Versions](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_Library.html)
- [Terraform aws_synthetics_canary](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/synthetics_canary)
- [Synthetics Canary Blueprints](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_Blueprints.html)

## Additional Resources

- [Canary Script Examples](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_Samples.html) -- API, heartbeat, and visual monitoring examples
- [VPC Canary Configuration](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_VPC.html) -- running canaries inside VPCs
- [Synthetics Pricing](https://aws.amazon.com/cloudwatch/pricing/) -- $0.0012 per canary run
- [Visual Monitoring with Synthetics](https://docs.aws.amazon.com/AmazonCloudWatch/latest/monitoring/CloudWatch_Synthetics_Canaries_Screenshots.html) -- screenshot comparison for UI regression detection

<details>
<summary>Full Solution</summary>

### File Structure

```
91-cloudwatch-synthetics-canaries/
├── main.tf
├── main.go
└── canary/
    └── canary.js
```

### `canary/canary.js`

```javascript
const synthetics = require('Synthetics');
const log = require('SyntheticsLogger');

const apiCanaryBlueprint = async function () {
    const url = process.env.API_URL;
    log.info('Checking endpoint: ' + url);

    const parsedUrl = new URL(url);

    const response = await synthetics.executeHttpStep(
        'checkApiHealth',
        {
            hostname: parsedUrl.hostname,
            path: parsedUrl.pathname,
            port: 443,
            protocol: 'https:',
            method: 'GET',
        }
    );

    const body = JSON.parse(response.body);

    if (body.status !== 'healthy') {
        throw new Error('API is not healthy: ' + JSON.stringify(body));
    }

    log.info('API health check passed. Uptime: ' + body.uptime);
};

exports.handler = async () => {
    return await apiCanaryBlueprint();
};
```

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

var startTime = time.Now()

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"status":    "healthy",
		"uptime":    time.Since(startTime).String(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module synthetics-demo

go 1.21

require (
    github.com/aws/aws-lambda-go v1.47.0
)
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
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "synthetics-demo"
}
```

### `build.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/lambda/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/lambda"
  }
}

data "archive_file" "app_zip" {
  type        = "zip"
  source_file = "${path.module}/lambda/bootstrap"
  output_path = "${path.module}/build/app.zip"
  depends_on  = [null_resource.go_build]
}

data "archive_file" "canary_zip" {
  type        = "zip"
  source_dir  = "${path.module}/canary"
  output_path = "${path.module}/build/canary.zip"
}
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

resource "aws_iam_role" "app" {
  name               = "${var.project_name}-app-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "app_basic" {
  role       = aws_iam_role.app.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

resource "aws_iam_role" "canary" {
  name               = "${var.project_name}-canary-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

data "aws_iam_policy_document" "canary_policy" {
  statement {
    actions   = ["s3:PutObject", "s3:GetObject"]
    resources = ["${aws_s3_bucket.canary_artifacts.arn}/*"]
  }
  statement {
    actions   = ["s3:GetBucketLocation", "s3:ListBucket"]
    resources = [aws_s3_bucket.canary_artifacts.arn]
  }
  statement {
    actions   = ["logs:CreateLogStream", "logs:CreateLogGroup", "logs:PutLogEvents"]
    resources = ["arn:aws:logs:*:*:log-group:/aws/lambda/cwsyn-*"]
  }
  statement {
    actions   = ["cloudwatch:PutMetricData"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "cloudwatch:namespace"
      values   = ["CloudWatchSynthetics"]
    }
  }
}

resource "aws_iam_role_policy" "canary" {
  name   = "canary-permissions"
  role   = aws_iam_role.canary.id
  policy = data.aws_iam_policy_document.canary_policy.json
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "app" {
  name              = "/aws/lambda/${var.project_name}-app"
  retention_in_days = 1
}

resource "aws_lambda_function" "app" {
  function_name    = "${var.project_name}-app"
  filename         = data.archive_file.app_zip.output_path
  source_code_hash = data.archive_file.app_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.app.arn
  memory_size      = 128
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.app_basic, aws_cloudwatch_log_group.app]
}

resource "aws_lambda_permission" "apigw" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.app.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_api_gateway_resource" "health" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "health"
}

resource "aws_api_gateway_method" "get_health" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.health.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "health" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.health.id
  http_method             = aws_api_gateway_method.get_health.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.app.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  depends_on  = [aws_api_gateway_integration.health]
  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "prod" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "prod"
}
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "canary_artifacts" {
  bucket        = "synthetics-canary-artifacts-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}
```

### `monitoring.tf`

```hcl
resource "aws_synthetics_canary" "api_health" {
  name                 = "api-health-check"
  artifact_s3_location = "s3://${aws_s3_bucket.canary_artifacts.id}/canary/"
  execution_role_arn   = aws_iam_role.canary.arn
  handler              = "canary.handler"
  runtime_version      = "syn-nodejs-puppeteer-9.0"
  zip_file             = data.archive_file.canary_zip.output_path
  start_canary         = true

  schedule {
    expression = "rate(5 minutes)"
  }

  run_config {
    timeout_in_seconds = 60
    environment_variables = {
      API_URL = "${aws_api_gateway_stage.prod.invoke_url}/health"
    }
  }

  depends_on = [aws_iam_role_policy.canary]
}

resource "aws_cloudwatch_metric_alarm" "canary_failure" {
  alarm_name          = "canary-api-health-failure"
  comparison_operator = "LessThanThreshold"
  evaluation_periods  = 1
  metric_name         = "SuccessPercent"
  namespace           = "CloudWatchSynthetics"
  period              = 300
  statistic           = "Average"
  threshold           = 100
  treat_missing_data  = "breaching"

  dimensions = {
    CanaryName = aws_synthetics_canary.api_health.name
  }
}
```

### `outputs.tf`

```hcl
output "api_url" {
  value = "${aws_api_gateway_stage.prod.invoke_url}/health"
}

output "canary_name" {
  value = aws_synthetics_canary.api_health.name
}

output "artifact_bucket" {
  value = aws_s3_bucket.canary_artifacts.id
}
```

</details>
