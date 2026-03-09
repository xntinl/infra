# 59. KMS Envelope Encryption with Data Keys

<!--
difficulty: intermediate
concepts: [kms-envelope-encryption, generate-data-key, aes-gcm, plaintext-data-key, encrypted-data-key, ciphertext-blob, data-at-rest-encryption]
tools: [terraform, aws-cli]
estimated_time: 45m
bloom_level: apply, analyze
prerequisites: [58-kms-key-types-symmetric-asymmetric]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates a KMS customer-managed key ($1.00/month) and a Lambda function. KMS API calls cost $0.03 per 10,000 requests. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

| Requirement | Verify |
|------------|--------|
| Terraform >= 1.7 | `terraform version` |
| AWS CLI configured | `aws sts get-caller-identity` |
| Go 1.21+ installed | `go version` |
| jq installed | `jq --version` |

## Learning Objectives

By the end of this exercise you will be able to:

1. **Explain** why KMS has a 4 KB plaintext limit and how envelope encryption solves it
2. **Implement** a Go Lambda that calls `GenerateDataKey` to obtain a plaintext data key and its encrypted copy
3. **Apply** AES-GCM symmetric encryption using the plaintext data key to encrypt an arbitrary-size payload
4. **Analyze** the security properties of envelope encryption: why the plaintext key must be discarded after use and only the encrypted data key is persisted
5. **Differentiate** between `GenerateDataKey` (returns plaintext + encrypted key) and `GenerateDataKeyWithoutPlaintext` (returns only encrypted key for deferred decryption)

## Why Envelope Encryption

KMS symmetric Encrypt and Decrypt APIs have a hard limit: you can encrypt at most 4 KB of plaintext per call. If you need to encrypt a 10 MB file, a 500 KB JSON payload, or any data larger than 4 KB, you cannot send it directly to KMS. Envelope encryption solves this.

The pattern works in three steps: (1) call `GenerateDataKey` -- KMS returns a plaintext data key and the same key encrypted under your KMS key (the "envelope"); (2) use the plaintext data key locally to encrypt your data with a standard symmetric algorithm like AES-GCM; (3) store the encrypted data alongside the encrypted data key, then **discard the plaintext data key from memory**. To decrypt later, send the encrypted data key to KMS to recover the plaintext key, then decrypt the data locally.

This pattern is the foundation of how S3 SSE-KMS, EBS encryption, and the AWS Encryption SDK work. The DVA-C02 exam tests this pattern frequently. A common exam trap is a developer who stores the plaintext data key alongside the encrypted data -- this defeats the entire purpose because anyone with file system access can decrypt the data without calling KMS.

## Building Blocks

Create the following files in your exercise directory. Your job is to fill in each `# TODO` block.

### `lambda/main.go`

This Lambda receives a payload, encrypts it using envelope encryption, and stores both the encrypted data and the encrypted data key in DynamoDB.

