# 22. Terraform Infrastructure Management with Just

<!--
difficulty: advanced
concepts:
  - terraform init with backend config
  - workspace-based environments
  - plan/apply/destroy with confirm
  - cargo-lambda build and deploy
  - SSM parameter management
  - AWS identity verification
tools: [just, terraform, aws-cli, cargo-lambda]
estimated_time: 50 minutes
bloom_level: evaluate
prerequisites:
  - just modules and imports
  - terraform basics (init, plan, apply, workspaces)
  - AWS CLI fundamentals
  - cargo-lambda for Rust Lambda builds
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| terraform | 1.5+ | `terraform --version` |
| aws-cli | 2.x | `aws --version` |
| cargo-lambda | 1.0+ | `cargo lambda --version` |

## Learning Objectives

- **Evaluate** safety mechanisms (`[confirm]`, identity checks, workspace guards) that prevent accidental infrastructure damage
- **Design** a Just-based Terraform workflow that enforces workspace-per-environment isolation
- **Justify** the separation of build, deploy, and infrastructure concerns in a Lambda-based project

## Why Just for Terraform Workflows

Terraform's CLI is powerful but verbose. A typical plan command involves changing directories, selecting workspaces, passing variable files, and remembering backend configuration. Teams end up with Makefiles or shell scripts that rot over time. Just provides a cleaner abstraction with built-in confirmation prompts, environment variable handling, and dependency chains.

The critical challenge in infrastructure management is safety. Running `terraform apply` against production instead of staging is a career-defining mistake. A well-designed justfile enforces guardrails: confirming the target environment, verifying AWS identity, checking workspace alignment, and requiring explicit confirmation for destructive operations.

This exercise builds a complete Terraform workflow for a Rust Lambda project, covering init, plan, apply, destroy, Lambda builds, and SSM parameter management — all orchestrated through Just with environment-based targeting.

## Step 1 -- Define Project Variables and Settings

```just
# justfile

set dotenv-load
set export
set positional-arguments

# Project configuration
project   := env('PROJECT_NAME', 'acme-platform')
env_name  := env('ENV', 'dev')
region    := env('AWS_REGION', 'us-east-1')
tf_dir    := 'terraform'
config_dir := 'config'

# Derived values
state_bucket := project + "-terraform-state"
state_key    := "terraform.tfstate"

# Color constants
RED    := '\033[0;31m'
GREEN  := '\033[0;32m'
YELLOW := '\033[1;33m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'
```

The `env()` function reads from environment variables with fallbacks. Combined with `set dotenv-load`, values can come from `.env`, shell exports, or inline overrides like `ENV=prod just plan`.

## Step 2 -- AWS Identity Verification

Before any infrastructure operation, verify who you are and where you are pointing.

```just
# Verify current AWS identity and display account info
whoami:
    @echo "{{ BOLD }}AWS Identity Check{{ NORMAL }}"
    @echo "─────────────────────────────"
    @aws sts get-caller-identity --output table
    @echo ""
    @echo "Target environment: {{ YELLOW }}{{ env_name }}{{ NORMAL }}"
    @echo "Target region:      {{ YELLOW }}{{ region }}{{ NORMAL }}"
    @echo "State bucket:       {{ state_bucket }}"
```

This recipe should be a dependency for any dangerous operation. Think about when you would skip it (hint: local-only commands like `fmt` or `validate`).

## Step 3 -- Terraform Init and Workspace Management

