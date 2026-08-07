package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"golang.org/x/time/rate"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
	"github.com/kernelpanic09/k8s-ai-operator/internal/bedrock"
	"github.com/kernelpanic09/k8s-ai-operator/internal/metrics"
)

// InvokeRequest is the JSON body the caller POSTs to /invoke/{namespace}/{name}.
type InvokeRequest struct {
	Messages     []bedrock.Message `json:"messages"`
	SystemPrompt string            `json:"systemPrompt,omitempty"`
	MaxTokens    int32             `json:"maxTokens,omitempty"`
	Temperature  float64           `json:"temperature,omitempty"`
}

// InvokeResponse is returned on success.
type InvokeResponse struct {
	Content      string `json:"content"`
	InputTokens  int64  `json:"inputTokens"`
	OutputTokens int64  `json:"outputTokens"`
	StopReason   string `json:"stopReason"`
	CostUSD      string `json:"costUSD"`
}

// RenderRequest is the JSON body for /render/{namespace}/{name}.
// Variables maps template variable names to their string values.
type RenderRequest struct {
	Variables map[string]string `json:"variables"`
}

// rateLimiterEntry holds a per-endpoint token bucket.
type rateLimiterEntry struct {
	limiter *rate.Limiter
}

// Handler processes /invoke and /render requests. It holds references to the
// K8s client (to read endpoint/template specs) and the Bedrock client.
type Handler struct {
	k8sClient     client.Client
	bedrockClient *bedrock.Client
	logger        *slog.Logger

	mu           sync.Mutex
	rateLimiters map[string]*rateLimiterEntry // key: namespace/name
}

// NewHandler constructs a Handler.
func NewHandler(k8s client.Client, br *bedrock.Client, logger *slog.Logger) *Handler {
	return &Handler{
		k8sClient:     k8s,
		bedrockClient: br,
		logger:        logger,
		rateLimiters:  make(map[string]*rateLimiterEntry),
	}
}

