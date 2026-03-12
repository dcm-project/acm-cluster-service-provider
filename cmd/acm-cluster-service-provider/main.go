package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
	spmclient "github.com/dcm-project/service-provider-manager/pkg/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// version is the application version, set at build time via
// -ldflags "-X main.version=X.Y.Z".
var version = "0.0.1-dev"

// TODO(topic-5): remove bootstrapHandler once K8s client is wired; the real
// health implementation lives in handler.Handler + health.Checker.
//
// bootstrapHandler provides a minimal ServerInterface implementation for
// startup. It embeds Unimplemented (returning 501 for all CRUD endpoints)
// and overrides only GetHealth to return 200, which is required for the
// server's readiness probe to succeed and trigger registration.
type bootstrapHandler struct {
	oapigen.Unimplemented
	startTime time.Time
}

func (h *bootstrapHandler) GetHealth(w http.ResponseWriter, _ *http.Request) {
	uptime := max(0, int(time.Since(h.startTime).Seconds()))
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(oapigen.Health{
		Status:  util.Ptr("healthy"),
		Type:    util.Ptr("acm-cluster-service-provider.dcm.io/health"),
		Path:    util.Ptr("health"),
		Version: &version,
		Uptime:  &uptime,
	})
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("fatal error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.Server.BindAddress)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Server.BindAddress, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	dcmClient, err := spmclient.NewClientWithResponses(cfg.Registration.DCMRegistrationURL)
	if err != nil {
		return fmt.Errorf("creating DCM client: %w", err)
	}

	restCfg, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("loading kubeconfig: %w", err)
	}

	k8sClient, err := client.New(restCfg, client.Options{})
	if err != nil {
		return fmt.Errorf("creating kubernetes client: %w", err)
	}

	registrar := registration.New(cfg.Registration, dcmClient, k8sClient, logger)

	srv := apiserver.New(cfg, logger, &bootstrapHandler{}).WithOnReady(registrar.Start)

	return srv.Run(ctx, ln)
}