```just
# Initialize Terraform with backend configuration
init: whoami
    terraform -chdir={{ tf_dir }} init \
        -backend-config="bucket={{ state_bucket }}" \
        -backend-config="key={{ state_key }}" \
        -backend-config="region={{ region }}"
    terraform -chdir={{ tf_dir }} workspace select {{ env_name }} || \
        terraform -chdir={{ tf_dir }} workspace new {{ env_name }}
    @echo "{{ GREEN }}Initialized workspace: {{ env_name }}{{ NORMAL }}"

# List all Terraform workspaces
workspaces:
    @terraform -chdir={{ tf_dir }} workspace list

# Show current workspace (and verify alignment with ENV)
workspace-check:
    #!/usr/bin/env bash
    set -euo pipefail
    current=$(terraform -chdir={{ tf_dir }} workspace show)
    if [[ "$current" != "{{ env_name }}" ]]; then
        echo "{{ RED }}MISMATCH: workspace is '$current' but ENV={{ env_name }}{{ NORMAL }}"
        echo "Run: just init"
        exit 1
    fi
    echo "{{ GREEN }}Workspace '$current' matches ENV={{ env_name }}{{ NORMAL }}"
```

The `workspace-check` recipe uses a shebang block for multi-line bash logic. Notice `set -euo pipefail` — always use this in shebang recipes to catch errors.

## Step 4 -- Plan and Apply with Safety Gates

```just
# Generate and display Terraform execution plan
plan: workspace-check
    terraform -chdir={{ tf_dir }} plan \
        -var="project_name={{ project }}" \
        -var="region={{ region }}" \
        -out={{ env_name }}.tfplan
    @echo ""
    @echo "{{ GREEN }}Plan saved to {{ env_name }}.tfplan{{ NORMAL }}"
    @echo "Run {{ BOLD }}just apply{{ NORMAL }} to execute"

# Apply the saved plan
[confirm("Apply Terraform changes to '{{ env_name }}'? (yes/no)")]
apply: workspace-check
    terraform -chdir={{ tf_dir }} apply {{ env_name }}.tfplan
    @echo "{{ GREEN }}Applied successfully to {{ env_name }}{{ NORMAL }}"

# Destroy all infrastructure in the target environment
[confirm("DESTROY all resources in '{{ env_name }}'? This cannot be undone. (yes/no)")]
destroy: whoami workspace-check
    @echo "{{ RED }}{{ BOLD }}Destroying {{ env_name }} infrastructure...{{ NORMAL }}"
    terraform -chdir={{ tf_dir }} destroy \
        -var="project_name={{ project }}" \
        -var="region={{ region }}"
```

The `[confirm]` attribute is the key safety mechanism. It prompts interactively before execution. For `destroy`, we chain both `whoami` and `workspace-check` to force the operator to see exactly what they are about to delete.

## Step 5 -- Lambda Build and Deploy

```just
# Build a single Lambda function
build name:
    cargo lambda build --release --arm64 -p {{ name }}
    @echo "{{ GREEN }}Built {{ name }}{{ NORMAL }}"

# Build all Lambda functions defined in config
build-all:
    #!/usr/bin/env bash
    set -euo pipefail
    for fn in $(yq -r '.functions[].name' {{ config_dir }}/functions.yaml); do
        echo "Building $fn..."
        cargo lambda build --release --arm64 -p "$fn"
    done
    echo "{{ GREEN }}All functions built{{ NORMAL }}"

# Deploy: build, plan, and apply
deploy: build-all plan
    @echo "{{ YELLOW }}Review the plan above, then run:{{ NORMAL }}"
    @echo "  just apply"

# Quick deploy a single function (update code only, no infra changes)
deploy-fn name: (build name)
    aws lambda update-function-code \
        --function-name {{ project }}-{{ env_name }}-{{ name }} \
        --zip-file fileb://target/lambda/{{ name }}/bootstrap.zip \
        --region {{ region }}
    @echo "{{ GREEN }}Deployed {{ name }} to {{ env_name }}{{ NORMAL }}"
```

## Step 6 -- SSM Parameter Management

