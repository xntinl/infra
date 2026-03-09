# 1. StatefulSets and Persistent Storage

<!--
difficulty: intermediate
concepts: [statefulset, headless-service, persistent-volume-claim, stable-network-identity, ordered-deployment]
tools: [kubectl, minikube]
estimated_time: 40m
bloom_level: apply
prerequisites: [none]
-->

## Prerequisites

- A running Kubernetes cluster (minikube, kind, or k3d)
- `kubectl` installed and configured to talk to your cluster
- A default StorageClass provisioner (minikube provides one out of the box)

Verify your cluster is ready and a StorageClass exists:

```bash
kubectl cluster-info
kubectl get storageclass
```

You should see at least one StorageClass listed (e.g., `standard` on minikube).

## Learning Objectives

By the end of this exercise you will be able to:

- **Apply** a StatefulSet with volumeClaimTemplates to provision persistent storage per pod
- **Analyze** ordered pod creation and stable network identities provided by a headless Service
- **Evaluate** whether a workload requires a StatefulSet versus a Deployment by testing data persistence and DNS behavior

## Why StatefulSets and Persistent Storage?

Deployments treat every pod as interchangeable. When a pod dies, the replacement gets a random name, a new IP address, and starts with a clean filesystem. This works perfectly for stateless web servers, but it is a disaster for databases, message queues, or any workload that stores data locally and needs other pods to find it by a predictable name.

StatefulSets solve this problem with three guarantees that Deployments cannot provide. First, each pod gets a stable hostname that follows the pattern `{statefulset-name}-{ordinal}` (e.g., `nginx-sts-0`, `nginx-sts-1`). Second, pods are created and terminated in order, ensuring that pod 0 is fully running before pod 1 starts. Third, each pod can have its own PersistentVolumeClaim that survives pod restarts and rescheduling. If `nginx-sts-1` is deleted and recreated, it reattaches to the exact same volume with all its data intact. These properties make StatefulSets the correct abstraction for clustered databases like PostgreSQL, distributed systems like Kafka, and any workload where identity and storage matter.

## Step 1: Create the Headless Service

A StatefulSet requires a headless Service to provide DNS records for each pod. A headless Service has `clusterIP: None`, which tells Kubernetes not to create a virtual IP. Instead, DNS queries return the individual pod IPs directly.

Create `headless-service.yaml`:

```yaml
# headless-service.yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-headless
  labels:
    app: nginx-sts
spec:
  ports:
    - port: 80
  clusterIP: None          # Headless — no load balancing, direct pod DNS
  selector:
    app: nginx-sts
```

Apply it:

```bash
kubectl apply -f headless-service.yaml
```

Verify:

```bash
kubectl get svc nginx-headless
```

Expected output:

```
NAME             TYPE        CLUSTER-IP   EXTERNAL-IP   PORT(S)   AGE
nginx-headless   ClusterIP   None         <none>        80/TCP    5s
```

The `CLUSTER-IP` column shows `None`, confirming this is a headless Service.

## Step 2: Fill in volumeClaimTemplates and Deploy

Create `statefulset.yaml` with the following content. The `volumeClaimTemplates` section is left as a TODO for you to complete:

```yaml
# statefulset.yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nginx-sts
spec:
  serviceName: nginx-headless    # Must match the headless Service name
  replicas: 3
  selector:
    matchLabels:
      app: nginx-sts
  template:
    metadata:
      labels:
        app: nginx-sts
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
          ports:
            - containerPort: 80
          volumeMounts:
            - name: www
              mountPath: /usr/share/nginx/html
  # TODO: Add volumeClaimTemplates
  # Requirements:
  #   - PVC name: www (must match the volumeMounts name above)
  #   - Access mode: ReadWriteOnce
  #   - Storage request: 1Gi
  # Docs: https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/#volume-claim-templates
```

Once you have filled in the `volumeClaimTemplates` block, apply the StatefulSet:

```bash
kubectl apply -f statefulset.yaml
```

Watch the pods being created in order:

```bash
kubectl get pods -l app=nginx-sts -w
```

Expected behavior: `nginx-sts-0` reaches `Running` first, then `nginx-sts-1`, then `nginx-sts-2`. Each pod must be ready before the next one starts. Press `Ctrl+C` once all three are running.

