# 63. Lambda Resource-Based Policies

<!--
difficulty: intermediate
concepts: [lambda-resource-based-policy, lambda-permission, source-arn, source-account, identity-vs-resource-policy, cross-service-invocation, event-source-permissions]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, differentiate
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, an S3 bucket, an API Gateway, and an EventBridge rule. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between Lambda's execution role (identity-based: what the function can do) and resource-based policy (who can invoke the function)
2. **Implement** `aws_lambda_permission` resources for S3, API Gateway, and EventBridge to invoke a Lambda function
3. **Apply** the `source_arn` condition to restrict which specific resources can trigger the function
4. **Analyze** the security risk of omitting `source_arn` -- allowing any resource of that service type to invoke the function
5. **Configure** cross-account invocation permissions using resource-based policies

## Why Lambda Resource-Based Policies

Lambda functions have two types of IAM policies working in opposite directions:

1. **Execution role** (identity-based): attached TO the Lambda function, controls what AWS services the function can call (e.g., read from DynamoDB, write to S3). This answers: "What can my function do?"

2. **Resource-based policy**: attached ON the Lambda function, controls who can invoke it. This answers: "Who can call my function?"

When S3, API Gateway, or EventBridge needs to invoke your Lambda function, it calls `lambda:InvokeFunction`. The Lambda service checks the resource-based policy to verify the calling service has permission. Without an `aws_lambda_permission`, the calling service gets `Access Denied` even if everything else is configured correctly.

The DVA-C02 exam tests the distinction between these two policy types. A common trap: a developer configures an S3 event notification to trigger Lambda, sets up the execution role with DynamoDB write permissions, but forgets the `aws_lambda_permission` for S3. The S3 notification fails silently because S3 cannot invoke the function.

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
  default     = "lambda-resource-policy-demo"
}
```

### `build.tf`

```hcl
data "aws_caller_identity" "current" {}

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
# main.go -- simple Go Lambda handler:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"fmt"
#
# 	"github.com/aws/aws-lambda-go/lambda"
# )
#
# func handler(ctx context.Context, event json.RawMessage) (map[string]string, error) {
# 	fmt.Printf("Received event: %s\n", string(event))
# 	return map[string]string{
# 		"status":  "success",
# 		"message": "Invoked by authorized service",
# 	}, nil
# }
#
# func main() { lambda.Start(handler) }

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
  timeout          = 30
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# =======================================================
# TODO 1 -- Lambda permission for S3
# =======================================================
# Allow S3 to invoke this Lambda when objects are created.
#
# Requirements:
#   - statement_id: "AllowS3Invoke"
#   - action: "lambda:InvokeFunction"
#   - principal: "s3.amazonaws.com"
#   - source_arn: the S3 bucket ARN
#   - source_account: your account ID (prevents confused deputy)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission


# =======================================================
# TODO 2 -- Lambda permission for API Gateway
# =======================================================
# Allow API Gateway to invoke this Lambda for HTTP requests.
#
# Requirements:
#   - statement_id: "AllowAPIGatewayInvoke"
#   - action: "lambda:InvokeFunction"
#   - principal: "apigateway.amazonaws.com"
#   - source_arn: "${aws_apigatewayv2_api.this.execution_arn}/*/*"
#     (covers all stages and routes)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission


# =======================================================
# TODO 3 -- Lambda permission for EventBridge
# =======================================================
# Allow EventBridge to invoke this Lambda when matching
# events are published.
#
# Requirements:
#   - statement_id: "AllowEventBridgeInvoke"
#   - action: "lambda:InvokeFunction"
#   - principal: "events.amazonaws.com"
#   - source_arn: the EventBridge rule ARN
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission
```

### `storage.tf`

```hcl
# -- S3 Bucket (trigger source) --
resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

# -- S3 Notification (depends on Lambda permission) --
resource "aws_s3_bucket_notification" "this" {
  bucket = aws_s3_bucket.this.id

  lambda_function {
    lambda_function_arn = aws_lambda_function.this.arn
    events              = ["s3:ObjectCreated:*"]
  }

  depends_on = [aws_lambda_permission.s3]
}
```

### `api.tf`

```hcl
# -- API Gateway (trigger source) --
resource "aws_apigatewayv2_api" "this" {
  name          = var.project_name
  protocol_type = "HTTP"
}

resource "aws_apigatewayv2_integration" "this" {
  api_id                 = aws_apigatewayv2_api.this.id
  integration_type       = "AWS_PROXY"
  integration_uri        = aws_lambda_function.this.invoke_arn
  payload_format_version = "2.0"
}

resource "aws_apigatewayv2_route" "this" {
  api_id    = aws_apigatewayv2_api.this.id
  route_key = "GET /test"
  target    = "integrations/${aws_apigatewayv2_integration.this.id}"
}

resource "aws_apigatewayv2_stage" "this" {
  api_id      = aws_apigatewayv2_api.this.id
  name        = "$default"
  auto_deploy = true
}
```

### `events.tf`

```hcl
# -- EventBridge Rule (trigger source) --
resource "aws_cloudwatch_event_rule" "this" {
  name          = "${var.project_name}-rule"
  event_pattern = jsonencode({
    source      = ["custom.app"]
    detail-type = ["TestEvent"]
  })
}

