# 7. Parameter Store and AppConfig for Runtime Configuration

<!--
difficulty: intermediate
concepts: [ssm-parameter-store, parameter-hierarchy, secure-string, kms, appconfig, deployment-strategy, lambda-extension]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a parameter hierarchy using SSM Parameter Store with path-based access control
2. **Justify** when to use Parameter Store versus Secrets Manager versus AppConfig for different configuration needs
3. **Implement** String, StringList, and SecureString parameter types with KMS encryption
4. **Configure** AppConfig with a deployment strategy that supports gradual rollout and automatic rollback
5. **Differentiate** between environment variables, Parameter Store, Secrets Manager, and AppConfig for Lambda runtime configuration

## Why This Matters

Hardcoding configuration into Lambda function code or environment variables creates a tight coupling between your application and its deployment. Changing a log level, toggling a feature flag, or rotating an API key requires a new deployment -- which means downtime risk, CI/CD pipeline execution, and version management overhead. SSM Parameter Store solves this by externalizing configuration into a hierarchical key-value store that your functions read at runtime. SecureString parameters add KMS encryption for sensitive values, and path-based IAM policies let you scope access (e.g., `/app/config/*` for all developers, `/app/secrets/*` for production only).

AppConfig goes further by adding deployment strategies to configuration changes. Instead of instantly pushing a new value (which could break all instances simultaneously), AppConfig can roll out changes gradually -- linear 50% over 10 minutes, for example -- and automatically roll back if CloudWatch alarms fire. The DVA-C02 exam tests your ability to choose the right configuration service: Parameter Store for simple key-value pairs (free tier for String/StringList), Secrets Manager for credentials that need automatic rotation, and AppConfig for feature flags and configuration that needs safe deployment with rollback. The Lambda extensions for Parameter Store and AppConfig add local caching so your function does not call the API on every invocation.

## Building Blocks

Create the following project files. Your job is to fill in each `# TODO` block.

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
  default     = "config-lab"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}

# -------------------------------------------------------
# KMS Key for SecureString parameters
# -------------------------------------------------------
resource "aws_kms_key" "params" {
  description             = "${var.project_name} parameter encryption key"
  deletion_window_in_days = 7
  enable_key_rotation     = true

  tags = {
    Name = "${var.project_name}-params-key"
  }
}

resource "aws_kms_alias" "params" {
  name          = "alias/${var.project_name}-params"
  target_key_id = aws_kms_key.params.key_id
}
```

### `iam.tf`

```hcl
# -------------------------------------------------------
# IAM Role for Lambda
# -------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
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

# =======================================================
# TODO 2 -- IAM Policy for Parameter Access
# =======================================================
# Requirements:
#   - Create an IAM policy that grants:
#     a) ssm:GetParameter AND ssm:GetParametersByPath
#        on arn:aws:ssm:${region}:${account}:parameter/app/config/*
#     b) ssm:GetParameter (only, not ByPath)
#        on arn:aws:ssm:${region}:${account}:parameter/app/secrets/*
#     c) kms:Decrypt on the KMS key ARN
#        (needed to read SecureString parameters)
#   - Attach the policy to the Lambda IAM role
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/iam_role_policy
# Hint: Use data.aws_caller_identity.current.account_id for the account
```

### `config.tf`

```hcl
# =======================================================
# TODO 1 -- Parameter Store Parameters
# =======================================================
# Requirements:
#   - Create a String parameter at /app/config/log_level
#     with value "INFO"
#   - Create a StringList parameter at /app/config/allowed_origins
#     with value "https://example.com,https://app.example.com"
#   - Create a SecureString parameter at /app/secrets/api_key
#     with value "sk-test-abc123-do-not-use-in-production"
#     encrypted with the KMS key defined above
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssm_parameter
# Hint: SecureString uses key_id = aws_kms_key.params.key_id


# =======================================================
# TODO 4 -- AppConfig Application + Environment + Config Profile
# =======================================================
# Requirements:
#   - Create an aws_appconfig_application named "${var.project_name}"
#   - Create an aws_appconfig_environment named "dev"
#   - Create an aws_appconfig_configuration_profile named "feature-flags"
#     with location_uri = "hosted" and type = "AWS.AppConfig.FeatureFlags"
#   - Create an aws_appconfig_hosted_configuration_version with the
#     following JSON content (content_type = "application/json"):
#     {
#       "version": "1",
#       "flags": {
#         "enable_new_ui": { "name": "enable_new_ui" },
#         "max_items_per_page": { "name": "max_items_per_page" }
#       },
#       "values": {
#         "enable_new_ui": { "enabled": false },
#         "max_items_per_page": { "enabled": true, "value": 25 }
#       }
#     }
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_application
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_environment
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_configuration_profile
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_hosted_configuration_version


