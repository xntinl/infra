# 44. Pin Module Versions from Git

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 43 (Module depends_on)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `version` constraint argument to pin modules from the Terraform Registry
- Reference Git-sourced modules by tag, branch, or commit SHA using the `ref` parameter
- Inspect the `.terraform.lock.hcl` file to verify provider hashes
- Upgrade dependencies within constraints using `terraform init -upgrade`

## Why Version Pinning

Without version constraints, `terraform init` downloads whatever the latest version of a module happens to be. This creates two problems. First, builds are not reproducible -- the same code might produce different plans on different machines or at different times. Second, a breaking change in an upstream module can silently alter your infrastructure.

Version pinning solves this by declaring which versions are acceptable. The `~>` operator allows automatic patch or minor updates within a bounded range (e.g., `~> 4.0` accepts `4.x` but not `5.0`), while Git refs lock to a specific tag, branch, or commit SHA. The commit SHA is the most precise option because tags can be moved and branches are mutable. Combining version constraints with the `.terraform.lock.hcl` lock file ensures that every team member and CI run uses the exact same dependencies.

## Step 1 -- Pin a Registry Module

Create `main.tf`:

```hcl
# main.tf

module "s3_bucket" {
  source  = "terraform-aws-modules/s3-bucket/aws"
  version = "~> 4.0"

  bucket = "kata-version-pin-test"
}

output "module_bucket_arn" {
  value = module.s3_bucket.s3_bucket_arn
}
```

The `version = "~> 4.0"` constraint allows any 4.x release but blocks 5.0 and above.

## Step 2 -- Explore Git-Based Sources

The following examples are commented out because they require specific repositories, but they illustrate the three Git pinning strategies:

```hcl
# From Git with tag (exact version):
# module "vpc" {
#   source = "git::https://github.com/terraform-aws-modules/terraform-aws-vpc.git?ref=v5.1.0"
# }

# From Git with branch (follows HEAD of that branch):
# module "experimental" {
#   source = "git::https://github.com/myorg/tf-modules.git//modules/experimental?ref=feature-branch"
# }

# From Git with commit SHA (most precise and immutable):
# module "locked" {
#   source = "git::https://github.com/myorg/tf-modules.git?ref=abc1234567890"
# }
```

**Tag**: readable and semantic, but tags can be moved (rare but possible).
**Branch**: convenient for development, but the target changes with every push. Never use in production.
**Commit SHA**: immutable and fully reproducible. Best choice for production pinning from Git.

## Step 3 -- Initialize and Inspect

```bash
terraform init
```

### Scenario A -- Successful init (expected)

```
Initializing modules...
Downloading registry.terraform.io/terraform-aws-modules/s3-bucket/aws 4.x.x for s3_bucket...

Initializing provider plugins...

Terraform has been successfully initialized!
```

### Scenario B -- Inspecting the lock file

```bash
cat .terraform.lock.hcl
```

The lock file records the exact provider versions and their cryptographic hashes. This file should be committed to version control.

### Scenario C -- Changing the version constraint

Change `version = "~> 4.0"` to `version = "~> 3.0"` and run:

```bash
terraform init -upgrade
```

Terraform downloads a 3.x release instead. The `-upgrade` flag is required because Terraform will not downgrade cached modules without it.

### Scenario D -- Upgrading within constraints

With `version = "~> 4.0"`, if a new 4.x release is published:

```bash
terraform init -upgrade
```

Terraform downloads the latest 4.x version that satisfies the constraint, updating the lock file accordingly.

## Common Mistakes

### Omitting version constraints on Registry modules

Without a `version` argument, Terraform downloads the latest version every time. This can introduce breaking changes silently. Always specify a constraint, even if it is broad (e.g., `>= 4.0, < 5.0`).

### Using a branch ref in production

Branch references (`ref=main`) follow the HEAD of the branch, which changes with every merge. This makes builds non-reproducible. For production, always use a tag or commit SHA.

## Verify What You Learned

```bash
terraform init
```

Expected: `Terraform has been successfully initialized!`

```bash
cat .terraform.lock.hcl | head -20
```

Expected: provider entries with `version` and `hashes` fields.

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: X to add, 0 to change, 0 to destroy.` (the exact count depends on what the S3 module creates internally).

## Section 10 Summary -- Modules

Across four exercises you learned the complete module workflow:

- **Exercise 41** -- A minimal module with `variables.tf`, `main.tf`, `outputs.tf`, input validation, and tag merging.
- **Exercise 42** -- `for_each` on module blocks to create multiple instances from a map with stable string keys.
- **Exercise 43** -- `depends_on` for declaring hidden dependencies between modules and resources.
- **Exercise 44** -- Version pinning from the Terraform Registry (`~>`) and Git (`ref=tag|branch|SHA`), plus the `.terraform.lock.hcl` lock file.

These patterns let you build, scale, order, and version modules reliably -- the foundation of every production Terraform codebase.

## What's Next

In section 11 (Testing) you will learn how to validate your modules using Terraform's native testing framework: plan-mode tests with `.tftest.hcl` files, integration tests that create and destroy real infrastructure, and mock providers for fast, credential-free unit testing.

## Reference

- [Module Sources](https://developer.hashicorp.com/terraform/language/modules/sources)

## Additional Resources

- [Lock and Upgrade Provider Versions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/provider-versioning) -- tutorial on the .terraform.lock.hcl file and provider version management
- [Terraform Module Versioning Best Practices](https://spacelift.io/blog/terraform-module-versioning) -- strategies for versioning modules with Git tags and semantic constraints
- [Terraform Module Versioning for Git Sources](https://medium.com/dazn-tech/terraform-module-versioning-for-git-sources-5a792ceb74d7) -- practical walkthrough of version pinning using Git repositories with ref
