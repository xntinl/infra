# 69. CloudFormation Custom Resources with Lambda

<!--
difficulty: basic
concepts: [cloudformation-custom-resource, cfn-response, lambda-backed-custom-resource, pre-signed-url, create-update-delete, physical-resource-id]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [01-lambda-environment-layers-configuration]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a CloudFormation stack, a Lambda function, and associated IAM resources. Cost is approximately $0.01/hr. Remember to delete the CloudFormation stack and run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how CloudFormation custom resources extend CFN beyond its native resource types
- **Construct** a Go Lambda function that handles Create, Update, and Delete lifecycle events from CloudFormation
- **Describe** the response protocol: Lambda must send SUCCESS or FAILED to the pre-signed S3 URL provided in the event
- **Verify** that the custom resource outputs are accessible via CloudFormation stack outputs
- **Identify** common failure modes: Lambda timeout, missing response, incorrect PhysicalResourceId

## Why CloudFormation Custom Resources

CloudFormation supports hundreds of AWS resource types, but sometimes you need to provision something it does not natively support -- looking up an AMI ID based on filters, creating a resource in a third-party service, running a database migration, or generating a random password. Custom resources fill this gap by invoking a Lambda function during stack creation, update, and deletion.

The protocol: (1) CloudFormation sends an event to your Lambda with `RequestType` (Create, Update, Delete), a `ResponseURL` (pre-signed S3 URL), and input properties; (2) your Lambda performs the custom action; (3) your Lambda sends a JSON response to the `ResponseURL` with `Status` (SUCCESS or FAILED), `PhysicalResourceId`, and optional `Data` (key-value pairs accessible via `!GetAtt`).

The DVA-C02 exam tests the response protocol. The most common failure: the Lambda function completes its work but **forgets to send the response to the pre-signed URL**. CloudFormation waits for the response until the stack operation times out (default 1 hour), then marks the custom resource as FAILED. Understanding this protocol is essential for debugging stuck stacks.

## Step 1 -- Create the Custom Resource Lambda

### `custom-resource/main.go`

```go
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
)

type CloudFormationEvent struct {
	RequestType        string            `json:"RequestType"`
	ResponseURL        string            `json:"ResponseURL"`
	StackId            string            `json:"StackId"`
	RequestId          string            `json:"RequestId"`
	LogicalResourceId  string            `json:"LogicalResourceId"`
	PhysicalResourceId string            `json:"PhysicalResourceId,omitempty"`
	ResourceProperties map[string]string `json:"ResourceProperties"`
}

type CloudFormationResponse struct {
	Status             string            `json:"Status"`
	Reason             string            `json:"Reason,omitempty"`
	PhysicalResourceId string            `json:"PhysicalResourceId"`
	StackId            string            `json:"StackId"`
	RequestId          string            `json:"RequestId"`
	LogicalResourceId  string            `json:"LogicalResourceId"`
	Data               map[string]string `json:"Data,omitempty"`
}

func handler(ctx context.Context, event CloudFormationEvent) error {
	response := CloudFormationResponse{
		StackId:           event.StackId,
		RequestId:         event.RequestId,
		LogicalResourceId: event.LogicalResourceId,
	}

	switch event.RequestType {
	case "Create", "Update":
		amiID, err := lookupAMI(ctx, event.ResourceProperties["Architecture"])
		if err != nil {
			response.Status = "FAILED"
			response.Reason = fmt.Sprintf("AMI lookup failed: %v", err)
			response.PhysicalResourceId = "failed-lookup"
		} else {
			response.Status = "SUCCESS"
			response.PhysicalResourceId = amiID
			response.Data = map[string]string{
				"AmiId":        amiID,
				"Architecture": event.ResourceProperties["Architecture"],
			}
		}

	case "Delete":
		// Nothing to clean up for an AMI lookup
		response.Status = "SUCCESS"
		response.PhysicalResourceId = event.PhysicalResourceId
	}

	return sendResponse(event.ResponseURL, response)
}

func lookupAMI(ctx context.Context, arch string) (string, error) {
	if arch == "" {
		arch = "x86_64"
	}

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return "", err
	}

	ec2Client := ec2.NewFromConfig(cfg)
	result, err := ec2Client.DescribeImages(ctx, &ec2.DescribeImagesInput{
		Owners: []string{"amazon"},
		Filters: []ec2types.Filter{
			{Name: aws.String("name"), Values: []string{"al2023-ami-2023.*"}},
			{Name: aws.String("architecture"), Values: []string{arch}},
			{Name: aws.String("state"), Values: []string{"available"}},
			{Name: aws.String("virtualization-type"), Values: []string{"hvm"}},
		},
	})
	if err != nil {
		return "", err
	}

	if len(result.Images) == 0 {
		return "", fmt.Errorf("no AMI found for architecture %s", arch)
	}

	// Find the most recent image
	latest := result.Images[0]
	for _, img := range result.Images[1:] {
		if *img.CreationDate > *latest.CreationDate {
			latest = img
		}
	}

	return *latest.ImageId, nil
}

func sendResponse(url string, response CloudFormationResponse) error {
	body, _ := json.Marshal(response)
	req, _ := http.NewRequest("PUT", url, bytes.NewReader(body))
	req.Header.Set("Content-Type", "")
	resp, err := (&http.Client{}).Do(req)
	if err != nil { return fmt.Errorf("failed to send response: %w", err) }
	defer resp.Body.Close()
	if resp.StatusCode != 200 { return fmt.Errorf("response status %d", resp.StatusCode) }
	return nil
}

func main() {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" {
		lambda.Start(handler)
	}
}
```

