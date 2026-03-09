services = {
  api = {
    port     = 8080
    replicas = 3
    public   = true
  }
  worker = {
    port = 9090
  }
  grpc = {
    port        = 50051
    protocol    = "https"
    health_path = "/grpc.health.v1.Health/Check"
  }
}
