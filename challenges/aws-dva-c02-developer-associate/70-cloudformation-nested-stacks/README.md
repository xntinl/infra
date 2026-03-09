# 70. CloudFormation Nested Stacks

<!--
difficulty: intermediate
concepts: [nested-stacks, parent-stack, child-stack, stack-outputs, cross-stack-references, template-url, parameter-passing, s3-template-hosting]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [11-cloudformation-intrinsic-functions-rollback]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates CloudFormation stacks, an S3 bucket for templates, a VPC, security groups, and a Lambda function. Cost is approximately $0.01/hr. Remember to delete all stacks and run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a parent stack that references child stacks using `AWS::CloudFormation::Stack`
2. **Configure** parameter passing from parent to child and output passing from child to parent
3. **Analyze** the lifecycle of nested stacks: how create, update, and delete cascade through the hierarchy
4. **Apply** the pattern of hosting child stack templates in S3 with proper access control
5. **Differentiate** between nested stacks (composition within a single deployment) and cross-stack references (independent stacks sharing outputs via Exports)

## Why Nested Stacks

As CloudFormation templates grow, they become difficult to maintain. A single template with VPC, security groups, IAM roles, Lambda functions, DynamoDB tables, and API Gateway can exceed 1,000 lines. Nested stacks solve this by decomposing infrastructure into reusable child templates.

The parent stack uses `AWS::CloudFormation::Stack` resources to reference child templates. Each child template is a complete CloudFormation template stored in S3. The parent passes parameters down to children and reads their outputs. CloudFormation manages the lifecycle: creating the parent creates all children, deleting the parent deletes all children, and updates cascade through the hierarchy.

The DVA-C02 exam tests the distinction between nested stacks and cross-stack references (Exports/ImportValue). Nested stacks are for **composition** -- components deployed and managed together as a single unit. Cross-stack references are for **sharing** -- independent stacks that export values for other stacks to consume. Nested stacks are preferred when the components have the same lifecycle.

A critical exam point: child stack templates must be accessible via an S3 URL. If the S3 bucket policy does not allow CloudFormation to read the template, the nested stack creation fails with `Template URL must reference a valid S3 object`.

## Building Blocks

This exercise creates three templates: a VPC stack, a security stack, and an app stack. The parent stack orchestrates all three. Your job is to fill in each `# TODO` block.

First, create the Terraform files for managing the template bucket and Lambda build.

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
  default     = "cfn-nested-demo"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "templates" {
  bucket        = "${var.project_name}-templates-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_object" "app_zip" {
  bucket = aws_s3_bucket.templates.id
  key    = "app.zip"
  source = data.archive_file.app_zip.output_path
  etag   = data.archive_file.app_zip.output_md5
}
```

### `build.tf`

```hcl
# Build Lambda for the app stack
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/app/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/app"
  }
}

data "archive_file" "app_zip" {
  type        = "zip"
  source_file = "${path.module}/app/bootstrap"
  output_path = "${path.module}/build/app.zip"
  depends_on  = [null_resource.go_build]
}
```

### `outputs.tf`

```hcl
output "template_bucket" { value = aws_s3_bucket.templates.id }
output "app_zip_key"     { value = aws_s3_object.app_zip.key }
```

Now create the child CloudFormation templates.

### `templates/vpc-stack.yaml`

This child template takes `ProjectName` and `VpcCidr` (default `10.0.0.0/16`) as parameters and creates a VPC with two private subnets in different AZs. Outputs: `VpcId`, `PrivateSubnetAId`, `PrivateSubnetBId`.

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: VPC child stack
Parameters:
  ProjectName: { Type: String }
  VpcCidr: { Type: String, Default: '10.0.0.0/16' }
Resources:
  VPC:
    Type: AWS::EC2::VPC
    Properties:
      CidrBlock: !Ref VpcCidr
      EnableDnsSupport: true
      EnableDnsHostnames: true
      Tags: [{ Key: Name, Value: !Sub '${ProjectName}-vpc' }]
  PrivateSubnetA:
    Type: AWS::EC2::Subnet
    Properties:
      VpcId: !Ref VPC
      CidrBlock: '10.0.1.0/24'
      AvailabilityZone: !Select [0, !GetAZs '']
  PrivateSubnetB:
    Type: AWS::EC2::Subnet
    Properties:
      VpcId: !Ref VPC
      CidrBlock: '10.0.2.0/24'
      AvailabilityZone: !Select [1, !GetAZs '']
Outputs:
  VpcId: { Value: !Ref VPC }
  PrivateSubnetAId: { Value: !Ref PrivateSubnetA }
  PrivateSubnetBId: { Value: !Ref PrivateSubnetB }
```

