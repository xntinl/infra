# Improvements

## 1. S3 native state locking

Terraform 1.10+ supports native S3 locking without DynamoDB.

```hcl
# backend.tf
terraform { backend "s3" { use_lockfile = true } }

# providers.tf
required_version = ">= 1.10"
```

## 2. Release profile

```toml
# Root Cargo.toml
[profile.release]
lto = "thin"
codegen-units = 1
panic = "abort"
strip = true
```

## 3. X-Ray tracing

Add to `modules/compute/main.tf` inside `aws_lambda_function`:

```hcl
tracing_config { mode = "Active" }
```

## 4. CI/CD (GitHub Actions)

Three jobs: `check` (fmt, clippy, test) -> `build` (cargo lambda build, upload zips) -> `terraform` (plan, apply on main). Use sccache for caching and OIDC for AWS auth.

## 5. Narrow aws_lambda_events features

```toml
aws_lambda_events = { version = "0.15", default-features = false, features = ["apigw", "cloudwatch_events"] }
```

## 6. Per-function IAM policies

Extend `functions.yaml` with `policies` field. Modify compute module to create inline policies dynamically.

## 7. OpenTelemetry

Add `opentelemetry-aws` + `tracing-opentelemetry` for distributed tracing with X-Ray ID correlation.

## 8. cargo lambda watch

```
just watch hello-api
```

Add to justfile: `watch name: cargo lambda watch -p {{name}}`

## 9. Terraform security scanning

Add `tfsec-action` to CI pipeline.
