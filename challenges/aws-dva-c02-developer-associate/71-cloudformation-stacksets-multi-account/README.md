# 71. CloudFormation StackSets for Multi-Account Deployment

<!--
difficulty: intermediate
concepts: [stacksets, stack-instances, self-managed-permissions, service-managed-permissions, deployment-targets, operation-preferences, admin-role, execution-role]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, differentiate
prerequisites: [11-cloudformation-intrinsic-functions-rollback]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates IAM roles for StackSet administration and a StackSet with stack instances. Cost is approximately $0.01/hr for the deployed resources. Remember to delete stack instances and the StackSet when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a StackSet that deploys a CloudFormation template across multiple regions within a single account
2. **Configure** self-managed permissions with an admin role and execution role for StackSet operations
3. **Differentiate** between self-managed permissions (explicit IAM roles) and service-managed permissions (AWS Organizations integration)
4. **Apply** operation preferences to control deployment parallelism and failure tolerance
5. **Analyze** the relationship between StackSets, stack instances, and the underlying CloudFormation stacks

## Why StackSets

StackSets extend CloudFormation to deploy the same template across multiple AWS accounts and regions with a single operation. Instead of manually running `create-stack` in each account and region, you create a StackSet once and specify the target accounts and regions. CloudFormation manages the stack instances for you.

Two permission models:

1. **Self-managed**: you create an admin role in the management account and an execution role in each target account. The admin role assumes the execution role to deploy stacks. Use this when you do not use AWS Organizations or need fine-grained control over which accounts participate.

2. **Service-managed**: AWS Organizations automatically manages the roles. You specify an OU (organizational unit) as the target, and CloudFormation deploys to all accounts in that OU. New accounts added to the OU automatically get stack instances. Use this when you use Organizations and want automatic deployment.

The DVA-C02 exam tests the permission model differences and common failure scenarios. Key trap: the admin role needs `sts:AssumeRole` permission on the execution role in target accounts. If this permission is missing, the StackSet operation fails with `FAILED` status and a "role not found" error.

## Building Blocks

This exercise uses a single account with multi-region deployment (the simplest StackSet scenario). Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `stackset-template.yaml`

The CloudFormation template that will be deployed to multiple regions.

```yaml
AWSTemplateFormatVersion: '2010-09-09'
Description: Multi-region SNS topic for alerting

Parameters:
  ProjectName:
    Type: String
    Default: stackset-demo
  Environment:
    Type: String
    Default: dev
    AllowedValues: [dev, staging, prod]

Resources:
  AlertTopic:
    Type: AWS::SNS::Topic
    Properties:
      TopicName: !Sub '${ProjectName}-alerts-${Environment}'
      Tags:
        - Key: Project
          Value: !Ref ProjectName
        - Key: Environment
          Value: !Ref Environment
        - Key: ManagedBy
          Value: StackSet

  AlertTopicPolicy:
    Type: AWS::SNS::TopicPolicy
    Properties:
      Topics:
        - !Ref AlertTopic
      PolicyDocument:
        Version: '2012-10-17'
        Statement:
          - Effect: Allow
            Principal:
              Service: cloudwatch.amazonaws.com
            Action: sns:Publish
            Resource: !Ref AlertTopic

Outputs:
  TopicArn:
    Value: !Ref AlertTopic
    Description: SNS topic ARN for alerts
  TopicName:
    Value: !GetAtt AlertTopic.TopicName
  Region:
    Value: !Ref 'AWS::Region'
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
  default     = "stackset-demo"
}
```

### `iam.tf`

```hcl
data "aws_caller_identity" "current" {}

# =======================================================
# TODO 1 -- StackSet Admin Role
# =======================================================
# Create an IAM role that CloudFormation StackSets uses
# to manage stack instances. For self-managed permissions:
#
# Requirements:
#   - Role name: "AWSCloudFormationStackSetAdministrationRole"
#     (this is the conventional name StackSets looks for)
#   - Trust policy: allow cloudformation.amazonaws.com
#     to assume this role
#   - Inline policy: allow sts:AssumeRole on the execution
#     role in target accounts
#
# Docs: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-prereqs-self-managed.html


# =======================================================
# TODO 2 -- StackSet Execution Role
# =======================================================
# Create the execution role in the target account.
# For single-account multi-region, this is the same account.
#
# Requirements:
#   - Role name: "AWSCloudFormationStackSetExecutionRole"
#     (conventional name)
#   - Trust policy: allow the admin role to assume it
#   - Permissions: CloudFormation needs broad permissions
#     to create the template's resources (SNS, IAM, etc.)
#   - For this exercise, attach AdministratorAccess
#     (in production, scope this down)
#
# Docs: https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-prereqs-self-managed.html
```

