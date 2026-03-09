# 92. Systems Manager Session Manager and Patch Manager

<!--
difficulty: intermediate
concepts: [systems-manager, session-manager, patch-manager, patch-baseline, maintenance-window, ssm-agent, managed-instance, run-command, fleet-manager, no-ssh, audit-trail]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [91-config-rules-remediation]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** EC2 t3.micro instance (~$0.0104/hr) for testing Session Manager and Patch Manager. SSM itself is free (Session Manager, Run Command, Patch Manager, Fleet Manager). Total ~$0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 91 (AWS Config) or equivalent knowledge
- Understanding of EC2 instances and IAM instance profiles
- Familiarity with SSH concepts (Session Manager replaces SSH)

## Learning Objectives

After completing this exercise, you will be able to:

1. **Implement** Systems Manager Session Manager as a secure SSH replacement with full audit logging and no inbound port 22
2. **Analyze** why Session Manager provides better security than SSH (no key management, no bastion hosts, CloudTrail audit trail)
3. **Apply** Patch Manager baselines and maintenance windows to automate OS and application patching
4. **Evaluate** the prerequisites for SSM-managed instances (SSM Agent, IAM role, network connectivity)
5. **Design** a fleet management strategy using SSM that eliminates SSH keys, bastion hosts, and VPN dependencies

## Why This Matters

Systems Manager is one of the most frequently tested services on the SAA-C03 exam because it touches security, operations, and cost optimization simultaneously. Session Manager eliminates the need for SSH keys, bastion hosts, and inbound security group rules on port 22. Instead of managing SSH key pairs across hundreds of instances, rotating them, and auditing who has access, Session Manager uses IAM policies for authentication and CloudTrail for audit logging. Every session is recorded -- who connected, what commands they ran, and when they disconnected.

The exam tests a specific architectural pattern: "How do you provide secure shell access to EC2 instances in private subnets without a bastion host?" The answer is Session Manager with VPC endpoints. No internet access required, no inbound security group rules, no SSH key management. This is cheaper (no bastion instance cost), more secure (no exposed ports), and more auditable (CloudTrail + S3 session logs) than traditional SSH.

Patch Manager automates the most dreaded operational task: OS patching. Without automation, patching requires manual SSH sessions to each instance, running update commands, verifying success, and rebooting if necessary. Patch Manager defines patch baselines (which patches to apply, how quickly after release), maintenance windows (when patching is allowed), and automatically applies patches across your fleet. The exam tests your understanding of patch baselines, approval rules, and the maintenance window scheduling model.

## Step 1 -- Create the EC2 Instance with SSM Access

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
  }
}

provider "aws" { region = var.region }
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
  default     = "saa-ex92"
}
```

### `main.tf`

```hcl
data "aws_vpc" "default" { default = true }

data "aws_subnets" "default" {
  filter { name = "vpc-id", values = [data.aws_vpc.default.id] }
  filter { name = "default-for-az", values = ["true"] }
}

# Amazon Linux 2023 comes with SSM Agent pre-installed and running.
# Without the SSM Agent, the instance will not appear in Fleet Manager.
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter { name = "name", values = ["al2023-ami-2023*-x86_64"] }
  filter { name = "virtualization-type", values = ["hvm"] }
}

# NO inbound rules -- Session Manager uses outbound HTTPS (443) only.
# No SSH port 22. No bastion host.
resource "aws_security_group" "instance" {
  name   = "${var.project_name}-instance-sg"
  vpc_id = data.aws_vpc.default.id
  egress { from_port = 443, to_port = 443, protocol = "tcp", cidr_blocks = ["0.0.0.0/0"] }
  egress { from_port = 80, to_port = 80, protocol = "tcp", cidr_blocks = ["0.0.0.0/0"] }
  tags = { Name = "${var.project_name}-instance-sg" }
}

