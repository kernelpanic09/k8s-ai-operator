# Installation

## Prerequisites

- Kubernetes 1.27+ (tested on EKS 1.28, 1.29, 1.30)
- AWS account with Bedrock enabled in your target region
- IRSA configured on your EKS cluster (see [irsa-setup.md](irsa-setup.md))
- `kubectl` >= 1.27
- `kustomize` >= 5.0 (or `kubectl kustomize`)

## Option 1: kubectl apply (single manifest)

```bash
kubectl apply -f https://github.com/kernelpanic09/k8s-ai-operator/releases/latest/download/install.yaml
```

The `install.yaml` bundles CRDs, RBAC, and the Deployment. After applying, annotate
the ServiceAccount with your operator IAM role ARN (see IRSA setup).

## Option 2: Kustomize

```bash
git clone https://github.com/kernelpanic09/k8s-ai-operator
cd k8s-ai-operator

# Edit config/rbac/service_account.yaml to set your IRSA role ARN, then:
kubectl apply -k config/default
```

To pin to a specific image tag, add an `images` override to your own kustomization:

```yaml
# kustomization.yaml in your own overlay directory
resources:
- github.com/kernelpanic09/k8s-ai-operator/config/default?ref=v0.2.0

images:
- name: ghcr.io/kernelpanic09/k8s-ai-operator
  newTag: v0.2.0
```

## Option 3: Helm (coming soon)

A Helm chart is planned for v0.3.0. Track progress at
https://github.com/kernelpanic09/k8s-ai-operator/issues/12.

## After installation

```bash
# Confirm the operator is running.
kubectl get pods -n ai-operator-system

# Apply the sample guardrail and endpoint.
kubectl apply -f config/samples/pii_guardrail.yaml
kubectl apply -f config/samples/claude_haiku_endpoint.yaml

# Check endpoint status.
kubectl get modelendpoints -n ai-workloads
# NAME          MODEL                                      REGION      AVAILABLE   COST/MONTH   INVOCATIONS
# claude-haiku  anthropic.claude-haiku-4-5-20251001-v1:0  us-east-1   true        0.00         0

# View the proxy Service the operator created.
kubectl get svc -n ai-workloads
# NAME            TYPE           CLUSTER-IP   EXTERNAL-IP   PORT(S)    AGE
# model-claude-haiku   ExternalName   <none>       k8s-ai-operator-proxy...   8090/TCP   10s
```

## Uninstalling

```bash
kubectl delete -k config/default
kubectl delete -k config/crd
```

CRDs are deleted separately so you have a chance to back up existing CRs before
removing them. If you delete CRDs while CRs still exist, Kubernetes will garbage-collect
the CRs immediately.
