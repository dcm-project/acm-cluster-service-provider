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

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/util"
)

// version is the application version, set at build time via
// -ldflags "-X main.version=X.Y.Z".
var version = "0.0.1-dev"

// bootstrapHandler provides a minimal ServerInterface implementation for
// startup. It embeds Unimplemented (returning 501 for all CRUD endpoints)
// and overrides only GetHealth to return 200, which is required for the
// server's readiness probe to succeed and trigger registration.
type bootstrapHandler struct {
	oapigen.Unimplemented
}

func (h *bootstrapHandler) GetHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(oapigen.Health{
		Status:  util.Ptr("healthy"),
		Version: &version,
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


	srv := apiserver.New(cfg, logger, &bootstrapHandler{})

	return srv.Run(ctx, ln)
}
