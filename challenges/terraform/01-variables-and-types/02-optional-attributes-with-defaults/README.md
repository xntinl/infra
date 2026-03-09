# 2. Optional Attributes with Defaults

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 1 (Map of Objects with for_each)

## Learning Objectives

After completing this exercise, you will be able to:

- Use the `optional()` modifier to make object attributes non-required
- Implement default values in `optional(type, default)` so Terraform fills in omitted fields automatically
- Distinguish between required and optional attributes when designing module interfaces

## Why Optional Attributes with Defaults

When you define a `map(object({...}))` variable, every attribute in the object is required by default. If a caller omits any field, Terraform rejects the input at plan time. This forces callers to specify every attribute even when most entries share the same values, leading to repetitive configuration.

The `optional()` modifier changes this contract. Marking an attribute as `optional(string, "tcp")` tells Terraform two things: the caller may omit this field, and if they do, Terraform will substitute `"tcp"` as the value. This is similar to function parameters with default arguments in programming languages -- you define sensible defaults once, and callers only specify what differs from the baseline.

This pattern is especially powerful for module interfaces where you want to keep simple use cases simple while allowing full customization for advanced cases.

## Defining Optional Attributes

The `optional()` modifier wraps the type and optionally provides a default value. Only the `port` attribute is required here; everything else has a sensible default.

Create `variables.tf`:

```hcl
variable "services" {
  type = map(object({
    port        = number
    protocol    = optional(string, "tcp")
    health_path = optional(string, "/health")
    replicas    = optional(number, 1)
    public      = optional(bool, false)
  }))
}
```

## Providing Partial Values

Each service only specifies the attributes that differ from the defaults. The `worker` service provides nothing beyond the required `port`, while `api` overrides `replicas` and `public`, and `grpc` overrides `protocol` and `health_path`.

Create `terraform.tfvars`:

```hcl
services = {
  api = {
    port     = 8080
    replicas = 3
    public   = true
  }
  worker = {
    port = 9090
  }
  grpc = {
    port        = 50051
    protocol    = "http2"
    health_path = "/grpc.health.v1.Health/Check"
  }
}
```

## Inspecting Resolved Values

The output shows the fully resolved objects with defaults filled in.

Create `outputs.tf`:

```hcl
output "resolved_services" {
  value = var.services
}
```

## Verifying Default Resolution

You can confirm that Terraform fills in the defaults by inspecting individual fields in the console.

```bash
terraform console
```

```
> var.services["worker"].protocol
"tcp"
> var.services["worker"].replicas
1
> var.services["api"].health_path
"/health"
> var.services["grpc"].protocol
"http2"
> exit
```

The `worker` service gets `"tcp"` for protocol and `1` for replicas from the defaults. The `api` service gets `"/health"` for health_path. The `grpc` service has `"http2"` because it explicitly overrode the default.

## What Happens Without optional()

If you remove `optional()` and define the variable as a plain object type, providing partial values triggers an error:

```hcl
# Without optional(): this definition requires ALL fields
variable "services_strict" {
  type = map(object({
    port        = number
    protocol    = string
    health_path = string
    replicas    = number
    public      = bool
  }))
}
```

```
Error: Invalid value for input variable

The given value is not suitable for var.services_strict: element "worker":
attribute "protocol" is required.
```

This is exactly the problem `optional()` solves.

## Common Mistakes

### Omitting the default value in optional()

If you write `optional(string)` without a second argument, omitted attributes become `null` instead of a useful value:

```hcl
# The protocol will be null, not "tcp"
protocol = optional(string)
```

Downstream code that expects a non-null string will fail. Always provide a default when the attribute should have a fallback value: `optional(string, "tcp")`.

### Assuming optional() works outside object types

The `optional()` modifier is only valid inside `object({...})` type definitions. It cannot be used on top-level variable types:

```hcl
# Wrong: optional() is not valid here
variable "region" {
  type = optional(string, "us-east-1")
}
```

For top-level variables, use the `default` argument on the variable block itself:

```hcl
variable "region" {
  type    = string
  default = "us-east-1"
}
```

## Verify What You Learned

```bash
terraform init
```

```
Terraform has been successfully initialized!
```

```bash
terraform plan
```

```
Changes to Outputs:
  + resolved_services = {
      + api    = {
          + health_path = "/health"
          + port        = 8080
          + protocol    = "tcp"
          + public      = true
          + replicas    = 3
        }
      + grpc   = {
          + health_path = "/grpc.health.v1.Health/Check"
          + port        = 50051
          + protocol    = "http2"
          + public      = false
          + replicas    = 1
        }
      + worker = {
          + health_path = "/health"
          + port        = 9090
          + protocol    = "tcp"
          + public      = false
          + replicas    = 1
        }
    }
```

```bash
terraform console
```

```
> var.services["worker"].protocol
"tcp"
> var.services["worker"].public
false
> var.services["grpc"].replicas
1
> exit
```

## What's Next

You learned how `optional()` with defaults simplifies module interfaces by letting callers specify only what differs from the baseline. In the next exercise, you will add validation blocks to variables so Terraform catches invalid inputs at plan time with clear, custom error messages.

## Reference

- [Type Constraints - optional](https://developer.hashicorp.com/terraform/language/expressions/type-constraints#optional-object-type-attributes)
- [Type Constraints - Structural Types](https://developer.hashicorp.com/terraform/language/expressions/type-constraints#structural-types)

## Additional Resources

- [Terraform Variables Tutorial](https://developer.hashicorp.com/terraform/tutorials/configuration-language/variables) -- Official tutorial covering variables, complex types, and type constraints step by step
- [Terraform Optional Variable](https://spacelift.io/blog/terraform-optional-variable) -- Detailed guide on the optional() modifier with practical examples of default values in objects
- [Type Constraints](https://developer.hashicorp.com/terraform/language/expressions/type-constraints) -- Reference documentation on type constraints including optional() with default values
