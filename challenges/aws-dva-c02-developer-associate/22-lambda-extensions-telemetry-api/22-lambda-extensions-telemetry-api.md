# 22. Lambda Extensions and Telemetry API

<!--
difficulty: intermediate
concepts: [lambda-extensions, external-extension, telemetry-api, extension-lifecycle, lambda-layers, init-invoke-shutdown]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: implement, differentiate
prerequisites: [exercise-01]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a Lambda function with an extension layer and an S3 bucket for telemetry storage. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| Exercise 01 completed | Understanding of Lambda Layers |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** an external Lambda extension as a layer that runs alongside the function handler during the INIT, INVOKE, and SHUTDOWN phases
2. **Differentiate** between internal extensions (in-process, same runtime) and external extensions (separate process, any language) and their lifecycle implications
3. **Configure** the Lambda Telemetry API subscription to receive platform telemetry events (function logs, platform metrics, extension logs)
4. **Describe** the three Lambda lifecycle phases (INIT, INVOKE, SHUTDOWN) and how extensions participate in each phase
5. **Design** a telemetry pipeline that captures Lambda execution data without modifying the function code

## Why This Matters

Lambda extensions let you integrate monitoring, security, and governance tools into the Lambda execution environment without modifying function code. An extension runs as a separate process alongside your function handler, participating in the Lambda lifecycle.

The three lifecycle phases are critical for the exam:

1. **INIT** -- Lambda starts the runtime and all extensions. Extensions register with the Extensions API and optionally subscribe to the Telemetry API. The function's `init()` code runs. This is where cold start overhead comes from.
2. **INVOKE** -- Lambda sends the event to the function handler and notifies extensions of the invocation. The function processes the event and returns a response. Extensions can perform work in parallel (e.g., flushing telemetry buffers).
3. **SHUTDOWN** -- Lambda signals extensions to clean up. Extensions have up to 2 seconds (or 300ms for internal extensions) to flush data, close connections, and exit. After this, the execution environment is frozen or destroyed.

External extensions are compiled as separate binaries and deployed as layers. They must be placed in the `/opt/extensions/` directory and be executable. Lambda discovers them by scanning this directory during INIT. The extension binary name becomes the extension name registered with the Extensions API.

The DVA-C02 exam tests extension concepts in the context of observability: how do you add custom logging or monitoring to Lambda without changing function code? The answer is extensions.

## Building Blocks

Create the function handler in `main.go`:

### `main.go`

```go
// main.go -- Lambda function (does not know about the extension)
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	fmt.Println("Function handler invoked")
	fmt.Printf("Event: %s\n", string(event))

	return map[string]interface{}{
		"function_name": os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"timestamp":     time.Now().Format(time.RFC3339),
		"message":       "Handler completed -- extension captures telemetry independently",
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

Create the external extension in `extension/extensions/telemetry-collector`:

### `extension/main.go`

```go
// extension/main.go -- External extension binary
// This is a separate Go program compiled as 'telemetry-collector'
// and placed in the layer at /opt/extensions/telemetry-collector
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

const extensionName = "telemetry-collector"

var (
	runtimeAPI    string
	extensionID  string
)

func main() {
	runtimeAPI = os.Getenv("AWS_LAMBDA_RUNTIME_API")
	if runtimeAPI == "" {
		fmt.Println("AWS_LAMBDA_RUNTIME_API not set, exiting")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle SIGTERM for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM)
	go func() {
		<-sigs
		fmt.Println("[extension] Received SIGTERM, shutting down")
		cancel()
	}()

	// INIT phase: Register the extension
	extensionID = registerExtension()
	fmt.Printf("[extension] Registered with ID: %s\n", extensionID)

	// Event loop: wait for INVOKE and SHUTDOWN events
	for {
		event := nextEvent(ctx)
		if event == nil {
			break
		}

		switch event["eventType"].(string) {
		case "INVOKE":
			fmt.Printf("[extension] INVOKE event at %s\n", time.Now().Format(time.RFC3339))
			// In production, you would flush telemetry buffers here
		case "SHUTDOWN":
			fmt.Printf("[extension] SHUTDOWN event, reason: %v\n", event["shutdownReason"])
			// Flush any remaining telemetry data
			fmt.Println("[extension] Flushing final telemetry data...")
			return
		}
	}
}

