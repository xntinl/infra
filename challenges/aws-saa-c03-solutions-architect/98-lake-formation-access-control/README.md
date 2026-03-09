# 98. Lake Formation Access Control

<!--
difficulty: advanced
concepts: [lake-formation, data-lake, fine-grained-access, column-level-permissions, row-level-security, cell-level-security, data-location-registration, lf-tags, governed-tables, data-permissions-model]
tools: [terraform, aws-cli]
estimated_time: 50m
bloom_level: evaluate
prerequisites: [97-glue-etl-data-catalog]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** Lake Formation itself is free. Costs come from underlying services: Glue Data Catalog, S3 storage, Athena queries. For this exercise with minimal data, total cost is ~$0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 97 (Glue ETL and Data Catalog) or equivalent knowledge
- Understanding of the Glue Data Catalog, crawlers, and table definitions
- Familiarity with IAM policies and permissions models
- Understanding of data governance concepts (column-level security, row-level filtering)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when Lake Formation's fine-grained permissions model provides value over IAM-based S3 access control
- **Design** a data lake governance architecture with column-level, row-level, and cell-level permissions
- **Analyze** the permission model transition from IAM policies to Lake Formation permissions and the risks of legacy IAM mode
- **Implement** data location registration, permission grants, and LF-Tags for scalable access control
- **Diagnose** common Lake Formation configuration issues, including the legacy IAM permissions mode trap

## Why This Matters

Lake Formation is the governance layer for AWS data lakes, and the SAA-C03 exam tests it as the answer to "how do you control who can access which columns and rows in your data lake?" Without Lake Formation, data lake access control relies on S3 bucket policies and IAM permissions, which operate at the bucket or prefix level. You cannot use IAM to say "this analyst can see the `name` and `email` columns but not the `salary` column in the employees table." Lake Formation provides this granularity.

The exam tests three permission levels:
- **Column-level**: grant access to specific columns in a table (e.g., hide `salary`, `ssn`)
- **Row-level**: filter rows based on conditions (e.g., only show data where `region = 'us-east'`)
- **Cell-level**: combine column and row filters (e.g., show `salary` only for your own department)

The most critical exam trap is the **legacy IAM permissions mode**. When you first enable Lake Formation, every table in the Glue Data Catalog has a special permission granted to `IAMAllowedPrincipals` that effectively bypasses Lake Formation. This means any IAM user with `glue:GetTable` and `s3:GetObject` permissions can access the data, ignoring Lake Formation's fine-grained permissions entirely. You must revoke this legacy permission for Lake Formation permissions to take effect.

## The Challenge

You are the data platform architect for a company with sensitive employee data in a data lake. Different teams need different access levels:

- **HR Team**: full access to all employee columns including salary and SSN
- **Analytics Team**: access to name, department, and hire date, but NOT salary or SSN
- **Regional Managers**: access to employees in their region only (row-level filter)
- **Auditors**: read-only access to all data with full audit trail

Design and implement Lake Formation permissions that enforce these access patterns.

### Architecture

```
Lake Formation → Column-Level (HR: all, Analysts: no salary/ssn)
               → Row-Level (Regional Mgrs: WHERE region='us-east')
               → Cell-Level (combine column + row filters)
               → Glue Data Catalog (metastore) + S3 (storage)
```

## Hints

<details>
<summary>Hint 1: Data Location Registration</summary>

Before Lake Formation can govern access to S3 data, you must register the S3 location with Lake Formation. This tells Lake Formation "I want to manage permissions for data in this location."

```hcl
resource "aws_lakeformation_resource" "data_location" {
  arn      = aws_s3_bucket.data_lake.arn
  role_arn = aws_iam_role.lakeformation.arn
}
```

The `role_arn` is a service role that Lake Formation uses to access the S3 data on behalf of users. When an analyst runs an Athena query, Lake Formation evaluates permissions and then uses this role to read the allowed columns from S3.

