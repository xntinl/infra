# 5. cidrsubnet() for Subnet Calculation

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed Section 01 (Variables and Types)
- Basic understanding of CIDR notation (e.g., what /16 and /24 mean)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `cidrsubnet()` to calculate subnet addresses from a base CIDR block by specifying `newbits` and `netnum`
- Use `cidrsubnets()` to generate multiple subnets of different sizes in a single call
- Implement dynamic subnet generation with `range()` and `for` expressions

## Why cidrsubnet()

When you create a VPC with a base CIDR like `10.0.0.0/16`, you need to divide it into subnets. Doing this manually means calculating IP ranges by hand, which is error-prone and does not scale. If you need 3 public subnets and 3 private subnets across availability zones, that is 6 CIDR ranges to calculate and maintain.

`cidrsubnet()` performs this calculation programmatically. You provide the base CIDR, how many bits to add to the subnet mask (`newbits`), and which subnet number you want (`netnum`). Terraform computes the resulting CIDR block. This means you can change the VPC CIDR or the number of availability zones and all subnet calculations update automatically.

Think of it as slicing a network address space the way you would partition a disk: you specify the size of each partition and which partition number you want, and the function returns the exact address range.

## Understanding newbits and netnum

The two key parameters of `cidrsubnet(prefix, newbits, netnum)`:

- **newbits**: How many bits to add to the prefix length. A /16 with newbits=8 produces /24 subnets. The higher the newbits, the smaller (more specific) the subnets.
- **netnum**: Which subnet to select from the available range. With newbits=8, there are 256 possible subnets (0-255).

| Base CIDR | newbits | Result prefix | Subnets available | Hosts per subnet |
|-----------|---------|--------------|-------------------|------------------|
| /16 | 8 | /24 | 256 | 254 |
| /16 | 4 | /20 | 16 | 4094 |
| /24 | 2 | /26 | 4 | 62 |

## Building the Configuration

This configuration creates public and private subnets separated by a `netnum` offset, generates mixed-size subnets, and subdivides a small /24 block.

Create `main.tf`:

```hcl
variable "vpc_cidr" {
  default = "10.0.0.0/16"
}

variable "az_count" {
  default = 3
}

locals {
  # Public subnets: /24 blocks at positions 0, 1, 2
  public_subnets = [
    for i in range(var.az_count) :
    cidrsubnet(var.vpc_cidr, 8, i)
  ]

  # Private subnets: /24 blocks at positions 100, 101, 102
  private_subnets = [
    for i in range(var.az_count) :
    cidrsubnet(var.vpc_cidr, 8, i + 100)
  ]

  # Mixed sizes: three /24 subnets and two /20 subnets
  mixed_subnets = cidrsubnets(var.vpc_cidr, 8, 8, 8, 4, 4)

  # Subdividing a small /24 into four /26 subnets
  small_subnets = [
    for i in range(4) :
    cidrsubnet("192.168.1.0/24", 2, i)
  ]
}

output "public_subnets"  { value = local.public_subnets }
output "private_subnets" { value = local.private_subnets }
output "mixed_subnets"   { value = local.mixed_subnets }
output "small_subnets"   { value = local.small_subnets }
```

## Exploring cidrsubnet() in Console

Before running `terraform plan`, explore the function interactively to build intuition.

```bash
terraform console
```

```
> cidrsubnet("10.0.0.0/16", 8, 0)
"10.0.0.0/24"
> cidrsubnet("10.0.0.0/16", 8, 1)
"10.0.1.0/24"
> cidrsubnet("10.0.0.0/16", 8, 100)
"10.0.100.0/24"
> cidrsubnet("10.0.0.0/16", 4, 1)
"10.0.16.0/20"
> cidrhost("10.0.0.0/24", 5)
"10.0.0.5"
> exit
```

Notice how `newbits=8` produces /24 subnets (256 addresses each) while `newbits=4` produces /20 subnets (4096 addresses each).

## Verifying the Outputs

```bash
terraform plan
```

```
Changes to Outputs:
  + mixed_subnets   = [
      + "10.0.0.0/24",
      + "10.0.1.0/24",
      + "10.0.2.0/24",
      + "10.0.16.0/20",
      + "10.0.32.0/20",
    ]
  + private_subnets = [
      + "10.0.100.0/24",
      + "10.0.101.0/24",
      + "10.0.102.0/24",
    ]
  + public_subnets  = [
      + "10.0.0.0/24",
      + "10.0.1.0/24",
      + "10.0.2.0/24",
    ]
  + small_subnets   = [
      + "192.168.1.0/26",
      + "192.168.1.64/26",
      + "192.168.1.128/26",
      + "192.168.1.192/26",
    ]
```

Public subnets use positions 0-2, private subnets use positions 100-102, ensuring no overlap. The mixed subnets demonstrate how `cidrsubnets()` allocates different-sized blocks contiguously.

## Common Mistakes

### Choosing newbits that exceed the available address space

With a /24 base, adding 9 newbits would require a /33 prefix, which exceeds the 32-bit IPv4 limit:

```hcl
# Wrong: 24 + 9 = 33, which is invalid
cidrsubnet("192.168.1.0/24", 9, 0)
```

```
Error: Invalid function argument: would extend prefix to 33 bits, which is not valid for IPv4.
```

Make sure `base prefix + newbits <= 32`.

### Overlapping subnets from netnum collisions

If you use the same netnum for both public and private subnets, they will overlap:

```hcl
# Wrong: both use netnum 0, producing the same CIDR
public  = cidrsubnet("10.0.0.0/16", 8, 0)  # 10.0.0.0/24
private = cidrsubnet("10.0.0.0/16", 8, 0)  # 10.0.0.0/24 -- same!
```

Use an offset (e.g., `i + 100` for private subnets) to guarantee non-overlapping ranges.

## Verify What You Learned

```bash
terraform console
```

```
> cidrsubnet("10.0.0.0/16", 8, 0)
"10.0.0.0/24"
> cidrsubnet("10.0.0.0/16", 8, 255)
"10.0.255.0/24"
> cidrsubnets("10.0.0.0/16", 8, 8, 4)
[
  "10.0.0.0/24",
  "10.0.1.0/24",
  "10.0.16.0/20",
]
> length(local.public_subnets)
3
> local.private_subnets[0]
"10.0.100.0/24"
> exit
```

## What's Next

You used `cidrsubnet()` and `cidrsubnets()` to calculate subnet ranges programmatically from a base CIDR. In the next exercise, you will learn how to use `flatten()` to unnest hierarchical data structures into flat lists suitable for `for_each` iteration.

## Reference

- [cidrsubnet Function](https://developer.hashicorp.com/terraform/language/functions/cidrsubnet)
- [cidrsubnets Function](https://developer.hashicorp.com/terraform/language/functions/cidrsubnets)
- [range Function](https://developer.hashicorp.com/terraform/language/functions/range)

## Additional Resources

- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial on Terraform functions including network functions like cidrsubnet
- [Terraform cidrsubnet Function](https://spacelift.io/blog/terraform-cidrsubnet) -- Practical guide on cidrsubnet() with visual explanations of newbits, netnum, and network partitioning
- [AWS VPC Subnet Sizing](https://docs.aws.amazon.com/vpc/latest/userguide/subnet-sizing.html) -- AWS reference on subnet sizing to understand CIDR ranges in a real-world context
