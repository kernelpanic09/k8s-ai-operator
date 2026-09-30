package proxy

import (
	"testing"

	"golang.org/x/time/rate"
)

func TestParsePathSegments(t *testing.T) {
	tests := []struct {
		name          string
		path          string
		prefix        string
		wantNamespace string
		wantName      string
		wantOK        bool
	}{
		{
			name:          "valid invoke path",
			path:          "/invoke/default/my-endpoint",
			prefix:        "/invoke/",
			wantNamespace: "default",
			wantName:      "my-endpoint",
			wantOK:        true,
		},
		{
			name:          "valid render path",
			path:          "/render/production/summarizer",
			prefix:        "/render/",
			wantNamespace: "production",
			wantName:      "summarizer",
			wantOK:        true,
		},
		{
			name:   "missing name segment",
			path:   "/invoke/default",
			prefix: "/invoke/",
			wantOK: false,
		},
		{
			name:   "empty namespace",
			path:   "/invoke//my-endpoint",
			prefix: "/invoke/",
			wantOK: false,
		},
		{
			name:   "prefix only",
			path:   "/invoke/",
			prefix: "/invoke/",
			wantOK: false,
		},
		{
			name:   "completely empty after prefix",
			path:   "/invoke/",
			prefix: "/invoke/",
			wantOK: false,
		},
		{
			name:          "name contains a slash (extra depth)",
			path:          "/invoke/ns/name/extra",
			prefix:        "/invoke/",
			wantNamespace: "ns",
			wantName:      "name/extra",
			wantOK:        true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ns, name, ok := parsePathSegments(tc.path, tc.prefix)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !tc.wantOK {
				return
			}
			if ns != tc.wantNamespace {
				t.Errorf("namespace = %q, want %q", ns, tc.wantNamespace)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
		})
	}
}

func TestRenderTemplate(t *testing.T) {
	tests := []struct {
		name     string
		tmpl     string
		vars     map[string]string
		want     string
		wantErr  bool
	}{
		{
			name: "simple variable substitution",
			tmpl: "Summarize the following: {{index . \"text\"}}",
			vars: map[string]string{"text": "hello world"},
			want: "Summarize the following: hello world",
		},
		{
			name: "multiple variables",
			tmpl: "Translate {{index . \"text\"}} from {{index . \"src\"}} to {{index . \"dst\"}}.",
			vars: map[string]string{"text": "bonjour", "src": "French", "dst": "English"},
			want: "Translate bonjour from French to English.",
		},
		{
			name: "no variables in template",
			tmpl: "Just a static prompt.",
			vars: map[string]string{},
			want: "Just a static prompt.",
		},
		{
			name:    "invalid template syntax",
			tmpl:    "Hello {{.Name",
			vars:    map[string]string{},
			wantErr: true,
		},
		{
			name: "missing key renders empty string",
			tmpl: "Value: {{index . \"missing\"}}",
			vars: map[string]string{},
			want: "Value: ",
		},
		{
			name: "nil vars map treated as empty",
			tmpl: "Static.",
			vars: nil,
			want: "Static.",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := renderTemplate(tc.tmpl, tc.vars)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if got != tc.want {
				t.Errorf("output = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestGetLimiter(t *testing.T) {
	tests := []struct {
		name      string
		rpm       int32
		wantLimit rate.Limit
		wantBurst int
	}{
		{
			name:      "60 rpm gives 1 req/sec with 10% burst",
			rpm:       60,
			wantLimit: rate.Limit(60.0 / 60.0),
			wantBurst: 6,
		},
		{
			name:      "rpm below 10 floors burst to 1",
			rpm:       5,
			wantLimit: rate.Limit(5.0 / 60.0),
			wantBurst: 1,
		},
		{
			name:      "rpm of exactly 10 gives burst of 1",
			rpm:       10,
			wantLimit: rate.Limit(10.0 / 60.0),
			wantBurst: 1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := NewHandler(nil, nil, nil)
			limiter := h.getLimiter("default", "endpoint", tc.rpm)
			if limiter.Limit() != tc.wantLimit {
				t.Errorf("Limit() = %v, want %v", limiter.Limit(), tc.wantLimit)
			}
			if limiter.Burst() != tc.wantBurst {
				t.Errorf("Burst() = %v, want %v", limiter.Burst(), tc.wantBurst)
			}
		})
	}
}

// getLimiter caches one limiter per namespace/name key, so a later call with a
// different rpm is silently ignored until the process restarts.
func TestGetLimiterCachesPerEndpoint(t *testing.T) {
	h := NewHandler(nil, nil, nil)

	first := h.getLimiter("default", "my-endpoint", 60)
	second := h.getLimiter("default", "my-endpoint", 60)
	if first != second {
		t.Error("getLimiter should return the same *rate.Limiter for repeated calls with the same key")
	}

	changedRPM := h.getLimiter("default", "my-endpoint", 120)
	if changedRPM != first {
		t.Error("getLimiter should keep serving the cached limiter even when rpm changes for an existing key")
	}

	other := h.getLimiter("default", "other-endpoint", 60)
	if other == first {
		t.Error("getLimiter should create a distinct limiter for a distinct namespace/name key")
	}
}