func registerExtension() string {
	url := fmt.Sprintf("http://%s/2020-01-01/extension/register", runtimeAPI)
	body, _ := json.Marshal(map[string][]string{
		"events": {"INVOKE", "SHUTDOWN"},
	})

	req, _ := http.NewRequest("POST", url, bytes.NewReader(body))
	req.Header.Set("Lambda-Extension-Name", extensionName)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		fmt.Printf("[extension] Registration failed: %v\n", err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	return resp.Header.Get("Lambda-Extension-Identifier")
}

func nextEvent(ctx context.Context) map[string]interface{} {
	url := fmt.Sprintf("http://%s/2020-01-01/extension/event/next", runtimeAPI)
	req, _ := http.NewRequestWithContext(ctx, "GET", url, nil)
	req.Header.Set("Lambda-Extension-Identifier", extensionID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	var event map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&event)
	return event
}
```

Create the following Terraform files. Your job is to fill in each `# TODO` block.

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

provider "aws" { region = var.region }
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region for all resources"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name used for resource naming"
  type        = string
  default     = "extensions-demo"
}
```

### `build.tf`

```hcl
# -------------------------------------------------------
# Build the function binary
# -------------------------------------------------------
resource "null_resource" "go_build_function" {
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
  depends_on  = [null_resource.go_build_function]
}

# -------------------------------------------------------
# Build the extension binary
# -------------------------------------------------------
# The extension binary must be at extensions/telemetry-collector
# inside the layer ZIP. Lambda discovers extensions by scanning
# /opt/extensions/ at startup.
resource "null_resource" "go_build_extension" {
  triggers = { source_hash = filebase64sha256("${path.module}/extension/main.go") }
  provisioner "local-exec" {
    command     = "cd extension && GOOS=linux GOARCH=arm64 go build -o extensions/telemetry-collector main.go"
    working_dir = path.module
  }
}

