# 79. CodeDeploy Deployment Configurations

<!--
difficulty: intermediate
concepts: [codedeploy-deployment-types, in-place-deployment, blue-green-deployment, lambda-deployment, canary-deployment, linear-deployment, deployment-group, deployment-configuration, auto-rollback, alarm-based-rollback]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: analyze, implement
prerequisites: [18-lambda-versions-aliases-traffic-shifting, 78-codebuild-environment-variables-secrets]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions, a CodeDeploy application, and deployment groups. Lambda and CodeDeploy costs are negligible, but if you add EC2 instances for in-place/blue-green testing, costs increase. Remember to run `terraform destroy` when finished. Estimated cost: ~$0.02/hr.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Differentiate** between CodeDeploy deployment types: in-place (EC2), blue/green (EC2), and Lambda (AllAtOnce, Canary, Linear)
2. **Implement** a custom deployment configuration (Linear10PercentEvery3Minutes) for gradual Lambda traffic shifting
3. **Configure** a deployment group with CloudWatch alarm-based auto-rollback that reverts traffic when error rates spike
4. **Analyze** the interaction between deployment configurations and rollback settings -- why auto-rollback is critical for Canary and Linear deployments
5. **Explain** the CodeDeploy deployment lifecycle: ApplicationStop, BeforeInstall, AfterInstall, ApplicationStart, ValidateService (EC2) and BeforeAllowTraffic, AfterAllowTraffic (Lambda)

## Why This Matters

CodeDeploy automates application deployments to EC2, ECS, and Lambda with built-in traffic shifting and rollback capabilities. The DVA-C02 exam heavily tests the three deployment types and their configurations.

**In-place** (EC2 only): CodeDeploy stops the application on each instance, deploys the new version, and restarts it. Instances are unavailable during deployment. Use deployment configurations like OneAtATime, HalfAtATime, or AllAtOnce to control how many instances are updated simultaneously.

**Blue/green** (EC2 and ECS): CodeDeploy creates new instances (green), deploys the new version, shifts traffic from old (blue) to new (green), then terminates old instances. Zero downtime because traffic shifts only after the new version is healthy.

**Lambda**: CodeDeploy shifts traffic from one Lambda alias version to another using three strategies. **AllAtOnce** shifts 100% immediately. **Canary** shifts a percentage (e.g., 10%) for a monitoring period, then shifts the remaining 90%. **Linear** shifts a fixed percentage at regular intervals until 100% is reached.

The exam frequently asks: "How do you deploy a Lambda function so that 10% of traffic goes to the new version for 5 minutes before full deployment?" The answer is `CodeDeployDefault.LambdaCanary10Percent5Minutes`. A follow-up question: "What happens if the CloudWatch alarm triggers during the canary period?" If auto-rollback is enabled, CodeDeploy reverts traffic to the original version.

## Building Blocks

### Lambda Function Code

### `lambda/main.go`

```go
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event map[string]interface{}) (map[string]interface{}, error) {
	version := os.Getenv("APP_VERSION")
	fmt.Printf("Processing request with version %s\n", version)

	return map[string]interface{}{
		"version":   version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"message":   fmt.Sprintf("Hello from version %s", version),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### Terraform Skeleton

Create the following files in your exercise directory:

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
  default     = "codedeploy-demo"
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

resource "aws_iam_role" "lambda" {
  name               = "${var.project_name}-lambda-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "codedeploy_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["codedeploy.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "codedeploy" {
  name               = "${var.project_name}-codedeploy-role"
  assume_role_policy = data.aws_iam_policy_document.codedeploy_assume.json
}

resource "aws_iam_role_policy_attachment" "codedeploy" {
  role       = aws_iam_role.codedeploy.name
  policy_arn = "arn:aws:iam::aws:policy/AWSCodeDeployRoleForLambda"
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
  role             = aws_iam_role.lambda.arn
  timeout          = 10
  publish          = true

  environment {
    variables = {
      APP_VERSION = "1.0.0"
    }
  }

  depends_on = [aws_iam_role_policy_attachment.lambda_basic, aws_cloudwatch_log_group.this]
}

resource "aws_lambda_alias" "live" {
  name             = "live"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_metric_alarm" "lambda_errors" {
  alarm_name          = "${var.project_name}-errors"
  comparison_operator = "GreaterThanThreshold"
  evaluation_periods  = 1
  metric_name         = "Errors"
  namespace           = "AWS/Lambda"
  period              = 60
  statistic           = "Sum"
  threshold           = 3
  alarm_description   = "Trigger rollback when Lambda errors exceed 3 per minute"

  dimensions = {
    FunctionName = aws_lambda_function.this.function_name
    Resource     = "${aws_lambda_function.this.function_name}:live"
  }
}
```

