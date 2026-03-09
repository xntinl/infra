environments = {
  dev = {
    instance_type = "t3.micro"
    min_size      = 1
    max_size      = 2
    enable_https  = false
  }
  staging = {
    instance_type = "t3.small"
    min_size      = 2
    max_size      = 4
    enable_https  = false
  }
  prod = {
    instance_type = "t3.medium"
    min_size      = 3
    max_size      = 10
    enable_https  = true
  }
}