```go
package main

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/aws/aws-sdk-go-v2/service/kms"
	"github.com/aws/aws-sdk-go-v2/service/kms/types"
)

var (
	kmsClient *kms.Client
	ddbClient *dynamodb.Client
	kmsKeyID  string
	tableName string
)

func init() {
	cfg, _ := config.LoadDefaultConfig(context.TODO())
	kmsClient = kms.NewFromConfig(cfg)
	ddbClient = dynamodb.NewFromConfig(cfg)
	kmsKeyID = os.Getenv("KMS_KEY_ID")
	tableName = os.Getenv("TABLE_NAME")
}

type EncryptRequest struct {
	RecordID string `json:"record_id"`
	Payload  string `json:"payload"`
}

type EncryptResponse struct {
	RecordID           string `json:"record_id"`
	EncryptedDataB64   string `json:"encrypted_data_b64"`
	EncryptedKeyB64    string `json:"encrypted_key_b64"`
	Message            string `json:"message"`
}

func handler(ctx context.Context, req EncryptRequest) (*EncryptResponse, error) {
	// =====================================================
	// TODO 1 -- Generate a data key using KMS
	// =====================================================
	// Call kmsClient.GenerateDataKey with:
	//   - KeyId: kmsKeyID
	//   - KeySpec: types.DataKeySpecAes256
	// The response contains:
	//   - Plaintext: the plaintext data key ([]byte)
	//   - CiphertextBlob: the encrypted data key ([]byte)
	//
	// Docs: https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/kms#Client.GenerateDataKey


	// =====================================================
	// TODO 2 -- Encrypt the payload with AES-GCM
	// =====================================================
	// Use the plaintext data key from TODO 1 to:
	//   1. Create an AES cipher block: aes.NewCipher(plaintextKey)
	//   2. Create a GCM wrapper: cipher.NewGCM(block)
	//   3. Generate a random nonce: make([]byte, gcm.NonceSize())
	//   4. Seal the plaintext: gcm.Seal(nonce, nonce, payload, nil)
	//      (prepending the nonce to the ciphertext)
	//
	// Docs: https://pkg.go.dev/crypto/aes, https://pkg.go.dev/crypto/cipher


	// =====================================================
	// TODO 3 -- Store encrypted data + encrypted key in DynamoDB
	// =====================================================
	// Store three items:
	//   - record_id (S): req.RecordID
	//   - encrypted_data (S): base64-encoded ciphertext from TODO 2
	//   - encrypted_key (S): base64-encoded CiphertextBlob from TODO 1
	//
	// IMPORTANT: Do NOT store the plaintext data key.
	// After this point, discard the plaintext key from memory.
	//
	// Docs: https://pkg.go.dev/github.com/aws/aws-sdk-go-v2/service/dynamodb#Client.PutItem


	// =====================================================
	// TODO 4 -- Zero out the plaintext key
	// =====================================================
	// Overwrite the plaintext key bytes with zeros to remove
	// sensitive material from memory:
	//   for i := range plaintextKey { plaintextKey[i] = 0 }


	return &EncryptResponse{
		RecordID:         req.RecordID,
		EncryptedDataB64: "REPLACE_WITH_BASE64_CIPHERTEXT",
		EncryptedKeyB64:  "REPLACE_WITH_BASE64_ENCRYPTED_KEY",
		Message:          "Payload encrypted with envelope encryption",
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
  description = "Project name for resource naming and tagging"
  type        = string
  default     = "envelope-encryption-demo"
}
```

### `kms.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_kms_key" "this" {
  description            = "Envelope encryption demo key"
  key_usage              = "ENCRYPT_DECRYPT"
  deletion_window_in_days = 7
  enable_key_rotation    = true

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { AWS = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root" }
      Action    = "kms:*"
      Resource  = "*"
    }]
  })
}

