# 83. CodeArtifact Package Management

<!--
difficulty: advanced
concepts: [codeartifact-domain, codeartifact-repository, go-modules-proxy, goproxy-configuration, upstream-connections, package-version-disposal, domain-policies, cross-account-access, authorization-token]
tools: [aws-cli, go]
estimated_time: 50m
bloom_level: create, evaluate
prerequisites: [78-codebuild-environment-variables-secrets]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a CodeArtifact domain and repository. CodeArtifact charges $0.05 per GB per month for storage and $0.001 per package version request. For testing volumes this is negligible. Remember to clean up when finished.

## Prerequisites

- AWS CLI configured with a sandbox account
- Go 1.21+ installed (`go version`)
- Terraform >= 1.7 installed

## Learning Objectives

After completing this exercise, you will be able to:

- **Create** a CodeArtifact domain and repository with Terraform, configure upstream connections to public repositories, and set domain-level policies
- **Configure** Go module proxying through CodeArtifact by setting GOPROXY to the CodeArtifact repository endpoint with an authorization token
- **Evaluate** the CodeArtifact authorization model: domain-level policies, repository-level policies, and time-limited authorization tokens
- **Design** a package management workflow: upstream connections cache packages from public repos, domain policies control cross-account access, and package version disposal manages storage
- **Analyze** the relationship between domains (administrative boundary, encryption), repositories (package storage), and upstream connections (package fetching)

## Why This Matters

CodeArtifact is a managed artifact repository service that stores software packages for Go modules, npm, Python (pip), Maven, NuGet, and more. It solves three problems for development teams.

**Supply chain security**: packages are cached from public registries (proxy.golang.org, npmjs.com, pypi.org) into your private repository. If a public package is compromised or deleted, your builds continue using the cached version.

**Access control**: domain and repository policies restrict who can publish, read, or manage packages. Cross-account access lets a central team maintain a shared package repository.

**Availability**: your builds do not depend on external registries being available. Once a package version is cached in CodeArtifact, it remains available even if the upstream is down.

The DVA-C02 exam tests CodeArtifact in CI/CD pipeline questions. Key concepts: CodeArtifact **domains** are the administrative boundary (encryption key, cross-account policies). **Repositories** store packages and can have **upstream connections** to other repositories or external registries. **Authorization tokens** are temporary credentials (12-hour default, configurable) used to authenticate package managers. The exam asks about configuring GOPROXY for Go, pip for Python, and npm for JavaScript.

## The Challenge

Create a CodeArtifact domain and repository, configure Go module proxying, and demonstrate upstream package caching from the public Go module proxy.

### Requirements

| Requirement | Description |
|---|---|
| Domain | CodeArtifact domain with default encryption |
| Repository | Private repository with upstream to public Go modules |
| GOPROXY | Configure Go toolchain to use CodeArtifact as module proxy |
| Auth Token | Obtain and use a time-limited authorization token |
| Verification | Download a public Go module through CodeArtifact and verify caching |

### Architecture

```
  Go Toolchain         CodeArtifact          Public Registry
  +----------+    +-------------------+    +------------------+
  |          |    | Domain            |    |                  |
  | GOPROXY= |--->| └── Repository   |--->| proxy.golang.org |
  | endpoint |    |     (cached pkgs) |    |                  |
  +----------+    +-------------------+    +------------------+
                  │                   │
                  │ Auth Token (12hr) │
                  │ Domain Policy     │
                  └───────────────────┘
```

## Hints

<details>
<summary>Hint 1: Creating the domain and repository</summary>

```hcl
resource "aws_codeartifact_domain" "this" {
  domain = "${var.project_name}-domain"
}

resource "aws_codeartifact_repository" "upstream" {
  repository  = "go-public-upstream"
  domain      = aws_codeartifact_domain.this.domain
  description = "Upstream connection to public Go module proxy"

  external_connections {
    external_connection_name = "public:go"
  }
}

resource "aws_codeartifact_repository" "private" {
  repository  = "${var.project_name}-repo"
  domain      = aws_codeartifact_domain.this.domain
  description = "Private Go module repository"

  upstream {
    repository_name = aws_codeartifact_repository.upstream.repository
  }
}
```

The upstream chain: your private repo -> upstream repo -> public Go modules proxy. When you request a module from the private repo, CodeArtifact checks the private repo first, then the upstream, then the external connection.

</details>

<details>
<summary>Hint 2: Getting an authorization token</summary>

```bash
# Get a 12-hour authorization token
AUTH_TOKEN=$(aws codeartifact get-authorization-token \
  --domain my-domain \
  --query authorizationToken --output text)

# Get the repository endpoint
ENDPOINT=$(aws codeartifact get-repository-endpoint \
  --domain my-domain \
  --repository my-repo \
  --format go \
  --query repositoryEndpoint --output text)
```

The token is a time-limited credential. Default duration is 12 hours (configurable up to 12 hours for IAM users, or the role session duration for IAM roles).

