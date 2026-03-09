# 100. S3 Pre-Signed URLs for Temporary Access

<!--
difficulty: intermediate
concepts: [s3-presigned-urls, temporary-access, presigned-put, presigned-get, url-expiration, http-methods, cors]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates an S3 bucket, a Lambda function, and API Gateway. S3 and Lambda costs are negligible for testing. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.5 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a Lambda function that generates S3 pre-signed URLs for both upload (PUT) and download (GET) operations
2. **Configure** URL expiration times appropriate for the use case (short for uploads, longer for downloads)
3. **Differentiate** between pre-signed PUT URLs (upload) and pre-signed GET URLs (download) and their HTTP method requirements
4. **Diagnose** the common bug of generating a pre-signed URL with the wrong HTTP method (GET for upload, PUT for download)
5. **Apply** S3 bucket policies and CORS configuration to support browser-based uploads using pre-signed URLs

## Why S3 Pre-Signed URLs

Client applications often need to upload or download files from S3, but you do not want to distribute AWS credentials to browsers, mobile apps, or third-party services. Pre-signed URLs solve this by generating a time-limited URL that grants temporary access to a specific S3 operation (GET, PUT) on a specific object.

The workflow: your backend Lambda generates a pre-signed URL using its IAM credentials, returns the URL to the client, and the client uses the URL directly with S3 -- no AWS SDK needed on the client side. The URL is valid only for the specified duration (e.g., 15 minutes) and only for the specific HTTP method and S3 key it was generated for.

The DVA-C02 exam tests pre-signed URLs in three areas. First, the distinction between **pre-signed GET** (download) and **pre-signed PUT** (upload) -- each is generated with a specific HTTP method and the client MUST use the correct method. A pre-signed PUT URL used with GET returns `403 Forbidden`. Second, **expiration**: the URL includes an `X-Amz-Expires` parameter and becomes invalid after expiration. Third, **permissions**: the pre-signed URL inherits the permissions of the IAM entity that generated it. If the Lambda's role does not have `s3:PutObject`, the generated upload URL fails with `AccessDenied` even though the URL itself is syntactically valid.

## Building Blocks

Create the following project files. Your job is to fill in the `// TODO` sections in the Go code.

### `lambda/main.go`

```go
// main.go
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/google/uuid"
)

var (
	s3Client       *s3.Client
	presignClient  *s3.PresignClient
	bucketName     string
)

func init() {
	bucketName = os.Getenv("BUCKET_NAME")
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	s3Client = s3.NewFromConfig(cfg)
	presignClient = s3.NewPresignClient(s3Client)
}

type PresignRequest struct {
	Action      string `json:"action"`
	Key         string `json:"key,omitempty"`
	ContentType string `json:"content_type,omitempty"`
}

type PresignResponse struct {
	URL        string `json:"url"`
	Key        string `json:"key"`
	Method     string `json:"method"`
	ExpiresIn  string `json:"expires_in"`
	BucketName string `json:"bucket"`
}

func handler(ctx context.Context, event events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	var req PresignRequest
	json.Unmarshal([]byte(event.Body), &req)

	var result PresignResponse
	var err error

	switch req.Action {
	case "upload":
		result, err = generateUploadURL(ctx, req)
	case "download":
		result, err = generateDownloadURL(ctx, req)
	default:
		return respond(400, map[string]string{"error": "action must be 'upload' or 'download'"})
	}

	if err != nil {
		return respond(500, map[string]string{"error": err.Error()})
	}

	return respond(200, result)
}

func generateUploadURL(ctx context.Context, req PresignRequest) (PresignResponse, error) {
	key := req.Key
	if key == "" {
		key = fmt.Sprintf("uploads/%s/%s", time.Now().Format("2006-01-02"), uuid.New().String())
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	// TODO 1 -- Generate a pre-signed PUT URL for uploading
	// Use presignClient.PresignPutObject to create a URL that allows
	// the client to upload a file directly to S3.
	//
	// Steps:
	//   1. Call presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
	//        Bucket:      aws.String(bucketName),
	//        Key:         aws.String(key),
	//        ContentType: aws.String(contentType),
	//      }, s3.WithPresignExpires(15*time.Minute))
	//   2. The returned PresignedHTTPRequest contains .URL and .Method
	//   3. The client must use HTTP PUT (not POST) with this URL
	//
	// Return a PresignResponse with the URL, key, method, expiration, and bucket

	return PresignResponse{
		Key:        key,
		Method:     "PUT",
		ExpiresIn:  "15m",
		BucketName: bucketName,
		URL:        "TODO: generate presigned PUT URL",
	}, nil
}

func generateDownloadURL(ctx context.Context, req PresignRequest) (PresignResponse, error) {
	if req.Key == "" {
		return PresignResponse{}, fmt.Errorf("key is required for download")
	}

	// TODO 2 -- Generate a pre-signed GET URL for downloading
	// Use presignClient.PresignGetObject to create a URL that allows
	// the client to download a file directly from S3.
	//
	// Steps:
	//   1. Call presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
	//        Bucket: aws.String(bucketName),
	//        Key:    aws.String(req.Key),
	//      }, s3.WithPresignExpires(60*time.Minute))
	//   2. The client uses HTTP GET with this URL
	//   3. Set a longer expiration (60 minutes) for download URLs
	//
	// Return a PresignResponse with the URL, key, method, expiration, and bucket

	return PresignResponse{
		Key:        req.Key,
		Method:     "GET",
		ExpiresIn:  "60m",
		BucketName: bucketName,
		URL:        "TODO: generate presigned GET URL",
	}, nil
}

func respond(status int, body interface{}) (events.APIGatewayProxyResponse, error) {
	data, _ := json.Marshal(body)
	return events.APIGatewayProxyResponse{
		StatusCode: status,
		Headers: map[string]string{
			"Content-Type":                "application/json",
			"Access-Control-Allow-Origin": "*",
		},
		Body: string(data),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `lambda/go.mod`

```
module presigned-demo

