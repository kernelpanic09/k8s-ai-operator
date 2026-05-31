package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	aiv1alpha1 "github.com/kernelpanic09/k8s-ai-operator/api/v1alpha1"
	"github.com/kernelpanic09/k8s-ai-operator/internal/bedrock"
	"github.com/kernelpanic09/k8s-ai-operator/internal/controller"
	_ "github.com/kernelpanic09/k8s-ai-operator/internal/metrics" // side-effect: registers Prometheus metrics
	"github.com/kernelpanic09/k8s-ai-operator/internal/proxy"
	internalwebhook "github.com/kernelpanic09/k8s-ai-operator/internal/webhook"
)

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(aiv1alpha1.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
}

func main() {
	var (
		metricsAddr          string
		probeAddr            string
		proxyAddr            string
		enableLeaderElection bool
		enableWebhooks       bool
		leaderElectionID     string
		proxyNamespace       string
		proxyServiceName     string
		logLevel             string
	)

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080",
		"Address the Prometheus metrics endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081",
		"Address the health/ready probe endpoint binds to.")
	flag.StringVar(&proxyAddr, "proxy-bind-address", ":8090",
		"Address the Bedrock proxy HTTP server binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election. Required when running more than one replica.")
	flag.BoolVar(&enableWebhooks, "enable-webhooks", false,
		"Enable admission webhooks. Requires cert-manager or manual TLS certificate provisioning.")
	flag.StringVar(&leaderElectionID, "leader-election-id", "ai.kernelpanic09.io",
		"Lease name used for leader election.")
	flag.StringVar(&proxyNamespace, "proxy-namespace", "ai-operator-system",
		"Namespace this operator runs in. Used to construct ExternalName service targets.")
	flag.StringVar(&proxyServiceName, "proxy-service-name", "k8s-ai-operator-proxy",
		"Name of this operator's Service. Used to construct ExternalName service targets.")
	flag.StringVar(&logLevel, "log-level", "info",
		"Log verbosity: debug, info, warn, error.")

	flag.Parse()

	// slog is the logging interface throughout the operator. controller-runtime
	// uses logr; we bridge via zapr but expose slog to our own code.
	level := slog.LevelInfo
	switch logLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	slogger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(slogger)

	// controller-runtime uses logr; give it a zap logger that mirrors our level.
	zapOpts := zap.Options{Development: level == slog.LevelDebug}
	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&zapOpts)))
	log.Log.Info("starting k8s-ai-operator")

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Metrics: metricsserver.Options{
			BindAddress: metricsAddr,
		},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       leaderElectionID,
		// GracefulShutdownTimeout gives in-flight reconciles time to finish.
		GracefulShutdownTimeout: func() *time.Duration { d := 30 * time.Second; return &d }(),
		WebhookServer: webhook.NewServer(webhook.Options{
			Port: 9443,
		}),
	})
	if err != nil {
		slogger.Error("creating manager", "error", err)
		os.Exit(1)
	}

	// Build the Bedrock client from ambient pod credentials (IRSA).
	// We use a background context here; the client caches credentials
	// and individual calls use the per-request context.
	bedrockClient, err := bedrock.NewClient(context.Background())
	if err != nil {
		slogger.Error("creating bedrock client", "error", err)
		os.Exit(1)
	}

	// Register controllers.
	if err := (&controller.ModelEndpointReconciler{
		Client:           mgr.GetClient(),
		Scheme:           mgr.GetScheme(),
		BedrockClient:    bedrockClient,
		Logger:           slogger.With("controller", "ModelEndpoint"),
		ProxyNamespace:   proxyNamespace,
		ProxyServiceName: proxyServiceName,
	}).SetupWithManager(mgr); err != nil {
		slogger.Error("creating ModelEndpoint controller", "error", err)
		os.Exit(1)
	}

	if err := (&controller.PromptTemplateReconciler{
		Client: mgr.GetClient(),
		Scheme: mgr.GetScheme(),
		Logger: slogger.With("controller", "PromptTemplate"),
	}).SetupWithManager(mgr); err != nil {
		slogger.Error("creating PromptTemplate controller", "error", err)
		os.Exit(1)
	}

	// GuardrailPolicy controller: build a control-plane client for the operator's
	// own role (used to create/update/delete guardrails in the operator's region).
	// The operator IAM role must have bedrock:CreateGuardrail etc.
	guardrailKey := bedrock.EndpointKey{
		RoleArn: os.Getenv("OPERATOR_ROLE_ARN"), // optional; defaults to ambient IRSA role
		Region:  os.Getenv("AWS_REGION"),
	}
	if guardrailKey.Region == "" {
		guardrailKey.Region = "us-east-1"
	}
	guardrailClient, err := bedrockClient.NewGuardrailClient(context.Background(), guardrailKey)
	if err != nil {
		// Non-fatal: GuardrailPolicy controller won't function but other controllers will.
		slogger.Warn("failed to build guardrail client; GuardrailPolicy controller disabled", "error", err)
		guardrailClient = nil
	}

	if guardrailClient != nil {
		if err := (&controller.GuardrailPolicyReconciler{
			Client:  mgr.GetClient(),
			Scheme:  mgr.GetScheme(),
			Logger:  slogger.With("controller", "GuardrailPolicy"),
			Bedrock: guardrailClient,
		}).SetupWithManager(mgr); err != nil {
			slogger.Error("creating GuardrailPolicy controller", "error", err)
			os.Exit(1)
		}
	}

	if enableWebhooks {
		validator := &internalwebhook.ModelEndpointValidator{}
		if err := validator.SetupWithManager(mgr); err != nil {
			slogger.Error("registering ModelEndpoint validator webhook", "error", err)
			os.Exit(1)
		}

		defaulter := internalwebhook.NewModelEndpointDefaulter(mgr.GetScheme())
		if err := defaulter.SetupWithManager(mgr); err != nil {
			slogger.Error("registering ModelEndpoint defaulter webhook", "error", err)
			os.Exit(1)
		}
	}

	// Start the proxy HTTP server in a goroutine managed by the controller-runtime
	// lifecycle. It shuts down cleanly when the manager context is cancelled.
	proxyHandler := proxy.NewHandler(mgr.GetClient(), bedrockClient, slogger.With("component", "proxy"))
	proxyServer := proxy.NewServer(proxy.ServerConfig{
		ListenAddr: proxyAddr,
		Handler:    proxyHandler,
	}, slogger.With("component", "proxy-server"))

	if err := mgr.Add(newRunnableProxy(proxyServer)); err != nil {
		slogger.Error("adding proxy to manager", "error", err)
		os.Exit(1)
	}

	// Health probes: /healthz returns 200 immediately; /readyz waits for cache sync.
	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		slogger.Error("adding healthz check", "error", err)
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		slogger.Error("adding readyz check", "error", err)
		os.Exit(1)
	}

	slogger.Info("manager starting",
		"metrics", metricsAddr,
		"probes", probeAddr,
		"proxy", proxyAddr,
		"leader-elect", enableLeaderElection,
		"webhooks", enableWebhooks,
	)

	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		slogger.Error("manager exited with error", "error", err)
		os.Exit(1)
	}
}

// runnableProxy adapts proxy.Server to the ctrl.Runnable interface so the
// manager can start and stop it as part of its lifecycle.
type runnableProxy struct {
	server *proxy.Server
}

func newRunnableProxy(s *proxy.Server) *runnableProxy {
	return &runnableProxy{server: s}
}

func (r *runnableProxy) Start(ctx context.Context) error {
	return r.server.Start(ctx)
}
