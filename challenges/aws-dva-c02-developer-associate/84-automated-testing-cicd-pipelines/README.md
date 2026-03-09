# 84. Automated Testing in CI/CD Pipelines

<!--
difficulty: advanced
concepts: [codebuild-test-reports, test-report-groups, junit-reports, code-coverage, pipeline-test-gates, unit-testing, integration-testing, buildspec-test-phase, test-artifacts]
tools: [terraform, aws-cli, go]
estimated_time: 55m
bloom_level: create, evaluate
prerequisites: [78-codebuild-environment-variables-secrets, 13-codepipeline-codebuild-lambda-deploy]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates CodePipeline, CodeBuild projects, and supporting resources. CodePipeline costs $1/month per active pipeline (prorated). CodeBuild charges per build minute (~$0.005/min). Estimated ~$0.02/hr during active use. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed (`go version`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CodeBuild project with a buildspec that runs unit tests, generates JUnit XML reports, and publishes code coverage data
- **Configure** CodeBuild test report groups to aggregate test results across builds with pass/fail trends and duration tracking
- **Design** a CodePipeline with separate build and test stages where the test stage gates deployment based on test results
- **Evaluate** the CodeBuild test report format (JUnit XML, Cucumber JSON) and code coverage format (JaCoCo, Clover, Cobertura, SimpleCov) supported by CodeBuild
- **Analyze** how test failures in CodeBuild propagate to CodePipeline -- a failed test phase fails the build action, which blocks the pipeline

## Why This Matters

Automated testing in CI/CD pipelines catches bugs before they reach production. CodeBuild integrates with test reporting natively -- you configure report groups in the buildspec, and CodeBuild collects test results, displays them in the console, and tracks trends over time. This eliminates the need for external test result aggregation tools.

The DVA-C02 exam tests CI/CD testing in pipeline design questions. Key concepts: CodeBuild buildspec has a `reports` section that defines report groups and their file locations. Test results in JUnit XML format are parsed by CodeBuild and displayed as pass/fail/skip with duration. If the `build` phase fails (e.g., tests fail and `go test` returns non-zero), the CodeBuild build fails, which fails the pipeline action, which blocks the pipeline from proceeding to the deploy stage.

A common exam question: "How do you ensure that a deployment only proceeds if all unit tests pass?" The answer is: put tests in the CodeBuild build phase, enable test reports for visibility, and the pipeline naturally gates on the build action's success/failure status. If tests fail, the build fails, and the deploy stage is never reached.

## The Challenge

Build a CI/CD pipeline with automated testing at two stages: unit tests during build (gates deployment) and integration tests post-deploy (validates the deployed service). Use CodeBuild test reports to track test results.

### Requirements

| Requirement | Description |
|---|---|
| Unit Test Build | CodeBuild project running Go unit tests with JUnit XML output |
| Test Reports | CodeBuild test report group collecting JUnit results |
| Coverage Reports | Code coverage output in Cobertura format |
| Pipeline Gate | Unit test failure blocks the deploy stage |
| Post-Deploy Tests | Integration test project that validates the deployed Lambda |

### Architecture

```
  Source    Build+Test     Approval    Deploy    Integration
  +-----+  +----------+  +--------+  +------+  +----------+
  | S3  |->| CodeBuild|->| Manual |->| Lambda|->| CodeBuild|
  |     |  | go test  |  | Gate   |  | Update|  | API test |
  +-----+  | + report |  +--------+  +------+  | + report |
            +----------+                        +----------+
```

## Hints

<details>
<summary>Hint 1: Buildspec with test reports</summary>

The `reports` section in buildspec.yml defines test report groups:

```yaml
version: 0.2

phases:
  install:
    runtime-versions:
      golang: 1.21
    commands:
      - go install github.com/jstemmer/go-junit-report/v2@latest

  build:
    commands:
      # Run tests and generate JUnit XML
      - go test -v -coverprofile=coverage.out ./... 2>&1 | go-junit-report -set-exit-code > report.xml
      # Generate Cobertura coverage report
      - go install github.com/boumenot/gocover-cobertura@latest
      - gocover-cobertura < coverage.out > coverage.xml

reports:
  unit-test-report:
    files:
      - report.xml
    file-format: JUNITXML
  coverage-report:
    files:
      - coverage.xml
    file-format: COBERTURAXML

artifacts:
  files:
    - function.zip
```

The report group name (`unit-test-report`) is prefixed with the CodeBuild project name to form the full report group ARN. CodeBuild creates the report group automatically if it does not exist.

</details>

<details>
<summary>Hint 2: Go test code for JUnit output</summary>

Go tests need the `go-junit-report` tool to convert `go test -v` output to JUnit XML:

```go
// handler_test.go
package main

import (
    "testing"
)

func TestHandlerSuccess(t *testing.T) {
    event := OrderEvent{OrderID: "test-001", Amount: 49.99}
    result, err := processOrder(event)
    if err != nil {
        t.Fatalf("expected no error, got: %v", err)
    }
    if result.Status != "processed" {
        t.Errorf("expected status 'processed', got '%s'", result.Status)
    }
}

func TestHandlerValidation(t *testing.T) {
    event := OrderEvent{OrderID: "", Amount: 49.99}
    _, err := processOrder(event)
    if err == nil {
        t.Fatal("expected validation error for empty order ID")
    }
}

func TestHandlerNegativeAmount(t *testing.T) {
    event := OrderEvent{OrderID: "test-002", Amount: -10.00}
    _, err := processOrder(event)
    if err == nil {
        t.Fatal("expected validation error for negative amount")
    }
}
```

</details>

<details>
<summary>Hint 3: CodeBuild report group configuration in Terraform</summary>

```hcl
resource "aws_codebuild_report_group" "unit_tests" {
  name           = "${var.project_name}-unit-tests"
  type           = "TEST"
  delete_reports = true

  export_config {
    type = "NO_EXPORT"
  }
}

resource "aws_codebuild_report_group" "coverage" {
  name           = "${var.project_name}-coverage"
  type           = "CODE_COVERAGE"
  delete_reports = true

  export_config {
    type = "NO_EXPORT"
  }
}
```

You can also export reports to S3 by changing `type = "S3"` with an `s3_destination` block.

</details>

<details>
<summary>Hint 4: Pipeline with test gate pattern</summary>

The pipeline should fail if tests fail. CodeBuild naturally handles this -- if `go test` returns non-zero (tests failed), the build phase fails, CodeBuild reports FAILED, and CodePipeline stops the pipeline at the build stage.

Pipeline stages: Source -> Build (tests fail = action fails = deploy blocked) -> Deploy -> IntegrationTest (separate CodeBuild project validates deployed service).

</details>

<details>
<summary>Hint 5: Integration test buildspec</summary>

The integration test CodeBuild project invokes the deployed Lambda and validates the response:

```yaml
version: 0.2

phases:
  build:
    commands:
      - |
        RESPONSE=$(aws lambda invoke --function-name my-function \
          --payload '{"order_id":"integ-001","amount":99.99}' \
          /dev/stdout 2>/dev/null)
        echo "Response: $RESPONSE"
        STATUS=$(echo "$RESPONSE" | jq -r '.status')
        if [ "$STATUS" != "processed" ]; then
          echo "FAIL: Expected status 'processed', got '$STATUS'"
          exit 1
        fi
        echo "PASS: Integration test passed"

reports:
  integration-test-report:
    files:
      - integ-report.xml
    file-format: JUNITXML
```

</details>

## Spot the Bug

A developer configures CodeBuild test reports, but the reports page shows "No reports found" even though tests ran successfully. **What is wrong?**

```yaml
version: 0.2

phases:
  build:
    commands:
      - go test -v ./... > test-output.txt 2>&1
      - go-junit-report < test-output.txt > /tmp/report.xml

reports:
  my-test-report:
    files:
      - report.xml                    # <-- BUG: wrong path
    base-directory: .                  # Looking in project root
    file-format: JUNITXML
```

<details>
<summary>Explain the bug</summary>

The test report XML is written to `/tmp/report.xml`, but the `reports` section looks for `report.xml` in the base directory (`.`). The file paths in the `files` list are relative to `base-directory`. Since the report is in `/tmp/`, CodeBuild cannot find it.

There are two possible fixes:

**Fix 1: Write the report to the project directory:**
```yaml
phases:
  build:
    commands:
      - go test -v ./... 2>&1 | go-junit-report -set-exit-code > report.xml

reports:
  my-test-report:
    files:
      - report.xml
    file-format: JUNITXML
```

**Fix 2: Change the base-directory:**
```yaml
reports:
  my-test-report:
    files:
      - report.xml
    base-directory: /tmp
    file-format: JUNITXML
```

On the exam, report file path mismatch is the canonical "reports show no data" scenario. Always verify that `files` paths and `base-directory` match where the report is written.

</details>

<details>
<summary>Full Solution</summary>

### Project structure

```
84-automated-testing-cicd-pipelines/
├── app/
│   ├── main.go
│   ├── handler.go
│   ├── handler_test.go
│   └── go.mod
├── buildspec-unit.yml
├── buildspec-integ.yml
└── main.tf
```

### `app/handler.go`

```go
package main

import "fmt"

type OrderEvent struct {
    OrderID string  `json:"order_id"`
    Amount  float64 `json:"amount"`
}

type OrderResult struct {
    OrderID string `json:"order_id"`
    Status  string `json:"status"`
}

func processOrder(event OrderEvent) (OrderResult, error) {
    if event.OrderID == "" {
        return OrderResult{}, fmt.Errorf("order_id is required")
    }
    if event.Amount <= 0 {
        return OrderResult{}, fmt.Errorf("amount must be positive")
    }
    return OrderResult{OrderID: event.OrderID, Status: "processed"}, nil
}
```

### `app/handler_test.go`

```go
package main

import "testing"

func TestProcessOrderSuccess(t *testing.T) {
    result, err := processOrder(OrderEvent{OrderID: "test-001", Amount: 49.99})
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if result.Status != "processed" {
        t.Errorf("expected 'processed', got '%s'", result.Status)
    }
}

func TestProcessOrderEmptyID(t *testing.T) {
    _, err := processOrder(OrderEvent{OrderID: "", Amount: 49.99})
    if err == nil {
        t.Fatal("expected error for empty order ID")
    }
}

func TestProcessOrderNegativeAmount(t *testing.T) {
    _, err := processOrder(OrderEvent{OrderID: "test-002", Amount: -10.00})
    if err == nil {
        t.Fatal("expected error for negative amount")
    }
}
```

### `app/main.go`

```go
package main

import (
    "context"
    "github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event OrderEvent) (OrderResult, error) {
    return processOrder(event)
}

func main() { lambda.Start(handler) }
```

### `app/go.mod`

```
module cicd-testing-demo

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
```

### `buildspec-unit.yml`

```yaml
version: 0.2
phases:
  install:
    runtime-versions:
      golang: 1.21
    commands:
      - go install github.com/jstemmer/go-junit-report/v2@latest
      - go install github.com/boumenot/gocover-cobertura@latest
  build:
    commands:
      - cd app && go test -v -coverprofile=coverage.out ./... 2>&1 | go-junit-report -set-exit-code > report.xml
      - gocover-cobertura < coverage.out > coverage.xml
      - GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap . && zip -j ../function.zip bootstrap && cd ..
reports:
  unit-test-report:
    files: [app/report.xml]
    file-format: JUNITXML
  coverage-report:
    files: [app/coverage.xml]
    file-format: COBERTURAXML
artifacts:
  files: [function.zip]
```

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
  default     = "cicd-testing-demo"
}
```

### `cicd.tf`

The Terraform configuration creates: S3 buckets (source + artifacts), Lambda function, two CodeBuild projects (unit + integration), CodePipeline with four stages (Source, BuildAndTest, Deploy, IntegrationTest), and required IAM roles.

Key CodeBuild IAM permissions for test reports:
```hcl
statement {
  actions = ["codebuild:CreateReportGroup", "codebuild:CreateReport",
             "codebuild:UpdateReport", "codebuild:BatchPutTestCases",
             "codebuild:BatchPutCodeCoverages"]
  resources = ["arn:aws:codebuild:*:*:report-group/${var.project_name}*"]
}
```

Pipeline stages:
```hcl
# Source -> BuildAndTest (unit tests gate deployment) -> Deploy -> IntegrationTest
```

### `outputs.tf`

```hcl
output "pipeline_name" {
  value = aws_codepipeline.this.name
}

