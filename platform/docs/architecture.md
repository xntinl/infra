# Architecture

Serverless Rust project with hexagonal architecture deployed to AWS via Terraform.

## Project structure

```
infra/
├── justfile                   Command runner (build, serve, init, plan, apply)
├── Cargo.toml                 Workspace root
├── config/                    Declarative YAML consumed by Terraform
│   ├── functions.yaml         Lambda definitions (timeout, memory)
│   ├── routes.yaml            HTTP routes (method, path, function)
│   ├── schedules.yaml         EventBridge rules (expression, function)
│   ├── workflows.yaml         Step Functions (definition, functions)
│   └── workflows/*.json       ASL definitions with ${fn_name_arn} placeholders
├── functions/                 One crate per Lambda binary
│   ├── hello-api/             HTTP: GET /hello
│   ├── health-api/            HTTP: GET /health
│   └── hello-scheduled/       EventBridge: rate(5 minutes)
├── libs/
│   ├── domain/                Business logic (zero cloud deps)
│   ├── ports/                 Traits / interfaces
│   ├── adapters-aws/          AWS implementations of ports
│   ├── app-http/              axum Router (routes + handlers)
│   └── shared/                Tracing init
└── terraform/
    ├── main.tf                Root module: reads YAML, instantiates modules
    ├── variables.tf           region, project_name
    ├── terraform.tfvars       us-east-1, infra
    ├── providers.tf           AWS ~5.0 with default tags
    ├── backend.tf             S3 backend
    ├── outputs.tf             api_endpoint, function_arns, etc
    └── modules/
        ├── compute/           Lambda + IAM + CloudWatch logs
        ├── api/               API Gateway HTTP v2
        ├── events/            EventBridge bus + rules
        └── workflow/          Step Functions + IAM
```

## Hexagonal layers

- **domain**: pure Rust, serde, tracing. No cloud deps.
- **ports**: traits the domain needs. thiserror for typed errors.
- **adapters-aws**: implements port traits with AWS services.
- **app-http**: axum routes connecting HTTP to domain functions.
- **functions/**: each Lambda binary wires the layers together. Uses anyhow for error handling.

## Feature flags

HTTP Lambdas have a `lambda` feature (default on):
- `cargo lambda build` -> Lambda runtime
- `cargo run --no-default-features` -> standalone HTTP server

## Terraform modules

All modules use `for_each` driven by the YAML config files.

| Module | AWS Resource | What it creates |
|--------|-------------|-----------------|
| `compute` | Lambda | Function, IAM role, CloudWatch log group |
| `api` | API Gateway v2 | HTTP API, routes, integrations, permissions |
| `events` | EventBridge | Custom bus, rules, targets, permissions |
| `workflow` | Step Functions | State machine, IAM roles, log group |

Resources are named `{project_name}-{workspace}-{name}` (e.g. `infra-dev-hello-api`).
