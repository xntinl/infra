# 86. AWS Backup Centralized Management

<!--
difficulty: intermediate
concepts: [aws-backup, backup-plan, backup-vault, vault-lock, lifecycle-rules, cross-region-copy, backup-selection, iam-role, compliance-mode, governance-mode, recovery-point, restore-testing, backup-policies-organizations]
tools: [terraform, aws-cli]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [03-ebs-volume-types-optimization, 83-efs-vs-fsx-decision-framework]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** AWS Backup charges for storage used by recovery points. EBS snapshots: $0.05/GB-month. EFS backup: $0.05/GB-month. Cross-region copy: additional storage + data transfer. For this exercise with small test resources, cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercises 03 (EBS) or 83 (EFS) | Understanding of storage resources |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** AWS Backup plans with lifecycle rules, retention policies, and scheduling using Terraform.
2. **Analyze** the difference between governance mode and compliance mode vault locks and their immutability guarantees.
3. **Apply** cross-region backup copies for disaster recovery.
4. **Evaluate** when to use AWS Backup versus native service backups (EBS snapshots, RDS automated backups).
5. **Design** a centralized backup strategy for multi-service, multi-account environments.

---

## Why This Matters

AWS Backup is the SAA-C03 answer to "how do you manage backups across multiple AWS services from a single place?" Before AWS Backup, each service had its own backup mechanism: EBS snapshots, RDS automated backups, DynamoDB on-demand backups, EFS backups. AWS Backup provides a unified backup policy that applies to all supported services, with centralized monitoring, compliance reporting, and cross-region/cross-account copy.

The exam tests three key concepts. First, backup plans and selections: a backup plan defines when, how often, and how long to retain backups; a backup selection defines which resources to back up (by tag, resource ARN, or resource type). Second, vault locks: governance mode allows users with sufficient IAM permissions to modify or delete the vault lock and recovery points; compliance mode makes the vault lock and recovery points immutable -- once set, they cannot be deleted by anyone, including the root user, until the retention period expires. Third, cross-region copy: backup plans can include copy actions that replicate recovery points to a vault in another region, providing disaster recovery for regional failures.

The vault lock distinction is heavily tested. The exam presents scenarios where "backup data must not be deletable by any user, including administrators, for regulatory compliance" -- the answer is compliance mode. "Backup data should be protected from accidental deletion but administrators can override in emergencies" -- the answer is governance mode. This parallels S3 Object Lock governance vs compliance modes.

---

## Building Blocks

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_version = ">= 1.5"
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

provider "aws" {
  alias  = "dr_region"
  region = "us-west-2"
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
  default     = "saa-ex86"
}
```

### `main.tf`

```hcl
# ============================================================
# TODO 1: Backup Vault (Primary Region)
# ============================================================
# Create a backup vault to store recovery points.
#
# Requirements:
#   - Resource: aws_backup_vault
#   - name = "${var.project_name}-vault"
#   - Optional: KMS key for encryption
#
# A backup vault is a container for recovery points (backups).
# Each account has a default vault, but creating dedicated
# vaults provides:
#   - Separate access control per vault
#   - Different encryption keys per vault
#   - Vault lock for compliance
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/backup_vault
# ============================================================


# ============================================================
# TODO 2: Backup Vault Lock (Governance Mode)
# ============================================================
# Configure a vault lock in governance mode.
#
# Requirements:
#   - Resource: aws_backup_vault_lock_configuration
#   - backup_vault_name = vault from TODO 1
#   - min_retention_days = 7
#   - max_retention_days = 365
#
# Governance mode vs Compliance mode:
#   - Governance: authorized IAM users CAN delete the lock
#     and recovery points. Use for protecting against
#     accidental deletion while allowing admin override.
#   - Compliance: NOBODY can delete the lock or recovery
#     points until retention expires. Not even root user.
#     Use for regulatory requirements (SEC 17a-4, CFTC, FINRA).
#     Set by adding changeable_for_days = 0 (or omitting it
#     after the grace period).
#
# WARNING: Compliance mode is IRREVERSIBLE. Once the grace
# period (changeable_for_days) expires, the lock cannot be
# removed and recovery points cannot be deleted until their
# retention period ends. You will be billed for storage.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/backup_vault_lock_configuration
# ============================================================


