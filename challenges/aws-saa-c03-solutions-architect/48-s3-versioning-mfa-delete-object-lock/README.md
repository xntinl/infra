# 48. S3 Versioning, MFA Delete, and Object Lock

<!--
difficulty: basic
concepts: [s3-versioning, mfa-delete, object-lock, worm, governance-mode, compliance-mode, retention-period, legal-hold, version-id]
tools: [terraform, aws-cli]
estimated_time: 35m
bloom_level: understand
prerequisites: [47-s3-storage-classes-lifecycle-policies]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** S3 storage with versioning keeps all object versions, which increases storage costs. For this exercise with small test objects, cost is negligible (~$0.01/hr). Object Lock buckets cannot be deleted until all locked objects expire. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 47 (S3 Storage Classes) or equivalent knowledge
- Understanding of S3 bucket and object operations

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how S3 versioning preserves every version of an object, including delete markers
- **Describe** the difference between a soft delete (delete marker) and a permanent delete (version-specific delete)
- **Identify** when MFA Delete is required and how it protects against accidental or malicious permanent deletions
- **Distinguish** between Object Lock governance mode (bypassed with special permissions) and compliance mode (immutable, not even root can delete)
- **Construct** a versioning-enabled bucket with Object Lock and retention policies using Terraform
- **Compare** retention periods vs legal holds and when to use each

## Why This Matters

Data protection is a core SAA-C03 exam domain, and S3 versioning combined with Object Lock represents the strongest data protection mechanism AWS offers. Versioning alone prevents accidental overwrites -- every PUT creates a new version, and you can always retrieve previous versions. But versioning does not prevent permanent deletes: anyone with `s3:DeleteObject` permission can delete a specific version ID, permanently removing that data.

MFA Delete adds a second authentication factor to permanent deletes and to disabling versioning itself. This protects against compromised IAM credentials -- even if an attacker gains admin access, they cannot permanently delete data without the MFA device. Object Lock goes further by implementing WORM (Write Once Read Many) storage. In compliance mode, no one -- not even the root account -- can delete or overwrite a locked object before its retention period expires. This is required for regulations like SEC Rule 17a-4, HIPAA, and FINRA. The exam tests your ability to choose the right combination of these features for specific compliance scenarios.

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
  default     = "saa-ex48"
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" {
  byte_length = 4
}

# ------------------------------------------------------------------
# Versioned bucket: every PUT creates a new version. Deletes add a
# delete marker (soft delete) instead of removing the object.
#
# Note: once enabled, versioning cannot be disabled -- only
# suspended. Suspension stops creating new versions but preserves
# all existing versions. Existing versions remain accessible by
# version ID forever (unless explicitly deleted).
# ------------------------------------------------------------------
resource "aws_s3_bucket" "versioned" {
  bucket        = "${var.project_name}-versioned-${random_id.suffix.hex}"
  force_destroy = true

  tags = { Name = "${var.project_name}-versioned" }
}