Verify all pods are ready:

```bash
kubectl get pods -l app=nginx-sts
```

Expected output:

```
NAME          READY   STATUS    RESTARTS   AGE
nginx-sts-0   1/1     Running   0          60s
nginx-sts-1   1/1     Running   0          45s
nginx-sts-2   1/1     Running   0          30s
```

Verify that PVCs were created for each pod:

```bash
kubectl get pvc -l app=nginx-sts
```

Expected output:

```
NAME              STATUS   VOLUME                                     CAPACITY   ACCESS MODES   STORAGECLASS   AGE
www-nginx-sts-0   Bound    pvc-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx   1Gi        RWO            standard       60s
www-nginx-sts-1   Bound    pvc-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx   1Gi        RWO            standard       45s
www-nginx-sts-2   Bound    pvc-xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx   1Gi        RWO            standard       30s
```

The PVC names follow the pattern `{volumeClaimTemplate-name}-{statefulset-name}-{ordinal}`.

## Step 3: Write Unique Data to Each Pod

Write different content to each pod's persistent volume to prove that each pod has its own independent storage:

```bash
kubectl exec nginx-sts-0 -- sh -c 'echo "pod-0" > /usr/share/nginx/html/index.html'
kubectl exec nginx-sts-1 -- sh -c 'echo "pod-1" > /usr/share/nginx/html/index.html'
kubectl exec nginx-sts-2 -- sh -c 'echo "pod-2" > /usr/share/nginx/html/index.html'
```

Verify the content is different on each pod:

```bash
kubectl exec nginx-sts-0 -- cat /usr/share/nginx/html/index.html
kubectl exec nginx-sts-1 -- cat /usr/share/nginx/html/index.html
kubectl exec nginx-sts-2 -- cat /usr/share/nginx/html/index.html
```

Expected output:

```
pod-0
pod-1
pod-2
```

## Step 4: Test Data Persistence Across Pod Deletion

Delete the middle pod and watch it come back with the same name:

```bash
kubectl delete pod nginx-sts-1
```

Watch the replacement:

```bash
kubectl get pods -l app=nginx-sts -w
```

Once `nginx-sts-1` is back in `Running` status, verify its data survived:

```bash
kubectl exec nginx-sts-1 -- cat /usr/share/nginx/html/index.html
```

Expected output:

```
pod-1
```

The data persisted because the new `nginx-sts-1` pod reattached to the same PVC (`www-nginx-sts-1`). A Deployment would have given the replacement pod a random name and a fresh filesystem.

## Step 5: Test DNS Resolution

Each StatefulSet pod gets a DNS record in the format `{pod-name}.{service-name}.{namespace}.svc.cluster.local`. Test this with a temporary busybox pod:

```bash
kubectl run dns-test --image=busybox:1.37 --rm -it --restart=Never -- nslookup nginx-sts-0.nginx-headless
```

Expected output:

```
Server:    10.96.0.10
Address 1: 10.96.0.10 kube-dns.kube-system.svc.cluster.local

Name:      nginx-sts-0.nginx-headless
Address 1: 10.244.x.x nginx-sts-0.nginx-headless.default.svc.cluster.local
```

The key takeaway: other pods in the cluster can reach `nginx-sts-0` by the DNS name `nginx-sts-0.nginx-headless`, and this name is stable even if the pod is rescheduled to a different node with a different IP.

## Spot the Bug

A teammate writes a StatefulSet with this configuration:

```yaml
apiVersion: apps/v1
kind: StatefulSet
metadata:
  name: nginx-sts
spec:
  serviceName: nginx-svc        # <-- Note the name
  replicas: 3
  selector:
    matchLabels:
      app: nginx-sts
  template:
    metadata:
      labels:
        app: nginx-sts
    spec:
      containers:
        - name: nginx
          image: nginx:1.27
```

And a Service named:

```yaml
apiVersion: v1
kind: Service
metadata:
  name: nginx-headless           # <-- Different name
spec:
  clusterIP: None
  selector:
    app: nginx-sts
  ports:
    - port: 80
```

**The pods create successfully and reach Running status. But when another pod tries `nslookup nginx-sts-0.nginx-headless`, it fails. Why?**