# =======================================================
# TODO 5 -- AppConfig Deployment Strategy + Deployment
# =======================================================
# Requirements:
#   - Create an aws_appconfig_deployment_strategy named "Linear50Pct10Min"
#   - deployment_duration_in_minutes = 10
#   - growth_factor = 50
#   - growth_type = "LINEAR"
#   - replicate_to = "NONE"
#   - final_bake_time_in_minutes = 2
#   - Create an aws_appconfig_deployment that deploys the
#     hosted configuration version using this strategy
#
# Docs:
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_deployment_strategy
#   https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/appconfig_deployment
```

### `lambda.tf`

```hcl
# =======================================================
# TODO 3 -- Lambda function that reads Parameter Store
# =======================================================
# Requirements:
#   - Create a Lambda function that reads parameters using
#     the AWS SDK for Go v2 at runtime
#   - Set environment variable PARAM_PREFIX to "/app/config"
#   - Set environment variable SECRET_PATH to "/app/secrets/api_key"
#   - Set environment variable AWS_APPCONFIG_EXTENSION_POLL_INTERVAL_SECONDS to "45"
#   - The function code is provided below -- create it using
#     data.archive_file and aws_lambda_function
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function
#
# Function code to use:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"os"
#
# 	"github.com/aws/aws-lambda-go/lambda"
# 	"github.com/aws/aws-sdk-go-v2/config"
# 	"github.com/aws/aws-sdk-go-v2/service/ssm"
# )
#
# var ssmClient *ssm.Client
#
# // Cache parameters outside the handler for reuse across invocations
# var paramCache = make(map[string]interface{})
#
# func init() {
# 	cfg, _ := config.LoadDefaultConfig(context.Background())
# 	ssmClient = ssm.NewFromConfig(cfg)
# }
#
# func getParams(ctx context.Context, prefix string) (map[string]string, error) {
# 	if cached, ok := paramCache[prefix]; ok {
# 		return cached.(map[string]string), nil
# 	}
# 	input := &ssm.GetParametersByPathInput{
# 		Path:           &prefix,
# 		Recursive:      boolPtr(true),
# 		WithDecryption: boolPtr(true),
# 	}
# 	result, err := ssmClient.GetParametersByPath(ctx, input)
# 	if err != nil {
# 		return nil, err
# 	}
# 	params := make(map[string]string)
# 	for _, p := range result.Parameters {
# 		params[*p.Name] = *p.Value
# 	}
# 	paramCache[prefix] = params
# 	return params, nil
# }
#
# func getSecret(ctx context.Context, path string) (string, error) {
# 	if cached, ok := paramCache[path]; ok {
# 		return cached.(string), nil
# 	}
# 	input := &ssm.GetParameterInput{
# 		Name:           &path,
# 		WithDecryption: boolPtr(true),
# 	}
# 	result, err := ssmClient.GetParameter(ctx, input)
# 	if err != nil {
# 		return "", err
# 	}
# 	value := *result.Parameter.Value
# 	paramCache[path] = value
# 	return value, nil
# }
#
# func boolPtr(b bool) *bool { return &b }
#
# func handler(ctx context.Context, event map[string]interface{}) (map[string]interface{}, error) {
# 	configParams, err := getParams(ctx, os.Getenv("PARAM_PREFIX"))
# 	if err != nil {
# 		return nil, err
# 	}
# 	apiKey, err := getSecret(ctx, os.Getenv("SECRET_PATH"))
# 	if err != nil {
# 		return nil, err
# 	}
#
# 	body, _ := json.Marshal(map[string]interface{}{
# 		"config":         configParams,
# 		"api_key_prefix": apiKey[:8] + "...",
# 	})
#
# 	return map[string]interface{}{
# 		"statusCode": 200,
# 		"body":       string(body),
# 	}, nil
# }
#
# func main() {
# 	lambda.Start(handler)
# }
```

### `outputs.tf`

```hcl
output "function_name" {
  value = aws_lambda_function.config_reader.function_name
}

output "log_level_param" {
  value = aws_ssm_parameter.log_level.name
}

output "appconfig_app_id" {
  value = aws_appconfig_application.this.id
}

