# 35. Structured Outputs

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 34 (Extending Terraform with the External Data Source)

## Learning Objectives

After completing this exercise, you will be able to:

- Build nested output maps that group multiple resource attributes into a single structured value
- Use `for` expressions with `if` clauses to create filtered subsets of data
- Add `description` to outputs for self-documenting module interfaces

## Why Structured Outputs

When a Terraform module creates multiple related resources, exposing each attribute as a separate output quickly becomes unmanageable. A module managing three services might need outputs for each service's port, path, endpoint, SSM parameter name, and ARN -- that is fifteen individual outputs for just three services.

Structured outputs solve this by grouping related data into nested maps. Instead of `api_port`, `api_path`, `api_endpoint`, `web_port`, `web_path`, `web_endpoint`, and so on, you expose a single `service_details` output where consumers access `service_details["api"].port`. This approach scales cleanly, makes the module interface predictable, and keeps `terraform output` readable.

Beyond grouping, `for` expressions with `if` clauses let you create filtered views of the same data -- services that use standard ports, services that are public-facing, or a flat list of all endpoints. These derived outputs make the module immediately useful to consumers without forcing them to filter the data themselves.

## Step 1 -- Create the Configuration

Create a file named `main.tf`:

```hcl
# main.tf

variable "services" {
  default = {
    api    = { port = 8080, path = "/api" }
    web    = { port = 3000, path = "/" }
    worker = { port = 9090, path = "/metrics" }
  }
}

resource "aws_ssm_parameter" "endpoints" {
  for_each = var.services
  name     = "/kata/services/${each.key}/endpoint"
  type     = "String"
  value    = "http://localhost:${each.value.port}${each.value.path}"
}

output "service_details" {
  description = "Complete service configuration including computed endpoints and SSM metadata"
  value = {
    for name, config in var.services : name => {
      port      = config.port
      path      = config.path
      endpoint  = "http://localhost:${config.port}${config.path}"
      ssm_param = aws_ssm_parameter.endpoints[name].name
      ssm_arn   = aws_ssm_parameter.endpoints[name].arn
    }
  }
}

output "standard_port_services" {
  description = "Services using ports below 9000"
  value = {
    for name, config in var.services : name => config
    if config.port < 9000
  }
}

output "all_endpoints" {
  description = "Flat list of all service endpoint URLs"
  value = [
    for name, config in var.services :
    "http://localhost:${config.port}${config.path}"
  ]
}
```

## Step 2 -- Understand the Three Output Patterns

**Nested map output (`service_details`)**: Uses a `for` expression to iterate over the services map and produce a new map where each key maps to an object with multiple fields. This is the richest output pattern -- consumers get everything they need in one place.

**Filtered map output (`standard_port_services`)**: Adds an `if` clause to the `for` expression to include only services matching a condition. The result has the same structure as the input but with a subset of entries.

**Flat list output (`all_endpoints`)**: Uses a `for` expression that produces a list (square brackets) instead of a map (curly braces). Useful when consumers only need one specific value from each item.

## Step 3 -- Initialize and Apply

```bash
terraform init
terraform apply -auto-approve
```

## Step 4 -- Explore the Outputs

```bash
# View the full structured output
terraform output -json service_details | jq .

# Access a specific service's details
terraform output -json service_details | jq '.api'

# View only the filtered subset
terraform output -json standard_port_services | jq 'keys'

# View the flat endpoint list
terraform output -json all_endpoints
```

## Common Mistakes

### Using individual outputs instead of structured maps

Creating separate outputs like `api_port`, `api_path`, `web_port`, `web_path` does not scale. When you add a new service, you must add new outputs. With a structured map, the output automatically includes any new services added to the input variable.

### Forgetting `description` on module outputs

Outputs without descriptions are opaque to consumers. When someone runs `terraform output` or reads generated documentation, they see the output name but have no idea what it contains or what format to expect. Always include `description` on outputs that other modules or teams will consume.

## Verify What You Learned

Run the following commands and confirm the output matches the expected patterns:

```bash
terraform output -json service_details | jq '.api.port'
```

Expected: `8080`

```bash
terraform output -json service_details | jq '.api.endpoint'
```

Expected: `"http://localhost:8080/api"`

```bash
terraform output -json standard_port_services | jq 'keys'
```

Expected: `["api", "web"]` (worker is excluded because its port 9090 >= 9000)

```bash
terraform output -json all_endpoints | jq 'length'
```

Expected: `3`

```bash
terraform output -json all_endpoints
```

Expected: `["http://localhost:3000/", "http://localhost:8080/api", "http://localhost:9090/metrics"]`

## What's Next

In the next exercise, you will use `locals` to derive values from input variables -- building name prefixes, conditional configurations, and common tags that keep your resource definitions clean and DRY.

## Reference

- [Terraform Output Values](https://developer.hashicorp.com/terraform/language/values/outputs)

## Additional Resources

- [Output Data from Terraform (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/configuration-language/outputs) -- hands-on tutorial covering output declarations including complex structures
- [Spacelift: Terraform Output Values](https://spacelift.io/blog/terraform-output) -- practical guide to outputs with examples of nested maps, descriptions, and sensitive values
- [AWS Prescriptive Guidance: Variables, Locals and Outputs](https://docs.aws.amazon.com/prescriptive-guidance/latest/getting-started-terraform/variables-locals-outputs.html) -- AWS best practices for organizing outputs in Terraform projects
