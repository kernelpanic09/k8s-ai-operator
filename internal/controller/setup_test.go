package controller_test

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

func init() {
	// Wire the corev1 scheme adder used by modelendpoint_controller_test.go.
	// Keeping this in a separate file makes it easy to see what init-time
	// scheme registration is happening.
	corev1SchemeAdder = func(s *runtime.Scheme) error {
		return corev1.AddToScheme(s)
	}

	_ = corev1.Pod{} // ensure corev1 is imported
}
