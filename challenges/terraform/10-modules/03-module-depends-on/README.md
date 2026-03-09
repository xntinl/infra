# 43. Module depends_on

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 42 (Module with for_each)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `depends_on` on a module block to enforce execution order for hidden dependencies
- Distinguish between implicit dependencies (derived from references) and explicit dependencies (declared with `depends_on`)
- Visualize the dependency graph with `terraform graph`

## Why Explicit Dependencies Between Modules

Terraform automatically detects dependencies when one resource references an attribute of another. If module B consumes an output from module A, Terraform knows to create A first. These are **implicit dependencies**, and they cover most cases.

However, some dependencies are invisible in the code. For example, an application module might require a network to be fully provisioned before it can function, yet nothing in the application's inputs directly references the network resource. In these situations, `depends_on` lets you declare the relationship explicitly. Without it, Terraform would create both in parallel, potentially causing runtime failures.

Use `depends_on` sparingly -- it is a last resort for when you cannot restructure the code to create an implicit dependency through references. Overusing it can serialize your plan unnecessarily and slow down applies.

## Step 1 -- Create the Dependency Chain

This exercise reuses the `tagged-parameter` module from exercise 41. Create `main.tf`:

```hcl
# main.tf

resource "aws_ssm_parameter" "network_ready" {
  name  = "/kata/network-status"
  type  = "String"
  value = "ready"
}

module "database_config" {
  source      = "./modules/tagged-parameter"
  name        = "database/endpoint"
  value       = "db.internal"
  environment = "dev"

  depends_on = [aws_ssm_parameter.network_ready]
}

module "app_config" {
  source      = "./modules/tagged-parameter"
  name        = "app/db-endpoint"
  value       = module.database_config.name
  environment = "dev"
}
```

This creates a three-step chain:

1. `aws_ssm_parameter.network_ready` is created first (explicit dependency from `database_config`).
2. `module.database_config` is created second (it depends_on `network_ready`).
3. `module.app_config` is created third (implicit dependency -- it references `module.database_config.name`).

## Step 2 -- Plan and Visualize

```bash
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

```
Plan: 3 to add, 0 to change, 0 to destroy.
```

Terraform creates resources in the correct order: network_ready, then database_config, then app_config.

### Scenario B -- Removing depends_on

Remove `depends_on = [aws_ssm_parameter.network_ready]` from the `database_config` module:

```bash
terraform plan
```

Terraform no longer enforces ordering between `network_ready` and `database_config`. Both may be created in parallel. If `database_config` genuinely needs the network to exist first (e.g., for a side-effect like a VPC endpoint being available), this would cause a runtime failure.

### Scenario C -- Visualizing the dependency graph

```bash
terraform graph | dot -Tpng > graph.png
```

Open `graph.png` to see the directed acyclic graph (DAG). With `depends_on` present, there is an edge from `network_ready` to `database_config`. Without it, those two nodes are independent.

## Common Mistakes

### Using depends_on when a reference would suffice

If you can restructure your code so that module B references an output from module A, do that instead of adding `depends_on`. Implicit dependencies are self-documenting and Terraform handles them automatically. Reach for `depends_on` only when no attribute reference can express the relationship.

### Applying depends_on to the wrong resource

`depends_on` accepts a list of resources or modules. A common error is pointing it at an output or a local value, which produces a validation error. Make sure the target is a `resource` or `module` block.

## Verify What You Learned

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: 3 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '[.planned_values.root_module.resources[].values.name, .planned_values.root_module.child_modules[].resources[].values.name]'
```

Expected:

```json
[
  "/kata/network-status",
  "/dev/database/endpoint",
  "/dev/app/db-endpoint"
]
```

```bash
terraform graph | grep -c "\->"
```

Expected: a number greater than 0, confirming dependency edges exist in the graph.

## What's Next

In exercise 44 you will learn how to pin module versions from Git repositories and the Terraform Registry, ensuring reproducible builds across teams and CI/CD pipelines.

## Reference

- [depends_on Meta-Argument](https://developer.hashicorp.com/terraform/language/meta-arguments/depends_on)

## Additional Resources

- [Manage Resource Dependencies](https://developer.hashicorp.com/terraform/tutorials/configuration-language/dependencies) -- official tutorial on implicit and explicit dependencies with practical examples
- [Terraform depends_on Meta-Argument](https://spacelift.io/blog/terraform-depends-on) -- practical guide with depends_on on resources and modules, including when to avoid it
- [Command: graph](https://developer.hashicorp.com/terraform/cli/commands/graph) -- reference for generating and visualizing the dependency DAG
