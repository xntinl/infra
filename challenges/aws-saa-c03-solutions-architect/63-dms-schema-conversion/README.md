# 63. DMS and Schema Conversion

<!--
difficulty: intermediate
concepts: [dms-replication-instance, source-endpoint, target-endpoint, migration-task, cdc, full-load, schema-conversion-tool, heterogeneous-migration, homogeneous-migration, lob-columns]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: apply, analyze
prerequisites: [01-rds-multi-az-read-replicas]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** DMS replication instance dms.t3.micro costs ~$0.018/hr. Source and target RDS instances add ~$0.03/hr total. Total ~$0.05/hr. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 01 (RDS basics) | Understanding of RDS instances |
| Default VPC with subnets in >= 2 AZs | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[*].AvailabilityZone'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a DMS replication instance, source/target endpoints, and migration task using Terraform.
2. **Distinguish** between homogeneous migrations (same engine, e.g., MySQL to MySQL) and heterogeneous migrations (different engines, e.g., Oracle to PostgreSQL).
3. **Apply** the three DMS migration types: full load only, CDC only, and full load + CDC for minimal downtime migration.
4. **Evaluate** when AWS Schema Conversion Tool (SCT) is needed and what it automates vs what requires manual conversion.
5. **Diagnose** common DMS issues: replication instance sizing for LOB columns, network connectivity, and CDC requirements.

---

## Why This Matters

DMS appears on the SAA-C03 exam in migration scenarios. The critical architectural insight is understanding the difference between homogeneous and heterogeneous migrations. Homogeneous migrations (MySQL to Aurora MySQL, PostgreSQL to Aurora PostgreSQL) are straightforward -- DMS copies data while the application continues writing to the source. Heterogeneous migrations (Oracle to PostgreSQL, SQL Server to Aurora) require the AWS Schema Conversion Tool (SCT) to translate stored procedures, data types, and schema objects that differ between engines.

The exam tests two key patterns: (1) "migrate with minimal downtime" -- the answer is DMS with full load + CDC, which copies all existing data then captures ongoing changes until cutover; (2) "migrate Oracle to Aurora PostgreSQL" -- the answer involves SCT for schema conversion plus DMS for data migration. Understanding that DMS handles data movement while SCT handles schema translation is the essential distinction.

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
  default     = "saa-ex63"
}
```

### `security.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