output "appconfig_env_id" {
  value = aws_appconfig_environment.dev.environment_id
}
```

## Spot the Bug

A developer's Lambda function can read individual parameters with `ssm:GetParameter` but gets an `AccessDeniedException` when calling `GetParametersByPath()`. **What is wrong?**

```hcl
data "aws_iam_policy_document" "ssm_access" {
  statement {
    actions = [
      "ssm:GetParameter",    # <-- only this action
    ]
    resources = [
      "arn:aws:ssm:us-east-1:123456789012:parameter/app/config/*"
    ]
  }
}
```

<details>
<summary>Explain the bug</summary>

The IAM policy grants `ssm:GetParameter` but not `ssm:GetParametersByPath`. These are **different IAM actions**. The `GetParametersByPath` API lets you retrieve all parameters under a path prefix in a single call, but it requires its own IAM permission.

The fix -- add both actions:

```hcl
data "aws_iam_policy_document" "ssm_access" {
  statement {
    actions = [
      "ssm:GetParameter",
      "ssm:GetParametersByPath",
    ]
    resources = [
      "arn:aws:ssm:us-east-1:123456789012:parameter/app/config/*"
    ]
  }
}
```

This is a common DVA-C02 exam question: understanding that SSM Parameter Store has granular IAM actions (`GetParameter`, `GetParameters`, `GetParametersByPath`, `GetParameterHistory`, `PutParameter`) and you must grant exactly the ones your code uses.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify parameters were created

```
aws ssm get-parameters-by-path \
  --path "/app/config" \
  --recursive \
  --query "Parameters[].{Name:Name,Type:Type,Value:Value}" \
  --output table
```

Expected:

```
--------------------------------------------------------------
|                    GetParametersByPath                      |
+-------------------------------+--------+-------------------+
|            Name               | Type   |      Value        |
+-------------------------------+--------+-------------------+
| /app/config/log_level         | String | INFO              |
| /app/config/allowed_origins   | StringList | https://...   |
+-------------------------------+--------+-------------------+
```

### Step 3 -- Verify SecureString parameter

```
aws ssm get-parameter \
  --name "/app/secrets/api_key" \
  --with-decryption \
  --query "Parameter.{Name:Name,Type:Type,Value:Value}" \
  --output table
```

Expected:

```
-----------------------------------------------------------------
|                        GetParameter                           |
+------------------------+--------------+-----------------------+
|         Name           |    Type      |        Value          |
+------------------------+--------------+-----------------------+
| /app/secrets/api_key   | SecureString | sk-test-abc123-...    |
+------------------------+--------------+-----------------------+
```

### Step 4 -- Invoke the Lambda function

```
aws lambda invoke \
  --function-name $(terraform output -raw function_name) \
  --payload '{}' \
  /tmp/config-response.json && \
cat /tmp/config-response.json | jq .
```

Expected:

```json
{
    "statusCode": 200,
    "body": "{\"config\": {\"/app/config/log_level\": \"INFO\", \"/app/config/allowed_origins\": \"https://example.com,https://app.example.com\"}, \"api_key_prefix\": \"sk-test-...\"}"
}
```

### Step 5 -- Verify AppConfig deployment

```
aws appconfig list-deployments \
  --application-id $(terraform output -raw appconfig_app_id) \
  --environment-id $(terraform output -raw appconfig_env_id) \
  --query "Items[].{State:State,Strategy:DeploymentStrategyId,StartedAt:StartedAt}" \
  --output table
```

Expected: At least one deployment with `State: COMPLETE`.

### Step 6 -- Update a parameter and observe no redeployment needed

```
aws ssm put-parameter \
  --name "/app/config/log_level" \
  --value "DEBUG" \
  --overwrite

aws lambda invoke \
  --function-name $(terraform output -raw function_name) \
  --payload '{}' \
  /tmp/config-response2.json && \
cat /tmp/config-response2.json | jq .
```

Expected: The response now shows `"log_level": "DEBUG"` -- the Lambda function picked up the change without redeployment. (Note: the cached value persists for warm invocations; in production you would use the Parameters and Secrets Lambda extension for TTL-based caching.)

## Solutions

<details>
<summary>TODO 1 -- Parameter Store Parameters (config.tf)</summary>

```hcl
resource "aws_ssm_parameter" "log_level" {
  name  = "/app/config/log_level"
  type  = "String"
  value = "INFO"

  tags = {
    Name = "${var.project_name}-log-level"
  }
}

resource "aws_ssm_parameter" "allowed_origins" {
  name  = "/app/config/allowed_origins"
  type  = "StringList"
  value = "https://example.com,https://app.example.com"

  tags = {
    Name = "${var.project_name}-allowed-origins"
  }
}

