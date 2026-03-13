# 58. KMS Key Types: Symmetric and Asymmetric

<!--
difficulty: basic
concepts: [kms-symmetric-key, kms-asymmetric-key, encrypt-decrypt, sign-verify, key-policy, key-spec, key-usage]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: apply
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise creates two KMS keys ($1.00/month each for customer-managed keys). Cryptographic operations cost $0.03 per 10,000 requests. Total cost is approximately $0.01/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Go 1.21+ installed locally
- base64 and openssl CLI tools available

## Learning Objectives

After completing this exercise, you will be able to:

- **Construct** KMS symmetric and asymmetric keys with appropriate key specs using Terraform
- **Demonstrate** encrypt/decrypt operations with a symmetric KMS key using the AWS CLI
- **Demonstrate** sign/verify operations with an asymmetric KMS key using the AWS CLI
- **Explain** why symmetric keys cannot be used for sign/verify and asymmetric keys have different usage restrictions
- **Describe** how KMS key policies control which principals can use a key for specific operations

## Why KMS Key Types

AWS KMS manages cryptographic keys in hardware security modules (HSMs). KMS supports two key types, each with different use cases.

**Symmetric keys** (AES-256) use the same key for encryption and decryption. The key material never leaves KMS -- you send plaintext to KMS, get ciphertext back, and later send ciphertext to KMS to get plaintext. This is the default key type and covers most encryption needs: S3 server-side encryption, EBS volume encryption, RDS encryption at rest, and envelope encryption in application code.

**Asymmetric keys** use a public/private key pair. The private key stays in KMS, while the public key can be downloaded. For **sign/verify** (RSA_2048, ECC_NIST_P256), you sign data with the private key via KMS and anyone with the public key can verify the signature -- useful for code signing, JWT tokens, and document integrity. For **encrypt/decrypt** (RSA_2048, RSA_4096), external parties encrypt with the public key and only KMS can decrypt with the private key -- useful when external systems need to send you encrypted data without access to your AWS account.

The DVA-C02 exam tests key type selection. Key traps: (1) a symmetric key with `key_usage = ENCRYPT_DECRYPT` cannot perform sign/verify -- you need an asymmetric key; (2) asymmetric signing keys cannot be used for encryption; (3) KMS symmetric encryption wraps your data with an envelope key -- for data larger than 4 KB, use `GenerateDataKey` for envelope encryption.

## Step 1 -- Create the Terraform Configuration

Create the following files in your exercise directory:

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = { source = "hashicorp/aws", version = "~> 5.0" }
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
  default     = "kms-demo"
}
```

### `main.tf`

```hcl
data "aws_caller_identity" "current" {}

locals {
  root_arn = "arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"
}

resource "aws_kms_key" "symmetric" {
  description              = "Symmetric encryption key"
  key_usage                = "ENCRYPT_DECRYPT"
  customer_master_key_spec = "SYMMETRIC_DEFAULT"
  deletion_window_in_days  = 7
  enable_key_rotation      = true

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"; Principal = { AWS = local.root_arn }
      Action = "kms:*"; Resource = "*"
    }]
  })
}

resource "aws_kms_alias" "symmetric" {
  name = "alias/${var.project_name}-symmetric"; target_key_id = aws_kms_key.symmetric.key_id
}

resource "aws_kms_key" "asymmetric" {
  description              = "Asymmetric signing key"
  key_usage                = "SIGN_VERIFY"
  customer_master_key_spec = "RSA_2048"
  deletion_window_in_days  = 7
  enable_key_rotation      = false  # Not supported for asymmetric keys

  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect = "Allow"; Principal = { AWS = local.root_arn }
      Action = "kms:*"; Resource = "*"
    }]
  })
}

resource "aws_kms_alias" "asymmetric" {
  name = "alias/${var.project_name}-asymmetric"; target_key_id = aws_kms_key.asymmetric.key_id
}
```

### `outputs.tf`

```hcl
output "symmetric_key_id"  { value = aws_kms_key.symmetric.key_id }
output "asymmetric_key_id" { value = aws_kms_key.asymmetric.key_id }
```

## Step 2 -- Deploy

```bash
terraform init
terraform apply -auto-approve
```

## Step 3 -- Symmetric Key: Encrypt and Decrypt

Encrypt a plaintext string:

```bash
SYM_KEY_ID=$(terraform output -raw symmetric_key_id)

# Encrypt
CIPHERTEXT=$(aws kms encrypt \
  --key-id "$SYM_KEY_ID" \
  --plaintext "Hello, KMS symmetric encryption!" \
  --query "CiphertextBlob" --output text)

echo "Ciphertext (base64): $CIPHERTEXT"
```

Decrypt the ciphertext:

```bash
# Decrypt -- KMS automatically identifies the correct key from the ciphertext metadata
PLAINTEXT=$(aws kms decrypt \
  --ciphertext-blob "$CIPHERTEXT" \
  --query "Plaintext" --output text | base64 --decode)

echo "Decrypted: $PLAINTEXT"
```

Expected: `Hello, KMS symmetric encryption!`

Note: `kms decrypt` does not require `--key-id` because the ciphertext blob includes metadata identifying the key. This is how envelope encryption works -- the encrypted data key carries its own key reference.

## Step 4 -- Asymmetric Key: Sign and Verify

Sign a message:

```bash
ASYM_KEY_ID=$(terraform output -raw asymmetric_key_id)
MESSAGE="This message is authentic"

# Create a message digest (SHA-256)
MESSAGE_BASE64=$(echo -n "$MESSAGE" | base64)

