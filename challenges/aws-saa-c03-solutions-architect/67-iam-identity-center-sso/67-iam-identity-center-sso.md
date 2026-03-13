# 67. IAM Identity Center (SSO)

<!--
difficulty: intermediate
concepts: [iam-identity-center, sso, permission-set, saml, external-idp, workforce-identity, aws-access-portal, multi-account-access, session-duration, attribute-based-access-control]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [65-iam-policies-identity-resource-scp, 66-organizations-service-control-policies]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** IAM Identity Center has no cost. This exercise creates Identity Center resources and permission sets. No charges beyond minimal API calls (~$0.01/hr). Remember to clean up resources when finished.

---

## Prerequisites

| Requirement | Verify |
|---|---|
| AWS CLI v2 installed and configured | `aws sts get-caller-identity` |
| Terraform >= 1.5 installed | `terraform version` |
| AWS Organizations enabled | `aws organizations describe-organization` |
| Completed exercise 65 (IAM policy types) | Understanding of identity-based policies |

---

## Learning Objectives

By the end of this exercise you will be able to:

1. **Implement** IAM Identity Center with permission sets for multi-account access management.
2. **Distinguish** between IAM Identity Center (workforce identity -- employees accessing AWS accounts) and Amazon Cognito (customer identity -- end users of your applications).
3. **Evaluate** permission set designs that follow least-privilege for different roles (developer, operator, auditor).
4. **Analyze** SAML 2.0 federation with external identity providers (Okta, Azure AD, Google Workspace) for centralized authentication.
5. **Design** a multi-account access strategy using permission sets, groups, and account assignments.

---

## Why This Matters

The SAA-C03 exam tests two distinctions: (1) Identity Center (workforce -- employees) vs Cognito (customers -- app users); (2) Identity Center (centralized multi-account access) vs direct IAM users (account-specific). For organizations with an existing IdP, Identity Center federates with SAML 2.0 -- no IAM users needed.

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
  default     = "saa-ex67"
}
```

### `iam.tf`

```hcl
# ============================================================
# TODO 1: Enable IAM Identity Center
# ============================================================
# IAM Identity Center must be enabled in the management account
# of your AWS Organization. It can only be enabled in ONE region.
#
# If not already enabled:
#   aws sso-admin create-instance-assignment
#   Or enable via the console: IAM Identity Center > Enable
#
# Once enabled, get the instance ARN and identity store ID:
#   aws sso-admin list-instances
#
# These values are needed for subsequent resources.
#
# Docs: https://docs.aws.amazon.com/singlesignon/latest/userguide/get-started-enable-identity-center.html
# ============================================================

# Use data source if Identity Center is already enabled
# data "aws_ssoadmin_instances" "this" {}

# ============================================================
# TODO 2: Create Permission Sets
# ============================================================
# Permission sets define WHAT access a user/group gets when
# assigned to an AWS account. They are reusable templates
# that can be assigned to multiple accounts.
#
# Create three permission sets:
#
# a) Developer: Read-only to most services + full access to
#    specific development services (CloudWatch, S3, Lambda)
#    - Resource: aws_ssoadmin_permission_set
#    - name = "DeveloperAccess"
#    - session_duration = "PT4H" (4 hours)
#    - Attach managed policy: ViewOnlyAccess
#    - Attach inline policy: Allow Lambda, S3, CloudWatch full access
#
# b) Operator: Full access to compute and monitoring but no
#    IAM or billing access
#    - name = "OperatorAccess"
#    - session_duration = "PT8H"
#    - Attach managed policy: PowerUserAccess
#
# c) Auditor: Read-only to everything, no write actions
#    - name = "AuditorAccess"
#    - session_duration = "PT2H" (short session for security)
#    - Attach managed policy: ReadOnlyAccess
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssoadmin_permission_set
# ============================================================


# ============================================================
# TODO 3: Create Groups and Users
# ============================================================
# Create groups in the Identity Center identity store and
# assign them to AWS accounts with specific permission sets.
#
# Groups:
#   - "Developers" group --> DeveloperAccess on dev account
#   - "Operators" group  --> OperatorAccess on prod account
#   - "Auditors" group   --> AuditorAccess on ALL accounts
#
# Resources:
#   - aws_identitystore_group
#   - aws_identitystore_user (optional — for testing)
#   - aws_ssoadmin_account_assignment
#
# Note: In production, groups/users come from your external
# IdP (Okta, Azure AD) via SCIM provisioning. You only create
# them in the Identity Center store for testing or when using
# Identity Center as the IdP.
#
# Docs: https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssoadmin_account_assignment
# ============================================================
```

### `outputs.tf`

```hcl
output "identity_center_start_url" {
  value = "Check aws sso-admin list-instances for the start URL"
}
```

---

## Identity Center vs Cognito

| Criterion | IAM Identity Center | Amazon Cognito |
|---|---|---|
| **Purpose** | Workforce identity (employees) | Customer identity (app users) |
| **Users** | Internal staff, contractors | External customers |
| **Access to** | AWS Console, CLI, applications | Your web/mobile applications |
| **Scale** | Hundreds to thousands of employees | Millions of end users |
| **Federation** | SAML 2.0 to corporate IdP | SAML 2.0, OIDC, social providers |
| **MFA** | Built-in MFA support | Built-in MFA support |
| **Account access** | Multi-account via permission sets | Application-level via tokens |
| **Pricing** | Free | Pay per MAU |
| **Exam signal** | "employees need access to multiple AWS accounts" | "users sign in to the application" |

### Decision Framework

```
Who needs access?
  |
  +--> Employees/contractors accessing AWS accounts
  |      = IAM Identity Center
  |      Signal: "console access", "CLI access", "multi-account"
  |
  +--> Customers/end users accessing your application
  |      = Amazon Cognito
  |      Signal: "sign up", "social login", "mobile app"
  |
  +--> Applications/services calling AWS APIs
         = IAM Roles (not Identity Center or Cognito)
         Signal: "EC2 instance", "Lambda function", "ECS task"
