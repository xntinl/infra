# 35. API Gateway Mutual TLS Authentication

<!--
difficulty: advanced
concepts: [mutual-tls, mtls, client-certificates, ca-certificates, truststore, s3-truststore, custom-domain, acm-certificate, tls-handshake, x509]
tools: [terraform, aws-cli, openssl, curl]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [02-api-gateway-rest-vs-http-validation]
aws_cost: ~$0.02/hr
-->

> **AWS Cost Warning:** This exercise creates an API Gateway custom domain with ACM certificate and an S3 bucket for the truststore. The custom domain costs ~$0.02/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- OpenSSL installed (`openssl version`)
- A registered domain name with Route 53 hosted zone (or ability to create DNS records)

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** when mutual TLS is appropriate compared to API keys, IAM auth, or Cognito authorizers for securing API endpoints
- **Design** a certificate chain for mTLS: generate a Certificate Authority (CA), sign client certificates, and build a truststore PEM bundle
- **Implement** mutual TLS on an API Gateway REST API by uploading the truststore to S3 and configuring the custom domain
- **Analyze** TLS handshake failures caused by certificate chain issues, expired certificates, or incorrect truststore format
- **Configure** the API Gateway custom domain with ACM certificate (server-side) and S3-based truststore (client verification)

## Why This Matters

Mutual TLS (mTLS) adds a layer of authentication beyond standard TLS: instead of only the server proving its identity to the client, the client must also present a certificate that the server trusts. This is essential for machine-to-machine communication where API keys are insufficient -- for example, IoT devices, partner integrations, or compliance-regulated systems that require certificate-based authentication.

The DVA-C02 exam tests mTLS in the context of API Gateway custom domains. You must understand that mTLS requires a custom domain name (it does not work with the default `execute-api` endpoint), the truststore must be a PEM file stored in S3, and the truststore contains the CA certificate(s) that signed the client certificates. The exam also tests failure scenarios: what happens when the client certificate is signed by a CA not in the truststore (403 Forbidden), when the certificate is expired, or when the truststore PEM file is malformed.

## The Challenge

Build an API Gateway REST API with mutual TLS authentication. Clients must present a valid client certificate signed by your CA to access the API.

### Requirements

| Requirement | Description |
|---|---|
| Certificate Authority | Generate a self-signed CA certificate using OpenSSL |
| Client Certificate | Generate a client key and certificate signed by the CA |
| Truststore | Upload CA certificate (PEM format) to S3 |
| Custom Domain | Create API Gateway custom domain with mTLS enabled |
| ACM Certificate | Request or import a certificate for the server side |
| Lambda Backend | Simple Go Lambda behind proxy integration |
| Verification | Prove that requests without client cert are rejected (403) |

### Architecture

```
  Client with cert         API Gateway              Lambda
  +--------------+    +------------------+    +-------------+
  |              |    |  Custom Domain   |    |             |
  | client.key   |--->|  mTLS enabled    |--->|  handler()  |
  | client.crt   |    |  truststore: S3  |    |             |
  |              |    |                  |    |             |
  +--------------+    +------------------+    +-------------+
                           |
                      Truststore (S3)
                      +------------------+
                      | ca-cert.pem      |
                      | (CA certificate  |
                      |  that signed     |
                      |  client.crt)     |
                      +------------------+
```

## Hints

<details>
<summary>Hint 1: Generating the CA and client certificates with OpenSSL</summary>

Create a `certs/` directory and generate the certificate chain:

```bash
mkdir -p certs && cd certs

# 1. Generate CA private key
openssl genrsa -out ca.key 2048

# 2. Generate self-signed CA certificate (valid for 365 days)
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/C=US/ST=State/L=City/O=MyOrg/CN=MyCA"

# 3. Generate client private key
openssl genrsa -out client.key 2048

# 4. Generate client certificate signing request (CSR)
openssl req -new -key client.key -out client.csr \
  -subj "/C=US/ST=State/L=City/O=MyOrg/CN=client1"

# 5. Sign the client certificate with the CA
openssl x509 -req -days 365 -in client.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt

# 6. Verify the chain
openssl verify -CAfile ca.crt client.crt
# Expected: client.crt: OK
```

