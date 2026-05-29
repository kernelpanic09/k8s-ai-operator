// Package proxy implements the HTTP server that sits in front of Bedrock.
// Each ModelEndpoint gets a Kubernetes Service that points at this server;
// workloads POST to the Service and the proxy handles auth, rate limiting,
// cost gating, and forwarding to Bedrock.
package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// Server is the proxy HTTP server. One instance handles all ModelEndpoints;
// routing is done by URL path (/invoke/{namespace}/{name}).
type Server struct {
	handler *Handler
	server  *http.Server
	logger  *slog.Logger
}

// ServerConfig is passed to NewServer.
type ServerConfig struct {
	// ListenAddr is the address:port the proxy listens on, e.g. ":8090".
	ListenAddr string

	// Handler is the request handler (contains rate limiters, bedrock client, etc.).
	Handler *Handler

	// ReadTimeout and WriteTimeout apply to individual HTTP connections.
	// WriteTimeout should be long enough for slow Bedrock completions.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// NewServer constructs a Server. Call Start to begin accepting connections.
func NewServer(cfg ServerConfig, logger *slog.Logger) *Server {
	mux := http.NewServeMux()

	s := &Server{
		handler: cfg.Handler,
		logger:  logger,
	}

	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/invoke/", cfg.Handler.HandleInvoke)
	mux.HandleFunc("/render/", cfg.Handler.HandleRender)

	readTimeout := cfg.ReadTimeout
	if readTimeout == 0 {
		readTimeout = 10 * time.Second
	}
	writeTimeout := cfg.WriteTimeout
	if writeTimeout == 0 {
		// Bedrock completions can be slow for long contexts; 120s is generous.
		writeTimeout = 120 * time.Second
	}

	s.server = &http.Server{
		Addr:         cfg.ListenAddr,
		Handler:      mux,
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
	}

	return s
}

// Start begins serving HTTP requests. It blocks until the context is cancelled.
func (s *Server) Start(ctx context.Context) error {
	errCh := make(chan error, 1)

	go func() {
		s.logger.Info("proxy server listening", "addr", s.server.Addr)
		if err := s.server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			errCh <- fmt.Errorf("proxy server: %w", err)
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := s.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("proxy server shutdown: %w", err)
		}
		return nil
	}
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
