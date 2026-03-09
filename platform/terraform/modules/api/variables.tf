variable "api_name" {
  type        = string
  description = "Name of the HTTP API"
}

variable "stage_name" {
  type        = string
  description = "Name of the API stage"
}

variable "routes" {
  type = map(object({
    lambda_invoke_arn    = string
    lambda_function_name = string
  }))
  description = "Map of route_key => Lambda integration (e.g. 'GET /hello')"
}

variable "tags" {
  type        = map(string)
  description = "Tags to apply to all resources"
  default     = {}
}
