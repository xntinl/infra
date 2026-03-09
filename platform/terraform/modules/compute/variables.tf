variable "function_name" {
  type        = string
  description = "Name of the Lambda function"
}

variable "description" {
  type        = string
  description = "Description of the Lambda function"
  default     = ""
}

variable "zip_path" {
  type        = string
  description = "Path to the Lambda deployment zip file"
}

variable "timeout" {
  type        = number
  description = "Lambda timeout in seconds"
  default     = 10
}

variable "memory_size" {
  type        = number
  description = "Lambda memory size in MB"
  default     = 128
}

variable "environment_variables" {
  type        = map(string)
  description = "Environment variables for the Lambda function"
  default     = {}
}

variable "tags" {
  type        = map(string)
  description = "Tags to apply to all resources"
  default     = {}
}