```

---

## Spot the Bug

A security team configured IAM Identity Center for their organization. They created permission sets and assigned them to accounts. A junior administrator was assigned `AdministratorAccess` to the production OU:

```hcl
resource "aws_ssoadmin_permission_set" "admin" {
  name             = "AdministratorAccess"
  instance_arn     = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  session_duration = "PT12H"
}

resource "aws_ssoadmin_managed_policy_attachment" "admin" {
  instance_arn       = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  managed_policy_arn = "arn:aws:iam::aws:policy/AdministratorAccess"
  permission_set_arn = aws_ssoadmin_permission_set.admin.arn
}

# Assigned to production account
resource "aws_ssoadmin_account_assignment" "junior_admin_prod" {
  instance_arn       = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  permission_set_arn = aws_ssoadmin_permission_set.admin.arn
  principal_id       = aws_identitystore_user.junior_admin.user_id
  principal_type     = "USER"
  target_id          = "111222333444"  # Production account
  target_type        = "AWS_ACCOUNT"
}
```

<details>
<summary>Explain the bug</summary>

**Three security issues with this configuration:**

1. **AdministratorAccess on production:** A junior administrator should not have full admin access to production. This violates least privilege and creates a blast radius risk. A misconfigured resource or accidental deletion could cause a production outage.

2. **12-hour session duration (`PT12H`):** Admin sessions in production should be as short as practical. A 12-hour session means a compromised token provides full access for half a day. Use 1-2 hours for production admin access, requiring re-authentication for extended work.

3. **Direct user assignment instead of group:** Assigning permission sets directly to users makes access management fragile. When the user's role changes, you must update every individual assignment. Groups enable role-based access control: move the user between groups, and their permissions update automatically.

**Fix:** Use least-privilege permission sets, short sessions, and group-based assignments:

```hcl
# Least-privilege permission set for production
resource "aws_ssoadmin_permission_set" "prod_operator" {
  name             = "ProductionOperator"
  instance_arn     = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  session_duration = "PT2H"  # Short session for production
}

resource "aws_ssoadmin_managed_policy_attachment" "prod_operator" {
  instance_arn       = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  managed_policy_arn = "arn:aws:iam::aws:policy/PowerUserAccess"
  permission_set_arn = aws_ssoadmin_permission_set.prod_operator.arn
}

# Assign to a GROUP, not individual user
resource "aws_ssoadmin_account_assignment" "operators_prod" {
  instance_arn       = tolist(data.aws_ssoadmin_instances.this.arns)[0]
  permission_set_arn = aws_ssoadmin_permission_set.prod_operator.arn
  principal_id       = aws_identitystore_group.operators.group_id
  principal_type     = "GROUP"
  target_id          = "111222333444"
  target_type        = "AWS_ACCOUNT"
}
```

Additionally, use SCPs on the production OU to restrict dangerous actions regardless of permission set, providing defense in depth.

</details>

---

---

## Verify What You Learned

1. **Check Identity Center and permission sets:**
   ```bash
   INSTANCE_ARN=$(aws sso-admin list-instances --query 'Instances[0].InstanceArn' --output text)
   aws sso-admin list-permission-sets --instance-arn "$INSTANCE_ARN" --output json
   terraform plan
   ```
   Expected: Instance ARN returned, permission sets listed, `No changes.`

---

## Solutions

<details>
<summary>TODO 2 -- Permission Sets (iam.tf)</summary>

```hcl
data "aws_ssoadmin_instances" "this" {}

locals {
  instance_arn = tolist(data.aws_ssoadmin_instances.this.arns)[0]
}

# Developer: read-only base + specific service access
resource "aws_ssoadmin_permission_set" "developer" {
  name             = "DeveloperAccess"
  instance_arn     = local.instance_arn
  session_duration = "PT4H"
  description      = "Read-only base with Lambda, S3, CloudWatch full access"
}

