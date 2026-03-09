provider "aws" {
  region = "us-east-1"
}

terraform {
  required_version = ">= 1.7"
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.0"
    }
  }
}

resource "aws_ssm_parameter" "env_config" {
  for_each = var.environments
  name     = "/app/${each.key}/config"
  type     = "String"
  value    = jsonencode(each.value)
}