# ============================================================
# TODO 3: Backup Plan with Lifecycle Rules
# ============================================================
# Create a backup plan with daily backups and lifecycle rules.
#
# Requirements:
#   - Resource: aws_backup_plan
#   - name = "${var.project_name}-daily-plan"
#   - Rule 1: "daily-backup"
#     - schedule = "cron(0 3 * * ? *)" (3 AM UTC daily)
#     - target_vault_name = vault from TODO 1
#     - start_window = 60 (minutes to start)
#     - completion_window = 180 (minutes to complete)
#     - lifecycle:
#       - cold_storage_after = 30 (days to transition to cold)
#       - delete_after = 365 (days to delete)
#     - copy_action: (cross-region copy)
#       - destination_vault_arn = DR vault ARN
#       - lifecycle:
#         - delete_after = 90 (shorter retention in DR)
#
# Lifecycle:
#   Warm storage (EBS/EFS snapshots) -> Cold storage (cheaper)
#   -> Delete after retention period
#
# Not all resource types support cold storage transition.
# EFS supports cold storage. EBS does not.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/backup_plan
# ============================================================


# ============================================================
# TODO 4: Backup Selection (Tag-Based)
# ============================================================
# Define which resources to back up using tag-based selection.
#
# Requirements:
#   - Resource: aws_backup_selection
#   - name = "${var.project_name}-tag-selection"
#   - plan_id = backup plan from TODO 3
#   - iam_role_arn = backup IAM role
#   - selection_tag:
#     - type = "STRINGEQUALS"
#     - key = "backup"
#     - value = "daily"
#
# Any resource tagged with backup=daily will be automatically
# included in the backup plan. This is the recommended
# approach for large environments -- tag new resources and
# they are automatically backed up.
#
# Alternative selections:
#   - resources = [list of ARNs] (explicit)
#   - not_resources = [list of ARNs to exclude]
#   - conditions block for complex tag-based filtering
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/backup_selection
# ============================================================


# ============================================================
# TODO 5: DR Region Backup Vault
# ============================================================
# Create a backup vault in the DR region for cross-region copy.
#
# Requirements:
#   - Resource: aws_backup_vault (with provider = aws.dr_region)
#   - name = "${var.project_name}-dr-vault"
#
# Cross-region copy provides disaster recovery:
#   - Primary region fails -> restore from DR vault
#   - Recovery points are independent copies, not references
#   - Data transfer charges apply for cross-region copy
# ============================================================


# ---------- Test Resources (to be backed up) ----------

