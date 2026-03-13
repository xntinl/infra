# 61. Neptune Graph Database and Use Cases

<!--
difficulty: intermediate
concepts: [neptune-cluster, graph-database, gremlin, sparql, property-graph, rdf, social-network, fraud-detection, recommendation-engine, knowledge-graph]
tools: [terraform, aws-cli, gremlin-console]
estimated_time: 40m
bloom_level: apply, analyze
prerequisites: [55-dynamodb-capacity-modes-throttling]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** Neptune db.t3.medium instance costs ~$0.10/hr. Storage and I/O costs are negligible for this exercise. Remember to run `terraform destroy` immediately when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Default VPC with subnets in >= 2 AZs | `aws ec2 describe-subnets --filters Name=default-for-az,Values=true --query 'Subnets[*].AvailabilityZone'` |
| curl installed | `curl --version` |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** a Neptune cluster with primary and replica instances using Terraform.
2. **Construct** Gremlin traversal queries for common graph patterns (shortest path, neighbors, recommendations).
3. **Evaluate** when Neptune (graph database) is the right choice vs DynamoDB (key-value), RDS (relational), or DocumentDB (document).
4. **Analyze** graph database use cases: social networks, fraud detection, recommendation engines, knowledge graphs, and network topology.
5. **Distinguish** between Gremlin (property graph) and SPARQL (RDF) query languages and when each applies.

---

## Why This Matters

Neptune appears on the SAA-C03 exam in scenarios involving relationship-heavy queries. The key architectural insight is understanding when relationships are the primary query pattern vs when they are secondary. A social network query like "find all friends of friends who also follow this topic" requires traversing multiple relationship hops -- each hop in a relational database means another JOIN, and performance degrades exponentially with depth. In Neptune, multi-hop traversals are native operations with constant-time performance per hop.

The architect decision is not "graph databases are always better." It is: how many relationship hops does your query require? For 1-2 hops (e.g., "find all orders for this customer"), a relational database with proper indexes is simpler and sufficient. For 3+ hops (e.g., "find all people within 4 degrees of connection who share 2+ interests"), Neptune provides orders-of-magnitude better performance. The exam tests this by describing a workload and asking which database service fits -- the signal is always the depth and centrality of relationship traversal in the query pattern.

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
  default     = "saa-ex61"
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

