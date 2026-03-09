# 10. Hybrid Storage with Storage Gateway Patterns

<!--
difficulty: advanced
concepts: [storage-gateway, file-gateway, volume-gateway, nfs, s3-integration, hybrid-cloud, cached-volumes, stored-volumes]
tools: [terraform, aws-cli]
estimated_time: 75m
bloom_level: evaluate
prerequisites: [none]
aws_cost: ~$0.15/hr
-->

> **AWS Cost Warning:** EC2 instance running Storage Gateway appliance (~$0.0116/hr for t3.xlarge) plus S3 storage and EBS cache volumes. Total ~$0.15/hr. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Completed exercises 01-09 or equivalent knowledge
- Understanding of NFS protocol basics
- Familiarity with S3 storage classes

## Learning Objectives

After completing this exercise, you will be able to:

- **Evaluate** which Storage Gateway type (File, Volume Cached, Volume Stored, Tape) best fits a given hybrid storage scenario
- **Design** a File Gateway architecture that presents S3 buckets as NFS shares to on-premises applications
- **Implement** an EC2-based Storage Gateway with cache disk provisioning and NFS share configuration
- **Analyze** cache hit/miss behavior and its impact on read latency when the local cache is undersized

## Why This Matters

Most enterprises do not migrate to the cloud overnight. They run hybrid architectures where on-premises applications need to access cloud storage without being rewritten. Storage Gateway bridges this gap by presenting cloud-backed storage using standard protocols (NFS, SMB, iSCSI) that existing applications already understand. The SAA-C03 exam heavily tests your ability to choose the correct gateway type for a given scenario. File Gateway maps NFS/SMB shares to S3 objects -- each file becomes an S3 object with the same name. Volume Gateway provides iSCSI block storage backed by S3 snapshots, with two sub-modes that trade off local capacity against cloud dependency. Getting this choice wrong on the exam -- or in production -- means either unacceptable latency, unnecessary cost, or both.

## The Challenge

You are an architect advising a company with a 50TB on-premises file server. The server holds document archives accessed primarily for reads, with occasional new document uploads. The company wants cloud backup with continued local read access for frequently accessed files. Design and implement the appropriate Storage Gateway solution.

### Requirements

1. Deploy an EC2 instance running the Storage Gateway appliance (simulating an on-premises gateway)
2. Configure the gateway in File Gateway mode with an NFS file share backed by an S3 bucket
3. Provision appropriate cache and upload buffer disks
4. Write test files via the NFS share and verify they appear as S3 objects
5. Demonstrate the relationship between cache size and read performance
6. Document your reasoning for choosing File Gateway over Volume Gateway for this scenario

### Architecture

```
On-Premises (simulated by EC2)              AWS Cloud
+----------------------------------+        +---------------------------+
|                                  |        |                           |
|  +----------------------------+  |        |  +---------------------+  |
|  | Application Server (EC2)   |  |        |  |    S3 Bucket        |  |
|  |                            |  |        |  |  (storage backend)  |  |
|  |  mount -t nfs              |  |        |  |                     |  |
|  |  gw-ip:/share /mnt/docs   |-----NFS---->  | file.txt = object  |  |
|  +----------------------------+  |        |  +---------------------+  |
|                                  |        |           ^               |
|  +----------------------------+  |        |           |               |
|  | Storage Gateway (EC2)      |  |        |     HTTPS upload         |
|  |  - File Gateway mode       |  |        |           |               |
|  |  - Cache disk (150 GiB)    |--+--------+-----------+               |
|  |  - Upload buffer (150 GiB) |  |        |                           |
|  +----------------------------+  |        |  +---------------------+  |
|                                  |        |  | CloudWatch Metrics  |  |
+----------------------------------+        |  |  - CacheHitPercent  |  |
                                            |  |  - CacheUsed        |  |
                                            |  +---------------------+  |
                                            +---------------------------+
```

## Hints