```just
# Set an SSM parameter
ssm-put path value:
    aws ssm put-parameter \
        --name "/{{ project }}/{{ env_name }}/{{ path }}" \
        --value "{{ value }}" \
        --type SecureString \
        --overwrite \
        --region {{ region }}
    @echo "{{ GREEN }}Set /{{ project }}/{{ env_name }}/{{ path }}{{ NORMAL }}"

# Read an SSM parameter
ssm-get path:
    @aws ssm get-parameter \
        --name "/{{ project }}/{{ env_name }}/{{ path }}" \
        --with-decryption \
        --query 'Parameter.Value' \
        --output text \
        --region {{ region }}

# List all parameters for current environment
ssm-list:
    @aws ssm get-parameters-by-path \
        --path "/{{ project }}/{{ env_name }}/" \
        --recursive \
        --query 'Parameters[].Name' \
        --output table \
        --region {{ region }}
```

## Step 7 -- Utility Recipes

```just
# Format Terraform files
fmt:
    terraform -chdir={{ tf_dir }} fmt -recursive
    @echo "{{ GREEN }}Formatted{{ NORMAL }}"

# Validate Terraform configuration
validate:
    terraform -chdir={{ tf_dir }} validate
    @echo "{{ GREEN }}Valid{{ NORMAL }}"

# Show Terraform outputs for current environment
outputs:
    @terraform -chdir={{ tf_dir }} output

# Show specific output value
output name:
    @terraform -chdir={{ tf_dir }} output -raw {{ name }}

# Tail Lambda logs
logs name lines='50':
    aws logs tail /aws/lambda/{{ project }}-{{ env_name }}-{{ name }} \
        --since 1h \
        --format short \
        --follow \
        --region {{ region }}
```

## Common Mistakes

**Wrong: Forgetting workspace alignment before apply**
```just
apply:
    terraform -chdir=terraform apply
```
What happens: If you last initialized against production but `ENV=dev` is set, you apply dev-intended changes to production state. Terraform uses the last-selected workspace regardless of your environment variable.
Fix: Always include `workspace-check` as a dependency for `plan`, `apply`, and `destroy`. The check ensures the active workspace matches the `ENV` variable.

**Wrong: Using `terraform apply` without a saved plan**
```just
apply:
    terraform -chdir=terraform apply -auto-approve
```
What happens: Terraform generates a new plan at apply time. If resources changed between plan and apply, you get unexpected modifications. `-auto-approve` bypasses all review.
Fix: Always `plan -out=file.tfplan` first, then `apply file.tfplan`. The saved plan ensures you apply exactly what you reviewed.

## Verify What You Learned

```bash
# Verify identity check runs before init
just whoami
# Expected: AWS identity table + target environment info

# Verify workspace check catches mismatches
ENV=prod just workspace-check
# Expected: MISMATCH error if workspace is not 'prod'

# Verify plan generates a saved plan file
just plan
# Expected: Plan saved to dev.tfplan

# Verify confirm prompt on apply
just apply
# Expected: "Apply Terraform changes to 'dev'? (yes/no)" prompt

# Verify confirm prompt on destroy
just destroy
# Expected: identity check + workspace check + "DESTROY all resources" prompt
```

## What's Next

The next exercise ([23. Cross-Platform Justfile](../03-cross-platform-justfile/03-cross-platform-justfile.md)) explores writing justfiles that work across Linux, macOS, and Windows using OS detection and platform-specific attributes.

## Summary

- `set dotenv-load` + `env()` with fallbacks enables flexible environment targeting
- `[confirm]` on `apply` and `destroy` prevents accidental execution
- Workspace-check recipes ensure `ENV` variable matches active Terraform workspace
- Saved plan files (`-out=`) guarantee you apply exactly what you reviewed
- Shebang recipes (`#!/usr/bin/env bash`) handle multi-line logic with proper error handling
- Dependency chains (`destroy: whoami workspace-check`) layer safety gates

## Reference

- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just Environment Variables](https://just.systems/man/en/environment-variables.html)
- [Just Shebang Recipes](https://just.systems/man/en/shebang-recipes.html)

## Additional Resources

- [Terraform Workspaces](https://developer.hashicorp.com/terraform/language/state/workspaces)
- [cargo-lambda Documentation](https://www.cargo-lambda.info/)
- [AWS CLI SSM Reference](https://docs.aws.amazon.com/cli/latest/reference/ssm/)
