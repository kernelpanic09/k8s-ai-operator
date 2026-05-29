![Go version](https://img.shields.io/badge/go-1.22-blue)
![Kubernetes](https://img.shields.io/badge/kubernetes-1.27%2B-blue)
![License](https://img.shields.io/badge/license-MIT-green)

# k8s-ai-operator

A Kubernetes operator that turns AWS Bedrock model access into first-class cluster resources, with cost tracking, rate limiting, and guardrails baked in.

## What this gives you

Your platform team defines `ModelEndpoint` resources in a cluster. Workloads get a cluster-local Service URL to POST messages to. The operator handles AWS authentication (IRSA), enforces rate limits and cost budgets, applies content guardrails, and exposes Prometheus metrics for every invocation.

The result: app teams call Bedrock without touching IAM or AWS SDKs, and platform teams get per-endpoint cost visibility and control.

## Quick start

```bash
# 1. Install CRDs and the operator.
kubectl apply -k config/default

# 2. Annotate the ServiceAccount with your operator IAM role.
kubectl annotate serviceaccount k8s-ai-operator \
  -n ai-operator-system \
  eks.amazonaws.com/role-arn=arn:aws:iam::123456789012:role/k8s-ai-operator

# 3. Apply a guardrail and an endpoint.
kubectl apply -f - <<EOF
apiVersion: ai.kernelpanic09.io/v1alpha1
kind: GuardrailPolicy
metadata:
  name: pii-and-safety
spec:
  sensitiveInformationPolicy:
    piiEntities:
    - type: AWS_ACCESS_KEY
      action: BLOCK
    - type: EMAIL
      action: ANONYMIZE
  contentPolicy:
    filters:
    - type: PROMPT_ATTACK
      inputStrength: HIGH
      outputStrength: HIGH
EOF

kubectl apply -f - <<EOF
apiVersion: ai.kernelpanic09.io/v1alpha1
kind: ModelEndpoint
metadata:
  name: claude-haiku
  namespace: ai-workloads
spec:
  modelId: anthropic.claude-haiku-4-5-20251001-v1:0
  region: us-east-1
  irsaRoleArn: arn:aws:iam::123456789012:role/bedrock-invoker
  maxTokens: 4096
  costBudget:
    daily: "10.00"
    monthly: "200.00"
  rateLimit:
    requestsPerMinute: 60
  guardrailPolicyRef: pii-and-safety
EOF

# 4. Check that it's ready.
kubectl get modelendpoints -n ai-workloads
# NAME          MODEL                                      REGION      AVAILABLE   COST/MONTH   INVOCATIONS
# claude-haiku  anthropic.claude-haiku-4-5-20251001-v1:0  us-east-1   true        0.00         0

# 5. Invoke from another pod in ai-workloads.
curl -s -X POST \
  http://model-claude-haiku.ai-workloads.svc.cluster.local:8090/invoke/ai-workloads/claude-haiku \
  -H "Content-Type: application/json" \
  -d '{"messages":[{"role":"user","content":"Explain Kubernetes operators in two sentences."}]}' \
  | jq .
```

Response:

```json
{
  "content": "A Kubernetes operator is a controller that extends the Kubernetes API with custom resources, encoding operational knowledge about a specific application. Operators watch those custom resources and take action to reconcile the actual state of the system toward the desired state you've declared.",
  "inputTokens": 24,
  "outputTokens": 51,
  "stopReason": "end_turn",
  "costUSD": "0.000224"
}
```

## Architecture

```
  Developer                        Cluster
     │                               │
     │  kubectl apply ModelEndpoint  │
     ├──────────────────────────────►│
     │                               │
     │                               │  Operator reconciles:
     │                               │  - validates IRSA role
     │                               │  - checks Bedrock reachability
     │                               │  - creates ExternalName Service
     │                               │
  Workload pod                       │  model-claude-haiku.ai-workloads.svc
     │                               │        │
     │  POST /invoke/ai-workloads/claude-haiku│
     ├──────────────────────────────────────► proxy:8090
     │                               │        │
     │                               │        │  IRSA → STS AssumeRole
     │                               │        │
     │                               │        ▼
     │                               │    AWS Bedrock
     │                               │        │
     │◄────── response + tokens ─────┼────────┘
     │                               │
     │                           Prometheus
     │                           /metrics:8080
     │                           invocations, tokens, cost
```

## CRDs

| Resource | Scope | Description |
|----------|-------|-------------|
| `ModelEndpoint` | Namespaced | Represents one Bedrock model. The operator creates a proxy Service in the same namespace. |
| `PromptTemplate` | Namespaced | A versioned, parameterized prompt that routes renders through a ModelEndpoint. Uses Go `text/template` syntax. |
| `GuardrailPolicy` | Cluster | Content moderation rules synced to the Bedrock Guardrails API. ModelEndpoints reference these by name. |

## Installation

### kubectl apply

```bash
kubectl apply -f https://github.com/kernelpanic09/k8s-ai-operator/releases/latest/download/install.yaml
```

### Kustomize

```bash
git clone https://github.com/kernelpanic09/k8s-ai-operator
cd k8s-ai-operator
# Edit config/rbac/service_account.yaml to set your operator IAM role ARN.
kubectl apply -k config/default
```

### Helm (coming in v0.3.0)

```bash
# Not yet available. Track: https://github.com/kernelpanic09/k8s-ai-operator/issues/12
```

See [docs/installation.md](docs/installation.md) for full details.

## IRSA setup

The operator authenticates to AWS using IRSA. You need an IAM role for the
operator itself (to call `sts:AssumeRole`) and one or more per-endpoint roles
(to call `bedrock:InvokeModel`).

Full setup guide: [docs/irsa-setup.md](docs/irsa-setup.md)

## Prompt templates

Define reusable, versioned prompts as a CRD:

```bash
kubectl apply -f - <<EOF
apiVersion: ai.kernelpanic09.io/v1alpha1
kind: PromptTemplate
metadata:
  name: code-review
  namespace: ai-workloads
spec:
  systemPrompt: "You are a senior engineer. Be direct and specific."
  template: |
    Review this diff focusing on {{.Focus}}:

    \`\`\`diff
    {{.Diff}}
    \`\`\`
  variables:
  - name: Diff
    type: string
    required: true
  - name: Focus
    type: string
    default: security and correctness
  modelEndpointRef:
    name: claude-haiku
EOF

# Render the template by POSTing variables.
curl -s -X POST \
  http://model-claude-haiku.ai-workloads.svc.cluster.local:8090/render/ai-workloads/code-review \
  -H "Content-Type: application/json" \
  -d '{"variables":{"Diff":"- password = \"hunter2\"\n+ password = os.getenv(\"PASSWORD\")","Focus":"security"}}' \
  | jq .content
```

## Metrics

All metrics are on `:8080/metrics`. Key ones for alerting:

```promql
# Cost per endpoint this month (cumulative counter, use rate() for trends)
k8s_ai_operator_bedrock_invocation_cost_dollars_total

# Endpoints currently unavailable
k8s_ai_operator_bedrock_endpoint_available == 0

# Rate-limited requests
rate(k8s_ai_operator_proxy_rate_limited_total[5m])

# p99 Bedrock latency
histogram_quantile(0.99, rate(k8s_ai_operator_bedrock_invocation_duration_seconds_bucket[5m]))
```

## Development

```bash
# Run tests
make test

# Build binary
make build

# Run locally against your current kubeconfig (install CRDs first)
make install
make run

# Regenerate CRD YAML from Go types (requires controller-gen)
make manifests

# Build image
make docker-build IMAGE_TAG=dev
```

## Related projects

- [agents-platform](https://github.com/kernelpanic09/agents-platform) - Multi-agent scheduling platform that this operator was designed to serve.
- [mcp-server-aws](https://github.com/kernelpanic09/mcp-server-aws) - Model Context Protocol server for AWS services.
- [terraform-aws-modules](https://github.com/terraform-aws-modules) - Terraform modules for the IAM roles this operator references.

## License

MIT. See [LICENSE](LICENSE).