## Step 2 -- Create the Terraform Configuration for the Lambda

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
  default     = "cfn-custom-resource-demo"
}
```

### `main.tf`

```hcl
# IAM: Lambda assume role + AWSLambdaBasicExecutionRole + ec2:DescribeImages
# Lambda: runtime = "provided.al2023", handler = "bootstrap", timeout = 60
```

### `outputs.tf`

```hcl
output "lambda_arn" {
  description = "ARN of the custom resource Lambda function"
  value       = aws_lambda_function.this.arn
}
```

## Step 3 -- Create the CloudFormation Template

Create a file named `cfn-template.yaml`. This template uses the Lambda-backed custom resource to look up the AMI ID:

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: Custom resource demo -- looks up latest AL2023 AMI

Parameters:
  LambdaArn:
    Type: String
    Description: ARN of the custom resource Lambda function
  Architecture:
    Type: String
    Default: arm64
    AllowedValues: [x86_64, arm64]

Resources:
  AmiLookup:
    Type: Custom::AmiLookup
    Properties:
      ServiceToken: !Ref LambdaArn
      Architecture: !Ref Architecture

Outputs:
  AmiId:
    Description: Latest AL2023 AMI ID
    Value: !GetAtt AmiLookup.AmiId
  AmiArchitecture:
    Description: AMI architecture
    Value: !GetAtt AmiLookup.Architecture
```

## Step 4 -- Build, Deploy, and Create the Stack

```bash
# Build the Lambda
cd custom-resource && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..

# Deploy the Lambda via Terraform
terraform init
terraform apply -auto-approve

# Create the CloudFormation stack using the custom resource
LAMBDA_ARN=$(terraform output -raw lambda_arn)
aws cloudformation create-stack \
  --stack-name cfn-custom-resource-demo \
  --template-body file://cfn-template.yaml \
  --parameters ParameterKey=LambdaArn,ParameterValue="$LAMBDA_ARN" ParameterKey=Architecture,ParameterValue=arm64

# Wait for stack creation
aws cloudformation wait stack-create-complete --stack-name cfn-custom-resource-demo
echo "Stack created successfully"
```

## Step 5 -- Verify the Custom Resource Output