Without registration, Lake Formation cannot enforce permissions on the data. Users with direct S3 IAM permissions can bypass Lake Formation entirely.

</details>

<details>
<summary>Hint 2: Revoking Legacy IAM Permissions</summary>

The most important step is revoking the default `IAMAllowedPrincipals` permission. By default, Lake Formation grants ALL permissions to a special group called `IAMAllowedPrincipals`, which includes every IAM user and role. This is the legacy mode that makes Lake Formation permissions irrelevant.

```hcl
resource "aws_lakeformation_data_lake_settings" "this" {
  admins = [data.aws_iam_session_context.current.issuer_arn]

  create_database_default_permissions {
    permissions = []  # No default permissions on new databases
    principal   = "IAM_ALLOWED_PRINCIPALS"
  }

  create_table_default_permissions {
    permissions = []  # No default permissions on new tables
    principal   = "IAM_ALLOWED_PRINCIPALS"
  }
}
```

For existing tables, you must explicitly revoke:

```hcl
resource "aws_lakeformation_permissions" "revoke_legacy" {
  principal   = "IAM_ALLOWED_PRINCIPALS"
  permissions = ["ALL"]

  table {
    database_name = aws_glue_catalog_database.this.name
    name          = aws_glue_catalog_table.employees.name
  }
}
```

</details>

<details>
<summary>Hint 3: Column-Level Permissions</summary>

Grant access to specific columns by listing them in the `table_with_columns` block:

```hcl
resource "aws_lakeformation_permissions" "analytics_team" {
  principal   = aws_iam_role.analytics.arn
  permissions = ["SELECT"]

  table_with_columns {
    database_name = "data_lake"
    name          = "employees"
    column_names  = ["name", "department", "hire_date", "region"]
    # salary and ssn are NOT listed -- analytics team cannot see them
  }
}
```

When the analytics team runs `SELECT * FROM employees` in Athena, they only see the four allowed columns. If they try `SELECT salary FROM employees`, the query fails with a permissions error.

</details>

<details>
<summary>Hint 4: Row-Level Security with Data Filters</summary>

Row-level security uses data cell filters to restrict which rows a principal can see:

```hcl
resource "aws_lakeformation_permissions" "regional_manager" {
  principal   = aws_iam_role.regional_mgr_east.arn
  permissions = ["SELECT"]

  data_cells_filter {
    database_name     = "data_lake"
    table_name        = "employees"
    name              = "east-region-filter"
  }
}

resource "aws_lakeformation_data_cells_filter" "east_region" {
  table_data {
    database_name = "data_lake"
    table_name    = "employees"
    name          = "east-region-filter"

    column_names = ["name", "department", "hire_date"]

    row_filter {
      filter_expression = "region='us-east'"
    }
  }
}
```

This grants the regional manager access to only employees in the us-east region, and only the name, department, and hire_date columns -- combining column-level and row-level filtering.

</details>

## Step 1 -- Create the Project Files

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws    = { source = "hashicorp/aws", version = "~> 5.0" }
    random = { source = "hashicorp/random", version = "~> 3.0" }
  }
}

provider "aws" { region = var.region }
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
  default     = "saa-ex98"
}
```

### `storage.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }

resource "aws_s3_bucket" "data_lake" {
  bucket = "${var.project_name}-data-lake-${random_id.suffix.hex}", force_destroy = true
  tags   = { Name = "${var.project_name}-data-lake" }
}

resource "aws_s3_bucket_public_access_block" "data_lake" {
  bucket = aws_s3_bucket.data_lake.id
  block_public_acls = true, block_public_policy = true
  ignore_public_acls = true, restrict_public_buckets = true
}

