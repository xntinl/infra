# 11. Migration Strategy: The 6 Rs with Application Discovery

<!--
difficulty: advanced
concepts: [migration-hub, application-discovery, 6rs, rehost, replatform, refactor, repurchase, retain, retire, dms, sct]
tools: [terraform, aws-cli]
estimated_time: 80m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** DMS replication instance (dms.t3.micro ~$0.018/hr), Aurora Serverless v2 (~$0.06/hr minimum), and supporting resources. Total ~$0.10/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 01-10 or equivalent knowledge
- Basic understanding of MySQL databases
- Familiarity with containerization concepts (Docker, ECS)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** each of the 6 Rs (Rehost, Replatform, Refactor, Repurchase, Retain, Retire) and select the appropriate strategy for a given workload component
- **Design** a migration plan for a multi-tier application using AWS Migration Hub and Application Discovery Service
- **Implement** a DMS replication task to migrate data from MySQL to Aurora MySQL with ongoing replication
- **Analyze** the trade-offs between migration strategies in terms of effort, cost, downtime, and long-term benefit

## Why This Matters

The SAA-C03 exam presents scenarios where you must choose the right migration strategy for different components of an application stack. A typical enterprise application has a web tier, application tier, database tier, monitoring, and file storage -- each with different migration constraints. Choosing "lift and shift" (Rehost) for everything wastes the opportunity to reduce operational burden. Choosing "Refactor" for everything takes too long and costs too much. The architect's job is to evaluate each component independently and select the strategy that balances speed, cost, risk, and long-term value. This exercise simulates that decision process with a concrete WordPress stack and walks you through implementing the most common migration pattern: DMS database migration.

## The Challenge

You are leading the migration of a legacy WordPress application stack from on-premises to AWS. The current architecture consists of:

- **Web server:** Apache + WordPress on a Linux VM
- **Database:** MySQL 5.7 on a dedicated server
- **File storage:** NFS share for WordPress uploads (media files)
- **Monitoring:** Nagios for alerting, Munin for metrics

Apply the 6 Rs framework to determine the migration strategy for each component, then implement the database migration using DMS.

### Requirements

1. Assess each component using the 6 Rs decision tree and document your reasoning
2. Set up AWS Migration Hub to track migration progress
3. Deploy DMS infrastructure: replication instance, source endpoint (simulated MySQL on EC2), target endpoint (Aurora MySQL)
4. Create and run a DMS migration task with full-load plus CDC (change data capture)
5. Verify data integrity between source and target
6. Document the cutover strategy (blue/green vs trickle migration)

### Architecture: Before and After

```
BEFORE (On-Premises)                 AFTER (AWS)
+------------------+                 +---------------------------+
| Nagios/Munin     |    Repurchase   | CloudWatch + SNS          |
| (monitoring)     | ─────────────>  | (managed monitoring)      |
+------------------+                 +---------------------------+

+------------------+                 +---------------------------+
| Apache+WordPress |    Replatform   | ECS Fargate               |
| (web server VM)  | ─────────────>  | (containerized WordPress) |
+------------------+                 | behind ALB                |
                                     +---------------------------+

+------------------+                 +---------------------------+
| MySQL 5.7        |    Replatform   | Aurora MySQL              |
| (database server)| ─────────────>  | (managed, Multi-AZ)       |
+------------------+     via DMS     +---------------------------+

+------------------+                 +---------------------------+
| NFS share        |    Relocate     | S3 + CloudFront           |
| (media files)    | ─────────────>  | (object storage + CDN)    |
+------------------+                 +---------------------------+
```

### 6 Rs Decision Tree for Each Component

| Component | Strategy | Rationale |
|---|---|---|
| Web server | **Replatform** | Containerize WordPress into ECS Fargate. Eliminates server patching. Not a full refactor (still WordPress), but gains managed compute. |
| Database | **Replatform** | MySQL to Aurora MySQL via DMS. Wire-compatible, zero application changes. Gains Multi-AZ, automated backups, read replicas, serverless scaling. |
| Media files | **Relocate** | NFS to S3. WordPress plugins support S3 natively. Add CloudFront for edge caching. |
| Monitoring | **Repurchase** | Replace Nagios/Munin with CloudWatch + SNS. Different product, same capability. No custom code to migrate. |
| Legacy admin panel | **Retire** | Internal tool with 2 users, replaced by AWS Console. No value in migrating. |
| Compliance archive | **Retain** | Regulatory requirement to keep on-premises for 2 more years. Revisit after compliance period. |

