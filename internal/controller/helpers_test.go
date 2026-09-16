package controller

import (
	"strings"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestSanitizeLabel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty string", "", ""},
		{"already valid", "my-model", "my-model"},
		{"dots and underscores allowed", "v1.0_rc", "v1.0_rc"},
		{"colon replaced", "model:version", "model_version"},
		// Real Bedrock model ID format: contains a colon before the version suffix.
		{"bedrock model id", "anthropic.claude-haiku-4-5-20251001-v1:0", "anthropic.claude-haiku-4-5-20251001-v1_0"},
		{"at-sign replaced", "ns@region", "ns_region"},
		{"slash replaced", "foo/bar", "foo_bar"},
		{"exactly 63 chars preserved", strings.Repeat("a", 63), strings.Repeat("a", 63)},
		{"64 chars truncated to 63", strings.Repeat("a", 64), strings.Repeat("a", 63)},
		{"100 chars truncated to 63", strings.Repeat("z", 100), strings.Repeat("z", 63)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sanitizeLabel(tt.input)
			if got != tt.want {
				t.Errorf("sanitizeLabel(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if len(got) > 63 {
				t.Errorf("sanitizeLabel(%q) returned %d chars, must be ≤ 63", tt.input, len(got))
			}
		})
	}
}

func TestFormatCost(t *testing.T) {
	tests := []struct {
		v    float64
		want string
	}{
		{0, "0.00"},
		{1, "1.00"},
		{0.5, "0.50"},
		{0.001, "0.00"},
		{1234.5678, "1234.57"},
	}

	for _, tt := range tests {
		got := FormatCost(tt.v)
		if got != tt.want {
			t.Errorf("FormatCost(%v) = %q, want %q", tt.v, got, tt.want)
		}
	}
}

func TestConditionStatus(t *testing.T) {
	if got := conditionStatus(true); got != metav1.ConditionTrue {
		t.Errorf("conditionStatus(true) = %v, want ConditionTrue", got)
	}
	if got := conditionStatus(false); got != metav1.ConditionFalse {
		t.Errorf("conditionStatus(false) = %v, want ConditionFalse", got)
	}
}

func TestSetCondition(t *testing.T) {
	t.Run("appends to empty slice", func(t *testing.T) {
		var conds []metav1.Condition
		setCondition(&conds, metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Available",
		})
		if len(conds) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conds))
		}
		if conds[0].Type != "Ready" || conds[0].Status != metav1.ConditionTrue {
			t.Errorf("unexpected condition: %+v", conds[0])
		}
	})

	t.Run("updates status and refreshes LastTransitionTime on status change", func(t *testing.T) {
		old := metav1.NewTime(time.Now().Add(-time.Hour))
		conds := []metav1.Condition{{
			Type:               "Ready",
			Status:             metav1.ConditionFalse,
			Reason:             "Initialising",
			LastTransitionTime: old,
		}}
		next := metav1.NewTime(time.Now())
		setCondition(&conds, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Available",
			LastTransitionTime: next,
		})
		if len(conds) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conds))
		}
		if conds[0].Status != metav1.ConditionTrue {
			t.Errorf("status should be updated to True, got %v", conds[0].Status)
		}
		if conds[0].LastTransitionTime.Equal(&old) {
			t.Error("LastTransitionTime should advance when status changes")
		}
	})

	t.Run("preserves LastTransitionTime when status unchanged", func(t *testing.T) {
		original := metav1.NewTime(time.Now().Add(-time.Hour))
		conds := []metav1.Condition{{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "Available",
			LastTransitionTime: original,
		}}
		setCondition(&conds, metav1.Condition{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "StillAvailable",
			LastTransitionTime: metav1.Now(),
		})
		if len(conds) != 1 {
			t.Fatalf("expected 1 condition, got %d", len(conds))
		}
		if !conds[0].LastTransitionTime.Equal(&original) {
			t.Errorf("LastTransitionTime should be preserved; got %v, want %v",
				conds[0].LastTransitionTime, original)
		}
		if conds[0].Reason != "StillAvailable" {
			t.Errorf("Reason should be updated; got %q", conds[0].Reason)
		}
	})

	t.Run("does not touch unrelated conditions", func(t *testing.T) {
		conds := []metav1.Condition{
			{Type: "BedrockReachable", Status: metav1.ConditionFalse, Reason: "Unreachable"},
			{Type: "Ready", Status: metav1.ConditionFalse, Reason: "Waiting"},
		}
		setCondition(&conds, metav1.Condition{
			Type:   "Ready",
			Status: metav1.ConditionTrue,
			Reason: "Available",
		})
		if len(conds) != 2 {
			t.Fatalf("expected 2 conditions, got %d", len(conds))
		}
		if conds[0].Type != "BedrockReachable" || conds[0].Status != metav1.ConditionFalse {
			t.Errorf("unrelated condition was modified: %+v", conds[0])
		}
	})
}
