# 60. DocumentDB and MongoDB Compatibility

<!--
difficulty: intermediate
concepts: [documentdb-cluster, mongodb-compatibility, storage-replication, compute-storage-separation, replica-instances, connection-string, tls-certificate, change-streams, global-clusters]
tools: [terraform, aws-cli, mongosh]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [55-dynamodb-capacity-modes-throttling]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** DocumentDB db.t3.medium instance costs ~$0.076/hr. Storage costs are negligible for this exercise. Total ~$0.10/hr including I/O. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| mongosh or mongo client installed | `mongosh --version` |
| Default VPC with subnets in >= 2 AZs | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[*].AvailabilityZone'` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a DocumentDB cluster with primary and replica instances using Terraform.
2. **Analyze** the DocumentDB architecture: Aurora-like storage layer (6 copies across 3 AZs) with separate compute instances.
3. **Evaluate** the trade-offs between DocumentDB (managed) and self-managed MongoDB on EC2 (control, version compatibility).
4. **Distinguish** MongoDB API features supported by DocumentDB vs those that require native MongoDB.
5. **Design** a document database deployment that matches specific availability, compatibility, and operational requirements.

---

## Why This Matters

DocumentDB appears on the SAA-C03 exam as the managed MongoDB-compatible option. The critical insight is that DocumentDB is NOT MongoDB -- it implements the MongoDB wire protocol on an Aurora-like storage engine (6 copies, 3 AZs, 128 TB auto-scaling) but NOT full MongoDB feature compatibility. DocumentDB wins when you want managed operations; self-managed MongoDB wins when you need full compatibility or the latest version.

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
  default     = "saa-ex60"
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