The truststore uploaded to S3 is the `ca.crt` file (PEM format). It contains the CA certificate that API Gateway uses to verify client certificates during the TLS handshake.

</details>

<details>
<summary>Hint 2: S3 bucket for the truststore</summary>

The truststore must be a PEM file in S3. API Gateway reads this file to validate client certificates. The S3 object must be accessible by API Gateway (the bucket does not need to be public -- API Gateway uses the service's own credentials).

```hcl
resource "aws_s3_bucket" "truststore" {
  bucket        = "${var.project_name}-truststore-${random_id.suffix.hex}"
  force_destroy = true
}

resource "random_id" "suffix" {
  byte_length = 4
}

resource "aws_s3_object" "truststore" {
  bucket = aws_s3_bucket.truststore.id
  key    = "truststore.pem"
  source = "${path.module}/certs/ca.crt"
  etag   = filemd5("${path.module}/certs/ca.crt")
}
```

The truststore PEM file can contain multiple CA certificates if you need to trust certificates from different CAs. Concatenate them into a single file:

```bash
cat ca1.crt ca2.crt > truststore.pem
```

</details>

<details>
<summary>Hint 3: Custom domain with mTLS configuration</summary>

Mutual TLS is configured on the API Gateway custom domain, not on the REST API itself. You need an ACM certificate for the server side and the S3 truststore URI for client verification.

```hcl
resource "aws_api_gateway_domain_name" "this" {
  domain_name              = "api.${var.domain_name}"
  regional_certificate_arn = aws_acm_certificate_validation.this.certificate_arn

  mutual_tls_authentication {
    truststore_uri = "s3://${aws_s3_bucket.truststore.id}/${aws_s3_object.truststore.key}"
  }

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}
```

The `truststore_uri` must use the `s3://bucket/key` format. If the truststore PEM is malformed or empty, API Gateway returns a validation error during domain creation.

</details>

<details>
<summary>Hint 4: Testing mTLS with curl</summary>

```bash
# With client certificate -- should succeed (200)
curl -s --cert certs/client.crt --key certs/client.key \
  "https://api.example.com/dev/hello" | jq .

# Without client certificate -- should fail (403)
curl -s "https://api.example.com/dev/hello"
# Returns: {"message":"Forbidden"}

# With wrong certificate -- should fail (403)
openssl req -new -x509 -days 365 -newkey rsa:2048 -nodes \
  -keyout certs/wrong.key -out certs/wrong.crt \
  -subj "/CN=wrong"
curl -s --cert certs/wrong.crt --key certs/wrong.key \
  "https://api.example.com/dev/hello"
# Returns: {"message":"Forbidden"}
```

</details>

<details>
<summary>Hint 5: Accessing client certificate information in Lambda</summary>

When mTLS is enabled, API Gateway passes client certificate information in the request context. In proxy integration, this is available in the `requestContext.identity` object:

```go
func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
    // Client certificate info is in the request context
    clientCert := req.RequestContext.Identity.ClientCert

    // clientCert contains:
    // - ClientCertPem: the full PEM-encoded client certificate
    // - SubjectDN: "CN=client1,O=MyOrg,L=City,ST=State,C=US"
    // - IssuerDN: "CN=MyCA,O=MyOrg,L=City,ST=State,C=US"
    // - SerialNumber: "1234567890"
    // - Validity.NotBefore: "2024-01-01T00:00:00Z"
    // - Validity.NotAfter: "2025-01-01T00:00:00Z"

    body, _ := json.Marshal(map[string]interface{}{
        "message": "Authenticated via mTLS",
        "subject": clientCert.SubjectDN,
        "issuer":  clientCert.IssuerDN,
    })

    return events.APIGatewayProxyResponse{
        StatusCode: 200,
        Body:       string(body),
    }, nil
}
```

</details>

## Spot the Bug

A developer configured mutual TLS on API Gateway. The API rejects all requests with 403 Forbidden, even when the client presents a valid certificate signed by the correct CA. **What is wrong?**

```hcl
resource "aws_s3_object" "truststore" {
  bucket       = aws_s3_bucket.truststore.id
  key          = "truststore.pem"
  content      = file("${path.module}/certs/client.crt")   # <-- BUG
  content_type = "application/x-pem-file"
}

resource "aws_api_gateway_domain_name" "this" {
  domain_name              = "api.example.com"
  regional_certificate_arn = aws_acm_certificate_validation.this.certificate_arn

  mutual_tls_authentication {
    truststore_uri = "s3://${aws_s3_bucket.truststore.id}/${aws_s3_object.truststore.key}"
  }

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}
```

<details>
<summary>Explain the bug</summary>

The truststore contains the **client certificate** (`client.crt`) instead of the **CA certificate** (`ca.crt`). The truststore must contain the Certificate Authority certificate(s) that signed the client certificates, not the client certificates themselves.

During the TLS handshake, API Gateway checks whether the presented client certificate was signed by a CA whose certificate is in the truststore. If the truststore contains the client certificate instead of the CA certificate, the chain of trust cannot be established, and all requests are rejected with 403.

The fix:

```hcl
resource "aws_s3_object" "truststore" {
  bucket       = aws_s3_bucket.truststore.id
  key          = "truststore.pem"
  content      = file("${path.module}/certs/ca.crt")   # CA certificate, not client
  content_type = "application/x-pem-file"
}
```

This is a common mistake and exam trap: the truststore holds the CA certificate(s), not the client certificate(s). The client sends its certificate during the handshake; the server (API Gateway) verifies it against the CA certificates in the truststore.

</details>

<details>
<summary>Full Solution</summary>

### File Structure

```
35-api-gateway-mutual-tls-authentication/
├── main.tf
├── main.go
└── certs/
    ├── ca.key
    ├── ca.crt
    ├── client.key
    ├── client.csr
    └── client.crt
```

### Generate certificates

```bash
mkdir -p certs && cd certs
openssl genrsa -out ca.key 2048
openssl req -new -x509 -days 365 -key ca.key -out ca.crt \
  -subj "/C=US/ST=State/L=City/O=MyOrg/CN=MyCA"
openssl genrsa -out client.key 2048
openssl req -new -key client.key -out client.csr \
  -subj "/C=US/ST=State/L=City/O=MyOrg/CN=client1"
openssl x509 -req -days 365 -in client.csr \
  -CA ca.crt -CAkey ca.key -CAcreateserial -out client.crt
cd ..
```

### `main.go`

```go
package main

import (
	"context"
	"encoding/json"
	"time"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
)

func handler(ctx context.Context, req events.APIGatewayProxyRequest) (events.APIGatewayProxyResponse, error) {
	body, _ := json.Marshal(map[string]interface{}{
		"message":   "Authenticated via mTLS",
		"path":      req.Path,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})

	return events.APIGatewayProxyResponse{
		StatusCode: 200,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       string(body),
	}, nil
}

func main() {
	lambda.Start(handler)
}
```

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws     = { source = "hashicorp/aws", version = "~> 5.0" }
    archive = { source = "hashicorp/archive", version = "~> 2.0" }
    null    = { source = "hashicorp/null", version = "~> 3.0" }
    random  = { source = "hashicorp/random", version = "~> 3.0" }
  }
}