## Hints

<details>
<summary>Hint 1: Setting Up Migration Hub</summary>

AWS Migration Hub provides a single place to track migration progress across multiple AWS services (DMS, Server Migration Service, CloudEndure). Enable it before starting migrations:

```bash
# Initialize Migration Hub in your home region
aws migrationhub-config create-home-region-control \
  --home-region us-east-1 \
  --target Type=ACCOUNT

# Verify
aws migrationhub-config describe-home-region-controls \
  --query "HomeRegionControls[0].HomeRegion"
```

Migration Hub is free -- you only pay for the migration tools themselves. It aggregates status from DMS tasks, application migration jobs, and manual updates into a single dashboard.

For discovery, the Application Discovery Service (ADS) would run agents on-premises to inventory servers, processes, and network dependencies. In this exercise we skip the agent and manually register the application in Migration Hub.

</details>

<details>
<summary>Hint 2: DMS Replication Instance Networking</summary>

The DMS replication instance acts as a bridge between source and target databases. It must have network connectivity to both. Common networking mistakes:

1. **Wrong VPC:** The replication instance must be in a VPC that can reach both source and target. If the source is "on-premises" (simulated by an EC2 MySQL instance), ensure the replication instance is in the same VPC or has routing to it.

2. **Security groups:** The replication instance needs outbound access to both databases. The source and target databases need inbound rules allowing the replication instance's security group.

3. **Subnet group:** DMS needs a replication subnet group spanning at least 2 AZs (similar to RDS):

```hcl
resource "aws_dms_replication_subnet_group" "this" {
  replication_subnet_group_id          = "dms-demo-subnet-group"
  replication_subnet_group_description = "DMS demo subnet group"
  subnet_ids = [
    aws_subnet.private_a.id,
    aws_subnet.private_b.id,
  ]
}
```

The replication instance class determines throughput. `dms.t3.micro` is sufficient for learning; production workloads typically need `dms.r5.large` or larger.

</details>

<details>
<summary>Hint 3: DMS Source and Target Endpoints</summary>

DMS endpoints define the connection parameters for source and target databases. Test connectivity before creating the migration task:

```hcl
resource "aws_dms_endpoint" "source" {
  endpoint_id   = "source-mysql"
  endpoint_type = "source"
  engine_name   = "mysql"
  server_name   = aws_instance.source_db.private_ip
  port          = 3306
  username      = "admin"
  password      = "SourcePass123!"
  database_name = "wordpress"
}

resource "aws_dms_endpoint" "target" {
  endpoint_id   = "target-aurora"
  endpoint_type = "target"
  engine_name   = "aurora"
  server_name   = aws_rds_cluster.aurora.endpoint
  port          = 3306
  username      = "admin"
  password      = "TargetPass123!"
  database_name = "wordpress"
}
```

Test endpoint connectivity:

```bash
aws dms test-connection \
  --replication-instance-arn "$REPLICATION_INSTANCE_ARN" \
  --endpoint-arn "$SOURCE_ENDPOINT_ARN"

aws dms test-connection \
  --replication-instance-arn "$REPLICATION_INSTANCE_ARN" \
  --endpoint-arn "$TARGET_ENDPOINT_ARN"
```

Both tests must return `status: successful` before creating the migration task.

</details>

<details>
<summary>Hint 4: DMS Migration Task Configuration</summary>

The migration task defines what data to migrate and how. For WordPress, use full-load plus CDC for minimal downtime:

