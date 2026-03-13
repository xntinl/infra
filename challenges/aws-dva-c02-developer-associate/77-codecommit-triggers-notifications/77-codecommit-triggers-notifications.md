# 77. CodeCommit Triggers and Notifications

<!--
difficulty: basic
concepts: [codecommit-repository, codecommit-triggers, sns-trigger, lambda-trigger, notification-rules, pull-request-events, branch-events]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: apply
prerequisites: [47-sns-topics-subscriptions-filtering]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a CodeCommit repository, SNS topic, and a Lambda function. CodeCommit is free for the first 5 active users per month. SNS and Lambda costs are negligible for testing. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally (for compiling the Lambda binary)
- git CLI installed (`git --version`)

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CodeCommit repository with Terraform and clone it using HTTPS GRC credentials or the AWS CLI credential helper
- **Configure** CodeCommit triggers that fire SNS notifications on push events and invoke Lambda functions on branch creation
- **Implement** a Lambda function in Go that processes CodeCommit trigger events and logs commit details
- **Differentiate** between CodeCommit triggers (limited to push/branch events) and notification rules (PR events, comment events, approval events)
- **Verify** end-to-end event flow from a git push through SNS delivery and Lambda invocation

## Why CodeCommit Triggers and Notifications

CodeCommit triggers let you react to repository events without polling. When a developer pushes code or creates a branch, CodeCommit fires a trigger that can publish to SNS or invoke a Lambda function. This enables automated workflows: sending Slack notifications on push, running static analysis on new branches, or updating a dashboard.

The DVA-C02 exam distinguishes two event mechanisms. **Triggers** fire only on push events and branch/tag creation or deletion. **Notification rules** (CodeStar Notifications) cover broader events: pull request creation, updates, merges, comments, and approval state changes. A common exam question: "PR comments should trigger a notification" -- the answer is notification rules, not triggers.

## Step 1 -- Create the Lambda Trigger Handler

### `trigger/main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/aws/aws-lambda-go/lambda"
)

type CodeCommitEvent struct {
	Records []struct {
		AwsRegion        string `json:"awsRegion"`
		EventTriggerName string `json:"eventTriggerName"`
		EventSourceARN   string `json:"eventSourceARN"`
		UserIdentityARN  string `json:"userIdentityARN"`
		CodeCommit       struct {
			References []struct {
				Commit  string `json:"commit"`
				Ref     string `json:"ref"`
				Created bool   `json:"created"`
			} `json:"references"`
		} `json:"codecommit"`
	} `json:"Records"`
}

func handler(ctx context.Context, event CodeCommitEvent) error {
	for _, record := range event.Records {
		fmt.Printf("Trigger: %s Repository: %s\n", record.EventTriggerName, record.EventSourceARN)
		for _, ref := range record.CodeCommit.References {
			fmt.Printf("  Branch: %s Commit: %s Created: %v\n", ref.Ref, ref.Commit, ref.Created)
		}
		raw, _ := json.MarshalIndent(record, "", "  ")
		fmt.Printf("Full event:\n%s\n", string(raw))
	}
	return nil
}

func main() {
	lambda.Start(handler)
}
```

## Step 2 -- Create the Terraform Configuration

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
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
  default     = "codecommit-triggers-demo"
}
```

### `cicd.tf`

```hcl
# CodeCommit Repository
resource "aws_codecommit_repository" "this" {
  repository_name = "${var.project_name}-repo"
  description     = "Demo repository for triggers and notifications"
}

# CodeCommit Triggers
resource "aws_codecommit_trigger" "sns_on_push" {
  repository_name = aws_codecommit_repository.this.repository_name

  trigger {
    name            = "push-to-sns"
    events          = ["all"]
    destination_arn = aws_sns_topic.push_notifications.arn
  }

  trigger {
    name            = "branch-to-lambda"
    events          = ["createReference"]
    destination_arn = aws_lambda_function.trigger_handler.arn
    branches        = []
  }
}

# Notification Rule -- PR events via CodeStar Notifications
resource "aws_codestarnotifications_notification_rule" "pr_events" {
  name        = "${var.project_name}-pr-rule"
  resource    = aws_codecommit_repository.this.arn
  detail_type = "FULL"

  event_type_ids = [
    "codecommit-repository-pull-request-created",
    "codecommit-repository-pull-request-merged",
    "codecommit-repository-pull-request-source-updated",
  ]

  target {
    address = aws_sns_topic.pr_notifications.arn
    type    = "SNS"
  }
}
```

