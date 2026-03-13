# 1. RDS Multi-AZ with Read Replicas

<!--
difficulty: basic
concepts: [rds-multi-az, synchronous-replication, read-replica, async-replication, dns-failover, rds-monitoring]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.08/hr
-->

> **AWS Cost Warning:** RDS db.t3.micro with Multi-AZ costs ~$0.034/hr (2x single-AZ). A read replica adds another ~$0.017/hr. Total ~$0.08/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Basic understanding of relational databases

## Learning Objectives

After completing this exercise, you will be able to:

- **Identify** the differences between Multi-AZ standby (synchronous replication) and read replicas (asynchronous replication)
- **Construct** an RDS MySQL instance with Multi-AZ enabled and a same-region read replica using Terraform
- **Verify** that DNS-based failover works by forcing a reboot and observing endpoint stability
- **Explain** why the Multi-AZ standby instance cannot serve read traffic and exists only for automatic failover
- **Describe** the cost implications of enabling Multi-AZ (doubles compute cost) versus adding read replicas
- **Compare** the use cases for Multi-AZ (high availability) versus read replicas (read scaling and cross-region DR)
- **Distinguish** synchronous replication (zero data loss on failover) from asynchronous replication (potential replica lag)

## Why RDS Multi-AZ with Read Replicas

RDS Multi-AZ and read replicas solve fundamentally different problems, and the SAA-C03 exam tests whether you can choose the right tool for each scenario. Multi-AZ is a high-availability mechanism: AWS maintains a synchronous standby copy of your database in a different Availability Zone. If the primary fails -- hardware fault, AZ outage, or planned maintenance -- RDS automatically updates the DNS endpoint to point to the standby. Your application reconnects without changing connection strings. The standby is not accessible for reads or writes during normal operation; it exists solely to minimize downtime. This is the answer when the exam asks about "automatic failover" or "minimal data loss."

Read replicas solve a different problem: scaling read-heavy workloads. They use asynchronous replication, which means there is a small delay (replica lag) between the primary and the replica. Applications can direct read queries to the replica endpoint, reducing load on the primary. Read replicas can also be created in a different region for disaster recovery -- you can promote a cross-region replica to a standalone database if the primary region fails entirely. On the exam, choosing between Multi-AZ and read replicas comes down to: "Do you need automatic failover with zero data loss?" (Multi-AZ) or "Do you need to scale reads or prepare for regional DR?" (read replica). Many production architectures use both simultaneously.

## Step 1 -- Create the Project Files

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
  default     = "rds-multi-az-demo"
}
```

### `vpc.tf`

```hcl
# ------------------------------------------------------------------
# Fetch available AZs. Multi-AZ RDS requires subnets in at least
# two different Availability Zones for the DB subnet group.
# ------------------------------------------------------------------
data "aws_availability_zones" "available" {
  state = "available"
}

# ------------------------------------------------------------------
# VPC: isolated network for the RDS instances. RDS does not require
# a public IP for this exercise -- we access it via CLI commands
# that use the AWS API, not direct database connections.
# ------------------------------------------------------------------
resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true

  tags = {
    Name = "${var.project_name}"
  }
}

# ------------------------------------------------------------------
# Two private subnets in different AZs. The DB subnet group requires
# this for Multi-AZ placement -- one subnet hosts the primary, the
# other hosts the standby.
# ------------------------------------------------------------------
resource "aws_subnet" "private_a" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]

  tags = {
    Name = "${var.project_name}-private-a"
  }
}

resource "aws_subnet" "private_b" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.2.0/24"
  availability_zone = data.aws_availability_zones.available.names[1]

  tags = {
    Name = "${var.project_name}-private-b"
  }
}
```

### `security.tf`

```hcl
# ------------------------------------------------------------------
# Security group: controls network access to the RDS instances.
# For this exercise we allow MySQL (3306) from within the VPC only.
# In production you would restrict this to specific application
# security groups.
# ------------------------------------------------------------------
resource "aws_security_group" "rds" {
  name_prefix = "${var.project_name}-"
  vpc_id      = aws_vpc.this.id
  description = "Allow MySQL access within VPC"

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
    description = "Allow all outbound"
  }

  tags = {
    Name = "${var.project_name}-sg"
  }
}
```

### `database.tf`

```hcl
# ------------------------------------------------------------------
# DB Subnet Group: tells RDS which subnets to place instances in.
# Multi-AZ requires subnets in 2+ AZs. Without this, terraform
# apply fails with "DBSubnetGroupDoesNotCoverEnoughAZs".
# ------------------------------------------------------------------
resource "aws_db_subnet_group" "this" {
  name       = var.project_name
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]

  tags = {
    Name = var.project_name
  }
}

