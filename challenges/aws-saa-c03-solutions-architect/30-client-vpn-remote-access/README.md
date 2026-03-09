# 30. AWS Client VPN for Remote Access

<!--
difficulty: advanced
concepts: [client-vpn, mutual-tls, certificate-authentication, authorization-rules, split-tunnel, full-tunnel, client-vpn-endpoint, network-association, openvpn]
tools: [terraform, aws-cli]
estimated_time: 55m
bloom_level: evaluate, create
prerequisites: [17-vpc-subnets-route-tables-igw, 19-security-groups-vs-nacls]
aws_cost: ~$0.10/hr
-->

> **AWS Cost Warning:** AWS Client VPN endpoint costs $0.10/hr when associated with a subnet, plus $0.05/hr per active client connection. Even with zero connected clients, the associated endpoint charges $0.10/hr (~$72/month). Destroy IMMEDIATELY when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- OpenSSL installed for certificate generation
- Understanding of VPC, subnets, and security groups

## Learning Objectives

After completing this exercise, you will be able to:

- **Design** a Client VPN solution for remote workforce access to VPC resources
- **Implement** mutual TLS authentication with self-signed certificates using ACM
- **Configure** authorization rules to control which users can access which networks
- **Evaluate** split-tunnel versus full-tunnel and the security, cost, and performance trade-offs
- **Explain** Client VPN network associations and how they determine which subnets are reachable
- **Describe** the authentication options: mutual TLS, Active Directory, SAML-based SSO

## Why Client VPN Matters

AWS Client VPN provides managed OpenVPN-compatible remote access for individual users connecting to VPC resources. Unlike Site-to-Site VPN (which connects entire networks), Client VPN connects individual devices -- laptops, phones, tablets. The SAA-C03 exam tests Client VPN in scenarios like: "Remote employees need to access private RDS instances" or "Contractors need temporary access to specific application servers."

The key architectural decisions are authentication method and tunnel mode. Mutual TLS uses client certificates -- each user has a unique certificate that authenticates them to the VPN endpoint. Active Directory integrates with your existing identity provider. SAML-based SSO allows users to authenticate through an identity provider like Okta or Azure AD. The exam expects you to know that mutual TLS provides certificate-based device authentication (the certificate is tied to the device, not the user), while AD/SAML provides user-based authentication.

Split-tunnel versus full-tunnel is a critical design decision. In full-tunnel mode, ALL traffic from the client goes through the VPN -- internet traffic, AWS traffic, everything. This gives you complete visibility and control but increases bandwidth costs (you pay for internet traffic flowing through the VPN endpoint) and adds latency for non-VPC traffic. In split-tunnel mode, only traffic destined for the VPC CIDR goes through the VPN; internet traffic uses the client's local internet connection. This is more efficient but means you cannot inspect or filter the user's internet traffic.

## The Challenge

Deploy a Client VPN endpoint with mutual TLS authentication, connect it to a VPC, and configure authorization rules that restrict access to specific subnets.

### Architecture

```
Remote User (laptop)
  |
  |-- OpenVPN Client (with client certificate)
  |
  +==== TLS tunnel ====> Client VPN Endpoint
                            |
                            |-- Network Association (subnet)
                            |
                       +----+----+
                       |   VPC   |
                       |         |
                       | +-----+ |
                       | | App | |
                       | +-----+ |
                       |         |
                       | +-----+ |
                       | | DB  | |
                       | +-----+ |
                       +---------+
```

### Requirements

1. Self-signed CA and client/server certificates for mutual TLS
2. Client VPN endpoint with mutual TLS authentication
3. Network association to a private subnet
4. Authorization rule allowing access to the VPC CIDR
5. Split-tunnel configuration (only VPC traffic through VPN)
6. Security group restricting VPN client access

## Hints

<details>
<summary>Hint 1: Certificate Generation</summary>

Generate a self-signed CA, server certificate, and client certificate using OpenSSL or easy-rsa. The following uses the AWS-recommended easy-rsa approach:

```bash
# Clone easy-rsa
git clone https://github.com/OpenVPN/easy-rsa.git
cd easy-rsa/easyrsa3

# Initialize PKI
./easyrsa init-pki

# Build CA (Certificate Authority)
./easyrsa build-ca nopass
# Common Name: VPN Demo CA

# Generate server certificate
./easyrsa build-server-full server nopass

# Generate client certificate
./easyrsa build-client-full client1.domain.tld nopass
```

Or using OpenSSL directly:

```bash
# Generate CA private key and certificate
openssl req -x509 -newkey rsa:2048 -keyout ca-key.pem -out ca-cert.pem \
  -days 365 -nodes -subj "/CN=VPN-Demo-CA"

# Generate server key and CSR
openssl req -newkey rsa:2048 -keyout server-key.pem -out server-csr.pem \
  -nodes -subj "/CN=server"
openssl x509 -req -in server-csr.pem -CA ca-cert.pem -CAkey ca-key.pem \
  -CAcreateserial -out server-cert.pem -days 365

# Generate client key and CSR
openssl req -newkey rsa:2048 -keyout client-key.pem -out client-csr.pem \
  -nodes -subj "/CN=client1"
openssl x509 -req -in client-csr.pem -CA ca-cert.pem -CAkey ca-key.pem \
  -CAcreateserial -out client-cert.pem -days 365
```

Import certificates into ACM:

```hcl
resource "aws_acm_certificate" "server" {
  private_key       = file("${path.module}/certs/server-key.pem")
  certificate_body  = file("${path.module}/certs/server-cert.pem")
  certificate_chain = file("${path.module}/certs/ca-cert.pem")

  lifecycle {
    create_before_destroy = true
  }
}

resource "aws_acm_certificate" "ca" {
  private_key       = file("${path.module}/certs/ca-key.pem")
  certificate_body  = file("${path.module}/certs/ca-cert.pem")

  lifecycle {
    create_before_destroy = true
  }
}
```

</details>

<details>
<summary>Hint 2: Client VPN Endpoint Configuration</summary>

```hcl
resource "aws_ec2_client_vpn_endpoint" "this" {
  description            = "Client VPN Demo"
  server_certificate_arn = aws_acm_certificate.server.arn
  client_cidr_block      = "172.16.0.0/16"

  # Mutual TLS authentication
  authentication_options {
    type                       = "certificate-authentication"
    root_certificate_chain_arn = aws_acm_certificate.ca.arn
  }

  # Connection logging
  connection_log_options {
    enabled               = true
    cloudwatch_log_group  = aws_cloudwatch_log_group.vpn.name
    cloudwatch_log_stream = aws_cloudwatch_log_stream.vpn.name
  }

  # Split tunnel: only VPC traffic goes through VPN
  split_tunnel = true

  # DNS servers (use VPC DNS)
  dns_servers = ["10.0.0.2"]

  # Transport protocol
  transport_protocol = "udp"

  # VPN port
  vpn_port = 443

  # Security group
  security_group_ids = [aws_security_group.vpn.id]
  vpc_id             = aws_vpc.this.id

  tags = { Name = "client-vpn-demo" }
}

resource "aws_cloudwatch_log_group" "vpn" {
  name              = "/aws/client-vpn/demo"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_stream" "vpn" {
  name           = "connections"
  log_group_name = aws_cloudwatch_log_group.vpn.name
}
```

Key parameters:
- **client_cidr_block**: IP range assigned to VPN clients. Must NOT overlap with VPC CIDR or on-premises networks. Use a completely separate range like 172.16.0.0/16.
- **split_tunnel = true**: Only VPC-destined traffic goes through VPN. Set to false for full-tunnel (all traffic through VPN).
- **vpn_port = 443**: Uses standard HTTPS port to avoid firewall blocking (many corporate networks block non-standard ports).

</details>

<details>
<summary>Hint 3: Network Associations and Authorization Rules</summary>