<details>
<summary>Hint 1: Choosing the Right Gateway Type</summary>

The scenario describes a **file server** with document archives. Applications access files by name and path. This maps directly to File Gateway because:

- File Gateway presents S3 as NFS/SMB shares -- each file becomes an S3 object
- Applications continue using standard file operations (open, read, write, list)
- No application changes needed
- S3 lifecycle policies can tier old documents to S3 Glacier automatically

Volume Gateway would require applications to use iSCSI block storage, which means reformatting the data access pattern. Tape Gateway is for backup applications that use tape-based workflows.

The decision matrix:

| Gateway Type | Protocol | Backend | Use Case |
|---|---|---|---|
| File Gateway | NFS / SMB | S3 objects | File shares, archives, data lakes |
| Volume Cached | iSCSI | S3 + local cache | Block storage, databases, frequently accessed subsets |
| Volume Stored | iSCSI | Local + S3 snapshots | Block storage, full dataset local, async backup |
| Tape Gateway | iSCSI VTL | S3 Glacier | Backup apps (Veeam, NetBackup, Commvault) |

</details>

<details>
<summary>Hint 2: Storage Gateway EC2 Deployment</summary>

When deploying Storage Gateway on EC2 (to simulate an on-premises appliance), use the official Storage Gateway AMI. The gateway requires:

- Instance type: `m5.xlarge` or `m6i.xlarge` minimum (4 vCPU, 16 GiB RAM)
- Cache disk: EBS volume, minimum 150 GiB, recommended gp3
- Upload buffer: separate EBS volume, minimum 150 GiB
- Network: must be able to reach S3 endpoints and Storage Gateway service endpoints

```hcl
data "aws_ssm_parameter" "sgw_ami" {
  name = "/aws/service/storagegateway/ami/FILE_S3/latest"
}

resource "aws_instance" "gateway" {
  ami           = data.aws_ssm_parameter.sgw_ami.value
  instance_type = "m5.xlarge"
  # ...
}
```

The cache disk stores recently accessed data locally. The upload buffer holds data waiting to be uploaded to S3. Both must be separate EBS volumes attached to the gateway instance -- do NOT use the root volume.

</details>

<details>
<summary>Hint 3: Activating the Gateway and Creating NFS Shares</summary>

After the EC2 instance launches, you must activate the gateway via the AWS CLI:

```bash
# Get activation key (gateway must be reachable on port 80)
ACTIVATION_KEY=$(curl -s "http://${GATEWAY_IP}/?activationRegion=us-east-1&no_redirect")

# Activate gateway
aws storagegateway activate-gateway \
  --activation-key "$ACTIVATION_KEY" \
  --gateway-name "file-gateway-demo" \
  --gateway-timezone "GMT" \
  --gateway-type "FILE_S3" \
  --gateway-region "us-east-1"
```

Then add the local disks as cache and upload buffer:

```bash
# List local disks on the gateway
aws storagegateway list-local-disks --gateway-arn "$GATEWAY_ARN"

# Add cache disk
aws storagegateway add-cache \
  --gateway-arn "$GATEWAY_ARN" \
  --disk-ids "$CACHE_DISK_ID"

# Create NFS file share
aws storagegateway create-nfs-file-share \
  --client-token "demo-share" \
  --gateway-arn "$GATEWAY_ARN" \
  --role "$IAM_ROLE_ARN" \
  --location-arn "arn:aws:s3:::my-file-gateway-bucket" \
  --default-storage-class "S3_STANDARD"
```

</details>

<details>
<summary>Hint 4: S3 Bucket Configuration and Lifecycle Policies</summary>

The S3 bucket backing the File Gateway should have lifecycle policies aligned with your access patterns:

