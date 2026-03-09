# 1. Lambda Environment Variables, Layers, and Configuration

<!--
difficulty: basic
concepts: [lambda-environment-variables, lambda-layers, lambda-configuration, memory, timeout, cloudwatch-logs]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function and a Lambda Layer. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the Lambda configuration parameters that affect runtime behavior (memory, timeout, environment variables)
- **Construct** a Lambda function with environment variables and an attached Lambda Layer using Terraform
- **Verify** that environment variables and layer data are accessible at runtime using `aws lambda invoke`
- **Explain** why certain environment variable names are reserved by the Lambda runtime and cannot be overridden
- **Describe** the directory structure requirements for Lambda Layers and how the runtime extracts layer contents to `/opt`

## Why Lambda Environment Variables, Layers, and Configuration

Lambda functions rarely operate in isolation. They connect to databases, read from queues, and call external APIs -- and each of those integrations requires configuration values like table names, queue URLs, and feature flags. Hardcoding these values into your function code means redeploying every time a setting changes. Environment variables let you inject configuration at deploy time, so the same code artifact can run in dev, staging, and production with different settings. On the exam, AWS tests whether you understand which env vars are reserved by the runtime (like `AWS_REGION`, `AWS_LAMBDA_FUNCTION_NAME`) and what happens when you try to override them.

Lambda Layers solve a different problem: sharing data and configuration across functions. When ten functions all need the same configuration file, embedding that file into each deployment package creates maintenance overhead and inflates package sizes. A Layer is a ZIP archive that the runtime extracts into `/opt` before your function executes. Unlike interpreted languages where layers typically carry shared code, Go compiles all dependencies statically into a single binary. However, layers remain useful for shipping shared data files -- configuration JSON, certificate bundles, or machine learning models -- that multiple functions can read from `/opt` at runtime. Understanding Layers is critical for the DVA-C02 exam because questions frequently test the layer directory structure, version ARN requirements, and the 250 MB unzipped deployment limit that includes all attached layers.

## Step 1 -- Create the Lambda Function Code

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
)

// LayerConfig represents the shared configuration loaded from the layer.
type LayerConfig struct {
	Version          string   `json:"version"`
	SupportedRegions []string `json:"supported_regions"`
	MaxRetries       int      `json:"max_retries"`
	TimeoutSeconds   int      `json:"timeout_seconds"`
	Source           string   `json:"source"`
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	// Environment variables set via Terraform
	appEnv := os.Getenv("APP_ENV")
	logLevel := os.Getenv("LOG_LEVEL")
	tableName := os.Getenv("TABLE_NAME")

	// Runtime-provided environment variables (read-only, set by Lambda)
	region := os.Getenv("AWS_REGION")
	functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	memoryLimit := os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE")

	// Load shared configuration from the layer (extracted to /opt)
	layerConfig, err := loadLayerConfig("/opt/config/shared_config.json")
	if err != nil {
		return nil, fmt.Errorf("failed to load layer config: %w", err)
	}

	body, _ := json.MarshalIndent(map[string]interface{}{
		"custom_env_vars": map[string]string{
			"APP_ENV":    appEnv,
			"LOG_LEVEL":  logLevel,
			"TABLE_NAME": tableName,
		},
		"runtime_env_vars": map[string]string{
			"AWS_REGION":                     region,
			"AWS_LAMBDA_FUNCTION_NAME":       functionName,
			"AWS_LAMBDA_FUNCTION_MEMORY_SIZE": memoryLimit,
		},
		"layer_config": layerConfig,
		"message":      "Environment and layer configuration loaded successfully",
	}, "", "  ")

	return map[string]interface{}{
		"statusCode": 200,
		"body":       string(body),
	}, nil
}

func loadLayerConfig(path string) (*LayerConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg LayerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Layer Data File

Create a file named `layer/config/shared_config.json`. The layer ZIP will contain a `config/` directory. The Lambda runtime extracts layer contents to `/opt`, so this file becomes `/opt/config/shared_config.json` at runtime:

```json
{
  "version": "1.0.0",
  "supported_regions": ["us-east-1", "us-west-2", "eu-west-1"],
  "max_retries": 3,
  "timeout_seconds": 10,
  "source": "shared-utils-layer"
}
```

## Step 3 -- Create the Terraform Project Files

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
  default     = "env-layers-demo"
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

# -- Package the compiled binary into a ZIP --
data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}

