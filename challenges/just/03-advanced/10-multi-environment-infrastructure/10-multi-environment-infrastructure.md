# 30. Multi-Environment Infrastructure

<!--
difficulty: advanced
concepts:
  - Terraform workspace management
  - environment-specific tfvars
  - plan/review/apply workflow with safety checks
  - state locking awareness
  - multi-region coordination
  - confirm for destroy
  - drift detection
  - cost estimation integration
tools: [just, terraform, aws-cli]
estimated_time: 55 minutes
bloom_level: evaluate
prerequisites:
  - just advanced (modules, conditional expressions, confirm attribute)
  - Terraform workspaces, state, and backend configuration
  - multi-environment infrastructure concepts
  - AWS fundamentals
-->

## Prerequisites

| Tool | Minimum Version | Check Command |
|------|----------------|---------------|
| just | 1.25+ | `just --version` |
| terraform | 1.5+ | `terraform --version` |
| aws-cli | 2.x | `aws --version` |

## Learning Objectives

- **Evaluate** safety mechanisms that prevent cross-environment contamination in infrastructure pipelines
- **Design** a multi-region, multi-environment Terraform workflow with workspace isolation and progressive deployment
- **Justify** the ordering of plan, review, and apply stages and the role of saved plans in preventing drift-related surprises

## Why Multi-Environment Infrastructure Orchestration

Most organizations run at least three environments: development for rapid iteration, staging for pre-production validation, and production for live traffic. Each environment may span multiple regions for latency or compliance. Managing this matrix with raw Terraform commands is error-prone — one wrong workspace selection and development changes hit production.

The challenge compounds with team coordination. Multiple engineers running `terraform apply` against the same state introduces race conditions. State locking prevents concurrent writes, but the justfile should make locking behavior visible rather than surprising. Drift detection — comparing the actual infrastructure state against the Terraform configuration — catches manual console changes that bypass the IaC workflow.

This exercise builds a comprehensive infrastructure management justfile with workspace-per-environment isolation, environment-specific variable files, progressive deployment (dev → staging → prod), drift detection, and cost estimation. Every destructive operation requires confirmation proportional to its blast radius.

## Step 1 -- Project Layout and Configuration

```just
# justfile

set dotenv-load
set export

# ─── Configuration ──────────────────────────────────────
project    := env("PROJECT_NAME", "platform")
env_name   := env("ENV", "dev")
region     := env("AWS_REGION", "us-east-1")
tf_dir     := "terraform"

# Derived values
state_bucket := project + "-terraform-state"
state_key    := "terraform.tfstate"
lock_table   := project + "-terraform-locks"
tfvars_file  := tf_dir + "/envs/" + env_name + ".tfvars"

# ─── Colors ─────────────────────────────────────────────
RED    := '\033[0;31m'
GREEN  := '\033[0;32m'
YELLOW := '\033[1;33m'
BLUE   := '\033[0;34m'
CYAN   := '\033[0;36m'
BOLD   := '\033[1m'
NORMAL := '\033[0m'

default:
    @just --list --unsorted
```

The expected directory structure:

```
terraform/
  main.tf
  variables.tf
  outputs.tf
  envs/
    dev.tfvars
    staging.tfvars
    prod.tfvars
  modules/
    compute/
    api/
    events/
```

Each environment gets its own `.tfvars` file and Terraform workspace. The workspace name matches the environment name.

## Step 2 -- Identity and Context Verification

```just
# ─── Identity ───────────────────────────────────────────

# Show AWS identity, target environment, and context
whoami:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BOLD }}Infrastructure Context{{ NORMAL }}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
    echo "{{ CYAN }}AWS Identity:{{ NORMAL }}"
    aws sts get-caller-identity --output table
    echo ""
    echo "{{ CYAN }}Target:{{ NORMAL }}"
    echo "  Environment: {{ BOLD }}{{ env_name }}{{ NORMAL }}"
    echo "  Region:      {{ region }}"
    echo "  State:       s3://{{ state_bucket }}/{{ state_key }}"
    echo "  Lock Table:  {{ lock_table }}"
    echo "  Vars File:   {{ tfvars_file }}"

# Verify the tfvars file exists for the target environment
_verify-env:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f "{{ tfvars_file }}" ]]; then
        echo "{{ RED }}No tfvars file for '{{ env_name }}': {{ tfvars_file }}{{ NORMAL }}"
        echo "Available environments:"
        ls {{ tf_dir }}/envs/*.tfvars 2>/dev/null | xargs -I{} basename {} .tfvars | sed 's/^/  /'
        exit 1
    fi
```

