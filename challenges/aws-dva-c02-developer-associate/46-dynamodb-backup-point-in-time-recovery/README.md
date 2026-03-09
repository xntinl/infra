# 46. DynamoDB Backup and Point-in-Time Recovery

<!--
difficulty: intermediate
concepts: [dynamodb-backup, on-demand-backup, point-in-time-recovery, pitr, continuous-backups, restore-table, backup-retention]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply
prerequisites: [03-dynamodb-developer-sdk-operations]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates DynamoDB tables in on-demand mode. PITR adds approximately $0.20/GB/month for continuous backup storage. Cost is approximately $0.01/hr for minimal table sizes. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

After completing this exercise, you will be able to:

- **Differentiate** between on-demand backups (manual snapshots) and point-in-time recovery (continuous 35-day window)
- **Configure** point-in-time recovery on a DynamoDB table using Terraform
- **Create** on-demand backups and restore them to new tables using the AWS CLI
- **Restore** a table to a specific timestamp within the PITR window using `restore-table-to-point-in-time`
- **Explain** why DynamoDB restore always creates a new table instead of overwriting the original

## Why DynamoDB Backup and PITR

DynamoDB offers two backup mechanisms: on-demand backups and point-in-time recovery (PITR). On-demand backups are manual snapshots you create at a specific moment -- useful before risky migrations or schema changes. PITR provides continuous backups with second-level granularity, letting you restore to any point within the last 35 days. Both are critical for disaster recovery and accidental deletion scenarios.

The DVA-C02 exam tests several nuances. First, restoring a backup always creates a new table -- it does not overwrite the original. This catches developers who expect `restore-table-from-backup` to restore in-place. Second, PITR must be explicitly enabled; it is off by default. Third, restored tables do not inherit certain settings from the source: auto-scaling policies, IAM policies, CloudWatch alarms, tags, stream settings, and TTL configuration must be reconfigured manually. Understanding these limitations is essential for both the exam and production disaster recovery planning.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

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
  default     = "backup-demo"
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "orders" {
  name         = "${var.project_name}-orders"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "order_id"

  attribute {
    name = "order_id"
    type = "S"
  }

  tags = {
    Name = "${var.project_name}-orders"
  }

  # =======================================================
  # TODO 1 -- Enable Point-in-Time Recovery (database.tf)
  # =======================================================
  # Requirements:
  #   - Add a point_in_time_recovery block
  #   - Set enabled = true
  #   - This enables continuous backups with 35-day retention
  #   - PITR is OFF by default -- you must explicitly enable it
  #
  # Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#point_in_time_recovery

}
```

### `outputs.tf`

```hcl
output "table_name" {
  value = aws_dynamodb_table.orders.name
}

output "table_arn" {
  value = aws_dynamodb_table.orders.arn
}
```

After deploying the table, complete the following CLI tasks:

```bash
# =======================================================
# TODO 2 -- Seed the table with test data
# =======================================================
# Insert 5 items into the table using aws dynamodb put-item
# Each item should have: order_id (S), customer (S), amount (N), status (S)
# Example:
#   aws dynamodb put-item --table-name backup-demo-orders \
#     --item '{"order_id":{"S":"order-001"},"customer":{"S":"alice"},"amount":{"N":"49.99"},"status":{"S":"shipped"}}'


# =======================================================
# TODO 3 -- Create an on-demand backup
# =======================================================
# Requirements:
#   - Use aws dynamodb create-backup
#   - Set --table-name and --backup-name
#   - Save the BackupArn from the response
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/dynamodb/create-backup.html
# Hint: aws dynamodb create-backup --table-name backup-demo-orders --backup-name pre-migration-backup


# =======================================================
# TODO 4 -- Verify PITR is enabled and note the earliest/latest restore times
# =======================================================
# Requirements:
#   - Use aws dynamodb describe-continuous-backups
#   - Check PointInTimeRecoveryDescription.PointInTimeRecoveryStatus = ENABLED
#   - Note EarliestRestorableDateTime and LatestRestorableDateTime
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/dynamodb/describe-continuous-backups.html


# =======================================================
# TODO 5 -- Simulate accidental data loss (delete 2 items)
# =======================================================
# Delete order-001 and order-002 from the table
# Then verify they are gone with a scan


# =======================================================
# TODO 6 -- Restore from on-demand backup to a NEW table
# =======================================================
# Requirements:
#   - Use aws dynamodb restore-table-from-backup
#   - Set --target-table-name to a NEW table name (e.g., backup-demo-orders-restored)
#   - Set --backup-arn to the ARN from TODO 3
#   - Wait for the restore to complete (table status ACTIVE)
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/dynamodb/restore-table-from-backup.html
# IMPORTANT: restore creates a NEW table. It does NOT overwrite the original.


