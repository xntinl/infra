# 64. RDS Proxy and Connection Pooling

<!--
difficulty: intermediate
concepts: [rds-proxy, connection-pooling, lambda-rds, secrets-manager, iam-authentication, connection-exhaustion, multiplexing, pinning, target-group, failover]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [01-rds-multi-az-read-replicas]
aws_cost: ~$0.05/hr
-->

> **AWS Cost Warning:** RDS Proxy minimum cost is ~$0.015/hr per vCPU. RDS db.t3.micro adds ~$0.017/hr. Secrets Manager secret adds $0.40/month (~$0.0006/hr). Total ~$0.05/hr. Remember to run `terraform destroy` immediately when finished.

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

1. **Implement** RDS Proxy with Secrets Manager integration for connection pooling between Lambda and RDS.
2. **Analyze** why hundreds of concurrent Lambda invocations overwhelm RDS connection limits.
3. **Evaluate** when RDS Proxy is necessary vs direct Lambda-to-RDS connections.
4. **Apply** IAM authentication through RDS Proxy for tokenless database access.
5. **Diagnose** common RDS Proxy issues: VPC configuration, security group rules, and connection pinning.

---

## Why This Matters

RDS Proxy solves the connection exhaustion problem: each Lambda invocation opens a new database connection, quickly exceeding `max_connections`. RDS Proxy maintains a pool of persistent connections, multiplexing thousands of Lambda requests across a small number of actual database connections. It also accelerates Multi-AZ failover from minutes to seconds.

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
  default     = "saa-ex64"
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

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}
```

### `secrets.tf`

```hcl
# ============================================================
# TODO 1: Create Secrets Manager Secret for RDS Credentials
# ============================================================
# RDS Proxy reads credentials from Secrets Manager.
# Store username, password, engine, host, port, dbname as JSON.
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret
# ============================================================
```

### `database.tf`

```hcl
# ============================================================
# TODO 2: Create RDS Instance
# ============================================================
# MySQL db.t3.micro with db_name = "appdb".
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_instance
# ============================================================


# ============================================================
# TODO 4: Create RDS Proxy
# ============================================================
# Lambda --> RDS Proxy (pool) --> RDS Instance
# 1000 Lambda invocations --> ~50 DB connections (multiplexed)
#
# Resources: aws_db_proxy, aws_db_proxy_default_target_group,
# aws_db_proxy_target. Configure connection pool percentages.
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_proxy
# ============================================================
```

### `iam.tf`

```hcl
# ============================================================
# TODO 3: Create IAM Role for RDS Proxy
# ============================================================
# Role assumed by rds.amazonaws.com with permission to read
# the Secrets Manager secret (secretsmanager:GetSecretValue).
# Docs: https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy-setup.html
# ============================================================
```

### `outputs.tf`

```hcl
output "proxy_endpoint" {
  value = "Set after TODO 4 implementation"
}

output "rds_endpoint" {
  value = "Set after TODO 2 implementation"
}
```

---

## Spot the Bug

A team deployed RDS Proxy for their Lambda-to-RDS architecture, but Lambda functions timeout after 30 seconds when trying to connect to the proxy:

```hcl
# Lambda function
resource "aws_lambda_function" "api" {
  function_name = "api-handler"
  runtime       = "provided.al2023"
  handler       = "bootstrap"
  architectures = ["arm64"]
  timeout       = 30
  filename      = "lambda.zip"
  role          = aws_iam_role.lambda.arn

  vpc_config {
    subnet_ids         = data.aws_subnets.private.ids        # Private subnets in VPC-A
    security_group_ids = [aws_security_group.lambda.id]
  }
}