```hcl
resource "aws_s3_bucket" "file_gateway" {
  bucket = "file-gateway-demo-${data.aws_caller_identity.current.account_id}"
}

resource "aws_s3_bucket_lifecycle_configuration" "file_gateway" {
  bucket = aws_s3_bucket.file_gateway.id

  rule {
    id     = "archive-old-documents"
    status = "Enabled"

    transition {
      days          = 90
      storage_class = "STANDARD_IA"
    }

    transition {
      days          = 365
      storage_class = "GLACIER"
    }
  }
}
```

When a file is transitioned to Glacier, the File Gateway cache may still have the file locally (fast reads). But if the cache evicts that file, reading it requires a Glacier restore -- which takes hours. Plan your cache size and lifecycle rules together.

</details>

<details>
<summary>Hint 5: Cache Sizing and Monitoring</summary>

The cache disk determines how much recently accessed data stays local. Monitor cache effectiveness with CloudWatch:

```bash
# Check cache hit percentage
aws cloudwatch get-metric-statistics \
  --namespace "AWS/StorageGateway" \
  --metric-name "CacheHitPercent" \
  --dimensions Name=GatewayId,Value="$GATEWAY_ID" \
  --start-time "$(date -u -v-1H +%Y-%m-%dT%H:%M:%S)" \
  --end-time "$(date -u +%Y-%m-%dT%H:%M:%S)" \
  --period 300 \
  --statistics Average
```

AWS recommends cache size = 20% of total working dataset. For 50TB total data:

- If 20% is actively accessed: cache = 10 TB
- If only 5% is actively accessed: cache = 2.5 TB

Undersized cache leads to frequent evictions and cache misses, which means reads go to S3 (higher latency). The gateway still works -- but reads slow from milliseconds (cache hit) to tens of milliseconds (S3 fetch) or hours (Glacier retrieval).

</details>

## Spot the Bug

A team deploys a File Gateway with a 150 GiB cache disk for a 10 TB dataset. Writes work perfectly. Reads of recently written files are fast. But users report that reading older files is sometimes slow -- taking 50-200ms instead of the expected 1-5ms:

```hcl
resource "aws_ebs_volume" "cache" {
  availability_zone = aws_instance.gateway.availability_zone
  size              = 150   # 150 GiB cache for 10 TB dataset
  type              = "gp3"
}
```

<details>
<summary>Explain the bug</summary>

The cache disk is only 150 GiB for a 10 TB dataset -- that is 1.5% of the total data. AWS recommends sizing the cache at approximately 20% of the actively accessed data. With such a small cache, frequently accessed files are constantly evicted to make room for newly accessed files.

When a user reads a file that has been evicted from cache, the gateway must fetch it from S3 over the network. This adds 50-200ms of latency per read compared to 1-5ms for a cache hit. The gateway still functions correctly -- no errors occur -- but performance degrades silently.

The fix is to increase the cache disk to at least 20% of the working dataset. If 2 TB of the 10 TB is regularly accessed, the cache should be at least 400 GiB:

```hcl
resource "aws_ebs_volume" "cache" {
  availability_zone = aws_instance.gateway.availability_zone
  size              = 400   # 400 GiB cache for ~2 TB working set
  type              = "gp3"
  iops              = 6000  # Higher IOPS for cache performance
}
```

Monitor `CacheHitPercent` in CloudWatch. If it drops below 80%, the cache is undersized for the workload. Also set a CloudWatch alarm to alert when `CachePercentDirty` exceeds 80%, which indicates the upload buffer cannot keep up with writes.

</details>

## Verify What You Learned

Confirm the gateway is active and configured:

```bash
# List gateways
aws storagegateway list-gateways \
  --query "Gateways[?GatewayName=='file-gateway-demo'].{Name:GatewayName,Type:GatewayType,Status:GatewayOperationalState}" \
  --output table
```

Expected: Gateway type `FILE_S3`, status `ACTIVE`.

```bash
# Verify NFS share exists
aws storagegateway list-file-shares \
  --gateway-arn "$GATEWAY_ARN" \
  --query "FileShareInfoList[*].{ShareARN:FileShareARN,Status:FileShareStatus,Type:FileShareType}" \
  --output table
```

