variable "workflow_name" {
  type        = string
  description = "Name of the Step Functions state machine"
}

variable "definition" {
  type        = string
  description = "ASL JSON definition (pre-rendered via templatefile)"
}

variable "lambda_arns" {
  type        = list(string)
  description = "Lambda ARNs to scope IAM invoke permissions"
}

variable "type" {
  type        = string
  description = "State machine type"
  default     = "STANDARD"

  validation {
    condition     = contains(["STANDARD", "EXPRESS"], var.type)
    error_message = "Type must be STANDARD or EXPRESS."
  }
}

variable "log_level" {
  type        = string
  description = "Logging level for the state machine"
  default     = "ERROR"

  validation {
    condition     = contains(["OFF", "ALL", "ERROR", "FATAL"], var.log_level)
    error_message = "Log level must be OFF, ALL, ERROR, or FATAL."
  }
}

variable "tags" {
  type        = map(string)
  description = "Tags to apply to all resources"
  default     = {}
}