### `templates/security-stack.yaml`

```yaml
# TODO 1 -- Create the security child stack
#
# This template receives VpcId as a parameter and creates:
#   - A security group for Lambda functions
#     - Egress: all traffic
#     - No ingress rules
#   - An IAM role for Lambda with:
#     - AWSLambdaBasicExecutionRole
#     - AWSLambdaVPCAccessExecutionRole
#
# Outputs:
#   - LambdaSecurityGroupId: the security group ID
#   - LambdaRoleArn: the IAM role ARN
#
# Docs: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-ec2-security-group.html
```

### `templates/app-stack.yaml`

```yaml
# TODO 2 -- Create the app child stack
#
# This template receives parameters from the parent:
#   - ProjectName (String)
#   - SubnetIds (CommaDelimitedList)
#   - SecurityGroupId (String)
#   - LambdaRoleArn (String)
#   - AppZipBucket (String)
#   - AppZipKey (String)
#
# Resources:
#   - Lambda function using provided.al2023 runtime
#     - handler: bootstrap
#     - Code from S3 (AppZipBucket/AppZipKey)
#     - VPC config with SubnetIds and SecurityGroupId
#     - Role: LambdaRoleArn
#
# Outputs:
#   - FunctionName
#   - FunctionArn
```

### `templates/parent-stack.yaml`

```yaml
# TODO 3 -- Create the parent stack
#
# This template orchestrates the three child stacks:
#
# Parameters:
#   - ProjectName (default: cfn-nested-demo)
#   - TemplateBucket (the S3 bucket with child templates)
#   - AppZipKey (the S3 key of the Lambda ZIP)
#
# Resources:
#   VPCStack:
#     Type: AWS::CloudFormation::Stack
#     Properties:
#       TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/vpc-stack.yaml'
#       Parameters:
#         ProjectName: !Ref ProjectName
#
#   SecurityStack:
#     Type: AWS::CloudFormation::Stack
#     Properties:
#       TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/security-stack.yaml'
#       Parameters:
#         ProjectName: !Ref ProjectName
#         VpcId: !GetAtt VPCStack.Outputs.VpcId
#         (child outputs are accessed via !GetAtt StackName.Outputs.OutputKey)
#
#   AppStack:
#     Type: AWS::CloudFormation::Stack
#     Properties:
#       TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/app-stack.yaml'
#       Parameters:
#         ProjectName: !Ref ProjectName
#         SubnetIds: !Join
#           - ','
#           - - !GetAtt VPCStack.Outputs.PrivateSubnetAId
#             - !GetAtt VPCStack.Outputs.PrivateSubnetBId
#         SecurityGroupId: !GetAtt SecurityStack.Outputs.LambdaSecurityGroupId
#         LambdaRoleArn: !GetAtt SecurityStack.Outputs.LambdaRoleArn
#         AppZipBucket: !Ref TemplateBucket
#         AppZipKey: !Ref AppZipKey
#
# Outputs:
#   - VpcId: from VPC stack
#   - FunctionName: from App stack
```

### `app/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, event json.RawMessage) (map[string]string, error) {
	return map[string]string{
		"message":  "Hello from nested stack Lambda!",
		"function": os.Getenv("AWS_LAMBDA_FUNCTION_NAME"),
		"region":   os.Getenv("AWS_REGION"),
	}, nil
}