resource "aws_s3_object" "employees" {
  bucket  = aws_s3_bucket.data_lake.id
  key     = "employees/employees.csv"
  content = <<-CSV
    employee_id,name,department,salary,ssn,hire_date,region
    E001,Alice Johnson,Engineering,145000,123-45-6789,2021-03-15,us-east
    E002,Bob Smith,Marketing,98000,234-56-7890,2020-07-22,us-west
    E003,Carol Williams,Engineering,152000,345-67-8901,2019-11-01,us-east
    E004,David Brown,HR,110000,456-78-9012,2022-01-10,eu-west
    E005,Eve Davis,Analytics,125000,567-89-0123,2021-08-30,us-east
    E006,Frank Miller,Engineering,160000,678-90-1234,2018-04-15,ap-east
    E007,Grace Wilson,Marketing,102000,789-01-2345,2023-02-28,us-west
    E008,Henry Taylor,Analytics,118000,890-12-3456,2020-12-05,us-east
  CSV
}

resource "aws_glue_catalog_database" "this" { name = "saa_ex98_data_lake" }

resource "aws_glue_catalog_table" "employees" {
  database_name = aws_glue_catalog_database.this.name
  name          = "employees"
  table_type    = "EXTERNAL_TABLE"
  parameters    = { "skip.header.line.count" = "1", "EXTERNAL" = "TRUE" }

  storage_descriptor {
    location      = "s3://${aws_s3_bucket.data_lake.id}/employees/"
    input_format  = "org.apache.hadoop.mapred.TextInputFormat"
    output_format = "org.apache.hadoop.hive.ql.io.HiveIgnoreKeyTextOutputFormat"

    ser_de_info {
      serialization_library = "org.apache.hadoop.hive.serde2.OpenCSVSerde"
      parameters = { "separatorChar" = ",", "quoteChar" = "\"" }
    }

    columns { name = "employee_id", type = "string" }
    columns { name = "name", type = "string" }
    columns { name = "department", type = "string" }
    columns { name = "salary", type = "string" }
    columns { name = "ssn", type = "string" }
    columns { name = "hire_date", type = "string" }
    columns { name = "region", type = "string" }
  }
}
```

## Step 2 -- Configure Lake Formation

### `iam.tf`

```hcl
data "aws_caller_identity" "current" {}
data "aws_iam_session_context" "current" {
  arn = data.aws_caller_identity.current.arn
}

data "aws_iam_policy_document" "lf_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service", identifiers = ["lakeformation.amazonaws.com"] }
  }
}

resource "aws_iam_role" "lakeformation" {
  name               = "${var.project_name}-lakeformation-role"
  assume_role_policy = data.aws_iam_policy_document.lf_assume.json
}

data "aws_iam_policy_document" "lf_s3" {
  statement {
    actions   = ["s3:GetObject", "s3:PutObject", "s3:DeleteObject", "s3:ListBucket", "s3:GetBucketLocation"]
    resources = [aws_s3_bucket.data_lake.arn, "${aws_s3_bucket.data_lake.arn}/*"]
  }
}

resource "aws_iam_role_policy" "lf_s3" {
  name = "s3-data-access", role = aws_iam_role.lakeformation.id
  policy = data.aws_iam_policy_document.lf_s3.json
}

data "aws_iam_policy_document" "team_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "AWS", identifiers = [data.aws_caller_identity.current.account_id] }
  }
}

resource "aws_iam_role" "hr_team"        { name = "${var.project_name}-hr-team",        assume_role_policy = data.aws_iam_policy_document.team_assume.json }
resource "aws_iam_role" "analytics_team" { name = "${var.project_name}-analytics-team", assume_role_policy = data.aws_iam_policy_document.team_assume.json }
```

### `lakeformation.tf`

```hcl
# CRITICAL: Override defaults to disable legacy IAMAllowedPrincipals.
# Without this, Lake Formation permissions are effectively ignored.
resource "aws_lakeformation_data_lake_settings" "this" {
  admins = [data.aws_iam_session_context.current.issuer_arn]
  create_database_default_permissions { permissions = [], principal = "IAM_ALLOWED_PRINCIPALS" }
  create_table_default_permissions    { permissions = [], principal = "IAM_ALLOWED_PRINCIPALS" }
}

