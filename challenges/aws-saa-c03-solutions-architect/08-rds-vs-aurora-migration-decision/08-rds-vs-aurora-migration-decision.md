# RDS vs Aurora: Migration Decision Framework

<!--
difficulty: intermediate
concepts: [rds-postgresql, aurora-postgresql, storage-model, read-replica-lag, failover-time, performance-insights, pgbench]
tools: [terraform, aws-cli]
estimated_time: 60m
bloom_level: design, justify, implement
prerequisites: [none]
aws_cost: ~$0.20/hr
-->

> **AWS Cost Warning:** RDS db.t3.micro (~$0.017/hr) + Aurora db.t3.medium (~$0.082/hr) running simultaneously. Total ~$0.20/hr. Destroy resources promptly after completing the exercise to avoid unexpected charges.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC with at least 2 AZs | `aws ec2 describe-availability-zones --query 'AvailabilityZones[*].ZoneName'` |
| PostgreSQL client (`psql`) installed | `psql --version` |
| Familiarity with basic SQL commands | N/A |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Design** a database architecture decision between RDS and Aurora based on workload characteristics.
2. **Justify** Aurora's higher cost with measurable benefits in replication lag, failover time, and storage durability.
3. **Implement** both RDS PostgreSQL and Aurora PostgreSQL side by side with identical schemas for comparison.
4. **Compare** storage models, read replica behavior, and Performance Insights metrics across both engines.
5. **Evaluate** the break-even cost point where Aurora's per-hour premium is offset by operational savings.

---

## Why This Matters

"Should we use RDS or Aurora?" is one of the most common architecture decisions in AWS, and the SAA-C03 tests it heavily. On the surface, both run the same PostgreSQL engine and accept the same SQL queries. The difference is underneath: RDS uses a traditional single-EBS-volume architecture, while Aurora uses a distributed storage layer that replicates data six ways across three Availability Zones automatically. This architectural difference creates measurable differences in replication lag (seconds vs milliseconds), failover time (60-120s vs ~30s), and storage durability. The exam expects you to know these numbers and apply them to scenarios.

The cost dimension makes this a genuine architecture trade-off rather than "always pick Aurora." Aurora's compute costs more per hour than equivalent RDS instances, and Aurora's I/O-based storage pricing (pay per million I/O requests) can be surprising for write-heavy workloads. The exam presents scenarios where RDS is the correct answer -- a small, single-AZ development database that does not need six-copy replication -- and scenarios where Aurora is clearly better -- a production read-heavy workload with global read replicas. This exercise forces you to deploy both, run the same benchmark, compare the numbers, and build your own decision framework instead of memorizing someone else's.

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
  default     = "saa-ex08"
}

variable "db_password" {
  type      = string
  sensitive = true
  default   = "LabPassword2026!"
}
```

### `main.tf`

```hcl
# ---------- Data Sources ----------

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