# ------------------------------------------------------------------
# RDS MySQL instance with Multi-AZ enabled.
#
# Key architect decisions:
# - multi_az = true: creates synchronous standby in another AZ.
#   AWS manages replication, failover, and DNS updates automatically.
#   This DOUBLES the compute cost but provides automatic failover
#   with near-zero data loss (synchronous replication).
# - skip_final_snapshot = true: for demo only. In production, always
#   take a final snapshot before deletion.
# - db.t3.micro: smallest instance class, sufficient for learning.
#   In production, size based on expected connections and query load.
# ------------------------------------------------------------------
resource "aws_db_instance" "primary" {
  identifier     = var.project_name
  engine         = "mysql"
  engine_version = "8.0"
  instance_class = "db.t3.micro"

  allocated_storage = 20
  storage_type      = "gp3"

  db_name  = "demodb"
  username = "admin"
  password = "ChangeMe123!"

  multi_az               = true
  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  # Monitoring: enable enhanced monitoring at 60-second intervals.
  # This gives you OS-level metrics (CPU, memory, swap, I/O) in
  # addition to the standard CloudWatch RDS metrics.
  monitoring_interval = 60
  monitoring_role_arn = aws_iam_role.rds_monitoring.arn

  # Demo settings -- do NOT use these in production
  skip_final_snapshot = true
  apply_immediately   = true

  tags = {
    Name        = var.project_name
    Environment = "learning"
  }
}

# ------------------------------------------------------------------
# Read Replica: asynchronous copy of the primary for read scaling.
#
# Architect trade-offs:
# - Asynchronous replication means replica lag is possible (typically
#   milliseconds, but can grow under heavy write load).
# - The replica has its own endpoint -- applications must be
#   designed to split reads and writes to different endpoints.
# - Can be promoted to standalone DB (breaks replication permanently).
# - Can be created cross-region for disaster recovery.
# - Costs the same as another RDS instance of the same class.
# - Does NOT provide automatic failover like Multi-AZ does.
# ------------------------------------------------------------------
resource "aws_db_instance" "read_replica" {
  identifier          = "${var.project_name}-replica"
  replicate_source_db = aws_db_instance.primary.identifier
  instance_class      = "db.t3.micro"

  # The replica inherits engine, storage, and subnet group from
  # the source. You can override instance_class to use a different
  # size -- useful when your read workload needs more resources
  # than the primary.

  # Do NOT set multi_az on the replica for this exercise.
  # A read replica CAN be Multi-AZ (for its own HA), but that
  # would add cost without learning value here.
  multi_az = false

  # Replica does not need its own credentials -- it replicates
  # data from the primary automatically.

  skip_final_snapshot = true
  apply_immediately   = true

  tags = {
    Name        = "${var.project_name}-replica"
    Environment = "learning"
  }
}
```

### `iam.tf`

```hcl
# ------------------------------------------------------------------
# IAM role for RDS Enhanced Monitoring. RDS needs permission to
# publish OS-level metrics to CloudWatch Logs.
# ------------------------------------------------------------------
data "aws_iam_policy_document" "rds_monitoring_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["monitoring.rds.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "rds_monitoring" {
  name               = "${var.project_name}-monitoring"
  assume_role_policy = data.aws_iam_policy_document.rds_monitoring_assume.json
}

resource "aws_iam_role_policy_attachment" "rds_monitoring" {
  role       = aws_iam_role.rds_monitoring.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AmazonRDSEnhancedMonitoringRole"
}
```

### `outputs.tf`

```hcl
output "primary_endpoint" {
  description = "Primary RDS endpoint (DNS-based, survives failover)"
  value       = aws_db_instance.primary.endpoint
}

output "primary_az" {
  description = "Current AZ of the primary instance"
  value       = aws_db_instance.primary.availability_zone
}