The `_verify-env` recipe catches typos like `ENV=prodd` before Terraform tries to load a nonexistent file.

## Step 3 -- Initialization and Workspace Management

```just
# ─── Init ───────────────────────────────────────────────

# Initialize Terraform and select the workspace for the target environment
init: _verify-env
    @echo "{{ BLUE }}Initializing Terraform for {{ env_name }}...{{ NORMAL }}"
    terraform -chdir={{ tf_dir }} init \
        -backend-config="bucket={{ state_bucket }}" \
        -backend-config="key={{ state_key }}" \
        -backend-config="region={{ region }}" \
        -backend-config="dynamodb_table={{ lock_table }}"
    @echo ""
    terraform -chdir={{ tf_dir }} workspace select {{ env_name }} 2>/dev/null || \
        terraform -chdir={{ tf_dir }} workspace new {{ env_name }}
    @echo "{{ GREEN }}Initialized: workspace={{ env_name }}, region={{ region }}{{ NORMAL }}"

# Reconfigure backend (needed when switching regions)
init-reconfigure: _verify-env
    terraform -chdir={{ tf_dir }} init -reconfigure \
        -backend-config="bucket={{ state_bucket }}" \
        -backend-config="key={{ state_key }}" \
        -backend-config="region={{ region }}" \
        -backend-config="dynamodb_table={{ lock_table }}"
    terraform -chdir={{ tf_dir }} workspace select {{ env_name }} 2>/dev/null || \
        terraform -chdir={{ tf_dir }} workspace new {{ env_name }}

# List all workspaces
workspaces:
    @terraform -chdir={{ tf_dir }} workspace list

# Verify workspace matches ENV
_workspace-check:
    #!/usr/bin/env bash
    set -euo pipefail
    current=$(terraform -chdir={{ tf_dir }} workspace show)
    if [[ "$current" != "{{ env_name }}" ]]; then
        echo "{{ RED }}WORKSPACE MISMATCH{{ NORMAL }}"
        echo "  Active workspace: $current"
        echo "  ENV variable:     {{ env_name }}"
        echo ""
        echo "Run: just init"
        exit 1
    fi
```

The DynamoDB lock table (`-backend-config="dynamodb_table="`) enables state locking. Concurrent `terraform apply` commands against the same workspace will block rather than corrupt state.

## Step 4 -- Plan with Saved Output

```just
# ─── Plan ───────────────────────────────────────────────

# Generate an execution plan
plan: _workspace-check _verify-env
    @echo "{{ BLUE }}Planning for {{ BOLD }}{{ env_name }}{{ NORMAL }}{{ BLUE }} ({{ region }})...{{ NORMAL }}"
    @echo ""
    terraform -chdir={{ tf_dir }} plan \
        -var-file="envs/{{ env_name }}.tfvars" \
        -var="project_name={{ project }}" \
        -var="region={{ region }}" \
        -out={{ env_name }}.tfplan
    @echo ""
    @echo "{{ GREEN }}Plan saved: {{ tf_dir }}/{{ env_name }}.tfplan{{ NORMAL }}"
    @echo "Review the plan, then run: {{ BOLD }}just apply{{ NORMAL }}"

# Plan for a specific module only (targeted plan)
plan-target target: _workspace-check _verify-env
    @echo "{{ BLUE }}Targeted plan for {{ target }} in {{ env_name }}...{{ NORMAL }}"
    terraform -chdir={{ tf_dir }} plan \
        -var-file="envs/{{ env_name }}.tfvars" \
        -var="project_name={{ project }}" \
        -var="region={{ region }}" \
        -target={{ target }} \
        -out={{ env_name }}-targeted.tfplan

# Show what a plan would change without saving it
plan-preview: _workspace-check _verify-env
    terraform -chdir={{ tf_dir }} plan \
        -var-file="envs/{{ env_name }}.tfvars" \
        -var="project_name={{ project }}" \
        -var="region={{ region }}"
```

