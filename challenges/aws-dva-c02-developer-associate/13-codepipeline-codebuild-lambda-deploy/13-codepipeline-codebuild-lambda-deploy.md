# 13. CodePipeline with CodeBuild for Lambda Deployment

<!--
difficulty: advanced
concepts: [codepipeline, codebuild, buildspec, codedeploy, lambda-traffic-shifting, appspec, build-cache]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.03/hr
-->

> **AWS Cost Warning:** This exercise creates a CodePipeline, CodeBuild project, CodeDeploy application, Lambda function with alias, and an S3 source bucket. Cost is approximately $0.03/hr. CodeBuild charges per build minute (first 100 minutes/month free). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Basic understanding of Lambda aliases and versions

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** the role of each CodePipeline stage (Source, Build, Deploy) and how artifacts flow between them
- **Design** a `buildspec.yml` with correct phase structure (install, pre_build, build, post_build) and artifact output configuration
- **Implement** a complete CI/CD pipeline from S3 source through CodeBuild to CodeDeploy with Lambda traffic shifting
- **Configure** CodeDeploy with an AppSpec file for Lambda deployments, including traffic shifting strategies (Linear, Canary, AllAtOnce)
- **Analyze** build failures by examining CodeBuild logs and artifact path mismatches between pipeline stages

## Why This Matters

The DVA-C02 exam dedicates an entire domain to deployment strategies, and CodePipeline with CodeBuild is the centerpiece. Unlike GitHub Actions or GitLab CI, AWS CI/CD services have specific configuration files (buildspec.yml, appspec.yml) with rigid structure requirements that the exam tests in detail. Can you identify which buildspec phase runs even if the build fails? (post_build with `on-failure: CONTINUE`). Do you know the difference between CodeDeploy's `Linear10PercentEvery1Minute` and `Canary10Percent5Minutes`? (Linear shifts 10% every minute for 10 minutes; Canary shifts 10% immediately, waits 5 minutes, then shifts the remaining 90%).

The exam also tests artifact flow between pipeline stages. CodeBuild produces output artifacts that become input artifacts for the next stage. If the `files` path in `buildspec.yml` does not match what CodeDeploy expects, the deploy stage fails with an empty or malformed artifact -- a subtle bug that appears to work in CodeBuild but breaks downstream.

## The Challenge

Build a complete CI/CD pipeline that takes Lambda source code from S3, builds it with CodeBuild, and deploys it to Lambda using CodeDeploy with traffic shifting. Configure build caching, proper artifact handoff, and a canary deployment strategy.

### Requirements

| Requirement | Description |
|---|---|
| Source Stage | S3 bucket as source, triggered by object upload to a specific key |
| Build Stage | CodeBuild project with buildspec.yml defining all four phases |
| Deploy Stage | CodeDeploy with Lambda deployment, traffic shifting via alias |
| buildspec.yml | install (go version), pre_build (vet), build (compile + zip), post_build (create appspec) |
| AppSpec | Lambda-specific appspec.yml with function name, alias, and version references |
| Traffic Shifting | `CodeDeployDefault.LambdaLinear10PercentEvery1Minute` strategy |
| Build Cache | S3-based cache for Go module dependencies |
| Lambda Alias | `live` alias pointing to the latest version, used by CodeDeploy for shifting |

### Architecture