```bash
# Get the AMI ID from stack outputs
aws cloudformation describe-stacks \
  --stack-name cfn-custom-resource-demo \
  --query "Stacks[0].Outputs" --output table
```

Expected: `AmiId` with a valid AMI ID (e.g., `ami-0abc12345def67890`) and `AmiArchitecture` showing `arm64`.

## Common Mistakes

### 1. Not sending a response to the pre-signed URL

If the Lambda does not send a response to the `ResponseURL`, CloudFormation waits until timeout (default 1 hour). **Fix:** Always send a response, even on failure.

### 2. Changing the PhysicalResourceId on Update

A different `PhysicalResourceId` on Update triggers a Delete event for the old ID (CloudFormation interprets it as replacement). **Fix:** Return the same ID unless the resource truly needs replacement.

### 3. Lambda timeout shorter than the custom action

If the Lambda times out before sending the response, CloudFormation waits for its own timeout. **Fix:** Set Lambda timeout to cover worst-case execution plus response transmission.

## Verify What You Learned

```bash
# Verify the AMI ID is valid
AMI_ID=$(aws cloudformation describe-stacks \
  --stack-name cfn-custom-resource-demo \
  --query "Stacks[0].Outputs[?OutputKey=='AmiId'].OutputValue" --output text)
aws ec2 describe-images --image-ids "$AMI_ID" \
  --query "Images[0].{Id:ImageId,Name:Name,Arch:Architecture}" --output table
```

Expected: a valid Amazon Linux 2023 AMI with `arm64` architecture.

```bash
# Verify stack status
aws cloudformation describe-stacks \
  --stack-name cfn-custom-resource-demo \
  --query "Stacks[0].StackStatus" --output text
```

Expected: `CREATE_COMPLETE`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Delete the CloudFormation stack first, then destroy Terraform resources:

```bash
# Delete CFN stack (triggers Delete event on custom resource)
aws cloudformation delete-stack --stack-name cfn-custom-resource-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-custom-resource-demo

# Destroy Terraform resources
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
aws cloudformation describe-stacks --stack-name cfn-custom-resource-demo 2>&1 | head -1
terraform state list
```

Expected: stack not found error and empty Terraform state.

## What's Next

You built a CloudFormation custom resource backed by a Go Lambda function. In the next exercise, you will explore **CloudFormation nested stacks** -- composing complex infrastructure from reusable child stack templates.

## Summary

- **Custom resources** extend CloudFormation with Lambda-backed provisioning logic for unsupported resource types
- The Lambda receives events with `RequestType` (Create, Update, Delete) and must respond to the **pre-signed S3 URL**
- The response must include `Status` (SUCCESS/FAILED), `PhysicalResourceId`, and optional `Data` (key-value outputs)
- Outputs in `Data` are accessible in the template via `!GetAtt LogicalName.Key`
- If the Lambda **fails to send a response**, CloudFormation waits until timeout (up to 1 hour by default)
- `Custom::TypeName` or `AWS::CloudFormation::CustomResource` -- both work, the type name after `Custom::` is informational
- The `ServiceToken` property specifies the Lambda ARN that CloudFormation invokes
- Set Lambda timeout generously -- the function must complete AND send the response before timing out

## Reference

- [CloudFormation Custom Resources](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/template-custom-resources.html)
- [Custom Resource Response Objects](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/crpg-ref-responses.html)
- [Custom Resource Request Objects](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/crpg-ref-requesttypes.html)

## Additional Resources

- [cfn-response Module](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/cfn-lambda-function-code-cfnresponsemodule.html) -- pre-built response helper (Node.js/Python only)
- [Custom Resource Best Practices](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/best-practices.html#cfn-best-practices-custom) -- AWS guidance
- [Stabilization and Timeouts](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-resource-cfn-customresource.html) -- timeout behavior and recovery
- [CDK Custom Resources](https://docs.aws.amazon.com/cdk/v2/guide/custom-resources.html) -- alternative approach using AWS CDK
