# 3. Ingress TLS Termination

<!--
difficulty: basic
concepts: [tls, ssl-termination, kubernetes-secret, certificate, https]
tools: [kubectl, minikube, helm, openssl]
estimated_time: 30m
bloom_level: understand
prerequisites: [06-01, 06-02]
-->

## Prerequisites

- A running Kubernetes cluster with the nginx Ingress Controller installed
- `kubectl` installed and configured
- `openssl` available on your machine
- Completion of [exercise 02 (Ingress Host-Based and Path-Based Routing)](../02-ingress-host-and-path-routing/02-ingress-host-and-path-routing.md)

## Learning Objectives

By the end of this exercise you will be able to:

- **Understand** how TLS termination works at the Ingress layer
- **Create** a self-signed TLS certificate and store it as a Kubernetes Secret
- **Configure** an Ingress resource to serve HTTPS and redirect HTTP to HTTPS

## Why TLS Termination at Ingress?

In production, every public-facing endpoint must serve HTTPS. Instead of configuring TLS in every individual application, you terminate TLS at the Ingress Controller. The controller handles certificate management, TLS handshakes, and cipher negotiation. Backend Services receive plain HTTP traffic over the internal cluster network, simplifying application code and centralizing certificate rotation.

This pattern separates security concerns (TLS) from application concerns (business logic). The Ingress Controller acts as the TLS endpoint, and certificates are stored in Kubernetes Secrets of type `kubernetes.io/tls`.

## Step 1: Generate a Self-Signed Certificate

For production, use cert-manager or a real CA. For this exercise, create a self-signed certificate:

```bash
openssl req -x509 -nodes -days 365 -newkey rsa:2048 \
  -keyout tls.key \
  -out tls.crt \
  -subj "/CN=secure.example.local/O=k8s-exercise" \
  -addext "subjectAltName=DNS:secure.example.local"
```

## Step 2: Create a TLS Secret

```bash
kubectl create secret tls secure-tls \
  --cert=tls.crt \
  --key=tls.key
```

Verify the Secret:

```bash
kubectl get secret secure-tls
kubectl describe secret secure-tls
```

The Secret type should be `kubernetes.io/tls` with `tls.crt` and `tls.key` data entries.

## Step 3: Deploy a Backend Application

```yaml
# app.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: secure-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: secure-app
  template:
    metadata:
      labels:
        app: secure-app
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80       # Backend serves plain HTTP
          command: ["/bin/sh", "-c"]
          args:
            - |
              echo '{"service":"secure-app","tls":"terminated-at-ingress","pod":"'$(hostname)'"}' > /usr/share/nginx/html/index.html
              nginx -g "daemon off;"
---
apiVersion: v1
kind: Service
metadata:
  name: secure-app-svc
spec:
  selector:
    app: secure-app
  ports:
    - port: 80
      targetPort: 80
      name: http
```

```bash
kubectl apply -f app.yaml
```

## Step 4: Create an Ingress with TLS

```yaml
# ingress-tls.yaml
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: secure-ingress
  annotations:
    nginx.ingress.kubernetes.io/ssl-redirect: "true"    # Force HTTPS redirect
spec:
  ingressClassName: nginx
  tls:
    - hosts:
        - secure.example.local        # Host this certificate covers
      secretName: secure-tls           # References the TLS Secret
  rules:
    - host: secure.example.local
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: secure-app-svc
                port:
                  number: 80
```

```bash
kubectl apply -f ingress-tls.yaml
```

## Step 5: Test HTTPS Access

```bash
INGRESS_HTTPS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')

# HTTPS request (skip certificate verification for self-signed cert)
curl -sk -H "Host: secure.example.local" https://$NODE_IP:$INGRESS_HTTPS_PORT/
```

Expected: JSON response from secure-app.

Test HTTP redirect:

```bash
INGRESS_HTTP_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
curl -s -I -H "Host: secure.example.local" http://$NODE_IP:$INGRESS_HTTP_PORT/
```

Expected: `308 Permanent Redirect` with `Location: https://secure.example.local/`.

## Common Mistakes

### Mistake 1: Secret Type Is Not kubernetes.io/tls

If you create a generic Secret instead of a TLS Secret, the Ingress Controller cannot read the certificate. Always use `kubectl create secret tls` or set `type: kubernetes.io/tls` in the YAML.

### Mistake 2: TLS Host Does Not Match Rules Host

The host in the `tls` section must match the host in the `rules` section. A mismatch means the controller uses its default (fake) certificate instead of yours.

### Mistake 3: Certificate SAN Does Not Match Hostname

Modern browsers and curl require the certificate's Subject Alternative Name (SAN) to match the requested hostname. The Common Name (CN) alone is no longer sufficient.

## Verify What You Learned

```bash
# Ingress shows TLS configuration
kubectl describe ingress secure-ingress

# Secret exists with correct type
kubectl get secret secure-tls -o jsonpath='{.type}'

# HTTPS responds
INGRESS_HTTPS_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="https")].nodePort}')
NODE_IP=$(kubectl get nodes -o jsonpath='{.items[0].status.addresses[?(@.type=="InternalIP")].address}')
curl -sk -H "Host: secure.example.local" https://$NODE_IP:$INGRESS_HTTPS_PORT/

# HTTP redirects to HTTPS
INGRESS_HTTP_PORT=$(kubectl get svc -n ingress-nginx ingress-nginx-controller -o jsonpath='{.spec.ports[?(@.name=="http")].nodePort}')
curl -s -o /dev/null -w "%{http_code}" -H "Host: secure.example.local" http://$NODE_IP:$INGRESS_HTTP_PORT/
```

## Cleanup

```bash
kubectl delete ingress secure-ingress
kubectl delete deployment secure-app
kubectl delete svc secure-app-svc
kubectl delete secret secure-tls
rm -f tls.crt tls.key
```

## What's Next

TLS termination is just one of many behaviors you can configure through Ingress annotations. In [exercise 04 (Ingress Annotations)](../04-ingress-annotations/04-ingress-annotations.md), you will explore rate limiting, CORS headers, and URL rewrites.

## Summary

- **TLS termination** at the Ingress layer centralizes certificate management and simplifies backend Services.
- Certificates are stored as **kubernetes.io/tls** Secrets containing `tls.crt` and `tls.key`.
- The `tls` section in the Ingress resource maps hostnames to certificate Secrets.
- `ssl-redirect: "true"` forces HTTP-to-HTTPS redirection.
- Certificate SANs must match the Ingress host for browsers and clients to accept the connection.
- For production, use **cert-manager** to automate certificate issuance and renewal.

## Reference

- [Ingress TLS](https://kubernetes.io/docs/concepts/services-networking/ingress/#tls) -- TLS configuration
- [TLS Secrets](https://kubernetes.io/docs/concepts/configuration/secret/#tls-secrets) -- Secret type for certificates

## Additional Resources

- [cert-manager](https://cert-manager.io/docs/) -- automated certificate management for Kubernetes
- [nginx-ingress TLS](https://kubernetes.github.io/ingress-nginx/user-guide/tls/) -- controller-specific TLS options