output "unit_test_project" {
  value = aws_codebuild_project.unit_tests.name
}

output "integ_test_project" {
  value = aws_codebuild_project.integ_tests.name
}
```

</details>

## Verify What You Learned

```bash
# Verify CodeBuild projects exist
aws codebuild batch-get-projects --names \
  $(terraform output -raw unit_test_project) \
  $(terraform output -raw integ_test_project) \
  --query "projects[*].{Name:name,Source:source.type}" --output table
```

Expected: two CodeBuild projects with CODEPIPELINE source type.

```bash
# Verify pipeline stages
aws codepipeline get-pipeline --name $(terraform output -raw pipeline_name) \
  --query "pipeline.stages[*].name" --output json
```

Expected: `["Source", "BuildAndTest", "Deploy", "IntegrationTest"]`

```bash
# List report groups
aws codebuild list-report-groups --query "reportGroups" --output json
```

Expected: report groups created after the first build run.

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

You built a pipeline with automated testing and test reports. Next, you will explore **CloudWatch custom metrics and dimensions** -- publishing application-specific metrics from Lambda functions.

## Summary

- **CodeBuild test reports** aggregate results from JUnit XML, Cucumber JSON, or TRX files -- configure in the `reports` section of buildspec.yml
- **Code coverage** supports JaCoCo, Clover, Cobertura, and SimpleCov formats
- Report file paths in `files` are relative to `base-directory` -- mismatched paths cause "no reports found"
- CodeBuild needs `codebuild:CreateReportGroup`, `CreateReport`, `UpdateReport`, `BatchPutTestCases`, `BatchPutCodeCoverages` IAM permissions
- **Test gates**: failed tests -> failed build -> failed pipeline action -> deploy stage blocked
- **Integration tests** run as a separate CodeBuild project in a post-deploy stage
- Use `go-junit-report` for JUnit XML and `gocover-cobertura` for Cobertura coverage format

## Reference

- [CodeBuild Test Reports](https://docs.aws.amazon.com/codebuild/latest/userguide/test-reporting.html)
- [CodeBuild Report Group Types](https://docs.aws.amazon.com/codebuild/latest/userguide/test-report-group.html)
- [Buildspec Reports Section](https://docs.aws.amazon.com/codebuild/latest/userguide/build-spec-ref.html#build-spec.reports)
- [Terraform aws_codebuild_report_group](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codebuild_report_group)

## Additional Resources

- [CodeBuild Code Coverage Reports](https://docs.aws.amazon.com/codebuild/latest/userguide/code-coverage-report.html) -- supported coverage formats
- [go-junit-report](https://github.com/jstemmer/go-junit-report) -- Go test to JUnit XML converter
- [CodePipeline Test Actions](https://docs.aws.amazon.com/codepipeline/latest/userguide/actions-test.html) -- using CodeBuild as a test action