# Register S3 location -- Lake Formation governs access to this data
resource "aws_lakeformation_resource" "data_lake" {
  arn = aws_s3_bucket.data_lake.arn, role_arn = aws_iam_role.lakeformation.arn
}
```

## Step 3 -- Create Permission Grants

### `permissions.tf`

```hcl
# HR Team: full access to all columns
resource "aws_lakeformation_permissions" "hr_full_access" {
  principal = aws_iam_role.hr_team.arn, permissions = ["SELECT"]
  table { database_name = aws_glue_catalog_database.this.name, name = aws_glue_catalog_table.employees.name }
}

# Analytics Team: no salary or SSN columns
resource "aws_lakeformation_permissions" "analytics_limited" {
  principal = aws_iam_role.analytics_team.arn, permissions = ["SELECT"]
  table_with_columns {
    database_name = aws_glue_catalog_database.this.name
    name          = aws_glue_catalog_table.employees.name
    column_names  = ["employee_id", "name", "department", "hire_date", "region"]
  }
}

# Database-level DESCRIBE for listing tables
resource "aws_lakeformation_permissions" "hr_db" {
  principal = aws_iam_role.hr_team.arn, permissions = ["DESCRIBE"]
  database { name = aws_glue_catalog_database.this.name }
}

resource "aws_lakeformation_permissions" "analytics_db" {
  principal = aws_iam_role.analytics_team.arn, permissions = ["DESCRIBE"]
  database { name = aws_glue_catalog_database.this.name }
}
```

## Step 4 -- Add Outputs and Apply

### `outputs.tf`

```hcl
output "data_lake_bucket" {
  value = aws_s3_bucket.data_lake.id
}

output "database" {
  value = aws_glue_catalog_database.this.name
}

output "hr_role_arn" {
  value = aws_iam_role.hr_team.arn
}