go 1.21

require (
	github.com/aws/aws-lambda-go v1.47.0
	github.com/aws/aws-sdk-go-v2 v1.30.0
	github.com/aws/aws-sdk-go-v2/config v1.27.0
	github.com/aws/aws-sdk-go-v2/service/s3 v1.58.0
	github.com/google/uuid v1.6.0
)
```

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    archive = {
      source  = "hashicorp/archive"
      version = "~> 2.0"
    }
    null = {
      source  = "hashicorp/null"
      version = "~> 3.0"
    }
  }
}

provider "aws" { region = "us-east-1" }
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}
```

### `storage.tf`

```hcl
# -------------------------------------------------------
# S3 Bucket
# -------------------------------------------------------
resource "aws_s3_bucket" "this" {
  bucket        = "presigned-demo-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

# CORS configuration for browser-based uploads
resource "aws_s3_bucket_cors_configuration" "this" {
  bucket = aws_s3_bucket.this.id

  cors_rule {
    allowed_headers = ["*"]
    allowed_methods = ["GET", "PUT"]
    allowed_origins = ["*"]
    max_age_seconds = 3600
  }
}
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build and package
# -------------------------------------------------------
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
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
# -------------------------------------------------------
# IAM
# -------------------------------------------------------
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "presigned-demo-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}

# The Lambda role MUST have s3:PutObject and s3:GetObject permissions.
# Pre-signed URLs inherit the IAM permissions of the entity that created them.
# If the role lacks s3:PutObject, upload URLs return AccessDenied.
data "aws_iam_policy_document" "s3_access" {
  statement {
    actions = [
      "s3:PutObject",
      "s3:GetObject",
      "s3:HeadObject",
    ]
    resources = ["${aws_s3_bucket.this.arn}/*"]
  }
}

resource "aws_iam_role_policy" "s3_access" {
  name   = "s3-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.s3_access.json
}
```

### `lambda.tf`

```hcl
# -------------------------------------------------------
# Lambda function
# -------------------------------------------------------
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/presigned-demo"
  retention_in_days = 1
}

resource "aws_lambda_function" "this" {
  function_name    = "presigned-demo"
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
      BUCKET_NAME = aws_s3_bucket.this.id
    }
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}
```

### `api.tf`

