# Deploy

## Prerequisites

- Rust stable + cargo-lambda (`pip3 install cargo-lambda`)
- just (`brew install just`)
- Terraform >= 1.5
- AWS CLI with valid credentials (`aws sts get-caller-identity`)

## State bucket (one-time setup)

```bash
ACCOUNT_ID=$(aws sts get-caller-identity --query Account --output text)
BUCKET="infra-terraform-state-$ACCOUNT_ID"

aws s3api create-bucket --bucket $BUCKET --region us-east-1
aws s3api put-bucket-versioning --bucket $BUCKET --versioning-configuration Status=Enabled
aws s3api put-bucket-encryption --bucket $BUCKET \
  --server-side-encryption-configuration \
  '{"Rules":[{"ApplyServerSideEncryptionByDefault":{"SSEAlgorithm":"AES256"}}]}'
```

## Step 1: Build

```bash
just build
```

Cross-compiles all functions to arm64 Linux binaries and packages them as zips in `target/lambda/{name}/bootstrap.zip`. Re-run after any Rust code change.

Single function: `just build-one hello-api`

## Step 2: Terraform init

```bash
just init
```

Configures the S3 backend and creates/selects the `dev` workspace. Only needed once per machine or after deleting `.terraform/`.

For a different environment: `ENV=staging just init`

## Step 3: Plan

```bash
just plan
```

Reads YAML config + compiled zips and shows what will be created. Does not touch AWS. Review before applying.

## Step 4: Apply

```bash
just apply
```

Creates the resources. Type `yes` to confirm.

## Step 5: Test

```bash
API=$(terraform -chdir=terraform output -raw api_endpoint)

curl $API/hello    # {"message":"Hello from Rust!"}
curl $API/health   # {"status":"ok"}
```

`hello-scheduled` runs automatically every 5 minutes via EventBridge. Check its logs:

```bash
aws logs tail /aws/lambda/infra-dev-hello-scheduled --follow
```

## Environments

```bash
ENV=staging just init
just build
just plan
just apply
```

Each workspace has isolated state. Resources: `infra-staging-hello-api`, etc.

## Teardown

```bash
just destroy        # remove AWS resources
just destroy-all    # remove AWS resources + state bucket
```

## Troubleshooting

```bash
aws logs tail /aws/lambda/infra-dev-hello-api --follow
terraform -chdir=terraform state list
```

## Adding a new HTTP Lambda

1. Create `functions/{name}/Cargo.toml` and `src/main.rs` (copy from `hello-api`)
2. Add to `members` in root `Cargo.toml`
3. Add to `config/functions.yaml`
4. Add route in `config/routes.yaml`
5. `just build && just plan && just apply`

For scheduled Lambdas: use `lambda_runtime` instead of `lambda_http`, add to `config/schedules.yaml` instead of `routes.yaml`. See `hello-scheduled` as reference.