output "analytics_role_arn" {
  value = aws_iam_role.analytics_team.arn
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 5 -- Verify Permissions

```bash
DATABASE=$(terraform output -raw database)
HR_ROLE=$(terraform output -raw hr_role_arn)
ANALYTICS_ROLE=$(terraform output -raw analytics_role_arn)

# List permissions for each team
aws lakeformation list-permissions \
  --principal DataLakePrincipalIdentifier="$HR_ROLE" \
  --query 'PrincipalResourcePermissions[*].{Resource:Resource,Permissions:Permissions}' --output json

aws lakeformation list-permissions \
  --principal DataLakePrincipalIdentifier="$ANALYTICS_ROLE" \
  --query 'PrincipalResourcePermissions[*].{Resource:Resource,Permissions:Permissions}' --output json
```

### Lake Formation Permission Model Comparison

| Access Method | Granularity | Best For |
|---|---|---|
| **S3 bucket/IAM policies** | Bucket/prefix level | Simple storage access |
| **Lake Formation (table)** | Table level | Basic data lake governance |
| **Lake Formation (column)** | Column level | Hiding sensitive columns (salary, SSN) |
| **Lake Formation (row)** | Row level | Multi-tenant data isolation |
| **Lake Formation (cell)** | Column + row | Strictest governance (PII) |
| **LF-Tags** | Tag-based | Scalable policies for large catalogs (100+ tables) |

## Spot the Bug

A data platform team enables Lake Formation but users can still access all columns including sensitive data:

```hcl
resource "aws_lakeformation_data_lake_settings" "this" {
  admins = [data.aws_iam_session_context.current.issuer_arn]

  # Default permissions NOT overridden -- uses default settings
}

resource "aws_lakeformation_permissions" "analyst" {
  principal   = aws_iam_role.analyst.arn
  permissions = ["SELECT"]

  table_with_columns {
    database_name = "data_lake"
    name          = "employees"
    column_names  = ["name", "department"]
  }
}
```

The analyst can still run `SELECT salary, ssn FROM employees` and see all data.

<details>
<summary>Explain the bug</summary>

**Bug: The legacy IAM permissions mode is still active -- Lake Formation permissions are ignored.**

When Lake Formation is first configured, it grants a default permission to a special principal called `IAMAllowedPrincipals` on every database and table. This permission says "any IAM principal with appropriate Glue and S3 IAM policies can access this data." Since the analyst's IAM role has `glue:GetTable` and `s3:GetObject` permissions, they bypass Lake Formation entirely.

The `create_database_default_permissions` and `create_table_default_permissions` blocks are missing, so defaults grant ALL permissions to `IAM_ALLOWED_PRINCIPALS`.

**Fix:** Add empty permission arrays in the settings (see Step 2), and for existing tables, explicitly revoke: `principal = "IAM_ALLOWED_PRINCIPALS"`, `permissions = ["ALL"]` on each table.

**This is the #1 Lake Formation mistake.** Always verify: `aws lakeformation list-permissions --principal DataLakePrincipalIdentifier="IAM_ALLOWED_PRINCIPALS"`. If this returns any permissions, Lake Formation governance is not in effect.

</details>

## Verify What You Learned

```bash
# Verify Lake Formation settings disable legacy mode
aws lakeformation get-data-lake-settings \
  --query 'DataLakeSettings.{Admins:DataLakeAdmins[*].DataLakePrincipalIdentifier,DbDefaults:CreateDatabaseDefaultPermissions,TableDefaults:CreateTableDefaultPermissions}' \
  --output json
```

Expected: Empty `CreateDatabaseDefaultPermissions` and `CreateTableDefaultPermissions` arrays (legacy mode disabled).

```bash
# Verify data location is registered
aws lakeformation list-resources \
  --query 'ResourceInfoList[*].{Resource:ResourceArn,Role:RoleArn}' \
  --output table
```

Expected: S3 bucket ARN registered with the Lake Formation role.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 99 covers the **ECS vs EKS Decision Framework**, where you will evaluate container orchestration options on AWS -- transitioning from the data analytics domain to the containers domain and applying the same architectural decision-making skills to compute platform selection.

## Summary

- **Lake Formation** provides fine-grained access control for data lakes at column, row, and cell levels
- **Legacy IAM mode** (`IAMAllowedPrincipals`) is enabled by default and must be explicitly disabled for Lake Formation permissions to work
- **Data location registration** is required before Lake Formation can govern access to S3 data
- **Column-level permissions** hide sensitive columns (salary, SSN); **row-level security** filters rows by condition; **cell-level** combines both
- **LF-Tags** provide scalable permission management for large catalogs by tagging databases, tables, and columns
- **Permission model**: additive -- a principal has NO access unless explicitly granted; at least one admin required
- **IAMAllowedPrincipals check**: Always verify this principal has no permissions on governed tables

## Reference

- [AWS Lake Formation Developer Guide](https://docs.aws.amazon.com/lake-formation/latest/dg/what-is-lake-formation.html)
- [Lake Formation Permissions Reference](https://docs.aws.amazon.com/lake-formation/latest/dg/lf-permissions-reference.html)
- [Terraform aws_lakeformation_permissions](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lakeformation_permissions)
- [Terraform aws_lakeformation_data_lake_settings](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/lakeformation_data_lake_settings)

## Additional Resources

- [Upgrading to Lake Formation Permissions Model](https://docs.aws.amazon.com/lake-formation/latest/dg/upgrade-glue-lake-formation.html) -- migrating from legacy IAM mode to Lake Formation governance
- [LF-Tags for Scalable Governance](https://docs.aws.amazon.com/lake-formation/latest/dg/tag-based-access-control.html) -- tag-based access control for large data catalogs
- [Data Cell Filters](https://docs.aws.amazon.com/lake-formation/latest/dg/data-cell-filters.html) -- row-level and cell-level security configuration
- [Cross-Account Lake Formation](https://docs.aws.amazon.com/lake-formation/latest/dg/access-control-cross-account.html) -- sharing governed data across AWS accounts