```hcl
resource "aws_dms_replication_task" "this" {
  replication_task_id      = "wordpress-migration"
  replication_instance_arn = aws_dms_replication_instance.this.replication_instance_arn
  source_endpoint_arn      = aws_dms_endpoint.source.endpoint_arn
  target_endpoint_arn      = aws_dms_endpoint.target.endpoint_arn
  migration_type           = "full-load-and-cdc"

  table_mappings = jsonencode({
    rules = [{
      rule-type = "selection"
      rule-id   = "1"
      rule-name = "all-tables"
      object-locator = {
        schema-name = "wordpress"
        table-name  = "%"
      }
      rule-action = "include"
    }]
  })

  replication_task_settings = jsonencode({
    TargetMetadata = {
      TargetSchema         = ""
      SupportLobs          = true
      FullLobMode          = false
      LobChunkSize         = 64
      LimitedSizeLobMode   = true
      LobMaxSize           = 32768
    }
    FullLoadSettings = {
      TargetTablePrepMode = "DROP_AND_CREATE"
    }
  })
}
```

**Migration types explained:**

| Type | Description | Downtime | Use Case |
|---|---|---|---|
| `full-load` | One-time copy of all data | Full downtime during copy | Small databases, acceptable downtime window |
| `cdc` | Ongoing replication of changes only | Requires pre-loaded target | Already migrated, need to keep in sync |
| `full-load-and-cdc` | Full copy then ongoing replication | Near-zero (cutover only) | Production migrations, minimal downtime |

</details>

<details>
<summary>Hint 5: Cutover Strategy</summary>

After the full load completes and CDC is keeping the target in sync, you need a cutover strategy. Two main approaches:

**Blue/Green Cutover (recommended for this scenario):**
1. DMS full-load + CDC running, target is in sync
2. Stop writes to source (set WordPress to maintenance mode)
3. Wait for CDC to drain remaining changes (seconds)
4. Verify row counts match between source and target
5. Update WordPress `wp-config.php` to point to Aurora endpoint
6. Restart WordPress (or deploy new ECS tasks with Aurora endpoint)
7. Monitor for errors; DMS task can remain running for rollback window
8. After validation period, stop DMS task and decommission source

**Trickle Migration (for zero-downtime requirements):**
1. DMS full-load + CDC running
2. Deploy new application stack pointing to Aurora
3. Route 53 weighted routing: 10% to new stack, 90% to old
4. Gradually increase weight to new stack over days
5. Monitor error rates and performance at each step
6. Once 100% on new stack, decommission old

Verify data integrity after cutover:

```bash
# Compare row counts
mysql -h source -e "SELECT COUNT(*) FROM wordpress.wp_posts;"
mysql -h aurora-endpoint -e "SELECT COUNT(*) FROM wordpress.wp_posts;"

# Check DMS task statistics
aws dms describe-table-statistics \
  --replication-task-arn "$TASK_ARN" \
  --query "TableStatistics[*].{Table:TableName,Inserts:Inserts,Deletes:Deletes,Updates:Updates,FullLoadRows:FullLoadRows}" \
  --output table
```

</details>

## Spot the Bug

A team creates a DMS replication instance and migration task. The task stays in "starting" state for 10+ minutes and eventually fails:

```hcl
resource "aws_dms_replication_instance" "this" {
  replication_instance_id    = "dms-demo"
  replication_instance_class = "dms.t3.micro"
  allocated_storage          = 50

  # Replication instance in VPC-A
  replication_subnet_group_id = aws_dms_replication_subnet_group.vpc_a.id
  vpc_security_group_ids      = [aws_security_group.dms.id]
}

resource "aws_dms_endpoint" "source" {
  endpoint_id   = "source-mysql"
  endpoint_type = "source"
  engine_name   = "mysql"
  # Source MySQL is in VPC-B with no peering to VPC-A
  server_name   = "10.1.5.20"
  port          = 3306
  username      = "replication_user"
  password      = "ReplicaPass!"
  database_name = "wordpress"
}
```

<details>
<summary>Explain the bug</summary>

The DMS replication instance is deployed in **VPC-A**, but the source MySQL database is in **VPC-B** at IP `10.1.5.20`. There is no VPC peering connection, Transit Gateway, or other network path between the two VPCs. The replication instance cannot reach the source database, so the migration task stays in "starting" state indefinitely before timing out.

