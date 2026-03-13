# Certificate Expiry and Rotation

<!--
difficulty: intermediate
concepts: [pki, certificates, kubeadm-certs, tls, certificate-rotation, ca]
tools: [kubeadm, openssl, kubectl]
estimated_time: 30m
bloom_level: apply
prerequisites: [01-kubeadm-cluster-setup]
-->

## Overview

Kubernetes clusters bootstrapped with kubeadm use a PKI (Public Key Infrastructure) for component authentication. Certificates expire after one year by default. This exercise covers inspecting certificate expiry, renewing certificates, and understanding the PKI structure.

## Why This Matters

Expired certificates cause cluster outages -- the API server, kubelet, and other components refuse connections when certificates are invalid. Understanding the PKI and knowing how to renew certificates before expiry prevents unplanned downtime.

## Step-by-Step Instructions

### Step 1 -- Inspect the Cluster PKI

```bash
# List all certificates managed by kubeadm
sudo kubeadm certs check-expiration

# Examine the PKI directory
sudo ls -la /etc/kubernetes/pki/
sudo ls -la /etc/kubernetes/pki/etcd/
```

Key certificates in `/etc/kubernetes/pki/`:

| File | Purpose |
|------|---------|
| `ca.crt` / `ca.key` | Cluster CA (10-year validity) |
| `apiserver.crt` | API server serving certificate |
| `apiserver-kubelet-client.crt` | API server -> kubelet client cert |
| `front-proxy-ca.crt` | Front proxy CA |
| `etcd/ca.crt` | etcd CA |
| `etcd/server.crt` | etcd server certificate |

### Step 2 -- Check Certificate Details with OpenSSL

```bash
# Check API server certificate expiry and SANs
sudo openssl x509 -in /etc/kubernetes/pki/apiserver.crt -noout -dates -subject -ext subjectAltName

# Check the CA certificate (valid for 10 years)
sudo openssl x509 -in /etc/kubernetes/pki/ca.crt -noout -dates -subject

# Check etcd server certificate
sudo openssl x509 -in /etc/kubernetes/pki/etcd/server.crt -noout -dates

# Verify a certificate against its CA
sudo openssl verify -CAfile /etc/kubernetes/pki/ca.crt /etc/kubernetes/pki/apiserver.crt
```

### Step 3 -- Renew All Certificates

```bash
# Renew all kubeadm-managed certificates
sudo kubeadm certs renew all

# Verify new expiry dates
sudo kubeadm certs check-expiration
```

### Step 4 -- Restart Control Plane Components

After renewal, the control plane static pods must be restarted to pick up new certificates.

```bash
# Restart by moving manifests out and back (kubeadm approach)
sudo mv /etc/kubernetes/manifests/kube-apiserver.yaml /tmp/
sleep 10
sudo mv /tmp/kube-apiserver.yaml /etc/kubernetes/manifests/

sudo mv /etc/kubernetes/manifests/kube-controller-manager.yaml /tmp/
sleep 10
sudo mv /tmp/kube-controller-manager.yaml /etc/kubernetes/manifests/

sudo mv /etc/kubernetes/manifests/kube-scheduler.yaml /tmp/
sleep 10
sudo mv /tmp/kube-scheduler.yaml /etc/kubernetes/manifests/

# Wait for the control plane to stabilize
sleep 30
kubectl get pods -n kube-system
```

### Step 5 -- Renew Individual Certificates

```bash
# Renew only the API server certificate
sudo kubeadm certs renew apiserver

# Renew only the kubelet client certificate
sudo kubeadm certs renew apiserver-kubelet-client

# List available certificate targets
sudo kubeadm certs renew --help
```

### Step 6 -- Update kubeconfig Files

kubeadm also manages kubeconfig files that contain embedded client certificates.

```bash
# Regenerate admin kubeconfig
sudo kubeadm certs renew admin.conf

# Copy the updated kubeconfig
sudo cp /etc/kubernetes/admin.conf $HOME/.kube/config
sudo chown $(id -u):$(id -g) $HOME/.kube/config

# Verify connectivity
kubectl get nodes
```

## TODO

Set up a cron job that checks certificate expiry and alerts when certificates are within 30 days of expiration.

<details>
<summary>Hint</summary>

```bash
# /etc/cron.daily/check-k8s-certs
#!/bin/bash
EXPIRY=$(sudo kubeadm certs check-expiration 2>/dev/null | grep -v "CERTIFICATE" | grep -v "^$" | awk '{print $NF}')
for date in $EXPIRY; do
  expiry_epoch=$(date -d "$date" +%s 2>/dev/null)
  now_epoch=$(date +%s)
  days_left=$(( (expiry_epoch - now_epoch) / 86400 ))
  if [ "$days_left" -lt 30 ]; then
    echo "WARNING: Kubernetes certificate expires in $days_left days" | logger -t k8s-certs
  fi
done
```
</details>

## Verify

```bash
# All certificates should show renewed expiry dates (roughly 1 year from now)
sudo kubeadm certs check-expiration

# Cluster should be functional
kubectl get nodes
kubectl get pods -n kube-system

# API server certificate should have a recent "Not Before" date
sudo openssl x509 -in /etc/kubernetes/pki/apiserver.crt -noout -dates
```

## Cleanup

No cleanup needed -- the renewed certificates are an improvement over the old ones.

## Reference

- [PKI certificates and requirements](https://kubernetes.io/docs/setup/best-practices/certificates/)
- [Certificate Management with kubeadm](https://kubernetes.io/docs/tasks/administer-cluster/kubeadm/kubeadm-certs/)