// HandleInvoke handles POST /invoke/{namespace}/{name}.
// The path segments identify the ModelEndpoint to use.
func (h *Handler) HandleInvoke(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	namespace, name, ok := parsePathSegments(r.URL.Path, "/invoke/")
	if !ok {
		http.Error(w, "path must be /invoke/{namespace}/{name}", http.StatusBadRequest)
		return
	}

	var req InvokeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	ep, err := h.fetchEndpoint(ctx, namespace, name)
	if err != nil {
		h.logger.Error("fetching ModelEndpoint", "namespace", namespace, "name", name, "error", err)
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	if !ep.Status.Available {
		http.Error(w, "endpoint not available", http.StatusServiceUnavailable)
		return
	}

	// Rate limiting check.
	if ep.Spec.RateLimit != nil {
		limiter := h.getLimiter(namespace, name, ep.Spec.RateLimit.RequestsPerMinute)
		if !limiter.Allow() {
			metrics.RateLimitedTotal.WithLabelValues(namespace, name).Inc()
			w.Header().Set("Retry-After", "10")
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
	}

	// Enforce the endpoint's maxTokens cap.
	maxTokens := req.MaxTokens
	if ep.Spec.MaxTokens != nil && (maxTokens == 0 || maxTokens > *ep.Spec.MaxTokens) {
		maxTokens = *ep.Spec.MaxTokens
	}

	key := bedrock.EndpointKey{
		RoleArn: ep.Spec.IRSARoleArn,
		Region:  ep.Spec.Region,
	}

	start := time.Now()
	result, err := h.bedrockClient.Invoke(ctx, key, bedrock.InvokeInput{
		ModelId:      ep.Spec.ModelId,
		Messages:     req.Messages,
		SystemPrompt: req.SystemPrompt,
		MaxTokens:    maxTokens,
		Temperature:  req.Temperature,
	})

	duration := time.Since(start).Seconds()
	labels := []string{namespace, name, ep.Spec.ModelId}

	if err != nil {
		h.logger.Error("bedrock invocation failed", "endpoint", name, "error", err)
		metrics.InvocationTotal.WithLabelValues(append([]string{namespace, name, ep.Spec.ModelId}, "error")...).Inc()
		metrics.ReconcileErrorsTotal.WithLabelValues("proxy", "invocation_error").Inc()
		http.Error(w, fmt.Sprintf("bedrock error: %v", err), http.StatusBadGateway)
		return
	}

	metrics.InvocationTotal.WithLabelValues(append(labels, "success")...).Inc()
	metrics.InputTokensTotal.WithLabelValues(labels...).Add(float64(result.InputTokens))
	metrics.OutputTokensTotal.WithLabelValues(labels...).Add(float64(result.OutputTokens))
	metrics.InvocationDurationSeconds.WithLabelValues(labels...).Observe(duration)

	cost, costErr := bedrock.CalculateCost(ep.Spec.ModelId, result.InputTokens, result.OutputTokens)
	costStr := "unknown"
	if costErr == nil {
		metrics.InvocationCostDollars.WithLabelValues(labels...).Add(cost)
		costStr = strconv.FormatFloat(cost, 'f', 6, 64)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(InvokeResponse{
		Content:      result.Content,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		StopReason:   result.StopReason,
		CostUSD:      costStr,
	})
}

// HandleRender handles POST /render/{namespace}/{name} where the path identifies
// a PromptTemplate. Variables are interpolated and the result is forwarded to
// the template's ModelEndpoint.
func (h *Handler) HandleRender(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	namespace, name, ok := parsePathSegments(r.URL.Path, "/render/")
	if !ok {
		http.Error(w, "path must be /render/{namespace}/{name}", http.StatusBadRequest)
		return
	}

	var req RenderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid request body: %v", err), http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	var pt aiv1alpha1.PromptTemplate
	if err := h.k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &pt); err != nil {
		http.Error(w, "template not found", http.StatusNotFound)
		return
	}

	// Validate that all required variables are present.
	for _, v := range pt.Spec.Variables {
		if v.Required {
			if _, ok := req.Variables[v.Name]; !ok {
				http.Error(w, fmt.Sprintf("missing required variable: %s", v.Name), http.StatusBadRequest)
				return
			}
		}
		// Apply defaults for optional missing variables.
		if _, ok := req.Variables[v.Name]; !ok && v.Default != "" {
			req.Variables[v.Name] = v.Default
		}
	}

	rendered, err := renderTemplate(pt.Spec.Template, req.Variables)
	if err != nil {
		http.Error(w, fmt.Sprintf("template rendering failed: %v", err), http.StatusBadRequest)
		return
	}

	// Forward to the endpoint as a single user message.
	epName := pt.Spec.ModelEndpointRef.Name

	ep, err := h.fetchEndpoint(ctx, namespace, epName)
	if err != nil {
		http.Error(w, "referenced endpoint not found", http.StatusNotFound)
		return
	}

	if !ep.Status.Available {
		http.Error(w, "endpoint not available", http.StatusServiceUnavailable)
		return
	}

	maxTokens := int32(0)
	if pt.Spec.MaxTokens != nil {
		maxTokens = *pt.Spec.MaxTokens
	}
	if ep.Spec.MaxTokens != nil && (maxTokens == 0 || maxTokens > *ep.Spec.MaxTokens) {
		maxTokens = *ep.Spec.MaxTokens
	}

	key := bedrock.EndpointKey{
		RoleArn: ep.Spec.IRSARoleArn,
		Region:  ep.Spec.Region,
	}

	result, err := h.bedrockClient.Invoke(ctx, key, bedrock.InvokeInput{
		ModelId:      ep.Spec.ModelId,
		Messages:     []bedrock.Message{{Role: "user", Content: rendered}},
		SystemPrompt: pt.Spec.SystemPrompt,
		MaxTokens:    maxTokens,
	})
	if err != nil {
		http.Error(w, fmt.Sprintf("bedrock error: %v", err), http.StatusBadGateway)
		return
	}

	labels := []string{namespace, epName, ep.Spec.ModelId}
	metrics.InvocationTotal.WithLabelValues(append(labels, "success")...).Inc()
	metrics.InputTokensTotal.WithLabelValues(labels...).Add(float64(result.InputTokens))
	metrics.OutputTokensTotal.WithLabelValues(labels...).Add(float64(result.OutputTokens))

	cost, costErr := bedrock.CalculateCost(ep.Spec.ModelId, result.InputTokens, result.OutputTokens)
	costStr := "unknown"
	if costErr == nil {
		metrics.InvocationCostDollars.WithLabelValues(labels...).Add(cost)
		costStr = strconv.FormatFloat(cost, 'f', 6, 64)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(InvokeResponse{
		Content:      result.Content,
		InputTokens:  result.InputTokens,
		OutputTokens: result.OutputTokens,
		StopReason:   result.StopReason,
		CostUSD:      costStr,
	})
}

func (h *Handler) fetchEndpoint(ctx context.Context, namespace, name string) (*aiv1alpha1.ModelEndpoint, error) {
	var ep aiv1alpha1.ModelEndpoint
	if err := h.k8sClient.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &ep); err != nil {
		return nil, err
	}
	return &ep, nil
}

func (h *Handler) getLimiter(namespace, name string, rpm int32) *rate.Limiter {
	key := namespace + "/" + name
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.rateLimiters[key]
	if !ok {
		// Convert requests-per-minute to the per-second rate and burst size.
		// Burst is set to 10% of rpm (minimum 1) to allow short bursts without
		// immediately queuing.
		r := rate.Limit(float64(rpm) / 60.0)
		burst := int(rpm) / 10
		if burst < 1 {
			burst = 1
		}
		entry = &rateLimiterEntry{limiter: rate.NewLimiter(r, burst)}
		h.rateLimiters[key] = entry
	}
	return entry.limiter
}

// parsePathSegments extracts namespace and name from a path like
// /invoke/{namespace}/{name}.
func parsePathSegments(path, prefix string) (namespace, name string, ok bool) {
	trimmed := strings.TrimPrefix(path, prefix)
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// renderTemplate interpolates a Go text/template with the provided variables.
// Variables map keys must match the {{.Key}} references in the template.
func renderTemplate(tmplText string, vars map[string]string) (string, error) {
	tmpl, err := template.New("prompt").Parse(tmplText)
	if err != nil {
		return "", fmt.Errorf("parsing template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("executing template: %w", err)
	}

	return buf.String(), nil
}