Think about it before expanding the answer.

<details>
<summary>Explanation</summary>

The `serviceName` field in the StatefulSet says `nginx-svc`, but the actual headless Service is named `nginx-headless`. Kubernetes uses the `serviceName` field to construct DNS records for each pod. The expected DNS records would be created under `nginx-svc`, not `nginx-headless`. Since no Service named `nginx-svc` exists, the DNS records are never populated.

The fix is to ensure `serviceName` in the StatefulSet spec exactly matches the `metadata.name` of the headless Service:

```yaml
spec:
  serviceName: nginx-headless    # Must match the Service's metadata.name
```

This is a common mistake because Kubernetes does not validate that the referenced Service exists when you create the StatefulSet. The pods run fine, but DNS resolution silently fails.

</details>

## Verify What You Learned

Run the following checks to confirm everything worked:

All three pods are running with sequential names:

```bash
kubectl get pods -l app=nginx-sts -o custom-columns=NAME:.metadata.name,STATUS:.status.phase
```

Expected output:

```
NAME          STATUS
nginx-sts-0   Running
nginx-sts-1   Running
nginx-sts-2   Running
```

All three PVCs are bound:

```bash
kubectl get pvc -l app=nginx-sts -o custom-columns=NAME:.metadata.name,STATUS:.status.phase,CAPACITY:.status.capacity.storage
```

Expected output:

```
NAME              STATUS   CAPACITY
www-nginx-sts-0   Bound    1Gi
www-nginx-sts-1   Bound    1Gi
www-nginx-sts-2   Bound    1Gi
```

Data persists after pod deletion (should still show "pod-1" after Step 4):

```bash
kubectl exec nginx-sts-1 -- cat /usr/share/nginx/html/index.html
```

Expected output:

```
pod-1
```

## Cleanup

Remove all resources created in this exercise. Note that StatefulSet deletion does not automatically delete PVCs, so you must clean them up separately:

```bash
kubectl delete statefulset nginx-sts
kubectl delete svc nginx-headless
kubectl delete pvc -l app=nginx-sts
```

Verify nothing remains:

```bash
kubectl get statefulset,svc,pvc -l app=nginx-sts
```

Expected output:

```
No resources found in default namespace.
```

## What's Next

StatefulSets always create pods in strict order by default, but this is not always necessary. In [exercise 02 (StatefulSet Scaling and Pod Management Policy)](../02-statefulset-scaling-behavior/), you will explore how scaling works and how `podManagementPolicy` changes the behavior.

## Summary

- **StatefulSets** provide stable pod names (`name-0`, `name-1`, ...), ordered deployment, and per-pod persistent storage.
- A **headless Service** (`clusterIP: None`) creates individual DNS records for each pod in the format `{pod}.{service}`.
- **volumeClaimTemplates** automatically provision a PVC for each pod, and that PVC survives pod deletion and rescheduling.
- The `serviceName` field must exactly match the headless Service name, or DNS resolution will silently fail.
- Deleting a StatefulSet pod causes it to be recreated with the same name and reattached to the same PVC.

## Reference

- [StatefulSets](https://kubernetes.io/docs/concepts/workloads/controllers/statefulset/) — official concept documentation
- [StatefulSet Basics Tutorial](https://kubernetes.io/docs/tutorials/stateful-application/basic-stateful-set/) — hands-on walkthrough
- [Headless Services](https://kubernetes.io/docs/concepts/services-networking/service/#headless-services) — DNS behavior without a cluster IP

## Additional Resources

- [Kubernetes API Reference: StatefulSet v1](https://kubernetes.io/docs/reference/kubernetes-api/workload-resources/stateful-set-v1/)
- [Persistent Volumes](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) — storage backend concepts
- [Dynamic Volume Provisioning](https://kubernetes.io/docs/concepts/storage/dynamic-provisioning/) — how StorageClasses automate PV creation

---

<details>
<summary>TODO Solution: volumeClaimTemplates</summary>

```yaml
  volumeClaimTemplates:
    - metadata:
        name: www
      spec:
        accessModes:
          - ReadWriteOnce
        resources:
          requests:
            storage: 1Gi
```

Place this block at the end of the StatefulSet spec, at the same indentation level as `template`.

</details>
