# 34. Extending Terraform with the External Data Source

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- `jq` installed (required by the external script)
- `git` installed
- Completed exercise 33 (Cross-Project State Sharing with terraform_remote_state)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `data "external"` data source to execute a shell script and consume its JSON output
- Implement the JSON stdin/stdout protocol that Terraform requires for external scripts
- Understand the string-only output limitation and how to work within it

## Why the External Data Source

Terraform providers cover the vast majority of infrastructure needs, but sometimes you need information that no provider exposes: the current Git branch, a value from a custom API, the output of a build tool, or system metadata. The `external` data source bridges this gap by letting you run any program and capture its output as Terraform data.

The protocol is simple: Terraform sends the `query` map as JSON to the program's stdin, and the program must return a flat JSON object (all values must be strings) on stdout. If the program exits with a non-zero code, Terraform treats it as an error.

This is a powerful escape hatch, but it comes with trade-offs. External scripts make your configuration depend on the execution environment (the script must be present, dependencies like `jq` must be installed, and behavior may vary across operating systems). Use it when no native data source exists and the information is genuinely needed at plan time.

## Step 1 -- Create the Script

Create the directory and script file `scripts/get-git-info.sh`:

```bash
#!/bin/bash
set -e

# Read the query parameters from stdin (JSON)
eval "$(jq -r '@sh "REPO_PATH=\(.repo_path)"')"

# Navigate to the repository, or return defaults if it fails
cd "$REPO_PATH" 2>/dev/null || {
  echo '{"branch":"unknown","commit":"unknown","dirty":"unknown"}'
  exit 0
}

# Extract Git information
BRANCH=$(git rev-parse --abbrev-ref HEAD 2>/dev/null || echo "unknown")
COMMIT=$(git rev-parse --short HEAD 2>/dev/null || echo "unknown")
DIRTY=$(git diff --quiet 2>/dev/null && echo "false" || echo "true")

# Return as a flat JSON object with string-only values
jq -n --arg branch "$BRANCH" --arg commit "$COMMIT" --arg dirty "$DIRTY" \
  '{"branch": $branch, "commit": $commit, "dirty": $dirty}'
```

Make it executable:

```bash
chmod +x scripts/get-git-info.sh
```

## Step 2 -- Create the Terraform Configuration

Create `main.tf`:

```hcl
# main.tf

data "external" "git_info" {
  program = ["bash", "${path.module}/scripts/get-git-info.sh"]

  query = {
    repo_path = path.module
  }
}

locals {
  git_branch = data.external.git_info.result.branch
  git_commit = data.external.git_info.result.commit
  git_dirty  = data.external.git_info.result.dirty
}

output "git_branch" { value = local.git_branch }
output "git_commit" { value = local.git_commit }
output "git_dirty"  { value = local.git_dirty }
```

## Step 3 -- Understand the Protocol

The communication between Terraform and the script follows a strict contract:

1. **Input**: Terraform sends the `query` map as JSON to stdin: `{"repo_path": "/path/to/module"}`
2. **Output**: The script must print a flat JSON object to stdout: `{"branch": "main", "commit": "abc1234", "dirty": "false"}`
3. **Exit code**: Zero means success. Non-zero means error, and Terraform will display stderr as the error message.
4. **String-only values**: All values in the output JSON must be strings. Numbers, booleans, arrays, and nested objects are not allowed.

## Step 4 -- Test the Script Independently

Before running it through Terraform, test the script directly:

```bash
echo '{"repo_path": "."}' | bash scripts/get-git-info.sh
```

Expected output (values depend on your local Git state):

```json
{"branch": "main", "commit": "abc1234", "dirty": "false"}
```

## Step 5 -- Initialize and Plan

```bash
terraform init
terraform plan
```

## Step 6 -- Test the Failure Scenario

Modify the script to exit with a non-zero code and observe how Terraform handles the error:

```bash
echo '{"repo_path": "/nonexistent/path"}' | bash scripts/get-git-info.sh
```

The script is designed to handle this gracefully by returning `"unknown"` values. A less defensive script that exits with code 1 would cause `terraform plan` to fail entirely.

## Common Mistakes

### Returning non-string values in the JSON output

The external data source requires all output values to be strings. Returning `{"dirty": true}` (a boolean) or `{"count": 42}` (a number) causes an error:

```
Error: command "bash" produced invalid output: key "dirty" has non-string value
```

Always serialize as strings: `{"dirty": "true"}`, `{"count": "42"}`.

### Writing debug output to stdout

If your script prints anything to stdout besides the final JSON object, Terraform will fail to parse the output. Use stderr for debug messages:

```bash
echo "Debug: processing..." >&2   # This goes to stderr (OK)
echo '{"result": "value"}'        # This goes to stdout (parsed by Terraform)
```

## Verify What You Learned

Run the following commands and confirm the output matches the expected patterns:

```bash
terraform apply -auto-approve
```

```bash
terraform output git_branch
```

Expected: the name of your current Git branch, e.g. `"main"` or `"feature/my-branch"`

```bash
terraform output git_commit
```

Expected: a short Git commit hash, e.g. `"abc1234"`

```bash
terraform output git_dirty
```

Expected: `"true"` or `"false"` depending on whether you have uncommitted changes

```bash
# Verify all output values are strings
terraform output -json | jq 'to_entries[] | {key: .key, type: (.value.value | type)}'
```

Expected: all values should show `"type": "string"`

## Section 07 Summary: Data Sources

Across exercises 30-34, you learned the core data source patterns in Terraform:

| Exercise | Data Source | What It Does |
|----------|-----------|--------------|
| 30 | `aws_ami` | Dynamically resolves AMI IDs by filters instead of hardcoding |
| 31 | `aws_availability_zones` | Discovers AZs at plan time for region-agnostic configs |
| 32 | `aws_caller_identity` / `aws_region` / `aws_partition` | Provides runtime context for building dynamic ARNs |
| 33 | `terraform_remote_state` | Reads outputs from another Terraform state for cross-project sharing |
| 34 | `external` | Runs a shell script and captures its JSON output |

**Key takeaways:**

- Data sources read information without creating resources -- they are the read-only counterpart to resources.
- Use data sources to eliminate hardcoded values that vary by region, account, or time.
- The `external` data source is an escape hatch for when no native provider exists, but use it sparingly due to environment dependencies.
- `terraform_remote_state` enables layered architectures with clear contracts between independent projects.

## What's Next

In section 08 (Outputs and Locals), you will learn how to build structured outputs that expose rich data from modules, use locals to derive and centralize computed values, and chain locals together for multi-step data transformations.

## Reference

- [Terraform external Data Source](https://registry.terraform.io/providers/hashicorp/external/latest/docs/data-sources/external)

## Additional Resources

- [Tech Notes: Terraform Bash Script External Data Source](https://www.tech-notes.net/terraform-bash-script-external-data-source/) -- step-by-step tutorial for creating bash scripts compatible with the external data source
- [Spacelift: How to Use Terraform Data Sources](https://spacelift.io/blog/terraform-data-sources) -- comprehensive guide covering the external provider and its use cases
- [Using External Data Sources in Terraform (DEV.to)](https://dev.to/pwd9000/using-external-data-sources-in-terraform-2c9g) -- practical examples of external scripts for extending Terraform