resource "aws_security_group" "docdb" {
  name   = "${var.project_name}-docdb-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 27017
    to_port     = 27017
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "DocumentDB from VPC"
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
# TODO 1: Create a DocumentDB Subnet Group
# ============================================================
# Create a subnet group for the DocumentDB cluster.
#
# Requirements:
#   - Resource: aws_docdb_subnet_group
#   - name = "${var.project_name}-subnet-group"
#   - subnet_ids = default subnets (from security.tf)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/docdb_subnet_group
# ============================================================


# ============================================================
# TODO 2: Create a DocumentDB Cluster
# ============================================================
# Create a DocumentDB cluster. The cluster defines the storage
# layer (shared across all instances) and cluster-level config.
#
# Architecture:
#   Cluster (storage layer: 6 copies across 3 AZs)
#     --> Primary Instance (read/write)
#     --> Replica Instance (read-only, failover target)
#
# Requirements:
#   - Resource: aws_docdb_cluster
#   - cluster_identifier = "${var.project_name}-cluster"
#   - engine = "docdb"
#   - master_username = "docdbadmin"
#   - master_password = "DocDB1234!" (use secrets in production)
#   - db_subnet_group_name = subnet group
#   - vpc_security_group_ids = [security group] (from security.tf)
#   - skip_final_snapshot = true
#   - storage_encrypted = true
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/docdb_cluster
# ============================================================


# ============================================================
# TODO 3: Create DocumentDB Cluster Instances
# ============================================================
# Create one primary instance. In production, add replicas
# for read scaling and high availability.
#
# Requirements:
#   - Resource: aws_docdb_cluster_instance
#   - count = 1 (primary only for cost savings in lab)
#   - identifier = "${var.project_name}-instance-${count.index}"
#   - cluster_identifier = cluster id
#   - instance_class = "db.t3.medium"
#
# Note: The first instance created becomes the primary.
# Additional instances become read replicas automatically.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/docdb_cluster_instance
# ============================================================


# ============================================================
# TODO 4: Connect and Test (CLI)
# ============================================================
# Download CA cert, connect with mongosh, test CRUD operations.
# See Solutions for full commands.
#
#   wget https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
#   mongosh --tls --tlsCAFile global-bundle.pem \
#     --host <endpoint>:27017 --username docdbadmin --password DocDB1234!
#
# Docs: https://docs.aws.amazon.com/documentdb/latest/developerguide/connect-from-outside-a-vpc.html
# ============================================================
```

### `outputs.tf`

```hcl
output "cluster_endpoint" {
  value = "Set after TODO 2 implementation"
}

output "reader_endpoint" {
  value = "Set after TODO 2 implementation"
}
```

---

## DocumentDB vs MongoDB on EC2

| Criterion | DocumentDB | MongoDB on EC2 |
|---|---|---|
| **Management** | Fully managed | Self-managed |
| **Compatibility** | MongoDB 3.6/4.0/5.0 API subset | Full MongoDB (any version) |
| **Storage** | Aurora-like (6 copies, 3 AZs, 128 TB) | EBS (manual scaling) |
| **Backup** | Continuous, point-in-time recovery | Manual snapshots |
| **Failover** | Automatic (~30 seconds) | Manual or replica set config |
| **Cost** | Instance + I/O + storage | EC2 + EBS + operational labor |
| **Vendor lock-in** | High (not real MongoDB) | None |

Choose DocumentDB for managed operations with moderate compatibility. Choose self-managed MongoDB for full feature compatibility or the latest version.

---

## Spot the Bug

A development team migrated their application from MongoDB 5.0 on EC2 to DocumentDB with MongoDB 5.0 compatibility. Their application fails with errors on certain queries:

```javascript
// Application code using MongoDB Node.js driver 6.x
const client = new MongoClient(documentDBUri, {
  tls: true,
  tlsCAFile: 'global-bundle.pem'
});

// This works fine
const users = await db.collection('users').find({ age: { $gt: 25 } }).toArray();

// This fails with "Command not supported"
const result = await db.collection('orders').aggregate([
  {
    $merge: {
      into: "monthly_totals",
      on: "_id",
      whenMatched: "merge",
      whenNotMatched: "insert"
    }
  }
]).toArray();

// This also fails
const changeStream = db.collection('orders').watch([], {
  fullDocumentBeforeChange: 'required'
});
```

<details>
<summary>Explain the bug</summary>

**The application uses MongoDB features not fully supported in DocumentDB.** Even though DocumentDB advertises MongoDB 5.0 compatibility, it implements a subset of the API:

1. **`$merge` aggregation stage:** DocumentDB has limited `$merge` support. Certain options like `whenMatched: "merge"` may not be available or behave differently. The `$out` stage is supported as an alternative but overwrites the entire target collection.

2. **`fullDocumentBeforeChange` in change streams:** Not supported in DocumentDB (MongoDB 6.0 feature).

3. **Driver version compatibility:** MongoDB driver 6.x targets MongoDB 6.0+ features, exposing unsupported commands with DocumentDB 5.0.

**Fix:** Audit your application's MongoDB feature usage against the [DocumentDB supported operations list](https://docs.aws.amazon.com/documentdb/latest/developerguide/mongo-apis.html) before migrating. Use a compatible driver version:

```javascript
// Use MongoDB driver 4.x or 5.x for DocumentDB 5.0 compatibility
// npm install mongodb@5

// Replace $merge with application-level upsert logic
for (const doc of aggregationResults) {
  await db.collection('monthly_totals').updateOne(
    { _id: doc._id },
    { $set: doc },
    { upsert: true }
  );
}

// Use change streams without fullDocumentBeforeChange
const changeStream = db.collection('orders').watch([], {
  fullDocument: 'updateLookup'  // Supported in DocumentDB
});
```

Always check the [DocumentDB supported operations list](https://docs.aws.amazon.com/documentdb/latest/developerguide/mongo-apis.html) before migration.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: DocumentDB cluster creation takes 5-10 minutes.

2. **Verify the cluster:**
   ```bash
   aws docdb describe-db-clusters \
     --db-cluster-identifier saa-ex60-cluster \
     --query 'DBClusters[0].{Status:Status,Engine:Engine,EngineVersion:EngineVersion,StorageEncrypted:StorageEncrypted,MultiAZ:MultiAZ,Endpoint:Endpoint,ReaderEndpoint:ReaderEndpoint}' \
     --output json
   ```
   Expected: Status = `available`, StorageEncrypted = `true`.

3. **Verify instances:**
   ```bash
   aws docdb describe-db-instances \
     --filters Name=db-cluster-id,Values=saa-ex60-cluster \
     --query 'DBInstances[*].{Id:DBInstanceIdentifier,Class:DBInstanceClass,AZ:AvailabilityZone,Status:DBInstanceStatus}' \
     --output table
   ```
   Expected: One instance with Status = `available`.

4. **Test connectivity:**
   ```bash
   ENDPOINT=$(aws docdb describe-db-clusters \
     --db-cluster-identifier saa-ex60-cluster \
     --query 'DBClusters[0].Endpoint' --output text)

   wget -q https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem

   mongosh --tls --tlsCAFile global-bundle.pem \
     --host "$ENDPOINT:27017" \
     --username docdbadmin --password DocDB1234! \
     --eval "db.runCommand({ ping: 1 })"
   ```
   Expected: `{ ok: 1 }`

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Subnet Group (database.tf)</summary>

```hcl
resource "aws_docdb_subnet_group" "this" {
  name       = "${var.project_name}-subnet-group"
  subnet_ids = data.aws_subnets.default.ids

  tags = { Name = "${var.project_name}-subnet-group" }
}
```

</details>

<details>
<summary>TODO 2 -- DocumentDB Cluster (database.tf)</summary>

```hcl
resource "aws_docdb_cluster" "this" {
  cluster_identifier  = "${var.project_name}-cluster"
  engine              = "docdb"
  master_username     = "docdbadmin"
  master_password     = "DocDB1234!"
  db_subnet_group_name    = aws_docdb_subnet_group.this.name
  vpc_security_group_ids  = [aws_security_group.docdb.id]
  skip_final_snapshot     = true
  storage_encrypted       = true

  tags = { Name = "${var.project_name}-cluster" }
}

output "cluster_endpoint" {
  value = aws_docdb_cluster.this.endpoint
}

output "reader_endpoint" {
  value = aws_docdb_cluster.this.reader_endpoint
}
```

The cluster defines the shared storage layer. Storage automatically replicates 6 copies across 3 AZs (like Aurora). You do not configure replication -- it is built into the storage engine.

</details>

<details>
<summary>TODO 3 -- Cluster Instances (database.tf)</summary>

```hcl
resource "aws_docdb_cluster_instance" "this" {
  count              = 1
  identifier         = "${var.project_name}-instance-${count.index}"
  cluster_identifier = aws_docdb_cluster.this.id
  instance_class     = "db.t3.medium"

  tags = { Name = "${var.project_name}-instance-${count.index}" }
}
```

The first instance becomes the primary (read/write). Adding `count = 2` or `count = 3` would create replica instances that handle read traffic and serve as automatic failover targets.

</details>

<details>
<summary>TODO 4 -- Connection Test (CLI)</summary>

```bash
wget -q https://truststore.pki.rds.amazonaws.com/global/global-bundle.pem
ENDPOINT=$(terraform output -raw cluster_endpoint)
mongosh --tls --tlsCAFile global-bundle.pem \
  --host "$ENDPOINT:27017" --username docdbadmin --password DocDB1234! \
  --eval "use testdb; db.users.insertOne({name:'Alice',age:30}); db.users.find()"
```

DocumentDB requires TLS by default. The `global-bundle.pem` validates server identity.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
aws docdb describe-db-clusters \
  --db-cluster-identifier saa-ex60-cluster 2>&1 || echo "Cluster deleted"
```

---

## What's Next

Exercise 61 covers **Amazon Neptune for graph database use cases**. You will explore when relationship-heavy queries (social networks, recommendation engines, fraud detection) benefit from a purpose-built graph database vs modeling relationships in DynamoDB or RDS -- a design pattern the exam tests through scenario-based questions.

---

## Summary

- **DocumentDB** implements the MongoDB wire protocol on an Aurora-like storage engine -- 6 copies across 3 AZs with automatic repair and scaling to 128 TB
- **Separate compute and storage** means replicas share the same storage volume -- no data duplication
- **MongoDB compatibility** covers 3.6/4.0/5.0 but is a subset -- audit feature usage before migrating
- **TLS is required by default** -- applications must use the RDS CA certificate bundle
- **Driver version matters** -- match the driver version to DocumentDB compatibility level
- **Automated failover** promotes a replica to primary in ~30 seconds
- **Continuous backups** with point-in-time recovery up to 35 days are built in
- **Cost model** is instance hours + I/O requests + storage -- I/O costs can surprise if workloads are write-heavy

## Reference

- [DocumentDB Developer Guide](https://docs.aws.amazon.com/documentdb/latest/developerguide/what-is.html)
- [Supported MongoDB APIs](https://docs.aws.amazon.com/documentdb/latest/developerguide/mongo-apis.html)
- [DocumentDB vs MongoDB](https://docs.aws.amazon.com/documentdb/latest/developerguide/functional-differences.html)
- [Terraform aws_docdb_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/docdb_cluster)

## Additional Resources

- [DocumentDB Compatibility Tool](https://github.com/awslabs/amazon-documentdb-tools) -- scan for unsupported features before migration
- [Migrating from MongoDB to DocumentDB](https://docs.aws.amazon.com/documentdb/latest/developerguide/docdb-migration.html) -- migration guide using DMS