# RDS Proxy
resource "aws_db_proxy" "this" {
  name          = "api-proxy"
  engine_family = "MYSQL"
  role_arn      = aws_iam_role.proxy.arn
  vpc_subnet_ids        = data.aws_subnets.database.ids      # Database subnets in VPC-B
  vpc_security_group_ids = [aws_security_group.proxy.id]
  require_tls   = true

  auth {
    auth_scheme = "SECRETS"
    iam_auth    = "REQUIRED"
    secret_arn  = aws_secretsmanager_secret.db.arn
  }
}
```

<details>
<summary>Explain the bug</summary>

**The Lambda function and RDS Proxy are in different VPCs.** The Lambda `vpc_config` places it in `VPC-A` (private subnets), while the RDS Proxy is deployed in `VPC-B` (database subnets). There is no network path between them, so the Lambda function's TCP connection to the proxy endpoint times out after 30 seconds.

Even within the same VPC, this would fail if the security groups do not allow traffic. The Lambda security group must allow outbound traffic on port 3306, and the proxy security group must allow inbound traffic on port 3306 from the Lambda security group.

**Fix:** Deploy the RDS Proxy in the same VPC as the Lambda function (or establish VPC peering), and configure security groups correctly:

```hcl
# Lambda and RDS Proxy must be in the SAME VPC
resource "aws_lambda_function" "api" {
  vpc_config {
    subnet_ids         = data.aws_subnets.private.ids
    security_group_ids = [aws_security_group.lambda.id]
  }
}

resource "aws_db_proxy" "this" {
  vpc_subnet_ids         = data.aws_subnets.private.ids  # Same VPC subnets
  vpc_security_group_ids = [aws_security_group.proxy.id]
}

# Security group: proxy allows inbound from Lambda
resource "aws_security_group_rule" "proxy_from_lambda" {
  type                     = "ingress"
  from_port                = 3306
  to_port                  = 3306
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.lambda.id
  security_group_id        = aws_security_group.proxy.id
}

# Security group: Lambda allows outbound to proxy
resource "aws_security_group_rule" "lambda_to_proxy" {
  type                     = "egress"
  from_port                = 3306
  to_port                  = 3306
  protocol                 = "tcp"
  source_security_group_id = aws_security_group.proxy.id
  security_group_id        = aws_security_group.lambda.id
}
```

All three (Lambda, RDS Proxy, RDS) must be in the same VPC with correct security group rules.

</details>

---

## Verify What You Learned

1. **Deploy the infrastructure:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```
   Note: RDS instance takes 5-10 minutes. RDS Proxy registration takes 5-10 minutes.

2. **Verify the proxy:**
   ```bash
   aws rds describe-db-proxies \
     --db-proxy-name saa-ex64-proxy \
     --query 'DBProxies[0].{Status:Status,Endpoint:Endpoint,EngineFamily:EngineFamily,RequireTLS:RequireTLS}' \
     --output json
   ```
   Expected: Status = `available`, RequireTLS = `true`.

3. **Verify target health:**
   ```bash
   aws rds describe-db-proxy-targets \
     --db-proxy-name saa-ex64-proxy \
     --query 'Targets[*].{Endpoint:Endpoint,TargetHealth:TargetHealth}' \
     --output json
   ```
   Expected: TargetHealth.State = `AVAILABLE`.

4. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Secrets Manager Secret (secrets.tf)</summary>

```hcl
resource "aws_secretsmanager_secret" "db" {
  name = "${var.project_name}-db-credentials"
  tags = { Name = "${var.project_name}-db-credentials" }
}

resource "aws_secretsmanager_secret_version" "db" {
  secret_id = aws_secretsmanager_secret.db.id
  secret_string = jsonencode({
    username = "admin"; password = "Admin1234!"; engine = "mysql"
    host = aws_db_instance.this.address; port = 3306; dbname = "appdb"
  })
}
```

</details>

<details>
<summary>TODO 2 -- RDS Instance (database.tf)</summary>

```hcl
resource "aws_db_instance" "this" {
  identifier          = "${var.project_name}-db"
  engine              = "mysql"
  engine_version      = "8.0"
  instance_class      = "db.t3.micro"
  allocated_storage   = 20
  db_name             = "appdb"
  username            = "admin"
  password            = "Admin1234!"
  skip_final_snapshot = true
  vpc_security_group_ids = [aws_security_group.db.id]

  tags = { Name = "${var.project_name}-db" }
}
```

Update `outputs.tf`:

```hcl
output "rds_endpoint" {
  value = aws_db_instance.this.address
}
```

</details>

<details>
<summary>TODO 3 -- IAM Role for RDS Proxy (iam.tf)</summary>

