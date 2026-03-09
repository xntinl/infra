# 67. Cognito Identity Pools with Federated Access

<!--
difficulty: advanced
concepts: [cognito-identity-pool, federated-identity, user-pool-idp, authenticated-role, unauthenticated-role, temporary-aws-credentials, role-mapping, principal-tags]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [04-cognito-user-pool-api-authorization]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates a Cognito User Pool, Identity Pool, IAM roles, an S3 bucket, and a Lambda function. Cost is approximately $0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- jq installed

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when to use Cognito Identity Pools versus User Pools and how they complement each other
- **Design** a federated identity flow: User Pool authentication to Identity Pool to temporary AWS credentials
- **Implement** authenticated and unauthenticated IAM roles with appropriately scoped permissions
- **Analyze** role mapping rules that assign IAM roles based on user attributes or token claims
- **Configure** fine-grained access control using principal tags derived from Cognito token claims

## Why Cognito Identity Pools

Cognito has two distinct components that serve different purposes:

1. **User Pools**: user directory and authentication. Users sign up, sign in, and receive JWT tokens (ID token, access token). User Pools handle the "who are you?" question.

2. **Identity Pools**: federated identity and authorization. Identity Pools exchange tokens (from User Pools, Google, Facebook, SAML) for temporary AWS credentials. Identity Pools handle the "what can you do?" question.

The typical flow: (1) user authenticates with a User Pool and gets an ID token; (2) the application sends the ID token to an Identity Pool; (3) the Identity Pool validates the token and returns temporary AWS credentials (STS); (4) the application uses these credentials to call AWS services directly (S3, DynamoDB, etc.) without going through your backend.

The DVA-C02 exam tests the distinction between User Pools and Identity Pools. A common scenario: "Users need to upload files directly to S3 from a mobile app without routing through your API." The answer is Cognito Identity Pool -- it gives users temporary AWS credentials scoped to their specific S3 prefix.

Identity Pools also support **unauthenticated** access: users who have not signed in can receive a limited set of AWS credentials. This is useful for allowing anonymous users to read public content from S3 or DynamoDB.

## The Challenge

Build a complete federated identity flow using Cognito User Pool as the identity provider for an Identity Pool.

### Requirements

| Requirement | Description |
|---|---|
| User Pool | User directory with email sign-in |
| Identity Pool | Federated with User Pool as identity provider |
| Authenticated Role | S3 read/write to user-specific prefix, DynamoDB read |
| Unauthenticated Role | S3 read-only on public prefix |
| Lambda Function | Tests the flow: sign up, sign in, exchange token, access S3 |
| Role Mapping | Map User Pool groups to IAM roles |

### Architecture

```
  User (app)
       |
       | 1. Sign in (email/password)
       v
  +-------------------+
  | Cognito User Pool  |
  | Returns ID Token   |
  +-------------------+
       |
       | 2. Exchange ID Token
       v
  +-------------------+
  | Cognito Identity   |
  | Pool               |
  | Returns temp creds |
  +-------------------+
       |
       | 3. Use AWS credentials
       v
  +-------------------+
  | S3 / DynamoDB      |
  | (scoped access)    |
  +-------------------+
```

## Hints

<details>
<summary>Hint 1: Cognito User Pool and App Client</summary>

Create a User Pool with a simple email-based configuration and an app client that supports `ALLOW_USER_PASSWORD_AUTH`:

```hcl
resource "aws_cognito_user_pool" "this" {
  name = "identity-pool-demo"

  auto_verified_attributes = ["email"]
  username_attributes      = ["email"]

  password_policy {
    minimum_length    = 8
    require_uppercase = false
    require_lowercase = false
    require_numbers   = false
    require_symbols   = false
  }

  schema {
    name                = "email"
    attribute_data_type = "String"
    required            = true
    mutable             = true
  }
}

resource "aws_cognito_user_pool_client" "this" {
  name         = "identity-pool-demo-client"
  user_pool_id = aws_cognito_user_pool.this.id

  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]
}
```

</details>

<details>
<summary>Hint 2: Identity Pool with User Pool provider</summary>

The Identity Pool references the User Pool as a "cognito identity provider":

