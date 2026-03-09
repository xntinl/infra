<!--
difficulty: advanced
concepts: [karpenter, nodepool, ec2nodeclass, provisioner, consolidation, drift-detection]
tools: [kubectl, helm, karpenter]
estimated_time: 45m
bloom_level: analyze
prerequisites: [cluster-autoscaler, resource-requests-and-limits]
-->

# 14.11 - Karpenter: Intelligent Node Provisioning

## Architecture

```
  +-------------------+
  |   Pending Pods    |
  +--------+----------+
           |
  +--------v----------+
  |    Karpenter      |
  | - watches pods    |
  | - groups by       |
  |   constraints     |  Directly calls EC2 APIs (no ASG)
  | - selects optimal |
  |   instance type   |
  +--------+----------+
           |
  +--------v----------+     +-------------------+
  | EC2 Fleet API     | --> | Right-sized Node  |
  | (spot / on-demand)|     | (exact fit for    |
  +-------------------+     |  pending pods)    |
                            +-------------------+
```

Unlike Cluster Autoscaler which scales predefined node groups (ASGs), Karpenter evaluates pending pod requirements and directly provisions the optimal instance type. It can mix instance types, architectures (amd64/arm64), and capacity types (on-demand/spot) to minimize cost and latency.

## What You Will Learn

- How Karpenter's NodePool and EC2NodeClass replace ASG-based scaling
- How consolidation automatically replaces expensive nodes with cheaper alternatives
- How disruption budgets and `do-not-disrupt` annotations control node lifecycle
- How drift detection reprovisiones nodes when their spec changes

## Suggested Steps

1. Install Karpenter via Helm into the `kube-system` namespace
2. Create a NodePool with instance type constraints and consolidation enabled
3. Create an EC2NodeClass with subnet and security group selectors
4. Deploy workloads that trigger Karpenter to provision new nodes
5. Observe how Karpenter picks the optimal instance type
6. Test consolidation by scaling down workloads and watching node replacement
7. Test drift detection by updating the EC2NodeClass AMI family

### NodePool

```yaml
# nodepool.yaml
apiVersion: karpenter.sh/v1
kind: NodePool
metadata:
  name: default
spec:
  template:
    metadata:
      labels:
        managed-by: karpenter
    spec:
      requirements:
        - key: kubernetes.io/arch
          operator: In
          values: ["amd64", "arm64"]
        - key: karpenter.sh/capacity-type
          operator: In
          values: ["on-demand", "spot"]
        - key: node.kubernetes.io/instance-type
          operator: In
          values:
            - m5.large
            - m5.xlarge
            - m6g.large
            - m6g.xlarge
            - c5.large
            - c6g.large
      nodeClassRef:
        group: karpenter.k8s.aws
        kind: EC2NodeClass
        name: default
  disruption:
    consolidationPolicy: WhenEmptyOrUnderutilized
    consolidateAfter: 30s
  limits:
    cpu: "100"                     # total CPU across all Karpenter-managed nodes
    memory: 200Gi
```

### EC2NodeClass

```yaml
# ec2nodeclass.yaml
apiVersion: karpenter.k8s.aws/v1
kind: EC2NodeClass
metadata:
  name: default
spec:
  role: KarpenterNodeRole-my-cluster
  amiSelectorTerms:
    - alias: al2023@latest
  subnetSelectorTerms:
    - tags:
        karpenter.sh/discovery: my-cluster
  securityGroupSelectorTerms:
    - tags:
        karpenter.sh/discovery: my-cluster
  blockDeviceMappings:
    - deviceName: /dev/xvda
      ebs:
        volumeSize: 50Gi
        volumeType: gp3
        encrypted: true
```

### Workload to Test Provisioning

```yaml
# inflate.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: inflate
spec:
  replicas: 0
  selector:
    matchLabels:
      app: inflate
  template:
    metadata:
      labels:
        app: inflate
    spec:
      containers:
        - name: inflate
          image: nginx:1.27
          resources:
            requests:
              cpu: 500m
              memory: 512Mi
```

### Do-Not-Disrupt Annotation

```yaml
# critical-pod.yaml
apiVersion: v1
kind: Pod
metadata:
  name: critical-workload
  annotations:
    karpenter.sh/do-not-disrupt: "true"
spec:
  containers:
    - name: app
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 128Mi
```

## Verify

```bash
# 1. Check Karpenter pods
kubectl get pods -n kube-system -l app.kubernetes.io/name=karpenter

# 2. Check NodePool status
kubectl get nodepool default
kubectl describe nodepool default

# 3. Scale up and watch Karpenter provision nodes
kubectl scale deployment inflate --replicas=10
kubectl get nodes --watch
kubectl logs -n kube-system -l app.kubernetes.io/name=karpenter --tail=100

# 4. Check which instance types Karpenter selected
kubectl get nodes -L node.kubernetes.io/instance-type -L karpenter.sh/capacity-type

# 5. Scale down and observe consolidation
kubectl scale deployment inflate --replicas=2
kubectl get nodes --watch

# 6. Check NodePool resource usage
kubectl get nodepool default -o yaml | grep -A5 status
```

## Cleanup

```bash
kubectl delete deployment inflate
kubectl delete pod critical-workload --ignore-not-found
kubectl delete nodepool default
kubectl delete ec2nodeclass default
```

## What's Next

Scaling adds nodes and pods, but workloads can become unevenly distributed over time. The next exercise covers the Descheduler, which rebalances running pods across nodes: [14.12 - Descheduler: Rebalancing Workloads](../12-descheduler/).

## Summary

- Karpenter provisions right-sized nodes by evaluating pending pod constraints directly
- NodePool defines instance type requirements, capacity type preferences, and resource limits
- EC2NodeClass configures AWS-specific settings (AMIs, subnets, security groups)
- Consolidation replaces underutilized or empty nodes with more efficient alternatives
- `karpenter.sh/do-not-disrupt: "true"` prevents a pod's node from being consolidated
