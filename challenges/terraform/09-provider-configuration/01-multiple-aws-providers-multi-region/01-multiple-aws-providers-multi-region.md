# 38. Multiple AWS Providers for Multi-Region

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 37 (Chained Local Transforms)

## Learning Objectives

After completing this exercise, you will be able to:

- Define multiple provider configurations using aliases to target different AWS regions
- Assign an aliased provider to a resource with the `provider` meta-argument
- Deploy the same logical resource across independent regional namespaces

## Why Provider Aliases

Most production architectures eventually outgrow a single region. Disaster recovery requires a standby in another geography, data-residency laws may force certain records into specific jurisdictions, and latency-sensitive workloads benefit from being close to users. Terraform addresses this with **provider aliases**: you declare the same provider more than once, give each additional declaration a unique alias, and then tell individual resources which configuration to use.

Without aliases, every AWS resource in a configuration targets the same region. With aliases, a single `terraform apply` can orchestrate resources across as many regions (or accounts) as you need -- all tracked in one state file. Understanding this mechanism is the foundation for the cross-account and module-passthrough patterns you will build in the next two exercises.

## Step 1 -- Define Three Provider Configurations

Create a file called `main.tf`:

```hcl
# main.tf

provider "aws" {
  region = "us-east-1"
}

provider "aws" {
  alias  = "west"
  region = "us-west-2"
}

provider "aws" {
  alias  = "eu"
  region = "eu-west-1"
}
```

The first provider block (without an alias) becomes the **default**. Any resource that does not specify a `provider` argument will use it automatically.

## Step 2 -- Create the Same Resource in Each Region

Add the following resources and outputs to `main.tf`:

```hcl
# main.tf (continued)

resource "aws_ssm_parameter" "east" {
  name  = "/kata/region"
  type  = "String"
  value = "us-east-1"
}

resource "aws_ssm_parameter" "west" {
  provider = aws.west
  name     = "/kata/region"
  type     = "String"
  value    = "us-west-2"
}

resource "aws_ssm_parameter" "eu" {
  provider = aws.eu
  name     = "/kata/region"
  type     = "String"
  value    = "eu-west-1"
}

output "east_arn" { value = aws_ssm_parameter.east.arn }
output "west_arn" { value = aws_ssm_parameter.west.arn }
output "eu_arn"   { value = aws_ssm_parameter.eu.arn }
```

All three parameters share the same name (`/kata/region`), yet they never conflict because each region is an independent namespace in AWS.

## Step 3 -- Run the Plan

```bash
terraform init
terraform plan
```

### Scenario A -- Successful plan (expected)

Terraform shows three SSM parameters to create, each in a different region:

```
Terraform will perform the following actions:

  # aws_ssm_parameter.east will be created
  + resource "aws_ssm_parameter" "east" {
      + name  = "/kata/region"
      + type  = "String"
      + value = "us-east-1"
      ...
    }

  # aws_ssm_parameter.eu will be created
  + resource "aws_ssm_parameter" "eu" {
      + name  = "/kata/region"
      + type  = "String"
      + value = "eu-west-1"
      ...
    }

  # aws_ssm_parameter.west will be created
  + resource "aws_ssm_parameter" "west" {
      + name  = "/kata/region"
      + type  = "String"
      + value = "us-west-2"
      ...
    }

Plan: 3 to add, 0 to change, 0 to destroy.
```

### Scenario B -- Missing provider argument

If you remove `provider = aws.west` from the `west` resource, Terraform will try to create both `east` and `west` in `us-east-1`. Because they share the same SSM name, the second will fail with a `ParameterAlreadyExists` error at apply time. This is exactly why the explicit `provider` argument matters.

## Common Mistakes

### Forgetting the provider argument on aliased resources

If you omit `provider = aws.west`, the resource silently falls back to the default provider. The plan will succeed, but the resource will be created in the wrong region. Always double-check that every resource intended for a non-default region carries an explicit `provider` reference.

### Confusing the alias name with the resource name

The alias (`west`) is an identifier for the provider configuration, not the resource. You reference it as `aws.west` in the `provider` argument, never as just `west`. Writing `provider = west` produces a syntax error.

## Verify What You Learned

```bash
terraform plan
```

Expected: `Plan: 3 to add, 0 to change, 0 to destroy.`

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq '.planned_values.root_module.resources[].values.name'
```

Expected (all three share the same name):

```
"/kata/region"
"/kata/region"
"/kata/region"
```

```bash
terraform plan -out=tf.plan && terraform show -json tf.plan | jq -r '.planned_values.root_module.resources[].provider_name'
```

Expected: three entries, all `registry.terraform.io/hashicorp/aws`.

```bash
terraform validate
```

Expected: `Success! The configuration is valid.`

## What's Next

In exercise 39 you will use the `assume_role` block inside a provider to deploy resources into a completely different AWS account, combining aliases with cross-account access via STS.

## Reference

- [Provider Configuration](https://developer.hashicorp.com/terraform/language/providers/configuration)

## Additional Resources

- [Spacelift: How to Use Terraform Provider Alias](https://spacelift.io/blog/terraform-provider-alias) -- practical guide with multi-region and multi-account examples
- [Deploy Multi-Region Resources with Terraform](https://cloud-cod.com/index.php/2025/08/05/deploying-aws-resources-in-multiple-regions-using-terraform-provider-aliases/) -- walkthrough of provider aliases for multi-region deployments
- [env0: Terraform Provider Configuration](https://www.env0.com/blog/terraform-providers) -- article covering provider setup including aliases and multi-region patterns