```hcl
resource "aws_cognito_identity_pool" "this" {
  identity_pool_name               = "identity-pool-demo"
  allow_unauthenticated_identities = true

  cognito_identity_providers {
    client_id               = aws_cognito_user_pool_client.this.id
    provider_name           = aws_cognito_user_pool.this.endpoint
    server_side_token_check = true
  }
}
```

Then attach authenticated and unauthenticated roles:

```hcl
resource "aws_cognito_identity_pool_roles_attachment" "this" {
  identity_pool_id = aws_cognito_identity_pool.this.id

  roles = {
    "authenticated"   = aws_iam_role.authenticated.arn
    "unauthenticated" = aws_iam_role.unauthenticated.arn
  }
}
```

</details>

<details>
<summary>Hint 3: IAM roles for Identity Pool</summary>

Identity Pool roles have a special trust policy that trusts `cognito-identity.amazonaws.com`:

```hcl
data "aws_iam_policy_document" "authenticated_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = ["cognito-identity.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "cognito-identity.amazonaws.com:aud"
      values   = [aws_cognito_identity_pool.this.id]
    }
    condition {
      test     = "ForAnyValue:StringLike"
      variable = "cognito-identity.amazonaws.com:amr"
      values   = ["authenticated"]
    }
  }
}

resource "aws_iam_role" "authenticated" {
  name               = "cognito-authenticated-role"
  assume_role_policy = data.aws_iam_policy_document.authenticated_trust.json
}
```

For the unauthenticated role, change the `amr` condition to `"unauthenticated"`.

</details>

<details>
<summary>Hint 4: Fine-grained S3 access with identity ID</summary>

Scope S3 access to a user-specific prefix using the Cognito identity ID as a policy variable:

```hcl
data "aws_iam_policy_document" "authenticated_policy" {
  statement {
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.this.arn}/users/$${cognito-identity.amazonaws.com:sub}/*"]
  }

  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.this.arn}/public/*"]
  }
}
```

The `$${cognito-identity.amazonaws.com:sub}` policy variable resolves to the user's unique identity ID at request time. Each user can only read/write objects under their own prefix.

</details>

## Spot the Bug

A developer creates an Identity Pool but the authenticated role's trust policy uses `sts:AssumeRole` instead of `sts:AssumeRoleWithWebIdentity`:

```hcl
data "aws_iam_policy_document" "auth_trust" {
  statement {
    actions = ["sts:AssumeRole"]                              # <-- BUG
    principals {
      type        = "Federated"
      identifiers = ["cognito-identity.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "cognito-identity.amazonaws.com:aud"
      values   = [aws_cognito_identity_pool.this.id]
    }
  }
}
```

<details>
<summary>Explain the bug</summary>

Cognito Identity Pools use **web identity federation**, which requires `sts:AssumeRoleWithWebIdentity`, not `sts:AssumeRole`. The `AssumeRole` action is for direct role assumption (one AWS principal assuming another role). `AssumeRoleWithWebIdentity` is for federation scenarios where an identity token from an external provider (Cognito, Google, etc.) is exchanged for AWS credentials.

With `sts:AssumeRole`, the Identity Pool cannot assume the role and users receive an error when trying to obtain credentials.

**Fix -- use the correct STS action:**

```hcl
data "aws_iam_policy_document" "auth_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]  # Correct action for federation
    principals {
      type        = "Federated"
      identifiers = ["cognito-identity.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "cognito-identity.amazonaws.com:aud"
      values   = [aws_cognito_identity_pool.this.id]
    }
    condition {
      test     = "ForAnyValue:StringLike"
      variable = "cognito-identity.amazonaws.com:amr"
      values   = ["authenticated"]
    }
  }
}
```

Also note the missing `amr` condition in the bug. Without it, both authenticated and unauthenticated identities could assume this role.

</details>

## Verify What You Learned

### Step 1 -- Deploy

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Create a test user

```bash
USER_POOL_ID=$(terraform output -raw user_pool_id)
CLIENT_ID=$(terraform output -raw client_id)

aws cognito-idp admin-create-user \
  --user-pool-id "$USER_POOL_ID" \
  --username "test@example.com" \
  --temporary-password "TempPass123!" \
  --message-action SUPPRESS

aws cognito-idp admin-set-user-password \
  --user-pool-id "$USER_POOL_ID" \
  --username "test@example.com" \
  --password "TestPass123!" \
  --permanent
```