resource "aws_security_group" "neptune" {
  name   = "${var.project_name}-neptune-sg"
  vpc_id = data.aws_vpc.default.id

  ingress {
    from_port   = 8182
    to_port     = 8182
    protocol    = "tcp"
    cidr_blocks = [data.aws_vpc.default.cidr_block]
    description = "Neptune from VPC"
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
# TODO 1: Create a Neptune Subnet Group
# ============================================================
# Requirements:
#   - Resource: aws_neptune_subnet_group
#   - name = "${var.project_name}-subnet-group"
#   - subnet_ids = default subnets (from security.tf)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/neptune_subnet_group
# ============================================================


# ============================================================
# TODO 2: Create a Neptune Cluster
# ============================================================
# Create a Neptune cluster with a single instance.
#
# Architecture (like Aurora):
#   Cluster (storage layer: 6 copies across 3 AZs)
#     --> Primary Instance (read/write)
#     --> Replica Instances (read-only, failover targets)
#
# Requirements:
#   - Resource: aws_neptune_cluster
#   - cluster_identifier = "${var.project_name}-cluster"
#   - engine = "neptune"
#   - neptune_subnet_group_name = subnet group
#   - vpc_security_group_ids = [security group] (from security.tf)
#   - skip_final_snapshot = true
#   - iam_database_authentication_enabled = true
#   - storage_encrypted = true
#
#   - Resource: aws_neptune_cluster_instance
#   - count = 1
#   - identifier = "${var.project_name}-instance-${count.index}"
#   - cluster_identifier = cluster id
#   - instance_class = "db.t3.medium"
#   - engine = "neptune"
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/neptune_cluster
# ============================================================


# ============================================================
# TODO 3: Run Gremlin Queries (CLI/HTTP)
# ============================================================
# From within the VPC, use the Gremlin HTTP endpoint to add
# vertices (people), edges (friendships), and query
# relationships. See Solutions for full curl commands.
#
# IMPORTANT: Write queries MUST end with .iterate() to execute.
# Read queries that return values execute implicitly.
#
# Docs: https://docs.aws.amazon.com/neptune/latest/userguide/access-graph-gremlin.html
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

output "gremlin_endpoint" {
  value = "Set after TODO 2 implementation"
}
```

---

## When to Use Neptune vs Other Databases

| Use Case | Neptune | DynamoDB | RDS/Aurora |
|---|---|---|---|
| **Social network (friends of friends)** | Best | Poor | Slow (recursive JOINs) |
| **Fraud detection (transaction rings)** | Best | Poor | Very slow |
| **Recommendation engine** | Best | Possible (pre-computed) | Slow at depth |
| **Knowledge graph** | Best (SPARQL) | Not suitable | Complex |
| **Simple key-value lookups** | Overkill | Best | Good |
| **OLTP with complex schemas** | Not suitable | Limited | Best |

**Decision:** Is the primary query pattern traversing 3+ relationship hops? Yes = Neptune. No = RDS or DynamoDB based on data model. Property graph = Gremlin. RDF = SPARQL.

---

## Spot the Bug

A developer wrote Gremlin queries to build a social network recommendation engine but reports that friend suggestions are never created:

```groovy
// Add a "suggests" edge from Alice to all friends-of-friends
// who she does not already know
g.V().has('name', 'Alice')
  .out('knows')
  .out('knows')
  .where(__.not(__.in('knows').has('name', 'Alice')))
  .dedup()
  .addE('suggested_friend').from(g.V().has('name', 'Alice'))
```

The query compiles without error, but no `suggested_friend` edges are created.

<details>
<summary>Explain the bug</summary>

**The Gremlin traversal is missing `.iterate()` at the end.** In Gremlin, a traversal that performs side effects (like `addE()`) must be explicitly executed with a terminal step. Without `.iterate()`, the traversal is compiled and the execution plan is created, but the query is never actually sent to the Neptune engine for processing.

This is a common Gremlin pitfall: read queries that return values (`.values()`, `.toList()`, `.next()`) implicitly trigger execution because the client needs to fetch results. But write queries that do not return values require an explicit terminal step.

**Fix:** Add `.iterate()` to execute the traversal:

```groovy
g.V().has('name', 'Alice')
  .out('knows')
  .out('knows')
  .where(__.not(__.in('knows').has('name', 'Alice')))
  .dedup()
  .addE('suggested_friend').from(g.V().has('name', 'Alice'))
  .iterate()  // <-- This triggers actual execution
```

Terminal steps in Gremlin:
- `.iterate()` -- execute without returning results (for mutations)
- `.next()` -- execute and return the next result
- `.toList()` -- execute and return all results as a list
- `.hasNext()` -- execute and check if results exist

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: Neptune cluster creation takes 10-15 minutes.

2. **Verify the cluster:**
   ```bash
   aws neptune describe-db-clusters \
     --db-cluster-identifier saa-ex61-cluster \
     --query 'DBClusters[0].{Status:Status,Engine:Engine,StorageEncrypted:StorageEncrypted,IAMAuth:IAMDatabaseAuthenticationEnabled,Endpoint:Endpoint}' \
     --output json
   ```
   Expected: Status = `available`, StorageEncrypted = `true`.

3. **Verify instances:**
   ```bash
   aws neptune describe-db-instances \
     --filters Name=db-cluster-id,Values=saa-ex61-cluster \
     --query 'DBInstances[*].{Id:DBInstanceIdentifier,Class:DBInstanceClass,Status:DBInstanceStatus}' \
     --output table
   ```
   Expected: One instance with Status = `available`.

4. **Test Gremlin endpoint (from within VPC):**
   ```bash
   ENDPOINT=$(aws neptune describe-db-clusters \
     --db-cluster-identifier saa-ex61-cluster \
     --query 'DBClusters[0].Endpoint' --output text)

   curl -s -X POST \
     -d '{"gremlin":"g.V().count()"}' \
     "https://$ENDPOINT:8182/gremlin"
   ```
   Expected: `{"result":{"data":{"@type":"g:List","@value":[{"@type":"g:Long","@value":0}]}}}`

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
resource "aws_neptune_subnet_group" "this" {
  name       = "${var.project_name}-subnet-group"
  subnet_ids = data.aws_subnets.default.ids

  tags = { Name = "${var.project_name}-subnet-group" }
}
```

</details>

<details>
<summary>TODO 2 -- Neptune Cluster and Instance (database.tf)</summary>

```hcl
resource "aws_neptune_cluster" "this" {
  cluster_identifier  = "${var.project_name}-cluster"
  engine              = "neptune"
  neptune_subnet_group_name  = aws_neptune_subnet_group.this.name
  vpc_security_group_ids     = [aws_security_group.neptune.id]
  skip_final_snapshot        = true
  iam_database_authentication_enabled = true
  storage_encrypted          = true

  tags = { Name = "${var.project_name}-cluster" }
}

resource "aws_neptune_cluster_instance" "this" {
  count              = 1
  identifier         = "${var.project_name}-instance-${count.index}"
  cluster_identifier = aws_neptune_cluster.this.id
  instance_class     = "db.t3.medium"
  engine             = "neptune"

  tags = { Name = "${var.project_name}-instance-${count.index}" }
}

output "cluster_endpoint" {
  value = aws_neptune_cluster.this.endpoint
}

output "reader_endpoint" {
  value = aws_neptune_cluster.this.reader_endpoint
}

output "gremlin_endpoint" {
  value = "https://${aws_neptune_cluster.this.endpoint}:8182/gremlin"
}
```

Neptune uses the same Aurora-like storage architecture as DocumentDB: 6 copies across 3 AZs with automatic repair. The cluster endpoint handles writes; the reader endpoint load-balances reads across replicas.

</details>

<details>
<summary>TODO 3 -- Gremlin Queries (CLI)</summary>

```bash
ENDPOINT=$(terraform output -raw cluster_endpoint)

# Add people
curl -s -X POST \
  -d '{"gremlin":"g.addV(\"person\").property(\"name\",\"Alice\").property(\"age\",30).iterate()"}' \
  "https://$ENDPOINT:8182/gremlin"

curl -s -X POST \
  -d '{"gremlin":"g.addV(\"person\").property(\"name\",\"Bob\").property(\"age\",25).iterate()"}' \
  "https://$ENDPOINT:8182/gremlin"

curl -s -X POST \
  -d '{"gremlin":"g.addV(\"person\").property(\"name\",\"Carol\").property(\"age\",35).iterate()"}' \
  "https://$ENDPOINT:8182/gremlin"

# Add friendships
curl -s -X POST \
  -d '{"gremlin":"g.V().has(\"name\",\"Alice\").addE(\"knows\").to(g.V().has(\"name\",\"Bob\")).iterate()"}' \
  "https://$ENDPOINT:8182/gremlin"

curl -s -X POST \
  -d '{"gremlin":"g.V().has(\"name\",\"Bob\").addE(\"knows\").to(g.V().has(\"name\",\"Carol\")).iterate()"}' \
  "https://$ENDPOINT:8182/gremlin"

# Friends of Alice (1 hop)
curl -s -X POST \
  -d '{"gremlin":"g.V().has(\"name\",\"Alice\").out(\"knows\").values(\"name\")"}' \
  "https://$ENDPOINT:8182/gremlin"
# Expected: Bob

# Friends of friends of Alice (2 hops)
curl -s -X POST \
  -d '{"gremlin":"g.V().has(\"name\",\"Alice\").out(\"knows\").out(\"knows\").values(\"name\")"}' \
  "https://$ENDPOINT:8182/gremlin"
# Expected: Carol

# Count all vertices
curl -s -X POST \
  -d '{"gremlin":"g.V().count()"}' \
  "https://$ENDPOINT:8182/gremlin"
# Expected: 3

# Count all edges
curl -s -X POST \
  -d '{"gremlin":"g.E().count()"}' \
  "https://$ENDPOINT:8182/gremlin"
# Expected: 2
```

Note the `.iterate()` on write queries (addV, addE) and its absence on read queries that return values (.values(), .count()).

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Neptune cluster deletion takes 5-10 minutes. Verify:

```bash
aws neptune describe-db-clusters \
  --db-cluster-identifier saa-ex61-cluster 2>&1 || echo "Cluster deleted"
```

---

## What's Next

Exercise 62 covers **Amazon Keyspaces (managed Apache Cassandra)**. You will create keyspaces and tables using CQL, compare with self-managed Cassandra, and learn which Cassandra-specific features are not supported in the managed service -- continuing the pattern of understanding "managed with limitations" trade-offs.

---

## Summary

- **Neptune** is a purpose-built graph database optimized for traversing relationships -- queries that are exponentially expensive in relational databases are constant-time per hop
- **Two query languages:** Gremlin (property graphs -- vertices with properties and labeled edges) and SPARQL (RDF triples -- subject-predicate-object for knowledge graphs)
- **Aurora-like storage** provides 6 copies across 3 AZs, automatic repair, and scaling to 128 TB
- **Use Neptune when** the primary query pattern involves traversing 3+ relationship hops (social networks, fraud detection, recommendation engines)
- **Do not use Neptune when** queries are primarily key-value lookups (DynamoDB), relational with 1-2 JOINs (RDS), or document-oriented (DocumentDB)
- **Gremlin terminal steps** are required for write operations -- `.iterate()` for mutations, `.next()` or `.toList()` for reads
- **IAM database authentication** provides token-based access control integrated with AWS IAM policies
- **Neptune Serverless** auto-scales compute capacity based on workload -- eliminates capacity planning

## Reference

- [Neptune User Guide](https://docs.aws.amazon.com/neptune/latest/userguide/intro.html)
- [Gremlin Query Language](https://docs.aws.amazon.com/neptune/latest/userguide/access-graph-gremlin.html)
- [Neptune vs Other Databases](https://docs.aws.amazon.com/neptune/latest/userguide/feature-overview-data-model.html)
- [Terraform aws_neptune_cluster](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/neptune_cluster)

## Additional Resources

- [Gremlin Recipes](https://tinkerpop.apache.org/docs/current/recipes/) -- common graph traversal patterns and idioms
- [Neptune Best Practices](https://docs.aws.amazon.com/neptune/latest/userguide/best-practices.html) -- query optimization, instance sizing, and connection management
- [Neptune Serverless](https://docs.aws.amazon.com/neptune/latest/userguide/neptune-serverless.html) -- auto-scaling compute for variable workloads