```
  +---------------------------------------------------------------------+
  |                        CodePipeline                                 |
  |                                                                     |
  |  +----------+     +--------------+     +-------------------+       |
  |  |  Source   |     |    Build     |     |      Deploy       |       |
  |  |          |     |              |     |                   |       |
  |  | S3 bucket|---->|  CodeBuild   |---->|   CodeDeploy      |       |
  |  | .zip     |     |  buildspec   |     |   appspec.yml     |       |
  |  |          |     |              |     |                   |       |
  |  +----------+     +--------------+     +-------------------+       |
  |       |                  |                       |                  |
  |       |          +-------+-------+       +-------+--------+        |
  |       |          |  Phases:      |       |  Traffic shift: |        |
  |       |          |  1. install   |       |  v1 (90%) --+  |        |
  |       |          |  2. pre_build |       |             |  |        |
  |       |          |  3. build     |       |  v2 (10%) --|  |        |
  |       |          |  4. post_build|       |     ...     |  |        |
  |       |          +--------------+       |  v2 (100%) <-+  |        |
  |       |                                  +----------------+        |
  +-------+------------------------------------------------------------+
          |
          v
  +--------------+     +------------------+
  |  S3 Cache    |     |  Lambda Function |
  |  (Go mods)   |     |  Alias: live     |
  |              |     |  v1 --> v2       |
  +--------------+     +------------------+

  Artifact flow:
  Source artifact (source.zip) --> Build input
  Build output artifact --> Deploy input (must contain appspec.yml + function.zip)
```

## Hints

<details>
<summary>Hint 1: buildspec.yml structure and phases</summary>

The `buildspec.yml` file defines what CodeBuild does at each phase. Each phase runs sequentially, and failures in early phases skip later phases (except `post_build` with `on-failure: CONTINUE`).

```yaml
version: 0.2

phases:
  install:
    commands:
      - echo "Verifying Go installation..."
      - go version

  pre_build:
    commands:
      - echo "Running vet..."
      - go vet ./...

  build:
    commands:
      - echo "Building Lambda function..."
      - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
      - zip function.zip bootstrap

  post_build:
    commands:
      - echo "Build completed on $(date)"
      - echo "Creating appspec..."
      - |
        cat > appspec.yml << 'APPSPEC'
        version: 0.0
        Resources:
          - MyFunction:
              Type: AWS::Lambda::Function
              Properties:
                Name: pipeline-demo-function
                Alias: live
                CurrentVersion: $CURRENT_VERSION
                TargetVersion: $TARGET_VERSION
        APPSPEC

artifacts:
  files:
    - appspec.yml
    - function.zip
  discard-paths: yes

cache:
  paths:
    - '/root/go/pkg/mod/**/*'
```

Key exam points:
- `version: 0.2` is required (not `0.1`)
- Go is pre-installed in `aws/codebuild/amazonlinux2-x86_64-standard:5.0`
- `artifacts.files` controls what goes into the output artifact -- paths are relative to the build root
- `discard-paths: yes` flattens the directory structure in the output artifact

</details>

<details>
<summary>Hint 2: CodeBuild project with S3 cache</summary>

The CodeBuild project needs an IAM role with permissions for S3 (source, artifacts, cache), CloudWatch Logs, and Lambda (for version publishing). S3 caching stores Go module dependencies between builds to speed up the install phase.

```hcl
resource "aws_codebuild_project" "this" {
  name          = "pipeline-demo-build"
  description   = "Build and package Lambda function"
  build_timeout = 10
  service_role  = aws_iam_role.codebuild.arn

  artifacts {
    type = "CODEPIPELINE"
  }

  environment {
    compute_type    = "BUILD_GENERAL1_SMALL"
    image           = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type            = "LINUX_CONTAINER"
    privileged_mode = false

    environment_variable {
      name  = "FUNCTION_NAME"
      value = aws_lambda_function.this.function_name
    }
  }

  source {
    type      = "CODEPIPELINE"
    buildspec = "buildspec.yml"
  }

  cache {
    type     = "S3"
    location = "${aws_s3_bucket.artifacts.bucket}/cache"
  }

  logs_config {
    cloudwatch_logs {
      group_name  = "/aws/codebuild/pipeline-demo"
      stream_name = ""
    }
  }
}
```

The `artifacts.type = "CODEPIPELINE"` tells CodeBuild to use the pipeline's artifact store rather than a standalone S3 location.

</details>

<details>
<summary>Hint 3: CodeDeploy for Lambda with traffic shifting</summary>

CodeDeploy for Lambda is different from EC2/ECS deployments. Instead of replacing instances, it shifts traffic between Lambda versions using a weighted alias.

