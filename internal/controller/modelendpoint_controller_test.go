package controller_test

import (
	"context"
	"log/slog"
	"os"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
	"github.com/kernelpanic09/k8s-ai-operator/internal/controller"
)

func newTestReconciler(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) (*controller.ModelEndpointReconciler, client.Client) {
	t.Helper()
	fakeClient := newFakeClientWithObjects(t, scheme, objs...)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	r := &controller.ModelEndpointReconciler{
		Client:           fakeClient,
		Scheme:           scheme,
		Logger:           logger,
		ProxyNamespace:   "ai-operator-system",
		ProxyServiceName: "k8s-ai-operator-proxy",
		// BedrockClient intentionally nil: the first reconcile only adds a
		// finalizer and requeues, so HealthCheck is never called in these tests.
	}
	return r, fakeClient
}

func TestModelEndpointReconcile_AddsFinalizerOnFirstRun(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := addCoreV1ToScheme(scheme); err != nil {
		t.Fatalf("adding core/v1 scheme: %v", err)
	}

	ep := &aiv1alpha1.ModelEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-ep",
			Namespace: "ai-workloads",
		},
		Spec: aiv1alpha1.ModelEndpointSpec{
			ModelId:     "anthropic.claude-haiku-4-5-20251001-v1:0",
			Region:      "us-east-1",
			IRSARoleArn: "arn:aws:iam::123456789012:role/bedrock-invoker",
		},
	}

	r, fakeClient := newTestReconciler(t, scheme, ep)

	// First reconcile should add the finalizer and return Requeue=true.
	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "ai-workloads", Name: "test-ep"},
	})
	if err != nil {
		t.Fatalf("unexpected reconcile error: %v", err)
	}
	if !result.Requeue {
		t.Error("expected Requeue=true after adding finalizer, got false")
	}

	var updated aiv1alpha1.ModelEndpoint
	if err := fakeClient.Get(context.Background(), types.NamespacedName{
		Namespace: "ai-workloads", Name: "test-ep",
	}, &updated); err != nil {
		t.Fatalf("fetching updated endpoint: %v", err)
	}

	found := false
	for _, f := range updated.Finalizers {
		if f == aiv1alpha1.FinalizerName {
			found = true
		}
	}
	if !found {
		t.Errorf("finalizer %q not found after first reconcile; got %v",
			aiv1alpha1.FinalizerName, updated.Finalizers)
	}
}

func TestModelEndpointReconcile_NotFound(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := addCoreV1ToScheme(scheme); err != nil {
		t.Fatalf("adding core/v1 scheme: %v", err)
	}

	r, _ := newTestReconciler(t, scheme)

	// A reconcile request for a resource that doesn't exist should return
	// no error. This happens when an object is deleted before the reconcile runs.
	_, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "ai-workloads", Name: "gone"},
	})
	if err != nil {
		t.Fatalf("expected nil error for missing resource, got: %v", err)
	}
}

func TestModelEndpointReconcile_DeletionWithoutFinalizer(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := aiv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	if err := addCoreV1ToScheme(scheme); err != nil {
		t.Fatalf("adding core/v1 scheme: %v", err)
	}

	now := metav1.Now()
	ep := &aiv1alpha1.ModelEndpoint{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "deleting-ep",
			Namespace:         "ai-workloads",
			DeletionTimestamp: &now,
			// The object is being deleted but does NOT carry OUR finalizer, so the
			// operator's handleDeletion is a no-op. A deleting object must still
			// carry at least one finalizer to exist at all (the API server would
			// otherwise garbage-collect it), and controller-runtime v0.19's fake
			// client enforces this invariant — so we give it an unrelated one.
			Finalizers: []string{"example.com/other-finalizer"},
		},
		Spec: aiv1alpha1.ModelEndpointSpec{
			ModelId:     "amazon.nova-lite-v1:0",
			Region:      "us-east-1",
			IRSARoleArn: "arn:aws:iam::123456789012:role/bedrock-invoker",
		},
	}

	r, _ := newTestReconciler(t, scheme, ep)

	result, err := r.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "ai-workloads", Name: "deleting-ep"},
	})
	if err != nil {
		t.Fatalf("unexpected error reconciling deleting resource: %v", err)
	}
	if result.Requeue || result.RequeueAfter != 0 {
		t.Errorf("expected empty result for deletion without finalizer, got %+v", result)
	}
}

// --- test helpers ---

func newFakeClientWithObjects(t *testing.T, scheme *runtime.Scheme, objs ...client.Object) client.Client {
	t.Helper()
	builder := newFakeClient(t).WithScheme(scheme)
	for _, obj := range objs {
		builder = builder.WithObjects(obj)
	}
	return builder.Build()
}

func addCoreV1ToScheme(scheme *runtime.Scheme) error {
	return corev1SchemeAdder(scheme)
}

var corev1SchemeAdder func(*runtime.Scheme) error
