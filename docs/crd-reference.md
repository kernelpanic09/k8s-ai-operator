# CRD Reference

## ModelEndpoint

Namespace-scoped. Short name: `me`. Category: `ai`.

`kubectl get modelendpoints` / `kubectl get me`

### spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `modelId` | string | yes | Bedrock model identifier, e.g. `anthropic.claude-haiku-4-5-20251001-v1:0`. Cross-region inference profile ARNs are also accepted. |
| `region` | string | yes | AWS region for Bedrock invocations. Must match a Bedrock-enabled region. |
| `irsaRoleArn` | string | yes | IAM role ARN the operator assumes (via IRSA) to call Bedrock on behalf of this endpoint. |
| `maxTokens` | int32 | no | Maximum tokens to request from Bedrock. Individual callers cannot exceed this value. Default: 4096 (applied by the mutating webhook). |
| `costBudget.daily` | string | no | Maximum USD spend per UTC calendar day. Format: decimal with up to 2 places (e.g. `"10.00"`). Requests are gated once breached until midnight UTC. |
| `costBudget.monthly` | string | no | Maximum USD spend per calendar month. |
| `rateLimit.requestsPerMinute` | int32 | no | Token-bucket rate limit, 1-3600. Default: 60 rpm (applied by the mutating webhook). |
| `guardrailPolicyRef` | string | no | Name of a cluster-scoped GuardrailPolicy to apply to all invocations through this endpoint. |

### status

| Field | Type | Description |
|-------|------|-------------|
| `available` | bool | True when Bedrock is reachable and no budget has been breached. |
| `conditions` | []Condition | Standard K8s conditions: `Ready`, `BedrockReachable`. |
| `costThisMonth` | string | Accumulated USD spend this calendar month. |
| `costToday` | string | Accumulated USD spend today (UTC). |
| `invocationsToday` | int64 | Invocation count since UTC midnight. |
| `totalInputTokens` | int64 | Lifetime input token count. |
| `totalOutputTokens` | int64 | Lifetime output token count. |
| `serviceName` | string | Name of the proxy Service created by the operator. |
| `observedGeneration` | int64 | Mirrors `metadata.generation` for the last successful reconcile. |

### Conditions

- **Ready**: True when the endpoint is configured and Bedrock is reachable.
- **BedrockReachable**: True when `GetFoundationModel` succeeds for this model in the configured region under the specified IRSA role.
- **BudgetBreached**: True when daily or monthly spend exceeds the configured limit.

---

## PromptTemplate

Namespace-scoped. Short name: `pt`. Category: `ai`.

`kubectl get prompttemplates` / `kubectl get pt`

### spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `template` | string | yes | Go `text/template` body. Variables are referenced as `{{.VarName}}`. |
| `modelEndpointRef.name` | string | yes | Name of a ModelEndpoint in the same namespace. |
| `variables` | []TemplateVariable | no | Declares variables used in the template. |
| `systemPrompt` | string | no | Prepended as the system message on every render. |
| `maxTokens` | int32 | no | Override the endpoint's maxTokens for this template's renders. Must not exceed the endpoint's limit. |

### spec.variables[]

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | yes | Variable name. Must match `^[A-Za-z][A-Za-z0-9_]*$`. |
| `type` | string | yes | `string`, `integer`, or `boolean`. |
| `required` | bool | no | If true, missing this variable in a render request returns 400. |
| `default` | string | no | Used when the variable is absent and `required` is false. |
| `description` | string | no | Human-readable description surfaced in docs and OpenAPI. |

### status

| Field | Type | Description |
|-------|------|-------------|
| `conditions` | []Condition | `Ready`, `TemplateValid`. |
| `renderCount` | int64 | Lifetime successful render count. |
| `observedGeneration` | int64 | Mirrors `metadata.generation`. |

---

## GuardrailPolicy

Cluster-scoped. Short name: `gp`. Category: `ai`.

`kubectl get guardrailpolicies` / `kubectl get gp`

The operator syncs this resource to the Bedrock Guardrails API on create/update
and deletes the Bedrock guardrail on delete (before removing the finalizer).

### spec

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `description` | string | no | Human-readable summary. Shown in the Bedrock console. |
| `blockedInputMessage` | string | no | Message returned to callers when input is blocked. |
| `blockedOutputsMessage` | string | no | Message returned when model output is blocked. |
| `contentPolicy.filters[]` | []ContentFilter | no | Per-category content filter thresholds. |
| `sensitiveInformationPolicy.piiEntities[]` | []PIIEntityConfig | no | PII detection and action configuration. |
| `topicPolicy.topics[]` | []DeniedTopic | no | Topics the guardrail refuses to engage with. |
| `wordPolicy.words[]` | []string | no | Exact words to block. |
| `wordPolicy.managedWordLists[]` | []string | no | Bedrock-managed word lists (e.g. `PROFANITY`). |

### status

| Field | Type | Description |
|-------|------|-------------|
| `guardrailId` | string | ID assigned by Bedrock after creation. |
| `guardrailVersion` | string | Bedrock-assigned version string (increments on each UpdateGuardrail call). |
| `conditions` | []Condition | `GuardrailSynced`. |
| `observedGeneration` | int64 | Mirrors `metadata.generation`. |