Saved plans (`-out=`) are critical. Between running `plan` and `apply`, another team member might apply changes. Without a saved plan, `apply` would generate a new plan that includes those changes — potentially conflicting with your intent. The saved plan applies exactly what you reviewed.

## Step 5 -- Apply with Environment-Scaled Confirmation

```just
# ─── Apply ──────────────────────────────────────────────

# Apply the saved plan (dev)
[confirm("Apply changes to '{{ env_name }}'? (yes/no)")]
apply: _workspace-check
    #!/usr/bin/env bash
    set -euo pipefail
    plan_file="{{ tf_dir }}/{{ env_name }}.tfplan"
    if [[ ! -f "$plan_file" ]]; then
        echo "{{ RED }}No saved plan found. Run 'just plan' first.{{ NORMAL }}"
        exit 1
    fi
    echo "{{ BLUE }}Applying plan to {{ env_name }}...{{ NORMAL }}"
    terraform -chdir={{ tf_dir }} apply {{ env_name }}.tfplan
    rm -f "$plan_file"
    echo "{{ GREEN }}Applied successfully to {{ env_name }}{{ NORMAL }}"

# Apply to production (extra safety: typed confirmation + clean tree)
apply-prod: _workspace-check
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ env_name }}" != "prod" ]]; then
        echo "{{ RED }}apply-prod requires ENV=prod{{ NORMAL }}"
        exit 1
    fi
    plan_file="{{ tf_dir }}/prod.tfplan"
    if [[ ! -f "$plan_file" ]]; then
        echo "{{ RED }}No saved plan. Run: ENV=prod just plan{{ NORMAL }}"
        exit 1
    fi
    if [[ -n "$(git status --porcelain)" ]]; then
        echo "{{ RED }}Working tree must be clean for production apply{{ NORMAL }}"
        exit 1
    fi
    echo ""
    echo "{{ RED }}{{ BOLD }}PRODUCTION APPLY{{ NORMAL }}"
    echo "  Environment: prod"
    echo "  Region:      {{ region }}"
    echo ""
    read -p "Type 'apply-prod' to confirm: " confirm
    if [[ "$confirm" != "apply-prod" ]]; then
        echo "{{ RED }}Aborted{{ NORMAL }}"
        exit 1
    fi
    terraform -chdir={{ tf_dir }} apply prod.tfplan
    rm -f "$plan_file"
    echo "{{ GREEN }}{{ BOLD }}Production apply complete{{ NORMAL }}"
```

## Step 6 -- Destroy, Drift Detection, and Cost Estimation

