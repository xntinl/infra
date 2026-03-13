# 82. CodePipeline Manual Approval Actions

<!--
difficulty: intermediate
concepts: [manual-approval, approval-action, sns-notification, pipeline-stages, approval-timeout, pipeline-execution, approval-url]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: implement, analyze
prerequisites: [13-codepipeline-codebuild-lambda-deploy, 47-sns-topics-subscriptions-filtering]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a CodePipeline with a manual approval stage, SNS topic, and supporting resources. CodePipeline costs $1/month per active pipeline (prorated). Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a CodePipeline manual approval action between build and deploy stages with an SNS notification topic
2. **Configure** the SNS topic and subscription so approval requests reach the appropriate reviewers
3. **Analyze** the approval flow: pipeline pauses, SNS notification sent, reviewer approves/rejects via console or CLI, pipeline continues or fails
4. **Differentiate** between approval action timeout behavior (7-day default) and the impact of stalled approvals on subsequent pipeline executions
5. **Debug** the common failure where no SNS subscription exists, causing approval requests to go unnoticed and the pipeline to stall indefinitely

## Why This Matters

Manual approval gates enforce human oversight before deploying to production. After automated build and test stages pass, the pipeline pauses and notifies a reviewer. The reviewer inspects test results, checks dashboards, and either approves (pipeline continues to deploy) or rejects (pipeline fails and deployment is skipped).

The DVA-C02 exam tests manual approvals in pipeline design questions. Key concepts: the approval action has a configurable **timeout** (default 7 days). If no one approves or rejects within the timeout, the action fails and the pipeline execution stops. The approval action can include an **external entity URL** (pointing to a dashboard, test report, or change request) and **custom data** (a message explaining what to review). The SNS notification includes a direct link to the approval page in the AWS Console.

A critical exam scenario: "The team configured a manual approval action with SNS notification, but no one receives the approval request and the pipeline stalls for 7 days." The root cause is almost always a **missing SNS subscription** -- the topic exists but no one is subscribed to receive notifications. This is the most common manual approval failure.

## Building Blocks

### Terraform Skeleton

Create the following files in your exercise directory:

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
  default     = "approval-pipeline"
}

