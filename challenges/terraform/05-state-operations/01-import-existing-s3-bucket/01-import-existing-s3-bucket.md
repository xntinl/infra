# 21. Import an Existing S3 Bucket

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 20 (last exercise of section 04)

## Learning Objectives

After completing this exercise, you will be able to:

- Import a pre-existing AWS resource into Terraform state using `terraform import`
- Align HCL configuration with real infrastructure to eliminate plan drift
- Verify that an imported resource is fully managed by Terraform

## Why Import Existing Resources

Most organizations do not start with a greenfield Terraform setup. They have resources that were created manually through the AWS Console, CLI scripts, or other tools. Before Terraform can manage those resources, it needs to know they exist -- and that knowledge lives in the state file.

`terraform import` bridges this gap. It takes a resource that already exists in your cloud account and records it in the Terraform state, associating it with a resource block you have written in HCL. After the import, Terraform treats the resource as if it had created it, and all future changes go through `plan` and `apply`.

However, importing is only half the work. The HCL configuration you write must match the real resource exactly. If there are differences -- a tag you did not include, a setting that was enabled in the console -- `terraform plan` will show drift. Resolving that drift is the real skill: you adjust your code until the plan says "No changes."

## Step 1 -- Create the Bucket Manually

Use the AWS CLI to create a bucket outside of Terraform. The timestamp suffix ensures a globally unique name.

```bash
BUCKET_NAME="kata-import-exercise-$(date +%s)"
aws s3 mb s3://$BUCKET_NAME
echo "Created bucket: $BUCKET_NAME"
```

Save the bucket name -- you will need it in the next steps.

## Step 2 -- Write the Terraform Configuration

Create a `main.tf` file with the provider and the resource block that will adopt the bucket.

```hcl
# main.tf

terraform {
  required_version = ">= 1.7"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = "us-east-1"
}

resource "aws_s3_bucket" "imported" {
  bucket = "kata-import-exercise-XXXXXXXXXX"  # Replace with your actual bucket name
}
```

## Step 3 -- Initialize and Import

```bash
terraform init
terraform import aws_s3_bucket.imported kata-import-exercise-XXXXXXXXXX
```

You should see:

```
aws_s3_bucket.imported: Importing from ID "kata-import-exercise-XXXXXXXXXX"...
aws_s3_bucket.imported: Import prepared!
  Prepared aws_s3_bucket for import
aws_s3_bucket.imported: Refreshing state...

Import successful!

The resources that were imported are shown above. These resources are now in
your Terraform state and will henceforth be managed by Terraform.
```

## Step 4 -- Inspect the Imported State

Check what Terraform now knows about the bucket:

```bash
terraform state show aws_s3_bucket.imported
```

This displays every attribute Terraform recorded from the real resource.

## Step 5 -- Align Configuration and Eliminate Drift

Run a plan to see if there is drift between your HCL and the real resource:

```bash
terraform plan
```

**Scenario A -- No drift:** The plan shows "No changes. Your infrastructure matches the configuration." You are done.

**Scenario B -- Drift detected:** The plan shows changes. This means the real bucket has attributes your HCL does not account for. Read the plan output carefully, update your `main.tf` to match, and run `terraform plan` again. Repeat until you see "No changes."

## Step 6 -- Clean Up

Once aligned, destroy the resource:

```bash
terraform destroy
```

Expected output includes:

```
aws_s3_bucket.imported: Destroying...
aws_s3_bucket.imported: Destruction complete after 1s

Destroy complete! Resources: 1 destroyed.
```

## Common Mistakes

### Forgetting to write the resource block before importing

Running `terraform import aws_s3_bucket.imported <name>` without having a `resource "aws_s3_bucket" "imported"` block in your HCL produces an error: "resource address aws_s3_bucket.imported does not exist in the configuration." Always write the resource block first, then import.

### Assuming import makes the plan clean automatically

A successful import does not mean your configuration is aligned. Import only writes the real resource into state. If your HCL is missing attributes (tags, ACLs, versioning settings), the next `plan` will show drift. You must review the plan and update your code until it shows "No changes."

## Verify What You Learned

1. Confirm the import succeeded:

```bash
terraform state list
```

Expected output:

```
aws_s3_bucket.imported
```

2. Inspect the resource in state:

```bash
terraform state show aws_s3_bucket.imported | head -5
```

Expected output (attributes will vary):

```
# aws_s3_bucket.imported:
resource "aws_s3_bucket" "imported" {
    arn                         = "arn:aws:s3:::kata-import-exercise-XXXXXXXXXX"
    bucket                      = "kata-import-exercise-XXXXXXXXXX"
    bucket_domain_name          = "kata-import-exercise-XXXXXXXXXX.s3.amazonaws.com"
```

3. Confirm no drift remains:

```bash
terraform plan
```

Expected output:

```
No changes. Your infrastructure matches the configuration.
```

4. Confirm the bucket is destroyed after cleanup:

```bash
terraform destroy -auto-approve
aws s3 ls s3://kata-import-exercise-XXXXXXXXXX 2>&1
```

Expected output from the second command (bucket no longer exists):

```
An error occurred (NoSuchBucket) when calling the ListObjectsV2 operation: The specified bucket does not exist
```

## What's Next

In the next exercise, you will learn how to refactor resources into modules using `terraform state mv` -- moving resources within the state file without destroying them in the cloud.

## Reference

- [terraform import Command](https://developer.hashicorp.com/terraform/cli/commands/import)
- [State Import Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/state-import)

## Additional Resources

- [Terrateam: How to Import an S3 Bucket into Terraform](https://terrateam.io/blog/aws-s3-import) -- practical guide specific to S3 bucket imports including post-import alignment
- [Spacelift: Importing Existing Infrastructure into Terraform](https://spacelift.io/blog/importing-existing-infrastructure-into-terraform) -- walkthrough of the import process with examples for multiple AWS resource types
- [AWS Workshop: Adopting Existing Resources](https://catalog.workshops.aws/terraform101/en-US/6-import) -- hands-on AWS workshop for adopting existing infrastructure with Terraform