```hcl
# Network association: connect VPN endpoint to a subnet
# This creates an ENI in the subnet for VPN traffic
resource "aws_ec2_client_vpn_network_association" "this" {
  client_vpn_endpoint_id = aws_ec2_client_vpn_endpoint.this.id
  subnet_id              = aws_subnet.private.id
}

# Authorization rule: allow VPN clients to access VPC CIDR
resource "aws_ec2_client_vpn_authorization_rule" "vpc" {
  client_vpn_endpoint_id = aws_ec2_client_vpn_endpoint.this.id
  target_network_cidr    = aws_vpc.this.cidr_block
  authorize_all_groups   = true
  description            = "Allow access to VPC"
}

# Optional: route for internet access through VPN (full-tunnel only)
# resource "aws_ec2_client_vpn_route" "internet" {
#   client_vpn_endpoint_id = aws_ec2_client_vpn_endpoint.this.id
#   destination_cidr_block = "0.0.0.0/0"
#   target_vpc_subnet_id   = aws_subnet.public.id
#   description            = "Internet via VPN"
# }
```

Network associations:
- Each association creates an ENI in the specified subnet
- Associate with multiple subnets for high availability (one per AZ)
- The subnet determines which VPC resources the VPN clients can reach
- Each association costs $0.10/hr (the primary cost driver)

Authorization rules:
- Define which CIDR ranges VPN clients can access
- `authorize_all_groups = true`: all authenticated clients can access
- For AD/SAML: specify `access_group_id` to restrict by AD group

</details>

<details>
<summary>Hint 4: Split-Tunnel vs Full-Tunnel</summary>