func main() { lambda.Start(handler) }
```

## Spot the Bug

A developer creates a nested stack but the child template URL points to an S3 bucket without proper access. CloudFormation fails with `Template URL must reference a valid S3 object`:

```yaml
Resources:
  VPCStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: https://my-private-bucket.s3.amazonaws.com/templates/vpc.yaml  # <-- BUG
      Parameters:
        ProjectName: my-app
```

The S3 bucket `my-private-bucket` has a bucket policy that blocks public access and does not grant CloudFormation read access.

<details>
<summary>Explain the bug</summary>

CloudFormation needs to **read** the child template from S3 during stack creation. If the bucket has Block Public Access enabled (default for new buckets) and no bucket policy granting CloudFormation access, CloudFormation cannot download the template.

Unlike Lambda deployment packages (where the Lambda service has its own S3 access), CloudFormation reads templates using the **calling principal's permissions**. The IAM user or role that calls `create-stack` must have `s3:GetObject` on the template objects.

**Three fixes (choose one):**

1. **Bucket policy granting read access** (recommended for most cases):

```json
{
  "Version": "2012-10-17",
  "Statement": [{
    "Effect": "Allow",
    "Principal": {"AWS": "arn:aws:iam::123456789012:root"},
    "Action": "s3:GetObject",
    "Resource": "arn:aws:s3:::my-private-bucket/templates/*"
  }]
}
```

2. **Ensure the calling principal has s3:GetObject** on the template bucket in their IAM policy.

3. **Use `aws cloudformation package`** to upload templates to a bucket where the caller has access:

```bash
aws cloudformation package \
  --template-file parent-stack.yaml \
  --s3-bucket my-accessible-bucket \
  --output-template-file packaged.yaml
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Security child stack (`templates/security-stack.yaml`)</summary>

Parameters: `ProjectName`, `VpcId`. Creates a security group (egress-only) and an IAM role with `AWSLambdaBasicExecutionRole` + `AWSLambdaVPCAccessExecutionRole`. Outputs: `LambdaSecurityGroupId`, `LambdaRoleArn`.

</details>

<details>
<summary>TODO 2 -- App child stack (`templates/app-stack.yaml`)</summary>

Parameters: `ProjectName`, `SubnetIds` (CommaDelimitedList), `SecurityGroupId`, `LambdaRoleArn`, `AppZipBucket`, `AppZipKey`. Creates `AWS::Lambda::Function` with `provided.al2023`, `bootstrap` handler, arm64, VpcConfig, Code from S3. Outputs: `FunctionName`, `FunctionArn`.

</details>

<details>
<summary>TODO 3 -- Parent stack (`templates/parent-stack.yaml`)</summary>

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: Parent stack orchestrating VPC, Security, and App stacks
Parameters:
  ProjectName: { Type: String, Default: cfn-nested-demo }
  TemplateBucket: { Type: String }
  AppZipKey: { Type: String }
Resources:
  VPCStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/vpc-stack.yaml'
      Parameters: { ProjectName: !Ref ProjectName }
  SecurityStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/security-stack.yaml'
      Parameters:
        ProjectName: !Ref ProjectName
        VpcId: !GetAtt VPCStack.Outputs.VpcId
  AppStack:
    Type: AWS::CloudFormation::Stack
    Properties:
      TemplateURL: !Sub 'https://${TemplateBucket}.s3.amazonaws.com/templates/app-stack.yaml'
      Parameters:
        ProjectName: !Ref ProjectName
        SubnetIds: !Join [',', [!GetAtt VPCStack.Outputs.PrivateSubnetAId, !GetAtt VPCStack.Outputs.PrivateSubnetBId]]
        SecurityGroupId: !GetAtt SecurityStack.Outputs.LambdaSecurityGroupId
        LambdaRoleArn: !GetAtt SecurityStack.Outputs.LambdaRoleArn
        AppZipBucket: !Ref TemplateBucket
        AppZipKey: !Ref AppZipKey
