package apiserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	v1alpha1 "github.com/dcm-project/acm-cluster-service-provider/api/v1alpha1"
	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/getkin/kin-openapi/openapi3"
	legacyrouter "github.com/getkin/kin-openapi/routers/legacy"
	"github.com/go-chi/chi/v5"
)

// Server is the HTTP server for the cluster service provider API.
type Server struct {
	cfg     *config.Config
	logger  *slog.Logger
	srv     *http.Server
	onReady func(context.Context)
}

// readinessProbeTimeout is how long to wait for the server to confirm it is
// serving HTTP requests before giving up and skipping the onReady callback.
const readinessProbeTimeout = 5 * time.Second

// readinessProbeInterval is the polling interval for the self-probe that
// checks if the server is accepting connections before firing onReady.
const readinessProbeInterval = 50 * time.Millisecond

// New creates a new Server with the given config and logger.
func New(cfg *config.Config, logger *slog.Logger, handler oapigen.ServerInterface) *Server {
	badReq := newBadRequestHandler(logger)

	r := chi.NewRouter()
	r.Use(rfc7807RecoveryMiddleware(logger))
	r.Use(requestLoggingMiddleware(logger))

	spec, err := v1alpha1.GetSwagger()
	if err != nil {
		logger.Warn("failed to load OpenAPI spec, request validation disabled", "error", err)
	} else {
		spec.Servers = nil
		specRouter, routerErr := legacyrouter.NewRouter(spec,
			openapi3.DisableExamplesValidation(),
		)
		if routerErr != nil {
			logger.Warn("failed to create OpenAPI router, request validation disabled", "error", routerErr)
		} else {
			r.Use(openAPIValidationMiddleware(specRouter, badReq))
		}
	}

	httpHandler := oapigen.HandlerWithOptions(handler, oapigen.ChiServerOptions{
		BaseRouter:       r,
		ErrorHandlerFunc: badReq,
	})

	return &Server{
		cfg:    cfg,
		logger: logger,
		srv: &http.Server{
			Handler:      httpHandler,
			ReadTimeout:  cfg.Server.ReadTimeout,
			WriteTimeout: cfg.Server.WriteTimeout,
			IdleTimeout:  cfg.Server.IdleTimeout,
		},
	}
}

// WithOnReady registers a callback invoked once the server is confirmed to be
// serving HTTP requests. The server verifies readiness by polling before
// calling fn.
func (s *Server) WithOnReady(fn func(context.Context)) *Server {
	s.onReady = fn
	return s
}

// waitForReady polls the server until it accepts HTTP connections or the
// context/timeout expires.
func (s *Server) waitForReady(ctx context.Context, addr string) error {
	url := fmt.Sprintf("http://%s/api/v1alpha1/clusters/health", addr)
	client := &http.Client{Timeout: 1 * time.Second}

	deadline := time.NewTimer(readinessProbeTimeout)
	defer deadline.Stop()

	ticker := time.NewTicker(readinessProbeInterval)
	defer ticker.Stop()

	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return fmt.Errorf("creating readiness probe request: %w", err)
		}
		resp, err := client.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return fmt.Errorf("server readiness probe timed out after %s", readinessProbeTimeout)
		case <-ticker.C:
		}
	}
}

// Run starts the HTTP server on the provided listener and blocks until
// the context is cancelled. Signal handling is the caller's responsibility;
// pass a context that is cancelled on SIGTERM/SIGINT (e.g., via
// signal.NotifyContext).
func (s *Server) Run(ctx context.Context, ln net.Listener) error {
	if s.cfg.Server.RequestTimeout > 0 {
		s.srv.Handler = requestTimeoutMiddleware(s.cfg.Server.RequestTimeout)(s.srv.Handler)
	}

	s.logger.Info("server starting", "address", ln.Addr().String())

	serveCh := make(chan error, 1)
	go func() {
		if err := s.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveCh <- err
		}
		close(serveCh)
	}()

	if s.onReady != nil {
		if err := s.waitForReady(ctx, ln.Addr().String()); err != nil {
			s.logger.Error("readiness probe failed, skipping onReady callback", "error", err)
		} else {
			func() {
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("onReady callback panicked", "panic", r)
					}
				}()
				s.onReady(ctx)
			}()
		}
	}

	select {
	case <-ctx.Done():
	case err := <-serveCh:
		if err != nil {
			return fmt.Errorf("serving on %s: %w", ln.Addr(), err)
		}
	}

	s.logger.Info("shutting down server")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), s.cfg.Server.ShutdownTimeout)
	defer shutdownCancel()

	if err := s.srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutting down server: %w", err)
	}
	return nil
}