```hcl
# -------------------------------------------------------
# API Gateway
# -------------------------------------------------------
resource "aws_api_gateway_rest_api" "this" {
  name = "presigned-demo-api"
}

resource "aws_api_gateway_resource" "presign" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "presign"
}

resource "aws_api_gateway_method" "post" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.presign.id
  http_method   = "POST"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "lambda" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.presign.id
  http_method             = aws_api_gateway_method.post.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_lambda_permission" "apigw" {
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  depends_on  = [aws_api_gateway_integration.lambda]
  lifecycle { create_before_destroy = true }
}

resource "aws_api_gateway_stage" "prod" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  deployment_id = aws_api_gateway_deployment.this.id
  stage_name    = "prod"
}
```

### `outputs.tf`

```hcl
output "api_url" {
  value = "${aws_api_gateway_stage.prod.invoke_url}/presign"
}

output "bucket_name" {
  value = aws_s3_bucket.this.id
}

output "function_name" {
  value = aws_lambda_function.this.function_name
}
```

## Spot the Bug

A developer creates a Lambda function that generates pre-signed URLs for file uploads. The client receives the URL and tries to upload a file, but gets `403 Forbidden` -- `SignatureDoesNotMatch`.

```go
func generateUploadURL(ctx context.Context, key string) (string, error) {
    // BUG: Using PresignGetObject instead of PresignPutObject
    result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
        Bucket: aws.String(bucketName),
        Key:    aws.String(key),
    }, s3.WithPresignExpires(15*time.Minute))
    if err != nil {
        return "", err
    }
    return result.URL, nil
}
```

The client then tries:

```bash
curl -X PUT "https://presigned-url..." \
  -H "Content-Type: application/json" \
  --data-binary @file.json
```

<details>
<summary>Explain the bug</summary>

The function calls `PresignGetObject` (which signs for HTTP GET) but the client sends an HTTP PUT request. The pre-signed URL signature includes the HTTP method as part of the signing calculation. When the client sends PUT but the URL was signed for GET, the signature does not match and S3 returns `403 SignatureDoesNotMatch`.

This is one of the most common pre-signed URL bugs. The URL "looks correct" -- it has the right bucket, key, and expiration -- but the embedded signature is mathematically bound to the HTTP method used during signing.

**Fix -- use `PresignPutObject` for upload URLs:**

```go
func generateUploadURL(ctx context.Context, key string) (string, error) {
    result, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
        Bucket:      aws.String(bucketName),
        Key:         aws.String(key),
        ContentType: aws.String("application/json"),
    }, s3.WithPresignExpires(15*time.Minute))
    if err != nil {
        return "", err
    }
    return result.URL, nil  // Client MUST use HTTP PUT with this URL
}
```

The rule is simple:
- **Upload (client sends PUT)**: Generate with `PresignPutObject`
- **Download (client sends GET)**: Generate with `PresignGetObject`

Additionally, if `ContentType` is included in the presigning, the client MUST send the same Content-Type header or the signature will not match.

</details>

## Verify What You Learned

### Step 1 -- Apply

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Generate an upload URL

```bash
API_URL=$(terraform output -raw api_url)

UPLOAD_RESPONSE=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d '{"action": "upload", "content_type": "application/json"}')

echo "$UPLOAD_RESPONSE" | jq .

UPLOAD_URL=$(echo "$UPLOAD_RESPONSE" | jq -r '.url')
UPLOAD_KEY=$(echo "$UPLOAD_RESPONSE" | jq -r '.key')
```

### Step 3 -- Upload a file using the pre-signed URL

```bash
echo '{"message": "Hello from pre-signed upload!"}' > /tmp/upload-test.json

curl -X PUT "$UPLOAD_URL" \
  -H "Content-Type: application/json" \
  --data-binary @/tmp/upload-test.json
```

Expected: HTTP 200 (no response body for PUT).

### Step 4 -- Generate a download URL and retrieve the file

```bash
DOWNLOAD_RESPONSE=$(curl -s -X POST "$API_URL" \
  -H "Content-Type: application/json" \
  -d "{\"action\": \"download\", \"key\": \"$UPLOAD_KEY\"}")

DOWNLOAD_URL=$(echo "$DOWNLOAD_RESPONSE" | jq -r '.url')

curl -s "$DOWNLOAD_URL" | jq .
```

