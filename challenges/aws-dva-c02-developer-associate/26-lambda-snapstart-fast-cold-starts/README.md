# 26. Lambda SnapStart for Fast Cold Starts

<!--
difficulty: advanced
concepts: [lambda-snapstart, snapshot-lifecycle, cold-start-optimization, init-duration, crac, uniqueness-requirements, go-snapstart]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [exercise-20]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions with and without SnapStart enabled. Lambda pricing is per-invocation and negligible for testing (~$0.01/hr or less). Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- Exercise 20 completed (understanding of cold starts and provisioned concurrency)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when SnapStart provides meaningful cold start reduction compared to provisioned concurrency and standard cold starts
- **Design** a Lambda function that is compatible with SnapStart's snapshot/restore lifecycle, avoiding uniqueness pitfalls
- **Implement** SnapStart configuration in Terraform and measure the init duration difference before and after enabling it
- **Analyze** the snapshot lifecycle: INIT (first publish) creates a snapshot of the initialized execution environment; subsequent cold starts restore from this snapshot instead of re-running INIT
- **Differentiate** between SnapStart (free, reduces init time, snapshot-based) and provisioned concurrency (paid, eliminates cold starts, pre-warmed)

## Why This Matters

Lambda SnapStart was initially launched for Java (Corretto) runtimes to address Java's notoriously slow cold starts (often 5-10 seconds for Spring Boot applications). In 2024, AWS extended SnapStart to Python and .NET runtimes, and support for Go with `provided.al2023` is available in preview. SnapStart takes a snapshot of the initialized execution environment after the INIT phase completes and caches it. When a cold start occurs, Lambda restores from the snapshot instead of re-executing INIT code, reducing cold start latency by up to 90%.

The exam tests several SnapStart concepts:

1. **Snapshot uniqueness**: The snapshot is taken once and reused across all cold starts. If your INIT code generates a unique ID, creates a random encryption key, or opens a database connection, all cold-started environments share the same values. You must regenerate unique values in the handler, not in `init()`.

2. **SnapStart vs provisioned concurrency**: SnapStart is free and reduces cold start latency but does not eliminate it entirely (restore still takes ~200ms). Provisioned concurrency eliminates cold starts completely but costs money even when idle. The exam may present a scenario where both are viable and ask which is more cost-effective.

3. **Supported runtimes**: SnapStart works with Java 11+, Python 3.12+, .NET 8+, and Go (provided.al2023 with specific configuration). It does not work with container images or functions with provisioned concurrency already enabled on the same version.

## The Challenge

Deploy two versions of the same Lambda function: one without SnapStart and one with SnapStart enabled. Measure the cold start (init duration) difference. Then identify and fix a uniqueness bug in the function code.

### Requirements

| Requirement | Description |
|---|---|
| Baseline Function | Go Lambda without SnapStart, measuring init duration |
| SnapStart Function | Same Go Lambda with SnapStart enabled via `snap_start` configuration |
| Measurement | Compare INIT duration from CloudWatch Logs for both functions |
| Uniqueness Test | Demonstrate the snapshot uniqueness problem with random values generated in init() |
| Fix | Move unique value generation from init() to the handler |

### Snapshot Lifecycle

```
  First cold start (after publish):
  +-------+    +--------+    +----------+
  | INIT  |--->| CREATE |--->| SNAPSHOT  |
  | phase |    |SNAPSHOT|    | CACHED    |
  +-------+    +--------+    +----------+
  (runs init()   (freezes       (stored
   code once)    memory state)   in cache)

  Subsequent cold starts (from snapshot):
  +----------+    +---------+    +--------+
  | RESTORE  |--->| INVOKE  |--->| Response|
  | snapshot |    | handler |    |         |
  +----------+    +---------+    +--------+
  (~200ms vs       (normal        (much faster
   full INIT       execution)     overall cold
   of 1-10s)                      start)
```

## Hints

<details>
<summary>Hint 1: Enabling SnapStart in Terraform</summary>

SnapStart is configured on the Lambda function resource using the `snap_start` block. It requires `publish = true` because SnapStart works on published versions, not `$LATEST`:

