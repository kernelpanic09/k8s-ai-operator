package bedrock

import (
	"testing"
)

func TestDefaultBlockedInput(t *testing.T) {
	tests := []struct {
		name string
		msg  string
		want string
	}{
		{
			name: "empty string returns built-in default",
			msg:  "",
			want: defaultGuardrailBlockedMessage,
		},
		{
			name: "custom message is returned unchanged",
			msg:  "Blocked by content policy.",
			want: "Blocked by content policy.",
		},
		{
			name: "whitespace-only string is not treated as empty",
			msg:  " ",
			want: " ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DefaultBlockedInput(tt.msg)
			if got == nil {
				t.Fatal("DefaultBlockedInput returned nil, want non-nil *string")
			}
			if *got != tt.want {
				t.Errorf("DefaultBlockedInput(%q) = %q, want %q", tt.msg, *got, tt.want)
			}
		})
	}
}
