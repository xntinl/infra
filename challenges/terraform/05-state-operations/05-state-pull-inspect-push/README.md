# 25. State Pull, Inspect, and Push

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- `jq` installed (JSON processor)
- Completed exercise 24 (Declarative Import Block)

## Learning Objectives

After completing this exercise, you will be able to:

- Extract the full Terraform state file as JSON using `terraform state pull`
- Navigate the state structure and identify key fields: version, serial, lineage, and resources
- Create safe backups of state files for disaster recovery
- Understand when and how `terraform state push` is used for state recovery

## Why Understand State Internals

Every `terraform plan` and `terraform apply` reads and writes the state file. When everything works, you never need to look inside it. But when things go wrong -- corrupted state, lost backend access, accidental deletions, or migration between backends -- understanding the state file's structure becomes essential.

The state file is JSON with a well-defined schema. Three fields are critical:

- **`version`**: The state file format version. Terraform uses this to know how to parse the file.
- **`serial`**: An incrementing counter that changes with every state modification. It serves as a concurrency control mechanism -- if two operations try to write the same serial, one will fail.
- **`lineage`**: A UUID that uniquely identifies this state's history. It prevents you from accidentally pushing a state file from one project into another. `terraform state push` will refuse to overwrite a state with a different lineage unless you force it.

Knowing how to pull, inspect, and back up state gives you the confidence to handle state emergencies.

## Step 1 -- Create Resources

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

resource "aws_ssm_parameter" "alpha" {
  name  = "/kata/alpha"
  type  = "String"
  value = "alpha-value"
}

resource "aws_ssm_parameter" "beta" {
  name  = "/kata/beta"
  type  = "String"
  value = "beta-value"
}
```

```bash
terraform init
terraform apply -auto-approve
```

## Step 2 -- Pull and Save the State

```bash
terraform state pull > state-backup.json
```

This downloads the entire state file as JSON to your local machine. The file is a complete snapshot of everything Terraform knows about your infrastructure.

## Step 3 -- Inspect the Top-Level Fields

```bash
cat state-backup.json | jq '.version'
```

Expected output:

```
4
```

```bash
cat state-backup.json | jq '.serial'
```

Expected output (number will vary):

```
3
```

```bash
cat state-backup.json | jq '.lineage'
```

Expected output (UUID will vary):

```
"a1b2c3d4-e5f6-7890-abcd-ef1234567890"
```

## Step 4 -- Inspect the Resources

```bash
cat state-backup.json | jq '.resources | length'
```

Expected output:

```
2
```

```bash
cat state-backup.json | jq '.resources[].type'
```

Expected output:

```
"aws_ssm_parameter"
"aws_ssm_parameter"
```

```bash
cat state-backup.json | jq '.resources[].instances[].attributes.name'
```

Expected output:

```
"/kata/alpha"
"/kata/beta"
```

## Step 5 -- Observe Serial Increment

Record the current serial:

```bash
SERIAL_BEFORE=$(cat state-backup.json | jq '.serial')
echo "Serial before: $SERIAL_BEFORE"
```

Make a change and apply:

```bash
terraform apply -auto-approve -var='unused=trigger' 2>/dev/null || terraform apply -auto-approve
```

Pull the state again and compare:

```bash
terraform state pull | jq '.serial'
```

The serial should be higher than before. Every state-modifying operation increments it.

Verify the lineage stayed the same:

```bash
terraform state pull | jq '.lineage'
```

The lineage UUID should be identical to what you saw earlier. It never changes for a given state chain.

## Step 6 -- Understand State Push (Conceptual)

`terraform state push` uploads a JSON state file to the configured backend. It is a dangerous operation meant only for recovery scenarios:

```bash
# DO NOT run this unless you need to recover state
# terraform state push state-backup.json
```

Safety checks enforced by `state push`:

- The serial in the pushed file must be greater than the current serial in the backend.
- The lineage must match the current state. A mismatched lineage requires the `-force` flag.

**When to use it:** Backend migration failures, state corruption recovery, or restoring from a backup after an accidental `terraform state rm`.

## Step 7 -- Clean Up

```bash
terraform destroy -auto-approve
rm -f state-backup.json
```

## Common Mistakes

### Editing the state JSON manually and pushing it back

Manually editing the state file is fragile and error-prone. If you change a resource attribute without updating the corresponding hash, or if you set the serial incorrectly, Terraform may behave unpredictably. Use `terraform state` subcommands (mv, rm, import) instead of manual JSON editing whenever possible.

### Using `state push -force` without understanding the consequences

The `-force` flag bypasses both serial and lineage checks. If you push a state file from a different project, you can permanently overwrite your real state. Only use `-force` when you are absolutely certain the file you are pushing is correct and you understand why the normal checks are failing.

## Verify What You Learned

1. Confirm the state has the expected top-level keys:

```bash
terraform state pull | jq 'keys'
```

Expected output:

```
[
  "check_results",
  "lineage",
  "outputs",
  "resources",
  "serial",
  "terraform_version",
  "version"
]
```

2. Confirm each resource has the required structure:

```bash
terraform state pull | jq '.resources[0] | keys'
```

Expected output includes:

```
[
  "instances",
  "mode",
  "name",
  "provider",
  "type"
]
```

3. Confirm the serial increments after an apply:

```bash
SERIAL1=$(terraform state pull | jq '.serial')
terraform apply -auto-approve -refresh-only
SERIAL2=$(terraform state pull | jq '.serial')
echo "Before: $SERIAL1, After: $SERIAL2"
```

The "After" value should be greater than the "Before" value.

4. Confirm the lineage remains constant:

```bash
terraform state pull | jq -r '.lineage'
```

This should output the same UUID every time, regardless of how many operations you perform.

## Section 05 Summary -- State Operations

Across these five exercises, you have built a comprehensive understanding of Terraform state management:

| Exercise | Skill | Command / Block |
|----------|-------|-----------------|
| 21 | Import existing resources | `terraform import` |
| 22 | Refactor without destroying | `terraform state mv` |
| 23 | Stop managing without deleting | `terraform state rm` / `removed` block |
| 24 | Declarative, reviewable imports | `import` block |
| 25 | Inspect and back up state | `terraform state pull` / `jq` |

Key takeaways:

- The state file is the single source of truth for what Terraform manages. Every operation you learned manipulates this file.
- Imperative commands (`import`, `state mv`, `state rm`) modify state immediately. Declarative alternatives (`import` block, `removed` block, `moved` block) are preferred in team workflows because they are visible in code reviews.
- The state's `serial` provides concurrency control. The `lineage` prevents cross-project state corruption. Understanding these fields is essential for disaster recovery.
- Always back up state before performing manual state operations.

## What's Next

In the next section, you will learn about lifecycle rules -- `create_before_destroy`, `prevent_destroy`, `ignore_changes`, and `replace_triggered_by` -- which give you fine-grained control over how Terraform manages the creation, update, and destruction of individual resources.

## Reference

- [terraform state pull Command](https://developer.hashicorp.com/terraform/cli/commands/state/pull)
- [terraform state push Command](https://developer.hashicorp.com/terraform/cli/commands/state/push)

## Additional Resources

- [HashiCorp: Manage Resources in Terraform State Tutorial](https://developer.hashicorp.com/terraform/tutorials/state/state-cli) -- official tutorial for inspecting and manipulating state with CLI commands
- [Spacelift: Terraform State Guide](https://spacelift.io/blog/terraform-state) -- comprehensive guide to state file structure, backups, and recovery operations
- [jq Manual](https://jqlang.github.io/jq/manual/) -- reference for the jq command-line JSON processor