resource "aws_kms_alias" "this" {
  name          = "alias/${var.project_name}"
  target_key_id = aws_kms_key.this.key_id
}
```

### `database.tf`

```hcl
resource "aws_dynamodb_table" "encrypted_records" {
  name         = "${var.project_name}-records"
  billing_mode = "PAY_PER_REQUEST"
  hash_key     = "record_id"

  attribute {
    name = "record_id"
    type = "S"
  }
}
```

### `lambda.tf`

Standard Go Lambda build + IAM (see Exercise 01 for detailed pattern). Key IAM permissions: `kms:GenerateDataKey` and `kms:Decrypt` on the KMS key, `dynamodb:PutItem` and `dynamodb:GetItem` on the table. Lambda: `runtime = "provided.al2023"`, `handler = "bootstrap"`, `architectures = ["arm64"]`. Environment variables: `KMS_KEY_ID`, `TABLE_NAME`.

```hcl
# IAM role with AWSLambdaBasicExecutionRole + inline policy for KMS and DynamoDB
# Lambda function referencing the Go build output
```

### `outputs.tf`

```hcl
output "function_name" { value = aws_lambda_function.this.function_name }
output "table_name"    { value = aws_dynamodb_table.encrypted_records.name }
output "kms_key_id"    { value = aws_kms_key.this.key_id }
```

## Spot the Bug

A developer implements envelope encryption but stores the plaintext data key in DynamoDB alongside the encrypted data:

```go
func handler(ctx context.Context, req EncryptRequest) error {
    dataKeyOutput, _ := kmsClient.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
        KeyId:   aws.String(kmsKeyID),
        KeySpec: types.DataKeySpecAes256,
    })

    // Encrypt payload with plaintext key
    ciphertext := encryptAESGCM(dataKeyOutput.Plaintext, []byte(req.Payload))

    // Store everything in DynamoDB
    _, err := ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
        TableName: aws.String(tableName),
        Item: map[string]dtypes.AttributeValue{
            "record_id":      &dtypes.AttributeValueMemberS{Value: req.RecordID},
            "encrypted_data": &dtypes.AttributeValueMemberS{Value: base64.StdEncoding.EncodeToString(ciphertext)},
            "encrypted_key":  &dtypes.AttributeValueMemberS{Value: base64.StdEncoding.EncodeToString(dataKeyOutput.CiphertextBlob)},
            "plaintext_key":  &dtypes.AttributeValueMemberS{Value: base64.StdEncoding.EncodeToString(dataKeyOutput.Plaintext)},  // <-- BUG
        },
    })
    return err
}
```

<details>
<summary>Explain the bug</summary>

The developer stores the **plaintext data key** (`dataKeyOutput.Plaintext`) in DynamoDB alongside the encrypted data. This completely defeats the purpose of envelope encryption.

The security model of envelope encryption depends on a critical property: the plaintext data key exists only in memory for the brief moment needed to encrypt the data, then is discarded. To decrypt later, you must call `kms:Decrypt` with the encrypted data key, which requires IAM permissions on the KMS key. By storing the plaintext key in DynamoDB, anyone with DynamoDB read access can decrypt the data without any KMS permissions.

**Fix -- store only the encrypted data key, never the plaintext key:**

```go
_, err := ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
    TableName: aws.String(tableName),
    Item: map[string]dtypes.AttributeValue{
        "record_id":      &dtypes.AttributeValueMemberS{Value: req.RecordID},
        "encrypted_data": &dtypes.AttributeValueMemberS{Value: base64.StdEncoding.EncodeToString(ciphertext)},
        "encrypted_key":  &dtypes.AttributeValueMemberS{Value: base64.StdEncoding.EncodeToString(dataKeyOutput.CiphertextBlob)},
    },
})

// Zero out the plaintext key in memory
for i := range dataKeyOutput.Plaintext {
    dataKeyOutput.Plaintext[i] = 0
}
```

</details>

## Solutions

<details>
<summary>TODO 1 -- Generate a data key using KMS</summary>

```go
dataKeyOutput, err := kmsClient.GenerateDataKey(ctx, &kms.GenerateDataKeyInput{
    KeyId:   aws.String(kmsKeyID),
    KeySpec: types.DataKeySpecAes256,
})
if err != nil {
    return nil, fmt.Errorf("failed to generate data key: %w", err)
}

plaintextKey := dataKeyOutput.Plaintext
encryptedKey := dataKeyOutput.CiphertextBlob
```

</details>

<details>
<summary>TODO 2 -- Encrypt the payload with AES-GCM</summary>

```go
block, err := aes.NewCipher(plaintextKey)
if err != nil {
    return nil, fmt.Errorf("failed to create cipher: %w", err)
}

gcm, err := cipher.NewGCM(block)
if err != nil {
    return nil, fmt.Errorf("failed to create GCM: %w", err)
}

nonce := make([]byte, gcm.NonceSize())
if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
    return nil, fmt.Errorf("failed to generate nonce: %w", err)
}

ciphertext := gcm.Seal(nonce, nonce, []byte(req.Payload), nil)
```

</details>

<details>
<summary>TODO 3 -- Store encrypted data + encrypted key in DynamoDB</summary>

```go
encryptedDataB64 := base64.StdEncoding.EncodeToString(ciphertext)
encryptedKeyB64 := base64.StdEncoding.EncodeToString(encryptedKey)