| Feature | Split-Tunnel | Full-Tunnel |
|---------|-------------|-------------|
| **VPC traffic** | Through VPN | Through VPN |
| **Internet traffic** | Direct (client's ISP) | Through VPN (NAT Gateway) |
| **Bandwidth cost** | Lower (only VPC traffic) | Higher (all traffic) |
| **Latency for internet** | Lower (direct path) | Higher (VPN + AWS egress) |
| **Visibility** | VPC traffic only | All client traffic |
| **Security** | Cannot inspect internet traffic | Full traffic inspection |
| **Client performance** | Better (streaming, downloads direct) | Worse (everything through VPN) |
| **Configuration** | `split_tunnel = true` | `split_tunnel = false` + 0.0.0.0/0 route |

**Choose split-tunnel when:**
- You only need to access VPC resources
- Bandwidth costs are a concern
- Users need fast internet for non-work browsing
- You trust the client device's internet security

**Choose full-tunnel when:**
- Compliance requires all traffic to be inspected
- You need to enforce web filtering on remote devices
- Users access sensitive data that must not be cached locally
- You want DNS visibility for all user queries

</details>

<details>
<summary>Hint 5: Client Configuration File</summary>

After creating the VPN endpoint, download the client configuration:

```bash
# Download the OpenVPN configuration file
aws ec2 export-client-vpn-client-configuration \
  --client-vpn-endpoint-id $(terraform output -raw vpn_endpoint_id) \
  --output text > client-config.ovpn
```

The downloaded .ovpn file needs to be modified to include the client certificate and key:

```bash
# Append client certificate and key to the config file
echo "" >> client-config.ovpn
echo "<cert>" >> client-config.ovpn
cat client-cert.pem >> client-config.ovpn
echo "</cert>" >> client-config.ovpn
echo "<key>" >> client-config.ovpn
cat client-key.pem >> client-config.ovpn
echo "</key>" >> client-config.ovpn
```

Users import this file into any OpenVPN-compatible client:
- AWS VPN Client (official AWS client)
- OpenVPN Connect
- Tunnelblick (macOS)

</details>

## Spot the Bug

A team deploys Client VPN but remote users report they cannot resolve private DNS names (like `database.internal`) even though they can ping private IP addresses:

```hcl
resource "aws_ec2_client_vpn_endpoint" "this" {
  server_certificate_arn = aws_acm_certificate.server.arn
  client_cidr_block      = "172.16.0.0/16"

  authentication_options {
    type                       = "certificate-authentication"
    root_certificate_chain_arn = aws_acm_certificate.ca.arn
  }

  connection_log_options {
    enabled = false
  }

  split_tunnel = true
  # dns_servers not specified  <-- Bug
}
```

<details>
<summary>Explain the bug</summary>

**No DNS servers are configured on the VPN endpoint.** When `dns_servers` is not specified, VPN clients use their local DNS resolver (ISP or corporate DNS). This resolver cannot resolve private Route 53 hosted zone names like `database.internal` because those zones are only visible within the VPC.

The client can ping private IP addresses (like `10.0.1.50`) because the VPN tunnel correctly routes traffic to the VPC. But DNS queries for `database.internal` go to the client's local DNS, which returns NXDOMAIN.

**The fix:** Configure the VPC DNS server (always at VPC CIDR base + 2):

```hcl
resource "aws_ec2_client_vpn_endpoint" "this" {
  # ...
  dns_servers = ["10.0.0.2"]  # VPC DNS resolver
}
```

With this setting, the VPN client uses the VPC's Route 53 Resolver for DNS queries. Private hosted zone names resolve correctly, and public DNS names still resolve through the VPC's DNS (which forwards to public resolvers).

For split-tunnel configurations, ensure the DNS server IP (10.0.0.2) is within the VPC CIDR so the DNS traffic routes through the VPN tunnel. If the DNS server were outside the VPC CIDR, DNS queries would bypass the VPN and use the local resolver.

</details>

## Authentication Options Comparison

| Method | Authentication | User Management | MFA Support | Use Case |
|--------|---------------|----------------|-------------|----------|
| **Mutual TLS** | Client certificate | Certificate management | No (device auth) | Small teams, IoT devices |
| **Active Directory** | AD credentials | AD/LDAP | Yes (AD MFA) | Enterprise with existing AD |
| **SAML SSO** | IdP (Okta, Azure AD) | Identity provider | Yes (IdP MFA) | Modern enterprise, SSO |
| **Mutual TLS + AD** | Both required | Certificate + AD | Yes | High security |
| **Mutual TLS + SAML** | Both required | Certificate + IdP | Yes | Maximum security |

## Full Solution

<details>
<summary>Complete Terraform Configuration</summary>

### `providers.tf`

```hcl
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
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
  default     = "client-vpn-demo"
}
```

### `tls.tf`

```hcl
# CA Certificate
resource "tls_private_key" "ca" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "tls_self_signed_cert" "ca" {
  private_key_pem = tls_private_key.ca.private_key_pem

  subject {
    common_name  = "VPN Demo CA"
    organization = "Demo"
  }

  validity_period_hours = 8760  # 1 year
  is_ca_certificate     = true

  allowed_uses = [
    "cert_signing",
    "crl_signing",
  ]
}

# Server Certificate
resource "tls_private_key" "server" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "tls_cert_request" "server" {
  private_key_pem = tls_private_key.server.private_key_pem

  subject {
    common_name  = "server.vpn.demo"
    organization = "Demo"
  }
}

resource "tls_locally_signed_cert" "server" {
  cert_request_pem   = tls_cert_request.server.cert_request_pem
  ca_private_key_pem = tls_private_key.ca.private_key_pem
  ca_cert_pem        = tls_self_signed_cert.ca.cert_pem

  validity_period_hours = 8760

  allowed_uses = [
    "digital_signature",
    "key_encipherment",
    "server_auth",
  ]
}

# Client Certificate
resource "tls_private_key" "client" {
  algorithm = "RSA"
  rsa_bits  = 2048
}

resource "tls_cert_request" "client" {
  private_key_pem = tls_private_key.client.private_key_pem

  subject {
    common_name  = "client1.vpn.demo"
    organization = "Demo"
  }
}

resource "tls_locally_signed_cert" "client" {
  cert_request_pem   = tls_cert_request.client.cert_request_pem
  ca_private_key_pem = tls_private_key.ca.private_key_pem
  ca_cert_pem        = tls_self_signed_cert.ca.cert_pem

  validity_period_hours = 8760

  allowed_uses = [
    "digital_signature",
    "key_encipherment",
    "client_auth",
  ]
}

# Import into ACM
resource "aws_acm_certificate" "server" {
  private_key       = tls_private_key.server.private_key_pem
  certificate_body  = tls_locally_signed_cert.server.cert_pem
  certificate_chain = tls_self_signed_cert.ca.cert_pem
}

resource "aws_acm_certificate" "ca" {
  private_key      = tls_private_key.ca.private_key_pem
  certificate_body = tls_self_signed_cert.ca.cert_pem
}
```

### `vpc.tf`

```hcl
data "aws_availability_zones" "available" {
  state = "available"
}

resource "aws_vpc" "this" {
  cidr_block           = "10.0.0.0/16"
  enable_dns_support   = true
  enable_dns_hostnames = true
  tags                 = { Name = var.project_name }
}

resource "aws_subnet" "private" {
  vpc_id            = aws_vpc.this.id
  cidr_block        = "10.0.1.0/24"
  availability_zone = data.aws_availability_zones.available.names[0]
  tags              = { Name = "${var.project_name}-private" }
}
```

### `security.tf`

```hcl
resource "aws_security_group" "vpn" {
  name_prefix = "${var.project_name}-"
  vpc_id      = aws_vpc.this.id
  description = "Client VPN security group"

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "udp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "VPN connections"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = { Name = "${var.project_name}-sg" }
}
```

### `vpn.tf`

```hcl
resource "aws_cloudwatch_log_group" "vpn" {
  name              = "/aws/client-vpn/${var.project_name}"
  retention_in_days = 7
}

resource "aws_cloudwatch_log_stream" "vpn" {
  name           = "connections"
  log_group_name = aws_cloudwatch_log_group.vpn.name
}

resource "aws_ec2_client_vpn_endpoint" "this" {
  description            = "Client VPN Demo"
  server_certificate_arn = aws_acm_certificate.server.arn
  client_cidr_block      = "172.16.0.0/16"

  authentication_options {
    type                       = "certificate-authentication"
    root_certificate_chain_arn = aws_acm_certificate.ca.arn
  }

  connection_log_options {
    enabled               = true
    cloudwatch_log_group  = aws_cloudwatch_log_group.vpn.name
    cloudwatch_log_stream = aws_cloudwatch_log_stream.vpn.name
  }

  split_tunnel       = true
  dns_servers        = ["10.0.0.2"]
  transport_protocol = "udp"
  vpn_port           = 443
  security_group_ids = [aws_security_group.vpn.id]
  vpc_id             = aws_vpc.this.id

  tags = { Name = var.project_name }
}

# Associate with subnet
resource "aws_ec2_client_vpn_network_association" "this" {
  client_vpn_endpoint_id = aws_ec2_client_vpn_endpoint.this.id
  subnet_id              = aws_subnet.private.id
}

# Authorization rule: allow access to VPC
resource "aws_ec2_client_vpn_authorization_rule" "vpc" {
  client_vpn_endpoint_id = aws_ec2_client_vpn_endpoint.this.id
  target_network_cidr    = aws_vpc.this.cidr_block
  authorize_all_groups   = true
  description            = "Allow access to VPC"
}
```

### `outputs.tf`

```hcl
output "vpn_endpoint_id" {
  value = aws_ec2_client_vpn_endpoint.this.id
}

output "vpn_dns_name" {
  value = aws_ec2_client_vpn_endpoint.this.dns_name
}

output "client_private_key" {
  value     = tls_private_key.client.private_key_pem
  sensitive = true
}

output "client_certificate" {
  value = tls_locally_signed_cert.client.cert_pem
}
```

</details>

## Verify What You Learned

```bash
# Verify VPN endpoint exists
aws ec2 describe-client-vpn-endpoints \
  --filters "Name=tag:Name,Values=client-vpn-demo" \
  --query "ClientVpnEndpoints[0].{Id:ClientVpnEndpointId,Status:Status.Code,SplitTunnel:SplitTunnel,DNS:DnsServers}" \
  --output table
```

Expected: Status=available, SplitTunnel=true, DNS=[10.0.0.2].

```bash
# Verify network association
ENDPOINT_ID=$(terraform output -raw vpn_endpoint_id)
aws ec2 describe-client-vpn-target-networks \
  --client-vpn-endpoint-id "$ENDPOINT_ID" \
  --query "ClientVpnTargetNetworks[*].{SubnetId:TargetNetworkId,Status:Status.Code}" \
  --output table
```

Expected: One subnet association with status "associated".

```bash
# Verify authorization rules
aws ec2 describe-client-vpn-authorization-rules \
  --client-vpn-endpoint-id "$ENDPOINT_ID" \
  --query "AuthorizationRules[*].{CIDR:DestinationCidr,Status:Status.Code}" \
  --output table
```

Expected: VPC CIDR with status "active".

```bash
# Export client configuration
aws ec2 export-client-vpn-client-configuration \
  --client-vpn-endpoint-id "$ENDPOINT_ID" \
  --output text > client-config.ovpn
echo "Client config exported to client-config.ovpn"
```

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

**Important:** Client VPN costs $0.10/hr per subnet association. Destroy immediately.

```bash
terraform destroy -auto-approve
```

Verify:

```bash
terraform state list
```

Expected: no output (empty state).

## What's Next

You have completed the VPC and Networking section of the SAA-C03 exercise series. You now understand the full networking stack: VPCs and subnets (17), NAT (18), security groups and NACLs (19), Flow Logs (20), VPC peering (21), VPC endpoints (22), PrivateLink (23), Transit Gateway (24), Direct Connect (25), Site-to-Site VPN (26), Network Firewall (27), Route 53 routing (28), Route 53 health checks (29), and Client VPN (30). These topics collectively represent one of the largest domains on the SAA-C03 exam.

## Summary

- **AWS Client VPN** provides managed OpenVPN-compatible remote access for individual users to VPC resources
- Authentication options: **mutual TLS** (device-based), **Active Directory** (user-based), **SAML SSO** (identity provider)
- **client_cidr_block** must not overlap with VPC CIDR or on-premises networks
- **Split-tunnel** routes only VPC traffic through VPN (lower cost, better performance); **full-tunnel** routes all traffic (complete visibility, higher cost)
- **Network associations** connect the VPN endpoint to subnets; each costs $0.10/hr
- **Authorization rules** control which CIDR ranges VPN clients can access; use AD groups or SAML attributes for fine-grained control
- Configure **dns_servers** to use VPC DNS (base + 2) for private hosted zone resolution
- Client VPN supports up to **2,000 concurrent connections** per endpoint
- The client configuration file (.ovpn) must include the client certificate and key for mutual TLS

## Reference

- [AWS Client VPN](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/what-is.html)
- [Client VPN Authentication](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/authentication-authrization.html)
- [Client VPN Authorization Rules](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/cvpn-working-rules.html)
- [Terraform aws_ec2_client_vpn_endpoint](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/ec2_client_vpn_endpoint)

## Additional Resources

- [Client VPN Pricing](https://aws.amazon.com/vpn/pricing/) -- per-endpoint and per-connection hourly costs
- [Split-Tunnel Configuration](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/split-tunnel-vpn.html) -- enabling split-tunnel and configuring routes
- [SAML-Based Authentication](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/client-authentication.html#saml) -- integrating with Okta, Azure AD, or other SAML 2.0 identity providers
- [Client VPN Scaling](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/scaling-considerations.html) -- connection limits, subnet sizing, and multi-AZ association for HA
- [Certificate Revocation Lists](https://docs.aws.amazon.com/vpn/latest/clientvpn-admin/cvpn-working-certificates.html) -- revoking compromised client certificates without reissuing all certificates