resource "aws_cloudwatch_event_target" "this" {
  rule = aws_cloudwatch_event_rule.this.name
  arn  = aws_lambda_function.this.arn
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "api_endpoint"  { value = aws_apigatewayv2_stage.this.invoke_url }
output "bucket_name"   { value = aws_s3_bucket.this.id }
```

## Spot the Bug

A developer creates an S3 notification trigger for Lambda but forgets the `source_arn` condition. Any S3 bucket in the account (or even cross-account) can invoke the function:

```hcl
resource "aws_lambda_permission" "s3" {
  statement_id  = "AllowS3Invoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "s3.amazonaws.com"
  # source_arn and source_account MISSING  # <-- BUG
}
```

<details>
<summary>Explain the bug</summary>

Without `source_arn` and `source_account`, the resource-based policy allows **any** S3 bucket to invoke this Lambda function. This is a **confused deputy** vulnerability: if an attacker creates an S3 bucket in another account and configures it to trigger your Lambda function on object upload, your function will process their events using your execution role's permissions.

For S3 specifically, always include both `source_arn` (the specific bucket) and `source_account` (your account ID):

```hcl
resource "aws_lambda_permission" "s3" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.this.function_name
  principal      = "s3.amazonaws.com"
  source_arn     = aws_s3_bucket.this.arn        # Only this bucket
  source_account = data.aws_caller_identity.current.account_id  # Only this account
}
```

For API Gateway and EventBridge, `source_arn` is sufficient because those services include the account ID in the ARN. S3 bucket ARNs do not include the account ID (`arn:aws:s3:::bucket-name`), so `source_account` provides the additional protection.

</details>

## Solutions

<details>
<summary>TODO 1 -- Lambda permission for S3 (`lambda.tf`)</summary>

```hcl
resource "aws_lambda_permission" "s3" {
  statement_id   = "AllowS3Invoke"
  action         = "lambda:InvokeFunction"
  function_name  = aws_lambda_function.this.function_name
  principal      = "s3.amazonaws.com"
  source_arn     = aws_s3_bucket.this.arn
  source_account = data.aws_caller_identity.current.account_id
}
```

</details>

<details>
<summary>TODO 2 -- Lambda permission for API Gateway (`lambda.tf`)</summary>

```hcl
resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGatewayInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_apigatewayv2_api.this.execution_arn}/*/*"
}
```

</details>

<details>
<summary>TODO 3 -- Lambda permission for EventBridge (`lambda.tf`)</summary>

```hcl
resource "aws_lambda_permission" "eventbridge" {
  statement_id  = "AllowEventBridgeInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.this.arn
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- View the resource-based policy

```bash
aws lambda get-policy --function-name $(terraform output -raw function_name) \
  --query "Policy" --output text | jq .
```

Expected: three statements (AllowS3Invoke, AllowAPIGatewayInvoke, AllowEventBridgeInvoke) with their respective principals and source ARN conditions.

### Step 3 -- Test each invocation source

```bash
# Test API Gateway
curl -s $(terraform output -raw api_endpoint)/test | jq .

# Test S3 trigger
aws s3 cp - s3://$(terraform output -raw bucket_name)/trigger-test.txt <<< "trigger test"
sleep 5
aws logs tail /aws/lambda/lambda-resource-policy-demo --since 1m 2>/dev/null | tail -5

# Test EventBridge
aws events put-events --entries '[{"Source":"custom.app","DetailType":"TestEvent","Detail":"{\"key\":\"value\"}"}]'
sleep 5
aws logs tail /aws/lambda/lambda-resource-policy-demo --since 1m 2>/dev/null | tail -5
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured Lambda resource-based policies for three different AWS services. In the next exercise, you will explore **API Gateway resource policies** -- controlling access to your API using IP whitelists, VPC endpoint restrictions, and cross-account access rules.

## Summary

- Lambda has two policy types: **execution role** (what the function can do) and **resource-based policy** (who can invoke it)
- `aws_lambda_permission` creates statements in the resource-based policy granting invoke access to specific services
- Always include **`source_arn`** to restrict which specific resource can trigger the function
- For S3, also include **`source_account`** because S3 bucket ARNs do not contain the account ID
- Without `source_arn`, any resource of that service type can invoke your function (confused deputy risk)
- Resource-based policies support **cross-account invocation** by specifying another account's principal
- The resource-based policy is separate from the execution role -- both must be configured correctly

## Reference

- [Lambda Resource-Based Policies](https://docs.aws.amazon.com/lambda/latest/dg/access-control-resource-based.html)
- [Terraform aws_lambda_permission](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_permission)
- [S3 Event Notifications](https://docs.aws.amazon.com/AmazonS3/latest/userguide/notification-how-to-event-types-and-destinations.html)

## Additional Resources

- [Confused Deputy Problem](https://docs.aws.amazon.com/IAM/latest/UserGuide/confused-deputy.html) -- why source_arn and source_account matter
- [Cross-Account Lambda Invocation](https://docs.aws.amazon.com/lambda/latest/dg/access-control-resource-based.html#permissions-resource-xaccountinvoke) -- granting invoke to other accounts
- [Lambda Permissions Model](https://docs.aws.amazon.com/lambda/latest/dg/lambda-permissions.html) -- complete overview of identity and resource policies
- [API Gateway Lambda Permissions](https://docs.aws.amazon.com/apigateway/latest/developerguide/api-gateway-control-access-using-iam-policies-to-invoke-api.html) -- how API Gateway invocation permissions work