# =======================================================
# TODO 7 -- Restore from PITR to a specific timestamp
# =======================================================
# Requirements:
#   - Use aws dynamodb restore-table-to-point-in-time
#   - Set --source-table-name to the original table
#   - Set --target-table-name to another NEW table name (e.g., backup-demo-orders-pitr)
#   - Set --restore-date-time to a timestamp BEFORE the deletions in TODO 5
#   - Wait for the restore to complete
#
# Docs: https://docs.aws.amazon.com/cli/latest/reference/dynamodb/restore-table-to-point-in-time.html
# Hint: Use --use-latest-restorable-time if you want the most recent state before deletions
```

## Spot the Bug

A developer enables PITR and writes a disaster recovery runbook. After an accidental bulk delete, they run the restore command expecting the original table to be overwritten with the backup data.

```bash
# Developer's restore command
aws dynamodb restore-table-to-point-in-time \
  --source-table-name production-orders \
  --target-table-name production-orders \
  --use-latest-restorable-time
```

<details>
<summary>Explain the bug</summary>

The `--target-table-name` is the same as the source table (`production-orders`). DynamoDB restore **always creates a new table** -- it cannot overwrite an existing table. This command fails with a `TableAlreadyExistsException` error.

The correct approach is to restore to a new table, verify the data, then swap:

```bash
# Step 1: Restore to a new table
aws dynamodb restore-table-to-point-in-time \
  --source-table-name production-orders \
  --target-table-name production-orders-restored \
  --use-latest-restorable-time

# Step 2: Wait for the restored table to become ACTIVE
aws dynamodb wait table-exists --table-name production-orders-restored

# Step 3: Verify the restored data
aws dynamodb scan --table-name production-orders-restored --select COUNT

# Step 4: Update application config to point to the restored table
#   OR copy items from restored table back to original table

# Step 5: Delete the original table (optional) and rename
#   DynamoDB does not support table rename -- you must update
#   application configuration to use the new table name
```

Settings NOT restored automatically: auto-scaling policies, IAM policies, CloudWatch alarms, tags, DynamoDB Streams, TTL, global table replication. On the exam, "how do you restore a DynamoDB table" always involves creating a new table, not overwriting the original.

</details>

## Verify What You Learned

```bash
# Verify PITR is enabled
aws dynamodb describe-continuous-backups \
  --table-name backup-demo-orders \
  --query "ContinuousBackupsDescription.PointInTimeRecoveryDescription.PointInTimeRecoveryStatus" \
  --output text
```

Expected: `ENABLED`

```bash
# List on-demand backups
aws dynamodb list-backups \
  --table-name backup-demo-orders \
  --query "BackupSummaries[*].{Name:BackupName,Status:BackupStatus,Created:BackupCreationDateTime}" \
  --output table
```

Expected: at least one backup with status `AVAILABLE`.

```bash
# Verify restored table has all original items
aws dynamodb scan --table-name backup-demo-orders-restored --select COUNT \
  --query "Count" --output text
```

Expected: `5` (all items from before the deletion).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Solutions

<details>
<summary>TODO 1 -- Enable Point-in-Time Recovery</summary>

Add the following block inside the `aws_dynamodb_table` resource in `database.tf`:

```hcl
  point_in_time_recovery {
    enabled = true
  }
```

</details>

<details>
<summary>TODO 2 -- Seed the table with test data</summary>

```bash
for i in $(seq 1 5); do
  aws dynamodb put-item --table-name backup-demo-orders \
    --item "{\"order_id\":{\"S\":\"order-00$i\"},\"customer\":{\"S\":\"customer-$i\"},\"amount\":{\"N\":\"$((i * 25))\"},\"status\":{\"S\":\"shipped\"}}"
done

# Verify
aws dynamodb scan --table-name backup-demo-orders --select COUNT --query "Count" --output text
# Expected: 5
```

</details>

<details>
<summary>TODO 3 -- Create an on-demand backup</summary>

```bash
BACKUP_ARN=$(aws dynamodb create-backup \
  --table-name backup-demo-orders \
  --backup-name pre-migration-backup \
  --query "BackupDetails.BackupArn" --output text)

echo "Backup ARN: $BACKUP_ARN"

# Verify backup status
aws dynamodb describe-backup --backup-arn "$BACKUP_ARN" \
  --query "BackupDescription.BackupDetails.BackupStatus" --output text
# Expected: AVAILABLE
```

</details>

<details>
<summary>TODO 4 -- Verify PITR status</summary>

```bash
aws dynamodb describe-continuous-backups \
  --table-name backup-demo-orders \
  --query "ContinuousBackupsDescription" --output json
