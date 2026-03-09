
output "instance_public_ip" {
  description = "public ip of the ec2 instance"
  value       = aws_instance.this.public_ip
}

output "ssg_command" {
  description = "ready to use ssh command"
  value       = "ssh -i my-key.pem ec2-user@${aws_instance.this.public_ip}"
}

output "ami_used" {
  description = "AMI ID that was resolved by the data source"
  value       = data.aws_ami.amazon_linux_2023.id
}



