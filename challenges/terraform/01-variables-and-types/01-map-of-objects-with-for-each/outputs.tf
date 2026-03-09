
output "config_arns" {
  value = { for k, v in aws_ssm_parameter.env_config : k => v.arn }
}
