# Module Source Restrictions: Controlling Where Your Code Comes From

## Prerequisites

- OPA CLI installed (`opa version`)
- Completed exercise 04-04 (cost-controls)

## Learning Objectives

After completing this exercise, you will be able to:

- Navigate the `configuration.root_module.module_calls` section of the Terraform plan JSON
- Parse and classify module sources (registry, GitHub, local paths) using string builtins
- Build environment-specific rules that treat the same source differently based on context
- Distinguish between blocking violations (deny) and informational notices (warnings)

## Why This Matters

A Terraform module from an unverified repository can work perfectly today and tomorrow include a change that exfiltrates your AWS credentials. It is a real supply-chain attack vector -- the infrastructure equivalent of a compromised npm or PyPI package.

This is not hypothetical. Supply-chain attacks are real and increasingly common. Terraform modules are a perfect vector: they have access to your providers, your credentials, and your infrastructure.

The solution is to control **where** your modules come from. Only allow trusted sources: your internal registry, your organization on GitHub, and nothing else. To do that, you need to look at a part of the plan JSON you have not touched yet: the `configuration` section.

---

## The `configuration` Section of the Plan JSON

So far you have worked with `resource_changes`, which describes what resources change. But `configuration` describes the **parsed source code** -- the modules, providers, and variables as written in your `.tf` files.

```
+-----------------------------------------------------+
|                   tfplan.json                        |
+-----------------------------------------------------+
|                                                      |
|  resource_changes ----> what will change              |
|                                                      |
|  configuration -------> how the code is written       |
|    +-- root_module                                    |
|        +-- module_calls --> which modules are used    |
|            +-- "vpc"                                  |
|            |    +-- source: "registry.terraform.io/." |
|            +-- "database"                             |
|            |    +-- source: "git::https://github..."  |
|            +-- "utils"                                |
|                 +-- source: "./modules/utils"         |
|                                                      |
+-----------------------------------------------------+
```

Every module you use with `module "name" { source = "..." }` appears in `configuration.root_module.module_calls` as an entry with its `source` and `version_constraint` (if applicable).

---

## The Data

Create `tfplan.json` with a `configuration` section that includes several types of module sources:

```json
{
  "format_version": "1.2",
  "terraform_version": "1.7.0",
  "variables": {
    "environment": {
      "value": "prod"
    }
  },
  "resource_changes": [],
  "configuration": {
    "provider_config": {
      "aws": {
        "name": "aws",
        "expressions": {
          "region": {
            "constant_value": "us-east-1"
          }
        }
      }
    },
    "root_module": {
      "module_calls": {
        "vpc": {
          "source": "registry.terraform.io/terraform-aws-modules/vpc/aws",
          "version_constraint": "5.1.0",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "eks": {
          "source": "registry.terraform.io/terraform-aws-modules/eks/aws",
          "version_constraint": "~> 19.0",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "custom_network": {
          "source": "git::https://github.com/myorg/terraform-modules.git//network?ref=v2.1.0",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "monitoring": {
          "source": "git::https://github.com/myorg/terraform-modules.git//monitoring?ref=v1.5.0",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "sketchy_module": {
          "source": "git::https://github.com/random-user/cool-terraform-stuff.git//lambda",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "local_utils": {
          "source": "./modules/utils",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "another_local": {
          "source": "../shared/common",
          "module": {
            "outputs": {},
            "resources": []
          }
        },
        "bitbucket_module": {
          "source": "git::https://bitbucket.org/someteam/infra-modules.git//storage",
          "module": {
            "outputs": {},
            "resources": []
          }
        }
      }
    }
  }
}
```

There are eight modules with different sources:

- `vpc` and `eks` -- from the public Terraform registry (trusted).
- `custom_network` and `monitoring` -- from your organization's GitHub `myorg` (trusted).
- `sketchy_module` -- from a random user on GitHub (not trusted).
- `local_utils` and `another_local` -- local modules with relative paths (not allowed in prod).
- `bitbucket_module` -- from Bitbucket, a git host not on the allowlist.

---

## The Policy

Create `policy.rego`:

