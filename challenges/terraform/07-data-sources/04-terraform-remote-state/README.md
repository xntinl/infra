# 33. Cross-Project State Sharing with terraform_remote_state

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 32 (Account-Aware ARNs with Context Data Sources)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `data "terraform_remote_state"` to read outputs from another Terraform project
- Design layered infrastructure with clear contracts between independent state files
- Understand the security properties of output-only access across state boundaries

## Why Cross-Project State Sharing

Real-world infrastructure is rarely a single Terraform configuration. Teams decompose infrastructure into layers -- networking, compute, databases, applications -- each managed independently with its own state file. This separation provides blast radius control (a bad apply to the app layer cannot destroy the VPC), independent lifecycles (the network changes rarely while the app deploys frequently), and team boundaries (the platform team owns networking, the product team owns the app).

But layers need to communicate. The app layer needs to know which VPC ID and subnet IDs the network layer created. The `terraform_remote_state` data source solves this by reading the outputs of another Terraform state file. Crucially, it can only access values explicitly declared as `output` in the source project -- internal resources and sensitive data remain hidden unless the source project chooses to expose them.

This exercise demonstrates the pattern using a local backend. In production, you would use an S3 backend, but the mechanism is identical.

## Step 1 -- Create the Network Project

Create a directory structure with two projects:

```
exercise-33/
  network/
    main.tf
  app/
    main.tf
```

Create `network/main.tf`:

```hcl
# network/main.tf

resource "aws_ssm_parameter" "vpc_id" {
  name  = "/kata/network/vpc-id"
  type  = "String"
  value = "vpc-fake-12345"
}

output "vpc_id"          { value = "vpc-fake-12345" }
output "private_subnets" { value = ["subnet-a", "subnet-b", "subnet-c"] }
output "public_subnets"  { value = ["subnet-d", "subnet-e", "subnet-f"] }
```

## Step 2 -- Apply the Network Project

The network project must be applied first so its state file exists for the app project to read:

```bash
cd network && terraform init && terraform apply -auto-approve
```

This creates `network/terraform.tfstate` with the outputs.

## Step 3 -- Create the App Project

Create `app/main.tf`:

```hcl
# app/main.tf

data "terraform_remote_state" "network" {
  backend = "local"

  config = {
    path = "${path.module}/../network/terraform.tfstate"
  }
}

locals {
  vpc_id          = data.terraform_remote_state.network.outputs.vpc_id
  private_subnets = data.terraform_remote_state.network.outputs.private_subnets
}

output "from_network_vpc_id"  { value = local.vpc_id }
output "from_network_subnets" { value = local.private_subnets }
```

## Step 4 -- Plan the App Project

```bash
cd app && terraform init && terraform plan
```

The plan should show outputs sourced from the network state file without creating any resources.

## Step 5 -- Test the Failure Case

What happens if the network state file does not exist? Delete the state and try again:

```bash
rm ../network/terraform.tfstate
cd app && terraform plan
```

Expected error:

```
Error: Unable to find remote state
```

This confirms that `terraform_remote_state` requires the source state to exist. In production, this means layers must be applied in dependency order.

Restore the state before continuing:

```bash
cd ../network && terraform apply -auto-approve
```

## Common Mistakes

### Trying to access resources, not outputs

`terraform_remote_state` can only read `output` values from the source project. You cannot access `aws_ssm_parameter.vpc_id.value` from the network state -- only values explicitly declared as `output` are available. If you need a value from another project, the source project must expose it as an output.

### Using relative paths that break in CI/CD

The `path` in the `config` block uses `${path.module}/../network/terraform.tfstate`. This works locally but breaks in CI/CD where the working directory may differ. In production, use an S3 backend with bucket and key to make the reference location-independent.

## Verify What You Learned

Run the following commands from the `app/` directory:

```bash
terraform output from_network_vpc_id
```

Expected: `"vpc-fake-12345"`

```bash
terraform output from_network_subnets
```

Expected: `["subnet-a", "subnet-b", "subnet-c"]`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

```bash
# Verify only output values are accessible, not internal resources
terraform console <<< 'data.terraform_remote_state.network.outputs'
```

Expected: a map containing only `vpc_id`, `private_subnets`, and `public_subnets`

## What's Next

In the next exercise, you will use the `external` data source to execute a shell script and consume its JSON output in Terraform, extending Terraform's capabilities beyond what native data sources provide.

## Reference

- [Terraform terraform_remote_state Data Source](https://developer.hashicorp.com/terraform/language/state/remote-state-data)

## Additional Resources

- [Manage Resources in Other State Files (HashiCorp Tutorial)](https://developer.hashicorp.com/terraform/tutorials/state/state-import) -- tutorial on state management and sharing data between configurations
- [Spacelift: How to Use Terraform Remote State](https://spacelift.io/blog/terraform-remote-state) -- practical guide covering remote state with S3 and local backend examples
- [env0: Terraform Remote State for Beginners](https://www.env0.com/blog/terraform-remote-state) -- step-by-step walkthrough of configuring and consuming remote state between projects
