package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	oapigen "github.com/dcm-project/acm-cluster-service-provider/internal/api/server"
	"github.com/dcm-project/acm-cluster-service-provider/internal/apiserver"
	"github.com/dcm-project/acm-cluster-service-provider/internal/cluster/dispatcher"
	"github.com/dcm-project/acm-cluster-service-provider/internal/config"
	"github.com/dcm-project/acm-cluster-service-provider/internal/handler"
	"github.com/dcm-project/acm-cluster-service-provider/internal/health"
	"github.com/dcm-project/acm-cluster-service-provider/internal/monitoring"
	"github.com/dcm-project/acm-cluster-service-provider/internal/registration"
	spmclient "github.com/dcm-project/service-provider-manager/pkg/client"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	dynamicClient, err := dynamic.NewForConfig(restCfg)
	if err != nil {
		return fmt.Errorf("creating dynamic kubernetes client: %w", err)
	}

	matrix, err := registration.LoadCompatibilityMatrix(cfg.Cluster.VersionMatrixPath)
	if err != nil {
		return fmt.Errorf("loading compatibility matrix: %w", err)
	}
	cfg.Cluster.VersionMatrix = map[string]string(matrix)

	registrar := registration.New(cfg.Registration, dcmClient, k8sClient, logger, matrix)
	clusterService := dispatcher.New(k8sClient, cfg.Cluster, cfg.Health.EnabledPlatforms)

	publisher, err := monitoring.NewNATSPublisher(cfg.Monitoring.NATSUrl, cfg.Registration.ProviderName, logger)
	if err != nil {
		return fmt.Errorf("creating NATS publisher: %w", err)
	}
	defer func() { _ = publisher.Close() }()

	monitorCfg := monitoring.MonitorConfig{
		Namespace:            cfg.Cluster.ClusterNamespace,
		ProviderName:         cfg.Registration.ProviderName,
		DebounceInterval:     cfg.Monitoring.DebounceInterval,
		ResyncInterval:       cfg.Monitoring.ResyncInterval,
		PublishRetryMax:      cfg.Monitoring.PublishRetryMax,
		PublishRetryInterval: cfg.Monitoring.PublishRetryInterval,
	}
	monitor := monitoring.New(dynamicClient, monitorCfg, publisher, logger)

	startTime := time.Now()
	checker := health.NewChecker(k8sClient, cfg.Health, version, startTime)
	strictHandler := handler.New(clusterService, checker)
	h := oapigen.NewStrictHandler(strictHandler, nil)
	srv := apiserver.New(cfg, logger, h).WithOnReady(func(ctx context.Context) {
		registrar.Start(ctx)
		go func() {
			if err := monitor.Start(ctx); err != nil {
				logger.Error("status monitor failed", "error", err)
			}
		}()
	})

	return srv.Run(ctx, ln)
}
