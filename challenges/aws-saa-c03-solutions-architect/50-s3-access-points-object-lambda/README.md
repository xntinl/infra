# 50. S3 Access Points and Object Lambda

<!--
difficulty: intermediate
concepts: [s3-access-points, object-lambda-access-point, vpc-access-point, pii-redaction, bucket-policy-delegation, access-point-policy, lambda-transformation]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [47-s3-storage-classes-lifecycle-policies, 52-s3-bucket-policies-acls]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 Access Points have no additional charge. Object Lambda incurs Lambda invocation costs ($0.0000002/request) and S3 GET request costs ($0.0004/1,000 requests). For this exercise with minimal test data, cost is negligible (~$0.01/hr). Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Understanding of S3 bucket policies | Exercise 52 or equivalent |
| Basic Lambda knowledge (Go) | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** S3 Access Points that simplify bucket policies by delegating per-application access control.
2. **Analyze** how access point policies interact with bucket policies (bucket policy is the ceiling, access point policy cannot exceed it).
3. **Apply** VPC-restricted access points that limit S3 access to specific VPCs.
4. **Design** an Object Lambda Access Point that transforms objects on retrieval without modifying the stored data.
5. **Evaluate** when to use Access Points vs bucket policies for multi-tenant access control.

---

## Why This Matters

As S3 buckets grow to serve multiple applications, teams, and use cases, bucket policies become unwieldy. A single bucket policy document has a 20 KB limit, and managing permissions for 15 different applications in one JSON document is error-prone. S3 Access Points solve this by creating named, per-application entry points to a bucket, each with its own policy and optional VPC restriction. Instead of one complex bucket policy, each application gets its own access point with a focused policy.

Object Lambda Access Points take this further by transforming data on the fly during GET requests. A single stored dataset can be served differently to different consumers -- redacting PII for analytics teams, converting formats for legacy systems, or watermarking images for external partners -- without maintaining multiple copies of the data. The SAA-C03 exam tests both features, particularly the policy hierarchy (bucket policy constrains access point policies) and the VPC restriction use case (ensuring S3 data never leaves your VPC).

---

## Building Blocks

Create the following files in your exercise directory:

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
  default     = "saa-ex50"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_region" "current" {}

resource "random_id" "suffix" {
  byte_length = 4
}

# ---------- S3 Bucket ----------

resource "aws_s3_bucket" "this" {
  bucket        = "${var.project_name}-data-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_bucket_public_access_block" "this" {
  bucket = aws_s3_bucket.this.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ---------- Bucket Policy Delegating to Access Points ----------

resource "aws_s3_bucket_policy" "delegate_to_access_points" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "DelegateToAccessPoints"
        Effect    = "Allow"
        Principal = "*"
        Action    = "s3:*"
        Resource = [
          aws_s3_bucket.this.arn,
          "${aws_s3_bucket.this.arn}/*"
        ]
        Condition = {
          StringEquals = {
            "s3:DataAccessPointAccount" = data.aws_caller_identity.current.account_id
          }
        }
      }
    ]
  })
}

# ---------- Upload Test Data ----------

resource "aws_s3_object" "customer_data" {
  bucket       = aws_s3_bucket.this.id
  key          = "customers/record-001.json"
  content_type = "application/json"
  content = jsonencode({
    id    = "001"
    name  = "Jane Smith"
    email = "jane.smith@example.com"
    ssn   = "123-45-6789"
    phone = "+1-555-0100"
    notes = "Premium customer since 2020"
  })
}

resource "aws_s3_object" "analytics_data" {
  bucket       = aws_s3_bucket.this.id
  key          = "analytics/metrics-2026-03.json"
  content_type = "application/json"
  content = jsonencode({
    month          = "2026-03"
    total_orders   = 15420
    revenue        = 892340.50
    avg_order_size = 57.87
  })
}


# ============================================================
# TODO 1: Access Point for Analytics Team
# ============================================================
# Create an S3 Access Point that grants the analytics team
# read-only access to the "analytics/" prefix only.
#
# Requirements:
#   - Resource: aws_s3_access_point
#   - name = "${var.project_name}-analytics"
#   - bucket = bucket ID
#   - Access point policy allowing:
#     - s3:GetObject on "analytics/*"
#     - s3:ListBucket with condition s3:prefix = "analytics/"
#     - Principal = current account root
#
# Note: Access point ARN format:
#   arn:aws:s3:region:account:accesspoint/name
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_access_point
# ============================================================