```hcl
data "aws_iam_policy_document" "proxy_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["rds.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "proxy" {
  name               = "${var.project_name}-proxy-role"
  assume_role_policy = data.aws_iam_policy_document.proxy_assume.json
}

data "aws_iam_policy_document" "proxy_policy" {
  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [aws_secretsmanager_secret.db.arn]
  }
  statement {
    actions   = ["kms:Decrypt"]
    resources = ["*"]
    condition {
      test     = "StringEquals"
      variable = "kms:ViaService"
      values   = ["secretsmanager.us-east-1.amazonaws.com"]
    }
  }
}

resource "aws_iam_role_policy" "proxy" {
  name   = "${var.project_name}-proxy-policy"
  role   = aws_iam_role.proxy.id
  policy = data.aws_iam_policy_document.proxy_policy.json
}
```

</details>

<details>
<summary>TODO 4 -- RDS Proxy (database.tf)</summary>

```hcl
resource "aws_db_proxy" "this" {
  name          = "${var.project_name}-proxy"
  engine_family = "MYSQL"
  role_arn      = aws_iam_role.proxy.arn
  vpc_subnet_ids         = data.aws_subnets.default.ids
  vpc_security_group_ids = [aws_security_group.db.id]
  require_tls            = true
  auth {
    auth_scheme = "SECRETS"; iam_auth = "REQUIRED"
    secret_arn  = aws_secretsmanager_secret.db.arn
  }
  tags = { Name = "${var.project_name}-proxy" }
}

resource "aws_db_proxy_default_target_group" "this" {
  db_proxy_name = aws_db_proxy.this.name
  connection_pool_config {
    max_connections_percent = 100; max_idle_connections_percent = 50
    connection_borrow_timeout = 120
  }
}

resource "aws_db_proxy_target" "this" {
  db_proxy_name          = aws_db_proxy.this.name
  target_group_name      = aws_db_proxy_default_target_group.this.name
  db_instance_identifier = aws_db_instance.this.identifier
}
```

Update `outputs.tf`:

```hcl
output "proxy_endpoint" { value = aws_db_proxy.this.endpoint }
```

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify (deletion takes 5-10 minutes):

```bash
aws rds describe-db-proxies --db-proxy-name saa-ex64-proxy 2>&1 || echo "Proxy deleted"
aws rds describe-db-instances --db-instance-identifier saa-ex64-db 2>&1 || echo "RDS deleted"
```

---

## What's Next

Exercise 65 begins the **Security and IAM** section with **IAM policy types: identity-based, resource-based, and SCPs**. Understanding how these policy types interact and override each other is fundamental to every AWS architecture and appears heavily on the SAA-C03 exam.

---

## Summary

- **RDS Proxy** multiplexes thousands of Lambda connections into a small pool of database connections
- **Connection exhaustion** occurs when Lambda opens one connection per invocation -- `db.t3.micro` supports only ~60 connections
- **Secrets Manager integration** is required -- RDS Proxy reads credentials from Secrets Manager
- **IAM authentication** eliminates database passwords in Lambda code
- **Same VPC requirement** -- Lambda, RDS Proxy, and RDS must be in the same VPC with correct security group rules
- **Connection pinning** occurs when session state prevents multiplexing -- monitor `DatabaseConnectionsCurrentlySessionPinned`
- **Failover acceleration** -- RDS Proxy routes to new primary in seconds vs minutes for direct connections
- **Not needed when** Lambda concurrency is low (<20) or the database is Aurora Serverless v2

## Reference

- [RDS Proxy User Guide](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy.html)
- [RDS Proxy with Lambda](https://docs.aws.amazon.com/lambda/latest/dg/configuration-database.html)
- [RDS Proxy IAM Authentication](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy-iam-auth.html)
- [Terraform aws_db_proxy](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/db_proxy)

## Additional Resources

- [RDS Proxy Pinning](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy-managing.html#rds-proxy-pinning) -- which SQL statements cause pinning
- [RDS Proxy CloudWatch Metrics](https://docs.aws.amazon.com/AmazonRDS/latest/UserGuide/rds-proxy.monitoring.html) -- key metrics for proxy health