output "primary_multi_az" {
  description = "Whether Multi-AZ is enabled on the primary"
  value       = aws_db_instance.primary.multi_az
}

output "replica_endpoint" {
  description = "Read replica endpoint (separate DNS name for read traffic)"
  value       = aws_db_instance.read_replica.endpoint
}

output "replica_az" {
  description = "AZ of the read replica"
  value       = aws_db_instance.read_replica.availability_zone
}
```

## Step 2 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

RDS instances take 5-10 minutes to create. Terraform will create the VPC, subnets, security group, DB subnet group, IAM role, primary Multi-AZ instance, and read replica.

### Intermediate Verification

Confirm the expected resources:

```bash
terraform state list
```

You should see entries including:

```
aws_db_instance.primary
aws_db_instance.read_replica
aws_db_subnet_group.this
aws_iam_role.rds_monitoring
aws_security_group.rds
aws_subnet.private_a
aws_subnet.private_b
aws_vpc.this
```

## Step 3 -- Compare Multi-AZ Standby vs Read Replica

Examine the primary instance details to understand Multi-AZ configuration:

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo \
  --query 'DBInstances[0].{Endpoint:Endpoint.Address,AZ:AvailabilityZone,MultiAZ:MultiAZ,SecondaryAZ:SecondaryAvailabilityZone,Status:DBInstanceStatus}' \
  --output table
```

Note the `SecondaryAvailabilityZone` field -- this is where the standby lives. The standby has no separate endpoint because it cannot serve traffic.

Now compare with the read replica, which has its own endpoint:

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo-replica \
  --query 'DBInstances[0].{Endpoint:Endpoint.Address,AZ:AvailabilityZone,Status:DBInstanceStatus,ReplicaLag:StatusInfos}' \
  --output table
```

### Decision Table: Multi-AZ Standby vs Read Replica

| Feature | Multi-AZ Standby | Read Replica |
|---------|------------------|--------------|
| Replication | Synchronous | Asynchronous |
| Serves read traffic | No (standby only) | Yes |
| Automatic failover | Yes (DNS update, 60-120s) | No (manual promotion) |
| Cross-region | No (same region only) | Yes |
| Data loss on failover | Near-zero | Possible (replica lag) |
| Separate endpoint | No (same DNS) | Yes (own endpoint) |
| Cost | 2x primary compute | Same as additional instance |
| Use case | High availability | Read scaling, DR |

## Step 4 -- Test Failover

Force a failover by rebooting the primary with the `--force-failover` flag:

```bash
aws rds reboot-db-instance \
  --db-instance-identifier rds-multi-az-demo \
  --force-failover
```

Monitor the failover event:

```bash
aws rds describe-events \
  --source-identifier rds-multi-az-demo \
  --source-type db-instance \
  --duration 10 \
  --query 'Events[*].{Time:Date,Message:Message}' \
  --output table
```

After the failover completes (60-120 seconds), verify the primary endpoint is unchanged but the AZ has swapped:

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo \
  --query 'DBInstances[0].{Endpoint:Endpoint.Address,AZ:AvailabilityZone,SecondaryAZ:SecondaryAvailabilityZone}' \
  --output table
```

The `AvailabilityZone` and `SecondaryAvailabilityZone` values should be swapped compared to before the failover. The endpoint address remains the same -- this is the DNS-based failover that makes Multi-AZ transparent to applications.

## Common Mistakes

### 1. Thinking the Multi-AZ standby can serve read traffic

**Wrong approach:** Directing read queries to the Multi-AZ standby to offload the primary.

**What happens:** There is no endpoint for the standby instance. The standby is completely invisible to your application. It receives synchronous writes from the primary and exists solely to take over if the primary fails. You cannot connect to it, query it, or even see its IP address through the AWS Console or API.

**Fix:** Use a read replica for read scaling. The read replica has its own endpoint and actively serves read queries. Multi-AZ and read replicas solve different problems and should be used together when you need both high availability and read scaling.

### 2. Not creating a DB subnet group spanning 2+ AZs

**Wrong approach:** Creating a DB subnet group with subnets in only one AZ:

```hcl
resource "aws_db_subnet_group" "this" {
  name       = "single-az-group"
  subnet_ids = [aws_subnet.private_a.id]  # Only one AZ
}
```

