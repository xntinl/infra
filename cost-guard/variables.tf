variable "region" {
  type    = string
  default = "us-east-1"
}

variable "project_name" {
  type    = string
  default = "cost-guard"
}

variable "alert_email" {
  type        = string
  description = "Email to receive budget alerts"
}

variable "monthly_limit" {
  type        = string
  default     = "5"
  description = "Monthly budget limit in USD"
}
