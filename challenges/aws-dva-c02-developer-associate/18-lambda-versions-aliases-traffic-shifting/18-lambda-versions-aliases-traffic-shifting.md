# 18. Lambda Versions, Aliases, and Traffic Shifting

<!--
difficulty: basic
concepts: [lambda-versions, lambda-aliases, traffic-shifting, weighted-alias, publish-version, canary-deployment]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function with published versions and aliases. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the difference between `$LATEST` (mutable) and numbered versions (immutable snapshots) of a Lambda function
- **Explain** how Lambda aliases work as named pointers to specific versions and how they enable environment-based routing (dev/staging/prod)
- **Construct** a Lambda function with published versions and aliases using Terraform, including a weighted alias for traffic shifting
- **Verify** that a weighted alias routes traffic between two versions by invoking it multiple times and observing the distribution
- **Describe** why aliases are required for CodeDeploy traffic shifting, event source mappings, and API Gateway stage variables

## Why Lambda Versions, Aliases, and Traffic Shifting

Every Lambda function has a `$LATEST` version that you can update at any time. When you publish a version, Lambda takes an immutable snapshot of the code and configuration. Version 1 is frozen forever -- you cannot change it. This immutability is the foundation for safe deployments: you can always roll back to an earlier version because it is guaranteed to be identical to when it was published.

Aliases add a human-readable name on top of version numbers. Instead of pointing your API Gateway integration at version 17 and manually updating it to version 18 during each deployment, you point it at the `prod` alias. Then deployment becomes a single operation: update the alias to point to the new version. If something breaks, update the alias back to the old version.

The exam heavily tests weighted aliases for canary deployments. A weighted alias splits traffic between two versions -- for example, 90% to version 1 and 10% to version 2. This lets you test new code with a small percentage of production traffic. If errors spike, you shift 100% back to the old version. If everything looks good, you shift 100% to the new version. CodeDeploy automates this pattern with strategies like `LambdaCanary10Percent5Minutes` and `LambdaLinear10PercentEvery1Minute`.

## Step 1 -- Create the Lambda Function Code (Version 1)

### `lambda/main.go`

