# Terraform Tutorial

> 47 hands-on exercises for Terraform organized in 11 sections.
> Each exercise is a self-contained tutorial with theory, code, and verification.
> From beginner to advanced.

**Requirements**:
- Terraform >= 1.7 installed ([install guide](https://developer.hashicorp.com/terraform/install))
- AWS CLI configured with a sandbox account ([configure guide](https://docs.aws.amazon.com/cli/latest/userguide/cli-configure-quickstart.html))
- `terraform console` available for interactive testing

**Convention**: Each exercise uses a clean directory. Run `terraform init` before `plan`/`apply`. Clean up with `terraform destroy` when done. The `.tf` files are shown as named code blocks that the reader creates manually. The act of creating each file is part of the learning process.

---

### 01 - Variables and Types (4 exercises)

> Complex types, optional attributes, validation rules, sensitive values.

| # | Exercise |
|---|----------|
| 1 | [Map of Objects with for_each](01-variables-and-types/01-map-of-objects-with-for-each/) |
| 2 | [Optional Attributes with Defaults](01-variables-and-types/02-optional-attributes-with-defaults/) |
| 3 | [Variable Validation with Custom Error Messages](01-variables-and-types/03-variable-validation-custom-error-messages/) |
| 4 | [Sensitive Variables](01-variables-and-types/04-sensitive-variables/) |

### 02 - Expressions and Functions (8 exercises)

> Network math, list flattening, templates, map operations, regex, string formatting, fallbacks.

| # | Exercise |
|---|----------|
| 5 | [cidrsubnet() for Subnet Calculation](02-expressions-and-functions/01-cidrsubnet-for-subnet-calculation/) |
| 6 | [flatten() for Nested Lists](02-expressions-and-functions/02-flatten-for-nested-lists/) |
| 7 | [templatefile() for User Data](02-expressions-and-functions/03-templatefile-for-user-data/) |
| 8 | [merge() and lookup() for Map Defaults](02-expressions-and-functions/04-merge-and-lookup-for-map-defaults/) |
| 9 | [regex() for Input Validation](02-expressions-and-functions/05-regex-for-input-validation/) |
| 10 | [formatlist() and join() for ARN Construction](02-expressions-and-functions/06-formatlist-and-join-for-arn-construction/) |
| 11 | [zipmap() to Create Maps from Lists](02-expressions-and-functions/07-zipmap-to-create-maps-from-lists/) |
| 12 | [coalesce() and try() for Fallback Values](02-expressions-and-functions/08-coalesce-and-try-for-fallback-values/) |

### 03 - Dynamic Blocks (3 exercises)

> Generate repeated nested blocks from data: security groups, IAM policies, tags.

| # | Exercise |
|---|----------|
| 13 | [Dynamic Ingress/Egress Rules](03-dynamic-blocks/01-dynamic-ingress-egress-rules/) |
| 14 | [Dynamic IAM Policy Statements](03-dynamic-blocks/02-dynamic-iam-policy-statements/) |
| 15 | [Dynamic Tags from a Map](03-dynamic-blocks/03-dynamic-tags-from-a-map/) |

### 04 - for_each and count Patterns (5 exercises)

> Resource iteration: lists, maps, conditionals, YAML-driven, set conversion.

| # | Exercise |
|---|----------|
| 16 | [Multiple S3 Buckets from a List](04-for-each-and-count-patterns/01-multiple-s3-buckets-from-a-list/) |
| 17 | [IAM Users with Different Policies via for_each](04-for-each-and-count-patterns/02-iam-users-with-different-policies/) |
| 18 | [Conditional Resource Creation with count](04-for-each-and-count-patterns/03-conditional-resource-creation-with-count/) |
| 19 | [Resources from YAML with yamldecode + for_each](04-for-each-and-count-patterns/04-resources-from-yaml-with-yamldecode/) |
| 20 | [for_each with toset()](04-for-each-and-count-patterns/05-for-each-with-toset/) |

### 05 - State Operations (5 exercises)

> Import, move, remove, declarative import blocks, raw state inspection.

| # | Exercise |
|---|----------|
| 21 | [Import an Existing S3 Bucket](05-state-operations/01-import-existing-s3-bucket/) |
| 22 | [Move a Resource to a Module with state mv](05-state-operations/02-move-resource-to-module-state-mv/) |
| 23 | [Remove from State Without Destroying](05-state-operations/03-remove-from-state-without-destroying/) |
| 24 | [Declarative Import Block](05-state-operations/04-declarative-import-block/) |
| 25 | [State Pull, Inspect, Push](05-state-operations/05-state-pull-inspect-push/) |

### 06 - Lifecycle Rules (4 exercises)

> Control resource replacement order, prevent accidental deletion, ignore drift, trigger replacements.

| # | Exercise |
|---|----------|
| 26 | [create_before_destroy](06-lifecycle-rules/01-create-before-destroy/) |
| 27 | [prevent_destroy](06-lifecycle-rules/02-prevent-destroy/) |
| 28 | [ignore_changes](06-lifecycle-rules/03-ignore-changes/) |
| 29 | [replace_triggered_by](06-lifecycle-rules/04-replace-triggered-by/) |

### 07 - Data Sources (5 exercises)

> Query AWS for AMIs, availability zones, account identity, remote state, external scripts.

| # | Exercise |
|---|----------|
| 30 | [aws_ami: Find Latest Amazon Linux 2023](07-data-sources/01-aws-ami-find-latest-amazon-linux/) |
| 31 | [aws_availability_zones for Dynamic AZs](07-data-sources/02-aws-availability-zones-dynamic/) |
| 32 | [aws_caller_identity for Account-Aware ARNs](07-data-sources/03-aws-caller-identity-account-aware-arns/) |
| 33 | [terraform_remote_state](07-data-sources/04-terraform-remote-state/) |
| 34 | [external Data Source](07-data-sources/05-external-data-source/) |

### 08 - Outputs and Locals (3 exercises)

> Structured outputs, derived values with locals, multi-step data transformations.

| # | Exercise |
|---|----------|
| 35 | [Structured Outputs](08-outputs-and-locals/01-structured-outputs/) |
| 36 | [Locals for Derived Values](08-outputs-and-locals/02-locals-for-derived-values/) |
| 37 | [Chained Locals for Multi-Step Transforms](08-outputs-and-locals/03-chained-locals-multi-step-transforms/) |

### 09 - Provider Configuration (3 exercises)

> Multi-region deployments, cross-account access, passing providers to modules.

| # | Exercise |
|---|----------|
| 38 | [Multiple AWS Providers for Multi-Region](09-provider-configuration/01-multiple-aws-providers-multi-region/) |
| 39 | [assume_role for Cross-Account Access](09-provider-configuration/02-assume-role-cross-account-access/) |
| 40 | [Pass Providers to Child Modules](09-provider-configuration/03-pass-providers-to-child-modules/) |

### 10 - Modules (4 exercises)

> Create reusable modules, iterate with for_each, control dependencies, pin versions.

| # | Exercise |
|---|----------|
| 41 | [Create a Minimal Module](10-modules/01-create-a-minimal-module/) |
| 42 | [Module with for_each](10-modules/02-module-with-for-each/) |
| 43 | [Module depends_on](10-modules/03-module-depends-on/) |
| 44 | [Pin Module Versions from Git](10-modules/04-pin-module-versions-from-git/) |

### 11 - Testing (3 exercises)

> Native Terraform testing: plan-mode assertions, integration tests with apply, mock providers.

| # | Exercise |
|---|----------|
| 45 | [Plan-Mode Test with .tftest.hcl](11-testing/01-plan-mode-test-tftest-hcl/) |
| 46 | [Integration Test with Apply](11-testing/02-integration-test-with-apply/) |
| 47 | [mock_provider for Unit Testing](11-testing/03-mock-provider-unit-testing/) |