```just
# ─── Destroy ────────────────────────────────────────────

# Destroy all resources in the target environment
[confirm("DESTROY all resources in '{{ env_name }}'? This is irreversible. (yes/no)")]
destroy: whoami _workspace-check
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ "{{ env_name }}" == "prod" ]]; then
        echo "{{ RED }}{{ BOLD }}PRODUCTION DESTROY{{ NORMAL }}"
        read -p "Type '{{ project }}-prod-destroy' to confirm: " confirm
        if [[ "$confirm" != "{{ project }}-prod-destroy" ]]; then
            echo "{{ RED }}Aborted{{ NORMAL }}"
            exit 1
        fi
    fi
    terraform -chdir={{ tf_dir }} destroy \
        -var-file="envs/{{ env_name }}.tfvars" \
        -var="project_name={{ project }}" \
        -var="region={{ region }}"
    echo "{{ RED }}{{ BOLD }}Environment {{ env_name }} destroyed{{ NORMAL }}"

# ─── Drift Detection ───────────────────────────────────

# Detect configuration drift (changes made outside Terraform)
drift: _workspace-check _verify-env
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BLUE }}Detecting drift in {{ env_name }}...{{ NORMAL }}"
    output=$(terraform -chdir={{ tf_dir }} plan -detailed-exitcode \
        -var-file="envs/{{ env_name }}.tfvars" \
        -var="project_name={{ project }}" \
        -var="region={{ region }}" 2>&1) || exit_code=$?

    case "${exit_code:-0}" in
        0) echo "{{ GREEN }}No drift detected — infrastructure matches configuration{{ NORMAL }}" ;;
        1) echo "{{ RED }}Error running plan{{ NORMAL }}"; echo "$output"; exit 1 ;;
        2) echo "{{ YELLOW }}{{ BOLD }}DRIFT DETECTED{{ NORMAL }}"
           echo "$output" | grep -E '^\s*(#|~|\+|-)'
           echo ""
           echo "Run {{ BOLD }}just plan{{ NORMAL }} to review and {{ BOLD }}just apply{{ NORMAL }} to reconcile"
           ;;
    esac

# ─── Cost Estimation ───────────────────────────────────

# Estimate cost impact of planned changes (requires infracost)
cost: _workspace-check _verify-env
    #!/usr/bin/env bash
    set -euo pipefail
    if ! command -v infracost >/dev/null 2>&1; then
        echo "{{ YELLOW }}infracost not installed. Install from https://www.infracost.io{{ NORMAL }}"
        echo "Falling back to Terraform plan summary..."
        terraform -chdir={{ tf_dir }} plan \
            -var-file="envs/{{ env_name }}.tfvars" \
            -var="project_name={{ project }}" \
            -var="region={{ region }}" | grep -E "Plan:|No changes"
        exit 0
    fi
    echo "{{ BLUE }}Estimating costs for {{ env_name }}...{{ NORMAL }}"
    infracost breakdown \
        --path {{ tf_dir }} \
        --terraform-var-file "envs/{{ env_name }}.tfvars" \
        --terraform-var "project_name={{ project }}" \
        --terraform-var "region={{ region }}"
```

Drift detection uses `terraform plan -detailed-exitcode`: exit code 0 means no changes, 1 means error, and 2 means changes detected. This maps cleanly to drift/no-drift/error.

## Step 7 -- Progressive Deployment and Utilities

```just
# ─── Progressive Deploy ─────────────────────────────────

# Deploy progressively: dev → staging → prod
promote-to-staging:
    @echo "{{ YELLOW }}Promoting to staging...{{ NORMAL }}"
    @echo "Step 1: Verify dev is healthy"
    ENV=dev just drift
    @echo ""
    @echo "Step 2: Plan staging changes"
    ENV=staging just plan
    @echo ""
    @echo "{{ YELLOW }}Review the staging plan above, then run:{{ NORMAL }}"
    @echo "  ENV=staging just apply"

promote-to-prod:
    @echo "{{ YELLOW }}Promoting to production...{{ NORMAL }}"
    @echo "Step 1: Verify staging is healthy"
    ENV=staging just drift
    @echo ""
    @echo "Step 2: Plan production changes"
    ENV=prod just plan
    @echo ""
    @echo "{{ YELLOW }}Review the production plan above, then run:{{ NORMAL }}"
    @echo "  ENV=prod just apply-prod"

# ─── Utilities ──────────────────────────────────────────

# Format Terraform files
fmt:
    terraform -chdir={{ tf_dir }} fmt -recursive
    @echo "{{ GREEN }}Formatted{{ NORMAL }}"

# Validate configuration
validate: _verify-env
    terraform -chdir={{ tf_dir }} validate
    @echo "{{ GREEN }}Configuration valid{{ NORMAL }}"

# Show Terraform outputs
outputs:
    @terraform -chdir={{ tf_dir }} output

# Show specific output
output name:
    @terraform -chdir={{ tf_dir }} output -raw {{ name }}

# Show resource count in state
state-stats: _workspace-check
    #!/usr/bin/env bash
    set -euo pipefail
    count=$(terraform -chdir={{ tf_dir }} state list 2>/dev/null | wc -l | tr -d ' ')
    echo "{{ BOLD }}State: {{ env_name }}{{ NORMAL }}"
    echo "  Resources: $count"
    echo "  Workspace: $(terraform -chdir={{ tf_dir }} workspace show)"

# List all resources in state
state-list: _workspace-check
    @terraform -chdir={{ tf_dir }} state list

# Show state for a specific resource
state-show resource: _workspace-check
    terraform -chdir={{ tf_dir }} state show {{ resource }}

# Overview of all environments
overview:
    #!/usr/bin/env bash
    set -euo pipefail
    echo "{{ BOLD }}Environment Overview{{ NORMAL }}"
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    for env_file in {{ tf_dir }}/envs/*.tfvars; do
        env=$(basename "$env_file" .tfvars)
        printf "  %-12s %s\n" "$env" "$env_file"
    done
    echo ""
    echo "{{ BOLD }}Workspaces{{ NORMAL }}"
    terraform -chdir={{ tf_dir }} workspace list 2>/dev/null || echo "  (not initialized)"
```

