package proxy

import (
	"testing"
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
