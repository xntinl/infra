variable "region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}


variable "project_name" {
  description = "project name for resource naming and tagging"
  type        = string
  default     = "pub-priv"
}
