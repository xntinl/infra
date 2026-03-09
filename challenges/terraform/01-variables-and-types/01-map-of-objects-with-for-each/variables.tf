variable "environments" {
  type = map(object({
    instance_type = string
    min_size      = number
    max_size      = number
    enable_https  = bool
  }))
}
