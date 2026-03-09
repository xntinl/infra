# 69. Secrets Manager vs Parameter Store

<!--
difficulty: intermediate
concepts: [secrets-manager, parameter-store, secret-rotation, lambda-rotation, cross-account-secrets, secure-string, secret-versioning, hierarchical-parameters, secret-caching]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: apply, analyze
prerequisites: [65-iam-policies-identity-resource-scp, 68-kms-key-management-rotation]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Secrets Manager charges $0.40/secret/month. Parameter Store Standard is free (up to 10,000 parameters). API calls are negligible. Total ~$0.01/hr for this exercise. Remember to run `terraform destroy` when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| Completed exercise 68 (KMS basics) | Understanding of encryption with KMS |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** both Secrets Manager secrets and Parameter Store parameters using Terraform.
2. **Evaluate** when to use Secrets Manager (automatic rotation, cross-account, higher cost) vs Parameter Store (free tier, hierarchical, no built-in rotation).
3. **Apply** Secrets Manager automatic rotation with a Lambda rotation function for RDS credentials.
4. **Analyze** the cost implications: Secrets Manager at $0.40/secret/month vs Parameter Store Standard (free) vs Parameter Store Advanced ($0.05/parameter/month).
5. **Design** a secrets management strategy that balances security (rotation frequency), cost (number of secrets), and operational complexity.

---

## Why This Matters

The SAA-C03 exam frequently presents scenarios where you must choose between Secrets Manager and Parameter Store. The decision framework is straightforward: use Secrets Manager when you need automatic rotation (especially for database credentials), cross-account secret sharing, or the secret is genuinely sensitive (API keys, passwords, certificates). Use Parameter Store when you need a free, hierarchical configuration store for non-secret values (feature flags, configuration URLs, environment-specific settings) or when cost matters and you have many parameters.

The critical distinction is rotation. Secrets Manager provides built-in Lambda-based rotation for RDS, Redshift, and DocumentDB credentials -- it automatically generates a new password, updates the database, and stores the new credential without any application downtime. Parameter Store SecureString can store secrets encrypted with KMS, but it provides no rotation mechanism in the free tier. If you store an RDS password in Parameter Store and manually rotate it on the database, you must also manually update the parameter -- creating a window where the stored credential is stale.

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
  default     = "saa-ex69"
}
```

### `secrets.tf`

```hcl
# ============================================================
# TODO 1: Create a Secrets Manager Secret
# ============================================================
# Create a secret for database credentials with automatic
# rotation configured.
#
# Requirements:
#   - Resource: aws_secretsmanager_secret
#   - name = "${var.project_name}/db-credentials"
#   - description = "RDS database credentials"
#
#   - Resource: aws_secretsmanager_secret_version
#   - secret_string = jsonencode({
#       username = "appuser"
#       password = "InitialPassword123!"
#       engine   = "mysql"
#       host     = "mydb.cluster-xxxx.us-east-1.rds.amazonaws.com"
#       port     = 3306
#       dbname   = "appdb"
#     })
#
# Note: In production, rotation would be configured with:
#   - Resource: aws_secretsmanager_secret_rotation
#   - rotation_lambda_arn = Lambda function ARN
#   - rotation_rules { automatically_after_days = 30 }
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret
# ============================================================


# ============================================================
# TODO 2: Create Parameter Store Parameters
# ============================================================
# Create a hierarchy of parameters for application configuration.
# Parameter Store supports hierarchical paths like a file system.
#
# Requirements:
#   - Resource: aws_ssm_parameter (3 parameters)
#
#   a) String parameter (not encrypted):
#      - name = "/${var.project_name}/config/app-url"
#      - type = "String"
#      - value = "https://api.example.com"
#
#   b) StringList parameter:
#      - name = "/${var.project_name}/config/allowed-origins"
#      - type = "StringList"
#      - value = "https://app.example.com,https://admin.example.com"
#
#   c) SecureString parameter (encrypted with KMS):
#      - name = "/${var.project_name}/secrets/api-key"
#      - type = "SecureString"
#      - value = "sk-test-1234567890abcdef"
#      (Uses the default aws/ssm KMS key unless you specify key_id)
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssm_parameter
# ============================================================