# ============================================================
# TODO 2: VPC-Restricted Access Point
# ============================================================
# Create an S3 Access Point that can ONLY be accessed from
# within a specific VPC. This prevents data exfiltration
# via the public internet.
#
# Requirements:
#   a) Create a VPC (or use default VPC)
#   b) Resource: aws_s3_access_point with:
#      - name = "${var.project_name}-vpc-only"
#      - bucket = bucket ID
#      - vpc_configuration block with vpc_id
#   c) Access point policy allowing s3:GetObject and
#      s3:ListBucket from the VPC
#
# Important: VPC access points require a VPC endpoint for S3
# (Gateway type) in the VPC. Without it, requests cannot
# reach the access point.
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-points-vpc.html
# ============================================================
```

### `lambda.tf`

```hcl
# ============================================================
# TODO 3: Object Lambda Access Point for PII Redaction
# ============================================================
# Create an Object Lambda Access Point that automatically
# redacts PII (SSN, phone number) from customer records
# when retrieved through this access point.
#
# Requirements:
#   a) Create a "supporting" access point (regular access point
#      that Object Lambda uses to fetch the original object):
#      - aws_s3_access_point with name "${var.project_name}-olap-support"
#
#   b) Create a Lambda function that:
#      - Receives the S3 Object Lambda event
#      - Fetches the original object from the presigned URL
#      - Redacts SSN and phone fields
#      - Calls WriteGetObjectResponse with the redacted data
#
#   c) Create the Object Lambda Access Point:
#      - Resource: aws_s3control_object_lambda_access_point
#      - name = "${var.project_name}-pii-redacted"
#      - configuration block with:
#        - supporting_access_point = supporting AP ARN
#        - transformation_configuration block with:
#          - actions = ["GetObject"]
#          - content_transformation.aws_lambda with
#            function_arn = Lambda function ARN
#
# Docs: https://docs.aws.amazon.com/AmazonS3/latest/userguide/transforming-objects.html
# ============================================================
```

### `outputs.tf`

```hcl
output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "bucket_arn" {
  value = aws_s3_bucket.this.arn
}
```

---

## Spot the Bug

The following access point policy has a permission escalation flaw. Identify the problem before expanding the answer.

```hcl
# Bucket policy: restrict to read-only via access points
resource "aws_s3_bucket_policy" "restrictive" {
  bucket = aws_s3_bucket.this.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "ReadOnlyViaAccessPoints"
        Effect    = "Allow"
        Principal = "*"
        Action    = ["s3:GetObject", "s3:ListBucket"]
        Resource = [
          "arn:aws:s3:::my-bucket",
          "arn:aws:s3:::my-bucket/*"
        ]
        Condition = {
          StringEquals = {
            "s3:DataAccessPointAccount" = "123456789012"
          }
        }
      }
    ]
  })
}

