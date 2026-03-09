# 16.14 OpenTelemetry Auto-Instrumentation with Operator

<!--
difficulty: advanced
concepts: [otel-operator, auto-instrumentation, sidecar-injection, instrumentation-crd, sdk-injection]
tools: [kubectl, helm]
estimated_time: 35m
bloom_level: analyze
prerequisites: [distributed-tracing-opentelemetry, opentelemetry-collector]
-->

## Architecture

```
+-----------------------------------+
| OpenTelemetry Operator            |
| watches Instrumentation CRDs      |
+-----------------------------------+
         |
         | mutating webhook injects
         | init-container + env vars
         v
+-----------------------------------+       +------------------+       +----------+
| Application Pod                   |       | OTel Collector   |       | Jaeger   |
| init: copy agent into shared vol  | ----> | (Deployment)     | ----> | Backend  |
| app: runs with OTel agent loaded  |  OTLP |                  |       |          |
+-----------------------------------+       +------------------+       +----------+
```

The OpenTelemetry Operator uses a Kubernetes mutating webhook to automatically
inject instrumentation (Java agent, Python SDK, Node.js SDK, .NET agent) into
application Pods. No code changes are needed -- just annotate your workload.

## What You Will Build

- OpenTelemetry Operator via Helm
- An Instrumentation CR that configures auto-instrumentation
- An OTel Collector to receive telemetry
- A sample application annotated for auto-instrumentation
- Jaeger for trace visualization

## Suggested Steps

1. Create namespace `otel-auto`.

2. Install cert-manager (required by the Operator):

```bash
kubectl apply -f https://github.com/cert-manager/cert-manager/releases/latest/download/cert-manager.yaml
kubectl wait --for=condition=Available deployment/cert-manager -n cert-manager --timeout=120s
```

3. Install the OpenTelemetry Operator:

```bash
helm repo add open-telemetry https://open-telemetry.github.io/opentelemetry-helm-charts
helm repo update
helm install otel-operator open-telemetry/opentelemetry-operator \
  --namespace otel-auto --create-namespace \
  --set manager.collectorImage.repository=otel/opentelemetry-collector-contrib \
  --wait
```

4. Deploy Jaeger and an OTel Collector (reuse from exercise 16.11).

5. Create an Instrumentation CR:

```yaml
# instrumentation.yaml
apiVersion: opentelemetry.io/v1alpha1
kind: Instrumentation
metadata:
  name: auto-instrumentation
  namespace: otel-auto
spec:
  exporter:
    endpoint: http://otel-collector.otel-auto.svc.cluster.local:4317
  propagators:
    - tracecontext
    - baggage
  sampler:
    type: parentbased_traceidratio
    argument: "1.0"
  python:
    image: ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-python:latest
  nodejs:
    image: ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-nodejs:latest
  java:
    image: ghcr.io/open-telemetry/opentelemetry-operator/autoinstrumentation-java:latest
```

6. Deploy a sample application with the auto-instrumentation annotation:

```yaml
# sample-app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: sample-app
  namespace: otel-auto
spec:
  replicas: 1
  selector:
    matchLabels:
      app: sample-app
  template:
    metadata:
      labels:
        app: sample-app
      annotations:
        instrumentation.opentelemetry.io/inject-python: "true"
    spec:
      containers:
        - name: app
          image: python:3.12-slim
          command: ["python", "-c"]
          args:
            - |
              from http.server import HTTPServer, BaseHTTPRequestHandler
              class Handler(BaseHTTPRequestHandler):
                  def do_GET(self):
                      self.send_response(200)
                      self.end_headers()
                      self.wfile.write(b"Hello from auto-instrumented app\n")
              HTTPServer(("", 8080), Handler).serve_forever()
          ports:
            - containerPort: 8080
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
            limits:
              cpu: 200m
              memory: 128Mi
```

7. Generate traffic and observe auto-generated traces in Jaeger.

## Verify

```bash
# Operator running
kubectl get pods -n otel-auto -l app.kubernetes.io/name=opentelemetry-operator

# Instrumentation CR created
kubectl get instrumentation -n otel-auto

# Check that the operator injected init container
kubectl get pod -n otel-auto -l app=sample-app -o jsonpath='{.items[0].spec.initContainers[*].name}'
# Should include opentelemetry-auto-instrumentation

# Check injected environment variables
kubectl get pod -n otel-auto -l app=sample-app -o jsonpath='{.items[0].spec.containers[0].env[*].name}' | tr ' ' '\n' | grep OTEL

# Generate traffic and check traces in Jaeger
kubectl port-forward -n otel-auto svc/sample-app 8080:8080 &
curl http://localhost:8080/
kubectl port-forward -n otel-auto svc/jaeger 16686:16686 &
curl -s "http://localhost:16686/api/services" | python3 -m json.tool
```

## Cleanup

```bash
kubectl delete namespace otel-auto
helm uninstall otel-operator -n otel-auto 2>/dev/null
```

## References

- [OpenTelemetry Operator](https://github.com/open-telemetry/opentelemetry-operator)
- [Auto-Instrumentation](https://opentelemetry.io/docs/kubernetes/operator/automatic/)
- [Instrumentation API Reference](https://github.com/open-telemetry/opentelemetry-operator/blob/main/docs/api.md)
- [cert-manager Installation](https://cert-manager.io/docs/installation/)