# ============================================================
# TODO 3: Compare Retrieval Methods
# ============================================================
# After terraform apply, compare how applications retrieve
# values from each service:
#
#   a) Retrieve Secrets Manager secret:
#      aws secretsmanager get-secret-value \
#        --secret-id saa-ex69/db-credentials \
#        --query SecretString --output text | python3 -m json.tool
#
#   b) Retrieve individual parameter:
#      aws ssm get-parameter \
#        --name /saa-ex69/config/app-url \
#        --query Parameter.Value --output text
#
#   c) Retrieve encrypted parameter (with decryption):
#      aws ssm get-parameter \
#        --name /saa-ex69/secrets/api-key \
#        --with-decryption \
#        --query Parameter.Value --output text
#
#   d) Retrieve all parameters by path (hierarchical):
#      aws ssm get-parameters-by-path \
#        --path /saa-ex69/config \
#        --query 'Parameters[*].{Name:Name,Value:Value}' \
#        --output table
#
#   e) Retrieve all parameters (including encrypted):
#      aws ssm get-parameters-by-path \
#        --path /saa-ex69 \
#        --recursive \
#        --with-decryption \
#        --query 'Parameters[*].{Name:Name,Type:Type,Value:Value}' \
#        --output table
#
# Docs: https://docs.aws.amazon.com/systems-manager/latest/userguide/sysman-paramstore-working.html
# ============================================================
```

### `outputs.tf`

```hcl
output "secret_arn" {
  value = "Set after TODO 1 implementation"
}

output "parameter_prefix" {
  value = "/${var.project_name}"
}
```

---

## Decision Table

| Criterion | Secrets Manager | Parameter Store (Standard) | Parameter Store (Advanced) |
|---|---|---|---|
| **Cost per secret/parameter** | $0.40/month | Free (up to 10,000) | $0.05/month |
| **API call cost** | $0.05/10K calls | Free (standard throughput) | $0.05/10K calls |
| **Max size** | 64 KB | 4 KB | 8 KB |
| **Max count** | Unlimited | 10,000 per account/region | 100,000 per account/region |
| **Automatic rotation** | Yes (built-in Lambda rotation) | No | No (policies only) |
| **Cross-account sharing** | Yes (resource policy) | No (same account only) | No (same account only) |
| **Versioning** | Automatic (AWSCURRENT, AWSPREVIOUS) | Yes (version labels) | Yes (version labels) |
| **Hierarchical paths** | No (flat namespace) | Yes (/app/env/key) | Yes (/app/env/key) |
| **Encryption** | Always encrypted (KMS) | Optional (SecureString) | Optional (SecureString) |
| **Notifications** | EventBridge events | EventBridge events | EventBridge events |
| **CloudFormation/TF resolution** | Dynamic reference | Dynamic reference | Dynamic reference |

### When to Use Secrets Manager

- Database credentials that need automatic rotation
- API keys, OAuth tokens, certificates for third-party services
- Secrets shared across AWS accounts
- Compliance requirements mandate rotation (PCI-DSS, SOC 2)

### When to Use Parameter Store

- Application configuration (URLs, feature flags, environment variables)
- Non-sensitive values that do not need encryption
- Cost-sensitive environments with many parameters (free tier)
- Hierarchical configuration organized by environment/service

---

## Spot the Bug

A team stores their RDS password in Parameter Store SecureString and manually rotates it quarterly:

```hcl
resource "aws_ssm_parameter" "db_password" {
  name  = "/production/database/password"
  type  = "SecureString"
  value = "OldPassword123!"  # Manually updated each quarter
}

