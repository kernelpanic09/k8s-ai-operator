# Architecture

## Overview

The operator follows the standard controller-runtime reconciliation model.
One manager process runs three controllers and an embedded HTTP proxy server.

```
                               ┌────────────────────────────────────────────┐
                               │          ai-operator-system namespace        │
                               │                                              │
  Developer                   │   ┌──────────────────────────────────────┐   │
     │                         │   │         k8s-ai-operator pod           │   │
     │  kubectl apply           │   │                                      │   │
     │  ModelEndpoint.yaml      │   │  ┌─────────────────────────────┐    │   │
     ▼                         │   │  │   controller-runtime manager │    │   │
  ┌──────────────┐             │   │  │                              │    │   │
  │  Kubernetes  │────watch────┼───┼─►│  ModelEndpoint controller   │    │   │
  │  API Server  │             │   │  │  PromptTemplate controller   │    │   │
  └──────────────┘             │   │  │  GuardrailPolicy controller  │    │   │
         │                     │   │  └──────────────┬──────────────┘    │   │
         │ creates              │   │                 │                   │   │
         ▼                     │   │                 │ CreateOrUpdate     │   │
  ┌──────────────┐             │   │                 ▼                   │   │
  │   Service    │             │   │  ┌──────────────────────────────┐   │   │
  │ (ExternalName│             │   │  │     Bedrock proxy (HTTP)     │   │   │
  │  → proxy)    │             │   │  │     :8090                    │   │   │
  └──────────────┘             │   │  └──────────────┬───────────────┘   │   │
         │                     │   │                 │                   │   │
         │ traffic             │   └─────────────────┼───────────────────┘   │
         │                     └───────────────────  │  ─────────────────────┘
         └────────────────────────────────────────── ┘
                                                      │  IRSA / STS AssumeRole
                                                      ▼
                                             ┌─────────────────┐
  Workload pod                               │   AWS Bedrock   │
     │                                       │   Runtime API   │
     │ POST /invoke/ai-workloads/claude-haiku│                 │
     ├──────────────────────────────────────►│ InvokeModel     │
     │                                       │                 │
     │◄──────────────────────────────────────│ response        │
     │                                       └─────────────────┘
     │
     │ Metrics emitted
     ▼
  ┌────────────────┐
  │   Prometheus   │
  │  /metrics :8080│
  └────────────────┘
```

## Component breakdown

### Controllers

**ModelEndpointReconciler** is the main controller. On each reconcile it:

1. Fetches the ModelEndpoint.
2. Handles deletion via finalizer (cleans up rate limiter state and Prometheus label cardinality).
3. Adds the finalizer on first run.
4. Calls `GetFoundationModel` (Bedrock control plane) to verify the model is accessible under the configured IRSA role. This is a cheap read operation; it doesn't invoke the model.
5. Creates or updates an `ExternalName` Service in the endpoint's namespace. The Service CNAME points to the operator's own proxy Service.
6. Updates status conditions and the `available` field.
7. Requeues in 5 minutes (steady state) or 10 seconds (transient error).

**PromptTemplateReconciler** validates the template at admission time (webhook) and via reconcile:

- Parses the Go text/template syntax to catch errors early.
- Resolves the referenced ModelEndpoint and sets `Ready=True` only when both the template is valid and the endpoint is available.

**GuardrailPolicyReconciler** syncs to the Bedrock Guardrails API:

- Create: calls `CreateGuardrail` and stores the returned `guardrailId` in status.
- Update: calls `UpdateGuardrail` with the stored ID when the spec changes.
- Delete: calls `DeleteGuardrail` before removing the finalizer.

### Proxy server

An HTTP server runs inside the manager process on `:8090`. It exposes:

- `POST /invoke/{namespace}/{name}` - invokes a ModelEndpoint directly with a list of messages.
- `POST /render/{namespace}/{name}` - renders a PromptTemplate with supplied variables and invokes its endpoint.
- `GET /healthz` - liveness check.

Per-endpoint `ExternalName` Services point to this server, so workload pods just `POST` to a cluster-local URL without knowing anything about AWS.

The proxy enforces:
- **Rate limiting**: per-endpoint token bucket (requests/minute from the spec).
- **Budget gating**: if the current period's accumulated cost exceeds the spec's limit, requests receive HTTP 429.
- **MaxTokens cap**: callers can request fewer tokens but not more than the endpoint's `maxTokens`.

### Metrics

All metrics are registered with `controller-runtime`'s Prometheus registry (`:8080/metrics`). Key metrics:

| Metric | Type | Description |
|--------|------|-------------|
| `k8s_ai_operator_bedrock_invocations_total` | Counter | Invocations by namespace, endpoint, model ID, status |
| `k8s_ai_operator_bedrock_input_tokens_total` | Counter | Input tokens consumed |
| `k8s_ai_operator_bedrock_output_tokens_total` | Counter | Output tokens generated |
| `k8s_ai_operator_bedrock_invocation_cost_dollars_total` | Counter | Estimated USD cost |
| `k8s_ai_operator_bedrock_invocation_duration_seconds` | Histogram | End-to-end latency |
| `k8s_ai_operator_bedrock_endpoint_available` | Gauge | 0/1 availability per endpoint |
| `k8s_ai_operator_proxy_rate_limited_total` | Counter | Requests rejected by rate limiter |
| `k8s_ai_operator_proxy_budget_breached_total` | Counter | Requests rejected by budget gate |

### IRSA flow

```
Operator pod (ServiceAccount annotated with role ARN)
  │
  │  AWS_ROLE_ARN + AWS_WEB_IDENTITY_TOKEN_FILE injected by EKS
  │
  ▼
AWS STS AssumeRoleWithWebIdentity
  │
  ▼
Operator base credentials (can call STS only)
  │
  │  For each ModelEndpoint, when the first request arrives:
  ▼
STS AssumeRole → per-endpoint role ARN from spec.irsaRoleArn
  │
  ▼
Bedrock InvokeModel with per-endpoint credentials
```

Clients are cached per (roleArn, region) pair and invalidated when the endpoint is deleted or its ARN/region changes.
