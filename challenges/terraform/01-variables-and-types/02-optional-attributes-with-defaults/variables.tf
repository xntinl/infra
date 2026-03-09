variable "services" {
  type = map(object({
    port        = number
    protocol    = optional(string, "tcp")
    health_path = optional(string, "/health")
    replicas    = optional(number, 1)
    public      = optional(bool, false)
  }))
}
