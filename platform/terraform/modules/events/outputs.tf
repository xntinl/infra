output "bus_name" {
  value = aws_cloudwatch_event_bus.this.name
}

output "bus_arn" {
  value = aws_cloudwatch_event_bus.this.arn
}

output "rule_arns" {
  value = { for k, v in aws_cloudwatch_event_rule.this : k => v.arn }
}
