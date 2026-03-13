# 19. Lambda Container Images Deployment

<!--
difficulty: basic
concepts: [lambda-container-image, ecr, dockerfile, container-deployment, zip-vs-container, image-uri]
tools: [terraform, aws-cli, docker]
estimated_time: 35m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function deployed from a container image and an ECR repository. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). ECR storage is $0.10/GB/month. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)
- Docker installed and running (for building the container image)

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the two Lambda deployment models (ZIP archive vs container image) and the maximum size limits for each (50 MB zipped / 250 MB unzipped vs 10 GB container)
- **Construct** a Dockerfile that packages a Go binary for Lambda using the `provided.al2023` base image
- **Explain** the role of the Runtime Interface Client (RIC) in container-based Lambda functions and why Go functions include it via the `aws-lambda-go` SDK
- **Verify** the deployed container image by invoking the Lambda function and inspecting the ECR repository
- **Describe** when to choose container deployment over ZIP (large dependencies, custom runtimes, existing CI/CD Docker pipelines, images > 50 MB)

## Why Lambda Container Images

Lambda originally supported only ZIP deployments with a 250 MB unzipped limit. Container image support (launched December 2020) raises this to 10 GB, enabling workloads that bundle large dependencies like machine learning models, scientific libraries, or custom binaries. You package your function as a Docker image, push it to ECR, and point Lambda at the image URI.

The key difference for the exam: container-based Lambda functions must include the Lambda Runtime Interface Client (RIC) to communicate with the Lambda execution environment. For Go, the `aws-lambda-go` SDK embeds the RIC, so your code is identical whether deploying as ZIP or container. For other languages, you either use an AWS-provided base image (which includes the RIC) or install the RIC manually.

The exam tests several container-specific details: the image must be in ECR (not Docker Hub), the function's `package_type` must be set to `Image` (not `Zip`), and you cannot specify `handler` or `runtime` when using container deployment -- those are baked into the image via the Dockerfile's `CMD` and `ENTRYPOINT`. Understanding these constraints helps you eliminate wrong answers quickly.

## Step 1 -- Create the Lambda Function Code

