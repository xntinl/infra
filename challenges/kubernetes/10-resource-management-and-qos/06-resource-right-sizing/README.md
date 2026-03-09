# 6. Resource Right-Sizing with VPA Recommendations

<!--
difficulty: intermediate
concepts: [vertical-pod-autoscaler, vpa, resource-recommendations, right-sizing, metrics-server]
tools: [kubectl, minikube, helm]
estimated_time: 40m
bloom_level: apply
prerequisites: [10-01, 10-02]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d) with metrics-server installed
- `kubectl` installed and configured
- Completion of [exercise 01](../01-resource-requests-and-limits/) and [exercise 02](../02-qos-classes/)

Enable metrics-server on minikube:

```bash
minikube addons enable metrics-server
kubectl top nodes
```

## Learning Objectives

After completing this exercise, you will be able to:

- **Apply** the Vertical Pod Autoscaler in recommendation-only mode to analyze resource usage
- **Analyze** VPA recommendations to identify over-provisioned and under-provisioned containers
- **Evaluate** when to use VPA versus manual tuning for resource right-sizing

## Why Resource Right-Sizing?

Most teams set resource requests and limits based on guesswork. This leads to two problems: over-provisioning wastes cluster capacity and money, while under-provisioning causes OOMKilled restarts and CPU throttling. The Vertical Pod Autoscaler (VPA) observes actual resource usage over time and recommends optimal values.

VPA has three modes: `Off` (recommendation only), `Initial` (applies recommendations on pod creation), and `Auto` (updates running pods). Start with `Off` to build confidence before enabling automatic updates.

## Step 1: Install the VPA

```bash
git clone https://github.com/kubernetes/autoscaler.git
cd autoscaler/vertical-pod-autoscaler
./hack/vpa-up.sh

# Verify installation
kubectl get pods -n kube-system | grep vpa
```

You should see `vpa-admission-controller`, `vpa-recommender`, and `vpa-updater` pods running.

## Step 2: Deploy a Workload with Intentionally Wrong Resources

```yaml
# deployment-over-provisioned.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: over-provisioned-app
spec:
  replicas: 2
  selector:
    matchLabels:
      app: over-provisioned
  template:
    metadata:
      labels:
        app: over-provisioned
    spec:
      containers:
        - name: app
          image: nginx:1.27
          resources:
            requests:
              cpu: "1"              # Way too high for nginx
              memory: "1Gi"         # Way too high for nginx
            limits:
              cpu: "2"
              memory: "2Gi"
```

```bash
kubectl apply -f deployment-over-provisioned.yaml
kubectl wait --for=condition=Available deployment/over-provisioned-app --timeout=60s
```

## Step 3: Create a VPA in Recommendation Mode

```yaml
# vpa-recommend.yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: vpa-over-provisioned
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: Deployment
    name: over-provisioned-app
  updatePolicy:
    updateMode: "Off"              # Recommendation only, no automatic changes
```

```bash
kubectl apply -f vpa-recommend.yaml
```

## Step 4: Generate Load and Wait for Recommendations

The VPA recommender needs a few minutes of metrics data. Generate some traffic:

```bash
kubectl expose deployment over-provisioned-app --port=80 --name=over-provisioned-svc
kubectl run load-gen --image=busybox:1.37 --restart=Never -- sh -c "while true; do wget -q -O- http://over-provisioned-svc; done"
```

Wait for recommendations (typically 2-5 minutes):

```bash
kubectl get vpa vpa-over-provisioned -o yaml
```

Look for the `recommendation` section with `target`, `lowerBound`, and `upperBound` values.

## Step 5: Interpret the Recommendations

```bash
kubectl get vpa vpa-over-provisioned -o jsonpath='{.status.recommendation.containerRecommendations[0]}' | python3 -m json.tool
```

The output contains:
- **target**: the recommended resource values
- **lowerBound**: the minimum recommended values
- **upperBound**: the maximum recommended values
- **uncappedTarget**: what VPA would recommend without any configured limits

Compare the VPA recommendation with your current settings:

```bash
echo "=== Current ==="
kubectl get deployment over-provisioned-app -o jsonpath='{.spec.template.spec.containers[0].resources}' | python3 -m json.tool
echo "=== VPA Recommendation ==="
kubectl get vpa vpa-over-provisioned -o jsonpath='{.status.recommendation.containerRecommendations[0].target}' | python3 -m json.tool
```

## Step 6: Apply Recommendations Manually

Based on the VPA recommendation, update the Deployment with right-sized values:

```bash
# Example: if VPA recommends cpu: 25m, memory: 50Mi
kubectl patch deployment over-provisioned-app -p '{"spec":{"template":{"spec":{"containers":[{"name":"app","resources":{"requests":{"cpu":"50m","memory":"64Mi"},"limits":{"cpu":"100m","memory":"128Mi"}}}]}}}}'
```

## Spot the Bug

This VPA is created but never produces recommendations. **Why?**

```yaml
apiVersion: autoscaling.k8s.io/v1
kind: VerticalPodAutoscaler
metadata:
  name: vpa-missing
spec:
  targetRef:
    apiVersion: "apps/v1"
    kind: Deployment
    name: nonexistent-app          # <-- BUG
  updatePolicy:
    updateMode: "Off"
```

<details>
<summary>Explanation</summary>

The `targetRef` references a Deployment called `nonexistent-app` that does not exist. VPA silently accepts the configuration but never produces recommendations because there are no pods to observe. Always verify the target exists and has running pods.

</details>

## Verify What You Learned

```bash
kubectl get vpa
kubectl top pods
kubectl get deployment over-provisioned-app -o jsonpath='{.spec.template.spec.containers[0].resources}'
```

## Cleanup

```bash
kubectl delete pod load-gen --ignore-not-found
kubectl delete service over-provisioned-svc --ignore-not-found
kubectl delete vpa vpa-over-provisioned --ignore-not-found
kubectl delete deployment over-provisioned-app --ignore-not-found
```

## What's Next

Now that you can right-size resources, the next exercise covers Priority Classes and how Kubernetes preempts lower-priority pods for critical workloads. Continue to [exercise 07 (Priority Classes and Preemption)](../07-priority-classes-and-preemption/).

## Summary

- **VPA** observes actual resource usage and recommends optimal requests and limits
- Use **`updateMode: Off`** to get recommendations without automatic changes
- VPA recommendations include **target**, **lowerBound**, and **upperBound** values
- Most workloads are **over-provisioned**; VPA typically recommends significantly lower values
- VPA requires **metrics-server** and needs several minutes of runtime data before producing recommendations

## Reference

- [Vertical Pod Autoscaler](https://github.com/kubernetes/autoscaler/tree/master/vertical-pod-autoscaler)
- [Resource Management for Pods and Containers](https://kubernetes.io/docs/concepts/configuration/manage-resources-containers/)