# -- Package the layer data. Lambda extracts the ZIP to /opt, so
#    config/shared_config.json becomes /opt/config/shared_config.json --
data "archive_file" "layer_zip" {
  type        = "zip"
  source_dir  = "${path.module}/layer"
  output_path = "${path.module}/build/layer.zip"
}
```

### `iam.tf`

```hcl
# -- IAM role: every Lambda needs an execution role --
data "aws_iam_policy_document" "lambda_assume_role" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume_role.json
}

# Basic execution policy so the function can write CloudWatch logs
resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -- Lambda Layer: shared data files available to all functions.
#    compatible_runtimes prevents accidental use from wrong runtimes.
#    The ARN includes the version number (:1), required when attaching. --
resource "aws_lambda_layer_version" "shared_config" {
  layer_name          = "shared-config"
  filename            = data.archive_file.layer_zip.output_path
  source_code_hash    = data.archive_file.layer_zip.output_base64sha256
  compatible_runtimes = ["provided.al2023"]
  description         = "Shared configuration data files"
}

# -- Lambda function --
# memory_size: 256 MB (default 128). More memory = proportionally more CPU.
# timeout: 30s (default 3s, max 900s).
# layers: list of layer VERSION ARNs (not the base layer ARN).
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

  environment {
    variables = {
      APP_ENV    = "dev"
      LOG_LEVEL  = "debug"
      TABLE_NAME = "orders"
    }
  }

  layers     = [aws_lambda_layer_version.shared_config.arn]
  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `monitoring.tf`

```hcl
# Explicit log group so Terraform manages (and destroys) it
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "function_name"     { value = aws_lambda_function.this.function_name }
output "function_arn"      { value = aws_lambda_function.this.arn }
output "layer_version_arn" { value = aws_lambda_layer_version.shared_config.arn }
output "configured_memory" { value = aws_lambda_function.this.memory_size }
output "configured_timeout" { value = aws_lambda_function.this.timeout }
```

## Step 4 -- Build and Apply

Build the Go binary and deploy:

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

Terraform will create 6 resources: the IAM role, IAM policy attachment, Lambda Layer version, CloudWatch Log Group, Lambda function, and the two archive data sources (resolved at plan time).

### Intermediate Verification

Confirm the expected resource count:

```bash
terraform state list
```

You should see entries including:

```
aws_cloudwatch_log_group.this
aws_iam_role.this
aws_iam_role_policy_attachment.lambda_basic_execution
aws_lambda_function.this
aws_lambda_layer_version.shared_config
```

## Step 5 -- Invoke the Function and Inspect Configuration

Invoke the function and capture the response:

