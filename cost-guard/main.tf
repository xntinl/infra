resource "aws_budgets_budget" "this" {
  name         = var.project_name
  budget_type  = "COST"
  limit_amount = var.monthly_limit
  limit_unit   = "USD"
  time_unit    = "MONTHLY"

  cost_types {
    include_tax          = true
    include_subscription = true
    include_support      = true
    include_credit       = false
    include_refund       = false
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 50
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 80
    threshold_type             = "PERCENTAGE"
    notification_type          = "ACTUAL"
    subscriber_email_addresses = [var.alert_email]
  }

  notification {
    comparison_operator        = "GREATER_THAN"
    threshold                  = 100
    threshold_type             = "PERCENTAGE"
    notification_type          = "FORECASTED"
    subscriber_email_addresses = [var.alert_email]
  }
}