```

Expected output includes:

```json
{
  "ContinuousBackupsStatus": "ENABLED",
  "PointInTimeRecoveryDescription": {
    "PointInTimeRecoveryStatus": "ENABLED",
    "EarliestRestorableDateTime": "2025-...",
    "LatestRestorableDateTime": "2025-..."
  }
}
```

</details>

<details>
<summary>TODO 5 -- Simulate accidental data loss</summary>

```bash
# Note the current time BEFORE deletion (for PITR restore)
RESTORE_TIME=$(date -u +%Y-%m-%dT%H:%M:%SZ)
echo "Restore time: $RESTORE_TIME"

# Wait a few seconds to ensure PITR captures the pre-deletion state
sleep 5

# Delete two items
aws dynamodb delete-item --table-name backup-demo-orders \
  --key '{"order_id":{"S":"order-001"}}'
aws dynamodb delete-item --table-name backup-demo-orders \
  --key '{"order_id":{"S":"order-002"}}'

# Verify deletion
aws dynamodb scan --table-name backup-demo-orders --select COUNT \
  --query "Count" --output text
# Expected: 3
```

</details>

<details>
<summary>TODO 6 -- Restore from on-demand backup</summary>

```bash
aws dynamodb restore-table-from-backup \
  --target-table-name backup-demo-orders-restored \
  --backup-arn "$BACKUP_ARN"

# Wait for the table to become ACTIVE
aws dynamodb wait table-exists --table-name backup-demo-orders-restored

# Verify all 5 items are present
aws dynamodb scan --table-name backup-demo-orders-restored --select COUNT \
  --query "Count" --output text
# Expected: 5
```

</details>

<details>
<summary>TODO 7 -- Restore from PITR</summary>

```bash
aws dynamodb restore-table-to-point-in-time \
  --source-table-name backup-demo-orders \
  --target-table-name backup-demo-orders-pitr \
  --restore-date-time "$RESTORE_TIME"

# Wait for the table to become ACTIVE
aws dynamodb wait table-exists --table-name backup-demo-orders-pitr

# Verify all 5 items are present (restored to state before deletions)
aws dynamodb scan --table-name backup-demo-orders-pitr --select COUNT \
  --query "Count" --output text
# Expected: 5
```

</details>

## Cleanup

Destroy Terraform-managed resources:

```bash
terraform destroy -auto-approve
```

Delete manually created restored tables (Terraform does not manage these):

```bash
aws dynamodb delete-table --table-name backup-demo-orders-restored 2>/dev/null
aws dynamodb delete-table --table-name backup-demo-orders-pitr 2>/dev/null
```

Delete on-demand backups:

```bash
aws dynamodb list-backups --table-name backup-demo-orders \
  --query "BackupSummaries[*].BackupArn" --output text | \
  xargs -I{} aws dynamodb delete-backup --backup-arn {}
```

Verify nothing remains:

```bash
terraform state list
aws dynamodb list-tables --query "TableNames[?contains(@, 'backup-demo')]" --output text
```

Expected: no output for both commands.

## What's Next

You configured both backup strategies for DynamoDB and practiced disaster recovery by restoring to new tables. In the next exercise, you will build **SNS topics with SQS and Lambda subscriptions** -- creating a pub/sub messaging system with multiple subscriber types and message delivery verification.

## Summary

- **On-demand backups** are manual snapshots created at a specific point in time -- useful before migrations or risky operations
- **Point-in-time recovery (PITR)** provides continuous backups with second-level granularity for the last 35 days
- PITR is **disabled by default** -- you must explicitly enable it via `point_in_time_recovery { enabled = true }`
- All restores create a **new table** -- DynamoDB cannot restore in-place or overwrite an existing table
- Restored tables do **not inherit** auto-scaling policies, IAM policies, CloudWatch alarms, tags, streams, or TTL settings
- On-demand backups are retained until explicitly deleted; PITR backups are retained for exactly 35 days
- Use `--use-latest-restorable-time` for PITR when you want the most recent recoverable state

## Reference

- [DynamoDB On-Demand Backup and Restore](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/BackupRestore.html)
- [DynamoDB Point-in-Time Recovery](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/PointInTimeRecovery.html)
- [Terraform aws_dynamodb_table point_in_time_recovery](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dynamodb_table#point_in_time_recovery)
- [AWS CLI restore-table-to-point-in-time](https://docs.aws.amazon.com/cli/latest/reference/dynamodb/restore-table-to-point-in-time.html)
- [AWS CLI create-backup](https://docs.aws.amazon.com/cli/latest/reference/dynamodb/create-backup.html)

## Additional Resources

- [Restoring a DynamoDB Table -- What Gets Restored](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/BackupRestore.Restore.html) -- settings that are and are not restored
- [AWS Backup for DynamoDB](https://docs.aws.amazon.com/aws-backup/latest/devguide/whatisbackup.html) -- centralized backup management across multiple AWS services
- [DynamoDB Export to S3](https://docs.aws.amazon.com/amazondynamodb/latest/developerguide/DataExport.html) -- long-term archival beyond the 35-day PITR window