# Sign the message
SIGNATURE=$(aws kms sign \
  --key-id "$ASYM_KEY_ID" \
  --message "$MESSAGE_BASE64" \
  --message-type RAW \
  --signing-algorithm RSASSA_PKCS1_V1_5_SHA_256 \
  --query "Signature" --output text)

echo "Signature (base64): ${SIGNATURE:0:40}..."
```

Verify the signature:

```bash
# Verify -- returns SignatureValid: true/false
aws kms verify \
  --key-id "$ASYM_KEY_ID" \
  --message "$MESSAGE_BASE64" \
  --message-type RAW \
  --signing-algorithm RSASSA_PKCS1_V1_5_SHA_256 \
  --signature "$SIGNATURE" \
  --query "SignatureValid" --output text
```

Expected: `True`

Download the public key for offline verification:

```bash
# Get the public key (can be shared externally)
aws kms get-public-key --key-id "$ASYM_KEY_ID" \
  --query "PublicKey" --output text | base64 --decode > public_key.der

echo "Public key saved to public_key.der"
```

## Step 5 -- Verify Key Type Restrictions

Attempt to use a symmetric key for signing (this will fail):

```bash
aws kms sign \
  --key-id "$SYM_KEY_ID" \
  --message "$(echo -n 'test' | base64)" \
  --message-type RAW \
  --signing-algorithm RSASSA_PKCS1_V1_5_SHA_256 2>&1 | head -3
```

Expected error: `UnsupportedOperationException` -- symmetric keys do not support sign/verify.

Attempt to use a signing key for encryption (this will fail):

```bash
aws kms encrypt \
  --key-id "$ASYM_KEY_ID" \
  --plaintext "test" 2>&1 | head -3
```

Expected error: `UnsupportedOperationException` -- this asymmetric key is configured for SIGN_VERIFY, not ENCRYPT_DECRYPT.

## Common Mistakes

### 1. Enabling key rotation on asymmetric keys

Automatic key rotation is only supported for symmetric keys. Setting `enable_key_rotation = true` on an asymmetric key causes an error. **Fix:** Set `enable_key_rotation = false` for asymmetric keys.

### 2. Using Encrypt/Decrypt for data larger than 4 KB

KMS symmetric Encrypt API has a 4 KB plaintext limit. For larger data, use envelope encryption: call `GenerateDataKey` to get a plaintext data key + encrypted data key, encrypt your data locally with the plaintext key, store the encrypted data key alongside the ciphertext, and discard the plaintext key from memory.

### 3. Forgetting the key policy root access statement

Without the root account statement (`kms:*` for the account root principal), no IAM policies can grant access to the key. The key becomes unmanageable.

## Verify What You Learned

```bash
# Verify both keys exist with correct usage
aws kms describe-key --key-id $(terraform output -raw symmetric_key_id) \
  --query "KeyMetadata.{Usage:KeyUsage,Spec:CustomerMasterKeySpec}" --output json
# Expected: KeyUsage=ENCRYPT_DECRYPT, Spec=SYMMETRIC_DEFAULT

aws kms describe-key --key-id $(terraform output -raw asymmetric_key_id) \
  --query "KeyMetadata.{Usage:KeyUsage,Spec:CustomerMasterKeySpec}" --output json
# Expected: KeyUsage=SIGN_VERIFY, Spec=RSA_2048

# Round-trip encrypt/decrypt test
ENCRYPTED=$(aws kms encrypt --key-id $(terraform output -raw symmetric_key_id) \
  --plaintext "verify-test" --query "CiphertextBlob" --output text)
aws kms decrypt --ciphertext-blob "$ENCRYPTED" \
  --query "Plaintext" --output text | base64 --decode
# Expected: verify-test
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
rm -f public_key.der
```

Verify nothing remains:

```bash
terraform state list
```

Expected: no output (empty state). Note: KMS keys enter a 7-day pending deletion window before permanent destruction.

## What's Next

You created symmetric and asymmetric KMS keys and demonstrated encrypt/decrypt and sign/verify operations. These are foundational encryption concepts that appear throughout AWS services. Future exercises will build on KMS with S3 server-side encryption, EBS encryption, and envelope encryption patterns.

## Summary

- **Symmetric keys** (SYMMETRIC_DEFAULT / AES-256) support encrypt/decrypt -- the key never leaves KMS
- **Asymmetric keys** support either sign/verify or encrypt/decrypt -- not both on the same key
- Asymmetric **signing keys** (RSA, ECC) sign with the private key in KMS; anyone with the public key can verify
- Asymmetric **encryption keys** (RSA) let external parties encrypt with the public key; only KMS can decrypt
- **Automatic key rotation** is only supported for symmetric keys (new key material every year, old material retained for decryption)
- KMS Encrypt API has a **4 KB plaintext limit** -- use `GenerateDataKey` for envelope encryption of larger data
- **Key policies** control access -- the root account statement must be present for IAM policies to work
- Customer-managed keys cost **$1.00/month** each, plus $0.03 per 10,000 API requests

## Reference

- [KMS Key Types](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#key-types)
- [KMS Symmetric Keys](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#symmetric-cmks)
- [KMS Asymmetric Keys](https://docs.aws.amazon.com/kms/latest/developerguide/symmetric-asymmetric.html)
- [Terraform aws_kms_key](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/kms_key)

## Additional Resources

- [Envelope Encryption](https://docs.aws.amazon.com/kms/latest/developerguide/concepts.html#enveloping) -- encrypting data larger than 4 KB
- [KMS Key Policies](https://docs.aws.amazon.com/kms/latest/developerguide/key-policies.html) -- how key policies interact with IAM policies
- [KMS Signing Algorithms](https://docs.aws.amazon.com/kms/latest/developerguide/asymmetric-key-specs.html) -- RSA and ECC key specs and supported algorithms
