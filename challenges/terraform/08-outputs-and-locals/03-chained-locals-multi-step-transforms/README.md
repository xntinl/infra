# 37. Chained Locals for Multi-Step Data Transformations

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 36 (Locals for Derived Values)

## Learning Objectives

After completing this exercise, you will be able to:

- Chain locals together so each step builds on the previous one, creating a data transformation pipeline
- Use `distinct()`, `sum()`, `replace()`, and `merge()` to enrich, aggregate, and normalize data
- Group and filter collections with `for` expressions and `if` clauses

## Why Chained Locals

Real-world configurations often need to transform raw input data through multiple steps before it is ready for use in resources. A flat map of services might need to be enriched with computed fields (normalized names, size classifications), grouped by team, filtered by visibility, and aggregated into summary metrics. Trying to do all of this in a single expression produces unreadable, untestable code.

Chained locals decompose complex transformations into named intermediate steps. Each local reads from the previous one, applies one transformation, and produces a clearly named result. This is analogous to a functional programming pipeline: raw data flows through enrichment, grouping, filtering, and aggregation stages, with each stage independently understandable and verifiable.

The key insight is that Terraform resolves local references automatically regardless of declaration order. You can define `local.enriched_services` before or after `local.services` -- Terraform builds the dependency graph and evaluates them in the correct order. This means you can organize your locals by logical step rather than worrying about evaluation order.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

variable "raw_config" {
  default = {
    services = {
      "user-api"    = { team = "identity", memory = 512,  public = true }
      "order-api"   = { team = "commerce", memory = 1024, public = true }
      "payment-svc" = { team = "commerce", memory = 2048, public = false }
      "email-svc"   = { team = "comms",    memory = 256,  public = false }
      "audit-log"   = { team = "security", memory = 512,  public = false }
    }
  }
}

locals {
  # Step 1: Extract the raw services map
  services = var.raw_config.services

  # Step 2: Enrich each service with derived fields
  enriched_services = {
    for name, svc in local.services : name => merge(svc, {
      name_normalized = replace(name, "-", "_")
      memory_gb       = svc.memory / 1024
      size_class      = svc.memory >= 1024 ? "large" : "small"
    })
  }

  # Step 3: Group services by team
  teams = distinct([for name, svc in local.services : svc.team])
  services_by_team = {
    for team in local.teams : team => [
      for name, svc in local.services : name if svc.team == team
    ]
  }

  # Step 4: Filter into subsets
  public_services  = { for k, v in local.enriched_services : k => v if v.public }
  private_services = { for k, v in local.enriched_services : k => v if !v.public }
  large_services   = { for k, v in local.enriched_services : k => v if v.size_class == "large" }

  # Step 5: Aggregate metrics
  total_memory = sum([for name, svc in local.services : svc.memory])
  team_memory = {
    for team in local.teams : team => sum([
      for name, svc in local.services : svc.memory if svc.team == team
    ])
  }
}

output "enriched_services"  { value = local.enriched_services }
output "services_by_team"   { value = local.services_by_team }
output "public_services"    { value = keys(local.public_services) }
output "private_services"   { value = keys(local.private_services) }
output "large_services"     { value = keys(local.large_services) }
output "total_memory_mb"    { value = local.total_memory }
output "team_memory"        { value = local.team_memory }
```

## Step 2 -- Trace the Pipeline

Each step depends on the previous one:

1. **Extract** (`local.services`): Pulls the services map out of the nested input variable for cleaner references downstream.
2. **Enrich** (`local.enriched_services`): Adds computed fields using `merge()` -- `name_normalized` converts hyphens to underscores, `memory_gb` converts MB to GB, and `size_class` categorizes by memory threshold.
3. **Group** (`local.services_by_team`): Uses `distinct()` to find unique team names, then builds a map where each team key points to a list of service names.
4. **Filter** (`local.public_services`, `local.private_services`, `local.large_services`): Creates subsets from the enriched data using `if` clauses.
5. **Aggregate** (`local.total_memory`, `local.team_memory`): Uses `sum()` to compute totals across all services and per-team breakdowns.

## Step 3 -- Initialize and Plan

```bash
terraform init
terraform plan
```

No resources are created -- this exercise is purely about data transformation. All results appear in the outputs.

## Step 4 -- Explore the Outputs

```bash
terraform apply -auto-approve