### Step 3 -- Authenticate and get Identity Pool credentials

```bash
# Get ID token from User Pool
AUTH_RESULT=$(aws cognito-idp initiate-auth \
  --auth-flow USER_PASSWORD_AUTH \
  --client-id "$CLIENT_ID" \
  --auth-parameters USERNAME=test@example.com,PASSWORD=TestPass123!)
ID_TOKEN=$(echo "$AUTH_RESULT" | jq -r '.AuthenticationResult.IdToken')

IDENTITY_POOL_ID=$(terraform output -raw identity_pool_id)

# Get Identity ID
IDENTITY_ID=$(aws cognito-identity get-id \
  --identity-pool-id "$IDENTITY_POOL_ID" \
  --logins "$(terraform output -raw user_pool_endpoint)=$ID_TOKEN" \
  --query "IdentityId" --output text)

echo "Identity ID: $IDENTITY_ID"

# Get temporary AWS credentials
aws cognito-identity get-credentials-for-identity \
  --identity-id "$IDENTITY_ID" \
  --logins "$(terraform output -raw user_pool_endpoint)=$ID_TOKEN" \
  --query "Credentials.{AccessKeyId:AccessKeyId,Expiration:Expiration}" --output json
```

Expected: temporary credentials with AccessKeyId and Expiration.

### Step 4 -- Verify role attachment

```bash
aws cognito-identity get-identity-pool-roles \
  --identity-pool-id $(terraform output -raw identity_pool_id) \
  --query "Roles" --output json
```

Expected: `authenticated` and `unauthenticated` roles attached.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You built a complete federated identity flow with Cognito User Pool and Identity Pool. In the next exercise, you will explore **KMS key policies and grants** -- controlling cryptographic key access through resource-based policies and temporary programmatic grants.

## Summary

- **User Pools** handle authentication (sign up, sign in, tokens); **Identity Pools** handle authorization (temporary AWS credentials)
- The flow: User Pool token -> Identity Pool -> temporary STS credentials -> direct AWS service access
- Identity Pool roles use `sts:AssumeRoleWithWebIdentity` (not `sts:AssumeRole`) with `cognito-identity.amazonaws.com` as the federated principal
- **Authenticated roles** require the `amr` condition set to `authenticated`; unauthenticated roles use `unauthenticated`
- **Fine-grained access**: use `${cognito-identity.amazonaws.com:sub}` as a policy variable to scope access per user
- **Unauthenticated access**: `allow_unauthenticated_identities = true` enables guest access with a limited role
- **Role mapping**: assign different IAM roles based on User Pool group membership or token claims
- Identity Pool credentials expire after 1 hour by default and must be refreshed

## Reference

