# 39. Assume Role for Cross-Account Access

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 38 (Multiple AWS Providers for Multi-Region)
- Access to two AWS accounts (Account A and Account B) or the ability to simulate them

## Learning Objectives

After completing this exercise, you will be able to:

- Configure the AWS provider's `assume_role` block to deploy resources into a different account
- Create an IAM role with a cross-account trust policy that allows STS AssumeRole
- Use `external_id` to mitigate the confused-deputy problem
- Deploy resources into two AWS accounts from a single Terraform configuration

## Why Cross-Account Access

Organizations rarely run everything inside a single AWS account. Separate accounts provide blast-radius isolation, independent billing, and cleaner security boundaries. A common pattern is a **management account** that runs Terraform and assumes roles into **workload accounts** to provision infrastructure. This eliminates the need to distribute long-lived credentials for every account; instead, the management account holds one set of credentials and uses STS `AssumeRole` to obtain short-lived tokens for each target.

Understanding `assume_role` inside the provider block is critical because it underpins every multi-account landing zone, CI/CD pipeline, and centralized infrastructure-as-code workflow you will encounter in production.

## Step 1 -- Create the Cross-Account Role in Account B

In a directory called `account-b-setup/`, create the following file.

```hcl
# account-b-setup/main.tf

data "aws_caller_identity" "current" {}

variable "trusted_account_id" {
  description = "Account A's AWS account ID"
  type        = string
}

resource "aws_iam_role" "cross_account" {
  name = "kata-cross-account-role"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::${var.trusted_account_id}:root" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "admin" {
  role       = aws_iam_role.cross_account.name
  policy_arn = "arn:aws:iam::aws:policy/PowerUserAccess"
}

output "role_arn" { value = aws_iam_role.cross_account.arn }
```

Apply this in Account B first. Note the `role_arn` output -- you will need it in the next step.

## Step 2 -- Deploy from Account A into Both Accounts

In a directory called `account-a-deploy/`, create the following file. Replace `ACCOUNT_B_ID` with the actual account ID.

```hcl
# account-a-deploy/main.tf

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "account_b"
  region = "us-east-1"

  assume_role {
    role_arn     = "arn:aws:iam::ACCOUNT_B_ID:role/kata-cross-account-role"
    session_name = "terraform-kata"
  }
}

resource "aws_ssm_parameter" "local" {
  name  = "/kata/account-a-resource"
  type  = "String"
  value = "deployed from Account A"
}

resource "aws_ssm_parameter" "remote" {
  provider = aws.account_b
  name     = "/kata/account-b-resource"
  type     = "String"
  value    = "deployed from Account A into Account B"
}
```

## Step 3 -- Plan and Inspect

```bash
cd account-a-deploy
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

Terraform shows two resources: one in Account A (default provider) and one in Account B (via `assume_role`). The ARN of the `remote` resource contains Account B's ID, confirming the role assumption worked.

```
Plan: 2 to add, 0 to change, 0 to destroy.
```

### Scenario B -- AssumeRole failure

If the trust policy in Account B does not list Account A's principal, or if the role does not exist, Terraform fails during provider initialization:

```
Error: error configuring Terraform AWS Provider: IAM Role (arn:aws:iam::...) cannot be assumed.
```

Fix: verify the trust policy `Principal` matches Account A's root ARN and that the role name is spelled correctly.

### Scenario C -- Adding external_id for extra security

To prevent confused-deputy attacks, add `external_id` to both sides:

```hcl
# In the trust policy (Account B):
Condition = {
  StringEquals = { "sts:ExternalId" = "kata-shared-secret" }
}

# In the provider (Account A):
assume_role {
  role_arn    = "arn:aws:iam::ACCOUNT_B_ID:role/kata-cross-account-role"
  external_id = "kata-shared-secret"
}
```

## Common Mistakes

### Forgetting the trust policy on the target role

The `assume_role` block in the provider only tells Terraform *which* role to assume. If the role in Account B does not have a trust policy that explicitly allows Account A's principal, the STS call will be denied. Always configure both sides: the provider block (caller) and the trust policy (target).

### Using PowerUserAccess in production

This exercise uses `PowerUserAccess` for simplicity. In real environments, follow the principle of least privilege and attach only the permissions Terraform actually needs (e.g., SSM, S3, IAM for the specific resources being managed).

## Verify What You Learned

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: 2 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq -r '.planned_values.root_module.resources[].values.name'
```

Expected:

```
/kata/account-a-resource
/kata/account-b-resource
```

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq -r '.planned_values.root_module.resources[] | select(.address == "aws_ssm_parameter.remote") | .values.value'
```

Expected: `deployed from Account A into Account B`

## What's Next

In exercise 40 you will learn how to pass provider configurations -- including aliases -- into child modules using `configuration_aliases` and the `providers` block, enabling reusable multi-region and multi-account modules.

## Reference

- [Use AssumeRole to Provision AWS Resources](https://developer.hashicorp.com/terraform/tutorials/aws/aws-assumerole)

## Additional Resources

- [AWS: Cross-Account Access with IAM Roles](https://docs.aws.amazon.com/IAM/latest/UserGuide/tutorial_cross-account-with-roles.html) -- official AWS tutorial on trust policies and STS AssumeRole between accounts
- [Spacelift: How to Use Terraform Provider Alias](https://spacelift.io/blog/terraform-provider-alias) -- guide covering multi-account patterns with assume_role and provider aliases
- [Cross-Account Terraform with Assume Roles](https://ruan.dev/blog/2024/09/15/cross-account-terraform-assume-roles-in-aws) -- practical walkthrough of cross-account setup