variable "approval_email" {
  description = "Email address for approval notifications"
  type        = string
  default     = "approver@example.com"
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "source" {
  bucket        = "${var.project_name}-source-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}

resource "aws_s3_bucket_versioning" "source" {
  bucket = aws_s3_bucket.source.id
  versioning_configuration { status = "Enabled" }
}

resource "aws_s3_bucket" "artifacts" {
  bucket        = "${var.project_name}-artifacts-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
}
```

### `cicd.tf`

```hcl
# IAM roles: pipeline (s3:*, codebuild:*, lambda:*, sns:Publish) and CodeBuild (logs:*, s3:*) -- omitted for brevity

resource "aws_codebuild_project" "build" {
  name         = "${var.project_name}-build"
  service_role = aws_iam_role.codebuild.arn

  source {
    type      = "CODEPIPELINE"
    buildspec = <<-EOT
      version: 0.2
      phases:
        build:
          commands:
            - echo "Build completed at $(date)"
            - echo "Ready for approval"
      artifacts:
        files:
          - "**/*"
    EOT
  }

  artifacts { type = "CODEPIPELINE" }

  environment {
    compute_type = "BUILD_GENERAL1_SMALL"
    image        = "aws/codebuild/amazonlinux2-x86_64-standard:5.0"
    type         = "LINUX_CONTAINER"
  }
}

# =======================================================
# TODO 1 -- SNS Topic for Approval Notifications
# =======================================================
# Create an SNS topic and email subscription for approval
# notifications. The pipeline will publish to this topic
# when the approval action is reached.
#
# Requirements:
#   - Create aws_sns_topic
#   - Create aws_sns_topic_subscription with email protocol
#   - The subscriber must confirm the email to receive notifications


# =======================================================
# TODO 2 -- Pipeline with Approval Stage
# =======================================================
# Create the CodePipeline with four stages:
#   1. Source -- S3 source
#   2. Build -- CodeBuild
#   3. Approval -- Manual approval action with:
#      - NotificationArn: SNS topic ARN
#      - CustomData: "Please review the build artifacts and
#        approve for production deployment."
#      - ExternalEntityLink: a URL to a dashboard or test
#        results (use a placeholder URL)
#   4. Deploy -- Placeholder deploy action
#
# Manual approval action configuration:
#   category  = "Approval"
#   owner     = "AWS"
#   provider  = "Manual"
#   version   = "1"
#   configuration = {
#     NotificationArn    = <topic-arn>
#     CustomData         = <message>
#     ExternalEntityLink = <url>
#   }
```

### `outputs.tf`

```hcl
output "pipeline_name"     { value = "TODO" }
output "approval_topic"    { value = "TODO" }
output "source_bucket"     { value = aws_s3_bucket.source.bucket }
```

## Spot the Bug

A developer configures a pipeline with a manual approval action and an SNS topic. The pipeline reaches the approval stage but no one receives the notification email. The approval times out after 7 days and the pipeline fails. **What is wrong?**

```hcl
resource "aws_sns_topic" "approval" {
  name = "pipeline-approval"
}

# No subscription created!

resource "aws_codepipeline" "this" {
  # ...
  stage {
    name = "Approval"
    action {
      name     = "ManualApproval"
      category = "Approval"
      owner    = "AWS"
      provider = "Manual"
      version  = "1"
      configuration = {
        NotificationArn = aws_sns_topic.approval.arn
        CustomData      = "Please approve for production"
      }
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

The SNS topic exists but has **no subscriptions**. When CodePipeline publishes the approval notification to the topic, SNS has no subscribers to deliver it to. The message is published successfully (CodePipeline does not check for subscribers), but nobody receives it.

The fix: add an SNS subscription:

```hcl
resource "aws_sns_topic_subscription" "approval_email" {
  topic_arn = aws_sns_topic.approval.arn
  protocol  = "email"
  endpoint  = "team-lead@example.com"
}
```

There is a second subtlety: email subscriptions require **manual confirmation**. After `terraform apply`, the subscriber receives a confirmation email and must click the confirmation link. Until confirmed, the subscription status is `PendingConfirmation` and no messages are delivered.

On the exam, this two-part failure (missing subscription + unconfirmed subscription) is the canonical manual approval debugging scenario. The pipeline stalls because:
1. No subscription exists (no one is subscribed), OR
2. The subscription exists but is not confirmed (pending confirmation)

Both cases result in the approval notification being published but never received, causing the pipeline to stall for the full timeout period (default 7 days).

</details>

## Solutions

<details>
<summary>cicd.tf -- TODO 1 -- SNS Topic and Subscription</summary>

```hcl
resource "aws_sns_topic" "approval" {
  name = "${var.project_name}-approval"
}

resource "aws_sns_topic_subscription" "approval_email" {
  topic_arn = aws_sns_topic.approval.arn
  protocol  = "email"
  endpoint  = var.approval_email
}
```

After applying, check the subscriber's email inbox and click the confirmation link. Verify:

```bash
aws sns list-subscriptions-by-topic --topic-arn $(terraform output -raw approval_topic) \
  --query "Subscriptions[*].{Protocol:Protocol,Endpoint:Endpoint,Status:SubscriptionArn}" --output table
```

If the SubscriptionArn shows `PendingConfirmation`, the email has not been confirmed yet.

</details>

<details>
<summary>cicd.tf -- TODO 2 -- Pipeline with Approval Stage</summary>

```hcl
resource "aws_codepipeline" "this" {
  name     = var.project_name
  role_arn = aws_iam_role.pipeline.arn

  artifact_store {
    location = aws_s3_bucket.artifacts.bucket
    type     = "S3"
  }

  stage {
    name = "Source"
    action {
      name             = "S3Source"
      category         = "Source"
      owner            = "AWS"
      provider         = "S3"
      version          = "1"
      output_artifacts = ["SourceOutput"]
      configuration = {
        S3Bucket             = aws_s3_bucket.source.bucket
        S3ObjectKey          = "source.zip"
        PollForSourceChanges = "true"
      }
    }
  }

  stage {
    name = "Build"
    action {
      name             = "Build"
      category         = "Build"
      owner            = "AWS"
      provider         = "CodeBuild"
      version          = "1"
      input_artifacts  = ["SourceOutput"]
      output_artifacts = ["BuildOutput"]
      configuration = {
        ProjectName = aws_codebuild_project.build.name
      }
    }
  }

  stage {
    name = "Approval"
    action {
      name     = "ManualApproval"
      category = "Approval"
      owner    = "AWS"
      provider = "Manual"
      version  = "1"
      configuration = {
        NotificationArn    = aws_sns_topic.approval.arn
        CustomData         = "Please review the build artifacts and approve for production deployment."
        ExternalEntityLink = "https://console.aws.amazon.com/cloudwatch/home"
      }
    }
  }

  stage {
    name = "Deploy"
    action {
      name            = "DeployPlaceholder"
      category        = "Deploy"
      owner           = "AWS"
      provider        = "S3"
      version         = "1"
      input_artifacts = ["BuildOutput"]
      configuration = {
        BucketName = aws_s3_bucket.artifacts.bucket
        Extract    = "true"
      }
    }
  }
}
```

Update outputs:
```hcl
output "pipeline_name"  { value = aws_codepipeline.this.name }
output "approval_topic" { value = aws_sns_topic.approval.arn }
```

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
terraform init && terraform apply -auto-approve
```

### Step 2 -- Verify pipeline structure

```bash
PIPELINE_NAME=$(terraform output -raw pipeline_name)

aws codepipeline get-pipeline --name "$PIPELINE_NAME" \
  --query "pipeline.stages[*].{Stage:name,Actions:actions[*].{Name:name,Category:category}}" --output json | jq .
```

Expected: four stages (Source, Build, Approval, Deploy) with the Approval stage having category "Approval".

### Step 3 -- Approve via CLI (when pipeline reaches approval)

```bash
# List pending approvals
aws codepipeline get-pipeline-state --name "$PIPELINE_NAME" \
  --query "stageStates[?stageName=='Approval'].actionStates[*].latestExecution" --output json

# Approve (replace token with actual value from the approval notification)
# aws codepipeline put-approval-result \
#   --pipeline-name "$PIPELINE_NAME" \
#   --stage-name Approval \
#   --action-name ManualApproval \
#   --result "summary=Looks good,status=Approved" \
#   --token "<approval-token>"
```

### Step 4 -- Verify no drift

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You added manual approval gates to a CodePipeline. In the next exercise, you will explore **CodeArtifact package management** -- creating repositories, configuring Go module proxies, and managing upstream connections.

## Summary

- **Manual approval actions** pause the pipeline and notify reviewers via SNS -- the pipeline waits for an Approve or Reject response
- The approval action has a **configurable timeout** (default 7 days) -- if no response within the timeout, the action fails
- Approval configuration includes: **NotificationArn** (SNS topic), **CustomData** (review instructions), and **ExternalEntityLink** (URL to test results or dashboard)
- Reviewers can approve or reject via the AWS Console or `aws codepipeline put-approval-result` CLI command
- The approval token is included in the SNS notification and is required for CLI-based approval
- Common failure: **SNS topic has no subscriptions** or subscriptions are **PendingConfirmation** -- notifications are published but never received
- Email SNS subscriptions require the subscriber to **click a confirmation link** before messages are delivered
- Pipeline IAM role needs **sns:Publish** permission on the approval topic
- Subsequent pipeline executions **queue** while the current execution is waiting for approval -- they do not supersede the waiting execution

## Reference

- [CodePipeline Manual Approval Actions](https://docs.aws.amazon.com/codepipeline/latest/userguide/approvals.html)
- [Terraform aws_codepipeline](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codepipeline)
- [PutApprovalResult CLI](https://docs.aws.amazon.com/cli/latest/reference/codepipeline/put-approval-result.html)
- [SNS Email Subscriptions](https://docs.aws.amazon.com/sns/latest/dg/sns-email-notifications.html)

## Additional Resources

- [Lambda Approval Automation](https://docs.aws.amazon.com/codepipeline/latest/userguide/approvals-approve-or-reject.html) -- using Lambda to auto-approve based on conditions
- [CodePipeline Execution Modes](https://docs.aws.amazon.com/codepipeline/latest/userguide/concepts-how-it-works.html) -- how queued executions interact with approval stages
- [SNS Subscription Confirmation](https://docs.aws.amazon.com/sns/latest/dg/sns-http-https-endpoint-as-subscriber.html) -- understanding the confirmation workflow
