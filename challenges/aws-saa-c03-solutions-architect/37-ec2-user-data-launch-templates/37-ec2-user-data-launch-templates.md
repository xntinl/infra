# 37. EC2 User Data and Launch Templates

<!--
difficulty: basic
concepts: [user-data, launch-templates, template-versioning, cloud-init, instance-metadata, base64-encoding, bootstrap-scripts]
tools: [terraform, aws-cli]
estimated_time: 30m
bloom_level: understand
prerequisites: [none]
aws_cost: ~$0.01/hr
-->

> **AWS Cost Warning:** This exercise launches a single t3.micro instance (~$0.0104/hr). Total cost for 30 minutes is ~$0.01. Remember to run `terraform destroy` when finished.

## Prerequisites

- Terraform >= 1.7 installed
- AWS CLI configured with a sandbox account
- Default VPC available in your target region
- Basic understanding of bash scripting

## Learning Objectives

After completing this exercise, you will be able to:

- **Explain** how EC2 user data scripts execute during instance launch (once, as root, on first boot only)
- **Construct** a launch template with versioning and demonstrate how to create new versions without replacing instances
- **Identify** the difference between user data (runs once on first boot) and cloud-init directives (can configure multi-boot behavior)
- **Verify** that user data executed successfully using instance metadata and system logs
- **Describe** why launch templates are preferred over launch configurations (versioning, mixed instances, Spot support)

## Why User Data and Launch Templates Matter

User data is the primary mechanism for bootstrapping EC2 instances. When you launch an instance, the user data script runs once as root during the first boot. This is how you install packages, configure services, pull application code, and register with configuration management tools -- all without creating a custom AMI for every minor change.

Launch templates extend this by adding versioning. Instead of modifying a launch configuration (which is immutable -- any change creates a new one), you create a new version of a launch template. Auto Scaling Groups can reference `$Latest` or `$Default` version, so rolling out a configuration change is as simple as creating a new version and triggering an instance refresh. The SAA-C03 exam tests this distinction directly: "How do you update the AMI used by an ASG without downtime?" The answer is: update the launch template, create a new version, and perform an instance refresh.

The most common mistake with user data is not understanding that it runs only on the first boot. If you stop and start an instance, user data does NOT re-run. If your user data script has a bug and the instance boots with a broken configuration, you must either terminate and relaunch or manually fix the instance. This is why user data scripts should be idempotent and why many teams use configuration management tools (SSM, Ansible, Chef) for ongoing configuration.

## Step 1 -- Create the Project Files

Create the following files in your exercise directory:

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
  default     = "saa-ex37"
}
```

### `main.tf`

```hcl
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
  filter {
    name   = "default-for-az"
    values = ["true"]
  }
}

data "aws_ami" "al2023" {
  most_recent = true
  owners      = ["amazon"]

  filter {
    name   = "name"
    values = ["al2023-ami-*-x86_64"]
  }

  filter {
    name   = "virtualization-type"
    values = ["hvm"]
  }
}

resource "aws_iam_role" "ec2" {
  name = "${var.project_name}-ec2-role"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Action    = "sts:AssumeRole"
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_instance_profile" "ec2" {
  name = "${var.project_name}-ec2-profile"
  role = aws_iam_role.ec2.name
}