```hcl
resource "aws_codedeploy_app" "this" {
  name             = "pipeline-demo-deploy"
  compute_platform = "Lambda"
}

resource "aws_codedeploy_deployment_group" "this" {
  app_name               = aws_codedeploy_app.this.name
  deployment_group_name  = "pipeline-demo-dg"
  deployment_config_name = "CodeDeployDefault.LambdaLinear10PercentEvery1Minute"
  service_role_arn       = aws_iam_role.codedeploy.arn

  deployment_style {
    deployment_type   = "BLUE_GREEN"
    deployment_option = "WITH_TRAFFIC_CONTROL"
  }

  auto_rollback_configuration {
    enabled = true
    events  = ["DEPLOYMENT_FAILURE"]
  }
}
```

Traffic shifting strategies for Lambda:
- `LambdaLinear10PercentEvery1Minute` -- 10% every minute (10 min total)
- `LambdaLinear10PercentEvery2Minutes` -- 10% every 2 minutes (20 min total)
- `LambdaLinear10PercentEvery3Minutes` -- 10% every 3 minutes (30 min total)
- `LambdaLinear10PercentEvery10Minutes` -- 10% every 10 minutes (100 min total)
- `LambdaCanary10Percent5Minutes` -- 10% immediately, wait 5 min, then 100%
- `LambdaCanary10Percent10Minutes` -- 10% immediately, wait 10 min, then 100%
- `LambdaCanary10Percent15Minutes` -- 10% immediately, wait 15 min, then 100%
- `LambdaCanary10Percent30Minutes` -- 10% immediately, wait 30 min, then 100%
- `LambdaAllAtOnce` -- 100% immediately (no gradual shift)

</details>

<details>
<summary>Hint 4: Lambda alias for CodeDeploy</summary>

CodeDeploy for Lambda requires a function alias (not `$LATEST`). The alias must point to a published version. CodeDeploy shifts traffic by adjusting the alias routing configuration between the old and new versions.

```hcl
resource "aws_lambda_function" "this" {
  function_name    = "pipeline-demo-function"
  filename         = data.archive_file.function.output_path
  source_code_hash = data.archive_file.function.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  role             = aws_iam_role.lambda.arn
  publish          = true  # Required: creates a new version on each deploy
}

resource "aws_lambda_alias" "live" {
  name             = "live"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version

  lifecycle {
    ignore_changes = [function_version, routing_config]
  }
}
```

The `lifecycle.ignore_changes` is critical: without it, Terraform would fight with CodeDeploy over the alias version. CodeDeploy updates the alias during deployment, and on the next `terraform plan`, Terraform would try to revert it.

The `publish = true` flag tells Lambda to create a new immutable version each time the function code changes.

</details>

<details>
<summary>Hint 5: CodePipeline connecting all stages</summary>

The pipeline connects Source, Build, and Deploy stages. Each stage produces or consumes named artifacts. The artifact names must match between stages.

```hcl
resource "aws_codepipeline" "this" {
  name     = "pipeline-demo"
  role_arn = aws_iam_role.pipeline.arn

  artifact_store {
    location = aws_s3_bucket.artifacts.bucket
    type     = "S3"
  }

  stage {
    name = "Source"

    action {
      name             = "S3Source"
      category         = "Source"
      owner            = "AWS"
      provider         = "S3"
      version          = "1"
      output_artifacts = ["source_output"]

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
      name             = "CodeBuild"
      category         = "Build"
      owner            = "AWS"
      provider         = "CodeBuild"
      version          = "1"
      input_artifacts  = ["source_output"]
      output_artifacts = ["build_output"]

      configuration = {
        ProjectName = aws_codebuild_project.this.name
      }
    }
  }

  stage {
    name = "Deploy"

    action {
      name            = "CodeDeploy"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "CodeDeployToLambda"
      version         = "1"
      input_artifacts = ["build_output"]

      configuration = {
        ApplicationName     = aws_codedeploy_app.this.name
        DeploymentGroupName = aws_codedeploy_deployment_group.this.deployment_group_name
      }
    }
  }
}
```

Note the artifact flow: `source_output` from Source becomes the input to Build. `build_output` from Build becomes the input to Deploy. If these names do not match, the pipeline fails at the consuming stage.