### `cloudformation.tf`

```hcl
# =======================================================
# TODO 3 -- CloudFormation StackSet
# =======================================================
# Create the StackSet using Terraform. Note: Terraform
# can create the StackSet, but stack instances are typically
# managed via AWS CLI for flexibility.
#
# Requirements:
#   - Use aws_cloudformation_stack_set resource
#   - name = var.project_name
#   - template_body = file("stackset-template.yaml")
#   - permission_model = "SELF_MANAGED"
#   - administration_role_arn = admin role ARN
#   - execution_role_name = execution role name
#   - parameters: ProjectName = var.project_name
#   - operation_preferences:
#     - max_concurrent_count = 1 (deploy one region at a time)
#     - failure_tolerance_count = 0 (stop on first failure)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack_set


# =======================================================
# TODO 4 -- Stack Instances (multi-region)
# =======================================================
# Create stack instances in us-east-1 and us-west-2.
#
# Requirements:
#   - Use aws_cloudformation_stack_set_instance resource
#   - One instance for each target region
#   - account_id = data.aws_caller_identity.current.account_id
#   - region = target region
#
# Alternatively, use AWS CLI:
#   aws cloudformation create-stack-instances \
#     --stack-set-name stackset-demo \
#     --accounts $ACCOUNT_ID \
#     --regions us-east-1 us-west-2
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack_set_instance
```

### `outputs.tf`

```hcl
output "stackset_name" { value = aws_cloudformation_stack_set.this.name }
```

## Spot the Bug

A developer creates a StackSet but the admin role does not have `sts:AssumeRole` permission on the execution role in the target account. Stack instance creation fails:

```hcl
resource "aws_iam_role" "stackset_admin" {
  name = "AWSCloudFormationStackSetAdministrationRole"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudformation.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

# Missing: inline policy with sts:AssumeRole permission  # <-- BUG
# The admin role has NO permissions to assume the execution role
```

<details>
<summary>Explain the bug</summary>

The admin role has a trust policy allowing CloudFormation to assume it, but it has **no permissions** to do anything once assumed. Specifically, it is missing the `sts:AssumeRole` permission that allows it to assume the execution role in target accounts.

When StackSets tries to create a stack instance, the flow is:
1. CloudFormation assumes the admin role
2. The admin role assumes the execution role in the target account
3. The execution role creates the resources

Without `sts:AssumeRole` permission in step 2, the operation fails with `FAILED` status and error: `Account gate check failed. The execution role is not available in account XXXX`.

**Fix -- add an inline policy with sts:AssumeRole:**