resource "aws_security_group" "web" {
  name_prefix = "${var.project_name}-"
  vpc_id      = data.aws_vpc.default.id
  description = "Web server demo"

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
    description = "HTTP from anywhere"
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ------------------------------------------------------------------
# Launch Template v1: installs and configures a web server.
#
# Launch templates support versioning -- you can create new
# versions without deleting the template. ASGs reference
# $Latest or $Default to pick up changes automatically.
#
# User data key behaviors:
# - Runs ONCE on first boot only (not on stop/start)
# - Runs as root (no sudo needed)
# - Must start with #!/bin/bash (or #!/bin/python, etc.)
# - Terraform requires base64 encoding via base64encode()
# - Maximum size: 16 KB (before base64 encoding)
# ------------------------------------------------------------------
resource "aws_launch_template" "web" {
  name        = "${var.project_name}-web-server"
  description = "Web server with nginx - version 1"
  image_id    = data.aws_ami.al2023.id

  instance_type = "t3.micro"

  iam_instance_profile {
    name = aws_iam_instance_profile.ec2.name
  }

  vpc_security_group_ids = [aws_security_group.web.id]

  # ----------------------------------------------------------
  # User data script: installs nginx and creates a status page.
  #
  # IMPORTANT: This script runs only on FIRST BOOT.
  # If the instance is stopped and started, it does NOT re-run.
  # If the script fails, the instance still launches -- check
  # /var/log/cloud-init-output.log for errors.
  # ----------------------------------------------------------
  user_data = base64encode(<<-EOF
    #!/bin/bash
    set -e

    # Log start time for debugging
    echo "User data started at $(date)" > /var/log/user-data-status.txt

    # Install web server
    yum update -y
    yum install -y nginx

    # Create a custom index page with instance metadata
    TOKEN=$(curl -s -X PUT "http://169.254.169.254/latest/api/token" \
      -H "X-aws-ec2-metadata-token-ttl-seconds: 21600")
    INSTANCE_ID=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" \
      http://169.254.169.254/latest/meta-data/instance-id)
    AZ=$(curl -s -H "X-aws-ec2-metadata-token: $TOKEN" \
      http://169.254.169.254/latest/meta-data/placement/availability-zone)

    cat > /usr/share/nginx/html/index.html <<HTMLEOF
    <!DOCTYPE html>
    <html>
    <body>
      <h1>SAA-C03 Exercise 37</h1>
      <p>Instance: $INSTANCE_ID</p>
      <p>AZ: $AZ</p>
      <p>Launched: $(date)</p>
      <p>Template Version: 1</p>
    </body>
    </html>
    HTMLEOF

    # Start nginx
    systemctl enable nginx
    systemctl start nginx

    # Log completion
    echo "User data completed at $(date)" >> /var/log/user-data-status.txt
  EOF
  )

  tag_specifications {
    resource_type = "instance"
    tags = {
      Name    = "${var.project_name}-web-server"
      Purpose = "launch-template-demo"
    }
  }
}

# ------------------------------------------------------------------
# Launch an instance from the template
# ------------------------------------------------------------------
resource "aws_instance" "web" {
  launch_template {
    id      = aws_launch_template.web.id
    version = "$Latest"
  }

  subnet_id = data.aws_subnets.default.ids[0]

  tags = {
    Name = "${var.project_name}-web-server"
  }
}
```

### `outputs.tf`

```hcl
output "instance_id" {
  value = aws_instance.web.id
}

output "public_ip" {
  value = aws_instance.web.public_ip
}

output "launch_template_id" {
  value = aws_launch_template.web.id
}

output "launch_template_version" {
  value = aws_launch_template.web.latest_version
}
```

## Step 2 -- Deploy and Verify User Data Execution

```bash
terraform init
terraform apply -auto-approve
```

Wait 1-2 minutes for user data to complete, then verify:

```bash
# Check the web server is responding
PUBLIC_IP=$(terraform output -raw public_ip)
curl http://$PUBLIC_IP
```

Expected: HTML page showing instance ID, AZ, launch time, and "Template Version: 1".

Verify user data execution logs:

```bash
INSTANCE_ID=$(terraform output -raw instance_id)

aws ssm send-command \
  --instance-ids $INSTANCE_ID \
  --document-name "AWS-RunShellScript" \
  --parameters 'commands=["cat /var/log/user-data-status.txt"]' \
  --query 'Command.CommandId' --output text
```

## Step 3 -- Examine Launch Template Versions

```bash
LT_ID=$(terraform output -raw launch_template_id)

# List all versions of the launch template
aws ec2 describe-launch-template-versions \
  --launch-template-id $LT_ID \
  --query 'LaunchTemplateVersions[*].{Version:VersionNumber,Description:VersionDescription,Default:DefaultVersion,Created:CreateTime}' \
  --output table
```

## Step 4 -- Understand the User Data Execution Model

### User Data vs Cloud-Init

| Feature | User Data (bash script) | Cloud-Init |
|---------|------------------------|------------|
| **Format** | `#!/bin/bash` script | YAML with `#cloud-config` header |
| **Runs as** | root | root |
| **When** | First boot only | Configurable (per-boot, per-instance, per-once) |
| **Max size** | 16 KB (raw) | 16 KB (raw) |
| **Error handling** | Script continues unless `set -e` | Per-module error handling |
| **Use case** | Simple bootstrapping | Complex multi-module configuration |

### Launch Template vs Launch Configuration

| Feature | Launch Template | Launch Configuration |
|---------|----------------|---------------------|
| **Versioning** | Yes (v1, v2, ..., $Latest, $Default) | No (immutable, new config per change) |
| **Mixed Instances** | Supported | Not supported |
| **Spot Instances** | Supported | Limited |
| **Network Interfaces** | Configurable | Limited |
| **T2/T3 Unlimited** | Configurable | Not configurable |
| **AWS recommendation** | Preferred | Legacy (deprecated for new features) |

## Common Mistakes

### 1. Expecting user data to re-run on stop/start

**Wrong assumption:** "I updated the user data in the launch template and restarted my instance. Why didn't the new configuration apply?"

**What happens:** User data runs only on the FIRST boot. When you stop and start an instance, the instance does not re-run user data -- it simply resumes from its existing root volume state. Even if you modify the launch template, existing instances are not affected.

**Fix:** To apply new user data, you must either:
- Terminate the instance and launch a new one from the updated template
- Use SSM Run Command or Ansible for ongoing configuration changes
- Use cloud-init with `per-boot` modules for scripts that should run on every boot

### 2. Missing the shebang line

**Wrong approach:** Writing user data without `#!/bin/bash` at the start:

```
yum install -y nginx
systemctl start nginx
```

**What happens:** cloud-init does not know how to interpret the script. It may be treated as a cloud-config YAML file or ignored entirely. The instance launches but none of your commands execute. Checking `/var/log/cloud-init-output.log` shows no output from your script.

**Fix:** Always start user data scripts with a shebang:

```bash
#!/bin/bash
set -e
yum install -y nginx
systemctl start nginx
```

### 3. User data script fails silently

**Wrong approach:** Not checking cloud-init logs when the instance seems misconfigured.

**What happens:** User data scripts fail but the instance still launches successfully. The AWS Console shows the instance as "running" with passing health checks (EC2 status checks only verify the OS is up, not that your application is configured correctly).

**Fix:** Always check cloud-init logs for user data errors:

```bash
# Via SSM Session Manager
cat /var/log/cloud-init-output.log
cat /var/log/user-data-status.txt

# Or via AWS CLI
aws ec2 get-console-output --instance-id <ID> --output text
```

Use `set -e` in your user data script to fail fast on the first error.

## Verify What You Learned

```bash
# Verify instance is running and using the launch template
aws ec2 describe-instances \
  --instance-ids $(terraform output -raw instance_id) \
  --query "Reservations[0].Instances[0].{State:State.Name,Type:InstanceType}" \
  --output table
```

Expected: State=running, Type=t3.micro

```bash
# Verify launch template exists with version 1
aws ec2 describe-launch-templates \
  --launch-template-names saa-ex37-web-server \
  --query "LaunchTemplates[0].{Name:LaunchTemplateName,LatestVersion:LatestVersionNumber,DefaultVersion:DefaultVersionNumber}" \
  --output table
```

Expected: LatestVersion=1, DefaultVersion=1

```bash
# Verify web server responds
curl -s http://$(terraform output -raw public_ip) | grep "Template Version"
```

Expected: `<p>Template Version: 1</p>`

```bash
terraform plan
```

Expected: `No changes. Your infrastructure matches the configuration.`

## Cleanup

```bash
terraform destroy -auto-approve
```

Verify cleanup:

```bash
aws ec2 describe-launch-templates \
  --launch-template-names saa-ex37-web-server 2>&1 | grep -c "does not exist" || \
aws ec2 describe-launch-templates \
  --launch-template-names saa-ex37-web-server \
  --query "LaunchTemplates[*].LaunchTemplateId" --output text
```

Expected: "does not exist" or no output.

## What's Next

You learned how to bootstrap instances with user data and manage launch template versions. In the next exercise, you will create **AMIs from running instances and copy them cross-region** -- building golden images that combine the base OS with your user data customizations for faster, more reliable launches.

## Summary

- **User data** scripts run once on first boot as root -- they do NOT re-run on stop/start
- Scripts must start with `#!/bin/bash` (or another shebang) -- without it, cloud-init ignores the script
- Maximum user data size is **16 KB** before base64 encoding
- **Launch templates** support versioning (`$Latest`, `$Default`) -- launch configurations are immutable and legacy
- Launch templates are required for mixed instances policies (Spot + On-Demand), T3 unlimited mode, and advanced networking
- Check `/var/log/cloud-init-output.log` to debug user data failures -- instances launch even when scripts fail
- Use `set -e` in user data scripts to fail fast and make errors visible
- For ongoing configuration (not just first boot), use **SSM State Manager** or **cloud-init per-boot modules**

## Reference

- [EC2 User Data](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/user-data.html)
- [Launch Templates](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-launch-templates.html)
- [Terraform aws_launch_template](https://registry.terraform.io/providers/hashicorp/aws/latest/docs/resources/launch_template)
- [Cloud-Init Documentation](https://cloudinit.readthedocs.io/en/latest/)

## Additional Resources

- [Instance Metadata Service v2 (IMDSv2)](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/configuring-instance-metadata-service.html) -- secure token-based metadata access used in the user data script
- [Launch Template Versioning](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/manage-launch-template-versions.html) -- creating, setting default, and deleting template versions
- [ASG Instance Refresh](https://docs.aws.amazon.com/autoscaling/ec2/userguide/asg-instance-refresh.html) -- rolling out new launch template versions across an ASG
- [cloud-init Modules](https://cloudinit.readthedocs.io/en/latest/reference/modules.html) -- per-boot, per-instance, and per-once execution modes
