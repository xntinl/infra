# 42. Module with for_each

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 41 (Create a Minimal Module)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `for_each` on a module block to create multiple instances from a map
- Access individual module instances by key using `module.name["key"]` syntax
- Build derived output maps and filtered lists using `for` expressions over module instances

## Why for_each on Modules

In the previous exercise you called the `tagged-parameter` module once for a single SSM parameter. In practice, you often need to create many instances of the same module -- one per microservice, one per database, one per team. Duplicating the module block for each instance is error-prone and hard to maintain.

The `for_each` meta-argument solves this by letting you drive module instantiation from a data structure (a map or set). Adding a new entry to the map creates a new instance; removing an entry destroys only that instance, leaving everything else untouched. Because instances are keyed by string (not by numeric index), Terraform can track them stably even when the collection changes.

## Step 1 -- Define the Parameter Map

This exercise reuses the `tagged-parameter` module from exercise 41. Create `main.tf`:

```hcl
# main.tf

variable "parameters" {
  default = {
    "db/host"     = { value = "db.internal", team = "platform" }
    "db/port"     = { value = "5432",        team = "platform" }
    "cache/host"  = { value = "redis.internal", team = "platform" }
    "api/key"     = { value = "abc123",      team = "backend" }
    "api/version" = { value = "v2",          team = "backend" }
  }
}
```

## Step 2 -- Create Module Instances with for_each

```hcl
# main.tf (continued)

module "params" {
  source   = "./modules/tagged-parameter"
  for_each = var.parameters

  name        = each.key
  value       = each.value.value
  environment = "dev"
  extra_tags  = { Team = each.value.team }
}
```

Each key in the map becomes a distinct module instance. Terraform tracks them as `module.params["db/host"]`, `module.params["api/key"]`, and so on.

## Step 3 -- Build Outputs

```hcl
# main.tf (continued)

output "db_host_arn" {
  value = module.params["db/host"].arn
}

output "all_arns" {
  value = { for k, v in module.params : k => v.arn }
}

output "platform_params" {
  value = [for k, v in var.parameters : k if v.team == "platform"]
}
```

## Step 4 -- Plan and Explore

```bash
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

```
Plan: 5 to add, 0 to change, 0 to destroy.

Changes to Outputs:
  + all_arns         = {
      + "api/key"     = (known after apply)
      + "api/version" = (known after apply)
      + "cache/host"  = (known after apply)
      + "db/host"     = (known after apply)
      + "db/port"     = (known after apply)
    }
  + db_host_arn      = (known after apply)
  + platform_params  = [
      + "cache/host",
      + "db/host",
      + "db/port",
    ]
```

### Scenario B -- Adding a new parameter

Add a sixth entry to the map:

```hcl
"cache/port" = { value = "6379", team = "platform" }
```

```bash
terraform plan
```

Only one new resource appears: `module.params["cache/port"]`. The existing five are unchanged.

### Scenario C -- Removing a parameter

Remove the `"api/version"` entry from the map:

```bash
terraform plan
```

Only `module.params["api/version"]` is marked for destruction. All other instances are untouched. This is the key advantage of `for_each` over `count` -- stable keys prevent cascading changes.

## Common Mistakes

### Using count instead of for_each for named resources

With `count`, instances are identified by numeric index. Removing an item from the middle of a list causes all subsequent items to shift indices, triggering unnecessary destroy-and-recreate operations. Always prefer `for_each` with a map when instances have meaningful identities.

### Referencing module.params without a key

Writing `module.params.arn` (without a key) produces an error because `module.params` is a map of instances, not a single instance. You must specify which instance: `module.params["db/host"].arn`, or iterate with a `for` expression.

## Verify What You Learned

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

```bash
terraform plan
```

Expected: `Plan: 5 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '[.planned_values.root_module.child_modules[].address] | sort'
```

Expected:

```json
[
  "module.params[\"api/key\"]",
  "module.params[\"api/version\"]",
  "module.params[\"cache/host\"]",
  "module.params[\"db/host\"]",
  "module.params[\"db/port\"]"
]
```

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '.planned_values.outputs.platform_params.value | sort'
```

Expected:

```json
[
  "cache/host",
  "db/host",
  "db/port"
]
```

## What's Next

In exercise 43 you will use `depends_on` with modules to declare hidden dependencies that Terraform cannot infer from resource references, controlling the order in which modules are created and destroyed.

## Reference

- [for_each with Modules](https://developer.hashicorp.com/terraform/language/meta-arguments/for_each)

## Additional Resources

- [Manage Similar Resources with For Each](https://developer.hashicorp.com/terraform/tutorials/configuration-language/for-each) -- official HashiCorp tutorial for mastering for_each with resources and modules
- [Terraform for_each Meta-Argument Guide](https://spacelift.io/blog/terraform-for-each) -- practical guide with for_each applied to modules, maps, and output filtering
- [Module Composition](https://developer.hashicorp.com/terraform/language/modules/develop/composition) -- patterns for composing multiple module instances and connecting their outputs
