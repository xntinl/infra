# 57. Secrets Manager with Automatic Rotation

<!--
difficulty: basic
concepts: [secrets-manager, secret-rotation, rotation-lambda, rds-credentials, rotation-schedule, secret-versions, staging-labels]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Secrets Manager secret ($0.40/month), a Lambda rotation function, and associated IAM resources. Cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Configure** a Secrets Manager secret with a rotation schedule using Terraform
- **Construct** a Go Lambda rotation function that implements the four-step rotation process (createSecret, setSecret, testSecret, finishSecret)
- **Explain** how staging labels (`AWSCURRENT`, `AWSPENDING`) track secret versions during rotation
- **Retrieve** secrets from Secrets Manager using the AWS CLI and Go SDK
- **Describe** the difference between automatic rotation and manual secret updates

## Why Secrets Manager Automatic Rotation

Hardcoded credentials in code or config files are a security risk. Secrets Manager centralizes credential storage and provides an API for retrieval, so applications fetch the current credential at runtime instead of embedding it. Automatic rotation goes further -- Secrets Manager invokes a Lambda function on a schedule to generate a new credential, update the target service, test it, and swap it as the current version.

The DVA-C02 exam tests the rotation lifecycle. A rotation Lambda implements four steps: (1) `createSecret` -- generates a new credential and stores it as `AWSPENDING`; (2) `setSecret` -- applies the new credential to the target service; (3) `testSecret` -- verifies the new credential works; (4) `finishSecret` -- moves the `AWSCURRENT` label to the new version and `AWSPREVIOUS` to the old version. Understanding these steps is essential for exam questions about "what happens when rotation fails at step X."

## Step 1 -- Create the Rotation Lambda Code

### `rotation/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var smClient *secretsmanager.Client

func init() {
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	smClient = secretsmanager.NewFromConfig(cfg)
}

type RotationEvent struct {
	Step string `json:"Step"`; SecretId string `json:"SecretId"`; ClientRequestToken string `json:"ClientRequestToken"`
}

type DBCredential struct {
	Username string `json:"username"`; Password string `json:"password"`
	Host string `json:"host"`; Port int `json:"port"`; DBName string `json:"dbname"`
}

func handler(ctx context.Context, event RotationEvent) error {
	switch event.Step {
	case "createSecret":
		// Get AWSCURRENT, generate new password, store as AWSPENDING
		current, _ := smClient.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(event.SecretId), VersionStage: aws.String("AWSCURRENT"),
		})
		var cred DBCredential
		json.Unmarshal([]byte(*current.SecretString), &cred)
		cred.Password = generatePassword(32)
		newSecret, _ := json.Marshal(cred)
		_, err := smClient.PutSecretValue(ctx, &secretsmanager.PutSecretValueInput{
			SecretId: aws.String(event.SecretId), ClientRequestToken: aws.String(event.ClientRequestToken),
			SecretString: aws.String(string(newSecret)), VersionStages: []string{"AWSPENDING"},
		})
		return err
	case "setSecret":
		// In production: ALTER USER with new password on RDS
		fmt.Println("setSecret: apply new credential to database (simulated)")
		return nil
	case "testSecret":
		// Verify AWSPENDING credential works (connect to DB in production)
		_, err := smClient.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{
			SecretId: aws.String(event.SecretId), VersionId: aws.String(event.ClientRequestToken),
			VersionStage: aws.String("AWSPENDING"),
		})
		return err
	case "finishSecret":
		// Move AWSCURRENT label from old version to new version
		meta, _ := smClient.DescribeSecret(ctx, &secretsmanager.DescribeSecretInput{
			SecretId: aws.String(event.SecretId),
		})
		for vid, stages := range meta.VersionIdsToStages {
			for _, s := range stages {
				if s == "AWSCURRENT" && vid != event.ClientRequestToken {
					_, err := smClient.UpdateSecretVersionStage(ctx, &secretsmanager.UpdateSecretVersionStageInput{
						SecretId: aws.String(event.SecretId), VersionStage: aws.String("AWSCURRENT"),
						MoveToVersionId: aws.String(event.ClientRequestToken), RemoveFromVersionId: aws.String(vid),
					})
					return err
				}
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown step: %s", event.Step)
	}
}