### `events.tf`

```hcl
# SNS Topic for Push Notifications
resource "aws_sns_topic" "push_notifications" {
  name = "${var.project_name}-push-events"
}

# SNS topic policy -- allows CodeCommit to publish
data "aws_iam_policy_document" "sns_policy" {
  statement {
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.push_notifications.arn]
    principals { type = "Service"; identifiers = ["codecommit.amazonaws.com"] }
  }
}

resource "aws_sns_topic_policy" "push_notifications" {
  arn    = aws_sns_topic.push_notifications.arn
  policy = data.aws_iam_policy_document.sns_policy.json
}

# SNS Topic for PR Notifications
resource "aws_sns_topic" "pr_notifications" {
  name = "${var.project_name}-pr-events"
}

data "aws_iam_policy_document" "pr_sns_policy" {
  statement {
    actions   = ["sns:Publish"]
    resources = [aws_sns_topic.pr_notifications.arn]
    principals { type = "Service"; identifiers = ["codestar-notifications.amazonaws.com"] }
  }
}

resource "aws_sns_topic_policy" "pr_notifications" {
  arn    = aws_sns_topic.pr_notifications.arn
  policy = data.aws_iam_policy_document.pr_sns_policy.json
}
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = {
    source_hash = filebase64sha256("${path.module}/trigger/main.go")
  }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = "${path.module}/trigger"
  }
}

data "archive_file" "trigger_handler" {
  type        = "zip"
  source_file = "${path.module}/trigger/bootstrap"
  output_path = "${path.module}/build/trigger.zip"
  depends_on  = [null_resource.go_build]
}
```

### `lambda.tf`

```hcl
# Lambda IAM role (AWSLambdaBasicExecutionRole) + log group -- standard pattern

resource "aws_lambda_function" "trigger_handler" {
  function_name    = "${var.project_name}-trigger"
  filename         = data.archive_file.trigger_handler.output_path
  source_code_hash = data.archive_file.trigger_handler.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.trigger_handler.arn
  timeout          = 10

  depends_on = [aws_iam_role_policy_attachment.trigger_basic, aws_cloudwatch_log_group.trigger_handler]
}

# Lambda permission -- allows CodeCommit to invoke the function
resource "aws_lambda_permission" "codecommit_invoke" {
  statement_id  = "AllowCodeCommitInvoke"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.trigger_handler.function_name
  principal     = "codecommit.amazonaws.com"
  source_arn    = aws_codecommit_repository.this.arn
}
```

### `outputs.tf`

```hcl
output "repository_clone_url" {
  description = "HTTPS clone URL for the CodeCommit repository"
  value       = aws_codecommit_repository.this.clone_url_http
}

output "repository_name" {
  description = "CodeCommit repository name"
  value       = aws_codecommit_repository.this.repository_name
}

output "function_name" {
  description = "Lambda trigger handler function name"
  value       = aws_lambda_function.trigger_handler.function_name
}
```

## Step 3 -- Build and Apply

```bash
cd trigger && GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go && cd ..
terraform init
terraform apply -auto-approve
```

## Step 4 -- Clone the Repository and Push a Commit

Configure the AWS CLI credential helper for CodeCommit and clone the repo:

```bash
REPO_URL=$(terraform output -raw repository_clone_url)
REPO_NAME=$(terraform output -raw repository_name)

git clone "$REPO_URL" /tmp/$REPO_NAME
cd /tmp/$REPO_NAME

echo "# Demo repo" > README.md
git add README.md
git commit -m "Initial commit"
git push origin main
```

## Step 5 -- Verify Trigger Fired

Create a new branch to fire the Lambda trigger and check logs:

