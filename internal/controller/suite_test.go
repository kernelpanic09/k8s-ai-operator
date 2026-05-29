package controller_test

import (
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// newFakeClient returns a fake client builder pre-seeded with our CRD types.
// Callers add objects and call Build() themselves.
func newFakeClient(t *testing.T) *fake.ClientBuilder {
	t.Helper()
	return fake.NewClientBuilder()
}