resource "aws_db_instance" "production" {
  identifier = "production-db"
  engine     = "mysql"
  username   = "admin"
  password   = aws_ssm_parameter.db_password.value
  # ... other config
}
```

Their quarterly rotation process:
1. Change password on RDS: `aws rds modify-db-instance --master-user-password "NewPassword456!"`
2. Update Parameter Store: `aws ssm put-parameter --name /production/database/password --value "NewPassword456!" --overwrite`

Between steps 1 and 2, the application experiences authentication failures.

<details>
<summary>Explain the bug</summary>

**The manual rotation process has a gap between updating the database password and updating Parameter Store.** Between step 1 (RDS password changed) and step 2 (Parameter Store updated), the application reads the old password from Parameter Store and fails to authenticate to RDS.

Even if the steps are executed seconds apart, any application instance that caches the Parameter Store value or opens a new connection during this window will fail. In a production environment with multiple application instances, this creates intermittent failures that are difficult to diagnose.

Additional issues:
- **No rollback plan:** If step 2 fails (API throttling, permissions error), the application stays broken with a stale password in Parameter Store.
- **Terraform state drift:** The password in `terraform.tfstate` no longer matches the actual RDS password after manual rotation, causing Terraform to attempt to reset it on the next apply.
- **No versioning strategy:** Parameter Store stores the value but there is no automatic AWSCURRENT/AWSPREVIOUS staging that allows the database to accept both old and new passwords during rotation.

**Fix:** Use Secrets Manager with automatic rotation:

```hcl
resource "aws_secretsmanager_secret" "db_password" {
  name = "/production/database/password"
}

resource "aws_secretsmanager_secret_rotation" "db_password" {
  secret_id           = aws_secretsmanager_secret.db_password.id
  rotation_lambda_arn = aws_lambda_function.rotation.arn

  rotation_rules {
    automatically_after_days = 90
  }
}
```

Secrets Manager rotation uses a two-phase process:
1. **Create secret:** Generate new password, store as AWSPENDING
2. **Set secret:** Update the database with the new password
3. **Test secret:** Verify the new password works
4. **Finish secret:** Promote AWSPENDING to AWSCURRENT, demote old to AWSPREVIOUS

If any step fails, the rotation rolls back. The database accepts both old and new passwords during the rotation window (multi-user rotation strategy) or the application retries with the new password (single-user rotation).

</details>

---

## Verify What You Learned

1. **Deploy the resources:**
   ```bash
   terraform init && terraform apply -auto-approve
   ```

2. **Verify Secrets Manager secret:**
   ```bash
   aws secretsmanager describe-secret \
     --secret-id saa-ex69/db-credentials \
     --query '{Name:Name,ARN:ARN,RotationEnabled:RotationEnabled}' \
     --output json
   ```
   Expected: Secret exists with RotationEnabled = `false` (no rotation Lambda in this exercise).

3. **Retrieve secret value:**
   ```bash
   aws secretsmanager get-secret-value \
     --secret-id saa-ex69/db-credentials \
     --query SecretString --output text | python3 -m json.tool
   ```
   Expected: JSON with username, password, engine, host, port, dbname.

4. **Verify Parameter Store parameters:**
   ```bash
   aws ssm get-parameters-by-path \
     --path /saa-ex69 \
     --recursive \
     --with-decryption \
     --query 'Parameters[*].{Name:Name,Type:Type}' \
     --output table
   ```
   Expected: Three parameters (String, StringList, SecureString).

5. **Verify Terraform state:**
   ```bash
   terraform plan
   ```
   Expected: `No changes. Your infrastructure matches the configuration.`

---

## Solutions

<details>
<summary>TODO 1 -- Secrets Manager Secret (secrets.tf)</summary>

```hcl
resource "aws_secretsmanager_secret" "db_credentials" {
  name        = "${var.project_name}/db-credentials"
  description = "RDS database credentials"

  tags = { Name = "${var.project_name}-db-credentials" }
}

resource "aws_secretsmanager_secret_version" "db_credentials" {
  secret_id = aws_secretsmanager_secret.db_credentials.id
  secret_string = jsonencode({
    username = "appuser"
    password = "InitialPassword123!"
    engine   = "mysql"
    host     = "mydb.cluster-xxxx.us-east-1.rds.amazonaws.com"
    port     = 3306
    dbname   = "appdb"
  })
}

```

Update `outputs.tf`:

```hcl
output "secret_arn" {
  value = aws_secretsmanager_secret.db_credentials.arn
}
```

The secret is automatically encrypted with the default `aws/secretsmanager` KMS key. To use a customer managed key, add `kms_key_id = aws_kms_key.this.arn`.

</details>

<details>
<summary>TODO 2 -- Parameter Store Parameters (secrets.tf)</summary>

```hcl
resource "aws_ssm_parameter" "app_url" {
  name  = "/${var.project_name}/config/app-url"
  type  = "String"
  value = "https://api.example.com"

  tags = { Name = "${var.project_name}-app-url" }
}

