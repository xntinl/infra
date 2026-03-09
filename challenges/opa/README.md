# OPA/Rego Tutorial

> 40 hands-on exercises for Open Policy Agent organized in 9 sections.
> Each exercise is a self-contained tutorial with theory, code, and verification.
> From beginner to advanced.

**Requirements**:
- `opa` CLI installed ([install guide](https://www.openpolicyagent.org/docs/latest/#running-opa))
- `conftest` installed (from section 07 onward) ([install guide](https://www.conftest.dev/install/))

**Convention**: Each exercise is self-contained in its README.md. The `.rego` and `.json` files are shown as named code blocks that the reader creates manually. The act of creating each file is part of the learning process.

---

### 01 - Elements (8 exercises)

> Foundations: data types, operators, `input`, rules, true/false/undefined, iteration with `some`, AND/OR/NOT logic.

| # | Exercise |
|---|----------|
| 1 | [Hello OPA](01-elements/01-hello-opa/) |
| 2 | [Data Types](01-elements/02-data-types/) |
| 3 | [Operators](01-elements/03-operators/) |
| 4 | [The Input Document](01-elements/04-input-document/) |
| 5 | [Your First Rule](01-elements/05-your-first-rule/) |
| 6 | [True, False, and Undefined](01-elements/06-true-false-undefined/) |
| 7 | [Iteration with some](01-elements/07-iteration-with-some/) |
| 8 | [Logic: AND, OR, and NOT](01-elements/08-logic-and-or-not/) |

### 02 - Rego Fundamentals (6 exercises)

> Input vs data, partial rules, comprehensions, built-ins, user functions, `every`.

| # | Exercise |
|---|----------|
| 1 | [Input vs Data](02-rego-fundamentals/01-input-and-data/) |
| 2 | [Partial Rules and Default](02-rego-fundamentals/02-partial-rules-and-default/) |
| 3 | [Comprehensions](02-rego-fundamentals/03-comprehensions/) |
| 4 | [Built-in Functions](02-rego-fundamentals/04-built-in-functions/) |
| 5 | [User Functions](02-rego-fundamentals/05-functions/) |
| 6 | [The every Keyword](02-rego-fundamentals/06-every-keyword/) |

### 03 - Rego Testing (4 exercises)

> Unit tests, fixtures, mocking, coverage, table-driven tests.

| # | Exercise |
|---|----------|
| 1 | [Unit Tests](03-rego-testing/01-unit-tests/) |
| 2 | [Fixtures and Mocking](03-rego-testing/02-fixtures-and-mocking/) |
| 3 | [Coverage](03-rego-testing/03-coverage/) |
| 4 | [Table-driven Tests](03-rego-testing/04-table-driven-tests/) |

### 04 - Terraform Policy (5 exercises)

> Validate Terraform plans: tags, security, costs, module sources.

| # | Exercise |
|---|----------|
| 1 | [Terraform Plan JSON](04-terraform-policy/01-plan-json/) |
| 2 | [Tag Enforcement](04-terraform-policy/02-tag-enforcement/) |
| 3 | [Security Guardrails](04-terraform-policy/03-security-guardrails/) |
| 4 | [Cost Controls](04-terraform-policy/04-cost-controls/) |
| 5 | [Module Source Restrictions](04-terraform-policy/05-module-sources/) |

### 05 - Kubernetes Admission (4 exercises)

> Gatekeeper, labels, image allowlist, security contexts.

| # | Exercise |
|---|----------|
| 1 | [OPA Gatekeeper](05-kubernetes-admission/01-gatekeeper/) |
| 2 | [Required Labels](05-kubernetes-admission/02-required-labels/) |
| 3 | [Image Allowlist](05-kubernetes-admission/03-image-allowlist/) |
| 4 | [Security Contexts](05-kubernetes-admission/04-security-contexts/) |

### 06 - API Authorization (4 exercises)

> HTTP RBAC, ABAC, JWT validation, decision logging.

| # | Exercise |
|---|----------|
| 1 | [HTTP RBAC](06-api-authorization/01-http-rbac/) |
| 2 | [ABAC](06-api-authorization/02-abac/) |
| 3 | [JWT Validation](06-api-authorization/03-jwt-validation/) |
| 4 | [Decision Logging](06-api-authorization/04-decision-logging/) |

### 07 - CI/CD Integration (3 exercises)

> Conftest for Dockerfiles, Kubernetes manifests, and Terraform plans.

| # | Exercise |
|---|----------|
| 1 | [Conftest + Dockerfiles](07-cicd-integration/01-conftest-dockerfiles/) |
| 2 | [Conftest + Kubernetes](07-cicd-integration/02-conftest-kubernetes/) |
| 3 | [Conftest + Terraform](07-cicd-integration/03-conftest-terraform/) |

### 08 - Policy Distribution (3 exercises)

> Bundles, OPA server, decision logs.

| # | Exercise |
|---|----------|
| 1 | [Bundles](08-policy-distribution/01-bundles/) |
| 2 | [OPA as a Server](08-policy-distribution/02-opa-server/) |
| 3 | [Decision Logs](08-policy-distribution/03-decision-logs/) |

### 09 - Advanced Patterns (3 exercises)

> Performance, composition, compliance frameworks.

| # | Exercise |
|---|----------|
| 1 | [Performance](09-advanced-patterns/01-performance/) |
| 2 | [Composition](09-advanced-patterns/02-composition/) |
| 3 | [Compliance Framework](09-advanced-patterns/03-compliance-framework/) |