### `cicd.tf`

```hcl
resource "aws_codedeploy_app" "this" {
  name             = var.project_name
  compute_platform = "Lambda"
}

# =======================================================
# TODO 1 -- Custom Deployment Configuration
# =======================================================
# Create a custom deployment configuration:
#   - Name: Linear10PercentEvery3Minutes
#   - Type: LINEAR
#   - Percentage: 10
#   - Interval: 3 (minutes)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_config


# =======================================================
# TODO 2 -- Deployment Group with Alarms and Auto-Rollback
# =======================================================
# Create a deployment group:
#   - deployment_config_name: use the custom config from TODO 1
#   - alarm_configuration: reference the lambda_errors alarm
#   - auto_rollback_configuration:
#     - enabled = true
#     - events = ["DEPLOYMENT_FAILURE", "DEPLOYMENT_STOP_ON_ALARM"]
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_group
```

### `outputs.tf`

```hcl
output "function_name"    { value = aws_lambda_function.this.function_name }
output "alias_name"       { value = aws_lambda_alias.live.name }
output "app_name"         { value = aws_codedeploy_app.this.name }
output "alarm_name"       { value = aws_cloudwatch_metric_alarm.lambda_errors.alarm_name }
```

## Spot the Bug

A developer configures a CodeDeploy deployment group with alarm-based auto-rollback. The CloudWatch alarm triggers during a canary deployment, but the deployment continues and completes instead of rolling back. **What is wrong?**

```hcl
resource "aws_codedeploy_deployment_group" "this" {
  app_name               = aws_codedeploy_app.this.name
  deployment_group_name  = "production"
  deployment_config_name = "CodeDeployDefault.LambdaCanary10Percent5Minutes"
  service_role_arn       = aws_iam_role.codedeploy.arn

  deployment_style {
    deployment_type   = "BLUE_GREEN"
    deployment_option = "WITH_TRAFFIC_CONTROL"
  }

  alarm_configuration {
    alarms  = [aws_cloudwatch_metric_alarm.lambda_errors.alarm_name]
    enabled = true
  }

  # BUG: auto_rollback_configuration is missing
}
```

<details>
<summary>Explain the bug</summary>

The `alarm_configuration` block tells CodeDeploy to **monitor** the alarm during deployment, but without `auto_rollback_configuration`, CodeDeploy does not know what to **do** when the alarm triggers. The alarm fires, CodeDeploy sees it, but no rollback action is configured.

You must add `auto_rollback_configuration` with the `DEPLOYMENT_STOP_ON_ALARM` event:

```hcl
auto_rollback_configuration {
  enabled = true
  events  = ["DEPLOYMENT_FAILURE", "DEPLOYMENT_STOP_ON_ALARM"]
}
```

The two events serve different purposes:
- `DEPLOYMENT_FAILURE` -- rolls back when a deployment lifecycle hook fails
- `DEPLOYMENT_STOP_ON_ALARM` -- rolls back when a configured CloudWatch alarm enters ALARM state during deployment

Without `DEPLOYMENT_STOP_ON_ALARM` in the events list, alarm monitoring is passive -- it logs the alarm but does not trigger a rollback. This is a common exam trap: both `alarm_configuration` AND `auto_rollback_configuration` with `DEPLOYMENT_STOP_ON_ALARM` are required for alarm-based rollback.

</details>

## Solutions

<details>
<summary>cicd.tf -- TODO 1 -- Custom Deployment Configuration</summary>

```hcl
resource "aws_codedeploy_deployment_config" "linear_10_3min" {
  deployment_config_name = "${var.project_name}-linear-10-3min"
  compute_platform       = "Lambda"

  traffic_routing_config {
    type = "TimeBasedLinear"

    time_based_linear {
      interval   = 3
      percentage = 10
    }
  }
}
```

This creates a custom Linear deployment configuration that shifts 10% of traffic every 3 minutes. After 30 minutes, all traffic is on the new version. During each interval, CloudWatch alarms are monitored -- if the alarm triggers, auto-rollback reverts all traffic to the original version.

</details>

