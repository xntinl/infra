# 81. CodePipeline Cross-Region Deployments

<!--
difficulty: intermediate
concepts: [codepipeline-cross-region, artifact-replication, cross-region-actions, multi-region-deployment, artifact-stores, s3-replication, codepipeline-stages]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: implement, analyze
prerequisites: [13-codepipeline-codebuild-lambda-deploy, 78-codebuild-environment-variables-secrets]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates CodePipeline, CodeBuild, S3 buckets in multiple regions, and Lambda functions. CodePipeline costs $1/month per active pipeline (prorated). S3 and Lambda costs are negligible. Estimated ~$0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a CodePipeline that deploys Lambda functions to multiple AWS regions using cross-region actions
2. **Configure** artifact replication buckets in target regions so deploy actions can access build artifacts
3. **Analyze** the relationship between pipeline artifact stores, cross-region artifact buckets, and the deploy actions that consume them
4. **Differentiate** between single-region and cross-region pipeline architectures and explain when cross-region deployment is necessary
5. **Debug** common cross-region failures: artifact bucket in wrong region, missing KMS key access, and IAM permission gaps

## Why This Matters

Multi-region deployment is required for low-latency global applications, disaster recovery, and compliance with data residency regulations. CodePipeline supports cross-region actions natively -- you define deploy actions that target resources in a different region, and CodePipeline handles artifact replication automatically (or you manage it explicitly).

The DVA-C02 exam tests cross-region deployment in pipeline architecture questions. Key concepts: each region that has a deploy action needs an **artifact bucket in that region** -- CodeDeploy, CloudFormation, and Lambda deploy actions can only read artifacts from S3 buckets in their own region. CodePipeline can create these buckets automatically, or you can specify them explicitly. The pipeline itself runs in one region (the pipeline region), and cross-region actions execute in their target regions using artifacts replicated to the target region's bucket.

A common exam trap: a deploy action in eu-west-1 fails because the pipeline's artifact bucket is in us-east-1. The solution is to add an artifact store for eu-west-1 and let CodePipeline replicate artifacts to it.

## Building Blocks