resource "aws_ssm_parameter" "api_key" {
  name   = "/app/secrets/api_key"
  type   = "SecureString"
  value  = "sk-test-abc123-do-not-use-in-production"
  key_id = aws_kms_key.params.key_id

  tags = {
    Name = "${var.project_name}-api-key"
  }
}
```

</details>

<details>
<summary>TODO 2 -- IAM Policy for Parameter Access (iam.tf)</summary>

```hcl
data "aws_iam_policy_document" "lambda_ssm" {
  statement {
    sid = "ReadConfigParams"
    actions = [
      "ssm:GetParameter",
      "ssm:GetParametersByPath",
    ]
    resources = [
      "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/app/config/*"
    ]
  }

  statement {
    sid = "ReadSecretParams"
    actions = [
      "ssm:GetParameter",
    ]
    resources = [
      "arn:aws:ssm:${var.region}:${data.aws_caller_identity.current.account_id}:parameter/app/secrets/*"
    ]
  }

  statement {
    sid = "DecryptParams"
    actions = [
      "kms:Decrypt",
    ]
    resources = [
      aws_kms_key.params.arn
    ]
  }
}

resource "aws_iam_role_policy" "lambda_ssm" {
  name   = "${var.project_name}-ssm-policy"
  role   = aws_iam_role.lambda.id
  policy = data.aws_iam_policy_document.lambda_ssm.json
}
```

</details>

<details>
<summary>TODO 3 -- Lambda function that reads Parameter Store (lambda.tf)</summary>

```hcl
# NOTE: For Go Lambdas, build the binary externally and reference the zip.
#   GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip config_reader.zip bootstrap
data "archive_file" "config_reader" {
  type        = "zip"
  output_path = "${path.module}/config_reader.zip"

  source {
    content  = <<-GO
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

var ssmClient *ssm.Client

// Cache parameters outside the handler for reuse across invocations
var paramCache = make(map[string]interface{})

func init() {
	cfg, _ := config.LoadDefaultConfig(context.Background())
	ssmClient = ssm.NewFromConfig(cfg)
}

func getParams(ctx context.Context, prefix string) (map[string]string, error) {
	if cached, ok := paramCache[prefix]; ok {
		return cached.(map[string]string), nil
	}
	recursive := true
	decrypt := true
	input := &ssm.GetParametersByPathInput{
		Path:           &prefix,
		Recursive:      &recursive,
		WithDecryption: &decrypt,
	}
	result, err := ssmClient.GetParametersByPath(ctx, input)
	if err != nil {
		return nil, err
	}
	params := make(map[string]string)
	for _, p := range result.Parameters {
		params[*p.Name] = *p.Value
	}
	paramCache[prefix] = params
	return params, nil
}

func getSecret(ctx context.Context, path string) (string, error) {
	if cached, ok := paramCache[path]; ok {
		return cached.(string), nil
	}
	decrypt := true
	input := &ssm.GetParameterInput{
		Name:           &path,
		WithDecryption: &decrypt,
	}
	result, err := ssmClient.GetParameter(ctx, input)
	if err != nil {
		return "", err
	}
	value := *result.Parameter.Value
	paramCache[path] = value
	return value, nil
}

func handler(ctx context.Context, event map[string]interface{}) (map[string]interface{}, error) {
	configParams, err := getParams(ctx, os.Getenv("PARAM_PREFIX"))
	if err != nil {
		return nil, err
	}
	apiKey, err := getSecret(ctx, os.Getenv("SECRET_PATH"))
	if err != nil {
		return nil, err
	}

	body, _ := json.Marshal(map[string]interface{}{
		"config":         configParams,
		"api_key_prefix": apiKey[:8] + "...",
	})

	return map[string]interface{}{
		"statusCode": 200,
		"body":       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
GO
    filename = "main.go"
  }
}

resource "aws_lambda_function" "config_reader" {
  function_name    = "${var.project_name}-config-reader"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  timeout          = 15
  filename         = data.archive_file.config_reader.output_path
  source_code_hash = data.archive_file.config_reader.output_base64sha256

  environment {
    variables = {
      PARAM_PREFIX                                   = "/app/config"
      SECRET_PATH                                    = "/app/secrets/api_key"
      AWS_APPCONFIG_EXTENSION_POLL_INTERVAL_SECONDS  = "45"
    }
  }

  tags = {
    Name = "${var.project_name}-config-reader"
  }
}
```

</details>

<details>
<summary>TODO 4 -- AppConfig Application + Environment + Config Profile (config.tf)</summary>

```hcl
resource "aws_appconfig_application" "this" {
  name        = var.project_name
  description = "Configuration management lab"

  tags = {
    Name = var.project_name
  }
}

resource "aws_appconfig_environment" "dev" {
  name           = "dev"
  application_id = aws_appconfig_application.this.id
  description    = "Development environment"

  tags = {
    Name = "${var.project_name}-dev"
  }
}

resource "aws_appconfig_configuration_profile" "feature_flags" {
  application_id = aws_appconfig_application.this.id
  name           = "feature-flags"
  location_uri   = "hosted"
  type           = "AWS.AppConfig.FeatureFlags"

  tags = {
    Name = "${var.project_name}-feature-flags"
  }
}

resource "aws_appconfig_hosted_configuration_version" "feature_flags" {
  application_id           = aws_appconfig_application.this.id
  configuration_profile_id = aws_appconfig_configuration_profile.feature_flags.configuration_profile_id
  content_type             = "application/json"

  content = jsonencode({
    version = "1"
    flags = {
      enable_new_ui      = { name = "enable_new_ui" }
      max_items_per_page = { name = "max_items_per_page" }
    }
    values = {
      enable_new_ui      = { enabled = false }
      max_items_per_page = { enabled = true, value = 25 }
    }
  })
}
```

</details>

<details>
<summary>TODO 5 -- AppConfig Deployment Strategy + Deployment (config.tf)</summary>

```hcl
resource "aws_appconfig_deployment_strategy" "linear_50" {
  name                           = "Linear50Pct10Min"
  deployment_duration_in_minutes = 10
  growth_factor                  = 50
  growth_type                    = "LINEAR"
  replicate_to                   = "NONE"
  final_bake_time_in_minutes     = 2

  tags = {
    Name = "${var.project_name}-linear-50"
  }
}

resource "aws_appconfig_deployment" "feature_flags" {
  application_id           = aws_appconfig_application.this.id
  environment_id           = aws_appconfig_environment.dev.environment_id
  configuration_profile_id = aws_appconfig_configuration_profile.feature_flags.configuration_profile_id
  configuration_version    = aws_appconfig_hosted_configuration_version.feature_flags.version_number
  deployment_strategy_id   = aws_appconfig_deployment_strategy.linear_50.id

  description = "Initial feature flags deployment"
}
```

</details>

## Cleanup

Destroy all resources:

```
terraform destroy -auto-approve
```

The KMS key enters a 7-day deletion window. Verify parameters are removed:

```
aws ssm get-parameters-by-path --path "/app" --recursive \
  --query "Parameters[].Name" --output text
```

This should return empty output.

## What's Next

In **Exercise 08 -- SQS-Lambda Concurrency, Throttling, and Batch Processing**, you will build an event-driven pipeline where SQS drives Lambda invocations, and you will configure concurrency limits, batch sizes, and partial batch failure reporting.

## Summary

You built a runtime configuration system using three AWS services:

- **SSM Parameter Store** -- String, StringList, and SecureString parameters organized in a `/app/config` and `/app/secrets` hierarchy
- **KMS encryption** -- SecureString parameters encrypted with a customer-managed key
- **Path-based IAM** -- granular access control using `ssm:GetParameter` vs `ssm:GetParametersByPath`
- **AppConfig** -- feature flags with a linear 50% deployment strategy and bake time

| Feature | Parameter Store | Secrets Manager | AppConfig |
|---------|----------------|-----------------|-----------|
| Cost | Free (Standard) | $0.40/secret/mo | Free tier available |
| Rotation | Manual | Automatic | N/A |
| Deployment strategy | Instant | Instant | Gradual with rollback |
| Best for | Config values | Credentials | Feature flags |

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_ssm_parameter` | Store configuration values |
| `aws_kms_key` | Encrypt SecureString parameters |
| `aws_appconfig_application` | AppConfig application container |
| `aws_appconfig_environment` | Target environment for deployments |
| `aws_appconfig_deployment_strategy` | Controls rollout speed |
| `aws_appconfig_deployment` | Deploys a configuration version |

## Additional Resources

- [SSM Parameter Store Documentation](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-parameter-store.html)
- [Parameter Store vs Secrets Manager](https://docs.aws.amazon.com/systems-manager/latest/userguide/ps-integration-secrets-manager.html)
- [AppConfig Documentation](https://docs.aws.amazon.com/appconfig/latest/userguide/what-is-appconfig.html)
- [Lambda Parameters and Secrets Extension](https://docs.aws.amazon.com/systems-manager/latest/userguide/ps-integration-lambda-extensions.html)
- [AppConfig Feature Flags](https://docs.aws.amazon.com/appconfig/latest/userguide/appconfig-creating-configuration-and-profile-feature-flags.html)
- [DVA-C02 Exam Guide](https://aws.amazon.com/certification/certified-developer-associate/)