- [Cognito Identity Pools](https://docs.aws.amazon.com/cognito/latest/developerguide/cognito-identity.html)
- [User Pool to Identity Pool Flow](https://docs.aws.amazon.com/cognito/latest/developerguide/amazon-cognito-integrating-user-pools-with-identity-pools.html)
- [Terraform aws_cognito_identity_pool](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/cognito_identity_pool)
- [IAM Roles for Identity Pools](https://docs.aws.amazon.com/cognito/latest/developerguide/iam-roles.html)

## Additional Resources

- [Role Mapping with Rules](https://docs.aws.amazon.com/cognito/latest/developerguide/role-based-access-control.html) -- assigning roles based on token claims
- [Principal Tags](https://docs.aws.amazon.com/cognito/latest/developerguide/attributes-for-access-control.html) -- fine-grained access using tags from token attributes
- [Unauthenticated Identities](https://docs.aws.amazon.com/cognito/latest/developerguide/switching-identities.html) -- guest access patterns
- [Enhanced Authentication Flow](https://docs.aws.amazon.com/cognito/latest/developerguide/authentication-flow.html) -- enhanced vs basic flow for credential retrieval

<details>
<summary>Full Solution</summary>

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
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
  default     = "identity-pool-demo"
}
```

### `cognito.tf`

```hcl
resource "aws_cognito_user_pool" "this" {
  name = var.project_name

  auto_verified_attributes = ["email"]
  username_attributes      = ["email"]

  password_policy {
    minimum_length    = 8
    require_uppercase = false
    require_lowercase = false
    require_numbers   = false
    require_symbols   = false
  }

  schema {
    name                = "email"
    attribute_data_type = "String"
    required            = true
    mutable             = true
  }
}

resource "aws_cognito_user_pool_client" "this" {
  name         = "${var.project_name}-client"
  user_pool_id = aws_cognito_user_pool.this.id

  explicit_auth_flows = [
    "ALLOW_USER_PASSWORD_AUTH",
    "ALLOW_REFRESH_TOKEN_AUTH",
  ]
}

resource "aws_cognito_identity_pool" "this" {
  identity_pool_name               = var.project_name
  allow_unauthenticated_identities = true

  cognito_identity_providers {
    client_id               = aws_cognito_user_pool_client.this.id
    provider_name           = aws_cognito_user_pool.this.endpoint
    server_side_token_check = true
  }
}

resource "aws_cognito_identity_pool_roles_attachment" "this" {
  identity_pool_id = aws_cognito_identity_pool.this.id

  roles = {
    "authenticated"   = aws_iam_role.authenticated.arn
    "unauthenticated" = aws_iam_role.unauthenticated.arn
  }
}
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "this" {
  bucket_prefix = "${var.project_name}-"
  force_destroy = true
}
```

### `iam.tf`

```hcl
# Authenticated role trust policy
data "aws_iam_policy_document" "authenticated_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = ["cognito-identity.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "cognito-identity.amazonaws.com:aud"
      values   = [aws_cognito_identity_pool.this.id]
    }
    condition {
      test     = "ForAnyValue:StringLike"
      variable = "cognito-identity.amazonaws.com:amr"
      values   = ["authenticated"]
    }
  }
}

resource "aws_iam_role" "authenticated" {
  name               = "${var.project_name}-authenticated"
  assume_role_policy = data.aws_iam_policy_document.authenticated_trust.json
}

data "aws_iam_policy_document" "authenticated_policy" {
  statement {
    actions   = ["s3:GetObject", "s3:PutObject"]
    resources = ["${aws_s3_bucket.this.arn}/users/$${cognito-identity.amazonaws.com:sub}/*"]
  }
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.this.arn}/public/*"]
  }
}

resource "aws_iam_role_policy" "authenticated" {
  name   = "authenticated-s3-access"
  role   = aws_iam_role.authenticated.id
  policy = data.aws_iam_policy_document.authenticated_policy.json
}

# Unauthenticated role trust policy
data "aws_iam_policy_document" "unauthenticated_trust" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = ["cognito-identity.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "cognito-identity.amazonaws.com:aud"
      values   = [aws_cognito_identity_pool.this.id]
    }
    condition {
      test     = "ForAnyValue:StringLike"
      variable = "cognito-identity.amazonaws.com:amr"
      values   = ["unauthenticated"]
    }
  }
}

resource "aws_iam_role" "unauthenticated" {
  name               = "${var.project_name}-unauthenticated"
  assume_role_policy = data.aws_iam_policy_document.unauthenticated_trust.json
}

data "aws_iam_policy_document" "unauthenticated_policy" {
  statement {
    actions   = ["s3:GetObject"]
    resources = ["${aws_s3_bucket.this.arn}/public/*"]
  }
}

resource "aws_iam_role_policy" "unauthenticated" {
  name   = "unauthenticated-s3-access"
  role   = aws_iam_role.unauthenticated.id
  policy = data.aws_iam_policy_document.unauthenticated_policy.json
}
```

### `outputs.tf`

```hcl
output "user_pool_id" {
  value = aws_cognito_user_pool.this.id
}

output "user_pool_endpoint" {
  value = aws_cognito_user_pool.this.endpoint
}

output "client_id" {
  value = aws_cognito_user_pool_client.this.id
}

output "identity_pool_id" {
  value = aws_cognito_identity_pool.this.id
}

output "bucket_name" {
  value = aws_s3_bucket.this.id
}
```

</details>