```rego
package terraform.modules

import rego.v1

# ============================================================
# Allowed source configuration
# ============================================================

# Allowed Terraform registries
allowed_registries := {"registry.terraform.io"}

# Allowed GitHub organizations
allowed_github_orgs := {"myorg", "myorg-infra"}

# The current environment
environment := input.variables.environment.value

# ============================================================
# Helpers
# ============================================================

# Extracts all module calls from the root module
module_calls[name] := mc if {
	some name, mc in input.configuration.root_module.module_calls
}

# Determines if a source is a local path (starts with ./ or ../)
is_local_path(source) if {
	startswith(source, "./")
}

is_local_path(source) if {
	startswith(source, "../")
}

# Determines if a source comes from a Terraform registry
is_registry_source(source) if {
	some registry in allowed_registries
	startswith(source, registry)
}

# Extracts the GitHub organization from a git source
# Example: "git::https://github.com/myorg/repo.git//path" -> "myorg"
github_org(source) := org if {
	contains(source, "github.com/")
	parts := split(source, "github.com/")
	after_github := parts[1]
	org_and_rest := split(after_github, "/")
	org := org_and_rest[0]
}

# Determines if a GitHub source comes from an allowed org
is_allowed_github(source) if {
	org := github_org(source)
	org in allowed_github_orgs
}

# Determines if a source comes from an allowed git host
# (only GitHub from allowed orgs for now)
is_allowed_git(source) if {
	startswith(source, "git::")
	is_allowed_github(source)
}

# ============================================================
# Deny: local modules in production
# ============================================================

deny contains msg if {
	environment == "prod"

	some name, mc in module_calls
	is_local_path(mc.source)

	msg := sprintf(
		"Module '%s' uses a local path ('%s'). In production, all modules must come from a registry or versioned repository.",
		[name, mc.source],
	)
}

# ============================================================
# Deny: unauthorized sources
# ============================================================

deny contains msg if {
	some name, mc in module_calls

	# Not local (locals are already covered above)
	not is_local_path(mc.source)

	# Not an allowed registry
	not is_registry_source(mc.source)

	# Not an allowed git repo
	not is_allowed_git(mc.source)

	msg := sprintf(
		"Module '%s' uses an unauthorized source: '%s'. Only registries %v and GitHub repositories from organizations %v are allowed.",
		[name, mc.source, allowed_registries, allowed_github_orgs],
	)
}

# ============================================================
# Warnings: git modules without pinned version
# ============================================================

warnings contains msg if {
	some name, mc in module_calls
	startswith(mc.source, "git::")

	# If it doesn't have ?ref= in the source, it's not pinned to a version
	not contains(mc.source, "?ref=")

	msg := sprintf(
		"Module '%s' uses a git repository without a fixed version (no ?ref=). This can cause unexpected changes: '%s'.",
		[name, mc.source],
	)
}

# ============================================================
# Report
# ============================================================

all_modules contains info if {
	some name, mc in module_calls
	info := {
		"name": name,
		"source": mc.source,
		"is_local": is_local_path(mc.source),
		"is_registry": is_registry_source(mc.source),
		"is_allowed_git": is_allowed_git(mc.source),
		"blocked": _is_blocked(name, mc),
	}
}

# Internal helper to determine if a module would be blocked
_is_blocked(name, mc) if {
	environment == "prod"
	is_local_path(mc.source)
} else if {
	not is_local_path(mc.source)
	not is_registry_source(mc.source)
	not is_allowed_git(mc.source)
} else := false
```

A few things worth examining.

The `github_org` function does fairly manual parsing of the source. It takes something like `git::https://github.com/myorg/terraform-modules.git//network?ref=v2.1.0`, splits on `github.com/`, keeps `myorg/terraform-modules.git//network?ref=v2.1.0`, and then splits on `/` to get `myorg`. It is not elegant, but it works.

The deny section has two separate rules: one for local modules in production, and one for unauthorized sources. They are separated because they have different messages and because local modules might be acceptable in dev/staging.

The warning for git modules without `?ref=` is important. A source like `git::https://github.com/myorg/repo.git//module` without `?ref=v1.0.0` points to the HEAD of the default branch, which can change without notice. It is the equivalent of using `latest` in a Docker image -- it works until it does not.

---

## Testing