resource "aws_s3_bucket_versioning" "versioned" {
  bucket = aws_s3_bucket.versioned.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "versioned" {
  bucket = aws_s3_bucket.versioned.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Object Lock bucket: WORM storage. object_lock_enabled must be set
# at bucket creation and cannot be changed later.
#
# Object Lock requires versioning, which S3 enables automatically
# when Object Lock is enabled.
#
# Two modes:
#   Governance: users with s3:BypassGovernanceRetention permission
#               can delete or overwrite locked objects.
#   Compliance: NO ONE can delete or overwrite, not even root.
#               The retention period cannot be shortened.
# ------------------------------------------------------------------
resource "aws_s3_bucket" "locked" {
  bucket              = "${var.project_name}-locked-${random_id.suffix.hex}"
  object_lock_enabled = true

  tags = { Name = "${var.project_name}-object-lock" }
}

resource "aws_s3_bucket_versioning" "locked" {
  bucket = aws_s3_bucket.locked.id
  versioning_configuration {
    status = "Enabled"
  }
}

resource "aws_s3_bucket_public_access_block" "locked" {
  bucket = aws_s3_bucket.locked.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# ------------------------------------------------------------------
# Default retention: applies Object Lock to every new object in
# the bucket automatically. Objects can still have individual
# retention settings that override the default.
#
# Using GOVERNANCE mode for this exercise so we can delete objects
# during cleanup. In production compliance scenarios, use COMPLIANCE.
# ------------------------------------------------------------------
resource "aws_s3_bucket_object_lock_configuration" "locked" {
  bucket = aws_s3_bucket.locked.id

  rule {
    default_retention {
      mode = "GOVERNANCE"
      days = 1
    }
  }
}
```

### `outputs.tf`

```hcl
output "versioned_bucket" {
  value = aws_s3_bucket.versioned.id
}

output "locked_bucket" {
  value = aws_s3_bucket.locked.id
}
```

## Step 2 -- Deploy

```bash
terraform init
terraform apply -auto-approve
```

## Step 3 -- Demonstrate Versioning Behavior

```bash
VERSIONED=$(terraform output -raw versioned_bucket)

# Upload version 1
echo "version 1 content" | aws s3 cp - "s3://$VERSIONED/document.txt"

# Upload version 2 (overwrites, but version 1 is preserved)
echo "version 2 content" | aws s3 cp - "s3://$VERSIONED/document.txt"

# Upload version 3
echo "version 3 content" | aws s3 cp - "s3://$VERSIONED/document.txt"

# List all versions
aws s3api list-object-versions \
  --bucket "$VERSIONED" \
  --prefix "document.txt" \
  --query 'Versions[*].{VersionId:VersionId,IsLatest:IsLatest,LastModified:LastModified,Size:Size}' \
  --output table
```

You should see three versions, with only one marked `IsLatest = true`.

```bash
# Retrieve a specific old version
OLDEST_VERSION=$(aws s3api list-object-versions \
  --bucket "$VERSIONED" \
  --prefix "document.txt" \
  --query 'Versions[-1].VersionId' --output text)

aws s3api get-object \
  --bucket "$VERSIONED" \
  --key "document.txt" \
  --version-id "$OLDEST_VERSION" \
  old-version.txt

cat old-version.txt
# Output: "version 1 content"
```

## Step 4 -- Demonstrate Delete Markers

```bash
VERSIONED=$(terraform output -raw versioned_bucket)

# "Delete" the object (adds a delete marker, does NOT remove versions)
aws s3 rm "s3://$VERSIONED/document.txt"

# Try to GET -- returns 404 because the delete marker hides the object
aws s3 cp "s3://$VERSIONED/document.txt" - 2>&1 || echo "Object appears deleted"

# But all versions still exist!
aws s3api list-object-versions \
  --bucket "$VERSIONED" \
  --prefix "document.txt" \
  --query '{Versions:Versions[*].VersionId,DeleteMarkers:DeleteMarkers[*].VersionId}' \
  --output json

# Remove the delete marker to "undelete" the object
DELETE_MARKER=$(aws s3api list-object-versions \
  --bucket "$VERSIONED" \
  --prefix "document.txt" \
  --query 'DeleteMarkers[0].VersionId' --output text)

aws s3api delete-object \
  --bucket "$VERSIONED" \
  --key "document.txt" \
  --version-id "$DELETE_MARKER"

# Object is accessible again
aws s3 cp "s3://$VERSIONED/document.txt" - 2>&1
# Output: "version 3 content"
```

## Step 5 -- Demonstrate Object Lock

```bash
LOCKED=$(terraform output -raw locked_bucket)

# Upload an object (default GOVERNANCE retention applies)
echo "protected data" | aws s3 cp - "s3://$LOCKED/protected.txt"

# Check the retention settings
VERSION_ID=$(aws s3api list-object-versions \
  --bucket "$LOCKED" \
  --prefix "protected.txt" \
  --query 'Versions[0].VersionId' --output text)

aws s3api get-object-retention \
  --bucket "$LOCKED" \
  --key "protected.txt" \
  --version-id "$VERSION_ID" \
  --output json

# Try to delete -- this will FAIL because of Object Lock
aws s3api delete-object \
  --bucket "$LOCKED" \
  --key "protected.txt" \
  --version-id "$VERSION_ID" 2>&1 || echo "Delete blocked by Object Lock"

# In GOVERNANCE mode, you can bypass with the right permission:
aws s3api delete-object \
  --bucket "$LOCKED" \
  --key "protected.txt" \
  --version-id "$VERSION_ID" \
  --bypass-governance-retention 2>&1
```

### Object Lock Decision Framework

| Feature | Governance Mode | Compliance Mode | Legal Hold |
|---|---|---|---|
| Who can delete | Users with `s3:BypassGovernanceRetention` | No one (including root) | No one until removed |
| Retention period | Can be extended or shortened by privileged users | Can only be extended, never shortened | No expiration |
| Use case | Internal data protection, testing | Regulatory compliance (SEC, HIPAA) | Litigation hold |
| Can be removed | Yes, with bypass permission | No, must wait for expiration | Yes, with `s3:PutObjectLegalHold` |

## Common Mistakes

### 1. Confusing soft delete with permanent delete

**Wrong approach:** Assuming `aws s3 rm` permanently removes data:

```bash
aws s3 rm "s3://my-bucket/important-file.txt"
echo "File is gone forever"
```

**What happens:** With versioning enabled, this only adds a delete marker. All previous versions remain in the bucket, consuming storage and incurring costs. The object appears deleted to normal GET operations, but all data is still there.

**Fix:** To permanently delete, you must delete each version individually:

```bash
# List and delete all versions
aws s3api list-object-versions \
  --bucket my-bucket --prefix "important-file.txt" \
  --query 'Versions[*].VersionId' --output text | \
  xargs -I{} aws s3api delete-object \
    --bucket my-bucket --key "important-file.txt" --version-id {}
```

### 2. Enabling Object Lock on an existing bucket

**Wrong approach:** Trying to add Object Lock after bucket creation:

```hcl
resource "aws_s3_bucket" "existing" {
  bucket              = "my-existing-bucket"
  object_lock_enabled = true  # ERROR: cannot add to existing bucket
}
```

**What happens:** Object Lock can only be enabled at bucket creation time. You cannot add it to an existing bucket. Note: AWS now allows enabling Object Lock on existing buckets via the console and API, but the objects uploaded before enabling are not retroactively locked.

**Fix:** Create a new bucket with Object Lock enabled and migrate objects, or enable Object Lock on the existing bucket and apply retention to new objects going forward.

### 3. Using Compliance mode in non-production environments

**Wrong approach:** Setting compliance mode with a long retention during testing:

```hcl
rule {
  default_retention {
    mode  = "COMPLIANCE"
    years = 7
  }
}
```

**What happens:** You cannot delete the objects, shorten the retention, or delete the bucket for 7 years. Not even the root account can override compliance mode. The bucket and its objects will incur storage charges for the full retention period.

**Fix:** Use Governance mode for testing and development. Reserve Compliance mode for production regulatory requirements where immutability is legally mandated.

## Verify What You Learned

```bash
VERSIONED=$(terraform output -raw versioned_bucket)
LOCKED=$(terraform output -raw locked_bucket)

# Verify versioning is enabled
aws s3api get-bucket-versioning --bucket "$VERSIONED" \
  --query 'Status' --output text
```

Expected: `Enabled`

```bash
# Verify Object Lock is enabled
aws s3api get-object-lock-configuration --bucket "$LOCKED" \
  --query 'ObjectLockConfiguration.{Enabled:ObjectLockEnabled,Mode:Rule.DefaultRetention.Mode,Days:Rule.DefaultRetention.Days}' \
  --output table
```

Expected: `Enabled = Enabled`, `Mode = GOVERNANCE`, `Days = 1`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
# For the Object Lock bucket, must bypass governance retention
LOCKED=$(terraform output -raw locked_bucket)
aws s3api list-object-versions --bucket "$LOCKED" \
  --query 'Versions[*].[Key,VersionId]' --output text | \
  while read key vid; do
    aws s3api delete-object --bucket "$LOCKED" --key "$key" \
      --version-id "$vid" --bypass-governance-retention 2>/dev/null
  done

terraform destroy -auto-approve
```

Verify buckets are deleted:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

Exercise 49 covers **S3 Cross-Region Replication (CRR)**, where you will replicate objects between buckets in different AWS regions for disaster recovery. You will configure replication rules, IAM roles for cross-region access, and filter rules to selectively replicate specific prefixes -- building on the versioning knowledge from this exercise since CRR requires versioning on both source and destination buckets.

## Summary

- **S3 versioning** preserves every version of an object; overwrites create new versions, deletes add delete markers
- **Delete markers** hide the current version but do not remove data -- remove the delete marker to "undelete"
- **Permanent deletion** requires deleting a specific version ID, which removes that version forever
- **MFA Delete** requires multi-factor authentication to permanently delete object versions or disable versioning
- **Object Lock** implements WORM storage -- objects cannot be deleted or overwritten during the retention period
- **Governance mode** can be bypassed by users with `s3:BypassGovernanceRetention` permission (testing, internal controls)
- **Compliance mode** cannot be bypassed by anyone, including the root account (regulatory requirements)
- **Legal Hold** prevents deletion indefinitely until explicitly removed -- independent of retention period
- **Versioning cannot be disabled**, only suspended -- all existing versions remain accessible forever
- **Object Lock must be enabled at bucket creation** -- it cannot be retroactively added to existing buckets

## Reference

- [S3 Versioning](https://docs.aws.amazon.com/AmazonS3/latest/userguide/Versioning.html)
- [S3 Object Lock](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock.html)
- [MFA Delete](https://docs.aws.amazon.com/AmazonS3/latest/userguide/MultiFactorAuthenticationDelete.html)
- [Terraform aws_s3_bucket_object_lock_configuration](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/s3_bucket_object_lock_configuration)

## Additional Resources

- [Object Lock Governance vs Compliance](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock-overview.html) -- detailed comparison of the two retention modes
- [SEC Rule 17a-4 Compliance](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lock-managing.html) -- how Object Lock compliance mode meets financial services regulations
- [Versioning Cost Considerations](https://docs.aws.amazon.com/AmazonS3/latest/userguide/versioning-workflows.html) -- managing storage costs when versioning creates many versions
- [S3 Bucket Key for SSE-KMS with Object Lock](https://docs.aws.amazon.com/AmazonS3/latest/userguide/bucket-key.html) -- reducing KMS costs when using encryption with Object Lock