resource "aws_ssoadmin_managed_policy_attachment" "developer_readonly" {
  instance_arn       = local.instance_arn
  managed_policy_arn = "arn:aws:iam::aws:policy/job-function/ViewOnlyAccess"
  permission_set_arn = aws_ssoadmin_permission_set.developer.arn
}

resource "aws_ssoadmin_permission_set_inline_policy" "developer_extra" {
  instance_arn       = local.instance_arn
  permission_set_arn = aws_ssoadmin_permission_set.developer.arn

  inline_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Effect   = "Allow"
        Action   = ["lambda:*", "s3:*", "logs:*", "cloudwatch:*"]
        Resource = "*"
      }
    ]
  })
}

# Operator: broad compute access, no IAM
resource "aws_ssoadmin_permission_set" "operator" {
  name             = "OperatorAccess"
  instance_arn     = local.instance_arn
  session_duration = "PT8H"
  description      = "Full access to compute and monitoring, no IAM"
}

resource "aws_ssoadmin_managed_policy_attachment" "operator" {
  instance_arn       = local.instance_arn
  managed_policy_arn = "arn:aws:iam::aws:policy/PowerUserAccess"
  permission_set_arn = aws_ssoadmin_permission_set.operator.arn
}

# Auditor: read-only to everything
resource "aws_ssoadmin_permission_set" "auditor" {
  name             = "AuditorAccess"
  instance_arn     = local.instance_arn
  session_duration = "PT2H"
  description      = "Read-only access for compliance auditing"
}

resource "aws_ssoadmin_managed_policy_attachment" "auditor" {
  instance_arn       = local.instance_arn
  managed_policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
  permission_set_arn = aws_ssoadmin_permission_set.auditor.arn
}
```

</details>

<details>
<summary>TODO 3 -- Groups and Account Assignments (iam.tf)</summary>

```hcl
locals {
  identity_store_id = tolist(data.aws_ssoadmin_instances.this.identity_store_ids)[0]
}

resource "aws_identitystore_group" "developers" {
  identity_store_id = local.identity_store_id
  display_name      = "Developers"
  description       = "Development team members"
}

resource "aws_identitystore_group" "operators" {
  identity_store_id = local.identity_store_id
  display_name      = "Operators"
  description       = "Operations team members"
}

# Account assignment example (replace account ID):
# resource "aws_ssoadmin_account_assignment" "devs_dev" {
#   instance_arn = local.instance_arn
#   permission_set_arn = aws_ssoadmin_permission_set.developer.arn
#   principal_id = aws_identitystore_group.developers.group_id
#   principal_type = "GROUP"
#   target_id = "111222333444"; target_type = "AWS_ACCOUNT"
# }
```

In production, groups/users come from the external IdP via SCIM provisioning.

</details>

---

## Cleanup

```bash
terraform destroy -auto-approve
```

Note: Deleting permission sets requires removing all account assignments first. Terraform handles this automatically during destroy.

Verify:

```bash
INSTANCE_ARN=$(aws sso-admin list-instances --query 'Instances[0].InstanceArn' --output text)
aws sso-admin list-permission-sets --instance-arn "$INSTANCE_ARN" --output json
```

Expected: Only AWS-managed permission sets remain.

---

## What's Next

Exercise 68 covers **AWS KMS key management and rotation**. You will create customer-managed keys, configure automatic annual rotation, and understand the critical difference between disabling a key (reversible) and deleting a key (irreversible with a waiting period) -- a decision with permanent consequences for encrypted data.

---

## Summary

- **IAM Identity Center** is the recommended way to manage workforce access to AWS accounts
- **Permission sets** are reusable access templates that create IAM roles behind the scenes
- **Identity Center vs Cognito:** Identity Center for employees, Cognito for application users
- **SAML 2.0 federation** enables corporate credentials for AWS access
- **Group-based assignments** preferred over direct user assignments
- **Session duration** should match sensitivity -- shorter for production
- **SCIM provisioning** automatically syncs users and groups from external IdPs to Identity Center
- **AdministratorAccess permission sets on production** should be avoided -- use least-privilege with SCPs as guardrails

## Reference

- [IAM Identity Center User Guide](https://docs.aws.amazon.com/singlesignon/latest/userguide/what-is.html)
- [Permission Sets](https://docs.aws.amazon.com/singlesignon/latest/userguide/permissionsetsconcept.html)
- [SAML 2.0 Federation](https://docs.aws.amazon.com/singlesignon/latest/userguide/manage-your-identity-source-idp.html)
- [Terraform aws_ssoadmin_permission_set](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ssoadmin_permission_set)

## Additional Resources

- [Identity Center with AWS CLI](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-sso.html) -- `aws configure sso` for CLI access
- [Attribute-Based Access Control](https://docs.aws.amazon.com/singlesignon/latest/userguide/abac.html) -- dynamic permissions based on user attributes
- [Identity Center vs IAM Federation](https://docs.aws.amazon.com/singlesignon/latest/userguide/identity-center-vs-iam-federation.html) -- when to use each approach