```bash
cd /tmp/$REPO_NAME
git checkout -b feature/test-trigger
echo "test" > test.txt && git add test.txt && git commit -m "Test branch trigger"
git push origin feature/test-trigger

sleep 5
FUNCTION_NAME=$(terraform output -raw function_name)
aws logs filter-log-events \
  --log-group-name "/aws/lambda/$FUNCTION_NAME" \
  --filter-pattern "Trigger" \
  --query "events[*].message" --output text
```

## Common Mistakes

### 1. Missing SNS topic policy for CodeCommit

Without a topic policy granting `sns:Publish` to `codecommit.amazonaws.com`, triggers fail silently. The trigger appears configured, but no messages reach the topic. Fix: add `aws_sns_topic_policy` granting publish permission to the CodeCommit service principal.

### 2. Confusing triggers with notification rules

Triggers only fire on push events (commits, branch/tag creation and deletion). If you need notifications for pull request events, comments, or approvals, use `aws_codestarnotifications_notification_rule`. The notification rule service principal is `codestar-notifications.amazonaws.com`, not `codecommit.amazonaws.com`.

### 3. Missing Lambda permission for CodeCommit

Without `aws_lambda_permission` allowing `codecommit.amazonaws.com`, the trigger cannot invoke the Lambda function. CodeCommit gets AccessDenied and the trigger fails silently.

## Verify What You Learned

```bash
# Verify repository exists
aws codecommit get-repository --repository-name $(terraform output -raw repository_name) \
  --query "repositoryMetadata.repositoryName" --output text
```

Expected: the repository name.

```bash
# Verify triggers are configured
aws codecommit get-repository-triggers --repository-name $(terraform output -raw repository_name) \
  --query "triggers[*].{Name:name,Events:events,Destination:destinationArn}" --output table
```

Expected: two triggers -- one SNS (all events) and one Lambda (createReference).

```bash
terraform plan
```

Expected: `No changes.`

## Cleanup

```bash
rm -rf /tmp/$(terraform output -raw repository_name)
terraform destroy -auto-approve
terraform state list  # Expected: empty
```

## What's Next

You built a CodeCommit repository with triggers and notification rules. Next, you will configure **CodeBuild environment variables and secrets** -- using plaintext, Parameter Store, and Secrets Manager references in buildspec files.

## Summary

- **CodeCommit triggers** fire on push events (commits, branch/tag create/delete) and can target SNS topics or Lambda functions
- **Notification rules** (CodeStar Notifications) cover broader events: PR creation, updates, merges, comments, and approval state changes -- they target SNS topics only
- Triggers require an **SNS topic policy** granting `codecommit.amazonaws.com` publish permission, or an **aws_lambda_permission** for Lambda targets
- Notification rules require an **SNS topic policy** granting `codestar-notifications.amazonaws.com` publish permission
- Triggers and notification rules fail **silently** when permissions are missing -- the trigger appears configured but no events are delivered
- The `branches` filter on triggers limits which branches fire the trigger -- an empty list means all branches
- Trigger events include: `all`, `updateReference`, `createReference`, `deleteReference`

## Reference

- [CodeCommit Triggers](https://docs.aws.amazon.com/codecommit/latest/userguide/how-to-notify.html)
- [Terraform aws_codecommit_trigger](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codecommit_trigger)
- [Terraform aws_codestarnotifications_notification_rule](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codestarnotifications_notification_rule)
- [CodeCommit Notification Rule Event Types](https://docs.aws.amazon.com/dtconsole/latest/userguide/concepts.html#concepts-api)

## Additional Resources

- [CodeCommit Repository Events](https://docs.aws.amazon.com/codecommit/latest/userguide/monitoring-events.html) -- EventBridge events for CodeCommit (an alternative to triggers)
- [Setting Up Git Credential Helper](https://docs.aws.amazon.com/codecommit/latest/userguide/setting-up-https-unixes.html) -- configuring HTTPS access to CodeCommit
- [CodeStar Notifications Concepts](https://docs.aws.amazon.com/dtconsole/latest/userguide/concepts.html) -- understanding notification rules, targets, and event types