Check the violations:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.modules.deny" --format pretty
```

Expected output:

```json
[
  "Module 'another_local' uses a local path ('../shared/common'). In production, all modules must come from a registry or versioned repository.",
  "Module 'bitbucket_module' uses an unauthorized source: 'git::https://bitbucket.org/someteam/infra-modules.git//storage'. Only registries {\"registry.terraform.io\"} and GitHub repositories from organizations {\"myorg\", \"myorg-infra\"} are allowed.",
  "Module 'local_utils' uses a local path ('./modules/utils'). In production, all modules must come from a registry or versioned repository.",
  "Module 'sketchy_module' uses an unauthorized source: 'git::https://github.com/random-user/cool-terraform-stuff.git//lambda'. Only registries {\"registry.terraform.io\"} and GitHub repositories from organizations {\"myorg\", \"myorg-infra\"} are allowed."
]
```

Four violations:
- The two local modules are blocked because the environment is prod.
- The `random-user` module is blocked because that GitHub org is not in the allowlist.
- The Bitbucket module is blocked because it is not a permitted git host.

The registry modules (`vpc`, `eks`) and the `myorg` GitHub modules (`custom_network`, `monitoring`) pass without issue.

Now check the warnings:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.modules.warnings" --format pretty
```

Expected output:

```json
[
  "Module 'bitbucket_module' uses a git repository without a fixed version (no ?ref=). This can cause unexpected changes: 'git::https://bitbucket.org/someteam/infra-modules.git//storage'.",
  "Module 'sketchy_module' uses a git repository without a fixed version (no ?ref=). This can cause unexpected changes: 'git::https://github.com/random-user/cool-terraform-stuff.git//lambda'."
]
```

The git modules without `?ref=` generate warnings. The `myorg` modules have `?ref=v2.1.0` and `?ref=v1.5.0`, so they do not appear.

To explore all modules:

```bash
opa eval -i tfplan.json -d policy.rego "data.terraform.modules.all_modules" --format pretty
```

This gives you a detailed report of each module with its classification.

---

## Verify What You Learned

**Command 1** -- Count how many modules are blocked:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.modules.deny)" --format pretty
```

Expected output: `4`

**Command 2** -- Verify that the `vpc` module from the registry does NOT generate violations:

```bash
opa eval -i tfplan.json -d policy.rego \
  "{msg | some msg in data.terraform.modules.deny; contains(msg, \"'vpc'\")}" \
  --format pretty
```

Expected output: `[]`

**Command 3** -- Count how many warnings there are for modules without a pinned version:

```bash
opa eval -i tfplan.json -d policy.rego "count(data.terraform.modules.warnings)" --format pretty
```

Expected output: `2`

---

## Section Summary

Across these five exercises you built a complete Terraform policy toolkit:

1. **Plan JSON** -- You learned to read and navigate the Terraform plan JSON structure, building reusable helpers for grouping resources by type and action.
2. **Tag Enforcement** -- You created data-driven policies that validate required tags and allowed values, handling the `null` tags edge case.
3. **Security Guardrails** -- You prevented dangerous configurations (open security groups, unencrypted storage) with an exception mechanism for approved bypasses.
4. **Cost Controls** -- You enforced per-environment instance type limits, reading the target environment directly from Terraform variables.
5. **Module Sources** -- You restricted where Terraform modules can come from, blocking unauthorized registries and local paths in production.

These patterns form the foundation for a policy-as-code practice around Terraform. Each policy is data-driven, produces clear messages, and separates concerns between detection and enforcement. In the next section, you will apply similar thinking to Kubernetes admission control with OPA Gatekeeper.

## What's Next

You have covered the major categories of Terraform plan policies: structure, tags, security, cost, and supply chain. In the next section, you will shift to Kubernetes and learn how OPA Gatekeeper enforces policies at the admission controller level -- same Rego skills, different input format.

## Reference

- [Terraform Plan JSON: Configuration](https://developer.hashicorp.com/terraform/internals/json-format#configuration-representation) -- the `configuration.root_module.module_calls` section with module source and version information.
- [OPA Policy Reference: Strings](https://www.openpolicyagent.org/docs/latest/policy-reference/#strings) -- `startswith`, `contains`, `split`, and other string builtins.
- Nested modules (modules inside modules) appear inside the `module` field of each module_call. For deep analysis, you would need a recursive function using `walk`.
- In production, you could connect this to a module approval system where each new source requires a security review before being added to the allowlist.

## Additional Resources

- [Terraform Module Sources](https://developer.hashicorp.com/terraform/language/modules/sources) -- all supported module source formats.
- [SLSA Framework](https://slsa.dev/) -- supply-chain security framework applicable to infrastructure modules.
- [Conftest](https://www.conftest.dev/) -- tool for running OPA tests against structured configuration data.
- [Terraform Registry](https://registry.terraform.io/) -- the official public registry for Terraform modules.
