# 7. templatefile() for User Data

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercise 6 (flatten() for Nested Lists)

## Learning Objectives

After completing this exercise, you will be able to:

- Use `templatefile()` to render dynamic scripts from `.tftpl` template files
- Implement template directives (`${var}`, `%{for}`, `%{if}`) for interpolation, loops, and conditionals
- Use `path.module` to reference template files relative to the module directory

## Why templatefile()

EC2 instances often need a user data script that installs packages, writes configuration files, and sets environment variables. Hardcoding these scripts in HCL strings is brittle: they become unreadable, hard to test, and impossible to syntax-check with shell linters.

`templatefile()` separates the script structure from the values it uses. You write the script as a standalone `.tftpl` file with placeholders, and Terraform renders it at plan time by substituting variables and evaluating control directives. The template file can be linted as a shell script (ignoring the template markers), version-controlled independently, and reused across modules with different input values.

This separation of template and data follows the same principle as configuration templating systems in other contexts: the template defines the structure, and the caller provides the values.

## Creating the Template File

The `.tftpl` extension is a convention that signals this file contains Terraform template syntax. The template uses `${var}` for interpolation, `%{for}` for loops, and the `~` suffix to trim trailing whitespace.

Create `scripts/setup.tftpl`:

```bash
#!/bin/bash
set -euo pipefail

echo "Configuring ${app_name} in ${environment}..."

%{ for pkg in packages ~}
yum install -y ${pkg}
%{ endfor ~}

cat > /etc/${app_name}/config.json <<'APPCONFIG'
{
  "port": ${port},
  "log_level": "${log_level}",
  "features": {
%{ for i, feature in features ~}
    "${feature}": true${ i < length(features) - 1 ? "," : "" }
%{ endfor ~}
  }
}
APPCONFIG

%{ for key, value in env_vars ~}
echo 'export ${key}="${value}"' >> /etc/environment
%{ endfor ~}

echo "Setup complete for ${app_name}"
```

## Rendering the Template

`templatefile()` takes the file path and a map of variables. Every variable referenced in the template must be provided -- missing variables cause an error at plan time.

Create `main.tf`:

```hcl
locals {
  user_data = templatefile("${path.module}/scripts/setup.tftpl", {
    app_name    = "myservice"
    environment = "production"
    packages    = ["docker", "jq", "aws-cli"]
    port        = 8080
    log_level   = "info"
    features    = ["auth", "caching", "metrics"]
    env_vars = {
      APP_ENV    = "production"
      AWS_REGION = "us-east-1"
      LOG_FORMAT = "json"
    }
  })
}

output "rendered_script" {
  value = local.user_data
}
```

## Inspecting the Rendered Output

After running `terraform plan`, inspect the output to confirm the template rendered correctly. You should see three `yum install` lines, a valid JSON config block, and three `export` lines.

```bash
terraform plan
```

```
Changes to Outputs:
  + rendered_script = <<-EOT
        #!/bin/bash
        set -euo pipefail

        echo "Configuring myservice in production..."

        yum install -y docker
        yum install -y jq
        yum install -y aws-cli

        cat > /etc/myservice/config.json <<'APPCONFIG'
        {
          "port": 8080,
          "log_level": "info",
          "features": {
            "auth": true,
            "caching": true,
            "metrics": true
          }
        }
        APPCONFIG

        echo 'export APP_ENV="production"' >> /etc/environment
        echo 'export AWS_REGION="us-east-1"' >> /etc/environment
        echo 'export LOG_FORMAT="json"' >> /etc/environment

        echo "Setup complete for myservice"
    EOT
```

No template syntax remains in the output -- all `${...}` and `%{...}` directives have been evaluated.

## Template Syntax Reference

| Syntax | Purpose | Example |
|--------|---------|---------|
| `${var}` | Variable interpolation | `${app_name}` |
| `%{for x in list}` | Loop over a list | `%{for pkg in packages}` |
| `%{for k, v in map}` | Loop over a map | `%{for key, value in env_vars}` |
| `%{if condition}` | Conditional block | `%{if enable_ssl}` |
| `~}` | Trim trailing whitespace | `%{endfor ~}` |

## Common Mistakes

### Missing a variable in the templatefile() call

Every variable used in the template must be provided in the second argument. If you add `${new_var}` to the template but forget to pass it:

```
Error: Invalid value for "vars" parameter: vars map does not contain key
"new_var", referenced at scripts/setup.tftpl:5,12-19.
```

Always keep the template variables and the `templatefile()` call in sync.

### Using Terraform expressions inside the template

Template files use template syntax, not HCL syntax. You cannot use `var.something` or `local.something` inside a `.tftpl` file. All values must be passed through the variables map:

```bash
# Wrong: Terraform references do not work in templates
echo "${var.environment}"

# Correct: use the variable name from the map
echo "${environment}"
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
  + rendered_script = <<-EOT
        #!/bin/bash
        ...
        yum install -y docker
        yum install -y jq
        yum install -y aws-cli
        ...
    EOT
```

```bash
terraform console
```

```
> length(local.user_data) > 0
true
> strcontains(local.user_data, "yum install -y docker")
true
> strcontains(local.user_data, "${")
false
> exit
```

## What's Next

You used `templatefile()` to render dynamic scripts from template files with interpolation and control directives. In the next exercise, you will learn how to use `merge()` and `lookup()` to implement layered configuration patterns where environment-specific values override sensible defaults.

## Reference

- [templatefile Function](https://developer.hashicorp.com/terraform/language/functions/templatefile)
- [String Templates](https://developer.hashicorp.com/terraform/language/expressions/strings#string-templates)
- [Filesystem and Workspace Info](https://developer.hashicorp.com/terraform/language/expressions/references#filesystem-and-workspace-info)

## Additional Resources

- [Perform Dynamic Operations with Functions](https://developer.hashicorp.com/terraform/tutorials/configuration-language/functions) -- Official HashiCorp tutorial covering Terraform functions including templatefile() for dynamic content generation
- [Terraform Templates](https://spacelift.io/blog/terraform-templates) -- Comprehensive guide on templatefile(), .tftpl syntax, for loops, and conditionals in templates
- [Create EC2 User Data Scripts](https://developer.hashicorp.com/terraform/tutorials/provision/cloud-init) -- Practical HashiCorp tutorial on user data scripts with cloud-init and templatefile