The function returns its version so you can identify which version is serving traffic:

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	version := os.Getenv("FUNCTION_VERSION")
	functionName := os.Getenv("AWS_LAMBDA_FUNCTION_NAME")
	qualifiedArn := os.Getenv("AWS_LAMBDA_FUNCTION_ARN") // includes qualifier if invoked via alias

	return map[string]interface{}{
		"function_name": functionName,
		"version_label": version,
		"qualified_arn": qualifiedArn,
		"message":       "Hello from version " + version,
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Project Files

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
  default     = "versions-aliases-demo"
}
```

### `build.tf`

```hcl
# -- Build the Go binary --
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

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
# -- Lambda function with publish = true --
# publish = true creates a new immutable version each time code or config changes.
# Without this, only $LATEST exists and you cannot create version-based aliases.
resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 10
  publish          = true

  environment {
    variables = {
      FUNCTION_VERSION = "v1"
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

# -- Aliases: dev, staging, prod --
# Each alias points to the current published version.
# In practice, dev might point to $LATEST while prod points to a stable version.
resource "aws_lambda_alias" "dev" {
  name             = "dev"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}

resource "aws_lambda_alias" "staging" {
  name             = "staging"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version
}

resource "aws_lambda_alias" "prod" {
  name             = "prod"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version

  # lifecycle.ignore_changes is used when CodeDeploy manages the alias.
  # For this exercise, Terraform manages it directly.
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
output "function_name"     { value = aws_lambda_function.this.function_name }
output "function_arn"      { value = aws_lambda_function.this.arn }
output "published_version" { value = aws_lambda_function.this.version }
output "dev_alias_arn"     { value = aws_lambda_alias.dev.arn }
output "staging_alias_arn" { value = aws_lambda_alias.staging.arn }
output "prod_alias_arn"    { value = aws_lambda_alias.prod.arn }
```

## Step 3 -- Build and Apply (Version 1)

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init
terraform apply -auto-approve
```

### Intermediate Verification

Check the published version:

```bash
aws lambda list-versions-by-function --function-name versions-aliases-demo \
  --query "Versions[*].{Version:Version,Description:Description}" --output table
```

You should see `$LATEST` and version `1`.

Invoke via the `prod` alias:

```bash
aws lambda invoke --function-name versions-aliases-demo --qualifier prod /dev/stdout 2>/dev/null | jq .
```

Expected: `"version_label": "v1"`

## Step 4 -- Publish Version 2 (Update the Code)

Change the `FUNCTION_VERSION` environment variable to `v2` in `lambda.tf`:

```hcl
  environment {
    variables = {
      FUNCTION_VERSION = "v2"
    }
  }
```

Apply the change:

```bash
terraform apply -auto-approve
```

Because `publish = true`, Terraform creates version 2 automatically. The aliases now point to version 2.

Verify both versions exist:

```bash
aws lambda list-versions-by-function --function-name versions-aliases-demo \
  --query "Versions[?Version!='\\$LATEST'].{Version:Version}" --output table
```

Expected: versions `1` and `2`.

## Step 5 -- Configure Weighted Traffic Shifting

Now configure the `prod` alias to split traffic: 90% to version 1, 10% to version 2. Update the `aws_lambda_alias "prod"` resource in `lambda.tf`:

```hcl
resource "aws_lambda_alias" "prod" {
  name             = "prod"
  function_name    = aws_lambda_function.this.function_name
  function_version = "2"

  routing_config {
    additional_version_weights = {
      "1" = 0.9
    }
  }
}
```

This means: the alias primarily points to version 2, but 90% of traffic is routed to version 1 via `additional_version_weights`. The result is 10% to version 2, 90% to version 1.

Apply:

```bash
terraform apply -auto-approve
```

## Step 6 -- Verify Traffic Shifting

Invoke the `prod` alias 20 times and observe the distribution:

```bash
for i in $(seq 1 20); do
  aws lambda invoke --function-name versions-aliases-demo --qualifier prod /dev/stdout 2>/dev/null | jq -r '.version_label'
done | sort | uniq -c | sort -rn
```

Expected output (approximately):

```
  18 v1
   2 v2
```

The exact numbers will vary, but roughly 90% should be `v1` and 10% `v2`.

## Common Mistakes

### 1. Forgetting publish = true on the Lambda function

Without `publish = true`, Terraform does not create numbered versions. Only `$LATEST` exists, and you cannot create aliases that point to immutable versions.

**Wrong -- no publish flag:**

```hcl
resource "aws_lambda_function" "this" {
  function_name = "my-function"
  # publish not set (defaults to false)
}

resource "aws_lambda_alias" "prod" {
  name             = "prod"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version  # This is "$LATEST", not a number
}
```

**What happens:** The alias creation fails because `$LATEST` is not a valid version for alias targets (aliases require a numbered version).

**Fix -- set publish = true:**

```hcl
resource "aws_lambda_function" "this" {
  function_name = "my-function"
  publish       = true  # Creates numbered versions on each change
}
```

### 2. Confusing the weighted alias routing direction

The `additional_version_weights` map specifies versions that receive a portion of traffic AWAY from the primary version. The primary version gets the remainder.

**Wrong understanding:**

```hcl
# "I want 90% to version 2, 10% to version 1"
resource "aws_lambda_alias" "prod" {
  function_version = "2"
  routing_config {
    additional_version_weights = {
      "1" = 0.9  # This sends 90% to version 1, not version 2!
    }
  }
}
```

**What actually happens:** Version 1 gets 90%, version 2 gets 10%. The weight value is the fraction sent TO the additional version.

**Fix -- swap the configuration:**

```hcl
resource "aws_lambda_alias" "prod" {
  function_version = "1"
  routing_config {
    additional_version_weights = {
      "2" = 0.1  # 10% to version 2, 90% to version 1
    }
  }
}
```

### 3. Trying to update a published version

Published Lambda versions are immutable. You cannot change the code or environment variables of version 1 after it is published.

**Wrong -- attempting to modify version 1:**

```bash
aws lambda update-function-configuration \
  --function-name my-function:1 \
  --environment '{"Variables":{"KEY":"new-value"}}'
```

**What happens:** Error `InvalidParameterValueException: A version cannot be updated`.

**Fix:** Publish a new version with the updated configuration. Only `$LATEST` is mutable.

## Verify What You Learned

```bash
aws lambda get-alias --function-name versions-aliases-demo --name prod \
  --query "{Version:FunctionVersion,RoutingConfig:RoutingConfig}" --output json
```

Expected: JSON showing `FunctionVersion: "2"` and a `RoutingConfig` with `AdditionalVersionWeights` containing `"1": 0.9`.

```bash
aws lambda list-aliases --function-name versions-aliases-demo \
  --query "Aliases[*].{Name:Name,Version:FunctionVersion}" --output table
```

Expected: three aliases (dev, staging, prod) with version numbers.

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

You published Lambda versions and configured aliases with weighted traffic shifting. In the next exercise, you will learn how to **deploy Lambda functions from container images** stored in Amazon ECR, including how to build a Dockerfile for a Go Lambda binary and compare ZIP versus container deployment models.

## Summary

- `$LATEST` is the **mutable** version of a Lambda function; published versions (1, 2, 3...) are **immutable** snapshots
- `publish = true` in Terraform creates a new numbered version on every code or configuration change
- **Aliases** are named pointers (dev, staging, prod) to specific versions, enabling environment-based routing
- **Weighted aliases** split traffic between two versions using `routing_config.additional_version_weights`
- The weight value specifies the fraction sent to the **additional** version; the primary version gets the remainder
- CodeDeploy uses weighted aliases to automate canary and linear deployment strategies
- Event source mappings and API Gateway integrations should target **aliases**, not version numbers, for zero-downtime deployments
- Published versions cannot be modified -- any change requires publishing a new version

## Reference

- [Lambda Versions](https://docs.aws.amazon.com/lambda/latest/dg/configuration-versions.html)
- [Lambda Aliases](https://docs.aws.amazon.com/lambda/latest/dg/configuration-aliases.html)
- [Terraform aws_lambda_alias Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_alias)
- [Shifting Traffic with Aliases](https://docs.aws.amazon.com/lambda/latest/dg/lambda-traffic-shifting-using-aliases.html)

## Additional Resources

- [CodeDeploy Lambda Deployments](https://docs.aws.amazon.com/codedeploy/latest/userguide/welcome.html#welcome-compute-platforms-lambda) -- how CodeDeploy automates weighted alias traffic shifting for canary and linear strategies
- [Using Aliases with API Gateway](https://docs.aws.amazon.com/lambda/latest/dg/configuration-aliases.html#using-aliases) -- stage variables referencing aliases for production routing
- [Lambda Version ARN Format](https://docs.aws.amazon.com/lambda/latest/dg/lambda-api-permissions-ref.html) -- difference between unqualified ARN, qualified ARN with version, and qualified ARN with alias
- [Lambda Deployment Best Practices](https://docs.aws.amazon.com/lambda/latest/dg/best-practices.html) -- recommended deployment patterns using versions and aliases
