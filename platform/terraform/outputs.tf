output "api_endpoint" {
  value = module.api.api_endpoint
}

output "function_arns" {
  value = { for k, v in module.compute : k => v.function_arn }
}

output "event_bus_arn" {
  value = module.events.bus_arn
}

output "workflow_arns" {
  value = { for k, v in module.workflow : k => v.state_machine_arn }
}