data "archive_file" "extension_zip" {
  type        = "zip"
  source_dir  = "${path.module}/extension"
  excludes    = ["main.go", "go.mod", "go.sum"]
  output_path = "${path.module}/build/extension.zip"
  depends_on  = [null_resource.go_build_extension]
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
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `storage.tf`

```hcl
# -------------------------------------------------------
# S3 bucket for telemetry data
# -------------------------------------------------------
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "telemetry" {
  bucket        = "${var.project_name}-telemetry-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

data "aws_iam_policy_document" "s3_access" {
  statement {
    actions   = ["s3:PutObject"]
    resources = ["${aws_s3_bucket.telemetry.arn}/*"]
  }
}

resource "aws_iam_role_policy" "s3_access" {
  name   = "s3-telemetry-access"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.s3_access.json
}
```

### `lambda.tf`

```hcl
# =======================================================
# TODO 1 -- Lambda Layer for the Extension
# =======================================================
# Requirements:
#   - Create an aws_lambda_layer_version for the extension
#   - Set filename to the extension ZIP
#   - Set compatible_runtimes to ["provided.al2023"]
#   - The layer ZIP must contain extensions/telemetry-collector
#     at the root -- Lambda extracts to /opt, making it
#     /opt/extensions/telemetry-collector
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_layer_version


# =======================================================
# TODO 2 -- Lambda Function with Extension Layer
# =======================================================
# Requirements:
#   - Create the aws_lambda_function
#   - Attach the extension layer using the layers attribute
#   - Set TELEMETRY_BUCKET environment variable to the S3
#     bucket name
#   - Use handler="bootstrap", runtime="provided.al2023"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function


# =======================================================
# TODO 3 -- CloudWatch Log Group
# =======================================================
# Requirements:
#   - Create an aws_cloudwatch_log_group with
#     name = "/aws/lambda/extensions-demo"
#   - Set retention_in_days = 1
#   - Add depends_on to the Lambda function
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudwatch_log_group
```

### `outputs.tf`

```hcl
output "function_name"    { value = aws_lambda_function.this.function_name }
output "layer_arn"        { value = aws_lambda_layer_version.extension.arn }
output "telemetry_bucket" { value = aws_s3_bucket.telemetry.bucket }
```

## Spot the Bug

A developer creates an extension layer but the extension is never discovered by Lambda. The layer contains:

```
layer.zip
└── telemetry-collector     # extension binary at root of ZIP
```

<details>
<summary>Explain the bug</summary>

Lambda discovers extensions by scanning the `/opt/extensions/` directory. Since layers extract to `/opt`, the extension binary must be inside an `extensions/` directory in the ZIP:

**Wrong structure:**
```
layer.zip
└── telemetry-collector     # Extracts to /opt/telemetry-collector (not discovered)
```

**Correct structure:**
```
layer.zip
└── extensions/
    └── telemetry-collector  # Extracts to /opt/extensions/telemetry-collector (discovered)
```

The binary must also be executable (`chmod +x`). Without the correct directory structure, Lambda starts normally but the extension never runs -- there is no error message, which makes this difficult to debug.

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
cd extension && GOOS=linux GOARCH=arm64 go build -o extensions/telemetry-collector main.go && cd ..
terraform init && terraform apply -auto-approve
```

### Step 2 -- Invoke the function

```bash
aws lambda invoke --function-name extensions-demo --payload '{"test": true}' /dev/stdout 2>/dev/null | jq .
```

Expected: response with `"message": "Handler completed -- extension captures telemetry independently"`

### Step 3 -- Check CloudWatch Logs for extension output

```bash
sleep 5
aws logs filter-log-events \
  --log-group-name /aws/lambda/extensions-demo \
  --filter-pattern "[extension]" \
  --query "events[*].message" --output text
```

Expected: log lines showing `[extension] Registered`, `[extension] INVOKE event`, confirming the extension ran alongside the function.

### Step 4 -- Verify the layer is attached

```bash
aws lambda get-function-configuration --function-name extensions-demo \
  --query "Layers[*].Arn" --output json
```

Expected: JSON array containing the extension layer ARN.

## Solutions

<details>
<summary>TODO 1 -- Lambda Layer for the Extension (lambda.tf)</summary>

```hcl
resource "aws_lambda_layer_version" "extension" {
  layer_name          = "telemetry-collector-extension"
  filename            = data.archive_file.extension_zip.output_path
  source_code_hash    = data.archive_file.extension_zip.output_base64sha256
  compatible_runtimes = ["provided.al2023"]
  description         = "External extension for telemetry collection"
}
```

</details>

<details>
<summary>TODO 2 -- Lambda Function with Extension Layer (lambda.tf)</summary>

```hcl
resource "aws_lambda_function" "this" {
  function_name    = "extensions-demo"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30

  layers = [aws_lambda_layer_version.extension.arn]

  environment {
    variables = {
      TELEMETRY_BUCKET = aws_s3_bucket.telemetry.bucket
    }
  }

  depends_on = [
    aws_iam_role_policy_attachment.basic,
    aws_cloudwatch_log_group.this,
  ]
}
```

</details>

<details>
<summary>TODO 3 -- CloudWatch Log Group (lambda.tf)</summary>

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/extensions-demo"
  retention_in_days = 1
}
```

</details>

## Cleanup

Destroy all resources:

```bash
# Empty the S3 bucket first
aws s3 rm s3://$(terraform output -raw telemetry_bucket) --recursive 2>/dev/null
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

In **Exercise 23 -- Lambda Event Source Mappings Deep Dive**, you will compare event source mapping behavior across SQS, Kinesis, and DynamoDB Streams, including batch settings, parallelization factor, and bisect-on-error configurations.

## Summary

You built and deployed an external Lambda extension as a layer:

- **External extensions** run as separate processes alongside the function handler, discovered from `/opt/extensions/`
- **Internal extensions** run in-process (same runtime), using runtime hooks -- not applicable to Go custom runtimes
- The Lambda lifecycle has three phases: **INIT** (register extensions, init code), **INVOKE** (handle event), **SHUTDOWN** (cleanup, max 2 seconds)
- Extensions register with the **Extensions API** (`/extension/register`) and poll for events (`/extension/event/next`)
- The **Telemetry API** allows extensions to subscribe to function logs, platform metrics, and extension logs
- Layer structure is critical: extension binaries must be in `extensions/` directory and be executable
- Extensions add latency to INIT (cold start) but run in parallel during INVOKE
- Use extensions for **monitoring, security, and governance** without modifying function code

## Reference

| Resource | Purpose |
|----------|---------|
| `aws_lambda_layer_version` | Packages the extension binary |
| `aws_lambda_function.layers` | Attaches extension layer to function |
| Extensions API | Registration and event polling |
| Telemetry API | Subscribe to platform telemetry |

## Additional Resources

- [Lambda Extensions API](https://docs.aws.amazon.com/lambda/latest/dg/runtimes-extensions-api.html)
- [Lambda Telemetry API](https://docs.aws.amazon.com/lambda/latest/dg/telemetry-api.html)
- [Building Lambda Extensions](https://aws.amazon.com/blogs/compute/building-extensions-for-aws-lambda-in-preview/)
- [Lambda Execution Environment Lifecycle](https://docs.aws.amazon.com/lambda/latest/dg/lambda-runtime-environment.html)
- [Lambda Extension Partners](https://docs.aws.amazon.com/lambda/latest/dg/extensions-api-partners.html)
