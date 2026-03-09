<!--
difficulty: advanced
concepts: [custom-scheduler, scheduler-extender, second-scheduler, scheduling-framework, leader-election]
tools: [kubectl, go, docker]
estimated_time: 45m
bloom_level: analyze
prerequisites: [scheduler-profiles, deployments, rbac]
-->

# 15.08 - Custom Schedulers: Deploying a Second Scheduler

## Architecture

```
  +--------------------+     +--------------------+
  |  default-scheduler |     |  custom-scheduler  |
  |  (kube-scheduler)  |     |  (your binary)     |
  +--------+-----------+     +--------+-----------+
           |                          |
           |  schedules pods with     |  schedules pods with
           |  schedulerName:          |  schedulerName:
           |  default-scheduler       |  custom-scheduler
           |                          |
  +--------v--------------------------v-----------+
  |                  API Server                    |
  |          (pod binding, node watch)             |
  +-----------------------+------------------------+
                          |
              +-----------v-----------+
              |    Worker Nodes       |
              +-----------------------+
```

Multiple schedulers can coexist in a cluster. The default scheduler handles pods without an explicit `schedulerName`. A custom scheduler handles pods that name it. This is useful for specialized scheduling logic -- GPU-aware placement, cost optimization, or gang scheduling for distributed ML jobs.

## What You Will Learn

- How to deploy a second scheduler as a Deployment in `kube-system`
- How RBAC requirements differ for custom schedulers
- How pods reference a custom scheduler via `spec.schedulerName`
- How leader election prevents conflicts when running multiple scheduler replicas

## Suggested Steps

1. Create ServiceAccount, ClusterRole, and ClusterRoleBinding for the custom scheduler
2. Deploy a second instance of `kube-scheduler` with a different `--scheduler-name` and config
3. Create pods that target the custom scheduler via `spec.schedulerName`
4. Verify that the default scheduler ignores these pods and the custom scheduler binds them
5. Test failover by deleting the custom scheduler pod

### RBAC for Custom Scheduler

```yaml
# rbac.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: custom-scheduler
  namespace: kube-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: custom-scheduler
rules:
  - apiGroups: [""]
    resources: [pods, nodes, namespaces, persistentvolumeclaims, persistentvolumes]
    verbs: [get, list, watch]
  - apiGroups: [""]
    resources: [pods/binding, pods/status]
    verbs: [create, patch, update]
  - apiGroups: [""]
    resources: [events]
    verbs: [create, patch, update]
  - apiGroups: ["coordination.k8s.io"]
    resources: [leases]
    verbs: [create, get, update]
  - apiGroups: ["storage.k8s.io"]
    resources: [storageclasses, csinodes, csidrivers]
    verbs: [get, list, watch]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: custom-scheduler
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: custom-scheduler
subjects:
  - kind: ServiceAccount
    name: custom-scheduler
    namespace: kube-system
```

### Custom Scheduler Configuration

```yaml
# custom-scheduler-config.yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: custom-scheduler-config
  namespace: kube-system
data:
  config.yaml: |
    apiVersion: kubescheduler.config.k8s.io/v1
    kind: KubeSchedulerConfiguration
    leaderElection:
      leaderElect: true
      resourceNamespace: kube-system
      resourceName: custom-scheduler
    profiles:
      - schedulerName: custom-scheduler
        pluginConfig:
          - name: NodeResourcesFit
            args:
              scoringStrategy:
                type: MostAllocated      # bin packing strategy
```

### Custom Scheduler Deployment

```yaml
# custom-scheduler-deployment.yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: custom-scheduler
  namespace: kube-system
spec:
  replicas: 2                           # HA with leader election
  selector:
    matchLabels:
      component: custom-scheduler
  template:
    metadata:
      labels:
        component: custom-scheduler
    spec:
      serviceAccountName: custom-scheduler
      containers:
        - name: scheduler
          image: registry.k8s.io/kube-scheduler:v1.29.0
          command:
            - kube-scheduler
            - --config=/etc/kubernetes/scheduler-config.yaml
            - --v=2
          volumeMounts:
            - name: config
              mountPath: /etc/kubernetes
          resources:
            requests:
              cpu: 100m
              memory: 128Mi
      volumes:
        - name: config
          configMap:
            name: custom-scheduler-config
```

### Pod Using Custom Scheduler

```yaml
# pod-custom-scheduled.yaml
apiVersion: v1
kind: Pod
metadata:
  name: custom-scheduled-pod
spec:
  schedulerName: custom-scheduler
  containers:
    - name: nginx
      image: nginx:1.27
      resources:
        requests:
          cpu: 100m
          memory: 64Mi
```

## Verify

```bash
# 1. Check custom scheduler is running
kubectl get pods -n kube-system -l component=custom-scheduler

# 2. Check leader election
kubectl get leases -n kube-system | grep custom-scheduler

# 3. Create a pod targeting the custom scheduler
kubectl apply -f pod-custom-scheduled.yaml

# 4. Verify it was scheduled by the custom scheduler
kubectl get pod custom-scheduled-pod -o wide
kubectl get events --field-selector involvedObject.name=custom-scheduled-pod

# 5. Check custom scheduler logs
kubectl logs -n kube-system -l component=custom-scheduler --tail=50

# 6. Verify the default scheduler ignores it
kubectl logs -n kube-system -l component=kube-scheduler --tail=50

# 7. Test failover: delete the leader pod
kubectl delete pod -n kube-system -l component=custom-scheduler --field-selector=status.phase=Running --wait=false
kubectl get pods -n kube-system -l component=custom-scheduler --watch
```

## Cleanup

```bash
kubectl delete pod custom-scheduled-pod
kubectl delete deployment custom-scheduler -n kube-system
kubectl delete configmap custom-scheduler-config -n kube-system
kubectl delete clusterrolebinding custom-scheduler
kubectl delete clusterrole custom-scheduler
kubectl delete serviceaccount custom-scheduler -n kube-system
```

## What's Next

You can now customize how pods are placed on nodes. The next exercise shifts focus to the nodes themselves, configuring the kubelet for resource reservations and eviction thresholds: [15.09 - Kubelet Configuration and Resource Reservations](../09-kubelet-configuration/).

## Summary

- Multiple schedulers can coexist; pods select one via `spec.schedulerName`
- Custom schedulers need RBAC for pods, nodes, bindings, events, and leases
- Leader election prevents split-brain when running scheduler replicas
- Using the stock `kube-scheduler` image with a custom config is the simplest approach
- Pods referencing a nonexistent scheduler stay Pending indefinitely