provider "aws" {
  region = var.region
}
```

### `variables.tf`

```hcl
variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "project_name" {
  description = "Project name for resource naming"
  type        = string
  default     = "mtls-lab"
}

variable "domain_name" {
  description = "Your registered domain (e.g., example.com)"
  type        = string
}
```

### `main.tf`

```hcl
resource "random_id" "suffix" { byte_length = 4 }
```

### `build.tf`

```hcl
resource "null_resource" "go_build" {
  triggers = { source_hash = filebase64sha256("${path.module}/main.go") }
  provisioner "local-exec" {
    command     = "GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go"
    working_dir = path.module
  }
}

data "archive_file" "function_zip" {
  type        = "zip"
  source_file = "${path.module}/bootstrap"
  output_path = "${path.module}/build/function.zip"
  depends_on  = [null_resource.go_build]
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals { type = "Service"; identifiers = ["lambda.amazonaws.com"] }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.project_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume.json
}

resource "aws_iam_role_policy_attachment" "basic" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/service-role/AWSLambdaBasicExecutionRole"
}
```

### `lambda.tf`

```hcl
resource "aws_lambda_function" "this" {
  function_name    = var.project_name
  filename         = data.archive_file.function_zip.output_path
  source_code_hash = data.archive_file.function_zip.output_base64sha256
  handler          = "bootstrap"
  runtime          = "provided.al2023"
  architectures    = ["arm64"]
  role             = aws_iam_role.this.arn
  timeout          = 10
  depends_on       = [aws_iam_role_policy_attachment.basic, aws_cloudwatch_log_group.this]
}