# Access point policy: grants write access
resource "aws_s3_access_point" "app_write" {
  bucket = aws_s3_bucket.this.id
  name   = "app-write-access"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::123456789012:role/app-role" }
        Action    = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject"]
        Resource  = "arn:aws:s3:us-east-1:123456789012:accesspoint/app-write-access/object/*"
      }
    ]
  })
}
```

<details>
<summary>Explain the bug</summary>

**The access point policy grants `s3:PutObject` and `s3:DeleteObject`, but the bucket policy only allows `s3:GetObject` and `s3:ListBucket`.** This is not a security vulnerability -- it is a silent failure. The access point policy is more permissive than the bucket policy, but the bucket policy acts as the ceiling.

When the `app-role` tries to write or delete objects through this access point, the requests will be **denied** because the bucket policy does not allow those actions. The access point policy cannot grant permissions that the bucket policy does not also allow. The developer will see `AccessDenied` errors and may spend hours debugging IAM policies, not realizing the bucket policy is the bottleneck.

**Fix:** Either update the bucket policy to allow the actions you need:

```json
"Action": ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket"]
```

Or use a wildcard delegation in the bucket policy and rely entirely on access point policies for fine-grained control:

```json
"Action": "s3:*"
```

The key insight for the exam: **bucket policy is the ceiling, access point policy is the floor.** The effective permission is the intersection of both policies.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Access data through the analytics access point:**
   ```bash
   ACCOUNT=$(aws sts get-caller-identity --query Account --output text)

   # Access via access point ARN
   aws s3api get-object \
     --bucket "arn:aws:s3:us-east-1:${ACCOUNT}:accesspoint/saa-ex50-analytics" \
     --key "analytics/metrics-2026-03.json" \
     /dev/stdout
   ```

3. **Verify analytics access point cannot access customer data:**
   ```bash
   aws s3api get-object \
     --bucket "arn:aws:s3:us-east-1:${ACCOUNT}:accesspoint/saa-ex50-analytics" \
     --key "customers/record-001.json" \
     /dev/stdout 2>&1 || echo "Access denied as expected"
   ```

4. **Access data through Object Lambda Access Point (redacted):**
   ```bash
   aws s3api get-object \
     --bucket "arn:aws:s3:us-east-1:${ACCOUNT}:accesspoint/saa-ex50-pii-redacted" \
     --key "customers/record-001.json" \
     /dev/stdout
   ```
   Expected: SSN and phone fields are redacted (e.g., `"ssn": "***-**-****"`).

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Access Points vs Bucket Policies Decision Framework

| Criterion | Bucket Policy | Access Points |
|---|---|---|
| **Policy size limit** | 20 KB per bucket | 20 KB per access point (scalable) |
| **Management** | Single document for all apps | Per-application policies |
| **VPC restriction** | Via condition keys | Native vpc_configuration |
| **Cross-account** | Via Principal + condition | Same + access point policy |
| **Object transformation** | Not supported | Via Object Lambda Access Points |
| **When to use** | Simple access patterns, few consumers | Multi-app, multi-team, VPC isolation |

---

## Solutions

<details>
<summary>TODO 1 -- Analytics Access Point (storage.tf)</summary>

```hcl
resource "aws_s3_access_point" "analytics" {
  bucket = aws_s3_bucket.this.id
  name   = "${var.project_name}-analytics"

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "s3:GetObject"
        Resource  = "arn:aws:s3:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:accesspoint/${var.project_name}-analytics/object/analytics/*"
      },
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = "s3:ListBucket"
        Resource  = "arn:aws:s3:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:accesspoint/${var.project_name}-analytics"
        Condition = {
          StringLike = {
            "s3:prefix" = "analytics/*"
          }
        }
      }
    ]
  })
}
```

</details>

<details>
<summary>TODO 2 -- VPC-Restricted Access Point (storage.tf)</summary>

```hcl
data "aws_vpc" "default" {
  default = true
}

resource "aws_s3_access_point" "vpc_only" {
  bucket = aws_s3_bucket.this.id
  name   = "${var.project_name}-vpc-only"

  vpc_configuration {
    vpc_id = data.aws_vpc.default.id
  }

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
        Action    = ["s3:GetObject", "s3:ListBucket"]
        Resource = [
          "arn:aws:s3:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:accesspoint/${var.project_name}-vpc-only",
          "arn:aws:s3:${data.aws_region.current.name}:${data.aws_caller_identity.current.account_id}:accesspoint/${var.project_name}-vpc-only/object/*"
        ]
      }
    ]
  })
}
```

Note: A VPC endpoint for S3 (Gateway type) must exist in the VPC for this access point to be reachable from within the VPC.

</details>

<details>
<summary>TODO 3 -- Object Lambda Access Point for PII Redaction (lambda.tf)</summary>

```hcl
# Supporting access point (required by Object Lambda)
resource "aws_s3_access_point" "olap_support" {
  bucket = aws_s3_bucket.this.id
  name   = "${var.project_name}-olap-support"
}