</details>

<details>
<summary>Hint 3: Configuring GOPROXY</summary>

```bash
# Set GOPROXY to use CodeArtifact, with fallback to direct
export GOPROXY="https://aws:${AUTH_TOKEN}@${ENDPOINT#https://}v1,direct"
export GONOSUMDB="*"
export GOFLAGS="-insecure"

# Or configure via go env
go env -w GOPROXY="https://aws:${AUTH_TOKEN}@${ENDPOINT#https://}v1,direct"
```

The GOPROXY URL format for CodeArtifact: `https://aws:TOKEN@ENDPOINT/v1`. The `aws` username is fixed -- CodeArtifact uses it for all authentication. The `,direct` suffix falls back to direct module download if CodeArtifact does not have the module.

</details>

<details>
<summary>Hint 4: Domain policies for cross-account access</summary>

Domain policies control who can access the domain and its repositories from other AWS accounts:

```hcl
resource "aws_codeartifact_domain_permissions_policy" "this" {
  domain = aws_codeartifact_domain.this.domain

  policy_document = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid       = "AllowAccountAccess"
        Effect    = "Allow"
        Principal = { AWS = "arn:aws:iam::${var.trusted_account_id}:root" }
        Action = [
          "codeartifact:GetAuthorizationToken",
          "codeartifact:GetDomainPermissionsPolicy",
          "codeartifact:ListRepositoriesInDomain",
          "sts:GetServiceBearerToken"
        ]
        Resource = "*"
      }
    ]
  })
}
```

</details>

<details>
<summary>Hint 5: Package version disposal</summary>

Package versions accumulate in the repository. Use the `dispose-package-versions` command or repository policies to manage storage:

```bash
# List package versions
aws codeartifact list-package-versions \
  --domain my-domain \
  --repository my-repo \
  --format go \
  --package "github.com/aws/aws-lambda-go" \
  --namespace "events"

# Dispose (soft-delete) old versions
aws codeartifact dispose-package-versions \
  --domain my-domain \
  --repository my-repo \
  --format go \
  --package "aws-lambda-go" \
  --namespace "github.com/aws" \
  --versions "1.40.0"
```

Disposed versions are removed from the repository but can be restored from the upstream if needed.

</details>

## Spot the Bug

A developer configures GOPROXY for CodeArtifact but `go get` fails with 401 Unauthorized. The authorization token was obtained 14 hours ago. **What is wrong?**

```bash
# Token obtained at 8:00 AM
AUTH_TOKEN=$(aws codeartifact get-authorization-token \
  --domain my-domain --query authorizationToken --output text)

# ... 14 hours later at 10:00 PM ...
GOPROXY="https://aws:${AUTH_TOKEN}@endpoint/v1"
go get github.com/aws/aws-lambda-go@v1.47.0
# ERROR: 401 Unauthorized
```

<details>
<summary>Explain the bug</summary>

CodeArtifact authorization tokens have a **default expiration of 12 hours**. The token was obtained 14 hours ago and has expired. Any request using the expired token returns 401 Unauthorized.

The fix: obtain a fresh token before using it:

```bash
AUTH_TOKEN=$(aws codeartifact get-authorization-token \
  --domain my-domain \
  --domain-owner $(aws sts get-caller-identity --query Account --output text) \
  --query authorizationToken --output text)
```

For CI/CD pipelines, the token should be obtained at the start of each build, not cached across builds. You can also set a shorter duration:

```bash
aws codeartifact get-authorization-token \
  --domain my-domain \
  --duration-seconds 3600 \
  --query authorizationToken --output text
```

On the exam, token expiration is the most common CodeArtifact authentication failure scenario. Remember: 12-hour default, configurable up to 12 hours for IAM users, or the IAM role's session duration for assumed roles (which might be shorter, e.g., 1 hour).

</details>

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
  default     = "codeartifact-demo"
}
```

### `cicd.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_codeartifact_domain" "this" {
  domain = "${var.project_name}-domain"
}

resource "aws_codeartifact_repository" "upstream" {
  repository  = "go-public-upstream"
  domain      = aws_codeartifact_domain.this.domain
  description = "Upstream connection to public Go module proxy"

  external_connections {
    external_connection_name = "public:go"
  }
}

resource "aws_codeartifact_repository" "private" {
  repository  = "${var.project_name}-repo"
  domain      = aws_codeartifact_domain.this.domain
  description = "Private Go module repository"

  upstream {
    repository_name = aws_codeartifact_repository.upstream.repository
  }
}
```

### `outputs.tf`

```hcl
output "domain_name" {
  value = aws_codeartifact_domain.this.domain
}

output "repository_name" {
  value = aws_codeartifact_repository.private.repository
}

output "domain_owner" {
  value = data.aws_caller_identity.current.account_id
}
```

### Setup and test script

```bash
# Deploy
terraform init && terraform apply -auto-approve