```hcl
resource "aws_iam_role" "stackset_admin" {
  name = "AWSCloudFormationStackSetAdministrationRole"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudformation.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "stackset_admin" {
  name = "AssumeExecutionRole"
  role = aws_iam_role.stackset_admin.id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sts:AssumeRole"
      Resource = "arn:aws:iam::*:role/AWSCloudFormationStackSetExecutionRole"
    }]
  })
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- StackSet Admin Role (`iam.tf`)</summary>

```hcl
resource "aws_iam_role" "stackset_admin" {
  name = "AWSCloudFormationStackSetAdministrationRole"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "cloudformation.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy" "stackset_admin" {
  name = "AssumeExecutionRole"
  role = aws_iam_role.stackset_admin.id

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "sts:AssumeRole"
      Resource = "arn:aws:iam::*:role/AWSCloudFormationStackSetExecutionRole"
    }]
  })
}
```

</details>

<details>
<summary>TODO 2 -- StackSet Execution Role (`iam.tf`)</summary>

```hcl
resource "aws_iam_role" "stackset_execution" {
  name = "AWSCloudFormationStackSetExecutionRole"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = aws_iam_role.stackset_admin.arn }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "stackset_execution" {
  role       = aws_iam_role.stackset_execution.name
  policy_arn = "arn:aws:iam::aws:policy/AdministratorAccess"
}
```

</details>

<details>
<summary>TODO 3 -- CloudFormation StackSet (`cloudformation.tf`)</summary>

```hcl
resource "aws_cloudformation_stack_set" "this" {
  name             = var.project_name
  permission_model = "SELF_MANAGED"

  administration_role_arn = aws_iam_role.stackset_admin.arn
  execution_role_name     = aws_iam_role.stackset_execution.name

  template_body = file("${path.module}/stackset-template.yaml")

  parameters = {
    ProjectName = var.project_name
  }

  operation_preferences {
    max_concurrent_count    = 1
    failure_tolerance_count = 0
  }

  depends_on = [
    aws_iam_role_policy.stackset_admin,
    aws_iam_role_policy_attachment.stackset_execution,
  ]
}
```

</details>

<details>
<summary>TODO 4 -- Stack Instances (`cloudformation.tf`)</summary>

```hcl
resource "aws_cloudformation_stack_set_instance" "us_east_1" {
  stack_set_name = aws_cloudformation_stack_set.this.name
  account_id = data.aws_caller_identity.current.account_id
  region     = "us-east-1"
}

resource "aws_cloudformation_stack_set_instance" "us_west_2" {
  stack_set_name = aws_cloudformation_stack_set.this.name
  account_id = data.aws_caller_identity.current.account_id
  region     = "us-west-2"
}
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify StackSet and instances

```bash
aws cloudformation list-stack-instances --stack-set-name stackset-demo \
  --query "Summaries[].{Account:Account,Region:Region,Status:Status}" --output table
```

Expected: two instances (us-east-1 and us-west-2) with status `CURRENT`.

### Step 3 -- Verify SNS topics in both regions

```bash
aws sns list-topics --region us-east-1 \
  --query "Topics[?contains(TopicArn, 'stackset-demo')]" --output text

aws sns list-topics --region us-west-2 \
  --query "Topics[?contains(TopicArn, 'stackset-demo')]" --output text
```

Expected: SNS topic ARNs in both regions.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Delete stack instances first, then the StackSet:

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws cloudformation list-stack-sets \
  --query "Summaries[?StackSetName=='stackset-demo']" --output text
terraform state list
```

Expected: no StackSet found and empty Terraform state.

## What's Next

You deployed a StackSet across multiple regions with self-managed permissions. In the next exercise, you will explore **CloudFormation helper scripts** -- using cfn-init, cfn-signal, and cfn-hup to configure EC2 instances during stack creation.

## Summary

- **StackSets** deploy the same template across multiple accounts and regions with a single operation
- **Self-managed permissions**: you create admin + execution roles explicitly; admin assumes execution role via STS
- **Service-managed permissions**: Organizations handles roles automatically; targets are OUs, not individual accounts
- The admin role needs `sts:AssumeRole` on the execution role -- missing this is the most common StackSet failure
- **Stack instances** are the per-account-per-region deployments managed by the StackSet
- **Operation preferences** control parallelism (`max_concurrent_count`) and failure tolerance (`failure_tolerance_count`)
- Updating the StackSet template automatically updates all stack instances
- StackSets use **eventual consistency** -- operations may take time to propagate to all instances

## Reference

- [CloudFormation StackSets](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/what-is-cfnstacksets.html)
- [Self-Managed Permissions](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-prereqs-self-managed.html)
- [Service-Managed Permissions](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-prereqs-service-managed.html)
- [Terraform aws_cloudformation_stack_set](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cloudformation_stack_set)

## Additional Resources

- [StackSet Operations](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-update.html) -- creating, updating, and deleting stack instances
- [StackSet Drift Detection](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-drift.html) -- detecting configuration changes
- [Automatic Deployments](https://docs.aws.amazon.com/AWSCloudFormation/latest/UserGuide/stacksets-orgs-associate-stackset-with-org.html) -- auto-deploy to new accounts in an OU
