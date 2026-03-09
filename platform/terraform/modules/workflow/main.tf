data "aws_iam_policy_document" "assume_role" {
  statement {
    effect  = "Allow"
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["states.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "${var.workflow_name}-role"
  assume_role_policy = data.aws_iam_policy_document.assume_role.json
  tags               = var.tags
}

data "aws_iam_policy_document" "invoke_lambda" {
  statement {
    effect    = "Allow"
    actions   = ["lambda:InvokeFunction"]
    resources = var.lambda_arns
  }
}

resource "aws_iam_role_policy" "invoke_lambda" {
  name   = "invoke-lambda"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.invoke_lambda.json
}

data "aws_iam_policy_document" "logs" {
  statement {
    effect = "Allow"
    actions = [
      "logs:CreateLogDelivery",
      "logs:CreateLogStream",
      "logs:GetLogDelivery",
      "logs:UpdateLogDelivery",
      "logs:DeleteLogDelivery",
      "logs:ListLogDeliveries",
      "logs:PutLogEvents",
      "logs:PutResourcePolicy",
      "logs:DescribeResourcePolicies",
      "logs:DescribeLogGroups",
    ]
    resources = ["*"]
  }
}

resource "aws_iam_role_policy" "logs" {
  name   = "cloudwatch-logs"
  role   = aws_iam_role.this.id
  policy = data.aws_iam_policy_document.logs.json
}

resource "aws_cloudwatch_log_group" "this" {
  name              = "/aws/states/${var.workflow_name}"
  retention_in_days = 14
  tags              = var.tags
}

resource "aws_sfn_state_machine" "this" {
  name     = var.workflow_name
  role_arn = aws_iam_role.this.arn
  type     = var.type

  definition = var.definition

  logging_configuration {
    log_destination        = "${aws_cloudwatch_log_group.this.arn}:*"
    include_execution_data = true
    level                  = var.log_level
  }

  tags = var.tags

  depends_on = [
    aws_iam_role_policy.invoke_lambda,
    aws_iam_role_policy.logs,
    aws_cloudwatch_log_group.this,
  ]
}
