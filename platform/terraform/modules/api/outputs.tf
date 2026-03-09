output "api_id" {
  value = aws_apigatewayv2_api.this.id
}

output "api_endpoint" {
  value = aws_apigatewayv2_stage.this.invoke_url
}

output "execution_arn" {
  value = aws_apigatewayv2_api.this.execution_arn
}

output "stage_name" {
  value = aws_apigatewayv2_stage.this.name
}
