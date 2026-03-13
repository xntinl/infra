# 78. CodeBuild Environment Variables and Secrets

<!--
difficulty: basic
concepts: [codebuild-project, environment-variables, plaintext-variables, parameter-store-reference, secrets-manager-reference, buildspec-yml, build-phases, codebuild-iam]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [07-parameter-store-appconfig-runtime-config, 77-codecommit-triggers-notifications]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a CodeBuild project, Parameter Store parameters, and a Secrets Manager secret. CodeBuild charges per build minute (~$0.005/min for small instance). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CodeBuild project with Terraform that uses environment variables from three sources: plaintext, Parameter Store, and Secrets Manager
- **Construct** a buildspec.yml file that accesses each variable type during the build phases (install, pre_build, build, post_build)
- **Configure** the IAM role permissions required for CodeBuild to read Parameter Store and Secrets Manager values
- **Explain** the difference between `PLAINTEXT`, `PARAMETER_STORE`, and `SECRETS_MANAGER` environment variable types and when to use each
- **Verify** that secret values are resolved at build time and are masked in CloudWatch Logs

## Why CodeBuild Environment Variables and Secrets

CodeBuild projects need configuration values -- database endpoints, API keys, feature flags, and credentials. Hardcoding these values in source code or buildspec files is insecure and inflexible. CodeBuild supports three environment variable types that solve this problem.

**PLAINTEXT** variables are stored directly in the project configuration -- visible in the console. **PARAMETER_STORE** variables reference SSM parameters resolved at build start time. **SECRETS_MANAGER** variables reference secrets using the format `secret-name:json-key:version-stage:version-id`, resolved at build start time.

The DVA-C02 exam tests: "build fails with AccessDenied reading a parameter" (missing `ssm:GetParameters` permission) and "developer sees secret in console" (used PLAINTEXT instead of SECRETS_MANAGER).

## Step 1 -- Create the Secrets and Parameters

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
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
  default     = "codebuild-env-demo"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

# SSM Parameter Store
resource "aws_ssm_parameter" "db_endpoint" {
  name  = "/${var.project_name}/db-endpoint"
  type  = "String"
  value = "mydb.cluster-abc123.us-east-1.rds.amazonaws.com"
}

resource "aws_ssm_parameter" "db_name" {
  name  = "/${var.project_name}/db-name"
  type  = "SecureString"
  value = "orders_production"
}

# Secrets Manager
resource "aws_secretsmanager_secret" "db_credentials" {
  name                    = "${var.project_name}/db-credentials"
  recovery_window_in_days = 0
}

resource "aws_secretsmanager_secret_version" "db_credentials" {
  secret_id     = aws_secretsmanager_secret.db_credentials.id
  secret_string = jsonencode({ username = "app_user", password = "S3cur3P@ssw0rd!" })
}
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "artifacts" {
  bucket        = "${var.project_name}-artifacts-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "codebuild_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["codebuild.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "codebuild" {
  name               = "${var.project_name}-codebuild-role"
  assume_role_policy = data.aws_iam_policy_document.codebuild_assume.json
}

data "aws_iam_policy_document" "codebuild_policy" {
  statement { # CloudWatch Logs
    actions   = ["logs:CreateLogGroup", "logs:CreateLogStream", "logs:PutLogEvents"]
    resources = ["arn:aws:logs:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:log-group:/aws/codebuild/${var.project_name}*"]
  }
  statement { # S3 artifacts
    actions   = ["s3:PutObject", "s3:GetObject", "s3:GetBucketLocation"]
    resources = [aws_s3_bucket.artifacts.arn, "${aws_s3_bucket.artifacts.arn}/*"]
  }
  statement { # SSM -- required for PARAMETER_STORE env vars
    actions   = ["ssm:GetParameters"]
    resources = ["arn:aws:ssm:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:parameter/${var.project_name}/*"]
  }
  statement { # Secrets Manager -- required for SECRETS_MANAGER env vars
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.db_credentials.arn]
  }
}

resource "aws_iam_role_policy" "codebuild" {
  name   = "codebuild-policy"
  role   = aws_iam_role.codebuild.id
  policy = data.aws_iam_policy_document.codebuild_policy.json
}
```

### `cicd.tf`

```hcl
resource "aws_codebuild_project" "this" {
  name         = var.project_name
  service_role = aws_iam_role.codebuild.arn

  source {
    type      = "NO_SOURCE"
    buildspec = file("${path.module}/buildspec.yml")
  }

  artifacts {
    type = "NO_ARTIFACTS"
  }

  environment {
    compute_type = "BUILD_GENERAL1_SMALL"
    image        = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type         = "LINUX_CONTAINER"

    # PLAINTEXT -- stored directly, visible in console
    environment_variable {
      name  = "APP_ENV"
      value = "production"
      type  = "PLAINTEXT"
    }

    environment_variable {
      name  = "APP_REGION"
      value = "us-east-1"
      type  = "PLAINTEXT"
    }

    # PARAMETER_STORE -- resolved at build time from SSM
    environment_variable {
      name  = "DB_ENDPOINT"
      value = "/${var.project_name}/db-endpoint"
      type  = "PARAMETER_STORE"
    }

    environment_variable {
      name  = "DB_NAME"
      value = "/${var.project_name}/db-name"
      type  = "PARAMETER_STORE"
    }

    # SECRETS_MANAGER -- resolved at build time from Secrets Manager
    # Format: secret-name:json-key:version-stage:version-id
    environment_variable {
      name  = "DB_USERNAME"
      value = "${var.project_name}/db-credentials:username"
      type  = "SECRETS_MANAGER"
    }

    environment_variable {
      name  = "DB_PASSWORD"
      value = "${var.project_name}/db-credentials:password"
      type  = "SECRETS_MANAGER"
    }
  }

  logs_config {
    cloudwatch_logs {
      group_name = "/aws/codebuild/${var.project_name}"
    }
  }
}

