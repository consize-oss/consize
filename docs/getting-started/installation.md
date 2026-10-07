---
description: Install Consize on a Kubernetes cluster with Helm and configure it to safely optimize workloads using your existing metrics.
---

# Production Installation

This guide walks you through installing Consize on a Kubernetes cluster using Helm.

## Prerequisites

Before installing Consize, make sure you have:

- A running Kubernetes cluster
- `kubectl` configured for the cluster
- Helm installed
- Prometheus or a compatible metrics endpoint

## 1. Create the Consize namespace

```sh
--8<-- "create-namespace.sh"
```

## 2. Configure your metrics connection

Consize needs access to your Prometheus metrics endpoint.

Create the required secret:

```sh
--8<-- "create-metrics-secret.sh"
```

Replace the URL with your Prometheus endpoint if it is different.

## 3. Configure optional integrations

### Slack

To enable Slack notifications:

```sh
kubectl -n consize-system create secret generic consize-alerts \
  --from-literal=slack-webhook='https://hooks.slack.com/services/...'
```

### GitHub

To enable GitHub integration:

```sh
kubectl -n consize-system create secret generic consize-github \
  --from-literal=token='github_pat_...'
```

### Cloud provider credentials

Consize only calls cloud provider APIs when you turn on cloud database metrics (`CONSIZE_DBMETRICS`) or cloud waste scanning. Both are opt-in, see [Configuration](../reference/configuration.md#cloud-database-metrics) for the switches. If you don't need either, skip this section.

=== "AWS"

    Grant the collector's IAM role (via IRSA, or an access key in a secret if you're not using IRSA) a policy scoped to exactly what it reads and, only if you enable it, what it's allowed to clean up:

```json
    {
      "Version": "2012-10-17",
      "Statement": [
        {
          "Sid": "ConsizeReadOnly",
          "Effect": "Allow",
          "Action": [
            "rds:DescribeDBInstances",
            "cloudwatch:GetMetricStatistics",
            "ec2:DescribeVolumes",
            "ec2:DescribeInstances"
          ],
          "Resource": "*"
        },
        {
          "Sid": "ConsizeWasteCleanupOptIn",
          "Effect": "Allow",
          "Action": [
            "ec2:DeleteVolume",
            "ec2:TerminateInstances"
          ],
          "Resource": "*"
        }
      ]
    }
```

    The `ConsizeWasteCleanupOptIn` statement is only needed if you let Consize apply cloud-waste cleanups directly rather than just reporting them, see [Kubernetes Rightsizing](../guides/rightsizing.md) for the review-vs-direct-apply distinction, which applies to cloud waste the same way it applies to Kubernetes rightsizing. Leave it out if you only want recommendations.

    Set the region with:

```yaml
    env:
      CONSIZE_AWS_REGION: us-east-1
```

=== "GCP"

    Grant the collector's service account these predefined roles, scoped to the project:

    | Role | Why |
    |---|---|
    | `roles/cloudsql.viewer` | Lists Cloud SQL instances (`sqladmin.googleapis.com`) |
    | `roles/monitoring.viewer` | Reads Cloud SQL metrics (`monitoring.googleapis.com`) |
    | `roles/compute.viewer` | Lists disks and instances for waste scanning (`compute.googleapis.com`) |

    If you let Consize clean up detected waste directly, it also needs delete permission on disks and instances, for example a custom role granting `compute.disks.delete` and `compute.instances.delete`, scoped to the same project, instead of the broader `roles/compute.admin`. As with AWS, this is only required if you enable direct cleanup rather than review-only recommendations.

    Set the project (or let it infer from the service account key):

```yaml
    env:
      CONSIZE_GCP_PROJECT: my-project-id
```

!!! note
    These are the API calls Consize's collector and cost-scanner currently make. Treat this as a starting point for writing your own least-privilege policy, not a guarantee against a specific release, if in doubt, check [SECURITY.md](https://github.com/consize-oss/consize/blob/main/SECURITY.md) or open a [Support](../resources/support.md) request and we'll confirm against the version you're running.

## 4. Configure collection scope

By default, Consize can collect data across the cluster.

To limit collection to specific namespaces:

```yaml
env:
  CONSIZE_NAMESPACES: boutique,payments,checkout
```

To collect across all namespaces:

```yaml
env:
  CONSIZE_NAMESPACES: ""
```

## 5. Configure Kubernetes permissions

Consize uses Kubernetes RBAC to control what it can read and what it can change.

### Read-only access

The collector requires read access to the resources it analyzes.

Example:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: consize-reader
  namespace: consize-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: consize-read
rules:
  - apiGroups: [""]
    resources: ["namespaces", "pods"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["apps"]
    resources: ["deployments", "statefulsets", "daemonsets", "replicasets"]
    verbs: ["get", "list", "watch"]
  - apiGroups: ["batch"]
    resources: ["jobs", "cronjobs"]
    verbs: ["get", "list", "watch"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: consize-read
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: consize-read
subjects:
  - kind: ServiceAccount
    name: consize-reader
    namespace: consize-system
```

### Direct apply access

If you want Consize to apply approved changes directly, grant write access only to the namespaces where it should operate.

For example:

```yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: consize-writer
  namespace: consize-system
---
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: consize-apply
  namespace: boutique
rules:
  - apiGroups: ["apps"]
    resources: ["deployments"]
    verbs: ["get", "list", "watch", "update"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: consize-apply
  namespace: boutique
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: consize-apply
subjects:
  - kind: ServiceAccount
    name: consize-writer
    namespace: consize-system
```

You can then enable automatic application for a namespace:

```sh
kubectl label namespace boutique consize.savings.dev/auto-apply=enabled
```

## 6. Install Consize with Helm

=== "From the published chart (recommended)"

    Install directly from the OCI chart on GitHub Container Registry (GHCR), no need to clone the repository:

```sh
    --8<-- "helm-install-oci.sh"
```

=== "From a local checkout"

    If you've cloned the repository and want to install from your local checkout instead:

```sh
    --8<-- "helm-install-source.sh"
```

For more advanced deployments, configure the Helm values for your environment.

## 7. Verify the installation

--8<-- "verify-install.md"

## Next steps

* [Interactive Sandbox](sandbox.md)
* [Configuration](../reference/configuration.md)
* [Supported Platforms](../reference/supported-platforms.md)
* [How Consize Works](../concepts/architecture.md)
* [The Safety Net](../concepts/safety-net.md)
* [Troubleshooting](../resources/troubleshooting.md) if something didn't come up healthy