DMS does not produce a clear error for network unreachability during task startup -- it simply keeps trying to connect. This makes the issue difficult to diagnose without checking network connectivity first.

The fix has three options:

1. **VPC Peering:** Create a peering connection between VPC-A and VPC-B, update route tables in both VPCs, and ensure security groups allow traffic between the replication instance and the source database.

2. **Same VPC:** Deploy the replication instance in the same VPC as the source database (or a VPC that already has connectivity).

3. **Public endpoint:** If the source is accessible via a public IP, set `publicly_accessible = true` on the replication instance and use the public IP/hostname for the source endpoint. This is less secure but simpler for testing.

Always test endpoint connectivity before starting the migration task:

```bash
aws dms test-connection \
  --replication-instance-arn "$REPL_ARN" \
  --endpoint-arn "$SOURCE_ARN"

# Wait and check result
aws dms describe-connections \
  --filter Name=endpoint-arn,Values="$SOURCE_ARN" \
  --query "Connections[0].Status"
```

If the test returns `failed`, fix the networking before proceeding.

</details>

## Verify What You Learned

```bash
# Confirm DMS replication instance is available
aws dms describe-replication-instances \
  --query "ReplicationInstances[?ReplicationInstanceIdentifier=='dms-demo'].{Id:ReplicationInstanceIdentifier,Status:ReplicationInstanceStatus,Class:ReplicationInstanceClass}" \
  --output table
```

Expected: status `available`.

```bash
# Verify endpoint connectivity
aws dms describe-connections \
  --filter Name=replication-instance-arn,Values="$REPL_INSTANCE_ARN" \
  --query "Connections[*].{Endpoint:EndpointIdentifier,Status:Status}" \
  --output table
```

Expected: both source and target endpoints show `successful`.

```bash
# Check migration task status
aws dms describe-replication-tasks \
  --query "ReplicationTasks[?ReplicationTaskIdentifier=='wordpress-migration'].{Task:ReplicationTaskIdentifier,Status:Status,MigrationType:MigrationType,Progress:ReplicationTaskStats.FullLoadProgressPercent}" \
  --output table
```

Expected: status `running` with CDC after full load completes (100% progress).

```bash
# Verify data in Aurora target
aws rds describe-db-clusters \
  --db-cluster-identifier aurora-wordpress \
  --query "DBClusters[0].{Endpoint:Endpoint,Status:Status,Engine:Engine}" \
  --output table
```

Expected: Aurora MySQL cluster status `available`.

## Cleanup

Destroy all resources to stop incurring charges. Stop the DMS task first (it must be stopped before it can be deleted):

```bash
# Stop DMS task
aws dms stop-replication-task --replication-task-arn "$TASK_ARN"
# Wait for stopped state
aws dms wait replication-task-stopped --filters Name=replication-task-arn,Values="$TASK_ARN"

# Destroy all Terraform-managed resources
terraform destroy -auto-approve
```

Verify cleanup:

```bash
aws dms describe-replication-instances --query "ReplicationInstances[?ReplicationInstanceIdentifier=='dms-demo']"
aws rds describe-db-clusters --query "DBClusters[?DBClusterIdentifier=='aurora-wordpress']" 2>&1 || echo "Cluster deleted"
```

Expected: empty results.

## What's Next

You have designed a migration plan using the 6 Rs framework and implemented database migration with DMS. In the next exercise, you will build a **multi-tier highly available architecture** and analyze the cost trade-offs of each architectural decision.

## Summary

- The **6 Rs** (Rehost, Replatform, Refactor, Repurchase, Retain, Retire) provide a framework for evaluating each application component independently
- **Replatform** is often the best balance of effort and benefit -- you gain managed services without rewriting the application
- **DMS** handles database migration with three modes: full-load, CDC, and full-load-and-cdc
- **full-load-and-cdc** is the standard production pattern: copy everything, then keep replicating changes until cutover
- The DMS replication instance must have **network connectivity** to both source and target databases
- Always **test endpoint connectivity** before creating a migration task
- **Cutover strategy** depends on downtime tolerance: blue/green for minimal downtime, trickle migration for zero downtime
- **Migration Hub** provides centralized tracking across multiple migration tools at no additional cost
- **Schema Conversion Tool (SCT)** assesses schema compatibility when migrating between different engines (e.g., Oracle to PostgreSQL)