**What happens:** `terraform apply` fails with `DBSubnetGroupDoesNotCoverEnoughAZs: DB Subnet Group doesn't meet availability zone coverage requirement. Please add subnets to cover at least 2 availability zones.` Multi-AZ needs two AZs so the standby can be placed in a different AZ from the primary.

**Fix:** Always include subnets from at least two AZs in your DB subnet group:

```hcl
resource "aws_db_subnet_group" "this" {
  name       = "multi-az-group"
  subnet_ids = [aws_subnet.private_a.id, aws_subnet.private_b.id]
}
```

### 3. Placing a read replica in the same AZ as the primary

**Wrong approach:** Not considering AZ placement for the replica, which may default to the same AZ as the primary.

**What happens:** The replica works correctly, but you lose the benefit of geographic distribution. If that AZ has an outage, both the primary and the replica are affected. While Multi-AZ handles primary failover, your read traffic also goes down.

**Fix:** Explicitly set the `availability_zone` on the replica to a different AZ, or let AWS distribute it automatically. For disaster recovery, consider a cross-region replica instead:

```hcl
resource "aws_db_instance" "read_replica" {
  availability_zone   = data.aws_availability_zones.available.names[1]
  replicate_source_db = aws_db_instance.primary.identifier
  # ...
}
```

## Verify What You Learned

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo \
  --query "DBInstances[0].MultiAZ" \
  --output text
```

Expected: `True`

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo \
  --query "DBInstances[0].Endpoint.Address" \
  --output text
```

Expected: an endpoint like `rds-multi-az-demo.xxxxxxxxxxxx.us-east-1.rds.amazonaws.com` (this DNS name does not change after failover).

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo-replica \
  --query "DBInstances[0].ReadReplicaSourceDBInstanceIdentifier" \
  --output text
```

Expected: `rds-multi-az-demo` (confirming it replicates from the primary).

```bash
aws rds describe-db-instances \
  --db-instance-identifier rds-multi-az-demo-replica \
  --query "DBInstances[0].Endpoint.Address" \
  --output text
```

Expected: a different endpoint like `rds-multi-az-demo-replica.xxxxxxxxxxxx.us-east-1.rds.amazonaws.com` (the replica has its own DNS name).

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You deployed a highly available database with a read scaling layer. In the next exercise, you will build **two types of load balancers** -- an Application Load Balancer with path-based routing and a Network Load Balancer with TCP passthrough -- and learn how to choose between Layer 7 and Layer 4 load balancing based on your workload requirements.

## Summary

- **Multi-AZ** creates a synchronous standby in a different AZ for automatic failover with near-zero data loss
- The Multi-AZ **standby cannot serve read traffic** -- it is invisible to applications and exists only for failover
- **Read replicas** use asynchronous replication and have their own endpoint for serving read queries
- Multi-AZ failover is **DNS-based** -- the endpoint address does not change, so applications reconnect automatically
- Read replicas can be **cross-region** for disaster recovery; Multi-AZ standby is same-region only
- Multi-AZ **doubles compute cost** (you pay for the standby instance); each read replica costs the same as another instance
- A DB subnet group must span **at least 2 AZs** for Multi-AZ to work
- Production architectures commonly use **both** Multi-AZ (for HA) and read replicas (for read scaling) together

## Reference

- [Amazon RDS Multi-AZ Deployments](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.MultiAZ.html)
- [Amazon RDS Read Replicas](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_ReadRepl.html)
- [Terraform aws_db_instance Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_instance)
- [Terraform aws_db_subnet_group Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_subnet_group)

## Additional Resources

- [RDS Multi-AZ Failover Process](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.MultiAZSingleStandby.html) -- detailed explanation of failover timing, DNS propagation, and conditions that trigger automatic failover
- [Monitoring Read Replica Lag](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_ReadRepl.html#USER_ReadRepl.Monitoring) -- how to monitor `ReplicaLag` in CloudWatch and set alarms for replication delay
- [RDS Pricing](https://aws.amazon.com/rds/pricing/) -- cost breakdown for Multi-AZ, storage, and data transfer across regions
- [Best Practices for Amazon RDS](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/CHAP_BestPractices.html) -- instance sizing, parameter groups, and backup strategies