data "aws_ami" "amazon_linux" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "db" {
  name   = "${var.project_name}-db-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port       = 5432
    to_port         = 5432
    protocol        = "tcp"
    security_groups = [aws_security_group.bastion.id]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

resource "aws_security_group" "bastion" {
  name   = "${var.project_name}-bastion-sg"
  vpc_id = data.aws_vpc.default.id

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
# ---------- Bastion Host (for pgbench) ----------

resource "aws_iam_role" "bastion" {
  name = "${var.project_name}-bastion-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "bastion_ssm" {
  role       = aws_iam_role.bastion.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "bastion" {
  name = "${var.project_name}-bastion-profile"
  role = aws_iam_role.bastion.name
}

resource "aws_instance" "bastion" {
  ami                    = data.aws_ami.amazon_linux.id
  instance_type          = "t3.micro"
  iam_instance_profile   = aws_iam_instance_profile.bastion.name
  vpc_security_group_ids = [aws_security_group.bastion.id]
  subnet_id              = data.aws_subnets.default.ids[0]

  user_data = base64encode(<<-EOF
    #!/bin/bash
    yum install -y postgresql16
  EOF
  )

  tags = {
    Name = "${var.project_name}-bastion"
  }
}
```

### `database.tf`

```hcl
# ---------- Networking ----------

resource "aws_db_subnet_group" "this" {
  name       = "${var.project_name}-db-subnet-group"
  subnet_ids = data.aws_subnets.default.ids

  tags = {
    Name = "${var.project_name}-db-subnet-group"
  }
}

# ---------- RDS PostgreSQL ----------

resource "aws_db_instance" "rds" {
  identifier     = "${var.project_name}-rds-pg"
  engine         = "postgres"
  engine_version = "16.4"
  instance_class = "db.t3.micro"

  allocated_storage     = 20
  max_allocated_storage = 50
  storage_type          = "gp3"

  db_name  = "benchmark"
  username = "labadmin"
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]

  multi_az            = false
  publicly_accessible = false
  skip_final_snapshot = true

  performance_insights_enabled = true

  tags = {
    Name = "${var.project_name}-rds"
  }
}

# ============================================================
# TODO 1: Create Aurora Cluster and Primary Instance
# ============================================================
# Deploy an Aurora PostgreSQL cluster with one writer instance
# in the same VPC as the RDS instance above.
#
# Requirements:
#   a) Resource: aws_rds_cluster
#      - cluster_identifier = "${var.project_name}-aurora-pg"
#      - engine = "aurora-postgresql"
#      - engine_version = "16.4"
#      - database_name = "benchmark"
#      - master_username = "labadmin"
#      - master_password = var.db_password
#      - db_subnet_group_name = same subnet group
#      - vpc_security_group_ids = same security group
#      - skip_final_snapshot = true
#
#   b) Resource: aws_rds_cluster_instance (writer)
#      - identifier = "${var.project_name}-aurora-pg-writer"
#      - cluster_identifier = cluster ID
#      - instance_class = "db.t3.medium"
#        (db.t3.micro is NOT available for Aurora in many regions)
#      - engine = "aurora-postgresql"
#      - performance_insights_enabled = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/rds_cluster
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/rds_cluster_instance
# ============================================================


# ============================================================
# TODO 2: Create Aurora Read Replica
# ============================================================
# Add a second instance to the Aurora cluster as a read replica.
# In Aurora, read replicas share the same storage volume as the
# writer -- they do NOT replicate data like RDS read replicas.
#
# Requirements:
#   - Resource: aws_rds_cluster_instance
#   - identifier = "${var.project_name}-aurora-pg-reader"
#   - cluster_identifier = same Aurora cluster
#   - instance_class = "db.t3.medium"
#   - engine = "aurora-postgresql"
#   - performance_insights_enabled = true
#
# Key insight: Aurora replicas typically have < 10ms replication
# lag because they read from the same distributed storage layer.
# RDS read replicas use asynchronous streaming replication with
# lag typically in the seconds range.
#
# Docs: https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.Replication.html
# ============================================================


# ============================================================
# TODO 3: Enable Performance Insights on Both Engines
# ============================================================
# Performance Insights is already enabled on the RDS instance
# above (performance_insights_enabled = true).
#
# Requirements:
#   - Verify Performance Insights is set on Aurora instances (TODO 1, 2)
#   - After deployment, compare Performance Insights dashboards:
#
#   a) Open Performance Insights for the RDS instance:
#      aws pi get-resource-metrics \
#        --service-type RDS \
#        --identifier <RDS_RESOURCE_ID> \
#        --metric-queries '[{"Metric": "db.load.avg"}]' \
#        --start-time $(date -u -d '-1 hour' +%Y-%m-%dT%H:%M:%SZ) \
#        --end-time $(date -u +%Y-%m-%dT%H:%M:%SZ) \
#        --period-in-seconds 60
#
#   b) Repeat for Aurora writer instance
#
#   c) Compare db.load.avg (active sessions) during pgbench
#
# Docs: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.html
# ============================================================


# ============================================================
# TODO 4: Run pgbench on Both Databases
# ============================================================
# This TODO is CLI-based. Connect to the bastion via SSM and
# run identical pgbench workloads against both databases.
#
# Requirements:
#   a) Connect to bastion:
#      aws ssm start-session --target <BASTION_INSTANCE_ID>
#
#   b) Initialize pgbench on RDS:
#      pgbench -i -s 10 \
#        -h <RDS_ENDPOINT> -U labadmin -d benchmark
#
#   c) Run pgbench on RDS (60 second test, 4 clients):
#      pgbench -c 4 -j 2 -T 60 \
#        -h <RDS_ENDPOINT> -U labadmin -d benchmark
#
#   d) Initialize pgbench on Aurora:
#      pgbench -i -s 10 \
#        -h <AURORA_CLUSTER_ENDPOINT> -U labadmin -d benchmark
#
#   e) Run pgbench on Aurora (same parameters):
#      pgbench -c 4 -j 2 -T 60 \
#        -h <AURORA_CLUSTER_ENDPOINT> -U labadmin -d benchmark
#
#   f) Record TPS (transactions per second) for each engine
#
# Expected results: Aurora should show higher TPS due to its
# optimized storage layer, especially for write-heavy workloads.
# The difference is more pronounced at larger scale factors.
#
# Docs: https://www.postgresql.org/docs/current/pgbench.html
# ============================================================


# ============================================================
# TODO 5: Compare Replication Lag
# ============================================================
# This TODO is CLI-based. Measure replication lag on both
# engines while pgbench is running.
#
# Requirements:
#   a) For Aurora (via SQL on the reader instance):
#      psql -h <AURORA_READER_ENDPOINT> -U labadmin -d benchmark \
#        -c "SELECT server_id, session_id, replica_lag_in_msec
#            FROM aurora_replica_status();"
#
#   b) For RDS read replica (if you created one -- optional):
#      aws rds describe-db-instances \
#        --db-instance-identifier <REPLICA_ID> \
#        --query 'DBInstances[0].StatusInfos'
#
#   c) Alternative via CloudWatch:
#      aws cloudwatch get-metric-statistics \
#        --namespace AWS/RDS \
#        --metric-name AuroraReplicaLag \
#        --dimensions Name=DBInstanceIdentifier,Value=<AURORA_READER_ID> \
#        --start-time $(date -u -d '-10 minutes' +%Y-%m-%dT%H:%M:%SZ) \
#        --end-time $(date -u +%Y-%m-%dT%H:%M:%SZ) \
#        --period 60 \
#        --statistics Average
#
# Expected results:
#   - Aurora replica lag: < 20ms (typically single-digit ms)
#   - RDS replica lag: 100ms-several seconds under load
#
# Docs: https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.AuroraMonitoring.Metrics.html
# ============================================================
```

### `outputs.tf`

```hcl
output "rds_endpoint" {
  value = aws_db_instance.rds.endpoint
}

output "rds_resource_id" {
  value = aws_db_instance.rds.resource_id
}

output "bastion_instance_id" {
  value = aws_instance.bastion.id
}
```

---

## Spot the Bug

The following Terraform creates an Aurora instance, but `terraform apply` will fail with a cryptic error. Identify the problem before expanding the answer.

```hcl
resource "aws_rds_cluster" "aurora" {
  cluster_identifier = "app-aurora-cluster"
  engine             = "aurora-postgresql"
  engine_version     = "16.4"
  database_name      = "appdb"
  master_username    = "admin"
  master_password    = "SecurePassword123!"

  skip_final_snapshot = true
}

resource "aws_rds_cluster_instance" "writer" {
  identifier         = "app-aurora-writer"
  cluster_identifier = aws_rds_cluster.aurora.id
  instance_class     = "db.t3.micro"
  engine             = "aurora-postgresql"
}
```

<details>
<summary>Explain the bug</summary>

The instance class `db.t3.micro` is **not available for Aurora PostgreSQL** in most AWS regions. Aurora has a different set of supported instance classes than standard RDS. The smallest burstable instance class typically available for Aurora is `db.t3.medium` or `db.t4g.medium`.

The Terraform error will be something like:

```
Error: creating RDS Cluster Instance (app-aurora-writer):
  DBInstanceClassNotFound: db.t3.micro is not available in this region
```

Or in some cases:

```
InvalidParameterCombination: RDS does not support creating a DB instance
with the following combination: DBInstanceClass=db.t3.micro, Engine=aurora-postgresql
```

**The fix:**

```hcl
resource "aws_rds_cluster_instance" "writer" {
  identifier         = "app-aurora-writer"
  cluster_identifier = aws_rds_cluster.aurora.id
  instance_class     = "db.t3.medium"  # Smallest widely available Aurora class
  engine             = "aurora-postgresql"
}
```

**For the exam:** This is not a trick question, but the concept matters. Aurora's minimum instance sizes are larger than RDS, which contributes to Aurora's higher base cost. When cost-comparing, always use the correct instance classes for each engine. You can check available instance classes with:

```bash
aws rds describe-orderable-db-instance-options \
  --engine aurora-postgresql \
  --query 'OrderableDBInstanceOptions[?DBInstanceClass==`db.t3.micro`]'
```

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify both databases are available:**
   ```bash
   aws rds describe-db-instances \
     --query 'DBInstances[?starts_with(DBInstanceIdentifier, `saa-ex08`)].{ID:DBInstanceIdentifier,Engine:Engine,Status:DBInstanceStatus,Class:DBInstanceClass}'

   aws rds describe-db-clusters \
     --query 'DBClusters[?starts_with(DBClusterIdentifier, `saa-ex08`)].{ID:DBClusterIdentifier,Engine:Engine,Status:Status}'
   ```

3. **Connect to bastion and run pgbench:**
   ```bash
   BASTION_ID=$(terraform output -raw bastion_instance_id)
   aws ssm start-session --target "$BASTION_ID"

   # Inside the session:
   RDS_HOST="<rds_endpoint from terraform output, without port>"
   AURORA_HOST="<aurora_cluster_endpoint from terraform output, without port>"

   export PGPASSWORD="LabPassword2026!"

   # Initialize and benchmark RDS
   pgbench -i -s 10 -h "$RDS_HOST" -U labadmin -d benchmark
   pgbench -c 4 -j 2 -T 60 -h "$RDS_HOST" -U labadmin -d benchmark

   # Initialize and benchmark Aurora
   pgbench -i -s 10 -h "$AURORA_HOST" -U labadmin -d benchmark
   pgbench -c 4 -j 2 -T 60 -h "$AURORA_HOST" -U labadmin -d benchmark
   ```

4. **Compare replication lag (Aurora reader):**
   ```bash
   # Inside the SSM session:
   AURORA_READER="<aurora_reader_endpoint>"
   psql -h "$AURORA_READER" -U labadmin -d benchmark \
     -c "SELECT now(), pg_last_wal_replay_lsn();"
   ```

5. **Check Performance Insights:**
   ```bash
   RDS_RESOURCE_ID=$(terraform output -raw rds_resource_id)
   aws pi get-resource-metrics \
     --service-type RDS \
     --identifier "db-$RDS_RESOURCE_ID" \
     --metric-queries '[{"Metric": "db.load.avg"}]' \
     --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%SZ)" \
     --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
     --period-in-seconds 60
   ```

6. **Compare storage characteristics:**
   ```bash
   # RDS: single EBS volume
   aws rds describe-db-instances \
     --db-instance-identifier saa-ex08-rds-pg \
     --query 'DBInstances[0].{Storage:AllocatedStorage,StorageType:StorageType,IOPS:Iops}'

   # Aurora: distributed storage (no AllocatedStorage concept)
   aws rds describe-db-clusters \
     --db-cluster-identifier saa-ex08-aurora-pg \
     --query 'DBClusters[0].{AllocatedStorage:AllocatedStorage,StorageEncrypted:StorageEncrypted}'
   ```

---

## RDS vs Aurora Decision Framework

| Criterion | RDS PostgreSQL | Aurora PostgreSQL |
|---|---|---|
| **Storage architecture** | Single EBS volume per instance | 6 copies across 3 AZs, shared storage |
| **Max storage** | 64 TB (gp3/io2) | 128 TB (auto-scaling, no pre-provisioning) |
| **Read replica lag** | Seconds (async streaming) | Milliseconds (shared storage) |
| **Max read replicas** | 5 (15 cross-region) | 15 (same cluster) + cross-region |
| **Failover time** | 60-120 seconds | ~30 seconds |
| **Minimum instance** | db.t3.micro (~$0.017/hr) | db.t3.medium (~$0.082/hr) |
| **Storage pricing** | EBS: $0.08-$0.125/GB/month | $0.10/GB/month + I/O charges |
| **I/O pricing** | Included in EBS cost | $0.20/million I/O requests |
| **Backtrack** | Not available | Up to 72 hours (MySQL only) |
| **Global Database** | Cross-region read replicas | True global database with < 1s lag |
| **Serverless option** | No | Aurora Serverless v2 |

### Cost Break-Even Analysis

```
Monthly cost comparison for a write-moderate workload:

RDS db.r6g.large Multi-AZ:
  Compute: $0.48/hr x 730 hrs = $350.40
  Storage: 100 GB x $0.115/GB = $11.50
  Total: ~$362/month

Aurora db.r6g.large (2 instances for HA):
  Compute: $0.52/hr x 2 x 730 hrs = $759.20
  Storage: 100 GB x $0.10/GB = $10.00
  I/O: 50M requests x $0.20/M = $10.00
  Total: ~$779/month

Aurora costs ~2.15x more for equivalent HA setup.

Break-even factors favoring Aurora:
  - Avoid read replica sync overhead (saves CPU)
  - Faster failover (less downtime cost)
  - No storage pre-provisioning (saves on over-provisioned EBS)
  - Read-heavy workloads (replicas share storage, no I/O for replication)
  - Aurora Serverless v2 for variable workloads (scale to zero)
```

---

## Solutions

<details>
<summary>database.tf -- TODO 1: Aurora Cluster and Primary Instance</summary>

```hcl
resource "aws_rds_cluster" "aurora" {
  cluster_identifier = "${var.project_name}-aurora-pg"
  engine             = "aurora-postgresql"
  engine_version     = "16.4"
  database_name      = "benchmark"
  master_username    = "labadmin"
  master_password    = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.this.name
  vpc_security_group_ids = [aws_security_group.db.id]

  skip_final_snapshot = true

  tags = {
    Name = "${var.project_name}-aurora"
  }
}

resource "aws_rds_cluster_instance" "writer" {
  identifier         = "${var.project_name}-aurora-pg-writer"
  cluster_identifier = aws_rds_cluster.aurora.id
  instance_class     = "db.t3.medium"
  engine             = "aurora-postgresql"

  performance_insights_enabled = true

  tags = {
    Name = "${var.project_name}-aurora-writer"
  }
}
```

Add outputs for Aurora endpoints:

```hcl
output "aurora_cluster_endpoint" {
  value = aws_rds_cluster.aurora.endpoint
}

output "aurora_reader_endpoint" {
  value = aws_rds_cluster.aurora.reader_endpoint
}
```

</details>

<details>
<summary>database.tf -- TODO 2: Aurora Read Replica</summary>

```hcl
resource "aws_rds_cluster_instance" "reader" {
  identifier         = "${var.project_name}-aurora-pg-reader"
  cluster_identifier = aws_rds_cluster.aurora.id
  instance_class     = "db.t3.medium"
  engine             = "aurora-postgresql"

  performance_insights_enabled = true

  tags = {
    Name = "${var.project_name}-aurora-reader"
  }
}
```

Key differences from RDS read replicas:
- Aurora replicas share the underlying storage volume with the writer. No data is physically replicated.
- Adding an Aurora replica is essentially adding a "read compute node" that points at the same distributed storage.
- RDS read replicas use PostgreSQL streaming replication, which consumes CPU on the primary to ship WAL records and CPU on the replica to replay them.
- Aurora replicas automatically become the writer during failover. With RDS, you must promote a read replica manually (or via Multi-AZ, which is a different mechanism).

</details>

<details>
<summary>TODO 3 -- Performance Insights Comparison</summary>

Performance Insights is already enabled via `performance_insights_enabled = true` on all instances. After running pgbench, compare dashboards:

```bash
# Get resource IDs
RDS_ID=$(aws rds describe-db-instances \
  --db-instance-identifier saa-ex08-rds-pg \
  --query 'DBInstances[0].DbiResourceId' --output text)

AURORA_WRITER_ID=$(aws rds describe-db-instances \
  --db-instance-identifier saa-ex08-aurora-pg-writer \
  --query 'DBInstances[0].DbiResourceId' --output text)

# Compare database load (active sessions) during benchmark
for DB_ID in "$RDS_ID" "$AURORA_WRITER_ID"; do
  echo "=== $DB_ID ==="
  aws pi get-resource-metrics \
    --service-type RDS \
    --identifier "db-$DB_ID" \
    --metric-queries '[{"Metric": "db.load.avg"}]' \
    --start-time "$(date -u -v-30M +%Y-%m-%dT%H:%M:%SZ)" \
    --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    --period-in-seconds 60 \
    --query 'MetricList[0].DataPoints[-5:]'
done
```

Key metrics to compare:
- `db.load.avg`: active sessions (lower is better for same throughput)
- `db.waitstates`: where time is spent (Aurora typically shows less I/O wait)

</details>

<details>
<summary>TODO 4 -- pgbench Benchmark Commands</summary>

```bash
# Connect to bastion
BASTION_ID=$(terraform output -raw bastion_instance_id)
aws ssm start-session --target "$BASTION_ID"

# Set credentials
export PGPASSWORD="LabPassword2026!"
RDS_HOST="saa-ex08-rds-pg.xxxx.us-east-1.rds.amazonaws.com"      # from terraform output
AURORA_HOST="saa-ex08-aurora-pg.cluster-xxxx.us-east-1.rds.amazonaws.com"  # from terraform output

# === RDS Benchmark ===
echo "--- Initializing RDS pgbench tables ---"
pgbench -i -s 10 -h "$RDS_HOST" -U labadmin -d benchmark

echo "--- Running RDS benchmark (60s, 4 clients) ---"
pgbench -c 4 -j 2 -T 60 -h "$RDS_HOST" -U labadmin -d benchmark
# Record: TPS (transactions per second)

# === Aurora Benchmark ===
echo "--- Initializing Aurora pgbench tables ---"
pgbench -i -s 10 -h "$AURORA_HOST" -U labadmin -d benchmark

echo "--- Running Aurora benchmark (60s, 4 clients) ---"
pgbench -c 4 -j 2 -T 60 -h "$AURORA_HOST" -U labadmin -d benchmark
# Record: TPS

# === Read-heavy comparison ===
echo "--- RDS read-only benchmark ---"
pgbench -c 4 -j 2 -T 60 -S -h "$RDS_HOST" -U labadmin -d benchmark

echo "--- Aurora read-only benchmark ---"
pgbench -c 4 -j 2 -T 60 -S -h "$AURORA_HOST" -U labadmin -d benchmark
```

Expected results at this scale (db.t3.micro vs db.t3.medium):
- The instance class difference (micro vs medium) will dominate the numbers.
- For a fair comparison, use the same instance class on both (requires upgrading RDS to db.t3.medium).
- At equal instance classes, Aurora typically shows 2-3x higher TPS for write workloads due to its storage architecture.

</details>

<details>
<summary>TODO 5 -- Compare Replication Lag</summary>

```bash
# Inside SSM session:
AURORA_READER="saa-ex08-aurora-pg.cluster-ro-xxxx.us-east-1.rds.amazonaws.com"

# Aurora replication lag (via SQL)
psql -h "$AURORA_READER" -U labadmin -d benchmark -c "
  SELECT server_id,
         session_id,
         replica_lag_in_msec
  FROM aurora_replica_status();
"

# Aurora replication lag (via CloudWatch)
aws cloudwatch get-metric-statistics \
  --namespace AWS/RDS \
  --metric-name AuroraReplicaLag \
  --dimensions Name=DBInstanceIdentifier,Value=saa-ex08-aurora-pg-reader \
  --start-time "$(date -u -v-10M +%Y-%m-%dT%H:%M:%SZ)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
  --period 60 \
  --statistics Average \
  --query 'Datapoints[*].{Time:Timestamp,AvgLagMs:Average}'
```

Expected results:
- Aurora: `replica_lag_in_msec` typically 5-20ms, often under 10ms
- The lag is inherently low because replicas read from the same storage -- no data transfer is needed
- Under heavy write load, Aurora lag may briefly spike to 50-100ms but recovers quickly

For RDS comparison (if you create a read replica):
- RDS replica lag: 100ms-5s under moderate load
- Under heavy write load: can exceed 10s
- Uses CloudWatch metric `ReplicaLag` (in seconds, not milliseconds)

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify all database resources are deleted:

```bash
aws rds describe-db-instances \
  --query 'DBInstances[?starts_with(DBInstanceIdentifier, `saa-ex08`)].DBInstanceIdentifier'

aws rds describe-db-clusters \
  --query 'DBClusters[?starts_with(DBClusterIdentifier, `saa-ex08`)].DBClusterIdentifier'
```

Both queries should return empty arrays. If instances are still deleting, wait 5-10 minutes and check again.

---

## What's Next

Exercise 09 explores caching strategies across three layers: CloudFront edge caching, ElastiCache Redis application-level caching, and DAX for DynamoDB-specific caching. You will implement each layer incrementally, measure latency improvements at each step, and build a decision framework for when to use which caching solution -- a natural extension of the performance optimization thinking from this database comparison exercise.

---

## Summary

You deployed RDS PostgreSQL and Aurora PostgreSQL side by side in the same VPC, ran identical pgbench benchmarks against both engines, compared replication lag characteristics, and analyzed Performance Insights data to understand where each engine spends its time. The key architectural insight is that Aurora's distributed storage layer eliminates the EBS bottleneck that limits RDS performance, but this comes at a higher per-hour compute cost and introduces I/O-based storage pricing. For the SAA-C03, remember the specific numbers: Aurora failover in ~30s vs RDS 60-120s, Aurora replica lag in milliseconds vs RDS in seconds, and Aurora storage automatically scales to 128TB while RDS requires manual EBS management up to 64TB.

---

## Reference

- [Aurora PostgreSQL Overview](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.AuroraPostgreSQL.html)
- [Aurora Storage Architecture](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.Overview.StorageReliability.html)
- [Aurora Replication](https://docs.aws.amazon.com/AmazonRDS/latest/AuroraUserGuide/Aurora.Replication.html)
- [RDS vs Aurora Feature Comparison](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/Concepts.RDSFeaturesRegionsDBEngines.grids.html)
- [Performance Insights](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/USER_PerfInsights.html)

## Additional Resources

- [Terraform aws_rds_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/rds_cluster)
- [Terraform aws_rds_cluster_instance](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/rds_cluster_instance)
- [pgbench Documentation](https://www.postgresql.org/docs/current/pgbench.html)
- [Aurora Pricing](https://aws.amazon.com/rds/aurora/pricing/)
- [RDS Pricing](https://aws.amazon.com/rds/postgresql/pricing/)