func generatePassword(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!@#$%^&*"
	p := make([]byte, length)
	for i := range p { p[i] = chars[r.Intn(len(chars))] }
	return string(p)
}

func main() { lambda.Start(handler) }
```

## Step 2 -- Create the Terraform Configuration

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
  default     = "secrets-rotation-demo"
}
```

### `secrets.tf`

```hcl
resource "aws_secretsmanager_secret" "db_creds" {
  name = "${var.project_name}-db-credentials"
}

resource "aws_secretsmanager_secret_version" "initial" {
  secret_id = aws_secretsmanager_secret.db_creds.id
  secret_string = jsonencode({
    username = "app_user", password = "initial-password-change-me"
    host = "mydb.cluster-abc123.us-east-1.rds.amazonaws.com", port = 5432, dbname = "orders"
  })
}

resource "aws_secretsmanager_secret_rotation" "db_creds" {
  secret_id = aws_secretsmanager_secret.db_creds.id; rotation_lambda_arn = aws_lambda_function.rotation.arn
  rotation_rules { automatically_after_days = 30 }
  depends_on = [aws_lambda_permission.secrets_manager]
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

resource "aws_iam_role" "rotation" {
  name               = "${var.project_name}-rotation-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "rotation_basic" {
  role       = aws_iam_role.rotation.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

data "aws_iam_policy_document" "rotation_permissions" {
  statement {
    actions   = ["secretsmanager:DescribeSecret", "secretsmanager:GetSecretValue",
                 "secretsmanager:PutSecretValue", "secretsmanager:UpdateSecretVersionStage"]
    resources = [aws_secretsmanager_secret.db_creds.arn]
  }
  statement { actions = ["secretsmanager:GetRandomPassword"]; resources = ["*"] }
}

resource "aws_iam_role_policy" "rotation_permissions" {
  name = "rotation-permissions"; role = aws_iam_role.rotation.id
  policy = data.aws_iam_policy_document.rotation_permissions.json
}
```

### `build.tf`

```hcl
resource "null_resource" "rotation_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/rotation/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/rotation"
  }
}

data "archive_file" "rotation" {
  type = "zip"; source_file = "${path.module}/rotation/bootstrap"
  output_path = "${path.module}/build/rotation.zip"; depends_on = [null_resource.rotation_build]
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "rotation" {
  name = "/aws/lambda/${var.project_name}-rotation"; retention_in_days = 1
}

resource "aws_lambda_function" "rotation" {
  function_name = "${var.project_name}-rotation"; filename = data.archive_file.rotation.output_path
  source_code_hash = data.archive_file.rotation.output_base64sha256
  handler = "bootstrap"; runtime = "provided.al2023"; architectures = ["arm64"]
  role = aws_iam_role.rotation.arn; timeout = 60
  depends_on = [aws_iam_role_policy_attachment.rotation_basic, aws_cloudwatch_log_group.rotation]
}

resource "aws_lambda_permission" "secrets_manager" {
  statement_id = "AllowSecretsManagerInvoke"; action = "lambda:InvokeFunction"
  function_name = aws_lambda_function.rotation.function_name
  principal = "secretsmanager.amazonaws.com"
}
```

### `outputs.tf`

```hcl
output "secret_name" { value = aws_secretsmanager_secret.db_creds.name }
output "secret_arn"  { value = aws_secretsmanager_secret.db_creds.arn }
output "rotation_function" { value = aws_lambda_function.rotation.function_name }
```

## Step 3 -- Build and Apply

```bash
cd rotation && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init
terraform apply -auto-approve
```

## Step 4 -- Retrieve the Secret and Trigger Rotation