# Lambda function for PII redaction
resource "aws_iam_role" "lambda" {
  name = "${var.project_name}-pii-redact-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "lambda.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "lambda_basic" {
  role       = aws_iam_role.lambda.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonS3ObjectLambdaExecutionRolePolicy"
}

# Build the Go binary before terraform apply:
#   cd lambda/redact && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
#   zip redact.zip bootstrap

resource "aws_lambda_function" "redact" {
  function_name    = "${var.project_name}-pii-redact"
  role             = aws_iam_role.lambda.arn
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  filename         = "${path.module}/lambda/redact/redact.zip"
  source_code_hash = filebase64sha256("${path.module}/lambda/redact/redact.zip")
  timeout          = 60
}

# lambda/redact/main.go:
#
# package main
#
# import (
# 	"context"
# 	"encoding/json"
# 	"io"
# 	"net/http"
#
# 	"github.com/aws/aws-lambda-go/lambda"
# 	"github.com/aws/aws-sdk-go-v2/config"
# 	"github.com/aws/aws-sdk-go-v2/service/s3"
# )
#
# type ObjectContext struct {
# 	OutputRoute string `json:"outputRoute"`
# 	OutputToken string `json:"outputToken"`
# 	InputS3Url  string `json:"inputS3Url"`
# }
#
# type Event struct {
# 	GetObjectContext ObjectContext `json:"getObjectContext"`
# }
#
# func handler(ctx context.Context, event Event) error {
# 	resp, _ := http.Get(event.GetObjectContext.InputS3Url)
# 	defer resp.Body.Close()
# 	body, _ := io.ReadAll(resp.Body)
#
# 	var original map[string]interface{}
# 	json.Unmarshal(body, &original)
#
# 	if _, ok := original["ssn"]; ok {
# 		original["ssn"] = "***-**-****"
# 	}
# 	if _, ok := original["phone"]; ok {
# 		original["phone"] = "***-***-****"
# 	}
#
# 	redacted, _ := json.Marshal(original)
# 	cfg, _ := config.LoadDefaultConfig(ctx)
# 	client := s3.NewFromConfig(cfg)
#
# 	contentType := "application/json"
# 	client.WriteGetObjectResponse(ctx, &s3.WriteGetObjectResponseInput{
# 		RequestRoute: &event.GetObjectContext.OutputRoute,
# 		RequestToken: &event.GetObjectContext.OutputToken,
# 		Body:         bytes.NewReader(redacted),
# 		ContentType:  &contentType,
# 	})
# 	return nil
# }
#
# func main() { lambda.Start(handler) }

# Object Lambda Access Point
resource "aws_s3control_object_lambda_access_point" "pii_redacted" {
  name = "${var.project_name}-pii-redacted"

  configuration {
    supporting_access_point = aws_s3_access_point.olap_support.arn

    transformation_configuration {
      actions = ["GetObject"]

      content_transformation {
        aws_lambda {
          function_arn = aws_lambda_function.redact.arn
        }
      }
    }
  }
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify resources are removed:

```bash
terraform state list
```

Expected: no output (empty state).

---

## What's Next

Exercise 51 covers **S3 server-side encryption comparison** -- SSE-S3, SSE-KMS, and SSE-C. You will upload objects with each encryption method, compare the key management responsibilities and audit capabilities, and build a decision table for choosing the right encryption approach based on compliance requirements.

---

## Summary

- **S3 Access Points** create named, per-application entry points to a bucket with individual policies, simplifying multi-tenant access management
- **Bucket policy is the ceiling** -- access point policies cannot grant permissions the bucket policy does not allow
- **VPC-restricted access points** ensure data can only be accessed from within a specific VPC, preventing internet-based exfiltration
- **Object Lambda Access Points** transform data on retrieval without modifying stored objects -- ideal for PII redaction, format conversion, and watermarking
- **Object Lambda requires a supporting access point** (a regular access point) that fetches the original object for transformation
- **Access point ARN format**: `arn:aws:s3:region:account:accesspoint/name` -- use this as the `--bucket` parameter in AWS CLI
- **20 KB policy limit per access point** means you can scale to hundreds of applications without hitting the single bucket policy size limit
- Access points can be **cross-account**: one account owns the bucket, another account's access point provides access with its own policy

## Reference

- [S3 Access Points](https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-points.html)
- [Object Lambda Access Points](https://docs.aws.amazon.com/AmazonS3/latest/userguide/transforming-objects.html)
- [Terraform aws_s3_access_point](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_access_point)
- [Terraform aws_s3control_object_lambda_access_point](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3control_object_lambda_access_point)

## Additional Resources

- [Access Point Policy Examples](https://docs.aws.amazon.com/AmazonS3/latest/userguide/access-points-policies.html) -- common policy patterns for access points
- [VPC Endpoints for S3](https://docs.aws.amazon.com/AmazonS3/latest/userguide/privatelink-interface-endpoints.html) -- required for VPC-restricted access points
- [Object Lambda Tutorial](https://docs.aws.amazon.com/AmazonS3/latest/userguide/olap-use-cases.html) -- AWS walkthrough of Object Lambda use cases
- [S3 Multi-Region Access Points](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiRegionAccessPoints.html) -- access points that route to the closest regional bucket
