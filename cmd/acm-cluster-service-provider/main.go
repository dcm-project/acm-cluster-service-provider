package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/handler"
	"github.com/dcm-project/acm-cluster-service-provider/pkg/runtime"
)

// version is the application version, set at build time via
// -ldflags "-X main.version=X.Y.Z".
var version = "0.0.1-dev"

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
	if err := runtime.PrepareConfig(cfg); err != nil {
		return fmt.Errorf("preparing configuration: %w", err)
	}

	ln, err := net.Listen("tcp", cfg.Server.BindAddress)
	if err != nil {
		return fmt.Errorf("listening on %s: %w", cfg.Server.BindAddress, err)
	}
	defer func() { _ = ln.Close() }()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	rt, err := runtime.New(ctx, cfg, logger, runtime.Options{Version: version})
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := rt.Close(); closeErr != nil {
			logger.Error("failed to close runtime", "error", closeErr)
		}
	}()

	strictHandler := handler.New(rt.ClusterService(), rt.HealthChecker(), logger)
	h := oapigen.NewStrictHandler(strictHandler, nil)
	srv := apiserver.New(cfg, logger, h).WithOnReady(rt.Start)

	return srv.Run(ctx, ln)
}