_, err = ddbClient.PutItem(ctx, &dynamodb.PutItemInput{
    TableName: aws.String(tableName),
    Item: map[string]dtypes.AttributeValue{
        "record_id":      &dtypes.AttributeValueMemberS{Value: req.RecordID},
        "encrypted_data": &dtypes.AttributeValueMemberS{Value: encryptedDataB64},
        "encrypted_key":  &dtypes.AttributeValueMemberS{Value: encryptedKeyB64},
    },
})
if err != nil {
    return nil, fmt.Errorf("failed to store record: %w", err)
}
```

</details>

<details>
<summary>TODO 4 -- Zero out the plaintext key</summary>

```go
for i := range plaintextKey {
    plaintextKey[i] = 0
}
```

</details>

## Verify What You Learned

### Step 1 -- Apply the infrastructure

```bash
GOOS=linux GOARCH=arm64 go build -tags lambda.norpc -o bootstrap main.go
terraform init && terraform apply -auto-approve
```

### Step 2 -- Encrypt a payload

```bash
aws lambda invoke --function-name $(terraform output -raw function_name) \
  --payload '{"record_id": "order-001", "payload": "Sensitive customer data: SSN 123-45-6789, CC 4111-1111-1111-1111"}' \
  /dev/stdout 2>/dev/null | jq .
```

Expected: response showing `record_id`, base64-encoded encrypted data, and base64-encoded encrypted key.

### Step 3 -- Verify DynamoDB has encrypted data and encrypted key only

```bash
aws dynamodb get-item \
  --table-name $(terraform output -raw table_name) \
  --key '{"record_id": {"S": "order-001"}}' \
  --query "Item" --output json | jq .
```

Expected: `record_id`, `encrypted_data`, and `encrypted_key` -- no `plaintext_key` attribute.

### Step 4 -- Verify KMS key has correct configuration

```bash
aws kms describe-key --key-id $(terraform output -raw kms_key_id) \
  --query "KeyMetadata.{Usage:KeyUsage,Spec:CustomerMasterKeySpec,Rotation:KeyRotationStatus}" \
  --output json
```

Expected: `ENCRYPT_DECRYPT`, `SYMMETRIC_DEFAULT`.

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state). The KMS key enters a 7-day pending deletion window.

## What's Next

You implemented envelope encryption using KMS `GenerateDataKey` and AES-GCM. In the next exercise, you will explore **IAM policy evaluation logic** -- how AWS evaluates overlapping identity-based policies, resource-based policies, SCPs, and permission boundaries to determine whether an API call is allowed or denied.

## Summary

- **Envelope encryption** solves the KMS 4 KB plaintext limit by encrypting data locally with a data key
- `GenerateDataKey` returns both a **plaintext key** (for immediate encryption) and an **encrypted key** (for storage)
- `GenerateDataKeyWithoutPlaintext` returns only the encrypted key -- useful when you need the key later but not now
- The plaintext data key must be **discarded from memory** after encryption -- never store it alongside the data
- **AES-GCM** provides authenticated encryption: both confidentiality and integrity in a single operation
- The encrypted data key is stored alongside the ciphertext -- to decrypt, call `kms:Decrypt` on the encrypted key first
- This pattern is how **S3 SSE-KMS**, **EBS encryption**, and the **AWS Encryption SDK** work internally
- IAM permissions needed: `kms:GenerateDataKey` for encryption, `kms:Decrypt` for decryption

## Reference

- [KMS Envelope Encryption](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#enveloping)
- [GenerateDataKey API](https://docs.aws.amazon.com/kms/latest/APIReference/API_GenerateDataKey.html)
- [Terraform aws_kms_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key)
- [Go crypto/aes Package](https://pkg.go.dev/crypto/aes)

## Additional Resources

- [AWS Encryption SDK](https://docs.aws.amazon.com/encryption-sdk/latest/developer-guide/introduction.html) -- library that implements envelope encryption with key rotation and multi-key support
- [S3 Server-Side Encryption with KMS](https://docs.aws.amazon.com/AmazonS3/latest/userguide/UsingKMSEncryption.html) -- how S3 uses envelope encryption internally
- [Data Key Caching](https://docs.aws.amazon.com/encryption-sdk/latest/developer-guide/data-key-caching.html) -- reuse data keys to reduce KMS API calls
