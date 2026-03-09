variable "project_name" {
  type = string
  validation {
    condition     = can(regex("^[a-z][a-z0-9-]{2,19}$", var.project_name))
    error_message = "Project name must be 3-20 chars, lowercase alphanumeric and hyphens, starting with a letter"
  }
}


variable "environment" {
  type = string
  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "Environment must be one of: dev, staging, prod."
  }
}

variable "instance_type" {
  type = string
  validation {
    condition     = can(regex("^t3\\.(micro|small|medium|large)$", var.instance_type))
    error_message = "Instance type must be t3.micro, t3.small, t3.medium, or t3.large."
  }
}

variable "port" {
  type = number
  validation {
    condition     = var.port >= 1024 && var.port <= 65535
    error_message = "Port must be between 1024 and 65535 (non-privileged)"
  }
}


variable "cidr_block" {
  type = string
  validation {
    condition     = can(cidrhost(var.cidr_block, 0))
    error_message = "Must be a valid CIDR block (e.g. 10.0.0.0/16)."
  }
}