# View enriched data for a specific service
terraform output -json enriched_services | jq '.["user-api"]'

# View team groupings
terraform output -json services_by_team | jq .

# View which services are public vs private
terraform output -json public_services
terraform output -json private_services

# View memory aggregations
terraform output total_memory_mb
terraform output -json team_memory
```

## Step 5 -- Modify and Observe

Add a new service to the `raw_config` variable:

```hcl
"notification-svc" = { team = "comms", memory = 512, public = true }
```

Run `terraform plan` again and observe how every derived value updates automatically: the new service appears in `enriched_services`, the `comms` team gains a member in `services_by_team`, `total_memory` increases, and `public_services` gains an entry.

## Common Mistakes

### Trying to do everything in one expression

Writing a single massive `for` expression that enriches, filters, and aggregates simultaneously is technically possible but produces unreadable code. Break complex transformations into named steps using chained locals.

### Confusing map `for` and list `for` syntax

- Curly braces produce a map: `{ for k, v in map : k => v }`
- Square brackets produce a list: `[ for k, v in map : v ]`

Using the wrong brackets changes the output type and can cause type errors downstream.

## Verify What You Learned

Run the following commands and confirm the exact output:

```bash
terraform output -json enriched_services | jq '.["user-api"].size_class'
```

Expected: `"small"`

```bash
terraform output -json enriched_services | jq '.["order-api"].memory_gb'
```

Expected: `1`

```bash
terraform output -json services_by_team | jq '.commerce'
```

Expected: `["order-api", "payment-svc"]`

```bash
terraform output -json public_services
```

Expected: `["order-api", "user-api"]`

```bash
terraform output total_memory_mb
```

Expected: `4352`

```bash
terraform output -json team_memory | jq '.commerce'
```

Expected: `3072`

## Section 08 Summary: Outputs and Locals

Across exercises 35-37, you learned how to use outputs and locals to organize, derive, and transform data in Terraform:

| Exercise | Concept | What You Learned |
|----------|---------|-----------------|
| 35 | Structured Outputs | Building nested map outputs, filtering with `for`/`if`, `description` for documentation |
| 36 | Locals for Derived Values | Computing values once (name prefixes, tags, conditionals), referencing throughout |
| 37 | Chained Locals | Multi-step data pipelines using `merge()`, `distinct()`, `sum()`, grouping and filtering |

**Key takeaways:**

- Structured outputs make modules predictable and scalable by grouping related data into nested maps instead of flat individual outputs.
- Locals eliminate duplication and centralize business logic. When a naming convention or environment rule changes, you update one local instead of every resource.
- Chained locals decompose complex transformations into named, readable steps. Each step builds on the previous one, creating a traceable data pipeline.
- `for` expressions with `if` clauses are the primary tool for filtering and transforming collections in Terraform.

## What's Next

In section 09 (Provider Configuration), you will learn how to configure multiple providers, use provider aliases for multi-region deployments, and manage provider versions and constraints.

## Reference

- [Terraform Local Values](https://developer.hashicorp.com/terraform/language/values/locals)
- [Terraform For Expressions](https://developer.hashicorp.com/terraform/language/expressions/for)

## Additional Resources

- [Simplify Terraform Configuration with Locals (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/locals) -- tutorial covering chained locals and multi-step transformations
- [Spacelift: Terraform Locals](https://spacelift.io/blog/terraform-locals) -- comprehensive guide covering chained locals, for expressions with filtering, and data grouping
- [Mastering Terraform Local Variables (ckdbtech)](https://ckdbtech.com/mastering-terraform-local-variables/) -- advanced tutorial with examples of `distinct()`, `sum()`, and transformation pipelines
- [Terraform For Expressions (Official Docs)](https://developer.hashicorp.com/terraform/language/expressions/for) -- reference for for expressions including filtering, grouping, and nested maps