Expected: at least one NFS file share with status `AVAILABLE`.

```bash
# Verify files appear as S3 objects
aws s3 ls s3://file-gateway-demo-${ACCOUNT_ID}/ --recursive
```

Expected: files written via NFS mount appear as S3 objects with matching keys.

```bash
# Check cache configuration
aws storagegateway describe-cache \
  --gateway-arn "$GATEWAY_ARN" \
  --query "{CacheAllocated:CacheAllocatedInBytes,CacheUsed:CacheUsedPercentage,CacheHit:CacheHitPercentage,CacheMiss:CacheMissPercentage}"
```

Expected: cache allocated matches your EBS volume size.

## Cleanup

Destroy all resources to stop incurring charges:

```bash
terraform destroy -auto-approve
```

Verify nothing remains:

```bash
aws storagegateway list-gateways --query "Gateways[?GatewayName=='file-gateway-demo']"
aws s3 ls s3://file-gateway-demo-${ACCOUNT_ID}/ 2>&1 || echo "Bucket deleted"
```

Expected: empty results.

## What's Next

You have deployed a hybrid storage solution using Storage Gateway. In the next exercise, you will apply **migration strategy** by evaluating the 6 Rs framework against a simulated application stack and implementing database migration with DMS.

## Summary

- **File Gateway** presents S3 buckets as NFS/SMB shares -- each file becomes an S3 object with the same key
- **Volume Gateway Cached** provides iSCSI block storage with hot data cached locally and full data in S3
- **Volume Gateway Stored** keeps full data locally with asynchronous snapshots to S3
- File Gateway is the right choice when applications need **file-level access** (NFS/SMB) to cloud storage
- **Cache sizing** is critical -- undersized caches cause silent performance degradation (cache misses fetch from S3)
- AWS recommends cache = **20% of actively accessed data**, not 20% of total data
- S3 lifecycle policies work with File Gateway but files transitioned to Glacier require restore before reading
- Monitor **CacheHitPercent** and **CachePercentDirty** in CloudWatch to detect cache sizing problems
- File Gateway requires a minimum of **m5.xlarge** (4 vCPU, 16 GiB RAM) and separate EBS volumes for cache and upload buffer

## Reference