```bash
# Get the current secret value
aws secretsmanager get-secret-value \
  --secret-id secrets-rotation-demo-db-credentials \
  --query "SecretString" --output text | jq .

# Trigger immediate rotation (does not wait for the 30-day schedule)
aws secretsmanager rotate-secret --secret-id secrets-rotation-demo-db-credentials
sleep 15

# Get the new secret value (password should have changed)
aws secretsmanager get-secret-value \
  --secret-id secrets-rotation-demo-db-credentials \
  --query "SecretString" --output text | jq .
```

## Common Mistakes

### 1. Forgetting the Lambda permission for Secrets Manager

Without `aws_lambda_permission`, Secrets Manager cannot invoke the rotation Lambda. Rotation is configured but never executes.

**Fix:** Add the permission with principal `secretsmanager.amazonaws.com`.

### 2. Not handling the AWSPENDING staging label correctly

If the rotation Lambda puts the new secret value without specifying `VersionStages: ["AWSPENDING"]`, the secret is stored but not marked as pending, breaking the rotation lifecycle.

### 3. Applications caching the secret value

If your application caches the secret string in memory and never refreshes it, rotation breaks the application. Always use the Secrets Manager SDK to fetch the current value, with appropriate caching (e.g., 5-minute TTL).

## Verify What You Learned

```bash
# Verify rotation is enabled and schedule is 30 days
aws secretsmanager describe-secret \
  --secret-id secrets-rotation-demo-db-credentials \
  --query "{Enabled:RotationEnabled,Days:RotationRules.AutomaticallyAfterDays}" --output json
```

Expected: `{"Enabled": true, "Days": 30}`

```bash
# Verify secret has AWSCURRENT and AWSPREVIOUS versions after rotation
aws secretsmanager describe-secret \
  --secret-id secrets-rotation-demo-db-credentials \
  --query "VersionIdsToStages" --output json
```

Expected: two version IDs, one with `AWSCURRENT` and one with `AWSPREVIOUS`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
# Force delete the secret (skip the 7-day recovery window)
aws secretsmanager delete-secret \
  --secret-id secrets-rotation-demo-db-credentials \
  --force-delete-without-recovery 2>/dev/null

terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured Secrets Manager with automatic rotation and a Go Lambda rotation function. In the next exercise, you will explore **KMS key types** -- creating symmetric keys for encrypt/decrypt and asymmetric keys for sign/verify operations.

## Summary

- **Secrets Manager** centralizes credential storage with an API for retrieval, decoupling secrets from application code
- **Automatic rotation** invokes a Lambda on a schedule to generate, apply, test, and activate new credentials
- The rotation Lambda implements **four steps**: createSecret, setSecret, testSecret, finishSecret
- **Staging labels** track versions: `AWSCURRENT` (active), `AWSPENDING` (rotating), `AWSPREVIOUS` (last active)
- If any step fails, `AWSCURRENT` remains unchanged -- applications are not disrupted
- The rotation Lambda needs permissions for both Secrets Manager and the target service (e.g., RDS)
- `aws_lambda_permission` with principal `secretsmanager.amazonaws.com` is required to invoke the rotation function

## Reference

- [Secrets Manager Rotation](https://docs.aws.amazon.com/secretsmanager/latest/userguide/rotating-secrets.html)
- [Rotation Lambda Function](https://docs.aws.amazon.com/secretsmanager/latest/userguide/rotating-secrets-lambda-function-overview.html)
- [Terraform aws_secretsmanager_secret_rotation](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret_rotation)

## Additional Resources

- [Rotation Function Templates](https://docs.aws.amazon.com/secretsmanager/latest/userguide/reference_available-rotation-templates.html) -- AWS-provided templates for RDS, Redshift, DocumentDB
- [Multi-User Rotation](https://docs.aws.amazon.com/secretsmanager/latest/userguide/rotating-secrets_strategies.html#rotating-secrets-two-users) -- alternating user strategy for zero-downtime rotation
- [Secrets Manager vs Parameter Store](https://docs.aws.amazon.com/systems-manager/latest/userguide/integration-ps-secretsmanager.html) -- when to use each service