resource "aws_security_group" "db" {
  name   = "${var.project_name}-db-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 3306
    to_port     = 3306
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "MySQL from VPC"
  }

  ingress {
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "PostgreSQL from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `database.tf`

```hcl
# ============================================================
# TODO 1: Create Source and Target RDS Instances
# ============================================================
# Create MySQL source (with binlog_format=ROW for CDC) and
# PostgreSQL target to simulate heterogeneous migration.
# See Solutions for full resource definitions.
#
# Docs: https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Source.MySQL.html
# ============================================================
```

### `dms.tf`

```hcl
# ============================================================
# TODO 2: Create DMS Replication Instance
# ============================================================
# Create replication instance + subnet group. Must have
# network connectivity to both source and target.
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_replication_instance
# ============================================================


# ============================================================
# TODO 3: Create DMS Endpoints
# ============================================================
# Source endpoint (MySQL) and target endpoint (PostgreSQL).
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_endpoint
# ============================================================


# ============================================================
# TODO 4: Create DMS Migration Task
# ============================================================
# migration_type = "full-load-and-cdc" for minimal downtime.
# table_mappings selects which schemas/tables to migrate.
#
# Types: "full-load" (one-time), "cdc" (ongoing only),
#        "full-load-and-cdc" (copy then stream changes)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_replication_task
# ============================================================
```

### `outputs.tf`

```hcl
output "source_endpoint" {
  value = "Set after TODO 1 implementation"
}

output "target_endpoint" {
  value = "Set after TODO 1 implementation"
}

output "replication_instance_arn" {
  value = "Set after TODO 2 implementation"
}
```

---

## Migration Decision Framework

| Question | Answer | Approach |
|---|---|---|
| **Same engine?** | Yes (MySQL to MySQL) | DMS only (homogeneous) |
| **Different engine?** | Yes (Oracle to PostgreSQL) | SCT (schema) + DMS (data) |
| **Hours downtime OK?** | Yes | full-load (one-time copy) |
| **Minutes downtime?** | Yes | full-load-and-cdc (copy then stream) |
| **Zero downtime?** | Yes | cdc (ongoing changes only) |

---

## Spot the Bug

A team configured a DMS migration from Oracle to Aurora PostgreSQL. The migration runs but fails on several tables with large text and image columns:

```hcl
resource "aws_dms_replication_instance" "this" {
  replication_instance_id    = "oracle-to-aurora"
  replication_instance_class = "dms.t3.micro"
  allocated_storage          = 20
}

resource "aws_dms_replication_task" "this" {
  replication_task_id      = "oracle-migration"
  replication_instance_arn = aws_dms_replication_instance.this.replication_instance_arn
  source_endpoint_arn      = aws_dms_endpoint.oracle.endpoint_arn
  target_endpoint_arn      = aws_dms_endpoint.aurora.endpoint_arn
  migration_type           = "full-load-and-cdc"

  table_mappings = jsonencode({
    rules = [{
      rule-type      = "selection"
      rule-id        = "1"
      rule-name      = "all-tables"
      object-locator = {
        schema-name = "HR"
        table-name  = "%"
      }
      rule-action = "include"
    }]
  })
}
```

The migration log shows: `"LOB column detected. LOB data might not be replicated correctly with limited LOB mode."` and several tables fail with out-of-memory errors.

<details>
<summary>Explain the bug</summary>

**The replication instance is too small (`dms.t3.micro`) for tables with LOB (Large Object) columns.** DMS processes LOB columns differently from regular columns:

1. **Limited LOB mode (default):** Truncates LOB data to a maximum size (default 32 KB). Fast but loses data if LOBs exceed the limit.
2. **Full LOB mode:** Reads complete LOB data but must hold each LOB in memory during transfer. Extremely slow for large LOBs.
3. **Inline LOB mode:** LOBs under a threshold are transferred inline; larger ones use full LOB mode. Best balance of speed and completeness.

The `dms.t3.micro` instance has only 1 GB RAM. When processing tables with large LOBs (Oracle CLOB/BLOB columns can be up to 4 GB), the instance runs out of memory.

**Fix:** Use a larger replication instance and configure LOB settings appropriately:

```hcl
resource "aws_dms_replication_instance" "this" {
  replication_instance_id    = "oracle-to-aurora"
  replication_instance_class = "dms.r5.large"  # 16 GB RAM for LOB processing
  allocated_storage          = 100              # More storage for CDC caching
}

resource "aws_dms_replication_task" "this" {
  # ... same as above ...

  replication_task_settings = jsonencode({
    TargetMetadata = {
      LobMaxSize     = 102400    # 100 MB max LOB size
      InlineLobMaxSize = 32768   # 32 KB inline threshold
      LobChunkSize   = 64        # 64 KB chunks
      LoadMaxFileSize = 0
      FullLobMode    = false
      LimitedSizeLobMode = true
    }
  })
}
```

Sizing rule: no LOBs = `dms.t3.medium`; small LOBs = `dms.r5.large`; large LOBs = `dms.r5.xlarge+`.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: RDS instances take 5-10 minutes. DMS replication instance takes 5-10 minutes.

2. **Verify the replication instance:**
   ```bash
   aws dms describe-replication-instances \
     --filters Name=replication-instance-id,Values=saa-ex63-repl \
     --query 'ReplicationInstances[0].{Status:ReplicationInstanceStatus,Class:ReplicationInstanceClass,Storage:AllocatedStorage}' \
     --output json
   ```
   Expected: Status = `available`.

3. **Verify migration task:**
   ```bash
   aws dms describe-replication-tasks \
     --filters Name=replication-task-id,Values=saa-ex63-task \
     --query 'ReplicationTasks[0].{Status:Status,MigrationType:MigrationType,TaskId:ReplicationTaskIdentifier}' \
     --output json
   ```
   Expected: MigrationType = `full-load-and-cdc`.

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Source and Target RDS (database.tf)</summary>

```hcl
resource "aws_db_parameter_group" "mysql_cdc" {
  name   = "${var.project_name}-mysql-cdc"
  family = "mysql8.0"
  parameter { name = "binlog_format"; value = "ROW" }
  parameter { name = "binlog_row_image"; value = "Full" }
}

resource "aws_db_instance" "source" {
  identifier = "${var.project_name}-source"; engine = "mysql"; engine_version = "8.0"
  instance_class = "db.t3.micro"; allocated_storage = 20
  db_name = "sourcedb"; username = "admin"; password = "Source1234!"
  skip_final_snapshot = true; vpc_security_group_ids = [aws_security_group.db.id]
  parameter_group_name = aws_db_parameter_group.mysql_cdc.name
  tags = { Name = "${var.project_name}-source" }
}

resource "aws_db_instance" "target" {
  identifier = "${var.project_name}-target"; engine = "postgres"; engine_version = "16"
  instance_class = "db.t3.micro"; allocated_storage = 20
  db_name = "targetdb"; username = "admin"; password = "Target1234!"
  skip_final_snapshot = true; vpc_security_group_ids = [aws_security_group.db.id]
  tags = { Name = "${var.project_name}-target" }
}
```

Update `outputs.tf` to replace the placeholder outputs:

```hcl
output "source_endpoint" { value = aws_db_instance.source.address }
output "target_endpoint" { value = aws_db_instance.target.address }
```

`binlog_format = ROW` is required for MySQL CDC. Without it, CDC fails silently.

</details>

<details>
<summary>TODO 2 -- DMS Replication Instance (dms.tf)</summary>

```hcl
resource "aws_dms_replication_subnet_group" "this" {
  replication_subnet_group_id          = "${var.project_name}-subnet-group"
  replication_subnet_group_description = "DMS subnet group"
  subnet_ids                           = data.aws_subnets.default.ids
}

resource "aws_dms_replication_instance" "this" {
  replication_instance_id     = "${var.project_name}-repl"
  replication_instance_class  = "dms.t3.micro"
  allocated_storage           = 20
  publicly_accessible         = false
  vpc_security_group_ids      = [aws_security_group.db.id]
  replication_subnet_group_id = aws_dms_replication_subnet_group.this.id
  tags = { Name = "${var.project_name}-replication-instance" }
}

```

Update `outputs.tf`:

```hcl
output "replication_instance_arn" { value = aws_dms_replication_instance.this.replication_instance_arn }
```

</details>

<details>
<summary>TODO 3 -- DMS Endpoints (dms.tf)</summary>

```hcl
resource "aws_dms_endpoint" "source" {
  endpoint_id = "${var.project_name}-source"; endpoint_type = "source"; engine_name = "mysql"
  server_name = aws_db_instance.source.address; port = 3306
  username = "admin"; password = "Source1234!"; database_name = "sourcedb"
}

resource "aws_dms_endpoint" "target" {
  endpoint_id = "${var.project_name}-target"; endpoint_type = "target"; engine_name = "postgres"
  server_name = aws_db_instance.target.address; port = 5432
  username = "admin"; password = "Target1234!"; database_name = "targetdb"
}
```

</details>

<details>
<summary>TODO 4 -- DMS Migration Task (dms.tf)</summary>

```hcl
resource "aws_dms_replication_task" "this" {
  replication_task_id      = "${var.project_name}-task"
  replication_instance_arn = aws_dms_replication_instance.this.replication_instance_arn
  source_endpoint_arn      = aws_dms_endpoint.source.endpoint_arn
  target_endpoint_arn      = aws_dms_endpoint.target.endpoint_arn
  migration_type           = "full-load-and-cdc"
  table_mappings = jsonencode({
    rules = [{ rule-type = "selection", rule-id = "1", rule-name = "include-all",
      object-locator = { schema-name = "sourcedb", table-name = "%" }, rule-action = "include" }]
  })
  tags = { Name = "${var.project_name}-migration-task" }
}
```

`full-load-and-cdc` copies all data then captures ongoing changes for minimal-downtime migration.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

DMS and RDS resource deletion takes 5-10 minutes. Verify:

```bash
aws dms describe-replication-instances \
  --filters Name=replication-instance-id,Values=saa-ex63-repl 2>&1 || echo "DMS deleted"
aws rds describe-db-instances \
  --db-instance-identifier saa-ex63-source 2>&1 || echo "Source deleted"
aws rds describe-db-instances \
  --db-instance-identifier saa-ex63-target 2>&1 || echo "Target deleted"
```

---

## What's Next

Exercise 64 covers **RDS Proxy for connection pooling**. You will configure RDS Proxy between Lambda functions and RDS to solve the connection exhaustion problem that occurs when hundreds of concurrent Lambda invocations each open their own database connection -- a critical serverless architecture pattern.

---

## Summary

- **DMS** migrates data between databases -- same engine (homogeneous) or different engines (heterogeneous)
- **Three migration types:** full-load (one-time copy), CDC (ongoing replication), full-load-and-cdc (minimal downtime migration)
- **Replication instance** is the compute that runs migration tasks -- must have network connectivity to both source and target
- **SCT (Schema Conversion Tool)** is needed for heterogeneous migrations -- converts schema objects, stored procedures, and data types between engines
- **CDC requires source database configuration** -- MySQL needs `binlog_format = ROW`, PostgreSQL needs logical replication, Oracle needs supplemental logging
- **LOB columns** require larger replication instances -- full LOB mode holds entire objects in memory during transfer
- **Table mappings** control which tables are migrated and can apply transformations (rename schemas, filter columns, add columns)
- **Endpoint connectivity test** should always be run before starting a migration task -- validates network, credentials, and permissions
- **DMS Serverless** eliminates capacity planning by auto-scaling replication compute

## Reference

- [DMS User Guide](https://docs.aws.amazon.com/dms/latest/userguide/Welcome.html)
- [DMS Best Practices](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_BestPractices.html)
- [Schema Conversion Tool](https://docs.aws.amazon.com/SchemaConversionTool/latest/userguide/CHAP_Welcome.html)
- [Terraform aws_dms_replication_task](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_replication_task)

## Additional Resources

- [DMS CDC Source Requirements](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Source.html) -- CDC prerequisites per source engine
- [DMS Table Mapping](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Tasks.CustomizingTasks.TableMapping.html) -- selection and transformation rules
- [DMS Serverless](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_Serverless.html) -- auto-scaling replication