resource "aws_ebs_volume" "test" {
  availability_zone = "us-east-1a"
  size              = 1
  type              = "gp3"

  tags = {
    Name   = "${var.project_name}-test-volume"
    backup = "daily"
  }
}
```

### `iam.tf`

```hcl
resource "aws_iam_role" "backup" {
  name = "${var.project_name}-backup-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "backup.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "backup" {
  role       = aws_iam_role.backup.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForBackup"
}

resource "aws_iam_role_policy_attachment" "restore" {
  role       = aws_iam_role.backup.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSBackupServiceRolePolicyForRestores"
}
```

### `outputs.tf`

```hcl
output "vault_name" {
  value = "Set after TODO 1 implementation"
}

output "backup_plan_id" {
  value = "Set after TODO 3 implementation"
}

output "test_volume_id" {
  value = aws_ebs_volume.test.id
}
```

---

## AWS Backup Supported Services

| Service | Cold Storage | Cross-Region | Cross-Account | Point-in-Time |
|---|---|---|---|---|
| **EBS** | No | Yes | Yes | No |
| **EFS** | Yes | Yes | Yes | No |
| **RDS** | No | Yes | Yes | Yes |
| **Aurora** | No | Yes | Yes | Yes |
| **DynamoDB** | Yes | Yes | Yes | Yes |
| **FSx (all types)** | No | Yes | Yes | No |
| **EC2 (AMI)** | No | Yes | Yes | No |
| **S3** | Yes | Yes | Yes | Yes (versioning) |
| **DocumentDB** | No | Yes | Yes | Yes |
| **Neptune** | No | Yes | Yes | No |

### Vault Lock Modes

| Feature | Governance Mode | Compliance Mode |
|---|---|---|
| **Who can delete lock** | IAM users with permission | Nobody (including root) |
| **Who can delete recovery points** | IAM users with permission | Nobody until retention expires |
| **Grace period** | N/A | `changeable_for_days` (optional) |
| **Reversible** | Yes | No (after grace period) |
| **Use case** | Protect from accidental deletion | Regulatory compliance (WORM) |
| **Risk** | Admin can override | Stuck paying for storage |

### AWS Backup vs Native Backups

| Feature | AWS Backup | Native (e.g., EBS Snapshots) |
|---|---|---|
| **Cross-service policy** | Single plan for all services | Per-service configuration |
| **Cross-region copy** | Built into backup plan | Separate automation required |
| **Cross-account copy** | Built-in | Separate automation required |
| **Lifecycle management** | Warm -> Cold -> Delete | Manual or custom automation |
| **Compliance** | Vault lock, audit | No equivalent |
| **Monitoring** | Centralized dashboard | Per-service CloudWatch |
| **Cost** | AWS Backup pricing | Native service pricing |

---

## Spot the Bug

The following backup configuration is intended to ensure that backup data cannot be deleted by anyone for regulatory compliance. Identify the flaw.

```hcl
resource "aws_backup_vault" "compliance" {
  name = "regulatory-compliance-vault"
}

resource "aws_backup_vault_lock_configuration" "compliance" {
  backup_vault_name = aws_backup_vault.compliance.name
  min_retention_days = 365
  max_retention_days = 2555  # 7 years
}

resource "aws_backup_plan" "daily" {
  name = "compliance-daily"

  rule {
    rule_name         = "daily-backup"
    target_vault_name = aws_backup_vault.compliance.name
    schedule          = "cron(0 3 * * ? *)"

    lifecycle {
      delete_after = 2555  # 7 years
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

**The vault lock is in governance mode, not compliance mode.** Governance mode is the default when `changeable_for_days` is not specified (or is omitted with no special configuration). In governance mode, IAM users with `backup:DeleteRecoveryPoint` and `backup:DeleteBackupVaultLockConfiguration` permissions can delete recovery points and remove the vault lock. This does not meet the requirement that "backup data cannot be deleted by anyone."

For true regulatory compliance (WORM - Write Once Read Many), you need compliance mode. Compliance mode is activated when the vault lock has a grace period that has expired:

**Fix:**

```hcl
resource "aws_backup_vault_lock_configuration" "compliance" {
  backup_vault_name  = aws_backup_vault.compliance.name
  changeable_for_days = 3       # 3-day grace period to verify config
  min_retention_days  = 365
  max_retention_days  = 2555
}
```

**CRITICAL WARNING:** After the `changeable_for_days` grace period expires, the vault lock enters compliance mode permanently:
- The lock CANNOT be removed by anyone, including root
- Recovery points CANNOT be deleted until their retention period expires
- You WILL be charged for storage for the entire retention period
- The minimum `changeable_for_days` is 3 days (to give you time to verify)

**Governance mode** (without `changeable_for_days`) is suitable when you want to:
- Protect against accidental deletion
- Allow administrators to override in emergencies
- Avoid the permanent commitment of compliance mode

**Compliance mode** (with `changeable_for_days`) is required when:
- Regulations mandate immutable backups (SEC 17a-4, HIPAA, PCI DSS)
- Audit requirements specify that no user, including administrators, can delete backups
- The organization accepts the cost commitment for the full retention period

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify the backup vault:**
   ```bash
   aws backup describe-backup-vault \
     --backup-vault-name saa-ex86-vault \
     --query '{Name:BackupVaultName,RecoveryPoints:NumberOfRecoveryPoints,Locked:Locked}' \
     --output table
   ```
   Expected: Vault exists with Locked status.

3. **Verify vault lock configuration:**
   ```bash
   aws backup describe-backup-vault \
     --backup-vault-name saa-ex86-vault \
     --query '{Locked:Locked,MinRetention:MinRetentionDays,MaxRetention:MaxRetentionDays}' \
     --output table
   ```
   Expected: Locked `true`, MinRetention `7`, MaxRetention `365`.

4. **Verify backup plan:**
   ```bash
   aws backup list-backup-plans \
     --query 'BackupPlansList[?BackupPlanName==`saa-ex86-daily-plan`].{Name:BackupPlanName,Id:BackupPlanId}' \
     --output table
   ```
   Expected: Backup plan exists.

5. **Verify backup selection (tagged resources):**
   ```bash
   PLAN_ID=$(terraform output -raw backup_plan_id)
   aws backup list-backup-selections \
     --backup-plan-id "$PLAN_ID" \
     --query 'BackupSelectionsList[*].{Name:SelectionName,Id:SelectionId}' \
     --output table
   ```
   Expected: Tag-based selection configured.

6. **Verify the EBS volume has the backup tag:**
   ```bash
   VOL_ID=$(terraform output -raw test_volume_id)
   aws ec2 describe-volumes --volume-ids "$VOL_ID" \
     --query 'Volumes[0].Tags[?Key==`backup`].Value' --output text
   ```
   Expected: `daily`

7. **Start an on-demand backup:**
   ```bash
   VOL_ID=$(terraform output -raw test_volume_id)
   aws backup start-backup-job \
     --backup-vault-name saa-ex86-vault \
     --resource-arn "arn:aws:ec2:us-east-1:$(aws sts get-caller-identity --query Account --output text):volume/$VOL_ID" \
     --iam-role-arn "$(aws iam get-role --role-name saa-ex86-backup-role --query 'Role.Arn' --output text)" \
     --lifecycle DeleteAfterDays=30
   ```
   Expected: Backup job created.

8. **Terraform state consistency:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>main.tf -- TODO 1: Primary Backup Vault</summary>

```hcl
resource "aws_backup_vault" "primary" {
  name = "${var.project_name}-vault"
  tags = { Name = "${var.project_name}-vault" }
}
```

</details>

<details>
<summary>main.tf -- TODO 2: Vault Lock (Governance Mode)</summary>

```hcl
resource "aws_backup_vault_lock_configuration" "primary" {
  backup_vault_name = aws_backup_vault.primary.name
  min_retention_days = 7
  max_retention_days = 365

  # NOTE: Omitting changeable_for_days = governance mode.
  # To enable compliance mode (IRREVERSIBLE), add:
  # changeable_for_days = 3
}
```

In governance mode, the lock enforces min/max retention on new recovery points but authorized IAM users can delete recovery points and remove the lock. For this exercise, governance mode is safe to deploy and destroy.

</details>

<details>
<summary>main.tf -- TODO 3 and TODO 5: Backup Plan with DR Vault</summary>

```hcl
resource "aws_backup_vault" "dr" {
  provider = aws.dr_region
  name     = "${var.project_name}-dr-vault"
  tags     = { Name = "${var.project_name}-dr-vault" }
}

resource "aws_backup_plan" "daily" {
  name = "${var.project_name}-daily-plan"

  rule {
    rule_name         = "daily-backup"
    target_vault_name = aws_backup_vault.primary.name
    schedule          = "cron(0 3 * * ? *)"
    start_window      = 60
    completion_window = 180

    lifecycle {
      cold_storage_after = 30
      delete_after       = 365
    }

    copy_action {
      destination_vault_arn = aws_backup_vault.dr.arn

      lifecycle {
        delete_after = 90
      }
    }
  }

  tags = { Name = "${var.project_name}-daily-plan" }
}
```

The lifecycle transitions recovery points from warm to cold storage after 30 days (cheaper storage, slower restore). After 365 days, recovery points are deleted. Cross-region copies have a shorter retention (90 days) because they are for disaster recovery, not long-term archival.

Note: Cold storage transition is supported for EFS and DynamoDB but not for EBS.

</details>

<details>
<summary>main.tf -- TODO 4: Tag-Based Selection</summary>

```hcl
resource "aws_backup_selection" "tagged" {
  name         = "${var.project_name}-tag-selection"
  plan_id      = aws_backup_plan.daily.id
  iam_role_arn = aws_iam_role.backup.arn

  selection_tag {
    type  = "STRINGEQUALS"
    key   = "backup"
    value = "daily"
  }
}
```

Tag-based selection is the most scalable approach. New resources tagged with `backup=daily` are automatically included in the backup plan without modifying the Terraform configuration.

</details>

<details>
<summary>outputs.tf -- Updated Outputs</summary>

```hcl
output "vault_name" {
  value = aws_backup_vault.primary.name
}

output "backup_plan_id" {
  value = aws_backup_plan.daily.id
}

output "test_volume_id" {
  value = aws_ebs_volume.test.id
}
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: If the vault lock is in governance mode, Terraform can delete the vault. If accidentally set to compliance mode, recovery points cannot be deleted until their retention period expires.

Verify:

```bash
aws backup describe-backup-vault --backup-vault-name saa-ex86-vault 2>&1 | grep -q "does not exist" && echo "Vault deleted" || echo "Still exists"
```

---

## What's Next

Exercise 87 begins the **Data Transfer** section with **Snow Family devices**, covering Snowcone, Snowball Edge, and Snowmobile for offline data transfer. You will evaluate when to use Snow Family versus online transfer methods (DataSync, Transfer Family) based on data volume, bandwidth, and timeline constraints.

---

## Summary

- **AWS Backup** provides centralized backup management across EBS, EFS, RDS, DynamoDB, FSx, EC2, S3, and more
- **Backup plans** define schedule, retention, lifecycle (warm -> cold -> delete), and cross-region copy rules
- **Backup selections** specify which resources to back up: by tag (recommended), ARN list, or resource type
- **Vault lock governance mode** protects against accidental deletion but allows admin override
- **Vault lock compliance mode** makes recovery points immutable -- nobody can delete until retention expires (WORM)
- **Compliance mode is irreversible** after the grace period (`changeable_for_days`) expires
- **Cross-region copy** replicates recovery points to a DR region vault for disaster recovery
- **Cross-account copy** uses AWS Organizations backup policies for multi-account backup management
- **Cold storage** is supported for EFS and DynamoDB (not EBS) -- reduces storage costs for long-retention backups
- **Tag-based selection** is the most scalable approach -- tag new resources and they are automatically backed up
- **AWS Backup vs native**: Backup adds centralized policy, cross-region/account copy, vault lock, and unified monitoring

## Reference

- [AWS Backup Developer Guide](https://docs.aws.amazon.com/aws-backup/latest/devguide/whatisbackup.html)
- [Backup Vault Lock](https://docs.aws.amazon.com/aws-backup/latest/devguide/vault-lock.html)
- [Terraform aws_backup_vault](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/backup_vault)
- [AWS Backup Pricing](https://aws.amazon.com/backup/pricing/)

## Additional Resources

- [Cross-Region Backup Copy](https://docs.aws.amazon.com/aws-backup/latest/devguide/cross-region-backup.html) -- disaster recovery with replicated recovery points
- [Cross-Account Backup](https://docs.aws.amazon.com/aws-backup/latest/devguide/manage-cross-account.html) -- centralized backup across an AWS Organization
- [Backup Audit Manager](https://docs.aws.amazon.com/aws-backup/latest/devguide/backup-audit-manager.html) -- audit and report on backup compliance
- [Restore Testing](https://docs.aws.amazon.com/aws-backup/latest/devguide/restore-testing.html) -- automated restore validation to verify backup integrity