Outputs:
  VpcId: { Value: !GetAtt VPCStack.Outputs.VpcId }
  FunctionName: { Value: !GetAtt AppStack.Outputs.FunctionName }
```

</details>

## Verify What You Learned

### Step 1 -- Deploy templates and create stack

```bash
# Deploy Lambda and template bucket
cd app && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init && terraform apply -auto-approve

# Upload templates to S3
BUCKET=$(terraform output -raw template_bucket)
aws s3 cp templates/vpc-stack.yaml s3://$BUCKET/templates/vpc-stack.yaml
aws s3 cp templates/security-stack.yaml s3://$BUCKET/templates/security-stack.yaml
aws s3 cp templates/app-stack.yaml s3://$BUCKET/templates/app-stack.yaml
aws s3 cp templates/parent-stack.yaml s3://$BUCKET/templates/parent-stack.yaml

# Create the parent stack
aws cloudformation create-stack \
  --stack-name cfn-nested-demo \
  --template-url "https://$BUCKET.s3.amazonaws.com/templates/parent-stack.yaml" \
  --parameters \
    ParameterKey=TemplateBucket,ParameterValue=$BUCKET \
    ParameterKey=AppZipKey,ParameterValue=app.zip \
  --capabilities CAPABILITY_NAMED_IAM

aws cloudformation wait stack-create-complete --stack-name cfn-nested-demo
```

### Step 2 -- Verify nested stack hierarchy

```bash
aws cloudformation list-stack-resources --stack-name cfn-nested-demo \
  --query "StackResourceSummaries[].{Type:ResourceType,LogicalId:LogicalResourceId,Status:ResourceStatus}" \
  --output table
```

Expected: three `AWS::CloudFormation::Stack` resources (VPCStack, SecurityStack, AppStack) with status `CREATE_COMPLETE`.

### Step 3 -- Verify outputs cascade

```bash
aws cloudformation describe-stacks --stack-name cfn-nested-demo \
  --query "Stacks[0].Outputs" --output table
```

Expected: VpcId and FunctionName from child stack outputs.

## Cleanup

Delete stacks in reverse order, then destroy Terraform resources:

```bash
aws cloudformation delete-stack --stack-name cfn-nested-demo
aws cloudformation wait stack-delete-complete --stack-name cfn-nested-demo
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

## What's Next

You built a nested stack hierarchy with parameter passing and output propagation. In the next exercise, you will explore **CloudFormation StackSets** -- deploying the same template across multiple AWS regions and accounts.

## Summary

- **Nested stacks** decompose large templates into reusable child templates managed as a single unit
- Child templates are stored in **S3** and referenced via `TemplateURL` in `AWS::CloudFormation::Stack`
- Parameters pass **down** from parent to child; outputs pass **up** via `!GetAtt StackName.Outputs.OutputKey`
- The calling principal needs **s3:GetObject** on the template bucket for CloudFormation to read child templates
- Creating/deleting the parent stack automatically creates/deletes all child stacks
- **Nested stacks** = same lifecycle, deployed together; **Cross-stack references** (Exports) = independent lifecycles, max nesting depth: keep shallow (2-3 levels)
- Use `DependsOn` or implicit references (`!GetAtt`) to control creation order between child stacks

## Reference

- [Nested Stacks](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-nested-stacks.html)
- [AWS::CloudFormation::Stack](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/aws-properties-stack.html)
- [Cross-Stack References](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/walkthrough-crossstackref.html)

## Additional Resources

- [Nested vs Cross-Stack](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-nested-stacks.html) -- when to use each
- [Template Packaging](https://docs.aws.amazon.com/cli/latest/reference/cloudformation/package.html) -- `aws cloudformation package`
- [Stack Update Behavior](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/using-cfn-updating-stacks-direct.html) -- how updates cascade