Expected: the JSON content uploaded in Step 3.

### Step 5 -- Verify the object exists in S3

```bash
aws s3 ls "s3://$(terraform output -raw bucket_name)/uploads/" --recursive
```

Expected: the uploaded file listed.

### Step 6 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- Generate Pre-Signed PUT URL (lambda/main.go)</summary>

Replace the placeholder in `generateUploadURL`:

```go
func generateUploadURL(ctx context.Context, req PresignRequest) (PresignResponse, error) {
	key := req.Key
	if key == "" {
		key = fmt.Sprintf("uploads/%s/%s", time.Now().Format("2006-01-02"), uuid.New().String())
	}

	contentType := req.ContentType
	if contentType == "" {
		contentType = "application/octet-stream"
	}

	result, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucketName),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(15*time.Minute))
	if err != nil {
		return PresignResponse{}, fmt.Errorf("failed to generate upload URL: %w", err)
	}

	return PresignResponse{
		URL:        result.URL,
		Key:        key,
		Method:     result.Method,
		ExpiresIn:  "15m",
		BucketName: bucketName,
	}, nil
}
```

</details>

<details>
<summary>TODO 2 -- Generate Pre-Signed GET URL (lambda/main.go)</summary>

Replace the placeholder in `generateDownloadURL`:

```go
func generateDownloadURL(ctx context.Context, req PresignRequest) (PresignResponse, error) {
	if req.Key == "" {
		return PresignResponse{}, fmt.Errorf("key is required for download")
	}

	result, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucketName),
		Key:    aws.String(req.Key),
	}, s3.WithPresignExpires(60*time.Minute))
	if err != nil {
		return PresignResponse{}, fmt.Errorf("failed to generate download URL: %w", err)
	}

	return PresignResponse{
		URL:        result.URL,
		Key:        req.Key,
		Method:     result.Method,
		ExpiresIn:  "60m",
		BucketName: bucketName,
	}, nil
}
```

</details>

## Cleanup

Destroy all resources to stop incurring charges:

```bash
# Empty the bucket first
aws s3 rm "s3://$(terraform output -raw bucket_name)" --recursive

terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Congratulations -- you have completed exercise 100! You now have hands-on experience with pre-signed URLs for secure, temporary S3 access. Review the exercises you found most challenging and revisit the exam-relevant concepts before your DVA-C02 exam.

## Summary

- **Pre-signed GET URLs** allow temporary download access; **pre-signed PUT URLs** allow temporary upload access
- The HTTP method is embedded in the URL signature -- using the **wrong method** (GET URL for PUT, PUT URL for GET) returns `403 SignatureDoesNotMatch`
- Pre-signed URLs inherit the **IAM permissions** of the entity that generated them -- if the Lambda role lacks `s3:PutObject`, upload URLs fail with `AccessDenied`
- **Expiration** is set during generation (`s3.WithPresignExpires`) and cannot be changed after creation
- For browser-based uploads, the S3 bucket needs **CORS configuration** allowing the PUT method from the client's origin
- `ContentType` included in presigning must match the client's `Content-Type` header exactly
- Pre-signed URLs do not require the client to have AWS credentials or the AWS SDK

## Reference

- [S3 Pre-Signed URLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html)
- [Uploading Objects Using Pre-Signed URLs](https://docs.aws.amazon.com/AmazonS3/latest/userguide/PresignedUrlUploadObject.html)
- [AWS SDK for Go v2 Presigning](https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/s3#PresignClient)
- [Terraform aws_s3_bucket_cors_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_cors_configuration)

## Additional Resources

- [Pre-Signed URL Security](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html#PresignedUrlUploadObject-LimitCapabilities) -- limiting pre-signed URL capabilities with bucket policies
- [Pre-Signed POST](https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-UsingHTTPPOST.html) -- browser form-based uploads with policy conditions
- [CORS Configuration for S3](https://docs.aws.amazon.com/AmazonS3/latest/userguide/cors.html) -- enabling cross-origin browser uploads
- [Pre-Signed URL Expiration Limits](https://docs.aws.amazon.com/AmazonS3/latest/userguide/using-presigned-url.html#presigned-url-permissions) -- maximum expiration depends on credential type (IAM user vs role)