```hcl
resource "aws_lambda_function" "with_snapstart" {
  function_name    = "snapstart-demo"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  publish          = true   # Required for SnapStart

  snap_start {
    apply_on = "PublishedVersions"
  }
}
```

The `apply_on` field only accepts `"PublishedVersions"`. After applying, Lambda creates a snapshot the first time the published version is invoked (or during publish, depending on the runtime).

To verify SnapStart is active:

```bash
aws lambda get-function-configuration \
  --function-name snapstart-demo \
  --qualifier 1 \
  --query "SnapStart"
```

Expected: `{"ApplyOn": "PublishedVersions", "OptimizationStatus": "On"}`

</details>

<details>
<summary>Hint 2: Go function with init phase measurement</summary>

Create a Go function that measures its own initialization time. The key is to track when `init()` runs versus when the handler runs:

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

var (
	initTime     time.Time
	initDuration time.Duration
	instanceID   string  // Generated in init -- PROBLEM with SnapStart!
)

func init() {
	start := time.Now()

	// Simulate heavy initialization
	time.Sleep(800 * time.Millisecond) // Simulates loading config, warming caches

	// Generate instance ID in init -- this value will be shared
	// across all SnapStart restores (snapshot uniqueness problem)
	bytes := make([]byte, 16)
	rand.Read(bytes)
	instanceID = hex.EncodeToString(bytes)

	initTime = time.Now()
	initDuration = time.Since(start)
	fmt.Printf("INIT completed in %dms, instanceID=%s\n", initDuration.Milliseconds(), instanceID)
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	invokeTime := time.Now()
	timeSinceInit := invokeTime.Sub(initTime)

	return map[string]interface{}{
		"function_name":     os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"init_duration_ms":  initDuration.Milliseconds(),
		"time_since_init_ms": timeSinceInit.Milliseconds(),
		"instance_id":       instanceID,
		"invoke_time":       invokeTime.Format(time.RFC3339Nano),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

With SnapStart, the `init()` function runs once during the snapshot creation. All subsequent cold starts restore from the snapshot, meaning `instanceID` is the same across all environments -- a security risk if used for encryption keys or session tokens.

</details>

<details>
<summary>Hint 3: Fixing the uniqueness problem</summary>

Move unique value generation from `init()` to the handler or use a lazy initialization pattern:

```go
var (
	initTime     time.Time
	initDuration time.Duration
	instanceID   string
	idGenerated  bool
)

func init() {
	start := time.Now()
	time.Sleep(800 * time.Millisecond)
	initTime = time.Now()
	initDuration = time.Since(start)
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	// Generate unique ID on first invocation AFTER restore, not during INIT
	if !idGenerated {
		bytes := make([]byte, 16)
		rand.Read(bytes)
		instanceID = hex.EncodeToString(bytes)
		idGenerated = true
	}

	// ... rest of handler
}
```

For Java, AWS provides the CRaC (Coordinated Restore at Checkpoint) API with `beforeCheckpoint()` and `afterRestore()` hooks. For Go, you must handle this manually by generating unique values in the handler.

</details>

<details>
<summary>Hint 4: Measuring the difference</summary>

CloudWatch Logs include an `Init Duration` field in the REPORT line for cold starts. For SnapStart, you see a `Restore Duration` instead:

```bash
# Without SnapStart -- look for "Init Duration"
aws logs filter-log-events \
  --log-group-name /aws/lambda/no-snapstart-demo \
  --filter-pattern "REPORT" \
  --query "events[*].message" --output text | grep "Init Duration"

# With SnapStart -- look for "Restore Duration"
aws logs filter-log-events \
  --log-group-name /aws/lambda/snapstart-demo \
  --filter-pattern "REPORT" \
  --query "events[*].message" --output text | grep "Restore Duration"
```

Typical results:
- Without SnapStart: `Init Duration: 850.00 ms` (full init() execution)
- With SnapStart: `Restore Duration: 180.00 ms` (snapshot restore)

To force a cold start for testing, update a dummy environment variable:

```bash
aws lambda update-function-configuration \
  --function-name snapstart-demo \
  --environment '{"Variables":{"FORCE_COLD_START":"'$(date +%s)'"}}'
```

</details>

## Spot the Bug

A developer enables SnapStart and sets up provisioned concurrency on the same published version:

```hcl
resource "aws_lambda_function" "this" {
  function_name = "fast-startup"
  publish       = true

  snap_start {
    apply_on = "PublishedVersions"
  }
}

resource "aws_lambda_provisioned_concurrency_config" "this" {
  function_name                  = aws_lambda_function.this.function_name
  qualifier                      = aws_lambda_function.this.version
  provisioned_concurrent_executions = 5
}
```

<details>
<summary>Explain the bug</summary>

SnapStart and provisioned concurrency **cannot be used together** on the same function version. They solve the same problem (cold starts) using different mechanisms:

- SnapStart: reduces cold start time by restoring from a cached snapshot (free)
- Provisioned concurrency: eliminates cold starts entirely by pre-warming environments (paid)

Applying both to the same version results in `InvalidParameterValueException: Provisioned Concurrency is not compatible with SnapStart`.

Choose one:
- Use **SnapStart** when cold start reduction (not elimination) is sufficient and cost is a concern
- Use **provisioned concurrency** when zero cold starts are required (e.g., latency-sensitive API with strict SLA)
- You can use SnapStart on one alias and provisioned concurrency on a different alias of the same function, but not on the same version

```hcl
# Option A: SnapStart only (free, reduces cold starts)
resource "aws_lambda_function" "this" {
  function_name = "fast-startup"
  publish       = true

  snap_start {
    apply_on = "PublishedVersions"
  }
}

# Option B: Provisioned concurrency only (paid, eliminates cold starts)
resource "aws_lambda_function" "this" {
  function_name = "fast-startup"
  publish       = true
  # No snap_start block
}

resource "aws_lambda_provisioned_concurrency_config" "this" {
  function_name                  = aws_lambda_function.this.function_name
  qualifier                      = aws_lambda_function.this.version
  provisioned_concurrent_executions = 5
}
```

</details>

## Verify What You Learned

```bash
# Verify SnapStart is enabled on the published version
aws lambda get-function-configuration \
  --function-name snapstart-demo \
  --qualifier $(aws lambda list-versions-by-function --function-name snapstart-demo --query "Versions[-1].Version" --output text) \
  --query "SnapStart" --output json
```

Expected: `{"ApplyOn": "PublishedVersions", "OptimizationStatus": "On"}`

```bash
# Invoke to trigger a cold start and check Restore Duration
aws lambda invoke --function-name snapstart-demo \
  --qualifier $(aws lambda list-versions-by-function --function-name snapstart-demo --query "Versions[-1].Version" --output text) \
  /dev/stdout 2>/dev/null | jq .
```

```bash
# Compare init durations in CloudWatch Logs
sleep 10
echo "=== Without SnapStart ==="
aws logs filter-log-events --log-group-name /aws/lambda/no-snapstart-demo \
  --filter-pattern "REPORT" --query "events[-1].message" --output text

echo "=== With SnapStart ==="
aws logs filter-log-events --log-group-name /aws/lambda/snapstart-demo \
  --filter-pattern "REPORT" --query "events[-1].message" --output text
```

Expected: The SnapStart function shows `Restore Duration` (significantly lower than `Init Duration` on the non-SnapStart function).

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You measured SnapStart's impact on cold start latency. In the next exercise, you will explore **Lambda recursive invocation detection**, understanding how AWS prevents infinite loops in event-driven architectures.

## Summary

- **SnapStart** takes a snapshot of the initialized execution environment and restores from it on cold starts
- Reduces cold start latency by **up to 90%** (restore ~200ms vs full INIT of 1-10s for Java)
- Supported runtimes: **Java 11+, Python 3.12+, .NET 8+**, and Go (provided.al2023, preview)
- Requires `publish = true` -- works on published versions, not `$LATEST`
- **Free** -- no additional charge beyond standard Lambda pricing
- **Snapshot uniqueness**: values generated in `init()` are shared across all restored environments; generate unique values in the handler
- **Cannot combine** with provisioned concurrency on the same version
- Does not work with **container images** or **ephemeral storage > 512 MB**
- CloudWatch shows `Restore Duration` instead of `Init Duration` for SnapStart cold starts

Key exam comparison:

| Feature | SnapStart | Provisioned Concurrency |
|---------|-----------|------------------------|
| Cost | Free | Paid (even when idle) |
| Cold start | Reduced (~200ms restore) | Eliminated (0ms) |
| Configuration | `snap_start.apply_on` | `provisioned_concurrent_executions` |
| Target | `PublishedVersions` | Alias or version |
| Uniqueness concern | Yes (shared snapshot) | No (each env initialized independently) |

## Reference

- [Lambda SnapStart](https://docs.aws.amazon.com/lambda/latest/dg/snapstart.html)
- [SnapStart Compatibility](https://docs.aws.amazon.com/lambda/latest/dg/snapstart-compatibility.html)
- [Terraform aws_lambda_function snap_start](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lambda_function#snap_start)
- [Lambda Cold Start Optimization](https://docs.aws.amazon.com/lambda/latest/operatorguide/execution-environments.html)

## Additional Resources

- [SnapStart Uniqueness Considerations](https://docs.aws.amazon.com/lambda/latest/dg/snapstart-uniqueness.html) -- handling unique values, random number generators, and connections
- [CRaC API for Java](https://docs.aws.amazon.com/lambda/latest/dg/snapstart-runtime-hooks.html) -- coordinated checkpoint/restore hooks for Java applications
- [SnapStart Best Practices](https://docs.aws.amazon.com/lambda/latest/dg/snapstart-best-practices.html) -- optimizing snapshot creation and restore performance
- [Measuring Lambda Cold Starts](https://docs.aws.amazon.com/lambda/latest/operatorguide/execution-environments.html#cold-start-latency) -- how to accurately measure init and restore durations

<details>
<summary>Full Solution</summary>

### File Structure

```
26-lambda-snapstart-fast-cold-starts/
├── main.go
├── go.mod
└── main.tf
```

### `main.go`

```go
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-lambda-go/lambda"
)

var (
	initTime     time.Time
	initDuration time.Duration
	instanceID   string
	idGenerated  bool
)

func init() {
	start := time.Now()
	// Simulate heavy initialization
	time.Sleep(800 * time.Millisecond)
	initTime = time.Now()
	initDuration = time.Since(start)
	fmt.Printf("INIT completed in %dms\n", initDuration.Milliseconds())
}

func handler(ctx context.Context, event json.RawMessage) (map[string]interface{}, error) {
	// Generate unique ID AFTER restore, not during INIT (SnapStart-safe)
	if !idGenerated {
		bytes := make([]byte, 16)
		rand.Read(bytes)
		instanceID = hex.EncodeToString(bytes)
		idGenerated = true
	}

	invokeTime := time.Now()

	return map[string]interface{}{
		"function_name":      os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"init_duration_ms":   initDuration.Milliseconds(),
		"time_since_init_ms": invokeTime.Sub(initTime).Milliseconds(),
		"instance_id":        instanceID,
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

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
  description = "Project name for resource naming"
  type        = string
  default     = "snapstart-demo"
}
```

### `build.tf`

```hcl
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

### `lambda.tf`

```hcl
# -- Without SnapStart (baseline) --
resource "aws_lambda_function" "no_snapstart" {
  function_name    = "no-${var.project_name}"
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30
  publish          = true

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.no_snapstart]
}

# -- With SnapStart --
resource "aws_lambda_function" "snapstart" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  memory_size      = 256
  timeout          = 30
  publish          = true

  snap_start {
    apply_on = "PublishedVersions"
  }

  depends_on = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.snapstart]
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "no_snapstart" {
  name              = "/aws/lambda/no-${var.project_name}"
  retention_in_days = 1
}

resource "aws_cloudwatch_log_group" "snapstart" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `outputs.tf`

```hcl
output "no_snapstart_function" { value = aws_lambda_function.no_snapstart.function_name }
output "snapstart_function"    { value = aws_lambda_function.snapstart.function_name }
```

</details>
