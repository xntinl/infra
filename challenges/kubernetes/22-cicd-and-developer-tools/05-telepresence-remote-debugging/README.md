# Telepresence: Remote Debugging in Kubernetes

<!--
difficulty: intermediate
concepts: [telepresence, remote-debugging, traffic-interception, local-development, service-mesh-bypass]
tools: [telepresence, kubectl, docker]
estimated_time: 25m
bloom_level: apply
prerequisites: [kubectl-basics, services, deployments]
-->

## Overview

Telepresence connects your local development machine to a remote Kubernetes cluster. It intercepts traffic destined for a service in the cluster and routes it to your local process. This means you can debug a service locally with your IDE and debugger, while it communicates with other services running in the cluster as if it were deployed there.

## Why This Matters

Reproducing bugs locally is hard when a service depends on databases, message queues, and other microservices running in the cluster. Telepresence lets you run a single service locally with full access to the cluster network, without replicating the entire environment on your machine.

## Step-by-Step Instructions

### Step 1 -- Install Telepresence

```bash
# macOS
brew install datawire/blackbird/telepresence2

# Linux
sudo curl -fL https://app.getambassador.io/download/tel2oss/releases/download/v2.18.0/telepresence-linux-amd64 \
  -o /usr/local/bin/telepresence
sudo chmod +x /usr/local/bin/telepresence

telepresence version
```

### Step 2 -- Deploy Test Services

```yaml
# microservices.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: telepresence-demo
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: frontend
  namespace: telepresence-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: frontend
  template:
    metadata:
      labels:
        app: frontend
    spec:
      containers:
        - name: frontend
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: frontend
  namespace: telepresence-demo
spec:
  selector:
    app: frontend
  ports:
    - port: 80
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: api
  namespace: telepresence-demo
spec:
  replicas: 1
  selector:
    matchLabels:
      app: api
  template:
    metadata:
      labels:
        app: api
    spec:
      containers:
        - name: api
          image: nginx:1.27
          ports:
            - containerPort: 80
          resources:
            requests:
              cpu: 50m
              memory: 64Mi
---
apiVersion: v1
kind: Service
metadata:
  name: api
  namespace: telepresence-demo
spec:
  selector:
    app: api
  ports:
    - port: 80
      targetPort: 80
```

```bash
kubectl apply -f microservices.yaml
kubectl wait --for=condition=ready pod -l app=frontend -n telepresence-demo --timeout=60s
kubectl wait --for=condition=ready pod -l app=api -n telepresence-demo --timeout=60s
```

### Step 3 -- Connect to the Cluster

```bash
# Connect Telepresence to the cluster
telepresence connect

# Verify connectivity -- you can now access cluster services by DNS name
curl http://api.telepresence-demo.svc.cluster.local
curl http://frontend.telepresence-demo.svc.cluster.local
```

### Step 4 -- Intercept a Service

Intercept routes traffic meant for the cluster service to your local process.

```bash
# Start a local version of the API service
# (In a real scenario, this would be your service running in your IDE)
python3 -m http.server 8080 &
LOCAL_PID=$!

# Intercept the api service in the cluster
telepresence intercept api \
  --namespace telepresence-demo \
  --port 8080:80 \
  --env-file /tmp/api-env.sh

# Now, traffic to api.telepresence-demo:80 in the cluster goes to localhost:8080
# Test from another pod in the cluster:
kubectl run test-curl --image=busybox:1.37 -n telepresence-demo --restart=Never \
  --command -- wget -qO- http://api.telepresence-demo.svc.cluster.local
kubectl logs test-curl -n telepresence-demo
kubectl delete pod test-curl -n telepresence-demo

# Or directly from your machine:
curl http://api.telepresence-demo.svc.cluster.local
```

### Step 5 -- Personal Intercepts (Preview URLs)

Personal intercepts route only specific requests to your local machine (based on headers), while other traffic continues to the cluster version.

```bash
# Create a personal intercept with header matching
telepresence intercept api \
  --namespace telepresence-demo \
  --port 8080:80 \
  --http-header x-debug-user=alice

# Only requests with "x-debug-user: alice" header go to local
# All other requests go to the cluster version
curl -H "x-debug-user: alice" http://api.telepresence-demo.svc.cluster.local  # goes to local
curl http://api.telepresence-demo.svc.cluster.local  # goes to cluster
```

### Step 6 -- Access Environment Variables and Volumes

```bash
# The --env-file flag exports the pod's environment variables
cat /tmp/api-env.sh
# source /tmp/api-env.sh to set them in your shell

# View active intercepts
telepresence list -n telepresence-demo

# Leave the intercept
telepresence leave api-telepresence-demo
```

## Verify

```bash
# Telepresence is connected
telepresence status

# Cluster DNS resolution works from local machine
nslookup api.telepresence-demo.svc.cluster.local

# Intercept is active
telepresence list -n telepresence-demo
```

## Cleanup

```bash
# Stop the local server
kill $LOCAL_PID 2>/dev/null

# Leave all intercepts
telepresence leave api-telepresence-demo 2>/dev/null

# Disconnect from the cluster
telepresence quit

# Delete test resources
kubectl delete namespace telepresence-demo

# Uninstall the traffic manager
telepresence uninstall --everything
```

## Reference

- [Telepresence Documentation](https://www.telepresence.io/docs/latest/)
- [Intercept a Service](https://www.telepresence.io/docs/latest/howtos/intercepts/)
- [Personal Intercepts](https://www.telepresence.io/docs/latest/howtos/personal-intercepts/)