resource "aws_cloudwatch_log_group" "codebuild" {
  name              = "/aws/codebuild/${var.project_name}"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "project_name" {
  description = "CodeBuild project name"
  value       = aws_codebuild_project.this.name
}

output "artifacts_bucket" {
  description = "S3 bucket for build artifacts"
  value       = aws_s3_bucket.artifacts.bucket
}

output "log_group" {
  description = "CloudWatch log group for build logs"
  value       = aws_cloudwatch_log_group.codebuild.name
}
```

## Step 2 -- Create the buildspec.yml

Create a file named `buildspec.yml`:

```yaml
version: 0.2

phases:
  install:
    runtime-versions:
      golang: 1.21
    commands:
      - echo "=== INSTALL PHASE ==="
      - go version

  pre_build:
    commands:
      - echo "PLAINTEXT: APP_ENV=$APP_ENV APP_REGION=$APP_REGION"
      - echo "PARAMETER_STORE: DB_ENDPOINT=$DB_ENDPOINT"
      - echo "SECRETS_MANAGER: DB_USERNAME length=${#DB_USERNAME} DB_PASSWORD length=${#DB_PASSWORD}"

  build:
    commands:
      - echo "All env vars resolved -- available to build process"
      - echo "Build completed at $(date)"
```

## Step 3 -- Build and Apply

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Start a Build and Verify

Start a build:

```bash
PROJECT_NAME=$(terraform output -raw project_name)

BUILD_ID=$(aws codebuild start-build --project-name "$PROJECT_NAME" \
  --query "build.id" --output text)

echo "Build started: $BUILD_ID"
```

Wait for the build to complete:

```bash
aws codebuild batch-get-builds --ids "$BUILD_ID" \
  --query "builds[0].buildStatus" --output text
```

Expected: `SUCCEEDED` (may take 1-2 minutes).

## Common Mistakes

### 1. Missing ssm:GetParameters permission

Without `ssm:GetParameters` in the CodeBuild IAM role, builds fail at start time with "Error in DOWNLOAD_SOURCE phase: AccessDeniedException". The error occurs before any build commands run because CodeBuild resolves all environment variables before starting the build.

### 2. Wrong Secrets Manager reference format

The format is `secret-name:json-key:version-stage:version-id`. Omitting the json-key resolves the entire JSON string. To extract just the password: `my-secret:password`. Version-stage and version-id default to AWSCURRENT.

### 3. Echoing secrets in build logs

Running `echo $DB_PASSWORD` prints the secret to CloudWatch Logs. Use `${#VAR}` (string length) to verify resolution without exposing values.

## Verify What You Learned

```bash
# Verify CodeBuild project has the environment variables
aws codebuild batch-get-projects --names $(terraform output -raw project_name) \
  --query "projects[0].environment.environmentVariables[*].{Name:name,Type:type}" --output table
```

Expected: six environment variables with types PLAINTEXT, PARAMETER_STORE, and SECRETS_MANAGER.

```bash
# Verify the build succeeded
aws codebuild list-builds-for-project --project-name $(terraform output -raw project_name) \
  --query "ids[0]" --output text | xargs -I {} \
  aws codebuild batch-get-builds --ids {} --query "builds[0].buildStatus" --output text
```

Expected: `SUCCEEDED`.

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

You configured a CodeBuild project with three types of environment variables. Next, you will explore **CodeDeploy deployment configurations** -- in-place, blue/green, and Lambda deployment types with custom traffic shifting.

## Summary

- CodeBuild supports three environment variable types: **PLAINTEXT** (visible in console), **PARAMETER_STORE** (resolved from SSM at build start), and **SECRETS_MANAGER** (resolved from Secrets Manager at build start)
- All environment variables are resolved **before the build starts** -- if resolution fails, the build fails in the DOWNLOAD_SOURCE phase
- PARAMETER_STORE variables reference SSM parameter names (e.g., `/my-app/db-endpoint`) and require `ssm:GetParameters` IAM permission
- SECRETS_MANAGER variables use the format `secret-name:json-key:version-stage:version-id` and require `secretsmanager:GetSecretValue` IAM permission
- **Never echo secret values** in build commands -- they appear in CloudWatch Logs
- Buildspec phases run in order: `install`, `pre_build`, `build`, `post_build` -- post_build runs even if build fails

## Reference

- [CodeBuild Environment Variables](https://docs.aws.amazon.com/codebuild/latest/userguide/build-env-ref-env-vars.html)
- [Terraform aws_codebuild_project](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codebuild_project)
- [Buildspec Reference](https://docs.aws.amazon.com/codebuild/latest/userguide/build-spec-ref.html)
- [CodeBuild IAM Permissions](https://docs.aws.amazon.com/codebuild/latest/userguide/setting-up.html#setting-up-service-role)

## Additional Resources

- [SSM Parameter Store SecureString](https://docs.aws.amazon.com/systems-manager/latest/userguide/sysman-paramstore-securestring.html) -- encryption and access control
- [Secrets Manager Best Practices](https://docs.aws.amazon.com/secretsmanager/latest/userguide/best-practices.html) -- rotation and access control
- [CodeBuild Docker Images](https://docs.aws.amazon.com/codebuild/latest/userguide/build-env-ref-available.html) -- available managed build images