### `lambda/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"os"
	"runtime"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	return map[string]interface{}{
		"deployment_type": "container-image",
		"go_version":      runtime.Version(),
		"go_arch":         runtime.GOARCH,
		"go_os":           runtime.GOOS,
		"function_name":   os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"memory_size":     os.Getenv("AWS_LAMBDA_FUNCTION_MEMORY_SIZE"),
		"message":         "Hello from a container-based Lambda function",
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module container-lambda-demo

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

## Step 2 -- Create the Dockerfile

### `Dockerfile`

This uses a multi-stage build: the first stage compiles the Go binary, the second stage copies it into the AWS-provided Lambda base image:

```dockerfile
# Stage 1: Build the Go binary
FROM golang:1.21-alpine AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY main.go .
RUN GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -tags lambda.norpc -o bootstrap main.go

# Stage 2: Copy into the Lambda base image
# The AWS base image provides the Lambda Runtime Interface Emulator (RIE)
# and sets ENTRYPOINT to the Lambda runtime bootstrap process.
FROM public.ecr.aws/lambda/provided:al2023-arm64

# Copy the compiled binary to the Lambda task root
COPY --from=builder /app/bootstrap ${LAMBDA_TASK_ROOT}/bootstrap

# CMD sets the handler -- for custom runtimes, the binary name
CMD ["bootstrap"]
```

Run `go mod tidy` to generate `go.sum`:

```bash
go mod tidy
```

## Step 3 -- Create the Terraform Project Files

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
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
  default     = "container-lambda-demo"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

locals {
  image_tag  = "latest"
  account_id = data.aws_caller_identity.current.account_id
  region     = data.aws_region.current.name
  ecr_url    = "${local.account_id}.dkr.ecr.${local.region}.amazonaws.com"
}

# -- ECR Repository --
resource "aws_ecr_repository" "this" {
  name                 = var.project_name
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

# -- IAM role --
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

resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}

# -- Lambda function from container image --
# Note: package_type = "Image" means no handler or runtime attributes.
# The handler is defined in the Dockerfile CMD, and the runtime is the
# base image itself (provided.al2023).
resource "aws_lambda_function" "this" {
  function_name = var.project_name
  role          = aws_iam_role.this.arn
  package_type  = "Image"
  image_uri     = "${aws_ecr_repository.this.repository_url}:${local.image_tag}"
  architectures = ["arm64"]
  memory_size   = 256
  timeout       = 30

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "ecr_repo_url"  { value = aws_ecr_repository.this.repository_url }
output "ecr_repo_name" { value = aws_ecr_repository.this.name }
output "image_uri"     { value = "${aws_ecr_repository.this.repository_url}:${local.image_tag}" }
```

## Step 4 -- Build, Push, and Deploy

First, build and push the Docker image to ECR:

```bash
# Authenticate Docker with ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin $(aws sts get-caller-identity --query Account --output text).dkr.ecr.us-east-1.amazonaws.com

# Create the ECR repository first (Terraform will manage it, but we need it for the push)
terraform init
terraform apply -target=aws_ecr_repository.this -auto-approve

# Build the image
ECR_URL=$(terraform output -raw ecr_repo_url)
docker build --platform linux/arm64 -t container-lambda-demo .

# Tag and push
docker tag container-lambda-demo:latest ${ECR_URL}:latest
docker push ${ECR_URL}:latest

# Now apply the full configuration (Lambda function referencing the pushed image)
terraform apply -auto-approve
```

### Intermediate Verification

Confirm the ECR image exists:

```bash
aws ecr describe-images --repository-name container-lambda-demo \
  --query "imageDetails[*].{Tags:imageTags,Size:imageSizeInBytes,Pushed:imagePushedAt}" --output table
```

## Step 5 -- Invoke the Container-Based Function

```bash
aws lambda invoke --function-name container-lambda-demo --payload '{}' /dev/stdout 2>/dev/null | jq .
```

Expected output:

```json
{
  "deployment_type": "container-image",
  "go_version": "go1.21.x",
  "go_arch": "arm64",
  "go_os": "linux",
  "function_name": "container-lambda-demo",
  "memory_size": "256",
  "message": "Hello from a container-based Lambda function"
}
```

## Step 6 -- Compare ZIP vs Container Deployment

Inspect the function configuration to see the container-specific fields:

```bash
aws lambda get-function --function-name container-lambda-demo \
  --query "{PackageType:Configuration.PackageType,ImageUri:Code.ImageUri,Runtime:Configuration.Runtime,Handler:Configuration.Handler}" --output json
```

Expected:

```json
{
  "PackageType": "Image",
  "ImageUri": "123456789012.dkr.ecr.us-east-1.amazonaws.com/container-lambda-demo:latest@sha256:...",
  "Runtime": null,
  "Handler": null
}
```

Notice that `Runtime` and `Handler` are null -- they are not applicable for container-based functions.

## Common Mistakes

### 1. Specifying handler and runtime with container deployment

When `package_type = "Image"`, you cannot set `handler` or `runtime`. These are defined in the Dockerfile.

**Wrong -- mixing ZIP and container attributes:**

```hcl
resource "aws_lambda_function" "this" {
  package_type = "Image"
  image_uri    = "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-func:latest"
  handler      = "bootstrap"         # NOT allowed with Image package type
  runtime      = "provided.al2023"   # NOT allowed with Image package type
}
```

**What happens:** `terraform apply` fails with `InvalidParameterValueException`.

**Fix -- remove handler and runtime:**

```hcl
resource "aws_lambda_function" "this" {
  package_type = "Image"
  image_uri    = "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-func:latest"
  # handler and runtime are defined in the Dockerfile
}
```

### 2. Using a non-ECR image URI

Lambda container images must be stored in Amazon ECR. You cannot use Docker Hub, GitHub Container Registry, or other registries.

**Wrong -- Docker Hub image:**

```hcl
image_uri = "docker.io/myuser/my-lambda:latest"
```

**What happens:** `terraform apply` fails with `InvalidParameterValueException: Source image must be from ECR`.

**Fix -- use an ECR URI:**

```hcl
image_uri = "123456789012.dkr.ecr.us-east-1.amazonaws.com/my-lambda:latest"
```

### 3. Forgetting to include the Runtime Interface Client

For custom runtimes, the container must include the Lambda Runtime Interface Client. Go's `aws-lambda-go` SDK includes it automatically, but other languages require explicit installation or must use an AWS-provided base image.

**Wrong -- bare Alpine image without RIC:**

```dockerfile
FROM alpine:3.19
COPY bootstrap /usr/local/bin/
CMD ["bootstrap"]
```

**What happens:** The function fails at startup with `Runtime.InvalidEntrypoint` because the Lambda execution environment cannot find the RIC bootstrap.

**Fix -- use the AWS base image or install the RIC:**

```dockerfile
FROM public.ecr.aws/lambda/provided:al2023-arm64
COPY bootstrap ${LAMBDA_TASK_ROOT}/bootstrap
CMD ["bootstrap"]
```

## Verify What You Learned

```bash
aws lambda get-function-configuration --function-name container-lambda-demo --query "PackageType" --output text
```

Expected: `Image`

```bash
aws ecr describe-repositories --repository-names container-lambda-demo --query "repositories[0].repositoryUri" --output text
```

Expected: an ECR URI like `123456789012.dkr.ecr.us-east-1.amazonaws.com/container-lambda-demo`

```bash
aws lambda invoke --function-name container-lambda-demo --payload '{}' /dev/stdout 2>/dev/null | jq -r '.deployment_type'
```

Expected: `container-image`

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

You deployed a Lambda function from a container image stored in ECR. In the next exercise, you will learn how to **configure provisioned concurrency to eliminate cold starts** and measure the latency difference between cold and warm function invocations.

## Summary

- Lambda supports two deployment models: **ZIP archive** (50 MB zipped / 250 MB unzipped) and **container image** (up to 10 GB)
- Container images must be stored in **Amazon ECR** -- no other registries are supported
- Set `package_type = "Image"` and `image_uri` on the Lambda function; do NOT set `handler` or `runtime` (these are in the Dockerfile)
- The **Runtime Interface Client (RIC)** is required in container images; Go's `aws-lambda-go` SDK includes it automatically
- AWS provides **base images** (`public.ecr.aws/lambda/provided:al2023`) that include the RIC and Runtime Interface Emulator for local testing
- Multi-stage Docker builds keep the final image small by separating the build environment from the runtime
- Choose container deployment when you have **large dependencies** (>250 MB), **custom runtimes**, or an existing **Docker-based CI/CD pipeline**

## Reference

- [Lambda Container Image Deployment](https://docs.aws.amazon.com/lambda/latest/dg/images-create.html)
- [AWS Lambda Base Images](https://docs.aws.amazon.com/lambda/latest/dg/runtimes-images.html)
- [Terraform aws_ecr_repository Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ecr_repository)
- [Terraform aws_lambda_function Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function)

## Additional Resources

- [Lambda Container Image Best Practices](https://docs.aws.amazon.com/lambda/latest/dg/images-create.html#images-create-best-practices) -- layer ordering, image size optimization, and caching strategies
- [Testing Lambda Container Images Locally](https://docs.aws.amazon.com/lambda/latest/dg/images-test.html) -- using the Runtime Interface Emulator (RIE) for local testing
- [ECR Image Scanning](https://docs.aws.amazon.com/AmazonECR/latest/userguide/image-scanning.html) -- automated vulnerability scanning for container images
- [Lambda Deployment Package Comparison](https://docs.aws.amazon.com/lambda/latest/dg/gettingstarted-package.html) -- detailed comparison of ZIP and container deployment models
