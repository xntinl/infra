# 80. CodeDeploy Lambda Canary and Linear Deployments

<!--
difficulty: intermediate
concepts: [lambda-canary-deployment, lambda-linear-deployment, pre-traffic-hook, post-traffic-hook, traffic-shifting, alias-routing, appspec-lambda, codedeploy-lifecycle-events]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: implement, evaluate
prerequisites: [79-codedeploy-deployment-configurations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions (main + hook functions) and CodeDeploy resources. All costs are negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a CodeDeploy Lambda deployment with Canary10Percent5Minutes traffic shifting and pre/post-traffic validation hooks
2. **Construct** pre-traffic and post-traffic hook Lambda functions in Go that validate deployment health by invoking the new version and reporting back to CodeDeploy
3. **Evaluate** the interaction between hook results and deployment progression -- what happens when a pre-traffic hook returns Failed vs Succeeded
4. **Analyze** the AppSpec file format for Lambda deployments including version, alias, and hook function references
5. **Debug** common deployment failures: hook timeouts treated as success, incorrect lifecycle event callbacks, and missing CodeDeploy permissions

## Why This Matters

Canary and linear deployments protect production by gradually shifting traffic, but traffic shifting alone is not enough. Without validation hooks, CodeDeploy blindly shifts traffic regardless of whether the new version is healthy. Pre-traffic hooks run before any traffic reaches the new version, and post-traffic hooks run after all traffic has shifted.

The DVA-C02 exam tests the AppSpec format and hook lifecycle: **BeforeAllowTraffic** -> **AllowTraffic** -> **AfterAllowTraffic**. Hook functions must call `PutLifecycleEventHookExecutionStatus` to report Succeeded or Failed. Critical trap: if a hook **times out without reporting**, CodeDeploy treats it as **Succeeded**, not Failed -- a broken hook silently passes.

## Building Blocks

### Main Lambda Function

### `app/main.go`

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
	fmt.Printf("Request handled by version %s\n", version)
	return map[string]interface{}{
		"version": version, "status": "ok", "timestamp": time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func main() { lambda.Start(handler) }
```

### Pre-Traffic Hook Function

### `hooks/pre-traffic/main.go`

```go
package main

import (
	"context"
	"fmt"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/codedeploy"
	lc "github.com/aws/aws-sdk-go-v2/service/lambda"
)

type CodeDeployEvent struct {
	DeploymentId                  string `json:"DeploymentId"`
	LifecycleEventHookExecutionId string `json:"LifecycleEventHookExecutionId"`
}

func handler(ctx context.Context, event CodeDeployEvent) error {
	fmt.Printf("Pre-traffic hook: DeploymentId=%s\n", event.DeploymentId)
	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return reportStatus(ctx, event, "Failed")
	}
	client := lc.NewFromConfig(cfg)
	fn := "codedeploy-hooks-demo:live"
	result, err := client.Invoke(ctx, &lc.InvokeInput{FunctionName: &fn, Payload: []byte(`{"test":true}`)})
	if err != nil || result.FunctionError != nil {
		return reportStatus(ctx, event, "Failed")
	}
	fmt.Println("Pre-traffic validation passed")
	return reportStatus(ctx, event, "Succeeded")
}

func reportStatus(ctx context.Context, event CodeDeployEvent, status string) error {
	cfg, _ := config.LoadDefaultConfig(ctx)
	cd := codedeploy.NewFromConfig(cfg)
	_, err := cd.PutLifecycleEventHookExecutionStatus(ctx,
		&codedeploy.PutLifecycleEventHookExecutionStatusInput{
			DeploymentId:                  &event.DeploymentId,
			LifecycleEventHookExecutionId: &event.LifecycleEventHookExecutionId,
			Status:                        codedeploy.LifecycleEventStatus(status),
		})
	return err
}

func main() { lambda.Start(handler) }
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
  default     = "codedeploy-hooks-demo"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
```

### `build.tf`

```hcl
# IAM roles for app + hook Lambda (AWSLambdaBasicExecutionRole)
# Hook also needs: lambda:InvokeFunction + codedeploy:PutLifecycleEventHookExecutionStatus

# Build resources for app and hook (null_resource + archive_file) -- standard pattern
resource "null_resource" "build_app" {
  triggers = { source_hash = filebase64sha256("${path.module}/app/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/app"
  }
}
data "archive_file" "app_zip" {
  type = "zip"; source_file = "${path.module}/app/bootstrap"
  output_path = "${path.module}/build/app.zip"; depends_on = [null_resource.build_app]
}
resource "null_resource" "build_hook" {
  triggers = { source_hash = filebase64sha256("${path.module}/hooks/pre-traffic/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/hooks/pre-traffic"
  }
}
data "archive_file" "hook_zip" {
  type = "zip"; source_file = "${path.module}/hooks/pre-traffic/bootstrap"
  output_path = "${path.module}/build/hook.zip"; depends_on = [null_resource.build_hook]
}
```

### `lambda.tf`

```hcl
# App Lambda + alias, Hook Lambda, CodeDeploy app -- standard Lambda resources
resource "aws_cloudwatch_log_group" "app" { name = "/aws/lambda/${var.project_name}"; retention_in_days = 1 }
resource "aws_lambda_function" "app" {
  function_name = var.project_name; filename = data.archive_file.app_zip.output_path
  source_code_hash = data.archive_file.app_zip.output_base64sha256
  handler = "bootstrap"; runtime = "provided.al2023"; architectures = ["arm64"]
  role = aws_iam_role.app.arn; timeout = 10; publish = true
  environment { variables = { APP_VERSION = "1.0.0" } }
}
resource "aws_lambda_alias" "live" {
  name = "live"; function_name = aws_lambda_function.app.function_name
  function_version = aws_lambda_function.app.version
}
resource "aws_cloudwatch_log_group" "hook" { name = "/aws/lambda/${var.project_name}-pre-traffic"; retention_in_days = 1 }
resource "aws_lambda_function" "pre_traffic_hook" {
  function_name = "${var.project_name}-pre-traffic"; filename = data.archive_file.hook_zip.output_path
  source_code_hash = data.archive_file.hook_zip.output_base64sha256
  handler = "bootstrap"; runtime = "provided.al2023"; architectures = ["arm64"]
  role = aws_iam_role.hook.arn; timeout = 60
}
```

### `cicd.tf`

```hcl
resource "aws_codedeploy_app" "this" { name = var.project_name; compute_platform = "Lambda" }

# =======================================================
# TODO 1 -- Deployment Group with Canary Config
# =======================================================
# Create a deployment group with:
#   - deployment_config_name: CodeDeployDefault.LambdaCanary10Percent5Minutes
#   - auto_rollback_configuration enabled with
#     DEPLOYMENT_FAILURE and DEPLOYMENT_STOP_ON_ALARM
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_group


# =======================================================
# TODO 2 -- AppSpec for Lambda Deployment
# =======================================================
# Create a local file or use a local_file resource with
# the AppSpec content for a Lambda deployment.
#
# The AppSpec format for Lambda:
# {
#   "version": 0.0,
#   "Resources": [{
#     "myLambdaFunction": {
#       "Type": "AWS::Lambda::Function",
#       "Properties": {
#         "Name": "<function-name>",
#         "Alias": "live",
#         "CurrentVersion": "<current-version>",
#         "TargetVersion": "<new-version>"
#       }
#     }
#   }],
#   "Hooks": [{
#     "BeforeAllowTraffic": "<pre-traffic-hook-function-name>"
#   }]
# }
```

### `outputs.tf`

```hcl
output "function_name"      { value = aws_lambda_function.app.function_name }
output "function_version"   { value = aws_lambda_function.app.version }
output "alias_name"         { value = aws_lambda_alias.live.name }
output "hook_function_name" { value = aws_lambda_function.pre_traffic_hook.function_name }
output "app_name"           { value = aws_codedeploy_app.this.name }
```

## Spot the Bug

A developer creates a pre-traffic hook Lambda that validates the new version. The hook function throws an unhandled exception, but the deployment succeeds and all traffic shifts to the new (potentially broken) version. **What is wrong?**

```go
func handler(ctx context.Context, event CodeDeployEvent) error {
    // Validate the new version
    err := validateNewVersion(ctx)
    if err != nil {
        // BUG: returns error but never calls PutLifecycleEventHookExecutionStatus
        return fmt.Errorf("validation failed: %w", err)
    }

    return reportStatus(ctx, event, "Succeeded")
}
```

<details>
<summary>Explain the bug</summary>

When the hook Lambda function returns an error (or times out), CodeDeploy treats the hook as **Succeeded** by default, not Failed. The hook must explicitly call `PutLifecycleEventHookExecutionStatus` with status `"Failed"` before returning or timing out.

The fix: always report status before returning, even on the error path:

```go
func handler(ctx context.Context, event CodeDeployEvent) error {
    err := validateNewVersion(ctx)
    if err != nil {
        // Report Failed BEFORE returning
        reportStatus(ctx, event, "Failed")
        return fmt.Errorf("validation failed: %w", err)
    }

    return reportStatus(ctx, event, "Succeeded")
}
```

The hook's return value does NOT control the deployment. Only `PutLifecycleEventHookExecutionStatus` determines Succeeded or Failed. If the hook never calls this API, the deployment proceeds as if the hook passed.

</details>

## Solutions

<details>
<summary>cicd.tf -- TODO 1 -- Deployment Group</summary>

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

  auto_rollback_configuration {
    enabled = true
    events  = ["DEPLOYMENT_FAILURE", "DEPLOYMENT_STOP_ON_ALARM"]
  }
}
```

</details>

<details>
<summary>cicd.tf -- TODO 2 -- AppSpec Content</summary>

Create a file `appspec.json`:

```json
{
  "version": 0.0,
  "Resources": [{
    "myLambdaFunction": {
      "Type": "AWS::Lambda::Function",
      "Properties": {
        "Name": "codedeploy-hooks-demo",
        "Alias": "live",
        "CurrentVersion": "1",
        "TargetVersion": "2"
      }
    }
  }],
  "Hooks": [{
    "BeforeAllowTraffic": "codedeploy-hooks-demo-pre-traffic"
  }]
}
```

Trigger deployment via CLI using `aws deploy create-deployment` with the appspec as inline JSON revision.

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
cd app && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
cd hooks/pre-traffic && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ../..
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify resources

```bash
APP_NAME=$(terraform output -raw app_name)

aws deploy get-deployment-group --application-name "$APP_NAME" \
  --deployment-group-name production \
  --query "deploymentGroupInfo.deploymentConfigName" --output text
```

Expected: `CodeDeployDefault.LambdaCanary10Percent5Minutes`

### Step 3 -- Invoke the function through the alias

```bash
FUNCTION_NAME=$(terraform output -raw function_name)
aws lambda invoke --function-name "${FUNCTION_NAME}:live" \
  --payload '{"test":true}' /dev/stdout 2>/dev/null | jq .
```

Expected: response with version "1.0.0"

```bash
terraform plan
```

Expected: `No changes.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list  # Expected: empty
```

## What's Next

You implemented Lambda canary deployments with pre-traffic hooks. Next, you will configure **CodePipeline cross-region deployments** -- deploying to multiple AWS regions with artifact replication.

## Summary

- Lambda CodeDeploy lifecycle: **BeforeAllowTraffic** (pre-traffic hook) -> **AllowTraffic** (traffic shift) -> **AfterAllowTraffic** (post-traffic hook)
- Hook Lambda functions receive `DeploymentId` and `LifecycleEventHookExecutionId` in the event payload
- Hooks MUST call **PutLifecycleEventHookExecutionStatus** to report Succeeded or Failed -- returning an error from the Lambda does NOT fail the deployment
- If a hook **times out or crashes** without reporting status, CodeDeploy treats it as **Succeeded** -- this is the most common exam trap
- **Canary** deploys shift a percentage (e.g., 10%) for a wait period, then shift the rest; **Linear** shifts a fixed percentage at regular intervals
- The AppSpec for Lambda specifies: function name, alias, current version, target version, and hook function names
- Hook functions need `codedeploy:PutLifecycleEventHookExecutionStatus` IAM permission AND `lambda:InvokeFunction` if they validate by invoking the target function
- Auto-rollback with `DEPLOYMENT_STOP_ON_ALARM` reverts traffic during the canary/linear wait period if CloudWatch alarms fire

## Reference

- [CodeDeploy Lambda Deployments](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployment-steps-lambda.html)
- [AppSpec for Lambda](https://docs.aws.amazon.com/codedeploy/latest/userguide/reference-appspec-file-structure-hooks.html#appspec-hooks-lambda)
- [Terraform aws_codedeploy_deployment_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codedeploy_deployment_group)
- [PutLifecycleEventHookExecutionStatus](https://docs.aws.amazon.com/codedeploy/latest/APIReference/API_PutLifecycleEventHookExecutionStatus.html)

## Additional Resources

- [Lambda Traffic Shifting](https://docs.aws.amazon.com/lambda/latest/dg/lambda-traffic-shifting-using-aliases.html) -- how CodeDeploy manages Lambda alias routing
- [SAM Safe Deployments](https://docs.aws.amazon.com/serverless-application-model/latest/developerguide/automating-updates-to-serverless-apps.html) -- using SAM DeploymentPreference for CodeDeploy