```bash
aws lambda invoke --function-name env-layers-demo --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

Expected output (values will match your deployment):

```json
{
  "custom_env_vars": {
    "APP_ENV": "dev",
    "LOG_LEVEL": "debug",
    "TABLE_NAME": "orders"
  },
  "runtime_env_vars": {
    "AWS_REGION": "us-east-1",
    "AWS_LAMBDA_FUNCTION_NAME": "env-layers-demo",
    "AWS_LAMBDA_FUNCTION_MEMORY_SIZE": "256"
  },
  "layer_config": {
    "version": "1.0.0",
    "supported_regions": ["us-east-1", "us-west-2", "eu-west-1"],
    "max_retries": 3,
    "timeout_seconds": 10,
    "source": "shared-utils-layer"
  },
  "message": "Environment and layer configuration loaded successfully"
}
```

Inspect the function configuration to verify memory, timeout, and layers:

```bash
aws lambda get-function-configuration --function-name env-layers-demo --query '{Memory: MemorySize, Timeout: Timeout, Runtime: Runtime, Layers: Layers[*].Arn, EnvVars: Environment.Variables}' --output json
```

## Common Mistakes

### 1. Using a reserved environment variable name

Lambda reserves environment variable names that start with `AWS_`. If you set `AWS_REGION` in your Terraform config, it is silently overridden by the runtime.

**Wrong -- attempting to override a reserved variable:**

```hcl
environment {
  variables = {
    AWS_REGION = "eu-west-1"  # Reserved -- Lambda overwrites this silently
    APP_ENV    = "dev"
  }
}
```

**What happens:** `terraform apply` succeeds, but at runtime `os.Getenv("AWS_REGION")` returns the actual region (`us-east-1`), not `eu-west-1`. No error is raised, making this hard to debug.

**Fix -- use a custom prefix for your variables:**

```hcl
environment {
  variables = {
    APP_REGION = "eu-west-1"  # Custom name, no conflict
    APP_ENV    = "dev"
  }
}
```

### 2. Using the layer ARN instead of the layer version ARN

Lambda requires the version-qualified ARN when attaching a layer. The layer ARN without a version suffix does not resolve to a specific artifact.

**Wrong -- unversioned layer ARN:**

```hcl
layers = ["arn:aws:lambda:us-east-1:123456789012:layer:shared-config"]
```

**What happens:** `terraform apply` fails with `InvalidParameterValueException: Layer version does not exist`.

**Fix -- use the version ARN (Terraform's `aws_lambda_layer_version` resource outputs this automatically):**

```hcl
layers = [aws_lambda_layer_version.shared_config.arn]
# Produces: arn:aws:lambda:us-east-1:123456789012:layer:shared-config:1
```

### 3. Not setting compatible_runtimes on the layer

Omitting `compatible_runtimes` means the layer appears compatible with all runtimes, but the data format or file paths inside may not match expectations. This causes confusing runtime errors when a function with a different runtime tries to read layer files from unexpected paths.

**Wrong -- no runtime restriction:**

```hcl
resource "aws_lambda_layer_version" "shared_config" {
  layer_name = "shared-config"
  filename   = data.archive_file.layer_zip.output_path
  # compatible_runtimes omitted -- any runtime can attach this layer
}
```

**What happens:** A function using a different runtime or architecture attaches the layer but cannot find the expected files at the assumed paths.

**Fix -- specify compatible runtimes explicitly:**

```hcl
resource "aws_lambda_layer_version" "shared_config" {
  layer_name          = "shared-config"
  filename            = data.archive_file.layer_zip.output_path
  compatible_runtimes = ["provided.al2023"]
}
```

## Verify What You Learned

```bash
aws lambda get-function-configuration --function-name env-layers-demo --query "MemorySize" --output text
```

Expected: `256`

```bash
aws lambda get-function-configuration --function-name env-layers-demo --query "Timeout" --output text
```

Expected: `30`

```bash
aws lambda get-function-configuration --function-name env-layers-demo --query "Environment.Variables.TABLE_NAME" --output text
```

Expected: `orders`

```bash
aws lambda get-function-configuration --function-name env-layers-demo --query "Layers[0].Arn" --output text
```

Expected: an ARN ending with `:shared-config:1` (the version-qualified layer ARN).

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

You deployed a single function with environment-based configuration and a shared data layer. In the next exercise, you will build **two API Gateway integrations** -- a REST API with request validation and an HTTP API with a simpler setup -- both backed by the same Lambda function, and learn how the two payload formats differ.

## Summary

- **Environment variables** inject configuration at deploy time without changing function code
- Lambda **reserves** variable names starting with `AWS_` -- they are silently overridden at runtime
- **Memory** allocation (128 MB to 10,240 MB) also scales CPU proportionally -- more memory means faster execution
- **Timeout** (1s to 900s) controls maximum execution duration -- set it based on your function's workload, not the maximum
- **Lambda Layers** extract to `/opt` at runtime; Go compiles dependencies statically, so layers are best used for shared data files (config, certs, models) rather than code libraries
- Layer references require the **version-qualified ARN** (ending in `:1`, `:2`, etc.), not the base layer ARN
- Setting `compatible_runtimes` on a layer prevents accidental attachment from incompatible function runtimes

## Reference

- [AWS Lambda Environment Variables](https://docs.aws.amazon.com/lambda/latest/dg/configuration-envvars.html)
- [AWS Lambda Layers](https://docs.aws.amazon.com/lambda/latest/dg/chapter-layers.html)
- [Terraform aws_lambda_function Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function)
- [Terraform aws_lambda_layer_version Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_layer_version)

## Additional Resources

- [Lambda Runtime Environment Variables (Reserved)](https://docs.aws.amazon.com/lambda/latest/dg/configuration-envvars.html#configuration-envvars-runtime) -- complete list of reserved variable names that cannot be overridden
- [Creating and Sharing Lambda Layers](https://docs.aws.amazon.com/lambda/latest/dg/layers-create.html) -- official guide to layer packaging, versioning, and cross-account sharing
- [Lambda Execution Environment](https://docs.aws.amazon.com/lambda/latest/dg/lambda-runtime-environment.html) -- how the runtime initializes, loads layers, and manages the execution lifecycle
- [Lambda Memory and CPU Allocation](https://docs.aws.amazon.com/lambda/latest/dg/configuration-function-common.html) -- relationship between memory setting and proportional CPU allocation
- [Building Lambda Functions with Go](https://docs.aws.amazon.com/lambda/latest/dg/lambda-golang.html) -- official guide for deploying Go functions with the provided.al2023 runtime
