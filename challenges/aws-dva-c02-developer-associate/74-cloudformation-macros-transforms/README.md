# 74. CloudFormation Macros and Transforms

<!--
difficulty: advanced
concepts: [cfn-macros, cfn-transforms, lambda-backed-transform, template-fragment-processing, macro-response-format, requestid-status, aws-include-transform, aws-serverless-transform]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: create, evaluate
prerequisites: [11-cloudformation-intrinsic-functions-rollback, 01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates Lambda functions and CloudFormation stacks with custom macros. All costs are negligible for testing (~$0.01/hr or less). Remember to delete your stacks and run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CloudFormation macro backed by a Lambda function that processes template fragments and auto-adds tags to all taggable resources
- **Evaluate** the macro event format (region, accountId, fragment, params, requestId, transformId) and the required response format (requestId, status, fragment)
- **Design** fragment processing logic that traverses nested resources and conditionally modifies their properties
- **Differentiate** between template-level transforms (`Transform` in template body) and resource-level transforms (`Fn::Transform` on individual resources)
- **Analyze** macro failure modes: wrong response format, missing requestId, status not "success", and Lambda timeout behavior

## Why This Matters

CloudFormation macros are Lambda-backed template preprocessors. Before CloudFormation creates any resources, it sends the template (or a fragment of it) to the macro Lambda function, which can modify it and return the transformed version. This enables powerful patterns: auto-tagging all resources, injecting security configurations, generating boilerplate resources, or enforcing organizational policies.

AWS provides built-in transforms: `AWS::Serverless-2016-10-31` (SAM) and `AWS::Include` (S3 snippets). Custom macros extend this with your own logic.

The DVA-C02 exam tests: the macro Lambda receives `fragment`, `params`, `requestId`, and `transformId`. It must return `requestId` (matching input), `status` (must be `"success"`), and `fragment` (modified template). Wrong format, timeout, or wrong status causes stack creation failure.

## The Challenge

Build a CloudFormation macro that automatically adds standard tags (Environment, Project, ManagedBy) to every taggable resource in a template. Then use the macro in a CloudFormation template to verify the tags are injected.

### Requirements

| Requirement | Description |
|---|---|
| Macro Lambda | Go function that processes CloudFormation template fragments |
| Tag injection | Add Environment, Project, and ManagedBy tags to all resources with a Tags property |
| Template-level macro | Apply the macro using the `Transform` key at the template level |
| Parameterization | Accept Environment and Project as macro parameters |
| Error handling | Return proper error responses for invalid input |

### Architecture

```
  CloudFormation          Macro Lambda          CloudFormation
  +---------------+    +------------------+    +---------------+
  | Template with |    | Process fragment |    | Modified      |
  | Transform:    |--->| Add tags to all  |--->| template with |
  | AutoTag       |    | taggable resources|   | tags injected |
  +---------------+    +------------------+    +---------------+
```

## Hints

<details>
<summary>Hint 1: Macro Lambda event format</summary>

The macro Lambda receives this event structure:

```json
{
  "region": "us-east-1",
  "accountId": "123456789012",
  "fragment": {
    "AWSTemplateFormatVersion": "2010-09-09",
    "Description": "...",
    "Resources": {
      "MyBucket": {
        "Type": "AWS::S3::Bucket",
        "Properties": {
          "BucketName": "my-bucket"
        }
      }
    }
  },
  "transformId": "123456789012::AutoTag",
  "params": {
    "Environment": "production",
    "Project": "my-app"
  },
  "requestId": "unique-request-id",
  "templateParameterValues": {}
}
```

For template-level macros, `fragment` is the entire template body. For resource-level macros (`Fn::Transform`), it is just the resource's properties.

</details>

<details>
<summary>Hint 2: Required response format</summary>

The Lambda must return exactly this structure:

```json
{
  "requestId": "unique-request-id",
  "status": "success",
  "fragment": {
    "...modified template fragment..."
  }
}
```

Critical rules:
- `requestId` must match the input `requestId` exactly
- `status` must be the string `"success"` (lowercase) for the transform to succeed
- `fragment` contains the modified template -- CloudFormation uses this instead of the original
- Any other status value (including `"failed"`, `"error"`, or `"Success"` with capital S) causes the transform to fail

</details>

<details>
<summary>Hint 3: Tag injection logic</summary>

To auto-tag resources, iterate over `fragment.Resources` and check if each resource type supports Tags. Most AWS resources accept a `Tags` property as a list of `{Key, Value}` objects.

```go
resources, ok := fragment["Resources"].(map[string]interface{})
if !ok {
    return response
}

for name, res := range resources {
    resource := res.(map[string]interface{})
    props, hasProps := resource["Properties"].(map[string]interface{})
    if !hasProps {
        props = map[string]interface{}{}
        resource["Properties"] = props
    }

    // Add tags (works for most AWS resource types)
    existingTags, _ := props["Tags"].([]interface{})
    newTags := []interface{}{
        map[string]interface{}{"Key": "Environment", "Value": environment},
        map[string]interface{}{"Key": "Project", "Value": project},
        map[string]interface{}{"Key": "ManagedBy", "Value": "CloudFormation-Macro"},
    }
    props["Tags"] = append(existingTags, newTags...)
}
```


</details>

<details>
<summary>Hint 4: Registering the macro with CloudFormation</summary>

The macro is registered using a `AWS::CloudFormation::Macro` resource:

```yaml
Resources:
  AutoTagMacro:
    Type: AWS::CloudFormation::Macro
    Properties:
      Name: AutoTag
      FunctionName: !GetAtt MacroFunction.Arn
      Description: Automatically adds standard tags to all resources
```

Or in Terraform:

```hcl
resource "aws_cloudformation_stack" "macro" {
  name = "auto-tag-macro"
  template_body = jsonencode({
    Resources = {
      AutoTagMacro = {
        Type = "AWS::CloudFormation::Macro"
        Properties = {
          Name         = "AutoTag"
          FunctionName = aws_lambda_function.macro.arn
        }
      }
    }
  })
}
```

The macro name (`AutoTag`) is what you reference in the `Transform` declaration.

</details>

## Spot the Bug

A developer creates a macro Lambda that adds tags, but CloudFormation fails with "Transform AutoTag failed with: Invalid macro response." **What is wrong?**

```go
func handler(ctx context.Context, event map[string]interface{}) (map[string]interface{}, error) {
    fragment := event["fragment"].(map[string]interface{})

    // ... process fragment, add tags ...

    return map[string]interface{}{
        "status":   "Success",                    // <-- BUG 1
        "fragment": fragment,
        // requestId is missing                    // <-- BUG 2
    }, nil
}
```

<details>
<summary>Explain the bug</summary>

There are two bugs:

**Bug 1: Wrong status value.** The status must be lowercase `"success"`, not `"Success"`. CloudFormation performs a case-sensitive comparison. Any value other than exactly `"success"` is treated as a failure.

**Bug 2: Missing requestId.** The response must include `requestId` matching the input event's `requestId`. Without it, CloudFormation cannot correlate the response with the original transform request.

The fix:

```go
return map[string]interface{}{
    "requestId": event["requestId"],          // Must match input
    "status":    "success",                   // Must be lowercase
    "fragment":  fragment,
}, nil
```

These are the two most common macro failures on the exam: status not exactly `"success"` (case-sensitive) and requestId missing or mismatched. Set Lambda timeout to at least 60 seconds for large templates.

</details>

<details>
<summary>Full Solution</summary>

### `macro/main.go`

```go
package main

import (
	"context"
	"fmt"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event map[string]interface{}) (map[string]interface{}, error) {
	fmt.Printf("Macro invoked: transformId=%s\n", event["transformId"])

	requestId, _ := event["requestId"].(string)
	fragment, _ := event["fragment"].(map[string]interface{})
	params, _ := event["params"].(map[string]interface{})

	// Extract parameters with defaults
	environment := getParam(params, "Environment", "dev")
	project := getParam(params, "Project", "unknown")

	fmt.Printf("Parameters: Environment=%s Project=%s\n", environment, project)

	// Process resources in the fragment
	resources, ok := fragment["Resources"].(map[string]interface{})
	if ok {
		for name, res := range resources {
			resource, ok := res.(map[string]interface{})
			if !ok {
				continue
			}

			props, ok := resource["Properties"].(map[string]interface{})
			if !ok {
				props = map[string]interface{}{}
				resource["Properties"] = props
			}

			// Add tags
			var existingTags []interface{}
			if tags, ok := props["Tags"].([]interface{}); ok {
				existingTags = tags
			}

			newTags := []interface{}{
				map[string]interface{}{"Key": "Environment", "Value": environment},
				map[string]interface{}{"Key": "Project", "Value": project},
				map[string]interface{}{"Key": "ManagedBy", "Value": "CloudFormation-Macro"},
			}

			props["Tags"] = append(existingTags, newTags...)
			fmt.Printf("Tagged resource: %s\n", name)
		}
	}

	return map[string]interface{}{
		"requestId": requestId,
		"status":    "success",
		"fragment":  fragment,
	}, nil
}

func getParam(params map[string]interface{}, key, defaultVal string) string {
	if v, ok := params[key].(string); ok && v != "" {
		return v
	}
	return defaultVal
}

func main() {
	lambda.Start(handler)
}
```

### `macro/go.mod`

```
module cfn-macro

go 1.21

require github.com/aws/aws-lambda-go v1.47.0
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
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "cfn-macro-demo"
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/macro/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/macro"
  }
}

data "archive_file" "macro_zip" {
  type        = "zip"
  source_file = "${path.module}/macro/bootstrap"
  output_path = "${path.module}/build/macro.zip"
  depends_on  = [null_resource.go_build]
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "lambda_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["lambda.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "macro" {
  name               = "${var.project_name}-macro-role"
  assume_role_policy = data.aws_iam_policy_document.lambda_assume.json
}

resource "aws_iam_role_policy_attachment" "macro_basic" {
  role       = aws_iam_role.macro.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
resource "aws_cloudwatch_log_group" "macro" {
  name              = "/aws/lambda/${var.project_name}-macro"
  retention_in_days = 1
}

resource "aws_lambda_function" "macro" {
  function_name    = "${var.project_name}-macro"
  filename         = data.archive_file.macro_zip.output_path
  source_code_hash = data.archive_file.macro_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.macro.arn
  timeout          = 60

  depends_on = [aws_iam_role_policy_attachment.macro_basic, aws_cloudwatch_log_group.macro]
}

resource "aws_lambda_permission" "cfn" {
  statement_id  = "AllowCloudFormation"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.macro.function_name
  principal     = "cloudformation.amazonaws.com"
}
```

### `cloudformation.tf`

```hcl
resource "aws_cloudformation_stack" "macro_registration" {
  name = "${var.project_name}-macro"
  template_body = jsonencode({
    AWSTemplateFormatVersion = "2010-09-09"
    Resources = {
      AutoTagMacro = {
        Type = "AWS::CloudFormation::Macro"
        Properties = {
          Name         = "AutoTag"
          FunctionName = aws_lambda_function.macro.arn
          Description  = "Auto-add standard tags to all resources"
        }
      }
    }
  })
}
```

### `outputs.tf`

```hcl
output "macro_function" {
  value = aws_lambda_function.macro.function_name
}

output "macro_name" {
  value = "AutoTag"
}
```

### Test template (test-template.yaml)

```yaml
AWSTemplateFormatVersion: "2010-09-09"
Transform:
  Name: AutoTag
  Parameters:
    Environment: production
    Project: my-app

Resources:
  TestBucket:
    Type: AWS::S3::Bucket
    Properties:
      BucketName: !Sub "auto-tagged-test-${AWS::AccountId}"

  TestQueue:
    Type: AWS::SQS::Queue
    Properties:
      QueueName: auto-tagged-test-queue
```

### Deploy and test

```bash
# Build and deploy the macro
cd macro && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init && terraform apply -auto-approve

# Create a stack using the macro
aws cloudformation create-stack \
  --stack-name auto-tag-test \
  --template-body file://test-template.yaml \
  --capabilities CAPABILITY_AUTO_EXPAND

aws cloudformation wait stack-create-complete --stack-name auto-tag-test

# Verify tags were injected
aws s3api get-bucket-tagging --bucket "auto-tagged-test-$(aws sts get-caller-identity --query Account --output text)" | jq .
```

</details>

## Verify What You Learned

```bash
aws cloudformation describe-stacks --stack-name cfn-macro-demo-macro \
  --query "Stacks[0].StackStatus" --output text  # Expected: CREATE_COMPLETE
```

```bash
aws cloudformation create-stack --stack-name macro-verify-test \
  --template-body file://test-template.yaml --capabilities CAPABILITY_AUTO_EXPAND
aws cloudformation wait stack-create-complete --stack-name macro-verify-test
```

## Cleanup

```bash
aws cloudformation delete-stack --stack-name macro-verify-test 2>/dev/null
aws cloudformation delete-stack --stack-name auto-tag-test 2>/dev/null
aws cloudformation wait stack-delete-complete --stack-name macro-verify-test 2>/dev/null
terraform destroy -auto-approve
terraform state list  # Expected: empty
```

## What's Next

You built a CloudFormation macro that auto-tags resources. In the next exercise, you will explore **AWS CDK with Go** -- creating infrastructure using L1, L2, and L3 constructs, synthesizing CloudFormation templates, and deploying with `cdk deploy`.

## Summary

- **CloudFormation macros** are Lambda-backed preprocessors that modify templates before resource creation
- The macro Lambda receives `fragment`, `params`, `requestId`, and `transformId`
- Response must include `requestId` (matching input), `status` (exactly `"success"`, case-sensitive), and `fragment`
- **Template-level** transforms process the entire template; **resource-level** (`Fn::Transform`) process individual resources
- `CAPABILITY_AUTO_EXPAND` is required for stacks using custom macros
- Built-in transforms: `AWS::Serverless-2016-10-31` (SAM) and `AWS::Include` (S3 snippets)
- Common failures: wrong status case, missing requestId, Lambda timeout on large templates

## Reference

- [CloudFormation Macros](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/template-macros.html)
- [AWS::CloudFormation::Macro](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-cloudformation-macro.html)
- [Terraform aws_cloudformation_stack](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack)

## Additional Resources

- [Macro Examples](https://github.com/awslabs/aws-cloudformation-templates/tree/master/aws/services/CloudFormation/MacrosExamples) -- AWS-provided macro examples
