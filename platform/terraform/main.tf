locals {
  config_path      = "${path.module}/../config"
  functions_config = yamldecode(file("${local.config_path}/functions.yaml"))
  routes_config    = yamldecode(file("${local.config_path}/routes.yaml"))
  schedules_config = yamldecode(file("${local.config_path}/schedules.yaml"))
  workflows_config = yamldecode(file("${local.config_path}/workflows.yaml"))

  lambda_base_path = "${path.module}/../target/lambda"

  functions = {
    for name, fn in local.functions_config.functions : name => {
      description = fn.description
      zip_path    = "${local.lambda_base_path}/${name}/bootstrap.zip"
      timeout     = fn.timeout
      memory_size = fn.memory_size
      env_vars    = try(fn.env_vars, {})
    }
  }

  api_routes = {
    for key, route in local.routes_config.routes :
    "${upper(route.method)} ${route.path}" => {
      lambda_invoke_arn    = module.compute[route.function].invoke_arn
      lambda_function_name = module.compute[route.function].function_name
    }
  }

  schedule_rules = {
    for name, schedule in local.schedules_config.schedules : name => {
      description          = schedule.description
      schedule_expression  = schedule.expression
      lambda_arn           = module.compute[schedule.function].function_arn
      lambda_function_name = module.compute[schedule.function].function_name
    }
  }

  workflows = {
    for name, wf in local.workflows_config.workflows : name => {
      description     = wf.description
      type            = try(wf.type, "STANDARD")
      log_level       = try(wf.log_level, "ERROR")
      definition_file = wf.definition_file
      functions       = wf.functions
    }
  }

  common_tags = {
    Environment = terraform.workspace
    Project     = var.project_name
  }
}

module "compute" {
  source   = "./modules/compute"
  for_each = local.functions

  function_name         = "${var.project_name}-${terraform.workspace}-${each.key}"
  description           = each.value.description
  zip_path              = each.value.zip_path
  timeout               = each.value.timeout
  memory_size           = each.value.memory_size
  environment_variables = each.value.env_vars
  tags                  = local.common_tags
}

module "api" {
  source = "./modules/api"

  api_name   = "${var.project_name}-${terraform.workspace}"
  stage_name = "$default"
  routes     = local.api_routes
  tags       = local.common_tags
}

module "events" {
  source = "./modules/events"

  bus_name = "${var.project_name}-${terraform.workspace}"
  rules    = local.schedule_rules
  tags     = local.common_tags
}

module "workflow" {
  source   = "./modules/workflow"
  for_each = local.workflows

  workflow_name = "${var.project_name}-${terraform.workspace}-${each.key}"
  definition = templatefile(
    "${local.config_path}/workflows/${each.value.definition_file}",
    { for fn in each.value.functions :
    "${replace(fn, "-", "_")}_arn" => module.compute[fn].function_arn }
  )
  lambda_arns = [for fn in each.value.functions : module.compute[fn].function_arn]
  type        = each.value.type
  log_level   = each.value.log_level
  tags        = local.common_tags
}