</details>

## Spot the Bug

A developer's CodePipeline successfully completes the Build stage, but the Deploy stage fails with `The deployment failed because the AppSpec file that specifies the Lambda function to deploy is missing`. The buildspec.yml looks like this:

```yaml
version: 0.2

phases:
  build:
    commands:
      - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
      - zip function.zip bootstrap
      - cp appspec.yml build/appspec.yml
      - cp function.zip build/function.zip

artifacts:
  files:
    - '**/*'
  base-directory: output
```

<details>
<summary>Explain the bug</summary>

The `artifacts.base-directory` is set to `output`, but the build commands copy files to `build/`. The artifact files are collected from the `output/` directory, which does not exist or is empty. CodeBuild still succeeds (the build phases completed), but the output artifact sent to CodePipeline contains no files.

When CodeDeploy receives this empty artifact, it cannot find `appspec.yml` and fails with the missing AppSpec error.

The fix -- align the `base-directory` with where the files are actually placed:

```yaml
artifacts:
  files:
    - appspec.yml
    - function.zip
  base-directory: build
```

Or, move the files to the `output/` directory:

```yaml
phases:
  build:
    commands:
      - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
      - zip function.zip bootstrap
      - mkdir -p output
      - cp appspec.yml output/
      - cp function.zip output/

artifacts:
  files:
    - '**/*'
  base-directory: output
```

This is a common exam scenario: the build succeeds but the deploy fails because of an artifact path mismatch. The key diagnostic is checking the CodeBuild output artifact contents, not the build logs.

</details>

## Verify What You Learned

```bash
# Verify the pipeline exists and has three stages
aws codepipeline get-pipeline --name pipeline-demo \
  --query "pipeline.stages[*].name" --output json
```

Expected: `["Source", "Build", "Deploy"]`

```bash
# Verify CodeDeploy is configured for Lambda with traffic shifting
aws deploy get-deployment-group \
  --application-name pipeline-demo-deploy \
  --deployment-group-name pipeline-demo-dg \
  --query "deploymentGroupInfo.{ComputePlatform:computePlatform,DeploymentConfig:deploymentConfigName}" \
  --output json
```

Expected: `{"ComputePlatform": "Lambda", "DeploymentConfig": "CodeDeployDefault.LambdaLinear10PercentEvery1Minute"}`

```bash
# Verify the Lambda alias exists
aws lambda get-alias --function-name pipeline-demo-function --name live \
  --query "{Version:FunctionVersion,Name:Name}" --output json
```

Expected: JSON with `Name: "live"` and a numeric version.

```bash
# Trigger the pipeline by uploading source
aws s3 cp source.zip s3://$(terraform output -raw source_bucket)/source.zip

# Check pipeline execution status
sleep 30
aws codepipeline get-pipeline-state --name pipeline-demo \
  --query "stageStates[*].{Stage:stageName,Status:latestExecution.status}" --output table
```

Expected: Source and Build stages showing `InProgress` or `Succeeded`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
# Empty S3 buckets before destroying (Terraform cannot delete non-empty buckets)
aws s3 rm s3://$(terraform output -raw source_bucket) --recursive
aws s3 rm s3://$(terraform output -raw artifacts_bucket) --recursive

terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a complete CI/CD pipeline with CodePipeline, CodeBuild, and CodeDeploy for Lambda traffic shifting. The next three exercises are **Insane-tier challenges** that combine everything from exercises 1-13 into real-world scenarios with no hints and no solutions provided.

## Summary