resource "aws_instance" "this" {
  ami                    = data.aws_ami.al2023.id
  instance_type          = "t3.micro"
  subnet_id              = data.aws_subnets.default.ids[0]
  iam_instance_profile   = aws_iam_instance_profile.ssm.name
  vpc_security_group_ids = [aws_security_group.instance.id]
  metadata_options { http_tokens = "required" }
  tags = { Name = "${var.project_name}-ssm-target", PatchGroup = "production", Environment = "exercise" }
}
```

### `iam.tf`

```hcl
# AmazonSSMManagedInstanceCore provides minimum permissions for
# Session Manager, Run Command, and Patch Manager.
data "aws_iam_policy_document" "ec2_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["ec2.amazonaws.com"] }
  }
}

resource "aws_iam_role" "ssm" {
  name               = "${var.project_name}-ssm-role"
  assume_role_policy = data.aws_iam_policy_document.ec2_assume.json
  tags = { Name = "${var.project_name}-ssm-role" }
}

resource "aws_iam_role_policy_attachment" "ssm_core" {
  role       = aws_iam_role.ssm.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ssm" {
  name = "${var.project_name}-ssm-profile"
  role = aws_iam_role.ssm.name
}
```

### `outputs.tf`

```hcl
output "instance_id" { value = aws_instance.this.id }
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Configure Session Manager

### TODO 1: Create Session Manager Preferences

Add the following to `ssm.tf`:

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

resource "aws_s3_bucket" "session_logs" {
  bucket        = "${var.project_name}-session-logs-${random_id.suffix.hex}"
  force_destroy = true
  tags = { Name = "${var.project_name}-session-logs" }
}