## Reference

- [AWS Database Migration Service](https://docs.aws.amazon.com/dms/latest/userguide/Welcome.html)
- [AWS Migration Hub](https://docs.aws.amazon.com/migrationhub/latest/ug/whatishub.html)
- [6 Strategies for Migrating Applications to the Cloud](https://aws.amazon.com/blogs/enterprise-strategy/6-strategies-for-migrating-applications-to-the-cloud/)
- [Terraform aws_dms_replication_instance Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_replication_instance)
- [Terraform aws_dms_replication_task Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/dms_replication_task)

## Additional Resources

- [AWS Schema Conversion Tool](https://docs.aws.amazon.com/SchemaConversionTool/latest/userguide/CHAP_Welcome.html) -- automated schema assessment and conversion for heterogeneous migrations
- [DMS Best Practices](https://docs.aws.amazon.com/dms/latest/userguide/CHAP_BestPractices.html) -- replication instance sizing, network optimization, LOB handling
- [Migration Readiness Assessment](https://aws.amazon.com/migration-acceleration-program/) -- AWS Migration Acceleration Program for enterprise migrations
- [CloudEndure Migration](https://docs.aws.amazon.com/prescriptive-guidance/latest/migration-rehosting-cloudendure/welcome.html) -- automated lift-and-shift for server migrations (Rehost strategy)

<details>
<summary>Full Solution</summary>

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
  default     = "dms-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" { state = "available" }

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = var.project_name }
}

resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags = { Name = "${var.project_name}-private-a" }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]
  tags = { Name = "${var.project_name}-private-b" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.10.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-public" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.project_name }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
resource "aws_security_group" "mysql" {
  name_prefix = "${var.project_name}-mysql-"
  vpc_id      = aws_vpc.this.id
  description = "Source MySQL"

  ingress {
    from_port   = 3306
    to_port     = 3306
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
    description = "MySQL from VPC"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "dms" {
  name_prefix = "${var.project_name}-repl-"
  vpc_id      = aws_vpc.this.id
  description = "DMS replication instance"

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "aurora" {
  name_prefix = "${var.project_name}-aurora-"
  vpc_id      = aws_vpc.this.id
  description = "Aurora MySQL target"

  ingress {
    from_port       = 3306
    to_port         = 3306
    protocol        = "tcp"
    security_groups = [aws_security_group.dms.id]
    description     = "MySQL from DMS"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `compute.tf`

```hcl
# Source: MySQL on EC2 (simulates on-premises database)
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }
}

resource "aws_instance" "source_db" {
  ami                    = data.aws_ami.al2023.value
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.mysql.id]

  user_data = <<-EOF
    #!/bin/bash
    dnf install -y mariadb105-server
    systemctl enable --now mariadb

    mysql -e "CREATE DATABASE wordpress;"
    mysql -e "CREATE USER 'admin'@'%' IDENTIFIED BY 'SourcePass123!';"
    mysql -e "GRANT ALL ON wordpress.* TO 'admin'@'%';"
    mysql -e "GRANT REPLICATION CLIENT, REPLICATION SLAVE ON *.* TO 'admin'@'%';"
    mysql -e "FLUSH PRIVILEGES;"

    mysql wordpress -e "
      CREATE TABLE wp_posts (
        ID BIGINT AUTO_INCREMENT PRIMARY KEY,
        post_title VARCHAR(255),
        post_content TEXT,
        post_date DATETIME DEFAULT CURRENT_TIMESTAMP
      );
      INSERT INTO wp_posts (post_title, post_content) VALUES
        ('Hello World', 'Welcome to WordPress on-premises'),
        ('Migration Plan', 'Moving to Aurora MySQL via DMS');
    "
  EOF

  tags = { Name = "${var.project_name}-source-mysql" }
}
```

### `database.tf`

```hcl
# Target: Aurora MySQL Serverless v2
resource "aws_db_subnet_group" "aurora" {
  name       = "${var.project_name}-aurora"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_rds_cluster" "aurora" {
  cluster_identifier     = "aurora-wordpress"
  engine                 = "aurora-mysql"
  engine_mode            = "provisioned"
  engine_version         = "8.0.mysql_aurora.3.05.2"
  master_username        = "admin"
  master_password        = "TargetPass123!"
  database_name          = "wordpress"
  db_subnet_group_name   = aws_db_subnet_group.aurora.name
  vpc_security_group_ids = [aws_security_group.aurora.id]
  skip_final_snapshot    = true

  serverlessv2_scaling_configuration {
    min_capacity = 0.5
    max_capacity = 2.0
  }
}

resource "aws_rds_cluster_instance" "aurora" {
  identifier         = "aurora-wordpress-instance-1"
  cluster_identifier = aws_rds_cluster.aurora.id
  instance_class     = "db.serverless"
  engine             = aws_rds_cluster.aurora.engine
  engine_version     = aws_rds_cluster.aurora.engine_version
}
```

### `dms.tf`

```hcl
# DMS Replication Instance
resource "aws_dms_replication_subnet_group" "this" {
  replication_subnet_group_id          = "${var.project_name}-subnet-group"
  replication_subnet_group_description = "DMS demo"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}

resource "aws_dms_replication_instance" "this" {
  replication_instance_id    = var.project_name
  replication_instance_class = "dms.t3.micro"
  allocated_storage          = 50
  replication_subnet_group_id = aws_dms_replication_subnet_group.this.id
  vpc_security_group_ids      = [aws_security_group.dms.id]
  publicly_accessible         = false
  apply_immediately           = true

  tags = { Name = var.project_name }
}

# DMS Endpoints
resource "aws_dms_endpoint" "source" {
  endpoint_id   = "source-mysql"
  endpoint_type = "source"
  engine_name   = "mysql"
  server_name   = aws_instance.source_db.private_ip
  port          = 3306
  username      = "admin"
  password      = "SourcePass123!"
  database_name = "wordpress"
}

resource "aws_dms_endpoint" "target" {
  endpoint_id   = "target-aurora"
  endpoint_type = "target"
  engine_name   = "aurora"
  server_name   = aws_rds_cluster.aurora.endpoint
  port          = 3306
  username      = "admin"
  password      = "TargetPass123!"
  database_name = "wordpress"
}

# DMS Migration Task
resource "aws_dms_replication_task" "this" {
  replication_task_id      = "wordpress-migration"
  replication_instance_arn = aws_dms_replication_instance.this.replication_instance_arn
  source_endpoint_arn      = aws_dms_endpoint.source.endpoint_arn
  target_endpoint_arn      = aws_dms_endpoint.target.endpoint_arn
  migration_type           = "full-load-and-cdc"
  start_replication_task   = true

  table_mappings = jsonencode({
    rules = [{
      rule-type = "selection"
      rule-id   = "1"
      rule-name = "all-wordpress-tables"
      object-locator = {
        schema-name = "wordpress"
        table-name  = "%"
      }
      rule-action = "include"
    }]
  })

  replication_task_settings = jsonencode({
    TargetMetadata = {
      TargetSchema       = ""
      SupportLobs        = true
      LimitedSizeLobMode = true
      LobMaxSize         = 32768
    }
    FullLoadSettings = {
      TargetTablePrepMode = "DROP_AND_CREATE"
    }
    Logging = {
      EnableLogging = true
    }
  })

  tags = { Name = "wordpress-migration" }
}
```

### `outputs.tf`

```hcl
output "source_mysql_ip" {
  value = aws_instance.source_db.private_ip
}

output "aurora_endpoint" {
  value = aws_rds_cluster.aurora.endpoint
}

output "aurora_reader_endpoint" {
  value = aws_rds_cluster.aurora.reader_endpoint
}

output "dms_replication_instance_arn" {
  value = aws_dms_replication_instance.this.replication_instance_arn
}

output "dms_task_arn" {
  value = aws_dms_replication_task.this.replication_task_arn
}
```

</details>