- [AWS Storage Gateway File Gateway](https://docs.aws.amazon.com/filegateway/latest/files3/what-is-file-s3.html)
- [Storage Gateway Hardware and VM Requirements](https://docs.aws.amazon.com/filegateway/latest/files3/Requirements.html)
- [Monitoring Storage Gateway with CloudWatch](https://docs.aws.amazon.com/filegateway/latest/files3/Main_monitoring-gateways-common.html)
- [Terraform aws_storagegateway_gateway Resource](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/storagegateway_gateway)

## Additional Resources

- [Choosing a Gateway Type](https://docs.aws.amazon.com/storagegateway/latest/userguide/WhatIsStorageGateway.html) -- comparison of File, Volume, and Tape gateway types with use case guidance
- [Storage Gateway Performance](https://docs.aws.amazon.com/filegateway/latest/files3/Performance.html) -- bandwidth requirements, cache disk IOPS recommendations, and network throughput guidelines
- [S3 Lifecycle Policies](https://docs.aws.amazon.com/AmazonS3/latest/userguide/object-lifecycle-mgmt.html) -- transitioning objects between storage classes and interaction with File Gateway
- [Hybrid Cloud Storage Architecture](https://aws.amazon.com/blogs/storage/hybrid-cloud-storage-architecture-with-aws-storage-gateway/) -- reference architectures for File Gateway in enterprise environments

<details>
<summary>Full Solution</summary>

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
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
  default     = "sgw-demo"
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" { state = "available" }

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags = { Name = "${var.project_name}" }
}

resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  cidr_block              = "10.0.1.0/24"
  availability_zone       = data.aws_availability_zones.available.names[0]
  map_public_ip_on_launch = true
  tags = { Name = "${var.project_name}-public" }
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = { Name = var.project_name }
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  route {
    cidr_block = "0.0.0.0/0"
    gateway_id = aws_internet_gateway.this.id
  }
  tags = { Name = "${var.project_name}-public" }
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}
```

### `security.tf`

```hcl
# Storage Gateway needs ports 80 (activation),
# 443 (AWS APIs), 2049 (NFS), and ephemeral ports for clients.
resource "aws_security_group" "gateway" {
  name_prefix = "${var.project_name}-"
  vpc_id      = aws_vpc.this.id
  description = "Storage Gateway and NFS access"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
    description = "Gateway activation"
  }

  ingress {
    from_port   = 2049
    to_port     = 2049
    protocol    = "tcp"
    cidr_blocks = [aws_vpc.this.cidr_block]
    description = "NFS"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
    description = "All outbound"
  }

  tags = { Name = var.project_name }
}
```

### `storage.tf`

```hcl
data "aws_caller_identity" "current" {}

resource "aws_s3_bucket" "file_gateway" {
  bucket        = "${var.project_name}-files-${data.aws_caller_identity.current.account_id}"
  force_destroy = true
  tags          = { Name = "${var.project_name}-files" }
}

resource "aws_s3_bucket_lifecycle_configuration" "file_gateway" {
  bucket = aws_s3_bucket.file_gateway.id

  rule {
    id     = "archive-old-documents"
    status = "Enabled"

    transition {
      days          = 90
      storage_class = "STANDARD_IA"
    }

    transition {
      days          = 365
      storage_class = "GLACIER"
    }
  }
}
```

### `iam.tf`

```hcl
data "aws_iam_policy_document" "sgw_assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["storagegateway.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "sgw_s3" {
  name               = "${var.project_name}-file-gateway-s3"
  assume_role_policy = data.aws_iam_policy_document.sgw_assume.json
}

data "aws_iam_policy_document" "sgw_s3" {
  statement {
    actions = [
      "s3:GetObject",
      "s3:PutObject",
      "s3:DeleteObject",
      "s3:ListBucket",
    ]
    resources = [
      aws_s3_bucket.file_gateway.arn,
      "${aws_s3_bucket.file_gateway.arn}/*",
    ]
  }
}

resource "aws_iam_role_policy" "sgw_s3" {
  name   = "s3-access"
  role   = aws_iam_role.sgw_s3.id
  policy = data.aws_iam_policy_document.sgw_s3.json
}
```

### `compute.tf`

```hcl
# Storage Gateway EC2 Instance
data "aws_ssm_parameter" "sgw_ami" {
  name = "/aws/service/storagegateway/ami/FILE_S3/latest"
}

resource "aws_instance" "gateway" {
  ami                    = data.aws_ssm_parameter.sgw_ami.value
  instance_type          = "m5.xlarge"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.gateway.id]

  tags = { Name = "${var.project_name}-file-gateway" }
}

# Cache disk: 150 GiB gp3 for frequently accessed data
resource "aws_ebs_volume" "cache" {
  availability_zone = aws_instance.gateway.availability_zone
  size              = 150
  type              = "gp3"
  tags              = { Name = "${var.project_name}-cache" }
}

resource "aws_volume_attachment" "cache" {
  device_name = "/dev/xvdb"
  volume_id   = aws_ebs_volume.cache.id
  instance_id = aws_instance.gateway.id
}

# Upload buffer: 150 GiB gp3 for data pending upload to S3
resource "aws_ebs_volume" "upload_buffer" {
  availability_zone = aws_instance.gateway.availability_zone
  size              = 150
  type              = "gp3"
  tags              = { Name = "${var.project_name}-upload-buffer" }
}

resource "aws_volume_attachment" "upload_buffer" {
  device_name = "/dev/xvdc"
  volume_id   = aws_ebs_volume.upload_buffer.id
  instance_id = aws_instance.gateway.id
}

# Client EC2 Instance (simulates on-premises app server)
data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]
  filter {
    name   = "name"
    values = ["al2023-ami-2023.*-x86_64"]
  }
}

resource "aws_instance" "client" {
  ami                    = data.aws_ami.al2023.value
  instance_type          = "t3.micro"
  subnet_id              = aws_subnet.public.id
  vpc_security_group_ids = [aws_security_group.gateway.id]

  user_data = <<-EOF
    #!/bin/bash
    dnf install -y nfs-utils
  EOF

  tags = { Name = "${var.project_name}-nfs-client" }
}
```

### `outputs.tf`

```hcl
output "gateway_private_ip" {
  value       = aws_instance.gateway.private_ip
  description = "Storage Gateway private IP (for activation and NFS mount)"
}

output "client_instance_id" {
  value       = aws_instance.client.id
  description = "Client EC2 instance ID (for SSM Session Manager)"
}

output "s3_bucket" {
  value       = aws_s3_bucket.file_gateway.id
  description = "S3 bucket backing the File Gateway share"
}

output "iam_role_arn" {
  value       = aws_iam_role.sgw_s3.arn
  description = "IAM role ARN for File Gateway S3 access"
}
```

### Post-Apply: Gateway Activation and NFS Share Creation

After `terraform apply`, activate the gateway and create the NFS share:

```bash
# 1. Get gateway IP
GW_IP=$(terraform output -raw gateway_private_ip)

# 2. Retrieve activation key
ACTIVATION_KEY=$(curl -sf "http://${GW_IP}/?activationRegion=us-east-1&no_redirect")

# 3. Activate gateway
GATEWAY_ARN=$(aws storagegateway activate-gateway \
  --activation-key "$ACTIVATION_KEY" \
  --gateway-name "file-gateway-demo" \
  --gateway-timezone "GMT" \
  --gateway-type "FILE_S3" \
  --gateway-region "us-east-1" \
  --query "GatewayARN" --output text)

# 4. Wait for gateway to be running
sleep 30

# 5. List and assign local disks
DISKS=$(aws storagegateway list-local-disks --gateway-arn "$GATEWAY_ARN")
CACHE_DISK=$(echo "$DISKS" | jq -r '.Disks[] | select(.DiskPath=="/dev/xvdb") | .DiskId')
UPLOAD_DISK=$(echo "$DISKS" | jq -r '.Disks[] | select(.DiskPath=="/dev/xvdc") | .DiskId')

aws storagegateway add-cache \
  --gateway-arn "$GATEWAY_ARN" \
  --disk-ids "$CACHE_DISK"

aws storagegateway add-upload-buffer \
  --gateway-arn "$GATEWAY_ARN" \
  --disk-ids "$UPLOAD_DISK"

# 6. Create NFS file share
ROLE_ARN=$(terraform output -raw iam_role_arn)
BUCKET_ARN="arn:aws:s3:::$(terraform output -raw s3_bucket)"

aws storagegateway create-nfs-file-share \
  --client-token "demo-share-$(date +%s)" \
  --gateway-arn "$GATEWAY_ARN" \
  --role "$ROLE_ARN" \
  --location-arn "$BUCKET_ARN" \
  --client-list "10.0.0.0/16" \
  --default-storage-class "S3_STANDARD" \
  --no-read-only

# 7. Mount from client (via SSM Session Manager)
aws ssm start-session --target "$(terraform output -raw client_instance_id)"
# Inside the session:
# mkdir -p /mnt/docs
# mount -t nfs -o nolock,hard ${GW_IP}:/$(terraform output -raw s3_bucket) /mnt/docs
# echo "Hello from NFS" > /mnt/docs/test.txt
# exit

# 8. Verify S3 object exists
aws s3 ls "s3://$(terraform output -raw s3_bucket)/"
```

</details>