## Common Mistakes

**Wrong: Using a single tfvars file with environment-specific variable overrides**
```just
plan:
    terraform plan -var="env={{ env_name }}" -var="instance_count={{ if env_name == "prod" { "10" } else { "2" } }}"
```
What happens: Environment configuration is scattered across the justfile, Terraform variables, and inline conditionals. Adding a new environment requires editing multiple places. Variable interactions become hard to reason about.
Fix: Use one `.tfvars` file per environment containing all environment-specific values. The justfile only passes `-var-file=envs/{{ env_name }}.tfvars`. All environment differences live in the tfvars files, which are version-controlled and reviewable.

**Wrong: Reusing plans across environments**
```just
plan:
    terraform plan -out=current.tfplan
apply:
    terraform apply current.tfplan
```
What happens: A developer runs `ENV=dev just plan`, then `ENV=prod just apply`. The `current.tfplan` was generated for dev but applied to prod. A saved plan includes the workspace/backend it targets, but the filename gives no indication of which environment it belongs to.
Fix: Include the environment in the plan filename: `-out={{ env_name }}.tfplan`. The apply recipe checks for the environment-specific file, preventing cross-environment accidents.

## Verify What You Learned

```bash
# Show identity and context
just whoami
# Expected: AWS identity + environment/region/state details

# List available environments
just overview
# Expected: dev.tfvars, staging.tfvars, prod.tfvars listed

# Verify workspace alignment
just _workspace-check
# Expected: "MISMATCH" error or silent success

# Plan for dev
ENV=dev just plan
# Expected: plan saved as terraform/dev.tfplan

# Detect drift
ENV=dev just drift
# Expected: "No drift detected" or "DRIFT DETECTED" with change summary

# Attempt production destroy (double confirmation)
ENV=prod just destroy
# Expected: whoami → [confirm] → typed confirmation prompt
```

## What's Next

Congratulations — you have completed the advanced Just exercises. You now have patterns for monorepo orchestration, infrastructure management, cross-platform builds, release pipelines, full-stack development, polyglot recipes, CI/CD pipelines, Kubernetes deployment, dynamic conditionals, and multi-environment infrastructure. Apply these patterns to your own projects and combine them as needed.

## Summary

- Workspace-per-environment isolation prevents cross-environment contamination
- Environment-specific `.tfvars` files keep all environment differences in one place
- Saved plans (`-out={{ env_name }}.tfplan`) prevent drift-related surprises between plan and apply
- DynamoDB state locking (`-backend-config="dynamodb_table="`) prevents concurrent state corruption
- Confirmation scales with blast radius: none for plan, `[confirm]` for apply, typed phrase for production
- `terraform plan -detailed-exitcode` detects drift: 0 = no changes, 2 = drift detected
- Progressive deployment (dev → staging → prod) with drift checks validates each stage before promoting
- `infracost` integration (with graceful fallback) estimates financial impact of changes
- Private recipes (`_verify-env`, `_workspace-check`) enforce invariants without cluttering the recipe list

## Reference

- [Just Confirm Attribute](https://just.systems/man/en/attributes.html)
- [Just Conditional Expressions](https://just.systems/man/en/conditional-expressions.html)
- [Just Shebang Recipes](https://just.systems/man/en/shebang-recipes.html)
- [Just env() Function](https://just.systems/man/en/functions.html)

## Additional Resources

- [Terraform Workspaces](https://developer.hashicorp.com/terraform/language/state/workspaces)
- [Terraform Backend Configuration](https://developer.hashicorp.com/terraform/language/backend)
- [Terraform State Locking](https://developer.hashicorp.com/terraform/language/state/locking)
- [Infracost](https://www.infracost.io/)