### Lambda Function Code

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
	return map[string]interface{}{
		"region":    os.Getenv("AWS_REGION"),
		"version":   os.Getenv("APP_VERSION"),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
		"message":   fmt.Sprintf("Hello from %s", os.Getenv("AWS_REGION")),
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

# Primary region
provider "aws" {
  region = "us-east-1"
  alias  = "primary"
}

# Secondary region for cross-region deployment
provider "aws" {
  region = "eu-west-1"
  alias  = "secondary"
}

provider "aws" {
  region = "us-east-1"
}
```

### `variables.tf`

```hcl
variable "project_name" {
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "cross-region-pipeline"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

# -- S3 source bucket (simulating CodeCommit) --
resource "aws_s3_bucket" "source" {
  provider      = aws.primary
  bucket        = "${var.project_name}-source-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "source" {
  provider = aws.primary
  bucket   = aws_s3_bucket.source.id
  versioning_configuration { status = "Enabled" }
}

# -- Artifact buckets --
resource "aws_s3_bucket" "artifacts_primary" {
  provider      = aws.primary
  bucket        = "${var.project_name}-artifacts-us-east-1-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

# =======================================================
# TODO 1 -- Artifact Bucket in Target Region
# =======================================================
# Create an S3 bucket in eu-west-1 for cross-region
# artifact replication. CodePipeline deploy actions in
# eu-west-1 can only read artifacts from a bucket in
# eu-west-1.
#
# Requirements:
#   - Use aws.secondary provider
#   - Enable force_destroy for cleanup
#   - Name: ${var.project_name}-artifacts-eu-west-1-${account_id}
```

### `cicd.tf`

```hcl
# -- IAM --
# IAM roles for pipeline (s3:*, codebuild:*, lambda:*) and CodeBuild (logs:*, s3:*) -- omitted for brevity

resource "aws_codebuild_project" "build" {
  provider     = aws.primary
  name         = "${var.project_name}-build"
  service_role = aws_iam_role.codebuild.arn

  source {
    type      = "CODEPIPELINE"
    buildspec = <<-EOT
      version: 0.2
      phases:
        build:
          commands:
            - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap app/main.go
            - zip function.zip bootstrap
      artifacts:
        files:
          - function.zip
    EOT
  }
  artifacts { type = "CODEPIPELINE" }
  environment {
    compute_type = "BUILD_GENERAL1_SMALL"
    image        = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type         = "LINUX_CONTAINER"
  }
}

# Lambda functions in both regions (same code, different providers)
# aws_lambda_function.primary  (provider = aws.primary)
# aws_lambda_function.secondary (provider = aws.secondary)

# =======================================================
# TODO 2 -- Cross-Region CodePipeline
# =======================================================
# Create the pipeline with:
#   - Source stage: S3 source
#   - Build stage: CodeBuild
#   - Deploy stage with TWO actions:
#     a) Deploy to us-east-1 Lambda (primary region)
#     b) Deploy to eu-west-1 Lambda (cross-region action)
#
# For cross-region actions, set the "region" parameter
# on the action block.
#
# The pipeline needs artifact_store blocks for BOTH regions.
# Use the "artifact_store" block with type "S3" and region:
#
#   artifact_store {
#     location = <bucket-name>
#     type     = "S3"
#     region   = "us-east-1"
#   }
#   artifact_store {
#     location = <bucket-name>
#     type     = "S3"
#     region   = "eu-west-1"
#   }
```

### `outputs.tf`

```hcl
output "pipeline_name"       { value = "TODO" }
output "source_bucket"       { value = aws_s3_bucket.source.bucket }
output "primary_function"    { value = aws_lambda_function.primary.function_name }
output "secondary_function"  { value = aws_lambda_function.secondary.function_name }
```

## Spot the Bug

A developer creates a cross-region pipeline that deploys to eu-west-1. The deploy action fails with "The deployment artifact is not available in the region eu-west-1." **What is wrong?**

```hcl
resource "aws_codepipeline" "this" {
  name     = "multi-region-deploy"
  role_arn = aws_iam_role.pipeline.arn

  artifact_store {
    location = aws_s3_bucket.artifacts_primary.bucket
    type     = "S3"
  }

  stage {
    name = "Source"
    action { /* ... */ }
  }

  stage {
    name = "Deploy"
    action {
      name            = "DeployEU"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "CloudFormation"
      version         = "1"
      region          = "eu-west-1"             # Cross-region action
      input_artifacts = ["BuildOutput"]
      configuration   = { /* ... */ }
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

The pipeline uses a single `artifact_store` block without a `region` parameter. For cross-region pipelines, you must define **separate artifact stores for each region** where actions execute.

The fix: use multiple `artifact_store` blocks, one per region:

```hcl
artifact_store {
  location = aws_s3_bucket.artifacts_us_east_1.bucket
  type     = "S3"
  region   = "us-east-1"
}

artifact_store {
  location = aws_s3_bucket.artifacts_eu_west_1.bucket
  type     = "S3"
  region   = "eu-west-1"
}
```

CodePipeline automatically replicates artifacts to each cross-region bucket. If the target region's bucket is missing, the action fails. On the exam, this is the most common cross-region pipeline question.

</details>

## Solutions

<details>
<summary>storage.tf -- TODO 1 -- Artifact Bucket in Target Region</summary>

```hcl
resource "aws_s3_bucket" "artifacts_secondary" {
  provider      = aws.secondary
  bucket        = "${var.project_name}-artifacts-eu-west-1-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}
```

Also update the pipeline IAM policy to grant S3 access to this bucket.

</details>

<details>
<summary>cicd.tf -- TODO 2 -- Cross-Region CodePipeline</summary>

```hcl
resource "aws_codepipeline" "this" {
  name     = var.project_name
  role_arn = aws_iam_role.pipeline.arn

  artifact_store {
    location = aws_s3_bucket.artifacts_primary.bucket
    type     = "S3"
    region   = "us-east-1"
  }

  artifact_store {
    location = aws_s3_bucket.artifacts_secondary.bucket
    type     = "S3"
    region   = "eu-west-1"
  }

  stage {
    name = "Source"
    action {
      name             = "S3Source"
      category         = "Source"
      owner            = "AWS"
      provider         = "S3"
      version          = "1"
      output_artifacts = ["SourceOutput"]
      configuration = {
        S3Bucket             = aws_s3_bucket.source.bucket
        S3ObjectKey          = "source.zip"
        PollForSourceChanges = "true"
      }
    }
  }

  stage {
    name = "Build"
    action {
      name             = "Build"
      category         = "Build"
      owner            = "AWS"
      provider         = "CodeBuild"
      version          = "1"
      input_artifacts  = ["SourceOutput"]
      output_artifacts = ["BuildOutput"]
      configuration = {
        ProjectName = aws_codebuild_project.build.name
      }
    }
  }

  stage {
    name = "Deploy"

    action {
      name            = "DeployPrimary"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "Lambda"
      version         = "1"
      region          = "us-east-1"
      input_artifacts = ["BuildOutput"]
      configuration = {
        FunctionName = aws_lambda_function.primary.function_name
        S3Bucket     = aws_s3_bucket.artifacts_primary.bucket
        S3Key        = "function.zip"
      }
    }

    action {
      name            = "DeploySecondary"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "Lambda"
      version         = "1"
      region          = "eu-west-1"
      input_artifacts = ["BuildOutput"]
      configuration = {
        FunctionName = aws_lambda_function.secondary.function_name
        S3Bucket     = aws_s3_bucket.artifacts_secondary.bucket
        S3Key        = "function.zip"
      }
    }
  }
}
```

Update the output:
```hcl
output "pipeline_name" { value = aws_codepipeline.this.name }
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify pipeline structure

```bash
aws codepipeline get-pipeline --name cross-region-pipeline \
  --query "pipeline.stages[*].{Stage:name,Actions:actions[*].{Name:name,Region:region}}" --output json | jq .
```

Expected: Deploy stage with two actions -- one in us-east-1 and one in eu-west-1.

### Step 3 -- Verify artifact stores

```bash
aws codepipeline get-pipeline --name cross-region-pipeline \
  --query "pipeline.artifactStores" --output json | jq .
```

Expected: artifact stores for both us-east-1 and eu-west-1.

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

You configured a cross-region CodePipeline with artifact replication. Next, you will add **manual approval actions** -- requiring human sign-off before production deployment.

## Summary

- Cross-region actions execute in a different region from the pipeline -- set `region` on the action block
- Each region with deploy actions needs its own **artifact S3 bucket** -- deploy actions cannot read from another region
- Use multiple `artifact_store` blocks (one per region) for cross-region pipelines
- CodePipeline **automatically replicates** artifacts to cross-region buckets
- If using KMS encryption, the cross-region bucket needs a KMS key in its region
- The pipeline IAM role must have S3 access to **all** artifact buckets across all regions
- Common failure: single `artifact_store` without `region` -- works single-region but fails cross-region

## Reference

- [CodePipeline Cross-Region Actions](https://docs.aws.amazon.com/codepipeline/latest/userguide/actions-create-cross-region.html)
- [Terraform aws_codepipeline](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codepipeline)
- [CodePipeline Artifact Stores](https://docs.aws.amazon.com/codepipeline/latest/userguide/welcome-introducing-artifacts.html)
- [Multi-Region Deployment Strategies](https://docs.aws.amazon.com/whitepapers/latest/aws-multi-region-fundamentals/deployment-strategies.html)

## Additional Resources

- [S3 Cross-Region Replication](https://docs.aws.amazon.com/AmazonS3/latest/userguide/replication.html) -- alternative approach using S3 CRR for artifacts
- [CodePipeline Encryption](https://docs.aws.amazon.com/codepipeline/latest/userguide/S3-artifact-encryption.html) -- KMS encryption for artifacts across regions