DOMAIN=$(terraform output -raw domain_name)
REPO=$(terraform output -raw repository_name)
OWNER=$(terraform output -raw domain_owner)

# Get authorization token
AUTH_TOKEN=$(aws codeartifact get-authorization-token \
  --domain "$DOMAIN" --domain-owner "$OWNER" \
  --query authorizationToken --output text)

# Get repository endpoint
ENDPOINT=$(aws codeartifact get-repository-endpoint \
  --domain "$DOMAIN" --domain-owner "$OWNER" \
  --repository "$REPO" --format go \
  --query repositoryEndpoint --output text)

echo "Endpoint: $ENDPOINT"

# Configure GOPROXY
export GOPROXY="https://aws:${AUTH_TOKEN}@${ENDPOINT#https://}v1,direct"
export GONOSUMDB="*"

# Test: download a module through CodeArtifact
mkdir -p /tmp/codeartifact-test && cd /tmp/codeartifact-test
go mod init test-module
go get github.com/aws/aws-lambda-go@v1.47.0

# Verify the module was cached in CodeArtifact
aws codeartifact list-packages \
  --domain "$DOMAIN" --domain-owner "$OWNER" \
  --repository "$REPO" --format go \
  --query "packages[*].{Package:package,Namespace:namespace}" --output table

cd -
rm -rf /tmp/codeartifact-test
```

</details>

## Verify What You Learned

```bash
DOMAIN=$(terraform output -raw domain_name)
OWNER=$(terraform output -raw domain_owner)
REPO=$(terraform output -raw repository_name)

# Verify domain exists
aws codeartifact describe-domain --domain "$DOMAIN" --domain-owner "$OWNER" \
  --query "domain.{Name:name,Status:status}" --output table
```

Expected: domain name with status "Active".

```bash
# Verify repository has upstream connection
aws codeartifact describe-repository --domain "$DOMAIN" --domain-owner "$OWNER" \
  --repository "$REPO" \
  --query "repository.{Name:name,Upstreams:upstreams[*].repositoryName}" --output json
```

Expected: repository with upstream pointing to the go-public-upstream repository.

```bash
# Verify auth token can be obtained
aws codeartifact get-authorization-token --domain "$DOMAIN" --domain-owner "$OWNER" \
  --query "expiration" --output text
```

Expected: a future timestamp (12 hours from now).

## Cleanup

```bash
# Delete the CodeArtifact resources
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured CodeArtifact for Go module management with upstream connections. In the next exercise, you will build **automated testing in CI/CD pipelines** -- integrating unit tests, integration tests, and CodeBuild test reports into a deployment pipeline.

## Summary

- **CodeArtifact domains** are the top-level administrative boundary -- they contain repositories and control encryption (AWS-managed or customer-managed KMS key)
- **Repositories** store package versions and can have **upstream connections** to other repositories or external public registries
- **External connections** proxy public registries: `public:go` (proxy.golang.org), `public:npmjs` (npmjs.com), `public:pypi` (pypi.org), `public:maven-central`
- **Authorization tokens** are time-limited (12-hour default) and obtained via `get-authorization-token` -- they must be refreshed before expiration
- For Go modules: set `GOPROXY=https://aws:TOKEN@ENDPOINT/v1,direct` to route module downloads through CodeArtifact
- **Domain policies** control cross-account access; **repository policies** control package-level access
- Packages downloaded through upstream connections are **cached** in the downstream repository -- subsequent requests do not hit the upstream
- **Package version disposal** soft-deletes versions to manage storage; disposed versions can be re-fetched from upstream if needed
- IAM permissions needed: `codeartifact:GetAuthorizationToken`, `codeartifact:GetRepositoryEndpoint`, `codeartifact:ReadFromRepository`, `sts:GetServiceBearerToken`

## Reference

- [CodeArtifact User Guide](https://docs.aws.amazon.com/codeartifact/latest/ug/welcome.html)
- [Terraform aws_codeartifact_domain](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codeartifact_domain)
- [Terraform aws_codeartifact_repository](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/codeartifact_repository)
- [CodeArtifact with Go](https://docs.aws.amazon.com/codeartifact/latest/ug/using-go.html)

## Additional Resources

- [CodeArtifact External Connections](https://docs.aws.amazon.com/codeartifact/latest/ug/external-connection.html) -- supported external registries and configuration
- [Cross-Account Access](https://docs.aws.amazon.com/codeartifact/latest/ug/repo-policies.html) -- sharing repositories across AWS accounts
- [CodeArtifact with CodeBuild](https://docs.aws.amazon.com/codeartifact/latest/ug/using-go.html#go-codebuild) -- configuring GOPROXY in CodeBuild buildspec
- [Package Version Lifecycle](https://docs.aws.amazon.com/codeartifact/latest/ug/packages-overview.html) -- understanding package status (Published, Archived, Disposed, Deleted)
