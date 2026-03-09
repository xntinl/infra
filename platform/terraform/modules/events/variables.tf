variable "bus_name" {
  type        = string
  description = "Name of the custom EventBridge bus"
}

variable "rules" {
  type = map(object({
    description          = optional(string, "")
    schedule_expression  = optional(string, null)
    event_pattern        = optional(string, null)
    lambda_arn           = string
    lambda_function_name = string
  }))
  description = "Map of rule_name => rule config. Use schedule_expression for schedules (default bus) or event_pattern for custom bus events."
}

variable "tags" {
  type        = map(string)
  description = "Tags to apply to all resources"
  default     = {}
}