resource "aws_ssm_parameter" "allowed_origins" {
  name  = "/${var.project_name}/config/allowed-origins"
  type  = "StringList"
  value = "https://app.example.com,https://admin.example.com"

  tags = { Name = "${var.project_name}-allowed-origins" }
}

resource "aws_ssm_parameter" "api_key" {
  name  = "/${var.project_name}/secrets/api-key"
  type  = "SecureString"
  value = "sk-test-1234567890abcdef"

  tags = { Name = "${var.project_name}-api-key" }
}
```

The hierarchical naming (`/project/category/key`) enables batch retrieval with `get-parameters-by-path`. This is useful for loading all configuration for a service at startup.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: Secrets Manager has a default 30-day recovery window. To delete immediately (not recommended in production):

```bash
aws secretsmanager delete-secret \
  --secret-id saa-ex69/db-credentials \
  --force-delete-without-recovery
```

Verify:

```bash
aws secretsmanager describe-secret \
  --secret-id saa-ex69/db-credentials 2>&1 || echo "Secret deleted"
aws ssm get-parameter \
  --name /saa-ex69/config/app-url 2>&1 || echo "Parameters deleted"
```

---

## What's Next

Exercise 70 covers **AWS Certificate Manager (ACM) for HTTPS**. You will request a public TLS certificate, validate it via DNS, and attach it to an ALB for HTTPS termination -- the standard pattern for securing web traffic that appears on nearly every architecture diagram.

---

## Summary

- **Secrets Manager** is for secrets that need automatic rotation, cross-account sharing, or compliance-mandated credential lifecycle management ($0.40/secret/month)
- **Parameter Store Standard** is for configuration values and basic secrets without rotation (free for up to 10,000 parameters)
- **Automatic rotation** is the primary reason to choose Secrets Manager -- it handles the two-phase rotation process (update secret, update database) atomically
- **Manual rotation creates a gap** between updating the database and updating the stored credential -- applications fail during this window
- **Hierarchical paths** in Parameter Store enable batch retrieval with `get-parameters-by-path` -- organize by `/environment/service/key`
- **SecureString parameters** are encrypted with KMS but provide no rotation mechanism -- suitable for secrets that change infrequently
- **Cross-account access** is only available in Secrets Manager via resource policies -- Parameter Store secrets cannot be shared across accounts
- **Cost matters at scale:** 100 secrets in Secrets Manager = $40/month; 100 parameters in Parameter Store Standard = $0/month
- **Both services integrate** with CloudFormation, Terraform, and ECS/EKS for dynamic secret resolution at deployment time
- **Secrets Manager versioning** (AWSCURRENT, AWSPENDING, AWSPREVIOUS) enables zero-downtime rotation -- applications always read AWSCURRENT

## Reference

- [Secrets Manager User Guide](https://docs.aws.amazon.com/secretsmanager/latest/userguide/intro.html)
- [Parameter Store User Guide](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-parameter-store.html)
- [Secrets Manager Rotation](https://docs.aws.amazon.com/secretsmanager/latest/userguide/rotating-secrets.html)
- [Terraform aws_secretsmanager_secret](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/secretsmanager_secret)

## Additional Resources

- [Secrets Manager Rotation Templates](https://docs.aws.amazon.com/secretsmanager/latest/userguide/reference_available-rotation-templates.html) -- pre-built Lambda rotation functions for RDS, Redshift, DocumentDB
- [Parameter Store vs Secrets Manager](https://docs.aws.amazon.com/systems-manager/latest/userguide/systems-manager-parameter-store.html#parameter-store-vs-secrets-manager) -- official AWS comparison
- [Secrets Manager Caching](https://docs.aws.amazon.com/secretsmanager/latest/userguide/retrieving-secrets_cache-ref.html) -- client-side caching to reduce API calls and latency
- [Parameter Store Parameter Policies](https://docs.aws.amazon.com/systems-manager/latest/userguide/parameter-store-policies.html) -- expiration notifications for Advanced parameters