- **CodePipeline** orchestrates stages (Source, Build, Deploy) with artifacts flowing between them via named references
- **buildspec.yml** has four phases: `install`, `pre_build`, `build`, `post_build` -- the `post_build` phase runs even on failure when `on-failure: CONTINUE` is set
- The **artifacts** section in buildspec controls what goes into the output artifact; `base-directory` and `files` paths must match the actual build output locations
- **CodeDeploy for Lambda** uses a weighted alias to shift traffic between versions -- `publish = true` on the Lambda function is required
- **Traffic shifting strategies**: Linear shifts a fixed percentage at regular intervals; Canary shifts a small percentage immediately, waits, then shifts the rest
- **S3 cache** in CodeBuild stores dependencies between builds to speed up the install phase
- Lambda alias `lifecycle.ignore_changes` prevents Terraform from reverting CodeDeploy's version updates
- The CodeDeploy **AppSpec** for Lambda specifies the function name, alias, current version, and target version

## Reference

- [CodePipeline Concepts](https://docs.aws.amazon.com/codepipeline/latest/userguide/concepts.html)
- [CodeBuild buildspec Reference](https://docs.aws.amazon.com/codebuild/latest/userguide/build-spec-ref.html)
- [CodeDeploy for Lambda](https://docs.aws.amazon.com/codedeploy/latest/userguide/welcome.html#welcome-compute-platforms-lambda)
- [AppSpec File for Lambda](https://docs.aws.amazon.com/codedeploy/latest/userguide/reference-appspec-file-structure-resources.html#reference-appspec-file-structure-resources-lambda)
- [Terraform aws_codepipeline](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codepipeline)

## Additional Resources

- [CodeBuild Environment Images](https://docs.aws.amazon.com/codebuild/latest/userguide/build-env-ref-available.html) -- available managed images and their pre-installed runtimes
- [CodeDeploy Deployment Configurations](https://docs.aws.amazon.com/codedeploy/latest/userguide/deployment-configurations.html) -- complete list of predefined traffic shifting strategies for Lambda, ECS, and EC2
- [CodePipeline S3 Source Action](https://docs.aws.amazon.com/codepipeline/latest/userguide/action-reference-S3.html) -- S3 source configuration, polling vs CloudTrail event detection
- [Lambda Versions and Aliases](https://docs.aws.amazon.com/lambda/latest/dg/configuration-aliases.html) -- how aliases, versions, and routing configurations work together

<details>
<summary>Full Solution</summary>

### File Structure

```
13-codepipeline-codebuild-lambda-deploy/
├── main.tf
├── source/
│   ├── main.go
│   ├── go.mod
│   └── buildspec.yml
└── initial_lambda/
    ├── main.go
    └── go.mod
```

### `initial_lambda/main.go`

This is the initial version deployed by Terraform before the pipeline takes over:

```go
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"version": "1.0.0",
		"message": "Initial deployment via Terraform",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `source/main.go`

This is the updated version that the pipeline will deploy:

```go
package main

import (
	"context"
	"encoding/json"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]string{
		"version": "2.0.0",
		"message": "Deployed via CodePipeline with traffic shifting",
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `source/go.mod`

```
module pipeline-demo

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

### `source/buildspec.yml`

```yaml
version: 0.2

phases:
  install:
    commands:
      - echo "Verifying Go installation..."
      - go version

  pre_build:
    commands:
      - echo "Pre-build validation..."
      - go vet ./...
      - echo "Vet check passed"

  build:
    commands:
      - echo "Building Lambda function..."
      - go mod tidy
      - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
      - zip function.zip bootstrap
      - echo "Package created: $(ls -la function.zip)"

  post_build:
    commands:
      - echo "Build completed on $(date)"
      - echo "Publishing new Lambda version..."
      - |
        NEW_VERSION=$(aws lambda update-function-code \
          --function-name $FUNCTION_NAME \
          --zip-file fileb://function.zip \
          --publish \
          --query 'Version' --output text)
      - echo "Published version: $NEW_VERSION"
      - |
        CURRENT_VERSION=$(aws lambda get-alias \
          --function-name $FUNCTION_NAME \
          --name live \
          --query 'FunctionVersion' --output text)
      - echo "Current live version: $CURRENT_VERSION"
      - |
        cat > appspec.yml << EOF
        version: 0.0
        Resources:
          - MyFunction:
              Type: AWS::Lambda::Function
              Properties:
                Name: $FUNCTION_NAME
                Alias: live
                CurrentVersion: "$CURRENT_VERSION"
                TargetVersion: "$NEW_VERSION"
        EOF
      - echo "AppSpec created:"
      - cat appspec.yml

artifacts:
  files:
    - appspec.yml
    - function.zip
  discard-paths: yes

cache:
  paths:
    - '/root/go/pkg/mod/**/*'
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
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
  description = "Project name for resource naming"
  type        = string
  default     = "pipeline-demo"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "source" {
  bucket        = "${var.project_name}-source-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "source" {
  bucket = aws_s3_bucket.source.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = "${var.project_name}-artifacts-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}
```

### `iam.tf`

```hcl
# -- Lambda IAM --
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

# -- CodeBuild IAM --
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
  statement {
    actions = [
      "logs:CreateLogGroup",
      "logs:CreateLogStream",
      "logs:PutLogEvents"
    ]
    resources = ["*"]
  }

  statement {
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:GetBucketLocation"
    ]
    resources = [
      "${aws_s3_bucket.source.arn}/*",
      "${aws_s3_bucket.artifacts.arn}/*"
    ]
  }

  statement {
    actions = [
      "lambda:UpdateFunctionCode",
      "lambda:GetAlias"
    ]
    resources = [
      aws_lambda_function.this.arn,
      "${aws_lambda_function.this.arn}:*"
    ]
  }
}

resource "aws_iam_role_policy" "codebuild" {
  name   = "codebuild-policy"
  role   = aws_iam_role.codebuild.id
  policy = data.aws_iam_policy_document.codebuild_policy.json
}

# -- CodeDeploy IAM --
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

# -- CodePipeline IAM --
data "aws_iam_policy_document" "pipeline_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["codepipeline.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "pipeline" {
  name               = "${var.project_name}-pipeline-role"
  assume_role_policy = data.aws_iam_policy_document.pipeline_assume.json
}

data "aws_iam_policy_document" "pipeline_policy" {
  statement {
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:GetBucketVersioning",
      "s3:GetBucketLocation"
    ]
    resources = [
      "${aws_s3_bucket.source.arn}/*",
      "${aws_s3_bucket.artifacts.arn}/*"
    ]
  }

  statement {
    actions = [
      "codebuild:BatchGetBuilds",
      "codebuild:StartBuild"
    ]
    resources = [aws_codebuild_project.this.arn]
  }

  statement {
    actions = [
      "codedeploy:CreateDeployment",
      "codedeploy:GetDeployment",
      "codedeploy:GetDeploymentConfig",
      "codedeploy:GetApplicationRevision",
      "codedeploy:RegisterApplicationRevision"
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "pipeline" {
  name   = "pipeline-policy"
  role   = aws_iam_role.pipeline.id
  policy = data.aws_iam_policy_document.pipeline_policy.json
}
```

### `lambda.tf`

```hcl
# NOTE: Build the initial Go binary before applying:
#   cd initial_lambda && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && zip ../build/initial_lambda.zip bootstrap && cd ..
data "archive_file" "initial_lambda" {
  type        = "zip"
  source_file = "${path.module}/initial_lambda/bootstrap"
  output_path = "${path.module}/build/initial_lambda.zip"
}

resource "aws_lambda_function" "this" {
  function_name    = "${var.project_name}-function"
  filename         = data.archive_file.initial_lambda.output_path
  source_code_hash = data.archive_file.initial_lambda.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  role             = aws_iam_role.lambda.arn
  timeout          = 10
  publish          = true

  depends_on = [
    aws_iam_role_policy_attachment.lambda_basic,
    aws_cloudwatch_log_group.lambda
  ]
}

resource "aws_lambda_alias" "live" {
  name             = "live"
  function_name    = aws_lambda_function.this.function_name
  function_version = aws_lambda_function.this.version

  lifecycle {
    ignore_changes = [function_version, routing_config]
  }
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "lambda" {
  name              = "/aws/lambda/${var.project_name}-function"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "codebuild" {
  name              = "/aws/codebuild/${var.project_name}"
  retention_in_days = 1
}
```

### `cicd.tf`

```hcl
# -- CodeBuild Project --
resource "aws_codebuild_project" "this" {
  name          = "${var.project_name}-build"
  description   = "Build and package Lambda function"
  build_timeout = 10
  service_role  = aws_iam_role.codebuild.arn

  artifacts {
    type = "CODEPIPELINE"
  }

  environment {
    compute_type    = "BUILD_GENERAL1_SMALL"
    image           = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type            = "LINUX_CONTAINER"
    privileged_mode = false

    environment_variable {
      name  = "FUNCTION_NAME"
      value = aws_lambda_function.this.function_name
    }
  }

  source {
    type      = "CODEPIPELINE"
    buildspec = "buildspec.yml"
  }

  cache {
    type     = "S3"
    location = "${aws_s3_bucket.artifacts.bucket}/cache"
  }

  logs_config {
    cloudwatch_logs {
      group_name  = aws_cloudwatch_log_group.codebuild.name
      stream_name = ""
    }
  }
}

# -- CodeDeploy --
resource "aws_codedeploy_app" "this" {
  name             = "${var.project_name}-deploy"
  compute_platform = "Lambda"
}

resource "aws_codedeploy_deployment_group" "this" {
  app_name               = aws_codedeploy_app.this.name
  deployment_group_name  = "${var.project_name}-dg"
  deployment_config_name = "CodeDeployDefault.LambdaLinear10PercentEvery1Minute"
  service_role_arn       = aws_iam_role.codedeploy.arn

  deployment_style {
    deployment_type   = "BLUE_GREEN"
    deployment_option = "WITH_TRAFFIC_CONTROL"
  }

  auto_rollback_configuration {
    enabled = true
    events  = ["DEPLOYMENT_FAILURE"]
  }
}

# -- CodePipeline --
resource "aws_codepipeline" "this" {
  name     = var.project_name
  role_arn = aws_iam_role.pipeline.arn

  artifact_store {
    location = aws_s3_bucket.artifacts.bucket
    type     = "S3"
  }

  stage {
    name = "Source"

    action {
      name             = "S3Source"
      category         = "Source"
      owner            = "AWS"
      provider         = "S3"
      version          = "1"
      output_artifacts = ["source_output"]

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
      name             = "CodeBuild"
      category         = "Build"
      owner            = "AWS"
      provider         = "CodeBuild"
      version          = "1"
      input_artifacts  = ["source_output"]
      output_artifacts = ["build_output"]

      configuration = {
        ProjectName = aws_codebuild_project.this.name
      }
    }
  }

  stage {
    name = "Deploy"

    action {
      name            = "CodeDeploy"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "CodeDeployToLambda"
      version         = "1"
      input_artifacts = ["build_output"]

      configuration = {
        ApplicationName     = aws_codedeploy_app.this.name
        DeploymentGroupName = aws_codedeploy_deployment_group.this.deployment_group_name
      }
    }
  }
}
```

### `outputs.tf`

```hcl
output "source_bucket" {
  value = aws_s3_bucket.source.bucket
}

output "artifacts_bucket" {
  value = aws_s3_bucket.artifacts.bucket
}

output "pipeline_name" {
  value = aws_codepipeline.this.name
}

output "function_name" {
  value = aws_lambda_function.this.function_name
}

output "alias_arn" {
  value = aws_lambda_alias.live.arn
}
```

### Triggering the Pipeline

```bash
# Deploy infrastructure
terraform init && terraform apply -auto-approve

# Package the source code
cd source && zip -r ../source.zip . && cd ..

# Upload to S3 (triggers the pipeline)
aws s3 cp source.zip s3://$(terraform output -raw source_bucket)/source.zip

# Monitor pipeline progress
aws codepipeline get-pipeline-state --name pipeline-demo \
  --query "stageStates[*].{Stage:stageName,Status:latestExecution.status}" \
  --output table

# After deployment completes, test the new version
aws lambda invoke --function-name pipeline-demo-function:live \
  --payload '{}' /dev/stdout 2>/dev/null | jq -r '.body' | jq .
```

</details>