resource "aws_lambda_permission" "apigw" {
  statement_id  = "AllowAPIGateway"
  action        = "lambda:InvokeFunction"
  function_name = aws_lambda_function.this.function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${aws_api_gateway_rest_api.this.execution_arn}/*/*"
}
```

### `monitoring.tf`

```hcl
resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/lambda/${var.project_name}"
  retention_in_days = 1
}
```

### `api.tf`

```hcl
resource "aws_api_gateway_rest_api" "this" {
  name = "${var.project_name}-api"
}

resource "aws_api_gateway_resource" "hello" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  parent_id   = aws_api_gateway_rest_api.this.root_resource_id
  path_part   = "hello"
}

resource "aws_api_gateway_method" "get_hello" {
  rest_api_id   = aws_api_gateway_rest_api.this.id
  resource_id   = aws_api_gateway_resource.hello.id
  http_method   = "GET"
  authorization = "NONE"
}

resource "aws_api_gateway_integration" "get_hello" {
  rest_api_id             = aws_api_gateway_rest_api.this.id
  resource_id             = aws_api_gateway_resource.hello.id
  http_method             = aws_api_gateway_method.get_hello.http_method
  integration_http_method = "POST"
  type                    = "AWS_PROXY"
  uri                     = aws_lambda_function.this.invoke_arn
}

resource "aws_api_gateway_deployment" "this" {
  rest_api_id = aws_api_gateway_rest_api.this.id
  triggers    = { redeployment = sha1(jsonencode([
    aws_api_gateway_resource.hello.id,
    aws_api_gateway_method.get_hello.id,
    aws_api_gateway_integration.get_hello.id,
  ])) }
  lifecycle { create_before_destroy = true }
  depends_on = [aws_api_gateway_integration.get_hello]
}

resource "aws_api_gateway_stage" "dev" {
  deployment_id = aws_api_gateway_deployment.this.id
  rest_api_id   = aws_api_gateway_rest_api.this.id
  stage_name    = "dev"
}

resource "aws_api_gateway_domain_name" "this" {
  domain_name              = "api.${var.domain_name}"
  regional_certificate_arn = aws_acm_certificate_validation.this.certificate_arn

  mutual_tls_authentication {
    truststore_uri = "s3://${aws_s3_bucket.truststore.id}/${aws_s3_object.truststore.key}"
  }

  endpoint_configuration {
    types = ["REGIONAL"]
  }
}

resource "aws_api_gateway_base_path_mapping" "this" {
  api_id      = aws_api_gateway_rest_api.this.id
  stage_name  = aws_api_gateway_stage.dev.stage_name
  domain_name = aws_api_gateway_domain_name.this.domain_name
}
```

### `storage.tf`

```hcl
resource "aws_s3_bucket" "truststore" {
  bucket        = "${var.project_name}-truststore-${random_id.suffix.hex}"
  force_destroy = true
}

resource "aws_s3_object" "truststore" {
  bucket = aws_s3_bucket.truststore.id
  key    = "truststore.pem"
  source = "${path.module}/certs/ca.crt"
  etag   = filemd5("${path.module}/certs/ca.crt")
}
```

### `dns.tf`

```hcl
data "aws_route53_zone" "this" {
  name = var.domain_name
}

resource "aws_acm_certificate" "this" {
  domain_name       = "api.${var.domain_name}"
  validation_method = "DNS"
}

resource "aws_route53_record" "cert_validation" {
  for_each = {
    for dvo in aws_acm_certificate.this.domain_validation_options : dvo.domain_name => {
      name   = dvo.resource_record_name
      record = dvo.resource_record_value
      type   = dvo.resource_record_type
    }
  }
  zone_id = data.aws_route53_zone.this.zone_id
  name    = each.value.name
  type    = each.value.type
  ttl     = 60
  records = [each.value.record]
}

resource "aws_acm_certificate_validation" "this" {
  certificate_arn         = aws_acm_certificate.this.arn
  validation_record_fqdns = [for record in aws_route53_record.cert_validation : record.fqdn]
}

resource "aws_route53_record" "api" {
  zone_id = data.aws_route53_zone.this.zone_id
  name    = "api.${var.domain_name}"
  type    = "A"
  alias {
    name                   = aws_api_gateway_domain_name.this.regional_domain_name
    zone_id                = aws_api_gateway_domain_name.this.regional_zone_id
    evaluate_target_health = false
  }
}
```

### `outputs.tf`

```hcl
output "api_domain" { value = "https://api.${var.domain_name}/hello" }
output "truststore_uri" { value = "s3://${aws_s3_bucket.truststore.id}/${aws_s3_object.truststore.key}" }
```

### Testing

```bash
# Build and deploy
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve

# With client certificate (success)
curl -s --cert certs/client.crt --key certs/client.key \
  "https://api.example.com/hello" | jq .

# Without client certificate (403)
curl -s "https://api.example.com/hello"
```

</details>

## Verify What You Learned

```bash
# Verify truststore is in S3
aws s3 ls s3://$(terraform output -raw truststore_bucket)/

# Verify mTLS is enabled on the domain
aws apigateway get-domain-name --domain-name api.example.com \
  --query "mutualTlsAuthentication" --output json

# Verify certificate chain
openssl verify -CAfile certs/ca.crt certs/client.crt

# Test with valid cert
curl -s --cert certs/client.crt --key certs/client.key \
  "https://api.example.com/hello" | jq .

# Test without cert (expect 403)
curl -s -o /dev/null -w "%{http_code}" "https://api.example.com/hello"
```

## Cleanup

```bash
terraform destroy -auto-approve
terraform state list
```

Expected: no output (empty state).

## What's Next

You configured mutual TLS authentication with a certificate chain and S3 truststore. In the next exercise, you will build **private REST APIs** accessible only through VPC endpoints -- combining VPC interface endpoints, resource policies, and DNS resolution.

## Summary

- **Mutual TLS** requires both server and client to present certificates during the TLS handshake -- the server proves its identity with an ACM certificate, the client proves its identity with a certificate signed by a trusted CA
- The **truststore** is a PEM file in S3 containing CA certificate(s), not client certificates -- API Gateway verifies client certificates against these CA certificates
- mTLS requires a **custom domain name** -- it does not work with the default `execute-api` endpoint
- Client certificate information (subject, issuer, serial number, validity) is available in `requestContext.identity.clientCert` in proxy integration
- Common failure: uploading the client certificate instead of the CA certificate to the truststore causes all requests to be rejected with 403
- The truststore PEM file can contain multiple CA certificates (concatenated) to trust certificates from different CAs

## Reference

- [API Gateway Mutual TLS](https://docs.aws.amazon.com/apigateway/latest/developerguide/rest-api-mutual-tls.html)
- [Terraform aws_api_gateway_domain_name](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/api_gateway_domain_name)
- [ACM Certificate Management](https://docs.aws.amazon.com/acm/latest/userguide/gs.html)
- [OpenSSL Certificate Authority Guide](https://jamielinux.com/docs/openssl-certificate-authority/)

## Additional Resources

- [Configuring Mutual TLS for REST APIs](https://docs.aws.amazon.com/apigateway/latest/developerguide/rest-api-mutual-tls.html) -- complete AWS guide for mTLS configuration
- [Troubleshooting mTLS](https://docs.aws.amazon.com/apigateway/latest/developerguide/rest-api-mutual-tls.html#rest-api-mutual-tls-troubleshooting) -- common certificate and truststore issues
- [Certificate Chain of Trust](https://knowledge.digicert.com/solution/how-certificate-chains-work) -- understanding root CA, intermediate CA, and end-entity certificates
- [X.509 Certificate Format](https://www.ssl.com/faqs/what-is-an-x-509-certificate/) -- PEM vs DER encoding and certificate fields
