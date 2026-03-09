resource "aws_cloudwatch_event_bus" "this" {
  name = var.bus_name
  tags = var.tags
}

resource "aws_cloudwatch_event_rule" "this" {
  for_each = var.rules

  name                = each.key
  description         = each.value.description
  schedule_expression = each.value.schedule_expression
  event_pattern       = each.value.event_pattern
  event_bus_name      = each.value.schedule_expression != null ? "default" : aws_cloudwatch_event_bus.this.name
  tags                = var.tags
}

resource "aws_cloudwatch_event_target" "this" {
  for_each = var.rules

  rule           = aws_cloudwatch_event_rule.this[each.key].name
  event_bus_name = aws_cloudwatch_event_rule.this[each.key].event_bus_name
  target_id      = "${each.key}-lambda"
  arn            = each.value.lambda_arn
}

resource "aws_lambda_permission" "eventbridge" {
  for_each = var.rules

  statement_id  = "AllowEventBridge-${each.key}"
  action        = "lambda:InvokeFunction"
  function_name = each.value.lambda_function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.this[each.key].arn
}