<details>
<summary>cicd.tf -- TODO 2 -- Deployment Group</summary>

```hcl
resource "aws_codedeploy_deployment_group" "this" {
  app_name               = aws_codedeploy_app.this.name
  deployment_group_name  = "production"
  deployment_config_name = aws_codedeploy_deployment_config.linear_10_3min.id
  service_role_arn       = aws_iam_role.codedeploy.arn

  deployment_style {
    deployment_type   = "BLUE_GREEN"
    deployment_option = "WITH_TRAFFIC_CONTROL"
  }

  alarm_configuration {
    alarms  = [aws_cloudwatch_metric_alarm.lambda_errors.alarm_name]
    enabled = true
  }

  auto_rollback_configuration {
    enabled = true
    events  = ["DEPLOYMENT_FAILURE", "DEPLOYMENT_STOP_ON_ALARM"]
  }
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify CodeDeploy resources

```bash
APP_NAME=$(terraform output -raw app_name)

aws deploy get-application --application-name "$APP_NAME" \
  --query "application.computePlatform" --output text
```

Expected: `Lambda`

```bash
aws deploy list-deployment-configs \
  --query "deploymentConfigsList[?contains(@, 'codedeploy-demo')]" --output json
```

Expected: the custom deployment configuration name.

### Step 3 -- Verify deployment group alarm config

```bash
aws deploy get-deployment-group --application-name "$APP_NAME" \
  --deployment-group-name production \
  --query "deploymentGroupInfo.{Alarms:alarmConfiguration,Rollback:autoRollbackConfiguration}" --output json
```

Expected: alarm configuration enabled with the error alarm, and auto-rollback enabled with DEPLOYMENT_FAILURE and DEPLOYMENT_STOP_ON_ALARM events.

### Step 4 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured CodeDeploy with custom deployment configurations and alarm-based auto-rollback. In the next exercise, you will focus specifically on **CodeDeploy Lambda canary and linear deployments** -- implementing pre/post traffic hooks that validate deployments before and after traffic shifting.

## Summary

- **In-place** (EC2 only): stops app, deploys, restarts -- instances are unavailable during update. Configurations: OneAtATime, HalfAtATime, AllAtOnce
- **Blue/green** (EC2/ECS): creates new fleet, shifts traffic, terminates old fleet -- zero downtime
- **Lambda**: shifts alias traffic between versions using AllAtOnce, Canary (percentage + wait), or Linear (percentage at intervals)
- Built-in Lambda configs: `LambdaAllAtOnce`, `LambdaCanary10Percent5Minutes`, `LambdaCanary10Percent10Minutes`, `LambdaCanary10Percent15Minutes`, `LambdaCanary10Percent30Minutes`, `LambdaLinear10PercentEvery1Minute`, etc.
- **Custom deployment configs** define custom traffic routing: TimeBasedCanary (percentage + interval) or TimeBasedLinear (percentage + interval)
- **Alarm-based rollback** requires BOTH `alarm_configuration` (which alarms to monitor) AND `auto_rollback_configuration` with `DEPLOYMENT_STOP_ON_ALARM` (what to do when alarm fires)
- `DEPLOYMENT_FAILURE` rolls back on hook failures; `DEPLOYMENT_STOP_ON_ALARM` rolls back on CloudWatch alarm state
- CodeDeploy for Lambda uses `BLUE_GREEN` deployment type with `WITH_TRAFFIC_CONTROL` option

## Reference

- [CodeDeploy Deployment Configurations](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployment-configurations.html)
- [Terraform aws_codedeploy_deployment_config](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_config)
- [Terraform aws_codedeploy_deployment_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_group)
- [CodeDeploy Lambda Deployments](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployment-steps-lambda.html)

## Additional Resources

- [CodeDeploy Deployment Lifecycle Events](https://docs.aws.amazon.com/codedeploy/latest/userguide/reference-appspec-file-structure-hooks.html) -- complete list of lifecycle hooks for EC2, ECS, and Lambda
- [Auto-Rollback Configuration](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployments-rollback-and-redeploy.html) -- rollback triggers and behavior
- [Predefined Deployment Configurations](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployment-configurations.html#deployment-configurations-predefined-lambda) -- complete list of built-in Lambda deployment configs
- [CodeDeploy with CloudWatch Alarms](https://docs.aws.amazon.com/codedeploy/latest/userguide/monitoring-create-alarms.html) -- setting up alarm-based rollback