resource "aws_s3_bucket_public_access_block" "session_logs" {
  bucket                  = aws_s3_bucket.session_logs.id
  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# TODO: Create SSM Document for Session Manager preferences
# Resource: aws_ssm_document, name: "SSM-SessionManagerRunShell"
# document_type: "Session", document_format: "JSON"
# Content: schemaVersion "1.0", sessionType "Standard_Stream"
# inputs: s3BucketName, s3EncryptionEnabled=true, idleSessionTimeout="20"
```

<details>
<summary>ssm.tf -- Solution: Session Manager Preferences</summary>

```hcl
resource "aws_ssm_document" "session_prefs" {
  name = "SSM-SessionManagerRunShell"
  document_type = "Session"
  document_format = "JSON"
  content = jsonencode({
    schemaVersion = "1.0", description = "Session Manager settings"
    sessionType = "Standard_Stream"
    inputs = {
      s3BucketName = aws_s3_bucket.session_logs.id
      s3EncryptionEnabled = true, idleSessionTimeout = "20"
      shellProfile = { linux = "exec /bin/bash" }
    }
  })
}
```

</details>

## Step 3 -- Use Session Manager

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

# Wait for SSM registration (1-2 minutes), then start a session
aws ssm describe-instance-information \
  --filters Key=InstanceIds,Values=$INSTANCE_ID \
  --query 'InstanceInformationList[*].{Id:InstanceId,PingStatus:PingStatus}' \
  --output table

# Interactive shell -- no SSH, no port 22, no key pairs
aws ssm start-session --target "$INSTANCE_ID"

# Non-interactive alternative: Run Command
aws ssm send-command --instance-ids "$INSTANCE_ID" \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["hostname","uptime"]' \
  --query 'Command.CommandId' --output text
```

## Step 4 -- Configure Patch Manager

### TODO 2: Create Patch Baseline and Maintenance Window

Add the following to `ssm.tf`:

```hcl
# TODO: Create patch baseline (aws_ssm_patch_baseline)
# - name: "${var.project_name}-production-baseline", operating_system: "AMAZON_LINUX_2023"
# - approval_rule: approve_after_days=7, CLASSIFICATION=["Security","Bugfix"], SEVERITY=["Critical","Important"]

# TODO: Register patch group (aws_ssm_patch_group)
# - patch_group: "production" (matches EC2 tag PatchGroup=production)

# TODO: Create maintenance window (aws_ssm_maintenance_window)
# - schedule: "cron(0 4 ? * SUN *)", duration: 3, cutoff: 1

# TODO: Register targets (aws_ssm_maintenance_window_target)
# - targets key: "tag:PatchGroup", values: ["production"]

# TODO: Create task (aws_ssm_maintenance_window_task)
# - task_type: "RUN_COMMAND", task_arn: "AWS-RunPatchBaseline"
# - run_command_parameters: Operation=["Install"]
```

<details>
<summary>ssm.tf -- Solution: Patch Manager Configuration</summary>

```hcl
resource "aws_ssm_patch_baseline" "production" {
  name             = "${var.project_name}-production-baseline"
  operating_system = "AMAZON_LINUX_2023"

  approved_patches_compliance_level = "HIGH"

  approval_rule {
    approve_after_days  = 7
    compliance_level    = "HIGH"

    patch_filter {
      key    = "PRODUCT"
      values = ["AmazonLinux2023"]
    }

    patch_filter {
      key    = "CLASSIFICATION"
      values = ["Security", "Bugfix"]
    }

    patch_filter {
      key    = "SEVERITY"
      values = ["Critical", "Important"]
    }
  }

  tags = { Name = "${var.project_name}-production-baseline" }
}

resource "aws_ssm_patch_group" "production" {
  baseline_id = aws_ssm_patch_baseline.production.id
  patch_group = "production"
}

resource "aws_ssm_maintenance_window" "patch" {
  name              = "${var.project_name}-patch-window"
  schedule          = "cron(0 4 ? * SUN *)"
  duration          = 3
  cutoff            = 1
  allow_unassociated_targets = false

  tags = { Name = "${var.project_name}-patch-window" }
}

resource "aws_ssm_maintenance_window_target" "patch" {
  window_id     = aws_ssm_maintenance_window.patch.id
  resource_type = "INSTANCE"

  targets {
    key    = "tag:PatchGroup"
    values = ["production"]
  }
}

resource "aws_ssm_maintenance_window_task" "patch" {
  window_id        = aws_ssm_maintenance_window.patch.id
  task_type        = "RUN_COMMAND"
  task_arn         = "AWS-RunPatchBaseline"
  max_concurrency  = "50%"
  max_errors       = "25%"

  targets {
    key    = "WindowTargetIds"
    values = [aws_ssm_maintenance_window_target.patch.id]
  }

  task_invocation_parameters {
    run_command_parameters {
      parameter {
        name   = "Operation"
        values = ["Install"]
      }
    }
  }
}
```

</details>

## Step 5 -- Verify SSM Registration and Patch Compliance

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

# Check instance SSM registration
aws ssm describe-instance-information \
  --filters Key=InstanceIds,Values=$INSTANCE_ID \
  --query 'InstanceInformationList[0].{Id:InstanceId,PingStatus:PingStatus,Platform:PlatformName,AgentVersion:AgentVersion}' \
  --output table

# Check patch compliance
aws ssm describe-instance-patch-states \
  --instance-ids "$INSTANCE_ID" \
  --query 'InstancePatchStates[0].{Instance:InstanceId,Installed:InstalledCount,Missing:MissingCount,Failed:FailedCount}' \
  --output table
```

## Spot the Bug

An operations team deploys EC2 instances in a private subnet but cannot connect via Session Manager:

```hcl
resource "aws_instance" "private" {
  ami           = data.aws_ami.al2023.id
  instance_type = "t3.micro"
  subnet_id     = aws_subnet.private.id  # No internet access
  # No IAM instance profile attached
  vpc_security_group_ids = [aws_security_group.private.id]
}

resource "aws_security_group" "private" {
  name   = "private-sg"
  vpc_id = aws_vpc.this.id
  egress { from_port = 0, to_port = 0, protocol = "-1", cidr_blocks = ["0.0.0.0/0"] }
}
```

The instance appears in EC2 but does not show up in SSM Fleet Manager.

<details>
<summary>Explain the bug</summary>

**Bug 1: No IAM instance profile.** Without `AmazonSSMManagedInstanceCore`, the SSM Agent cannot authenticate. Fix: add `iam_instance_profile = aws_iam_instance_profile.ssm.name`.

**Bug 2: Private subnet with no route to SSM endpoints.** The SSM Agent needs HTTPS (443) access to `ssm.{region}.amazonaws.com`, `ssmmessages.{region}.amazonaws.com`, and `ec2messages.{region}.amazonaws.com`. Without a NAT Gateway or VPC Interface Endpoints, the agent cannot reach these endpoints.

**Fix:** Add a NAT Gateway or (preferred) create VPC Interface Endpoints for `ssm`, `ssmmessages`, and `ec2messages`. Both bugs must be fixed -- IAM role alone is not sufficient without network connectivity, and vice versa.

</details>

## Verify What You Learned

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

# Verify instance is registered with SSM
aws ssm describe-instance-information \
  --filters Key=InstanceIds,Values=$INSTANCE_ID \
  --query 'InstanceInformationList[0].PingStatus' --output text
```

Expected: `Online`

```bash
# Verify maintenance window exists
aws ssm describe-maintenance-windows \
  --filters Key=Name,Values=saa-ex92-patch-window \
  --query 'WindowIdentities[0].{Name:Name,Schedule:Schedule,Duration:Duration}' \
  --output table
```

Expected: Window with `cron(0 4 ? * SUN *)` schedule and 3-hour duration.

```bash
# Verify security group has NO inbound port 22
aws ec2 describe-security-groups \
  --group-ids $(aws ec2 describe-instances --instance-ids $INSTANCE_ID \
    --query 'Reservations[0].Instances[0].SecurityGroups[0].GroupId' --output text) \
  --query 'SecurityGroups[0].IpPermissions' --output json
```

Expected: Empty array `[]` (no inbound rules -- Session Manager does not need port 22).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 93 covers **EventBridge for Operational Events**, where you will create rules to capture EC2 state changes, EBS snapshot failures, and RDS failovers, triggering automated remediation via SNS and Lambda -- building on the Systems Manager integration patterns from this exercise.

## Summary

- **Session Manager** replaces SSH with a secure, auditable, keyless connection that uses IAM for authentication
- **No inbound ports** are needed -- Session Manager uses outbound HTTPS (443) from the instance to SSM endpoints
- **Three prerequisites** for SSM: (1) SSM Agent installed, (2) IAM role with `AmazonSSMManagedInstanceCore`, (3) network access to SSM endpoints
- **VPC Endpoints** enable Session Manager for instances in private subnets without NAT Gateways or internet access
- **Session logging** to S3 and CloudWatch Logs provides a complete audit trail of all commands executed
- **Patch Manager** automates OS patching with baselines (which patches), maintenance windows (when), and patch groups (which instances)
- **Patch baselines** control approval delays (e.g., 7 days after release), severity filters, and compliance levels
- **Maintenance windows** use cron schedules with duration and cutoff parameters to control the patching window
- **Run Command** executes commands across multiple instances without SSH -- used by Patch Manager under the hood
- **Fleet Manager** provides a console view of all SSM-managed instances with their compliance status

## Reference

- [AWS Systems Manager Session Manager](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager.html)
- [Patch Manager](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-patch.html)
- [Terraform aws_ssm_patch_baseline](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssm_patch_baseline)
- [Terraform aws_ssm_maintenance_window](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssm_maintenance_window)

## Additional Resources

- [Session Manager VPC Endpoint Setup](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-getting-started-privatelink.html) -- required VPC endpoints for private subnet instances
- [Patch Baseline Best Practices](https://docs.aws.amazon.com/systems-manager/latest/userguide/patch-manager-approved-rejected-package-name-formats.html) -- approval rules, rejected patches, and patch exceptions
- [SSM Agent Troubleshooting](https://docs.aws.amazon.com/systems-manager/latest/userguide/ssm-agent-status-and-restart.html) -- diagnosing why instances do not appear in SSM
- [Session Manager Logging](https://docs.aws.amazon.com/systems-manager/latest/userguide/session-manager-logging.html) -- S3 and CloudWatch Logs session transcript configuration
